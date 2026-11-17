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
//   - Le é pura (sem estado entre chamadas). A sanidade no tempo (vida_max que
//     não pode pular, recarga que não pode subir do nada) fica no saneador do
//     motor, um por passe (sanidade.go).
//   - Erro = a tela não é a que o leitor conhece (resolução ou escala do HUD
//     diferentes): o motor avisa uma vez e segue com tudo nulo.
type LeitorDeTela interface {
	Le(img *image.RGBA, largura, altura int) (Leitura, error)
}

// Recarga especial: habilidade ainda não aprendida (nível 0). Fica gravada
// assim mesmo nas colunas cd_* (-1 s), para distinguir de "não lido" (nulo).
const RecargaNaoAprendida = -1.0

// Leitura é o que se leu da tela num instante.
type Leitura struct {
	// PainelPresente: o painel do jogador selecionado está na tela (não está
	// quando o jogador morreu com a seleção vazia ou a câmera é a dirigida).
	PainelPresente bool
	// Painel do jogador selecionado (canto inferior esquerdo). Mana/ManaMax é
	// o recurso da barra azul (ou energia, fúria... o que a barra mostrar em
	// texto); nulo quando a barra não tem texto (campeão sem recurso).
	Vida, VidaMax, Mana, ManaMax *float64
	// Nivel do campeão no retrato do painel (confere com o playerlist: se não
	// bate, o painel é de outro jogador e o motor descarta o painel).
	Nivel *int
	// Recargas em segundos (0 = pronta, RecargaNaoAprendida = nível 0, nulo =
	// não deu para ler, inclusive ícone escurecido sem número).
	CD  Recargas
	Atr Atributos
	// Quadros laterais: os 10, na ordem do playerlist (0–4 ORDER, 5–9 CHAOS).
	Quadros [10]QuadroLateral
	// Confianca de cada campo lido, 0–1 (nome do campo como na coluna:
	// "vida", "mana", "cd_q", "atr_ad", "q3_vida_pct"...). Campo abaixo do
	// limiar vem nulo, e a confiança dele fica aqui para diagnóstico.
	Confianca map[string]float64
}

// QuadroLateral é a barra de um jogador nos quadros laterais, em % (0–100).
type QuadroLateral struct {
	VidaPct, ManaPct *float64
	// Morto: contador vermelho de renascimento sobre o retrato. Respawn é o
	// número do contador (segundos, arredondado para cima pelo jogo), quando
	// lido com confiança. Não vão para o banco (o playerlist já tem isDead e
	// respawnTimer exatos); servem de conferência.
	Morto   *bool
	Respawn *float64
}

// LeitorNulo não lê nada (tarefa 2): todas as colunas da tela ficam NULL.
type LeitorNulo struct{}

func (LeitorNulo) Le(*image.RGBA, int, int) (Leitura, error) { return Leitura{}, nil }
