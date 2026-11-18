package atributos

import (
	"encoding/json"
	"math"
	"testing"

	"loltracking/server/rastreio"
)

func pf(v float64) *float64 { return &v }

// serie monta amostras a cada 250 ms de de a ate (ms) com o atributo dado por f(t).
func serie(de, ate int64, nivel int, f func(t int64) (armadura, velMov float64)) []rastreio.Amostra {
	var as []rastreio.Amostra
	for t := de; t <= ate; t += 250 {
		a, m := f(t)
		as = append(as, rastreio.Amostra{TMs: t, Nivel: nivel, Atr: rastreio.Atributos{Armadura: pf(a), RM: pf(a / 2), VelMov: pf(m)}})
	}
	return as
}

func TestMedeQuedaMontanhaComReducaoNoMeio(t *testing.T) {
	// O caso da Ambessa na 3304217746: 59 → 62 na queda, depois 46 (redução de
	// armadura de inimigo por 7 s) e de volta a 62. O degrau é 59 → 62, não 59 → 46.
	q := QuedaDeDragao{TMs: 511000, Sub: "EARTH_DRAGON", Time: 100}
	as := serie(495000, 540000, 7, func(t int64) (float64, float64) {
		switch {
		case t < 511200:
			return 59, 360
		case t < 513500:
			return 62, 360
		case t < 520000:
			return 46, 360
		default:
			return 62, 360
		}
	})
	r := MedeQueda(q, 1, []int{1, 2}, map[int][]rastreio.Amostra{1: as}, nil)
	var d Degrau
	for _, x := range r.Degraus {
		if x.ParticipanteID == 1 && x.Atributo == "armadura" {
			d = x
		}
	}
	if d.Antes != 59 || d.Depois != 62 || d.Confianca != "alta" {
		t.Fatalf("degrau = %+v", d)
	}
	if math.Abs(d.Percentual-0.0508) > 0.0001 || d.PorAcumuloMin > 0.05 || d.PorAcumuloMax < 0.05 {
		t.Fatalf("percentual = %v [%v, %v]", d.Percentual, d.PorAcumuloMin, d.PorAcumuloMax)
	}
	// Jogador 2 sem amostras: não rastreado, sem número.
	for _, x := range r.Degraus {
		if x.ParticipanteID == 2 && x.Confianca != "nenhuma" {
			t.Fatalf("não rastreado mediu: %+v", x)
		}
	}
}

func TestMedeQuedaCompraNaQuedaNaoMede(t *testing.T) {
	q := QuedaDeDragao{TMs: 10000, Sub: "EARTH_DRAGON", Time: 100}
	as := serie(0, 20000, 3, func(t int64) (float64, float64) {
		if t < 10000 {
			return 40, 340
		}
		return 70, 340
	})
	es := []rastreio.EstadoJogador{{TMs: 9000}, {TMs: 10100}}
	es[1].Slots[0] = rastreio.SlotItem{ID: 1031, Qtd: 1}
	d := medeUm(q, 1, 1, "armadura", as, es)
	if d.Confianca != "nenhuma" || d.Nota == "" {
		t.Fatalf("com compra colada na queda devia recusar: %+v", d)
	}
}

func TestMedeQuedaNuvensPrimeiroTrechoForaDeLuta(t *testing.T) {
	// A Diana na 3304217746: 413 antes; em luta segue 413; fora de luta 429 (o
	// teto suave come parte dos 5%: 413 × 1,05 = 433,65 → 429,9).
	q := QuedaDeDragao{TMs: 834700, Sub: "AIR_DRAGON", Time: 100}
	as := serie(820000, 900000, 11, func(t int64) (float64, float64) {
		switch {
		case t < 837000:
			return 72, 413
		case t < 841000:
			return 72, 429
		case t < 845000:
			return 72, 495 // fantasma ou buff de velocidade: acima da faixa
		default:
			return 72, 429
		}
	})
	d := medeUm(q, 1, 3, "vel_mov", as, nil)
	if d.Antes != 413 || d.Depois != 429 {
		t.Fatalf("degrau = %+v", d)
	}
	if d.PorAcumuloMin > 0.05 || d.PorAcumuloMax < 0.045 {
		t.Fatalf("sem o teto devia dar ~5%%: %+v", d)
	}
}

func TestResumeCruzaAsFaixas(t *testing.T) {
	qs := []Queda{{Sub: "FIRE_DRAGON", Degraus: []Degrau{
		{Atributo: "ad", PorAcumulo: 0.0337, PorAcumuloMin: 0.028, PorAcumuloMax: 0.0394, Confianca: "alta"},
		{Atributo: "ad", PorAcumulo: 0.033, PorAcumuloMin: 0.0293, PorAcumuloMax: 0.0367, Confianca: "alta"},
		{Atributo: "ad", Confianca: "nenhuma"},
		{Atributo: "ap", Confianca: "nenhuma"},
	}}}
	r := Resume(qs)
	if len(r) != 2 {
		t.Fatalf("resumo = %+v", r)
	}
	ad, ap := r[0], r[1]
	if ad.Confianca != "alta" || ad.Medido == nil || math.Abs(*ad.Medido-0.033) > 0.0001 || ad.Fonte != "medido" {
		t.Fatalf("ad = %+v", ad)
	}
	if ap.Medido != nil || ap.Usado != 0.03 || ap.Fonte != "tabela" {
		t.Fatalf("ap sem medição devia cair na tabela: %+v", ap)
	}
}

func TestDragoesDaLinhaTimeDeQuemAjudou(t *testing.T) {
	bruto := func(sub string) json.RawMessage {
		b, _ := json.Marshal(map[string]any{"monsterType": "DRAGON", "monsterSubType": sub})
		return b
	}
	lt := rastreio.LinhaDoTempo{Eventos: []rastreio.EventoLT{
		{Tipo: "ELITE_MONSTER_KILL", TMs: 1, Quem: 0, Assistencias: []int{2, 3}, Bruto: bruto("AIR_DRAGON")},
		{Tipo: "ELITE_MONSTER_KILL", TMs: 2, Quem: 7, Bruto: bruto("FIRE_DRAGON")},
		{Tipo: "ELITE_MONSTER_KILL", TMs: 3, Quem: 7, Bruto: bruto("ELDER_DRAGON")},
	}}
	r := DragoesDaLinha(lt, map[int]int{2: 100, 3: 100, 7: 200})
	if len(r) != 2 || r[0].Time != 100 || r[1].Time != 200 || r[1].Sub != "FIRE_DRAGON" {
		t.Fatalf("dragões = %+v", r)
	}
}

func TestTeto(t *testing.T) {
	for _, v := range []float64{300, 415, 450, 490, 600} {
		if math.Abs(ComTeto(semTeto(v))-v) > 1e-9 {
			t.Fatalf("teto ida e volta em %v", v)
		}
	}
	if ComTeto(433.65) < 429.9 || ComTeto(433.65) > 430 {
		t.Fatalf("teto(433,65) = %v", ComTeto(433.65))
	}
}
