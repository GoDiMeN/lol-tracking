// Package partida guarda as partidas vindas do cliente do LoL (dados finais
// dos 10, linha do tempo por minuto), sabe o estado do replay de cada uma e
// baixa o replay como execução com progresso por SSE.
package partida

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"

	"loltracking/server/banco"
	"loltracking/server/campeao"
	"loltracking/server/lcu"
)

// Migra cria as tabelas do pacote. Ver docs/arquitetura/dados.md.
func Migra(db *banco.DB) error {
	return banco.Executa(db,
		`CREATE TABLE IF NOT EXISTS partidas (
			game_id          INTEGER PRIMARY KEY,
			plataforma       TEXT NOT NULL DEFAULT '',
			fila             INTEGER NOT NULL DEFAULT 0,
			modo             TEXT NOT NULL DEFAULT '',
			tipo             TEXT NOT NULL DEFAULT '',
			mapa             INTEGER NOT NULL DEFAULT 0,
			versao           TEXT NOT NULL DEFAULT '',
			duracao_s        INTEGER NOT NULL DEFAULT 0,
			criada_em        TEXT NOT NULL DEFAULT '',
			criada_em_ms     INTEGER NOT NULL DEFAULT 0,
			time_vencedor    INTEGER NOT NULL DEFAULT 0,
			completa         INTEGER NOT NULL DEFAULT 0,
			estado_replay    TEXT NOT NULL DEFAULT '',
			progresso_replay REAL NOT NULL DEFAULT 0,
			estado_replay_em TEXT NOT NULL DEFAULT '',
			bruto            TEXT NOT NULL,
			gravada_em       TEXT NOT NULL,
			atualizada_em    TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS participantes (
			game_id              INTEGER NOT NULL,
			participante_id      INTEGER NOT NULL,
			puuid                TEXT NOT NULL DEFAULT '',
			nome                 TEXT NOT NULL DEFAULT '',
			tag                  TEXT NOT NULL DEFAULT '',
			campeao_id           INTEGER NOT NULL DEFAULT 0,
			campeao              TEXT NOT NULL DEFAULT '',
			time                 INTEGER NOT NULL DEFAULT 0,
			posicao              TEXT NOT NULL DEFAULT '',
			rota_lcu             TEXT NOT NULL DEFAULT '',
			papel_lcu            TEXT NOT NULL DEFAULT '',
			venceu               INTEGER NOT NULL DEFAULT 0,
			abates               INTEGER NOT NULL DEFAULT 0,
			mortes               INTEGER NOT NULL DEFAULT 0,
			assistencias         INTEGER NOT NULL DEFAULT 0,
			cs                   INTEGER NOT NULL DEFAULT 0,
			cs_rota              INTEGER NOT NULL DEFAULT 0,
			cs_selva             INTEGER NOT NULL DEFAULT 0,
			nivel                INTEGER NOT NULL DEFAULT 0,
			ouro                 INTEGER NOT NULL DEFAULT 0,
			ouro_gasto           INTEGER NOT NULL DEFAULT 0,
			dano_campeoes        INTEGER NOT NULL DEFAULT 0,
			dano_fisico_campeoes INTEGER NOT NULL DEFAULT 0,
			dano_magico_campeoes INTEGER NOT NULL DEFAULT 0,
			dano_verdadeiro_campeoes INTEGER NOT NULL DEFAULT 0,
			dano_total           INTEGER NOT NULL DEFAULT 0,
			dano_recebido        INTEGER NOT NULL DEFAULT 0,
			dano_mitigado        INTEGER NOT NULL DEFAULT 0,
			dano_objetivos       INTEGER NOT NULL DEFAULT 0,
			dano_torres          INTEGER NOT NULL DEFAULT 0,
			cura_total           INTEGER NOT NULL DEFAULT 0,
			tempo_cc             INTEGER NOT NULL DEFAULT 0,
			visao                INTEGER NOT NULL DEFAULT 0,
			wards_colocadas      INTEGER NOT NULL DEFAULT 0,
			wards_destruidas     INTEGER NOT NULL DEFAULT 0,
			sentinelas_compradas INTEGER NOT NULL DEFAULT 0,
			torres               INTEGER NOT NULL DEFAULT 0,
			inibidores           INTEGER NOT NULL DEFAULT 0,
			maior_multiabate     INTEGER NOT NULL DEFAULT 0,
			maior_sequencia      INTEGER NOT NULL DEFAULT 0,
			duplos               INTEGER NOT NULL DEFAULT 0,
			triplos              INTEGER NOT NULL DEFAULT 0,
			quadras              INTEGER NOT NULL DEFAULT 0,
			pentas               INTEGER NOT NULL DEFAULT 0,
			primeiro_abate       INTEGER NOT NULL DEFAULT 0,
			maior_tempo_vivo     INTEGER NOT NULL DEFAULT 0,
			feitico1             INTEGER NOT NULL DEFAULT 0,
			feitico2             INTEGER NOT NULL DEFAULT 0,
			itens                TEXT NOT NULL DEFAULT '[]',
			runa_principal       INTEGER NOT NULL DEFAULT 0,
			estilo_primario      INTEGER NOT NULL DEFAULT 0,
			estilo_secundario    INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (game_id, participante_id)
		)`,
		`CREATE INDEX IF NOT EXISTS participantes_puuid ON participantes (puuid, game_id)`,
		`CREATE TABLE IF NOT EXISTS linhas_do_tempo (
			game_id    INTEGER PRIMARY KEY,
			quadros    INTEGER NOT NULL DEFAULT 0,
			bruto      TEXT NOT NULL,
			gravada_em TEXT NOT NULL
		)`,
	)
}

// Deps é o que o pacote precisa do mundo de fora.
type Deps struct {
	DB       *banco.DB
	LCU      lcu.Cliente
	Campeoes campeao.Nomes
}

// ErrNaoEncontrada: a partida não está no banco nem no cliente do LoL.
var ErrNaoEncontrada = errors.New("partida não encontrada")

// ErroInvalido é pedido malformado (400).
type ErroInvalido struct{ Msg string }

func (e *ErroInvalido) Error() string { return e.Msg }

// --- O formato do LCU ---------------------------------------------------------

// jogoLCU é o pedaço de /lol-match-history/v1/games/{id} (e de cada item do
// histórico) que vira coluna. O resto fica no JSON bruto.
type jogoLCU struct {
	GameID       int64  `json:"gameId"`
	PlatformID   string `json:"platformId"`
	GameCreation int64  `json:"gameCreation"`
	GameDuration int    `json:"gameDuration"`
	QueueID      int    `json:"queueId"`
	MapID        int    `json:"mapId"`
	GameVersion  string `json:"gameVersion"`
	GameMode     string `json:"gameMode"`
	GameType     string `json:"gameType"`
	Teams        []struct {
		TeamID int    `json:"teamId"`
		Win    string `json:"win"`
	} `json:"teams"`
	Participants []struct {
		ParticipantID int `json:"participantId"`
		TeamID        int `json:"teamId"`
		ChampionID    int `json:"championId"`
		Spell1ID      int `json:"spell1Id"`
		Spell2ID      int `json:"spell2Id"`
		Timeline      struct {
			Lane string `json:"lane"`
			Role string `json:"role"`
		} `json:"timeline"`
		Stats statsLCU `json:"stats"`
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

type statsLCU struct {
	Win                            bool  `json:"win"`
	Kills                          int   `json:"kills"`
	Deaths                         int   `json:"deaths"`
	Assists                        int   `json:"assists"`
	TotalMinionsKilled             int   `json:"totalMinionsKilled"`
	NeutralMinionsKilled           int   `json:"neutralMinionsKilled"`
	ChampLevel                     int   `json:"champLevel"`
	GoldEarned                     int   `json:"goldEarned"`
	GoldSpent                      int   `json:"goldSpent"`
	TotalDamageDealtToChampions    int64 `json:"totalDamageDealtToChampions"`
	PhysicalDamageDealtToChampions int64 `json:"physicalDamageDealtToChampions"`
	MagicDamageDealtToChampions    int64 `json:"magicDamageDealtToChampions"`
	TrueDamageDealtToChampions     int64 `json:"trueDamageDealtToChampions"`
	TotalDamageDealt               int64 `json:"totalDamageDealt"`
	TotalDamageTaken               int64 `json:"totalDamageTaken"`
	DamageSelfMitigated            int64 `json:"damageSelfMitigated"`
	DamageDealtToObjectives        int64 `json:"damageDealtToObjectives"`
	DamageDealtToTurrets           int64 `json:"damageDealtToTurrets"`
	TotalHeal                      int64 `json:"totalHeal"`
	TimeCCingOthers                int   `json:"timeCCingOthers"`
	VisionScore                    int   `json:"visionScore"`
	WardsPlaced                    int   `json:"wardsPlaced"`
	WardsKilled                    int   `json:"wardsKilled"`
	VisionWardsBoughtInGame        int   `json:"visionWardsBoughtInGame"`
	TurretKills                    int   `json:"turretKills"`
	InhibitorKills                 int   `json:"inhibitorKills"`
	LargestMultiKill               int   `json:"largestMultiKill"`
	LargestKillingSpree            int   `json:"largestKillingSpree"`
	DoubleKills                    int   `json:"doubleKills"`
	TripleKills                    int   `json:"tripleKills"`
	QuadraKills                    int   `json:"quadraKills"`
	PentaKills                     int   `json:"pentaKills"`
	FirstBloodKill                 bool  `json:"firstBloodKill"`
	LongestTimeSpentLiving         int   `json:"longestTimeSpentLiving"`
	Item0                          int   `json:"item0"`
	Item1                          int   `json:"item1"`
	Item2                          int   `json:"item2"`
	Item3                          int   `json:"item3"`
	Item4                          int   `json:"item4"`
	Item5                          int   `json:"item5"`
	Item6                          int   `json:"item6"`
	Perk0                          int   `json:"perk0"`
	PerkPrimaryStyle               int   `json:"perkPrimaryStyle"`
	PerkSubStyle                   int   `json:"perkSubStyle"`
}

// --- O que a API devolve --------------------------------------------------------

// Participante é um dos 10 jogadores de uma partida, com os dados finais.
type Participante struct {
	GameID                 int64  `json:"game_id"`
	ParticipanteID         int    `json:"participante_id"`
	Puuid                  string `json:"puuid"`
	Nome                   string `json:"nome"`
	Tag                    string `json:"tag"`
	CampeaoID              int    `json:"campeao_id"`
	Campeao                string `json:"campeao"`
	Time                   int    `json:"time"`
	Posicao                string `json:"posicao"`
	RotaLCU                string `json:"rota_lcu"`
	PapelLCU               string `json:"papel_lcu"`
	Venceu                 bool   `json:"venceu"`
	Abates                 int    `json:"abates"`
	Mortes                 int    `json:"mortes"`
	Assistencias           int    `json:"assistencias"`
	CS                     int    `json:"cs"`
	CSRota                 int    `json:"cs_rota"`
	CSSelva                int    `json:"cs_selva"`
	Nivel                  int    `json:"nivel"`
	Ouro                   int    `json:"ouro"`
	OuroGasto              int    `json:"ouro_gasto"`
	DanoCampeoes           int64  `json:"dano_campeoes"`
	DanoFisicoCampeoes     int64  `json:"dano_fisico_campeoes"`
	DanoMagicoCampeoes     int64  `json:"dano_magico_campeoes"`
	DanoVerdadeiroCampeoes int64  `json:"dano_verdadeiro_campeoes"`
	DanoTotal              int64  `json:"dano_total"`
	DanoRecebido           int64  `json:"dano_recebido"`
	DanoMitigado           int64  `json:"dano_mitigado"`
	DanoObjetivos          int64  `json:"dano_objetivos"`
	DanoTorres             int64  `json:"dano_torres"`
	CuraTotal              int64  `json:"cura_total"`
	TempoCC                int    `json:"tempo_cc"`
	Visao                  int    `json:"visao"`
	WardsColocadas         int    `json:"wards_colocadas"`
	WardsDestruidas        int    `json:"wards_destruidas"`
	SentinelasCompradas    int    `json:"sentinelas_compradas"`
	Torres                 int    `json:"torres"`
	Inibidores             int    `json:"inibidores"`
	MaiorMultiabate        int    `json:"maior_multiabate"`
	MaiorSequencia         int    `json:"maior_sequencia"`
	Duplos                 int    `json:"duplos"`
	Triplos                int    `json:"triplos"`
	Quadras                int    `json:"quadras"`
	Pentas                 int    `json:"pentas"`
	PrimeiroAbate          bool   `json:"primeiro_abate"`
	MaiorTempoVivo         int    `json:"maior_tempo_vivo"`
	Feitico1               int    `json:"feitico1"`
	Feitico2               int    `json:"feitico2"`
	Itens                  []int  `json:"itens"`
	RunaPrincipal          int    `json:"runa_principal"`
	EstiloPrimario         int    `json:"estilo_primario"`
	EstiloSecundario       int    `json:"estilo_secundario"`
}

// Replay é o estado do replay de uma partida, na linguagem do app:
//   - baixavel: dá para baixar (LCU "download"/"retryDownload");
//   - baixando: download em andamento (LCU "downloading"/"checking");
//   - baixado: o .rofl está no disco (LCU "watch");
//   - incompativel: patch diferente do instalado (LCU "incompatible"), não abre mais;
//   - indisponivel: o servidor não tem mais o replay, ou deu erro.
type Replay struct {
	Estado    string  `json:"estado"`
	Progresso float64 `json:"progresso"`
	// EstadoLCU é o texto cru do cliente, para diagnóstico.
	EstadoLCU string `json:"estado_lcu"`
	Detalhe   string `json:"detalhe,omitempty"`
}

// Partida é a partida inteira.
type Partida struct {
	GameID          int64          `json:"game_id"`
	Plataforma      string         `json:"plataforma"`
	Fila            int            `json:"fila"`
	FilaNome        string         `json:"fila_nome"`
	Modo            string         `json:"modo"`
	Tipo            string         `json:"tipo"`
	Mapa            int            `json:"mapa"`
	Versao          string         `json:"versao"`
	Patch           string         `json:"patch"`
	DuracaoS        int            `json:"duracao_s"`
	CriadaEm        string         `json:"criada_em"`
	CriadaEmMs      int64          `json:"criada_em_ms"`
	TimeVencedor    int            `json:"time_vencedor"`
	Completa        bool           `json:"completa"`
	Replay          Replay         `json:"replay"`
	TemLinhaDoTempo bool           `json:"tem_linha_do_tempo"`
	Participantes   []Participante `json:"participantes"`
}

// Resumo é uma linha da lista de partidas de um jogador: a partida vista por ele.
type Resumo struct {
	GameID          int64  `json:"game_id"`
	Fila            int    `json:"fila"`
	FilaNome        string `json:"fila_nome"`
	Modo            string `json:"modo"`
	Versao          string `json:"versao"`
	Patch           string `json:"patch"`
	DuracaoS        int    `json:"duracao_s"`
	CriadaEm        string `json:"criada_em"`
	Venceu          bool   `json:"venceu"`
	CampeaoID       int    `json:"campeao_id"`
	Campeao         string `json:"campeao"`
	Posicao         string `json:"posicao"`
	Abates          int    `json:"abates"`
	Mortes          int    `json:"mortes"`
	Assistencias    int    `json:"assistencias"`
	CS              int    `json:"cs"`
	Ouro            int    `json:"ouro"`
	DanoCampeoes    int64  `json:"dano_campeoes"`
	Completa        bool   `json:"completa"`
	TemLinhaDoTempo bool   `json:"tem_linha_do_tempo"`
	Replay          Replay `json:"replay"`
}

// --- Conversão do LCU -----------------------------------------------------------

func leJogo(bruto []byte) (jogoLCU, error) {
	var j jogoLCU
	if err := json.Unmarshal(bruto, &j); err != nil {
		return j, fmt.Errorf("partida do LCU em formato inesperado: %w", err)
	}
	if j.GameID == 0 {
		return j, fmt.Errorf("partida do LCU sem gameId")
	}
	return j, nil
}

// participantesDe monta os participantes. A posição vem da ordem dentro do
// time (ver posicoesPorOrdem), só em Summoner's Rift com 5 por time.
func participantesDe(ctx context.Context, j jogoLCU, nomes campeao.Nomes) []Participante {
	ident := map[int]int{}
	for i, pi := range j.ParticipantIdentities {
		ident[pi.ParticipantID] = i
	}
	porTime := map[int][]int{}
	for _, p := range j.Participants {
		porTime[p.TeamID] = append(porTime[p.TeamID], p.ParticipantID)
	}
	posicao := map[int]string{}
	if j.MapID == 11 {
		for _, ids := range porTime {
			if len(ids) != 5 {
				continue
			}
			sort.Ints(ids)
			for k, id := range ids {
				posicao[id] = posicoesPorOrdem[k]
			}
		}
	}
	var lista []Participante
	for _, p := range j.Participants {
		s := p.Stats
		pt := Participante{
			GameID: j.GameID, ParticipanteID: p.ParticipantID, CampeaoID: p.ChampionID,
			Campeao: nomes.Nome(ctx, p.ChampionID), Time: p.TeamID, Posicao: posicao[p.ParticipantID],
			RotaLCU: p.Timeline.Lane, PapelLCU: p.Timeline.Role, Venceu: s.Win,
			Abates: s.Kills, Mortes: s.Deaths, Assistencias: s.Assists,
			CS: s.TotalMinionsKilled + s.NeutralMinionsKilled, CSRota: s.TotalMinionsKilled, CSSelva: s.NeutralMinionsKilled,
			Nivel: s.ChampLevel, Ouro: s.GoldEarned, OuroGasto: s.GoldSpent,
			DanoCampeoes: s.TotalDamageDealtToChampions, DanoFisicoCampeoes: s.PhysicalDamageDealtToChampions,
			DanoMagicoCampeoes: s.MagicDamageDealtToChampions, DanoVerdadeiroCampeoes: s.TrueDamageDealtToChampions,
			DanoTotal: s.TotalDamageDealt, DanoRecebido: s.TotalDamageTaken, DanoMitigado: s.DamageSelfMitigated,
			DanoObjetivos: s.DamageDealtToObjectives, DanoTorres: s.DamageDealtToTurrets, CuraTotal: s.TotalHeal,
			TempoCC: s.TimeCCingOthers, Visao: s.VisionScore, WardsColocadas: s.WardsPlaced, WardsDestruidas: s.WardsKilled,
			SentinelasCompradas: s.VisionWardsBoughtInGame, Torres: s.TurretKills, Inibidores: s.InhibitorKills,
			MaiorMultiabate: s.LargestMultiKill, MaiorSequencia: s.LargestKillingSpree,
			Duplos: s.DoubleKills, Triplos: s.TripleKills, Quadras: s.QuadraKills, Pentas: s.PentaKills,
			PrimeiroAbate: s.FirstBloodKill, MaiorTempoVivo: s.LongestTimeSpentLiving,
			Feitico1: p.Spell1ID, Feitico2: p.Spell2ID,
			Itens:         []int{s.Item0, s.Item1, s.Item2, s.Item3, s.Item4, s.Item5, s.Item6},
			RunaPrincipal: s.Perk0, EstiloPrimario: s.PerkPrimaryStyle, EstiloSecundario: s.PerkSubStyle,
		}
		if i, ok := ident[p.ParticipantID]; ok {
			pl := j.ParticipantIdentities[i].Player
			pt.Puuid, pt.Nome, pt.Tag = pl.Puuid, pl.GameName, pl.TagLine
		}
		lista = append(lista, pt)
	}
	sort.Slice(lista, func(a, b int) bool { return lista[a].ParticipanteID < lista[b].ParticipanteID })
	return lista
}

func timeVencedor(j jogoLCU) int {
	for _, t := range j.Teams {
		if t.Win == "Win" {
			return t.TeamID
		}
	}
	// O item do histórico não traz teams: deduz pelo participante que venceu.
	for _, p := range j.Participants {
		if p.Stats.Win {
			return p.TeamID
		}
	}
	return 0
}

// Grava insere ou atualiza a partida e os participantes numa transação.
// completa=true quando o JSON veio de games/{id} com os 10; uma gravação
// incompleta (do histórico, só o dono) nunca sobrescreve uma completa.
func Grava(ctx context.Context, d Deps, bruto []byte, completa bool) error {
	j, err := leJogo(bruto)
	if err != nil {
		return err
	}
	parts := participantesDe(ctx, j, d.Campeoes)
	tx, err := d.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var jaCompleta int
	err = tx.QueryRow(`SELECT completa FROM partidas WHERE game_id = ?`, j.GameID).Scan(&jaCompleta)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if jaCompleta == 1 && !completa {
		return nil
	}
	agora := banco.Agora()
	_, err = tx.Exec(`INSERT INTO partidas (game_id, plataforma, fila, modo, tipo, mapa, versao, duracao_s,
			criada_em, criada_em_ms, time_vencedor, completa, bruto, gravada_em, atualizada_em)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (game_id) DO UPDATE SET plataforma = excluded.plataforma, fila = excluded.fila,
			modo = excluded.modo, tipo = excluded.tipo, mapa = excluded.mapa, versao = excluded.versao,
			duracao_s = excluded.duracao_s, criada_em = excluded.criada_em, criada_em_ms = excluded.criada_em_ms,
			time_vencedor = excluded.time_vencedor, completa = excluded.completa, bruto = excluded.bruto,
			atualizada_em = excluded.atualizada_em`,
		j.GameID, j.PlatformID, j.QueueID, j.GameMode, j.GameType, j.MapID, j.GameVersion, j.GameDuration,
		banco.DeMilissegundos(j.GameCreation), j.GameCreation, timeVencedor(j), b2i(completa), string(bruto), agora, agora)
	if err != nil {
		return fmt.Errorf("gravar partida %d: %w", j.GameID, err)
	}
	if _, err := tx.Exec(`DELETE FROM participantes WHERE game_id = ?`, j.GameID); err != nil {
		return err
	}
	for _, p := range parts {
		itens, _ := json.Marshal(p.Itens)
		_, err := tx.Exec(`INSERT INTO participantes (game_id, participante_id, puuid, nome, tag, campeao_id, campeao,
				time, posicao, rota_lcu, papel_lcu, venceu, abates, mortes, assistencias, cs, cs_rota, cs_selva, nivel,
				ouro, ouro_gasto, dano_campeoes, dano_fisico_campeoes, dano_magico_campeoes, dano_verdadeiro_campeoes,
				dano_total, dano_recebido, dano_mitigado, dano_objetivos, dano_torres, cura_total, tempo_cc, visao,
				wards_colocadas, wards_destruidas, sentinelas_compradas, torres, inibidores, maior_multiabate,
				maior_sequencia, duplos, triplos, quadras, pentas, primeiro_abate, maior_tempo_vivo, feitico1, feitico2,
				itens, runa_principal, estilo_primario, estilo_secundario)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			p.GameID, p.ParticipanteID, p.Puuid, p.Nome, p.Tag, p.CampeaoID, p.Campeao,
			p.Time, p.Posicao, p.RotaLCU, p.PapelLCU, b2i(p.Venceu), p.Abates, p.Mortes, p.Assistencias, p.CS, p.CSRota, p.CSSelva, p.Nivel,
			p.Ouro, p.OuroGasto, p.DanoCampeoes, p.DanoFisicoCampeoes, p.DanoMagicoCampeoes, p.DanoVerdadeiroCampeoes,
			p.DanoTotal, p.DanoRecebido, p.DanoMitigado, p.DanoObjetivos, p.DanoTorres, p.CuraTotal, p.TempoCC, p.Visao,
			p.WardsColocadas, p.WardsDestruidas, p.SentinelasCompradas, p.Torres, p.Inibidores, p.MaiorMultiabate,
			p.MaiorSequencia, p.Duplos, p.Triplos, p.Quadras, p.Pentas, b2i(p.PrimeiroAbate), p.MaiorTempoVivo, p.Feitico1, p.Feitico2,
			string(itens), p.RunaPrincipal, p.EstiloPrimario, p.EstiloSecundario)
		if err != nil {
			return fmt.Errorf("gravar participante %d da partida %d: %w", p.ParticipanteID, j.GameID, err)
		}
	}
	return tx.Commit()
}

// GravaReplay guarda o último estado do replay visto (cache para quando o
// cliente do LoL estiver fechado).
func GravaReplay(db *banco.DB, gameID int64, r Replay) error {
	_, err := db.Exec(`UPDATE partidas SET estado_replay = ?, progresso_replay = ?, estado_replay_em = ? WHERE game_id = ?`,
		r.Estado, r.Progresso, banco.Agora(), gameID)
	return err
}

// GravaLinhaDoTempo guarda o JSON de game-timelines/{id}.
func GravaLinhaDoTempo(db *banco.DB, gameID int64, bruto []byte) error {
	var t struct {
		Frames []json.RawMessage `json:"frames"`
	}
	if err := json.Unmarshal(bruto, &t); err != nil {
		return fmt.Errorf("linha do tempo da partida %d em formato inesperado: %w", gameID, err)
	}
	_, err := db.Exec(`INSERT INTO linhas_do_tempo (game_id, quadros, bruto, gravada_em) VALUES (?, ?, ?, ?)
		ON CONFLICT (game_id) DO UPDATE SET quadros = excluded.quadros, bruto = excluded.bruto, gravada_em = excluded.gravada_em`,
		gameID, len(t.Frames), string(bruto), banco.Agora())
	return err
}

// LeLinhaDoTempo devolve o JSON cru guardado.
func LeLinhaDoTempo(db *banco.DB, gameID int64) ([]byte, error) {
	var s string
	err := db.QueryRow(`SELECT bruto FROM linhas_do_tempo WHERE game_id = ?`, gameID).Scan(&s)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNaoEncontrada
	}
	return []byte(s), err
}

func temLinhaDoTempo(db *banco.DB, gameID int64) bool {
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM linhas_do_tempo WHERE game_id = ?`, gameID).Scan(&n)
	return n > 0
}

func estaCompleta(db *banco.DB, gameID int64) bool {
	var n int
	db.QueryRow(`SELECT completa FROM partidas WHERE game_id = ?`, gameID).Scan(&n)
	return n == 1
}

// Le devolve a partida guardada com os participantes.
func Le(db *banco.DB, gameID int64) (Partida, error) {
	var p Partida
	var completa int
	err := db.QueryRow(`SELECT game_id, plataforma, fila, modo, tipo, mapa, versao, duracao_s, criada_em, criada_em_ms,
			time_vencedor, completa, estado_replay, progresso_replay FROM partidas WHERE game_id = ?`, gameID).
		Scan(&p.GameID, &p.Plataforma, &p.Fila, &p.Modo, &p.Tipo, &p.Mapa, &p.Versao, &p.DuracaoS, &p.CriadaEm,
			&p.CriadaEmMs, &p.TimeVencedor, &completa, &p.Replay.Estado, &p.Replay.Progresso)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNaoEncontrada
	}
	if err != nil {
		return p, err
	}
	p.Completa = completa == 1
	p.FilaNome = NomeDaFila(p.Fila)
	p.Patch = lcu.PatchDe(p.Versao)
	p.TemLinhaDoTempo = temLinhaDoTempo(db, gameID)
	p.Participantes, err = leParticipantes(db, `WHERE game_id = ? ORDER BY participante_id`, gameID)
	p.Participantes = nuncaNil(p.Participantes)
	return p, err
}

func nuncaNil(p []Participante) []Participante {
	if p == nil {
		return []Participante{}
	}
	return p
}

const colunasParticipante = `game_id, participante_id, puuid, nome, tag, campeao_id, campeao, time, posicao, rota_lcu,
	papel_lcu, venceu, abates, mortes, assistencias, cs, cs_rota, cs_selva, nivel, ouro, ouro_gasto, dano_campeoes,
	dano_fisico_campeoes, dano_magico_campeoes, dano_verdadeiro_campeoes, dano_total, dano_recebido, dano_mitigado,
	dano_objetivos, dano_torres, cura_total, tempo_cc, visao, wards_colocadas, wards_destruidas, sentinelas_compradas,
	torres, inibidores, maior_multiabate, maior_sequencia, duplos, triplos, quadras, pentas, primeiro_abate,
	maior_tempo_vivo, feitico1, feitico2, itens, runa_principal, estilo_primario, estilo_secundario`

func leParticipantes(db *banco.DB, filtro string, args ...any) ([]Participante, error) {
	linhas, err := db.Query(`SELECT `+colunasParticipante+` FROM participantes `+filtro, args...)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	var lista []Participante
	for linhas.Next() {
		var p Participante
		var venceu, primeiro int
		var itens string
		if err := linhas.Scan(&p.GameID, &p.ParticipanteID, &p.Puuid, &p.Nome, &p.Tag, &p.CampeaoID, &p.Campeao, &p.Time,
			&p.Posicao, &p.RotaLCU, &p.PapelLCU, &venceu, &p.Abates, &p.Mortes, &p.Assistencias, &p.CS, &p.CSRota,
			&p.CSSelva, &p.Nivel, &p.Ouro, &p.OuroGasto, &p.DanoCampeoes, &p.DanoFisicoCampeoes, &p.DanoMagicoCampeoes,
			&p.DanoVerdadeiroCampeoes, &p.DanoTotal, &p.DanoRecebido, &p.DanoMitigado, &p.DanoObjetivos, &p.DanoTorres,
			&p.CuraTotal, &p.TempoCC, &p.Visao, &p.WardsColocadas, &p.WardsDestruidas, &p.SentinelasCompradas, &p.Torres,
			&p.Inibidores, &p.MaiorMultiabate, &p.MaiorSequencia, &p.Duplos, &p.Triplos, &p.Quadras, &p.Pentas, &primeiro,
			&p.MaiorTempoVivo, &p.Feitico1, &p.Feitico2, &itens, &p.RunaPrincipal, &p.EstiloPrimario, &p.EstiloSecundario); err != nil {
			return nil, err
		}
		p.Venceu, p.PrimeiroAbate = venceu == 1, primeiro == 1
		json.Unmarshal([]byte(itens), &p.Itens)
		lista = append(lista, p)
	}
	return lista, linhas.Err()
}

// resumoDe monta a linha da lista para o jogador puuid a partir do banco.
func resumoDe(db *banco.DB, gameID int64, puuid string) (Resumo, error) {
	p, err := Le(db, gameID)
	if err != nil {
		return Resumo{}, err
	}
	r := Resumo{GameID: p.GameID, Fila: p.Fila, FilaNome: p.FilaNome, Modo: p.Modo, Versao: p.Versao, Patch: p.Patch,
		DuracaoS: p.DuracaoS, CriadaEm: p.CriadaEm, Completa: p.Completa, TemLinhaDoTempo: p.TemLinhaDoTempo, Replay: p.Replay}
	for _, pt := range p.Participantes {
		if pt.Puuid == puuid {
			r.Venceu, r.CampeaoID, r.Campeao, r.Posicao = pt.Venceu, pt.CampeaoID, pt.Campeao, pt.Posicao
			r.Abates, r.Mortes, r.Assistencias, r.CS, r.Ouro, r.DanoCampeoes = pt.Abates, pt.Mortes, pt.Assistencias, pt.CS, pt.Ouro, pt.DanoCampeoes
		}
	}
	return r, nil
}

// partidasNoBanco lista as partidas guardadas em que o puuid jogou (o caminho
// quando o cliente do LoL está fechado).
func partidasNoBanco(db *banco.DB, puuid string) ([]int64, error) {
	linhas, err := db.Query(`SELECT p.game_id FROM partidas p JOIN participantes pt ON pt.game_id = p.game_id
		WHERE pt.puuid = ? ORDER BY p.criada_em_ms DESC`, puuid)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	var ids []int64
	for linhas.Next() {
		var id int64
		if err := linhas.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, linhas.Err()
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
