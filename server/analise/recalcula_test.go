package analise

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"loltracking/server/banco"
	"loltracking/server/campeao"
	"loltracking/server/lcu"
	"loltracking/server/partida"
	"loltracking/server/rastreio"
	"loltracking/server/replay"
	"loltracking/server/riot"
	"loltracking/server/web"
)

// A partida do LCU falso com replay baixado.
const partidaTeste = 3304789000

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

// riotFalsa devolve uma linha do tempo da Riot com o abate do jogo falso.
type riotFalsa struct {
	pedidos int
	err     error
}

func (r *riotFalsa) LinhaDoTempo(ctx context.Context, gameID int64) ([]byte, error) {
	r.pedidos++
	if r.err != nil {
		return nil, r.err
	}
	return []byte(`{"info":{"frames":[{"timestamp":0,"participantFrames":{},"events":[
		{"type":"WARD_PLACED","timestamp":50000,"creatorId":3,"wardType":"YELLOW_TRINKET"},
		{"type":"CHAMPION_KILL","timestamp":100200,"killerId":8,"victimId":3,"bounty":300,"killStreakLength":1,
		 "position":{"x":7000,"y":7000},
		 "victimDamageReceived":[{"basic":true,"magicDamage":0,"name":"LeBlanc","participantId":8,"physicalDamage":120,"spellName":"","spellSlot":64,"trueDamage":0,"type":"OTHER"},
		  {"basic":false,"magicDamage":300,"name":"LeBlanc","participantId":8,"physicalDamage":0,"spellName":"leblancq","spellSlot":0,"trueDamage":0,"type":"OTHER"}],
		 "victimDamageDealt":[]}]}]}}`), nil
}

type ambiente struct {
	db  *banco.DB
	d   Deps
	mux *http.ServeMux
}

// preparaRastreada roda um rastreio falso dos jogadores 3 e 8 (o 3 morre de
// 100 a 120 s, morto pelo 8) e devolve o banco pronto para a análise.
func preparaRastreada(t *testing.T, rf rastreio.FonteRiot) *ambiente {
	t.Helper()
	rastreio.IntervaloFase = 0
	db, err := banco.AbreMemoria(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, m := range []func(*banco.DB) error{partida.Migra, rastreio.Migra, Migra} {
		if err := m(db); err != nil {
			t.Fatal(err)
		}
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
	jogo.DuracaoS = 240
	jogo.Elenco = nil
	for _, pt := range p.Participantes {
		tm := "ORDER"
		if pt.Time == 200 {
			tm = "CHAOS"
		}
		jogo.Elenco = append(jogo.Elenco, replay.JogadorFalso{Nome: pt.Nome, Campeao: pt.Campeao, Time: tm})
	}
	jogo.Mortes = []replay.MorteFalsa{{Indice: 2, De: 100, Ate: 120, Matador: 7}}
	if err := partida.GravaLinhaDoTempo(db, partidaTeste, jogo.LinhaDoTempo()); err != nil {
		t.Fatal(err)
	}
	cli := lcuComJogo{Falso: l, jogo: jogo}
	hub := web.NovoHub()
	d := Deps{DB: db, Riot: rf}
	fila := rastreio.NovaFila(rastreio.Deps{DB: db, LCU: cli, Hub: hub,
		Replay: replay.Deps{LCU: cli, API: jogo, Jogo: jogo, Teclado: jogo, Tela: jogo, Relogio: rel}})
	if _, _, err := fila.Cria(ctx, partidaTeste, rastreio.Pedido{Jogadores: []int{3, 8}, Velocidade: 8}); err != nil {
		t.Fatal(err)
	}
	fila.Espera()
	mux := http.NewServeMux()
	Registra(mux, d)
	return &ambiente{db: db, d: d, mux: mux}
}

func (a *ambiente) pede(t *testing.T, metodo, caminho string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	a.mux.ServeHTTP(rec, httptest.NewRequest(metodo, caminho, strings.NewReader("")))
	var m map[string]any
	json.Unmarshal(rec.Body.Bytes(), &m)
	return rec.Code, m
}

func fotografia(t *testing.T, db *banco.DB) string {
	t.Helper()
	var b strings.Builder
	for _, q := range []string{
		`SELECT n || dados FROM retratos_abate ORDER BY n`,
		`SELECT participante_id || indicador || COALESCE(valor, 'nulo') || aproximado || detalhe FROM indicadores ORDER BY participante_id, indicador`,
		`SELECT passe_id || t_ms || tipo || dados FROM cadeia ORDER BY passe_id, t_ms, tipo, dados`,
	} {
		linhas, err := db.Query(q)
		if err != nil {
			t.Fatal(err)
		}
		for linhas.Next() {
			var s string
			linhas.Scan(&s)
			b.WriteString(s + "\n")
		}
		linhas.Close()
	}
	return b.String()
}

func TestRecalculaIdempotenteERetrato(t *testing.T) {
	rf := &riotFalsa{}
	a := preparaRastreada(t, rf)
	r1, err := Recalcula(context.Background(), a.d, partidaTeste)
	if err != nil {
		t.Fatal(err)
	}
	if len(r1.Rastreados) != 2 || r1.Abates != 1 || !r1.Riot || r1.Indicadores == 0 {
		t.Fatalf("%+v", r1)
	}
	foto := fotografia(t, a.db)
	r2, err := Recalcula(context.Background(), a.d, partidaTeste)
	if err != nil {
		t.Fatal(err)
	}
	if foto2 := fotografia(t, a.db); foto2 != foto {
		t.Fatal("Recalcula não é idempotente")
	}
	if rf.pedidos != 1 || r2.Abates != 1 {
		t.Fatalf("a linha do tempo da Riot devia ser pedida uma vez só (%d)", rf.pedidos)
	}
	rt, err := LeRetrato(a.db, partidaTeste, 1)
	if err != nil {
		t.Fatal(err)
	}
	if rt.Vitima != 3 || rt.Matador != 8 || len(rt.Jogadores) != 10 {
		t.Fatalf("%+v", rt)
	}
	por := map[int]JogadorNoAbate{}
	for _, j := range rt.Jogadores {
		por[j.ParticipanteID] = j
	}
	if v := por[3]; v.Papel != "vitima" || v.Origem != "evento" || v.Vivo == nil || *v.Vivo || !v.Rastreado {
		t.Fatalf("vítima %+v", v)
	}
	if m := por[8]; m.Papel != "matador" || m.Origem != "rastreado" || m.Aproximada || m.DistVitima == nil {
		t.Fatalf("matador %+v", m)
	}
	if o := por[1]; o.Origem != "linha_do_tempo" || !o.Aproximada || o.Rastreado {
		t.Fatalf("não rastreado %+v", o)
	}
	if rt.Riot == nil || len(rt.Riot.Recebido) != 2 || rt.Riot.PorJogador[8] != 420 || rt.Riot.Recebido[1].Habilidade != "leblancq" {
		t.Fatalf("riot %+v", rt.Riot)
	}
	// Indicadores: os da linha do tempo para os 10; os do rastreio só para quem foi rastreado.
	inds, _ := ListaIndicadores(a.db, partidaTeste, 0)
	tem := map[string]bool{}
	for _, in := range inds {
		tem[in.Indicador.Indicador+"/"+string(rune('0'+in.ParticipanteID))] = true
		if in.Indicador.Indicador == "mortes" && in.ParticipanteID == 3 && (in.Valor == nil || *in.Valor != 1) {
			t.Fatalf("mortes do 3: %+v", in)
		}
	}
	for _, k := range []string{"mortes/1", "cs_por_min/1", "saidas_rota_ate14_n/3", "habilidades_por_min/8", "wards_ate14/3", "tempo_morto_s/1"} {
		if !tem[k] && !strings.HasPrefix(k, "habilidades") {
			t.Errorf("faltou %s", k)
		}
	}
	if tem["saidas_rota_ate14_n/1"] {
		t.Error("indicador do rastreio para quem não foi rastreado")
	}
}

func TestRecalculaSemRiotESemLinha(t *testing.T) {
	a := preparaRastreada(t, &riotFalsa{err: riot.ErrSemChave})
	r, err := Recalcula(context.Background(), a.d, partidaTeste)
	if err != nil || r.Riot || len(r.Avisos) != 0 {
		t.Fatalf("%+v %v", r, err)
	}
	rt, _ := LeRetrato(a.db, partidaTeste, 1)
	if rt.Riot != nil {
		t.Fatal("sem chave não há Riot")
	}
	// Chave recusada vira aviso, não erro.
	a.d.Riot = &riotFalsa{err: riot.ErrChaveRecusada}
	if r, err = Recalcula(context.Background(), a.d, partidaTeste); err != nil || len(r.Avisos) != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := a.db.Exec(`DELETE FROM linhas_do_tempo WHERE game_id = ?`, partidaTeste); err != nil {
		t.Fatal(err)
	}
	if _, err := Recalcula(context.Background(), a.d, partidaTeste); !errors.Is(err, ErrSemLinhaDoTempo) {
		t.Fatal(err)
	}
	if _, err := Recalcula(context.Background(), a.d, 42); !errors.Is(err, partida.ErrNaoEncontrada) {
		t.Fatal(err)
	}
}

func TestRotasDaAnalise(t *testing.T) {
	a := preparaRastreada(t, &riotFalsa{})
	id := "3304789000"
	if c, m := a.pede(t, "GET", "/api/partidas/"+id+"/analise"); c != 200 || m["analise"] != nil {
		t.Fatal(c, m)
	}
	if c, m := a.pede(t, "POST", "/api/partidas/"+id+"/analise"); c != 200 || m["analise"].(map[string]any)["abates"].(float64) != 1 {
		t.Fatal(c, m)
	}
	if c, m := a.pede(t, "GET", "/api/partidas/"+id+"/analise"); c != 200 || m["analise"] == nil {
		t.Fatal(c, m)
	}
	if c, m := a.pede(t, "GET", "/api/partidas/"+id+"/abates"); c != 200 || len(m["abates"].([]any)) != 1 {
		t.Fatal(c, m)
	}
	if c, m := a.pede(t, "GET", "/api/partidas/"+id+"/abates/1"); c != 200 || m["abate"] == nil {
		t.Fatal(c, m)
	}
	if c, _ := a.pede(t, "GET", "/api/partidas/"+id+"/abates/9"); c != 404 {
		t.Fatal(c)
	}
	if c, m := a.pede(t, "GET", "/api/partidas/"+id+"/indicadores?participante=3"); c != 200 || len(m["indicadores"].([]any)) < 10 {
		t.Fatal(c, m)
	}
	if c, _ := a.pede(t, "GET", "/api/partidas/"+id+"/indicadores?participante=11"); c != 400 {
		t.Fatal(c)
	}
	if c, m := a.pede(t, "GET", "/api/partidas/"+id+"/saidas-de-rota"); c != 200 || m["saidas"] == nil {
		t.Fatal(c, m)
	}
	if c, m := a.pede(t, "GET", "/api/partidas/"+id+"/atividades?de=90&ate=130"); c != 200 || len(m["atividades"].([]any)) == 0 {
		t.Fatal(c, m)
	}
	if c, m := a.pede(t, "GET", "/api/partidas/"+id+"/cs?participante=3"); c != 200 || len(m["jogadores"].([]any)) != 1 {
		t.Fatal(c, m)
	}
	if c, m := a.pede(t, "GET", "/api/mapa/zonas?celula=500"); c != 200 || len(m["zonas"].([]any)) != len(Zonas) || m["grade"] == nil {
		t.Fatal(c, m)
	}
	if c, _ := a.pede(t, "GET", "/api/mapa/zonas?celula=5"); c != 400 {
		t.Fatal(c)
	}
	if c, m := a.pede(t, "GET", "/api/indicadores"); c != 200 || len(m["indicadores"].([]any)) != len(Vocabulario) {
		t.Fatal(c, m)
	}
	if c, _ := a.pede(t, "POST", "/api/partidas/1/analise"); c != 404 {
		t.Fatal(c)
	}
}
