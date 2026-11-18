package aprendizado

import (
	"context"
	"fmt"
	"sync"

	"loltracking/server/lcu"
	"loltracking/server/riot"
)

// RiotFalso é a API da Riot do modo teste e dos testes: determinística, sem
// rede, casada com os jogadores de referência do LCU falso
// (lcu.ReferenciaFalsa). Em cada elo, os 15 de referência com LP decrescente
// pelo n (mas fora de ordem na resposta, como a Riot) e, no topo, um
// "Fantasma" que o cliente não acha (trocou de conta). Os puuids são os da
// Riot ("riot-ref-..."), diferentes dos do LCU, como na vida real.
type RiotFalso struct {
	mu sync.Mutex
	// Recusada simula a chave vencida.
	Recusada bool
	// Pedidos conta as chamadas (Liga + Conta).
	Pedidos int
}

// NovoRiotFalso cria a Riot falsa.
func NovoRiotFalso() *RiotFalso { return &RiotFalso{} }

func (f *RiotFalso) Confere(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Recusada {
		return riot.ErrChaveRecusada
	}
	return nil
}

func indiceElo(elo string) int {
	for i, e := range lcu.ElosReferencia {
		if e == elo {
			return i
		}
	}
	return -1
}

func (f *RiotFalso) Liga(ctx context.Context, reg riot.Regiao, elo, divisao string, pagina int) ([]riot.EntradaLiga, error) {
	f.mu.Lock()
	f.Pedidos++
	recusada := f.Recusada
	f.mu.Unlock()
	if recusada {
		return nil, riot.ErrChaveRecusada
	}
	e := indiceElo(elo)
	if e < 0 {
		return nil, riot.ErrNaoEncontradoRiot
	}
	apex := riot.EloApex(elo)
	// Abaixo de Mestre: os de referência na divisão I e os que a filtragem
	// descarta na II; III, IV e as páginas seguintes vazias.
	if !apex && (pagina > 1 || (divisao != "I" && divisao != "II")) {
		return []riot.EntradaLiga{}, nil
	}
	topo := 99
	if apex {
		topo = 1000 + e*400
	}
	var l []riot.EntradaLiga
	if apex || divisao == "II" {
		// Descartados (ou, no ápice, o veterano no fim da fila), com a melhor taxa de todas.
		l = append(l,
			riot.EntradaLiga{Puuid: fmt.Sprintf("riot-veterano-%d", e), Tier: elo, Rank: "II", LP: topo, Vitorias: 95, Derrotas: 5, Veteran: true},
			riot.EntradaLiga{Puuid: fmt.Sprintf("riot-inativo-%d", e), Tier: elo, Rank: "II", LP: topo, Vitorias: 95, Derrotas: 5, Inactive: true})
		if !apex {
			l = append(l,
				riot.EntradaLiga{Puuid: fmt.Sprintf("riot-poucas-%d", e), Tier: elo, Rank: "II", LP: topo, Vitorias: 18, Derrotas: 2},
				riot.EntradaLiga{Puuid: fmt.Sprintf("riot-travado-%d", e), Tier: elo, Rank: "II", LP: topo, Vitorias: 140, Derrotas: 160})
			return l, nil
		}
	}
	l = append(l, riot.EntradaLiga{Puuid: fmt.Sprintf("riot-fantasma-%d", e), Tier: elo, Rank: "I", LP: topo + 5, Vitorias: 85, Derrotas: 15})
	for i := 0; i < lcu.ReferenciasPorElo; i++ {
		// Fora de ordem de propósito: quem chama ordena. Taxa cai com o n;
		// n%5 == 0 é recém-chegado e n%7 == 0 está em sequência de vitórias.
		n := (i * 7) % lcu.ReferenciasPorElo
		l = append(l, riot.EntradaLiga{Puuid: fmt.Sprintf("riot-ref-%d-%d", e, n), Tier: elo, Rank: "I", LP: topo - n*3,
			Vitorias: 70 - 2*n, Derrotas: 30 + 2*n, FreshBlood: n%5 == 0, HotStreak: n%7 == 0})
	}
	return l, nil
}

func (f *RiotFalso) ContaPorPuuid(ctx context.Context, reg riot.Regiao, puuid string) (riot.Conta, error) {
	f.mu.Lock()
	f.Pedidos++
	recusada := f.Recusada
	f.mu.Unlock()
	if recusada {
		return riot.Conta{}, riot.ErrChaveRecusada
	}
	var e, n int
	if _, err := fmt.Sscanf(puuid, "riot-ref-%d-%d", &e, &n); err == nil && e >= 0 && e < len(lcu.ElosReferencia) && n >= 0 && n < lcu.ReferenciasPorElo {
		inv := lcu.ReferenciaFalsa(e, n)
		// Tag em maiúsculas: o LCU acha pelo Riot ID sem diferenciar.
		return riot.Conta{Puuid: puuid, Nome: inv.Nome, Tag: "BR1"}, nil
	}
	if _, err := fmt.Sscanf(puuid, "riot-fantasma-%d", &e); err == nil && e >= 0 && e < len(lcu.ElosReferencia) {
		return riot.Conta{Puuid: puuid, Nome: "Fantasma " + lcu.ElosReferencia[e], Tag: "br1"}, nil
	}
	return riot.Conta{}, riot.ErrNaoEncontradoRiot
}
