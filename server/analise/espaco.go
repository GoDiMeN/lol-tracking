package analise

import (
	"encoding/json"

	"loltracking/server/arquivo"
	"loltracking/server/banco"
)

// EspacoDasPartidas soma o que retratos e indicadores das partidas ocupam. Os
// retratos são os bytes do arquivo (já comprimido); os indicadores ficam no
// banco e a conta é aproximada (o texto das colunas mais ~24 bytes de números
// por linha; a sobra do SQLite fica de fora).
func EspacoDasPartidas(db *banco.DB, gameIDs []int64) (retratos, indicadores int64, err error) {
	if len(gameIDs) == 0 {
		return 0, 0, nil
	}
	for _, g := range gameIDs {
		m, _ := arquivo.Tamanhos(db, g)
		retratos += m[arquivoRetratos]
	}
	lista, _ := json.Marshal(gameIDs)
	err = db.QueryRow(`SELECT COALESCE(SUM(LENGTH(indicador) + LENGTH(detalhe) + LENGTH(calculado_em) + 24), 0) FROM indicadores
		WHERE game_id IN (SELECT value FROM json_each(?))`, string(lista)).Scan(&indicadores)
	return
}
