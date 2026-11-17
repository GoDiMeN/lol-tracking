package partida

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"loltracking/server/banco"
	"loltracking/server/campeao"
	"loltracking/server/lcu"
	"loltracking/server/web"
)

type ambiente struct {
	mux *http.ServeMux
	srv *httptest.Server
	f   *lcu.Falso
	db  *banco.DB
}

func prepara(t *testing.T) *ambiente {
	t.Helper()
	IntervaloPoll, EsperaCriacao = time.Millisecond, time.Millisecond
	db, err := banco.AbreMemoria(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := Migra(db); err != nil {
		t.Fatal(err)
	}
	f := lcu.NovoFalso()
	mux := http.NewServeMux()
	hub := web.NovoHub()
	web.RegistraExecucoes(mux, hub)
	Registra(mux, Deps{DB: db, LCU: f, Campeoes: campeao.Fixo(lcu.CampeoesFalsos)}, hub)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &ambiente{mux: mux, srv: srv, f: f, db: db}
}

func (a *ambiente) pede(t *testing.T, metodo, caminho string) (int, []byte) {
	t.Helper()
	rec := httptest.NewRecorder()
	a.mux.ServeHTTP(rec, httptest.NewRequest(metodo, caminho, nil))
	return rec.Code, rec.Body.Bytes()
}

func TestListaPaginaEEstados(t *testing.T) {
	a := prepara(t)
	status, corpo := a.pede(t, "GET", "/api/jogadores/"+lcu.FalsoLogado.Puuid+"/partidas?quantidade=15")
	if status != 200 {
		t.Fatalf("%d %s", status, corpo)
	}
	var pag Pagina
	json.Unmarshal(corpo, &pag)
	if len(pag.Partidas) != 15 || !pag.TemMais || pag.Fonte != "cliente" {
		t.Fatalf("página: %d partidas, tem_mais %v, fonte %s", len(pag.Partidas), pag.TemMais, pag.Fonte)
	}
	conta := map[string]int{}
	for _, p := range pag.Partidas {
		conta[p.Replay.Estado]++
	}
	// 12 no patch atual (1 já baixada), 2 no 16.21 e 1 no 16.10 nesta página.
	if conta[Baixado] != 1 || conta[Baixavel] != 11 || conta[Incompativel] != 3 {
		t.Fatalf("estados: %v", conta)
	}
	p0 := pag.Partidas[0]
	if p0.Campeao != "Akshan" || p0.FilaNome != "ranqueada solo" || !p0.Completa || p0.Patch != "16.22" {
		t.Fatalf("primeira: %+v", p0)
	}
	// A baixada já ganhou linha do tempo na listagem.
	if !pag.Partidas[1].TemLinhaDoTempo {
		t.Fatalf("a partida baixada deveria ter linha do tempo")
	}
	// Só as do patch atual foram apresentadas ao plugin (create); as velhas, não.
	creates := 0
	for _, c := range a.f.Chamadas {
		if strings.HasPrefix(c, "create ") {
			creates++
		}
	}
	if creates != 11 {
		t.Fatalf("creates = %d, esperava 11 (%v)", creates, a.f.Chamadas)
	}

	// Segunda página, e o fim.
	_, corpo = a.pede(t, "GET", "/api/jogadores/"+lcu.FalsoLogado.Puuid+"/partidas?inicio=20&quantidade=10")
	json.Unmarshal(corpo, &pag)
	if len(pag.Partidas) != 5 || pag.TemMais {
		t.Fatalf("última página: %d partidas, tem_mais %v", len(pag.Partidas), pag.TemMais)
	}
}

func TestPartidaInteiraComDezParticipantes(t *testing.T) {
	a := prepara(t)
	id := int64(3304790000)
	status, corpo := a.pede(t, "GET", fmt.Sprintf("/api/partidas/%d", id))
	if status != 200 {
		t.Fatalf("%d %s", status, corpo)
	}
	var r struct{ Partida Partida }
	json.Unmarshal(corpo, &r)
	p := r.Partida
	if len(p.Participantes) != 10 || p.TimeVencedor != 200 {
		t.Fatalf("participantes %d, vencedor %d", len(p.Participantes), p.TimeVencedor)
	}
	pos := []string{}
	for _, pt := range p.Participantes {
		pos = append(pos, pt.Posicao)
		if pt.Venceu != (pt.Time == 200) {
			t.Fatalf("venceu errado em %+v", pt)
		}
	}
	if strings.Join(pos, ",") != "TOP,JUNGLE,MIDDLE,BOTTOM,UTILITY,TOP,JUNGLE,MIDDLE,BOTTOM,UTILITY" {
		t.Fatalf("posições: %v", pos)
	}
	dono := p.Participantes[0]
	if dono.Puuid != lcu.FalsoLogado.Puuid || dono.Campeao != "Akshan" || len(dono.Itens) != 7 {
		t.Fatalf("dono: %+v", dono)
	}
	if s, _ := a.pede(t, "GET", "/api/partidas/123"); s != 404 {
		t.Fatalf("inexistente: %d", s)
	}
	if s, _ := a.pede(t, "GET", "/api/partidas/abc"); s != 400 {
		t.Fatalf("id ruim: %d", s)
	}
}

// eventos lê o SSE da execução até o fim e devolve "tipo:dados" de cada um.
func eventos(t *testing.T, base, exID string) []string {
	t.Helper()
	r, err := http.Get(base + "/api/execucoes/" + exID + "/eventos")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	var lista []string
	tipo := ""
	sc := bufio.NewScanner(r.Body)
	for sc.Scan() {
		l := sc.Text()
		if s, ok := strings.CutPrefix(l, "event: "); ok {
			tipo = s
		}
		if s, ok := strings.CutPrefix(l, "data: "); ok {
			lista = append(lista, tipo+":"+s)
		}
	}
	return lista
}

func baixa(t *testing.T, a *ambiente, id int64) (int, string) {
	t.Helper()
	r, err := http.Post(fmt.Sprintf("%s/api/partidas/%d/replay/baixar", a.srv.URL, id), "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	var corpo struct {
		Execucao struct{ ID string }
		Erro     string
	}
	json.NewDecoder(r.Body).Decode(&corpo)
	return r.StatusCode, corpo.Execucao.ID
}

func TestDownloadComProgressoELinhaDoTempo(t *testing.T) {
	a := prepara(t)
	id := int64(3304790000) // mais nova do logado: patch atual, ainda não apresentada
	status, exID := baixa(t, a, id)
	if status != 202 || exID == "" {
		t.Fatalf("baixar: %d", status)
	}
	ev := eventos(t, a.srv.URL, exID)
	junto := strings.Join(ev, "\n")
	if !strings.Contains(junto, `"estado":"baixando"`) || !strings.Contains(junto, "concluido:") ||
		!strings.HasSuffix(junto, "fim:{}") || strings.Contains(junto, "erro:") {
		t.Fatalf("eventos:\n%s", junto)
	}
	if !strings.Contains(junto, `"linha_do_tempo":true`) {
		t.Fatalf("linha do tempo não guardada:\n%s", junto)
	}
	s, corpo := a.pede(t, "GET", fmt.Sprintf("/api/partidas/%d/linha-do-tempo", id))
	if s != 200 || !strings.Contains(string(corpo), "participantFrames") {
		t.Fatalf("linha do tempo: %d", s)
	}
	_, corpo = a.pede(t, "GET", fmt.Sprintf("/api/partidas/%d/replay", id))
	if !strings.Contains(string(corpo), `"estado":"baixado"`) {
		t.Fatalf("replay depois: %s", corpo)
	}
	// Nunca abre o replay.
	for _, c := range a.f.Chamadas {
		if strings.HasPrefix(c, "watch") {
			t.Fatalf("o download chamou watch: %v", a.f.Chamadas)
		}
	}
}

func TestDownloadRecusas(t *testing.T) {
	a := prepara(t)
	// Patch antigo.
	_, exID := baixa(t, a, 3304790000-20*1000)
	if j := strings.Join(eventos(t, a.srv.URL, exID), "\n"); !strings.Contains(j, "erro:") || !strings.Contains(j, "patch") {
		t.Fatalf("incompatível:\n%s", j)
	}
	// Partida ao vivo no cliente.
	a.f.Fase = "InProgress"
	_, exID = baixa(t, a, 3304790000)
	if j := strings.Join(eventos(t, a.srv.URL, exID), "\n"); !strings.Contains(j, "partida em andamento") {
		t.Fatalf("ao vivo:\n%s", j)
	}
	a.f.Fase = ""
	// Cliente fechado: a execução falha com a frase certa.
	a.f.Fechado = true
	_, exID = baixa(t, a, 3304790000)
	if j := strings.Join(eventos(t, a.srv.URL, exID), "\n"); !strings.Contains(j, "não está aberto") {
		t.Fatalf("fechado:\n%s", j)
	}
}

func TestDownloadDuploE409(t *testing.T) {
	a := prepara(t)
	a.f.PassoDownload = 0.5 // lento: o segundo pedido chega com o primeiro rodando
	IntervaloPoll = 5 * time.Millisecond
	s1, ex1 := baixa(t, a, 3304790000)
	s2, ex2 := baixa(t, a, 3304790000)
	if s1 != 202 || s2 != 409 || ex1 != ex2 {
		t.Fatalf("esperava 202 e 409 com a mesma execução: %d %d %s %s", s1, s2, ex1, ex2)
	}
	http.Post(a.srv.URL+"/api/execucoes/"+ex1+"/cancelar", "application/json", nil)
	if j := strings.Join(eventos(t, a.srv.URL, ex1), "\n"); !strings.Contains(j, `"cancelada":true`) {
		t.Fatalf("cancelar:\n%s", j)
	}
}

func TestListaComClienteFechadoUsaBanco(t *testing.T) {
	a := prepara(t)
	a.pede(t, "GET", "/api/jogadores/"+lcu.FalsoLogado.Puuid+"/partidas?quantidade=5")
	a.f.Fechado = true
	s, corpo := a.pede(t, "GET", "/api/jogadores/"+lcu.FalsoLogado.Puuid+"/partidas?quantidade=20")
	var pag Pagina
	json.Unmarshal(corpo, &pag)
	if s != 200 || pag.Fonte != "banco" || len(pag.Partidas) != 5 || pag.Partidas[1].Replay.Estado != Baixado {
		t.Fatalf("%d %s", s, corpo)
	}
	if s, _ := a.pede(t, "GET", "/api/jogadores/x/partidas?quantidade=500"); s != 400 {
		t.Fatalf("quantidade inválida: %d", s)
	}
}

func TestTraduz(t *testing.T) {
	casos := map[string]string{"download": Baixavel, "watch": Baixado, "incompatible": Incompativel,
		"downloading": Baixando, "lost": Indisponivel, "": Indisponivel}
	for lcuEstado, app := range casos {
		if got := traduz(lcu.MetadataReplay{Estado: lcuEstado}).Estado; got != app {
			t.Errorf("%q → %q, esperava %q", lcuEstado, got, app)
		}
	}
	// O lixo que o LCU de verdade mandou em downloadProgress fora do download.
	if r := traduz(lcu.MetadataReplay{Estado: "download", Progresso: 2426758726}); r.Progresso != 0 {
		t.Errorf("progresso fora do download: %v", r.Progresso)
	}
	if r := traduz(lcu.MetadataReplay{Estado: "downloading", Progresso: 250}); r.Progresso != 100 {
		t.Errorf("progresso sem teto: %v", r.Progresso)
	}
}
