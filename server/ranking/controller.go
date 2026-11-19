package ranking

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"loltracking/server/lcu"
	"loltracking/server/riot"
	"loltracking/server/web"
)

// Registra pendura as rotas do ranking (docs/arquitetura/contrato-http.md, "Ranking do Lane").
func Registra(mux *http.ServeMux, a *Atualizador) {
	c := &controller{a: a}
	mux.HandleFunc("GET /api/rankings", c.indice)
	mux.HandleFunc("GET /api/rankings/formula", c.formula)
	mux.HandleFunc("GET /api/rankings/orcamento", c.orcamento)
	mux.HandleFunc("GET /api/rankings/recorte", c.recorte)
	mux.HandleFunc("GET /api/rankings/comparar", c.comparar)
	mux.HandleFunc("GET /api/rankings/{id}", c.porID)
	mux.HandleFunc("GET /api/rankings/atualizacoes", c.lista)
	mux.HandleFunc("POST /api/rankings/atualizacoes", c.cria)
	mux.HandleFunc("GET /api/rankings/atualizacoes/{id}", c.le)
	mux.HandleFunc("POST /api/rankings/atualizacoes/{id}/pausar", c.pausa)
	mux.HandleFunc("POST /api/rankings/atualizacoes/{id}/retomar", c.retoma)
	mux.HandleFunc("POST /api/rankings/atualizacoes/{id}/cancelar", c.cancela)
	mux.HandleFunc("GET /api/rankings/atualizacoes/{id}/eventos", c.eventos)
}

type controller struct{ a *Atualizador }

func (c *controller) erro(w http.ResponseWriter, err error, acao string) {
	var ep *ErroPedido
	var ec *ErroConflito
	switch {
	case errors.As(err, &ep):
		web.WriteErr(w, http.StatusBadRequest, ep.Msg)
	case errors.As(err, &ec):
		web.WriteErr(w, http.StatusConflict, ec.Msg)
	case errors.Is(err, ErrNaoEncontrado):
		web.WriteErr(w, http.StatusNotFound, acao+": não encontrado")
	default:
		if status, msg, ok := lcu.StatusHTTP(err); ok {
			web.WriteErr(w, status, msg)
			return
		}
		log.Printf("ranking: %s: %v", acao, err)
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

func regiaoDe(r *http.Request) string {
	if v := strings.TrimSpace(r.URL.Query().Get("regiao")); v != "" {
		return v
	}
	return riot.RegiaoPadrao
}

func modoDe(r *http.Request) (string, error) {
	m := strings.TrimSpace(r.URL.Query().Get("modo"))
	switch m {
	case "":
		return ModoSubindo, nil
	case ModoSubindo, ModoMelhor:
		return m, nil
	}
	return "", &ErroPedido{`modo precisa ser "subindo" ou "melhor"`}
}

// recorteDe lê rota ou campeao da consulta.
func recorteDe(r *http.Request) (string, int, error) {
	q := r.URL.Query()
	rota := strings.ToUpper(strings.TrimSpace(q.Get("rota")))
	camp, _ := strconv.Atoi(q.Get("campeao"))
	switch {
	case rota != "" && camp > 0:
		return "", 0, &ErroPedido{"use rota ou campeao, não os dois"}
	case camp > 0:
		return "", camp, nil
	case NomeRota[rota] != "":
		return rota, 0, nil
	}
	return "", 0, &ErroPedido{"rota precisa ser TOP, JUNGLE, MIDDLE, BOTTOM ou UTILITY (ou use campeao=<id>)"}
}

// GET /api/rankings?regiao&patch&elo: os patches com ranking e, com patch e
// elo, os recortes (rotas e campeões) com o ranking mais recente de cada.
func (c *controller) indice(w http.ResponseWriter, r *http.Request) {
	reg := regiaoDe(r)
	q := r.URL.Query()
	ps, err := Patches(c.a.d.DB, reg)
	if err != nil {
		c.erro(w, err, "listar os rankings")
		return
	}
	patch, elo := q.Get("patch"), strings.ToUpper(q.Get("elo"))
	if patch == "" && len(ps) > 0 {
		patch = ps[0]
	}
	elos := []string{}
	if linhas, err := c.a.d.DB.Query(`SELECT DISTINCT elo FROM rankings WHERE regiao = ? AND patch = ?`, reg, patch); err == nil {
		tem := map[string]bool{}
		for linhas.Next() {
			var e string
			linhas.Scan(&e)
			tem[e] = true
		}
		linhas.Close()
		for _, e := range Elos {
			if tem[e] {
				elos = append(elos, e)
			}
		}
	}
	recs := []Ranking{}
	if patch != "" && elo != "" {
		recs, err = Recortes(c.a.d.DB, reg, patch, elo)
		if err != nil {
			c.erro(w, err, "listar os rankings")
			return
		}
		for i := range recs {
			c.poeCampeao(r.Context(), &recs[i])
		}
	}
	web.WriteJSON(w, http.StatusOK, map[string]any{"regiao": reg, "patch": patch, "elo": elo, "patches": ps, "elos": elos,
		"recortes": web.NuncaNil(recs)})
}

func (c *controller) poeCampeao(ctx context.Context, rk *Ranking) {
	if rk.CampeaoID > 0 {
		rk.Campeao = c.a.nomeCampeao(ctx, rk.CampeaoID)
	}
}

// FormulaTipo é a fórmula de um tipo num modo, para a tela.
type FormulaTipo struct {
	Pesos []PesoTexto `json:"pesos"`
	Texto string      `json:"texto"`
}

// PesoTexto: o peso com o rótulo e a regra.
type PesoTexto struct {
	Componente string  `json:"componente"`
	Rotulo     string  `json:"rotulo"`
	Peso       float64 `json:"peso"`
	Como       string  `json:"como"`
}

// Formula monta a fórmula de um tipo e modo.
func Formula(tipo, modo string) FormulaTipo {
	f := FormulaTipo{}
	var partes []string
	for _, p := range Pesos[tipo][modo] {
		f.Pesos = append(f.Pesos, PesoTexto{Componente: p.Componente, Rotulo: NomeComponente[p.Componente], Peso: p.Peso, Como: ComoCalcula[p.Componente]})
		sinal := "+"
		if p.Peso < 0 {
			sinal = "−"
		}
		partes = append(partes, fmt.Sprintf("%s %.2f × %s", sinal, abs(p.Peso), NomeComponente[p.Componente]))
	}
	t := strings.TrimPrefix(strings.Join(partes, " "), "+ ")
	f.Texto = "nota = 100 × (" + strings.ReplaceAll(t, ".", ",") + ")"
	return f
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// FormulaCompleta: as fórmulas dos dois tipos e modos, os limiares e as regras.
func FormulaCompleta() map[string]any {
	tipos := map[string]map[string]FormulaTipo{}
	for _, t := range []string{TipoRota, TipoCampeao} {
		tipos[t] = map[string]FormulaTipo{}
		for _, m := range Modos {
			tipos[t][m] = Formula(t, m)
		}
	}
	return map[string]any{
		"versao_regra": VersaoRegra,
		"tipos":        tipos,
		"limiares": map[string]any{
			"z_wilson": ZWilson, "wilson_piso": WilsonPiso, "wilson_teto": WilsonTeto, "volume_temporada_ref": VolumeTemporadaRef,
			"volume_recorte_ref": VolumeRecorteRef, "recentes_k": RecentesK, "min_partidas_recorte": MinPartidasRecorte,
			"amostra_pequena": AmostraPequena, "min_jogadores_campeao": MinJogadoresCampeao, "duracao_minima_s": DuracaoMinimaS,
			"paginas_por_divisao": PaginasPorDivisao, "teto_candidatos_padrao": TetoCandidatosPadrao, "teto_partidas_padrao": TetoPartidasPadrao,
			"fim_historico_lcu": FimHistoricoLCU,
		},
		"regras": []string{
			"Só ranqueada solo/duo (fila 420) do patch pedido, sem remake (menos de 5 min ou rendição antecipada).",
			fmt.Sprintf("Entra no ranking da rota quem tem pelo menos %d partidas do patch nela; no de campeão, %d com ele. Com menos de %d, a posição sai marcada \"amostra pequena\".", MinPartidasRecorte, MinPartidasRecorte, AmostraPequena),
			"Inativos (parados, perdendo PDL) ficam de fora antes de tudo.",
			"Empate: notas iguais com 2 casas; desempata por mais partidas no recorte, depois na temporada, depois PDL. A posição diz que empatou.",
			fmt.Sprintf("Quem tem o histórico lido: os primeiros pela pré-nota \"subindo\" (Wilson da temporada + sinais − veterano) e pela \"melhor\" (posição na liga), intercalados, até o teto (padrão %d). Abaixo de Mestre a liga lida é uma amostra de %d páginas por divisão.", TetoCandidatosPadrao, PaginasPorDivisao),
			fmt.Sprintf("Wilson a 99%%: 5 vitórias em 5 valem %.3f e 60 em 100 valem %.3f (a amostra pequena não passa na frente).", Wilson(5, 5), Wilson(60, 100)),
		},
	}
}

func (c *controller) formula(w http.ResponseWriter, r *http.Request) {
	web.WriteJSON(w, http.StatusOK, FormulaCompleta())
}

// GET /api/rankings/orcamento?elo&fonte&candidatos&partidas_por_jogador
func (c *controller) orcamento(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	p := Pedido{Elo: q.Get("elo"), Fonte: q.Get("fonte")}
	p.Candidatos, _ = strconv.Atoi(q.Get("candidatos"))
	p.PartidasPorJogador, _ = strconv.Atoi(q.Get("partidas_por_jogador"))
	if _, err := p.valida(c.a.d.FontePadrao); err != nil {
		c.erro(w, err, "calcular o orçamento")
		return
	}
	web.WriteJSON(w, http.StatusOK, CalculaOrcamento(p.Elo, p.Fonte, p.Candidatos, p.PartidasPorJogador))
}

// RespostaRanking: o cabeçalho, as posições num modo e a fórmula daquele modo.
type RespostaRanking struct {
	Ranking  Ranking     `json:"ranking"`
	Modo     string      `json:"modo"`
	Posicoes []Posicao   `json:"posicoes"`
	Formula  FormulaTipo `json:"formula"`
}

func (c *controller) responde(w http.ResponseWriter, r *http.Request, rk Ranking) {
	modo, err := modoDe(r)
	if err != nil {
		c.erro(w, err, "ler o ranking")
		return
	}
	ps, err := Posicoes(c.a.d.DB, rk.ID, modo)
	if err != nil {
		c.erro(w, err, "ler o ranking")
		return
	}
	c.poeCampeao(r.Context(), &rk)
	for i := range ps {
		if id := ps[i].Numeros.CampeaoID; id > 0 {
			ps[i].Campeao = c.a.nomeCampeao(r.Context(), id)
		}
	}
	web.WriteJSON(w, http.StatusOK, RespostaRanking{Ranking: rk, Modo: modo, Posicoes: ps, Formula: Formula(rk.Tipo, modo)})
}

// GET /api/rankings/recorte?regiao&patch&elo&rota|campeao&modo: o mais recente do recorte.
func (c *controller) recorte(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	rota, camp, err := recorteDe(r)
	if err != nil {
		c.erro(w, err, "ler o ranking")
		return
	}
	rk, err := Ultimo(c.a.d.DB, regiaoDe(r), q.Get("patch"), strings.ToUpper(q.Get("elo")), rota, camp)
	if err != nil {
		c.erro(w, err, "ranking deste recorte")
		return
	}
	c.responde(w, r, rk)
}

func (c *controller) porID(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	var rk Ranking
	if err == nil {
		rk, err = LeRanking(c.a.d.DB, id)
	}
	if err != nil {
		c.erro(w, err, "ranking")
		return
	}
	c.responde(w, r, rk)
}

// GET /api/rankings/comparar?regiao&elo&rota|campeao&patch_a&patch_b&modo&topo
func (c *controller) comparar(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	rota, camp, err := recorteDe(r)
	if err != nil {
		c.erro(w, err, "comparar os rankings")
		return
	}
	modo, err := modoDe(r)
	if err != nil {
		c.erro(w, err, "comparar os rankings")
		return
	}
	topo, _ := strconv.Atoi(q.Get("topo"))
	if topo == 0 {
		topo = 10
	}
	if topo < 1 || topo > 100 {
		c.erro(w, &ErroPedido{"topo precisa ser de 1 a 100"}, "comparar os rankings")
		return
	}
	reg, elo := regiaoDe(r), strings.ToUpper(q.Get("elo"))
	a, err := Ultimo(c.a.d.DB, reg, q.Get("patch_a"), elo, rota, camp)
	if err != nil {
		c.erro(w, err, "ranking do patch_a")
		return
	}
	b, err := Ultimo(c.a.d.DB, reg, q.Get("patch_b"), elo, rota, camp)
	if err != nil {
		c.erro(w, err, "ranking do patch_b")
		return
	}
	c.poeCampeao(r.Context(), &a)
	c.poeCampeao(r.Context(), &b)
	cmp, err := Compara(r.Context(), c.a.d.DB, a, b, modo, topo, c.a.nomeCampeao)
	if err != nil {
		c.erro(w, err, "comparar os rankings")
		return
	}
	web.WriteJSON(w, http.StatusOK, cmp)
}

func (c *controller) lista(w http.ResponseWriter, r *http.Request) {
	l, err := c.a.Lista()
	if err != nil {
		c.erro(w, err, "listar as atualizações")
		return
	}
	web.WriteLista(w, "atualizacoes", l, map[string]any{"fonte_padrao": c.a.d.FontePadrao})
}

// POST /api/rankings/atualizacoes {"regiao"?, "elo", "patch"?, "fonte"?, "candidatos"?, "partidas_por_jogador"?}
func (c *controller) cria(w http.ResponseWriter, r *http.Request) {
	var p Pedido
	if err := web.LeJSON(w, r, &p); err != nil {
		web.WriteErr(w, http.StatusBadRequest, err.Error())
		return
	}
	at, exID, err := c.a.Cria(r.Context(), p)
	if err != nil {
		c.erro(w, err, "começar a atualização do ranking")
		return
	}
	web.WriteJSON(w, http.StatusAccepted, map[string]any{"atualizacao": at, "execucao": map[string]string{"id": exID},
		"orcamento": CalculaOrcamento(at.Elo, at.Fonte, at.TetoCandidatos, at.TetoPartidas)})
}

func (c *controller) le(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	var at Atualizacao
	if err == nil {
		at, err = c.a.Le(id)
	}
	if err != nil {
		c.erro(w, err, "atualização")
		return
	}
	web.WriteJSON(w, http.StatusOK, at)
}

func (c *controller) pausa(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	if err == nil {
		err = c.a.Pausa(id)
	}
	if err != nil {
		c.erro(w, err, "pausar a atualização")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (c *controller) retoma(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	var exID string
	if err == nil {
		exID, err = c.a.Retoma(r.Context(), id)
	}
	if err != nil {
		c.erro(w, err, "retomar a atualização")
		return
	}
	web.WriteJSON(w, http.StatusAccepted, map[string]any{"execucao": map[string]string{"id": exID}})
}

func (c *controller) cancela(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	if err == nil {
		err = c.a.Cancela(id)
	}
	if err != nil {
		c.erro(w, err, "cancelar a atualização")
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
	exID, ok := c.a.ExecucaoDe(id)
	if !ok {
		web.WriteErr(w, http.StatusNotFound, "esta atualização não tem execução recente")
		return
	}
	c.a.d.Hub.Servir(w, r, exID)
}
