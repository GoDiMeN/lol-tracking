package rastreio

import (
	"database/sql"
	"fmt"
	"math"
	"strconv"
	"strings"

	"loltracking/server/banco"
)

// Passes de visão de time: dois por partida (a visão do azul e a do
// vermelho), em que o motor não segue ninguém: aperta F1 (ou F2) uma vez,
// confere pelo painel que a névoa mudou, e só toca o replay capturando o
// minimapa. O que vale guardar fica em arquivo (visao_arquivo.go); a base tem
// o estado de cada passe (passes_visao) e, só durante o passe, as capturas
// lidas (visao_capturas_brutas, para pausar e retomar como os outros passes).

func migraVisao(db *banco.DB) error {
	return banco.Executa(db,
		`CREATE TABLE IF NOT EXISTS passes_visao (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			game_id       INTEGER NOT NULL,
			rastreio_id   INTEGER NOT NULL DEFAULT 0,
			time          INTEGER NOT NULL,
			ordem         INTEGER NOT NULL DEFAULT 0,
			estado        TEXT NOT NULL DEFAULT 'pendente',
			velocidade    REAL NOT NULL,
			t_inicio_ms   INTEGER NOT NULL DEFAULT 0,
			t_fim_ms      INTEGER NOT NULL DEFAULT 0,
			t_atual_ms    INTEGER NOT NULL DEFAULT 0,
			duracao_ms    INTEGER NOT NULL DEFAULT 0,
			capturas      INTEGER NOT NULL DEFAULT 0,
			capturas_modo_ok INTEGER NOT NULL DEFAULT 0,
			taxa          REAL,
			teclas        INTEGER NOT NULL DEFAULT 0,
			iniciado_em   TEXT NOT NULL DEFAULT '',
			terminado_em  TEXT NOT NULL DEFAULT '',
			erro          TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS passes_visao_partida ON passes_visao (game_id, time)`,
		`CREATE INDEX IF NOT EXISTS passes_visao_rastreio ON passes_visao (rastreio_id, ordem)`,
		// Tabela de trabalho: as capturas lidas do passe em andamento.
		`CREATE TABLE IF NOT EXISTS visao_capturas_brutas (
			passe_id   INTEGER NOT NULL,
			t_ms       INTEGER NOT NULL,
			modo       TEXT NOT NULL,
			retratos   TEXT NOT NULL DEFAULT '',
			sentinelas TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS visao_capturas_brutas_passe ON visao_capturas_brutas (passe_id, t_ms)`,
	)
}

// PasseVisao é o passe de visão de um time numa partida.
type PasseVisao struct {
	ID         int64   `json:"id"`
	GameID     int64   `json:"game_id"`
	RastreioID int64   `json:"rastreio_id"`
	Time       int     `json:"time"`
	Ordem      int     `json:"ordem"`
	Estado     string  `json:"estado"`
	Velocidade float64 `json:"velocidade"`
	TInicioMs  int64   `json:"t_inicio_ms"`
	TFimMs     int64   `json:"t_fim_ms"`
	TAtualMs   int64   `json:"t_atual_ms"`
	DuracaoMs  int64   `json:"duracao_ms"`
	Capturas   int     `json:"capturas"`
	// CapturasModoOK: capturas em que o painel conferiu a névoa do time.
	CapturasModoOK int      `json:"capturas_modo_ok"`
	Taxa           *float64 `json:"taxa"`
	// Teclas: quantas vezes F1/F2 (e F3 no fim) foram apertadas.
	Teclas      int    `json:"teclas"`
	IniciadoEm  string `json:"iniciado_em"`
	TerminadoEm string `json:"terminado_em"`
	Erro        string `json:"erro"`
}

const colunasPasseVisao = `id, game_id, rastreio_id, time, ordem, estado, velocidade, t_inicio_ms, t_fim_ms, t_atual_ms,
	duracao_ms, capturas, capturas_modo_ok, taxa, teclas, iniciado_em, terminado_em, erro`

func lePassesVisao(db *banco.DB, filtro string, args ...any) ([]PasseVisao, error) {
	linhas, err := db.Query(`SELECT `+colunasPasseVisao+` FROM passes_visao `+filtro, args...)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	lista := []PasseVisao{}
	for linhas.Next() {
		var p PasseVisao
		var taxa sql.NullFloat64
		if err := linhas.Scan(&p.ID, &p.GameID, &p.RastreioID, &p.Time, &p.Ordem, &p.Estado, &p.Velocidade, &p.TInicioMs,
			&p.TFimMs, &p.TAtualMs, &p.DuracaoMs, &p.Capturas, &p.CapturasModoOK, &taxa, &p.Teclas, &p.IniciadoEm,
			&p.TerminadoEm, &p.Erro); err != nil {
			return nil, err
		}
		p.Taxa = nf(taxa)
		lista = append(lista, p)
	}
	return lista, linhas.Err()
}

// LePasseVisao devolve um passe de visão.
func LePasseVisao(db *banco.DB, id int64) (PasseVisao, error) {
	l, err := lePassesVisao(db, `WHERE id = ?`, id)
	if err != nil {
		return PasseVisao{}, err
	}
	if len(l) == 0 {
		return PasseVisao{}, fmt.Errorf("passe de visão %d: %w", id, ErrNaoEncontrado)
	}
	return l[0], nil
}

// PassesVisaoDaPartida: todos os passes de visão da partida, do mais velho ao mais novo.
func PassesVisaoDaPartida(db *banco.DB, gameID int64) ([]PasseVisao, error) {
	return lePassesVisao(db, `WHERE game_id = ? ORDER BY id`, gameID)
}

// criaPassesVisao grava os passes de visão (um por time, na ordem dada).
func criaPassesVisao(q interface {
	Exec(string, ...any) (sql.Result, error)
}, gameID, rastreioID int64, times []int, vel float64, tIni, tFim int64) error {
	for i, t := range times {
		if _, err := q.Exec(`INSERT INTO passes_visao (game_id, rastreio_id, time, ordem, velocidade, t_inicio_ms, t_fim_ms, t_atual_ms)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, gameID, rastreioID, t, i, vel, tIni, tFim, tIni); err != nil {
			return err
		}
	}
	return nil
}

func mudaEstadoPasseVisao(db *banco.DB, id int64, estado, erro string) error {
	_, err := db.Exec(`UPDATE passes_visao SET estado = ?, erro = ?,
		iniciado_em = CASE WHEN iniciado_em = '' AND ? = 'rodando' THEN ? ELSE iniciado_em END,
		terminado_em = CASE WHEN ? IN ('concluido','cancelado','falhou') THEN ? ELSE terminado_em END WHERE id = ?`,
		estado, erro, estado, banco.Agora(), estado, banco.Agora(), id)
	return err
}

// CapturaVisao é uma captura lida no passe de visão.
type CapturaVisao struct {
	TMs        int64
	Modo       string
	Retratos   []MarcaMinimapa
	Sentinelas []MarcaMinimapa
}

// textoMarcas: "100:x:y:cob;200:x:y:cob" (posição no mapa em unidades
// inteiras; cobertura com 2 casas). Curto e legível no arquivo.
func textoMarcas(ms []MarcaMinimapa) string {
	var b strings.Builder
	for i, m := range ms {
		if i > 0 {
			b.WriteByte(';')
		}
		fmt.Fprintf(&b, "%d:%d:%d:%s", m.Time, int(math.Round(m.X)), int(math.Round(m.Y)), strconv.FormatFloat(m.Cobertura, 'f', -1, 64))
	}
	return b.String()
}

func marcasDoTexto(s string) ([]MarcaMinimapa, error) {
	var ms []MarcaMinimapa
	if s == "" {
		return ms, nil
	}
	for _, p := range strings.Split(s, ";") {
		c := strings.Split(p, ":")
		if len(c) != 4 {
			return nil, fmt.Errorf("marca fora do formato: %q", p)
		}
		t, e1 := strconv.Atoi(c[0])
		x, e2 := strconv.ParseFloat(c[1], 64)
		y, e3 := strconv.ParseFloat(c[2], 64)
		k, e4 := strconv.ParseFloat(c[3], 64)
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
			return nil, fmt.Errorf("marca fora do formato: %q", p)
		}
		ms = append(ms, MarcaMinimapa{Time: t, X: x, Y: y, Cobertura: k})
	}
	return ms, nil
}

// gravaCapturasBrutas grava um lote de capturas do passe e o progresso.
func gravaCapturasBrutas(db *banco.DB, p PasseVisao, cs []CapturaVisao, tAtual int64, total, modoOK, teclas int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, c := range cs {
		if _, err := tx.Exec(`INSERT INTO visao_capturas_brutas (passe_id, t_ms, modo, retratos, sentinelas) VALUES (?, ?, ?, ?, ?)`,
			p.ID, c.TMs, c.Modo, textoMarcas(c.Retratos), textoMarcas(c.Sentinelas)); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE passes_visao SET t_atual_ms = ?, capturas = ?, capturas_modo_ok = ?, teclas = ? WHERE id = ?`,
		tAtual, total, modoOK, teclas, p.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func leCapturasBrutas(db *banco.DB, passeID int64) ([]CapturaVisao, error) {
	linhas, err := db.Query(`SELECT t_ms, modo, retratos, sentinelas FROM visao_capturas_brutas WHERE passe_id = ? ORDER BY t_ms`, passeID)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	var cs []CapturaVisao
	for linhas.Next() {
		var c CapturaVisao
		var r, s string
		if err := linhas.Scan(&c.TMs, &c.Modo, &r, &s); err != nil {
			return nil, err
		}
		if c.Retratos, err = marcasDoTexto(r); err != nil {
			return nil, err
		}
		if c.Sentinelas, err = marcasDoTexto(s); err != nil {
			return nil, err
		}
		cs = append(cs, c)
	}
	return cs, linhas.Err()
}
