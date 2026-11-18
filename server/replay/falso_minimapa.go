package replay

import (
	"image"
	"math"
)

// O minimapa e o painel "Fog of War" do jogo falso: desenhados na geometria e
// nas cores medidas (minimapa.go), para o passe de visão ser testado com a
// MESMA leitura que roda no jogo de verdade (pacote visao). O desenho é
// simples (fundo liso, retrato cinza), mas o que a leitura usa está igual: o
// anel de 14,5 px na cor do time, o texto da névoa nas três cores, o
// marcador de sentinela e a moldura preta.

func pinta(img *image.RGBA, regiao image.Rectangle, x, y int, c [3]uint8) {
	p := image.Pt(x, y)
	if !p.In(regiao) {
		return
	}
	i := img.PixOffset(x-regiao.Min.X, y-regiao.Min.Y)
	img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c[0], c[1], c[2], 255
}

func enche(img *image.RGBA, regiao, r image.Rectangle, c [3]uint8) {
	r = r.Intersect(regiao)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			pinta(img, regiao, x, y, c)
		}
	}
}

// desenhaVisao desenha o recorte regiao (coordenadas da tela de referência)
// no tempo t (f.mu travado). Só a tela de referência é desenhada.
func (f *Falso) desenhaVisao(img *image.RGBA, regiao image.Rectangle, t float64) {
	// Painel: fundo e um texto feito de traços na cor do modo.
	enche(img, regiao, RegiaoPainelNevoa, CorFundoPainel)
	cor, x0, x1 := CorTextoBranco, 236, 252
	switch f.visao {
	case 100:
		cor, x0, x1 = CorTextoAzul, 233, 262
	case 200:
		cor, x0, x1 = CorTextoVermelho, 233, 256
	}
	for x := x0; x < x1; x += 3 {
		for y := 1358; y < 1370; y++ {
			pinta(img, regiao, x, y, cor)
		}
	}

	enche(img, regiao, RegiaoMinimapa, [3]uint8{0x22, 0x26, 0x2a})
	if f.SemMinimapa {
		return
	}
	q := QuadradoMinimapa
	enche(img, regiao, q, [3]uint8{0, 0, 0})
	enche(img, regiao, q.Inset(10), [3]uint8{0x3a, 0x40, 0x30})

	// Sentinelas do time da visão (as dos dois em "todos").
	for _, s := range f.Sentinelas {
		if t < s.De || t >= s.Ate || (f.visao != 0 && s.Time != f.visao) {
			continue
		}
		c := CorSentinelaVermelha
		if s.Time == 100 {
			c = CorSentinelaAzul
		}
		px, py := MapaParaMinimapa(s.X, s.Y, LarguraRef, AlturaRef)
		h, w := len(MarcadorSentinela), len(MarcadorSentinela[0])
		ox, oy := int(math.Round(px))-w/2, int(math.Round(py))-h/2
		for yy, linha := range MarcadorSentinela {
			for xx := range linha {
				if linha[xx] == '#' {
					pinta(img, regiao, ox+xx, oy+yy, c)
				}
			}
		}
	}

	// Retratos, na ordem do elenco (o de depois fica por cima, como no jogo
	// quando dois se encostam).
	for i := range f.Elenco {
		if !f.visivelPara(f.visao, i, t) {
			continue
		}
		c := CorAnelAzul
		if f.timeDe(i) == 200 {
			c = CorAnelVermelho
		}
		x, y := f.Posicao(i, t)
		cx, cy := MapaParaMinimapa(x, y, LarguraRef, AlturaRef)
		for yy := int(cy) - 17; yy <= int(cy)+17; yy++ {
			for xx := int(cx) - 17; xx <= int(cx)+17; xx++ {
				d := math.Hypot(float64(xx)-cx, float64(yy)-cy)
				switch {
				case d <= 12.5:
					// Retrato: cinza-arroxeado com um pouco de variação.
					v := uint8(0x50 + (xx*7+yy*3)%0x30)
					pinta(img, regiao, xx, yy, [3]uint8{v, v - 0x10, v + 0x08})
				case d <= 13.5:
					pinta(img, regiao, xx, yy, [3]uint8{0x10, 0x10, 0x10})
				case d <= 15.5:
					pinta(img, regiao, xx, yy, c)
				}
			}
		}
	}
}
