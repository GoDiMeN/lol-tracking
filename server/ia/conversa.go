package ia

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"loltracking/server/analise"
	"loltracking/server/banco"
	"loltracking/server/rastreio"
	"loltracking/server/web"
)

// AS CONVERSAS: sobre uma partida (com um jogador em foco; o pacote de
// contexto vai no prompt de sistema) ou gerais (game_id 0: o servidor escolhe
// as partidas que respondem à pergunta SÓ pelo banco, e manda uma linha por
// partida). A IA não tem ferramentas: quando precisa de dado que o texto não
// traz, responde só com linhas de pedido (PEDIDO_MOMENTO, PEDIDO_PARTIDA), o
// SERVIDOR atende pelo banco, guarda o que anexou como mensagem "dados" e
// chama de novo, no máximo MaxVoltasPedido vezes.

// MaxVoltasPedido: quantas vezes o servidor atende pedidos numa resposta.
const MaxVoltasPedido = 2

// MaxPedidosPorVolta: quantos pedidos valem numa volta.
const MaxPedidosPorVolta = 3

// Conversa é uma linha de ia_conversas.
type Conversa struct {
	ID             int64  `json:"id"`
	GameID         int64  `json:"game_id"` // 0 = conversa geral
	ParticipanteID int    `json:"participante_id"`
	Titulo         string `json:"titulo"`
	CriadaEm       string `json:"criada_em"`
	AtualizadaEm   string `json:"atualizada_em"`
	Mensagens      int    `json:"mensagens"`
}

// MensagemConversa é uma linha de ia_mensagens. Papel: usuario, assistente
// ou dados (o que o servidor anexou a pedido da IA).
type MensagemConversa struct {
	ID                int64          `json:"id"`
	ConversaID        int64          `json:"conversa_id"`
	Papel             string         `json:"papel"`
	Texto             string         `json:"texto"`
	Estado            string         `json:"estado"` // ok, rodando, falhou, cancelada
	Erro              string         `json:"erro"`
	CriadaEm          string         `json:"criada_em"`
	Modelo            string         `json:"modelo"`
	DuracaoMs         int64          `json:"duracao_ms"`
	CaracteresEntrada int            `json:"caracteres_entrada"`
	CaracteresSaida   int            `json:"caracteres_saida"`
	Partidas          []int64        `json:"partidas"`
	Dados             map[string]any `json:"dados"`
}

const colunasMensagem = `id, conversa_id, papel, texto, estado, erro, criada_em, modelo, duracao_ms, caracteres_entrada, caracteres_saida, partidas, dados`

func leMensagem(s interface{ Scan(...any) error }) (MensagemConversa, error) {
	var m MensagemConversa
	var partidas, dados string
	err := s.Scan(&m.ID, &m.ConversaID, &m.Papel, &m.Texto, &m.Estado, &m.Erro, &m.CriadaEm, &m.Modelo, &m.DuracaoMs,
		&m.CaracteresEntrada, &m.CaracteresSaida, &partidas, &dados)
	if err != nil {
		return m, err
	}
	_ = json.Unmarshal([]byte(partidas), &m.Partidas)
	_ = json.Unmarshal([]byte(dados), &m.Dados)
	m.Partidas = web.NuncaNil(m.Partidas)
	if m.Dados == nil {
		m.Dados = map[string]any{}
	}
	return m, nil
}

// ErrConversaNaoEncontrada: 404.
var ErrConversaNaoEncontrada = errors.New("conversa não encontrada")

// CriaConversa abre uma conversa (game_id 0 = geral; senão com o jogador em foco).
func (s *Servico) CriaConversa(ctx context.Context, gameID int64, pid int) (Conversa, error) {
	if gameID != 0 {
		info, err := s.Fontes.Partida(ctx, gameID)
		if err != nil {
			return Conversa{}, err
		}
		if _, ok := info.Do(pid); !ok {
			return Conversa{}, &ErrPedido{fmt.Sprintf("o participante %d não está na partida", pid)}
		}
	} else {
		pid = 0
	}
	agora := banco.Agora()
	res, err := s.DB.Exec(`INSERT INTO ia_conversas (game_id, participante_id, criada_em, atualizada_em) VALUES (?, ?, ?, ?)`, gameID, pid, agora, agora)
	if err != nil {
		return Conversa{}, err
	}
	id, _ := res.LastInsertId()
	return LeConversa(s.DB, id)
}

// LeConversa lê uma conversa.
func LeConversa(db *banco.DB, id int64) (Conversa, error) {
	var c Conversa
	err := db.QueryRow(`SELECT c.id, c.game_id, c.participante_id, c.titulo, c.criada_em, c.atualizada_em,
		(SELECT COUNT(*) FROM ia_mensagens m WHERE m.conversa_id = c.id AND m.papel <> 'dados')
		FROM ia_conversas c WHERE c.id = ?`, id).Scan(&c.ID, &c.GameID, &c.ParticipanteID, &c.Titulo, &c.CriadaEm, &c.AtualizadaEm, &c.Mensagens)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrConversaNaoEncontrada
	}
	return c, err
}

// ListaConversas: as de uma partida (e jogador; 0 = qualquer) ou as gerais (gameID 0).
func ListaConversas(db *banco.DB, gameID int64, pid int) ([]Conversa, error) {
	q := `SELECT c.id, c.game_id, c.participante_id, c.titulo, c.criada_em, c.atualizada_em,
		(SELECT COUNT(*) FROM ia_mensagens m WHERE m.conversa_id = c.id AND m.papel <> 'dados')
		FROM ia_conversas c WHERE c.game_id = ?`
	args := []any{gameID}
	if pid > 0 {
		q += ` AND c.participante_id = ?`
		args = append(args, pid)
	}
	linhas, err := db.Query(q+` ORDER BY c.atualizada_em DESC, c.id DESC LIMIT 50`, args...)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	var r []Conversa
	for linhas.Next() {
		var c Conversa
		if err := linhas.Scan(&c.ID, &c.GameID, &c.ParticipanteID, &c.Titulo, &c.CriadaEm, &c.AtualizadaEm, &c.Mensagens); err != nil {
			return nil, err
		}
		r = append(r, c)
	}
	return r, linhas.Err()
}

// ListaMensagens: todas as mensagens de uma conversa, na ordem.
func ListaMensagens(db *banco.DB, conversaID int64) ([]MensagemConversa, error) {
	linhas, err := db.Query(`SELECT `+colunasMensagem+` FROM ia_mensagens WHERE conversa_id = ? ORDER BY id`, conversaID)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	var r []MensagemConversa
	for linhas.Next() {
		m, err := leMensagem(linhas)
		if err != nil {
			return nil, err
		}
		r = append(r, m)
	}
	return r, linhas.Err()
}

func leUmaMensagem(db *banco.DB, id int64) (MensagemConversa, error) {
	return leMensagem(db.QueryRow(`SELECT `+colunasMensagem+` FROM ia_mensagens WHERE id = ?`, id))
}

// ApagaConversa apaga a conversa e as mensagens.
func (s *Servico) ApagaConversa(id int64) error {
	if s.execucaoDe(fmt.Sprintf("c:%d", id)) != nil {
		return &ErrRodando{Msg: "a conversa está esperando uma resposta; cancele antes de apagar", Execucao: s.execucaoDe(fmt.Sprintf("c:%d", id))}
	}
	if _, err := LeConversa(s.DB, id); err != nil {
		return err
	}
	if _, err := s.DB.Exec(`DELETE FROM ia_mensagens WHERE conversa_id = ?`, id); err != nil {
		return err
	}
	_, err := s.DB.Exec(`DELETE FROM ia_conversas WHERE id = ?`, id)
	return err
}

// ExecucaoDaConversa: a execução da resposta em andamento (nil se não há).
func (s *Servico) ExecucaoDaConversa(id int64) *web.Execucao {
	return s.execucaoDe(fmt.Sprintf("c:%d", id))
}

func (s *Servico) gravaMensagem(conversaID int64, papel, texto, estado string, partidas []int64, dados map[string]any) (int64, error) {
	pj, _ := json.Marshal(web.NuncaNil(partidas))
	if dados == nil {
		dados = map[string]any{}
	}
	dj, _ := json.Marshal(dados)
	agora := banco.Agora()
	res, err := s.DB.Exec(`INSERT INTO ia_mensagens (conversa_id, papel, texto, estado, criada_em, partidas, dados) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		conversaID, papel, texto, estado, agora, string(pj), string(dj))
	if err != nil {
		return 0, err
	}
	_, _ = s.DB.Exec(`UPDATE ia_conversas SET atualizada_em = ? WHERE id = ?`, agora, conversaID)
	return res.LastInsertId()
}

// Envia guarda a pergunta e começa a resposta (execução com SSE). `puuid`
// (opcional) é o jogador buscado no app: na conversa geral, "eu"/"minhas" na
// pergunta filtra as partidas dele.
func (s *Servico) Envia(ctx context.Context, conversaID int64, texto, puuid string) (MensagemConversa, MensagemConversa, *web.Execucao, error) {
	texto = strings.TrimSpace(texto)
	if texto == "" {
		return MensagemConversa{}, MensagemConversa{}, nil, &ErrPedido{"escreva a pergunta"}
	}
	if utf8.RuneCountInString(texto) > 4000 {
		return MensagemConversa{}, MensagemConversa{}, nil, &ErrPedido{"a pergunta passa de 4000 caracteres"}
	}
	c, err := LeConversa(s.DB, conversaID)
	if err != nil {
		return MensagemConversa{}, MensagemConversa{}, nil, err
	}
	est := s.IA.Estado(ctx)
	if !est.Disponivel {
		return MensagemConversa{}, MensagemConversa{}, nil, &erroIA{est.Motivo, ErrIndisponivel}
	}
	e := s.Hub.Nova()
	chave := fmt.Sprintf("c:%d", conversaID)
	if x := s.ocupa(chave, e); x != nil {
		e.Fecha()
		return MensagemConversa{}, MensagemConversa{}, nil, &ErrRodando{"a conversa ainda está esperando a resposta anterior", x}
	}
	if c.Titulo == "" {
		t := []rune(texto)
		if len(t) > 70 {
			t = append(t[:70], '…')
		}
		_, _ = s.DB.Exec(`UPDATE ia_conversas SET titulo = ? WHERE id = ?`, string(t), conversaID)
	}
	idU, err := s.gravaMensagem(conversaID, PapelUsuario, texto, "ok", nil, nil)
	if err != nil {
		s.libera(chave, e)
		e.Fecha()
		return MensagemConversa{}, MensagemConversa{}, nil, err
	}
	ctxTrab, cancela := context.WithCancel(context.Background())
	e.AoCancelar(cancela)
	go func() {
		defer cancela()
		defer s.libera(chave, e)
		defer e.Fecha()
		s.rodaConversa(ctxTrab, e, c, texto, puuid)
	}()
	u, _ := leUmaMensagem(s.DB, idU)
	// A resposta só entra no banco no fim (depois dos dados anexados, na
	// ordem da conversa); até lá é a execução.
	a := MensagemConversa{ConversaID: conversaID, Papel: PapelAssistente, Estado: "rodando", Partidas: []int64{}, Dados: map[string]any{}}
	return u, a, e, nil
}

var (
	rePedidoMomento = regexp.MustCompile(`(?m)^\s*PEDIDO_MOMENTO\s+(?:partida=(\d+)\s+)?t=(\d{1,2}:\d{2})\s*$`)
	rePedidoPartida = regexp.MustCompile(`(?m)^\s*PEDIDO_PARTIDA\s+partida=(\d+)\s+participante=(\d{1,2})\s*$`)
	reCitaPartida   = regexp.MustCompile(`partida (\d{6,})`)
	reProxima       = regexp.MustCompile(`(?m)^\s*\**PR[ÓO]XIMA PERGUNTA:?\**:?\s*(.+?)\s*$`)
)

// textoMaterial: a seção "Material das vozes" (as sem material marcadas).
func textoMaterial(vozes []MaterialVoz) string {
	var b strings.Builder
	var com []string
	for _, v := range vozes {
		if v.TemMaterial() {
			com = append(com, v.ID)
		}
	}
	if len(com) == 0 {
		b.WriteString("## Material das vozes\nVOZES COM MATERIAL: nenhuma. Nenhuma voz (profissional, especialista de solo queue, mono, melhores do elo, Challenger, Esmeralda recém-promovido, Especialista Lane) tem dados de referência no app: diga isso se perguntarem por elas, sem inventar.\n")
		return b.String()
	}
	fmt.Fprintf(&b, "## Material das vozes\nVOZES COM MATERIAL: %s\n", strings.Join(com, ", "))
	for _, v := range vozes {
		if v.TemMaterial() {
			fmt.Fprintf(&b, "\n### Voz %s (%s): MATERIAL\n%s\n", v.ID, v.Rotulo, v.Texto)
		} else {
			fmt.Fprintf(&b, "\n### Voz %s (%s): SEM MATERIAL\n", v.ID, v.Rotulo)
		}
	}
	return b.String()
}

// PedidoDado é um pedido da IA que o servidor atende.
type PedidoDado struct {
	Tipo           string `json:"tipo"` // momento, partida
	GameID         int64  `json:"game_id"`
	TMs            int64  `json:"t_ms"`
	ParticipanteID int    `json:"participante_id"`
}

// LePedidos acha os pedidos de dado numa resposta (no máximo MaxPedidosPorVolta).
func LePedidos(texto string, gameID int64) []PedidoDado {
	var r []PedidoDado
	for _, m := range rePedidoMomento.FindAllStringSubmatch(texto, -1) {
		g := gameID
		if m[1] != "" {
			g, _ = strconv.ParseInt(m[1], 10, 64)
		}
		if ms, ok := msDeTexto(m[2]); ok && g != 0 {
			r = append(r, PedidoDado{Tipo: "momento", GameID: g, TMs: ms})
		}
	}
	for _, m := range rePedidoPartida.FindAllStringSubmatch(texto, -1) {
		g, _ := strconv.ParseInt(m[1], 10, 64)
		pid, _ := strconv.Atoi(m[2])
		r = append(r, PedidoDado{Tipo: "partida", GameID: g, ParticipanteID: pid})
	}
	if len(r) > MaxPedidosPorVolta {
		r = r[:MaxPedidosPorVolta]
	}
	return r
}

// textoParaIA: como uma mensagem guardada vai para a IA.
func textoParaIA(m MensagemConversa) Mensagem {
	if m.Papel == "dados" {
		return Mensagem{Papel: PapelUsuario, Texto: "[o app anexou, a seu pedido]\n" + m.Texto}
	}
	return Mensagem{Papel: m.Papel, Texto: m.Texto}
}

func (s *Servico) rodaConversa(ctx context.Context, e *web.Execucao, c Conversa, pergunta, puuid string) {
	var idA int64
	falha := func(estado, msg string) {
		if idA == 0 {
			idA, _ = s.gravaMensagem(c.ID, PapelAssistente, "", estado, nil, nil)
		}
		_, _ = s.DB.Exec(`UPDATE ia_mensagens SET estado = ?, erro = ? WHERE id = ?`, estado, msg, idA)
		if estado == "cancelada" {
			e.Emite("cancelado", map[string]any{"conversa_id": c.ID})
			return
		}
		e.Falha(msg)
	}
	// O prompt de sistema: o contexto da partida, ou as partidas escolhidas.
	var sistema string
	conhecidas := map[int64]bool{}
	dados := map[string]any{}
	if c.GameID != 0 {
		ctxTexto, err := s.Fontes.ContextoPartida(ctx, c.GameID, c.ParticipanteID)
		if err != nil {
			falha("falhou", "não deu para montar o texto da partida: "+err.Error())
			return
		}
		info, err := s.Fontes.Partida(ctx, c.GameID)
		if err == nil {
			ctxTexto = listaParticipantes(info) + "\n" + ctxTexto
			// O material das vozes: as que têm podem responder à consulta.
			if foco, ok := info.Do(c.ParticipanteID); ok {
				ctxTexto += "\n\n" + textoMaterial(s.materialVozes(info, foco, s.EloDeComparacao(ctx, info, foco, false)))
			}
		}
		sistema = SistemaConversaPartida(ctxTexto)
		conhecidas[c.GameID] = true
		e.Emite("contexto", map[string]any{"enviado": "pacote de contexto da partida " + strconv.FormatInt(c.GameID, 10) + " com o jogador em foco, a lista de participantes, o histórico desta conversa e a pergunta",
			"caracteres_sistema": utf8.RuneCountInString(sistema)})
	} else {
		campeoes, _ := s.Fontes.Campeoes()
		f, descr := FiltroDaPergunta(pergunta, campeoes, puuid)
		texto, ids, usado, err := s.escolhePartidas(f, descr)
		if err != nil {
			falha("falhou", "não deu para escolher as partidas: "+err.Error())
			return
		}
		for _, id := range ids {
			conhecidas[id] = true
		}
		sistema = SistemaConversaGeral(texto, usado)
		dados["filtro"] = usado
		dados["partidas_escolhidas"] = ids
		e.Emite("partidas", map[string]any{"filtro": usado, "partidas": web.NuncaNil(ids)})
		e.Emite("contexto", map[string]any{"enviado": "uma linha por partida escolhida (só a base de fatos), o histórico desta conversa e a pergunta",
			"caracteres_sistema": utf8.RuneCountInString(sistema)})
	}
	var total Resposta
	for volta := 0; ; volta++ {
		hist, err := ListaMensagens(s.DB, c.ID)
		if err != nil {
			falha("falhou", "não deu para ler a conversa")
			return
		}
		var ms []Mensagem
		for _, m := range hist {
			if m.Papel == PapelAssistente && m.Estado != "ok" {
				continue
			}
			ms = append(ms, textoParaIA(m))
		}
		if len(ms) > 24 {
			ms = ms[len(ms)-24:]
		}
		if s.IA.Ocupada() {
			e.Emite("na_fila", map[string]any{"texto": "outra chamada à IA está rodando; esta espera a vez"})
		}
		e.Emite("inicio", map[string]any{"volta": volta})
		res, err := s.IA.Conversa(ctx, Pedido{Tipo: TipoConversa, Sistema: sistema, Mensagens: ms}, func(t string) {
			e.Emite("trecho", map[string]any{"texto": t})
		})
		total.Modelo = res.Modelo
		total.DuracaoMs += res.DuracaoMs
		total.CaracteresEntrada += res.CaracteresEntrada
		total.CaracteresSaida += res.CaracteresSaida
		if err != nil {
			log.Printf("ia: conversa %d: %v (%v)", c.ID, err, errors.Unwrap(err))
			if ctx.Err() != nil {
				falha("cancelada", "cancelada")
			} else {
				falha("falhou", err.Error())
			}
			return
		}
		gid := c.GameID
		pedidos := LePedidos(res.Texto, gid)
		if len(pedidos) > 0 && volta < MaxVoltasPedido {
			for _, p := range pedidos {
				e.Emite("pedido", p)
				txt, err := s.atendePedido(ctx, p, c)
				if err != nil {
					txt = "Não deu para atender o pedido: " + err.Error()
				}
				conhecidas[p.GameID] = conhecidas[p.GameID] || err == nil
				if _, err := s.gravaMensagem(c.ID, "dados", txt, "ok", []int64{p.GameID}, map[string]any{"pedido": p}); err != nil {
					falha("falhou", "não deu para guardar o dado anexado")
					return
				}
			}
			e.Emite("recomeca", map[string]any{"volta": volta + 1})
			continue
		}
		final := res.Texto
		if len(pedidos) > 0 {
			// Pediu de novo depois do limite: tira as linhas de pedido.
			final = strings.TrimSpace(rePedidoPartida.ReplaceAllString(rePedidoMomento.ReplaceAllString(final, ""), ""))
			if final == "" {
				final = "A IA pediu mais dados do que o limite de voltas permite. Refaça a pergunta mais direta."
			}
		}
		// A próxima pergunta proposta pela IA vira um botão (sai do texto).
		if m := reProxima.FindStringSubmatchIndex(final); m != nil {
			dados["proxima_pergunta"] = strings.TrimSpace(final[m[2]:m[3]])
			final = strings.TrimSpace(final[:m[0]] + final[m[1]:])
		}
		var citadas []int64
		visto := map[int64]bool{}
		for _, m := range reCitaPartida.FindAllStringSubmatch(final, -1) {
			id, _ := strconv.ParseInt(m[1], 10, 64)
			if !visto[id] && (conhecidas[id] || s.existePartida(id)) && (c.GameID != 0 || s.dePesquisa(id)) {
				visto[id] = true
				citadas = append(citadas, id)
			}
		}
		pj, _ := json.Marshal(web.NuncaNil(citadas))
		dj, _ := json.Marshal(dados)
		idA, err = s.gravaMensagem(c.ID, PapelAssistente, "", "rodando", nil, nil)
		if err != nil {
			falha("falhou", "não deu para guardar a resposta")
			return
		}
		if _, err := s.DB.Exec(`UPDATE ia_mensagens SET texto = ?, estado = 'ok', modelo = ?, duracao_ms = ?, caracteres_entrada = ?,
			caracteres_saida = ?, partidas = ?, dados = ? WHERE id = ?`, final, total.Modelo, total.DuracaoMs, total.CaracteresEntrada,
			total.CaracteresSaida, string(pj), string(dj), idA); err != nil {
			falha("falhou", "não deu para guardar a resposta")
			return
		}
		_, _ = s.DB.Exec(`UPDATE ia_conversas SET atualizada_em = ? WHERE id = ?`, banco.Agora(), c.ID)
		m, _ := leUmaMensagem(s.DB, idA)
		e.Emite("concluido", map[string]any{"mensagem": m})
		return
	}
}

func (s *Servico) existePartida(id int64) bool {
	var n int
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM resumos_partida WHERE game_id = ?`, id).Scan(&n)
	return n > 0
}

// dePesquisa: a partida veio da busca de um jogador (rastreio/origem.go).
func (s *Servico) dePesquisa(id int64) bool {
	var n int
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM partidas_origem WHERE game_id = ? AND origem = ?`, id, rastreio.OrigemPesquisa).Scan(&n)
	return n > 0
}

// atendePedido busca o dado pedido pela IA (só banco e arquivos da partida).
func (s *Servico) atendePedido(ctx context.Context, p PedidoDado, c Conversa) (string, error) {
	if c.GameID != 0 && p.GameID != c.GameID {
		return "", fmt.Errorf("esta conversa é só da partida %d", c.GameID)
	}
	// Na conversa geral, só partida do usuário: a IA não puxa partida de
	// estudo pelo número como se fosse dele.
	if c.GameID == 0 && !s.dePesquisa(p.GameID) {
		return "", fmt.Errorf("a partida %d não está entre as partidas rastreadas do usuário", p.GameID)
	}
	switch p.Tipo {
	case "momento":
		t, err := s.Fontes.Momento(ctx, p.GameID, p.TMs)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("DADOS DO INSTANTE %s da partida %d:\n%s", analise.MinSeg(p.TMs), p.GameID, t), nil
	case "partida":
		info, err := s.Fontes.Partida(ctx, p.GameID)
		if err != nil {
			return "", err
		}
		if _, ok := info.Do(p.ParticipanteID); !ok {
			return "", fmt.Errorf("o participante %d não está na partida %d", p.ParticipanteID, p.GameID)
		}
		t, err := s.Fontes.ContextoPartida(ctx, p.GameID, p.ParticipanteID)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("PACOTE DA PARTIDA %d (foco no participante %d):\n%s\n%s", p.GameID, p.ParticipanteID, listaParticipantes(info), t), nil
	}
	return "", fmt.Errorf("pedido desconhecido")
}

// escolhePartidas tenta o filtro inteiro e vai afrouxando até achar alguma
// partida (só o banco). Devolve o texto, os ids e a descrição do filtro usado.
func (s *Servico) escolhePartidas(f analise.FiltroResumos, descr []string) (string, []int64, string, error) {
	f.Limite = 30
	// Só as partidas do usuário (vindas da busca de um jogador): as de estudo
	// entram na conversa só como referência das vozes, nunca como "suas".
	f.SoPesquisa = true
	tentativas := []func(analise.FiltroResumos) (analise.FiltroResumos, string){
		func(x analise.FiltroResumos) (analise.FiltroResumos, string) { return x, "" },
		func(x analise.FiltroResumos) (analise.FiltroResumos, string) {
			x.Tipo, x.Tag, x.AteMs, x.Texto = "", "", 0, ""
			return x, "sem o filtro de momento"
		},
		func(x analise.FiltroResumos) (analise.FiltroResumos, string) {
			x.Tipo, x.Tag, x.AteMs, x.Texto, x.Resultado, x.Funcao, x.Elo = "", "", 0, "", "", "", ""
			return x, "só campeão/jogador"
		},
		func(x analise.FiltroResumos) (analise.FiltroResumos, string) {
			return analise.FiltroResumos{Limite: 30, SoPesquisa: true}, "as partidas mais recentes (nada casou com a pergunta)"
		},
	}
	for i, tf := range tentativas {
		x, nota := tf(f)
		texto, ids, err := s.Fontes.VariasPartidas(x)
		if err != nil {
			return "", nil, "", err
		}
		if len(ids) > 0 || i == len(tentativas)-1 {
			usado := strings.Join(descr, ", ")
			if usado == "" {
				usado = "nenhum (a pergunta não citou campeão, função, resultado nem momento)"
			}
			if nota != "" {
				usado += "; afrouxado: " + nota
			}
			return texto, ids, usado, nil
		}
	}
	return "", nil, "", nil
}

// FiltroDaPergunta lê a pergunta da conversa geral e monta o filtro da busca
// (determinístico, por palavras). Devolve também a descrição do que achou.
func FiltroDaPergunta(texto string, campeoes []string, puuid string) (analise.FiltroResumos, []string) {
	var f analise.FiltroResumos
	var d []string
	baixo := " " + strings.ToLower(texto) + " "
	palavra := func(ps ...string) bool {
		for _, p := range ps {
			if regexp.MustCompile(`[^\p{L}\p{N}]` + regexp.QuoteMeta(p) + `[^\p{L}\p{N}]`).MatchString(baixo) {
				return true
			}
		}
		return false
	}
	for _, c := range campeoes {
		if palavra(strings.ToLower(c)) {
			f.Campeao = c
			d = append(d, "campeão "+c)
			break
		}
	}
	funcoes := []struct {
		cod string
		ps  []string
	}{
		{"JUNGLE", []string{"jungle", "caçador", "caçadora", "jg", "jungler"}},
		{"TOP", []string{"top", "topo"}},
		{"MIDDLE", []string{"mid", "meio"}},
		{"BOTTOM", []string{"adc", "atirador", "atiradora", "bot"}},
		{"UTILITY", []string{"suporte", "sup", "support"}},
	}
	for _, x := range funcoes {
		if palavra(x.ps...) {
			f.Funcao = x.cod
			d = append(d, "função "+nomeFuncaoCurto(x.cod))
			break
		}
	}
	v := palavra("vitória", "vitórias", "vitoria", "vitorias", "ganhei", "ganhou", "venci", "venceu", "ganho", "ganha", "ganhar")
	p := palavra("derrota", "derrotas", "perdi", "perdeu", "perco", "perde", "perder")
	if v != p {
		if v {
			f.Resultado = "vitoria"
			d = append(d, "vitórias")
		} else {
			f.Resultado = "derrota"
			d = append(d, "derrotas")
		}
	}
	momentos := []struct {
		tipo string
		ps   []string
	}{
		{"abate", []string{"morte", "mortes", "morri", "morro", "morreu", "morre", "abate", "abates", "morrer"}},
		{"objetivo", []string{"dragão", "dragões", "dragao", "barão", "barao", "arauto", "objetivo", "objetivos", "atakhan", "vastilarvas"}},
		{"torre", []string{"torre", "torres"}},
		{"freeze", []string{"freeze", "congelar", "congelamento"}},
		{"luta", []string{"luta", "lutas", "teamfight", "teamfights"}},
		{"janela_perdida", []string{"janela", "janelas", "invasão", "invadir", "roaming", "roam"}},
	}
	for _, x := range momentos {
		if palavra(x.ps...) {
			f.Tipo = x.tipo
			d = append(d, "momentos do tipo "+x.tipo)
			break
		}
	}
	if m := regexp.MustCompile(`(?:antes d[oa]s?|até (?:os|aos)?)\s*(\d{1,2})\s*(?:min|minutos)`).FindStringSubmatch(baixo); m != nil {
		n, _ := strconv.Atoi(m[1])
		if f.Tipo == "" {
			f.Tipo = "abate"
			d = append(d, "momentos do tipo abate")
		}
		f.AteMs = int64(n) * 60000
		d = append(d, fmt.Sprintf("até os %d min", n))
	}
	elos := []struct {
		cod string
		ps  []string
	}{
		{"CHALLENGER", []string{"challenger", "desafiante"}},
		{"GRANDMASTER", []string{"grão-mestre", "grao-mestre", "grandmaster"}},
		{"MASTER", []string{"mestre", "master"}},
		{"DIAMOND", []string{"diamante", "diamond"}},
		{"EMERALD", []string{"esmeralda", "emerald"}},
		{"PLATINUM", []string{"platina", "platinum"}},
		{"SILVER", []string{"prata", "silver"}},
		{"BRONZE", []string{"bronze"}},
		{"IRON", []string{"ferro", "iron"}},
	}
	for _, x := range elos {
		if palavra(x.ps...) {
			f.Elo = x.cod
			d = append(d, "elo "+x.cod)
			break
		}
	}
	if puuid != "" && palavra("eu", "meu", "minha", "meus", "minhas", "mim", "morri", "morro", "ganhei", "perdi", "venci", "perco") {
		f.Puuid = puuid
		d = append(d, "as partidas do jogador buscado no app")
	}
	return f, d
}
