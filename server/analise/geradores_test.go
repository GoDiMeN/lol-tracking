package analise

import (
	"testing"

	"loltracking/server/rastreio"
)

// trajeto monta amostras sintéticas a 4 por segundo.
type trajeto struct{ as []rastreio.Amostra }

func f(v float64) *float64 { return &v }

// anda vai de a até b entre t0 e t1 (ms), com a vida do painel dada por vida(t).
func (tr *trajeto) anda(t0, t1 int64, a, b Ponto, vida func(t int64) float64) {
	for t := t0; t < t1; t += 250 {
		fr := float64(t-t0) / float64(t1-t0)
		am := rastreio.Amostra{TMs: t, X: f(a.X + (b.X-a.X)*fr), Y: f(a.Y + (b.Y-a.Y)*fr), Origem: rastreio.OrigemCamera, Nivel: 5}
		if vida != nil {
			am.Vida, am.VidaMax = f(vida(t)), f(1000)
			am.Mana, am.ManaMax = f(300), f(500)
		}
		tr.as = append(tr.as, am)
	}
}

func (tr *trajeto) morre(t0, t1 int64, p Ponto) {
	for t := t0; t < t1; t += 250 {
		tr.as = append(tr.as, rastreio.Amostra{TMs: t, X: f(p.X), Y: f(p.Y), Origem: rastreio.OrigemMorte, Morto: true})
	}
}

func cheia(int64) float64 { return 1000 }

func ctx(pid, time int, pos string, as []rastreio.Amostra) *rastreio.ContextoCadeia {
	times := map[int]int{}
	for i := 1; i <= 10; i++ {
		times[i] = 100
		if i > 5 {
			times[i] = 200
		}
	}
	return &rastreio.ContextoCadeia{GameID: 1, ParticipanteID: pid, Time: time, Posicao: pos, Amostras: as, Times: times,
		Outros: map[int][]rastreio.Amostra{}}
}

func tipos(itens []rastreio.ItemCadeia, tipo string) []rastreio.ItemCadeia {
	var r []rastreio.ItemCadeia
	for _, it := range itens {
		if it.Tipo == tipo {
			r = append(r, it)
		}
	}
	return r
}

var (
	meio     = Ponto{7000, 7000}
	meio2    = Ponto{6600, 6650}
	rioCima  = Ponto{5300, 9200}
	selvaAzC = Ponto{3800, 7900}
	topo     = Ponto{1124, 9000}
)

// Um pisca de 1 s no rio não troca a zona; 10 s trocam, e a troca começa
// quando ele entrou.
func TestMudouZonaComHisterese(t *testing.T) {
	var tr trajeto
	tr.anda(0, 10000, meio, meio2, cheia)
	tr.anda(10000, 11000, Ponto{6200, 8700}, Ponto{6200, 8700}, cheia) // 1 s no rio
	tr.anda(11000, 20000, meio2, meio, cheia)
	tr.anda(20000, 30000, rioCima, rioCima, cheia)
	itens := geraMudouZona(ctx(3, 100, "MIDDLE", tr.as))
	if len(itens) != 1 || itens[0].TMs != 20000 || itens[0].Dados["de"] != ZRotaMeio || itens[0].Dados["para"] != ZRioCima {
		t.Fatalf("%+v", itens)
	}
}

// Saída do meio para o rio com abate no meio do caminho, volta; depois outra
// saída que termina em morte.
func TestSaidaDaRota(t *testing.T) {
	var tr trajeto
	tr.anda(0, 20000, meio, meio2, func(int64) float64 { return 350 }) // sai com 35% de vida
	tr.anda(20000, 35000, rioCima, rioCima, cheia)
	tr.anda(35000, 50000, meio2, meio, cheia)
	tr.anda(50000, 60000, selvaAzC, selvaAzC, cheia)
	tr.morre(60000, 70000, selvaAzC)
	c := ctx(3, 100, "MIDDLE", tr.as)
	c.Linha = rastreio.LinhaDoTempo{Eventos: []rastreio.EventoLT{
		{Tipo: "CHAMPION_KILL", TMs: 30000, Quem: 3, EmQuem: 8, X: f(5300), Y: f(9200)},
		{Tipo: "CHAMPION_KILL", TMs: 60000, Quem: 7, EmQuem: 3, X: f(3800), Y: f(7900)},
	}}
	itens := geraRota(c)
	saidas := tipos(itens, "saiu_da_rota")
	if len(saidas) != 2 || len(tipos(itens, "voltou_para_rota")) != 1 {
		t.Fatalf("%+v", itens)
	}
	s := saidas[0].Dados
	if saidas[0].TMs != 20000 || s["duracao_ms"] != int64(15000) || s["para"] != ZRioCima || s["desfecho"] != "abate" ||
		s["fim_motivo"] != "voltou" || *(s["vida_pct"].(*float64)) != 35 || s["fase"] != "rotas" {
		t.Fatalf("primeira saída %+v", s)
	}
	s = saidas[1].Dados
	if saidas[1].TMs != 50000 || s["fim_motivo"] != "morreu" || s["desfecho"] != "morte" || s["para"] != ZSelvaAzulCima {
		t.Fatalf("segunda saída %+v", s)
	}
	// Caçador não tem saída: tem entrada em rota.
	var cj trajeto
	cj.anda(0, 10000, selvaAzC, selvaAzC, cheia)
	cj.anda(10000, 22000, topo, topo, cheia)
	cj.anda(22000, 30000, selvaAzC, selvaAzC, cheia)
	ent := geraRota(ctx(2, 100, "JUNGLE", cj.as))
	if len(ent) != 1 || ent[0].Tipo != "entrou_em_rota" || ent[0].Dados["rota"] != ZRotaTopo || ent[0].TMs != 10000 {
		t.Fatalf("%+v", ent)
	}
}

// Recall: salto para a fonte; renascer: depois de morto.
func TestNaBase(t *testing.T) {
	var tr trajeto
	tr.anda(0, 10000, meio, meio, cheia)
	tr.anda(10000, 20000, FonteAzul, FonteAzul, cheia)
	tr.anda(20000, 30000, meio, meio, cheia)
	tr.morre(30000, 40000, meio)
	tr.anda(40000, 45000, FonteAzul, FonteAzul, cheia)
	itens := geraNaBase(ctx(3, 100, "MIDDLE", tr.as))
	if len(itens) != 2 || itens[0].Dados["chegada"] != "recall" || itens[1].Dados["chegada"] != "renasceu" {
		t.Fatalf("%+v", itens)
	}
	// O recall não é saída da rota.
	if s := tipos(geraRota(ctx(3, 100, "MIDDLE", tr.as)), "saiu_da_rota"); len(s) != 0 {
		t.Fatalf("%+v", s)
	}
}

func estadosCom(pid int, t0, t1 int64, vida func(t int64) float64) []rastreio.EstadoJogador {
	var es []rastreio.EstadoJogador
	for t := t0; t < t1; t += 1000 {
		es = append(es, rastreio.EstadoJogador{TMs: t, ParticipanteID: pid, VidaPct: f(vida(t))})
	}
	return es
}

// Atividade: farmando (CS subindo, ninguém perto), trocando com o 8 (perto e a
// vida caindo), lutando (dois inimigos), morto.
func TestAtividade(t *testing.T) {
	var tr trajeto
	cai := func(t int64) float64 { return 1000 - float64(t-60000)/10 } // −10% por s
	tr.anda(0, 60000, meio, meio2, cheia)
	tr.anda(60000, 66000, meio2, meio2, cai)
	tr.anda(66000, 72000, meio2, meio2, func(t int64) float64 { return 400 - float64(t-66000)/20 })
	tr.morre(72000, 80000, meio2)
	c := ctx(3, 100, "MIDDLE", tr.as)
	c.Linha = rastreio.LinhaDoTempo{Quadros: []rastreio.QuadroLT{
		{TMs: 0, Jogadores: map[int]rastreio.JogadorLT{3: {CS: 0}}},
		{TMs: 60000, Jogadores: map[int]rastreio.JogadorLT{3: {CS: 8}}},
		{TMs: 120000, Jogadores: map[int]rastreio.JogadorLT{3: {CS: 9}}},
	}}
	var e8, e7 trajeto
	e8.anda(0, 30000, Ponto{9500, 9500}, Ponto{9500, 9500}, nil)
	e8.anda(60000, 80000, Ponto{6900, 6900}, Ponto{6900, 6900}, nil)
	e7.anda(66000, 80000, Ponto{6500, 6400}, Ponto{6500, 6400}, nil)
	c.Outros[8], c.Outros[7] = e8.as, e7.as
	itens := geraAtividade(c)
	var seq []string
	for _, it := range itens {
		seq = append(seq, it.Dados["atividade"].(string))
	}
	quer := []string{AFarmando, ATrocando, ALutando, AMorto}
	if len(seq) != len(quer) {
		t.Fatalf("%v", seq)
	}
	for i := range quer {
		if seq[i] != quer[i] {
			t.Fatalf("%v", seq)
		}
	}
	tr2 := itens[1]
	if com := tr2.Dados["com"].([]int); len(com) != 1 || com[0] != 8 || tr2.Dados["confianca"] != "alta" || tr2.TMs < 60000 || tr2.TMs > 61000 {
		t.Fatalf("%+v", tr2)
	}
	if itens[0].Dados["confianca"] != "media" {
		t.Fatalf("%+v", itens[0])
	}
}

// A vida do inimigo caindo (quadro lateral) também conta como troca, e o
// objetivo: perto do dragão até 40 s antes de ele cair.
func TestAtividadeObjetivoEVidaDoOutro(t *testing.T) {
	var tr trajeto
	tr.anda(0, 10000, meio, meio, cheia)
	tr.anda(10000, 30000, CovaDragao, CovaDragao, cheia)
	c := ctx(3, 100, "MIDDLE", tr.as)
	var e8 trajeto
	e8.anda(0, 10000, Ponto{7300, 7100}, Ponto{7300, 7100}, nil)
	c.Outros[8] = e8.as
	c.Estados = estadosCom(8, 0, 10000, func(t int64) float64 { return 100 - float64(t)/200 })
	c.Linha = rastreio.LinhaDoTempo{Eventos: []rastreio.EventoLT{{Tipo: "ELITE_MONSTER_KILL", TMs: 29000, Quem: 2, X: f(9866), Y: f(4414)}}}
	itens := geraAtividade(c)
	if len(itens) != 2 || itens[0].Dados["atividade"] != ATrocando || itens[0].Dados["confianca"] != "alta" ||
		itens[1].Dados["atividade"] != AObjetivo {
		t.Fatalf("%+v", itens)
	}
}
