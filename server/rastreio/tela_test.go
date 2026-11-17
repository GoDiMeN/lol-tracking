package rastreio

import (
	"context"
	"encoding/json"
	"image"
	"math"
	"strings"
	"testing"

	"loltracking/server/replay"
)

// leitorCiclico finge a tela: vida 800/1000, quadros a 50%/40%, e o Q usado a
// cada 40 leituras (pronto por 20, depois recarga descendo de 8 s).
type leitorCiclico struct{ n int }

func (l *leitorCiclico) Le(*image.RGBA, int, int) (Leitura, error) {
	l.n++
	f := func(v float64) *float64 { return &v }
	lt := Leitura{PainelPresente: true, Vida: f(800), VidaMax: f(1000), Mana: f(300), ManaMax: f(500),
		Atr: Atributos{AD: f(99), VelAtaque: f(0.79)}, CD: Recargas{W: f(RecargaNaoAprendida), D: f(0), F: f(127.5)}}
	if fase := l.n % 40; fase < 20 {
		lt.CD.Q = f(0)
	} else {
		lt.CD.Q = f(8 - float64(fase-20)*0.25)
	}
	for k := range lt.Quadros {
		lt.Quadros[k] = QuadroLateral{VidaPct: f(50), ManaPct: f(40)}
	}
	return lt, nil
}

// A tela passa pelo motor, vai para o bloco compactado e sai nas rotas; o uso
// do Q vira habilidade_usada na cadeia.
func TestColunasDaTelaNoBlocoENasRotas(t *testing.T) {
	a := prepara(t, func(j *replay.Falso, c *Config) { c.Leitor = &leitorCiclico{} })
	r, _, err := a.fila.Cria(context.Background(), partidaTeste, Pedido{Jogadores: []int{3}, TFimMs: 60000})
	if err != nil {
		t.Fatal(err)
	}
	a.fila.Espera()
	if r, _ = LeRastreio(a.db, r.ID); r.Estado != Concluido {
		t.Fatalf("rastreio %s %s", r.Estado, r.Erro)
	}
	code, m := a.pede(t, "GET", "/api/partidas/"+itoa(partidaTeste)+"/amostras?participante=3&de=20&ate=21", "")
	if code != 200 || len(m["amostras"].([]any)) == 0 {
		t.Fatal(code, m)
	}
	am := m["amostras"].([]any)[0].(map[string]any)
	cd := am["cd"].(map[string]any)
	atr := am["atributos"].(map[string]any)
	if am["vida"] != 800.0 || am["vida_max"] != 1000.0 || am["mana_max"] != 500.0 || cd["w"] != -1.0 || cd["d"] != 0.0 ||
		cd["f"] != 127.5 || cd["e"] != nil || atr["vel_ataque"] != 0.79 || atr["ad"] != 99.0 {
		t.Fatalf("amostra %v", am)
	}
	code, m = a.pede(t, "GET", "/api/partidas/"+itoa(partidaTeste)+"/estados?de=20&ate=20", "")
	j := m["quadros"].([]any)[0].(map[string]any)["jogadores"].([]any)
	if code != 200 || j[0].(map[string]any)["vida_pct"] != 50.0 || j[9].(map[string]any)["mana_pct"] != 40.0 ||
		j[2].(map[string]any)["vida"] != 800.0 || j[2].(map[string]any)["cd"].(map[string]any)["f"] != 127.5 {
		t.Fatalf("estados %v", j)
	}
	code, m = a.pede(t, "GET", "/api/partidas/"+itoa(partidaTeste)+"/momento?t=20", "")
	if code != 200 || m["quadro"] == nil {
		t.Fatal(code, m)
	}
	rc, err := ListaCadeia(a.db, partidaTeste, 3, map[string]bool{"habilidade_usada": true})
	if err != nil {
		t.Fatal(err)
	}
	// ~240 leituras em 60 s de jogo → ~6 ciclos de 40.
	if len(rc.Itens) < 4 || len(rc.Itens) > 7 {
		t.Fatalf("%d usos do Q", len(rc.Itens))
	}
	for _, it := range rc.Itens {
		if it.Dados["habilidade"] != "q" {
			t.Fatalf("uso inesperado %+v", it)
		}
	}
}

type riotFalsa struct{ pedidos int }

func (r *riotFalsa) LinhaDoTempo(ctx context.Context, gameID int64) ([]byte, error) {
	r.pedidos++
	// Minuto 1: bate com o leitorCiclico (vida 800/1000, AD 99); minuto 0 sem
	// amostra perto; vida_max divergente no quadro de outro jogador.
	return []byte(`{"info":{"frames":[
		{"timestamp":0,"participantFrames":{"3":{"level":1,"championStats":{"health":800,"healthMax":1000}}}},
		{"timestamp":30010,"participantFrames":{"3":{"level":LVL,"championStats":{"health":790,"healthMax":1000,"power":300,"powerMax":500,"attackDamage":99,"attackSpeed":80,"armor":10}}}}
	]}}`), nil
}

func TestConferenciaComRiot(t *testing.T) {
	var as []Amostra
	f := func(v float64) *float64 { return &v }
	for i := 0; i < 200; i++ {
		as = append(as, Amostra{TMs: int64(i) * 250, Nivel: 2, Vida: f(800), VidaMax: f(1000), Mana: f(300), ManaMax: f(500),
			Atr: Atributos{AD: f(99), VelAtaque: f(0.79)}})
	}
	bruto, _ := (&riotFalsa{}).LinhaDoTempo(context.Background(), 1)
	bruto = []byte(strings.Replace(string(bruto), "LVL", "2", 1))
	c, err := ConfereComRiot(bruto, 3, as)
	if err != nil {
		t.Fatal(err)
	}
	// Minuto 30 s: vida a 10 de diferença (dentro de 3%), o resto exato; a Riot dá a
	// velocidade de ataque numa unidade que não é a do HUD: fica de fora.
	if c.Minutos != 1 || c.N != 5 || c.Iguais != 5 || math.Abs(c.MaiorDif-10) > 1e-9 || c.Campos["vida"].MaiorDif != 10 {
		t.Fatalf("%+v %+v", c, c.Campos["atr_vel_ataque"])
	}
	// Nível diferente no quadro: pula (subida de nível no meio).
	bruto2 := []byte(strings.Replace(string(bruto), `"level":2`, `"level":3`, 1))
	if c, _ := ConfereComRiot(bruto2, 3, as); c.Minutos != 0 {
		t.Fatalf("quadro com nível diferente deveria ser pulado: %+v", c)
	}
}

// O leitor do modo teste lê o tempo pintado pelo jogo falso: a tela muda com o
// jogo, a "última leitura" sai no evento amostra e as colunas chegam ao bloco.
func TestLeitorFalsoDoModoTeste(t *testing.T) {
	antes := IntervaloEventoSSE
	IntervaloEventoSSE = 0
	t.Cleanup(func() { IntervaloEventoSSE = antes })
	a := prepara(t, func(j *replay.Falso, c *Config) { c.Leitor = LeitorFalso{} })
	r, exID, err := a.fila.Cria(context.Background(), partidaTeste, Pedido{Jogadores: []int{3}, TFimMs: 40000})
	if err != nil {
		t.Fatal(err)
	}
	a.fila.Espera()
	if r, _ = LeRastreio(a.db, r.ID); r.Estado != Concluido {
		t.Fatalf("rastreio %s %s", r.Estado, r.Erro)
	}
	ex, ok := a.hub.Busca(exID)
	if !ok {
		t.Fatal("execução sumiu")
	}
	achou := false
	for _, ev := range ex.Eventos() {
		if ev.Tipo != "amostra" {
			continue
		}
		var d map[string]any
		json.Unmarshal(ev.Dados, &d)
		cd, _ := d["cd"].(map[string]any)
		// A primeira leitura do passe ainda não confirmou o máximo (saneador):
		// basta uma amostra com a tela inteira.
		if d["vida_max"] == 1000.0 && d["mana_max"] == 500.0 && cd != nil && cd["q"] != nil && d["nivel"] != nil {
			achou = true
		}
	}
	if !achou {
		t.Fatal("nenhum evento amostra trouxe a leitura da tela")
	}
	_, m := a.pede(t, "GET", "/api/partidas/"+itoa(partidaTeste)+"/amostras?participante=3&de=10&ate=30", "")
	vidas := map[float64]bool{}
	for _, x := range m["amostras"].([]any) {
		if v, ok := x.(map[string]any)["vida"].(float64); ok {
			vidas[v] = true
		}
	}
	if len(vidas) < 5 {
		t.Fatalf("a vida do leitor falso deveria variar com o tempo: %v", vidas)
	}
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	l1, _ := LeitorFalso{}.Le(img, 4, 4)
	l2, _ := LeitorFalso{}.Le(img, 4, 4)
	if *l1.Vida != *l2.Vida || *l1.CD.Q != *l2.CD.Q {
		t.Fatal("o leitor falso tem de ser puro")
	}
}
