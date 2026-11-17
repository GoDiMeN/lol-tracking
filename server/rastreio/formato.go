package rastreio

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"

	"loltracking/server/banco"
)

// Origem da posição de uma amostra.
const (
	OrigemCamera      = "camera"      // câmera presa + screenPositionCenter (fórmula calibrada)
	OrigemMorte       = "morte"       // morto: local da morte (evento da linha do tempo)
	OrigemBase        = "base"        // acabou de renascer, câmera ainda não voltou: a fonte do time
	OrigemInterpolada = "interpolada" // trava perdida com o jogador vivo: interpolada entre vizinhas
)

// Recargas são os segundos que faltam para cada habilidade (nulo = não lido).
// P é a passiva: só alguns campeões têm recarga nela (Vex, Malphite...); nos
// outros o ícone fica sempre aceso e a leitura dá 0.
type Recargas struct {
	Q *float64 `json:"q"`
	W *float64 `json:"w"`
	E *float64 `json:"e"`
	R *float64 `json:"r"`
	D *float64 `json:"d"`
	F *float64 `json:"f"`
	P *float64 `json:"p"`
}

// Atributos do painel do jogador selecionado (nulo = não lido).
type Atributos struct {
	AD        *float64 `json:"ad"`
	AP        *float64 `json:"ap"`
	Armadura  *float64 `json:"armadura"`
	RM        *float64 `json:"rm"`
	VelAtaque *float64 `json:"vel_ataque"`
	VelMov    *float64 `json:"vel_mov"`
}

// Amostra é uma leitura do jogador seguido.
type Amostra struct {
	TMs          int64     `json:"t_ms"`
	X            *float64  `json:"x"`
	Y            *float64  `json:"y"`
	Origem       string    `json:"origem"`
	CamX         *float64  `json:"cam_x"`
	CamZ         *float64  `json:"cam_z"`
	TelaX        *float64  `json:"tela_x"`
	TelaY        *float64  `json:"tela_y"`
	SelecaoOK    bool      `json:"selecao_ok"`
	TravaPerdida bool      `json:"trava_perdida"`
	Morto        bool      `json:"morto"`
	Respawn      float64   `json:"respawn"`
	Nivel        int       `json:"nivel"`
	CS           int       `json:"cs"`
	Abates       int       `json:"abates"`
	Mortes       int       `json:"mortes"`
	Assistencias int       `json:"assistencias"`
	Ward         float64   `json:"ward"`
	DtMs         int64     `json:"dt_ms"`
	Vida         *float64  `json:"vida"`
	VidaMax      *float64  `json:"vida_max"`
	Mana         *float64  `json:"mana"`
	ManaMax      *float64  `json:"mana_max"`
	CD           Recargas  `json:"cd"`
	Atr          Atributos `json:"atributos"`
}

// SlotItem é um slot do inventário (ID 0 = vazio).
type SlotItem struct {
	ID  int `json:"id"`
	Qtd int `json:"qtd"`
}

// EstadoJogador é o estado de um dos 10 num instante (pela API e, na tarefa 3,
// pelos quadros laterais da tela).
type EstadoJogador struct {
	TMs            int64       `json:"t_ms"`
	ParticipanteID int         `json:"participante_id"`
	Nivel          int         `json:"nivel"`
	Abates         int         `json:"abates"`
	Mortes         int         `json:"mortes"`
	Assistencias   int         `json:"assistencias"`
	CS             int         `json:"cs"`
	Ward           float64     `json:"ward"`
	Morto          bool        `json:"morto"`
	Respawn        float64     `json:"respawn"`
	Slots          [7]SlotItem `json:"-"`
	VidaPct        *float64    `json:"vida_pct"`
	ManaPct        *float64    `json:"mana_pct"`
}

// Itens são os slots ocupados.
func (e EstadoJogador) Itens() []SlotItem {
	l := []SlotItem{}
	for i, s := range e.Slots {
		if s.ID != 0 {
			l = append(l, SlotItem{ID: s.ID, Qtd: s.Qtd})
			_ = i
		}
	}
	return l
}

// defCol liga uma coluna do bloco a um campo do struct. campo devolve um
// ponteiro: *int64, *int, *float64, **float64, *bool ou *string.
type defCol[T any] struct {
	nome   string
	escala int64
	campo  func(*T) any
}

// colunasAmostra é a lista de colunas do conteúdo "amostras". Acrescentar
// coluna aqui é compatível com blobs antigos (a coluna vem nula).
var colunasAmostra = []defCol[Amostra]{
	{"t_ms", 1, func(a *Amostra) any { return &a.TMs }},
	{"x", 1, func(a *Amostra) any { return &a.X }},
	{"y", 1, func(a *Amostra) any { return &a.Y }},
	{"origem", 0, func(a *Amostra) any { return &a.Origem }},
	{"cam_x", 1, func(a *Amostra) any { return &a.CamX }},
	{"cam_z", 1, func(a *Amostra) any { return &a.CamZ }},
	{"tela_x", 10, func(a *Amostra) any { return &a.TelaX }},
	{"tela_y", 10, func(a *Amostra) any { return &a.TelaY }},
	{"selecao_ok", 1, func(a *Amostra) any { return &a.SelecaoOK }},
	{"trava_perdida", 1, func(a *Amostra) any { return &a.TravaPerdida }},
	{"morto", 1, func(a *Amostra) any { return &a.Morto }},
	{"respawn", 100, func(a *Amostra) any { return &a.Respawn }},
	{"nivel", 1, func(a *Amostra) any { return &a.Nivel }},
	{"cs", 1, func(a *Amostra) any { return &a.CS }},
	{"abates", 1, func(a *Amostra) any { return &a.Abates }},
	{"mortes", 1, func(a *Amostra) any { return &a.Mortes }},
	{"assistencias", 1, func(a *Amostra) any { return &a.Assistencias }},
	{"ward", 100, func(a *Amostra) any { return &a.Ward }},
	{"dt_ms", 1, func(a *Amostra) any { return &a.DtMs }},
	{"vida", 1, func(a *Amostra) any { return &a.Vida }},
	{"vida_max", 1, func(a *Amostra) any { return &a.VidaMax }},
	{"mana", 1, func(a *Amostra) any { return &a.Mana }},
	{"mana_max", 1, func(a *Amostra) any { return &a.ManaMax }},
	{"cd_q", 10, func(a *Amostra) any { return &a.CD.Q }},
	{"cd_w", 10, func(a *Amostra) any { return &a.CD.W }},
	{"cd_e", 10, func(a *Amostra) any { return &a.CD.E }},
	{"cd_r", 10, func(a *Amostra) any { return &a.CD.R }},
	{"cd_d", 10, func(a *Amostra) any { return &a.CD.D }},
	{"cd_f", 10, func(a *Amostra) any { return &a.CD.F }},
	{"cd_p", 10, func(a *Amostra) any { return &a.CD.P }},
	{"atr_ad", 1, func(a *Amostra) any { return &a.Atr.AD }},
	{"atr_ap", 1, func(a *Amostra) any { return &a.Atr.AP }},
	{"atr_armadura", 1, func(a *Amostra) any { return &a.Atr.Armadura }},
	{"atr_rm", 1, func(a *Amostra) any { return &a.Atr.RM }},
	{"atr_vel_ataque", 100, func(a *Amostra) any { return &a.Atr.VelAtaque }},
	{"atr_vel_mov", 1, func(a *Amostra) any { return &a.Atr.VelMov }},
}

var colunasEstado = func() []defCol[EstadoJogador] {
	cs := []defCol[EstadoJogador]{
		{"participante_id", 1, func(e *EstadoJogador) any { return &e.ParticipanteID }},
		{"t_ms", 1, func(e *EstadoJogador) any { return &e.TMs }},
		{"nivel", 1, func(e *EstadoJogador) any { return &e.Nivel }},
		{"abates", 1, func(e *EstadoJogador) any { return &e.Abates }},
		{"mortes", 1, func(e *EstadoJogador) any { return &e.Mortes }},
		{"assistencias", 1, func(e *EstadoJogador) any { return &e.Assistencias }},
		{"cs", 1, func(e *EstadoJogador) any { return &e.CS }},
		{"ward", 100, func(e *EstadoJogador) any { return &e.Ward }},
		{"morto", 1, func(e *EstadoJogador) any { return &e.Morto }},
		{"respawn", 100, func(e *EstadoJogador) any { return &e.Respawn }},
		{"vida_pct", 10, func(e *EstadoJogador) any { return &e.VidaPct }},
		{"mana_pct", 10, func(e *EstadoJogador) any { return &e.ManaPct }},
	}
	for k := 0; k < 7; k++ {
		k := k
		cs = append(cs,
			defCol[EstadoJogador]{fmt.Sprintf("item%d", k), 1, func(e *EstadoJogador) any { return &e.Slots[k].ID }},
			defCol[EstadoJogador]{fmt.Sprintf("qtd%d", k), 1, func(e *EstadoJogador) any { return &e.Slots[k].Qtd }})
	}
	return cs
}()

func paraBloco[T any](linhas []T, defs []defCol[T]) Bloco {
	b := Bloco{Versao: VersaoFormato, N: len(linhas)}
	for _, d := range defs {
		col := Coluna{Nome: d.nome, Escala: d.escala}
		var amostraCampo any
		if len(linhas) > 0 {
			amostraCampo = d.campo(&linhas[0])
		} else {
			var z T
			amostraCampo = d.campo(&z)
		}
		_, texto := amostraCampo.(*string)
		col.Texto = texto
		if texto {
			col.Escala = 0
			col.Textos = make([]string, len(linhas))
		} else {
			if col.Escala <= 0 {
				col.Escala = 1
			}
			col.Ints = make([]int64, len(linhas))
		}
		temNulo := false
		nulos := make([]bool, len(linhas))
		esc := float64(col.Escala)
		for i := range linhas {
			switch p := d.campo(&linhas[i]).(type) {
			case *string:
				col.Textos[i] = *p
			case *int64:
				col.Ints[i] = *p
			case *int:
				col.Ints[i] = int64(*p)
			case *bool:
				if *p {
					col.Ints[i] = 1
				}
			case *float64:
				col.Ints[i] = int64(math.Round(*p * esc))
			case **float64:
				if *p == nil || math.IsNaN(**p) {
					nulos[i], temNulo = true, true
				} else {
					col.Ints[i] = int64(math.Round(**p * esc))
				}
			}
		}
		if temNulo {
			col.Nulos = nulos
		}
		b.Colunas = append(b.Colunas, col)
	}
	return b
}

func doBloco[T any](b Bloco, defs []defCol[T]) []T {
	linhas := make([]T, b.N)
	for _, d := range defs {
		col := b.Col(d.nome)
		if col == nil {
			continue // blob anterior à coluna: fica nulo/zero
		}
		esc := float64(col.Escala)
		if esc <= 0 {
			esc = 1
		}
		for i := range linhas {
			switch p := d.campo(&linhas[i]).(type) {
			case *string:
				if col.Texto && !col.Nulo(i) {
					*p = col.Textos[i]
				}
			case *int64:
				if !col.Texto && !col.Nulo(i) {
					*p = int64(float64(col.Ints[i]) / esc)
				}
			case *int:
				if !col.Texto && !col.Nulo(i) {
					*p = int(float64(col.Ints[i]) / esc)
				}
			case *bool:
				if !col.Texto && !col.Nulo(i) {
					*p = col.Ints[i] != 0
				}
			case *float64:
				if !col.Texto && !col.Nulo(i) {
					*p = float64(col.Ints[i]) / esc
				}
			case **float64:
				if !col.Texto && !col.Nulo(i) {
					v := float64(col.Ints[i]) / esc
					*p = &v
				}
			}
		}
	}
	return linhas
}

// BlocoDeAmostras / AmostrasDoBloco: o conteúdo "amostras".
func BlocoDeAmostras(as []Amostra) Bloco { return paraBloco(as, colunasAmostra) }
func AmostrasDoBloco(b Bloco) []Amostra  { return doBloco(b, colunasAmostra) }
func BlocoDeEstados(es []EstadoJogador) Bloco {
	// Ordenado por jogador e tempo: as colunas de cada jogador ficam contínuas
	// (deltas pequenos, corridas longas).
	s := append([]EstadoJogador(nil), es...)
	sort.SliceStable(s, func(a, b int) bool {
		if s[a].ParticipanteID != s[b].ParticipanteID {
			return s[a].ParticipanteID < s[b].ParticipanteID
		}
		return s[a].TMs < s[b].TMs
	})
	return paraBloco(s, colunasEstado)
}
func EstadosDoBloco(b Bloco) []EstadoJogador { return doBloco(b, colunasEstado) }

// --- tabelas de trabalho ---

const colunasBrutas = `t_ms, x, y, origem, cam_x, cam_z, tela_x, tela_y, selecao_ok, trava_perdida, morto, respawn, nivel, cs,
	abates, mortes, assistencias, ward, dt_ms, vida, vida_max, mana, mana_max, cd_q, cd_w, cd_e, cd_r, cd_d, cd_f,
	atr_ad, atr_ap, atr_armadura, atr_rm, atr_vel_ataque, atr_vel_mov, cd_p`

func valoresBrutos(a *Amostra) []any {
	return []any{a.TMs, a.X, a.Y, a.Origem, a.CamX, a.CamZ, a.TelaX, a.TelaY, a.SelecaoOK, a.TravaPerdida, a.Morto, a.Respawn,
		a.Nivel, a.CS, a.Abates, a.Mortes, a.Assistencias, a.Ward, a.DtMs, a.Vida, a.VidaMax, a.Mana, a.ManaMax,
		a.CD.Q, a.CD.W, a.CD.E, a.CD.R, a.CD.D, a.CD.F, a.Atr.AD, a.Atr.AP, a.Atr.Armadura, a.Atr.RM, a.Atr.VelAtaque, a.Atr.VelMov, a.CD.P}
}

// progresso é o que o passe guarda junto de cada lote de amostras: é de onde
// a retomada continua.
type progresso struct {
	TAtualMs                       int64
	Retravas, Tentativas, SemTrava int
}

// gravaBrutas grava amostras, estados e o progresso do passe numa transação só.
func gravaBrutas(db *banco.DB, passeID, gameID int64, pid int, as []Amostra, es []EstadoJogador, pr progresso) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if len(as) > 0 {
		st, err := tx.Prepare(`INSERT INTO amostras_brutas (passe_id, game_id, participante_id, ` + colunasBrutas + `)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
		if err != nil {
			return err
		}
		defer st.Close()
		for i := range as {
			if _, err := st.Exec(append([]any{passeID, gameID, pid}, valoresBrutos(&as[i])...)...); err != nil {
				return err
			}
		}
	}
	if len(es) > 0 {
		st, err := tx.Prepare(`INSERT INTO amostras_time_brutas (passe_id, game_id, t_ms, participante_id, nivel, abates, mortes,
			assistencias, cs, ward, morto, respawn, itens, vida_pct, mana_pct) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
		if err != nil {
			return err
		}
		defer st.Close()
		for _, e := range es {
			itens, _ := json.Marshal(e.Slots)
			if _, err := st.Exec(passeID, gameID, e.TMs, e.ParticipanteID, e.Nivel, e.Abates, e.Mortes, e.Assistencias,
				e.CS, e.Ward, e.Morto, e.Respawn, string(itens), e.VidaPct, e.ManaPct); err != nil {
				return err
			}
		}
	}
	if _, err := tx.Exec(`UPDATE passes SET amostras = amostras + ?, t_atual_ms = MAX(t_atual_ms, ?), retravas = ?,
			tentativas_trava = ?, amostras_sem_trava = ? WHERE id = ?`,
		len(as), pr.TAtualMs, pr.Retravas, pr.Tentativas, pr.SemTrava, passeID); err != nil {
		return err
	}
	return tx.Commit()
}

func lePtr(n sql.NullFloat64) *float64 { return nf(n) }

// leBrutas lê as amostras de trabalho do passe (opcionalmente numa janela).
func leBrutas(db *banco.DB, passeID int64, deMs, ateMs int64) ([]Amostra, error) {
	linhas, err := db.Query(`SELECT `+colunasBrutas+` FROM amostras_brutas WHERE passe_id = ? AND t_ms >= ? AND t_ms <= ?
		ORDER BY t_ms`, passeID, deMs, ateMs)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	var lista []Amostra
	for linhas.Next() {
		var a Amostra
		var n [24]sql.NullFloat64
		if err := linhas.Scan(&a.TMs, &n[0], &n[1], &a.Origem, &n[2], &n[3], &n[4], &n[5], &a.SelecaoOK, &a.TravaPerdida,
			&a.Morto, &a.Respawn, &a.Nivel, &a.CS, &a.Abates, &a.Mortes, &a.Assistencias, &a.Ward, &a.DtMs,
			&n[6], &n[7], &n[8], &n[9], &n[10], &n[11], &n[12], &n[13], &n[14], &n[15], &n[16], &n[17], &n[18], &n[19], &n[20], &n[21], &n[22]); err != nil {
			return nil, err
		}
		a.X, a.Y, a.CamX, a.CamZ, a.TelaX, a.TelaY = lePtr(n[0]), lePtr(n[1]), lePtr(n[2]), lePtr(n[3]), lePtr(n[4]), lePtr(n[5])
		a.Vida, a.VidaMax, a.Mana, a.ManaMax = lePtr(n[6]), lePtr(n[7]), lePtr(n[8]), lePtr(n[9])
		a.CD = Recargas{Q: lePtr(n[10]), W: lePtr(n[11]), E: lePtr(n[12]), R: lePtr(n[13]), D: lePtr(n[14]), F: lePtr(n[15]), P: lePtr(n[22])}
		a.Atr = Atributos{lePtr(n[16]), lePtr(n[17]), lePtr(n[18]), lePtr(n[19]), lePtr(n[20]), lePtr(n[21])}
		lista = append(lista, a)
	}
	return lista, linhas.Err()
}

// leEstadosBrutos lê o estado dos 10 de trabalho do passe.
func leEstadosBrutos(db *banco.DB, passeID int64, deMs, ateMs int64) ([]EstadoJogador, error) {
	linhas, err := db.Query(`SELECT t_ms, participante_id, nivel, abates, mortes, assistencias, cs, ward, morto, respawn, itens,
		vida_pct, mana_pct FROM amostras_time_brutas WHERE passe_id = ? AND t_ms >= ? AND t_ms <= ? ORDER BY t_ms, participante_id`,
		passeID, deMs, ateMs)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	var lista []EstadoJogador
	for linhas.Next() {
		var e EstadoJogador
		var itens string
		var v, m sql.NullFloat64
		if err := linhas.Scan(&e.TMs, &e.ParticipanteID, &e.Nivel, &e.Abates, &e.Mortes, &e.Assistencias, &e.CS, &e.Ward,
			&e.Morto, &e.Respawn, &itens, &v, &m); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(itens), &e.Slots)
		e.VidaPct, e.ManaPct = nf(v), nf(m)
		lista = append(lista, e)
	}
	return lista, linhas.Err()
}
