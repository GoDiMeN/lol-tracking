package ia

import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"loltracking/server/analise"
	"loltracking/server/partida"
	"loltracking/server/web"
)

// Registra pendura as rotas da IA (docs/arquitetura/contrato-http.md, "IA").
func Registra(mux *http.ServeMux, s *Servico) {
	c := &controller{s: s}
	mux.HandleFunc("GET /api/ia/estado", c.estado)
	mux.HandleFunc("GET /api/partidas/{id}/ia/relatorio", c.leRelatorio)
	mux.HandleFunc("POST /api/partidas/{id}/ia/relatorio", c.analisa)
	mux.HandleFunc("GET /api/partidas/{id}/ia/relatorios", c.relatorios)
	mux.HandleFunc("GET /api/partidas/{id}/ia/previa", c.previa)
	mux.HandleFunc("GET /api/partidas/{id}/ia/referencias", c.referencias)
	mux.HandleFunc("PUT /api/partidas/{id}/ia/elo", c.defineElo)
	mux.HandleFunc("GET /api/ia/conversas", c.conversas)
	mux.HandleFunc("POST /api/ia/conversas", c.criaConversa)
	mux.HandleFunc("GET /api/ia/conversas/{id}", c.conversa)
	mux.HandleFunc("DELETE /api/ia/conversas/{id}", c.apagaConversa)
	mux.HandleFunc("POST /api/ia/conversas/{id}/mensagens", c.envia)
	// O Especialista Lane é parte do Estudo: some com o interruptor desligado.
	if !s.SemEstudo {
		registraEspecialista(mux, c)
	}
}

type controller struct{ s *Servico }

func execucaoJSON(e *web.Execucao) any {
	if e == nil {
		return nil
	}
	return map[string]string{"id": e.ID()}
}

func (c *controller) erro(w http.ResponseWriter, err error, acao string) {
	var ep *ErrPedido
	var er *ErrRodando
	var ei *erroIA
	switch {
	case errors.As(err, &ep):
		web.WriteErr(w, http.StatusBadRequest, ep.Msg)
	case errors.As(err, &er):
		web.WriteJSON(w, http.StatusConflict, map[string]any{"erro": er.Msg, "execucao": execucaoJSON(er.Execucao)})
	case errors.Is(err, partida.ErrNaoEncontrada):
		web.WriteErr(w, http.StatusNotFound, "partida não encontrada")
	case errors.Is(err, ErrConversaNaoEncontrada):
		web.WriteErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, analise.ErrSemResumo), errors.Is(err, analise.ErrSemLinhaDoTempo):
		web.WriteErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrIndisponivel) && errors.As(err, &ei):
		web.WriteErr(w, http.StatusServiceUnavailable, ei.frase)
	default:
		log.Printf("ia: %s: %v", acao, err)
		web.WriteErr(w, http.StatusInternalServerError, "não foi possível "+acao)
	}
}

func idDe(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, &ErrPedido{"id inválido"}
	}
	return id, nil
}

func inteiroDe(r *http.Request, nome string, min, max int) (int, error) {
	s := r.URL.Query().Get(nome)
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < min || n > max {
		return 0, &ErrPedido{nome + " fora do intervalo"}
	}
	return n, nil
}

// GET /api/ia/estado
func (c *controller) estado(w http.ResponseWriter, r *http.Request) {
	e := c.s.IA.Estado(r.Context())
	web.WriteJSON(w, http.StatusOK, map[string]any{
		"disponivel": e.Disponivel, "implementacao": e.Implementacao, "versao": e.Versao, "motivo": e.Motivo, "modelos": e.Modelos,
		"fila":         map[string]any{"ocupada": c.s.IA.Ocupada(), "esperando": c.s.IA.Esperando()},
		"vozes":        Vozes,
		"conhecimento": c.s.Conhecimento,
		// Por voz: de onde vem o material e quantos temas há (temas.go).
		"material":          InventarioConhecimento(c.s.Conhecimento),
		"teto_material_voz": TetoMaterialVoz,
	})
}

// GET /api/partidas/{id}/ia/relatorio?participante=N&versao=V
func (c *controller) leRelatorio(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	var pid, versao int
	if err == nil {
		pid, err = inteiroDe(r, "participante", 1, 10)
	}
	if err == nil && pid == 0 {
		err = &ErrPedido{"informe ?participante= (1 a 10)"}
	}
	if err == nil {
		versao, err = inteiroDe(r, "versao", 1, 1<<30)
	}
	if err != nil {
		c.erro(w, err, "")
		return
	}
	rel, ok, err := LeRelatorio(c.s.DB, id, pid, versao)
	if err != nil {
		c.erro(w, err, "ler o relatório")
		return
	}
	vs, err := ListaVersoes(c.s.DB, id, pid)
	if err != nil {
		c.erro(w, err, "ler as versões")
		return
	}
	var corpo any
	if ok {
		corpo = rel
	}
	web.WriteJSON(w, http.StatusOK, map[string]any{"relatorio": corpo, "versoes": web.NuncaNil(vs),
		"execucao": execucaoJSON(c.s.execucaoDe(chaveRelatorio(id, pid)))})
}

// POST /api/partidas/{id}/ia/relatorio {"participante": N}
func (c *controller) analisa(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	var corpo struct {
		Participante int `json:"participante"`
	}
	if err == nil {
		err = web.LeJSON(w, r, &corpo)
		if err != nil {
			err = &ErrPedido{err.Error()}
		}
	}
	if err == nil && (corpo.Participante < 1 || corpo.Participante > 10) {
		err = &ErrPedido{"participante precisa ser de 1 a 10"}
	}
	if err != nil {
		c.erro(w, err, "")
		return
	}
	rel, e, err := c.s.Analisa(r.Context(), id, corpo.Participante)
	if err != nil {
		c.erro(w, err, "começar a análise com IA")
		return
	}
	web.WriteJSON(w, http.StatusAccepted, map[string]any{"relatorio": rel, "execucao": execucaoJSON(e)})
}

// GET /api/partidas/{id}/ia/previa?participante=N: o texto exato que a
// análise mandaria (nenhuma chamada à IA).
func (c *controller) previa(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	var pid int
	if err == nil {
		pid, err = inteiroDe(r, "participante", 1, 10)
	}
	if err == nil && pid == 0 {
		err = &ErrPedido{"informe ?participante= (1 a 10)"}
	}
	if err != nil {
		c.erro(w, err, "")
		return
	}
	p, env, vozes, err := c.s.PreviaAnalise(r.Context(), id, pid)
	if err != nil {
		c.erro(w, err, "montar o texto da análise")
		return
	}
	web.WriteJSON(w, http.StatusOK, map[string]any{"sistema": p.Sistema, "mensagem": TextoDaConversa(p.Mensagens), "enviado": env,
		"vozes": web.NuncaNil(vozes)})
}

// GET /api/partidas/{id}/ia/relatorios: quem já tem relatório concluído.
func (c *controller) relatorios(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	if err != nil {
		c.erro(w, err, "")
		return
	}
	vs, pids, err := RelatoriosDaPartida(c.s.DB, id)
	if err != nil {
		c.erro(w, err, "ler os relatórios")
		return
	}
	l := []map[string]any{}
	for i := range vs {
		l = append(l, map[string]any{"participante_id": pids[i], "versao": vs[i].Versao})
	}
	web.WriteLista(w, "relatorios", l, nil)
}

// GET /api/ia/conversas?partida=ID&participante=N (partida 0 ou ausente = gerais)
func (c *controller) conversas(w http.ResponseWriter, r *http.Request) {
	var gid int64
	if s := r.URL.Query().Get("partida"); s != "" {
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil || v < 0 {
			c.erro(w, &ErrPedido{"partida inválida"}, "")
			return
		}
		gid = v
	}
	pid, err := inteiroDe(r, "participante", 1, 10)
	if err != nil {
		c.erro(w, err, "")
		return
	}
	l, err := ListaConversas(c.s.DB, gid, pid)
	if err != nil {
		c.erro(w, err, "listar as conversas")
		return
	}
	web.WriteLista(w, "conversas", l, nil)
}

// POST /api/ia/conversas {"game_id": 0|id, "participante": N}
func (c *controller) criaConversa(w http.ResponseWriter, r *http.Request) {
	var corpo struct {
		GameID       int64 `json:"game_id"`
		Participante int   `json:"participante"`
	}
	if err := web.LeJSON(w, r, &corpo); err != nil {
		c.erro(w, &ErrPedido{err.Error()}, "")
		return
	}
	if corpo.GameID < 0 || (corpo.GameID > 0 && (corpo.Participante < 1 || corpo.Participante > 10)) {
		c.erro(w, &ErrPedido{"conversa de partida precisa de participante de 1 a 10"}, "")
		return
	}
	cv, err := c.s.CriaConversa(r.Context(), corpo.GameID, corpo.Participante)
	if err != nil {
		c.erro(w, err, "abrir a conversa")
		return
	}
	web.WriteJSON(w, http.StatusCreated, map[string]any{"conversa": cv})
}

// GET /api/ia/conversas/{id}
func (c *controller) conversa(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	if err != nil {
		c.erro(w, err, "")
		return
	}
	cv, err := LeConversa(c.s.DB, id)
	if err != nil {
		c.erro(w, err, "ler a conversa")
		return
	}
	ms, err := ListaMensagens(c.s.DB, id)
	if err != nil {
		c.erro(w, err, "ler as mensagens")
		return
	}
	web.WriteJSON(w, http.StatusOK, map[string]any{"conversa": cv, "mensagens": web.NuncaNil(ms),
		"execucao": execucaoJSON(c.s.ExecucaoDaConversa(id))})
}

// DELETE /api/ia/conversas/{id}
func (c *controller) apagaConversa(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	if err == nil {
		err = c.s.ApagaConversa(id)
	}
	if err != nil {
		c.erro(w, err, "apagar a conversa")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/ia/conversas/{id}/mensagens {"texto", "puuid"?}
func (c *controller) envia(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	var corpo struct {
		Texto string `json:"texto"`
		Puuid string `json:"puuid"`
	}
	if err == nil {
		if e := web.LeJSON(w, r, &corpo); e != nil {
			err = &ErrPedido{e.Error()}
		}
	}
	if err != nil {
		c.erro(w, err, "")
		return
	}
	u, a, e, err := c.s.Envia(r.Context(), id, corpo.Texto, corpo.Puuid)
	if err != nil {
		c.erro(w, err, "mandar a pergunta")
		return
	}
	web.WriteJSON(w, http.StatusAccepted, map[string]any{"pergunta": u, "resposta": a, "execucao": execucaoJSON(e)})
}

// GET /api/partidas/{id}/ia/referencias?participante=N: o elo de comparação
// do jogador em foco e, por voz, o material que ela teria e o que falta.
func (c *controller) referencias(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	var pid int
	if err == nil {
		pid, err = inteiroDe(r, "participante", 1, 10)
	}
	if err == nil && pid == 0 {
		err = &ErrPedido{"informe ?participante= (1 a 10)"}
	}
	if err != nil {
		c.erro(w, err, "")
		return
	}
	c.respondeReferencias(w, r, id, pid)
}

func (c *controller) respondeReferencias(w http.ResponseWriter, r *http.Request, id int64, pid int) {
	elo, vozes, err := c.s.ReferenciasDoJogador(r.Context(), id, pid)
	if err != nil {
		c.erro(w, err, "ler as referências das vozes")
		return
	}
	web.WriteJSON(w, http.StatusOK, map[string]any{"elo": elo, "vozes": web.NuncaNil(vozes)})
}

// PUT /api/partidas/{id}/ia/elo {"participante": N, "elo": "PLATINUM"|""}:
// o elo com que a voz dos melhores do elo compara este jogador (guardado por
// jogador; "" volta ao padrão: Riot, ou o elo da coleta).
func (c *controller) defineElo(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	var corpo struct {
		Participante int    `json:"participante"`
		Elo          string `json:"elo"`
	}
	if err == nil {
		if e := web.LeJSON(w, r, &corpo); e != nil {
			err = &ErrPedido{e.Error()}
		}
	}
	if err == nil && (corpo.Participante < 1 || corpo.Participante > 10) {
		err = &ErrPedido{"participante precisa ser de 1 a 10"}
	}
	if err == nil {
		err = c.s.DefineEloDoJogador(r.Context(), id, corpo.Participante, corpo.Elo)
	}
	if err != nil {
		c.erro(w, err, "guardar o elo")
		return
	}
	c.respondeReferencias(w, r, id, corpo.Participante)
}
