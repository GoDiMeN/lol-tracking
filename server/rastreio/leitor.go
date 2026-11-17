package rastreio

import (
	"image"
	"math"
)

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

// LeitorFalso é o leitor do modo teste (LOL_TESTE=1). A captura do jogo falso
// (replay.Falso.Captura) pinta a imagem com o segundo de jogo módulo 256 no
// canal vermelho; daqui sai uma tela inventada mas coerente com o tempo
// (vida que desce e sobe, recargas que correm, barras dos 10). Serve para a
// interface mostrar a "última leitura" e o mapa com play sem o jogo de
// verdade. Puro: o mesmo pixel dá a mesma leitura.
type LeitorFalso struct{}

func (LeitorFalso) Le(img *image.RGBA, _, _ int) (Leitura, error) {
	if img == nil || img.Bounds().Empty() {
		return Leitura{}, nil
	}
	t := float64(img.RGBAAt(img.Bounds().Min.X, img.Bounds().Min.Y).R)
	f := func(v float64) *float64 { return &v }
	// recarga: pronta na primeira parte do ciclo, depois descendo até 0.
	recarga := func(ciclo, pronta float64) *float64 {
		fase := math.Mod(t, ciclo)
		if fase < pronta {
			return f(0)
		}
		return f(ciclo - fase)
	}
	lt := Leitura{PainelPresente: true,
		Vida: f(math.Round(1000 * (0.55 + 0.45*math.Cos(t*2*math.Pi/64)))), VidaMax: f(1000),
		Mana: f(math.Round(500 * (0.3 + 0.7*math.Mod(t, 128)/128))), ManaMax: f(500),
		CD:  Recargas{Q: recarga(10, 4), W: recarga(16, 6), E: recarga(22, 10), R: recarga(90, 30), D: recarga(256, 200), F: recarga(64, 40)},
		Atr: Atributos{AD: f(80), AP: f(0), Armadura: f(40), RM: f(32), VelAtaque: f(0.72), VelMov: f(345)}}
	for k := range lt.Quadros {
		v := math.Round(10*(55+40*math.Cos((t+float64(k)*20)/30))) / 10
		m := math.Round(10*(60+30*math.Sin((t+float64(k)*15)/40))) / 10
		lt.Quadros[k] = QuadroLateral{VidaPct: f(v), ManaPct: f(m)}
	}
	return lt, nil
}
