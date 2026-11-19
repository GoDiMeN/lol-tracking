package analise

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"loltracking/server/banco"
	"loltracking/server/campeao"
	"loltracking/server/lcu"
	"loltracking/server/partida"
)

// O contexto junta o banco e o histórico buscado: sem repetir partida, só
// Summoner's Rift, nunca partida de depois, e diz a fonte e o alcance.
func TestContextoJuntaBancoEHistoricoBuscado(t *testing.T) {
	db, err := banco.AbreMemoria(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := partida.Migra(db); err != nil {
		t.Fatal(err)
	}
	if err := Migra(db); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	l := lcu.NovoFalso()
	bruto, _ := l.Historico(ctx, lcu.FalsoLogado.Puuid, 0, 100)
	var h struct {
		Games struct {
			Games []struct {
				GameID       int64 `json:"gameId"`
				GameCreation int64 `json:"gameCreation"`
				Participants []struct {
					ParticipantID int `json:"participantId"`
					ChampionID    int `json:"championId"`
				} `json:"participants"`
			} `json:"games"`
		} `json:"games"`
	}
	json.Unmarshal(bruto, &h)
	jogos := h.Games.Games
	// A partida em foco é a 3 (0 = a mais nova); no banco, também as 4 a 8
	// (a 4 é ARAM: não conta).
	for i := 3; i <= 8; i++ {
		inteira, _ := l.Partida(ctx, jogos[i].GameID)
		if err := partida.Grava(ctx, partida.Deps{DB: db, LCU: l, Campeoes: campeao.Fixo(lcu.CampeoesFalsos)}, inteira, true); err != nil {
			t.Fatal(err)
		}
	}
	alvo := jogos[3]
	pid := alvo.Participants[0].ParticipantID
	pt, _ := partida.Le(db, alvo.GameID)

	// Só o banco: 4 partidas de Summoner's Rift antes (abaixo do mínimo).
	c := contextoDoJogador(db, alvo.GameID, pt.Participantes[pid-1], pt.CriadaEmMs)
	if c.Fonte != "banco" || c.PartidasGuardadas != 4 || c.FuncaoPrincipal != "" || c.Mono != nil || len(c.Falta) == 0 {
		t.Fatalf("%+v", c)
	}

	// O histórico buscado: repete a 5 (já no banco), traz uma partida de DEPOIS
	// (ignorada) e mais três de antes.
	ext := HistoricoExterno{ParticipanteID: pid, Fonte: "cliente", Estado: HistParcial, Vistas: 20, Jogos: []JogoExterno{
		{GameID: jogos[5].GameID, CriadaMs: jogos[5].GameCreation, Fila: 420, Posicao: "TOP", CampeaoID: 54, Venceu: true},
		{GameID: jogos[1].GameID, CriadaMs: jogos[1].GameCreation, Fila: 420, Posicao: "TOP", CampeaoID: 54, Venceu: true},
		{GameID: 1, CriadaMs: jogos[8].GameCreation - 1000, Fila: 420, Posicao: "MIDDLE", CampeaoID: alvo.Participants[0].ChampionID},
		{GameID: 2, CriadaMs: jogos[8].GameCreation - 2000, Fila: 440, Posicao: "MIDDLE", CampeaoID: alvo.Participants[0].ChampionID},
		{GameID: 3, CriadaMs: jogos[8].GameCreation - 3000, Fila: 450, Posicao: "", CampeaoID: 1},
	}}
	if err := GravaHistoricoExterno(db, alvo.GameID, ext); err != nil {
		t.Fatal(err)
	}
	c = contextoDoJogador(db, alvo.GameID, pt.Participantes[pid-1], pt.CriadaEmMs)
	// 4 do banco + 2 novas de Summoner's Rift (a de depois, a repetida e a ARAM fora).
	if c.PartidasGuardadas != 6 || c.Fonte != "cliente + banco" || c.BuscadoEm == "" || c.CampeaoDe != 6 || c.Mono == nil {
		t.Fatalf("%+v", c)
	}
	if !strings.Contains(c.Alcance, "só 2 partida(s) de antes") {
		t.Fatalf("alcance: %q", c.Alcance)
	}
	if txt := textoContexto(c, pt.Participantes[pid-1].Posicao); !strings.Contains(txt, "6 partida(s) de antes desta (cliente + banco)") ||
		!strings.Contains(txt, "com este campeão") {
		t.Fatalf("texto: %s", txt)
	}
	// O que a tela lê: a mesma conta para os 10.
	todos, err := ContextoDosJogadores(db, alvo.GameID)
	if err != nil || len(todos) != 10 || todos[pid].PartidasGuardadas != 6 {
		t.Fatal(len(todos), err)
	}
}
