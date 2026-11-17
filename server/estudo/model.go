// Package estudo guarda os estudos: um jogador e uma lista de partidas
// escolhidas À MÃO (vitórias e derrotas), com anotação livre por partida, e
// compara os indicadores da análise entre as vitórias e as derrotas. É conta
// (média, mediana, tamanho de efeito), não IA, e não diz "causa": com poucas
// partidas a diferença pode ser acaso, e a resposta diz isso.
package estudo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"loltracking/server/analise"
	"loltracking/server/banco"
	"loltracking/server/partida"
)

// Migra cria as tabelas do estudo (dados.md).
func Migra(db *banco.DB) error {
	return banco.Executa(db,
		`CREATE TABLE IF NOT EXISTS estudos (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			nome          TEXT NOT NULL,
			puuid         TEXT NOT NULL,
			campeao_id    INTEGER NOT NULL DEFAULT 0,
			posicao       TEXT NOT NULL DEFAULT '',
			criado_em     TEXT NOT NULL,
			atualizado_em TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS estudo_partidas (
			estudo_id     INTEGER NOT NULL,
			game_id       INTEGER NOT NULL,
			resultado     TEXT NOT NULL,
			anotacao      TEXT NOT NULL DEFAULT '',
			adicionada_em TEXT NOT NULL,
			atualizada_em TEXT NOT NULL,
			PRIMARY KEY (estudo_id, game_id)
		)`,
	)
}

// Resultados.
const (
	Vitoria = "vitoria"
	Derrota = "derrota"
)

// Estudo é um estudo.
type Estudo struct {
	ID           int64  `json:"id"`
	Nome         string `json:"nome"`
	Puuid        string `json:"puuid"`
	Jogador      string `json:"jogador"` // Riot ID mais recente que o banco conhece ("" se nenhum)
	CampeaoID    int    `json:"campeao_id"`
	Campeao      string `json:"campeao"`
	Posicao      string `json:"posicao"`
	CriadoEm     string `json:"criado_em"`
	AtualizadoEm string `json:"atualizado_em"`
	Vitorias     int    `json:"vitorias"`
	Derrotas     int    `json:"derrotas"`
}

// PartidaDoEstudo é uma partida da lista, do ponto de vista do jogador.
type PartidaDoEstudo struct {
	GameID         int64  `json:"game_id"`
	Resultado      string `json:"resultado"`
	ParticipanteID int    `json:"participante_id"`
	CampeaoID      int    `json:"campeao_id"`
	Campeao        string `json:"campeao"`
	Posicao        string `json:"posicao"`
	Abates         int    `json:"abates"`
	Mortes         int    `json:"mortes"`
	Assistencias   int    `json:"assistencias"`
	CriadaEm       string `json:"criada_em"`
	DuracaoS       int    `json:"duracao_s"`
	Anotacao       string `json:"anotacao"`
	AdicionadaEm   string `json:"adicionada_em"`
	AtualizadaEm   string `json:"atualizada_em"`
	// ForaDoFiltro: jogou outro campeão/posição que o do estudo (entra assim mesmo: a escolha é da pessoa).
	ForaDoFiltro bool `json:"fora_do_filtro"`
	// Analisada: há indicadores; Rastreados: quem foi rastreado nela.
	Analisada  bool  `json:"analisada"`
	Rastreado  bool  `json:"rastreado"` // o jogador do estudo foi rastreado nesta partida
	Rastreados []int `json:"rastreados"`
}

// ErrNaoEncontrado: estudo que não existe.
var ErrNaoEncontrado = errors.New("estudo não encontrado")

// ErroPedido: pedido inválido (400).
type ErroPedido struct{ Msg string }

func (e *ErroPedido) Error() string { return e.Msg }

// ErroConflito: já existe (409).
type ErroConflito struct{ Msg string }

func (e *ErroConflito) Error() string { return e.Msg }

// Novo é o corpo de criar/editar.
type Novo struct {
	Nome      *string `json:"nome"`
	Puuid     string  `json:"puuid"`
	CampeaoID *int    `json:"campeao_id"`
	Posicao   *string `json:"posicao"`
}

var posicoes = map[string]bool{"": true, "TOP": true, "JUNGLE": true, "MIDDLE": true, "BOTTOM": true, "UTILITY": true}

// Cria grava um estudo novo.
func Cria(db *banco.DB, n Novo) (Estudo, error) {
	nome := ""
	if n.Nome != nil {
		nome = strings.TrimSpace(*n.Nome)
	}
	if strings.TrimSpace(n.Puuid) == "" {
		return Estudo{}, &ErroPedido{"informe o puuid do jogador"}
	}
	if nome == "" {
		return Estudo{}, &ErroPedido{"informe o nome do estudo"}
	}
	camp, pos := 0, ""
	if n.CampeaoID != nil {
		camp = *n.CampeaoID
	}
	if n.Posicao != nil {
		pos = strings.ToUpper(strings.TrimSpace(*n.Posicao))
	}
	if !posicoes[pos] || camp < 0 {
		return Estudo{}, &ErroPedido{"posição precisa ser TOP, JUNGLE, MIDDLE, BOTTOM ou UTILITY (ou vazia)"}
	}
	agora := banco.Agora()
	res, err := db.Exec(`INSERT INTO estudos (nome, puuid, campeao_id, posicao, criado_em, atualizado_em) VALUES (?,?,?,?,?,?)`,
		nome, strings.TrimSpace(n.Puuid), camp, pos, agora, agora)
	if err != nil {
		return Estudo{}, err
	}
	id, _ := res.LastInsertId()
	return Le(db, id)
}

// Edita muda nome e filtros (o jogador não muda: é outro estudo).
func Edita(db *banco.DB, id int64, n Novo) (Estudo, error) {
	e, err := Le(db, id)
	if err != nil {
		return e, err
	}
	if n.Nome != nil {
		if s := strings.TrimSpace(*n.Nome); s != "" {
			e.Nome = s
		} else {
			return e, &ErroPedido{"o nome não pode ficar vazio"}
		}
	}
	if n.CampeaoID != nil {
		e.CampeaoID = *n.CampeaoID
	}
	if n.Posicao != nil {
		e.Posicao = strings.ToUpper(strings.TrimSpace(*n.Posicao))
	}
	if !posicoes[e.Posicao] || e.CampeaoID < 0 {
		return e, &ErroPedido{"posição precisa ser TOP, JUNGLE, MIDDLE, BOTTOM ou UTILITY (ou vazia)"}
	}
	if _, err := db.Exec(`UPDATE estudos SET nome = ?, campeao_id = ?, posicao = ?, atualizado_em = ? WHERE id = ?`,
		e.Nome, e.CampeaoID, e.Posicao, banco.Agora(), id); err != nil {
		return e, err
	}
	return Le(db, id)
}

// Apaga tira o estudo e a lista dele.
func Apaga(db *banco.DB, id int64) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`DELETE FROM estudos WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNaoEncontrado
	}
	if _, err := tx.Exec(`DELETE FROM estudo_partidas WHERE estudo_id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

const colunasEstudo = `e.id, e.nome, e.puuid, e.campeao_id, e.posicao, e.criado_em, e.atualizado_em,
	(SELECT COUNT(*) FROM estudo_partidas p WHERE p.estudo_id = e.id AND p.resultado = 'vitoria'),
	(SELECT COUNT(*) FROM estudo_partidas p WHERE p.estudo_id = e.id AND p.resultado = 'derrota')`

func leEstudos(db *banco.DB, filtro string, args ...any) ([]Estudo, error) {
	linhas, err := db.Query(`SELECT `+colunasEstudo+` FROM estudos e `+filtro, args...)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	l := []Estudo{}
	for linhas.Next() {
		var e Estudo
		if err := linhas.Scan(&e.ID, &e.Nome, &e.Puuid, &e.CampeaoID, &e.Posicao, &e.CriadoEm, &e.AtualizadoEm, &e.Vitorias,
			&e.Derrotas); err != nil {
			return nil, err
		}
		l = append(l, e)
	}
	if err := linhas.Err(); err != nil {
		return nil, err
	}
	for i := range l {
		completa(db, &l[i])
	}
	return l, nil
}

// completa põe o Riot ID e o nome do campeão (do que o banco já tem).
func completa(db *banco.DB, e *Estudo) {
	var nome, tag string
	if db.QueryRow(`SELECT nome, tag FROM jogadores WHERE puuid = ?`, e.Puuid).Scan(&nome, &tag) != nil {
		db.QueryRow(`SELECT nome, tag FROM participantes WHERE puuid = ? ORDER BY game_id DESC LIMIT 1`, e.Puuid).Scan(&nome, &tag)
	}
	if nome != "" {
		e.Jogador = nome + "#" + tag
	}
	if e.CampeaoID > 0 {
		db.QueryRow(`SELECT campeao FROM participantes WHERE campeao_id = ? LIMIT 1`, e.CampeaoID).Scan(&e.Campeao)
	}
}

// Lista devolve os estudos (o mais mexido primeiro).
func Lista(db *banco.DB) ([]Estudo, error) {
	return leEstudos(db, `ORDER BY e.atualizado_em DESC, e.id DESC`)
}

// Le devolve um estudo.
func Le(db *banco.DB, id int64) (Estudo, error) {
	l, err := leEstudos(db, `WHERE e.id = ?`, id)
	if err != nil {
		return Estudo{}, err
	}
	if len(l) == 0 {
		return Estudo{}, ErrNaoEncontrado
	}
	return l[0], nil
}

// Partidas devolve a lista do estudo, da mais nova para a mais antiga.
func Partidas(db *banco.DB, e Estudo) ([]PartidaDoEstudo, error) {
	linhas, err := db.Query(`SELECT game_id, resultado, anotacao, adicionada_em, atualizada_em FROM estudo_partidas
		WHERE estudo_id = ?`, e.ID)
	if err != nil {
		return nil, err
	}
	var l []PartidaDoEstudo
	for linhas.Next() {
		var p PartidaDoEstudo
		if err := linhas.Scan(&p.GameID, &p.Resultado, &p.Anotacao, &p.AdicionadaEm, &p.AtualizadaEm); err != nil {
			linhas.Close()
			return nil, err
		}
		l = append(l, p)
	}
	linhas.Close()
	for i := range l {
		completaPartida(db, e, &l[i])
	}
	sort.SliceStable(l, func(a, b int) bool { return l[a].CriadaEm > l[b].CriadaEm })
	if l == nil {
		l = []PartidaDoEstudo{}
	}
	return l, nil
}

func completaPartida(db *banco.DB, e Estudo, p *PartidaDoEstudo) {
	p.Rastreados = []int{}
	pt, err := partida.Le(db, p.GameID)
	if err != nil {
		return
	}
	p.CriadaEm, p.DuracaoS = pt.CriadaEm, pt.DuracaoS
	for _, x := range pt.Participantes {
		if x.Puuid == e.Puuid {
			p.ParticipanteID, p.CampeaoID, p.Campeao, p.Posicao = x.ParticipanteID, x.CampeaoID, x.Campeao, x.Posicao
			p.Abates, p.Mortes, p.Assistencias = x.Abates, x.Mortes, x.Assistencias
		}
	}
	p.ForaDoFiltro = (e.CampeaoID > 0 && p.CampeaoID != e.CampeaoID) || (e.Posicao != "" && p.Posicao != e.Posicao)
	if r, ok, err := analise.LeResumo(db, p.GameID); err == nil && ok {
		p.Analisada, p.Rastreados = true, r.Rastreados
		for _, pid := range r.Rastreados {
			if pid == p.ParticipanteID {
				p.Rastreado = true
			}
		}
	}
}

// AdicionaPartida põe uma partida na lista. O resultado vem dos dados da
// partida (participantes.venceu), não da pessoa. Se a partida ainda não foi
// analisada, analisa agora (os indicadores da linha do tempo saem mesmo sem
// rastreio).
func AdicionaPartida(ctx context.Context, d analise.Deps, id, gameID int64, anotacao string) (PartidaDoEstudo, error) {
	db := d.DB
	e, err := Le(db, id)
	if err != nil {
		return PartidaDoEstudo{}, err
	}
	pt, err := partida.Le(db, gameID)
	if err != nil {
		return PartidaDoEstudo{}, err
	}
	var eu *partida.Participante
	for i := range pt.Participantes {
		if pt.Participantes[i].Puuid == e.Puuid {
			eu = &pt.Participantes[i]
		}
	}
	if eu == nil {
		if !pt.Completa {
			return PartidaDoEstudo{}, &ErroPedido{"a partida ainda não tem os 10 participantes; abra a partida antes"}
		}
		return PartidaDoEstudo{}, &ErroPedido{"o jogador deste estudo não está nesta partida"}
	}
	res := Derrota
	if eu.Venceu {
		res = Vitoria
	}
	agora := banco.Agora()
	if _, err := db.Exec(`INSERT INTO estudo_partidas (estudo_id, game_id, resultado, anotacao, adicionada_em, atualizada_em)
		VALUES (?,?,?,?,?,?)`, id, gameID, res, anotacao, agora, agora); err != nil {
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM estudo_partidas WHERE estudo_id = ? AND game_id = ?`, id, gameID).Scan(&n)
		if n > 0 {
			return PartidaDoEstudo{}, &ErroConflito{"esta partida já está no estudo"}
		}
		return PartidaDoEstudo{}, err
	}
	db.Exec(`UPDATE estudos SET atualizado_em = ? WHERE id = ?`, agora, id)
	if _, ok, _ := analise.LeResumo(db, gameID); !ok {
		// Sem a linha do tempo não há o que calcular ainda: a partida entra e
		// a comparação a conta quando tiver indicadores.
		analise.Recalcula(ctx, d, gameID)
	}
	return LePartida(db, id, gameID)
}

// LePartida devolve uma partida do estudo.
func LePartida(db *banco.DB, id, gameID int64) (PartidaDoEstudo, error) {
	e, err := Le(db, id)
	if err != nil {
		return PartidaDoEstudo{}, err
	}
	p := PartidaDoEstudo{GameID: gameID}
	err = db.QueryRow(`SELECT resultado, anotacao, adicionada_em, atualizada_em FROM estudo_partidas WHERE estudo_id = ? AND game_id = ?`,
		id, gameID).Scan(&p.Resultado, &p.Anotacao, &p.AdicionadaEm, &p.AtualizadaEm)
	if err == sql.ErrNoRows {
		return p, ErrPartidaFora
	}
	if err != nil {
		return p, err
	}
	completaPartida(db, e, &p)
	return p, nil
}

// ErrPartidaFora: a partida não está no estudo.
var ErrPartidaFora = errors.New("esta partida não está no estudo")

// Anota troca a anotação da partida no estudo.
func Anota(db *banco.DB, id, gameID int64, texto string) (PartidaDoEstudo, error) {
	if len(texto) > 20000 {
		return PartidaDoEstudo{}, &ErroPedido{"anotação grande demais (máximo 20 000 caracteres)"}
	}
	res, err := db.Exec(`UPDATE estudo_partidas SET anotacao = ?, atualizada_em = ? WHERE estudo_id = ? AND game_id = ?`,
		texto, banco.Agora(), id, gameID)
	if err != nil {
		return PartidaDoEstudo{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := Le(db, id); err != nil {
			return PartidaDoEstudo{}, err
		}
		return PartidaDoEstudo{}, ErrPartidaFora
	}
	return LePartida(db, id, gameID)
}

// TiraPartida tira a partida da lista.
func TiraPartida(db *banco.DB, id, gameID int64) error {
	if _, err := Le(db, id); err != nil {
		return err
	}
	res, err := db.Exec(`DELETE FROM estudo_partidas WHERE estudo_id = ? AND game_id = ?`, id, gameID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrPartidaFora
	}
	db.Exec(`UPDATE estudos SET atualizado_em = ? WHERE id = ?`, banco.Agora(), id)
	return nil
}

// Sugestao é uma partida do jogador no banco que ainda não está no estudo.
type Sugestao struct {
	PartidaDoEstudo
	TemLinhaDoTempo bool `json:"tem_linha_do_tempo"`
}

// Sugestoes: as partidas do jogador que o banco conhece, fora da lista, que
// cabem no filtro do estudo. A escolha continua sendo da pessoa.
func Sugestoes(db *banco.DB, id int64) ([]Sugestao, error) {
	e, err := Le(db, id)
	if err != nil {
		return nil, err
	}
	linhas, err := db.Query(`SELECT p.game_id, p.venceu FROM participantes p WHERE p.puuid = ?
		AND p.game_id NOT IN (SELECT game_id FROM estudo_partidas WHERE estudo_id = ?)
		AND (? = 0 OR p.campeao_id = ?) AND (? = '' OR p.posicao = ?) ORDER BY p.game_id DESC`,
		e.Puuid, id, e.CampeaoID, e.CampeaoID, e.Posicao, e.Posicao)
	if err != nil {
		return nil, err
	}
	var l []Sugestao
	for linhas.Next() {
		var s Sugestao
		var venceu bool
		if err := linhas.Scan(&s.GameID, &venceu); err != nil {
			linhas.Close()
			return nil, err
		}
		s.Resultado = Derrota
		if venceu {
			s.Resultado = Vitoria
		}
		l = append(l, s)
	}
	linhas.Close()
	for i := range l {
		completaPartida(db, e, &l[i].PartidaDoEstudo)
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM linhas_do_tempo WHERE game_id = ?`, l[i].GameID).Scan(&n)
		l[i].TemLinhaDoTempo = n > 0
	}
	if l == nil {
		l = []Sugestao{}
	}
	return l, nil
}

// --- comparação -------------------------------------------------------------------

// MinimoPorLado: com menos partidas que isso de um lado, a linha é marcada
// como insuficiente.
const MinimoPorLado = 3

// Ressalva vai em toda comparação.
const Ressalva = "Comparação por conta, com as partidas que você escolheu. Com poucas partidas, uma diferença pode ser acaso; " +
	"diferença não é causa. Linhas marcadas como insuficientes têm menos de 3 partidas de um dos lados."

// Lado é o resumo de um indicador num dos lados (vitórias ou derrotas).
type Lado struct {
	N       int      `json:"n"`
	Media   *float64 `json:"media"`
	Mediana *float64 `json:"mediana"`
	Min     *float64 `json:"min"`
	Max     *float64 `json:"max"`
	DP      *float64 `json:"desvio"`
}

// LinhaComparacao é um indicador comparado.
type LinhaComparacao struct {
	Indicador string `json:"indicador"`
	Rotulo    string `json:"rotulo"`
	Unidade   string `json:"unidade"`
	Fonte     string `json:"fonte"`
	Melhor    string `json:"melhor"`
	Vitorias  Lado   `json:"vitorias"`
	Derrotas  Lado   `json:"derrotas"`
	// Diferenca = média nas vitórias − média nas derrotas.
	Diferenca *float64 `json:"diferenca"`
	// Efeito = d de Cohen (diferença ÷ desvio-padrão combinado): ~0,2
	// pequeno, ~0,5 médio, ≥0,8 grande. Nulo sem variação ou sem n.
	Efeito       *float64 `json:"efeito"`
	Insuficiente bool     `json:"insuficiente"`
	Aproximado   bool     `json:"aproximado"` // algum valor veio de rastreio parcial/posição aproximada
	Observacao   string   `json:"observacao"`
}

// Comparacao é a resposta da comparação.
type Comparacao struct {
	Estudo     Estudo            `json:"estudo"`
	Vitorias   int               `json:"vitorias"`
	Derrotas   int               `json:"derrotas"`
	SemAnalise []int64           `json:"sem_analise"` // partidas da lista sem indicadores
	Ressalva   string            `json:"ressalva"`
	Linhas     []LinhaComparacao `json:"linhas"`
}

// valoresDoEstudo: partida → nome → valor, e o resultado de cada partida.
func valoresDoEstudo(db *banco.DB, e Estudo) ([]PartidaDoEstudo, map[int64]map[string]analise.ValorIndicador, error) {
	ps, err := Partidas(db, e)
	if err != nil {
		return nil, nil, err
	}
	pid := map[int64]int{}
	for _, p := range ps {
		if p.ParticipanteID > 0 {
			pid[p.GameID] = p.ParticipanteID
		}
	}
	vs, err := analise.IndicadoresDoJogador(db, pid)
	return ps, vs, err
}

// Compara monta a comparação vitórias × derrotas, ordenada pelos indicadores
// que mais diferem (tamanho de efeito; os insuficientes no fim).
func Compara(db *banco.DB, id int64) (Comparacao, error) {
	e, err := Le(db, id)
	if err != nil {
		return Comparacao{}, err
	}
	c := Comparacao{Estudo: e, Ressalva: Ressalva, SemAnalise: []int64{}, Linhas: []LinhaComparacao{}}
	ps, vs, err := valoresDoEstudo(db, e)
	if err != nil {
		return c, err
	}
	for _, p := range ps {
		if p.Resultado == Vitoria {
			c.Vitorias++
		} else {
			c.Derrotas++
		}
		if len(vs[p.GameID]) == 0 {
			c.SemAnalise = append(c.SemAnalise, p.GameID)
		}
	}
	for _, def := range analise.Vocabulario {
		var v, d []float64
		aprox := false
		for _, p := range ps {
			x, ok := vs[p.GameID][def.Nome]
			if !ok || x.Valor == nil {
				continue
			}
			aprox = aprox || x.Aproximado
			if p.Resultado == Vitoria {
				v = append(v, *x.Valor)
			} else {
				d = append(d, *x.Valor)
			}
		}
		if len(v)+len(d) == 0 {
			continue
		}
		l := LinhaComparacao{Indicador: def.Nome, Rotulo: def.Rotulo, Unidade: def.Unidade, Fonte: def.Fonte, Melhor: def.Melhor,
			Vitorias: resumo(v), Derrotas: resumo(d), Aproximado: aprox}
		l.Insuficiente = len(v) < MinimoPorLado || len(d) < MinimoPorLado
		if len(v) > 0 && len(d) > 0 {
			dif := *l.Vitorias.Media - *l.Derrotas.Media
			l.Diferenca = arred(dif)
			l.Efeito = cohen(v, d)
		}
		l.Observacao = observacao(l)
		c.Linhas = append(c.Linhas, l)
	}
	sort.SliceStable(c.Linhas, func(a, b int) bool {
		la, lb := c.Linhas[a], c.Linhas[b]
		if la.Insuficiente != lb.Insuficiente {
			return !la.Insuficiente
		}
		return abs(la.Efeito) > abs(lb.Efeito)
	})
	return c, nil
}

func abs(p *float64) float64 {
	if p == nil {
		return -1
	}
	return math.Abs(*p)
}

func arred(v float64) *float64 {
	r := math.Round(v*100) / 100
	return &r
}

func resumo(xs []float64) Lado {
	l := Lado{N: len(xs)}
	if len(xs) == 0 {
		return l
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	soma := 0.0
	for _, x := range s {
		soma += x
	}
	m := soma / float64(len(s))
	med := s[len(s)/2]
	if len(s)%2 == 0 {
		med = (s[len(s)/2-1] + s[len(s)/2]) / 2
	}
	l.Media, l.Mediana, l.Min, l.Max = arred(m), arred(med), arred(s[0]), arred(s[len(s)-1])
	if len(s) > 1 {
		q := 0.0
		for _, x := range s {
			q += (x - m) * (x - m)
		}
		l.DP = arred(math.Sqrt(q / float64(len(s)-1)))
	}
	return l
}

// cohen: (média v − média d) ÷ desvio combinado. Nulo se não dá para calcular.
func cohen(v, d []float64) *float64 {
	nv, nd := float64(len(v)), float64(len(d))
	if nv+nd < 3 || nv == 0 || nd == 0 {
		return nil
	}
	media := func(xs []float64) float64 {
		s := 0.0
		for _, x := range xs {
			s += x
		}
		return s / float64(len(xs))
	}
	mv, md := media(v), media(d)
	q := 0.0
	for _, x := range v {
		q += (x - mv) * (x - mv)
	}
	for _, x := range d {
		q += (x - md) * (x - md)
	}
	sp := math.Sqrt(q / (nv + nd - 2))
	if sp == 0 {
		return nil
	}
	return arred((mv - md) / sp)
}

func observacao(l LinhaComparacao) string {
	if l.Diferenca == nil {
		return "só há partidas de um dos lados"
	}
	var s string
	switch {
	case *l.Diferenca > 0:
		s = "maior nas vitórias"
	case *l.Diferenca < 0:
		s = "maior nas derrotas"
	default:
		s = "igual nos dois lados"
	}
	if l.Efeito != nil {
		e := math.Abs(*l.Efeito)
		switch {
		case e >= 0.8:
			s += "; diferença grande"
		case e >= 0.5:
			s += "; diferença média"
		case e >= 0.2:
			s += "; diferença pequena"
		default:
			s += "; quase nenhuma diferença"
		}
	}
	if l.Insuficiente {
		s += fmt.Sprintf(" (amostra insuficiente: %d × %d partidas)", l.Vitorias.N, l.Derrotas.N)
	}
	return s
}

// ValorNaPartida é um indicador numa partida do estudo, com os momentos.
type ValorNaPartida struct {
	GameID         int64             `json:"game_id"`
	Resultado      string            `json:"resultado"`
	ParticipanteID int               `json:"participante_id"`
	CriadaEm       string            `json:"criada_em"`
	Campeao        string            `json:"campeao"`
	Valor          *float64          `json:"valor"`
	Aproximado     bool              `json:"aproximado"`
	Momentos       []analise.Momento `json:"momentos"`
	Detalhe        map[string]any    `json:"detalhe"`
}

// DoIndicador devolve, para um indicador, o valor em cada partida do estudo e
// os momentos que o compõem (para a tela mostrar exemplos).
func DoIndicador(db *banco.DB, id int64, nome string) (analise.DefIndicador, []ValorNaPartida, error) {
	def, ok := analise.DefDe(nome)
	if !ok {
		return def, nil, &ErroPedido{"indicador desconhecido: " + nome}
	}
	e, err := Le(db, id)
	if err != nil {
		return def, nil, err
	}
	ps, vs, err := valoresDoEstudo(db, e)
	if err != nil {
		return def, nil, err
	}
	l := []ValorNaPartida{}
	for _, p := range ps {
		x, ok := vs[p.GameID][nome]
		v := ValorNaPartida{GameID: p.GameID, Resultado: p.Resultado, ParticipanteID: p.ParticipanteID, CriadaEm: p.CriadaEm,
			Campeao: p.Campeao, Momentos: []analise.Momento{}, Detalhe: map[string]any{}}
		if ok {
			v.Valor, v.Aproximado = x.Valor, x.Aproximado
			if x.Detalhe != nil {
				v.Detalhe = x.Detalhe
				if ms, ok := x.Detalhe["momentos"]; ok {
					b, _ := json.Marshal(ms)
					json.Unmarshal(b, &v.Momentos)
					delete(v.Detalhe, "momentos")
				}
			}
		}
		l = append(l, v)
	}
	return def, l, nil
}
