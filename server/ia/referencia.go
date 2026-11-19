package ia

import (
	"fmt"
	"sort"
	"strings"

	"loltracking/server/analise"
)

// AS VOZES POR DADO (mono, melhores do elo, Challenger, Esmeralda): o material
// delas não é a linha-resumo da partida de referência, é a COMPARAÇÃO. Para o
// jogador em foco e cada jogador de referência da coleta por elo (mesma
// função; e mesmo elo ou mesmo campeão, conforme a voz), o app põe os mesmos
// indicadores lado a lado, só do banco (analise.Referencias), e, para cada
// situação candidata, os indicadores que tocam aquela situação. A amostra vai
// sempre escrita ("1 partida de 1 jogador de Platina II na selva"): a voz tem de
// dizê-la e, se é pequena, não generalizar (prompt.go; ValidaRelatorio confere).

// MaxColunasReferencia: quantos jogadores de referência entram na tabela.
const MaxColunasReferencia = 6

// AmostraPequena: abaixo disto a voz diz que a amostra é pequena.
const AmostraPequena = 5

// vozPorDado: as vozes que falam pelas partidas da coleta.
func vozPorDado(id string) bool {
	return id == "mono" || id == "melhores_do_elo" || id == "challenger" || id == "esmeralda_recem"
}

var funcaoNaFrase = map[string]string{"TOP": "no topo", "JUNGLE": "na selva", "MIDDLE": "no meio", "BOTTOM": "como atirador",
	"UTILITY": "como suporte"}

// FuncaoNaFrase: "JUNGLE" → "na selva".
func FuncaoNaFrase(f string) string {
	if s, ok := funcaoNaFrase[f]; ok {
		return s
	}
	return "sem função"
}

// refVoz: o filtro de referência de uma voz e como dizê-lo.
type refVoz struct {
	filtro analise.FiltroReferencia
	// descr: o filtro em palavras ("vitórias da coleta em Platina, na selva").
	descr string
	// falta: o que falta quando não há referência (tela e relatório).
	falta string
}

// referenciaDaVoz: nil quando a voz não é por dado. Com o elo de comparação
// desconhecido, a voz dos melhores do elo não tem filtro (só a falta).
func referenciaDaVoz(v Voz, foco Participante, elo EloComparacao, gameID int64) *refVoz {
	fn := FuncaoNaFrase(foco.Funcao)
	base := analise.FiltroReferencia{Funcao: foco.Funcao, ExcluiGame: gameID, ExcluiPuuid: foco.Puuid, Limite: 10}
	r := &refVoz{filtro: base}
	switch v.ID {
	case "mono":
		r.filtro.Campeao = foco.Campeao
		r.descr = "partidas da coleta por elo (qualquer elo) com " + foco.Campeao + " " + fn
		r.falta = "faltam partidas de referência de " + foco.Campeao + " " + fn + ": rode a coleta por elo até entrar uma partida com esse campeão nessa função"
	case "melhores_do_elo":
		if elo.Elo == "" {
			r.falta = "o elo de comparação deste jogador não é conhecido: escolha \"comparar com o elo\" na tela da análise (ou ponha a chave da Riot)"
			r.filtro.Funcao = ""
			return r
		}
		r.filtro.Elo, r.filtro.Resultado = elo.Elo, "vitoria"
		r.descr = "vitórias da coleta por elo em " + NomeElo(elo.Elo) + " " + fn
		r.falta = "faltam vitórias de " + NomeElo(elo.Elo) + " " + fn + " na coleta: rode a coleta por elo em " + NomeElo(elo.Elo)
	case "challenger":
		r.filtro.Elo = "CHALLENGER"
		r.descr = "partidas da coleta por elo em Desafiante " + fn
		r.falta = "faltam partidas de Desafiante " + fn + ": rode a coleta por elo em Desafiante"
	case "esmeralda_recem":
		r.filtro.Elo = "EMERALD"
		r.descr = "partidas da coleta por elo em Esmeralda " + fn + " (o app não sabe se o jogador acabou de subir)"
		r.falta = "faltam partidas de Esmeralda " + fn + ": rode a coleta por elo em Esmeralda"
	default:
		return nil
	}
	if foco.Funcao == "" {
		r.filtro.Funcao = ""
		r.falta = "o jogador em foco não tem função nesta partida: sem função não há com quem comparar"
	}
	return r
}

func plural(n int, um, varios string) string {
	if n == 1 {
		return "1 " + um
	}
	return fmt.Sprintf("%d %s", n, varios)
}

// Amostra: "1 partida de 1 jogador de Platina II na selva".
func Amostra(refs []analise.IndicadoresJogador, funcao string) string {
	jogos, quem, elos := map[int64]bool{}, map[string]bool{}, []string{}
	visto := map[string]bool{}
	for _, r := range refs {
		jogos[r.GameID] = true
		k := r.Puuid
		if k == "" {
			k = fmt.Sprint(r.GameID, ":", r.ParticipanteID)
		}
		quem[k] = true
		e := NomeElo(r.Elo)
		if e == "" {
			e = NomeElo(r.EloPartida)
		}
		if !visto[e] {
			visto[e] = true
			elos = append(elos, e)
		}
	}
	sort.Strings(elos)
	return fmt.Sprintf("%s de %s de %s %s", plural(len(jogos), "partida", "partidas"), plural(len(quem), "jogador", "jogadores"),
		strings.Join(elos, " e "), FuncaoNaFrase(funcao))
}

func decimal(v float64) string {
	return strings.Replace(fmt.Sprintf("%.1f", v), ".", ",", 1)
}

func porDezMin(n, duracaoS int) string {
	if duracaoS <= 0 {
		return "—"
	}
	return decimal(float64(n) * 600 / float64(duracaoS))
}

func intOuTraco(p *int) string {
	if p == nil {
		return "—"
	}
	return fmt.Sprint(*p)
}

func sinal(p *int) string {
	if p == nil {
		return "—"
	}
	return fmt.Sprintf("%+d", *p)
}

// segundosFase: segundos na atividade e % da fase (fase "total" = a partida).
func segundosFase(x analise.IndicadoresJogador, fase string, atvs ...string) (int, int) {
	m := x.Etiquetas[fase]
	tot, n := 0, 0
	for a, s := range m {
		tot += s
		for _, q := range atvs {
			if a == q {
				n += s
			}
		}
	}
	if tot == 0 {
		return 0, 0
	}
	return n, (n*100 + tot/2) / tot
}

// DependemDosOutros: as atividades que só existem com os outros jogadores
// rastreados (inimigo ou aliado rastreado perto; dados.md, "Atividade"). Numa
// partida com menos de 10 rastreados (a da coleta tem 1) elas não aparecem e o
// tempo delas vira andando/farmando: comparar dá um falso zero.
var DependemDosOutros = map[string]bool{"lutando": true, "trocando": true, "dive": true, "agrupado": true, "gankando": true,
	"roaming": true, "rotacionando": true, "counter_gank": true, "split_push": true}

func tempoTexto(x analise.IndicadoresJogador, fase string, atvs ...string) string {
	if !x.Rastreado || len(x.Etiquetas[fase]) == 0 {
		return "— (sem atividade rastreada)"
	}
	if x.RastreadosNaPartida < 10 {
		for _, a := range atvs {
			if DependemDosOutros[a] {
				return fmt.Sprintf("n/d (só %d rastreado(s) na partida)", x.RastreadosNaPartida)
			}
		}
	}
	s, p := segundosFase(x, fase, atvs...)
	return fmt.Sprintf("%d s (%d%%)", s, p)
}

func nomeFase(f string) string {
	switch f {
	case "rotas":
		return fmt.Sprintf("fase das rotas (0:00–%s)", analise.MinSeg(analise.Param.FaseRotasMs))
	case "meio":
		return fmt.Sprintf("meio de jogo (%s–%s)", analise.MinSeg(analise.Param.FaseRotasMs), analise.MinSeg(analise.Param.FaseMeioMs))
	}
	return fmt.Sprintf("fim de jogo (depois de %s)", analise.MinSeg(analise.Param.FaseMeioMs))
}

// linhaIndicador é uma linha da tabela.
type linhaIndicador struct {
	nome string
	val  func(x analise.IndicadoresJogador) string
}

func indicadoresFixos() []linhaIndicador {
	rastreada := func(x analise.IndicadoresJogador, n int) string {
		if !x.Rastreado {
			return "— (não rastreado)"
		}
		return fmt.Sprint(n)
	}
	return []linhaIndicador{
		{"resultado e duração", func(x analise.IndicadoresJogador) string {
			r := "derrota"
			if x.Venceu {
				r = "vitória"
			}
			return r + ", " + analise.MinSeg(int64(x.DuracaoS)*1000)
		}},
		{"papel na partida (por regra)", func(x analise.IndicadoresJogador) string {
			if x.Papel.Papel == "" {
				return "—"
			}
			s := x.Papel.Rotulo
			if x.Papel.Estilo != "" {
				s += " · " + x.Papel.EstiloRotulo
			}
			return s
		}},
		{"abates/mortes/assistências (KDA)", func(x analise.IndicadoresJogador) string {
			return fmt.Sprintf("%d/%d/%d (%s)", x.Abates, x.Mortes, x.Assistencias, decimal(x.KDA))
		}},
		{"mortes por 10 min", func(x analise.IndicadoresJogador) string { return porDezMin(x.Mortes, x.DuracaoS) }},
		{"mortes nas rotas / meio / fim", func(x analise.IndicadoresJogador) string {
			return fmt.Sprintf("%d / %d / %d", x.MortesRotas, x.MortesMeio, x.MortesFinal)
		}},
		{"mortes sozinho (aliado longe)", func(x analise.IndicadoresJogador) string { return rastreada(x, x.MortesSozinho) }},
		{"mortes em inferioridade numérica", func(x analise.IndicadoresJogador) string { return rastreada(x, x.MortesInferioridade) }},
		{"mortes sem troca (o time não matou ninguém)", func(x analise.IndicadoresJogador) string { return fmt.Sprint(x.MortesSemTroca) }},
		{"mortes antes de objetivo inimigo", func(x analise.IndicadoresJogador) string { return fmt.Sprint(x.MortesAntesObjetivo) }},
		{"mortes caras", func(x analise.IndicadoresJogador) string { return fmt.Sprint(x.MortesCaras) }},
		{"ouro aos 10 / 15 min", func(x analise.IndicadoresJogador) string { return intOuTraco(x.Ouro10) + " / " + intOuTraco(x.Ouro15) }},
		{"ouro − adversário direto aos 10 / 15", func(x analise.IndicadoresJogador) string { return sinal(x.DifOuro10) + " / " + sinal(x.DifOuro15) }},
		{"CS aos 10 / 15 min", func(x analise.IndicadoresJogador) string { return intOuTraco(x.CS10) + " / " + intOuTraco(x.CS15) }},
		{"CS − adversário direto aos 10 / 15", func(x analise.IndicadoresJogador) string { return sinal(x.DifCS10) + " / " + sinal(x.DifCS15) }},
		{"objetivos do time em que participou", func(x analise.IndicadoresJogador) string {
			if x.ObjetivosTime == 0 {
				return "0 de 0"
			}
			return fmt.Sprintf("%d de %d (%d%%)", x.ObjetivosParticipou, x.ObjetivosTime, (x.ObjetivosParticipou*100+x.ObjetivosTime/2)/x.ObjetivosTime)
		}},
		{"sentinelas colocadas (de controle) e por 10 min", func(x analise.IndicadoresJogador) string {
			return fmt.Sprintf("%d (%d) · %s/10 min", x.Sentinelas, x.SentinelasControle, porDezMin(x.Sentinelas, x.DuracaoS))
		}},
		{"janelas aproveitadas / perdidas (adversário direto morto)", func(x analise.IndicadoresJogador) string {
			if !x.Rastreado {
				return "— (não rastreado)"
			}
			return fmt.Sprintf("%d / %d", x.JanelasAproveitadas, x.JanelasPerdidas)
		}},
		{"gankando, partida inteira", func(x analise.IndicadoresJogador) string { return tempoTexto(x, "total", "gankando") }},
		{"invadindo, partida inteira", func(x analise.IndicadoresJogador) string { return tempoTexto(x, "total", "invadindo") }},
	}
}

func celula(s string) string { return strings.ReplaceAll(s, "|", "/") }

// tabelaComparacao: o jogador em foco e as referências, lado a lado.
func tabelaComparacao(eu analise.IndicadoresJogador, refs []analise.IndicadoresJogador) string {
	return tabelaComRotulos(eu, refs, fmt.Sprintf("FOCO: %s (esta partida)", eu.Campeao), func(i int, r analise.IndicadoresJogador) string {
		return fmt.Sprintf("ref. %d: %s, %s, partida %d", i+1, r.Campeao, NomeElo(r.Elo), r.GameID)
	})
}

// tabelaComRotulos: a mesma tabela com os cabeçalhos escolhidos por quem
// chama (o Especialista Lane põe a referência na primeira coluna e os
// jogadores do usuário nas outras).
func tabelaComRotulos(eu analise.IndicadoresJogador, refs []analise.IndicadoresJogador, cabEu string, cabOutro func(int, analise.IndicadoresJogador) string) string {
	var b strings.Builder
	cols := refs
	if len(cols) > MaxColunasReferencia {
		cols = cols[:MaxColunasReferencia]
	}
	cab := []string{"indicador", cabEu}
	for i, r := range cols {
		cab = append(cab, cabOutro(i, r))
	}
	b.WriteString("| " + strings.Join(cab, " | ") + " |\n|" + strings.Repeat("---|", len(cab)) + "\n")
	linha := func(nome string, f func(analise.IndicadoresJogador) string) {
		c := []string{nome, celula(f(eu))}
		for _, r := range cols {
			c = append(c, celula(f(r)))
		}
		b.WriteString("| " + strings.Join(c, " | ") + " |\n")
	}
	for _, l := range indicadoresFixos() {
		linha(l.nome, l.val)
	}
	// Tempo por atividade em cada fase: as atividades com pelo menos 3% da
	// fase em alguém da tabela, na ordem do maior tempo do jogador em foco.
	todos := append([]analise.IndicadoresJogador{eu}, cols...)
	for _, fs := range analise.Fases {
		peso := map[string]int{}
		for _, x := range todos {
			for a := range x.Etiquetas[fs] {
				if _, p := segundosFase(x, fs, a); p >= 3 {
					peso[a], _ = segundosFase(eu, fs, a)
				}
			}
		}
		var atvs []string
		for a := range peso {
			atvs = append(atvs, a)
		}
		sort.Slice(atvs, func(i, j int) bool {
			if peso[atvs[i]] != peso[atvs[j]] {
				return peso[atvs[i]] > peso[atvs[j]]
			}
			return atvs[i] < atvs[j]
		})
		for _, a := range atvs {
			linha(nomeFase(fs)+": "+strings.ReplaceAll(a, "_", " "), func(x analise.IndicadoresJogador) string { return tempoTexto(x, fs, a) })
		}
	}
	if len(refs) > len(cols) {
		fmt.Fprintf(&b, "(mais %d referência(s) não cabem na tabela)\n", len(refs)-len(cols))
	}
	return b.String()
}

// comparacaoPorSituacao: para cada situação candidata (S1, S2... na ordem do
// pedido), os indicadores que tocam o lance, do foco e de cada referência.
func comparacaoPorSituacao(eu analise.IndicadoresJogador, refs []analise.IndicadoresJogador, sits []analise.MomentoChave) string {
	if len(sits) == 0 {
		return ""
	}
	cols := refs
	if len(cols) > MaxColunasReferencia {
		cols = cols[:MaxColunasReferencia]
	}
	par := func(nome string, f func(analise.IndicadoresJogador) string) string {
		p := []string{eu.Campeao + " (foco) " + f(eu)}
		for i, r := range cols {
			p = append(p, fmt.Sprintf("ref. %d (%s) %s", i+1, r.Campeao, f(r)))
		}
		return nome + ": " + strings.Join(p, " · ")
	}
	contaFase := func(fs string) func(analise.IndicadoresJogador) string {
		return func(x analise.IndicadoresJogador) string {
			switch fs {
			case "rotas":
				return fmt.Sprint(x.MortesRotas)
			case "meio":
				return fmt.Sprint(x.MortesMeio)
			}
			return fmt.Sprint(x.MortesFinal)
		}
	}
	var b strings.Builder
	b.WriteString("Comparação por situação (o número do jogador em foco NESTA partida ao lado do de cada referência NA PARTIDA DELA; não é o mesmo lance, é o mesmo tipo de coisa):\n")
	for i, m := range sits {
		fs := analise.Fase(m.TMs)
		var ps []string
		tem := func(t string) bool { return contem2(m.Tags, t) }
		switch m.Tipo {
		case "abate":
			ps = append(ps, par("mortes no "+nomeFase(fs), contaFase(fs)))
			if tem("sozinho") {
				ps = append(ps, par("mortes sozinho na partida", func(x analise.IndicadoresJogador) string { return fmt.Sprint(x.MortesSozinho) }))
			}
			if tem("inferioridade") {
				ps = append(ps, par("mortes em inferioridade", func(x analise.IndicadoresJogador) string { return fmt.Sprint(x.MortesInferioridade) }))
			}
			if tem("sem_troca") {
				ps = append(ps, par("mortes sem troca", func(x analise.IndicadoresJogador) string { return fmt.Sprint(x.MortesSemTroca) }))
			}
			if tem("antes_de_objetivo") {
				ps = append(ps, par("mortes antes de objetivo inimigo", func(x analise.IndicadoresJogador) string { return fmt.Sprint(x.MortesAntesObjetivo) }))
			}
			if m.Subtipo == "morte_cara" {
				ps = append(ps, par("mortes caras", func(x analise.IndicadoresJogador) string { return fmt.Sprint(x.MortesCaras) }))
			}
		case "janela_perdida", "janela_aproveitada":
			ps = append(ps, par("janelas aproveitadas / perdidas", func(x analise.IndicadoresJogador) string {
				return fmt.Sprintf("%d / %d", x.JanelasAproveitadas, x.JanelasPerdidas)
			}))
			ps = append(ps, par("invadindo no "+nomeFase(fs), func(x analise.IndicadoresJogador) string { return tempoTexto(x, fs, "invadindo") }))
			ps = append(ps, par("gankando no "+nomeFase(fs), func(x analise.IndicadoresJogador) string { return tempoTexto(x, fs, "gankando") }))
		case "objetivo", "luta":
			ps = append(ps, par("objetivos do time em que participou", func(x analise.IndicadoresJogador) string {
				return fmt.Sprintf("%d de %d", x.ObjetivosParticipou, x.ObjetivosTime)
			}))
			ps = append(ps, par("mortes antes de objetivo inimigo", func(x analise.IndicadoresJogador) string { return fmt.Sprint(x.MortesAntesObjetivo) }))
			ps = append(ps, par("em objetivo no "+nomeFase(fs), func(x analise.IndicadoresJogador) string { return tempoTexto(x, fs, "objetivo") }))
		}
		for _, t := range m.Tags {
			if a, ok := strings.CutPrefix(t, "previa_"); ok {
				ps = append(ps, par(strings.ReplaceAll(a, "_", " ")+" no "+nomeFase(fs), func(x analise.IndicadoresJogador) string { return tempoTexto(x, fs, a) }))
			}
		}
		fmt.Fprintf(&b, "- S%d [%s] %s: %s.\n", i+1, analise.MinSeg(m.TMs), m.Tipo, strings.Join(ps, "; "))
	}
	return b.String()
}

// materialReferencia monta o material por dado de uma voz.
func materialReferencia(eu analise.IndicadoresJogador, refs []analise.IndicadoresJogador, sits []analise.MomentoChave, r *refVoz) (string, string) {
	amostra := Amostra(refs, eu.Funcao)
	var b strings.Builder
	fmt.Fprintf(&b, "#### Referência por dado (%s)\n", r.descr)
	fmt.Fprintf(&b, "AMOSTRA: %s.", amostra)
	if n := len(refs); n < AmostraPequena {
		b.WriteString(" AMOSTRA PEQUENA: diga o tamanho da amostra na resposta e NÃO generalize (\"na partida de referência, o caçador fez X\", nunca \"os melhores fazem X\").")
	} else {
		b.WriteString(" Diga o tamanho da amostra na resposta.")
	}
	b.WriteString(" Só entra a mesma função: não compare com outra. Ao comparar, diga como a referência jogou pelo papel dela" +
		" (a linha \"papel\" de cada referência: \"o Kayn de referência carregou: 24% do ouro do time...\").\n")
	if eu.RastreadosNaPartida < 10 || algumParcial(refs) {
		b.WriteString("ATENÇÃO: na partida da coleta só o jogador escolhido foi rastreado. Luta, troca, dive, gank, roaming, rotação, " +
			"agrupado e split push precisam dos outros rastreados: ali ficam \"n/d\" e o tempo deles aparece como andando/farmando " +
			"(não compare essas linhas nem conclua pelo andando/farmando a mais). Morte sozinho/em inferioridade usa a posição dos " +
			"outros pela linha do tempo da Riot (por minuto).\n")
	}
	b.WriteString("Partidas de referência (o jogador escolhido pela coleta, rastreado; nomes de invocador omitidos):\n")
	for i, x := range refs {
		if i >= MaxColunasReferencia {
			break
		}
		fmt.Fprintf(&b, "- ref. %d: %s %s, %s, patch %s; %s\n", i+1, x.Campeao, FuncaoNaFrase(x.Funcao), NomeElo(x.Elo), x.Patch, x.TextoPartida)
		// Como a referência jogou (papel por regra), para a voz dizer "o Kayn de
		// referência carregou: 24% do ouro do time..." e não só os números soltos.
		if f := FrasePapelReferencia(x); f != "" {
			fmt.Fprintf(&b, "  papel: %s\n", f)
		}
	}
	b.WriteString("\n")
	b.WriteString(tabelaComparacao(eu, refs))
	b.WriteString("\n")
	b.WriteString(comparacaoPorSituacao(eu, refs, sits))
	return strings.TrimSpace(b.String()), amostra
}

func algumParcial(refs []analise.IndicadoresJogador) bool {
	for _, r := range refs {
		if r.RastreadosNaPartida < 10 {
			return true
		}
	}
	return false
}

// FrasePapelReferencia: "o Kayn de referência carregou: 24% do ouro do time,
// 29% do dano a campeões do time, ...; estilo invasão / controle da selva:
// ..." (o papel por regra, analise/papel.go). "" sem papel calculado.
func FrasePapelReferencia(x analise.IndicadoresJogador) string {
	p := x.Papel
	if p.Papel == "" {
		return ""
	}
	verbo := map[string]string{"carregou": "carregou", "habilitou": "habilitou o time", "foi_carregado": "foi carregado",
		"afundou": "afundou", "estavel": "jogou estável"}[p.Papel]
	s := fmt.Sprintf("o %s de referência %s (confiança %s)", x.Campeao, verbo, strings.Replace(p.Confianca, "media", "média", 1))
	if len(p.Motivos) > 0 {
		s += ": " + strings.Join(p.Motivos, ", ")
	}
	if p.Estilo != "" {
		s += "; estilo " + p.EstiloRotulo
		if len(p.MotivosEstilo) > 0 {
			s += ": " + strings.Join(p.MotivosEstilo, "; ")
		}
	}
	if len(p.NaoAvaliado) > 0 {
		s += " (não avaliado: " + strings.Join(p.NaoAvaliado, "; ") + ")"
	}
	return s
}
