package analise

import (
	"testing"

	"loltracking/server/rastreio"
)

func TestAcampamentoPerto(t *testing.T) {
	// Posições medidas da Vi na partida 3304217746 (15:44 e 15:49).
	if ac, d, ok := acampamentoPerto(Ponto{11746, 7003}); !ok || ac.Nome != "azul (lado vermelho)" || d > 700 {
		t.Fatalf("azul do lado vermelho: %v %.0f %v", ac.Nome, d, ok)
	}
	if ac, _, ok := acampamentoPerto(Ponto{11213, 8406}); !ok || ac.Nome != "lobos (lado vermelho)" {
		t.Fatalf("lobos do lado vermelho: %v %v", ac.Nome, ok)
	}
	// Meio da rota do meio: longe de qualquer acampamento.
	if _, _, ok := acampamentoPerto(Ponto{7400, 7400}); ok {
		t.Fatal("o meio da rota não é acampamento")
	}
}

func TestUsosDeHabilidade(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	as := []rastreio.Amostra{
		{TMs: 1000, CD: rastreio.Recargas{Q: f(0), E: f(0)}},
		{TMs: 1250, CD: rastreio.Recargas{Q: f(7), E: f(0)}},   // usou Q
		{TMs: 1500, CD: rastreio.Recargas{Q: f(6.7)}},          // E não lido: não muda o estado
		{TMs: 1750, CD: rastreio.Recargas{Q: f(6.5), E: f(9)}}, // usou E (o anterior lido era 0)
		{TMs: 9000, CD: rastreio.Recargas{Q: f(0), E: f(0)}},
	}
	usos := usosDeHabilidade(as)
	if len(usos) != 2 || usos[0] != 1250 || usos[1] != 1750 {
		t.Fatalf("usos: %v", usos)
	}
	if !usouHabilidadePerto(usos, 3000, 3000) || usouHabilidadePerto(usos, 6000, 3000) || !usouHabilidadePerto(usos, 0, 3000) {
		t.Fatal("janela do uso de habilidade")
	}
}
