package ranking

import (
	"context"
	"errors"
	"strings"

	"loltracking/server/banco"
	"loltracking/server/lcu"
	"loltracking/server/riot"
)

// O HISTÓRICO DE ANTES DE UMA PARTIDA: o contexto dos jogadores de uma partida
// (server/contexto) precisa, de cada um dos 10, das partidas que ele jogou
// ANTES dela. As mesmas duas fontes do ranking, com o mesmo cache de partidas
// (ranking_partidas_cache e ranking_participacoes): uma partida lida por um
// não é paga de novo pelo outro.
//
//   - cliente (LCU): o histórico pelo puuid do LCU (a rota já permitida, a
//     mesma da fonte "lcu"; nada novo em lcu/permitidas.go). Vê só as
//     FimHistoricoLCU partidas mais recentes (todas as filas): partida velha
//     pode ficar fora do alcance, e isso é dito (Alcanca/Parcial).
//   - riot (match-v5): os ids com endTime = o início da partida (1 pedido) e
//     1 pedido por partida não vista, até tetoPartidas.

// FilasSR: as filas de Summoner's Rift com rota que contam no contexto (escolha
// alternada 400, solo 420, às cegas 430, flexível 440, rápida 490). ARAM, Arena
// e os modos de evento não dizem nada da função de ninguém.
var FilasSR = map[int]bool{400: true, 420: true, 430: true, 440: true, 490: true}

// FonteRiotAntes é a FonteRiotAPI com a lista de ids de antes de uma data.
type FonteRiotAntes interface {
	FonteRiotAPI
	IDsPartidasAntes(ctx context.Context, reg riot.Regiao, puuid string, fila, quantidade int, antesMs int64) ([]string, error)
}

// JogoAnterior: uma partida do jogador antes da partida de referência.
type JogoAnterior struct {
	MatchID   string `json:"match_id"`
	CriadaMs  int64  `json:"criada_ms"`
	Fila      int    `json:"fila"`
	Posicao   string `json:"posicao"`
	CampeaoID int    `json:"campeao_id"`
	Venceu    bool   `json:"venceu"`
}

// HistoricoAntes: o que a fonte deu de um jogador antes de uma partida.
type HistoricoAntes struct {
	// Jogos: de Summoner's Rift (FilasSR), sem remake, criadas antes da
	// partida, da mais nova para a mais velha, no máximo o pedido.
	Jogos []JogoAnterior
	// Vistas: as partidas que a fonte devolveu (todas as filas e datas);
	// MaisVelhaMs: a criação da mais velha delas (0 = nenhuma).
	Vistas      int
	MaisVelhaMs int64
	// Alcanca: a fonte chega a antes da partida (falso = o cliente só tem
	// partidas posteriores a ela). Parcial: chega, mas pode haver mais partidas
	// de antes fora do alcance (o cliente cortou nas mais recentes, ou o teto de
	// pedidos à Riot parou antes de juntar o pedido).
	Alcanca bool
	Parcial bool
}

// HistoricoAntesLCU lê pelo cliente do LoL o histórico do puuid do LCU antes
// de antesMs. Até 2 pedidos (o histórico de outro jogador às vezes passa do
// prazo do cliente), contados em g.LCU.
func HistoricoAntesLCU(ctx context.Context, db *banco.DB, l lcu.Cliente, reg riot.Regiao, puuid string, antesMs int64, quantos int,
	g *Gasto) (HistoricoAntes, error) {
	var bruto []byte
	var err error
	for tentativa := 0; tentativa < 2; tentativa++ {
		g.LCU++
		bruto, err = l.Historico(ctx, puuid, 0, FimHistoricoLCU)
		if err == nil || errors.Is(err, lcu.ErrNaoEncontrado) || paraTudo(ctx, err) {
			break
		}
	}
	if errors.Is(err, lcu.ErrNaoEncontrado) {
		return HistoricoAntes{}, ErrNaoAchado
	}
	if err != nil {
		if paraTudo(ctx, err) {
			return HistoricoAntes{}, err
		}
		return HistoricoAntes{}, ErrSemHistorico
	}
	jogos, err := leHistoricoLCU(bruto, puuid)
	if err != nil {
		return HistoricoAntes{}, ErrSemHistorico
	}
	h := HistoricoAntes{Vistas: len(jogos)}
	antes := 0
	for _, j := range jogos {
		id := matchIDDe(reg, j.gameID)
		gravaPartida(db, reg, id, j.versao, j.fila, j.criadaMs, j.duracaoS, false, []participacao{{puuid: puuid, posicao: j.posicao,
			campeaoID: j.campeaoID, venceu: j.venceu, rendicao: j.rendicao}})
		if h.MaisVelhaMs == 0 || j.criadaMs < h.MaisVelhaMs {
			h.MaisVelhaMs = j.criadaMs
		}
		if j.criadaMs >= antesMs {
			continue
		}
		antes++
		if len(h.Jogos) >= quantos || !FilasSR[j.fila] || j.duracaoS < DuracaoMinimaS || j.rendicao {
			continue
		}
		h.Jogos = append(h.Jogos, JogoAnterior{MatchID: id, CriadaMs: j.criadaMs, Fila: j.fila, Posicao: j.posicao, CampeaoID: j.campeaoID,
			Venceu: j.venceu})
	}
	// Lista mais curta que o teto do cliente = é tudo o que ele tem do jogador.
	inteira := len(jogos) < FimHistoricoLCU
	h.Alcanca = antes > 0 || inteira
	h.Parcial = h.Alcanca && !inteira && len(h.Jogos) < quantos
	return h, nil
}

// ParticipanteRiot: um participante da partida pela match-v5 (o puuid da Riot,
// que não é o do LCU).
type ParticipanteRiot struct {
	Puuid     string
	Nome, Tag string
	CampeaoID int
	Posicao   string
}

// ParticipantesDaPartidaRiot: os 10 da partida pela match-v5 (o cache, se ela
// já foi lida inteira; senão 1 pedido, que fica no cache).
func ParticipantesDaPartidaRiot(ctx context.Context, db *banco.DB, api FonteRiotAPI, reg riot.Regiao, gameID int64, g *Gasto) ([]ParticipanteRiot, error) {
	id := matchIDDe(reg, gameID)
	var completa int
	if err := db.QueryRow(`SELECT completa FROM ranking_partidas_cache WHERE regiao = ? AND match_id = ?`, reg.Codigo, id).Scan(&completa); err == nil && completa == 1 {
		linhas, err := db.Query(`SELECT puuid, nome, tag, campeao_id, posicao FROM ranking_participacoes WHERE regiao = ? AND match_id = ?`, reg.Codigo, id)
		if err != nil {
			return nil, err
		}
		defer linhas.Close()
		var l []ParticipanteRiot
		for linhas.Next() {
			var p ParticipanteRiot
			if err := linhas.Scan(&p.Puuid, &p.Nome, &p.Tag, &p.CampeaoID, &p.Posicao); err != nil {
				return nil, err
			}
			// O cache também guarda participações do LCU (só o dono, puuid do
			// LCU): numa partida completa as da Riot são as que têm Riot ID.
			if p.Nome != "" {
				l = append(l, p)
			}
		}
		if len(l) == 10 {
			g.DoCache++
			return l, linhas.Err()
		}
	}
	g.Partidas++
	v5, err := api.Partida(ctx, reg, id)
	if errors.Is(err, riot.ErrNaoEncontradoRiot) {
		return nil, ErrNaoAchado
	}
	if err != nil {
		return nil, err
	}
	var ps []participacao
	var l []ParticipanteRiot
	for _, pt := range v5.Info.Participants {
		ps = append(ps, participacao{puuid: pt.Puuid, nome: pt.RiotNome, tag: pt.RiotTag, posicao: pt.TeamPosition,
			campeaoID: pt.ChampionID, venceu: pt.Win, rendicao: pt.GameEndedInEarlySurrender || pt.TeamEarlySurrendered})
		l = append(l, ParticipanteRiot{Puuid: pt.Puuid, Nome: pt.RiotNome, Tag: pt.RiotTag, CampeaoID: pt.ChampionID, Posicao: pt.TeamPosition})
	}
	gravaPartida(db, reg, id, v5.Info.GameVersion, v5.Info.QueueID, v5.Info.GameCreation, v5.Info.GameDuration, len(ps) == 10, ps)
	return l, nil
}

// AchaParticipanteRiot: o participante da match-v5 de um jogador da partida,
// pelo Riot ID (sem diferenciar maiúsculas) ou, sem ele, pelo campeão.
func AchaParticipanteRiot(l []ParticipanteRiot, nome, tag string, campeaoID int) (ParticipanteRiot, bool) {
	if nome != "" {
		for _, p := range l {
			if strings.EqualFold(p.Nome, nome) && strings.EqualFold(p.Tag, tag) {
				return p, true
			}
		}
	}
	achou, n := ParticipanteRiot{}, 0
	for _, p := range l {
		if campeaoID != 0 && p.CampeaoID == campeaoID {
			achou, n = p, n+1
		}
	}
	return achou, n == 1
}

// HistoricoAntesRiot lê pela match-v5 o histórico do puuid da Riot antes de
// antesMs: 1 pedido de ids (endTime) e até tetoPartidas pedidos de partida (as
// do cache não contam), parando ao juntar quantos.
func HistoricoAntesRiot(ctx context.Context, db *banco.DB, api FonteRiotAntes, reg riot.Regiao, puuid string, antesMs int64, quantos,
	margem, tetoPartidas int, g *Gasto) (HistoricoAntes, error) {
	g.IDs++
	ids, err := api.IDsPartidasAntes(ctx, reg, puuid, 0, quantos+margem, antesMs)
	if errors.Is(err, riot.ErrNaoEncontradoRiot) {
		return HistoricoAntes{}, ErrNaoAchado
	}
	if err != nil {
		return HistoricoAntes{}, err
	}
	// endTime: a match-v5 só devolve as de antes; a lista toda é de antes.
	h := HistoricoAntes{Vistas: len(ids), Alcanca: true}
	lidas := 0
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return HistoricoAntes{}, err
		}
		p, ok := partidaDoCache(db, reg, id, puuid)
		if ok && (p.completa || p.achou) {
			g.DoCache++
		} else {
			if lidas >= tetoPartidas {
				h.Parcial = true
				break
			}
			lidas++
			g.Partidas++
			v5, err := api.Partida(ctx, reg, id)
			if errors.Is(err, riot.ErrNaoEncontradoRiot) {
				continue
			}
			if err != nil {
				return HistoricoAntes{}, err
			}
			var ps []participacao
			for _, pt := range v5.Info.Participants {
				ps = append(ps, participacao{puuid: pt.Puuid, nome: pt.RiotNome, tag: pt.RiotTag, posicao: pt.TeamPosition,
					campeaoID: pt.ChampionID, venceu: pt.Win, rendicao: pt.GameEndedInEarlySurrender || pt.TeamEarlySurrendered})
			}
			gravaPartida(db, reg, id, v5.Info.GameVersion, v5.Info.QueueID, v5.Info.GameCreation, v5.Info.GameDuration, len(ps) == 10, ps)
			p, _ = partidaDoCache(db, reg, id, puuid)
		}
		if h.MaisVelhaMs == 0 || p.criadaMs < h.MaisVelhaMs {
			h.MaisVelhaMs = p.criadaMs
		}
		if p.criadaMs >= antesMs || !p.achou || !FilasSR[p.fila] || p.duracaoS < DuracaoMinimaS || p.rendicao {
			continue
		}
		h.Jogos = append(h.Jogos, JogoAnterior{MatchID: id, CriadaMs: p.criadaMs, Fila: p.fila, Posicao: p.posicao, CampeaoID: p.campeaoID,
			Venceu: p.venceu})
		if len(h.Jogos) >= quantos {
			break
		}
	}
	return h, nil
}
