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

// Teclas de seleção: na ordem do playerlist, 1–5 para o time ORDER (azul),
// Q W E R T para o CHAOS (vermelho).
var (
	teclasOrder = []rune{'1', '2', '3', '4', '5'}
	teclasChaos = []rune{'Q', 'W', 'E', 'R', 'T'}
)

// Teclas da visão de time do replay (F1 = névoa do time azul, F2 = do
// vermelho, F3 = todos), autorizadas pelo dono em 2026-11-17 para as passadas
// de visibilidade e conferidas no jogo (o painel do replay mostra "Fog of War:
// Blue | Red | All"). Teclas de função não são caracteres: ficam em runas da
// área de uso privado do Unicode, que nenhum texto digitado produz.
const (
	TeclaF1 rune = 0xF701
	TeclaF2 rune = 0xF702
	TeclaF3 rune = 0xF703
)

// teclasPermitidas é a lista inteira (regra 3 do CLAUDE.md): as 10 de seleção
// e as 3 da visão de time. Nenhuma outra sai do app.
var teclasPermitidas = []rune{'1', '2', '3', '4', '5', 'Q', 'W', 'E', 'R', 'T', TeclaF1, TeclaF2, TeclaF3}

// TeclaPermitida diz se a tecla está na lista.
func TeclaPermitida(t rune) bool {
	for _, k := range teclasPermitidas {
		if k == t {
			return true
		}
	}
	return false
}

// NomeTecla é o nome legível da tecla (para log, erro e o registro do falso).
func NomeTecla(t rune) string {
	switch t {
	case TeclaF1:
		return "F1"
	case TeclaF2:
		return "F2"
	case TeclaF3:
		return "F3"
	}
	return string(t)
}

// TeclaDaVisao é a tecla que põe a névoa do replay na visão do time (100 =
// azul/ORDER → F1, 200 = vermelho/CHAOS → F2) ou em "todos" (0 → F3).
func TeclaDaVisao(time int) (rune, error) {
	switch time {
	case 100:
		return TeclaF1, nil
	case 200:
		return TeclaF2, nil
	case 0:
		return TeclaF3, nil
	}
	return 0, fmt.Errorf("time %d não tem visão no replay (só 100, 200 ou 0 = todos)", time)
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
