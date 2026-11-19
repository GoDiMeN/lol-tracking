package captura

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"loltracking/server/rastreio"
)

// filaFalsa e coletasFalsas: o mínimo para conferir o roteamento (o que pausa
// o quê, e o que "retomar a última" retoma). A pausa de verdade (o replay
// fecha, o passe guarda o ponto, a retomada sem buraco) está nos testes do
// pacote rastreio (TestPausaAgoraERetomaAUltima).
type filaFalsa struct {
	rodando  *rastreio.Corrida
	parada   *rastreio.Parada
	pausadas int
	retomou  int
}

func (f *filaFalsa) CorridaAtual() (rastreio.Corrida, bool) {
	if f.rodando == nil {
		return rastreio.Corrida{}, false
	}
	return *f.rodando, true
}
func (f *filaFalsa) UltimaParada() (rastreio.Parada, bool) {
	if f.parada == nil {
		return rastreio.Parada{}, false
	}
	return *f.parada, true
}
func (f *filaFalsa) PausaAgora() (rastreio.Corrida, error) {
	if f.rodando == nil {
		return rastreio.Corrida{}, rastreio.ErrNadaRodando
	}
	c := *f.rodando
	f.pausadas++
	f.parada = &rastreio.Parada{Tipo: c.Tipo, ID: c.ID, GameID: c.GameID}
	f.rodando = nil
	c.Pausando = true
	return c, nil
}
func (f *filaFalsa) RetomaParada(ctx context.Context) (rastreio.Parada, string, error) {
	p := *f.parada
	f.retomou++
	f.parada = nil
	f.rodando = &rastreio.Corrida{Tipo: p.Tipo, ID: p.ID, GameID: p.GameID}
	return p, "ex-fila", nil
}
func (f *filaFalsa) EsqueceParada() { f.parada = nil }

type coletasFalsas struct {
	atual     int64
	estado    map[int64]string
	donaDe    map[int64]int64
	pausouJa  []int64
	retomadas []int64
}

func (c *coletasFalsas) ColetaAtual() (int64, string, bool) {
	return c.atual, "ex-coleta", c.atual != 0
}
func (c *coletasFalsas) PausaAgora(id int64) error {
	c.pausouJa = append(c.pausouJa, id)
	return nil
}
func (c *coletasFalsas) Retoma(ctx context.Context, id int64) (string, error) {
	c.retomadas = append(c.retomadas, id)
	return "ex-coleta-2", nil
}
func (c *coletasFalsas) ColetaDoRastreio(rid int64) (int64, bool) {
	id, ok := c.donaDe[rid]
	return id, ok
}
func (c *coletasFalsas) EstadoDaColeta(id int64) (string, string) { return c.estado[id], "" }

func pede(t *testing.T, mux *http.ServeMux, metodo, caminho string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(metodo, caminho, nil))
	var m map[string]any
	json.Unmarshal(rec.Body.Bytes(), &m)
	return rec.Code, m
}

func TestRastreioPausaERetomaPeloAtalho(t *testing.T) {
	f := &filaFalsa{rodando: &rastreio.Corrida{Tipo: rastreio.TipoRastreio, ID: 9, GameID: 3305486988}}
	s := &Servico{Fila: f, Coletas: &coletasFalsas{}}
	mux := http.NewServeMux()
	Registra(mux, s)

	code, e := pede(t, mux, "GET", "/api/captura")
	if code != 200 || e["rodando"] == nil || e["pausada"] != nil {
		t.Fatalf("estado %d %v", code, e)
	}
	// O atalho: alterna pausa o que roda...
	code, r := pede(t, mux, "POST", "/api/captura/alternar")
	if code != 200 || r["acao"] != "pausou" || f.pausadas != 1 {
		t.Fatalf("alternar 1: %d %v", code, r)
	}
	if _, e = pede(t, mux, "GET", "/api/captura"); e["rodando"] != nil || e["pausada"].(map[string]any)["id"].(float64) != 9 {
		t.Fatalf("depois da pausa %v", e)
	}
	// ...e de novo retoma a última pausada.
	code, r = pede(t, mux, "POST", "/api/captura/alternar")
	if code != 200 || r["acao"] != "retomou" || f.retomou != 1 || r["execucao"].(map[string]any)["id"] != "ex-fila" {
		t.Fatalf("alternar 2: %d %v", code, r)
	}
	// Retomar com algo rodando, ou pausar com nada: 409.
	if code, _ = pede(t, mux, "POST", "/api/captura/retomar"); code != 409 {
		t.Fatalf("retomar rodando: %d", code)
	}
	f.rodando = nil
	if code, _ = pede(t, mux, "POST", "/api/captura/pausar"); code != 409 {
		t.Fatalf("pausar nada: %d", code)
	}
	if code, _ = pede(t, mux, "POST", "/api/captura/retomar"); code != 409 {
		t.Fatalf("retomar sem parada: %d", code)
	}
}

// Coleta rodando: pausar pausa a coleta (agora); a parada que a fila anota é
// o rastreio da coleta, mas "retomar" retoma a COLETA (senão ela ficaria
// parada com o rastreio terminado por fora).
func TestColetaPausaERetomaAColeta(t *testing.T) {
	f := &filaFalsa{rodando: &rastreio.Corrida{Tipo: rastreio.TipoRastreio, ID: 30, GameID: 77}}
	c := &coletasFalsas{atual: 5, estado: map[int64]string{5: "rodando"}, donaDe: map[int64]int64{30: 5}}
	s := &Servico{Fila: f, Coletas: c}
	e := s.Le()
	if e.Rodando == nil || e.Rodando.Tipo != TipoColeta || e.Rodando.ID != 5 || e.Rodando.GameID != 77 {
		t.Fatalf("estado %+v", e.Rodando)
	}
	cp, err := s.Pausa()
	if err != nil || cp.Tipo != TipoColeta || len(c.pausouJa) != 1 || f.pausadas != 0 {
		t.Fatalf("pausa %+v %v", cp, err)
	}
	// A coleta parou: a fila anotou o rastreio 30.
	c.atual, c.estado[5] = 0, "pausado"
	f.rodando, f.parada = nil, &rastreio.Parada{Tipo: rastreio.TipoRastreio, ID: 30, GameID: 77}
	if e = s.Le(); e.Pausada == nil || e.Pausada.Tipo != TipoColeta || e.Pausada.ID != 5 {
		t.Fatalf("pausada %+v", e.Pausada)
	}
	acao, cp, ex, err := s.Alterna(context.Background())
	if err != nil || acao != "retomou" || ex != "ex-coleta-2" || len(c.retomadas) != 1 || f.retomou != 0 || f.parada != nil {
		t.Fatalf("retomada %s %+v %s %v", acao, cp, ex, err)
	}
}
