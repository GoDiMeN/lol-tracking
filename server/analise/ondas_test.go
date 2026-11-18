package analise

import (
	"bufio"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"

	"loltracking/server/rastreio"
)

// Casos sintéticos das ondas: dois jogadores do topo (1 azul, 6 vermelho), a
// onda numa posição s(t) do eixo e cada um a MeiaDistanciaOnda do lado dele.

func TestRelogioDasOndas(t *testing.T) {
	ondas := RelogioDasOndas(30000, 31*60000)
	saida := map[int]int64{}
	canhao := map[int]bool{}
	for _, o := range ondas {
		saida[o.N], canhao[o.N] = o.SaidaMs, o.Canhao
	}
	// 30 em 30 s até 14:00, 25 em 25 s depois, 20 em 20 s depois dos 30:00.
	if saida[1] != 30000 || saida[27] != 13*60000+30000 || saida[28] != 14*60000 || saida[29] != 14*60000+25000 {
		t.Fatalf("saídas: %v %v %v %v", saida[1], saida[27], saida[28], saida[29])
	}
	for i, o := range ondas {
		if MinSeg(o.SaidaMs) == "29:50" && (i+1 >= len(ondas) || MinSeg(ondas[i+1].SaidaMs) != "30:10") {
			t.Fatalf("depois das 29:50 devia vir 30:10")
		}
	}
	// Canhão: 3ª onda, de 3 em 3 até 14 min (12:00, 13:30), 14:25, 15:15; depois
	// dos 25 min em toda onda (24:25, 25:15, 25:40).
	for _, n := range []int{3, 6, 24, 27, 29, 31} {
		if !canhao[n] {
			t.Errorf("onda %d (%s) devia ter canhão", n, MinSeg(saida[n]))
		}
	}
	for _, n := range []int{1, 2, 4, 25, 28, 30} {
		if canhao[n] {
			t.Errorf("onda %d (%s) não devia ter canhão", n, MinSeg(saida[n]))
		}
	}
	for _, o := range ondas {
		switch MinSeg(o.SaidaMs) {
		case "24:25", "25:15", "25:40":
			if !o.Canhao {
				t.Errorf("onda das %s sem canhão", MinSeg(o.SaidaMs))
			}
		case "24:50":
			if o.Canhao {
				t.Errorf("onda das 24:50 com canhão")
			}
		}
	}
	// Meio: 8000 a 350/s; laterais, 1ª onda sem bônus e 2ª com.
	if v := ViagemAteEncontro(ZRotaMeio, 1, 30000); v != 22857 {
		t.Errorf("viagem do meio = %d", v)
	}
	if a, b := ViagemAteEncontro(ZRotaTopo, 1, 30000), ViagemAteEncontro(ZRotaTopo, 2, 60000); a <= b || b > 27000 {
		t.Errorf("viagem do topo: 1ª %d, 2ª %d", a, b)
	}
}

func TestEixoDaRota(t *testing.T) {
	topo, baixo := NovoEixo(ZRotaTopo), NovoEixo(ZRotaBaixo)
	torre := func(e EixoRota, time int) float64 {
		tr, _ := e.torre(time, 1)
		return tr.S
	}
	if math.Abs(torre(topo, 100)-6941) > 1 || math.Abs(torre(topo, 200)-12356) > 1 {
		t.Fatalf("torres do topo: %v", topo.Torres)
	}
	// O baixo é o topo espelhado: a torre externa azul de baixo fica onde a
	// vermelha do topo fica, contada da base azul.
	if math.Abs(torre(baixo, 100)-(topo.Comprimento-torre(topo, 200))) > 20 {
		t.Fatalf("torre azul de baixo em %.0f", torre(baixo, 100))
	}
	for _, e := range []EixoRota{topo, baixo, NovoEixo(ZRotaMeio)} {
		for _, s := range []float64{2000, 7000, 9650, 12000} {
			if s2, d := e.S(e.Ponto(s)); math.Abs(s2-s) > 1 || d > 1 {
				t.Errorf("%s: S(Ponto(%.0f)) = %.0f (d %.0f)", e.Rota, s, s2, d)
			}
		}
	}
}

// cenario monta a entrada: onda em sOnda(t); azul (1) e vermelho (6) a meia
// distância dela, salvo onde `fora` diz outra coisa (ponto fixo ou morto).
type foraDaOnda func(pid int, t int64) (p *Ponto, morto, mexe bool)

func cenario(ate int64, sOnda func(int64) float64, fora foraDaOnda) EntradaOndas {
	e := NovoEixo(ZRotaTopo)
	in := EntradaOndas{SaidaTropasMs: 30000, SaidaMedida: true, Times: map[int]int{1: 100, 6: 200},
		Posicoes: map[int]string{1: "TOP", 6: "TOP"}, Amostras: map[int][]rastreio.Amostra{}}
	for _, pid := range []int{1, 6} {
		lado := -Param.MeiaDistanciaOnda
		if pid == 6 {
			lado = Param.MeiaDistanciaOnda
		}
		for tm := int64(0); tm <= ate; tm += 500 {
			// Vai e vem do último golpe: ±150 em 6 s.
			p := e.Ponto(sOnda(tm) + lado + 150*math.Sin(float64(tm)/6000*2*math.Pi))
			a := rastreio.Amostra{TMs: tm}
			if fora != nil {
				if q, morto, mexe := fora(pid, tm); mexe {
					if morto {
						a.Morto = true
					} else {
						p = *q
					}
				}
			}
			x, y := p.X, p.Y
			if !a.Morto {
				a.X, a.Y = &x, &y
			}
			in.Amostras[pid] = append(in.Amostras[pid], a)
		}
	}
	// Os dois farmam 6 CS por minuto.
	for m := int64(0); m*60000 <= ate+60000; m++ {
		in.Linha.Quadros = append(in.Linha.Quadros, rastreio.QuadroLT{TMs: m * 60000,
			Jogadores: map[int]rastreio.JogadorLT{1: {CS: int(6 * m)}, 6: {CS: int(6 * m)}}})
	}
	return in
}

func rotaDe(o OndasDaPartida, rota string) OndasDaRota {
	for _, r := range o.Rotas {
		if r.Rota == rota {
			return r
		}
	}
	return OndasDaRota{}
}

// estadoEm: o trecho que vale em t.
func estadoEm(r OndasDaRota, t int64) TrechoOnda {
	for _, e := range r.Estados {
		if e.DeMs <= t && t < e.AteMs {
			return e
		}
	}
	return TrechoOnda{}
}

func TestOndasFreeze(t *testing.T) {
	enc := NovoEixo(ZRotaTopo).Encontro
	// A onda presa 1200 para o lado azul do ponto de encontro (fora do alcance
	// da torre azul, que está a ~2700 do encontro): freeze do azul.
	in := cenario(6*60000, func(int64) float64 { return enc - 1200 }, nil)
	r := rotaDe(DeduzOndas(in), ZRotaTopo)
	for _, tm := range []int64{90000, 180000, 300000} {
		if e := estadoEm(r, tm); e.Estado != EOndaFreeze || e.Time != 100 {
			t.Fatalf("aos %s: %+v (estados %+v)", MinSeg(tm), e, r.Estados)
		}
	}
	if e := estadoEm(r, 180000); e.Confianca != "alta" {
		t.Errorf("os dois lados presentes e a confiança é %q", e.Confianca)
	}
	if len(r.Perdidas) != 0 {
		t.Errorf("ninguém saiu e há ondas perdidas: %+v", r.Perdidas)
	}
	// A mesma onda presa NO ponto de encontro não é freeze de ninguém.
	r = rotaDe(DeduzOndas(cenario(6*60000, func(int64) float64 { return enc }, nil)), ZRotaTopo)
	if e := estadoEm(r, 180000); e.Estado != EOndaParada || e.Time != 0 {
		t.Fatalf("onda parada no meio: %+v", e)
	}
	// Na cadeia: freeze a favor do azul, contra para o vermelho.
	for pid, favor := range map[int]bool{1: true, 6: false} {
		c := &rastreio.ContextoCadeia{GameID: 1, ParticipanteID: pid, Time: in.Times[pid], Amostras: in.Amostras[pid], Linha: in.Linha,
			Posicao: "TOP", Times: in.Times, Posicoes: in.Posicoes, Outros: map[int][]rastreio.Amostra{7 - pid: in.Amostras[7-pid]},
			Extra: map[string]any{ExtraSaidaTropas: int64(30000)}}
		achou := false
		for _, it := range geraOndas(c) {
			if it.Tipo == EOndaFreeze {
				achou = true
				if it.Dados["a_favor"] != favor || it.Dados["time"] != 100 {
					t.Errorf("participante %d: %v", pid, it.Dados)
				}
			}
		}
		if !achou {
			t.Errorf("participante %d sem freeze na cadeia", pid)
		}
	}
}

func TestOndasCrashEVolta(t *testing.T) {
	e := NovoEixo(ZRotaTopo)
	torreV, _ := e.torre(200, 1)
	// 1:00–2:30 o azul empurra do encontro até embaixo da torre vermelha; fica
	// lá até 3:00; volta rápido até 1500 do lado azul até 4:00.
	s := func(tm int64) float64 {
		alvo := torreV.S - 300
		switch {
		case tm < 60000:
			return e.Encontro
		case tm < 150000:
			return e.Encontro + (alvo-e.Encontro)*float64(tm-60000)/90000
		case tm < 180000:
			return alvo
		case tm < 240000:
			return alvo + (e.Encontro-1500-alvo)*float64(tm-180000)/60000
		}
		return e.Encontro - 1500
	}
	r := rotaDe(DeduzOndas(cenario(5*60000, s, nil)), ZRotaTopo)
	if x := estadoEm(r, 100000); x.Estado != EOndaEmpurra || x.Time != 100 {
		t.Errorf("1:40: %+v", x)
	}
	if x := estadoEm(r, 165000); x.Estado != EOndaNaTorre || x.Time != 200 {
		t.Errorf("2:45: %+v", x)
	}
	if x := estadoEm(r, 215000); x.Estado != EOndaVoltando || x.Time != 200 || x.Velocidade != "rapida" {
		t.Errorf("3:35: %+v (todos %+v)", x, r.Estados)
	}
}

func TestOndasPerdidas(t *testing.T) {
	enc := NovoEixo(ZRotaTopo).Encontro
	fonte := FonteAzul
	// O azul vai à base de 2:50 a 3:45; o vermelho morre de 4:20 a 5:00.
	fora := func(pid int, tm int64) (*Ponto, bool, bool) {
		if pid == 1 && tm >= 170000 && tm < 225000 {
			return &fonte, false, true
		}
		if pid == 6 && tm >= 260000 && tm < 300000 {
			return nil, true, true
		}
		return nil, false, false
	}
	r := rotaDe(DeduzOndas(cenario(6*60000, func(int64) float64 { return enc - 1200 }, fora)), ZRotaTopo)
	motivos := map[int]string{}
	for _, p := range r.Perdidas {
		motivos[p.ParticipanteID] += p.Motivo + " "
	}
	if !strings.Contains(motivos[1], "na_base") || strings.Contains(motivos[1], "morto") {
		t.Errorf("azul: %q (%+v)", motivos[1], r.Perdidas)
	}
	if !strings.Contains(motivos[6], "morto") {
		t.Errorf("vermelho: %q (%+v)", motivos[6], r.Perdidas)
	}
	// Com um lado só (o azul na base) a posição segue estimada, com confiança média.
	if p, ok := pontoPerto(r.Pontos, 200000, 1000); !ok || p.Confianca != "media" {
		t.Errorf("3:20: %+v %v", p, ok)
	}
	// Sem ninguém na rota não há ponto (não se inventa).
	vazio := rotaDe(DeduzOndas(cenario(6*60000, func(int64) float64 { return enc }, func(pid int, tm int64) (*Ponto, bool, bool) {
		if tm >= 120000 && tm < 180000 {
			return nil, true, true
		}
		return nil, false, false
	})), ZRotaTopo)
	if _, ok := pontoPerto(vazio.Pontos, 150000, 3000); ok {
		t.Errorf("os dois mortos e há posição da onda aos 2:30")
	}
}

// TestOndasCasoRealTopo: a rota do topo da partida 3304217746 (Ambessa × Garen,
// testdata, gerado por TestRealGeraCasoTopo). O dono viu no jogo o freeze do
// azul de ~4:30 a ~8:00 e a onda embaixo da torre azul aos 9:00–9:30.
func TestOndasCasoRealTopo(t *testing.T) {
	f, err := os.Open("testdata/ondas_topo_3304217746.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	in := EntradaOndas{SaidaTropasMs: 30000, SaidaMedida: true, Times: map[int]int{1: 100, 6: 200},
		Posicoes: map[int]string{1: "TOP", 6: "TOP"}, Amostras: map[int][]rastreio.Amostra{}}
	num := func(s string) float64 { v, _ := strconv.ParseFloat(s, 64); return v }
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		c := strings.Split(sc.Text(), ",")
		switch c[0] {
		case "cs":
			in.Linha.Quadros = append(in.Linha.Quadros, rastreio.QuadroLT{TMs: int64(num(c[1])),
				Jogadores: map[int]rastreio.JogadorLT{1: {CS: int(num(c[2]))}, 6: {CS: int(num(c[3]))}}})
		case "k":
			x, y := num(c[2]), num(c[3])
			in.Linha.Eventos = append(in.Linha.Eventos, rastreio.EventoLT{Tipo: "CHAMPION_KILL", TMs: int64(num(c[1])), X: &x, Y: &y})
		case "p":
			a := rastreio.Amostra{TMs: int64(num(c[1])), Morto: c[5] == "1"}
			if !a.Morto {
				x, y := num(c[3]), num(c[4])
				a.X, a.Y = &x, &y
			}
			pid := int(num(c[2]))
			in.Amostras[pid] = append(in.Amostras[pid], a)
		}
	}
	in.Eventos = in.Linha.Eventos
	r := rotaDe(DeduzOndas(in), ZRotaTopo)
	for _, tm := range []int64{4*60000 + 40000, 5 * 60000, 6 * 60000, 6*60000 + 40000, 7*60000 + 45000} {
		if e := estadoEm(r, tm); e.Estado != EOndaFreeze || e.Time != 100 {
			t.Errorf("aos %s: %+v", MinSeg(tm), e)
		}
	}
	if e := estadoEm(r, 9*60000+20000); e.Estado != EOndaNaTorre || e.Time != 100 {
		t.Errorf("aos 9:20: %+v", e)
	}
	if e := estadoEm(r, 2*60000+40000); e.Estado != EOndaNaTorre || e.Time != 200 {
		t.Errorf("aos 2:40: %+v", e)
	}
}
