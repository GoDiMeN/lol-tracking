package ia

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

// AS VOZES do relatório. Cada uma só fala com base em MATERIAL que o app lhe
// dá: um arquivo Markdown escrito à mão na pasta de conhecimento e/ou as
// partidas de referência da coleta por elo que casam com ela. Sem material, o
// app nem pede a voz à IA: escreve ele mesmo "ainda sem dados de referência
// para esta voz" (o prompt também proíbe inventar). Assim uma voz nunca sai
// do conhecimento geral do modelo fingindo ser um especialista.
//
// A pasta de conhecimento (LOL_CONHECIMENTO; padrão <dados>/conhecimento, fora
// do git como o resto de dados/) tem um arquivo por voz:
//
//	profissional.md                 o que um profissional faria
//	especialista-solo-queue.md      o especialista de solo queue
//	mono-<campeão>.md               o mono daquele campeão (mono-vi.md, mono-lee-sin.md)
//	melhores-do-elo.md              o que os melhores do elo fazem (geral)
//	melhores-do-elo-<elo>.md        idem, de um elo (melhores-do-elo-platinum.md)
//	challenger.md                   o que um Challenger faria
//	esmeralda-recem-promovido.md    o Esmeralda que acabou de subir
//
// Texto livre em Markdown; o app corta em LimiteArquivoVoz caracteres.
//
// As vozes de arquivo fixo podem ter, em vez do arquivo único, uma pasta de
// TEMAS com o mesmo nome (profissional/, especialista-solo-queue/...): o app
// escolhe os temas que casam com as situações da partida (temas.go). O
// arquivo único continua valendo quando não há pasta ou nenhum tema casa.

// Voz é uma das vozes do relatório.
type Voz struct {
	ID     string `json:"voz"`
	Rotulo string `json:"rotulo"`
	// Arquivo: o nome do arquivo na pasta (para mono, com o campeão).
	Arquivo string `json:"arquivo"`
	// Referencia: que partidas da coleta por elo servem de material.
	Referencia string `json:"referencia"`
}

// Vozes, na ordem do relatório.
var Vozes = []Voz{
	{"profissional", "O que um profissional faria", "profissional.md", ""},
	{"especialista_soloq", "O que um especialista de solo queue faria", "especialista-solo-queue.md", ""},
	{"mono", "O que o mono champion faria", "mono-<campeão>.md", "partidas da coleta com o mesmo campeão e a mesma função"},
	{"melhores_do_elo", "O que os melhores do elo fazem", "melhores-do-elo.md", "vitórias da coleta no mesmo elo e na mesma função"},
	{"challenger", "O que um Challenger faria", "challenger.md", "partidas da coleta no Challenger, na mesma função"},
	{"esmeralda_recem", "O que o Esmeralda que acabou de subir faz", "esmeralda-recem-promovido.md", "partidas da coleta no Esmeralda, na mesma função"},
	// A síntese do Especialista Lane do patch × elo × rota do jogador em foco (especialista_voz.go).
	{IDVozEspecialista, "O que o Especialista Lane tirou dos jogadores de referência", "especialista-lane/<patch>/<elo>-<rota>.md",
		"relatórios de referência da coleta por elo, juntados por patch, elo e rota"},
}

// SemMaterial é o texto que o app escreve numa voz sem material.
const SemMaterial = "Ainda sem dados de referência para esta voz."

// LimiteArquivoVoz: o teto de cada arquivo de conhecimento (caracteres).
const LimiteArquivoVoz = 6000

// MaterialVoz é o que uma voz recebe numa análise.
type MaterialVoz struct {
	Voz
	// Arquivos lidos (nomes, sem o caminho: o caminho não sai da máquina).
	Arquivos []string `json:"arquivos"`
	Texto    string   `json:"-"`
	// Temas: os temas escolhidos para estas situações (temas.go); vazio
	// quando valeu o arquivo único.
	Temas []TemaEscolhido `json:"temas"`
	// Fora: temas que casaram mas não couberam no teto.
	Fora []string `json:"fora"`
	// Caracteres: o tamanho do material (arquivos/temas + partidas).
	Caracteres int `json:"caracteres"`
	// Partidas de referência que entraram e a amostra em palavras ("1 partida
	// de 1 jogador de Platina II na selva"; vazia sem referência).
	Partidas int    `json:"partidas"`
	Amostra  string `json:"amostra"`
	Falta    string `json:"falta"` // por que não tem (quando não tem)
}

// TemMaterial: a voz tem algo em que se apoiar.
func (m MaterialVoz) TemMaterial() bool { return strings.TrimSpace(m.Texto) != "" }

// semAcento: o bastante para nomes de campeão e de elo (sem depender de
// golang.org/x/text só para isto).
var semAcento = map[rune]rune{'á': 'a', 'à': 'a', 'â': 'a', 'ã': 'a', 'ä': 'a', 'é': 'e', 'ê': 'e', 'è': 'e',
	'ë': 'e', 'í': 'i', 'î': 'i', 'ï': 'i', 'ó': 'o', 'ô': 'o', 'õ': 'o', 'ö': 'o', 'ú': 'u', 'ü': 'u', 'ç': 'c'}

// slug: "Lee Sin" → "lee-sin", "Kai'Sa" → "kaisa", "Nunu & Willump" → "nunu-willump".
func slug(s string) string {
	var b strings.Builder
	traco := false
	for _, r := range strings.ToLower(s) {
		if sem, ok := semAcento[r]; ok {
			r = sem
		}
		switch {
		case r == '\'', r == '.':
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			traco = false
		default:
			if b.Len() > 0 && !traco {
				b.WriteByte('-')
				traco = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// ArquivosDaVoz: os arquivos que valem para a voz, na ordem (o mais
// específico primeiro).
func ArquivosDaVoz(v Voz, campeao, elo string) []string {
	switch v.ID {
	case "mono":
		if campeao == "" {
			return nil
		}
		return []string{"mono-" + slug(campeao) + ".md"}
	case "melhores_do_elo":
		var r []string
		if elo != "" {
			r = append(r, "melhores-do-elo-"+slug(elo)+".md")
		}
		return append(r, "melhores-do-elo.md")
	}
	return []string{v.Arquivo}
}

// LeArquivosDaVoz lê os arquivos da voz que existem na pasta.
func LeArquivosDaVoz(pasta string, v Voz, campeao, elo string) (nomes []string, texto string) {
	if pasta == "" {
		return nil, ""
	}
	var b strings.Builder
	for _, nome := range ArquivosDaVoz(v, campeao, elo) {
		dados, err := os.ReadFile(filepath.Join(pasta, nome))
		if err != nil || len(strings.TrimSpace(string(dados))) == 0 {
			continue
		}
		s := strings.TrimSpace(strings.ReplaceAll(string(dados), "\r\n", "\n"))
		if utf8.RuneCountInString(s) > LimiteArquivoVoz {
			s = string([]rune(s)[:LimiteArquivoVoz]) + "\n[… arquivo cortado pelo app]"
		}
		nomes = append(nomes, nome)
		fmt.Fprintf(&b, "### Do arquivo %s\n%s\n\n", nome, s)
	}
	return nomes, strings.TrimSpace(b.String())
}
