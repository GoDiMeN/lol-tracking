package contexto

import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"loltracking/server/partida"
	"loltracking/server/web"
)

// Registra as rotas do contexto dos jogadores (contrato-http.md, "Análise").
func Registra(mux *http.ServeMux, s *Servico) {
	mux.HandleFunc("GET /api/partidas/{id}/contexto-jogadores", s.estado)
	mux.HandleFunc("POST /api/partidas/{id}/contexto-jogadores", s.busca)
}

func execucaoJSON(e *web.Execucao) any {
	if e == nil {
		return nil
	}
	return map[string]string{"id": e.ID()}
}

func gameDaRota(w http.ResponseWriter, r *http.Request) (int64, bool) {
	g, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || g <= 0 {
		web.WriteErr(w, http.StatusBadRequest, "id de partida inválido")
		return 0, false
	}
	return g, true
}

func responde(w http.ResponseWriter, err error, acao string) {
	var p *ErrPedido
	switch {
	case errors.As(err, &p):
		if p.Execucao != nil {
			web.WriteJSON(w, p.Status, map[string]any{"erro": p.Texto, "execucao": execucaoJSON(p.Execucao)})
			return
		}
		web.WriteErr(w, p.Status, p.Texto)
	case errors.Is(err, partida.ErrNaoEncontrada):
		web.WriteErr(w, http.StatusNotFound, "partida não encontrada")
	default:
		log.Printf("contexto: %s: %v", acao, err)
		web.WriteErr(w, http.StatusInternalServerError, "não foi possível "+acao)
	}
}

// GET /api/partidas/{id}/contexto-jogadores
func (s *Servico) estado(w http.ResponseWriter, r *http.Request) {
	g, ok := gameDaRota(w, r)
	if !ok {
		return
	}
	e, err := s.LeEstado(r.Context(), g)
	if err != nil {
		responde(w, err, "ler o contexto dos jogadores")
		return
	}
	web.WriteJSON(w, http.StatusOK, map[string]any{"game_id": e.GameID, "jogadores": e.Jogadores, "faltam": e.Faltam, "fontes": e.Fontes,
		"contextos": e.Contextos, "execucao": execucaoJSON(e.Execucao), "rodando": e.Execucao != nil})
}

// POST /api/partidas/{id}/contexto-jogadores {"fonte"?}
func (s *Servico) busca(w http.ResponseWriter, r *http.Request) {
	g, ok := gameDaRota(w, r)
	if !ok {
		return
	}
	var corpo struct {
		Fonte string `json:"fonte"`
	}
	if err := web.LeJSON(w, r, &corpo); err != nil {
		web.WriteErr(w, http.StatusBadRequest, err.Error())
		return
	}
	e, fonte, err := s.Inicia(r.Context(), g, corpo.Fonte)
	if err != nil {
		responde(w, err, "buscar o contexto dos jogadores")
		return
	}
	web.WriteJSON(w, http.StatusAccepted, map[string]any{"execucao": execucaoJSON(e), "fonte": fonte})
}
