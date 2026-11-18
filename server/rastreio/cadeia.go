package rastreio

import (
	"fmt"
	"math"
	"sort"
	"sync"

	"loltracking/server/banco"
	"loltracking/server/partida"
)

// A cadeia reduzida: o que importa para contar o jogo de um jogador, não cada
// pontinho. É gerada do bloco compactado por GERADORES registráveis: a tarefa
// 4 acrescenta tipos (zona, saída de rota, habilidade, atividade) registrando
// um gerador e chamando RegeraCadeia nos passes já gravados. Vocabulário dos
// tipos: docs/arquitetura/dados.md.

// ItemCadeia é um elo da cadeia.
type ItemCadeia struct {
	TMs   int64          `json:"t_ms"`
	Tipo  string         `json:"tipo"`
	Dados map[string]any `json:"dados"`
}

// ContextoCadeia é o que um gerador recebe.
type ContextoCadeia struct {
	GameID         int64
	ParticipanteID int
	// Time: 100 (ORDER) ou 200 (CHAOS).
	Time int
	// Amostras do jogador seguido, em ordem de tempo.
	Amostras []Amostra
	// Estados dos 10 a cada ~1 s.
	Estados []EstadoJogador
	Linha   LinhaDoTempo
	// ToleranciaTrajeto é a do Douglas-Peucker, em unidades do mapa.
	ToleranciaTrajeto float64

	// O resto vem de completaContexto (contexto.go); pode vir vazio (testes).
	// Posicao do jogador na partida: TOP JUNGLE MIDDLE BOTTOM UTILITY.
	Posicao string
	// Times e Posicoes dos 10 (participante → 100/200; → posição).
	Times    map[int]int
	Posicoes map[int]string
	// Outros: as amostras dos outros jogadores já rastreados nesta partida
	// (o melhor passe de cada um).
	Outros map[int][]Amostra
	// Extra: o que outros pacotes acrescentam (RegistraContexto), por nome.
	Extra map[string]any
}

// Gerador produz itens da cadeia.
type Gerador func(c *ContextoCadeia) []ItemCadeia

var (
	geradoresMu sync.Mutex
	geradores   []struct {
		nome string
		g    Gerador
	}
)

// RegistraGerador acrescenta (ou troca, se o nome já existe) um gerador.
func RegistraGerador(nome string, g Gerador) {
	geradoresMu.Lock()
	defer geradoresMu.Unlock()
	for i := range geradores {
		if geradores[i].nome == nome {
			geradores[i].g = g
			return
		}
	}
	geradores = append(geradores, struct {
		nome string
		g    Gerador
	}{nome, g})
}

func init() {
	RegistraGerador("vida", geraVida)
	RegistraGerador("trajeto", geraTrajeto)
	RegistraGerador("combate", geraCombate)
	RegistraGerador("nivel", geraNivel)
	RegistraGerador("itens", geraItens)
	RegistraGerador("minuto", geraMinuto)
	RegistraGerador("trava", geraTrava)
}

// ToleranciaTrajetoPadrao é a tolerância do Douglas-Peucker (~ meio segundo
// andando; o mapa tem ~15 000 de lado).
var ToleranciaTrajetoPadrao = 150.0

// GeraCadeia roda todos os geradores e ordena por tempo.
func GeraCadeia(c *ContextoCadeia) []ItemCadeia {
	if c.ToleranciaTrajeto <= 0 {
		c.ToleranciaTrajeto = ToleranciaTrajetoPadrao
	}
	geradoresMu.Lock()
	gs := append([]struct {
		nome string
		g    Gerador
	}(nil), geradores...)
	geradoresMu.Unlock()
	var itens []ItemCadeia
	for _, g := range gs {
		itens = append(itens, g.g(c)...)
	}
	sort.SliceStable(itens, func(a, b int) bool { return itens[a].TMs < itens[b].TMs })
	return itens
}

func arred(v float64) int64 { return int64(math.Round(v)) }

func temPos(a Amostra) bool { return a.X != nil && a.Y != nil }

// marcos são os instantes que o trajeto não pode apagar: morte, renascimento, recall.
type marco struct {
	i    int
	tipo string
}

const (
	distanciaRecall = 3000.0
	raioFonte       = 1800.0
)

func marcosDe(c *ContextoCadeia) []marco {
	as := c.Amostras
	fonte := FonteDoTime(c.Time)
	var ms []marco
	ultimoVivo := -1
	for i := range as {
		if i > 0 && as[i].Morto != as[i-1].Morto {
			if as[i].Morto {
				ms = append(ms, marco{i, "morte"})
			} else {
				ms = append(ms, marco{i, "renascimento"})
			}
		}
		if as[i].Morto || !temPos(as[i]) {
			continue
		}
		if ultimoVivo >= 0 && !as[ultimoVivo].Morto {
			a, b := as[ultimoVivo], as[i]
			semMorte := true
			for k := ultimoVivo; k <= i; k++ {
				semMorte = semMorte && !as[k].Morto
			}
			if semMorte && b.TMs-a.TMs < 3000 && math.Hypot(*b.X-*a.X, *b.Y-*a.Y) > distanciaRecall &&
				math.Hypot(*b.X-fonte[0], *b.Y-fonte[1]) < raioFonte {
				ms = append(ms, marco{i, "recall"})
			}
		}
		ultimoVivo = i
	}
	return ms
}

func geraVida(c *ContextoCadeia) []ItemCadeia {
	var itens []ItemCadeia
	as := c.Amostras
	for _, m := range marcosDe(c) {
		a := as[m.i]
		d := map[string]any{}
		if temPos(a) {
			d["x"], d["y"] = arred(*a.X), arred(*a.Y)
		}
		switch m.tipo {
		case "morte":
			if e := c.Linha.MorteAntes(c.ParticipanteID, a.TMs); e != nil {
				d["matador"] = e.Quem
				d["assistencias"] = e.Assistencias
				if e.X != nil {
					d["x"], d["y"] = arred(*e.X), arred(*e.Y)
				}
			}
			d["respawn_s"] = a.Respawn
		case "recall":
			if m.i > 0 {
				for k := m.i - 1; k >= 0; k-- {
					if temPos(as[k]) {
						d["de_x"], d["de_y"] = arred(*as[k].X), arred(*as[k].Y)
						break
					}
				}
			}
		}
		itens = append(itens, ItemCadeia{TMs: a.TMs, Tipo: m.tipo, Dados: d})
	}
	return itens
}

// geraTrajeto: Douglas-Peucker em cada trecho vivo, cortado nos marcos e em
// buracos de mais de 3 s.
func geraTrajeto(c *ContextoCadeia) []ItemCadeia {
	as := c.Amostras
	corte := map[int]bool{}
	for _, m := range marcosDe(c) {
		corte[m.i] = true
	}
	var itens []ItemCadeia
	var trecho []int
	fecha := func() {
		for _, k := range douglasPeucker(as, trecho, c.ToleranciaTrajeto) {
			a := as[k]
			itens = append(itens, ItemCadeia{TMs: a.TMs, Tipo: "ponto", Dados: map[string]any{
				"x": arred(*a.X), "y": arred(*a.Y), "origem": a.Origem}})
		}
		trecho = trecho[:0]
	}
	for i, a := range as {
		if a.Morto || !temPos(a) {
			fecha()
			continue
		}
		if len(trecho) > 0 && (corte[i] || a.TMs-as[trecho[len(trecho)-1]].TMs > 3000) {
			fecha()
		}
		trecho = append(trecho, i)
	}
	fecha()
	return itens
}

// douglasPeucker devolve os índices mantidos (sempre o primeiro e o último).
func douglasPeucker(as []Amostra, idx []int, tol float64) []int {
	if len(idx) <= 2 {
		return append([]int(nil), idx...)
	}
	manter := make([]bool, len(idx))
	manter[0], manter[len(idx)-1] = true, true
	var rec func(i, j int)
	rec = func(i, j int) {
		if j <= i+1 {
			return
		}
		a, b := as[idx[i]], as[idx[j]]
		maior, k := -1.0, -1
		for m := i + 1; m < j; m++ {
			p := as[idx[m]]
			if d := distSegmento(*p.X, *p.Y, *a.X, *a.Y, *b.X, *b.Y); d > maior {
				maior, k = d, m
			}
		}
		if maior > tol {
			manter[k] = true
			rec(i, k)
			rec(k, j)
		}
	}
	rec(0, len(idx)-1)
	var r []int
	for m, ok := range manter {
		if ok {
			r = append(r, idx[m])
		}
	}
	return r
}

func distSegmento(px, py, ax, ay, bx, by float64) float64 {
	dx, dy := bx-ax, by-ay
	l2 := dx*dx + dy*dy
	if l2 == 0 {
		return math.Hypot(px-ax, py-ay)
	}
	t := math.Max(0, math.Min(1, ((px-ax)*dx+(py-ay)*dy)/l2))
	return math.Hypot(px-(ax+t*dx), py-(ay+t*dy))
}

func janelaDe(c *ContextoCadeia) (int64, int64) {
	if len(c.Amostras) == 0 {
		return 0, -1
	}
	return c.Amostras[0].TMs, c.Amostras[len(c.Amostras)-1].TMs
}

func geraCombate(c *ContextoCadeia) []ItemCadeia {
	ini, fim := janelaDe(c)
	var itens []ItemCadeia
	for _, e := range c.Linha.Eventos {
		if e.Tipo != "CHAMPION_KILL" || e.TMs < ini || e.TMs > fim {
			continue
		}
		d := map[string]any{"vitima": e.EmQuem, "matador": e.Quem}
		if e.X != nil {
			d["x"], d["y"] = arred(*e.X), arred(*e.Y)
		}
		if e.Quem == c.ParticipanteID {
			itens = append(itens, ItemCadeia{TMs: e.TMs, Tipo: "abate", Dados: d})
			continue
		}
		for _, a := range e.Assistencias {
			if a == c.ParticipanteID {
				itens = append(itens, ItemCadeia{TMs: e.TMs, Tipo: "assistencia", Dados: d})
			}
		}
	}
	return itens
}

func geraNivel(c *ContextoCadeia) []ItemCadeia {
	var itens []ItemCadeia
	for i := 1; i < len(c.Amostras); i++ {
		if c.Amostras[i].Nivel > c.Amostras[i-1].Nivel && c.Amostras[i-1].Nivel > 0 {
			itens = append(itens, ItemCadeia{TMs: c.Amostras[i].TMs, Tipo: "nivel", Dados: map[string]any{"nivel": c.Amostras[i].Nivel}})
		}
	}
	return itens
}

// geraItens compara o inventário segundo a segundo. "venda" é qualquer item
// que saiu: venda de verdade, consumo (poção, ward) ou componente que virou
// item maior — a API não distingue.
func geraItens(c *ContextoCadeia) []ItemCadeia {
	var itens []ItemCadeia
	var ant map[int]int
	for _, e := range c.Estados {
		if e.ParticipanteID != c.ParticipanteID {
			continue
		}
		agora := map[int]int{}
		for _, s := range e.Slots {
			if s.ID != 0 {
				agora[s.ID]++
			}
		}
		if ant != nil {
			for id, n := range agora {
				for k := ant[id]; k < n; k++ {
					itens = append(itens, ItemCadeia{TMs: e.TMs, Tipo: "compra", Dados: map[string]any{"item": id}})
				}
			}
			for id, n := range ant {
				for k := agora[id]; k < n; k++ {
					itens = append(itens, ItemCadeia{TMs: e.TMs, Tipo: "venda", Dados: map[string]any{"item": id}})
				}
			}
		}
		ant = agora
	}
	sort.SliceStable(itens, func(a, b int) bool {
		if itens[a].TMs != itens[b].TMs {
			return itens[a].TMs < itens[b].TMs
		}
		return fmt.Sprint(itens[a].Dados["item"]) < fmt.Sprint(itens[b].Dados["item"])
	})
	return itens
}

// LimiteDivergenciaCS: folga (além da dezena do creepScore) entre o CS lido no
// jogo e o da linha do tempo antes de virar item "cs_divergente".
const LimiteDivergenciaCS = 3

func geraMinuto(c *ContextoCadeia) []ItemCadeia {
	ini, fim := janelaDe(c)
	var itens []ItemCadeia
	as := c.Amostras
	for _, q := range c.Linha.Quadros {
		j, ok := q.Jogadores[c.ParticipanteID]
		if !ok || q.TMs < ini || q.TMs > fim {
			continue
		}
		d := map[string]any{"cs_lcu": j.CS, "ouro_total": j.OuroTotal, "ouro_atual": j.OuroAtual, "xp": j.XP, "nivel": j.Nivel}
		i := sort.Search(len(as), func(k int) bool { return as[k].TMs > q.TMs }) - 1
		if i >= 0 && q.TMs-as[i].TMs <= 2000 {
			// O creepScore do playerlist anda de 10 em 10 (medido: 30→40→50; é o
			// CS arredondado para baixo na dezena). Bate se o da linha do tempo
			// está na mesma dezena, com folga de LimiteDivergenciaCS.
			d["cs"] = as[i].CS
			dif := j.CS - as[i].CS
			d["diferenca"] = dif
			if dif < -LimiteDivergenciaCS || dif > 9+LimiteDivergenciaCS {
				itens = append(itens, ItemCadeia{TMs: q.TMs, Tipo: "cs_divergente", Dados: map[string]any{
					"cs": as[i].CS, "cs_lcu": j.CS, "diferenca": dif}})
			}
		}
		itens = append(itens, ItemCadeia{TMs: q.TMs, Tipo: "minuto", Dados: d})
	}
	return itens
}

func geraTrava(c *ContextoCadeia) []ItemCadeia {
	var itens []ItemCadeia
	perdida := false
	for _, a := range c.Amostras {
		if a.TravaPerdida && !perdida {
			itens = append(itens, ItemCadeia{TMs: a.TMs, Tipo: "trava_perdida", Dados: map[string]any{}})
		}
		if !a.TravaPerdida && perdida {
			itens = append(itens, ItemCadeia{TMs: a.TMs, Tipo: "trava_retomada", Dados: map[string]any{}})
		}
		perdida = a.TravaPerdida
	}
	return itens
}

// contextoDoPasse monta o contexto de geração a partir do banco (blob).
func contextoDoPasse(db *banco.DB, p Passe) (*ContextoCadeia, error) {
	ba, ok, err := leCompacto(db, p.ID, ConteudoAmostras)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("o passe %d ainda não foi compactado", p.ID)
	}
	c := &ContextoCadeia{GameID: p.GameID, ParticipanteID: p.ParticipanteID, Amostras: AmostrasDoBloco(ba)}
	if be, ok, err := leCompacto(db, p.ID, ConteudoTime); err != nil {
		return nil, err
	} else if ok {
		c.Estados = EstadosDoBloco(be)
	}
	if bruto, err := partida.LeLinhaDoTempo(db, p.GameID); err == nil {
		if lt, err := LeLinhaDoTempo(bruto); err == nil {
			c.Linha = lt
		}
	}
	db.QueryRow(`SELECT time FROM participantes WHERE game_id = ? AND participante_id = ?`, p.GameID, p.ParticipanteID).Scan(&c.Time)
	completaContexto(db, c)
	return c, nil
}

// RegeraCadeia refaz a cadeia de um passe a partir do bloco compactado, com
// os geradores registrados agora. Devolve quantos itens gravou.
func RegeraCadeia(db *banco.DB, passeID int64) (int, error) {
	p, err := LePasse(db, passeID)
	if err != nil {
		return 0, err
	}
	c, err := contextoDoPasse(db, p)
	if err != nil {
		return 0, err
	}
	itens := GeraCadeia(c)
	if _, err := gravaCadeia(db, p.ID, p.GameID, itens); err != nil {
		return 0, err
	}
	return len(itens), nil
}
