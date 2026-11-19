package analise

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"loltracking/server/banco"
	"loltracking/server/campeao"
	"loltracking/server/partida"
)

// A BASE DE FATOS da partida: um resumo pesquisável (linhas pequenas no banco,
// nada de arquivo), os momentos-chave com o custo estimado de cada um, as
// janelas de oportunidade, a nota de impacto de cada jogador com os componentes
// e a build lado a lado com o estado do jogo. Tudo por regra (sem IA), gerado
// ao fim de Recalcula a partir do que a análise já calculou; quem consulta
// (busca, pacote de contexto) lê só estas tabelas e nunca descompacta os
// arquivos da partida. Regras e vocabulário em docs/arquitetura/dados.md
// ("Base de fatos").

// VersaoFatos sobe quando a regra muda: partida com versão menor é refeita no
// próximo preenchimento (subida do servidor ou primeiro pedido).
// 2: "sentinelas" do jogador conta só os quatro tipos conhecidos (o UNDEFINED da
// Riot não é sentinela: o Kayn da 3305481832 tinha 88 assim e 0 de verdade).
// 3: o papel do jogador na partida (papel.go): colunas papel e estilo e dados.papel.
// 4: papel: a linha de frente de uma vitória não afunda nem é carregada (mortes
// em lutas que o time venceu); estilo objetivos do caçador exige mais que os
// épicos (participação nos objetivos do time ou tempo em objetivo).
// 5: tempos da selva por patch e conferidos (tempos_selva.go): no 26.x os
// acampamentos nascem a 0:55/1:07 (eram 1:30 de memória) e o primeiro
// nascimento é o piso das janelas. O contexto dos jogadores junta o histórico
// buscado (fatos_historico.go), só Summoner's Rift, e diz campeão e fonte.
// 6: a visão antes de cada abate (visao.go): dados.visao_atacantes e
// dados.visao_cacador, etiquetas atacante_nao_visto e cacador_nao_visto.
const VersaoFatos = 6

func migraFatos(db *banco.DB) error {
	if err := migraTabelasFatos(db); err != nil {
		return err
	}
	if err := migraHistoricos(db); err != nil {
		return err
	}
	// O papel (VersaoFatos 3): colunas para a busca ("o topo de Platina que
	// carregou"); a partida com versão menor é refeita no preenchimento.
	for _, c := range []string{"papel", "estilo"} {
		if err := banco.AdicionaColuna(db, "resumos_jogador", c, "TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}
	return banco.Executa(db, `CREATE INDEX IF NOT EXISTS resumos_jogador_papel ON resumos_jogador (funcao, papel, estilo)`)
}

func migraTabelasFatos(db *banco.DB) error {
	return banco.Executa(db,
		`CREATE TABLE IF NOT EXISTS resumos_partida (
			game_id                    INTEGER PRIMARY KEY,
			versao                     INTEGER NOT NULL,
			calculado_em               TEXT NOT NULL,
			criada_em                  TEXT NOT NULL DEFAULT '',
			patch                      TEXT NOT NULL DEFAULT '',
			duracao_s                  INTEGER NOT NULL DEFAULT 0,
			vencedor                   INTEGER NOT NULL DEFAULT 0,
			elo                        TEXT NOT NULL DEFAULT '',
			abates_azul                INTEGER NOT NULL DEFAULT 0,
			abates_vermelho            INTEGER NOT NULL DEFAULT 0,
			dragoes_azul               INTEGER NOT NULL DEFAULT 0,
			dragoes_vermelho           INTEGER NOT NULL DEFAULT 0,
			baroes_azul                INTEGER NOT NULL DEFAULT 0,
			baroes_vermelho            INTEGER NOT NULL DEFAULT 0,
			torres_azul                INTEGER NOT NULL DEFAULT 0,
			torres_vermelho            INTEGER NOT NULL DEFAULT 0,
			maior_vantagem_azul        INTEGER NOT NULL DEFAULT 0,
			maior_vantagem_azul_ms     INTEGER NOT NULL DEFAULT 0,
			maior_vantagem_vermelho    INTEGER NOT NULL DEFAULT 0,
			maior_vantagem_vermelho_ms INTEGER NOT NULL DEFAULT 0,
			viradas                    INTEGER NOT NULL DEFAULT 0,
			texto                      TEXT NOT NULL DEFAULT '',
			dados                      TEXT NOT NULL DEFAULT '{}'
		)`,
		`CREATE TABLE IF NOT EXISTS resumos_jogador (
			game_id              INTEGER NOT NULL,
			participante_id      INTEGER NOT NULL,
			puuid                TEXT NOT NULL DEFAULT '',
			nome                 TEXT NOT NULL DEFAULT '',
			campeao              TEXT NOT NULL DEFAULT '',
			funcao               TEXT NOT NULL DEFAULT '',
			time                 INTEGER NOT NULL DEFAULT 0,
			venceu               INTEGER NOT NULL DEFAULT 0,
			rastreado            INTEGER NOT NULL DEFAULT 0,
			elo                  TEXT NOT NULL DEFAULT '',
			abates               INTEGER NOT NULL DEFAULT 0,
			mortes               INTEGER NOT NULL DEFAULT 0,
			assistencias         INTEGER NOT NULL DEFAULT 0,
			kda                  REAL NOT NULL DEFAULT 0,
			ouro_10              INTEGER,
			ouro_15              INTEGER,
			ouro_fim             INTEGER,
			cs_10                INTEGER,
			cs_15                INTEGER,
			cs_fim               INTEGER,
			adversario           INTEGER NOT NULL DEFAULT 0,
			dif_ouro_10          INTEGER,
			dif_ouro_15          INTEGER,
			dif_cs_10            INTEGER,
			dif_cs_15            INTEGER,
			mortes_rotas         INTEGER NOT NULL DEFAULT 0,
			mortes_meio          INTEGER NOT NULL DEFAULT 0,
			mortes_final         INTEGER NOT NULL DEFAULT 0,
			mortes_caras         INTEGER NOT NULL DEFAULT 0,
			objetivos_participou INTEGER NOT NULL DEFAULT 0,
			objetivos_time       INTEGER NOT NULL DEFAULT 0,
			sentinelas           INTEGER NOT NULL DEFAULT 0,
			sentinelas_controle  INTEGER NOT NULL DEFAULT 0,
			nota                 INTEGER NOT NULL DEFAULT 0,
			posicao_nota         INTEGER NOT NULL DEFAULT 0,
			funcao_principal     TEXT NOT NULL DEFAULT '',
			etiquetas            TEXT NOT NULL DEFAULT '{}',
			dados                TEXT NOT NULL DEFAULT '{}',
			PRIMARY KEY (game_id, participante_id)
		)`,
		`CREATE INDEX IF NOT EXISTS resumos_jogador_campeao ON resumos_jogador (campeao, funcao)`,
		`CREATE INDEX IF NOT EXISTS resumos_jogador_puuid ON resumos_jogador (puuid)`,
		`CREATE TABLE IF NOT EXISTS momentos_chave (
			game_id          INTEGER NOT NULL,
			n                INTEGER NOT NULL,
			t_ms             INTEGER NOT NULL,
			fim_ms           INTEGER NOT NULL,
			tipo             TEXT NOT NULL,
			subtipo          TEXT NOT NULL DEFAULT '',
			time             INTEGER NOT NULL DEFAULT 0,
			participante_id  INTEGER NOT NULL DEFAULT 0,
			autor            INTEGER NOT NULL DEFAULT 0,
			funcao           TEXT NOT NULL DEFAULT '',
			campeao          TEXT NOT NULL DEFAULT '',
			envolvidos       TEXT NOT NULL DEFAULT '[]',
			atividade        TEXT NOT NULL DEFAULT '',
			atividade_previa TEXT NOT NULL DEFAULT '',
			zona             TEXT NOT NULL DEFAULT '',
			rota             TEXT NOT NULL DEFAULT '',
			duracao_ms       INTEGER NOT NULL DEFAULT 0,
			saldo_antes      INTEGER,
			saldo_depois     INTEGER,
			custo_azul       INTEGER NOT NULL DEFAULT 0,
			custo_vermelho   INTEGER NOT NULL DEFAULT 0,
			impacto          INTEGER NOT NULL DEFAULT 0,
			chave            INTEGER NOT NULL DEFAULT 0,
			estimado         INTEGER NOT NULL DEFAULT 0,
			tags             TEXT NOT NULL DEFAULT '',
			frase            TEXT NOT NULL DEFAULT '',
			dados            TEXT NOT NULL DEFAULT '{}',
			PRIMARY KEY (game_id, n)
		)`,
		`CREATE INDEX IF NOT EXISTS momentos_chave_tipo ON momentos_chave (tipo, game_id)`,
	)
}

// --- Tipos ------------------------------------------------------------------------

// ResumoPartida é a partida inteira em fatos (GET /api/partidas/{id}/resumo).
type ResumoPartida struct {
	GameID      int64  `json:"game_id"`
	Versao      int    `json:"versao"`
	CalculadoEm string `json:"calculado_em"`
	CriadaEm    string `json:"criada_em"`
	Patch       string `json:"patch"`
	Fila        string `json:"fila"`
	DuracaoS    int    `json:"duracao_s"`
	Vencedor    int    `json:"vencedor"`
	// Elo: o da partida quando o app sabe (coleta por elo); "" = desconhecido.
	Elo   string       `json:"elo"`
	Times []ResumoTime `json:"times"`
	// Virada de ouro: a maior vantagem de cada lado (azul − vermelho, pelos
	// quadros de minuto) e quando; viradas = quantas vezes o time na frente
	// trocou (contando só vantagem de pelo menos 1 000).
	MaiorVantagemAzul       int         `json:"maior_vantagem_azul"`
	MaiorVantagemAzulMs     int64       `json:"maior_vantagem_azul_ms"`
	MaiorVantagemVermelho   int         `json:"maior_vantagem_vermelho"`
	MaiorVantagemVermelhoMs int64       `json:"maior_vantagem_vermelho_ms"`
	Viradas                 int         `json:"viradas"`
	CurvaOuro               []PontoOuro `json:"curva_ouro"`
	Rastreados              []int       `json:"rastreados"`
	Riot                    bool        `json:"riot"`
	// Texto: a linha curta da partida (o contexto de várias partidas usa).
	Texto     string          `json:"texto"`
	Avisos    []string        `json:"avisos"`
	Jogadores []ResumoJogador `json:"jogadores"`
	Momentos  []MomentoChave  `json:"momentos"`
}

// ResumoTime: o placar de objetivos de um time (o que ELE derrubou/pegou).
type ResumoTime struct {
	Time        int      `json:"time"`
	Venceu      bool     `json:"venceu"`
	Abates      int      `json:"abates"`
	OuroFinal   int      `json:"ouro_final"`
	Dragoes     []string `json:"dragoes"` // na ordem, com o nome ("dragão infernal")
	Anciao      int      `json:"anciao"`
	Baroes      int      `json:"baroes"`
	Arautos     int      `json:"arautos"`
	Vastilarvas int      `json:"vastilarvas"`
	Atakhan     int      `json:"atakhan"`
	Torres      int      `json:"torres"`
	Inibidores  int      `json:"inibidores"`
	Placas      int      `json:"placas"` // só com a linha do tempo da Riot
}

// PontoOuro: o ouro total de cada time num quadro de minuto.
type PontoOuro struct {
	TMs       int64 `json:"t_ms"`
	Azul      int   `json:"azul"`
	Vermelho  int   `json:"vermelho"`
	Diferenca int   `json:"diferenca"` // azul − vermelho
}

// ResumoJogador é um jogador na partida, em fatos.
type ResumoJogador struct {
	ParticipanteID int     `json:"participante_id"`
	Puuid          string  `json:"puuid"`
	Nome           string  `json:"nome"`
	Campeao        string  `json:"campeao"`
	Funcao         string  `json:"funcao"`
	Time           int     `json:"time"`
	Venceu         bool    `json:"venceu"`
	Rastreado      bool    `json:"rastreado"`
	Elo            string  `json:"elo"`
	Abates         int     `json:"abates"`
	Mortes         int     `json:"mortes"`
	Assistencias   int     `json:"assistencias"`
	KDA            float64 `json:"kda"`
	Ouro10         *int    `json:"ouro_10"`
	Ouro15         *int    `json:"ouro_15"`
	OuroFim        *int    `json:"ouro_fim"`
	CS10           *int    `json:"cs_10"`
	CS15           *int    `json:"cs_15"`
	CSFim          *int    `json:"cs_fim"`
	Adversario     int     `json:"adversario"`
	DifOuro10      *int    `json:"dif_ouro_10"`
	DifOuro15      *int    `json:"dif_ouro_15"`
	DifCS10        *int    `json:"dif_cs_10"`
	DifCS15        *int    `json:"dif_cs_15"`
	MortesRotas    int     `json:"mortes_rotas"`
	MortesMeio     int     `json:"mortes_meio"`
	MortesFinal    int     `json:"mortes_final"`
	MortesCaras    int     `json:"mortes_caras"`
	// Objetivos (dragão, barão, arauto, vastilarvas, atakhan, torre, inibidor)
	// do time dele em que ele matou ou ajudou, e o total do time.
	ObjetivosParticipou int `json:"objetivos_participou"`
	ObjetivosTime       int `json:"objetivos_time"`
	Sentinelas          int `json:"sentinelas"`
	SentinelasControle  int `json:"sentinelas_controle"`
	// Etiquetas: segundos em cada atividade, no total e por fase
	// ({"total": {"farmando": 412, ...}, "rotas": {...}}); só rastreados.
	Etiquetas map[string]map[string]int `json:"etiquetas"`
	Itens     []ItemComprado            `json:"itens"`
	Build     []EvidenciaBuild          `json:"build"`
	// Nota de impacto (ouro equivalente) e a posição dela na partida (1 = maior).
	Nota        int              `json:"nota"`
	PosicaoNota int              `json:"posicao_nota"`
	Componentes []ComponenteNota `json:"componentes"`
	Contexto    ContextoJogador  `json:"contexto"`
	// Papel: como ele jogou (papel.go): carregou, habilitou... e o estilo da função.
	Papel PapelJogador `json:"papel"`
}

// ItemComprado: uma compra, na ordem.
type ItemComprado struct {
	TMs      int64  `json:"t_ms"`
	ItemID   int    `json:"item_id"`
	Nome     string `json:"nome"`
	Custo    *int   `json:"custo"`
	Completo bool   `json:"completo"`
}

// MomentoChave é um acontecimento da partida com o custo estimado para cada
// time (tabela momentos_chave). Tipos: abate, luta, objetivo, torre,
// inibidor, freeze, janela_perdida, janela_aproveitada.
type MomentoChave struct {
	N     int    `json:"n"`
	TMs   int64  `json:"t_ms"`
	FimMs int64  `json:"fim_ms"`
	Tipo  string `json:"tipo"`
	// Subtipo: abate → "morte_cara" ou ""; objetivo/torre → o nome do alvo
	// ("dragão infernal", "torre do topo"); freeze → a rota; janela → "selva" ou
	// "rota".
	Subtipo string `json:"subtipo"`
	// Time: quem ganhou o momento (0 = nenhum dos dois).
	Time int `json:"time"`
	// ParticipanteID: o protagonista (a vítima no abate; o jogador da janela;
	// quem matou o objetivo); Autor: quem matou (abate, objetivo).
	ParticipanteID  int            `json:"participante_id"`
	Autor           int            `json:"autor"`
	Funcao          string         `json:"funcao"`
	Campeao         string         `json:"campeao"`
	Envolvidos      []int          `json:"envolvidos"`
	Atividade       string         `json:"atividade"`
	AtividadePrevia string         `json:"atividade_previa"`
	Zona            string         `json:"zona"`
	Rota            string         `json:"rota"`
	DuracaoMs       int64          `json:"duracao_ms"`
	SaldoAntes      *int           `json:"saldo_antes"`
	SaldoDepois     *int           `json:"saldo_depois"`
	CustoAzul       int            `json:"custo_azul"`
	CustoVermelho   int            `json:"custo_vermelho"`
	Impacto         int            `json:"impacto"`
	Chave           bool           `json:"chave"`
	Estimado        bool           `json:"estimado"`
	Tags            []string       `json:"tags"`
	Frase           string         `json:"frase"`
	Dados           map[string]any `json:"dados"`
}

// --- Geração ------------------------------------------------------------------------

// ErrSemResumo: a partida nunca foi analisada (a base de fatos sai da análise).
var ErrSemResumo = errors.New("a partida ainda não foi analisada: a base de fatos sai da análise")

// geraFatos monta a base de fatos de uma partida já carregada (dp) com os
// retratos e indicadores que a análise acabou de calcular.
func geraFatos(ctx context.Context, d Deps, dp *dadosPartida, retratos []Retrato) ResumoPartida {
	r := ResumoPartida{GameID: dp.pt.GameID, Versao: VersaoFatos, CriadaEm: dp.pt.CriadaEm, Patch: dp.pt.Patch,
		Fila: dp.pt.FilaNome, DuracaoS: dp.pt.DuracaoS, Vencedor: dp.pt.TimeVencedor, Riot: dp.riot != nil,
		Avisos: []string{}, Rastreados: []int{}}
	for pid := range dp.amostras {
		r.Rastreados = append(r.Rastreados, pid)
	}
	sort.Ints(r.Rastreados)
	var est *campeao.Estaticos
	if d.Estaticos != nil {
		c2, cancela := context.WithTimeout(ctx, 20*time.Second)
		ids := []int{}
		for _, p := range dp.pt.Participantes {
			ids = append(ids, p.CampeaoID)
		}
		e, err := d.Estaticos.Estaticos(c2, dp.pt.Versao, ids)
		cancela()
		if err == nil {
			est = &e
		} else {
			r.Avisos = append(r.Avisos, "sem os dados estáticos da versão (nome e custo dos itens): "+err.Error())
		}
	} else {
		r.Avisos = append(r.Avisos, "sem os dados estáticos da versão: itens sem nome e sem custo")
	}
	if dp.riot == nil {
		r.Avisos = append(r.Avisos, "sem a linha do tempo da Riot: ouro do abate pelo valor padrão, sem placas, sem sentinelas")
	}
	f := novoFatos(dp, retratos, est)
	if !f.selvaCoberta {
		r.Avisos = append(r.Avisos, fmt.Sprintf("patch %q fora da tabela de tempos da selva: usados os tempos de %s", dp.pt.Patch, f.selva.Patches))
	}
	r.CurvaOuro = f.curva
	r.MaiorVantagemAzul, r.MaiorVantagemAzulMs, r.MaiorVantagemVermelho, r.MaiorVantagemVermelhoMs, r.Viradas = viradaDeOuro(f.curva)
	r.Times = f.times()
	r.Momentos = f.momentos()
	r.Jogadores = f.jogadores(d.DB)
	f.notas(r.Jogadores, r.Momentos)
	f.papeis(r.Jogadores)
	r.Elo = eloDaPartida(d.DB, r.GameID)
	r.Texto = textoDaPartida(r)
	return r
}

// gravaFatos troca as linhas da partida nas três tabelas (numa transação).
func gravaFatos(db *banco.DB, r ResumoPartida) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, t := range []string{"resumos_partida", "resumos_jogador", "momentos_chave"} {
		if _, err := tx.Exec(`DELETE FROM `+t+` WHERE game_id = ?`, r.GameID); err != nil {
			return err
		}
	}
	r.CalculadoEm = banco.Agora()
	tAz, tVe := timeDoResumo(r, 100), timeDoResumo(r, 200)
	dadosP := map[string]any{"times": r.Times, "curva_ouro": r.CurvaOuro, "rastreados": r.Rastreados, "riot": r.Riot,
		"avisos": r.Avisos, "fila": r.Fila}
	if _, err := tx.Exec(`INSERT INTO resumos_partida (game_id, versao, calculado_em, criada_em, patch, duracao_s, vencedor, elo,
		abates_azul, abates_vermelho, dragoes_azul, dragoes_vermelho, baroes_azul, baroes_vermelho, torres_azul, torres_vermelho,
		maior_vantagem_azul, maior_vantagem_azul_ms, maior_vantagem_vermelho, maior_vantagem_vermelho_ms, viradas, texto, dados)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.GameID, r.Versao, r.CalculadoEm, r.CriadaEm, r.Patch, r.DuracaoS, r.Vencedor, r.Elo,
		tAz.Abates, tVe.Abates, len(tAz.Dragoes)+tAz.Anciao, len(tVe.Dragoes)+tVe.Anciao, tAz.Baroes, tVe.Baroes, tAz.Torres, tVe.Torres,
		r.MaiorVantagemAzul, r.MaiorVantagemAzulMs, r.MaiorVantagemVermelho, r.MaiorVantagemVermelhoMs, r.Viradas, r.Texto,
		jsonTexto(dadosP)); err != nil {
		return err
	}
	for _, j := range r.Jogadores {
		dadosJ := map[string]any{"itens": j.Itens, "build": j.Build, "componentes": j.Componentes, "contexto": j.Contexto, "papel": j.Papel}
		if _, err := tx.Exec(`INSERT INTO resumos_jogador (game_id, participante_id, puuid, nome, campeao, funcao, time, venceu, rastreado,
			elo, abates, mortes, assistencias, kda, ouro_10, ouro_15, ouro_fim, cs_10, cs_15, cs_fim, adversario, dif_ouro_10, dif_ouro_15,
			dif_cs_10, dif_cs_15, mortes_rotas, mortes_meio, mortes_final, mortes_caras, objetivos_participou, objetivos_time, sentinelas,
			sentinelas_controle, nota, posicao_nota, funcao_principal, etiquetas, dados, papel, estilo)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			r.GameID, j.ParticipanteID, j.Puuid, j.Nome, j.Campeao, j.Funcao, j.Time, j.Venceu, j.Rastreado,
			j.Elo, j.Abates, j.Mortes, j.Assistencias, j.KDA, j.Ouro10, j.Ouro15, j.OuroFim, j.CS10, j.CS15, j.CSFim, j.Adversario,
			j.DifOuro10, j.DifOuro15, j.DifCS10, j.DifCS15, j.MortesRotas, j.MortesMeio, j.MortesFinal, j.MortesCaras,
			j.ObjetivosParticipou, j.ObjetivosTime, j.Sentinelas, j.SentinelasControle, j.Nota, j.PosicaoNota,
			j.Contexto.FuncaoPrincipal, jsonTexto(j.Etiquetas), jsonTexto(dadosJ), j.Papel.Papel, j.Papel.Estilo); err != nil {
			return err
		}
	}
	for _, m := range r.Momentos {
		tags := ""
		if len(m.Tags) > 0 {
			// Vírgula nas pontas: `tags LIKE '%,sozinho,%'` acha a etiqueta inteira.
			tags = "," + strings.Join(m.Tags, ",") + ","
		}
		if _, err := tx.Exec(`INSERT INTO momentos_chave (game_id, n, t_ms, fim_ms, tipo, subtipo, time, participante_id, autor, funcao,
			campeao, envolvidos, atividade, atividade_previa, zona, rota, duracao_ms, saldo_antes, saldo_depois, custo_azul, custo_vermelho,
			impacto, chave, estimado, tags, frase, dados) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			r.GameID, m.N, m.TMs, m.FimMs, m.Tipo, m.Subtipo, m.Time, m.ParticipanteID, m.Autor, m.Funcao, m.Campeao,
			jsonTexto(m.Envolvidos), m.Atividade, m.AtividadePrevia, m.Zona, m.Rota, m.DuracaoMs, m.SaldoAntes, m.SaldoDepois,
			m.CustoAzul, m.CustoVermelho, m.Impacto, m.Chave, m.Estimado, tags, m.Frase, jsonTexto(m.Dados)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func timeDoResumo(r ResumoPartida, t int) ResumoTime {
	for _, x := range r.Times {
		if x.Time == t {
			return x
		}
	}
	return ResumoTime{Time: t, Dragoes: []string{}}
}

// --- Preenchimento (partidas analisadas antes da base de fatos) ---------------------

var preencheMu sync.Mutex

// GeraFatosDaPartida refaz só a base de fatos de uma partida já analisada,
// sem refazer a cadeia nem gravar retratos: lê os arquivos da partida UMA vez
// (carrega) e grava as linhas.
func GeraFatosDaPartida(ctx context.Context, d Deps, gameID int64) error {
	recalculaMu.Lock()
	defer recalculaMu.Unlock()
	if _, ok, err := LeResumo(d.DB, gameID); err != nil {
		return err
	} else if !ok {
		return ErrSemResumo
	}
	dp, err := carrega(d.DB, gameID)
	if err != nil {
		return err
	}
	return gravaFatos(d.DB, geraFatos(ctx, d, dp, dp.retratos()))
}

// partidasSemFatos: as analisadas sem resumo, ou com resumo de regra antiga.
func partidasSemFatos(db *banco.DB) ([]int64, error) {
	linhas, err := db.Query(`SELECT a.game_id FROM analises a LEFT JOIN resumos_partida r ON r.game_id = a.game_id
		WHERE r.game_id IS NULL OR r.versao < ? ORDER BY a.game_id`, VersaoFatos)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	var ids []int64
	for linhas.Next() {
		var g int64
		if err := linhas.Scan(&g); err != nil {
			return nil, err
		}
		ids = append(ids, g)
	}
	return ids, linhas.Err()
}

// PreencheFatos gera a base de fatos das partidas analisadas que ainda não a
// têm (ou a têm de regra antiga). Uma partida com erro não segura as outras.
func PreencheFatos(ctx context.Context, d Deps) (int, error) {
	preencheMu.Lock()
	defer preencheMu.Unlock()
	ids, err := partidasSemFatos(d.DB)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, g := range ids {
		if ctx.Err() != nil {
			return n, ctx.Err()
		}
		if err := GeraFatosDaPartida(ctx, d, g); err != nil {
			log.Printf("base de fatos da partida %d: %v", g, err)
			continue
		}
		n++
	}
	return n, nil
}

// PreencheFatosEmSegundoPlano é o que main chama na subida: não segura o
// servidor; erro vira log.
func PreencheFatosEmSegundoPlano(d Deps) {
	go func() {
		if n, err := PreencheFatos(context.Background(), d); err != nil {
			log.Printf("base de fatos: %v", err)
		} else if n > 0 {
			log.Printf("base de fatos: %d partida(s) preenchida(s)", n)
		}
	}()
}

// GaranteFatos é garanteFatos para quem está fora do pacote (a IA monta o
// contexto e lê o resumo da partida pela mesma porta das rotas).
func GaranteFatos(ctx context.Context, d Deps, gameID int64) error {
	return garanteFatos(ctx, d, gameID)
}

// garanteFatos: no primeiro pedido de uma partida analisada sem resumo (ou de
// regra antiga), gera antes de responder.
func garanteFatos(ctx context.Context, d Deps, gameID int64) error {
	var v sql.NullInt64
	err := d.DB.QueryRow(`SELECT versao FROM resumos_partida WHERE game_id = ?`, gameID).Scan(&v)
	if err == nil && v.Valid && v.Int64 >= VersaoFatos {
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if _, err := partida.Le(d.DB, gameID); err != nil {
		return err
	}
	return GeraFatosDaPartida(ctx, d, gameID)
}

// --- Elo (registrado de fora: quem sabe é o pacote aprendizado) ----------------------

// EloConhecido é o elo que o app já tem guardado de um jogador.
type EloConhecido struct {
	Elo      string `json:"elo"`
	Divisao  string `json:"divisao"`
	PDL      int    `json:"pdl"`
	Vitorias int    `json:"vitorias"`
	Derrotas int    `json:"derrotas"`
	// EmSequencia: o "hot streak" da Riot (3+ vitórias seguidas na fila).
	EmSequencia bool   `json:"em_sequencia"`
	Fonte       string `json:"fonte"`
}

// FonteElo responde, pelo banco, o elo de um jogador (puuid do LCU) e o de uma
// partida. Sem o dado: ok falso. Nenhuma chamada de rede.
type FonteElo interface {
	EloDoJogador(db *banco.DB, puuid string) (EloConhecido, bool)
	EloDaPartida(db *banco.DB, gameID int64) (string, bool)
}

var fontesElo []FonteElo

// RegistraFonteElo: o pacote que guarda elo (aprendizado) se registra no init.
// Assim a análise lê o elo sem importar quem o guarda (aprendizado já importa
// a análise).
func RegistraFonteElo(f FonteElo) { fontesElo = append(fontesElo, f) }

func eloDoJogador(db *banco.DB, puuid string) (EloConhecido, bool) {
	for _, f := range fontesElo {
		if e, ok := f.EloDoJogador(db, puuid); ok {
			return e, true
		}
	}
	return EloConhecido{}, false
}

func eloDaPartida(db *banco.DB, gameID int64) string {
	for _, f := range fontesElo {
		if e, ok := f.EloDaPartida(db, gameID); ok {
			return e
		}
	}
	return ""
}

// --- Textos -----------------------------------------------------------------------

var nomesFuncao = map[string]string{"TOP": "topo", "JUNGLE": "caçador", "MIDDLE": "meio", "BOTTOM": "atirador", "UTILITY": "suporte"}

// NomeFuncao: "JUNGLE" → "caçador".
func NomeFuncao(f string) string {
	if n, ok := nomesFuncao[f]; ok {
		return n
	}
	if f == "" {
		return "sem função"
	}
	return strings.ToLower(f)
}

// NomeTime: 100 → "azul".
func NomeTime(t int) string {
	switch t {
	case 100:
		return "azul"
	case 200:
		return "vermelho"
	}
	return "nenhum"
}

// ouroTexto: 1234 → "1,2 mil"; 850 → "850".
func ouroTexto(v int) string {
	s := ""
	if v < 0 {
		s, v = "−", -v
	}
	if v >= 1000 {
		return s + strings.Replace(fmt.Sprintf("%.1f mil", float64(v)/1000), ".", ",", 1)
	}
	return s + fmt.Sprint(v)
}

func sinalOuro(v int) string {
	if v > 0 {
		return "+" + ouroTexto(v)
	}
	return ouroTexto(v)
}

func textoDaPartida(r ResumoPartida) string {
	az, ve := timeDoResumo(r, 100), timeDoResumo(r, 200)
	p := []string{fmt.Sprintf("partida %d", r.GameID)}
	if r.CriadaEm != "" && len(r.CriadaEm) >= 10 {
		p = append(p, r.CriadaEm[:10])
	}
	p = append(p, MinSeg(int64(r.DuracaoS)*1000), NomeTime(r.Vencedor)+" venceu")
	if r.Elo != "" {
		p = append(p, "elo "+r.Elo)
	}
	p = append(p, fmt.Sprintf("abates %d×%d", az.Abates, ve.Abates),
		fmt.Sprintf("dragões %d×%d", len(az.Dragoes)+az.Anciao, len(ve.Dragoes)+ve.Anciao),
		fmt.Sprintf("barões %d×%d", az.Baroes, ve.Baroes), fmt.Sprintf("torres %d×%d", az.Torres, ve.Torres))
	if r.MaiorVantagemAzul > 0 {
		p = append(p, fmt.Sprintf("maior vantagem do azul %s aos %s", ouroTexto(r.MaiorVantagemAzul), MinSeg(r.MaiorVantagemAzulMs)))
	}
	if r.MaiorVantagemVermelho > 0 {
		p = append(p, fmt.Sprintf("maior vantagem do vermelho %s aos %s", ouroTexto(r.MaiorVantagemVermelho), MinSeg(r.MaiorVantagemVermelhoMs)))
	}
	if r.Viradas > 0 {
		p = append(p, fmt.Sprintf("%d virada(s)", r.Viradas))
	}
	return strings.Join(p, " · ")
}

func ip(v int) *int { return &v }

// jsonMapa decodifica um JSON guardado (vazio ou inválido = mapa vazio).
func jsonMapa(s string) map[string]any {
	m := map[string]any{}
	json.Unmarshal([]byte(s), &m)
	return m
}
