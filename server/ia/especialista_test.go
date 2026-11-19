package ia

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"loltracking/server/analise"
	"loltracking/server/banco"
	"loltracking/server/web"
)

// fontesEsp: a partida da Vi (do usuário, fontesFalsas) e duas partidas da
// coleta: o Kayn de Platina II (3305481832, participante 7) e um Viego de
// Platina I (3305486988, participante 2), ambos na selva, patch 16.23.
type fontesEsp struct {
	fontesFalsas
	// porPatch: jogadores de referência a mais por patch (comparação).
	extras []analise.IndicadoresJogador
}

const jogoKayn, jogoViego = 3305481832, 3305486988

func refKayn() analise.IndicadoresJogador {
	d := 300
	return analise.IndicadoresJogador{GameID: jogoKayn, ParticipanteID: 7, Puuid: "kayn", Nome: "JogadorKayn", Campeao: "Kayn", Funcao: "JUNGLE",
		Elo: "PLATINUM II", EloPartida: "PLATINUM", Patch: "16.23", Venceu: true, DuracaoS: 2280, Rastreado: true, RastreadosNaPartida: 1,
		Abates: 19, Mortes: 7, Assistencias: 12, KDA: 4.4, DifOuro15: &d, ObjetivosParticipou: 5, ObjetivosTime: 7, Sentinelas: 9,
		Papel: analise.PapelJogador{Papel: "carregou", Rotulo: "carregou", Estilo: "invasao", EstiloRotulo: "invasão", Frase: "carregou"}}
}

func refViego() analise.IndicadoresJogador {
	x := refKayn()
	x.GameID, x.ParticipanteID, x.Puuid, x.Nome, x.Campeao, x.Elo = jogoViego, 2, "viego", "JogadorViego", "Viego", "PLATINUM I"
	x.RastreadosNaPartida = 10
	return x
}

func (f fontesEsp) Partida(ctx context.Context, id int64) (InfoPartida, error) {
	if id != jogoKayn && id != jogoViego {
		return f.fontesFalsas.Partida(ctx, id)
	}
	info, _ := f.fontesFalsas.Partida(ctx, jogoTeste)
	info.GameID, info.Patch, info.DuracaoS = id, "16.23", 2280
	info.Participantes[6].Campeao, info.Participantes[6].Nome, info.Participantes[6].Puuid = "Kayn", "JogadorKayn", "kayn"
	info.Participantes[1].Campeao, info.Participantes[1].Nome, info.Participantes[1].Puuid = "Viego", "JogadorViego", "viego"
	info.Momentos = []analise.MomentoChave{
		{N: 1, TMs: 300000, Tipo: "abate", ParticipanteID: 3, Autor: 7, Envolvidos: []int{3, 7}, Impacto: 700, Frase: "Kayn matou Ahri."},
		{N: 2, TMs: 840000, Tipo: "objetivo", Subtipo: "dragão infernal", Time: 200, Autor: 7, Envolvidos: []int{7, 8}, Impacto: 900, Frase: "Dragão do vermelho."},
		{N: 3, TMs: 1500000, Tipo: "abate", ParticipanteID: 7, Autor: 2, Envolvidos: []int{7, 2}, Impacto: 500, Frase: "Kayn morreu para Lee Sin."},
		{N: 4, TMs: 2000000, Tipo: "janela_aproveitada", ParticipanteID: 7, Impacto: 200, Frase: "Kayn invadiu."},
	}
	return info, nil
}

func (f fontesEsp) Indicadores(id int64, pid int) (analise.IndicadoresJogador, error) {
	switch {
	case id == jogoKayn && pid == 7:
		return refKayn(), nil
	case id == jogoViego && pid == 2:
		return refViego(), nil
	case id == jogoKayn || id == jogoViego:
		// Os outros 9 da partida da coleta: sem elo (não são referência).
		return analise.IndicadoresJogador{GameID: id, ParticipanteID: pid, Campeao: "Ahri", Funcao: "MIDDLE", Patch: "16.23"}, nil
	}
	return f.fontesFalsas.Indicadores(id, pid)
}

func (f fontesEsp) Referencias(fl analise.FiltroReferencia) ([]analise.IndicadoresJogador, error) {
	if fl.ForaDaColeta {
		if fl.Funcao != "JUNGLE" {
			return nil, nil
		}
		vi, _ := f.fontesFalsas.Indicadores(jogoTeste, 7)
		return []analise.IndicadoresJogador{vi}, nil
	}
	todas := append([]analise.IndicadoresJogador{refKayn(), refViego()}, f.extras...)
	var r []analise.IndicadoresJogador
	for _, x := range todas {
		if fl.Funcao != "" && x.Funcao != fl.Funcao {
			continue
		}
		if fl.Elo != "" && TierDe(x.Elo) != fl.Elo {
			continue
		}
		r = append(r, x)
	}
	return r, nil
}

func preparaEsp(t *testing.T, f fontesEsp) (ambienteTeste, *CadernoFalso, string) {
	t.Helper()
	db, err := banco.AbreMemoria(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := Migra(db); err != nil {
		t.Fatal(err)
	}
	fa := &Falso{}
	pasta := t.TempDir()
	s := NovoServico(db, fa, f, web.NovoHub(), pasta)
	cad := &CadernoFalso{}
	s.Caderno = cad
	mux := http.NewServeMux()
	web.RegistraExecucoes(mux, s.Hub)
	Registra(mux, s)
	return ambienteTeste{s: s, mux: mux, f: fa, db: db}, cad, pasta
}

func esperaLivre(t *testing.T, s *Servico, chave string) {
	t.Helper()
	for i := 0; i < 400; i++ {
		if s.execucaoDe(chave) == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("a execução %s não terminou", chave)
}

func TestEspecialistaRelatorioSinteseVozEspelho(t *testing.T) {
	a, cad, pasta := preparaEsp(t, fontesEsp{})

	// Não-referência: 400 (os outros 9 da partida da coleta).
	if c, m := a.pede(t, "POST", "/api/especialista/relatorios/3305481832/3", nil); c != 400 {
		t.Fatalf("participante sem elo: %d %v", c, m)
	}
	// Síntese sem relatório: 409.
	if c, _ := a.pede(t, "POST", "/api/especialista/sintese", map[string]any{"patch": "16.23", "elo": "PLATINUM", "rota": "JUNGLE"}); c != 409 {
		t.Fatalf("síntese sem relatório: %d", c)
	}
	// A prévia não chama a IA e avisa do 1 rastreado.
	c, m := a.pede(t, "GET", "/api/especialista/relatorios/3305481832/7/previa", nil)
	if c != 200 || a.f.Chamadas != 0 {
		t.Fatalf("prévia: %d, chamadas %d", c, a.f.Chamadas)
	}
	msg := m["mensagem"].(string)
	for _, q := range []string{"JOGADOR DE REFERÊNCIA: participante 7", "RASTREADOS: 1 de 10", "### L1", "Objetivos (base de fatos)", "usuário 1: Vi"} {
		if !strings.Contains(msg, q) {
			t.Fatalf("a prévia não tem %q:\n%s", q, msg)
		}
	}

	// Gera os dois relatórios.
	for _, ref := range [][2]int64{{jogoKayn, 7}, {jogoViego, 2}} {
		c, m := a.pede(t, "POST", fmt.Sprintf("/api/especialista/relatorios/%d/%d", ref[0], ref[1]), nil)
		if c != 202 {
			t.Fatalf("gerar: %d %v", c, m)
		}
		esperaLivre(t, a.s, chaveRelEsp(ref[0], int(ref[1])))
	}
	r, ok, err := LeRelatorioReferencia(a.db, jogoKayn, 7, 0)
	if err != nil || !ok || r.Estado != "concluido" || r.Conteudo == nil {
		t.Fatalf("relatório: %+v %v", r, err)
	}
	if len(r.Conteudo.Decisoes) != 3 || r.Conteudo.Decisoes[0].Hipotese || !r.Conteudo.Decisoes[1].Hipotese {
		t.Fatalf("decisões/hipótese: %+v", r.Conteudo.Decisoes)
	}
	if r.Conteudo.Decisoes[0].Quadro == nil || len(r.Conteudo.NaoFez) != 1 || r.Conteudo.Quem.Rastreados != 1 {
		t.Fatalf("quadro/não fez/quem: %+v", r.Conteudo)
	}
	if !strings.HasPrefix(r.Conteudo.NaoDaParaSaber[0], "Só 1 de 10 rastreados") {
		t.Fatalf("o que falta: %v", r.Conteudo.NaoDaParaSaber)
	}
	md, err := os.ReadFile(filepath.Join(pasta, filepath.FromSlash(r.Arquivo)))
	if err != nil || !strings.Contains(string(md), "HIPÓTESE: um caso só") || strings.Contains(string(md), "JogadorKayn") {
		t.Fatalf("markdown (%s): %v\n%s", r.Arquivo, err, md)
	}
	if r.Arquivo != "especialista-lane/16.23/relatorios/platinum-jungle-kayn-3305481832-p7.md" {
		t.Fatalf("arquivo: %s", r.Arquivo)
	}

	// Painel: 1 recorte com 2 partidas, 2 relatórios, sem síntese.
	c, m = a.pede(t, "GET", "/api/especialista", nil)
	rec := m["recortes"].([]any)
	if c != 200 || len(rec) != 1 || rec[0].(map[string]any)["relatorios"].(float64) != 2 || rec[0].(map[string]any)["sintese"] != nil {
		t.Fatalf("painel: %d %v", c, m)
	}

	// Síntese: a regra citada pelos dois é repetida (2 jogadores); a do R1 é caso único;
	// a mudança entre patches é descartada (sem outro patch).
	if c, m := a.pede(t, "POST", "/api/especialista/sintese", map[string]any{"patch": "16.23", "elo": "PLATINUM", "rota": "JUNGLE"}); c != 202 {
		t.Fatalf("síntese: %d %v", c, m)
	}
	rc := Recorte{Patch: "16.23", Elo: "PLATINUM", Funcao: "JUNGLE"}
	esperaLivre(t, a.s, chaveSintese(rc))
	sv, ok, err := LeSintese(a.db, rc, 0)
	if err != nil || !ok || sv.Estado != "concluido" || sv.Conteudo == nil {
		t.Fatalf("síntese: %+v %v", sv, err)
	}
	if len(sv.Conteudo.Regras) != 1 || sv.Conteudo.Regras[0].Jogadores != 2 || len(sv.Conteudo.CasoUnico) != 1 || len(sv.Conteudo.MudancasPatch) != 0 {
		t.Fatalf("regras: %+v", sv.Conteudo)
	}
	arq, err := os.ReadFile(filepath.Join(pasta, "especialista-lane", "16.23", "platinum-jungle.md"))
	if err != nil || !strings.Contains(string(arq), "em 2 jogadores") || !strings.Contains(string(arq), "partidas=2 jogadores=2 relatorios=2") {
		t.Fatalf("arquivo da síntese: %v\n%s", err, arq)
	}
	if _, err := os.Stat(filepath.Join(pasta, "especialista-lane", "indice.md")); err != nil {
		t.Fatal("sem índice")
	}

	// A voz na análise da Vi (patch 16.22, Platina pelo elo da partida): usa a
	// síntese do 16.23 e avisa.
	info, _ := a.s.Fontes.Partida(context.Background(), jogoTeste)
	info.Patch = "16.22"
	foco, _ := info.Do(7)
	arqs, texto, partidas, amostra, falta := a.s.materialEspecialista(info, foco, EloComparacao{Elo: "PLATINUM"})
	if falta != "" || len(arqs) != 1 || partidas != 2 || !strings.Contains(texto, "não há síntese do patch 16.22") || !strings.Contains(amostra, "patch 16.23") {
		t.Fatalf("voz: %v %q %d %q %q", arqs, texto, partidas, amostra, falta)
	}
	if _, _, _, _, falta := a.s.materialEspecialista(info, foco, EloComparacao{Elo: "GOLD"}); !strings.Contains(falta, "sem síntese") {
		t.Fatalf("sem síntese de Ouro: %q", falta)
	}

	// Espelho: sem caderno, 409; com caderno, 3 fontes; de novo, nada; relatório
	// refeito substitui a fonte antiga.
	if c, _ := a.pede(t, "POST", "/api/especialista/espelho", nil); c != 409 {
		t.Fatalf("espelho sem caderno: %d", c)
	}
	if c, _ := a.pede(t, "PUT", "/api/especialista/espelho", map[string]any{"caderno": "não é uuid"}); c != 400 {
		t.Fatalf("caderno inválido: %d", c)
	}
	const cad1 = "5d1c235c-174b-4123-a1c0-d23e50093600"
	if c, _ := a.pede(t, "PUT", "/api/especialista/espelho", map[string]any{"caderno": cad1}); c != 200 {
		t.Fatalf("caderno: %d", c)
	}
	envia := func() {
		t.Helper()
		if c, m := a.pede(t, "POST", "/api/especialista/espelho", nil); c != 202 {
			t.Fatalf("espelho: %d %v", c, m)
		}
		esperaLivre(t, a.s, "espelho")
	}
	envia()
	if len(cad.Fontes) != 3 {
		t.Fatalf("fontes: %v", cad.Fontes)
	}
	for _, tt := range cad.Fontes {
		if !strings.HasPrefix(tt, "16.23 · Platina · selva") {
			t.Fatalf("título: %q", tt)
		}
	}
	for _, tx := range cad.Textos {
		if strings.Contains(tx, "JogadorKayn") || strings.Contains(tx, "JogadorViego") {
			t.Fatal("nome de invocador foi ao caderno")
		}
	}
	envia()
	if cad.Semente != 3 {
		t.Fatalf("reenviou sem mudança: %d", cad.Semente)
	}
	if c, _ := a.pede(t, "POST", "/api/especialista/relatorios/3305481832/7", nil); c != 202 {
		t.Fatal("refazer")
	}
	esperaLivre(t, a.s, chaveRelEsp(jogoKayn, 7))
	envia()
	if cad.Semente != 4 || len(cad.Removidas) != 1 || len(cad.Fontes) != 3 {
		t.Fatalf("substituição: semente %d, removidas %v, fontes %d", cad.Semente, cad.Removidas, len(cad.Fontes))
	}
	_, m = a.pede(t, "GET", "/api/especialista/espelho", nil)
	e := m["espelho"].(map[string]any)
	if e["enviados"].(float64) != 3 || e["pendentes"].(float64) != 0 || !strings.Contains(e["sai_da_maquina"].(string), "nomes de invocador") {
		t.Fatalf("estado do espelho: %v", e)
	}
	// Painel: a síntese ficou para trás? Não (o relatório refeito é do mesmo par, mas o id mudou: sim).
	_, m = a.pede(t, "GET", "/api/especialista", nil)
	if s := m["recortes"].([]any)[0].(map[string]any)["sintese"].(map[string]any); s["desatualizada"] != true {
		t.Fatalf("a síntese deveria estar desatualizada: %v", s)
	}
}

func TestEspelhoSemFerramenta(t *testing.T) {
	a, cad, _ := preparaEsp(t, fontesEsp{})
	cad.SemNLM = true
	a.s.CadernoPadrao = "5d1c235c-174b-4123-a1c0-d23e50093600"
	c, m := a.pede(t, "POST", "/api/especialista/espelho", nil)
	if c != 409 || !strings.Contains(m["erro"].(string), "nlm") {
		t.Fatalf("%d %v", c, m)
	}
	_, m = a.pede(t, "GET", "/api/especialista/espelho", nil)
	e := m["espelho"].(map[string]any)
	if e["ferramenta"] != false || !strings.Contains(e["como_exportar"].(string), "Texto copiado") || e["caderno"] != a.s.CadernoPadrao {
		t.Fatalf("%v", e)
	}
}

func TestValidaReferencia(t *testing.T) {
	info := InfoPartida{GameID: 1, DuracaoS: 600, Participantes: []Participante{{ID: 1, Campeao: "Kayn"}}}
	resp := `{"resumo": "r", "fases": [{"fase": "rotas", "plano": "p", "acoes": [{"t": "2:00", "fez": "f"}, {"t": "99:00", "fez": "x"}]},
		{"fase": "lanche"}],
		"decisoes": [{"t": "3:00", "regra": "invada sempre", "casos": ["3:00", "4:10"]}, {"t": "5:00", "regra": "se a, então b, porque c"}],
		"nao_fez": [{"texto": "x"}]}`
	c, av, err := ValidaReferencia(resp, info, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Fases) != 1 || len(c.Fases[0].Acoes) != 1 || len(c.NaoFez) != 0 {
		t.Fatalf("%+v", c)
	}
	if c.Decisoes[0].Hipotese || !c.Decisoes[1].Hipotese || len(c.Decisoes[0].Casos) != 2 {
		t.Fatalf("hipótese: %+v", c.Decisoes)
	}
	tudo := strings.Join(av, "\n")
	for _, q := range []string{"99:00", "lanche", "não está no formato", "não fez", "o pedido é de 3 a 6"} {
		if !strings.Contains(strings.ToLower(tudo), strings.ToLower(q)) && !strings.Contains(tudo, q) {
			t.Fatalf("faltou aviso %q em:\n%s", q, tudo)
		}
	}
}

func TestComparacaoPatchesAmostraMinima(t *testing.T) {
	r := Recorte{Patch: "16.23", Elo: "PLATINUM", Funcao: "JUNGLE"}
	gera := func(patch string, n int, venceu bool, base int64) []analise.IndicadoresJogador {
		var l []analise.IndicadoresJogador
		for i := 0; i < n; i++ {
			x := refKayn()
			x.GameID, x.Patch, x.Venceu, x.Mortes = base+int64(i), patch, venceu, 3+i%2
			l = append(l, x)
		}
		return l
	}
	// 9 × 9: insuficiente, nada "mudou".
	c := CalculaComparacaoPatches(append(gera("16.23", 9, true, 100), gera("16.22", 9, false, 200)...), r)
	if c.Suficiente || c.Outro != "16.22" || !strings.Contains(c.Texto, "AMOSTRA INSUFICIENTE") {
		t.Fatalf("%+v", c)
	}
	for _, l := range c.Linhas {
		if l.Leitura != "amostra insuficiente" {
			t.Fatalf("%+v", l)
		}
	}
	// 10 × 10: vitórias 100% × 0% mudou; mortes iguais não.
	c = CalculaComparacaoPatches(append(gera("16.23", 10, true, 100), gera("16.22", 10, false, 200)...), r)
	if !c.Suficiente {
		t.Fatalf("%+v", c)
	}
	leitura := map[string]string{}
	for _, l := range c.Linhas {
		leitura[l.Indicador] = l.Leitura
	}
	if leitura["vitórias (%)"] != "mudou" || leitura["mortes na fase de rotas"] != "sem mudança clara" {
		t.Fatalf("%v", leitura)
	}
	// Mudança da IA com amostra insuficiente é descartada.
	fontes := []FonteSintese{{Codigo: "R1", GameID: 1, puuid: "a"}}
	cs, av, err := ValidaSintese(`{"resumo": "x", "regras": [{"regra": "se a, então b, porque c", "relatorios": ["R1", "R9"]}],
		"mudancas_patch": [{"texto": "mudou tudo"}]}`, fontes, ComparacaoPatches{})
	if err != nil || len(cs.MudancasPatch) != 0 || len(cs.CasoUnico) != 1 || !strings.Contains(strings.Join(av, "|"), "R9") {
		t.Fatalf("%+v %v %v", cs, av, err)
	}
}

func TestComparaPatchETier(t *testing.T) {
	if ComparaPatch("16.9", "16.10") != -1 || ComparaPatch("16.23", "16.23") != 0 || TierDe("platinum ii") != "PLATINUM" {
		t.Fatal("patch/tier")
	}
	if _, err := validaRecorte(Recorte{Patch: "16.23", Elo: "PLATINUM II", Funcao: "jungle"}); err != nil {
		t.Fatal(err)
	}
	if _, err := validaRecorte(Recorte{Patch: "x", Elo: "PLATINUM", Funcao: "JUNGLE"}); err == nil {
		t.Fatal("patch inválido passou")
	}
	m := metaDoArquivo("# x\n<!-- lane: tipo=sintese partidas=3 jogadores=2 -->\n")
	if m["partidas"] != "3" || m["jogadores"] != "2" {
		t.Fatalf("%v", m)
	}
	var _ = json.Marshal
}
