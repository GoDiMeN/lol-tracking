package estudo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"loltracking/server/analise"
	"loltracking/server/banco"
	"loltracking/server/campeao"
	"loltracking/server/lcu"
	"loltracking/server/partida"
)

// idPartida: a i-ésima partida do logado no LCU falso (0 = mais nova). Nas
// 8 primeiras ele perde a 0, 3, 5, 7 e ganha a 1, 2, 4, 6.
func idPartida(i int) int64 { return 3304790000 - int64(i)*1000 }

type amb struct {
	db  *banco.DB
	mux *http.ServeMux
}

func prepara(t *testing.T) *amb {
	t.Helper()
	db, err := banco.AbreMemoria(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, m := range []func(*banco.DB) error{partida.Migra, analise.Migra, Migra} {
		if err := m(db); err != nil {
			t.Fatal(err)
		}
	}
	l := lcu.NovoFalso()
	ctx := context.Background()
	for _, g := range []int64{idPartida(0), idPartida(1), idPartida(2), idPartida(3), idPartida(4), idPartida(5), idPartida(6),
		idPartida(7), 3304690000 /* do rival */} {
		bruto, err := l.Partida(ctx, g)
		if err != nil {
			t.Fatal(err)
		}
		if err := partida.Grava(ctx, partida.Deps{DB: db, LCU: l, Campeoes: campeao.Fixo(lcu.CampeoesFalsos)}, bruto, true); err != nil {
			t.Fatal(err)
		}
	}
	mux := http.NewServeMux()
	Registra(mux, analise.Deps{DB: db})
	return &amb{db: db, mux: mux}
}

func (a *amb) pede(t *testing.T, metodo, caminho, corpo string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	a.mux.ServeHTTP(rec, httptest.NewRequest(metodo, caminho, strings.NewReader(corpo)))
	var m map[string]any
	json.Unmarshal(rec.Body.Bytes(), &m)
	return rec.Code, m
}

// indicador grava um valor como a análise gravaria.
func (a *amb) indicador(t *testing.T, g int64, nome string, v float64, momentos ...string) {
	t.Helper()
	var pid int
	if err := a.db.QueryRow(`SELECT participante_id FROM participantes WHERE game_id = ? AND puuid = ?`, g, lcu.FalsoLogado.Puuid).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	ms := []analise.Momento{}
	for i, m := range momentos {
		ms = append(ms, analise.Momento{TMs: int64(i+1) * 60000, Texto: m})
	}
	det, _ := json.Marshal(map[string]any{"momentos": ms})
	if _, err := a.db.Exec(`INSERT INTO indicadores (game_id, participante_id, indicador, valor, aproximado, detalhe, calculado_em)
		VALUES (?,?,?,?,0,?,?)`, g, pid, nome, v, string(det), banco.Agora()); err != nil {
		t.Fatal(err)
	}
}

func TestEstudoCRUDEComparacao(t *testing.T) {
	a := prepara(t)
	if c, _ := a.pede(t, "POST", "/api/estudos", `{"nome":"x"}`); c != 400 {
		t.Fatal(c)
	}
	if c, _ := a.pede(t, "POST", "/api/estudos", `{"nome":"x","puuid":"p","posicao":"MEIO"}`); c != 400 {
		t.Fatal(c)
	}
	c, m := a.pede(t, "POST", "/api/estudos", `{"nome":"Akshan no meio","puuid":"`+lcu.FalsoLogado.Puuid+`","campeao_id":166}`)
	if c != 201 {
		t.Fatal(c, m)
	}
	e := m["estudo"].(map[string]any)
	id := fmt.Sprint(e["id"])
	if e["jogador"] != "Jogador Teste#br1" || e["campeao"] != "Akshan" {
		t.Fatal(e)
	}
	// Sugestões: as do jogador com Akshan (as três mais novas).
	if c, m = a.pede(t, "GET", "/api/estudos/"+id+"/sugestoes", ""); c != 200 || len(m["partidas"].([]any)) != 3 {
		t.Fatal(c, m)
	}
	for i := 0; i < 8; i++ {
		c, m = a.pede(t, "POST", fmt.Sprintf("/api/estudos/%s/partidas/%d", id, idPartida(i)), `{"anotacao":"vi tal coisa"}`)
		if c != 201 {
			t.Fatal(i, c, m)
		}
		p := m["partida"].(map[string]any)
		quer := Derrota
		if i == 1 || i == 2 || i == 4 || i == 6 {
			quer = Vitoria
		}
		if p["resultado"] != quer || p["anotacao"] != "vi tal coisa" || p["fora_do_filtro"] != (i >= 3) {
			t.Fatal(i, p)
		}
	}
	if c, _ = a.pede(t, "POST", fmt.Sprintf("/api/estudos/%s/partidas/%d", id, idPartida(0)), ``); c != 409 {
		t.Fatal(c)
	}
	if c, m = a.pede(t, "POST", "/api/estudos/"+id+"/partidas/3304690000", ``); c != 400 {
		t.Fatal(c, m)
	}
	if c, _ = a.pede(t, "POST", "/api/estudos/"+id+"/partidas/42", ``); c != 404 {
		t.Fatal(c)
	}
	// Valores: mortes bem diferentes (4 × 4); cs_10 só em 2 derrotas (insuficiente).
	mortes := map[int]float64{0: 5, 1: 1, 2: 2, 3: 6, 4: 1, 5: 5, 6: 2, 7: 7}
	for i, v := range mortes {
		var ms []string
		if v >= 5 {
			ms = []string{"morreu fora da rota aos 8:48"}
		}
		a.indicador(t, idPartida(i), "mortes", v, ms...)
		a.indicador(t, idPartida(i), "abates", 3+float64(i%2), nil...)
	}
	for _, i := range []int{1, 2, 4, 0, 3} {
		a.indicador(t, idPartida(i), "cs_10", 70)
	}
	c, m = a.pede(t, "GET", "/api/estudos/"+id+"/comparacao", "")
	if c != 200 || m["vitorias"].(float64) != 4 || m["derrotas"].(float64) != 4 || m["ressalva"] == "" {
		t.Fatal(c, m)
	}
	ls := m["linhas"].([]any)
	if len(ls) != 3 {
		t.Fatal(ls)
	}
	l0 := ls[0].(map[string]any)
	if l0["indicador"] != "mortes" || l0["insuficiente"] != false || l0["efeito"].(float64) > -2 ||
		l0["vitorias"].(map[string]any)["media"].(float64) != 1.5 || l0["derrotas"].(map[string]any)["mediana"].(float64) != 5.5 ||
		!strings.Contains(l0["observacao"].(string), "maior nas derrotas") {
		t.Fatal(l0)
	}
	last := ls[2].(map[string]any)
	if last["indicador"] != "cs_10" || last["insuficiente"] != true || last["efeito"] != nil ||
		!strings.Contains(last["observacao"].(string), "insuficiente") {
		t.Fatal(last)
	}
	// O indicador com as partidas e os momentos.
	c, m = a.pede(t, "GET", "/api/estudos/"+id+"/indicadores/mortes", "")
	if c != 200 || len(m["partidas"].([]any)) != 8 {
		t.Fatal(c, m)
	}
	achou := false
	for _, p := range m["partidas"].([]any) {
		pm := p.(map[string]any)
		if ms := pm["momentos"].([]any); len(ms) == 1 && ms[0].(map[string]any)["texto"] == "morreu fora da rota aos 8:48" {
			achou = true
		}
	}
	if !achou {
		t.Fatal(m)
	}
	if c, _ = a.pede(t, "GET", "/api/estudos/"+id+"/indicadores/nao_existe", ""); c != 400 {
		t.Fatal(c)
	}
	// Anotação, tirar, editar, apagar.
	if c, m = a.pede(t, "PUT", fmt.Sprintf("/api/estudos/%s/partidas/%d", id, idPartida(1)), `{"anotacao":"ganhou na rota"}`); c != 200 ||
		m["partida"].(map[string]any)["anotacao"] != "ganhou na rota" {
		t.Fatal(c, m)
	}
	if c, _ = a.pede(t, "PUT", fmt.Sprintf("/api/estudos/%s/partidas/%d", id, idPartida(9)), `{"anotacao":"x"}`); c != 404 {
		t.Fatal(c)
	}
	if c, _ = a.pede(t, "DELETE", fmt.Sprintf("/api/estudos/%s/partidas/%d", id, idPartida(7)), ``); c != 204 {
		t.Fatal(c)
	}
	if c, m = a.pede(t, "GET", "/api/estudos/"+id, ""); c != 200 || len(m["partidas"].([]any)) != 7 || m["estudo"].(map[string]any)["derrotas"].(float64) != 3 {
		t.Fatal(c, m)
	}
	if c, m = a.pede(t, "PATCH", "/api/estudos/"+id, `{"nome":"Akshan","campeao_id":0}`); c != 200 || m["estudo"].(map[string]any)["nome"] != "Akshan" {
		t.Fatal(c, m)
	}
	if c, m = a.pede(t, "GET", "/api/estudos", ""); c != 200 || len(m["estudos"].([]any)) != 1 {
		t.Fatal(c, m)
	}
	if c, _ = a.pede(t, "DELETE", "/api/estudos/"+id, ""); c != 204 {
		t.Fatal(c)
	}
	if c, _ = a.pede(t, "GET", "/api/estudos/"+id, ""); c != 404 {
		t.Fatal(c)
	}
	var n int
	a.db.QueryRow(`SELECT COUNT(*) FROM estudo_partidas`).Scan(&n)
	if n != 0 {
		t.Fatal("a lista do estudo apagado ficou")
	}
}

func TestEstatistica(t *testing.T) {
	r := resumo([]float64{4, 1, 3, 2})
	if *r.Media != 2.5 || *r.Mediana != 2.5 || *r.Min != 1 || *r.Max != 4 || *r.DP != 1.29 {
		t.Fatalf("%+v", r)
	}
	if e := cohen([]float64{1, 2, 3}, []float64{4, 5, 6}); e == nil || *e != -3 {
		t.Fatal(e)
	}
	if cohen([]float64{2, 2}, []float64{2, 2}) != nil || cohen([]float64{1}, nil) != nil {
		t.Fatal("sem variação ou sem um lado: nulo")
	}
}
