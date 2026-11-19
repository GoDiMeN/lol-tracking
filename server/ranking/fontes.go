package ranking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"loltracking/server/banco"
	"loltracking/server/lcu"
	"loltracking/server/partida"
	"loltracking/server/riot"
)

// AS DUAS FONTES DE HISTÓRICO. O ranking precisa, de cada candidato, das
// ranqueadas solo do patch com a rota, o campeão e o resultado. Duas fontes
// dão isso, escolhidas por configuração (LOL_RANKING_FONTE) ou no pedido:
//
//   - "lcu": o cliente do LoL. Riot ID pela account-v1 (1 pedido à Riot por
//     candidato, com cache), o jogador pelo Riot ID e o histórico pelo LCU
//     (rotas já permitidas; nada novo em lcu/permitidas.go). Não gasta a cota
//     da match-v5, mas só vê as FimHistoricoLCU partidas mais recentes (todas as
//     filas) e precisa do cliente aberto. A rota é a da ordem do participante
//     (partida.PosicaoDoParticipante), como no resto do app.
//   - "riot": só a API da Riot (para o servidor sem cliente). match-v5: 1
//     pedido de ids (fila 420, da mais nova para a mais velha) e 1 pedido por
//     partida não vista; a partida lida guarda os 10 participantes, então o
//     próximo candidato que esteve nela não paga de novo. A rota é a
//     teamPosition da Riot. Para na primeira partida de patch mais velho que o
//     pedido ou no teto de partidas.

// Fontes.
const (
	FonteLCU  = "lcu"
	FonteRiot = "riot"
)

// FonteRiotAPI é o que o ranking pede à API da Riot. De verdade:
// *riot.Cliente; nos testes e no modo teste: aprendizado.RiotFalso.
type FonteRiotAPI interface {
	Confere(ctx context.Context) error
	Liga(ctx context.Context, reg riot.Regiao, elo, divisao string, pagina int) ([]riot.EntradaLiga, error)
	ContaPorPuuid(ctx context.Context, reg riot.Regiao, puuid string) (riot.Conta, error)
	IDsPartidas(ctx context.Context, reg riot.Regiao, puuid string, fila, inicio, quantidade int) ([]string, error)
	Partida(ctx context.Context, reg riot.Regiao, id string) (riot.PartidaV5, error)
}

// Gasto: os pedidos à Riot de um passo, por tipo, e as leituras do cache.
type Gasto struct {
	Liga     int `json:"liga"`
	Conta    int `json:"conta"`
	IDs      int `json:"ids"`
	Partidas int `json:"partidas"`
	DoCache  int `json:"do_cache"`
	// LCU: pedidos ao cliente do LoL (só o histórico de antes de uma partida, antes.go, conta).
	LCU int `json:"lcu,omitempty"`
}

// Historico: o que a fonte achou de um candidato.
type Historico struct {
	Nome string
	Tag  string
	// Partidas: ranqueadas solo do patch (sem remake), da mais nova para a mais velha.
	Partidas []PartidaDoJogador
	// PatchMaisNovo: o patch da partida solo mais nova vista (resolve o patch
	// quando o pedido não diz qual).
	PatchMaisNovo string
	// Truncado: parou no teto de partidas (havia mais no patch).
	Truncado bool
}

// ErrNaoAchado: o candidato não foi achado (conta sumiu, Riot ID não está no
// cliente). Fica de fora com "nao_achado"; a atualização segue.
var ErrNaoAchado = errors.New("jogador não achado")

// ErrSemHistorico: o histórico não veio (prazo, formato). Fica de fora com
// "sem_historico"; a atualização segue.
var ErrSemHistorico = errors.New("histórico não veio")

// FonteHistorico lê o histórico de um candidato no patch.
type FonteHistorico interface {
	Nome() string
	// Le: patch "" = o da partida mais nova do jogador. teto: no máximo tantas
	// partidas do patch. g acumula os pedidos.
	Le(ctx context.Context, reg riot.Regiao, puuid, patch string, teto int, g *Gasto) (Historico, error)
}

// --- Cache ---------------------------------------------------------------------

// matchID do LCU no formato da match-v5.
func matchIDDe(reg riot.Regiao, gameID int64) string {
	return fmt.Sprintf("%s_%d", reg.Codigo, gameID)
}

func contaDoCache(db *banco.DB, reg riot.Regiao, puuid string) (riot.Conta, bool) {
	var c riot.Conta
	var quando string
	if err := db.QueryRow(`SELECT nome, tag, consultado_em FROM ranking_contas WHERE regiao = ? AND puuid = ?`, reg.Codigo, puuid).
		Scan(&c.Nome, &c.Tag, &quando); err != nil {
		return c, false
	}
	t, err := time.Parse(banco.FormatoData, quando)
	if err != nil || time.Since(t) > 24*time.Hour {
		return c, false
	}
	c.Puuid = puuid
	return c, true
}

func gravaConta(db *banco.DB, reg riot.Regiao, c riot.Conta) {
	db.Exec(`INSERT INTO ranking_contas (regiao, puuid, nome, tag, consultado_em) VALUES (?,?,?,?,?)
		ON CONFLICT (regiao, puuid) DO UPDATE SET nome = excluded.nome, tag = excluded.tag, consultado_em = excluded.consultado_em`,
		reg.Codigo, c.Puuid, c.Nome, c.Tag, banco.Agora())
}

// contaDe: Riot ID pelo cache ou pela account-v1 (1 pedido).
func contaDe(ctx context.Context, db *banco.DB, api FonteRiotAPI, reg riot.Regiao, puuid string, g *Gasto) (riot.Conta, error) {
	if c, ok := contaDoCache(db, reg, puuid); ok {
		g.DoCache++
		return c, nil
	}
	g.Conta++
	c, err := api.ContaPorPuuid(ctx, reg, puuid)
	if errors.Is(err, riot.ErrNaoEncontradoRiot) {
		return c, ErrNaoAchado
	}
	if err != nil {
		return c, err
	}
	c.Puuid = puuid
	gravaConta(db, reg, c)
	return c, nil
}

func idsDoCache(db *banco.DB, reg riot.Regiao, puuid, fonte string) ([]string, bool) {
	var ids, quando string
	if err := db.QueryRow(`SELECT ids, buscado_em FROM ranking_historicos WHERE regiao = ? AND puuid = ? AND fonte = ?`,
		reg.Codigo, puuid, fonte).Scan(&ids, &quando); err != nil {
		return nil, false
	}
	t, err := time.Parse(banco.FormatoData, quando)
	if err != nil || time.Since(t) > ValidadeHistoricoH*time.Hour {
		return nil, false
	}
	var l []string
	if json.Unmarshal([]byte(ids), &l) != nil {
		return nil, false
	}
	return l, true
}

func gravaIDs(db *banco.DB, reg riot.Regiao, puuid, fonte string, ids []string) {
	b, _ := json.Marshal(ids)
	db.Exec(`INSERT INTO ranking_historicos (regiao, puuid, fonte, ids, buscado_em) VALUES (?,?,?,?,?)
		ON CONFLICT (regiao, puuid, fonte) DO UPDATE SET ids = excluded.ids, buscado_em = excluded.buscado_em`,
		reg.Codigo, puuid, fonte, string(b), banco.Agora())
}

// partidaCache: o cabeçalho de uma partida lida e a participação de um jogador.
type partidaCache struct {
	versao    string
	patch     string
	fila      int
	criadaMs  int64
	duracaoS  int
	completa  bool
	achou     bool // o jogador estava nela
	posicao   string
	campeaoID int
	venceu    bool
	rendicao  bool
	nome, tag string
}

func partidaDoCache(db *banco.DB, reg riot.Regiao, matchID, puuid string) (partidaCache, bool) {
	var p partidaCache
	var completa int
	if err := db.QueryRow(`SELECT versao, patch, fila, criada_ms, duracao_s, completa FROM ranking_partidas_cache
		WHERE regiao = ? AND match_id = ?`, reg.Codigo, matchID).Scan(&p.versao, &p.patch, &p.fila, &p.criadaMs, &p.duracaoS, &completa); err != nil {
		return p, false
	}
	p.completa = completa == 1
	var venceu, rend int
	err := db.QueryRow(`SELECT nome, tag, posicao, campeao_id, venceu, rendicao FROM ranking_participacoes
		WHERE regiao = ? AND match_id = ? AND puuid = ?`, reg.Codigo, matchID, puuid).Scan(&p.nome, &p.tag, &p.posicao, &p.campeaoID, &venceu, &rend)
	p.achou = err == nil
	p.venceu, p.rendicao = venceu == 1, rend == 1
	return p, true
}

type participacao struct {
	puuid, nome, tag, posicao string
	campeaoID                 int
	venceu, rendicao          bool
}

func gravaPartida(db *banco.DB, reg riot.Regiao, matchID, versao string, fila int, criadaMs int64, duracaoS int, completa bool, ps []participacao) {
	tx, err := db.Begin()
	if err != nil {
		log.Printf("ranking: guardar a partida no cache: %v", err)
		return
	}
	defer tx.Rollback()
	// Uma partida completa (os 10) nunca volta a incompleta.
	tx.Exec(`INSERT INTO ranking_partidas_cache (regiao, match_id, versao, patch, fila, criada_ms, duracao_s, completa, lida_em)
		VALUES (?,?,?,?,?,?,?,?,?) ON CONFLICT (regiao, match_id) DO UPDATE SET completa = MAX(completa, excluded.completa)`,
		reg.Codigo, matchID, versao, lcu.PatchDe(versao), fila, criadaMs, duracaoS, b2i(completa), banco.Agora())
	for _, p := range ps {
		tx.Exec(`INSERT INTO ranking_participacoes (regiao, match_id, puuid, nome, tag, posicao, campeao_id, venceu, rendicao)
			VALUES (?,?,?,?,?,?,?,?,?) ON CONFLICT (regiao, match_id, puuid) DO NOTHING`,
			reg.Codigo, matchID, p.puuid, p.nome, p.tag, p.posicao, p.campeaoID, b2i(p.venceu), b2i(p.rendicao))
	}
	if err := tx.Commit(); err != nil {
		log.Printf("ranking: guardar a partida no cache: %v", err)
	}
}

// coleta junta as partidas do patch na ordem dada (mais nova primeiro):
// partida de patch mais novo que o pedido é pulada; a primeira de patch mais
// velho encerra. Devolve parar=true quando não precisa olhar as próximas.
type coletor struct {
	patch    string
	teto     int
	h        Historico
	primeira bool
}

func (c *coletor) poe(matchID string, p partidaCache) (parar bool) {
	if p.fila != FilaSolo {
		return false
	}
	if !c.primeira {
		c.primeira = true
		c.h.PatchMaisNovo = p.patch
		if c.patch == "" {
			c.patch = p.patch
		}
	}
	switch cmp := ComparaPatch(p.patch, c.patch); {
	case cmp > 0:
		return false
	case cmp < 0:
		return true
	}
	if !p.achou || p.duracaoS < DuracaoMinimaS || p.rendicao {
		return false
	}
	if len(c.h.Partidas) >= c.teto {
		c.h.Truncado = true
		return true
	}
	if c.h.Nome == "" {
		c.h.Nome, c.h.Tag = p.nome, p.tag
	}
	c.h.Partidas = append(c.h.Partidas, PartidaDoJogador{MatchID: matchID, CriadaMs: p.criadaMs, Posicao: p.posicao,
		CampeaoID: p.campeaoID, Venceu: p.venceu})
	return false
}

// --- Fonte Riot (match-v5) -------------------------------------------------------

type fonteRiot struct {
	db  *banco.DB
	api FonteRiotAPI
}

// NovaFonteRiot: o histórico só pela API da Riot (match-v5).
func NovaFonteRiot(db *banco.DB, api FonteRiotAPI) FonteHistorico {
	return &fonteRiot{db: db, api: api}
}

func (f *fonteRiot) Nome() string { return FonteRiot }

func (f *fonteRiot) Le(ctx context.Context, reg riot.Regiao, puuid, patch string, teto int, g *Gasto) (Historico, error) {
	ids, ok := idsDoCache(f.db, reg, puuid, FonteRiot)
	if ok {
		g.DoCache++
	} else {
		var err error
		g.IDs++
		ids, err = f.api.IDsPartidas(ctx, reg, puuid, FilaSolo, 0, teto+MargemIDs)
		if errors.Is(err, riot.ErrNaoEncontradoRiot) {
			return Historico{}, ErrNaoAchado
		}
		if err != nil {
			return Historico{}, err
		}
		gravaIDs(f.db, reg, puuid, FonteRiot, ids)
	}
	c := &coletor{patch: patch, teto: teto}
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return Historico{}, err
		}
		p, ok := partidaDoCache(f.db, reg, id, puuid)
		if ok && (p.completa || p.achou) {
			g.DoCache++
		} else {
			g.Partidas++
			v5, err := f.api.Partida(ctx, reg, id)
			if errors.Is(err, riot.ErrNaoEncontradoRiot) {
				continue
			}
			if err != nil {
				return Historico{}, err
			}
			var ps []participacao
			for _, pt := range v5.Info.Participants {
				ps = append(ps, participacao{puuid: pt.Puuid, nome: pt.RiotNome, tag: pt.RiotTag, posicao: pt.TeamPosition,
					campeaoID: pt.ChampionID, venceu: pt.Win, rendicao: pt.GameEndedInEarlySurrender || pt.TeamEarlySurrendered})
			}
			gravaPartida(f.db, reg, id, v5.Info.GameVersion, v5.Info.QueueID, v5.Info.GameCreation, v5.Info.GameDuration, len(ps) == 10, ps)
			p, _ = partidaDoCache(f.db, reg, id, puuid)
		}
		if c.poe(id, p) {
			break
		}
	}
	return c.h, nil
}

// --- Fonte LCU -------------------------------------------------------------------

type fonteLCU struct {
	db  *banco.DB
	api FonteRiotAPI
	lcu lcu.Cliente
}

// NovaFonteLCU: Riot ID pela Riot (account-v1, com cache) e o histórico pelo cliente do LoL.
func NovaFonteLCU(db *banco.DB, api FonteRiotAPI, l lcu.Cliente) FonteHistorico {
	return &fonteLCU{db: db, api: api, lcu: l}
}

func (f *fonteLCU) Nome() string { return FonteLCU }

func (f *fonteLCU) Le(ctx context.Context, reg riot.Regiao, puuid, patch string, teto int, g *Gasto) (Historico, error) {
	conta, err := contaDe(ctx, f.db, f.api, reg, puuid, g)
	if err != nil {
		return Historico{}, err
	}
	// O puuid da Riot não é o do LCU: o jogador é achado no cliente pelo Riot ID.
	inv, err := f.lcu.BuscaAlias(ctx, conta.Nome, conta.Tag)
	if errors.Is(err, lcu.ErrNaoEncontrado) {
		return Historico{}, ErrNaoAchado
	}
	if err != nil {
		if paraTudo(ctx, err) {
			return Historico{}, err
		}
		log.Printf("ranking: buscar um candidato no cliente: %v", err)
		return Historico{}, ErrNaoAchado
	}
	// O histórico de outro jogador às vezes passa do prazo do cliente: tenta 2 vezes.
	var bruto []byte
	for tentativa := 0; tentativa < 2; tentativa++ {
		bruto, err = f.lcu.Historico(ctx, inv.Puuid, 0, FimHistoricoLCU)
		if err == nil || errors.Is(err, lcu.ErrNaoEncontrado) || paraTudo(ctx, err) {
			break
		}
	}
	if err != nil {
		if paraTudo(ctx, err) {
			return Historico{}, err
		}
		return Historico{}, ErrSemHistorico
	}
	jogos, err := leHistoricoLCU(bruto, inv.Puuid)
	if err != nil {
		log.Printf("ranking: histórico do LCU em formato inesperado: %v", err)
		return Historico{}, ErrSemHistorico
	}
	c := &coletor{patch: patch, teto: teto}
	for _, j := range jogos {
		id := matchIDDe(reg, j.gameID)
		gravaPartida(f.db, reg, id, j.versao, j.fila, j.criadaMs, j.duracaoS, false, []participacao{{puuid: puuid, nome: conta.Nome,
			tag: conta.Tag, posicao: j.posicao, campeaoID: j.campeaoID, venceu: j.venceu, rendicao: j.rendicao}})
		p := partidaCache{versao: j.versao, patch: lcu.PatchDe(j.versao), fila: j.fila, criadaMs: j.criadaMs, duracaoS: j.duracaoS,
			achou: true, posicao: j.posicao, campeaoID: j.campeaoID, venceu: j.venceu, rendicao: j.rendicao, nome: conta.Nome, tag: conta.Tag}
		if c.poe(id, p) {
			break
		}
	}
	c.h.Nome, c.h.Tag = conta.Nome, conta.Tag
	return c.h, nil
}

// paraTudo: erro que para a atualização (cliente fechado, rota recusada, contexto).
func paraTudo(ctx context.Context, err error) bool {
	var proibida *lcu.ErroRotaProibida
	return ctx.Err() != nil || errors.Is(err, lcu.ErrClienteFechado) || errors.As(err, &proibida)
}

type jogoLCU struct {
	gameID    int64
	criadaMs  int64
	fila      int
	versao    string
	duracaoS  int
	posicao   string
	campeaoID int
	venceu    bool
	rendicao  bool
}

// leHistoricoLCU: o histórico do LCU (só o participante dono vem em cada jogo),
// na ordem do cliente (mais nova primeiro).
func leHistoricoLCU(bruto []byte, puuid string) ([]jogoLCU, error) {
	var h struct {
		Games struct {
			Games []struct {
				GameID       int64  `json:"gameId"`
				GameCreation int64  `json:"gameCreation"`
				MapID        int    `json:"mapId"`
				QueueID      int    `json:"queueId"`
				GameVersion  string `json:"gameVersion"`
				GameDuration int    `json:"gameDuration"`
				Participants []struct {
					ParticipantID int `json:"participantId"`
					ChampionID    int `json:"championId"`
					Stats         struct {
						Win                       bool `json:"win"`
						GameEndedInEarlySurrender bool `json:"gameEndedInEarlySurrender"`
						TeamEarlySurrendered      bool `json:"teamEarlySurrendered"`
					} `json:"stats"`
				} `json:"participants"`
				ParticipantIdentities []struct {
					ParticipantID int `json:"participantId"`
					Player        struct {
						Puuid string `json:"puuid"`
					} `json:"player"`
				} `json:"participantIdentities"`
			} `json:"games"`
		} `json:"games"`
	}
	if err := json.Unmarshal(bruto, &h); err != nil {
		return nil, err
	}
	var l []jogoLCU
	for _, g := range h.Games.Games {
		if len(g.Participants) == 0 {
			continue
		}
		idx := 0
		for _, pi := range g.ParticipantIdentities {
			if strings.EqualFold(pi.Player.Puuid, puuid) {
				for i, p := range g.Participants {
					if p.ParticipantID == pi.ParticipantID {
						idx = i
					}
				}
			}
		}
		p := g.Participants[idx]
		l = append(l, jogoLCU{gameID: g.GameID, criadaMs: g.GameCreation, fila: g.QueueID, versao: g.GameVersion, duracaoS: g.GameDuration,
			posicao: partida.PosicaoDoParticipante(g.MapID, p.ParticipantID), campeaoID: p.ChampionID, venceu: p.Stats.Win,
			rendicao: p.Stats.GameEndedInEarlySurrender || p.Stats.TeamEarlySurrendered})
	}
	// A regra "para na primeira de patch mais velho" precisa da mais nova primeiro.
	sort.SliceStable(l, func(a, b int) bool { return l[a].criadaMs > l[b].criadaMs })
	return l, nil
}
