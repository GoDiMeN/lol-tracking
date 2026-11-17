//go:build real

package replay

// Sonda curta da janela e da trava com o jogo de verdade (cliente aberto,
// partida 3304594274 baixada). ABRE E FECHA o replay; fica aberto ~1 min.
//
//	cd server && go test -tags real -run TestRealJanela -count=1 -v ./replay/

import (
	"context"
	"image"
	"math"
	"os"
	"sort"
	"testing"
	"time"

	"loltracking/server/lcu"
)

const partidaSonda = 3304594274

func depsReais(t *testing.T) Deps {
	lock := os.Getenv("LOL_LOCKFILE")
	if lock == "" {
		lock = `C:\Riot Games\League of Legends\lockfile`
	}
	if _, err := os.Stat(lock); err != nil {
		t.Skip("cliente do LoL fechado")
	}
	// Um -timeout curto mata o processo sem rodar os defers: o replay ficaria aberto (regra 5).
	if prazo, ok := t.Deadline(); ok && time.Until(prazo) < 5*time.Minute {
		t.Skip("rode com -timeout 10m ou mais (um timeout no meio deixaria o replay aberto)")
	}
	j := NovaJanela()
	return Deps{LCU: lcu.NovoReal(lock), API: NovaAPI(EnderecoPadrao), Jogo: j, Teclado: j, Tela: j,
		Relogio: RelogioReal{}, GameCfg: GameCfgDe(lock)}
}

func TestRealJanela(t *testing.T) {
	d := depsReais(t)
	ctx, cancela := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancela()
	inicio := time.Now()
	s, err := Abre(ctx, d, partidaSonda)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ini := time.Now()
		err := s.Fecha()
		t.Logf("Fecha: %v em %s; jogo aberto depois: %v", err, time.Since(ini), d.Jogo.Aberto())
	}()
	t.Logf("abriu em %s; duração %.1f s; área do jogo %dx%d", time.Since(inicio), s.Duracao, s.Largura, s.Altura)
	for i, j := range s.Jogadores {
		k, _ := TeclaDo(s.Jogadores, j.NomeDeSelecao())
		t.Logf("  %d %s %-6s %-16s %-10s tecla %c", i, j.Time, j.Posicao, j.Nome, j.Campeao, k)
	}
	if _, err := d.API.MudaPlayback(ctx, MudaPlayback{Tempo: Ptr(360.0), Velocidade: Ptr(2.0), Pausado: Ptr(false)}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond)
	n, err := s.TravaCamera(ctx, "GodiNzt")
	t.Logf("trava GodiNzt: %d tentativas, %v", n, err)

	var sxs, sys []float64
	var tPB, tR, tPL, tCap []time.Duration
	fim := time.Now().Add(8 * time.Second)
	for time.Now().Before(fim) {
		a := time.Now()
		p1, _ := d.API.Playback(ctx)
		b := time.Now()
		r, _ := d.API.Render(ctx)
		c := time.Now()
		js, _ := d.API.Jogadores(ctx)
		e := time.Now()
		p2, _ := d.API.Playback(ctx)
		f := time.Now()
		img, err := d.Tela.Captura(image.Rectangle{})
		g := time.Now()
		tPB, tR, tPL, tCap = append(tPB, b.Sub(a), f.Sub(e)), append(tR, c.Sub(b)), append(tPL, e.Sub(c)), append(tCap, g.Sub(f))
		for _, j := range js {
			if j.Nome == "GodiNzt" && j.Centro.Valido {
				sxs, sys = append(sxs, j.Centro.X), append(sys, j.Centro.Y)
				if len(sxs)%10 == 1 {
					tam := image.Point{}
					if img != nil {
						tam = img.Bounds().Size()
					}
					t.Logf("t %.2f..%.2f sel %q cam (%.0f,%.0f) centro (%.1f,%.1f) captura %v err %v", p1.Tempo, p2.Tempo,
						r.Selecao, r.CameraPosicao.X, r.CameraPosicao.Z, j.Centro.X, j.Centro.Y, tam, err)
				}
			}
		}
	}
	med := func(v []float64) float64 {
		if len(v) == 0 {
			return math.NaN()
		}
		s := append([]float64(nil), v...)
		sort.Float64s(s)
		return s[len(s)/2]
	}
	medD := func(v []time.Duration) time.Duration {
		s := append([]time.Duration(nil), v...)
		sort.Slice(s, func(a, b int) bool { return s[a] < s[b] })
		if len(s) == 0 {
			return 0
		}
		return s[len(s)/2]
	}
	t.Logf("%d leituras; centro mediano (%.1f, %.1f), x %.0f..%.0f", len(tR), med(sxs), med(sys), minF(sxs), maxF(sxs))
	t.Logf("medianas: playback %s, render %s, playerlist %s, captura %s", medD(tPB), medD(tR), medD(tPL), medD(tCap))
}

func minF(v []float64) float64 {
	m := math.Inf(1)
	for _, x := range v {
		m = math.Min(m, x)
	}
	return m
}

func maxF(v []float64) float64 {
	m := math.Inf(-1)
	for _, x := range v {
		m = math.Max(m, x)
	}
	return m
}
