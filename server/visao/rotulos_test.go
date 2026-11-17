package visao

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"testing"
)

// rotulo é o que se leu a olho numa captura (testdata/rotulos.txt).
type rotulo struct {
	arquivo string
	painel  bool
	// vida, vida_max, mana, mana_max, nivel, ad, ap, armadura, rm, vel_ataque, vel_mov
	num [11]float64
	// q w e r d f: valor; nulo (bloqueada) = nil
	cd [6]*float64
	// texto original de cada campo (para gerar os gabaritos com o texto exato:
	// "0.79", "1.20").
	txt [17]string
}

var nomesNum = [11]string{"vida", "vida_max", "mana", "mana_max", "nivel", "atr_ad", "atr_ap", "atr_armadura", "atr_rm", "atr_vel_ataque", "atr_vel_mov"}
var nomesCD = [6]string{"cd_q", "cd_w", "cd_e", "cd_r", "cd_d", "cd_f"}

func leRotulos(t testing.TB) []rotulo {
	t.Helper()
	f, err := os.Open("testdata/rotulos.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rs []rotulo
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		c := strings.Fields(l)
		r := rotulo{arquivo: c[0]}
		if len(c) == 2 && c[1] == "-" {
			rs = append(rs, r)
			continue
		}
		if len(c) != 18 {
			t.Fatalf("rótulo com %d campos: %q", len(c), l)
		}
		r.painel = true
		for i := 0; i < 11; i++ {
			v, err := strconv.ParseFloat(c[1+i], 64)
			if err != nil {
				t.Fatalf("%s: %v", r.arquivo, err)
			}
			r.num[i] = v
			r.txt[i] = c[1+i]
		}
		for i := 0; i < 6; i++ {
			r.txt[11+i] = c[12+i]
			if c[12+i] == "x" {
				continue
			}
			v, err := strconv.ParseFloat(c[12+i], 64)
			if err != nil {
				t.Fatalf("%s: %v", r.arquivo, err)
			}
			r.cd[i] = &v
		}
		rs = append(rs, r)
	}
	return rs
}

// Níveis das habilidades Q W E R lidos a olho em algumas capturas: só servem
// para gerar o gabarito "nivel_habilidade" (o "0" de não aprendida).
var niveisHab = map[string][4]int{
	"p03_t0380251": {3, 1, 1, 0},
	"p03_t0400345": {3, 1, 1, 1},
	"p03_t0500801": {4, 1, 1, 1},
	"p03_t0840957": {5, 1, 3, 1},
	"p03_t0901442": {5, 1, 3, 2},
	"p03_t1006338": {5, 1, 4, 2},
	"p03_t1187524": {5, 1, 5, 2},
	"p03_t1241433": {5, 2, 5, 2},
	"p03_t1362469": {5, 3, 5, 2},
	"p03_t1604613": {5, 3, 5, 3},
	"p03_t1685293": {5, 4, 5, 3},
	"p03_t1786149": {5, 5, 5, 3},
	"p10_t0720129": {1, 1, 3, 1},
}
