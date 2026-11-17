package rastreio

import (
	"math"
	"sort"

	"loltracking/server/replay"
)

// Calibracao é a fórmula câmera → mapa (descobertas 3.2):
//
//	x = cam_x + X0 + XPx·(sx − CX)
//	y = cam_z + Y0 − YPx·(sy − CY)
//
// com (sx, sy) = screenPositionCenter do jogador e (CX, CY) o centro da tela.
type Calibracao struct {
	X0, XPx, Y0, YPx float64
	CX, CY           float64
	Largura, Altura  int
	// Modo: "padrao" (números da seção 3.2 escalados pela tela), "deslocamento"
	// (só X0/Y0 ajustados: poucos pontos) ou "completo".
	Modo        string
	Pontos      int
	Descartados int
	ErroMediano *float64
	ErroMaximo  *float64
}

// CalibracaoPadrao são os coeficientes medidos a 2× numa tela 2560×1440,
// com os por-pixel escalados para a tela atual.
func CalibracaoPadrao(larg, alt int) Calibracao {
	if larg <= 0 || alt <= 0 {
		larg, alt = replay.LarguraRef, replay.AlturaRef
	}
	return Calibracao{
		X0: replay.CoefX0, XPx: replay.CoefXPx * replay.LarguraRef / float64(larg),
		Y0: replay.CoefY0, YPx: replay.CoefYPx * replay.AlturaRef / float64(alt),
		CX: float64(larg) / 2, CY: float64(alt) / 2, Largura: larg, Altura: alt, Modo: "padrao",
	}
}

// Posicao aplica a fórmula.
func (c Calibracao) Posicao(camX, camZ, sx, sy float64) (float64, float64) {
	return camX + c.X0 + c.XPx*(sx-c.CX), camZ + c.Y0 - c.YPx*(sy-c.CY)
}

// PontoCal é um par (leitura da câmera, posição exata da linha do tempo).
type PontoCal struct {
	TMs                int64
	CamX, CamZ, SX, SY float64
	TlX, TlY           float64
}

// MinPontosCompleto: abaixo disso só os deslocamentos são ajustados (quatro
// coeficientes com quatro pontos decoram o ruído).
const MinPontosCompleto = 8

// Ajusta refaz a calibração por mínimos quadrados contra os pontos, partindo
// de base. Um ponto muito fora (acima de 3× a mediana e de 250 unidades) é
// descartado e o ajuste refeito uma vez: é a leitura com a câmera ainda
// chegando ou o carimbo do quadro longe da leitura.
func Ajusta(pontos []PontoCal, base Calibracao) Calibracao {
	c := base
	c.Pontos = len(pontos)
	if len(pontos) == 0 {
		return c
	}
	if len(pontos) >= 2 {
		c = ajustaUmaVez(pontos, base)
		erros := errosDe(c, pontos)
		corte := math.Max(250, 3*mediana(erros))
		var bons []PontoCal
		for i, p := range pontos {
			if erros[i] <= corte {
				bons = append(bons, p)
			}
		}
		if len(bons) < len(pontos) && len(bons) >= 2 {
			c = ajustaUmaVez(bons, base)
			c.Descartados = len(pontos) - len(bons)
			pontos = bons
		}
	}
	erros := errosDe(c, pontos)
	med, mx := mediana(erros), maximo(erros)
	c.ErroMediano, c.ErroMaximo = &med, &mx
	c.Pontos = len(pontos)
	return c
}

func ajustaUmaVez(ps []PontoCal, base Calibracao) Calibracao {
	c := base
	n := float64(len(ps))
	var dxs, rxs, dys, rys []float64
	for _, p := range ps {
		dxs = append(dxs, p.SX-c.CX)
		rxs = append(rxs, p.TlX-p.CamX)
		dys = append(dys, -(p.SY - c.CY))
		rys = append(rys, p.TlY-p.CamZ)
	}
	completo := len(ps) >= MinPontosCompleto && variancia(dxs) > 400 && variancia(dys) > 400
	if completo {
		c.X0, c.XPx = retaMQ(dxs, rxs)
		c.Y0, c.YPx = retaMQ(dys, rys)
		c.Modo = "completo"
		// Inclinação absurda (pontos quase todos no centro): volta à de base.
		if c.XPx <= 0 || c.YPx <= 0 || c.XPx > 4*base.XPx || c.YPx > 4*base.YPx {
			completo = false
		}
	}
	if !completo {
		c.XPx, c.YPx = base.XPx, base.YPx
		sx, sy := 0.0, 0.0
		for i := range ps {
			sx += rxs[i] - c.XPx*dxs[i]
			sy += rys[i] - c.YPx*dys[i]
		}
		c.X0, c.Y0 = sx/n, sy/n
		c.Modo = "deslocamento"
	}
	return c
}

// retaMQ ajusta r = a + b·d.
func retaMQ(d, r []float64) (a, b float64) {
	n := float64(len(d))
	md, mr := media(d), media(r)
	var sdd, sdr float64
	for i := range d {
		sdd += (d[i] - md) * (d[i] - md)
		sdr += (d[i] - md) * (r[i] - mr)
	}
	if sdd == 0 || n < 2 {
		return mr, 0
	}
	b = sdr / sdd
	return mr - b*md, b
}

func errosDe(c Calibracao, ps []PontoCal) []float64 {
	e := make([]float64, len(ps))
	for i, p := range ps {
		x, y := c.Posicao(p.CamX, p.CamZ, p.SX, p.SY)
		e[i] = math.Hypot(x-p.TlX, y-p.TlY)
	}
	return e
}

func media(v []float64) float64 {
	s := 0.0
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}

func variancia(v []float64) float64 {
	m := media(v)
	s := 0.0
	for _, x := range v {
		s += (x - m) * (x - m)
	}
	return s / float64(len(v))
}

func mediana(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}

func maximo(v []float64) float64 {
	m := 0.0
	for _, x := range v {
		m = math.Max(m, x)
	}
	return m
}

// usavelNaCalibracao: amostra com o jogador vivo, a seleção certa e câmera e
// tela lidas.
func usavelNaCalibracao(a Amostra) bool {
	return !a.Morto && a.SelecaoOK && !a.TravaPerdida && a.CamX != nil && a.CamZ != nil && a.TelaX != nil && a.TelaY != nil
}

// PontosDoPasse casa as amostras com os quadros da linha do tempo: para cada
// quadro, interpola câmera e tela entre a amostra de antes e a de depois
// (até 700 ms de jogo entre elas). Quadro com o jogador morto perto (±3 s)
// fica de fora: a câmera não está nele.
func PontosDoPasse(as []Amostra, lt LinhaDoTempo, pid int) []PontoCal {
	var ps []PontoCal
	for _, q := range lt.Quadros {
		j, ok := q.Jogadores[pid]
		if !ok || q.TMs <= 0 {
			continue
		}
		i := sort.Search(len(as), func(k int) bool { return as[k].TMs >= q.TMs })
		if i == 0 || i >= len(as) {
			continue
		}
		a, b := as[i-1], as[i]
		if b.TMs-a.TMs > 700 || !usavelNaCalibracao(a) || !usavelNaCalibracao(b) {
			continue
		}
		morto := false
		for k := i - 1; k >= 0 && as[k].TMs >= q.TMs-3000; k-- {
			morto = morto || as[k].Morto
		}
		for k := i; k < len(as) && as[k].TMs <= q.TMs+3000; k++ {
			morto = morto || as[k].Morto
		}
		if morto {
			continue
		}
		f := 0.0
		if b.TMs > a.TMs {
			f = float64(q.TMs-a.TMs) / float64(b.TMs-a.TMs)
		}
		lerp := func(u, v *float64) float64 { return *u + (*v-*u)*f }
		ps = append(ps, PontoCal{TMs: q.TMs, CamX: lerp(a.CamX, b.CamX), CamZ: lerp(a.CamZ, b.CamZ),
			SX: lerp(a.TelaX, b.TelaX), SY: lerp(a.TelaY, b.TelaY), TlX: j.X, TlY: j.Y})
	}
	return ps
}
