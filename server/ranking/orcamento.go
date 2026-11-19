package ranking

import (
	"fmt"
	"math"

	"loltracking/server/riot"
)

// ORÇAMENTO de pedidos de uma atualização (1 elo; os mesmos candidatos servem
// às 5 rotas e aos rankings de campeão). Limite da chave de desenvolvimento:
// 20 pedidos/s e 100 a cada 2 min POR HOST (riot.LimitesPedidos): a league-v4
// vai ao host da plataforma (br1) e a account-v1 e a match-v5 ao do
// roteamento (americas). O gargalo é sempre o "100 a cada 2 min" do americas.
//
//   - liga (br1): Mestre para cima 1 pedido; abaixo, 4 divisões × PaginasPorDivisao.
//   - fonte "riot" (americas): por candidato, 1 pedido de ids + 1 por partida
//     não vista, até o teto de partidas + 1 (a primeira de patch mais velho,
//     que encerra). Nenhum de conta: o Riot ID vem na própria partida.
//   - fonte "lcu" (americas): 1 de conta por candidato (cache de 24 h); o
//     histórico é do cliente (sem cota), ~SegundosHistoricoLCU cada.
//
// Pior caso = nada no cache. Do Mestre para cima os jogadores jogam entre si e
// uma partida lida serve a vários: a estimativa desconta FracaoCacheApice.

// Orcamento é o custo previsto de uma atualização.
type Orcamento struct {
	Elo                string `json:"elo"`
	Fonte              string `json:"fonte"`
	Candidatos         int    `json:"candidatos"`
	PartidasPorJogador int    `json:"partidas_por_jogador"`
	// Pedidos por host, no pior caso.
	PedidosPlataforma int `json:"pedidos_plataforma"`
	PedidosRegional   int `json:"pedidos_regional"`
	// PedidosRegionalEstimado: com o desconto do cache (só no ápice, fonte riot).
	PedidosRegionalEstimado int `json:"pedidos_regional_estimado"`
	// Minutos até terminar (pior caso e estimado), pelo limite da chave e, na
	// fonte lcu, pelo tempo do cliente.
	MinutosPiorCaso float64 `json:"minutos_pior_caso"`
	MinutosEstimado float64 `json:"minutos_estimado"`
	Texto           string  `json:"texto"`
}

// minutosPorPedidos: o tempo para n pedidos num host (os primeiros 100 saem
// de uma vez; depois, 100 a cada 2 min; 20/s por cima).
func minutosPorPedidos(n int) float64 {
	t := float64(n) / 20 / 60
	if n > PedidosPorJanela {
		t = math.Max(t, float64(n-PedidosPorJanela)/PedidosPorJanela*JanelaMin)
	}
	return math.Round(t*10) / 10
}

// CalculaOrcamento: o custo de uma atualização de 1 elo.
func CalculaOrcamento(elo, fonte string, candidatos, partidas int) Orcamento {
	o := Orcamento{Elo: elo, Fonte: fonte, Candidatos: candidatos, PartidasPorJogador: partidas}
	apex := riot.EloApex(elo)
	o.PedidosPlataforma = 4 * PaginasPorDivisao
	if apex {
		o.PedidosPlataforma = 1
	}
	if fonte == FonteRiot {
		porCandidato := 1 + partidas + 1
		o.PedidosRegional = candidatos * porCandidato
		o.PedidosRegionalEstimado = o.PedidosRegional
		if apex {
			o.PedidosRegionalEstimado = candidatos * (1 + int(math.Ceil(float64(partidas+1)*(1-FracaoCacheApice))))
		}
		o.MinutosPiorCaso = math.Max(minutosPorPedidos(o.PedidosRegional), minutosPorPedidos(o.PedidosPlataforma))
		o.MinutosEstimado = math.Max(minutosPorPedidos(o.PedidosRegionalEstimado), minutosPorPedidos(o.PedidosPlataforma))
		o.Texto = fmt.Sprintf("fonte riot: %d pedidos à liga (br1) + %d à match-v5 (americas: %d candidatos × (1 de ids + até %d partidas + 1)); %.0f min no pior caso pelo limite de 100 a cada 2 min",
			o.PedidosPlataforma, o.PedidosRegional, candidatos, partidas, o.MinutosPiorCaso)
		if apex {
			o.Texto += fmt.Sprintf("; ~%.0f min estimados com ~%.0f%% das partidas já no cache (eles jogam entre si; estimativa, não medida)",
				o.MinutosEstimado, FracaoCacheApice*100)
		}
		return o
	}
	o.PedidosRegional = candidatos
	o.PedidosRegionalEstimado = candidatos
	lcuMin := math.Round(float64(candidatos)*SegundosHistoricoLCU/60*10) / 10
	o.MinutosPiorCaso = math.Max(minutosPorPedidos(o.PedidosRegional), lcuMin)
	o.MinutosEstimado = o.MinutosPiorCaso
	o.Texto = fmt.Sprintf("fonte lcu: %d pedidos à liga (br1) + %d à account-v1 (americas, 1 por candidato, cache de 24 h); o histórico vem do cliente do LoL (sem cota, ~%.0f s cada, estimado): ~%.1f min",
		o.PedidosPlataforma, o.PedidosRegional, SegundosHistoricoLCU, o.MinutosPiorCaso)
	return o
}
