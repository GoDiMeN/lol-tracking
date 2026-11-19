package ranking_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"loltracking/server/aprendizado"
	"loltracking/server/banco"
	"loltracking/server/campeao"
	"loltracking/server/lcu"
	"loltracking/server/ranking"
	"loltracking/server/web"
)

// Os testes de ponta a ponta usam a Riot falsa do aprendizado (liga, conta e a
// match-v5 traduzida do LCU falso) e o LCU falso: nada sai para a rede.

type amb struct {
	db   *banco.DB
	riot *aprendizado.RiotFalso
	a    *ranking.Atualizador
	mux  *http.ServeMux
}

func prepara(t *testing.T, fonte string, semCliente bool) *amb {
	t.Helper()
	db, err := banco.AbreMemoria(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := ranking.Migra(db); err != nil {
		t.Fatal(err)
	}
	x := &amb{db: db, riot: aprendizado.NovoRiotFalso()}
	d := ranking.Deps{DB: db, Riot: x.riot, Hub: web.NovoHub(), FontePadrao: fonte, Campeoes: campeao.Fixo(lcu.CampeoesFalsos)}
	if !semCliente {
		d.LCU = lcu.NovoFalso()
	}
	x.a = ranking.Novo(d)
	x.mux = http.NewServeMux()
	web.RegistraExecucoes(x.mux, d.Hub)
	ranking.Registra(x.mux, x.a)
	return x
}

func (x *amb) pede(t *testing.T, metodo, caminho, corpo string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	x.mux.ServeHTTP(rec, httptest.NewRequest(metodo, caminho, strings.NewReader(corpo)))
	var m map[string]any
	json.Unmarshal(rec.Body.Bytes(), &m)
	return rec.Code, m
}

// atualiza roda uma atualização até o fim e devolve o id.
func (x *amb) atualiza(t *testing.T, corpo string) int64 {
	t.Helper()
	c, m := x.pede(t, "POST", "/api/rankings/atualizacoes", corpo)
	if c != 202 {
		t.Fatal(c, m)
	}
	x.a.Espera()
	id := int64(m["atualizacao"].(map[string]any)["id"].(float64))
	at, err := ranking.LeAtualizacao(x.db, id)
	if err != nil || at.Estado != ranking.Concluido {
		t.Fatalf("%+v %v", at, err)
	}
	return id
}

// ordem: os puuids e notas de um ranking num modo.
func ordem(t *testing.T, db *banco.DB, patch, elo, rota, modo string) ([]string, []float64, ranking.Ranking) {
	t.Helper()
	r, err := ranking.Ultimo(db, "BR1", patch, elo, rota, 0)
	if err != nil {
		t.Fatal(rota, err)
	}
	ps, err := ranking.Posicoes(db, r.ID, modo)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	var notas []float64
	for _, p := range ps {
		ids = append(ids, p.Puuid)
		notas = append(notas, p.Nota)
	}
	return ids, notas, r
}

var patchFalso = lcu.PatchDe(lcu.VersaoFalsa)

// As duas fontes de histórico (cliente do LoL e match-v5) dão o mesmo ranking.
func TestDuasFontesMesmoRanking(t *testing.T) {
	porLCU := prepara(t, ranking.FonteLCU, false)
	porRiot := prepara(t, ranking.FonteRiot, true)
	porLCU.atualiza(t, `{"elo":"GOLD"}`)
	porRiot.atualiza(t, `{"elo":"GOLD"}`)
	for _, rota := range ranking.Rotas {
		for _, modo := range ranking.Modos {
			a, na, ra := ordem(t, porLCU.db, patchFalso, "GOLD", rota, modo)
			b, nb, rb := ordem(t, porRiot.db, patchFalso, "GOLD", rota, modo)
			if strings.Join(a, ",") != strings.Join(b, ",") || len(na) != len(nb) {
				t.Fatalf("%s/%s: lcu %v, riot %v", rota, modo, a, b)
			}
			for i := range na {
				if na[i] != nb[i] {
					t.Fatalf("%s/%s: notas %v e %v", rota, modo, na, nb)
				}
			}
			if ra.Fonte != ranking.FonteLCU || rb.Fonte != ranking.FonteRiot || ra.Patch != patchFalso || ra.VersaoRegra != ranking.VersaoRegra {
				t.Fatalf("%+v %+v", ra, rb)
			}
		}
	}
	// Os de referência jogam sempre a mesma rota (n%5): 3 por rota; o suporte
	// (n = 4, 9, 14) só tem partida de patch antigo e fica vazio.
	top, _, r := ordem(t, porRiot.db, patchFalso, "GOLD", "TOP", ranking.ModoSubindo)
	if len(top) != 3 || r.Amostra != 3 || r.EloInteiro || r.PaginasLidas != 2 || !strings.Contains(r.DescricaoAmostra, "AMOSTRA de 2 páginas") {
		t.Fatalf("%v %+v", top, r)
	}
	sup, _, rs := ordem(t, porRiot.db, patchFalso, "GOLD", "UTILITY", ranking.ModoSubindo)
	if len(sup) != 0 || rs.Fora[ranking.ForaSemPatch] != 3 || rs.Fora[ranking.ForaNaoAchado] != 4 || rs.Fora[ranking.ForaInativo] != 1 {
		t.Fatalf("%v %+v", sup, rs.Fora)
	}
	// Ranking de campeão só com 2+ jogadores qualificados.
	recs, err := ranking.Recortes(porLCU.db, "BR1", patchFalso, "GOLD")
	if err != nil {
		t.Fatal(err)
	}
	for _, rk := range recs {
		if rk.Tipo == ranking.TipoCampeao && rk.Amostra < ranking.MinJogadoresCampeao {
			t.Fatalf("%+v", rk)
		}
	}
}

func TestTelaDoRanking(t *testing.T) {
	x := prepara(t, ranking.FonteLCU, false)
	x.atualiza(t, `{"elo":"CHALLENGER","candidatos":20}`)
	c, m := x.pede(t, "GET", "/api/rankings/recorte?patch="+patchFalso+"&elo=CHALLENGER&rota=JUNGLE&modo=melhor", "")
	if c != 200 {
		t.Fatal(c, m)
	}
	rk := m["ranking"].(map[string]any)
	if rk["elo_inteiro"] != true || rk["teto_candidatos"].(float64) != 20 || !strings.Contains(rk["descricao_amostra"].(string), "a liga inteira de Desafiante") {
		t.Fatal(rk)
	}
	ps := m["posicoes"].([]any)
	if len(ps) == 0 {
		t.Fatal("selva vazia")
	}
	p := ps[0].(map[string]any)
	comps := p["componentes"].([]any)
	if len(comps) != len(ranking.Pesos[ranking.TipoRota][ranking.ModoMelhor]) || p["frase"] == "" || p["campeao"] == "" {
		t.Fatal(p)
	}
	if f := m["formula"].(map[string]any); !strings.HasPrefix(f["texto"].(string), "nota = 100 × (0,30 × posição na liga") {
		t.Fatal(f["texto"])
	}
	// O índice lista o patch e os recortes.
	if c, m := x.pede(t, "GET", "/api/rankings?elo=CHALLENGER", ""); c != 200 || m["patch"] != patchFalso || len(m["recortes"].([]any)) < 5 {
		t.Fatal(c, m)
	}
	if c, m := x.pede(t, "GET", "/api/rankings/formula", ""); c != 200 || m["versao_regra"].(float64) != ranking.VersaoRegra {
		t.Fatal(c, m)
	}
	if c, m := x.pede(t, "GET", "/api/rankings/orcamento?elo=PLATINUM&fonte=riot", ""); c != 200 || m["pedidos_regional"].(float64) != 1320 {
		t.Fatal(c, m)
	}
	for _, caminho := range []string{
		"/api/rankings/recorte?patch=" + patchFalso + "&elo=CHALLENGER&rota=XYZ",
		"/api/rankings/recorte?patch=" + patchFalso + "&elo=CHALLENGER&rota=TOP&modo=outro",
		"/api/rankings/orcamento?elo=FERRO",
	} {
		if c, _ := x.pede(t, "GET", caminho, ""); c != 400 {
			t.Fatal(caminho, c)
		}
	}
	if c, _ := x.pede(t, "GET", "/api/rankings/recorte?patch=1.1&elo=CHALLENGER&rota=TOP", ""); c != 404 {
		t.Fatal(c)
	}
}

func TestPedidosRecusados(t *testing.T) {
	x := prepara(t, ranking.FonteLCU, true)
	// Fonte lcu num servidor sem cliente: 409, com a saída.
	if c, m := x.pede(t, "POST", "/api/rankings/atualizacoes", `{"elo":"GOLD"}`); c != 409 || !strings.Contains(m["erro"].(string), `"fonte": "riot"`) {
		t.Fatal(c, m)
	}
	x.riot.Recusada = true
	if c, m := x.pede(t, "POST", "/api/rankings/atualizacoes", `{"elo":"GOLD","fonte":"riot"}`); c != 409 || !strings.Contains(m["erro"].(string), "recusou a chave") {
		t.Fatal(c, m)
	}
	for _, corpo := range []string{`{"elo":"IRON"}`, `{"elo":"GOLD","fonte":"x"}`, `{"elo":"GOLD","candidatos":301}`, `{"elo":"GOLD","patch":"abc"}`,
		`{"elo":"GOLD","partidas_por_jogador":2}`} {
		if c, _ := x.pede(t, "POST", "/api/rankings/atualizacoes", corpo); c != 400 {
			t.Fatal(corpo, c)
		}
	}
}

// Retomada: pausa no meio dos históricos e continua do próximo pendente, sem
// pagar de novo o que já foi lido; a segunda atualização vem do cache; o gasto
// fica dentro do orçamento.
func TestRetomadaCacheEOrcamento(t *testing.T) {
	// Referência: a mesma atualização sem pausa.
	ref := prepara(t, ranking.FonteRiot, true)
	ref.atualiza(t, `{"elo":"GOLD"}`)
	pedidosRef := ref.riot.PedidosIDs + ref.riot.PedidosPartidas

	x := prepara(t, ranking.FonteRiot, true)
	var uma sync.Once
	x.riot.AntesDoPedido = func() {
		if x.riot.PedidosPartidas >= 15 {
			uma.Do(func() {
				var rodando int64
				x.db.QueryRow(`SELECT id FROM ranking_atualizacoes WHERE estado = 'rodando'`).Scan(&rodando)
				x.a.Pausa(rodando)
			})
		}
	}
	at, _, err := x.a.Cria(context.Background(), ranking.Pedido{Elo: "GOLD"})
	if err != nil {
		t.Fatal(err)
	}
	id := at.ID
	x.a.Espera()
	meio, _ := ranking.LeAtualizacao(x.db, id)
	if meio.Estado != ranking.Pausado || meio.CandidatosVistos == 0 || meio.CandidatosVistos >= meio.Candidatos || meio.Fase != ranking.FaseHistorico {
		t.Fatalf("%+v", meio)
	}
	x.riot.AntesDoPedido = nil
	if _, err := x.a.Retoma(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	x.a.Espera()
	fim, _ := ranking.LeAtualizacao(x.db, id)
	if fim.Estado != ranking.Concluido || fim.CandidatosVistos != fim.Candidatos || fim.Rankings < 5 {
		t.Fatalf("%+v", fim)
	}
	if got := x.riot.PedidosIDs + x.riot.PedidosPartidas; got != pedidosRef {
		t.Fatalf("a retomada pagou de novo: %d pedidos, sem pausa %d", got, pedidosRef)
	}
	// O gasto registrado é o que a Riot falsa contou, e cabe no orçamento.
	o := ranking.CalculaOrcamento("GOLD", ranking.FonteRiot, fim.TetoCandidatos, fim.TetoPartidas)
	if fim.PedidosIDs+fim.PedidosPartidas != pedidosRef || fim.PedidosIDs+fim.PedidosPartidas > o.PedidosRegional ||
		fim.PedidosLiga > o.PedidosPlataforma || fim.PedidosConta != 0 {
		t.Fatalf("%+v %+v", fim, o)
	}
	// Segunda atualização do mesmo elo: ids e partidas do cache; só a liga é
	// paga, e os não achados (404 não fica no cache: a conta pode voltar).
	antesP := x.riot.PedidosPartidas
	id2 := x.atualiza(t, `{"elo":"GOLD"}`)
	seg, _ := ranking.LeAtualizacao(x.db, id2)
	if x.riot.PedidosPartidas != antesP || seg.PedidosPartidas != 0 || seg.PedidosIDs != seg.Fora[ranking.ForaNaoAchado] ||
		seg.DoCache == 0 || seg.PedidosLiga == 0 {
		t.Fatalf("%+v", seg)
	}
}

func TestCancelaECorrida(t *testing.T) {
	x := prepara(t, ranking.FonteRiot, true)
	bloqueia := make(chan struct{})
	solta := sync.OnceFunc(func() { close(bloqueia) })
	x.riot.AntesDoPedido = func() { <-bloqueia }
	at, _, err := x.a.Cria(context.Background(), ranking.Pedido{Elo: "GOLD"})
	if err != nil {
		t.Fatal(err)
	}
	// Uma por vez.
	if _, _, err := x.a.Cria(context.Background(), ranking.Pedido{Elo: "SILVER"}); err == nil || !strings.Contains(err.Error(), "já há uma atualização") {
		t.Fatal(err)
	}
	if err := x.a.Cancela(at.ID); err != nil {
		t.Fatal(err)
	}
	solta()
	x.a.Espera()
	if a, _ := ranking.LeAtualizacao(x.db, at.ID); a.Estado != ranking.Cancelado {
		t.Fatalf("%+v", a)
	}
}
