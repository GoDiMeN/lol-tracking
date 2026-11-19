package aprendizado

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"math"
	"sort"
	"time"

	"loltracking/server/banco"
	"loltracking/server/lcu"
	"loltracking/server/partida"
	"loltracking/server/riot"
)

// APROVEITAMENTO ("aproveitar": true, o padrão). Quando a coleta escolhe a
// partida de um jogador, ela confere os OUTROS 9 daquela partida: quem também
// serve de referência (elo pedido pela coleta, o mesmo critério do modo em uso
// e a rota com vaga na cota) entra como jogador da coleta, com a origem
// marcada, e é rastreado na MESMA abertura do replay (um passe a mais no
// mesmo rastreio: coletor.go, rodaPartida), sem baixar nem abrir de novo.
//
// A ESCOLHA da partida também leva isso em conta: das partidas do jogador que
// passam nas regras, as PartidasConferidasAproveitamento de maior pontuação
// têm os aproveitáveis contados, e a pontuação da partida passa a ser
// pontuacao_jogador + PesoAproveitavel × aproveitaveis (os dois componentes
// ficam gravados). Quando há várias partidas com aproveitáveis, a coleta pega
// até MaxPartidasComAproveitaveis delas (ou partidas_por_jogador, se maior).
//
// Custo na Riot: o puuid da partida é o do LCU, que a Riot não aceita, então o
// elo de cada um dos outros sai de 2 pedidos (account-v1 pelo Riot ID e
// league-v4 por puuid), pelo mesmo limitador do cliente. Antes de perguntar,
// tira quem é de rota sem vaga em nenhum elo pedido; a resposta fica em
// aprendizado_elos_vistos (por puuid do LCU) por ValidadeEloVisto, e a
// conferência de uma partida para ao achar MaxAproveitadosPorPartida.
// Pior caso por jogador: PartidasConferidasAproveitamento × 9 × 2 = 72
// pedidos (o cache e o corte por rota descem muito isso na prática).

// Regras do aproveitamento (var: os testes ajustam).
var (
	// MaxAproveitadosPorPartida: teto de jogadores aproveitados (ou marcados)
	// por partida conferida.
	MaxAproveitadosPorPartida = 2
	// PartidasConferidasAproveitamento: das partidas candidatas do jogador, só
	// as N de maior pontuação têm os outros 9 conferidos.
	PartidasConferidasAproveitamento = 4
	// MaxPartidasComAproveitaveis: com várias partidas boas por terem
	// aproveitáveis, até tantas por jogador (ou partidas_por_jogador, se maior).
	MaxPartidasComAproveitaveis = 3
)

const (
	// PesoAproveitavel: pontos (na escala 0–100 de Pontuacao) por jogador
	// aproveitável na partida: um aproveitável vale mais que a diferença
	// típica entre duas vitórias boas do mesmo jogador.
	PesoAproveitavel = 10.0
	// CorteSubidaAproveitado: no modo "subindo", o aproveitado precisa passar
	// nos filtros da liga (subida.go) e ter pontuação de subida a partir disto
	// (Wilson ~0,50: ganha mais do que perde com folga estatística).
	CorteSubidaAproveitado = 0.50
	// ValidadeEloVisto: o elo consultado de um dos 9 vale um dia.
	ValidadeEloVisto = 24 * time.Hour
	// OrigemAproveitado é a origem do jogador aproveitado.
	OrigemAproveitado = "aproveitado"
)

func migraElosVistos(db *banco.DB) error {
	return banco.Executa(db, `CREATE TABLE IF NOT EXISTS aprendizado_elos_vistos (
		puuid_lcu     TEXT PRIMARY KEY,
		nome          TEXT NOT NULL DEFAULT '',
		tag           TEXT NOT NULL DEFAULT '',
		puuid_riot    TEXT NOT NULL DEFAULT '',
		tem_solo      INTEGER NOT NULL DEFAULT 0,
		elo           TEXT NOT NULL DEFAULT '',
		divisao       TEXT NOT NULL DEFAULT '',
		lp            INTEGER NOT NULL DEFAULT 0,
		vitorias      INTEGER NOT NULL DEFAULT 0,
		derrotas      INTEGER NOT NULL DEFAULT 0,
		fresh_blood   INTEGER NOT NULL DEFAULT 0,
		veteran       INTEGER NOT NULL DEFAULT 0,
		hot_streak    INTEGER NOT NULL DEFAULT 0,
		inactive      INTEGER NOT NULL DEFAULT 0,
		consultado_em TEXT NOT NULL
	)`)
}

func contemInt(l []int, v int) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}

func contemStr(l []string, v string) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}

// eloDoParticipante: o elo da fila solo de um participante, do cache ou da
// Riot. Erro só quando a coleta precisa parar (chave, contexto).
func (k *Coletor) eloDoParticipante(ctx context.Context, pt partida.Participante) (riot.EntradaLiga, bool, error) {
	var e riot.EntradaLiga
	var tem, fresh, vet, hot, ina int
	var quando string
	err := k.d.DB.QueryRow(`SELECT puuid_riot, tem_solo, elo, divisao, lp, vitorias, derrotas, fresh_blood, veteran, hot_streak, inactive,
		consultado_em FROM aprendizado_elos_vistos WHERE puuid_lcu = ?`, pt.Puuid).Scan(&e.Puuid, &tem, &e.Tier, &e.Rank, &e.LP,
		&e.Vitorias, &e.Derrotas, &fresh, &vet, &hot, &ina, &quando)
	if err == nil {
		if t, perr := time.Parse("2006-01-02T15:04:05Z", quando); perr == nil && time.Since(t) < ValidadeEloVisto {
			e.FreshBlood, e.Veteran, e.HotStreak, e.Inactive = fresh == 1, vet == 1, hot == 1, ina == 1
			return e, tem == 1, nil
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return e, false, err
	}
	if pt.Puuid == "" || pt.Nome == "" || pt.Tag == "" {
		return e, false, nil
	}
	ent, ok, err := k.d.Riot.EloSoloPorRiotID(ctx, pt.Nome, pt.Tag)
	switch {
	case err == nil:
	case errors.Is(err, riot.ErrNaoEncontradoRiot):
		ok = false
	case errors.Is(err, riot.ErrSemChave) || errors.Is(err, riot.ErrChaveRecusada) || ctx.Err() != nil:
		return e, false, err
	default:
		// Erro de rede num dos 9 não para a coleta: ele só não é aproveitado.
		log.Printf("aprendizado: elo de um participante da partida %d: %v", pt.GameID, err)
		return e, false, nil
	}
	if !ok {
		ent = riot.EntradaLiga{}
	}
	k.d.DB.Exec(`INSERT INTO aprendizado_elos_vistos (puuid_lcu, nome, tag, puuid_riot, tem_solo, elo, divisao, lp, vitorias, derrotas,
		fresh_blood, veteran, hot_streak, inactive, consultado_em) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT (puuid_lcu) DO UPDATE SET nome = excluded.nome, tag = excluded.tag, puuid_riot = excluded.puuid_riot,
		tem_solo = excluded.tem_solo, elo = excluded.elo, divisao = excluded.divisao, lp = excluded.lp, vitorias = excluded.vitorias,
		derrotas = excluded.derrotas, fresh_blood = excluded.fresh_blood, veteran = excluded.veteran, hot_streak = excluded.hot_streak,
		inactive = excluded.inactive, consultado_em = excluded.consultado_em`,
		pt.Puuid, pt.Nome, pt.Tag, ent.Puuid, b2i(ok), ent.Tier, ent.Rank, ent.LP, ent.Vitorias, ent.Derrotas, b2i(ent.FreshBlood),
		b2i(ent.Veteran), b2i(ent.HotStreak), b2i(ent.Inactive), banco.Agora())
	return ent, ok, nil
}

// passaSubida: o critério do modo "subindo" para um aproveitado.
func passaSubida(e riot.EntradaLiga) bool {
	l, _ := ordenaCandidatos([]riot.EntradaLiga{e}, riot.EloApex(e.Tier))
	return len(l) == 1 && PontuacaoSubida(e) >= CorteSubidaAproveitado
}

// aproveitavel é um dos outros 9 que serve. jaJogador > 0: já é jogador da
// coleta (só marca a partida para ele, sem pergunta à Riot).
type aproveitavel struct {
	pt        partida.Participante
	ent       riot.EntradaLiga
	jaJogador int64
	mono      bool
	camp      int
	fracao    float64
	nSolo     int
}

// algumaVaga: alguma das rotas dos elos pedidos ainda cabe esta função.
func (k *Coletor) algumaVaga(col Coleta, funcao string) bool {
	for _, e := range col.Elos {
		if vaga(k.d.DB, col, e, funcao) == "" {
			return true
		}
	}
	return false
}

// aproveitaveisDa confere os outros participantes da partida (já no banco: a
// escolha a leu inteira) e devolve os que servem, até MaxAproveitadosPorPartida.
func (k *Coletor) aproveitaveisDa(ctx context.Context, col Coleta, gameID int64, donoPID int) ([]aproveitavel, error) {
	p, err := partida.Le(k.d.DB, gameID)
	if err != nil {
		return nil, nil
	}
	var l []aproveitavel
	for _, pt := range p.Participantes {
		if len(l) >= MaxAproveitadosPorPartida {
			break
		}
		if pt.ParticipanteID == donoPID || pt.Posicao == "" || pt.Puuid == "" {
			continue
		}
		var n int
		k.d.DB.QueryRow(`SELECT COUNT(*) FROM aprendizado_partidas WHERE coleta_id = ? AND game_id = ? AND participante_id = ?`,
			col.ID, gameID, pt.ParticipanteID).Scan(&n)
		if n > 0 {
			continue // nunca dois passes do mesmo jogador na mesma partida
		}
		var jid int64
		if err := k.d.DB.QueryRow(`SELECT id FROM aprendizado_jogadores WHERE coleta_id = ? AND puuid_lcu = ? ORDER BY id LIMIT 1`,
			col.ID, pt.Puuid).Scan(&jid); err == nil {
			l = append(l, aproveitavel{pt: pt, jaJogador: jid})
			continue
		}
		// Antes de gastar pedido à Riot: a rota dele tem vaga em algum elo pedido?
		if !k.algumaVaga(col, pt.Posicao) {
			continue
		}
		ent, tem, err := k.eloDoParticipante(ctx, pt)
		if err != nil {
			return nil, err
		}
		if !tem || !contemStr(col.Elos, ent.Tier) || vaga(k.d.DB, col, ent.Tier, pt.Posicao) != "" {
			continue
		}
		a := aproveitavel{pt: pt, ent: ent}
		if !col.Amostragem {
			if !passaSubida(ent) {
				continue
			}
			if exigeMono(ent.Tier) {
				// Do Mestre para cima a coleta quer mono champion: o histórico dele
				// (rota permitida do LCU) diz, e ele precisa estar no campeão dele.
				bruto, err := k.d.LCU.Historico(ctx, pt.Puuid, 0, 20)
				if err != nil {
					if paraTudo(ctx, err) {
						return nil, err
					}
					continue
				}
				hist, err := leHistorico(bruto, pt.Puuid)
				if err != nil {
					continue
				}
				a.camp, a.fracao, a.nSolo = campeaoPrincipal(hist)
				a.mono = a.nSolo >= MonoMinPartidas && a.fracao >= MonoMinFracao
				if !a.mono || a.camp != pt.CampeaoID {
					continue
				}
			}
		}
		l = append(l, a)
	}
	return l, nil
}

// escolheComAproveitaveis põe o termo dos aproveitáveis na pontuação das
// partidas candidatas (já em ordem de pontuação do jogador) e corta.
func (k *Coletor) escolheComAproveitaveis(ctx context.Context, col Coleta, ps []PartidaDaColeta) ([]PartidaDaColeta, error) {
	for i := range ps {
		ps[i].PontuacaoJogador = ps[i].Pontuacao
	}
	ppj := max(1, col.PartidasPorJogador)
	if !col.Aproveitar {
		return ps[:min(len(ps), ppj)], nil
	}
	for i := range ps {
		if i >= PartidasConferidasAproveitamento {
			break
		}
		l, err := k.aproveitaveisDa(ctx, col, ps[i].GameID, ps[i].ParticipanteID)
		if err != nil {
			return nil, err
		}
		ps[i].Aproveitaveis = len(l)
		ps[i].PontuacaoAproveitaveis = PesoAproveitavel * float64(len(l))
		ps[i].Pontuacao = math.Round((ps[i].PontuacaoJogador+ps[i].PontuacaoAproveitaveis)*10) / 10
	}
	sort.SliceStable(ps, func(a, b int) bool { return ps[a].Pontuacao > ps[b].Pontuacao })
	com := 0
	for _, p := range ps {
		if p.Aproveitaveis > 0 {
			com++
		}
	}
	n := max(ppj, min(com, MaxPartidasComAproveitaveis))
	return ps[:min(len(ps), n)], nil
}

// aproveitaDasPartidas grava, para cada partida escolhida do jogador dono, os
// outros que servem: jogador novo da coleta (origem aproveitado) com o passe
// nesta partida, ou só a partida para quem já é da coleta. Devolve quantos
// jogadores NOVOS entraram assim.
func (k *Coletor) aproveitaDasPartidas(ctx context.Context, col Coleta, dono JogadorDaColeta, ps []PartidaDaColeta, patch string) (int, error) {
	novos := 0
	for i, p := range ps {
		l, err := k.aproveitaveisDa(ctx, col, p.GameID, p.ParticipanteID)
		if err != nil {
			return novos, err
		}
		pt, err := partida.Le(k.d.DB, p.GameID)
		if err != nil {
			continue
		}
		for _, a := range l {
			pa := partidaDoParticipante(col.Regiao, pt, a.pt)
			if a.jaJogador > 0 {
				js, err := leJogadores(k.d.DB, `WHERE id = ?`, a.jaJogador)
				if err != nil || len(js) == 0 {
					continue
				}
				if err := gravaPartidas(k.d.DB, col.ID, js[0].ID, js[0], []PartidaDaColeta{pa}, col.Quem, i); err != nil {
					return novos, err
				}
				continue
			}
			// A vaga pode ter acabado com o aproveitado anterior (dois da mesma rota).
			if vaga(k.d.DB, col, a.ent.Tier, a.pt.Posicao) != "" {
				continue
			}
			j := JogadorDaColeta{Regiao: col.Regiao, Elo: a.ent.Tier, Posicao: dono.Posicao, LP: a.ent.LP, Nome: a.pt.Nome, Tag: a.pt.Tag,
				CampeaoID: a.pt.CampeaoID, Campeao: a.pt.Campeao, Mono: a.mono, PartidasSolo: a.nSolo,
				FracaoCampeao: math.Round(a.fracao*1000) / 1000, Estado: JogadorComPartidas, puuidRiot: a.ent.Puuid,
				puuidLCU: a.pt.Puuid, Divisao: a.ent.Rank, Vitorias: a.ent.Vitorias, Derrotas: a.ent.Derrotas,
				FreshBlood: a.ent.FreshBlood, Veteran: a.ent.Veteran, HotStreak: a.ent.HotStreak, PontuacaoSubida: PontuacaoSubida(a.ent),
				Funcao: a.pt.Posicao, Origem: OrigemAproveitado, OrigemGameID: p.GameID, OrigemJogadorID: dono.ID}
			if j.puuidRiot == "" {
				j.puuidRiot = "lcu:" + a.pt.Puuid
			}
			if n := j.Vitorias + j.Derrotas; n > 0 {
				j.TaxaVitoria = math.Round(float64(j.Vitorias)/float64(n)*1000) / 1000
			}
			if riot.EloApex(j.Elo) {
				j.Divisao = ""
			}
			jid, _, err := gravaJogador(k.d.DB, col.ID, j, []PartidaDaColeta{pa}, col.Quem)
			if err != nil {
				// O mesmo puuid da Riot já na coleta (UNIQUE): não é erro da coleta.
				log.Printf("aprendizado: aproveitar um jogador da partida %d: %v", p.GameID, err)
				continue
			}
			novos++
			// Partidas a mais dele só se partidas_por_jogador pedir (regra 2).
			if col.PartidasPorJogador > 1 {
				j.ID = jid
				if err := k.maisPartidasDoAproveitado(ctx, col, j, p.GameID, patch); err != nil {
					return novos, err
				}
			}
		}
	}
	return novos, nil
}

// partidaDoParticipante: a partida do ponto de vista de um dos outros 9.
// lcuInvocador: o jogador da coleta como o LCU o conhece.
func lcuInvocador(j JogadorDaColeta) lcu.Invocador {
	return lcu.Invocador{Puuid: j.puuidLCU, Nome: j.Nome, Tag: j.Tag}
}

func partidaDoParticipante(regiao string, p partida.Partida, pt partida.Participante) PartidaDaColeta {
	abatesTime := 0
	for _, x := range p.Participantes {
		if x.Time == pt.Time {
			abatesTime += x.Abates
		}
	}
	kp := 0.0
	if abatesTime > 0 {
		kp = float64(pt.Abates+pt.Assistencias) / float64(abatesTime)
	}
	pj := Pontuacao(pt.Abates, pt.Mortes, pt.Assistencias, pt.CS, pt.DanoCampeoes, p.DuracaoS, kp)
	return PartidaDaColeta{Regiao: regiao, GameID: p.GameID, ParticipanteID: pt.ParticipanteID, CampeaoID: pt.CampeaoID,
		Campeao: pt.Campeao, Abates: pt.Abates, Mortes: pt.Mortes, Assistencias: pt.Assistencias, DuracaoS: p.DuracaoS,
		Pontuacao: pj, PontuacaoJogador: pj, Aproveitado: true, posicao: pt.Posicao, venceu: pt.Venceu}
}

// gravaPartidas põe partidas de um jogador que já está na coleta.
func gravaPartidas(db *banco.DB, coletaID, jid int64, j JogadorDaColeta, ps []PartidaDaColeta, quem string, ordem int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, p := range ps {
		if _, err := gravaPartidaTx(tx, coletaID, jid, j, p, ordem+i, quem); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// maisPartidasDoAproveitado: com partidas_por_jogador > 1, as outras partidas
// dele pela regra do modo (sem aproveitar de novo a partir delas).
func (k *Coletor) maisPartidasDoAproveitado(ctx context.Context, col Coleta, j JogadorDaColeta, jaTem int64, patch string) error {
	bruto, err := k.d.LCU.Historico(ctx, j.puuidLCU, 0, 20)
	if err != nil {
		if paraTudo(ctx, err) {
			return err
		}
		return nil
	}
	hist, err := leHistorico(bruto, j.puuidLCU)
	if err != nil {
		return nil
	}
	var outros []itemHist
	for _, h := range hist {
		if h.GameID != jaTem {
			outros = append(outros, h)
		}
	}
	inv := lcuInvocador(j)
	semAprov := col
	semAprov.Aproveitar = false
	var ps []PartidaDaColeta
	if col.Amostragem {
		ps, err = k.maisRecentes(ctx, semAprov, outros, inv, patch, j.Funcao)
	} else {
		exigido := 0
		if exigeMono(j.Elo) {
			exigido = j.CampeaoID
		}
		ps, err = k.escolhePartidas(ctx, semAprov, outros, inv, patch, exigido, j.Funcao)
	}
	if err != nil {
		return err
	}
	if len(ps) > col.PartidasPorJogador-1 {
		ps = ps[:col.PartidasPorJogador-1]
	}
	for i := range ps {
		ps[i].PontuacaoJogador = ps[i].Pontuacao
	}
	return gravaPartidas(k.d.DB, col.ID, j.ID, j, ps, col.Quem, 1)
}
