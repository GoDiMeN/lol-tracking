package analise

import (
	"errors"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"

	"loltracking/server/partida"
	"loltracking/server/web"
)

// Registra pendura as rotas da análise (docs/arquitetura/contrato-http.md).
func Registra(mux *http.ServeMux, d Deps) {
	c := &controller{d: d}
	mux.HandleFunc("GET /api/mapa/zonas", c.zonas)
	mux.HandleFunc("GET /api/indicadores", c.vocabulario)
	mux.HandleFunc("GET /api/partidas/{id}/analise", c.leAnalise)
	mux.HandleFunc("POST /api/partidas/{id}/analise", c.recalcula)
	mux.HandleFunc("GET /api/partidas/{id}/qualidade", c.qualidade)
	mux.HandleFunc("GET /api/partidas/{id}/abates", c.abates)
	mux.HandleFunc("GET /api/partidas/{id}/abates/{n}", c.abate)
	mux.HandleFunc("GET /api/partidas/{id}/indicadores", c.indicadores)
	mux.HandleFunc("GET /api/partidas/{id}/saidas-de-rota", c.saidas)
	mux.HandleFunc("GET /api/partidas/{id}/atividades", c.atividades)
	mux.HandleFunc("GET /api/partidas/{id}/cs", c.cs)
	mux.HandleFunc("GET /api/partidas/{id}/fases", c.fases)
	mux.HandleFunc("GET /api/partidas/{id}/ondas", c.ondas)
	mux.HandleFunc("GET /api/partidas/{id}/sentinelas", c.sentinelas)
}

// GET /api/partidas/{id}/fases
func (c *controller) fases(w http.ResponseWriter, r *http.Request) {
	id, err := idPartida(r)
	if err != nil {
		c.erro(w, err, "")
		return
	}
	l, dur, err := LeFases(c.d.DB, id)
	if err != nil {
		c.erro(w, err, "ler as fases da partida")
		return
	}
	web.WriteLista(w, "fases", l, map[string]any{"duracao_ms": dur})
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
	case errors.Is(err, ErrNaoAnalisada):
		web.WriteErr(w, http.StatusNotFound, "abate não encontrado (a partida foi analisada?)")
	default:
		log.Printf("análise: %s: %v", acao, err)
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

// participante lê ?participante= (0 = todos).
func participante(r *http.Request) (int, error) {
	s := r.URL.Query().Get("participante")
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 10 {
		return 0, &erroPedido{"participante precisa ser de 1 a 10"}
	}
	return n, nil
}

func segundos(r *http.Request, nome string, padrao int64) (int64, error) {
	s := r.URL.Query().Get(nome)
	if s == "" {
		return padrao, nil
	}
	v, err := strconv.ParseFloat(strings.Replace(s, ",", ".", 1), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return 0, &erroPedido{nome + " precisa ser um número de segundos ≥ 0"}
	}
	return int64(math.Round(v * 1000)), nil
}

// GET /api/mapa/zonas?celula=100
func (c *controller) zonas(w http.ResponseWriter, r *http.Request) {
	cel := 100.0
	if s := r.URL.Query().Get("celula"); s != "" {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil || v < 25 || v > 1000 {
			web.WriteErr(w, http.StatusBadRequest, "celula precisa ser de 25 a 1000 unidades")
			return
		}
		cel = v
	}
	web.WriteJSON(w, http.StatusOK, map[string]any{
		"zonas": Zonas, "subzonas": NomesSubzona, "grupos": GruposRelativos,
		"limites": map[string]any{"min": map[string]float64{"x": MapaMin.X, "y": MapaMin.Y}, "max": map[string]float64{"x": MapaMax.X, "y": MapaMax.Y}},
		"grade":   GeraGrade(cel),
		"imagem":  "map11.png do Data Dragon: x da esquerda para a direita, y de baixo para cima (inverter na tela)",
	})
}

// GET /api/indicadores
func (c *controller) vocabulario(w http.ResponseWriter, r *http.Request) {
	web.WriteLista(w, "indicadores", Vocabulario, map[string]any{"atividades": Atividades, "fases": Fases,
		"parametros": Param})
}

// GET /api/partidas/{id}/qualidade: o que a checagem do dado achou em cada passe.
func (c *controller) qualidade(w http.ResponseWriter, r *http.Request) {
	id, err := idPartida(r)
	if err != nil {
		c.erro(w, err, "")
		return
	}
	q, err := LeQualidade(c.d.DB, id)
	if err != nil {
		c.erro(w, err, "ler a checagem de qualidade")
		return
	}
	web.WriteJSON(w, http.StatusOK, q)
}

func (c *controller) leAnalise(w http.ResponseWriter, r *http.Request) {
	id, err := idPartida(r)
	if err != nil {
		c.erro(w, err, "")
		return
	}
	res, ok, err := LeResumo(c.d.DB, id)
	if err != nil {
		c.erro(w, err, "ler a análise")
		return
	}
	if !ok {
		web.WriteJSON(w, http.StatusOK, map[string]any{"analise": nil})
		return
	}
	web.WriteJSON(w, http.StatusOK, map[string]any{"analise": res})
}

// POST /api/partidas/{id}/analise — recalcula agora (síncrono: ~1 s).
func (c *controller) recalcula(w http.ResponseWriter, r *http.Request) {
	id, err := idPartida(r)
	if err != nil {
		c.erro(w, err, "")
		return
	}
	res, err := Recalcula(r.Context(), c.d, id)
	if err != nil {
		c.erro(w, err, "analisar a partida")
		return
	}
	web.WriteJSON(w, http.StatusOK, map[string]any{"analise": res})
}

func (c *controller) abates(w http.ResponseWriter, r *http.Request) {
	id, err := idPartida(r)
	if err != nil {
		c.erro(w, err, "")
		return
	}
	l, err := ListaRetratos(c.d.DB, id)
	if err != nil {
		c.erro(w, err, "ler os abates")
		return
	}
	web.WriteLista(w, "abates", l, nil)
}

func (c *controller) abate(w http.ResponseWriter, r *http.Request) {
	id, err := idPartida(r)
	if err != nil {
		c.erro(w, err, "")
		return
	}
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 1 {
		web.WriteErr(w, http.StatusBadRequest, "n precisa ser o número do abate (1, 2, ...)")
		return
	}
	rt, err := LeRetrato(c.d.DB, id, n)
	if err != nil {
		c.erro(w, err, "ler o abate")
		return
	}
	web.WriteJSON(w, http.StatusOK, map[string]any{"abate": rt})
}

func (c *controller) indicadores(w http.ResponseWriter, r *http.Request) {
	id, err := idPartida(r)
	var pid int
	if err == nil {
		pid, err = participante(r)
	}
	if err != nil {
		c.erro(w, err, "")
		return
	}
	l, err := ListaIndicadores(c.d.DB, id, pid)
	if err != nil {
		c.erro(w, err, "ler os indicadores")
		return
	}
	web.WriteLista(w, "indicadores", l, nil)
}

func (c *controller) saidas(w http.ResponseWriter, r *http.Request) {
	id, err := idPartida(r)
	var pid int
	if err == nil {
		pid, err = participante(r)
	}
	if err != nil {
		c.erro(w, err, "")
		return
	}
	l, err := ListaSaidas(c.d.DB, id, pid)
	if err != nil {
		c.erro(w, err, "ler as saídas de rota")
		return
	}
	web.WriteLista(w, "saidas", l, nil)
}

// GET /api/partidas/{id}/atividades?de=0&ate=600&participante= (segundos)
func (c *controller) atividades(w http.ResponseWriter, r *http.Request) {
	id, err := idPartida(r)
	var pid int
	var de, ate int64
	if err == nil {
		pid, err = participante(r)
	}
	if err == nil {
		de, err = segundos(r, "de", 0)
	}
	if err == nil {
		ate, err = segundos(r, "ate", math.MaxInt64/2)
	}
	if err != nil {
		c.erro(w, err, "")
		return
	}
	l, err := ListaAtividades(c.d.DB, id, pid, de, ate)
	if err != nil {
		c.erro(w, err, "ler as atividades")
		return
	}
	web.WriteLista(w, "atividades", l, map[string]any{"de_ms": de, "ate_ms": ate})
}

func (c *controller) cs(w http.ResponseWriter, r *http.Request) {
	id, err := idPartida(r)
	var pid int
	if err == nil {
		pid, err = participante(r)
	}
	if err != nil {
		c.erro(w, err, "")
		return
	}
	l, err := ConfereCS(c.d.DB, id, pid)
	if err != nil {
		c.erro(w, err, "conferir o CS")
		return
	}
	web.WriteLista(w, "jogadores", l, nil)
}

// GET /api/partidas/{id}/ondas — o estado da onda de tropas em cada rota,
// calculado na hora pelo que está guardado (não grava nada).
func (c *controller) ondas(w http.ResponseWriter, r *http.Request) {
	id, err := idPartida(r)
	if err != nil {
		c.erro(w, err, "")
		return
	}
	o, err := LeOndas(c.d.DB, id)
	if err != nil {
		c.erro(w, err, "deduzir as ondas")
		return
	}
	web.WriteJSON(w, http.StatusOK, o)
}

// GET /api/partidas/{id}/sentinelas — as sentinelas colocadas e destruídas
// (linha do tempo da Riot), com a posição estimada; calculado na hora.
func (c *controller) sentinelas(w http.ResponseWriter, r *http.Request) {
	id, err := idPartida(r)
	if err != nil {
		c.erro(w, err, "")
		return
	}
	s, err := LeSentinelas(c.d.DB, id)
	if err != nil {
		c.erro(w, err, "ler as sentinelas")
		return
	}
	web.WriteJSON(w, http.StatusOK, s)
}
