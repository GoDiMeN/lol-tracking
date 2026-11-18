package aprendizado

import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"loltracking/server/estudo"
	"loltracking/server/lcu"
	"loltracking/server/riot"
	"loltracking/server/web"
)

// Registra pendura as rotas do aprendizado (docs/arquitetura/contrato-http.md).
func Registra(mux *http.ServeMux, k *Coletor) {
	c := &controller{k: k}
	mux.HandleFunc("GET /api/aprendizado/regioes", c.regioes)
	mux.HandleFunc("GET /api/aprendizado/coletas", c.lista)
	mux.HandleFunc("POST /api/aprendizado/coletas", c.cria)
	mux.HandleFunc("GET /api/aprendizado/coletas/{id}", c.le)
	mux.HandleFunc("POST /api/aprendizado/coletas/{id}/pausar", c.pausa)
	mux.HandleFunc("POST /api/aprendizado/coletas/{id}/retomar", c.retoma)
	mux.HandleFunc("POST /api/aprendizado/coletas/{id}/cancelar", c.cancela)
	mux.HandleFunc("GET /api/aprendizado/coletas/{id}/eventos", c.eventos)
	mux.HandleFunc("GET /api/aprendizado/coletas/{id}/espaco", c.espaco)
	mux.HandleFunc("POST /api/aprendizado/jogadores/{id}/estudo", c.criaEstudo)
	// O rastreio das partidas de um estudo é uma coleta sem descoberta: a rota
	// fica com o estudo, o motor é o desta coleta (pausar/cancelar pelas rotas
	// da coleta).
	mux.HandleFunc("GET /api/estudos/{id}/rastreio", c.rastreioEstudo)
	mux.HandleFunc("POST /api/estudos/{id}/rastreio", c.rastreiaEstudo)
}

type controller struct{ k *Coletor }

func (c *controller) erro(w http.ResponseWriter, err error, acao string) {
	var ep *ErroPedido
	var ec *ErroConflito
	var epe *estudo.ErroPedido
	switch {
	case errors.As(err, &ep):
		web.WriteErr(w, http.StatusBadRequest, ep.Msg)
	case errors.As(err, &ec):
		web.WriteErr(w, http.StatusConflict, ec.Msg)
	case errors.Is(err, ErrNaoEncontrada):
		web.WriteErr(w, http.StatusNotFound, "coleta não encontrada")
	case errors.Is(err, ErrJogadorNaoEncontrado):
		web.WriteErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, estudo.ErrNaoEncontrado):
		web.WriteErr(w, http.StatusNotFound, "estudo não encontrado")
	case errors.As(err, &epe):
		web.WriteErr(w, http.StatusBadRequest, epe.Msg)
	default:
		if status, msg, ok := lcu.StatusHTTP(err); ok {
			web.WriteErr(w, status, msg)
			return
		}
		log.Printf("aprendizado: %s: %v", acao, err)
		web.WriteErr(w, http.StatusInternalServerError, "não foi possível "+acao)
	}
}

func idDe(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, &ErroPedido{"id inválido"}
	}
	return id, nil
}

func (c *controller) regioes(w http.ResponseWriter, r *http.Request) {
	web.WriteLista(w, "regioes", riot.Regioes(), nil)
}

func (c *controller) lista(w http.ResponseWriter, r *http.Request) {
	l, err := c.k.Lista()
	if err != nil {
		c.erro(w, err, "listar as coletas")
		return
	}
	web.WriteLista(w, "coletas", l, nil)
}

// POST /api/aprendizado/coletas {"regiao"?, "elos"?, "partidas_por_jogador"?, "quem"?, "velocidade"?}
func (c *controller) cria(w http.ResponseWriter, r *http.Request) {
	var p Pedido
	if err := web.LeJSON(w, r, &p); err != nil {
		web.WriteErr(w, http.StatusBadRequest, err.Error())
		return
	}
	col, exID, err := c.k.Cria(r.Context(), p)
	if err != nil {
		c.erro(w, err, "começar a coleta")
		return
	}
	web.WriteJSON(w, http.StatusAccepted, map[string]any{"coleta": col, "execucao": map[string]string{"id": exID}})
}

func (c *controller) le(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	var d Detalhe
	if err == nil {
		d, err = c.k.Detalhe(id)
	}
	if err != nil {
		c.erro(w, err, "ler a coleta")
		return
	}
	web.WriteJSON(w, http.StatusOK, d)
}

func (c *controller) pausa(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	if err == nil {
		err = c.k.Pausa(id)
	}
	if err != nil {
		c.erro(w, err, "pausar a coleta")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (c *controller) retoma(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	var exID string
	if err == nil {
		exID, err = c.k.Retoma(r.Context(), id)
	}
	if err != nil {
		c.erro(w, err, "retomar a coleta")
		return
	}
	web.WriteJSON(w, http.StatusAccepted, map[string]any{"execucao": map[string]string{"id": exID}})
}

func (c *controller) cancela(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	if err == nil {
		err = c.k.Cancela(id)
	}
	if err != nil {
		c.erro(w, err, "cancelar a coleta")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (c *controller) eventos(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	if err != nil {
		c.erro(w, err, "")
		return
	}
	exID, ok := c.k.ExecucaoDe(id)
	if !ok {
		web.WriteErr(w, http.StatusNotFound, "esta coleta não tem execução recente")
		return
	}
	c.k.d.Hub.Servir(w, r, exID)
}

// POST /api/aprendizado/jogadores/{id}/estudo {"por_lado"?: 3, "nome"?}
func (c *controller) criaEstudo(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	if err != nil {
		c.erro(w, err, "")
		return
	}
	var p PedidoEstudo
	if err := web.LeJSON(w, r, &p); err != nil {
		web.WriteErr(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := c.k.CriaEstudo(r.Context(), id, p)
	if err != nil {
		c.erro(w, err, "criar o estudo")
		return
	}
	web.WriteJSON(w, http.StatusCreated, res)
}

func (c *controller) rastreioEstudo(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	var res RastreioDoEstudo
	if err == nil {
		res, err = c.k.LeRastreioDoEstudo(id)
	}
	if err != nil {
		c.erro(w, err, "ler o rastreio do estudo")
		return
	}
	web.WriteJSON(w, http.StatusOK, res)
}

// POST /api/estudos/{id}/rastreio {"velocidade"?: 4}
func (c *controller) rastreiaEstudo(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	if err != nil {
		c.erro(w, err, "")
		return
	}
	var p struct {
		Velocidade float64 `json:"velocidade"`
	}
	if err := web.LeJSON(w, r, &p); err != nil {
		web.WriteErr(w, http.StatusBadRequest, err.Error())
		return
	}
	res, exID, err := c.k.RastreiaEstudo(r.Context(), id, p.Velocidade)
	if err != nil {
		c.erro(w, err, "rastrear as partidas do estudo")
		return
	}
	web.WriteJSON(w, http.StatusAccepted, map[string]any{"rastreio": res, "execucao": map[string]string{"id": exID}})
}

func (c *controller) espaco(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	var e Espaco
	if err == nil {
		e, err = c.k.EspacoDe(id)
	}
	if err != nil {
		c.erro(w, err, "medir o espaço da coleta")
		return
	}
	web.WriteJSON(w, http.StatusOK, e)
}
