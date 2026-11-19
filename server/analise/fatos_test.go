package analise

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"loltracking/server/arquivo"
	"loltracking/server/campeao"
)

func TestDifEmEViradaDeOuro(t *testing.T) {
	c := []PontoOuro{{TMs: 0, Diferenca: 0}, {TMs: 60000, Diferenca: 1200}, {TMs: 120000, Diferenca: -1500}, {TMs: 180000, Diferenca: 2000}}
	if d, ok := difEm(c, 30000); !ok || d != 600 {
		t.Fatalf("interpolação: %d %v", d, ok)
	}
	if d, _ := difEm(c, 500000); d != 2000 {
		t.Fatalf("depois do último quadro vale o último: %d", d)
	}
	az, tAz, ve, tVe, vir := viradaDeOuro(c)
	if az != 2000 || tAz != 180000 || ve != 1500 || tVe != 120000 || vir != 2 {
		t.Fatalf("virada: %d %d %d %d %d", az, tAz, ve, tVe, vir)
	}
	if _, ok := difEm(nil, 1000); ok {
		t.Fatal("sem quadros não há saldo")
	}
}

func TestMontaComLimiteCortaNaOrdem(t *testing.T) {
	longa := strings.Repeat("x", 300)
	secoes := []secao{
		{titulo: "", linhas: []string{"# cabeça"}},
		{titulo: "primeira", linhas: []string{longa, longa}},
		{titulo: "segunda", linhas: []string{longa, longa, longa}},
		{titulo: "terceira", linhas: []string{"curta"}},
	}
	texto, cortadas := montaComLimite(secoes, 1600)
	if n := utf8.RuneCountInString(texto); n > 1600 {
		t.Fatalf("passou do limite: %d", n)
	}
	if !strings.Contains(texto, "## primeira") || !strings.Contains(texto, "## segunda") {
		t.Fatalf("as seções de maior prioridade entram primeiro:\n%s", texto)
	}
	if len(cortadas) != 2 || cortadas[0] != "segunda (em parte)" || cortadas[1] != "terceira" {
		t.Fatalf("cortadas: %v", cortadas)
	}
	if !strings.Contains(texto, "linha(s) cortada(s)") || !strings.Contains(texto, "Fora do pacote") {
		t.Fatalf("o corte tem de aparecer no texto:\n%s", texto)
	}
	if _, c := montaComLimite(secoes, 100000); len(c) != 0 {
		t.Fatalf("com folga nada é cortado: %v", c)
	}
}

func TestEstadoAcampamentosPelaLimpeza(t *testing.T) {
	f := &fatos{}
	f.selva, _ = TemposDaSelva("16.22")
	// Os lobos do lado azul limpos aos 2:00 (voltam aos 4:15); o resto nunca visto.
	lobos := -1
	for k, a := range Acampamentos {
		if a.Nome == "lobos (lado azul)" {
			lobos = k
		}
	}
	ls := []limpeza{{acamp: lobos, t: 120000, quem: 2}}
	es := f.estadoAcampamentos(100, 150000, 200000, ls)
	if len(es) != 6 {
		t.Fatalf("o lado azul tem 6 acampamentos: %d", len(es))
	}
	for _, e := range es {
		switch {
		case e.Nome == "lobos (lado azul)":
			if e.Estado != "morto" || e.NasceMs != 255000 || e.UltimaLimpezaMs == nil || e.Por != 2 {
				t.Fatalf("lobos: %+v", e)
			}
		case e.Estado != "vivo":
			t.Fatalf("sem limpeza vista e depois do primeiro nascimento, vivo (estimado): %+v", e)
		}
	}
	// O azul nasce aos 0:55 no 26.x e aos 1:30 no 25.x: a mesma janela vê
	// estados diferentes conforme o patch da partida.
	if es := f.estadoAcampamentos(100, 30000, 60000, nil); es[0].Nome != "azul (lado azul)" || es[0].Estado != "nasce_na_janela" || es[0].NasceMs != 55000 {
		t.Fatalf("antes do primeiro nascimento (26.x): %+v", es[0])
	}
	if es := f.estadoAcampamentos(100, 60000, 100000, nil); es[0].Estado != "vivo" {
		t.Fatalf("depois do 0:55 (26.x): %+v", es[0])
	}
	f.selva, _ = TemposDaSelva("15.20")
	if es := f.estadoAcampamentos(100, 60000, 100000, nil); es[0].Estado != "nasce_na_janela" || es[0].NasceMs != 90000 {
		t.Fatalf("antes do 1:30 (25.x): %+v", es[0])
	}
}

// A base de fatos sai da análise, a busca e o pacote de contexto leem só o
// banco (os arquivos da partida podem nem existir mais), e a partida
// analisada antes da base ganha o resumo no primeiro pedido.
func TestBaseDeFatosRotasEBusca(t *testing.T) {
	a := preparaRastreada(t, &riotFalsa{})
	a.d.Estaticos = campeao.EstaticosDoTeste()
	a.mux = novoMux(a.d)
	if _, err := Recalcula(context.Background(), a.d, partidaTeste); err != nil {
		t.Fatal(err)
	}
	var n int
	a.db.QueryRow(`SELECT COUNT(*) FROM resumos_jogador WHERE game_id = ?`, partidaTeste).Scan(&n)
	if n != 10 {
		t.Fatalf("um resumo por jogador: %d", n)
	}
	cod, m := a.pede(t, "GET", "/api/partidas/3304789000/resumo")
	if cod != 200 {
		t.Fatalf("%d %v", cod, m)
	}
	res := m["resumo"].(map[string]any)
	if res["versao"].(float64) != VersaoFatos || len(res["jogadores"].([]any)) != 10 {
		t.Fatalf("%v", res)
	}
	achou := false
	for _, x := range res["momentos"].([]any) {
		mo := x.(map[string]any)
		if mo["tipo"] == "abate" && mo["participante_id"].(float64) == 3 && mo["autor"].(float64) == 8 {
			achou = true
			if !strings.Contains(mo["frase"].(string), "morreu") || mo["custo_azul"].(float64)+mo["custo_vermelho"].(float64) <= 0 {
				t.Fatalf("o abate tem frase e custo: %v", mo)
			}
		}
	}
	if !achou {
		t.Fatal("o abate do 3 pelo 8 tem de virar momento")
	}

	// Sem os arquivos da partida: busca e contexto continuam (só o banco).
	if err := os.RemoveAll(arquivo.PastaDaPartida(a.db, partidaTeste)); err != nil {
		t.Fatal(err)
	}
	cod, m = a.pede(t, "GET", "/api/resumos?tipo=abate&ate=200")
	if cod != 200 || len(m["resumos"].([]any)) != 1 {
		t.Fatalf("busca por tipo: %d %v", cod, m)
	}
	linha := m["resumos"].([]any)[0].(map[string]any)
	if linha["jogador"].(map[string]any)["participante_id"].(float64) != 3 || len(linha["momentos"].([]any)) != 1 {
		t.Fatalf("a linha é a da vítima, com o momento: %v", linha)
	}
	if cod, m = a.pede(t, "GET", "/api/resumos?tipo=abate&ate=50"); cod != 200 || len(m["resumos"].([]any)) != 0 {
		t.Fatalf("o abate é aos 100 s: %d %v", cod, m)
	}
	if cod, m = a.pede(t, "GET", "/api/resumos?resultado=vitoria&limite=200"); cod != 200 || len(m["resumos"].([]any)) != 5 {
		t.Fatalf("5 vencedores: %d %v", cod, m)
	}
	cod, m = a.pede(t, "GET", "/api/partidas/3304789000/contexto-ia?participante=3")
	if cod != 200 {
		t.Fatalf("%d %v", cod, m)
	}
	texto := m["texto"].(string)
	// O papel de cada um: do vocabulário, com os números, pesquisável.
	papeis := map[string]int{}
	for _, x := range res["jogadores"].([]any) {
		pp := x.(map[string]any)["papel"].(map[string]any)
		if RotuloPapel(pp["papel"].(string)) == "" || len(pp["motivos"].([]any)) == 0 || pp["frase"] == "" || pp["confianca"] == "" {
			t.Fatalf("papel: %v", pp)
		}
		papeis[pp["papel"].(string)]++
	}
	for p, n := range papeis {
		if cod, m := a.pede(t, "GET", "/api/resumos?limite=200&papel="+p); cod != 200 || len(m["resumos"].([]any)) != n {
			t.Fatalf("busca por papel %s: %d %v", p, cod, m)
		}
	}
	if !strings.Contains(texto, "Papel na partida (por regra)") || !strings.Contains(texto, "; papel: ") {
		t.Fatalf("o papel vai no pacote:\n%s", texto)
	}
	if !strings.Contains(texto, "# Partida 3304789000") || !strings.Contains(texto, "## Mortes de") ||
		int(m["caracteres"].(float64)) > Param.LimiteContextoIA || utf8.RuneCountInString(texto) != int(m["caracteres"].(float64)) {
		t.Fatalf("contexto:\n%s", texto)
	}
	if cod, m = a.pede(t, "GET", "/api/resumos/contexto-ia?funcao=MIDDLE"); cod != 200 || !strings.Contains(m["texto"].(string), "partida 3304789000") {
		t.Fatalf("contexto de várias: %d %v", cod, m)
	}
	for _, c := range []string{"/api/resumos?funcao=MEIO", "/api/resumos?resultado=empate", "/api/resumos?tipo=gank",
		"/api/resumos?limite=0", "/api/resumos?papel=heroi", "/api/partidas/3304789000/contexto-ia", "/api/partidas/3304789000/contexto-ia?participante=11"} {
		if cod, _ := a.pede(t, "GET", c); cod != 400 {
			t.Fatalf("%s devia ser 400: %d", c, cod)
		}
	}
	if cod, _ := a.pede(t, "GET", "/api/partidas/42/resumo"); cod != 404 {
		t.Fatalf("partida desconhecida: %d", cod)
	}
}

func TestBaseDeFatosPreenchimento(t *testing.T) {
	a := preparaRastreada(t, &riotFalsa{})
	if _, err := Recalcula(context.Background(), a.d, partidaTeste); err != nil {
		t.Fatal(err)
	}
	// Como uma partida analisada antes da base de fatos existir.
	for _, tb := range []string{"resumos_partida", "resumos_jogador", "momentos_chave"} {
		if _, err := a.db.Exec(`DELETE FROM ` + tb); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := PreencheFatos(context.Background(), a.d); err != nil || n != 1 {
		t.Fatalf("preencheu %d: %v", n, err)
	}
	if n, _ := PreencheFatos(context.Background(), a.d); n != 0 {
		t.Fatalf("a segunda vez não tem o que preencher: %d", n)
	}
	// Recalculável: refazer dá as mesmas linhas.
	foto := func() string {
		var b strings.Builder
		for _, q := range []string{`SELECT texto || dados FROM resumos_partida`,
			`SELECT participante_id || nota || etiquetas || dados FROM resumos_jogador ORDER BY participante_id`,
			`SELECT n || tipo || impacto || tags || frase || dados FROM momentos_chave ORDER BY n`} {
			linhas, err := a.db.Query(q)
			if err != nil {
				t.Fatal(err)
			}
			for linhas.Next() {
				var s string
				linhas.Scan(&s)
				b.WriteString(s + "\n")
			}
			linhas.Close()
		}
		return b.String()
	}
	antes := foto()
	if _, err := Recalcula(context.Background(), a.d, partidaTeste); err != nil {
		t.Fatal(err)
	}
	if foto() != antes {
		t.Fatal("a base de fatos não é idempotente")
	}
	// Regra antiga: refeita no primeiro pedido.
	a.db.Exec(`UPDATE resumos_partida SET versao = 0`)
	if cod, m := a.pede(t, "GET", "/api/partidas/3304789000/resumo"); cod != 200 || m["resumo"].(map[string]any)["versao"].(float64) != VersaoFatos {
		t.Fatalf("%d %v", cod, m)
	}
	// Nunca analisada: 409.
	a.db.Exec(`DELETE FROM analises`)
	a.db.Exec(`DELETE FROM resumos_partida`)
	if cod, _ := a.pede(t, "GET", "/api/partidas/3304789000/resumo"); cod != 409 {
		t.Fatalf("sem análise devia ser 409: %d", cod)
	}
}

func novoMux(d Deps) *http.ServeMux {
	mux := http.NewServeMux()
	Registra(mux, d)
	return mux
}
