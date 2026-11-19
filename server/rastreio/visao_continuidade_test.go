package rastreio

import (
	"strings"
	"testing"
)

func f64(v float64) *float64 { return &v }

// Trechos fora do mapa: 2+ amostras seguidas sem leitura boa com a trava
// perdida e a seleção nele (Investida Alfa, teleporte em trânsito). A perda
// de uma amostra só, ou com a seleção fora, não entra.
func TestTrechosForaDoMapa(t *testing.T) {
	boa := func(ms int64) Amostra {
		return Amostra{TMs: ms, X: f64(5000), Y: f64(5000), Origem: OrigemCamera, SelecaoOK: true}
	}
	perdida := func(ms int64, sel bool) Amostra { return Amostra{TMs: ms, TravaPerdida: true, SelecaoOK: sel} }
	as := []Amostra{boa(0), boa(250), perdida(500, true), perdida(750, true), perdida(1000, true), boa(1250), // Investida Alfa
		perdida(1500, true), boa(1750), // uma amostra só
		perdida(2000, false), perdida(2250, false), boa(2500)} // seleção fora (a câmera foi para outro lugar)
	ts := trechosForaDoMapa(as)
	if len(ts) != 1 || ts[0] != [2]int64{500, 1250} {
		t.Fatalf("%v", ts)
	}
	f := fonteDePosicoes{times: map[int]int{2: 100}, amostra: map[int][]Amostra{2: as}, foraDoMapa: map[int][][2]int64{2: ts}}
	if p := f.noInstante(2, 800); p.TemPosicao || !p.ForaDoMapa {
		t.Fatalf("no meio da Investida Alfa: %+v", p)
	}
	if p := f.noInstante(2, 2100); p.TemPosicao || p.ForaDoMapa {
		t.Fatalf("sem a seleção: %+v", p)
	}
	// Na decisão: fora do mapa é estado próprio, não "sem_posicao".
	js := []PosicaoNoInstante{pos(6, 200, 3000, 3000), {ParticipanteID: 2, Time: 100, ForaDoMapa: true}, {ParticipanteID: 3, Time: 100}}
	r := ClassificaCaptura(LeituraMinimapa{Modo: ModoNevoaVermelho, Retratos: []MarcaMinimapa{{Time: 200, X: 3000, Y: 3000}}}, true, 200, js)
	if r.Estados[2] != VisForaDoMapa || r.Estados[3] != VisIndeterminado || r.Motivos[3] != "sem_posicao" {
		t.Fatalf("%+v", r)
	}
}

// seqDe monta uma sequência de capturas de 500 ms com os estados dados
// ("v" visível, "f" fora, "i" indeterminado coberto, "o" fora por ordem) e
// as capturas classificadas com o inimigo 6 em (x, y) e o aliado 1 à
// distância dist[k] dele.
func seqDe(estados string, dist []float64) ([]estadoCap, []CapturaClassificada) {
	var seq []estadoCap
	var ccs []CapturaClassificada
	for k, c := range estados {
		de := int64(k) * 500
		e := estadoCap{de: de, ate: de + 500, k: k}
		switch c {
		case 'v':
			e.estado = VisVisivel
		case 'f':
			e.estado = VisFora
		case 'o':
			e.estado, e.porOrdem = VisFora, true
		case 's':
			e.estado, e.motivo = VisIndeterminado, "sem_posicao"
		default:
			e.estado, e.motivo = VisIndeterminado, "coberto"
		}
		seq = append(seq, e)
		ccs = append(ccs, CapturaClassificada{De: de, Ate: de + 500, TMs: de + 250,
			Inimigos:        map[int]PosicaoNoInstante{6: {ParticipanteID: 6, Time: 200, TemPosicao: c != 's', X: 7000, Y: 7000}},
			AliadoMaisPerto: map[int]float64{6: dist[k]}})
	}
	return seq, ccs
}

func estadosDe(seq []estadoCap) string {
	var b strings.Builder
	for _, e := range seq {
		c := map[string]byte{VisVisivel: 'v', VisFora: 'f', VisIndeterminado: 'i'}[e.estado]
		if e.inferido {
			c -= 'a' - 'A'
		}
		b.WriteByte(c)
	}
	return b.String()
}

func distancias(n int, d float64) []float64 {
	ds := make([]float64, n)
	for i := range ds {
		ds[i] = d
	}
	return ds
}

func TestInferirTrechos(t *testing.T) {
	casos := []struct {
		nome, entrada string
		dist          []float64
		sents         []SentinelaLida
		esperado      string
	}{
		// Visível dos dois lados, 2 s coberto, aliado do observador a 800: herda.
		{"visível perto do aliado", "vviiiivv", distancias(8, 800), nil, "vvVVVVvv"},
		// O mesmo com o aliado longe: só até InferenciaCurtaMs (1 s) herda.
		{"visível longe, curto", "vviivv", distancias(6, 3000), nil, "vvVVvv"},
		{"visível longe, comprido", "vviiiivv", distancias(8, 3000), nil, "vviiiivv"},
		// Mais que InferenciaVisivelMaxMs (3 s): não herda.
		{"visível comprido demais", "vviiiiiiivv", distancias(11, 500), nil, "vviiiiiiivv"},
		// Nunca através de uma troca.
		{"troca", "vviiff", distancias(6, 500), nil, "vviiff"},
		// Fora dos dois lados, longe de aliado e de sentinela: herda.
		{"fora longe", "ffiiiff", distancias(7, 4000), nil, "ffFFFff"},
		// Fora com aliado perto (pode tê-lo visto): não.
		{"fora com aliado perto", "ffiiiff", distancias(7, 1500), nil, "ffiiiff"},
		// Fora com sentinela do observador lida perto: não.
		{"fora com sentinela", "ffiiiff", distancias(7, 4000), []SentinelaLida{{X: 7500, Y: 7000, DeMs: 0, AteMs: 9000}}, "ffiiiff"},
		// Sem posição no meio: não.
		{"sem posição", "vvisivv", distancias(7, 500), nil, "vviiivv"},
		// Sem leitura encostada de um dos lados (começo da sequência): não.
		{"ponta", "iivv", distancias(4, 500), nil, "iivv"},
	}
	for _, c := range casos {
		seq, ccs := seqDe(c.entrada, c.dist)
		inferirTrechos(seq, ccs, 6, c.sents)
		if got := estadosDe(seq); got != c.esperado {
			t.Errorf("%s: %s, esperado %s", c.nome, got, c.esperado)
		}
	}
}

// "Fora" só pela ordem de desenho, curto e com visível dos dois lados, volta
// a indeterminado (e a continuidade pode então herdar o visível); com fora
// firme de um lado, fica.
func TestDesfazForaPorOrdem(t *testing.T) {
	seq, ccs := seqDe("vvoovv", distancias(6, 600))
	desfazForaPorOrdem(seq)
	if got := estadosDe(seq); got != "vviivv" {
		t.Fatalf("entre visíveis: %s", got)
	}
	inferirTrechos(seq, ccs, 6, nil)
	if got := estadosDe(seq); got != "vvVVvv" {
		t.Fatalf("depois da continuidade: %s", got)
	}
	seq, _ = seqDe("ffoovv", distancias(6, 600))
	desfazForaPorOrdem(seq)
	if got := estadosDe(seq); got != "ffffvv" {
		t.Fatalf("com fora firme de um lado: %s", got)
	}
}

// Ida e volta do arquivo de capturas com a coluna dos arcos e a máscara;
// arquivo velho (4 colunas) continua lendo.
func TestCapturasVisaoArcos(t *testing.T) {
	cs := []CapturaVisao{{TMs: 1000, Modo: ModoNevoaVermelho,
		Retratos: []MarcaMinimapa{{Time: 200, X: 5000, Y: 7000, Cobertura: 1}, {Time: 100, X: 5300, Y: 7000, Cobertura: 0.7, Arco: 0xfffffffffff0000f}},
		Arcos:    []MarcaMinimapa{{Time: 100, X: 5200, Y: 7100, Cobertura: 0.41, Arco: 0x00ffff00ff}}}}
	lidas, err := capturasDoTexto(textoCapturas(cs))
	if err != nil || len(lidas) != 1 || len(lidas[0].Arcos) != 1 || lidas[0].Arcos[0].Arco != 0x00ffff00ff || lidas[0].Arcos[0].Cobertura != 0.41 ||
		lidas[0].Retratos[1].Arco != 0xfffffffffff0000f || lidas[0].Retratos[0].Arco != 0 {
		t.Fatalf("ida e volta: %+v %v", lidas, err)
	}
	if _, err := capturasDoTexto([]byte("1000\tazul\t100:5000:7000:1:zz\t\t\n")); err == nil {
		t.Fatal("máscara inválida passou")
	}
	velho, err := capturasDoTexto([]byte("1000\tazul\t100:5000:7000:1\t\n"))
	if err != nil || len(velho) != 1 || len(velho[0].Arcos) != 0 {
		t.Fatalf("arquivo de 4 colunas: %+v %v", velho, err)
	}
}
