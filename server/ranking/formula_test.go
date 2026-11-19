package ranking

import (
	"math"
	"strings"
	"testing"
)

// partidas: n partidas na rota (v vitórias, as primeiras são as mais novas) e
// fora outras na outra rota.
func partidas(rota string, n, v int, outra string, fora int) []PartidaDoJogador {
	var l []PartidaDoJogador
	for i := 0; i < n; i++ {
		l = append(l, PartidaDoJogador{MatchID: rota + string(rune('a'+i)), Posicao: rota, CampeaoID: 64, Venceu: i < v})
	}
	for i := 0; i < fora; i++ {
		l = append(l, PartidaDoJogador{MatchID: outra + string(rune('a'+i)), Posicao: outra, CampeaoID: 99})
	}
	return l
}

func TestWilsonAmostraPequenaNaoPassaNaFrente(t *testing.T) {
	// O exemplo do dono: 5 em 5 não pode ganhar de 60 em 100.
	if Wilson(5, 5) >= Wilson(60, 100) {
		t.Fatalf("5/5 = %.3f, 60/100 = %.3f", Wilson(5, 5), Wilson(60, 100))
	}
	if Wilson(0, 0) != 0 {
		t.Fatal("sem partidas vale 0")
	}
	// 58% em 200 vale mais que 60% em 10.
	if Wilson(116, 200) <= Wilson(6, 10) {
		t.Fatal("volume tem de pesar")
	}
}

func TestPesosSomamUm(t *testing.T) {
	for tipo, modos := range Pesos {
		for modo, ps := range modos {
			soma := 0.0
			vistos := map[string]bool{}
			for _, p := range ps {
				if vistos[p.Componente] {
					t.Fatalf("%s/%s: %s repetido", tipo, modo, p.Componente)
				}
				vistos[p.Componente] = true
				if NomeComponente[p.Componente] == "" || ComoCalcula[p.Componente] == "" {
					t.Fatalf("%s sem rótulo ou regra", p.Componente)
				}
				if p.Peso > 0 {
					soma += p.Peso
				}
			}
			if math.Abs(soma-1) > 1e-9 {
				t.Fatalf("%s/%s: positivos somam %.3f", tipo, modo, soma)
			}
		}
	}
}

func TestCalculaBordas(t *testing.T) {
	selva := Recorte{Tipo: TipoRota, Rota: "JUNGLE"}
	cx := Contexto{}
	js := []Jogador{
		// Subindo: Ouro II 40 PDL, sequência de vitórias e recém-chegado, ganha tudo na selva.
		{Entrada: Entrada{Puuid: "sobe", Divisao: "II", LP: 40, Vitorias: 60, Derrotas: 40, Hot: true, Fresh: true}, Nome: "Sobe",
			Partidas: partidas("JUNGLE", 15, 11, "TOP", 1)},
		// Travado no topo do elo: I 95 PDL, veterano, 50% na selva.
		{Entrada: Entrada{Puuid: "topo", Divisao: "I", LP: 95, Vitorias: 260, Derrotas: 250, Veteran: true}, Nome: "Topo",
			Partidas: partidas("JUNGLE", 20, 10, "", 0)},
		// Poucas partidas na selva (2): fora.
		{Entrada: Entrada{Puuid: "poucas", Divisao: "I", LP: 99, Vitorias: 90, Derrotas: 10}, Partidas: partidas("JUNGLE", 2, 2, "MIDDLE", 10)},
		// Sem a rota: fora.
		{Entrada: Entrada{Puuid: "meio", Divisao: "I", LP: 50, Vitorias: 90, Derrotas: 10}, Partidas: partidas("MIDDLE", 12, 9, "", 0)},
		// Sem partida no patch: fora.
		{Entrada: Entrada{Puuid: "parado", Divisao: "I", LP: 10, Vitorias: 50, Derrotas: 50}},
		// Inativo: fora.
		{Entrada: Entrada{Puuid: "inativo", Divisao: "I", LP: 10, Inativo: true}, Partidas: partidas("JUNGLE", 10, 10, "", 0)},
		// Amostra pequena (4 partidas na selva): entra, marcado.
		{Entrada: Entrada{Puuid: "pequena", Divisao: "IV", LP: 0, Vitorias: 20, Derrotas: 20}, Partidas: partidas("JUNGLE", 4, 4, "", 0)},
	}
	sub, fora := Calcula(ModoSubindo, selva, js, cx, "selva")
	if fora[ForaPoucas] != 1 || fora[ForaSemRecorte] != 1 || fora[ForaSemPatch] != 1 || fora[ForaInativo] != 1 {
		t.Fatalf("fora %v", fora)
	}
	if len(sub) != 3 || sub[0].Puuid != "sobe" {
		t.Fatalf("subindo: %+v", sub)
	}
	mel, _ := Calcula(ModoMelhor, selva, js, cx, "selva")
	if mel[0].Puuid != "topo" {
		t.Fatalf("melhor: o do topo da liga primeiro, veio %s", mel[0].Puuid)
	}
	for _, p := range append(sub, mel...) {
		// A nota é a soma das contribuições (cada uma com 2 casas).
		soma := 0.0
		for _, c := range p.Componentes {
			soma += c.Contribuicao
			if c.Cru == "" || c.Rotulo == "" || c.Valor < 0 || c.Valor > 1 {
				t.Fatalf("componente %+v", c)
			}
		}
		if math.Abs(soma-p.Nota) > 0.05 {
			t.Fatalf("%s: soma %.2f nota %.2f", p.Puuid, soma, p.Nota)
		}
		if p.Frase == "" || !strings.Contains(p.Frase, "na selva no patch") {
			t.Fatal(p.Frase)
		}
	}
	var peq Posicao
	for _, p := range sub {
		if p.Puuid == "pequena" {
			peq = p
		}
	}
	if !peq.AmostraPequena || !strings.Contains(peq.Frase, "amostra pequena") {
		t.Fatalf("%+v", peq)
	}
	// A frase do que sobe diz os sinais.
	if !strings.Contains(sub[0].Frase, "em sequência de vitórias") || !strings.Contains(sub[0].Frase, "recém-chegado") ||
		sub[0].Numeros.PartidasRecorte != 15 || sub[0].Numeros.VitoriasRecentes != 5 {
		t.Fatalf("%+v", sub[0])
	}
}

func TestEmpateDitoEDesempatado(t *testing.T) {
	topo := Recorte{Tipo: TipoRota, Rota: "TOP"}
	e := Entrada{Divisao: "I", LP: 50, Vitorias: 60, Derrotas: 40}
	a, b := e, e
	a.Puuid, b.Puuid = "a", "b"
	js := []Jogador{{Entrada: b, Partidas: partidas("TOP", 10, 6, "", 0)}, {Entrada: a, Partidas: partidas("TOP", 10, 6, "", 0)}}
	l, _ := Calcula(ModoMelhor, topo, js, Contexto{}, "topo")
	if len(l) != 2 || !l[0].Empatado || !l[1].Empatado || l[0].Nota != l[1].Nota {
		t.Fatalf("%+v", l)
	}
	// Tudo igual: desempata pelo puuid (estável), e a frase diz com quem empatou.
	if l[0].Puuid != "a" || !strings.Contains(l[1].Frase, "empatado na nota com o 1º") {
		t.Fatalf("%+v", l)
	}
	// Mais partidas no recorte desempata antes.
	js[0].Partidas = partidas("TOP", 10, 6, "", 0)
	js[1].Entrada.Vitorias, js[1].Entrada.Derrotas = 60, 40
	c := Contexto{}
	n1, _ := Nota(TipoRota, ModoMelhor, numerosDe(js[0], topo), js[0].Entrada, c)
	n2, _ := Nota(TipoRota, ModoMelhor, numerosDe(js[1], topo), js[1].Entrada, c)
	if n1 != n2 {
		t.Fatal("deviam empatar")
	}
}

func TestLigaNoApice(t *testing.T) {
	cx := Contexto{Apex: true, LPMax: 2000}
	if v := ValorLiga(Entrada{LP: 1000}, cx); v != 0.5 {
		t.Fatal(v)
	}
	if v := ValorLiga(Entrada{Divisao: "I", LP: 100}, Contexto{}); v != 1 {
		t.Fatal(v)
	}
	if v := ValorLiga(Entrada{Divisao: "IV", LP: 0}, Contexto{}); v != 0 {
		t.Fatal(v)
	}
}

func TestRankingDeCampeao(t *testing.T) {
	lee := Recorte{Tipo: TipoCampeao, CampeaoID: 64}
	js := []Jogador{
		{Entrada: Entrada{Puuid: "x", Divisao: "I", LP: 10, Vitorias: 50, Derrotas: 50}, Partidas: partidas("JUNGLE", 6, 4, "TOP", 2)},
		{Entrada: Entrada{Puuid: "y", Divisao: "I", LP: 10, Vitorias: 50, Derrotas: 50}, Partidas: partidas("TOP", 0, 0, "MIDDLE", 5)},
	}
	l, fora := Calcula(ModoMelhor, lee, js, Contexto{}, "Lee Sin")
	if len(l) != 1 || fora[ForaSemRecorte] != 1 || l[0].Numeros.Rota != "JUNGLE" || l[0].Numeros.CampeaoID != 64 ||
		!strings.Contains(l[0].Frase, "com Lee Sin") {
		t.Fatalf("%+v %v", l, fora)
	}
}

func TestEscolheCandidatosIntercala(t *testing.T) {
	ents := []Entrada{
		{Puuid: "lp-alto", Divisao: "I", LP: 99, Vitorias: 100, Derrotas: 100, Veteran: true},
		{Puuid: "lp-medio", Divisao: "I", LP: 60, Vitorias: 100, Derrotas: 100},
		{Puuid: "sobe", Divisao: "III", LP: 10, Vitorias: 70, Derrotas: 30, Hot: true},
		{Puuid: "sobe2", Divisao: "IV", LP: 10, Vitorias: 60, Derrotas: 40, Fresh: true},
		{Puuid: "parado", Divisao: "I", LP: 100, Inativo: true},
	}
	cs, inativos := EscolheCandidatos(ents, Contexto{}, 3)
	if inativos != 1 || len(cs) != 3 {
		t.Fatal(inativos, cs)
	}
	// Um de cada lista: subindo, melhor, subindo.
	if cs[0].puuid != "sobe" || cs[0].preOrdem != ModoSubindo || cs[1].puuid != "lp-alto" || cs[1].preOrdem != ModoMelhor ||
		cs[2].puuid != "sobe2" {
		t.Fatalf("%+v", cs)
	}
}

func TestComparaPatch(t *testing.T) {
	if ComparaPatch("16.10", "16.9") <= 0 || ComparaPatch("16.9", "16.10") >= 0 || ComparaPatch("16.2", "16.2") != 0 ||
		ComparaPatch("17.1", "16.24") <= 0 {
		t.Fatal("ordem de patch")
	}
}

func TestOrcamento(t *testing.T) {
	// Platina, fonte riot, 60 candidatos × 20 partidas: 8 à liga (br1) e
	// 60 × 22 = 1320 à match-v5 (americas): (1320 − 100) ÷ 100 × 2 = 24,4 min.
	o := CalculaOrcamento("PLATINUM", FonteRiot, 60, 20)
	if o.PedidosPlataforma != 8 || o.PedidosRegional != 1320 || o.MinutosPiorCaso != 24.4 {
		t.Fatalf("%+v", o)
	}
	// Desafiante inteiro (300): 6600 pedidos, 130 min no pior caso; com o cache estimado, menos.
	d := CalculaOrcamento("CHALLENGER", FonteRiot, 300, 20)
	if d.PedidosPlataforma != 1 || d.PedidosRegional != 6600 || d.MinutosPiorCaso != 130 || d.MinutosEstimado >= d.MinutosPiorCaso {
		t.Fatalf("%+v", d)
	}
	// Fonte lcu: 1 de conta por candidato; 60 cabem na janela, o tempo é do cliente.
	l := CalculaOrcamento("PLATINUM", FonteLCU, 60, 20)
	if l.PedidosRegional != 60 || l.MinutosPiorCaso != 3 {
		t.Fatalf("%+v", l)
	}
	if !strings.Contains(o.Texto, "100 a cada 2 min") {
		t.Fatal(o.Texto)
	}
}
