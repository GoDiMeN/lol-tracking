package analise

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"loltracking/server/banco"
	"loltracking/server/partida"
	"loltracking/server/ranking"
)

// O CONTEXTO DOS 10 JOGADORES: quem cada um era ANTES desta partida (função
// principal, campeão, sequência, vitórias recentes, elo). Duas origens:
//
//   - o banco (partida.HistoricoGuardado): as partidas do jogador que o app
//     já guardou, quase sempre só as do dono da conta;
//   - o histórico buscado fora (server/contexto, a pedido): o cliente do LoL
//     ou a match-v5, guardado aqui (contexto_historicos) com a data e a fonte.
//
// Só partidas criadas ANTES desta contam; se a fonte não alcança a data da
// partida, o contexto diz isso (Alcance) em vez de usar partidas posteriores.
// O contexto é informação ao lado: a nota de impacto não o usa.

// Estados do histórico buscado de um jogador.
const (
	HistOK         = "ok"
	HistParcial    = "parcial"     // alcança a partida, mas pode faltar partida de antes
	HistNaoAlcanca = "nao_alcanca" // a fonte só tem partidas posteriores à partida
	HistNaoAchado  = "nao_achado"  // a fonte não achou o jogador
	HistErro       = "erro"
)

// JogoExterno: uma partida de antes, do histórico buscado.
type JogoExterno struct {
	GameID    int64  `json:"game_id"`
	CriadaMs  int64  `json:"criada_ms"`
	Fila      int    `json:"fila"`
	Posicao   string `json:"posicao"`
	CampeaoID int    `json:"campeao_id"`
	Venceu    bool   `json:"venceu"`
}

// HistoricoExterno: o histórico de antes de um jogador, buscado fora do banco.
type HistoricoExterno struct {
	ParticipanteID int    `json:"participante_id"`
	Puuid          string `json:"puuid"`
	Fonte          string `json:"fonte"` // cliente | riot
	BuscadoEm      string `json:"buscado_em"`
	Estado         string `json:"estado"`
	Aviso          string `json:"aviso"`
	// Vistas e MaisVelhaMs: o que a fonte devolveu (todas as filas e datas).
	Vistas      int           `json:"vistas"`
	MaisVelhaMs int64         `json:"mais_velha_ms"`
	Jogos       []JogoExterno `json:"jogos"`
}

func migraHistoricos(db *banco.DB) error {
	return banco.Executa(db, `CREATE TABLE IF NOT EXISTS contexto_historicos (
		game_id         INTEGER NOT NULL,
		participante_id INTEGER NOT NULL,
		puuid           TEXT NOT NULL DEFAULT '',
		fonte           TEXT NOT NULL,
		buscado_em      TEXT NOT NULL,
		estado          TEXT NOT NULL,
		aviso           TEXT NOT NULL DEFAULT '',
		vistas          INTEGER NOT NULL DEFAULT 0,
		mais_velha_ms   INTEGER NOT NULL DEFAULT 0,
		jogos           TEXT NOT NULL DEFAULT '[]',
		PRIMARY KEY (game_id, participante_id)
	)`)
}

// GravaHistoricoExterno guarda (troca) o histórico buscado de um jogador da partida.
func GravaHistoricoExterno(db *banco.DB, gameID int64, h HistoricoExterno) error {
	if h.Jogos == nil {
		h.Jogos = []JogoExterno{}
	}
	if h.BuscadoEm == "" {
		h.BuscadoEm = banco.Agora()
	}
	_, err := db.Exec(`INSERT INTO contexto_historicos (game_id, participante_id, puuid, fonte, buscado_em, estado, aviso, vistas, mais_velha_ms, jogos)
		VALUES (?,?,?,?,?,?,?,?,?,?) ON CONFLICT (game_id, participante_id) DO UPDATE SET puuid = excluded.puuid, fonte = excluded.fonte,
		buscado_em = excluded.buscado_em, estado = excluded.estado, aviso = excluded.aviso, vistas = excluded.vistas,
		mais_velha_ms = excluded.mais_velha_ms, jogos = excluded.jogos`,
		gameID, h.ParticipanteID, h.Puuid, h.Fonte, h.BuscadoEm, h.Estado, h.Aviso, h.Vistas, h.MaisVelhaMs, jsonTexto(h.Jogos))
	return err
}

// LeHistoricosExternos: os históricos buscados dos jogadores da partida, por participante.
func LeHistoricosExternos(db *banco.DB, gameID int64) (map[int]HistoricoExterno, error) {
	linhas, err := db.Query(`SELECT participante_id, puuid, fonte, buscado_em, estado, aviso, vistas, mais_velha_ms, jogos
		FROM contexto_historicos WHERE game_id = ? ORDER BY participante_id`, gameID)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	r := map[int]HistoricoExterno{}
	for linhas.Next() {
		var h HistoricoExterno
		var jogos string
		if err := linhas.Scan(&h.ParticipanteID, &h.Puuid, &h.Fonte, &h.BuscadoEm, &h.Estado, &h.Aviso, &h.Vistas, &h.MaisVelhaMs, &jogos); err != nil {
			return nil, err
		}
		h.Jogos = []JogoExterno{}
		json.Unmarshal([]byte(jogos), &h.Jogos)
		r[h.ParticipanteID] = h
	}
	return r, linhas.Err()
}

func leHistoricoExterno(db *banco.DB, gameID int64, pid int) (HistoricoExterno, bool) {
	var h HistoricoExterno
	var jogos string
	err := db.QueryRow(`SELECT participante_id, puuid, fonte, buscado_em, estado, aviso, vistas, mais_velha_ms, jogos
		FROM contexto_historicos WHERE game_id = ? AND participante_id = ?`, gameID, pid).
		Scan(&h.ParticipanteID, &h.Puuid, &h.Fonte, &h.BuscadoEm, &h.Estado, &h.Aviso, &h.Vistas, &h.MaisVelhaMs, &jogos)
	if errors.Is(err, sql.ErrNoRows) || err != nil {
		return h, false
	}
	h.Jogos = []JogoExterno{}
	json.Unmarshal([]byte(jogos), &h.Jogos)
	return h, true
}

// GameIDDoMatch: "BR1_3304217746" → 3304217746 (0 se não der).
func GameIDDoMatch(matchID string) int64 {
	if i := strings.LastIndex(matchID, "_"); i >= 0 {
		matchID = matchID[i+1:]
	}
	g, _ := strconv.ParseInt(matchID, 10, 64)
	return g
}

// jogoCtx: uma partida de antes, do banco ou de fora, no mesmo formato.
type jogoCtx struct {
	gameID    int64
	criadaMs  int64
	fila      int
	posicao   string
	campeaoID int
	campeao   string
	venceu    bool
	externo   bool
}

// --- Contexto do jogador ----------------------------------------------------------------------

// RotaHistoricoLCU é a rota PERMITIDA (lcu/permitidas.go) que dá o histórico
// recente de qualquer jogador; a base de fatos NÃO a chama (só lê o banco): quem
// chama é a busca do contexto dos jogadores (server/contexto), a pedido.
const RotaHistoricoLCU = "GET /lol-match-history/v1/products/lol/{puuid}/matches"

// contextoDoJogador: o que se sabe do jogador de ANTES desta partida, juntando
// o banco e o histórico buscado (se houver), só nas filas de Summoner's Rift.
func contextoDoJogador(db *banco.DB, gameID int64, part partida.Participante, criadaEmMs int64) ContextoJogador {
	c := ContextoJogador{FuncoesGuardadas: map[string]int{}, Falta: []string{}, Fonte: "banco"}
	hist, err := partida.HistoricoGuardado(db, part.Puuid, criadaEmMs)
	if err != nil {
		c.Falta = append(c.Falta, "histórico guardado: "+err.Error())
	}
	var todos []jogoCtx
	visto := map[int64]bool{}
	for _, h := range hist {
		if h.GameID == gameID || !ranking.FilasSR[h.Fila] {
			continue
		}
		visto[h.GameID] = true
		todos = append(todos, jogoCtx{gameID: h.GameID, criadaMs: h.CriadaEmMs, fila: h.Fila, posicao: h.Posicao, campeao: h.Campeao, venceu: h.Venceu})
	}
	ext, temExt := leHistoricoExterno(db, gameID, part.ParticipanteID)
	if temExt {
		c.BuscadoEm = ext.BuscadoEm
		c.Fonte = ext.Fonte
		extras := 0
		for _, j := range ext.Jogos {
			// Só de antes (a busca já filtra; conferido de novo aqui) e sem repetir o banco.
			if j.CriadaMs >= criadaEmMs || j.GameID == gameID || (j.GameID != 0 && visto[j.GameID]) || !ranking.FilasSR[j.Fila] {
				continue
			}
			visto[j.GameID] = true
			todos = append(todos, jogoCtx{gameID: j.GameID, criadaMs: j.CriadaMs, fila: j.Fila, posicao: j.Posicao, campeaoID: j.CampeaoID,
				venceu: j.Venceu, externo: true})
			extras++
		}
		if len(todos) > extras {
			c.Fonte += " + banco"
		}
		switch ext.Estado {
		case HistNaoAlcanca:
			c.Alcance = fmt.Sprintf("o histórico do %s (%d partida(s), a mais velha de %s) não chega antes desta partida", nomeFonteHist(ext.Fonte),
				ext.Vistas, dataDeMs(ext.MaisVelhaMs))
		case HistParcial:
			c.Alcance = fmt.Sprintf("só %d partida(s) de antes ao alcance do %s (pode haver mais antes delas)", extras, nomeFonteHist(ext.Fonte))
		case HistNaoAchado, HistErro:
			c.Falta = append(c.Falta, fmt.Sprintf("histórico pelo %s: %s", nomeFonteHist(ext.Fonte), ext.Aviso))
		}
	}
	sort.SliceStable(todos, func(a, b int) bool { return todos[a].criadaMs > todos[b].criadaMs })
	if len(todos) > Param.PartidasContexto {
		todos = todos[:Param.PartidasContexto]
	}
	c.PartidasGuardadas = len(todos)
	comFuncao := 0
	for _, h := range todos {
		if h.posicao != "" {
			c.FuncoesGuardadas[h.posicao]++
			comFuncao++
		}
		c.Ultimas++
		if h.venceu {
			c.VitoriasUltimas++
		}
		if (h.campeaoID != 0 && h.campeaoID == part.CampeaoID) || (h.campeao != "" && h.campeao == part.Campeao) {
			c.CampeaoVezes++
		}
	}
	c.CampeaoDe = len(todos)
	for i, h := range todos {
		if i == 0 {
			c.Sequencia = map[bool]int{true: 1, false: -1}[h.venceu]
			continue
		}
		if h.venceu != todos[0].venceu {
			break
		}
		c.Sequencia += map[bool]int{true: 1, false: -1}[h.venceu]
	}
	if len(todos) >= Param.MinPartidasContexto {
		melhor, n := "", 0
		for _, k := range []string{"TOP", "JUNGLE", "MIDDLE", "BOTTOM", "UTILITY"} {
			if c.FuncoesGuardadas[k] > n {
				melhor, n = k, c.FuncoesGuardadas[k]
			}
		}
		c.FuncaoPrincipal = melhor
		if comFuncao > 0 {
			c.FracaoPrincipal = float64(n) / float64(comFuncao)
		}
		if melhor != "" && part.Posicao != "" {
			fora := part.Posicao != melhor
			c.ForaDaFuncao = &fora
		}
		mono := float64(c.CampeaoVezes)/float64(c.CampeaoDe) >= Param.FracaoMono
		primeira := c.CampeaoVezes == 0
		c.Mono, c.SemOCampeao = &mono, &primeira
	} else {
		onde := "busque o contexto dos jogadores na tela da partida (cliente do LoL ou API da Riot)"
		if temExt {
			onde = "o histórico buscado não tinha mais partidas de antes"
		}
		c.Falta = append(c.Falta, fmt.Sprintf("função principal e campeão: %d partida(s) de antes desta (mín. %d); %s",
			len(todos), Param.MinPartidasContexto, onde))
	}
	if e, ok := eloDoJogador(db, part.Puuid); ok {
		c.Elo = &e
	} else {
		c.Falta = append(c.Falta, "elo: nenhuma rota permitida do LCU dá; só a API da Riot (league-v4), na busca do contexto com a chave")
	}
	return c
}

func nomeFonteHist(f string) string {
	switch f {
	case "cliente":
		return "cliente do LoL"
	case "riot":
		return "API da Riot (match-v5)"
	}
	return f
}

// dataDeMs: 1794800000000 → "2026-11-15" (UTC).
func dataDeMs(ms int64) string {
	if ms <= 0 {
		return "?"
	}
	return time.UnixMilli(ms).UTC().Format("2006-01-02")
}

// ContextoDosJogadores: o contexto de cada um dos 10 da partida, calculado
// agora do banco e do histórico buscado (sem a base de fatos: serve à tela do
// contexto e aos testes mesmo com a partida não analisada).
func ContextoDosJogadores(db *banco.DB, gameID int64) (map[int]ContextoJogador, error) {
	pt, err := partida.Le(db, gameID)
	if err != nil {
		return nil, err
	}
	r := map[int]ContextoJogador{}
	for _, p := range pt.Participantes {
		r[p.ParticipanteID] = contextoDoJogador(db, gameID, p, pt.CriadaEmMs)
	}
	return r, nil
}
