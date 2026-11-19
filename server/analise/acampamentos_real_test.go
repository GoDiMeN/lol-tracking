//go:build real

package analise

import (
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"

	"loltracking/server/banco"
)

// TestRealTemposSelva confere a tabela de tempos da selva (tempos_selva.go)
// contra o dado de partidas reais, numa CÓPIA do banco (não abre o jogo nem
// chama o LCU): LOL_BANCO_REAL=<cópia>/lol.db e LOL_PARTIDAS (lista separada
// por vírgula; padrão as três com os 10 rastreados).
//
//   - épicos: o intervalo entre dois abates seguidos do mesmo monstro na linha
//     do tempo da Riot nunca pode ser menor que o renascimento da tabela (o
//     abate vem depois do nascimento), e o primeiro abate nunca antes do
//     primeiro nascimento;
//   - acampamentos: o primeiro CS dos caçadores (estado dos 10) e as limpezas
//     deduzidas do rastreio (perto do acampamento usando habilidade): a
//     primeira limpeza não pode vir antes do primeiro nascimento, e duas
//     limpezas do mesmo acampamento não podem estar mais perto que o
//     renascimento (quando estão, é a dedução que errou ou o tempo).
func TestRealTemposSelva(t *testing.T) {
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
	jogos := []int64{3304217746, 3305449546, 3305481832}
	if s := os.Getenv("LOL_PARTIDAS"); s != "" {
		jogos = nil
		for _, x := range strings.Split(s, ",") {
			g, _ := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
			jogos = append(jogos, g)
		}
	}
	for _, g := range jogos {
		dp, err := carrega(db, g)
		if err != nil {
			t.Errorf("partida %d: %v", g, err)
			continue
		}
		tab, coberto := TemposDaSelva(dp.pt.Patch)
		t.Logf("=== partida %d, patch %s, tabela %q (coberto %v)", g, dp.pt.Patch, tab.Patches, coberto)
		f := novoFatos(dp, dp.retratos(), nil)

		// Épicos (linha do tempo da Riot).
		ultimo := map[string]int64{}
		for _, o := range f.objs {
			tipo := tipoEpico(o.tipo, o.sub)
			if tipo == "" {
				continue
			}
			tp, ok := tab.Epicos[tipo]
			if !ok {
				continue
			}
			if u, ok := ultimo["dragao"]; ok && tp.AposAlmaMs > 0 && ultimo[tipo] == 0 {
				// O primeiro ancião: depois do dragão da alma (aqui, o último dragão).
				marca := ""
				if o.t-u < tp.AposAlmaMs {
					marca = "  <-- MENOR QUE O TEMPO APÓS A ALMA DA TABELA"
				}
				t.Logf("épico %-10s %s (%s depois do último dragão; tabela %s)%s", tipo, MinSeg(o.t), MinSeg(o.t-u), MinSeg(tp.AposAlmaMs), marca)
			} else if u, ok := ultimo[tipo]; ok {
				iv := o.t - u
				marca := ""
				if tp.VoltaMs > 0 && iv < tp.VoltaMs {
					marca = "  <-- MENOR QUE O RENASCIMENTO DA TABELA"
				}
				if iv > 0 { // vastilarvas: as três do mesmo grupo
					t.Logf("épico %-10s %s (intervalo %s, tabela %s)%s", tipo, MinSeg(o.t), MinSeg(iv), MinSeg(tp.VoltaMs), marca)
				}
			} else {
				marca := ""
				if o.t < tp.PrimeiroMs {
					marca = "  <-- ANTES DO PRIMEIRO NASCIMENTO DA TABELA"
				}
				t.Logf("épico %-10s %s (primeiro; tabela %s)%s", tipo, MinSeg(o.t), MinSeg(tp.PrimeiroMs), marca)
			}
			ultimo[tipo] = o.t
		}

		// Primeiro CS dos caçadores (estado dos 10, todos os passes).
		for _, p := range dp.pt.Participantes {
			if p.Posicao != "JUNGLE" {
				continue
			}
			es := dp.estados[p.ParticipanteID]
			for _, e := range es {
				if e.CS > 0 {
					t.Logf("caçador %d (%s, time %d): primeiro CS no estado dos 10 aos %s (CS %d)", p.ParticipanteID, p.Campeao, p.Time, MinSeg(e.TMs), e.CS)
					break
				}
			}
			for k, q := range dp.quadrosRiot {
				for _, alvo := range []int64{60000, 120000} {
					if k > alvo-2000 && k < alvo+2000 {
						t.Logf("caçador %d: CS de selva na Riot aos %s = %d", p.ParticipanteID, MinSeg(k), q[p.ParticipanteID].JungleMinionsKilled)
					}
				}
			}
		}

		// Limpezas deduzidas do rastreio.
		por := map[int][]limpeza{}
		for _, l := range f.limpezas() {
			por[l.acamp] = append(por[l.acamp], l)
		}
		ks := make([]int, 0, len(por))
		for k := range por {
			ks = append(ks, k)
		}
		sort.Ints(ks)
		menor, menores := int64(-1), 0
		for _, k := range ks {
			a := Acampamentos[k]
			tp := tab.Acampamentos[tipoAcampamento(a.Nome)]
			var b strings.Builder
			for i, l := range por[k] {
				b.WriteString(" " + MinSeg(l.t) + "(" + strconv.Itoa(l.quem) + ")")
				if i == 0 && l.t < tp.PrimeiroMs {
					b.WriteString("<ANTES-DO-NASCIMENTO")
				}
				if i > 0 {
					iv := l.t - por[k][i-1].t
					if iv < tp.VoltaMs {
						b.WriteString("<" + MinSeg(iv))
						menores++
					}
					if menor < 0 || iv < menor {
						menor = iv
					}
				}
			}
			t.Logf("limpezas %-26s nasce %s volta %s:%s", a.Nome, MinSeg(tp.PrimeiroMs), MinSeg(tp.VoltaMs), b.String())
		}
		t.Logf("partida %d: %d intervalo(s) entre limpezas menor(es) que o renascimento; menor intervalo %s", g, menores, MinSeg(menor))

		// Só o caçador dono do lado, nos acampamentos do lado dele: a série mais
		// limpa (quem passa pela selva usando habilidade também vira "limpeza").
		// Dentro de 20 s é a mesma visita (a limpeza é carimbada no último uso).
		for _, k := range ks {
			a := Acampamentos[k]
			lado := ladoAcampamento(a.Nome)
			tp := tab.Acampamentos[tipoAcampamento(a.Nome)]
			var dono int
			for _, p := range dp.pt.Participantes {
				if p.Posicao == "JUNGLE" && p.Time == lado {
					dono = p.ParticipanteID
				}
			}
			if dono == 0 {
				continue
			}
			var ts []int64
			for _, l := range por[k] {
				if l.quem == dono {
					ts = append(ts, l.t)
				}
			}
			var b strings.Builder
			curtos := 0
			for i := 1; i < len(ts); i++ {
				iv := ts[i] - ts[i-1]
				b.WriteString(" " + MinSeg(iv))
				if iv < tp.VoltaMs-20000 {
					b.WriteString("!")
					curtos++
				}
			}
			t.Logf("dono %2d %-26s volta %s: %d visitas, intervalos%s (%d curto(s) demais)", dono, a.Nome, MinSeg(tp.VoltaMs), len(ts), b.String(), curtos)
		}
	}
}
