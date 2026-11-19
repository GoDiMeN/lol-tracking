package ia

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// A VOZ "especialista_lane": fala pela SÍNTESE do Especialista Lane do
// patch × elo × rota do jogador em foco (o arquivo de conhecimento
// especialista-lane/<patch>/<elo>-<rota>.md, escrito pela síntese ou à mão).
// Patch: o da partida analisada; se não há síntese dele, a do patch mais novo
// que houver, com o AVISO de qual patch é. Elo: o elo de comparação do
// jogador (elo.go). Convive com "melhores do elo", que segue sendo a
// comparação numérica com as partidas da coleta.

// IDVozEspecialista: o id da voz.
const IDVozEspecialista = "especialista_lane"

// LimiteSinteseVoz: o teto do arquivo da síntese no pedido (caracteres).
const LimiteSinteseVoz = 12000

var reMetaLane = regexp.MustCompile(`<!--\s*lane:([^>]*)-->`)

// metaDoArquivo lê a linha "<!-- lane: chave=valor ... -->" da síntese.
func metaDoArquivo(texto string) map[string]string {
	r := map[string]string{}
	m := reMetaLane.FindStringSubmatch(texto)
	if m == nil {
		return r
	}
	for _, kv := range strings.Fields(m[1]) {
		if k, v, ok := strings.Cut(kv, "="); ok {
			r[k] = v
		}
	}
	return r
}

// PatchesComSintese: os patches que têm o arquivo do elo + rota, do mais novo.
func PatchesComSintese(conhecimento, elo, funcao string) []string {
	if conhecimento == "" || elo == "" || funcao == "" {
		return nil
	}
	base := filepath.Join(conhecimento, PastaEspecialista)
	ents, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	nome := slug(elo) + "-" + slug(funcao) + ".md"
	var r []string
	for _, e := range ents {
		if !e.IsDir() || !patchValido(e.Name()) {
			continue
		}
		if info, err := os.Stat(filepath.Join(base, e.Name(), nome)); err == nil && info.Size() > 0 {
			r = append(r, e.Name())
		}
	}
	for i := 1; i < len(r); i++ {
		for j := i; j > 0 && ComparaPatch(r[j], r[j-1]) > 0; j-- {
			r[j], r[j-1] = r[j-1], r[j]
		}
	}
	return r
}

// materialEspecialista: o arquivo da síntese que vale para o jogador em foco.
func (s *Servico) materialEspecialista(info InfoPartida, foco Participante, elo EloComparacao) (arquivos []string, texto string, partidas int, amostra, falta string) {
	tier := TierDe(elo.Elo)
	switch {
	case foco.Funcao == "":
		return nil, "", 0, "", "o jogador em foco não tem função nesta partida: a síntese é por rota"
	case tier == "":
		return nil, "", 0, "", "o elo de comparação deste jogador não é conhecido: escolha \"comparar com o elo\" na tela da análise (ou ponha a chave da Riot)"
	}
	r := Recorte{Patch: info.Patch, Elo: tier, Funcao: foco.Funcao}
	patches := PatchesComSintese(s.Conhecimento, tier, foco.Funcao)
	if len(patches) == 0 {
		return nil, "", 0, "", fmt.Sprintf("sem síntese do Especialista Lane de %s %s: gere relatórios de referência e a síntese em Estudo → Especialista Lane",
			NomeElo(tier), FuncaoNaFrase(foco.Funcao))
	}
	usado := patches[0]
	for _, p := range patches {
		if p == info.Patch {
			usado = p
		}
	}
	r.Patch = usado
	rel := r.ArquivoSintese()
	conteudo, err := s.leConhecimento(rel)
	if err != nil {
		return nil, "", 0, "", "não deu para ler " + rel
	}
	conteudo = strings.TrimSpace(conteudo)
	meta := metaDoArquivo(conteudo)
	partidas, _ = strconv.Atoi(meta["partidas"])
	jogadores, _ := strconv.Atoi(meta["jogadores"])
	relatorios, _ := strconv.Atoi(meta["relatorios"])
	if utf8.RuneCountInString(conteudo) > LimiteSinteseVoz {
		conteudo = string([]rune(conteudo)[:LimiteSinteseVoz]) + "\n[… síntese cortada pelo app]"
	}
	if relatorios > 0 {
		amostra = fmt.Sprintf("síntese de %s de referência (%s, %s) de %s %s, patch %s", plural(relatorios, "relatório", "relatórios"),
			plural(partidas, "partida", "partidas"), plural(jogadores, "jogador", "jogadores"), NomeElo(tier), FuncaoNaFrase(foco.Funcao), usado)
	} else {
		amostra = fmt.Sprintf("síntese escrita à mão de %s %s, patch %s (amostra não informada)", NomeElo(tier), FuncaoNaFrase(foco.Funcao), usado)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "#### Síntese do Especialista Lane (de %s; AMOSTRA: %s)\n", rel, amostra)
	if info.Patch != "" && usado != info.Patch {
		fmt.Fprintf(&b, "ATENÇÃO: não há síntese do patch %s (o desta partida); esta é do patch %s. Diga isso na resposta (\"na síntese do patch %s...\").\n",
			info.Patch, usado, usado)
	}
	b.WriteString("Como usar: aplique a regra cujas condições (\"se X e Y\") batem com os fatos da ficha. Regra \"em N jogadores\" se repetiu; " +
		"\"HIPÓTESE\" veio de um jogador só: diga que é hipótese. Sempre diga a amostra. Não invente regra que não está aqui.\n\n")
	b.WriteString(conteudo)
	return []string{rel}, b.String(), partidas, amostra, ""
}
