package riot

import (
	"context"
	"fmt"
	"net/url"
)

// match-v5 para o RANKING do Lane (server/ranking): o histórico de um jogador
// pela API da Riot, sem o cliente do LoL. O estudo vai rodar num servidor sem
// cliente aberto, então o ranking precisa poder ser calculado só com a chave.
// Os dois pedidos vão ao host do roteamento regional (americas para o BR) e
// passam pelo mesmo limitador da league-v4 (liga.go): 20/s e 100 a cada 2 min
// por host.

// MaxIDsPorPedido é o teto de "count" da match-v5 (ids por pedido).
const MaxIDsPorPedido = 100

// ParticipanteV5 é o pedaço de cada participante da match-v5 que o ranking usa.
type ParticipanteV5 struct {
	Puuid    string `json:"puuid"`
	RiotNome string `json:"riotIdGameName"`
	RiotTag  string `json:"riotIdTagline"`
	// TeamPosition: TOP, JUNGLE, MIDDLE, BOTTOM, UTILITY (vazio em modo sem rota).
	TeamPosition string `json:"teamPosition"`
	ChampionID   int    `json:"championId"`
	Win          bool   `json:"win"`
	// Rendição antecipada (remake): a partida não conta.
	GameEndedInEarlySurrender bool `json:"gameEndedInEarlySurrender"`
	TeamEarlySurrendered      bool `json:"teamEarlySurrendered"`
}

// PartidaV5 é o detalhe de uma partida (match-v5 /matches/{id}).
type PartidaV5 struct {
	Metadata struct {
		MatchID string `json:"matchId"`
	} `json:"metadata"`
	Info struct {
		GameID       int64  `json:"gameId"`
		GameCreation int64  `json:"gameCreation"`
		GameDuration int    `json:"gameDuration"` // segundos (desde a 11.20)
		GameVersion  string `json:"gameVersion"`
		QueueID      int    `json:"queueId"`
		MapID        int    `json:"mapId"`
		// Participants: os 10, com o puuid da Riot (o mesmo da league-v4).
		Participants []ParticipanteV5 `json:"participants"`
	} `json:"info"`
}

// IDsPartidas devolve os ids ("BR1_<gameId>") das partidas do jogador, da mais
// nova para a mais velha, na fila dada (0 = todas), a partir de inicio. Um
// pedido; quantidade de 1 a MaxIDsPorPedido.
func (c *Cliente) IDsPartidas(ctx context.Context, reg Regiao, puuid string, fila, inicio, quantidade int) ([]string, error) {
	quantidade = max(1, min(quantidade, MaxIDsPorPedido))
	q := url.Values{}
	q.Set("start", fmt.Sprint(max(0, inicio)))
	q.Set("count", fmt.Sprint(quantidade))
	if fila > 0 {
		q.Set("queue", fmt.Sprint(fila))
	}
	var l []string
	err := c.pedeJSON(ctx, c.host(reg.Roteamento), "/lol/match/v5/matches/by-puuid/"+url.PathEscape(puuid)+"/ids?"+q.Encode(), &l)
	if l == nil {
		l = []string{}
	}
	return l, err
}

// Partida devolve o detalhe de uma partida pelo id da match-v5 ("BR1_<gameId>"). Um pedido.
func (c *Cliente) Partida(ctx context.Context, reg Regiao, id string) (PartidaV5, error) {
	var p PartidaV5
	err := c.pedeJSON(ctx, c.host(reg.Roteamento), "/lol/match/v5/matches/"+url.PathEscape(id), &p)
	return p, err
}

// IDsPartidasAntes é IDsPartidas só com as partidas de antes de antesMs (o
// início de outra partida, em ms): o parâmetro endTime da match-v5, em
// segundos. Usado pelo contexto dos jogadores de uma partida (o histórico de
// ANTES dela). Um pedido; quem chama ainda confere a data de cada partida.
func (c *Cliente) IDsPartidasAntes(ctx context.Context, reg Regiao, puuid string, fila, quantidade int, antesMs int64) ([]string, error) {
	quantidade = max(1, min(quantidade, MaxIDsPorPedido))
	q := url.Values{}
	q.Set("start", "0")
	q.Set("count", fmt.Sprint(quantidade))
	q.Set("endTime", fmt.Sprint(antesMs/1000))
	if fila > 0 {
		q.Set("queue", fmt.Sprint(fila))
	}
	var l []string
	err := c.pedeJSON(ctx, c.host(reg.Roteamento), "/lol/match/v5/matches/by-puuid/"+url.PathEscape(puuid)+"/ids?"+q.Encode(), &l)
	if l == nil {
		l = []string{}
	}
	return l, err
}
