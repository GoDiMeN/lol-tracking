package aprendizado

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"loltracking/server/lcu"
	"loltracking/server/ranking"
	"loltracking/server/riot"
)

// COLETA A PARTIR DO RANKING ("ranking" no pedido): "rastrear os N primeiros
// de cada rota". Em vez da descoberta, a lista de cada elo × rota é a do
// ranking do Lane (server/ranking) mais recente daquele patch, na ordem do
// modo pedido. Regras:
//   - quem = "todos" (a regra do estudo: os 10 de cada partida são rastreados);
//     pedir "jogador" é 400;
//   - o patch do ranking tem de ser o do jogo instalado (o replay só abre nele);
//   - a cota é por rota (funcoes × primeiros) e cada jogador conta na rota do
//     ranking dele, não na função principal do histórico do LCU;
//   - sem exigir mono (o ranking é o critério);
//   - quem não tem partida rastreável no patch não ocupa vaga: a lista desce
//     até o fim do ranking (o 4º entra no lugar do 2º sem replay);
//   - aproveitar começa desligado: com "todos" os 10 já são rastreados, e o
//     aproveitado só tiraria a vaga dos primeiros do ranking.

// RankingDaColeta: de qual ranking saiu a lista de um elo × rota.
type RankingDaColeta struct {
	Elo       string `json:"elo"`
	Rota      string `json:"rota"`
	RankingID int64  `json:"ranking_id"`
}

// doRanking: a coleta parte do ranking.
func (c Coleta) doRanking() bool { return len(c.Rankings) > 0 }

func (c Coleta) rankingDe(elo, rota string) int64 {
	for _, r := range c.Rankings {
		if r.Elo == elo && r.Rota == rota {
			return r.RankingID
		}
	}
	return 0
}

// validaRanking completa os padrões do pedido a partir do ranking.
func (p *Pedido) validaRanking() error {
	r := p.Ranking
	if len(p.Elos) == 0 {
		return &ErroPedido{`com "ranking", diga os "elos" (ex.: ["PLATINUM", "CHALLENGER"])`}
	}
	if p.Amostragem {
		return &ErroPedido{`"ranking" e "amostragem" são dois jeitos de escolher os jogadores: use um só`}
	}
	if p.Funcao != "" {
		return &ErroPedido{`com "ranking", use "funcoes" (lista de rotas; vazia = as cinco), não "funcao"`}
	}
	switch p.Quem {
	case "":
		p.Quem = QuemTodos
	case QuemTodos:
	default:
		return &ErroPedido{`a coleta a partir do ranking rastreia os 10 de cada partida (regra do estudo): "quem" precisa ser "todos"`}
	}
	r.Modo = strings.TrimSpace(r.Modo)
	if r.Modo == "" {
		r.Modo = ranking.ModoSubindo
	}
	if r.Modo != ranking.ModoSubindo && r.Modo != ranking.ModoMelhor {
		return &ErroPedido{`ranking.modo precisa ser "subindo" ou "melhor"`}
	}
	if r.Primeiros == 0 {
		r.Primeiros = 3
	}
	if r.Primeiros < 1 || r.Primeiros > MaxJogadoresPorFuncao {
		return &ErroPedido{fmt.Sprintf("ranking.primeiros precisa ser de 1 a %d", MaxJogadoresPorFuncao)}
	}
	if p.Funcoes == nil {
		p.Funcoes = []string{}
	}
	if p.JogadoresPorFuncao != 0 && p.JogadoresPorFuncao != r.Primeiros {
		return &ErroPedido{`com "ranking", a cota de cada rota é ranking.primeiros: não mande "jogadores_por_funcao"`}
	}
	p.JogadoresPorFuncao = r.Primeiros
	if p.Aproveitar == nil {
		nao := false
		p.Aproveitar = &nao
	}
	return nil
}

// resolveRankings acha o ranking de cada elo × rota no patch (o instalado se
// o pedido não diz) e confere que é o patch instalado.
func (k *Coletor) resolveRankings(ctx context.Context, p Pedido) ([]RankingDaColeta, string, error) {
	cfg, err := k.d.LCU.ConfiguracaoReplays(ctx)
	if err != nil {
		return nil, "", err
	}
	instalado := lcu.PatchDe(cfg.VersaoJogo)
	patch := lcu.PatchDe(strings.TrimSpace(p.Ranking.Patch))
	if patch == "" {
		patch = instalado
	}
	if patch != instalado {
		return nil, "", &ErroConflito{fmt.Sprintf("o ranking pedido é do patch %s e o jogo instalado é do %s: o replay só abre no patch instalado", patch, instalado)}
	}
	var l []RankingDaColeta
	var faltam []string
	for _, elo := range p.Elos {
		for _, f := range p.Funcoes {
			r, err := ranking.Ultimo(k.d.DB, p.Regiao, patch, elo, f, 0)
			if errors.Is(err, ranking.ErrNaoEncontrado) {
				faltam = append(faltam, NomeElo[elo]+" · "+nomeFuncao[f])
				continue
			}
			if err != nil {
				return nil, "", err
			}
			l = append(l, RankingDaColeta{Elo: elo, Rota: f, RankingID: r.ID})
		}
	}
	if len(faltam) > 0 {
		return nil, "", &ErroConflito{fmt.Sprintf("não há ranking do patch %s para: %s. Atualize o ranking desses elos antes (Estudo → Ranking)", patch, strings.Join(faltam, ", "))}
	}
	return l, patch, nil
}

func rankingsJSON(l []RankingDaColeta) string {
	if len(l) == 0 {
		return ""
	}
	b, _ := json.Marshal(l)
	return string(b)
}

// descobreEloRanking: a "descoberta" de um elo é descer as listas do ranking,
// uma rota depois da outra, até encher a cota de cada uma. Retomada: a lista
// é a mesma (o ranking gravado não muda), então continua de candidatos_vistos.
func (k *Coletor) descobreEloRanking(ctx context.Context, c *corridaColeta, col Coleta, reg riot.Regiao, e EloDaColeta, patch string) error {
	if col.RankingPatch != "" && col.RankingPatch != patch {
		return &ErroConflito{fmt.Sprintf("o ranking desta coleta é do patch %s e o jogo instalado agora é do %s: o replay só abre no patch instalado", col.RankingPatch, patch)}
	}
	c.ex.Emite("elo_inicio", map[string]any{"elo": e.Elo, "nome": e.Nome, "ranking": true, "modo": col.ModoRanking})
	type item struct {
		rota string
		pos  ranking.Posicao
	}
	var lista []item
	for _, f := range col.Funcoes {
		rid := col.rankingDe(e.Elo, f)
		if rid == 0 {
			continue
		}
		ps, err := ranking.Posicoes(k.d.DB, rid, col.ModoRanking)
		if err != nil {
			return err
		}
		for _, p := range ps {
			lista = append(lista, item{rota: f, pos: p})
		}
	}
	vistos, desc := e.CandidatosVistos, e.Descartes
	mudaElo(k.d.DB, col.ID, e.Elo, EloDescobrindo, vistos, desc, "")
	for ; vistos < len(lista); vistos++ {
		if c.pausar.Load() {
			mudaElo(k.d.DB, col.ID, e.Elo, EloDescobrindo, vistos, desc, "")
			return errPausa
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		it := lista[vistos]
		// Rota já cheia: o resto da lista dela não é olhado (nem conta descarte).
		if vaga(k.d.DB, col, e.Elo, it.rota) != "" {
			continue
		}
		n := it.pos.Numeros
		ent := riot.EntradaLiga{Puuid: it.pos.Puuid, Tier: e.Elo, Rank: n.Divisao, LP: n.LP, Vitorias: n.Vitorias, Derrotas: n.Derrotas,
			HotStreak: n.Hot, FreshBlood: n.Fresh, Veteran: n.Veteran}
		opc := opcCandidato{funcao: it.rota}
		if it.pos.Nome != "" {
			opc.conta = &riot.Conta{Puuid: it.pos.Puuid, Nome: it.pos.Nome, Tag: it.pos.Tag}
		}
		res, info, err := k.avaliaCandidato(ctx, col, reg, e.Elo, it.pos.Posicao, ent, patch, opc)
		if err != nil {
			return err
		}
		ev := map[string]any{"elo": e.Elo, "posicao": it.pos.Posicao, "rota": it.rota, "nota": it.pos.Nota, "lp": n.LP,
			"divisao": n.Divisao, "vitorias": n.Vitorias, "derrotas": n.Derrotas, "resultado": res, "ranking": true}
		for kk, v := range info {
			ev[kk] = v
		}
		c.ex.Emite("candidato", ev)
		if res != Aceito && res != "" {
			desc[res]++
		}
		mudaElo(k.d.DB, col.ID, e.Elo, EloDescobrindo, vistos+1, desc, "")
	}
	aceitos := achadosNoElo(k.d.DB, col.ID, e.Elo)
	aviso := ""
	if a := avisoCotas(k.d.DB, col, e.Elo); a != "" {
		aviso = "o ranking acabou antes de encher: " + a
	}
	mudaElo(k.d.DB, col.ID, e.Elo, EloPronto, vistos, desc, aviso)
	c.ex.Emite("elo_fim", map[string]any{"elo": e.Elo, "jogadores": aceitos, "aviso": aviso})
	return nil
}

// fracaoNaFuncao: a fração das ranqueadas solo do histórico na função dada.
func fracaoNaFuncao(hist []itemHist, funcao string) float64 {
	n, na := 0, 0
	for _, h := range hist {
		if h.Fila == FilaSolo && h.Posicao != "" {
			n++
			if h.Posicao == funcao {
				na++
			}
		}
	}
	if n == 0 {
		return 0
	}
	return float64(na) / float64(n)
}

// MotivoRanking: o "porquê" do jogador que veio do ranking.
func MotivoRanking(j JogadorDaColeta, modo string) string {
	return fmt.Sprintf("ranking do Lane (%s): %dº na %s", modo, j.Posicao, nomeFuncao[j.Funcao])
}
