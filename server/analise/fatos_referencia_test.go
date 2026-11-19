package analise

import (
	"errors"
	"testing"

	"loltracking/server/banco"
)

// Uma partida da coleta (elo PLATINUM) com UM jogador rastreado (o caçador
// escolhido, 7) e a partida do dono (sem elo, os 10 rastreados): a referência
// é só o 7 da coleta, com os números da linha dele e as etiquetas das mortes.
func TestReferenciasSoOEscolhidoDaColeta(t *testing.T) {
	db, err := banco.AbreMemoria(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Migra(db); err != nil {
		t.Fatal(err)
	}
	coleta := ResumoPartida{GameID: 1001, Versao: VersaoFatos, CriadaEm: "2026-11-18", Patch: "16.23", DuracaoS: 1800, Vencedor: 200, Elo: "PLATINUM",
		Jogadores: []ResumoJogador{
			{ParticipanteID: 2, Puuid: "b", Campeao: "Lee Sin", Funcao: "JUNGLE", Time: 100},
			{ParticipanteID: 7, Puuid: "k", Campeao: "Kayn", Funcao: "JUNGLE", Time: 200, Venceu: true, Rastreado: true, Elo: "PLATINUM II",
				Abates: 19, Mortes: 7, Assistencias: 12, CS10: ip(71), Sentinelas: 88, ObjetivosParticipou: 9, ObjetivosTime: 16,
				Etiquetas: map[string]map[string]int{"rotas": {"farmando": 265, "invadindo": 229}}},
			{ParticipanteID: 6, Puuid: "g", Campeao: "Gnar", Funcao: "TOP", Time: 200, Venceu: true},
		},
		Momentos: []MomentoChave{
			{N: 1, TMs: 253000, Tipo: "abate", ParticipanteID: 7, Tags: []string{"sozinho", "inferioridade", "sem_troca"}},
			{N: 2, TMs: 1353000, Tipo: "abate", ParticipanteID: 7, Tags: []string{"antes_de_objetivo"}},
			{N: 3, TMs: 280000, Tipo: "janela_aproveitada", ParticipanteID: 7},
			{N: 4, TMs: 937000, Tipo: "janela_perdida", ParticipanteID: 7},
			{N: 5, TMs: 400000, Tipo: "abate", ParticipanteID: 2, Tags: []string{"sozinho"}},
		}}
	dono := ResumoPartida{GameID: 2002, Versao: VersaoFatos, CriadaEm: "2026-11-17", DuracaoS: 1711, Vencedor: 100,
		Jogadores: []ResumoJogador{{ParticipanteID: 7, Puuid: "eu", Campeao: "Vi", Funcao: "JUNGLE", Time: 200, Rastreado: true}}}
	for _, r := range []ResumoPartida{coleta, dono} {
		if err := gravaFatos(db, r); err != nil {
			t.Fatal(err)
		}
	}
	refs, err := Referencias(db, FiltroReferencia{Elo: "platinum", Funcao: "jungle", ExcluiGame: 2002, ExcluiPuuid: "eu"})
	if err != nil || len(refs) != 1 {
		t.Fatalf("%v %+v", err, refs)
	}
	k := refs[0]
	if k.Campeao != "Kayn" || k.Elo != "PLATINUM II" || k.EloPartida != "PLATINUM" || k.Abates != 19 || *k.CS10 != 71 ||
		k.MortesSozinho != 1 || k.MortesInferioridade != 1 || k.MortesSemTroca != 1 || k.MortesAntesObjetivo != 1 ||
		k.JanelasAproveitadas != 1 || k.JanelasPerdidas != 1 || k.Etiquetas["rotas"]["invadindo"] != 229 || k.DuracaoS != 1800 {
		t.Fatalf("Kayn: %+v", k)
	}
	// Outra função, outro campeão, outro elo, só derrotas: ninguém.
	for _, f := range []FiltroReferencia{{Elo: "PLATINUM", Funcao: "TOP"}, {Campeao: "Vi"}, {Elo: "EMERALD"}, {Elo: "PLATINUM", Resultado: "derrota"}} {
		if r, err := Referencias(db, f); err != nil || len(r) != 0 {
			t.Fatalf("%+v: %v %+v", f, err, r)
		}
	}
	// A partida do dono (sem elo) nunca é referência, nem sem filtro.
	if r, _ := Referencias(db, FiltroReferencia{}); len(r) != 1 {
		t.Fatalf("sem filtro: %+v", r)
	}
	eu, err := IndicadoresDe(db, 2002, 7)
	if err != nil || eu.Campeao != "Vi" || eu.EloPartida != "" {
		t.Fatalf("%v %+v", err, eu)
	}
	if _, err := IndicadoresDe(db, 9, 1); !errors.Is(err, ErrSemResumo) {
		t.Fatalf("sem resumo: %v", err)
	}
}
