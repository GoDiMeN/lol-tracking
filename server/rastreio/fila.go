package rastreio

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"runtime/debug"
	"sort"
	"sync"
	"sync/atomic"

	"loltracking/server/banco"
	"loltracking/server/lcu"
	"loltracking/server/partida"
	"loltracking/server/replay"
	"loltracking/server/web"
)

// Deps é o mundo de fora do rastreio.
type Deps struct {
	DB     *banco.DB
	LCU    lcu.Cliente
	Replay replay.Deps
	Hub    *web.Hub
	Config Config
	// AoFecharPasse é chamado depois que um passe da partida fecha (concluído
	// ou cancelado com amostras): a análise refaz o que depende dele. Roda na
	// goroutine do rastreio; quem pendura decide se faz em segundo plano.
	AoFecharPasse func(gameID int64)
	// SemEstudo: o interruptor LOL_ESTUDO desligado. A lista de partidas
	// rastreadas não mostra nem admite partida só de estudo (origem.go).
	SemEstudo bool
}

// Fila roda um rastreio por vez. Um rastreio = até 10 passes em sequência,
// todos na mesma abertura do replay.
type Fila struct {
	d Deps

	mu        sync.Mutex
	atual     *corrida
	execucoes map[int64]string
	// repescagens: partida → última execução de repescagem (para a tela da
	// partida reencontrar o progresso).
	repescagens map[int64]string
	// visoes: partida → última execução dos passes de visão avulsos.
	visoes map[int64]string
	// esperando: o motor está esperando a pessoa parar de usar o computador
	// (uso.go); nil = não. ultima: a última corrida que parou pausada (captura.go).
	esperando atomic.Pointer[EsperaUsuario]
	ultima    *Parada
}

type corrida struct {
	rastreioID int64
	ex         *web.Execucao
	cancela    context.CancelCauseFunc
	feito      chan struct{}
	// pausarNoFim: pausa pedida "ao fim do passe" (a coleta do aprendizado
	// usa): o passe em andamento termina e fecha normalmente, e o rastreio
	// para antes do próximo.
	pausarNoFim atomic.Bool
	// repescagemDe: a partida, quando a corrida é a repescagem de passes já
	// gravados (Repesca) e não um rastreio. Ocupa a fila do mesmo jeito (um
	// replay aberto por vez); rastreioID fica com o rastreio do primeiro passe.
	repescagemDe int64
	// visaoDe: a partida, quando a corrida são os passes de visão avulsos
	// (RastreiaVisao, visao_fila.go).
	visaoDe int64
	// pedidoRepescagem e pedidoVisao: o pedido da corrida, para "retomar" a
	// repescagem ou a visão pausadas pela captura (captura.go).
	pedidoRepescagem PedidoRepescagem
	pedidoVisao      PedidoVisao
	// pausando: PausaAgora pediu a pausa (captura.go).
	pausando atomic.Bool
}

// deRastreio diz se a corrida é o rastreio id (e não repescagem nem visão avulsa).
func (c *corrida) deRastreio(id int64) bool {
	return c != nil && c.repescagemDe == 0 && c.visaoDe == 0 && c.rastreioID == id
}

// NovaFila cria a fila. Rastreio que estava "rodando" no banco (o processo
// morreu no meio) vira "pausado", e o jogo que tenha ficado aberto é fechado.
func NovaFila(d Deps) *Fila {
	d.Config = d.Config.comPadroes()
	if d.Replay.Relogio == nil {
		d.Replay.Relogio = replay.RelogioReal{}
	}
	f := &Fila{d: d, execucoes: map[int64]string{}, repescagens: map[int64]string{}, visoes: map[int64]string{}}
	if n, err := Recupera(d.DB); err != nil {
		log.Printf("rastreio: recuperar rastreios interrompidos: %v", err)
	} else if n > 0 && d.Replay.Jogo != nil && d.Replay.Jogo.Aberto() {
		log.Printf("rastreio: %d rastreio(s) interrompido(s); fechando o replay que ficou aberto", n)
		go replay.FechaJogo(d.Replay)
	}
	return f
}

// Pedido é o corpo de criar rastreio. TInicioMs/TFimMs: o trecho da partida
// (de_ms/ate_ms na rota; TFimMs 0 = até o fim).
type Pedido struct {
	Jogadores  []int
	Velocidade float64
	TInicioMs  int64
	TFimMs     int64
	// Visao: depois dos passes dos jogadores, os dois passes de visão de time
	// (azul e vermelho), na mesma abertura do replay.
	Visao bool
}

// ErroOcupado: já há um rastreio rodando (409).
type ErroOcupado struct {
	RastreioID int64
	Execucao   string
	// RepescagemDe: a partida, se o que está rodando é uma repescagem.
	RepescagemDe int64
	// VisaoDe: a partida, se o que está rodando são os passes de visão avulsos.
	VisaoDe int64
}

func (e *ErroOcupado) Error() string {
	if e.RepescagemDe != 0 {
		return fmt.Sprintf("já há uma repescagem rodando (partida %d); espere ela terminar ou cancele", e.RepescagemDe)
	}
	if e.VisaoDe != 0 {
		return fmt.Sprintf("já há um rastreio da visão dos times rodando (partida %d); espere ele terminar ou cancele", e.VisaoDe)
	}
	return fmt.Sprintf("já há um rastreio rodando (rastreio %d); pause ou cancele antes", e.RastreioID)
}

// ocupadoTravado devolve o ErroOcupado da corrida atual (f.mu travado).
func (f *Fila) ocupadoTravado() error {
	return &ErroOcupado{RastreioID: f.atual.rastreioID, Execucao: f.atual.ex.ID(), RepescagemDe: f.atual.repescagemDe, VisaoDe: f.atual.visaoDe}
}

// ErroEstado: a ação não cabe no estado atual (409).
type ErroEstado struct{ Msg string }

func (e *ErroEstado) Error() string { return e.Msg }

// ErroInvalido: pedido malformado (400).
type ErroInvalido struct{ Msg string }

func (e *ErroInvalido) Error() string { return e.Msg }

// Atual devolve o rastreio rodando e a execução dele (0, "" se nenhum).
func (f *Fila) Atual() (int64, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.atual == nil {
		return 0, ""
	}
	return f.atual.rastreioID, f.atual.ex.ID()
}

// ExecucaoDe é a última execução do rastreio (para o SSE).
func (f *Fila) ExecucaoDe(id int64) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.execucoes[id]
	return e, ok
}

// Espera bloqueia até o rastreio em andamento terminar (testes e sonda).
func (f *Fila) Espera() {
	f.mu.Lock()
	c := f.atual
	f.mu.Unlock()
	if c != nil {
		<-c.feito
	}
}

// garanteLinha devolve a linha do tempo guardada, buscando no LCU se faltar.
func (f *Fila) garanteLinha(ctx context.Context, gameID int64) (LinhaDoTempo, error) {
	bruto, err := partida.LeLinhaDoTempo(f.d.DB, gameID)
	if errors.Is(err, partida.ErrNaoEncontrada) {
		bruto, err = f.d.LCU.LinhaDoTempo(ctx, gameID)
		if err == nil {
			err = partida.GravaLinhaDoTempo(f.d.DB, gameID, bruto)
		}
	}
	if err != nil {
		return LinhaDoTempo{}, err
	}
	return LeLinhaDoTempo(bruto)
}

// Cria valida o pedido, grava o rastreio e começa a rodar.
func (f *Fila) Cria(ctx context.Context, gameID int64, ped Pedido) (Rastreio, string, error) {
	p, err := partida.Le(f.d.DB, gameID)
	if err != nil {
		return Rastreio{}, "", err
	}
	if !p.Completa || len(p.Participantes) == 0 {
		return Rastreio{}, "", &ErroInvalido{"a partida ainda não tem os 10 participantes; abra a partida antes"}
	}
	ids := map[int]bool{}
	for _, pt := range p.Participantes {
		ids[pt.ParticipanteID] = true
	}
	jogadores := ped.Jogadores
	if len(jogadores) == 0 {
		for id := range ids {
			jogadores = append(jogadores, id)
		}
		sort.Ints(jogadores)
	}
	vistos := map[int]bool{}
	for _, j := range jogadores {
		if !ids[j] {
			return Rastreio{}, "", &ErroInvalido{fmt.Sprintf("participante %d não existe nesta partida", j)}
		}
		if vistos[j] {
			return Rastreio{}, "", &ErroInvalido{fmt.Sprintf("participante %d repetido", j)}
		}
		vistos[j] = true
	}
	vel := ped.Velocidade
	if vel == 0 {
		vel = f.d.Config.Velocidade
	}
	if vel < 0.5 || vel > 8 {
		return Rastreio{}, "", &ErroInvalido{"velocidade precisa estar entre 0,5 e 8"}
	}
	if ped.Visao && f.d.Config.Minimapa == nil {
		return Rastreio{}, "", &ErroEstado{ErrSemLeitorMinimapa.Error()}
	}
	fase, err := f.d.LCU.FaseGameflow(ctx)
	if err != nil {
		return Rastreio{}, "", err
	}
	if !replay.FasePermitida(fase) {
		return Rastreio{}, "", &ErroEstado{(&replay.ErroFase{Fase: fase}).Error()}
	}
	if m, err := f.d.LCU.MetadataReplay(ctx, gameID); err != nil || m.Estado != "watch" {
		return Rastreio{}, "", &ErroEstado{"o replay desta partida não está baixado; baixe antes de rastrear"}
	}
	lt, err := f.garanteLinha(ctx, gameID)
	if err != nil {
		log.Printf("rastreio: linha do tempo de %d: %v (segue com a calibração padrão)", gameID, err)
	} else {
		nomes := map[int]string{}
		for _, pt := range p.Participantes {
			nomes[pt.ParticipanteID] = pt.Nome
		}
		if err := ImportaLinhaDoTempo(f.d.DB, gameID, lt, nomes); err != nil {
			return Rastreio{}, "", err
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.atual != nil {
		return Rastreio{}, "", f.ocupadoTravado()
	}
	id, err := criaRastreio(f.d.DB, gameID, vel, ped.TInicioMs, ped.TFimMs, jogadores)
	if err != nil {
		return Rastreio{}, "", err
	}
	if ped.Visao {
		if err := criaPassesVisao(f.d.DB, gameID, id, []int{100, 200}, f.d.Config.VelocidadeVisao, ped.TInicioMs, ped.TFimMs); err != nil {
			return Rastreio{}, "", err
		}
	}
	exID := f.comecaTravado(id)
	r, err := LeRastreio(f.d.DB, id)
	return r, exID, err
}

// comecaTravado inicia a corrida (f.mu já travado).
func (f *Fila) comecaTravado(id int64) string {
	ex := f.d.Hub.Nova()
	ctx, cancela := context.WithCancelCause(context.Background())
	ex.AoCancelar(func() { cancela(ErrCancelado) })
	c := &corrida{rastreioID: id, ex: ex, cancela: cancela, feito: make(chan struct{})}
	f.atual = c
	f.execucoes[id] = ex.ID()
	mudaEstadoRastreio(f.d.DB, id, Rodando, "")
	go f.corre(ctx, c)
	return ex.ID()
}

// Retoma continua um rastreio pausado (ou que falhou) do último t gravado.
func (f *Fila) Retoma(id int64) (string, error) {
	r, err := LeRastreio(f.d.DB, id)
	if err != nil {
		return "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.atual != nil {
		return "", f.ocupadoTravado()
	}
	if r.Estado != Pausado && r.Estado != Falhou {
		return "", &ErroEstado{fmt.Sprintf("o rastreio está %s; só dá para retomar pausado ou que falhou", r.Estado)}
	}
	return f.comecaTravado(id), nil
}

// Pausa para o rastreio e fecha o replay; o progresso fica para retomar.
func (f *Fila) Pausa(id int64) error {
	f.mu.Lock()
	if f.atual.deRastreio(id) {
		c := f.atual
		f.mu.Unlock()
		c.cancela(ErrPausado)
		return nil
	}
	f.mu.Unlock()
	r, err := LeRastreio(f.d.DB, id)
	if err != nil {
		return err
	}
	switch r.Estado {
	case Pausado:
		return nil
	case Rodando:
		return mudaEstadoRastreio(f.d.DB, id, Pausado, "")
	}
	return &ErroEstado{fmt.Sprintf("o rastreio está %s", r.Estado)}
}

// PausaAoFimDoPasse é a pausa graciosa: o passe em andamento termina
// normalmente (compactação, cadeia, análise), o replay fecha e o rastreio fica
// pausado antes do próximo passe. Se o passe era o último, o rastreio conclui.
// Com o rastreio parado, vale o mesmo que Pausa.
func (f *Fila) PausaAoFimDoPasse(id int64) error {
	f.mu.Lock()
	if f.atual.deRastreio(id) {
		c := f.atual
		f.mu.Unlock()
		if !c.pausarNoFim.Swap(true) {
			c.ex.Emite("pausa_pedida", map[string]any{"rastreio_id": id, "quando": "fim_do_passe"})
		}
		return nil
	}
	f.mu.Unlock()
	return f.Pausa(id)
}

// Cancela para o rastreio de vez. Passe começado é fechado com o que tem.
func (f *Fila) Cancela(id int64) error {
	f.mu.Lock()
	if f.atual.deRastreio(id) {
		c := f.atual
		f.mu.Unlock()
		c.ex.Cancela()
		return nil
	}
	f.mu.Unlock()
	r, err := LeRastreio(f.d.DB, id)
	if err != nil {
		return err
	}
	switch r.Estado {
	case Cancelado:
		return nil
	case Concluido:
		return &ErroEstado{"o rastreio já terminou"}
	}
	f.encerraPasses(context.Background(), r.ID, r.GameID, CalibracaoPadrao(0, 0))
	return mudaEstadoRastreio(f.d.DB, id, Cancelado, "")
}

// encerraPasses fecha como cancelado todo passe que não terminou: o que tem
// amostras de trabalho é compactado (nada fica nas tabelas de trabalho).
func (f *Fila) encerraPasses(ctx context.Context, rastreioID, gameID int64, base Calibracao) {
	ps, err := lePasses(f.d.DB, `WHERE p.rastreio_id = ? AND p.estado NOT IN ('concluido','cancelado','ruim') ORDER BY p.ordem`, rastreioID)
	if err != nil {
		log.Printf("rastreio: encerrar passes de %d: %v", rastreioID, err)
		return
	}
	lt, _ := f.garanteLinha(ctx, gameID)
	for _, p := range ps {
		var n int
		f.d.DB.QueryRow(`SELECT COUNT(*) FROM amostras_brutas WHERE passe_id = ?`, p.ID).Scan(&n)
		if n == 0 {
			mudaEstadoPasse(f.d.DB, p.ID, Cancelado, "")
			continue
		}
		if _, err := finalizaPasse(f.d.DB, p, lt, timeDe(f.d.DB, gameID, p.ParticipanteID), base, Cancelado, ""); err != nil {
			log.Printf("rastreio: fechar o passe %d: %v", p.ID, err)
		} else if f.d.AoFecharPasse != nil {
			f.d.AoFecharPasse(gameID)
		}
	}
	pvs, err := lePassesVisao(f.d.DB, `WHERE rastreio_id = ? AND estado NOT IN ('concluido','cancelado','falhou') ORDER BY ordem`, rastreioID)
	if err != nil {
		log.Printf("rastreio: encerrar os passes de visão de %d: %v", rastreioID, err)
		return
	}
	f.encerraPassesVisao(pvs)
}

// encerraPassesVisao fecha como cancelado cada passe de visão que não
// terminou (o que tem capturas vira arquivo, se o time ainda não tem um).
func (f *Fila) encerraPassesVisao(pvs []PasseVisao) {
	for _, p := range pvs {
		if _, err := finalizaPasseVisao(f.d.DB, p, Cancelado, ""); err != nil {
			log.Printf("rastreio: fechar o passe de visão %d: %v", p.ID, err)
		} else if f.d.AoFecharPasse != nil && p.Capturas > 0 {
			f.d.AoFecharPasse(p.GameID)
		}
	}
}

func timeDe(db *banco.DB, gameID int64, pid int) int {
	var t int
	db.QueryRow(`SELECT time FROM participantes WHERE game_id = ? AND participante_id = ?`, gameID, pid).Scan(&t)
	return t
}

// corre é a execução: abre o replay, roda os passes que faltam e SEMPRE fecha
// o replay (sucesso, erro, pausa, cancelamento ou pânico).
func (f *Fila) corre(ctx context.Context, c *corrida) {
	ex, id := c.ex, c.rastreioID
	defer func() {
		f.mu.Lock()
		if f.atual == c {
			f.atual = nil
		}
		f.mu.Unlock()
		close(c.feito)
	}()
	defer ex.Fecha()
	var sess *replay.Sessao
	defer func() {
		if sess != nil {
			ex.Emite("fechando_replay", nil)
			if err := sess.Fecha(); err != nil {
				ex.Emite("aviso", map[string]string{"texto": "o replay não fechou direito: " + err.Error()})
			}
		}
	}()
	falha := func(msg string) {
		log.Printf("rastreio %d: %s", id, msg)
		mudaEstadoRastreio(f.d.DB, id, Falhou, msg)
		f.d.DB.Exec(`UPDATE passes SET estado = ? WHERE rastreio_id = ? AND estado = ?`, Falhou, id, Rodando)
		f.d.DB.Exec(`UPDATE passes_visao SET estado = ? WHERE rastreio_id = ? AND estado = ?`, Falhou, id, Rodando)
		ex.Emite("erro", map[string]string{"texto": msg})
	}
	defer func() {
		if p := recover(); p != nil {
			log.Printf("rastreio %d: pânico: %v\n%s", id, p, debug.Stack())
			falha(fmt.Sprintf("erro interno no rastreio: %v", p))
		}
	}()

	r, err := LeRastreio(f.d.DB, id)
	if err != nil {
		falha("rastreio sumiu do banco")
		return
	}
	pt, err := partida.Le(f.d.DB, r.GameID)
	if err != nil {
		falha("a partida não está no banco")
		return
	}
	lt, err := f.garanteLinha(ctx, r.GameID)
	if err != nil {
		ex.Emite("aviso", map[string]string{"texto": "sem a linha do tempo da partida: a posição usa a calibração padrão e a morte fica sem local"})
	}
	cfg := f.d.Config
	cfg.Velocidade = r.Velocidade
	ex.Emite("abrindo_replay", map[string]any{"rastreio_id": id, "game_id": r.GameID})
	sess, err = replay.Abre(ctx, f.d.Replay, r.GameID)
	if err != nil {
		sess = nil
		f.trataParada(ctx, ex, r, err, CalibracaoPadrao(0, 0))
		return
	}
	m, nomeNoJogo := f.novoMotor(sess, pt, lt, cfg, ex)
	ex.Emite("replay_aberto", map[string]any{"duracao_ms": int64(sess.Duracao * 1000), "largura": sess.Largura, "altura": sess.Altura})

	ps, err := lePasses(f.d.DB, `WHERE p.rastreio_id = ? AND p.estado NOT IN ('concluido','cancelado','ruim') ORDER BY p.ordem`, id)
	if err != nil {
		falha("ler os passes: " + err.Error())
		return
	}
	// rodaLista roda os passes em ordem; false = a corrida parou (pausa,
	// cancelamento, falha: já tratado).
	rodaLista := func(ps []Passe, primeiro bool) bool {
		for i, p := range ps {
			// Pausa graciosa: pedida durante o passe anterior, vale antes deste.
			if (i > 0 || !primeiro) && c.pausarNoFim.Load() {
				mudaEstadoRastreio(f.d.DB, id, Pausado, "")
				ex.Emite("pausado", map[string]any{"rastreio_id": id, "motivo": "pausa pedida: o passe em andamento terminou"})
				f.anotaParada(Parada{Tipo: TipoRastreio, ID: id, GameID: r.GameID, Motivo: "pausa pedida ao fim do passe"})
				return false
			}
			nome, ok := nomeNoJogo[p.ParticipanteID]
			if !ok {
				mudaEstadoPasse(f.d.DB, p.ID, Falhou, "jogador não encontrado no replay")
				continue
			}
			p = f.recomecaSeRuim(ex, p, r)
			time := timeDe(f.d.DB, r.GameID, p.ParticipanteID)
			a := alvo{pid: p.ParticipanteID, nome: nome, time: time}
			st, err := m.rodaPasse(ctx, p, a, r.TFimMs)
			// O eventdata só tem o que passou desde o último seek: coleta antes da
			// repescagem (que volta o replay).
			m.coletaEventos(r.GameID)
			if err != nil {
				f.trataParada(ctx, ex, r, err, m.cal)
				return false
			}
			// Passe ruim (qualidade_passe.go): não repesca (regravaria buracos de
			// um passe que não vale), fecha como "ruim" e a fila refaz no fim.
			brutas, _ := leBrutas(f.d.DB, p.ID, math.MinInt64, math.MaxInt64)
			av := AvaliaAmostras(brutas, f.d.Config.AmostrasPorSegundo)
			estado, motivo := Concluido, ""
			var resc ResumoRepescagem
			if av.Ruim {
				estado, motivo = Ruim, av.Motivo
			} else {
				// Repescagem dos buracos, com o replay ainda aberto e antes de fechar o passe.
				if resc, err = m.repescaNoFim(ctx, p, a); err != nil {
					f.trataParada(ctx, ex, r, err, m.cal)
					return false
				}
			}
			p, _ = LePasse(f.d.DB, p.ID)
			res, err := finalizaPasse(f.d.DB, p, lt, time, m.cal, estado, motivo)
			if err != nil {
				falha("fechar o passe: " + err.Error())
				return false
			}
			if estado == Concluido {
				if conf, err := conferePasse(f.d.DB, f.d.Config.Riot, p); err != nil {
					logConferencia(p, err)
				} else if conf != nil {
					ex.Emite("conferencia_riot", map[string]any{"passe_id": p.ID, "n": conf.N, "iguais": conf.Iguais,
						"maior_dif": conf.MaiorDif, "campos": conf.Campos})
					n, ig := int64(conf.N), int64(conf.Iguais)
					if c := AvaliaContagem(av.Vivas, av.SemTrava, av.Taxa, 0, &n, &ig); c.Ruim {
						estado, motivo = Ruim, c.Motivo
						if err := marcaRuim(f.d.DB, p.ID, motivo); err != nil {
							log.Printf("rastreio: marcar o passe %d como ruim: %v", p.ID, err)
						}
					}
				}
			}
			fim := map[string]any{"passe_id": p.ID, "participante_id": p.ParticipanteID, "estatisticas": st,
				"amostras": res.Amostras, "taxa": res.Taxa, "bytes": res.Bytes, "bytes_time": res.BytesTime, "cadeia": res.Cadeia,
				"calibracao": map[string]any{"modo": res.Calibracao.Modo, "pontos": res.Calibracao.Pontos,
					"erro_mediano": res.Calibracao.ErroMediano, "erro_maximo": res.Calibracao.ErroMaximo},
				"repescagem": resc, "estado": estado, "avaliacao": av}
			if estado == Ruim {
				fim["motivo"] = motivo
				log.Printf("rastreio %d: passe %d (participante %d) %s", id, p.ID, p.ParticipanteID, motivo)
				ex.Emite("passe_ruim", map[string]any{"passe_id": p.ID, "participante_id": p.ParticipanteID, "motivo": motivo,
					"avaliacao": av, "refaz_de": p.RefazDe})
			}
			ex.Emite("passe_fim", fim)
			if estado == Concluido && f.d.AoFecharPasse != nil {
				f.d.AoFecharPasse(r.GameID)
			}
		}
		return true
	}
	if !rodaLista(ps, true) {
		return
	}
	// Passe ruim é refeito UMA vez, no fim (MaxRefacoesAutomaticas), na mesma
	// abertura do replay: a causa pode ter passado (a pessoa parou de usar o
	// computador).
	refeitos, err := refacoesAutomaticas(f.d.DB, r)
	if err != nil {
		falha("criar os passes que refazem os ruins: " + err.Error())
		return
	}
	if len(refeitos) > 0 {
		ex.Emite("refazendo", map[string]any{"rastreio_id": id, "participantes": refeitos,
			"texto": fmt.Sprintf("%d passe(s) ruim(ns) refeito(s) uma vez no fim: participantes %v", len(refeitos), refeitos)})
		novos, err := lePasses(f.d.DB, `WHERE p.rastreio_id = ? AND p.estado = 'pendente' AND p.refaz_de <> 0 ORDER BY p.ordem`, id)
		if err != nil {
			falha("ler os passes novos: " + err.Error())
			return
		}
		if !rodaLista(novos, len(ps) == 0) {
			return
		}
	}
	// Os passes de visão de time pedidos com o rastreio, no fim e na mesma
	// abertura do replay (as posições dos jogadores já estão gravadas).
	pvs, err := lePassesVisao(f.d.DB, `WHERE rastreio_id = ? AND estado NOT IN ('concluido','cancelado') ORDER BY ordem`, id)
	if err != nil {
		falha("ler os passes de visão: " + err.Error())
		return
	}
	for i, pv := range pvs {
		if (i > 0 || len(ps) > 0) && c.pausarNoFim.Load() {
			mudaEstadoRastreio(f.d.DB, id, Pausado, "")
			ex.Emite("pausado", map[string]any{"rastreio_id": id, "motivo": "pausa pedida: o passe em andamento terminou"})
			f.anotaParada(Parada{Tipo: TipoRastreio, ID: id, GameID: r.GameID, Motivo: "pausa pedida ao fim do passe"})
			return
		}
		if err := f.passeVisao(ctx, m, pv); err != nil {
			f.trataParada(ctx, ex, r, err, m.cal)
			return
		}
	}
	mudaEstadoRastreio(f.d.DB, id, Concluido, "")
	fim := map[string]any{"rastreio_id": id, "refeitos": refeitos, "ruins": ainda(f.d.DB, id)}
	ex.Emite("concluido", fim)
}

// ainda: os participantes do rastreio que terminaram só com passe ruim (nem
// a refação saiu boa). É o "resumo" do fim: a tela diz quais refazer.
func ainda(db *banco.DB, rastreioID int64) []int {
	ps, err := lePasses(db, `WHERE p.rastreio_id = ? ORDER BY p.ordem`, rastreioID)
	if err != nil {
		return []int{}
	}
	bom := map[int]bool{}
	for _, p := range ps {
		if p.Estado == Concluido {
			bom[p.ParticipanteID] = true
		}
	}
	r, visto := []int{}, map[int]bool{}
	for _, p := range ps {
		if p.Estado == Ruim && !bom[p.ParticipanteID] && !visto[p.ParticipanteID] {
			visto[p.ParticipanteID] = true
			r = append(r, p.ParticipanteID)
		}
	}
	return r
}

// passeVisao roda e fecha um passe de visão. Erro devolvido = parada da
// corrida (pausa, cancelamento, partida ao vivo, replay que não responde). A
// névoa que não muda ou o minimapa que não se lê falham só este passe (fica
// "falhou" com a frase, e a corrida segue).
func (f *Fila) passeVisao(ctx context.Context, m *motor, pv PasseVisao) error {
	if pv.Estado == Falhou {
		// Falhou antes: se as capturas de trabalho ficaram (o processo caiu),
		// continua de onde parou; se não ficaram (a névoa não mudou), recomeça.
		var n int
		f.d.DB.QueryRow(`SELECT COUNT(*) FROM visao_capturas_brutas WHERE passe_id = ?`, pv.ID).Scan(&n)
		if n == 0 {
			f.d.DB.Exec(`UPDATE passes_visao SET t_atual_ms = t_inicio_ms, capturas = 0, capturas_modo_ok = 0, erro = '' WHERE id = ?`, pv.ID)
			pv, _ = LePasseVisao(f.d.DB, pv.ID)
		}
	}
	st, err := m.rodaPasseVisao(ctx, pv)
	if err != nil {
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}
		if errors.Is(err, ErrNevoa) || errors.Is(err, ErrSemLeitorMinimapa) {
			pv, _ = LePasseVisao(f.d.DB, pv.ID)
			if _, e := finalizaPasseVisao(f.d.DB, pv, Falhou, err.Error()); e != nil {
				log.Printf("rastreio: fechar o passe de visão %d: %v", pv.ID, e)
			}
			m.ex.Emite("aviso", map[string]string{"texto": err.Error()})
			m.ex.Emite("visao_fim", map[string]any{"passe_visao_id": pv.ID, "time": pv.Time, "estado": Falhou, "erro": err.Error()})
			return nil
		}
		return err
	}
	pv, _ = LePasseVisao(f.d.DB, pv.ID)
	fechado, err := finalizaPasseVisao(f.d.DB, pv, Concluido, "")
	if err != nil {
		return fmt.Errorf("fechar o passe de visão: %w", err)
	}
	ev := map[string]any{"passe_visao_id": fechado.ID, "time": fechado.Time, "estado": fechado.Estado, "capturas": fechado.Capturas,
		"capturas_modo_ok": fechado.CapturasModoOK, "taxa": fechado.Taxa, "estatisticas": st}
	if v, ok, _ := LeVisao(f.d.DB, fechado.GameID); ok {
		if vt, ok := v.DoObservador(fechado.Time); ok {
			ev["sentinelas"], ev["aliados_achados"] = len(vt.Sentinelas), vt.AliadosAchados
		}
	}
	m.ex.Emite("visao_fim", ev)
	if f.d.AoFecharPasse != nil {
		f.d.AoFecharPasse(fechado.GameID)
	}
	return nil
}

// novoMotor monta o motor de uma sessão aberta e casa os participantes da
// partida com os nomes do playerlist.
func (f *Fila) novoMotor(sess *replay.Sessao, pt partida.Partida, lt LinhaDoTempo, cfg Config, ex *web.Execucao) (*motor, map[int]string) {
	m := &motor{db: f.d.DB, s: sess, d: sess.Deps(), cfg: cfg, ex: ex, lt: lt,
		cal: CalibracaoPadrao(sess.Largura, sess.Altura), pidDe: map[string]int{}}
	m.d.Relogio = f.d.Replay.Relogio
	m.aoEsperar = func(e *EsperaUsuario) { f.esperando.Store(e) }
	nomeNoJogo := map[int]string{}
	for _, part := range pt.Participantes {
		for _, j := range sess.Jogadores {
			if j.NomeDeSelecao() == part.Nome {
				nomeNoJogo[part.ParticipanteID] = j.NomeDeSelecao()
				m.pidDe[j.NomeDeSelecao()] = part.ParticipanteID
			}
		}
	}
	// Quem não casou pelo nome (trocou de nome depois da partida) casa pela
	// ordem: o playerlist vem ORDER e CHAOS na mesma ordem dos participantes.
	io, ic := 0, 5
	for _, j := range sess.Jogadores {
		pid := 0
		if j.Time == "ORDER" {
			io++
			pid = io
		} else {
			ic++
			pid = ic
		}
		if _, ok := m.pidDe[j.NomeDeSelecao()]; !ok {
			if _, usado := nomeNoJogo[pid]; !usado {
				m.pidDe[j.NomeDeSelecao()] = pid
				nomeNoJogo[pid] = j.NomeDeSelecao()
			}
		}
		m.ordem = append(m.ordem, m.pidDe[j.NomeDeSelecao()])
	}
	return m, nomeNoJogo
}

// trataParada decide o que um erro/cancelamento significa para o rastreio.
func (f *Fila) trataParada(ctx context.Context, ex *web.Execucao, r Rastreio, err error, base Calibracao) {
	if ctx.Err() != nil {
		// Cancelado no meio de um pedido: o erro é "context canceled"; o que
		// importa é a causa (pausa ou cancelamento).
		err = context.Cause(ctx)
	}
	var fase *replay.ErroFase
	marcaRodando := func(estado, msg string) {
		f.d.DB.Exec(`UPDATE passes SET estado = ?, erro = ? WHERE rastreio_id = ? AND estado = ?`, estado, msg, r.ID, Rodando)
		f.d.DB.Exec(`UPDATE passes_visao SET estado = ?, erro = ? WHERE rastreio_id = ? AND estado = ?`, estado, msg, r.ID, Rodando)
	}
	switch {
	case errors.Is(err, ErrPausado):
		// Pausa pedida (o botão, o atalho) ou decidida pelo motor (a pessoa
		// usando o computador além do teto, o disjuntor de novo): o motivo fica
		// no rastreio e a tela/o aviso do sistema mostram.
		motivo := MotivoPausa(err)
		marcaRodando(Pausado, motivo)
		mudaEstadoRastreio(f.d.DB, r.ID, Pausado, motivo)
		ev := map[string]any{"rastreio_id": r.ID}
		if motivo != "" {
			ev["motivo"] = motivo
		}
		ex.Emite("pausado", ev)
		f.anotaParada(Parada{Tipo: TipoRastreio, ID: r.ID, GameID: r.GameID, Motivo: motivo})
	case errors.Is(err, ErrCancelado):
		f.encerraPasses(context.Background(), r.ID, r.GameID, base)
		mudaEstadoRastreio(f.d.DB, r.ID, Cancelado, "")
	case errors.As(err, &fase):
		// Partida ao vivo no meio: pausa (dá para retomar depois).
		marcaRodando(Pausado, err.Error())
		mudaEstadoRastreio(f.d.DB, r.ID, Pausado, err.Error())
		ex.Emite("aviso", map[string]string{"texto": err.Error()})
		ex.Emite("pausado", map[string]any{"rastreio_id": r.ID, "motivo": err.Error()})
		f.anotaParada(Parada{Tipo: TipoRastreio, ID: r.ID, GameID: r.GameID, Motivo: err.Error()})
	default:
		msg := err.Error()
		if _, frase, ok := lcu.StatusHTTP(err); ok {
			msg = frase
		}
		log.Printf("rastreio %d: %v", r.ID, err)
		marcaRodando(Falhou, msg)
		mudaEstadoRastreio(f.d.DB, r.ID, Falhou, msg)
		ex.Emite("erro", map[string]string{"texto": msg})
	}
}
