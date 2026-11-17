// Package estado responde GET /api/estado: o retrato do que o app enxerga
// agora (cliente do LoL conectado, quem está logado, versão do jogo, fase do
// gameflow, chave da Riot). A barra de título da interface vive disso.
//
// Não tem tabela nem model: é só leitura do LCU e da chave, por isso o pacote
// tem um controller e nada mais.
package estado

import (
	"context"
	"errors"
	"net/http"
	"time"

	"loltracking/server/lcu"
	"loltracking/server/riot"
	"loltracking/server/web"
)

// Estado é a resposta de GET /api/estado.
type Estado struct {
	LCU struct {
		Conectado bool   `json:"conectado"`
		Erro      string `json:"erro,omitempty"`
	} `json:"lcu"`
	Invocador          *lcu.Invocador `json:"invocador"`
	VersaoJogo         string         `json:"versao_jogo"`
	Patch              string         `json:"patch"`
	Fase               string         `json:"fase"`
	Regiao             string         `json:"regiao"`
	ReplaysHabilitados bool           `json:"replays_habilitados"`
	PastaReplays       string         `json:"pasta_replays"`
	ChaveRiot          riot.Estado    `json:"chave_riot"`
	ModoTeste          bool           `json:"modo_teste"`
}

// Monta consulta o LCU e a chave. Erro do LCU não é erro da rota: vira
// "conectado: false" com a frase, que é justamente o que a tela quer mostrar.
// O retorno tem nome porque o defer que junta a chave escreve nele.
func Monta(ctx context.Context, cli lcu.Cliente, chave *riot.Cliente, modoTeste bool) (e Estado) {
	e.ModoTeste = modoTeste
	ctx, cancela := context.WithTimeout(ctx, 10*time.Second)
	defer cancela()

	// A chave é conferida em paralelo: ela pode levar segundos (rede), o LCU não.
	pronto := make(chan riot.Estado, 1)
	go func() { pronto <- chave.Estado(ctx) }()
	defer func() { e.ChaveRiot = <-pronto }()

	inv, err := cli.InvocadorAtual(ctx)
	if err != nil {
		e.LCU.Erro = err.Error()
		if !errors.Is(err, lcu.ErrClienteFechado) {
			e.LCU.Erro = "o cliente do LoL está aberto mas não respondeu: " + err.Error()
		}
		return e
	}
	e.LCU.Conectado = true
	e.Invocador = &inv
	if fase, err := cli.FaseGameflow(ctx); err == nil {
		e.Fase = fase
	}
	if cfg, err := cli.ConfiguracaoReplays(ctx); err == nil {
		e.VersaoJogo, e.Patch, e.ReplaysHabilitados = cfg.VersaoJogo, lcu.PatchDe(cfg.VersaoJogo), cfg.Habilitado
	}
	if reg, err := cli.Regiao(ctx); err == nil {
		e.Regiao = reg.Regiao
	}
	if p, err := cli.PastaReplays(ctx); err == nil {
		e.PastaReplays = p
	}
	return e
}

// Registra pendura GET /api/estado.
func Registra(mux *http.ServeMux, cli lcu.Cliente, chave *riot.Cliente, modoTeste bool) {
	mux.HandleFunc("GET /api/estado", func(w http.ResponseWriter, r *http.Request) {
		web.WriteJSON(w, http.StatusOK, Monta(r.Context(), cli, chave, modoTeste))
	})
}
