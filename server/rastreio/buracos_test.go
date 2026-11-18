package rastreio

import "testing"

func amCam(t int64, x, y float64) Amostra {
	return Amostra{TMs: t, X: &x, Y: &y, Origem: OrigemCamera, SelecaoOK: true}
}

func amPerdida(t int64) Amostra {
	return Amostra{TMs: t, Origem: OrigemInterpolada, TravaPerdida: true}
}

func TestBuracos(t *testing.T) {
	var as []Amostra
	for k := int64(0); k <= 10000; k += 250 {
		as = append(as, amCam(k, 5000, 5000))
	}
	// Vazio de 6 s (o motor parado retravando).
	for k := int64(16000); k <= 20000; k += 250 {
		as = append(as, amCam(k, 5000, 5000))
	}
	// 5 s de trava perdida (amostras marcadas, sem posição).
	for k := int64(20250); k < 25250; k += 250 {
		as = append(as, amPerdida(k))
	}
	for k := int64(25250); k <= 30000; k += 250 {
		as = append(as, amCam(k, 5000, 5000))
	}
	// Morto 15 s sem amostra no meio: não é buraco.
	m1 := amCam(30250, 5000, 5000)
	m1.Morto, m1.Origem = true, OrigemMorte
	m2 := m1
	m2.TMs = 45250
	as = append(as, m1, m2)
	bs := Buracos(as, LimiarRepescagemMs)
	if len(bs) != 2 || bs[0].DeMs != 10000 || bs[0].AteMs != 16000 || bs[0].Amostras != 0 ||
		bs[1].DeMs != 20000 || bs[1].AteMs != 25250 || bs[1].Amostras != 20 {
		t.Fatalf("%+v", bs)
	}
	if ok, _ := fechou([]Amostra{amCam(8000, 1, 1), amCam(10500, 1, 1), amCam(13000, 1, 1), amCam(15500, 1, 1), amCam(18000, 1, 1)}, bs[0], LimiarRepescagemMs); !ok {
		t.Fatal("trecho coberto deveria fechar")
	}
	if ok, pior := fechou([]Amostra{amCam(8000, 1, 1), amPerdida(12000), amCam(18000, 1, 1)}, bs[0], LimiarRepescagemMs); ok || pior != 10000 {
		t.Fatalf("trecho ainda sem câmera não fecha (%d)", pior)
	}
}

// Item 4: buraco curto entre duas leituras boas sai interpolado; longo, com
// morte no meio ou com volta à base, fica sem posição (a tela usa a linha do tempo).
func TestPosicaoNoBuraco(t *testing.T) {
	quadro := func(as []Amostra, tm int64, time int) JogadorNoQuadro {
		j := JogadorNoQuadro{}
		posicaoDasAmostras(&j, as, tm, time)
		return j
	}
	curto := []Amostra{amCam(100000, 6000, 6000), amPerdida(104000), amPerdida(108000), amCam(110000, 7000, 6000)}
	j := quadro(curto, 105000, 100)
	if j.X == nil || j.Origem != OrigemInterpolada || abs(*j.X-6500) > 1 || !j.Rastreado {
		t.Fatalf("curto: %+v", j)
	}
	// Vazio de verdade (sem amostra nenhuma no meio): o mesmo.
	j = quadro([]Amostra{curto[0], curto[3]}, 105000, 100)
	if j.X == nil || j.Origem != OrigemInterpolada || j.Vida != nil {
		t.Fatalf("vazio: %+v", j)
	}
	longo := []Amostra{amCam(100000, 6000, 6000), amCam(125000, 7000, 6000)}
	if j = quadro(longo, 112000, 100); j.X != nil {
		t.Fatalf("longo: %+v", j)
	}
	morreu := []Amostra{amCam(100000, 6000, 6000), amCam(110000, 6100, 6000)}
	morreu[1].Mortes = 1
	if j = quadro(morreu, 105000, 100); j.X != nil {
		t.Fatalf("morte no meio: %+v", j)
	}
	f := FonteDoTime(100)
	recall := []Amostra{amCam(100000, 3000, 3000), amCam(112000, f[0], f[1])}
	if j = quadro(recall, 106000, 100); j.X != nil {
		t.Fatalf("volta à base: %+v", j)
	}
	salto := []Amostra{amCam(100000, 2000, 2000), amCam(105000, 12000, 12000)}
	if j = quadro(salto, 102000, 0); j.X != nil {
		t.Fatalf("salto: %+v", j)
	}
	// Leitura boa perto: a de sempre, com a origem dela.
	if j = quadro(curto, 100200, 100); j.Origem != OrigemCamera || *j.X != 6000 {
		t.Fatalf("perto: %+v", j)
	}
}

// Item 5a: a definição única de recall. Salto para a fonte vivo = recall (também
// com um ponto no meio do caminho); chegada à fonte logo depois de morto = renascer
// com a câmera atrasada, não recall.
func TestChegadasPorRecall(t *testing.T) {
	f := FonteDoTime(100)
	var as []Amostra
	for k := int64(0); k < 8000; k += 250 {
		as = append(as, amCam(k, 6000, 6000))
	}
	as = append(as, amCam(8250, 8000, 8000)) // ponto no meio do caminho (câmera)
	as = append(as, amCam(8500, f[0], f[1]))
	for k := int64(8750); k < 20000; k += 250 {
		as = append(as, amCam(k, f[0], f[1]))
	}
	for k := int64(20000); k < 40000; k += 250 {
		as = append(as, amCam(k, 6000+float64(k-20000)/10, 6000))
	}
	morto := amCam(40000, 7000, 6000)
	morto.Morto, morto.Origem = true, OrigemMorte
	as = append(as, morto)
	as = append(as, amCam(43000, 7000, 6000)) // a câmera ainda no ponto velho ao renascer
	as = append(as, amCam(43250, f[0], f[1]))
	r := ChegadasPorRecall(as, 100)
	if len(r) != 1 || !r[33] {
		t.Fatalf("recalls %v (esperado só o índice 33)", r)
	}
	ms := marcosDe(&ContextoCadeia{Amostras: as, Time: 100})
	n := 0
	for _, m := range ms {
		if m.tipo == "recall" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("marcos %+v", ms)
	}
}
