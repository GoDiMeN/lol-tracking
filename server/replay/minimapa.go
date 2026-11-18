package replay

import (
	"image"
	"math"
)

// Geometria do minimapa e do painel "Fog of War" do replay, na tela de
// referência 2560×1440 com o HUD e o minimapa na escala padrão. Medida em
// 2026-11-17 na partida 3304217746 (10:03 de jogo, pausado; capturas do
// minimapa com a névoa em "todos", azul e vermelho): os centros de 6 retratos
// achados na captura contra as posições rastreadas dos 6 no mesmo instante
// (amostras dos passes, ~4 por segundo) deram, por mínimos quadrados,
//
//	px = 2206,79 + 0,022681·x      py = 1423,53 − 0,023068·y
//
// com erro quadrático médio de 1,05 px (máximo 2,0 px, no retrato meio coberto
// por outro) — 1 px ≈ 44 unidades do mapa. Os limites do mapa 11 que o projeto
// usa para desenhar o map11.png ((−120,−120)–(14870,14980)) esticados no
// quadrado preto do minimapa (x 2206–2546, y 1083–1423) dão 2,9 px de erro
// médio no mesmo conjunto: o minimapa do jogo não é exatamente o map11.png
// (a escala vertical é ~1,7% maior). Só 2560×1440 foi medido.
//
// Esta geometria fica no pacote replay porque o jogo falso (falso.go) desenha
// o minimapa com ela para os testes; quem LÊ é o pacote visao.
const (
	MinimapaX0  = 2206.79
	MinimapaXPx = 0.022681
	MinimapaY0  = 1423.53
	MinimapaYPx = 0.023068
	// RaioRetratoMinimapa: raio do anel colorido em volta do retrato do
	// campeão (centro da linha do anel), em pixels; a linha tem ~2 px.
	RaioRetratoMinimapa = 14.5
)

// Regiões de referência (2560×1440).
var (
	// RegiaoMinimapa é o recorte do minimapa usado nas medidas (o quadrado
	// preto com o mapa dentro e uma folga em volta).
	RegiaoMinimapa = image.Rect(2195, 1070, 2555, 1435)
	// QuadradoMinimapa é o quadrado preto onde o mapa é desenhado.
	QuadradoMinimapa = image.Rect(2206, 1083, 2545, 1423)
	// RegiaoPainelNevoa é o texto da caixa "Fog of War" do painel do replay
	// (canto inferior esquerdo): "Blue" em azul, "Red" em vermelho, "All" em
	// branco.
	RegiaoPainelNevoa = image.Rect(226, 1353, 300, 1376)
	// RegiaoVisao é o retângulo que o passe de visão captura: cobre o painel e
	// o minimapa (uma captura só; a janela é copiada inteira de todo jeito).
	RegiaoVisao = image.Rect(0, 1070, 2560, 1440)
)

// Cores medidas nas capturas (RGB).
var (
	CorAnelAzul      = [3]uint8{0x00, 0x99, 0xe0}
	CorAnelVermelho  = [3]uint8{0xe8, 0x3d, 0x3d}
	CorTextoAzul     = [3]uint8{0x40, 0xc1, 0xff}
	CorTextoVermelho = [3]uint8{0xff, 0x33, 0x33}
	CorTextoBranco   = [3]uint8{0xc8, 0xc8, 0xc8}
	CorFundoPainel   = [3]uint8{0x18, 0x27, 0x26}
	// Marcador de sentinela no minimapa (o "olho com asas"): vermelho vivo no
	// time vermelho; azul médio, com contorno escuro, no azul.
	CorSentinelaVermelha = [3]uint8{0xde, 0x24, 0x29}
	CorSentinelaAzul     = [3]uint8{0x28, 0x80, 0xba}
)

// MarcadorSentinela é o marcador de sentinela do minimapa ("olho com asas"),
// medido na sentinela vermelha da captura de 10:03 (pixels da cor viva). O
// azul tem o mesmo desenho, com só o contorno na cor (o miolo é azul-escuro):
// a leitura compara com tolerância de 1 px (visao/minimapa.go).
var MarcadorSentinela = []string{
	".....##.....",
	".....###....",
	".....###....",
	".....##.....",
	"###......###",
	".####..####.",
	"...##..##...",
	"....####....",
	".....##.....",
	".....##.....",
}

// MapaParaMinimapa converte uma posição do mapa (unidades do jogo) no pixel
// do minimapa numa tela larg×alt. Só a referência foi medida; outra tela
// escala pela altura (o HUD do LoL escala com a altura) e ancora na borda
// direita, como os quadros laterais — é hipótese, e a leitura recusa outra
// resolução de todo jeito.
func MapaParaMinimapa(x, y float64, larg, alt int) (float64, float64) {
	px := MinimapaX0 + MinimapaXPx*x
	py := MinimapaY0 - MinimapaYPx*y
	if larg == LarguraRef && alt == AlturaRef {
		return px, py
	}
	s := float64(alt) / AlturaRef
	return float64(larg) - (LarguraRef-px)*s, py * s
}

// MinimapaParaMapa é a inversa de MapaParaMinimapa.
func MinimapaParaMapa(px, py float64, larg, alt int) (float64, float64) {
	if larg != LarguraRef || alt != AlturaRef {
		s := float64(alt) / AlturaRef
		px = LarguraRef - (float64(larg)-px)/s
		py = py / s
	}
	return (px - MinimapaX0) / MinimapaXPx, (MinimapaY0 - py) / MinimapaYPx
}

// UnidadesPorPixelMinimapa: quantas unidades do mapa cabem num pixel do
// minimapa (média dos dois eixos), na tela de referência.
func UnidadesPorPixelMinimapa() float64 {
	return (1/MinimapaXPx + 1/MinimapaYPx) / 2
}

// DistanciaMinimapa é a distância em pixels do minimapa entre duas posições do
// mapa (na referência).
func DistanciaMinimapa(x1, y1, x2, y2 float64) float64 {
	return math.Hypot((x1-x2)*MinimapaXPx, (y1-y2)*MinimapaYPx)
}
