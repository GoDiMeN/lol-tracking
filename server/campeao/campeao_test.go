package campeao

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestDataDragonBaixaUmaVezEGuardaCache(t *testing.T) {
	var pedidos atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pedidos.Add(1)
		switch r.URL.Path {
		case "/api/versions.json":
			w.Write([]byte(`["16.22.1","16.21.1"]`))
		case "/cdn/16.22.1/data/pt_BR/champion.json":
			w.Write([]byte(`{"data":{"Akshan":{"key":"166","name":"Akshan"},"MonkeyKing":{"key":"62","name":"Wukong"}}}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	d := NovoDataDragon(dir)
	d.base = srv.URL
	ctx := context.Background()
	if n := d.Nome(ctx, 166); n != "Akshan" {
		t.Fatalf("166 = %q", n)
	}
	if n := d.Nome(ctx, 62); n != "Wukong" {
		t.Fatalf("62 = %q", n)
	}
	if pedidos.Load() != 2 {
		t.Fatalf("pedidos = %d, esperava 2", pedidos.Load())
	}
	// Id desconhecido: tenta de novo só depois de 10 min; aqui cai na reserva.
	if n := d.Nome(ctx, 9999); n != "campeão 9999" {
		t.Fatalf("9999 = %q", n)
	}

	// Outro processo, mesmo diretório, servidor fora do ar: lê do cache.
	srv.Close()
	d2 := NovoDataDragon(dir)
	d2.base = srv.URL
	if n := d2.Nome(ctx, 166); n != "Akshan" {
		t.Fatalf("do cache: %q", n)
	}
}

func TestDataDragonOfflineUsaID(t *testing.T) {
	d := NovoDataDragon(t.TempDir())
	d.base = "http://127.0.0.1:1"
	if n := d.Nome(context.Background(), 166); n != "campeão 166" {
		t.Fatalf("offline = %q", n)
	}
}

func TestFixo(t *testing.T) {
	f := Fixo{1: "Annie"}
	if f.Nome(context.Background(), 1) != "Annie" || f.Nome(context.Background(), 2) != "campeão 2" {
		t.Fatal("Fixo errado")
	}
}
