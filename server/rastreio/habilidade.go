package rastreio

import "math"

// Itens da cadeia que saem da leitura da tela (tarefa 3): uso de habilidade e
// vida baixa. Vocabulário em docs/arquitetura/dados.md.

func init() {
	RegistraGerador("habilidade", geraHabilidade)
	RegistraGerador("vida_baixa", geraVidaBaixa)
}

// Histerese do uso de habilidade: a transição só conta com LeiturasConfirma
// leituras seguidas (ignorando as nulas) no estado novo, e uma lacuna de
// mais de LacunaEsqueceMs sem leitura faz esquecer o estado (não dá para saber
// o que aconteceu no meio).
const (
	LeiturasConfirma = 2
	LacunaEsqueceMs  = 3000
	// SaltoRecargaS: recarga que sobe mais que isso de uma leitura para a outra
	// (e se confirma) é uso novo logo depois de a habilidade voltar.
	SaltoRecargaS = 3.0
)

var nomesHabilidade = [6]string{"q", "w", "e", "r", "d", "f"}

func recargaDe(a *Amostra, i int) *float64 {
	switch i {
	case 0:
		return a.CD.Q
	case 1:
		return a.CD.W
	case 2:
		return a.CD.E
	case 3:
		return a.CD.R
	case 4:
		return a.CD.D
	}
	return a.CD.F
}

func posDados(a *Amostra, d map[string]any) map[string]any {
	if temPos(*a) {
		d["x"], d["y"] = arred(*a.X), arred(*a.Y)
	}
	return d
}

// geraHabilidade: "habilidade_usada" quando o ícone passa de pronto para em
// recarga (ou a recarga salta para cima, uso logo depois de voltar).
func geraHabilidade(c *ContextoCadeia) []ItemCadeia {
	var itens []ItemCadeia
	as := c.Amostras
	for h := 0; h < 6; h++ {
		// estado: 0 desconhecido, 1 pronta, 2 em recarga.
		estado := 0
		var ultimoT int64 = -1 << 40
		var ultimoV float64
		pend := -1 // índice da primeira leitura do candidato a uso
		pendN := 0
		for i := range as {
			a := &as[i]
			v := recargaDe(a, h)
			if v == nil || *v < 0 || a.Morto {
				if a.TMs-ultimoT > LacunaEsqueceMs {
					estado, pend, pendN = 0, -1, 0
				}
				continue
			}
			if a.TMs-ultimoT > LacunaEsqueceMs {
				estado, pend, pendN = 0, -1, 0
			}
			pronta := *v == 0
			switch {
			case pronta:
				pend, pendN = -1, 0
				estado = 1
			case estado == 1 || (estado == 2 && *v > ultimoV+SaltoRecargaS) || (pend >= 0 && pendN > 0):
				// Candidato: as leituras seguintes têm de descer (ou ficar) a partir dele.
				if pend < 0 {
					pend, pendN = i, 1
				} else if *v <= ultimoV+0.5 {
					pendN++
				} else {
					pend, pendN = i, 1
				}
				if pendN >= LeiturasConfirma {
					p := &as[pend]
					itens = append(itens, ItemCadeia{TMs: p.TMs, Tipo: "habilidade_usada", Dados: posDados(p, map[string]any{
						"habilidade": nomesHabilidade[h], "recarga_s": *recargaDe(p, h)})})
					estado, pend, pendN = 2, -1, 0
				}
			default:
				estado = 2
			}
			ultimoT, ultimoV = a.TMs, *v
		}
	}
	return itens
}

// Limiares da vida baixa (fração da vida máxima lida no painel).
const (
	LimiarVidaBaixa = 0.30
	LimiarRecuperou = 0.50
)

// geraVidaBaixa: "vida_baixa" ao cruzar para baixo de 30% (2 leituras seguidas)
// e "recuperou" ao voltar a 50% sem ter morrido no meio.
func geraVidaBaixa(c *ContextoCadeia) []ItemCadeia {
	var itens []ItemCadeia
	as := c.Amostras
	baixa := false
	var baixaEm int64
	conta, contaAlta := 0, 0
	var ultimoT int64 = -1 << 40
	for i := range as {
		a := &as[i]
		if a.Morto {
			baixa, conta, contaAlta = false, 0, 0
			continue
		}
		if a.Vida == nil || a.VidaMax == nil || *a.VidaMax <= 0 {
			continue
		}
		if a.TMs-ultimoT > LacunaEsqueceMs {
			conta, contaAlta = 0, 0
		}
		ultimoT = a.TMs
		f := *a.Vida / *a.VidaMax
		pct := math.Round(f*1000) / 10
		switch {
		case !baixa && f < LimiarVidaBaixa:
			conta++
			if conta >= LeiturasConfirma {
				baixa, baixaEm, conta = true, a.TMs, 0
				itens = append(itens, ItemCadeia{TMs: a.TMs, Tipo: "vida_baixa", Dados: posDados(a, map[string]any{
					"vida": *a.Vida, "vida_max": *a.VidaMax, "pct": pct})})
			}
		case baixa && f >= LimiarRecuperou:
			contaAlta++
			if contaAlta >= LeiturasConfirma {
				baixa, contaAlta = false, 0
				itens = append(itens, ItemCadeia{TMs: a.TMs, Tipo: "recuperou", Dados: posDados(a, map[string]any{
					"vida": *a.Vida, "vida_max": *a.VidaMax, "pct": pct, "duracao_ms": a.TMs - baixaEm})})
			}
		default:
			conta, contaAlta = 0, 0
		}
	}
	return itens
}
