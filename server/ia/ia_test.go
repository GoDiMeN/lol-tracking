package ia

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"loltracking/server/analise"
	"loltracking/server/banco"
	"loltracking/server/partida"
	"loltracking/server/rastreio"
	"loltracking/server/web"
)

// fontesFalsas: uma partida fixa (3304217746, a Vi é a 7), sem banco.
type fontesFalsas struct {
	outras []analise.LinhaBusca
	refs   bool // há partidas da coleta por elo (referências das vozes)
	// semElo: a partida não é da coleta (o elo dela é desconhecido), como a do dono.
	semElo bool
}

const jogoTeste = 3304217746

func (f fontesFalsas) Partida(ctx context.Context, id int64) (InfoPartida, error) {
	if id != jogoTeste {
		return InfoPartida{}, partida.ErrNaoEncontrada
	}
	info := InfoPartida{GameID: id, DuracaoS: 1800, Elo: "PLATINUM"}
	if f.semElo {
		info.Elo = ""
	}
	camps := []string{"Garen", "Lee Sin", "Ahri", "Jinx", "Leona", "Darius", "Vi", "Syndra", "Kai'Sa", "Nautilus"}
	funcs := []string{"TOP", "JUNGLE", "MIDDLE", "BOTTOM", "UTILITY"}
	for i, c := range camps {
		t := 100
		if i >= 5 {
			t = 200
		}
		info.Participantes = append(info.Participantes, Participante{ID: i + 1, Campeao: c, Nome: "Nome" + fmt.Sprint(i+1), Funcao: funcs[i%5], Time: t, Puuid: fmt.Sprint("p", i+1), Tag: "BR1"})
	}
	info.Momentos = []analise.MomentoChave{
		{N: 1, TMs: 528000, Tipo: "abate", Subtipo: "morte_cara", ParticipanteID: 7, Autor: 2, Envolvidos: []int{7, 2, 3}, Impacto: 900, Tags: []string{"sozinho"}, Frase: "Vi morreu sozinha para Lee Sin."},
		{N: 2, TMs: 738000, Tipo: "abate", ParticipanteID: 7, Autor: 3, Envolvidos: []int{7, 3}, Impacto: 600, Tags: []string{"sozinho"}, Frase: "Vi morreu para Ahri."},
		{N: 3, TMs: 300000, Tipo: "janela_perdida", ParticipanteID: 7, Impacto: 200, Frase: "Lee Sin morto e Vi farmando."},
	}
	return info, nil
}
func (fontesFalsas) ContextoPartida(ctx context.Context, id int64, pid int) (string, error) {
	return fmt.Sprintf("# Partida %d — foco no %d\n## Mortes de Vi\n- [8:48] Vi morreu sozinha.\n- [12:18] Vi morreu para Ahri.\n## Janelas\n- [5:00] janela perdida.", id, pid), nil
}
func (fontesFalsas) Visao(ctx context.Context, id int64, pid int) (string, error) {
	return "## Visão\n- Sem passe de visão de time.\n", nil
}
func (fontesFalsas) Momento(ctx context.Context, id int64, t int64) (string, error) {
	return fmt.Sprintf("Os 10 no instante %s: Vi na selva vermelha de cima.\n", analise.MinSeg(t)), nil
}
func (fontesFalsas) Fichas(ctx context.Context, id int64, pid int, ts []int64) ([]string, error) {
	var r []string
	for _, t := range ts {
		r = append(r, fmt.Sprintf("- ficha em %s: Lee Sin perto, sem sentinela.\n", analise.MinSeg(t)))
	}
	return r, nil
}
func (f fontesFalsas) OutrasDoJogador(puuid, tipo, tag string, exclui int64) ([]analise.LinhaBusca, error) {
	return f.outras, nil
}
func (f fontesFalsas) VariasPartidas(fl analise.FiltroResumos) (string, []int64, error) {
	// A conversa geral só escolhe entre as partidas pesquisadas: sem o filtro, erro.
	if !fl.SoPesquisa {
		return "", nil, fmt.Errorf("conversa geral sem o filtro SoPesquisa")
	}
	if (fl.SoComElo || fl.Elo != "") && !f.refs || fl.Elo == "CHALLENGER" {
		return "", nil, nil
	}
	if fl.Campeao != "" && fl.Campeao != "Vi" {
		return "", nil, nil
	}
	return "# Base de fatos de várias partidas\n- partida 3304217746, Vi 3/7/5 | [8:48] Vi morreu sozinha.", []int64{jogoTeste}, nil
}
func (fontesFalsas) Campeoes() ([]string, error) { return []string{"Lee Sin", "Vi", "Ahri"}, nil }

// Indicadores: a Vi (7) desta partida, rastreada, com 3 mortes sozinha.
func (fontesFalsas) Indicadores(id int64, pid int) (analise.IndicadoresJogador, error) {
	return analise.IndicadoresJogador{GameID: id, ParticipanteID: pid, Puuid: "p7", Campeao: "Vi", Funcao: "JUNGLE", DuracaoS: 1711,
		Rastreado: true, Abates: 3, Mortes: 7, Assistencias: 4, MortesSozinho: 3, MortesMeio: 4, JanelasPerdidas: 2,
		Etiquetas: map[string]map[string]int{"meio": {"farmando": 191, "andando": 108}, "total": {"gankando": 43}}}, nil
}

// Referencias: com refs, um caçador da coleta (o mesmo campeão quando o filtro
// pede); nada no Challenger nem com campeão que não seja a Vi.
func (f fontesFalsas) Referencias(fl analise.FiltroReferencia) ([]analise.IndicadoresJogador, error) {
	if !f.refs || strings.EqualFold(fl.Elo, "CHALLENGER") || (fl.Campeao != "" && fl.Campeao != "Vi") || (fl.Funcao != "" && fl.Funcao != "JUNGLE") {
		return nil, nil
	}
	camp, elo := "Kayn", fl.Elo
	if fl.Campeao != "" {
		camp = fl.Campeao
	}
	if elo == "" {
		elo = "PLATINUM"
	}
	return []analise.IndicadoresJogador{{GameID: 3305481832, ParticipanteID: 7, Puuid: "ref", Campeao: camp, Funcao: "JUNGLE", Elo: elo + " II",
		EloPartida: elo, Venceu: true, DuracaoS: 2280, Rastreado: true, Abates: 19, Mortes: 7, Assistencias: 12, MortesSozinho: 4,
		MortesMeio: 1, JanelasAproveitadas: 10, JanelasPerdidas: 1,
		Etiquetas: map[string]map[string]int{"meio": {"farmando": 296, "andando": 165}, "total": {"invadindo": 312}}}}, nil
}

type ambienteTeste struct {
	s   *Servico
	mux *http.ServeMux
	f   *Falso
	db  *banco.DB
}

func preparaServico(t *testing.T, c Cliente, conhecimento string, refs ...bool) ambienteTeste {
	t.Helper()
	db, err := banco.AbreMemoria(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := Migra(db); err != nil {
		t.Fatal(err)
	}
	f, _ := c.(*Falso)
	if c == nil {
		f = &Falso{}
		c = f
	}
	s := NovoServico(db, c, fontesFalsas{outras: []analise.LinhaBusca{{GameID: 999999001, Momentos: []analise.MomentoChave{{TMs: 400000}}}}, refs: len(refs) > 0 && refs[0]},
		web.NovoHub(), conhecimento)
	mux := http.NewServeMux()
	web.RegistraExecucoes(mux, s.Hub)
	Registra(mux, s)
	return ambienteTeste{s: s, mux: mux, f: f, db: db}
}

func (a ambienteTeste) pede(t *testing.T, metodo, caminho string, corpo any) (int, map[string]any) {
	t.Helper()
	var b bytes.Buffer
	if corpo != nil {
		json.NewEncoder(&b).Encode(corpo)
	}
	r := httptest.NewRequest(metodo, caminho, &b)
	w := httptest.NewRecorder()
	a.mux.ServeHTTP(w, r)
	var m map[string]any
	json.Unmarshal(w.Body.Bytes(), &m)
	return w.Code, m
}

func esperaExecucao(t *testing.T, s *Servico, id string) []web.Evento {
	t.Helper()
	e, ok := s.Hub.Busca(id)
	if !ok {
		t.Fatalf("execução %s não existe", id)
	}
	for i := 0; i < 500 && !e.Fechada(); i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if !e.Fechada() {
		t.Fatal("a execução não terminou")
	}
	return e.Eventos()
}

func tipos(evs []web.Evento) string {
	var t []string
	for _, e := range evs {
		if e.Tipo != "trecho" {
			t = append(t, e.Tipo)
		}
	}
	return strings.Join(t, ",")
}

func TestRelatorioComIAFalsaSemMaterial(t *testing.T) {
	a := preparaServico(t, nil, t.TempDir())
	cod, m := a.pede(t, "POST", "/api/partidas/3304217746/ia/relatorio", map[string]any{"participante": 7})
	if cod != 202 {
		t.Fatalf("%d %v", cod, m)
	}
	evs := esperaExecucao(t, a.s, m["execucao"].(map[string]any)["id"].(string))
	if tt := tipos(evs); !strings.HasPrefix(tt, "contexto,inicio") || !strings.HasSuffix(tt, "concluido,fim") {
		t.Fatalf("eventos: %s", tt)
	}
	// O que foi mandado: foco, participantes, fichas, vozes sem material.
	msg := a.f.Pedidos[0].Mensagens[0].Texto
	for _, x := range []string{"FOCO: participante 7 (Vi", "VOZES COM MATERIAL: nenhuma", "## Participantes", "## Situações candidatas",
		"### S1 [5:00]", "ficha em 8:43", "OUTRAS partidas guardadas", "Mesmo tipo e etiqueta \"sozinho\" nesta partida também em: 12:18",
		"SEM MATERIAL (não preencha esta voz)"} {
		if !strings.Contains(msg, x) {
			t.Fatalf("faltou %q no texto mandado:\n%s", x, msg)
		}
	}
	if !strings.Contains(a.f.Pedidos[0].Sistema, "SITUAÇÕES") || a.f.Pedidos[0].Tipo != TipoAnalise {
		t.Fatal("o prompt de sistema da análise")
	}
	cod, m = a.pede(t, "GET", "/api/partidas/3304217746/ia/relatorio?participante=7", nil)
	if cod != 200 {
		t.Fatalf("%d %v", cod, m)
	}
	rel := m["relatorio"].(map[string]any)
	if rel["estado"] != "concluido" || rel["versao"].(float64) != 1 {
		t.Fatalf("%v", rel)
	}
	c := rel["conteudo"].(map[string]any)
	sits := c["situacoes"].([]any)
	if len(sits) != 2 {
		t.Fatalf("situações: %v", sits)
	}
	s1 := sits[0].(map[string]any)
	if s1["impacto"].(float64) != 300 || s1["pergunta"] == "" || s1["quadro"].(map[string]any)["seta"].(map[string]any)["para"].(map[string]any)["zona"] != "base_azul" {
		t.Fatalf("situação 1: %v", s1)
	}
	for _, v := range s1["vozes"].([]any) {
		if v.(map[string]any)["sem_dados"] != true || v.(map[string]any)["resposta"] != SemMaterial {
			t.Fatalf("sem material, toda voz diz que não tem: %v", v)
		}
	}
	if len(s1["vozes"].([]any)) != len(Vozes) || len(c["padroes"].([]any)) != 1 {
		t.Fatalf("vozes/padrões: %v", c)
	}
	env := rel["enviado"].(map[string]any)
	if env["caracteres_mensagem"].(float64) <= 0 || len(env["arquivos_conhecimento"].([]any)) != 0 {
		t.Fatalf("enviado: %v", env)
	}
	// Refazer: versão 2; a 1 continua lá.
	cod, m = a.pede(t, "POST", "/api/partidas/3304217746/ia/relatorio", map[string]any{"participante": 7})
	esperaExecucao(t, a.s, m["execucao"].(map[string]any)["id"].(string))
	cod, m = a.pede(t, "GET", "/api/partidas/3304217746/ia/relatorio?participante=7", nil)
	if m["relatorio"].(map[string]any)["versao"].(float64) != 2 || len(m["versoes"].([]any)) != 2 {
		t.Fatalf("versões: %v", m)
	}
	if cod, m = a.pede(t, "GET", "/api/partidas/3304217746/ia/relatorio?participante=7&versao=1", nil); m["relatorio"].(map[string]any)["versao"].(float64) != 1 {
		t.Fatalf("versão 1: %d %v", cod, m)
	}
	if _, m = a.pede(t, "GET", "/api/partidas/3304217746/ia/relatorios", nil); len(m["relatorios"].([]any)) != 1 {
		t.Fatalf("relatórios da partida: %v", m)
	}
	// A prévia: o mesmo texto, sem chamar a IA.
	antes := a.f.Chamadas
	if c, pv := a.pede(t, "GET", "/api/partidas/3304217746/ia/previa?participante=7", nil); c != 200 || !strings.Contains(pv["mensagem"].(string), "FOCO: participante 7") || a.f.Chamadas != antes {
		t.Fatalf("prévia: %d %v", c, pv)
	}
	// Erros.
	for _, x := range []struct {
		corpo any
		cam   string
		cod   int
	}{{map[string]any{"participante": 11}, "/api/partidas/3304217746/ia/relatorio", 400}, {map[string]any{"participante": 3}, "/api/partidas/42/ia/relatorio", 404}} {
		if c, _ := a.pede(t, "POST", x.cam, x.corpo); c != x.cod {
			t.Fatalf("%s %v: %d", x.cam, x.corpo, c)
		}
	}
	if c, _ := a.pede(t, "GET", "/api/partidas/3304217746/ia/relatorio", nil); c != 400 {
		t.Fatalf("sem participante: %d", c)
	}
	if _, m = a.pede(t, "GET", "/api/partidas/3304217746/ia/relatorio?participante=3", nil); m["relatorio"] != nil {
		t.Fatalf("sem relatório: %v", m)
	}
}

func TestRelatorioComMaterialDeUmaVoz(t *testing.T) {
	pasta := t.TempDir()
	os.WriteFile(filepath.Join(pasta, "profissional.md"), []byte("# Caderno do profissional\nCaçador não entra sem visão do rio."), 0o644)
	os.WriteFile(filepath.Join(pasta, "mono-vi.md"), []byte("Vi: guarde o R para o carregador."), 0o644)
	a := preparaServico(t, nil, pasta, true)
	_, m := a.pede(t, "POST", "/api/partidas/3304217746/ia/relatorio", map[string]any{"participante": 7})
	esperaExecucao(t, a.s, m["execucao"].(map[string]any)["id"].(string))
	msg := a.f.Pedidos[0].Mensagens[0].Texto
	// mono tem arquivo E partidas de referência (o filtro do mono não é Challenger); melhores do elo também.
	if !strings.Contains(msg, "VOZES COM MATERIAL: profissional, mono, melhores_do_elo, esmeralda_recem") ||
		!strings.Contains(msg, "Caçador não entra sem visão do rio") || !strings.Contains(msg, "### Do arquivo mono-vi.md") {
		t.Fatalf("material:\n%s", msg)
	}
	if strings.Contains(msg, "pasta") && strings.Contains(msg, pasta) {
		t.Fatal("o caminho da pasta não sai da máquina")
	}
	_, m = a.pede(t, "GET", "/api/partidas/3304217746/ia/relatorio?participante=7", nil)
	rel := m["relatorio"].(map[string]any)
	vs := rel["conteudo"].(map[string]any)["situacoes"].([]any)[0].(map[string]any)["vozes"].([]any)
	por := map[string]map[string]any{}
	for _, v := range vs {
		por[v.(map[string]any)["voz"].(string)] = v.(map[string]any)
	}
	if por["profissional"]["sem_dados"] != false || por["challenger"]["sem_dados"] != true || por["especialista_soloq"]["resposta"] != SemMaterial {
		t.Fatalf("vozes: %v", por)
	}
	arqs := rel["enviado"].(map[string]any)["arquivos_conhecimento"].([]any)
	if len(arqs) != 2 {
		t.Fatalf("arquivos: %v", arqs)
	}
}

func TestValidaRelatorio(t *testing.T) {
	info, _ := fontesFalsas{}.Partida(context.Background(), jogoTeste)
	vozes := []MaterialVoz{{Voz: Vozes[0], Texto: "material"}, {Voz: Vozes[1]}}
	resp := "Aqui está:\n```json\n" + `{
  "resumo": "ok", // comentário copiado do esquema
  "padroes": [{"texto": "3 vezes", "instantes": ["8:48", "99:00"]}],
  "situacoes": [
    {"t": "8:48", "titulo": "A", "situacao": "fatos", "pergunta": "?", "impacto": "~1.200",
     "vozes": [{"voz": "profissional", "resposta": "pelo caderno", "fonte": "profissional.md"}, {"voz": "especialista_soloq", "resposta": "inventado"}],
     "quadro": {"t": "8:43", "legenda": "x", "realce": [7, 42], "seta": {"de": "7", "para": "cova_dragao"}, "regiao": "lua"}},
    {"t": "45:00", "titulo": "fora da partida"},
    {"t": "2:00", "titulo": "B", "impacto": 5000, "quadro": {"seta": {"de": 99, "para": 2}}}
  ],
  "jogadores_chave": [{"participante": 2, "papel": "x"}, {"participante": 12}],
  "janelas": [{"t": "5:00", "tipo": "talvez"}],
  "visao": {"texto": "v", "itens": [{"t": "x"}]}
}` + "\n```"
	c, avisos, err := ValidaRelatorio(resp, info, vozes)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Situacoes) != 2 || c.Situacoes[0].T != "2:00" || c.Situacoes[1].Impacto != 1200 {
		t.Fatalf("situações (por impacto, sem a fora da partida): %+v", c.Situacoes)
	}
	s := c.Situacoes[1]
	if s.Quadro.T != "8:43" || len(s.Quadro.Realce) != 1 || s.Quadro.Seta == nil || s.Quadro.Seta.De.Participante != 7 ||
		s.Quadro.Seta.Para.Zona != "cova_dragao" || s.Quadro.Regiao != "" {
		t.Fatalf("quadro: %+v %+v", s.Quadro, s.Quadro.Seta)
	}
	if s.Vozes[0].Resposta != "pelo caderno" || !s.Vozes[1].SemDados || s.Vozes[1].Resposta != SemMaterial {
		t.Fatalf("vozes: %+v", s.Vozes)
	}
	if c.Situacoes[0].Quadro.Seta != nil {
		t.Fatal("seta com participante inexistente cai")
	}
	if len(c.JogadoresChave) != 1 || c.JogadoresChave[0].Campeao != "Lee Sin" || c.Janelas[0].Tipo != "perdida" || len(c.Visao.Itens) != 0 {
		t.Fatalf("%+v", c)
	}
	if len(c.Padroes[0].Instantes) != 1 {
		t.Fatalf("instante fora da partida sai do padrão: %v", c.Padroes)
	}
	tudo := strings.Join(avisos, "\n")
	for _, x := range []string{"situação 2 descartado", "voz \"especialista_soloq\", que não tem material", "realce \"42\" ignorado", "região \"lua\" ignorada", "seta ignorada", "jogador-chave 2 descartado"} {
		if !strings.Contains(tudo, x) {
			t.Fatalf("faltou o aviso %q:\n%s", x, tudo)
		}
	}
	if _, _, err := ValidaRelatorio("não sei responder", info, vozes); !errors.Is(err, ErrFormato) {
		t.Fatalf("sem JSON: %v", err)
	}
}

func TestConversaDaPartidaComPedidoDeMomento(t *testing.T) {
	a := preparaServico(t, nil, t.TempDir())
	cod, m := a.pede(t, "POST", "/api/ia/conversas", map[string]any{"game_id": jogoTeste, "participante": 7})
	if cod != 201 {
		t.Fatalf("%d %v", cod, m)
	}
	id := int64(m["conversa"].(map[string]any)["id"].(float64))
	cod, m = a.pede(t, "POST", fmt.Sprintf("/api/ia/conversas/%d/mensagens", id), map[string]any{"texto": "O que deu errado aos 8:48?"})
	if cod != 202 {
		t.Fatalf("%d %v", cod, m)
	}
	evs := esperaExecucao(t, a.s, m["execucao"].(map[string]any)["id"].(string))
	if tt := tipos(evs); tt != "contexto,inicio,pedido,recomeca,inicio,concluido,fim" {
		t.Fatalf("eventos: %s", tt)
	}
	if a.f.Chamadas != 2 {
		t.Fatalf("duas chamadas (pedido + resposta): %d", a.f.Chamadas)
	}
	segunda := TextoDaConversa(a.f.Pedidos[1].Mensagens)
	if !strings.Contains(segunda, "DADOS DO INSTANTE 8:48 da partida 3304217746") || !strings.Contains(a.f.Pedidos[1].Sistema, "## Material das vozes") {
		t.Fatalf("a segunda chamada leva os dados:\n%s", segunda)
	}
	_, m = a.pede(t, "GET", fmt.Sprintf("/api/ia/conversas/%d", id), nil)
	ms := m["mensagens"].([]any)
	if len(ms) != 3 {
		t.Fatalf("pergunta, dados, resposta: %v", ms)
	}
	r := ms[2].(map[string]any)
	if r["papel"] != "assistente" || r["estado"] != "ok" || strings.Contains(r["texto"].(string), "PRÓXIMA") ||
		r["dados"].(map[string]any)["proxima_pergunta"] != "E o que fazer na próxima vez em 8:48?" {
		t.Fatalf("resposta: %v", r)
	}
	if ms[1].(map[string]any)["papel"] != "dados" || m["conversa"].(map[string]any)["titulo"] != "O que deu errado aos 8:48?" {
		t.Fatalf("%v", m)
	}
	// Lista e apagar.
	if _, m = a.pede(t, "GET", "/api/ia/conversas?partida=3304217746&participante=7", nil); len(m["conversas"].([]any)) != 1 {
		t.Fatalf("lista: %v", m)
	}
	if c, _ := a.pede(t, "POST", fmt.Sprintf("/api/ia/conversas/%d/mensagens", id), map[string]any{"texto": "  "}); c != 400 {
		t.Fatalf("pergunta vazia: %d", c)
	}
	if c, _ := a.pede(t, "DELETE", fmt.Sprintf("/api/ia/conversas/%d", id), nil); c != 204 {
		t.Fatalf("apagar: %d", c)
	}
	if c, _ := a.pede(t, "GET", fmt.Sprintf("/api/ia/conversas/%d", id), nil); c != 404 {
		t.Fatalf("apagada: %d", c)
	}
	if c, _ := a.pede(t, "POST", "/api/ia/conversas", map[string]any{"game_id": jogoTeste}); c != 400 {
		t.Fatalf("conversa de partida sem participante: %d", c)
	}
}

func TestConversaGeral(t *testing.T) {
	a := preparaServico(t, nil, t.TempDir())
	// A partida da base é do usuário (veio da busca de um jogador); a 555 é só de estudo.
	if err := rastreio.Migra(a.db); err != nil {
		t.Fatal(err)
	}
	if err := rastreio.MarcaOrigem(a.db, jogoTeste, rastreio.OrigemPesquisa, "p7"); err != nil {
		t.Fatal(err)
	}
	if err := rastreio.MarcaOrigem(a.db, 555, rastreio.OrigemEstudo, "ref"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.s.atendePedido(context.Background(), PedidoDado{Tipo: "partida", GameID: 555, ParticipanteID: 1}, Conversa{}); err == nil ||
		!strings.Contains(err.Error(), "não está entre as partidas rastreadas do usuário") {
		t.Fatalf("a conversa geral não puxa partida de estudo: %v", err)
	}
	_, m := a.pede(t, "POST", "/api/ia/conversas", map[string]any{"game_id": 0})
	id := int64(m["conversa"].(map[string]any)["id"].(float64))
	_, m = a.pede(t, "POST", fmt.Sprintf("/api/ia/conversas/%d/mensagens", id), map[string]any{"texto": "Por que eu morro tanto de Vi antes dos 10 min?", "puuid": "p7"})
	evs := esperaExecucao(t, a.s, m["execucao"].(map[string]any)["id"].(string))
	var partidas map[string]any
	for _, e := range evs {
		if e.Tipo == "partidas" {
			json.Unmarshal(e.Dados, &partidas)
		}
	}
	if partidas == nil || !strings.Contains(partidas["filtro"].(string), "campeão Vi") || len(partidas["partidas"].([]any)) != 1 {
		t.Fatalf("partidas escolhidas: %v", partidas)
	}
	sis := a.f.Pedidos[0].Sistema
	if !strings.Contains(sis, "VÁRIAS PARTIDAS") || !strings.Contains(sis, "partida 3304217746, Vi") {
		t.Fatalf("sistema:\n%s", sis)
	}
	_, m = a.pede(t, "GET", fmt.Sprintf("/api/ia/conversas/%d", id), nil)
	r := m["mensagens"].([]any)[1].(map[string]any)
	if ps := r["partidas"].([]any); len(ps) != 1 || ps[0].(float64) != jogoTeste {
		t.Fatalf("partidas citadas: %v", r)
	}
	// Champion que não está na base: afrouxa até achar.
	_, m = a.pede(t, "POST", fmt.Sprintf("/api/ia/conversas/%d/mensagens", id), map[string]any{"texto": "e no Challenger?"})
	evs = esperaExecucao(t, a.s, m["execucao"].(map[string]any)["id"].(string))
	for _, e := range evs {
		if e.Tipo == "partidas" && !strings.Contains(string(e.Dados), "afrouxado") {
			t.Fatalf("devia afrouxar: %s", e.Dados)
		}
	}
}

func TestFiltroDaPergunta(t *testing.T) {
	c := []string{"Lee Sin", "Vi"}
	f, d := FiltroDaPergunta("Por que eu perco de Lee Sin no jungle antes dos 12 minutos?", c, "p1")
	if f.Campeao != "Lee Sin" || f.Funcao != "JUNGLE" || f.Resultado != "derrota" || f.Tipo != "abate" || f.AteMs != 720000 || f.Puuid != "p1" {
		t.Fatalf("%+v %v", f, d)
	}
	f, _ = FiltroDaPergunta("vitórias de Vi com dragão na platina", c, "")
	if f.Campeao != "Vi" || f.Resultado != "vitoria" || f.Tipo != "objetivo" || f.Elo != "PLATINUM" || f.Puuid != "" {
		t.Fatalf("%+v", f)
	}
	// "Vision" não casa com "Vi": só palavra inteira.
	if f, _ = FiltroDaPergunta("Vision score?", c, ""); f.Campeao != "" {
		t.Fatalf("%+v", f)
	}
}

func TestLePedidos(t *testing.T) {
	p := LePedidos("PEDIDO_MOMENTO t=8:48\nPEDIDO_MOMENTO partida=123456789 t=12:05\nPEDIDO_PARTIDA partida=123456789 participante=7\nPEDIDO_MOMENTO t=1:00", 42)
	if len(p) != MaxPedidosPorVolta || p[0] != (PedidoDado{Tipo: "momento", GameID: 42, TMs: 528000}) || p[1].GameID != 123456789 || p[2].Tipo != "momento" {
		t.Fatalf("%+v", p)
	}
	if len(LePedidos("No 8:48 a Vi entrou sozinha.", 42)) != 0 {
		t.Fatal("texto comum não é pedido")
	}
}

func TestIndisponivelE409(t *testing.T) {
	a := preparaServico(t, indisponivel{}, t.TempDir())
	if c, m := a.pede(t, "POST", "/api/partidas/3304217746/ia/relatorio", map[string]any{"participante": 7}); c != 503 || !strings.Contains(m["erro"].(string), "Claude Code") {
		t.Fatalf("%d %v", c, m)
	}
	if c, m := a.pede(t, "GET", "/api/ia/estado", nil); c != 200 || m["disponivel"] != false || len(m["vozes"].([]any)) != len(Vozes) {
		t.Fatalf("%d %v", c, m)
	}
	// Uma análise rodando do mesmo jogador: 409 com a execução.
	trava := make(chan struct{})
	b := preparaServico(t, &Falso{Responde: func(p Pedido) (string, error) { <-trava; return relatorioFalso(p.Mensagens[0].Texto), nil }}, t.TempDir())
	_, m := b.pede(t, "POST", "/api/partidas/3304217746/ia/relatorio", map[string]any{"participante": 7})
	c, m2 := b.pede(t, "POST", "/api/partidas/3304217746/ia/relatorio", map[string]any{"participante": 7})
	if c != 409 || m2["execucao"].(map[string]any)["id"] != m["execucao"].(map[string]any)["id"] {
		t.Fatalf("%d %v", c, m2)
	}
	// Cancelar: o relatório fica "cancelado".
	ex, _ := b.s.Hub.Busca(m["execucao"].(map[string]any)["id"].(string))
	ex.Cancela()
	close(trava)
	esperaExecucao(t, b.s, ex.ID())
	_, m = b.pede(t, "GET", "/api/partidas/3304217746/ia/relatorio?participante=7", nil)
	if est := m["relatorio"].(map[string]any)["estado"]; est != "cancelado" && est != "concluido" {
		t.Fatalf("estado: %v", est)
	}
}

type indisponivel struct{}

func (indisponivel) Estado(context.Context) Estado {
	return Estado{Implementacao: "terminal", Motivo: "O Claude Code não foi encontrado nesta máquina."}
}
func (indisponivel) Conversa(context.Context, Pedido, func(string)) (Resposta, error) {
	return Resposta{}, errors.New("não devia ser chamado")
}

func TestFilaUmaPorVez(t *testing.T) {
	solta := make(chan struct{})
	dentro := make(chan struct{}, 2)
	f := NovaFila(&Falso{Responde: func(Pedido) (string, error) { dentro <- struct{}{}; <-solta; return "ok", nil }})
	go f.Conversa(context.Background(), Pedido{}, nil)
	<-dentro
	if !f.Ocupada() {
		t.Fatal("ocupada")
	}
	ctx, cancela := context.WithCancel(context.Background())
	feito := make(chan error)
	go func() { _, err := f.Conversa(ctx, Pedido{}, nil); feito <- err }()
	time.Sleep(20 * time.Millisecond)
	if f.Esperando() != 1 || len(dentro) != 0 {
		t.Fatalf("a segunda espera: %d", f.Esperando())
	}
	cancela()
	if err := <-feito; err == nil || !strings.Contains(err.Error(), "esperava a vez") {
		t.Fatalf("%v", err)
	}
	close(solta)
}

func TestSlugEArquivosDaVoz(t *testing.T) {
	for in, out := range map[string]string{"Lee Sin": "lee-sin", "Kai'Sa": "kaisa", "Nunu & Willump": "nunu-willump", "Dr. Mundo": "dr-mundo", "Bel'Veth": "belveth"} {
		if s := slug(in); s != out {
			t.Fatalf("%s → %s", in, s)
		}
	}
	if a := ArquivosDaVoz(Vozes[3], "Vi", "PLATINUM"); len(a) != 2 || a[0] != "melhores-do-elo-platinum.md" {
		t.Fatalf("%v", a)
	}
}

// Interruptor LOL_ESTUDO desligado: as rotas do Especialista Lane (parte do
// Estudo) não existem; as da IA da partida continuam.
func TestEspecialistaSomeComOEstudoDesligado(t *testing.T) {
	a := preparaServico(t, nil, t.TempDir())
	if c, _ := a.pede(t, "GET", "/api/especialista", nil); c != 200 {
		t.Fatalf("com o estudo ligado o painel responde: %d", c)
	}
	a.s.SemEstudo = true
	mux := http.NewServeMux()
	mux.Handle("/", web.Static(""))
	Registra(mux, a.s)
	for _, rota := range []string{"/api/especialista", "/api/especialista/sintese?patch=16.23&elo=PLATINUM&rota=JUNGLE", "/api/especialista/espelho"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", rota, nil))
		if w.Code != 404 {
			t.Fatalf("%s com o estudo desligado: %d", rota, w.Code)
		}
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/ia/estado", nil))
	if w.Code != 200 {
		t.Fatalf("a IA da partida continua: %d", w.Code)
	}
}
