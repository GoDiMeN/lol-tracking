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

// Repescagem de passe já gravado (POST /api/partidas/{id}/rastreio/repescagem):
// abre o replay da partida, repesca os buracos de cada passe que vale (o mesmo
// laço do rastreio, repescagem.go), fecha de novo os passes que ganharam algum
// trecho (amostras, estado dos 10 e cadeia, pelo fechamento normal), pede a
// análise de novo e fecha o replay. É uma corrida da fila como o rastreio: uma
// coisa por vez, replay fechado ao terminar, falhar ou cancelar.

// PlanoRepescagem são os buracos de um passe que a repescagem vai tentar.
type PlanoRepescagem struct {
	PasseID        int64    `json:"passe_id"`
	ParticipanteID int      `json:"participante_id"`
	Buracos        []Buraco `json:"buracos"`
	passe          Passe
	// jaRegravados: buracos que uma repescagem anterior já regravou e em que
	// o jogo tirou o jogador da tela de novo (BuracoNaoSelecionavel): não
	// entram no plano, mas voltam ao resumo novo (senão a prova se perde e a
	// próxima repescagem tentaria de novo).
	jaRegravados []Buraco
}

// PlanoDeRepescagem acha, para cada participante (todos ou os pedidos), o
// passe que vale na partida (o concluído mais recente, com bloco) e os buracos
// dele maiores que LimiarRepescagemMs. Só entram passes com buraco. Buraco que
// uma repescagem já regravou e em que o jogo tirou o jogador da tela de novo
// (ClassificaBuracos) não entra: regravar daria o mesmo vazio.
func PlanoDeRepescagem(db *banco.DB, gameID int64, participantes []int) ([]PlanoRepescagem, error) {
	pt, err := partida.Le(db, gameID)
	if err != nil {
		return nil, err
	}
	pedidos := map[int]bool{}
	for _, id := range participantes {
		pedidos[id] = true
	}
	planos := []PlanoRepescagem{}
	for _, part := range pt.Participantes {
		if len(pedidos) > 0 && !pedidos[part.ParticipanteID] {
			continue
		}
		p, ok, err := melhorPasse(db, gameID, part.ParticipanteID)
		if err != nil {
			return nil, err
		}
		if !ok || p.Estado != Concluido || !p.Compactado {
			continue
		}
		as, _, err := amostrasDoPasse(db, p)
		if err != nil {
			return nil, err
		}
		bs := Buracos(as, LimiarRepescagemMs)
		ClassificaBuracos(as, bs, timeDe(db, gameID, p.ParticipanteID), p.Repescagem)
		pl := PlanoRepescagem{PasseID: p.ID, ParticipanteID: p.ParticipanteID, Buracos: []Buraco{}, passe: p}
		for _, b := range bs {
			if b.Tipo == BuracoNaoSelecionavel && reproduzido(b, p.Repescagem) {
				b.Motivo = "já regravado antes: o jogo tira o jogador da tela aqui; não é defeito"
				pl.jaRegravados = append(pl.jaRegravados, b)
				continue
			}
			pl.Buracos = append(pl.Buracos, b)
		}
		if len(pl.Buracos) > 0 {
			planos = append(planos, pl)
		}
	}
	sort.Slice(planos, func(a, b int) bool { return planos[a].ParticipanteID < planos[b].ParticipanteID })
	return planos, nil
}

// TotalDeBuracos soma os buracos dos planos.
func TotalDeBuracos(planos []PlanoRepescagem) int {
	n := 0
	for _, p := range planos {
		n += len(p.Buracos)
	}
	return n
}

// PedidoRepescagem é o corpo da rota.
type PedidoRepescagem struct {
	Participantes []int
	Velocidade    float64
}

// ExecucaoDaRepescagem é a última execução de repescagem da partida neste
// processo, e se ainda roda.
func (f *Fila) ExecucaoDaRepescagem(gameID int64) (string, bool, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ex, ok := f.repescagens[gameID]
	rodando := ok && f.atual != nil && f.atual.repescagemDe == gameID
	return ex, ok, rodando
}

// Repesca valida, monta o plano e começa a repescagem. Devolve a execução.
func (f *Fila) Repesca(ctx context.Context, gameID int64, ped PedidoRepescagem) (string, []PlanoRepescagem, error) {
	pt, err := partida.Le(f.d.DB, gameID)
	if err != nil {
		return "", nil, err
	}
	ids := map[int]bool{}
	for _, p := range pt.Participantes {
		ids[p.ParticipanteID] = true
	}
	vistos := map[int]bool{}
	for _, id := range ped.Participantes {
		if !ids[id] {
			return "", nil, &ErroInvalido{fmt.Sprintf("participante %d não existe nesta partida", id)}
		}
		if vistos[id] {
			return "", nil, &ErroInvalido{fmt.Sprintf("participante %d repetido", id)}
		}
		vistos[id] = true
	}
	vel := ped.Velocidade
	if vel == 0 {
		vel = f.d.Config.Velocidade
	}
	if vel < 0.5 || vel > 8 {
		return "", nil, &ErroInvalido{"velocidade precisa estar entre 0,5 e 8"}
	}
	planos, err := PlanoDeRepescagem(f.d.DB, gameID, ped.Participantes)
	if err != nil {
		return "", nil, err
	}
	if len(planos) == 0 {
		return "", planos, &ErroEstado{"nenhum trecho sem amostra para recuperar nos passes desta partida"}
	}
	fase, err := f.d.LCU.FaseGameflow(ctx)
	if err != nil {
		return "", nil, err
	}
	if !replay.FasePermitida(fase) {
		return "", nil, &ErroEstado{(&replay.ErroFase{Fase: fase}).Error()}
	}
	if m, err := f.d.LCU.MetadataReplay(ctx, gameID); err != nil || m.Estado != "watch" {
		return "", nil, &ErroEstado{"o replay desta partida não está baixado (ou não abre mais neste patch); baixe antes de repescar"}
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.atual != nil {
		return "", nil, f.ocupadoTravado()
	}
	ex := f.d.Hub.Nova()
	cctx, cancela := context.WithCancelCause(context.Background())
	ex.AoCancelar(func() { cancela(ErrCancelado) })
	c := &corrida{rastreioID: planos[0].passe.RastreioID, ex: ex, cancela: cancela, feito: make(chan struct{}), repescagemDe: gameID}
	f.atual = c
	f.repescagens[gameID] = ex.ID()
	go f.correRepescagem(cctx, c, pt, planos, vel)
	return ex.ID(), planos, nil
}

// correRepescagem é a execução: abre o replay, repesca passe a passe e SEMPRE
// fecha o replay (sucesso, erro, cancelamento ou pânico).
func (f *Fila) correRepescagem(ctx context.Context, c *corrida, pt partida.Partida, planos []PlanoRepescagem, vel float64) {
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
	mexeu := false
	// A análise refaz o que depende dos passes regravados (também se parou no meio).
	defer func() {
		if mexeu && f.d.AoFecharPasse != nil {
			f.d.AoFecharPasse(gameID)
		}
	}()
	falha := func(err error) {
		if ctx.Err() != nil {
			err = context.Cause(ctx)
		}
		var fase *replay.ErroFase
		switch {
		case errors.Is(err, ErrCancelado):
			ex.Emite("cancelado", map[string]any{"game_id": gameID})
		case errors.As(err, &fase):
			ex.Emite("aviso", map[string]string{"texto": err.Error()})
			ex.Emite("erro", map[string]string{"texto": err.Error()})
		default:
			msg := err.Error()
			if _, frase, ok := lcu.StatusHTTP(err); ok {
				msg = frase
			}
			log.Printf("repescagem da partida %d: %v", gameID, err)
			ex.Emite("erro", map[string]string{"texto": msg})
		}
	}
	defer func() {
		if p := recover(); p != nil {
			log.Printf("repescagem da partida %d: pânico: %v\n%s", gameID, p, debug.Stack())
			ex.Emite("erro", map[string]string{"texto": fmt.Sprintf("erro interno na repescagem: %v", p)})
		}
	}()

	lt, err := f.garanteLinha(ctx, gameID)
	if err != nil {
		ex.Emite("aviso", map[string]string{"texto": "sem a linha do tempo da partida: a morte fica sem local"})
	}
	cfg := f.d.Config
	cfg.Velocidade = vel
	ex.Emite("abrindo_replay", map[string]any{"game_id": gameID, "repescagem": true, "buracos": TotalDeBuracos(planos)})
	sess, err = replay.Abre(ctx, f.d.Replay, gameID)
	if err != nil {
		sess = nil
		falha(err)
		return
	}
	m, nomeNoJogo := f.novoMotor(sess, pt, lt, cfg, ex)
	ex.Emite("replay_aberto", map[string]any{"duracao_ms": int64(sess.Duracao * 1000), "largura": sess.Largura, "altura": sess.Altura})

	tot := ResumoRepescagem{Quando: "depois", Restantes: []Buraco{}}
	for _, pl := range planos {
		p := pl.passe
		nome, ok := nomeNoJogo[p.ParticipanteID]
		if !ok {
			ex.Emite("aviso", map[string]string{"texto": fmt.Sprintf("participante %d não encontrado no replay", p.ParticipanteID)})
			continue
		}
		if _, err := carregaTrabalho(f.d.DB, p); err != nil {
			descartaTrabalho(f.d.DB, p.ID)
			ex.Emite("aviso", map[string]string{"texto": fmt.Sprintf("passe %d: %v", p.ID, err)})
			continue
		}
		time := timeDe(f.d.DB, gameID, p.ParticipanteID)
		res, errR := m.repesca(ctx, p, alvo{pid: p.ParticipanteID, nome: nome, time: time}, pl.Buracos, "depois")
		// Fecha de novo só o que ganhou trecho; o resto volta a ser só o bloco antigo.
		if res.Recuperados > 0 {
			atual, _ := LePasse(f.d.DB, p.ID)
			if r, err := refinalizaPasse(f.d.DB, atual, lt, time, m.cal); err != nil {
				descartaTrabalho(f.d.DB, p.ID)
				ex.Emite("aviso", map[string]string{"texto": fmt.Sprintf("passe %d: não deu para regravar: %v", p.ID, err)})
				res.Restantes = append(res.Restantes, pl.Buracos...)
				res.Recuperados = 0
			} else {
				mexeu = true
				ex.Emite("passe_regravado", map[string]any{"passe_id": p.ID, "participante_id": p.ParticipanteID,
					"amostras": r.Amostras, "taxa": r.Taxa, "cadeia": r.Cadeia})
			}
		} else {
			descartaTrabalho(f.d.DB, p.ID)
		}
		tentados := res.Restantes
		// O resumo gravado guarda também os que ficaram de fora do plano.
		res.Restantes = append(append([]Buraco{}, tentados...), pl.jaRegravados...)
		res.Sobraram = len(res.Restantes)
		if errR == nil || res.Recuperados > 0 {
			if err := gravaResumoRepescagem(f.d.DB, p.ID, res); err != nil {
				log.Printf("repescagem: gravar o resumo do passe %d: %v", p.ID, err)
			}
		}
		tot.Buracos += res.Buracos
		tot.Recuperados += res.Recuperados
		tot.Restantes = append(tot.Restantes, tentados...)
		if errR != nil {
			falha(errR)
			return
		}
	}
	ex.Emite("concluido", map[string]any{"game_id": gameID, "buracos": tot.Buracos, "recuperados": tot.Recuperados,
		"sobraram": len(tot.Restantes)})
}
