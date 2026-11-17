package analise

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"loltracking/server/banco"
	"loltracking/server/partida"
	"loltracking/server/rastreio"
	"loltracking/server/riot"
)

// Migra cria as tabelas da análise. Todas são RECALCULÁVEIS (Recalcula apaga
// e refaz as linhas da partida), menos a linha do tempo da Riot, que é cópia
// do que a Riot devolveu (não muda depois da partida). Ver dados.md.
func Migra(db *banco.DB) error {
	return banco.Executa(db,
		`CREATE TABLE IF NOT EXISTS linhas_do_tempo_riot (
			game_id    INTEGER PRIMARY KEY,
			bruto      TEXT NOT NULL,
			gravada_em TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS retratos_abate (
			game_id      INTEGER NOT NULL,
			n            INTEGER NOT NULL,
			t_ms         INTEGER NOT NULL,
			vitima       INTEGER NOT NULL,
			matador      INTEGER NOT NULL,
			dados        TEXT NOT NULL,
			calculado_em TEXT NOT NULL,
			PRIMARY KEY (game_id, n)
		)`,
		`CREATE TABLE IF NOT EXISTS indicadores (
			game_id         INTEGER NOT NULL,
			participante_id INTEGER NOT NULL,
			indicador       TEXT NOT NULL,
			valor           REAL,
			aproximado      INTEGER NOT NULL DEFAULT 0,
			detalhe         TEXT NOT NULL DEFAULT '{}',
			calculado_em    TEXT NOT NULL,
			PRIMARY KEY (game_id, participante_id, indicador)
		)`,
		`CREATE INDEX IF NOT EXISTS indicadores_nome ON indicadores (indicador, game_id)`,
		`CREATE TABLE IF NOT EXISTS analises (
			game_id      INTEGER PRIMARY KEY,
			calculada_em TEXT NOT NULL,
			rastreados   TEXT NOT NULL DEFAULT '[]',
			passes       INTEGER NOT NULL DEFAULT 0,
			riot         INTEGER NOT NULL DEFAULT 0,
			abates       INTEGER NOT NULL DEFAULT 0,
			indicadores  INTEGER NOT NULL DEFAULT 0,
			avisos       TEXT NOT NULL DEFAULT '[]',
			ms           INTEGER NOT NULL DEFAULT 0
		)`,
	)
}

// Deps é o mundo de fora da análise. Riot é opcional (nil = sem a API da Riot).
type Deps struct {
	DB   *banco.DB
	Riot rastreio.FonteRiot
}

// ErrSemLinhaDoTempo: a partida não tem a linha do tempo do LCU guardada.
var ErrSemLinhaDoTempo = errors.New("a partida ainda não tem a linha do tempo guardada; abra a partida com o cliente do LoL aberto")

// Resumo é o resultado de Recalcula (e de GET /api/partidas/{id}/analise).
type Resumo struct {
	GameID      int64    `json:"game_id"`
	CalculadaEm string   `json:"calculada_em"`
	Rastreados  []int    `json:"rastreados"`
	Passes      int      `json:"passes"`
	Riot        bool     `json:"riot"`
	Abates      int      `json:"abates"`
	Indicadores int      `json:"indicadores"`
	Avisos      []string `json:"avisos"`
	Ms          int64    `json:"ms"`
}

// Uma análise por vez no processo: o rastreio chama ao fechar cada passe e a
// pessoa pode pedir pela rota ao mesmo tempo.
var recalculaMu sync.Mutex

// Recalcula refaz tudo o que a análise deriva de uma partida: a cadeia de
// cada passe (com os geradores e o contexto de agora), os retratos dos abates
// e os indicadores dos 10. Idempotente: rodar de novo dá o mesmo resultado.
func Recalcula(ctx context.Context, d Deps, gameID int64) (Resumo, error) {
	recalculaMu.Lock()
	defer recalculaMu.Unlock()
	inicio := time.Now()
	r := Resumo{GameID: gameID, Rastreados: []int{}, Avisos: []string{}}
	db := d.DB
	if _, err := partida.Le(db, gameID); err != nil {
		return r, err
	}
	if _, err := partida.LeLinhaDoTempo(db, gameID); errors.Is(err, partida.ErrNaoEncontrada) {
		return r, ErrSemLinhaDoTempo
	} else if err != nil {
		return r, err
	}
	// Linha do tempo da Riot: guardada na primeira vez que der.
	_, temRiot, err := LeLinhaRiot(db, gameID)
	if err != nil {
		return r, err
	}
	if !temRiot && d.Riot != nil {
		c2, cancela := context.WithTimeout(ctx, 45*time.Second)
		b, err := d.Riot.LinhaDoTempo(c2, gameID)
		cancela()
		switch {
		case err == nil:
			if err := GravaLinhaRiot(db, gameID, b); err != nil {
				return r, err
			}
		case errors.Is(err, riot.ErrSemChave):
		default:
			r.Avisos = append(r.Avisos, "sem a linha do tempo da Riot: "+err.Error())
		}
	}

	// 1. A cadeia de cada passe, de novo (com o contexto de agora).
	passes, err := rastreio.PassesCompactados(db, gameID)
	if err != nil {
		return r, err
	}
	for _, p := range passes {
		if _, err := rastreio.RegeraCadeia(db, p.ID); err != nil {
			return r, fmt.Errorf("refazer a cadeia do passe %d: %w", p.ID, err)
		}
	}
	r.Passes = len(passes)

	// 2. O que a partida tem de cada um.
	dp, err := carrega(db, gameID)
	if err != nil {
		return r, err
	}
	r.Riot = dp.riot != nil
	for pid := range dp.amostras {
		r.Rastreados = append(r.Rastreados, pid)
	}
	sort.Ints(r.Rastreados)

	// 3. Retratos e indicadores.
	retratos := dp.retratos()
	inds := dp.indicadores(retratos)
	r.Abates, r.Indicadores = len(retratos), len(inds)

	tx, err := db.Begin()
	if err != nil {
		return r, err
	}
	defer tx.Rollback()
	agora := banco.Agora()
	r.CalculadaEm = agora
	for _, q := range []string{`DELETE FROM retratos_abate WHERE game_id = ?`, `DELETE FROM indicadores WHERE game_id = ?`} {
		if _, err := tx.Exec(q, gameID); err != nil {
			return r, err
		}
	}
	for _, rt := range retratos {
		if _, err := tx.Exec(`INSERT INTO retratos_abate (game_id, n, t_ms, vitima, matador, dados, calculado_em) VALUES (?,?,?,?,?,?,?)`,
			gameID, rt.N, rt.TMs, rt.Vitima, rt.Matador, jsonTexto(rt), agora); err != nil {
			return r, err
		}
	}
	for _, in := range inds {
		if _, err := tx.Exec(`INSERT INTO indicadores (game_id, participante_id, indicador, valor, aproximado, detalhe, calculado_em)
			VALUES (?,?,?,?,?,?,?)`, gameID, in.ParticipanteID, in.Indicador, in.Valor, in.Aproximado, jsonTexto(in.Detalhe), agora); err != nil {
			return r, err
		}
	}
	r.Ms = time.Since(inicio).Milliseconds()
	if _, err := tx.Exec(`INSERT INTO analises (game_id, calculada_em, rastreados, passes, riot, abates, indicadores, avisos, ms)
		VALUES (?,?,?,?,?,?,?,?,?) ON CONFLICT (game_id) DO UPDATE SET calculada_em = excluded.calculada_em,
		rastreados = excluded.rastreados, passes = excluded.passes, riot = excluded.riot, abates = excluded.abates,
		indicadores = excluded.indicadores, avisos = excluded.avisos, ms = excluded.ms`,
		gameID, agora, jsonTexto(r.Rastreados), r.Passes, r.Riot, r.Abates, r.Indicadores, jsonTexto(r.Avisos), r.Ms); err != nil {
		return r, err
	}
	return r, tx.Commit()
}

// RecalculaEmSegundoPlano é o que o rastreio chama ao fechar um passe: não
// segura o próximo passe, e erro vira só log.
func RecalculaEmSegundoPlano(d Deps) func(gameID int64) {
	return func(gameID int64) {
		go func() {
			if _, err := Recalcula(context.Background(), d, gameID); err != nil {
				log.Printf("análise da partida %d: %v", gameID, err)
			}
		}()
	}
}

// LeResumo devolve a última análise da partida (ok falso se nunca rodou).
func LeResumo(db *banco.DB, gameID int64) (Resumo, bool, error) {
	r := Resumo{GameID: gameID}
	var rast, av string
	err := db.QueryRow(`SELECT calculada_em, rastreados, passes, riot, abates, indicadores, avisos, ms FROM analises WHERE game_id = ?`,
		gameID).Scan(&r.CalculadaEm, &rast, &r.Passes, &r.Riot, &r.Abates, &r.Indicadores, &av, &r.Ms)
	if err == sql.ErrNoRows {
		return r, false, nil
	}
	if err != nil {
		return r, false, err
	}
	json.Unmarshal([]byte(rast), &r.Rastreados)
	json.Unmarshal([]byte(av), &r.Avisos)
	if r.Rastreados == nil {
		r.Rastreados = []int{}
	}
	if r.Avisos == nil {
		r.Avisos = []string{}
	}
	return r, true, nil
}

// --- linha do tempo da Riot ---------------------------------------------------

// GravaLinhaRiot guarda o JSON cru da match-v5 timeline.
func GravaLinhaRiot(db *banco.DB, gameID int64, bruto []byte) error {
	_, err := db.Exec(`INSERT INTO linhas_do_tempo_riot (game_id, bruto, gravada_em) VALUES (?,?,?)
		ON CONFLICT (game_id) DO UPDATE SET bruto = excluded.bruto, gravada_em = excluded.gravada_em`, gameID, string(bruto), banco.Agora())
	return err
}

// LeLinhaRiot devolve a linha do tempo da Riot guardada.
func LeLinhaRiot(db *banco.DB, gameID int64) ([]byte, bool, error) {
	var s string
	err := db.QueryRow(`SELECT bruto FROM linhas_do_tempo_riot WHERE game_id = ?`, gameID).Scan(&s)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return []byte(s), true, nil
}

// eventosRiot lê os eventos da linha do tempo da Riot no formato do rastreio
// (o JSON da Riot é o do LCU dentro de "info").
func eventosRiot(bruto []byte) []rastreio.EventoLT {
	var r struct {
		Info json.RawMessage `json:"info"`
	}
	if json.Unmarshal(bruto, &r) != nil || len(r.Info) == 0 {
		return nil
	}
	lt, err := rastreio.LeLinhaDoTempo(r.Info)
	if err != nil {
		return nil
	}
	return lt.Eventos
}

func init() {
	// Os geradores da análise usam as placas de torre da Riot (o LCU não tem).
	rastreio.RegistraContexto("analise_riot", func(db *banco.DB, c *rastreio.ContextoCadeia) {
		b, ok, err := LeLinhaRiot(db, c.GameID)
		if err != nil || !ok {
			return
		}
		if c.Extra == nil {
			c.Extra = map[string]any{}
		}
		c.Extra[ExtraEventosRiot] = eventosRiot(b)
	})
}

func jsonTexto(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// carrega lê tudo o que a análise usa de uma partida (sem refazer a cadeia).
func carrega(db *banco.DB, gameID int64) (*dadosPartida, error) {
	pt, err := partida.Le(db, gameID)
	if err != nil {
		return nil, err
	}
	bruto, err := partida.LeLinhaDoTempo(db, gameID)
	if errors.Is(err, partida.ErrNaoEncontrada) {
		return nil, ErrSemLinhaDoTempo
	}
	if err != nil {
		return nil, err
	}
	lt, err := rastreio.LeLinhaDoTempo(bruto)
	if err != nil {
		return nil, err
	}
	dp := &dadosPartida{pt: pt, lt: lt, amostras: map[int][]rastreio.Amostra{}, cadeias: map[int][]rastreio.ItemCadeia{}}
	for _, part := range pt.Participantes {
		p, ok, err := rastreio.MelhorPasse(db, gameID, part.ParticipanteID)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		as, err := rastreio.AmostrasDoPasse(db, p)
		if err != nil {
			return nil, err
		}
		if len(as) == 0 {
			continue
		}
		dp.amostras[part.ParticipanteID] = as
		if rc, err := rastreio.ListaCadeia(db, gameID, part.ParticipanteID, nil); err == nil {
			dp.cadeias[part.ParticipanteID] = rc.Itens
		}
	}
	if dp.estados, err = rastreio.EstadosDaPartida(db, gameID); err != nil {
		return nil, err
	}
	if rb, ok, err := LeLinhaRiot(db, gameID); err != nil {
		return nil, err
	} else if ok {
		dp.riot = leAbatesRiot(rb)
		dp.quadrosRiot = leQuadrosRiot(rb)
		dp.wards = leWardsRiot(rb)
	}
	return dp, nil
}
