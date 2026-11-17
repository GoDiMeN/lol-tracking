//go:build real

package analise

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"loltracking/server/banco"
	"loltracking/server/rastreio"
	"loltracking/server/riot"
)

// TestRealAnalise roda a análise sobre um banco com passes reais (não abre o
// jogo): LOL_BANCO_REAL=dados/analise-real/lol.db, partida LOL_PARTIDA.
// Imprime o que foi deduzido para conferir a olho.
func TestRealAnalise(t *testing.T) {
	arq := os.Getenv("LOL_BANCO_REAL")
	if arq == "" {
		t.Skip("LOL_BANCO_REAL não definido")
	}
	db, err := banco.Abre(arq)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var game int64 = 3304594274
	ps, err := db.Query(`SELECT id, participante_id FROM passes WHERE game_id = ? AND estado = 'concluido'`, game)
	if err != nil {
		t.Fatal(err)
	}
	type par struct {
		id  int64
		pid int
	}
	var lista []par
	for ps.Next() {
		var p par
		ps.Scan(&p.id, &p.pid)
		lista = append(lista, p)
	}
	ps.Close()
	for _, p := range lista {
		n, err := rastreio.RegeraCadeia(db, p.id)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("passe %d (participante %d): %d itens", p.id, p.pid, n)
		rc, err := rastreio.ListaCadeia(db, game, p.pid, map[string]bool{"saiu_da_rota": true, "voltou_para_rota": true,
			"entrou_em_rota": true, "na_base": true, "morte": true, "abate": true, "mudou_zona": true, "atividade": os.Getenv("LOL_ATV") == "1"})
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range rc.Itens {
			d := it.Dados
			switch it.Tipo {
			case "mudou_zona":
				t.Logf("  %s %-14s %v → %v (%v)", MinSeg(it.TMs), it.Tipo, d["de"], d["para"], d["subzona"])
			case "atividade":
				t.Logf("  %s %-14s %v %v com=%v %s conf=%v %v", MinSeg(it.TMs), it.Tipo, d["atividade"], MinSeg(int64(d["duracao_ms"].(float64))), d["com"], d["zona"], d["confianca"], d["sinais"])
			default:
				b, _ := json.Marshal(d)
				t.Logf("  %s %-14s %s", MinSeg(it.TMs), it.Tipo, b)
			}
		}
	}
}

// TestRealRecalcula roda Recalcula (com a chave da Riot de token.txt, se
// válida) e imprime retratos, indicadores e o CS para conferir.
func TestRealRecalcula(t *testing.T) {
	arq := os.Getenv("LOL_BANCO_REAL")
	if arq == "" {
		t.Skip("LOL_BANCO_REAL não definido")
	}
	db, err := banco.Abre(arq)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Migra(db); err != nil {
		t.Fatal(err)
	}
	var game int64 = 3304594274
	d := Deps{DB: db, Riot: riot.Novo("../../token.txt", false)}
	res, err := Recalcula(context.Background(), d, game)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(res)
	t.Logf("resumo: %s", b)
	rs, _ := ListaRetratos(db, game)
	for _, r := range rs {
		if r.TMs > 16*60000 {
			break
		}
		t.Logf("abate %d %s: %d mata %d (assist %v) em %s/%s; perto da vítima: %d aliados × %d inimigos; aliado mais perto %v; aprox %v",
			r.N, MinSeg(r.TMs), r.Matador, r.Vitima, r.Assistencias, r.Zona, r.Subzona, r.AliadosDaVitima, r.InimigosDaVitima, ptr(r.AliadoMaisProximo), r.Aproximado)
		for _, j := range r.Jogadores {
			t.Logf("    %2d %-11s %-14s (%5.0f,%5.0f) %-20s %-13s vivo=%v vida%%=%v vida=%v/%v R=%v F=%v fora=%v aliados=%d inimigos=%d dist=%v",
				j.ParticipanteID, j.Papel, j.Origem, ptr(j.X), ptr(j.Y), j.Zona, j.Subzona, bptr(j.Vivo), ptr(j.VidaPct), ptr(j.Vida), ptr(j.VidaMax),
				bptr(j.RDisponivel), bptr(j.FlashDisponivel), iptr(j.ForaDaRotaMs), j.AliadosPerto, j.InimigosPerto, ptr(j.DistVitima))
		}
		if r.Riot != nil {
			for _, x := range r.Riot.Recebido {
				t.Logf("    riot: %d %s %s (slot %d) %d", x.ParticipanteID, x.Nome, x.Habilidade, x.Slot, x.Total)
			}
		}
	}
	for _, pid := range []int{3, 8} {
		inds, _ := ListaIndicadores(db, game, pid)
		for _, in := range inds {
			if strings.HasPrefix(in.Indicador.Indicador, "pct_") && (in.Valor == nil || *in.Valor == 0) {
				continue
			}
			m := ""
			if ms, ok := in.Detalhe["momentos"].([]any); ok && len(ms) > 0 {
				mb, _ := json.Marshal(ms)
				m = string(mb)
			}
			t.Logf("  %d %-34s %v aprox=%v %s", pid, in.Indicador.Indicador, ptr(in.Valor), in.Aproximado, m)
		}
	}
	cs, _ := ConfereCS(db, game, 0)
	for _, c := range cs {
		var s []string
		for _, m := range c.Minutos {
			s = append(s, fmt.Sprintf("%d:%d/%d", m.Minuto, *m.CSJogo, m.CSLCU))
		}
		t.Logf("cs %d divergências %d: %s", c.ParticipanteID, c.Divergencias, strings.Join(s, " "))
	}
}

func ptr(v *float64) any {
	if v == nil {
		return "—"
	}
	return *v
}

func bptr(v *bool) any {
	if v == nil {
		return "—"
	}
	return *v
}

func iptr(v *int64) any {
	if v == nil {
		return "—"
	}
	return MinSeg(*v)
}
