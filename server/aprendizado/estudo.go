package aprendizado

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"

	"loltracking/server/banco"
	"loltracking/server/estudo"
	"loltracking/server/lcu"
	"loltracking/server/partida"
	"loltracking/server/rastreio"
)

// Estudo a partir de um jogador da coleta, e o rastreio das partidas de um
// estudo. Ficam aqui (e não no pacote estudo) porque reaproveitam a escolha de
// partidas da descoberta e a máquina da coleta (uma partida por vez, download,
// pausa ao fim do passe, retomada, SSE); o estudo em si continua sendo do
// pacote estudo (estudo.Cria, estudo.AdicionaPartida).

// Limites do estudo criado a partir de um jogador.
const (
	PorLadoPadrao = 3
	PorLadoMin    = 3
	PorLadoMax    = 5
)

// ErrJogadorNaoEncontrado: jogador da coleta que não existe (404).
var ErrJogadorNaoEncontrado = errors.New("jogador da coleta não encontrado")

// PedidoEstudo é o corpo de POST /api/aprendizado/jogadores/{id}/estudo.
type PedidoEstudo struct {
	PorLado int    `json:"por_lado"`
	Nome    string `json:"nome"`
}

// EstudoCriado é a resposta: o estudo e o que faltou para a comparação valer.
type EstudoCriado struct {
	Estudo         estudo.Estudo `json:"estudo"`
	Funcao         string        `json:"funcao"`
	Vitorias       int           `json:"vitorias"`
	Derrotas       int           `json:"derrotas"`
	FaltamVitorias int           `json:"faltam_vitorias"`
	FaltamDerrotas int           `json:"faltam_derrotas"`
	Aviso          string        `json:"aviso"`
}

var nomeFuncao = map[string]string{"TOP": "topo", "JUNGLE": "selva", "MIDDLE": "meio", "BOTTOM": "atirador", "UTILITY": "suporte"}

// CriaEstudo monta um estudo com as N melhores vitórias e as N derrotas mais
// recentes rastreáveis do jogador (histórico do LCU, mesma regra de
// rastreável da coleta), na função principal dele. Com menos de
// estudo.MinimoPorLado de um lado, cria assim mesmo e diz quantas faltam.
func (k *Coletor) CriaEstudo(ctx context.Context, jogadorID int64, p PedidoEstudo) (EstudoCriado, error) {
	if p.PorLado == 0 {
		p.PorLado = PorLadoPadrao
	}
	if p.PorLado < PorLadoMin || p.PorLado > PorLadoMax {
		return EstudoCriado{}, &ErroPedido{fmt.Sprintf("por_lado precisa ser de %d a %d", PorLadoMin, PorLadoMax)}
	}
	js, err := leJogadores(k.d.DB, `WHERE id = ?`, jogadorID)
	if err != nil {
		return EstudoCriado{}, err
	}
	if len(js) == 0 {
		return EstudoCriado{}, ErrJogadorNaoEncontrado
	}
	j := js[0]
	cfg, err := k.d.LCU.ConfiguracaoReplays(ctx)
	if err != nil {
		return EstudoCriado{}, err
	}
	patch := lcu.PatchDe(cfg.VersaoJogo)
	var bruto []byte
	for tentativa := 0; tentativa < 2; tentativa++ {
		bruto, err = k.d.LCU.Historico(ctx, j.puuidLCU, 0, 20)
		if err == nil || errors.Is(err, lcu.ErrNaoEncontrado) || paraTudo(ctx, err) {
			break
		}
	}
	if err != nil {
		return EstudoCriado{}, err
	}
	hist, err := leHistorico(bruto, j.puuidLCU)
	if err != nil {
		return EstudoCriado{}, fmt.Errorf("histórico em formato inesperado: %w", err)
	}
	funcao := j.Funcao
	if funcao == "" {
		funcao, _ = funcaoPrincipal(hist)
	}
	var vit, der []itemHist
	for _, h := range hist {
		if !rastreavel(h, patch) || !naFuncao(h, funcao) {
			continue
		}
		if h.Venceu {
			vit = append(vit, h)
		} else {
			der = append(der, h)
		}
	}
	inv := lcu.Invocador{Puuid: j.puuidLCU, Nome: j.Nome, Tag: j.Tag}
	melhores, err := k.melhoresVitorias(ctx, j.Regiao, vit, inv, funcao)
	if err != nil {
		return EstudoCriado{}, err
	}
	if len(melhores) > p.PorLado {
		melhores = melhores[:p.PorLado]
	}
	// Derrotas: as mais recentes (não as "piores": o que se quer ver é o
	// jogador perdendo do jeito de sempre, não a partida mais desastrosa).
	sort.SliceStable(der, func(a, b int) bool { return der[a].CriadaMs > der[b].CriadaMs })
	recentes, err := k.partidasDoDono(ctx, j.Regiao, der, inv, funcao, p.PorLado)
	if err != nil {
		return EstudoCriado{}, err
	}

	nome := p.Nome
	if nome == "" {
		nome = fmt.Sprintf("%s · %s", NomeElo[j.Elo], j.RiotID)
		if f := nomeFuncao[funcao]; f != "" {
			nome += " · " + f
		}
	}
	est, err := estudo.Cria(k.d.DB, estudo.Novo{Nome: &nome, Puuid: j.puuidLCU, Posicao: &funcao})
	if err != nil {
		return EstudoCriado{}, err
	}
	r := EstudoCriado{Funcao: funcao}
	for _, pt := range append(melhores, recentes...) {
		// A linha do tempo (rota permitida do LCU) deixa a comparação ter os
		// indicadores dela antes de qualquer rastreio.
		if err := partida.GuardaLinhaDoTempo(ctx, k.d.Partida, pt.GameID); err != nil {
			log.Printf("aprendizado: linha do tempo da partida %d para o estudo: %v", pt.GameID, err)
		}
		pe, err := estudo.AdicionaPartida(ctx, k.d.Analise, est.ID, pt.GameID, "")
		if err != nil {
			log.Printf("aprendizado: pôr a partida %d no estudo %d: %v", pt.GameID, est.ID, err)
			continue
		}
		if pe.Resultado == estudo.Vitoria {
			r.Vitorias++
		} else {
			r.Derrotas++
		}
	}
	r.FaltamVitorias = max(0, estudo.MinimoPorLado-r.Vitorias)
	r.FaltamDerrotas = max(0, estudo.MinimoPorLado-r.Derrotas)
	r.Aviso = AvisoFaltam(r.FaltamVitorias, r.FaltamDerrotas)
	if r.Estudo, err = estudo.Le(k.d.DB, est.ID); err != nil {
		return r, err
	}
	return r, nil
}

// AvisoFaltam é a frase para quando um lado tem menos que o mínimo.
func AvisoFaltam(fv, fd int) string {
	if fv == 0 && fd == 0 {
		return ""
	}
	partes := ""
	switch {
	case fv > 0 && fd > 0:
		partes = fmt.Sprintf("%s e %s", plural(fv, "vitória", "vitórias"), plural(fd, "derrota", "derrotas"))
	case fv > 0:
		partes = plural(fv, "vitória", "vitórias")
	default:
		partes = plural(fd, "derrota", "derrotas")
	}
	return fmt.Sprintf("faltam %s rastreáveis para a comparação valer (ela precisa de pelo menos %d de cada lado); "+
		"quando o jogador tiver mais partidas no patch atual, acrescente pelas sugestões", partes, estudo.MinimoPorLado)
}

func plural(n int, um, varios string) string {
	if n == 1 {
		return "1 " + um
	}
	return fmt.Sprintf("%d %s", n, varios)
}

// --- Rastreio das partidas de um estudo ----------------------------------------

// RastreioDoEstudo é a resposta de GET/POST /api/estudos/{id}/rastreio.
type RastreioDoEstudo struct {
	Coleta   *Coleta           `json:"coleta"`
	Partidas []PartidaDaColeta `json:"partidas"`
	// Pendentes: partidas do estudo em que o jogador ainda não foi rastreado.
	Pendentes int `json:"pendentes"`
}

// ultimaDoEstudo é a coleta mais nova do estudo (nil se nunca rastreou).
func (k *Coletor) ultimaDoEstudo(estudoID int64) (*Coleta, error) {
	l, err := leColetas(k.d.DB, `WHERE estudo_id = ? ORDER BY id DESC LIMIT 1`, estudoID)
	if err != nil || len(l) == 0 {
		return nil, err
	}
	return &l[0], nil
}

// pendentesDoEstudo: as partidas do estudo sem o jogador rastreado. Olha o
// passe concluído além da análise: a análise é refeita em segundo plano depois
// do passe e pode ainda não ter chegado.
func pendentesDoEstudo(db *banco.DB, ps []estudo.PartidaDoEstudo) []PartidaDaColeta {
	var l []PartidaDaColeta
	for _, p := range ps {
		if p.Rastreado || p.ParticipanteID <= 0 {
			continue
		}
		if ps, ok, err := rastreio.MelhorPasse(db, p.GameID, p.ParticipanteID); err == nil && ok && ps.Estado == rastreio.Concluido {
			continue
		}
		l = append(l, PartidaDaColeta{GameID: p.GameID, ParticipanteID: p.ParticipanteID, CampeaoID: p.CampeaoID,
			Campeao: p.Campeao, Abates: p.Abates, Mortes: p.Mortes, Assistencias: p.Assistencias, DuracaoS: p.DuracaoS})
	}
	return l
}

// LeRastreioDoEstudo devolve o último rastreio do estudo, com o progresso.
func (k *Coletor) LeRastreioDoEstudo(estudoID int64) (RastreioDoEstudo, error) {
	e, err := estudo.Le(k.d.DB, estudoID)
	if err != nil {
		return RastreioDoEstudo{}, err
	}
	ps, err := estudo.Partidas(k.d.DB, e)
	if err != nil {
		return RastreioDoEstudo{}, err
	}
	r := RastreioDoEstudo{Partidas: []PartidaDaColeta{}, Pendentes: len(pendentesDoEstudo(k.d.DB, ps))}
	c, err := k.ultimaDoEstudo(estudoID)
	if err != nil || c == nil {
		return r, err
	}
	d, err := k.Detalhe(c.ID)
	if err != nil {
		return r, err
	}
	r.Coleta, r.Partidas = &d.Coleta, d.Partidas
	return r, nil
}

// RastreiaEstudo rastreia, uma por vez, as partidas do estudo em que o jogador
// ainda não foi rastreado (só ele em cada partida), baixando o replay quando
// precisa. Se o último rastreio do estudo está pausado ou parou com erro,
// retoma esse.
func (k *Coletor) RastreiaEstudo(ctx context.Context, estudoID int64, vel float64) (RastreioDoEstudo, string, error) {
	if vel == 0 {
		vel = 4
	}
	if vel < 0.5 || vel > 8 {
		return RastreioDoEstudo{}, "", &ErroPedido{"velocidade precisa estar entre 0,5 e 8"}
	}
	e, err := estudo.Le(k.d.DB, estudoID)
	if err != nil {
		return RastreioDoEstudo{}, "", err
	}
	if c, err := k.ultimaDoEstudo(estudoID); err != nil {
		return RastreioDoEstudo{}, "", err
	} else if c != nil {
		switch c.Estado {
		case Rodando:
			return RastreioDoEstudo{}, "", &ErroConflito{"as partidas deste estudo já estão sendo rastreadas"}
		case Pausado, Falhou:
			exID, err := k.Retoma(ctx, c.ID)
			if err != nil {
				return RastreioDoEstudo{}, "", err
			}
			r, err := k.LeRastreioDoEstudo(estudoID)
			return r, exID, err
		}
	}
	ps, err := estudo.Partidas(k.d.DB, e)
	if err != nil {
		return RastreioDoEstudo{}, "", err
	}
	pend := pendentesDoEstudo(k.d.DB, ps)
	if len(ps) == 0 {
		return RastreioDoEstudo{}, "", &ErroConflito{"o estudo ainda não tem partidas"}
	}
	if len(pend) == 0 {
		return RastreioDoEstudo{}, "", &ErroConflito{"todas as partidas deste estudo já têm o jogador rastreado"}
	}
	// Regra 2 e cliente aberto: confere antes de prometer (o rastreio confere de novo).
	if _, err := k.d.LCU.FaseGameflow(ctx); err != nil {
		return RastreioDoEstudo{}, "", err
	}
	k.mu.Lock()
	if err := k.ocupado(); err != nil {
		k.mu.Unlock()
		return RastreioDoEstudo{}, "", err
	}
	id, err := criaColeta(k.d.DB, novaColeta{ppj: 1, quem: QuemJogador, vel: vel, estudoID: estudoID, partidas: pend})
	if err != nil {
		k.mu.Unlock()
		return RastreioDoEstudo{}, "", err
	}
	exID := k.comecaTravado(id)
	k.mu.Unlock()
	r, err := k.LeRastreioDoEstudo(estudoID)
	return r, exID, err
}
