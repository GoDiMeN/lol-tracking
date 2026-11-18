// Package aprendizado monta sozinho um conjunto de partidas de referência por
// elo e rastreia cada uma, para depois (fora deste pacote) aprender o que os
// jogadores de cada elo fazem.
//
// Uma COLETA tem dois passos. Na descoberta, para cada elo, os melhores
// jogadores vêm da API da Riot (league-v4; o ranking nunca vem do LCU), o
// Riot ID de cada um da account-v1, e o resto do caminho que o app já usa: o
// jogador e o histórico pelo LCU, achado pelo Riot ID (o puuid da Riot não é
// usado no LCU). No rastreio, uma partida por vez: baixa o replay se preciso e
// rastreia pelo motor do pacote rastreio (com todas as regras de segurança
// dele). Pausar é gracioso: o passe em andamento termina e fecha normalmente.
package aprendizado

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
		`CREATE TABLE IF NOT EXISTS aprendizado_coletas (
			id                   INTEGER PRIMARY KEY AUTOINCREMENT,
			regiao               TEXT NOT NULL,
			elos                 TEXT NOT NULL,
			partidas_por_jogador INTEGER NOT NULL,
			quem                 TEXT NOT NULL,
			velocidade           REAL NOT NULL,
			estado               TEXT NOT NULL,
			fase                 TEXT NOT NULL DEFAULT 'descoberta',
			pausa_pedida         INTEGER NOT NULL DEFAULT 0,
			erro                 TEXT NOT NULL DEFAULT '',
			criado_em            TEXT NOT NULL,
			atualizado_em        TEXT NOT NULL,
			terminado_em         TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS aprendizado_elos (
			coleta_id         INTEGER NOT NULL,
			elo               TEXT NOT NULL,
			ordem             INTEGER NOT NULL,
			estado            TEXT NOT NULL DEFAULT 'pendente',
			candidatos_vistos INTEGER NOT NULL DEFAULT 0,
			descartes         TEXT NOT NULL DEFAULT '{}',
			aviso             TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (coleta_id, elo)
		)`,
		`CREATE TABLE IF NOT EXISTS aprendizado_jogadores (
			id             INTEGER PRIMARY KEY AUTOINCREMENT,
			coleta_id      INTEGER NOT NULL,
			regiao         TEXT NOT NULL,
			elo            TEXT NOT NULL,
			posicao        INTEGER NOT NULL,
			lp             INTEGER NOT NULL,
			puuid_riot     TEXT NOT NULL,
			nome           TEXT NOT NULL,
			tag            TEXT NOT NULL,
			puuid_lcu      TEXT NOT NULL,
			campeao_id     INTEGER NOT NULL DEFAULT 0,
			campeao        TEXT NOT NULL DEFAULT '',
			fracao_campeao REAL NOT NULL DEFAULT 0,
			partidas_solo  INTEGER NOT NULL DEFAULT 0,
			mono           INTEGER NOT NULL DEFAULT 0,
			divisao        TEXT NOT NULL DEFAULT '',
			vitorias       INTEGER NOT NULL DEFAULT 0,
			derrotas       INTEGER NOT NULL DEFAULT 0,
			taxa_vitoria   REAL NOT NULL DEFAULT 0,
			fresh_blood    INTEGER NOT NULL DEFAULT 0,
			veteran        INTEGER NOT NULL DEFAULT 0,
			hot_streak     INTEGER NOT NULL DEFAULT 0,
			pontuacao_subida REAL NOT NULL DEFAULT 0,
			estado         TEXT NOT NULL,
			criado_em      TEXT NOT NULL,
			UNIQUE (coleta_id, puuid_riot)
		)`,
		`CREATE INDEX IF NOT EXISTS aprendizado_jogadores_coleta ON aprendizado_jogadores (coleta_id, elo, posicao)`,
		`CREATE TABLE IF NOT EXISTS aprendizado_partidas (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			coleta_id       INTEGER NOT NULL,
			jogador_id      INTEGER NOT NULL,
			regiao          TEXT NOT NULL,
			elo             TEXT NOT NULL,
			game_id         INTEGER NOT NULL,
			participante_id INTEGER NOT NULL,
			campeao_id      INTEGER NOT NULL DEFAULT 0,
			campeao         TEXT NOT NULL DEFAULT '',
			abates          INTEGER NOT NULL DEFAULT 0,
			mortes          INTEGER NOT NULL DEFAULT 0,
			assistencias    INTEGER NOT NULL DEFAULT 0,
			duracao_s       INTEGER NOT NULL DEFAULT 0,
			pontuacao       REAL NOT NULL DEFAULT 0,
			ordem           INTEGER NOT NULL DEFAULT 0,
			estado          TEXT NOT NULL DEFAULT 'pendente',
			rastreio_id     INTEGER,
			erro            TEXT NOT NULL DEFAULT '',
			atualizado_em   TEXT NOT NULL,
			UNIQUE (coleta_id, game_id, participante_id)
		)`,
		`CREATE INDEX IF NOT EXISTS aprendizado_partidas_coleta ON aprendizado_partidas (coleta_id, estado)`,
	); err != nil {
		return err
	}
	// Colunas que vieram depois (o banco de quem já coletou não as tem).
	novas := []struct{ tabela, coluna, def string }{
		// so_descobrir: só acha os jogadores, não rastreia nada.
		{"aprendizado_coletas", "so_descobrir", "INTEGER NOT NULL DEFAULT 0"},
		// estudo_id > 0: a "coleta" que rastreia as partidas de um estudo (sem descoberta).
		{"aprendizado_coletas", "estudo_id", "INTEGER NOT NULL DEFAULT 0"},
		// funcao: só entram jogadores com esta função principal ("" = qualquer).
		{"aprendizado_coletas", "funcao", "TEXT NOT NULL DEFAULT ''"},
		{"aprendizado_jogadores", "funcao", "TEXT NOT NULL DEFAULT ''"},
		{"aprendizado_jogadores", "fracao_funcao", "REAL NOT NULL DEFAULT 0"},
		{"aprendizado_jogadores", "vitorias_rastreaveis", "INTEGER NOT NULL DEFAULT 0"},
		{"aprendizado_jogadores", "derrotas_rastreaveis", "INTEGER NOT NULL DEFAULT 0"},
	}
	for _, n := range novas {
		if err := banco.AdicionaColuna(db, n.tabela, n.coluna, n.def); err != nil {
			return err
		}
	}
	return nil
}

// Estados da coleta.
const (
	Rodando   = "rodando"
	Pausado   = "pausado"
	Concluido = "concluido"
	Cancelado = "cancelado"
	Falhou    = "falhou"
)

// Fases da coleta.
const (
	FaseDescoberta = "descoberta"
	FaseRastreio   = "rastreio"
	FaseFim        = "fim"
)

// Estados de uma partida da coleta.
const (
	PartidaPendente   = "pendente"
	PartidaBaixando   = "baixando"
	PartidaRastreando = "rastreando"
	PartidaPausada    = "pausada"
	PartidaConcluida  = "concluida"
	PartidaErro       = "erro"
	PartidaCancelada  = "cancelada"
)

// Estados de um elo na descoberta e de um jogador.
const (
	EloPendente    = "pendente"
	EloDescobrindo = "descobrindo"
	EloPronto      = "pronto"

	JogadorComPartidas = "com_partidas"
	JogadorSemPartida  = "sem_partida"
)

// Quem rastrear em cada partida.
const (
	QuemJogador = "jogador"
	QuemTodos   = "todos"
)

// Elos é a ordem dos elos (nada abaixo de Prata) e o nome de cada um na tela.
var Elos = []string{"SILVER", "GOLD", "PLATINUM", "EMERALD", "DIAMOND", "MASTER", "GRANDMASTER", "CHALLENGER"}

// NomeElo dá o nome em português.
var NomeElo = map[string]string{
	"SILVER": "Prata", "GOLD": "Ouro", "PLATINUM": "Platina", "EMERALD": "Esmeralda", "DIAMOND": "Diamante",
	"MASTER": "Mestre", "GRANDMASTER": "Grão-Mestre", "CHALLENGER": "Desafiante",
}

// Regras da descoberta e da escolha (contrato-http.md, "Aprendizado por elo").
const (
	JogadoresPorElo     = 10
	MaxCandidatosPorElo = 60
	MonoMinPartidas     = 10
	MonoMinFracao       = 0.6
	DuracaoMinimaS      = 15 * 60
	FilaSolo            = 420
	MaxPartidasAnalisar = 6
	SegundosPorPasseRef = 30 * 60 // duração média de partida para estimar antes de descobrir
	SobraPorPasseS      = 15      // abrir o replay, travar a câmera, fechar
	BytesPasseRef       = 53 * 1024
	BytesPartidaRef     = 45 * 1024
)

// Coleta é uma coleta com o resumo do progresso.
type Coleta struct {
	ID                  int64    `json:"id"`
	Regiao              string   `json:"regiao"`
	RegiaoNome          string   `json:"regiao_nome"`
	Elos                []string `json:"elos"`
	PartidasPorJogador  int      `json:"partidas_por_jogador"`
	Quem                string   `json:"quem"`
	Velocidade          float64  `json:"velocidade"`
	SoDescobrir         bool     `json:"so_descobrir"`
	EstudoID            int64    `json:"estudo_id"`
	Funcao              string   `json:"funcao"`
	Estado              string   `json:"estado"`
	Fase                string   `json:"fase"`
	PausaPedida         bool     `json:"pausa_pedida"`
	Erro                string   `json:"erro"`
	CriadoEm            string   `json:"criado_em"`
	AtualizadoEm        string   `json:"atualizado_em"`
	TerminadoEm         string   `json:"terminado_em"`
	Jogadores           int      `json:"jogadores"`
	JogadoresSemPartida int      `json:"jogadores_sem_partida"`
	PartidasTotal       int      `json:"partidas_total"`
	PartidasConcluidas  int      `json:"partidas_concluidas"`
	PartidasComErro     int      `json:"partidas_com_erro"`
	PassesTotal         int      `json:"passes_total"`
	PassesConcluidos    int      `json:"passes_concluidos"`
	Progresso           float64  `json:"progresso"`
	EstimativaS         float64  `json:"estimativa_s"`
	PartidaAtual        *int64   `json:"partida_atual"`
	RastreioAtual       *int64   `json:"rastreio_atual"`
	Execucao            *IDExec  `json:"execucao"`
}

// IDExec é a referência à execução (SSE).
type IDExec struct {
	ID string `json:"id"`
}

// EloDaColeta é o andamento de um elo.
type EloDaColeta struct {
	Elo                string         `json:"elo"`
	Nome               string         `json:"nome"`
	ExigeMono          bool           `json:"exige_mono"`
	Estado             string         `json:"estado"`
	CandidatosVistos   int            `json:"candidatos_vistos"`
	Jogadores          int            `json:"jogadores"`
	Descartes          map[string]int `json:"descartes"`
	Aviso              string         `json:"aviso"`
	PartidasTotal      int            `json:"partidas_total"`
	PartidasConcluidas int            `json:"partidas_concluidas"`
	ordem              int
}

// JogadorDaColeta é um jogador escolhido para a coleta.
type JogadorDaColeta struct {
	ID            int64   `json:"id"`
	Regiao        string  `json:"regiao"`
	Elo           string  `json:"elo"`
	Posicao       int     `json:"posicao"`
	LP            int     `json:"lp"`
	Nome          string  `json:"nome"`
	Tag           string  `json:"tag"`
	RiotID        string  `json:"riot_id"`
	CampeaoID     int     `json:"campeao_id"`
	Campeao       string  `json:"campeao"`
	FracaoCampeao float64 `json:"fracao_campeao"`
	PartidasSolo  int     `json:"partidas_solo"`
	Mono          bool    `json:"mono"`
	Estado        string  `json:"estado"`
	// Os números da liga que justificaram a escolha (aprendizado/subida.go).
	Divisao         string  `json:"divisao"`
	Vitorias        int     `json:"vitorias"`
	Derrotas        int     `json:"derrotas"`
	TaxaVitoria     float64 `json:"taxa_vitoria"`
	FreshBlood      bool    `json:"fresh_blood"`
	Veteran         bool    `json:"veteran"`
	HotStreak       bool    `json:"hot_streak"`
	PontuacaoSubida float64 `json:"pontuacao_subida"`
	Motivo          string  `json:"motivo"`
	// Funcao é a posição mais jogada nas ranqueadas solo do histórico (TOP,
	// JUNGLE, MIDDLE, BOTTOM, UTILITY; "" se não deu para saber) e a fração.
	Funcao       string  `json:"funcao"`
	FracaoFuncao float64 `json:"fracao_funcao"`
	// Partidas do histórico que dá para rastrear (ranqueada solo do patch
	// instalado, 15 min ou mais, sem rendição antecipada), na função principal.
	VitoriasRastreaveis int `json:"vitorias_rastreaveis"`
	DerrotasRastreaveis int `json:"derrotas_rastreaveis"`
	puuidRiot           string
	puuidLCU            string
}

// PartidaDaColeta é uma partida escolhida para rastrear.
type PartidaDaColeta struct {
	ID               int64   `json:"id"`
	Regiao           string  `json:"regiao"`
	JogadorID        int64   `json:"jogador_id"`
	Elo              string  `json:"elo"`
	GameID           int64   `json:"game_id"`
	ParticipanteID   int     `json:"participante_id"`
	CampeaoID        int     `json:"campeao_id"`
	Campeao          string  `json:"campeao"`
	Abates           int     `json:"abates"`
	Mortes           int     `json:"mortes"`
	Assistencias     int     `json:"assistencias"`
	DuracaoS         int     `json:"duracao_s"`
	Pontuacao        float64 `json:"pontuacao"`
	Estado           string  `json:"estado"`
	RastreioID       *int64  `json:"rastreio_id"`
	PassesTotal      int     `json:"passes_total"`
	PassesConcluidos int     `json:"passes_concluidos"`
	Erro             string  `json:"erro"`
	ordem            int
	progresso        float64
	// posicao e venceu: da partida inteira; só a escolha do estudo usa.
	posicao string
	venceu  bool
}

// ErrNaoEncontrada: coleta que não existe (404).
var ErrNaoEncontrada = errors.New("coleta não encontrada")

// ErroPedido é pedido malformado (400).
type ErroPedido struct{ Msg string }

func (e *ErroPedido) Error() string { return e.Msg }

// ErroConflito é ação que não cabe agora (409).
type ErroConflito struct{ Msg string }

func (e *ErroConflito) Error() string { return e.Msg }

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// --- Gravação -------------------------------------------------------------------

// novaColeta é o que criaColeta grava.
type novaColeta struct {
	regiao      string
	elos        []string
	ppj         int
	quem        string
	vel         float64
	soDescobrir bool
	funcao      string
	// estudoID > 0: a coleta do estudo começa direto no rastreio, com estas partidas.
	estudoID int64
	partidas []PartidaDaColeta
}

func criaColeta(db *banco.DB, n novaColeta) (int64, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	agora := banco.Agora()
	if n.elos == nil {
		n.elos = []string{}
	}
	lista, _ := json.Marshal(n.elos)
	fase := FaseDescoberta
	if n.estudoID > 0 {
		fase = FaseRastreio
	}
	res, err := tx.Exec(`INSERT INTO aprendizado_coletas (regiao, elos, partidas_por_jogador, quem, velocidade, estado, fase,
		so_descobrir, estudo_id, funcao, criado_em, atualizado_em) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.regiao, string(lista), n.ppj, n.quem, n.vel, Pausado, fase, b2i(n.soDescobrir), n.estudoID, n.funcao, agora, agora)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	for i, e := range n.elos {
		if _, err := tx.Exec(`INSERT INTO aprendizado_elos (coleta_id, elo, ordem) VALUES (?, ?, ?)`, id, e, i); err != nil {
			return 0, err
		}
	}
	// As partidas do estudo não têm jogador da coleta nem elo (jogador_id 0, elo
	// vazio): o jogador é o do estudo.
	for i, p := range n.partidas {
		if _, err := tx.Exec(`INSERT INTO aprendizado_partidas (coleta_id, jogador_id, regiao, elo, game_id, participante_id,
			campeao_id, campeao, abates, mortes, assistencias, duracao_s, pontuacao, ordem, estado, atualizado_em)
			VALUES (?,0,?,'',?,?,?,?,?,?,?,?,0,?,?,?)`,
			id, n.regiao, p.GameID, p.ParticipanteID, p.CampeaoID, p.Campeao, p.Abates, p.Mortes, p.Assistencias, p.DuracaoS,
			i, PartidaPendente, agora); err != nil {
			return 0, err
		}
	}
	return id, tx.Commit()
}

func mudaEstado(db *banco.DB, id int64, estado, erro string) error {
	terminado := ""
	if estado == Concluido || estado == Cancelado {
		terminado = banco.Agora()
	}
	_, err := db.Exec(`UPDATE aprendizado_coletas SET estado = ?, erro = ?, atualizado_em = ?,
		pausa_pedida = CASE WHEN ? = 'rodando' THEN pausa_pedida ELSE 0 END,
		terminado_em = CASE WHEN ? = '' THEN terminado_em ELSE ? END WHERE id = ?`,
		estado, erro, banco.Agora(), estado, terminado, terminado, id)
	return err
}

func mudaFase(db *banco.DB, id int64, fase string) error {
	_, err := db.Exec(`UPDATE aprendizado_coletas SET fase = ?, atualizado_em = ? WHERE id = ?`, fase, banco.Agora(), id)
	return err
}

func marcaPausaPedida(db *banco.DB, id int64) error {
	_, err := db.Exec(`UPDATE aprendizado_coletas SET pausa_pedida = 1, atualizado_em = ? WHERE id = ?`, banco.Agora(), id)
	return err
}

func mudaElo(db *banco.DB, coletaID int64, elo, estado string, vistos int, descartes map[string]int, aviso string) error {
	d, _ := json.Marshal(descartes)
	_, err := db.Exec(`UPDATE aprendizado_elos SET estado = ?, candidatos_vistos = ?, descartes = ?, aviso = ?
		WHERE coleta_id = ? AND elo = ?`, estado, vistos, string(d), aviso, coletaID, elo)
	return err
}

func mudaPartida(db *banco.DB, id int64, estado, erro string) error {
	_, err := db.Exec(`UPDATE aprendizado_partidas SET estado = ?, erro = ?, atualizado_em = ? WHERE id = ?`,
		estado, erro, banco.Agora(), id)
	return err
}

func ligaRastreio(db *banco.DB, id, rastreioID int64) error {
	_, err := db.Exec(`UPDATE aprendizado_partidas SET rastreio_id = ?, estado = ?, erro = '', atualizado_em = ? WHERE id = ?`,
		rastreioID, PartidaRastreando, banco.Agora(), id)
	return err
}

// gravaJogador grava o jogador e as partidas escolhidas numa transação. Partida
// que já está na coleta (outro jogador da mesma partida) não entra de novo.
func gravaJogador(db *banco.DB, coletaID int64, j JogadorDaColeta, ps []PartidaDaColeta, quem string) (int64, int, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	agora := banco.Agora()
	res, err := tx.Exec(`INSERT INTO aprendizado_jogadores (coleta_id, regiao, elo, posicao, lp, puuid_riot, nome, tag, puuid_lcu,
		campeao_id, campeao, fracao_campeao, partidas_solo, mono, divisao, vitorias, derrotas, taxa_vitoria, fresh_blood, veteran,
		hot_streak, pontuacao_subida, estado, criado_em, funcao, fracao_funcao, vitorias_rastreaveis, derrotas_rastreaveis)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		coletaID, j.Regiao, j.Elo, j.Posicao, j.LP, j.puuidRiot, j.Nome, j.Tag, j.puuidLCU, j.CampeaoID, j.Campeao,
		j.FracaoCampeao, j.PartidasSolo, b2i(j.Mono), j.Divisao, j.Vitorias, j.Derrotas, j.TaxaVitoria, b2i(j.FreshBlood),
		b2i(j.Veteran), b2i(j.HotStreak), j.PontuacaoSubida, j.Estado, agora, j.Funcao, j.FracaoFuncao,
		j.VitoriasRastreaveis, j.DerrotasRastreaveis)
	if err != nil {
		return 0, 0, fmt.Errorf("gravar jogador %s#%s: %w", j.Nome, j.Tag, err)
	}
	jid, _ := res.LastInsertId()
	n := 0
	for i, p := range ps {
		// Com "todos", a partida inteira já é rastreada por quem chegou antes.
		filtro := `SELECT COUNT(*) FROM aprendizado_partidas WHERE coleta_id = ? AND game_id = ? AND (participante_id = ? OR ? = 'todos')`
		var ja int
		if err := tx.QueryRow(filtro, coletaID, p.GameID, p.ParticipanteID, quem).Scan(&ja); err != nil {
			return 0, 0, err
		}
		if ja > 0 {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO aprendizado_partidas (coleta_id, jogador_id, regiao, elo, game_id, participante_id,
			campeao_id, campeao, abates, mortes, assistencias, duracao_s, pontuacao, ordem, estado, atualizado_em)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			coletaID, jid, j.Regiao, j.Elo, p.GameID, p.ParticipanteID, p.CampeaoID, p.Campeao, p.Abates, p.Mortes,
			p.Assistencias, p.DuracaoS, p.Pontuacao, i, PartidaPendente, agora); err != nil {
			return 0, 0, err
		}
		n++
	}
	// Só descobrir: nenhuma partida é escolhida, e o jogador vale assim mesmo.
	if n == 0 && j.Estado == JogadorComPartidas && ps != nil {
		if _, err := tx.Exec(`UPDATE aprendizado_jogadores SET estado = ? WHERE id = ?`, JogadorSemPartida, jid); err != nil {
			return 0, 0, err
		}
	}
	return jid, n, tx.Commit()
}

// Recupera é para a subida do servidor: coleta "rodando" é de um processo que
// morreu no meio. Vira "pausado" (retomável); a partida que estava baixando
// volta a pendente e a que estava rastreando fica pausada (o rastreio dela
// também foi pausado pelo pacote rastreio).
func Recupera(db *banco.DB) error {
	if _, err := db.Exec(`UPDATE aprendizado_coletas SET estado = ?, pausa_pedida = 0,
		erro = 'o servidor foi encerrado no meio da coleta', atualizado_em = ? WHERE estado = ?`, Pausado, banco.Agora(), Rodando); err != nil {
		return err
	}
	if _, err := db.Exec(`UPDATE aprendizado_partidas SET estado = ? WHERE estado = ?`, PartidaPendente, PartidaBaixando); err != nil {
		return err
	}
	_, err := db.Exec(`UPDATE aprendizado_partidas SET estado = ? WHERE estado = ?`, PartidaPausada, PartidaRastreando)
	return err
}

// --- Leitura --------------------------------------------------------------------

const colunasColeta = `id, regiao, elos, partidas_por_jogador, quem, velocidade, estado, fase, pausa_pedida, erro,
	criado_em, atualizado_em, terminado_em, so_descobrir, estudo_id, funcao`

func leColetas(db *banco.DB, filtro string, args ...any) ([]Coleta, error) {
	linhas, err := db.Query(`SELECT `+colunasColeta+` FROM aprendizado_coletas `+filtro, args...)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	var l []Coleta
	for linhas.Next() {
		var c Coleta
		var elos string
		var pausa, so int
		if err := linhas.Scan(&c.ID, &c.Regiao, &elos, &c.PartidasPorJogador, &c.Quem, &c.Velocidade, &c.Estado, &c.Fase,
			&pausa, &c.Erro, &c.CriadoEm, &c.AtualizadoEm, &c.TerminadoEm, &so, &c.EstudoID, &c.Funcao); err != nil {
			return nil, err
		}
		c.PausaPedida, c.SoDescobrir = pausa == 1, so == 1
		json.Unmarshal([]byte(elos), &c.Elos)
		l = append(l, c)
	}
	return l, linhas.Err()
}

// LeColeta devolve a coleta (sem o resumo do progresso).
func LeColeta(db *banco.DB, id int64) (Coleta, error) {
	l, err := leColetas(db, `WHERE id = ?`, id)
	if err != nil {
		return Coleta{}, err
	}
	if len(l) == 0 {
		return Coleta{}, ErrNaoEncontrada
	}
	return l[0], nil
}

func leElos(db *banco.DB, coletaID int64) ([]EloDaColeta, error) {
	linhas, err := db.Query(`SELECT elo, ordem, estado, candidatos_vistos, descartes, aviso,
		(SELECT COUNT(*) FROM aprendizado_jogadores j WHERE j.coleta_id = e.coleta_id AND j.elo = e.elo),
		(SELECT COUNT(*) FROM aprendizado_partidas p WHERE p.coleta_id = e.coleta_id AND p.elo = e.elo),
		(SELECT COUNT(*) FROM aprendizado_partidas p WHERE p.coleta_id = e.coleta_id AND p.elo = e.elo AND p.estado = 'concluida')
		FROM aprendizado_elos e WHERE coleta_id = ? ORDER BY ordem`, coletaID)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	var l []EloDaColeta
	for linhas.Next() {
		var e EloDaColeta
		var desc string
		if err := linhas.Scan(&e.Elo, &e.ordem, &e.Estado, &e.CandidatosVistos, &desc, &e.Aviso, &e.Jogadores,
			&e.PartidasTotal, &e.PartidasConcluidas); err != nil {
			return nil, err
		}
		e.Nome = NomeElo[e.Elo]
		e.ExigeMono = exigeMono(e.Elo)
		e.Descartes = map[string]int{"nao_achado": 0, "nao_mono": 0, "sem_historico": 0, DescInativo: 0, DescVeterano: 0,
			DescPoucasPartidas: 0, DescTravado: 0, DescSemPartida: 0, DescOutraFuncao: 0}
		json.Unmarshal([]byte(desc), &e.Descartes)
		l = append(l, e)
	}
	return l, linhas.Err()
}

const colunasJogador = `id, regiao, elo, posicao, lp, nome, tag, campeao_id, campeao, fracao_campeao, partidas_solo, mono, estado,
	puuid_riot, puuid_lcu, divisao, vitorias, derrotas, taxa_vitoria, fresh_blood, veteran, hot_streak, pontuacao_subida,
	funcao, fracao_funcao, vitorias_rastreaveis, derrotas_rastreaveis`

func leJogadores(db *banco.DB, filtro string, args ...any) ([]JogadorDaColeta, error) {
	linhas, err := db.Query(`SELECT `+colunasJogador+` FROM aprendizado_jogadores `+filtro, args...)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	var l []JogadorDaColeta
	for linhas.Next() {
		var j JogadorDaColeta
		var mono, fresh, vet, hot int
		if err := linhas.Scan(&j.ID, &j.Regiao, &j.Elo, &j.Posicao, &j.LP, &j.Nome, &j.Tag, &j.CampeaoID, &j.Campeao,
			&j.FracaoCampeao, &j.PartidasSolo, &mono, &j.Estado, &j.puuidRiot, &j.puuidLCU, &j.Divisao, &j.Vitorias,
			&j.Derrotas, &j.TaxaVitoria, &fresh, &vet, &hot, &j.PontuacaoSubida, &j.Funcao, &j.FracaoFuncao,
			&j.VitoriasRastreaveis, &j.DerrotasRastreaveis); err != nil {
			return nil, err
		}
		j.Mono, j.FreshBlood, j.Veteran, j.HotStreak = mono == 1, fresh == 1, vet == 1, hot == 1
		j.RiotID = j.Nome + "#" + j.Tag
		j.Motivo = Motivo(j)
		l = append(l, j)
	}
	return l, linhas.Err()
}

const colunasPartida = `p.id, p.regiao, p.jogador_id, p.elo, p.game_id, p.participante_id, p.campeao_id, p.campeao, p.abates,
	p.mortes, p.assistencias, p.duracao_s, p.pontuacao, p.ordem, p.estado, p.rastreio_id, p.erro`

func lePartidas(db *banco.DB, filtro string, args ...any) ([]PartidaDaColeta, error) {
	// LEFT JOIN: as partidas da coleta de um estudo não têm elo nem jogador da coleta.
	linhas, err := db.Query(`SELECT `+colunasPartida+` FROM aprendizado_partidas p
		LEFT JOIN aprendizado_elos e ON e.coleta_id = p.coleta_id AND e.elo = p.elo
		LEFT JOIN aprendizado_jogadores j ON j.id = p.jogador_id `+filtro, args...)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	var l []PartidaDaColeta
	for linhas.Next() {
		var p PartidaDaColeta
		var rid sql.NullInt64
		if err := linhas.Scan(&p.ID, &p.Regiao, &p.JogadorID, &p.Elo, &p.GameID, &p.ParticipanteID, &p.CampeaoID, &p.Campeao,
			&p.Abates, &p.Mortes, &p.Assistencias, &p.DuracaoS, &p.Pontuacao, &p.ordem, &p.Estado, &rid, &p.Erro); err != nil {
			return nil, err
		}
		if rid.Valid {
			v := rid.Int64
			p.RastreioID = &v
		}
		l = append(l, p)
	}
	return l, linhas.Err()
}

// ordemDasPartidas: elo, posição do jogador no ranking, ordem da partida.
const ordemDasPartidas = ` ORDER BY e.ordem, j.posicao, p.ordem, p.id`

func exigeMono(elo string) bool {
	return elo == "MASTER" || elo == "GRANDMASTER" || elo == "CHALLENGER"
}
