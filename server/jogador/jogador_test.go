package jogador

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"loltracking/server/banco"
	"loltracking/server/lcu"
)

func prepara(t *testing.T) (*http.ServeMux, *lcu.Falso) {
	t.Helper()
	db, err := banco.AbreMemoria(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := Migra(db); err != nil {
		t.Fatal(err)
	}
	f := lcu.NovoFalso()
	mux := http.NewServeMux()
	Registra(mux, db, f)
	return mux, f
}

func pede(mux *http.ServeMux, metodo, caminho, corpo string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(metodo, caminho, strings.NewReader(corpo)))
	return rec
}

func TestSeparaRiotID(t *testing.T) {
	casos := []struct{ texto, nome, tag string }{
		{"GodiNzt#br1", "GodiNzt", "br1"},
		{"  Nacht Faust#STF ", "Nacht Faust", "STF"},
		{"GodiNzt", "GodiNzt", "br1"},
		{"GodiNzt#", "GodiNzt", "br1"},
	}
	for _, c := range casos {
		n, tg, err := SeparaRiotID(c.texto, "br1")
		if err != nil || n != c.nome || tg != c.tag {
			t.Errorf("%q → %q %q %v", c.texto, n, tg, err)
		}
	}
	for _, ruim := range []string{"", "  ", "#br1"} {
		if _, _, err := SeparaRiotID(ruim, "br1"); err == nil {
			t.Errorf("%q deveria ser inválido", ruim)
		}
	}
}

func TestBuscaGravaEListaRecentes(t *testing.T) {
	mux, _ := prepara(t)
	rec := pede(mux, "POST", "/api/jogadores/buscar", `{"texto":"jogador teste#BR1"}`)
	if rec.Code != 200 {
		t.Fatalf("busca: %d %s", rec.Code, rec.Body.String())
	}
	var r struct{ Jogador Jogador }
	json.Unmarshal(rec.Body.Bytes(), &r)
	if r.Jogador.Puuid != lcu.FalsoLogado.Puuid || r.Jogador.RiotID != "Jogador Teste#br1" {
		t.Fatalf("jogador: %+v", r.Jogador)
	}
	// Sem tag: usa a da região (BR → br1).
	if rec := pede(mux, "POST", "/api/jogadores/buscar", `{"texto":"Rival"}`); rec.Code != 200 {
		t.Fatalf("sem tag: %d %s", rec.Code, rec.Body.String())
	}
	pede(mux, "POST", "/api/jogadores/buscar", `{"texto":"Jogador Teste#br1"}`)

	rec = pede(mux, "GET", "/api/jogadores", "")
	var l struct{ Jogadores []Jogador }
	json.Unmarshal(rec.Body.Bytes(), &l)
	if len(l.Jogadores) != 2 || l.Jogadores[0].Nome != "Jogador Teste" || l.Jogadores[0].Buscas != 2 {
		t.Fatalf("recentes: %s", rec.Body.String())
	}
	if rec := pede(mux, "GET", "/api/jogadores/"+lcu.FalsoRival.Puuid, ""); rec.Code != 200 {
		t.Fatalf("le: %d", rec.Code)
	}
}

func TestBuscaErros(t *testing.T) {
	mux, f := prepara(t)
	if rec := pede(mux, "POST", "/api/jogadores/buscar", `{"texto":"Ninguem#br1"}`); rec.Code != 404 || !strings.Contains(rec.Body.String(), "Ninguem#br1") {
		t.Fatalf("inexistente: %d %s", rec.Code, rec.Body.String())
	}
	if rec := pede(mux, "POST", "/api/jogadores/buscar", `{"texto":""}`); rec.Code != 400 {
		t.Fatalf("vazio: %d", rec.Code)
	}
	if rec := pede(mux, "POST", "/api/jogadores/buscar", `{`); rec.Code != 400 {
		t.Fatalf("json ruim: %d", rec.Code)
	}
	if rec := pede(mux, "GET", "/api/jogadores", ""); !strings.Contains(rec.Body.String(), `"jogadores":[]`) {
		t.Fatalf("lista vazia deveria ser []: %s", rec.Body.String())
	}
	f.Fechado = true
	rec := pede(mux, "POST", "/api/jogadores/buscar", `{"texto":"Rival#br1"}`)
	if rec.Code != 503 || !strings.Contains(rec.Body.String(), "não está aberto") {
		t.Fatalf("cliente fechado: %d %s", rec.Code, rec.Body.String())
	}
}
