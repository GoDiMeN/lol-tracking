package replay

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"
)

// LCU é o pedaço do cliente do LoL que a sessão usa (lcu.Cliente serve).
type LCU interface {
	FaseGameflow(ctx context.Context) (string, error)
	AssisteReplay(ctx context.Context, gameID int64) error
}

// Deps é o mundo de fora de uma sessão.
type Deps struct {
	LCU     LCU
	API     API
	Jogo    Jogo
	Teclado Teclado
	Tela    Tela
	Relogio Relogio
	// GameCfg é o caminho do game.cfg; vazio = não confere (testes).
	GameCfg string
}

// Tempos da sessão. Variáveis para o teste (com relógio virtual nem precisa).
var (
	// PrazoAbrir: do watch até a Replay API responder (o jogo carrega em ~20–40 s).
	PrazoAbrir = 2 * time.Minute
	// PrazoCarregar: da API responder até o playerlist ter os 10.
	PrazoCarregar = time.Minute
	// PrazoSair: depois do /Exit, quanto esperar o processo sumir antes de matar (regra 5).
	PrazoSair = 15 * time.Second
	// IntervaloEspera: de quanto em quanto as esperas acima consultam.
	IntervaloEspera = 500 * time.Millisecond
	// EntreTeclas: pausa entre o 1º e o 2º aperto (o duplo é o que prende a câmera).
	EntreTeclas = 150 * time.Millisecond
	// EsperaTrava: depois do 2º aperto, quanto esperar a câmera chegar antes de conferir.
	EsperaTrava = 600 * time.Millisecond
	// MaxTentativasTrava: quantas vezes tentar prender a câmera antes de desistir.
	MaxTentativasTrava = 5
)

// FasePermitida é a regra 2: só fora de partida ("None") ou já assistindo
// replay ("WatchInProgress").
func FasePermitida(fase string) bool {
	return fase == "None" || fase == "WatchInProgress"
}

// ErroFase: o cliente está numa fase em que o app não abre replay nem manda tecla.
type ErroFase struct{ Fase string }

func (e *ErroFase) Error() string {
	return fmt.Sprintf("o cliente do LoL está na fase %q (partida ao vivo ou fila); o rastreio só roda fora de partida", e.Fase)
}

// ErrSemReplay: a tecla foi recusada porque não há prova de que a janela é um
// replay (a Replay API não respondeu, ou a sessão já fechou).
var ErrSemReplay = errors.New("a Replay API não respondeu: sem prova de que o jogo aberto é um replay, nenhuma tecla é enviada")

// ErrMorto: o jogador está morto; a câmera não fica nele (descobertas 3.2).
var ErrMorto = errors.New("o jogador está morto")

// ErrTravaFalhou: a câmera não prendeu no jogador depois de todas as tentativas.
var ErrTravaFalhou = errors.New("a câmera não prendeu no jogador")

// Sessao é um replay aberto. Criada por Abre; quem a recebe faz defer Fecha().
type Sessao struct {
	d      Deps
	GameID int64
	// Jogadores é o playerlist no momento em que o replay terminou de carregar
	// (a ordem define as teclas).
	Jogadores []Jogador
	// Duracao do replay em segundos de jogo.
	Duracao float64
	// Largura e Altura da área do jogo em pixels (o espaço do screenPositionCenter).
	Largura, Altura int

	mu         sync.Mutex
	confirmado bool
	fechada    bool
	umaVez     sync.Once
	errFecha   error
}

// Deps devolve as dependências (o motor usa a mesma API e tela).
func (s *Sessao) Deps() Deps { return s.d }

func confereFase(ctx context.Context, l LCU) error {
	fase, err := l.FaseGameflow(ctx)
	if err != nil {
		return fmt.Errorf("consultar a fase do cliente do LoL: %w", err)
	}
	if !FasePermitida(fase) {
		return &ErroFase{Fase: fase}
	}
	return nil
}

// Abre abre o replay da partida e espera ele carregar. Na ordem:
//  1. confere a fase do gameflow (regra 2) — recusa sem abrir nada;
//  2. garante EnableReplayApi=1 no game.cfg;
//  3. fecha um jogo que tenha sobrado aberto (replay anterior que não fechou);
//  4. pede o watch ao LCU e espera a Replay API responder (é a prova de replay);
//  5. espera o playerlist ter os 10.
//
// Se falhar depois do watch, fecha o jogo antes de devolver o erro (também em
// pânico). Com sucesso, fechar é com quem chamou: defer s.Fecha().
func Abre(ctx context.Context, d Deps, gameID int64) (_ *Sessao, errAbre error) {
	if d.Relogio == nil {
		d.Relogio = RelogioReal{}
	}
	s := &Sessao{d: d, GameID: gameID}
	if err := confereFase(ctx, d.LCU); err != nil {
		return nil, err
	}
	if d.GameCfg != "" {
		if mudou, err := GaranteReplayAPI(d.GameCfg); err != nil {
			return nil, err
		} else if mudou {
			log.Printf("replay: EnableReplayApi=1 gravado em %s (backup em %s%s)", d.GameCfg, d.GameCfg, SufixoBackup)
		}
	}
	if d.Jogo.Aberto() {
		log.Printf("replay: havia um jogo aberto antes do rastreio; fechando")
		if err := FechaJogo(d); err != nil {
			return nil, fmt.Errorf("fechar o jogo que estava aberto: %w", err)
		}
	}

	ok := false
	defer func() {
		if !ok {
			s.Fecha()
		}
	}()
	if err := d.LCU.AssisteReplay(ctx, gameID); err != nil {
		return nil, fmt.Errorf("pedir ao cliente para abrir o replay: %w", err)
	}
	limite := d.Relogio.Agora().Add(PrazoAbrir)
	for {
		pb, err := d.API.Playback(ctx)
		if err == nil && pb.Duracao > 0 {
			s.Duracao = pb.Duracao
			break
		}
		if ctx.Err() != nil {
			return nil, context.Cause(ctx)
		}
		if d.Relogio.Agora().After(limite) {
			return nil, fmt.Errorf("o replay não respondeu em %s: %w", PrazoAbrir, ErrSemReplay)
		}
		if err := d.Relogio.Dorme(ctx, IntervaloEspera); err != nil {
			return nil, err
		}
	}
	s.mu.Lock()
	s.confirmado = true
	s.mu.Unlock()

	limite = d.Relogio.Agora().Add(PrazoCarregar)
	for {
		js, err := d.API.Jogadores(ctx)
		if err == nil && len(js) >= 10 {
			s.Jogadores = js
			break
		}
		if d.Relogio.Agora().After(limite) {
			return nil, fmt.Errorf("o replay abriu mas o playerlist não trouxe os 10 jogadores em %s", PrazoCarregar)
		}
		if err := d.Relogio.Dorme(ctx, IntervaloEspera); err != nil {
			return nil, err
		}
	}
	if w, h, err := d.Tela.Tamanho(); err == nil {
		s.Largura, s.Altura = w, h
	} else {
		return nil, fmt.Errorf("tamanho da janela do jogo: %w", err)
	}
	ok = true
	return s, nil
}

// Fecha fecha o replay (regra 5). Idempotente: pode estar no defer e ser
// chamado também no caminho normal.
func (s *Sessao) Fecha() error {
	s.umaVez.Do(func() {
		s.mu.Lock()
		s.fechada = true
		s.mu.Unlock()
		s.errFecha = FechaJogo(s.d)
		if s.errFecha != nil {
			log.Printf("replay: fechar o jogo: %v", s.errFecha)
		}
	})
	return s.errFecha
}

// FechaJogo pede /Exit, espera até PrazoSair o processo sumir e, se não sumir,
// encerra SÓ o processo "League of Legends". Não depende de contexto: tem de
// rodar também quando o rastreio foi cancelado.
func FechaJogo(d Deps) error {
	if d.Relogio == nil {
		d.Relogio = RelogioReal{}
	}
	bg := context.Background()
	c, cancela := context.WithTimeout(bg, 3*time.Second)
	if err := d.API.Sai(c); err != nil {
		log.Printf("replay: /Exit: %v", err)
	}
	cancela()
	espera := func(prazo time.Duration) bool {
		limite := d.Relogio.Agora().Add(prazo)
		for d.Jogo.Aberto() {
			if d.Relogio.Agora().After(limite) {
				return false
			}
			d.Relogio.Dorme(bg, IntervaloEspera)
		}
		return true
	}
	if espera(PrazoSair) {
		return nil
	}
	log.Printf("replay: o jogo não fechou em %s depois do /Exit; encerrando o processo do jogo", PrazoSair)
	if err := d.Jogo.Encerra(); err != nil {
		return fmt.Errorf("encerrar o processo do jogo: %w", err)
	}
	if !espera(5 * time.Second) {
		return errors.New("o processo do jogo continua aberto depois de encerrado")
	}
	return nil
}

// Tecla manda uma tecla de seleção ao jogo, só se: a tecla é uma das 10, a
// sessão provou que é replay e não fechou, a fase do cliente continua
// permitida, a Replay API responde agora, e a janela do jogo está em primeiro
// plano (conferido). Qualquer falha = nenhuma tecla.
func (s *Sessao) Tecla(ctx context.Context, t rune) error {
	if !TeclaPermitida(t) {
		return fmt.Errorf("tecla %q fora da lista de seleção de jogador", t)
	}
	s.mu.Lock()
	ok := s.confirmado && !s.fechada
	s.mu.Unlock()
	if !ok {
		return ErrSemReplay
	}
	if err := confereFase(ctx, s.d.LCU); err != nil {
		return err
	}
	if _, err := s.d.API.Playback(ctx); err != nil {
		return fmt.Errorf("%w (%v)", ErrSemReplay, err)
	}
	if err := s.d.Jogo.Foco(); err != nil {
		return err
	}
	return s.d.Teclado.Aperta(t)
}

// Margem (pixels) que o centro do campeão precisa ter da borda para a trava
// contar como boa.
const MargemTela = 4

// ConfereTrava diz se a câmera está presa no jogador: a seleção é ele, ele está
// vivo e o centro dele está dentro da tela. Só a seleção não basta (o `T`
// pausado muda a seleção sem mover a câmera — descobertas 3.1).
func (s *Sessao) ConfereTrava(r Render, js []Jogador, nome string) (bool, string) {
	return ConfereTrava(r, js, nome, float64(s.Largura), float64(s.Altura))
}

// ConfereTrava é a conferência pura (sem sessão), para o motor e os testes.
func ConfereTrava(r Render, js []Jogador, nome string, larg, alt float64) (bool, string) {
	var j *Jogador
	for i := range js {
		if js[i].NomeDeSelecao() == nome {
			j = &js[i]
		}
	}
	switch {
	case j == nil:
		return false, "jogador fora do playerlist"
	case j.Morto:
		return false, "morto"
	case r.Selecao != nome:
		return false, fmt.Sprintf("seleção %q", r.Selecao)
	case !j.Centro.Dentro(larg, alt, MargemTela):
		return false, "fora da tela"
	}
	return true, ""
}

// TravaCamera prende a câmera no jogador: com o jogo RODANDO (pausado o `T`
// falha), aperta a tecla dele duas vezes, espera e confere. Tenta de novo até
// MaxTentativasTrava. Devolve quantas tentativas usou. ErrMorto se o jogador
// está morto (não há o que travar).
func (s *Sessao) TravaCamera(ctx context.Context, nome string) (int, error) {
	tecla, err := TeclaDo(s.Jogadores, nome)
	if err != nil {
		return 0, err
	}
	motivo := ""
	for tentativa := 1; tentativa <= MaxTentativasTrava; tentativa++ {
		pb, err := s.d.API.Playback(ctx)
		if err != nil {
			return tentativa - 1, fmt.Errorf("%w (%v)", ErrSemReplay, err)
		}
		if pb.Pausado {
			if _, err := s.d.API.MudaPlayback(ctx, MudaPlayback{Pausado: Ptr(false)}); err != nil {
				return tentativa - 1, err
			}
		}
		js, err := s.d.API.Jogadores(ctx)
		if err != nil {
			return tentativa - 1, err
		}
		for _, j := range js {
			if j.NomeDeSelecao() == nome && j.Morto {
				return tentativa - 1, ErrMorto
			}
		}
		if err := s.Tecla(ctx, tecla); err != nil {
			return tentativa, err
		}
		if err := s.d.Relogio.Dorme(ctx, EntreTeclas); err != nil {
			return tentativa, err
		}
		if err := s.Tecla(ctx, tecla); err != nil {
			return tentativa, err
		}
		if err := s.d.Relogio.Dorme(ctx, EsperaTrava); err != nil {
			return tentativa, err
		}
		r, err := s.d.API.Render(ctx)
		if err != nil {
			return tentativa, err
		}
		js, err = s.d.API.Jogadores(ctx)
		if err != nil {
			return tentativa, err
		}
		ok, m := s.ConfereTrava(r, js, nome)
		if ok {
			return tentativa, nil
		}
		if m == "morto" {
			return tentativa, ErrMorto
		}
		motivo = m
	}
	return MaxTentativasTrava, fmt.Errorf("%w %s (%s)", ErrTravaFalhou, nome, motivo)
}
