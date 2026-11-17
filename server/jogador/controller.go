package jogador

import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"loltracking/server/banco"
	"loltracking/server/lcu"
	"loltracking/server/web"
)

// Registra pendura as rotas de jogador (docs/arquitetura/contrato-http.md).
func Registra(mux *http.ServeMux, db *banco.DB, cli lcu.Cliente) {
	c := &controller{db: db, cli: cli}
	mux.HandleFunc("POST /api/jogadores/buscar", c.busca)
	mux.HandleFunc("GET /api/jogadores", c.recentes)
	mux.HandleFunc("GET /api/jogadores/{puuid}", c.le)
}

type controller struct {
	db  *banco.DB
	cli lcu.Cliente
}

// erro traduz o erro do model em status.
func erro(w http.ResponseWriter, err error, acao string) {
	var inv *ErroInvalido
	if errors.As(err, &inv) {
		web.WriteErr(w, http.StatusBadRequest, inv.Msg)
		return
	}
	if errors.Is(err, ErrNaoEncontrado) {
		web.WriteErr(w, http.StatusNotFound, err.Error())
		return
	}
	if status, msg, ok := lcu.StatusHTTP(err); ok {
		if status >= 500 && status != 503 {
			log.Printf("jogador: %s: %v", acao, err)
		}
		web.WriteErr(w, status, msg)
		return
	}
	log.Printf("jogador: %s: %v", acao, err)
	web.WriteErr(w, http.StatusInternalServerError, "não foi possível "+acao)
}

// POST /api/jogadores/buscar {"texto": "Nome#TAG"}
func (c *controller) busca(w http.ResponseWriter, r *http.Request) {
	var corpo struct {
		Texto string `json:"texto"`
	}
	if err := web.LeJSON(w, r, &corpo); err != nil {
		web.WriteErr(w, http.StatusBadRequest, err.Error())
		return
	}
	j, err := Busca(r.Context(), c.db, c.cli, corpo.Texto)
	if err != nil {
		erro(w, err, "buscar o jogador")
		return
	}
	web.WriteJSON(w, http.StatusOK, map[string]any{"jogador": j})
}

// GET /api/jogadores?limite=20
func (c *controller) recentes(w http.ResponseWriter, r *http.Request) {
	limite := 20
	if s := r.URL.Query().Get("limite"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > 200 {
			web.WriteErr(w, http.StatusBadRequest, "limite precisa ser um número de 1 a 200")
			return
		}
		limite = n
	}
	lista, err := Recentes(c.db, limite)
	if err != nil {
		erro(w, err, "listar os jogadores")
		return
	}
	web.WriteLista(w, "jogadores", lista, nil)
}

// GET /api/jogadores/{puuid}
func (c *controller) le(w http.ResponseWriter, r *http.Request) {
	j, err := Le(c.db, r.PathValue("puuid"))
	if err != nil {
		erro(w, err, "ler o jogador")
		return
	}
	web.WriteJSON(w, http.StatusOK, map[string]any{"jogador": j})
}
