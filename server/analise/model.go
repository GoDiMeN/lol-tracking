package analise

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"loltracking/server/arquivo"
	"loltracking/server/banco"
	"loltracking/server/campeao"
	"loltracking/server/partida"
	"loltracking/server/rastreio"
	"loltracking/server/riot"
)

// Migra cria as tabelas da análise. Todas são RECALCULÁVEIS (Recalcula apaga
// e refaz as linhas da partida), menos a linha do tempo da Riot, que é cópia
// do que a Riot devolveu (não muda depois da partida). Ver dados.md.
func Migra(db *banco.DB) error {
	if err := converteFormatoAntigo(db); err != nil {
		return fmt.Errorf("converter a análise para o formato de arquivos: %w", err)
	}
	if err := migraQualidade(db); err != nil {
		return err
	}
	if err := migraFatos(db); err != nil {
		return err
	}
	return banco.Executa(db,
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
// Estaticos também (nil = a base de fatos sai sem nome e custo dos itens).
type Deps struct {
	DB        *banco.DB
	Riot      rastreio.FonteRiot
	Estaticos campeao.FonteEstaticos
	// SemEstudo: o interruptor LOL_ESTUDO desligado. A busca da base de fatos
	// (/api/resumos) só vê as partidas que vieram da busca de um jogador.
	SemEstudo bool
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

	// 0. A visão dos times (passes de visão), de novo: ela depende das posições
	// rastreadas, que mudam com passe novo ou repescado; a cadeia (1) lê dela.
	if _, err := rastreio.RecalculaVisao(db, gameID); err != nil {
		r.Avisos = append(r.Avisos, "a visão dos times não foi refeita: "+err.Error())
	}

	// 1. A cadeia de cada passe, de novo (com o contexto de agora).
	passes, err := rastreio.PassesCompactados(db, gameID)
	if err != nil {
		return r, err
	}
	// Passe com o arquivo faltando ou ilegível não segura os outros: vira aviso, e a checagem de
	// qualidade (1b) aponta qual arquivo é.
	for _, p := range passes {
		if _, err := rastreio.RegeraCadeia(db, p.ID); err != nil {
			r.Avisos = append(r.Avisos, fmt.Sprintf("a cadeia do passe %d não foi refeita: %v", p.ID, err))
		}
	}
	r.Passes = len(passes)

	// 1b. A checagem de qualidade do dado e do armazenamento de cada passe (qualidade.go): só
	// aponta, não muda nada. Gravada já aqui, para valer mesmo se o resto falhar.
	var qs map[int64]*QualidadePasse
	todos, errTodos := rastreio.PassesDaPartida(db, gameID)
	if pt, err := partida.Le(db, gameID); err == nil && errTodos == nil {
		timeDe := map[int]int{}
		for _, x := range pt.Participantes {
			timeDe[x.ParticipanteID] = x.Time
		}
		if qs, err = confereQualidadeDaPartida(db, gameID, todos, timeDe); err != nil {
			r.Avisos = append(r.Avisos, "a checagem de qualidade do dado não rodou: "+err.Error())
			qs = nil
		}
	}

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

	// 3b. A checagem de qualidade da análise (qualidade_analise.go), por cima da do dado.
	if qs != nil {
		if err := confereAnaliseDaPartida(db, gameID, dp, retratos, todos, qs); err != nil {
			r.Avisos = append(r.Avisos, "a checagem de qualidade da análise não rodou: "+err.Error())
		}
	}

	tx, err := db.Begin()
	if err != nil {
		return r, err
	}
	defer tx.Rollback()
	agora := banco.Agora()
	r.CalculadaEm = agora
	if err := gravaRetratos(db, gameID, retratos); err != nil {
		return r, err
	}
	if _, err := tx.Exec(`DELETE FROM indicadores WHERE game_id = ?`, gameID); err != nil {
		return r, err
	}
	for _, in := range inds {
		if _, err := tx.Exec(`INSERT INTO indicadores (game_id, participante_id, indicador, valor, aproximado, detalhe, calculado_em)
			VALUES (?,?,?,?,?,?,?)`, gameID, in.ParticipanteID, in.Indicador, in.Valor, in.Aproximado, jsonTexto(in.Detalhe), agora); err != nil {
			return r, err
		}
	}
	// 4. A base de fatos (fatos.go): calculada aqui, com os dados já carregados, e
	// gravada depois do commit (é derivada; se falhar, a análise continua valendo).
	fatos := geraFatos(ctx, d, dp, retratos)
	r.Ms = time.Since(inicio).Milliseconds()
	if _, err := tx.Exec(`INSERT INTO analises (game_id, calculada_em, rastreados, passes, riot, abates, indicadores, avisos, ms)
		VALUES (?,?,?,?,?,?,?,?,?) ON CONFLICT (game_id) DO UPDATE SET calculada_em = excluded.calculada_em,
		rastreados = excluded.rastreados, passes = excluded.passes, riot = excluded.riot, abates = excluded.abates,
		indicadores = excluded.indicadores, avisos = excluded.avisos, ms = excluded.ms`,
		gameID, agora, jsonTexto(r.Rastreados), r.Passes, r.Riot, r.Abates, r.Indicadores, jsonTexto(r.Avisos), r.Ms); err != nil {
		return r, err
	}
	if err := tx.Commit(); err != nil {
		return r, err
	}
	if err := gravaFatos(db, fatos); err != nil {
		log.Printf("base de fatos da partida %d: %v", gameID, err)
	}
	return r, nil
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

// Os retratos dos abates e a linha do tempo da Riot são pesados e ficam em
// arquivo (pacote arquivo), não no banco:
//
//	retratos.jsonl   um retrato por linha (JSON), na ordem dos abates
//	linha-riot.json  o JSON cru da match-v5 timeline
const (
	arquivoRetratos  = "retratos.jsonl"
	arquivoLinhaRiot = "linha-riot.json"
)

func gravaRetratos(db *banco.DB, gameID int64, retratos []Retrato) error {
	var b bytes.Buffer
	for _, rt := range retratos {
		b.WriteString(jsonTexto(rt))
		b.WriteByte('\n')
	}
	if _, err := arquivo.Grava(db, gameID, arquivoRetratos, b.Bytes()); err != nil {
		return fmt.Errorf("gravar os retratos: %w", err)
	}
	return nil
}

// GravaLinhaRiot guarda o JSON cru da match-v5 timeline.
func GravaLinhaRiot(db *banco.DB, gameID int64, bruto []byte) error {
	_, err := arquivo.Grava(db, gameID, arquivoLinhaRiot, bruto)
	return err
}

// LeLinhaRiot devolve a linha do tempo da Riot guardada.
func LeLinhaRiot(db *banco.DB, gameID int64) ([]byte, bool, error) {
	return arquivo.Le(db, gameID, arquivoLinhaRiot)
}

// converteFormatoAntigo roda uma vez num banco de antes dos arquivos: leva os
// retratos e a linha do tempo da Riot para arquivo e REMOVE as duas tabelas.
func converteFormatoAntigo(db *banco.DB) error {
	existe := func(tabela string) (bool, error) {
		var tem bool
		err := db.QueryRow(`SELECT COUNT(*) > 0 FROM sqlite_master WHERE type = 'table' AND name = ?`, tabela).Scan(&tem)
		return tem, err
	}
	if tem, err := existe("retratos_abate"); err != nil {
		return err
	} else if tem {
		porPartida := map[int64]*bytes.Buffer{}
		linhas, err := db.Query(`SELECT game_id, dados FROM retratos_abate ORDER BY game_id, n`)
		if err != nil {
			return err
		}
		for linhas.Next() {
			var g int64
			var dados string
			if err := linhas.Scan(&g, &dados); err != nil {
				linhas.Close()
				return err
			}
			if porPartida[g] == nil {
				porPartida[g] = &bytes.Buffer{}
			}
			porPartida[g].WriteString(dados)
			porPartida[g].WriteByte('\n')
		}
		linhas.Close()
		if err := linhas.Err(); err != nil {
			return err
		}
		for g, b := range porPartida {
			if _, err := arquivo.Grava(db, g, arquivoRetratos, b.Bytes()); err != nil {
				return err
			}
		}
		if _, err := db.Exec(`DROP TABLE retratos_abate`); err != nil {
			return err
		}
	}
	if tem, err := existe("linhas_do_tempo_riot"); err != nil {
		return err
	} else if tem {
		var ids []int64
		linhas, err := db.Query(`SELECT game_id FROM linhas_do_tempo_riot`)
		if err != nil {
			return err
		}
		for linhas.Next() {
			var g int64
			if err := linhas.Scan(&g); err != nil {
				linhas.Close()
				return err
			}
			ids = append(ids, g)
		}
		linhas.Close()
		for _, g := range ids {
			var bruto string
			if err := db.QueryRow(`SELECT bruto FROM linhas_do_tempo_riot WHERE game_id = ?`, g).Scan(&bruto); err != nil {
				return err
			}
			if _, err := arquivo.Grava(db, g, arquivoLinhaRiot, []byte(bruto)); err != nil {
				return err
			}
		}
		if _, err := db.Exec(`DROP TABLE linhas_do_tempo_riot`); err != nil {
			return err
		}
		if _, err := db.Exec(`VACUUM`); err != nil {
			return err
		}
	}
	return nil
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

// LeFases devolve as fases da partida (FasesDaPartida) pela linha do tempo do
// LCU guardada. Não depende de rastreio nem de a análise ter rodado.
func LeFases(db *banco.DB, gameID int64) ([]TrechoFase, int64, error) {
	pt, err := partida.Le(db, gameID)
	if err != nil {
		return nil, 0, err
	}
	bruto, err := partida.LeLinhaDoTempo(db, gameID)
	if errors.Is(err, partida.ErrNaoEncontrada) {
		return nil, 0, ErrSemLinhaDoTempo
	}
	if err != nil {
		return nil, 0, err
	}
	lt, err := rastreio.LeLinhaDoTempo(bruto)
	if err != nil {
		return nil, 0, err
	}
	duracao := int64(pt.DuracaoS) * 1000
	return FasesDaPartida(lt.Eventos, duracao), duracao, nil
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
		dp.eventosRiot = eventosRiot(rb)
		dp.eventosSentinela = sentinelasDosEventos(dp.eventosRiot)
	}
	if v, ok, err := rastreio.LeVisao(db, gameID); err != nil {
		return nil, err
	} else if ok {
		dp.visao = v
	}
	return dp, nil
}
