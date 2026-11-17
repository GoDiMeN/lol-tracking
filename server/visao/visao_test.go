package visao

import (
	"errors"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"loltracking/server/rastreio"
)

// placar conta, por campo, quantos bateram, quantos saíram nulos e quantos
// saíram ERRADOS (o que não pode acontecer: melhor nulo que errado).
type placar map[string]*[3]int

func (p placar) conta(campo string, esperado, lido *float64, tol float64) {
	c := p[campo]
	if c == nil {
		c = &[3]int{}
		p[campo] = c
	}
	switch {
	case esperado == nil && lido == nil:
		c[0]++
	case lido == nil:
		c[1]++
	case esperado != nil && math.Abs(*esperado-*lido) <= tol:
		c[0]++
	default:
		c[2]++
	}
}

func (p placar) relata(t *testing.T) (erros int) {
	var nomes []string
	for n := range p {
		nomes = append(nomes, n)
	}
	sort.Strings(nomes)
	var b strings.Builder
	for _, n := range nomes {
		c := p[n]
		tot := c[0] + c[1] + c[2]
		fmt.Fprintf(&b, "\n  %-16s %3d/%3d certos (%5.1f%%)  %2d nulos  %2d errados", n, c[0], tot, 100*float64(c[0])/float64(tot), c[1], c[2])
		erros += c[2]
	}
	t.Log("acerto por campo:" + b.String())
	return erros
}

func f64(v float64) *float64 { return &v }

func intPtr(p *int) *float64 {
	if p == nil {
		return nil
	}
	v := float64(*p)
	return &v
}

// avalia roda o leitor nas capturas rotuladas e compara campo a campo.
func avalia(t *testing.T, abre func(nome string) (*image.RGBA, infoCaptura, bool), soPainel bool) placar {
	l, err := Novo()
	if err != nil {
		t.Fatal(err)
	}
	p := placar{}
	n := 0
	for _, r := range leRotulos(t) {
		img, info, ok := abre(r.arquivo)
		if !ok {
			continue
		}
		n++
		lt, err := l.Le(img, refLargura, refAltura)
		if err != nil {
			t.Errorf("%s: %v", r.arquivo, err)
			continue
		}
		pres := 0.0
		if lt.PainelPresente {
			pres = 1
		}
		esp := 0.0
		if r.painel {
			esp = 1
		}
		p.conta("painel_presente", &esp, &pres, 0)
		if r.painel {
			lidos := []*float64{lt.Vida, lt.VidaMax, lt.Mana, lt.ManaMax, intPtr(lt.Nivel), lt.Atr.AD, lt.Atr.AP,
				lt.Atr.Armadura, lt.Atr.RM, lt.Atr.VelAtaque, lt.Atr.VelMov}
			for i, v := range lidos {
				p.conta(nomesNum[i], f64(r.num[i]), v, 0.001)
				if v != nil && math.Abs(*v-r.num[i]) > 0.001 {
					t.Logf("%s: %s lido %v, esperado %v", r.arquivo, nomesNum[i], *v, r.num[i])
				}
			}
			cds := []*float64{lt.CD.Q, lt.CD.W, lt.CD.E, lt.CD.R, lt.CD.D, lt.CD.F}
			for i, v := range cds {
				p.conta(nomesCD[i], r.cd[i], v, 0.001)
				if v != nil && (r.cd[i] == nil || math.Abs(*v-*r.cd[i]) > 0.001) {
					t.Logf("%s: %s lido %v, esperado %s", r.arquivo, nomesCD[i], *v, r.txt[11+i])
				}
			}
		} else if lt.Vida != nil || lt.CD.Q != nil {
			t.Errorf("%s: sem painel, mas leu vida/recarga", r.arquivo)
		}
		if soPainel {
			continue
		}
		// Quadros laterais contra o playerlist do instante.
		for k, j := range info.Jogadores {
			if k >= 10 {
				break
			}
			q := lt.Quadros[k]
			m, mo := 0.0, 0.0
			if j.Morto {
				m = 1
			}
			if q.Morto != nil && *q.Morto {
				mo = 1
			}
			var mp *float64
			if q.Morto != nil {
				mp = &mo
			}
			p.conta("quadro_morto", &m, mp, 0)
			if j.Morto && q.Respawn != nil {
				// O número da tela é o respawnTimer arredondado para cima; a captura
				// e o playerlist não são do mesmo milissegundo: ±1.
				p.conta("quadro_respawn", f64(math.Ceil(j.Renasce)), q.Respawn, 1)
			} else if j.Morto {
				p.conta("quadro_respawn", f64(math.Ceil(j.Renasce)), nil, 1)
			}
			if j.Morto {
				p.conta("quadro_vida_morto", f64(0), q.VidaPct, 0)
			}
		}
		// A barra do quadro do jogador seguido tem de bater com o texto do painel.
		if r.painel {
			k := -1
			for i, j := range info.Jogadores {
				if j.Nome == info.Selecao {
					k = i
				}
			}
			if k >= 0 && k < 10 {
				p.conta("quadro_vida_pct", f64(100*r.num[0]/r.num[1]), lt.Quadros[k].VidaPct, 3)
				p.conta("quadro_mana_pct", f64(100*r.num[2]/r.num[3]), lt.Quadros[k].ManaPct, 3)
			}
		}
	}
	t.Logf("%d capturas avaliadas", n)
	if n == 0 {
		t.Skip("nenhuma captura")
	}
	return p
}

// TestCapturasCompletas: as 63 capturas inteiras (fora do git; pula se não
// estão na máquina).
func TestCapturasCompletas(t *testing.T) {
	pasta := pastaCapturas()
	if _, err := os.Stat(pasta); err != nil {
		t.Skip("capturas completas não estão nesta máquina")
	}
	p := avalia(t, func(nome string) (*image.RGBA, infoCaptura, bool) {
		img, info := abreCaptura(t, pasta, nome)
		return img, info, true
	}, false)
	if e := p.relata(t); e > 0 {
		t.Errorf("%d leituras erradas (tem de ser 0: melhor nulo que errado)", e)
	}
}

// TestCapturasRecortadas: o subconjunto do repositório (testdata/*.png, só
// as regiões do HUD; o resto da tela é preto). Roda sempre.
func TestCapturasRecortadas(t *testing.T) {
	p := avalia(t, func(nome string) (*image.RGBA, infoCaptura, bool) {
		if _, err := os.Stat(filepath.Join("testdata", nome+".png")); err != nil {
			return nil, infoCaptura{}, false
		}
		img, info := abreCaptura(t, "testdata", nome)
		return img, info, true
	}, false)
	if e := p.relata(t); e > 0 {
		t.Errorf("%d leituras erradas (tem de ser 0: melhor nulo que errado)", e)
	}
	// Mínimo de acerto por campo (o que se mediu ao fechar a tarefa 3).
	for campo, c := range p {
		tot := c[0] + c[1] + c[2]
		if tot > 0 && float64(c[0])/float64(tot) < 0.9 {
			t.Errorf("%s: só %d de %d certos", campo, c[0], tot)
		}
	}
}

func TestResolucaoNaoMedida(t *testing.T) {
	l, err := Novo()
	if err != nil {
		t.Fatal(err)
	}
	_, err = l.Le(image.NewRGBA(image.Rect(0, 0, 1920, 1080)), 1920, 1080)
	if !errors.Is(err, ErrResolucao) {
		t.Fatalf("erro = %v, queria ErrResolucao", err)
	}
}

// Tela preta (ou HUD em outra escala): nenhum marco no lugar → erro claro, e
// não uma leitura cheia de lixo.
func TestHUDForaDoLugar(t *testing.T) {
	l, _ := Novo()
	img := image.NewRGBA(image.Rect(0, 0, refLargura, refAltura))
	for i := range img.Pix {
		img.Pix[i] = 0x70
	}
	_, err := l.Le(img, refLargura, refAltura)
	if !errors.Is(err, ErrHUD) {
		t.Fatalf("erro = %v, queria ErrHUD", err)
	}
}

var _ rastreio.LeitorDeTela = (*Leitor)(nil)

func BenchmarkLe(b *testing.B) {
	pasta := pastaCapturas()
	nome := "p03_t0840957"
	if _, err := os.Stat(filepath.Join(pasta, nome+".png")); err != nil {
		pasta = "testdata"
	}
	img, _ := abreCaptura(b, pasta, nome)
	l, err := Novo()
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := l.Le(img, refLargura, refAltura); err != nil {
			b.Fatal(err)
		}
	}
}

func TestDiagnosticoMorto(t *testing.T) {
	pasta := pastaCapturas()
	if os.Getenv("LOL_DIAG") == "" {
		t.Skip()
	}
	l, _ := Novo()
	for _, r := range leRotulos(t) {
		img, info := abreCaptura(t, pasta, r.arquivo)
		lt, _ := l.Le(img, refLargura, refAltura)
		for k, j := range info.Jogadores {
			if k < 10 && lt.Quadros[k].Morto != nil && *lt.Quadros[k].Morto != j.Morto {
				t.Logf("%s quadro %d: tela morto=%v api morto=%v respawn=%.2f", r.arquivo, k, *lt.Quadros[k].Morto, j.Morto, j.Renasce)
			}
		}
	}
}
