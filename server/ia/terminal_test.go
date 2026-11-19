package ia

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// O binário de teste faz o papel do `claude`: com LANE_AJUDANTE definido,
// TestAjudanteClaude imprime o stream-json de cada modo e sai. Nada toca no
// Claude Code de verdade.
func TestAjudanteClaude(t *testing.T) {
	modo := os.Getenv("LANE_AJUDANTE")
	if modo == "" {
		t.Skip("só como processo filho")
	}
	args := os.Args
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	out := bufio.NewWriter(os.Stdout)
	defer func() { out.Flush(); os.Exit(0) }()
	linha := func(v any) { b, _ := json.Marshal(v); out.Write(append(b, '\n')); out.Flush() }
	if len(args) > 0 && args[0] == "--version" {
		fmt.Fprintln(out, "9.9.9 (Claude Code)")
		return
	}
	if len(args) > 1 && args[0] == "auth" {
		fmt.Fprintf(out, `{"loggedIn": %v, "authMethod": "claude.ai", "email": "x@y"}`, modo != "deslogado")
		return
	}
	entrada, _ := io.ReadAll(os.Stdin)
	sistema := ""
	for i, a := range args {
		if a == "--system-prompt-file" && i+1 < len(args) {
			b, _ := os.ReadFile(args[i+1])
			sistema = string(b)
		}
	}
	ferr := []string{}
	if modo == "ferramentas" {
		ferr = []string{"Bash", "Read"}
	}
	linha(map[string]any{"type": "system", "subtype": "init", "tools": ferr, "mcp_servers": []any{}, "model": "claude-teste"})
	switch modo {
	case "login":
		linha(map[string]any{"type": "result", "subtype": "success", "is_error": true, "result": "Not logged in · Please run /login"})
		return
	case "devagar":
		time.Sleep(10 * time.Second)
	case "ferramentas":
		time.Sleep(2 * time.Second)
	}
	resposta := fmt.Sprintf("recebi %d de sistema e %d de entrada; args=%s", len([]rune(sistema)), len([]rune(string(entrada))), strings.Join(args, " "))
	for _, pedaco := range []string{resposta[:10], resposta[10:]} {
		linha(map[string]any{"type": "stream_event", "event": map[string]any{"type": "content_block_delta", "delta": map[string]any{"type": "text_delta", "text": pedaco}}})
	}
	linha(map[string]any{"type": "assistant", "message": map[string]any{"model": "claude-teste", "content": []any{map[string]any{"type": "text", "text": resposta}}}})
	linha(map[string]any{"type": "result", "subtype": "success", "is_error": false, "result": resposta, "total_cost_usd": 0.01,
		"usage": map[string]any{"input_tokens": 10, "cache_read_input_tokens": 5, "output_tokens": 7}, "modelUsage": map[string]any{"claude-teste-1": map[string]any{}}})
}

func terminalDeTeste(t *testing.T, modo string) *Terminal {
	t.Setenv("LANE_AJUDANTE", modo)
	tm := NovoTerminal(os.Args[0], "", "")
	tm.prefixo = []string{"-test.run=^TestAjudanteClaude$", "--"}
	return tm
}

func TestTerminalChamadaCompleta(t *testing.T) {
	tm := terminalDeTeste(t, "ok")
	var pedacos []string
	p := Pedido{Tipo: TipoAnalise, Sistema: "sistema é aqui", Mensagens: []Mensagem{{Papel: PapelUsuario, Texto: "olá"}}}
	r, err := tm.Conversa(context.Background(), p, func(s string) { pedacos = append(pedacos, s) })
	if err != nil {
		t.Fatal(err)
	}
	if len(pedacos) != 2 || strings.Join(pedacos, "") != r.Texto {
		t.Fatalf("fluxo: %q vs %q", pedacos, r.Texto)
	}
	if !strings.Contains(r.Texto, "recebi 14 de sistema e 3 de entrada") {
		t.Fatalf("o sistema vai por arquivo e a conversa pela entrada: %s", r.Texto)
	}
	// As opções de segurança estão na linha de comando.
	for _, o := range []string{"-p", "--tools  --strict-mcp-config", "--safe-mode", "--disable-slash-commands", "--no-session-persistence",
		"--permission-mode dontAsk", "--max-turns 1", "--model fable", "--fallback-model opus", "--effort high", "--output-format stream-json"} {
		if !strings.Contains(r.Texto, o) {
			t.Fatalf("faltou %q: %s", o, r.Texto)
		}
	}
	if r.Modelo != "claude-teste-1" || r.TokensEntrada != 15 || r.TokensSaida != 7 || r.CaracteresEntrada != 17 {
		t.Fatalf("uso: %+v", r)
	}
	if e := tm.Estado(context.Background()); !e.Disponivel || e.Versao != "9.9.9 (Claude Code)" {
		t.Fatalf("estado: %+v", e)
	}
}

func TestTerminalRecusaFerramentas(t *testing.T) {
	tm := terminalDeTeste(t, "ferramentas")
	inicio := time.Now()
	_, err := tm.Conversa(context.Background(), Pedido{Tipo: TipoConversa, Mensagens: []Mensagem{{Texto: "x"}}}, nil)
	if err == nil || !strings.Contains(err.Error(), "ofereceu ferramentas") || !strings.Contains(err.Error(), "Bash") {
		t.Fatalf("%v", err)
	}
	if time.Since(inicio) > 1500*time.Millisecond {
		t.Fatal("a recusa encerra o processo no init, sem esperar a resposta")
	}
}

func TestTerminalErros(t *testing.T) {
	tm := terminalDeTeste(t, "login")
	if _, err := tm.Conversa(context.Background(), Pedido{Tipo: TipoConversa, Mensagens: []Mensagem{{Texto: "x"}}}, nil); err == nil || !strings.Contains(err.Error(), "não está logado") {
		t.Fatalf("login: %v", err)
	}
	tm = terminalDeTeste(t, "devagar")
	tm.Tempos[TipoConversa] = 300 * time.Millisecond
	if _, err := tm.Conversa(context.Background(), Pedido{Tipo: TipoConversa, Mensagens: []Mensagem{{Texto: "x"}}}, nil); err == nil || !strings.Contains(err.Error(), "não terminou") {
		t.Fatalf("tempo limite: %v", err)
	}
	tm = terminalDeTeste(t, "devagar")
	ctx, cancela := context.WithCancel(context.Background())
	go func() { time.Sleep(200 * time.Millisecond); cancela() }()
	if _, err := tm.Conversa(ctx, Pedido{Tipo: TipoConversa, Mensagens: []Mensagem{{Texto: "x"}}}, nil); err == nil || !strings.Contains(err.Error(), "cancelada") {
		t.Fatalf("cancelar: %v", err)
	}
	tm = terminalDeTeste(t, "deslogado")
	if e := tm.Estado(context.Background()); e.Disponivel || !strings.Contains(e.Motivo, "não está logado") {
		t.Fatalf("deslogado: %+v", e)
	}
	tm = NovoTerminal("claude-que-nao-existe-lane", "", "")
	if e := tm.Estado(context.Background()); e.Disponivel || !strings.Contains(e.Motivo, "não foi encontrado") {
		t.Fatalf("ausente: %+v", e)
	}
}

func TestTextoDaConversa(t *testing.T) {
	if s := TextoDaConversa([]Mensagem{{Papel: PapelUsuario, Texto: "só"}}); s != "só" {
		t.Fatal(s)
	}
	s := TextoDaConversa([]Mensagem{{PapelUsuario, "a"}, {PapelAssistente, "b"}, {PapelUsuario, "c"}})
	if !strings.Contains(s, "### USUÁRIO\na") || !strings.Contains(s, "### VOCÊ (resposta anterior)\nb") || !strings.HasSuffix(s, "(responda a esta)\nc") {
		t.Fatal(s)
	}
}
