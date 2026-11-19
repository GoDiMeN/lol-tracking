package rastreio

// A ORIGEM de uma partida rastreada: por que ela foi rastreada.
//
//   - "pesquisa": a pessoa buscou um jogador (tela Jogadores) e mandou rastrear
//     uma partida do histórico dele. Guarda QUAL jogador (o puuid do
//     pesquisado): a tela "Partidas rastreadas" mostra a partida do ponto de
//     vista dele e filtra por ele.
//   - "estudo": a partida entrou pelo estudo (coleta por elo, estudo de um
//     jogador, ranking). Guarda o jogador de referência. Partida só de estudo
//     NUNCA aparece em "Partidas rastreadas" nem na conversa geral com a IA:
//     ela é material das vozes, não partida do usuário.
//
// Uma partida pode ter as duas origens (e mais de um pesquisado). A tabela é
// do rastreio porque é ele quem sabe que a partida foi rastreada; o pacote
// aprendizado marca "estudo" antes de pedir o rastreio, o controller do
// rastreio marca "pesquisa" com o puuid que a tela do jogador manda.

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	"loltracking/server/banco"
)

// As duas origens.
const (
	OrigemPesquisa = "pesquisa"
	OrigemEstudo   = "estudo"
)

// SQLPartidasDePesquisa é a subconsulta das partidas que vieram da busca de um
// jogador. Outros pacotes (a busca da base de fatos para a conversa geral) a
// usam como `game_id IN (...)`: assim a regra fica num lugar só.
const SQLPartidasDePesquisa = `SELECT game_id FROM partidas_origem WHERE origem = 'pesquisa'`

func migraOrigem(db *banco.DB) error {
	return banco.Executa(db,
		`CREATE TABLE IF NOT EXISTS partidas_origem (
			game_id   INTEGER NOT NULL,
			origem    TEXT NOT NULL CHECK (origem IN ('pesquisa','estudo')),
			-- puuid: o pesquisado (pesquisa) ou o jogador de referência (estudo);
			-- vazio quando não se soube quem.
			puuid     TEXT NOT NULL DEFAULT '',
			-- como a linha nasceu: 'pedido' (no pedido do rastreio) ou
			-- 'preenchida' (dedução para as partidas de antes desta tabela).
			como      TEXT NOT NULL DEFAULT 'pedido',
			criado_em TEXT NOT NULL,
			PRIMARY KEY (game_id, origem, puuid)
		)`,
		`CREATE INDEX IF NOT EXISTS partidas_origem_puuid ON partidas_origem (origem, puuid)`,
	)
}

// MarcaOrigem grava uma origem da partida (repetir não duplica).
func MarcaOrigem(db *banco.DB, gameID int64, origem, puuid string) error {
	if origem != OrigemPesquisa && origem != OrigemEstudo {
		return fmt.Errorf("origem desconhecida: %q", origem)
	}
	_, err := db.Exec(`INSERT OR IGNORE INTO partidas_origem (game_id, origem, puuid, como, criado_em) VALUES (?, ?, ?, 'pedido', ?)`,
		gameID, origem, strings.TrimSpace(puuid), banco.Agora())
	return err
}

// MarcaOrigemDoParticipante grava a origem com o puuid do participante da
// partida (o jogador de referência do estudo).
func MarcaOrigemDoParticipante(db *banco.DB, gameID int64, origem string, pid int) error {
	var puuid string
	err := db.QueryRow(`SELECT puuid FROM participantes WHERE game_id = ? AND participante_id = ?`, gameID, pid).Scan(&puuid)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return MarcaOrigem(db, gameID, origem, puuid)
}

// OrigensDe são as origens gravadas da partida ("pesquisa", "estudo"), sem repetir.
func OrigensDe(db *banco.DB, gameID int64) ([]string, error) {
	linhas, err := db.Query(`SELECT DISTINCT origem FROM partidas_origem WHERE game_id = ? ORDER BY origem DESC`, gameID)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	r := []string{}
	for linhas.Next() {
		var o string
		if err := linhas.Scan(&o); err != nil {
			return nil, err
		}
		r = append(r, o)
	}
	return r, linhas.Err()
}

// PesquisadoProvavel deduz quem foi o jogador pesquisado de uma partida que
// não disse (pedido sem puuid, ou partida de antes da tabela): entre os
// participantes que já foram buscados na tela Jogadores (tabela jogadores),
//  1. o invocador da conta do cliente (conta), se ele está na partida;
//  2. senão, o buscado por último ANTES de `ate` (o momento do rastreio: quem
//     estava na tela quando o rastreio foi pedido);
//  3. senão, o mais buscado (empate: o buscado por último).
//
// "" = nenhum participante foi buscado.
func PesquisadoProvavel(db *banco.DB, gameID int64, conta, ate string) (string, error) {
	if !temTabela(db, "jogadores") {
		return "", nil
	}
	linhas, err := db.Query(`SELECT j.puuid, j.buscas, j.buscado_em, j.ordem FROM participantes p
		JOIN jogadores j ON j.puuid = p.puuid WHERE p.game_id = ? AND p.puuid <> ''`, gameID)
	if err != nil {
		return "", err
	}
	type cand struct {
		puuid, em     string
		buscas, ordem int
	}
	var cs []cand
	for linhas.Next() {
		var c cand
		if err := linhas.Scan(&c.puuid, &c.buscas, &c.em, &c.ordem); err != nil {
			linhas.Close()
			return "", err
		}
		cs = append(cs, c)
	}
	linhas.Close()
	if err := linhas.Err(); err != nil || len(cs) == 0 {
		return "", err
	}
	for _, c := range cs {
		if conta != "" && c.puuid == conta {
			return c.puuid, nil
		}
	}
	melhor := -1
	for i, c := range cs {
		if ate != "" && c.em > ate {
			continue
		}
		if melhor < 0 || c.em > cs[melhor].em || (c.em == cs[melhor].em && c.ordem > cs[melhor].ordem) {
			melhor = i
		}
	}
	if melhor >= 0 {
		return cs[melhor].puuid, nil
	}
	sort.SliceStable(cs, func(a, b int) bool {
		if cs[a].buscas != cs[b].buscas {
			return cs[a].buscas > cs[b].buscas
		}
		return cs[a].ordem > cs[b].ordem
	})
	return cs[0].puuid, nil
}

// temTabela: o preenchimento lê tabelas de outros pacotes (aprendizado,
// estudo), que um banco de teste do rastreio não tem.
func temTabela(db *banco.DB, nome string) bool {
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, nome).Scan(&n)
	return n > 0
}

// OrigemPreenchida: o que o preenchimento decidiu para uma partida (vai para o log).
type OrigemPreenchida struct {
	GameID int64
	Origem string
	Puuid  string
	Motivo string
}

// PreencheOrigens dá origem às partidas rastreadas que não têm nenhuma (as de
// antes da tabela, ou um pedido sem puuid em que não deu para deduzir quem):
//   - com linha em aprendizado_partidas (coleta por elo, ranking, estudo de
//     jogador da coleta) ou em estudo_partidas: "estudo", com o jogador de
//     referência;
//   - senão: "pesquisa", com o pesquisado deduzido por PesquisadoProvavel;
//   - nenhum participante buscado: "pesquisa" sem jogador (puuid vazio; a
//     tela mostra o primeiro rastreado).
//
// Roda na subida, depois de TODAS as migrações (main.go), e só mexe em partida
// sem origem: rodar de novo não muda nada do que já foi decidido.
func PreencheOrigens(db *banco.DB, conta string) ([]OrigemPreenchida, error) {
	linhas, err := db.Query(`SELECT r.game_id, MIN(r.criado_em) FROM rastreios r
		WHERE NOT EXISTS (SELECT 1 FROM partidas_origem o WHERE o.game_id = r.game_id)
		GROUP BY r.game_id ORDER BY r.game_id`)
	if err != nil {
		return nil, err
	}
	type pend struct {
		g  int64
		em string
	}
	var ps []pend
	for linhas.Next() {
		var p pend
		if err := linhas.Scan(&p.g, &p.em); err != nil {
			linhas.Close()
			return nil, err
		}
		ps = append(ps, p)
	}
	linhas.Close()
	if err := linhas.Err(); err != nil {
		return nil, err
	}
	aprendizado, estudos := temTabela(db, "aprendizado_partidas"), temTabela(db, "estudo_partidas")
	var feitas []OrigemPreenchida
	grava := func(g int64, origem, puuid, motivo string) error {
		if _, err := db.Exec(`INSERT OR IGNORE INTO partidas_origem (game_id, origem, puuid, como, criado_em) VALUES (?, ?, ?, 'preenchida', ?)`,
			g, origem, puuid, banco.Agora()); err != nil {
			return err
		}
		feitas = append(feitas, OrigemPreenchida{GameID: g, Origem: origem, Puuid: puuid, Motivo: motivo})
		return nil
	}
	for _, p := range ps {
		var refs []string
		if aprendizado {
			rs, err := colunaTexto(db, `SELECT DISTINCT COALESCE(pt.puuid, '') FROM aprendizado_partidas a
				LEFT JOIN participantes pt ON pt.game_id = a.game_id AND pt.participante_id = a.participante_id WHERE a.game_id = ?`, p.g)
			if err != nil {
				return feitas, err
			}
			refs = append(refs, rs...)
		}
		if estudos {
			rs, err := colunaTexto(db, `SELECT DISTINCT e.puuid FROM estudo_partidas ep JOIN estudos e ON e.id = ep.estudo_id WHERE ep.game_id = ?`, p.g)
			if err != nil {
				return feitas, err
			}
			refs = append(refs, rs...)
		}
		if len(refs) > 0 {
			for _, r := range refs {
				if err := grava(p.g, OrigemEstudo, r, "partida do estudo (coleta por elo / estudo)"); err != nil {
					return feitas, err
				}
			}
			continue
		}
		puuid, err := PesquisadoProvavel(db, p.g, conta, p.em)
		if err != nil {
			return feitas, err
		}
		motivo := "jogador buscado que está na partida"
		if puuid == "" {
			// Rastreada pela rota do usuário, mas sem ninguém buscado: é do
			// usuário, sem jogador em foco (a tela mostra o primeiro rastreado).
			motivo = "nenhum participante foi buscado na tela Jogadores"
		}
		if err := grava(p.g, OrigemPesquisa, puuid, motivo); err != nil {
			return feitas, err
		}
	}
	return feitas, nil
}

func colunaTexto(db *banco.DB, q string, args ...any) ([]string, error) {
	linhas, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	var r []string
	for linhas.Next() {
		var s string
		if err := linhas.Scan(&s); err != nil {
			return nil, err
		}
		r = append(r, s)
	}
	return r, linhas.Err()
}

// --- A lista de "Partidas rastreadas" ------------------------------------------

// Filtros da lista.
const (
	// OrigemTodas: qualquer origem (as telas que só precisam saber quem foi
	// rastreado numa partida: o mapa com play, a tela da partida).
	OrigemTodas = "todas"
)

// FiltroRastreadas: o que a lista pede.
type FiltroRastreadas struct {
	// Origem: pesquisa (padrão), estudo ou todas.
	Origem string
	// Puuid: só as partidas em que este jogador foi o pesquisado (pesquisa) ou
	// a referência (estudo); ele vira o jogador em foco.
	Puuid string
	// SemEstudo: o interruptor LOL_ESTUDO desligado. "todas" vira só pesquisa
	// (o controller recusa "estudo" antes).
	SemEstudo bool
}

// JogadorEmFoco: do ponto de vista de quem a linha é mostrada.
type JogadorEmFoco struct {
	Puuid          string `json:"puuid"`
	ParticipanteID int    `json:"participante_id"`
	RiotID         string `json:"riot_id"`
}

// JogadorPesquisado: um jogador da lista do filtro, com quantas partidas.
type JogadorPesquisado struct {
	Puuid    string `json:"puuid"`
	RiotID   string `json:"riot_id"`
	Partidas int    `json:"partidas"`
}

type origemLinha struct {
	origem, puuid, riot string
	pid                 int
	em                  string
}

// origensDasPartidas lê todas as origens com o participante e o Riot ID de cada puuid.
func origensDasPartidas(db *banco.DB) (map[int64][]origemLinha, error) {
	// O nome do pesquisado vem do que a tela Jogadores guardou (o mais novo) e,
	// sem ele (banco de teste sem a tabela), do participante.
	nomeJ, juntaJ := "NULL", ""
	if temTabela(db, "jogadores") {
		nomeJ, juntaJ = "NULLIF(j.nome, '') || '#' || j.tag", "LEFT JOIN jogadores j ON j.puuid = o.puuid"
	}
	linhas, err := db.Query(`SELECT o.game_id, o.origem, o.puuid, COALESCE(p.participante_id, 0),
		COALESCE(` + nomeJ + `, NULLIF(p.nome, '') || '#' || p.tag, ''), o.criado_em
		FROM partidas_origem o
		LEFT JOIN participantes p ON p.game_id = o.game_id AND p.puuid = o.puuid AND o.puuid <> ''
		` + juntaJ + `
		ORDER BY o.game_id, o.criado_em DESC, o.rowid DESC`)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	r := map[int64][]origemLinha{}
	for linhas.Next() {
		var g int64
		var o origemLinha
		if err := linhas.Scan(&g, &o.origem, &o.puuid, &o.pid, &o.riot, &o.em); err != nil {
			return nil, err
		}
		r[g] = append(r[g], o)
	}
	return r, linhas.Err()
}

// FiltraRastreadas aplica a origem e o jogador à lista de partidas com passe
// concluído, preenche origens/pesquisados/foco de cada linha e devolve também
// os jogadores pesquisados (com contagem) para o filtro da tela.
func FiltraRastreadas(db *banco.DB, f FiltroRastreadas) ([]PartidaRastreada, []JogadorPesquisado, error) {
	origem := f.Origem
	if origem == "" {
		origem = OrigemPesquisa
	}
	if f.SemEstudo && origem == OrigemTodas {
		origem = OrigemPesquisa
	}
	todas, err := ListaRastreadas(db)
	if err != nil {
		return nil, nil, err
	}
	ors, err := origensDasPartidas(db)
	if err != nil {
		return nil, nil, err
	}
	var lista []PartidaRastreada
	contagem := map[string]*JogadorPesquisado{}
	var ordem []string
	for _, p := range todas {
		ol := ors[p.GameID]
		p.Origens, p.Pesquisados = []string{}, []string{}
		temP, temE := false, false
		var focoPesq, focoEst, focoPedido *origemLinha
		for i := range ol {
			o := &ol[i]
			switch o.origem {
			case OrigemPesquisa:
				temP = true
				if o.puuid != "" && !contemTexto(p.Pesquisados, o.puuid) {
					p.Pesquisados = append(p.Pesquisados, o.puuid)
				}
				if focoPesq == nil && o.pid > 0 {
					focoPesq = o
				}
			case OrigemEstudo:
				temE = true
				if focoEst == nil && o.pid > 0 {
					focoEst = o
				}
			}
			if f.Puuid != "" && o.puuid == f.Puuid && o.pid > 0 && (origem == OrigemTodas || o.origem == origem) && focoPedido == nil {
				focoPedido = o
			}
		}
		if temP {
			p.Origens = append(p.Origens, OrigemPesquisa)
		}
		if temE {
			p.Origens = append(p.Origens, OrigemEstudo)
		}
		// Os jogadores do filtro contam as partidas de pesquisa (o filtro é da
		// tela "Partidas rastreadas"), antes do filtro por jogador.
		if temP {
			for _, pu := range p.Pesquisados {
				j, ok := contagem[pu]
				if !ok {
					j = &JogadorPesquisado{Puuid: pu}
					contagem[pu] = j
					ordem = append(ordem, pu)
				}
				j.Partidas++
				for _, o := range ol {
					if o.puuid == pu && o.riot != "" {
						j.RiotID = o.riot
						break
					}
				}
			}
		}
		switch origem {
		case OrigemPesquisa:
			if !temP {
				continue
			}
		case OrigemEstudo:
			if !temE {
				continue
			}
		case OrigemTodas:
			if f.SemEstudo && !temP {
				continue
			}
		}
		if f.Puuid != "" && focoPedido == nil {
			continue
		}
		foco := focoPedido
		if foco == nil {
			if origem == OrigemEstudo {
				foco = focoEst
			} else {
				foco = focoPesq
				if foco == nil && origem == OrigemTodas {
					foco = focoEst
				}
			}
		}
		if foco != nil {
			p.Foco = &JogadorEmFoco{Puuid: foco.puuid, ParticipanteID: foco.pid, RiotID: foco.riot}
		}
		if f.SemEstudo {
			// Com o estudo desligado a linha não diz que a partida também é de estudo.
			p.Origens = []string{OrigemPesquisa}
		}
		lista = append(lista, p)
	}
	jogs := make([]JogadorPesquisado, 0, len(ordem))
	for _, pu := range ordem {
		j := *contagem[pu]
		if j.RiotID == "" {
			j.RiotID = pu
		}
		jogs = append(jogs, j)
	}
	sort.SliceStable(jogs, func(a, b int) bool {
		if jogs[a].Partidas != jogs[b].Partidas {
			return jogs[a].Partidas > jogs[b].Partidas
		}
		return strings.ToLower(jogs[a].RiotID) < strings.ToLower(jogs[b].RiotID)
	})
	return lista, jogs, nil
}

func contemTexto(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}
