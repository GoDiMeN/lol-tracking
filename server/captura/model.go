// Package captura é o "pausar a captura, seja qual for" (2026-11-18, pedido do
// dono: "coloque algo para pausar quando quero falar com você"). Captura é
// tudo o que abre o replay e manda tecla: o rastreio, a coleta por elo (que
// roda rastreios), a repescagem de passe gravado e os passes de visão avulsos.
// O atalho global do Electron (Ctrl+Alt+P), o botão da barra de título e o
// item da bandeja chamam as mesmas rotas daqui.
//
// A pausa é a "agora" de sempre: o replay fecha, o passe guarda o último t, e
// a retomada continua do ponto (seek 4 s antes, só grava depois do ponto: sem
// buraco, TestPausaERetoma). Se a parte já gravada do passe cai no critério de
// passe ruim, a retomada recomeça o passe do início (rastreio.recomecaSeRuim).
package captura

import (
	"context"
	"errors"

	"loltracking/server/aprendizado"
	"loltracking/server/rastreio"
)

// Tipos de captura (os da fila e mais a coleta).
const TipoColeta = "coleta"

// Fila é o pedaço da fila de rastreio que a captura usa (rastreio.Fila).
type Fila interface {
	CorridaAtual() (rastreio.Corrida, bool)
	UltimaParada() (rastreio.Parada, bool)
	PausaAgora() (rastreio.Corrida, error)
	RetomaParada(ctx context.Context) (rastreio.Parada, string, error)
	EsqueceParada()
}

// Coletas é o pedaço do coletor que a captura usa (aprendizado.Coletor).
type Coletas interface {
	ColetaAtual() (int64, string, bool)
	PausaAgora(id int64) error
	Retoma(ctx context.Context, id int64) (string, error)
	ColetaDoRastreio(rastreioID int64) (int64, bool)
	EstadoDaColeta(id int64) (string, string)
}

var (
	_ Fila    = (*rastreio.Fila)(nil)
	_ Coletas = (*aprendizado.Coletor)(nil)
)

// Captura é uma captura rodando ou pausada.
type Captura struct {
	Tipo     string           `json:"tipo"`
	ID       int64            `json:"id"`
	GameID   int64            `json:"game_id"`
	Execucao *rastreio.IDExec `json:"execucao"`
	// Esperando: o motor está esperando a pessoa parar de usar o computador
	// (o replay pausado; volta sozinho com o computador parado).
	Esperando *rastreio.EsperaUsuario `json:"esperando"`
	// Pausando: a pausa foi pedida e o replay está fechando.
	Pausando bool `json:"pausando"`
	// Motivo: por que parou (vazio = pausa pedida pela pessoa).
	Motivo string `json:"motivo"`
	Em     string `json:"em"`
}

// Estado é a resposta de GET /api/captura.
type Estado struct {
	Rodando *Captura `json:"rodando"`
	Pausada *Captura `json:"pausada"`
}

// Servico junta a fila e o coletor.
type Servico struct {
	Fila    Fila
	Coletas Coletas
}

// ErrNada: pausar com nada rodando, ou retomar sem nada pausado (409).
var ErrNada = errors.New("nada para fazer")

// Erro409 é a recusa com frase (409).
type Erro409 struct{ Msg string }

func (e *Erro409) Error() string { return e.Msg }

// Le monta o estado: o que roda agora e a última captura pausada.
func (s *Servico) Le() Estado {
	var e Estado
	cor, rodando := s.Fila.CorridaAtual()
	if id, ex, ok := s.Coletas.ColetaAtual(); ok {
		c := &Captura{Tipo: TipoColeta, ID: id, Execucao: &rastreio.IDExec{ID: ex}}
		if rodando {
			c.GameID, c.Esperando, c.Pausando = cor.GameID, cor.Esperando, cor.Pausando
		}
		e.Rodando = c
	} else if rodando {
		e.Rodando = &Captura{Tipo: cor.Tipo, ID: cor.ID, GameID: cor.GameID, Execucao: cor.Exec, Esperando: cor.Esperando,
			Pausando: cor.Pausando}
	}
	if e.Rodando == nil {
		e.Pausada = s.pausada()
	}
	return e
}

// pausada: a última parada da fila; se o rastreio é de uma coleta, a coleta.
func (s *Servico) pausada() *Captura {
	p, ok := s.Fila.UltimaParada()
	if !ok {
		return nil
	}
	c := &Captura{Tipo: p.Tipo, ID: p.ID, GameID: p.GameID, Motivo: p.Motivo, Em: p.Em}
	if p.Tipo == rastreio.TipoRastreio {
		if col, ok := s.Coletas.ColetaDoRastreio(p.ID); ok {
			estado, erro := s.Coletas.EstadoDaColeta(col)
			if estado != "pausado" && estado != "falhou" {
				return nil
			}
			c.Tipo, c.ID = TipoColeta, col
			if c.Motivo == "" {
				c.Motivo = erro
			}
		}
	}
	return c
}

// Pausa para AGORA o que estiver rodando.
func (s *Servico) Pausa() (Captura, error) {
	if id, _, ok := s.Coletas.ColetaAtual(); ok {
		if err := s.Coletas.PausaAgora(id); err != nil {
			return Captura{}, err
		}
		c := Captura{Tipo: TipoColeta, ID: id, Pausando: true}
		if cor, ok := s.Fila.CorridaAtual(); ok {
			c.GameID = cor.GameID
		}
		return c, nil
	}
	cor, err := s.Fila.PausaAgora()
	if errors.Is(err, rastreio.ErrNadaRodando) {
		return Captura{}, &Erro409{"nada está sendo capturado agora"}
	}
	if err != nil {
		return Captura{}, err
	}
	return Captura{Tipo: cor.Tipo, ID: cor.ID, GameID: cor.GameID, Execucao: cor.Exec, Pausando: true}, nil
}

// Retoma recomeça a última captura pausada. Devolve a execução nova.
func (s *Servico) Retoma(ctx context.Context) (Captura, string, error) {
	if e := s.Le(); e.Rodando != nil {
		return *e.Rodando, "", &Erro409{"já há uma captura rodando; pause antes"}
	}
	p := s.pausada()
	if p == nil {
		return Captura{}, "", &Erro409{"não há captura pausada para retomar"}
	}
	if p.Tipo == TipoColeta {
		ex, err := s.Coletas.Retoma(ctx, p.ID)
		if err != nil {
			return *p, "", err
		}
		s.Fila.EsqueceParada()
		p.Execucao = &rastreio.IDExec{ID: ex}
		return *p, ex, nil
	}
	_, ex, err := s.Fila.RetomaParada(ctx)
	if err != nil {
		return *p, "", err
	}
	p.Execucao = &rastreio.IDExec{ID: ex}
	return *p, ex, nil
}

// Alterna: pausa o que roda; sem nada rodando, retoma a última pausada (o
// atalho global e a bandeja usam). acao = "pausou" ou "retomou".
func (s *Servico) Alterna(ctx context.Context) (string, Captura, string, error) {
	if e := s.Le(); e.Rodando != nil {
		if e.Rodando.Pausando {
			return "pausou", *e.Rodando, "", nil
		}
		c, err := s.Pausa()
		return "pausou", c, "", err
	}
	c, ex, err := s.Retoma(ctx)
	return "retomou", c, ex, err
}
