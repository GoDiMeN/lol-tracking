package analise

import (
	"sort"

	"loltracking/server/rastreio"
)

// Acampamento é um ponto fixo de monstros da selva (mapa 11). Coordenadas do
// centro do acampamento, nas mesmas unidades da posição dos jogadores.
type Acampamento struct {
	Nome string
	Ponto
}

// Acampamentos do Summoner's Rift. Conferido na partida 3304217746: a Vi usa Q
// a ~600 do azul do lado vermelho aos 15:45 e E + Golpear a ~200 dos lobos aos
// 15:49 (posições medidas no rastreio).
var Acampamentos = []Acampamento{
	{"azul (lado azul)", Ponto{3821, 8101}},
	{"sapo (lado azul)", Ponto{2110, 8450}},
	{"lobos (lado azul)", Ponto{3781, 6444}},
	{"aves (lado azul)", Ponto{6943, 5422}},
	{"vermelho (lado azul)", Ponto{7765, 4020}},
	{"krugues (lado azul)", Ponto{8482, 2705}},
	{"azul (lado vermelho)", Ponto{11131, 6990}},
	{"sapo (lado vermelho)", Ponto{12703, 6444}},
	{"lobos (lado vermelho)", Ponto{11008, 8387}},
	{"aves (lado vermelho)", Ponto{7987, 9471}},
	{"vermelho (lado vermelho)", Ponto{7067, 10976}},
	{"krugues (lado vermelho)", Ponto{6317, 12146}},
	{"caranguejo (rio de cima)", Ponto{4400, 9600}},
	{"caranguejo (rio de baixo)", Ponto{10500, 5170}},
}

// acampamentoPerto devolve o acampamento mais próximo dentro de RaioAcampamento.
func acampamentoPerto(p Ponto) (Acampamento, float64, bool) {
	melhor, md := -1, Param.RaioAcampamento
	for i, a := range Acampamentos {
		if d := dist(p, a.Ponto); d <= md {
			melhor, md = i, d
		}
	}
	if melhor < 0 {
		return Acampamento{}, 0, false
	}
	return Acampamentos[melhor], md, true
}

// usosDeHabilidade devolve, em ordem, os instantes em que uma habilidade (Q, W,
// E, R) ou feitiço (D, F) saiu de pronta para em recarga: duas leituras
// seguidas não nulas, a primeira 0 e a segunda maior que 0.
func usosDeHabilidade(as []rastreio.Amostra) []int64 {
	recargas := func(a *rastreio.Amostra) [6]*float64 {
		return [6]*float64{a.CD.Q, a.CD.W, a.CD.E, a.CD.R, a.CD.D, a.CD.F}
	}
	var usos []int64
	var ant [6]*float64
	for i := range as {
		agora := recargas(&as[i])
		for h := range agora {
			if agora[h] == nil {
				continue // leitura nula não muda o estado
			}
			if ant[h] != nil && *ant[h] == 0 && *agora[h] > 0 {
				usos = append(usos, as[i].TMs)
			}
			ant[h] = agora[h]
		}
	}
	sort.Slice(usos, func(a, b int) bool { return usos[a] < usos[b] })
	return usos
}

// usouHabilidadePerto diz se houve uso de habilidade a até janela de t.
func usouHabilidadePerto(usos []int64, t, janela int64) bool {
	i := sort.Search(len(usos), func(k int) bool { return usos[k] >= t-janela })
	return i < len(usos) && usos[i] <= t+janela
}
