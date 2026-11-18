package aprendizado

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"

	"loltracking/server/lcu"
	"loltracking/server/partida"
	"loltracking/server/riot"
)

// Resultados de um candidato na descoberta.
const (
	Aceito       = "aceito"
	NaoAchado    = "nao_achado"
	NaoMono      = "nao_mono"
	SemHistorico = "sem_historico"
	// DescSemPartida: o jogador não tem partida rastreável (nenhuma vitória de
	// 15 min ou mais no patch instalado no histórico) e não ocupa vaga do elo.
	DescSemPartida = "sem_partida"
	// DescOutraFuncao: a coleta pediu uma função e a principal do jogador é outra.
	DescOutraFuncao = "outra_funcao"
)

// errPausa: a pausa pedida chegou a um ponto de parada.
var errPausa = errors.New("pausa pedida")

// candidatosDoElo traz os jogadores do elo pela Riot, filtrados e ordenados
// pela pontuação de subida (subida.go), e os descartes da filtragem. Abaixo
// de Mestre: as divisões I a IV, PaginasPorDivisao páginas de cada
// (aproximação: a Riot não devolve a divisão inteira nem ordenada).
func (k *Coletor) candidatosDoElo(ctx context.Context, reg riot.Regiao, elo string) ([]riot.EntradaLiga, map[string]int, int, int, error) {
	var todos []riot.EntradaLiga
	paginas, pedidos := 0, 0
	apex := riot.EloApex(elo)
	if apex {
		l, err := k.d.Riot.Liga(ctx, reg, elo, "", 0)
		pedidos++
		if err != nil {
			return nil, nil, 0, 0, err
		}
		todos = l
		if len(l) > 0 {
			paginas++
		}
	} else {
		for _, div := range riot.Divisoes {
			for pg := 1; pg <= PaginasPorDivisao; pg++ {
				l, err := k.d.Riot.Liga(ctx, reg, elo, div, pg)
				pedidos++
				if err != nil {
					return nil, nil, 0, 0, err
				}
				if len(l) > 0 {
					paginas++
				}
				for i := range l {
					if l[i].Rank == "" {
						l[i].Rank = div
					}
				}
				todos = append(todos, l...)
				if len(l) == 0 {
					break
				}
			}
		}
	}
	vistos := map[string]bool{}
	var l []riot.EntradaLiga
	for _, e := range todos {
		if e.Puuid == "" || vistos[e.Puuid] {
			continue
		}
		vistos[e.Puuid] = true
		l = append(l, e)
	}
	ord, desc := ordenaCandidatos(l, apex)
	return ord, desc, paginas, pedidos, nil
}

// descobreElo acha os jogadores do elo, do candidato onde parou. Devolve
// errPausa se a pausa foi pedida no meio (entre um candidato e outro).
func (k *Coletor) descobreElo(ctx context.Context, c *corridaColeta, col Coleta, reg riot.Regiao, e EloDaColeta, patch string) error {
	c.ex.Emite("elo_inicio", map[string]any{"elo": e.Elo, "nome": e.Nome})
	cands, descFiltro, paginas, pedidos, err := k.candidatosDoElo(ctx, reg, e.Elo)
	if err != nil {
		return err
	}
	mudaPaginas(k.d.DB, col.ID, e.Elo, paginas, e.PedidosLiga+pedidos)
	alvo := col.JogadoresPorElo
	aceitos := e.Jogadores
	vistos := e.CandidatosVistos
	desc := e.Descartes
	// A filtragem é refeita a cada leitura da liga: os números dela substituem.
	for _, kk := range []string{DescInativo, DescVeterano, DescPoucasPartidas, DescTravado} {
		desc[kk] = descFiltro[kk]
	}
	mudaElo(k.d.DB, col.ID, e.Elo, EloDescobrindo, vistos, desc, "")
	for ; vistos < len(cands) && vistos < MaxCandidatosPorElo && aceitos < alvo; vistos++ {
		if c.pausar.Load() {
			return errPausa
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		res, info, err := k.avaliaCandidato(ctx, col, reg, e.Elo, vistos+1, cands[vistos], patch)
		if err != nil {
			return err
		}
		ce := cands[vistos]
		ev := map[string]any{"elo": e.Elo, "posicao": vistos + 1, "lp": ce.LP, "divisao": ce.Rank, "vitorias": ce.Vitorias,
			"derrotas": ce.Derrotas, "pontuacao_subida": PontuacaoSubida(ce), "resultado": res}
		for kk, v := range info {
			ev[kk] = v
		}
		c.ex.Emite("candidato", ev)
		if res == Aceito {
			aceitos++
		} else if res != "" {
			desc[res]++
		}
		mudaElo(k.d.DB, col.ID, e.Elo, EloDescobrindo, vistos+1, desc, "")
	}
	aviso := ""
	if aceitos < alvo {
		limite := min(len(cands), MaxCandidatosPorElo)
		if exigeMono(e.Elo) {
			aviso = fmt.Sprintf("achou %d de %d mono champions com partida rastreável entre os %d primeiros candidatos", aceitos, alvo, limite)
		} else {
			aviso = fmt.Sprintf("achou %d de %d jogadores com partida rastreável entre os %d primeiros candidatos", aceitos, alvo, limite)
		}
	}
	mudaElo(k.d.DB, col.ID, e.Elo, EloPronto, vistos, desc, aviso)
	c.ex.Emite("elo_fim", map[string]any{"elo": e.Elo, "jogadores": aceitos, "aviso": aviso})
	return nil
}

// avaliaCandidato resolve o candidato pelo Riot ID no LCU, lê o histórico,
// confere o mono (Mestre para cima), escolhe as partidas e grava. Erro só
// quando a coleta precisa parar (chave, cliente fechado, contexto); o resto é
// um resultado ("nao_achado" e afins). Resultado "" = repetido (já na coleta).
func (k *Coletor) avaliaCandidato(ctx context.Context, col Coleta, reg riot.Regiao, elo string, posicao int,
	e riot.EntradaLiga, patch string) (string, map[string]any, error) {
	var ja int
	k.d.DB.QueryRow(`SELECT COUNT(*) FROM aprendizado_jogadores WHERE coleta_id = ? AND puuid_riot = ?`, col.ID, e.Puuid).Scan(&ja)
	if ja > 0 {
		return "", nil, nil
	}
	conta, err := k.d.Riot.ContaPorPuuid(ctx, reg, e.Puuid)
	if errors.Is(err, riot.ErrNaoEncontradoRiot) {
		return NaoAchado, nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	info := map[string]any{"riot_id": conta.Nome + "#" + conta.Tag}
	// O puuid da Riot não é o do LCU: o jogador é achado no cliente pelo Riot ID.
	inv, err := k.d.LCU.BuscaAlias(ctx, conta.Nome, conta.Tag)
	if errors.Is(err, lcu.ErrNaoEncontrado) {
		return NaoAchado, info, nil
	}
	if err != nil {
		if paraTudo(ctx, err) {
			return "", nil, err
		}
		log.Printf("aprendizado: buscar um candidato de %s no cliente: %v", elo, err)
		return NaoAchado, info, nil
	}
	// O histórico de outro jogador às vezes demora mais que o prazo do cliente
	// (medido: > 20 s): tenta de novo uma vez e, se não vier, segue para o próximo.
	var bruto []byte
	for tentativa := 0; tentativa < 2; tentativa++ {
		bruto, err = k.d.LCU.Historico(ctx, inv.Puuid, 0, 20)
		if err == nil || errors.Is(err, lcu.ErrNaoEncontrado) || paraTudo(ctx, err) {
			break
		}
	}
	if errors.Is(err, lcu.ErrNaoEncontrado) {
		return SemHistorico, info, nil
	}
	if err != nil {
		if paraTudo(ctx, err) {
			return "", nil, err
		}
		log.Printf("aprendizado: histórico de um candidato de %s: %v", elo, err)
		return SemHistorico, info, nil
	}
	hist, err := leHistorico(bruto, inv.Puuid)
	if err != nil {
		log.Printf("aprendizado: histórico de um candidato de %s em formato inesperado: %v", elo, err)
		return SemHistorico, info, nil
	}
	if len(hist) == 0 {
		return SemHistorico, info, nil
	}
	camp, fracao, nSolo := campeaoPrincipal(hist)
	info["campeao_id"], info["fracao"], info["partidas_solo"] = camp, fracao, nSolo
	mono := nSolo >= MonoMinPartidas && fracao >= MonoMinFracao
	// Na amostragem o mono não é exigido: o critério é só ter partida rastreável.
	exigeMonoAqui := exigeMono(elo) && !col.Amostragem
	if exigeMonoAqui && !mono {
		return NaoMono, info, nil
	}
	nomeCamp := ""
	if camp != 0 && k.d.Partida.Campeoes != nil {
		nomeCamp = k.d.Partida.Campeoes.Nome(ctx, camp)
	}
	info["campeao"] = nomeCamp
	exigido := 0
	if exigeMonoAqui {
		exigido = camp
	}
	funcao, fracFuncao := funcaoPrincipal(hist)
	// Na amostragem sem função pedida, qualquer posição serve.
	funcaoConta := funcao
	if col.Amostragem && col.Funcao == "" {
		funcaoConta = ""
	}
	vr, dr := contaRastreaveis(hist, patch, funcaoConta)
	info["funcao"], info["vitorias_rastreaveis"], info["derrotas_rastreaveis"] = funcao, vr, dr
	if col.Funcao != "" && funcao != col.Funcao {
		return DescOutraFuncao, info, nil
	}
	// Sem partida rastreável o jogador não ocupa vaga: a descoberta segue
	// descendo a lista (até MaxCandidatosPorElo). Só descobrir não escolhe
	// partida (não lê a partida inteira): vale ter vitória rastreável na função.
	// Na amostragem vale vitória ou derrota (o que se quer é alguém com partida
	// no patch), e a partida rastreada é a mais recente.
	var ps []PartidaDaColeta
	switch {
	case col.SoDescobrir && col.Amostragem:
		if vr+dr == 0 {
			return DescSemPartida, info, nil
		}
	case col.SoDescobrir:
		if vr == 0 {
			return DescSemPartida, info, nil
		}
	case col.Amostragem:
		ps, err = k.maisRecentes(ctx, col, hist, inv, patch)
		if err != nil {
			return "", nil, err
		}
		if len(ps) == 0 {
			return DescSemPartida, info, nil
		}
	default:
		ps, err = k.escolhePartidas(ctx, col, hist, inv, patch, exigido)
		if err != nil {
			return "", nil, err
		}
		if len(ps) == 0 {
			return DescSemPartida, info, nil
		}
	}
	j := JogadorDaColeta{Regiao: col.Regiao, Elo: elo, Posicao: posicao, LP: e.LP, Nome: conta.Nome, Tag: conta.Tag,
		CampeaoID: camp, Campeao: nomeCamp, FracaoCampeao: math.Round(fracao*1000) / 1000, PartidasSolo: nSolo, Mono: mono,
		Estado: JogadorComPartidas, puuidRiot: e.Puuid, puuidLCU: inv.Puuid,
		Divisao: e.Rank, Vitorias: e.Vitorias, Derrotas: e.Derrotas, FreshBlood: e.FreshBlood, Veteran: e.Veteran,
		HotStreak: e.HotStreak, PontuacaoSubida: PontuacaoSubida(e),
		Funcao: funcao, FracaoFuncao: math.Round(fracFuncao*1000) / 1000, VitoriasRastreaveis: vr, DerrotasRastreaveis: dr}
	if n := e.Vitorias + e.Derrotas; n > 0 {
		j.TaxaVitoria = math.Round(float64(e.Vitorias)/float64(n)*1000) / 1000
	}
	if riot.EloApex(elo) {
		j.Divisao = ""
	}
	if _, n, err := gravaJogador(k.d.DB, col.ID, j, ps, col.Quem); err != nil {
		return "", nil, err
	} else {
		info["partidas"] = n
	}
	return Aceito, info, nil
}

// paraTudo: erro que para a coleta (cliente fechado, rota recusada, contexto)
// e não só o candidato.
func paraTudo(ctx context.Context, err error) bool {
	var proibida *lcu.ErroRotaProibida
	return ctx.Err() != nil || errors.Is(err, lcu.ErrClienteFechado) || errors.As(err, &proibida)
}

// itemHist é o que a descoberta usa de cada partida do histórico do LCU (só o
// participante dono vem no histórico).
type itemHist struct {
	GameID    int64
	CriadaMs  int64
	Mapa      int
	Posicao   string // pela ordem do participante (partida.PosicaoDoParticipante)
	Fila      int
	Versao    string
	DuracaoS  int
	CampeaoID int
	Venceu    bool
	Rendicao  bool
	Abates    int
	Mortes    int
	Assist    int
	CS        int
	Dano      int64
}

func leHistorico(bruto []byte, puuid string) ([]itemHist, error) {
	var h struct {
		Games struct {
			Games []struct {
				GameID       int64  `json:"gameId"`
				GameCreation int64  `json:"gameCreation"`
				MapID        int    `json:"mapId"`
				QueueID      int    `json:"queueId"`
				GameVersion  string `json:"gameVersion"`
				GameDuration int    `json:"gameDuration"`
				Participants []struct {
					ParticipantID int `json:"participantId"`
					ChampionID    int `json:"championId"`
					Stats         struct {
						Win                       bool  `json:"win"`
						Kills                     int   `json:"kills"`
						Deaths                    int   `json:"deaths"`
						Assists                   int   `json:"assists"`
						TotalMinionsKilled        int   `json:"totalMinionsKilled"`
						NeutralMinionsKilled      int   `json:"neutralMinionsKilled"`
						TotalDamageToChampions    int64 `json:"totalDamageDealtToChampions"`
						GameEndedInEarlySurrender bool  `json:"gameEndedInEarlySurrender"`
						TeamEarlySurrendered      bool  `json:"teamEarlySurrendered"`
						CausedEarlySurrender      bool  `json:"causedEarlySurrender"`
						EarlySurrenderAccomplice  bool  `json:"earlySurrenderAccomplice"`
					} `json:"stats"`
				} `json:"participants"`
				ParticipantIdentities []struct {
					ParticipantID int `json:"participantId"`
					Player        struct {
						Puuid string `json:"puuid"`
					} `json:"player"`
				} `json:"participantIdentities"`
			} `json:"games"`
		} `json:"games"`
	}
	if err := json.Unmarshal(bruto, &h); err != nil {
		return nil, err
	}
	var l []itemHist
	for _, g := range h.Games.Games {
		if len(g.Participants) == 0 {
			continue
		}
		// O dono: pela identidade; sem ela, o único participante do item.
		idx := 0
		for _, pi := range g.ParticipantIdentities {
			if pi.Player.Puuid == puuid {
				for i, p := range g.Participants {
					if p.ParticipantID == pi.ParticipantID {
						idx = i
					}
				}
			}
		}
		p := g.Participants[idx]
		s := p.Stats
		l = append(l, itemHist{GameID: g.GameID, CriadaMs: g.GameCreation, Mapa: g.MapID,
			Posicao: partida.PosicaoDoParticipante(g.MapID, p.ParticipantID), Fila: g.QueueID, Versao: g.GameVersion, DuracaoS: g.GameDuration,
			CampeaoID: p.ChampionID, Venceu: s.Win, Rendicao: s.GameEndedInEarlySurrender || s.TeamEarlySurrendered,
			Abates: s.Kills, Mortes: s.Deaths, Assist: s.Assists, CS: s.TotalMinionsKilled + s.NeutralMinionsKilled,
			Dano: s.TotalDamageToChampions})
	}
	return l, nil
}

// campeaoPrincipal: o campeão mais jogado nas ranqueadas solo do histórico, a
// fração e quantas ranqueadas solo havia.
func campeaoPrincipal(hist []itemHist) (campeao int, fracao float64, n int) {
	cont := map[int]int{}
	for _, h := range hist {
		if h.Fila == FilaSolo {
			cont[h.CampeaoID]++
			n++
		}
	}
	melhor := 0
	for c, q := range cont {
		if q > melhor || (q == melhor && c < campeao) {
			campeao, melhor = c, q
		}
	}
	if n > 0 {
		fracao = float64(melhor) / float64(n)
	}
	return
}

// funcaoPrincipal: a posição mais jogada nas ranqueadas solo do histórico e a
// fração ("" se nenhuma tinha posição conhecida).
func funcaoPrincipal(hist []itemHist) (string, float64) {
	cont := map[string]int{}
	n := 0
	for _, h := range hist {
		if h.Fila == FilaSolo && h.Posicao != "" {
			cont[h.Posicao]++
			n++
		}
	}
	melhor, qual := 0, ""
	for p, q := range cont {
		if q > melhor || (q == melhor && p < qual) {
			qual, melhor = p, q
		}
	}
	if n == 0 {
		return "", 0
	}
	return qual, float64(melhor) / float64(n)
}

// rastreavel: ranqueada solo do patch instalado (só ela tem replay que abre),
// 15 min ou mais, sem rendição antecipada. Vale para vitória e derrota.
func rastreavel(h itemHist, patch string) bool {
	if h.Fila != FilaSolo || h.DuracaoS < DuracaoMinimaS || h.Rendicao {
		return false
	}
	return patch == "" || lcu.PatchDe(h.Versao) == patch
}

// naFuncao: a partida foi na função pedida ("" = qualquer; posição
// desconhecida da partida também passa, para não esconder o que não se sabe).
func naFuncao(h itemHist, funcao string) bool {
	return funcao == "" || h.Posicao == "" || h.Posicao == funcao
}

// contaRastreaveis: vitórias e derrotas rastreáveis do histórico na função.
func contaRastreaveis(hist []itemHist, patch, funcao string) (v, d int) {
	for _, h := range hist {
		if !rastreavel(h, patch) || !naFuncao(h, funcao) {
			continue
		}
		if h.Venceu {
			v++
		} else {
			d++
		}
	}
	return
}

// Pontuacao de uma partida, 0–100 (contrato-http.md): 35% KDA (teto 10), 25%
// participação em abates, 20% CS/min (teto 10), 20% dano a campeões/min (teto
// 1500). kp < 0 = participação desconhecida (vale 0,5 na pré-escolha).
func Pontuacao(abates, mortes, assist, cs int, dano int64, duracaoS int, kp float64) float64 {
	kda := float64(abates+assist) / float64(max(1, mortes))
	min_ := max(1, float64(duracaoS)/60)
	if kp < 0 {
		kp = 0.5
	}
	v := 0.35*math.Min(kda, 10)/10 + 0.25*math.Min(kp, 1) + 0.2*math.Min(float64(cs)/min_, 10)/10 +
		0.2*math.Min(float64(dano)/min_, 1500)/1500
	return math.Round(v*1000) / 10
}

// escolhePartidas: fila 420, patch instalado, vitória, ≥ 15 min, sem rendição
// antecipada e, se campeao != 0, nele. As melhores pela pontuação (com a
// participação em abates lida da partida inteira, games/{id}).
func (k *Coletor) escolhePartidas(ctx context.Context, col Coleta, hist []itemHist, inv lcu.Invocador, patch string, campeao int) ([]PartidaDaColeta, error) {
	var cands []itemHist
	for _, h := range hist {
		if !h.Venceu || !rastreavel(h, patch) {
			continue
		}
		if campeao != 0 && h.CampeaoID != campeao {
			continue
		}
		cands = append(cands, h)
	}
	l, err := k.melhoresVitorias(ctx, col.Regiao, cands, inv, "")
	if err != nil {
		return nil, err
	}
	if len(l) > col.PartidasPorJogador {
		l = l[:col.PartidasPorJogador]
	}
	return l, nil
}

// maisRecentes (amostragem): as partidas_por_jogador ranqueadas solo
// rastreáveis mais recentes, vitória ou derrota, na função pedida (se houver).
func (k *Coletor) maisRecentes(ctx context.Context, col Coleta, hist []itemHist, inv lcu.Invocador, patch string) ([]PartidaDaColeta, error) {
	var cands []itemHist
	for _, h := range hist {
		if rastreavel(h, patch) && naFuncao(h, col.Funcao) {
			cands = append(cands, h)
		}
	}
	sort.SliceStable(cands, func(a, b int) bool { return cands[a].CriadaMs > cands[b].CriadaMs })
	return k.partidasDoDono(ctx, col.Regiao, cands, inv, col.Funcao, max(1, col.PartidasPorJogador))
}

// melhoresVitorias pré-ordena as vitórias candidatas pela pontuação do
// histórico (participação desconhecida), lê as MaxPartidasAnalisar primeiras
// inteiras e devolve na ordem da pontuação de verdade. É a regra da coleta; o
// estudo criado a partir de um jogador usa a mesma.
func (k *Coletor) melhoresVitorias(ctx context.Context, regiao string, cands []itemHist, inv lcu.Invocador, funcao string) ([]PartidaDaColeta, error) {
	cands = append([]itemHist(nil), cands...)
	sort.SliceStable(cands, func(a, b int) bool {
		return Pontuacao(cands[a].Abates, cands[a].Mortes, cands[a].Assist, cands[a].CS, cands[a].Dano, cands[a].DuracaoS, -1) >
			Pontuacao(cands[b].Abates, cands[b].Mortes, cands[b].Assist, cands[b].CS, cands[b].Dano, cands[b].DuracaoS, -1)
	})
	if len(cands) > MaxPartidasAnalisar {
		cands = cands[:MaxPartidasAnalisar]
	}
	l, err := k.partidasDoDono(ctx, regiao, cands, inv, funcao, 0)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(l, func(a, b int) bool { return l[a].Pontuacao > l[b].Pontuacao })
	return l, nil
}

// partidasDoDono lê cada partida inteira (games/{id}) na ordem dada e monta a
// partida do ponto de vista do dono, com a pontuação. Pula a que não veio
// completa e, com funcao != "", a que o dono jogou em outra posição. limite >
// 0 para depois de achar tantas.
func (k *Coletor) partidasDoDono(ctx context.Context, regiao string, hs []itemHist, inv lcu.Invocador, funcao string, limite int) ([]PartidaDaColeta, error) {
	var l []PartidaDaColeta
	for _, h := range hs {
		if limite > 0 && len(l) >= limite {
			break
		}
		p, err := partida.LePartida(ctx, k.d.Partida, h.GameID)
		if errors.Is(err, lcu.ErrClienteFechado) || ctx.Err() != nil {
			return nil, err
		}
		if err != nil || !p.Completa {
			log.Printf("aprendizado: partida %d sem os 10 participantes: %v", h.GameID, err)
			continue
		}
		var dono *partida.Participante
		for i := range p.Participantes {
			pt := &p.Participantes[i]
			if pt.Puuid == inv.Puuid || (dono == nil && strings.EqualFold(pt.Nome, inv.Nome) && strings.EqualFold(pt.Tag, inv.Tag)) {
				dono = pt
			}
		}
		if dono == nil || (funcao != "" && dono.Posicao != "" && dono.Posicao != funcao) {
			continue
		}
		abatesTime := 0
		for _, pt := range p.Participantes {
			if pt.Time == dono.Time {
				abatesTime += pt.Abates
			}
		}
		kp := 0.0
		if abatesTime > 0 {
			kp = float64(dono.Abates+dono.Assistencias) / float64(abatesTime)
		}
		l = append(l, PartidaDaColeta{Regiao: regiao, GameID: h.GameID, ParticipanteID: dono.ParticipanteID,
			CampeaoID: dono.CampeaoID, Campeao: dono.Campeao, Abates: dono.Abates, Mortes: dono.Mortes,
			Assistencias: dono.Assistencias, DuracaoS: p.DuracaoS, posicao: dono.Posicao, venceu: dono.Venceu,
			Pontuacao: Pontuacao(dono.Abates, dono.Mortes, dono.Assistencias, dono.CS, dono.DanoCampeoes, p.DuracaoS, kp)})
	}
	return l, nil
}
