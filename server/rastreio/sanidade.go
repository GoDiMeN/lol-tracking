package rastreio

import "math"

// saneador aplica à leitura da tela as regras que dependem do tempo e do que a
// API diz. Um por passe (o leitor é puro). Regra geral: na dúvida, nulo.
//
//   - Painel de outro jogador: com a trava perdida, morto ou com o nível da tela
//     diferente do nível do playerlist, o painel não é do jogador seguido (é o
//     de quem a câmera pegou) → todos os campos do painel nulos.
//   - vida_max e mana_max só mudam com nível, item ou efeito: um máximo novo só
//     é aceito depois de lido igual em 2 leituras seguidas, ou se a mudança for
//     pequena (≤ 15%). Até lá o par (atual, máximo) sai nulo.
//   - vida ≤ vida_max e mana ≤ mana_max (o leitor já garante).
//   - Atributos: idem com folga maior (buffs mudam de verdade): mudança > 60%
//     só depois de 2 leituras iguais.
type saneador struct {
	vidaMax, manaMax maximoConfirmado
	atr              [6]maximoConfirmado
}

// maximoConfirmado guarda o valor aceito e um candidato a substituí-lo.
type maximoConfirmado struct {
	aceito, candidato float64
	temAceito         bool
	folga             float64
}

// aceita diz se v pode ser usado agora.
func (m *maximoConfirmado) aceita(v float64) bool {
	if !m.temAceito {
		// Primeira leitura do passe: precisa de confirmação também.
		if m.candidato == v {
			m.aceito, m.temAceito = v, true
			return true
		}
		m.candidato = v
		return false
	}
	if m.aceito == v || math.Abs(v-m.aceito) <= m.folga*m.aceito {
		m.aceito, m.candidato = v, 0
		return true
	}
	if m.candidato == v {
		m.aceito, m.candidato = v, 0
		return true
	}
	m.candidato = v
	return false
}

func novoSaneador() *saneador {
	s := &saneador{vidaMax: maximoConfirmado{folga: 0.15}, manaMax: maximoConfirmado{folga: 0.15}}
	for i := range s.atr {
		s.atr[i].folga = 0.6
	}
	return s
}

// PainelCobertoMs: tempo de jogo com o jogador vivo e seguido sem nenhuma vida
// lida a partir do qual o passe avisa que o painel está coberto. Na partida
// 3304217746 um passe inteiro saiu assim (183 leituras em 1541) sem aviso.
const PainelCobertoMs = 30000

// limpaPainel zera os campos do painel.
func limpaPainel(l *Leitura) {
	l.Vida, l.VidaMax, l.Mana, l.ManaMax, l.Nivel = nil, nil, nil, nil, nil
	l.CD, l.Atr = Recargas{}, Atributos{}
}

// saneia ajusta a leitura no lugar. selecaoOK: a câmera está no jogador
// seguido; morto e nivel: do playerlist.
func (s *saneador) saneia(l *Leitura, selecaoOK, morto bool, nivel int) {
	if !selecaoOK || morto || (l.Nivel != nil && nivel > 0 && *l.Nivel != nivel) {
		limpaPainel(l)
		return
	}
	if l.VidaMax != nil && !s.vidaMax.aceita(*l.VidaMax) {
		l.Vida, l.VidaMax = nil, nil
	}
	if l.ManaMax != nil && !s.manaMax.aceita(*l.ManaMax) {
		l.Mana, l.ManaMax = nil, nil
	}
	atr := []**float64{&l.Atr.AD, &l.Atr.AP, &l.Atr.Armadura, &l.Atr.RM, &l.Atr.VelAtaque, &l.Atr.VelMov}
	for i, p := range atr {
		if *p != nil && !s.atr[i].aceita(**p) {
			*p = nil
		}
	}
}
