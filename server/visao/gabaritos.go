package visao

import (
	"bufio"
	"embed"
	"fmt"
	"io"
	"strings"
)

// Os gabaritos ficam em gabaritos/<fonte>.txt, um arquivo de texto legível
// (dá para ver o glifo no diff):
//
//	# comentário
//	glifo 7 6 9          <- caractere, largura, altura
//	..5ff8..             <- uma linha por linha do glifo; cada pixel é a
//	.cf..fc.                tinta/16 em hexadecimal ('.' = 0)
//
// Gerados das capturas reais por TestGeraGabaritos (gera_test.go, só com
// LOL_GERA_GABARITOS=1); em tempo de execução são só lidos.

//go:embed gabaritos/*.txt
var arquivosGabaritos embed.FS

func leGabaritos(r io.Reader) ([]gabarito, error) {
	var gs []gabarito
	sc := bufio.NewScanner(r)
	var atual *gabarito
	linha := 0
	for sc.Scan() {
		linha++
		t := strings.TrimSpace(sc.Text())
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if strings.HasPrefix(t, "glifo ") {
			var c string
			var w, h int
			if _, err := fmt.Sscanf(t, "glifo %s %d %d", &c, &w, &h); err != nil || len(c) != 1 || w <= 0 || h <= 0 {
				return nil, fmt.Errorf("linha %d: cabeçalho de glifo inválido: %q", linha, t)
			}
			gs = append(gs, gabarito{c: c[0], g: glifo{w: w, h: h, v: make([]uint8, 0, w*h)}})
			atual = &gs[len(gs)-1]
			continue
		}
		if atual == nil || len(t) != atual.g.w || len(atual.g.v) >= atual.g.w*atual.g.h {
			return nil, fmt.Errorf("linha %d: linha de glifo fora do lugar", linha)
		}
		for _, ch := range []byte(t) {
			var v uint8
			switch {
			case ch == '.':
			case ch >= '0' && ch <= '9':
				v = (ch - '0') * 16
			case ch >= 'a' && ch <= 'f':
				v = (ch - 'a' + 10) * 16
			default:
				return nil, fmt.Errorf("linha %d: pixel inválido %q", linha, ch)
			}
			if v > 0 {
				v += 8 // meio do intervalo
			}
			atual.g.v = append(atual.g.v, v)
		}
	}
	for _, g := range gs {
		if len(g.g.v) != g.g.w*g.g.h {
			return nil, fmt.Errorf("glifo %q incompleto", g.c)
		}
		_ = g
	}
	return gs, sc.Err()
}

func escreveGabaritos(w io.Writer, cab string, gs []gabarito) error {
	bw := bufio.NewWriter(w)
	for _, l := range strings.Split(cab, "\n") {
		fmt.Fprintf(bw, "# %s\n", l)
	}
	for _, g := range gs {
		fmt.Fprintf(bw, "glifo %c %d %d\n", g.c, g.g.w, g.g.h)
		for y := 0; y < g.g.h; y++ {
			for x := 0; x < g.g.w; x++ {
				v := g.g.v[y*g.g.w+x] / 16
				if v == 0 {
					bw.WriteByte('.')
				} else {
					bw.WriteByte("0123456789abcdef"[v])
				}
			}
			bw.WriteByte('\n')
		}
	}
	return bw.Flush()
}

// carregaFonte põe os gabaritos embutidos na fonte (arquivo ausente = fonte
// sem gabarito: tudo dela sai nulo, nunca errado).
func carregaFonte(f *fonte) error {
	arq, err := arquivosGabaritos.Open("gabaritos/" + f.nome + ".txt")
	if err != nil {
		return nil
	}
	defer arq.Close()
	gs, err := leGabaritos(arq)
	if err != nil {
		return fmt.Errorf("gabaritos de %s: %w", f.nome, err)
	}
	f.gab = gs
	return nil
}
