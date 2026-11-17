package rastreio

import (
	"bytes"
	"encoding/json"
	"math"
	"math/rand"
	"os"
	"testing"
)

// Os 20 pontos medidos pausado (docs/calibracao/pontos.json): sem
// screenPositionCenter, então só o deslocamento entra. Esperado: (−2, +1291),
// erro mediano ~74–84, e o `T` pausado mais grosseiro (câmera parada no
// jogador anterior) descartado.
func TestCalibracaoPontosMedidos(t *testing.T) {
	b, err := os.ReadFile("../../docs/calibracao/pontos.json")
	if err != nil {
		t.Fatal(err)
	}
	var pts []struct {
		CamX float64 `json:"cam_x"`
		CamZ float64 `json:"cam_z"`
		TlX  float64 `json:"tl_x"`
		TlY  float64 `json:"tl_y"`
	}
	// O arquivo foi gravado pelo PowerShell, com BOM.
	if err := json.Unmarshal(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")), &pts); err != nil || len(pts) != 20 {
		t.Fatalf("pontos.json: %d pontos, %v", len(pts), err)
	}
	base := CalibracaoPadrao(2560, 1440)
	var ps []PontoCal
	for _, p := range pts {
		ps = append(ps, PontoCal{CamX: p.CamX, CamZ: p.CamZ, SX: base.CX, SY: base.CY, TlX: p.TlX, TlY: p.TlY})
	}
	c := Ajusta(ps, base)
	t.Logf("modo %s, X0 %.1f Y0 %.1f, pontos %d, descartados %d, erro mediano %.0f máx %.0f",
		c.Modo, c.X0, c.Y0, c.Pontos, c.Descartados, *c.ErroMediano, *c.ErroMaximo)
	// O m14_p10 (câmera parada no jogador vizinho, a ~190 do certo) não dá para distinguir de uma leitura ruim.
	if c.Descartados < 1 || c.Modo != "deslocamento" {
		t.Fatalf("descartados %d modo %s", c.Descartados, c.Modo)
	}
	if math.Abs(c.X0-(-2)) > 15 || math.Abs(c.Y0-1291) > 15 {
		t.Fatalf("deslocamento (%.1f, %.1f) longe de (−2, +1291)", c.X0, c.Y0)
	}
	if *c.ErroMediano > 90 || *c.ErroMaximo > 200 {
		t.Fatalf("erro %.0f/%.0f", *c.ErroMediano, *c.ErroMaximo)
	}
}

// Pontos gerados pela fórmula da seção 3.2 (com ruído de ±30): o ajuste
// completo devolve os coeficientes; numa tela 3840×2160 os por-pixel escalam.
func TestCalibracaoRecuperaFormula(t *testing.T) {
	for _, tela := range [][2]int{{2560, 1440}, {3840, 2160}, {1920, 1080}} {
		verdade := CalibracaoPadrao(tela[0], tela[1])
		r := rand.New(rand.NewSource(1))
		var ps []PontoCal
		for i := 0; i < 30; i++ {
			camX, camZ := 1000+r.Float64()*12000, 1000+r.Float64()*12000
			sx := verdade.CX + (r.Float64()-0.5)*float64(tela[0])*0.3
			sy := verdade.CY + (r.Float64()-0.5)*float64(tela[1])*0.3
			x, y := verdade.Posicao(camX, camZ, sx, sy)
			ps = append(ps, PontoCal{CamX: camX, CamZ: camZ, SX: sx, SY: sy, TlX: x + (r.Float64()-0.5)*60, TlY: y + (r.Float64()-0.5)*60})
		}
		// Um ponto com a câmera em outro lugar: tem de ser descartado.
		ps = append(ps, PontoCal{CamX: 5000, CamZ: 5000, SX: verdade.CX, SY: verdade.CY, TlX: 9000, TlY: 2000})
		// Parte de uma base errada (como se a tela fosse outra): o centro errado
		// vai para o deslocamento, e o por-pixel sai certo.
		c := Ajusta(ps, CalibracaoPadrao(2560, 1440))
		if c.Modo != "completo" || c.Descartados != 1 {
			t.Fatalf("%v: modo %s descartados %d", tela, c.Modo, c.Descartados)
		}
		if math.Abs(c.XPx-verdade.XPx)/verdade.XPx > 0.05 || math.Abs(c.YPx-verdade.YPx)/verdade.YPx > 0.05 {
			t.Fatalf("%v: por pixel (%.3f, %.3f) x verdade (%.3f, %.3f)", tela, c.XPx, c.YPx, verdade.XPx, verdade.YPx)
		}
		if *c.ErroMediano > 40 {
			t.Fatalf("%v: erro mediano %.0f", tela, *c.ErroMediano)
		}
	}
	// Com poucos pontos, só o deslocamento muda.
	base := CalibracaoPadrao(2560, 1440)
	ps := []PontoCal{{CamX: 100, CamZ: 100, SX: 1300, SY: 700, TlX: 150, TlY: 1400}, {CamX: 900, CamZ: 900, SX: 1260, SY: 740, TlX: 950, TlY: 2140}}
	c := Ajusta(ps, base)
	if c.Modo != "deslocamento" || c.XPx != base.XPx {
		t.Fatalf("%+v", c)
	}
}

func TestPontosDoPasseExcluiMorto(t *testing.T) {
	lt := LinhaDoTempo{Quadros: []QuadroLT{
		{TMs: 60030, Jogadores: map[int]JogadorLT{3: {X: 1000, Y: 2000}}},
		{TMs: 120030, Jogadores: map[int]JogadorLT{3: {X: 1100, Y: 2100}}},
	}}
	var as []Amostra
	for tm := int64(59000); tm <= 121000; tm += 250 {
		a := Amostra{TMs: tm, SelecaoOK: true, CamX: pf(1000), CamZ: pf(750), TelaX: pf(1280), TelaY: pf(720), Origem: OrigemCamera}
		a.Morto = tm > 118000
		as = append(as, a)
	}
	ps := PontosDoPasse(as, lt, 3)
	if len(ps) != 1 || ps[0].TMs != 60030 {
		t.Fatalf("%+v", ps)
	}
}
