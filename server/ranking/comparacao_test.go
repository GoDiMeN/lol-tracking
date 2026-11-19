package ranking

import (
	"context"
	"testing"

	"loltracking/server/banco"
)

// gravaDeTeste grava o ranking de selva de um patch com os jogadores dados.
func gravaDeTeste(t *testing.T, db *banco.DB, patch string, js []Jogador) Ranking {
	t.Helper()
	at := Atualizacao{Regiao: "BR1", Patch: patch, Elo: "GOLD", Fonte: FonteRiot, TetoCandidatos: 60, TetoPartidas: 20}
	id, err := criaAtualizacao(db, at)
	if err != nil {
		t.Fatal(err)
	}
	at.ID = id
	r := Recorte{Tipo: TipoRota, Rota: "JUNGLE"}
	rc := rankingCalculado{recorte: r, posicoes: map[string][]Posicao{}, fora: map[string]int{}}
	for _, m := range Modos {
		rc.posicoes[m], _ = Calcula(m, r, js, Contexto{}, "selva")
		rc.amostra = len(rc.posicoes[m])
	}
	if err := gravaRankings(db, at, len(js), len(js), []rankingCalculado{rc}); err != nil {
		t.Fatal(err)
	}
	rk, err := Ultimo(db, "BR1", patch, "GOLD", "JUNGLE", 0)
	if err != nil {
		t.Fatal(err)
	}
	return rk
}

func jog(puuid string, lp, vit, camp int) Jogador {
	ps := partidas("JUNGLE", 10, vit, "", 0)
	for i := range ps {
		ps[i].CampeaoID = camp
	}
	return Jogador{Entrada: Entrada{Puuid: puuid, Divisao: "I", LP: lp, Vitorias: 60, Derrotas: 40}, Nome: puuid, Partidas: ps}
}

func TestComparaDoisPatches(t *testing.T) {
	db, err := banco.AbreMemoria(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Migra(db); err != nil {
		t.Fatal(err)
	}
	a := gravaDeTeste(t, db, "16.21", []Jogador{jog("a", 90, 9, 64), jog("b", 80, 8, 64), jog("c", 70, 7, 11)})
	b := gravaDeTeste(t, db, "16.22", []Jogador{jog("b", 90, 9, 11), jog("d", 85, 8, 11), jog("a", 10, 3, 64)})
	c, err := Compara(context.Background(), db, a, b, ModoMelhor, 2, func(_ context.Context, id int) string {
		return map[int]string{64: "Lee Sin", 11: "Master Yi"}[id]
	})
	if err != nil {
		t.Fatal(err)
	}
	// Topo 2: A = a, b; B = b, d. Entrou d, saiu a, ficou b (2º → 1º).
	if len(c.Entraram) != 1 || c.Entraram[0].Puuid != "d" || len(c.Sairam) != 1 || c.Sairam[0].Puuid != "a" ||
		len(c.Ficaram) != 1 || c.Ficaram[0].PosicaoA != 2 || c.Ficaram[0].PosicaoB != 1 {
		t.Fatalf("%+v", c)
	}
	// Presença no topo: Lee Sin 2 → 0, Master Yi 0 → 2.
	pres := map[string]Presenca{}
	for _, p := range c.Presenca {
		pres[p.Nome] = p
	}
	if pres["Lee Sin"].A != 2 || pres["Lee Sin"].B != 0 || pres["Master Yi"].B != 2 || c.PresencaDe != "campeao" {
		t.Fatalf("%+v", c.Presenca)
	}
	if len(c.Avisos) < 2 {
		t.Fatal(c.Avisos)
	}
	ps, _ := Patches(db, "BR1")
	if len(ps) != 2 || ps[0] != "16.22" {
		t.Fatal(ps)
	}
}
