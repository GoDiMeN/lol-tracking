// Package ranking é o RANKING do Lane: por região × patch × elo × rota (e,
// numa segunda visão, por campeão: "especialistas de X"), quem são os melhores
// jogadores para o estudo. É nosso e transparente: a nota é uma soma de
// componentes nomeados (formula.go), com os pesos num lugar só
// (parametros.go), e cada posição guarda os números crus, a contribuição de
// cada componente e uma frase. Não raspa nem copia ranking de site nenhum.
//
// Os dados vêm da API da Riot (league-v4 para a liga) e do histórico de cada
// candidato, por uma de duas fontes (fontes.go): o cliente do LoL (LCU, rápido
// e sem gastar a cota da chave) ou a match-v5 da Riot (para rodar num
// servidor sem cliente). Uma ATUALIZAÇÃO (atualizacao.go) é uma execução com
// SSE, pausável e retomável, com cache de contas, ids e partidas já vistos.
package ranking

import (
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"

	"loltracking/server/banco"
)

// Migra cria as tabelas do pacote. Ver docs/arquitetura/dados.md ("ranking").
func Migra(db *banco.DB) error {
	return banco.Executa(db,
		`CREATE TABLE IF NOT EXISTS ranking_atualizacoes (
			id               INTEGER PRIMARY KEY AUTOINCREMENT,
			regiao           TEXT NOT NULL,
			patch            TEXT NOT NULL DEFAULT '',
			elo              TEXT NOT NULL,
			fonte            TEXT NOT NULL,
			teto_candidatos  INTEGER NOT NULL,
			teto_partidas    INTEGER NOT NULL,
			estado           TEXT NOT NULL,
			fase             TEXT NOT NULL DEFAULT 'liga',
			pausa_pedida     INTEGER NOT NULL DEFAULT 0,
			erro             TEXT NOT NULL DEFAULT '',
			paginas_lidas    INTEGER NOT NULL DEFAULT 0,
			entradas_lidas   INTEGER NOT NULL DEFAULT 0,
			elo_inteiro      INTEGER NOT NULL DEFAULT 0,
			lp_max           INTEGER NOT NULL DEFAULT 0,
			inativos         INTEGER NOT NULL DEFAULT 0,
			pedidos_liga     INTEGER NOT NULL DEFAULT 0,
			pedidos_conta    INTEGER NOT NULL DEFAULT 0,
			pedidos_ids      INTEGER NOT NULL DEFAULT 0,
			pedidos_partidas INTEGER NOT NULL DEFAULT 0,
			do_cache         INTEGER NOT NULL DEFAULT 0,
			rankings         INTEGER NOT NULL DEFAULT 0,
			criado_em        TEXT NOT NULL,
			atualizado_em    TEXT NOT NULL,
			terminado_em     TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS ranking_candidatos (
			atualizacao_id INTEGER NOT NULL,
			ordem          INTEGER NOT NULL,
			puuid          TEXT NOT NULL,
			entrada        TEXT NOT NULL,
			pre_ordem      TEXT NOT NULL DEFAULT '',
			estado         TEXT NOT NULL DEFAULT 'pendente',
			motivo         TEXT NOT NULL DEFAULT '',
			nome           TEXT NOT NULL DEFAULT '',
			tag            TEXT NOT NULL DEFAULT '',
			partidas       TEXT NOT NULL DEFAULT '[]',
			truncado       INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (atualizacao_id, puuid)
		)`,
		`CREATE INDEX IF NOT EXISTS ranking_candidatos_ordem ON ranking_candidatos (atualizacao_id, ordem)`,
		`CREATE TABLE IF NOT EXISTS rankings (
			id                 INTEGER PRIMARY KEY AUTOINCREMENT,
			atualizacao_id     INTEGER NOT NULL,
			regiao             TEXT NOT NULL,
			patch              TEXT NOT NULL,
			elo                TEXT NOT NULL,
			tipo               TEXT NOT NULL,
			rota               TEXT NOT NULL DEFAULT '',
			campeao_id         INTEGER NOT NULL DEFAULT 0,
			versao_regra       INTEGER NOT NULL,
			fonte              TEXT NOT NULL,
			amostra            INTEGER NOT NULL,
			candidatos_olhados INTEGER NOT NULL,
			candidatos_lista   INTEGER NOT NULL,
			entradas_lidas     INTEGER NOT NULL,
			paginas_lidas      INTEGER NOT NULL,
			elo_inteiro        INTEGER NOT NULL,
			teto_candidatos    INTEGER NOT NULL,
			teto_partidas      INTEGER NOT NULL,
			lp_max             INTEGER NOT NULL DEFAULT 0,
			fora               TEXT NOT NULL DEFAULT '{}',
			criado_em          TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS rankings_recorte ON rankings (regiao, elo, tipo, rota, campeao_id, patch)`,
		`CREATE TABLE IF NOT EXISTS ranking_posicoes (
			ranking_id       INTEGER NOT NULL,
			modo             TEXT NOT NULL,
			posicao          INTEGER NOT NULL,
			puuid            TEXT NOT NULL,
			nome             TEXT NOT NULL,
			tag              TEXT NOT NULL,
			nota             REAL NOT NULL,
			empatado         INTEGER NOT NULL DEFAULT 0,
			amostra_pequena  INTEGER NOT NULL DEFAULT 0,
			frase            TEXT NOT NULL,
			componentes      TEXT NOT NULL,
			numeros          TEXT NOT NULL,
			lp               INTEGER NOT NULL DEFAULT 0,
			divisao          TEXT NOT NULL DEFAULT '',
			vitorias         INTEGER NOT NULL DEFAULT 0,
			derrotas         INTEGER NOT NULL DEFAULT 0,
			partidas_recorte INTEGER NOT NULL DEFAULT 0,
			vitorias_recorte INTEGER NOT NULL DEFAULT 0,
			fracao_recorte   REAL NOT NULL DEFAULT 0,
			campeao_id       INTEGER NOT NULL DEFAULT 0,
			rota             TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (ranking_id, modo, posicao)
		)`,
		// Cache (vale entre atualizações): a lista de ids de cada jogador, as
		// contas (Riot ID) e as partidas lidas, com os participantes.
		`CREATE TABLE IF NOT EXISTS ranking_historicos (
			regiao     TEXT NOT NULL,
			puuid      TEXT NOT NULL,
			fonte      TEXT NOT NULL,
			ids        TEXT NOT NULL,
			buscado_em TEXT NOT NULL,
			PRIMARY KEY (regiao, puuid, fonte)
		)`,
		`CREATE TABLE IF NOT EXISTS ranking_contas (
			regiao        TEXT NOT NULL,
			puuid         TEXT NOT NULL,
			nome          TEXT NOT NULL,
			tag           TEXT NOT NULL,
			consultado_em TEXT NOT NULL,
			PRIMARY KEY (regiao, puuid)
		)`,
		`CREATE TABLE IF NOT EXISTS ranking_partidas_cache (
			regiao    TEXT NOT NULL,
			match_id  TEXT NOT NULL,
			versao    TEXT NOT NULL,
			patch     TEXT NOT NULL,
			fila      INTEGER NOT NULL,
			criada_ms INTEGER NOT NULL,
			duracao_s INTEGER NOT NULL,
			completa  INTEGER NOT NULL DEFAULT 0,
			lida_em   TEXT NOT NULL,
			PRIMARY KEY (regiao, match_id)
		)`,
		`CREATE TABLE IF NOT EXISTS ranking_participacoes (
			regiao     TEXT NOT NULL,
			match_id   TEXT NOT NULL,
			puuid      TEXT NOT NULL,
			nome       TEXT NOT NULL DEFAULT '',
			tag        TEXT NOT NULL DEFAULT '',
			posicao    TEXT NOT NULL DEFAULT '',
			campeao_id INTEGER NOT NULL DEFAULT 0,
			venceu     INTEGER NOT NULL DEFAULT 0,
			rendicao   INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (regiao, match_id, puuid)
		)`,
	)
}

// Estados e fases de uma atualização.
const (
	Rodando   = "rodando"
	Pausado   = "pausado"
	Concluido = "concluido"
	Cancelado = "cancelado"
	Falhou    = "falhou"

	FaseLiga      = "liga"
	FaseHistorico = "historicos"
	FaseCalculo   = "calculo"
	FaseFim       = "fim"
)

// Estados de um candidato.
const (
	CandPendente = "pendente"
	CandVisto    = "visto"
	CandFora     = "fora"
)

// ErrNaoEncontrado: atualização ou ranking que não existe (404).
var ErrNaoEncontrado = errors.New("não encontrado")

// ErroPedido (400) e ErroConflito (409).
type ErroPedido struct{ Msg string }

func (e *ErroPedido) Error() string { return e.Msg }

type ErroConflito struct{ Msg string }

func (e *ErroConflito) Error() string { return e.Msg }

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// --- Atualização -----------------------------------------------------------------

// Atualizacao é uma execução que (re)calcula os rankings de um elo num patch.
type Atualizacao struct {
	ID             int64  `json:"id"`
	Regiao         string `json:"regiao"`
	Patch          string `json:"patch"`
	Elo            string `json:"elo"`
	Fonte          string `json:"fonte"`
	TetoCandidatos int    `json:"teto_candidatos"`
	TetoPartidas   int    `json:"teto_partidas"`
	Estado         string `json:"estado"`
	Fase           string `json:"fase"`
	PausaPedida    bool   `json:"pausa_pedida"`
	Erro           string `json:"erro"`
	PaginasLidas   int    `json:"paginas_lidas"`
	EntradasLidas  int    `json:"entradas_lidas"`
	EloInteiro     bool   `json:"elo_inteiro"`
	LPMax          int    `json:"lp_max"`
	Inativos       int    `json:"inativos"`
	// Pedidos à Riot gastos até agora, por tipo, e quantas leituras vieram do cache.
	PedidosLiga     int    `json:"pedidos_liga"`
	PedidosConta    int    `json:"pedidos_conta"`
	PedidosIDs      int    `json:"pedidos_ids"`
	PedidosPartidas int    `json:"pedidos_partidas"`
	DoCache         int    `json:"do_cache"`
	Rankings        int    `json:"rankings"`
	CriadoEm        string `json:"criado_em"`
	AtualizadoEm    string `json:"atualizado_em"`
	TerminadoEm     string `json:"terminado_em"`
	// Do resumo (não são colunas).
	Candidatos       int            `json:"candidatos"`
	CandidatosVistos int            `json:"candidatos_vistos"`
	Fora             map[string]int `json:"fora"`
	Progresso        float64        `json:"progresso"`
	Execucao         *IDExec        `json:"execucao"`
}

// IDExec é a referência à execução (SSE).
type IDExec struct {
	ID string `json:"id"`
}

const colunasAtualizacao = `id, regiao, patch, elo, fonte, teto_candidatos, teto_partidas, estado, fase, pausa_pedida, erro,
	paginas_lidas, entradas_lidas, elo_inteiro, lp_max, inativos, pedidos_liga, pedidos_conta, pedidos_ids, pedidos_partidas,
	do_cache, rankings, criado_em, atualizado_em, terminado_em`

func leAtualizacoes(db *banco.DB, filtro string, args ...any) ([]Atualizacao, error) {
	linhas, err := db.Query(`SELECT `+colunasAtualizacao+` FROM ranking_atualizacoes `+filtro, args...)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	var l []Atualizacao
	for linhas.Next() {
		var a Atualizacao
		var pausa, inteiro int
		if err := linhas.Scan(&a.ID, &a.Regiao, &a.Patch, &a.Elo, &a.Fonte, &a.TetoCandidatos, &a.TetoPartidas, &a.Estado, &a.Fase,
			&pausa, &a.Erro, &a.PaginasLidas, &a.EntradasLidas, &inteiro, &a.LPMax, &a.Inativos, &a.PedidosLiga, &a.PedidosConta,
			&a.PedidosIDs, &a.PedidosPartidas, &a.DoCache, &a.Rankings, &a.CriadoEm, &a.AtualizadoEm, &a.TerminadoEm); err != nil {
			return nil, err
		}
		a.PausaPedida, a.EloInteiro = pausa == 1, inteiro == 1
		l = append(l, a)
	}
	if err := linhas.Err(); err != nil {
		return nil, err
	}
	linhas.Close()
	for i := range l {
		if err := resumeAtualizacao(db, &l[i]); err != nil {
			return nil, err
		}
	}
	return l, nil
}

// resumeAtualizacao conta os candidatos e o que ficou de fora.
func resumeAtualizacao(db *banco.DB, a *Atualizacao) error {
	a.Fora = map[string]int{}
	if a.Inativos > 0 {
		a.Fora[ForaInativo] = a.Inativos
	}
	linhas, err := db.Query(`SELECT estado, motivo, COUNT(*) FROM ranking_candidatos WHERE atualizacao_id = ? GROUP BY estado, motivo`, a.ID)
	if err != nil {
		return err
	}
	defer linhas.Close()
	for linhas.Next() {
		var estado, motivo string
		var n int
		if err := linhas.Scan(&estado, &motivo, &n); err != nil {
			return err
		}
		a.Candidatos += n
		if estado != CandPendente {
			a.CandidatosVistos += n
		}
		if estado == CandFora {
			a.Fora[motivo] += n
		}
	}
	switch {
	case a.Estado == Concluido:
		a.Progresso = 1
	case a.Candidatos > 0:
		a.Progresso = float64(a.CandidatosVistos) / float64(a.Candidatos)
	}
	return linhas.Err()
}

// LeAtualizacao devolve uma atualização com o resumo.
func LeAtualizacao(db *banco.DB, id int64) (Atualizacao, error) {
	l, err := leAtualizacoes(db, `WHERE id = ?`, id)
	if err != nil {
		return Atualizacao{}, err
	}
	if len(l) == 0 {
		return Atualizacao{}, ErrNaoEncontrado
	}
	return l[0], nil
}

func criaAtualizacao(db *banco.DB, a Atualizacao) (int64, error) {
	agora := banco.Agora()
	res, err := db.Exec(`INSERT INTO ranking_atualizacoes (regiao, patch, elo, fonte, teto_candidatos, teto_partidas, estado, fase,
		criado_em, atualizado_em) VALUES (?,?,?,?,?,?,?,?,?,?)`, a.Regiao, a.Patch, a.Elo, a.Fonte, a.TetoCandidatos, a.TetoPartidas,
		Pausado, FaseLiga, agora, agora)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func mudaEstado(db *banco.DB, id int64, estado, erro string) error {
	terminado := ""
	if estado == Concluido || estado == Cancelado {
		terminado = banco.Agora()
	}
	_, err := db.Exec(`UPDATE ranking_atualizacoes SET estado = ?, erro = ?, atualizado_em = ?,
		pausa_pedida = CASE WHEN ? = 'rodando' THEN pausa_pedida ELSE 0 END,
		terminado_em = CASE WHEN ? = '' THEN terminado_em ELSE ? END WHERE id = ?`,
		estado, erro, banco.Agora(), estado, terminado, terminado, id)
	return err
}

func mudaFase(db *banco.DB, id int64, fase string) error {
	_, err := db.Exec(`UPDATE ranking_atualizacoes SET fase = ?, atualizado_em = ? WHERE id = ?`, fase, banco.Agora(), id)
	return err
}

func mudaPatch(db *banco.DB, id int64, patch string) error {
	_, err := db.Exec(`UPDATE ranking_atualizacoes SET patch = ? WHERE id = ?`, patch, id)
	return err
}

// somaGasto acumula os pedidos de um passo.
func somaGasto(db *banco.DB, id int64, g Gasto) error {
	_, err := db.Exec(`UPDATE ranking_atualizacoes SET pedidos_liga = pedidos_liga + ?, pedidos_conta = pedidos_conta + ?,
		pedidos_ids = pedidos_ids + ?, pedidos_partidas = pedidos_partidas + ?, do_cache = do_cache + ?, atualizado_em = ? WHERE id = ?`,
		g.Liga, g.Conta, g.IDs, g.Partidas, g.DoCache, banco.Agora(), id)
	return err
}

// Recupera: atualização "rodando" é de um processo que morreu; vira "pausado".
func Recupera(db *banco.DB) error {
	_, err := db.Exec(`UPDATE ranking_atualizacoes SET estado = ?, pausa_pedida = 0,
		erro = 'o servidor foi encerrado no meio da atualização', atualizado_em = ? WHERE estado = ?`, Pausado, banco.Agora(), Rodando)
	return err
}

// --- Candidatos ------------------------------------------------------------------

type candidato struct {
	ordem    int
	puuid    string
	entrada  Entrada
	preOrdem string
	estado   string
	motivo   string
	nome     string
	tag      string
	partidas []PartidaDoJogador
}

func gravaCandidatos(db *banco.DB, id int64, cs []candidato, paginas, entradas, inativos, lpMax int, inteiro bool) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM ranking_candidatos WHERE atualizacao_id = ?`, id); err != nil {
		return err
	}
	for _, c := range cs {
		e, _ := json.Marshal(c.entrada)
		if _, err := tx.Exec(`INSERT INTO ranking_candidatos (atualizacao_id, ordem, puuid, entrada, pre_ordem) VALUES (?,?,?,?,?)`,
			id, c.ordem, c.puuid, string(e), c.preOrdem); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE ranking_atualizacoes SET paginas_lidas = ?, entradas_lidas = ?, inativos = ?, lp_max = ?,
		elo_inteiro = ?, fase = ?, atualizado_em = ? WHERE id = ?`, paginas, entradas, inativos, lpMax, b2i(inteiro), FaseHistorico,
		banco.Agora(), id); err != nil {
		return err
	}
	return tx.Commit()
}

func leCandidatos(db *banco.DB, id int64, filtro string) ([]candidato, error) {
	linhas, err := db.Query(`SELECT ordem, puuid, entrada, pre_ordem, estado, motivo, nome, tag, partidas FROM ranking_candidatos
		WHERE atualizacao_id = ? `+filtro+` ORDER BY ordem`, id)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	var l []candidato
	for linhas.Next() {
		var c candidato
		var e, ps string
		if err := linhas.Scan(&c.ordem, &c.puuid, &e, &c.preOrdem, &c.estado, &c.motivo, &c.nome, &c.tag, &ps); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(e), &c.entrada)
		json.Unmarshal([]byte(ps), &c.partidas)
		l = append(l, c)
	}
	return l, linhas.Err()
}

func marcaCandidato(db *banco.DB, id int64, puuid, estado, motivo string, h Historico) error {
	ps := h.Partidas
	if ps == nil {
		ps = []PartidaDoJogador{}
	}
	b, _ := json.Marshal(ps)
	_, err := db.Exec(`UPDATE ranking_candidatos SET estado = ?, motivo = ?, nome = ?, tag = ?, partidas = ?, truncado = ?
		WHERE atualizacao_id = ? AND puuid = ?`, estado, motivo, h.Nome, h.Tag, string(b), b2i(h.Truncado), id, puuid)
	return err
}

// --- Rankings --------------------------------------------------------------------

// Ranking é o cabeçalho de um ranking.
type Ranking struct {
	ID            int64  `json:"id"`
	AtualizacaoID int64  `json:"atualizacao_id"`
	Regiao        string `json:"regiao"`
	Patch         string `json:"patch"`
	Elo           string `json:"elo"`
	Tipo          string `json:"tipo"`
	Rota          string `json:"rota"`
	CampeaoID     int    `json:"campeao_id"`
	Campeao       string `json:"campeao"`
	VersaoRegra   int    `json:"versao_regra"`
	Fonte         string `json:"fonte"`
	// Amostra: quantos entraram (elegíveis); CandidatosOlhados: quantos tiveram
	// o histórico lido; CandidatosLista: quantos foram escolhidos da liga para
	// ler; EntradasLidas: quantos jogadores a liga lida tinha.
	Amostra           int            `json:"amostra"`
	CandidatosOlhados int            `json:"candidatos_olhados"`
	CandidatosLista   int            `json:"candidatos_lista"`
	EntradasLidas     int            `json:"entradas_lidas"`
	PaginasLidas      int            `json:"paginas_lidas"`
	EloInteiro        bool           `json:"elo_inteiro"`
	TetoCandidatos    int            `json:"teto_candidatos"`
	TetoPartidas      int            `json:"teto_partidas"`
	LPMax             int            `json:"lp_max"`
	Fora              map[string]int `json:"fora"`
	CriadoEm          string         `json:"criado_em"`
	// DescricaoAmostra: a frase do que foi olhado ("amostra de 8 páginas...").
	DescricaoAmostra string `json:"descricao_amostra"`
}

const colunasRanking = `id, atualizacao_id, regiao, patch, elo, tipo, rota, campeao_id, versao_regra, fonte, amostra,
	candidatos_olhados, candidatos_lista, entradas_lidas, paginas_lidas, elo_inteiro, teto_candidatos, teto_partidas, lp_max,
	fora, criado_em`

func leRankings(db *banco.DB, filtro string, args ...any) ([]Ranking, error) {
	linhas, err := db.Query(`SELECT `+colunasRanking+` FROM rankings `+filtro, args...)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	var l []Ranking
	for linhas.Next() {
		var r Ranking
		var inteiro int
		var fora string
		if err := linhas.Scan(&r.ID, &r.AtualizacaoID, &r.Regiao, &r.Patch, &r.Elo, &r.Tipo, &r.Rota, &r.CampeaoID, &r.VersaoRegra,
			&r.Fonte, &r.Amostra, &r.CandidatosOlhados, &r.CandidatosLista, &r.EntradasLidas, &r.PaginasLidas, &inteiro,
			&r.TetoCandidatos, &r.TetoPartidas, &r.LPMax, &fora, &r.CriadoEm); err != nil {
			return nil, err
		}
		r.EloInteiro = inteiro == 1
		r.Fora = map[string]int{}
		json.Unmarshal([]byte(fora), &r.Fora)
		r.DescricaoAmostra = DescricaoAmostra(r)
		l = append(l, r)
	}
	return l, linhas.Err()
}

// LeRanking devolve o cabeçalho pelo id.
func LeRanking(db *banco.DB, id int64) (Ranking, error) {
	l, err := leRankings(db, `WHERE id = ?`, id)
	if err != nil {
		return Ranking{}, err
	}
	if len(l) == 0 {
		return Ranking{}, ErrNaoEncontrado
	}
	return l[0], nil
}

// Ultimo devolve o ranking mais recente do recorte (rota, ou campeão se
// campeaoID > 0) no patch. ErrNaoEncontrado se não há.
func Ultimo(db *banco.DB, regiao, patch, elo, rota string, campeaoID int) (Ranking, error) {
	tipo := TipoRota
	if campeaoID > 0 {
		tipo, rota = TipoCampeao, ""
	}
	l, err := leRankings(db, `WHERE regiao = ? AND patch = ? AND elo = ? AND tipo = ? AND rota = ? AND campeao_id = ?
		ORDER BY id DESC LIMIT 1`, regiao, patch, elo, tipo, rota, campeaoID)
	if err != nil {
		return Ranking{}, err
	}
	if len(l) == 0 {
		return Ranking{}, ErrNaoEncontrado
	}
	return l[0], nil
}

// Posicoes devolve as posições de um ranking num modo, na ordem.
func Posicoes(db *banco.DB, rankingID int64, modo string) ([]Posicao, error) {
	linhas, err := db.Query(`SELECT posicao, puuid, nome, tag, nota, empatado, amostra_pequena, frase, componentes, numeros
		FROM ranking_posicoes WHERE ranking_id = ? AND modo = ? ORDER BY posicao`, rankingID, modo)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	l := []Posicao{}
	for linhas.Next() {
		var p Posicao
		var emp, peq int
		var comps, nums string
		if err := linhas.Scan(&p.Posicao, &p.Puuid, &p.Nome, &p.Tag, &p.Nota, &emp, &peq, &p.Frase, &comps, &nums); err != nil {
			return nil, err
		}
		p.Empatado, p.AmostraPequena = emp == 1, peq == 1
		json.Unmarshal([]byte(comps), &p.Componentes)
		json.Unmarshal([]byte(nums), &p.Numeros)
		l = append(l, p)
	}
	return l, linhas.Err()
}

// rankingCalculado é um recorte pronto para gravar (os dois modos).
type rankingCalculado struct {
	recorte  Recorte
	amostra  int
	fora     map[string]int
	posicoes map[string][]Posicao
}

func gravaRankings(db *banco.DB, a Atualizacao, olhados, lista int, rs []rankingCalculado) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	agora := banco.Agora()
	for _, r := range rs {
		fora, _ := json.Marshal(r.fora)
		res, err := tx.Exec(`INSERT INTO rankings (atualizacao_id, regiao, patch, elo, tipo, rota, campeao_id, versao_regra, fonte,
			amostra, candidatos_olhados, candidatos_lista, entradas_lidas, paginas_lidas, elo_inteiro, teto_candidatos, teto_partidas,
			lp_max, fora, criado_em) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			a.ID, a.Regiao, a.Patch, a.Elo, r.recorte.Tipo, r.recorte.Rota, r.recorte.CampeaoID, VersaoRegra, a.Fonte, r.amostra,
			olhados, lista, a.EntradasLidas, a.PaginasLidas, b2i(a.EloInteiro), a.TetoCandidatos, a.TetoPartidas, a.LPMax,
			string(fora), agora)
		if err != nil {
			return err
		}
		rid, _ := res.LastInsertId()
		for _, modo := range Modos {
			for _, p := range r.posicoes[modo] {
				c, _ := json.Marshal(p.Componentes)
				n, _ := json.Marshal(p.Numeros)
				if _, err := tx.Exec(`INSERT INTO ranking_posicoes (ranking_id, modo, posicao, puuid, nome, tag, nota, empatado,
					amostra_pequena, frase, componentes, numeros, lp, divisao, vitorias, derrotas, partidas_recorte, vitorias_recorte,
					fracao_recorte, campeao_id, rota) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
					rid, modo, p.Posicao, p.Puuid, p.Nome, p.Tag, p.Nota, b2i(p.Empatado), b2i(p.AmostraPequena), p.Frase, string(c),
					string(n), p.Numeros.LP, p.Numeros.Divisao, p.Numeros.Vitorias, p.Numeros.Derrotas, p.Numeros.PartidasRecorte,
					p.Numeros.VitoriasRecorte, p.Numeros.FracaoRecorte, p.Numeros.CampeaoID, p.Numeros.Rota); err != nil {
					return err
				}
			}
		}
	}
	if _, err := tx.Exec(`UPDATE ranking_atualizacoes SET rankings = ?, atualizado_em = ? WHERE id = ?`, len(rs), agora, a.ID); err != nil {
		return err
	}
	return tx.Commit()
}

// Patches: os patches com ranking na região, do mais novo ao mais velho.
func Patches(db *banco.DB, regiao string) ([]string, error) {
	linhas, err := db.Query(`SELECT DISTINCT patch FROM rankings WHERE regiao = ?`, regiao)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	l := []string{}
	for linhas.Next() {
		var p string
		if err := linhas.Scan(&p); err != nil {
			return nil, err
		}
		l = append(l, p)
	}
	sort.SliceStable(l, func(a, b int) bool { return ComparaPatch(l[a], l[b]) > 0 })
	return l, linhas.Err()
}

// Recortes: os rankings mais recentes de cada recorte num patch e elo (a
// lista da tela: as rotas e os campeões com ranking).
func Recortes(db *banco.DB, regiao, patch, elo string) ([]Ranking, error) {
	return leRankings(db, `WHERE id IN (SELECT MAX(id) FROM rankings WHERE regiao = ? AND patch = ? AND elo = ?
		GROUP BY tipo, rota, campeao_id) ORDER BY tipo DESC, rota, amostra DESC, campeao_id`, regiao, patch, elo)
}

// ComparaPatch: >0 se a é mais novo que b ("16.10" > "16.9"), 0 se iguais.
func ComparaPatch(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < max(len(pa), len(pb)); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			return x - y
		}
	}
	return 0
}
