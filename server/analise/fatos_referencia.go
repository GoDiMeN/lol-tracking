package analise

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"loltracking/server/banco"
)

// REFERÊNCIAS para as vozes por dado da IA ("o que os melhores do elo fazem",
// mono, Challenger, Esmeralda): o jogador em foco e os jogadores de
// referência da coleta por elo em números comparáveis, lado a lado. Só as três
// tabelas da base de fatos; nenhum arquivo de partida é aberto.
//
// Jogador de referência = o jogador ESCOLHIDO pela coleta numa partida da
// coleta: a partida tem elo (resumos_partida.elo, que só a coleta preenche),
// o jogador foi rastreado e tem o elo dele guardado (resumos_jogador.elo, da
// league-v4 no dia da coleta). Os outros 9 da partida da coleta não entram:
// não têm o elo sabido e, na coleta por amostragem, nem foram rastreados (sem
// atividade, sem janelas, sem as etiquetas das mortes). Partida com um só
// rastreado é o caso normal da coleta e dá a linha completa daquele jogador.

// IndicadoresJogador: um jogador numa partida, em números comparáveis.
type IndicadoresJogador struct {
	GameID         int64  `json:"game_id"`
	ParticipanteID int    `json:"participante_id"`
	Puuid          string `json:"-"`
	Nome           string `json:"nome"`
	Campeao        string `json:"campeao"`
	Funcao         string `json:"funcao"`
	// Elo: o do jogador ("PLATINUM II", da coleta); EloPartida: o da partida
	// ("PLATINUM", o elo da coleta). Vazios fora da coleta.
	Elo        string `json:"elo"`
	EloPartida string `json:"elo_partida"`
	Patch      string `json:"patch"`
	CriadaEm   string `json:"criada_em"`
	Venceu     bool   `json:"venceu"`
	DuracaoS   int    `json:"duracao_s"`
	Rastreado  bool   `json:"rastreado"`
	// RastreadosNaPartida: quantos dos 10 foram rastreados. Com menos de 10,
	// as atividades que precisam dos outros rastreados (luta, troca, gank,
	// rotação...) não aparecem: o tempo delas vira andando/farmando.
	RastreadosNaPartida int    `json:"rastreados_na_partida"`
	TextoPartida        string `json:"texto_partida"`

	Abates       int     `json:"abates"`
	Mortes       int     `json:"mortes"`
	Assistencias int     `json:"assistencias"`
	KDA          float64 `json:"kda"`
	Ouro10       *int    `json:"ouro_10"`
	Ouro15       *int    `json:"ouro_15"`
	CS10         *int    `json:"cs_10"`
	CS15         *int    `json:"cs_15"`
	DifOuro10    *int    `json:"dif_ouro_10"`
	DifOuro15    *int    `json:"dif_ouro_15"`
	DifCS10      *int    `json:"dif_cs_10"`
	DifCS15      *int    `json:"dif_cs_15"`

	MortesRotas int `json:"mortes_rotas"`
	MortesMeio  int `json:"mortes_meio"`
	MortesFinal int `json:"mortes_final"`
	MortesCaras int `json:"mortes_caras"`
	// Das etiquetas das mortes (momentos_chave): só com o jogador rastreado
	// (sozinho e inferioridade precisam da posição dos 10 ou da linha do
	// tempo; antes_de_objetivo e sem_troca saem da linha do tempo).
	MortesSozinho       int `json:"mortes_sozinho"`
	MortesAntesObjetivo int `json:"mortes_antes_objetivo"`
	MortesSemTroca      int `json:"mortes_sem_troca"`
	MortesInferioridade int `json:"mortes_inferioridade"`

	ObjetivosParticipou int `json:"objetivos_participou"`
	ObjetivosTime       int `json:"objetivos_time"`
	Sentinelas          int `json:"sentinelas"`
	SentinelasControle  int `json:"sentinelas_controle"`
	JanelasAproveitadas int `json:"janelas_aproveitadas"`
	JanelasPerdidas     int `json:"janelas_perdidas"`
	// Etiquetas: segundos por atividade, no total e por fase (rotas, meio, final).
	Etiquetas map[string]map[string]int `json:"etiquetas"`
	// Papel: como ele jogou (papel.go): carregou, habilitou... e o estilo da função.
	Papel PapelJogador `json:"papel"`
}

// IndicadoresDe lê os indicadores de um jogador numa partida (ErrSemResumo
// se a partida não tem base de fatos).
func IndicadoresDe(db *banco.DB, gameID int64, pid int) (IndicadoresJogador, error) {
	var x IndicadoresJogador
	var dados string
	if err := db.QueryRow(`SELECT criada_em, patch, duracao_s, elo, texto, dados FROM resumos_partida WHERE game_id = ?`, gameID).
		Scan(&x.CriadaEm, &x.Patch, &x.DuracaoS, &x.EloPartida, &x.TextoPartida, &dados); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return x, ErrSemResumo
		}
		return x, err
	}
	_, j, err := leJogador(db.QueryRow(`SELECT `+colunasJogador+` FROM resumos_jogador WHERE game_id = ? AND participante_id = ?`, gameID, pid))
	if err != nil {
		return x, err
	}
	var d struct {
		Rastreados []int `json:"rastreados"`
	}
	json.Unmarshal([]byte(dados), &d)
	x.RastreadosNaPartida = len(d.Rastreados)
	x.GameID, x.ParticipanteID, x.Puuid, x.Nome, x.Campeao, x.Funcao = gameID, pid, j.Puuid, j.Nome, j.Campeao, j.Funcao
	x.Elo, x.Venceu, x.Rastreado = j.Elo, j.Venceu, j.Rastreado
	x.Abates, x.Mortes, x.Assistencias, x.KDA = j.Abates, j.Mortes, j.Assistencias, j.KDA
	x.Ouro10, x.Ouro15, x.CS10, x.CS15 = j.Ouro10, j.Ouro15, j.CS10, j.CS15
	x.DifOuro10, x.DifOuro15, x.DifCS10, x.DifCS15 = j.DifOuro10, j.DifOuro15, j.DifCS10, j.DifCS15
	x.MortesRotas, x.MortesMeio, x.MortesFinal, x.MortesCaras = j.MortesRotas, j.MortesMeio, j.MortesFinal, j.MortesCaras
	x.ObjetivosParticipou, x.ObjetivosTime = j.ObjetivosParticipou, j.ObjetivosTime
	x.Sentinelas, x.SentinelasControle = j.Sentinelas, j.SentinelasControle
	x.Etiquetas, x.Papel = j.Etiquetas, j.Papel
	linhas, err := db.Query(`SELECT tipo, tags FROM momentos_chave WHERE game_id = ? AND participante_id = ?
		AND tipo IN ('abate', 'janela_perdida', 'janela_aproveitada')`, gameID, pid)
	if err != nil {
		return x, err
	}
	defer linhas.Close()
	for linhas.Next() {
		var tipo, tags string
		if err := linhas.Scan(&tipo, &tags); err != nil {
			return x, err
		}
		switch tipo {
		case "janela_perdida":
			x.JanelasPerdidas++
		case "janela_aproveitada":
			x.JanelasAproveitadas++
		case "abate":
			tem := func(t string) bool { return strings.Contains(tags, ","+t+",") }
			if tem("sozinho") {
				x.MortesSozinho++
			}
			if tem("antes_de_objetivo") {
				x.MortesAntesObjetivo++
			}
			if tem("sem_troca") {
				x.MortesSemTroca++
			}
			if tem("inferioridade") {
				x.MortesInferioridade++
			}
		}
	}
	return x, linhas.Err()
}

// FiltroReferencia escolhe os jogadores de referência (vazio = não filtra).
type FiltroReferencia struct {
	// Elo: o da partida da coleta ("PLATINUM", "CHALLENGER"), sem caixa.
	Elo       string
	Funcao    string
	Campeao   string
	Resultado string // vitoria, derrota
	// ExcluiGame e ExcluiPuuid: a própria partida e o próprio jogador não são
	// referência dele mesmo.
	ExcluiGame  int64
	ExcluiPuuid string
	Limite      int
	// ForaDaColeta inverte a origem: em vez dos jogadores de referência da
	// coleta, os jogadores RASTREADOS das partidas que não são da coleta (as
	// do usuário). O Especialista Lane compara com eles o que o jogador de
	// referência fez; Elo não se aplica (essas partidas não têm elo sabido).
	ForaDaColeta bool
}

// Referencias devolve os jogadores de referência que casam, da partida mais
// nova para a mais velha.
func Referencias(db *banco.DB, f FiltroReferencia) ([]IndicadoresJogador, error) {
	if f.Limite <= 0 || f.Limite > 200 {
		f.Limite = 20
	}
	cond := []string{`p.elo <> ''`, `j.rastreado = 1`, `j.elo <> ''`}
	if f.ForaDaColeta {
		cond = []string{`p.elo = ''`, `j.rastreado = 1`}
	}
	var args []any
	if f.Elo != "" && !f.ForaDaColeta {
		// O elo do JOGADOR ("PLATINUM II", "CHALLENGER"), não o da partida: o
		// jogador aproveitado na partida de outro (aprendizado/aproveita.go) pode
		// ser de outro elo pedido pela coleta.
		cond, args = append(cond, `(UPPER(j.elo) = UPPER(?) OR UPPER(j.elo) LIKE UPPER(?) || ' %')`), append(args, f.Elo, f.Elo)
	}
	if f.Funcao != "" {
		cond, args = append(cond, `j.funcao = ?`), append(args, strings.ToUpper(f.Funcao))
	}
	if f.Campeao != "" {
		cond, args = append(cond, `LOWER(j.campeao) = LOWER(?)`), append(args, f.Campeao)
	}
	switch f.Resultado {
	case "vitoria":
		cond = append(cond, `j.venceu = 1`)
	case "derrota":
		cond = append(cond, `j.venceu = 0`)
	}
	if f.ExcluiGame != 0 {
		cond, args = append(cond, `j.game_id <> ?`), append(args, f.ExcluiGame)
	}
	if f.ExcluiPuuid != "" {
		cond, args = append(cond, `j.puuid <> ?`), append(args, f.ExcluiPuuid)
	}
	linhas, err := db.Query(`SELECT j.game_id, j.participante_id FROM resumos_jogador j JOIN resumos_partida p ON p.game_id = j.game_id
		WHERE `+strings.Join(cond, " AND ")+` ORDER BY p.criada_em DESC, j.game_id DESC, j.participante_id LIMIT ?`, append(args, f.Limite)...)
	if err != nil {
		return nil, err
	}
	type chave struct {
		g   int64
		pid int
	}
	var ks []chave
	for linhas.Next() {
		var k chave
		if err := linhas.Scan(&k.g, &k.pid); err != nil {
			linhas.Close()
			return nil, err
		}
		ks = append(ks, k)
	}
	linhas.Close()
	if err := linhas.Err(); err != nil {
		return nil, err
	}
	r := []IndicadoresJogador{}
	for _, k := range ks {
		x, err := IndicadoresDe(db, k.g, k.pid)
		if err != nil {
			return nil, err
		}
		r = append(r, x)
	}
	return r, nil
}
