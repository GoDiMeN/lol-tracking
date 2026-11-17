// Package lcu fala com a API local do cliente do League of Legends (LCU).
//
// Tudo o que o app pede ao cliente passa pela interface Cliente. Há duas
// implementações: Real (HTTPS em 127.0.0.1 com a senha do lockfile, recusando
// qualquer rota fora de permitidas.go antes de enviar) e Falso (determinístico,
// para os testes e para LOL_TESTE=1).
package lcu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Cliente é o que o app sabe pedir ao LCU. Cada método corresponde a uma rota
// de permitidas.go; o JSON das partidas volta cru porque é guardado inteiro no
// banco e lido de novo pelos próximos pacotes.
type Cliente interface {
	// InvocadorAtual é quem está logado no cliente.
	InvocadorAtual(ctx context.Context) (Invocador, error)
	// BuscaAlias acha o puuid por nome e tag. ErrNaoEncontrado se não existe.
	BuscaAlias(ctx context.Context, nome, tag string) (Invocador, error)
	// Historico devolve a resposta de matches entre inicio e fim (índices do LCU).
	Historico(ctx context.Context, puuid string, inicio, fim int) (json.RawMessage, error)
	// Partida é a partida inteira, com os 10 participantes.
	Partida(ctx context.Context, gameID int64) (json.RawMessage, error)
	// LinhaDoTempo são os quadros de minuto em minuto e os eventos.
	LinhaDoTempo(ctx context.Context, gameID int64) (json.RawMessage, error)
	// FaseGameflow é "None", "Lobby", "InProgress", "WatchInProgress"...
	FaseGameflow(ctx context.Context) (string, error)
	// Regiao é a região do cliente ("BR") e o idioma.
	Regiao(ctx context.Context) (Regiao, error)
	// ConfiguracaoReplays traz a versão do jogo instalado.
	ConfiguracaoReplays(ctx context.Context) (ConfigReplays, error)
	// PastaReplays é onde o cliente grava os .rofl.
	PastaReplays(ctx context.Context) (string, error)
	// MetadataReplay é o estado do replay. ErrNaoEncontrado quando o cliente
	// nunca viu a partida (aí é preciso CriaMetadataReplay).
	MetadataReplay(ctx context.Context, gameID int64) (MetadataReplay, error)
	// CriaMetadataReplay apresenta a partida ao plugin de replays (v2/.../create).
	CriaMetadataReplay(ctx context.Context, gameID int64, m CriaMetadata) error
	// BaixaReplay pede o download; o progresso aparece no MetadataReplay.
	BaixaReplay(ctx context.Context, gameID int64) error
	// AssisteReplay abre o replay no jogo. Só o rastreio usa, e sempre fecha.
	AssisteReplay(ctx context.Context, gameID int64) error
}

// Invocador é um jogador identificado.
type Invocador struct {
	Puuid string `json:"puuid"`
	Nome  string `json:"nome"`
	Tag   string `json:"tag"`
}

// Regiao vem de /riotclient/region-locale.
type Regiao struct {
	Regiao string `json:"region"`
	Locale string `json:"locale"`
}

// ConfigReplays é o pedaço de /lol-replays/v1/configuration que o app usa.
type ConfigReplays struct {
	VersaoJogo string `json:"gameVersion"`
	Habilitado bool   `json:"isReplaysEnabled"`
}

// MetadataReplay é a resposta de /lol-replays/v1/metadata/{id}.
type MetadataReplay struct {
	Estado    string  `json:"state"`
	Progresso float64 `json:"downloadProgress"`
}

// CriaMetadata é o corpo de POST /lol-replays/v2/metadata/{id}/create, com os
// nomes que o cliente espera.
type CriaMetadata struct {
	VersaoJogo string `json:"gameVersion"`
	TipoJogo   string `json:"gameType"`
	FilaID     int    `json:"queueId"`
	FimMs      int64  `json:"gameEnd"`
}

// ErrClienteFechado: sem lockfile, ou o lockfile aponta para um cliente que
// não atende mais. A frase é para a pessoa.
var ErrClienteFechado = errors.New("o cliente do League of Legends não está aberto; abra o cliente e entre na conta")

// ErrNaoEncontrado é o 404 do LCU.
var ErrNaoEncontrado = errors.New("o cliente do LoL não encontrou o que foi pedido")

// ErroLCU é uma resposta de erro do LCU que não é 404.
type ErroLCU struct {
	Status   int
	Mensagem string
}

func (e *ErroLCU) Error() string {
	if e.Mensagem == "" {
		return fmt.Sprintf("o cliente do LoL respondeu %d", e.Status)
	}
	return fmt.Sprintf("o cliente do LoL respondeu %d: %s", e.Status, e.Mensagem)
}

// TagDaRegiao é a tag que o Riot ID ganha por padrão em cada região, usada
// quando a pessoa digita só o nome. Região desconhecida: a própria região.
func TagDaRegiao(regiao string) string {
	tags := map[string]string{
		"BR": "br1", "NA": "na1", "EUW": "euw", "EUNE": "eune", "LAN": "lan", "LAS": "las",
		"OCE": "oce", "TR": "tr1", "RU": "ru", "JP": "jp1", "KR": "kr",
	}
	if t, ok := tags[regiao]; ok {
		return t
	}
	return regiao
}
