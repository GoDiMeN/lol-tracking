//go:build real

package analise

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"loltracking/server/banco"
)

// TestRealOndas imprime as ondas deduzidas da partida 3304217746 num banco com os
// passes reais (uma CÓPIA; não abre o jogo): LOL_BANCO_REAL=<cópia>/lol.db.
func TestRealOndas(t *testing.T) {
	arq := os.Getenv("LOL_BANCO_REAL")
	if arq == "" {
		t.Skip()
	}
	db, err := banco.Abre(arq)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	od, err := LeOndas(db, 3304217746)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("saída %s medida=%v fim rotas %s até %s", MinSeg(od.SaidaTropasMs), od.SaidaMedida, MinSeg(od.FimFaseRotasMs), MinSeg(od.AteMs))
	for _, o := range od.Ondas[:4] {
		t.Logf("onda %d saída %s canhão %v chegada topo %s meio %s baixo %s", o.N, MinSeg(o.SaidaMs), o.Canhao,
			relogioDec(o.Chegada[ZRotaTopo]), relogioDec(o.Chegada[ZRotaMeio]), relogioDec(o.Chegada[ZRotaBaixo]))
	}
	for _, r := range od.Rotas {
		t.Logf("== %s rastreados %v encontro %.0f torres %v", r.Rota, r.Rastreados, r.Eixo.Encontro, r.Eixo.Torres)
		for _, j := range r.Janelas {
			s, av := "  —  ", "  —  "
			if j.S != nil {
				s = fmtF(*j.S)
			}
			if j.Avanco != nil {
				av = fmtF(*j.Avanco)
			}
			t.Logf("  onda %2d %s–%s s=%s av=%s %-5s %-14s time=%d lados=%v %v", j.Onda, MinSeg(j.DeMs), MinSeg(j.AteMs), s, av, j.Confianca, j.Estado, j.Time, j.lados, j.Sinais)
		}
		for _, e := range r.Estados {
			t.Logf("  TRECHO %s–%s %-14s time=%d vel=%s conf=%s s=%.0f ondas=%v", MinSeg(e.DeMs), MinSeg(e.AteMs), e.Estado, e.Time, e.Velocidade, e.Confianca, e.S, e.Ondas)
		}
		for _, p := range r.Perdidas {
			t.Logf("  PERDIDA %s pid %d onda %d %s %s dist %.0f (%s)", MinSeg(p.TMs), p.ParticipanteID, p.Onda, p.Motivo, p.Zona, p.Distancia, p.PosicaoOnda)
		}
	}
}

func fmtF(v float64) string { return fmt.Sprintf("%5.0f", v) }

// TestRealGeraCasoTopo refaz testdata/ondas_topo_3304217746.csv (o caso real
// pequeno de TestOndasCasoRealTopo) a partir do banco real.
func TestRealGeraCasoTopo(t *testing.T) {
	db, err := banco.Abre(os.Getenv("LOL_BANCO_REAL"))
	if err != nil {
		t.Skip()
	}
	defer db.Close()
	dp, _ := carrega(db, 3304217746)
	var b strings.Builder
	b.WriteString("# partida 3304217746, rota do topo: Ambessa (1, azul) e Garen (6, vermelho), 1 amostra/s de 0:30 a 11:00\n")
	b.WriteString("# cs: minuto,cs1,cs6 | p: t_ms,pid,x,y,morto | k: t_ms,x,y (abates perto do topo)\n")
	for _, q := range dp.lt.Quadros {
		if q.TMs > 12*60000 {
			break
		}
		fmt.Fprintf(&b, "cs,%d,%d,%d\n", q.TMs, q.Jogadores[1].CS, q.Jogadores[6].CS)
	}
	for _, e := range dp.lt.Eventos {
		if e.Tipo == "CHAMPION_KILL" && e.X != nil && e.TMs < 11*60000 && *e.X < 6000 && *e.Y > 8000 {
			fmt.Fprintf(&b, "k,%d,%.0f,%.0f\n", e.TMs, *e.X, *e.Y)
		}
	}
	for tm := int64(30000); tm <= 660000; tm += 1000 {
		for _, pid := range []int{1, 6} {
			a := AmostraEm(dp.amostras[pid], tm, 600)
			if a == nil {
				continue
			}
			if a.Morto || a.X == nil {
				fmt.Fprintf(&b, "p,%d,%d,,,1\n", tm, pid)
				continue
			}
			fmt.Fprintf(&b, "p,%d,%d,%.0f,%.0f,0\n", tm, pid, *a.X, *a.Y)
		}
	}
	os.WriteFile("testdata/ondas_topo_3304217746.csv", []byte(b.String()), 0o644)
}
