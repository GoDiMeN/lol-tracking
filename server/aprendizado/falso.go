package aprendizado

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"loltracking/server/lcu"
	"loltracking/server/partida"
	"loltracking/server/riot"
)

// RiotFalso é a API da Riot do modo teste e dos testes: determinística, sem
// rede, casada com os jogadores de referência do LCU falso
// (lcu.ReferenciaFalsa). Em cada elo, os 15 de referência com LP decrescente
// pelo n (mas fora de ordem na resposta, como a Riot) e, no topo, um
// "Fantasma" que o cliente não acha (trocou de conta). Os puuids são os da
// Riot ("riot-ref-..."), diferentes dos do LCU, como na vida real.
type RiotFalso struct {
	mu sync.Mutex
	// Recusada simula a chave vencida.
	Recusada bool
	// Pedidos conta os pedidos (Liga, Conta e os 2 de cada EloSoloPorRiotID);
	// PedidosElo, as consultas de elo de um dos 9 (aproveita.go).
	Pedidos    int
	PedidosElo int
	// PedidosIDs e PedidosPartidas: os da match-v5 (o ranking, fonte "riot").
	PedidosIDs      int
	PedidosPartidas int
	// PedidosConta: os da account-v1 por puuid.
	PedidosConta int
	// AntesDoPedido, se não nil, roda antes de cada pedido (o teste pausa no meio).
	AntesDoPedido func()
	lcu           *lcu.Falso
}

// NovoRiotFalso cria a Riot falsa.
func NovoRiotFalso() *RiotFalso { return &RiotFalso{} }

func (f *RiotFalso) Confere(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Recusada {
		return riot.ErrChaveRecusada
	}
	return nil
}

func indiceElo(elo string) int {
	for i, e := range lcu.ElosReferencia {
		if e == elo {
			return i
		}
	}
	return -1
}

func (f *RiotFalso) Liga(ctx context.Context, reg riot.Regiao, elo, divisao string, pagina int) ([]riot.EntradaLiga, error) {
	f.mu.Lock()
	f.Pedidos++
	recusada := f.Recusada
	f.mu.Unlock()
	if recusada {
		return nil, riot.ErrChaveRecusada
	}
	e := indiceElo(elo)
	if e < 0 {
		return nil, riot.ErrNaoEncontradoRiot
	}
	apex := riot.EloApex(elo)
	// Abaixo de Mestre: os de referência na divisão I e os que a filtragem
	// descarta na II; III, IV e as páginas seguintes vazias.
	if !apex && (pagina > 1 || (divisao != "I" && divisao != "II")) {
		return []riot.EntradaLiga{}, nil
	}
	topo := 99
	if apex {
		topo = 1000 + e*400
	}
	var l []riot.EntradaLiga
	if apex || divisao == "II" {
		// Descartados (ou, no ápice, o veterano no fim da fila), com a melhor taxa de todas.
		l = append(l,
			riot.EntradaLiga{Puuid: fmt.Sprintf("riot-veterano-%d", e), Tier: elo, Rank: "II", LP: topo, Vitorias: 95, Derrotas: 5, Veteran: true},
			riot.EntradaLiga{Puuid: fmt.Sprintf("riot-inativo-%d", e), Tier: elo, Rank: "II", LP: topo, Vitorias: 95, Derrotas: 5, Inactive: true})
		if !apex {
			l = append(l,
				riot.EntradaLiga{Puuid: fmt.Sprintf("riot-poucas-%d", e), Tier: elo, Rank: "II", LP: topo, Vitorias: 18, Derrotas: 2},
				riot.EntradaLiga{Puuid: fmt.Sprintf("riot-travado-%d", e), Tier: elo, Rank: "II", LP: topo, Vitorias: 140, Derrotas: 160})
			return l, nil
		}
	}
	l = append(l, riot.EntradaLiga{Puuid: fmt.Sprintf("riot-fantasma-%d", e), Tier: elo, Rank: "I", LP: topo + 5, Vitorias: 85, Derrotas: 15})
	for i := 0; i < lcu.ReferenciasPorElo; i++ {
		// Fora de ordem de propósito: quem chama ordena. Taxa cai com o n;
		// n%5 == 0 é recém-chegado e n%7 == 0 está em sequência de vitórias.
		n := (i * 7) % lcu.ReferenciasPorElo
		l = append(l, riot.EntradaLiga{Puuid: fmt.Sprintf("riot-ref-%d-%d", e, n), Tier: elo, Rank: "I", LP: topo - n*3,
			Vitorias: 70 - 2*n, Derrotas: 30 + 2*n, FreshBlood: n%5 == 0, HotStreak: n%7 == 0})
	}
	return l, nil
}

// EloSoloPorRiotID: os de referência ("Ref <ELO> <n>#br1") têm a entrada que
// a Liga dá a eles; qualquer outro existe e não tem ranqueada solo. Conta 2
// pedidos (account-v1 + league-v4), como o cliente de verdade.
func (f *RiotFalso) EloSoloPorRiotID(ctx context.Context, nome, tag string) (riot.EntradaLiga, bool, error) {
	f.mu.Lock()
	f.Pedidos += 2
	f.PedidosElo++
	recusada := f.Recusada
	f.mu.Unlock()
	if recusada {
		return riot.EntradaLiga{}, false, riot.ErrChaveRecusada
	}
	for e, elo := range lcu.ElosReferencia {
		for n := 0; n < lcu.ReferenciasPorElo; n++ {
			if lcu.ReferenciaFalsa(e, n).Nome != nome {
				continue
			}
			topo := 99
			if riot.EloApex(elo) {
				topo = 1000 + e*400
			}
			return riot.EntradaLiga{Puuid: fmt.Sprintf("riot-ref-%d-%d", e, n), Tier: elo, Rank: "I", LP: topo - n*3,
				Vitorias: 70 - 2*n, Derrotas: 30 + 2*n, FreshBlood: n%5 == 0, HotStreak: n%7 == 0, Fila: riot.FilaSolo}, true, nil
		}
	}
	return riot.EntradaLiga{}, false, nil
}

func (f *RiotFalso) ContaPorPuuid(ctx context.Context, reg riot.Regiao, puuid string) (riot.Conta, error) {
	f.antes()
	f.mu.Lock()
	f.Pedidos++
	f.PedidosConta++
	recusada := f.Recusada
	f.mu.Unlock()
	if recusada {
		return riot.Conta{}, riot.ErrChaveRecusada
	}
	var e, n int
	if _, err := fmt.Sscanf(puuid, "riot-ref-%d-%d", &e, &n); err == nil && e >= 0 && e < len(lcu.ElosReferencia) && n >= 0 && n < lcu.ReferenciasPorElo {
		inv := lcu.ReferenciaFalsa(e, n)
		// Tag em maiúsculas: o LCU acha pelo Riot ID sem diferenciar.
		return riot.Conta{Puuid: puuid, Nome: inv.Nome, Tag: "BR1"}, nil
	}
	if _, err := fmt.Sscanf(puuid, "riot-fantasma-%d", &e); err == nil && e >= 0 && e < len(lcu.ElosReferencia) {
		return riot.Conta{Puuid: puuid, Nome: "Fantasma " + lcu.ElosReferencia[e], Tag: "br1"}, nil
	}
	return riot.Conta{}, riot.ErrNaoEncontradoRiot
}

func (f *RiotFalso) antes() {
	f.mu.Lock()
	fn := f.AntesDoPedido
	f.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// --- match-v5 (o ranking, fonte "riot") ------------------------------------------
//
// A match-v5 falsa é a tradução das partidas do LCU falso: os mesmos jogos, os
// mesmos resultados, com o puuid da Riot ("puuid-..." do LCU vira "riot-...")
// e a teamPosition pela ordem do participante. Assim as duas fontes de
// histórico do ranking dão o mesmo ranking.

func (f *RiotFalso) lcuFalso() *lcu.Falso {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.lcu == nil {
		f.lcu = lcu.NovoFalso()
	}
	return f.lcu
}

// puuidLCUDe: o puuid do LCU falso de um puuid da Riot falsa ("riot-ref-e-n").
func puuidLCUDe(puuid string) (string, bool) {
	var e, n int
	if _, err := fmt.Sscanf(puuid, "riot-ref-%d-%d", &e, &n); err == nil && e >= 0 && e < len(lcu.ElosReferencia) && n >= 0 && n < lcu.ReferenciasPorElo {
		return lcu.ReferenciaFalsa(e, n).Puuid, true
	}
	return "", false
}

// IDsPartidas: as partidas do jogador de referência (as do LCU falso), da mais
// nova para a mais velha, na fila pedida. Quem não é de referência: 404.
func (f *RiotFalso) IDsPartidas(ctx context.Context, reg riot.Regiao, puuid string, fila, inicio, quantidade int) ([]string, error) {
	return f.ids(ctx, reg, puuid, fila, inicio, quantidade, 0)
}

// IDsPartidasAntes: as mesmas, só as criadas antes de antesMs (o endTime da
// match-v5 de verdade).
func (f *RiotFalso) IDsPartidasAntes(ctx context.Context, reg riot.Regiao, puuid string, fila, quantidade int, antesMs int64) ([]string, error) {
	return f.ids(ctx, reg, puuid, fila, 0, quantidade, antesMs)
}

func (f *RiotFalso) ids(ctx context.Context, reg riot.Regiao, puuid string, fila, inicio, quantidade int, antesMs int64) ([]string, error) {
	f.antes()
	f.mu.Lock()
	f.Pedidos++
	f.PedidosIDs++
	recusada := f.Recusada
	f.mu.Unlock()
	if recusada {
		return nil, riot.ErrChaveRecusada
	}
	pl, ok := puuidLCUDe(puuid)
	if !ok {
		return []string{}, riot.ErrNaoEncontradoRiot
	}
	bruto, err := f.lcuFalso().Historico(ctx, pl, 0, 100)
	if err != nil {
		return nil, err
	}
	var h struct {
		Games struct {
			Games []struct {
				GameID       int64 `json:"gameId"`
				GameCreation int64 `json:"gameCreation"`
				QueueID      int   `json:"queueId"`
			} `json:"games"`
		} `json:"games"`
	}
	if err := json.Unmarshal(bruto, &h); err != nil {
		return nil, err
	}
	jogos := h.Games.Games
	sort.SliceStable(jogos, func(a, b int) bool { return jogos[a].GameCreation > jogos[b].GameCreation })
	ids := []string{}
	for _, g := range jogos {
		if (fila == 0 || g.QueueID == fila) && (antesMs == 0 || g.GameCreation < antesMs) {
			ids = append(ids, fmt.Sprintf("%s_%d", reg.Codigo, g.GameID))
		}
	}
	if inicio >= len(ids) {
		return []string{}, nil
	}
	ids = ids[inicio:]
	if len(ids) > quantidade {
		ids = ids[:quantidade]
	}
	return ids, nil
}

// Partida: a partida inteira do LCU falso no formato da match-v5.
func (f *RiotFalso) Partida(ctx context.Context, reg riot.Regiao, id string) (riot.PartidaV5, error) {
	f.antes()
	f.mu.Lock()
	f.Pedidos++
	f.PedidosPartidas++
	recusada := f.Recusada
	f.mu.Unlock()
	var p riot.PartidaV5
	if recusada {
		return p, riot.ErrChaveRecusada
	}
	gameID, err := strconv.ParseInt(strings.TrimPrefix(id, reg.Codigo+"_"), 10, 64)
	if err != nil {
		return p, riot.ErrNaoEncontradoRiot
	}
	bruto, err := f.lcuFalso().Partida(ctx, gameID)
	if err != nil {
		return p, riot.ErrNaoEncontradoRiot
	}
	var g struct {
		GameID       int64  `json:"gameId"`
		GameCreation int64  `json:"gameCreation"`
		GameDuration int    `json:"gameDuration"`
		GameVersion  string `json:"gameVersion"`
		QueueID      int    `json:"queueId"`
		MapID        int    `json:"mapId"`
		Participants []struct {
			ParticipantID int `json:"participantId"`
			ChampionID    int `json:"championId"`
			Stats         struct {
				Win bool `json:"win"`
			} `json:"stats"`
		} `json:"participants"`
		ParticipantIdentities []struct {
			ParticipantID int `json:"participantId"`
			Player        struct {
				Puuid    string `json:"puuid"`
				GameName string `json:"gameName"`
				TagLine  string `json:"tagLine"`
			} `json:"player"`
		} `json:"participantIdentities"`
	}
	if err := json.Unmarshal(bruto, &g); err != nil {
		return p, err
	}
	p.Metadata.MatchID = id
	p.Info.GameID, p.Info.GameCreation, p.Info.GameDuration = g.GameID, g.GameCreation, g.GameDuration
	p.Info.GameVersion, p.Info.QueueID, p.Info.MapID = g.GameVersion, g.QueueID, g.MapID
	for _, pt := range g.Participants {
		v := riot.ParticipanteV5{ChampionID: pt.ChampionID, Win: pt.Stats.Win,
			TeamPosition: partida.PosicaoDoParticipante(g.MapID, pt.ParticipantID)}
		for _, pi := range g.ParticipantIdentities {
			if pi.ParticipantID == pt.ParticipantID {
				v.Puuid = "riot-" + strings.TrimPrefix(pi.Player.Puuid, "puuid-")
				v.RiotNome, v.RiotTag = pi.Player.GameName, pi.Player.TagLine
			}
		}
		p.Info.Participants = append(p.Info.Participants, v)
	}
	return p, nil
}
