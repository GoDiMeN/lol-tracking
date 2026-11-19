package aprendizado

import (
	"context"
	"strings"
	"testing"

	"loltracking/server/campeao"
	"loltracking/server/lcu"
	"loltracking/server/ranking"
)

// rankingDoOuro roda o ranking do Ouro (fonte lcu) no mesmo banco da coleta.
func rankingDoOuro(t *testing.T, a *amb) {
	t.Helper()
	at := ranking.Novo(ranking.Deps{DB: a.db, Riot: a.riot, LCU: a.l, Hub: a.hub, FontePadrao: ranking.FonteLCU,
		Campeoes: campeao.Fixo(lcu.CampeoesFalsos)})
	if _, _, err := at.Cria(context.Background(), ranking.Pedido{Elo: "GOLD"}); err != nil {
		t.Fatal(err)
	}
	at.Espera()
}

func TestColetaDoRanking(t *testing.T) {
	a := prepara(t, nil)
	// Sem ranking ainda: 409 dizendo qual falta.
	c, m := a.pede(t, "POST", "/api/aprendizado/coletas", `{"elos":["GOLD"],"funcoes":["TOP"],"ranking":{"primeiros":1}}`)
	if c != 409 || !strings.Contains(m["erro"].(string), "Ouro · topo") {
		t.Fatal(c, m)
	}
	rankingDoOuro(t, a)
	for corpo, frase := range map[string]string{
		`{"elos":["GOLD"],"quem":"jogador","ranking":{}}`: `"quem" precisa ser "todos"`,
		`{"ranking":{}}`: `diga os "elos"`,
		`{"elos":["GOLD"],"amostragem":true,"ranking":{}}`:        "use um só",
		`{"elos":["GOLD"],"ranking":{"primeiros":9}}`:             "ranking.primeiros",
		`{"elos":["GOLD"],"ranking":{"modo":"x"}}`:                "ranking.modo",
		`{"elos":["GOLD"],"funcao":"TOP","ranking":{}}`:           `não "funcao"`,
		`{"elos":["GOLD"],"jogadores_por_funcao":2,"ranking":{}}`: "jogadores_por_funcao",
	} {
		if c, m := a.pede(t, "POST", "/api/aprendizado/coletas", corpo); c != 400 || !strings.Contains(m["erro"].(string), frase) {
			t.Fatal(corpo, c, m)
		}
	}
	// Patch do ranking diferente do instalado: 409.
	if c, m := a.pede(t, "POST", "/api/aprendizado/coletas", `{"elos":["GOLD"],"funcoes":["TOP"],"ranking":{"patch":"16.1"}}`); c != 409 ||
		!strings.Contains(m["erro"].(string), "patch instalado") {
		t.Fatal(c, m)
	}

	// "Rastrear o 1º do topo": a lista é a do ranking, quem é "todos".
	c, m = a.pede(t, "POST", "/api/aprendizado/coletas", `{"elos":["GOLD"],"funcoes":["TOP"],"ranking":{"primeiros":1,"modo":"melhor"}}`)
	if c != 202 {
		t.Fatal(c, m)
	}
	col := m["coleta"].(map[string]any)
	if col["quem"] != QuemTodos || col["aproveitar"] != false || col["modo_ranking"] != "melhor" || len(col["rankings"].([]any)) != 1 {
		t.Fatal(col)
	}
	a.k.Espera()
	d := a.detalhe(t, int64(col["id"].(float64)))
	if d.Coleta.Estado != Concluido || len(d.Jogadores) != 1 {
		t.Fatalf("%+v %+v", d.Coleta, d.Jogadores)
	}
	rk, err := ranking.Ultimo(a.db, "BR1", lcu.PatchDe(lcu.VersaoFalsa), "GOLD", "TOP", 0)
	if err != nil {
		t.Fatal(err)
	}
	ps, _ := ranking.Posicoes(a.db, rk.ID, ranking.ModoMelhor)
	j := d.Jogadores[0]
	if j.Funcao != "TOP" || j.Posicao != 1 || j.puuidRiot != ps[0].Puuid || !strings.HasPrefix(j.Motivo, "ranking do Lane (melhor): 1º na topo") {
		t.Fatalf("%+v (ranking: %+v)", j, ps[0])
	}
	// Com "todos", a partida dele tem os 10 passes.
	if len(d.Partidas) != 1 || d.Partidas[0].PassesTotal != 10 || d.Partidas[0].Estado != PartidaConcluida {
		t.Fatalf("%+v", d.Partidas)
	}
}
