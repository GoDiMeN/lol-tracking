package replay

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Presenca são os sinais do SISTEMA de que a pessoa está usando o computador.
// Nada de ler o que ela digita: só o instante da última entrada de teclado ou
// mouse (GetLastInputInfo, que o Windows guarda para o protetor de tela), se
// uma tecla modificadora está apertada agora e de que programa é a janela da
// frente. Motivo (2026-11-18, partida 3305486988): o dono usava o computador
// durante o rastreio e 4 passes saíram com a câmera fora do jogador em ~70% do
// tempo, com 844–933 teclas de trava cada; o motor não tinha como saber que
// havia alguém usando a máquina e seguia trazendo o jogo para a frente.
type Presenca interface {
	// UltimaEntradaDoUsuario: o instante da última entrada de teclado ou
	// mouse que NÃO foi do app (as teclas do motor e o cursor posto no meio
	// pelo Foco são descontados). Zero = nenhuma vista.
	UltimaEntradaDoUsuario() time.Time
	// ModificadorPressionado: Shift, Ctrl, Alt ou Windows apertado agora. A
	// tecla do motor sairia combinada com ele (Ctrl+4 não é a tecla 4), e é
	// sinal certo de que a pessoa está no teclado.
	ModificadorPressionado() bool
	// PrimeiroPlano: o executável da janela da frente ("" = não deu para
	// saber). Só para o diagnóstico do passe.
	PrimeiroPlano() string
}

// ExeDoJogo é o executável do jogo (o mesmo NomeProcessoJogo da janela do
// Windows; aqui para valer em toda plataforma).
const ExeDoJogo = "League of Legends.exe"

// LimiarUsoUsuario (N): entrada da pessoa há menos que isso = o computador
// está em uso: o app não manda tecla nem traz o jogo para a frente. Variável
// para o teste.
var LimiarUsoUsuario = 3 * time.Second

// ErrUsuarioUsando: a tecla não saiu porque a pessoa está usando o computador.
var ErrUsuarioUsando = errors.New("a pessoa está usando o computador: a tecla não foi enviada e o jogo não foi trazido para a frente")

// EmUso diz se a pessoa está usando o computador agora, e por quê. Sem
// Presenca (nil), nunca.
func EmUso(p Presenca, agora time.Time) (bool, string) {
	if p == nil {
		return false, ""
	}
	if p.ModificadorPressionado() {
		return true, "Shift, Ctrl, Alt ou Windows apertado"
	}
	if u := p.UltimaEntradaDoUsuario(); !u.IsZero() && agora.Sub(u) < LimiarUsoUsuario {
		return true, fmt.Sprintf("teclado ou mouse usado há %.1f s", agora.Sub(u).Seconds())
	}
	return false, ""
}

// OciosoDesde: há quanto tempo a pessoa não usa o computador (sem Presenca ou
// sem entrada vista, um tempo enorme).
func OciosoDesde(p Presenca, agora time.Time) time.Duration {
	if p == nil {
		return time.Duration(1 << 62)
	}
	u := p.UltimaEntradaDoUsuario()
	if u.IsZero() {
		return time.Duration(1 << 62)
	}
	return agora.Sub(u)
}

// DiagTeclas é o registro das teclas da sessão, para dizer depois por que a
// trava não pegou: quantas saíram, quantas vezes o jogo NÃO estava na frente
// (o Foco teve de trazê-lo), quantas foram recusadas porque a pessoa usava o
// computador, e de que programa era a janela da frente antes de cada uma.
type DiagTeclas struct {
	Enviadas      int            `json:"teclas_enviadas"`
	FocoTrocado   int            `json:"foco_trocado"`
	RecusadasUso  int            `json:"recusadas_por_uso"`
	FrenteAntes   map[string]int `json:"frente_antes"`
	ModificadorOn int            `json:"recusadas_modificador"`
}

// Menos devolve d − antes (o que aconteceu entre duas fotos).
func (d DiagTeclas) Menos(antes DiagTeclas) DiagTeclas {
	r := DiagTeclas{Enviadas: d.Enviadas - antes.Enviadas, FocoTrocado: d.FocoTrocado - antes.FocoTrocado,
		RecusadasUso: d.RecusadasUso - antes.RecusadasUso, ModificadorOn: d.ModificadorOn - antes.ModificadorOn,
		FrenteAntes: map[string]int{}}
	for k, v := range d.FrenteAntes {
		if n := v - antes.FrenteAntes[k]; n > 0 {
			r.FrenteAntes[k] = n
		}
	}
	return r
}

// Soma junta dois registros (o passe retomado soma o de antes).
func (d DiagTeclas) Soma(o DiagTeclas) DiagTeclas {
	r := DiagTeclas{Enviadas: d.Enviadas + o.Enviadas, FocoTrocado: d.FocoTrocado + o.FocoTrocado,
		RecusadasUso: d.RecusadasUso + o.RecusadasUso, ModificadorOn: d.ModificadorOn + o.ModificadorOn, FrenteAntes: map[string]int{}}
	for k, v := range d.FrenteAntes {
		r.FrenteAntes[k] += v
	}
	for k, v := range o.FrenteAntes {
		r.FrenteAntes[k] += v
	}
	return r
}

// MaisFrequentes: os programas da frente em ordem de vezes (para o log).
func (d DiagTeclas) MaisFrequentes() []string {
	var ks []string
	for k := range d.FrenteAntes {
		ks = append(ks, k)
	}
	sort.Slice(ks, func(a, b int) bool { return d.FrenteAntes[ks[a]] > d.FrenteAntes[ks[b]] })
	return ks
}

type diagSessao struct {
	mu sync.Mutex
	d  DiagTeclas
}

func (g *diagSessao) anota(f func(d *DiagTeclas)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.d.FrenteAntes == nil {
		g.d.FrenteAntes = map[string]int{}
	}
	f(&g.d)
}

func (g *diagSessao) foto() DiagTeclas {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.d.Soma(DiagTeclas{})
}
