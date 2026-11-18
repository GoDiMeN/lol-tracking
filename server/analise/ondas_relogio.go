package analise

import "math"

// O RELÓGIO DAS ONDAS. As tropas saem de tempo em tempo, sempre nos mesmos
// instantes a partir da primeira saída: sabendo quando a primeira saiu (evento
// MinionsSpawning do jogo, gravado em `eventos`), sabe-se quando cada onda sai
// e, pela velocidade das tropas, quando ela chega ao ponto de encontro de cada
// rota. Nenhuma tropa é rastreada.
//
// REGRAS DO PATCH 26.x (o jogo da partida 3304217746 é a versão 16.22, que a
// Riot chama de 26.22). Fonte: League of Legends Wiki, página "Minion (League of
// Legends)", seção de ondas e o histórico de patches da página (entradas V26.01,
// V25.S1.1, V14.22 e V13.10), consultada em 17/11/2026:
//
//   - V26.01: a primeira onda sai aos 0:30 (era 1:05); aos 14:00 as ondas passam
//     a sair de 25 em 25 s (sequência 13:30 / 14:00 / 14:25) e aos 30:00 de 20 em
//     20 s (29:25 / 29:50 / 30:10); o primeiro canhão vem na 3ª onda (1:30).
//   - Canhão: um a cada 3 ondas nos primeiros 14 min, a cada 2 depois dos 14 min
//     (12:00 / 13:30 / 14:25 / 15:15) e em toda onda depois dos 25 min
//     (24:25 / 25:15 / 25:40). Onda com supertropa não tem canhão (inibidor
//     caído: fora da fase de rotas, ignorado aqui).
//   - V26.01: velocidade base das tropas 350 (era 325); V25.S1.1: ela começa a
//     subir aos 11:00 (degraus de 25). Os minutos dos degraus seguintes NÃO foram
//     confirmados: aqui só o primeiro (375 a partir dos 11:00), o que basta para
//     a fase de rotas.
//   - V13.10/V14.22: nas rotas laterais, da 2ª onda até os 14:00, a tropa nasce
//     com velocidade bônus (111 na 2ª onda, 4,5 a menos a cada onda seguinte),
//     que cai 15 a cada estágio (3 de 7 s e 1 de 4 s). INTERPRETAÇÃO: depois do
//     4º estágio o bônus some (a Wiki diz que o bônus é retirado em 4 estágios
//     em 25 s, sem dizer o que sobra).
//
// ESTIMATIVA (não está em fonte nenhuma): a distância da saída das tropas até
// o ponto de encontro de cada rota. Medida no desenho do mapa (do quartel atrás
// do inibidor até o meio da rota) e conferida no dado da partida 3304217746: os
// dois jogadores do meio se encontram no meio da rota aos ~0:55 e os do topo aos
// ~1:05 (a Ambessa espera no meio da rota desde 0:40; o Garen chega a 1:05).
var RegrasOndas = struct {
	// SaidaPadraoMs: a primeira saída quando o evento do jogo não foi gravado.
	SaidaPadraoMs int64
	// Intervalos: o intervalo entre uma onda e a seguinte vale enquanto a
	// próxima saída não passar de AteMs (é o que dá 13:30/14:00/14:25 e
	// 29:25/29:50/30:10).
	Intervalos []struct{ AteMs, Ms int64 }
	// PrimeiroCanhao: a onda do primeiro canhão; CanhaoACada: de quantas em
	// quantas ondas vem o canhão, pelo instante de saída da onda.
	PrimeiroCanhao int
	CanhaoACada    []struct {
		AteMs int64
		N     int
	}
	// Velocidades: velocidade base das tropas a partir de DeMs.
	Velocidades []struct {
		DeMs int64
		V    float64
	}
	// Bônus das laterais (da 2ª onda até FimBonusLateralMs).
	BonusLateralInicial float64
	BonusLateralPorOnda float64
	EstagiosBonusMs     []int64
	QuedaPorEstagio     float64
	FimBonusLateralMs   int64
	// DistanciaEncontro: da saída das tropas até o ponto de encontro (meio da
	// rota), por rota. ESTIMATIVA.
	DistanciaEncontro map[string]float64
	Fonte             string
}{
	SaidaPadraoMs: 30000,
	Intervalos: []struct{ AteMs, Ms int64 }{
		{14 * 60000, 30000},
		{30 * 60000, 25000},
		{math.MaxInt64, 20000},
	},
	PrimeiroCanhao: 3,
	CanhaoACada: []struct {
		AteMs int64
		N     int
	}{{14 * 60000, 3}, {25 * 60000, 2}, {math.MaxInt64, 1}},
	Velocidades: []struct {
		DeMs int64
		V    float64
	}{{0, 350}, {11 * 60000, 375}},
	BonusLateralInicial: 111,
	BonusLateralPorOnda: 4.5,
	EstagiosBonusMs:     []int64{7000, 7000, 7000, 4000},
	QuedaPorEstagio:     15,
	FimBonusLateralMs:   14 * 60000,
	DistanciaEncontro:   map[string]float64{ZRotaTopo: 11500, ZRotaMeio: 8000, ZRotaBaixo: 11500},
	Fonte: "League of Legends Wiki, \"Minion (League of Legends)\" (histórico V26.01, V25.S1.1, V14.22, V13.10), " +
		"consultada em 17/11/2026; distância até o ponto de encontro estimada no mapa e conferida na partida 3304217746",
}

// Onda é uma onda de tropas: número (1 = a primeira), saída, se tem canhão e
// quando chega ao ponto de encontro de cada rota.
type Onda struct {
	N       int              `json:"n"`
	SaidaMs int64            `json:"saida_ms"`
	Canhao  bool             `json:"canhao"`
	Chegada map[string]int64 `json:"chegada_ms"`
}

// intervaloDepois: o intervalo até a próxima onda, dada a saída desta.
func intervaloDepois(saidaMs int64) int64 {
	for _, iv := range RegrasOndas.Intervalos {
		if saidaMs+iv.Ms <= iv.AteMs {
			return iv.Ms
		}
	}
	return RegrasOndas.Intervalos[len(RegrasOndas.Intervalos)-1].Ms
}

func canhaoACada(saidaMs int64) int {
	for _, c := range RegrasOndas.CanhaoACada {
		if saidaMs < c.AteMs {
			return c.N
		}
	}
	return 1
}

func velocidadeTropa(tMs int64) float64 {
	v := RegrasOndas.Velocidades[0].V
	for _, x := range RegrasOndas.Velocidades {
		if tMs >= x.DeMs {
			v = x.V
		}
	}
	return v
}

// ViagemAteEncontro: quanto a onda n, saindo em saidaMs, leva até o ponto de
// encontro da rota (ms). Nas laterais conta o bônus de velocidade.
func ViagemAteEncontro(rota string, n int, saidaMs int64) int64 {
	dist := RegrasOndas.DistanciaEncontro[rota]
	base := velocidadeTropa(saidaMs)
	bonus := 0.0
	if rota != ZRotaMeio && n >= 2 && saidaMs < RegrasOndas.FimBonusLateralMs {
		bonus = math.Max(0, RegrasOndas.BonusLateralInicial-RegrasOndas.BonusLateralPorOnda*float64(n-2))
	}
	t := 0.0 // segundos
	if bonus > 0 {
		for k, d := range RegrasOndas.EstagiosBonusMs {
			v := base + math.Max(0, bonus-RegrasOndas.QuedaPorEstagio*float64(k))
			s := float64(d) / 1000
			if v*s >= dist {
				return int64(math.Round((t + dist/v) * 1000))
			}
			dist -= v * s
			t += s
		}
	}
	return int64(math.Round((t + dist/base) * 1000))
}

// RelogioDasOndas numera as ondas que saem de saidaMs até ateMs.
func RelogioDasOndas(saidaMs, ateMs int64) []Onda {
	var ondas []Onda
	ultimoCanhao := 0
	t := saidaMs
	for n := 1; t <= ateMs; n++ {
		o := Onda{N: n, SaidaMs: t, Chegada: map[string]int64{}}
		if n == RegrasOndas.PrimeiroCanhao || (ultimoCanhao > 0 && n-ultimoCanhao >= canhaoACada(t)) {
			o.Canhao = true
			ultimoCanhao = n
		}
		for _, r := range []string{ZRotaTopo, ZRotaMeio, ZRotaBaixo} {
			o.Chegada[r] = t + ViagemAteEncontro(r, n, t)
		}
		ondas = append(ondas, o)
		t += intervaloDepois(t)
	}
	return ondas
}
