package analise

import (
	"strings"
	"testing"

	"loltracking/server/rastreio"
)

func TestVisaoAntes(t *testing.T) {
	vt := rastreio.VisaoDoTime{Time: 200, Jogadores: []rastreio.VisibilidadeJogador{
		// 2: fora até 40 s, visível de 40 a 41 s, indeterminado depois (o Yi da 5:52).
		{ParticipanteID: 2, Fora: [][2]int64{{0, 40000}, {41500, 42000}}, Visivel: [][2]int64{{40000, 41500}},
			Indeterminado: [][2]int64{{42000, 50000}}},
		// 3: fora o tempo todo, com 2 s herdados e 1 s fora do mapa.
		{ParticipanteID: 3, Fora: [][2]int64{{0, 49000}}, Inferido: [][2]int64{{44000, 46000}}, ForaDoMapa: [][2]int64{{49000, 50000}}},
		// 4: fora metade, indeterminado metade.
		{ParticipanteID: 4, Fora: [][2]int64{{35000, 42000}}, Indeterminado: [][2]int64{{42000, 50000}}},
		// 5: visível só por herança.
		{ParticipanteID: 5, Fora: [][2]int64{{35000, 45000}}, Visivel: [][2]int64{{45000, 46000}}, Inferido: [][2]int64{{45000, 46000}}},
		// 6: morto (sem nada) quase a janela toda.
		{ParticipanteID: 6, Fora: [][2]int64{{35000, 37000}}},
	}}
	r, ok := visaoAntes(vt, 2, 35000, 50000)
	if !ok || r.Resposta != VistoSim || *r.VistoDesdeMs != 40000 || *r.UltimoVistoMs != 41500 || r.VistoInferido {
		t.Fatalf("visto: %+v", r)
	}
	if r.Cobertura != 0.467 { // (1,5 s visível + 5,5 s fora) ÷ 15 s
		t.Fatalf("cobertura %v", r.Cobertura)
	}
	r, _ = visaoAntes(vt, 3, 35000, 50000)
	if r.Resposta != VistoNao || r.ForaDoMapaMs != 1000 || r.InferidoMs != 2000 || r.Cobertura != 0.857 || r.CoberturaComInferido != 1 {
		t.Fatalf("não visto: %+v", r)
	}
	r, _ = visaoAntes(vt, 4, 35000, 50000)
	if r.Resposta != VistoNaoSabe {
		t.Fatalf("metade indeterminado: %+v", r)
	}
	r, _ = visaoAntes(vt, 5, 35000, 50000)
	if r.Resposta != VistoSim || !r.VistoInferido || r.VisivelMs != 0 {
		t.Fatalf("visto só por herança: %+v", r)
	}
	r, _ = visaoAntes(vt, 6, 35000, 50000)
	if r.Resposta != VistoNaoSabe {
		t.Fatalf("quase sem leitura: %+v", r)
	}
	if _, ok := visaoAntes(vt, 9, 35000, 50000); ok {
		t.Fatal("jogador sem visão respondeu")
	}
	// A frase e a leitura de volta do momento (como vem do banco).
	r, _ = visaoAntes(vt, 2, 35000, 50000)
	c := int64(41000)
	r.ChegadaMs = &c
	a, _ := visaoAntes(vt, 2, 35000, 41000)
	r.AntesDaChegada = &a
	txt := TextoVisaoAntes(r, "Master Yi", 50000)
	if !strings.Contains(txt, "Master Yi visto pelo vermelho desde 0:40 (10 s antes)") || !strings.Contains(txt, "ANTES disso já tinha sido visto") {
		t.Fatalf("frase: %s", txt)
	}
	m := MomentoChave{Tipo: "abate", TMs: 50000, Dados: map[string]any{"visao_atacantes": []any{
		map[string]any{"participante_id": 3.0, "time_observador": 200.0, "de_ms": 35000.0, "ate_ms": 50000.0, "resposta": "nao", "cobertura": 0.857}}}}
	ls := LinhasVisaoDoAbate(m, map[int]string{3: "Ashe"})
	if len(ls) != 1 || !strings.Contains(ls[0], "Ashe NÃO foi visto pelo vermelho") {
		t.Fatalf("linhas: %v", ls)
	}
}
