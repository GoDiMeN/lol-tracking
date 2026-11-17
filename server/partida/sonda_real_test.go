//go:build real

package partida

// Sonda da lista de partidas contra o cliente do LoL de verdade. Não baixa nem
// abre replay: só leitura e, para partidas do patch atual que o plugin ainda não
// conhecia, o create do metadata (rota permitida).
//
//	cd server && go test -tags real -run TestReal -count=1 -v ./partida/

import (
	"context"
	"os"
	"testing"
	"time"

	"loltracking/server/banco"
	"loltracking/server/campeao"
	"loltracking/server/lcu"
)

func TestRealListaDoLogado(t *testing.T) {
	lock := os.Getenv("LOL_LOCKFILE")
	if lock == "" {
		lock = `C:\Riot Games\League of Legends\lockfile`
	}
	if _, err := os.Stat(lock); err != nil {
		t.Skip("cliente do LoL fechado")
	}
	db, err := banco.AbreMemoria(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	Migra(db)
	cli := lcu.NovoReal(lock)
	d := Deps{DB: db, LCU: cli, Campeoes: campeao.NovoDataDragon(t.TempDir())}
	ctx, cancela := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancela()
	eu, err := cli.InvocadorAtual(ctx)
	if err != nil {
		t.Fatal(err)
	}
	pag, err := ListaDoJogador(ctx, d, eu.Puuid, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d partidas, tem_mais=%v", len(pag.Partidas), pag.TemMais)
	for _, p := range pag.Partidas {
		t.Logf("%d %s %-12s %-8s %s %s %s/%d/%d replay=%s(%s)", p.GameID, p.Patch, p.Campeao, p.Posicao, map[bool]string{true: "V", false: "D"}[p.Venceu],
			p.FilaNome, kdaTexto(p), p.Mortes, p.Assistencias, p.Replay.Estado, p.Replay.EstadoLCU)
		if p.GameID == 3304594274 && p.Replay.Estado != Baixado {
			t.Errorf("3304594274 deveria estar baixado: %+v", p.Replay)
		}
		if lcu.PatchDe(p.Versao) != "16.22" && p.Replay.Estado != Incompativel {
			t.Errorf("%d (%s) deveria ser incompatível: %+v", p.GameID, p.Versao, p.Replay)
		}
		if !p.Completa {
			t.Errorf("%d não ficou completa", p.GameID)
		}
	}
	// A partida de outro jogador que já foi baixada nesta máquina.
	if err := garantePartida(ctx, d, 3304553006); err != nil {
		t.Fatal(err)
	}
	r, err := ConsultaReplay(ctx, d, 3304553006, "16.22.834.8910")
	if err != nil || r.Estado != Baixado {
		t.Errorf("3304553006: %+v %v", r, err)
	}
}

func kdaTexto(p Resumo) string { return itoa(int64(p.Abates)) }
