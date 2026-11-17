package lcu

import (
	"fmt"
	"strings"
)

// A lista de rotas do LCU que este projeto pode chamar. É a regra 1 do
// CLAUDE.md em forma de código: só leitura de invocador, histórico e linha do
// tempo de partida, a fase do gameflow, a região, e o plugin de replays. Nada de
// lobby, fila, seleção de campeão, chat, loja, saque ou configuração de conta.
//
// O cliente confere esta lista ANTES de montar o pedido: rota fora dela nunca
// sai do processo. Mudar esta lista é decisão de segurança da conta, não
// detalhe de implementação — acrescente só com motivo escrito ao lado.
//
// Padrão: segmentos separados por "/"; um segmento entre chaves casa com um
// segmento do caminho real, e o nome diz o que ele aceita:
//   - {id}:    só dígitos (gameId);
//   - {puuid}: letras, dígitos, "-" e "_" (o puuid e o alias "current-summoner").
var rotasPermitidas = []struct {
	metodo string
	padrao string
}{
	// Quem está logado (nome, tag, puuid).
	{"GET", "/lol-summoner/v1/current-summoner"},
	// A busca de jogador por nome e tag.
	{"GET", "/lol-summoner/v1/alias/lookup"},
	// Histórico de partidas de qualquer puuid.
	{"GET", "/lol-match-history/v1/products/lol/{puuid}/matches"},
	// A partida inteira (os 10) e a linha do tempo por minuto.
	{"GET", "/lol-match-history/v1/games/{id}"},
	{"GET", "/lol-match-history/v1/game-timelines/{id}"},
	// A fase do cliente: é o que impede rastrear com partida ao vivo (regra 2).
	{"GET", "/lol-gameflow/v1/gameflow-phase"},
	// A região, para completar a tag quando a pessoa digita só o nome.
	{"GET", "/riotclient/region-locale"},
	// O plugin de replays: leitura do estado e as três ações permitidas.
	{"GET", "/lol-replays/v1/configuration"},
	{"GET", "/lol-replays/v1/rofls/path"},
	{"GET", "/lol-replays/v1/metadata/{id}"},
	{"POST", "/lol-replays/v2/metadata/{id}/create"},
	{"POST", "/lol-replays/v1/rofls/{id}/download"},
	// Abrir o replay: só o rastreio (tarefa 2) usa, sempre fechando ao terminar.
	{"POST", "/lol-replays/v1/rofls/{id}/watch"},
}

// ErroRotaProibida é devolvido quando alguém tenta chamar uma rota do LCU fora
// da lista. É erro de programação: o código do app nunca deveria pedir isso.
type ErroRotaProibida struct {
	Metodo, Caminho string
}

func (e *ErroRotaProibida) Error() string {
	return fmt.Sprintf("rota do LCU fora da lista permitida (server/lcu/permitidas.go): %s %s", e.Metodo, e.Caminho)
}

// Permitida diz se o método e o caminho (sem query) estão na lista. O caminho
// precisa estar na forma canônica: sem "..", sem "//", sem "%", sem "?" ou "#",
// sem barra no fim. Qualquer coisa estranha é recusada em vez de normalizada:
// normalizar é onde mora o truque para passar uma rota proibida.
func Permitida(metodo, caminho string) bool {
	if !caminhoCanonico(caminho) {
		return false
	}
	partes := strings.Split(caminho[1:], "/")
	for _, r := range rotasPermitidas {
		if r.metodo != metodo {
			continue
		}
		if casa(strings.Split(r.padrao[1:], "/"), partes) {
			return true
		}
	}
	return false
}

func caminhoCanonico(c string) bool {
	if !strings.HasPrefix(c, "/") || strings.HasSuffix(c, "/") || len(c) > 512 {
		return false
	}
	if strings.ContainsAny(c, "?#%\\ \t\r\n") || strings.Contains(c, "//") {
		return false
	}
	for _, seg := range strings.Split(c[1:], "/") {
		if seg == "." || seg == ".." || seg == "" {
			return false
		}
	}
	return true
}

func casa(padrao, partes []string) bool {
	if len(padrao) != len(partes) {
		return false
	}
	for i, p := range padrao {
		s := partes[i]
		switch p {
		case "{id}":
			if !soDigitos(s) {
				return false
			}
		case "{puuid}":
			if !puuidValido(s) {
				return false
			}
		default:
			if p != s {
				return false
			}
		}
	}
	return true
}

func soDigitos(s string) bool {
	if s == "" || len(s) > 20 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func puuidValido(s string) bool {
	if s == "" || len(s) > 100 {
		return false
	}
	for _, c := range s {
		ok := c == '-' || c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		if !ok {
			return false
		}
	}
	return true
}
