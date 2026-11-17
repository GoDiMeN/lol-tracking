package rastreio

import (
	"encoding/json"
	"errors"
	"math"
	"sort"
	"sync"

	"loltracking/server/banco"
	"loltracking/server/partida"
)

// As rotas de leitura leem do bloco compactado (decodificado uma vez e guardado
// num cache pequeno) ou, com o passe em andamento, da tabela de trabalho.

type entradaCache struct {
	amostras []Amostra
	estados  []EstadoJogador
}

var (
	cacheMu    sync.Mutex
	cache      = map[int64]*entradaCache{}
	cacheOrdem []int64
)

// TamanhoCache: quantos passes decodificados ficam na memória (~1 MB cada).
const TamanhoCache = 24

func esqueceCache(passeID int64) {
	cacheMu.Lock()
	delete(cache, passeID)
	cacheMu.Unlock()
}

func doCache(db *banco.DB, passeID int64) (*entradaCache, bool, error) {
	cacheMu.Lock()
	if e, ok := cache[passeID]; ok {
		cacheMu.Unlock()
		return e, true, nil
	}
	cacheMu.Unlock()
	ba, ok, err := leCompacto(db, passeID, ConteudoAmostras)
	if err != nil || !ok {
		return nil, false, err
	}
	e := &entradaCache{amostras: AmostrasDoBloco(ba)}
	if be, ok, err := leCompacto(db, passeID, ConteudoTime); err != nil {
		return nil, false, err
	} else if ok {
		e.estados = EstadosDoBloco(be)
	}
	cacheMu.Lock()
	cache[passeID] = e
	cacheOrdem = append(cacheOrdem, passeID)
	for len(cacheOrdem) > TamanhoCache {
		delete(cache, cacheOrdem[0])
		cacheOrdem = cacheOrdem[1:]
	}
	cacheMu.Unlock()
	return e, true, nil
}

// amostrasDoPasse: do bloco (compactado) ou da tabela de trabalho.
func amostrasDoPasse(db *banco.DB, p Passe) ([]Amostra, string, error) {
	if p.Compactado {
		e, ok, err := doCache(db, p.ID)
		if err != nil {
			return nil, "", err
		}
		if ok {
			return e.amostras, "compactado", nil
		}
	}
	as, err := leBrutas(db, p.ID, math.MinInt64, math.MaxInt64)
	return as, "trabalho", err
}

func estadosDoPasse(db *banco.DB, p Passe) ([]EstadoJogador, error) {
	if p.Compactado {
		e, ok, err := doCache(db, p.ID)
		if err != nil {
			return nil, err
		}
		if ok {
			return e.estados, nil
		}
	}
	return leEstadosBrutos(db, p.ID, math.MinInt64, math.MaxInt64)
}

// melhorPasse é o passe que representa o jogador na partida: o concluído
// mais recente; senão o mais recente com amostras (cancelado ou em andamento).
func melhorPasse(db *banco.DB, gameID int64, pid int) (Passe, bool, error) {
	ps, err := lePasses(db, `WHERE p.game_id = ? AND p.participante_id = ? AND p.amostras > 0
		ORDER BY (p.estado = 'concluido') DESC, p.id DESC LIMIT 1`, gameID, pid)
	if err != nil || len(ps) == 0 {
		return Passe{}, false, err
	}
	return ps[0], true, nil
}

// PaginaAmostras é a resposta de amostras por janela.
type PaginaAmostras struct {
	ParticipanteID int       `json:"participante_id"`
	PasseID        int64     `json:"passe_id"`
	Fonte          string    `json:"fonte"`
	DeMs           int64     `json:"de_ms"`
	AteMs          int64     `json:"ate_ms"`
	PassoMs        int64     `json:"passo_ms"`
	Amostras       []Amostra `json:"amostras"`
	Proximo        *int64    `json:"proximo_ms"`
}

// ErrSemAmostras: o participante ainda não foi rastreado nesta partida.
var ErrSemAmostras = errors.New("este jogador ainda não foi rastreado nesta partida")

// ListaAmostras devolve as amostras do participante em [de, ate], uma por
// passo (a primeira de cada intervalo; passo 0 = todas), até limite.
func ListaAmostras(db *banco.DB, gameID int64, pid int, deMs, ateMs, passoMs int64, limite int) (PaginaAmostras, error) {
	pg := PaginaAmostras{ParticipanteID: pid, DeMs: deMs, AteMs: ateMs, PassoMs: passoMs, Amostras: []Amostra{}}
	p, ok, err := melhorPasse(db, gameID, pid)
	if err != nil {
		return pg, err
	}
	if !ok {
		return pg, ErrSemAmostras
	}
	pg.PasseID = p.ID
	as, fonte, err := amostrasDoPasse(db, p)
	if err != nil {
		return pg, err
	}
	pg.Fonte = fonte
	i := sort.Search(len(as), func(k int) bool { return as[k].TMs >= deMs })
	balde := int64(math.MinInt64)
	for ; i < len(as) && as[i].TMs <= ateMs; i++ {
		if passoMs > 0 {
			b := as[i].TMs / passoMs
			if b == balde {
				continue
			}
			balde = b
		}
		if len(pg.Amostras) >= limite {
			t := as[i].TMs
			pg.Proximo = &t
			break
		}
		pg.Amostras = append(pg.Amostras, as[i])
	}
	return pg, nil
}

// JogadorNoQuadro é um dos 10 num instante.
type JogadorNoQuadro struct {
	ParticipanteID int        `json:"participante_id"`
	X              *float64   `json:"x"`
	Y              *float64   `json:"y"`
	Origem         string     `json:"origem"`
	Rastreado      bool       `json:"rastreado"`
	Morto          bool       `json:"morto"`
	Respawn        float64    `json:"respawn"`
	Nivel          int        `json:"nivel"`
	Abates         int        `json:"abates"`
	Mortes         int        `json:"mortes"`
	Assistencias   int        `json:"assistencias"`
	CS             int        `json:"cs"`
	Itens          []SlotItem `json:"itens"`
	VidaPct        *float64   `json:"vida_pct"`
	ManaPct        *float64   `json:"mana_pct"`
	Vida           *float64   `json:"vida"`
	VidaMax        *float64   `json:"vida_max"`
	Mana           *float64   `json:"mana"`
	ManaMax        *float64   `json:"mana_max"`
	// Recargas e atributos lidos da tela (só do jogador seguido no passe).
	CD  Recargas  `json:"cd"`
	Atr Atributos `json:"atributos"`
}

// Quadro é o estado dos 10 num instante.
type Quadro struct {
	TMs       int64             `json:"t_ms"`
	Jogadores []JogadorNoQuadro `json:"jogadores"`
}

// MaxQuadros por pedido de estados.
const MaxQuadros = 1200

// ListaEstados monta os quadros de de até ate, de passo em passo. Posição:
// das amostras do jogador (interpolada entre as vizinhas), senão dos quadros
// por minuto da linha do tempo (origem "linha_do_tempo", grosseira). Estado
// (nível, itens, placar, morto): do estado dos 10 gravado em qualquer passe.
func ListaEstados(db *banco.DB, gameID int64, deMs, ateMs, passoMs int64) ([]Quadro, error) {
	if passoMs <= 0 {
		passoMs = 1000
	}
	p, err := partida.Le(db, gameID)
	if err != nil {
		return nil, err
	}
	var lt LinhaDoTempo
	if bruto, err := partida.LeLinhaDoTempo(db, gameID); err == nil {
		lt, _ = LeLinhaDoTempo(bruto)
	}
	amostras := map[int][]Amostra{}
	for _, pt := range p.Participantes {
		if ps, ok, err := melhorPasse(db, gameID, pt.ParticipanteID); err != nil {
			return nil, err
		} else if ok {
			as, _, err := amostrasDoPasse(db, ps)
			if err != nil {
				return nil, err
			}
			amostras[pt.ParticipanteID] = as
		}
	}
	estados := map[int][]EstadoJogador{}
	todos, err := lePasses(db, `WHERE p.game_id = ? AND p.amostras > 0 ORDER BY (p.estado = 'concluido') DESC, p.id DESC`, gameID)
	if err != nil {
		return nil, err
	}
	for _, ps := range todos {
		es, err := estadosDoPasse(db, ps)
		if err != nil {
			return nil, err
		}
		for _, e := range es {
			estados[e.ParticipanteID] = append(estados[e.ParticipanteID], e)
		}
	}
	for k := range estados {
		s := estados[k]
		sort.SliceStable(s, func(a, b int) bool { return s[a].TMs < s[b].TMs })
	}
	var quadros []Quadro
	for t := deMs; t <= ateMs && len(quadros) < MaxQuadros; t += passoMs {
		q := Quadro{TMs: t}
		for _, pt := range p.Participantes {
			j := JogadorNoQuadro{ParticipanteID: pt.ParticipanteID, Itens: []SlotItem{}}
			if as := amostras[pt.ParticipanteID]; len(as) > 0 {
				posicaoDasAmostras(&j, as, t)
			}
			if j.X == nil {
				if x, y, ok := lt.PosicaoInterpolada(pt.ParticipanteID, t); ok {
					j.X, j.Y, j.Origem = &x, &y, "linha_do_tempo"
				}
			}
			if es := estados[pt.ParticipanteID]; len(es) > 0 {
				i := sort.Search(len(es), func(k int) bool { return es[k].TMs > t }) - 1
				if i >= 0 && t-es[i].TMs <= 3000 {
					e := es[i]
					j.Morto, j.Respawn, j.Nivel, j.Abates, j.Mortes, j.Assistencias, j.CS = e.Morto, e.Respawn, e.Nivel,
						e.Abates, e.Mortes, e.Assistencias, e.CS
					j.Itens, j.VidaPct, j.ManaPct = e.Itens(), e.VidaPct, e.ManaPct
				}
			}
			q.Jogadores = append(q.Jogadores, j)
		}
		quadros = append(quadros, q)
	}
	return quadros, nil
}

func posicaoDasAmostras(j *JogadorNoQuadro, as []Amostra, t int64) {
	i := sort.Search(len(as), func(k int) bool { return as[k].TMs >= t })
	var a, b *Amostra
	if i > 0 && t-as[i-1].TMs <= 1500 {
		a = &as[i-1]
	}
	if i < len(as) && as[i].TMs-t <= 1500 {
		b = &as[i]
	}
	if a == nil && b == nil {
		return
	}
	perto := a
	if perto == nil || (b != nil && b.TMs-t < t-a.TMs) {
		perto = b
	}
	j.Rastreado = true
	j.Origem, j.Morto, j.Respawn, j.Nivel, j.CS = perto.Origem, perto.Morto, perto.Respawn, perto.Nivel, perto.CS
	j.Abates, j.Mortes, j.Assistencias = perto.Abates, perto.Mortes, perto.Assistencias
	j.Vida, j.VidaMax, j.Mana, j.ManaMax = perto.Vida, perto.VidaMax, perto.Mana, perto.ManaMax
	j.CD, j.Atr = perto.CD, perto.Atr
	if a != nil && b != nil && temPos(*a) && temPos(*b) && !a.Morto && !b.Morto && b.TMs > a.TMs {
		f := float64(t-a.TMs) / float64(b.TMs-a.TMs)
		x, y := *a.X+(*b.X-*a.X)*f, *a.Y+(*b.Y-*a.Y)*f
		j.X, j.Y = &x, &y
		return
	}
	if temPos(*perto) {
		x, y := *perto.X, *perto.Y
		j.X, j.Y = &x, &y
	}
}

// Cadeia de um participante na partida (do melhor passe).
type RespostaCadeia struct {
	ParticipanteID int          `json:"participante_id"`
	PasseID        int64        `json:"passe_id"`
	Itens          []ItemCadeia `json:"itens"`
}

// ListaCadeia devolve a cadeia do participante (tipos vazio = todos).
func ListaCadeia(db *banco.DB, gameID int64, pid int, tipos map[string]bool) (RespostaCadeia, error) {
	rc := RespostaCadeia{ParticipanteID: pid, Itens: []ItemCadeia{}}
	ps, err := lePasses(db, `WHERE p.game_id = ? AND p.participante_id = ? AND EXISTS (SELECT 1 FROM cadeia c WHERE c.passe_id = p.id)
		ORDER BY (p.estado = 'concluido') DESC, p.id DESC LIMIT 1`, gameID, pid)
	if err != nil {
		return rc, err
	}
	if len(ps) == 0 {
		return rc, ErrSemAmostras
	}
	rc.PasseID = ps[0].ID
	linhas, err := db.Query(`SELECT t_ms, tipo, dados FROM cadeia WHERE passe_id = ? ORDER BY t_ms, id`, rc.PasseID)
	if err != nil {
		return rc, err
	}
	defer linhas.Close()
	for linhas.Next() {
		var it ItemCadeia
		var dados string
		if err := linhas.Scan(&it.TMs, &it.Tipo, &dados); err != nil {
			return rc, err
		}
		if len(tipos) > 0 && !tipos[it.Tipo] {
			continue
		}
		json.Unmarshal([]byte(dados), &it.Dados)
		rc.Itens = append(rc.Itens, it)
	}
	return rc, linhas.Err()
}

// PartidaRastreada é uma linha da lista de partidas rastreadas.
type PartidaRastreada struct {
	GameID        int64 `json:"game_id"`
	Participantes []int `json:"participantes"`
	Rastreados    int   `json:"rastreados"`
}

// ListaRastreadas: partidas com pelo menos um passe concluído.
func ListaRastreadas(db *banco.DB) ([]PartidaRastreada, error) {
	linhas, err := db.Query(`SELECT DISTINCT game_id, participante_id FROM passes WHERE estado = 'concluido'
		ORDER BY game_id DESC, participante_id`)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	var lista []PartidaRastreada
	for linhas.Next() {
		var g int64
		var pid int
		if err := linhas.Scan(&g, &pid); err != nil {
			return nil, err
		}
		if len(lista) == 0 || lista[len(lista)-1].GameID != g {
			lista = append(lista, PartidaRastreada{GameID: g, Participantes: []int{}})
		}
		u := &lista[len(lista)-1]
		u.Participantes = append(u.Participantes, pid)
		u.Rastreados = len(u.Participantes)
	}
	return lista, linhas.Err()
}
