// Package jogador busca jogadores por Riot ID (Nome#TAG) no cliente do LoL e
// guarda os que já foram buscados, para a tela oferecer os recentes.
package jogador

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"loltracking/server/banco"
	"loltracking/server/lcu"
)

// Migra cria a tabela dos jogadores buscados.
func Migra(db *banco.DB) error {
	return banco.Executa(db, `CREATE TABLE IF NOT EXISTS jogadores (
		puuid      TEXT PRIMARY KEY,
		nome       TEXT NOT NULL,
		tag        TEXT NOT NULL,
		buscas     INTEGER NOT NULL DEFAULT 0,
		buscado_em TEXT NOT NULL,
		-- ordem: contador da última busca. A data tem resolução de segundo e
		-- duas buscas no mesmo segundo empatariam na lista de recentes.
		ordem      INTEGER NOT NULL DEFAULT 0,
		criado_em  TEXT NOT NULL
	)`, `CREATE INDEX IF NOT EXISTS jogadores_ordem ON jogadores (ordem DESC)`)
}

// Jogador é como o jogador aparece na API.
type Jogador struct {
	Puuid     string `json:"puuid"`
	Nome      string `json:"nome"`
	Tag       string `json:"tag"`
	RiotID    string `json:"riot_id"`
	Buscas    int    `json:"buscas"`
	BuscadoEm string `json:"buscado_em"`
}

// ErroInvalido é pedido malformado; a mensagem vai para a pessoa num 400.
type ErroInvalido struct{ Msg string }

func (e *ErroInvalido) Error() string { return e.Msg }

// ErrNaoEncontrado: o Riot ID não existe (ou o puuid não foi buscado ainda).
var ErrNaoEncontrado = errors.New("jogador não encontrado")

// SeparaRiotID divide "Nome#TAG". Sem "#", a tag é a padrão da região. O nome
// pode ter espaços ("Nacht Faust"); o "#" que vale é o último.
func SeparaRiotID(texto, tagPadrao string) (nome, tag string, err error) {
	texto = strings.TrimSpace(texto)
	if texto == "" {
		return "", "", &ErroInvalido{"digite o nome do jogador, como Nome#TAG"}
	}
	nome, tag = texto, ""
	if i := strings.LastIndex(texto, "#"); i >= 0 {
		nome, tag = strings.TrimSpace(texto[:i]), strings.TrimSpace(texto[i+1:])
	}
	if tag == "" {
		tag = tagPadrao
	}
	if nome == "" {
		return "", "", &ErroInvalido{"falta o nome antes do #"}
	}
	if len([]rune(nome)) > 32 || len([]rune(tag)) > 10 || strings.ContainsAny(nome+tag, "#/\\?") {
		return "", "", &ErroInvalido{"o nome não parece um Riot ID (Nome#TAG)"}
	}
	return nome, tag, nil
}

// Busca acha o jogador pelo Riot ID no cliente do LoL e o grava (ou atualiza)
// em jogadores.
func Busca(ctx context.Context, db *banco.DB, cli lcu.Cliente, texto string) (Jogador, error) {
	tagPadrao := ""
	if !strings.Contains(texto, "#") || strings.HasSuffix(strings.TrimSpace(texto), "#") {
		reg, err := cli.Regiao(ctx)
		if err != nil {
			return Jogador{}, err
		}
		tagPadrao = lcu.TagDaRegiao(reg.Regiao)
	}
	nome, tag, err := SeparaRiotID(texto, tagPadrao)
	if err != nil {
		return Jogador{}, err
	}
	inv, err := cli.BuscaAlias(ctx, nome, tag)
	if errors.Is(err, lcu.ErrNaoEncontrado) {
		return Jogador{}, fmt.Errorf("%w: %s#%s", ErrNaoEncontrado, nome, tag)
	}
	if err != nil {
		return Jogador{}, err
	}
	return Grava(db, inv)
}

// Grava insere ou atualiza o jogador e conta mais uma busca. O nome e a tag
// são sempre os mais novos: o Riot ID pode mudar, o puuid não.
func Grava(db *banco.DB, inv lcu.Invocador) (Jogador, error) {
	agora := banco.Agora()
	_, err := db.Exec(`INSERT INTO jogadores (puuid, nome, tag, buscas, buscado_em, ordem, criado_em)
		VALUES (?, ?, ?, 1, ?, (SELECT COALESCE(MAX(ordem), 0) + 1 FROM jogadores), ?)
		ON CONFLICT (puuid) DO UPDATE SET nome = excluded.nome, tag = excluded.tag,
			buscas = jogadores.buscas + 1, buscado_em = excluded.buscado_em, ordem = excluded.ordem`,
		inv.Puuid, inv.Nome, inv.Tag, agora, agora)
	if err != nil {
		return Jogador{}, fmt.Errorf("gravar jogador: %w", err)
	}
	return Le(db, inv.Puuid)
}

const colunas = `puuid, nome, tag, buscas, buscado_em`

func escaneia(s interface{ Scan(...any) error }) (Jogador, error) {
	var j Jogador
	err := s.Scan(&j.Puuid, &j.Nome, &j.Tag, &j.Buscas, &j.BuscadoEm)
	j.RiotID = j.Nome + "#" + j.Tag
	return j, err
}

// Le devolve um jogador já buscado.
func Le(db *banco.DB, puuid string) (Jogador, error) {
	j, err := escaneia(db.QueryRow(`SELECT `+colunas+` FROM jogadores WHERE puuid = ?`, puuid))
	if errors.Is(err, sql.ErrNoRows) {
		return Jogador{}, ErrNaoEncontrado
	}
	return j, err
}

// Recentes lista os últimos jogadores buscados, do mais recente ao mais antigo.
func Recentes(db *banco.DB, limite int) ([]Jogador, error) {
	linhas, err := db.Query(`SELECT `+colunas+` FROM jogadores ORDER BY ordem DESC LIMIT ?`, limite)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	var lista []Jogador
	for linhas.Next() {
		j, err := escaneia(linhas)
		if err != nil {
			return nil, err
		}
		lista = append(lista, j)
	}
	return lista, linhas.Err()
}
