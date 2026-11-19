package aprendizado

import (
	"fmt"
	"strings"

	"loltracking/server/banco"
)

// COTA POR ROTA. O estudo precisa do melhor de cada rota em cada elo (e dos
// melhores Desafiantes de cada rota), não de "os N primeiros do elo": numa
// lista ordenada só pela subida a selva e o meio enchem a vaga e o suporte
// quase não aparece. Com "funcoes" (lista; vazia = as cinco) e
// "jogadores_por_funcao", cada elo tem uma cota por rota; a descoberta desce
// a lista do elo (na ordem de sempre: a pontuação de subida, ou a amostragem)
// e cada candidato entra na rota da FUNÇÃO PRINCIPAL dele se ela ainda tem
// vaga. Quem é de rota já cheia é descartado como "rota_completa"; quem é de
// rota não pedida, "outra_funcao". O primeiro aceito de cada rota é, então, o
// candidato de maior pontuação de subida daquela rota entre os conferidos.

// FuncoesTodas: as cinco, na ordem da tela.
var FuncoesTodas = []string{"TOP", "JUNGLE", "MIDDLE", "BOTTOM", "UTILITY"}

// Regras da cota por rota.
const (
	// JogadoresPorFuncaoPadrao: com "funcoes" e sem "jogadores_por_funcao".
	JogadoresPorFuncaoPadrao = 2
	// MaxJogadoresPorFuncao: 5 rotas × 4 = 20 = MaxJogadoresPorElo.
	MaxJogadoresPorFuncao = 4
	// MaxCandidatosPorVaga: com cota, o teto de candidatos olhados no elo é
	// max(MaxCandidatosPorElo, MaxCandidatosPorVaga × vagas): achar 2 suportes
	// entre os do topo da lista pede descer mais que achar 10 quaisquer.
	MaxCandidatosPorVaga = 10
	// DescRotaCompleta: a função principal do candidato foi pedida, mas a cota
	// dela no elo já encheu.
	DescRotaCompleta = "rota_completa"
)

// cotas: função → alvo no elo; nil = sem cota (qualquer função até
// jogadores_por_elo).
func (c Coleta) cotas() map[string]int {
	if len(c.Funcoes) == 0 {
		return nil
	}
	m := map[string]int{}
	for _, f := range c.Funcoes {
		m[f] = c.JogadoresPorFuncao
	}
	return m
}

// limiteCandidatos: quantos candidatos a descoberta normal olha no elo.
func (c Coleta) limiteCandidatos() int {
	if c.cotas() == nil {
		return MaxCandidatosPorElo
	}
	return max(MaxCandidatosPorElo, MaxCandidatosPorVaga*c.JogadoresPorElo)
}

// vaga diz se cabe mais um jogador desta função no elo: "" = cabe; senão o
// descarte (outra_funcao, rota_completa).
func vaga(db *banco.DB, col Coleta, elo, funcao string) string {
	cont, _, err := contaPorFuncao(db, col.ID, elo)
	if err != nil {
		return DescRotaCompleta
	}
	cotas := col.cotas()
	if cotas == nil {
		total := 0
		for _, n := range cont {
			total += n
		}
		if total >= col.JogadoresPorElo {
			return DescRotaCompleta
		}
		return ""
	}
	alvo, ok := cotas[funcao]
	switch {
	case !ok || funcao == "":
		return DescOutraFuncao
	case cont[funcao] >= alvo:
		return DescRotaCompleta
	}
	return ""
}

// achadosNoElo: quantos jogadores o elo já tem (descobertos e aproveitados).
func achadosNoElo(db *banco.DB, coletaID int64, elo string) int {
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM aprendizado_jogadores WHERE coleta_id = ? AND elo = ?`, coletaID, elo).Scan(&n)
	return n
}

// avisoCotas: "topo 2/2, selva 1/2, ..." (as rotas que não encheram primeiro).
func avisoCotas(db *banco.DB, col Coleta, elo string) string {
	cotas := col.cotas()
	if cotas == nil {
		return ""
	}
	cont, _, _ := contaPorFuncao(db, col.ID, elo)
	var falta []string
	for _, f := range col.Funcoes {
		if cont[f] < cotas[f] {
			falta = append(falta, fmt.Sprintf("%s %d/%d", nomeFuncao[f], cont[f], cotas[f]))
		}
	}
	if len(falta) == 0 {
		return ""
	}
	return "rotas sem a cota cheia: " + strings.Join(falta, ", ")
}

// funcaoDasPartidas: em que função escolher as partidas do candidato. Com
// cota, a principal dele (é nela que ele conta); sem cota, a pedida (ou
// qualquer).
func funcaoDasPartidas(col Coleta, principal string) string {
	if col.cotas() != nil {
		return principal
	}
	return col.Funcao
}
