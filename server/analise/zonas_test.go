package analise

import (
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"strconv"
	"testing"
)

func confereZona(t *testing.T, nome string, x, y float64, zona, sub string) {
	t.Helper()
	z, s := Zona(x, y)
	if z != zona || (sub != "" && s != sub) {
		t.Errorf("%s (%.0f, %.0f): %s/%s, esperado %s/%s", nome, x, y, z, s, zona, sub)
	}
}

// Pontos conhecidos do mapa: fontes, torres, covas e o centro.
func TestZonasPontosConhecidos(t *testing.T) {
	confereZona(t, "fonte azul", 404, 571, ZBaseAzul, SFonte)
	confereZona(t, "fonte vermelha", 14340, 14507, ZBaseVermelha, SFonte)
	confereZona(t, "nexus azul", 1748, 2270, ZBaseAzul, SBase)
	confereZona(t, "inibidor do meio vermelho", 11598, 11667, ZBaseVermelha, SBase)
	// Torres externas (o trecho é decidido por elas: a torre fica na divisa).
	confereZona(t, "externa topo azul", 981, 10300, ZRotaTopo, SLadoAzul)
	confereZona(t, "interna topo azul", 1512, 6699, ZRotaTopo, SLadoAzul)
	confereZona(t, "externa topo vermelha", 4500, 13875, ZRotaTopo, SLadoVermelho)
	confereZona(t, "interna topo vermelha", 7943, 13411, ZRotaTopo, SLadoVermelho)
	confereZona(t, "canto do topo", 1700, 13000, ZRotaTopo, SCentro)
	confereZona(t, "externa meio azul", 5700, 6250, ZRotaMeio, SLadoAzul)
	confereZona(t, "externa meio vermelha", 9100, 8650, ZRotaMeio, SLadoVermelho)
	confereZona(t, "centro do mapa", 7400, 7450, ZRotaMeio, SCentro)
	confereZona(t, "externa baixo azul", 10300, 1029, ZRotaBaixo, SLadoAzul)
	confereZona(t, "interna baixo azul", 6919, 1483, ZRotaBaixo, SLadoAzul)
	confereZona(t, "externa baixo vermelha", 13866, 4600, ZRotaBaixo, SLadoVermelho)
	confereZona(t, "interna baixo vermelha", 13327, 8226, ZRotaBaixo, SLadoVermelho)
	confereZona(t, "canto de baixo", 13200, 2000, ZRotaBaixo, SCentro)
	confereZona(t, "barão", 5007, 10471, ZCovaBarao, SCova)
	confereZona(t, "dragão", 9866, 4414, ZCovaDragao, SCova)
	confereZona(t, "entrada do barão", 5900, 11300, ZCovaBarao, SArredores)
	confereZona(t, "rio de cima perto do meio", 6300, 8650, ZRioCima, SRio)
	confereZona(t, "rio de baixo perto do meio", 8500, 6300, ZRioBaixo, SRio)
	// Campos da selva (posições aproximadas dos acampamentos).
	confereZona(t, "azul do time azul", 3800, 7900, ZSelvaAzulCima, SSelva)
	confereZona(t, "sapo do time azul", 2300, 8400, ZSelvaAzulCima, SSelva)
	confereZona(t, "vermelho do time azul", 7800, 4100, ZSelvaAzulBaixo, SSelva)
	confereZona(t, "pedras do time azul", 8400, 2700, ZSelvaAzulBaixo, SSelva)
	confereZona(t, "azul do time vermelho", 10900, 7000, ZSelvaVermelhaBaixo, SSelva)
	confereZona(t, "vermelho do time vermelho", 7000, 10800, ZSelvaVermelhaCima, SSelva)
	confereZona(t, "fora", -2000, 5000, ZForaDoMapa, "")
}

// O mapa é simétrico: o espelho de cada ponto cai na zona "do outro lado".
func TestZonasSimetria(t *testing.T) {
	par := map[string]string{
		ZBaseAzul: ZBaseVermelha, ZRotaTopo: ZRotaBaixo, ZRotaMeio: ZRotaMeio, ZRioCima: ZRioBaixo,
		ZSelvaAzulCima: ZSelvaVermelhaBaixo, ZSelvaAzulBaixo: ZSelvaVermelhaCima,
	}
	for k, v := range par {
		par[v] = k
	}
	dif := 0
	for x := 500.0; x < 14500; x += 250 {
		for y := 500.0; y < 14500; y += 250 {
			z, _ := Zona(x, y)
			e := espelho(Ponto{x, y})
			z2, _ := Zona(e.X, e.Y)
			if esp, ok := par[z]; ok && z2 != esp {
				dif++
			}
		}
	}
	// As covas não são espelho exato uma da outra (posições da Riot); fora
	// delas a geometria é simétrica por construção.
	if dif > 40 {
		t.Fatalf("%d pontos fora da simetria", dif)
	}
}

// Os pontos reais da calibração (docs/calibracao/pontos.json): cada um com a
// posição exata da linha do tempo aos 4 e aos 14 min.
func TestZonasPontosReais(t *testing.T) {
	b, err := os.ReadFile("../../docs/calibracao/pontos.json")
	if err != nil {
		t.Skip("sem docs/calibracao/pontos.json")
	}
	if len(b) >= 3 && b[0] == 0xEF {
		b = b[3:]
	}
	var ps []struct {
		ID, Campeao, Rota, Time string
		Minuto                  int
		TlX                     float64 `json:"tl_x"`
		TlY                     float64 `json:"tl_y"`
	}
	if err := json.Unmarshal(b, &ps); err != nil {
		t.Fatal(err)
	}
	// O que se vê no minimapa das capturas (docs/calibracao/*.jpg) e na
	// posição: aos 4 min todo rotador está na própria rota; aos 14 min a
	// Seraphine e o Mestre Yi estão de passagem.
	esperado := map[string]string{
		"m04_p02": ZRioCima,        // Master Yi no rio de cima (perto do caranguejo)
		"m04_p07": ZRotaTopo,       // Warwick saindo da base pela rota do topo (minimapa em m04_p07.jpg)
		"m14_p02": ZSelvaAzulBaixo, // Master Yi perto do vermelho dele
		"m14_p05": ZRioCima,        // Seraphine no rio, encostada no meio
		"m14_p07": ZSelvaVermelhaBaixo,
	}
	for _, p := range ps {
		z, s := Zona(p.TlX, p.TlY)
		want, ok := esperado[p.ID]
		if !ok {
			want = RotaDaPosicao(p.Rota)
		}
		if z != want {
			t.Errorf("%s %s (%s) em (%.0f, %.0f): %s/%s, esperado %s", p.ID, p.Campeao, p.Rota, p.TlX, p.TlY, z, s, want)
		}
	}
}

func TestRotaEGrupo(t *testing.T) {
	if RotaDaPosicao("UTILITY") != ZRotaBaixo || RotaDaPosicao("JUNGLE") != "" {
		t.Fatal("rota da posição")
	}
	casos := []struct {
		zona, rota string
		time       int
		grupo      string
	}{
		{ZRotaMeio, ZRotaMeio, 100, "rota_propria"},
		{ZRotaTopo, ZRotaMeio, 100, "outra_rota"},
		{ZSelvaAzulCima, ZRotaMeio, 100, "selva_propria"},
		{ZSelvaAzulCima, ZRotaMeio, 200, "selva_inimiga"},
		{ZBaseVermelha, "", 200, "base_propria"},
		{ZCovaDragao, "", 200, "covas"},
	}
	for _, c := range casos {
		if g := GrupoRelativo(c.zona, c.rota, c.time); g != c.grupo {
			t.Errorf("%v: %s", c, g)
		}
	}
	g := GeraGrade(500)
	if len(g.Linhas) != g.NLinhas || len(g.Linhas[0]) != g.Colunas || g.Codigos[g.Linhas[0][0]-'A'] != ZBaseAzul {
		t.Fatalf("grade %d×%d %q", g.Colunas, g.NLinhas, g.Linhas[0][:3])
	}
}

// TestGeraImagemZonas pinta as zonas sobre o map11.png e grava
// docs/arquitetura/zonas.png (só com LOL_GERA_ZONAS=1): é a conferência a
// olho das fronteiras. Pontos da calibração em branco (contorno preto).
func TestGeraImagemZonas(t *testing.T) {
	if os.Getenv("LOL_GERA_ZONAS") != "1" {
		t.Skip("LOL_GERA_ZONAS=1 para gerar docs/arquitetura/zonas.png")
	}
	f, err := os.Open("../../docs/calibracao/map11.png")
	if err != nil {
		t.Fatal(err)
	}
	src, err := png.Decode(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	const N = 1024
	cores := map[string]color.RGBA{}
	for _, z := range Zonas {
		v, _ := strconv.ParseUint(z.Cor[1:], 16, 32)
		cores[z.Codigo] = color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255}
	}
	sb := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, N, N))
	larg, alt := MapaMax.X-MapaMin.X, MapaMax.Y-MapaMin.Y
	zonaDe := make([][]string, N)
	for py := 0; py < N; py++ {
		zonaDe[py] = make([]string, N)
		for px := 0; px < N; px++ {
			x := MapaMin.X + (float64(px)+0.5)/N*larg
			y := MapaMin.Y + (1-(float64(py)+0.5)/N)*alt
			z, s := Zona(x, y)
			zonaDe[py][px] = z + "/" + s
			r, g, b, _ := src.At(sb.Min.X+px*sb.Dx()/N, sb.Min.Y+py*sb.Dy()/N).RGBA()
			c := cores[z]
			// 45% da cor da zona sobre o mapa; trecho do centro e arredores um
			// pouco mais claros para aparecer a divisão.
			a := 0.45
			if s == SCentro || s == SArredores || s == SFonte {
				a = 0.3
			}
			mix := func(m uint32, z uint8) uint8 { return uint8(float64(m>>8)*(1-a) + float64(z)*a) }
			dst.Set(px, py, color.RGBA{mix(r, c.R), mix(g, c.G), mix(b, c.B), 255})
		}
	}
	// Bordas entre zonas/subzonas em preto.
	for py := 1; py < N; py++ {
		for px := 1; px < N; px++ {
			if zonaDe[py][px] != zonaDe[py-1][px] || zonaDe[py][px] != zonaDe[py][px-1] {
				dst.Set(px, py, color.RGBA{0, 0, 0, 255})
			}
		}
	}
	// Pontos reais da calibração.
	if b, err := os.ReadFile("../../docs/calibracao/pontos.json"); err == nil {
		if b[0] == 0xEF {
			b = b[3:]
		}
		var ps []struct {
			TlX float64 `json:"tl_x"`
			TlY float64 `json:"tl_y"`
		}
		json.Unmarshal(b, &ps)
		for _, p := range ps {
			cx := int((p.TlX - MapaMin.X) / larg * N)
			cy := int((1 - (p.TlY-MapaMin.Y)/alt) * N)
			for dy := -6; dy <= 6; dy++ {
				for dx := -6; dx <= 6; dx++ {
					if d := dx*dx + dy*dy; d <= 36 {
						c := color.RGBA{255, 255, 255, 255}
						if d > 16 {
							c = color.RGBA{0, 0, 0, 255}
						}
						dst.Set(cx+dx, cy+dy, c)
					}
				}
			}
		}
	}
	o, err := os.Create("../../docs/arquitetura/zonas.png")
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	if err := png.Encode(o, dst); err != nil {
		t.Fatal(err)
	}
}
