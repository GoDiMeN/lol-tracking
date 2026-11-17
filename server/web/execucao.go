package web

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"loltracking/server/banco"
)

// Trabalho demorado (baixar replay, e depois o rastreio) não segura a resposta
// HTTP: a rota cria uma Execucao, responde 202 com o id, e o trabalho publica
// eventos que o renderer lê por SSE em GET /api/execucoes/{id}/eventos.
//
// O hub guarda em memória tudo o que cada execução publicou: o renderer
// costuma abrir o EventSource depois do 202, quando os primeiros eventos já
// saíram, e reconectar no meio. Quem conecta recebe o histórico e segue ao vivo.
// O que precisa sobreviver a um reinício (o replay baixado, a linha do tempo)
// vai para o banco pelo pacote dono; o hub é só o "ao vivo".

// Retencao é quanto uma execução terminada continua consultável. Variável só
// para o teste encurtar.
var Retencao = 30 * time.Minute

// Evento é uma mensagem do SSE. Dados já vai serializado: a cópia guardada não
// muda se quem emitiu mexer no valor depois.
type Evento struct {
	ID    int             `json:"id"`
	Tipo  string          `json:"tipo"`
	Dados json.RawMessage `json:"dados"`
}

// Hub é o registro das execuções do processo. Um só, criado no main.
type Hub struct {
	mu        sync.Mutex
	execucoes map[string]*Execucao
}

// NovoHub cria o hub e a limpeza periódica das execuções vencidas.
func NovoHub() *Hub {
	h := &Hub{execucoes: map[string]*Execucao{}}
	go func() {
		for agora := range time.Tick(time.Minute) {
			h.limpa(agora)
		}
	}()
	return h
}

func (h *Hub) limpa(agora time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, e := range h.execucoes {
		e.mu.Lock()
		vencida := e.fechada && agora.Sub(e.fechadaEm) > Retencao
		e.mu.Unlock()
		if vencida {
			delete(h.execucoes, id)
		}
	}
}

// Execucao é um trabalho com começo, meio e fim.
type Execucao struct {
	id        string
	mu        sync.Mutex
	eventos   []Evento
	fechada   bool
	fechadaEm time.Time
	// novo é fechado e trocado a cada evento: todos os ouvintes acordam juntos
	// sem fila própria.
	novo      chan struct{}
	cancelar  func()
	cancelada bool
}

// Nova cria e registra uma execução aberta.
func (h *Hub) Nova() *Execucao {
	e := &Execucao{id: banco.NovoID(), novo: make(chan struct{})}
	h.mu.Lock()
	h.execucoes[e.id] = e
	h.mu.Unlock()
	return e
}

// Busca acha a execução pelo id enquanto ela está na memória.
func (h *Hub) Busca(id string) (*Execucao, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	e, ok := h.execucoes[id]
	return e, ok
}

// ID é o id devolvido no 202.
func (e *Execucao) ID() string { return e.id }

// Emite publica um evento. Depois do "fim" é ignorado: ninguém mais o leria.
func (e *Execucao) Emite(tipo string, dados any) {
	bruto := json.RawMessage("{}")
	if dados != nil {
		if b, err := json.Marshal(dados); err == nil {
			bruto = b
		} else {
			log.Printf("execução: evento %q não serializou: %v", tipo, err)
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.fechada {
		return
	}
	e.emiteTravado(tipo, bruto)
}

func (e *Execucao) emiteTravado(tipo string, dados json.RawMessage) {
	e.eventos = append(e.eventos, Evento{ID: len(e.eventos) + 1, Tipo: tipo, Dados: dados})
	close(e.novo)
	e.novo = make(chan struct{})
}

// Fecha emite "fim" e encerra. Idempotente: o trabalho costuma ter um defer
// Fecha() além do fechamento explícito.
func (e *Execucao) Fecha() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.fechada {
		return
	}
	fim := json.RawMessage("{}")
	if e.cancelada {
		fim = json.RawMessage(`{"cancelada":true}`)
	}
	e.emiteTravado("fim", fim)
	e.fechada = true
	e.fechadaEm = time.Now()
}

// Falha emite "erro" {"texto": msg} e fecha.
func (e *Execucao) Falha(msg string) {
	e.Emite("erro", map[string]string{"texto": msg})
	e.Fecha()
}

// AoCancelar registra como parar o trabalho (em geral o cancel do contexto).
// Registrado antes do 202, para não existir id sem o que parar.
func (e *Execucao) AoCancelar(fn func()) {
	e.mu.Lock()
	e.cancelar = fn
	e.mu.Unlock()
}

// Cancela é o "parar" da pessoa. Idempotente. A função roda fora da trava
// porque quem escuta o contexto emite eventos nesta mesma execução.
func (e *Execucao) Cancela() {
	e.mu.Lock()
	if e.fechada || e.cancelada || e.cancelar == nil {
		e.mu.Unlock()
		return
	}
	e.cancelada = true
	fn := e.cancelar
	e.mu.Unlock()
	fn()
}

// Eventos devolve uma cópia de tudo o que saiu (os testes usam).
func (e *Execucao) Eventos() []Evento {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]Evento(nil), e.eventos...)
}

// Fechada diz se a execução já emitiu "fim".
func (e *Execucao) Fechada() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.fechada
}

// depois devolve, sob a mesma trava, os eventos depois do n-ésimo, o aviso do
// próximo e se fechou: sem janela para um evento escapar entre a cópia e a espera.
func (e *Execucao) depois(n int) ([]Evento, <-chan struct{}, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var novos []Evento
	if n < len(e.eventos) {
		novos = append(novos, e.eventos[n:]...)
	}
	return novos, e.novo, e.fechada
}

// Servir é o SSE de uma execução: o que já saiu (a partir do Last-Event-ID) e
// depois ao vivo até o "fim".
func (h *Hub) Servir(w http.ResponseWriter, r *http.Request, id string) {
	e, ok := h.Busca(id)
	if !ok {
		WriteErr(w, http.StatusNotFound, "execução não encontrada (ela pode ter expirado)")
		return
	}
	visto := 0
	if s := strings.TrimSpace(r.Header.Get("Last-Event-ID")); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			visto = n
		}
	}
	f, ok := w.(http.Flusher)
	if !ok {
		WriteErr(w, http.StatusInternalServerError, "o servidor não conseguiu abrir o fluxo de eventos")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	f.Flush()
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		novos, aviso, fechada := e.depois(visto)
		for _, ev := range novos {
			// json.Marshal nunca põe quebra de linha crua: o data cabe numa linha.
			if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", ev.ID, ev.Tipo, ev.Dados); err != nil {
				return
			}
			f.Flush()
			visto = ev.ID
			if ev.Tipo == "fim" {
				return
			}
		}
		if fechada {
			return
		}
		select {
		case <-aviso:
		case <-ping.C:
			// Sem tráfego o Chromium derruba a conexão ociosa; o comentário a mantém.
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			f.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// RegistraExecucoes pendura as rotas comuns a toda execução.
func RegistraExecucoes(mux *http.ServeMux, h *Hub) {
	mux.HandleFunc("GET /api/execucoes/{id}/eventos", func(w http.ResponseWriter, r *http.Request) {
		h.Servir(w, r, r.PathValue("id"))
	})
	mux.HandleFunc("POST /api/execucoes/{id}/cancelar", func(w http.ResponseWriter, r *http.Request) {
		e, ok := h.Busca(r.PathValue("id"))
		if !ok {
			WriteErr(w, http.StatusNotFound, "execução não encontrada")
			return
		}
		// Já terminada também é 204: o efeito que a pessoa quer (nada rodando) já existe.
		e.Cancela()
		w.WriteHeader(http.StatusNoContent)
	})
}
