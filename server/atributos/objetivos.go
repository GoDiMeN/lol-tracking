package atributos

import (
	"encoding/json"
	"math"
	"sort"

	"loltracking/server/rastreio"
)

// Medição do bônus dos dragões no PRÓPRIO dado: os atributos são lidos da tela
// ~4 vezes por segundo de jogo, então no instante em que o dragão cai o
// atributo de cada jogador do time dá um degrau, e o degrau é o bônus real
// daquele jogador naquele patch (sem depender de tabela de wiki).

// Tabela é o valor por acúmulo que a fonte dá (fração do TOTAL do atributo).
// Fonte: wiki.leagueoflegends.com/en-us/Dragon_Slayer (valores do V13.20, sem
// mudança até o 26.22; ver app/src/renderer/bonus.js). É o reserva quando a
// medição não fecha.
var Tabela = map[string]map[string]float64{
	"FIRE_DRAGON":    {"ad": 0.03, "ap": 0.03},
	"EARTH_DRAGON":   {"armadura": 0.05, "rm": 0.05},
	"AIR_DRAGON":     {"vel_mov": 0.05},
	"HEXTECH_DRAGON": {"vel_ataque": 0.05},
}

// Janelas da medição.
const (
	// JanelaAntesMs: até quanto antes da queda se procura o valor de antes.
	JanelaAntesMs = 15000
	// JanelaDepoisMs: até quanto depois se procura o primeiro trecho estável.
	JanelaDepoisMs = 20000
	// JanelaForaDeLutaMs: a velocidade das nuvens só vale fora de combate; o
	// primeiro trecho fora de luta pode demorar (o time luta pelo dragão).
	JanelaForaDeLutaMs = 90000
	// EstavelMs / EstavelAmostras: um trecho com o mesmo valor por pelo menos
	// isso conta como leitura firme (buffs de luta piscam por menos).
	EstavelMs       = 1500
	EstavelAmostras = 4
	// MargemQuedaMs: amostras coladas no carimbo do evento ficam de fora (o
	// carimbo da linha do tempo e o instante da tela diferem por fração de s).
	MargemQuedaMs = 300
)

// Degrau é a medição de um atributo de um jogador num dragão.
type Degrau struct {
	ParticipanteID int     `json:"participante_id"`
	Atributo       string  `json:"atributo"`
	Antes          float64 `json:"antes"`
	Depois         float64 `json:"depois"`
	Degrau         float64 `json:"degrau"`
	// Percentual: depois/antes − 1 (para a velocidade de movimento, antes do
	// teto suave do jogo). PorAcumulo: o mesmo convertido para "fração do total
	// por dragão" levando em conta os dragões do mesmo tipo que o time já tinha.
	Percentual float64 `json:"percentual"`
	PorAcumulo float64 `json:"por_acumulo"`
	// Faixa que a tela permite: os números saem arredondados (±0,5).
	PorAcumuloMin float64 `json:"por_acumulo_min"`
	PorAcumuloMax float64 `json:"por_acumulo_max"`
	AntesDeMs     int64   `json:"antes_de_ms"`
	AntesAteMs    int64   `json:"antes_ate_ms"`
	DepoisDeMs    int64   `json:"depois_de_ms"`
	DepoisAteMs   int64   `json:"depois_ate_ms"`
	// Confianca: "alta" (trechos firmes dos dois lados), "media" (um lado curto)
	// ou "nenhuma" (não deu para medir: Nota diz por quê).
	Confianca string `json:"confianca"`
	Nota      string `json:"nota,omitempty"`
}

// Queda é um dragão do time com as medições.
type Queda struct {
	TMs      int64    `json:"t_ms"`
	Sub      string   `json:"sub"`
	Time     int      `json:"time"`
	Acumulo  int      `json:"acumulo"`
	Degraus  []Degrau `json:"degraus"`
	Medidos  int      `json:"medidos"`
	Previsto []string `json:"atributos"`
}

// Resumo junta as quedas do mesmo tipo: o valor por acúmulo que a tela usa.
type Resumo struct {
	Sub      string  `json:"sub"`
	Atributo string  `json:"atributo"`
	Tabela   float64 `json:"tabela"`
	// Medido: nulo quando nenhuma medição foi confiável.
	Medido *float64 `json:"medido"`
	Min    *float64 `json:"min"`
	Max    *float64 `json:"max"`
	N      int      `json:"n"`
	// Confianca: "alta" (as faixas de todos os jogadores se cruzam), "media"
	// (não se cruzam: fica a mediana) ou "nenhuma".
	Confianca string `json:"confianca"`
	// Usado: o que a tela usa (o medido quando há; senão a tabela).
	Usado float64 `json:"usado"`
	Fonte string  `json:"fonte"`
}

// QuedaDeDragao é um dragão elemental na linha do tempo.
type QuedaDeDragao struct {
	TMs  int64
	Sub  string
	Time int
}

// DragoesDaLinha tira os dragões elementais da linha do tempo, com o time de
// quem matou (ou de quem ajudou: o killerId vem 0 às vezes, como nas nuvens da
// 3304217746).
func DragoesDaLinha(lt rastreio.LinhaDoTempo, timeDe map[int]int) []QuedaDeDragao {
	var r []QuedaDeDragao
	for _, e := range lt.Eventos {
		if e.Tipo != "ELITE_MONSTER_KILL" {
			continue
		}
		var b struct {
			MonsterType    string `json:"monsterType"`
			MonsterSubType string `json:"monsterSubType"`
			KillerTeamID   int    `json:"killerTeamId"`
		}
		if json.Unmarshal(e.Bruto, &b) != nil || b.MonsterType != "DRAGON" || b.MonsterSubType == "ELDER_DRAGON" {
			continue
		}
		time := timeDe[e.Quem]
		for _, a := range e.Assistencias {
			if time == 0 {
				time = timeDe[a]
			}
		}
		if time == 0 && (b.KillerTeamID == 100 || b.KillerTeamID == 200) {
			time = b.KillerTeamID
		}
		if time == 0 {
			continue
		}
		r = append(r, QuedaDeDragao{TMs: e.TMs, Sub: b.MonsterSubType, Time: time})
	}
	return r
}

// valorDe lê o atributo da amostra.
func valorDe(a rastreio.Amostra, atr string) *float64 {
	switch atr {
	case "ad":
		return a.Atr.AD
	case "ap":
		return a.Atr.AP
	case "armadura":
		return a.Atr.Armadura
	case "rm":
		return a.Atr.RM
	case "vel_ataque":
		return a.Atr.VelAtaque
	case "vel_mov":
		return a.Atr.VelMov
	}
	return nil
}

// trecho é uma sequência de amostras seguidas com o mesmo valor.
type trecho struct {
	v        float64
	de, ate  int64
	amostras int
}

func (t trecho) firme() bool { return t.ate-t.de >= EstavelMs && t.amostras >= EstavelAmostras }

// trechos agrupa as amostras (já filtradas, em ordem) em trechos de valor igual.
// Um buraco de mais de 1,5 s entre amostras quebra o trecho.
func trechos(as []rastreio.Amostra, atr string) []trecho {
	var r []trecho
	for _, a := range as {
		p := valorDe(a, atr)
		if p == nil {
			continue
		}
		if n := len(r); n > 0 && r[n-1].v == *p && a.TMs-r[n-1].ate <= 1500 {
			r[n-1].ate = a.TMs
			r[n-1].amostras++
			continue
		}
		r = append(r, trecho{v: *p, de: a.TMs, ate: a.TMs, amostras: 1})
	}
	return r
}

// Velocidade de movimento: o jogo aplica um teto suave (wiki.leagueoflegends.com/
// en-us/Movement_speed): da velocidade bruta, acima de 415 só conta 80% do que
// passa; acima de 490, 50% (bruto 490 = 475 na tela). Para comparar o percentual
// com a tabela, desfaz-se o teto.
func semTeto(v float64) float64 {
	switch {
	case v <= 415:
		return v
	case v <= 475:
		return 415 + (v-415)/0.8
	default:
		return 490 + (v-475)/0.5
	}
}

// ComTeto aplica o teto suave.
func ComTeto(v float64) float64 {
	switch {
	case v <= 415:
		return v
	case v <= 490:
		return 415 + (v-415)*0.8
	default:
		return 415 + 75*0.8 + (v-490)*0.5
	}
}

// porAcumulo converte a razão depois/antes no valor por dragão: com k dragões
// do tipo depois da queda e bônus somado (1 + k·p sobre a base sem dragão), a
// razão é (1+k·p)/(1+(k−1)·p) → p = (r−1)/(k − r·(k−1)).
func porAcumulo(r float64, k int) float64 {
	d := float64(k) - r*float64(k-1)
	if d <= 0 {
		return math.NaN()
	}
	return (r - 1) / d
}

// mudancas são os instantes em que o inventário do jogador mudou (compra,
// venda, consumo), pelo estado dos 10.
func mudancas(es []rastreio.EstadoJogador) []int64 {
	var r []int64
	var ant map[int]int
	for _, e := range es {
		agora := map[int]int{}
		for _, s := range e.Slots {
			if s.ID != 0 {
				agora[s.ID] += max(1, s.Qtd)
			}
		}
		if ant != nil && !mesmoInventario(ant, agora) {
			r = append(r, e.TMs)
		}
		ant = agora
	}
	return r
}

func mesmoInventario(a, b map[int]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// MedeQueda mede o degrau de cada atributo do dragão em cada jogador do time.
// amostras/estados por participante; acumulo = quantos do tipo o time tem
// depois desta queda.
func MedeQueda(q QuedaDeDragao, acumulo int, jogadores []int, amostras map[int][]rastreio.Amostra,
	estados map[int][]rastreio.EstadoJogador) Queda {
	r := Queda{TMs: q.TMs, Sub: q.Sub, Time: q.Time, Acumulo: acumulo, Degraus: []Degrau{}, Previsto: []string{}}
	tab := Tabela[q.Sub]
	for atr := range tab {
		r.Previsto = append(r.Previsto, atr)
	}
	sort.Strings(r.Previsto)
	for _, pid := range jogadores {
		for _, atr := range r.Previsto {
			d := medeUm(q, acumulo, pid, atr, amostras[pid], estados[pid])
			if d.Confianca != "nenhuma" {
				r.Medidos++
			}
			r.Degraus = append(r.Degraus, d)
		}
	}
	return r
}

func medeUm(q QuedaDeDragao, acumulo, pid int, atr string, as []rastreio.Amostra, es []rastreio.EstadoJogador) Degrau {
	d := Degrau{ParticipanteID: pid, Atributo: atr, Confianca: "nenhuma"}
	if len(as) == 0 {
		d.Nota = "jogador não rastreado"
		return d
	}
	movimento := atr == "vel_mov"
	ini := q.TMs - JanelaAntesMs
	fim := q.TMs + JanelaDepoisMs
	if movimento {
		fim = q.TMs + JanelaForaDeLutaMs
	}
	// Compra ou venda corta a janela (o item muda o atributo); colada na queda,
	// não dá para separar.
	for _, m := range mudancas(es) {
		if m <= q.TMs-MargemQuedaMs && m > ini {
			ini = m
		}
		if m >= q.TMs+MargemQuedaMs && m < fim {
			fim = m
		}
		if m > q.TMs-MargemQuedaMs && m < q.TMs+MargemQuedaMs {
			d.Nota = "compra ou venda de item no instante da queda"
			return d
		}
	}
	// Nível novo também muda armadura, RM, DA e velocidade de ataque (não a de
	// movimento): o nível de antes vale para os dois lados.
	nivel := 0
	var antes, depois []rastreio.Amostra
	for _, a := range as {
		if a.TMs < ini || a.TMs > fim || a.Morto {
			continue
		}
		if a.TMs <= q.TMs-MargemQuedaMs {
			antes = append(antes, a)
			nivel = a.Nivel
		}
	}
	for _, a := range as {
		if a.TMs < q.TMs+MargemQuedaMs || a.TMs > fim {
			continue
		}
		if a.Morto {
			// Morto não perde os dragões, mas a leitura some; o trecho segue depois.
			continue
		}
		if !movimento && nivel > 0 && a.Nivel != nivel {
			break
		}
		depois = append(depois, a)
	}
	ta := trechos(antes, atr)
	td := trechos(depois, atr)
	if len(ta) == 0 || len(td) == 0 {
		d.Nota = "sem leitura do atributo perto da queda"
		return d
	}
	// Antes: o último trecho firme (o valor sem luta logo antes da queda); sem
	// nenhum firme, o mais longo.
	var a *trecho
	for i := len(ta) - 1; i >= 0; i-- {
		if ta[i].firme() {
			a = &ta[i]
			break
		}
	}
	firmeAntes := a != nil
	if a == nil {
		a = &ta[0]
		for i := range ta {
			if ta[i].ate-ta[i].de > a.ate-a.de {
				a = &ta[i]
			}
		}
	}
	if a.v <= 0 {
		d.Antes = a.v
		d.Nota = "atributo zerado (o campeão não tem)"
		return d
	}
	// Depois: o primeiro trecho firme ACIMA do valor de antes e até 3 vezes o
	// bônus da tabela (redução de armadura e lentidão ficam abaixo; buffs de luta
	// e fantasma, bem acima). A velocidade das nuvens só aparece fora de luta: o
	// primeiro trecho firme acima de antes é o primeiro fora de luta.
	teto := a.v * (1 + 3*Tabela[q.Sub][atr]*float64(acumulo))
	if movimento {
		teto = ComTeto(semTeto(a.v) * (1 + 3*Tabela[q.Sub][atr]))
	}
	var b *trecho
	for i := range td {
		if td[i].v > a.v && td[i].v <= teto+0.5 && td[i].firme() {
			b = &td[i]
			break
		}
	}
	firmeDepois := b != nil
	if b == nil {
		// Sem trecho firme: o trecho (curto) mais comprido na faixa.
		for i := range td {
			if td[i].v > a.v && td[i].v <= teto+0.5 && td[i].amostras >= 2 && (b == nil || td[i].ate-td[i].de > b.ate-b.de) {
				b = &td[i]
			}
		}
	}
	d.Antes, d.AntesDeMs, d.AntesAteMs = a.v, a.de, a.ate
	if b == nil {
		if movimento {
			d.Nota = "nenhum trecho fora de luta com velocidade maior na janela"
		} else {
			d.Nota = "o valor não subiu num trecho firme depois da queda"
		}
		return d
	}
	d.Depois, d.DepoisDeMs, d.DepoisAteMs = b.v, b.de, b.ate
	d.Degrau = b.v - a.v
	va, vd := a.v, b.v
	// Faixa do arredondamento da tela (±0,5; ±0,005 na velocidade de ataque).
	meio := 0.5
	if atr == "vel_ataque" {
		meio = 0.005
	}
	conv := func(x float64) float64 { return x }
	if movimento {
		conv = semTeto
	}
	r := conv(vd) / conv(va)
	rMin := conv(vd-meio) / conv(va+meio)
	rMax := conv(vd+meio) / conv(va-meio)
	d.Percentual = arred4(r - 1)
	d.PorAcumulo = arred4(porAcumulo(r, acumulo))
	d.PorAcumuloMin = arred4(porAcumulo(rMin, acumulo))
	d.PorAcumuloMax = arred4(porAcumulo(rMax, acumulo))
	switch {
	case firmeAntes && firmeDepois:
		d.Confianca = "alta"
	default:
		d.Confianca = "media"
		d.Nota = "trecho curto de um dos lados"
	}
	return d
}

func arred4(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return math.Round(v*10000) / 10000
}

// Resume junta as medições de todas as quedas por (tipo, atributo). Com as
// faixas de todos os jogadores se cruzando, o valor é o meio do cruzamento
// (confiança alta); senão, a mediana (média). Só entra medição alta ou média.
func Resume(quedas []Queda) []Resumo {
	type chave struct{ sub, atr string }
	grupos := map[chave][]Degrau{}
	for _, q := range quedas {
		for _, d := range q.Degraus {
			k := chave{q.Sub, d.Atributo}
			if _, ok := grupos[k]; !ok {
				grupos[k] = nil
			}
			if d.Confianca == "alta" || d.Confianca == "media" {
				grupos[k] = append(grupos[k], d)
			}
		}
	}
	var r []Resumo
	for k, ds := range grupos {
		res := Resumo{Sub: k.sub, Atributo: k.atr, Tabela: Tabela[k.sub][k.atr], N: len(ds), Confianca: "nenhuma",
			Usado: Tabela[k.sub][k.atr], Fonte: "tabela"}
		if len(ds) > 0 {
			lo, hi := math.Inf(-1), math.Inf(1)
			var pts []float64
			for _, d := range ds {
				lo = math.Max(lo, d.PorAcumuloMin)
				hi = math.Min(hi, d.PorAcumuloMax)
				pts = append(pts, d.PorAcumulo)
			}
			sort.Float64s(pts)
			var v float64
			if lo <= hi {
				v = (lo + hi) / 2
				res.Confianca = "alta"
				l, h := arred4(lo), arred4(hi)
				res.Min, res.Max = &l, &h
			} else {
				v = pts[len(pts)/2]
				if len(pts)%2 == 0 {
					v = (pts[len(pts)/2-1] + pts[len(pts)/2]) / 2
				}
				res.Confianca = "media"
				l, h := pts[0], pts[len(pts)-1]
				res.Min, res.Max = &l, &h
			}
			v = arred4(v)
			res.Medido = &v
			res.Usado = v
			res.Fonte = "medido"
		}
		r = append(r, res)
	}
	sort.Slice(r, func(a, b int) bool {
		if r[a].Sub != r[b].Sub {
			return r[a].Sub < r[b].Sub
		}
		return r[a].Atributo < r[b].Atributo
	})
	return r
}
