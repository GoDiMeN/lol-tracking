package ranking

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// A FÓRMULA. Pura: recebe os números de cada jogador (liga + partidas do
// patch) e devolve as posições com a nota, cada componente (número cru,
// valor de 0 a 1, peso e contribuição) e a frase. Os pesos e limiares estão em
// parametros.go.

// Entrada é o que a league-v4 diz do jogador.
type Entrada struct {
	Puuid    string `json:"puuid"`
	Divisao  string `json:"divisao"` // I–IV; vazio de Mestre para cima
	LP       int    `json:"lp"`
	Vitorias int    `json:"vitorias"`
	Derrotas int    `json:"derrotas"`
	Hot      bool   `json:"hot_streak"`
	Fresh    bool   `json:"fresh_blood"`
	Veteran  bool   `json:"veteran"`
	Inativo  bool   `json:"inactive"`
}

// PartidaDoJogador é uma ranqueada solo do jogador no patch (já filtrada:
// fila 420, patch pedido, sem remake), da mais nova para a mais velha.
type PartidaDoJogador struct {
	MatchID   string `json:"match_id"`
	CriadaMs  int64  `json:"criada_ms"`
	Posicao   string `json:"posicao"`
	CampeaoID int    `json:"campeao_id"`
	Venceu    bool   `json:"venceu"`
}

// Jogador é um candidato com o histórico lido.
type Jogador struct {
	Entrada
	Nome     string
	Tag      string
	Partidas []PartidaDoJogador
}

// Recorte: a rota (TipoRota) ou o campeão (TipoCampeao).
type Recorte struct {
	Tipo      string `json:"tipo"`
	Rota      string `json:"rota"`
	CampeaoID int    `json:"campeao_id"`
}

func (r Recorte) dentro(p PartidaDoJogador) bool {
	if r.Tipo == TipoCampeao {
		return p.CampeaoID == r.CampeaoID
	}
	return p.Posicao == r.Rota
}

// Contexto da liga lida: no ápice, a posição na liga é PDL ÷ o maior PDL.
type Contexto struct {
	Apex  bool
	LPMax int
}

// Numeros: os números crus de uma posição (vão para o banco e para a tela).
type Numeros struct {
	LP              int     `json:"lp"`
	Divisao         string  `json:"divisao"`
	Vitorias        int     `json:"vitorias"`
	Derrotas        int     `json:"derrotas"`
	TaxaTemporada   float64 `json:"taxa_temporada"`
	PartidasPatch   int     `json:"partidas_patch"`
	PartidasRecorte int     `json:"partidas_recorte"`
	VitoriasRecorte int     `json:"vitorias_recorte"`
	FracaoRecorte   float64 `json:"fracao_recorte"`
	// VitoriasRecentes nas RecentesK mais recentes do recorte.
	VitoriasRecentes int  `json:"vitorias_recentes"`
	Hot              bool `json:"hot_streak"`
	Fresh            bool `json:"fresh_blood"`
	Veteran          bool `json:"veteran"`
	// CampeaoID: no ranking de rota, o campeão mais jogado na rota no patch;
	// Rota: no ranking de campeão, a rota mais jogada com ele.
	CampeaoID int    `json:"campeao_id"`
	Rota      string `json:"rota"`
}

// Componente é uma parcela da nota.
type Componente struct {
	Nome   string `json:"nome"`
	Rotulo string `json:"rotulo"`
	// Cru: o número de onde saiu o valor, em texto ("46 de 79", "Plat II 45 PDL").
	Cru          string  `json:"cru"`
	Valor        float64 `json:"valor"` // 0–1
	Peso         float64 `json:"peso"`
	Contribuicao float64 `json:"contribuicao"` // 100 × peso × valor
}

// Posicao é um jogador no ranking.
type Posicao struct {
	Posicao        int          `json:"posicao"`
	Puuid          string       `json:"puuid"`
	Nome           string       `json:"nome"`
	Tag            string       `json:"tag"`
	Nota           float64      `json:"nota"`
	Empatado       bool         `json:"empatado"`
	AmostraPequena bool         `json:"amostra_pequena"`
	Frase          string       `json:"frase"`
	Componentes    []Componente `json:"componentes"`
	Numeros        Numeros      `json:"numeros"`
	// Campeao: o nome do campeão de Numeros.CampeaoID (preenchido na leitura).
	Campeao string `json:"campeao"`
}

// Motivos de ficar de fora de um ranking (contados no cabeçalho).
const (
	ForaInativo      = "inativo"
	ForaSemPatch     = "sem_partida_no_patch"
	ForaSemRecorte   = "sem_o_recorte"
	ForaPoucas       = "poucas_no_recorte"
	ForaNaoAchado    = "nao_achado"
	ForaSemHistorico = "sem_historico"
)

// Wilson: limite inferior do intervalo de Wilson da taxa de vitórias (z =
// ZWilson). 0 sem partidas.
func Wilson(vitorias, partidas int) float64 {
	if partidas <= 0 {
		return 0
	}
	n := float64(partidas)
	p := float64(vitorias) / n
	z2 := ZWilson * ZWilson
	return (p + z2/(2*n) - ZWilson*math.Sqrt(p*(1-p)/n+z2/(4*n*n))) / (1 + z2/n)
}

// escalaWilson põe o Wilson na escala WilsonPiso..WilsonTeto → 0..1.
func escalaWilson(w float64) float64 {
	return limita((w - WilsonPiso) / (WilsonTeto - WilsonPiso))
}

func limita(v float64) float64 {
	return math.Max(0, math.Min(1, v))
}

// indiceDivisao: IV = 0 ... I = 3.
func indiceDivisao(d string) int {
	switch d {
	case "I":
		return 3
	case "II":
		return 2
	case "III":
		return 1
	}
	return 0
}

// ValorLiga: o componente "posição na liga" (ver CompLiga).
func ValorLiga(e Entrada, cx Contexto) float64 {
	if cx.Apex {
		if cx.LPMax <= 0 {
			return 0
		}
		return limita(float64(e.LP) / float64(cx.LPMax))
	}
	return limita(float64(indiceDivisao(e.Divisao)*100+min(max(e.LP, 0), 100)) / 400)
}

// Elegivel diz se o jogador entra no ranking do recorte e, se não, por quê.
func Elegivel(j Jogador, r Recorte) (bool, string) {
	if j.Inativo {
		return false, ForaInativo
	}
	if len(j.Partidas) == 0 {
		return false, ForaSemPatch
	}
	n := 0
	for _, p := range j.Partidas {
		if r.dentro(p) {
			n++
		}
	}
	switch {
	case n == 0:
		return false, ForaSemRecorte
	case n < MinPartidasRecorte:
		return false, ForaPoucas
	}
	return true, ""
}

// numerosDe junta os números crus do jogador no recorte.
func numerosDe(j Jogador, r Recorte) Numeros {
	n := Numeros{LP: j.LP, Divisao: j.Divisao, Vitorias: j.Vitorias, Derrotas: j.Derrotas, PartidasPatch: len(j.Partidas),
		Hot: j.Hot, Fresh: j.Fresh, Veteran: j.Veteran}
	if t := j.Vitorias + j.Derrotas; t > 0 {
		n.TaxaTemporada = arredonda(float64(j.Vitorias)/float64(t), 3)
	}
	campeoes, rotas := map[int]int{}, map[string]int{}
	for _, p := range j.Partidas {
		if !r.dentro(p) {
			continue
		}
		if n.PartidasRecorte < RecentesK && p.Venceu {
			n.VitoriasRecentes++
		}
		n.PartidasRecorte++
		if p.Venceu {
			n.VitoriasRecorte++
		}
		campeoes[p.CampeaoID]++
		if p.Posicao != "" {
			rotas[p.Posicao]++
		}
	}
	if n.PartidasPatch > 0 {
		n.FracaoRecorte = arredonda(float64(n.PartidasRecorte)/float64(n.PartidasPatch), 3)
	}
	if r.Tipo == TipoCampeao {
		n.CampeaoID = r.CampeaoID
		n.Rota = maisFrequente(rotas)
	} else {
		n.Rota = r.Rota
		n.CampeaoID = maisFrequenteInt(campeoes)
	}
	return n
}

func maisFrequente(m map[string]int) string {
	melhor, qual := 0, ""
	for k, v := range m {
		if v > melhor || (v == melhor && k < qual) {
			qual, melhor = k, v
		}
	}
	return qual
}

func maisFrequenteInt(m map[int]int) int {
	melhor, qual := 0, 0
	for k, v := range m {
		if v > melhor || (v == melhor && k < qual) {
			qual, melhor = k, v
		}
	}
	return qual
}

func arredonda(v float64, casas int) float64 {
	f := math.Pow(10, float64(casas))
	return math.Round(v*f) / f
}

// componentes calcula o valor (0–1) e o texto cru de cada componente.
func componentes(n Numeros, e Entrada, cx Contexto) map[string]Componente {
	pct := func(a, b int) string {
		if b == 0 {
			return "0 de 0"
		}
		return fmt.Sprintf("%d de %d (%.0f%%)", a, b, 100*float64(a)/float64(b))
	}
	liga := fmt.Sprintf("%s %d PDL", e.Divisao, e.LP)
	if cx.Apex {
		liga = fmt.Sprintf("%d PDL (maior da liga: %d)", e.LP, cx.LPMax)
	}
	sinais := 0.0
	var ss []string
	if e.Hot {
		sinais += 0.5
		ss = append(ss, "sequência de vitórias")
	}
	if e.Fresh {
		sinais += 0.5
		ss = append(ss, "recém-chegado")
	}
	if len(ss) == 0 {
		ss = append(ss, "nenhum")
	}
	vet := 0.0
	vetTx := "não"
	if e.Veteran {
		vet, vetTx = 1, "sim"
	}
	temp := e.Vitorias + e.Derrotas
	wt, wr := Wilson(e.Vitorias, temp), Wilson(n.VitoriasRecorte, n.PartidasRecorte)
	m := map[string]Componente{
		CompLiga:         {Cru: strings.TrimSpace(liga), Valor: ValorLiga(e, cx)},
		CompVitTemporada: {Cru: fmt.Sprintf("%s; Wilson %.3f", pct(e.Vitorias, temp), wt), Valor: escalaWilson(wt)},
		CompVitRecorte:   {Cru: fmt.Sprintf("%s; Wilson %.3f", pct(n.VitoriasRecorte, n.PartidasRecorte), wr), Valor: escalaWilson(wr)},
		CompVolTemporada: {Cru: fmt.Sprintf("%d partidas", temp), Valor: limita(float64(temp) / VolumeTemporadaRef)},
		CompVolRecorte:   {Cru: fmt.Sprintf("%d partidas", n.PartidasRecorte), Valor: limita(float64(n.PartidasRecorte) / VolumeRecorteRef)},
		CompFracao:       {Cru: pct(n.PartidasRecorte, n.PartidasPatch), Valor: n.FracaoRecorte},
		CompSinais:       {Cru: strings.Join(ss, " e "), Valor: sinais},
		CompRecentes:     {Cru: fmt.Sprintf("%d nas %d mais recentes", n.VitoriasRecentes, RecentesK), Valor: float64(n.VitoriasRecentes) / RecentesK},
		CompVeterano:     {Cru: vetTx, Valor: vet},
	}
	return m
}

// Nota calcula a nota e os componentes de um jogador num tipo e modo.
func Nota(tipo, modo string, n Numeros, e Entrada, cx Contexto) (float64, []Componente) {
	valores := componentes(n, e, cx)
	var l []Componente
	total := 0.0
	for _, p := range Pesos[tipo][modo] {
		c := valores[p.Componente]
		c.Nome, c.Rotulo, c.Peso = p.Componente, NomeComponente[p.Componente], p.Peso
		c.Valor = arredonda(c.Valor, 4)
		c.Contribuicao = arredonda(100*p.Peso*c.Valor, 2)
		total += 100 * p.Peso * c.Valor
		l = append(l, c)
	}
	return arredonda(total, CasasNota), l
}

// Calcula monta o ranking de um recorte num modo: só os elegíveis, na ordem da
// nota, com empates marcados e a frase de cada um. fora conta quem ficou de
// fora e por quê.
func Calcula(modo string, r Recorte, js []Jogador, cx Contexto, nomeRecorte string) ([]Posicao, map[string]int) {
	fora := map[string]int{}
	var l []Posicao
	for _, j := range js {
		ok, motivo := Elegivel(j, r)
		if !ok {
			fora[motivo]++
			continue
		}
		n := numerosDe(j, r)
		nota, comps := Nota(r.Tipo, modo, n, j.Entrada, cx)
		l = append(l, Posicao{Puuid: j.Puuid, Nome: j.Nome, Tag: j.Tag, Nota: nota, Componentes: comps, Numeros: n,
			AmostraPequena: n.PartidasRecorte < AmostraPequena})
	}
	sort.SliceStable(l, func(a, b int) bool {
		x, y := l[a], l[b]
		if x.Nota != y.Nota {
			return x.Nota > y.Nota
		}
		if x.Numeros.PartidasRecorte != y.Numeros.PartidasRecorte {
			return x.Numeros.PartidasRecorte > y.Numeros.PartidasRecorte
		}
		tx, ty := x.Numeros.Vitorias+x.Numeros.Derrotas, y.Numeros.Vitorias+y.Numeros.Derrotas
		if tx != ty {
			return tx > ty
		}
		if x.Numeros.LP != y.Numeros.LP {
			return x.Numeros.LP > y.Numeros.LP
		}
		return x.Puuid < y.Puuid
	})
	for i := range l {
		l[i].Posicao = i + 1
		empateCom := 0
		if i > 0 && l[i-1].Nota == l[i].Nota {
			empateCom = i
		} else if i+1 < len(l) && l[i+1].Nota == l[i].Nota {
			empateCom = i + 2
		}
		l[i].Empatado = empateCom > 0
		l[i].Frase = Frase(r, l[i].Numeros, nomeRecorte, empateCom)
	}
	return l, fora
}

// Frase: "58% de vitórias em 79 partidas na temporada; 12 partidas na selva no
// patch (81% das dele), 8 vitórias; em sequência de vitórias".
func Frase(r Recorte, n Numeros, nomeRecorte string, empateCom int) string {
	var ps []string
	if t := n.Vitorias + n.Derrotas; t > 0 {
		ps = append(ps, fmt.Sprintf("%.0f%% de vitórias em %d partidas na temporada", 100*float64(n.Vitorias)/float64(t), t))
	}
	onde := "na " + nomeRecorte
	if r.Tipo == TipoCampeao {
		onde = "com " + nomeRecorte
	}
	ps = append(ps, fmt.Sprintf("%d partidas %s no patch (%.0f%% das partidas dele), %d vitórias", n.PartidasRecorte, onde,
		100*n.FracaoRecorte, n.VitoriasRecorte))
	if n.Hot {
		ps = append(ps, "em sequência de vitórias")
	}
	if n.Fresh {
		ps = append(ps, "recém-chegado ao elo")
	}
	if n.Veteran {
		ps = append(ps, "há muito tempo nesta liga")
	}
	if n.PartidasRecorte < AmostraPequena {
		ps = append(ps, fmt.Sprintf("amostra pequena (menos de %d partidas)", AmostraPequena))
	}
	if empateCom > 0 {
		ps = append(ps, fmt.Sprintf("empatado na nota com o %dº (desempate: mais partidas no recorte, depois na temporada, depois PDL)", empateCom))
	}
	return strings.Join(ps, "; ")
}

// PreNota é a ordem dos candidatos ANTES de ler o histórico (só com a liga):
// "melhor" = a posição na liga; "subindo" = Wilson da temporada na escala +
// sinais − veterano. A atualização lê o histórico dos primeiros de cada uma,
// intercalados (ver escolheCandidatos).
func PreNota(modo string, e Entrada, cx Contexto) float64 {
	if modo == ModoMelhor {
		return ValorLiga(e, cx)
	}
	v := escalaWilson(Wilson(e.Vitorias, e.Vitorias+e.Derrotas))
	if e.Hot {
		v += 0.5
	}
	if e.Fresh {
		v += 0.5
	}
	if e.Veteran {
		v -= 1
	}
	return v
}
