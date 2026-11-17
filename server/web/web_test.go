package web

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteListaNuncaNull(t *testing.T) {
	rec := httptest.NewRecorder()
	var nada []string
	WriteLista(rec, "itens", nada, map[string]any{"total": 0})
	var corpo map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &corpo); err != nil {
		t.Fatal(err)
	}
	if _, ok := corpo["itens"].([]any); !ok {
		t.Fatalf("itens deveria ser lista, veio %s", rec.Body.String())
	}
}

func TestWriteErr(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteErr(rec, 404, "não achei")
	if rec.Code != 404 || !strings.Contains(rec.Body.String(), `"erro":"não achei"`) {
		t.Fatalf("resposta: %d %s", rec.Code, rec.Body.String())
	}
}

func TestStaticAPIDesconhecidaEJSON(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<p>oi</p>"), 0o644)
	h := Static(dir)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/nao-existe", nil))
	if rec.Code != 404 || !strings.Contains(rec.Body.String(), "erro") {
		t.Fatalf("api desconhecida: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "oi") {
		t.Fatalf("raiz: %d %s", rec.Code, rec.Body.String())
	}
}

func TestExecucaoSSEComReplay(t *testing.T) {
	h := NovoHub()
	mux := http.NewServeMux()
	RegistraExecucoes(mux, h)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	e := h.Nova()
	e.Emite("progresso", map[string]int{"pct": 10})
	go func() {
		time.Sleep(50 * time.Millisecond)
		e.Emite("progresso", map[string]int{"pct": 90})
		e.Fecha()
	}()

	r, err := http.Get(srv.URL + "/api/execucoes/" + e.ID() + "/eventos")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	var tipos []string
	sc := bufio.NewScanner(r.Body)
	for sc.Scan() {
		if s, ok := strings.CutPrefix(sc.Text(), "event: "); ok {
			tipos = append(tipos, s)
		}
	}
	if strings.Join(tipos, ",") != "progresso,progresso,fim" {
		t.Fatalf("eventos: %v", tipos)
	}

	// Reconexão depois do fim, com Last-Event-ID: não pode ficar pendurada.
	req, _ := http.NewRequest("GET", srv.URL+"/api/execucoes/"+e.ID()+"/eventos", nil)
	req.Header.Set("Last-Event-ID", "3")
	r2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r2.Body.Close()
}

func TestExecucaoCancelaUmaVez(t *testing.T) {
	h := NovoHub()
	e := h.Nova()
	n := 0
	e.AoCancelar(func() { n++ })
	e.Cancela()
	e.Cancela()
	e.Fecha()
	if n != 1 {
		t.Fatalf("cancelar rodou %d vezes", n)
	}
	ev := e.Eventos()
	if ev[len(ev)-1].Tipo != "fim" || !strings.Contains(string(ev[len(ev)-1].Dados), "cancelada") {
		t.Fatalf("fim sem cancelada: %+v", ev)
	}
}

func TestExecucaoDesconhecida404(t *testing.T) {
	h := NovoHub()
	mux := http.NewServeMux()
	RegistraExecucoes(mux, h)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/execucoes/x/eventos", nil))
	if rec.Code != 404 {
		t.Fatalf("status %d", rec.Code)
	}
}
