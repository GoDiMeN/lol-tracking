package analise

import (
	"testing"

	"loltracking/server/partida"
)

// Uma regra só para o UNDEFINED da Riot: não conta como sentinela em lugar
// nenhum (resumo do jogador e do time, indicadores, wards_ate14), mas continua
// na lista da partida com comum=false. Caso real: o Kayn da 3305481832 saía
// com 0 na base de fatos e 88 na seção "Visão" do pacote da IA.
func TestUndefinedNaoContaEmLugarNenhum(t *testing.T) {
	if ContaComoSentinela("UNDEFINED") || ContaComoSentinela("") || !ContaComoSentinela(TipoTotem) || !ContaComoSentinela(TipoControle) {
		t.Fatal("a regra: só os quatro tipos conhecidos contam")
	}
	d := &dadosPartida{pt: partida.Partida{DuracaoS: 1800, Participantes: []partida.Participante{{ParticipanteID: 7, Time: 200}, {ParticipanteID: 2, Time: 100}}}}
	for i := 0; i < 5; i++ {
		d.eventosSentinela = append(d.eventosSentinela, eventoSentinela{tMs: int64(60000 + i*1000), quem: 7, tipo: "UNDEFINED", colocada: true})
		d.wards = append(d.wards, wardRiot{tMs: int64(60000 + i*1000), quem: 7, tipo: "UNDEFINED"})
	}
	d.eventosSentinela = append(d.eventosSentinela,
		eventoSentinela{tMs: 300000, quem: 7, tipo: TipoTotem, colocada: true},
		eventoSentinela{tMs: 400000, quem: 7, tipo: "UNDEFINED", colocada: false}, // destruiu um UNDEFINED
		eventoSentinela{tMs: 500000, quem: 7, tipo: TipoControle, colocada: false})
	d.wards = append(d.wards, wardRiot{tMs: 300000, quem: 7, tipo: TipoTotem})

	j := d.resumoSentinelas(7, 200)
	if j.Colocadas != 1 || j.PorFase["rotas"] != 1 || j.Destruidas != 1 {
		t.Fatalf("resumo do jogador: colocadas %d, rotas %d, destruídas %d", j.Colocadas, j.PorFase["rotas"], j.Destruidas)
	}
	if n := len(d.wardsAte14(7)); n != 1 {
		t.Fatalf("wards_ate14 contou %d (o UNDEFINED não conta)", n)
	}
	s, _ := d.sentinelas()
	if len(s) != 6 {
		t.Fatalf("a lista da partida continua com as 6 (5 de tipo cru): %d", len(s))
	}
	for _, x := range s {
		if x.Comum != ContaComoSentinela(x.Tipo) {
			t.Fatalf("comum precisa seguir a regra: %+v", x)
		}
	}
}
