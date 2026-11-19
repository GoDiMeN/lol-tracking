package ranking

import (
	"context"
	"fmt"
	"sort"

	"loltracking/server/banco"
)

// COMPARAÇÃO de dois patches no mesmo recorte (elo × rota, ou elo × campeão):
// quem entrou e quem saiu do topo, quem ficou (e quanto andou) e a mudança
// dos campeões (no ranking de rota) ou das rotas (no de campeão) mais
// presentes no topo. Sempre com a amostra de cada lado: um ranking de 6
// jogadores não diz o mesmo que um de 40.

// ItemComparacao é um jogador nos dois lados.
type ItemComparacao struct {
	Puuid    string  `json:"puuid"`
	Nome     string  `json:"nome"`
	Tag      string  `json:"tag"`
	PosicaoA int     `json:"posicao_a"` // 0 = não estava no topo de A
	PosicaoB int     `json:"posicao_b"`
	NotaA    float64 `json:"nota_a"`
	NotaB    float64 `json:"nota_b"`
}

// Presenca: quantos do topo jogam (mais) aquele campeão ou rota em cada lado.
type Presenca struct {
	Chave string `json:"chave"`
	Nome  string `json:"nome"`
	A     int    `json:"a"`
	B     int    `json:"b"`
}

// Comparacao é a resposta de GET /api/rankings/comparar.
type Comparacao struct {
	A        Ranking          `json:"a"`
	B        Ranking          `json:"b"`
	Modo     string           `json:"modo"`
	Topo     int              `json:"topo"`
	Entraram []ItemComparacao `json:"entraram"`
	Sairam   []ItemComparacao `json:"sairam"`
	Ficaram  []ItemComparacao `json:"ficaram"`
	Presenca []Presenca       `json:"presenca"`
	// PresencaDe: "campeao" (ranking de rota) ou "rota" (ranking de campeão).
	PresencaDe string   `json:"presenca_de"`
	Avisos     []string `json:"avisos"`
}

// Compara monta a comparação de dois rankings (A = o patch mais velho, de
// costume; a ordem é a do pedido).
func Compara(ctx context.Context, db *banco.DB, a, b Ranking, modo string, topo int, nomeCampeao func(context.Context, int) string) (Comparacao, error) {
	c := Comparacao{A: a, B: b, Modo: modo, Topo: topo, Entraram: []ItemComparacao{}, Sairam: []ItemComparacao{},
		Ficaram: []ItemComparacao{}, Presenca: []Presenca{}, Avisos: []string{}}
	pa, err := Posicoes(db, a.ID, modo)
	if err != nil {
		return c, err
	}
	pb, err := Posicoes(db, b.ID, modo)
	if err != nil {
		return c, err
	}
	corta := func(l []Posicao) []Posicao {
		if len(l) > topo {
			return l[:topo]
		}
		return l
	}
	ta, tb := corta(pa), corta(pb)
	emA := map[string]Posicao{}
	for _, p := range ta {
		emA[p.Puuid] = p
	}
	emB := map[string]Posicao{}
	for _, p := range tb {
		emB[p.Puuid] = p
	}
	for _, p := range tb {
		if x, ok := emA[p.Puuid]; ok {
			c.Ficaram = append(c.Ficaram, ItemComparacao{Puuid: p.Puuid, Nome: p.Nome, Tag: p.Tag, PosicaoA: x.Posicao, PosicaoB: p.Posicao, NotaA: x.Nota, NotaB: p.Nota})
		} else {
			c.Entraram = append(c.Entraram, ItemComparacao{Puuid: p.Puuid, Nome: p.Nome, Tag: p.Tag, PosicaoB: p.Posicao, NotaB: p.Nota})
		}
	}
	for _, p := range ta {
		if _, ok := emB[p.Puuid]; !ok {
			c.Sairam = append(c.Sairam, ItemComparacao{Puuid: p.Puuid, Nome: p.Nome, Tag: p.Tag, PosicaoA: p.Posicao, NotaA: p.Nota})
		}
	}
	// Presença no topo: o campeão mais jogado na rota (ranking de rota) ou a
	// rota mais jogada com o campeão (ranking de campeão).
	c.PresencaDe = "campeao"
	if a.Tipo == TipoCampeao {
		c.PresencaDe = "rota"
	}
	conta := map[string]*Presenca{}
	soma := func(l []Posicao, lado string) {
		for _, p := range l {
			chave, nome := fmt.Sprint(p.Numeros.CampeaoID), ""
			if c.PresencaDe == "rota" {
				chave, nome = p.Numeros.Rota, NomeRota[p.Numeros.Rota]
			} else if nomeCampeao != nil {
				nome = nomeCampeao(ctx, p.Numeros.CampeaoID)
			}
			if chave == "" || chave == "0" {
				continue
			}
			x := conta[chave]
			if x == nil {
				x = &Presenca{Chave: chave, Nome: nome}
				conta[chave] = x
			}
			if lado == "a" {
				x.A++
			} else {
				x.B++
			}
		}
	}
	soma(ta, "a")
	soma(tb, "b")
	for _, x := range conta {
		c.Presenca = append(c.Presenca, *x)
	}
	sort.SliceStable(c.Presenca, func(i, j int) bool {
		x, y := c.Presenca[i], c.Presenca[j]
		if x.B+x.A != y.B+y.A {
			return x.B+x.A > y.B+y.A
		}
		return x.Chave < y.Chave
	})
	c.Avisos = append(c.Avisos, fmt.Sprintf("amostra: %d jogadores no ranking do %s e %d no do %s (topo comparado: %d)", a.Amostra, a.Patch, b.Amostra, b.Patch, topo))
	if a.VersaoRegra != b.VersaoRegra {
		c.Avisos = append(c.Avisos, fmt.Sprintf("regras diferentes (v%d e v%d): parte da mudança pode ser da fórmula, não do jogo", a.VersaoRegra, b.VersaoRegra))
	}
	if a.Fonte != b.Fonte {
		c.Avisos = append(c.Avisos, fmt.Sprintf("fontes diferentes (%s e %s): a fonte lcu vê só as %d partidas mais recentes de cada um", a.Fonte, b.Fonte, FimHistoricoLCU))
	}
	if !a.EloInteiro || !b.EloInteiro {
		c.Avisos = append(c.Avisos, "abaixo de Mestre cada ranking é de uma AMOSTRA de páginas da liga: quem 'saiu' pode só não ter caído na amostra")
	}
	return c, nil
}
