package rastreio

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime/debug"
	"sort"
	"sync"

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
}

// Fila roda um rastreio por vez. Um rastreio = até 10 passes em sequência,
// todos na mesma abertura do replay.
type Fila struct {
	d Deps

	mu        sync.Mutex
	atual     *corrida
	execucoes map[int64]string
}

type corrida struct {
	rastreioID int64
	ex         *web.Execucao
	cancela    context.CancelCauseFunc
	feito      chan struct{}
}

// NovaFila cria a fila. Rastreio que estava "rodando" no banco (o processo
// morreu no meio) vira "pausado", e o jogo que tenha ficado aberto é fechado.
func NovaFila(d Deps) *Fila {
	d.Config = d.Config.comPadroes()
	if d.Replay.Relogio == nil {
		d.Replay.Relogio = replay.RelogioReal{}
	}
	f := &Fila{d: d, execucoes: map[int64]string{}}
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
}

// ErroOcupado: já há um rastreio rodando (409).
type ErroOcupado struct {
	RastreioID int64
	Execucao   string
}

func (e *ErroOcupado) Error() string {
	return fmt.Sprintf("já há um rastreio rodando (rastreio %d); pause ou cancele antes", e.RastreioID)
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
		return Rastreio{}, "", &ErroOcupado{RastreioID: f.atual.rastreioID, Execucao: f.atual.ex.ID()}
	}
	id, err := criaRastreio(f.d.DB, gameID, vel, ped.TInicioMs, ped.TFimMs, jogadores)
	if err != nil {
		return Rastreio{}, "", err
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
		return "", &ErroOcupado{RastreioID: f.atual.rastreioID, Execucao: f.atual.ex.ID()}
	}
	if r.Estado != Pausado && r.Estado != Falhou {
		return "", &ErroEstado{fmt.Sprintf("o rastreio está %s; só dá para retomar pausado ou que falhou", r.Estado)}
	}
	return f.comecaTravado(id), nil
}

// Pausa para o rastreio e fecha o replay; o progresso fica para retomar.
func (f *Fila) Pausa(id int64) error {
	f.mu.Lock()
	if f.atual != nil && f.atual.rastreioID == id {
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

// Cancela para o rastreio de vez. Passe começado é fechado com o que tem.
func (f *Fila) Cancela(id int64) error {
	f.mu.Lock()
	if f.atual != nil && f.atual.rastreioID == id {
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
	ps, err := lePasses(f.d.DB, `WHERE p.rastreio_id = ? AND p.estado NOT IN ('concluido','cancelado') ORDER BY p.ordem`, rastreioID)
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
	m := &motor{db: f.d.DB, s: sess, d: sess.Deps(), cfg: cfg, ex: ex, lt: lt,
		cal: CalibracaoPadrao(sess.Largura, sess.Altura), pidDe: map[string]int{}}
	m.d.Relogio = f.d.Replay.Relogio
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
	ex.Emite("replay_aberto", map[string]any{"duracao_ms": int64(sess.Duracao * 1000), "largura": sess.Largura, "altura": sess.Altura})

	ps, err := lePasses(f.d.DB, `WHERE p.rastreio_id = ? AND p.estado NOT IN ('concluido','cancelado') ORDER BY p.ordem`, id)
	if err != nil {
		falha("ler os passes: " + err.Error())
		return
	}
	for _, p := range ps {
		nome, ok := nomeNoJogo[p.ParticipanteID]
		if !ok {
			mudaEstadoPasse(f.d.DB, p.ID, Falhou, "jogador não encontrado no replay")
			continue
		}
		time := timeDe(f.d.DB, r.GameID, p.ParticipanteID)
		st, err := m.rodaPasse(ctx, p, alvo{pid: p.ParticipanteID, nome: nome, time: time}, r.TFimMs)
		m.coletaEventos(r.GameID)
		if err != nil {
			f.trataParada(ctx, ex, r, err, m.cal)
			return
		}
		p, _ = LePasse(f.d.DB, p.ID)
		res, err := finalizaPasse(f.d.DB, p, lt, time, m.cal, Concluido, "")
		if err != nil {
			falha("fechar o passe: " + err.Error())
			return
		}
		if conf, err := conferePasse(f.d.DB, f.d.Config.Riot, p); err != nil {
			logConferencia(p, err)
		} else if conf != nil {
			ex.Emite("conferencia_riot", map[string]any{"passe_id": p.ID, "n": conf.N, "iguais": conf.Iguais,
				"maior_dif": conf.MaiorDif, "campos": conf.Campos})
		}
		ex.Emite("passe_fim", map[string]any{"passe_id": p.ID, "participante_id": p.ParticipanteID, "estatisticas": st,
			"amostras": res.Amostras, "taxa": res.Taxa, "bytes": res.Bytes, "bytes_time": res.BytesTime, "cadeia": res.Cadeia,
			"calibracao": map[string]any{"modo": res.Calibracao.Modo, "pontos": res.Calibracao.Pontos,
				"erro_mediano": res.Calibracao.ErroMediano, "erro_maximo": res.Calibracao.ErroMaximo}})
	}
	mudaEstadoRastreio(f.d.DB, id, Concluido, "")
	ex.Emite("concluido", map[string]any{"rastreio_id": id})
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
	}
	switch {
	case errors.Is(err, ErrPausado):
		marcaRodando(Pausado, "")
		mudaEstadoRastreio(f.d.DB, r.ID, Pausado, "")
		ex.Emite("pausado", map[string]any{"rastreio_id": r.ID})
	case errors.Is(err, ErrCancelado):
		f.encerraPasses(context.Background(), r.ID, r.GameID, base)
		mudaEstadoRastreio(f.d.DB, r.ID, Cancelado, "")
	case errors.As(err, &fase):
		// Partida ao vivo no meio: pausa (dá para retomar depois).
		marcaRodando(Pausado, err.Error())
		mudaEstadoRastreio(f.d.DB, r.ID, Pausado, err.Error())
		ex.Emite("aviso", map[string]string{"texto": err.Error()})
		ex.Emite("pausado", map[string]any{"rastreio_id": r.ID, "motivo": err.Error()})
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
