package visao

import (
	"encoding/json"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	"loltracking/server/rastreio"
	"loltracking/server/replay"
)

// As capturas de 2026-11-17 (partida 3304217746, 10:03 de jogo, pausado):
// o mesmo instante com a névoa em "todos" (0 e 3), na visão azul (1, F1) e na
// vermelha (2, F2). *_mini.png é o recorte replay.RegiaoMinimapa; *_barra.png
// é o painel do replay a partir de (0, 1290), 760×150.

func abrePNG(t *testing.T, nome string) *image.RGBA {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "minimapa", nome))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	r := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			r.Set(x, y, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return r
}

type posicoesTeste struct {
	TMs       int64 `json:"t_ms"`
	Jogadores []struct {
		ParticipanteID int     `json:"participante_id"`
		Campeao        string  `json:"campeao"`
		Time           int     `json:"time"`
		X              float64 `json:"x"`
		Y              float64 `json:"y"`
		Morto          bool    `json:"morto"`
	} `json:"jogadores"`
}

func lePosicoes(t *testing.T) posicoesTeste {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "minimapa", "posicoes_603.json"))
	if err != nil {
		t.Fatal(err)
	}
	var p posicoesTeste
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func leMini(t *testing.T, l *Leitor, nome string) rastreio.LeituraMinimapa {
	t.Helper()
	lt, err := l.LeMinimapa(abrePNG(t, nome), replay.RegiaoMinimapa, refLargura, refAltura)
	if err != nil {
		t.Fatalf("%s: %v", nome, err)
	}
	return lt
}

func TestPainelNevoa(t *testing.T) {
	regiao := image.Rect(0, 1290, 760, 1440)
	casos := map[string]string{"0-inicial_barra.png": rastreio.ModoNevoaTodos, "1-F1_barra.png": rastreio.ModoNevoaAzul,
		"2-F2_barra.png": rastreio.ModoNevoaVermelho, "3-F3_barra.png": rastreio.ModoNevoaTodos}
	for arq, esperado := range casos {
		if m := ModoNevoa(abrePNG(t, arq), regiao); m != esperado {
			t.Errorf("%s: modo %q, esperado %q", arq, m, esperado)
		}
	}
	// Sem a caixa na captura (o minimapa sozinho): não lido.
	if m := ModoNevoa(abrePNG(t, "1-F1_mini.png"), replay.RegiaoMinimapa); m != "" {
		t.Errorf("sem a caixa: %q", m)
	}
}

// Os retratos achados nas 4 capturas, contra o que se vê nelas: em "todos",
// os 7 vivos (a Senna fora do lugar rastreado, ver abaixo); na visão azul
// somem a Vi e a Senna; na vermelha some a Leona. Geometria: cada retrato
// casado a ≤ 2,5 px da posição rastreada.
func TestRetratosNasCapturas(t *testing.T) {
	l, err := Novo()
	if err != nil {
		t.Fatal(err)
	}
	pos := lePosicoes(t)
	esperados := map[string][]string{
		"0-inicial_mini.png": {"Diana", "Yunara", "Leona", "Garen", "Vi", "Orianna"},
		"1-F1_mini.png":      {"Diana", "Yunara", "Leona", "Garen", "Orianna"},
		"2-F2_mini.png":      {"Diana", "Yunara", "Garen", "Vi", "Orianna"},
		"3-F3_mini.png":      {"Diana", "Yunara", "Leona", "Garen", "Vi", "Orianna"},
	}
	// A Senna (vermelha) aparece em (14234, 11735) em "todos" e na visão
	// vermelha, a ~7 800 unidades de onde o passe dela (e o quadro de 10:00 da
	// linha do tempo do LCU) a põe: o anel existe e não casa com ninguém.
	sennaNoMinimapa := map[string]bool{"0-inicial_mini.png": true, "2-F2_mini.png": true, "3-F3_mini.png": true}
	for arq, nomes := range esperados {
		lt := leMini(t, l, arq)
		achou := map[string]float64{}
		sobra := 0
		for _, r := range lt.Retratos {
			quem, dist := "", math.Inf(1)
			for _, j := range pos.Jogadores {
				if j.Time != r.Time || j.Morto {
					continue
				}
				if d := replay.DistanciaMinimapa(r.X, r.Y, j.X, j.Y); d < dist {
					quem, dist = j.Campeao, d
				}
			}
			if dist <= 2.5 {
				achou[quem] = dist
			} else {
				sobra++
				if !sennaNoMinimapa[arq] || r.Time != 200 || replay.DistanciaMinimapa(r.X, r.Y, 14234, 11735) > 3 {
					t.Errorf("%s: retrato %d em (%.0f, %.0f) não esperado", arq, r.Time, r.X, r.Y)
				}
			}
		}
		if len(achou) != len(nomes) {
			t.Errorf("%s: achou %v, esperado %v", arq, achou, nomes)
		}
		for _, n := range nomes {
			if _, ok := achou[n]; !ok {
				t.Errorf("%s: faltou o retrato de %s", arq, n)
			}
		}
		if sennaNoMinimapa[arq] != (sobra == 1) {
			t.Errorf("%s: %d retrato(s) sem dono", arq, sobra)
		}
	}
}

// Sentinelas nas capturas: na visão azul, as 2 azuis (rio do meio e rio de
// baixo); na vermelha, as 2 vermelhas inteiras (rio do meio e rio de baixo).
// Ficam de fora, de propósito: a vermelha meio coberta pelo retrato da Vi e
// um marcador vermelho de outro desenho no rio de cima (tipo não conhecido).
// Em "todos" a azul do rio de baixo some embaixo da vermelha.
func TestSentinelasNasCapturas(t *testing.T) {
	l, err := Novo()
	if err != nil {
		t.Fatal(err)
	}
	type s struct {
		time int
		x, y float64
	}
	esperado := map[string][]s{
		"1-F1_mini.png":      {{100, 6402, 8325}, {100, 12222, 4401}},
		"2-F2_mini.png":      {{200, 6711, 8736}, {200, 12222, 4510}},
		"0-inicial_mini.png": {{100, 6380, 8325}, {200, 6711, 8736}, {200, 12222, 4510}},
	}
	for arq, es := range esperado {
		lt := leMini(t, l, arq)
		if len(lt.Sentinelas) != len(es) {
			t.Errorf("%s: %d sentinelas, esperado %d: %+v", arq, len(lt.Sentinelas), len(es), lt.Sentinelas)
			continue
		}
		for _, e := range es {
			ok := false
			for _, m := range lt.Sentinelas {
				if m.Time == e.time && replay.DistanciaMinimapa(m.X, m.Y, e.x, e.y) <= 1.5 {
					ok = true
				}
			}
			if !ok {
				t.Errorf("%s: faltou a sentinela %d em (%.0f, %.0f): %+v", arq, e.time, e.x, e.y, lt.Sentinelas)
			}
		}
	}
}

// A decisão de visibilidade nas capturas, com as posições rastreadas.
func TestVisibilidadeNasCapturas(t *testing.T) {
	l, err := Novo()
	if err != nil {
		t.Fatal(err)
	}
	pos := lePosicoes(t)
	var js []rastreio.PosicaoNoInstante
	nome := map[int]string{}
	for _, j := range pos.Jogadores {
		js = append(js, rastreio.PosicaoNoInstante{ParticipanteID: j.ParticipanteID, Time: j.Time, TemPosicao: true, X: j.X, Y: j.Y, Morto: j.Morto})
		nome[j.ParticipanteID] = j.Campeao
	}
	casos := []struct {
		arq        string
		observador int
		esperado   map[string]string
		aliados    [2]int
	}{
		{"1-F1_mini.png", 100, map[string]string{"Garen": rastreio.VisVisivel, "Orianna": rastreio.VisVisivel,
			"Vi": rastreio.VisFora, "Senna": rastreio.VisFora}, [2]int{3, 3}},
		{"2-F2_mini.png", 200, map[string]string{"Diana": rastreio.VisVisivel, "Yunara": rastreio.VisVisivel,
			"Leona": rastreio.VisFora}, [2]int{3, 4}},
	}
	for _, c := range casos {
		lt := leMini(t, l, c.arq)
		r := rastreio.ClassificaCaptura(lt, true, c.observador, js)
		got := map[string]string{}
		for pid, e := range r.Estados {
			got[nome[pid]] = e
		}
		if len(got) != len(c.esperado) {
			t.Errorf("%s: %v, esperado %v", c.arq, got, c.esperado)
		}
		for n, e := range c.esperado {
			if got[n] != e {
				t.Errorf("%s: %s %q (motivo %q), esperado %q", c.arq, n, got[n], r.Motivos[pidDe(nome, n)], e)
			}
		}
		if r.AliadosAchados != c.aliados[0] || r.AliadosVivos != c.aliados[1] {
			t.Errorf("%s: aliados %d de %d, esperado %v", c.arq, r.AliadosAchados, r.AliadosVivos, c.aliados)
		}
		t.Logf("%s (visão %d): %v; aliados achados %d de %d; anéis sem dono %d", c.arq, c.observador, got, r.AliadosAchados, r.AliadosVivos, r.Sobras)
	}
	// Painel sem conferir: tudo indeterminado.
	r := rastreio.ClassificaCaptura(leMini(t, l, "0-inicial_mini.png"), false, 100, js)
	for pid, e := range r.Estados {
		if e != rastreio.VisIndeterminado || r.Motivos[pid] != "painel" {
			t.Errorf("sem o painel: %s %s %s", nome[pid], e, r.Motivos[pid])
		}
	}
	// Sem posição (não rastreado): indeterminado, mesmo com o retrato à vista.
	semGaren := append([]rastreio.PosicaoNoInstante(nil), js...)
	for i := range semGaren {
		if nome[semGaren[i].ParticipanteID] == "Garen" {
			semGaren[i].TemPosicao = false
		}
	}
	r = rastreio.ClassificaCaptura(leMini(t, l, "1-F1_mini.png"), true, 100, semGaren)
	for pid, e := range r.Estados {
		want := map[string]string{"Garen": rastreio.VisIndeterminado, "Orianna": rastreio.VisVisivel, "Vi": rastreio.VisFora, "Senna": rastreio.VisFora}[nome[pid]]
		if e != want {
			t.Errorf("Garen sem posição: %s %s (%s), esperado %s", nome[pid], e, r.Motivos[pid], want)
		}
	}
}

func pidDe(nome map[int]string, n string) int {
	for pid, x := range nome {
		if x == n {
			return pid
		}
	}
	return 0
}

// Custo da leitura de uma captura (a 8× o ciclo do passe de visão é de ~31 ms).
func BenchmarkLeMinimapa(b *testing.B) {
	l, _ := Novo()
	t := &testing.T{}
	img := abrePNG(t, "0-inicial_mini.png")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.LeMinimapa(img, replay.RegiaoMinimapa, refLargura, refAltura)
	}
}

func TestMinimapaDiagnostico(t *testing.T) {
	if os.Getenv("LOL_DIAG") == "" {
		t.Skip("LOL_DIAG=1 para listar o que a leitura acha em cada captura")
	}
	l, err := Novo()
	if err != nil {
		t.Fatal(err)
	}
	pos := lePosicoes(t)
	for _, arq := range []string{"0-inicial_mini.png", "1-F1_mini.png", "2-F2_mini.png", "3-F3_mini.png"} {
		lt := leMini(t, l, arq)
		t.Logf("== %s", arq)
		for _, r := range lt.Retratos {
			quem, dist := "", math.Inf(1)
			for _, j := range pos.Jogadores {
				if j.Time != r.Time || j.Morto {
					continue
				}
				if d := replay.DistanciaMinimapa(r.X, r.Y, j.X, j.Y); d < dist {
					quem, dist = j.Campeao, d
				}
			}
			t.Logf("  retrato %d (%.0f, %.0f) cobertura %.2f — mais perto: %s a %.1f px", r.Time, r.X, r.Y, r.Cobertura, quem, dist)
		}
		for _, s := range lt.Sentinelas {
			px, py := replay.MapaParaMinimapa(s.X, s.Y, refLargura, refAltura)
			t.Logf("  sentinela %d (%.0f, %.0f) px (%.0f, %.0f) semelhança %.2f", s.Time, s.X, s.Y, px-2195, py-1070, s.Cobertura)
		}
	}
}
