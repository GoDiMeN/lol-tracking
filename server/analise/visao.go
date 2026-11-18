package analise

import (
	"fmt"

	"loltracking/server/rastreio"
)

// Indicadores da visão dos times (passes de visão, rastreio/visao_*.go): do
// ponto de vista de cada jogador rastreado, como o time INIMIGO o via.
//
//   - pct_visivel_inimigo_<fase>: tempo à vista ÷ (à vista + fora da visão) na
//     fase. O indeterminado (retrato coberto, painel não conferido, sem
//     posição) fica de fora da conta; com mais de 10% da fase indeterminado, o
//     valor sai aproximado. Fase com menos de 1 min decidido: sem valor.
//   - ganks_fora_da_visao_n (caçador): trechos `gankando` (a aproximação que
//     terminou em luta numa rota, etiquetas.go) que começaram com ele fora da
//     visão do inimigo. O começo do trecho indeterminado não conta nem como
//     visto nem como fora (vai no detalhe).

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
			"indeterminado_ms": ind, "time_observador": vt.Time})
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
		switch j.EstadoEm(it.TMs) {
		case rastreio.VisFora:
			fora = append(fora, Momento{it.TMs, fmt.Sprintf("começou um gank fora da visão do inimigo aos %s", MinSeg(it.TMs))})
		case rastreio.VisVisivel:
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
