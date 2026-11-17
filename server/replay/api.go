// Package replay fala com o jogo aberto em modo replay: a Replay API e o Live
// Client Data (https://127.0.0.1:2999), a janela do jogo (foco, tecla de
// seleção de jogador, captura da tela) e o game.cfg. A Sessao junta tudo e é
// quem garante as regras 2, 3 e 5 do CLAUDE.md: só abre com o cliente fora de
// partida, só manda tecla depois de a Replay API responder (prova de que é
// replay) e sempre fecha o jogo.
//
// Tudo o que toca no mundo de fora fica atrás de interface (API, Jogo,
// Teclado, Tela, Relogio), com o Falso determinístico para os testes.
// Medidas que justificam cada detalhe: docs/arquitetura/descobertas-replay.md.
package replay

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// API é o que o app pede ao jogo aberto.
type API interface {
	// Playback é o relógio do replay.
	Playback(ctx context.Context) (Playback, error)
	// MudaPlayback faz seek (Tempo), muda a velocidade ou pausa. Campo nil não muda.
	MudaPlayback(ctx context.Context, m MudaPlayback) (Playback, error)
	// Render é a câmera e a seleção.
	Render(ctx context.Context) (Render, error)
	// Jogadores é o /liveclientdata/playerlist, na ordem do jogo (ORDER e depois CHAOS).
	Jogadores(ctx context.Context) ([]Jogador, error)
	// Eventos é o /liveclientdata/eventdata (só o que passou desde o último seek).
	Eventos(ctx context.Context) ([]EventoJogo, error)
	// Estatisticas é o /liveclientdata/gamestats.
	Estatisticas(ctx context.Context) (Estatisticas, error)
	// Sai pede ao jogo para fechar (POST /Exit).
	Sai(ctx context.Context) error
}

// Playback é a resposta de GET /replay/playback. Tempos em segundos de jogo.
type Playback struct {
	Duracao    float64 `json:"length"`
	Pausado    bool    `json:"paused"`
	Buscando   bool    `json:"seeking"`
	Velocidade float64 `json:"speed"`
	Tempo      float64 `json:"time"`
}

// MudaPlayback é o corpo de POST /replay/playback.
type MudaPlayback struct {
	Tempo      *float64 `json:"time,omitempty"`
	Velocidade *float64 `json:"speed,omitempty"`
	Pausado    *bool    `json:"paused,omitempty"`
}

// Vetor3 é uma posição ou rotação da câmera.
type Vetor3 struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

// Render é o pedaço de GET /replay/render que o app usa.
type Render struct {
	CameraPosicao Vetor3  `json:"cameraPosition"`
	CameraRotacao Vetor3  `json:"cameraRotation"`
	CampoDeVisao  float64 `json:"fieldOfView"`
	CameraModo    string  `json:"cameraMode"`
	CameraPresa   bool    `json:"cameraAttached"`
	Selecao       string  `json:"selectionName"`
}

// Ponto é um ponto na tela (pixels). Valido=false quando o jogo devolveu o
// marcador de "fora da tela" (3.4e38) ou um texto que não deu para ler.
type Ponto struct {
	X, Y   float64
	Valido bool
}

// ForaDaTela é o valor que o jogo usa para "não está na tela" (FLT_MAX).
const ForaDaTela = 3.4e38

// LePonto lê o "x,y" de screenPositionCenter (texto com ponto decimal).
func LePonto(s string) Ponto {
	partes := strings.Split(strings.TrimSpace(s), ",")
	if len(partes) != 2 {
		return Ponto{}
	}
	x, e1 := strconv.ParseFloat(strings.TrimSpace(partes[0]), 64)
	y, e2 := strconv.ParseFloat(strings.TrimSpace(partes[1]), 64)
	if e1 != nil || e2 != nil || math.Abs(x) > 1e30 || math.Abs(y) > 1e30 || math.IsNaN(x) || math.IsNaN(y) {
		return Ponto{}
	}
	return Ponto{X: x, Y: y, Valido: true}
}

// Dentro diz se o ponto está dentro de uma tela larg×alt, com margem (pixels)
// para dentro.
func (p Ponto) Dentro(larg, alt, margem float64) bool {
	return p.Valido && p.X >= margem && p.Y >= margem && p.X <= larg-margem && p.Y <= alt-margem
}

// Item é um item no inventário de um jogador.
type Item struct {
	ID         int `json:"itemID"`
	Slot       int `json:"slot"`
	Quantidade int `json:"count"`
}

// Placar é o scores do playerlist.
type Placar struct {
	Abates       int     `json:"kills"`
	Mortes       int     `json:"deaths"`
	Assistencias int     `json:"assists"`
	CS           int     `json:"creepScore"`
	Ward         float64 `json:"wardScore"`
}

// Jogador é uma linha do playerlist.
type Jogador struct {
	Nome        string  `json:"riotIdGameName"`
	Tag         string  `json:"riotIdTagLine"`
	Invocador   string  `json:"summonerName"`
	Campeao     string  `json:"championName"`
	Time        string  `json:"team"`
	Posicao     string  `json:"position"`
	Nivel       int     `json:"level"`
	Morto       bool    `json:"isDead"`
	Renasce     float64 `json:"respawnTimer"`
	Itens       []Item  `json:"items"`
	Placar      Placar  `json:"scores"`
	CentroTexto string  `json:"screenPositionCenter"`
	// Centro é CentroTexto já lido.
	Centro Ponto `json:"-"`
}

// NomeDeSelecao é o nome que aparece em render.selectionName para o jogador.
func (j Jogador) NomeDeSelecao() string {
	if j.Nome != "" {
		return j.Nome
	}
	return j.Invocador
}

// EventoJogo é um item de eventdata. Bruto guarda o JSON inteiro, porque cada
// tipo de evento tem campos próprios (DragonType, TurretKilled...).
type EventoJogo struct {
	ID          int             `json:"EventID"`
	Nome        string          `json:"EventName"`
	Tempo       float64         `json:"EventTime"`
	Matador     string          `json:"KillerName"`
	Vitima      string          `json:"VictimName"`
	Assistentes []string        `json:"Assisters"`
	Bruto       json.RawMessage `json:"-"`
}

// Estatisticas é o pedaço de gamestats que o app usa.
type Estatisticas struct {
	TempoJogo float64 `json:"gameTime"`
	Mapa      int     `json:"mapNumber"`
	Modo      string  `json:"gameMode"`
}

// ErrJogoFechado: a porta 2999 não atende (o jogo não está aberto ou ainda
// carregando).
var ErrJogoFechado = errors.New("o jogo (replay) não está respondendo em 127.0.0.1:2999")

// Real é o cliente HTTP de verdade.
type Real struct {
	base string
	http *http.Client
}

// EnderecoPadrao é onde o jogo escuta.
const EnderecoPadrao = "https://127.0.0.1:2999"

// NovaAPI cria o cliente em base (EnderecoPadrao no app; httptest nos testes).
func NovaAPI(base string) *Real {
	discador := &net.Dialer{Timeout: 2 * time.Second}
	return &Real{base: strings.TrimRight(base, "/"), http: &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			Proxy: nil,
			// O certificado do jogo é autoassinado. Sem verificação TLS só porque o
			// discador recusa tudo o que não é 127.0.0.1: a conexão nunca sai da máquina.
			DialContext: func(ctx context.Context, rede, endereco string) (net.Conn, error) {
				host, _, err := net.SplitHostPort(endereco)
				if err != nil || host != "127.0.0.1" {
					return nil, fmt.Errorf("replay: conexão recusada para %q: o jogo só é chamado em 127.0.0.1", endereco)
				}
				return discador.DialContext(ctx, rede, endereco)
			},
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
			MaxIdleConnsPerHost: 4,
			IdleConnTimeout:     30 * time.Second,
		},
	}}
}

func (c *Real) pede(ctx context.Context, metodo, caminho string, corpo any, destino any) error {
	var leitor io.Reader
	if corpo != nil {
		b, err := json.Marshal(corpo)
		if err != nil {
			return err
		}
		leitor = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, metodo, c.base+caminho, leitor)
	if err != nil {
		return err
	}
	if corpo != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w: %v", ErrJogoFechado, err)
	}
	defer resp.Body.Close()
	dados, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return fmt.Errorf("ler a resposta do jogo em %s: %w", caminho, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		s := strings.TrimSpace(string(dados))
		if len(s) > 200 {
			s = s[:200]
		}
		return fmt.Errorf("o jogo respondeu %d em %s: %s", resp.StatusCode, caminho, s)
	}
	if destino == nil || len(dados) == 0 {
		return nil
	}
	if err := json.Unmarshal(dados, destino); err != nil {
		return fmt.Errorf("resposta inesperada do jogo em %s: %w", caminho, err)
	}
	return nil
}

func (c *Real) Playback(ctx context.Context) (Playback, error) {
	var p Playback
	err := c.pede(ctx, http.MethodGet, "/replay/playback", nil, &p)
	return p, err
}

func (c *Real) MudaPlayback(ctx context.Context, m MudaPlayback) (Playback, error) {
	var p Playback
	err := c.pede(ctx, http.MethodPost, "/replay/playback", m, &p)
	return p, err
}

func (c *Real) Render(ctx context.Context) (Render, error) {
	var r Render
	err := c.pede(ctx, http.MethodGet, "/replay/render", nil, &r)
	return r, err
}

func (c *Real) Jogadores(ctx context.Context) ([]Jogador, error) {
	var js []Jogador
	if err := c.pede(ctx, http.MethodGet, "/liveclientdata/playerlist", nil, &js); err != nil {
		return nil, err
	}
	for i := range js {
		js[i].Centro = LePonto(js[i].CentroTexto)
	}
	return js, nil
}

func (c *Real) Eventos(ctx context.Context) ([]EventoJogo, error) {
	var r struct {
		Events []json.RawMessage `json:"Events"`
	}
	if err := c.pede(ctx, http.MethodGet, "/liveclientdata/eventdata", nil, &r); err != nil {
		return nil, err
	}
	return LeEventos(r.Events), nil
}

// LeEventos converte os eventos crus guardando o JSON de cada um.
func LeEventos(crus []json.RawMessage) []EventoJogo {
	evs := make([]EventoJogo, 0, len(crus))
	for _, b := range crus {
		var e EventoJogo
		if json.Unmarshal(b, &e) != nil {
			continue
		}
		e.Bruto = append(json.RawMessage(nil), b...)
		evs = append(evs, e)
	}
	return evs
}

func (c *Real) Estatisticas(ctx context.Context) (Estatisticas, error) {
	var e Estatisticas
	err := c.pede(ctx, http.MethodGet, "/liveclientdata/gamestats", nil, &e)
	return e, err
}

func (c *Real) Sai(ctx context.Context) error {
	return c.pede(ctx, http.MethodPost, "/Exit", nil, nil)
}

// Ptr é o atalho para montar MudaPlayback.
func Ptr[T any](v T) *T { return &v }
