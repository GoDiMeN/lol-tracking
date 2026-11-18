package rastreio

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"loltracking/server/replay"
	"loltracking/server/web"
)

// tiposDe conta os eventos da execução por tipo.
func tiposDe(t *testing.T, hub *web.Hub, exID string) (map[string]int, []web.Evento) {
	t.Helper()
	ex, ok := hub.Busca(exID)
	if !ok {
		t.Fatalf("execução %s sumiu", exID)
	}
	n := map[string]int{}
	evs := ex.Eventos()
	for _, e := range evs {
		n[e.Tipo]++
	}
	return n, evs
}

// teclasPermitidas: só as dez teclas de seleção de jogador (CLAUDE.md, regra 3).
func teclasPermitidas(t *testing.T, teclas []string) {
	t.Helper()
	for _, k := range teclas {
		if !strings.Contains("12345QWERT", k) || len(k) != 1 {
			t.Fatalf("tecla fora da lista: %q", k)
		}
	}
}

// O jogador 3 (índice 2) perde a trava entre 150 e 165 s na primeira passagem.
// Durante a perda o motor continua gravando (amostras sem posição, marcadas);
// ao fim do passe a repescagem volta ao trecho, trava e regrava; o passe fecha
// sem buraco e o resumo diz 1 buraco, 1 recuperado, 0 sobrando.
func TestRepescagemNoFimDoPasse(t *testing.T) {
	var naPerda, comTrava int64
	var maiorVazio, ultimoT int64
	a := prepara(t, func(j *replay.Falso, c *Config) {
		j.PerdasTrava = []replay.PerdaTravaFalsa{{Indice: 2, De: 150, Ate: 165, Vezes: 1}}
		c.DepoisDaAmostra = func(passeID int64, am Amostra) {
			// Só a primeira passagem (a repescagem volta o tempo).
			if am.TMs < atomic.LoadInt64(&ultimoT) {
				return
			}
			if am.TMs >= 150500 && am.TMs < 164500 {
				if am.TravaPerdida {
					atomic.AddInt64(&naPerda, 1)
				} else {
					atomic.AddInt64(&comTrava, 1)
				}
			}
			if u := atomic.LoadInt64(&ultimoT); u > 0 && am.TMs-u > maiorVazio {
				maiorVazio = am.TMs - u
			}
			atomic.StoreInt64(&ultimoT, am.TMs)
		}
	})
	r, exID, err := a.fila.Cria(context.Background(), partidaTeste, Pedido{Jogadores: []int{3}, Velocidade: 4})
	if err != nil {
		t.Fatal(err)
	}
	a.fila.Espera()
	p := a.passes(t, r.ID)[0]
	if p.Estado != Concluido || p.Repescagem == nil {
		t.Fatalf("passe %+v", p)
	}
	t.Logf("primeira passagem: %d amostras sem trava e %d com trava em 150–165 s, maior vazio %d ms; resumo %+v",
		naPerda, comTrava, maiorVazio, *p.Repescagem)
	// Item 3: durante a perda o motor não parou de gravar (a 4 por segundo de
	// jogo são ~56 amostras em 14 s), e nenhum vazio passou de 1,5 s de jogo
	// (a tecla dupla bloqueia ~0,3 s real = ~1,2 s de jogo a 4×). Algumas
	// leituras saem boas mesmo na perda: a tecla seleciona sem prender, e
	// enquanto o campeão ainda está na tela a fórmula da posição vale.
	if naPerda < 25 || naPerda+comTrava < 40 || maiorVazio > 1500 {
		t.Fatalf("durante a perda: %d amostras sem trava, maior vazio %d ms", naPerda, maiorVazio)
	}
	rs := p.Repescagem
	if rs.Quando != "fim_do_passe" || rs.Buracos != 1 || rs.Recuperados != 1 || rs.Sobraram != 0 {
		t.Fatalf("resumo %+v", *rs)
	}
	// O dado final não tem buraco: o trecho é de câmera, com a posição certa.
	pg, _ := ListaAmostras(a.db, partidaTeste, 3, 0, 1e9, 0, 100000)
	if bs := Buracos(pg.Amostras, LimiarRepescagemMs); len(bs) != 0 {
		t.Fatalf("sobraram buracos: %+v", bs)
	}
	camera := 0
	for i, am := range pg.Amostras {
		if i > 0 && am.TMs <= pg.Amostras[i-1].TMs {
			t.Fatalf("amostras fora de ordem/repetidas em %d", am.TMs)
		}
		if am.TMs < 151000 || am.TMs > 164000 {
			continue
		}
		if am.Origem == OrigemCamera {
			camera++
			ex, ey := a.jogo.Posicao(2, float64(am.TMs)/1000)
			if abs(*am.X-ex) > 60 || abs(*am.Y-ey) > 60 {
				t.Fatalf("posição em %d: (%.0f,%.0f) x verdade (%.0f,%.0f)", am.TMs, *am.X, *am.Y, ex, ey)
			}
		}
	}
	if camera < 45 {
		t.Fatalf("só %d amostras de câmera no trecho repescado", camera)
	}
	// O estado dos 10 também foi regravado no trecho (sem duplicar).
	if n := a.conta(t, `SELECT COUNT(*) FROM amostras_brutas`); n != 0 {
		t.Fatalf("%d linhas de trabalho sobraram", n)
	}
	n, evs := tiposDe(t, a.hub, exID)
	if n["repescagem_inicio"] != 1 || n["repescagem_trecho"] != 1 || n["repescagem_fim"] != 1 {
		t.Fatalf("eventos %v", n)
	}
	for _, e := range evs {
		if e.Tipo == "passe_fim" {
			var d struct {
				Repescagem ResumoRepescagem `json:"repescagem"`
			}
			json.Unmarshal(e.Dados, &d)
			if d.Repescagem.Buracos != 1 || d.Repescagem.Recuperados != 1 {
				t.Fatalf("passe_fim sem o resumo: %s", e.Dados)
			}
		}
	}
	teclasPermitidas(t, a.jogo.Teclas)
	if a.jogo.Aberto() || a.jogo.Saidas != 1 {
		t.Fatalf("o replay não fechou (saídas %d)", a.jogo.Saidas)
	}
}

// O jogo esconde o campeão com a seleção nele (teleporte, Arauto do Vale;
// descobertas 8) em toda passagem: a repescagem tenta UMA vez, vê que a perda
// se repete com a seleção nele e marca o trecho como nao_selecionavel, que
// não volta ao plano de repescagem nem conta como buraco na qualidade.
func TestRepescagemNaoSelecionavel(t *testing.T) {
	a := prepara(t, func(j *replay.Falso, c *Config) {
		j.PerdasTrava = []replay.PerdaTravaFalsa{{Indice: 2, De: 150, Ate: 158, Escondido: true}}
	})
	r, exID, err := a.fila.Cria(context.Background(), partidaTeste, Pedido{Jogadores: []int{3}, Velocidade: 4})
	if err != nil {
		t.Fatal(err)
	}
	a.fila.Espera()
	p := a.passes(t, r.ID)[0]
	if p.Estado != Concluido || p.Repescagem == nil || p.Repescagem.Buracos != 1 || p.Repescagem.Recuperados != 0 ||
		len(p.Repescagem.Restantes) != 1 || p.Repescagem.Restantes[0].Tipo != BuracoNaoSelecionavel ||
		!strings.Contains(p.Repescagem.Restantes[0].Motivo, "não é defeito") {
		t.Fatalf("passe %s resumo %+v", p.Estado, p.Repescagem)
	}
	if n, _ := tiposDe(t, a.hub, exID); n["repescagem_trecho"] != 1 {
		t.Fatalf("tentativas: %v", n)
	}
	// As amostras do trecho estão marcadas e com a seleção no jogador.
	pg, _ := ListaAmostras(a.db, partidaTeste, 3, 150500, 152000, 0, 1000)
	sel := 0
	for _, am := range pg.Amostras {
		if am.TravaPerdida && am.SelecaoOK {
			sel++
		}
	}
	if sel < 3 {
		t.Fatalf("só %d amostras perdidas com a seleção nele", sel)
	}
	planos, err := PlanoDeRepescagem(a.db, partidaTeste, nil)
	if err != nil || len(planos) != 0 {
		t.Fatalf("plano: %+v %v", planos, err)
	}
	teclasPermitidas(t, a.jogo.Teclas)
	if a.jogo.Aberto() {
		t.Fatal("o replay ficou aberto")
	}
}

// Classificação pelo dado: teleporte (salto maior que o que se anda, seleção
// nele no começo), repetido numa repescagem anterior (motivo antigo ou tipo)
// e o buraco comum (a seleção caiu).
func TestClassificaBuracos(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	am := func(tms int64, x, y float64, perdida, sel bool) Amostra {
		a := Amostra{TMs: tms, X: f(x), Y: f(y), Origem: OrigemCamera, SelecaoOK: sel}
		if perdida {
			a.Origem, a.TravaPerdida = OrigemInterpolada, true
		}
		return a
	}
	// 0–1 s parado na fonte azul, 1,25–5 s sem a câmera, 5,25 s no meio do mapa.
	var as []Amostra
	for t := int64(0); t <= 1000; t += 250 {
		as = append(as, am(t, 400, 600, false, true))
	}
	for t := int64(1250); t <= 5000; t += 250 {
		as = append(as, am(t, 400, 600, true, true))
	}
	as = append(as, am(5250, 7400, 7400, false, true))
	bs := Buracos(as, LimiarRepescagemMs)
	ClassificaBuracos(as, bs, 100, nil)
	if len(bs) != 1 || bs[0].Tipo != BuracoNaoSelecionavel {
		t.Fatalf("teleporte: %+v", bs)
	}
	// A seleção caiu logo no começo: buraco comum, mesmo com o salto.
	semSel := append([]Amostra(nil), as...)
	semSel[6].SelecaoOK = false
	bs = Buracos(semSel, LimiarRepescagemMs)
	ClassificaBuracos(semSel, bs, 100, nil)
	if bs[0].Tipo != "" {
		t.Fatalf("seleção caiu: %+v", bs)
	}
	// Chegando à fonte (volta à base): não é teleporte.
	volta := append([]Amostra(nil), as...)
	for i := range volta {
		if !volta[i].TravaPerdida {
			x, y := 7400.0, 7400.0
			if volta[i].TMs == 5250 {
				x, y = 400, 600
			}
			volta[i].X, volta[i].Y = &x, &y
		}
	}
	bs = Buracos(volta, LimiarRepescagemMs)
	ClassificaBuracos(volta, bs, 100, nil)
	if bs[0].Tipo != "" {
		t.Fatalf("volta à base: %+v", bs)
	}
	// Sem salto (andou), mas a repescagem anterior repetiu: pelo motivo antigo e pelo tipo.
	anda := append([]Amostra(nil), as...)
	x, y := 1200.0, 1400.0
	anda[len(anda)-1].X, anda[len(anda)-1].Y = &x, &y
	bs = Buracos(anda, LimiarRepescagemMs)
	ClassificaBuracos(anda, bs, 100, nil)
	if bs[0].Tipo != "" {
		t.Fatalf("andando, sem repescagem: %+v", bs)
	}
	for _, r := range []Buraco{{DeMs: 1000, AteMs: 5250, Motivo: "o trecho regravado ainda ficou 4.2 s sem a câmera no jogador"},
		{DeMs: 900, AteMs: 5000, Tipo: BuracoNaoSelecionavel}} {
		bs = Buracos(anda, LimiarRepescagemMs)
		ClassificaBuracos(anda, bs, 100, &ResumoRepescagem{Restantes: []Buraco{r}})
		if bs[0].Tipo != BuracoNaoSelecionavel {
			t.Fatalf("repetido (%+v): %+v", r, bs)
		}
	}
}

// A perda acontece em toda passagem: a repescagem tenta duas vezes (uma a
// mais), não fecha, e o buraco fica registrado com o motivo. O passe conclui.
func TestRepescagemQueNaoFecha(t *testing.T) {
	a := prepara(t, func(j *replay.Falso, c *Config) {
		j.PerdasTrava = []replay.PerdaTravaFalsa{{Indice: 2, De: 150, Ate: 165}}
	})
	r, exID, err := a.fila.Cria(context.Background(), partidaTeste, Pedido{Jogadores: []int{3}, Velocidade: 4})
	if err != nil {
		t.Fatal(err)
	}
	a.fila.Espera()
	p := a.passes(t, r.ID)[0]
	if p.Estado != Concluido || p.Repescagem == nil || p.Repescagem.Buracos != 1 || p.Repescagem.Recuperados != 0 ||
		p.Repescagem.Sobraram != 1 || !strings.Contains(p.Repescagem.Restantes[0].Motivo, "sem a câmera") {
		t.Fatalf("passe %s resumo %+v", p.Estado, p.Repescagem)
	}
	n, _ := tiposDe(t, a.hub, exID)
	if n["repescagem_trecho"] != 1+TentativasExtrasRepescagem {
		t.Fatalf("tentativas: %v", n)
	}
	// O trecho continua marcado (trava perdida), não vazio.
	pg, _ := ListaAmostras(a.db, partidaTeste, 3, 150000, 165000, 0, 1000)
	perdidas := 0
	for _, am := range pg.Amostras {
		if am.TravaPerdida {
			perdidas++
		}
	}
	if perdidas < 40 {
		t.Fatalf("só %d amostras marcadas no buraco", perdidas)
	}
	if a.jogo.Aberto() {
		t.Fatal("o replay ficou aberto")
	}
}

// Passe já gravado: a perda vale para as 3 primeiras passagens da abertura
// (passe + 2 tentativas da repescagem no fim), então o rastreio termina com o
// buraco. A rota de repescagem abre o replay de novo, recupera, regrava os
// arquivos do passe pelo fechamento normal, pede a análise e fecha o replay.
func TestRepescagemDePasseGravado(t *testing.T) {
	a := prepara(t, func(j *replay.Falso, c *Config) {
		j.PerdasTrava = []replay.PerdaTravaFalsa{{Indice: 2, De: 150, Ate: 165, Vezes: 3}}
	})
	var analises int32
	a.fila.d.AoFecharPasse = func(int64) { atomic.AddInt32(&analises, 1) }
	r, _, err := a.fila.Cria(context.Background(), partidaTeste, Pedido{Jogadores: []int{3, 5}, Velocidade: 4})
	if err != nil {
		t.Fatal(err)
	}
	a.fila.Espera()
	ps := a.passes(t, r.ID)
	if ps[0].Repescagem == nil || ps[0].Repescagem.Sobraram != 1 {
		t.Fatalf("o rastreio deveria terminar com 1 buraco: %+v", ps[0].Repescagem)
	}
	antes := ps[0]
	cadeiaAntes, _ := ListaCadeia(a.db, partidaTeste, 3, nil)
	saidasAntes, abertasAntes := a.jogo.Saidas, a.jogo.Aberturas

	// O plano (GET) aponta só o passe do jogador 3.
	code, m := a.pede(t, "GET", "/api/partidas/"+itoa(partidaTeste)+"/rastreio/repescagem", "")
	if code != 200 || m["total"].(float64) != 1 || len(m["passes"].([]any)) != 1 || m["execucao"] != nil {
		t.Fatalf("plano: %d %v", code, m)
	}
	// Partida ao vivo: recusa sem abrir nada (regra 2).
	a.l.Fase = "InProgress"
	if code, m := a.pede(t, "POST", "/api/partidas/"+itoa(partidaTeste)+"/rastreio/repescagem", ""); code != 409 {
		t.Fatalf("com partida ao vivo: %d %v", code, m)
	}
	a.l.Fase = ""
	if a.jogo.Aberturas != abertasAntes {
		t.Fatal("abriu o replay com partida ao vivo")
	}
	code, m = a.pede(t, "POST", "/api/partidas/"+itoa(partidaTeste)+"/rastreio/repescagem", `{"participantes": [3]}`)
	if code != 202 {
		t.Fatalf("repescagem: %d %v", code, m)
	}
	exID := m["execucao"].(map[string]any)["id"].(string)
	// Com ela rodando, a fila está ocupada.
	if _, _, err := a.fila.Cria(context.Background(), partidaTeste, Pedido{Jogadores: []int{4}}); err == nil {
		t.Log("(a repescagem já tinha terminado quando o rastreio foi pedido)")
	} else if !strings.Contains(err.Error(), "repescagem") {
		t.Fatalf("ocupado: %v", err)
	}
	a.fila.Espera()
	n, _ := tiposDe(t, a.hub, exID)
	if n["concluido"] != 1 || n["passe_regravado"] != 1 || n["erro"] != 0 || n["fechando_replay"] != 1 {
		t.Fatalf("eventos: %v", n)
	}
	depois, _ := LePasse(a.db, antes.ID)
	if depois.Repescagem == nil || depois.Repescagem.Quando != "depois" || depois.Repescagem.Recuperados != 1 || depois.Repescagem.Sobraram != 0 {
		t.Fatalf("resumo: %+v", depois.Repescagem)
	}
	if depois.Estado != Concluido || !depois.Compactado || depois.CalibracaoPontos != antes.CalibracaoPontos {
		t.Fatalf("passe regravado: %+v", depois)
	}
	pg, _ := ListaAmostras(a.db, partidaTeste, 3, 0, 1e9, 0, 100000)
	if bs := Buracos(pg.Amostras, LimiarRepescagemMs); len(bs) != 0 {
		t.Fatalf("sobraram buracos: %+v", bs)
	}
	for _, am := range pg.Amostras {
		if am.TMs > 152000 && am.TMs < 163000 && am.Origem == OrigemCamera {
			ex, ey := a.jogo.Posicao(2, float64(am.TMs)/1000)
			if abs(*am.X-ex) > 60 || abs(*am.Y-ey) > 60 {
				t.Fatalf("posição em %d: (%.0f,%.0f) x verdade (%.0f,%.0f)", am.TMs, *am.X, *am.Y, ex, ey)
			}
		}
	}
	cadeiaDepois, err := ListaCadeia(a.db, partidaTeste, 3, nil)
	if err != nil || len(cadeiaDepois.Itens) == 0 || cadeiaDepois.PasseID != cadeiaAntes.PasseID {
		t.Fatalf("cadeia: %v %d", err, len(cadeiaDepois.Itens))
	}
	if x := a.conta(t, `SELECT COUNT(*) FROM amostras_brutas`) + a.conta(t, `SELECT COUNT(*) FROM amostras_time_brutas`); x != 0 {
		t.Fatalf("%d linhas de trabalho sobraram", x)
	}
	if atomic.LoadInt32(&analises) < 3 {
		t.Fatalf("a análise não foi pedida depois da repescagem (%d)", analises)
	}
	if a.jogo.Aberto() || a.jogo.Saidas != saidasAntes+1 || a.jogo.Aberturas != abertasAntes+1 {
		t.Fatalf("replay: aberto=%v saídas %d→%d", a.jogo.Aberto(), saidasAntes, a.jogo.Saidas)
	}
	teclasPermitidas(t, a.jogo.Teclas)
	// Nada mais a recuperar: 409.
	if code, m := a.pede(t, "POST", "/api/partidas/"+itoa(partidaTeste)+"/rastreio/repescagem", ""); code != 409 {
		t.Fatalf("sem buraco: %d %v", code, m)
	}
	code, m = a.pede(t, "GET", "/api/partidas/"+itoa(partidaTeste)+"/rastreio/repescagem", "")
	if code != 200 || m["total"].(float64) != 0 || m["execucao"] == nil || m["rodando"] != false {
		t.Fatalf("plano depois: %v", m)
	}
}

// Cancelar a repescagem no meio fecha o replay e não deixa linha de trabalho.
func TestRepescagemCancelada(t *testing.T) {
	a := prepara(t, func(j *replay.Falso, c *Config) {
		j.PerdasTrava = []replay.PerdaTravaFalsa{{Indice: 2, De: 150, Ate: 165, Vezes: 3}}
	})
	r, _, err := a.fila.Cria(context.Background(), partidaTeste, Pedido{Jogadores: []int{3}, Velocidade: 4})
	if err != nil {
		t.Fatal(err)
	}
	a.fila.Espera()
	antes := a.passes(t, r.ID)[0]
	var exID atomic.Value
	var uma int32
	a.fila.d.Config.DepoisDaAmostra = func(passeID int64, am Amostra) {
		id, _ := exID.Load().(string)
		if id != "" && atomic.CompareAndSwapInt32(&uma, 0, 1) {
			if ex, ok := a.hub.Busca(id); ok {
				ex.Cancela()
			}
		}
	}
	// A config do motor é copiada na corrida: o gancho vale para a próxima.
	id, _, err := a.fila.Repesca(context.Background(), partidaTeste, PedidoRepescagem{})
	if err != nil {
		t.Fatal(err)
	}
	exID.Store(id)
	a.fila.Espera()
	if a.jogo.Aberto() {
		t.Fatal("o replay ficou aberto depois de cancelar")
	}
	if x := a.conta(t, `SELECT COUNT(*) FROM amostras_brutas`); x != 0 {
		t.Fatalf("%d linhas de trabalho sobraram", x)
	}
	depois, _ := LePasse(a.db, antes.ID)
	if depois.Amostras != antes.Amostras || depois.Estado != Concluido {
		t.Fatalf("o passe mudou: %d→%d %s", antes.Amostras, depois.Amostras, depois.Estado)
	}
}
