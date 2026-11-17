package analise

import (
	"fmt"
	"math"
	"sort"

	"loltracking/server/rastreio"
)

// Indicadores por jogador e partida (tabela `indicadores`), recalculáveis. O
// vocabulário (Vocabulario) diz o nome, o rótulo, a unidade, de onde vem e
// para que lado é "melhor" — o estudo compara vitórias × derrotas por eles.

// DefIndicador descreve um indicador.
type DefIndicador struct {
	Nome      string `json:"nome"`
	Rotulo    string `json:"rotulo"`
	Unidade   string `json:"unidade"` // n, s, %, cs, ouro, xp, unidades, por_min, dano
	Fonte     string `json:"fonte"`   // linha_do_tempo, riot, rastreio, retrato, estado
	Quem      string `json:"quem"`    // todos, rastreados, rotador, cacador
	Melhor    string `json:"melhor"`  // maior, menor ou "" (neutro)
	Descricao string `json:"descricao"`
}

// Momento é um instante que compõe um indicador (para a tela mostrar
// exemplos: "morreu fora da rota aos 8:48").
type Momento struct {
	TMs   int64  `json:"t_ms"`
	Texto string `json:"texto"`
}

// Indicador é um valor calculado.
type Indicador struct {
	ParticipanteID int            `json:"participante_id"`
	Indicador      string         `json:"indicador"`
	Valor          *float64       `json:"valor"`
	Aproximado     bool           `json:"aproximado"`
	Detalhe        map[string]any `json:"detalhe"`
}

// MinSeg escreve ms de jogo como m:ss.
func MinSeg(ms int64) string { return fmt.Sprintf("%d:%02d", ms/60000, ms%60000/1000) }

// Vocabulario é a lista de indicadores, na ordem em que a tela mostra.
var Vocabulario = func() []DefIndicador {
	v := []DefIndicador{
		{"saidas_rota_ate14_n", "Saídas de rota até os 14 min", "n", "rastreio", "rotador", "", "saídas que contam (não a ida andando para a base) na fase de rotas"},
		{"saidas_rota_ate14_tempo_s", "Tempo fora da rota até os 14 min", "s", "rastreio", "rotador", "", "soma da duração das saídas da fase de rotas"},
		{"cs_perdido_saidas_est", "CS perdido nas saídas (estimado)", "cs", "rastreio", "rotador", "menor", "ritmo de CS dos 2 min antes × duração de cada saída da fase de rotas"},
		{"mortes_fora_da_rota", "Mortes fora da rota", "n", "rastreio", "rotador", "menor", "mortes no meio de uma saída de rota (qualquer fase)"},
		{"mortes_saida_vida_baixa", "Mortes saindo da rota com pouca vida", "n", "rastreio", "rotador", "menor", "morreu numa saída que começou com menos de 40% de vida"},
		{"visitas_rota_ate14_n", "Visitas a rotas até os 14 min", "n", "rastreio", "cacador", "", "entradas do caçador numa rota (gank/visita) na fase de rotas"},
		{"visitas_rota_ate14_com_abate", "Visitas a rotas com abate até os 14 min", "n", "rastreio", "cacador", "maior", "visitas da fase de rotas que tiveram abate ou assistência"},
		{"mortes", "Mortes", "n", "linha_do_tempo", "todos", "menor", ""},
		{"mortes_ate14", "Mortes até os 14 min", "n", "linha_do_tempo", "todos", "menor", ""},
		{"abates", "Abates", "n", "linha_do_tempo", "todos", "maior", ""},
		{"assistencias", "Assistências", "n", "linha_do_tempo", "todos", "maior", ""},
		{"participacao_abates", "Participação nos abates", "%", "linha_do_tempo", "todos", "maior", "(abates + assistências) ÷ abates do time"},
		{"mortes_com_r_em_recarga", "Mortes com o R em recarga", "n", "retrato", "rastreados", "menor", "só as mortes em que a recarga do R foi lida e o R já tinha sido aprendido"},
		{"mortes_com_flash_em_recarga", "Mortes com o Flash em recarga", "n", "retrato", "rastreados", "menor", "só as mortes em que a recarga do Flash foi lida"},
		{"dist_aliado_mais_proximo_morte", "Distância do aliado mais perto ao morrer", "unidades", "retrato", "todos", "menor", "média nas mortes; aproximada quando algum aliado não foi rastreado"},
		{"mortes_em_inferioridade", "Mortes em inferioridade numérica", "n", "retrato", "todos", "menor", "mais inimigos que aliados (contando a vítima) a menos de 1500 dela"},
		{"cs_10", "CS aos 10 min", "cs", "linha_do_tempo", "todos", "maior", ""},
		{"cs_15", "CS aos 15 min", "cs", "linha_do_tempo", "todos", "maior", ""},
		{"cs_por_min", "CS por minuto (partida)", "por_min", "linha_do_tempo", "todos", "maior", ""},
		{"ouro_10", "Ouro aos 10 min", "ouro", "linha_do_tempo", "todos", "maior", ""},
		{"ouro_15", "Ouro aos 15 min", "ouro", "linha_do_tempo", "todos", "maior", ""},
		{"xp_10", "XP aos 10 min", "xp", "linha_do_tempo", "todos", "maior", ""},
		{"xp_15", "XP aos 15 min", "xp", "linha_do_tempo", "todos", "maior", ""},
		{"dif_cs_10", "CS aos 10 contra o adversário de rota", "cs", "linha_do_tempo", "todos", "maior", "o dele menos o do adversário direto (mesma posição, outro time)"},
		{"dif_cs_15", "CS aos 15 contra o adversário de rota", "cs", "linha_do_tempo", "todos", "maior", ""},
		{"dif_ouro_10", "Ouro aos 10 contra o adversário de rota", "ouro", "linha_do_tempo", "todos", "maior", ""},
		{"dif_ouro_15", "Ouro aos 15 contra o adversário de rota", "ouro", "linha_do_tempo", "todos", "maior", ""},
		{"dif_xp_10", "XP aos 10 contra o adversário de rota", "xp", "linha_do_tempo", "todos", "maior", ""},
		{"dif_xp_15", "XP aos 15 contra o adversário de rota", "xp", "linha_do_tempo", "todos", "maior", ""},
		{"dano_campeoes_10", "Dano a campeões até os 10 min", "dano", "riot", "todos", "maior", "damageStats da linha do tempo da Riot"},
		{"dano_campeoes_15", "Dano a campeões até os 15 min", "dano", "riot", "todos", "maior", ""},
		{"wards_ate14", "Sentinelas colocadas até os 14 min", "n", "riot", "todos", "maior", "WARD_PLACED da linha do tempo da Riot"},
		{"tempo_morto_s", "Tempo morto", "s", "estado", "todos", "menor", "no trecho rastreado (estado dos 10)"},
		{"tempo_vida_baixa_s", "Tempo com vida baixa", "s", "estado", "todos", "menor", "vivo com menos de 30% (quadro lateral), no trecho rastreado"},
		{"habilidades_por_min", "Habilidades usadas por minuto", "por_min", "rastreio", "rastreados", "", "habilidade_usada da cadeia ÷ minutos vivo rastreados"},
		{"recalls", "Recalls", "n", "rastreio", "rastreados", "", "chegadas à base por recall no trecho rastreado"},
		{"cs_divergencias", "Minutos com CS divergente", "n", "estado", "todos", "", "CS do jogo fora da dezena do CS da linha do tempo"},
	}
	nomesFase := map[string]string{"rotas": "até 14 min", "meio": "14–25 min", "final": "depois dos 25"}
	rotGrupo := map[string]string{"rota_propria": "na própria rota", "outra_rota": "em outra rota", "rio": "no rio", "covas": "nas covas",
		"selva_propria": "na própria selva", "selva_inimiga": "na selva inimiga", "base_propria": "na base", "base_inimiga": "na base inimiga"}
	for _, f := range Fases {
		for _, g := range GruposRelativos {
			v = append(v, DefIndicador{"pct_" + g + "_" + f, "% do tempo " + rotGrupo[g] + " (" + nomesFase[f] + ")", "%", "rastreio",
				"rastreados", "", "do tempo vivo rastreado na fase"})
		}
	}
	rotAtv := map[string]string{AMorto: "morto", ANaBase: "na base", ALutando: "lutando", ATrocando: "trocando", AObjetivo: "em objetivo",
		ATorre: "em torre", AFarmando: "farmando", AAndando: "andando"}
	for _, f := range Fases {
		for _, a := range Atividades {
			v = append(v, DefIndicador{"pct_atv_" + a + "_" + f, "% do tempo " + rotAtv[a] + " (" + nomesFase[f] + ")", "%", "rastreio",
				"rastreados", "", "atividade deduzida, do tempo rastreado na fase"})
		}
	}
	return v
}()

var defPorNome = func() map[string]DefIndicador {
	m := map[string]DefIndicador{}
	for _, d := range Vocabulario {
		m[d.Nome] = d
	}
	return m
}()

// DefDe devolve a definição de um indicador.
func DefDe(nome string) (DefIndicador, bool) {
	d, ok := defPorNome[nome]
	return d, ok
}

// coleta guarda os indicadores de um participante.
type coleta struct {
	pid  int
	inds []Indicador
}

func (c *coleta) poe(nome string, v *float64, aprox bool, det map[string]any) {
	if det == nil {
		det = map[string]any{}
	}
	if v != nil {
		r := math.Round(*v*100) / 100
		v = &r
	}
	c.inds = append(c.inds, Indicador{ParticipanteID: c.pid, Indicador: nome, Valor: v, Aproximado: aprox, Detalhe: det})
}

func (c *coleta) conta(nome string, momentos []Momento, aprox bool, extra map[string]any) {
	if momentos == nil {
		momentos = []Momento{}
	}
	det := map[string]any{"momentos": momentos}
	for k, v := range extra {
		det[k] = v
	}
	c.poe(nome, fp(float64(len(momentos))), aprox, det)
}

// cobertura: quanto de [ini, fim) as amostras cobrem (vão > 3 s não conta).
func cobertura(as []rastreio.Amostra, ini, fim int64) int64 {
	var tot int64
	for i := 0; i+1 < len(as); i++ {
		a, b := as[i].TMs, as[i+1].TMs
		if b-a > 3000 {
			continue
		}
		a, b = max(a, ini), min(b, fim)
		if b > a {
			tot += b - a
		}
	}
	return tot
}

func (d *dadosPartida) duracaoMs() int64 { return int64(d.pt.DuracaoS) * 1000 }

func (d *dadosPartida) indicadores(retratos []Retrato) []Indicador {
	var todos []Indicador
	for _, part := range d.pt.Participantes {
		c := &coleta{pid: part.ParticipanteID}
		d.indicadoresLinha(c)
		d.indicadoresRetrato(c, retratos)
		d.indicadoresEstado(c)
		if len(d.amostras[part.ParticipanteID]) > 0 {
			d.indicadoresRastreio(c)
		}
		todos = append(todos, c.inds...)
	}
	return todos
}

// quadroEm é o quadro da linha do tempo do minuto m (nil se a partida não
// chegou lá).
func (d *dadosPartida) quadroEm(m int) *rastreio.QuadroLT {
	for i := range d.lt.Quadros {
		if q := &d.lt.Quadros[i]; q.TMs >= int64(m)*60000-1000 && q.TMs <= int64(m)*60000+1000 {
			return q
		}
	}
	return nil
}

func (d *dadosPartida) adversario(pid int) int {
	p := d.participante(pid)
	if p == nil || p.Posicao == "" {
		return 0
	}
	for _, o := range d.pt.Participantes {
		if o.Time != p.Time && o.Posicao == p.Posicao {
			return o.ParticipanteID
		}
	}
	return 0
}

func (d *dadosPartida) indicadoresLinha(c *coleta) {
	pid := c.pid
	part := d.participante(pid)
	adv := d.adversario(pid)
	for _, m := range []int{10, 15} {
		q := d.quadroEm(m)
		if q == nil {
			continue
		}
		j, ok := q.Jogadores[pid]
		if !ok {
			continue
		}
		c.poe(fmt.Sprintf("cs_%d", m), fp(float64(j.CS)), false, nil)
		c.poe(fmt.Sprintf("ouro_%d", m), fp(float64(j.OuroTotal)), false, nil)
		c.poe(fmt.Sprintf("xp_%d", m), fp(float64(j.XP)), false, nil)
		if o, ok := q.Jogadores[adv]; ok && adv > 0 {
			det := map[string]any{"adversario": adv}
			c.poe(fmt.Sprintf("dif_cs_%d", m), fp(float64(j.CS-o.CS)), false, det)
			c.poe(fmt.Sprintf("dif_ouro_%d", m), fp(float64(j.OuroTotal-o.OuroTotal)), false, det)
			c.poe(fmt.Sprintf("dif_xp_%d", m), fp(float64(j.XP-o.XP)), false, det)
		}
		if d.quadrosRiot != nil {
			if qr, ok := d.quadrosRiot[q.TMs]; ok {
				if s, ok := qr[pid]; ok {
					c.poe(fmt.Sprintf("dano_campeoes_%d", m), fp(float64(s.DanoCampeoes)), false, nil)
				}
			}
		}
	}
	if part != nil && d.pt.DuracaoS > 0 {
		c.poe("cs_por_min", fp(float64(part.CS)/(float64(d.pt.DuracaoS)/60)), false, nil)
		c.poe("abates", fp(float64(part.Abates)), false, nil)
		c.poe("assistencias", fp(float64(part.Assistencias)), false, nil)
		doTime := 0
		for _, o := range d.pt.Participantes {
			if o.Time == part.Time {
				doTime += o.Abates
			}
		}
		if doTime > 0 {
			c.poe("participacao_abates", fp(float64(part.Abates+part.Assistencias)/float64(doTime)*100), false,
				map[string]any{"abates_do_time": doTime})
		}
	}
	var mortes, ate14 []Momento
	for _, e := range d.lt.Eventos {
		if e.Tipo != "CHAMPION_KILL" || e.EmQuem != pid {
			continue
		}
		m := Momento{e.TMs, fmt.Sprintf("morreu aos %s", MinSeg(e.TMs))}
		if e.X != nil {
			z, _ := Zona(*e.X, *e.Y)
			m.Texto += " (" + Info(z).Nome + ")"
		}
		mortes = append(mortes, m)
		if e.TMs < Param.FaseRotasMs {
			ate14 = append(ate14, m)
		}
	}
	c.conta("mortes", mortes, false, nil)
	c.conta("mortes_ate14", ate14, false, nil)
	if d.riot != nil {
		c.conta("wards_ate14", d.wardsAte14(pid), false, nil)
	}
}

// wardsAte14: WARD_PLACED da Riot (creatorId) na fase de rotas.
func (d *dadosPartida) wardsAte14(pid int) []Momento {
	ms := []Momento{}
	for _, w := range d.wards {
		if w.quem == pid && w.tMs < Param.FaseRotasMs {
			ms = append(ms, Momento{w.tMs, fmt.Sprintf("sentinela (%s) aos %s", w.tipo, MinSeg(w.tMs))})
		}
	}
	return ms
}

func (d *dadosPartida) indicadoresRetrato(c *coleta, retratos []Retrato) {
	pid := c.pid
	var r, flash, infer []Momento
	nR, nF, nDist := 0, 0, 0
	soma, aprox, aproxInf := 0.0, false, false
	var distMs []Momento
	for _, rt := range retratos {
		if rt.Vitima != pid {
			continue
		}
		var eu *JogadorNoAbate
		for i := range rt.Jogadores {
			if rt.Jogadores[i].ParticipanteID == pid {
				eu = &rt.Jogadores[i]
			}
		}
		if eu == nil {
			continue
		}
		if eu.RDisponivel != nil {
			nR++
			if !*eu.RDisponivel {
				r = append(r, Momento{rt.TMs, fmt.Sprintf("morreu com o R em recarga aos %s", MinSeg(rt.TMs))})
			}
		}
		if eu.FlashDisponivel != nil {
			nF++
			if !*eu.FlashDisponivel {
				flash = append(flash, Momento{rt.TMs, fmt.Sprintf("morreu com o Flash em recarga aos %s", MinSeg(rt.TMs))})
			}
		}
		if rt.AliadoMaisProximo != nil {
			nDist++
			soma += *rt.AliadoMaisProximo
			aprox = aprox || rt.Aproximado
			distMs = append(distMs, Momento{rt.TMs, fmt.Sprintf("aliado mais perto a %.0f ao morrer aos %s", *rt.AliadoMaisProximo, MinSeg(rt.TMs))})
		}
		if rt.Superioridade > 0 {
			aproxInf = aproxInf || rt.Aproximado
			infer = append(infer, Momento{rt.TMs, fmt.Sprintf("morreu %d contra %d aos %s", rt.InimigosDaVitima, rt.AliadosDaVitima+1, MinSeg(rt.TMs))})
		}
	}
	if nR > 0 {
		c.conta("mortes_com_r_em_recarga", r, false, map[string]any{"mortes_lidas": nR})
	}
	if nF > 0 {
		c.conta("mortes_com_flash_em_recarga", flash, false, map[string]any{"mortes_lidas": nF})
	}
	if nDist > 0 {
		c.poe("dist_aliado_mais_proximo_morte", fp(soma/float64(nDist)), aprox, map[string]any{"momentos": distMs, "mortes": nDist})
	}
	c.conta("mortes_em_inferioridade", infer, aproxInf, nil)
}

// indicadoresEstado: do estado dos 10 (todos os passes): tempo morto, vida
// baixa, conferência do CS.
func (d *dadosPartida) indicadoresEstado(c *coleta) {
	es := d.estados[c.pid]
	if len(es) < 2 {
		return
	}
	var cob, morto, baixa int64
	for i := 0; i+1 < len(es); i++ {
		dt := es[i+1].TMs - es[i].TMs
		if dt > 3000 {
			continue
		}
		cob += dt
		if es[i].Morto {
			morto += dt
		} else if es[i].VidaPct != nil && *es[i].VidaPct < Param.VidaBaixaPct {
			baixa += dt
		}
	}
	aprox := cob < d.duracaoMs()*9/10
	det := map[string]any{"cobertura_ms": cob}
	c.poe("tempo_morto_s", fp(float64(morto)/1000), aprox, det)
	c.poe("tempo_vida_baixa_s", fp(float64(baixa)/1000), aprox, det)
	var div []Momento
	conf := d.ConferenciaCS(c.pid)
	for _, l := range conf {
		if l.Divergente {
			div = append(div, Momento{l.TMs, fmt.Sprintf("CS %d no jogo × %d na linha do tempo aos %s", *l.CSJogo, l.CSLCU, MinSeg(l.TMs))})
		}
	}
	if len(conf) > 0 {
		c.conta("cs_divergencias", div, aprox, map[string]any{"minutos_conferidos": len(conf)})
	}
}

func (d *dadosPartida) indicadoresRastreio(c *coleta) {
	pid := c.pid
	as := d.amostras[pid]
	part := d.participante(pid)
	cadeia := d.cadeias[pid]
	rota := ""
	time := 0
	if part != nil {
		rota, time = RotaDaPosicao(part.Posicao), part.Time
	}
	fimRotas := min(Param.FaseRotasMs, d.duracaoMs())
	cobRotas := cobertura(as, 0, fimRotas)
	aproxRotas := cobRotas < fimRotas*9/10
	detRotas := map[string]any{"cobertura_ms": cobRotas}
	if part != nil && part.Posicao != "" {
		if rota != "" {
			var saidas, mortesFora, mortesBaixa []Momento
			tempo, perdido := 0.0, 0.0
			for _, it := range cadeia {
				if it.Tipo != "saiu_da_rota" {
					continue
				}
				dd := it.Dados
				dur := numero(dd["duracao_ms"])
				para, _ := dd["para"].(string)
				motivo, _ := dd["fim_motivo"].(string)
				conta, _ := dd["conta"].(bool)
				ate14, _ := dd["fase_rotas"].(bool)
				if conta && ate14 {
					saidas = append(saidas, Momento{it.TMs, fmt.Sprintf("saiu da rota aos %s para %s (%.0f s, %s)", MinSeg(it.TMs),
						Info(para).Nome, dur/1000, textoDesfecho(dd["desfecho"]))})
					tempo += dur / 1000
					if v := numero(dd["cs_perdido_est"]); !math.IsNaN(v) {
						perdido += v
					}
				}
				if motivo == "morreu" {
					fim := int64(numero(dd["fim_ms"]))
					mortesFora = append(mortesFora, Momento{fim, fmt.Sprintf("morreu fora da rota aos %s (%s, %.0f s depois de sair)",
						MinSeg(fim), Info(para).Nome, dur/1000)})
					if v := numero(dd["vida_pct"]); !math.IsNaN(v) && v < Param.VidaSaidaArriscadaPct {
						mortesBaixa = append(mortesBaixa, Momento{fim, fmt.Sprintf("saiu com %.0f%% de vida aos %s e morreu aos %s",
							v, MinSeg(it.TMs), MinSeg(fim))})
					}
				}
			}
			c.conta("saidas_rota_ate14_n", saidas, aproxRotas, detRotas)
			c.poe("saidas_rota_ate14_tempo_s", fp(tempo), aproxRotas, detRotas)
			c.poe("cs_perdido_saidas_est", fp(perdido), true, detRotas)
			cobTudo := map[string]any{"cobertura_ms": cobertura(as, 0, d.duracaoMs())}
			aproxTudo := cobertura(as, 0, d.duracaoMs()) < d.duracaoMs()*9/10
			c.conta("mortes_fora_da_rota", mortesFora, aproxTudo, cobTudo)
			c.conta("mortes_saida_vida_baixa", mortesBaixa, aproxTudo, cobTudo)
		} else {
			var visitas, comAbate []Momento
			for _, it := range cadeia {
				if it.Tipo != "entrou_em_rota" {
					continue
				}
				if ate14, _ := it.Dados["fase_rotas"].(bool); !ate14 {
					continue
				}
				r, _ := it.Dados["rota"].(string)
				m := Momento{it.TMs, fmt.Sprintf("entrou na %s aos %s (%s)", Info(r).Nome, MinSeg(it.TMs), textoDesfecho(it.Dados["desfecho"]))}
				visitas = append(visitas, m)
				if it.Dados["desfecho"] == "abate" {
					comAbate = append(comAbate, m)
				}
			}
			c.conta("visitas_rota_ate14_n", visitas, aproxRotas, detRotas)
			c.conta("visitas_rota_ate14_com_abate", comAbate, aproxRotas, detRotas)
		}
	}
	// Habilidades por minuto vivo e recalls.
	var vivo int64
	for i := 0; i+1 < len(as); i++ {
		if dt := as[i+1].TMs - as[i].TMs; dt <= 3000 && !as[i].Morto {
			vivo += dt
		}
	}
	nHab, nRecall := 0, []Momento{}
	for _, it := range cadeia {
		switch it.Tipo {
		case "habilidade_usada":
			nHab++
		case "na_base":
			if it.Dados["chegada"] == "recall" {
				nRecall = append(nRecall, Momento{it.TMs, fmt.Sprintf("recall aos %s", MinSeg(it.TMs))})
			}
		}
	}
	cobTot := cobertura(as, 0, d.duracaoMs())
	aproxTot := cobTot < d.duracaoMs()*9/10
	if vivo > 60000 {
		c.poe("habilidades_por_min", fp(float64(nHab)/(float64(vivo)/60000)), aproxTot, map[string]any{"usos": nHab, "vivo_ms": vivo})
	}
	c.conta("recalls", nRecall, aproxTot, map[string]any{"cobertura_ms": cobTot})

	// % do tempo por grupo de zona e por atividade, por fase.
	tempoGrupo := map[string]map[string]int64{}
	vivoFase := map[string]int64{}
	for i := 0; i+1 < len(as); i++ {
		a := as[i]
		dt := as[i+1].TMs - a.TMs
		if dt > 3000 || a.Morto || a.X == nil || a.Y == nil {
			continue
		}
		f := Fase(a.TMs)
		z, _ := Zona(*a.X, *a.Y)
		g := GrupoRelativo(z, rota, time)
		if tempoGrupo[f] == nil {
			tempoGrupo[f] = map[string]int64{}
		}
		tempoGrupo[f][g] += dt
		vivoFase[f] += dt
	}
	for _, f := range Fases {
		if vivoFase[f] < 60000 {
			continue
		}
		for _, g := range GruposRelativos {
			c.poe("pct_"+g+"_"+f, fp(float64(tempoGrupo[f][g])/float64(vivoFase[f])*100), d.faseIncompleta(as, f),
				map[string]any{"vivo_ms": vivoFase[f]})
		}
	}
	tempoAtv := map[string]map[string]int64{}
	totAtv := map[string]int64{}
	for _, it := range cadeia {
		if it.Tipo != "atividade" {
			continue
		}
		a, _ := it.Dados["atividade"].(string)
		ini, fim := it.TMs, int64(numero(it.Dados["fim_ms"]))
		for _, f := range Fases {
			fi, ff := limitesFase(f)
			x, y := max(ini, fi), min(fim, ff)
			if y > x {
				if tempoAtv[f] == nil {
					tempoAtv[f] = map[string]int64{}
				}
				tempoAtv[f][a] += y - x
				totAtv[f] += y - x
			}
		}
	}
	for _, f := range Fases {
		if totAtv[f] < 60000 {
			continue
		}
		for _, a := range Atividades {
			c.poe("pct_atv_"+a+"_"+f, fp(float64(tempoAtv[f][a])/float64(totAtv[f])*100), d.faseIncompleta(as, f),
				map[string]any{"rastreado_ms": totAtv[f]})
		}
	}
}

func limitesFase(f string) (int64, int64) {
	switch f {
	case "rotas":
		return 0, Param.FaseRotasMs
	case "meio":
		return Param.FaseRotasMs, Param.FaseMeioMs
	}
	return Param.FaseMeioMs, math.MaxInt64 / 2
}

// faseIncompleta: o rastreio não cobre 90% da fase (dentro da partida).
func (d *dadosPartida) faseIncompleta(as []rastreio.Amostra, f string) bool {
	ini, fim := limitesFase(f)
	fim = min(fim, d.duracaoMs())
	if fim <= ini {
		return true
	}
	return cobertura(as, ini, fim) < (fim-ini)*9/10
}

func textoDesfecho(v any) string {
	switch v {
	case "abate":
		return "com abate"
	case "morte":
		return "morreu"
	case "objetivo":
		return "objetivo"
	}
	return "nada aconteceu"
}

// --- CS --------------------------------------------------------------------------

// LinhaCS é a conferência do CS de um minuto.
type LinhaCS struct {
	Minuto     int   `json:"minuto"`
	TMs        int64 `json:"t_ms"`
	CSLCU      int   `json:"cs_lcu"`  // linha do tempo (exato)
	CSJogo     *int  `json:"cs_jogo"` // creepScore do playerlist (anda de 10 em 10), a ≤ 2 s antes do quadro
	Diferenca  *int  `json:"diferenca"`
	Divergente bool  `json:"divergente"`
}

// ConferenciaCS compara, minuto a minuto, o CS do jogo (estado dos 10,
// qualquer passe) com o da linha do tempo. O creepScore do playerlist anda
// de 10 em 10: bate se o exato está na mesma dezena, com folga de
// rastreio.LimiteDivergenciaCS.
func (d *dadosPartida) ConferenciaCS(pid int) []LinhaCS {
	es := d.estados[pid]
	l := []LinhaCS{}
	for _, q := range d.lt.Quadros {
		j, ok := q.Jogadores[pid]
		if !ok || q.TMs <= 0 {
			continue
		}
		e := EstadoEm(es, q.TMs, 2000)
		if e == nil {
			continue
		}
		cs, dif := e.CS, j.CS-e.CS
		l = append(l, LinhaCS{Minuto: int((q.TMs + 30000) / 60000), TMs: q.TMs, CSLCU: j.CS, CSJogo: &cs, Diferenca: &dif,
			Divergente: dif < -rastreio.LimiteDivergenciaCS || dif > 9+rastreio.LimiteDivergenciaCS})
	}
	sort.Slice(l, func(a, b int) bool { return l[a].TMs < l[b].TMs })
	return l
}
