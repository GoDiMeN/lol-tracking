package aprendizado

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"loltracking/server/rastreio"
	"loltracking/server/replay"
)

// Passe ruim que sobra da refação automática não deixa a partida "concluida":
// ela fica "incompleta" com quantos faltam, não conta como pronta, e o
// "refazer passes ruins" (o da tela da partida) a completa.
func TestColetaComPasseRuimFicaIncompleta(t *testing.T) {
	c, f := rastreio.DisjuntorCiclos, rastreio.DisjuntorFracaoSemTrava
	rastreio.DisjuntorCiclos, rastreio.DisjuntorFracaoSemTrava = 1<<30, 2
	t.Cleanup(func() { rastreio.DisjuntorCiclos, rastreio.DisjuntorFracaoSemTrava = c, f })
	a := prepara(t, nil)
	// A câmera não fica em ninguém em quase todo o minuto de jogo, nas duas passagens.
	a.jogo.PerdasTrava = []replay.PerdaTravaFalsa{{Indice: -1, De: 5, Ate: 55}}
	nao := false
	col, _, err := a.k.Cria(context.Background(), Pedido{Elos: []string{"GOLD"}, JogadoresPorElo: 1, Aproveitar: &nao, Velocidade: 4})
	if err != nil {
		t.Fatal(err)
	}
	a.k.Espera()
	d := a.detalhe(t, col.ID)
	if len(d.Partidas) != 1 {
		t.Fatalf("%d partidas", len(d.Partidas))
	}
	p := d.Partidas[0]
	if d.Coleta.Estado != Concluido || p.Estado != PartidaIncompleta || p.PassesRuins != 1 ||
		len(p.ParticipantesRuins) != 1 || p.ParticipantesRuins[0] != p.ParticipanteID || !strings.Contains(p.Erro, "refaça o passe ruim") {
		t.Fatalf("coleta %s, partida %+v", d.Coleta.Estado, p)
	}
	if d.Coleta.PartidasConcluidas != 0 || d.Coleta.PartidasIncompletas != 1 || eloDe(d, "GOLD").PartidasConcluidas != 0 {
		t.Fatalf("contagem %+v", d.Coleta)
	}
	// O evento do fim da partida diz o estado e quantos faltam.
	exID, _ := a.k.ExecucaoDe(col.ID)
	ex, _ := a.hub.Busca(exID)
	achou := false
	for _, ev := range ex.Eventos() {
		if ev.Tipo == "partida_fim" && strings.Contains(string(ev.Dados), `"estado":"incompleta"`) && strings.Contains(string(ev.Dados), `"passes_ruins":1`) {
			achou = true
		}
	}
	if !achou {
		t.Fatal("partida_fim sem o estado incompleta")
	}
	// Pela rota do estudo/coleta também.
	code, m := a.pede(t, "GET", fmt.Sprintf("/api/aprendizado/coletas/%d", col.ID), "")
	if code != 200 || !strings.Contains(fmt.Sprint(m), "incompleta") {
		t.Fatal(code, m)
	}

	// A causa passou: refazer os passes ruins completa a partida.
	a.jogo.PerdasTrava = nil
	if _, _, ar, err := a.fila.RefazRuins(context.Background(), p.GameID, 4); err != nil || len(ar.Participantes) != 1 {
		t.Fatal(err, ar)
	}
	a.fila.Espera()
	d = a.detalhe(t, col.ID)
	p = d.Partidas[0]
	if p.Estado != PartidaConcluida || p.PassesRuins != 0 || p.Erro != "" || d.Coleta.PartidasConcluidas != 1 || d.Coleta.PartidasIncompletas != 0 {
		t.Fatalf("depois de refazer: %+v %+v", p, d.Coleta)
	}
	if eloDe(a.detalhe(t, col.ID), "GOLD").PartidasConcluidas != 1 {
		t.Fatal("o elo não contou a partida completada")
	}
	if a.jogo.Aberto() {
		t.Fatal("o replay ficou aberto")
	}
}
