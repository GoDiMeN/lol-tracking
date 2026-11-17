package visao

import (
	"image"
	"sort"
)

// Leitura de texto por gabarito. O HUD do LoL desenha números com fontes
// fixas e sem variação de posição sub-pixel que importe; então dá para:
//  1. marcar a "tinta" de cada pixel da região (branco, luminância ou vermelho,
//     conforme a fonte) e ficar com o que passa do limiar;
//  2. separar os glifos por componentes conexos (8-vizinhança). O contorno
//     preto que o jogo põe em volta dos números das recargas separa os dígitos
//     dos traços brancos do relógio de recarga;
//  3. descartar componente que encosta na borda da região (traço do relógio,
//     arte do ícone, coisa que vem de fora) e componente fino demais (traço);
//  4. comparar cada glifo com os gabaritos da fonte (extraídos das capturas
//     reais, gabaritos/*.txt) e ficar com o mais parecido, com margem sobre o
//     segundo. Sem IA: diferença absoluta normalizada entre bitmaps.

// glifo é um bitmap de tinta (0 = vazio).
type glifo struct {
	w, h int
	v    []uint8
	// x0, y0: canto no recorte de origem (para ordenar e medir espaços).
	x0, y0 int
	area   int
}

// tinta diz quanto um pixel parece texto (0–255).
type tinta func(r, g, b uint8) uint8

func brancura(r, g, b uint8) uint8 { return min(r, g, b) }

func luminancia(r, g, b uint8) uint8 {
	return uint8((uint32(r)*299 + uint32(g)*587 + uint32(b)*114) / 1000)
}

// vermelho: o contador de renascimento é vermelho puro sobre o retrato cinza.
func vermelho(r, g, b uint8) uint8 {
	m := max(g, b)
	if r <= m {
		return 0
	}
	return r - m
}

// fonte é um tipo de texto do HUD com os seus gabaritos.
type fonte struct {
	nome   string
	tinta  tinta
	limiar uint8
	// areaMin: componente menor que isso é sujeira (o ponto decimal tem ~6).
	areaMin int
	// alturaMin/alturaMax: altura aceitável de um glifo (o ponto é exceção:
	// é aceito se for pequeno e estiver na base da linha).
	alturaMin, alturaMax int
	gab                  []gabarito
}

type gabarito struct {
	c byte
	g glifo
}

// componentes acha os glifos de uma região (coordenadas da imagem).
func componentes(img *image.RGBA, r image.Rectangle, f *fonte) []glifo {
	r = r.Intersect(img.Rect)
	w, h := r.Dx(), r.Dy()
	if w <= 0 || h <= 0 {
		return nil
	}
	val := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		off := (r.Min.Y+y-img.Rect.Min.Y)*img.Stride + (r.Min.X-img.Rect.Min.X)*4
		for x := 0; x < w; x++ {
			i := off + x*4
			t := f.tinta(img.Pix[i], img.Pix[i+1], img.Pix[i+2])
			if t >= f.limiar {
				val[y*w+x] = t
			}
		}
	}
	rot := make([]int32, w*h) // rótulo do componente (0 = nenhum)
	var fila []int32
	var gs []glifo
	var n int32
	for i := range val {
		if val[i] == 0 || rot[i] != 0 {
			continue
		}
		n++
		rot[i] = n
		fila = append(fila[:0], int32(i))
		x0, y0, x1, y1 := w, h, -1, -1
		borda := false
		for k := 0; k < len(fila); k++ {
			p := int(fila[k])
			px, py := p%w, p/w
			x0, y0, x1, y1 = min(x0, px), min(y0, py), max(x1, px), max(y1, py)
			if px == 0 || py == 0 || px == w-1 || py == h-1 {
				borda = true
			}
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					qx, qy := px+dx, py+dy
					if qx < 0 || qy < 0 || qx >= w || qy >= h {
						continue
					}
					q := qy*w + qx
					if val[q] != 0 && rot[q] == 0 {
						rot[q] = n
						fila = append(fila, int32(q))
					}
				}
			}
		}
		area := len(fila)
		if borda || area < f.areaMin {
			continue
		}
		gw, gh := x1-x0+1, y1-y0+1
		// Traço fino (relógio de recarga, borda de ícone): comprido e ralo.
		if max(gw, gh) >= 6 && float64(area) < 0.22*float64(gw*gh) {
			continue
		}
		if gh > f.alturaMax {
			continue
		}
		g := glifo{w: gw, h: gh, v: make([]uint8, gw*gh), x0: x0, y0: y0, area: area}
		for _, p := range fila {
			px, py := int(p)%w, int(p)/w
			g.v[(py-y0)*gw+(px-x0)] = val[p]
		}
		gs = append(gs, g)
	}
	sort.Slice(gs, func(a, b int) bool { return gs[a].x0 < gs[b].x0 })
	return naLinha(gs, f)
}

// naLinha fica só com os glifos que estão na mesma linha de texto: os dígitos
// têm a mesma base e quase a mesma altura; o ponto decimal é pequeno e fica na
// base. Pedaço de traço do relógio ou de arte do ícone/retrato que sobrou
// raramente tem as duas coisas.
func naLinha(gs []glifo, f *fonte) []glifo {
	var bases, alturas []int
	for _, g := range gs {
		if g.h >= f.alturaMin {
			bases = append(bases, g.y0+g.h)
			alturas = append(alturas, g.h)
		}
	}
	if len(bases) == 0 {
		return nil
	}
	sort.Ints(bases)
	sort.Ints(alturas)
	base, altura := bases[len(bases)/2], alturas[len(alturas)/2]
	out := gs[:0]
	for _, g := range gs {
		b := g.y0 + g.h
		alto := g.h >= f.alturaMin && abs(g.h-altura) <= 2 && abs(b-base) <= 1
		// O ponto é um quadradinho cheio; pedaço de traço é comprido ou ralo.
		ponto := g.h < f.alturaMin && g.w >= 2 && g.h >= 2 && g.w <= 4 && g.h <= 4 && abs(g.w-g.h) <= 1 &&
			g.area*10 >= g.w*g.h*5 && abs(b-base) <= 1
		if alto || ponto {
			out = append(out, g)
		}
	}
	// Ponto só entre dígitos.
	for len(out) > 0 && out[0].h < f.alturaMin {
		out = out[1:]
	}
	for len(out) > 0 && out[len(out)-1].h < f.alturaMin {
		out = out[:len(out)-1]
	}
	return out
}

// diferenca entre dois glifos, 0 (iguais) a 1 (nada em comum), na melhor de 9
// posições relativas (±1 px), alinhando os cantos superiores esquerdos.
func diferenca(a, b *glifo) float64 {
	if abs(a.w-b.w) > 2 || abs(a.h-b.h) > 2 {
		return 1
	}
	melhor := 1.0
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			// b deslocado de (dx, dy) em relação a a.
			ux0, uy0 := min(0, dx), min(0, dy)
			ux1, uy1 := max(a.w, b.w+dx), max(a.h, b.h+dy)
			var dif, soma int
			for y := uy0; y < uy1; y++ {
				for x := ux0; x < ux1; x++ {
					va, vb := 0, 0
					if x >= 0 && y >= 0 && x < a.w && y < a.h {
						va = int(a.v[y*a.w+x])
					}
					bx, by := x-dx, y-dy
					if bx >= 0 && by >= 0 && bx < b.w && by < b.h {
						vb = int(b.v[by*b.w+bx])
					}
					dif += abs(va - vb)
					soma += va + vb
				}
			}
			if soma > 0 {
				if d := float64(dif) / float64(soma); d < melhor {
					melhor = d
				}
			}
		}
	}
	return melhor
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// Limiares da classificação: diferença máxima aceita e margem mínima sobre o
// segundo caractere mais parecido. Medidos nas capturas (gabaritos_test.go).
const (
	difMaxima  = 0.30
	margemMin  = 0.05
	confMinima = 0.70
)

// classifica devolve o caractere mais parecido e a confiança (0–1). '?' se
// nenhum gabarito serve ou se a dúvida entre dois é grande.
func (f *fonte) classifica(g *glifo) (byte, float64) {
	// Melhor diferença por caractere (um caractere tem várias variantes).
	var porChar [128]float64
	for i := range porChar {
		porChar[i] = 1
	}
	for i := range f.gab {
		if d := diferenca(g, &f.gab[i].g); d < porChar[f.gab[i].c&127] {
			porChar[f.gab[i].c&127] = d
		}
	}
	melhor, segundo := 1.0, 1.0
	var c byte = '?'
	for k, d := range porChar {
		switch {
		case d < melhor:
			segundo, melhor, c = melhor, d, byte(k)
		case d < segundo:
			segundo = d
		}
	}
	conf := 1 - melhor
	if melhor > difMaxima || segundo-melhor < margemMin {
		return '?', conf
	}
	return c, conf
}

// texto lê os glifos de uma região. Devolve o texto (com '?' onde não deu) e
// a confiança mínima entre os glifos (0 se não há glifo).
func (f *fonte) texto(gs []glifo) (string, float64) {
	if len(gs) == 0 {
		return "", 0
	}
	b := make([]byte, 0, len(gs))
	conf := 1.0
	lmax := 0
	for i := range f.gab {
		lmax = max(lmax, f.gab[i].g.w)
	}
	for i := range gs {
		c, k := f.classifica(&gs[i])
		if c == '?' && gs[i].w > lmax+1 {
			// Dois dígitos encostados (o "4" do atributo encosta no vizinho).
			if s, k2, ok := f.divide(&gs[i], 2); ok {
				b = append(b, s...)
				conf = min(conf, k2)
				continue
			}
		}
		b = append(b, c)
		conf = min(conf, k)
	}
	return string(b), conf
}

// divide tenta separar um componente largo em glifos conhecidos, cortando em
// cada coluna possível (até "fundo" cortes) e ficando com o melhor.
func (f *fonte) divide(g *glifo, fundo int) (string, float64, bool) {
	melhorS, melhorK, achou := "", 0.0, false
	for x := 3; x <= g.w-3; x++ {
		a, b := recorta(g, 0, x), recorta(g, x, g.w)
		if a.w == 0 || b.w == 0 {
			continue
		}
		ca, ka := f.classifica(&a)
		if ca == '?' {
			continue
		}
		cb, kb := f.classifica(&b)
		sb := string(cb)
		if cb == '?' {
			if fundo <= 1 {
				continue
			}
			var ok bool
			if sb, kb, ok = f.divide(&b, fundo-1); !ok {
				continue
			}
		}
		if k := min(ka, kb); k > melhorK {
			melhorS, melhorK, achou = string(ca)+sb, k, true
		}
	}
	return melhorS, melhorK, achou
}

// recorta as colunas [x0,x1) do glifo e apara linhas e colunas vazias.
func recorta(g *glifo, x0, x1 int) glifo {
	cx0, cx1, cy0, cy1 := x1, x0-1, g.h, -1
	for y := 0; y < g.h; y++ {
		for x := x0; x < x1; x++ {
			if g.v[y*g.w+x] != 0 {
				cx0, cx1, cy0, cy1 = min(cx0, x), max(cx1, x), min(cy0, y), max(cy1, y)
			}
		}
	}
	if cx1 < cx0 {
		return glifo{}
	}
	r := glifo{w: cx1 - cx0 + 1, h: cy1 - cy0 + 1, x0: g.x0 + cx0, y0: g.y0 + cy0}
	r.v = make([]uint8, r.w*r.h)
	for y := 0; y < r.h; y++ {
		copy(r.v[y*r.w:(y+1)*r.w], g.v[(y+cy0)*g.w+cx0:(y+cy0)*g.w+cx1+1])
		for _, v := range r.v[y*r.w : (y+1)*r.w] {
			if v != 0 {
				r.area++
			}
		}
	}
	return r
}
