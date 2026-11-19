package rastreio

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"loltracking/server/banco"
	"loltracking/server/campeao"
	"loltracking/server/lcu"
	"loltracking/server/partida"
	"loltracking/server/replay"
	"loltracking/server/web"
)

// A partida baixada do LCU falso (a segunda do logado, estado "watch").
const partidaTeste = 3304789000

// lcuComJogo é o LCU falso cujo watch "abre" o jogo falso.
type lcuComJogo struct {
	*lcu.Falso
	jogo *replay.Falso
}

func (l lcuComJogo) AssisteReplay(ctx context.Context, id int64) error {
	if err := l.Falso.AssisteReplay(ctx, id); err != nil {
		return err
	}
	l.jogo.AbreReplay()
	return nil
}

type amb struct {
	db   *banco.DB
	l    *lcu.Falso
	jogo *replay.Falso
	rel  *replay.RelogioFalso
	fila *Fila
	mux  *http.ServeMux
	cfg  *Config
	hub  *web.Hub
}

func prepara(t *testing.T, ajusta func(j *replay.Falso, c *Config)) *amb {
	t.Helper()
	IntervaloFase = 0
	db, err := banco.AbreMemoria(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := partida.Migra(db); err != nil {
		t.Fatal(err)
	}
	if err := Migra(db); err != nil {
		t.Fatal(err)
	}
	l := lcu.NovoFalso()
	ctx := context.Background()
	bruto, _ := l.Partida(ctx, partidaTeste)
	if err := partida.Grava(ctx, partida.Deps{DB: db, LCU: l, Campeoes: campeao.Fixo(lcu.CampeoesFalsos)}, bruto, true); err != nil {
		t.Fatal(err)
	}
	p, _ := partida.Le(db, partidaTeste)
	rel := replay.NovoRelogioFalso()
	jogo := replay.NovoFalso(rel)
	jogo.DuracaoS = 300
	jogo.Elenco = nil
	for _, pt := range p.Participantes {
		time := "ORDER"
		if pt.Time == 200 {
			time = "CHAOS"
		}
		jogo.Elenco = append(jogo.Elenco, replay.JogadorFalso{Nome: pt.Nome, Campeao: pt.Campeao, Time: time})
	}
	cfg := Config{}
	if ajusta != nil {
		ajusta(jogo, &cfg)
	}
	if err := partida.GravaLinhaDoTempo(db, partidaTeste, jogo.LinhaDoTempo()); err != nil {
		t.Fatal(err)
	}
	cli := lcuComJogo{Falso: l, jogo: jogo}
	hub := web.NovoHub()
	a := &amb{db: db, l: l, jogo: jogo, rel: rel, hub: hub, cfg: &cfg}
	a.fila = NovaFila(Deps{DB: db, LCU: cli, Hub: hub, Config: cfg,
		Replay: replay.Deps{LCU: cli, API: jogo, Jogo: jogo, Teclado: jogo, Tela: jogo, Relogio: rel, Presenca: jogo}})
	a.mux = http.NewServeMux()
	web.RegistraExecucoes(a.mux, hub)
	Registra(a.mux, a.fila)
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

func (a *amb) passes(t *testing.T, id int64) []Passe {
	ps, err := lePasses(a.db, `WHERE p.rastreio_id = ? ORDER BY p.ordem`, id)
	if err != nil {
		t.Fatal(err)
	}
	return ps
}

func (a *amb) conta(t *testing.T, q string, args ...any) int {
	var n int
	if err := a.db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRastreioCompletoUmJogador(t *testing.T) {
	a := prepara(t, func(j *replay.Falso, c *Config) {
		// O jogador 3 morre de 100 a 120 s; a seleção volta sozinha ao renascer.
		j.Mortes = []replay.MorteFalsa{{Indice: 2, De: 100, Ate: 120, Matador: 7}}
	})
	r, _, err := a.fila.Cria(context.Background(), partidaTeste, Pedido{Jogadores: []int{3}})
	if err != nil {
		t.Fatal(err)
	}
	a.fila.Espera()
	r, _ = LeRastreio(a.db, r.ID)
	if r.Estado != Concluido || r.Progresso != 1 {
		t.Fatalf("rastreio %s %v %s", r.Estado, r.Progresso, r.Erro)
	}
	if a.jogo.Aberto() || a.jogo.Saidas != 1 {
		t.Fatalf("o replay não fechou (saídas %d)", a.jogo.Saidas)
	}
	p := a.passes(t, r.ID)[0]
	t.Logf("passe: %d amostras, taxa %.2f, retravas %d, tentativas %d, sem trava %d, calibração %d pontos erro %.1f/%.1f, %d bytes",
		p.Amostras, *p.Taxa, p.Retravas, p.TentativasTrava, p.AmostrasSemTrava, p.CalibracaoPontos,
		*p.ErroCalibracaoMediano, *p.ErroCalibracaoMaximo, p.Bytes)
	if p.Estado != Concluido || !p.Compactado || p.Amostras < 1100 || *p.Taxa < 3.8 || *p.Taxa > 4.2 {
		t.Fatalf("passe %+v", p)
	}
	if p.CalibracaoPontos < 3 || *p.ErroCalibracaoMediano > 25 {
		t.Fatalf("calibração: %d pontos, erro %.1f", p.CalibracaoPontos, *p.ErroCalibracaoMediano)
	}
	// As linhas de trabalho sumiram; o bloco e a cadeia ficaram.
	if n := a.conta(t, `SELECT COUNT(*) FROM amostras_brutas`) + a.conta(t, `SELECT COUNT(*) FROM amostras_time_brutas`); n != 0 {
		t.Fatalf("%d linhas de trabalho sobraram", n)
	}
	if n := a.conta(t, `SELECT COUNT(*) FROM passes_compactos WHERE passe_id = ?`, p.ID); n != 2 {
		t.Fatalf("%d blocos", n)
	}
	// Morte: origem "morte" no local do evento, sem câmera; a seleção voltou sozinha (sem retrava).
	pg, err := ListaAmostras(a.db, partidaTeste, 3, 0, 1e9, 0, 100000)
	if err != nil || pg.Fonte != "compactado" {
		t.Fatal(err, pg.Fonte)
	}
	mortas, base, camera := 0, 0, 0
	mx, my := a.jogo.Posicao(2, 100)
	for _, am := range pg.Amostras {
		switch am.Origem {
		case OrigemMorte:
			mortas++
			if !am.Morto || am.X == nil || abs(*am.X-mx) > 2 || abs(*am.Y-my) > 2 {
				t.Fatalf("amostra morta %+v (esperado %.0f,%.0f)", am, mx, my)
			}
		case OrigemBase:
			base++
		case OrigemCamera:
			camera++
			ex, ey := a.jogo.Posicao(2, float64(am.TMs)/1000)
			if abs(*am.X-ex) > 60 || abs(*am.Y-ey) > 60 {
				t.Fatalf("posição em %d: (%.0f,%.0f) x verdade (%.0f,%.0f)", am.TMs, *am.X, *am.Y, ex, ey)
			}
		}
	}
	if mortas < 70 || camera < 1000 || p.Retravas != 0 {
		t.Fatalf("mortas %d base %d câmera %d retravas %d", mortas, base, camera, p.Retravas)
	}
	// Cadeia: morte, renascimento, pontos do trajeto, nível, minuto.
	rc, err := ListaCadeia(a.db, partidaTeste, 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	tipos := map[string]int{}
	for _, it := range rc.Itens {
		tipos[it.Tipo]++
	}
	t.Logf("cadeia: %v", tipos)
	for _, tp := range []string{"morte", "renascimento", "ponto", "nivel", "minuto", "compra"} {
		if tipos[tp] == 0 {
			t.Fatalf("cadeia sem %s: %v", tp, tipos)
		}
	}
	if tipos["ponto"] > 400 {
		t.Fatalf("trajeto não foi simplificado: %d pontos", tipos["ponto"])
	}
	// Regerar dá a mesma coisa.
	n, err := RegeraCadeia(a.db, p.ID)
	if err != nil || n != len(rc.Itens) {
		t.Fatalf("regera: %d x %d %v", n, len(rc.Itens), err)
	}
	// Eventos: o abate da linha do tempo e o do jogo viraram um só.
	evs, _ := ListaEventos(a.db, partidaTeste, "abate")
	if len(evs) != 1 || !evs[0].ConfirmadoJogo || evs[0].Fonte != FonteLinhaDoTempo {
		t.Fatalf("eventos: %+v", evs)
	}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func TestRetravaQuandoASelecaoNaoVolta(t *testing.T) {
	a := prepara(t, func(j *replay.Falso, c *Config) {
		j.Mortes = []replay.MorteFalsa{{Indice: 9, De: 60, Ate: 80, Matador: 1}}
		j.VoltaAoRenascer = false
		j.DuracaoS = 150
	})
	r, _, err := a.fila.Cria(context.Background(), partidaTeste, Pedido{Jogadores: []int{10}})
	if err != nil {
		t.Fatal(err)
	}
	a.fila.Espera()
	p := a.passes(t, r.ID)[0]
	if p.Estado != Concluido || p.Retravas != 1 || p.AmostrasSemTrava == 0 {
		t.Fatalf("%+v", p)
	}
	// O jogador 10 é a tecla T.
	teclas := strings.Join(a.jogo.Teclas, "")
	if strings.Trim(teclas, "T") != "" || len(teclas) != 4 {
		t.Fatalf("teclas %q", teclas)
	}
	rc, _ := ListaCadeia(a.db, partidaTeste, 10, map[string]bool{"trava_perdida": true, "trava_retomada": true})
	if len(rc.Itens) != 2 {
		t.Fatalf("%+v", rc.Itens)
	}
}

func TestPausaERetoma(t *testing.T) {
	var fila *Fila
	var rid int64
	var uma sync.Once
	a := prepara(t, func(j *replay.Falso, c *Config) {
		c.DepoisDaAmostra = func(passeID int64, am Amostra) {
			if am.TMs > 100000 {
				uma.Do(func() { fila.Pausa(rid) })
			}
		}
	})
	fila = a.fila
	r, _, err := a.fila.Cria(context.Background(), partidaTeste, Pedido{Jogadores: []int{2, 7}})
	if err != nil {
		t.Fatal(err)
	}
	rid = r.ID
	a.fila.Espera()
	r, _ = LeRastreio(a.db, r.ID)
	ps := a.passes(t, r.ID)
	if r.Estado != Pausado || ps[0].Estado != Pausado || ps[1].Estado != Pendente || a.jogo.Aberto() {
		t.Fatalf("pausa: %s %s %s aberto=%v", r.Estado, ps[0].Estado, ps[1].Estado, a.jogo.Aberto())
	}
	if ps[0].TAtualMs < 100000 || ps[0].TAtualMs > 102000 || a.conta(t, `SELECT COUNT(*) FROM amostras_brutas`) == 0 {
		t.Fatalf("t_atual %d", ps[0].TAtualMs)
	}
	// Pausado responde 204 de novo; criar outro enquanto nada roda é permitido, retomar dois não.
	if code, _ := a.pede(t, "POST", "/api/rastreios/"+itoa(r.ID)+"/pausar", ""); code != 204 {
		t.Fatal(code)
	}
	teclasAntes := len(a.jogo.Teclas)
	if _, err := a.fila.Retoma(r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.fila.Retoma(r.ID); err == nil {
		t.Fatal("retomar duas vezes deveria dar 409")
	}
	a.fila.Espera()
	r, _ = LeRastreio(a.db, r.ID)
	ps = a.passes(t, r.ID)
	if r.Estado != Concluido || ps[0].Estado != Concluido || ps[1].Estado != Concluido || a.jogo.Aberto() {
		t.Fatalf("retomada: %s %+v", r.Estado, ps)
	}
	// Retomar fez seek: a trava caiu e foi refeita.
	if len(a.jogo.Teclas) <= teclasAntes || a.jogo.Aberturas != 2 {
		t.Fatalf("teclas %d→%d aberturas %d", teclasAntes, len(a.jogo.Teclas), a.jogo.Aberturas)
	}
	pg, _ := ListaAmostras(a.db, partidaTeste, 2, 0, 1e9, 0, 100000)
	maiorBuraco := int64(0)
	for i := 1; i < len(pg.Amostras); i++ {
		maiorBuraco = max(maiorBuraco, pg.Amostras[i].TMs-pg.Amostras[i-1].TMs)
	}
	if pg.Amostras[0].TMs > 2500 || pg.Amostras[len(pg.Amostras)-1].TMs < 298000 || maiorBuraco > 2000 {
		t.Fatalf("amostras de %d a %d, maior buraco %d", pg.Amostras[0].TMs, pg.Amostras[len(pg.Amostras)-1].TMs, maiorBuraco)
	}
}

func TestPausaAoFimDoPasse(t *testing.T) {
	var fila *Fila
	var rid int64
	var uma sync.Once
	a := prepara(t, func(j *replay.Falso, c *Config) {
		c.DepoisDaAmostra = func(passeID int64, am Amostra) {
			if am.TMs > 100000 {
				uma.Do(func() { fila.PausaAoFimDoPasse(rid) })
			}
		}
	})
	fila = a.fila
	r, exID, err := a.fila.Cria(context.Background(), partidaTeste, Pedido{Jogadores: []int{2, 7}})
	if err != nil {
		t.Fatal(err)
	}
	rid = r.ID
	a.fila.Espera()
	r, _ = LeRastreio(a.db, r.ID)
	ps := a.passes(t, r.ID)
	// O passe em andamento terminou inteiro e foi compactado; o seguinte nem começou.
	if r.Estado != Pausado || ps[0].Estado != Concluido || !ps[0].Compactado || ps[0].TAtualMs < 298000 ||
		ps[1].Estado != Pendente || a.jogo.Aberto() {
		t.Fatalf("pausa ao fim: %s %+v %+v aberto=%v", r.Estado, ps[0], ps[1], a.jogo.Aberto())
	}
	cadeia, temCadeia, err := leCadeia(a.db, ps[0].ID, ps[0].GameID)
	if err != nil {
		t.Fatal(err)
	}
	if a.conta(t, `SELECT COUNT(*) FROM amostras_brutas`) != 0 || !temCadeia || len(cadeia) == 0 {
		t.Fatal("o passe terminado não virou bloco e cadeia")
	}
	ex, _ := a.hub.Busca(exID)
	tipos := map[string]bool{}
	for _, ev := range ex.Eventos() {
		tipos[ev.Tipo] = true
	}
	if !tipos["pausa_pedida"] || !tipos["pausado"] || !tipos["passe_fim"] || tipos["concluido"] {
		t.Fatalf("eventos %v", tipos)
	}
	if code, _ := a.pede(t, "POST", "/api/rastreios/"+itoa(r.ID)+"/pausar?quando=depois", ""); code != 400 {
		t.Fatal(code)
	}
	if code, _ := a.pede(t, "POST", "/api/rastreios/"+itoa(r.ID)+"/pausar?quando=fim_do_passe", ""); code != 204 {
		t.Fatal(code)
	}
	if _, err := a.fila.Retoma(r.ID); err != nil {
		t.Fatal(err)
	}
	a.fila.Espera()
	r, _ = LeRastreio(a.db, r.ID)
	if ps = a.passes(t, r.ID); r.Estado != Concluido || ps[1].Estado != Concluido || a.jogo.Aberto() {
		t.Fatalf("retomada: %s %+v", r.Estado, ps)
	}
}

func TestCancelaFechaOPasseEOReplay(t *testing.T) {
	var fila *Fila
	var rid int64
	var uma sync.Once
	a := prepara(t, func(j *replay.Falso, c *Config) {
		c.DepoisDaAmostra = func(passeID int64, am Amostra) {
			if am.TMs > 50000 {
				uma.Do(func() { fila.Cancela(rid) })
			}
		}
	})
	fila = a.fila
	r, exID, err := a.fila.Cria(context.Background(), partidaTeste, Pedido{})
	if err != nil {
		t.Fatal(err)
	}
	rid = r.ID
	a.fila.Espera()
	r, _ = LeRastreio(a.db, r.ID)
	ps := a.passes(t, r.ID)
	if r.Estado != Cancelado || len(ps) != 10 || ps[0].Estado != Cancelado || !ps[0].Compactado || ps[1].Estado != Cancelado {
		t.Fatalf("%s %+v", r.Estado, ps[0])
	}
	if a.jogo.Aberto() || a.conta(t, `SELECT COUNT(*) FROM amostras_brutas`) != 0 {
		t.Fatal("replay aberto ou linhas de trabalho sobrando")
	}
	ex, _ := a.hub.Busca(exID)
	evs := ex.Eventos()
	if evs[len(evs)-1].Tipo != "fim" || !strings.Contains(string(evs[len(evs)-1].Dados), "cancelada") {
		t.Fatalf("último evento %+v", evs[len(evs)-1])
	}
}

type leitorQueEstoura struct{ n int }

func (l *leitorQueEstoura) Le(*image.RGBA, int, int) (Leitura, error) {
	l.n++
	if l.n > 50 {
		panic("leitor quebrado")
	}
	return Leitura{}, nil
}

func TestPanicoFechaOReplay(t *testing.T) {
	a := prepara(t, func(j *replay.Falso, c *Config) { c.Leitor = &leitorQueEstoura{} })
	r, _, err := a.fila.Cria(context.Background(), partidaTeste, Pedido{Jogadores: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	a.fila.Espera()
	r, _ = LeRastreio(a.db, r.ID)
	if r.Estado != Falhou || !strings.Contains(r.Erro, "leitor quebrado") || a.jogo.Aberto() || a.jogo.Saidas != 1 {
		t.Fatalf("%s %q aberto=%v saídas %d", r.Estado, r.Erro, a.jogo.Aberto(), a.jogo.Saidas)
	}
	// Dá para retomar depois de falhar (e o leitor ainda estoura, mas o replay fecha de novo).
	if _, err := a.fila.Retoma(r.ID); err != nil {
		t.Fatal(err)
	}
	a.fila.Espera()
	if a.jogo.Aberto() || a.jogo.Saidas != 2 {
		t.Fatal("não fechou na segunda vez")
	}
}

func TestSemTeclaComFaseAoVivoOuSemReplay(t *testing.T) {
	a := prepara(t, nil)
	a.l.Fase = "InProgress"
	_, _, err := a.fila.Cria(context.Background(), partidaTeste, Pedido{})
	var est *ErroEstado
	if !errors.As(err, &est) {
		t.Fatal(err)
	}
	if len(a.jogo.Teclas) != 0 || a.jogo.Aberturas != 0 {
		t.Fatal("abriu ou apertou com partida ao vivo")
	}
	// A Replay API nunca responde: o rastreio falha, nenhuma tecla, jogo encerrado.
	a.l.Fase = "None"
	a.jogo.NuncaAbre, a.jogo.NaoSai = true, true
	r, _, err := a.fila.Cria(context.Background(), partidaTeste, Pedido{Jogadores: []int{4}})
	if err != nil {
		t.Fatal(err)
	}
	a.fila.Espera()
	r, _ = LeRastreio(a.db, r.ID)
	if r.Estado != Falhou || len(a.jogo.Teclas) != 0 || a.jogo.Aberto() || a.jogo.Encerrados != 1 {
		t.Fatalf("%s %q teclas %v encerrados %d", r.Estado, r.Erro, a.jogo.Teclas, a.jogo.Encerrados)
	}
}

func TestFaseMudaNoMeioPausa(t *testing.T) {
	var l *lcu.Falso
	var uma sync.Once
	a := prepara(t, func(j *replay.Falso, c *Config) {
		c.DepoisDaAmostra = func(passeID int64, am Amostra) {
			if am.TMs > 60000 {
				uma.Do(func() { l.Fase = "ChampSelect" })
			}
		}
	})
	l = a.l
	r, _, err := a.fila.Cria(context.Background(), partidaTeste, Pedido{Jogadores: []int{5}})
	if err != nil {
		t.Fatal(err)
	}
	a.fila.Espera()
	r, _ = LeRastreio(a.db, r.ID)
	if r.Estado != Pausado || !strings.Contains(r.Erro, "ChampSelect") || a.jogo.Aberto() {
		t.Fatalf("%s %q", r.Estado, r.Erro)
	}
}

func TestRotas(t *testing.T) {
	a := prepara(t, func(j *replay.Falso, c *Config) {
		j.Mortes = []replay.MorteFalsa{{Indice: 0, De: 30, Ate: 45, Matador: 6}}
	})
	code, m := a.pede(t, "POST", "/api/partidas/"+itoa(partidaTeste)+"/rastreio", `{"jogadores":[1],"velocidade":4}`)
	if code != 202 || m["execucao"] == nil {
		t.Fatal(code, m)
	}
	a.fila.Espera()
	if code, m = a.pede(t, "POST", "/api/partidas/"+itoa(partidaTeste)+"/rastreio", `{"jogadores":[11]}`); code != 400 {
		t.Fatal(code, m)
	}
	if code, m = a.pede(t, "POST", "/api/partidas/1/rastreio", `{}`); code != 404 {
		t.Fatal(code, m)
	}
	if code, m = a.pede(t, "GET", "/api/rastreios", ""); code != 200 || len(m["rastreios"].([]any)) != 1 {
		t.Fatal(code, m)
	}
	if code, m = a.pede(t, "GET", "/api/rastreios/1", ""); code != 200 || len(m["passes"].([]any)) != 1 {
		t.Fatal(code, m)
	}
	if code, m = a.pede(t, "GET", "/api/partidas/"+itoa(partidaTeste)+"/amostras?participante=1&de=10&ate=20&passo=1", ""); code != 200 ||
		len(m["amostras"].([]any)) < 10 || len(m["amostras"].([]any)) > 11 {
		t.Fatal(code, len(m["amostras"].([]any)))
	}
	if code, m = a.pede(t, "GET", "/api/partidas/"+itoa(partidaTeste)+"/amostras?participante=2", ""); code != 404 {
		t.Fatal(code, m)
	}
	code, m = a.pede(t, "GET", "/api/partidas/"+itoa(partidaTeste)+"/estados?de=35&ate=37&passo=1", "")
	if code != 200 || len(m["quadros"].([]any)) != 3 {
		t.Fatal(code, m)
	}
	q := m["quadros"].([]any)[0].(map[string]any)["jogadores"].([]any)
	j1, j2 := q[0].(map[string]any), q[1].(map[string]any)
	if j1["morto"] != true || j1["rastreado"] != true || j2["origem"] != "linha_do_tempo" || j2["nivel"].(float64) < 1 ||
		len(j2["itens"].([]any)) == 0 {
		t.Fatalf("%v\n%v", j1, j2)
	}
	if code, m = a.pede(t, "GET", "/api/partidas/"+itoa(partidaTeste)+"/momento?t=200", ""); code != 200 || m["quadro"] == nil {
		t.Fatal(code, m)
	}
	if code, m = a.pede(t, "GET", "/api/partidas/"+itoa(partidaTeste)+"/eventos", ""); code != 200 || len(m["eventos"].([]any)) == 0 {
		t.Fatal(code, m)
	}
	if code, m = a.pede(t, "GET", "/api/partidas/"+itoa(partidaTeste)+"/cadeia?participante=1&tipos=morte", ""); code != 200 ||
		len(m["itens"].([]any)) != 1 {
		t.Fatal(code, m)
	}
	if code, m = a.pede(t, "GET", "/api/partidas/rastreadas", ""); code != 200 || len(m["partidas"].([]any)) != 1 {
		t.Fatal(code, m)
	}
	if code, _ = a.pede(t, "POST", "/api/rastreios/1/retomar", ""); code != 409 {
		t.Fatal("retomar concluído deveria ser 409", code)
	}
	if code, _ = a.pede(t, "POST", "/api/rastreios/99/pausar", ""); code != 404 {
		t.Fatal(code)
	}
}

func TestOcupado409(t *testing.T) {
	bloqueio := make(chan struct{})
	var uma sync.Once
	a := prepara(t, func(j *replay.Falso, c *Config) {
		c.DepoisDaAmostra = func(int64, Amostra) { uma.Do(func() { <-bloqueio }) }
	})
	_, _, err := a.fila.Cria(context.Background(), partidaTeste, Pedido{Jogadores: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	code, m := a.pede(t, "POST", "/api/partidas/"+itoa(partidaTeste)+"/rastreio", `{}`)
	if code != 409 || m["execucao"] == nil {
		t.Fatal(code, m)
	}
	close(bloqueio)
	a.fila.Espera()
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// O trecho (de_ms/ate_ms) pedido pela rota: o passe termina em ate_ms, não no
// fim da partida (o jogo falso dura 300 s).
func TestRotaRastreioComTrecho(t *testing.T) {
	a := prepara(t, nil)
	code, m := a.pede(t, "POST", "/api/partidas/"+itoa(partidaTeste)+"/rastreio",
		`{"jogadores":[2],"velocidade":4,"de_ms":30000,"ate_ms":90000}`)
	if code != 202 {
		t.Fatal(code, m)
	}
	r := m["rastreio"].(map[string]any)
	if r["t_inicio_ms"].(float64) != 30000 || r["t_fim_ms"].(float64) != 90000 {
		t.Fatal(r)
	}
	a.fila.Espera()
	ps := a.passes(t, int64(r["id"].(float64)))
	if len(ps) != 1 || ps[0].Estado != Concluido {
		t.Fatal(ps)
	}
	var ini, fim int64
	if err := a.db.QueryRow(`SELECT t_ini_ms, t_fim_ms FROM passes_compactos WHERE passe_id = ? AND conteudo = ?`,
		ps[0].ID, ConteudoAmostras).Scan(&ini, &fim); err != nil {
		t.Fatal(err)
	}
	if ini < 29000 || ini > 31000 || fim < 89000 || fim > 92000 {
		t.Fatalf("trecho gravado %d–%d, pedido 30000–90000", ini, fim)
	}
	for _, corpo := range []string{`{"de_ms":-1}`, `{"de_ms":60000,"ate_ms":60000}`, `{"ate_ms":-5}`} {
		if code, m := a.pede(t, "POST", "/api/partidas/"+itoa(partidaTeste)+"/rastreio", corpo); code != 400 {
			t.Fatal(corpo, code, m)
		}
	}
}
