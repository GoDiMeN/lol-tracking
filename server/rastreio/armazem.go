package rastreio

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strconv"

	"loltracking/server/arquivo"
	"loltracking/server/banco"
)

// O que é pesado no passe fica em arquivo (pacote arquivo), um por conteúdo:
//
//	passe-<id>-amostras.ltc       bloco colunar das amostras (corpo sem flate)
//	passe-<id>-amostras_time.ltc  bloco colunar do estado dos 10
//	passe-<id>-cadeia.tsv         a cadeia: uma linha por item, "t_ms \t tipo \t dados JSON"
//
// Em passes_compactos fica só a referência (n, janela de tempo, bytes no
// disco). Só existe esta forma: um banco gravado antes dela é convertido uma
// vez na subida (converteFormatoAntigo) e perde a coluna blob e a tabela cadeia.

func nomeBloco(passeID int64, conteudo string) string {
	return fmt.Sprintf("passe-%d-%s.ltc", passeID, conteudo)
}

func nomeCadeia(passeID int64) string { return fmt.Sprintf("passe-%d-cadeia.tsv", passeID) }

// textoDaCadeia serializa os itens: uma linha por item, na ordem dada.
func textoDaCadeia(itens []ItemCadeia) []byte {
	var b bytes.Buffer
	for _, it := range itens {
		if it.Dados == nil {
			it.Dados = map[string]any{}
		}
		b.WriteString(strconv.FormatInt(it.TMs, 10))
		b.WriteByte('\t')
		b.WriteString(it.Tipo)
		b.WriteByte('\t')
		// json.Marshal escapa tabulação e quebra de linha: a linha é inteira.
		b.WriteString(jsonTexto(it.Dados))
		b.WriteByte('\n')
	}
	return b.Bytes()
}

func cadeiaDoTexto(texto []byte) ([]ItemCadeia, error) {
	itens := []ItemCadeia{}
	for n, linha := range bytes.Split(texto, []byte{'\n'}) {
		if len(linha) == 0 {
			continue
		}
		p := bytes.SplitN(linha, []byte{'\t'}, 3)
		if len(p) != 3 {
			return nil, fmt.Errorf("cadeia: linha %d fora do formato", n+1)
		}
		t, err := strconv.ParseInt(string(p[0]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("cadeia: linha %d: %w", n+1, err)
		}
		it := ItemCadeia{TMs: t, Tipo: string(p[1])}
		if err := json.Unmarshal(p[2], &it.Dados); err != nil {
			return nil, fmt.Errorf("cadeia: linha %d: %w", n+1, err)
		}
		itens = append(itens, it)
	}
	return itens, nil
}

// gravaCadeia grava a cadeia do passe em arquivo. Devolve os bytes em disco.
func gravaCadeia(db *banco.DB, passeID, gameID int64, itens []ItemCadeia) (int, error) {
	n, err := arquivo.Grava(db, gameID, nomeCadeia(passeID), textoDaCadeia(itens))
	if err != nil {
		return 0, fmt.Errorf("gravar a cadeia: %w", err)
	}
	return n, nil
}

// leCadeia devolve a cadeia do passe (ok falso = o passe não tem cadeia).
func leCadeia(db *banco.DB, passeID, gameID int64) ([]ItemCadeia, bool, error) {
	texto, ok, err := arquivo.Le(db, gameID, nomeCadeia(passeID))
	if err != nil || !ok {
		return nil, false, err
	}
	itens, err := cadeiaDoTexto(texto)
	return itens, err == nil, err
}

// leCompacto lê e decodifica um conteúdo compactado do passe (ok falso = o
// passe ainda não foi compactado).
func leCompacto(db *banco.DB, passeID int64, conteudo string) (Bloco, bool, error) {
	var gameID int64
	err := db.QueryRow(`SELECT p.game_id FROM passes_compactos c JOIN passes p ON p.id = c.passe_id
		WHERE c.passe_id = ? AND c.conteudo = ?`, passeID, conteudo).Scan(&gameID)
	if err == sql.ErrNoRows {
		return Bloco{}, false, nil
	}
	if err != nil {
		return Bloco{}, false, err
	}
	cru, ok, err := arquivo.Le(db, gameID, nomeBloco(passeID, conteudo))
	if err != nil {
		return Bloco{}, false, err
	}
	if !ok {
		return Bloco{}, false, fmt.Errorf("o arquivo do passe %d (%s) não está em %s", passeID, conteudo, arquivo.PastaDaPartida(db, gameID))
	}
	b, err := Decodifica(cru)
	return b, err == nil, err
}

// converteFormatoAntigo roda uma vez, na subida, num banco gravado antes dos
// arquivos: leva para arquivo os blobs de passes_compactos e as linhas da
// tabela cadeia, e depois REMOVE a coluna blob e a tabela cadeia. Daí em
// diante só existe a forma nova; nenhuma leitura conhece a antiga.
func converteFormatoAntigo(db *banco.DB) error {
	temBlob, temCadeia := false, false
	if err := db.QueryRow(`SELECT COUNT(*) > 0 FROM pragma_table_info('passes_compactos') WHERE name = 'blob'`).Scan(&temBlob); err != nil {
		return err
	}
	if err := db.QueryRow(`SELECT COUNT(*) > 0 FROM sqlite_master WHERE type = 'table' AND name = 'cadeia'`).Scan(&temCadeia); err != nil {
		return err
	}
	if !temBlob && !temCadeia {
		return nil
	}
	convertidos := 0
	if temBlob {
		type bloco struct {
			passeID, gameID int64
			conteudo        string
			blob            []byte
		}
		var bs []bloco
		linhas, err := db.Query(`SELECT c.passe_id, p.game_id, c.conteudo, c.blob FROM passes_compactos c
			JOIN passes p ON p.id = c.passe_id WHERE length(c.blob) > 0`)
		if err != nil {
			return err
		}
		for linhas.Next() {
			var b bloco
			if err := linhas.Scan(&b.passeID, &b.gameID, &b.conteudo, &b.blob); err != nil {
				linhas.Close()
				return err
			}
			bs = append(bs, b)
		}
		linhas.Close()
		if err := linhas.Err(); err != nil {
			return err
		}
		for _, b := range bs {
			bl, err := Decodifica(b.blob)
			if err != nil {
				return fmt.Errorf("converter o passe %d (%s): %w", b.passeID, b.conteudo, err)
			}
			// As amostras passam pelo formato atual: perdem as colunas que só
			// servem durante o passe.
			if b.conteudo == ConteudoAmostras {
				bl = BlocoDeAmostras(AmostrasDoBloco(bl))
			}
			cru, err := Codifica(bl)
			if err != nil {
				return err
			}
			n, err := arquivo.Grava(db, b.gameID, nomeBloco(b.passeID, b.conteudo), cru)
			if err != nil {
				return err
			}
			if _, err := db.Exec(`UPDATE passes_compactos SET bytes = ? WHERE passe_id = ? AND conteudo = ?`, n, b.passeID, b.conteudo); err != nil {
				return err
			}
			convertidos++
		}
		if _, err := db.Exec(`ALTER TABLE passes_compactos DROP COLUMN blob`); err != nil {
			return fmt.Errorf("remover a coluna blob: %w", err)
		}
	}
	if temCadeia {
		passes := map[int64]int64{}
		linhas, err := db.Query(`SELECT DISTINCT passe_id, game_id FROM cadeia`)
		if err != nil {
			return err
		}
		for linhas.Next() {
			var p, g int64
			if err := linhas.Scan(&p, &g); err != nil {
				linhas.Close()
				return err
			}
			passes[p] = g
		}
		linhas.Close()
		if err := linhas.Err(); err != nil {
			return err
		}
		for passeID, gameID := range passes {
			var itens []ItemCadeia
			ls, err := db.Query(`SELECT t_ms, tipo, dados FROM cadeia WHERE passe_id = ? ORDER BY t_ms, id`, passeID)
			if err != nil {
				return err
			}
			for ls.Next() {
				var it ItemCadeia
				var dados string
				if err := ls.Scan(&it.TMs, &it.Tipo, &dados); err != nil {
					ls.Close()
					return err
				}
				json.Unmarshal([]byte(dados), &it.Dados)
				itens = append(itens, it)
			}
			ls.Close()
			if _, err := gravaCadeia(db, passeID, gameID, itens); err != nil {
				return err
			}
			convertidos++
		}
		if _, err := db.Exec(`DROP TABLE cadeia`); err != nil {
			return fmt.Errorf("remover a tabela cadeia: %w", err)
		}
	}
	// Devolve ao disco o espaço que os blobs e a cadeia ocupavam.
	if _, err := db.Exec(`VACUUM`); err != nil {
		return fmt.Errorf("compactar o banco depois da conversão: %w", err)
	}
	log.Printf("rastreio: %d conteúdos levados do banco para arquivos (formato novo)", convertidos)
	return nil
}

// BytesDaCadeia devolve os bytes em disco da cadeia do passe (0 se não há).
func BytesDaCadeia(db *banco.DB, passeID, gameID int64) int64 {
	m, _ := arquivo.Tamanhos(db, gameID)
	return m[nomeCadeia(passeID)]
}
