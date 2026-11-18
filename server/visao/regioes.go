package visao

import "image"

// Regiões do HUD, medidas em capturas 2560×1440 com o HUD na escala padrão
// (descobertas-replay.md, seção 4.2). Coordenadas de referência; o leitor
// escala pela altura (o HUD do LoL escala com a altura da tela) e ancora os
// quadros da direita na borda direita. Só 2560×1440 foi medido: outra
// resolução dá erro claro em vez de leitura (ver Le).

const (
	refLargura = 2560
	refAltura  = 1440
)

// ret é um retângulo de referência [x0,x1)×[y0,y1).
type ret struct {
	x0, y0, x1, y1 int
	direita        bool // ancorado na borda direita
}

func (r ret) desloca(dy int) ret { r.y0 += dy; r.y1 += dy; return r }

// escala converte a referência para a tela.
type escala struct {
	s        float64
	largura  int
	origemX  int
	origemY  int
	identica bool
}

func novaEscala(rect image.Rectangle) escala {
	h := rect.Dy()
	e := escala{s: float64(h) / refAltura, largura: rect.Dx(), origemX: rect.Min.X, origemY: rect.Min.Y}
	e.identica = rect.Dx() == refLargura && h == refAltura
	return e
}

func (e escala) r(r ret) image.Rectangle {
	if e.identica {
		return image.Rect(r.x0+e.origemX, r.y0+e.origemY, r.x1+e.origemX, r.y1+e.origemY)
	}
	px := func(x int) int { return int(float64(x)*e.s + 0.5) }
	x0, x1 := px(r.x0), px(r.x1)
	if r.direita {
		x0 = e.largura - px(refLargura-r.x0)
		x1 = e.largura - px(refLargura-r.x1)
	}
	return image.Rect(x0+e.origemX, px(r.y0)+e.origemY, x1+e.origemX, px(r.y1)+e.origemY)
}

// --- Painel do jogador selecionado (canto inferior esquerdo) ---

var (
	// Texto "vida / vida_max" sobre a barra verde (glifos nas linhas 1142–1151).
	rVida = ret{x0: 102, y0: 1137, x1: 235, y1: 1158}
	// Texto do recurso sobre a barra azul (glifos nas linhas 1170–1183).
	rMana = ret{x0: 102, y0: 1166, x1: 235, y1: 1188}
	// Linhas escuras fixas do painel: com elas o painel está na tela. A primeira
	// é a borda de baixo da barra de vida; a segunda, a borda de cima da barra
	// de experiência (y 1193). A segunda NÃO pode ser a borda da barra de
	// recurso (y 1189, como era): em campeão sem recurso (Garen) o jogo às
	// vezes nem desenha a caixa dessa barra, e o painel era dado como ausente
	// (partida 3304217746: 176 leituras em 1537 amostras vivas do Garen).
	rSeparador1 = ret{x0: 106, y0: 1160, x1: 230, y1: 1161}
	rSeparador2 = ret{x0: 106, y0: 1193, x1: 230, y1: 1194}
	// Nível no canto do retrato.
	rNivel = ret{x0: 64, y0: 1190, x1: 98, y1: 1213}
	// Atributos: duas colunas de três (o ícone fica fora da região).
	rAD        = ret{x0: 266, y0: 1133, x1: 313, y1: 1154}
	rArmadura  = ret{x0: 266, y0: 1160, x1: 313, y1: 1181}
	rVelAtaque = ret{x0: 266, y0: 1185, x1: 313, y1: 1206}
	rAP        = ret{x0: 336, y0: 1133, x1: 380, y1: 1154}
	rRM        = ret{x0: 336, y0: 1160, x1: 380, y1: 1181}
	rVelMov    = ret{x0: 336, y0: 1185, x1: 380, y1: 1206}
)

// Fileira de ícones: Q, W, E, R, D, F e, por último, a passiva (que na tela é
// a primeira, em 20–63). O número da recarga fica no meio do ícone (linhas
// ~1242–1253); o nível da habilidade, embaixo, saindo do ícone (linhas
// ~1261–1274). A passiva não tem nível; a recarga dela aparece do mesmo jeito
// que nas outras (medido na Vex, partida 3301749762).
var xIcones = [7][2]int{{72, 115}, {124, 167}, {175, 218}, {227, 270}, {293, 336}, {340, 383}, {20, 63}}

// iPassiva é o índice da passiva em xIcones.
const iPassiva = 6

const (
	yIcone0, yIcone1           = 1225, 1268
	yRecarga0, yRecarga1       = 1236, 1259
	yNivelHab0, yNivelHab1     = 1257, 1281
	margemSaturacaoIcone   int = 5
)

func rIcone(i int) ret {
	return ret{x0: xIcones[i][0] + margemSaturacaoIcone, y0: yIcone0 + margemSaturacaoIcone,
		x1: xIcones[i][1] - margemSaturacaoIcone, y1: yIcone1 - margemSaturacaoIcone}
}
func rRecarga(i int) ret {
	return ret{x0: xIcones[i][0], y0: yRecarga0, x1: xIcones[i][1], y1: yRecarga1}
}
func rNivelHab(i int) ret {
	return ret{x0: xIcones[i][0], y0: yNivelHab0, x1: xIcones[i][1], y1: yNivelHab1}
}

// --- Quadros laterais (5 à esquerda, 5 à direita, 137 px entre eles) ---

const passoQuadro = 137

// Barras de 54 px que enchem da esquerda para a direita nos dois lados.
var (
	rBarraVidaE = ret{x0: 42, y0: 277, x1: 96, y1: 281}
	rBarraManaE = ret{x0: 42, y0: 289, x1: 96, y1: 293}
	rBarraVidaD = ret{x0: 2463, y0: 277, x1: 2517, y1: 281, direita: true}
	rBarraManaD = ret{x0: 2463, y0: 289, x1: 2517, y1: 293, direita: true}
	// Faixa escura entre as duas barras: confere que o quadro está onde se espera.
	rEntreBarrasE = ret{x0: 43, y0: 284, x1: 95, y1: 285}
	rEntreBarrasD = ret{x0: 2464, y0: 284, x1: 2516, y1: 285, direita: true}
	// Retrato (o contador vermelho de renascimento aparece por cima).
	rRetratoE = ret{x0: 41, y0: 205, x1: 98, y1: 270}
	rRetratoD = ret{x0: 2462, y0: 205, x1: 2519, y1: 270, direita: true}
)

func quadro(k int) (vida, mana, entre, retrato ret) {
	if k < 5 {
		d := passoQuadro * k
		return rBarraVidaE.desloca(d), rBarraManaE.desloca(d), rEntreBarrasE.desloca(d), rRetratoE.desloca(d)
	}
	d := passoQuadro * (k - 5)
	return rBarraVidaD.desloca(d), rBarraManaD.desloca(d), rEntreBarrasD.desloca(d), rRetratoD.desloca(d)
}
