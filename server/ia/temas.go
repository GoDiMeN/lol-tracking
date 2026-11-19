package ia

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"loltracking/server/analise"
)

// MATERIAL POR TEMA. Um arquivo único por voz (6 mil caracteres) é pouco para
// o que os cadernos das vozes cobrem (dezenas de vídeos). Então a voz pode ter
// uma PASTA com um Markdown por tema, e o servidor escolhe, a cada pedido, só
// os temas que casam com as situações candidatas daquela partida (pelo tipo e
// pelas etiquetas do momento) e com a função do jogador em foco, até um teto
// de caracteres por voz:
//
//	conhecimento/profissional/indice.md            (só para quem lê; o app não manda)
//	conhecimento/profissional/rotas-e-ondas.md
//	conhecimento/profissional/funcao-selva.md
//	conhecimento/especialista-solo-queue/objetivos.md
//	...
//
// Sem a pasta (ou sem tema que case), vale o arquivo único de antes
// (profissional.md), que fica como resumo geral. O nome do arquivo é o tema;
// QUAL situação puxa QUAL tema está só aqui, em TemasPorSinal e TemaDaFuncao.

// TetoMaterialVoz: o máximo de caracteres de material por voz num pedido
// (soma dos temas escolhidos). Maior que o arquivo único (LimiteArquivoVoz)
// porque o pedido tem folga: o resto do texto da partida passa de 40 mil.
const TetoMaterialVoz = 18000

// LimiteArquivoTema: o teto de cada arquivo de tema (o excesso é cortado).
const LimiteArquivoTema = 5000

// IndiceTemas: o arquivo da pasta que é só índice (não vai para a IA).
const IndiceTemas = "indice.md"

// TemasPorSinal: que temas cada sinal de uma situação candidata puxa, do mais
// ao menos ligado. O sinal é o tipo do momento ("abate", "janela_perdida",
// "luta", "objetivo", "torre", "inibidor", "freeze"), o tipo com o subtipo
// ("janela_perdida:selva") ou uma etiqueta do momento ("sozinho",
// "antes_de_objetivo", "previa_gankando"...; analise/fatos_momentos.go e
// fatos_calculo.go). O primeiro tema da lista vale 2 pontos, os outros 1.
var TemasPorSinal = map[string][]string{
	// Tipos de momento.
	"abate":                {"morte-e-risco"},
	"abate:morte_cara":     {"morte-e-risco"},
	"janela_perdida":       {"meio-de-jogo", "objetivos"},
	"janela_perdida:selva": {"selva", "visao"},
	"janela_perdida:rota":  {"rotas-e-ondas", "meio-de-jogo"},
	"luta":                 {"lutas"},
	"objetivo":             {"objetivos", "visao"},
	"torre":                {"meio-de-jogo", "rotas-e-ondas"},
	"inibidor":             {"meio-de-jogo"},
	"freeze":               {"rotas-e-ondas"},
	// Etiquetas de morte.
	"sozinho":            {"morte-e-risco", "visao"},
	"antes_de_objetivo":  {"objetivos", "morte-e-risco"},
	"inferioridade":      {"lutas", "morte-e-risco"},
	"sem_troca":          {"lutas", "morte-e-risco"},
	"r_em_recarga":       {"lutas"},
	"flash_em_recarga":   {"lutas", "morte-e-risco"},
	"passiva_em_recarga": {"lutas"},
	"fora_da_rota":       {"meio-de-jogo", "morte-e-risco"},
	// (a etiqueta "luta" de uma morte usa a linha do tipo "luta", acima)
	// O que o jogador fazia antes de morrer (previa_<atividade>).
	"previa_gankando":       {"selva"},
	"previa_counter_gank":   {"selva", "lutas"},
	"previa_invadindo":      {"selva", "visao"},
	"previa_farmando":       {"rotas-e-ondas", "morte-e-risco"},
	"previa_coletando_ouro": {"rotas-e-ondas"},
	"previa_trocando":       {"rotas-e-ondas", "lutas"},
	"previa_roaming":        {"meio-de-jogo", "rotas-e-ondas"},
	"previa_rotacionando":   {"meio-de-jogo"},
	"previa_split_push":     {"meio-de-jogo", "morte-e-risco"},
	"previa_agrupado":       {"lutas", "meio-de-jogo"},
	"previa_objetivo":       {"objetivos"},
	"previa_torre":          {"meio-de-jogo"},
	"previa_dive":           {"lutas", "rotas-e-ondas"},
	"previa_andando":        {"morte-e-risco", "visao"},
	"previa_parado":         {"morte-e-risco"},
	"previa_recall":         {"rotas-e-ondas"},
	// Etiquetas de luta e objetivo.
	"objetivo_depois": {"objetivos", "lutas"},
	"depois_de_morte": {"objetivos", "morte-e-risco"},
}

// TemaDaFuncao: o tema de cada função; entra antes dos outros (vale para
// toda situação do jogador em foco).
var TemaDaFuncao = map[string]string{"TOP": "funcao-topo", "JUNGLE": "funcao-selva", "MIDDLE": "funcao-meio",
	"BOTTOM": "funcao-atirador", "UTILITY": "funcao-suporte"}

// TemasComplemento: entram por último, se ainda couberem no teto, mesmo sem
// sinal (o relatório tem a seção de build, que não sai de uma situação).
var TemasComplemento = []string{"build-e-adaptacao"}

// OrdemTemas: o desempate (e a ordem do índice). Tema fora desta lista
// (um arquivo novo na pasta) só entra se um sinal o puxar.
var OrdemTemas = []string{"morte-e-risco", "objetivos", "lutas", "selva", "visao", "rotas-e-ondas", "meio-de-jogo",
	"build-e-adaptacao", "mentalidade", "funcao-topo", "funcao-selva", "funcao-meio", "funcao-atirador", "funcao-suporte"}

// TemaEscolhido: um tema que entrou no material da voz, e por quê.
type TemaEscolhido struct {
	Tema       string   `json:"tema"`
	Arquivo    string   `json:"arquivo"` // pasta/tema.md (sem o caminho do disco)
	Caracteres int      `json:"caracteres"`
	Motivos    []string `json:"motivos"` // "função do jogador em foco", "S2 abate: sozinho"...
}

// sinaisDoMomento: os sinais de uma situação candidata.
func sinaisDoMomento(m analise.MomentoChave) []string {
	s := []string{m.Tipo}
	if m.Subtipo != "" {
		s = append(s, m.Tipo+":"+m.Subtipo)
	}
	return append(s, m.Tags...)
}

// PastaDeTemas: o nome da pasta de temas da voz ("" para as vozes cujo
// arquivo depende do campeão ou do elo).
func PastaDeTemas(v Voz) string {
	if v.ID == "mono" || v.ID == "melhores_do_elo" || v.ID == IDVozEspecialista {
		return ""
	}
	return strings.TrimSuffix(v.Arquivo, ".md")
}

// TemasNaPasta: os temas (arquivos .md não vazios, menos o índice) da pasta
// da voz, em ordem alfabética.
func TemasNaPasta(pasta string, v Voz) []string {
	p := PastaDeTemas(v)
	if pasta == "" || p == "" {
		return nil
	}
	ents, err := os.ReadDir(filepath.Join(pasta, p))
	if err != nil {
		return nil
	}
	var r []string
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".md") || n == IndiceTemas {
			continue
		}
		if info, err := e.Info(); err != nil || info.Size() == 0 {
			continue
		}
		r = append(r, strings.TrimSuffix(n, ".md"))
	}
	sort.Strings(r)
	return r
}

// PontuaTemas: os temas que casam com as situações e com a função, na ordem
// em que entram (função primeiro, depois por pontos e pela OrdemTemas), com
// os motivos. Só devolve temas da lista existentes.
func PontuaTemas(existentes []string, situacoes []analise.MomentoChave, funcao string) []TemaEscolhido {
	tem := map[string]bool{}
	for _, t := range existentes {
		tem[t] = true
	}
	pontos := map[string]int{}
	// Motivos agrupados por sinal ("sozinho (S1, S3)"): curto no cabeçalho do tema.
	sinaisDe := map[string][]string{}
	sitsDe := map[string]map[string][]string{}
	for i, m := range situacoes {
		for _, sinal := range sinaisDoMomento(m) {
			for k, t := range TemasPorSinal[sinal] {
				if !tem[t] {
					continue
				}
				if k == 0 {
					pontos[t] += 2
				} else {
					pontos[t]++
				}
				if sitsDe[t] == nil {
					sitsDe[t] = map[string][]string{}
				}
				if _, ok := sitsDe[t][sinal]; !ok {
					sinaisDe[t] = append(sinaisDe[t], sinal)
				}
				if s := fmt.Sprintf("S%d", i+1); !contem2(sitsDe[t][sinal], s) {
					sitsDe[t][sinal] = append(sitsDe[t][sinal], s)
				}
			}
		}
	}
	motivos := map[string][]string{}
	for t, ss := range sinaisDe {
		for _, s := range ss {
			motivos[t] = append(motivos[t], s+" ("+strings.Join(sitsDe[t][s], ", ")+")")
		}
	}
	posicao := map[string]int{}
	for i, t := range OrdemTemas {
		posicao[t] = i
	}
	var lista []string
	for t := range pontos {
		lista = append(lista, t)
	}
	sort.Slice(lista, func(a, b int) bool {
		if pontos[lista[a]] != pontos[lista[b]] {
			return pontos[lista[a]] > pontos[lista[b]]
		}
		pa, oka := posicao[lista[a]]
		pb, okb := posicao[lista[b]]
		if oka != okb {
			return oka
		}
		if pa != pb {
			return pa < pb
		}
		return lista[a] < lista[b]
	})
	var r []TemaEscolhido
	if tf := TemaDaFuncao[funcao]; tf != "" && tem[tf] {
		r = append(r, TemaEscolhido{Tema: tf, Motivos: []string{"função do jogador em foco (" + nomeFuncaoCurto(funcao) + ")"}})
	}
	for _, t := range lista {
		if t == TemaDaFuncao[funcao] {
			r[0].Motivos = append(r[0].Motivos, motivos[t]...)
			continue
		}
		r = append(r, TemaEscolhido{Tema: t, Motivos: motivos[t]})
	}
	if len(r) == 0 {
		// Nada casou: o complemento sozinho não justifica trocar o arquivo único.
		return r
	}
	for _, t := range TemasComplemento {
		if tem[t] && pontos[t] == 0 && t != TemaDaFuncao[funcao] {
			r = append(r, TemaEscolhido{Tema: t, Motivos: []string{"complemento (seção de build do relatório), se couber"}})
		}
	}
	return r
}

// LeTemasDaVoz escolhe e lê os temas da voz para estas situações, até
// TetoMaterialVoz. Tema que não cabe fica em fora ("tema: motivo"). ok falso
// quando a voz não tem pasta de temas ou nenhum tema casou (vale o arquivo
// único).
func LeTemasDaVoz(pasta string, v Voz, situacoes []analise.MomentoChave, funcao string) (escolhidos []TemaEscolhido, fora []string, texto string, ok bool) {
	existentes := TemasNaPasta(pasta, v)
	if len(existentes) == 0 {
		return nil, nil, "", false
	}
	p := PastaDeTemas(v)
	var b strings.Builder
	total := 0
	for _, t := range PontuaTemas(existentes, situacoes, funcao) {
		dados, err := os.ReadFile(filepath.Join(pasta, p, t.Tema+".md"))
		if err != nil {
			continue
		}
		s := strings.TrimSpace(strings.ReplaceAll(string(dados), "\r\n", "\n"))
		if utf8.RuneCountInString(s) > LimiteArquivoTema {
			s = string([]rune(s)[:LimiteArquivoTema]) + "\n[… tema cortado pelo app]"
		}
		bloco := fmt.Sprintf("#### Tema %s (de %s/%s.md; casou com: %s)\n%s\n\n", t.Tema, p, t.Tema, strings.Join(t.Motivos, "; "), s)
		n := utf8.RuneCountInString(bloco)
		if total+n > TetoMaterialVoz {
			fora = append(fora, t.Tema+": não coube no teto de "+fmt.Sprint(TetoMaterialVoz)+" caracteres")
			continue
		}
		total += n
		t.Arquivo = p + "/" + t.Tema + ".md"
		t.Caracteres = n
		escolhidos = append(escolhidos, t)
		b.WriteString(bloco)
	}
	if len(escolhidos) == 0 {
		return nil, fora, "", false
	}
	return escolhidos, fora, strings.TrimSpace(b.String()), true
}

// MaterialNaPasta: o que GET /api/ia/estado diz de cada voz (quantos temas e
// de onde vem o material), sem ler conteúdo.
type MaterialNaPasta struct {
	Voz      string   `json:"voz"`
	Origem   string   `json:"origem"` // temas, arquivo, por_campeao, por_elo, nenhum
	Pasta    string   `json:"pasta"`  // a subpasta de temas ("" sem)
	Temas    int      `json:"temas"`
	Lista    []string `json:"lista"`    // os temas
	Arquivos []string `json:"arquivos"` // arquivos únicos que existem
}

// InventarioConhecimento: o material de cada voz na pasta.
func InventarioConhecimento(pasta string) []MaterialNaPasta {
	var nomes []string
	if pasta != "" {
		if ents, err := os.ReadDir(pasta); err == nil {
			for _, e := range ents {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
					nomes = append(nomes, e.Name())
				}
			}
		}
	}
	var r []MaterialNaPasta
	for _, v := range Vozes {
		m := MaterialNaPasta{Voz: v.ID, Origem: "nenhum", Lista: []string{}, Arquivos: []string{}}
		switch v.ID {
		case "mono", "melhores_do_elo":
			prefixo := "mono-"
			if v.ID == "melhores_do_elo" {
				prefixo = "melhores-do-elo"
			}
			for _, n := range nomes {
				if strings.HasPrefix(n, prefixo) {
					m.Arquivos = append(m.Arquivos, n)
				}
			}
			if len(m.Arquivos) > 0 {
				m.Origem = map[string]string{"mono": "por_campeao", "melhores_do_elo": "por_elo"}[v.ID]
			}
		case IDVozEspecialista:
			// As sínteses por patch: especialista-lane/<patch>/<elo>-<rota>.md.
			base := filepath.Join(pasta, PastaEspecialista)
			if pasta != "" {
				if ps, err := os.ReadDir(base); err == nil {
					for _, p := range ps {
						if !p.IsDir() || !patchValido(p.Name()) {
							continue
						}
						if fs, err := os.ReadDir(filepath.Join(base, p.Name())); err == nil {
							for _, f := range fs {
								if !f.IsDir() && strings.HasSuffix(f.Name(), ".md") {
									m.Arquivos = append(m.Arquivos, PastaEspecialista+"/"+p.Name()+"/"+f.Name())
								}
							}
						}
					}
				}
			}
			if len(m.Arquivos) > 0 {
				m.Origem = "por_patch"
				m.Pasta = PastaEspecialista + "/"
			}
		default:
			for _, n := range nomes {
				if n == v.Arquivo {
					m.Arquivos = append(m.Arquivos, n)
					m.Origem = "arquivo"
				}
			}
			if ts := TemasNaPasta(pasta, v); len(ts) > 0 {
				m.Origem, m.Pasta, m.Temas, m.Lista = "temas", PastaDeTemas(v)+"/", len(ts), ts
			}
		}
		r = append(r, m)
	}
	return r
}
