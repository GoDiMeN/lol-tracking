package analise

import (
	"fmt"
	"math"
	"sort"
)

// As etiquetas de atividade que dependem de mais que a amostra do instante:
// recall (o salto para a fonte no fim de um trecho parado), dive (torre
// inimiga de pé), split push (aliados longe) e agrupado (o time junto). A
// regra e a ordem de prioridade estão em classifica (geradores.go) e em
// docs/arquitetura/dados.md, seção Atividade.

// --- recall ------------------------------------------------------------------

// intervaloRecall: a canalização [ini, chegada) que terminou com o jogador na
// fonte do próprio time.
type intervaloRecall struct {
	ini, fimParado, chegada int64
	ponto                   Ponto
}

// distanciaSaltoRecall e o raio da fonte são os mesmos do item "recall" da
// cadeia (rastreio/cadeia.go): salto de mais de 3000 em menos de 3 s, chegando
// a menos de RaioFonte da fonte do time.
const distanciaSaltoRecall = 3000.0

// intervalosRecall acha cada salto vivo para a própria fonte e, olhando para
// trás, o trecho parado que o antecede (a canalização). Salto sem trecho parado
// de pelo menos CanalizacaoMinimaMs antes não é recall que dê para etiquetar.
// A canalização interrompida (saiu andando ou não foi para a base) não tem
// salto e por isso nunca vira recall.
func (p *preparo) intervalosRecall() []intervaloRecall {
	if p.recallsOK {
		return p.recalls
	}
	p.recallsOK = true
	as := p.c.Amostras
	temPos := func(k int) bool { return !as[k].Morto && as[k].X != nil && as[k].Y != nil }
	pt := func(k int) Ponto { return Ponto{*as[k].X, *as[k].Y} }
	ant := -1 // última amostra viva com posição
	for i := range as {
		a := as[i]
		if a.Morto {
			ant = -1
			continue
		}
		if a.X == nil || a.Y == nil {
			continue
		}
		b := Ponto{*a.X, *a.Y}
		// Chegada à fonte vindo de fora dela. A câmera às vezes grava uma amostra no meio do
		// caminho (Orianna, 7:47 da 3304217746: 9612 → 12006 → 14334 em meio segundo): o ponto
		// de partida é a última amostra PARADA (a menos de RaioCanalizacao da anterior) até 3 s
		// antes, não a amostra imediatamente anterior.
		// A chegada é a da definição única (rastreio.ChegadasPorRecall); aqui só se
		// procura, para trás, a canalização que a antecede.
		if ant >= 0 && p.chegadasRecall()[i] {
			j := -1
			for k := ant; k > 0 && a.TMs-as[k].TMs < 3000; k-- {
				if as[k].Morto {
					break
				}
				if temPos(k) && temPos(k-1) && dist(pt(k), pt(k-1)) <= Param.RaioCanalizacao && dist(pt(k), b) > distanciaSaltoRecall {
					j = k
					break
				}
			}
			if j < 0 {
				ant = i
				continue
			}
			q := pt(j)
			{
				ini := as[j].TMs
				for k := j; k >= 0; k-- {
					s := as[k]
					if s.Morto || s.X == nil || s.Y == nil || dist(Ponto{*s.X, *s.Y}, q) > Param.RaioCanalizacao ||
						as[j].TMs-s.TMs > Param.DuracaoRecallMs+500 {
						break
					}
					ini = s.TMs
				}
				if as[j].TMs-ini >= Param.CanalizacaoMinimaMs {
					p.recalls = append(p.recalls, intervaloRecall{ini: ini, fimParado: as[j].TMs, chegada: a.TMs, ponto: q})
				}
			}
		}
		ant = i
	}
	return p.recalls
}

// recallEm: o rótulo "recall" se t está numa canalização que terminou na fonte.
func (p *preparo) recallEm(t int64, zona string) (rotuloAtv, bool) {
	for _, r := range p.intervalosRecall() {
		if t >= r.ini && t < r.chegada {
			return rotuloAtv{atv: ARecall, confianca: "alta", zona: zona, sinais: []string{
				fmt.Sprintf("parado %s s em (%.0f, %.0f)", segundosDec(r.fimParado-r.ini), r.ponto.X, r.ponto.Y),
				fmt.Sprintf("surgiu na fonte aos %s", relogioDec(r.chegada))}}, true
		}
	}
	return rotuloAtv{}, false
}

// --- torres (dive) -------------------------------------------------------------

// torreComQueda é uma torre que atira (as três de cada rota e as duas do
// nexo) com o instante em que caiu (caiu falso = de pé até o fim).
type torreComQueda struct {
	TorreDaRota
	queda int64
	caiu  bool
}

// TorresDoNexo: as duas torres do nexo de cada time, pelas posições dos
// BUILDING_KILL (NEXUS_TURRET) da partida 3304217746 para o vermelho; as do azul
// pelo espelho (Simetria) arredondado às posições da Riot.
var TorresDoNexo = []TorreDaRota{
	{ZRotaMeio, 100, 4, Ponto{1748, 2270}}, {ZRotaMeio, 100, 4, Ponto{2177, 1807}},
	{ZRotaMeio, 200, 4, Ponto{12611, 13084}}, {ZRotaMeio, 200, 4, Ponto{13052, 12612}},
}

// torresDaPartida: as torres com a queda tirada dos BUILDING_KILL de torre
// (casados pela posição, a torre mais perto a até 1000).
func (p *preparo) torresDaPartida() []torreComQueda {
	if p.torresOK {
		return p.torres
	}
	p.torresOK = true
	for _, t := range append(append([]TorreDaRota(nil), TorresDasRotas...), TorresDoNexo...) {
		p.torres = append(p.torres, torreComQueda{TorreDaRota: t})
	}
	for _, e := range p.eventos {
		if e.Tipo != "BUILDING_KILL" || e.X == nil || e.Y == nil {
			continue
		}
		pe := Ponto{*e.X, *e.Y}
		melhor, k := 1000.0, -1
		for i, t := range p.torres {
			if d := dist(pe, t.P); d < melhor {
				melhor, k = d, i
			}
		}
		if k >= 0 && !p.torres[k].caiu {
			p.torres[k].queda, p.torres[k].caiu = e.TMs, true
		}
	}
	return p.torres
}

// nomeTorre: "torre externa do topo", "torre do nexo".
func nomeTorre(t TorreDaRota) string {
	if t.Ordem == 4 {
		return "torre do nexo"
	}
	ordem := map[int]string{1: "externa", 2: "interna", 3: "do inibidor"}[t.Ordem]
	rota := map[string]string{ZRotaTopo: "do topo", ZRotaMeio: "do meio", ZRotaBaixo: "de baixo"}[t.Rota]
	return "torre " + ordem + " " + rota
}

// torreInimigaPerto: a torre inimiga de pé em t mais perto do ponto, a até
// AlcanceTorre.
func (p *preparo) torreInimigaPerto(pt Ponto, t int64) (TorreDaRota, float64, bool) {
	melhor, achou, ok := Param.AlcanceTorre, TorreDaRota{}, false
	for _, tr := range p.torresDaPartida() {
		if tr.Time == p.c.Time || (tr.caiu && tr.queda <= t) {
			continue
		}
		if d := dist(pt, tr.P); d <= melhor {
			melhor, achou, ok = d, tr.TorreDaRota, true
		}
	}
	return achou, melhor, ok
}

// talvezDive troca a luta ou troca por "dive" se o jogador está dentro do
// alcance de uma torre inimiga de pé. `com` fica só com os inimigos (em quem ele
// mergulha); o alvo é a torre.
func (p *preparo) talvezDive(r rotuloAtv, pt Ponto, t int64) rotuloAtv {
	tr, d, ok := p.torreInimigaPerto(pt, t)
	if !ok {
		return r
	}
	var inimigos []int
	for _, pid := range r.com {
		if contem(p.inimigos, pid) {
			inimigos = append(inimigos, pid)
		}
	}
	if len(inimigos) == 0 {
		return r
	}
	r.atv, r.com, r.alvo = ADive, inimigos, nomeTorre(tr)
	r.sinais = append([]string{fmt.Sprintf("a %.0f da %s inimiga, de pé", d, nomeTorre(tr))}, r.sinais...)
	return r
}

// --- aliados: vivo/morto e posição ----------------------------------------------

// aliadoMorto: o aliado estava morto em t (amostras do passe dele ou o estado
// dos 10).
func (p *preparo) aliadoMorto(pid int, t int64) bool {
	if a := AmostraEm(p.c.Outros[pid], t, 1500); a != nil {
		return a.Morto
	}
	if e := EstadoEm(p.estados[pid], t, 2000); e != nil {
		return e.Morto
	}
	return false
}

// --- split push -------------------------------------------------------------------

// alemDoEncontro: o ponto da rota lateral está do lado inimigo do ponto de
// encontro das torres externas (o rio cruza as rotas laterais ali).
func alemDoEncontro(zona string, pt Ponto, time int) bool {
	e := NovoEixo(zona)
	s, _ := e.S(pt)
	if time == 200 {
		return s < e.Encontro
	}
	return s > e.Encontro
}

// talvezSplit troca "farmando"/"torre" por "split_push" quando, depois da fase
// de rotas, ele está sozinho numa rota lateral além do encontro para o lado
// inimigo e pelo menos três aliados estão mortos ou longe.
func (p *preparo) talvezSplit(r rotuloAtv, pt Ponto, t int64) rotuloAtv {
	if t < p.fimDaFaseDeRotas() || (r.zona != ZRotaTopo && r.zona != ZRotaBaixo) || !alemDoEncontro(r.zona, pt, p.c.Time) {
		return r
	}
	longe := 0
	for _, pid := range p.aliados {
		if p.aliadoMorto(pid, t) {
			longe++
			continue
		}
		q, ok := p.posicaoDe(pid, t)
		if !ok {
			continue // sem posição: não dá para dizer que está longe
		}
		d := dist(pt, q)
		if d < Param.AlcanceLuta {
			return r // não está sozinho
		}
		if d > Param.DistanciaSplitAliados {
			longe++
		}
	}
	if longe < 3 {
		return r
	}
	era := r.atv
	r.atv = ASplitPush
	r.sinais = append(r.sinais, fmt.Sprintf("sozinho além do rio na %s (era %s)", Info(r.zona).Nome, era),
		fmt.Sprintf("%d aliados mortos ou a mais de %.0f", longe, Param.DistanciaSplitAliados))
	return r
}

// --- agrupado ----------------------------------------------------------------------

// agrupado: ele e pelo menos três aliados vivos, todos a até DistanciaAgrupado
// uns dos outros. Devolve os aliados do maior grupo (em ordem).
func (p *preparo) agrupado(pt Ponto, t int64) ([]int, bool) {
	type viz struct {
		pid int
		q   Ponto
	}
	var cand []viz
	for _, pid := range p.aliados {
		if q, ok := p.posicaoDe(pid, t); ok && dist(pt, q) <= Param.DistanciaAgrupado {
			cand = append(cand, viz{pid, q})
		}
	}
	if len(cand) < 3 {
		return nil, false
	}
	// No máximo 4 candidatos: testa os subconjuntos do maior para o menor.
	var melhor []int
	n := len(cand)
	for m := 1; m < 1<<n; m++ {
		var sel []viz
		for i := 0; i < n; i++ {
			if m&(1<<i) != 0 {
				sel = append(sel, cand[i])
			}
		}
		if len(sel) < 3 || len(sel) <= len(melhor) {
			continue
		}
		ok := true
		for a := 0; a < len(sel) && ok; a++ {
			for b := a + 1; b < len(sel); b++ {
				if dist(sel[a].q, sel[b].q) > Param.DistanciaAgrupado {
					ok = false
					break
				}
			}
		}
		if ok {
			melhor = melhor[:0]
			for _, s := range sel {
				melhor = append(melhor, s.pid)
			}
		}
	}
	if len(melhor) < 3 {
		return nil, false
	}
	sort.Ints(melhor)
	return melhor, true
}

// --- selva inimiga ------------------------------------------------------------------

// selvaInimiga: a zona é a selva do outro time.
func (p *preparo) selvaInimiga(zona string) bool {
	if TipoZona(zona) != "selva" {
		return false
	}
	meu := "azul"
	if p.c.Time == 200 {
		meu = "vermelho"
	}
	return Info(zona).Lado != meu
}

// metadeInimiga: o ponto está na metade do mapa do outro time (pelo rio).
func metadeInimiga(pt Ponto, time int) bool {
	if math.IsNaN(pt.X) {
		return false
	}
	azul := ladoAzulDoRio(pt)
	if time == 200 {
		return azul
	}
	return !azul
}
