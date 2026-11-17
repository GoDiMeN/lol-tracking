package partida

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"sync"

	"loltracking/server/lcu"
	"loltracking/server/web"
)

// Registra pendura as rotas de partida (docs/arquitetura/contrato-http.md).
func Registra(mux *http.ServeMux, d Deps, hub *web.Hub) {
	c := &controller{d: d, hub: hub, base: context.Background(), baixando: map[int64]string{}}
	mux.HandleFunc("GET /api/jogadores/{puuid}/partidas", c.lista)
	mux.HandleFunc("GET /api/partidas/{id}", c.le)
	mux.HandleFunc("GET /api/partidas/{id}/replay", c.replay)
	mux.HandleFunc("POST /api/partidas/{id}/replay/baixar", c.baixa)
	mux.HandleFunc("GET /api/partidas/{id}/linha-do-tempo", c.linhaDoTempo)
}

type controller struct {
	d    Deps
	hub  *web.Hub
	base context.Context

	mu sync.Mutex
	// baixando: game_id → id da execução de download em andamento.
	baixando map[int64]string
}

func erro(w http.ResponseWriter, err error, acao string) {
	var inv *ErroInvalido
	switch {
	case errors.As(err, &inv):
		web.WriteErr(w, http.StatusBadRequest, inv.Msg)
		return
	case errors.Is(err, ErrNaoEncontrada):
		web.WriteErr(w, http.StatusNotFound, "partida não encontrada")
		return
	}
	if status, msg, ok := lcu.StatusHTTP(err); ok {
		if status != http.StatusServiceUnavailable {
			log.Printf("partida: %s: %v", acao, err)
		}
		web.WriteErr(w, status, msg)
		return
	}
	log.Printf("partida: %s: %v", acao, err)
	web.WriteErr(w, http.StatusInternalServerError, "não foi possível "+acao)
}

func idDaRota(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		web.WriteErr(w, http.StatusBadRequest, "id de partida inválido")
		return 0, false
	}
	return id, true
}

func inteiro(r *http.Request, nome string, padrao int) (int, error) {
	s := r.URL.Query().Get(nome)
	if s == "" {
		return padrao, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, &ErroInvalido{nome + " precisa ser um número"}
	}
	return n, nil
}

// GET /api/jogadores/{puuid}/partidas?inicio=0&quantidade=20
func (c *controller) lista(w http.ResponseWriter, r *http.Request) {
	inicio, err := inteiro(r, "inicio", 0)
	if err != nil {
		erro(w, err, "listar as partidas")
		return
	}
	quantidade, err := inteiro(r, "quantidade", 20)
	if err != nil {
		erro(w, err, "listar as partidas")
		return
	}
	pag, err := ListaDoJogador(r.Context(), c.d, r.PathValue("puuid"), inicio, quantidade)
	if err != nil {
		erro(w, err, "listar as partidas")
		return
	}
	web.WriteJSON(w, http.StatusOK, pag)
}

// GET /api/partidas/{id}
func (c *controller) le(w http.ResponseWriter, r *http.Request) {
	id, ok := idDaRota(w, r)
	if !ok {
		return
	}
	p, err := LePartida(r.Context(), c.d, id)
	if err != nil {
		erro(w, err, "ler a partida")
		return
	}
	web.WriteJSON(w, http.StatusOK, map[string]any{"partida": p})
}

// GET /api/partidas/{id}/replay — o estado vivo, consultado no cliente agora.
func (c *controller) replay(w http.ResponseWriter, r *http.Request) {
	id, ok := idDaRota(w, r)
	if !ok {
		return
	}
	if err := garantePartida(r.Context(), c.d, id); err != nil {
		erro(w, err, "consultar o replay")
		return
	}
	cfg, err := c.d.LCU.ConfiguracaoReplays(r.Context())
	if err != nil {
		erro(w, err, "consultar o replay")
		return
	}
	rep, err := ConsultaReplay(r.Context(), c.d, id, cfg.VersaoJogo)
	if err != nil {
		erro(w, err, "consultar o replay")
		return
	}
	GravaReplay(c.d.DB, id, rep)
	resp := map[string]any{"replay": rep}
	c.mu.Lock()
	if ex, ok := c.baixando[id]; ok {
		resp["execucao"] = map[string]string{"id": ex}
	}
	c.mu.Unlock()
	web.WriteJSON(w, http.StatusOK, resp)
}

// POST /api/partidas/{id}/replay/baixar → 202 {"execucao": {"id"}}
// Um download por partida: um segundo pedido com o primeiro rodando é 409 e
// devolve o id da execução que já existe, para a tela se ligar a ela.
func (c *controller) baixa(w http.ResponseWriter, r *http.Request) {
	id, ok := idDaRota(w, r)
	if !ok {
		return
	}
	c.mu.Lock()
	if exID, ok := c.baixando[id]; ok {
		c.mu.Unlock()
		web.WriteJSON(w, http.StatusConflict, map[string]any{
			"erro": "o replay desta partida já está sendo baixado", "execucao": map[string]string{"id": exID}})
		return
	}
	ex := c.hub.Nova()
	c.baixando[id] = ex.ID()
	c.mu.Unlock()

	ctx, cancela := context.WithCancel(c.base)
	ex.AoCancelar(cancela)
	go func() {
		defer func() {
			c.mu.Lock()
			delete(c.baixando, id)
			c.mu.Unlock()
		}()
		defer cancela()
		Baixa(ctx, c.d, ex, id)
	}()
	web.WriteJSON(w, http.StatusAccepted, map[string]any{"execucao": map[string]string{"id": ex.ID()}})
}

// GET /api/partidas/{id}/linha-do-tempo — o JSON cru do LCU, guardado no download.
func (c *controller) linhaDoTempo(w http.ResponseWriter, r *http.Request) {
	id, ok := idDaRota(w, r)
	if !ok {
		return
	}
	b, err := LeLinhaDoTempo(c.d.DB, id)
	if errors.Is(err, ErrNaoEncontrada) {
		web.WriteErr(w, http.StatusNotFound, "a linha do tempo desta partida ainda não foi guardada (ela vem junto com o download do replay)")
		return
	}
	if err != nil {
		erro(w, err, "ler a linha do tempo")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Write(b)
}
