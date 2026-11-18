package analise

import (
	"fmt"
	"math"
	"sort"

	"loltracking/server/rastreio"
)

// O ESTADO DA ONDA DE TROPAS em cada rota, sem rastrear tropa nenhuma: tudo sai
// do que já está gravado (posição dos jogadores rastreados, eventos, linha do
// tempo). O relógio (ondas_relogio.go) diz quando cada onda chega; a posição
// dos jogadores da rota diz onde a onda está; e a sequência das posições, de
// onda em onda, diz o estado: freeze, onda na torre, onda voltando, empurrando
// ou parada. Vocabulário em docs/arquitetura/dados.md (seção "ondas").
//
// LIMITES (ditos na tela e no relatório): sem contar tropas não dá para saber
// o tamanho da onda; "devagar/rápido" é só a velocidade do avanço. Depois da
// fase de rotas os jogadores saem das rotas e a estimativa perde sentido: só
// se marca enquanto há jogador da rota na rota, farmando, e nunca depois dos
// 14 min (FaseRotasMs).

// Estados da onda (vocabulário).
const (
	EOndaFreeze   = "freeze"
	EOndaNaTorre  = "onda_na_torre"
	EOndaVoltando = "onda_voltando"
	EOndaEmpurra  = "empurrando"
	// EOndaParada: a onda ficou no mesmo lugar por duas ondas ou mais, mas não
	// é freeze (parada no meio da rota, ou perto de uma torre sem o jogador
	// daquele time rastreado farmando ali). Não estava no pedido; sem ele a
	// janela parada ficava sem estado.
	EOndaParada  = "onda_parada"
	EOndaPerdida = "onda_perdida"
)

// EstadosOnda é o vocabulário dos trechos (onda_perdida é por jogador).
var EstadosOnda = []string{EOndaFreeze, EOndaNaTorre, EOndaVoltando, EOndaEmpurra, EOndaParada}

// --- Eixo da rota ------------------------------------------------------------

// EixoRota é a coordenada ao longo de uma rota, da base azul (0) à vermelha
// (Comprimento). Topo e baixo fazem a curva no canto (RotaTopo e o espelho
// dela); o meio é a diagonal (RotaMeio). As torres vêm de TorresDasRotas.
type EixoRota struct {
	Rota        string        `json:"rota"`
	Comprimento float64       `json:"comprimento"`
	Encontro    float64       `json:"encontro_s"` // ponto de encontro natural: o meio entre as torres externas
	Torres      []TorreNoEixo `json:"torres"`
}

// TorreNoEixo é uma torre com a coordenada dela no eixo.
type TorreNoEixo struct {
	Time  int     `json:"time"`
	Ordem int     `json:"ordem"`
	S     float64 `json:"s"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
}

func comprimento(l []Ponto) float64 {
	t := 0.0
	for i := 0; i+1 < len(l); i++ {
		t += dist(l[i], l[i+1])
	}
	return t
}

// pontoNaLinha: o ponto a `ao` do começo da polilinha.
func pontoNaLinha(l []Ponto, ao float64) Ponto {
	if ao <= 0 {
		return l[0]
	}
	for i := 0; i+1 < len(l); i++ {
		d := dist(l[i], l[i+1])
		if ao <= d {
			f := ao / d
			return Ponto{l[i].X + (l[i+1].X-l[i].X)*f, l[i].Y + (l[i+1].Y-l[i].Y)*f}
		}
		ao -= d
	}
	return l[len(l)-1]
}

// S devolve a coordenada de p ao longo da rota e a distância dele à linha.
func (e EixoRota) S(p Ponto) (s, d float64) {
	switch e.Rota {
	case ZRotaMeio:
		d, s = projecao(p, RotaMeio)
	case ZRotaBaixo:
		var ao float64
		d, ao = projecao(espelho(p), RotaTopo)
		s = e.Comprimento - ao
	default:
		d, s = projecao(p, RotaTopo)
	}
	return s, d
}

// Ponto é o lugar do mapa na coordenada s da rota.
func (e EixoRota) Ponto(s float64) Ponto {
	switch e.Rota {
	case ZRotaMeio:
		return pontoNaLinha(RotaMeio, s)
	case ZRotaBaixo:
		return espelho(pontoNaLinha(RotaTopo, e.Comprimento-s))
	}
	return pontoNaLinha(RotaTopo, s)
}

// NovoEixo monta o eixo da rota (rota_topo, rota_meio, rota_baixo).
func NovoEixo(rota string) EixoRota {
	e := EixoRota{Rota: rota, Comprimento: comprimento(RotaTopo)}
	if rota == ZRotaMeio {
		e.Comprimento = comprimento(RotaMeio)
	}
	var ext [2]float64
	for _, t := range TorresDasRotas {
		if t.Rota != rota {
			continue
		}
		s, _ := e.S(t.P)
		e.Torres = append(e.Torres, TorreNoEixo{Time: t.Time, Ordem: t.Ordem, S: math.Round(s), X: t.P.X, Y: t.P.Y})
		if t.Ordem == 1 {
			ext[t.Time/100-1] = s
		}
	}
	e.Encontro = math.Round((ext[0] + ext[1]) / 2)
	return e
}

// torreDePe: a torre mais avançada do time na rota que ainda não caiu em t.
func (e EixoRota) torreDePe(time int, caidas map[[2]int]int64, t int64) (TorreNoEixo, bool) {
	for ordem := 1; ordem <= 3; ordem++ {
		for _, tr := range e.Torres {
			if tr.Time != time || tr.Ordem != ordem {
				continue
			}
			if tc, ok := caidas[[2]int{time, ordem}]; ok && tc <= t {
				break
			}
			return tr, true
		}
	}
	return TorreNoEixo{}, false
}

// torre: a torre do time e da ordem no eixo.
func (e EixoRota) torre(time, ordem int) (TorreNoEixo, bool) {
	for _, tr := range e.Torres {
		if tr.Time == time && tr.Ordem == ordem {
			return tr, true
		}
	}
	return TorreNoEixo{}, false
}

// torreDoEvento: qual torre de rota (rota, time, ordem) está no lugar do
// evento (placa ou torre derrubada), pela torre mais perto a até 1000.
func torreDoEvento(e rastreio.EventoLT) (TorreDaRota, bool) {
	if e.X == nil || e.Y == nil {
		return TorreDaRota{}, false
	}
	p := Ponto{*e.X, *e.Y}
	melhor, achou := 1000.0, TorreDaRota{}
	ok := false
	for _, t := range TorresDasRotas {
		if d := dist(p, t.P); d < melhor {
			melhor, achou, ok = d, t, true
		}
	}
	return achou, ok
}

// --- Entrada e saída ------------------------------------------------------------

// EntradaOndas é o que a dedução das ondas usa de uma partida.
type EntradaOndas struct {
	// SaidaTropasMs: a primeira saída das tropas (evento MinionsSpawning);
	// SaidaMedida falso = não gravado, usada a regra do patch.
	SaidaTropasMs int64
	SaidaMedida   bool
	Times         map[int]int
	Posicoes      map[int]string
	// Amostras só dos rastreados (posição de verdade).
	Amostras map[int][]rastreio.Amostra
	// Eventos: linha do tempo do LCU + placas da Riot, em ordem de tempo.
	Eventos []rastreio.EventoLT
	Linha   rastreio.LinhaDoTempo
}

// OndasDaPartida é o resultado (GET /api/partidas/{id}/ondas).
type OndasDaPartida struct {
	SaidaTropasMs  int64         `json:"saida_tropas_ms"`
	SaidaMedida    bool          `json:"saida_medida"`
	FimFaseRotasMs int64         `json:"fim_fase_rotas_ms"`
	AteMs          int64         `json:"ate_ms"`
	Ondas          []Onda        `json:"ondas"`
	Rotas          []OndasDaRota `json:"rotas"`
	Regras         string        `json:"regras"`
	Limites        []string      `json:"limites"`
}

// OndasDaRota é o que se deduziu de uma rota.
type OndasDaRota struct {
	Rota       string        `json:"rota"`
	Rastreados []int         `json:"rastreados"`
	Eixo       EixoRota      `json:"eixo"`
	Pontos     []PontoOnda   `json:"pontos"`
	Janelas    []JanelaOnda  `json:"janelas"`
	Estados    []TrechoOnda  `json:"estados"`
	Perdidas   []OndaPerdida `json:"perdidas"`
}

// PontoOnda: a posição estimada da onda num instante.
type PontoOnda struct {
	TMs       int64   `json:"t_ms"`
	S         float64 `json:"s"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Confianca string  `json:"confianca"` // alta: os dois lados na rota; media: um lado só
}

// JanelaOnda: do momento em que uma onda chega ao ponto de encontro até a
// chegada da seguinte.
type JanelaOnda struct {
	Onda      int      `json:"onda"`
	Canhao    bool     `json:"canhao"`
	DeMs      int64    `json:"de_ms"`
	AteMs     int64    `json:"ate_ms"`
	S         *float64 `json:"s"`
	Confianca string   `json:"confianca"`
	Estado    string   `json:"estado"`
	Time      int      `json:"time"`
	Avanco    *float64 `json:"avanco"` // unidades por onda, + = para a base vermelha
	Sinais    []string `json:"sinais"`
	lados     map[int]int
	// sTorre: a torre da placa/torre que caiu na janela (para o trecho ter
	// lugar mesmo sem posição estimada).
	sTorre *float64
}

// TrechoOnda: janelas seguidas no mesmo estado.
type TrechoOnda struct {
	Estado string `json:"estado"`
	DeMs   int64  `json:"de_ms"`
	AteMs  int64  `json:"ate_ms"`
	Ondas  []int  `json:"ondas"`
	// Time: freeze = o time que segura a onda perto da própria torre; na torre
	// = o DONO da torre; voltando = o dono da torre de onde a onda volta;
	// empurrando = quem empurra (a onda anda para a torre do outro); parada =
	// o time de cuja torre ela está mais perto (0 = no meio).
	Time       int      `json:"time"`
	Velocidade string   `json:"velocidade,omitempty"` // lenta, rapida (empurrando/voltando)
	Avanco     *float64 `json:"avanco_por_onda,omitempty"`
	Confianca  string   `json:"confianca"`
	S          float64  `json:"s"`
	X          float64  `json:"x"`
	Y          float64  `json:"y"`
	Sinais     []string `json:"sinais"`
}

// OndaPerdida: a onda chegou e o jogador da rota não estava lá.
type OndaPerdida struct {
	ParticipanteID int     `json:"participante_id"`
	Onda           int     `json:"onda"`
	Canhao         bool    `json:"canhao"`
	TMs            int64   `json:"t_ms"`
	Motivo         string  `json:"motivo"` // morto, na_base, longe
	Zona           string  `json:"zona"`
	Distancia      float64 `json:"distancia"` // da posição da onda, em linha reta (0 se morto)
	OndaS          float64 `json:"onda_s"`
	PosicaoOnda    string  `json:"posicao_onda"` // estimada ou encontro (sem estimativa: o ponto de encontro natural)
	X              float64 `json:"x"`
	Y              float64 `json:"y"`
}

// LimitesOndas é o que a tela e o relatório têm de dizer.
var LimitesOndas = []string{
	"Nenhuma tropa é rastreada: a posição da onda é deduzida de onde os jogadores rastreados da rota estão.",
	"Sem contar tropas não dá para saber o tamanho da onda: \"devagar/rápido\" é só a velocidade do avanço de uma onda para a outra.",
	"Só entra quem foi rastreado; com os dois lados na rota a confiança é alta, com um lado só é média, sem ninguém a posição é desconhecida.",
	"Depois da fase de rotas só se marca enquanto o jogador da rota está nela farmando, e nunca depois dos 14 min.",
	"Os tempos de chegada das ondas saem do relógio do patch 26.x; a distância até o ponto de encontro é estimativa.",
}

// rotasDoJogo e as posições de cada rota.
var rotasDoJogo = []string{ZRotaTopo, ZRotaMeio, ZRotaBaixo}

func daRota(rota, posicao string) bool {
	return RotaDaPosicao(posicao) == rota
}

// --- A dedução -----------------------------------------------------------------

// DeduzOndas calcula tudo. Determinística e pura: o gerador da cadeia e a rota
// GET .../ondas chamam com a mesma entrada e chegam ao mesmo resultado.
func DeduzOndas(in EntradaOndas) OndasDaPartida {
	fimRotas, _ := FimDaFaseDeRotas(in.Eventos)
	ate := int64(0)
	for _, as := range in.Amostras {
		if n := len(as); n > 0 && as[n-1].TMs > ate {
			ate = as[n-1].TMs
		}
	}
	ate = min(ate, Param.FaseRotasMs)
	saida := in.SaidaTropasMs
	if saida <= 0 {
		saida = RegrasOndas.SaidaPadraoMs
	}
	r := OndasDaPartida{SaidaTropasMs: saida, SaidaMedida: in.SaidaMedida, FimFaseRotasMs: fimRotas, AteMs: ate,
		Ondas: RelogioDasOndas(saida, ate), Regras: RegrasOndas.Fonte, Limites: LimitesOndas}
	if r.Ondas == nil {
		r.Ondas = []Onda{}
	}
	caidas := map[string]map[[2]int]int64{}
	for _, rota := range rotasDoJogo {
		caidas[rota] = map[[2]int]int64{}
	}
	for _, e := range in.Eventos {
		if e.Tipo != "BUILDING_KILL" {
			continue
		}
		if t, ok := torreDoEvento(e); ok {
			k := [2]int{t.Time, t.Ordem}
			if _, ja := caidas[t.Rota][k]; !ja {
				caidas[t.Rota][k] = e.TMs
			}
		}
	}
	for _, rota := range rotasDoJogo {
		r.Rotas = append(r.Rotas, deduzRota(in, rota, r.Ondas, fimRotas, ate, caidas[rota]))
	}
	return r
}

// estimativaCrua: a posição da onda em t pelos jogadores da rota presentes.
type estimativaCrua struct {
	s     float64
	alta  bool
	lados map[int]bool
}

type deducaoRota struct {
	in       EntradaOndas
	eixo     EixoRota
	rota     string
	da       map[int][]int // time → rastreados da rota
	outros   []int         // rastreados de fora da rota (para a luta)
	fimRotas int64
	caidas   map[[2]int]int64
}

func deduzRota(in EntradaOndas, rota string, ondas []Onda, fimRotas, ate int64, caidas map[[2]int]int64) OndasDaRota {
	d := &deducaoRota{in: in, eixo: NovoEixo(rota), rota: rota, da: map[int][]int{}, fimRotas: fimRotas, caidas: caidas}
	res := OndasDaRota{Rota: rota, Eixo: d.eixo, Rastreados: []int{}, Pontos: []PontoOnda{}, Janelas: []JanelaOnda{},
		Estados: []TrechoOnda{}, Perdidas: []OndaPerdida{}}
	var pids []int
	for pid := range in.Amostras {
		pids = append(pids, pid)
	}
	sort.Ints(pids)
	for _, pid := range pids {
		if daRota(rota, in.Posicoes[pid]) {
			d.da[in.Times[pid]] = append(d.da[in.Times[pid]], pid)
			res.Rastreados = append(res.Rastreados, pid)
		} else {
			d.outros = append(d.outros, pid)
		}
	}
	if len(res.Rastreados) == 0 || len(ondas) == 0 {
		return res
	}
	// 1. Estimativa crua a cada passo e a mediana na janela de suavização.
	ini := ondas[0].Chegada[rota]
	var ts []int64
	var cruas []*estimativaCrua
	for t := ini; t <= ate; t += Param.PassoOndaMs {
		ts = append(ts, t)
		cruas = append(cruas, d.crua(t))
	}
	meia := int((Param.SuavizaOndaMs / 2) / Param.PassoOndaMs)
	for k := range ts {
		if cruas[k] == nil {
			continue
		}
		var ss []float64
		altas, total := 0, 0
		for j := max(0, k-meia); j <= min(len(ts)-1, k+meia); j++ {
			if c := cruas[j]; c != nil {
				ss = append(ss, c.s)
				total++
				if c.alta {
					altas++
				}
			}
		}
		s := mediana(ss)
		conf := "media"
		if altas*2 > total {
			conf = "alta"
		}
		p := d.eixo.Ponto(s)
		res.Pontos = append(res.Pontos, PontoOnda{TMs: ts[k], S: math.Round(s), X: math.Round(p.X), Y: math.Round(p.Y), Confianca: conf})
	}
	// 2. Janelas de onda.
	for i, o := range ondas {
		de := o.Chegada[rota]
		fim := ate
		if i+1 < len(ondas) {
			fim = min(ondas[i+1].Chegada[rota], ate)
		}
		if de >= fim {
			continue
		}
		j := JanelaOnda{Onda: o.N, Canhao: o.Canhao, DeMs: de, AteMs: fim, Sinais: []string{}, lados: map[int]int{}}
		var ss []float64
		altas := 0
		for _, p := range res.Pontos {
			if p.TMs >= de && p.TMs < fim {
				ss = append(ss, p.S)
				if p.Confianca == "alta" {
					altas++
				}
			}
		}
		for k, t := range ts {
			if t >= de && t < fim && cruas[k] != nil {
				for time := range cruas[k].lados {
					j.lados[time]++
				}
			}
		}
		passos := float64(fim-de) / float64(Param.PassoOndaMs)
		if len(ss) > 0 && float64(len(ss)) >= Param.CoberturaJanelaOnda*passos {
			s := math.Round(mediana(ss))
			j.S = &s
			j.Confianca = "media"
			if altas*2 > len(ss) {
				j.Confianca = "alta"
			}
		}
		res.Janelas = append(res.Janelas, j)
	}
	d.classifica(res.Janelas)
	res.Estados = d.trechos(res.Janelas)
	res.Perdidas = d.perdidas(ondas, res.Pontos, res.Janelas)
	return res
}

func mediana(v []float64) float64 {
	if len(v) == 0 {
		return math.NaN()
	}
	c := append([]float64(nil), v...)
	sort.Float64s(c)
	if len(c)%2 == 1 {
		return c[len(c)/2]
	}
	return (c[len(c)/2-1] + c[len(c)/2]) / 2
}

// posicaoNaRota: onde o pid está em t, se está vivo, na zona da rota, parado
// (não atravessando o mapa) e fora de luta. É a amostra que conta para a onda.
func (d *deducaoRota) posicaoNaRota(pid int, t int64) (float64, bool) {
	as := d.in.Amostras[pid]
	p, ok := PosicaoEm(as, t)
	if !ok {
		return 0, false
	}
	if z, _ := Zona(p.X, p.Y); z != d.rota {
		return 0, false
	}
	// Andando reto (volta da base, indo embora): não está na onda.
	if q, ok := PosicaoEm(as, t-Param.JanelaDeslocamentoMs); ok && dist(p, q) > Param.DeslocamentoAndando {
		return 0, false
	}
	// Luta: um abate perto, ou um inimigo de fora da rota (gank) perto.
	for _, e := range d.in.Linha.Eventos {
		if e.TMs < t-Param.JanelaLutaOndaMs {
			continue
		}
		if e.TMs > t+Param.JanelaLutaOndaMs {
			break
		}
		if e.Tipo == "CHAMPION_KILL" && e.X != nil && dist(p, Ponto{*e.X, *e.Y}) < Param.RaioLutaOnda {
			return 0, false
		}
	}
	for _, o := range d.outros {
		if d.in.Times[o] == d.in.Times[pid] {
			continue
		}
		if q, ok := PosicaoEm(d.in.Amostras[o], t); ok && dist(p, q) < Param.AlcanceLuta {
			return 0, false
		}
	}
	// Depois da fase de rotas só conta quem está farmando (CS subindo no minuto).
	if t >= d.fimRotas {
		if r, ok := csPorMinuto(d.in.Linha, pid, t); !ok || r < Param.CSMinimoPorMin {
			return 0, false
		}
	}
	s, _ := d.eixo.S(p)
	return s, true
}

func (d *deducaoRota) crua(t int64) *estimativaCrua {
	var lado [2][]float64
	for time, pids := range d.da {
		for _, pid := range pids {
			if s, ok := d.posicaoNaRota(pid, t); ok {
				lado[time/100-1] = append(lado[time/100-1], s)
			}
		}
	}
	media := func(v []float64) float64 {
		t := 0.0
		for _, x := range v {
			t += x
		}
		return t / float64(len(v))
	}
	switch {
	case len(lado[0]) > 0 && len(lado[1]) > 0:
		a, v := media(lado[0]), media(lado[1])
		alta := v-a <= Param.SeparacaoMaximaPar && a-v <= Param.MeiaDistanciaOnda
		return &estimativaCrua{s: (a + v) / 2, alta: alta, lados: map[int]bool{100: true, 200: true}}
	case len(lado[0]) > 0:
		// O jogador fica atrás das próprias tropas: a onda está à frente dele.
		return &estimativaCrua{s: media(lado[0]) + Param.MeiaDistanciaOnda, lados: map[int]bool{100: true}}
	case len(lado[1]) > 0:
		return &estimativaCrua{s: media(lado[1]) - Param.MeiaDistanciaOnda, lados: map[int]bool{200: true}}
	}
	return nil
}

func outroTime(t int) int {
	if t == 100 {
		return 200
	}
	return 100
}

func nomeTime(t int) string {
	switch t {
	case 100:
		return "azul"
	case 200:
		return "vermelho"
	}
	return "nenhum"
}

// classifica dá o estado de cada janela com posição.
func (d *deducaoRota) classifica(js []JanelaOnda) {
	// Placas e torres caindo na rota: a onda bateu na torre daquele time.
	for i := range js {
		j := &js[i]
		for _, e := range d.in.Eventos {
			if e.TMs < j.DeMs || e.TMs >= j.AteMs || (e.Tipo != "TURRET_PLATE_DESTROYED" && e.Tipo != "BUILDING_KILL") {
				continue
			}
			if tr, ok := torreDoEvento(e); ok && tr.Rota == d.rota {
				j.Estado, j.Time = EOndaNaTorre, tr.Time
				if tn, ok := d.eixo.torre(tr.Time, tr.Ordem); ok {
					st := tn.S
					j.sTorre = &st
				}
				o := "placa"
				if e.Tipo == "BUILDING_KILL" {
					o = "torre"
				}
				j.Sinais = append(j.Sinais, fmt.Sprintf("%s da torre %s caiu aos %s", o, nomeTime(tr.Time), relogio(e.TMs)))
			}
		}
	}
	ultimo := -1 // última janela com posição
	for i := range js {
		j := &js[i]
		if j.S == nil {
			continue
		}
		s := *j.S
		meio := (j.DeMs + j.AteMs) / 2
		if j.Estado == "" {
			for _, time := range []int{100, 200} {
				tr, ok := d.eixo.torreDePe(time, d.caidas, meio)
				if !ok {
					continue
				}
				// Distância até a torre para o lado de fora (para a outra base).
				fora := s - tr.S
				if time == 200 {
					fora = tr.S - s
				}
				if fora <= Param.AlcanceTorreOnda {
					j.Estado, j.Time = EOndaNaTorre, time
					j.Sinais = append(j.Sinais, fmt.Sprintf("onda a %.0f da torre %s (alcance %.0f)", math.Max(0, fora), nomeTime(time), Param.AlcanceTorreOnda))
					break
				}
			}
		}
		if ultimo >= 0 && js[ultimo].Onda >= j.Onda-2 {
			av := (s - *js[ultimo].S) / float64(j.Onda-js[ultimo].Onda)
			av = math.Round(av)
			j.Avanco = &av
		}
		if j.Estado == "" && j.Avanco != nil {
			av := *j.Avanco
			ant := js[ultimo]
			// Voltando: logo depois da onda na torre (ou já voltando), andando
			// para longe daquela torre.
			if (ant.Estado == EOndaNaTorre || ant.Estado == EOndaVoltando) && ant.Time != 0 {
				longe := av
				if ant.Time == 200 {
					longe = -av
				}
				if longe >= Param.AvancoEmpurrando {
					j.Estado, j.Time = EOndaVoltando, ant.Time
					j.Sinais = append(j.Sinais, fmt.Sprintf("voltando da torre %s: %.0f por onda", nomeTime(ant.Time), longe))
				}
			}
			if j.Estado == "" && math.Abs(av) >= Param.AvancoEmpurrando {
				quem := 100
				if av < 0 {
					quem = 200
				}
				j.Estado, j.Time = EOndaEmpurra, quem
				j.Sinais = append(j.Sinais, fmt.Sprintf("a onda andou %.0f por onda para a torre %s", math.Abs(av), nomeTime(outroTime(quem))))
			}
		}
		if j.Estado == "" && j.Avanco != nil {
			j.Estado = EOndaParada
			j.Sinais = append(j.Sinais, fmt.Sprintf("a onda andou só %.0f por onda", math.Abs(*j.Avanco)))
		}
		ultimo = i
	}
	d.marcaFreezes(js)
}

// marcaFreezes procura as sequências de janelas com a onda parada pela
// definição do freeze: ondas seguidas (OndasFreeze ou mais) com todas as
// posições dentro de uma faixa de FaixaFreeze, sem andar mais que meia faixa
// do começo ao fim e sem bater na torre. É pela faixa, não pelo avanço de uma
// janela para a outra: uma janela com só um lado presente oscila algumas
// centenas e partia o freeze do topo da partida 3304217746 em pedaços. A
// sequência puxada para perto da torre de um time (DeslocamentoFreeze), com
// um jogador rastreado daquele time farmando ali, é o freeze dele; senão é
// onda_parada.
func (d *deducaoRota) marcaFreezes(js []JanelaOnda) {
	cabe := func(j JanelaOnda) bool { return j.S != nil && j.Estado != EOndaNaTorre }
	for i := 0; i < len(js); {
		if !cabe(js[i]) {
			i++
			continue
		}
		lo, hi := *js[i].S, *js[i].S
		k := i
		for k+1 < len(js) && cabe(js[k+1]) && js[k+1].Onda == js[k].Onda+1 {
			s := *js[k+1].S
			if math.Max(hi, s)-math.Min(lo, s) > Param.FaixaFreeze {
				break
			}
			lo, hi = math.Min(lo, s), math.Max(hi, s)
			k++
		}
		run := js[i : k+1]
		deriva := math.Abs(*run[len(run)-1].S - *run[0].S)
		if len(run) < Param.OndasFreeze || deriva > Param.FaixaFreeze/2 {
			i++
			continue
		}
		var ss []float64
		for _, j := range run {
			ss = append(ss, *j.S)
		}
		centro := mediana(ss)
		desloc := centro - d.eixo.Encontro // < 0 = puxada para o azul
		time := 0
		if desloc <= -Param.DeslocamentoFreeze {
			time = 100
		} else if desloc >= Param.DeslocamentoFreeze {
			time = 200
		}
		farm, quem := false, 0
		if time != 0 {
			farm, quem = d.farmando(time, run)
		}
		for x := range run {
			j := &run[x]
			j.Time = time
			j.Sinais = []string{fmt.Sprintf("parada %d ondas numa faixa de %.0f (%.0f a %.0f)", len(run), hi-lo, lo, hi)}
			switch {
			case farm:
				j.Estado = EOndaFreeze
				j.Sinais = append(j.Sinais, fmt.Sprintf("%.0f do ponto de encontro para o lado %s, fora do alcance da torre", math.Abs(desloc), nomeTime(time)),
					fmt.Sprintf("o participante %d farmando ali", quem))
			case time != 0:
				j.Estado = EOndaParada
				j.Sinais = append(j.Sinais, fmt.Sprintf("perto da torre %s, mas nenhum jogador rastreado do time %s farmando ali", nomeTime(time), nomeTime(time)))
			default:
				j.Estado = EOndaParada
				j.Sinais = append(j.Sinais, "no meio da rota (ponto de encontro natural)")
			}
		}
		i = k + 1
	}
	// Janela parada fora de uma sequência: o lado é o da torre mais perto.
	for i := range js {
		if js[i].Estado == EOndaParada && js[i].Time == 0 && js[i].S != nil {
			if desloc := *js[i].S - d.eixo.Encontro; desloc <= -Param.DeslocamentoFreeze {
				js[i].Time = 100
			} else if desloc >= Param.DeslocamentoFreeze {
				js[i].Time = 200
			}
		}
	}
}

// farmando: um rastreado da rota do time (não o suporte) estava presente na
// maior parte das janelas e o CS dele subiu no trecho (linha do tempo, dos
// quadros em volta do trecho) no ritmo de "farmando".
func (d *deducaoRota) farmando(time int, run []JanelaOnda) (bool, int) {
	presentes := 0
	for _, j := range run {
		if j.lados[time] > 0 {
			presentes++
		}
	}
	if presentes*2 < len(run) {
		return false, 0
	}
	for _, pid := range d.da[time] {
		if d.in.Posicoes[pid] == "UTILITY" {
			continue
		}
		if r, ok := csNoTrecho(d.in.Linha, pid, run[0].DeMs, run[len(run)-1].AteMs); ok && r >= Param.CSMinimoPorMin {
			return true, pid
		}
	}
	return false, 0
}

// csNoTrecho: o ritmo de CS (por minuto) do quadro da linha do tempo de antes
// de `de` ao de depois de `ate`.
func csNoTrecho(lt rastreio.LinhaDoTempo, pid int, de, ate int64) (float64, bool) {
	q := lt.Quadros
	a, b := -1, -1
	for k := range q {
		if q[k].TMs <= de {
			a = k
		}
		if q[k].TMs >= ate && b < 0 {
			b = k
		}
	}
	if a < 0 || b <= a {
		return 0, false
	}
	ja, okA := q[a].Jogadores[pid]
	jb, okB := q[b].Jogadores[pid]
	if !okA || !okB {
		return 0, false
	}
	// Minutos inteiros: os quadros são de minuto em minuto, mas o carimbo de
	// tempo de cada um passa alguns ms do minuto (6 CS em "2,0003 min" dava
	// 2,9995/min, abaixo do limiar de 3).
	minutos := math.Max(1, math.Round(float64(q[b].TMs-q[a].TMs)/60000))
	return float64(jb.CS-ja.CS) / minutos, true
}

// trechos junta as janelas seguidas no mesmo estado (e mesmo time).
func (d *deducaoRota) trechos(js []JanelaOnda) []TrechoOnda {
	out := []TrechoOnda{}
	var ss, avs []float64
	altas, n := 0, 0
	fecha := func() {
		if len(out) == 0 || len(ss) == 0 {
			return
		}
		t := &out[len(out)-1]
		t.S = math.Round(mediana(ss))
		p := d.eixo.Ponto(t.S)
		t.X, t.Y = math.Round(p.X), math.Round(p.Y)
		t.Confianca = "media"
		if altas*2 > n {
			t.Confianca = "alta"
		}
		if len(avs) > 0 && (t.Estado == EOndaEmpurra || t.Estado == EOndaVoltando) {
			m := 0.0
			for _, a := range avs {
				m += math.Abs(a)
			}
			m = math.Round(m / float64(len(avs)))
			t.Avanco = &m
			t.Velocidade = "lenta"
			if m >= Param.AvancoRapido {
				t.Velocidade = "rapida"
			}
		}
		ss, avs, altas, n = nil, nil, 0, 0
	}
	for _, j := range js {
		if j.Estado == "" || j.S == nil && j.Estado != EOndaNaTorre {
			fecha()
			// Janela sem estado corta o trecho: o próximo começa novo.
			if len(out) > 0 {
				out = append(out, TrechoOnda{}) // marcador de corte, removido abaixo
			}
			continue
		}
		u := len(out) - 1
		if u >= 0 && out[u].Estado == j.Estado && out[u].Time == j.Time && out[u].AteMs >= j.DeMs-1 {
			out[u].AteMs = j.AteMs
			out[u].Ondas = append(out[u].Ondas, j.Onda)
			out[u].Sinais = juntaSinais(out[u].Sinais, j.Sinais)
		} else {
			fecha()
			out = append(out, TrechoOnda{Estado: j.Estado, DeMs: j.DeMs, AteMs: j.AteMs, Ondas: []int{j.Onda}, Time: j.Time,
				Sinais: append([]string{}, j.Sinais...)})
		}
		if j.S != nil {
			ss = append(ss, *j.S)
		} else if j.sTorre != nil {
			ss = append(ss, *j.sTorre)
		}
		if j.Avanco != nil {
			avs = append(avs, *j.Avanco)
		}
		n++
		if j.Confianca == "alta" {
			altas++
		}
	}
	fecha()
	limpo := out[:0]
	for _, t := range out {
		if t.Estado != "" {
			if t.Confianca == "" {
				t.Confianca = "media"
			}
			limpo = append(limpo, t)
		}
	}
	return limpo
}

// perdidas: para cada onda que chega na fase de rotas, quem é da rota (não o
// suporte: ele não farma) e não passou perto dela, vivo e na rota.
func (d *deducaoRota) perdidas(ondas []Onda, pontos []PontoOnda, js []JanelaOnda) []OndaPerdida {
	out := []OndaPerdida{}
	estadoDa := map[int]JanelaOnda{}
	for _, j := range js {
		estadoDa[j.Onda] = j
	}
	for time, pids := range d.da {
		for _, pid := range pids {
			if d.in.Posicoes[pid] == "UTILITY" {
				continue
			}
			as := d.in.Amostras[pid]
			for _, o := range ondas {
				tc := o.Chegada[d.rota]
				if tc >= d.fimRotas {
					break
				}
				fimJ := tc + Param.JanelaPresencaOndaMs
				// Sem rastreio cobrindo a janela não dá para dizer.
				if len(as) == 0 || as[0].TMs > tc || as[len(as)-1].TMs < fimJ {
					continue
				}
				// Onda batendo na torre inimiga: ir embora é o certo (o recall
				// depois do crash), não é onda perdida.
				if j, ok := estadoDa[o.N]; ok && j.Estado == EOndaNaTorre && j.Time != time {
					continue
				}
				ref, fonte := d.eixo.Encontro, "encontro"
				if p, ok := pontoPerto(pontos, tc, 15000); ok {
					ref, fonte = p.S, "estimada"
				}
				presente := false
				for t := tc; t <= fimJ; t += 1000 {
					p, ok := PosicaoEm(as, t)
					if !ok {
						continue
					}
					if z, _ := Zona(p.X, p.Y); z != d.rota {
						continue
					}
					if s, _ := d.eixo.S(p); math.Abs(s-ref) <= Param.RaioPresencaOnda {
						presente = true
						break
					}
				}
				if presente {
					continue
				}
				meio := tc + Param.JanelaPresencaOndaMs/2
				op := OndaPerdida{ParticipanteID: pid, Onda: o.N, Canhao: o.Canhao, TMs: tc, OndaS: math.Round(ref), PosicaoOnda: fonte}
				a := AmostraEm(as, meio, 1500)
				pOnda := d.eixo.Ponto(ref)
				switch {
				case a != nil && a.Morto:
					op.Motivo = "morto"
					if a.X != nil {
						op.X, op.Y = *a.X, *a.Y
					}
				default:
					p, ok := PosicaoEm(as, meio)
					if !ok {
						op.Motivo = "longe"
						break
					}
					op.X, op.Y = math.Round(p.X), math.Round(p.Y)
					op.Zona, _ = Zona(p.X, p.Y)
					op.Distancia = math.Round(dist(p, pOnda))
					op.Motivo = "longe"
					if TipoZona(op.Zona) == "base" && GrupoRelativo(op.Zona, "", time) == "base_propria" {
						op.Motivo = "na_base"
					}
				}
				out = append(out, op)
			}
		}
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].TMs != out[b].TMs {
			return out[a].TMs < out[b].TMs
		}
		return out[a].ParticipanteID < out[b].ParticipanteID
	})
	return out
}

// pontoPerto: o ponto estimado mais perto de t, a até `ate` ms.
func pontoPerto(ps []PontoOnda, t, ate int64) (PontoOnda, bool) {
	i := sort.Search(len(ps), func(k int) bool { return ps[k].TMs >= t })
	melhor, ok := PontoOnda{}, false
	md := ate + 1
	for _, k := range []int{i - 1, i} {
		if k >= 0 && k < len(ps) {
			if dd := absI(ps[k].TMs - t); dd < md {
				melhor, ok, md = ps[k], true, dd
			}
		}
	}
	return melhor, ok
}

func absI(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
