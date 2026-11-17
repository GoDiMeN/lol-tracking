package analise

// Parametros das regras da análise. Todos são deduções com limiar escolhido à
// mão (medidos contra a partida 3304594274, docs/decisoes); mudar aqui e
// chamar Recalcula refaz tudo.
type Parametros struct {
	// HistereseZonaMs: tempo mínimo na zona nova para valer a troca de zona
	// (o erro da posição é ~30–100 unidades e as rotas encostam no rio).
	HistereseZonaMs int64
	// SaidaMinimaMs: tempo mínimo fora da rota (rotador) ou dentro de uma rota
	// (caçador) para virar saída/visita.
	SaidaMinimaMs int64
	// FaseRotasMs e FaseMeioMs dividem a partida em fases: rotas (até 14 min),
	// meio (14–25) e final.
	FaseRotasMs int64
	FaseMeioMs  int64
	// AlcanceTroca: inimigo a menos disso está "ao alcance" (o alcance dos
	// campeões vai de 125 a ~650; mais o que andam em 1 s).
	AlcanceTroca float64
	// AlcanceLuta: raio de quem conta como envolvido numa luta.
	AlcanceLuta float64
	// QuedaVidaPct: queda mínima de vida (pontos percentuais do máximo) dentro
	// de JanelaVidaMs para "a vida está caindo".
	QuedaVidaPct float64
	JanelaVidaMs int64
	// RaioObjetivo e JanelaObjetivoMs: perto do lugar de um objetivo/torre
	// derrubado e até esse tempo antes dele = "objetivo"/"torre".
	RaioObjetivo     float64
	JanelaObjetivoMs int64
	// CSMinimoPorMin: ritmo de CS (linha do tempo, no minuto) para "farmando".
	CSMinimoPorMin float64
	// JanelaDeslocamentoMs e DeslocamentoAndando: andou mais que isso em linha
	// reta na janela = está indo para algum lugar, não farmando (a ~350 de
	// velocidade, 4 s andando reto são ~1400).
	JanelaDeslocamentoMs int64
	DeslocamentoAndando  float64
	// TrechoMinimoMs: trecho de atividade "andando"/"farmando" mais curto que
	// isso é absorvido pelo anterior (tira o pisca-pisca).
	TrechoMinimoMs int64
	// RaioSuperioridade: aliados e inimigos a menos disso contam no retrato do
	// abate (superioridade numérica).
	RaioSuperioridade float64
	// VidaBaixaPct: abaixo disso é "vida baixa" (indicadores).
	VidaBaixaPct float64
	// VidaSaidaArriscadaPct: saída de rota com menos que isso de vida é
	// arriscada (indicador mortes_saida_vida_baixa).
	VidaSaidaArriscadaPct float64
}

// Param são os parâmetros em uso.
var Param = Parametros{
	HistereseZonaMs:       3000,
	SaidaMinimaMs:         6000,
	FaseRotasMs:           14 * 60000,
	FaseMeioMs:            25 * 60000,
	AlcanceTroca:          1000,
	AlcanceLuta:           1400,
	QuedaVidaPct:          4,
	JanelaVidaMs:          2000,
	RaioObjetivo:          1500,
	JanelaObjetivoMs:      40000,
	CSMinimoPorMin:        3,
	JanelaDeslocamentoMs:  4000,
	DeslocamentoAndando:   1000,
	TrechoMinimoMs:        3000,
	RaioSuperioridade:     1500,
	VidaBaixaPct:          30,
	VidaSaidaArriscadaPct: 40,
}

// Fase do instante: "rotas", "meio" ou "final".
func Fase(tMs int64) string {
	switch {
	case tMs < Param.FaseRotasMs:
		return "rotas"
	case tMs < Param.FaseMeioMs:
		return "meio"
	}
	return "final"
}

// Fases é o vocabulário de Fase.
var Fases = []string{"rotas", "meio", "final"}
