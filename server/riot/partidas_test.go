package riot

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestIDsEPartidaV5(t *testing.T) {
	c := clienteDeTeste(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Riot-Token") != chaveDeTeste {
			t.Errorf("cabeçalho errado")
		}
		switch r.URL.Path {
		case "/lol/match/v5/matches/by-puuid/p1/ids":
			q := r.URL.Query()
			if q.Get("queue") != "420" || q.Get("start") != "0" || q.Get("count") != "100" {
				t.Errorf("consulta %v", q)
			}
			w.Write([]byte(`["BR1_2","BR1_1"]`))
		case "/lol/match/v5/matches/BR1_2":
			w.Write([]byte(`{"metadata":{"matchId":"BR1_2"},"info":{"gameId":2,"gameCreation":1794800000000,"gameDuration":1800,
				"gameVersion":"16.23.1.2","queueId":420,"mapId":11,"participants":[{"puuid":"p1","riotIdGameName":"Fulano",
				"riotIdTagline":"BR1","teamPosition":"JUNGLE","championId":64,"win":true}]}}`))
		default:
			w.WriteHeader(404)
		}
	})
	reg, _ := RegiaoPorCodigo("BR1")
	ctx := context.Background()
	// count acima do teto da Riot vira 100.
	ids, err := c.IDsPartidas(ctx, reg, "p1", 420, 0, 500)
	if err != nil || len(ids) != 2 || ids[0] != "BR1_2" {
		t.Fatal(ids, err)
	}
	p, err := c.Partida(ctx, reg, "BR1_2")
	if err != nil || p.Info.GameVersion != "16.23.1.2" || len(p.Info.Participants) != 1 {
		t.Fatal(p, err)
	}
	pt := p.Info.Participants[0]
	if pt.Puuid != "p1" || pt.TeamPosition != "JUNGLE" || pt.ChampionID != 64 || !pt.Win || pt.RiotNome != "Fulano" {
		t.Fatalf("%+v", pt)
	}
	if _, err := c.Partida(ctx, reg, "BR1_9"); !errors.Is(err, ErrNaoEncontradoRiot) {
		t.Fatal(err)
	}
	// Jogador sem partida: lista vazia, nunca nil.
	if ids, err := c.IDsPartidas(ctx, reg, "outro", 420, 0, 10); !errors.Is(err, ErrNaoEncontradoRiot) || ids == nil {
		t.Fatal(ids, err)
	}
}

func TestPartidaV5SemChaveNaoSai(t *testing.T) {
	c := Novo("nao-existe.txt", false)
	reg, _ := RegiaoPorCodigo("BR1")
	if _, err := c.IDsPartidas(context.Background(), reg, "p1", 420, 0, 10); !errors.Is(err, ErrSemChave) {
		t.Fatal(err)
	}
	if !strings.HasSuffix(rotaSemID("/lol/match/v5/matches/by-puuid/segredo/ids?count=1"), "/by-puuid") {
		t.Fatal("o puuid não pode ir para a mensagem de erro")
	}
}

// O histórico de antes de uma partida: endTime em segundos, sem fila = todas.
func TestIDsPartidasAntes(t *testing.T) {
	c := clienteDeTeste(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/lol/match/v5/matches/by-puuid/p1/ids" || q.Get("endTime") != "1794800000" || q.Get("count") != "15" ||
			q.Has("queue") || q.Get("start") != "0" {
			t.Errorf("pedido %s %v", r.URL.Path, q)
		}
		w.Write([]byte(`["BR1_1"]`))
	})
	reg, _ := RegiaoPorCodigo("BR1")
	ids, err := c.IDsPartidasAntes(context.Background(), reg, "p1", 0, 15, 1794800000999)
	if err != nil || len(ids) != 1 {
		t.Fatal(ids, err)
	}
}
