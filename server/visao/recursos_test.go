package visao

import (
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// Painéis reais (recortados) de campeões que fogem do caso comum: recarga na
// passiva (Vex, Garen), fúria (Renekton), energia (Akali) e o nível 9, que
// faltava nos gabaritos. Capturas das partidas 3301749762, 3302252878 e
// 3304217746, a 2560×1440; os números esperados foram lidos a olho.
func TestPainelDeRecursosDiferentes(t *testing.T) {
	l, err := Novo()
	if err != nil {
		t.Fatal(err)
	}
	f := func(v float64) *float64 { return &v }
	casos := []struct {
		arquivo           string
		passiva           *float64
		recurso, recursoM *float64
		nivel             int
	}{
		{"passiva_vex_2", f(2), f(551), f(766), 6},
		{"passiva_vex_12", f(12), f(784), f(1199), 8},
		{"passiva_vex_5", f(5), f(1166), f(1620), 11},
		{"passiva_vex_pronta", f(0), f(1068), f(1120), 7},
		// Garen não tem barra de recurso: mana nula, o resto lido normalmente.
		{"passiva_garen_5", f(5), nil, nil, 3},
		{"recurso_furia_cheia", f(0), f(100), f(100), 8},
		{"recurso_energia_nivel9", f(0), f(47), f(300), 9},
	}
	igual := func(a, b *float64) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }
	for _, c := range casos {
		arq, err := os.Open(filepath.Join("testdata", c.arquivo+".png"))
		if err != nil {
			t.Fatal(err)
		}
		src, err := png.Decode(arq)
		arq.Close()
		if err != nil {
			t.Fatal(err)
		}
		img := image.NewRGBA(src.Bounds())
		draw.Draw(img, img.Rect, src, src.Bounds().Min, draw.Src)
		lt, err := l.Le(img, refLargura, refAltura)
		if err != nil {
			t.Fatalf("%s: %v", c.arquivo, err)
		}
		if !lt.PainelPresente || lt.Vida == nil {
			t.Errorf("%s: painel não lido", c.arquivo)
			continue
		}
		if !igual(lt.CD.P, c.passiva) {
			t.Errorf("%s: passiva = %v, esperado %v", c.arquivo, val(lt.CD.P), val(c.passiva))
		}
		if !igual(lt.Mana, c.recurso) || !igual(lt.ManaMax, c.recursoM) {
			t.Errorf("%s: recurso = %v/%v, esperado %v/%v", c.arquivo, val(lt.Mana), val(lt.ManaMax), val(c.recurso), val(c.recursoM))
		}
		if lt.Nivel == nil || *lt.Nivel != c.nivel {
			t.Errorf("%s: nível = %v, esperado %d", c.arquivo, lt.Nivel, c.nivel)
		}
	}
}

func val(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}
