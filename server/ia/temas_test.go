package ia

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"loltracking/server/analise"
)

// O mapa sinal → tema só aponta para temas conhecidos, e todo tipo e
// etiqueta que a base de fatos produz para as situações candidatas puxa algum
// tema (se a análise ganhar etiqueta nova, este teste lembra de mapear).
func TestMapaTemasCobreSinais(t *testing.T) {
	conhecido := map[string]bool{}
	for _, t := range OrdemTemas {
		conhecido[t] = true
	}
	for sinal, ts := range TemasPorSinal {
		if len(ts) == 0 {
			t.Fatalf("sinal %q sem tema", sinal)
		}
		for _, tema := range ts {
			if !conhecido[tema] {
				t.Fatalf("sinal %q aponta para o tema desconhecido %q", sinal, tema)
			}
		}
	}
	for f, tema := range TemaDaFuncao {
		if !conhecido[tema] {
			t.Fatalf("função %s → tema desconhecido %q", f, tema)
		}
	}
	sinais := []string{"abate", "janela_perdida", "janela_perdida:selva", "janela_perdida:rota", "luta", "objetivo", "torre",
		"inibidor", "freeze", "sozinho", "antes_de_objetivo", "inferioridade", "sem_troca", "r_em_recarga", "flash_em_recarga",
		"passiva_em_recarga", "fora_da_rota", "objetivo_depois", "depois_de_morte"}
	for _, a := range []string{analise.AGankando, analise.ACounterGank, analise.AInvadindo, analise.AFarmando, analise.ASplitPush,
		analise.ARoaming, analise.AAgrupado, analise.AObjetivo, analise.ATrocando, analise.ADive} {
		sinais = append(sinais, "previa_"+a)
	}
	for _, s := range sinais {
		if len(TemasPorSinal[s]) == 0 {
			t.Fatalf("o sinal %q não puxa tema nenhum", s)
		}
	}
	for _, f := range []string{"TOP", "JUNGLE", "MIDDLE", "BOTTOM", "UTILITY"} {
		if TemaDaFuncao[f] == "" {
			t.Fatalf("função %s sem tema", f)
		}
	}
}

func TestPontuaTemas(t *testing.T) {
	todos := append([]string{}, OrdemTemas...)
	sits := []analise.MomentoChave{
		{Tipo: "janela_perdida", Subtipo: "selva"},
		{Tipo: "abate", Tags: []string{"sozinho", "antes_de_objetivo", "previa_gankando"}},
		{Tipo: "abate", Tags: []string{"sozinho"}},
	}
	r := PontuaTemas(todos, sits, "JUNGLE")
	if len(r) == 0 || r[0].Tema != "funcao-selva" || !strings.Contains(r[0].Motivos[0], "caçador") {
		t.Fatalf("a função vem primeiro: %+v", r)
	}
	// morte-e-risco: 2 (abate) + 2 (sozinho) + 1 (antes_de_objetivo) + 2 + 2 = 9 → primeiro depois da função.
	if r[1].Tema != "morte-e-risco" || !contem2(r[1].Motivos, "sozinho (S2, S3)") || !contem2(r[1].Motivos, "abate (S2, S3)") {
		t.Fatalf("ordem/motivos: %+v", r)
	}
	ids := map[string]bool{}
	for _, x := range r {
		ids[x.Tema] = true
	}
	for _, tema := range []string{"selva", "visao", "objetivos"} {
		if !ids[tema] {
			t.Fatalf("faltou %s: %+v", tema, r)
		}
	}
	if ids["mentalidade"] || ids["funcao-topo"] {
		t.Fatalf("tema sem sinal entrou: %+v", r)
	}
	// O complemento vem por último.
	if u := r[len(r)-1]; u.Tema != "build-e-adaptacao" || !strings.Contains(u.Motivos[0], "complemento") {
		t.Fatalf("complemento: %+v", r)
	}
	if r := PontuaTemas([]string{"build-e-adaptacao", "mentalidade"}, sits, "JUNGLE"); len(r) != 0 {
		t.Fatalf("só complemento não vale: %+v", r)
	}
	// Só os que existem na pasta.
	if r := PontuaTemas([]string{"objetivos"}, sits, "JUNGLE"); len(r) != 1 || r[0].Tema != "objetivos" {
		t.Fatalf("só os existentes: %+v", r)
	}
}

func escreve(t *testing.T, caminho, texto string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(caminho), 0o755)
	if err := os.WriteFile(caminho, []byte(texto), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLeTemasDaVozTetoECorte(t *testing.T) {
	pasta := t.TempDir()
	grande := strings.Repeat("regra ", 1500) // 9 000 caracteres: cortado em LimiteArquivoTema
	for _, tema := range []string{"morte-e-risco", "visao", "objetivos", "lutas", "funcao-meio"} {
		escreve(t, filepath.Join(pasta, "profissional", tema+".md"), "# "+tema+"\n"+grande)
	}
	escreve(t, filepath.Join(pasta, "profissional", IndiceTemas), "# índice")
	sits := []analise.MomentoChave{{Tipo: "abate", Tags: []string{"sozinho", "antes_de_objetivo"}}, {Tipo: "luta"}}
	esc, fora, texto, ok := LeTemasDaVoz(pasta, Vozes[0], sits, "MIDDLE")
	if !ok || len(esc) == 0 {
		t.Fatal("devia ter temas")
	}
	if n := utf8.RuneCountInString(texto); n > TetoMaterialVoz {
		t.Fatalf("passou do teto: %d", n)
	}
	if esc[0].Tema != "funcao-meio" || esc[0].Arquivo != "profissional/funcao-meio.md" || !strings.Contains(texto, "[… tema cortado pelo app]") {
		t.Fatalf("escolhidos: %+v", esc)
	}
	if len(fora) == 0 || !strings.Contains(fora[0], "não coube no teto") {
		t.Fatalf("fora: %v", fora)
	}
	if strings.Contains(texto, "# índice") || strings.Contains(texto, pasta) {
		t.Fatal("o índice e o caminho do disco não vão para a IA")
	}
	// Sem pasta de temas: não vale (fica o arquivo único).
	if _, _, _, ok := LeTemasDaVoz(t.TempDir(), Vozes[0], sits, "MIDDLE"); ok {
		t.Fatal("sem pasta não há temas")
	}
	// Mono e melhores do elo não têm pasta de temas.
	if PastaDeTemas(Vozes[2]) != "" || PastaDeTemas(Vozes[3]) != "" || PastaDeTemas(Vozes[1]) != "especialista-solo-queue" {
		t.Fatal("pastas de temas")
	}
}

// Com pasta de temas, a análise manda só os temas que casam (com o motivo);
// a prévia e o estado dizem quais; sem tema que case, vale o arquivo único.
func TestRelatorioComTemas(t *testing.T) {
	pasta := t.TempDir()
	escreve(t, filepath.Join(pasta, "profissional", "morte-e-risco.md"), "# Morte e risco\n- Se andar sozinho sem visão, então recue. (Fonte: Vídeo A)")
	escreve(t, filepath.Join(pasta, "profissional", "funcao-selva.md"), "# Caçador\n- Se a rota tem prioridade, então jogue do lado dela. (Fonte: Vídeo B)")
	escreve(t, filepath.Join(pasta, "profissional", "funcao-topo.md"), "# Topo\n- regra do topo")
	escreve(t, filepath.Join(pasta, "profissional", "mentalidade.md"), "# Mentalidade\n- regra de mentalidade")
	escreve(t, filepath.Join(pasta, "profissional.md"), "# Resumo geral do profissional")
	// A voz de solo queue só tem um tema que não casa: vale o arquivo único.
	escreve(t, filepath.Join(pasta, "especialista-solo-queue", "mentalidade.md"), "# Mentalidade solo\n- x")
	escreve(t, filepath.Join(pasta, "especialista-solo-queue.md"), "# Resumo geral da solo queue")
	a := preparaServico(t, nil, pasta)

	_, est := a.pede(t, "GET", "/api/ia/estado", nil)
	mat := est["material"].([]any)
	pro, solo := mat[0].(map[string]any), mat[1].(map[string]any)
	if pro["origem"] != "temas" || pro["temas"].(float64) != 4 || pro["pasta"] != "profissional/" || solo["temas"].(float64) != 1 ||
		mat[2].(map[string]any)["origem"] != "nenhum" || est["teto_material_voz"].(float64) != TetoMaterialVoz {
		t.Fatalf("estado: %v", est)
	}

	c, pv := a.pede(t, "GET", "/api/partidas/3304217746/ia/previa?participante=7", nil)
	if c != 200 {
		t.Fatalf("%d %v", c, pv)
	}
	msg := pv["mensagem"].(string)
	for _, x := range []string{"#### Tema funcao-selva (de profissional/funcao-selva.md; casou com: função do jogador em foco (caçador))",
		"#### Tema morte-e-risco", "sozinho (S2, S3)", "Fonte: Vídeo A", "# Resumo geral da solo queue", "profissional (temas: funcao-selva, morte-e-risco)"} {
		if !strings.Contains(msg+strings.Join(toStrings(pv["enviado"].(map[string]any)["secoes"]), "|"), x) {
			t.Fatalf("faltou %q:\n%s", x, msg)
		}
	}
	for _, x := range []string{"regra do topo", "regra de mentalidade", "Resumo geral do profissional", pasta} {
		if strings.Contains(msg, x) {
			t.Fatalf("não devia ir %q", x)
		}
	}
	vz := pv["vozes"].([]any)[0].(map[string]any)
	if len(vz["temas"].([]any)) != 2 || vz["caracteres"].(float64) <= 0 {
		t.Fatalf("vozes da prévia: %v", vz)
	}
	arqs := toStrings(pv["enviado"].(map[string]any)["arquivos_conhecimento"])
	if strings.Join(arqs, ",") != "profissional/funcao-selva.md,profissional/morte-e-risco.md,especialista-solo-queue.md" {
		t.Fatalf("arquivos: %v", arqs)
	}
}

func toStrings(v any) []string {
	var r []string
	for _, x := range v.([]any) {
		r = append(r, x.(string))
	}
	return r
}
