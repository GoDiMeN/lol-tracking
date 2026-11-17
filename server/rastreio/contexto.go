package rastreio

import (
	"sort"
	"sync"

	"loltracking/server/banco"
)

// O contexto que os geradores da cadeia recebem vai além do próprio passe: a
// posição do jogador na partida, o time de cada um, as amostras dos outros
// jogadores já rastreados (para "quem estava perto de quem") e o que outros
// pacotes acrescentarem (RegistraContexto; a análise põe os eventos da Riot).
// Com a partida rastreada aos poucos, o contexto melhora a cada passe: por isso
// a análise refaz a cadeia de todos os passes da partida quando um termina.

// Enriquecedor acrescenta ao contexto o que for de outro pacote.
type Enriquecedor func(db *banco.DB, c *ContextoCadeia)

var (
	enriquecedoresMu sync.Mutex
	enriquecedores   = map[string]Enriquecedor{}
)

// RegistraContexto acrescenta (ou troca) um enriquecedor do contexto.
func RegistraContexto(nome string, f Enriquecedor) {
	enriquecedoresMu.Lock()
	defer enriquecedoresMu.Unlock()
	enriquecedores[nome] = f
}

// completaContexto preenche posição, times, os outros rastreados e roda os
// enriquecedores. Erro de leitura dos outros passes não impede a cadeia: o
// gerador que precisa deles trata a falta.
func completaContexto(db *banco.DB, c *ContextoCadeia) {
	c.Times = map[int]int{}
	c.Posicoes = map[int]string{}
	if linhas, err := db.Query(`SELECT participante_id, time, posicao FROM participantes WHERE game_id = ?`, c.GameID); err == nil {
		for linhas.Next() {
			var pid, time int
			var pos string
			if linhas.Scan(&pid, &time, &pos) == nil {
				c.Times[pid], c.Posicoes[pid] = time, pos
			}
		}
		linhas.Close()
	}
	if c.Time == 0 {
		c.Time = c.Times[c.ParticipanteID]
	}
	c.Posicao = c.Posicoes[c.ParticipanteID]
	c.Outros = map[int][]Amostra{}
	for pid := range c.Times {
		if pid == c.ParticipanteID {
			continue
		}
		p, ok, err := melhorPasse(db, c.GameID, pid)
		if err != nil || !ok {
			continue
		}
		if as, _, err := amostrasDoPasse(db, p); err == nil && len(as) > 0 {
			c.Outros[pid] = as
		}
	}
	enriquecedoresMu.Lock()
	nomes := make([]string, 0, len(enriquecedores))
	for n := range enriquecedores {
		nomes = append(nomes, n)
	}
	sort.Strings(nomes)
	fs := make([]Enriquecedor, 0, len(nomes))
	for _, n := range nomes {
		fs = append(fs, enriquecedores[n])
	}
	enriquecedoresMu.Unlock()
	for _, f := range fs {
		f(db, c)
	}
}
