package ia

import (
	"context"
	"sync/atomic"
)

// Fila deixa passar uma chamada por vez: o Claude Code é um processo pesado
// e a cota é da pessoa. Quem chega com outra rodando espera a vez (ou desiste
// quando o contexto dele é cancelado). Cumpre Cliente.
type Fila struct {
	c       Cliente
	vez     chan struct{}
	esperam atomic.Int32
	rodando atomic.Bool
}

// NovaFila embrulha o cliente.
func NovaFila(c Cliente) *Fila {
	return &Fila{c: c, vez: make(chan struct{}, 1)}
}

// Ocupada diz se há chamada rodando (a tela avisa "na fila").
func (f *Fila) Ocupada() bool { return f.rodando.Load() }

// Esperando: quantas chamadas esperam a vez.
func (f *Fila) Esperando() int { return int(f.esperam.Load()) }

// Estado repassa (não ocupa a vez).
func (f *Fila) Estado(ctx context.Context) Estado { return f.c.Estado(ctx) }

// Conversa espera a vez e chama.
func (f *Fila) Conversa(ctx context.Context, p Pedido, aoTrecho func(string)) (Resposta, error) {
	f.esperam.Add(1)
	select {
	case f.vez <- struct{}{}:
		f.esperam.Add(-1)
	case <-ctx.Done():
		f.esperam.Add(-1)
		return Resposta{}, &erroIA{"a chamada à IA foi cancelada enquanto esperava a vez", ctx.Err()}
	}
	f.rodando.Store(true)
	defer func() {
		f.rodando.Store(false)
		<-f.vez
	}()
	return f.c.Conversa(ctx, p, aoTrecho)
}
