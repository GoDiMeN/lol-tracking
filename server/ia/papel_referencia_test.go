package ia

import (
	"strings"
	"testing"

	"loltracking/server/analise"
)

// A voz por dado recebe como a referência jogou: "o Kayn de referência carregou: ...".
func TestFrasePapelReferencia(t *testing.T) {
	x := analise.IndicadoresJogador{Campeao: "Kayn", Funcao: "JUNGLE", Papel: analise.PapelJogador{Papel: "carregou", Rotulo: "carregou",
		Confianca: "media", Motivos: []string{"38% do ouro do time", "41% do dano a campeões do time"}, Estilo: "ganks",
		EstiloRotulo: "ganks", MotivosEstilo: []string{"6 abate(s)/assistência(s) nas rotas antes dos 14 min"},
		NaoAvaliado: []string{"luta e gank pela etiqueta: precisam dos outros rastreados"}}}
	f := FrasePapelReferencia(x)
	for _, s := range []string{"o Kayn de referência carregou (confiança média): 38% do ouro do time", "estilo ganks: 6 abate",
		"não avaliado: luta e gank"} {
		if !strings.Contains(f, s) {
			t.Fatalf("%q sem %q", f, s)
		}
	}
	if FrasePapelReferencia(analise.IndicadoresJogador{Campeao: "Kayn"}) != "" {
		t.Fatal("sem papel calculado: frase vazia")
	}
	m, _ := materialReferencia(x, []analise.IndicadoresJogador{x}, nil, &refVoz{descr: "teste"})
	if !strings.Contains(m, "  papel: o Kayn de referência carregou") || !strings.Contains(m, "| papel na partida (por regra) |") {
		t.Fatalf("material:\n%s", m)
	}
}
