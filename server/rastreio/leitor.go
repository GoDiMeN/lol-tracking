package rastreio

import "image"

// LeitorDeTela é o ponto de extensão da tarefa 3 (pacote visao): a cada
// amostra o motor captura a área do jogo e chama Le. O que vier preenchido
// vai para as colunas da amostra (vida, mana, recargas, atributos) e do
// estado dos 10 (vida_pct, mana_pct); o que vier nil fica NULL.
//
// Regras para quem implementar:
//   - Le roda no laço de amostragem: o custo entra na taxa (meta 4 amostras
//     por segundo de jogo). Medir com a sonda real.
//   - img pode ser reaproveitada pela próxima captura: não guarde a referência.
//   - largura/altura são a área do jogo em pixels físicos (o HUD escala com ela;
//     a referência medida é 2560×1440, descobertas seção 4).
type LeitorDeTela interface {
	Le(img *image.RGBA, largura, altura int) (Leitura, error)
}

// Leitura é o que se leu da tela num instante.
type Leitura struct {
	// Painel do jogador selecionado (canto inferior esquerdo).
	Vida, VidaMax, Mana, ManaMax *float64
	// Recargas em segundos (0 = pronta).
	CD  Recargas
	Atr Atributos
	// Quadros laterais: os 10, na ordem do playerlist (0–4 ORDER, 5–9 CHAOS).
	Quadros [10]QuadroLateral
}

// QuadroLateral é a barra de um jogador nos quadros laterais, em % (0–100).
type QuadroLateral struct {
	VidaPct, ManaPct *float64
}

// LeitorNulo não lê nada (tarefa 2): todas as colunas da tela ficam NULL.
type LeitorNulo struct{}

func (LeitorNulo) Le(*image.RGBA, int, int) (Leitura, error) { return Leitura{}, nil }
