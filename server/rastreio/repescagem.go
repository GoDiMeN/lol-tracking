package rastreio

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"

	"loltracking/server/banco"
	"loltracking/server/replay"
)

// Repescagem: com o replay ainda aberto, volta a um buraco (trecho sem a
// posição de verdade do jogador vivo, buracos.go), trava a câmera de novo e
// grava só aquele trecho, com FolgaRepescagemMs dos dois lados, trocando o que
// havia ali. Roda ao fim de cada passe (fila.go, antes de fechar o passe) e
// pela rota de repescagem de um passe já gravado (Fila.Repesca). Todas as
// regras do rastreio valem, porque é o mesmo laço (motor.segue): fase do
// cliente, tecla só depois de a Replay API responder, só as teclas de seleção.

// repesca tenta fechar os buracos bs do passe p (as linhas de trabalho do
// passe têm de estar nas tabelas de trabalho). Devolve o resumo. Erro só para
// pausa, cancelamento ou partida ao vivo (o chamador trata como no passe);
// outra falha (o replay parou de responder...) encerra a repescagem e o que
// faltou fica como restante, sem perder o passe.
func (m *motor) repesca(ctx context.Context, p Passe, a alvo, bs []Buraco, quando string) (ResumoRepescagem, error) {
	res := ResumoRepescagem{Quando: quando, Buracos: len(bs), Restantes: []Buraco{}}
	m.ex.Emite("repescagem_inicio", map[string]any{"passe_id": p.ID, "participante_id": a.pid, "buracos": bs,
		"limite": MaxRepescagensPorPasse, "quando": quando})
	parou := ""
	for k, b := range bs {
		if parou != "" {
			b.Motivo = parou
			res.Restantes = append(res.Restantes, b)
			continue
		}
		if k >= MaxRepescagensPorPasse {
			b.Motivo = fmt.Sprintf("acima do limite de %d repescagens por passe", MaxRepescagensPorPasse)
			res.Restantes = append(res.Restantes, b)
			continue
		}
		ok, motivo := false, ""
		for tent := 1; tent <= 1+TentativasExtrasRepescagem && !ok; tent++ {
			as, es, err := m.regrava(ctx, p, a, b)
			var fase *replay.ErroFase
			switch {
			case err != nil && (ctx.Err() != nil || errors.As(err, &fase)):
				if ctx.Err() != nil {
					err = context.Cause(ctx)
				}
				return res, err
			case err != nil:
				motivo = "a repescagem parou: " + err.Error()
				parou = motivo
			default:
				fechouAgora, pior := fechou(as, b, LimiarRepescagemMs)
				if fechouAgora {
					if err := substituiTrecho(m.db, p.ID, p.GameID, a.pid, as, es); err != nil {
						motivo = "gravar o trecho novo: " + err.Error()
					} else {
						ok = true
					}
				} else {
					motivo = fmt.Sprintf("o trecho regravado ainda ficou %.1f s sem a câmera no jogador", float64(pior)/1000)
				}
			}
			ev := map[string]any{"passe_id": p.ID, "participante_id": a.pid, "n": k + 1, "de_ms": b.DeMs, "ate_ms": b.AteMs,
				"tentativa": tent, "ok": ok}
			if !ok {
				ev["motivo"] = motivo
			}
			m.ex.Emite("repescagem_trecho", ev)
			if parou != "" {
				break
			}
		}
		if ok {
			res.Recuperados++
		} else {
			b.Motivo = motivo
			res.Restantes = append(res.Restantes, b)
		}
	}
	res.Sobraram = len(res.Restantes)
	res.FeitaEm = banco.Agora()
	m.ex.Emite("repescagem_fim", map[string]any{"passe_id": p.ID, "participante_id": a.pid, "buracos": res.Buracos,
		"recuperados": res.Recuperados, "sobraram": res.Sobraram, "restantes": res.Restantes, "quando": quando})
	return res, nil
}

// regrava segue o jogador de b.DeMs-FolgaRepescagemMs até b.AteMs+FolgaRepescagemMs
// e devolve o que leu (sem gravar: quem decide se troca é repesca).
func (m *motor) regrava(ctx context.Context, p Passe, a alvo, b Buraco) ([]Amostra, []EstadoJogador, error) {
	ini := max(0, b.DeMs-FolgaRepescagemMs)
	fim := b.AteMs + FolgaRepescagemMs
	if d := int64(math.Round(m.s.Duracao * 1000)); d > 0 && fim > d {
		fim = d
	}
	var as []Amostra
	var es []EstadoJogador
	pr := progresso{TAtualMs: ini}
	_, err := m.segue(ctx, trecho{p: p, a: a, inicio: ini, fim: fim, pr: &pr, repescagem: true,
		destino: func(x []Amostra, y []EstadoJogador, _ progresso) error {
			as = append(as, x...)
			es = append(es, y...)
			return nil
		}})
	return as, es, err
}

// repescaNoFim é a repescagem ao fim do passe, antes de fechá-lo: acha os
// buracos nas linhas de trabalho e tenta fechar. Grava o resumo no passe.
func (m *motor) repescaNoFim(ctx context.Context, p Passe, a alvo) (ResumoRepescagem, error) {
	as, err := leBrutas(m.db, p.ID, math.MinInt64, math.MaxInt64)
	if err != nil {
		return ResumoRepescagem{}, err
	}
	bs := Buracos(as, LimiarRepescagemMs)
	res := ResumoRepescagem{Quando: "fim_do_passe", Restantes: []Buraco{}, FeitaEm: banco.Agora()}
	if len(bs) > 0 {
		if res, err = m.repesca(ctx, p, a, bs, "fim_do_passe"); err != nil {
			return res, err
		}
	}
	if err := gravaResumoRepescagem(m.db, p.ID, res); err != nil {
		log.Printf("rastreio: gravar o resumo da repescagem do passe %d: %v", p.ID, err)
	}
	return res, nil
}
