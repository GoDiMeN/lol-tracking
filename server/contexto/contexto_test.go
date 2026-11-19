package contexto

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"loltracking/server/analise"
	"loltracking/server/aprendizado"
	"loltracking/server/banco"
	"loltracking/server/campeao"
	"loltracking/server/ia"
	"loltracking/server/lcu"
	"loltracking/server/partida"
	"loltracking/server/ranking"
	"loltracking/server/web"
)

// Os testes usam o LCU falso e a Riot falsa (a match-v5 traduzida do LCU
// falso): nada sai para a rede, nada toca no LoL de verdade.

type amb struct {
	db   *banco.DB
	lcu  *lcu.Falso
	riot *aprendizado.RiotFalso
	hub  *web.Hub
	s    *Servico
	mux  *http.ServeMux
}

func prepara(t *testing.T, comCliente, comRiot bool) *amb {
	t.Helper()
	db, err := banco.AbreMemoria(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, m := range []func(*banco.DB) error{partida.Migra, analise.Migra, ranking.Migra, ia.Migra} {
		if err := m(db); err != nil {
			t.Fatal(err)
		}
	}
	a := &amb{db: db, lcu: lcu.NovoFalso(), riot: aprendizado.NovoRiotFalso(), hub: web.NovoHub()}
	d := Deps{DB: db, Hub: a.hub, Analise: analise.Deps{DB: db}}
	if comCliente {
		d.LCU = a.lcu
	}
	if comRiot {
		d.Riot = a.riot
	}
	a.s = Novo(d)
	a.mux = http.NewServeMux()
	web.RegistraExecucoes(a.mux, a.hub)
	Registra(a.mux, a.s)
	return a
}

func (a *amb) pede(t *testing.T, metodo, caminho, corpo string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	a.mux.ServeHTTP(rec, httptest.NewRequest(metodo, caminho, strings.NewReader(corpo)))
	var m map[string]any
	json.Unmarshal(rec.Body.Bytes(), &m)
	return rec.Code, m
}

// partidaDoRef grava no banco a i-ésima partida (0 = a mais nova) do jogador de
// referência GOLD n do LCU falso e devolve o id e o participante dele.
func (a *amb) partidaDoRef(t *testing.T, n, i int) (int64, int) {
	t.Helper()
	ctx := context.Background()
	ref := lcu.ReferenciaFalsa(1, n)
	bruto, err := a.lcu.Historico(ctx, ref.Puuid, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var h struct {
		Games struct {
			Games []struct {
				GameID       int64 `json:"gameId"`
				Participants []struct {
					ParticipantID int `json:"participantId"`
				} `json:"participants"`
			} `json:"games"`
		} `json:"games"`
	}
	json.Unmarshal(bruto, &h)
	g := h.Games.Games[i]
	inteira, err := a.lcu.Partida(ctx, g.GameID)
	if err != nil {
		t.Fatal(err)
	}
	if err := partida.Grava(ctx, partida.Deps{DB: a.db, LCU: a.lcu, Campeoes: campeao.Fixo(lcu.CampeoesFalsos)}, inteira, true); err != nil {
		t.Fatal(err)
	}
	return g.GameID, g.Participants[0].ParticipantID
}

func (a *amb) busca(t *testing.T, gameID int64, corpo string) (map[string]any, []web.Evento) {
	t.Helper()
	c, m := a.pede(t, "POST", "/api/partidas/"+itoa(gameID)+"/contexto-jogadores", corpo)
	if c != 202 {
		t.Fatal(c, m)
	}
	id := m["execucao"].(map[string]any)["id"].(string)
	e, _ := a.hub.Busca(id)
	for i := 0; i < 500 && !e.Fechada(); i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if !e.Fechada() {
		t.Fatal("a busca não terminou")
	}
	return m, e.Eventos()
}

func itoa(g int64) string { return strconv.FormatInt(g, 10) }

func eventos(evs []web.Evento, tipo string) []map[string]any {
	var l []map[string]any
	for _, e := range evs {
		if e.Tipo == tipo {
			var m map[string]any
			json.Unmarshal(e.Dados, &m)
			l = append(l, m)
		}
	}
	return l
}

// Pelo cliente do LoL: o histórico de ANTES da partida, só Summoner's Rift, e o
// contexto do jogador de referência (mono, função principal) sai dele.
func TestContextoPeloCliente(t *testing.T) {
	a := prepara(t, true, false)
	gameID, pid := a.partidaDoRef(t, 0, 5)
	pt, _ := partida.Le(a.db, gameID)

	c, m := a.pede(t, "GET", "/api/partidas/"+itoa(gameID)+"/contexto-jogadores", "")
	if c != 200 || len(m["faltam"].([]any)) != 10 || m["fontes"].(map[string]any)["cliente"] != true || m["fontes"].(map[string]any)["riot"] != false {
		t.Fatal(c, m)
	}
	r, evs := a.busca(t, gameID, "")
	if r["fonte"] != FonteCliente {
		t.Fatal(r)
	}
	conc := eventos(evs, "concluido")
	if len(conc) != 1 || len(eventos(evs, "jogador")) != 10 {
		t.Fatalf("%+v", evs)
	}
	// 10 históricos pedidos ao cliente (1 por jogador; o falso responde de primeira).
	if p := conc[0]["pedidos"].(map[string]any); p["lcu"].(float64) != 10 {
		t.Fatalf("pedidos: %v", p)
	}
	hs, err := analise.LeHistoricosExternos(a.db, gameID)
	if err != nil || len(hs) != 10 {
		t.Fatal(hs, err)
	}
	h := hs[pid]
	if h.Fonte != FonteCliente || h.Estado != analise.HistOK || len(h.Jogos) != PartidasPorJogador || h.BuscadoEm == "" {
		t.Fatalf("%+v", h)
	}
	for _, j := range h.Jogos {
		if j.CriadaMs >= pt.CriadaEmMs || !ranking.FilasSR[j.Fila] || j.GameID == 0 {
			t.Fatalf("partida de depois ou fora de Summoner's Rift: %+v", j)
		}
	}
	// Os outros 9 não têm histórico no falso: lista vazia, mas buscada.
	for p, x := range hs {
		if p != pid && (x.Estado != analise.HistOK || len(x.Jogos) != 0) {
			t.Fatalf("%d: %+v", p, x)
		}
	}
	ctxs, err := analise.ContextoDosJogadores(a.db, gameID)
	if err != nil {
		t.Fatal(err)
	}
	cx := ctxs[pid]
	if cx.FuncaoPrincipal != "TOP" || cx.FracaoPrincipal != 1 || cx.ForaDaFuncao == nil || *cx.ForaDaFuncao || cx.Ultimas != 10 ||
		cx.CampeaoDe != 10 || cx.Mono == nil || !*cx.Mono || cx.Fonte != FonteCliente || cx.BuscadoEm == "" {
		t.Fatalf("%+v", cx)
	}
	// O GET passa a dizer que não falta ninguém, com o contexto calculado.
	_, m = a.pede(t, "GET", "/api/partidas/"+itoa(gameID)+"/contexto-jogadores", "")
	if len(m["faltam"].([]any)) != 0 || len(m["jogadores"].([]any)) != 10 || m["contextos"].(map[string]any)[itoa(int64(pid))] == nil {
		t.Fatal(m)
	}
}

// A partida mais velha do histórico do cliente: as 25 que ele tem são todas
// de depois dela. O contexto diz isso e não usa partida posterior.
func TestContextoQueNaoAlcanca(t *testing.T) {
	a := prepara(t, true, false)
	gameID, pid := a.partidaDoRef(t, 0, 24)
	a.busca(t, gameID, `{"fonte":"cliente"}`)
	hs, _ := analise.LeHistoricosExternos(a.db, gameID)
	if h := hs[pid]; h.Estado != analise.HistNaoAlcanca || len(h.Jogos) != 0 || h.Vistas != 25 {
		t.Fatalf("%+v", h)
	}
	ctxs, _ := analise.ContextoDosJogadores(a.db, gameID)
	if cx := ctxs[pid]; !strings.Contains(cx.Alcance, "não chega antes desta partida") || cx.Ultimas != 0 || cx.FuncaoPrincipal != "" {
		t.Fatalf("%+v", cx)
	}
}

// Pela match-v5 (sem cliente): os puuids da Riot pela própria partida, os ids
// com endTime, o cache do ranking e o elo; o mesmo contexto que o cliente deu.
func TestContextoPelaRiot(t *testing.T) {
	a := prepara(t, false, true)
	gameID, pid := a.partidaDoRef(t, 0, 5)
	_, evs := a.busca(t, gameID, "")
	conc := eventos(evs, "concluido")
	if len(conc) != 1 || conc[0]["fonte"] != FonteRiot {
		t.Fatalf("%+v", evs)
	}
	hs, _ := analise.LeHistoricosExternos(a.db, gameID)
	h := hs[pid]
	if h.Fonte != FonteRiot || h.Estado != analise.HistOK || len(h.Jogos) != PartidasPorJogador {
		t.Fatalf("%+v", h)
	}
	// Os outros 9 não existem na Riot falsa.
	for p, x := range hs {
		if p != pid && x.Estado != analise.HistNaoAchado {
			t.Fatalf("%d: %+v", p, x)
		}
	}
	// 1 pedido da partida + 1 de ids por jogador achado + as partidas dele;
	// os 9 não achados custam 1 de ids cada; nada passa do teto.
	if a.riot.PedidosIDs != 10 || a.riot.PedidosPartidas < 1+PartidasPorJogador || a.riot.PedidosPartidas > 1+TetoPartidasRiotPorJogador {
		t.Fatalf("ids %d partidas %d", a.riot.PedidosIDs, a.riot.PedidosPartidas)
	}
	// O elo de cada um (Riot falsa: só o de referência tem ranqueada).
	ctxs, _ := analise.ContextoDosJogadores(a.db, gameID)
	cx := ctxs[pid]
	if cx.Elo == nil || cx.Elo.Elo != "GOLD" || cx.FuncaoPrincipal != "TOP" || cx.Mono == nil || !*cx.Mono || cx.Ultimas != 10 {
		t.Fatalf("%+v", cx)
	}

	// A mesma partida pelo cliente dá o mesmo contexto (as duas fontes concordam).
	b := prepara(t, true, false)
	gb, pb := b.partidaDoRef(t, 0, 5)
	b.busca(t, gb, "")
	cb, _ := analise.ContextoDosJogadores(b.db, gb)
	x := cb[pb]
	if x.Sequencia != cx.Sequencia || x.VitoriasUltimas != cx.VitoriasUltimas || x.CampeaoVezes != cx.CampeaoVezes {
		t.Fatalf("cliente %+v\nriot %+v", x, cx)
	}

	// Buscar de novo: a partida da Riot e as dele vêm do cache do ranking.
	antes := a.riot.PedidosPartidas
	a.busca(t, gameID, `{"fonte":"riot"}`)
	if a.riot.PedidosPartidas != antes {
		t.Fatalf("o cache não valeu: %d → %d", antes, a.riot.PedidosPartidas)
	}
}

// Fonte indisponível: 409 na hora (nada roda); partida que não existe: 404.
func TestContextoSemFonte(t *testing.T) {
	a := prepara(t, true, true)
	gameID, _ := a.partidaDoRef(t, 0, 5)
	a.lcu.Fechado = true
	a.riot.Recusada = true
	if c, m := a.pede(t, "POST", "/api/partidas/"+itoa(gameID)+"/contexto-jogadores", ""); c != 409 || !strings.Contains(m["erro"].(string), "fechado") {
		t.Fatal(c, m)
	}
	if c, _ := a.pede(t, "POST", "/api/partidas/"+itoa(gameID)+"/contexto-jogadores", `{"fonte":"outra"}`); c != 400 {
		t.Fatal(c)
	}
	if c, _ := a.pede(t, "POST", "/api/partidas/123/contexto-jogadores", ""); c != 404 {
		t.Fatal(c)
	}
	// A IA: sem fonte, só avisa e segue.
	var avisos []string
	if a.s.GaranteParaIA(context.Background(), gameID, func(tipo string, d any) {
		avisos = append(avisos, tipo)
	}) || len(avisos) != 1 {
		t.Fatal(avisos)
	}
}

// Antes da análise com IA: busca quem falta (uma vez); com tudo buscado, nada.
func TestGaranteParaIA(t *testing.T) {
	a := prepara(t, true, false)
	gameID, _ := a.partidaDoRef(t, 0, 5)
	n := 0
	emite := func(tipo string, d any) {
		if tipo == "contexto_jogadores" {
			n++
		}
	}
	if !a.s.GaranteParaIA(context.Background(), gameID, emite) || n == 0 {
		t.Fatal("devia buscar")
	}
	n = 0
	if a.s.GaranteParaIA(context.Background(), gameID, emite) || n != 0 {
		t.Fatal("já buscado: não busca de novo")
	}
}
