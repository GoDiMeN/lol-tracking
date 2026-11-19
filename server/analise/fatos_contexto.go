package analise

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"loltracking/server/banco"
)

// O PACOTE DE CONTEXTO: um texto compacto em português (Markdown simples), só
// com o que está na base de fatos, para entregar a uma IA depois (por ora, para
// um humano ler). Tem limite de tamanho (Param.LimiteContextoIA, em
// caracteres) e as seções entram na ORDEM DE PRIORIDADE abaixo: a que não
// couber inteira entra até onde der, e as seguintes ficam de fora (a última
// linha diz quais).

// ContextoIA é a resposta de GET /api/partidas/{id}/contexto-ia.
type ContextoIA struct {
	GameID         int64    `json:"game_id,omitempty"`
	ParticipanteID int      `json:"participante_id,omitempty"`
	Texto          string   `json:"texto"`
	Caracteres     int      `json:"caracteres"`
	Limite         int      `json:"limite"`
	Cortadas       []string `json:"cortadas"` // seções que não couberam (inteiras ou em parte)
}

// secao é um bloco do texto: título e linhas.
type secao struct {
	titulo string
	linhas []string
}

// montaComLimite junta as seções na ordem até o limite. Reserva espaço para a
// linha final que diz o que ficou de fora.
func montaComLimite(secoes []secao, limite int) (string, []string) {
	const reserva = 400
	var b strings.Builder
	cortadas := []string{}
	tam := 0
	cabe := func(s string) bool { return tam+utf8.RuneCountInString(s) <= limite-reserva }
	escreve := func(s string) {
		b.WriteString(s)
		tam += utf8.RuneCountInString(s)
	}
	parou := false
	for _, s := range secoes {
		if parou {
			cortadas = append(cortadas, s.titulo)
			continue
		}
		cab := ""
		if s.titulo != "" {
			cab = "\n## " + s.titulo + "\n"
		}
		if !cabe(cab) {
			parou = true
			cortadas = append(cortadas, s.titulo)
			continue
		}
		escreve(cab)
		for i, l := range s.linhas {
			if !cabe(l + "\n") {
				escreve(fmt.Sprintf("(… %d linha(s) cortada(s) pelo limite de tamanho)\n", len(s.linhas)-i))
				cortadas = append(cortadas, s.titulo+" (em parte)")
				parou = true
				break
			}
			escreve(l + "\n")
		}
	}
	if len(cortadas) > 0 {
		escreve("\n_Fora do pacote pelo limite de " + fmt.Sprint(limite) + " caracteres: " + strings.Join(cortadas, "; ") + "._\n")
	}
	return strings.TrimLeft(b.String(), "\n"), cortadas
}

func numOuTraco(p *int) string {
	if p == nil {
		return "—"
	}
	return fmt.Sprint(*p)
}

func difOuTraco(p *int) string {
	if p == nil {
		return "—"
	}
	return sinalOuro(*p)
}

// linhaJogador: a linha curta de um jogador ("Vi (caçador vermelho) 3/7/4 ...").
func linhaJogador(j ResumoJogador) string {
	res := "derrota"
	if j.Venceu {
		res = "vitória"
	}
	s := fmt.Sprintf("%s (%s %s, %s) %d/%d/%d, KDA %.2f; ouro aos 15 %s (%s contra o adversário); CS aos 15 %s (%s); mortes rotas/meio/final %d/%d/%d; mortes caras %d; objetivos %d de %d do time; sentinelas %d",
		j.Campeao, NomeFuncao(j.Funcao), NomeTime(j.Time), res, j.Abates, j.Mortes, j.Assistencias, j.KDA, numOuTraco(j.Ouro15),
		difOuTraco(j.DifOuro15), numOuTraco(j.CS15), difOuTraco(j.DifCS15), j.MortesRotas, j.MortesMeio, j.MortesFinal, j.MortesCaras,
		j.ObjetivosParticipou, j.ObjetivosTime, j.Sentinelas)
	if !j.Rastreado {
		s += "; não rastreado (só dados por minuto)"
	}
	return s
}

func textoContexto(c ContextoJogador, funcao string) string {
	var p []string
	if c.FuncaoPrincipal != "" {
		t := "função principal " + NomeFuncao(c.FuncaoPrincipal)
		if c.FracaoPrincipal > 0 {
			t += fmt.Sprintf(" (%.0f%% das partidas)", c.FracaoPrincipal*100)
		}
		if c.ForaDaFuncao != nil && *c.ForaDaFuncao {
			t += " (nesta partida FORA dela, como " + NomeFuncao(funcao) + ")"
		}
		p = append(p, t)
	}
	switch {
	case c.Mono != nil && *c.Mono:
		p = append(p, fmt.Sprintf("mono do campeão (%d das últimas %d com ele)", c.CampeaoVezes, c.CampeaoDe))
	case c.SemOCampeao != nil && *c.SemOCampeao:
		p = append(p, fmt.Sprintf("nenhuma das últimas %d com este campeão (talvez a primeira vez)", c.CampeaoDe))
	case c.CampeaoDe > 0 && c.Mono != nil:
		p = append(p, fmt.Sprintf("%d das últimas %d com este campeão", c.CampeaoVezes, c.CampeaoDe))
	}
	if c.PartidasGuardadas > 0 {
		p = append(p, fmt.Sprintf("%d partida(s) de antes desta (%s)", c.PartidasGuardadas, c.Fonte))
		if c.Ultimas > 0 {
			p = append(p, fmt.Sprintf("%d vitória(s) nas últimas %d", c.VitoriasUltimas, c.Ultimas))
		}
		switch {
		case c.Sequencia > 1:
			p = append(p, fmt.Sprintf("vinha de %d vitórias seguidas", c.Sequencia))
		case c.Sequencia < -1:
			p = append(p, fmt.Sprintf("vinha de %d derrotas seguidas", -c.Sequencia))
		}
	}
	if c.Elo != nil {
		e := "elo " + c.Elo.Elo
		if c.Elo.Divisao != "" {
			e += " " + c.Elo.Divisao
		}
		if c.Elo.EmSequencia {
			e += " (em sequência de vitórias)"
		}
		p = append(p, e)
	}
	if c.Alcance != "" {
		p = append(p, c.Alcance)
	}
	return strings.Join(p, "; ")
}

func topEtiquetas(e map[string]int, n int) string {
	type kv struct {
		k string
		v int
	}
	var l []kv
	for k, v := range e {
		if k != AMorto {
			l = append(l, kv{k, v})
		}
	}
	sort.Slice(l, func(a, b int) bool {
		if l[a].v != l[b].v {
			return l[a].v > l[b].v
		}
		return l[a].k < l[b].k
	})
	var p []string
	for i, x := range l {
		if i >= n {
			break
		}
		p = append(p, fmt.Sprintf("%s %s", NomeAtividade(x.k), segundosTexto(int64(x.v)*1000)))
	}
	if m, ok := e[AMorto]; ok && m > 0 {
		p = append(p, fmt.Sprintf("morto %s", segundosTexto(int64(m)*1000)))
	}
	return strings.Join(p, ", ")
}

func linhaMomento(m MomentoChave) string {
	s := fmt.Sprintf("- [%s] %s", MinSeg(m.TMs), m.Frase)
	if m.Tipo != "freeze" && m.Tipo != "janela_perdida" && m.Tipo != "janela_aproveitada" && m.SaldoAntes != nil && m.SaldoDepois != nil {
		s += fmt.Sprintf(" Saldo %s → %s.", sinalOuro(*m.SaldoAntes), sinalOuro(*m.SaldoDepois))
	}
	if m.Estimado {
		s += " (estimado)"
	}
	return s
}

var nomesTag = map[string]string{"sozinho": "sozinho", "inferioridade": "em inferioridade", "sem_troca": "sem troca",
	"r_em_recarga": "R em recarga", "flash_em_recarga": "Flash em recarga", "passiva_em_recarga": "passiva em recarga",
	"fora_da_rota": "fora da rota"}

// linhaMorte: a morte do jogador em foco, curta (a frase inteira fica no banco).
func linhaMorte(m MomentoChave, nomes map[int]string, comVitima bool) string {
	nome := func(p int) string {
		if n := nomes[p]; n != "" {
			return n
		}
		return fmt.Sprintf("jogador %d", p)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "- [%s] ", MinSeg(m.TMs))
	if m.Subtipo == "morte_cara" {
		b.WriteString("CARA: ")
	}
	if comVitima {
		fmt.Fprintf(&b, "%s (%s %s) ", nome(m.ParticipanteID), NomeFuncao(m.Funcao), NomeTime(300-m.Time))
	}
	if m.Autor > 0 && len(m.Envolvidos) > 1 {
		fmt.Fprintf(&b, "morreu para %s", nome(m.Autor))
	} else {
		b.WriteString("morreu")
	}
	if m.Zona != "" {
		fmt.Fprintf(&b, " em %s", nomeZona(m.Zona))
	}
	if m.AtividadePrevia != "" {
		fmt.Fprintf(&b, " (antes: %s)", NomeAtividade(m.AtividadePrevia))
	} else if m.Atividade != "" {
		fmt.Fprintf(&b, " (%s)", NomeAtividade(m.Atividade))
	}
	var tags []string
	for _, t := range m.Tags {
		if n, ok := nomesTag[t]; ok {
			tags = append(tags, n)
		}
	}
	if o, ok := m.Dados["objetivo_depois"].(string); ok {
		tags = append(tags, fmt.Sprintf("o inimigo levou %s aos %s", o, MinSeg(int64(numero(m.Dados["objetivo_depois_ms"])))))
	}
	if len(tags) > 0 {
		b.WriteString("; " + strings.Join(tags, ", "))
	}
	fmt.Fprintf(&b, "; custo ~%s (abate %s + %s em %s morto)", ouroTexto(m.CustoAzul+m.CustoVermelho), ouroTexto(int(numero(m.Dados["ouro_abate"]))),
		ouroTexto(max(0, int(numero(m.Dados["ouro_negado"])))), segundosTexto(int64(numero(m.Dados["morto_ms"]))))
	if m.SaldoAntes != nil && m.SaldoDepois != nil {
		fmt.Fprintf(&b, "; saldo %s → %s", sinalOuro(*m.SaldoAntes), sinalOuro(*m.SaldoDepois))
	}
	b.WriteString(".")
	return b.String()
}

func contextoOuNada(c ContextoJogador, funcao string) string {
	if t := textoContexto(c, funcao); t != "" {
		return t
	}
	return "nada guardado deste jogador fora desta partida"
}

// linhaJanela: a janela de oportunidade, curta (a frase inteira fica no banco).
func linhaJanela(m MomentoChave, nomes map[int]string) string {
	d := m.Dados
	nome := func(p int) string {
		if n := nomes[p]; n != "" {
			return n
		}
		return fmt.Sprintf("jogador %d", p)
	}
	x := int(numero(d["ausente"]))
	var b strings.Builder
	tipo := "PERDIDA"
	if m.Tipo == "janela_aproveitada" {
		tipo = "aproveitada"
	}
	fmt.Fprintf(&b, "- [%s] %s (%s, %s): %s ", MinSeg(m.TMs), tipo, m.Subtipo, segundosTexto(m.DuracaoMs), nome(x))
	if d["motivo"] == "morte" {
		b.WriteString("morreu")
		if m.Zona != "" {
			fmt.Fprintf(&b, " em %s", nomeZona(m.Zona))
		}
		fmt.Fprintf(&b, ", renasceu %s", MinSeg(int64(numero(d["renasceu_ms"]))))
	} else {
		b.WriteString("foi à base")
	}
	volta := "à selva"
	if m.Subtipo == "rota" {
		volta = "à rota"
	}
	fmt.Fprintf(&b, ", voltou %s %s", volta, MinSeg(m.FimMs))
	if est, _ := d["fim_estimado"].(bool); est {
		b.WriteString(" (est.)")
	}
	b.WriteString(".")
	if l, ok := d["acampamentos"].([]any); ok && len(l) > 0 {
		var vivos, nascem []string
		for _, a := range l {
			am, _ := a.(map[string]any)
			n := tipoAcampamento(fmt.Sprint(am["nome"]))
			switch am["estado"] {
			case "vivo":
				vivos = append(vivos, n)
			case "nasce_na_janela":
				nascem = append(nascem, n+" "+MinSeg(int64(numero(am["nasce_ms"]))))
			}
		}
		b.WriteString(" Acampamentos dele (est.): ")
		if len(vivos) > 0 {
			b.WriteString("vivos " + strings.Join(vivos, ", "))
		} else {
			b.WriteString("nenhum vivo")
		}
		if len(nascem) > 0 {
			b.WriteString("; nascem " + strings.Join(nascem, ", "))
		}
		b.WriteString(".")
	}
	eu := nome(m.ParticipanteID)
	if z, ok := d["zona_inicio"].(string); ok && z != "" {
		fmt.Fprintf(&b, " %s em %s", eu, nomeZona(z))
		if v, ok := d["dist_acampamento_vivo"].(float64); ok {
			fmt.Fprintf(&b, " (a %.0f do vivo mais perto)", v)
		}
	} else {
		fmt.Fprintf(&b, " %s", eu)
	}
	if a, ok := d["atividades"].(map[string]any); ok && len(a) > 0 {
		type kv struct {
			k string
			v float64
		}
		var l []kv
		for k, v := range a {
			if numero(v) >= 2000 {
				l = append(l, kv{k, numero(v)})
			}
		}
		sort.Slice(l, func(i, j int) bool {
			if l[i].v != l[j].v {
				return l[i].v > l[j].v
			}
			return l[i].k < l[j].k
		})
		var p []string
		for _, x := range l {
			p = append(p, fmt.Sprintf("%s %s", NomeAtividade(x.k), segundosTexto(int64(x.v))))
		}
		b.WriteString(": " + strings.Join(p, ", "))
	} else if r, _ := d["rastreado"].(bool); !r {
		b.WriteString(": não rastreado")
	}
	b.WriteString(".")
	if f, ok := d["feitos"].([]any); ok && len(f) > 0 {
		var p []string
		for _, x := range f {
			p = append(p, fmt.Sprint(x))
		}
		b.WriteString(" Fez: " + strings.Join(p, "; ") + ".")
	}
	fmt.Fprintf(&b, " Valor ~%s.", ouroTexto(int(numero(d["valor"]))))
	return b.String()
}

// MontaContextoIA monta o pacote de uma partida com um jogador em foco.
func MontaContextoIA(r ResumoPartida, pid int) (ContextoIA, error) {
	c := ContextoIA{GameID: r.GameID, ParticipanteID: pid, Limite: Param.LimiteContextoIA}
	var eu *ResumoJogador
	nomes := map[int]string{}
	for i := range r.Jogadores {
		nomes[r.Jogadores[i].ParticipanteID] = r.Jogadores[i].Campeao
		if r.Jogadores[i].ParticipanteID == pid {
			eu = &r.Jogadores[i]
		}
	}

	if eu == nil {
		return c, &erroPedido{fmt.Sprintf("o participante %d não está na base de fatos da partida", pid)}
	}
	var secoes []secao

	// 1. A partida.
	cab := secao{titulo: ""}
	cab.linhas = append(cab.linhas, fmt.Sprintf("# Partida %d — foco em %s (%s, %s)", r.GameID, eu.Campeao, NomeFuncao(eu.Funcao), eu.Nome),
		"", "Fatos calculados por regra (sem IA) a partir do que o app rastreou; \"estimado\"/\"est.\" marca dedução. Custo e impacto em ouro EQUIVALENTE: "+
			"abate = ouro do abate + o que a vítima deixou de ganhar morta; objetivo/torre = valor fixo do app. Morte CARA = sozinho, antes de objetivo "+
			"que o inimigo levou, ou com R/Flash/passiva em recarga sem troca. Janela = o adversário direto morto ou na base. Saldo = vantagem de ouro "+
			"azul − vermelho antes → depois.", "",
		"- "+r.Texto+".")
	if r.Patch != "" || r.Fila != "" {
		cab.linhas = append(cab.linhas, fmt.Sprintf("- Patch %s, %s; %d de 10 jogadores rastreados.", r.Patch, r.Fila, len(r.Rastreados)))
	}
	for _, t := range r.Times {
		d := "nenhum"
		if len(t.Dragoes) > 0 {
			d = strings.Join(t.Dragoes, ", ")
		}
		cab.linhas = append(cab.linhas, fmt.Sprintf("- Time %s%s: %d abates; dragões: %s; ancião %d; barões %d; arautos %d; vastilarvas %d; atakhan %d; torres %d; inibidores %d; placas %d; ouro final %s.",
			NomeTime(t.Time), map[bool]string{true: " (venceu)", false: ""}[t.Venceu], t.Abates, d, t.Anciao, t.Baroes, t.Arautos, t.Vastilarvas,
			t.Atakhan, t.Torres, t.Inibidores, t.Placas, ouroTexto(t.OuroFinal)))
	}
	var curva []string
	for _, p := range r.CurvaOuro {
		if p.TMs > 0 && (p.TMs+30000)/60000%5 == 0 {
			curva = append(curva, fmt.Sprintf("%d min %s", (p.TMs+30000)/60000, sinalOuro(p.Diferenca)))
		}
	}
	if len(curva) > 0 {
		cab.linhas = append(cab.linhas, "- Vantagem de ouro (azul − vermelho) a cada 5 min: "+strings.Join(curva, "; ")+".")
	}
	secoes = append(secoes, cab)

	// 2. O jogador em foco.
	sj := secao{titulo: fmt.Sprintf("Jogador em foco: %s", eu.Campeao)}
	sj.linhas = append(sj.linhas, "- "+linhaJogador(*eu)+".",
		"- Contexto: "+contextoOuNada(eu.Contexto, eu.Funcao)+".")
	if l := linhaPapel(eu.Papel); l != "" {
		sj.linhas = append(sj.linhas, "- Papel na partida (por regra): "+l)
	}
	if t, ok := eu.Etiquetas["total"]; ok && len(t) > 0 {
		sj.linhas = append(sj.linhas, "- Tempo por atividade (partida): "+topEtiquetas(t, 8)+".")
	}
	if t, ok := eu.Etiquetas["rotas"]; ok && len(t) > 0 {
		sj.linhas = append(sj.linhas, "- Tempo por atividade (até 14 min): "+topEtiquetas(t, 6)+".")
	}
	if eu.Adversario > 0 {
		for _, o := range r.Jogadores {
			if o.ParticipanteID == eu.Adversario {
				sj.linhas = append(sj.linhas, "- Adversário direto: "+linhaJogador(o)+".")
			}
		}
	}
	secoes = append(secoes, sj)

	// 3. As mortes dele; 4. as janelas dele; 5. os momentos-chave.
	sm := secao{titulo: fmt.Sprintf("Mortes de %s", eu.Campeao)}
	sjan := secao{titulo: fmt.Sprintf("Janelas de oportunidade de %s", eu.Campeao)}
	var chave []MomentoChave
	var outros []string
	for _, m := range r.Momentos {
		switch {
		case m.Tipo == "abate" && m.ParticipanteID == pid:
			sm.linhas = append(sm.linhas, linhaMorte(m, nomes, false))
		case (m.Tipo == "janela_perdida" || m.Tipo == "janela_aproveitada") && m.ParticipanteID == pid:
			sjan.linhas = append(sjan.linhas, linhaJanela(m, nomes))
		case m.Tipo == "abate" && contem(m.Envolvidos, pid):
			outros = append(outros, fmt.Sprintf("- [%s] %s", MinSeg(m.TMs), m.Frase))
		}
		if m.Chave {
			chave = append(chave, m)
		}
	}
	if len(sm.linhas) == 0 {
		sm.linhas = []string{"- Nenhuma morte."}
	}
	if len(sjan.linhas) == 0 {
		sjan.linhas = []string{"- Nenhuma janela (o adversário direto não ficou ausente o bastante, ou ele também estava morto)."}
	}
	secoes = append(secoes, sm, sjan)
	sort.SliceStable(chave, func(a, b int) bool { return chave[a].Impacto > chave[b].Impacto })
	sk := secao{titulo: "Momentos-chave da partida (maior impacto primeiro; custo em ouro equivalente)"}
	for k, m := range chave {
		if k >= 10 {
			break
		}
		l := linhaMomento(m)
		if m.Tipo == "abate" {
			l = strings.TrimSuffix(linhaMorte(m, nomes, true), ".") + "."
		}
		sk.linhas = append(sk.linhas, l+fmt.Sprintf(" Impacto ~%s.", ouroTexto(m.Impacto)))
	}
	secoes = append(secoes, sk)

	// 6. Jogadores-chave.
	js := append([]ResumoJogador{}, r.Jogadores...)
	sort.SliceStable(js, func(a, b int) bool { return js[a].PosicaoNota < js[b].PosicaoNota })
	sn := secao{titulo: "Jogadores-chave (nota de impacto em ouro equivalente, com os componentes)"}
	abrev := map[string]string{"abates": "abates", "objetivos": "objetivos", "mortes": "mortes", "mortes_caras": "caras",
		"momentos_chave": "momentos", "vantagem_rota": "rota"}
	for _, j := range js {
		var comp []string
		for _, x := range j.Componentes {
			comp = append(comp, fmt.Sprintf("%s %s", abrev[x.Nome], sinalOuro(x.Valor)))
		}
		l := fmt.Sprintf("%d. %s (%s %s): %s = %s", j.PosicaoNota, j.Campeao, NomeFuncao(j.Funcao), NomeTime(j.Time),
			sinalOuro(j.Nota), strings.Join(comp, ", "))
		if j.Papel.Papel != "" {
			l += "; papel: " + j.Papel.Rotulo
			if j.Papel.Estilo != "" {
				l += " (" + j.Papel.EstiloRotulo + ")"
			}
		}
		if t := textoContexto(j.Contexto, j.Funcao); t != "" {
			l += "; " + t
		}
		sn.linhas = append(sn.linhas, l+".")
	}
	secoes = append(secoes, sn)

	// 7. A build dele, lado a lado com o estado do jogo.
	sb := secao{titulo: fmt.Sprintf("Build de %s (fatos lado a lado, sem julgamento)", eu.Campeao)}
	for _, e := range eu.Build {
		l := fmt.Sprintf("- [%s] %s", MinSeg(e.TMs), nomeItem(e.Nome, e.ItemID))
		if len(e.Atributos) > 0 {
			l += " (" + strings.Join(e.Atributos, ", ") + ")"
		}
		l += "; placar dele " + e.Placar
		if e.VantagemTime != nil {
			l += "; time dele " + sinalOuro(*e.VantagemTime) + " de ouro"
		}
		if e.OuroJogador != nil && e.OuroAdversario != nil {
			l += fmt.Sprintf("; ele %s × adversário %s", ouroTexto(*e.OuroJogador), ouroTexto(*e.OuroAdversario))
		}
		if f := e.InimigoMaisForte; f != nil {
			l += fmt.Sprintf("; inimigo com mais ouro: %s (%s, %s; dano %d%% físico, %d%% mágico, %d%% verdadeiro na partida)",
				f.Campeao, ouroTexto(f.Ouro), f.Placar, f.FisicoPct, f.MagicoPct, f.VerdadeiroPct)
		}
		if d := e.DanoNasMortes; d != nil {
			l += fmt.Sprintf("; nas %d morte(s) até ali recebeu %d%% físico, %d%% mágico, %d%% verdadeiro", d.Mortes, d.FisicoPct, d.MagicoPct, d.VerdadeiroPct)
		}
		sb.linhas = append(sb.linhas, l+".")
	}
	if len(eu.Build) > 0 {
		if d := eu.Build[0].DanoTimeInimigo; d != nil {
			sb.linhas = append(sb.linhas, fmt.Sprintf("- Dano do time inimigo na partida: %d%% físico, %d%% mágico, %d%% verdadeiro.", d["fisico"], d["magico"], d["verdadeiro"]))
		}
	} else {
		sb.linhas = append(sb.linhas, "- Nenhum item completo identificado (sem os dados estáticos da versão, ou nenhum comprado).")
	}
	var ordem []string
	for _, it := range eu.Itens {
		ordem = append(ordem, fmt.Sprintf("%s %s", MinSeg(it.TMs), nomeItem(it.Nome, it.ItemID)))
	}
	if len(ordem) > 0 {
		sb.linhas = append(sb.linhas, "- Todas as compras: "+strings.Join(ordem, "; ")+".")
	}
	secoes = append(secoes, sb)

	// 8. Os outros; 9. os outros momentos dele.
	so := secao{titulo: "Os outros jogadores"}
	for _, j := range r.Jogadores {
		if j.ParticipanteID != pid {
			so.linhas = append(so.linhas, "- "+linhaJogador(j)+".")
		}
	}
	secoes = append(secoes, so)
	if len(outros) > 0 {
		secoes = append(secoes, secao{titulo: fmt.Sprintf("Abates de que %s participou", eu.Campeao), linhas: outros})
	}
	c.Texto, c.Cortadas = montaComLimite(secoes, c.Limite)
	c.Caracteres = utf8.RuneCountInString(c.Texto)
	return c, nil
}

func nomeItem(nome string, id int) string {
	if nome != "" {
		return nome
	}
	return fmt.Sprintf("item %d", id)
}

// ContextoDaPartida lê a base de fatos e monta o pacote.
func ContextoDaPartida(db *banco.DB, gameID int64, pid int) (ContextoIA, error) {
	r, err := LeFatos(db, gameID)
	if err != nil {
		return ContextoIA{}, err
	}
	return MontaContextoIA(r, pid)
}

// ContextoDeVariasPartidas: uma linha curta por partida (e jogador) que casa
// com o filtro, para a conversa geral escolher partidas sem abrir nenhum
// arquivo. Com filtro de momento, as frases dos momentos que casaram vão junto.
func ContextoDeVariasPartidas(db *banco.DB, f FiltroResumos) (ContextoIA, error) {
	c := ContextoIA{Limite: Param.LimiteContextoIA}
	ls, err := BuscaResumos(db, f)
	if err != nil {
		return c, err
	}
	s := secao{titulo: fmt.Sprintf("Partidas na base de fatos (%d linha(s) que casam com o filtro)", len(ls))}
	for _, l := range ls {
		linha := "- " + l.TextoPartida
		if j := l.Jogador; j != nil {
			linha += fmt.Sprintf(" | %s %d/%d/%d, %s; nota %s (%dª de 10); mortes caras %d; ouro aos 15 %s contra o adversário",
				fmt.Sprintf("%s (%s %s)", j.Campeao, NomeFuncao(j.Funcao), NomeTime(j.Time)), j.Abates, j.Mortes, j.Assistencias,
				map[bool]string{true: "vitória", false: "derrota"}[j.Venceu], sinalOuro(j.Nota), j.PosicaoNota, j.MortesCaras, difOuTraco(j.DifOuro15))
			if j.Papel.Papel != "" {
				linha += "; papel: " + j.Papel.Frase
			}
		}
		for _, m := range l.Momentos {
			linha += fmt.Sprintf(" | [%s] %s", MinSeg(m.TMs), m.Frase)
		}
		s.linhas = append(s.linhas, linha)
	}
	if len(ls) == 0 {
		s.linhas = []string{"- Nenhuma partida casa com o filtro."}
	}
	c.Texto, c.Cortadas = montaComLimite([]secao{{linhas: []string{"# Base de fatos de várias partidas", "",
		"Uma linha por partida (e jogador), só do banco; para detalhes, o pacote de uma partida."}}, s}, c.Limite)
	c.Caracteres = utf8.RuneCountInString(c.Texto)
	return c, nil
}
