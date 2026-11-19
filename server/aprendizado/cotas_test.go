package aprendizado

import (
	"encoding/json"
	"strings"
	"testing"

	"loltracking/server/lcu"
	"loltracking/server/rastreio"
)

// O pedido com cota por rota: lista vazia = as cinco; "funcao" é o atalho.
func TestPedidoCotaPorRota(t *testing.T) {
	casos := []struct {
		corpo  string
		erro   string
		fs     []string
		jpf    int
		jpe    int
		aprove bool
	}{
		{corpo: `{"funcoes":[]}`, fs: FuncoesTodas, jpf: 2, jpe: 10, aprove: true},
		{corpo: `{"funcoes":["utility","TOP"],"jogadores_por_funcao":1,"aproveitar":false}`, fs: []string{"TOP", "UTILITY"}, jpf: 1, jpe: 2},
		{corpo: `{"jogadores_por_funcao":3}`, fs: FuncoesTodas, jpf: 3, jpe: 15, aprove: true},
		{corpo: `{"funcao":"jungle","jogadores_por_elo":4}`, fs: []string{"JUNGLE"}, jpf: 4, jpe: 4, aprove: true},
		{corpo: `{}`, jpe: 10, aprove: true},
		{corpo: `{"funcao":"TOP","funcoes":["JUNGLE"]}`, erro: "não os dois"},
		{corpo: `{"funcoes":["RIO"]}`, erro: "função desconhecida"},
		{corpo: `{"funcoes":["TOP","top"]}`, erro: "repetida"},
		{corpo: `{"funcoes":[],"jogadores_por_funcao":5}`, erro: "jogadores_por_funcao"},
	}
	for _, c := range casos {
		var p Pedido
		if err := json.Unmarshal([]byte(c.corpo), &p); err != nil {
			t.Fatal(err)
		}
		_, err := p.valida()
		if c.erro != "" {
			if err == nil || !strings.Contains(err.Error(), c.erro) {
				t.Fatalf("%s: %v", c.corpo, err)
			}
			continue
		}
		if err != nil || strings.Join(p.Funcoes, ",") != strings.Join(c.fs, ",") || p.JogadoresPorFuncao != c.jpf ||
			p.JogadoresPorElo != c.jpe || *p.Aproveitar != c.aprove {
			t.Fatalf("%s: %v %+v", c.corpo, err, p)
		}
	}
}

// idFalsoTeste: o gameId da i-ésima partida do dono no LCU falso (lcu/falso.go).
func idFalsoTeste(dono, i int) int64 { return 3304790000 - int64(dono)*100000 - int64(i)*1000 }

// Cota de 1 topo e 1 caçador no Ouro, com aproveitamento: o primeiro do Ouro
// (Ref GOLD 1, topo) tem, numa das vitórias, o Ref GOLD 2 (caçador, também
// subindo) no time. A escolha prefere essa partida (termo dos aproveitáveis),
// o Ref GOLD 2 entra como aproveitado (enche a cota da selva sem ser
// procurado) e os dois são rastreados no MESMO rastreio: 1 download, 1
// abertura, 2 passes.
func TestCotaPorRotaComAproveitamento(t *testing.T) {
	antes := PartidasConferidasAproveitamento
	PartidasConferidasAproveitamento = 10
	defer func() { PartidasConferidasAproveitamento = antes }()
	a := prepara(t, nil)
	// Dono 17 = Ref GOLD 1 (n = 0, topo, slot 1 ou 6). Na partida i = 3 (vitória
	// do vermelho, ele no slot 6) o caçador do mesmo time (slot 7) vira o Ref GOLD 2.
	alvo := idFalsoTeste(2+1*lcu.ReferenciasPorElo+0, 3)
	ref2 := lcu.ReferenciaFalsa(1, 1)
	a.troca = func(id int64, b json.RawMessage) json.RawMessage {
		if id != alvo {
			return b
		}
		var m map[string]any
		json.Unmarshal(b, &m)
		for _, x := range m["participantIdentities"].([]any) {
			pi := x.(map[string]any)
			if pi["participantId"].(float64) == 7 {
				pi["player"] = map[string]any{"gameName": ref2.Nome, "tagLine": ref2.Tag, "puuid": ref2.Puuid, "platformId": "BR1"}
			}
		}
		out, _ := json.Marshal(m)
		return out
	}
	c, m := a.pede(t, "POST", "/api/aprendizado/coletas", `{"elos":["GOLD"],"funcoes":["TOP","JUNGLE"],"jogadores_por_funcao":1}`)
	if c != 202 {
		t.Fatal(c, m)
	}
	id := int64(m["coleta"].(map[string]any)["id"].(float64))
	a.k.Espera()
	d := a.detalhe(t, id)
	if d.Coleta.Estado != Concluido || !d.Coleta.Aproveitar || d.Coleta.Aproveitados != 1 || d.Coleta.JogadoresPorElo != 2 ||
		strings.Join(d.Coleta.Funcoes, ",") != "TOP,JUNGLE" || d.Coleta.JogadoresPorFuncao != 1 {
		t.Fatalf("%+v", d.Coleta)
	}
	if len(d.Jogadores) != 2 {
		t.Fatalf("%+v", d.Jogadores)
	}
	topo, selva := d.Jogadores[0], d.Jogadores[1]
	if topo.Funcao == "JUNGLE" {
		topo, selva = selva, topo
	}
	if topo.Funcao != "TOP" || topo.Origem != "" || selva.Funcao != "JUNGLE" || selva.Origem != OrigemAproveitado ||
		selva.RiotID != ref2.Nome+"#"+ref2.Tag || selva.OrigemGameID != alvo || selva.OrigemJogadorID != topo.ID ||
		!strings.Contains(selva.OrigemTexto, topo.RiotID) || selva.Elo != "GOLD" || selva.Vitorias != 68 {
		t.Fatalf("topo %+v\nselva %+v", topo, selva)
	}
	// As duas linhas são da mesma partida e do mesmo rastreio (2 passes, uma abertura).
	if len(d.Partidas) != 2 || d.Partidas[0].GameID != alvo || d.Partidas[1].GameID != alvo || d.Partidas[0].RastreioID == nil ||
		d.Partidas[1].RastreioID == nil || *d.Partidas[0].RastreioID != *d.Partidas[1].RastreioID {
		t.Fatalf("%+v", d.Partidas)
	}
	var donoP, aprovP PartidaDaColeta
	for _, p := range d.Partidas {
		if p.Estado != PartidaConcluida {
			t.Fatalf("%+v", p)
		}
		if p.Aproveitado {
			aprovP = p
		} else {
			donoP = p
		}
	}
	// A pontuação da partida do dono mostra os dois componentes.
	if donoP.Aproveitaveis != 1 || donoP.PontuacaoAproveitaveis != PesoAproveitavel || donoP.PontuacaoJogador <= 0 ||
		donoP.Pontuacao != donoP.PontuacaoJogador+PesoAproveitavel || aprovP.ParticipanteID != 7 || aprovP.Campeao == "" {
		t.Fatalf("dono %+v\naproveitado %+v", donoP, aprovP)
	}
	r, _ := rastreio.LeRastreio(a.db, *donoP.RastreioID)
	if r.Estado != rastreio.Concluido || r.PassesTotal != 2 || r.PassesConcluidos != 2 {
		t.Fatalf("%+v", r)
	}
	if d.Coleta.PassesTotal != 2 || d.Coleta.PassesConcluidos != 2 || d.Coleta.PartidasConcluidas != 2 {
		t.Fatalf("%+v", d.Coleta)
	}
	downloads, aberturas := 0, 0
	for _, ch := range a.l.Chamadas {
		if strings.HasPrefix(ch, "download ") {
			downloads++
		}
		if strings.HasPrefix(ch, "watch ") {
			aberturas++
		}
	}
	if downloads != 1 || aberturas != 1 || a.jogo.Aberto() {
		t.Fatalf("%d downloads, %d aberturas", downloads, aberturas)
	}
	// A matriz elo × rota: topo 1/1, selva 1/1 (aproveitado).
	ouro := eloDe(d, "GOLD")
	if len(ouro.Funcoes) != 2 || ouro.Funcoes[0] != (ContagemFuncao{"TOP", 1, 1, 0}) || ouro.Funcoes[1] != (ContagemFuncao{"JUNGLE", 1, 1, 1}) ||
		ouro.Aproveitados != 1 || ouro.Jogadores != 2 || ouro.Aviso != "" {
		t.Fatalf("%+v", ouro)
	}
	// O elo dos outros foi perguntado uma vez por jogador (cache): o Ref GOLD 2
	// conferido na escolha e de novo na gravação, mas a Riot ouviu uma vez.
	var consultas int
	a.db.QueryRow(`SELECT COUNT(*) FROM aprendizado_elos_vistos`).Scan(&consultas)
	if a.riot.PedidosElo != consultas || consultas == 0 {
		t.Fatalf("%d pedidos de elo, %d no cache", a.riot.PedidosElo, consultas)
	}
}

// Amostragem com cota de 1 atirador e 1 topo (o suporte do falso, n%5 == 4, não tem partida no patch), sem aproveitar: cada um entra na
// rota da função principal dele; os de outra função são descartados.
func TestCotaPorRotaNaAmostragem(t *testing.T) {
	a := prepara(t, nil)
	c, m := a.pede(t, "POST", "/api/aprendizado/coletas",
		`{"elos":["SILVER"],"amostragem":true,"funcoes":["BOTTOM","TOP"],"jogadores_por_funcao":1,"aproveitar":false}`)
	if c != 202 {
		t.Fatal(c, m)
	}
	id := int64(m["coleta"].(map[string]any)["id"].(float64))
	a.k.Espera()
	d := a.detalhe(t, id)
	prata := eloDe(d, "SILVER")
	if d.Coleta.Estado != Concluido || len(d.Jogadores) != 2 || prata.Descartes[DescOutraFuncao] == 0 || d.Coleta.Aproveitados != 0 ||
		a.riot.PedidosElo != 0 {
		t.Fatalf("%+v %+v %+v", d.Coleta, prata, d.Jogadores)
	}
	fs := map[string]bool{}
	for _, j := range d.Jogadores {
		fs[j.Funcao] = true
	}
	if !fs["TOP"] || !fs["BOTTOM"] || prata.Funcoes[0].Funcao != "TOP" || prata.Funcoes[1] != (ContagemFuncao{"BOTTOM", 1, 1, 0}) {
		t.Fatalf("%+v %+v", d.Jogadores, prata.Funcoes)
	}
	// Cada partida é na função em que ele conta.
	for _, p := range d.Partidas {
		for _, j := range d.Jogadores {
			if j.ID == p.JogadorID && (p.ParticipanteID-1)%5 != map[string]int{"TOP": 0, "BOTTOM": 3}[j.Funcao] {
				t.Fatalf("%+v %+v", p, j)
			}
		}
	}
}
