package visao

import (
	"errors"
	"fmt"
	"image"
	"math"
	"sort"

	"loltracking/server/rastreio"
	"loltracking/server/replay"
)

// Leitura do minimapa e do painel "Fog of War" no passe de visão de time
// (implementa rastreio.LeitorDeMinimapa). Sem IA: limiar de cor, votação de
// círculos e componentes conexos, nas cores medidas nas capturas de
// 2026-11-17 (replay/minimapa.go tem a geometria e as cores).
//
//   - Painel: o texto da caixa da névoa é "Blue" em azul vivo, "Red" em
//     vermelho vivo e "All" em branco: conta os pixels de cada cor.
//   - Retratos: cada campeão no minimapa é um retrato redondo com um anel de
//     ~2 px na cor do time (azul 0099e0, vermelho e83d3d) e raio 14,5 px. As
//     torres e inibidores têm outras cores (mais escuras) e outras formas; as
//     tropas são pontos. Acha os centros por votação (cada pixel da cor vota
//     nos centros a 13,5–15,5 px dele) e confirma pela fração do anel
//     presente em volta do centro (retrato meio coberto por outro continua
//     valendo acima de CoberturaMinimaRetrato) e pelo miolo sem a cor.
//   - Sentinelas: o marcador da sentinela do time é um "olho com asas" de
//     ~12×10 px; componente conexo da cor, fora dos anéis, comparado com o
//     gabarito do marcador medido.
//
// Melhor não achar que achar errado: o que fica no limiar sai de fora, e a
// decisão de visibilidade (que junta isto com as posições rastreadas) trata a
// falta como "indeterminado" quando não dá para ter certeza.

// ErrMinimapa: o minimapa não está onde a leitura espera (escala do
// minimapa diferente da padrão, minimapa escondido, outra resolução).
var ErrMinimapa = errors.New("leitura do minimapa: o minimapa não está onde a leitura espera (escala do minimapa diferente da padrão ou minimapa escondido)")

// Limiares do minimapa (medidos nas 3 capturas de 2026-11-17).
const (
	// CoberturaMinimaRetrato: fração mínima de raios (de 64) em que o anel
	// aparece. Retrato inteiro: 1,00; a Diana meio coberta pela Orianna: 0,83;
	// os círculos dos inibidores da base azul (raio menor) chegam a 0,52.
	CoberturaMinimaRetrato = 0.62
	// MioloMaximo: fração máxima de pixels da cor do anel no miolo (raio 9).
	MioloMaximo = 0.15
	// SemelhancaMinimaSentinela: semelhança mínima com o gabarito do marcador
	// (semelhanca). Os 4 marcadores inteiros das capturas: 0,91–1,00; uma
	// corrente de tropas do tamanho certo: 0,58.
	SemelhancaMinimaSentinela = 0.75
)

func anelAzul(p []uint8) bool {
	return p[2] >= 0xb0 && p[1] >= 0x70 && p[1] <= 0xc0 && p[0] <= 0x50
}

func anelVermelho(p []uint8) bool {
	return p[0] >= 0xc0 && p[1] >= 0x20 && p[1] <= 0x70 && p[2] >= 0x20 && p[2] <= 0x70
}

func textoAzul(p []uint8) bool {
	return p[2] >= 0xb0 && int(p[2]) >= int(p[0])+0x70 && p[1] >= 0x70
}

func textoVermelho(p []uint8) bool {
	return p[0] >= 0xb0 && int(p[0]) >= int(p[1])+0x70 && int(p[0]) >= int(p[2])+0x70
}

func textoBranco(p []uint8) bool {
	return min(p[0], p[1], p[2]) >= 0x90 && max(p[0], p[1], p[2])-min(p[0], p[1], p[2]) <= 0x20
}

func sentinelaVermelha(p []uint8) bool {
	return p[0] >= 0xb0 && p[1] <= 0x60 && p[2] <= 0x60 && int(p[0]) >= int(p[1])+0x70
}

// O azul da sentinela é médio (2880ba) e fica sobre o rio (107b9b): o que
// separa é o azul bem acima do verde. O anel (0099e0) e as tropas (008fd2)
// têm o vermelho zerado.
func sentinelaAzul(p []uint8) bool {
	return p[2] >= 0x75 && p[0] >= 0x14 && p[0] <= 0x40 && p[1] >= 0x45 && p[1] <= 0x90 && int(p[2]) >= int(p[1])+0x26
}

// gabaritoSentinela: o marcador medido (replay.MarcadorSentinela, que o jogo
// falso também desenha).
var gabaritoSentinela = replay.MarcadorSentinela

// LeMinimapa implementa rastreio.LeitorDeMinimapa.
func (l *Leitor) LeMinimapa(img *image.RGBA, regiao image.Rectangle, largura, altura int) (rastreio.LeituraMinimapa, error) {
	var lt rastreio.LeituraMinimapa
	if img == nil {
		return lt, errors.New("leitura do minimapa: sem captura")
	}
	if largura != refLargura || altura != refAltura {
		return lt, fmt.Errorf("%w (a do jogo é %d×%d)", ErrResolucao, largura, altura)
	}
	if !replay.RegiaoMinimapa.In(regiao) || img.Rect.Dx() < regiao.Dx() || img.Rect.Dy() < regiao.Dy() {
		return lt, errors.New("leitura do minimapa: a captura não cobre o minimapa")
	}
	lt.Modo = ModoNevoa(img, regiao)
	v := vista{img: img, off: regiao.Min.Sub(img.Rect.Min)}
	if !v.molduraPresente() {
		return rastreio.LeituraMinimapa{Modo: lt.Modo}, ErrMinimapa
	}
	lt.Retratos = append(v.retratos(anelAzul, 100), v.retratos(anelVermelho, 200)...)
	lt.Sentinelas = append(v.sentinelas(sentinelaAzul, 100, lt.Retratos), v.sentinelas(sentinelaVermelha, 200, lt.Retratos)...)
	return lt, nil
}

// ModoNevoa lê o modo da névoa no painel do replay, se a captura (o
// retângulo regiao da área do jogo de referência) cobre a caixa; senão "".
func ModoNevoa(img *image.RGBA, regiao image.Rectangle) string {
	if img == nil || !replay.RegiaoPainelNevoa.In(regiao) {
		return ""
	}
	return vista{img: img, off: regiao.Min.Sub(img.Rect.Min)}.modoNevoa(replay.RegiaoPainelNevoa)
}

// vista lê a imagem em coordenadas de referência.
type vista struct {
	img *image.RGBA
	off image.Point // ponto de referência que cai em img.Rect.Min
}

func (v vista) px(x, y int) []uint8 {
	x, y = x-v.off.X, y-v.off.Y
	if !(image.Point{x, y}).In(v.img.Rect) {
		return nil
	}
	i := v.img.PixOffset(x, y)
	return v.img.Pix[i : i+3 : i+3]
}

// modoNevoa conta as cores do texto da caixa da névoa. "Blue" e "Red" têm
// dezenas de pixels da cor viva; "All" tem uns 30 pixels brancos e nenhuma
// cor viva. Sem maioria clara: "" (não lido).
func (v vista) modoNevoa(r image.Rectangle) string {
	na, nv, nb := 0, 0, 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			p := v.px(x, y)
			if p == nil {
				continue
			}
			switch {
			case textoAzul(p):
				na++
			case textoVermelho(p):
				nv++
			case textoBranco(p):
				nb++
			}
		}
	}
	switch {
	case na >= 20 && na > 3*(nv+nb):
		return rastreio.ModoNevoaAzul
	case nv >= 20 && nv > 3*(na+nb):
		return rastreio.ModoNevoaVermelho
	case nb >= 12 && na < 5 && nv < 5:
		return rastreio.ModoNevoaTodos
	}
	return ""
}

// molduraPresente: a borda preta do quadrado do minimapa (faixas de 6 px à
// esquerda e embaixo, por dentro do quadrado) está no lugar. Retratos podem
// cobrir um pedaço: basta a maior parte.
func (v vista) molduraPresente() bool {
	q := replay.QuadradoMinimapa
	n, pretos := 0, 0
	conta := func(r image.Rectangle) {
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				p := v.px(x, y)
				if p == nil {
					continue
				}
				n++
				if max(p[0], p[1], p[2]) <= 0x14 {
					pretos++
				}
			}
		}
	}
	conta(image.Rect(q.Min.X+1, q.Min.Y+20, q.Min.X+7, q.Max.Y-20))
	conta(image.Rect(q.Min.X+20, q.Max.Y-7, q.Max.X-20, q.Max.Y-1))
	return n > 0 && pretos*10 >= n*7
}

// retratos acha os anéis de uma cor.
func (v vista) retratos(cor func([]uint8) bool, time int) []rastreio.MarcaMinimapa {
	r := replay.RegiaoMinimapa
	w, h := r.Dx(), r.Dy()
	mask := make([]bool, w*h)
	var pontos []image.Point
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if p := v.px(r.Min.X+x, r.Min.Y+y); p != nil && cor(p) {
				mask[y*w+x] = true
				pontos = append(pontos, image.Pt(x, y))
			}
		}
	}
	if len(pontos) < 40 {
		return nil
	}
	em := func(x, y int) bool { return x >= 0 && y >= 0 && x < w && y < h && mask[y*w+x] }
	// Votação: cada pixel da cor vota nos centros a 13,5–15,5 px.
	votos := make([]uint16, w*h)
	for _, p := range pontos {
		for _, d := range raiosDeVoto {
			cx, cy := p.X-d.X, p.Y-d.Y
			if cx >= 0 && cy >= 0 && cx < w && cy < h {
				votos[cy*w+cx]++
			}
		}
	}
	// Um anel inteiro dá ~cobertura × 48 direções × alguns raios; o limiar
	// de votos só corta o que não tem chance (o teste de verdade é a cobertura).
	type cand struct {
		x, y int
		v    uint16
	}
	var cs []cand
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if votos[y*w+x] >= 40 && !mask[y*w+x] {
				cs = append(cs, cand{x, y, votos[y*w+x]})
			}
		}
	}
	sort.Slice(cs, func(a, b int) bool { return cs[a].v > cs[b].v })
	type achado struct {
		x, y float64
		cob  float64
	}
	var achados []achado
	for _, c := range cs {
		perto := false
		for _, a := range achados {
			if math.Hypot(a.x-float64(c.x), a.y-float64(c.y)) < 8 {
				perto = true
				break
			}
		}
		if perto {
			continue
		}
		// Refina: a melhor cobertura numa vizinhança de ±2 px; centro = média
		// das posições empatadas no topo.
		melhor := -1.0
		type pc struct {
			x, y int
			cob  float64
		}
		var viz []pc
		for dy := -2; dy <= 2; dy++ {
			for dx := -2; dx <= 2; dx++ {
				x, y := c.x+dx, c.y+dy
				if em(x, y) {
					continue
				}
				cob := cobertura(em, x, y)
				if cob < 0 || miolo(em, x, y) > MioloMaximo {
					continue
				}
				viz = append(viz, pc{x, y, cob})
				melhor = math.Max(melhor, cob)
			}
		}
		if melhor < CoberturaMinimaRetrato {
			continue
		}
		sx, sy, n := 0.0, 0.0, 0.0
		for _, p := range viz {
			if p.cob >= melhor-0.02 {
				sx += float64(p.x)
				sy += float64(p.y)
				n++
			}
		}
		achados = append(achados, achado{sx / n, sy / n, melhor})
	}
	var out []rastreio.MarcaMinimapa
	for _, a := range achados {
		px, py := float64(r.Min.X)+a.x, float64(r.Min.Y)+a.y
		mx, my := replay.MinimapaParaMapa(px, py, refLargura, refAltura)
		out = append(out, rastreio.MarcaMinimapa{Time: time, X: math.Round(mx), Y: math.Round(my), Cobertura: math.Round(a.cob*100) / 100})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].X < out[b].X })
	return out
}

// raiosDeVoto: os deslocamentos (48 direções × 3 raios) sem repetição.
var raiosDeVoto = func() []image.Point {
	vistos := map[image.Point]bool{}
	var ds []image.Point
	for _, r := range []float64{13.5, 14.5, 15.5} {
		for k := 0; k < 48; k++ {
			a := 2 * math.Pi * float64(k) / 48
			d := image.Pt(int(math.Round(r*math.Cos(a))), int(math.Round(r*math.Sin(a))))
			if !vistos[d] {
				vistos[d] = true
				ds = append(ds, d)
			}
		}
	}
	return ds
}()

// cobertura: fração de 64 direções em que há pixel da cor a 12,5–16,5 px do
// centro.
func cobertura(em func(x, y int) bool, cx, cy int) float64 {
	const n = 64
	hit := 0
	for k := 0; k < n; k++ {
		c, s := direcoes[k][0], direcoes[k][1]
		for r := 12.5; r <= 16.5; r += 0.5 {
			if em(cx+int(math.Round(r*c)), cy+int(math.Round(r*s))) {
				hit++
				break
			}
		}
	}
	return float64(hit) / n
}

var direcoes = func() [64][2]float64 {
	var d [64][2]float64
	for k := range d {
		a := 2 * math.Pi * float64(k) / 64
		d[k] = [2]float64{math.Cos(a), math.Sin(a)}
	}
	return d
}()

// miolo: fração de pixels da cor a até 9 px do centro (o retrato não tem a
// cor do anel; um disco cheio da cor não é retrato).
func miolo(em func(x, y int) bool, cx, cy int) float64 {
	n, c := 0, 0
	for dy := -9; dy <= 9; dy++ {
		for dx := -9; dx <= 9; dx++ {
			if dx*dx+dy*dy > 81 {
				continue
			}
			n++
			if em(cx+dx, cy+dy) {
				c++
			}
		}
	}
	return float64(c) / float64(n)
}

// sentinelas acha os marcadores de sentinela de uma cor, fora dos anéis.
func (v vista) sentinelas(cor func([]uint8) bool, time int, retratos []rastreio.MarcaMinimapa) []rastreio.MarcaMinimapa {
	q := replay.QuadradoMinimapa
	w, h := q.Dx(), q.Dy()
	var centros [][2]float64
	for _, rt := range retratos {
		px, py := replay.MapaParaMinimapa(rt.X, rt.Y, refLargura, refAltura)
		centros = append(centros, [2]float64{px - float64(q.Min.X), py - float64(q.Min.Y)})
	}
	mask := make([]bool, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			p := v.px(q.Min.X+x, q.Min.Y+y)
			if p == nil || !cor(p) {
				continue
			}
			dentro := false
			for _, c := range centros {
				if math.Hypot(float64(x)-c[0], float64(y)-c[1]) <= replay.RaioRetratoMinimapa+3 {
					dentro = true
					break
				}
			}
			mask[y*w+x] = !dentro
		}
	}
	var out []rastreio.MarcaMinimapa
	visto := make([]bool, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if !mask[y*w+x] || visto[y*w+x] {
				continue
			}
			comp := componente(mask, visto, w, h, x, y)
			if len(comp) < 20 || len(comp) > 80 {
				continue
			}
			// O marcador inteiro tem 12–14 × 10–13 px. Duas tropas encostadas
			// (os pontos das ondas) dão 7–9 px de largura e também se parecem
			// com o gabarito na tolerância de 1 px: o tamanho é o que separa.
			b := caixa(comp)
			if b.Dx() < 11 || b.Dx() > 16 || b.Dy() < 9 || b.Dy() > 15 {
				continue
			}
			s := semelhanca(comp, b)
			if s < SemelhancaMinimaSentinela {
				continue
			}
			cx := float64(q.Min.X) + float64(b.Min.X+b.Max.X)/2
			cy := float64(q.Min.Y) + float64(b.Min.Y+b.Max.Y)/2
			mx, my := replay.MinimapaParaMapa(cx, cy, refLargura, refAltura)
			out = append(out, rastreio.MarcaMinimapa{Time: time, X: math.Round(mx), Y: math.Round(my), Cobertura: math.Round(s*100) / 100})
		}
	}
	return out
}

// componente: o componente que contém (x, y), juntando pixels a até 2 px um
// do outro (o marcador da sentinela tem a "cabeça" separada das asas por uma
// linha de 1 px de outra cor).
func componente(mask, visto []bool, w, h, x, y int) []image.Point {
	pilha := []image.Point{{x, y}}
	visto[y*w+x] = true
	var comp []image.Point
	for len(pilha) > 0 {
		p := pilha[len(pilha)-1]
		pilha = pilha[:len(pilha)-1]
		comp = append(comp, p)
		for dy := -2; dy <= 2; dy++ {
			for dx := -2; dx <= 2; dx++ {
				q := image.Pt(p.X+dx, p.Y+dy)
				if q.X < 0 || q.Y < 0 || q.X >= w || q.Y >= h || visto[q.Y*w+q.X] || !mask[q.Y*w+q.X] {
					continue
				}
				visto[q.Y*w+q.X] = true
				pilha = append(pilha, q)
			}
		}
	}
	return comp
}

func caixa(comp []image.Point) image.Rectangle {
	b := image.Rectangle{Min: comp[0], Max: comp[0].Add(image.Pt(1, 1))}
	for _, p := range comp {
		b = b.Union(image.Rectangle{Min: p, Max: p.Add(image.Pt(1, 1))})
	}
	return b
}

// semelhanca compara o componente com o gabarito, tolerando 1 px: o menor
// entre a fração dos pixels do componente que caem a até 1 px do gabarito e a
// fração do gabarito que tem pixel do componente a até 1 px. A tolerância é o
// que deixa o mesmo gabarito servir para o marcador vermelho (cheio) e o azul
// (só o contorno na cor; o miolo é azul-escuro). Melhor deslocamento até 2 px
// em volta do centro da caixa.
func semelhanca(comp []image.Point, b image.Rectangle) float64 {
	gh, gw := len(gabaritoSentinela), len(gabaritoSentinela[0])
	em := map[image.Point]bool{}
	for _, p := range comp {
		em[p] = true
	}
	perto := func(m func(image.Point) bool, p image.Point) bool {
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				if m(p.Add(image.Pt(dx, dy))) {
					return true
				}
			}
		}
		return false
	}
	melhor := 0.0
	cx := (b.Min.X + b.Max.X - gw) / 2
	cy := (b.Min.Y + b.Max.Y - gh) / 2
	for dy := -2; dy <= 2; dy++ {
		for dx := -2; dx <= 2; dx++ {
			o := image.Pt(cx+dx, cy+dy)
			noGabarito := func(p image.Point) bool {
				q := p.Sub(o)
				return q.X >= 0 && q.Y >= 0 && q.X < gw && q.Y < gh && gabaritoSentinela[q.Y][q.X] == '#'
			}
			noComp := func(p image.Point) bool { return em[p] }
			dentro := 0
			for _, p := range comp {
				if perto(noGabarito, p) {
					dentro++
				}
			}
			cobertos, total := 0, 0
			for y := 0; y < gh; y++ {
				for x := 0; x < gw; x++ {
					if gabaritoSentinela[y][x] != '#' {
						continue
					}
					total++
					if perto(noComp, o.Add(image.Pt(x, y))) {
						cobertos++
					}
				}
			}
			s := math.Min(float64(dentro)/float64(len(comp)), float64(cobertos)/float64(total))
			if s > melhor {
				melhor = s
			}
		}
	}
	return melhor
}
