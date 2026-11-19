package rastreio

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"loltracking/server/banco"
	"loltracking/server/jogador"
	"loltracking/server/partida"
)

// bancoOrigem: partida + rastreio + jogadores, sem jogo falso (as linhas vão
// direto nas tabelas).
func bancoOrigem(t *testing.T) *banco.DB {
	t.Helper()
	db, err := banco.AbreMemoria(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, m := range []func(*banco.DB) error{jogador.Migra, partida.Migra, Migra} {
		if err := m(db); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func execSQL(t *testing.T, db *banco.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

// partidaRastreada grava os 10 participantes (puuid "p<game>-<pid>", salvo os
// pedidos) e um passe concluído por participante rastreado.
func partidaRastreada(t *testing.T, db *banco.DB, g int64, criado string, puuids map[int]string, rastreados ...int) {
	t.Helper()
	for pid := 1; pid <= 10; pid++ {
		pu := puuids[pid]
		if pu == "" {
			pu = fmt.Sprintf("p%d-%d", g, pid)
		}
		execSQL(t, db, `INSERT INTO participantes (game_id, participante_id, puuid, nome, tag, campeao) VALUES (?, ?, ?, ?, 'BR1', 'X')`,
			g, pid, pu, fmt.Sprintf("Jogador%d", pid))
	}
	execSQL(t, db, `INSERT INTO rastreios (game_id, estado, velocidade, criado_em, atualizado_em) VALUES (?, 'concluido', 2, ?, ?)`, g, criado, criado)
	for i, pid := range rastreados {
		execSQL(t, db, `INSERT INTO passes (rastreio_id, game_id, participante_id, ordem, estado) VALUES (
			(SELECT MAX(id) FROM rastreios), ?, ?, ?, 'concluido')`, g, pid, i)
	}
}

func buscado(t *testing.T, db *banco.DB, puuid, nome, em string, buscas, ordem int) {
	t.Helper()
	execSQL(t, db, `INSERT INTO jogadores (puuid, nome, tag, buscas, buscado_em, ordem, criado_em) VALUES (?, ?, 'BR1', ?, ?, ?, ?)`,
		puuid, nome, buscas, em, ordem, em)
}

func ids(l []PartidaRastreada) []int64 {
	var r []int64
	for _, p := range l {
		r = append(r, p.GameID)
	}
	return r
}

func TestFiltraRastreadasPorOrigemEJogador(t *testing.T) {
	db := bancoOrigem(t)
	// A: pesquisa do Dono (participante 7), os 10 rastreados.
	partidaRastreada(t, db, 100, "2026-11-17T10:00:00Z", map[int]string{7: "dono"}, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10)
	// B: só estudo (referência no 3), só ele rastreado.
	partidaRastreada(t, db, 200, "2026-11-17T11:00:00Z", map[int]string{3: "ref"}, 3)
	// C: das duas origens (pesquisa da Amiga no 2, estudo da referência no 9).
	partidaRastreada(t, db, 300, "2026-11-17T12:00:00Z", map[int]string{2: "amiga", 9: "ref"}, 2, 9)
	buscado(t, db, "dono", "Dono", "2026-11-17T09:00:00Z", 3, 1)
	buscado(t, db, "amiga", "Amiga", "2026-11-17T09:30:00Z", 1, 2)
	for _, o := range []struct {
		g      int64
		origem string
		pid    int
	}{{100, OrigemPesquisa, 7}, {200, OrigemEstudo, 3}, {300, OrigemPesquisa, 2}, {300, OrigemEstudo, 9}} {
		if err := MarcaOrigemDoParticipante(db, o.g, o.origem, o.pid); err != nil {
			t.Fatal(err)
		}
	}
	// Repetir não duplica.
	if err := MarcaOrigem(db, 100, OrigemPesquisa, "dono"); err != nil {
		t.Fatal(err)
	}

	l, jogs, err := FiltraRastreadas(db, FiltroRastreadas{})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(ids(l)) != "[300 100]" {
		t.Fatalf("padrão (pesquisa) deveria esconder a partida só de estudo: %v", ids(l))
	}
	if l[1].Foco == nil || l[1].Foco.ParticipanteID != 7 || l[1].Foco.RiotID != "Dono#BR1" || l[1].Rastreados != 10 {
		t.Fatalf("foco da partida do dono: %+v", l[1].Foco)
	}
	if l[0].Foco == nil || l[0].Foco.ParticipanteID != 2 || fmt.Sprint(l[0].Origens) != "[pesquisa estudo]" {
		t.Fatalf("partida das duas origens: %+v %v", l[0].Foco, l[0].Origens)
	}
	if len(jogs) != 2 || jogs[0].RiotID != "Amiga#BR1" || jogs[0].Partidas != 1 || jogs[1].Puuid != "dono" {
		t.Fatalf("jogadores do filtro: %+v", jogs)
	}

	l, _, _ = FiltraRastreadas(db, FiltroRastreadas{Puuid: "dono"})
	if fmt.Sprint(ids(l)) != "[100]" {
		t.Fatalf("filtro por jogador: %v", ids(l))
	}
	l, _, _ = FiltraRastreadas(db, FiltroRastreadas{Puuid: "ref"})
	if len(l) != 0 {
		t.Fatalf("a referência do estudo não é jogador pesquisado: %v", ids(l))
	}
	l, _, _ = FiltraRastreadas(db, FiltroRastreadas{Origem: OrigemEstudo})
	if fmt.Sprint(ids(l)) != "[300 200]" || l[0].Foco.ParticipanteID != 9 || l[1].Foco.ParticipanteID != 3 {
		t.Fatalf("estudo, do ponto de vista da referência: %v", ids(l))
	}
	l, _, _ = FiltraRastreadas(db, FiltroRastreadas{Origem: OrigemTodas})
	if fmt.Sprint(ids(l)) != "[300 200 100]" || l[0].Foco.ParticipanteID != 2 {
		t.Fatalf("todas: %v", ids(l))
	}
	l, _, _ = FiltraRastreadas(db, FiltroRastreadas{Origem: OrigemTodas, SemEstudo: true})
	if fmt.Sprint(ids(l)) != "[300 100]" || fmt.Sprint(l[0].Origens) != "[pesquisa]" {
		t.Fatalf("com o estudo desligado, todas = só pesquisa: %v %v", ids(l), l[0].Origens)
	}
}

func TestPreencheOrigens(t *testing.T) {
	db := bancoOrigem(t)
	// Tabelas de outros pacotes, só com as colunas que o preenchimento lê.
	execSQL(t, db, `CREATE TABLE aprendizado_partidas (game_id INTEGER, participante_id INTEGER)`)
	execSQL(t, db, `CREATE TABLE estudos (id INTEGER PRIMARY KEY, puuid TEXT)`)
	execSQL(t, db, `CREATE TABLE estudo_partidas (estudo_id INTEGER, game_id INTEGER)`)
	// 1: coleta por elo → estudo com a referência.
	partidaRastreada(t, db, 1, "2026-11-18T10:00:00Z", map[int]string{7: "ref"}, 7)
	execSQL(t, db, `INSERT INTO aprendizado_partidas VALUES (1, 7)`)
	// 2: estudo de jogador → estudo com o puuid do estudo.
	partidaRastreada(t, db, 2, "2026-11-18T10:00:00Z", map[int]string{4: "estudado"}, 4)
	execSQL(t, db, `INSERT INTO estudos VALUES (1, 'estudado')`)
	execSQL(t, db, `INSERT INTO estudo_partidas VALUES (1, 2)`)
	// 3: dois buscados na partida; o buscado por último ANTES do rastreio vence
	// (o outro foi buscado de novo depois do rastreio).
	partidaRastreada(t, db, 3, "2026-11-18T13:49:06Z", map[int]string{4: "kaisa", 5: "nami", 6: "depois"}, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10)
	buscado(t, db, "kaisa", "Kaisa", "2026-11-18T13:48:32Z", 1, 1)
	buscado(t, db, "nami", "Nami", "2026-11-18T13:48:38Z", 1, 2)
	buscado(t, db, "depois", "Depois", "2026-11-18T15:00:00Z", 5, 3)
	// 4: a conta do cliente está na partida: ela vence.
	partidaRastreada(t, db, 4, "2026-11-18T13:49:06Z", map[int]string{2: "conta", 8: "outro"}, 1)
	buscado(t, db, "conta", "Conta", "2026-11-17T08:00:00Z", 1, 4)
	buscado(t, db, "outro", "Outro", "2026-11-18T13:00:00Z", 1, 5)
	// 5: ninguém buscado → pesquisa sem jogador.
	partidaRastreada(t, db, 5, "2026-11-18T13:49:06Z", nil, 1)
	// 6: já tem origem: não mexe.
	partidaRastreada(t, db, 6, "2026-11-18T13:49:06Z", map[int]string{1: "conta"}, 1)
	if err := MarcaOrigem(db, 6, OrigemEstudo, "x"); err != nil {
		t.Fatal(err)
	}

	feitas, err := PreencheOrigens(db, "conta")
	if err != nil {
		t.Fatal(err)
	}
	got := map[int64]string{}
	for _, f := range feitas {
		got[f.GameID] += f.Origem + ":" + f.Puuid
	}
	want := map[int64]string{1: "estudo:ref", 2: "estudo:estudado", 3: "pesquisa:nami", 4: "pesquisa:conta", 5: "pesquisa:"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("preenchimento:\n got %v\nwant %v", got, want)
	}
	// Rodar de novo não muda nada.
	if feitas, _ = PreencheOrigens(db, "conta"); len(feitas) != 0 {
		t.Fatalf("segunda rodada mexeu em %v", feitas)
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM partidas_origem WHERE game_id = 6`).Scan(&n)
	if n != 1 {
		t.Fatal("partida com origem não deveria ganhar outra", n)
	}
}

func TestRotaRastreioGuardaPesquisado(t *testing.T) {
	a := prepara(t, nil)
	if err := jogador.Migra(a.db); err != nil {
		t.Fatal(err)
	}
	p, _ := partida.Le(a.db, partidaTeste)
	pesq := p.Participantes[3]
	caminho := "/api/partidas/" + itoa(partidaTeste) + "/rastreio"
	if code, m := a.pede(t, "POST", caminho, `{"jogadores":[1],"velocidade":8,"puuid":"nao-esta"}`); code != 400 {
		t.Fatal("puuid fora da partida deveria ser 400", code, m)
	}
	if code, m := a.pede(t, "POST", caminho, `{"jogadores":[1],"velocidade":8,"puuid":"`+pesq.Puuid+`"}`); code != 202 {
		t.Fatal(code, m)
	}
	a.fila.Espera()
	code, m := a.pede(t, "GET", "/api/partidas/rastreadas", "")
	if code != 200 || len(m["partidas"].([]any)) != 1 || len(m["jogadores"].([]any)) != 1 {
		t.Fatal(code, m)
	}
	foco := m["partidas"].([]any)[0].(map[string]any)["foco"].(map[string]any)
	if foco["puuid"] != pesq.Puuid || int(foco["participante_id"].(float64)) != pesq.ParticipanteID {
		t.Fatalf("foco %v, queria o participante %d", foco, pesq.ParticipanteID)
	}
	if code, m = a.pede(t, "GET", "/api/partidas/rastreadas?puuid=outro", ""); code != 200 || len(m["partidas"].([]any)) != 0 {
		t.Fatal(code, m)
	}
	if code, _ = a.pede(t, "GET", "/api/partidas/rastreadas?origem=qualquer", ""); code != 400 {
		t.Fatal(code)
	}
	// Com o estudo desligado, pedir as de estudo é 404.
	a.fila.d.SemEstudo = true
	rec := httptest.NewRecorder()
	a.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/partidas/rastreadas?origem=estudo", strings.NewReader("")))
	if rec.Code != 404 {
		t.Fatal(rec.Code)
	}
}
