package riot

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func clienteDeTeste(t *testing.T, h http.HandlerFunc) *Cliente {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	arq := filepath.Join(t.TempDir(), "token.txt")
	os.WriteFile(arq, []byte(chaveDeTeste), 0o600)
	c := Novo(arq, false)
	c.redireciona = srv.URL
	return c
}

func TestLigaContaE429(t *testing.T) {
	var estouros atomic.Int32
	c := clienteDeTeste(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Riot-Token") != chaveDeTeste {
			t.Errorf("cabeçalho errado")
		}
		switch {
		case r.URL.Path == "/lol/league/v4/challengerleagues/by-queue/RANKED_SOLO_5x5":
			// O primeiro pedido estoura o limite: o cliente obedece o Retry-After.
			if estouros.Add(1) == 1 {
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(429)
				return
			}
			w.Write([]byte(`{"tier":"CHALLENGER","entries":[{"puuid":"p1","leaguePoints":1500,"rank":"I","wins":10,"losses":5}]}`))
		case r.URL.Path == "/lol/league/v4/entries/RANKED_SOLO_5x5/GOLD/III":
			if r.URL.Query().Get("page") != "2" {
				t.Errorf("página %q", r.URL.Query().Get("page"))
			}
			w.Write([]byte(`[{"puuid":"p2","tier":"GOLD","rank":"III","leaguePoints":88,"wins":50,"losses":30,"freshBlood":true,"veteran":false,"hotStreak":true,"inactive":false}]`))
		case r.URL.Path == "/riot/account/v1/accounts/by-puuid/p1":
			w.Write([]byte(`{"puuid":"p1","gameName":"Fulano","tagLine":"BR1"}`))
		default:
			w.WriteHeader(404)
		}
	})
	reg, _ := RegiaoPorCodigo("BR1")
	ctx := context.Background()
	inicio := time.Now()
	l, err := c.Liga(ctx, reg, "CHALLENGER", "", 0)
	if err != nil || len(l) != 1 || l[0].Puuid != "p1" || l[0].LP != 1500 || l[0].Tier != "CHALLENGER" {
		t.Fatal(l, err)
	}
	if time.Since(inicio) < 900*time.Millisecond || estouros.Load() != 2 {
		t.Fatalf("não esperou o Retry-After (%v, %d pedidos)", time.Since(inicio), estouros.Load())
	}
	if l, err = c.Liga(ctx, reg, "GOLD", "III", 2); err != nil || len(l) != 1 || l[0].LP != 88 || !l[0].FreshBlood ||
		!l[0].HotStreak || l[0].Veteran || l[0].Rank != "III" || l[0].Vitorias != 50 {
		t.Fatal(l, err)
	}
	if _, err = c.Liga(ctx, reg, "GOLD", "V", 1); err == nil {
		t.Fatal("divisão V não existe")
	}
	ct, err := c.ContaPorPuuid(ctx, reg, "p1")
	if err != nil || ct.Nome != "Fulano" || ct.Tag != "BR1" {
		t.Fatal(ct, err)
	}
	if _, err := c.ContaPorPuuid(ctx, reg, "nao-existe"); !errors.Is(err, ErrNaoEncontradoRiot) {
		t.Fatal(err)
	}
}

func TestLigaChaveRecusadaNaoVaza(t *testing.T) {
	c := clienteDeTeste(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) })
	var saida bytes.Buffer
	log.SetOutput(&saida)
	defer log.SetOutput(os.Stderr)
	reg, _ := RegiaoPorCodigo("BR1")
	_, err := c.Liga(context.Background(), reg, "MASTER", "", 0)
	if !errors.Is(err, ErrChaveRecusada) || strings.Contains(err.Error(), chaveDeTeste) || strings.Contains(saida.String(), chaveDeTeste) {
		t.Fatal(err)
	}
	if _, err := Novo(filepath.Join(t.TempDir(), "x"), false).Liga(context.Background(), reg, "MASTER", "", 0); !errors.Is(err, ErrSemChave) {
		t.Fatal(err)
	}
}

func TestLimitadorPorHost(t *testing.T) {
	antes := LimitesPedidos
	LimitesPedidos = []Limite{{N: 2, Janela: 300 * time.Millisecond}}
	defer func() { LimitesPedidos = antes }()
	l := &limitador{pedidos: map[string][]time.Time{}, bloqueado: map[string]time.Time{}}
	ctx := context.Background()
	inicio := time.Now()
	for i := 0; i < 3; i++ {
		l.espera(ctx, "a")
	}
	if d := time.Since(inicio); d < 280*time.Millisecond {
		t.Fatalf("o terceiro pedido não esperou a janela: %v", d)
	}
	// Outro host tem a própria conta.
	inicio = time.Now()
	l.espera(ctx, "b")
	if time.Since(inicio) > 50*time.Millisecond {
		t.Fatal("host b esperou à toa")
	}
	ctx2, cancela := context.WithCancel(ctx)
	cancela()
	l.espera(ctx, "a")
	if err := l.espera(ctx2, "a"); err == nil {
		t.Fatal("contexto cancelado deveria parar a espera")
	}
}

func TestRegioes(t *testing.T) {
	if r, ok := RegiaoPorCodigo("BR1"); !ok || r.RegiaoCliente != "BR" || r.Plataforma != BaseRiot || r.Roteamento != BaseRegional {
		t.Fatal(r)
	}
	if _, ok := RegiaoPorCodigo("NA1"); ok {
		t.Fatal("só a BR está habilitada")
	}
	if len(Regioes()) != 1 {
		t.Fatal(Regioes())
	}
}
