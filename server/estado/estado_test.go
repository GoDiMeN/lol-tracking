package estado

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
