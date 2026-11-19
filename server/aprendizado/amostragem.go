package aprendizado

import (
	"context"
	"fmt"
	"sort"

	"loltracking/server/riot"
)

// Descoberta por amostragem ("amostragem": true na coleta). Logo depois de uma
// troca de patch, os primeiros de cada divisão (o que a descoberta normal lê)
// quase nunca têm partida no patch novo, e o replay só abre no patch
// instalado. Aqui o que se quer é ALGUÉM que já jogou: começa no meio do elo
// (divisões II e III, página do meio) e espalha como numa busca binária (a do
// meio, depois os meios das metades), sem os filtros de "está subindo"
// (veterano, poucas partidas, travado, mono). O único critério é ter ranqueada
// solo rastreável no patch instalado (rastreavel em descoberta.go).
//
// Pedidos à API da Riot (limites da chave de desenvolvimento, riot.LimitesPedidos:
// 20/s e 100 a cada 2 min, por host). league-v4 vai ao host da plataforma
// (br1) e account-v1 ao do roteamento (americas): contam à parte.
//   - league-v4, por divisão: achar o número de páginas custa no máximo
//     1 + log2(MaxPaginasLiga) pedidos para achar a primeira vazia (1, 2, 4, ...,
//     512) mais log2(MaxPaginasLiga)-1 para dividir o último intervalo: 18; as
//     páginas visitadas (no máximo PaginasPorDivisaoAmostragem) reaproveitam as
//     que a busca já trouxe, então no pior caso mais 5. 4 divisões × 23 = 92
//     pedidos, abaixo dos 100 da janela de 2 min.
//   - account-v1: 1 por candidato conferido, no máximo MaxCandidatosAmostragem
//     (200) por elo: os 100 primeiros saem logo e os outros 100 esperam a
//     janela de 2 min abrir (o limitador do cliente espera sozinho).
var (
	// MaxCandidatosAmostragem: teto de candidatos conferidos por elo. 200 =
	// 4 divisões × 5 páginas × 10 por página; mais que os 60 da descoberta
	// normal porque, logo depois do patch, a maioria não tem partida nova. São
	// 200 pedidos à account-v1 (2 janelas de 2 min da chave) e 200 históricos
	// no LCU.
	MaxCandidatosAmostragem = 200
	// CandidatosPorPaginaAmostragem: quantos de cada página, os com mais
	// chance de ter jogado hoje; poucos por página espalham a busca pelo elo.
	CandidatosPorPaginaAmostragem = 10
	// PaginasPorDivisaoAmostragem: páginas visitadas por divisão.
	PaginasPorDivisaoAmostragem = 5
	// MaxPaginasLiga: até onde a busca do número de páginas sobe (~205 por
	// página: ~100 mil jogadores numa divisão, muito acima de qualquer uma do BR).
	MaxPaginasLiga = 512
)

// DivisoesAmostragem: o meio primeiro (II e III), depois as pontas.
var DivisoesAmostragem = []string{"II", "III", "I", "IV"}

// ordemBinaria: as páginas 1..n na ordem de uma busca binária em largura: o
// meio, depois o meio de cada metade, e assim por diante (9 → 5 2 7 1 3 6 8 4 9).
// Nunca devolve página fora de 1..n.
func ordemBinaria(n int) []int {
	var l []int
	type faixa struct{ a, b int }
	fila := []faixa{{1, n}}
	for len(fila) > 0 {
		f := fila[0]
		fila = fila[1:]
		if f.a > f.b {
			continue
		}
		m := (f.a + f.b) / 2
		l = append(l, m)
		fila = append(fila, faixa{f.a, m - 1}, faixa{m + 1, f.b})
	}
	return l
}

// ordenaAmostra: tira os inativos (parados: não jogaram hoje) e põe primeiro
// quem tem mais chance de ter jogado hoje pelos sinais da league-v4:
// hotStreak (ganhou as últimas), freshBlood (mudou de liga há pouco), mais
// partidas na liga; empate fica na ordem da página. São sinais fracos: a
// league-v4 não diz quando foi a última partida.
func ordenaAmostra(l []riot.EntradaLiga) ([]riot.EntradaLiga, int) {
	var ativos []riot.EntradaLiga
	inativos := 0
	for _, e := range l {
		if e.Inactive {
			inativos++
			continue
		}
		ativos = append(ativos, e)
	}
	sort.SliceStable(ativos, func(a, b int) bool {
		x, y := ativos[a], ativos[b]
		if x.HotStreak != y.HotStreak {
			return x.HotStreak
		}
		if x.FreshBlood != y.FreshBlood {
			return x.FreshBlood
		}
		return x.Vitorias+x.Derrotas > y.Vitorias+y.Derrotas
	})
	return ativos, inativos
}

// paginasLiga lê as páginas de uma divisão guardando o que já veio (a busca
// do número de páginas e a visita usam as mesmas) e conta os pedidos.
type paginasLiga struct {
	k       *Coletor
	reg     riot.Regiao
	elo     string
	div     string
	lidas   map[int][]riot.EntradaLiga
	pedidos *int
}

func (p *paginasLiga) pagina(ctx context.Context, n int) ([]riot.EntradaLiga, error) {
	if l, ok := p.lidas[n]; ok {
		return l, nil
	}
	*p.pedidos++
	l, err := p.k.d.Riot.Liga(ctx, p.reg, p.elo, p.div, n)
	if err != nil {
		return nil, err
	}
	for i := range l {
		if l[i].Rank == "" {
			l[i].Rank = p.div
		}
	}
	p.lidas[n] = l
	return l, nil
}

// conta acha o número de páginas da divisão por busca binária na própria
// league-v4 (página vazia = passou do fim): dobra até a primeira vazia e
// divide o intervalo ao meio. Para em MaxPaginasLiga.
func (p *paginasLiga) conta(ctx context.Context) (int, error) {
	vazia := func(n int) (bool, error) {
		l, err := p.pagina(ctx, n)
		return len(l) == 0, err
	}
	if v, err := vazia(1); err != nil || v {
		return 0, err
	}
	cheia, fim := 1, 0 // cheia: última com gente conhecida; fim: primeira vazia conhecida
	for n := 2; n <= MaxPaginasLiga; n *= 2 {
		v, err := vazia(n)
		if err != nil {
			return 0, err
		}
		if v {
			fim = n
			break
		}
		cheia = n
	}
	if fim == 0 {
		return cheia, nil
	}
	for fim-cheia > 1 {
		m := (cheia + fim) / 2
		v, err := vazia(m)
		if err != nil {
			return 0, err
		}
		if v {
			fim = m
		} else {
			cheia = m
		}
	}
	return cheia, nil
}

// blocoAmostra é um grupo de candidatos de onde a amostragem tira os seus: uma
// página de uma divisão ou, de Mestre para cima, um bloco da liga inteira.
type blocoAmostra struct {
	divisao string
	pagina  int
	ler     func(ctx context.Context) ([]riot.EntradaLiga, error)
}

// descobreEloAmostragem acha os jogadores do elo por amostragem (ver acima).
// Retomada: refaz o mesmo caminho e pula os candidatos já conferidos.
func (k *Coletor) descobreEloAmostragem(ctx context.Context, c *corridaColeta, col Coleta, reg riot.Regiao, e EloDaColeta, patch string) error {
	c.ex.Emite("elo_inicio", map[string]any{"elo": e.Elo, "nome": e.Nome, "amostragem": true})
	alvo := col.JogadoresPorElo
	aceitos, vistos, desc := e.Jogadores, e.CandidatosVistos, e.Descartes
	pular := vistos
	// Os inativos são recontados no caminho (as páginas são lidas de novo).
	desc[DescInativo] = 0
	pedidos, paginas := 0, 0
	grava := func(estado, aviso string) {
		mudaElo(k.d.DB, col.ID, e.Elo, estado, vistos, desc, aviso)
		mudaPaginas(k.d.DB, col.ID, e.Elo, paginas, e.PedidosLiga+pedidos)
	}
	grava(EloDescobrindo, "")
	acabou := func() bool { return aceitos >= alvo || vistos >= MaxCandidatosAmostragem }

	// Os blocos de cada divisão só são lidos quando a busca chega nela.
	var divisoes [][]blocoAmostra
	if riot.EloApex(e.Elo) {
		pedidos++
		l, err := k.d.Riot.Liga(ctx, reg, e.Elo, "", 0)
		if err != nil {
			return err
		}
		// A liga inteira por LP (a ordem do ranking), em blocos visitados do meio para fora.
		sort.SliceStable(l, func(a, b int) bool { return l[a].LP > l[b].LP })
		n := (len(l) + CandidatosPorPaginaAmostragem - 1) / CandidatosPorPaginaAmostragem
		var bs []blocoAmostra
		for _, pg := range ordemBinaria(n) {
			ini := (pg - 1) * CandidatosPorPaginaAmostragem
			bloco := l[ini:min(len(l), ini+CandidatosPorPaginaAmostragem)]
			bs = append(bs, blocoAmostra{pagina: pg, ler: func(context.Context) ([]riot.EntradaLiga, error) { return bloco, nil }})
		}
		divisoes = append(divisoes, bs)
	}

	vistosAqui := map[string]bool{}
	avalia := func(b blocoAmostra) error {
		l, err := b.ler(ctx)
		if err != nil {
			return err
		}
		ord, inativos := ordenaAmostra(l)
		desc[DescInativo] += inativos
		if len(ord) == 0 {
			return nil
		}
		paginas++
		if len(ord) > CandidatosPorPaginaAmostragem {
			ord = ord[:CandidatosPorPaginaAmostragem]
		}
		for _, ce := range ord {
			if acabou() {
				return nil
			}
			if ce.Puuid == "" || vistosAqui[ce.Puuid] {
				continue
			}
			vistosAqui[ce.Puuid] = true
			if pular > 0 {
				pular--
				continue
			}
			if c.pausar.Load() {
				return errPausa
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			res, info, err := k.avaliaCandidato(ctx, col, reg, e.Elo, vistos+1, ce, patch, opcCandidato{})
			if err != nil {
				return err
			}
			ev := map[string]any{"elo": e.Elo, "posicao": vistos + 1, "lp": ce.LP, "divisao": ce.Rank, "pagina": b.pagina,
				"vitorias": ce.Vitorias, "derrotas": ce.Derrotas, "pontuacao_subida": PontuacaoSubida(ce), "resultado": res, "amostragem": true}
			for kk, v := range info {
				ev[kk] = v
			}
			c.ex.Emite("candidato", ev)
			vistos++
			if res != Aceito && res != "" {
				desc[res]++
			}
			// Recontado do banco: os aproveitados também ocupam vaga.
			aceitos = achadosNoElo(k.d.DB, col.ID, e.Elo)
			grava(EloDescobrindo, "")
		}
		return nil
	}

	var err error
	if riot.EloApex(e.Elo) {
		for _, b := range divisoes[0] {
			if acabou() {
				break
			}
			if err = avalia(b); err != nil {
				break
			}
		}
	} else {
		for _, div := range DivisoesAmostragem {
			if acabou() {
				break
			}
			pl := &paginasLiga{k: k, reg: reg, elo: e.Elo, div: div, lidas: map[int][]riot.EntradaLiga{}, pedidos: &pedidos}
			var n int
			if n, err = pl.conta(ctx); err != nil {
				break
			}
			ordem := ordemBinaria(n)
			if len(ordem) > PaginasPorDivisaoAmostragem {
				ordem = ordem[:PaginasPorDivisaoAmostragem]
			}
			for _, pg := range ordem {
				if acabou() {
					break
				}
				b := blocoAmostra{divisao: div, pagina: pg, ler: func(ctx context.Context) ([]riot.EntradaLiga, error) { return pl.pagina(ctx, pg) }}
				if err = avalia(b); err != nil {
					break
				}
			}
			if err != nil {
				break
			}
		}
	}
	if err != nil {
		grava(EloDescobrindo, "")
		return err
	}
	aviso := ""
	if aceitos < alvo {
		aviso = fmt.Sprintf("por amostragem: achou %d de %d jogadores com partida no patch %s entre %d candidatos conferidos (%d páginas, %d pedidos à liga)",
			aceitos, alvo, patch, vistos, paginas, pedidos)
		if a := avisoCotas(k.d.DB, col, e.Elo); a != "" {
			aviso += "; " + a
		}
	}
	grava(EloPronto, aviso)
	c.ex.Emite("elo_fim", map[string]any{"elo": e.Elo, "jogadores": aceitos, "aviso": aviso, "candidatos_vistos": vistos,
		"paginas_vistas": paginas, "pedidos_liga": pedidos})
	return nil
}

// MotivoAmostragem é o "porquê" do jogador achado por amostragem.
func MotivoAmostragem(j JogadorDaColeta) string {
	s := "por amostragem: tem partida no patch atual"
	if n := j.Vitorias + j.Derrotas; n > 0 {
		s += fmt.Sprintf("; %.0f%% de vitórias em %d partidas na liga", j.TaxaVitoria*100, n)
	}
	return s
}
