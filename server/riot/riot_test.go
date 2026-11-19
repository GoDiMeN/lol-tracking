package riot

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

const chaveDeTeste = "RGAPI-segredo-que-nao-pode-vazar"

func TestEstadoComChaveRecusadaENaoVaza(t *testing.T) {
	var pedidos atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pedidos.Add(1)
		if r.Header.Get("X-Riot-Token") != chaveDeTeste {
			t.Errorf("cabeçalho errado")
		}
		w.WriteHeader(401)
		w.Write([]byte(`{"status":{"message":"Unknown apikey","status_code":401}}`))
	}))
	defer srv.Close()
	arq := filepath.Join(t.TempDir(), "token.txt")
	os.WriteFile(arq, []byte(chaveDeTeste+"\n"), 0o600)

	var saida bytes.Buffer
	log.SetOutput(&saida)
	defer log.SetOutput(os.Stderr)

	c := Novo(arq, false)
	c.base = srv.URL
	e := c.Estado(context.Background())
	if !e.Configurada || e.Valida == nil || *e.Valida {
		t.Fatalf("estado: %+v", e)
	}
	c.Estado(context.Background())
	if pedidos.Load() != 1 {
		t.Fatalf("cache não segurou: %d pedidos", pedidos.Load())
	}
	b, _ := json.Marshal(e)
	if strings.Contains(string(b), chaveDeTeste) || strings.Contains(saida.String(), chaveDeTeste) {
		t.Fatal("a chave vazou para a resposta ou para o log")
	}
}

func TestEstadoValidaSemChaveEModoTeste(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{}`)) }))
	defer srv.Close()
	arq := filepath.Join(t.TempDir(), "token.txt")
	os.WriteFile(arq, []byte(chaveDeTeste), 0o600)
	c := Novo(arq, false)
	c.base = srv.URL
	if e := c.Estado(context.Background()); e.Valida == nil || !*e.Valida {
		t.Fatalf("válida: %+v", e)
	}
	if e := Novo(filepath.Join(t.TempDir(), "nao-existe"), false).Estado(context.Background()); e.Configurada {
		t.Fatalf("sem arquivo: %+v", e)
	}
	if e := Novo(arq, true).Estado(context.Background()); !e.Configurada || e.Valida != nil {
		t.Fatalf("modo teste: %+v", e)
	}
}

// Chave trocada no token.txt dentro dos 10 min do cache: o resultado guardado
// era da chave velha e não vale; a nova é conferida na hora. A mesma chave
// continua no cache.
func TestEstadoConfereDeNovoQuandoAChaveMuda(t *testing.T) {
	const nova = "RGAPI-outra-chave-nova"
	var pedidos atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pedidos.Add(1)
		if r.Header.Get("X-Riot-Token") == nova {
			w.Write([]byte(`{}`))
			return
		}
		w.WriteHeader(403)
	}))
	defer srv.Close()
	arq := filepath.Join(t.TempDir(), "token.txt")
	os.WriteFile(arq, []byte(chaveDeTeste), 0o600)
	c := Novo(arq, false)
	c.base = srv.URL
	if e := c.Estado(context.Background()); e.Valida == nil || *e.Valida {
		t.Fatalf("chave velha (vencida): %+v", e)
	}
	os.WriteFile(arq, []byte(nova+"\n"), 0o600)
	if e := c.Estado(context.Background()); e.Valida == nil || !*e.Valida {
		t.Fatalf("chave nova devia ser conferida na hora: %+v", e)
	}
	c.Estado(context.Background())
	if pedidos.Load() != 2 {
		t.Fatalf("a mesma chave devia ficar no cache: %d pedidos", pedidos.Load())
	}
}
