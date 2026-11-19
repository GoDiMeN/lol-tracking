package ranking

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"loltracking/server/banco"
	"loltracking/server/campeao"
	"loltracking/server/lcu"
	"loltracking/server/riot"
	"loltracking/server/web"
)

// A ATUALIZAÇÃO de um ranking é uma execução com SSE, uma por vez:
//  1. liga: lê a liga do elo (Mestre para cima: a liga inteira, 1 pedido;
//     abaixo: PaginasPorDivisao páginas de cada divisão, uma AMOSTRA) e escolhe
//     os candidatos: os primeiros pela pré-nota "subindo" e pela "melhor",
//     intercalados, até o teto (PreNota em formula.go);
//  2. históricos: lê o histórico de cada candidato no patch pela fonte
//     (fontes.go), um por vez, guardando cada um no banco: a pausa para entre
//     um candidato e outro e a retomada continua do próximo pendente; o cache
//     (contas, ids, partidas) faz a retomada e a próxima atualização não pagarem
//     de novo;
//  3. cálculo: os rankings das 5 rotas e os de cada campeão com jogadores
//     suficientes, nos dois modos, gravados de uma vez.

// Elos é a ordem dos elos (nada abaixo de Prata, como a coleta).
var Elos = []string{"SILVER", "GOLD", "PLATINUM", "EMERALD", "DIAMOND", "MASTER", "GRANDMASTER", "CHALLENGER"}

// NomeElo dá o nome em português.
var NomeElo = map[string]string{
	"SILVER": "Prata", "GOLD": "Ouro", "PLATINUM": "Platina", "EMERALD": "Esmeralda", "DIAMOND": "Diamante",
	"MASTER": "Mestre", "GRANDMASTER": "Grão-Mestre", "CHALLENGER": "Desafiante",
}

// Rotas: as cinco, na ordem da tela.
var Rotas = []string{"TOP", "JUNGLE", "MIDDLE", "BOTTOM", "UTILITY"}

// NomeRota: o nome da rota na tela.
var NomeRota = map[string]string{"TOP": "topo", "JUNGLE": "selva", "MIDDLE": "meio", "BOTTOM": "atirador", "UTILITY": "suporte"}

// Deps é o mundo de fora do ranking.
type Deps struct {
	DB   *banco.DB
	Riot FonteRiotAPI
	// LCU: nil = sem cliente do LoL (servidor): só a fonte "riot".
	LCU lcu.Cliente
	Hub *web.Hub
	// FontePadrao: a fonte quando o pedido não diz (LOL_RANKING_FONTE; "lcu" na máquina do dono).
	FontePadrao string
	// PatchAtual: o patch quando o pedido não diz (o do jogo instalado). nil ou
	// erro = o da partida solo mais nova do primeiro candidato com partida.
	PatchAtual func(ctx context.Context) (string, error)
	// Campeoes: o nome dos campeões (nil = "campeão <id>").
	Campeoes campeao.Nomes
}

// Atualizador roda uma atualização por vez.
type Atualizador struct {
	d Deps

	mu        sync.Mutex
	atual     *corrida
	execucoes map[int64]string
}

type corrida struct {
	id      int64
	ex      *web.Execucao
	cancela context.CancelFunc
	feito   chan struct{}
	pausar  atomic.Bool
}

// Novo cria o atualizador. Atualização que estava "rodando" vira "pausada".
func Novo(d Deps) *Atualizador {
	if d.FontePadrao == "" {
		d.FontePadrao = FonteLCU
	}
	if err := Recupera(d.DB); err != nil {
		log.Printf("ranking: recuperar atualizações interrompidas: %v", err)
	}
	return &Atualizador{d: d, execucoes: map[int64]string{}}
}

// DB: o banco (o aprendizado lê os rankings por ele).
func (a *Atualizador) DB() *banco.DB { return a.d.DB }

// Pedido é o corpo de criar uma atualização.
type Pedido struct {
	Regiao string `json:"regiao"`
	Elo    string `json:"elo"`
	// Patch: "16.23"; vazio = o do jogo instalado (ou o da partida mais nova).
	Patch string `json:"patch"`
	// Fonte: "lcu" ou "riot"; vazio = a padrão da configuração.
	Fonte              string `json:"fonte"`
	Candidatos         int    `json:"candidatos"`
	PartidasPorJogador int    `json:"partidas_por_jogador"`
}

func (p *Pedido) valida(padrao string) (riot.Regiao, error) {
	if p.Regiao == "" {
		p.Regiao = riot.RegiaoPadrao
	}
	reg, ok := riot.RegiaoPorCodigo(p.Regiao)
	if !ok {
		return reg, &ErroPedido{fmt.Sprintf("por enquanto só a região BR (BR1); %q ainda não está disponível", p.Regiao)}
	}
	p.Elo = strings.ToUpper(strings.TrimSpace(p.Elo))
	if _, ok := NomeElo[p.Elo]; !ok {
		return reg, &ErroPedido{fmt.Sprintf("elo desconhecido: %q (use SILVER a CHALLENGER)", p.Elo)}
	}
	p.Patch = strings.TrimSpace(p.Patch)
	if p.Patch != "" {
		partes := strings.Split(p.Patch, ".")
		if len(partes) < 2 || ComparaPatch(p.Patch, "0") <= 0 {
			return reg, &ErroPedido{fmt.Sprintf("patch inválido: %q (use o formato 16.23)", p.Patch)}
		}
		p.Patch = lcu.PatchDe(p.Patch)
	}
	if p.Fonte == "" {
		p.Fonte = padrao
	}
	if p.Fonte != FonteLCU && p.Fonte != FonteRiot {
		return reg, &ErroPedido{`fonte precisa ser "lcu" (cliente do LoL) ou "riot" (só a API da Riot)`}
	}
	if p.Candidatos == 0 {
		p.Candidatos = TetoCandidatosPadrao
	}
	if p.Candidatos < 1 || p.Candidatos > MaxCandidatos {
		return reg, &ErroPedido{fmt.Sprintf("candidatos precisa ser de 1 a %d", MaxCandidatos)}
	}
	if p.PartidasPorJogador == 0 {
		p.PartidasPorJogador = TetoPartidasPadrao
	}
	if p.PartidasPorJogador < MinPartidasRecorte || p.PartidasPorJogador > MaxPartidas {
		return reg, &ErroPedido{fmt.Sprintf("partidas_por_jogador precisa ser de %d a %d", MinPartidasRecorte, MaxPartidas)}
	}
	return reg, nil
}

// frasesRiot traduz o erro da chave para a pessoa.
func frasesRiot(err error) error {
	switch {
	case errors.Is(err, riot.ErrSemChave):
		return &ErroConflito{"sem chave da API da Riot: o ranking precisa dela para ler a liga. Gere uma chave em developer.riotgames.com e coloque em token.txt"}
	case errors.Is(err, riot.ErrChaveRecusada):
		return &ErroConflito{"a Riot recusou a chave (a de desenvolvimento vence em 24 h): gere outra em developer.riotgames.com, troque o token.txt e retome"}
	}
	return err
}

// confere: a chave e, na fonte LCU, o cliente aberto na região da atualização.
func (a *Atualizador) confere(ctx context.Context, reg riot.Regiao, fonte string) error {
	if err := a.d.Riot.Confere(ctx); err != nil {
		if errors.Is(err, riot.ErrSemChave) || errors.Is(err, riot.ErrChaveRecusada) {
			return frasesRiot(err)
		}
		return &ErroConflito{"não deu para conferir a chave da Riot: " + err.Error()}
	}
	if fonte != FonteLCU {
		return nil
	}
	if a.d.LCU == nil {
		return &ErroConflito{`a fonte "lcu" precisa do cliente do LoL, e este servidor não tem: use "fonte": "riot"`}
	}
	r, err := a.d.LCU.Regiao(ctx)
	if err != nil {
		return err
	}
	if r.Regiao != reg.RegiaoCliente {
		return &ErroConflito{fmt.Sprintf("a conta logada no cliente do LoL é da região %q e o ranking é de %s: com a fonte \"lcu\" o jogador é achado pelo cliente. Use \"fonte\": \"riot\" ou entre com uma conta de %s",
			r.Regiao, reg.Nome, reg.Nome)}
	}
	return nil
}

func (a *Atualizador) fonte(nome string) FonteHistorico {
	if nome == FonteRiot {
		return NovaFonteRiot(a.d.DB, a.d.Riot)
	}
	return NovaFonteLCU(a.d.DB, a.d.Riot, a.d.LCU)
}

func (a *Atualizador) ocupado() error {
	if a.atual != nil {
		return &ErroConflito{fmt.Sprintf("já há uma atualização de ranking rodando (nº %d); pause ou cancele antes", a.atual.id)}
	}
	return nil
}

// Cria valida, confere e começa uma atualização.
func (a *Atualizador) Cria(ctx context.Context, p Pedido) (Atualizacao, string, error) {
	reg, err := p.valida(a.d.FontePadrao)
	if err != nil {
		return Atualizacao{}, "", err
	}
	if err := a.confere(ctx, reg, p.Fonte); err != nil {
		return Atualizacao{}, "", err
	}
	if p.Patch == "" && a.d.PatchAtual != nil {
		if v, err := a.d.PatchAtual(ctx); err == nil && v != "" {
			p.Patch = lcu.PatchDe(v)
		}
	}
	a.mu.Lock()
	if err := a.ocupado(); err != nil {
		a.mu.Unlock()
		return Atualizacao{}, "", err
	}
	id, err := criaAtualizacao(a.d.DB, Atualizacao{Regiao: reg.Codigo, Patch: p.Patch, Elo: p.Elo, Fonte: p.Fonte,
		TetoCandidatos: p.Candidatos, TetoPartidas: p.PartidasPorJogador})
	if err != nil {
		a.mu.Unlock()
		return Atualizacao{}, "", err
	}
	exID := a.comecaTravado(id)
	a.mu.Unlock()
	// Le trava a.mu de novo (a execução atual): fora da trava.
	at, err := a.Le(id)
	return at, exID, err
}

// Retoma continua uma atualização pausada (ou que falhou) do próximo candidato pendente.
func (a *Atualizador) Retoma(ctx context.Context, id int64) (string, error) {
	at, err := LeAtualizacao(a.d.DB, id)
	if err != nil {
		return "", err
	}
	if at.Estado != Pausado && at.Estado != Falhou {
		return "", &ErroConflito{fmt.Sprintf("a atualização está %s; só dá para retomar pausada ou que parou com erro", at.Estado)}
	}
	reg, ok := riot.RegiaoPorCodigo(at.Regiao)
	if !ok {
		return "", &ErroConflito{"a região desta atualização não está mais disponível"}
	}
	if at.Fase == FaseLiga || at.Fase == FaseHistorico {
		if err := a.confere(ctx, reg, at.Fonte); err != nil {
			return "", err
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.ocupado(); err != nil {
		return "", err
	}
	return a.comecaTravado(id), nil
}

func (a *Atualizador) comecaTravado(id int64) string {
	ex := a.d.Hub.Nova()
	ctx, cancela := context.WithCancel(context.Background())
	c := &corrida{id: id, ex: ex, cancela: cancela, feito: make(chan struct{})}
	ex.AoCancelar(func() { a.Cancela(id) })
	a.atual = c
	a.execucoes[id] = ex.ID()
	mudaEstado(a.d.DB, id, Rodando, "")
	go a.corre(ctx, c)
	return ex.ID()
}

// Pausa: termina o candidato em andamento e para (retomável).
func (a *Atualizador) Pausa(id int64) error {
	a.mu.Lock()
	c := a.atual
	a.mu.Unlock()
	if c != nil && c.id == id {
		if !c.pausar.Swap(true) {
			a.d.DB.Exec(`UPDATE ranking_atualizacoes SET pausa_pedida = 1 WHERE id = ?`, id)
			c.ex.Emite("pausa_pedida", nil)
		}
		return nil
	}
	at, err := LeAtualizacao(a.d.DB, id)
	if err != nil {
		return err
	}
	switch at.Estado {
	case Pausado:
		return nil
	case Rodando, Falhou:
		return mudaEstado(a.d.DB, id, Pausado, at.Erro)
	}
	return &ErroConflito{fmt.Sprintf("a atualização está %s", at.Estado)}
}

// Cancela para já; o que já foi lido fica no cache.
func (a *Atualizador) Cancela(id int64) error {
	a.mu.Lock()
	c := a.atual
	a.mu.Unlock()
	if c != nil && c.id == id {
		c.cancela()
		return nil
	}
	at, err := LeAtualizacao(a.d.DB, id)
	if err != nil {
		return err
	}
	switch at.Estado {
	case Cancelado:
		return nil
	case Concluido:
		return &ErroConflito{"a atualização já terminou"}
	}
	return mudaEstado(a.d.DB, id, Cancelado, "")
}

// Espera bloqueia até a atualização em andamento parar (testes).
func (a *Atualizador) Espera() {
	a.mu.Lock()
	c := a.atual
	a.mu.Unlock()
	if c != nil {
		<-c.feito
	}
}

// Le devolve a atualização com o resumo e a execução, se houver.
func (a *Atualizador) Le(id int64) (Atualizacao, error) {
	at, err := LeAtualizacao(a.d.DB, id)
	if err != nil {
		return at, err
	}
	a.poeExecucao(&at)
	return at, nil
}

// Lista: as atualizações, da mais nova à mais velha.
func (a *Atualizador) Lista() ([]Atualizacao, error) {
	l, err := leAtualizacoes(a.d.DB, `ORDER BY id DESC LIMIT 50`)
	for i := range l {
		a.poeExecucao(&l[i])
	}
	return l, err
}

func (a *Atualizador) poeExecucao(at *Atualizacao) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.atual != nil && a.atual.id == at.ID {
		at.Execucao = &IDExec{ID: a.atual.ex.ID()}
	}
}

// ExecucaoDe: a última execução da atualização (SSE).
func (a *Atualizador) ExecucaoDe(id int64) (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	e, ok := a.execucoes[id]
	return e, ok
}

var errPausa = errors.New("pausa pedida")

func (a *Atualizador) corre(ctx context.Context, c *corrida) {
	defer func() {
		a.mu.Lock()
		if a.atual == c {
			a.atual = nil
		}
		a.mu.Unlock()
		c.cancela()
		close(c.feito)
	}()
	defer c.ex.Fecha()
	defer func() {
		if p := recover(); p != nil {
			log.Printf("ranking %d: pânico: %v\n%s", c.id, p, debug.Stack())
			a.trataParada(ctx, c, fmt.Errorf("erro interno na atualização: %v", p))
		}
	}()
	at, err := LeAtualizacao(a.d.DB, c.id)
	if err != nil {
		a.trataParada(ctx, c, err)
		return
	}
	reg, _ := riot.RegiaoPorCodigo(at.Regiao)
	if at.Fase == FaseLiga {
		c.ex.Emite("fase", map[string]string{"fase": FaseLiga})
		if err := a.leLiga(ctx, c, at, reg); err != nil {
			a.trataParada(ctx, c, err)
			return
		}
		at.Fase = FaseHistorico
	}
	if at.Fase == FaseHistorico {
		c.ex.Emite("fase", map[string]string{"fase": FaseHistorico})
		if err := a.leHistoricos(ctx, c, c.id, reg); err != nil {
			a.trataParada(ctx, c, err)
			return
		}
		mudaFase(a.d.DB, c.id, FaseCalculo)
	}
	c.ex.Emite("fase", map[string]string{"fase": FaseCalculo})
	n, err := a.calcula(ctx, c.id)
	if err != nil {
		a.trataParada(ctx, c, err)
		return
	}
	mudaFase(a.d.DB, c.id, FaseFim)
	mudaEstado(a.d.DB, c.id, Concluido, "")
	c.ex.Emite("concluido", map[string]any{"atualizacao_id": c.id, "rankings": n})
}

func (a *Atualizador) trataParada(ctx context.Context, c *corrida, err error) {
	var conflito *ErroConflito
	err = frasesRiot(err)
	switch {
	case ctx.Err() != nil:
		mudaEstado(a.d.DB, c.id, Cancelado, "")
		c.ex.Emite("cancelado", map[string]any{"atualizacao_id": c.id})
	case errors.Is(err, errPausa):
		mudaEstado(a.d.DB, c.id, Pausado, "")
		c.ex.Emite("pausado", map[string]any{"atualizacao_id": c.id})
	case errors.Is(err, lcu.ErrClienteFechado):
		mudaEstado(a.d.DB, c.id, Pausado, err.Error())
		c.ex.Emite("pausado", map[string]any{"atualizacao_id": c.id, "motivo": err.Error()})
	case errors.As(err, &conflito):
		mudaEstado(a.d.DB, c.id, Falhou, conflito.Msg)
		c.ex.Emite("erro", map[string]string{"texto": conflito.Msg})
	default:
		msg := err.Error()
		if _, frase, ok := lcu.StatusHTTP(err); ok {
			msg = frase
		}
		log.Printf("ranking %d: %v", c.id, err)
		mudaEstado(a.d.DB, c.id, Falhou, msg)
		c.ex.Emite("erro", map[string]string{"texto": msg})
	}
}

// --- 1. Liga ---------------------------------------------------------------------

func (a *Atualizador) leLiga(ctx context.Context, c *corrida, at Atualizacao, reg riot.Regiao) error {
	var todas []riot.EntradaLiga
	paginas := 0
	g := Gasto{}
	defer func() { somaGasto(a.d.DB, at.ID, g) }()
	apex := riot.EloApex(at.Elo)
	if apex {
		g.Liga++
		l, err := a.d.Riot.Liga(ctx, reg, at.Elo, "", 0)
		if err != nil {
			return err
		}
		todas, paginas = l, 1
	} else {
		for _, div := range riot.Divisoes {
			for pg := 1; pg <= PaginasPorDivisao; pg++ {
				if err := ctx.Err(); err != nil {
					return err
				}
				g.Liga++
				l, err := a.d.Riot.Liga(ctx, reg, at.Elo, div, pg)
				if err != nil {
					return err
				}
				if len(l) == 0 {
					break
				}
				paginas++
				for i := range l {
					if l[i].Rank == "" {
						l[i].Rank = div
					}
				}
				todas = append(todas, l...)
			}
		}
	}
	vistos := map[string]bool{}
	var ents []Entrada
	lpMax := 0
	for _, e := range todas {
		if e.Puuid == "" || vistos[e.Puuid] {
			continue
		}
		vistos[e.Puuid] = true
		div := e.Rank
		if apex {
			div = ""
		}
		ents = append(ents, Entrada{Puuid: e.Puuid, Divisao: div, LP: e.LP, Vitorias: e.Vitorias, Derrotas: e.Derrotas,
			Hot: e.HotStreak, Fresh: e.FreshBlood, Veteran: e.Veteran, Inativo: e.Inactive})
		lpMax = max(lpMax, e.LP)
	}
	cx := Contexto{Apex: apex, LPMax: lpMax}
	cs, inativos := EscolheCandidatos(ents, cx, at.TetoCandidatos)
	if err := gravaCandidatos(a.d.DB, at.ID, cs, paginas, len(ents), inativos, lpMax, apex); err != nil {
		return err
	}
	c.ex.Emite("liga", map[string]any{"paginas": paginas, "entradas": len(ents), "inativos": inativos, "candidatos": len(cs),
		"elo_inteiro": apex, "pedidos": g.Liga})
	return nil
}

// EscolheCandidatos: sem os inativos, os primeiros pela pré-nota "subindo" e
// pela "melhor", intercalados (um de cada lista, sem repetir), até o teto.
// Quem só tem a liga não sabe a rota: os candidatos servem às 5 rotas.
func EscolheCandidatos(ents []Entrada, cx Contexto, teto int) ([]candidato, int) {
	var ativos []Entrada
	inativos := 0
	for _, e := range ents {
		if e.Inativo {
			inativos++
			continue
		}
		ativos = append(ativos, e)
	}
	ordena := func(modo string) []Entrada {
		l := append([]Entrada(nil), ativos...)
		sort.SliceStable(l, func(a, b int) bool {
			pa, pb := PreNota(modo, l[a], cx), PreNota(modo, l[b], cx)
			if pa != pb {
				return pa > pb
			}
			if l[a].LP != l[b].LP {
				return l[a].LP > l[b].LP
			}
			return l[a].Puuid < l[b].Puuid
		})
		return l
	}
	listas := map[string][]Entrada{ModoSubindo: ordena(ModoSubindo), ModoMelhor: ordena(ModoMelhor)}
	pos := map[string]int{}
	ja := map[string]bool{}
	var cs []candidato
	for len(cs) < teto && len(cs) < len(ativos) {
		for _, modo := range Modos {
			l := listas[modo]
			for pos[modo] < len(l) && ja[l[pos[modo]].Puuid] {
				pos[modo]++
			}
			if pos[modo] >= len(l) || len(cs) >= teto {
				continue
			}
			e := l[pos[modo]]
			ja[e.Puuid] = true
			cs = append(cs, candidato{ordem: len(cs) + 1, puuid: e.Puuid, entrada: e, preOrdem: modo})
		}
	}
	return cs, inativos
}

// --- 2. Históricos ---------------------------------------------------------------

func (a *Atualizador) leHistoricos(ctx context.Context, c *corrida, id int64, reg riot.Regiao) error {
	at, err := LeAtualizacao(a.d.DB, id)
	if err != nil {
		return err
	}
	pend, err := leCandidatos(a.d.DB, id, `AND estado = 'pendente'`)
	if err != nil {
		return err
	}
	f := a.fonte(at.Fonte)
	for _, cand := range pend {
		if c.pausar.Load() {
			return errPausa
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		g := Gasto{}
		h, err := f.Le(ctx, reg, cand.puuid, at.Patch, at.TetoPartidas, &g)
		somaGasto(a.d.DB, id, g)
		ev := map[string]any{"ordem": cand.ordem, "total": at.Candidatos, "pre_ordem": cand.preOrdem, "pedidos": g}
		switch {
		case errors.Is(err, ErrNaoAchado):
			marcaCandidato(a.d.DB, id, cand.puuid, CandFora, ForaNaoAchado, h)
			ev["resultado"] = ForaNaoAchado
		case errors.Is(err, ErrSemHistorico):
			marcaCandidato(a.d.DB, id, cand.puuid, CandFora, ForaSemHistorico, h)
			ev["resultado"] = ForaSemHistorico
		case err != nil:
			return err
		default:
			if at.Patch == "" && h.PatchMaisNovo != "" {
				at.Patch = h.PatchMaisNovo
				mudaPatch(a.d.DB, id, at.Patch)
				c.ex.Emite("aviso", map[string]string{"texto": fmt.Sprintf("patch não informado: usando o %s (o da partida solo mais nova do primeiro candidato)", at.Patch)})
			}
			marcaCandidato(a.d.DB, id, cand.puuid, CandVisto, "", h)
			ev["resultado"], ev["riot_id"], ev["partidas"] = CandVisto, h.Nome+"#"+h.Tag, len(h.Partidas)
		}
		c.ex.Emite("candidato", ev)
	}
	return nil
}

// --- 3. Cálculo ------------------------------------------------------------------

func (a *Atualizador) nomeCampeao(ctx context.Context, id int) string {
	if a.d.Campeoes != nil {
		if n := a.d.Campeoes.Nome(ctx, id); n != "" {
			return n
		}
	}
	return fmt.Sprintf("campeão %d", id)
}

// calcula monta e grava os rankings da atualização; devolve quantos.
func (a *Atualizador) calcula(ctx context.Context, id int64) (int, error) {
	at, err := LeAtualizacao(a.d.DB, id)
	if err != nil {
		return 0, err
	}
	if at.Patch == "" {
		return 0, &ErroConflito{"nenhum candidato tinha ranqueada solo no histórico: não deu para saber o patch; informe o patch e atualize de novo"}
	}
	cs, err := leCandidatos(a.d.DB, id, "")
	if err != nil {
		return 0, err
	}
	var js []Jogador
	olhados := 0
	foraLista := map[string]int{}
	if at.Inativos > 0 {
		foraLista[ForaInativo] = at.Inativos
	}
	for _, c := range cs {
		switch c.estado {
		case CandVisto:
			olhados++
			js = append(js, Jogador{Entrada: c.entrada, Nome: c.nome, Tag: c.tag, Partidas: c.partidas})
		case CandFora:
			olhados++
			foraLista[c.motivo]++
		}
	}
	cx := Contexto{Apex: at.EloInteiro, LPMax: at.LPMax}
	monta := func(r Recorte, nome string) rankingCalculado {
		rc := rankingCalculado{recorte: r, posicoes: map[string][]Posicao{}, fora: map[string]int{}}
		for k, v := range foraLista {
			rc.fora[k] = v
		}
		for i, modo := range Modos {
			ps, fora := Calcula(modo, r, js, cx, nome)
			rc.posicoes[modo] = ps
			rc.amostra = len(ps)
			if i == 0 {
				for k, v := range fora {
					rc.fora[k] += v
				}
			}
		}
		return rc
	}
	var rs []rankingCalculado
	for _, rota := range Rotas {
		rs = append(rs, monta(Recorte{Tipo: TipoRota, Rota: rota}, NomeRota[rota]))
	}
	campeoes := map[int]bool{}
	for _, j := range js {
		for _, p := range j.Partidas {
			if p.CampeaoID > 0 {
				campeoes[p.CampeaoID] = true
			}
		}
	}
	var ids []int
	for c := range campeoes {
		ids = append(ids, c)
	}
	sort.Ints(ids)
	for _, cid := range ids {
		rc := monta(Recorte{Tipo: TipoCampeao, CampeaoID: cid}, a.nomeCampeao(ctx, cid))
		if rc.amostra >= MinJogadoresCampeao {
			rs = append(rs, rc)
		}
	}
	at.Candidatos = len(cs)
	if err := gravaRankings(a.d.DB, at, olhados, len(cs), rs); err != nil {
		return 0, err
	}
	return len(rs), nil
}

// DescricaoAmostra: o que foi olhado para o ranking, dito com as letras todas.
func DescricaoAmostra(r Ranking) string {
	elo := NomeElo[r.Elo]
	var s string
	if r.EloInteiro {
		s = fmt.Sprintf("a liga inteira de %s (%d jogadores, 1 pedido à league-v4)", elo, r.EntradasLidas)
	} else {
		s = fmt.Sprintf("AMOSTRA de %d páginas da league-v4 (até %d de cada divisão, I a IV; %d jogadores): não é o elo inteiro, a Riot não dá a lista de %s ordenada nem de uma vez",
			r.PaginasLidas, PaginasPorDivisao, r.EntradasLidas, elo)
	}
	s += fmt.Sprintf("; %d candidatos escolhidos pelas duas pré-ordens (teto %d), %d com o histórico olhado (até %d partidas do patch %s por jogador, fonte %s); %d entraram neste ranking",
		r.CandidatosLista, r.TetoCandidatos, r.CandidatosOlhados, r.TetoPartidas, r.Patch, r.Fonte, r.Amostra)
	return s
}
