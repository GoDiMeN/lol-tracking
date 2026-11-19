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

// O RELATÓRIO DA PARTIDA COM IA: uma execução (SSE) que monta o texto da
// partida (pacote de contexto + visão + participantes + material das vozes),
// chama a IA uma vez, valida a resposta (JSON no formato de SistemaAnalise) e
// guarda por partida + jogador + versão. A IA devolve instantes e anotações
// curtas dos quadros; quem desenha o mapa é o app.

// Relatorio é a resposta de GET /api/partidas/{id}/ia/relatorio.
type Relatorio struct {
	ID             int64              `json:"id"`
	GameID         int64              `json:"game_id"`
	ParticipanteID int                `json:"participante_id"`
	Versao         int                `json:"versao"`
	Estado         string             `json:"estado"` // rodando, concluido, falhou, cancelado
	Erro           string             `json:"erro"`
	CriadoEm       string             `json:"criado_em"`
	TerminadoEm    string             `json:"terminado_em"`
	Modelo         string             `json:"modelo"`
	Implementacao  string             `json:"implementacao"`
	VersaoPrompt   int                `json:"versao_prompt"`
	Conteudo       *ConteudoRelatorio `json:"conteudo"`
	Avisos         []string           `json:"avisos"`
	Vozes          []MaterialVoz      `json:"vozes"`
	Enviado        Enviado            `json:"enviado"`
	Uso            Resposta           `json:"uso"`
	// Bruto: a resposta crua da IA (só quando não deu para ler o formato).
	Bruto string `json:"bruto,omitempty"`
}

// Enviado descreve o que saiu da máquina na chamada (a tela mostra).
type Enviado struct {
	CaracteresSistema  int      `json:"caracteres_sistema"`
	CaracteresMensagem int      `json:"caracteres_mensagem"`
	Secoes             []string `json:"secoes"`
	Arquivos           []string `json:"arquivos_conhecimento"`
	PartidasReferencia int      `json:"partidas_referencia"`
	Cortadas           []string `json:"cortadas"`
}

// ConteudoRelatorio: as seções, cada uma com a fonte de cada afirmação.
// O relatório abre com as SITUAÇÕES (as 3 a 5 que mais custaram), cada uma
// como uma consulta ao especialista: os fatos, a pergunta, o que cada voz com
// material diz, a leitura da IA (separada), a próxima vez e as repetições.
type ConteudoRelatorio struct {
	Resumo         string         `json:"resumo"`
	Padroes        []Padrao       `json:"padroes"`
	Situacoes      []Situacao     `json:"situacoes"`
	JogadoresChave []JogadorChave `json:"jogadores_chave"`
	Build          []ItemBuild    `json:"build"`
	Janelas        []Janela       `json:"janelas"`
	Visao          SecaoVisao     `json:"visao"`
	// Vozes: o estado de cada voz neste relatório (quem tinha material e de onde).
	Vozes []VozRelatorio `json:"vozes"`
}

// Padrao: o que se repetiu, com os instantes.
type Padrao struct {
	Texto     string   `json:"texto"`
	Instantes []string `json:"instantes"`
}

// Situacao é uma consulta ao especialista sobre um lance.
type Situacao struct {
	T          string        `json:"t"`
	TMs        int64         `json:"t_ms"`
	Titulo     string        `json:"titulo"`
	Situacao   string        `json:"situacao"`
	Pergunta   string        `json:"pergunta"`
	Vozes      []RespostaVoz `json:"vozes"`
	Leitura    string        `json:"leitura"`
	ProximaVez string        `json:"proxima_vez"`
	Repeticoes []string      `json:"repeticoes"`
	Custo      string        `json:"custo"`
	Impacto    int           `json:"impacto"`
	Fonte      string        `json:"fonte"`
	Quadro     *Quadro       `json:"quadro"`
}

// RespostaVoz: o que uma voz diz numa situação (sem material: o app escreve).
type RespostaVoz struct {
	Voz      string `json:"voz"`
	Rotulo   string `json:"rotulo"`
	Resposta string `json:"resposta"`
	Fonte    string `json:"fonte"`
	SemDados bool   `json:"sem_dados"`
}

type JogadorChave struct {
	Participante int      `json:"participante"`
	Campeao      string   `json:"campeao"`
	Time         int      `json:"time"`
	Papel        string   `json:"papel"`
	PorQue       string   `json:"por_que"`
	Instantes    []string `json:"instantes"`
	Fonte        string   `json:"fonte"`
}

type ItemBuild struct {
	T           string `json:"t"`
	TMs         int64  `json:"t_ms"`
	Item        string `json:"item"`
	Observacao  string `json:"observacao"`
	Alternativa string `json:"alternativa"`
	Fonte       string `json:"fonte"`
}

type Janela struct {
	T         string  `json:"t"`
	TMs       int64   `json:"t_ms"`
	Tipo      string  `json:"tipo"` // perdida, aproveitada
	Fato      string  `json:"fato"`
	OQueFazer string  `json:"o_que_fazer"`
	Fonte     string  `json:"fonte"`
	Quadro    *Quadro `json:"quadro"`
}

type SecaoVisao struct {
	Texto string      `json:"texto"`
	Itens []ItemVisao `json:"itens"`
}

type ItemVisao struct {
	T     string `json:"t"`
	TMs   int64  `json:"t_ms"`
	Fato  string `json:"fato"`
	Fonte string `json:"fonte"`
}

type VozRelatorio struct {
	Voz      string `json:"voz"`
	Rotulo   string `json:"rotulo"`
	Texto    string `json:"texto"` // o material que ela teve, ou SemMaterial
	SemDados bool   `json:"sem_dados"`
}

// Quadro é a anotação de um quadro do mapa (o app desenha).
type Quadro struct {
	T       string `json:"t"`
	TMs     int64  `json:"t_ms"`
	Legenda string `json:"legenda"`
	Realce  []int  `json:"realce"`
	Seta    *Seta  `json:"seta"`
	Regiao  string `json:"regiao"`
}

// Seta: de e para são um participante (1–10) ou uma zona.
type Seta struct {
	De   Ponta `json:"de"`
	Para Ponta `json:"para"`
}

type Ponta struct {
	Participante int    `json:"participante,omitempty"`
	Zona         string `json:"zona,omitempty"`
}

// --- Montagem do pedido ------------------------------------------------------

// pedidoAnalise monta o texto que vai à IA e o que a tela mostra como "enviado".
func (s *Servico) pedidoAnalise(ctx context.Context, info InfoPartida, foco Participante) (Pedido, Enviado, []MaterialVoz, error) {
	contexto, err := s.Fontes.ContextoPartida(ctx, info.GameID, foco.ID)
	if err != nil {
		return Pedido{}, Enviado{}, nil, err
	}
	visao, err := s.Fontes.Visao(ctx, info.GameID, foco.ID)
	if err != nil {
		visao = "## Visão\n- Não deu para montar a seção de visão: " + err.Error() + "\n"
	}
	elo := s.EloDeComparacao(ctx, info, foco, true)
	vozes := s.materialVozes(info, foco, elo)
	env := Enviado{Secoes: []string{"participantes (campeão, função, time, nome de invocador)", "pacote de contexto da partida (base de fatos)", "visão (sentinelas e passe de visão de time)"},
		Arquivos: []string{}, Cortadas: []string{}}
	var com, comTemas []string
	var mat strings.Builder
	mat.WriteString("## Material das vozes\n")
	for _, v := range vozes {
		if v.TemMaterial() {
			com = append(com, v.ID)
			if len(v.Temas) > 0 {
				var ts []string
				for _, t := range v.Temas {
					ts = append(ts, t.Tema)
				}
				comTemas = append(comTemas, v.ID+" (temas: "+strings.Join(ts, ", ")+")")
			} else {
				comTemas = append(comTemas, v.ID)
			}
			env.Arquivos = append(env.Arquivos, v.Arquivos...)
			if vozPorDado(v.ID) {
				env.PartidasReferencia += v.Partidas
			}
			fmt.Fprintf(&mat, "\n### Voz %s (%s): MATERIAL\n%s\n", v.ID, v.Rotulo, v.Texto)
		} else {
			fmt.Fprintf(&mat, "\n### Voz %s (%s): SEM MATERIAL (não preencha esta voz)\n", v.ID, v.Rotulo)
		}
	}
	if len(com) > 0 {
		env.Secoes = append(env.Secoes, "material das vozes: "+strings.Join(comTemas, "; "))
	}
	listaVozes := "nenhuma"
	if len(com) > 0 {
		listaVozes = strings.Join(com, ", ")
	}
	time := "azul"
	if foco.Time == 200 {
		time = "vermelho"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "FOCO: participante %d (%s, %s, time %s, %s)\nELO DE COMPARAÇÃO: %s\nVOZES COM MATERIAL: %s\n\n", foco.ID, foco.Campeao,
		nomeFuncaoCurto(foco.Funcao), time, foco.Nome, textoElo(elo), listaVozes)
	b.WriteString(listaParticipantes(info))
	b.WriteString("\n")
	b.WriteString(contexto)
	b.WriteString("\n\n")
	b.WriteString(visao)
	b.WriteString("\n")
	if sit := s.situacoesCandidatas(ctx, info, foco); sit != "" {
		b.WriteString(sit)
		b.WriteString("\n")
		env.Secoes = append(env.Secoes, "situações candidatas (ficha de fatos de cada instante: os 10, ondas, sentinelas, visão, recargas; e as repetições em outras partidas do jogador)")
	}
	b.WriteString(mat.String())
	b.WriteString("\nEscreva o relatório agora, só o JSON.")
	p := Pedido{Tipo: TipoAnalise, Sistema: SistemaAnalise(), Mensagens: []Mensagem{{Papel: PapelUsuario, Texto: b.String()}}}
	env.CaracteresSistema = utf8.RuneCountInString(p.Sistema)
	env.CaracteresMensagem = utf8.RuneCountInString(p.Mensagens[0].Texto)
	if i := strings.Index(contexto, "Fora do pacote"); i >= 0 {
		env.Cortadas = append(env.Cortadas, strings.TrimSpace(firstLine(contexto[i:])))
	}
	return p, env, vozes, nil
}

// MaxCandidatas: quantas situações candidatas o app manda (a IA escolhe 3 a 5).
const MaxCandidatas = 6

// candidatas: os momentos do jogador em foco que mais custaram (mortes,
// janelas perdidas, momentos-chave de que ele participou), por impacto.
func candidatas(info InfoPartida, pid int) []analise.MomentoChave {
	var r []analise.MomentoChave
	for _, m := range info.Momentos {
		dele := m.ParticipanteID == pid && (m.Tipo == "abate" || m.Tipo == "janela_perdida")
		participou := m.Chave && (m.Tipo == "luta" || m.Tipo == "objetivo") && contem(m.Envolvidos, pid)
		if dele || participou {
			r = append(r, m)
		}
	}
	sort.SliceStable(r, func(a, b int) bool { return r[a].Impacto > r[b].Impacto })
	if len(r) > MaxCandidatas {
		r = r[:MaxCandidatas]
	}
	sort.SliceStable(r, func(a, b int) bool { return r[a].TMs < r[b].TMs })
	return r
}

func contem(l []int, v int) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}

// Etiquetas que dizem "a mesma situação" (na ordem de preferência).
var etiquetasDePadrao = []string{"sozinho", "antes_de_objetivo", "sem_troca", "previa_gankando", "previa_invadindo", "previa_farmando"}

// situacoesCandidatas monta a seção das fichas.
func (s *Servico) situacoesCandidatas(ctx context.Context, info InfoPartida, foco Participante) string {
	cs := candidatas(info, foco.ID)
	if len(cs) == 0 {
		return ""
	}
	instantes := make([]int64, len(cs))
	for i, m := range cs {
		// Na morte, a ficha é de 5 s antes: o lance que levou a ela.
		instantes[i] = m.TMs
		if m.Tipo == "abate" && m.TMs >= 5000 {
			instantes[i] = m.TMs - 5000
		}
	}
	fichas, err := s.Fontes.Fichas(ctx, info.GameID, foco.ID, instantes)
	if err != nil {
		fichas = nil
	}
	var b strings.Builder
	b.WriteString("## Situações candidatas (o app escolheu as que mais custaram: mortes, janelas perdidas e momentos-chave de que o jogador em foco participou; impacto em ouro equivalente)\n")
	for i, m := range cs {
		fmt.Fprintf(&b, "\n### S%d [%s] %s%s, impacto ~%d\n", i+1, analise.MinSeg(m.TMs), m.Tipo, map[bool]string{true: " (" + m.Subtipo + ")", false: ""}[m.Subtipo != ""], m.Impacto)
		fmt.Fprintf(&b, "- O que aconteceu: %s Custo: azul %d, vermelho %d.", m.Frase, m.CustoAzul, m.CustoVermelho)
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
		if i < len(fichas) {
			fmt.Fprintf(&b, "- Ficha do instante %s%s:\n%s", analise.MinSeg(instantes[i]),
				map[bool]string{true: " (5 s antes da morte)", false: ""}[instantes[i] != m.TMs], fichas[i])
		}
		tag := ""
		for _, e := range etiquetasDePadrao {
			if contem2(m.Tags, e) {
				tag = e
				break
			}
		}
		if tag == "" {
			continue
		}
		var mesmas []string
		for _, o := range info.Momentos {
			if o.N != m.N && o.Tipo == m.Tipo && o.ParticipanteID == foco.ID && contem2(o.Tags, tag) {
				mesmas = append(mesmas, analise.MinSeg(o.TMs))
			}
		}
		if len(mesmas) > 0 {
			fmt.Fprintf(&b, "- Mesmo tipo e etiqueta %q nesta partida também em: %s.\n", tag, strings.Join(mesmas, ", "))
		}
		if outras, err := s.Fontes.OutrasDoJogador(foco.Puuid, m.Tipo, tag, info.GameID); err == nil && len(outras) > 0 {
			var ls []string
			n := 0
			for _, l := range outras {
				for _, mo := range l.Momentos {
					n++
					if len(ls) < 6 {
						ls = append(ls, fmt.Sprintf("partida %d %s", l.GameID, analise.MinSeg(mo.TMs)))
					}
				}
			}
			fmt.Fprintf(&b, "- Em OUTRAS partidas guardadas deste jogador, %s com a etiqueta %q: %d vez(es) (%s).\n", m.Tipo, tag, n, strings.Join(ls, "; "))
		}
	}
	return b.String()
}

func contem2(l []string, v string) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// materialVozes: arquivos (ou temas) e a comparação com as referências da
// coleta por elo de cada voz. Os temas e a comparação por situação saem das
// mesmas situações candidatas que o pedido manda (S1, S2... na mesma ordem).
// elo é o elo de comparação do jogador em foco (elo.go).
func (s *Servico) materialVozes(info InfoPartida, foco Participante, elo EloComparacao) []MaterialVoz {
	var r []MaterialVoz
	sits := candidatas(info, foco.ID)
	var eu *analise.IndicadoresJogador
	for _, v := range Vozes {
		m := MaterialVoz{Voz: v, Arquivos: []string{}, Temas: []TemaEscolhido{}, Fora: []string{}}
		if v.ID == IDVozEspecialista {
			arqs, texto, partidas, amostra, falta := s.materialEspecialista(info, foco, elo)
			m.Arquivos = append(m.Arquivos, arqs...)
			m.Texto, m.Partidas, m.Amostra, m.Falta = texto, partidas, amostra, falta
			m.Caracteres = utf8.RuneCountInString(texto)
			r = append(r, m)
			continue
		}
		temas, fora, texto, ok := LeTemasDaVoz(s.Conhecimento, v, sits, foco.Funcao)
		m.Fora = append(m.Fora, fora...)
		if ok {
			m.Temas = temas
			for _, t := range temas {
				m.Arquivos = append(m.Arquivos, t.Arquivo)
			}
		} else {
			var nomes []string
			nomes, texto = LeArquivosDaVoz(s.Conhecimento, v, foco.Campeao, elo.Elo)
			m.Arquivos = append(m.Arquivos, nomes...)
		}
		var b strings.Builder
		b.WriteString(texto)
		ref := referenciaDaVoz(v, foco, elo, info.GameID)
		faltaRef := ""
		if ref != nil {
			faltaRef = ref.falta
			var refs []analise.IndicadoresJogador
			if ref.filtro.Funcao != "" {
				var err error
				if refs, err = s.Fontes.Referencias(ref.filtro); err != nil {
					faltaRef = "não deu para ler as referências: " + err.Error()
					refs = nil
				}
			}
			if len(refs) > 0 && eu == nil {
				x, err := s.Fontes.Indicadores(info.GameID, foco.ID)
				if err != nil {
					faltaRef = "não deu para ler os números do jogador em foco: " + err.Error()
					refs = nil
				} else {
					eu = &x
				}
			}
			if len(refs) > 0 {
				txt, amostra := materialReferencia(*eu, refs, sits, ref)
				m.Partidas, m.Amostra, faltaRef = len(refs), amostra, ""
				if b.Len() > 0 {
					b.WriteString("\n\n")
				}
				b.WriteString(txt)
			} else if ref.filtro.Funcao != "" {
				// Há referência de OUTRA função? A voz não usa, mas a falta diz.
				outro := ref.filtro
				outro.Funcao = ""
				if outras, err := s.Fontes.Referencias(outro); err == nil && len(outras) > 0 {
					faltaRef = fmt.Sprintf("há %s de referência nesse filtro, mas nenhuma %s (a voz não compara com outra função); %s",
						plural(len(outras), "partida", "partidas"), FuncaoNaFrase(foco.Funcao), ref.falta)
				}
				if b.Len() > 0 {
					// A voz fala pelo arquivo; a IA precisa saber que não há dado.
					fmt.Fprintf(&b, "\n\n#### Referência por dado: NENHUMA (%s). Não cite partidas de referência nem compare com outra função.", faltaRef)
				}
			}
		}
		m.Texto = b.String()
		m.Caracteres = utf8.RuneCountInString(m.Texto)
		if !m.TemMaterial() {
			falta := "sem o arquivo " + strings.Join(ArquivosDaVoz(v, foco.Campeao, elo.Elo), " ou ") + " na pasta de conhecimento"
			if p := PastaDeTemas(v); p != "" {
				falta = "sem tema da pasta " + p + "/ que case com as situações e sem o arquivo " + v.Arquivo + " na pasta de conhecimento"
			}
			if faltaRef != "" {
				// Nas vozes por dado, o que falta de dado vem primeiro (é o que a pessoa resolve rodando a coleta).
				falta = faltaRef + "; ou " + falta
			}
			m.Falta = falta
		} else if faltaRef != "" {
			m.Falta = "fala só pelo arquivo: " + faltaRef
		}
		r = append(r, m)
	}
	return r
}

// --- Validação da resposta ---------------------------------------------------

// tiraCerca: tira a cerca de código e o que vier antes do { e depois do }.
func extraiJSON(s string) string {
	s = strings.TrimSpace(s)
	i := strings.IndexByte(s, '{')
	j := strings.LastIndexByte(s, '}')
	if i < 0 || j < i {
		return s
	}
	return s[i : j+1]
}

// semComentarios tira // comentários fora de strings (o esquema do prompt tem
// comentários e a IA às vezes os copia).
func semComentarios(s string) string {
	var b strings.Builder
	emString, escape := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if emString {
			b.WriteByte(c)
			if escape {
				escape = false
			} else if c == '\\' {
				escape = true
			} else if c == '"' {
				emString = false
			}
			continue
		}
		if c == '"' {
			emString = true
		}
		if c == '/' && i+1 < len(s) && s[i+1] == '/' {
			for i < len(s) && s[i] != '\n' {
				i++
			}
			if i < len(s) {
				b.WriteByte('\n')
			}
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// Formato solto da resposta (números podem vir como texto; seta como número
// ou zona).
type quadroBruto struct {
	T       string        `json:"t"`
	Legenda string        `json:"legenda"`
	Realce  []json.Number `json:"realce"`
	Seta    *struct {
		De   json.RawMessage `json:"de"`
		Para json.RawMessage `json:"para"`
	} `json:"seta"`
	Regiao *string `json:"regiao"`
}

type relatorioBruto struct {
	Resumo  string `json:"resumo"`
	Padroes []struct {
		Texto     string   `json:"texto"`
		Instantes []string `json:"instantes"`
	} `json:"padroes"`
	Situacoes []struct {
		T        string `json:"t"`
		Titulo   string `json:"titulo"`
		Situacao string `json:"situacao"`
		Pergunta string `json:"pergunta"`
		Vozes    []struct {
			Voz      string `json:"voz"`
			Resposta string `json:"resposta"`
			Fonte    string `json:"fonte"`
		} `json:"vozes"`
		Leitura    string          `json:"leitura"`
		ProximaVez string          `json:"proxima_vez"`
		Repeticoes []string        `json:"repeticoes"`
		Custo      string          `json:"custo"`
		Impacto    json.RawMessage `json:"impacto"`
		Fonte      string          `json:"fonte"`
		Quadro     *quadroBruto    `json:"quadro"`
	} `json:"situacoes"`
	JogadoresChave []struct {
		Participante json.Number `json:"participante"`
		Papel        string      `json:"papel"`
		PorQue       string      `json:"por_que"`
		Instantes    []string    `json:"instantes"`
		Fonte        string      `json:"fonte"`
	} `json:"jogadores_chave"`
	Build []struct {
		T           string `json:"t"`
		Item        string `json:"item"`
		Observacao  string `json:"observacao"`
		Alternativa string `json:"alternativa"`
		Fonte       string `json:"fonte"`
	} `json:"build"`
	Janelas []struct {
		T         string       `json:"t"`
		Tipo      string       `json:"tipo"`
		Fato      string       `json:"fato"`
		OQueFazer string       `json:"o_que_fazer"`
		Fonte     string       `json:"fonte"`
		Quadro    *quadroBruto `json:"quadro"`
	} `json:"janelas"`
	Visao struct {
		Texto string `json:"texto"`
		Itens []struct {
			T     string `json:"t"`
			Fato  string `json:"fato"`
			Fonte string `json:"fonte"`
		} `json:"itens"`
	} `json:"visao"`
}

// ErrFormato: a IA não respondeu no formato pedido.
var ErrFormato = errors.New("a resposta da IA não veio no formato pedido (JSON do relatório)")

// ValidaRelatorio lê a resposta da IA e devolve o relatório normalizado e os
// avisos (o que foi descartado e por quê). Instante fora da partida ou
// ausente derruba o item: toda afirmação tem de apontar um instante.
func ValidaRelatorio(texto string, info InfoPartida, vozes []MaterialVoz) (ConteudoRelatorio, []string, error) {
	var rb relatorioBruto
	bruto := extraiJSON(texto)
	dec := func(s string) error {
		d := json.NewDecoder(strings.NewReader(s))
		d.UseNumber()
		return d.Decode(&rb)
	}
	if err := dec(bruto); err != nil {
		rb = relatorioBruto{}
		if err2 := dec(semComentarios(bruto)); err2 != nil {
			return ConteudoRelatorio{}, nil, fmt.Errorf("%w: %v", ErrFormato, err)
		}
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
	c := ConteudoRelatorio{Resumo: limpa(rb.Resumo, 1200), Padroes: []Padrao{}, Situacoes: []Situacao{}, JogadoresChave: []JogadorChave{},
		Build: []ItemBuild{}, Janelas: []Janela{}, Visao: SecaoVisao{Texto: limpa(rb.Visao.Texto, 1500), Itens: []ItemVisao{}}, Vozes: []VozRelatorio{}}
	instantes := func(ts []string) []string {
		r := []string{}
		for _, t := range ts {
			if ms, ok := msDeTexto(t); ok && (duracaoMs == 0 || ms <= duracaoMs+1000) {
				r = append(r, analise.MinSeg(ms))
			}
		}
		return r
	}
	for _, p := range rb.Padroes {
		if t := limpa(p.Texto, 600); t != "" {
			c.Padroes = append(c.Padroes, Padrao{Texto: t, Instantes: instantes(p.Instantes)})
		}
	}
	comMaterial := map[string]MaterialVoz{}
	for _, m := range vozes {
		if m.TemMaterial() {
			comMaterial[m.ID] = m
		}
	}
	for i, d := range rb.Situacoes {
		ms, t, ok := instante(fmt.Sprintf("situação %d", i+1), d.T)
		if !ok {
			continue
		}
		x := Situacao{T: t, TMs: ms, Titulo: limpa(d.Titulo, 160), Situacao: limpa(d.Situacao, 1500), Pergunta: limpa(d.Pergunta, 300),
			Leitura: limpa(d.Leitura, 1200), ProximaVez: limpa(d.ProximaVez, 300), Repeticoes: instantes(d.Repeticoes),
			Custo: limpa(d.Custo, 200), Impacto: numeroSolto(d.Impacto), Fonte: limpa(d.Fonte, 200), Vozes: []RespostaVoz{}}
		daIA := map[string]int{}
		for k, v := range d.Vozes {
			daIA[strings.TrimSpace(v.Voz)] = k
		}
		for id := range daIA {
			if _, ok := comMaterial[id]; !ok {
				avisos = append(avisos, fmt.Sprintf("situação %d: a IA escreveu pela voz %q, que não tem material: descartado", i+1, id))
			}
		}
		// Todas as vozes, na ordem fixa: as sem material com o aviso do app.
		for _, m := range vozes {
			rv := RespostaVoz{Voz: m.ID, Rotulo: m.Rotulo}
			k, falou := daIA[m.ID]
			switch {
			case !m.TemMaterial():
				rv.Resposta, rv.SemDados = SemMaterial, true
			case !falou || strings.TrimSpace(d.Vozes[k].Resposta) == "":
				rv.Resposta, rv.SemDados = "O material desta voz não cobre esta situação (a IA não respondeu por ela).", true
			default:
				rv.Resposta, rv.Fonte = limpa(d.Vozes[k].Resposta, 1200), limpa(d.Vozes[k].Fonte, 200)
				// Voz por dado: a resposta tem de dizer a amostra (prompt). Se não
				// disse, o app escreve na frente: a pessoa nunca lê um "os melhores
				// fazem X" sem saber que é 1 partida.
				if m.Amostra != "" && !citaAmostra(rv.Resposta, m) {
					rv.Resposta = "(Amostra: " + m.Amostra + ".) " + rv.Resposta
					avisos = append(avisos, fmt.Sprintf("situação %d: a voz %s não disse o tamanho da amostra; o app escreveu", i+1, m.ID))
				}
			}
			x.Vozes = append(x.Vozes, rv)
		}
		x.Quadro = validaQuadro(d.Quadro, t, ms, info, &avisos, fmt.Sprintf("quadro da situação %d", i+1))
		c.Situacoes = append(c.Situacoes, x)
	}
	sort.SliceStable(c.Situacoes, func(a, b int) bool { return c.Situacoes[a].Impacto > c.Situacoes[b].Impacto })
	if len(c.Situacoes) > 6 {
		c.Situacoes = c.Situacoes[:6]
	}
	for i, j := range rb.JogadoresChave {
		n, _ := j.Participante.Int64()
		p, ok := info.Do(int(n))
		if !ok {
			avisos = append(avisos, fmt.Sprintf("jogador-chave %d descartado: participante %q não existe", i+1, j.Participante))
			continue
		}
		jc := JogadorChave{Participante: p.ID, Campeao: p.Campeao, Time: p.Time, Papel: limpa(j.Papel, 160), PorQue: limpa(j.PorQue, 800),
			Fonte: limpa(j.Fonte, 200), Instantes: []string{}}
		for _, t := range j.Instantes {
			if ms, ok := msDeTexto(t); ok && (duracaoMs == 0 || ms <= duracaoMs+1000) {
				jc.Instantes = append(jc.Instantes, analise.MinSeg(ms))
			}
		}
		c.JogadoresChave = append(c.JogadoresChave, jc)
	}
	for i, x := range rb.Build {
		ms, t, ok := instante(fmt.Sprintf("item de build %d", i+1), x.T)
		if !ok {
			continue
		}
		c.Build = append(c.Build, ItemBuild{T: t, TMs: ms, Item: limpa(x.Item, 120), Observacao: limpa(x.Observacao, 800),
			Alternativa: limpa(x.Alternativa, 800), Fonte: limpa(x.Fonte, 200)})
	}
	for i, x := range rb.Janelas {
		ms, t, ok := instante(fmt.Sprintf("janela %d", i+1), x.T)
		if !ok {
			continue
		}
		tipo := strings.ToLower(strings.TrimSpace(x.Tipo))
		if tipo != "perdida" && tipo != "aproveitada" {
			tipo = "perdida"
			avisos = append(avisos, fmt.Sprintf("janela %d: tipo %q virou \"perdida\"", i+1, x.Tipo))
		}
		jn := Janela{T: t, TMs: ms, Tipo: tipo, Fato: limpa(x.Fato, 800), OQueFazer: limpa(x.OQueFazer, 800), Fonte: limpa(x.Fonte, 200)}
		jn.Quadro = validaQuadro(x.Quadro, t, ms, info, &avisos, fmt.Sprintf("quadro da janela %d", i+1))
		c.Janelas = append(c.Janelas, jn)
	}
	for i, x := range rb.Visao.Itens {
		ms, t, ok := instante(fmt.Sprintf("item de visão %d", i+1), x.T)
		if !ok {
			continue
		}
		c.Visao.Itens = append(c.Visao.Itens, ItemVisao{T: t, TMs: ms, Fato: limpa(x.Fato, 600), Fonte: limpa(x.Fonte, 200)})
	}
	for _, m := range vozes {
		vr := VozRelatorio{Voz: m.ID, Rotulo: m.Rotulo}
		if m.TemMaterial() {
			partes := append([]string{}, m.Arquivos...)
			if m.Partidas > 0 {
				partes = append(partes, fmt.Sprintf("%d partida(s) de referência (amostra: %s)", m.Partidas, m.Amostra))
			}
			vr.Texto = "Material: " + strings.Join(partes, ", ") + "."
		} else {
			vr.Texto, vr.SemDados = SemMaterial+" ("+m.Falta+")", true
		}
		c.Vozes = append(c.Vozes, vr)
	}
	if c.Resumo == "" && len(c.Situacoes) == 0 && len(c.Janelas) == 0 {
		return c, avisos, ErrFormato
	}
	return c, avisos, nil
}

func numeroSolto(r json.RawMessage) int {
	s := strings.Trim(strings.TrimSpace(string(r)), `"`)
	s = strings.NewReplacer("~", "", " ", "", ".", "", "ouro", "").Replace(s)
	var v float64
	if _, err := fmt.Sscan(s, &v); err != nil {
		return 0
	}
	return int(v)
}

func codigoZona(s string) (string, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	for _, z := range CodigosZona {
		if z == s {
			return z, true
		}
	}
	return "", false
}

// ponta lê um lado da seta: número (participante) ou zona.
func ponta(r json.RawMessage, info InfoPartida) (Ponta, bool) {
	var n json.Number
	if json.Unmarshal(r, &n) == nil {
		if v, err := n.Int64(); err == nil {
			if _, ok := info.Do(int(v)); ok {
				return Ponta{Participante: int(v)}, true
			}
		}
		return Ponta{}, false
	}
	var s string
	if json.Unmarshal(r, &s) == nil {
		var v int
		if _, err := fmt.Sscan(s, &v); err == nil {
			if _, ok := info.Do(v); ok {
				return Ponta{Participante: v}, true
			}
		}
		if z, ok := codigoZona(s); ok {
			return Ponta{Zona: z}, true
		}
	}
	return Ponta{}, false
}

func validaQuadro(q *quadroBruto, t string, ms int64, info InfoPartida, avisos *[]string, onde string) *Quadro {
	if q == nil {
		return nil
	}
	r := &Quadro{T: t, TMs: ms, Legenda: strings.TrimSpace(q.Legenda), Realce: []int{}}
	if qms, ok := msDeTexto(q.T); ok && (info.DuracaoS == 0 || qms <= int64(info.DuracaoS)*1000+1000) {
		r.TMs, r.T = qms, analise.MinSeg(qms)
	}
	if rr := []rune(r.Legenda); len(rr) > 160 {
		r.Legenda = string(rr[:160]) + "…"
	}
	for _, n := range q.Realce {
		v, err := n.Int64()
		if _, ok := info.Do(int(v)); err == nil && ok && len(r.Realce) < 6 {
			r.Realce = append(r.Realce, int(v))
		} else {
			*avisos = append(*avisos, fmt.Sprintf("%s: realce %q ignorado", onde, n))
		}
	}
	if q.Seta != nil && len(q.Seta.De) > 0 && string(q.Seta.De) != "null" {
		de, ok1 := ponta(q.Seta.De, info)
		para, ok2 := ponta(q.Seta.Para, info)
		if ok1 && ok2 {
			r.Seta = &Seta{De: de, Para: para}
		} else {
			*avisos = append(*avisos, fmt.Sprintf("%s: seta ignorada (ponta que não é participante nem zona)", onde))
		}
	}
	if q.Regiao != nil && strings.TrimSpace(*q.Regiao) != "" {
		if z, ok := codigoZona(*q.Regiao); ok {
			r.Regiao = z
		} else {
			*avisos = append(*avisos, fmt.Sprintf("%s: região %q ignorada", onde, *q.Regiao))
		}
	}
	return r
}

// --- Banco --------------------------------------------------------------------

const colunasRelatorio = `id, game_id, participante_id, versao, estado, erro, criado_em, terminado_em, modelo, implementacao, versao_prompt,
	conteudo, bruto, avisos, vozes, enviado, duracao_ms, caracteres_entrada, caracteres_saida, tokens_entrada, tokens_saida, custo_usd`

func leRelatorio(s interface{ Scan(...any) error }) (Relatorio, error) {
	var r Relatorio
	var conteudo, avisos, vozes, enviado string
	err := s.Scan(&r.ID, &r.GameID, &r.ParticipanteID, &r.Versao, &r.Estado, &r.Erro, &r.CriadoEm, &r.TerminadoEm, &r.Modelo,
		&r.Implementacao, &r.VersaoPrompt, &conteudo, &r.Bruto, &avisos, &vozes, &enviado, &r.Uso.DuracaoMs,
		&r.Uso.CaracteresEntrada, &r.Uso.CaracteresSaida, &r.Uso.TokensEntrada, &r.Uso.TokensSaida, &r.Uso.CustoUSD)
	if err != nil {
		return r, err
	}
	r.Uso.Modelo = r.Modelo
	if conteudo != "" {
		var c ConteudoRelatorio
		if json.Unmarshal([]byte(conteudo), &c) == nil {
			r.Conteudo = &c
		}
	}
	_ = json.Unmarshal([]byte(avisos), &r.Avisos)
	_ = json.Unmarshal([]byte(vozes), &r.Vozes)
	_ = json.Unmarshal([]byte(enviado), &r.Enviado)
	r.Avisos = web.NuncaNil(r.Avisos)
	r.Vozes = web.NuncaNil(r.Vozes)
	r.Enviado.Secoes = web.NuncaNil(r.Enviado.Secoes)
	r.Enviado.Arquivos = web.NuncaNil(r.Enviado.Arquivos)
	r.Enviado.Cortadas = web.NuncaNil(r.Enviado.Cortadas)
	if r.Conteudo != nil {
		// Com o relatório lido, a resposta crua não precisa ir à tela.
		r.Bruto = ""
	}
	return r, nil
}

// LeRelatorio: a versão pedida (0 = a mais nova) do relatório de um jogador.
// ok falso se não há nenhum.
func LeRelatorio(db *banco.DB, gameID int64, pid, versao int) (Relatorio, bool, error) {
	q := `SELECT ` + colunasRelatorio + ` FROM ia_relatorios WHERE game_id = ? AND participante_id = ?`
	args := []any{gameID, pid}
	if versao > 0 {
		q += ` AND versao = ?`
		args = append(args, versao)
	}
	q += ` ORDER BY versao DESC LIMIT 1`
	r, err := leRelatorio(db.QueryRow(q, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return r, false, nil
	}
	return r, err == nil, err
}

// VersaoResumo: uma linha da lista de versões.
type VersaoResumo struct {
	Versao   int    `json:"versao"`
	Estado   string `json:"estado"`
	CriadoEm string `json:"criado_em"`
	Modelo   string `json:"modelo"`
}

// ListaVersoes: as versões do relatório de um jogador, da mais nova.
func ListaVersoes(db *banco.DB, gameID int64, pid int) ([]VersaoResumo, error) {
	linhas, err := db.Query(`SELECT versao, estado, criado_em, modelo FROM ia_relatorios WHERE game_id = ? AND participante_id = ?
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

// RelatoriosDaPartida: o último de cada jogador (a tela da partida marca quem tem).
func RelatoriosDaPartida(db *banco.DB, gameID int64) ([]VersaoResumo, []int, error) {
	linhas, err := db.Query(`SELECT participante_id, MAX(versao) FROM ia_relatorios WHERE game_id = ? AND estado = 'concluido'
		GROUP BY participante_id ORDER BY participante_id`, gameID)
	if err != nil {
		return nil, nil, err
	}
	defer linhas.Close()
	var vs []VersaoResumo
	var pids []int
	for linhas.Next() {
		var pid, v int
		if err := linhas.Scan(&pid, &v); err != nil {
			return nil, nil, err
		}
		pids = append(pids, pid)
		vs = append(vs, VersaoResumo{Versao: v, Estado: "concluido"})
	}
	return vs, pids, linhas.Err()
}

// --- A execução ----------------------------------------------------------------

// ErrPedido: pedido malformado (400); ErrRodando: já roda (409).
type ErrPedido struct{ Msg string }

func (e *ErrPedido) Error() string { return e.Msg }

type ErrRodando struct {
	Msg      string
	Execucao *web.Execucao
}

func (e *ErrRodando) Error() string { return e.Msg }

func chaveRelatorio(gameID int64, pid int) string { return fmt.Sprintf("r:%d:%d", gameID, pid) }

// PreviaAnalise devolve exatamente o texto que a análise mandaria à IA (sem
// chamar nada): a pessoa pode ler o que sai da máquina antes de pedir.
// Devolve também o material de cada voz (que temas entraram e por quê).
func (s *Servico) PreviaAnalise(ctx context.Context, gameID int64, pid int) (Pedido, Enviado, []MaterialVoz, error) {
	info, err := s.Fontes.Partida(ctx, gameID)
	if err != nil {
		return Pedido{}, Enviado{}, nil, err
	}
	foco, ok := info.Do(pid)
	if !ok {
		return Pedido{}, Enviado{}, nil, &ErrPedido{fmt.Sprintf("o participante %d não está na partida", pid)}
	}
	return s.pedidoAnalise(ctx, info, foco)
}

// Analisa começa o relatório de um jogador. Devolve a linha criada (estado
// rodando) e a execução; o trabalho segue em segundo plano.
func (s *Servico) Analisa(ctx context.Context, gameID int64, pid int) (Relatorio, *web.Execucao, error) {
	info, err := s.Fontes.Partida(ctx, gameID)
	if err != nil {
		return Relatorio{}, nil, err
	}
	foco, ok := info.Do(pid)
	if !ok {
		return Relatorio{}, nil, &ErrPedido{fmt.Sprintf("o participante %d não está na partida", pid)}
	}
	est := s.IA.Estado(ctx)
	if !est.Disponivel {
		return Relatorio{}, nil, &erroIA{est.Motivo, ErrIndisponivel}
	}
	e := s.Hub.Nova()
	chave := chaveRelatorio(gameID, pid)
	if x := s.ocupa(chave, e); x != nil {
		e.Fecha()
		return Relatorio{}, nil, &ErrRodando{"a análise com IA deste jogador já está rodando", x}
	}
	var versao int
	_ = s.DB.QueryRow(`SELECT COALESCE(MAX(versao), 0) + 1 FROM ia_relatorios WHERE game_id = ? AND participante_id = ?`, gameID, pid).Scan(&versao)
	res, err := s.DB.Exec(`INSERT INTO ia_relatorios (game_id, participante_id, versao, estado, criado_em, implementacao, versao_prompt)
		VALUES (?, ?, ?, 'rodando', ?, ?, ?)`, gameID, pid, versao, banco.Agora(), est.Implementacao, VersaoPrompt)
	if err != nil {
		s.libera(chave, e)
		e.Fecha()
		return Relatorio{}, nil, err
	}
	id, _ := res.LastInsertId()
	ctxTrab, cancela := context.WithCancel(context.Background())
	e.AoCancelar(cancela)
	go func() {
		defer cancela()
		defer s.libera(chave, e)
		defer e.Fecha()
		s.rodaAnalise(ctxTrab, e, id, info, foco, est)
	}()
	r, _, err := LeRelatorio(s.DB, gameID, pid, versao)
	return r, e, err
}

func (s *Servico) rodaAnalise(ctx context.Context, e *web.Execucao, id int64, info InfoPartida, foco Participante, est Estado) {
	termina := func(estado, erro string, conteudo *ConteudoRelatorio, bruto string, avisos []string, vozes []MaterialVoz, env Enviado, uso Resposta) {
		cj := ""
		if conteudo != nil {
			b, _ := json.Marshal(conteudo)
			cj = string(b)
		}
		aj, _ := json.Marshal(web.NuncaNil(avisos))
		vj, _ := json.Marshal(web.NuncaNil(vozes))
		ej, _ := json.Marshal(env)
		if _, err := s.DB.Exec(`UPDATE ia_relatorios SET estado = ?, erro = ?, terminado_em = ?, modelo = ?, conteudo = ?, bruto = ?, avisos = ?,
			vozes = ?, enviado = ?, duracao_ms = ?, caracteres_entrada = ?, caracteres_saida = ?, tokens_entrada = ?, tokens_saida = ?, custo_usd = ?
			WHERE id = ?`, estado, erro, banco.Agora(), uso.Modelo, cj, bruto, string(aj), string(vj), string(ej), uso.DuracaoMs,
			uso.CaracteresEntrada, uso.CaracteresSaida, uso.TokensEntrada, uso.TokensSaida, uso.CustoUSD, id); err != nil {
			log.Printf("ia: gravar o relatório %d: %v", id, err)
		}
	}
	if s.ContextoAntes != nil && s.ContextoAntes(ctx, info.GameID, e.Emite) {
		// A base de fatos foi refeita com o contexto dos jogadores: relê.
		if x, err := s.Fontes.Partida(ctx, info.GameID); err == nil {
			info = x
			if f, ok := x.Do(foco.ID); ok {
				foco = f
			}
		}
	}
	p, env, vozes, err := s.pedidoAnalise(ctx, info, foco)
	if err != nil {
		termina("falhou", "não deu para montar o texto da partida: "+err.Error(), nil, "", nil, nil, env, Resposta{})
		e.Falha("não deu para montar o texto da partida: " + err.Error())
		return
	}
	var semMat []map[string]string
	for _, v := range vozes {
		if !v.TemMaterial() {
			semMat = append(semMat, map[string]string{"voz": v.ID, "falta": v.Falta})
		}
	}
	e.Emite("contexto", map[string]any{"enviado": env, "vozes_sem_material": web.NuncaNil(semMat), "modelo": est.Modelos[TipoAnalise]})
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
		log.Printf("ia: relatório %d: %v (%v)", id, err, errors.Unwrap(err))
		termina(estado, err.Error(), nil, uso.Texto, nil, vozes, env, uso)
		if estado == "cancelado" {
			e.Emite("cancelado", map[string]any{"id": id})
			return
		}
		e.Falha(err.Error())
		return
	}
	conteudo, avisos, err := ValidaRelatorio(uso.Texto, info, vozes)
	if err != nil {
		termina("falhou", err.Error(), nil, uso.Texto, avisos, vozes, env, uso)
		e.Falha(err.Error() + " — a resposta crua ficou guardada; refaça a análise")
		return
	}
	termina("concluido", "", &conteudo, uso.Texto, avisos, vozes, env, uso)
	r, _, _ := LeRelatorio(s.DB, info.GameID, foco.ID, 0)
	e.Emite("concluido", map[string]any{"relatorio": r})
}

// citaAmostra: a resposta de uma voz por dado diz o tamanho da amostra
// ("amostra", ou "1 partida"/"3 partidas" com o número certo).
func citaAmostra(resposta string, m MaterialVoz) bool {
	r := strings.ToLower(resposta)
	return strings.Contains(r, "amostra") || strings.Contains(r, plural(m.Partidas, "partida", "partidas")) ||
		(m.Partidas == 1 && (strings.Contains(r, "uma partida") || strings.Contains(r, "única partida")))
}
