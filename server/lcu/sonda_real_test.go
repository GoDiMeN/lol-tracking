//go:build real

package lcu

// Sonda contra o cliente do LoL de verdade (precisa do cliente aberto e logado).
// Só leitura: nenhuma rota de ação (create/download/watch) é chamada aqui.
//
//	cd server && go test -tags real -run TestReal -count=1 -v ./lcu/
//
// LOL_SONDA_DIR=<pasta> grava as respostas cruas lá, para inspecionar formatos.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const lockfilePadrao = `C:\Riot Games\League of Legends\lockfile`

func clienteReal(t *testing.T) *Real {
	t.Helper()
	caminho := os.Getenv("LOL_LOCKFILE")
	if caminho == "" {
		caminho = lockfilePadrao
	}
	if _, err := os.Stat(caminho); err != nil {
		t.Skipf("cliente do LoL fechado (%s)", caminho)
	}
	return NovoReal(caminho)
}

func guarda(t *testing.T, nome string, v any) {
	dir := os.Getenv("LOL_SONDA_DIR")
	if dir == "" {
		return
	}
	var b []byte
	switch x := v.(type) {
	case json.RawMessage:
		b = x
	default:
		b, _ = json.MarshalIndent(v, "", "  ")
	}
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, nome), b, 0o644)
}

func TestRealSondaLeitura(t *testing.T) {
	c := clienteReal(t)
	ctx, cancela := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancela()

	eu, err := c.InvocadorAtual(ctx)
	if err != nil {
		t.Fatalf("current-summoner: %v", err)
	}
	t.Logf("logado: %s#%s (puuid %d caracteres)", eu.Nome, eu.Tag, len(eu.Puuid))

	reg, err := c.Regiao(ctx)
	if err != nil {
		t.Fatalf("region-locale: %v", err)
	}
	t.Logf("região: %+v → tag padrão %s", reg, TagDaRegiao(reg.Regiao))

	fase, err := c.FaseGameflow(ctx)
	if err != nil {
		t.Fatalf("gameflow-phase: %v", err)
	}
	t.Logf("fase: %s", fase)

	cfg, err := c.ConfiguracaoReplays(ctx)
	if err != nil {
		t.Fatalf("lol-replays/configuration: %v", err)
	}
	t.Logf("replays: %+v", cfg)

	pasta, err := c.PastaReplays(ctx)
	if err != nil {
		t.Fatalf("rofls/path: %v", err)
	}
	t.Logf("pasta: %s", pasta)

	achado, err := c.BuscaAlias(ctx, "GodiNzt", "br1")
	if err != nil {
		t.Fatalf("alias/lookup: %v", err)
	}
	if achado.Puuid != eu.Puuid {
		t.Errorf("alias GodiNzt#br1 deu outro puuid")
	}
	if _, err := c.BuscaAlias(ctx, "NinguemComEsseNomeXyz123", "zz9"); !errors.Is(err, ErrNaoEncontrado) {
		t.Logf("alias inexistente: err=%v (esperado ErrNaoEncontrado)", err)
	}

	hist, err := c.Historico(ctx, eu.Puuid, 0, 5)
	if err != nil {
		t.Fatalf("matches: %v", err)
	}
	guarda(t, "historico.json", hist)
	t.Logf("histórico: %d bytes", len(hist))

	// Paginação: o LCU respeita begIndex/endIndex? (no 16.22 o pedido 0–5
	// devolveu as 19 que ele tinha; aqui conferimos as páginas seguintes.)
	for _, pg := range [][2]int{{0, 10}, {10, 20}, {20, 40}} {
		b, err := c.Historico(ctx, eu.Puuid, pg[0], pg[1])
		if err != nil {
			t.Errorf("matches %v: %v", pg, err)
			continue
		}
		var r struct {
			Games struct {
				GameIndexBegin, GameIndexEnd, GameCount int
				Games                                   []struct {
					GameID      int64  `json:"gameId"`
					GameVersion string `json:"gameVersion"`
				} `json:"games"`
			} `json:"games"`
		}
		json.Unmarshal(b, &r)
		ids := []string{}
		for _, g := range r.Games.Games {
			ids = append(ids, itoa(g.GameID)+"@"+g.GameVersion)
		}
		t.Logf("página %v → begin %d end %d count %d: %v", pg, r.Games.GameIndexBegin, r.Games.GameIndexEnd, r.Games.GameCount, ids)
	}

	for _, id := range []int64{3304594274, 3304553006} {
		p, err := c.Partida(ctx, id)
		if err != nil {
			t.Errorf("games/%d: %v", id, err)
			continue
		}
		guarda(t, "partida-"+itoa(id)+".json", p)
		tl, err := c.LinhaDoTempo(ctx, id)
		if err != nil {
			t.Errorf("game-timelines/%d: %v", id, err)
		} else {
			guarda(t, "linha-"+itoa(id)+".json", tl)
		}
		m, err := c.MetadataReplay(ctx, id)
		t.Logf("partida %d: %d bytes, linha do tempo %d bytes, replay %+v err=%v", id, len(p), len(tl), m, err)
	}
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
