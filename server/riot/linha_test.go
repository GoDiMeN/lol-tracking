package riot

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinhaDoTempo(t *testing.T) {
	IntervaloPedidos = 0
	status := 200
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Riot-Token") != chaveDeTeste || r.URL.Path != "/lol/match/v5/matches/BR1_42/timeline" {
			t.Errorf("pedido errado: %s", r.URL.Path)
		}
		w.WriteHeader(status)
		w.Write([]byte(`{"info":{"frames":[]}}`))
	}))
	defer srv.Close()
	arq := filepath.Join(t.TempDir(), "token.txt")
	os.WriteFile(arq, []byte(chaveDeTeste), 0o600)
	c := Novo(arq, false)
	c.baseRegional = srv.URL
	b, err := c.LinhaDoTempo(context.Background(), 42)
	if err != nil || !strings.Contains(string(b), "frames") {
		t.Fatal(err, string(b))
	}
	status = 401
	if _, err := c.LinhaDoTempo(context.Background(), 42); !errors.Is(err, ErrChaveRecusada) || strings.Contains(err.Error(), chaveDeTeste) {
		t.Fatal(err)
	}
	if _, err := Novo(filepath.Join(t.TempDir(), "nada.txt"), false).LinhaDoTempo(context.Background(), 42); !errors.Is(err, ErrSemChave) {
		t.Fatal(err)
	}
	if _, err := Novo(arq, true).LinhaDoTempo(context.Background(), 42); !errors.Is(err, ErrSemChave) {
		t.Fatal("modo teste não pode ir à rede", err)
	}
}
