package ia

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// A FERRAMENTA `nlm` (notebooklm-mcp-cli) do espelho no NotebookLM.
//
// Medido nesta máquina (2026-11-19): o `nlm` do PATH, 0.9.6 (instalado por
// pipx), responde ao --version mas o `source add` dele falha devolvendo um
// JSON de erro (a tela via só "o `nlm` falhou: {", a primeira linha); o
// `uvx --from notebooklm-mcp-cli nlm`, 0.16.5, funciona. Por isso:
//
//   - LOL_NLM aceita um COMANDO com argumentos (`uvx --from notebooklm-mcp-cli
//     nlm`), dividido aqui com aspas (DivideComando) e chamado sem shell;
//   - na subida, Detecta testa o configurado (ou o `nlm` do PATH) com
//     `--version` (barato, sem rede) e, se falhar ou for mais velho que
//     VersaoMinimaNLM, tenta o uvx; o que valeu aparece no estado do espelho;
//   - o erro guardado por item traz stderr + stdout resumidos numa linha
//     (resumoSaida), e o de login vira frase clara;
//   - o processo filho sempre roda com PYTHONIOENCODING=utf-8 (o Windows
//     usaria a página de código do console e os títulos com "·" quebrariam).

// VersaoMinimaNLM: a menor versão conferida funcionando com o `source add
// --title --text --json` e o `source delete --confirm` que o app usa (0.16.5
// funciona; 0.9.6 não). Abaixo dela o app procura o uvx.
const VersaoMinimaNLM = "0.16.0"

// ComandoUVX: a alternativa (o uv baixa e guarda a versão nova na primeira vez).
var ComandoUVX = []string{"uvx", "--from", "notebooklm-mcp-cli", "nlm"}

// InfoFerramenta: qual `nlm` o espelho usa. Origem: "configurado" (LOL_NLM),
// "path" (o `nlm` do PATH) ou "uvx". Aviso: dá para usar, mas com ressalva
// (versão abaixo da mínima sem alternativa que funcione, ou a explicação de
// por que o primeiro candidato não serviu).
type InfoFerramenta struct {
	Comando string `json:"comando"`
	Versao  string `json:"versao"`
	Origem  string `json:"origem"`
	Aviso   string `json:"aviso"`
}

// NLM chama o `nlm` da máquina.
type NLM struct {
	// Configurado: o LOL_NLM como veio ("" = o `nlm` do PATH).
	Configurado string
	// Reserva: a alternativa quando o primeiro não serve (ComandoUVX; os testes trocam).
	Reserva []string
	// Tempo por chamada; TempoVersao para o --version (o uvx baixa na primeira vez).
	Tempo       time.Duration
	TempoVersao time.Duration

	inicio sync.Once
	pronto chan struct{}
	mu     sync.Mutex
	argv   []string
	info   InfoFerramenta
	motivo string // por que nenhum serve ("" = argv vale)
}

// NovoNLM: configurado "" = "nlm" no PATH. A detecção começa com Inicia (a
// subida) ou na primeira pergunta de Disponivel.
func NovoNLM(configurado string) *NLM {
	r := append([]string(nil), ComandoUVX...)
	return &NLM{Configurado: strings.TrimSpace(configurado), Reserva: r, Tempo: 3 * time.Minute, TempoVersao: 2 * time.Minute,
		pronto: make(chan struct{})}
}

// Inicia a detecção em segundo plano (uma vez): a subida não espera o uvx.
func (n *NLM) Inicia() {
	n.inicio.Do(func() {
		go func() {
			n.detecta(context.Background())
		}()
	})
}

// Detecta roda a detecção agora e espera (os testes; a subida usa Inicia).
func (n *NLM) Detecta(ctx context.Context) InfoFerramenta {
	n.inicio.Do(func() { n.detecta(ctx) })
	<-n.pronto
	return n.Ferramenta()
}

type candidatoNLM struct {
	argv   []string
	origem string
}

func (n *NLM) detecta(ctx context.Context) {
	defer close(n.pronto)
	var cands []candidatoNLM
	var falhas []string
	if n.Configurado != "" {
		argv, err := DivideComando(n.Configurado)
		if err != nil {
			falhas = append(falhas, "LOL_NLM inválida: "+err.Error())
		} else {
			cands = append(cands, candidatoNLM{argv, "configurado"})
		}
	} else {
		cands = append(cands, candidatoNLM{[]string{"nlm"}, "path"})
	}
	if len(n.Reserva) > 0 && (len(cands) == 0 || TextoComando(cands[0].argv) != TextoComando(n.Reserva)) {
		cands = append(cands, candidatoNLM{n.Reserva, "uvx"})
	}
	var velho *candidatoNLM
	velhoVer := ""
	for i, c := range cands {
		ver, err := n.versao(ctx, c.argv)
		switch {
		case err != nil:
			falhas = append(falhas, fmt.Sprintf("`%s --version` falhou: %v", TextoComando(c.argv), err))
		case comparaVersao(ver, VersaoMinimaNLM) < 0:
			falhas = append(falhas, fmt.Sprintf("`%s` é a versão %s (a mínima é %s)", TextoComando(c.argv), ver, VersaoMinimaNLM))
			if velho == nil {
				velho, velhoVer = &cands[i], ver
			}
		default:
			n.escolhe(c, ver, strings.Join(falhas, "; "))
			return
		}
	}
	if velho != nil {
		n.escolhe(*velho, velhoVer, "usando uma versão abaixo da mínima, pode falhar: "+strings.Join(falhas, "; ")+
			". Atualize com `uv tool install --force notebooklm-mcp-cli` ou instale o uv para o app usar o `uvx`")
		return
	}
	n.mu.Lock()
	n.motivo = "a ferramenta `nlm` (notebooklm-mcp-cli) não funcionou nesta máquina: " + strings.Join(falhas, "; ") +
		". Instale com `uv tool install notebooklm-mcp-cli` (ou instale o uv para o app usar o `uvx`) e reinicie o app"
	n.mu.Unlock()
	log.Printf("especialista: %s", n.motivo)
}

func (n *NLM) escolhe(c candidatoNLM, ver, aviso string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.argv = c.argv
	n.info = InfoFerramenta{Comando: TextoComando(c.argv), Versao: ver, Origem: c.origem}
	if aviso != "" && c.origem == "uvx" {
		aviso = "usando o `uvx`: " + aviso
	}
	n.info.Aviso = aviso
	log.Printf("especialista: espelho no NotebookLM pelo `%s` (versão %s, %s)", n.info.Comando, ver, c.origem)
}

var reVersao = regexp.MustCompile(`(\d+)\.(\d+)(?:\.(\d+))?`)

// versao roda `<argv> --version` e lê "nlm version X.Y.Z".
func (n *NLM) versao(ctx context.Context, argv []string) (string, error) {
	tempo := n.TempoVersao
	if tempo <= 0 {
		tempo = 2 * time.Minute
	}
	saida, erro, err := n.executa(ctx, tempo, argv, "--version")
	if err != nil {
		return "", errors.New(resumoSaida(saida, erro, err))
	}
	m := reVersao.FindString(saida + "\n" + erro)
	if m == "" {
		return "", fmt.Errorf("não disse a versão: %s", resumoSaida(saida, erro, nil))
	}
	return m, nil
}

// executa roda argv + args sem shell, com PYTHONIOENCODING=utf-8.
func (n *NLM) executa(ctx context.Context, tempo time.Duration, argv []string, args ...string) (string, string, error) {
	c, cancela := context.WithTimeout(ctx, tempo)
	defer cancela()
	cmd := exec.CommandContext(c, argv[0], append(append([]string(nil), argv[1:]...), args...)...)
	// Por último: vence um PYTHONIOENCODING diferente herdado do ambiente.
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8", "PYTHONUTF8=1")
	var saida, erro bytes.Buffer
	cmd.Stdout, cmd.Stderr = &saida, &erro
	err := cmd.Run()
	if err != nil && errors.Is(c.Err(), context.DeadlineExceeded) {
		err = fmt.Errorf("não respondeu em %s", tempo)
	}
	return saida.String(), erro.String(), err
}

// Ferramenta: o que a detecção escolheu (vazio enquanto procura ou se nenhum serve).
func (n *NLM) Ferramenta() InfoFerramenta {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.info
}

func (n *NLM) Disponivel() (bool, string) {
	n.Inicia()
	select {
	case <-n.pronto:
	default:
		return false, "procurando a ferramenta `nlm` (notebooklm-mcp-cli): confere o `nlm` e o `uvx`; tente de novo em alguns segundos"
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.argv == nil {
		return false, n.motivo
	}
	return true, ""
}

func (n *NLM) roda(ctx context.Context, args ...string) (string, error) {
	if ok, motivo := n.Disponivel(); !ok {
		return "", &erroIA{motivo, errors.New("nlm indisponível")}
	}
	n.mu.Lock()
	argv, origem := n.argv, n.info.Origem
	n.mu.Unlock()
	saida, erro, err := n.executa(ctx, n.Tempo, argv, args...)
	if err != nil {
		return saida + "\n" + erro, fraseNLM(saida, erro, err, argv, origem)
	}
	return saida + "\n" + erro, nil
}

var reLoginNLM = regexp.MustCompile(`(?i)authenticat|unauthori[sz]ed|\blogin\b|credentials|cookies? (have |has )?expired|session expired|token expired|\b401\b`)

// fraseNLM: o erro guardado no item. Login vencido vira a frase do que fazer;
// o resto leva a saída inteira resumida numa linha (não só a primeira).
func fraseNLM(saida, erro string, err error, argv []string, origem string) error {
	resumo := resumoSaida(saida, erro, err)
	tudo := saida + "\n" + erro
	switch {
	case reLoginNLM.MatchString(tudo):
		frase := "o login do NotebookLM venceu: rode `nlm login`"
		if origem == "uvx" || (len(argv) > 0 && argv[0] != "nlm") {
			frase += " (o app usa `" + TextoComando(argv) + "`: rode `" + TextoComando(argv) + " login`)"
		}
		return &erroIA{frase + " no terminal desta máquina e envie de novo (" + corta(resumo, 200) + ")", err}
	case strings.Contains(strings.ToLower(tudo), "not found") && strings.Contains(strings.ToLower(tudo), "notebook"):
		return &erroIA{"o caderno do NotebookLM não foi encontrado: confira o id (" + corta(resumo, 200) + ")", err}
	}
	return &erroIA{"o `nlm` falhou: " + resumo, err}
}

// resumoSaida junta stderr e stdout numa linha: a mensagem de um JSON de erro
// e a última linha de um traceback do Python vêm primeiro (é onde está a
// causa); molduras do terminal (rich) saem; teto de 600 caracteres.
func resumoSaida(saida, erro string, err error) string {
	var partes []string
	vistos := map[string]bool{}
	poe := func(s string) {
		s = strings.Join(strings.Fields(s), " ")
		if s != "" && !vistos[s] {
			vistos[s] = true
			partes = append(partes, s)
		}
	}
	if msg := mensagemJSON(saida); msg != "" {
		poe(msg)
	}
	linhasErro := linhasLimpas(erro)
	if strings.Contains(erro, "Traceback") && len(linhasErro) > 0 {
		poe(linhasErro[len(linhasErro)-1])
	}
	for _, l := range linhasErro {
		poe(l)
	}
	for _, l := range linhasLimpas(saida) {
		poe(l)
	}
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			poe(fmt.Sprintf("(saída %d)", ee.ExitCode()))
		} else {
			poe(err.Error())
		}
	}
	if len(partes) == 0 {
		return "sem mensagem"
	}
	return corta(strings.Join(partes, " · "), 600)
}

// mensagemJSON: {"error": "..."} / {"message": ...} / {"detail": ...} na saída.
func mensagemJSON(s string) string {
	s = strings.TrimSpace(s)
	i := strings.IndexByte(s, '{')
	if i < 0 {
		return ""
	}
	var m map[string]any
	if json.Unmarshal([]byte(s[i:]), &m) != nil {
		return ""
	}
	var partes []string
	for _, k := range []string{"error", "erro", "message", "detail", "hint"} {
		switch v := m[k].(type) {
		case string:
			if v != "" {
				partes = append(partes, v)
			}
		case map[string]any:
			if b, err := json.Marshal(v); err == nil {
				partes = append(partes, string(b))
			}
		}
	}
	return strings.Join(partes, " · ")
}

// linhasLimpas: as linhas sem a moldura do terminal e sem as vazias.
func linhasLimpas(s string) []string {
	var l []string
	for _, x := range strings.Split(s, "\n") {
		x = strings.TrimFunc(x, func(r rune) bool {
			return unicode.IsSpace(r) || (r >= 0x2500 && r <= 0x257F) // desenho de caixa
		})
		if x != "" && strings.Trim(x, "{}[],") != "" {
			l = append(l, x)
		}
	}
	return l
}

func corta(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

func (n *NLM) Adiciona(ctx context.Context, caderno, titulo, texto string) (string, error) {
	args := []string{"source", "add", caderno, "--title", titulo, "--json"}
	if utf8.RuneCountInString(texto) <= LimiteTextoLinha {
		args = append(args, "--text", texto)
	} else {
		dir, err := os.MkdirTemp("", "lane-nlm-")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(dir)
		arq := filepath.Join(dir, slug(titulo)+".md")
		if err := os.WriteFile(arq, []byte(texto), 0o600); err != nil {
			return "", err
		}
		args = append(args, "--file", arq)
	}
	saida, err := n.roda(ctx, args...)
	if err != nil {
		return "", err
	}
	for _, id := range reUUID.FindAllString(saida, -1) {
		if !strings.EqualFold(id, caderno) {
			return strings.ToLower(id), nil
		}
	}
	return "", &erroIA{"o `nlm` não devolveu o id da fonte: " + resumoSaida(saida, "", nil), errors.New(strings.TrimSpace(saida))}
}

func (n *NLM) Remove(ctx context.Context, caderno, fonteID string) error {
	_, err := n.roda(ctx, "source", "delete", fonteID, "--confirm")
	return err
}

// DivideComando divide um comando em argumentos como um terminal faria, sem
// shell: espaço separa; aspas duplas ou simples agrupam (e somem); a barra
// invertida é literal (caminho do Windows: `"C:\Program Files\x\nlm.exe"`).
func DivideComando(s string) ([]string, error) {
	var args []string
	var cur strings.Builder
	tem := false
	var aspas rune
	for _, r := range s {
		switch {
		case aspas != 0:
			if r == aspas {
				aspas = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			aspas, tem = r, true
		case unicode.IsSpace(r):
			if tem {
				args = append(args, cur.String())
				cur.Reset()
				tem = false
			}
		default:
			cur.WriteRune(r)
			tem = true
		}
	}
	if aspas != 0 {
		return nil, fmt.Errorf("aspas sem fechar em %q", s)
	}
	if tem {
		args = append(args, cur.String())
	}
	if len(args) == 0 {
		return nil, errors.New("comando vazio")
	}
	return args, nil
}

// TextoComando: o comando para mostrar (argumento com espaço vai entre aspas).
func TextoComando(argv []string) string {
	partes := make([]string, len(argv))
	for i, a := range argv {
		if a == "" || strings.ContainsAny(a, " \t") {
			a = `"` + a + `"`
		}
		partes[i] = a
	}
	return strings.Join(partes, " ")
}

// comparaVersao: -1, 0 ou 1 (X.Y.Z; parte que falta = 0).
func comparaVersao(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}
