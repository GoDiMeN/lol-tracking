package rastreio

import (
	"math"
	"sort"

	"loltracking/server/banco"
)

// Na cadeia de cada jogador, quando o time INIMIGO passou a vê-lo e deixou de
// vê-lo (do passe de visão do time inimigo, visao.json):
//
//	ficou_visivel   {time_observador, ate_ms, duracao_ms, x, y}
//	sumiu_da_visao  {time_observador, visivel_desde_ms, duracao_ms, x, y}
//
// Só nas trocas decididas: "indeterminado" curto no meio não troca nada (o
// retrato coberto por outro por um instante), e "fora" de menos de
// ForaMinimoMs entre dois trechos visíveis também não (uma captura sem o anel).
// Um buraco sem captura (morto, ou fora do passe) zera: depois dele não sai
// item até a primeira troca de verdade.

// ExtraVisao: chave de ContextoCadeia.Extra com o *VisaoDaPartida.
const ExtraVisao = "visao"

const (
	// IndeterminadoCurtoMs: indeterminado até isto entre dois trechos
	// decididos é ignorado.
	IndeterminadoCurtoMs int64 = 2000
	// ForaMinimoMs: "fora" mais curto que isto entre dois visíveis não conta.
	ForaMinimoMs int64 = 500
)

func init() {
	RegistraContexto("visao", func(db *banco.DB, c *ContextoCadeia) {
		if v, ok, err := LeVisao(db, c.GameID); err == nil && ok {
			if c.Extra == nil {
				c.Extra = map[string]any{}
			}
			c.Extra[ExtraVisao] = v
		}
	})
	RegistraGerador("visao", geraVisao)
}

// TrechoVisao é um pedaço contínuo de um estado.
type TrechoVisao struct {
	De, Ate int64
	Estado  string
}

// TrechosVisao junta os intervalos do jogador numa sequência em ordem de
// tempo, já com as regras de cima: indeterminado curto entre dois decididos
// some (vira o estado de antes), "fora" curto entre dois visíveis vira
// visível, e trechos iguais seguidos se juntam. Buraco sem captura continua
// buraco.
func TrechosVisao(j VisibilidadeJogador) []TrechoVisao {
	var ts []TrechoVisao
	for _, l := range []struct {
		e  string
		is [][2]int64
	}{{VisVisivel, j.Visivel}, {VisFora, j.Fora}, {VisIndeterminado, j.Indeterminado}} {
		for _, i := range l.is {
			ts = append(ts, TrechoVisao{i[0], i[1], l.e})
		}
	}
	sort.Slice(ts, func(a, b int) bool { return ts[a].De < ts[b].De })
	// Indeterminado curto entre dois decididos encostados.
	for k := 1; k+1 < len(ts); k++ {
		if ts[k].Estado == VisIndeterminado && ts[k].Ate-ts[k].De <= IndeterminadoCurtoMs &&
			ts[k-1].Ate == ts[k].De && ts[k].Ate == ts[k+1].De && ts[k-1].Estado != VisIndeterminado {
			ts[k].Estado = ts[k-1].Estado
		}
	}
	ts = juntaTrechos(ts)
	for k := 1; k+1 < len(ts); k++ {
		if ts[k].Estado == VisFora && ts[k].Ate-ts[k].De < ForaMinimoMs && ts[k-1].Estado == VisVisivel && ts[k+1].Estado == VisVisivel &&
			ts[k-1].Ate == ts[k].De && ts[k].Ate == ts[k+1].De {
			ts[k].Estado = VisVisivel
		}
	}
	return juntaTrechos(ts)
}

func juntaTrechos(ts []TrechoVisao) []TrechoVisao {
	var out []TrechoVisao
	for _, t := range ts {
		if n := len(out); n > 0 && out[n-1].Estado == t.Estado && out[n-1].Ate >= t.De {
			out[n-1].Ate = max(out[n-1].Ate, t.Ate)
			continue
		}
		out = append(out, t)
	}
	return out
}

func geraVisao(c *ContextoCadeia) []ItemCadeia {
	v, _ := c.Extra[ExtraVisao].(*VisaoDaPartida)
	if v == nil || len(c.Amostras) == 0 || c.Time == 0 {
		return nil
	}
	observador := 300 - c.Time
	vt, ok := v.DoObservador(observador)
	if !ok {
		return nil
	}
	j, ok := vt.Jogador(c.ParticipanteID)
	if !ok {
		return nil
	}
	ini, fim := c.Amostras[0].TMs, c.Amostras[len(c.Amostras)-1].TMs
	ts := TrechosVisao(j)
	var itens []ItemCadeia
	ultimo := "" // último estado decidido (zera num buraco)
	visivelDesde := int64(-1)
	for k, t := range ts {
		if k > 0 && ts[k-1].Ate < t.De {
			ultimo, visivelDesde = "", -1 // buraco sem captura
		}
		if t.Estado == VisIndeterminado {
			continue
		}
		if t.De >= ini && t.De <= fim {
			switch {
			case t.Estado == VisVisivel && ultimo == VisFora:
				x, y := posicaoPerto(c.Amostras, t.De)
				itens = append(itens, ItemCadeia{TMs: t.De, Tipo: "ficou_visivel", Dados: map[string]any{
					"time_observador": observador, "ate_ms": t.Ate, "duracao_ms": t.Ate - t.De, "x": x, "y": y}})
			case t.Estado == VisFora && ultimo == VisVisivel:
				x, y := posicaoPerto(c.Amostras, t.De)
				d := map[string]any{"time_observador": observador, "x": x, "y": y}
				if visivelDesde >= 0 {
					d["visivel_desde_ms"], d["duracao_ms"] = visivelDesde, t.De-visivelDesde
				}
				itens = append(itens, ItemCadeia{TMs: t.De, Tipo: "sumiu_da_visao", Dados: d})
			}
		}
		if t.Estado == VisVisivel && ultimo != VisVisivel {
			visivelDesde = t.De
		}
		ultimo = t.Estado
	}
	return itens
}

// posicaoPerto: a posição boa mais perto do instante (nil sem nenhuma a 2 s).
func posicaoPerto(as []Amostra, t int64) (any, any) {
	i := sort.Search(len(as), func(k int) bool { return as[k].TMs >= t })
	melhor, d := -1, int64(math.MaxInt64)
	for k := max(0, i-8); k < min(len(as), i+8); k++ {
		if as[k].X == nil || as[k].Y == nil {
			continue
		}
		if dd := abs64(as[k].TMs - t); dd < d {
			melhor, d = k, dd
		}
	}
	if melhor < 0 || d > 2000 {
		return nil, nil
	}
	return math.Round(*as[melhor].X), math.Round(*as[melhor].Y)
}
