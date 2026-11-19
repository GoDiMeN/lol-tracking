package aprendizado

import (
	"loltracking/server/analise"
	"loltracking/server/banco"
)

// O elo que a coleta por elo já guardou, para a base de fatos da análise
// (analise.RegistraFonteElo): só leitura do banco, nenhum pedido à Riot. A
// análise não importa este pacote (este é que importa a análise), por isso o
// registro é de cá.

type fonteElo struct{}

func init() { analise.RegistraFonteElo(fonteElo{}) }

// EloDoJogador: o da coleta mais recente em que o jogador (puuid do LCU) entrou.
func (fonteElo) EloDoJogador(db *banco.DB, puuid string) (analise.EloConhecido, bool) {
	var e analise.EloConhecido
	if puuid == "" {
		return e, false
	}
	err := db.QueryRow(`SELECT elo, divisao, lp, vitorias, derrotas, hot_streak FROM aprendizado_jogadores
		WHERE puuid_lcu = ? ORDER BY id DESC LIMIT 1`, puuid).Scan(&e.Elo, &e.Divisao, &e.PDL, &e.Vitorias, &e.Derrotas, &e.EmSequencia)
	if err != nil {
		// Sem linha, ou a tabela ainda não existe (banco sem a migração do
		// aprendizado, como nos testes da análise): não se sabe.
		return e, false
	}
	e.Fonte = "coleta por elo (API da Riot, league-v4, no dia da coleta)"
	return e, true
}

// EloDaPartida: o elo da coleta em que a partida entrou: o do jogador que a
// escolheu (um aproveitado de outro elo não muda o elo da partida; o elo dele
// fica no resumo do jogador).
func (fonteElo) EloDaPartida(db *banco.DB, gameID int64) (string, bool) {
	var elo string
	if err := db.QueryRow(`SELECT elo FROM aprendizado_partidas WHERE game_id = ? ORDER BY aproveitado, id DESC LIMIT 1`, gameID).Scan(&elo); err != nil {
		return "", false
	}
	return elo, elo != ""
}
