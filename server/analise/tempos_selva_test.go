package analise

import "testing"

// A tabela escolhe a linha pelo patch da partida; os números do 26.x são os
// das notas do patch 26.1 (tempos_selva.go).
func TestTemposDaSelvaPorPatch(t *testing.T) {
	n, ok := TemposDaSelva("16.22")
	if !ok || n.Patches != "26.1 em diante" {
		t.Fatalf("16.22: %q %v", n.Patches, ok)
	}
	esperado := map[string][2]int64{"azul": {55000, 300000}, "vermelho": {55000, 300000}, "lobos": {55000, 135000},
		"aves": {55000, 135000}, "sapo": {67000, 135000}, "krugues": {67000, 135000}, "caranguejo": {175000, 150000}}
	for tipo, e := range esperado {
		a := n.Acampamentos[tipo]
		if a.PrimeiroMs != e[0] || a.VoltaMs != e[1] || a.Ouro <= 0 {
			t.Fatalf("%s no 26.x: %+v", tipo, a)
		}
	}
	// Todo acampamento do mapa tem tempo em toda linha.
	for _, l := range TabelaTemposSelva {
		for _, a := range Acampamentos {
			if _, ok := l.Acampamentos[tipoAcampamento(a.Nome)]; !ok {
				t.Fatalf("%s sem tempo para %s", l.Patches, a.Nome)
			}
		}
	}
	if b := n.Epicos["barao"]; b.PrimeiroMs != 1200000 || b.VoltaMs != 360000 {
		t.Fatalf("barão no 26.x: %+v", b)
	}
	if _, tem := n.Epicos["atakhan"]; tem {
		t.Fatal("o Atakhan saiu do jogo no 26.1")
	}
	if n.primeiroAcampamentoMs() != 55000 {
		t.Fatalf("piso das janelas no 26.x: %d", n.primeiroAcampamentoMs())
	}
	if v, ok := TemposDaSelva("16.1.123.4"); !ok || v.Patches != n.Patches {
		t.Fatalf("16.1 (versão longa): %q %v", v.Patches, ok)
	}

	v, ok := TemposDaSelva("15.20")
	if !ok || v.Acampamentos["azul"].PrimeiroMs != 90000 || v.Acampamentos["krugues"].PrimeiroMs != 102000 ||
		v.Epicos["barao"].PrimeiroMs != 1500000 || v.Epicos["atakhan"].PrimeiroMs != 1200000 {
		t.Fatalf("15.20: %+v", v)
	}
	if v.primeiroAcampamentoMs() != 90000 {
		t.Fatalf("piso das janelas no 25.x: %d", v.primeiroAcampamentoMs())
	}
	if v, _ := TemposDaSelva("15.3"); v.Epicos["vastilarvas"].PrimeiroMs != 360000 || v.Epicos["vastilarvas"].VoltaMs != 240000 {
		t.Fatalf("15.3: vastilarvas %+v", v.Epicos["vastilarvas"])
	}
	// Sem patch: a mais nova; mais velho que a tabela: a mais velha. Os dois
	// avisam que não estão cobertos.
	if v, ok := TemposDaSelva(""); ok || v.Patches != TabelaTemposSelva[0].Patches {
		t.Fatalf("vazio: %q %v", v.Patches, ok)
	}
	if v, ok := TemposDaSelva("14.20"); ok || v.Patches != TabelaTemposSelva[len(TabelaTemposSelva)-1].Patches {
		t.Fatalf("14.20: %q %v", v.Patches, ok)
	}
}

func TestTipoEpico(t *testing.T) {
	casos := map[string]string{"DRAGON:FIRE_DRAGON": "dragao", "DRAGON:ELDER_DRAGON": "anciao", "BARON_NASHOR": "barao",
		"RIFTHERALD": "arauto", "HORDE": "vastilarvas", "ATAKHAN": "atakhan", "OUTER_TURRET": ""}
	for sub, e := range casos {
		if g := tipoEpico("objetivo", sub); g != e {
			t.Fatalf("%s: %q, esperado %q", sub, g, e)
		}
	}
	if tipoEpico("torre", "DRAGON:FIRE_DRAGON") != "" {
		t.Fatal("só objetivo é épico")
	}
}
