package aprendizado

import (
	"encoding/json"
	"sort"

	"loltracking/server/analise"
	"loltracking/server/rastreio"
	"loltracking/server/riot"
)

func textoDoEvento(dados json.RawMessage) string {
	var d struct {
		Texto string `json:"texto"`
	}
	json.Unmarshal(dados, &d)
	return d.Texto
}

// passesPorPartida: só o jogador escolhido ou os 10.
func passesPorPartida(quem string) int {
	if quem == QuemTodos {
		return 10
	}
	return 1
}

// Detalhe é a coleta com elos, jogadores e partidas (GET .../coletas/{id}).
type Detalhe struct {
	Coleta    Coleta            `json:"coleta"`
	Elos      []EloDaColeta     `json:"elos"`
	Jogadores []JogadorDaColeta `json:"jogadores"`
	Partidas  []PartidaDaColeta `json:"partidas"`
}

// Le devolve a coleta com o resumo do progresso.
func (k *Coletor) Le(id int64) (Coleta, error) {
	d, err := k.Detalhe(id)
	return d.Coleta, err
}

// Lista devolve as coletas, da mais nova à mais velha, com o resumo.
func (k *Coletor) Lista() ([]Coleta, error) {
	// O rastreio de um estudo é uma coleta por dentro, mas aparece no estudo, não aqui.
	l, err := leColetas(k.d.DB, `WHERE estudo_id = 0 ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	for i := range l {
		d, err := k.Detalhe(l[i].ID)
		if err != nil {
			return nil, err
		}
		l[i] = d.Coleta
	}
	return l, nil
}

// Detalhe monta a coleta inteira e calcula progresso e estimativa.
func (k *Coletor) Detalhe(id int64) (Detalhe, error) {
	col, err := LeColeta(k.d.DB, id)
	if err != nil {
		return Detalhe{}, err
	}
	d := Detalhe{Coleta: col}
	if r, ok := riot.RegiaoPorCodigo(col.Regiao); ok {
		d.Coleta.RegiaoNome = r.Nome
	} else {
		d.Coleta.RegiaoNome = col.Regiao
	}
	if d.Elos, err = leElos(k.d.DB, id); err != nil {
		return d, err
	}
	if d.Jogadores, err = leJogadores(k.d.DB, `WHERE coleta_id = ? ORDER BY elo, posicao`, id); err != nil {
		return d, err
	}
	// Na ordem dos elos da coleta.
	ordem := map[string]int{}
	for _, e := range d.Elos {
		ordem[e.Elo] = e.ordem
	}
	js := d.Jogadores
	sort.SliceStable(js, func(a, b int) bool { return ordem[js[a].Elo] < ordem[js[b].Elo] })
	if d.Partidas, err = lePartidas(k.d.DB, `WHERE p.coleta_id = ?`+ordemDasPartidas, id); err != nil {
		return d, err
	}
	ppp := passesPorPartida(col.Quem)
	c := &d.Coleta
	c.Jogadores = len(d.Jogadores)
	for _, j := range d.Jogadores {
		if j.Estado == JogadorSemPartida {
			c.JogadoresSemPartida++
		}
	}
	feito, restanteS := 0.0, 0.0
	for i := range d.Partidas {
		p := &d.Partidas[i]
		p.PassesTotal = ppp
		frac := 0.0
		if p.RastreioID != nil {
			if r, err := rastreio.LeRastreio(k.d.DB, *p.RastreioID); err == nil {
				p.PassesTotal, p.PassesConcluidos, frac = r.PassesTotal, r.PassesConcluidos, r.Progresso
			}
		}
		c.PartidasTotal++
		switch p.Estado {
		case PartidaConcluida:
			c.PartidasConcluidas++
			frac = 1
		case PartidaErro, PartidaCancelada:
			if p.Estado == PartidaErro {
				c.PartidasComErro++
			}
			// Não vai mais rodar: conta como feita para o progresso.
			feito += float64(p.PassesTotal) * (1 - frac)
		}
		p.progresso = frac
		c.PassesTotal += p.PassesTotal
		c.PassesConcluidos += p.PassesConcluidos
		feito += float64(p.PassesTotal) * frac
		if p.Estado != PartidaConcluida && p.Estado != PartidaErro && p.Estado != PartidaCancelada {
			dur := float64(p.DuracaoS)
			if dur <= 0 {
				dur = SegundosPorPasseRef
			}
			restanteS += float64(p.PassesTotal) * (1 - frac) * (dur/col.Velocidade + SobraPorPasseS)
		}
	}
	// Elo ainda por descobrir: a estimativa de 10 jogadores × partidas.
	for _, e := range d.Elos {
		if e.Estado == EloPronto || col.SoDescobrir {
			continue
		}
		faltam := max(0, JogadoresPorElo-e.Jogadores) * col.PartidasPorJogador * ppp
		c.PassesTotal += faltam
		restanteS += float64(faltam) * (SegundosPorPasseRef/col.Velocidade + SobraPorPasseS)
	}
	if c.PassesTotal > 0 {
		c.Progresso = min(1, feito/float64(c.PassesTotal))
	}
	if c.Estado == Concluido {
		c.Progresso = 1
	}
	if c.Estado != Concluido && c.Estado != Cancelado {
		c.EstimativaS = restanteS
	}
	if ex, ok := k.ExecucaoDe(id); ok {
		c.Execucao = &IDExec{ID: ex}
	}
	k.mu.Lock()
	if a := k.atual; a != nil && a.coletaID == id {
		if g := a.partida.Load(); g != 0 {
			c.PartidaAtual = &g
		}
		if r := a.rastreio.Load(); r != 0 {
			c.RastreioAtual = &r
		}
	}
	k.mu.Unlock()
	d.Elos = nuncaNil(d.Elos)
	d.Jogadores = nuncaNil(d.Jogadores)
	d.Partidas = nuncaNil(d.Partidas)
	return d, nil
}

func nuncaNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// Espaco é quanto a coleta ocupa no banco (GET .../espaco).
type Espaco struct {
	Compactado      int64 `json:"compactado"`
	Cadeia          int64 `json:"cadeia"`
	Retratos        int64 `json:"retratos"`
	Indicadores     int64 `json:"indicadores"`
	Total           int64 `json:"total"`
	Passes          int   `json:"passes"`
	Partidas        int   `json:"partidas"`
	EstimativaTotal int64 `json:"estimativa_total"`
}

// EspacoDe soma os blocos compactos e a cadeia dos rastreios da coleta, e os
// retratos e indicadores das partidas dela. A estimativa ao terminar usa a
// média do que já foi feito ou, sem nada feito, a medida de referência (um
// passe ≈ 37 KB de blocos + 16 KB de cadeia; uma partida ≈ 45 KB de retratos,
// já em arquivo comprimido: partida 3304217746 rastreada de novo em 2026-11-17).
func (k *Coletor) EspacoDe(id int64) (Espaco, error) {
	d, err := k.Detalhe(id)
	if err != nil {
		return Espaco{}, err
	}
	var rids, gids []int64
	for _, p := range d.Partidas {
		if p.RastreioID != nil {
			rids = append(rids, *p.RastreioID)
			gids = append(gids, p.GameID)
		}
	}
	er, err := rastreio.EspacoDosRastreios(k.d.DB, rids)
	if err != nil {
		return Espaco{}, err
	}
	ret, ind, err := analise.EspacoDasPartidas(k.d.DB, gids)
	if err != nil {
		return Espaco{}, err
	}
	e := Espaco{Compactado: er.Compactado, Cadeia: er.Cadeia, Retratos: ret, Indicadores: ind, Passes: er.Passes, Partidas: len(gids)}
	e.Total = e.Compactado + e.Cadeia + e.Retratos + e.Indicadores
	porPasse, porPartida := int64(BytesPasseRef), int64(BytesPartidaRef)
	if e.Passes > 0 {
		porPasse = (e.Compactado + e.Cadeia) / int64(e.Passes)
	}
	if e.Partidas > 0 && e.Retratos+e.Indicadores > 0 {
		porPartida = (e.Retratos + e.Indicadores) / int64(e.Partidas)
	}
	partidas := d.Coleta.PartidasTotal
	for _, el := range d.Elos {
		if el.Estado != EloPronto && !d.Coleta.SoDescobrir {
			partidas += max(0, JogadoresPorElo-el.Jogadores) * d.Coleta.PartidasPorJogador
		}
	}
	e.EstimativaTotal = porPasse*int64(d.Coleta.PassesTotal) + porPartida*int64(partidas)
	return e, nil
}
