package rastreio

import (
	"database/sql"
	"encoding/json"
	"math"
	"strings"

	"loltracking/server/banco"
	"loltracking/server/replay"
)

// Fontes de evento.
const (
	FonteLinhaDoTempo = "linha_do_tempo"
	FonteJogo         = "jogo"
)

// tipoNormal põe as duas fontes no mesmo vocabulário. Os tipos que existem
// nas duas (abate, monstro, estrutura) são os que podem duplicar.
func tipoNormal(original string) string {
	switch original {
	case "CHAMPION_KILL", "ChampionKill":
		return "abate"
	case "ELITE_MONSTER_KILL", "DragonKill", "BaronKill", "HeraldKill", "HordeKill", "AtakhanKill":
		return "monstro"
	case "BUILDING_KILL", "TurretKilled", "InhibKilled":
		return "estrutura"
	case "Multikill":
		return "multiabate"
	case "FirstBlood":
		return "primeiro_abate"
	case "Ace":
		return "ace"
	case "GameStart":
		return "inicio"
	case "GameEnd":
		return "fim"
	}
	return strings.ToLower(original)
}

func podeDuplicar(tipo string) bool {
	return tipo == "abate" || tipo == "monstro" || tipo == "estrutura"
}

// Evento é uma linha de eventos.
type Evento struct {
	ID             int64           `json:"id"`
	Fonte          string          `json:"fonte"`
	Tipo           string          `json:"tipo"`
	TipoOriginal   string          `json:"tipo_original"`
	TMs            int64           `json:"t_ms"`
	QuemID         *int            `json:"quem_id"`
	QuemNome       string          `json:"quem_nome"`
	EmQuemID       *int            `json:"em_quem_id"`
	EmQuemNome     string          `json:"em_quem_nome"`
	Assistencias   []int           `json:"assistencias"`
	X              *float64        `json:"x"`
	Y              *float64        `json:"y"`
	ConfirmadoJogo bool            `json:"confirmado_jogo"`
	Dados          json.RawMessage `json:"dados"`
}

func idOuNulo(id int) *int {
	if id <= 0 {
		return nil
	}
	return &id
}

// ImportaLinhaDoTempo grava os eventos da linha do tempo do LCU (idempotente).
// nomes: participante_id → Riot ID (nome).
func ImportaLinhaDoTempo(db *banco.DB, gameID int64, lt LinhaDoTempo, nomes map[int]string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, e := range lt.Eventos {
		assist := e.Assistencias
		if assist == nil {
			assist = []int{}
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO eventos (game_id, fonte, tipo, tipo_original, t_ms, quem_id, quem_nome,
				em_quem_id, em_quem_nome, assistencias, x, y, bruto) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			gameID, FonteLinhaDoTempo, tipoNormal(e.Tipo), e.Tipo, e.TMs, idOuNulo(e.Quem), nomes[e.Quem],
			idOuNulo(e.EmQuem), nomes[e.EmQuem], jsonTexto(assist), e.X, e.Y, string(e.Bruto)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GravaEventosJogo grava o eventdata do jogo. Abate, monstro e estrutura que a
// linha do tempo já tem (mesmo tipo a ±3 s; no abate, mesma vítima) não viram
// linha nova: a linha do tempo fica, marcada confirmado_jogo = 1, porque ela
// tem a posição. idDe: nome no jogo → participante_id.
func GravaEventosJogo(db *banco.DB, gameID int64, evs []replay.EventoJogo, idDe map[string]int) (novos int, err error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	for _, e := range evs {
		tipo := tipoNormal(e.Nome)
		tMs := int64(math.Round(e.Tempo * 1000))
		vitima := idDe[e.Vitima]
		if podeDuplicar(tipo) {
			var id int64
			q := `SELECT id FROM eventos WHERE game_id = ? AND fonte = ? AND tipo = ? AND ABS(t_ms - ?) <= 3000`
			args := []any{gameID, FonteLinhaDoTempo, tipo, tMs}
			if tipo == "abate" {
				q += ` AND em_quem_id = ?`
				args = append(args, vitima)
			}
			err := tx.QueryRow(q+` ORDER BY ABS(t_ms - ?) LIMIT 1`, append(args, tMs)...).Scan(&id)
			if err == nil {
				if _, err := tx.Exec(`UPDATE eventos SET confirmado_jogo = 1 WHERE id = ?`, id); err != nil {
					return novos, err
				}
				continue
			}
			if err != sql.ErrNoRows {
				return novos, err
			}
		}
		assist := []int{}
		for _, a := range e.Assistentes {
			if id := idDe[a]; id > 0 {
				assist = append(assist, id)
			}
		}
		bruto := string(e.Bruto)
		if bruto == "" {
			bruto = "{}"
		}
		res, err := tx.Exec(`INSERT OR IGNORE INTO eventos (game_id, fonte, tipo, tipo_original, t_ms, quem_id, quem_nome,
				em_quem_id, em_quem_nome, assistencias, bruto) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
			gameID, FonteJogo, tipo, e.Nome, tMs, idOuNulo(idDe[e.Matador]), e.Matador, idOuNulo(vitima), e.Vitima,
			jsonTexto(assist), bruto)
		if err != nil {
			return novos, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			novos++
		}
	}
	return novos, tx.Commit()
}

// ListaEventos devolve os eventos da partida em ordem de tempo.
func ListaEventos(db *banco.DB, gameID int64, tipo string) ([]Evento, error) {
	q := `SELECT id, fonte, tipo, tipo_original, t_ms, quem_id, quem_nome, em_quem_id, em_quem_nome, assistencias, x, y,
		confirmado_jogo, bruto FROM eventos WHERE game_id = ?`
	args := []any{gameID}
	if tipo != "" {
		q += ` AND tipo = ?`
		args = append(args, tipo)
	}
	linhas, err := db.Query(q+` ORDER BY t_ms, id`, args...)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	var lista []Evento
	for linhas.Next() {
		var e Evento
		var quem, emQuem sql.NullInt64
		var x, y sql.NullFloat64
		var assist, bruto string
		if err := linhas.Scan(&e.ID, &e.Fonte, &e.Tipo, &e.TipoOriginal, &e.TMs, &quem, &e.QuemNome, &emQuem, &e.EmQuemNome,
			&assist, &x, &y, &e.ConfirmadoJogo, &bruto); err != nil {
			return nil, err
		}
		if quem.Valid {
			v := int(quem.Int64)
			e.QuemID = &v
		}
		if emQuem.Valid {
			v := int(emQuem.Int64)
			e.EmQuemID = &v
		}
		e.X, e.Y = nf(x), nf(y)
		json.Unmarshal([]byte(assist), &e.Assistencias)
		if e.Assistencias == nil {
			e.Assistencias = []int{}
		}
		e.Dados = json.RawMessage(bruto)
		lista = append(lista, e)
	}
	return lista, linhas.Err()
}
