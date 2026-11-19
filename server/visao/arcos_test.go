package visao

import (
	"image"
	"math"
	"testing"

	"loltracking/server/rastreio"
	"loltracking/server/replay"
)

// minimapaSintetico: o recorte replay.RegiaoMinimapa com a moldura preta e o
// fundo liso (como o jogo falso), sem nenhum retrato.
func minimapaSintetico() *image.RGBA {
	r := replay.RegiaoMinimapa
	img := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	enche := func(q image.Rectangle, c [3]uint8) {
		q = q.Intersect(r)
		for y := q.Min.Y; y < q.Max.Y; y++ {
			for x := q.Min.X; x < q.Max.X; x++ {
				i := img.PixOffset(x-r.Min.X, y-r.Min.Y)
				img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c[0], c[1], c[2], 255
			}
		}
	}
	enche(r, [3]uint8{0x22, 0x26, 0x2a})
	enche(replay.QuadradoMinimapa, [3]uint8{0, 0, 0})
	enche(replay.QuadradoMinimapa.Inset(10), [3]uint8{0x3a, 0x40, 0x30})
	return img
}

// pintaRetrato desenha um retrato (miolo cinza, contorno escuro, anel na cor
// do time) com centro (cx, cy) na tela de referência, por cima do que houver:
// o mesmo desenho do jogo falso.
func pintaRetrato(img *image.RGBA, cx, cy float64, cor [3]uint8) {
	r := replay.RegiaoMinimapa
	for yy := int(cy) - 17; yy <= int(cy)+17; yy++ {
		for xx := int(cx) - 17; xx <= int(cx)+17; xx++ {
			if !(image.Point{xx, yy}).In(r) {
				continue
			}
			d := math.Hypot(float64(xx)-cx, float64(yy)-cy)
			var c [3]uint8
			switch {
			case d <= 12.5:
				v := uint8(0x50 + (xx*7+yy*3)%0x30)
				c = [3]uint8{v, v - 0x10, v + 0x08}
			case d <= 13.5:
				c = [3]uint8{0x10, 0x10, 0x10}
			case d <= 15.5:
				c = cor
			default:
				continue
			}
			i := img.PixOffset(xx-r.Min.X, yy-r.Min.Y)
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c[0], c[1], c[2], 255
		}
	}
}

// Um retrato azul com um vermelho por cima a 9 px: o vermelho sai inteiro
// nos retratos, o azul sai como ARCO (cobertura abaixo da do retrato), com a
// falta do lado do vermelho; na visão vermelha, a decisão dá o azul como
// visível pelo arco. Sem o vermelho por cima (só o arco, com a falta num
// lugar sem retrato), não.
func TestArcoDeRetratoEncostado(t *testing.T) {
	l, err := Novo()
	if err != nil {
		t.Fatal(err)
	}
	img := minimapaSintetico()
	ax, ay := 2330.0, 1250.0 // azul
	vx, vy := ax+9, ay       // vermelho, desenhado depois (por cima)
	pintaRetrato(img, ax, ay, replay.CorAnelAzul)
	pintaRetrato(img, vx, vy, replay.CorAnelVermelho)
	lt, err := l.LeMinimapa(img, replay.RegiaoMinimapa, refLargura, refAltura)
	if err != nil {
		t.Fatal(err)
	}
	mx, my := replay.MinimapaParaMapa(ax, ay, refLargura, refAltura)
	wx, wy := replay.MinimapaParaMapa(vx, vy, refLargura, refAltura)
	if len(lt.Retratos) != 1 || lt.Retratos[0].Time != 200 || replay.DistanciaMinimapa(lt.Retratos[0].X, lt.Retratos[0].Y, wx, wy) > 1.5 {
		t.Fatalf("retratos: %+v", lt.Retratos)
	}
	if len(lt.Arcos) != 1 {
		t.Fatalf("arcos: %+v", lt.Arcos)
	}
	a := lt.Arcos[0]
	if a.Time != 100 || a.Cobertura < CoberturaMinimaArco || a.Cobertura >= CoberturaMinimaRetrato || a.Arco == 0 ||
		replay.DistanciaMinimapa(a.X, a.Y, mx, my) > 2 {
		t.Fatalf("arco: %+v (esperado em %.0f, %.0f)", a, mx, my)
	}
	// A falta fica do lado do vermelho (direção 0 = +x na tela); o lado
	// oposto (direção 32) aparece.
	if a.Arco&1 != 0 || a.Arco&(1<<32) == 0 {
		t.Fatalf("máscara %016x", a.Arco)
	}
	js := []rastreio.PosicaoNoInstante{
		{ParticipanteID: 1, Time: 100, TemPosicao: true, X: mx, Y: my},
		{ParticipanteID: 6, Time: 200, TemPosicao: true, X: wx, Y: wy},
	}
	r := rastreio.ClassificaCaptura(lt, true, 200, js)
	if r.Estados[1] != rastreio.VisVisivel || !r.PeloArco[1] {
		t.Fatalf("o azul embaixo do vermelho devia sair visível pelo arco: %+v", r)
	}
	// O mesmo arco sem o retrato vermelho em cima (outra captura, a falta não
	// tem explicação): não vale; o azul longe de tudo sai fora.
	lt2 := rastreio.LeituraMinimapa{Modo: lt.Modo, Arcos: lt.Arcos}
	r = rastreio.ClassificaCaptura(lt2, true, 200, js[:1])
	if r.Estados[1] != rastreio.VisFora || r.PeloArco[1] {
		t.Fatalf("arco sem retrato por cima: %+v", r)
	}
}

// Nas 4 capturas de 2026-11-17 os arcos não mudam a decisão (o teste de
// visibilidade continua igual) e o que sai como arco é anotado.
func TestArcosNasCapturas(t *testing.T) {
	l, err := Novo()
	if err != nil {
		t.Fatal(err)
	}
	for _, arq := range []string{"0-inicial_mini.png", "1-F1_mini.png", "2-F2_mini.png", "3-F3_mini.png"} {
		lt := leMini(t, l, arq)
		for _, a := range lt.Arcos {
			if a.Cobertura < CoberturaMinimaArco || a.Cobertura >= CoberturaMinimaRetrato || a.Arco == 0 {
				t.Errorf("%s: arco fora da faixa: %+v", arq, a)
			}
		}
		t.Logf("%s: %d retratos, %d arcos %+v", arq, len(lt.Retratos), len(lt.Arcos), lt.Arcos)
	}
}
