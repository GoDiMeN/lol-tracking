package aprendizado

import (
	"fmt"
	"math"
	"sort"

	"loltracking/server/riot"
)

// Quem entra na coleta não é "quem tem mais LP no elo", e sim quem tem o
// comportamento de quem SOBE: o jogador que passa a temporada inteira no Ouro 1
// ensina como ficar no Ouro, não como sair dele. A Riot não informa o elo
// anterior de ninguém, então "ex-Prata" ou "ex-Diamante" não dá para afirmar:
// o critério usa só o que cada entrada da league-v4 traz (vitórias, derrotas,
// freshBlood, veteran, hotStreak, inactive, LP).
//
// Prata a Diamante (divisões I a IV):
//   - descarta inactive (parado) e veteran (muitas partidas na mesma liga);
//   - descarta quem tem menos de MinPartidasTaxa partidas (a taxa não vale);
//   - descarta o travado: mais de MaxPartidasTravado partidas e taxa abaixo de
//     TaxaMinTravado;
//   - ordena pela pontuação de subida: limite inferior de Wilson da taxa de
//     vitória (corrige amostra pequena) + BonusFreshBlood + BonusHotStreak; LP
//     só desempata.
//
// Mestre para cima: a liga inteira; descarta inactive; quem é veteran vai para
// o fim da fila; o resto pela mesma pontuação; LP desempata. Depois vem o
// filtro de mono champion.
const (
	MinPartidasTaxa    = 30
	MaxPartidasTravado = 250
	TaxaMinTravado     = 0.53
	BonusFreshBlood    = 0.03
	BonusHotStreak     = 0.02
	// ZWilson: 95% de confiança no limite inferior da taxa de vitória.
	ZWilson = 1.96
	// PaginasPorDivisao: páginas lidas de cada divisão (~205 por página).
	// 4 divisões × 2 páginas = 8 pedidos por elo, longe do limite da chave.
	PaginasPorDivisao = 2
)

// Descartes da filtragem (contados no elo).
const (
	DescInativo        = "inativo"
	DescVeterano       = "veterano"
	DescPoucasPartidas = "poucas_partidas"
	DescTravado        = "travado"
)

// Wilson é o limite inferior do intervalo de Wilson da taxa de vitórias:
// 60% em 10 partidas vale menos que 58% em 200.
func Wilson(vitorias, partidas int) float64 {
	if partidas <= 0 {
		return 0
	}
	n := float64(partidas)
	p := float64(vitorias) / n
	z2 := ZWilson * ZWilson
	return (p + z2/(2*n) - ZWilson*math.Sqrt(p*(1-p)/n+z2/(4*n*n))) / (1 + z2/n)
}

// PontuacaoSubida é a nota que ordena os candidatos.
func PontuacaoSubida(e riot.EntradaLiga) float64 {
	v := Wilson(e.Vitorias, e.Vitorias+e.Derrotas)
	if e.FreshBlood {
		v += BonusFreshBlood
	}
	if e.HotStreak {
		v += BonusHotStreak
	}
	return math.Round(v*10000) / 10000
}

// ordenaCandidatos filtra e ordena as entradas do elo (regra acima) e conta
// os descartes.
func ordenaCandidatos(entradas []riot.EntradaLiga, apex bool) ([]riot.EntradaLiga, map[string]int) {
	desc := map[string]int{}
	var l []riot.EntradaLiga
	for _, e := range entradas {
		n := e.Vitorias + e.Derrotas
		switch {
		case e.Inactive:
			desc[DescInativo]++
			continue
		case apex:
		case e.Veteran:
			desc[DescVeterano]++
			continue
		case n < MinPartidasTaxa:
			desc[DescPoucasPartidas]++
			continue
		case n > MaxPartidasTravado && float64(e.Vitorias)/float64(n) < TaxaMinTravado:
			desc[DescTravado]++
			continue
		}
		l = append(l, e)
	}
	sort.SliceStable(l, func(a, b int) bool {
		if apex && l[a].Veteran != l[b].Veteran {
			return !l[a].Veteran
		}
		pa, pb := PontuacaoSubida(l[a]), PontuacaoSubida(l[b])
		if pa != pb {
			return pa > pb
		}
		return l[a].LP > l[b].LP
	})
	return l, desc
}

// Motivo é a frase que explica a escolha: "subindo: 63% de vitórias em 84
// partidas, recém-chegado ao elo".
func Motivo(j JogadorDaColeta) string {
	n := j.Vitorias + j.Derrotas
	if n == 0 {
		return ""
	}
	s := fmt.Sprintf("subindo: %.0f%% de vitórias em %d partidas", j.TaxaVitoria*100, n)
	if j.FreshBlood {
		s += ", recém-chegado ao elo"
	}
	if j.HotStreak {
		s += ", em sequência de vitórias"
	}
	if j.Veteran {
		s = fmt.Sprintf("%.0f%% de vitórias em %d partidas, há muito tempo nesta liga", j.TaxaVitoria*100, n)
	}
	return s
}
