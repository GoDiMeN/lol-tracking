package rastreio

import (
	"math"
	"math/rand"
	"reflect"
	"testing"
)

func pf(v float64) *float64 { return &v }

func TestCodecIdaEVolta(t *testing.T) {
	b := Bloco{Versao: VersaoFormato, N: 6, Colunas: []Coluna{
		{Nome: "t_ms", Escala: 1, Ints: []int64{0, 250, 500, 760, 1000, 1250}},
		{Nome: "x", Escala: 1, Ints: []int64{100, 0, 120, -5, 0, 140}, Nulos: []bool{false, true, false, false, true, false}},
		{Nome: "constante", Escala: 10, Ints: []int64{7, 7, 7, 7, 7, 7}},
		{Nome: "todos_nulos", Escala: 1, Ints: make([]int64, 6), Nulos: []bool{true, true, true, true, true, true}},
		{Nome: "origem", Texto: true, Textos: []string{"camera", "camera", "morte", "", "camera", "interpolada"},
			Nulos: []bool{false, false, false, true, false, false}},
		{Nome: "grande", Escala: 1, Ints: []int64{math.MaxInt64 / 2, math.MinInt64 / 2, 0, 1, -1, 3}},
	}}
	blob, err := Codifica(b)
	if err != nil {
		t.Fatal(err)
	}
	d, err := Decodifica(blob)
	if err != nil {
		t.Fatal(err)
	}
	if d.N != 6 || len(d.Colunas) != len(b.Colunas) {
		t.Fatalf("%+v", d)
	}
	for i, c := range b.Colunas {
		dc := d.Col(c.Nome)
		for k := 0; k < b.N; k++ {
			if c.Nulo(k) != dc.Nulo(k) {
				t.Fatalf("%s[%d] nulo %v x %v", c.Nome, k, c.Nulo(k), dc.Nulo(k))
			}
			if c.Nulo(k) {
				continue
			}
			if c.Texto && c.Textos[k] != dc.Textos[k] || !c.Texto && c.Ints[k] != dc.Ints[k] {
				t.Fatalf("coluna %d %s[%d] diferente", i, c.Nome, k)
			}
		}
	}
	if d.Col("nao_existe") != nil {
		t.Fatal("coluna inexistente")
	}
	if _, err := Decodifica([]byte("xxxx")); err == nil {
		t.Fatal("lixo deveria falhar")
	}
	if _, err := Decodifica(blob[:len(blob)/2]); err == nil {
		t.Fatal("blob cortado deveria falhar")
	}
}

// amostrasSinteticas imita um passe de verdade: 4 por segundo, andando,
// com mortes, trava perdida às vezes e as colunas da tela nulas.
func amostrasSinteticas(n int) []Amostra {
	as := make([]Amostra, n)
	r := rand.New(rand.NewSource(7))
	x, y := 1000.0, 1000.0
	tm := int64(0)
	for i := range as {
		a := &as[i]
		// O tempo real anda 250 ms ± o ritmo do laço; a posição anda com ruído.
		tm += 230 + r.Int63n(45)
		a.TMs = tm
		ang := float64(i)/40 + r.Float64()*0.6
		parado := (i/80)%3 == 0
		if !parado {
			x += 85*math.Cos(ang) + r.Float64()*20 - 10
			y += 85*math.Sin(ang*0.7) + r.Float64()*20 - 10
		}
		a.Morto = i%2000 > 1900
		a.Origem = OrigemCamera
		if a.Morto {
			a.Origem = OrigemMorte
			a.Respawn = float64(2000-i%2000) / 4
		} else if i%500 == 7 {
			a.Origem = OrigemInterpolada
			a.TravaPerdida = true
		}
		xv, yv := math.Round(x), math.Round(y)
		a.X, a.Y = &xv, &yv
		// A câmera atrasa e o campeão sai do centro: tela com décimos de pixel.
		cx, cz := math.Round(xv-3-r.Float64()*60), math.Round(yv-1250-r.Float64()*60)
		a.CamX, a.CamZ = &cx, &cz
		a.TelaX, a.TelaY = pf(math.Round((1280+r.Float64()*80-40)*10)/10), pf(math.Round((720+r.Float64()*60-30)*10)/10)
		a.SelecaoOK = !a.Morto
		a.Nivel = 1 + i/600
		a.CS = i / 30
		a.Abates, a.Mortes, a.Assistencias = i/3000, i/2000, i/1500
		a.Ward = float64(i/400) * 1.25
		a.DtMs = 20 + r.Int63n(25)
	}
	return as
}

func estadosSinteticos(segundos int) []EstadoJogador {
	var es []EstadoJogador
	for s := 0; s < segundos; s++ {
		for p := 1; p <= 10; p++ {
			e := EstadoJogador{TMs: int64(s)*1000 + 13, ParticipanteID: p, Nivel: 1 + s/130, CS: s * p / 60, Ward: float64(s/90) * 1.5,
				Abates: s / (300 + p*10), Mortes: s / 400, Assistencias: s / 350}
			e.Slots[0] = SlotItem{ID: 1055, Qtd: 1}
			if s > 300 {
				e.Slots[1] = SlotItem{ID: 3006, Qtd: 1}
			}
			if s > 900 {
				e.Slots[2] = SlotItem{ID: 3031, Qtd: 1}
			}
			e.Slots[6] = SlotItem{ID: 3340, Qtd: 1}
			es = append(es, e)
		}
	}
	return es
}

func TestAmostrasIdaEVolta(t *testing.T) {
	as := amostrasSinteticas(3000)
	as[5].Vida, as[5].VidaMax, as[5].CD.R = pf(812), pf(1274), pf(61.4)
	as[6].X, as[6].Y = nil, nil
	blob, err := Codifica(BlocoDeAmostras(as))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Decodifica(blob)
	if err != nil {
		t.Fatal(err)
	}
	volta := AmostrasDoBloco(b)
	for i := range as {
		if !reflect.DeepEqual(as[i], volta[i]) {
			t.Fatalf("amostra %d:\n%+v\n%+v", i, as[i], volta[i])
		}
	}
	es := estadosSinteticos(120)
	blobE, _ := Codifica(BlocoDeEstados(es))
	be, err := Decodifica(blobE)
	if err != nil {
		t.Fatal(err)
	}
	ve := EstadosDoBloco(be)
	conta := map[int64]EstadoJogador{}
	for _, e := range ve {
		conta[e.TMs*100+int64(e.ParticipanteID)] = e
	}
	for _, e := range es {
		if !reflect.DeepEqual(conta[e.TMs*100+int64(e.ParticipanteID)], e) {
			t.Fatalf("estado %+v", e)
		}
	}
}

// Blob antigo (sem uma coluna nova) continua lendo: a coluna vem nula.
func TestColunaNovaCompativel(t *testing.T) {
	as := amostrasSinteticas(10)
	b := BlocoDeAmostras(as)
	var sem []Coluna
	for _, c := range b.Colunas {
		if c.Nome != "vida" && c.Nome != "cs" {
			sem = append(sem, c)
		}
	}
	b.Colunas = sem
	blob, _ := Codifica(b)
	d, _ := Decodifica(blob)
	volta := AmostrasDoBloco(d)
	if volta[3].Vida != nil || volta[3].CS != 0 || *volta[3].X != *as[3].X {
		t.Fatalf("%+v", volta[3])
	}
}

// TestTamanhoDoBloco mede o tamanho para uma partida de 39 min.
func TestTamanhoDoBloco(t *testing.T) {
	n := 39 * 60 * 4
	as := amostrasSinteticas(n)
	bloco := BlocoDeAmostras(as)
	blob, _ := Codifica(bloco)
	es := estadosSinteticos(39 * 60)
	blobE, _ := Codifica(BlocoDeEstados(es))
	porAmostra := float64(len(blob)) / float64(n)
	t.Logf("amostras: %d em %d bytes = %.2f bytes/amostra; estado dos 10 (%d linhas): %d bytes",
		n, len(blob), porAmostra, len(es), len(blobE))
	t.Logf("partida de 39 min × 10 passes ≈ %.1f KB (amostras) + %.1f KB (estado dos 10, um por passe)",
		float64(len(blob))*10/1024, float64(len(blobE))*10/1024)
	if porAmostra > 12 {
		t.Fatalf("bloco grande demais: %.2f bytes/amostra", porAmostra)
	}
}
