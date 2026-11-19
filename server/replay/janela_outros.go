//go:build !windows

package replay

import (
	"errors"
	"image"
	"time"
)

// O jogo só existe no Windows; fora dele a janela "de verdade" recusa tudo,
// para o pacote compilar (CI, editor) sem fingir que funciona.

var errSoWindows = errors.New("a janela do jogo só existe no Windows")

type JanelaWindows struct{}

func NovaJanela() *JanelaWindows { return &JanelaWindows{} }

func (*JanelaWindows) Aberto() bool                                 { return false }
func (*JanelaWindows) Foco() error                                  { return errSoWindows }
func (*JanelaWindows) Encerra() error                               { return nil }
func (*JanelaWindows) Aperta(rune) error                            { return errSoWindows }
func (*JanelaWindows) Tamanho() (int, int, error)                   { return 0, 0, errSoWindows }
func (*JanelaWindows) Captura(image.Rectangle) (*image.RGBA, error) { return nil, errSoWindows }
func (*JanelaWindows) UltimaEntradaDoUsuario() time.Time            { return time.Time{} }
func (*JanelaWindows) ModificadorPressionado() bool                 { return false }
func (*JanelaWindows) PrimeiroPlano() string                        { return "" }
