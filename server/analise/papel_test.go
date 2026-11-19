package analise

import (
	"strings"
	"testing"
)

// As regras do papel com números escolhidos: cada caso cai no papel esperado
// e diz os números que o justificam.
func TestClassificaPapel(t *testing.T) {
	casos := []struct {
		nome  string
		j     ResumoJogador
		n     NumerosPapel
		papel string
		frase string
	}{
		{"carregou", ResumoJogador{Abates: 12, Mortes: 3, Venceu: true},
			NumerosPapel{FatiaOuro: 30, FatiaDano: 35, FatiaAbates: 40, ParticipacaoAbates: 70, DifOuro15: ip(800), DifOuroFim: ip(3000)},
			"carregou", "30% do ouro do time"},
		{"afundou", ResumoJogador{Abates: 1, Mortes: 11, Assistencias: 3},
			NumerosPapel{FatiaOuro: 14, FatiaDano: 12, FatiaMortes: 30, MortesCaras: 7, DifOuro15: ip(-1000), DifOuroFim: ip(-4000)},
			"afundou", "30% das mortes do time"},
		// Morre muito mas produz (KDA alto, à frente no fim): não afundou.
		{"morre mas produz", ResumoJogador{Abates: 10, Mortes: 13, Assistencias: 9, Venceu: true},
			NumerosPapel{FatiaOuro: 23, FatiaDano: 23, FatiaMortes: 32, MortesCaras: 8, ParticipacaoAbates: 54, DifOuro15: ip(100), DifOuroFim: ip(3900)},
			"estavel", "54%"},
		{"habilitou", ResumoJogador{Abates: 2, Mortes: 4, Assistencias: 25},
			NumerosPapel{FatiaOuro: 15, FatiaDano: 9, ParticipacaoAbates: 75, FatiaVisao: 40, ObjetivosParticip: 9, ObjetivosTime: 12},
			"habilitou", "40% da visão do time"},
		{"foi carregado", ResumoJogador{Abates: 2, Mortes: 6, Assistencias: 3, Venceu: true},
			NumerosPapel{FatiaOuro: 18, FatiaDano: 11, ParticipacaoAbates: 20},
			"foi_carregado", "venceu"},
	}
	for _, c := range casos {
		pp := PapelJogador{Numeros: c.n}
		(&fatos{}).classificaPapel(&c.j, &pp)
		if pp.Papel != c.papel || !strings.Contains(strings.Join(pp.Motivos, " | "), c.frase) || pp.Confianca == "" || pp.Rotulo == "" {
			t.Fatalf("%s: %+v", c.nome, pp)
		}
	}
}

// O estilo depende da função: os mesmos números de posição viram coisas
// diferentes no topo e no suporte; sem critério que passe, estilo vazio e o
// motivo do mais perto.
func TestClassificaEstiloPorFuncao(t *testing.T) {
	n := NumerosPapel{PctSozinhoNoLado: 50, MinutosDepois: 20, PctComAtirador: 80, FatiaVisao: 20, DifOuro15: ip(0), DifOuro10: ip(0)}
	topo := PapelJogador{Numeros: n}
	(&fatos{}).classificaEstilo(&ResumoJogador{Funcao: "TOP"}, &topo, 1)
	if topo.Estilo != "ilha_split" || topo.ConfiancaEstilo != "media" || len(topo.MotivosEstilo) == 0 {
		t.Fatalf("topo: %+v", topo)
	}
	sup := PapelJogador{Numeros: n}
	(&fatos{}).classificaEstilo(&ResumoJogador{Funcao: "UTILITY"}, &sup, 1)
	if sup.Estilo != "com_atirador" {
		t.Fatalf("suporte: %+v", sup)
	}
	cac := PapelJogador{Numeros: NumerosPapel{AbatesEmRota: 1, PctSelvaInimiga: 5}}
	(&fatos{}).classificaEstilo(&ResumoJogador{Funcao: "JUNGLE"}, &cac, 1)
	if cac.Estilo != "" || cac.ConfiancaEstilo != "baixa" || !strings.Contains(cac.MotivosEstilo[0], "nenhum critério") {
		t.Fatalf("caçador sem estilo: %+v", cac)
	}
	// Caçador com 6 abates nas rotas antes dos 14: ganks.
	cac = PapelJogador{Numeros: NumerosPapel{AbatesEmRota: 6}}
	(&fatos{}).classificaEstilo(&ResumoJogador{Funcao: "JUNGLE"}, &cac, 1)
	if cac.Estilo != "ganks" || cac.ConfiancaEstilo != "alta" {
		t.Fatalf("ganks: %+v", cac)
	}
}

// O tanque do topo que vence não afunda só por morrer na frente: o Gragas 1/7/10
// da vitória 3305486035 (47% do controle do time). Sem a linha de frente (pouco
// controle, pouco dano recebido, mortes em lutas perdidas) continua afundando.
func TestLinhaDeFrenteNaoAfunda(t *testing.T) {
	gragas := ResumoJogador{Abates: 1, Mortes: 7, Assistencias: 10, Venceu: true}
	n := NumerosPapel{FatiaOuro: 15, FatiaDano: 14, FatiaMortes: 17, MortesCaras: 7, ParticipacaoAbates: 31, FatiaControle: 47,
		FatiaDanoRecebido: 23, MortesEmLutaVencida: 4, DifOuro15: ip(-410), DifOuroFim: ip(-1677)}
	pp := PapelJogador{Numeros: n}
	(&fatos{}).classificaPapel(&gragas, &pp)
	if pp.Papel != "estavel" || pp.Confianca != "media" || !strings.Contains(strings.Join(pp.Motivos, " | "), "linha de frente") {
		t.Fatalf("tanque da vitória: %+v", pp)
	}
	// As mesmas mortes sozinho ou em lutas perdidas (o Gragas real: 0 de 7):
	// continua não afundando, com confiança baixa.
	n.MortesEmLutaVencida = 0
	pp = PapelJogador{Numeros: n}
	(&fatos{}).classificaPapel(&gragas, &pp)
	if pp.Papel != "estavel" || pp.Confianca != "baixa" {
		t.Fatalf("mortes em lutas perdidas: %+v", pp)
	}
	// Quem mais morreu no time (≥ 25% das mortes) afunda mesmo na frente.
	muito := n
	muito.FatiaMortes = 30
	pp = PapelJogador{Numeros: muito}
	(&fatos{}).classificaPapel(&gragas, &pp)
	if pp.Papel != "afundou" {
		t.Fatalf("o que mais morreu: %+v", pp)
	}
	// Muito controle sem apanhar (lentidão de longe, como a Ashe do meio): não é linha de frente.
	longe := n
	longe.FatiaControle, longe.FatiaDanoRecebido = 49, 12
	pp = PapelJogador{Numeros: longe}
	(&fatos{}).classificaPapel(&gragas, &pp)
	if pp.Papel != "afundou" {
		t.Fatalf("controle de longe: %+v", pp)
	}
	// Sem frente (pouco controle e pouco dano recebido): afundou.
	sem := n
	sem.FatiaControle, sem.FatiaDanoRecebido = 10, 15
	pp = PapelJogador{Numeros: sem}
	(&fatos{}).classificaPapel(&gragas, &pp)
	if pp.Papel != "afundou" {
		t.Fatalf("sem linha de frente: %+v", pp)
	}
	// Na derrota a linha de frente não salva.
	n.MortesEmLutaVencida = 4
	derrota := gragas
	derrota.Venceu = false
	pp = PapelJogador{Numeros: n}
	(&fatos{}).classificaPapel(&derrota, &pp)
	if pp.Papel != "afundou" {
		t.Fatalf("derrota: %+v", pp)
	}
}

// A luta em que ele morre: o time matou pelo menos tantos quantos perdeu na
// janela em volta da morte.
func TestMortesEmLutaVencida(t *testing.T) {
	f := &fatos{timeDe: map[int]int{1: 100, 2: 100, 6: 200, 7: 200}, retratos: []Retrato{
		{TMs: 100000, Vitima: 1}, {TMs: 105000, Vitima: 6}, {TMs: 108000, Vitima: 7}, // morreu, o time matou 2: vencida
		{TMs: 500000, Vitima: 1}, {TMs: 503000, Vitima: 2}, // morreu com o aliado e ninguém do outro lado: perdida
		{TMs: 900000, Vitima: 1}, {TMs: 930000, Vitima: 6}, // a troca veio fora da janela: perdida
	}}
	if n := f.mortesEmLutaVencida(1, 100); n != 1 {
		t.Fatalf("mortes em luta vencida: %d", n)
	}
}

// Objetivos do caçador: ter dado o Golpear em quase todos os épicos não basta.
func TestObjetivosDoCacadorExigeMaisQueOsEpicos(t *testing.T) {
	// O Kayn da 3305481832: 5 de 5 épicos, 9 de 16 objetivos (56%), 4% do tempo em objetivo.
	kayn := ResumoJogador{Funcao: "JUNGLE", Rastreado: true, Etiquetas: map[string]map[string]int{"total": {AObjetivo: 95, AFarmando: 2188}}}
	n := NumerosPapel{EpicosParticipou: 5, EpicosTime: 5, ObjetivosParticip: 9, ObjetivosTime: 16, AbatesEmRota: 3}
	pp := PapelJogador{Numeros: n}
	(&fatos{}).classificaEstilo(&kayn, &pp, 1)
	if pp.Estilo == "objetivos" {
		t.Fatalf("só os épicos não fazem o estilo objetivos: %+v", pp)
	}
	// Com tempo relevante em objetivo (≥ 8% do tempo dele): objetivos.
	kayn.Etiquetas["total"][AObjetivo] = 250
	pp = PapelJogador{Numeros: n}
	(&fatos{}).classificaEstilo(&kayn, &pp, 1)
	if pp.Estilo != "objetivos" {
		t.Fatalf("tempo em objetivo: %+v", pp)
	}
	// Não rastreado, com participação acima do esperado nos objetivos do time: objetivos.
	n.ObjetivosParticip, n.AbatesEmRota = 12, 0
	pp = PapelJogador{Numeros: n}
	(&fatos{}).classificaEstilo(&ResumoJogador{Funcao: "JUNGLE"}, &pp, 1)
	if pp.Estilo != "objetivos" {
		t.Fatalf("participação acima do esperado: %+v", pp)
	}
}
