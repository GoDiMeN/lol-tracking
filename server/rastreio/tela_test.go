package rastreio

import (
	"context"
	"image"
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
