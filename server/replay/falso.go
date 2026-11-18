package replay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"
	"sync"
	"time"
)

// RelogioFalso é o tempo virtual dos testes: só anda quando alguém dorme (ou
// quando o jogo falso cobra a "latência" de cada pedido).
type RelogioFalso struct {
	mu    sync.Mutex
	agora time.Time
}

// NovoRelogioFalso começa numa data fixa (determinístico).
func NovoRelogioFalso() *RelogioFalso {
	return &RelogioFalso{agora: time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)}
}

func (r *RelogioFalso) Agora() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.agora
}

// Avanca anda o relógio sem dormir.
func (r *RelogioFalso) Avanca(d time.Duration) {
	r.mu.Lock()
	r.agora = r.agora.Add(d)
	r.mu.Unlock()
}

func (r *RelogioFalso) Dorme(ctx context.Context, d time.Duration) error {
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	if d > 0 {
		r.Avanca(d)
	}
	return nil
}

// JogadorFalso é um dos 10 do replay falso.
type JogadorFalso struct {
	Nome    string
	Campeao string
	Time    string // ORDER ou CHAOS
}

// MorteFalsa: o jogador Indice (0–9, ordem do playerlist) morre em De e
// renasce em Ate (segundos de jogo).
type MorteFalsa struct {
	Indice  int
	De, Ate float64
	Matador int
}

// PerdaTravaFalsa: entre De e Ate (segundos de jogo) a câmera não fica no
// jogador Indice (0–9; -1 = qualquer um que esteja sendo seguido): se estava
// presa nele, solta (seleção vazia, câmera parada onde estava), e a tecla dupla
// não prende. É o que se viu no passe da Leona da 3304217746 (14,8 s sem trava
// aos 16:16). Vezes limita em quantas PASSAGENS do replay pelo trecho a perda
// acontece (0 = sempre): uma passagem começa quando o tempo entra no trecho, e
// todo seek conta como saída. Assim o teste simula "perdeu na primeira vez e a
// repescagem (que volta o replay) consegue". A contagem é por jogador e vale
// para o processo inteiro (não zera ao reabrir: a repescagem depois reabre).
type PerdaTravaFalsa struct {
	Indice  int
	De, Ate float64
	Vezes   int
}

// SentinelaFalsa: uma sentinela do time Time (100/200) em (X, Y), de De a Ate
// (segundos de jogo). Dá visão num raio de RaioSentinelaFalso e aparece no
// minimapa do falso para o próprio time (e em "todos").
type SentinelaFalsa struct {
	Time    int
	X, Y    float64
	De, Ate float64
}

// Raios de visão do jogo falso (unidades do mapa): um campeão vê os inimigos a
// menos de RaioVisaoFalso; uma sentinela, a menos de RaioSentinelaFalso. É
// uma regra inventada e determinística (sem mato nem parede), só para os
// testes terem uma verdade para comparar.
const (
	RaioVisaoFalso     = 1200.0
	RaioSentinelaFalso = 900.0
)

// Coeficientes da câmera medidos a 2× (descobertas 3.2), para tela 2560×1440.
const (
	CoefX0  = 0.2
	CoefXPx = 1.374
	CoefY0  = 1249.6
	CoefYPx = 1.768
	// Largura e altura da tela em que os coeficientes por pixel foram medidos.
	LarguraRef = 2560
	AlturaRef  = 1440
)

// Falso é o jogo em replay dos testes e do LOL_TESTE=1. Implementa API, Jogo,
// Teclado e Tela, e reproduz as manias medidas no jogo de verdade:
//   - o tempo anda com a velocidade e para no fim; seek derruba a seleção;
//   - a tecla dupla prende a câmera; a simples só seleciona;
//   - com o jogo pausado, o `T` muda a seleção e a câmera não sai do lugar;
//   - a câmera segue o campeão com atraso (AtrasoCamera);
//   - morto, a seleção cai e a câmera vai para outro lugar; ao renascer a
//     seleção volta sozinha (se VoltaAoRenascer);
//   - POST render com selectionName não prende nada (a "Directed Camera");
//   - F1/F2/F3 trocam a névoa (azul, vermelho, todos), e a captura de uma
//     região (Captura com retângulo) desenha o painel "Fog of War" e o
//     minimapa daquela visão: o retrato (anel na cor do time) dos vivos que o
//     time enxerga (VisivelPara) e as sentinelas do time, na geometria medida
//     (minimapa.go). A captura da tela inteira continua a imagem pequena.
type Falso struct {
	mu  sync.Mutex
	rel Relogio

	Elenco   []JogadorFalso
	DuracaoS float64
	Mortes   []MorteFalsa
	// VoltaAoRenascer: a seleção volta sozinha quando o jogador renasce.
	VoltaAoRenascer bool
	// AtrasoCamera em segundos de jogo.
	AtrasoCamera float64
	// FalhasTrava: as próximas N teclas duplas não prendem a câmera.
	FalhasTrava int
	// PerdasTrava: trechos em que a câmera não fica no jogador (ver PerdaTravaFalsa).
	PerdasTrava []PerdaTravaFalsa
	// NuncaAbre: a Replay API nunca responde (o watch "abre" algo que não é replay).
	NuncaAbre bool
	// SemFoco: a janela nunca vai para o primeiro plano.
	SemFoco bool
	// NaoSai: o /Exit não fecha o processo (força o Encerra).
	NaoSai bool
	// Largura e Altura da tela do jogo.
	Largura, Altura int
	// Latencia que cada pedido à API "custa" no relógio.
	Latencia time.Duration
	// DemoraAbrir: do watch até a API responder.
	DemoraAbrir time.Duration
	// Sentinelas do minimapa falso.
	Sentinelas []SentinelaFalsa
	// IgnoraVisao: F1/F2/F3 não mudam a névoa (o painel fica em "todos").
	IgnoraVisao bool
	// SemMinimapa: a captura de região sai sem o minimapa (o HUD escondido).
	SemMinimapa bool

	// Estado.
	processo     bool
	abreEm       time.Time
	fechaEm      time.Time
	tempoBase    float64
	refReal      time.Time
	velocidade   float64
	pausado      bool
	selecao      string
	seguindo     bool
	camFixa      [2]float64
	perdidoMorte string
	inicioEvs    float64
	ultTecla     rune
	ultTeclaEm   time.Time
	// passagens[k][jogador]: quantas vezes o replay entrou no trecho da perda k
	// seguindo esse jogador; dentro[k][jogador]: está numa passagem agora.
	passagens map[int]map[int]int
	dentro    map[int]map[int]bool
	// visao: a névoa do replay (0 = todos, 100 = azul, 200 = vermelho).
	visao    int
	imgVisao *image.RGBA

	// Registro para os testes.
	Teclas     []string
	Saidas     int
	Encerrados int
	Aberturas  int
}

// NovoFalso cria o jogo falso com 10 jogadores "Jogador 1".."Jogador 10".
func NovoFalso(rel Relogio) *Falso {
	f := &Falso{rel: rel, DuracaoS: 600, VoltaAoRenascer: true, AtrasoCamera: 0.15,
		Largura: LarguraRef, Altura: AlturaRef, Latencia: 3 * time.Millisecond, DemoraAbrir: 5 * time.Second}
	for k := 1; k <= 10; k++ {
		t := "ORDER"
		if k > 5 {
			t = "CHAOS"
		}
		f.Elenco = append(f.Elenco, JogadorFalso{Nome: fmt.Sprintf("Jogador %d", k), Campeao: "Campeão", Time: t})
	}
	return f
}

// ConfiguraPartida troca os 10 jogadores e a duração (o modo teste chama a
// cada watch, com os participantes da partida pedida).
func (f *Falso) ConfiguraPartida(elenco []JogadorFalso, duracaoS float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(elenco) > 0 {
		f.Elenco = elenco
	}
	if duracaoS > 0 {
		f.DuracaoS = duracaoS
	}
}

// AbreReplay é o que o watch do LCU falso chama: o jogo "abre" e a API passa a
// responder depois de DemoraAbrir.
func (f *Falso) AbreReplay() {
	f.mu.Lock()
	defer f.mu.Unlock()
	agora := f.rel.Agora()
	f.processo, f.abreEm, f.fechaEm = true, agora.Add(f.DemoraAbrir), time.Time{}
	f.tempoBase, f.refReal, f.velocidade, f.pausado = 0, agora, 1, false
	f.selecao, f.seguindo, f.perdidoMorte, f.inicioEvs = "", false, "", 0
	f.camFixa = [2]float64{7400, 7400}
	f.visao = 0
	f.Aberturas++
}

// Visao é a névoa atual do replay falso (0 todos, 100 azul, 200 vermelho).
func (f *Falso) Visao() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.visao
}

func (f *Falso) timeDe(i int) int {
	if f.Elenco[i].Time == "CHAOS" {
		return 200
	}
	return 100
}

// VisivelPara diz se o time (100/200; 0 = todos) enxerga o jogador i (0–9) no
// tempo t: vivo e do próprio time, ou a menos de RaioVisaoFalso de um vivo do
// time, ou a menos de RaioSentinelaFalso de uma sentinela ativa do time. É a
// verdade que os testes comparam com a leitura do minimapa.
func (f *Falso) VisivelPara(time, i int, t float64) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.visivelPara(time, i, t)
}

func (f *Falso) visivelPara(time, i int, t float64) bool {
	if f.morte(i, t) != nil {
		return false
	}
	if time == 0 || f.timeDe(i) == time {
		return true
	}
	x, y := f.Posicao(i, t)
	for j := range f.Elenco {
		if f.timeDe(j) != time || f.morte(j, t) != nil {
			continue
		}
		if jx, jy := f.Posicao(j, t); math.Hypot(jx-x, jy-y) < RaioVisaoFalso {
			return true
		}
	}
	for _, s := range f.Sentinelas {
		if s.Time == time && t >= s.De && t < s.Ate && math.Hypot(s.X-x, s.Y-y) < RaioSentinelaFalso {
			return true
		}
	}
	return false
}

// perdaAtiva diz se, no tempo t, a câmera não pode ficar no jogador i: há uma
// perda cobrindo t e esta passagem ainda está dentro do limite de Vezes. Conta a
// passagem na primeira vez que vê t dentro do trecho (f.mu travado).
func (f *Falso) perdaAtiva(i int, t float64) bool {
	ativa := false
	for k, p := range f.PerdasTrava {
		if p.Indice >= 0 && p.Indice != i {
			continue
		}
		if f.passagens == nil {
			f.passagens, f.dentro = map[int]map[int]int{}, map[int]map[int]bool{}
		}
		if f.passagens[k] == nil {
			f.passagens[k], f.dentro[k] = map[int]int{}, map[int]bool{}
		}
		if t < p.De || t >= p.Ate {
			f.dentro[k][i] = false
			continue
		}
		if !f.dentro[k][i] {
			f.dentro[k][i] = true
			f.passagens[k][i]++
		}
		if p.Vezes == 0 || f.passagens[k][i] <= p.Vezes {
			ativa = true
		}
	}
	return ativa
}

// saiDasPerdas: o seek tira o replay de qualquer passagem (a próxima entrada
// no trecho conta como outra).
func (f *Falso) saiDasPerdas() {
	for k := range f.dentro {
		for i := range f.dentro[k] {
			f.dentro[k][i] = false
		}
	}
}

func (f *Falso) cobra() time.Time {
	if r, ok := f.rel.(*RelogioFalso); ok && f.Latencia > 0 {
		r.Avanca(f.Latencia)
	}
	return f.rel.Agora()
}

func (f *Falso) vivoTravado(agora time.Time) bool {
	return f.processo && (f.fechaEm.IsZero() || agora.Before(f.fechaEm))
}

func (f *Falso) respondendo(agora time.Time) error {
	if f.NuncaAbre || !f.vivoTravado(agora) || agora.Before(f.abreEm) {
		return ErrJogoFechado
	}
	return nil
}

func (f *Falso) tempo(agora time.Time) float64 {
	t := f.tempoBase
	if !f.pausado {
		t += agora.Sub(f.refReal).Seconds() * f.velocidade
	}
	return math.Min(t, f.DuracaoS)
}

// Posicao é onde o jogador i está no tempo t (determinístico: círculos a
// ~350 unidades/s). Morto: parado no local da morte.
func (f *Falso) Posicao(i int, t float64) (float64, float64) {
	if m := f.morte(i, t); m != nil {
		t = m.De
	}
	cx, cy := 2500+float64(i)*1100, 3000+float64((i*3)%10)*900
	r := 1500.0
	w := 350 / r
	return cx + r*math.Cos(w*t+float64(i)), cy + r*math.Sin(w*t+float64(i))
}

func (f *Falso) morte(i int, t float64) *MorteFalsa {
	for k := range f.Mortes {
		m := &f.Mortes[k]
		if m.Indice == i && t >= m.De && t < m.Ate {
			return m
		}
	}
	return nil
}

func (f *Falso) indice(nome string) int {
	for i, j := range f.Elenco {
		if j.Nome == nome {
			return i
		}
	}
	return -1
}

func (f *Falso) coefs() (a1, b1 float64) {
	return CoefXPx * LarguraRef / float64(f.Largura), CoefYPx * AlturaRef / float64(f.Altura)
}

// atualiza aplica as regras de morte/renascimento à seleção.
func (f *Falso) atualiza(t float64) {
	if f.seguindo && f.selecao != "" && len(f.PerdasTrava) > 0 {
		if i := f.indice(f.selecao); i >= 0 && f.perdaAtiva(i, t) {
			// Perdeu a trava: a câmera fica onde estava e a seleção cai.
			lx, ly := f.alvoCamera(t)
			f.camFixa = [2]float64{lx, ly}
			f.selecao, f.seguindo = "", false
		}
	}
	if f.seguindo && f.selecao != "" {
		if i := f.indice(f.selecao); i >= 0 && f.morte(i, t) != nil {
			f.perdidoMorte = f.selecao
			f.selecao, f.seguindo = "", false
			f.camFixa = [2]float64{7400, 7400}
		}
	}
	if f.perdidoMorte != "" && f.VoltaAoRenascer {
		if i := f.indice(f.perdidoMorte); i >= 0 && f.morte(i, t) == nil {
			f.selecao, f.seguindo, f.perdidoMorte = f.perdidoMorte, true, ""
		}
	}
}

// alvoCamera: o ponto do mapa no centro da tela.
func (f *Falso) alvoCamera(t float64) (float64, float64) {
	if f.seguindo {
		if i := f.indice(f.selecao); i >= 0 {
			// Ao renascer a câmera pula para a fonte (descobertas 4.1: a primeira
			// amostra depois do respawn já é de câmera, na fonte): sem atraso.
			ta := math.Max(0, t-f.AtrasoCamera)
			if f.morte(i, ta) != nil && f.morte(i, t) == nil {
				ta = t
			}
			return f.Posicao(i, ta)
		}
	}
	return f.camFixa[0], f.camFixa[1]
}

func (f *Falso) Playback(ctx context.Context) (Playback, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	agora := f.cobra()
	if err := f.respondendo(agora); err != nil {
		return Playback{}, err
	}
	return Playback{Duracao: f.DuracaoS, Pausado: f.pausado, Velocidade: f.velocidade, Tempo: f.tempo(agora)}, nil
}

func (f *Falso) MudaPlayback(ctx context.Context, m MudaPlayback) (Playback, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	agora := f.cobra()
	if err := f.respondendo(agora); err != nil {
		return Playback{}, err
	}
	t := f.tempo(agora)
	if m.Tempo != nil {
		t = math.Max(0, math.Min(*m.Tempo, f.DuracaoS))
		// Seek derruba a trava (descobertas 3).
		if f.seguindo {
			lx, ly := f.alvoCamera(f.tempo(agora))
			f.camFixa = [2]float64{lx, ly}
		}
		f.selecao, f.seguindo, f.perdidoMorte = "", false, ""
		f.inicioEvs = t
		f.saiDasPerdas()
	}
	f.tempoBase, f.refReal = t, agora
	if m.Velocidade != nil && *m.Velocidade > 0 {
		f.velocidade = *m.Velocidade
	}
	if m.Pausado != nil {
		f.pausado = *m.Pausado
	}
	return Playback{Duracao: f.DuracaoS, Pausado: f.pausado, Velocidade: f.velocidade, Tempo: f.tempo(agora)}, nil
}

func (f *Falso) Render(ctx context.Context) (Render, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	agora := f.cobra()
	if err := f.respondendo(agora); err != nil {
		return Render{}, err
	}
	t := f.tempo(agora)
	f.atualiza(t)
	lx, ly := f.alvoCamera(t)
	return Render{CameraPosicao: Vetor3{X: lx - CoefX0, Y: 1911.85, Z: ly - CoefY0}, CameraRotacao: Vetor3{Y: 56},
		CampoDeVisao: 40, CameraModo: "top", CameraPresa: f.seguindo, Selecao: f.selecao}, nil
}

// MudaRender aceita e não prende nada: é a armadilha da Directed Camera.
func (f *Falso) MudaRender(ctx context.Context, campos map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.respondendo(f.cobra())
}

func (f *Falso) Jogadores(ctx context.Context) ([]Jogador, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	agora := f.cobra()
	if err := f.respondendo(agora); err != nil {
		return nil, err
	}
	t := f.tempo(agora)
	f.atualiza(t)
	lx, ly := f.alvoCamera(t)
	a1, b1 := f.coefs()
	w, h := float64(f.Largura), float64(f.Altura)
	var js []Jogador
	for i, jf := range f.Elenco {
		x, y := f.Posicao(i, t)
		sx, sy := w/2+(x-lx)/a1, h/2-(y-ly)/b1
		centro := Ponto{X: sx, Y: sy, Valido: true}
		texto := fmt.Sprintf("%.3f,%.3f", sx, sy)
		if sx < 0 || sy < 0 || sx > w || sy > h {
			centro, texto = Ponto{}, "340000000000000000000000000000000000000.000,340000000000000000000000000000000000000.000"
		}
		j := Jogador{Nome: jf.Nome, Tag: "br1", Invocador: jf.Nome, Campeao: jf.Campeao, Time: jf.Time,
			Nivel: min(18, 1+int(t/90)), Placar: Placar{CS: int(t/7) / 10 * 10, Ward: math.Floor(t/60*10) / 10},
			CentroTexto: texto, Centro: centro}
		j.Itens = []Item{{ID: 1055, Slot: 0, Quantidade: 1}}
		if t > 150 {
			j.Itens = append(j.Itens, Item{ID: 3006, Slot: 1, Quantidade: 1})
		}
		for _, m := range f.Mortes {
			if m.De <= t {
				if m.Indice == i {
					j.Placar.Mortes++
				}
				if m.Matador == i {
					j.Placar.Abates++
				}
			}
		}
		if m := f.morte(i, t); m != nil {
			j.Morto, j.Renasce = true, m.Ate-t
		}
		js = append(js, j)
	}
	return js, nil
}

func (f *Falso) Eventos(ctx context.Context) ([]EventoJogo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	agora := f.cobra()
	if err := f.respondendo(agora); err != nil {
		return nil, err
	}
	t := f.tempo(agora)
	var crus []json.RawMessage
	add := func(v map[string]any) {
		b, _ := json.Marshal(v)
		crus = append(crus, b)
	}
	if f.inicioEvs == 0 {
		add(map[string]any{"EventID": 0, "EventName": "GameStart", "EventTime": 0.05})
	}
	for k, m := range f.Mortes {
		if m.De >= f.inicioEvs && m.De <= t {
			add(map[string]any{"EventID": k + 1, "EventName": "ChampionKill", "EventTime": m.De + 0.4,
				"KillerName": f.Elenco[m.Matador].Nome, "VictimName": f.Elenco[m.Indice].Nome, "Assisters": []string{}})
		}
	}
	return LeEventos(crus), nil
}

func (f *Falso) Estatisticas(ctx context.Context) (Estatisticas, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	agora := f.cobra()
	if err := f.respondendo(agora); err != nil {
		return Estatisticas{}, err
	}
	return Estatisticas{TempoJogo: f.tempo(agora), Mapa: 11, Modo: "CLASSIC"}, nil
}

// Sai: o processo fecha 3 s depois (ou nunca, com NaoSai).
func (f *Falso) Sai(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	agora := f.cobra()
	if err := f.respondendo(agora); err != nil {
		return err
	}
	f.Saidas++
	if !f.NaoSai {
		f.fechaEm = agora.Add(3 * time.Second)
	}
	return nil
}

// --- Jogo, Teclado, Tela ---

func (f *Falso) Aberto() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.vivoTravado(f.rel.Agora())
}

func (f *Falso) Foco() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.SemFoco || !f.vivoTravado(f.rel.Agora()) {
		return errors.New("janela do jogo falso fora do primeiro plano")
	}
	return nil
}

func (f *Falso) Encerra() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.processo {
		f.Encerrados++
	}
	f.processo = false
	return nil
}

func (f *Falso) Aperta(tecla rune) error {
	if !TeclaPermitida(tecla) {
		return fmt.Errorf("tecla %q proibida", tecla)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	agora := f.rel.Agora()
	f.Teclas = append(f.Teclas, NomeTecla(tecla))
	if r, ok := f.rel.(*RelogioFalso); ok {
		r.Avanca(60 * time.Millisecond)
	}
	if v, ok := map[rune]int{TeclaF1: 100, TeclaF2: 200, TeclaF3: 0}[tecla]; ok {
		if !f.IgnoraVisao {
			f.visao = v
		}
		return nil
	}
	idx := strings.IndexRune("12345", tecla)
	if idx < 0 {
		if k := strings.IndexRune("QWERT", tecla); k >= 0 {
			idx = 5 + k
		}
	}
	if idx < 0 || idx >= len(f.Elenco) {
		return nil
	}
	nome := f.Elenco[idx].Nome
	duplo := f.ultTecla == tecla && agora.Sub(f.ultTeclaEm) <= 500*time.Millisecond
	f.ultTecla, f.ultTeclaEm = tecla, agora
	if f.seguindo {
		lx, ly := f.alvoCamera(f.tempo(agora))
		f.camFixa = [2]float64{lx, ly}
	}
	f.selecao, f.seguindo, f.perdidoMorte = nome, false, ""
	if !duplo {
		return nil
	}
	switch {
	case f.pausado && tecla == 'T':
		// Medido: pausado, o T muda a seleção e a câmera não se move.
	case f.FalhasTrava > 0:
		f.FalhasTrava--
	case len(f.PerdasTrava) > 0 && f.perdaAtiva(idx, f.tempo(agora)):
		// Dentro de um trecho de perda a tecla seleciona e não prende.
	default:
		f.seguindo = true
	}
	return nil
}

func (f *Falso) Tamanho() (int, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Largura, f.Altura, nil
}

// Captura da tela inteira (região vazia) devolve uma imagem pequena (o leitor
// da tarefa 3 é testado com capturas de verdade guardadas, não com esta). Uma
// região devolve aquele recorte com o painel da névoa e o minimapa desenhados
// (o passe de visão captura replay.RegiaoVisao), reaproveitando a imagem.
func (f *Falso) Captura(regiao image.Rectangle) (*image.RGBA, error) {
	if !regiao.Empty() {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.imgVisao == nil || f.imgVisao.Rect.Dx() != regiao.Dx() || f.imgVisao.Rect.Dy() != regiao.Dy() {
			f.imgVisao = image.NewRGBA(image.Rect(0, 0, regiao.Dx(), regiao.Dy()))
		}
		f.desenhaVisao(f.imgVisao, regiao, f.tempo(f.rel.Agora()))
		return f.imgVisao, nil
	}
	img := image.NewRGBA(image.Rect(0, 0, 32, 18))
	f.mu.Lock()
	c := uint8(int(f.tempo(f.rel.Agora())) % 256)
	f.mu.Unlock()
	for i := 0; i < 32*18; i++ {
		img.Set(i%32, i/32, color.RGBA{c, 0, 0, 255})
	}
	return img, nil
}

// LinhaDoTempo gera a linha do tempo do LCU coerente com o jogo falso:
// quadros de 60 em 60 s (carimbo com +30 ms, como no real) com a posição exata,
// e CHAMPION_KILL de cada morte. participantId = índice+1.
func (f *Falso) LinhaDoTempo() []byte {
	var quadros []any
	for m := 0; float64(m*60) <= f.DuracaoS; m++ {
		ts := m*60000 + 30
		t := float64(ts) / 1000
		pf := map[string]any{}
		for i := range f.Elenco {
			x, y := f.Posicao(i, t)
			pf[fmt.Sprint(i+1)] = map[string]any{"participantId": i + 1, "level": min(18, 1+int(t/90)),
				"minionsKilled": int(t / 7), "jungleMinionsKilled": 0, "currentGold": 500, "totalGold": 500 + m*350, "xp": m * 500,
				"position": map[string]any{"x": math.Round(x), "y": math.Round(y)}}
		}
		evs := []any{}
		for _, mo := range f.Mortes {
			if mo.De*1000 >= float64((m-1)*60000) && mo.De*1000 < float64(m*60000) {
				x, y := f.Posicao(mo.Indice, mo.De)
				evs = append(evs, map[string]any{"type": "CHAMPION_KILL", "timestamp": int(mo.De * 1000),
					"killerId": mo.Matador + 1, "victimId": mo.Indice + 1, "assistingParticipantIds": []int{},
					"position": map[string]any{"x": math.Round(x), "y": math.Round(y)}})
			}
		}
		quadros = append(quadros, map[string]any{"timestamp": ts, "participantFrames": pf, "events": evs})
	}
	b, _ := json.Marshal(map[string]any{"frames": quadros})
	return b
}
