package lcu

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestPermitidaAceitaAsRotasDaLista(t *testing.T) {
	casos := []struct{ metodo, caminho string }{
		{"GET", "/lol-summoner/v1/current-summoner"},
		{"GET", "/lol-summoner/v1/alias/lookup"},
		{"GET", "/lol-match-history/v1/products/lol/0a1b2c3d-4e5f-6789-abcd-ef0123456789/matches"},
		{"GET", "/lol-match-history/v1/products/lol/current-summoner/matches"},
		{"GET", "/lol-match-history/v1/games/3304594274"},
		{"GET", "/lol-match-history/v1/game-timelines/3304594274"},
		{"GET", "/lol-gameflow/v1/gameflow-phase"},
		{"GET", "/riotclient/region-locale"},
		{"GET", "/lol-replays/v1/configuration"},
		{"GET", "/lol-replays/v1/rofls/path"},
		{"GET", "/lol-replays/v1/metadata/3304594274"},
		{"POST", "/lol-replays/v2/metadata/3304594274/create"},
		{"POST", "/lol-replays/v1/rofls/3304594274/download"},
		{"POST", "/lol-replays/v1/rofls/3304594274/watch"},
	}
	for _, c := range casos {
		if !Permitida(c.metodo, c.caminho) {
			t.Errorf("deveria permitir %s %s", c.metodo, c.caminho)
		}
	}
}

// As rotas que a regra 1 do CLAUDE.md proíbe: nenhuma pode passar, nem
// disfarçada.
var rotasProibidas = []struct{ metodo, caminho string }{
	// Lobby, fila, seleção de campeão, chat, loja, configurações.
	{"GET", "/lol-lobby/v2/lobby"},
	{"POST", "/lol-lobby/v2/lobby"},
	{"POST", "/lol-lobby/v2/lobby/matchmaking/search"},
	{"DELETE", "/lol-lobby/v2/lobby"},
	{"GET", "/lol-matchmaking/v1/search"},
	{"POST", "/lol-matchmaking/v1/ready-check/accept"},
	{"GET", "/lol-champ-select/v1/session"},
	{"PATCH", "/lol-champ-select/v1/session/actions/1"},
	{"GET", "/lol-chat/v1/friends"},
	{"POST", "/lol-chat/v1/conversations/x/messages"},
	{"PUT", "/lol-chat/v1/me"},
	{"GET", "/lol-store/v1/catalog"},
	{"POST", "/lol-loot/v1/recipes/x/craft"},
	{"GET", "/lol-settings/v2/account/LCUPreferences"},
	{"POST", "/process-control/v1/process/quit"},
	{"POST", "/riotclient/kill-and-restart-ux"},
	// POST/PUT/PATCH/DELETE em rota de leitura, ou fora de lol-replays.
	{"POST", "/lol-summoner/v1/current-summoner"},
	{"PUT", "/lol-summoner/v1/current-summoner/icon"},
	{"POST", "/lol-match-history/v1/games/3304594274"},
	{"DELETE", "/lol-match-history/v1/games/3304594274"},
	{"PUT", "/lol-gameflow/v1/gameflow-phase"},
	{"POST", "/lol-gameflow/v1/reconnect"},
	// Métodos não previstos dentro de lol-replays.
	{"DELETE", "/lol-replays/v1/rofls/3304594274/download"},
	{"PATCH", "/lol-replays/v1/configuration"},
	{"POST", "/lol-replays/v1/configuration"},
	{"POST", "/lol-replays/v1/metadata/3304594274"},
	{"POST", "/lol-replays/v1/metadata/3304594274/create"}, // a v1 nem existe; v2 é a única
	// Disfarces: travessia, codificação, barras, parâmetros que não são número.
	{"GET", "/lol-replays/v1/metadata/../../lol-lobby/v2/lobby"},
	{"GET", "/lol-replays/v1/metadata/%2e%2e"},
	{"GET", "/lol-match-history/v1/games/3304594274/../../../lol-chat/v1/me"},
	{"GET", "/lol-match-history/v1/games/abc"},
	{"GET", "/lol-match-history/v1/games/"},
	{"GET", "/lol-match-history/v1/games//1"},
	{"GET", "/lol-summoner/v1/current-summoner/"},
	{"GET", "/lol-summoner/v1/current-summoner?x=1"},
	{"GET", "lol-summoner/v1/current-summoner"},
	{"GET", "/LOL-SUMMONER/v1/current-summoner"},
	{"GET", "/lol-match-history/v1/products/lol/a b/matches"},
	{"GET", "/lol-match-history/v1/products/lol/x/y/matches"},
	{"get", "/lol-summoner/v1/current-summoner"},
	{"GET", ""},
	{"GET", "/"},
}

func TestPermitidaRecusaRotasForaDaLista(t *testing.T) {
	for _, c := range rotasProibidas {
		if Permitida(c.metodo, c.caminho) {
			t.Errorf("deveria recusar %s %q", c.metodo, c.caminho)
		}
	}
}

// lcuDeTeste sobe um LCU falso em TLS (como o de verdade) com o lockfile
// apontando para ele, e conta quantos pedidos chegaram.
func lcuDeTeste(t *testing.T, h http.HandlerFunc) (*Real, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		if u, s, ok := r.BasicAuth(); !ok || u != "riot" || s != "senha-de-teste" {
			w.WriteHeader(401)
			return
		}
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	porta := srv.URL[strings.LastIndex(srv.URL, ":")+1:]
	lock := filepath.Join(t.TempDir(), "lockfile")
	os.WriteFile(lock, []byte(fmt.Sprintf("LeagueClient:1234:%s:senha-de-teste:https", porta)), 0o644)
	return NovoReal(lock), &n
}

func TestRealRecusaAntesDeEnviar(t *testing.T) {
	c, n := lcuDeTeste(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	for _, rota := range rotasProibidas {
		_, err := c.pede(context.Background(), rota.metodo, rota.caminho, nil, map[string]string{"x": "y"})
		var proibida *ErroRotaProibida
		if !errors.As(err, &proibida) {
			t.Errorf("%s %q: esperava ErroRotaProibida, veio %v", rota.metodo, rota.caminho, err)
		}
	}
	if got := n.Load(); got != 0 {
		t.Fatalf("%d pedido(s) proibido(s) chegaram ao LCU", got)
	}
	// E uma permitida chega, com a autenticação certa.
	if _, err := c.pede(context.Background(), "GET", "/lol-gameflow/v1/gameflow-phase", nil, nil); err != nil {
		t.Fatalf("rota permitida falhou: %v", err)
	}
	if n.Load() != 1 {
		t.Fatalf("a rota permitida não chegou")
	}
}

func TestRealLeLockfileACadaPedido(t *testing.T) {
	c, _ := lcuDeTeste(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/lol-gameflow/v1/gameflow-phase":
			w.Write([]byte(`"None"`))
		case "/lol-summoner/v1/alias/lookup":
			if r.URL.Query().Get("gameName") != "GodiNzt" || r.URL.Query().Get("tagLine") != "br1" {
				w.WriteHeader(404)
				w.Write([]byte(`{"message":"not found"}`))
				return
			}
			w.Write([]byte(`{"alias":{"gameName":"GodiNzt","tagLine":"br1"},"puuid":"p-1"}`))
		case "/lol-replays/v1/metadata/1":
			w.WriteHeader(404)
			w.Write([]byte(`{"errorCode":"RPC_ERROR","httpStatus":404,"message":"Plugin found no local metadata"}`))
		default:
			w.WriteHeader(500)
		}
	})
	ctx := context.Background()
	fase, err := c.FaseGameflow(ctx)
	if err != nil || fase != "None" {
		t.Fatalf("fase %q err %v", fase, err)
	}
	inv, err := c.BuscaAlias(ctx, "GodiNzt", "br1")
	if err != nil || inv.Puuid != "p-1" || inv.Nome != "GodiNzt" {
		t.Fatalf("alias %+v err %v", inv, err)
	}
	if _, err := c.BuscaAlias(ctx, "Outro", "br1"); !errors.Is(err, ErrNaoEncontrado) {
		t.Fatalf("alias inexistente: %v", err)
	}
	if _, err := c.MetadataReplay(ctx, 1); !errors.Is(err, ErrNaoEncontrado) {
		t.Fatalf("metadata 404: %v", err)
	}

	// O cliente reiniciou: lockfile sumiu → erro claro, sem tentar conectar.
	os.Remove(c.lockfile)
	if _, err := c.FaseGameflow(ctx); !errors.Is(err, ErrClienteFechado) {
		t.Fatalf("sem lockfile: %v", err)
	}
}

func TestRealLockfileVelhoEClienteFechado(t *testing.T) {
	lock := filepath.Join(t.TempDir(), "lockfile")
	// Porta 1: ninguém atende. É o lockfile que sobra de um cliente que caiu.
	os.WriteFile(lock, []byte("LeagueClient:1:1:x:https"), 0o644)
	if _, err := NovoReal(lock).FaseGameflow(context.Background()); !errors.Is(err, ErrClienteFechado) {
		t.Fatalf("esperava ErrClienteFechado, veio %v", err)
	}
}

func TestDiscadorSoLoopback(t *testing.T) {
	c := clienteLoopback()
	_, err := c.Get("https://example.com/")
	if err == nil || !strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatalf("deveria recusar host de fora: %v", err)
	}
}

func TestFalsoMetadataDownload(t *testing.T) {
	f := NovoFalso()
	ctx := context.Background()
	id := idFalso(0, 0)
	if _, err := f.MetadataReplay(ctx, id); !errors.Is(err, ErrNaoEncontrado) {
		t.Fatalf("antes do create: %v", err)
	}
	if err := f.CriaMetadataReplay(ctx, id, CriaMetadata{VersaoJogo: VersaoFalsa}); err != nil {
		t.Fatal(err)
	}
	if err := f.BaixaReplay(ctx, id); err != nil {
		t.Fatal(err)
	}
	var m MetadataReplay
	for i := 0; i < 10 && m.Estado != "watch"; i++ {
		m, _ = f.MetadataReplay(ctx, id)
	}
	if m.Estado != "watch" {
		t.Fatalf("download não terminou: %+v", m)
	}
	velha := idFalso(0, 20)
	f.CriaMetadataReplay(ctx, velha, CriaMetadata{VersaoJogo: "16.10.776.5552"})
	if m, _ := f.MetadataReplay(ctx, velha); m.Estado != "incompatible" {
		t.Fatalf("patch antigo: %+v", m)
	}
}

func TestTagDaRegiao(t *testing.T) {
	if TagDaRegiao("BR") != "br1" || PatchDe("16.22.834.8910") != "16.22" {
		t.Fatal("tag ou patch errado")
	}
}
