package visao

import (
	"context"
	"testing"

	"loltracking/server/banco"
	"loltracking/server/campeao"
	"loltracking/server/lcu"
	"loltracking/server/partida"
	"loltracking/server/rastreio"
	"loltracking/server/replay"
	"loltracking/server/web"
)

// O passe de visão de ponta a ponta, sem o jogo: o jogo falso desenha o painel
// e o minimapa de cada visão (anéis na cor do time, sentinelas, geometria
// medida), ESTA leitura lê, e o rastreio decide e grava. A verdade vem do
// próprio falso (VisivelPara: um time vê quem está a menos de 1200 de um vivo
// do time ou de uma sentinela dele).

const partidaFalsa = 3304789000 // a segunda partida do logado no LCU falso (replay "watch")

func TestPasseDeVisaoPontaAPonta(t *testing.T) {
	rastreio.IntervaloFase = 0
	db, err := banco.AbreMemoria(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := partida.Migra(db); err != nil {
		t.Fatal(err)
	}
	if err := rastreio.Migra(db); err != nil {
		t.Fatal(err)
	}
	l := lcu.NovoFalso()
	ctx := context.Background()
	bruto, _ := l.Partida(ctx, partidaFalsa)
	if err := partida.Grava(ctx, partida.Deps{DB: db, LCU: l, Campeoes: campeao.Fixo(lcu.CampeoesFalsos)}, bruto, true); err != nil {
		t.Fatal(err)
	}
	pt, _ := partida.Le(db, partidaFalsa)
	rel := replay.NovoRelogioFalso()
	jogo := replay.NovoFalso(rel)
	jogo.DuracaoS = 180
	jogo.Elenco = nil
	for _, p := range pt.Participantes {
		tm := "ORDER"
		if p.Time == 200 {
			tm = "CHAOS"
		}
		jogo.Elenco = append(jogo.Elenco, replay.JogadorFalso{Nome: p.Nome, Campeao: p.Campeao, Time: tm})
	}
	jogo.Mortes = []replay.MorteFalsa{{Indice: 1, De: 60, Ate: 80, Matador: 6}, {Indice: 7, De: 120, Ate: 140, Matador: 2}}
	jogo.Sentinelas = []replay.SentinelaFalsa{{Time: 100, X: 6000, Y: 7000, De: 20, Ate: 150}, {Time: 200, X: 9000, Y: 6000, De: 40, Ate: 170}}
	if err := partida.GravaLinhaDoTempo(db, partidaFalsa, jogo.LinhaDoTempo()); err != nil {
		t.Fatal(err)
	}
	l.AoAssistir = func(int64) { jogo.AbreReplay() }
	leitor, err := Novo()
	if err != nil {
		t.Fatal(err)
	}
	fila := rastreio.NovaFila(rastreio.Deps{DB: db, LCU: l, Hub: web.NovoHub(), Config: rastreio.Config{Velocidade: 4, Minimapa: leitor},
		Replay: replay.Deps{LCU: l, API: jogo, Jogo: jogo, Teclado: jogo, Tela: jogo, Relogio: rel}})
	r, _, err := fila.Cria(ctx, partidaFalsa, rastreio.Pedido{Visao: true})
	if err != nil {
		t.Fatal(err)
	}
	fila.Espera()
	r, _ = rastreio.LeRastreio(db, r.ID)
	if r.Estado != rastreio.Concluido || !r.ComVisao || r.PassesVisaoConcluidos != 2 {
		t.Fatalf("rastreio %s (%s), visão %d de %d", r.Estado, r.Erro, r.PassesVisaoConcluidos, r.PassesVisaoTotal)
	}
	if jogo.Aberto() {
		t.Fatal("o replay ficou aberto")
	}
	// Teclas: as 10 de seleção (duplas, retravas) e só F1/F2/F3 a mais, na ordem
	// F1 ... F3, F2 ... F3.
	var visao []string
	for _, k := range jogo.Teclas {
		switch k {
		case "F1", "F2", "F3":
			visao = append(visao, k)
		case "1", "2", "3", "4", "5", "Q", "W", "E", "R", "T":
		default:
			t.Fatalf("tecla fora da lista: %q", k)
		}
	}
	if len(visao) != 4 || visao[0] != "F1" || visao[1] != "F3" || visao[2] != "F2" || visao[3] != "F3" {
		t.Fatalf("teclas da visão: %v", visao)
	}
	v, ok, err := rastreio.LeVisao(db, partidaFalsa)
	if err != nil || !ok || len(v.Times) != 2 {
		t.Fatalf("visao.json: %v %v", ok, err)
	}
	certos, errados, indet, total := 0, 0, 0, 0
	for _, vt := range v.Times {
		if vt.ModoConferido < 0.99 || vt.AliadosAchados == nil || *vt.AliadosAchados < 0.95 {
			t.Errorf("visão %d: painel conferido %.3f, aliados achados %v", vt.Time, vt.ModoConferido, vt.AliadosAchados)
		}
		if vt.CapturasPorS < 3.5 {
			t.Errorf("visão %d: %.2f capturas por segundo de jogo", vt.Time, vt.CapturasPorS)
		}
		for _, j := range vt.Jogadores {
			i := j.ParticipanteID - 1
			for tt := vt.DeMs + 500; tt < vt.AteMs; tt += 250 {
				e := j.EstadoEm(tt)
				if e == "" {
					continue
				}
				total++
				verdade := jogo.VisivelPara(vt.Time, i, float64(tt)/1000)
				// Perto de uma troca (±300 ms) a captura pode estar de um lado e o
				// instante conferido do outro: não conta.
				troca := jogo.VisivelPara(vt.Time, i, float64(tt-300)/1000) != verdade || jogo.VisivelPara(vt.Time, i, float64(tt+300)/1000) != verdade
				switch {
				case e == rastreio.VisIndeterminado:
					indet++
				case (e == rastreio.VisVisivel) == verdade:
					certos++
				case !troca:
					errados++
					if errados <= 5 {
						t.Logf("errado: visão %d, participante %d em %d ms: %s, verdade %v", vt.Time, j.ParticipanteID, tt, e, verdade)
					}
				}
			}
		}
		t.Logf("visão %d: %d capturas (%.2f/s), %d sentinelas lidas: %+v", vt.Time, vt.Capturas, vt.CapturasPorS, len(vt.Sentinelas), vt.Sentinelas)
		if len(vt.Sentinelas) != 1 {
			t.Errorf("visão %d: %d sentinelas, esperada 1", vt.Time, len(vt.Sentinelas))
		}
	}
	t.Logf("instantes conferidos: %d; certos %d, errados %d, indeterminados %d", total, certos, errados, indet)
	if total == 0 || errados*100 > total || indet*100 > total*25 {
		t.Fatalf("leitura da visão ruim: %d certos, %d errados, %d indeterminados de %d", certos, errados, indet, total)
	}
	// As sentinelas lidas batem com as do falso (lugar e prazo). O prazo tem
	// folga de 4 s: com um retrato em cima do marcador no começo ou no fim, a
	// leitura só vê a sentinela quando ele sai (na azul deste teste: 22 s × 20
	// e 147 s × 150).
	for _, vt := range v.Times {
		for _, s := range vt.Sentinelas {
			var f replay.SentinelaFalsa
			for _, x := range jogo.Sentinelas {
				if x.Time == vt.Time {
					f = x
				}
			}
			if replay.DistanciaMinimapa(s.X, s.Y, f.X, f.Y) > 1.5 || absF(float64(s.DeMs)/1000-f.De) > 4 || absF(float64(s.AteMs)/1000-f.Ate) > 4 {
				t.Errorf("sentinela %d lida %+v, verdade %+v", vt.Time, s, f)
			}
		}
	}
	// Cadeia: a visibilidade aparece para o time inimigo. No app quem refaz a
	// cadeia dos passes depois do passe de visão é a análise (AoFecharPasse);
	// aqui não há análise, então refaz à mão.
	ps, err := rastreio.PassesCompactados(db, partidaFalsa)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ps {
		if _, err := rastreio.RegeraCadeia(db, p.ID); err != nil {
			t.Fatal(err)
		}
	}
	itens := 0
	for _, p := range pt.Participantes {
		rc, err := rastreio.ListaCadeia(db, partidaFalsa, p.ParticipanteID, map[string]bool{"ficou_visivel": true, "sumiu_da_visao": true})
		if err != nil {
			t.Fatal(err)
		}
		itens += len(rc.Itens)
	}
	if itens == 0 {
		t.Fatal("nenhum ficou_visivel/sumiu_da_visao na cadeia")
	}
	t.Logf("itens de visão na cadeia dos 10: %d", itens)
}

func absF(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
