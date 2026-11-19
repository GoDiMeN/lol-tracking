// Package ia é a única porta do app para uma IA. O rastreio NÃO usa IA: ela
// só entra quando a pessoa pede, nas telas de análise e de conversa, e só lê
// o texto que o app monta a partir da base de fatos (analise/fatos*.go).
//
// A IA fica atrás da interface Cliente. Hoje há duas implementações:
//   - Terminal: o Claude Code instalado na máquina, em modo não interativo
//     (`claude -p`), sem ferramenta nenhuma (terminal.go);
//   - Falso: determinístico, para os testes e para LOL_TESTE=1 (falso.go).
//
// O lugar de uma implementação "api" (a API da Anthropic direto, com chave) é
// ao lado delas: basta outro tipo que cumpra Cliente e uma linha no main. Não
// existe ainda de propósito: nada de chave de API neste projeto por enquanto.
package ia

import (
	"context"
	"errors"
)

// Papel de uma mensagem na conversa mandada à IA.
const (
	PapelUsuario    = "usuario"
	PapelAssistente = "assistente"
)

// Tipos de pedido: escolhem o modelo (a análise usa o mais capaz; a conversa,
// um mais rápido) e o tempo limite.
const (
	TipoAnalise  = "analise"
	TipoConversa = "conversa"
)

// Mensagem é uma fala da conversa. O texto é o que vai para a IA, inteiro.
type Mensagem struct {
	Papel string
	Texto string
}

// Pedido é uma chamada: o prompt de sistema e a conversa até aqui (a última
// mensagem é a do usuário). Cada chamada é independente: nada fica guardado
// do lado da IA, o histórico vai inteiro a cada vez.
type Pedido struct {
	Tipo      string
	Sistema   string
	Mensagens []Mensagem
}

// Resposta é o texto completo devolvido e o que a chamada custou.
type Resposta struct {
	Texto  string `json:"-"`
	Modelo string `json:"modelo"`
	// DuracaoMs: do início do processo até a última linha (relógio de parede).
	DuracaoMs int64 `json:"duracao_ms"`
	// Caracteres: o que saiu da máquina (sistema + mensagens) e o que voltou.
	CaracteresEntrada int `json:"caracteres_entrada"`
	CaracteresSaida   int `json:"caracteres_saida"`
	// Tokens e custo: como o Claude Code informou (0 quando não informa; o
	// custo é a estimativa dele em dólar, não a cobrança da assinatura).
	TokensEntrada int     `json:"tokens_entrada"`
	TokensSaida   int     `json:"tokens_saida"`
	CustoUSD      float64 `json:"custo_usd"`
}

// Estado responde GET /api/ia/estado.
type Estado struct {
	Disponivel    bool   `json:"disponivel"`
	Implementacao string `json:"implementacao"` // terminal, falso
	Versao        string `json:"versao"`
	// Motivo: a frase para a pessoa quando não está disponível.
	Motivo  string            `json:"motivo"`
	Modelos map[string]string `json:"modelos"` // tipo de pedido → modelo
}

// Cliente é a IA. Conversa chama aoTrecho com cada pedaço do texto assim que
// ele chega (fluxo) e devolve a resposta completa no fim. O contexto cancela
// a chamada (o processo do terminal é encerrado).
type Cliente interface {
	Estado(ctx context.Context) Estado
	Conversa(ctx context.Context, p Pedido, aoTrecho func(string)) (Resposta, error)
}

// ErrIndisponivel: a IA não pode ser chamada (Claude Code ausente ou sem
// login). A mensagem que acompanha já é frase para a pessoa.
var ErrIndisponivel = errors.New("a IA não está disponível")

// erroIA carrega a frase para a pessoa e a causa técnica (que vai para o log).
type erroIA struct {
	frase string
	causa error
}

func (e *erroIA) Error() string { return e.frase }
func (e *erroIA) Unwrap() error { return e.causa }

// caracteresDoPedido conta o que sai da máquina numa chamada.
func caracteresDoPedido(p Pedido) int {
	n := len([]rune(p.Sistema))
	for _, m := range p.Mensagens {
		n += len([]rune(m.Texto))
	}
	return n
}
