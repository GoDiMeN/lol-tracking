package aprendizado

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"loltracking/server/estudo"
	"loltracking/server/partida"
	"loltracking/server/rastreio"
)

var posicoesFalsas = []string{"TOP", "JUNGLE", "MIDDLE", "BOTTOM", "UTILITY"}

// soDescobre roda a descoberta de Platina e Esmeralda sem rastrear.
func soDescobre(t *testing.T, a *amb) Detalhe {
	t.Helper()
	c, m := a.pede(t, "POST", "/api/aprendizado/coletas", `{"elos":["EMERALD","PLATINUM"],"so_descobrir":true}`)
	if c != 202 || m["coleta"].(map[string]any)["so_descobrir"] != true {
		t.Fatal(c, m)
	}
	a.k.Espera()
	return a.detalhe(t, int64(m["coleta"].(map[string]any)["id"].(float64)))
}

func TestSoDescobrir(t *testing.T) {
	a := prepara(t, nil)
	d := soDescobre(t, a)
	if d.Coleta.Estado != Concluido || d.Coleta.Fase != FaseFim || len(d.Partidas) != 0 || d.Coleta.PassesTotal != 0 ||
		d.Coleta.EstimativaS != 0 || len(d.Jogadores) != 20 {
		t.Fatalf("%+v jogadores=%d partidas=%d", d.Coleta, len(d.Jogadores), len(d.Partidas))
	}
	// Nada foi baixado nem aberto: só leitura.
	for _, ch := range a.l.Chamadas {
		if strings.HasPrefix(ch, "download ") || strings.HasPrefix(ch, "watch ") || strings.HasPrefix(ch, "create ") {
			t.Fatal("só descobrir tocou no replay:", ch)
		}
	}
	if a.jogo.Aberto() {
		t.Fatal("o replay abriu")
	}
	for _, e := range d.Elos {
		// n = 4, 9, 14: sem partida rastreável, contados e fora das vagas.
		if e.Jogadores != 10 || e.Descartes[DescSemPartida] != 2 || e.Estado != EloPronto {
			t.Fatalf("%+v", e)
		}
	}
	for _, j := range d.Jogadores {
		var n int
		fmt.Sscanf(j.Nome[strings.LastIndex(j.Nome, " ")+1:], "%d", &n)
		// O de referência joga sempre na posição (n-1)%5; 10 ranqueadas solo do patch, 4 vitórias e 6 derrotas.
		if j.Funcao != posicoesFalsas[(n-1)%5] || j.FracaoFuncao != 1 || j.VitoriasRastreaveis != 4 || j.DerrotasRastreaveis != 6 ||
			j.Estado != JogadorComPartidas {
			t.Fatalf("%s: %+v", j.Nome, j)
		}
	}
	// A coleta de só descobrir aparece na lista do cartão.
	if c, m := a.pede(t, "GET", "/api/aprendizado/coletas", ""); c != 200 || len(m["coletas"].([]any)) != 1 {
		t.Fatal(c, m)
	}
}

func TestSoDescobrirUmaFuncao(t *testing.T) {
	a := prepara(t, nil)
	if c, m := a.pede(t, "POST", "/api/aprendizado/coletas", `{"elos":["PLATINUM"],"so_descobrir":true,"funcao":"meio"}`); c != 400 {
		t.Fatal(c, m)
	}
	c, m := a.pede(t, "POST", "/api/aprendizado/coletas", `{"elos":["PLATINUM"],"so_descobrir":true,"funcao":"jungle"}`)
	if c != 202 || m["coleta"].(map[string]any)["funcao"] != "JUNGLE" {
		t.Fatal(c, m)
	}
	a.k.Espera()
	d := a.detalhe(t, int64(m["coleta"].(map[string]any)["id"].(float64)))
	// No falso só n = 1, 6, 11 jogam na selva (o 11 não é mono, mas abaixo de Mestre isso não importa).
	e := d.Elos[0]
	if len(d.Jogadores) != 3 || e.Descartes[DescOutraFuncao] == 0 || !strings.Contains(e.Aviso, "achou 3 de 10") {
		t.Fatalf("%+v %d", e, len(d.Jogadores))
	}
	for _, j := range d.Jogadores {
		if j.Funcao != "JUNGLE" {
			t.Fatal(j)
		}
	}
}

// cacador é um jogador de selva da coleta.
func cacador(t *testing.T, d Detalhe, elo string) JogadorDaColeta {
	t.Helper()
	for _, j := range d.Jogadores {
		if j.Elo == elo && j.Funcao == "JUNGLE" {
			return j
		}
	}
	t.Fatal("nenhum caçador em", elo)
	return JogadorDaColeta{}
}

func TestCriaEstudoERastreia(t *testing.T) {
	a := prepara(t, nil)
	d := soDescobre(t, a)
	j := cacador(t, d, "PLATINUM")
	base := fmt.Sprintf("/api/aprendizado/jogadores/%d/estudo", j.ID)
	if c, m := a.pede(t, "POST", base, `{"por_lado":6}`); c != 400 || !strings.Contains(fmt.Sprint(m["erro"]), "3 a 5") {
		t.Fatal(c, m)
	}
	if c, _ := a.pede(t, "POST", "/api/aprendizado/jogadores/999/estudo", `{}`); c != 404 {
		t.Fatal(c)
	}
	c, m := a.pede(t, "POST", base, `{}`)
	if c != 201 || m["vitorias"].(float64) != 3 || m["derrotas"].(float64) != 3 || m["aviso"] != "" || m["funcao"] != "JUNGLE" {
		t.Fatal(c, m)
	}
	est := m["estudo"].(map[string]any)
	eid := int64(est["id"].(float64))
	if est["posicao"] != "JUNGLE" || !strings.Contains(est["nome"].(string), "Platina · "+j.RiotID+" · selva") {
		t.Fatal(est)
	}
	e, _ := estudo.Le(a.db, eid)
	ps, _ := estudo.Partidas(a.db, e)
	for _, p := range ps {
		pt, _ := partida.Le(a.db, p.GameID)
		// Na função do estudo, ranqueada solo do patch instalado, com linha do tempo e analisada.
		if p.Posicao != "JUNGLE" || p.ForaDoFiltro || pt.Fila != 420 || pt.Patch != "16.22" || !pt.TemLinhaDoTempo || !p.Analisada {
			t.Fatalf("%+v %+v", p, pt)
		}
	}
	// Com 5 por lado só há 4 vitórias rastreáveis: cria assim mesmo (4 V, 5 D); a comparação já vale.
	c, m = a.pede(t, "POST", base, `{"por_lado":5}`)
	if c != 201 || m["vitorias"].(float64) != 4 || m["derrotas"].(float64) != 5 || m["faltam_vitorias"].(float64) != 0 {
		t.Fatal(c, m)
	}

	// Rastrear as partidas do estudo: só o jogador, uma por vez, e o replay fecha.
	caminho := fmt.Sprintf("/api/estudos/%d/rastreio", eid)
	if c, m := a.pede(t, "GET", caminho, ""); c != 200 || m["coleta"] != nil || m["pendentes"].(float64) != 6 {
		t.Fatal(c, m)
	}
	if c, m := a.pede(t, "POST", caminho, `{"velocidade":20}`); c != 400 {
		t.Fatal(c, m)
	}
	c, m = a.pede(t, "POST", caminho, `{}`)
	if c != 202 {
		t.Fatal(c, m)
	}
	a.k.Espera()
	r, err := a.k.LeRastreioDoEstudo(eid)
	if err != nil {
		t.Fatal(err)
	}
	if r.Coleta == nil || r.Coleta.Estado != Concluido || r.Coleta.EstudoID != eid || len(r.Partidas) != 6 || r.Pendentes != 0 ||
		r.Coleta.PassesConcluidos != 6 || r.Coleta.Progresso != 1 {
		t.Fatalf("%+v %+v", r.Coleta, r)
	}
	for _, p := range r.Partidas {
		rr, _ := rastreio.LeRastreio(a.db, *p.RastreioID)
		if p.Estado != PartidaConcluida || rr.PassesTotal != 1 || rr.Velocidade != 4 {
			t.Fatalf("%+v %+v", p, rr)
		}
		passe, ok, _ := rastreio.MelhorPasse(a.db, p.GameID, p.ParticipanteID)
		if !ok || passe.Estado != rastreio.Concluido {
			t.Fatal("o jogador do estudo não foi rastreado", p.GameID)
		}
	}
	if a.jogo.Aberto() {
		t.Fatal("o replay ficou aberto")
	}
	// Tudo rastreado: pedir de novo é 409; e o rastreio do estudo não aparece no cartão do aprendizado.
	if c, m := a.pede(t, "POST", caminho, `{}`); c != 409 || !strings.Contains(fmt.Sprint(m["erro"]), "já têm") {
		t.Fatal(c, m)
	}
	if c, m := a.pede(t, "GET", "/api/aprendizado/coletas", ""); c != 200 || len(m["coletas"].([]any)) != 1 {
		t.Fatal(c, m)
	}
	if c, _ := a.pede(t, "GET", "/api/estudos/999/rastreio", ""); c != 404 {
		t.Fatal(c)
	}
}

func TestRastreioDoEstudoPausaERetoma(t *testing.T) {
	var a *amb
	var colID int64
	a = prepara(t, func(c *rastreio.Config) {
		c.DepoisDaAmostra = func(passeID int64, am rastreio.Amostra) {
			if am.TMs > 30000 && passeID == 1 {
				a.k.Pausa(colID)
			}
		}
	})
	d := soDescobre(t, a)
	j := cacador(t, d, "EMERALD")
	res, err := a.k.CriaEstudo(context.Background(), j.ID, PedidoEstudo{})
	if err != nil {
		t.Fatal(err)
	}
	r, _, err := a.k.RastreiaEstudo(context.Background(), res.Estudo.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	colID = r.Coleta.ID
	// Ocupado: nova coleta é recusada com a frase do estudo.
	if _, _, err := a.k.Cria(context.Background(), Pedido{Elos: []string{"GOLD"}}); err == nil || !strings.Contains(err.Error(), "estudo") {
		t.Fatal(err)
	}
	a.k.Espera()
	r, _ = a.k.LeRastreioDoEstudo(res.Estudo.ID)
	if r.Coleta.Estado != Pausado || r.Coleta.PassesConcluidos != 1 || r.Coleta.EstimativaS <= 0 {
		t.Fatalf("%+v", r.Coleta)
	}
	// POST de novo retoma o mesmo.
	r2, _, err := a.k.RastreiaEstudo(context.Background(), res.Estudo.ID, 0)
	if err != nil || r2.Coleta.ID != colID {
		t.Fatal(err, r2.Coleta)
	}
	a.k.Espera()
	r, _ = a.k.LeRastreioDoEstudo(res.Estudo.ID)
	if r.Coleta.Estado != Concluido || r.Coleta.PassesConcluidos != 6 {
		t.Fatalf("%+v", r.Coleta)
	}
}

func TestAvisoFaltamEFuncao(t *testing.T) {
	if s := AvisoFaltam(1, 2); !strings.HasPrefix(s, "faltam 1 vitória e 2 derrotas rastreáveis") {
		t.Fatal(s)
	}
	if AvisoFaltam(0, 0) != "" || !strings.HasPrefix(AvisoFaltam(0, 3), "faltam 3 derrotas") {
		t.Fatal(AvisoFaltam(0, 3))
	}
	h := []itemHist{{Fila: 420, Posicao: "JUNGLE"}, {Fila: 420, Posicao: "JUNGLE"}, {Fila: 420, Posicao: "TOP"}, {Fila: 450, Posicao: ""}}
	if f, fr := funcaoPrincipal(h); f != "JUNGLE" || fr < 0.66 || fr > 0.67 {
		t.Fatal(f, fr)
	}
	if partida.PosicaoDoParticipante(11, 7) != "JUNGLE" || partida.PosicaoDoParticipante(12, 7) != "" {
		t.Fatal("posição pelo participante")
	}
}
