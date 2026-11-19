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
	// VisForaDoMapa: o JOGO tirou o campeão do mapa (Investida Alfa do Master
	// Yi, teleporte em trânsito, montado no Arauto): não é alvo de visão, e
	// não é "não deu para saber". Vem do passe do próprio jogador (trava
	// perdida com a seleção nele, foraDoMapa em visao_arquivo.go); na
	// 3305449546 o anel do Yi some do minimapa do próprio time nesses
	// instantes (anéis − aliados vivos = 0 em 3% das capturas, contra 29% com
	// ele à vista).
	VisForaDoMapa = "fora_do_mapa"
)

// TimeDesenhadoPorCima: no minimapa, os retratos do vermelho (200) são
// desenhados por cima dos do azul, nas DUAS visões. Medido na 3305449546
// (descobertas 9.2): retrato azul com um vermelho à vista a menos de 14 px é
// achado em 10% das capturas (visão azul, 756) e o vermelho ao lado de um
// azul em 81%; na visão vermelha, o vermelho ao lado de um azul à vista é
// achado em 100% (72) e o azul ao lado de um vermelho em 8% (917). Dentro do
// mesmo time a ordem não é firme (o de número maior fica por cima em 63–97%
// dos pares, não em todos): ali qualquer um pode cobrir o outro.
const TimeDesenhadoPorCima = 200

// podeCobrir: o retrato de um campeão do time de pode ficar por cima do de
// um do time sob.
func podeCobrir(de, sob int) bool {
	return de == sob || (de == TimeDesenhadoPorCima && sob != TimeDesenhadoPorCima)
}

// Tolerâncias, em pixels do minimapa (1 px ≈ 44 unidades do mapa).
const (
	// ToleranciaRetratoPx: até onde um anel achado conta como o retrato do
	// jogador. Soma o erro da geometria (≤ 2 px nas capturas de 2026-11-17), o
	// da posição rastreada (mediana 17–30 unidades, máximo ~100 ≈ 2,3 px) e o
	// do instante da captura (±0,1 s de jogo a 8×, ~1 px a 400 unidades/s).
	ToleranciaRetratoPx = 6.0
	// OclusaoPx: outro retrato (achado, ou de um aliado do observador, que é
	// sempre desenhado) a menos disto do lugar do jogador pode esconder o anel
	// dele (o de cima esconde o de baixo). Era 12; na 3305449546 (47 mil
	// aliados conferidos, descobertas 9) o anel de quem tem outro campeão a
	// 12–16 px é achado em 67–81% das vezes, a 16–20 px em 77–90% e a mais de
	// 20 px em 94–98%: abaixo de 20 px a falta do anel não prova "fora".
	OclusaoPx = 20.0
	// SobrepostoPx: aliado com outro campeão vivo a menos disto não entra na
	// conferência dos aliados (o anel dele pode estar embaixo do outro; mesma
	// medida de OclusaoPx).
	SobrepostoPx = 20.0
	// FonteConferencia: aliado a menos disto (unidades) da própria fonte não
	// entra na conferência: ali a posição rastreada é a fonte fixa (origem
	// "base", logo depois de renascer) e não a de verdade, e os retratos se
	// juntam; na 3305449546, 48–66% achados.
	FonteConferencia = 2500.0
	// ArcoExplicado: fração mínima das direções que faltam num arco que cai
	// embaixo de um retrato que pode estar por cima (o resto é o erro da
	// posição e do desenho). Abaixo disto o arco não vale (o círculo de um
	// inibidor tem a falta em lugar nenhum de retrato).
	ArcoExplicado = 0.85
	// ArcoCobridorPx: a falta numa direção é explicada se o ponto do anel
	// nela fica a menos disto do centro de um retrato por cima (o disco do
	// retrato com o anel tem ~15,5 px; mais 1,5 de folga).
	ArcoCobridorPx = 17.0
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
	// ForaDoMapa: sem posição porque o jogo o tirou do mapa (VisForaDoMapa).
	ForaDoMapa bool
}

// ClassificacaoCaptura é o resultado de uma captura.
type ClassificacaoCaptura struct {
	// Estados dos inimigos vivos do observador (participante → estado).
	Estados map[int]string
	// Motivos dos indeterminados (participante → motivo curto).
	Motivos map[int]string
	// Aliados conferíveis (vivos, com posição, longe da própria fonte e sem
	// outro campeão a menos de SobrepostoPx) e quantos deles têm o retrato
	// achado (no próprio time o retrato aparece sempre: é a conferência da
	// leitura, da geometria e da posição rastreada).
	AliadosVivos, AliadosAchados int
	// Sobras: anéis inimigos que não casaram com nenhum inimigo.
	Sobras int
	// PeloArco: inimigos dados como visíveis por um arco (anel parcial com a
	// falta embaixo de um retrato por cima), não por um anel inteiro.
	PeloArco map[int]bool
	// PorOrdem: inimigos dados como "fora" só porque o retrato encostado é do
	// time desenhado por baixo (TimeDesenhadoPorCima); antes da regra eram
	// "coberto". A continuidade desfaz esse "fora" quando ele cai entre dois
	// visíveis (desfazForaPorOrdem em visao_arquivo.go).
	PorOrdem map[int]bool
}

// ClassificaCaptura decide a visibilidade de cada inimigo do observador
// (100/200) numa captura. modoOK diz se o painel conferiu a névoa do time.
func ClassificaCaptura(lt LeituraMinimapa, modoOK bool, observador int, js []PosicaoNoInstante) ClassificacaoCaptura {
	c := ClassificacaoCaptura{Estados: map[int]string{}, Motivos: map[int]string{}, PeloArco: map[int]bool{}, PorOrdem: map[int]bool{}}
	var inimigos []PosicaoNoInstante
	// Retratos que podem cobrir o de um inimigo: os achados e os dos aliados
	// do observador (desenhados sempre, mesmo quando a leitura não os achou).
	cobrem := append([]MarcaMinimapa(nil), lt.Retratos...)
	for _, j := range js {
		if j.Morto {
			continue // morto não conta (nem aparece no minimapa)
		}
		if j.Time == observador {
			if j.TemPosicao {
				cobrem = append(cobrem, MarcaMinimapa{Time: j.Time, X: j.X, Y: j.Y})
				if conferivel(j, js) {
					c.AliadosVivos++
					if _, ok := maisPerto(lt.Retratos, observador, j.X, j.Y, ToleranciaRetratoPx); ok {
						c.AliadosAchados++
					}
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
	arcoUsado := map[int]bool{}
	for ji, j := range inimigos {
		id := j.ParticipanteID
		switch {
		case !j.TemPosicao && j.ForaDoMapa:
			c.Estados[id] = VisForaDoMapa
			continue
		case !j.TemPosicao:
			c.Estados[id], c.Motivos[id] = VisIndeterminado, "sem_posicao"
			continue
		case hasKey(jogAnel, ji):
			c.Estados[id] = VisVisivel
			continue
		}
		// Só pode esconder o anel dele quem é desenhado por cima.
		var porCima []MarcaMinimapa
		embolado := emboladoComInimigos(j, js, observador)
		for _, m := range cobrem {
			if embolado || podeCobrir(m.Time, j.Time) {
				porCima = append(porCima, m)
			}
		}
		if ai, ok := arcoDoJogador(lt.Arcos, arcoUsado, j, porCima); ok {
			arcoUsado[ai] = true
			c.Estados[id], c.PeloArco[id] = VisVisivel, true
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
		case pertoDeAlgum(porCima, j.X, j.Y, OclusaoPx):
			motivo = "coberto"
		case naBorda(j.X, j.Y):
			motivo = "borda"
		}
		if motivo != "" {
			c.Estados[id], c.Motivos[id] = VisIndeterminado, motivo
		} else {
			c.Estados[id] = VisFora
			c.PorOrdem[id] = pertoDeAlgum(cobrem, j.X, j.Y, OclusaoPx)
		}
	}
	return c
}

func hasKey(m map[int]int, k int) bool { _, ok := m[k]; return ok }

// emboladoComInimigos: além dos aliados do observador, há outro campeão do
// time de j (vivo, com posição) a menos de OclusaoPx dele.
func emboladoComInimigos(j PosicaoNoInstante, js []PosicaoNoInstante, observador int) bool {
	for _, o := range js {
		if o.ParticipanteID != j.ParticipanteID && o.Time != observador && !o.Morto && o.TemPosicao &&
			replay.DistanciaMinimapa(o.X, o.Y, j.X, j.Y) < OclusaoPx {
			return true
		}
	}
	return false
}

// arcoDoJogador procura, entre os arcos do time de j ainda não usados, um a
// até ToleranciaRetratoPx do lugar dele cuja falta caia (ArcoExplicado) embaixo
// dos retratos que podem estar por cima (porCima). Devolve o índice.
func arcoDoJogador(arcos []MarcaMinimapa, usados map[int]bool, j PosicaoNoInstante, porCima []MarcaMinimapa) (int, bool) {
	melhor, dMelhor := -1, math.Inf(1)
	for ai, a := range arcos {
		if usados[ai] || a.Time != j.Time || a.Arco == 0 || a.Cobertura < 0.3 {
			continue
		}
		d := replay.DistanciaMinimapa(a.X, a.Y, j.X, j.Y)
		if d > ToleranciaRetratoPx || d >= dMelhor || !faltaExplicada(a, porCima) {
			continue
		}
		melhor, dMelhor = ai, d
	}
	return melhor, melhor >= 0
}

// faltaExplicada: das direções em que o anel do arco não apareceu, ao menos
// ArcoExplicado têm o ponto do anel embaixo de um retrato de porCima.
func faltaExplicada(a MarcaMinimapa, porCima []MarcaMinimapa) bool {
	ax, ay := replay.MapaParaMinimapa(a.X, a.Y, replay.LarguraRef, replay.AlturaRef)
	var centros [][2]float64
	for _, m := range porCima {
		if replay.DistanciaMinimapa(m.X, m.Y, a.X, a.Y) < 1 {
			continue // o próprio arco não se cobre
		}
		mx, my := replay.MapaParaMinimapa(m.X, m.Y, replay.LarguraRef, replay.AlturaRef)
		centros = append(centros, [2]float64{mx, my})
	}
	faltam, explicadas := 0, 0
	for k := 0; k < DirecoesArco; k++ {
		if a.Arco&(1<<k) != 0 {
			continue
		}
		faltam++
		ang := 2 * math.Pi * float64(k) / DirecoesArco
		px, py := ax+replay.RaioRetratoMinimapa*math.Cos(ang), ay+replay.RaioRetratoMinimapa*math.Sin(ang)
		for _, c := range centros {
			if math.Hypot(px-c[0], py-c[1]) < ArcoCobridorPx {
				explicadas++
				break
			}
		}
	}
	return faltam > 0 && float64(explicadas) >= ArcoExplicado*float64(faltam)
}

// conferivel diz se o aliado j entra na conferência dos aliados: longe da
// própria fonte e sem outro campeão vivo com posição a menos de SobrepostoPx
// (inimigo também: se estiver à vista, o retrato dele pode estar por cima).
func conferivel(j PosicaoNoInstante, js []PosicaoNoInstante) bool {
	if j.Time == 100 || j.Time == 200 {
		f := FonteDoTime(j.Time)
		if math.Hypot(j.X-f[0], j.Y-f[1]) < FonteConferencia {
			return false
		}
	}
	for _, o := range js {
		if o.ParticipanteID != j.ParticipanteID && !o.Morto && o.TemPosicao && replay.DistanciaMinimapa(o.X, o.Y, j.X, j.Y) < SobrepostoPx {
			return false
		}
	}
	return true
}

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
