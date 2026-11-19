package rastreio

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime/debug"
	"sort"

	"loltracking/server/banco"
	"loltracking/server/lcu"
	"loltracking/server/partida"
	"loltracking/server/replay"
)

// Os dois passes de visão de time avulsos (POST /api/partidas/{id}/visao/rastreio),
// para partida já rastreada: abre o replay, roda a visão do azul e a do
// vermelho, fecha o replay. É uma corrida da fila como o rastreio e a
// repescagem: uma coisa por vez, replay fechado ao terminar, falhar ou
// cancelar.

// Folgas da estimativa (s): abrir e fechar o replay e, por passe, o seek, a
// tecla e a conferência do painel.
const (
	FolgaAbrirReplayS = 40.0
	FolgaPasseVisaoS  = 10.0
)

// EstimativaVisaoS: quanto os dois passes levam numa partida de duracaoS.
func EstimativaVisaoS(duracaoS, vel float64) float64 {
	if vel <= 0 {
		vel = VelocidadeVisaoPadrao
	}
	return 2*(duracaoS/vel+FolgaPasseVisaoS) + FolgaAbrirReplayS
}

// EstadoVisaoPartida é a resposta de GET /api/partidas/{id}/visao.
type EstadoVisaoPartida struct {
	GameID int64 `json:"game_id"`
	// Disponivel: dá para rastrear a visão agora (há jogadores rastreados e a
	// leitura do minimapa existe); senão Motivo diz por quê.
	Disponivel       bool            `json:"disponivel"`
	Motivo           string          `json:"motivo"`
	Rastreados       []int           `json:"rastreados"`
	VelocidadePadrao float64         `json:"velocidade_padrao"`
	EstimativaS      float64         `json:"estimativa_s"`
	Passes           []PasseVisao    `json:"passes"`
	Execucao         *IDExec         `json:"execucao"`
	Rodando          bool            `json:"rodando"`
	Visao            *VisaoDaPartida `json:"visao"`
}

// rastreadosDaPartida: participantes com passe concluído com amostras.
func rastreadosDaPartida(db *banco.DB, gameID int64) ([]int, error) {
	ps, err := PassesDaPartida(db, gameID)
	if err != nil {
		return nil, err
	}
	vistos := map[int]bool{}
	ids := []int{}
	for _, p := range ps {
		if p.Estado == Concluido && p.Amostras > 0 && !vistos[p.ParticipanteID] {
			vistos[p.ParticipanteID] = true
			ids = append(ids, p.ParticipanteID)
		}
	}
	sort.Ints(ids)
	return ids, nil
}

// ExecucaoDaVisao é a última execução dos passes de visão avulsos da partida
// neste processo, e se ainda roda.
func (f *Fila) ExecucaoDaVisao(gameID int64) (string, bool, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ex, ok := f.visoes[gameID]
	rodando := ok && f.atual != nil && f.atual.visaoDe == gameID
	return ex, ok, rodando
}

// EstadoDaVisao monta a resposta de GET .../visao.
func (f *Fila) EstadoDaVisao(gameID int64) (EstadoVisaoPartida, error) {
	pt, err := partida.Le(f.d.DB, gameID)
	if err != nil {
		return EstadoVisaoPartida{}, err
	}
	e := EstadoVisaoPartida{GameID: gameID, VelocidadePadrao: f.d.Config.VelocidadeVisao,
		EstimativaS: EstimativaVisaoS(float64(pt.DuracaoS), f.d.Config.VelocidadeVisao)}
	if e.Rastreados, err = rastreadosDaPartida(f.d.DB, gameID); err != nil {
		return e, err
	}
	if e.Passes, err = PassesVisaoDaPartida(f.d.DB, gameID); err != nil {
		return e, err
	}
	switch {
	case f.d.Config.Minimapa == nil:
		e.Motivo = ErrSemLeitorMinimapa.Error()
	case len(e.Rastreados) == 0:
		e.Motivo = "a partida não tem jogadores rastreados: a visibilidade é decidida pela posição deles, rastreie os jogadores antes"
	default:
		e.Disponivel = true
	}
	if exID, ok, rodando := f.ExecucaoDaVisao(gameID); ok {
		e.Execucao, e.Rodando = &IDExec{ID: exID}, rodando
	}
	// Passe de visão de um rastreio rodando agora também conta como rodando.
	f.mu.Lock()
	if c := f.atual; c != nil && !e.Rodando && c.repescagemDe == 0 && c.visaoDe == 0 {
		for _, p := range e.Passes {
			if p.RastreioID == c.rastreioID && p.Estado == Rodando {
				e.Rodando, e.Execucao = true, &IDExec{ID: c.ex.ID()}
			}
		}
	}
	f.mu.Unlock()
	if v, ok, err := LeVisao(f.d.DB, gameID); err != nil {
		return e, err
	} else if ok {
		e.Visao = v
	}
	return e, nil
}

// PedidoVisao é o corpo de POST .../visao/rastreio.
type PedidoVisao struct {
	Velocidade float64
	// TInicioMs/TFimMs: o trecho (0/0 = a partida inteira). A sonda usa trechos.
	TInicioMs, TFimMs int64
}

// RastreiaVisao valida e começa os dois passes de visão. Passes avulsos que
// pararam no meio (pausados pela queda do servidor, ou que falharam) são
// continuados; senão, cria os dois.
func (f *Fila) RastreiaVisao(ctx context.Context, gameID int64, ped PedidoVisao) (string, []PasseVisao, error) {
	pt, err := partida.Le(f.d.DB, gameID)
	if err != nil {
		return "", nil, err
	}
	if f.d.Config.Minimapa == nil {
		return "", nil, &ErroEstado{ErrSemLeitorMinimapa.Error()}
	}
	rast, err := rastreadosDaPartida(f.d.DB, gameID)
	if err != nil {
		return "", nil, err
	}
	if len(rast) == 0 {
		return "", nil, &ErroEstado{"a partida não tem jogadores rastreados: a visibilidade é decidida pela posição deles, rastreie os jogadores antes"}
	}
	vel := ped.Velocidade
	if vel == 0 {
		vel = f.d.Config.VelocidadeVisao
	}
	if vel < 0.5 || vel > 8 {
		return "", nil, &ErroInvalido{"velocidade precisa estar entre 0,5 e 8"}
	}
	if ped.TInicioMs < 0 || ped.TFimMs < 0 || (ped.TFimMs > 0 && ped.TFimMs <= ped.TInicioMs) {
		return "", nil, &ErroInvalido{"de_ms e ate_ms precisam ser ≥ 0, com ate_ms > de_ms (ate_ms 0 = até o fim)"}
	}
	fase, err := f.d.LCU.FaseGameflow(ctx)
	if err != nil {
		return "", nil, err
	}
	if !replay.FasePermitida(fase) {
		return "", nil, &ErroEstado{(&replay.ErroFase{Fase: fase}).Error()}
	}
	if m, err := f.d.LCU.MetadataReplay(ctx, gameID); err != nil || m.Estado != "watch" {
		return "", nil, &ErroEstado{"o replay desta partida não está baixado (ou não abre mais neste patch); baixe antes de rastrear a visão"}
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.atual != nil {
		return "", nil, f.ocupadoTravado()
	}
	pvs, err := lePassesVisao(f.d.DB, `WHERE game_id = ? AND rastreio_id = 0 AND estado IN ('pendente','pausado','falhou','rodando') ORDER BY id`, gameID)
	if err != nil {
		return "", nil, err
	}
	if len(pvs) == 0 || ped.TInicioMs > 0 || ped.TFimMs > 0 {
		// Trecho pedido: passes novos (os parados no meio eram de outro trecho).
		for _, p := range pvs {
			f.d.DB.Exec(`UPDATE passes_visao SET estado = ? WHERE id = ?`, Cancelado, p.ID)
			f.d.DB.Exec(`DELETE FROM visao_capturas_brutas WHERE passe_id = ?`, p.ID)
		}
		if err := criaPassesVisao(f.d.DB, gameID, 0, []int{100, 200}, vel, ped.TInicioMs, ped.TFimMs); err != nil {
			return "", nil, err
		}
		if pvs, err = lePassesVisao(f.d.DB, `WHERE game_id = ? AND rastreio_id = 0 AND estado = 'pendente' ORDER BY id`, gameID); err != nil {
			return "", nil, err
		}
	} else {
		for i := range pvs {
			f.d.DB.Exec(`UPDATE passes_visao SET velocidade = ? WHERE id = ?`, vel, pvs[i].ID)
			pvs[i].Velocidade = vel
		}
	}
	ex := f.d.Hub.Nova()
	cctx, cancela := context.WithCancelCause(context.Background())
	ex.AoCancelar(func() { cancela(ErrCancelado) })
	c := &corrida{ex: ex, cancela: cancela, feito: make(chan struct{}), visaoDe: gameID, pedidoVisao: PedidoVisao{Velocidade: vel}}
	f.atual = c
	f.visoes[gameID] = ex.ID()
	go f.correVisao(cctx, c, pt, pvs)
	return ex.ID(), pvs, nil
}

// correVisao é a execução: abre o replay, roda os passes de visão e SEMPRE
// fecha o replay (sucesso, erro, cancelamento ou pânico).
func (f *Fila) correVisao(ctx context.Context, c *corrida, pt partida.Partida, pvs []PasseVisao) {
	ex, gameID := c.ex, pt.GameID
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
	ids := make([]int64, len(pvs))
	for i, p := range pvs {
		ids[i] = p.ID
	}
	pendentes := func() []PasseVisao {
		var l []PasseVisao
		for _, id := range ids {
			if p, err := LePasseVisao(f.d.DB, id); err == nil && p.Estado != Concluido && p.Estado != Cancelado && p.Estado != Falhou {
				l = append(l, p)
			}
		}
		return l
	}
	marcaRodando := func(estado, msg string) {
		for _, id := range ids {
			f.d.DB.Exec(`UPDATE passes_visao SET estado = ?, erro = ? WHERE id = ? AND estado = ?`, estado, msg, id, Rodando)
		}
	}
	falha := func(err error) {
		if ctx.Err() != nil {
			err = context.Cause(ctx)
		}
		var fase *replay.ErroFase
		switch {
		case errors.Is(err, ErrCancelado):
			f.encerraPassesVisao(pendentes())
			ex.Emite("cancelado", map[string]any{"game_id": gameID})
		case errors.Is(err, ErrPausado):
			// Pausar a captura: o passe de visão fica pausado e o próximo pedido
			// continua dele (RastreiaVisao sem trecho).
			motivo := MotivoPausa(err)
			marcaRodando(Pausado, motivo)
			ex.Emite("pausado", map[string]any{"game_id": gameID, "motivo": motivo})
			f.anotaParada(Parada{Tipo: TipoVisao, ID: gameID, GameID: gameID, Motivo: motivo})
		case errors.As(err, &fase):
			marcaRodando(Pausado, err.Error())
			ex.Emite("aviso", map[string]string{"texto": err.Error()})
			ex.Emite("erro", map[string]string{"texto": err.Error()})
		default:
			msg := err.Error()
			if _, frase, ok := lcu.StatusHTTP(err); ok {
				msg = frase
			}
			log.Printf("visão dos times da partida %d: %v", gameID, err)
			marcaRodando(Falhou, msg)
			ex.Emite("erro", map[string]string{"texto": msg})
		}
	}
	defer func() {
		if p := recover(); p != nil {
			log.Printf("visão dos times da partida %d: pânico: %v\n%s", gameID, p, debug.Stack())
			marcaRodando(Falhou, fmt.Sprintf("erro interno: %v", p))
			ex.Emite("erro", map[string]string{"texto": fmt.Sprintf("erro interno no rastreio da visão: %v", p)})
		}
	}()

	lt, err := f.garanteLinha(ctx, gameID)
	if err != nil {
		ex.Emite("aviso", map[string]string{"texto": "sem a linha do tempo da partida"})
	}
	cfg := f.d.Config
	ex.Emite("abrindo_replay", map[string]any{"game_id": gameID, "visao": true, "passes": len(pvs)})
	sess, err = replay.Abre(ctx, f.d.Replay, gameID)
	if err != nil {
		sess = nil
		falha(err)
		return
	}
	m, _ := f.novoMotor(sess, pt, lt, cfg, ex)
	ex.Emite("replay_aberto", map[string]any{"duracao_ms": int64(sess.Duracao * 1000), "largura": sess.Largura, "altura": sess.Altura})
	for _, pv := range pvs {
		pv, _ = LePasseVisao(f.d.DB, pv.ID)
		if err := f.passeVisao(ctx, m, pv); err != nil {
			falha(err)
			return
		}
	}
	ex.Emite("concluido", map[string]any{"game_id": gameID})
}
