package riot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// BaseRegional é o servidor regional da match-v5 (o BR fica nas Américas).
const BaseRegional = "https://americas.api.riotgames.com"

// ErrSemChave: não há chave (ou é o modo teste). Quem chama pula em silêncio.
var ErrSemChave = errors.New("sem chave da Riot")

// ErrChaveRecusada: a Riot recusou a chave (vencida: dura 24 h).
var ErrChaveRecusada = errors.New("a Riot recusou a chave")

// Intervalo mínimo entre pedidos à Riot: o limite da chave de desenvolvimento
// é 20/s e 100 a cada 2 min; um pedido por passe fica muito abaixo, e o
// intervalo garante isso mesmo se vários passes fecharem juntos.
var IntervaloPedidos = 1500 * time.Millisecond

// LinhaDoTempo devolve o JSON cru de match-v5 .../BR1_{gameId}/timeline (com
// championStats por jogador e por minuto). A chave não aparece em erro nenhum.
func (c *Cliente) LinhaDoTempo(ctx context.Context, gameID int64) ([]byte, error) {
	chave := c.chave()
	if chave == "" || c.modoTeste {
		return nil, ErrSemChave
	}
	base := c.baseRegional
	if base == "" {
		base = BaseRegional
	}
	for tentativa := 0; tentativa < 2; tentativa++ {
		c.ritmo.Lock()
		if espera := IntervaloPedidos - time.Since(c.ultimoPedido); espera > 0 {
			time.Sleep(espera)
		}
		c.ultimoPedido = time.Now()
		c.ritmo.Unlock()
		ctx2, cancela := context.WithTimeout(ctx, 20*time.Second)
		req, err := http.NewRequestWithContext(ctx2, http.MethodGet, fmt.Sprintf("%s/lol/match/v5/matches/BR1_%d/timeline", base, gameID), nil)
		if err != nil {
			cancela()
			return nil, err
		}
		req.Header.Set("X-Riot-Token", chave)
		resp, err := c.http.Do(req)
		if err != nil {
			cancela()
			return nil, errors.New("sem conexão com a API da Riot")
		}
		corpo, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		resp.Body.Close()
		cancela()
		switch {
		case resp.StatusCode == http.StatusOK && err == nil:
			return corpo, nil
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			return nil, ErrChaveRecusada
		case resp.StatusCode == http.StatusTooManyRequests && tentativa == 0:
			s, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
			select {
			case <-time.After(time.Duration(max(1, min(s, 30))) * time.Second):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			continue
		default:
			return nil, fmt.Errorf("a API da Riot respondeu %d à linha do tempo", resp.StatusCode)
		}
	}
	return nil, errors.New("a API da Riot ficou no limite de pedidos")
}
