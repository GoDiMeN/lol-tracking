package ia

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"unicode/utf8"

	"loltracking/server/analise"
	"loltracking/server/banco"
	"loltracking/server/web"
)

// A SÍNTESE de um recorte (patch × elo × rota): junta os relatórios de
// referência concluídos (a última versão de cada partida + jogador) e vira o
// arquivo de conhecimento da voz especialista_lane. A IA propõe as regras e
// diz de quais relatórios cada uma saiu; quem conta os JOGADORES distintos e
// decide "se repete" × "caso único (hipótese)" é o app. Refeita a pedido
// (botão), nunca sozinha: cada uma é uma chamada à IA.

// AmostraMinimaPatch: partidas por lado abaixo das quais a comparação entre
// patches não afirma mudança nenhuma (só mostra os números crus).
const AmostraMinimaPatch = 10

// Sintese é a resposta de GET /api/especialista/sintese.
type Sintese struct {
	ID            int64            `json:"id"`
	Patch         string           `json:"patch"`
	Elo           string           `json:"elo"`
	Funcao        string           `json:"rota"`
	Versao        int              `json:"versao"`
	Estado        string           `json:"estado"`
	Erro          string           `json:"erro"`
	CriadoEm      string           `json:"criado_em"`
	TerminadoEm   string           `json:"terminado_em"`
	Modelo        string           `json:"modelo"`
	Implementacao string           `json:"implementacao"`
	VersaoPrompt  int              `json:"versao_prompt"`
	Relatorios    []int64          `json:"relatorios"`
	Partidas      int              `json:"partidas"`
	Jogadores     int              `json:"jogadores"`
	Conteudo      *ConteudoSintese `json:"conteudo"`
	Avisos        []string         `json:"avisos"`
	Enviado       Enviado          `json:"enviado"`
	Arquivo       string           `json:"arquivo"`
	Uso           Resposta         `json:"uso"`
	Bruto         string           `json:"bruto,omitempty"`
}

// ConteudoSintese: o que a síntese diz.
type ConteudoSintese struct {
	Amostra     string         `json:"amostra"`
	Resumo      string         `json:"resumo"`
	PapelEstilo string         `json:"papel_estilo"`
	Contagem    ContagemPapel  `json:"contagem"`
	Regras      []RegraSintese `json:"regras"`
	CasoUnico   []RegraSintese `json:"caso_unico"`
	ErrosComuns []RegraSintese `json:"erros_comuns"`
	// Comparacao: com outro patch do mesmo elo e rota (calculada pelo app).
	Comparacao    ComparacaoPatches `json:"comparacao"`
	MudancasPatch []MudancaPatch    `json:"mudancas_patch"`
	OQueFalta     []string          `json:"o_que_falta"`
	Fontes        []FonteSintese    `json:"fontes"`
}

// ContagemPapel: quantos jogadores de referência do recorte em cada papel e estilo.
type ContagemPapel struct {
	Papeis  map[string]int `json:"papeis"`
	Estilos map[string]int `json:"estilos"`
	Total   int            `json:"total"`
}

// RegraSintese: uma regra (ou erro) e de onde saiu.
type RegraSintese struct {
	Texto      string   `json:"texto"`
	Relatorios []string `json:"relatorios"`
	Jogadores  int      `json:"jogadores"`
	Partidas   []int64  `json:"partidas"`
	Fonte      string   `json:"fonte"`
}

// MudancaPatch: uma mudança entre patches (só com amostra suficiente).
type MudancaPatch struct {
	Texto string `json:"texto"`
	Base  string `json:"base"`
}

// FonteSintese: R1, R2... → o relatório.
type FonteSintese struct {
	Codigo         string `json:"codigo"`
	RelatorioID    int64  `json:"relatorio_id"`
	GameID         int64  `json:"game_id"`
	ParticipanteID int    `json:"participante_id"`
	Campeao        string `json:"campeao"`
	Venceu         bool   `json:"venceu"`
	Versao         int    `json:"versao"`
	Arquivo        string `json:"arquivo"`
	puuid          string
}

// ComparacaoPatches: os indicadores médios do recorte num patch e noutro.
type ComparacaoPatches struct {
	Patch      string       `json:"patch"`
	Outro      string       `json:"outro"`
	Elo        string       `json:"elo"`
	Funcao     string       `json:"rota"`
	NPatch     int          `json:"partidas_patch"`
	NOutro     int          `json:"partidas_outro"`
	Minimo     int          `json:"minimo"`
	Suficiente bool         `json:"suficiente"`
	Linhas     []LinhaPatch `json:"linhas"`
	// RegrasOutro: as regras da síntese do outro patch (para ler lado a lado).
	RegrasOutro []string `json:"regras_outro"`
	Texto       string   `json:"texto"`
}

// LinhaPatch: um indicador nos dois patches.
type LinhaPatch struct {
	Indicador string  `json:"indicador"`
	Patch     float64 `json:"patch"`
	Outro     float64 `json:"outro"`
	NPatch    int     `json:"n_patch"`
	NOutro    int     `json:"n_outro"`
	// Leitura: "mudou", "sem mudança clara" ou "amostra insuficiente".
	Leitura string `json:"leitura"`
}

type indicadorPatch struct {
	nome string
	val  func(analise.IndicadoresJogador) (float64, bool)
}

func indicadoresPatch() []indicadorPatch {
	por10 := func(n, d int) (float64, bool) {
		if d <= 0 {
			return 0, false
		}
		return float64(n) * 600 / float64(d), true
	}
	return []indicadorPatch{
		{"vitórias (%)", func(x analise.IndicadoresJogador) (float64, bool) {
			if x.Venceu {
				return 100, true
			}
			return 0, true
		}},
		{"KDA", func(x analise.IndicadoresJogador) (float64, bool) { return x.KDA, true }},
		{"mortes por 10 min", func(x analise.IndicadoresJogador) (float64, bool) { return por10(x.Mortes, x.DuracaoS) }},
		{"mortes na fase de rotas", func(x analise.IndicadoresJogador) (float64, bool) { return float64(x.MortesRotas), true }},
		{"ouro aos 15 contra o adversário", func(x analise.IndicadoresJogador) (float64, bool) {
			if x.DifOuro15 == nil {
				return 0, false
			}
			return float64(*x.DifOuro15), true
		}},
		{"CS aos 15", func(x analise.IndicadoresJogador) (float64, bool) {
			if x.CS15 == nil {
				return 0, false
			}
			return float64(*x.CS15), true
		}},
		{"objetivos do time com participação (%)", func(x analise.IndicadoresJogador) (float64, bool) {
			if x.ObjetivosTime <= 0 {
				return 0, false
			}
			return float64(x.ObjetivosParticipou) * 100 / float64(x.ObjetivosTime), true
		}},
		{"sentinelas por 10 min", func(x analise.IndicadoresJogador) (float64, bool) { return por10(x.Sentinelas, x.DuracaoS) }},
		{"janelas aproveitadas", func(x analise.IndicadoresJogador) (float64, bool) { return float64(x.JanelasAproveitadas), true }},
	}
}

func mediaDesvio(v []float64) (float64, float64) {
	if len(v) == 0 {
		return 0, 0
	}
	var s float64
	for _, x := range v {
		s += x
	}
	m := s / float64(len(v))
	if len(v) < 2 {
		return m, 0
	}
	var q float64
	for _, x := range v {
		q += (x - m) * (x - m)
	}
	return m, math.Sqrt(q / float64(len(v)-1))
}

func partidasDe(xs []analise.IndicadoresJogador) int {
	g := map[int64]bool{}
	for _, x := range xs {
		g[x.GameID] = true
	}
	return len(g)
}

// CalculaComparacaoPatches compara o recorte com o patch mais próximo que tem
// partidas do mesmo elo e rota (o anterior; se não houver, o seguinte). Só
// marca "mudou" com AmostraMinimaPatch partidas ou mais em CADA lado e
// diferença maior que 2 erros-padrão (as duas médias têm variação própria;
// abaixo disso, a diferença pode ser só a amostra).
func CalculaComparacaoPatches(todas []analise.IndicadoresJogador, r Recorte) ComparacaoPatches {
	c := ComparacaoPatches{Patch: r.Patch, Elo: r.Elo, Funcao: r.Funcao, Minimo: AmostraMinimaPatch, Linhas: []LinhaPatch{}, RegrasOutro: []string{}}
	porPatch := map[string][]analise.IndicadoresJogador{}
	for _, x := range todas {
		if x.Funcao != r.Funcao || (TierDe(x.Elo) != r.Elo && TierDe(x.EloPartida) != r.Elo) || x.Patch == "" {
			continue
		}
		porPatch[x.Patch] = append(porPatch[x.Patch], x)
	}
	var antes, depois string
	for p := range porPatch {
		switch ComparaPatch(p, r.Patch) {
		case -1:
			if antes == "" || ComparaPatch(p, antes) > 0 {
				antes = p
			}
		case 1:
			if depois == "" || ComparaPatch(p, depois) < 0 {
				depois = p
			}
		}
	}
	c.Outro = antes
	if c.Outro == "" {
		c.Outro = depois
	}
	a := porPatch[r.Patch]
	c.NPatch = partidasDe(a)
	if c.Outro == "" {
		c.Texto = fmt.Sprintf("Sem outro patch com partidas de referência de %s %s: não há com o que comparar (este patch: %s).",
			NomeElo(r.Elo), FuncaoNaFrase(r.Funcao), plural(c.NPatch, "partida", "partidas"))
		return c
	}
	o := porPatch[c.Outro]
	c.NOutro = partidasDe(o)
	c.Suficiente = c.NPatch >= AmostraMinimaPatch && c.NOutro >= AmostraMinimaPatch
	for _, ind := range indicadoresPatch() {
		var va, vo []float64
		for _, x := range a {
			if v, ok := ind.val(x); ok {
				va = append(va, v)
			}
		}
		for _, x := range o {
			if v, ok := ind.val(x); ok {
				vo = append(vo, v)
			}
		}
		if len(va) == 0 && len(vo) == 0 {
			continue
		}
		ma, da := mediaDesvio(va)
		mo, do := mediaDesvio(vo)
		l := LinhaPatch{Indicador: ind.nome, Patch: arred(ma), Outro: arred(mo), NPatch: len(va), NOutro: len(vo), Leitura: "amostra insuficiente"}
		if c.Suficiente && len(va) >= AmostraMinimaPatch && len(vo) >= AmostraMinimaPatch {
			ep := math.Sqrt(da*da/float64(len(va)) + do*do/float64(len(vo)))
			dif := math.Abs(ma - mo)
			if (ep == 0 && dif > 0) || (ep > 0 && dif > 2*ep) {
				l.Leitura = "mudou"
			} else {
				l.Leitura = "sem mudança clara"
			}
		}
		c.Linhas = append(c.Linhas, l)
	}
	var b strings.Builder
	if c.Suficiente {
		fmt.Fprintf(&b, "Comparação com o patch %s: AMOSTRA SUFICIENTE (%d partidas neste patch, %d no %s; mínimo %d por lado). "+
			"\"Mudou\" = diferença das médias maior que 2 erros-padrão.\n", c.Outro, c.NPatch, c.NOutro, c.Outro, AmostraMinimaPatch)
	} else {
		fmt.Fprintf(&b, "Comparação com o patch %s: AMOSTRA INSUFICIENTE (%d partida(s) neste patch, %d no %s; o mínimo para afirmar "+
			"mudança é %d por lado). Os números abaixo são crus: não dizem que o comportamento mudou.\n", c.Outro, c.NPatch, c.NOutro, c.Outro, AmostraMinimaPatch)
	}
	fmt.Fprintf(&b, "| indicador | patch %s | patch %s | leitura |\n|---|---|---|---|\n", r.Patch, c.Outro)
	for _, l := range c.Linhas {
		fmt.Fprintf(&b, "| %s | %s (n=%d) | %s (n=%d) | %s |\n", l.Indicador, decimal(l.Patch), l.NPatch, decimal(l.Outro), l.NOutro, l.Leitura)
	}
	c.Texto = b.String()
	return c
}

func arred(v float64) float64 { return math.Round(v*10) / 10 }

// ComparacaoDoRecorte: a comparação e as regras da síntese do outro patch.
func (s *Servico) ComparacaoDoRecorte(r Recorte) (ComparacaoPatches, error) {
	todas, err := s.Fontes.Referencias(analise.FiltroReferencia{Elo: r.Elo, Funcao: r.Funcao, Limite: 200})
	if err != nil {
		return ComparacaoPatches{}, err
	}
	c := CalculaComparacaoPatches(todas, r)
	if c.Outro != "" {
		if sv, ok, err := LeSintese(s.DB, Recorte{Patch: c.Outro, Elo: r.Elo, Funcao: r.Funcao}, 0); err == nil && ok && sv.Conteudo != nil {
			for _, x := range append(append([]RegraSintese{}, sv.Conteudo.Regras...), sv.Conteudo.CasoUnico...) {
				c.RegrasOutro = append(c.RegrasOutro, x.Texto)
			}
		}
	}
	return c, nil
}

// ErrSemRelatorios: o recorte não tem relatório concluído (409).
var ErrSemRelatorios = errors.New("este recorte ainda não tem relatório de referência concluído: gere pelo menos um")

// dadosSintese: o que a síntese precisa, lido do banco.
type dadosSintese struct {
	r          Recorte
	relatorios []RelatorioReferencia
	fontes     []FonteSintese
	refs       []analise.IndicadoresJogador // os jogadores de referência do recorte (com e sem relatório)
	comp       ComparacaoPatches
	contagem   ContagemPapel
	jogadores  int
	partidas   int
	amostra    string
}

func (s *Servico) leDadosSintese(r Recorte) (dadosSintese, error) {
	d := dadosSintese{r: r}
	ids, err := relatoriosConcluidosDoRecorte(s.DB, r)
	if err != nil {
		return d, err
	}
	if len(ids) == 0 {
		return d, ErrSemRelatorios
	}
	quem, jogos := map[string]bool{}, map[int64]bool{}
	vitorias := 0
	for i, id := range ids {
		rr, err := leRelEspPorID(s.DB, id)
		if err != nil {
			return d, err
		}
		d.relatorios = append(d.relatorios, rr)
		f := FonteSintese{Codigo: fmt.Sprintf("R%d", i+1), RelatorioID: id, GameID: rr.GameID, ParticipanteID: rr.ParticipanteID, Campeao: rr.Campeao,
			Venceu: rr.Venceu, Versao: rr.Versao, Arquivo: rr.Arquivo, puuid: rr.puuid}
		if f.puuid == "" {
			f.puuid = fmt.Sprint(rr.GameID, ":", rr.ParticipanteID)
		}
		quem[f.puuid] = true
		jogos[rr.GameID] = true
		if rr.Venceu {
			vitorias++
		}
		d.fontes = append(d.fontes, f)
	}
	d.jogadores, d.partidas = len(quem), len(jogos)
	todas, err := s.Fontes.Referencias(analise.FiltroReferencia{Elo: r.Elo, Funcao: r.Funcao, Limite: 200})
	if err != nil {
		return d, err
	}
	d.comp = CalculaComparacaoPatches(todas, r)
	if d.comp.Outro != "" {
		if sv, ok, err := LeSintese(s.DB, Recorte{Patch: d.comp.Outro, Elo: r.Elo, Funcao: r.Funcao}, 0); err == nil && ok && sv.Conteudo != nil {
			for _, x := range append(append([]RegraSintese{}, sv.Conteudo.Regras...), sv.Conteudo.CasoUnico...) {
				d.comp.RegrasOutro = append(d.comp.RegrasOutro, x.Texto)
			}
		}
	}
	d.contagem = ContagemPapel{Papeis: map[string]int{}, Estilos: map[string]int{}}
	for _, x := range todas {
		if x.Patch != r.Patch {
			continue
		}
		d.refs = append(d.refs, x)
		d.contagem.Total++
		if x.Papel.Rotulo != "" {
			d.contagem.Papeis[x.Papel.Rotulo]++
		}
		if x.Papel.EstiloRotulo != "" {
			d.contagem.Estilos[x.Papel.EstiloRotulo]++
		}
	}
	d.amostra = fmt.Sprintf("%s de referência (%s, %s; %s) de %s %s no patch %s",
		plural(len(ids), "relatório", "relatórios"), plural(d.partidas, "partida", "partidas"), plural(d.jogadores, "jogador", "jogadores"),
		plural(vitorias, "vitória", "vitórias"), NomeElo(r.Elo), FuncaoNaFrase(r.Funcao), r.Patch)
	if len(d.refs) > len(ids) {
		d.amostra += fmt.Sprintf("; há %d jogador(es) de referência no recorte, %d ainda sem relatório", len(d.refs), len(d.refs)-len(ids))
	}
	return d, nil
}

func textoContagem(c ContagemPapel) string {
	if c.Total == 0 {
		return "sem papel calculado"
	}
	lista := func(m map[string]int) string {
		var ks []string
		for k := range m {
			ks = append(ks, k)
		}
		sort.Slice(ks, func(i, j int) bool {
			if m[ks[i]] != m[ks[j]] {
				return m[ks[i]] > m[ks[j]]
			}
			return ks[i] < ks[j]
		})
		var p []string
		for _, k := range ks {
			p = append(p, fmt.Sprintf("%s %d", k, m[k]))
		}
		if len(p) == 0 {
			return "nenhum"
		}
		return strings.Join(p, ", ")
	}
	return fmt.Sprintf("de %d jogador(es) de referência do recorte: papel — %s; estilo — %s", c.Total, lista(c.Papeis), lista(c.Estilos))
}

// pedidoSintese monta o texto da síntese.
func (s *Servico) pedidoSintese(d dadosSintese) (Pedido, Enviado) {
	r := d.r
	env := Enviado{Secoes: []string{"os relatórios de referência do recorte (sem nomes de invocador)", "indicadores dos jogadores de referência do recorte",
		"contagem de papel e estilo", "comparação com outro patch (calculada pelo app)"}, Arquivos: []string{}, Cortadas: []string{}}
	var b strings.Builder
	fmt.Fprintf(&b, "RECORTE: patch %s · %s · %s\n", r.Patch, NomeElo(r.Elo), FuncaoNaFrase(r.Funcao))
	fmt.Fprintf(&b, "AMOSTRA: %s.\n", d.amostra)
	if d.jogadores < 2 {
		b.WriteString("AMOSTRA PEQUENA: com um jogador só, toda regra é caso único (hipótese). Diga isso no resumo.\n")
	}
	fmt.Fprintf(&b, "PAPEL E ESTILO (contagem do app): %s.\n\n", textoContagem(d.contagem))
	b.WriteString("## Jogadores de referência do recorte (números da base de fatos)\n")
	for _, x := range d.refs {
		dif := "—"
		if x.DifOuro15 != nil {
			dif = fmt.Sprintf("%+d", *x.DifOuro15)
		}
		fmt.Fprintf(&b, "- partida %d, %s, %s, %s: %d/%d/%d, mortes por 10 min %s, ouro aos 15 contra o adversário %s, objetivos %d de %d, sentinelas %d, janelas %d aproveitadas / %d perdidas, rastreados %d de 10; papel: %s\n",
			x.GameID, x.Campeao, NomeElo(x.Elo), map[bool]string{true: "vitória", false: "derrota"}[x.Venceu], x.Abates, x.Mortes, x.Assistencias,
			porDezMin(x.Mortes, x.DuracaoS), dif, x.ObjetivosParticipou, x.ObjetivosTime, x.Sentinelas, x.JanelasAproveitadas, x.JanelasPerdidas,
			x.RastreadosNaPartida, x.Papel.Frase)
	}
	b.WriteString("\n## Relatórios de referência\n")
	for i, rr := range d.relatorios {
		f := d.fontes[i]
		c := rr.Conteudo
		if c == nil {
			continue
		}
		fmt.Fprintf(&b, "\n### %s: %s, partida %d, %s, %s (relatório v%d; %d de 10 rastreados)\n", f.Codigo, rr.Campeao, rr.GameID, NomeElo(rr.Elo),
			map[bool]string{true: "vitória", false: "derrota"}[rr.Venceu], rr.Versao, rr.Rastreados)
		if c.Quem.FrasePapel != "" {
			fmt.Fprintf(&b, "Papel: %s\n", c.Quem.FrasePapel)
		}
		fmt.Fprintf(&b, "Resumo: %s\n", c.Resumo)
		for _, fs := range c.Fases {
			fmt.Fprintf(&b, "Plano (%s): %s\n", fs.Fase, fs.Plano)
		}
		for k, dc := range c.Decisoes {
			marca := "casos " + strings.Join(dc.Casos, ", ")
			if dc.Hipotese {
				marca = "1 caso"
			}
			fmt.Fprintf(&b, "Decisão %d [%s] %s. Fatos: %s Regra: %s (%s)\n", k+1, dc.T, dc.Titulo, dc.Fatos, dc.Regra, marca)
		}
		for _, e := range c.Erros {
			fmt.Fprintf(&b, "Erro [%s]: %s Lição: %s\n", e.T, e.Fato, e.Licao)
		}
		for _, n := range c.NaoFez {
			fmt.Fprintf(&b, "Não fez (vs usuário): %s (%s)\n", n.Texto, n.Numeros)
		}
	}
	b.WriteString("\n## Comparação com outro patch (calculada pelo app)\n")
	b.WriteString(d.comp.Texto)
	if len(d.comp.RegrasOutro) > 0 {
		fmt.Fprintf(&b, "\nRegras da síntese do patch %s:\n", d.comp.Outro)
		for _, x := range d.comp.RegrasOutro {
			b.WriteString("- " + x + "\n")
		}
	}
	b.WriteString("\nEscreva a síntese agora, só o JSON.")
	p := Pedido{Tipo: TipoAnalise, Sistema: SistemaSintese(), Mensagens: []Mensagem{{Papel: PapelUsuario, Texto: tiraNomesDosRelatorios(b.String(), d)}}}
	env.CaracteresSistema = utf8.RuneCountInString(p.Sistema)
	env.CaracteresMensagem = utf8.RuneCountInString(p.Mensagens[0].Texto)
	env.PartidasReferencia = d.partidas
	return p, env
}

// tiraNomesDosRelatorios: os nomes dos jogadores de referência não vão (a
// base de fatos guarda; a síntese não precisa).
func tiraNomesDosRelatorios(s string, d dadosSintese) string {
	for _, x := range d.refs {
		if n := strings.TrimSpace(x.Nome); utf8.RuneCountInString(n) >= 3 && !strings.EqualFold(n, x.Campeao) {
			s = strings.ReplaceAll(s, n, x.Campeao)
		}
	}
	return s
}

type sinteseBruta struct {
	Resumo      string `json:"resumo"`
	PapelEstilo string `json:"papel_estilo"`
	Regras      []struct {
		Regra      string   `json:"regra"`
		Relatorios []string `json:"relatorios"`
		Fonte      string   `json:"fonte"`
	} `json:"regras"`
	ErrosComuns []struct {
		Texto      string   `json:"texto"`
		Relatorios []string `json:"relatorios"`
	} `json:"erros_comuns"`
	MudancasPatch []struct {
		Texto string `json:"texto"`
		Base  string `json:"base"`
	} `json:"mudancas_patch"`
	OQueFalta []string `json:"o_que_falta"`
}

// ValidaSintese: o app decide repetida × caso único pelos jogadores distintos
// dos relatórios citados; mudança entre patches só com amostra suficiente.
func ValidaSintese(texto string, fontes []FonteSintese, comp ComparacaoPatches) (ConteudoSintese, []string, error) {
	var sb sinteseBruta
	if err := decodificaSolto(texto, &sb); err != nil {
		return ConteudoSintese{}, nil, err
	}
	avisos := []string{}
	limpa := func(s string, max int) string {
		s = strings.TrimSpace(s)
		if r := []rune(s); len(r) > max {
			s = string(r[:max]) + "…"
		}
		return s
	}
	porCodigo := map[string]FonteSintese{}
	for _, f := range fontes {
		porCodigo[f.Codigo] = f
	}
	monta := func(onde, texto string, codigos []string, fonte string) (RegraSintese, bool) {
		x := RegraSintese{Texto: limpa(texto, 600), Relatorios: []string{}, Partidas: []int64{}, Fonte: limpa(fonte, 200)}
		if x.Texto == "" {
			return x, false
		}
		quem, jogos := map[string]bool{}, map[int64]bool{}
		for _, c := range codigos {
			c = strings.ToUpper(strings.TrimSpace(c))
			f, ok := porCodigo[c]
			if !ok {
				avisos = append(avisos, fmt.Sprintf("%s: relatório %q não existe; ignorado", onde, c))
				continue
			}
			if contem2(x.Relatorios, c) {
				continue
			}
			x.Relatorios = append(x.Relatorios, c)
			quem[f.puuid] = true
			if !jogos[f.GameID] {
				jogos[f.GameID] = true
				x.Partidas = append(x.Partidas, f.GameID)
			}
		}
		if len(x.Relatorios) == 0 {
			avisos = append(avisos, onde+" descartada: não cita nenhum relatório válido")
			return x, false
		}
		x.Jogadores = len(quem)
		return x, true
	}
	c := ConteudoSintese{Resumo: limpa(sb.Resumo, 1500), PapelEstilo: limpa(sb.PapelEstilo, 800), Regras: []RegraSintese{}, CasoUnico: []RegraSintese{},
		ErrosComuns: []RegraSintese{}, MudancasPatch: []MudancaPatch{}, OQueFalta: []string{}, Fontes: fontes, Comparacao: comp}
	for i, r := range sb.Regras {
		x, ok := monta(fmt.Sprintf("regra %d", i+1), r.Regra, r.Relatorios, r.Fonte)
		if !ok {
			continue
		}
		if !regraNoFormato(x.Texto) {
			avisos = append(avisos, fmt.Sprintf("regra %d: fora do formato \"se X, então Z, porque W\"", i+1))
		}
		if x.Jogadores >= 2 {
			c.Regras = append(c.Regras, x)
		} else {
			c.CasoUnico = append(c.CasoUnico, x)
		}
	}
	sort.SliceStable(c.Regras, func(a, b int) bool { return c.Regras[a].Jogadores > c.Regras[b].Jogadores })
	for i, e := range sb.ErrosComuns {
		if x, ok := monta(fmt.Sprintf("erro %d", i+1), e.Texto, e.Relatorios, ""); ok {
			c.ErrosComuns = append(c.ErrosComuns, x)
		}
	}
	if len(sb.MudancasPatch) > 0 && !comp.Suficiente {
		avisos = append(avisos, fmt.Sprintf("%d mudança(s) entre patches descartada(s): amostra insuficiente (mínimo %d partidas por lado)",
			len(sb.MudancasPatch), AmostraMinimaPatch))
	} else {
		for _, m := range sb.MudancasPatch {
			if t := limpa(m.Texto, 600); t != "" {
				c.MudancasPatch = append(c.MudancasPatch, MudancaPatch{Texto: t, Base: limpa(m.Base, 400)})
			}
		}
	}
	for _, o := range sb.OQueFalta {
		if t := limpa(o, 400); t != "" {
			c.OQueFalta = append(c.OQueFalta, t)
		}
	}
	if c.Resumo == "" && len(c.Regras) == 0 && len(c.CasoUnico) == 0 {
		return c, avisos, ErrFormato
	}
	return c, avisos, nil
}

// --- Banco ------------------------------------------------------------------------------

const colunasSintese = `id, patch, elo, funcao, versao, estado, erro, criado_em, terminado_em, modelo, implementacao, versao_prompt, relatorios,
	partidas, jogadores, conteudo, bruto, avisos, enviado, arquivo, duracao_ms, caracteres_entrada, caracteres_saida, tokens_entrada,
	tokens_saida, custo_usd`

func leSinteseLinha(s interface{ Scan(...any) error }) (Sintese, error) {
	var x Sintese
	var rels, conteudo, avisos, enviado string
	err := s.Scan(&x.ID, &x.Patch, &x.Elo, &x.Funcao, &x.Versao, &x.Estado, &x.Erro, &x.CriadoEm, &x.TerminadoEm, &x.Modelo, &x.Implementacao,
		&x.VersaoPrompt, &rels, &x.Partidas, &x.Jogadores, &conteudo, &x.Bruto, &avisos, &enviado, &x.Arquivo, &x.Uso.DuracaoMs,
		&x.Uso.CaracteresEntrada, &x.Uso.CaracteresSaida, &x.Uso.TokensEntrada, &x.Uso.TokensSaida, &x.Uso.CustoUSD)
	if err != nil {
		return x, err
	}
	x.Uso.Modelo = x.Modelo
	_ = json.Unmarshal([]byte(rels), &x.Relatorios)
	x.Relatorios = web.NuncaNil(x.Relatorios)
	if conteudo != "" {
		var c ConteudoSintese
		if json.Unmarshal([]byte(conteudo), &c) == nil {
			x.Conteudo = &c
			x.Bruto = ""
		}
	}
	_ = json.Unmarshal([]byte(avisos), &x.Avisos)
	_ = json.Unmarshal([]byte(enviado), &x.Enviado)
	x.Avisos = web.NuncaNil(x.Avisos)
	x.Enviado.Secoes = web.NuncaNil(x.Enviado.Secoes)
	x.Enviado.Arquivos = web.NuncaNil(x.Enviado.Arquivos)
	x.Enviado.Cortadas = web.NuncaNil(x.Enviado.Cortadas)
	return x, nil
}

// LeSintese: a versão pedida (0 = a mais nova) do recorte.
func LeSintese(db *banco.DB, r Recorte, versao int) (Sintese, bool, error) {
	q := `SELECT ` + colunasSintese + ` FROM especialista_sinteses WHERE patch = ? AND elo = ? AND funcao = ?`
	args := []any{r.Patch, r.Elo, r.Funcao}
	if versao > 0 {
		q += ` AND versao = ?`
		args = append(args, versao)
	}
	x, err := leSinteseLinha(db.QueryRow(q+` ORDER BY versao DESC LIMIT 1`, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return x, false, nil
	}
	return x, err == nil, err
}

// VersoesSintese: as versões do recorte, da mais nova.
func VersoesSintese(db *banco.DB, r Recorte) ([]VersaoResumo, error) {
	linhas, err := db.Query(`SELECT versao, estado, criado_em, modelo FROM especialista_sinteses WHERE patch = ? AND elo = ? AND funcao = ?
		ORDER BY versao DESC`, r.Patch, r.Elo, r.Funcao)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	var l []VersaoResumo
	for linhas.Next() {
		var v VersaoResumo
		if err := linhas.Scan(&v.Versao, &v.Estado, &v.CriadoEm, &v.Modelo); err != nil {
			return nil, err
		}
		l = append(l, v)
	}
	return l, linhas.Err()
}

// --- Execução -----------------------------------------------------------------------------

// PreviaSintese: o texto exato, sem chamar a IA.
func (s *Servico) PreviaSintese(r Recorte) (Pedido, Enviado, error) {
	d, err := s.leDadosSintese(r)
	if err != nil {
		return Pedido{}, Enviado{}, err
	}
	p, env := s.pedidoSintese(d)
	return p, env, nil
}

// RefazSintese começa a síntese do recorte (execução com SSE).
func (s *Servico) RefazSintese(ctx context.Context, r Recorte) (Sintese, *web.Execucao, error) {
	d, err := s.leDadosSintese(r)
	if err != nil {
		return Sintese{}, nil, err
	}
	est := s.IA.Estado(ctx)
	if !est.Disponivel {
		return Sintese{}, nil, &erroIA{est.Motivo, ErrIndisponivel}
	}
	e := s.Hub.Nova()
	chave := chaveSintese(r)
	if x := s.ocupa(chave, e); x != nil {
		e.Fecha()
		return Sintese{}, nil, &ErrRodando{"a síntese deste recorte já está rodando", x}
	}
	var ids []int64
	for _, f := range d.fontes {
		ids = append(ids, f.RelatorioID)
	}
	var versao int
	_ = s.DB.QueryRow(`SELECT COALESCE(MAX(versao), 0) + 1 FROM especialista_sinteses WHERE patch = ? AND elo = ? AND funcao = ?`, r.Patch, r.Elo, r.Funcao).Scan(&versao)
	res, err := s.DB.Exec(`INSERT INTO especialista_sinteses (patch, elo, funcao, versao, estado, criado_em, implementacao, versao_prompt, relatorios,
		partidas, jogadores) VALUES (?, ?, ?, ?, 'rodando', ?, ?, ?, ?, ?, ?)`, r.Patch, r.Elo, r.Funcao, versao, banco.Agora(), est.Implementacao,
		VersaoPromptEspecialista, jsonOuVazio(ids, "[]"), d.partidas, d.jogadores)
	if err != nil {
		s.libera(chave, e)
		e.Fecha()
		return Sintese{}, nil, err
	}
	id, _ := res.LastInsertId()
	ctxTrab, cancela := context.WithCancel(context.Background())
	e.AoCancelar(cancela)
	go func() {
		defer cancela()
		defer s.libera(chave, e)
		defer e.Fecha()
		s.rodaSintese(ctxTrab, e, id, d, est)
	}()
	x, _, err := LeSintese(s.DB, r, versao)
	return x, e, err
}

func (s *Servico) rodaSintese(ctx context.Context, e *web.Execucao, id int64, d dadosSintese, est Estado) {
	termina := func(estado, erro string, c *ConteudoSintese, bruto string, avisos []string, env Enviado, uso Resposta, arquivo string) {
		cj := ""
		if c != nil {
			cj = jsonOuVazio(c, "")
		}
		if _, err := s.DB.Exec(`UPDATE especialista_sinteses SET estado = ?, erro = ?, terminado_em = ?, modelo = ?, conteudo = ?, bruto = ?,
			avisos = ?, enviado = ?, arquivo = ?, duracao_ms = ?, caracteres_entrada = ?, caracteres_saida = ?, tokens_entrada = ?, tokens_saida = ?,
			custo_usd = ? WHERE id = ?`, estado, erro, banco.Agora(), uso.Modelo, cj, bruto, jsonOuVazio(web.NuncaNil(avisos), "[]"), jsonOuVazio(env, "{}"),
			arquivo, uso.DuracaoMs, uso.CaracteresEntrada, uso.CaracteresSaida, uso.TokensEntrada, uso.TokensSaida, uso.CustoUSD, id); err != nil {
			log.Printf("especialista: gravar a síntese %d: %v", id, err)
		}
	}
	p, env := s.pedidoSintese(d)
	e.Emite("contexto", map[string]any{"enviado": env, "modelo": est.Modelos[TipoAnalise], "amostra": d.amostra})
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
		log.Printf("especialista: síntese %d: %v (%v)", id, err, errors.Unwrap(err))
		termina(estado, err.Error(), nil, uso.Texto, nil, env, uso, "")
		if estado == "cancelado" {
			e.Emite("cancelado", map[string]any{"id": id})
			return
		}
		e.Falha(err.Error())
		return
	}
	c, avisos, err := ValidaSintese(uso.Texto, d.fontes, d.comp)
	if err != nil {
		termina("falhou", err.Error(), nil, uso.Texto, avisos, env, uso, "")
		e.Falha(err.Error() + " — a resposta crua ficou guardada; refaça a síntese")
		return
	}
	c.Amostra, c.Contagem = d.amostra, d.contagem
	x, _ := leSinteseLinhaPorID(s.DB, id)
	x.Modelo = uso.Modelo
	x.Conteudo = &c
	arquivo := d.r.ArquivoSintese()
	if err := s.escreveConhecimento(arquivo, MarkdownSintese(x)); err != nil {
		avisos = append(avisos, "não deu para gravar o arquivo de conhecimento: "+err.Error())
		arquivo = ""
	}
	termina("concluido", "", &c, uso.Texto, avisos, env, uso, arquivo)
	if err := s.escreveIndice(); err != nil {
		log.Printf("especialista: índice: %v", err)
	}
	x, _, _ = LeSintese(s.DB, d.r, 0)
	e.Emite("concluido", map[string]any{"sintese": x})
}

func leSinteseLinhaPorID(db *banco.DB, id int64) (Sintese, error) {
	return leSinteseLinha(db.QueryRow(`SELECT `+colunasSintese+` FROM especialista_sinteses WHERE id = ?`, id))
}

// --- Markdown ------------------------------------------------------------------------------

// TituloSintese: o título padronizado.
func TituloSintese(r Recorte) string { return r.Nome() + " · SÍNTESE" }

// MarkdownSintese: o arquivo de conhecimento da voz especialista_lane.
func MarkdownSintese(x Sintese) string {
	c := x.Conteudo
	if c == nil {
		return ""
	}
	r := Recorte{Patch: x.Patch, Elo: x.Elo, Funcao: x.Funcao}
	var b strings.Builder
	fmt.Fprintf(&b, "# Especialista Lane · %s\n\n", r.Nome())
	fmt.Fprintf(&b, "<!-- lane: tipo=sintese patch=%s elo=%s rota=%s versao=%d partidas=%d jogadores=%d relatorios=%d -->\n\n",
		x.Patch, x.Elo, x.Funcao, x.Versao, x.Partidas, x.Jogadores, len(x.Relatorios))
	fmt.Fprintf(&b, "**Amostra:** %s.\n\n", c.Amostra)
	fmt.Fprintf(&b, "Síntese versão %d (%s, modelo %s). Como ler: \"em N jogadores\" = a regra apareceu em relatórios de N jogadores "+
		"diferentes (contado pelo app); \"caso único\" = HIPÓTESE tirada de um jogador só. Nada aqui vale como lei com amostra pequena.\n\n",
		x.Versao, x.CriadoEm, x.Modelo)
	if c.Resumo != "" {
		b.WriteString("## Resumo\n\n" + c.Resumo + "\n\n")
	}
	b.WriteString("## Papel e estilo\n\n")
	fmt.Fprintf(&b, "Contagem do app: %s.\n\n", textoContagem(c.Contagem))
	if c.PapelEstilo != "" {
		b.WriteString(c.PapelEstilo + "\n\n")
	}
	codigos := map[string]FonteSintese{}
	for _, f := range c.Fontes {
		codigos[f.Codigo] = f
	}
	onde := func(x RegraSintese) string {
		var ps []string
		for _, cod := range x.Relatorios {
			f := codigos[cod]
			ps = append(ps, fmt.Sprintf("%s, partida %d (%s)", f.Campeao, f.GameID, cod))
		}
		return strings.Join(ps, "; ")
	}
	b.WriteString("## Regras que se repetem (mais de um jogador)\n\n")
	if len(c.Regras) == 0 {
		fmt.Fprintf(&b, "Nenhuma ainda: com %s, nenhuma regra apareceu em dois jogadores diferentes.\n\n", plural(x.Jogadores, "jogador", "jogadores"))
	}
	for _, rg := range c.Regras {
		fmt.Fprintf(&b, "- %s — em %d jogadores, %s: %s\n", rg.Texto, rg.Jogadores, plural(len(rg.Partidas), "partida", "partidas"), onde(rg))
	}
	if len(c.Regras) > 0 {
		b.WriteString("\n")
	}
	b.WriteString("## Regras de um caso só (hipóteses)\n\n")
	if len(c.CasoUnico) == 0 {
		b.WriteString("Nenhuma.\n\n")
	}
	for _, rg := range c.CasoUnico {
		fmt.Fprintf(&b, "- HIPÓTESE: %s — 1 jogador: %s\n", rg.Texto, onde(rg))
	}
	if len(c.CasoUnico) > 0 {
		b.WriteString("\n")
	}
	if len(c.ErrosComuns) > 0 {
		b.WriteString("## Erros que aparecem nos relatórios\n\n")
		for _, e := range c.ErrosComuns {
			fmt.Fprintf(&b, "- %s — %s jogador(es): %s\n", e.Texto, fmt.Sprint(e.Jogadores), onde(e))
		}
		b.WriteString("\n")
	}
	b.WriteString("## Comparação com outro patch\n\n")
	b.WriteString(c.Comparacao.Texto + "\n")
	if len(c.MudancasPatch) > 0 {
		b.WriteString("\nMudanças (amostra suficiente):\n")
		for _, m := range c.MudancasPatch {
			fmt.Fprintf(&b, "- %s (%s)\n", m.Texto, m.Base)
		}
	}
	b.WriteString("\n")
	if len(c.OQueFalta) > 0 {
		b.WriteString("## O que a amostra ainda não diz\n\n")
		for _, o := range c.OQueFalta {
			b.WriteString("- " + o + "\n")
		}
		b.WriteString("\n")
	}
	b.WriteString("## Relatórios usados\n\n")
	for _, f := range c.Fontes {
		fmt.Fprintf(&b, "- %s: %s, partida %d, participante %d, %s, relatório v%d (%s)\n", f.Codigo, f.Campeao, f.GameID, f.ParticipanteID,
			map[bool]string{true: "vitória", false: "derrota"}[f.Venceu], f.Versao, f.Arquivo)
	}
	return b.String()
}

// escreveIndice refaz especialista-lane/indice.md com a última síntese
// concluída de cada recorte (só para quem lê; a voz não manda o índice).
func (s *Servico) escreveIndice() error {
	linhas, err := s.DB.Query(`SELECT patch, elo, funcao, MAX(versao) FROM especialista_sinteses WHERE estado = 'concluido' GROUP BY patch, elo, funcao`)
	if err != nil {
		return err
	}
	var rs []Recorte
	versoes := map[Recorte]int{}
	for linhas.Next() {
		var r Recorte
		var v int
		if err := linhas.Scan(&r.Patch, &r.Elo, &r.Funcao, &v); err != nil {
			linhas.Close()
			return err
		}
		rs = append(rs, r)
		versoes[r] = v
	}
	linhas.Close()
	sort.Slice(rs, func(i, j int) bool {
		if c := ComparaPatch(rs[i].Patch, rs[j].Patch); c != 0 {
			return c > 0
		}
		return rs[i].Elo+rs[i].Funcao < rs[j].Elo+rs[j].Funcao
	})
	var b strings.Builder
	b.WriteString("# Especialista Lane · índice\n\nAs sínteses por patch, elo e rota (a última versão concluída de cada uma). " +
		"Os relatórios de referência ficam em `<patch>/relatorios/`. Escrito pelo app a cada síntese.\n\n")
	b.WriteString("| patch | elo | rota | amostra | arquivo |\n|---|---|---|---|---|\n")
	for _, r := range rs {
		x, ok, err := LeSintese(s.DB, r, versoes[r])
		if err != nil || !ok || x.Conteudo == nil {
			continue
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n", r.Patch, NomeElo(r.Elo), nomeRota(r.Funcao), strings.ReplaceAll(x.Conteudo.Amostra, "|", "/"),
			strings.TrimPrefix(r.ArquivoSintese(), PastaEspecialista+"/"))
	}
	return s.escreveConhecimento(PastaEspecialista+"/indice.md", b.String())
}
