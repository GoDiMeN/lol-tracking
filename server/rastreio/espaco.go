package rastreio

import (
	"encoding/json"

	"loltracking/server/arquivo"
	"loltracking/server/banco"
)

// Espaco é quanto os passes de um conjunto de rastreios ocupam em disco: os
// blocos compactados (amostras e estado dos 10) e a cadeia. São os bytes dos
// arquivos, já comprimidos (pacote arquivo).
type Espaco struct {
	Passes     int   `json:"passes"`
	Compactado int64 `json:"compactado"`
	Cadeia     int64 `json:"cadeia"`
}

// EspacoDosRastreios soma o espaço dos passes fechados dos rastreios dados.
func EspacoDosRastreios(db *banco.DB, ids []int64) (Espaco, error) {
	var e Espaco
	if len(ids) == 0 {
		return e, nil
	}
	lista, _ := json.Marshal(ids)
	linhas, err := db.Query(`SELECT p.id, p.game_id, COALESCE(SUM(c.bytes), 0) FROM passes_compactos c
		JOIN passes p ON p.id = c.passe_id WHERE p.rastreio_id IN (SELECT value FROM json_each(?))
		GROUP BY p.id, p.game_id`, string(lista))
	if err != nil {
		return e, err
	}
	defer linhas.Close()
	// Os tamanhos dos arquivos de cada partida são lidos uma vez só.
	porPartida := map[int64]map[string]int64{}
	for linhas.Next() {
		var passeID, gameID, bytes int64
		if err := linhas.Scan(&passeID, &gameID, &bytes); err != nil {
			return e, err
		}
		e.Passes++
		e.Compactado += bytes
		if porPartida[gameID] == nil {
			porPartida[gameID], _ = arquivo.Tamanhos(db, gameID)
		}
		e.Cadeia += porPartida[gameID][nomeCadeia(passeID)]
	}
	return e, linhas.Err()
}
