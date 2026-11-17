package rastreio

import "testing"

// amostrasCD monta amostras de 250 em 250 ms com a recarga do Q dada (nil =
// leitura nula).
func amostrasCD(cds ...*float64) []Amostra {
	var as []Amostra
	for i, v := range cds {
		x, y := 1000.0+float64(i), 2000.0
		as = append(as, Amostra{TMs: int64(i) * 250, X: &x, Y: &y, CD: Recargas{Q: v}})
	}
	return as
}

func TestHabilidadeUsada(t *testing.T) {
	casos := []struct {
		nome string
		cds  []*float64
		quer []int64 // instantes dos usos
	}{
		{"uso simples", []*float64{pf(0), pf(0), pf(8), pf(7.8), pf(7.5), pf(7.3)}, []int64{500}},
		{"nulo no meio não dispara nem atrapalha", []*float64{pf(0), nil, pf(0), pf(6), nil, pf(5.5)}, []int64{750}},
		{"ruído de uma leitura não é uso", []*float64{pf(0), pf(0), pf(9), pf(0), pf(0)}, nil},
		{"começo em recarga não é uso", []*float64{pf(5), pf(4.7), pf(4.5), pf(0)}, nil},
		{"uso logo depois de voltar (salto)", []*float64{pf(0), pf(3), pf(2.8), pf(0.2), pf(10), pf(9.8)}, []int64{250, 1000}},
		{"não aprendida não conta", []*float64{pf(RecargaNaoAprendida), pf(RecargaNaoAprendida), pf(0), pf(0)}, nil},
	}
	for _, c := range casos {
		itens := geraHabilidade(&ContextoCadeia{Amostras: amostrasCD(c.cds...)})
		var got []int64
		for _, it := range itens {
			if it.Tipo != "habilidade_usada" || it.Dados["habilidade"] != "q" {
				t.Fatalf("%s: item inesperado %+v", c.nome, it)
			}
			got = append(got, it.TMs)
		}
		if len(got) != len(c.quer) {
			t.Errorf("%s: usos em %v, queria %v", c.nome, got, c.quer)
			continue
		}
		for i := range got {
			if got[i] != c.quer[i] {
				t.Errorf("%s: usos em %v, queria %v", c.nome, got, c.quer)
			}
		}
	}
}

func TestVidaBaixaERecuperou(t *testing.T) {
	vidas := []float64{1000, 900, 280, 250, 270, 600, 650, 700, 200, 150}
	var as []Amostra
	for i, v := range vidas {
		as = append(as, Amostra{TMs: int64(i) * 250, Vida: pf(v), VidaMax: pf(1000)})
	}
	as[len(as)-1].Morto = true
	itens := geraVidaBaixa(&ContextoCadeia{Amostras: as})
	var tipos []string
	for _, it := range itens {
		tipos = append(tipos, it.Tipo)
	}
	want := []string{"vida_baixa", "recuperou"}
	if len(tipos) != 2 || tipos[0] != want[0] || tipos[1] != want[1] {
		t.Fatalf("itens = %v, queria %v (o último cair para 20%% com uma leitura só e morrer não é vida_baixa)", tipos, want)
	}
	if itens[0].TMs != 750 || itens[1].TMs != 1500 {
		t.Fatalf("tempos %d e %d", itens[0].TMs, itens[1].TMs)
	}
}

func TestSaneador(t *testing.T) {
	s := novoSaneador()
	l := func(v, vm float64) *Leitura {
		return &Leitura{Vida: pf(v), VidaMax: pf(vm), Nivel: func() *int { n := 6; return &n }()}
	}
	// 1ª leitura: precisa de confirmação.
	a := l(500, 1000)
	s.saneia(a, true, false, 6)
	if a.Vida != nil {
		t.Fatal("primeira leitura do máximo deveria esperar confirmação")
	}
	b := l(510, 1000)
	s.saneia(b, true, false, 6)
	if b.VidaMax == nil || *b.VidaMax != 1000 {
		t.Fatal("máximo confirmado deveria passar")
	}
	// Pulo absurdo de vida_max numa leitura só: nulo.
	c := l(500, 7000)
	s.saneia(c, true, false, 6)
	if c.Vida != nil || c.VidaMax != nil {
		t.Fatal("vida_max que pula de 1000 para 7000 numa leitura só deveria sair nula")
	}
	// Mudança pequena (item): passa.
	d := l(500, 1080)
	s.saneia(d, true, false, 6)
	if d.VidaMax == nil {
		t.Fatal("mudança de 8% deveria passar")
	}
	// Painel de outro jogador (nível diferente, ou trava perdida): tudo nulo.
	e := l(500, 1080)
	s.saneia(e, true, false, 7)
	if e.Vida != nil || e.Nivel != nil {
		t.Fatal("nível diferente do playerlist: painel de outro jogador")
	}
	f := l(500, 1080)
	s.saneia(f, false, false, 6)
	if f.Vida != nil {
		t.Fatal("sem a trava o painel é de outro jogador")
	}
}
