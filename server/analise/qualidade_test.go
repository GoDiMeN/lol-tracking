package analise

import (
	"testing"

	"loltracking/server/rastreio"
)

func TestConfereQualidade(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	am := func(ms int64, x, y float64, nivel, cs int) rastreio.Amostra {
		return rastreio.Amostra{TMs: ms, X: f(x), Y: f(y), Origem: "camera", Nivel: nivel, CS: cs}
	}
	as := []rastreio.Amostra{
		am(0, 7000, 7000, 5, 40),
		am(250, 7010, 7000, 5, 41),
		// Salto de 3 000 unidades em 0,25 s, longe da base: glitch.
		am(500, 10000, 7000, 5, 41),
		// Volta à base (recall): salto legítimo.
		am(750, 600, 700, 5, 41),
		am(1000, 7000, 7000, 5, 41),
		// Buraco de 5 s; nível e CS para trás.
		am(6000, 7000, 7000, 4, 30),
		am(6250, 20000, 7000, 4, 30),
	}
	// Morte e renascimento: não contam.
	morto := am(6500, 7000, 7000, 4, 30)
	morto.Morto = true
	as = append(as, morto)
	// Parado no mesmo ponto exato por 21 s, vivo, longe da base.
	for ms := int64(7000); ms <= 28000; ms += 1000 {
		as = append(as, am(ms, 8000, 8000, 6, 50))
	}
	taxa := 2.0
	p := rastreio.Passe{ID: 1, ParticipanteID: 3, Taxa: &taxa, Amostras: len(as), DuracaoMs: 60000, Retravas: 5,
		ErroCalibracaoMediano: f(400), ConferenciaMaiorDif: f(10)}
	q := ConfereQualidade(p, 100, as)
	esperado := map[string]int{QSalto: 2, QBuraco: 1, QNivelVoltou: 1, QCSVoltou: 1, QForaDoMapa: 1, QParado: 1, QTaxaBaixa: 1,
		QRetravas: 1, QCalibracao: 1}
	for k, v := range esperado {
		if q.Contagem[k] != v {
			t.Errorf("%s: %d, esperado %d (%+v)", k, q.Contagem[k], v, q.Pontos)
		}
	}
	if q.Contagem[QConferencia] != 0 || q.Total != 10 || len(q.Pontos) != 10 {
		t.Fatalf("%+v", q)
	}
	// Passe limpo: nada.
	limpo := ConfereQualidade(rastreio.Passe{ID: 2, Taxa: f(4), DuracaoMs: 60000}, 200, []rastreio.Amostra{am(0, 7000, 7000, 1, 0), am(250, 7050, 7000, 1, 0)})
	if limpo.Total != 0 {
		t.Fatalf("%+v", limpo)
	}
}
