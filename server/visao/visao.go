// Package visao lê o HUD do replay por gabarito: o painel do jogador
// selecionado (vida, recurso, nível, atributos, recargas) e as barras dos 10
// nos quadros laterais. Sem IA em tempo de execução: só recortes pequenos da
// captura, limiar de cor, componentes conexos e comparação com glifos
// extraídos das capturas reais (gabaritos/). Implementa rastreio.LeitorDeTela.
//
// Princípio: melhor nulo que número errado. Cada campo tem uma confiança; abaixo
// do limiar ele sai nulo. A sanidade no tempo (vida_max que não pula etc.) fica
// no saneador do motor (rastreio/sanidade.go), que tem estado por passe.
package visao

import (
	"errors"
	"fmt"
	"image"
	"strconv"
	"strings"

	"loltracking/server/rastreio"
)

// ErrResolucao: a área do jogo não tem o tamanho medido (2560×1440). As
// regiões e os glifos só foram medidos nele; ler em outro tamanho seria ler
// lixo.
var ErrResolucao = errors.New("leitura da tela: só a resolução 2560×1440 foi medida")

// ErrHUD: nenhum dos marcos fixos do HUD (painel, quadros laterais) está onde
// deveria. Quase sempre é a escala do HUD diferente da padrão (Configurações →
// Interface → Escala do HUD) ou os quadros escondidos.
var ErrHUD = errors.New("leitura da tela: o HUD não está onde a leitura espera (escala do HUD diferente da padrão ou quadros laterais escondidos)")

// Leitor implementa rastreio.LeitorDeTela. Sem estado entre chamadas: pode ser
// usado por várias goroutines.
type Leitor struct {
	vida, mana, atr, recarga, nivelHab, nivel, morte fonte
}

// Novo carrega os gabaritos embutidos.
func Novo() (*Leitor, error) {
	l := &Leitor{
		vida:     fonte{nome: "vida", tinta: brancura, limiar: 140, areaMin: 3, alturaMin: 7, alturaMax: 13},
		mana:     fonte{nome: "mana", tinta: brancura, limiar: 140, areaMin: 3, alturaMin: 9, alturaMax: 17},
		atr:      fonte{nome: "atributo", tinta: luminancia, limiar: 140, areaMin: 3, alturaMin: 7, alturaMax: 14},
		recarga:  fonte{nome: "recarga", tinta: brancura, limiar: 160, areaMin: 3, alturaMin: 8, alturaMax: 16},
		nivelHab: fonte{nome: "nivel_habilidade", tinta: brancura, limiar: 160, areaMin: 8, alturaMin: 9, alturaMax: 17},
		nivel:    fonte{nome: "nivel", tinta: brancura, limiar: 160, areaMin: 8, alturaMin: 9, alturaMax: 17},
		morte:    fonte{nome: "renascimento", tinta: vermelho, limiar: 120, areaMin: 10, alturaMin: 10, alturaMax: 30},
	}
	for _, f := range l.fontes() {
		if err := carregaFonte(f); err != nil {
			return nil, err
		}
	}
	return l, nil
}

func (l *Leitor) fontes() []*fonte {
	return []*fonte{&l.vida, &l.mana, &l.atr, &l.recarga, &l.nivelHab, &l.nivel, &l.morte}
}

// Le lê a captura. largura/altura são a área do jogo (a imagem pode ser maior
// só se vier com a área no canto; usa-se img.Rect).
func (l *Leitor) Le(img *image.RGBA, largura, altura int) (rastreio.Leitura, error) {
	var lt rastreio.Leitura
	if img == nil {
		return lt, errors.New("leitura da tela: sem captura")
	}
	if largura != refLargura || altura != refAltura || img.Rect.Dx() < refLargura || img.Rect.Dy() < refAltura {
		return lt, fmt.Errorf("%w (a do jogo é %d×%d)", ErrResolucao, largura, altura)
	}
	e := novaEscala(image.Rect(img.Rect.Min.X, img.Rect.Min.Y, img.Rect.Min.X+largura, img.Rect.Min.Y+altura))
	lt.Confianca = make(map[string]float64, 40)

	quadrosOK := l.leQuadros(img, e, &lt)
	lt.PainelPresente = faixaEscura(img, e.r(rSeparador1)) && faixaEscura(img, e.r(rSeparador2))
	if lt.PainelPresente {
		l.lePainel(img, e, &lt)
	}
	if !lt.PainelPresente && quadrosOK == 0 {
		return rastreio.Leitura{}, ErrHUD
	}
	return lt, nil
}

// faixaEscura: a linha separadora do painel é cinza-quase-preta uniforme.
func faixaEscura(img *image.RGBA, r image.Rectangle) bool {
	n, ok := 0, 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			i := img.PixOffset(x, y)
			p := img.Pix[i : i+3 : i+3]
			n++
			if max(p[0], p[1], p[2]) <= 0x22 && max(p[0], p[1], p[2])-min(p[0], p[1], p[2]) <= 8 {
				ok++
			}
		}
	}
	return n > 0 && ok*10 >= n*9
}

func (l *Leitor) lePainel(img *image.RGBA, e escala, lt *rastreio.Leitura) {
	c := lt.Confianca
	if a, b, k, ok := lePar(&l.vida, img, e.r(rVida)); ok {
		c["vida"] = k
		if k >= confMinima && a <= b && b > 0 {
			lt.Vida, lt.VidaMax = &a, &b
		}
	} else {
		c["vida"] = k
	}
	if a, b, k, ok := lePar(&l.mana, img, e.r(rMana)); ok {
		c["mana"] = k
		if k >= confMinima && a <= b && b > 0 {
			lt.Mana, lt.ManaMax = &a, &b
		}
	} else {
		c["mana"] = k
	}
	if v, k, ok := leNumero(&l.nivel, img, e.r(rNivel), false); ok {
		c["nivel"] = k
		if k >= confMinima && v >= 1 && v <= 18 && v == float64(int(v)) {
			n := int(v)
			lt.Nivel = &n
		}
	}
	atr := []struct {
		nome    string
		r       ret
		dst     **float64
		decimal bool
		maximo  float64
	}{
		{"atr_ad", rAD, &lt.Atr.AD, false, 5000},
		{"atr_ap", rAP, &lt.Atr.AP, false, 5000},
		{"atr_armadura", rArmadura, &lt.Atr.Armadura, false, 2000},
		{"atr_rm", rRM, &lt.Atr.RM, false, 2000},
		{"atr_vel_ataque", rVelAtaque, &lt.Atr.VelAtaque, true, 10},
		{"atr_vel_mov", rVelMov, &lt.Atr.VelMov, false, 2000},
	}
	for _, a := range atr {
		v, k, ok := leNumero(&l.atr, img, e.r(a.r), a.decimal)
		c[a.nome] = k
		if ok && k >= confMinima && v <= a.maximo {
			*a.dst = &v
		}
	}
	cds := []struct {
		nome string
		dst  **float64
	}{{"cd_q", &lt.CD.Q}, {"cd_w", &lt.CD.W}, {"cd_e", &lt.CD.E}, {"cd_r", &lt.CD.R}, {"cd_d", &lt.CD.D}, {"cd_f", &lt.CD.F}}
	for i, cd := range cds {
		v, k, ok := l.leRecarga(img, e, i)
		c[cd.nome] = k
		if ok {
			*cd.dst = &v
		}
	}
}

// IconeColorido: fração mínima de pixels saturados no miolo do ícone para ele
// contar como pronto. Ícone em recarga, bloqueado ou não aprendido é cinza
// puro (r = g = b); a moldura amarela de habilidade ativa fica fora do miolo.
const IconeColorido = 0.12

// saturacao devolve a fração de pixels com cor (max−min > 30).
func saturacao(img *image.RGBA, r image.Rectangle) float64 {
	n, s := 0, 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		i := img.PixOffset(r.Min.X, y)
		for x := r.Min.X; x < r.Max.X; x++ {
			p := img.Pix[i : i+3 : i+3]
			if max(p[0], p[1], p[2])-min(p[0], p[1], p[2]) > 30 {
				s++
			}
			n++
			i += 4
		}
	}
	if n == 0 {
		return 0
	}
	return float64(s) / float64(n)
}

// leRecarga: ícone colorido = pronto (0). Cinza com número = recarga. Cinza
// sem número: nível 0 embaixo = não aprendida; senão bloqueada (nulo).
func (l *Leitor) leRecarga(img *image.RGBA, e escala, i int) (float64, float64, bool) {
	sat := saturacao(img, e.r(rIcone(i)))
	if sat >= IconeColorido {
		// Confiança cresce com a distância do limiar.
		return 0, min(1, 0.7+sat), true
	}
	gs := componentes(img, e.r(rRecarga(i)), &l.recarga)
	if len(gs) > 0 {
		txt, k := l.recarga.texto(gs)
		v, ok := numero(txt, true)
		if !ok || k < confMinima || v > 600 {
			return 0, k, false
		}
		return v, k, true
	}
	if i >= 4 {
		return 0, 0.5, false // feitiço cinza sem número: bloqueado
	}
	gs = componentes(img, e.r(rNivelHab(i)), &l.nivelHab)
	txt, k := l.nivelHab.texto(gs)
	if txt == "0" && k >= confMinima {
		return rastreio.RecargaNaoAprendida, k, true
	}
	return 0, k, false
}

// leNumero lê um número inteiro (ou decimal, se permitido) da região.
func leNumero(f *fonte, img *image.RGBA, r image.Rectangle, decimal bool) (float64, float64, bool) {
	gs := componentes(img, r, f)
	txt, k := f.texto(gs)
	v, ok := numero(txt, decimal)
	return v, k, ok
}

func numero(txt string, decimal bool) (float64, bool) {
	if txt == "" || strings.ContainsAny(txt, "?/") {
		return 0, false
	}
	if strings.Contains(txt, ".") {
		if !decimal || strings.Count(txt, ".") > 1 || txt[0] == '.' || txt[len(txt)-1] == '.' {
			return 0, false
		}
	} else if len(txt) > 1 && txt[0] == '0' {
		return 0, false // "05" não existe no HUD
	}
	v, err := strconv.ParseFloat(txt, 64)
	return v, err == nil
}

// lePar lê "a / b". A barra às vezes não passa do limiar (sobre a barra
// azul-clara); sem ela, divide no maior espaço entre glifos.
func lePar(f *fonte, img *image.RGBA, r image.Rectangle) (float64, float64, float64, bool) {
	gs := componentes(img, r, f)
	if len(gs) < 3 {
		return 0, 0, 0, false
	}
	txt, k := f.texto(gs)
	var sa, sb string
	if i := strings.IndexByte(txt, '/'); i >= 0 {
		sa, sb = txt[:i], txt[i+1:]
	} else {
		// Maior espaço entre glifos vizinhos.
		maior, onde := -1, -1
		for j := 1; j < len(gs); j++ {
			if d := gs[j].x0 - (gs[j-1].x0 + gs[j-1].w); d > maior {
				maior, onde = d, j
			}
		}
		sa, sb = txt[:onde], txt[onde:]
	}
	a, ok1 := numero(sa, false)
	b, ok2 := numero(sb, false)
	return a, b, k, ok1 && ok2
}
