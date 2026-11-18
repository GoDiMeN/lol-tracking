package analise

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"loltracking/server/banco"
)

// Leitura da base de fatos: só as três tabelas (resumos_partida,
// resumos_jogador, momentos_chave). Nada aqui abre arquivo de partida.

const colunasMomento = `game_id, n, t_ms, fim_ms, tipo, subtipo, time, participante_id, autor, funcao, campeao, envolvidos,
	atividade, atividade_previa, zona, rota, duracao_ms, saldo_antes, saldo_depois, custo_azul, custo_vermelho, impacto, chave,
	estimado, tags, frase, dados`

func leMomento(s interface{ Scan(...any) error }) (int64, MomentoChave, error) {
	var m MomentoChave
	var g int64
	var env, tags, dados string
	var sa, sd sql.NullInt64
	err := s.Scan(&g, &m.N, &m.TMs, &m.FimMs, &m.Tipo, &m.Subtipo, &m.Time, &m.ParticipanteID, &m.Autor, &m.Funcao, &m.Campeao,
		&env, &m.Atividade, &m.AtividadePrevia, &m.Zona, &m.Rota, &m.DuracaoMs, &sa, &sd, &m.CustoAzul, &m.CustoVermelho,
		&m.Impacto, &m.Chave, &m.Estimado, &tags, &m.Frase, &dados)
	if err != nil {
		return 0, m, err
	}
	if sa.Valid {
		m.SaldoAntes = ip(int(sa.Int64))
	}
	if sd.Valid {
		m.SaldoDepois = ip(int(sd.Int64))
	}
	m.Envolvidos = []int{}
	json.Unmarshal([]byte(env), &m.Envolvidos)
	m.Tags = []string{}
	for _, t := range strings.Split(strings.Trim(tags, ","), ",") {
		if t != "" {
			m.Tags = append(m.Tags, t)
		}
	}
	m.Dados = jsonMapa(dados)
	return g, m, nil
}

const colunasJogador = `game_id, participante_id, puuid, nome, campeao, funcao, time, venceu, rastreado, elo, abates, mortes,
	assistencias, kda, ouro_10, ouro_15, ouro_fim, cs_10, cs_15, cs_fim, adversario, dif_ouro_10, dif_ouro_15, dif_cs_10, dif_cs_15,
	mortes_rotas, mortes_meio, mortes_final, mortes_caras, objetivos_participou, objetivos_time, sentinelas, sentinelas_controle,
	nota, posicao_nota, etiquetas, dados`

func leJogador(s interface{ Scan(...any) error }) (int64, ResumoJogador, error) {
	var j ResumoJogador
	var g int64
	var etq, dados string
	var o10, o15, ofim, c10, c15, cfim, do10, do15, dc10, dc15 sql.NullInt64
	err := s.Scan(&g, &j.ParticipanteID, &j.Puuid, &j.Nome, &j.Campeao, &j.Funcao, &j.Time, &j.Venceu, &j.Rastreado, &j.Elo,
		&j.Abates, &j.Mortes, &j.Assistencias, &j.KDA, &o10, &o15, &ofim, &c10, &c15, &cfim, &j.Adversario, &do10, &do15, &dc10, &dc15,
		&j.MortesRotas, &j.MortesMeio, &j.MortesFinal, &j.MortesCaras, &j.ObjetivosParticipou, &j.ObjetivosTime, &j.Sentinelas,
		&j.SentinelasControle, &j.Nota, &j.PosicaoNota, &etq, &dados)
	if err != nil {
		return 0, j, err
	}
	n := func(v sql.NullInt64) *int {
		if !v.Valid {
			return nil
		}
		return ip(int(v.Int64))
	}
	j.Ouro10, j.Ouro15, j.OuroFim, j.CS10, j.CS15, j.CSFim = n(o10), n(o15), n(ofim), n(c10), n(c15), n(cfim)
	j.DifOuro10, j.DifOuro15, j.DifCS10, j.DifCS15 = n(do10), n(do15), n(dc10), n(dc15)
	j.Etiquetas = map[string]map[string]int{}
	json.Unmarshal([]byte(etq), &j.Etiquetas)
	var d struct {
		Itens       []ItemComprado   `json:"itens"`
		Build       []EvidenciaBuild `json:"build"`
		Componentes []ComponenteNota `json:"componentes"`
		Contexto    ContextoJogador  `json:"contexto"`
	}
	json.Unmarshal([]byte(dados), &d)
	j.Itens, j.Build, j.Componentes, j.Contexto = d.Itens, d.Build, d.Componentes, d.Contexto
	if j.Itens == nil {
		j.Itens = []ItemComprado{}
	}
	if j.Build == nil {
		j.Build = []EvidenciaBuild{}
	}
	if j.Componentes == nil {
		j.Componentes = []ComponenteNota{}
	}
	if j.Contexto.Falta == nil {
		j.Contexto.Falta = []string{}
	}
	return g, j, nil
}

// LeFatos devolve a base de fatos de uma partida (ErrSemResumo se não há).
func LeFatos(db *banco.DB, gameID int64) (ResumoPartida, error) {
	r := ResumoPartida{GameID: gameID}
	var dados string
	err := db.QueryRow(`SELECT versao, calculado_em, criada_em, patch, duracao_s, vencedor, elo, maior_vantagem_azul,
		maior_vantagem_azul_ms, maior_vantagem_vermelho, maior_vantagem_vermelho_ms, viradas, texto, dados
		FROM resumos_partida WHERE game_id = ?`, gameID).Scan(&r.Versao, &r.CalculadoEm, &r.CriadaEm, &r.Patch, &r.DuracaoS,
		&r.Vencedor, &r.Elo, &r.MaiorVantagemAzul, &r.MaiorVantagemAzulMs, &r.MaiorVantagemVermelho, &r.MaiorVantagemVermelhoMs,
		&r.Viradas, &r.Texto, &dados)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrSemResumo
	}
	if err != nil {
		return r, err
	}
	var d struct {
		Times      []ResumoTime `json:"times"`
		Curva      []PontoOuro  `json:"curva_ouro"`
		Rastreados []int        `json:"rastreados"`
		Riot       bool         `json:"riot"`
		Avisos     []string     `json:"avisos"`
		Fila       string       `json:"fila"`
	}
	json.Unmarshal([]byte(dados), &d)
	r.Times, r.CurvaOuro, r.Rastreados, r.Riot, r.Avisos, r.Fila = d.Times, d.Curva, d.Rastreados, d.Riot, d.Avisos, d.Fila
	if r.Avisos == nil {
		r.Avisos = []string{}
	}
	if r.Rastreados == nil {
		r.Rastreados = []int{}
	}
	if r.CurvaOuro == nil {
		r.CurvaOuro = []PontoOuro{}
	}
	if r.Times == nil {
		r.Times = []ResumoTime{}
	}
	linhas, err := db.Query(`SELECT `+colunasJogador+` FROM resumos_jogador WHERE game_id = ? ORDER BY participante_id`, gameID)
	if err != nil {
		return r, err
	}
	r.Jogadores = []ResumoJogador{}
	for linhas.Next() {
		_, j, err := leJogador(linhas)
		if err != nil {
			linhas.Close()
			return r, err
		}
		r.Jogadores = append(r.Jogadores, j)
	}
	linhas.Close()
	if err := linhas.Err(); err != nil {
		return r, err
	}
	ms, err := db.Query(`SELECT `+colunasMomento+` FROM momentos_chave WHERE game_id = ? ORDER BY n`, gameID)
	if err != nil {
		return r, err
	}
	defer ms.Close()
	r.Momentos = []MomentoChave{}
	for ms.Next() {
		_, m, err := leMomento(ms)
		if err != nil {
			return r, err
		}
		r.Momentos = append(r.Momentos, m)
	}
	return r, ms.Err()
}

// --- Busca ------------------------------------------------------------------------------

// FiltroResumos são os filtros de GET /api/resumos (vazio = não filtra).
type FiltroResumos struct {
	Campeao   string
	Funcao    string
	Resultado string // vitoria, derrota
	Puuid     string
	Tipo      string // tipo de momento
	Tag       string // etiqueta de momento (sozinho, previa_gankando...)
	Texto     string // pedaço da frase do momento
	AteMs     int64  // momento até esse instante (0 = sem limite)
	Limite    int
}

func (f FiltroResumos) temMomento() bool {
	return f.Tipo != "" || f.Texto != "" || f.Tag != "" || f.AteMs > 0
}

// LinhaBusca: um jogador numa partida (e os momentos dele que casaram).
type LinhaBusca struct {
	GameID       int64          `json:"game_id"`
	CriadaEm     string         `json:"criada_em"`
	DuracaoS     int            `json:"duracao_s"`
	Vencedor     int            `json:"vencedor"`
	TextoPartida string         `json:"texto_partida"`
	Jogador      *ResumoJogador `json:"jogador"`
	Momentos     []MomentoChave `json:"momentos"`
}

// BuscaResumos responde à busca só com as linhas do banco: primeiro as chaves
// que casam (uma consulta), depois cada linha pela chave.
func BuscaResumos(db *banco.DB, f FiltroResumos) ([]LinhaBusca, error) {
	if f.Limite <= 0 || f.Limite > 200 {
		f.Limite = 50
	}
	var cond []string
	var args []any
	if f.Campeao != "" {
		cond, args = append(cond, `LOWER(j.campeao) = LOWER(?)`), append(args, f.Campeao)
	}
	if f.Funcao != "" {
		cond, args = append(cond, `j.funcao = ?`), append(args, strings.ToUpper(f.Funcao))
	}
	switch f.Resultado {
	case "vitoria":
		cond = append(cond, `j.venceu = 1`)
	case "derrota":
		cond = append(cond, `j.venceu = 0`)
	}
	if f.Puuid != "" {
		cond, args = append(cond, `j.puuid = ?`), append(args, f.Puuid)
	}
	var q string
	if f.temMomento() {
		if f.Tipo != "" {
			cond, args = append(cond, `m.tipo = ?`), append(args, f.Tipo)
		}
		if f.Tag != "" {
			cond, args = append(cond, `m.tags LIKE ?`), append(args, "%,"+f.Tag+",%")
		}
		if f.Texto != "" {
			cond, args = append(cond, `LOWER(m.frase) LIKE LOWER(?)`), append(args, "%"+f.Texto+"%")
		}
		if f.AteMs > 0 {
			cond, args = append(cond, `m.t_ms <= ?`), append(args, f.AteMs)
		}
		q = `SELECT m.game_id, m.participante_id, m.n FROM momentos_chave m
			JOIN resumos_partida p ON p.game_id = m.game_id
			JOIN resumos_jogador j ON j.game_id = m.game_id AND j.participante_id = m.participante_id`
	} else {
		q = `SELECT j.game_id, j.participante_id, 0 FROM resumos_jogador j JOIN resumos_partida p ON p.game_id = j.game_id`
	}
	if len(cond) > 0 {
		q += ` WHERE ` + strings.Join(cond, " AND ")
	}
	if f.temMomento() {
		q += ` ORDER BY p.criada_em DESC, m.game_id DESC, m.impacto DESC LIMIT ?`
	} else {
		q += ` ORDER BY p.criada_em DESC, j.game_id DESC, j.participante_id LIMIT ?`
	}
	type chave struct {
		g   int64
		pid int
		n   int
	}
	var chaves []chave
	linhas, err := db.Query(q, append(args, f.Limite)...)
	if err != nil {
		return nil, err
	}
	for linhas.Next() {
		var c chave
		if err := linhas.Scan(&c.g, &c.pid, &c.n); err != nil {
			linhas.Close()
			return nil, err
		}
		chaves = append(chaves, c)
	}
	linhas.Close()
	if err := linhas.Err(); err != nil {
		return nil, err
	}
	l := []LinhaBusca{}
	idx := map[[2]int64]int{}
	for _, c := range chaves {
		k := [2]int64{c.g, int64(c.pid)}
		i, ok := idx[k]
		if !ok {
			lb := LinhaBusca{GameID: c.g, Momentos: []MomentoChave{}}
			if err := db.QueryRow(`SELECT criada_em, duracao_s, vencedor, texto FROM resumos_partida WHERE game_id = ?`, c.g).
				Scan(&lb.CriadaEm, &lb.DuracaoS, &lb.Vencedor, &lb.TextoPartida); err != nil {
				return nil, err
			}
			_, j, err := leJogador(db.QueryRow(`SELECT `+colunasJogador+` FROM resumos_jogador WHERE game_id = ? AND participante_id = ?`, c.g, c.pid))
			if err != nil {
				return nil, err
			}
			lb.Jogador = &j
			l = append(l, lb)
			i = len(l) - 1
			idx[k] = i
		}
		if c.n > 0 {
			_, m, err := leMomento(db.QueryRow(`SELECT `+colunasMomento+` FROM momentos_chave WHERE game_id = ? AND n = ?`, c.g, c.n))
			if err != nil {
				return nil, err
			}
			l[i].Momentos = append(l[i].Momentos, m)
		}
	}
	return l, nil
}
