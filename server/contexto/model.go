// Package contexto busca, a pedido, o CONTEXTO DOS 10 JOGADORES de uma
// partida: o histórico de cada um de ANTES dela (função principal, campeão,
// sequência, vitórias recentes) e o elo da fila solo. "Às vezes quem ganha o
// game é o suporte que é do meio, que jogava muito e vinha de uma sequência de
// vitórias": o placar não diz isso, o histórico diz.
//
// Fontes (as do ranking, com o mesmo cache de partidas; ranking/antes.go):
//   - cliente: o histórico pelo cliente do LoL (a rota já PERMITIDA do
//     histórico, lcu/permitidas.go não muda), pelo puuid do LCU de cada um.
//     Vê só as ranking.FimHistoricoLCU partidas mais recentes de cada jogador;
//     partida velha pode ficar fora do alcance, e isso é dito, nunca trocado
//     por partidas posteriores;
//   - riot: a match-v5 (sem cliente: o servidor), 1 pedido para os puuids da
//     Riot dos 10 (a partida), e por jogador 1 de ids com endTime + até
//     TetoPartidasRiotPorJogador partidas.
//
// O resultado vai para a base de fatos (analise.GravaHistoricoExterno, tabela
// contexto_historicos) com a data, e a base de fatos da partida é refeita. É
// uma execução por partida, nunca um lote: só o botão da tela da partida e a
// análise com IA (quando falta o histórico de algum dos 10) chamam.
package contexto

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"loltracking/server/analise"
	"loltracking/server/banco"
	"loltracking/server/ia"
	"loltracking/server/lcu"
	"loltracking/server/partida"
	"loltracking/server/ranking"
	"loltracking/server/riot"
	"loltracking/server/web"
)

// Fontes.
const (
	FonteCliente = "cliente"
	FonteRiot    = "riot"
)

// Limites nomeados de uma busca (uma partida, 10 jogadores).
const (
	// PartidasPorJogador: as partidas de antes que se juntam de cada um (o
	// contexto usa as até analise.Param.PartidasContexto mais recentes).
	PartidasPorJogador = 10
	// MargemIDsRiot: ids a mais pedidos à match-v5 (ARAM, remakes e outras
	// filas que não contam).
	MargemIDsRiot = 5
	// TetoPartidasRiotPorJogador: pedidos de partida à match-v5 por jogador
	// (as do cache não contam).
	TetoPartidasRiotPorJogador = 15
	// TetoPedidosRiot: o teto de pedidos à Riot da busca inteira (match-v5 +
	// elo). 1 + 10 × (1 + 15) + 10 × 2 = 181 no pior caso; passou disso, os que
	// faltam ficam com erro dizendo o teto.
	TetoPedidosRiot = 200
	// PrazoHistoricoLCU: o prazo de cada histórico no cliente (o de outro
	// jogador já passou de 20 s).
	PrazoHistoricoLCU = 30 * time.Second
	// PrazoFonte: a conferência de que a fonte responde (cliente aberto, chave válida).
	PrazoFonte = 4 * time.Second
)

// FonteRiotAPI: a match-v5 com endTime e o elo por Riot ID. De verdade:
// *riot.Cliente; nos testes e no modo teste: aprendizado.RiotFalso.
type FonteRiotAPI interface {
	ranking.FonteRiotAntes
	ia.FonteEloRiot
}

// Deps do serviço.
type Deps struct {
	DB *banco.DB
	// LCU nil = sem cliente; Riot nil = sem a API da Riot.
	LCU     lcu.Cliente
	Riot    FonteRiotAPI
	Hub     *web.Hub
	Analise analise.Deps
}

// Servico guarda a execução de cada partida (uma por vez por partida).
type Servico struct {
	d       Deps
	mu      sync.Mutex
	rodando map[int64]*web.Execucao
}

// Novo cria o serviço.
func Novo(d Deps) *Servico {
	return &Servico{d: d, rodando: map[int64]*web.Execucao{}}
}

// ErrPedido: pedido que não dá para atender (409 / 404 no controller).
type ErrPedido struct {
	Texto    string
	Status   int
	Execucao *web.Execucao
}

func (e *ErrPedido) Error() string { return e.Texto }

// Fontes: o que dá para usar agora.
type Fontes struct {
	Cliente       bool   `json:"cliente"`
	Riot          bool   `json:"riot"`
	MotivoCliente string `json:"motivo_cliente"`
	MotivoRiot    string `json:"motivo_riot"`
}

// ConfereFontes pergunta ao cliente (o invocador logado, rota permitida) e à
// chave da Riot (conferida no máximo a cada 10 min) se dá para usar cada um.
func (s *Servico) ConfereFontes(ctx context.Context) Fontes {
	var f Fontes
	if s.d.LCU == nil {
		f.MotivoCliente = "sem o cliente do LoL neste servidor"
	} else {
		c2, cancela := context.WithTimeout(ctx, PrazoFonte)
		_, err := s.d.LCU.InvocadorAtual(c2)
		cancela()
		switch {
		case err == nil:
			f.Cliente = true
		case errors.Is(err, lcu.ErrClienteFechado):
			f.MotivoCliente = "o cliente do LoL está fechado"
		default:
			f.MotivoCliente = "o cliente do LoL não respondeu: " + err.Error()
		}
	}
	if s.d.Riot == nil {
		f.MotivoRiot = "sem a API da Riot neste servidor"
	} else {
		c2, cancela := context.WithTimeout(ctx, PrazoFonte)
		err := s.d.Riot.Confere(c2)
		cancela()
		switch {
		case err == nil:
			f.Riot = true
		case errors.Is(err, riot.ErrSemChave):
			f.MotivoRiot = "sem chave da Riot (token.txt)"
		case errors.Is(err, riot.ErrChaveRecusada):
			f.MotivoRiot = "a Riot recusou a chave (a de desenvolvimento vence em 24 h): troque o token.txt"
		default:
			f.MotivoRiot = err.Error()
		}
	}
	return f
}

// escolheFonte: a pedida, ou (vazia) o cliente se aberto, senão a Riot.
func escolheFonte(f Fontes, pedida string) (string, error) {
	switch pedida {
	case FonteCliente:
		if !f.Cliente {
			return "", &ErrPedido{Texto: f.MotivoCliente, Status: 409}
		}
		return FonteCliente, nil
	case FonteRiot:
		if !f.Riot {
			return "", &ErrPedido{Texto: f.MotivoRiot, Status: 409}
		}
		return FonteRiot, nil
	case "":
		if f.Cliente {
			return FonteCliente, nil
		}
		if f.Riot {
			return FonteRiot, nil
		}
		return "", &ErrPedido{Texto: "nenhuma fonte de histórico agora: " + f.MotivoCliente + "; " + f.MotivoRiot, Status: 409}
	}
	return "", &ErrPedido{Texto: `fonte inválida: use "cliente" ou "riot" (ou nada)`, Status: 400}
}

// Estado é a resposta do GET da tela.
type Estado struct {
	GameID    int64                      `json:"game_id"`
	Jogadores []analise.HistoricoExterno `json:"jogadores"`
	Faltam    []int                      `json:"faltam"`
	Fontes    Fontes                     `json:"fontes"`
	// Contextos: o contexto de cada um calculado agora (banco + buscado), por participante.
	Contextos map[int]analise.ContextoJogador `json:"contextos"`
	Execucao  *web.Execucao                   `json:"-"`
}

// LeEstado: os históricos já buscados da partida e quem falta.
func (s *Servico) LeEstado(ctx context.Context, gameID int64) (Estado, error) {
	pt, err := partida.Le(s.d.DB, gameID)
	if err != nil {
		return Estado{}, err
	}
	hs, err := analise.LeHistoricosExternos(s.d.DB, gameID)
	if err != nil {
		return Estado{}, err
	}
	e := Estado{GameID: gameID, Jogadores: []analise.HistoricoExterno{}, Faltam: []int{}, Fontes: s.ConfereFontes(ctx)}
	for _, p := range pt.Participantes {
		if h, ok := hs[p.ParticipanteID]; ok {
			e.Jogadores = append(e.Jogadores, h)
		} else {
			e.Faltam = append(e.Faltam, p.ParticipanteID)
		}
	}
	if e.Contextos, err = analise.ContextoDosJogadores(s.d.DB, gameID); err != nil {
		return Estado{}, err
	}
	s.mu.Lock()
	if x, ok := s.rodando[gameID]; ok && !x.Fechada() {
		e.Execucao = x
	}
	s.mu.Unlock()
	return e, nil
}

// Faltam: os participantes da partida sem histórico buscado (só banco).
func Faltam(db *banco.DB, gameID int64) ([]int, error) {
	pt, err := partida.Le(db, gameID)
	if err != nil {
		return nil, err
	}
	hs, err := analise.LeHistoricosExternos(db, gameID)
	if err != nil {
		return nil, err
	}
	var l []int
	for _, p := range pt.Participantes {
		if _, ok := hs[p.ParticipanteID]; !ok {
			l = append(l, p.ParticipanteID)
		}
	}
	return l, nil
}

func (s *Servico) ocupa(gameID int64, e *web.Execucao) *web.Execucao {
	s.mu.Lock()
	defer s.mu.Unlock()
	if x, ok := s.rodando[gameID]; ok && !x.Fechada() {
		return x
	}
	s.rodando[gameID] = e
	return nil
}

func (s *Servico) libera(gameID int64, e *web.Execucao) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rodando[gameID] == e {
		delete(s.rodando, gameID)
	}
}

// Inicia a busca da partida como execução (a rota). A fonte é conferida antes
// do 202: fonte indisponível é 409 na hora.
func (s *Servico) Inicia(ctx context.Context, gameID int64, pedida string) (*web.Execucao, string, error) {
	if _, err := partida.Le(s.d.DB, gameID); err != nil {
		return nil, "", err
	}
	fonte, err := escolheFonte(s.ConfereFontes(ctx), pedida)
	if err != nil {
		return nil, "", err
	}
	e := s.d.Hub.Nova()
	if x := s.ocupa(gameID, e); x != nil {
		e.Fecha()
		return nil, "", &ErrPedido{Texto: "a busca do contexto dos jogadores desta partida já está rodando", Status: 409, Execucao: x}
	}
	ctxTrab, cancela := context.WithCancel(context.Background())
	e.AoCancelar(cancela)
	go func() {
		defer cancela()
		defer s.libera(gameID, e)
		defer e.Fecha()
		r, err := s.Busca(ctxTrab, gameID, fonte, e.Emite)
		switch {
		case ctxTrab.Err() != nil:
			e.Emite("cancelado", map[string]any{"game_id": gameID})
		case err != nil:
			e.Falha(err.Error())
		default:
			e.Emite("concluido", r)
		}
	}()
	return e, fonte, nil
}

// GaranteParaIA: a análise com IA chama antes de montar o pedido. Se falta o
// histórico de algum dos 10 e há fonte, busca (na execução da IA, com os
// eventos prefixados); sem fonte ou com a busca manual rodando, só avisa. Devolve
// se buscou (a IA relê a partida).
func (s *Servico) GaranteParaIA(ctx context.Context, gameID int64, emite func(tipo string, dados any)) bool {
	faltam, err := Faltam(s.d.DB, gameID)
	if err != nil || len(faltam) == 0 {
		return false
	}
	aviso := func(t string) { emite("contexto_jogadores", map[string]any{"tipo": "aviso", "texto": t}) }
	fonte, err := escolheFonte(s.ConfereFontes(ctx), "")
	if err != nil {
		aviso("contexto dos jogadores não buscado (" + err.Error() + "): a análise segue com o que o banco tem")
		return false
	}
	e := s.d.Hub.Nova()
	defer e.Fecha()
	if x := s.ocupa(gameID, e); x != nil {
		aviso("a busca do contexto dos jogadores já está rodando: a análise segue com o que já foi guardado")
		return false
	}
	defer s.libera(gameID, e)
	_, err = s.Busca(ctx, gameID, fonte, func(tipo string, dados any) {
		emite("contexto_jogadores", map[string]any{"tipo": tipo, "dados": dados})
	})
	if err != nil {
		aviso("a busca do contexto dos jogadores falhou (" + err.Error() + "): a análise segue com o que foi guardado")
	}
	return err == nil
}

// Resultado: o evento "concluido".
type Resultado struct {
	GameID        int64         `json:"game_id"`
	Fonte         string        `json:"fonte"`
	Pedidos       ranking.Gasto `json:"pedidos"`
	Elos          int           `json:"elos"`
	FatosRefeitos bool          `json:"fatos_refeitos"`
	Avisos        []string      `json:"avisos"`
}

// paraTudo: erro que derruba a busca inteira (cancelada, cliente fechado no
// meio, rota recusada pela lista de permitidas).
func paraTudo(ctx context.Context, err error) bool {
	var proibida *lcu.ErroRotaProibida
	return ctx.Err() != nil || errors.Is(err, lcu.ErrClienteFechado) || errors.As(err, &proibida)
}

// Busca o histórico de antes da partida de cada um dos 10 pela fonte dada,
// guarda e refaz a base de fatos. Para no primeiro erro que vale para todos
// (cancelada, cliente fechado); o erro de um jogador fica no histórico dele.
func (s *Servico) Busca(ctx context.Context, gameID int64, fonte string, emite func(tipo string, dados any)) (Resultado, error) {
	r := Resultado{GameID: gameID, Fonte: fonte, Avisos: []string{}}
	pt, err := partida.Le(s.d.DB, gameID)
	if err != nil {
		return r, err
	}
	if (fonte == FonteCliente && s.d.LCU == nil) || (fonte == FonteRiot && s.d.Riot == nil) || (fonte != FonteCliente && fonte != FonteRiot) {
		return r, fmt.Errorf("fonte %q indisponível neste servidor", fonte)
	}
	if pt.CriadaEmMs <= 0 {
		return r, errors.New("a partida não tem a data de criação: sem ela não dá para separar o que veio antes")
	}
	reg, ok := riot.RegiaoPorCodigo(pt.Plataforma)
	if !ok {
		reg, _ = riot.RegiaoPorCodigo(riot.RegiaoPadrao)
	}
	ps := append([]partida.Participante(nil), pt.Participantes...)
	sort.Slice(ps, func(a, b int) bool { return ps[a].ParticipanteID < ps[b].ParticipanteID })
	emite("inicio", map[string]any{"game_id": gameID, "fonte": fonte, "jogadores": len(ps), "limites": map[string]any{
		"partidas_por_jogador": PartidasPorJogador, "teto_partidas_riot_por_jogador": TetoPartidasRiotPorJogador,
		"teto_pedidos_riot": TetoPedidosRiot, "historico_cliente": ranking.FimHistoricoLCU}})

	// Riot: os puuids da Riot dos 10 pela própria partida (1 pedido ou o cache).
	var daRiot []ranking.ParticipanteRiot
	var erroRiot error
	if fonte == FonteRiot {
		daRiot, erroRiot = ranking.ParticipantesDaPartidaRiot(ctx, s.d.DB, s.d.Riot, reg, gameID, &r.Pedidos)
		if paraTudo(ctx, erroRiot) {
			return r, erroRiot
		}
		if erroRiot != nil {
			r.Avisos = append(r.Avisos, "a partida na match-v5: "+textoErro(erroRiot))
			emite("aviso", map[string]any{"texto": "a match-v5 não deu os participantes da partida: " + textoErro(erroRiot)})
		}
	}
	riotOK := s.d.Riot != nil && s.d.Riot.Confere(ctx) == nil
	pedidosRiot := func() int { return r.Pedidos.IDs + r.Pedidos.Partidas + r.Pedidos.Conta + 2*r.Elos }

	for _, p := range ps {
		if err := ctx.Err(); err != nil {
			return r, err
		}
		h := analise.HistoricoExterno{ParticipanteID: p.ParticipanteID, Puuid: p.Puuid, Fonte: fonte, Jogos: []analise.JogoExterno{}}
		var ha ranking.HistoricoAntes
		var err error
		switch {
		case fonte == FonteCliente && p.Puuid == "":
			err = ranking.ErrNaoAchado
		case fonte == FonteCliente:
			c2, cancela := context.WithTimeout(ctx, PrazoHistoricoLCU)
			ha, err = ranking.HistoricoAntesLCU(c2, s.d.DB, s.d.LCU, reg, p.Puuid, pt.CriadaEmMs, PartidasPorJogador, &r.Pedidos)
			cancela()
		case erroRiot != nil:
			err = erroRiot
		case pedidosRiot()+1+TetoPartidasRiotPorJogador > TetoPedidosRiot:
			err = fmt.Errorf("teto de %d pedidos à Riot desta busca atingido", TetoPedidosRiot)
		default:
			rp, achou := ranking.AchaParticipanteRiot(daRiot, p.Nome, p.Tag, p.CampeaoID)
			if !achou {
				err = ranking.ErrNaoAchado
				break
			}
			ha, err = ranking.HistoricoAntesRiot(ctx, s.d.DB, s.d.Riot, reg, rp.Puuid, pt.CriadaEmMs, PartidasPorJogador, MargemIDsRiot,
				TetoPartidasRiotPorJogador, &r.Pedidos)
		}
		if err != nil && paraTudo(ctx, err) {
			return r, err
		}
		switch {
		case errors.Is(err, ranking.ErrNaoAchado):
			h.Estado, h.Aviso = analise.HistNaoAchado, "a fonte não achou o jogador (conta trocada, sem Riot ID ou sem histórico)"
		case err != nil:
			h.Estado, h.Aviso = analise.HistErro, textoErro(err)
		case !ha.Alcanca:
			h.Estado = analise.HistNaoAlcanca
			h.Aviso = fmt.Sprintf("as %d partida(s) que a fonte tem são todas depois desta", ha.Vistas)
		case ha.Parcial:
			h.Estado = analise.HistParcial
			h.Aviso = fmt.Sprintf("%d partida(s) de antes ao alcance (de %d pedidas)", len(ha.Jogos), PartidasPorJogador)
		default:
			h.Estado = analise.HistOK
			if ha.Vistas == 0 {
				h.Aviso = "a fonte não devolveu nenhuma partida deste jogador"
			}
		}
		h.Vistas, h.MaisVelhaMs = ha.Vistas, ha.MaisVelhaMs
		for _, j := range ha.Jogos {
			if j.CriadaMs >= pt.CriadaEmMs {
				continue // nunca uma partida de depois (a fonte já filtra; aqui é a garantia)
			}
			h.Jogos = append(h.Jogos, analise.JogoExterno{GameID: analise.GameIDDoMatch(j.MatchID), CriadaMs: j.CriadaMs, Fila: j.Fila,
				Posicao: j.Posicao, CampeaoID: j.CampeaoID, Venceu: j.Venceu})
		}
		if err := analise.GravaHistoricoExterno(s.d.DB, gameID, h); err != nil {
			return r, err
		}
		// O elo da fila solo (o de hoje), com o cache de 24 h da IA.
		elo := ""
		if riotOK && pedidosRiot()+2 <= TetoPedidosRiot {
			if e, _ := ia.ConsultaEloRiot(ctx, s.d.DB, s.d.Riot, p.Puuid, p.Nome, p.Tag); e != nil {
				r.Elos++
				elo = e.Elo
				if e.Divisao != "" {
					elo += " " + e.Divisao
				}
			}
		}
		emite("jogador", map[string]any{"participante_id": p.ParticipanteID, "campeao": p.Campeao, "estado": h.Estado,
			"partidas": len(h.Jogos), "aviso": h.Aviso, "elo": elo})
	}

	// A base de fatos da partida passa a usar o que foi guardado.
	switch err := analise.GeraFatosDaPartida(ctx, s.d.Analise, gameID); {
	case err == nil:
		r.FatosRefeitos = true
	case errors.Is(err, analise.ErrSemResumo):
		r.Avisos = append(r.Avisos, "a partida ainda não foi analisada: o contexto entra na base de fatos quando ela for")
	default:
		r.Avisos = append(r.Avisos, "refazer a base de fatos: "+err.Error())
	}
	return r, nil
}

// textoErro: a frase do erro de uma fonte (nunca a chave: os erros da Riot já
// vêm sem ela).
func textoErro(err error) string {
	switch {
	case errors.Is(err, riot.ErrChaveRecusada):
		return "a Riot recusou a chave (troque o token.txt)"
	case errors.Is(err, riot.ErrSemChave):
		return "sem chave da Riot"
	case errors.Is(err, ranking.ErrSemHistorico):
		return "o histórico não veio (prazo do cliente ou formato)"
	case errors.Is(err, context.DeadlineExceeded):
		return "a fonte não respondeu a tempo"
	}
	return err.Error()
}
