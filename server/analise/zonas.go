// Package analise tira do que o rastreio gravou o que a pessoa quer saber: em
// que zona do mapa cada um estava, quando saiu da rota, o que estava fazendo
// (atividade deduzida), como estava cada um dos 10 em cada abate e os
// indicadores por jogador e partida. Tudo é conta e regra determinística (sem
// IA) e tudo é RECALCULÁVEL a partir do bloco compactado e das linhas do
// tempo: Recalcula(partida) apaga e refaz.
package analise

import (
	"math"
)

// Ponto é uma coordenada do jogo.
type Ponto struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Vocabulário das zonas (fixo; documentado em docs/arquitetura/dados.md).
const (
	ZBaseAzul           = "base_azul"
	ZBaseVermelha       = "base_vermelha"
	ZRotaTopo           = "rota_topo"
	ZRotaMeio           = "rota_meio"
	ZRotaBaixo          = "rota_baixo"
	ZRioCima            = "rio_cima"
	ZRioBaixo           = "rio_baixo"
	ZCovaBarao          = "cova_barao"
	ZCovaDragao         = "cova_dragao"
	ZSelvaAzulCima      = "selva_azul_cima"
	ZSelvaAzulBaixo     = "selva_azul_baixo"
	ZSelvaVermelhaCima  = "selva_vermelha_cima"
	ZSelvaVermelhaBaixo = "selva_vermelha_baixo"
	ZForaDoMapa         = "fora_do_mapa"
)

// Subzonas.
const (
	SFonte        = "fonte"
	SBase         = "base"
	SLadoAzul     = "lado_azul"
	SCentro       = "centro"
	SLadoVermelho = "lado_vermelho"
	SRio          = "rio"
	SCova         = "cova"
	SArredores    = "arredores"
	SSelva        = "selva"
)

// InfoZona descreve uma zona para a tela.
type InfoZona struct {
	Codigo   string   `json:"codigo"`
	Nome     string   `json:"nome"`
	Tipo     string   `json:"tipo"` // base, rota, rio, cova, selva, fora
	Lado     string   `json:"lado"` // azul, vermelho ou "" (rotas, rios, covas)
	Subzonas []string `json:"subzonas"`
	Cor      string   `json:"cor"` // sugestão para o mapa (a da imagem de conferência)
}

// Zonas é o vocabulário, na ordem do índice da grade.
var Zonas = []InfoZona{
	{ZBaseAzul, "base azul", "base", "azul", []string{SFonte, SBase}, "#3c78dc"},
	{ZBaseVermelha, "base vermelha", "base", "vermelho", []string{SFonte, SBase}, "#dc3c46"},
	{ZRotaTopo, "rota do topo", "rota", "", []string{SLadoAzul, SCentro, SLadoVermelho}, "#f0c828"},
	{ZRotaMeio, "rota do meio", "rota", "", []string{SLadoAzul, SCentro, SLadoVermelho}, "#f08c1e"},
	{ZRotaBaixo, "rota de baixo", "rota", "", []string{SLadoAzul, SCentro, SLadoVermelho}, "#d2dc32"},
	{ZRioCima, "rio de cima", "rio", "", []string{SRio}, "#28c8f0"},
	{ZRioBaixo, "rio de baixo", "rio", "", []string{SRio}, "#1e96e6"},
	{ZCovaBarao, "cova do Barão", "cova", "", []string{SCova, SArredores}, "#a03cdc"},
	{ZCovaDragao, "cova do Dragão", "cova", "", []string{SCova, SArredores}, "#e650b4"},
	{ZSelvaAzulCima, "selva azul de cima", "selva", "azul", []string{SSelva}, "#32b464"},
	{ZSelvaAzulBaixo, "selva azul de baixo", "selva", "azul", []string{SSelva}, "#1e8c5a"},
	{ZSelvaVermelhaCima, "selva vermelha de cima", "selva", "vermelho", []string{SSelva}, "#b4823c"},
	{ZSelvaVermelhaBaixo, "selva vermelha de baixo", "selva", "vermelho", []string{SSelva}, "#8c5a32"},
	{ZForaDoMapa, "fora do mapa", "fora", "", []string{}, "#000000"},
}

// NomesSubzona são os rótulos das subzonas.
var NomesSubzona = map[string]string{
	SFonte: "fonte", SBase: "base", SLadoAzul: "lado azul", SCentro: "centro", SLadoVermelho: "lado vermelho",
	SRio: "rio", SCova: "cova", SArredores: "arredores", SSelva: "selva",
}

var infoPorCodigo = func() map[string]InfoZona {
	m := map[string]InfoZona{}
	for _, z := range Zonas {
		m[z.Codigo] = z
	}
	return m
}()

// Info devolve a descrição de uma zona (vazia se não existe).
func Info(codigo string) InfoZona { return infoPorCodigo[codigo] }

// TipoZona: base, rota, rio, cova, selva ou fora.
func TipoZona(codigo string) string { return infoPorCodigo[codigo].Tipo }

// espelho leva um ponto do lado vermelho para o azul (e vice-versa).
func espelho(p Ponto) Ponto { return Ponto{Simetria.X - p.X, Simetria.Y - p.Y} }

func dist(a, b Ponto) float64 { return math.Hypot(a.X-b.X, a.Y-b.Y) }

// projecao devolve a distância de p à polilinha e o comprimento ao longo dela
// do ponto mais perto (para os trechos da rota).
func projecao(p Ponto, linha []Ponto) (d, ao float64) {
	d = math.Inf(1)
	acum := 0.0
	for i := 0; i+1 < len(linha); i++ {
		a, b := linha[i], linha[i+1]
		dx, dy := b.X-a.X, b.Y-a.Y
		l2 := dx*dx + dy*dy
		t := 0.0
		if l2 > 0 {
			t = math.Max(0, math.Min(1, ((p.X-a.X)*dx+(p.Y-a.Y)*dy)/l2))
		}
		q := Ponto{a.X + t*dx, a.Y + t*dy}
		if dq := dist(p, q); dq < d {
			d, ao = dq, acum+t*math.Sqrt(l2)
		}
		acum += math.Sqrt(l2)
	}
	return d, ao
}

// trecho pelo comprimento ao longo da rota, comparado às torres externas.
func trecho(ao, aoAzul, aoVermelha float64) string {
	switch {
	case ao < aoAzul:
		return SLadoAzul
	case ao > aoVermelha:
		return SLadoVermelho
	}
	return SCentro
}

// noTopo: o ponto está na rota do topo? Devolve o trecho.
func noTopo(p Ponto) (bool, string, float64) {
	d, ao := projecao(p, RotaTopo)
	canto := (p.X-CantoTopo.X)+(CantoTopo.Y-p.Y) < CorteCantoRota
	if d > MeiaLarguraRotaLateral && !canto {
		return false, "", d
	}
	_, aoA := projecao(TorreExternaTopoAzul, RotaTopo)
	_, aoV := projecao(TorreExternaTopoVermelha, RotaTopo)
	if canto {
		d = math.Min(d, MeiaLarguraRotaLateral)
	}
	return true, trecho(ao, aoA, aoV), d
}

func noMeio(p Ponto) (bool, string, float64) {
	d, ao := projecao(p, RotaMeio)
	if d > MeiaLarguraRotaMeio {
		return false, "", d
	}
	_, aoA := projecao(TorreExternaMeioAzul, RotaMeio)
	_, aoV := projecao(TorreExternaMeioVermelha, RotaMeio)
	return true, trecho(ao, aoA, aoV), d
}

// rioCompleto: rio de cima, centro e rio de baixo (espelhado), estendido até
// os cantos, para saber de que lado do rio um ponto está.
var rioCompleto = func() []Ponto {
	l := []Ponto{CantoTopo}
	l = append(l, RioCima...)
	for i := len(RioCima) - 2; i >= 0; i-- {
		l = append(l, espelho(RioCima[i]))
	}
	return append(l, espelho(CantoTopo))
}()

// ladoAzulDoRio: o ponto está do lado da base azul do rio (pelo segmento do rio
// mais perto; o rio vai do canto de cima à esquerda ao de baixo à direita).
func ladoAzulDoRio(p Ponto) bool {
	melhor, cruz := math.Inf(1), 0.0
	for i := 0; i+1 < len(rioCompleto); i++ {
		a, b := rioCompleto[i], rioCompleto[i+1]
		dx, dy := b.X-a.X, b.Y-a.Y
		l2 := dx*dx + dy*dy
		t := math.Max(0, math.Min(1, ((p.X-a.X)*dx+(p.Y-a.Y)*dy)/l2))
		if d := dist(p, Ponto{a.X + t*dx, a.Y + t*dy}); d < melhor {
			melhor, cruz = d, dx*(p.Y-a.Y)-dy*(p.X-a.X)
		}
	}
	return cruz < 0
}

// deCima: acima da diagonal do meio (o lado do topo).
func deCima(p Ponto) bool {
	c := Ponto{Simetria.X / 2, Simetria.Y / 2}
	return (p.Y - c.Y) > (p.X - c.X)
}

// Zona devolve a zona e a subzona de um ponto do mapa 11. Ordem de decisão
// (o primeiro que couber): fora do mapa, base (fonte), rota (trecho), cova
// (cova/arredores), rio, selva (pelo lado do rio e da diagonal do meio).
func Zona(x, y float64) (zona, subzona string) {
	p := Ponto{x, y}
	if math.IsNaN(x) || math.IsNaN(y) || x < MapaMin.X-200 || y < MapaMin.Y-200 || x > MapaMax.X+200 || y > MapaMax.Y+200 {
		return ZForaDoMapa, ""
	}
	if dist(p, CantoBaseAzul) < RaioBase {
		if dist(p, FonteAzul) < RaioFonte {
			return ZBaseAzul, SFonte
		}
		return ZBaseAzul, SBase
	}
	if dist(espelho(p), CantoBaseAzul) < RaioBase {
		if dist(p, FonteVermelha) < RaioFonte {
			return ZBaseVermelha, SFonte
		}
		return ZBaseVermelha, SBase
	}
	if foraDaArea(p) {
		return ZForaDoMapa, ""
	}
	// Rotas: a mais perto entre as que aceitam o ponto.
	melhorD, melhorZ, melhorT := math.Inf(1), "", ""
	if ok, t, d := noTopo(p); ok && d < melhorD {
		melhorD, melhorZ, melhorT = d, ZRotaTopo, t
	}
	if ok, t, d := noTopo(espelho(p)); ok && d < melhorD {
		// Espelhada, o lado azul da rota do topo é o lado vermelho da de baixo.
		switch t {
		case SLadoAzul:
			t = SLadoVermelho
		case SLadoVermelho:
			t = SLadoAzul
		}
		melhorD, melhorZ, melhorT = d, ZRotaBaixo, t
	}
	if ok, t, d := noMeio(p); ok && d < melhorD {
		melhorD, melhorZ, melhorT = d, ZRotaMeio, t
	}
	if melhorZ != "" {
		return melhorZ, melhorT
	}
	if d := dist(p, CovaBarao); d < RaioArredoresCova {
		if d < RaioCova {
			return ZCovaBarao, SCova
		}
		return ZCovaBarao, SArredores
	}
	if d := dist(p, CovaDragao); d < RaioArredoresCova {
		if d < RaioCova {
			return ZCovaDragao, SCova
		}
		return ZCovaDragao, SArredores
	}
	cima := deCima(p)
	if d, _ := projecao(p, RioCima); d < MeiaLarguraRio && cima {
		return ZRioCima, SRio
	}
	if d, _ := projecao(espelho(p), RioCima); d < MeiaLarguraRio && !cima {
		return ZRioBaixo, SRio
	}
	azul := ladoAzulDoRio(p)
	switch {
	case azul && cima:
		return ZSelvaAzulCima, SSelva
	case azul:
		return ZSelvaAzulBaixo, SSelva
	case cima:
		return ZSelvaVermelhaCima, SSelva
	}
	return ZSelvaVermelhaBaixo, SSelva
}

// RotaDaPosicao: a zona de rota do jogador pela posição dele na partida
// (TOP, MIDDLE, BOTTOM, UTILITY). Caçador (JUNGLE) e posição vazia: "".
func RotaDaPosicao(posicao string) string {
	switch posicao {
	case "TOP":
		return ZRotaTopo
	case "MIDDLE":
		return ZRotaMeio
	case "BOTTOM", "UTILITY":
		return ZRotaBaixo
	}
	return ""
}

// GrupoRelativo põe a zona do ponto de vista de um jogador: rota_propria,
// outra_rota, rio, covas, selva_propria, selva_inimiga, base_propria,
// base_inimiga, fora. time 100 = azul.
func GrupoRelativo(zona, rotaPropria string, time int) string {
	info := infoPorCodigo[zona]
	meuLado := "azul"
	if time == 200 {
		meuLado = "vermelho"
	}
	switch info.Tipo {
	case "rota":
		if zona == rotaPropria {
			return "rota_propria"
		}
		return "outra_rota"
	case "rio":
		return "rio"
	case "cova":
		return "covas"
	case "selva":
		if info.Lado == meuLado {
			return "selva_propria"
		}
		return "selva_inimiga"
	case "base":
		if info.Lado == meuLado {
			return "base_propria"
		}
		return "base_inimiga"
	}
	return "fora"
}

// GruposRelativos é o vocabulário de GrupoRelativo.
var GruposRelativos = []string{"rota_propria", "outra_rota", "rio", "covas", "selva_propria", "selva_inimiga",
	"base_propria", "base_inimiga"}

// Grade é o mapa das zonas em células, para a tela desenhar sem refazer a
// geometria: Linhas[i][j] é o índice (em Codigos) da zona no centro da célula
// da coluna j e da linha i, a linha 0 embaixo (y mínimo).
type Grade struct {
	Origem  Ponto    `json:"origem"`
	Celula  float64  `json:"celula"`
	Colunas int      `json:"colunas"`
	NLinhas int      `json:"n_linhas"`
	Codigos []string `json:"codigos"`
	Linhas  []string `json:"linhas"`
	Legenda string   `json:"legenda"`
}

// GeraGrade monta a grade com células de `celula` unidades. Cada caractere de
// uma linha é 'A' + índice da zona em Codigos.
func GeraGrade(celula float64) Grade {
	g := Grade{Origem: MapaMin, Celula: celula, Legenda: "caractere = 'A' + índice em codigos; linha 0 = y mínimo"}
	g.Colunas = int(math.Ceil((MapaMax.X - MapaMin.X) / celula))
	g.NLinhas = int(math.Ceil((MapaMax.Y - MapaMin.Y) / celula))
	idx := map[string]int{}
	for i, z := range Zonas {
		g.Codigos = append(g.Codigos, z.Codigo)
		idx[z.Codigo] = i
	}
	for i := 0; i < g.NLinhas; i++ {
		b := make([]byte, g.Colunas)
		for j := 0; j < g.Colunas; j++ {
			z, _ := Zona(MapaMin.X+(float64(j)+0.5)*celula, MapaMin.Y+(float64(i)+0.5)*celula)
			b[j] = byte('A' + idx[z])
		}
		g.Linhas = append(g.Linhas, string(b))
	}
	return g
}

// foraDaArea: fora do quadrado arredondado jogável (as bases já foram vistas).
func foraDaArea(p Ponto) bool {
	e := espelho(p)
	if p.X < LimiteArea || p.Y < LimiteArea || e.X < LimiteArea || e.Y < LimiteArea {
		return true
	}
	canto := func(q Ponto) bool {
		return q.X < ArcoCantoCentro.X && q.Y > ArcoCantoCentro.Y && dist(q, ArcoCantoCentro) > ArcoCantoRaio
	}
	return canto(p) || canto(espelho(p))
}
