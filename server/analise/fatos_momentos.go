package analise

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Os momentos da partida, com o custo estimado para cada time e o saldo de
// ouro antes/depois. Todo abate, luta de time, objetivo, torre, freeze e janela
// de oportunidade vira uma linha (para a busca por SQL); os de maior impacto
// ganham `chave` (a lista curta). Impacto = |custo do azul − custo do
// vermelho|, em ouro equivalente (parametros.go).

// momentos monta, ordena e numera todos os momentos (uma vez por partida).
func (f *fatos) momentos() []MomentoChave {
	if f.ms != nil {
		return f.ms
	}
	var l []MomentoChave
	lutas := f.lutas()
	for _, rt := range f.retratos {
		l = append(l, f.momentoAbate(rt))
	}
	l = append(l, lutas...)
	for i := range f.objs {
		l = append(l, f.momentoObjetivo(&f.objs[i]))
	}
	l = append(l, f.freezes()...)
	l = append(l, f.janelas()...)
	for i := range l {
		m := &l[i]
		if m.FimMs < m.TMs {
			m.FimMs = m.TMs
		}
		if a, ok := difEm(f.curva, m.TMs-Param.SaldoAntesMs); ok {
			m.SaldoAntes = ip(a)
		}
		if b, ok := difEm(f.curva, m.FimMs+Param.SaldoDepoisMs); ok {
			m.SaldoDepois = ip(b)
		}
		if m.Impacto == 0 {
			m.Impacto = absInt(m.CustoAzul - m.CustoVermelho)
		}
		if m.Envolvidos == nil {
			m.Envolvidos = []int{}
		}
		if m.Tags == nil {
			m.Tags = []string{}
		}
		if m.Dados == nil {
			m.Dados = map[string]any{}
		}
		if m.ParticipanteID > 0 {
			m.Funcao, m.Campeao = f.posDe[m.ParticipanteID], f.campeao(m.ParticipanteID)
		}
	}
	// A lista curta: os de maior impacto. O abate que faz parte de uma luta de
	// time não concorre (a luta o representa).
	var cand []int
	for i, m := range l {
		_, emLuta := m.Dados["luta_t_ms"]
		if m.Impacto < Param.ImpactoMinimoChave || (m.Tipo == "abate" && emLuta) {
			continue
		}
		cand = append(cand, i)
	}
	sort.SliceStable(cand, func(a, b int) bool { return l[cand[a]].Impacto > l[cand[b]].Impacto })
	for k, i := range cand {
		if k >= Param.MomentosChave {
			break
		}
		l[i].Chave = true
	}
	sort.SliceStable(l, func(a, b int) bool {
		if l[a].TMs != l[b].TMs {
			return l[a].TMs < l[b].TMs
		}
		return l[a].Tipo < l[b].Tipo
	})
	for i := range l {
		l[i].N = i + 1
	}
	f.ms = l
	return l
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// custoPara põe o custo no time certo.
func custoPara(m *MomentoChave, time, v int) {
	if time == 100 {
		m.CustoAzul += v
	} else if time == 200 {
		m.CustoVermelho += v
	}
}

func (f *fatos) momentoAbate(rt Retrato) MomentoChave {
	im := f.mortes[rt.N]
	tv := f.timeDe[rt.Vitima]
	m := MomentoChave{TMs: rt.TMs, FimMs: rt.TMs, Tipo: "abate", Time: 300 - tv, ParticipanteID: rt.Vitima, Autor: rt.Matador,
		Zona: rt.Zona, Estimado: im.estimado, Tags: append([]string{}, im.tags...), DuracaoMs: im.mortoMs}
	if tv == 0 {
		m.Time = 0
	}
	m.Envolvidos = []int{rt.Vitima}
	if rt.Matador > 0 {
		m.Envolvidos = append(m.Envolvidos, rt.Matador)
	}
	for _, a := range rt.Assistencias {
		if !contem(m.Envolvidos, a) {
			m.Envolvidos = append(m.Envolvidos, a)
		}
	}
	if im.cara {
		m.Subtipo = "morte_cara"
	}
	if z := rt.Zona; TipoZona(z) == "rota" {
		m.Rota = z
	}
	m.Atividade = f.atividadeEm(rt.Vitima, rt.TMs-1000).atv
	if m.Atividade == AMorto {
		m.Atividade = ""
	}
	m.AtividadePrevia = f.atividadePrevia(rt.Vitima, rt.TMs)
	if m.AtividadePrevia != "" {
		m.Tags = append(m.Tags, "previa_"+m.AtividadePrevia)
	}
	custoPara(&m, tv, im.custo)
	m.Dados = map[string]any{"ouro_abate": im.ouroAbate, "ouro_negado": im.negado, "morto_ms": im.mortoMs, "retrato": rt.N,
		"aliado_mais_proximo": rt.AliadoMaisProximo, "aliados_perto": rt.AliadosDaVitima, "inimigos_perto": rt.InimigosDaVitima}
	if im.luta > 0 {
		m.Dados["luta_t_ms"] = im.luta
		m.Tags = append(m.Tags, "luta")
	}
	if im.objetivoDepois != nil {
		m.Dados["objetivo_depois"] = im.objetivoDepois.nome
		m.Dados["objetivo_depois_ms"] = im.objetivoDepois.t
	}
	f.visaoDoAbate(&m, rt)
	// A frase.
	var b strings.Builder
	fmt.Fprintf(&b, "%s morreu", f.quem(rt.Vitima))
	if rt.Matador > 0 {
		fmt.Fprintf(&b, " para %s", f.campeao(rt.Matador))
		if len(rt.Assistencias) > 0 {
			fmt.Fprintf(&b, " (com %s)", f.nomes(rt.Assistencias))
		}
	}
	fmt.Fprintf(&b, " aos %s", MinSeg(rt.TMs))
	if rt.Zona != "" {
		fmt.Fprintf(&b, " em %s", nomeZona(rt.Zona))
	}
	switch {
	case m.AtividadePrevia != "" && m.AtividadePrevia != m.Atividade:
		fmt.Fprintf(&b, ", %s antes da luta", NomeAtividade(m.AtividadePrevia))
	case m.Atividade != "":
		fmt.Fprintf(&b, ", %s", NomeAtividade(m.Atividade))
	}
	var motivos []string
	for _, t := range im.tags {
		switch t {
		case "sozinho":
			motivos = append(motivos, fmt.Sprintf("sozinho (aliado mais perto a %.0f)", *rt.AliadoMaisProximo))
		case "inferioridade":
			motivos = append(motivos, fmt.Sprintf("%d contra %d", rt.InimigosDaVitima, rt.AliadosDaVitima+1))
		case "r_em_recarga":
			motivos = append(motivos, "com o R em recarga")
		case "flash_em_recarga":
			motivos = append(motivos, "com o Flash em recarga")
		case "passiva_em_recarga":
			motivos = append(motivos, "com a passiva em recarga")
		case "sem_troca":
			motivos = append(motivos, "sem troca (o time dele não matou ninguém na briga)")
		case "antes_de_objetivo":
			motivos = append(motivos, fmt.Sprintf("antes de o inimigo levar %s aos %s", im.objetivoDepois.nome, MinSeg(im.objetivoDepois.t)))
		}
	}
	for _, t := range m.Tags {
		switch t {
		case "atacante_nao_visto":
			motivos = append(motivos, fmt.Sprintf("%s chegou sem ser visto pelo time da vítima", f.campeao(rt.Matador)))
		case "cacador_nao_visto":
			motivos = append(motivos, "sem ver o caçador inimigo antes")
		}
	}
	if len(motivos) > 0 {
		fmt.Fprintf(&b, "; %s", juntaE(motivos))
	}
	fmt.Fprintf(&b, ". Custo ~%s para o %s (%s do abate + %s que deixou de ganhar em %s morto",
		ouroTexto(im.custo), NomeTime(tv), ouroTexto(im.ouroAbate), ouroTexto(max(0, im.negado)), segundosTexto(im.mortoMs))
	if im.objetivoDepois != nil {
		fmt.Fprintf(&b, " + %s do objetivo", ouroTexto(int(math.Round(float64(im.objetivoDepois.valor)*Param.PesoObjetivoNaMorte))))
	}
	b.WriteString(").")
	m.Frase = b.String()
	return m
}

// visaoDoAbate: com passe de visão do time da vítima, para cada atacante
// (matador e assistências do outro time) e para o caçador inimigo, "era visto
// pelo time da vítima nos VisaoAntesMs antes do abate, e antes de chegar a
// ela?" (visao.go). Vai em dados.visao_atacantes e dados.visao_cacador; as
// etiquetas atacante_nao_visto (o matador, resposta firme "não" na janela ou
// antes da chegada) e cacador_nao_visto deixam achar pela busca.
func (f *fatos) visaoDoAbate(m *MomentoChave, rt Retrato) {
	tv := f.timeDe[rt.Vitima]
	if f.dp.visao == nil || tv == 0 {
		return
	}
	vt, ok := f.dp.visao.DoObservador(tv)
	if !ok || rt.TMs < vt.DeMs || rt.TMs > vt.AteMs+1000 {
		return
	}
	de := max(0, rt.TMs-Param.VisaoAntesMs)
	atac := []VisaoAntes{}
	envolvido := map[int]bool{}
	for _, p := range append([]int{rt.Matador}, rt.Assistencias...) {
		if p <= 0 || f.timeDe[p] == tv || envolvido[p] {
			continue
		}
		envolvido[p] = true
		if r, ok := f.dp.visaoDoAtacante(vt, p, rt.Vitima, de, rt.TMs); ok {
			atac = append(atac, r)
			if p == rt.Matador && naoVisto(r) {
				m.Tags = append(m.Tags, "atacante_nao_visto")
			}
		}
	}
	m.Dados["visao_atacantes"] = atac
	for p, pos := range f.posDe {
		if pos != "JUNGLE" || f.timeDe[p] != 300-tv {
			continue
		}
		if r, ok := f.dp.visaoDoAtacante(vt, p, rt.Vitima, de, rt.TMs); ok {
			m.Dados["visao_cacador"] = r
			if naoVisto(r) {
				m.Tags = append(m.Tags, "cacador_nao_visto")
			}
		}
	}
}

// naoVisto: resposta firme de que ele não foi visto, na janela toda ou antes
// de chegar à vítima.
func naoVisto(r VisaoAntes) bool {
	return r.Resposta == VistoNao || (r.AntesDaChegada != nil && r.AntesDaChegada.Resposta == VistoNao)
}

// lutas agrupa os abates seguidos (a menos de JanelaLutaTimeMs um do outro) em
// lutas de time, quando somam AbatesLutaTime ou mais.
func (f *fatos) lutas() []MomentoChave {
	var l []MomentoChave
	rs := f.retratos
	for i := 0; i < len(rs); {
		j := i + 1
		for j < len(rs) && rs[j].TMs-rs[j-1].TMs <= Param.JanelaLutaTimeMs {
			j++
		}
		if j-i >= Param.AbatesLutaTime {
			l = append(l, f.momentoLuta(rs[i:j]))
		}
		i = j
	}
	return l
}

func (f *fatos) momentoLuta(rs []Retrato) MomentoChave {
	m := MomentoChave{TMs: rs[0].TMs, FimMs: rs[len(rs)-1].TMs, Tipo: "luta", Zona: rs[0].Zona, Envolvidos: []int{}}
	m.DuracaoMs = m.FimMs - m.TMs
	mortos := map[int][]string{}
	estimado := false
	for _, rt := range rs {
		im := f.mortes[rt.N]
		im.luta = int(m.TMs) // o abate aponta para a luta pelo instante (o n só existe depois de ordenar)
		tv := f.timeDe[rt.Vitima]
		custoPara(&m, tv, im.custo)
		mortos[tv] = append(mortos[tv], f.campeao(rt.Vitima))
		estimado = estimado || im.estimado
		for _, p := range append([]int{rt.Vitima, rt.Matador}, rt.Assistencias...) {
			if p > 0 && !contem(m.Envolvidos, p) {
				m.Envolvidos = append(m.Envolvidos, p)
			}
		}
	}
	m.Estimado = estimado
	az, ve := len(mortos[200]), len(mortos[100]) // abates de cada time = mortos do outro
	switch {
	case m.CustoVermelho > m.CustoAzul:
		m.Time = 100
	case m.CustoAzul > m.CustoVermelho:
		m.Time = 200
	}
	m.Dados = map[string]any{"abates_azul": az, "abates_vermelho": ve, "mortos_azul": mortos[100], "mortos_vermelho": mortos[200]}
	// Objetivo que o vencedor levou logo depois (até JanelaMorteObjetivoMs).
	for _, o := range f.objs {
		if o.time == m.Time && m.Time != 0 && o.t >= m.TMs && o.t <= m.FimMs+Param.JanelaMorteObjetivoMs {
			m.Dados["objetivo_depois"] = o.nome
			m.Tags = append(m.Tags, "objetivo_depois")
			break
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Luta de time de %s a %s", MinSeg(m.TMs), MinSeg(m.FimMs))
	if m.Zona != "" {
		fmt.Fprintf(&b, " em %s", nomeZona(m.Zona))
	}
	fmt.Fprintf(&b, ": azul %d × %d vermelho", az, ve)
	var quem []string
	if len(mortos[100]) > 0 {
		quem = append(quem, "do azul "+juntaE(mortos[100]))
	}
	if len(mortos[200]) > 0 {
		quem = append(quem, "do vermelho "+juntaE(mortos[200]))
	}
	if len(quem) > 0 {
		fmt.Fprintf(&b, " (morreram %s)", strings.Join(quem, "; "))
	}
	fmt.Fprintf(&b, ". Custo ~%s para o azul e ~%s para o vermelho", ouroTexto(m.CustoAzul), ouroTexto(m.CustoVermelho))
	if o, ok := m.Dados["objetivo_depois"].(string); ok {
		fmt.Fprintf(&b, "; o %s levou %s em seguida", NomeTime(m.Time), o)
	}
	b.WriteString(".")
	m.Frase = b.String()
	return m
}

func (f *fatos) momentoObjetivo(o *objetivoEv) MomentoChave {
	m := MomentoChave{TMs: o.t, FimMs: o.t, Tipo: o.tipo, Subtipo: o.nome, Time: o.time, ParticipanteID: o.autor, Autor: o.autor,
		Rota: o.rota, Envolvidos: append([]int{}, o.participantes...)}
	if o.x != nil {
		m.Zona, _ = Zona(*o.x, *o.y)
	}
	if m.ParticipanteID > 0 && f.timeDe[m.ParticipanteID] != o.time {
		// Torre derrubada pela tropa (killerId do outro time ou 0): sem protagonista.
		m.ParticipanteID, m.Autor = 0, 0
	}
	custoPara(&m, 300-o.time, o.valor)
	m.Estimado = true // o valor do objetivo é equivalente, não ouro pago
	// Mortes do time que perdeu o objetivo logo antes (até JanelaMorteObjetivoMs).
	var mortos []string
	for _, rt := range f.retratos {
		if f.timeDe[rt.Vitima] == 300-o.time && rt.TMs <= o.t && rt.TMs >= o.t-Param.JanelaMorteObjetivoMs {
			mortos = append(mortos, f.campeao(rt.Vitima))
		}
	}
	m.Dados = map[string]any{"valor": o.valor, "sub": o.sub}
	var b strings.Builder
	verbo := "pegou"
	if o.tipo != "objetivo" {
		verbo = "derrubou"
	}
	fmt.Fprintf(&b, "O %s %s %s aos %s", NomeTime(o.time), verbo, o.nome, MinSeg(o.t))
	if len(o.participantes) > 0 {
		fmt.Fprintf(&b, " (%s)", f.nomes(o.participantes))
	}
	if len(mortos) > 0 {
		m.Tags = append(m.Tags, "depois_de_morte")
		m.Dados["mortos_antes"] = mortos
		fmt.Fprintf(&b, ", com %s do %s mortos pouco antes", juntaE(mortos), NomeTime(300-o.time))
	}
	fmt.Fprintf(&b, ". Valor equivalente ~%s.", ouroTexto(o.valor))
	m.Frase = b.String()
	return m
}

// freezes: os trechos "freeze" das ondas que a análise pôs na cadeia de cada
// jogador de rota (uma vez por rota e instante). O impacto é o CS que o time
// do freeze tirou a mais que o adversário no trecho (quadros de minuto) em
// ouro: estimado.
func (f *fatos) freezes() []MomentoChave {
	var l []MomentoChave
	visto := map[string]bool{}
	pids := make([]int, 0, len(f.dp.cadeias))
	for pid := range f.dp.cadeias {
		pids = append(pids, pid)
	}
	sort.Ints(pids)
	for _, pid := range pids {
		for _, it := range f.dp.cadeias[pid] {
			if it.Tipo != EOndaFreeze {
				continue
			}
			rota, _ := it.Dados["rota"].(string)
			k := fmt.Sprintf("%s/%d", rota, it.TMs)
			if visto[k] {
				continue
			}
			visto[k] = true
			fim := int64(numero(it.Dados["fim_ms"]))
			tm := int(numero(it.Dados["time"]))
			m := MomentoChave{TMs: it.TMs, FimMs: fim, DuracaoMs: fim - it.TMs, Tipo: "freeze", Subtipo: rota, Rota: rota, Zona: rota,
				Time: tm, Estimado: true, Envolvidos: []int{}}
			dono, outro := f.jogadorDaRota(rota, tm), f.jogadorDaRota(rota, 300-tm)
			for _, p := range []int{dono, outro} {
				if p > 0 {
					m.Envolvidos = append(m.Envolvidos, p)
				}
			}
			m.ParticipanteID = dono
			var difCS float64
			temCS := false
			if dono > 0 && outro > 0 {
				a0, ok1 := f.valorJogadorEm(dono, it.TMs, csTotal)
				a1, ok2 := f.valorJogadorEm(dono, fim, csTotal)
				b0, ok3 := f.valorJogadorEm(outro, it.TMs, csTotal)
				b1, ok4 := f.valorJogadorEm(outro, fim, csTotal)
				if ok1 && ok2 && ok3 && ok4 {
					difCS, temCS = (a1-a0)-(b1-b0), true
				}
			}
			if temCS && difCS > 0 {
				custoPara(&m, 300-tm, int(math.Round(difCS*Param.OuroPorCS)))
			}
			m.Dados = map[string]any{"confianca": it.Dados["confianca"]}
			var b strings.Builder
			fmt.Fprintf(&b, "Freeze do %s em %s de %s a %s (%s)", NomeTime(tm), nomeZona(rota), MinSeg(it.TMs), MinSeg(fim), segundosTexto(fim-it.TMs))
			if temCS && dono > 0 && outro > 0 {
				m.Dados["dif_cs"] = math.Round(difCS)
				fmt.Fprintf(&b, "; no trecho %s fez %s de CS a mais que %s", f.campeao(dono), strings.Replace(fmt.Sprintf("%+.0f", difCS), "-", "−", 1), f.campeao(outro))
			}
			b.WriteString(" (onda estimada pela posição dos jogadores).")
			m.Frase = b.String()
			l = append(l, m)
		}
	}
	return l
}

// jogadorDaRota: o jogador do time naquela rota (o atirador na de baixo).
func (f *fatos) jogadorDaRota(rota string, time int) int {
	quer := map[string]string{ZRotaTopo: "TOP", ZRotaMeio: "MIDDLE", ZRotaBaixo: "BOTTOM"}[rota]
	for _, p := range f.dp.pt.Participantes {
		if p.Time == time && p.Posicao == quer {
			return p.ParticipanteID
		}
	}
	return 0
}
