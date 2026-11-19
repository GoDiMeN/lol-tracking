// Package riot cuida da chave opcional da API da Riot (token.txt).
//
// Hoje o app não depende dela para nada: tudo vem do cliente do LoL. O pacote
// só diz, em GET /api/estado, se há chave e se a Riot a aceita, para a tela
// avisar. A chave nunca vai para log, resposta HTTP ou mensagem de erro.
package riot

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// BaseRiot é o servidor da plataforma BR na API da Riot.
const BaseRiot = "https://br1.api.riotgames.com"

// Estado é o que a tela recebe sobre a chave.
type Estado struct {
	Configurada bool `json:"configurada"`
	// Valida: true/false quando a Riot respondeu; null quando não deu para
	// saber (sem chave, sem rede, modo teste).
	Valida       *bool  `json:"valida"`
	Detalhe      string `json:"detalhe,omitempty"`
	VerificadaEm string `json:"verificada_em,omitempty"`
}

// Cliente lê a chave do arquivo e confere com a Riot, com cache.
type Cliente struct {
	arquivo   string
	modoTeste bool
	base      string
	http      *http.Client

	baseRegional string
	ritmo        sync.Mutex
	ultimoPedido time.Time
	// lim: o limite por host dos pedidos do aprendizado (liga.go).
	lim *limitador
	// redireciona: no teste, todo host da tabela de regiões vira este.
	redireciona string

	mu     sync.Mutex
	quando time.Time
	ultimo Estado
	// conferida: o resumo (sha256) da chave que deu o resultado guardado. Se o
	// token.txt muda, o resultado guardado não vale para a chave nova.
	conferida [32]byte
}

// Validade do resultado: a chave de desenvolvimento vence em 24 h, e conferir a
// cada abertura de tela gastaria a cota dela à toa.
const validade = 10 * time.Minute

// Novo cria o cliente. No modo teste nada sai para a rede.
func Novo(arquivo string, modoTeste bool) *Cliente {
	return &Cliente{arquivo: arquivo, modoTeste: modoTeste, base: BaseRiot, http: &http.Client{Timeout: 8 * time.Second}}
}

// chave lê o token.txt. Lido a cada conferência: a pessoa troca a chave
// vencida sem reiniciar o app.
func (c *Cliente) chave() string {
	b, err := os.ReadFile(c.arquivo)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// Estado devolve se há chave e se ela vale, conferindo no máximo a cada 10 min.
// Chave trocada no token.txt é conferida de novo na hora (o resultado guardado
// era da chave velha).
func (c *Cliente) Estado(ctx context.Context) Estado {
	c.mu.Lock()
	defer c.mu.Unlock()
	chave := c.chave()
	if chave == "" {
		return Estado{Detalhe: "sem chave da Riot (token.txt); o app funciona sem ela"}
	}
	if c.modoTeste {
		return Estado{Configurada: true, Detalhe: "modo teste: a chave não é conferida"}
	}
	resumo := sha256.Sum256([]byte(chave))
	if !c.quando.IsZero() && time.Since(c.quando) < validade && resumo == c.conferida {
		return c.ultimo
	}
	e := c.confere(ctx, chave)
	e.Configurada = true
	e.VerificadaEm = time.Now().UTC().Format("2006-01-02T15:04:05Z")
	c.quando, c.ultimo, c.conferida = time.Now(), e, resumo
	return e
}

func (c *Cliente) confere(ctx context.Context, chave string) Estado {
	ctx, cancela := context.WithTimeout(ctx, 8*time.Second)
	defer cancela()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/lol/status/v4/platform-data", nil)
	if err != nil {
		return Estado{Detalhe: "não foi possível montar a conferência da chave"}
	}
	req.Header.Set("X-Riot-Token", chave)
	resp, err := c.http.Do(req)
	if err != nil {
		// O erro do http.Client não contém cabeçalhos, mas a frase é nossa de
		// propósito: nada do pedido vai para a tela.
		var semRede interface{ Timeout() bool }
		if errors.As(err, &semRede) && semRede.Timeout() {
			return Estado{Detalhe: "a API da Riot não respondeu a tempo"}
		}
		return Estado{Detalhe: "sem conexão com a API da Riot"}
	}
	defer resp.Body.Close()
	sim, nao := true, false
	switch resp.StatusCode {
	case http.StatusOK:
		return Estado{Valida: &sim}
	case http.StatusUnauthorized, http.StatusForbidden:
		return Estado{Valida: &nao, Detalhe: fmt.Sprintf("a Riot recusou a chave (%d); chave de desenvolvimento vence em 24 h", resp.StatusCode)}
	case http.StatusTooManyRequests:
		return Estado{Valida: &sim, Detalhe: "chave aceita, mas no limite de pedidos agora"}
	}
	return Estado{Detalhe: fmt.Sprintf("a API da Riot respondeu %d", resp.StatusCode)}
}
