package estudo

import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"loltracking/server/analise"
	"loltracking/server/partida"
	"loltracking/server/web"
)

// Registra pendura as rotas do estudo (docs/arquitetura/contrato-http.md).
func Registra(mux *http.ServeMux, d analise.Deps) {
	c := &controller{d: d}
	mux.HandleFunc("GET /api/estudos", c.lista)
	mux.HandleFunc("POST /api/estudos", c.cria)
	mux.HandleFunc("GET /api/estudos/{id}", c.le)
	mux.HandleFunc("PATCH /api/estudos/{id}", c.edita)
	mux.HandleFunc("DELETE /api/estudos/{id}", c.apaga)
	mux.HandleFunc("GET /api/estudos/{id}/sugestoes", c.sugestoes)
	mux.HandleFunc("POST /api/estudos/{id}/partidas/{game_id}", c.adiciona)
	mux.HandleFunc("PUT /api/estudos/{id}/partidas/{game_id}", c.anota)
	mux.HandleFunc("DELETE /api/estudos/{id}/partidas/{game_id}", c.tira)
	mux.HandleFunc("GET /api/estudos/{id}/comparacao", c.compara)
	mux.HandleFunc("GET /api/estudos/{id}/indicadores/{nome}", c.indicador)
}

type controller struct{ d analise.Deps }

func (c *controller) erro(w http.ResponseWriter, err error, acao string) {
	var ep *ErroPedido
	var ec *ErroConflito
	switch {
	case errors.As(err, &ep):
		web.WriteErr(w, http.StatusBadRequest, ep.Msg)
	case errors.As(err, &ec):
		web.WriteErr(w, http.StatusConflict, ec.Msg)
	case errors.Is(err, ErrNaoEncontrado):
		web.WriteErr(w, http.StatusNotFound, "estudo não encontrado")
	case errors.Is(err, ErrPartidaFora):
		web.WriteErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, partida.ErrNaoEncontrada):
		web.WriteErr(w, http.StatusNotFound, "partida não encontrada")
	default:
		log.Printf("estudo: %s: %v", acao, err)
		web.WriteErr(w, http.StatusInternalServerError, "não foi possível "+acao)
	}
}

func idDe(r *http.Request, nome string) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue(nome), 10, 64)
	if err != nil || id <= 0 {
		return 0, &ErroPedido{nome + " inválido"}
	}
	return id, nil
}

func (c *controller) lista(w http.ResponseWriter, r *http.Request) {
	l, err := Lista(c.d.DB)
	if err != nil {
		c.erro(w, err, "listar os estudos")
		return
	}
	web.WriteLista(w, "estudos", l, nil)
}

func (c *controller) cria(w http.ResponseWriter, r *http.Request) {
	var n Novo
	if err := web.LeJSON(w, r, &n); err != nil {
		web.WriteErr(w, http.StatusBadRequest, err.Error())
		return
	}
	e, err := Cria(c.d.DB, n)
	if err != nil {
		c.erro(w, err, "criar o estudo")
		return
	}
	web.WriteJSON(w, http.StatusCreated, map[string]any{"estudo": e})
}

func (c *controller) le(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r, "id")
	var e Estudo
	if err == nil {
		e, err = Le(c.d.DB, id)
	}
	if err != nil {
		c.erro(w, err, "ler o estudo")
		return
	}
	ps, err := Partidas(c.d.DB, e)
	if err != nil {
		c.erro(w, err, "ler o estudo")
		return
	}
	web.WriteJSON(w, http.StatusOK, map[string]any{"estudo": e, "partidas": ps})
}

func (c *controller) edita(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r, "id")
	if err != nil {
		c.erro(w, err, "")
		return
	}
	var n Novo
	if err := web.LeJSON(w, r, &n); err != nil {
		web.WriteErr(w, http.StatusBadRequest, err.Error())
		return
	}
	e, err := Edita(c.d.DB, id, n)
	if err != nil {
		c.erro(w, err, "editar o estudo")
		return
	}
	web.WriteJSON(w, http.StatusOK, map[string]any{"estudo": e})
}

func (c *controller) apaga(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r, "id")
	if err == nil {
		err = Apaga(c.d.DB, id)
	}
	if err != nil {
		c.erro(w, err, "apagar o estudo")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (c *controller) sugestoes(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r, "id")
	var l []Sugestao
	if err == nil {
		l, err = Sugestoes(c.d.DB, id)
	}
	if err != nil {
		c.erro(w, err, "listar as sugestões")
		return
	}
	web.WriteLista(w, "partidas", l, nil)
}

func (c *controller) ids(w http.ResponseWriter, r *http.Request) (int64, int64, bool) {
	id, err := idDe(r, "id")
	var g int64
	if err == nil {
		g, err = idDe(r, "game_id")
	}
	if err != nil {
		c.erro(w, err, "")
		return 0, 0, false
	}
	return id, g, true
}

func (c *controller) adiciona(w http.ResponseWriter, r *http.Request) {
	id, g, ok := c.ids(w, r)
	if !ok {
		return
	}
	var corpo struct {
		Anotacao string `json:"anotacao"`
	}
	if err := web.LeJSON(w, r, &corpo); err != nil {
		web.WriteErr(w, http.StatusBadRequest, err.Error())
		return
	}
	p, err := AdicionaPartida(r.Context(), c.d, id, g, corpo.Anotacao)
	if err != nil {
		c.erro(w, err, "adicionar a partida")
		return
	}
	web.WriteJSON(w, http.StatusCreated, map[string]any{"partida": p})
}

func (c *controller) anota(w http.ResponseWriter, r *http.Request) {
	id, g, ok := c.ids(w, r)
	if !ok {
		return
	}
	var corpo struct {
		Anotacao *string `json:"anotacao"`
	}
	if err := web.LeJSON(w, r, &corpo); err != nil {
		web.WriteErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if corpo.Anotacao == nil {
		web.WriteErr(w, http.StatusBadRequest, "informe a anotacao (texto; vazio apaga)")
		return
	}
	p, err := Anota(c.d.DB, id, g, *corpo.Anotacao)
	if err != nil {
		c.erro(w, err, "guardar a anotação")
		return
	}
	web.WriteJSON(w, http.StatusOK, map[string]any{"partida": p})
}

func (c *controller) tira(w http.ResponseWriter, r *http.Request) {
	id, g, ok := c.ids(w, r)
	if !ok {
		return
	}
	if err := TiraPartida(c.d.DB, id, g); err != nil {
		c.erro(w, err, "tirar a partida")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (c *controller) compara(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r, "id")
	var cmp Comparacao
	if err == nil {
		cmp, err = Compara(c.d.DB, id)
	}
	if err != nil {
		c.erro(w, err, "comparar")
		return
	}
	web.WriteJSON(w, http.StatusOK, cmp)
}

func (c *controller) indicador(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r, "id")
	if err != nil {
		c.erro(w, err, "")
		return
	}
	def, l, err := DoIndicador(c.d.DB, id, r.PathValue("nome"))
	if err != nil {
		c.erro(w, err, "ler o indicador")
		return
	}
	web.WriteJSON(w, http.StatusOK, map[string]any{"indicador": def, "partidas": l})
}
