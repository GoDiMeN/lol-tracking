package analise

import (
	"testing"

	"loltracking/server/rastreio"
)

func seqAtv(itens []rastreio.ItemCadeia) []string {
	var s []string
	for _, it := range itens {
		s = append(s, it.Dados["atividade"].(string))
	}
	return s
}

func achaAtv(itens []rastreio.ItemCadeia, atv string) *rastreio.ItemCadeia {
	for i := range itens {
		if itens[i].Dados["atividade"] == atv {
			return &itens[i]
		}
	}
	return nil
}

func zonaE(t *testing.T, p Ponto, quer string) {
	t.Helper()
	if z, _ := Zona(p.X, p.Y); z != quer {
		t.Fatalf("ponto de teste %v em %s, queria %s", p, z, quer)
	}
}

// Recall: parado 10 s e depois surge na fonte. A canalização vira "recall" a partir de no máximo
// 8,5 s antes do salto; a parada anterior fica "parado". Interrompida (sai andando): nada.
func TestRecall(t *testing.T) {
	p := Ponto{3800, 9000}
	zonaE(t, p, ZSelvaAzulCima)
	var tr trajeto
	tr.anda(0, 6000, Ponto{3800, 6500}, p, cheia)
	tr.anda(6000, 16000, p, p, cheia)
	tr.anda(16000, 22000, FonteAzul, FonteAzul, cheia)
	itens := geraAtividade(ctx(3, 100, "MIDDLE", tr.as))
	r := achaAtv(itens, ARecall)
	if r == nil || r.TMs < 7000 || r.TMs > 7600 || r.Dados["fim_ms"].(int64) != 16000 {
		t.Fatalf("%v %+v", seqAtv(itens), r)
	}
	if achaAtv(itens, ANaBase) == nil {
		t.Fatalf("%v", seqAtv(itens))
	}
	// Canalização interrompida: parado 6 s e sai andando para a rota.
	var tr2 trajeto
	tr2.anda(0, 6000, p, p, cheia)
	tr2.anda(6000, 16000, p, Ponto{1124, 9000}, cheia)
	if r := achaAtv(geraAtividade(ctx(3, 100, "MIDDLE", tr2.as)), ARecall); r != nil {
		t.Fatalf("recall sem salto: %+v", r)
	}
}

// Dive: trocando com o Orianna debaixo da torre externa do meio vermelha, de pé. Depois que ela
// cai, a mesma troca é só "trocando".
func TestDive(t *testing.T) {
	eu := Ponto{8600, 8200}
	cai := func(t int64) float64 { return 1000 - float64(t)/20 }
	monta := func(eventos []rastreio.EventoLT) []rastreio.ItemCadeia {
		var tr, e8 trajeto
		tr.anda(0, 8000, eu, eu, cai)
		e8.anda(0, 8000, Ponto{8900, 8450}, Ponto{8900, 8450}, nil)
		c := ctx(3, 100, "MIDDLE", tr.as)
		c.Outros[8] = e8.as
		c.Linha = rastreio.LinhaDoTempo{Eventos: eventos}
		return geraAtividade(c)
	}
	itens := monta(nil)
	d := achaAtv(itens, ADive)
	if d == nil || d.Dados["alvo"] != "torre externa do meio" {
		t.Fatalf("%v %+v", seqAtv(itens), d)
	}
	if com := d.Dados["com"].([]int); len(com) != 1 || com[0] != 8 {
		t.Fatalf("%+v", d)
	}
	caiu := []rastreio.EventoLT{{Tipo: "BUILDING_KILL", TMs: -1000, X: f(8955), Y: f(8510), Bruto: []byte(`{"buildingType":"TOWER_BUILDING"}`)}}
	if itens := monta(caiu); achaAtv(itens, ADive) != nil || achaAtv(itens, ATrocando) == nil {
		t.Fatalf("torre no chão: %v", seqAtv(itens))
	}
}

// Invadindo: o caçador azul limpando o azul da selva vermelha. Atravessando a selva inimiga até
// uma luta na rota de baixo: gankando (não invadindo).
func TestInvadindo(t *testing.T) {
	camp := Ponto{11131, 6990}
	zonaE(t, Ponto{11200, 7050}, ZSelvaVermelhaBaixo)
	var tr trajeto
	tr.anda(0, 10000, Ponto{11200, 7050}, Ponto{11200, 7050}, cheia)
	for i := range tr.as {
		q := 0.0
		if tr.as[i].TMs >= 5000 {
			q = 8
		}
		tr.as[i].CD.Q = f(q)
	}
	_ = camp
	itens := geraAtividade(ctx(2, 100, "JUNGLE", tr.as))
	var inv *rastreio.ItemCadeia
	for i := range itens {
		if itens[i].Dados["alvo"] == "azul (lado vermelho)" && itens[i].Dados["atividade"] == AInvadindo {
			inv = &itens[i]
		}
	}
	if inv == nil {
		t.Fatalf("%v %+v", seqAtv(itens), inv)
	}
	// A caminho do gank: da selva vermelha de baixo até a rota de baixo.
	de, ate := Ponto{11500, 7500}, Ponto{12300, 2900}
	zonaE(t, de, ZSelvaVermelhaBaixo)
	zonaE(t, ate, ZRotaBaixo)
	var j2, e9 trajeto
	j2.anda(0, 14000, de, ate, cheia)
	j2.anda(14000, 20000, ate, ate, func(t int64) float64 { return 1000 - float64(t-14000)/10 })
	e9.anda(0, 20000, Ponto{12450, 3000}, Ponto{12450, 3000}, nil)
	c := ctx(2, 100, "JUNGLE", j2.as)
	c.Outros[9] = e9.as
	c.Posicoes = map[int]string{2: "JUNGLE", 9: "BOTTOM"}
	itens = geraAtividade(c)
	g := achaAtv(itens, AGankando)
	if g == nil || g.TMs > 1000 {
		t.Fatalf("%v %+v", seqAtv(itens), g)
	}
	if inv := achaAtv(itens, AInvadindo); inv != nil {
		t.Fatalf("atravessar a selva até a luta virou invasão: %v", seqAtv(itens))
	}
}

// Split push: depois da fase de rotas, farmando no topo do lado vermelho com os quatro aliados
// no meio/baixo. Com um aliado junto, é só farmando.
func TestSplitPush(t *testing.T) {
	topoVerm := Ponto{6500, 13727}
	zonaE(t, topoVerm, ZRotaTopo)
	t0 := Param.FaseRotasMs + 60000
	monta := func(ajudante bool) []rastreio.ItemCadeia {
		var tr trajeto
		tr.anda(t0, t0+30000, topoVerm, topoVerm, cheia)
		c := ctx(1, 100, "TOP", tr.as)
		for pid, p := range map[int]Ponto{2: {9000, 5000}, 3: {7000, 7000}, 4: {11500, 1500}, 5: {11000, 1600}} {
			if ajudante && pid == 5 {
				p = Ponto{6700, 13600}
			}
			var a trajeto
			a.anda(t0, t0+30000, p, p, nil)
			c.Outros[pid] = a.as
		}
		c.Linha = rastreio.LinhaDoTempo{Quadros: []rastreio.QuadroLT{
			{TMs: t0 - 60000, Jogadores: map[int]rastreio.JogadorLT{1: {CS: 100}}},
			{TMs: t0, Jogadores: map[int]rastreio.JogadorLT{1: {CS: 106}}},
			{TMs: t0 + 60000, Jogadores: map[int]rastreio.JogadorLT{1: {CS: 114}}},
		}}
		return geraAtividade(c)
	}
	if itens := monta(false); achaAtv(itens, ASplitPush) == nil {
		t.Fatalf("%v", seqAtv(itens))
	}
	if itens := monta(true); achaAtv(itens, ASplitPush) != nil || achaAtv(itens, AFarmando) == nil {
		t.Fatalf("com aliado junto: %v", seqAtv(itens))
	}
}

// Agrupado: ele e três aliados no meio, sem luta.
func TestAgrupado(t *testing.T) {
	var tr trajeto
	tr.anda(0, 20000, meio, meio, cheia)
	c := ctx(3, 100, "MIDDLE", tr.as)
	for pid, p := range map[int]Ponto{1: {7400, 7100}, 2: {6700, 6800}, 4: {7100, 7500}, 5: {2000, 12000}} {
		var a trajeto
		a.anda(0, 20000, p, p, nil)
		c.Outros[pid] = a.as
	}
	itens := geraAtividade(c)
	g := achaAtv(itens, AAgrupado)
	if g == nil {
		t.Fatalf("%v", seqAtv(itens))
	}
	if com := g.Dados["com"].([]int); len(com) != 3 || com[0] != 1 || com[2] != 4 {
		t.Fatalf("%+v", g)
	}
}

// Sentinelas: prazo da amarela pelo nível, limite de 3 (a quarta derruba a mais velha), de
// controle substituída pela seguinte e destruição casada pela proximidade de quem destruiu.
func TestMontaSentinelas(t *testing.T) {
	var v7, l5 trajeto
	v7.anda(0, 600000, Ponto{11600, 6600}, Ponto{11600, 6600}, nil)
	l5.anda(0, 600000, Ponto{11700, 6700}, Ponto{11700, 6700}, nil)
	amostras := map[int][]rastreio.Amostra{7: v7.as, 5: l5.as}
	timeDe := map[int]int{5: 100, 7: 200, 9: 200}
	lt := rastreio.LinhaDoTempo{Quadros: []rastreio.QuadroLT{{TMs: 0, Jogadores: map[int]rastreio.JogadorLT{1: {Nivel: 1}, 2: {Nivel: 1}}}}}
	evs := []eventoSentinela{
		{tMs: 10000, quem: 7, tipo: TipoTotem, colocada: true},
		{tMs: 20000, quem: 7, tipo: TipoControle, colocada: true},
		{tMs: 30000, quem: 9, tipo: TipoStealth, colocada: true},
		{tMs: 31000, quem: 9, tipo: TipoStealth, colocada: true},
		{tMs: 32000, quem: 9, tipo: TipoStealth, colocada: true},
		{tMs: 33000, quem: 9, tipo: TipoTotem, colocada: true}, // quarta: derruba a de 30 s
		{tMs: 50000, quem: 5, tipo: TipoControle, colocada: false},
		{tMs: 60000, quem: 7, tipo: "UNDEFINED", colocada: true},
	}
	s, d := montaSentinelas(evs, timeDe, amostras, lt, 900000)
	if len(s) != 7 || len(d) != 1 {
		t.Fatalf("%d %d", len(s), len(d))
	}
	if *s[0].AteMs != 100000 || s[0].Fim != "expirou" || s[0].X == nil || *s[0].Raio != 700 {
		t.Fatalf("amarela: %+v", s[0])
	}
	if s[1].Fim != "destruida" || *s[1].AteMs != 50000 || *s[1].DestruidaPor != 5 || d[0].Casada == nil || *d[0].Casada != 1 {
		t.Fatalf("controle: %+v %+v", s[1], d[0])
	}
	if s[2].Fim != "substituida" || *s[2].AteMs != 33000 || s[2].X != nil || s[2].Rastreado {
		t.Fatalf("limite: %+v", s[2])
	}
	if s[6].Comum || s[6].AteMs != nil || s[6].Fim != "desconhecido" || s[6].TipoNome != "UNDEFINED" {
		t.Fatalf("tipo cru: %+v", s[6])
	}
}

// Checagem da análise: "farmando" com ele morto, pisca-pisca e aproximação sem luta.
func TestConfereEtiquetas(t *testing.T) {
	var tr trajeto
	tr.anda(0, 10000, meio, meio, cheia)
	tr.morre(10000, 20000, meio)
	tr.anda(20000, 60000, meio, meio, cheia)
	atv := func(ini, fim int64, a string) rastreio.ItemCadeia {
		return rastreio.ItemCadeia{TMs: ini, Tipo: "atividade", Dados: map[string]any{"atividade": a, "fim_ms": float64(fim)}}
	}
	cad := []rastreio.ItemCadeia{
		atv(0, 10000, AFarmando), atv(10000, 20000, AFarmando), // morto e "farmando": impossível
		atv(20000, 22000, ALutando), atv(22000, 24000, AAndando), atv(24000, 26000, ALutando), atv(26000, 28000, AAndando),
		atv(28000, 30000, ALutando),                                    // 4 trocas em 8 s
		atv(40000, 45000, ARotacionando), atv(45000, 60000, AFarmando), // sem luta depois
	}
	q := &QualidadePasse{Contagem: map[string]int{}, Pontos: []PontoQualidade{}}
	confereEtiquetas(q, cad, tr.as, 100)
	if q.Contagem[QEtiquetaImpossivel] != 1 || q.Contagem[QEtiquetaPiscando] != 1 || q.Contagem[QAproxSemLuta] != 1 {
		t.Fatalf("%+v", q)
	}
}
