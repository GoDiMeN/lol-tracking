package rastreio

import (
	"errors"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"

	"loltracking/server/lcu"
	"loltracking/server/partida"
	"loltracking/server/web"
)

// Registra pendura as rotas do rastreio (docs/arquitetura/contrato-http.md).
func Registra(mux *http.ServeMux, f *Fila) {
	c := &controller{f: f}
	mux.HandleFunc("POST /api/partidas/{id}/rastreio", c.cria)
	mux.HandleFunc("GET /api/rastreios", c.lista)
	mux.HandleFunc("GET /api/rastreios/{id}", c.le)
	mux.HandleFunc("POST /api/rastreios/{id}/pausar", c.pausa)
	mux.HandleFunc("POST /api/rastreios/{id}/retomar", c.retoma)
	mux.HandleFunc("POST /api/rastreios/{id}/cancelar", c.cancela)
	mux.HandleFunc("GET /api/rastreios/{id}/eventos", c.eventosSSE)
	mux.HandleFunc("GET /api/partidas/rastreadas", c.rastreadas)
	mux.HandleFunc("GET /api/partidas/{id}/amostras", c.amostras)
	mux.HandleFunc("GET /api/partidas/{id}/estados", c.estados)
	mux.HandleFunc("GET /api/partidas/{id}/momento", c.momento)
	mux.HandleFunc("GET /api/partidas/{id}/eventos", c.eventos)
	mux.HandleFunc("GET /api/partidas/{id}/cadeia", c.cadeia)
}

type controller struct{ f *Fila }

func (c *controller) erro(w http.ResponseWriter, err error, acao string) {
	var inv *ErroInvalido
	var est *ErroEstado
	var ocu *ErroOcupado
	switch {
	case errors.As(err, &inv):
		web.WriteErr(w, http.StatusBadRequest, inv.Msg)
	case errors.As(err, &ocu):
		web.WriteJSON(w, http.StatusConflict, map[string]any{"erro": ocu.Error(), "rastreio_id": ocu.RastreioID,
			"execucao": map[string]string{"id": ocu.Execucao}})
	case errors.As(err, &est):
		web.WriteErr(w, http.StatusConflict, est.Msg)
	case errors.Is(err, partida.ErrNaoEncontrada):
		web.WriteErr(w, http.StatusNotFound, "partida não encontrada")
	case errors.Is(err, ErrNaoEncontrado):
		web.WriteErr(w, http.StatusNotFound, "rastreio não encontrado")
	case errors.Is(err, ErrSemAmostras):
		web.WriteErr(w, http.StatusNotFound, ErrSemAmostras.Error())
	default:
		if status, msg, ok := lcu.StatusHTTP(err); ok {
			web.WriteErr(w, status, msg)
			return
		}
		log.Printf("rastreio: %s: %v", acao, err)
		web.WriteErr(w, http.StatusInternalServerError, "não foi possível "+acao)
	}
}

func idDe(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, &ErroInvalido{"id inválido"}
	}
	return id, nil
}

// segundos lê um parâmetro em segundos (aceita fração) e devolve ms.
func segundos(r *http.Request, nome string, padrao int64) (int64, error) {
	s := r.URL.Query().Get(nome)
	if s == "" {
		return padrao, nil
	}
	v, err := strconv.ParseFloat(strings.Replace(s, ",", ".", 1), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return 0, &ErroInvalido{nome + " precisa ser um número de segundos ≥ 0"}
	}
	return int64(math.Round(v * 1000)), nil
}

func inteiro(r *http.Request, nome string, padrao int) (int, error) {
	s := r.URL.Query().Get(nome)
	if s == "" {
		return padrao, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, &ErroInvalido{nome + " precisa ser um número inteiro"}
	}
	return n, nil
}

func (c *controller) comExecucao(r *Rastreio) {
	if id, ok := c.f.ExecucaoDe(r.ID); ok {
		r.Execucao = &IDExec{ID: id}
	}
}

// POST /api/partidas/{id}/rastreio {"jogadores": [ids]?, "velocidade": 2?}
func (c *controller) cria(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	if err != nil {
		c.erro(w, err, "")
		return
	}
	var corpo struct {
		Jogadores  []int   `json:"jogadores"`
		Velocidade float64 `json:"velocidade"`
	}
	if err := web.LeJSON(w, r, &corpo); err != nil {
		web.WriteErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ras, exID, err := c.f.Cria(r.Context(), id, Pedido{Jogadores: corpo.Jogadores, Velocidade: corpo.Velocidade})
	if err != nil {
		c.erro(w, err, "criar o rastreio")
		return
	}
	ras.Execucao = &IDExec{ID: exID}
	web.WriteJSON(w, http.StatusAccepted, map[string]any{"rastreio": ras, "execucao": map[string]string{"id": exID}})
}

// GET /api/rastreios?limite=50
func (c *controller) lista(w http.ResponseWriter, r *http.Request) {
	lim, err := inteiro(r, "limite", 50)
	if err != nil || lim < 1 || lim > 500 {
		web.WriteErr(w, http.StatusBadRequest, "limite precisa ser de 1 a 500")
		return
	}
	l, err := leRastreios(c.f.d.DB, `ORDER BY id DESC LIMIT ?`, lim)
	if err != nil {
		c.erro(w, err, "listar os rastreios")
		return
	}
	for i := range l {
		c.comExecucao(&l[i])
	}
	web.WriteLista(w, "rastreios", l, nil)
}

// GET /api/rastreios/{id}
func (c *controller) le(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	if err != nil {
		c.erro(w, err, "")
		return
	}
	ras, err := LeRastreio(c.f.d.DB, id)
	if err != nil {
		c.erro(w, err, "ler o rastreio")
		return
	}
	c.comExecucao(&ras)
	ps, err := lePasses(c.f.d.DB, `WHERE p.rastreio_id = ? ORDER BY p.ordem`, id)
	if err != nil {
		c.erro(w, err, "ler o rastreio")
		return
	}
	web.WriteJSON(w, http.StatusOK, map[string]any{"rastreio": ras, "passes": web.NuncaNil(ps)})
}

func (c *controller) pausa(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	if err == nil {
		err = c.f.Pausa(id)
	}
	if err != nil {
		c.erro(w, err, "pausar o rastreio")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (c *controller) retoma(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	var exID string
	if err == nil {
		exID, err = c.f.Retoma(id)
	}
	if err != nil {
		c.erro(w, err, "retomar o rastreio")
		return
	}
	web.WriteJSON(w, http.StatusAccepted, map[string]any{"execucao": map[string]string{"id": exID}})
}

func (c *controller) cancela(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	if err == nil {
		err = c.f.Cancela(id)
	}
	if err != nil {
		c.erro(w, err, "cancelar o rastreio")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/rastreios/{id}/eventos — o SSE da execução mais recente do rastreio.
func (c *controller) eventosSSE(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	if err != nil {
		c.erro(w, err, "")
		return
	}
	exID, ok := c.f.ExecucaoDe(id)
	if !ok {
		web.WriteErr(w, http.StatusNotFound, "este rastreio não tem execução recente")
		return
	}
	c.f.d.Hub.Servir(w, r, exID)
}

func (c *controller) rastreadas(w http.ResponseWriter, r *http.Request) {
	l, err := ListaRastreadas(c.f.d.DB)
	if err != nil {
		c.erro(w, err, "listar as partidas rastreadas")
		return
	}
	web.WriteLista(w, "partidas", l, nil)
}

// GET /api/partidas/{id}/amostras?participante=3&de=0&ate=600&passo=0&limite=5000 (tempos em segundos)
func (c *controller) amostras(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	if err != nil {
		c.erro(w, err, "")
		return
	}
	pid, err := inteiro(r, "participante", 0)
	if err == nil && (pid < 1 || pid > 10) {
		err = &ErroInvalido{"participante precisa ser de 1 a 10"}
	}
	var de, ate, passo int64
	if err == nil {
		de, err = segundos(r, "de", 0)
	}
	if err == nil {
		ate, err = segundos(r, "ate", math.MaxInt64/2)
	}
	if err == nil {
		passo, err = segundos(r, "passo", 0)
	}
	lim := 0
	if err == nil {
		lim, err = inteiro(r, "limite", 5000)
		if err == nil && (lim < 1 || lim > 20000) {
			err = &ErroInvalido{"limite precisa ser de 1 a 20000"}
		}
	}
	if err != nil {
		c.erro(w, err, "")
		return
	}
	pg, err := ListaAmostras(c.f.d.DB, id, pid, de, ate, passo, lim)
	if err != nil {
		c.erro(w, err, "ler as amostras")
		return
	}
	web.WriteJSON(w, http.StatusOK, pg)
}

// GET /api/partidas/{id}/estados?de=0&ate=60&passo=1 (segundos)
func (c *controller) estados(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	var de, ate, passo int64
	if err == nil {
		de, err = segundos(r, "de", 0)
	}
	if err == nil {
		ate, err = segundos(r, "ate", de+60000)
	}
	if err == nil {
		passo, err = segundos(r, "passo", 1000)
		if err == nil && passo < 100 {
			err = &ErroInvalido{"passo mínimo é 0,1 s"}
		}
	}
	if err == nil && ate < de {
		err = &ErroInvalido{"ate precisa ser ≥ de"}
	}
	if err != nil {
		c.erro(w, err, "")
		return
	}
	qs, err := ListaEstados(c.f.d.DB, id, de, ate, passo)
	if err != nil {
		c.erro(w, err, "ler o estado dos jogadores")
		return
	}
	web.WriteLista(w, "quadros", qs, map[string]any{"de_ms": de, "ate_ms": ate, "passo_ms": passo})
}

// GET /api/partidas/{id}/momento?t=513
func (c *controller) momento(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	var t int64
	if err == nil {
		if r.URL.Query().Get("t") == "" {
			err = &ErroInvalido{"informe t (segundos)"}
		} else {
			t, err = segundos(r, "t", 0)
		}
	}
	if err != nil {
		c.erro(w, err, "")
		return
	}
	qs, err := ListaEstados(c.f.d.DB, id, t, t, 1000)
	if err != nil {
		c.erro(w, err, "ler o momento")
		return
	}
	web.WriteJSON(w, http.StatusOK, map[string]any{"quadro": qs[0]})
}

// GET /api/partidas/{id}/eventos?tipo=abate
func (c *controller) eventos(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	if err != nil {
		c.erro(w, err, "")
		return
	}
	evs, err := ListaEventos(c.f.d.DB, id, r.URL.Query().Get("tipo"))
	if err != nil {
		c.erro(w, err, "ler os eventos")
		return
	}
	web.WriteLista(w, "eventos", evs, nil)
}

// GET /api/partidas/{id}/cadeia?participante=3&tipos=morte,abate
func (c *controller) cadeia(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	pid := 0
	if err == nil {
		pid, err = inteiro(r, "participante", 0)
		if err == nil && (pid < 1 || pid > 10) {
			err = &ErroInvalido{"participante precisa ser de 1 a 10"}
		}
	}
	if err != nil {
		c.erro(w, err, "")
		return
	}
	tipos := map[string]bool{}
	for _, t := range strings.Split(r.URL.Query().Get("tipos"), ",") {
		if t = strings.TrimSpace(t); t != "" {
			tipos[t] = true
		}
	}
	rc, err := ListaCadeia(c.f.d.DB, id, pid, tipos)
	if err != nil {
		c.erro(w, err, "ler a cadeia")
		return
	}
	web.WriteJSON(w, http.StatusOK, rc)
}
