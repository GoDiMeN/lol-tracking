// Package rastreio assiste o replay sozinho, um passe por jogador, e grava o
// que viu: a posição e o estado do jogador seguido (amostras), o estado dos 10
// a cada segundo (amostras_time) e os eventos da partida.
//
// Durante o passe as leituras vão para tabelas de trabalho (amostras_brutas,
// amostras_time_brutas), que só existem para pausar e retomar. Ao fechar o
// passe (finaliza.go) elas viram duas coisas, na mesma transação, e somem:
//   - o BLOCO COMPACTADO (passes_compactos, formato em codec.go): tudo o que
//     foi lido, colunar e comprimido; alimenta o mapa com play;
//   - a CADEIA REDUZIDA (cadeia): o que importa para contar o jogo do jogador
//     (trajeto simplificado, mortes, abates, níveis, itens, CS...), regerável
//     do bloco por RegeraCadeia.
package rastreio

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"loltracking/server/banco"
)

// Migra cria as tabelas do pacote. Ver docs/arquitetura/dados.md.
func Migra(db *banco.DB) error {
	if err := banco.Executa(db,
		`CREATE TABLE IF NOT EXISTS rastreios (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			game_id       INTEGER NOT NULL,
			estado        TEXT NOT NULL,
			velocidade    REAL NOT NULL,
			t_inicio_ms   INTEGER NOT NULL DEFAULT 0,
			t_fim_ms      INTEGER NOT NULL DEFAULT 0,
			criado_em     TEXT NOT NULL,
			atualizado_em TEXT NOT NULL,
			terminado_em  TEXT NOT NULL DEFAULT '',
			erro          TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS rastreios_partida ON rastreios (game_id)`,
		`CREATE TABLE IF NOT EXISTS passes (
			id                INTEGER PRIMARY KEY AUTOINCREMENT,
			rastreio_id       INTEGER NOT NULL,
			game_id           INTEGER NOT NULL,
			participante_id   INTEGER NOT NULL,
			ordem             INTEGER NOT NULL,
			estado            TEXT NOT NULL DEFAULT 'pendente',
			t_atual_ms        INTEGER NOT NULL DEFAULT 0,
			duracao_ms        INTEGER NOT NULL DEFAULT 0,
			amostras          INTEGER NOT NULL DEFAULT 0,
			tentativas_trava  INTEGER NOT NULL DEFAULT 0,
			retravas          INTEGER NOT NULL DEFAULT 0,
			amostras_sem_trava INTEGER NOT NULL DEFAULT 0,
			taxa              REAL,
			calibracao        TEXT NOT NULL DEFAULT '',
			calibracao_pontos INTEGER NOT NULL DEFAULT 0,
			erro_calibracao_mediano REAL,
			erro_calibracao_maximo  REAL,
			iniciado_em       TEXT NOT NULL DEFAULT '',
			terminado_em      TEXT NOT NULL DEFAULT '',
			erro              TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS passes_rastreio ON passes (rastreio_id, ordem)`,
		`CREATE INDEX IF NOT EXISTS passes_partida ON passes (game_id, participante_id)`,
		// Tabela de trabalho: as amostras do passe em andamento.
		`CREATE TABLE IF NOT EXISTS amostras_brutas (
			passe_id        INTEGER NOT NULL,
			game_id         INTEGER NOT NULL,
			participante_id INTEGER NOT NULL,
			t_ms            INTEGER NOT NULL,
			x REAL, y REAL,
			origem          TEXT NOT NULL,
			cam_x REAL, cam_z REAL, tela_x REAL, tela_y REAL,
			selecao_ok      INTEGER NOT NULL DEFAULT 0,
			trava_perdida   INTEGER NOT NULL DEFAULT 0,
			morto           INTEGER NOT NULL DEFAULT 0,
			respawn         REAL NOT NULL DEFAULT 0,
			nivel           INTEGER NOT NULL DEFAULT 0,
			cs              INTEGER NOT NULL DEFAULT 0,
			abates          INTEGER NOT NULL DEFAULT 0,
			mortes          INTEGER NOT NULL DEFAULT 0,
			assistencias    INTEGER NOT NULL DEFAULT 0,
			ward            REAL NOT NULL DEFAULT 0,
			dt_ms           INTEGER NOT NULL DEFAULT 0,
			vida REAL, vida_max REAL, mana REAL, mana_max REAL,
			cd_q REAL, cd_w REAL, cd_e REAL, cd_r REAL, cd_d REAL, cd_f REAL,
			atr_ad REAL, atr_ap REAL, atr_armadura REAL, atr_rm REAL, atr_vel_ataque REAL, atr_vel_mov REAL
		)`,
		`CREATE INDEX IF NOT EXISTS amostras_brutas_passe ON amostras_brutas (passe_id, t_ms)`,
		// Tabela de trabalho: o estado dos 10 a cada ~1 s de jogo.
		`CREATE TABLE IF NOT EXISTS amostras_time_brutas (
			passe_id        INTEGER NOT NULL,
			game_id         INTEGER NOT NULL,
			t_ms            INTEGER NOT NULL,
			participante_id INTEGER NOT NULL,
			nivel           INTEGER NOT NULL DEFAULT 0,
			abates          INTEGER NOT NULL DEFAULT 0,
			mortes          INTEGER NOT NULL DEFAULT 0,
			assistencias    INTEGER NOT NULL DEFAULT 0,
			cs              INTEGER NOT NULL DEFAULT 0,
			ward            REAL NOT NULL DEFAULT 0,
			morto           INTEGER NOT NULL DEFAULT 0,
			respawn         REAL NOT NULL DEFAULT 0,
			itens           TEXT NOT NULL DEFAULT '[]',
			vida_pct REAL, mana_pct REAL
		)`,
		`CREATE INDEX IF NOT EXISTS amostras_time_brutas_passe ON amostras_time_brutas (passe_id, t_ms)`,
		`CREATE TABLE IF NOT EXISTS passes_compactos (
			passe_id   INTEGER NOT NULL,
			conteudo   TEXT NOT NULL,
			versao     INTEGER NOT NULL,
			n          INTEGER NOT NULL,
			t_ini_ms   INTEGER NOT NULL,
			t_fim_ms   INTEGER NOT NULL,
			bytes      INTEGER NOT NULL,
			gravado_em TEXT NOT NULL,
			PRIMARY KEY (passe_id, conteudo)
		)`,
		`CREATE TABLE IF NOT EXISTS eventos (
			id               INTEGER PRIMARY KEY AUTOINCREMENT,
			game_id          INTEGER NOT NULL,
			fonte            TEXT NOT NULL,
			tipo             TEXT NOT NULL,
			tipo_original    TEXT NOT NULL,
			t_ms             INTEGER NOT NULL,
			quem_id          INTEGER,
			quem_nome        TEXT NOT NULL DEFAULT '',
			em_quem_id       INTEGER,
			em_quem_nome     TEXT NOT NULL DEFAULT '',
			assistencias     TEXT NOT NULL DEFAULT '[]',
			x REAL, y REAL,
			confirmado_jogo  INTEGER NOT NULL DEFAULT 0,
			bruto            TEXT NOT NULL DEFAULT '{}',
			UNIQUE (game_id, fonte, tipo_original, t_ms, quem_nome, em_quem_nome)
		)`,
		`CREATE INDEX IF NOT EXISTS eventos_partida ON eventos (game_id, t_ms)`,
	); err != nil {
		return err
	}
	// Tarefa 3: conferência da leitura da tela com a API da Riot (opcional).
	for _, c := range [][2]string{
		{"conferencia_riot", "TEXT NOT NULL DEFAULT ''"},
		{"conferencia_n", "INTEGER"},
		{"conferencia_iguais", "INTEGER"},
		{"conferencia_maior_dif", "REAL"},
	} {
		if err := banco.AdicionaColuna(db, "passes", c[0], c[1]); err != nil {
			return err
		}
	}
	// Banco de antes dos arquivos: converte uma vez e remove as estruturas antigas.
	if err := converteFormatoAntigo(db); err != nil {
		return fmt.Errorf("converter o banco para o formato de arquivos: %w", err)
	}
	// Recarga da passiva (Vex e afins): coluna nova na tabela de trabalho.
	if err := banco.AdicionaColuna(db, "amostras_brutas", "cd_p", "REAL"); err != nil {
		return err
	}
	// Repescagem dos buracos (buracos.go): o resumo da última, em JSON.
	if err := banco.AdicionaColuna(db, "passes", "repescagem", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	// Linhas de trabalho de passe já fechado só existem durante a repescagem de
	// um passe gravado (o bloco é carregado de volta nelas). Se o processo morreu
	// no meio, o bloco antigo continua valendo: as linhas sobrando saem.
	if _, err := db.Exec(`DELETE FROM amostras_brutas WHERE passe_id IN (SELECT id FROM passes WHERE estado IN ('concluido','cancelado'))
		AND passe_id IN (SELECT passe_id FROM passes_compactos)`); err != nil {
		return err
	}
	if _, err := db.Exec(`DELETE FROM amostras_time_brutas WHERE passe_id IN (SELECT id FROM passes WHERE estado IN ('concluido','cancelado'))
		AND passe_id IN (SELECT passe_id FROM passes_compactos)`); err != nil {
		return err
	}
	// Passes de visão de time (visao_modelo.go).
	if err := migraVisao(db); err != nil {
		return err
	}
	if _, err := db.Exec(`DELETE FROM visao_capturas_brutas WHERE passe_id IN (SELECT id FROM passes_visao WHERE estado IN ('concluido','cancelado'))`); err != nil {
		return err
	}
	return nil
}

// Estados de rastreio e de passe.
const (
	Pendente  = "pendente"
	Rodando   = "rodando"
	Pausado   = "pausado"
	Concluido = "concluido"
	Cancelado = "cancelado"
	Falhou    = "falhou"
)

// Rastreio é uma fila de passes de uma partida.
type Rastreio struct {
	ID               int64   `json:"id"`
	GameID           int64   `json:"game_id"`
	Estado           string  `json:"estado"`
	Velocidade       float64 `json:"velocidade"`
	TInicioMs        int64   `json:"t_inicio_ms"`
	TFimMs           int64   `json:"t_fim_ms"`
	CriadoEm         string  `json:"criado_em"`
	AtualizadoEm     string  `json:"atualizado_em"`
	TerminadoEm      string  `json:"terminado_em"`
	Erro             string  `json:"erro"`
	PassesTotal      int     `json:"passes_total"`
	PassesConcluidos int     `json:"passes_concluidos"`
	PasseAtual       *int    `json:"passe_atual"`
	Progresso        float64 `json:"progresso"`
	EstimativaS      float64 `json:"estimativa_s"`
	Execucao         *IDExec `json:"execucao"`
	// ComVisao: o rastreio tem os dois passes de visão de time no fim.
	ComVisao              bool `json:"com_visao"`
	PassesVisaoTotal      int  `json:"passes_visao_total"`
	PassesVisaoConcluidos int  `json:"passes_visao_concluidos"`
}

// IDExec é a referência à execução (SSE) em andamento.
type IDExec struct {
	ID string `json:"id"`
}

// Passe é o rastreio de um jogador.
type Passe struct {
	ID                    int64    `json:"id"`
	RastreioID            int64    `json:"rastreio_id"`
	GameID                int64    `json:"game_id"`
	ParticipanteID        int      `json:"participante_id"`
	Ordem                 int      `json:"ordem"`
	Estado                string   `json:"estado"`
	TAtualMs              int64    `json:"t_atual_ms"`
	DuracaoMs             int64    `json:"duracao_ms"`
	Amostras              int      `json:"amostras"`
	TentativasTrava       int      `json:"tentativas_trava"`
	Retravas              int      `json:"retravas"`
	AmostrasSemTrava      int      `json:"amostras_sem_trava"`
	Taxa                  *float64 `json:"taxa"`
	Calibracao            string   `json:"-"`
	CalibracaoPontos      int      `json:"calibracao_pontos"`
	ErroCalibracaoMediano *float64 `json:"erro_calibracao_mediano"`
	ErroCalibracaoMaximo  *float64 `json:"erro_calibracao_maximo"`
	IniciadoEm            string   `json:"iniciado_em"`
	TerminadoEm           string   `json:"terminado_em"`
	Erro                  string   `json:"erro"`
	Compactado            bool     `json:"compactado"`
	Bytes                 int      `json:"bytes"`
	// Conferência da tela com a Riot (nulo = não conferido: sem chave).
	ConferenciaN        *int64          `json:"conferencia_n"`
	ConferenciaIguais   *int64          `json:"conferencia_iguais"`
	ConferenciaMaiorDif *float64        `json:"conferencia_maior_dif"`
	Conferencia         json.RawMessage `json:"conferencia_riot"`
	// Repescagem: a última repescagem dos buracos do passe (nulo = nunca rodou).
	Repescagem *ResumoRepescagem `json:"repescagem"`
}

// ErrNaoEncontrado: rastreio ou passe que não existe.
var ErrNaoEncontrado = errors.New("rastreio não encontrado")

func criaRastreio(db *banco.DB, gameID int64, vel float64, tIni, tFim int64, participantes []int) (int64, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	agora := banco.Agora()
	res, err := tx.Exec(`INSERT INTO rastreios (game_id, estado, velocidade, t_inicio_ms, t_fim_ms, criado_em, atualizado_em)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, gameID, Pausado, vel, tIni, tFim, agora, agora)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	for i, p := range participantes {
		if _, err := tx.Exec(`INSERT INTO passes (rastreio_id, game_id, participante_id, ordem, t_atual_ms) VALUES (?, ?, ?, ?, ?)`,
			id, gameID, p, i, tIni); err != nil {
			return 0, err
		}
	}
	return id, tx.Commit()
}

const colunasRastreio = `id, game_id, estado, velocidade, t_inicio_ms, t_fim_ms, criado_em, atualizado_em, terminado_em, erro`

func leRastreios(db *banco.DB, filtro string, args ...any) ([]Rastreio, error) {
	linhas, err := db.Query(`SELECT `+colunasRastreio+` FROM rastreios `+filtro, args...)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	var lista []Rastreio
	for linhas.Next() {
		var r Rastreio
		if err := linhas.Scan(&r.ID, &r.GameID, &r.Estado, &r.Velocidade, &r.TInicioMs, &r.TFimMs, &r.CriadoEm,
			&r.AtualizadoEm, &r.TerminadoEm, &r.Erro); err != nil {
			return nil, err
		}
		lista = append(lista, r)
	}
	if err := linhas.Err(); err != nil {
		return nil, err
	}
	for i := range lista {
		completaRastreio(db, &lista[i])
	}
	return lista, nil
}

// completaRastreio calcula passe atual, progresso e estimativa a partir dos passes.
func completaRastreio(db *banco.DB, r *Rastreio) {
	ps, err := lePasses(db, `WHERE rastreio_id = ? ORDER BY ordem`, r.ID)
	if err != nil {
		return
	}
	r.PassesTotal = len(ps)
	restante, soma := 0.0, 0.0
	// Repescagem ao fim de cada passe que falta: a média de buracos dos passes
	// já repescados deste rastreio (sem nenhum, BuracosEsperadosPorPasse).
	nRep, somaBuracos, faltam := 0, 0, 0
	for _, p := range ps {
		if p.Repescagem != nil && p.Repescagem.Quando == "fim_do_passe" {
			nRep++
			somaBuracos += p.Repescagem.Buracos
		}
		if p.Estado != Concluido && p.Estado != Cancelado {
			faltam++
		}
	}
	buracosPorPasse := BuracosEsperadosPorPasse
	if nRep > 0 {
		buracosPorPasse = float64(somaBuracos) / float64(nRep)
	}
	for _, p := range ps {
		ini, fim := r.TInicioMs, r.TFimMs
		if fim <= 0 {
			fim = p.DuracaoMs
		}
		frac := 0.0
		switch {
		case p.Estado == Concluido:
			r.PassesConcluidos++
			frac = 1
		case fim > ini && p.DuracaoMs > 0:
			frac = min(1, max(0, float64(p.TAtualMs-ini)/float64(fim-ini)))
		}
		if p.Estado == Rodando && r.PasseAtual == nil {
			pid := p.ParticipanteID
			r.PasseAtual = &pid
		}
		soma += frac
		if p.Estado != Concluido && p.Estado != Cancelado {
			dur := float64(fim - ini)
			if dur <= 0 {
				dur = 35 * 60000 // duração ainda não lida: estimativa grosseira
			}
			restante += dur * (1 - frac)
		}
	}
	// Passes de visão de time pedidos junto (visao_modelo.go): entram no
	// progresso como um passe cada e na estimativa pela velocidade deles.
	pvs, _ := lePassesVisao(db, `WHERE rastreio_id = ? ORDER BY ordem`, r.ID)
	restanteVisao := 0.0
	for _, p := range pvs {
		r.ComVisao = true
		r.PassesVisaoTotal++
		ini, fim := max(r.TInicioMs, p.TInicioMs), p.TFimMs
		if fim <= 0 {
			fim = p.DuracaoMs
		}
		frac := 0.0
		switch {
		case p.Estado == Concluido:
			r.PassesVisaoConcluidos++
			frac = 1
		case p.Estado == Cancelado || p.Estado == Falhou:
			frac = 1
		case fim > ini && p.DuracaoMs > 0:
			frac = min(1, max(0, float64(p.TAtualMs-ini)/float64(fim-ini)))
		}
		soma += frac
		if frac < 1 {
			dur := float64(fim - ini)
			if dur <= 0 {
				dur = 35 * 60000
			}
			vel := p.Velocidade
			if vel <= 0 {
				vel = VelocidadeVisaoPadrao
			}
			restanteVisao += dur * (1 - frac) / vel / 1000
		}
	}
	if n := len(ps) + len(pvs); n > 0 {
		r.Progresso = soma / float64(n)
	}
	if r.Velocidade > 0 {
		r.EstimativaS = restante/1000/r.Velocidade + float64(faltam)*estimativaRepescagemS(buracosPorPasse, r.Velocidade) + restanteVisao
	}
}

// LeRastreio devolve um rastreio.
func LeRastreio(db *banco.DB, id int64) (Rastreio, error) {
	l, err := leRastreios(db, `WHERE id = ?`, id)
	if err != nil {
		return Rastreio{}, err
	}
	if len(l) == 0 {
		return Rastreio{}, ErrNaoEncontrado
	}
	return l[0], nil
}

const colunasPasse = `p.id, p.rastreio_id, p.game_id, p.participante_id, p.ordem, p.estado, p.t_atual_ms, p.duracao_ms, p.amostras,
	p.tentativas_trava, p.retravas, p.amostras_sem_trava, p.taxa, p.calibracao, p.calibracao_pontos,
	p.erro_calibracao_mediano, p.erro_calibracao_maximo, p.iniciado_em, p.terminado_em, p.erro,
	p.conferencia_n, p.conferencia_iguais, p.conferencia_maior_dif, p.conferencia_riot,
	COALESCE((SELECT SUM(bytes) FROM passes_compactos c WHERE c.passe_id = p.id), -1), p.repescagem`

func lePasses(db *banco.DB, filtro string, args ...any) ([]Passe, error) {
	linhas, err := db.Query(`SELECT `+colunasPasse+` FROM passes p `+filtro, args...)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	var lista []Passe
	for linhas.Next() {
		var p Passe
		var taxa, med, mx, cmd sql.NullFloat64
		var cn, ci sql.NullInt64
		var conf, resc string
		if err := linhas.Scan(&p.ID, &p.RastreioID, &p.GameID, &p.ParticipanteID, &p.Ordem, &p.Estado, &p.TAtualMs,
			&p.DuracaoMs, &p.Amostras, &p.TentativasTrava, &p.Retravas, &p.AmostrasSemTrava, &taxa, &p.Calibracao,
			&p.CalibracaoPontos, &med, &mx, &p.IniciadoEm, &p.TerminadoEm, &p.Erro, &cn, &ci, &cmd, &conf, &p.Bytes, &resc); err != nil {
			return nil, err
		}
		if resc != "" {
			var r ResumoRepescagem
			if json.Unmarshal([]byte(resc), &r) == nil {
				if r.Restantes == nil {
					r.Restantes = []Buraco{}
				}
				p.Repescagem = &r
			}
		}
		p.Taxa, p.ErroCalibracaoMediano, p.ErroCalibracaoMaximo = nf(taxa), nf(med), nf(mx)
		p.ConferenciaMaiorDif = nf(cmd)
		if cn.Valid {
			p.ConferenciaN, p.ConferenciaIguais = &cn.Int64, &ci.Int64
		}
		if conf != "" {
			p.Conferencia = json.RawMessage(conf)
		} else {
			p.Conferencia = json.RawMessage("null")
		}
		p.Compactado = p.Bytes >= 0
		if p.Bytes < 0 {
			p.Bytes = 0
		}
		lista = append(lista, p)
	}
	return lista, linhas.Err()
}

// LePasse devolve um passe.
func LePasse(db *banco.DB, id int64) (Passe, error) {
	l, err := lePasses(db, `WHERE p.id = ?`, id)
	if err != nil {
		return Passe{}, err
	}
	if len(l) == 0 {
		return Passe{}, fmt.Errorf("passe %d: %w", id, ErrNaoEncontrado)
	}
	return l[0], nil
}

func nf(n sql.NullFloat64) *float64 {
	if !n.Valid {
		return nil
	}
	v := n.Float64
	return &v
}

func mudaEstadoRastreio(db *banco.DB, id int64, estado, erro string) error {
	terminado := ""
	if estado == Concluido || estado == Cancelado || estado == Falhou {
		terminado = banco.Agora()
	}
	_, err := db.Exec(`UPDATE rastreios SET estado = ?, erro = ?, atualizado_em = ?,
		terminado_em = CASE WHEN ? = '' THEN terminado_em ELSE ? END WHERE id = ?`,
		estado, erro, banco.Agora(), terminado, terminado, id)
	return err
}

func mudaEstadoPasse(db *banco.DB, id int64, estado, erro string) error {
	_, err := db.Exec(`UPDATE passes SET estado = ?, erro = ?,
		iniciado_em = CASE WHEN iniciado_em = '' AND ? = 'rodando' THEN ? ELSE iniciado_em END,
		terminado_em = CASE WHEN ? IN ('concluido','cancelado','falhou') THEN ? ELSE terminado_em END WHERE id = ?`,
		estado, erro, estado, banco.Agora(), estado, banco.Agora(), id)
	return err
}

// Recupera é para a subida do servidor: um rastreio "rodando" no banco é de um
// processo que morreu no meio. Vira "pausado" (dá para retomar). Devolve
// quantos estavam rodando: se algum, o replay pode ter ficado aberto.
func Recupera(db *banco.DB) (int, error) {
	res, err := db.Exec(`UPDATE rastreios SET estado = ?, erro = 'o servidor foi encerrado no meio do rastreio', atualizado_em = ?
		WHERE estado = ?`, Pausado, banco.Agora(), Rodando)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	db.Exec(`UPDATE passes SET estado = ? WHERE estado = ?`, Pausado, Rodando)
	// Passe de visão rodando: do rastreio (fica pausado com ele) ou avulso
	// (a execução morreu: vira pausado e o próximo pedido continua dele).
	if r, err := db.Exec(`UPDATE passes_visao SET estado = ? WHERE estado = ?`, Pausado, Rodando); err == nil {
		if m, _ := r.RowsAffected(); m > 0 && n == 0 {
			n = m
		}
	}
	return int(n), nil
}

func jsonTexto(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
