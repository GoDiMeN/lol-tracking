package captura

import (
	"errors"
	"log"
	"net/http"

	"loltracking/server/aprendizado"
	"loltracking/server/lcu"
	"loltracking/server/rastreio"
	"loltracking/server/web"
)

// Registra pendura as rotas da captura (docs/arquitetura/contrato-http.md, "Captura").
func Registra(mux *http.ServeMux, s *Servico) {
	c := &controller{s: s}
	mux.HandleFunc("GET /api/captura", c.le)
	mux.HandleFunc("POST /api/captura/pausar", c.pausa)
	mux.HandleFunc("POST /api/captura/retomar", c.retoma)
	mux.HandleFunc("POST /api/captura/alternar", c.alterna)
}

type controller struct{ s *Servico }

func (c *controller) erro(w http.ResponseWriter, err error, acao string) {
	var e409 *Erro409
	var est *rastreio.ErroEstado
	var ocu *rastreio.ErroOcupado
	var inv *rastreio.ErroInvalido
	var conf *aprendizado.ErroConflito
	switch {
	case errors.As(err, &e409):
		web.WriteErr(w, http.StatusConflict, e409.Msg)
	case errors.As(err, &est):
		web.WriteErr(w, http.StatusConflict, est.Msg)
	case errors.As(err, &ocu):
		web.WriteErr(w, http.StatusConflict, ocu.Error())
	case errors.As(err, &conf):
		web.WriteErr(w, http.StatusConflict, conf.Msg)
	case errors.As(err, &inv):
		web.WriteErr(w, http.StatusBadRequest, inv.Msg)
	default:
		if status, msg, ok := lcu.StatusHTTP(err); ok {
			web.WriteErr(w, status, msg)
			return
		}
		log.Printf("captura: %s: %v", acao, err)
		web.WriteErr(w, http.StatusInternalServerError, "não foi possível "+acao)
	}
}

// GET /api/captura
func (c *controller) le(w http.ResponseWriter, r *http.Request) {
	web.WriteJSON(w, http.StatusOK, c.s.Le())
}

// POST /api/captura/pausar
func (c *controller) pausa(w http.ResponseWriter, r *http.Request) {
	cp, err := c.s.Pausa()
	if err != nil {
		c.erro(w, err, "pausar a captura")
		return
	}
	web.WriteJSON(w, http.StatusOK, map[string]any{"pausada": cp})
}

// POST /api/captura/retomar
func (c *controller) retoma(w http.ResponseWriter, r *http.Request) {
	cp, ex, err := c.s.Retoma(r.Context())
	if err != nil {
		c.erro(w, err, "retomar a captura")
		return
	}
	web.WriteJSON(w, http.StatusAccepted, map[string]any{"retomada": cp, "execucao": map[string]string{"id": ex}})
}

// POST /api/captura/alternar
func (c *controller) alterna(w http.ResponseWriter, r *http.Request) {
	acao, cp, ex, err := c.s.Alterna(r.Context())
	if err != nil {
		c.erro(w, err, "alternar a captura")
		return
	}
	corpo := map[string]any{"acao": acao, "captura": cp, "execucao": nil}
	if ex != "" {
		corpo["execucao"] = map[string]string{"id": ex}
	}
	web.WriteJSON(w, http.StatusOK, corpo)
}
