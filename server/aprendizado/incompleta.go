package aprendizado

import (
	"fmt"
	"log"
	"sort"

	"loltracking/server/banco"
	"loltracking/server/rastreio"
)

// PARTIDA INCOMPLETA. O rastreio fecha "concluido" mesmo quando sobra passe
// ruim (rastreio/qualidade_passe.go: a fila refaz o ruim UMA vez no fim; se a
// refação também sai ruim, o jogador fica sem passe bom). Para a coleta e o
// estudo isso não é partida pronta: o jogador pedido não tem dado que valha.
// A partida fica "incompleta", com quantos passes faltam; o "refazer passes
// ruins" (o mesmo da tela da partida) completa e ela volta a "concluida".
//
// Pedidos: com quem = "todos", os 10 da partida; com "jogador", só o
// participante daquela linha da coleta (a irmã aproveitada responde pelo dela).

// ruinsDe devolve os participantes pedidos (pid 0 = todos da partida) que só
// têm passe ruim: nenhum passe concluído (de qualquer rastreio da partida).
func ruinsDe(db *banco.DB, gameID int64, pid int) ([]int, error) {
	ps, err := rastreio.PassesDaPartida(db, gameID)
	if err != nil {
		return nil, err
	}
	bom, ruim := map[int]bool{}, map[int]bool{}
	for _, p := range ps {
		switch p.Estado {
		case rastreio.Concluido:
			bom[p.ParticipanteID] = true
		case rastreio.Ruim:
			ruim[p.ParticipanteID] = true
		}
	}
	l := []int{}
	for x := range ruim {
		if !bom[x] && (pid == 0 || x == pid) {
			l = append(l, x)
		}
	}
	sort.Ints(l)
	return l, nil
}

// pidPedido: quem a linha da coleta pediu (0 = os 10).
func pidPedido(quem string, p PartidaDaColeta) int {
	if quem == QuemTodos {
		return 0
	}
	return p.ParticipanteID
}

// fraseIncompleta: o erro guardado na partida incompleta.
func fraseIncompleta(n int) string {
	if n == 1 {
		return "1 passe saiu ruim (também na refação automática): refaça o passe ruim para a partida valer"
	}
	return fmt.Sprintf("%d passes saíram ruins (também na refação automática): refaça os passes ruins para a partida valer", n)
}

// fechaRastreada decide o estado das linhas da coleta ligadas a um rastreio
// concluído: "concluida" só se todos os passes pedidos de cada uma ficaram
// bons; senão "incompleta". Devolve o estado da linha principal (id) e os
// participantes que faltam nela.
func fechaRastreada(db *banco.DB, coletaID, rastreioID int64, quem string, id int64) (string, []int) {
	linhas, err := lePartidas(db, `WHERE p.coleta_id = ? AND p.rastreio_id = ?`, coletaID, rastreioID)
	if err != nil {
		log.Printf("aprendizado: ler as partidas do rastreio %d: %v", rastreioID, err)
		mudaPartida(db, id, PartidaConcluida, "")
		return PartidaConcluida, []int{}
	}
	estado, faltam := PartidaConcluida, []int{}
	for _, p := range linhas {
		rs, err := ruinsDe(db, p.GameID, pidPedido(quem, p))
		if err != nil {
			log.Printf("aprendizado: passes ruins da partida %d: %v", p.GameID, err)
			rs = []int{}
		}
		e, msg := PartidaConcluida, ""
		if len(rs) > 0 {
			e, msg = PartidaIncompleta, fraseIncompleta(len(rs))
		}
		mudaPartida(db, p.ID, e, msg)
		if p.ID == id {
			estado, faltam = e, rs
		}
	}
	return estado, faltam
}

// acompanhaRuins põe em cada partida rastreada quantos passes ruins faltam e
// acerta o estado guardado com o que os passes dizem agora: a incompleta cujo
// passe foi refeito bom (na tela da partida ou do estudo) vira "concluida"; a
// "concluida" de antes desta regra com passe ruim vira "incompleta".
func acompanhaRuins(db *banco.DB, quem string, p *PartidaDaColeta) {
	p.ParticipantesRuins = []int{}
	if p.RastreioID == nil || (p.Estado != PartidaConcluida && p.Estado != PartidaIncompleta) {
		return
	}
	rs, err := ruinsDe(db, p.GameID, pidPedido(quem, *p))
	if err != nil {
		return
	}
	p.ParticipantesRuins, p.PassesRuins = rs, len(rs)
	switch {
	case len(rs) > 0 && p.Estado == PartidaConcluida:
		p.Estado, p.Erro = PartidaIncompleta, fraseIncompleta(len(rs))
	case len(rs) == 0 && p.Estado == PartidaIncompleta:
		p.Estado, p.Erro = PartidaConcluida, ""
	case len(rs) > 0 && p.Estado == PartidaIncompleta && p.Erro != fraseIncompleta(len(rs)):
		p.Erro = fraseIncompleta(len(rs))
	default:
		return
	}
	if err := mudaPartida(db, p.ID, p.Estado, p.Erro); err != nil {
		log.Printf("aprendizado: acertar a partida %d: %v", p.ID, err)
	}
}
