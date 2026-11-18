package analise

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"

	"loltracking/server/banco"
	"loltracking/server/partida"
	"loltracking/server/rastreio"
)

// Onde as ondas entram: um gerador da cadeia (o mesmo padrão dos outros da
// análise) põe os trechos da rota do jogador e as ondas que ele perdeu na
// cadeia de cada jogador de rota; POST /api/partidas/{id}/analise refaz tudo
// para as partidas já rastreadas, sem rastrear de novo. A rota GET .../ondas
// calcula na hora, com a MESMA função (DeduzOndas), o que a tela desenha.

// ExtraSaidaTropas: chave de ContextoCadeia.Extra com o instante (ms) do
// evento MinionsSpawning do jogo (int64), quando gravado.
const ExtraSaidaTropas = "saida_tropas_ms"

func init() {
	rastreio.RegistraGerador("analise_ondas", geraOndas)
	rastreio.RegistraContexto("analise_ondas", func(db *banco.DB, c *rastreio.ContextoCadeia) {
		if t, ok := saidaDasTropas(db, c.GameID); ok {
			if c.Extra == nil {
				c.Extra = map[string]any{}
			}
			c.Extra[ExtraSaidaTropas] = t
		}
	})
}

// saidaDasTropas: o instante do evento MinionsSpawning gravado (o jogo manda
// um por jogador assistido; todos no mesmo instante).
func saidaDasTropas(db *banco.DB, gameID int64) (int64, bool) {
	var t sql.NullInt64
	if err := db.QueryRow(`SELECT MIN(t_ms) FROM eventos WHERE game_id = ? AND tipo = 'minionsspawning'`, gameID).Scan(&t); err != nil || !t.Valid {
		return 0, false
	}
	return t.Int64, true
}

// entradaDoContexto monta a entrada das ondas a partir do contexto da cadeia:
// o jogador seguido e os outros rastreados.
func entradaDoContexto(p *preparo) EntradaOndas {
	c := p.c
	in := EntradaOndas{Times: c.Times, Posicoes: c.Posicoes, Amostras: map[int][]rastreio.Amostra{}, Eventos: p.eventos, Linha: c.Linha}
	for pid, as := range c.Outros {
		if len(as) > 0 {
			in.Amostras[pid] = as
		}
	}
	if len(c.Amostras) > 0 {
		in.Amostras[c.ParticipanteID] = c.Amostras
	}
	if t, ok := c.Extra[ExtraSaidaTropas].(int64); ok {
		in.SaidaTropasMs, in.SaidaMedida = t, true
	}
	return in
}

// ondas do contexto, calculadas uma vez por contexto.
func (p *preparo) ondasDaPartida() OndasDaPartida {
	if p.ondas == nil {
		o := DeduzOndas(entradaDoContexto(p))
		p.ondas = &o
	}
	return *p.ondas
}

// geraOndas: os trechos da rota do jogador (com "a_favor" do ponto de vista
// dele) e as ondas que ele perdeu. Caçador não tem rota: nada.
func geraOndas(c *rastreio.ContextoCadeia) []rastreio.ItemCadeia {
	p := prepara(c)
	if p.rota == "" || c.Times == nil {
		return nil
	}
	od := p.ondasDaPartida()
	var itens []rastreio.ItemCadeia
	for _, r := range od.Rotas {
		if r.Rota != p.rota {
			continue
		}
		for _, t := range r.Estados {
			d := map[string]any{
				"rota": r.Rota, "fim_ms": t.AteMs, "duracao_ms": t.AteMs - t.DeMs, "ondas": t.Ondas, "time": t.Time,
				"confianca": t.Confianca, "s": t.S, "x": t.X, "y": t.Y, "sinais": t.Sinais, "fase_rotas": t.DeMs < od.FimFaseRotasMs,
			}
			if t.Time != 0 {
				// A favor: freeze do próprio time, onda batendo na torre do outro,
				// voltando da torre do outro, o próprio time empurrando.
				meu := t.Time == c.Time
				if t.Estado == EOndaNaTorre || t.Estado == EOndaVoltando {
					meu = !meu
				}
				d["a_favor"] = meu
			}
			if t.Velocidade != "" {
				d["velocidade"] = t.Velocidade
				d["avanco_por_onda"] = t.Avanco
			}
			itens = append(itens, rastreio.ItemCadeia{TMs: t.DeMs, Tipo: t.Estado, Dados: d})
		}
		for _, op := range r.Perdidas {
			if op.ParticipanteID != c.ParticipanteID {
				continue
			}
			itens = append(itens, rastreio.ItemCadeia{TMs: op.TMs, Tipo: EOndaPerdida, Dados: map[string]any{
				"rota": r.Rota, "onda": op.Onda, "canhao": op.Canhao, "motivo": op.Motivo, "zona": op.Zona, "distancia": op.Distancia,
				"onda_s": op.OndaS, "posicao_onda": op.PosicaoOnda, "x": op.X, "y": op.Y}})
		}
	}
	return itens
}

// indicadoresOndas: tempo em freeze a favor/contra, crashes na torre inimiga e
// ondas perdidas, pelos itens de onda da cadeia do jogador (fase de rotas).
// Todos aproximados: a onda é estimada pela posição dos jogadores.
func (d *dadosPartida) indicadoresOndas(c *coleta, cadeia []rastreio.ItemCadeia, posicao string, det map[string]any) {
	var aFavor, contra float64
	var msFavor, msContra, crashes, perdidas []Momento
	for _, it := range cadeia {
		dd := it.Dados
		if fr, _ := dd["fase_rotas"].(bool); !fr && it.Tipo != EOndaPerdida {
			continue
		}
		favor, _ := dd["a_favor"].(bool)
		switch it.Tipo {
		case EOndaFreeze:
			dur := numero(dd["duracao_ms"]) / 1000
			m := Momento{it.TMs, fmt.Sprintf("freeze %s aos %s (%.0f s)", map[bool]string{true: "a favor", false: "contra"}[favor], MinSeg(it.TMs), dur)}
			if favor {
				aFavor += dur
				msFavor = append(msFavor, m)
			} else {
				contra += dur
				msContra = append(msContra, m)
			}
		case EOndaNaTorre:
			if favor {
				crashes = append(crashes, Momento{it.TMs, fmt.Sprintf("onda batendo na torre inimiga aos %s", MinSeg(it.TMs))})
			}
		case EOndaPerdida:
			onde := map[string]string{"morto": "morto", "na_base": "na base"}[fmt.Sprint(dd["motivo"])]
			if onde == "" {
				z, _ := dd["zona"].(string)
				onde = fmt.Sprintf("longe (%s, a %.0f da onda)", Info(z).Nome, numero(dd["distancia"]))
			}
			can := ""
			if b, _ := dd["canhao"].(bool); b {
				can = " com canhão"
			}
			perdidas = append(perdidas, Momento{it.TMs, fmt.Sprintf("perdeu a onda %.0f%s aos %s: %s", numero(dd["onda"]), can, MinSeg(it.TMs), onde)})
		}
	}
	if msFavor == nil {
		msFavor = []Momento{}
	}
	if msContra == nil {
		msContra = []Momento{}
	}
	c.poe("freeze_a_favor_s", fp(aFavor), true, map[string]any{"momentos": msFavor, "cobertura_ms": det["cobertura_ms"]})
	c.poe("freeze_contra_s", fp(contra), true, map[string]any{"momentos": msContra, "cobertura_ms": det["cobertura_ms"]})
	c.conta("ondas_na_torre_inimiga_n", crashes, true, det)
	if posicao != "UTILITY" {
		c.conta("ondas_perdidas_rotas_n", perdidas, true, det)
	}
}

// LeOndas calcula as ondas de uma partida pelo que está guardado (não grava
// nada e não depende de a análise ter rodado): os rastreados (melhor passe de
// cada um), a linha do tempo do LCU, as placas da Riot (se guardada) e a saída
// das tropas.
func LeOndas(db *banco.DB, gameID int64) (OndasDaPartida, error) {
	pt, err := partida.Le(db, gameID)
	if err != nil {
		return OndasDaPartida{}, err
	}
	bruto, err := partida.LeLinhaDoTempo(db, gameID)
	if errors.Is(err, partida.ErrNaoEncontrada) {
		return OndasDaPartida{}, ErrSemLinhaDoTempo
	}
	if err != nil {
		return OndasDaPartida{}, err
	}
	lt, err := rastreio.LeLinhaDoTempo(bruto)
	if err != nil {
		return OndasDaPartida{}, err
	}
	in := EntradaOndas{Times: map[int]int{}, Posicoes: map[int]string{}, Amostras: map[int][]rastreio.Amostra{}, Linha: lt}
	for _, part := range pt.Participantes {
		in.Times[part.ParticipanteID], in.Posicoes[part.ParticipanteID] = part.Time, part.Posicao
		ps, ok, err := rastreio.MelhorPasse(db, gameID, part.ParticipanteID)
		if err != nil {
			return OndasDaPartida{}, fmt.Errorf("ler o passe do participante %d: %w", part.ParticipanteID, err)
		}
		if !ok {
			continue
		}
		if as, err := rastreio.AmostrasDoPasse(db, ps); err == nil && len(as) > 0 {
			in.Amostras[part.ParticipanteID] = as
		}
	}
	in.Eventos = append(in.Eventos, lt.Eventos...)
	if rb, ok, err := LeLinhaRiot(db, gameID); err != nil {
		return OndasDaPartida{}, err
	} else if ok {
		for _, e := range eventosRiot(rb) {
			if e.Tipo == "TURRET_PLATE_DESTROYED" {
				in.Eventos = append(in.Eventos, e)
			}
		}
	}
	sort.SliceStable(in.Eventos, func(a, b int) bool { return in.Eventos[a].TMs < in.Eventos[b].TMs })
	if t, ok := saidaDasTropas(db, gameID); ok {
		in.SaidaTropasMs, in.SaidaMedida = t, true
	}
	return DeduzOndas(in), nil
}
