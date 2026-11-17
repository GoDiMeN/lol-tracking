package visao

import (
	"image"
	"math"

	"loltracking/server/rastreio"
)

// Barras dos quadros laterais: contagem de colunas com a cor da barra. A barra
// enche da esquerda para a direita nos dois lados; o que sobra é cinza-escuro
// (0x131313). Na barra de vida, o pedaço vermelho (dano recente) e o branco
// (escudo) não contam como vida.

func verde(p []uint8) bool {
	return p[1] >= 90 && int(p[1]) >= int(p[0])+50 && int(p[1]) >= int(p[2])+25
}

// recurso: qualquer cor viva (mana azul, energia amarela, fúria vermelha...).
func recurso(p []uint8) bool {
	return max(p[0], p[1], p[2]) >= 110 && max(p[0], p[1], p[2])-min(p[0], p[1], p[2]) >= 60
}

func escuro(p []uint8) bool { return max(p[0], p[1], p[2]) <= 0x30 }

// leBarra devolve a fração cheia (0–1) e a confiança. Coluna conta como cheia
// se a maioria das linhas da faixa tem a cor. As cheias têm de ser um prefixo
// (tolerância de 1 coluna) e o resto tem de ser escuro ou, na vida, vermelho
// ou branco; senão algo cobre a barra e a leitura sai nula.
func leBarra(img *image.RGBA, r image.Rectangle, cheio func([]uint8) bool, vida bool) (float64, float64) {
	w := r.Dx()
	if w <= 0 {
		return 0, 0
	}
	ultimaCheia, cheias, estranhas := -1, 0, 0
	for x := r.Min.X; x < r.Max.X; x++ {
		c, outra := 0, 0
		for y := r.Min.Y; y < r.Max.Y; y++ {
			i := img.PixOffset(x, y)
			p := img.Pix[i : i+3 : i+3]
			switch {
			case cheio(p):
				c++
			case escuro(p):
			case vida && (p[0] >= 150 && p[1] < 100) || vida && min(p[0], p[1], p[2]) >= 150:
				// dano recente (vermelho) ou escudo (branco)
			default:
				outra++
			}
		}
		h := r.Dy()
		if c*2 > h {
			cheias++
			ultimaCheia = x - r.Min.X
		} else if outra*2 > h {
			estranhas++
		}
	}
	buracos := (ultimaCheia + 1) - cheias
	conf := 1.0
	if buracos > 1 {
		conf -= 0.1 * float64(buracos)
	}
	conf -= 0.1 * float64(estranhas)
	return float64(ultimaCheia+1) / float64(w), math.Max(0, conf)
}

// leQuadros lê os 10 quadros. Devolve quantos estavam no lugar esperado.
func (l *Leitor) leQuadros(img *image.RGBA, e escala, lt *rastreio.Leitura) int {
	ok := 0
	nomes := [10]string{"q1", "q2", "q3", "q4", "q5", "q6", "q7", "q8", "q9", "q10"}
	for k := 0; k < 10; k++ {
		rv, rm, re, rr := quadro(k)
		if !faixaEscura(img, e.r(re)) {
			continue // quadro coberto ou HUD fora do lugar
		}
		ok++
		q := &lt.Quadros[k]
		c := lt.Confianca
		// Morto: contador vermelho sobre o retrato.
		ret := e.r(rr)
		// Morto = retrato cinza + número vermelho puro por cima (só o vermelho
		// não basta: o retrato do Warwick tem vermelho de sobra).
		vermelhos, cinzas, total := 0, 0, 0
		for y := ret.Min.Y; y < ret.Max.Y; y++ {
			i := img.PixOffset(ret.Min.X, y)
			for x := ret.Min.X; x < ret.Max.X; x++ {
				p := img.Pix[i : i+3 : i+3]
				total++
				if p[0] >= 200 && p[1] < 60 && p[2] < 60 {
					vermelhos++
				} else if max(p[0], p[1], p[2])-min(p[0], p[1], p[2]) <= 12 {
					cinzas++
				}
				i += 4
			}
		}
		morto := vermelhos >= 40 && cinzas*2 >= total-vermelhos
		q.Morto = &morto
		if morto {
			if v, k2, okn := leNumero(&l.morte, img, ret, false); okn && k2 >= confMinima && v >= 1 && v <= 150 {
				q.Respawn = &v
				c[nomes[k]+"_respawn"] = k2
			}
		}
		fv, kv := leBarra(img, e.r(rv), verde, true)
		c[nomes[k]+"_vida_pct"] = kv
		if kv >= confMinima {
			v := math.Round(fv*1000) / 10
			q.VidaPct = &v
		}
		fm, km := leBarra(img, e.r(rm), recurso, false)
		c[nomes[k]+"_mana_pct"] = km
		// Barra de recurso vazia: não dá para distinguir "0 de mana" de "campeão
		// sem recurso" (Garen, Katarina...): nulo.
		if km >= confMinima && fm > 0 {
			v := math.Round(fm*1000) / 10
			q.ManaPct = &v
		}
	}
	return ok
}
