package analise

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"

	"loltracking/server/rastreio"
)

// Indicadores da visão dos times (passes de visão, rastreio/visao_*.go): do
// ponto de vista de cada jogador rastreado, como o time INIMIGO o via.
//
//   - pct_visivel_inimigo_<fase>: tempo à vista ÷ (à vista + fora da visão) na
//     fase. O indeterminado (retrato coberto, painel não conferido, sem
//     posição) e o fora do mapa (Investida Alfa, teleporte) ficam de fora da
//     conta; com mais de 10% da fase indeterminado, o valor sai aproximado.
//     Fase com menos de 1 min decidido: sem valor.
//   - ganks_fora_da_visao_n (caçador): trechos `gankando` (a aproximação que
//     terminou em luta numa rota, etiquetas.go) em que ele NUNCA foi visto
//     pelo inimigo (VisaoAntes resposta "nao"). Gank com resposta "não dá
//     para saber" não conta nem como visto nem como fora (vai no detalhe e
//     deixa o valor aproximado).
//
// E a pergunta dos abates (VisaoAntes): "o atacante era visto pelo time da
// vítima nos VisaoAntesMs anteriores?", guardada no momento do abate na base
// de fatos (dados.visao_atacantes e dados.visao_cacador).

// Respostas de VisaoAntes.
const (
	VistoSim     = "sim"
	VistoNao     = "nao"
	VistoNaoSabe = "nao_sabe"
)

// VisaoAntes responde se o jogador era visto pelo time observador numa
// janela (antes de um abate, ou durante a aproximação de um gank).
type VisaoAntes struct {
	ParticipanteID int `json:"participante_id"`
	// TimeObservador: o time que via (ou não) o jogador.
	TimeObservador int   `json:"time_observador"`
	DeMs           int64 `json:"de_ms"`
	AteMs          int64 `json:"ate_ms"`
	// Resposta: "sim" (visto em algum instante da janela), "nao" (nunca
	// visto, com leitura de fora em VisaoCoberturaNao do tempo no mapa) ou
	// "nao_sabe".
	Resposta string `json:"resposta"`
	// VistoDesdeMs: o primeiro instante visto na janela; UltimoVistoMs, o
	// último (nulos se não visto). VistoInferido: todo o tempo visto na janela
	// foi herdado pela continuidade, nenhum anel lido.
	VistoDesdeMs  *int64 `json:"visto_desde_ms"`
	UltimoVistoMs *int64 `json:"ultimo_visto_ms"`
	VistoInferido bool   `json:"visto_inferido"`
	// Tempos na janela (ms): visível e fora lidos (firmes), herdados
	// (inferidos), indeterminado, fora do mapa e sem leitura (morto, sem
	// captura).
	VisivelMs       int64 `json:"visivel_ms"`
	ForaMs          int64 `json:"fora_ms"`
	InferidoMs      int64 `json:"inferido_ms"`
	IndeterminadoMs int64 `json:"indeterminado_ms"`
	ForaDoMapaMs    int64 `json:"fora_do_mapa_ms"`
	// Cobertura: fração (0–1) do tempo dele no mapa com captura que teve
	// leitura firme (visível ou fora lidos); CoberturaComInferido soma o
	// herdado.
	Cobertura            float64 `json:"cobertura"`
	CoberturaComInferido float64 `json:"cobertura_com_inferido"`
	// ChegadaMs e AntesDaChegada (só nos abates, com as posições do atacante
	// e da vítima): o começo do último trecho em que ele esteve a até
	// Param.AlcanceLuta da vítima antes do abate, e a mesma pergunta na
	// janela até ali. Nos 15 s antes de um abate o atacante quase sempre
	// aparece (está lutando): "veio sem ser visto" é o AntesDaChegada.
	ChegadaMs      *int64      `json:"chegada_ms,omitempty"`
	AntesDaChegada *VisaoAntes `json:"antes_da_chegada,omitempty"`
}

// ChegadaMinimaMs: com menos que isto entre o começo da janela e a chegada,
// não há "antes da chegada" (ele já estava em cima da vítima).
const ChegadaMinimaMs int64 = 2000

// chegada: o começo do trecho contínuo, terminando em t, em que a e v
// estiveram a até Param.AlcanceLuta (passos de 250 ms, até de). ok falso sem
// as duas posições em t.
func (d *dadosPartida) chegada(a, v int, de, t int64) (int64, bool) {
	perto := func(x int64) (bool, bool) {
		pa, ok1 := posicaoAmostra(d.amostras[a], x)
		pv, ok2 := posicaoAmostra(d.amostras[v], x)
		if !ok1 || !ok2 {
			return false, false
		}
		return math.Hypot(pa.X-pv.X, pa.Y-pv.Y) <= Param.AlcanceLuta, true
	}
	ref := t - 500 // o instante do abate: a vítima já pode estar marcada morta
	if p, ok := perto(ref); !ok || !p {
		return 0, false
	}
	c := ref
	for x := ref - 250; x >= de; x -= 250 {
		p, ok := perto(x)
		if !ok || !p {
			break
		}
		c = x
	}
	return c, true
}

// posicaoAmostra: a posição gravada mais perto de t (a até 1 s).
func posicaoAmostra(as []rastreio.Amostra, t int64) (Ponto, bool) {
	i := sort.Search(len(as), func(k int) bool { return as[k].TMs >= t })
	melhor, dm := -1, int64(1001)
	for _, k := range []int{i - 1, i} {
		if k < 0 || k >= len(as) || as[k].X == nil || as[k].Y == nil {
			continue
		}
		if dd := max(as[k].TMs-t, t-as[k].TMs); dd < dm {
			melhor, dm = k, dd
		}
	}
	if melhor < 0 {
		return Ponto{}, false
	}
	return Ponto{X: *as[melhor].X, Y: *as[melhor].Y}, true
}

// visaoDoAtacante: visaoAntes na janela [de, t) e, com a chegada à vítima,
// a mesma pergunta antes dela.
func (d *dadosPartida) visaoDoAtacante(vt rastreio.VisaoDoTime, a, vitima int, de, t int64) (VisaoAntes, bool) {
	r, ok := visaoAntes(vt, a, de, t)
	if !ok {
		return r, false
	}
	if c, ok := d.chegada(a, vitima, de, t); ok {
		r.ChegadaMs = &c
		if c-de >= ChegadaMinimaMs {
			if ac, ok := visaoAntes(vt, a, de, c); ok {
				r.AntesDaChegada = &ac
			}
		}
	}
	return r, true
}

// visaoAntes calcula a VisaoAntes do jogador pid para o observador vt na
// janela [de, ate).
func visaoAntes(vt rastreio.VisaoDoTime, pid int, de, ate int64) (VisaoAntes, bool) {
	j, ok := vt.Jogador(pid)
	if !ok || ate <= de {
		return VisaoAntes{}, false
	}
	r := VisaoAntes{ParticipanteID: pid, TimeObservador: vt.Time, DeMs: de, AteMs: ate, Resposta: VistoNaoSabe}
	visTot, foraTot := somaNaJanela(j.Visivel, de, ate), somaNaJanela(j.Fora, de, ate)
	visInf := somaInterseccao(j.Visivel, j.Inferido, de, ate)
	foraInf := somaInterseccao(j.Fora, j.Inferido, de, ate)
	r.VisivelMs, r.ForaMs, r.InferidoMs = visTot-visInf, foraTot-foraInf, visInf+foraInf
	r.IndeterminadoMs = somaNaJanela(j.Indeterminado, de, ate)
	r.ForaDoMapaMs = somaNaJanela(j.ForaDoMapa, de, ate)
	noMapa := visTot + foraTot + r.IndeterminadoMs
	if noMapa > 0 {
		r.Cobertura = arred3(float64(r.VisivelMs+r.ForaMs) / float64(noMapa))
		r.CoberturaComInferido = arred3(float64(visTot+foraTot) / float64(noMapa))
	}
	if a, b, ok := primeiroEUltimo(j.Visivel, de, ate); ok {
		r.VistoDesdeMs, r.UltimoVistoMs = &a, &b
		r.VistoInferido = r.VisivelMs == 0
		r.Resposta = VistoSim
		return r, true
	}
	// Sem leitura bastante da janela (morto, sem captura): não dá para saber.
	if float64(noMapa+r.ForaDoMapaMs) < Param.VisaoNoMapaMinimo*float64(ate-de) {
		return r, true
	}
	if noMapa > 0 && float64(foraTot) >= Param.VisaoCoberturaNao*float64(noMapa) {
		r.Resposta = VistoNao
	}
	return r, true
}

func arred3(v float64) float64 { return float64(int64(v*1000+0.5)) / 1000 }

// somaInterseccao: quanto de is ∩ inf cai em [de, ate) (os dois em ordem).
func somaInterseccao(is, inf [][2]int64, de, ate int64) int64 {
	var s int64
	for _, a := range is {
		for _, b := range inf {
			x, y := max(a[0], b[0], de), min(a[1], b[1], ate)
			if y > x {
				s += y - x
			}
		}
	}
	return s
}

// primeiroEUltimo: o primeiro e o último instante de is dentro de [de, ate).
func primeiroEUltimo(is [][2]int64, de, ate int64) (int64, int64, bool) {
	var a, b int64
	ok := false
	for _, i := range is {
		x, y := max(i[0], de), min(i[1], ate)
		if y <= x {
			continue
		}
		if !ok {
			a = x
		}
		b, ok = y, true
	}
	return a, b, ok
}

// TextoVisaoAntes: a frase curta da resposta, para a IA e o contexto
// ("Master Yi visto pelo vermelho desde 5:40 (12 s antes) ...").
func TextoVisaoAntes(v VisaoAntes, nome string, refMs int64) string {
	s := textoVisaoJanela(v, nome, refMs)
	if v.ChegadaMs != nil {
		s += fmt.Sprintf("; chegou a até %.0f da vítima aos %s", Param.AlcanceLuta, MinSeg(*v.ChegadaMs))
		if a := v.AntesDaChegada; a != nil {
			switch a.Resposta {
			case VistoSim:
				s += fmt.Sprintf(" e ANTES disso já tinha sido visto (desde %s)", MinSeg(*a.VistoDesdeMs))
			case VistoNao:
				s += fmt.Sprintf(" e antes disso NÃO foi visto (%s a %s, leitura firme em %.0f%%): veio sem ser visto",
					MinSeg(a.DeMs), MinSeg(a.AteMs), a.Cobertura*100)
			default:
				s += fmt.Sprintf(" e antes disso não dá para saber (leitura firme em %.0f%%)", a.Cobertura*100)
			}
		}
	}
	return s
}

func textoVisaoJanela(v VisaoAntes, nome string, refMs int64) string {
	time := NomeTime(v.TimeObservador)
	cob := fmt.Sprintf("leitura firme em %.0f%% do tempo dele no mapa", v.Cobertura*100)
	if v.InferidoMs > 0 {
		cob += fmt.Sprintf(", %.0f%% com o herdado", v.CoberturaComInferido*100)
	}
	switch v.Resposta {
	case VistoSim:
		s := fmt.Sprintf("%s visto pelo %s desde %s (%s antes)", nome, time, MinSeg(*v.VistoDesdeMs), segundosTexto(refMs-*v.VistoDesdeMs))
		if *v.UltimoVistoMs < refMs-1000 {
			s += fmt.Sprintf(", visto pela última vez aos %s", MinSeg(*v.UltimoVistoMs))
		}
		if v.VistoInferido {
			s += " (só por continuidade, sem o anel lido)"
		}
		return s + " (" + cob + ")"
	case VistoNao:
		return fmt.Sprintf("%s NÃO foi visto pelo %s de %s a %s (%s)", nome, time, MinSeg(v.DeMs), MinSeg(v.AteMs), cob)
	}
	return fmt.Sprintf("não dá para saber se %s era visto pelo %s de %s a %s (%s)", nome, time, MinSeg(v.DeMs), MinSeg(v.AteMs), cob)
}

// VisaoDoMomento lê de um momento abate (recém-gerado ou lido do banco) as
// respostas guardadas em dados.visao_atacantes e dados.visao_cacador.
func VisaoDoMomento(m MomentoChave) (atacantes []VisaoAntes, cacador *VisaoAntes) {
	if b, err := json.Marshal(m.Dados["visao_atacantes"]); err == nil {
		_ = json.Unmarshal(b, &atacantes)
	}
	if x, ok := m.Dados["visao_cacador"]; ok && x != nil {
		var c VisaoAntes
		if b, err := json.Marshal(x); err == nil && json.Unmarshal(b, &c) == nil && c.ParticipanteID > 0 {
			cacador = &c
		}
	}
	return atacantes, cacador
}

// LinhasVisaoDoAbate: as frases da visão guardada num momento abate (cada
// atacante do outro time e o caçador inimigo), na regra de visaoDoAbate.
// Vazio sem passe de visão do time da vítima.
func LinhasVisaoDoAbate(m MomentoChave, nomes map[int]string) []string {
	if m.Tipo != "abate" {
		return nil
	}
	atac, cac := VisaoDoMomento(m)
	var ls []string
	cacAtacou := false
	for _, r := range atac {
		quem := nomes[r.ParticipanteID]
		if cac != nil && r.ParticipanteID == cac.ParticipanteID {
			quem, cacAtacou = quem+" (o caçador inimigo)", true
		}
		ls = append(ls, TextoVisaoAntes(r, quem, m.TMs))
	}
	if cac != nil && !cacAtacou {
		ls = append(ls, "caçador inimigo: "+TextoVisaoAntes(*cac, nomes[cac.ParticipanteID], m.TMs))
	}
	return ls
}

func (d *dadosPartida) indicadoresVisao(c *coleta) {
	part := d.participante(c.pid)
	if d.visao == nil || part == nil {
		return
	}
	vt, ok := d.visao.DoObservador(300 - part.Time)
	if !ok {
		return
	}
	j, ok := vt.Jogador(c.pid)
	if !ok {
		return
	}
	for _, f := range Fases {
		ini, fim := limitesFase(f)
		fim = min(fim, d.duracaoMs())
		vis, fora, ind := somaNaJanela(j.Visivel, ini, fim), somaNaJanela(j.Fora, ini, fim), somaNaJanela(j.Indeterminado, ini, fim)
		if vis+fora < 60000 {
			continue
		}
		v := float64(vis) / float64(vis+fora) * 100
		c.poe("pct_visivel_inimigo_"+f, fp(v), ind*10 > vis+fora+ind, map[string]any{"visivel_ms": vis, "fora_ms": fora,
			"indeterminado_ms": ind, "inferido_ms": somaNaJanela(j.Inferido, ini, fim), "fora_do_mapa_ms": somaNaJanela(j.ForaDoMapa, ini, fim),
			"time_observador": vt.Time})
	}
	if RotaDaPosicao(part.Posicao) != "" {
		return // só caçador
	}
	var fora []Momento
	vistos, indet := 0, 0
	for _, it := range d.cadeias[c.pid] {
		if it.Tipo != "atividade" || it.Dados["atividade"] != AGankando {
			continue
		}
		r, ok := visaoAntes(vt, c.pid, it.TMs, max(it.TMs+1, int64(numero(it.Dados["fim_ms"]))))
		switch {
		case ok && r.Resposta == VistoNao:
			fora = append(fora, Momento{it.TMs, fmt.Sprintf("gank aos %s sem ser visto pelo inimigo até a luta", MinSeg(it.TMs))})
		case ok && r.Resposta == VistoSim:
			vistos++
		default:
			indet++
		}
	}
	c.conta("ganks_fora_da_visao_n", fora, indet > 0, map[string]any{"ganks_vistos": vistos, "ganks_indeterminados": indet,
		"time_observador": vt.Time})
}

// somaNaJanela: quanto dos intervalos cai em [ini, fim).
func somaNaJanela(is [][2]int64, ini, fim int64) int64 {
	var s int64
	for _, i := range is {
		a, b := max(i[0], ini), min(i[1], fim)
		if b > a {
			s += b - a
		}
	}
	return s
}
