package visao

import (
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestGeraGabaritos extrai os glifos das capturas reais rotuladas e regrava
// gabaritos/*.txt. Só roda com LOL_GERA_GABARITOS=1 (tempo de
// desenvolvimento); as capturas inteiras ficam fora do git, em
// server/dados/capturas (LOL_CAPTURAS muda a pasta).
//
//	LOL_GERA_GABARITOS=1 go test -run TestGeraGabaritos -v ./visao/
func TestGeraGabaritos(t *testing.T) {
	if os.Getenv("LOL_GERA_GABARITOS") != "1" {
		t.Skip("só com LOL_GERA_GABARITOS=1")
	}
	pasta := pastaCapturas()
	l := &Leitor{}
	nl, _ := Novo()
	*l = *nl
	amostras := map[string][]gabarito{}
	cab := map[string]string{}
	add := func(f *fonte, gs []glifo, esperado, onde string) {
		if len(gs) == len(esperado)-1 && strings.Contains(esperado, "/") {
			esperado = strings.Replace(esperado, "/", "", 1)
		}
		if len(gs) != len(esperado) {
			t.Logf("%s %s: %d glifos para %q", onde, f.nome, len(gs), esperado)
			return
		}
		for i := range gs {
			amostras[f.nome] = append(amostras[f.nome], gabarito{c: esperado[i], g: gs[i]})
		}
	}
	satPronto, satCinza := 1.0, 0.0
	// LOL_GERA_METADE=1: só as capturas de índice par (para medir o acerto nas
	// outras, que não entraram nos gabaritos).
	metade := os.Getenv("LOL_GERA_METADE") == "1"
	for n, r := range leRotulos(t) {
		if metade && n%2 == 1 {
			continue
		}
		img, info := abreCaptura(t, pasta, r.arquivo)
		e := novaEscala(img.Rect)
		if r.painel {
			add(&l.vida, componentes(img, e.r(rVida), &l.vida), r.txt[0]+"/"+r.txt[1], r.arquivo)
			add(&l.mana, componentes(img, e.r(rMana), &l.mana), r.txt[2]+"/"+r.txt[3], r.arquivo)
			add(&l.nivel, componentes(img, e.r(rNivel), &l.nivel), r.txt[4], r.arquivo)
			for i, rr := range []ret{rAD, rAP, rArmadura, rRM, rVelAtaque, rVelMov} {
				add(&l.atr, componentes(img, e.r(rr), &l.atr), r.txt[5+i], r.arquivo)
			}
			for i := 0; i < 6; i++ {
				sat := saturacao(img, e.r(rIcone(i)))
				txt := r.txt[11+i]
				if txt == "0" {
					satPronto = math.Min(satPronto, sat)
					continue
				}
				satCinza = math.Max(satCinza, sat)
				if txt == "x" || txt == "-1" {
					continue
				}
				add(&l.recarga, componentes(img, e.r(rRecarga(i)), &l.recarga), txt, fmt.Sprintf("%s icone %d", r.arquivo, i))
			}
			if nh, ok := niveisHab[r.arquivo]; ok {
				for i := 0; i < 4; i++ {
					add(&l.nivelHab, componentes(img, e.r(rNivelHab(i)), &l.nivelHab), fmt.Sprint(nh[i]), fmt.Sprintf("%s nível %d", r.arquivo, i))
				}
			}
		}
		for k, j := range info.Jogadores {
			if !j.Morto || k >= 10 {
				continue
			}
			fr := j.Renasce - math.Floor(j.Renasce)
			if fr < 0.25 || fr > 0.75 {
				continue // perto da virada do segundo: o número da tela pode ser o vizinho
			}
			_, _, _, rr := quadro(k)
			add(&l.morte, componentes(img, e.r(rr), &l.morte), fmt.Sprint(math.Ceil(j.Renasce)), fmt.Sprintf("%s quadro %d", r.arquivo, k))
		}
	}
	t.Logf("saturação: menor de ícone pronto %.3f, maior de ícone cinza %.3f (limiar %.2f)", satPronto, satCinza, IconeColorido)
	for _, f := range l.fontes() {
		var gs []gabarito
		porChar := map[byte]int{}
		for _, a := range amostras[f.nome] {
			repetido := false
			for i := range gs {
				if gs[i].c == a.c && diferenca(&gs[i].g, &a.g) < 0.06 {
					repetido = true
					break
				}
			}
			if !repetido && porChar[a.c] < 8 {
				a.g.x0, a.g.y0 = 0, 0
				gs = append(gs, a)
				porChar[a.c]++
			}
		}
		sort.SliceStable(gs, func(a, b int) bool { return gs[a].c < gs[b].c })
		var chars []string
		for c, n := range porChar {
			chars = append(chars, fmt.Sprintf("%c×%d", c, n))
		}
		sort.Strings(chars)
		t.Logf("fonte %s: %d amostras, %d gabaritos: %s", f.nome, len(amostras[f.nome]), len(gs), strings.Join(chars, " "))
		if len(gs) == 0 {
			continue
		}
		arq, err := os.Create(filepath.Join("gabaritos", f.nome+".txt"))
		if err != nil {
			t.Fatal(err)
		}
		c := cab[f.nome]
		if c == "" {
			c = fmt.Sprintf("Fonte %q do HUD (2560×1440, escala padrão). Gerado por TestGeraGabaritos a partir das\ncapturas rotuladas em testdata/rotulos.txt. Não edite à mão: rode de novo.", f.nome)
		}
		if err := escreveGabaritos(arq, c, gs); err != nil {
			t.Fatal(err)
		}
		arq.Close()
	}
}

func pastaCapturas() string {
	if p := os.Getenv("LOL_CAPTURAS"); p != "" {
		return p
	}
	return filepath.Join("..", "dados", "capturas", "3304594274")
}

// infoCaptura é o JSON que o motor grava ao lado de cada captura.
type infoCaptura struct {
	Jogadores []struct {
		Nome    string  `json:"riotIdGameName"`
		Campeao string  `json:"championName"`
		Morto   bool    `json:"isDead"`
		Renasce float64 `json:"respawnTimer"`
		Nivel   int     `json:"level"`
	} `json:"jogadores"`
	Selecao string `json:"selecao"`
}

func abreCaptura(t testing.TB, pasta, nome string) (*image.RGBA, infoCaptura) {
	t.Helper()
	f, err := os.Open(filepath.Join(pasta, nome+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	src, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	img, ok := src.(*image.RGBA)
	if !ok {
		img = image.NewRGBA(src.Bounds())
		draw.Draw(img, img.Rect, src, src.Bounds().Min, draw.Src)
	}
	var info infoCaptura
	if b, err := os.ReadFile(filepath.Join(pasta, nome+".json")); err == nil {
		json.Unmarshal(b, &info)
	}
	return img, info
}

// Capturas que vão para o repositório (testdata/), recortadas só nas regiões
// do HUD que o leitor usa: cobrem painel ausente, vida baixa e com dano
// recente, recargas de 1, 2 e 3 dígitos e decimais, habilidade não aprendida
// e bloqueada, muitos mortos nos quadros, e os dois jogadores (Akshan e Lux).
var capturasDoRepositorio = []string{
	"p03_t0360166", "p03_t0380251", "p03_t0420436", "p03_t0440523", "p03_t0480708", "p03_t0500801",
	"p03_t0520887", "p03_t0540965", "p03_t0581163", "p03_t0840957", "p03_t0861123", "p03_t0881297",
	"p03_t0921616", "p03_t0966152", "p03_t0986152", "p03_t1026506", "p03_t1046679", "p03_t1066868",
	"p03_t1127078", "p03_t1167324", "p03_t1221231", "p03_t1241433", "p03_t1261611", "p03_t1382647",
	"p03_t1402849", "p03_t1423007", "p03_t1523871", "p03_t1644963", "p03_t1685293", "p03_t1765983",
	"p03_t1786149", "p10_t0720129", "p10_t0740208", "p10_t0760296",
}

// Regiões copiadas para o recorte (o resto fica preto).
var regioesPainel = []image.Rectangle{image.Rect(60, 1130, 386, 1283)}

// Quadros laterais (retrato e barras) só nas capturas de quadrosNoRepositorio
// e nas sem painel; nas outras, o JSON vai sem "jogadores" e o teste dos
// quadros pula.
var regioesQuadros = []image.Rectangle{image.Rect(38, 200, 100, 850), image.Rect(2460, 200, 2522, 850)}

var quadrosNoRepositorio = map[string]bool{"p03_t0360166": true, "p03_t0420436": true, "p03_t0861123": true,
	"p03_t0881297": true, "p03_t0966152": true, "p03_t1046679": true, "p03_t1221231": true, "p03_t1241433": true,
	"p10_t0720129": true}

// TestGeraTestdata: LOL_GERA_TESTDATA=1 regrava testdata/*.png e *.json.
func TestGeraTestdata(t *testing.T) {
	if os.Getenv("LOL_GERA_TESTDATA") != "1" {
		t.Skip("só com LOL_GERA_TESTDATA=1")
	}
	pasta := pastaCapturas()
	total := 0
	for _, nome := range capturasDoRepositorio {
		img, _ := abreCaptura(t, pasta, nome)
		rec := image.NewRGBA(img.Rect)
		for i := 3; i < len(rec.Pix); i += 4 {
			rec.Pix[i] = 255
		}
		regs := regioesPainel
		comQuadros := quadrosNoRepositorio[nome] || !temPainel(t, nome)
		if comQuadros {
			regs = append(append([]image.Rectangle(nil), regs...), regioesQuadros...)
		}
		for _, r := range regs {
			draw.Draw(rec, r, img, r.Min, draw.Src)
		}
		f, err := os.Create(filepath.Join("testdata", nome+".png"))
		if err != nil {
			t.Fatal(err)
		}
		enc := png.Encoder{CompressionLevel: png.BestCompression}
		if err := enc.Encode(f, rec); err != nil {
			t.Fatal(err)
		}
		st, _ := f.Stat()
		total += int(st.Size())
		f.Close()
		// O JSON vai inteiro menos o que não serve ao teste.
		var bruto map[string]any
		b, _ := os.ReadFile(filepath.Join(pasta, nome+".json"))
		json.Unmarshal(b, &bruto)
		if !comQuadros {
			delete(bruto, "jogadores")
		}
		if js, ok := bruto["jogadores"].([]any); ok {
			for _, j := range js {
				if m, ok := j.(map[string]any); ok {
					delete(m, "items")
					delete(m, "runes")
					delete(m, "summonerName")
				}
			}
		}
		b, _ = json.MarshalIndent(bruto, "", " ")
		os.WriteFile(filepath.Join("testdata", nome+".json"), b, 0o644)
	}
	t.Logf("%d capturas, %.1f MB", len(capturasDoRepositorio), float64(total)/1e6)
}

func temPainel(t *testing.T, nome string) bool {
	for _, r := range leRotulos(t) {
		if r.arquivo == nome {
			return r.painel
		}
	}
	return false
}
