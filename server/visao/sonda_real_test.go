//go:build real

package visao

// Sonda da leitura da tela com o jogo de verdade (cliente aberto e logado,
// partida 3304594274 baixada). Cada rastreio ABRE E FECHA o replay. Banco em
// dados/sonda-tela.db; capturas em dados/capturas (a cada 5 s de jogo).
//
//	cd server && go test -tags real -run TestRealTela -count=1 -v -timeout 40m ./visao/

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"loltracking/server/banco"
	"loltracking/server/campeao"
	"loltracking/server/lcu"
	"loltracking/server/partida"
	"loltracking/server/rastreio"
	"loltracking/server/replay"
	"loltracking/server/riot"
	"loltracking/server/web"
)

const partidaReal = 3304594274

type sonda struct {
	db   *banco.DB
	fila *rastreio.Fila
	hub  *web.Hub
}

func preparaSonda(t *testing.T) *sonda {
	lock := `C:\Riot Games\League of Legends\lockfile`
	if _, err := os.Stat(lock); err != nil {
		t.Skip("cliente do LoL fechado")
	}
	if prazo, ok := t.Deadline(); ok && time.Until(prazo) < 20*time.Minute {
		t.Skip("rode com -timeout 40m (um timeout no meio deixaria o replay aberto)")
	}
	db, err := banco.Abre(filepath.Join("..", "dados", "sonda-tela.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	partida.Migra(db)
	rastreio.Migra(db)
	cli := lcu.NovoReal(lock)
	ctx := context.Background()
	bruto, err := cli.Partida(ctx, partidaReal)
	if err != nil {
		t.Fatal(err)
	}
	if err := partida.Grava(ctx, partida.Deps{DB: db, LCU: cli, Campeoes: campeao.NovoDataDragon(filepath.Join("..", "dados"))}, bruto, true); err != nil {
		t.Fatal(err)
	}
	tl, err := cli.LinhaDoTempo(ctx, partidaReal)
	if err != nil {
		t.Fatal(err)
	}
	partida.GravaLinhaDoTempo(db, partidaReal, tl)
	leitor, err := Novo()
	if err != nil {
		t.Fatal(err)
	}
	j := replay.NovaJanela()
	hub := web.NovoHub()
	cfg := rastreio.Config{Leitor: leitor, Riot: riot.Novo(filepath.Join("..", "..", "token.txt"), false),
		PastaCapturas: filepath.Join("..", "dados", "capturas"), CapturaACadaS: 5}
	f := rastreio.NovaFila(rastreio.Deps{DB: db, LCU: cli, Hub: hub, Config: cfg, Replay: replay.Deps{LCU: cli,
		API: replay.NovaAPI(replay.EnderecoPadrao), Jogo: j, Teclado: j, Tela: j, GameCfg: replay.GameCfgDe(lock)}})
	return &sonda{db: db, fila: f, hub: hub}
}

func ms(v int64) string { return fmt.Sprintf("%d:%02d.%d", v/60000, v/1000%60, v%1000/100) }

func (s *sonda) roda(t *testing.T, ped rastreio.Pedido) {
	inicio := time.Now()
	r, exID, err := s.fila.Cria(context.Background(), partidaReal, ped)
	if err != nil {
		t.Fatal(err)
	}
	s.fila.Espera()
	ex, _ := s.hub.Busca(exID)
	r, _ = rastreio.LeRastreio(s.db, r.ID)
	t.Logf("rastreio %d (%v, %.0fx, %s→%s): %s %q em %s", r.ID, ped.Jogadores, r.Velocidade, ms(ped.TInicioMs), ms(ped.TFimMs),
		r.Estado, r.Erro, time.Since(inicio).Round(time.Second))
	for _, e := range ex.Eventos() {
		switch e.Tipo {
		case "passe_fim", "erro", "aviso", "aviso_tela", "conferencia_riot":
			t.Logf("  %s %s", e.Tipo, e.Dados)
		}
	}
	confereFechado(t)
	linhas, _ := s.db.Query(`SELECT id FROM passes WHERE rastreio_id = ? ORDER BY ordem`, r.ID)
	var ids []int64
	for linhas.Next() {
		var id int64
		linhas.Scan(&id)
		ids = append(ids, id)
	}
	linhas.Close()
	for _, id := range ids {
		p, err := rastreio.LePasse(s.db, id)
		if err != nil {
			t.Fatal(err)
		}
		s.relata(t, p)
	}
}

// relata: % de amostras com cada campo lido, conferência, usos de habilidade.
func (s *sonda) relata(t *testing.T, p rastreio.Passe) {
	pg, err := rastreio.ListaAmostras(s.db, partidaReal, p.ParticipanteID, 0, math.MaxInt64/2, 0, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	as := pg.Amostras
	taxa := 0.0
	if p.Taxa != nil {
		taxa = *p.Taxa
	}
	t.Logf("  passe %d (participante %d): %d amostras, taxa %.2f por s de jogo", p.ID, p.ParticipanteID, len(as), taxa)
	vivas := 0
	cont := map[string]int{}
	for _, a := range as {
		if a.Morto || !a.SelecaoOK {
			continue
		}
		vivas++
		campos := map[string]*float64{"vida": a.Vida, "vida_max": a.VidaMax, "mana": a.Mana, "mana_max": a.ManaMax,
			"cd_q": a.CD.Q, "cd_w": a.CD.W, "cd_e": a.CD.E, "cd_r": a.CD.R, "cd_d": a.CD.D, "cd_f": a.CD.F,
			"atr_ad": a.Atr.AD, "atr_ap": a.Atr.AP, "atr_armadura": a.Atr.Armadura, "atr_rm": a.Atr.RM,
			"atr_vel_ataque": a.Atr.VelAtaque, "atr_vel_mov": a.Atr.VelMov}
		for n, v := range campos {
			if v != nil {
				cont[n]++
			}
		}
	}
	var nomes []string
	for n := range cont {
		nomes = append(nomes, n)
	}
	sort.Strings(nomes)
	var b strings.Builder
	for _, n := range nomes {
		fmt.Fprintf(&b, " %s=%.1f%%", n, 100*float64(cont[n])/float64(max(1, vivas)))
	}
	t.Logf("  com a câmera nele e vivo: %d amostras; lidos:%s", vivas, b.String())
	// Estado dos 10: quantos com vida_pct.
	qs, _ := rastreio.ListaEstados(s.db, partidaReal, as[0].TMs, as[len(as)-1].TMs, 1000)
	comPct, tot := 0, 0
	for _, q := range qs {
		for _, j := range q.Jogadores {
			tot++
			if j.VidaPct != nil {
				comPct++
			}
		}
	}
	t.Logf("  quadros laterais: vida_pct em %d de %d (%.1f%%)", comPct, tot, 100*float64(comPct)/float64(max(1, tot)))
	if p.ConferenciaN != nil {
		t.Logf("  conferência Riot: %d comparados, %d iguais, maior diferença %.2f: %s", *p.ConferenciaN, *p.ConferenciaIguais,
			*p.ConferenciaMaiorDif, string(p.Conferencia))
	} else {
		t.Logf("  conferência Riot: não feita")
	}
	rc, _ := rastreio.ListaCadeia(s.db, partidaReal, p.ParticipanteID, map[string]bool{"habilidade_usada": true,
		"vida_baixa": true, "recuperou": true, "morte": true, "renascimento": true})
	for _, it := range rc.Itens {
		d, _ := json.Marshal(it.Dados)
		t.Logf("    %s %s %s", ms(it.TMs), it.Tipo, d)
	}
}

func confereFechado(t *testing.T) {
	out, _ := exec.Command("tasklist", "/FO", "CSV", "/NH").Output()
	jogo, cliente := false, false
	for _, l := range strings.Split(string(out), "\n") {
		jogo = jogo || strings.HasPrefix(l, `"League of Legends.exe"`)
		cliente = cliente || strings.HasPrefix(l, `"LeagueClientUx.exe"`)
	}
	t.Logf("  processos: League of Legends.exe=%v LeagueClientUx.exe=%v", jogo, cliente)
	if jogo {
		t.Errorf("o jogo ficou aberto")
	}
	if !cliente {
		t.Errorf("o cliente do LoL não está mais aberto")
	}
}

func TestRealTela(t *testing.T) {
	s := preparaSonda(t)
	// Akshan (3), min 6 a 10 a 2×: luta e morte em 8:48.
	s.roda(t, rastreio.Pedido{Jogadores: []int{3}, Velocidade: 2, TInicioMs: 6 * 60000, TFimMs: 10 * 60000})
	// Akshan a 8×, 1 min.
	s.roda(t, rastreio.Pedido{Jogadores: []int{3}, Velocidade: 8, TInicioMs: 16 * 60000, TFimMs: 17 * 60000})
	// Outros campeões, 30 s cada a 2×: Master Yi (2), Teemo (6), Warwick (7).
	s.roda(t, rastreio.Pedido{Jogadores: []int{2, 6, 7}, Velocidade: 2, TInicioMs: 13 * 60000, TFimMs: 13*60000 + 30000})
}
