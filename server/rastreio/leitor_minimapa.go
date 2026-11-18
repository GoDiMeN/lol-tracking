package rastreio

import "image"

// LeitorDeMinimapa é o ponto de extensão do passe de visão de time (pacote
// visao): a cada captura o motor chama LeMinimapa com o recorte
// replay.RegiaoVisao (painel "Fog of War" + minimapa). Regras de quem
// implementa, as mesmas do LeitorDeTela: puro, sem guardar a imagem, e melhor
// não achar do que achar errado (anel duvidoso fica de fora; a decisão
// "visível / não visível / indeterminado" é tomada depois, com as posições
// rastreadas, em visao_classifica.go).
type LeitorDeMinimapa interface {
	// LeMinimapa lê a imagem img, que é o retângulo regiao da área do jogo
	// (img.Rect começa em 0,0), numa área de jogo largura×altura. Erro = a
	// tela não é a que a leitura conhece (resolução, minimapa fora do lugar).
	LeMinimapa(img *image.RGBA, regiao image.Rectangle, largura, altura int) (LeituraMinimapa, error)
}

// Modos da névoa no painel do replay.
const (
	ModoNevoaAzul     = "azul"
	ModoNevoaVermelho = "vermelho"
	ModoNevoaTodos    = "todos"
)

// ModoDoTime é o modo do painel que corresponde à visão do time.
func ModoDoTime(time int) string {
	switch time {
	case 100:
		return ModoNevoaAzul
	case 200:
		return ModoNevoaVermelho
	}
	return ModoNevoaTodos
}

// LeituraMinimapa é o que se leu numa captura.
type LeituraMinimapa struct {
	// Modo da névoa lido no painel: azul, vermelho, todos ou "" (não deu).
	Modo string
	// Retratos achados: o anel colorido em volta do retrato de um campeão.
	Retratos []MarcaMinimapa
	// Sentinelas: o marcador de sentinela ("olho com asas") de cada time.
	Sentinelas []MarcaMinimapa
}

// MarcaMinimapa é um retrato ou uma sentinela achada no minimapa.
type MarcaMinimapa struct {
	// Time pela cor: 100 (azul) ou 200 (vermelho).
	Time int `json:"time"`
	// X, Y: a posição no mapa (unidades do jogo) pela geometria do minimapa.
	X float64 `json:"x"`
	Y float64 `json:"y"`
	// Cobertura: fração do anel achada (0–1; retrato meio coberto por outro
	// fica abaixo de 1). Nas sentinelas, a semelhança com o marcador.
	Cobertura float64 `json:"cobertura"`
}
