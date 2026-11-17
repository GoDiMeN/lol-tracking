package replay

import (
	"context"
	"errors"
	"fmt"
	"image"
	"time"
)

// Jogo é o processo e a janela do jogo ("League of Legends.exe", janela
// "League of Legends (TM) Client"). Nunca o LeagueClient: ele é o cliente onde
// a pessoa está logada e não é nosso para fechar.
type Jogo interface {
	// Aberto diz se o processo "League of Legends" está rodando.
	Aberto() bool
	// Foco traz a janela do jogo para frente e CONFERE que ela é a janela em
	// primeiro plano. Erro = não mande tecla.
	Foco() error
	// Encerra mata só o processo "League of Legends".
	Encerra() error
}

// Teclado manda uma tecla para a janela em primeiro plano. Quem chama
// (Sessao.Tecla) garante antes que a janela é a do replay.
type Teclado interface {
	Aperta(tecla rune) error
}

// Tela captura a área do jogo.
type Tela interface {
	// Tamanho é a área cliente da janela do jogo, em pixels físicos.
	Tamanho() (largura, altura int, err error)
	// Captura copia a área cliente da janela (região vazia = tudo). A imagem
	// devolvida pode ser reaproveitada na próxima captura: quem precisa
	// guardar, copia.
	Captura(regiao image.Rectangle) (*image.RGBA, error)
}

// Relogio é o tempo de verdade no app e um tempo virtual nos testes (o
// replay falso anda com ele, e os testes rodam 40 min de jogo em milissegundos).
type Relogio interface {
	Agora() time.Time
	Dorme(ctx context.Context, d time.Duration) error
}

// RelogioReal é o relógio do sistema.
type RelogioReal struct{}

func (RelogioReal) Agora() time.Time { return time.Now() }

func (RelogioReal) Dorme(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case <-t.C:
		return nil
	}
}

// Teclas de seleção: as únicas que o app manda (regra 3). Na ordem do
// playerlist: 1–5 para o time ORDER (azul), Q W E R T para o CHAOS (vermelho).
var (
	teclasOrder = []rune{'1', '2', '3', '4', '5'}
	teclasChaos = []rune{'Q', 'W', 'E', 'R', 'T'}
)

// TeclaPermitida diz se a tecla está na lista das 10.
func TeclaPermitida(t rune) bool {
	for _, k := range append(append([]rune{}, teclasOrder...), teclasChaos...) {
		if k == t {
			return true
		}
	}
	return false
}

// ErrJogadorNaoEncontrado: o nome não está no playerlist.
var ErrJogadorNaoEncontrado = errors.New("jogador não está no playerlist do replay")

// TeclaDo devolve a tecla que seleciona o jogador de nome dado.
func TeclaDo(js []Jogador, nome string) (rune, error) {
	io, ic := 0, 0
	for _, j := range js {
		var t rune
		switch j.Time {
		case "ORDER":
			if io < len(teclasOrder) {
				t = teclasOrder[io]
			}
			io++
		case "CHAOS":
			if ic < len(teclasChaos) {
				t = teclasChaos[ic]
			}
			ic++
		default:
			continue
		}
		if j.NomeDeSelecao() == nome {
			if t == 0 {
				return 0, fmt.Errorf("jogador %q além do 5º do time", nome)
			}
			return t, nil
		}
	}
	return 0, fmt.Errorf("%w: %q", ErrJogadorNaoEncontrado, nome)
}
