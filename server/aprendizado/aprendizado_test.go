package aprendizado

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"loltracking/server/analise"
	"loltracking/server/banco"
	"loltracking/server/campeao"
	"loltracking/server/estudo"
	"loltracking/server/lcu"
	"loltracking/server/partida"
	"loltracking/server/ranking"
	"loltracking/server/rastreio"
	"loltracking/server/replay"
	"loltracking/server/riot"
	"loltracking/server/web"
)

// lcuComJogo é o LCU falso cujo watch "abre" o jogo falso com os 10 da
// partida pedida (um minuto de jogo, para o teste andar rápido) e cuja região
// pode ser trocada.
type lcuComJogo struct {
	*lcu.Falso
	jogo   *replay.Falso
	db     *banco.DB
	regiao *string
	// troca: muda a partida inteira que o LCU devolve (nil = como está).
	troca *func(id int64, bruto json.RawMessage) json.RawMessage
}

func (l lcuComJogo) Partida(ctx context.Context, id int64) (json.RawMessage, error) {
	b, err := l.Falso.Partida(ctx, id)
	if err == nil && *l.troca != nil {
		b = (*l.troca)(id, b)
	}
	return b, err
}

func (l lcuComJogo) AssisteReplay(ctx context.Context, id int64) error {
	if err := l.Falso.AssisteReplay(ctx, id); err != nil {
		return err
	}
	if p, err := partida.Le(l.db, id); err == nil {
		var elenco []replay.JogadorFalso
		for _, pt := range p.Participantes {
			t := "ORDER"
			if pt.Time == 200 {
				t = "CHAOS"
			}
			elenco = append(elenco, replay.JogadorFalso{Nome: pt.Nome, Campeao: pt.Campeao, Time: t})
		}
		l.jogo.ConfiguraPartida(elenco, 60)
	}
	l.jogo.AbreReplay()
	return nil
}

func (l lcuComJogo) Regiao(ctx context.Context) (lcu.Regiao, error) {
	r, err := l.Falso.Regiao(ctx)
	if err == nil && *l.regiao != "" {
		r.Regiao = *l.regiao
	}
	return r, err
}

type amb struct {
	db     *banco.DB
	l      *lcu.Falso
	jogo   *replay.Falso
	riot   *RiotFalso
	k      *Coletor
	fila   *rastreio.Fila
	hub    *web.Hub
	mux    *http.ServeMux
	regiao string
	troca  func(id int64, bruto json.RawMessage) json.RawMessage
}

func prepara(t *testing.T, ajusta func(c *rastreio.Config)) *amb {
	t.Helper()
	rastreio.IntervaloFase = 0
	partida.IntervaloPoll = time.Millisecond
	partida.EsperaCriacao = time.Millisecond
	db, err := banco.AbreMemoria(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, m := range []func(*banco.DB) error{partida.Migra, rastreio.Migra, analise.Migra, estudo.Migra, ranking.Migra, Migra} {
		if err := m(db); err != nil {
			t.Fatal(err)
		}
	}
	a := &amb{db: db, l: lcu.NovoFalso(), riot: NovoRiotFalso(), hub: web.NovoHub()}
	rel := replay.NovoRelogioFalso()
	a.jogo = replay.NovoFalso(rel)
	cli := lcuComJogo{Falso: a.l, jogo: a.jogo, db: db, regiao: &a.regiao, troca: &a.troca}
	cfg := rastreio.Config{}
	if ajusta != nil {
		ajusta(&cfg)
	}
	a.fila = rastreio.NovaFila(rastreio.Deps{DB: db, LCU: cli, Hub: a.hub, Config: cfg,
		Replay: replay.Deps{LCU: cli, API: a.jogo, Jogo: a.jogo, Teclado: a.jogo, Tela: a.jogo, Relogio: rel}})
	a.k = Novo(Deps{DB: db, LCU: cli, Riot: a.riot, Fila: a.fila, Hub: a.hub,
		Partida: partida.Deps{DB: db, LCU: cli, Campeoes: campeao.Fixo(lcu.CampeoesFalsos)}, Analise: analise.Deps{DB: db}})
	a.mux = http.NewServeMux()
	web.RegistraExecucoes(a.mux, a.hub)
	Registra(a.mux, a.k)
	return a
}

func (a *amb) pede(t *testing.T, metodo, caminho, corpo string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	a.mux.ServeHTTP(rec, httptest.NewRequest(metodo, caminho, strings.NewReader(corpo)))
	var m map[string]any
	json.Unmarshal(rec.Body.Bytes(), &m)
	return rec.Code, m
}

func (a *amb) detalhe(t *testing.T, id int64) Detalhe {
	t.Helper()
	d, err := a.k.Detalhe(id)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func eloDe(d Detalhe, elo string) EloDaColeta {
	for _, e := range d.Elos {
		if e.Elo == elo {
			return e
		}
	}
	return EloDaColeta{}
}

func TestColetaCompleta(t *testing.T) {
	a := prepara(t, nil)
	c, m := a.pede(t, "POST", "/api/aprendizado/coletas", `{"elos":["CHALLENGER","GOLD"]}`)
	if c != 202 {
		t.Fatal(c, m)
	}
	col := m["coleta"].(map[string]any)
	id := int64(col["id"].(float64))
	if col["regiao"] != "BR1" || col["regiao_nome"] != "Brasil (BR)" || col["quem"] != "jogador" || col["velocidade"].(float64) != 4 {
		t.Fatal(col)
	}
	a.k.Espera()
	d := a.detalhe(t, id)
	if d.Coleta.Estado != Concluido || d.Coleta.Fase != FaseFim || d.Coleta.Progresso != 1 || d.Coleta.Erro != "" {
		t.Fatalf("%+v", d.Coleta)
	}
	// Ordem dos elos: do menor para o maior, mesmo pedidos fora de ordem.
	if d.Coleta.Elos[0] != "GOLD" || d.Elos[0].Elo != "GOLD" {
		t.Fatal(d.Coleta.Elos)
	}
	// Desafiante: o fantasma e o veterano (não achados no cliente), 3 não mono (n = 3, 7, 11), 3 sem
	// partida rastreável (n = 4, 9, 14: não ocupam vaga, a descoberta segue descendo) e 9 aceitos: a
	// lista acabou antes de 10, e o aviso diz.
	de := eloDe(d, "CHALLENGER")
	if de.Jogadores != 9 || de.CandidatosVistos != 17 || de.Descartes["nao_achado"] != 2 || de.Descartes["nao_mono"] != 3 ||
		de.Descartes[DescSemPartida] != 3 || !strings.Contains(de.Aviso, "achou 9 de 10") || !de.ExigeMono || de.Estado != EloPronto {
		t.Fatalf("desafiante %+v", de)
	}
	// No ápice o inativo sai e o veterano vai para o fim da fila (não chega a ser visto).
	if de.Descartes[DescInativo] != 1 || de.Descartes[DescVeterano] != 0 {
		t.Fatalf("desafiante %+v", de.Descartes)
	}
	// Ouro: inativo, veterano, poucas partidas e travado ficam de fora antes de olhar o resto.
	ouro := eloDe(d, "GOLD")
	if ouro.Jogadores != 10 || ouro.CandidatosVistos != 13 || ouro.Descartes[DescSemPartida] != 2 || ouro.Descartes["nao_mono"] != 0 || ouro.ExigeMono ||
		ouro.Descartes[DescInativo] != 1 || ouro.Descartes[DescVeterano] != 1 || ouro.Descartes[DescPoucasPartidas] != 1 ||
		ouro.Descartes[DescTravado] != 1 {
		t.Fatalf("ouro %+v", ouro)
	}
	// n = 4, 9 e 14 só têm partida de patch antigo: não ocupam vaga, então nenhum jogador da coleta fica sem partida.
	semPartida := 0
	for i, j := range d.Jogadores {
		if j.Estado == JogadorSemPartida {
			semPartida++
		}
		if j.Elo == "CHALLENGER" && (!j.Mono || j.FracaoCampeao < 0.6 || j.PartidasSolo != 20 || j.Campeao == "") {
			t.Fatalf("mono %+v", j)
		}
		// Ordem: pela pontuação de subida (taxa de vitória com Wilson + bônus), não por LP.
		if i > 0 && d.Jogadores[i-1].Elo == j.Elo && (d.Jogadores[i-1].Posicao >= j.Posicao ||
			d.Jogadores[i-1].PontuacaoSubida < j.PontuacaoSubida) {
			t.Fatalf("jogadores fora da ordem de subida: %+v %+v", d.Jogadores[i-1], j)
		}
		if !strings.HasPrefix(j.RiotID, "Ref ") || j.Regiao != "BR1" || j.Vitorias == 0 || j.TaxaVitoria <= 0 ||
			!strings.HasPrefix(j.Motivo, "subindo: ") || j.Veteran {
			t.Fatal(j)
		}
		if (j.Elo == "GOLD") != (j.Divisao == "I") {
			t.Fatalf("divisão %+v", j)
		}
	}
	if semPartida != 0 || d.Coleta.JogadoresSemPartida != 0 || len(d.Jogadores) != 19 {
		t.Fatalf("sem partida %d de %d", semPartida, len(d.Jogadores))
	}
	if len(d.Partidas) != 19 || d.Coleta.PartidasConcluidas != 19 || d.Coleta.PassesConcluidos != 19 || d.Coleta.PassesTotal != 19 {
		t.Fatalf("partidas %d, %+v", len(d.Partidas), d.Coleta)
	}
	for _, p := range d.Partidas {
		if p.Estado != PartidaConcluida || p.RastreioID == nil || p.PassesTotal != 1 || p.Pontuacao <= 0 || p.DuracaoS < DuracaoMinimaS {
			t.Fatalf("%+v", p)
		}
		r, _ := rastreio.LeRastreio(a.db, *p.RastreioID)
		pt, _ := partida.Le(a.db, p.GameID)
		if r.Estado != rastreio.Concluido || r.Velocidade != 4 || pt.Participantes[p.ParticipanteID-1].Venceu != true || pt.Fila != 420 {
			t.Fatalf("rastreio %+v", r)
		}
	}
	if a.jogo.Aberto() {
		t.Fatal("o replay ficou aberto")
	}
	// O download passou pelo LCU (create + download), e só por rotas permitidas.
	baixou := 0
	for _, ch := range a.l.Chamadas {
		if strings.HasPrefix(ch, "download ") {
			baixou++
		}
	}
	if baixou != 19 {
		t.Fatalf("%d downloads", baixou)
	}
	c, m = a.pede(t, "GET", fmt.Sprintf("/api/aprendizado/coletas/%d/espaco", id), "")
	if c != 200 || m["compactado"].(float64) <= 0 || m["cadeia"].(float64) <= 0 || m["passes"].(float64) != 19 ||
		m["total"].(float64) < m["compactado"].(float64)+m["cadeia"].(float64) || m["estimativa_total"].(float64) < m["total"].(float64) {
		t.Fatal(c, m)
	}
	c, m = a.pede(t, "GET", "/api/aprendizado/coletas", "")
	if c != 200 || len(m["coletas"].([]any)) != 1 {
		t.Fatal(c, m)
	}
	// Terminada: pausar e cancelar não cabem; retomar também não.
	if c, _ = a.pede(t, "POST", fmt.Sprintf("/api/aprendizado/coletas/%d/cancelar", id), ""); c != 409 {
		t.Fatal(c)
	}
	if c, _ = a.pede(t, "POST", fmt.Sprintf("/api/aprendizado/coletas/%d/retomar", id), ""); c != 409 {
		t.Fatal(c)
	}
	// O SSE teve as fases e o fim.
	exID, _ := a.k.ExecucaoDe(id)
	ex, _ := a.hub.Busca(exID)
	tipos := map[string]int{}
	for _, ev := range ex.Eventos() {
		tipos[ev.Tipo]++
	}
	if tipos["elo_fim"] != 2 || tipos["candidato"] != 30 || tipos["rastreio"] != 19 || tipos["partida_fim"] != 19 || tipos["concluido"] != 1 || tipos["fim"] != 1 {
		t.Fatal(tipos)
	}
}

func TestPausaGraciosaRetomaECancela(t *testing.T) {
	var a *amb
	var id int64
	var pausa, cancela sync.Once
	var passesVistos sync.Map
	a = prepara(t, func(c *rastreio.Config) {
		c.DepoisDaAmostra = func(passeID int64, am rastreio.Amostra) {
			passesVistos.Store(passeID, true)
			n := 0
			passesVistos.Range(func(any, any) bool { n++; return true })
			if am.TMs > 30000 && n == 1 {
				pausa.Do(func() { a.k.Pausa(id) })
			}
			if am.TMs > 30000 && n == 3 {
				cancela.Do(func() { a.k.Cancela(id) })
			}
		}
	})
	// "todos": cada partida é um rastreio de 10 passes.
	col, _, err := a.k.Cria(context.Background(), Pedido{Elos: []string{"SILVER"}, Quem: QuemTodos, Velocidade: 8})
	if err != nil {
		t.Fatal(err)
	}
	id = col.ID
	a.k.Espera()
	d := a.detalhe(t, id)
	p := d.Partidas[0]
	if d.Coleta.Estado != Pausado || d.Coleta.PausaPedida || p.Estado != PartidaPausada || p.RastreioID == nil {
		t.Fatalf("pausa: %+v %+v", d.Coleta, p)
	}
	// O passe em andamento terminou inteiro (compactado, com cadeia); o seguinte nem começou.
	r, _ := rastreio.LeRastreio(a.db, *p.RastreioID)
	if r.Estado != rastreio.Pausado || r.PassesConcluidos != 1 || r.PassesTotal != 10 || a.jogo.Aberto() {
		t.Fatalf("rastreio pausado %+v aberto=%v", r, a.jogo.Aberto())
	}
	if d.Coleta.PassesConcluidos != 1 || d.Coleta.PassesTotal != len(d.Partidas)*10 || d.Coleta.EstimativaS <= 0 {
		t.Fatalf("%+v", d.Coleta)
	}
	// Retomar continua o mesmo rastreio do passe 2; o cancelamento no passe 3 para tudo.
	c, m := a.pede(t, "POST", fmt.Sprintf("/api/aprendizado/coletas/%d/retomar", id), "")
	if c != 202 {
		t.Fatal(c, m)
	}
	a.k.Espera()
	d = a.detalhe(t, id)
	r, _ = rastreio.LeRastreio(a.db, *d.Partidas[0].RastreioID)
	if d.Coleta.Estado != Cancelado || r.Estado != rastreio.Cancelado || r.PassesConcluidos != 2 || a.jogo.Aberto() {
		t.Fatalf("cancelado: %+v rastreio %+v", d.Coleta, r)
	}
	for _, p := range d.Partidas {
		if p.Estado != PartidaCancelada {
			t.Fatalf("%+v", p)
		}
	}
	var trabalho int
	a.db.QueryRow(`SELECT COUNT(*) FROM amostras_brutas`).Scan(&trabalho)
	if trabalho != 0 {
		t.Fatal("linhas de trabalho sobraram")
	}
}

func TestRecusasAntesDeComecar(t *testing.T) {
	a := prepara(t, nil)
	casos := []struct {
		corpo  string
		status int
		trecho string
	}{
		{`{"regiao":"NA1"}`, 400, "só a região BR"},
		{`{"elos":["BRONZE"]}`, 400, "abaixo de Prata"},
		{`{"elos":["GOLD","GOLD"]}`, 400, "repetido"},
		{`{"partidas_por_jogador":4}`, 400, "1 a 3"},
		{`{"quem":"metade"}`, 400, "quem"},
		{`{"velocidade":20}`, 400, "velocidade"},
	}
	for _, cs := range casos {
		c, m := a.pede(t, "POST", "/api/aprendizado/coletas", cs.corpo)
		if c != cs.status || !strings.Contains(fmt.Sprint(m["erro"]), cs.trecho) {
			t.Fatalf("%s: %d %v", cs.corpo, c, m)
		}
	}
	a.riot.Recusada = true
	if c, m := a.pede(t, "POST", "/api/aprendizado/coletas", `{}`); c != 409 || !strings.Contains(fmt.Sprint(m["erro"]), "recusou a chave") {
		t.Fatal(c, m)
	}
	a.riot.Recusada = false
	a.regiao = "EUW"
	if c, m := a.pede(t, "POST", "/api/aprendizado/coletas", `{}`); c != 409 || !strings.Contains(fmt.Sprint(m["erro"]), "região") {
		t.Fatal(c, m)
	}
	a.regiao = ""
	a.l.Fechado = true
	if c, _ := a.pede(t, "POST", "/api/aprendizado/coletas", `{}`); c != 503 {
		t.Fatal(c)
	}
	a.l.Fechado = false
	if c, m := a.pede(t, "GET", "/api/aprendizado/regioes", ""); c != 200 || len(m["regioes"].([]any)) != 1 {
		t.Fatal(c, m)
	}
	if c, _ := a.pede(t, "GET", "/api/aprendizado/coletas/99", ""); c != 404 {
		t.Fatal(c)
	}
	if c, _ := a.pede(t, "GET", "/api/aprendizado/coletas/99/eventos", ""); c != 404 {
		t.Fatal(c)
	}
	// Chave vencida no meio da descoberta: a coleta para com a frase e é retomável.
	col, _, err := a.k.Cria(context.Background(), Pedido{Elos: []string{"MASTER"}})
	if err != nil {
		t.Fatal(err)
	}
	a.riot.Recusada = true
	a.k.Espera()
	d := a.detalhe(t, col.ID)
	if d.Coleta.Estado != Falhou && d.Coleta.Estado != Concluido {
		t.Fatalf("%+v", d.Coleta)
	}
}

func TestRecuperaNaSubida(t *testing.T) {
	a := prepara(t, nil)
	id, err := criaColeta(a.db, novaColeta{regiao: "BR1", elos: []string{"GOLD"}, ppj: 1, quem: QuemJogador, vel: 4})
	if err != nil {
		t.Fatal(err)
	}
	mudaEstado(a.db, id, Rodando, "")
	a.db.Exec(`INSERT INTO aprendizado_jogadores (coleta_id, regiao, elo, posicao, lp, puuid_riot, nome, tag, puuid_lcu, estado, criado_em)
		VALUES (?, 'BR1', 'GOLD', 1, 90, 'x', 'n', 't', 'y', 'com_partidas', '')`, id)
	a.db.Exec(`INSERT INTO aprendizado_partidas (coleta_id, jogador_id, regiao, elo, game_id, participante_id, estado, atualizado_em)
		VALUES (?, 1, 'BR1', 'GOLD', 1, 1, 'rastreando', ''), (?, 1, 'BR1', 'GOLD', 2, 1, 'baixando', '')`, id, id)
	if err := Recupera(a.db); err != nil {
		t.Fatal(err)
	}
	d := a.detalhe(t, id)
	if d.Coleta.Estado != Pausado || !strings.Contains(d.Coleta.Erro, "servidor") || d.Partidas[0].Estado != PartidaPausada ||
		d.Partidas[1].Estado != PartidaPendente {
		t.Fatalf("%+v %+v", d.Coleta, d.Partidas)
	}
}

func TestCriterioDeSubida(t *testing.T) {
	// Wilson: 60% em 10 vale menos que 58% em 200.
	if Wilson(6, 10) >= Wilson(116, 200) || Wilson(0, 0) != 0 {
		t.Fatal(Wilson(6, 10), Wilson(116, 200))
	}
	es := []riot.EntradaLiga{
		{Puuid: "travado", Vitorias: 140, Derrotas: 160, LP: 99},
		{Puuid: "velho-bom", Vitorias: 170, Derrotas: 130, LP: 10}, // > 250 partidas mas 57%: fica
		{Puuid: "novo", Vitorias: 40, Derrotas: 30, FreshBlood: true, LP: 1},
		{Puuid: "igual-novo", Vitorias: 40, Derrotas: 30, LP: 50},
		{Puuid: "veterano", Vitorias: 90, Derrotas: 10, Veteran: true},
		{Puuid: "inativo", Vitorias: 90, Derrotas: 10, Inactive: true},
		{Puuid: "poucas", Vitorias: 20, Derrotas: 2},
	}
	l, desc := ordenaCandidatos(es, false)
	// 57% em 300 (Wilson ~0,51) vence 57% em 70 com o bônus de recém-chegado (~0,49); este vence o igual sem bônus.
	if len(l) != 3 || l[0].Puuid != "velho-bom" || l[1].Puuid != "novo" || l[2].Puuid != "igual-novo" ||
		desc[DescTravado] != 1 || desc[DescVeterano] != 1 || desc[DescInativo] != 1 || desc[DescPoucasPartidas] != 1 {
		t.Fatal(l, desc)
	}
	// Ápice: o veterano fica, mas no fim; o inativo sai.
	l, desc = ordenaCandidatos(es, true)
	if l[len(l)-1].Puuid != "veterano" || desc[DescInativo] != 1 || len(l) != 6 {
		t.Fatal(l, desc)
	}
	j := JogadorDaColeta{Vitorias: 53, Derrotas: 31, TaxaVitoria: 0.631, FreshBlood: true}
	if m := Motivo(j); m != "subindo: 63% de vitórias em 84 partidas, recém-chegado ao elo" {
		t.Fatal(m)
	}
}

func TestPontuacaoEPrincipal(t *testing.T) {
	if p := Pontuacao(10, 0, 10, 300, 60000, 1800, 1); p != 100 {
		t.Fatal(p)
	}
	if Pontuacao(2, 8, 1, 100, 5000, 1800, 0.1) >= Pontuacao(8, 2, 6, 220, 30000, 1800, 0.6) {
		t.Fatal("a melhor partida pontuou menos")
	}
	h := []itemHist{{Fila: 420, CampeaoID: 1}, {Fila: 420, CampeaoID: 1}, {Fila: 420, CampeaoID: 2}, {Fila: 450, CampeaoID: 2}, {Fila: 450, CampeaoID: 2}}
	if c, f, n := campeaoPrincipal(h); c != 1 || n != 3 || f < 0.66 || f > 0.67 {
		t.Fatal(c, f, n)
	}
}
