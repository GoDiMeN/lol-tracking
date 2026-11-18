package aprendizado

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"

	"loltracking/server/analise"
	"loltracking/server/banco"
	"loltracking/server/lcu"
	"loltracking/server/partida"
	"loltracking/server/rastreio"
	"loltracking/server/replay"
	"loltracking/server/riot"
	"loltracking/server/web"
)

// FonteRiot é o que a coleta pede à API da Riot. De verdade: *riot.Cliente;
// no modo teste e nos testes: RiotFalso.
type FonteRiot interface {
	// Confere: nil, riot.ErrSemChave, riot.ErrChaveRecusada ou erro de rede.
	Confere(ctx context.Context) error
	Liga(ctx context.Context, reg riot.Regiao, elo, divisao string, pagina int) ([]riot.EntradaLiga, error)
	ContaPorPuuid(ctx context.Context, reg riot.Regiao, puuid string) (riot.Conta, error)
}

// Deps é o mundo de fora da coleta.
type Deps struct {
	DB      *banco.DB
	LCU     lcu.Cliente
	Riot    FonteRiot
	Fila    *rastreio.Fila
	Partida partida.Deps
	Hub     *web.Hub
	// Analise: para o estudo criado a partir de um jogador da coleta (o estudo
	// analisa cada partida que entra).
	Analise analise.Deps
}

// Coletor roda uma coleta por vez.
type Coletor struct {
	d Deps

	mu        sync.Mutex
	atual     *corridaColeta
	execucoes map[int64]string
}

type corridaColeta struct {
	coletaID int64
	ex       *web.Execucao
	cancela  context.CancelFunc
	feito    chan struct{}
	// pausar: pausa graciosa pedida; o que está em andamento termina.
	pausar atomic.Bool
	// rastreio: o rastreio rodando agora (0 = nenhum).
	rastreio atomic.Int64
	partida  atomic.Int64
}

// Novo cria o coletor. Coleta que estava "rodando" no banco vira "pausada".
func Novo(d Deps) *Coletor {
	if err := Recupera(d.DB); err != nil {
		log.Printf("aprendizado: recuperar coletas interrompidas: %v", err)
	}
	return &Coletor{d: d, execucoes: map[int64]string{}}
}

// Pedido é o corpo de criar coleta.
type Pedido struct {
	Regiao             string   `json:"regiao"`
	Elos               []string `json:"elos"`
	PartidasPorJogador int      `json:"partidas_por_jogador"`
	Quem               string   `json:"quem"`
	Velocidade         float64  `json:"velocidade"`
	// SoDescobrir: só acha os jogadores (com os números e a função de cada
	// um) e termina; nenhum replay é baixado nem aberto.
	SoDescobrir bool `json:"so_descobrir"`
	// Funcao (TOP, JUNGLE, MIDDLE, BOTTOM, UTILITY; vazio = qualquer): só entra quem tem essa
	// função principal (para achar "o caçador que mais sobe" sem esgotar a vaga com outros).
	Funcao string `json:"funcao"`
}

// valida completa os padrões e confere os limites.
func (p *Pedido) valida() (riot.Regiao, error) {
	if p.Regiao == "" {
		p.Regiao = riot.RegiaoPadrao
	}
	reg, ok := riot.RegiaoPorCodigo(p.Regiao)
	if !ok {
		return reg, &ErroPedido{fmt.Sprintf("por enquanto só a região BR (BR1); %q ainda não está disponível", p.Regiao)}
	}
	if len(p.Elos) == 0 {
		p.Elos = Elos
	}
	pedidos := map[string]bool{}
	for _, e := range p.Elos {
		if _, ok := NomeElo[e]; !ok {
			return reg, &ErroPedido{fmt.Sprintf("elo desconhecido: %q (use SILVER a CHALLENGER; nada abaixo de Prata)", e)}
		}
		if pedidos[e] {
			return reg, &ErroPedido{fmt.Sprintf("elo repetido: %s", e)}
		}
		pedidos[e] = true
	}
	// Sempre na ordem do menor para o maior elo.
	p.Elos = nil
	for _, e := range Elos {
		if pedidos[e] {
			p.Elos = append(p.Elos, e)
		}
	}
	if p.PartidasPorJogador == 0 {
		p.PartidasPorJogador = 1
	}
	if p.PartidasPorJogador < 1 || p.PartidasPorJogador > 3 {
		return reg, &ErroPedido{"partidas_por_jogador precisa ser de 1 a 3"}
	}
	if p.Quem == "" {
		p.Quem = QuemJogador
	}
	if p.Quem != QuemJogador && p.Quem != QuemTodos {
		return reg, &ErroPedido{`quem precisa ser "jogador" ou "todos"`}
	}
	p.Funcao = strings.ToUpper(strings.TrimSpace(p.Funcao))
	if p.Funcao != "" && nomeFuncao[p.Funcao] == "" {
		return reg, &ErroPedido{"funcao precisa ser TOP, JUNGLE, MIDDLE, BOTTOM ou UTILITY (ou vazia)"}
	}
	if p.Velocidade == 0 {
		p.Velocidade = 4
	}
	if p.Velocidade < 0.5 || p.Velocidade > 8 {
		return reg, &ErroPedido{"velocidade precisa estar entre 0,5 e 8"}
	}
	return reg, nil
}

// frasesRiot traduz o erro da chave para a pessoa.
func frasesRiot(err error) error {
	switch {
	case errors.Is(err, riot.ErrSemChave):
		return &ErroConflito{"sem chave da API da Riot: a coleta precisa dela para achar os melhores de cada elo. Gere uma chave em developer.riotgames.com e coloque em token.txt"}
	case errors.Is(err, riot.ErrChaveRecusada):
		return &ErroConflito{"a Riot recusou a chave (a de desenvolvimento vence em 24 h): gere outra em developer.riotgames.com, troque o token.txt e retome"}
	}
	return err
}

// confereAntes: a chave (se ainda há elo para descobrir) e a região do cliente.
func (k *Coletor) confereAntes(ctx context.Context, reg riot.Regiao, precisaRiot bool) error {
	if precisaRiot {
		if err := k.d.Riot.Confere(ctx); err != nil {
			if errors.Is(err, riot.ErrSemChave) || errors.Is(err, riot.ErrChaveRecusada) {
				return frasesRiot(err)
			}
			return &ErroConflito{"não deu para conferir a chave da Riot: " + err.Error()}
		}
	}
	return k.confereRegiao(ctx, reg)
}

// confereRegiao: o replay só abre para partidas da plataforma da conta logada.
func (k *Coletor) confereRegiao(ctx context.Context, reg riot.Regiao) error {
	r, err := k.d.LCU.Regiao(ctx)
	if err != nil {
		return err
	}
	if r.Regiao != reg.RegiaoCliente {
		return &ErroConflito{fmt.Sprintf("a conta logada no cliente do LoL é da região %q, e a coleta é de %s: o replay só abre para partidas da região da conta. Entre com uma conta de %s",
			r.Regiao, reg.Nome, reg.Nome)}
	}
	return nil
}

func (k *Coletor) ocupado() error {
	if k.atual != nil {
		if c, err := LeColeta(k.d.DB, k.atual.coletaID); err == nil && c.EstudoID > 0 {
			return &ErroConflito{fmt.Sprintf("o rastreio das partidas do estudo %d está rodando; pause ou cancele antes", c.EstudoID)}
		}
		return &ErroConflito{fmt.Sprintf("já há uma coleta rodando (coleta %d); pause ou cancele antes", k.atual.coletaID)}
	}
	if rid, _ := k.d.Fila.Atual(); rid != 0 {
		return &ErroConflito{fmt.Sprintf("há um rastreio rodando (nº %d); espere ele terminar ou pause-o na Fila de rastreio", rid)}
	}
	return nil
}

// Cria valida, confere a chave e a região, grava e começa.
func (k *Coletor) Cria(ctx context.Context, p Pedido) (Coleta, string, error) {
	reg, err := p.valida()
	if err != nil {
		return Coleta{}, "", err
	}
	if err := k.confereAntes(ctx, reg, true); err != nil {
		return Coleta{}, "", err
	}
	k.mu.Lock()
	if err := k.ocupado(); err != nil {
		k.mu.Unlock()
		return Coleta{}, "", err
	}
	id, err := criaColeta(k.d.DB, novaColeta{regiao: reg.Codigo, elos: p.Elos, ppj: p.PartidasPorJogador, quem: p.Quem,
		vel: p.Velocidade, soDescobrir: p.SoDescobrir, funcao: p.Funcao})
	if err != nil {
		k.mu.Unlock()
		return Coleta{}, "", err
	}
	exID := k.comecaTravado(id)
	k.mu.Unlock()
	// Le trava k.mu de novo (a partida e o rastreio atuais): fora da trava.
	c, err := k.Le(id)
	return c, exID, err
}

// Retoma continua uma coleta pausada (ou que falhou) de onde parou.
func (k *Coletor) Retoma(ctx context.Context, id int64) (string, error) {
	col, err := LeColeta(k.d.DB, id)
	if err != nil {
		return "", err
	}
	if col.Estado != Pausado && col.Estado != Falhou {
		return "", &ErroConflito{fmt.Sprintf("a coleta está %s; só dá para retomar pausada ou que parou com erro", col.Estado)}
	}
	if col.EstudoID > 0 {
		// O rastreio de um estudo: sem descoberta (sem Riot) e as partidas vieram
		// do próprio cliente, então a região não se confere; o cliente, sim.
		if _, err := k.d.LCU.FaseGameflow(ctx); err != nil {
			return "", err
		}
		k.mu.Lock()
		defer k.mu.Unlock()
		if err := k.ocupado(); err != nil {
			return "", err
		}
		return k.comecaTravado(id), nil
	}
	reg, ok := riot.RegiaoPorCodigo(col.Regiao)
	if !ok {
		return "", &ErroConflito{"a região desta coleta não está mais disponível"}
	}
	elos, err := leElos(k.d.DB, id)
	if err != nil {
		return "", err
	}
	falta := false
	for _, e := range elos {
		falta = falta || e.Estado != EloPronto
	}
	if err := k.confereAntes(ctx, reg, falta); err != nil {
		return "", err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if err := k.ocupado(); err != nil {
		return "", err
	}
	return k.comecaTravado(id), nil
}

func (k *Coletor) comecaTravado(id int64) string {
	ex := k.d.Hub.Nova()
	ctx, cancela := context.WithCancel(context.Background())
	c := &corridaColeta{coletaID: id, ex: ex, cancela: cancela, feito: make(chan struct{})}
	ex.AoCancelar(func() { k.Cancela(id) })
	k.atual = c
	k.execucoes[id] = ex.ID()
	mudaEstado(k.d.DB, id, Rodando, "")
	go k.corre(ctx, c)
	return ex.ID()
}

// Pausa pede a pausa graciosa: termina o que está em andamento e para.
func (k *Coletor) Pausa(id int64) error {
	k.mu.Lock()
	c := k.atual
	k.mu.Unlock()
	if c != nil && c.coletaID == id {
		if !c.pausar.Swap(true) {
			marcaPausaPedida(k.d.DB, id)
			c.ex.Emite("pausa_pedida", nil)
		}
		if rid := c.rastreio.Load(); rid != 0 {
			if err := k.d.Fila.PausaAoFimDoPasse(rid); err != nil {
				log.Printf("aprendizado: pausar o rastreio %d ao fim do passe: %v", rid, err)
			}
		}
		return nil
	}
	col, err := LeColeta(k.d.DB, id)
	if err != nil {
		return err
	}
	switch col.Estado {
	case Pausado:
		return nil
	case Rodando, Falhou:
		return mudaEstado(k.d.DB, id, Pausado, col.Erro)
	}
	return &ErroConflito{fmt.Sprintf("a coleta está %s", col.Estado)}
}

// Cancela para já. O rastreio em andamento é cancelado pelo pacote rastreio
// (o passe começado é fechado como cancelado e o replay fecha).
func (k *Coletor) Cancela(id int64) error {
	k.mu.Lock()
	c := k.atual
	k.mu.Unlock()
	if c != nil && c.coletaID == id {
		c.cancela()
		if rid := c.rastreio.Load(); rid != 0 {
			if err := k.d.Fila.Cancela(rid); err != nil {
				log.Printf("aprendizado: cancelar o rastreio %d: %v", rid, err)
			}
		}
		return nil
	}
	col, err := LeColeta(k.d.DB, id)
	if err != nil {
		return err
	}
	switch col.Estado {
	case Cancelado:
		return nil
	case Concluido:
		return &ErroConflito{"a coleta já terminou"}
	}
	k.encerraPartidas(id)
	return mudaEstado(k.d.DB, id, Cancelado, "")
}

// encerraPartidas: o que não terminou vira cancelada; rastreio pausado dela
// é cancelado também (fecha os passes começados).
func (k *Coletor) encerraPartidas(coletaID int64) {
	ps, err := lePartidas(k.d.DB, `WHERE p.coleta_id = ? AND p.estado IN ('pendente','baixando','rastreando','pausada')`, coletaID)
	if err != nil {
		log.Printf("aprendizado: encerrar partidas da coleta %d: %v", coletaID, err)
		return
	}
	for _, p := range ps {
		if p.RastreioID != nil {
			if r, err := rastreio.LeRastreio(k.d.DB, *p.RastreioID); err == nil && r.Estado != rastreio.Concluido && r.Estado != rastreio.Cancelado {
				k.d.Fila.Cancela(r.ID)
			}
		}
		mudaPartida(k.d.DB, p.ID, PartidaCancelada, "")
	}
}

// Espera bloqueia até a coleta em andamento parar (testes).
func (k *Coletor) Espera() {
	k.mu.Lock()
	c := k.atual
	k.mu.Unlock()
	if c != nil {
		<-c.feito
	}
}

// ExecucaoDe é a última execução da coleta (para o SSE).
func (k *Coletor) ExecucaoDe(id int64) (string, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	e, ok := k.execucoes[id]
	return e, ok
}

// erroParada: a coleta pausa (retomável) com esta frase: partida ao vivo,
// outro rastreio rodando, o rastreio parou sozinho.
type erroParada struct{ msg string }

func (e *erroParada) Error() string { return e.msg }

// corre é a execução da coleta.
func (k *Coletor) corre(ctx context.Context, c *corridaColeta) {
	id, ex := c.coletaID, c.ex
	defer func() {
		k.mu.Lock()
		if k.atual == c {
			k.atual = nil
		}
		k.mu.Unlock()
		c.cancela()
		close(c.feito)
	}()
	defer ex.Fecha()
	defer func() {
		if p := recover(); p != nil {
			log.Printf("aprendizado %d: pânico: %v\n%s", id, p, debug.Stack())
			k.trataParada(ctx, c, fmt.Errorf("erro interno na coleta: %v", p))
		}
	}()
	col, err := LeColeta(k.d.DB, id)
	if err != nil {
		k.trataParada(ctx, c, err)
		return
	}
	reg, _ := riot.RegiaoPorCodigo(col.Regiao)
	if col.EstudoID == 0 {
		if err := k.confereRegiao(ctx, reg); err != nil {
			k.trataParada(ctx, c, err)
			return
		}
	}
	if col.Fase == FaseDescoberta {
		ex.Emite("fase", map[string]string{"fase": FaseDescoberta})
		if err := k.descobre(ctx, c, col, reg); err != nil {
			k.trataParada(ctx, c, err)
			return
		}
		if col.SoDescobrir {
			// Só descobrir: os jogadores estão escolhidos, nada a rastrear.
			mudaFase(k.d.DB, id, FaseFim)
			mudaEstado(k.d.DB, id, Concluido, "")
			ex.Emite("concluido", map[string]any{"coleta_id": id})
			return
		}
		mudaFase(k.d.DB, id, FaseRastreio)
	}
	ex.Emite("fase", map[string]string{"fase": FaseRastreio})
	for {
		if c.pausar.Load() {
			k.trataParada(ctx, c, errPausa)
			return
		}
		if err := ctx.Err(); err != nil {
			k.trataParada(ctx, c, err)
			return
		}
		ps, err := lePartidas(k.d.DB, `WHERE p.coleta_id = ? AND p.estado IN ('pendente','baixando','rastreando','pausada')`+
			ordemDasPartidas+` LIMIT 1`, id)
		if err != nil {
			k.trataParada(ctx, c, err)
			return
		}
		if len(ps) == 0 {
			break
		}
		if err := k.rodaPartida(ctx, c, col, ps[0]); err != nil {
			k.trataParada(ctx, c, err)
			return
		}
	}
	mudaFase(k.d.DB, id, FaseFim)
	mudaEstado(k.d.DB, id, Concluido, "")
	ex.Emite("concluido", map[string]any{"coleta_id": id})
}

// descobre roda a descoberta dos elos que faltam.
func (k *Coletor) descobre(ctx context.Context, c *corridaColeta, col Coleta, reg riot.Regiao) error {
	cfg, err := k.d.LCU.ConfiguracaoReplays(ctx)
	if err != nil {
		return err
	}
	patch := lcu.PatchDe(cfg.VersaoJogo)
	elos, err := leElos(k.d.DB, col.ID)
	if err != nil {
		return err
	}
	for _, e := range elos {
		if e.Estado == EloPronto {
			continue
		}
		if err := k.descobreElo(ctx, c, col, reg, e, patch); err != nil {
			return err
		}
	}
	return nil
}

// trataParada decide o que um erro significa para a coleta.
func (k *Coletor) trataParada(ctx context.Context, c *corridaColeta, err error) {
	id, ex := c.coletaID, c.ex
	var parada *erroParada
	var conflito *ErroConflito
	err = frasesRiot(err)
	switch {
	case ctx.Err() != nil:
		k.encerraPartidas(id)
		mudaEstado(k.d.DB, id, Cancelado, "")
	case errors.Is(err, errPausa):
		mudaEstado(k.d.DB, id, Pausado, "")
		ex.Emite("pausado", map[string]any{"coleta_id": id})
	case errors.Is(err, lcu.ErrClienteFechado):
		mudaEstado(k.d.DB, id, Pausado, err.Error())
		ex.Emite("aviso", map[string]string{"texto": err.Error()})
		ex.Emite("pausado", map[string]any{"coleta_id": id, "motivo": err.Error()})
	case errors.As(err, &parada):
		mudaEstado(k.d.DB, id, Pausado, parada.msg)
		ex.Emite("aviso", map[string]string{"texto": parada.msg})
		ex.Emite("pausado", map[string]any{"coleta_id": id, "motivo": parada.msg})
	case errors.As(err, &conflito):
		mudaEstado(k.d.DB, id, Falhou, conflito.Msg)
		ex.Emite("erro", map[string]string{"texto": conflito.Msg})
	default:
		msg := err.Error()
		if _, frase, ok := lcu.StatusHTTP(err); ok {
			msg = frase
		}
		log.Printf("aprendizado %d: %v", id, err)
		mudaEstado(k.d.DB, id, Falhou, msg)
		ex.Emite("erro", map[string]string{"texto": msg})
	}
}

// rodaPartida baixa (se preciso) e rastreia uma partida. Erro só quando a
// coleta precisa parar; problema da partida fica nela e a coleta segue.
func (k *Coletor) rodaPartida(ctx context.Context, c *corridaColeta, col Coleta, p PartidaDaColeta) error {
	ex := c.ex
	c.partida.Store(p.GameID)
	defer c.partida.Store(0)
	falhaPartida := func(msg string) error {
		mudaPartida(k.d.DB, p.ID, PartidaErro, msg)
		ex.Emite("partida_fim", map[string]any{"partida_id": p.ID, "game_id": p.GameID, "estado": PartidaErro, "erro": msg})
		return nil
	}
	ex.Emite("partida_inicio", map[string]any{"partida_id": p.ID, "game_id": p.GameID, "elo": p.Elo, "campeao": p.Campeao})

	// Regra 2: nada com partida ao vivo (o rastreio confere de novo).
	fase, err := k.d.LCU.FaseGameflow(ctx)
	if err != nil {
		return err
	}
	if !replay.FasePermitida(fase) {
		return &erroParada{(&replay.ErroFase{Fase: fase}).Error()}
	}

	var rid int64
	if p.RastreioID != nil {
		// Retomada: o rastreio desta partida já existe.
		r, err := rastreio.LeRastreio(k.d.DB, *p.RastreioID)
		if err != nil {
			return falhaPartida("o rastreio desta partida sumiu do banco")
		}
		switch r.Estado {
		case rastreio.Concluido:
			mudaPartida(k.d.DB, p.ID, PartidaConcluida, "")
			return nil
		case rastreio.Cancelado:
			mudaPartida(k.d.DB, p.ID, PartidaCancelada, "o rastreio foi cancelado fora da coleta")
			return nil
		case rastreio.Pausado, rastreio.Falhou:
			if _, err := k.d.Fila.Retoma(r.ID); err != nil {
				return k.erroDoRastreio(ctx, err)
			}
		}
		rid = r.ID
		mudaPartida(k.d.DB, p.ID, PartidaRastreando, "")
	} else {
		if _, err := partida.LePartida(ctx, k.d.Partida, p.GameID); err != nil {
			if errors.Is(err, lcu.ErrClienteFechado) {
				return err
			}
			return falhaPartida("não deu para ler a partida no cliente do LoL")
		}
		cfg, err := k.d.LCU.ConfiguracaoReplays(ctx)
		if err != nil {
			return err
		}
		rep, err := partida.ConsultaReplay(ctx, k.d.Partida, p.GameID, cfg.VersaoJogo)
		if err != nil {
			if errors.Is(err, lcu.ErrClienteFechado) || ctx.Err() != nil {
				return err
			}
			return falhaPartida("não deu para consultar o replay")
		}
		partida.GravaReplay(k.d.DB, p.GameID, rep)
		switch rep.Estado {
		case partida.Incompativel, partida.Indisponivel:
			return falhaPartida("replay indisponível: " + rep.Detalhe)
		case partida.Baixavel, partida.Baixando:
			mudaPartida(k.d.DB, p.ID, PartidaBaixando, "")
			if msg, err := k.baixa(ctx, c, p.GameID); err != nil {
				return err
			} else if msg != "" {
				return falhaPartida(msg)
			}
		}
		// Pausa pedida durante o download: o download terminou, para aqui.
		if c.pausar.Load() {
			mudaPartida(k.d.DB, p.ID, PartidaPendente, "")
			return errPausa
		}
		var jogadores []int
		if col.Quem == QuemJogador {
			jogadores = []int{p.ParticipanteID}
		}
		r, exID, err := k.d.Fila.Cria(ctx, p.GameID, rastreio.Pedido{Jogadores: jogadores, Velocidade: col.Velocidade})
		if err != nil {
			mudaPartida(k.d.DB, p.ID, PartidaPendente, "")
			var inv *rastreio.ErroInvalido
			if errors.As(err, &inv) {
				return falhaPartida(inv.Msg)
			}
			return k.erroDoRastreio(ctx, err)
		}
		rid = r.ID
		ligaRastreio(k.d.DB, p.ID, rid)
		ex.Emite("rastreio", map[string]any{"partida_id": p.ID, "rastreio_id": rid, "execucao": map[string]string{"id": exID}})
	}

	c.rastreio.Store(rid)
	if c.pausar.Load() {
		k.d.Fila.PausaAoFimDoPasse(rid)
	}
	if ctx.Err() != nil {
		k.d.Fila.Cancela(rid)
	}
	k.d.Fila.Espera()
	c.rastreio.Store(0)

	r, err := rastreio.LeRastreio(k.d.DB, rid)
	if err != nil {
		return err
	}
	switch r.Estado {
	case rastreio.Concluido:
		mudaPartida(k.d.DB, p.ID, PartidaConcluida, "")
		ex.Emite("partida_fim", map[string]any{"partida_id": p.ID, "game_id": p.GameID, "estado": PartidaConcluida})
		return nil
	case rastreio.Pausado:
		mudaPartida(k.d.DB, p.ID, PartidaPausada, "")
		if c.pausar.Load() {
			return errPausa
		}
		msg := r.Erro
		if msg == "" {
			msg = "o rastreio da partida foi pausado fora da coleta"
		}
		return &erroParada{msg}
	case rastreio.Cancelado:
		if ctx.Err() != nil {
			return ctx.Err()
		}
		mudaPartida(k.d.DB, p.ID, PartidaCancelada, "o rastreio foi cancelado fora da coleta")
		ex.Emite("partida_fim", map[string]any{"partida_id": p.ID, "game_id": p.GameID, "estado": PartidaCancelada})
		return nil
	case rastreio.Falhou:
		return falhaPartida(r.Erro)
	}
	return &erroParada{fmt.Sprintf("o rastreio da partida ficou %s", r.Estado)}
}

// erroDoRastreio traduz a recusa da fila de rastreio para a coleta.
func (k *Coletor) erroDoRastreio(ctx context.Context, err error) error {
	var ocu *rastreio.ErroOcupado
	var est *rastreio.ErroEstado
	switch {
	case errors.As(err, &ocu):
		return &erroParada{fmt.Sprintf("há outro rastreio rodando (nº %d): a coleta pausou; retome quando ele terminar", ocu.RastreioID)}
	case errors.As(err, &est):
		return &erroParada{est.Msg}
	}
	return err
}

// baixa o replay pelo mesmo caminho do botão "Baixar" (partida.Baixa). msg
// != "" = o download falhou (fica na partida); err = a coleta para.
func (k *Coletor) baixa(ctx context.Context, c *corridaColeta, gameID int64) (string, error) {
	sub := k.d.Hub.Nova()
	c.ex.Emite("download", map[string]any{"game_id": gameID, "estado": "baixando", "execucao": map[string]string{"id": sub.ID()}})
	partida.Baixa(ctx, k.d.Partida, sub, gameID)
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	msg := "o download do replay não terminou"
	for _, ev := range sub.Eventos() {
		switch ev.Tipo {
		case "concluido":
			c.ex.Emite("download", map[string]any{"game_id": gameID, "estado": "baixado"})
			return "", nil
		case "erro":
			msg = textoDoEvento(ev.Dados)
		}
	}
	if fechado := msg == lcu.ErrClienteFechado.Error(); fechado {
		return "", lcu.ErrClienteFechado
	}
	c.ex.Emite("download", map[string]any{"game_id": gameID, "estado": "falhou", "erro": msg})
	return msg, nil
}
