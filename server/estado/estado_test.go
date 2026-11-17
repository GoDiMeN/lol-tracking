package estado

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"loltracking/server/lcu"
	"loltracking/server/riot"
)

func pedeEstado(t *testing.T, f *lcu.Falso) Estado {
	t.Helper()
	mux := http.NewServeMux()
	Registra(mux, f, riot.Novo(filepath.Join(t.TempDir(), "sem-token.txt"), true), true)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/estado", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	var e Estado
	json.Unmarshal(rec.Body.Bytes(), &e)
	return e
}

func TestEstadoConectado(t *testing.T) {
	e := pedeEstado(t, lcu.NovoFalso())
	if !e.LCU.Conectado || e.Invocador == nil || e.Invocador.Nome != "Jogador Teste" || e.Patch != "16.22" ||
		e.Fase != "None" || e.Regiao != "BR" || !e.ModoTeste || e.ChaveRiot.Configurada {
		t.Fatalf("%+v", e)
	}
}

func TestEstadoClienteFechado(t *testing.T) {
	f := lcu.NovoFalso()
	f.Fechado = true
	e := pedeEstado(t, f)
	if e.LCU.Conectado || e.LCU.Erro == "" || e.Invocador != nil || e.ChaveRiot.Detalhe == "" {
		t.Fatalf("%+v", e)
	}
}

func TestDadosSomaPorTipo(t *testing.T) {
	dir := t.TempDir()
	escreve := func(rel string, n int) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, make([]byte, n), 0o644)
	}
	escreve("lol.db", 100)
	escreve("lol.db-wal", 20)
	escreve("imagens/campeao/166.png", 7)
	escreve("capturas/1/a.png", 5)
	escreve("campeoes.json", 3)
	mux := http.NewServeMux()
	RegistraDados(mux, dir, "lol.db")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/dados", nil))
	var d Dados
	json.Unmarshal(rec.Body.Bytes(), &d)
	if rec.Code != 200 || d.Bytes.Banco != 120 || d.Bytes.Imagens != 7 || d.Bytes.Capturas != 5 || d.Bytes.Outros != 3 ||
		d.Bytes.Total != 135 || d.Banco != "lol.db" || !filepath.IsAbs(d.Pasta) {
		t.Fatalf("%d %+v", rec.Code, d)
	}
}
