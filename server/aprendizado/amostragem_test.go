package aprendizado

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"loltracking/server/riot"
)

// riotPaginado é a Riot falsa com o Ouro II em 9 páginas de 10, como depois
// de uma troca de patch: nas primeiras páginas ninguém tem partida no patch
// instalado (contas que o cliente não acha e os de referência só com patch
// antigo, n = 4, 9, 14); só nas páginas do meio (5 e 7) há quem tenha. As
// outras divisões do Ouro vêm vazias; os outros elos são os da RiotFalso.
type riotPaginado struct {
	*RiotFalso
	mu      sync.Mutex
	paginas map[int][]riot.EntradaLiga
	pedidas []string
}

const paginasOuroII = 9

func novoRiotPaginado() *riotPaginado {
	r := &riotPaginado{RiotFalso: NovoRiotFalso(), paginas: map[int][]riot.EntradaLiga{}}
	ref := func(n int) riot.EntradaLiga {
		// Os de referência com sequência de vitórias: a amostragem os põe na frente da página.
		return riot.EntradaLiga{Puuid: fmt.Sprintf("riot-ref-1-%d", n), Tier: "GOLD", Rank: "II", LP: 50, Vitorias: 40, Derrotas: 30, HotStreak: true}
	}
	for p := 1; p <= paginasOuroII; p++ {
		var l []riot.EntradaLiga
		for i := 0; i < 10; i++ {
			l = append(l, riot.EntradaLiga{Puuid: fmt.Sprintf("riot-sumido-%d-%d", p, i), Tier: "GOLD", Rank: "II", LP: 60, Vitorias: 40, Derrotas: 30})
		}
		switch p {
		case 2:
			l[0], l[1], l[2] = ref(4), ref(9), ref(14)
		case 5:
			l[6], l[7], l[8], l[9] = ref(0), ref(1), ref(2), ref(3)
		case 7:
			l[6], l[7], l[8], l[9] = ref(5), ref(6), ref(7), ref(8)
		}
		r.paginas[p] = l
	}
	return r
}

func (r *riotPaginado) Liga(ctx context.Context, reg riot.Regiao, elo, divisao string, pagina int) ([]riot.EntradaLiga, error) {
	if elo != "GOLD" {
		return r.RiotFalso.Liga(ctx, reg, elo, divisao, pagina)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pedidas = append(r.pedidas, fmt.Sprintf("%s/%d", divisao, pagina))
	if divisao != "II" {
		return []riot.EntradaLiga{}, nil
	}
	return append([]riot.EntradaLiga(nil), r.paginas[pagina]...), nil
}

func preparaPaginado(t *testing.T) (*amb, *riotPaginado) {
	t.Helper()
	a := prepara(t, nil)
	rp := novoRiotPaginado()
	a.k.d.Riot = rp
	return a, rp
}

// paginasDosCandidatos: as páginas na ordem em que os candidatos foram conferidos.
func (a *amb) paginasDosCandidatos(t *testing.T, id int64) []int {
	t.Helper()
	exID, _ := a.k.ExecucaoDe(id)
	ex, _ := a.hub.Busca(exID)
	var l []int
	for _, ev := range ex.Eventos() {
		if ev.Tipo != "candidato" {
			continue
		}
		var x struct {
			Pagina int `json:"pagina"`
		}
		json.Unmarshal(ev.Dados, &x)
		if len(l) == 0 || l[len(l)-1] != x.Pagina {
			l = append(l, x.Pagina)
		}
	}
	return l
}

func (a *amb) coleta(t *testing.T, p Pedido) Detalhe {
	t.Helper()
	col, _, err := a.k.Cria(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	a.k.Espera()
	return a.detalhe(t, col.ID)
}

func TestAmostragemAchaOndeANormalNaoAcha(t *testing.T) {
	a, rp := preparaPaginado(t)
	// A descoberta normal lê as 2 primeiras páginas de cada divisão: ninguém com partida no patch.
	d := a.coleta(t, Pedido{Elos: []string{"GOLD"}, SoDescobrir: true})
	ouro := eloDe(d, "GOLD")
	if d.Coleta.Estado != Concluido || ouro.Jogadores != 0 || ouro.Descartes[DescSemPartida] != 3 || ouro.PaginasVistas != 2 ||
		!strings.Contains(ouro.Aviso, "achou 0 de 10") {
		t.Fatalf("normal %+v %+v", d.Coleta, ouro)
	}

	// Por amostragem: páginas do meio, como numa busca binária (5, 2, 7, 1, 3: no máximo 5 por divisão).
	rp.pedidas = nil
	d = a.coleta(t, Pedido{Elos: []string{"GOLD"}, SoDescobrir: true, Amostragem: true})
	ouro = eloDe(d, "GOLD")
	if !d.Coleta.Amostragem || d.Coleta.JogadoresPorElo != 10 || ouro.Jogadores != 8 || ouro.CandidatosVistos != 50 ||
		ouro.PaginasVistas != 5 || ouro.Descartes[DescSemPartida] != 3 || ouro.Descartes["nao_achado"] != 39 ||
		!strings.Contains(ouro.Aviso, "por amostragem: achou 8 de 10") {
		t.Fatalf("amostragem %+v %+v", d.Coleta, ouro)
	}
	if ps := a.paginasDosCandidatos(t, d.Coleta.ID); !reflect.DeepEqual(ps, []int{5, 2, 7, 1, 3}) {
		t.Fatalf("ordem das páginas %v", ps)
	}
	// O número de páginas veio por busca binária (1, 2, 4, 8, 16 vazia, 12, 10, 9) e nenhuma além do fim foi
	// visitada: depois da busca, só as páginas 5, 7 e 3 eram novas; III, I e IV vazias na página 1.
	if ouro.PedidosLiga != 14 || len(rp.pedidas) != 14 {
		t.Fatalf("%d pedidos à liga: %v", ouro.PedidosLiga, rp.pedidas)
	}
	visitadas := map[string]int{}
	for _, p := range rp.pedidas {
		visitadas[p]++
		var n int
		fmt.Sscanf(p[strings.Index(p, "/")+1:], "%d", &n)
		if n > 2*paginasOuroII || visitadas[p] > 1 {
			t.Fatalf("página pedida fora da busca ou repetida: %v", rp.pedidas)
		}
	}
	// Sem os filtros de subida: o não mono (n = 3) entra; o motivo não diz "subindo".
	achou := map[string]bool{}
	for _, j := range d.Jogadores {
		achou[j.RiotID] = true
		if !strings.HasPrefix(j.Motivo, "por amostragem") || j.Estado != JogadorComPartidas {
			t.Fatalf("%+v", j)
		}
	}
	if !achou["Ref GOLD 4#BR1"] || len(d.Jogadores) != 8 {
		t.Fatalf("jogadores %v", achou)
	}
}

func TestAmostragemParaAoAcharN(t *testing.T) {
	a, _ := preparaPaginado(t)
	d := a.coleta(t, Pedido{Elos: []string{"GOLD"}, SoDescobrir: true, Amostragem: true, JogadoresPorElo: 1})
	ouro := eloDe(d, "GOLD")
	// Página do meio (5), e os de sequência de vitórias na frente: o primeiro conferido já serve.
	if ouro.Jogadores != 1 || ouro.CandidatosVistos != 1 || ouro.PaginasVistas != 1 || ouro.Aviso != "" || d.Coleta.JogadoresPorElo != 1 {
		t.Fatalf("%+v", ouro)
	}
}

func TestAmostragemRespeitaOTeto(t *testing.T) {
	antes := MaxCandidatosAmostragem
	MaxCandidatosAmostragem = 15
	defer func() { MaxCandidatosAmostragem = antes }()
	a, _ := preparaPaginado(t)
	d := a.coleta(t, Pedido{Elos: []string{"GOLD"}, SoDescobrir: true, Amostragem: true})
	ouro := eloDe(d, "GOLD")
	// Página 5 inteira (4 achados) e 5 da página 2: para nos 15, longe dos 10 pedidos.
	if ouro.CandidatosVistos != 15 || ouro.Jogadores != 4 || ouro.PaginasVistas != 2 || !strings.Contains(ouro.Aviso, "entre 15 candidatos") {
		t.Fatalf("%+v", ouro)
	}
}

func TestAmostragemRastreiaAMaisRecente(t *testing.T) {
	a, _ := preparaPaginado(t)
	d := a.coleta(t, Pedido{Elos: []string{"GOLD"}, Amostragem: true, JogadoresPorElo: 1})
	if d.Coleta.Estado != Concluido || len(d.Jogadores) != 1 || len(d.Partidas) != 1 {
		t.Fatalf("%+v %d jogadores %d partidas", d.Coleta, len(d.Jogadores), len(d.Partidas))
	}
	// O achado é o Ref GOLD 1 (dono 2 + 15×1 + 0 do LCU falso); a partida mais nova dele (a 0) é
	// ranqueada solo do patch instalado, com 25 min: é a rastreada, mesmo sendo derrota.
	p := d.Partidas[0]
	if d.Jogadores[0].RiotID != "Ref GOLD 1#BR1" || p.GameID != 3304790000-17*100000 || p.Estado != PartidaConcluida || a.jogo.Aberto() {
		t.Fatalf("%+v %+v", d.Jogadores[0], p)
	}
}

func TestAmostragemPedido(t *testing.T) {
	a := prepara(t, nil)
	for _, corpo := range []string{`{"jogadores_por_elo":21}`, `{"jogadores_por_elo":-1}`} {
		if c, m := a.pede(t, "POST", "/api/aprendizado/coletas", corpo); c != 400 || !strings.Contains(fmt.Sprint(m["erro"]), "jogadores_por_elo") {
			t.Fatal(corpo, c, m)
		}
	}
	c, m := a.pede(t, "POST", "/api/aprendizado/coletas", `{"elos":["CHALLENGER"],"so_descobrir":true,"amostragem":true,"jogadores_por_elo":2}`)
	if c != 202 {
		t.Fatal(c, m)
	}
	col := m["coleta"].(map[string]any)
	if col["amostragem"] != true || col["jogadores_por_elo"].(float64) != 2 {
		t.Fatal(col)
	}
	a.k.Espera()
	d := a.detalhe(t, int64(col["id"].(float64)))
	// Desafiante por amostragem: a liga inteira em blocos do meio para fora, sem exigir mono.
	de := eloDe(d, "CHALLENGER")
	if de.Jogadores != 2 || de.Descartes["nao_mono"] != 0 || de.PedidosLiga != 1 {
		t.Fatalf("%+v", de)
	}
}

func TestOrdemBinariaEAmostra(t *testing.T) {
	casos := map[int][]int{0: nil, 1: {1}, 2: {1, 2}, 3: {2, 1, 3}, 9: {5, 2, 7, 1, 3, 6, 8, 4, 9}}
	for n, quer := range casos {
		if got := ordemBinaria(n); !reflect.DeepEqual(got, quer) {
			t.Fatalf("%d: %v", n, got)
		}
	}
	// Todas as páginas, cada uma uma vez, nunca fora de 1..n.
	for n := 1; n <= 300; n++ {
		l := ordemBinaria(n)
		vistas := map[int]bool{}
		for _, p := range l {
			if p < 1 || p > n || vistas[p] {
				t.Fatalf("%d: %v", n, l)
			}
			vistas[p] = true
		}
		if len(l) != n {
			t.Fatal(n, len(l))
		}
	}
	es := []riot.EntradaLiga{
		{Puuid: "pouco", Vitorias: 5, Derrotas: 5},
		{Puuid: "muito", Vitorias: 100, Derrotas: 100},
		{Puuid: "parado", Inactive: true, HotStreak: true},
		{Puuid: "novo", FreshBlood: true},
		{Puuid: "quente", HotStreak: true},
		{Puuid: "veterano", Veteran: true, Vitorias: 300, Derrotas: 300},
	}
	l, inativos := ordenaAmostra(es)
	var nomes []string
	for _, e := range l {
		nomes = append(nomes, e.Puuid)
	}
	if inativos != 1 || !reflect.DeepEqual(nomes, []string{"quente", "novo", "veterano", "muito", "pouco"}) {
		t.Fatal(inativos, nomes)
	}
}
