package ia

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"unicode/utf8"

	"loltracking/server/analise"
	"loltracking/server/banco"
	"loltracking/server/web"
)

// O RELATÓRIO DE REFERÊNCIA: uma partida da coleta por elo e o jogador de
// referência dela. A IA recebe o pacote de contexto, a visão, as fichas dos
// lances de que ele participou (os 10 no instante, ondas, sentinelas, visão,
// objetivos antes e depois) e a comparação com os jogadores do usuário; o app
// valida (instantes, regra no formato, quadros) e escreve o Markdown.

// RelatorioReferencia é a resposta de GET /api/especialista/relatorios/{game}/{pid}.
type RelatorioReferencia struct {
	ID             int64               `json:"id"`
	GameID         int64               `json:"game_id"`
	ParticipanteID int                 `json:"participante_id"`
	Versao         int                 `json:"versao"`
	Estado         string              `json:"estado"`
	Erro           string              `json:"erro"`
	CriadoEm       string              `json:"criado_em"`
	TerminadoEm    string              `json:"terminado_em"`
	Modelo         string              `json:"modelo"`
	Implementacao  string              `json:"implementacao"`
	VersaoPrompt   int                 `json:"versao_prompt"`
	Patch          string              `json:"patch"`
	Elo            string              `json:"elo"`
	EloTier        string              `json:"elo_tier"`
	Funcao         string              `json:"rota"`
	Campeao        string              `json:"campeao"`
	Venceu         bool                `json:"venceu"`
	Rastreados     int                 `json:"rastreados"`
	Conteudo       *ConteudoReferencia `json:"conteudo"`
	Avisos         []string            `json:"avisos"`
	Enviado        Enviado             `json:"enviado"`
	Arquivo        string              `json:"arquivo"`
	Uso            Resposta            `json:"uso"`
	Bruto          string              `json:"bruto,omitempty"`
	puuid          string
}

// ConteudoReferencia: o que o relatório de referência diz.
type ConteudoReferencia struct {
	Quem     QuemReferencia `json:"quem"`
	Resumo   string         `json:"resumo"`
	Fases    []FasePlano    `json:"fases"`
	Decisoes []Decisao      `json:"decisoes"`
	Erros    []ErroRef      `json:"erros"`
	NaoFez   []NaoFez       `json:"nao_fez"`
	// Comparacao: com quem o "não fez" compara (amostra) ou por que não há.
	Comparacao string `json:"comparacao"`
	// NaoDaParaSaber: o que os dados não respondem (a IA e o app escrevem).
	NaoDaParaSaber []string `json:"nao_da_para_saber"`
}

// QuemReferencia: o cabeçalho, escrito pelo app (não pela IA).
type QuemReferencia struct {
	Participante int    `json:"participante"`
	Campeao      string `json:"campeao"`
	Funcao       string `json:"rota"`
	Time         int    `json:"time"`
	Elo          string `json:"elo"`
	Patch        string `json:"patch"`
	Venceu       bool   `json:"venceu"`
	DuracaoS     int    `json:"duracao_s"`
	Rastreados   int    `json:"rastreados"`
	Papel        string `json:"papel"`
	Estilo       string `json:"estilo"`
	FrasePapel   string `json:"frase_papel"`
}

// FasePlano: o plano numa fase (rotas, meio, final) e as ações.
type FasePlano struct {
	Fase  string      `json:"fase"`
	Plano string      `json:"plano"`
	Acoes []AcaoPlano `json:"acoes"`
}

// AcaoPlano: o que ele fez e em resposta a quê.
type AcaoPlano struct {
	T           string `json:"t"`
	TMs         int64  `json:"t_ms"`
	Fez         string `json:"fez"`
	EmRespostaA string `json:"em_resposta_a"`
	Fonte       string `json:"fonte"`
}

// Decisao: uma decisão que pesou e a regra tirada dela.
type Decisao struct {
	T      string `json:"t"`
	TMs    int64  `json:"t_ms"`
	Titulo string `json:"titulo"`
	Fatos  string `json:"fatos"`
	Peso   string `json:"peso"`
	Regra  string `json:"regra"`
	// Casos: os instantes desta partida em que a mesma regra se aplicou.
	Casos []string `json:"casos"`
	// Hipotese: a regra vem de um caso só (o app decide pelos casos).
	Hipotese bool    `json:"hipotese"`
	Fonte    string  `json:"fonte"`
	Quadro   *Quadro `json:"quadro"`
}

// ErroRef: um erro do jogador de referência.
type ErroRef struct {
	T      string  `json:"t"`
	TMs    int64   `json:"t_ms"`
	Fato   string  `json:"fato"`
	Custo  string  `json:"custo"`
	Licao  string  `json:"licao"`
	Fonte  string  `json:"fonte"`
	Quadro *Quadro `json:"quadro"`
}

// NaoFez: o que ele não fez que os jogadores do usuário fazem (com números).
type NaoFez struct {
	Texto   string `json:"texto"`
	Numeros string `json:"numeros"`
	Fonte   string `json:"fonte"`
}

// MaxLances: quantos lances com ficha o pedido leva.
const MaxLances = 10

// lancesDaReferencia: os momentos de que o jogador participou (abates dele e
// nele, lutas, objetivos, torres, janelas), por fase e por impacto (até
// MaxLances).
func lancesDaReferencia(info InfoPartida, pid int) []analise.MomentoChave {
	var todos []analise.MomentoChave
	for _, m := range info.Momentos {
		envolvido := m.ParticipanteID == pid || m.Autor == pid || contem(m.Envolvidos, pid)
		switch m.Tipo {
		case "abate", "luta", "objetivo", "torre", "inibidor":
		case "janela_aproveitada", "janela_perdida":
			envolvido = m.ParticipanteID == pid
		default:
			continue
		}
		if envolvido {
			todos = append(todos, m)
		}
	}
	sort.SliceStable(todos, func(a, b int) bool { return todos[a].Impacto > todos[b].Impacto })
	grupo := func(t string) string {
		if t == "abate" {
			return "abate"
		}
		if strings.HasPrefix(t, "janela") {
			return "janela"
		}
		return "outro"
	}
	var r []analise.MomentoChave
	usado := map[int]bool{}
	// Perto (3 s) de um lance já escolhido é o mesmo lance visto de outro
	// jeito (a luta e o abate dentro dela): não gasta ficha.
	perto := func(m analise.MomentoChave) bool {
		for _, x := range r {
			if d := m.TMs - x.TMs; d > -3000 && d < 3000 {
				return true
			}
		}
		return false
	}
	pega := func(m analise.MomentoChave) {
		usado[m.N] = true
		r = append(r, m)
	}
	// O plano é POR FASE: cada fase tem a sua cota (a de rotas é maior: é
	// onde a decisão do caçador e do roaming aparece primeiro). Em cada fase,
	// primeiro o melhor de cada grupo (abate, janela, luta/objetivo), depois o
	// resto por impacto.
	cota := map[string]int{"rotas": 4, "meio": 3, "final": 3}
	for _, fs := range analise.Fases {
		n := 0
		visto := map[string]bool{}
		for _, m := range todos {
			if n < cota[fs] && analise.Fase(m.TMs) == fs && !visto[grupo(m.Tipo)] && !perto(m) {
				visto[grupo(m.Tipo)] = true
				pega(m)
				n++
			}
		}
		for _, m := range todos {
			if n < cota[fs] && analise.Fase(m.TMs) == fs && !usado[m.N] && !perto(m) {
				pega(m)
				n++
			}
		}
	}
	// Sobra de cota (fase curta ou vazia) vai para o que tiver mais impacto.
	for _, m := range todos {
		if len(r) < MaxLances && !usado[m.N] && !perto(m) {
			pega(m)
		}
	}
	sort.SliceStable(r, func(a, b int) bool { return r[a].TMs < r[b].TMs })
	return r
}

// objetivosEmVolta: o último objetivo tomado antes do instante e o próximo
// depois (só o que a base de fatos registrou; o app não sabe o relógio de
// nascimento de cada objetivo além disso).
func objetivosEmVolta(info InfoPartida, t int64) string {
	var antes, depois *analise.MomentoChave
	for i := range info.Momentos {
		m := &info.Momentos[i]
		if m.Tipo != "objetivo" {
			continue
		}
		if m.TMs < t && (antes == nil || m.TMs > antes.TMs) {
			antes = m
		}
		if m.TMs >= t && (depois == nil || m.TMs < depois.TMs) {
			depois = m
		}
	}
	desc := func(m *analise.MomentoChave) string {
		quem := "sem time"
		if m.Time != 0 {
			quem = "time " + analise.NomeTime(m.Time)
		}
		alvo := m.Subtipo
		if alvo == "" {
			alvo = "objetivo"
		}
		return fmt.Sprintf("%s em %s (%s)", alvo, analise.MinSeg(m.TMs), quem)
	}
	var partes []string
	if antes != nil {
		partes = append(partes, "último antes: "+desc(antes))
	}
	if depois != nil {
		partes = append(partes, "próximo tomado: "+desc(depois))
	}
	if len(partes) == 0 {
		return ""
	}
	return "- Objetivos (base de fatos): " + strings.Join(partes, "; ") + ".\n"
}

// comparacaoUsuario: o jogador de referência ao lado dos jogadores rastreados
// das partidas do usuário (fora da coleta) na mesma função.
func (s *Servico) comparacaoUsuario(eu analise.IndicadoresJogador) (texto, amostra string) {
	outros, err := s.Fontes.Referencias(analise.FiltroReferencia{ForaDaColeta: true, Funcao: eu.Funcao, ExcluiGame: eu.GameID, Limite: 10})
	if err != nil || len(outros) == 0 {
		return "", ""
	}
	jogos, quem := map[int64]bool{}, map[string]bool{}
	for _, o := range outros {
		jogos[o.GameID] = true
		quem[fmt.Sprint(o.GameID, ":", o.ParticipanteID)] = true
	}
	amostra = fmt.Sprintf("%s de %s rastreados %s nas partidas do usuário (fora da coleta; aliados e adversários dele, logo a faixa de elo dele pelo matchmaking; o elo de cada um não é sabido)",
		plural(len(quem), "jogador", "jogadores"), plural(len(jogos), "partida", "partidas"), FuncaoNaFrase(eu.Funcao))
	var b strings.Builder
	fmt.Fprintf(&b, "AMOSTRA DO USUÁRIO: %s.", amostra)
	if len(outros) < AmostraPequena {
		b.WriteString(" AMOSTRA PEQUENA: diga isso e não generalize.")
	}
	b.WriteString("\n")
	b.WriteString(tabelaComRotulos(eu, outros, fmt.Sprintf("REFERÊNCIA: %s (esta partida)", eu.Campeao),
		func(i int, x analise.IndicadoresJogador) string {
			return fmt.Sprintf("usuário %d: %s, partida %d, %s", i+1, x.Campeao, x.GameID, map[bool]string{true: "vitória", false: "derrota"}[x.Venceu])
		}))
	return b.String(), amostra
}

// pedidoReferencia monta o texto do relatório de referência.
func (s *Servico) pedidoReferencia(ctx context.Context, info InfoPartida, foco Participante, eu analise.IndicadoresJogador) (Pedido, Enviado, QuemReferencia, string, error) {
	quem := QuemReferencia{Participante: foco.ID, Campeao: foco.Campeao, Funcao: foco.Funcao, Time: foco.Time, Elo: eu.Elo, Patch: eu.Patch,
		Venceu: eu.Venceu, DuracaoS: eu.DuracaoS, Rastreados: eu.RastreadosNaPartida, Papel: eu.Papel.Rotulo, Estilo: eu.Papel.EstiloRotulo,
		FrasePapel: FrasePapelReferencia(eu)}
	if quem.Patch == "" {
		quem.Patch = info.Patch
	}
	contexto, err := s.Fontes.ContextoPartida(ctx, info.GameID, foco.ID)
	if err != nil {
		return Pedido{}, Enviado{}, quem, "", err
	}
	visao, err := s.Fontes.Visao(ctx, info.GameID, foco.ID)
	if err != nil {
		visao = "## Visão\n- Não deu para montar a seção de visão: " + err.Error() + "\n"
	}
	env := Enviado{Secoes: []string{"participantes (campeão, função, time, nome de invocador)", "pacote de contexto da partida (base de fatos)",
		"visão (sentinelas e passe de visão de time)"}, Arquivos: []string{}, Cortadas: []string{}}
	time := "azul"
	if foco.Time == 200 {
		time = "vermelho"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "JOGADOR DE REFERÊNCIA: participante %d (%s, %s, time %s)\n", foco.ID, foco.Campeao, nomeFuncaoCurto(foco.Funcao), time)
	fmt.Fprintf(&b, "ELO: %s (da coleta por elo) · PATCH: %s · RESULTADO: %s · DURAÇÃO: %s\n", NomeElo(eu.Elo), quem.Patch,
		map[bool]string{true: "vitória", false: "derrota"}[eu.Venceu], analise.MinSeg(int64(eu.DuracaoS)*1000))
	if quem.FrasePapel != "" {
		fmt.Fprintf(&b, "PAPEL E ESTILO (por regra, analise/papel.go): %s\n", quem.FrasePapel)
	}
	if eu.RastreadosNaPartida >= 10 {
		b.WriteString("RASTREADOS: os 10. As fichas trazem a posição rastreada de todos (salvo quando dito aproximada).\n")
	} else {
		fmt.Fprintf(&b, "RASTREADOS: %d de 10 (só o jogador de referência). FALTA: a posição dos outros 9 é a da linha do tempo da Riot "+
			"(uma por minuto, interpolada), sem vida/mana deles; atividades que precisam dos outros rastreados (luta, troca, gank, "+
			"rotação, agrupado) não aparecem. Diga o que isso impede de saber.\n", eu.RastreadosNaPartida)
	}
	comp, amostra := s.comparacaoUsuario(eu)
	if comp == "" {
		b.WriteString("COMPARAÇÃO COM OS JOGADORES DO USUÁRIO: NENHUMA (não há jogador rastreado da mesma função fora da coleta). Deixe \"nao_fez\" vazio.\n")
	} else {
		fmt.Fprintf(&b, "COMPARAÇÃO COM OS JOGADORES DO USUÁRIO: %s\n", amostra)
	}
	b.WriteString("\n")
	b.WriteString(listaParticipantes(info))
	b.WriteString("\n")
	b.WriteString(contexto)
	b.WriteString("\n\n")
	b.WriteString(visao)
	b.WriteString("\n")
	lances := lancesDaReferencia(info, foco.ID)
	if len(lances) > 0 {
		instantes := make([]int64, len(lances))
		for i, m := range lances {
			instantes[i] = m.TMs
			if m.Tipo != "janela_aproveitada" && m.Tipo != "janela_perdida" && m.TMs >= 5000 {
				instantes[i] = m.TMs - 5000
			}
		}
		fichas, err := s.Fontes.Fichas(ctx, info.GameID, foco.ID, instantes)
		if err != nil {
			fichas = nil
		}
		b.WriteString("## Lances do jogador de referência (o app escolheu os de mais impacto de que ele participou; ficha de 5 s antes, salvo nas janelas)\n")
		for i, m := range lances {
			papel := "participou"
			switch {
			case m.Tipo == "abate" && m.ParticipanteID == foco.ID:
				papel = "MORREU"
			case m.Autor == foco.ID:
				papel = "foi o autor"
			}
			fmt.Fprintf(&b, "\n### L%d [%s] %s%s (%s), fase %s, impacto ~%d\n", i+1, analise.MinSeg(m.TMs), m.Tipo,
				map[bool]string{true: " (" + m.Subtipo + ")", false: ""}[m.Subtipo != ""], papel, analise.Fase(m.TMs), m.Impacto)
			fmt.Fprintf(&b, "- O que aconteceu: %s", m.Frase)
			if m.SaldoAntes != nil && m.SaldoDepois != nil {
				fmt.Fprintf(&b, " Ouro (azul − vermelho): %+d antes, %+d depois.", *m.SaldoAntes, *m.SaldoDepois)
			}
			if len(m.Tags) > 0 {
				b.WriteString(" Etiquetas: " + strings.Join(m.Tags, ", ") + ".")
			}
			if m.Estimado {
				b.WriteString(" (estimado)")
			}
			b.WriteString("\n")
			b.WriteString(objetivosEmVolta(info, m.TMs))
			if i < len(fichas) {
				fmt.Fprintf(&b, "- Ficha do instante %s:\n%s", analise.MinSeg(instantes[i]), fichas[i])
			}
		}
		b.WriteString("\n")
		env.Secoes = append(env.Secoes, fmt.Sprintf("%d lances com a ficha do instante (os 10, ondas, sentinelas, visão, objetivos antes e depois)", len(lances)))
	}
	if comp != "" {
		b.WriteString("## Comparação com os jogadores do usuário (mesma função; só a base de fatos)\n")
		b.WriteString(comp)
		b.WriteString("\n")
		env.Secoes = append(env.Secoes, "tabela de indicadores: referência × jogadores do usuário da mesma função")
	} else {
		b.WriteString("## Indicadores do jogador de referência\n")
		b.WriteString(tabelaComRotulos(eu, nil, fmt.Sprintf("REFERÊNCIA: %s (esta partida)", eu.Campeao), nil))
		b.WriteString("\n")
		env.Secoes = append(env.Secoes, "tabela de indicadores do jogador de referência")
	}
	b.WriteString("\nEscreva o relatório de referência agora, só o JSON.")
	p := Pedido{Tipo: TipoAnalise, Sistema: SistemaReferencia(), Mensagens: []Mensagem{{Papel: PapelUsuario, Texto: b.String()}}}
	env.CaracteresSistema = utf8.RuneCountInString(p.Sistema)
	env.CaracteresMensagem = utf8.RuneCountInString(p.Mensagens[0].Texto)
	if i := strings.Index(contexto, "Fora do pacote"); i >= 0 {
		env.Cortadas = append(env.Cortadas, strings.TrimSpace(firstLine(contexto[i:])))
	}
	return p, env, quem, amostra, nil
}

// --- Validação ---------------------------------------------------------------------

type referenciaBruta struct {
	Resumo string `json:"resumo"`
	Fases  []struct {
		Fase  string `json:"fase"`
		Plano string `json:"plano"`
		Acoes []struct {
			T           string `json:"t"`
			Fez         string `json:"fez"`
			EmRespostaA string `json:"em_resposta_a"`
			Fonte       string `json:"fonte"`
		} `json:"acoes"`
	} `json:"fases"`
	Decisoes []struct {
		T      string       `json:"t"`
		Titulo string       `json:"titulo"`
		Fatos  string       `json:"fatos"`
		Peso   string       `json:"peso"`
		Regra  string       `json:"regra"`
		Casos  []string     `json:"casos"`
		Fonte  string       `json:"fonte"`
		Quadro *quadroBruto `json:"quadro"`
	} `json:"decisoes"`
	Erros []struct {
		T      string       `json:"t"`
		Fato   string       `json:"fato"`
		Custo  string       `json:"custo"`
		Licao  string       `json:"licao"`
		Fonte  string       `json:"fonte"`
		Quadro *quadroBruto `json:"quadro"`
	} `json:"erros"`
	NaoFez []struct {
		Texto   string `json:"texto"`
		Numeros string `json:"numeros"`
		Fonte   string `json:"fonte"`
	} `json:"nao_fez"`
	NaoDaParaSaber []string `json:"nao_da_para_saber"`
}

// decodificaSolto: JSON da IA (tira cerca e, se preciso, comentários //).
func decodificaSolto(texto string, v any) error {
	bruto := extraiJSON(texto)
	dec := func(s string) error {
		d := json.NewDecoder(strings.NewReader(s))
		d.UseNumber()
		return d.Decode(v)
	}
	if err := dec(bruto); err != nil {
		if err2 := dec(semComentarios(bruto)); err2 != nil {
			return fmt.Errorf("%w: %v", ErrFormato, err)
		}
	}
	return nil
}

// regraNoFormato: "se X e Y, então Z, porque W" (o "e Y" é opcional).
func regraNoFormato(r string) bool {
	l := strings.ToLower(strings.TrimSpace(r))
	return strings.HasPrefix(l, "se ") && strings.Contains(l, "então") && strings.Contains(l, "porque")
}

// ValidaReferencia lê a resposta da IA. Instante fora da partida derruba o
// item; regra fora do formato fica, com aviso; a hipótese é decidida pelo app
// (menos de 2 casos distintos nesta partida).
func ValidaReferencia(texto string, info InfoPartida, temComparacao bool) (ConteudoReferencia, []string, error) {
	var rb referenciaBruta
	if err := decodificaSolto(texto, &rb); err != nil {
		return ConteudoReferencia{}, nil, err
	}
	avisos := []string{}
	duracaoMs := int64(info.DuracaoS) * 1000
	instante := func(onde, t string) (int64, string, bool) {
		ms, ok := msDeTexto(t)
		if !ok || ms < 0 || (duracaoMs > 0 && ms > duracaoMs+1000) {
			avisos = append(avisos, fmt.Sprintf("%s descartado: instante %q fora da partida ou ilegível", onde, t))
			return 0, "", false
		}
		return ms, analise.MinSeg(ms), true
	}
	limpa := func(s string, max int) string {
		s = strings.TrimSpace(s)
		if r := []rune(s); len(r) > max {
			s = string(r[:max]) + "…"
		}
		return s
	}
	c := ConteudoReferencia{Resumo: limpa(rb.Resumo, 1500), Fases: []FasePlano{}, Decisoes: []Decisao{}, Erros: []ErroRef{}, NaoFez: []NaoFez{},
		NaoDaParaSaber: []string{}}
	fases := map[string]bool{"rotas": true, "meio": true, "final": true}
	for i, f := range rb.Fases {
		nome := strings.ToLower(strings.TrimSpace(f.Fase))
		if !fases[nome] {
			avisos = append(avisos, fmt.Sprintf("fase %d descartada: %q não é rotas, meio ou final", i+1, f.Fase))
			continue
		}
		fases[nome] = false
		fp := FasePlano{Fase: nome, Plano: limpa(f.Plano, 1200), Acoes: []AcaoPlano{}}
		for k, a := range f.Acoes {
			ms, t, ok := instante(fmt.Sprintf("ação %d da fase %s", k+1, nome), a.T)
			if !ok {
				continue
			}
			if len(fp.Acoes) >= 8 {
				break
			}
			fp.Acoes = append(fp.Acoes, AcaoPlano{T: t, TMs: ms, Fez: limpa(a.Fez, 500), EmRespostaA: limpa(a.EmRespostaA, 500), Fonte: limpa(a.Fonte, 200)})
		}
		c.Fases = append(c.Fases, fp)
	}
	ordemFase := map[string]int{"rotas": 0, "meio": 1, "final": 2}
	sort.SliceStable(c.Fases, func(a, b int) bool { return ordemFase[c.Fases[a].Fase] < ordemFase[c.Fases[b].Fase] })
	for i, d := range rb.Decisoes {
		ms, t, ok := instante(fmt.Sprintf("decisão %d", i+1), d.T)
		if !ok {
			continue
		}
		x := Decisao{T: t, TMs: ms, Titulo: limpa(d.Titulo, 160), Fatos: limpa(d.Fatos, 1500), Peso: limpa(d.Peso, 400), Regra: limpa(d.Regra, 600),
			Fonte: limpa(d.Fonte, 200), Casos: []string{}}
		vistos := map[string]bool{t: true}
		x.Casos = append(x.Casos, t)
		for _, ct := range d.Casos {
			if cms, ok := msDeTexto(ct); ok && (duracaoMs == 0 || cms <= duracaoMs+1000) {
				if s := analise.MinSeg(cms); !vistos[s] {
					vistos[s] = true
					x.Casos = append(x.Casos, s)
				}
			}
		}
		x.Hipotese = len(x.Casos) < 2
		if x.Regra == "" {
			avisos = append(avisos, fmt.Sprintf("decisão %d: sem regra", i+1))
		} else if !regraNoFormato(x.Regra) {
			avisos = append(avisos, fmt.Sprintf("decisão %d: a regra não está no formato \"se X, então Z, porque W\"", i+1))
		}
		x.Quadro = validaQuadro(d.Quadro, t, ms, info, &avisos, fmt.Sprintf("quadro da decisão %d", i+1))
		c.Decisoes = append(c.Decisoes, x)
		if len(c.Decisoes) >= 6 {
			break
		}
	}
	for i, e := range rb.Erros {
		ms, t, ok := instante(fmt.Sprintf("erro %d", i+1), e.T)
		if !ok {
			continue
		}
		x := ErroRef{T: t, TMs: ms, Fato: limpa(e.Fato, 1000), Custo: limpa(e.Custo, 200), Licao: limpa(e.Licao, 400), Fonte: limpa(e.Fonte, 200)}
		x.Quadro = validaQuadro(e.Quadro, t, ms, info, &avisos, fmt.Sprintf("quadro do erro %d", i+1))
		c.Erros = append(c.Erros, x)
		if len(c.Erros) >= 5 {
			break
		}
	}
	if !temComparacao && len(rb.NaoFez) > 0 {
		avisos = append(avisos, "\"não fez\" descartado: não há jogadores do usuário para comparar")
	} else {
		for _, n := range rb.NaoFez {
			if t := limpa(n.Texto, 600); t != "" {
				c.NaoFez = append(c.NaoFez, NaoFez{Texto: t, Numeros: limpa(n.Numeros, 400), Fonte: limpa(n.Fonte, 200)})
			}
		}
	}
	for _, n := range rb.NaoDaParaSaber {
		if t := limpa(n, 400); t != "" {
			c.NaoDaParaSaber = append(c.NaoDaParaSaber, t)
		}
	}
	if len(c.Decisoes) < 3 {
		avisos = append(avisos, fmt.Sprintf("a IA devolveu %d decisão(ões) válidas (o pedido é de 3 a 6)", len(c.Decisoes)))
	}
	if c.Resumo == "" && len(c.Decisoes) == 0 && len(c.Fases) == 0 {
		return c, avisos, ErrFormato
	}
	return c, avisos, nil
}

// --- Banco ---------------------------------------------------------------------------

const colunasRelEsp = `id, game_id, participante_id, versao, estado, erro, criado_em, terminado_em, modelo, implementacao, versao_prompt,
	patch, elo, elo_tier, funcao, campeao, puuid, venceu, rastreados, conteudo, bruto, avisos, enviado, arquivo,
	duracao_ms, caracteres_entrada, caracteres_saida, tokens_entrada, tokens_saida, custo_usd`

func leRelEsp(s interface{ Scan(...any) error }) (RelatorioReferencia, error) {
	var r RelatorioReferencia
	var conteudo, avisos, enviado string
	err := s.Scan(&r.ID, &r.GameID, &r.ParticipanteID, &r.Versao, &r.Estado, &r.Erro, &r.CriadoEm, &r.TerminadoEm, &r.Modelo, &r.Implementacao,
		&r.VersaoPrompt, &r.Patch, &r.Elo, &r.EloTier, &r.Funcao, &r.Campeao, &r.puuid, &r.Venceu, &r.Rastreados, &conteudo, &r.Bruto, &avisos,
		&enviado, &r.Arquivo, &r.Uso.DuracaoMs, &r.Uso.CaracteresEntrada, &r.Uso.CaracteresSaida, &r.Uso.TokensEntrada, &r.Uso.TokensSaida, &r.Uso.CustoUSD)
	if err != nil {
		return r, err
	}
	r.Uso.Modelo = r.Modelo
	if conteudo != "" {
		var c ConteudoReferencia
		if json.Unmarshal([]byte(conteudo), &c) == nil {
			r.Conteudo = &c
			r.Bruto = ""
		}
	}
	_ = json.Unmarshal([]byte(avisos), &r.Avisos)
	_ = json.Unmarshal([]byte(enviado), &r.Enviado)
	r.Avisos = web.NuncaNil(r.Avisos)
	r.Enviado.Secoes = web.NuncaNil(r.Enviado.Secoes)
	r.Enviado.Arquivos = web.NuncaNil(r.Enviado.Arquivos)
	r.Enviado.Cortadas = web.NuncaNil(r.Enviado.Cortadas)
	return r, nil
}

// LeRelatorioReferencia: a versão pedida (0 = a mais nova).
func LeRelatorioReferencia(db *banco.DB, gameID int64, pid, versao int) (RelatorioReferencia, bool, error) {
	q := `SELECT ` + colunasRelEsp + ` FROM especialista_relatorios WHERE game_id = ? AND participante_id = ?`
	args := []any{gameID, pid}
	if versao > 0 {
		q += ` AND versao = ?`
		args = append(args, versao)
	}
	r, err := leRelEsp(db.QueryRow(q+` ORDER BY versao DESC LIMIT 1`, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return r, false, nil
	}
	return r, err == nil, err
}

func leRelEspPorID(db *banco.DB, id int64) (RelatorioReferencia, error) {
	return leRelEsp(db.QueryRow(`SELECT `+colunasRelEsp+` FROM especialista_relatorios WHERE id = ?`, id))
}

// VersoesReferencia: as versões, da mais nova.
func VersoesReferencia(db *banco.DB, gameID int64, pid int) ([]VersaoResumo, error) {
	linhas, err := db.Query(`SELECT versao, estado, criado_em, modelo FROM especialista_relatorios WHERE game_id = ? AND participante_id = ?
		ORDER BY versao DESC`, gameID, pid)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	var r []VersaoResumo
	for linhas.Next() {
		var v VersaoResumo
		if err := linhas.Scan(&v.Versao, &v.Estado, &v.CriadoEm, &v.Modelo); err != nil {
			return nil, err
		}
		r = append(r, v)
	}
	return r, linhas.Err()
}

// --- Execução --------------------------------------------------------------------------

// referenciaValida: a partida e o jogador de referência (o escolhido pela
// coleta ou o aproveitado: elo da partida e do jogador sabidos, rastreado).
func (s *Servico) referenciaValida(ctx context.Context, gameID int64, pid int) (InfoPartida, Participante, analise.IndicadoresJogador, error) {
	info, err := s.Fontes.Partida(ctx, gameID)
	if err != nil {
		return info, Participante{}, analise.IndicadoresJogador{}, err
	}
	foco, ok := info.Do(pid)
	if !ok {
		return info, foco, analise.IndicadoresJogador{}, &ErrPedido{fmt.Sprintf("o participante %d não está na partida", pid)}
	}
	eu, err := s.Fontes.Indicadores(gameID, pid)
	if err != nil {
		return info, foco, eu, err
	}
	if eu.EloPartida == "" && info.Elo != "" {
		eu.EloPartida = info.Elo
	}
	if eu.Elo == "" || eu.EloPartida == "" || !eu.Rastreado {
		return info, foco, eu, &ErrPedido{"este jogador não é jogador de referência da coleta por elo (elo sabido e rastreado)"}
	}
	if eu.Patch == "" {
		eu.Patch = info.Patch
	}
	return info, foco, eu, nil
}

// PreviaReferencia: o texto exato, sem chamar a IA.
func (s *Servico) PreviaReferencia(ctx context.Context, gameID int64, pid int) (Pedido, Enviado, error) {
	info, foco, eu, err := s.referenciaValida(ctx, gameID, pid)
	if err != nil {
		return Pedido{}, Enviado{}, err
	}
	p, env, _, _, err := s.pedidoReferencia(ctx, info, foco, eu)
	return p, env, err
}

// GeraReferencia começa o relatório de referência (execução com SSE).
func (s *Servico) GeraReferencia(ctx context.Context, gameID int64, pid int) (RelatorioReferencia, *web.Execucao, error) {
	info, foco, eu, err := s.referenciaValida(ctx, gameID, pid)
	if err != nil {
		return RelatorioReferencia{}, nil, err
	}
	est := s.IA.Estado(ctx)
	if !est.Disponivel {
		return RelatorioReferencia{}, nil, &erroIA{est.Motivo, ErrIndisponivel}
	}
	e := s.Hub.Nova()
	chave := chaveRelEsp(gameID, pid)
	if x := s.ocupa(chave, e); x != nil {
		e.Fecha()
		return RelatorioReferencia{}, nil, &ErrRodando{"o relatório de referência deste jogador já está rodando", x}
	}
	var versao int
	_ = s.DB.QueryRow(`SELECT COALESCE(MAX(versao), 0) + 1 FROM especialista_relatorios WHERE game_id = ? AND participante_id = ?`, gameID, pid).Scan(&versao)
	res, err := s.DB.Exec(`INSERT INTO especialista_relatorios (game_id, participante_id, versao, estado, criado_em, implementacao, versao_prompt,
		patch, elo, elo_tier, funcao, campeao, puuid, venceu, rastreados) VALUES (?, ?, ?, 'rodando', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		gameID, pid, versao, banco.Agora(), est.Implementacao, VersaoPromptEspecialista, eu.Patch, eu.Elo, TierDe(eu.Elo), eu.Funcao, foco.Campeao,
		foco.Puuid, eu.Venceu, eu.RastreadosNaPartida)
	if err != nil {
		s.libera(chave, e)
		e.Fecha()
		return RelatorioReferencia{}, nil, err
	}
	id, _ := res.LastInsertId()
	ctxTrab, cancela := context.WithCancel(context.Background())
	e.AoCancelar(cancela)
	go func() {
		defer cancela()
		defer s.libera(chave, e)
		defer e.Fecha()
		s.rodaReferencia(ctxTrab, e, id, info, foco, eu, est)
	}()
	r, _, err := LeRelatorioReferencia(s.DB, gameID, pid, versao)
	return r, e, err
}

func (s *Servico) rodaReferencia(ctx context.Context, e *web.Execucao, id int64, info InfoPartida, foco Participante, eu analise.IndicadoresJogador, est Estado) {
	termina := func(estado, erro string, c *ConteudoReferencia, bruto string, avisos []string, env Enviado, uso Resposta, arquivo string) {
		cj := ""
		if c != nil {
			cj = jsonOuVazio(c, "")
		}
		if _, err := s.DB.Exec(`UPDATE especialista_relatorios SET estado = ?, erro = ?, terminado_em = ?, modelo = ?, conteudo = ?, bruto = ?,
			avisos = ?, enviado = ?, arquivo = ?, duracao_ms = ?, caracteres_entrada = ?, caracteres_saida = ?, tokens_entrada = ?, tokens_saida = ?,
			custo_usd = ? WHERE id = ?`, estado, erro, banco.Agora(), uso.Modelo, cj, bruto, jsonOuVazio(web.NuncaNil(avisos), "[]"),
			jsonOuVazio(env, "{}"), arquivo, uso.DuracaoMs, uso.CaracteresEntrada, uso.CaracteresSaida, uso.TokensEntrada, uso.TokensSaida,
			uso.CustoUSD, id); err != nil {
			log.Printf("especialista: gravar o relatório %d: %v", id, err)
		}
	}
	p, env, quem, amostra, err := s.pedidoReferencia(ctx, info, foco, eu)
	if err != nil {
		termina("falhou", "não deu para montar o texto da partida: "+err.Error(), nil, "", nil, env, Resposta{}, "")
		e.Falha("não deu para montar o texto da partida: " + err.Error())
		return
	}
	e.Emite("contexto", map[string]any{"enviado": env, "modelo": est.Modelos[TipoAnalise], "rastreados": eu.RastreadosNaPartida})
	if s.IA.Ocupada() {
		e.Emite("na_fila", map[string]any{"texto": "outra chamada à IA está rodando; esta espera a vez"})
	}
	e.Emite("inicio", map[string]any{"modelo": est.Modelos[TipoAnalise]})
	recebidos := 0
	uso, err := s.IA.Conversa(ctx, p, func(t string) {
		recebidos += utf8.RuneCountInString(t)
		e.Emite("trecho", map[string]any{"texto": t, "caracteres": recebidos})
	})
	if err != nil {
		estado := "falhou"
		if ctx.Err() != nil {
			estado = "cancelado"
		}
		log.Printf("especialista: relatório %d: %v (%v)", id, err, errors.Unwrap(err))
		termina(estado, err.Error(), nil, uso.Texto, nil, env, uso, "")
		if estado == "cancelado" {
			e.Emite("cancelado", map[string]any{"id": id})
			return
		}
		e.Falha(err.Error())
		return
	}
	c, avisos, err := ValidaReferencia(uso.Texto, info, amostra != "")
	if err != nil {
		termina("falhou", err.Error(), nil, uso.Texto, avisos, env, uso, "")
		e.Falha(err.Error() + " — a resposta crua ficou guardada; gere de novo")
		return
	}
	c.Quem = quem
	if amostra != "" {
		c.Comparacao = "Comparado com " + amostra + "."
	} else {
		c.Comparacao = "Sem comparação: não há jogador rastreado da mesma função nas partidas do usuário."
	}
	if eu.RastreadosNaPartida < 10 {
		c.NaoDaParaSaber = append([]string{fmt.Sprintf("Só %d de 10 rastreados: a posição dos outros 9 é a da linha do tempo da Riot (por minuto); "+
			"vida, mana e atividade deles não são sabidas.", eu.RastreadosNaPartida)}, c.NaoDaParaSaber...)
	}
	r, _ := leRelEspPorID(s.DB, id)
	r.Modelo = uso.Modelo
	arquivo := arquivoRelatorio(eu.Patch, TierDe(eu.Elo), eu.Funcao, foco.Campeao, info.GameID, foco.ID)
	r.Conteudo = &c
	md := MarkdownReferencia(r, info)
	if err := s.escreveConhecimento(arquivo, md); err != nil {
		avisos = append(avisos, "não deu para gravar o Markdown: "+err.Error())
		arquivo = ""
	}
	termina("concluido", "", &c, uso.Texto, avisos, env, uso, arquivo)
	r, _, _ = LeRelatorioReferencia(s.DB, info.GameID, foco.ID, 0)
	e.Emite("concluido", map[string]any{"relatorio": r})
}

// --- Markdown ----------------------------------------------------------------------------

// TituloReferencia: o título padronizado (arquivo e NotebookLM).
func TituloReferencia(r RelatorioReferencia) string {
	return fmt.Sprintf("%s · %s · %s · %s · partida %d", r.Patch, NomeElo(r.EloTier), nomeRota(r.Funcao), r.Campeao, r.GameID)
}

// tiraNomes troca os nomes de invocador pelo campeão: o Markdown vai para a
// pasta e, a pedido, para o NotebookLM; nomes de jogadores não precisam ir.
func tiraNomes(s string, info InfoPartida) string {
	for _, p := range info.Participantes {
		if n := strings.TrimSpace(p.Nome); utf8.RuneCountInString(n) >= 3 && !strings.EqualFold(n, p.Campeao) {
			s = strings.ReplaceAll(s, n, p.Campeao)
		}
	}
	return s
}

func quadroMD(q *Quadro, nomes map[int]string) string {
	if q == nil {
		return ""
	}
	var partes []string
	for _, r := range q.Realce {
		partes = append(partes, nomes[r])
	}
	s := fmt.Sprintf("  - Quadro do mapa em %s: %s", q.T, q.Legenda)
	if len(partes) > 0 {
		s += " (destaque: " + strings.Join(partes, ", ") + ")"
	}
	if q.Regiao != "" {
		s += "; região " + nomeZona(q.Regiao)
	}
	return s + "\n"
}

// MarkdownReferencia: o relatório em Markdown (sem nomes de invocador).
func MarkdownReferencia(r RelatorioReferencia, info InfoPartida) string {
	c := r.Conteudo
	if c == nil {
		return ""
	}
	nomes := map[int]string{}
	for _, p := range info.Participantes {
		nomes[p.ID] = p.Campeao
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Relatório de referência · %s\n\n", TituloReferencia(r))
	fmt.Fprintf(&b, "<!-- lane: tipo=relatorio game=%d participante=%d versao=%d -->\n\n", r.GameID, r.ParticipanteID, r.Versao)
	q := c.Quem
	fmt.Fprintf(&b, "**Quem:** %s (%s, participante %d, time %s), %s, patch %s, %s em %s. Rastreados na partida: %d de 10.\n\n",
		q.Campeao, nomeRota(q.Funcao), q.Participante, analise.NomeTime(q.Time), NomeElo(q.Elo), q.Patch,
		map[bool]string{true: "vitória", false: "derrota"}[q.Venceu], analise.MinSeg(int64(q.DuracaoS)*1000), q.Rastreados)
	if q.FrasePapel != "" {
		fmt.Fprintf(&b, "**Papel e estilo (por regra):** %s.\n\n", q.FrasePapel)
	}
	fmt.Fprintf(&b, "Gerado pelo Lane (versão %d do relatório, modelo %s, %s). Só fatos que o app calculou do replay; cada afirmação tem o instante.\n\n",
		r.Versao, r.Modelo, r.CriadoEm)
	if c.Resumo != "" {
		b.WriteString("## Resumo\n\n" + c.Resumo + "\n\n")
	}
	if len(c.Fases) > 0 {
		b.WriteString("## Plano de jogo por fase\n\n")
		for _, f := range c.Fases {
			fmt.Fprintf(&b, "### %s\n\n%s\n\n", map[string]string{"rotas": "Fase de rotas", "meio": "Meio de jogo", "final": "Final de jogo"}[f.Fase], f.Plano)
			for _, a := range f.Acoes {
				fmt.Fprintf(&b, "- **%s** %s", a.T, a.Fez)
				if a.EmRespostaA != "" {
					fmt.Fprintf(&b, " — em resposta a: %s", a.EmRespostaA)
				}
				b.WriteString("\n")
			}
			b.WriteString("\n")
		}
	}
	if len(c.Decisoes) > 0 {
		b.WriteString("## Decisões que mais pesaram\n\n")
		for i, d := range c.Decisoes {
			fmt.Fprintf(&b, "### %d. %s (%s)\n\n", i+1, d.Titulo, d.T)
			fmt.Fprintf(&b, "- Fatos: %s\n", d.Fatos)
			if d.Peso != "" {
				fmt.Fprintf(&b, "- Por que pesou: %s\n", d.Peso)
			}
			marca := fmt.Sprintf("casos nesta partida: %s", strings.Join(d.Casos, ", "))
			if d.Hipotese {
				marca = "HIPÓTESE: um caso só (" + d.T + ")"
			}
			fmt.Fprintf(&b, "- **Regra:** %s (%s)\n", d.Regra, marca)
			b.WriteString(quadroMD(d.Quadro, nomes))
			b.WriteString("\n")
		}
	}
	if len(c.Erros) > 0 {
		b.WriteString("## Erros dele\n\n")
		for _, e := range c.Erros {
			fmt.Fprintf(&b, "- **%s** %s", e.T, strings.TrimSpace(e.Fato))
			if e.Custo != "" {
				fmt.Fprintf(&b, " Custo: %s.", strings.TrimRight(e.Custo, ". "))
			}
			if e.Licao != "" {
				fmt.Fprintf(&b, " Lição: %s", e.Licao)
			}
			b.WriteString("\n")
			b.WriteString(quadroMD(e.Quadro, nomes))
		}
		b.WriteString("\n")
	}
	b.WriteString("## O que ele não fez que os jogadores do usuário fazem\n\n")
	b.WriteString(c.Comparacao + "\n\n")
	for _, n := range c.NaoFez {
		fmt.Fprintf(&b, "- %s", n.Texto)
		if n.Numeros != "" {
			fmt.Fprintf(&b, " (%s)", n.Numeros)
		}
		b.WriteString("\n")
	}
	if len(c.NaoFez) > 0 {
		b.WriteString("\n")
	}
	if len(c.NaoDaParaSaber) > 0 {
		b.WriteString("## O que os dados não dizem\n\n")
		for _, n := range c.NaoDaParaSaber {
			b.WriteString("- " + n + "\n")
		}
	}
	return tiraNomes(b.String(), info)
}
