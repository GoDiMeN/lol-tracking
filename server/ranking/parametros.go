package ranking

// PARÂMETROS DO RANKING: todos os pesos, limiares e tetos num lugar só. A
// fórmula está escrita em docs/arquitetura/dados.md ("ranking") e a tela mostra
// esta mesma tabela (GET /api/rankings/formula). Mudou um número que muda a
// ordem: suba VersaoRegra (os rankings velhos ficam com a versão deles).

// VersaoRegra vai no cabeçalho de cada ranking.
const VersaoRegra = 1

// Tipos de ranking.
const (
	TipoRota    = "rota"
	TipoCampeao = "campeao"
)

// Modos de ordenação: "melhor" (posição e consistência: quem está no topo e
// joga bem a rota) e "subindo" (o critério do dono: quem tem a ação necessária
// para subir, não o travado no elo).
const (
	ModoMelhor  = "melhor"
	ModoSubindo = "subindo"
)

// Modos na ordem da tela.
var Modos = []string{ModoSubindo, ModoMelhor}

// Componentes da nota. Cada um vale de 0 a 1; a nota é 100 × Σ peso × valor.
const (
	// CompLiga: posição na liga. Abaixo de Mestre, (divisão + PDL) de IV 0 PDL
	// (0) a I 100 PDL (1): ((índice da divisão, IV=0..I=3) × 100 + PDL) ÷ 400.
	// Mestre para cima, PDL ÷ maior PDL da liga lida (o primeiro do elo vale 1).
	CompLiga = "liga"
	// CompVitTemporada: vitórias na temporada (league-v4), pelo limite inferior
	// de Wilson a 99% e na escala WilsonPiso..WilsonTeto.
	CompVitTemporada = "vitorias_temporada"
	// CompVitRecorte: vitórias nas partidas do patch no recorte (na rota, ou com
	// o campeão), pelo mesmo Wilson e a mesma escala.
	CompVitRecorte = "vitorias_recorte"
	// CompVolTemporada: partidas na temporada ÷ VolumeTemporadaRef (teto 1).
	CompVolTemporada = "volume_temporada"
	// CompVolRecorte: partidas do patch no recorte ÷ VolumeRecorteRef (teto 1).
	CompVolRecorte = "volume_recorte"
	// CompFracao: fração das partidas do patch que foram no recorte (na rota =
	// "é a função principal dele"; com o campeão = "é o campeão dele").
	CompFracao = "fracao_recorte"
	// CompSinais: sinais de subida da league-v4: 0,5 se hotStreak (sequência de
	// vitórias) + 0,5 se freshBlood (chegou há pouco nesta liga).
	CompSinais = "sinais_subida"
	// CompRecentes: vitórias nas RecentesK partidas mais recentes do recorte ÷
	// RecentesK (quem tem menos de K partidas conta as que faltam como não vitória).
	CompRecentes = "vitorias_recentes"
	// CompVeterano: 1 se veteran (muitas partidas na mesma liga: travado). Peso negativo.
	CompVeterano = "veterano"
)

// NomeComponente: o rótulo da tela.
var NomeComponente = map[string]string{
	CompLiga:         "posição na liga",
	CompVitTemporada: "vitórias na temporada",
	CompVitRecorte:   "vitórias no patch (recorte)",
	CompVolTemporada: "volume na temporada",
	CompVolRecorte:   "volume no patch (recorte)",
	CompFracao:       "fração no recorte",
	CompSinais:       "sinais de subida",
	CompRecentes:     "vitórias recentes",
	CompVeterano:     "veterano (travado)",
}

// ComoCalcula: a regra de cada componente, como a tela mostra.
var ComoCalcula = map[string]string{
	CompLiga:         "abaixo de Mestre: (divisão IV=0, III=1, II=2, I=3) × 100 + PDL, dividido por 400; de Mestre para cima: PDL ÷ o maior PDL da liga",
	CompVitTemporada: "limite inferior de Wilson (99%) de vitórias ÷ partidas da temporada (league-v4), na escala 0,35 → 0 e 0,65 → 1",
	CompVitRecorte:   "o mesmo Wilson, com as vitórias e partidas do patch no recorte (na rota, ou com o campeão)",
	CompVolTemporada: "partidas da temporada ÷ 200 (teto 1)",
	CompVolRecorte:   "partidas do patch no recorte ÷ 20 (teto 1)",
	CompFracao:       "partidas do patch no recorte ÷ partidas do patch (é a rota/o campeão dele?)",
	CompSinais:       "0,5 se em sequência de vitórias (hotStreak) + 0,5 se recém-chegado à liga (freshBlood)",
	CompRecentes:     "vitórias nas 5 partidas mais recentes do recorte ÷ 5",
	CompVeterano:     "1 se a Riot marca veterano (muitas partidas na mesma liga); peso negativo",
}

// Peso de um componente num modo.
type Peso struct {
	Componente string  `json:"componente"`
	Peso       float64 `json:"peso"`
}

// Pesos: tipo → modo → pesos. Os positivos somam 1 em cada modo (nota máxima
// 100); o veterano tira até 10 pontos no "subindo".
//
// Por que estes números:
//   - "melhor" na rota: a posição na liga é o que mais pesa (30%: é o que o elo
//     diz de quem está no topo), mas sozinha ela premia quem chegou lá e parou;
//     as vitórias no patch na rota (25%) e na temporada (15%) medem a
//     consistência; a fração na rota (15%) garante que é a função dele, não
//     uma partida de passagem; volume (10% + 5%) separa quem joga de quem
//     tem 3 partidas boas.
//   - "subindo" na rota: o que mais pesa é ganhar AGORA: vitórias no patch na
//     rota (25%), as 5 mais recentes (20%) e os sinais da liga (20%:
//     sequência de vitórias e recém-chegado). A posição na liga quase não
//     conta (5%, só desempata quem está colado): quem está no I 90 PDL há 300
//     partidas não é quem ensina a subir. Veterano tira 10.
//   - campeão: a mesma ideia, com o recorte "partidas com o campeão" no lugar
//     da rota, e o volume com ele pesando mais (é o que faz de alguém um
//     especialista).
var Pesos = map[string]map[string][]Peso{
	TipoRota: {
		ModoMelhor: {
			{CompLiga, 0.30}, {CompVitRecorte, 0.25}, {CompVitTemporada, 0.15}, {CompFracao, 0.15},
			{CompVolRecorte, 0.10}, {CompVolTemporada, 0.05},
		},
		ModoSubindo: {
			{CompVitRecorte, 0.25}, {CompRecentes, 0.20}, {CompSinais, 0.20}, {CompVitTemporada, 0.10},
			{CompFracao, 0.10}, {CompVolRecorte, 0.10}, {CompLiga, 0.05}, {CompVeterano, -0.10},
		},
	},
	TipoCampeao: {
		ModoMelhor: {
			{CompVitRecorte, 0.30}, {CompLiga, 0.25}, {CompVolRecorte, 0.20}, {CompFracao, 0.15},
			{CompVitTemporada, 0.10},
		},
		ModoSubindo: {
			{CompVitRecorte, 0.30}, {CompSinais, 0.20}, {CompRecentes, 0.15}, {CompVolRecorte, 0.15},
			{CompFracao, 0.10}, {CompVitTemporada, 0.10}, {CompVeterano, -0.10},
		},
	},
}

// Limiares e escalas.
const (
	// ZWilson: 99% de confiança. Com 95% (z = 1,96) 5 vitórias em 5 (0,566)
	// passariam na frente de 60 em 100 (0,502); com 99% ficam 0,43 contra 0,47.
	ZWilson = 2.576
	// WilsonPiso e WilsonTeto: a escala do Wilson (abaixo do piso = 0, acima do
	// teto = 1). Wilson cru fica entre ~0,35 e ~0,60 em quase todo mundo:
	// sem a escala, a diferença entre o melhor e o pior seria pequena perto da
	// posição na liga.
	WilsonPiso = 0.35
	WilsonTeto = 0.65
	// VolumeTemporadaRef: partidas na temporada que valem 1 no volume.
	VolumeTemporadaRef = 200
	// VolumeRecorteRef: partidas do patch no recorte que valem 1 (= o teto de
	// partidas lidas por jogador: quem chegou no teto tem o volume cheio).
	VolumeRecorteRef = 20
	// RecentesK: quantas partidas mais recentes do recorte contam em "vitórias recentes".
	RecentesK = 5
	// MinPartidasRecorte: menos que isto no patch no recorte e o jogador fica
	// fora daquele ranking ("poucas partidas na rota").
	MinPartidasRecorte = 3
	// AmostraPequena: com menos que isto no recorte, a posição sai marcada
	// "amostra pequena" (entra, mas a tela avisa).
	AmostraPequena = 8
	// MinJogadoresCampeao: um ranking de campeão só é gravado com pelo menos
	// tantos jogadores qualificados (com 1 só não há ranking, há um nome).
	MinJogadoresCampeao = 2
	// DuracaoMinimaS: partida mais curta que isto (remake) não conta.
	DuracaoMinimaS = 5 * 60
	// CasasNota: a nota é arredondada a 2 casas; notas iguais assim são EMPATE
	// (dito na posição), desempatado por mais partidas no recorte, depois mais
	// partidas na temporada, depois PDL.
	CasasNota = 2
)

// Tetos de uma atualização (pedidos à Riot; ver Orcamento).
const (
	// FilaSolo: só ranqueada solo/duo conta no ranking.
	FilaSolo = 420
	// TetoCandidatosPadrao: quantos candidatos do elo têm o histórico lido. Os
	// mesmos candidatos servem às 5 rotas e aos rankings de campeão.
	TetoCandidatosPadrao = 60
	// MaxCandidatos: o teto do pedido (300 = o Desafiante inteiro do BR).
	MaxCandidatos = 300
	// TetoPartidasPadrao: partidas do patch lidas por jogador (as mais recentes).
	TetoPartidasPadrao = 20
	MaxPartidas        = 40
	// MargemIDs: ids a mais pedidos à match-v5 além do teto (remakes e partidas
	// de patch mais novo que o pedido).
	MargemIDs = 10
	// PaginasPorDivisao: de Diamante para baixo a league-v4 só dá páginas de
	// ~205 jogadores sem ordem; o ranking lê esta amostra de cada divisão
	// (I a IV) e diz no cabeçalho que é amostra, não o elo inteiro.
	PaginasPorDivisao = 2
	// FimHistoricoLCU: quantas partidas do histórico do LCU (todas as filas) a
	// fonte LCU lê. 20 é o que o app sempre usou e foi medido; mais que isso
	// não foi conferido no cliente.
	FimHistoricoLCU = 20
	// ValidadeHistoricoH: a lista de ids de partidas de um jogador vale tantas
	// horas no cache (a retomada e a atualização seguinte não pagam de novo).
	ValidadeHistoricoH = 6
)

// Limite da chave de desenvolvimento por host (riot.LimitesPedidos): 100 a
// cada 2 min é o que manda (20/s nunca é o gargalo). Só o Orcamento usa.
const (
	PedidosPorJanela = 100
	JanelaMin        = 2.0
)

// FracaoCacheApice: a fração de partidas que um candidato de Mestre para
// cima divide com outro já lido (eles jogam entre si). Estimativa para o
// Orcamento, não medida: a tela diz "estimado".
const FracaoCacheApice = 0.4

// SegundosHistoricoLCU: o tempo de um histórico de outro jogador no LCU
// (ESTIMADO, não medido aqui; a coleta já viu passar de 20 s). Só o Orcamento usa.
const SegundosHistoricoLCU = 3.0
