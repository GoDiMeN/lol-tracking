package rastreio

import (
	"context"
	"errors"
	"image"
	"strings"
	"testing"

	"loltracking/server/arquivo"
	"loltracking/server/replay"
)

// leitorDoFalso é a leitura de minimapa dos testes deste pacote (o pacote
// visao, que lê os pixels, importa este; o teste de ponta a ponta com a
// leitura de verdade fica lá: visao/visao_passe_test.go). Ele pergunta ao
// jogo falso o que o minimapa desenharia agora: a névoa e quem está à vista.
type leitorDoFalso struct {
	jogo *replay.Falso
	// aCada é chamado a cada leitura, com quantas já foram (na goroutine do
	// motor: o teste muda a fase do LCU daqui sem corrida).
	aCada func(n int)
	n     int
}

func (l *leitorDoFalso) LeMinimapa(_ *image.RGBA, _ image.Rectangle, _, _ int) (LeituraMinimapa, error) {
	l.n++
	if l.aCada != nil {
		l.aCada(l.n)
	}
	pb, err := l.jogo.Playback(context.Background())
	if err != nil {
		return LeituraMinimapa{}, err
	}
	v := l.jogo.Visao()
	lt := LeituraMinimapa{Modo: ModoDoTime(v)}
	for i, j := range l.jogo.Elenco {
		if !l.jogo.VisivelPara(v, i, pb.Tempo) {
			continue
		}
		tm := 100
		if j.Time == "CHAOS" {
			tm = 200
		}
		x, y := l.jogo.Posicao(i, pb.Tempo)
		lt.Retratos = append(lt.Retratos, MarcaMinimapa{Time: tm, X: x, Y: y, Cobertura: 1})
	}
	return lt, nil
}

func pos(pid, time int, x, y float64) PosicaoNoInstante {
	return PosicaoNoInstante{ParticipanteID: pid, Time: time, TemPosicao: true, X: x, Y: y}
}

func TestClassificaCapturaRegras(t *testing.T) {
	js := []PosicaoNoInstante{pos(1, 100, 3000, 3000), pos(2, 100, 5000, 2000), pos(6, 200, 8000, 8000), pos(7, 200, 11000, 11000),
		pos(8, 200, 4000, 9000)}
	// Visão azul: o 6 à vista (anel a ~2 px), o 7 e o 8 fora; os dois azuis achados.
	lt := LeituraMinimapa{Modo: ModoNevoaAzul, Retratos: []MarcaMinimapa{{Time: 100, X: 3000, Y: 3010}, {Time: 100, X: 5030, Y: 2000},
		{Time: 200, X: 8050, Y: 8040}}}
	r := ClassificaCaptura(lt, true, 100, js)
	if r.Estados[6] != VisVisivel || r.Estados[7] != VisFora || r.Estados[8] != VisFora || r.AliadosAchados != 2 || len(r.Estados) != 3 {
		t.Fatalf("%+v", r)
	}
	// Anel a 10 px: não casa (tolerância 6 px). Todos os inimigos têm posição,
	// então o anel é de um deles fora do lugar: ninguém sem anel é dado como
	// fora da visão.
	lt.Retratos[2].X = 8000 + 10*44
	r = ClassificaCaptura(lt, true, 100, js)
	if r.Estados[6] != VisIndeterminado || r.Motivos[6] != "anel_sem_dono" || r.Estados[7] != VisIndeterminado || r.Sobras != 1 {
		t.Fatalf("anel deslocado: %+v", r)
	}
	// Com um inimigo sem posição (que pode ser o dono do anel), só quem está
	// perto do anel (< 25 px) fica indeterminado.
	sem8 := append([]PosicaoNoInstante(nil), js...)
	sem8[4].TemPosicao = false
	r = ClassificaCaptura(lt, true, 100, sem8)
	if r.Estados[6] != VisIndeterminado || r.Motivos[6] != "anel_sem_dono" || r.Estados[7] != VisFora || r.Estados[8] != VisIndeterminado {
		t.Fatalf("anel deslocado com um sem posição: %+v", r)
	}
	// Anel inimigo longe de todos e todos com posição: é de alguém deslocado.
	lt.Retratos[2].X, lt.Retratos[2].Y = 13000, 2000
	r = ClassificaCaptura(lt, true, 100, js)
	for _, pid := range []int{6, 7, 8} {
		if r.Estados[pid] != VisIndeterminado {
			t.Fatalf("anel sobrando: %d %s", pid, r.Estados[pid])
		}
	}
	// ... mas se há inimigo sem posição, o anel é dele: os outros seguem fora.
	sem := append([]PosicaoNoInstante(nil), js...)
	sem[4].TemPosicao = false
	r = ClassificaCaptura(lt, true, 100, sem)
	if r.Estados[8] != VisIndeterminado || r.Motivos[8] != "sem_posicao" || r.Estados[6] != VisFora || r.Estados[7] != VisFora {
		t.Fatalf("sem posição: %+v", r)
	}
	// Retrato de aliado em cima do lugar do inimigo: coberto.
	lt2 := LeituraMinimapa{Modo: ModoNevoaAzul, Retratos: []MarcaMinimapa{{Time: 100, X: 3000, Y: 3000}, {Time: 100, X: 8000 + 8*44, Y: 8000}}}
	r = ClassificaCaptura(lt2, true, 100, js)
	if r.Estados[6] != VisIndeterminado || r.Motivos[6] != "coberto" {
		t.Fatalf("coberto: %+v", r)
	}
	// Aliados não achados (menos da metade): tudo indeterminado.
	r = ClassificaCaptura(LeituraMinimapa{Modo: ModoNevoaAzul}, true, 100, js)
	if r.Estados[6] != VisIndeterminado || r.Motivos[6] != "aliados" {
		t.Fatalf("sem aliados: %+v", r)
	}
	// Um aliado só e ele não achado no lugar (posição rastreada deslocada, o
	// que o modo teste mostra): indeterminado, nunca "fora".
	umAliado := []PosicaoNoInstante{pos(1, 100, 3000, 3000), pos(6, 200, 8000, 8000)}
	r = ClassificaCaptura(LeituraMinimapa{Modo: ModoNevoaAzul, Retratos: []MarcaMinimapa{{Time: 100, X: 3000, Y: 6100}}}, true, 100, umAliado)
	if r.Estados[6] != VisIndeterminado || r.Motivos[6] != "aliados" {
		t.Fatalf("um aliado fora do lugar: %+v", r)
	}
	// Morto não conta.
	morto := append([]PosicaoNoInstante(nil), js...)
	morto[2].Morto = true
	r = ClassificaCaptura(lt, true, 100, morto)
	if _, ok := r.Estados[6]; ok {
		t.Fatal("morto entrou")
	}
	// Na borda do minimapa (perto da fonte vermelha, no canto): indeterminado.
	borda := []PosicaoNoInstante{pos(1, 100, 3000, 3000), pos(6, 200, 14800, 14800)}
	r = ClassificaCaptura(LeituraMinimapa{Modo: ModoNevoaAzul, Retratos: []MarcaMinimapa{{Time: 100, X: 3000, Y: 3000}}}, true, 100, borda)
	if r.Estados[6] != VisIndeterminado || r.Motivos[6] != "borda" {
		t.Fatalf("borda: %+v", r)
	}
}

func TestTrechosVisao(t *testing.T) {
	j := VisibilidadeJogador{
		Visivel:       [][2]int64{{0, 1000}, {1300, 3000}, {5000, 6000}, {9000, 9500}},
		Fora:          [][2]int64{{1000, 1300}, {3000, 4000}, {6000, 8000}},
		Indeterminado: [][2]int64{{4000, 5000}},
	}
	ts := TrechosVisao(j)
	// fora de 300 ms entre visíveis vira visível; indeterminado de 1 s entre
	// fora e visível vira fora; buraco 8000–9000 continua buraco.
	esperado := []TrechoVisao{{0, 3000, VisVisivel}, {3000, 5000, VisFora}, {5000, 6000, VisVisivel}, {6000, 8000, VisFora}, {9000, 9500, VisVisivel}}
	if len(ts) != len(esperado) {
		t.Fatalf("%+v", ts)
	}
	for i := range ts {
		if ts[i] != esperado[i] {
			t.Fatalf("%d: %+v, esperado %+v", i, ts[i], esperado[i])
		}
	}
	if j.EstadoEm(1100) != VisFora || j.EstadoEm(4500) != VisIndeterminado || j.EstadoEm(8500) != "" {
		t.Fatal("EstadoEm")
	}
	// Na cadeia: sumiu em 3000, ficou em 5000, sumiu em 6000; depois do buraco, nada.
	as := []Amostra{{TMs: 0}, {TMs: 10000}}
	c := &ContextoCadeia{ParticipanteID: 6, Time: 200, Amostras: as, Extra: map[string]any{ExtraVisao: &VisaoDaPartida{
		Times: []VisaoDoTime{{Time: 100, Jogadores: []VisibilidadeJogador{func() VisibilidadeJogador { j.ParticipanteID = 6; return j }()}}}}}}
	itens := geraVisao(c)
	var got []string
	for _, it := range itens {
		got = append(got, it.Tipo)
	}
	if strings.Join(got, ",") != "sumiu_da_visao,ficou_visivel,sumiu_da_visao" || itens[1].TMs != 5000 || itens[1].Dados["duracao_ms"] != int64(1000) {
		t.Fatalf("%+v", itens)
	}
}

func prepVisao(t *testing.T, ajusta func(j *replay.Falso)) *amb {
	return prepVisaoCom(t, ajusta, nil)
}

func prepVisaoCom(t *testing.T, ajusta func(j *replay.Falso), aCada func(n int)) *amb {
	a := prepara(t, func(j *replay.Falso, c *Config) {
		c.Minimapa = &leitorDoFalso{jogo: j, aCada: aCada}
		c.Velocidade = 4
		j.DuracaoS = 120
		if ajusta != nil {
			ajusta(j)
		}
	})
	return a
}

func TestRastreioComVisao(t *testing.T) {
	a := prepVisao(t, nil)
	r, _, err := a.fila.Cria(context.Background(), partidaTeste, Pedido{Jogadores: []int{3, 8}, Visao: true})
	if err != nil {
		t.Fatal(err)
	}
	a.fila.Espera()
	r, _ = LeRastreio(a.db, r.ID)
	if r.Estado != Concluido || r.PassesVisaoConcluidos != 2 || r.Progresso != 1 {
		t.Fatalf("rastreio %s %s: visão %d/%d, progresso %v", r.Estado, r.Erro, r.PassesVisaoConcluidos, r.PassesVisaoTotal, r.Progresso)
	}
	if a.jogo.Aberto() {
		t.Fatal("replay aberto")
	}
	if a.jogo.Aberturas != 1 {
		t.Fatalf("o replay abriu %d vezes; os passes de visão são na mesma abertura", a.jogo.Aberturas)
	}
	pvs, _ := PassesVisaoDaPartida(a.db, partidaTeste)
	for _, p := range pvs {
		if p.Estado != Concluido || p.Capturas < 400 || p.CapturasModoOK != p.Capturas || p.Taxa == nil || *p.Taxa < 3.5 || p.Teclas != 2 {
			t.Fatalf("passe de visão %+v", p)
		}
		if !arquivo.Existe(a.db, partidaTeste, nomeCapturasVisao(p.ID)) {
			t.Fatal("sem o arquivo das capturas")
		}
	}
	if n := a.conta(t, `SELECT COUNT(*) FROM visao_capturas_brutas`); n != 0 {
		t.Fatalf("%d capturas de trabalho sobraram", n)
	}
	v, ok, err := LeVisao(a.db, partidaTeste)
	if err != nil || !ok || len(v.Times) != 2 {
		t.Fatalf("visao.json %v %v", ok, err)
	}
	// Só o 3 (azul) e o 8 (vermelho) foram rastreados: na visão azul, o 8 é
	// decidido e os outros 4 vermelhos são indeterminados (sem posição).
	azul, _ := v.DoObservador(100)
	for _, j := range azul.Jogadores {
		decidido := somaIntervalos(j.Visivel) + somaIntervalos(j.Fora)
		if (j.ParticipanteID == 8) != (decidido > 0) || (j.ParticipanteID != 8 && j.Motivos["sem_posicao"] == 0) {
			t.Fatalf("visão azul, participante %d: %+v", j.ParticipanteID, j)
		}
	}
	// Teclas: F1 e F3, F2 e F3, nessa ordem, e nada fora da lista.
	var fs []string
	for _, k := range a.jogo.Teclas {
		if strings.HasPrefix(k, "F") {
			fs = append(fs, k)
		}
	}
	if strings.Join(fs, ",") != "F1,F3,F2,F3" {
		t.Fatalf("teclas da visão %v", fs)
	}
	// A rota.
	code, m := a.pede(t, "GET", "/api/partidas/3304789000/visao", "")
	if code != 200 || m["disponivel"] != true || m["visao"] == nil || len(m["passes"].([]any)) != 2 {
		t.Fatalf("%d %v", code, m)
	}
}

func TestVisaoAvulsaPelaRota(t *testing.T) {
	a := prepVisao(t, nil)
	// Sem jogador rastreado: indisponível e 409.
	code, m := a.pede(t, "GET", "/api/partidas/3304789000/visao", "")
	if code != 200 || m["disponivel"] != false || !strings.Contains(m["motivo"].(string), "rastreados") || m["visao"] != nil {
		t.Fatalf("%d %v", code, m)
	}
	if code, _ := a.pede(t, "POST", "/api/partidas/3304789000/visao/rastreio", "{}"); code != 409 {
		t.Fatalf("sem rastreados: %d", code)
	}
	if _, _, err := a.fila.Cria(context.Background(), partidaTeste, Pedido{Jogadores: []int{2, 7}}); err != nil {
		t.Fatal(err)
	}
	a.fila.Espera()
	if code, _ := a.pede(t, "POST", "/api/partidas/3304789000/visao/rastreio", `{"velocidade": 9}`); code != 400 {
		t.Fatalf("velocidade 9: %d", code)
	}
	code, m = a.pede(t, "POST", "/api/partidas/3304789000/visao/rastreio", `{}`)
	if code != 202 || m["estimativa_s"].(float64) <= 0 {
		t.Fatalf("%d %v", code, m)
	}
	// Com ela rodando, criar rastreio é 409 com visao_de.
	if code, m := a.pede(t, "POST", "/api/partidas/3304789000/rastreio", `{"jogadores":[1]}`); code != 409 || m["visao_de"] == nil {
		t.Fatalf("ocupado: %d %v", code, m)
	}
	a.fila.Espera()
	code, m = a.pede(t, "GET", "/api/partidas/3304789000/visao", "")
	if code != 200 || m["rodando"] != false || m["visao"] == nil || m["execucao"] == nil {
		t.Fatalf("%d %v", code, m)
	}
	pvs, _ := PassesVisaoDaPartida(a.db, partidaTeste)
	if len(pvs) != 2 || pvs[0].Estado != Concluido || pvs[1].Estado != Concluido || pvs[0].RastreioID != 0 || pvs[0].Velocidade != VelocidadeVisaoPadrao {
		t.Fatalf("%+v", pvs)
	}
	if a.jogo.Aberto() {
		t.Fatal("replay aberto")
	}
	// De novo: dois passes novos, e o arquivo do anterior sai.
	if code, _ := a.pede(t, "POST", "/api/partidas/3304789000/visao/rastreio", `{"velocidade": 4}`); code != 202 {
		t.Fatal(code)
	}
	a.fila.Espera()
	if arquivo.Existe(a.db, partidaTeste, nomeCapturasVisao(pvs[0].ID)) {
		t.Fatal("o arquivo do passe de visão antigo ficou")
	}
	v, _, _ := LeVisao(a.db, partidaTeste)
	if v.Times[0].PasseVisaoID == pvs[0].ID {
		t.Fatal("o resultado continua no passe antigo")
	}
}

// A névoa que não muda: o passe falha com a frase, sem F3, e o rastreio segue.
func TestNevoaQueNaoMuda(t *testing.T) {
	a := prepVisao(t, func(j *replay.Falso) { j.IgnoraVisao = true })
	r, _, err := a.fila.Cria(context.Background(), partidaTeste, Pedido{Jogadores: []int{3}, Visao: true})
	if err != nil {
		t.Fatal(err)
	}
	a.fila.Espera()
	r, _ = LeRastreio(a.db, r.ID)
	if r.Estado != Concluido {
		t.Fatalf("rastreio %s %s", r.Estado, r.Erro)
	}
	pvs, _ := PassesVisaoDaPartida(a.db, partidaTeste)
	for _, p := range pvs {
		if p.Estado != Falhou || !strings.Contains(p.Erro, "névoa") {
			t.Fatalf("%+v", p)
		}
	}
	var fs []string
	for _, k := range a.jogo.Teclas {
		if strings.HasPrefix(k, "F") {
			fs = append(fs, k)
		}
	}
	if strings.Join(fs, ",") != "F1,F1,F1,F2,F2,F2" {
		t.Fatalf("teclas %v", fs)
	}
	if _, ok, _ := LeVisao(a.db, partidaTeste); ok {
		t.Fatal("sem passe que vale não há visao.json")
	}
}

// Partida ao vivo no meio do passe de visão: para, nenhuma tecla depois, e o
// rastreio fica pausado para retomar.
func TestVisaoParaComPartidaAoVivo(t *testing.T) {
	var a *amb
	// A fase vira "partida ao vivo" na 200ª leitura do primeiro passe de visão.
	a = prepVisaoCom(t, nil, func(n int) {
		if n == 200 {
			a.l.Fase = "InProgress"
		}
	})
	r, _, err := a.fila.Cria(context.Background(), partidaTeste, Pedido{Jogadores: []int{3}, Visao: true})
	if err != nil {
		t.Fatal(err)
	}
	a.fila.Espera()
	r, _ = LeRastreio(a.db, r.ID)
	if r.Estado != Pausado {
		t.Fatalf("rastreio %s %s", r.Estado, r.Erro)
	}
	if a.jogo.Aberto() {
		t.Fatal("replay aberto")
	}
	var fs []string
	for _, k := range a.jogo.Teclas {
		if strings.HasPrefix(k, "F") {
			fs = append(fs, k)
		}
	}
	// F1 no começo; o F3 do fim é recusado (fase ao vivo) e não sai.
	if strings.Join(fs, ",") != "F1" {
		t.Fatalf("teclas %v", fs)
	}
	pvs, _ := PassesVisaoDaPartida(a.db, partidaTeste)
	if pvs[0].Estado != Pausado || pvs[0].TAtualMs == 0 {
		t.Fatalf("%+v", pvs[0])
	}
	// Retoma: continua do ponto e conclui.
	a.l.Fase = "None"
	if _, err := a.fila.Retoma(r.ID); err != nil {
		t.Fatal(err)
	}
	a.fila.Espera()
	r, _ = LeRastreio(a.db, r.ID)
	if r.Estado != Concluido || r.PassesVisaoConcluidos != 2 {
		t.Fatalf("depois de retomar: %s %s %d", r.Estado, r.Erro, r.PassesVisaoConcluidos)
	}
}

func TestErroSemLeitorMinimapa(t *testing.T) {
	a := prepara(t, nil)
	_, _, err := a.fila.Cria(context.Background(), partidaTeste, Pedido{Jogadores: []int{3}, Visao: true})
	var est *ErroEstado
	if !errors.As(err, &est) {
		t.Fatalf("%v", err)
	}
}
