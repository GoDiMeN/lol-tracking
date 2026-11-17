//go:build windows

package replay

import (
	"errors"
	"fmt"
	"image"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// A janela de verdade, por Win32 puro (syscall, sem CGO).

// NomeProcessoJogo é o executável do jogo. O cliente (LeagueClient*.exe) tem
// outro nome e nunca casa com esta comparação exata.
const NomeProcessoJogo = "League of Legends.exe"

// TituloJanelaJogo é o título da janela do jogo.
const TituloJanelaJogo = "League of Legends (TM) Client"

var (
	user32 = windows.NewLazySystemDLL("user32.dll")
	gdi32  = windows.NewLazySystemDLL("gdi32.dll")

	pSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	pSetProcessDPIAware            = user32.NewProc("SetProcessDPIAware")
	pFindWindowW                   = user32.NewProc("FindWindowW")
	pGetWindowThreadProcessId      = user32.NewProc("GetWindowThreadProcessId")
	pGetForegroundWindow           = user32.NewProc("GetForegroundWindow")
	pSetForegroundWindow           = user32.NewProc("SetForegroundWindow")
	pBringWindowToTop              = user32.NewProc("BringWindowToTop")
	pShowWindow                    = user32.NewProc("ShowWindow")
	pIsIconic                      = user32.NewProc("IsIconic")
	pAttachThreadInput             = user32.NewProc("AttachThreadInput")
	pSendInput                     = user32.NewProc("SendInput")
	pGetClientRect                 = user32.NewProc("GetClientRect")
	pClientToScreen                = user32.NewProc("ClientToScreen")
	pGetDC                         = user32.NewProc("GetDC")
	pReleaseDC                     = user32.NewProc("ReleaseDC")

	pCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	pCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	pSelectObject           = gdi32.NewProc("SelectObject")
	pBitBlt                 = gdi32.NewProc("BitBlt")
	pGetDIBits              = gdi32.NewProc("GetDIBits")
	pDeleteObject           = gdi32.NewProc("DeleteObject")
	pDeleteDC               = gdi32.NewProc("DeleteDC")
)

var dpiUmaVez sync.Once

// ligaDPI faz o processo enxergar pixels físicos. Sem isso, numa tela com
// escala de 150% o Windows devolve tamanhos e capturas "virtuais" (2560×1440
// numa tela 3840×2160) e a captura sai borrada.
func ligaDPI() {
	dpiUmaVez.Do(func() {
		// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = -4
		if pSetProcessDpiAwarenessContext.Find() == nil {
			menos4 := ^uintptr(3)
			if r, _, _ := pSetProcessDpiAwarenessContext.Call(menos4); r != 0 {
				return
			}
		}
		if pSetProcessDPIAware.Find() == nil {
			pSetProcessDPIAware.Call()
		}
	})
}

// JanelaWindows implementa Jogo, Teclado e Tela.
type JanelaWindows struct {
	mu  sync.Mutex
	buf []byte
	img *image.RGBA
}

// NovaJanela cria a implementação de verdade (e liga a consciência de DPI).
func NovaJanela() *JanelaWindows {
	ligaDPI()
	return &JanelaWindows{}
}

func pidsDoJogo() ([]uint32, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	var pids []uint32
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if strings.EqualFold(windows.UTF16ToString(e.ExeFile[:]), NomeProcessoJogo) {
			pids = append(pids, e.ProcessID)
		}
	}
	return pids, nil
}

func (j *JanelaWindows) Aberto() bool {
	pids, err := pidsDoJogo()
	return err == nil && len(pids) > 0
}

func exeDoPid(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, 1024)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return ""
	}
	return filepath.Base(windows.UTF16ToString(buf[:n]))
}

// janela acha a janela do jogo e confere que ela pertence ao processo
// "League of Legends.exe" (um título igual em outro programa não serve).
func janela() (uintptr, error) {
	titulo, _ := windows.UTF16PtrFromString(TituloJanelaJogo)
	h, _, _ := pFindWindowW.Call(0, uintptr(unsafe.Pointer(titulo)))
	if h == 0 {
		return 0, errors.New("a janela do jogo não está aberta")
	}
	var pid uint32
	pGetWindowThreadProcessId.Call(h, uintptr(unsafe.Pointer(&pid)))
	if !strings.EqualFold(exeDoPid(pid), NomeProcessoJogo) {
		return 0, fmt.Errorf("a janela %q não é do processo %s", TituloJanelaJogo, NomeProcessoJogo)
	}
	return h, nil
}

func primeiroPlano() uintptr {
	h, _, _ := pGetForegroundWindow.Call()
	return h
}

func (j *JanelaWindows) Foco() error {
	h, err := janela()
	if err != nil {
		return err
	}
	if primeiroPlano() == h {
		return nil
	}
	// O Windows só deixa trocar o primeiro plano de quem já está nele; ligar a
	// fila de entrada desta thread à da janela da frente contorna isso sem
	// mandar tecla nenhuma.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if ic, _, _ := pIsIconic.Call(h); ic != 0 {
		pShowWindow.Call(h, 9) // SW_RESTORE
	}
	frente := primeiroPlano()
	atual := windows.GetCurrentThreadId()
	var outra uintptr
	if frente != 0 {
		outra, _, _ = pGetWindowThreadProcessId.Call(frente, 0)
	}
	if outra != 0 && uint32(outra) != atual {
		pAttachThreadInput.Call(uintptr(atual), outra, 1)
		defer pAttachThreadInput.Call(uintptr(atual), outra, 0)
	}
	pBringWindowToTop.Call(h)
	pSetForegroundWindow.Call(h)
	for i := 0; i < 20; i++ {
		if primeiroPlano() == h {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return errors.New("não foi possível pôr a janela do jogo em primeiro plano; a tecla não foi enviada")
}

func (j *JanelaWindows) Encerra() error {
	pids, err := pidsDoJogo()
	if err != nil {
		return err
	}
	var erros []error
	for _, pid := range pids {
		// Confere de novo pelo caminho do executável: nunca o LeagueClient.
		if !strings.EqualFold(exeDoPid(pid), NomeProcessoJogo) {
			continue
		}
		h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, pid)
		if err != nil {
			erros = append(erros, err)
			continue
		}
		if err := windows.TerminateProcess(h, 1); err != nil {
			erros = append(erros, err)
		}
		windows.CloseHandle(h)
	}
	return errors.Join(erros...)
}

// scancodes do teclado US/ABNT (a linha de cima e os números são iguais).
var scancodes = map[rune]uint16{
	'1': 0x02, '2': 0x03, '3': 0x04, '4': 0x05, '5': 0x06,
	'Q': 0x10, 'W': 0x11, 'E': 0x12, 'R': 0x13, 'T': 0x14,
}

type keybdInput struct {
	vk, scan    uint16
	flags, time uint32
	extra       uintptr
}

// input é o INPUT do Win32 (40 bytes no amd64: tipo, preenchimento, união de 32).
type input struct {
	tipo uint32
	_    uint32
	ki   keybdInput
	_    [8]byte
}

const (
	inputTeclado      = 1
	keyeventfKeyup    = 0x0002
	keyeventfScancode = 0x0008
	duracaoTecla      = 60 * time.Millisecond
)

// Aperta manda a tecla por scancode (é o que o jogo lê; VK puro foi ignorado
// nas medidas), ~60 ms pressionada.
func (j *JanelaWindows) Aperta(tecla rune) error {
	sc, ok := scancodes[tecla]
	if !ok || !TeclaPermitida(tecla) {
		return fmt.Errorf("tecla %q fora da lista de seleção de jogador", tecla)
	}
	envia := func(flags uint32) error {
		in := input{tipo: inputTeclado, ki: keybdInput{scan: sc, flags: flags}}
		n, _, err := pSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
		if n != 1 {
			return fmt.Errorf("SendInput: %v", err)
		}
		return nil
	}
	if err := envia(keyeventfScancode); err != nil {
		return err
	}
	time.Sleep(duracaoTecla)
	return envia(keyeventfScancode | keyeventfKeyup)
}

type rect struct{ esq, topo, dir, base int32 }

func areaCliente() (image.Rectangle, error) {
	h, err := janela()
	if err != nil {
		return image.Rectangle{}, err
	}
	var r rect
	if ok, _, _ := pGetClientRect.Call(h, uintptr(unsafe.Pointer(&r))); ok == 0 {
		return image.Rectangle{}, errors.New("GetClientRect falhou")
	}
	p := struct{ x, y int32 }{}
	pClientToScreen.Call(h, uintptr(unsafe.Pointer(&p)))
	return image.Rect(int(p.x), int(p.y), int(p.x+r.dir), int(p.y+r.base)), nil
}

func (j *JanelaWindows) Tamanho() (int, int, error) {
	a, err := areaCliente()
	if err != nil {
		return 0, 0, err
	}
	return a.Dx(), a.Dy(), nil
}

type bitmapInfoHeader struct {
	size                     uint32
	largura, altura          int32
	planos, bits             uint16
	compressao, tamImagem    uint32
	xppm, yppm               int32
	coresUsadas, coresImport uint32
}

// Captura copia da tela (GDI BitBlt) a área cliente da janela do jogo, ou a
// região dada (coordenadas relativas à área cliente).
func (j *JanelaWindows) Captura(regiao image.Rectangle) (*image.RGBA, error) {
	area, err := areaCliente()
	if err != nil {
		return nil, err
	}
	alvo := area
	if !regiao.Empty() {
		alvo = regiao.Add(area.Min).Intersect(area)
	}
	w, h := alvo.Dx(), alvo.Dy()
	if w <= 0 || h <= 0 {
		return nil, errors.New("região de captura vazia")
	}
	j.mu.Lock()
	defer j.mu.Unlock()

	tela, _, _ := pGetDC.Call(0)
	if tela == 0 {
		return nil, errors.New("GetDC falhou")
	}
	defer pReleaseDC.Call(0, tela)
	mem, _, _ := pCreateCompatibleDC.Call(tela)
	if mem == 0 {
		return nil, errors.New("CreateCompatibleDC falhou")
	}
	defer pDeleteDC.Call(mem)
	bmp, _, _ := pCreateCompatibleBitmap.Call(tela, uintptr(w), uintptr(h))
	if bmp == 0 {
		return nil, errors.New("CreateCompatibleBitmap falhou")
	}
	defer pDeleteObject.Call(bmp)
	antigo, _, _ := pSelectObject.Call(mem, bmp)
	const srccopy = 0x00CC0020
	ok, _, _ := pBitBlt.Call(mem, 0, 0, uintptr(w), uintptr(h), tela, uintptr(alvo.Min.X), uintptr(alvo.Min.Y), srccopy)
	pSelectObject.Call(mem, antigo)
	if ok == 0 {
		return nil, errors.New("BitBlt falhou")
	}
	if len(j.buf) != w*h*4 {
		j.buf = make([]byte, w*h*4)
		j.img = image.NewRGBA(image.Rect(0, 0, w, h))
	}
	bi := bitmapInfoHeader{largura: int32(w), altura: -int32(h), planos: 1, bits: 32}
	bi.size = uint32(unsafe.Sizeof(bi))
	// BITMAPINFO = cabeçalho + 1 RGBQUAD; o espaço extra evita escrita fora.
	info := make([]byte, unsafe.Sizeof(bi)+16)
	*(*bitmapInfoHeader)(unsafe.Pointer(&info[0])) = bi
	linhas, _, _ := pGetDIBits.Call(mem, bmp, 0, uintptr(h), uintptr(unsafe.Pointer(&j.buf[0])), uintptr(unsafe.Pointer(&info[0])), 0)
	if int(linhas) != h {
		return nil, errors.New("GetDIBits falhou")
	}
	// BGRA → RGBA.
	src, dst := j.buf, j.img.Pix
	for i := 0; i+3 < len(src); i += 4 {
		dst[i], dst[i+1], dst[i+2], dst[i+3] = src[i+2], src[i+1], src[i], 255
	}
	return j.img, nil
}
