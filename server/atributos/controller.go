package atributos

import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"loltracking/server/partida"
	"loltracking/server/web"
)

// Registra pendura as rotas (contrato-http.md, "Atributos e build").
func Registra(mux *http.ServeMux, d Deps) {
	c := &controller{d: d}
	mux.HandleFunc("GET /api/partidas/{id}/bonus-objetivos", c.objetivos)
	mux.HandleFunc("GET /api/partidas/{id}/estaticos", c.estaticos)
	mux.HandleFunc("GET /api/partidas/{id}/build", c.build)
}

type controller struct{ d Deps }

type erroPedido struct{ msg string }

func (e *erroPedido) Error() string { return e.msg }

func (c *controller) erro(w http.ResponseWriter, err error, acao string) {
	var ep *erroPedido
	switch {
	case errors.As(err, &ep):
		web.WriteErr(w, http.StatusBadRequest, ep.msg)
	case errors.Is(err, partida.ErrNaoEncontrada):
		web.WriteErr(w, http.StatusNotFound, "partida não encontrada")
	case errors.Is(err, ErrSemLinhaDoTempo):
		web.WriteErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrSemEstaticos):
		web.WriteErr(w, http.StatusServiceUnavailable, err.Error())
	default:
		log.Printf("atributos: %s: %v", acao, err)
		web.WriteErr(w, http.StatusInternalServerError, "não foi possível "+acao)
	}
}

func idPartida(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, &erroPedido{"id inválido"}
	}
	return id, nil
}

// GET /api/partidas/{id}/bonus-objetivos
func (c *controller) objetivos(w http.ResponseWriter, r *http.Request) {
	id, err := idPartida(r)
	if err != nil {
		c.erro(w, err, "")
		return
	}
	res, err := MedeObjetivos(c.d.DB, id)
	if err != nil {
		c.erro(w, err, "medir o bônus dos objetivos")
		return
	}
	web.WriteJSON(w, http.StatusOK, res)
}

// GET /api/partidas/{id}/estaticos
func (c *controller) estaticos(w http.ResponseWriter, r *http.Request) {
	id, err := idPartida(r)
	if err != nil {
		c.erro(w, err, "")
		return
	}
	res, err := EstaticosDaPartida(r.Context(), c.d, id)
	if err != nil {
		c.erro(w, err, "ler os dados estáticos da versão da partida")
		return
	}
	web.WriteJSON(w, http.StatusOK, res)
}

// GET /api/partidas/{id}/build?participante=
func (c *controller) build(w http.ResponseWriter, r *http.Request) {
	id, err := idPartida(r)
	if err != nil {
		c.erro(w, err, "")
		return
	}
	pid := 0
	if s := r.URL.Query().Get("participante"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > 10 {
			c.erro(w, &erroPedido{"participante precisa ser de 1 a 10"}, "")
			return
		}
		pid = n
	}
	res, err := Build(r.Context(), c.d, id, pid)
	if err != nil {
		c.erro(w, err, "montar a build")
		return
	}
	web.WriteJSON(w, http.StatusOK, res)
}
