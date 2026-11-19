package ia

import (
	"errors"
	"net/http"
	"strconv"

	"loltracking/server/web"
)

// registraEspecialista pendura as rotas do Especialista Lane
// (docs/arquitetura/contrato-http.md, "Especialista Lane").
func registraEspecialista(mux *http.ServeMux, c *controller) {
	mux.HandleFunc("GET /api/especialista", c.painelEsp)
	mux.HandleFunc("GET /api/especialista/relatorios/{game}/{pid}", c.leRelEsp)
	mux.HandleFunc("POST /api/especialista/relatorios/{game}/{pid}", c.geraRelEsp)
	mux.HandleFunc("GET /api/especialista/relatorios/{game}/{pid}/previa", c.previaRelEsp)
	mux.HandleFunc("GET /api/especialista/sintese", c.leSintese)
	mux.HandleFunc("POST /api/especialista/sintese", c.refazSintese)
	mux.HandleFunc("GET /api/especialista/sintese/previa", c.previaSintese)
	mux.HandleFunc("GET /api/especialista/comparacao", c.comparacaoPatches)
	mux.HandleFunc("GET /api/especialista/espelho", c.estadoEspelho)
	mux.HandleFunc("PUT /api/especialista/espelho", c.defineCaderno)
	mux.HandleFunc("POST /api/especialista/espelho", c.enviaEspelho)
}

// erroEsp: os erros próprios do especialista; o resto vai para c.erro.
func (c *controller) erroEsp(w http.ResponseWriter, err error, acao string) {
	var ee *ErrEspelho
	switch {
	case errors.Is(err, ErrSemRelatorios):
		web.WriteErr(w, http.StatusConflict, err.Error())
	case errors.As(err, &ee):
		web.WriteErr(w, http.StatusConflict, ee.Msg)
	default:
		c.erro(w, err, acao)
	}
}

func jogoEParticipante(r *http.Request) (int64, int, error) {
	g, err := strconv.ParseInt(r.PathValue("game"), 10, 64)
	if err != nil || g <= 0 {
		return 0, 0, &ErrPedido{"partida inválida"}
	}
	p, err := strconv.Atoi(r.PathValue("pid"))
	if err != nil || p < 1 || p > 10 {
		return 0, 0, &ErrPedido{"participante precisa ser de 1 a 10"}
	}
	return g, p, nil
}

func recorteDe(r *http.Request) (Recorte, error) {
	q := r.URL.Query()
	return validaRecorte(Recorte{Patch: q.Get("patch"), Elo: q.Get("elo"), Funcao: q.Get("rota")})
}

// GET /api/especialista
func (c *controller) painelEsp(w http.ResponseWriter, r *http.Request) {
	p, err := c.s.MontaPainel(r.Context())
	if err != nil {
		c.erroEsp(w, err, "montar o painel do Especialista Lane")
		return
	}
	web.WriteJSON(w, http.StatusOK, p)
}

// GET /api/especialista/relatorios/{game}/{pid}?versao=V
func (c *controller) leRelEsp(w http.ResponseWriter, r *http.Request) {
	g, p, err := jogoEParticipante(r)
	var versao int
	if err == nil {
		versao, err = inteiroDe(r, "versao", 1, 1<<30)
	}
	if err != nil {
		c.erroEsp(w, err, "")
		return
	}
	rel, ok, err := LeRelatorioReferencia(c.s.DB, g, p, versao)
	if err != nil {
		c.erroEsp(w, err, "ler o relatório de referência")
		return
	}
	vs, err := VersoesReferencia(c.s.DB, g, p)
	if err != nil {
		c.erroEsp(w, err, "ler as versões")
		return
	}
	var corpo any
	if ok {
		corpo = rel
	}
	web.WriteJSON(w, http.StatusOK, map[string]any{"relatorio": corpo, "versoes": web.NuncaNil(vs),
		"execucao": execucaoJSON(c.s.execucaoDe(chaveRelEsp(g, p)))})
}

// POST /api/especialista/relatorios/{game}/{pid}
func (c *controller) geraRelEsp(w http.ResponseWriter, r *http.Request) {
	g, p, err := jogoEParticipante(r)
	if err != nil {
		c.erroEsp(w, err, "")
		return
	}
	rel, e, err := c.s.GeraReferencia(r.Context(), g, p)
	if err != nil {
		c.erroEsp(w, err, "começar o relatório de referência")
		return
	}
	web.WriteJSON(w, http.StatusAccepted, map[string]any{"relatorio": rel, "execucao": execucaoJSON(e)})
}

// GET /api/especialista/relatorios/{game}/{pid}/previa
func (c *controller) previaRelEsp(w http.ResponseWriter, r *http.Request) {
	g, p, err := jogoEParticipante(r)
	if err != nil {
		c.erroEsp(w, err, "")
		return
	}
	ped, env, err := c.s.PreviaReferencia(r.Context(), g, p)
	if err != nil {
		c.erroEsp(w, err, "montar o texto do relatório de referência")
		return
	}
	web.WriteJSON(w, http.StatusOK, map[string]any{"sistema": ped.Sistema, "mensagem": TextoDaConversa(ped.Mensagens), "enviado": env})
}

// GET /api/especialista/sintese?patch=&elo=&rota=&versao=
func (c *controller) leSintese(w http.ResponseWriter, r *http.Request) {
	rc, err := recorteDe(r)
	var versao int
	if err == nil {
		versao, err = inteiroDe(r, "versao", 1, 1<<30)
	}
	if err != nil {
		c.erroEsp(w, err, "")
		return
	}
	x, ok, err := LeSintese(c.s.DB, rc, versao)
	if err != nil {
		c.erroEsp(w, err, "ler a síntese")
		return
	}
	vs, err := VersoesSintese(c.s.DB, rc)
	if err != nil {
		c.erroEsp(w, err, "ler as versões")
		return
	}
	comp, err := c.s.ComparacaoDoRecorte(rc)
	if err != nil {
		c.erroEsp(w, err, "comparar os patches")
		return
	}
	var corpo any
	if ok {
		corpo = x
	}
	web.WriteJSON(w, http.StatusOK, map[string]any{"sintese": corpo, "versoes": web.NuncaNil(vs), "comparacao": comp,
		"recorte":  map[string]any{"patch": rc.Patch, "elo": rc.Elo, "rota": rc.Funcao, "nome": rc.Nome(), "arquivo": rc.ArquivoSintese()},
		"execucao": execucaoJSON(c.s.execucaoDe(chaveSintese(rc)))})
}

// POST /api/especialista/sintese {"patch", "elo", "rota"}
func (c *controller) refazSintese(w http.ResponseWriter, r *http.Request) {
	var corpo Recorte
	if err := web.LeJSON(w, r, &corpo); err != nil {
		c.erroEsp(w, &ErrPedido{err.Error()}, "")
		return
	}
	rc, err := validaRecorte(corpo)
	if err != nil {
		c.erroEsp(w, err, "")
		return
	}
	x, e, err := c.s.RefazSintese(r.Context(), rc)
	if err != nil {
		c.erroEsp(w, err, "começar a síntese")
		return
	}
	web.WriteJSON(w, http.StatusAccepted, map[string]any{"sintese": x, "execucao": execucaoJSON(e)})
}

// GET /api/especialista/sintese/previa?patch=&elo=&rota=
func (c *controller) previaSintese(w http.ResponseWriter, r *http.Request) {
	rc, err := recorteDe(r)
	if err != nil {
		c.erroEsp(w, err, "")
		return
	}
	p, env, err := c.s.PreviaSintese(rc)
	if err != nil {
		c.erroEsp(w, err, "montar o texto da síntese")
		return
	}
	web.WriteJSON(w, http.StatusOK, map[string]any{"sistema": p.Sistema, "mensagem": TextoDaConversa(p.Mensagens), "enviado": env})
}

// GET /api/especialista/comparacao?patch=&elo=&rota=
func (c *controller) comparacaoPatches(w http.ResponseWriter, r *http.Request) {
	rc, err := recorteDe(r)
	if err != nil {
		c.erroEsp(w, err, "")
		return
	}
	comp, err := c.s.ComparacaoDoRecorte(rc)
	if err != nil {
		c.erroEsp(w, err, "comparar os patches")
		return
	}
	web.WriteJSON(w, http.StatusOK, map[string]any{"comparacao": comp})
}

// GET /api/especialista/espelho
func (c *controller) estadoEspelho(w http.ResponseWriter, r *http.Request) {
	web.WriteJSON(w, http.StatusOK, map[string]any{"espelho": c.s.EstadoDoEspelho(r.Context(), false)})
}

// PUT /api/especialista/espelho {"caderno": "uuid"|""}
func (c *controller) defineCaderno(w http.ResponseWriter, r *http.Request) {
	var corpo struct {
		Caderno string `json:"caderno"`
	}
	if err := web.LeJSON(w, r, &corpo); err != nil {
		c.erroEsp(w, &ErrPedido{err.Error()}, "")
		return
	}
	if err := c.s.DefineCaderno(corpo.Caderno); err != nil {
		c.erroEsp(w, err, "guardar o caderno")
		return
	}
	web.WriteJSON(w, http.StatusOK, map[string]any{"espelho": c.s.EstadoDoEspelho(r.Context(), false)})
}

// POST /api/especialista/espelho
func (c *controller) enviaEspelho(w http.ResponseWriter, r *http.Request) {
	e, err := c.s.EnviaEspelho(r.Context())
	if err != nil {
		c.erroEsp(w, err, "enviar ao NotebookLM")
		return
	}
	web.WriteJSON(w, http.StatusAccepted, map[string]any{"execucao": execucaoJSON(e)})
}
