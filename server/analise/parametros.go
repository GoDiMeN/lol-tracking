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
	// RaioAcampamento e JanelaAcampamentoMs: a menos disso do centro de um
	// acampamento da selva e com uma habilidade usada até esse tempo antes ou
	// depois = "farmando" (limpando o acampamento), mesmo andando entre um e
	// outro. O caçador limpa um acampamento em poucos segundos e segue: sem
	// isso a regra do deslocamento o dava como "andando" a selva inteira.
	RaioAcampamento     float64
	JanelaAcampamentoMs int64
	// DeslocamentoParado: deslocou menos que isso na janela e não há outro sinal
	// = "parado" (em vez de "andando").
	DeslocamentoParado float64
	// JanelaAproximacaoMs e AproximacaoMinima: o trecho "andando" que termina
	// numa luta vira "gankando"/"roaming" até esse tempo antes dela, se nesse
	// tempo o jogador percorreu ao menos essa distância até o lugar da luta.
	JanelaAproximacaoMs int64
	AproximacaoMinima   float64
	// VelocidadeMaxima (unidades por segundo): aproximação mais rápida que isso
	// não foi andando (teleporte); fica sem a etiqueta.
	VelocidadeMaxima float64
	// JanelaContatoDepoisMs e MargemCounterMs: o counter gank. O primeiro contato
	// de cada caçador com um inimigo de rota é procurado da janela da aproximação
	// até esse tempo depois do começo da luta; quem chega ao contato ao menos
	// MargemCounterMs depois do outro é o counter gank (menos que isso: chegaram
	// juntos, os dois ficam gankando).
	JanelaContatoDepoisMs int64
	MargemCounterMs       int64
	// TrechoMinimoMs: trecho de atividade "andando"/"farmando" mais curto que
	// isso é absorvido pelo anterior (tira o pisca-pisca).
	TrechoMinimoMs int64
	// HistereseAtividadeMs: tempo mínimo de uma atividade nova para a etiqueta
	// trocar entre atividades vizinhas da mesma briga (luta, troca, dive, torre).
	HistereseAtividadeMs int64
	// RaioSuperioridade: aliados e inimigos a menos disso contam no retrato do
	// abate (superioridade numérica).
	RaioSuperioridade float64
	// VidaBaixaPct: abaixo disso é "vida baixa" (indicadores).
	VidaBaixaPct float64
	// VidaSaidaArriscadaPct: saída de rota com menos que isso de vida é
	// arriscada (indicador mortes_saida_vida_baixa).
	VidaSaidaArriscadaPct float64

	// --- Etiquetas recall, dive, split push e agrupado (geradores.go).

	// DuracaoRecallMs, RaioCanalizacao e CanalizacaoMinimaMs: o recall. A
	// canalização dura 8 s (League of Legends Wiki, "Recall", conferido em
	// 17/11/2026; 4 s com o Barão). O trecho parado (sem sair mais que
	// RaioCanalizacao do ponto de onde sumiu: o erro da posição é ~30–100) que
	// termina no salto para a fonte vira "recall", no máximo DuracaoRecallMs (+
	// 0,5 s de folga da leitura) antes do salto; parado menos que
	// CanalizacaoMinimaMs não é canalização (o recall mais curto, com o Barão,
	// leva 4 s).
	DuracaoRecallMs     int64
	RaioCanalizacao     float64
	CanalizacaoMinimaMs int64
	// AlcanceTorre: a menos disso do centro de uma torre inimiga de pé, luta ou
	// troca é "dive". O alcance da torre é 750 a partir da borda dela (Wiki); o
	// raio da torre (~88) e o do campeão (~65) dão ~900 de centro a centro.
	AlcanceTorre float64
	// DistanciaSplitAliados: split push = depois da fase de rotas, sozinho
	// (nenhum aliado a menos de AlcanceLuta) numa rota lateral, além do ponto de
	// encontro das torres externas (o rio cruza a rota ali) para o lado inimigo,
	// farmando ou em torre, com pelo menos três aliados mortos ou a mais que isso.
	DistanciaSplitAliados float64
	// DistanciaAgrupado: agrupado = ele e mais três ou quatro aliados vivos, todos
	// a menos disso uns dos outros (dois a dois), sem luta nem objetivo. Um time
	// agrupado no meio ou no rio ocupa ~1 000–1 500 de diâmetro.
	DistanciaAgrupado float64

	// --- Ondas de tropas (ondas.go). A posição da onda é deduzida de onde os
	// jogadores da rota estão; nenhuma tropa é rastreada. Medidos/escolhidos
	// contra a partida 3304217746 (freeze do topo de ~4:30 a ~8:00).

	// PassoOndaMs: de quanto em quanto tempo a posição estimada da onda é
	// calculada (a tela interpola entre um ponto e outro).
	PassoOndaMs int64
	// SuavizaOndaMs: a posição de cada instante é a mediana das estimativas
	// cruas nessa janela em volta dele. O jogador vai e volta para dar o último
	// golpe; sem a mediana a onda "tremia" centenas de unidades por segundo.
	SuavizaOndaMs int64
	// MeiaDistanciaOnda: com um time só presente, a onda fica à frente dele por
	// essa distância (o jogador fica atrás das próprias tropas). Medido na
	// partida 3304217746: com os dois lados na rota, a meia distância entre os
	// dois foi de ~450–700 na fase de rotas; 500 é estimativa.
	MeiaDistanciaOnda float64
	// SeparacaoMaximaPar: com os dois lados presentes e mais longe que isso um
	// do outro ao longo da rota (ou o azul à frente do vermelho por mais de
	// MeiaDistanciaOnda), eles não estão na mesma onda: a confiança cai para média.
	SeparacaoMaximaPar float64
	// JanelaLutaOndaMs e RaioLutaOnda: um abate até esse tempo antes/depois, a
	// até esse raio do jogador, tira a amostra da estimativa (na luta ninguém
	// está na onda).
	JanelaLutaOndaMs int64
	RaioLutaOnda     float64
	// CoberturaJanelaOnda: fração mínima de uma janela de onda (o intervalo até
	// a próxima onda chegar) com posição estimada para a janela ter posição.
	CoberturaJanelaOnda float64
	// AlcanceTorreOnda: onda a menos disso da torre (ao longo da rota) está
	// batendo na torre. O alcance da torre é 750 a partir da borda dela (Wiki);
	// a posição estimada é o meio entre os dois jogadores e o defensor fica
	// embaixo da torre, por isso a folga.
	AlcanceTorreOnda float64
	// AvancoEmpurrando e AvancoRapido: avanço da posição de uma janela de onda
	// para a seguinte (unidades por onda). Abaixo de AvancoEmpurrando a onda
	// está parada (dentro do erro da estimativa, ~±400); de AvancoRapido para
	// cima o empurrão é rápido (a onda vai do meio à torre, ~3 000, em 2 ondas).
	// Sem contar tropas, "devagar/rápido" é SÓ a velocidade do avanço.
	AvancoEmpurrando float64
	AvancoRapido     float64
	// OndasFreeze, FaixaFreeze e DeslocamentoFreeze: freeze é a onda parada
	// por OndasFreeze ondas ou mais, com todas as janelas dentro de uma faixa de
	// FaixaFreeze unidades, fora do alcance da torre e puxada para perto da torre
	// de um time por ao menos DeslocamentoFreeze em relação ao ponto de encontro
	// natural (onda parada no meio da rota não é freeze de ninguém).
	OndasFreeze        int
	FaixaFreeze        float64
	DeslocamentoFreeze float64
	// JanelaPresencaOndaMs e RaioPresencaOnda: a onda chegou ao ponto de
	// encontro; o jogador da rota que não passar, nesse tempo, a menos desse
	// raio (ao longo da rota) da posição da onda, vivo e na rota, perdeu a onda.
	JanelaPresencaOndaMs int64
	RaioPresencaOnda     float64
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
	DeslocamentoParado:    250,
	JanelaAproximacaoMs:   25000,
	AproximacaoMinima:     1500,
	VelocidadeMaxima:      800,
	RaioAcampamento:       800,
	JanelaAcampamentoMs:   3000,
	JanelaContatoDepoisMs: 5000,
	MargemCounterMs:       500,
	TrechoMinimoMs:        3000,
	HistereseAtividadeMs:  4000,
	RaioSuperioridade:     1500,
	VidaBaixaPct:          30,
	VidaSaidaArriscadaPct: 40,
	DuracaoRecallMs:       8000,
	RaioCanalizacao:       200,
	CanalizacaoMinimaMs:   3500,
	AlcanceTorre:          900,
	DistanciaSplitAliados: 5000,
	DistanciaAgrupado:     2000,
	PassoOndaMs:           1000,
	SuavizaOndaMs:         10000,
	MeiaDistanciaOnda:     500,
	SeparacaoMaximaPar:    2500,
	JanelaLutaOndaMs:      8000,
	RaioLutaOnda:          2000,
	CoberturaJanelaOnda:   0.3,
	AlcanceTorreOnda:      1000,
	AvancoEmpurrando:      450,
	AvancoRapido:          1200,
	OndasFreeze:           2,
	FaixaFreeze:           900,
	DeslocamentoFreeze:    400,
	JanelaPresencaOndaMs:  25000,
	RaioPresencaOnda:      1800,
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
