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

func TestImagensBaixaUmaVezEServeDoCache(t *testing.T) {
	var pedidos atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pedidos.Add(1)
		switch r.URL.Path {
		case "/api/versions.json":
			w.Write([]byte(`["16.22.1"]`))
		case "/cdn/16.22.1/data/pt_BR/champion.json":
			w.Write([]byte(`{"data":{"MonkeyKing":{"id":"MonkeyKing","key":"62","name":"Wukong"}}}`))
		case "/cdn/16.22.1/img/champion/MonkeyKing.png", "/cdn/16.22.1/img/item/1055.png":
			w.Write([]byte("\x89PNG falso"))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	dd := NovoDataDragon(dir)
	dd.base = srv.URL
	mux := http.NewServeMux()
	RegistraImagens(mux, NovasImagens(dd, dir))
	pede := func(caminho string) (int, string) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", caminho, nil))
		return rec.Code, rec.Body.String()
	}
	if c, b := pede("/api/imagens/campeao/62"); c != 200 || b != "\x89PNG falso" {
		t.Fatal(c, b)
	}
	if c, _ := pede("/api/imagens/item/1055"); c != 200 {
		t.Fatal(c)
	}
	antes := pedidos.Load()
	srv.Close()
	if c, _ := pede("/api/imagens/campeao/62"); c != 200 || pedidos.Load() != antes {
		t.Fatal("devia servir do cache sem rede", c)
	}
	if c, _ := pede("/api/imagens/item/9999"); c != 404 {
		t.Fatal(c)
	}
	if c, _ := pede("/api/imagens/item/abc"); c != 400 {
		t.Fatal(c)
	}
	// Modo teste: sem Data Dragon, nada sai para a rede.
	mux2 := http.NewServeMux()
	RegistraImagens(mux2, NovasImagens(nil, t.TempDir()))
	rec := httptest.NewRecorder()
	mux2.ServeHTTP(rec, httptest.NewRequest("GET", "/api/imagens/campeao/62", nil))
	if rec.Code != 404 {
		t.Fatal(rec.Code)
	}
}
