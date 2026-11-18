package replay

import (
	"image"
	"math"
)

// Geometria do minimapa e do painel "Fog of War" do replay, na tela de
// referência 2560×1440 com o HUD e o minimapa na escala padrão.
//
// A primeira medida (2026-11-17, partida 3304217746, 10:03 pausado) ajustou
// px = 2206,79 + 0,022681·x, py = 1423,53 − 0,023068·y com 6 retratos de UMA
// captura (GeometriaMinimapa20261117): 1,05 px de erro ali, mas o ajuste com
// poucos pontos perto do centro errava as escalas (~0,8% em cada eixo, até
// ~3 px nos cantos).
//
// Refeita em 2026-11-18 (descobertas 9) com os passes de visão da partida
// 3305449546 a 8×: ~38 mil pares "anel aliado achado ↔ posição rastreada do
// mesmo jogador", os dois times, o mapa inteiro (fora a 2 500 unidades das
// fontes, onde a posição "base" é a fonte fixa e não a de verdade), com a
// posição rastreada tomada AtrasoMinimapaMs antes do carimbo da captura:
//
//	px = 2205,37 + 0,022861·x      py = 1422,54 − 0,022893·y
//
// Erro quadrático médio 1,68 px, mediana 0,71 px, 95% abaixo de 3,5 px; a
// geometria velha nos mesmos pares: 1,93 / 1,20 / 3,7 (e 2,45 / 1,81 / 4,5
// sem a correção do atraso). É linear: o resíduo médio em cada nono do mapa
// fica abaixo de 0,8 px, e um ajuste com termos quadráticos e cruzados só
// baixa o erro de 1,68 para 1,66 px. As escalas dos dois eixos saem iguais a
// 0,1% (o minimapa é quadrado): 1 px ≈ 43,7 unidades. Só 2560×1440 foi medido.
//
// Esta geometria fica no pacote replay porque o jogo falso (falso.go) desenha
// o minimapa com ela para os testes; quem LÊ é o pacote visao.
const (
	MinimapaX0  = 2205.37
	MinimapaXPx = 0.022861
	MinimapaY0  = 1422.54
	MinimapaYPx = 0.022893
	// RaioRetratoMinimapa: raio do anel colorido em volta do retrato do
	// campeão (centro da linha do anel), em pixels; a linha tem ~2 px.
	RaioRetratoMinimapa = 14.5
	// AtrasoMinimapaMs: o minimapa capturado mostra os campeões onde estavam
	// tanto tempo REAL antes do carimbo da captura (a média dos dois playback
	// em volta dela). Medido em 2026-11-18 (descobertas 9) a 8×: o erro dos
	// ~38 mil pares é mínimo com a posição rastreada 175 ms de jogo antes
	// (mediana 0,71 px; 1,54 px sem deslocar; ±100 ms de jogo em volta do
	// mínimo pioram de 0,1–0,2 px), ~22 ms reais — pouco mais de um quadro a
	// 60 Hz: é o quadro que o PrintWindow devolve, já composto. Tomado como
	// tempo real (a captura e o desenho andam no relógio da máquina); só a
	// 8× foi medido: a outra velocidade é hipótese.
	AtrasoMinimapaMs = 22.0
)

// GeometriaMinimapa são os coeficientes de MapaParaMinimapa (referência
// 2560×1440): px = X0 + XPx·x, py = Y0 − YPx·y.
type GeometriaMinimapa struct{ X0, XPx, Y0, YPx float64 }

var (
	// GeometriaMinimapaAtual é a que MapaParaMinimapa usa.
	GeometriaMinimapaAtual = GeometriaMinimapa{MinimapaX0, MinimapaXPx, MinimapaY0, MinimapaYPx}
	// GeometriaMinimapa20261117 é a primeira medida (6 pontos de uma captura).
	// As capturas de visão gravadas com ela (arquivo sem a linha da geometria)
	// guardam as marcas em posições do mapa calculadas por ela.
	GeometriaMinimapa20261117 = GeometriaMinimapa{2206.79, 0.022681, 1423.53, 0.023068}
)

// ParaAtual leva uma posição do mapa calculada com a geometria g para a
// posição que a geometria atual dá ao MESMO pixel do minimapa (as marcas
// lidas são pixels; o arquivo as guarda já convertidas).
func (g GeometriaMinimapa) ParaAtual(x, y float64) (float64, float64) {
	px, py := g.X0+g.XPx*x, g.Y0-g.YPx*y
	a := GeometriaMinimapaAtual
	return (px - a.X0) / a.XPx, (a.Y0 - py) / a.YPx
}

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
