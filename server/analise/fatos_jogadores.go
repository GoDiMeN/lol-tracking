package analise

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"

	"loltracking/server/banco"
)

// O resumo de cada jogador, a build lado a lado com o estado do jogo, a nota de
// impacto (com os componentes à mostra) e o contexto que o banco já tem dele.

// ComponenteNota é uma parcela da nota de impacto (ouro equivalente).
type ComponenteNota struct {
	Nome    string `json:"nome"`
	Rotulo  string `json:"rotulo"`
	Valor   int    `json:"valor"`
	Detalhe string `json:"detalhe"`
}

// ContextoJogador: quem o jogador era ANTES desta partida (fatos_historico.go),
// pelas até Param.PartidasContexto partidas de Summoner's Rift de antes dela
// (banco + histórico buscado). Campo vazio = não se sabe; Falta diz o quê e de
// onde viria. Informação ao lado: a nota de impacto não usa.
type ContextoJogador struct {
	// PartidasGuardadas: as partidas de antes usadas (no máximo PartidasContexto).
	PartidasGuardadas int            `json:"partidas_guardadas"`
	FuncoesGuardadas  map[string]int `json:"funcoes_guardadas"`
	FuncaoPrincipal   string         `json:"funcao_principal"`
	// FracaoPrincipal: a fração das partidas (com função conhecida) na principal.
	FracaoPrincipal float64 `json:"fracao_principal"`
	ForaDaFuncao    *bool   `json:"fora_da_funcao"`
	// Sequencia: nas partidas logo antes desta, +n vitórias seguidas ou −n
	// derrotas seguidas (0 = nenhuma).
	Sequencia int `json:"sequencia"`
	// Ultimas e VitoriasUltimas: as até PartidasContexto partidas de antes.
	Ultimas         int `json:"ultimas"`
	VitoriasUltimas int `json:"vitorias_ultimas"`
	// CampeaoVezes de CampeaoDe: das partidas de antes, quantas com o campeão
	// desta. Mono: em ≥ FracaoMono delas; SemOCampeao: em nenhuma (talvez a
	// primeira vez). Nulos com menos de MinPartidasContexto partidas.
	CampeaoVezes int   `json:"campeao_vezes"`
	CampeaoDe    int   `json:"campeao_de"`
	Mono         *bool `json:"mono"`
	SemOCampeao  *bool `json:"sem_o_campeao"`
	// Fonte: "banco", "cliente" ou "riot" (o histórico buscado; "+ banco" se o
	// banco somou partidas); BuscadoEm: quando foi buscado.
	Fonte     string `json:"fonte"`
	BuscadoEm string `json:"buscado_em,omitempty"`
	// Alcance: o histórico buscado não chega (ou chega só em parte) a antes
	// desta partida; nunca se usam partidas posteriores no lugar.
	Alcance string        `json:"alcance,omitempty"`
	Elo     *EloConhecido `json:"elo"`
	Falta   []string      `json:"falta"`
}

// EvidenciaBuild: a compra de um item completo e o estado do jogo naquele
// instante, lado a lado, sem julgamento.
type EvidenciaBuild struct {
	TMs       int64    `json:"t_ms"`
	ItemID    int      `json:"item_id"`
	Nome      string   `json:"nome"`
	Custo     *int     `json:"custo"`
	Atributos []string `json:"atributos"`
	// VantagemTime: ouro do time dele − do outro (quadros de minuto).
	VantagemTime   *int   `json:"vantagem_time"`
	OuroJogador    *int   `json:"ouro_jogador"`
	OuroAdversario *int   `json:"ouro_adversario"`
	Placar         string `json:"placar"` // abates/mortes/assistências dele até ali
	// InimigoMaisForte: o inimigo com mais ouro naquele instante.
	InimigoMaisForte *InimigoForte `json:"inimigo_mais_forte"`
	// DanoTimeInimigo: % do dano a campeões do time inimigo por tipo (partida
	// inteira: o LCU só dá o total do fim).
	DanoTimeInimigo map[string]int `json:"dano_time_inimigo"`
	// DanoNasMortes: o dano que ele recebeu nas mortes até ali, por tipo (Riot).
	DanoNasMortes *DanoRecebido `json:"dano_nas_mortes"`
}

// InimigoForte é o inimigo de mais ouro num instante.
type InimigoForte struct {
	ParticipanteID int    `json:"participante_id"`
	Campeao        string `json:"campeao"`
	Ouro           int    `json:"ouro"`
	Placar         string `json:"placar"`
	// Dano por tipo (% do dano a campeões dele na partida inteira).
	FisicoPct     int `json:"fisico_pct"`
	MagicoPct     int `json:"magico_pct"`
	VerdadeiroPct int `json:"verdadeiro_pct"`
}

// DanoRecebido: % do dano recebido por tipo, somado nas mortes.
type DanoRecebido struct {
	Mortes        int `json:"mortes"`
	FisicoPct     int `json:"fisico_pct"`
	MagicoPct     int `json:"magico_pct"`
	VerdadeiroPct int `json:"verdadeiro_pct"`
}

func pct(a, tot int64) int {
	if tot <= 0 {
		return 0
	}
	return int(math.Round(float64(a) / float64(tot) * 100))
}

func (f *fatos) jogadores(db *banco.DB) []ResumoJogador {
	var l []ResumoJogador
	for _, part := range f.dp.pt.Participantes {
		pid := part.ParticipanteID
		j := ResumoJogador{ParticipanteID: pid, Puuid: part.Puuid, Nome: part.Nome, Campeao: part.Campeao, Funcao: part.Posicao,
			Time: part.Time, Venceu: part.Venceu, Rastreado: len(f.dp.amostras[pid]) > 0, Abates: part.Abates, Mortes: part.Mortes,
			Assistencias: part.Assistencias, Adversario: f.dp.adversario(pid), Componentes: []ComponenteNota{},
			Etiquetas: map[string]map[string]int{}}
		j.KDA = math.Round(float64(part.Abates+part.Assistencias)/math.Max(1, float64(part.Mortes))*100) / 100
		j.OuroFim, j.CSFim = ip(part.Ouro), ip(part.CS)
		for _, mnt := range []int{10, 15} {
			q := f.dp.quadroEm(mnt)
			if q == nil {
				continue
			}
			eu, ok := q.Jogadores[pid]
			if !ok {
				continue
			}
			o, c := ip(eu.OuroTotal), ip(eu.CS)
			var dO, dC *int
			if adv, ok := q.Jogadores[j.Adversario]; ok && j.Adversario > 0 {
				dO, dC = ip(eu.OuroTotal-adv.OuroTotal), ip(eu.CS-adv.CS)
			}
			if mnt == 10 {
				j.Ouro10, j.CS10, j.DifOuro10, j.DifCS10 = o, c, dO, dC
			} else {
				j.Ouro15, j.CS15, j.DifOuro15, j.DifCS15 = o, c, dO, dC
			}
		}
		for _, rt := range f.retratos {
			if rt.Vitima != pid {
				continue
			}
			switch Fase(rt.TMs) {
			case "rotas":
				j.MortesRotas++
			case "meio":
				j.MortesMeio++
			default:
				j.MortesFinal++
			}
			if f.mortes[rt.N].cara {
				j.MortesCaras++
			}
		}
		for _, o := range f.objs {
			if o.time != part.Time {
				continue
			}
			j.ObjetivosTime++
			if contem(o.participantes, pid) {
				j.ObjetivosParticipou++
			}
		}
		for _, w := range f.dp.wards {
			// Só os tipos conhecidos (NomesSentinela): o UNDEFINED da Riot não é sentinela.
			if w.quem == pid && ContaComoSentinela(w.tipo) {
				j.Sentinelas++
				if w.tipo == TipoControle {
					j.SentinelasControle++
				}
			}
		}
		if len(f.trechos[pid]) > 0 {
			seg := func(m map[string]int64) map[string]int {
				r := map[string]int{}
				for a, ms := range m {
					if s := int((ms + 500) / 1000); s > 0 {
						r[a] = s
					}
				}
				return r
			}
			j.Etiquetas["total"] = seg(f.tempoPorAtividade(pid, 0, math.MaxInt64/2))
			for _, fs := range Fases {
				ini, fim := limitesFase(fs)
				j.Etiquetas[fs] = seg(f.tempoPorAtividade(pid, ini, fim))
			}
		}
		j.Itens = f.compras(pid)
		j.Build = f.evidenciasBuild(pid, j.Itens)
		j.Contexto = contextoDoJogador(db, f.dp.pt.GameID, part, f.dp.pt.CriadaEmMs)
		if j.Contexto.Elo != nil {
			j.Elo = j.Contexto.Elo.Elo
			if j.Contexto.Elo.Divisao != "" {
				j.Elo += " " + j.Contexto.Elo.Divisao
			}
		}
		l = append(l, j)
	}
	return l
}

// --- Itens -------------------------------------------------------------------------------

func (f *fatos) item(id int) (nome string, custo *int, completo bool) {
	if f.est == nil {
		return "", nil, false
	}
	it, ok := f.est.Itens[id]
	if !ok {
		return "", nil, false
	}
	c := it.Custo
	return it.Nome, &c, it.Custo >= Param.CustoItemCompleto && !ItensDeSuporte[id]
}

// compras: as compras do jogador na ordem. Com a linha do tempo da Riot,
// ITEM_PURCHASED (o instante exato; ITEM_UNDO desfaz a compra); sem ela, as
// mudanças do inventário no estado dos 10 (como a rota /build).
func (f *fatos) compras(pid int) []ItemComprado {
	l := []ItemComprado{}
	if len(f.dp.eventosRiot) > 0 {
		for _, e := range f.dp.eventosRiot {
			if e.Tipo != "ITEM_PURCHASED" && e.Tipo != "ITEM_UNDO" {
				continue
			}
			var b struct {
				Item   int `json:"itemId"`
				Quem   int `json:"participantId"`
				Antes  int `json:"beforeId"`
				Depois int `json:"afterId"`
			}
			json.Unmarshal(e.Bruto, &b)
			if b.Quem != pid {
				continue
			}
			if e.Tipo == "ITEM_UNDO" {
				// Desfez a compra de "beforeId": tira a última compra dele.
				for k := len(l) - 1; k >= 0; k-- {
					if b.Antes != 0 && l[k].ItemID == b.Antes {
						l = append(l[:k], l[k+1:]...)
						break
					}
				}
				continue
			}
			nome, custo, comp := f.item(b.Item)
			l = append(l, ItemComprado{TMs: e.TMs, ItemID: b.Item, Nome: nome, Custo: custo, Completo: comp})
		}
		return l
	}
	var ant map[int]int
	for _, e := range f.dp.estados[pid] {
		agora := map[int]int{}
		for _, s := range e.Slots {
			if s.ID != 0 {
				agora[s.ID]++
			}
		}
		if ant == nil {
			ant = map[int]int{}
		}
		var ids []int
		for id, n := range agora {
			for k := ant[id]; k < n; k++ {
				ids = append(ids, id)
			}
		}
		sort.Ints(ids)
		for _, id := range ids {
			nome, custo, comp := f.item(id)
			l = append(l, ItemComprado{TMs: e.TMs, ItemID: id, Nome: nome, Custo: custo, Completo: comp})
		}
		ant = agora
	}
	return l
}

// placarEm: abates/mortes/assistências do jogador até t (linha do tempo).
func (f *fatos) placarEm(pid int, t int64) string {
	k, d, a := 0, 0, 0
	for _, rt := range f.retratos {
		if rt.TMs > t {
			break
		}
		switch {
		case rt.Matador == pid:
			k++
		case rt.Vitima == pid:
			d++
		case contem(rt.Assistencias, pid):
			a++
		}
	}
	return fmt.Sprintf("%d/%d/%d", k, d, a)
}

func (f *fatos) evidenciasBuild(pid int, itens []ItemComprado) []EvidenciaBuild {
	l := []EvidenciaBuild{}
	tm := f.timeDe[pid]
	adv := f.dp.adversario(pid)
	// O dano a campeões do time inimigo, por tipo (partida inteira).
	var fis, mag, ver int64
	for _, p := range f.dp.pt.Participantes {
		if p.Time != tm {
			fis, mag, ver = fis+p.DanoFisicoCampeoes, mag+p.DanoMagicoCampeoes, ver+p.DanoVerdadeiroCampeoes
		}
	}
	tot := fis + mag + ver
	for _, it := range itens {
		if !it.Completo {
			continue
		}
		e := EvidenciaBuild{TMs: it.TMs, ItemID: it.ItemID, Nome: it.Nome, Custo: it.Custo, Atributos: []string{}, Placar: f.placarEm(pid, it.TMs)}
		if f.est != nil {
			if x, ok := f.est.Itens[it.ItemID]; ok && x.Linhas != nil {
				e.Atributos = x.Linhas
			}
		}
		if d, ok := difEm(f.curva, it.TMs); ok {
			if tm == 200 {
				d = -d
			}
			e.VantagemTime = ip(d)
		}
		if o, ok := f.valorJogadorEm(pid, it.TMs, ouroTotal); ok {
			e.OuroJogador = ip(int(math.Round(o)))
		}
		if adv > 0 {
			if o, ok := f.valorJogadorEm(adv, it.TMs, ouroTotal); ok {
				e.OuroAdversario = ip(int(math.Round(o)))
			}
		}
		if tot > 0 {
			e.DanoTimeInimigo = map[string]int{"fisico": pct(fis, tot), "magico": pct(mag, tot), "verdadeiro": pct(ver, tot)}
		}
		var forte *InimigoForte
		for _, p := range f.dp.pt.Participantes {
			if p.Time == tm {
				continue
			}
			o, ok := f.valorJogadorEm(p.ParticipanteID, it.TMs, ouroTotal)
			if !ok {
				continue
			}
			if forte == nil || int(o) > forte.Ouro {
				t := p.DanoFisicoCampeoes + p.DanoMagicoCampeoes + p.DanoVerdadeiroCampeoes
				forte = &InimigoForte{ParticipanteID: p.ParticipanteID, Campeao: p.Campeao, Ouro: int(math.Round(o)),
					Placar: f.placarEm(p.ParticipanteID, it.TMs), FisicoPct: pct(p.DanoFisicoCampeoes, t),
					MagicoPct: pct(p.DanoMagicoCampeoes, t), VerdadeiroPct: pct(p.DanoVerdadeiroCampeoes, t)}
			}
		}
		e.InimigoMaisForte = forte
		var dr DanoRecebido
		var rf, rm, rv int64
		for _, rt := range f.retratos {
			if rt.TMs > it.TMs {
				break
			}
			if rt.Vitima != pid || rt.Riot == nil {
				continue
			}
			dr.Mortes++
			for _, x := range rt.Riot.Recebido {
				if x.ParticipanteID > 0 && f.timeDe[x.ParticipanteID] != tm {
					rf, rm, rv = rf+int64(x.Fisico), rm+int64(x.Magico), rv+int64(x.Verdadeiro)
				}
			}
		}
		if dr.Mortes > 0 && rf+rm+rv > 0 {
			t := rf + rm + rv
			dr.FisicoPct, dr.MagicoPct, dr.VerdadeiroPct = pct(rf, t), pct(rm, t), pct(rv, t)
			e.DanoNasMortes = &dr
		}
		l = append(l, e)
	}
	return l
}

// --- Nota de impacto ------------------------------------------------------------------------

// notas calcula a nota de impacto de cada um (em ouro equivalente), com os
// componentes, e a posição na partida. Não é MVP por placar: entra o que o
// jogador gerou e negou (abates e objetivos repartidos entre quem participou),
// o custo das próprias mortes, as mortes caras, a participação nos
// momentos-chave (a favor ou contra o time dele) e a vantagem de rota aos 15.
func (f *fatos) notas(js []ResumoJogador, ms []MomentoChave) {
	for i := range js {
		j := &js[i]
		pid, tm := j.ParticipanteID, j.Time
		var gerado, objetivos, mortes float64
		nAb, nObj, nMortes := 0, 0, 0
		for _, rt := range f.retratos {
			im := f.mortes[rt.N]
			if rt.Vitima == pid {
				mortes += float64(im.custo)
				nMortes++
				continue
			}
			if rt.Matador == pid || contem(rt.Assistencias, pid) {
				gerado += float64(im.custo) / float64(1+len(rt.Assistencias))
				nAb++
			}
		}
		for _, o := range f.objs {
			if o.time == tm && contem(o.participantes, pid) {
				objetivos += float64(o.valor) / float64(len(o.participantes))
				nObj++
			}
		}
		var nos, contra float64
		nMom := 0
		for _, m := range ms {
			if !m.Chave {
				continue
			}
			dele := contem(m.Envolvidos, pid)
			if m.Tipo == "janela_perdida" || m.Tipo == "janela_aproveitada" {
				dele = m.ParticipanteID == pid
			}
			if !dele || m.Time == 0 {
				continue
			}
			nMom++
			if m.Time == tm {
				nos += float64(m.Impacto) * Param.PesoMomentoChave
			} else {
				contra += float64(m.Impacto) * Param.PesoMomentoChave
			}
		}
		var rota float64
		detRota := "sem adversário com quadro aos 10/15"
		if j.DifOuro15 != nil {
			rota = float64(*j.DifOuro15) * Param.PesoVantagemRota
			detRota = fmt.Sprintf("%s aos 15 × %.2f", sinalOuro(*j.DifOuro15), Param.PesoVantagemRota)
		} else if j.DifOuro10 != nil {
			rota = float64(*j.DifOuro10) * Param.PesoVantagemRota
			detRota = fmt.Sprintf("%s aos 10 × %.2f", sinalOuro(*j.DifOuro10), Param.PesoVantagemRota)
		}
		r := func(v float64) int { return int(math.Round(v)) }
		j.Componentes = []ComponenteNota{
			{"abates", "Abates e assistências", r(gerado), fmt.Sprintf("%d abate(s)/assistência(s), custo da morte ÷ participantes", nAb)},
			{"objetivos", "Objetivos e torres", r(objetivos), fmt.Sprintf("%d objetivo(s)/torre(s), valor ÷ participantes", nObj)},
			{"mortes", "Custo das próprias mortes", -r(mortes), fmt.Sprintf("%d morte(s)", nMortes)},
			{"mortes_caras", "Mortes caras", -j.MortesCaras * Param.PenalidadeMorteCara, fmt.Sprintf("%d × %d", j.MortesCaras, Param.PenalidadeMorteCara)},
			{"momentos_chave", "Momentos-chave", r(nos - contra), fmt.Sprintf("%d momento(s): +%.0f a favor, −%.0f contra", nMom, nos, contra)},
			{"vantagem_rota", "Vantagem de rota", r(rota), detRota},
		}
		soma := 0
		for _, c := range j.Componentes {
			soma += c.Valor
		}
		j.Nota = soma
	}
	ordem := make([]int, len(js))
	for i := range ordem {
		ordem[i] = i
	}
	sort.SliceStable(ordem, func(a, b int) bool { return js[ordem[a]].Nota > js[ordem[b]].Nota })
	for k, i := range ordem {
		js[i].PosicaoNota = k + 1
	}
}
