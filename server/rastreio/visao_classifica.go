package rastreio

import (
	"math"
	"sort"

	"loltracking/server/replay"
)

// A decisão "o time X enxerga o jogador Y agora?" a partir de uma captura do
// minimapa na visão de X (LeituraMinimapa) e das posições rastreadas dos 10
// no mesmo instante. O jogo não informa visibilidade (o playerlist não tem
// campo de visível): o que existe é o minimapa, que na visão de um time só
// desenha o retrato de um inimigo quando o time o enxerga. Princípio do pacote
// visao: melhor "indeterminado" que errado.

// Estados de visibilidade de um inimigo para o time observador.
const (
	VisVisivel       = "visivel"
	VisFora          = "fora"
	VisIndeterminado = "indeterminado"
)

// Tolerâncias, em pixels do minimapa (1 px ≈ 44 unidades do mapa).
const (
	// ToleranciaRetratoPx: até onde um anel achado conta como o retrato do
	// jogador. Soma o erro da geometria (≤ 2 px nas capturas de 2026-11-17), o
	// da posição rastreada (mediana 17–30 unidades, máximo ~100 ≈ 2,3 px) e o
	// do instante da captura (±0,1 s de jogo a 8×, ~1 px a 400 unidades/s).
	ToleranciaRetratoPx = 6.0
	// OclusaoPx: outro retrato achado a menos disto do lugar do jogador cobre
	// boa parte do anel dele (o de cima esconde o de baixo).
	OclusaoPx = 12.0
	// DuvidaPx: um anel inimigo sem dono a menos disto do lugar do jogador
	// pode ser ele mesmo com a posição atrasada: indeterminado.
	DuvidaPx = 25.0
	// BordaPx: perto da borda do quadrado do minimapa o anel sai cortado.
	BordaPx = 8.0
)

// PosicaoNoInstante é o que se sabe de um jogador num instante.
type PosicaoNoInstante struct {
	ParticipanteID int
	Time           int
	// TemPosicao: posição rastreada boa perto do instante (câmera, base ou
	// morte; nada interpolado num buraco).
	TemPosicao bool
	X, Y       float64
	Morto      bool
}

// ClassificacaoCaptura é o resultado de uma captura.
type ClassificacaoCaptura struct {
	// Estados dos inimigos vivos do observador (participante → estado).
	Estados map[int]string
	// Motivos dos indeterminados (participante → motivo curto).
	Motivos map[int]string
	// Aliados vivos com posição e quantos deles têm o retrato achado (no
	// próprio time o retrato aparece sempre: é a conferência da leitura).
	AliadosVivos, AliadosAchados int
	// Sobras: anéis inimigos que não casaram com nenhum inimigo.
	Sobras int
}

// ClassificaCaptura decide a visibilidade de cada inimigo do observador
// (100/200) numa captura. modoOK diz se o painel conferiu a névoa do time.
func ClassificaCaptura(lt LeituraMinimapa, modoOK bool, observador int, js []PosicaoNoInstante) ClassificacaoCaptura {
	c := ClassificacaoCaptura{Estados: map[int]string{}, Motivos: map[int]string{}}
	var inimigos []PosicaoNoInstante
	for _, j := range js {
		if j.Morto {
			continue // morto não conta (nem aparece no minimapa)
		}
		if j.Time == observador {
			if j.TemPosicao {
				c.AliadosVivos++
				if _, ok := maisPerto(lt.Retratos, observador, j.X, j.Y, ToleranciaRetratoPx); ok {
					c.AliadosAchados++
				}
			}
			continue
		}
		inimigos = append(inimigos, j)
	}
	todos := func(motivo string) ClassificacaoCaptura {
		for _, j := range inimigos {
			c.Estados[j.ParticipanteID] = VisIndeterminado
			c.Motivos[j.ParticipanteID] = motivo
		}
		return c
	}
	if !modoOK {
		return todos("painel")
	}
	// Menos da metade dos aliados achados no lugar (com um aliado só: ele não
	// achado): a leitura, a posição rastreada ou o instante estão errados
	// (minimapa coberto, geometria, calibração do passe). É a conferência que
	// pega a posição rastreada errada de propósito: com ela, um inimigo com a
	// posição deslocada sairia "fora" (o anel dele está em outro lugar).
	if c.AliadosVivos >= 1 && c.AliadosAchados*2 < c.AliadosVivos {
		return todos("aliados")
	}

	// Casa anéis inimigos com inimigos com posição, do par mais perto ao mais
	// longe, um anel por jogador.
	var aneis []MarcaMinimapa
	for _, r := range lt.Retratos {
		if r.Time != observador {
			aneis = append(aneis, r)
		}
	}
	type par struct {
		a, j int
		d    float64
	}
	var pares []par
	for ai, a := range aneis {
		for ji, j := range inimigos {
			if !j.TemPosicao {
				continue
			}
			if d := replay.DistanciaMinimapa(a.X, a.Y, j.X, j.Y); d <= ToleranciaRetratoPx {
				pares = append(pares, par{ai, ji, d})
			}
		}
	}
	sort.Slice(pares, func(x, y int) bool { return pares[x].d < pares[y].d })
	anelDono := map[int]int{}
	jogAnel := map[int]int{}
	for _, p := range pares {
		if _, ok := anelDono[p.a]; ok {
			continue
		}
		if _, ok := jogAnel[p.j]; ok {
			continue
		}
		anelDono[p.a], jogAnel[p.j] = p.j, p.a
	}
	semPosicao := 0
	for _, j := range inimigos {
		if !j.TemPosicao {
			semPosicao++
		}
	}
	var sobras []MarcaMinimapa
	for ai, a := range aneis {
		if _, ok := anelDono[ai]; !ok {
			sobras = append(sobras, a)
		}
	}
	c.Sobras = len(sobras)
	for ji, j := range inimigos {
		id := j.ParticipanteID
		switch {
		case !j.TemPosicao:
			c.Estados[id], c.Motivos[id] = VisIndeterminado, "sem_posicao"
			continue
		case hasKey(jogAnel, ji):
			c.Estados[id] = VisVisivel
			continue
		}
		// Sem anel no lugar dele. Antes de dizer "fora da visão":
		motivo := ""
		switch {
		case len(sobras) > semPosicao:
			// Anel inimigo a mais do que os sem posição explicam: é de alguém
			// com posição, deslocado (instante ou posição errados).
			motivo = "anel_sem_dono"
		case pertoDeAlgum(sobras, j.X, j.Y, DuvidaPx):
			motivo = "anel_sem_dono"
		case pertoDeAlgum(lt.Retratos, j.X, j.Y, OclusaoPx):
			motivo = "coberto"
		case naBorda(j.X, j.Y):
			motivo = "borda"
		}
		if motivo != "" {
			c.Estados[id], c.Motivos[id] = VisIndeterminado, motivo
		} else {
			c.Estados[id] = VisFora
		}
	}
	return c
}

func hasKey(m map[int]int, k int) bool { _, ok := m[k]; return ok }

func maisPerto(ms []MarcaMinimapa, time int, x, y, tol float64) (MarcaMinimapa, bool) {
	melhor, d := MarcaMinimapa{}, math.Inf(1)
	for _, m := range ms {
		if m.Time != time {
			continue
		}
		if dd := replay.DistanciaMinimapa(m.X, m.Y, x, y); dd < d {
			melhor, d = m, dd
		}
	}
	return melhor, d <= tol
}

func pertoDeAlgum(ms []MarcaMinimapa, x, y, raio float64) bool {
	for _, m := range ms {
		if replay.DistanciaMinimapa(m.X, m.Y, x, y) < raio {
			return true
		}
	}
	return false
}

// naBorda: o centro do retrato a menos de BordaPx da borda do quadrado do
// minimapa (ou fora dele).
func naBorda(x, y float64) bool {
	px, py := replay.MapaParaMinimapa(x, y, replay.LarguraRef, replay.AlturaRef)
	q := replay.QuadradoMinimapa
	return px < float64(q.Min.X)+BordaPx || px > float64(q.Max.X)-BordaPx || py < float64(q.Min.Y)+BordaPx || py > float64(q.Max.Y)-BordaPx
}
