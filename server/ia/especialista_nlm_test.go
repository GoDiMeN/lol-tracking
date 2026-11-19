package ia

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// O `nlm` falso é o próprio binário do teste: com LANE_NLM_FALSO=1 ele não
// roda os testes, faz o papel da ferramenta no modo do primeiro argumento.
func TestMain(m *testing.M) {
	if os.Getenv("LANE_NLM_FALSO") == "1" {
		os.Exit(nlmFalso(os.Args[1:]))
	}
	os.Exit(m.Run())
}

// nlmFalso: "novo" (0.16.5, funciona), "velho" (0.9.6: o --version responde e
// o source add devolve um JSON de erro, como o da máquina do dono), "quebrado"
// (traceback em tudo) e "login" (0.16.5 com o login vencido).
func nlmFalso(args []string) int {
	if len(args) == 0 {
		return 2
	}
	modo, resto := args[0], args[1:]
	if os.Getenv("PYTHONIOENCODING") != "utf-8" {
		fmt.Fprintln(os.Stderr, "UnicodeEncodeError: 'charmap' codec can't encode character '\\xb7'")
		return 3
	}
	versao := len(resto) > 0 && resto[0] == "--version"
	switch modo {
	case "novo", "login":
		if versao {
			fmt.Println("nlm version 0.16.5")
			return 0
		}
		if modo == "login" {
			fmt.Fprintln(os.Stderr, "Error: Authentication expired. Run 'nlm login' to re-authenticate.")
			return 1
		}
		if len(resto) >= 2 && resto[0] == "source" && resto[1] == "add" {
			fmt.Println(`{"source_id": "11111111-2222-3333-4444-555555555555", "title": "x"}`)
			return 0
		}
		return 0
	case "velho":
		if versao {
			fmt.Println("nlm version 0.9.6")
			return 0
		}
		fmt.Println("{\n  \"status\": \"error\",\n  \"error\": \"Unexpected response format from NotebookLM\"\n}")
		return 1
	case "quebrado":
		fmt.Fprintln(os.Stderr, "Traceback (most recent call last):\n  File \"nlm\", line 1, in <module>\nModuleNotFoundError: No module named 'notebooklm_tools'")
		return 1
	}
	return 2
}

// falso: o comando do `nlm` falso no modo pedido, com o caminho entre aspas
// (exercita a divisão do LOL_NLM).
func falso(t *testing.T, modo string) []string {
	t.Helper()
	t.Setenv("LANE_NLM_FALSO", "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return []string{exe, modo}
}

func nlmDe(t *testing.T, conf []string, reserva []string) *NLM {
	t.Helper()
	n := NovoNLM(`"` + conf[0] + `" ` + strings.Join(conf[1:], " "))
	n.Reserva = reserva
	return n
}

func TestDivideComando(t *testing.T) {
	casos := []struct {
		in   string
		quer []string
	}{
		{"nlm", []string{"nlm"}},
		{"  uvx --from notebooklm-mcp-cli   nlm ", []string{"uvx", "--from", "notebooklm-mcp-cli", "nlm"}},
		{`"C:\Program Files\nlm\nlm.exe" --x`, []string{`C:\Program Files\nlm\nlm.exe`, "--x"}},
		{`'C:\a b\nlm' "dois  espaços" ""`, []string{`C:\a b\nlm`, "dois  espaços", ""}},
		{`C:\Users\luizg\.local\bin\nlm.exe`, []string{`C:\Users\luizg\.local\bin\nlm.exe`}},
	}
	for _, c := range casos {
		got, err := DivideComando(c.in)
		if err != nil || fmt.Sprintf("%q", got) != fmt.Sprintf("%q", c.quer) {
			t.Fatalf("%q: %q %v", c.in, got, err)
		}
	}
	for _, ruim := range []string{"", "   ", `"sem fechar`} {
		if _, err := DivideComando(ruim); err == nil {
			t.Fatalf("%q devia falhar", ruim)
		}
	}
	if s := TextoComando([]string{`C:\a b\nlm`, "x"}); s != `"C:\a b\nlm" x` {
		t.Fatal(s)
	}
	if comparaVersao("0.9.6", VersaoMinimaNLM) >= 0 || comparaVersao("0.16.5", VersaoMinimaNLM) <= 0 || comparaVersao("1.0", "1.0.0") != 0 {
		t.Fatal("comparação de versão")
	}
}

func TestNLMConfiguradoNovo(t *testing.T) {
	n := nlmDe(t, falso(t, "novo"), nil)
	info := n.Detecta(context.Background())
	if ok, motivo := n.Disponivel(); !ok || info.Origem != "configurado" || info.Versao != "0.16.5" || info.Aviso != "" ||
		!strings.HasSuffix(info.Comando, " novo") {
		t.Fatalf("%v %q %+v", ok, motivo, info)
	}
	// Sai com PYTHONIOENCODING=utf-8 mesmo com outro valor no ambiente.
	t.Setenv("PYTHONIOENCODING", "cp1252")
	id, err := n.Adiciona(context.Background(), "5d1c235c-174b-4123-a1c0-d23e50093600", "16.23 · Platina · selva", "texto")
	if err != nil || id != "11111111-2222-3333-4444-555555555555" {
		t.Fatal(id, err)
	}
	// Texto grande vai por arquivo.
	if _, err := n.Adiciona(context.Background(), "5d1c235c-174b-4123-a1c0-d23e50093600", "t", strings.Repeat("a", LimiteTextoLinha+1)); err != nil {
		t.Fatal(err)
	}
}

// O `nlm` do PATH velho (0.9.6) cai na reserva (o uvx), e o estado diz por quê.
func TestNLMVelhoCaiNaReserva(t *testing.T) {
	n := nlmDe(t, falso(t, "velho"), falso(t, "novo"))
	info := n.Detecta(context.Background())
	if info.Origem != "uvx" || info.Versao != "0.16.5" || !strings.Contains(info.Aviso, "0.9.6") || !strings.Contains(info.Aviso, VersaoMinimaNLM) {
		t.Fatalf("%+v", info)
	}
}

// Sem reserva que funcione: o velho fica, com o aviso; o erro do item traz a
// mensagem inteira (não o "{" da primeira linha).
func TestNLMVelhoSemReservaEOErroInteiro(t *testing.T) {
	n := nlmDe(t, falso(t, "velho"), []string{filepath.Join(t.TempDir(), "nao-existe.exe")})
	info := n.Detecta(context.Background())
	if ok, _ := n.Disponivel(); !ok || info.Versao != "0.9.6" || !strings.Contains(info.Aviso, "abaixo da mínima") || !strings.Contains(info.Aviso, "nao-existe") {
		t.Fatalf("%v %+v", ok, info)
	}
	_, err := n.Adiciona(context.Background(), "5d1c235c-174b-4123-a1c0-d23e50093600", "t", "x")
	if err == nil || !strings.Contains(err.Error(), "Unexpected response format from NotebookLM") || strings.Contains(err.Error(), "\n") ||
		strings.HasSuffix(err.Error(), "{") || !strings.Contains(err.Error(), "saída 1") {
		t.Fatalf("%v", err)
	}
}

func TestNLMNenhumServe(t *testing.T) {
	n := nlmDe(t, falso(t, "quebrado"), []string{filepath.Join(t.TempDir(), "nao-existe.exe")})
	n.Detecta(context.Background())
	ok, motivo := n.Disponivel()
	if ok || !strings.Contains(motivo, "No module named 'notebooklm_tools'") || !strings.Contains(motivo, "uv tool install") {
		t.Fatalf("%v %q", ok, motivo)
	}
	if _, err := n.Adiciona(context.Background(), "x", "t", "x"); err == nil {
		t.Fatal("sem ferramenta não devia rodar")
	}
}

func TestNLMLoginVencido(t *testing.T) {
	n := nlmDe(t, falso(t, "login"), nil)
	n.Detecta(context.Background())
	_, err := n.Adiciona(context.Background(), "5d1c235c-174b-4123-a1c0-d23e50093600", "t", "x")
	if err == nil || !strings.HasPrefix(err.Error(), "o login do NotebookLM venceu: rode `nlm login`") || !strings.Contains(err.Error(), "Authentication expired") {
		t.Fatalf("%v", err)
	}
}

func TestNLMLOLNLMInvalida(t *testing.T) {
	n := NovoNLM(`"sem fechar`)
	n.Reserva = []string{filepath.Join(t.TempDir(), "nao-existe.exe")}
	n.Detecta(context.Background())
	if ok, motivo := n.Disponivel(); ok || !strings.Contains(motivo, "LOL_NLM inválida") {
		t.Fatalf("%v %q", ok, motivo)
	}
}

// O estado do espelho mostra qual ferramenta vale.
func TestEspelhoMostraAFerramenta(t *testing.T) {
	a, _, _ := preparaEsp(t, fontesEsp{})
	n := nlmDe(t, falso(t, "velho"), falso(t, "novo"))
	n.Detecta(context.Background())
	a.s.Caderno = n
	a.s.CadernoPadrao = "5d1c235c-174b-4123-a1c0-d23e50093600"
	_, m := a.pede(t, "GET", "/api/especialista/espelho", nil)
	e := m["espelho"].(map[string]any)
	if e["ferramenta"] != true || e["versao"] != "0.16.5" || e["origem"] != "uvx" || !strings.HasSuffix(e["comando"].(string), " novo") ||
		!strings.Contains(e["motivo"].(string), "0.9.6") {
		t.Fatalf("%v", e)
	}
}
