package ia

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Terminal chama o Claude Code instalado na máquina, um processo por chamada:
//
//	claude -p --output-format stream-json --verbose --include-partial-messages
//	  --model <modelo> [--fallback-model <reserva>] --effort <esforço>
//	  --system-prompt-file <arquivo temporário> --tools "" --strict-mcp-config
//	  --safe-mode --disable-slash-commands --no-session-persistence
//	  --permission-mode dontAsk --max-turns 1
//
// e o texto da conversa pela entrada padrão. O porquê de cada opção está em
// argumentos(). O processo roda numa pasta temporária vazia (removida no fim)
// e é encerrado quando o contexto é cancelado ou vence.
type Terminal struct {
	// Executavel: "claude" (procurado no PATH) ou o caminho (LOL_IA_CLAUDE).
	Executavel string
	// Modelos e Reservas por tipo de pedido (TipoAnalise, TipoConversa).
	Modelos  map[string]string
	Reservas map[string]string
	Esforcos map[string]string
	Tempos   map[string]time.Duration

	// prefixo: argumentos antes dos do Claude Code (o teste roda o próprio
	// binário de teste fazendo o papel do claude).
	prefixo []string

	mu         sync.Mutex
	estado     *Estado
	estadoEm   time.Time
	estadoErro error
}

// NovoTerminal monta o cliente com os padrões do app.
func NovoTerminal(executavel, modeloAnalise, modeloConversa string) *Terminal {
	if strings.TrimSpace(executavel) == "" {
		executavel = "claude"
	}
	if modeloAnalise == "" {
		modeloAnalise = ModeloAnalisePadrao
	}
	if modeloConversa == "" {
		modeloConversa = ModeloConversaPadrao
	}
	return &Terminal{
		Executavel: executavel,
		Modelos:    map[string]string{TipoAnalise: modeloAnalise, TipoConversa: modeloConversa},
		Reservas:   map[string]string{TipoAnalise: ReservaAnalisePadrao},
		Esforcos:   map[string]string{TipoAnalise: "high", TipoConversa: "medium"},
		Tempos:     map[string]time.Duration{TipoAnalise: 15 * time.Minute, TipoConversa: 5 * time.Minute},
	}
}

// Os modelos padrão, por apelido do Claude Code (o apelido segue o modelo mais
// novo daquela família sem mexer no app). A análise usa o mais capaz; se ele
// não estiver disponível na conta, o próprio Claude Code cai na reserva
// (--fallback-model) e, se nem assim, o app tenta de novo uma vez com ela.
// A conversa usa um mais rápido.
const (
	ModeloAnalisePadrao  = "fable"
	ReservaAnalisePadrao = "opus"
	ModeloConversaPadrao = "sonnet"
)

// argumentos devolve a linha de comando de uma chamada.
func (t *Terminal) argumentos(modelo, reserva, esforco, arqSistema string) []string {
	a := append([]string{}, t.prefixo...)
	a = append(a,
		// Modo não interativo: lê a entrada padrão, responde e sai.
		"-p",
		// Uma linha JSON por evento, com os pedaços do texto assim que chegam
		// (--include-partial-messages); --verbose é exigido pelo stream-json.
		"--output-format", "stream-json", "--verbose", "--include-partial-messages",
		"--model", modelo,
	)
	if reserva != "" && reserva != modelo {
		a = append(a, "--fallback-model", reserva)
	}
	if esforco != "" {
		a = append(a, "--effort", esforco)
	}
	a = append(a,
		// O prompt de sistema do app SUBSTITUI o do Claude Code (que fala de
		// programar, da pasta atual, do git): a IA só sabe o que o app manda.
		// Em arquivo e não na linha de comando: o Windows limita a linha.
		"--system-prompt-file", arqSistema,
		// Nenhuma ferramenta embutida: não lê nem escreve arquivo, não roda
		// comando, não navega. A IA só lê o texto e responde.
		"--tools", "",
		// Nenhum servidor MCP (só os de --mcp-config, e não passamos nenhum).
		"--strict-mcp-config",
		// Sem CLAUDE.md, memória, skills, plugins, hooks nem agentes da
		// máquina: nada do que a pessoa configurou para programar entra no
		// contexto nem sai junto com o pedido.
		"--safe-mode",
		"--disable-slash-commands",
		// Não grava a sessão em disco (~/.claude/projects): o histórico é do
		// app, no banco dele.
		"--no-session-persistence",
		// Se algo ainda pedisse permissão, é negado sem perguntar a ninguém.
		"--permission-mode", "dontAsk",
		// Uma volta só: sem ferramenta não há o que continuar.
		"--max-turns", "1",
	)
	return a
}

// ambiente: o do servidor, sem as variáveis que o Claude Code põe nos
// processos que ele mesmo abre (se o servidor foi aberto de dentro de uma
// sessão dele, o filho se acharia aninhado).
func ambiente() []string {
	var r []string
	for _, kv := range os.Environ() {
		nome := strings.ToUpper(strings.SplitN(kv, "=", 2)[0])
		if nome == "CLAUDECODE" || nome == "CLAUDE_CODE_ENTRYPOINT" || nome == "CLAUDE_CODE_SSE_PORT" {
			continue
		}
		r = append(r, kv)
	}
	return r
}

// TextoDaConversa junta a conversa num texto só (a entrada padrão do
// terminal): o Claude Code em -p recebe uma mensagem; o histórico vai dentro
// dela, marcado. Uma mensagem só vai como está.
func TextoDaConversa(ms []Mensagem) string {
	if len(ms) == 1 {
		return ms[0].Texto
	}
	var b strings.Builder
	b.WriteString("<conversa_ate_aqui>\n")
	for _, m := range ms[:len(ms)-1] {
		quem := "USUÁRIO"
		if m.Papel == PapelAssistente {
			quem = "VOCÊ (resposta anterior)"
		}
		fmt.Fprintf(&b, "\n### %s\n%s\n", quem, strings.TrimSpace(m.Texto))
	}
	b.WriteString("</conversa_ate_aqui>\n\n### MENSAGEM ATUAL DO USUÁRIO (responda a esta)\n")
	b.WriteString(strings.TrimSpace(ms[len(ms)-1].Texto))
	return b.String()
}

// Estado responde se o Claude Code está aqui e logado. Guarda a resposta por
// um minuto: a tela pergunta a cada abertura e o `claude auth status` demora.
func (t *Terminal) Estado(ctx context.Context) Estado {
	t.mu.Lock()
	if t.estado != nil && time.Since(t.estadoEm) < time.Minute {
		e := *t.estado
		t.mu.Unlock()
		return e
	}
	t.mu.Unlock()
	e := Estado{Implementacao: "terminal", Modelos: map[string]string{}}
	for k, v := range t.Modelos {
		e.Modelos[k] = v
	}
	ctx, cancela := context.WithTimeout(ctx, 20*time.Second)
	defer cancela()
	versao, err := t.roda(ctx, "--version")
	switch {
	case err != nil && errors.Is(err, exec.ErrNotFound):
		e.Motivo = fmt.Sprintf("O Claude Code não foi encontrado nesta máquina (comando %q). Instale o Claude Code e faça o login, ou aponte LOL_IA_CLAUDE para o executável.", t.Executavel)
	case err != nil:
		e.Motivo = "O Claude Code não respondeu ao `claude --version`: " + resumoErro(err)
	default:
		e.Versao = strings.TrimSpace(versao)
		// `claude auth status` devolve JSON; só lemos se está logado e como
		// (o resto tem e-mail e organização, que não saem daqui).
		saida, err := t.roda(ctx, "auth", "status")
		var st struct {
			LoggedIn   *bool  `json:"loggedIn"`
			AuthMethod string `json:"authMethod"`
		}
		if json.Unmarshal([]byte(saida), &st) == nil && st.LoggedIn != nil {
			if *st.LoggedIn {
				e.Disponivel = true
			} else {
				e.Motivo = "O Claude Code está instalado mas não está logado. Abra um terminal, rode `claude` e faça o login (/login)."
			}
		} else if err != nil && strings.Contains(strings.ToLower(saida+err.Error()), "not logged") {
			e.Motivo = "O Claude Code está instalado mas não está logado. Abra um terminal, rode `claude` e faça o login (/login)."
		} else {
			// Versão que não tem o `auth status` em JSON: tenta mesmo assim; o
			// erro de login, se houver, aparece na chamada.
			e.Disponivel = true
		}
	}
	t.mu.Lock()
	t.estado, t.estadoEm = &e, time.Now()
	t.mu.Unlock()
	return e
}

// roda executa um comando curto do Claude Code e devolve a saída padrão.
func (t *Terminal) roda(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, t.Executavel, append(append([]string{}, t.prefixo...), args...)...)
	cmd.Env = ambiente()
	cmd.Dir = os.TempDir()
	var saida, erro bytes.Buffer
	cmd.Stdout, cmd.Stderr = &saida, &erro
	err := cmd.Run()
	if err != nil && erro.Len() > 0 {
		err = fmt.Errorf("%w: %s", err, strings.TrimSpace(erro.String()))
	}
	return saida.String(), err
}

// Conversa faz uma chamada. Com a análise, se o modelo principal não existir
// na conta e nada tiver chegado ainda, tenta uma vez com a reserva.
func (t *Terminal) Conversa(ctx context.Context, p Pedido, aoTrecho func(string)) (Resposta, error) {
	modelo := t.Modelos[p.Tipo]
	if modelo == "" {
		modelo = t.Modelos[TipoConversa]
	}
	reserva := t.Reservas[p.Tipo]
	r, recebeu, err := t.chama(ctx, p, modelo, reserva, aoTrecho)
	if err != nil && !recebeu && reserva != "" && reserva != modelo && ctx.Err() == nil && erroDeModelo(err) {
		r, _, err = t.chama(ctx, p, reserva, "", aoTrecho)
	}
	return r, err
}

func erroDeModelo(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "model") || strings.Contains(s, "modelo")
}

// Eventos do stream-json do Claude Code que interessam.
type eventoTerminal struct {
	Tipo        string            `json:"type"`
	Subtipo     string            `json:"subtype"`
	Ferramentas []string          `json:"tools"`
	MCP         []json.RawMessage `json:"mcp_servers"`
	Modelo      string            `json:"model"`
	Evento      *struct {
		Tipo  string `json:"type"`
		Delta *struct {
			Tipo  string `json:"type"`
			Texto string `json:"text"`
		} `json:"delta"`
	} `json:"event"`
	Mensagem *struct {
		Modelo   string `json:"model"`
		Conteudo []struct {
			Tipo  string `json:"type"`
			Texto string `json:"text"`
		} `json:"content"`
	} `json:"message"`
	Erro      bool    `json:"is_error"`
	Resultado string  `json:"result"`
	CustoUSD  float64 `json:"total_cost_usd"`
	Uso       *struct {
		Entrada     int `json:"input_tokens"`
		CacheCriado int `json:"cache_creation_input_tokens"`
		CacheLido   int `json:"cache_read_input_tokens"`
		Saida       int `json:"output_tokens"`
	} `json:"usage"`
	UsoPorModelo map[string]json.RawMessage `json:"modelUsage"`
}

// chama roda um processo. `recebeu` diz se algum texto chegou (então não dá
// para tentar de novo sem a pessoa ver o texto duas vezes).
func (t *Terminal) chama(ctx context.Context, p Pedido, modelo, reserva string, aoTrecho func(string)) (Resposta, bool, error) {
	inicio := time.Now()
	res := Resposta{Modelo: modelo, CaracteresEntrada: caracteresDoPedido(p)}
	limite := t.Tempos[p.Tipo]
	if limite <= 0 {
		limite = 5 * time.Minute
	}
	ctx, cancela := context.WithTimeout(ctx, limite)
	defer cancela()

	pasta, err := os.MkdirTemp("", "lane-ia-")
	if err != nil {
		return res, false, &erroIA{"não foi possível preparar a chamada à IA (pasta temporária)", err}
	}
	defer os.RemoveAll(pasta)
	arq := filepath.Join(pasta, "sistema.md")
	if err := os.WriteFile(arq, []byte(p.Sistema), 0o600); err != nil {
		return res, false, &erroIA{"não foi possível preparar a chamada à IA (prompt de sistema)", err}
	}

	cmd := exec.CommandContext(ctx, t.Executavel, t.argumentos(modelo, reserva, t.Esforcos[p.Tipo], arq)...)
	cmd.Dir = pasta
	cmd.Env = ambiente()
	cmd.Stdin = strings.NewReader(TextoDaConversa(p.Mensagens))
	cmd.WaitDelay = 5 * time.Second
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	saida, err := cmd.StdoutPipe()
	if err != nil {
		return res, false, &erroIA{"não foi possível chamar o Claude Code", err}
	}
	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return res, false, &erroIA{fmt.Sprintf("O Claude Code não foi encontrado nesta máquina (comando %q).", t.Executavel), errors.Join(ErrIndisponivel, err)}
		}
		return res, false, &erroIA{"não foi possível chamar o Claude Code", err}
	}

	var texto strings.Builder
	recebeu := false
	var final *eventoTerminal
	var recusa error
	leitor := bufio.NewScanner(saida)
	leitor.Buffer(make([]byte, 0, 64*1024), 32*1024*1024)
	for leitor.Scan() {
		linha := bytes.TrimSpace(leitor.Bytes())
		if len(linha) == 0 || linha[0] != '{' {
			continue
		}
		var ev eventoTerminal
		if json.Unmarshal(linha, &ev) != nil {
			continue
		}
		switch ev.Tipo {
		case "system":
			if ev.Subtipo != "init" {
				continue
			}
			if ev.Modelo != "" {
				res.Modelo = ev.Modelo
			}
			// A trava de segurança: se o Claude Code oferecer QUALQUER
			// ferramenta ou servidor MCP à IA, a chamada é encerrada aqui,
			// antes de o pedido ir ao modelo (o init sai antes da chamada).
			if len(ev.Ferramentas) > 0 || len(ev.MCP) > 0 {
				recusa = &erroIA{fmt.Sprintf("o Claude Code ofereceu ferramentas à IA (%s); a chamada foi recusada antes de sair da máquina",
					strings.Join(ev.Ferramentas, ", ")), nil}
				cancela()
			}
		case "stream_event":
			if ev.Evento != nil && ev.Evento.Tipo == "content_block_delta" && ev.Evento.Delta != nil && ev.Evento.Delta.Tipo == "text_delta" {
				texto.WriteString(ev.Evento.Delta.Texto)
				recebeu = true
				if aoTrecho != nil {
					aoTrecho(ev.Evento.Delta.Texto)
				}
			}
		case "assistant":
			// Sem os pedaços (versão que não os manda), o texto vem inteiro aqui.
			if ev.Mensagem != nil {
				if ev.Mensagem.Modelo != "" {
					res.Modelo = ev.Mensagem.Modelo
				}
				if !recebeu {
					for _, c := range ev.Mensagem.Conteudo {
						if c.Tipo == "text" && c.Texto != "" {
							texto.WriteString(c.Texto)
							recebeu = true
							if aoTrecho != nil {
								aoTrecho(c.Texto)
							}
						}
					}
				}
			}
		case "result":
			e := ev
			final = &e
		}
	}
	errEspera := cmd.Wait()
	res.DuracaoMs = time.Since(inicio).Milliseconds()

	if recusa != nil {
		return res, false, recusa
	}
	if ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return res, recebeu, &erroIA{fmt.Sprintf("a IA não terminou em %d min; tente de novo", int(limite.Minutes())), ctx.Err()}
		}
		return res, recebeu, &erroIA{"a chamada à IA foi cancelada", ctx.Err()}
	}
	if final != nil {
		if final.Uso != nil {
			res.TokensEntrada = final.Uso.Entrada + final.Uso.CacheCriado + final.Uso.CacheLido
			res.TokensSaida = final.Uso.Saida
		}
		res.CustoUSD = final.CustoUSD
		for m := range final.UsoPorModelo {
			res.Modelo = m
		}
		if final.Erro || (final.Subtipo != "" && final.Subtipo != "success") {
			return res, recebeu, fraseDoErro(final.Resultado+" "+final.Subtipo, stderr.String())
		}
		if !recebeu && final.Resultado != "" {
			texto.WriteString(final.Resultado)
			if aoTrecho != nil {
				aoTrecho(final.Resultado)
			}
		}
	} else if errEspera != nil || texto.Len() == 0 {
		return res, recebeu, fraseDoErro("", stderr.String()+" "+fmt.Sprint(errEspera))
	}
	res.Texto = texto.String()
	res.CaracteresSaida = len([]rune(res.Texto))
	return res, recebeu, nil
}

// fraseDoErro traduz o erro do Claude Code para a pessoa.
func fraseDoErro(resultado, stderr string) error {
	tudo := strings.TrimSpace(resultado + " " + stderr)
	baixo := strings.ToLower(tudo)
	switch {
	case strings.Contains(baixo, "login") || strings.Contains(baixo, "not logged") || strings.Contains(baixo, "authenticat") ||
		strings.Contains(baixo, "api key") || strings.Contains(baixo, "oauth"):
		return &erroIA{"O Claude Code não está logado (ou o login venceu). Abra um terminal, rode `claude` e faça o login (/login).", errors.New(tudo)}
	case strings.Contains(baixo, "rate limit") || strings.Contains(baixo, "usage limit") || strings.Contains(baixo, "limit reached"):
		return &erroIA{"O limite de uso do Claude Code foi atingido; tente mais tarde.", errors.New(tudo)}
	}
	return &erroIA{"o Claude Code respondeu erro: " + resumoErro(errors.New(tudo)), errors.New(tudo)}
}

func resumoErro(err error) string {
	s := strings.Join(strings.Fields(err.Error()), " ")
	if r := []rune(s); len(r) > 300 {
		s = string(r[:300]) + "…"
	}
	return s
}
