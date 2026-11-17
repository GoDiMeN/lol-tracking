// Package web guarda o que todo pacote precisa para falar HTTP: escrever JSON,
// escrever erro sempre no mesmo formato, ler corpo, envelopar listas, servir o
// renderer e o hub de execuções (execucao.go). Fica num pacote próprio porque
// os pacotes de função são irmãos e não importam o main.
package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// WriteJSON escreve v como JSON com o status dado.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	// Erro aqui é cliente que desconectou: o status já saiu, não há o que fazer.
	_ = json.NewEncoder(w).Encode(v)
}

// WriteErr é o único jeito de responder erro: {"erro": msg}. A mensagem é para
// a pessoa; o detalhe técnico vai para o log de quem chamou.
func WriteErr(w http.ResponseWriter, status int, msg string) {
	WriteJSON(w, status, map[string]string{"erro": msg})
}

// NuncaNil troca a fatia nil por uma vazia: o JSON sai [] e não null, e o
// renderer não precisa se defender em cada lista.
func NuncaNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// WriteLista é o envelope de listas do contrato: {"<chave>": [...], ...extras}.
// Lista nunca vai crua na raiz: assim dá para acrescentar paginação depois sem
// quebrar quem lê.
func WriteLista[T any](w http.ResponseWriter, chave string, itens []T, extras map[string]any) {
	corpo := map[string]any{chave: NuncaNil(itens)}
	for k, v := range extras {
		corpo[k] = v
	}
	WriteJSON(w, http.StatusOK, corpo)
}

// LimiteCorpo é o teto do corpo de uma requisição: o app só manda pedidos
// curtos (um nome, um id), e ler sem teto deixaria encher a memória.
const LimiteCorpo = 1 << 20

// ErrCorpoInvalido já é frase para a pessoa: o controller repassa num 400.
var ErrCorpoInvalido = errors.New("o corpo da requisição não é um JSON válido")

// LeJSON decodifica o corpo em v. Corpo vazio não é erro: rota de corpo
// opcional não deve exigir "{}".
func LeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, LimiteCorpo)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		var grande *http.MaxBytesError
		if errors.As(err, &grande) {
			return errors.New("o corpo da requisição é grande demais")
		}
		return ErrCorpoInvalido
	}
	return nil
}

// Static serve o renderer em "/" e responde 404 em JSON para qualquer /api/*
// que ninguém registrou. Sem esse desvio, um fetch para rota digitada errado
// receberia o index.html com 200 e o renderer quebraria lendo HTML como JSON.
func Static(dir string) http.Handler {
	var arquivos http.Handler
	if dir != "" {
		arquivos = http.FileServer(http.Dir(dir))
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
			WriteErr(w, http.StatusNotFound, "rota não encontrada")
			return
		}
		if arquivos == nil {
			WriteErr(w, http.StatusNotFound, "o servidor não está servindo a interface (LOL_RENDERER vazio)")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			WriteErr(w, http.StatusMethodNotAllowed, "método não permitido")
			return
		}
		if path.Clean("/"+r.URL.Path) == "/" {
			if _, err := os.Stat(filepath.Join(dir, "index.html")); err != nil {
				WriteErr(w, http.StatusNotFound, "index.html não encontrado em "+dir)
				return
			}
		}
		// O renderer muda a cada edição e é servido de loopback: cache só
		// atrapalharia quem está mexendo na interface.
		w.Header().Set("Cache-Control", "no-cache")
		arquivos.ServeHTTP(w, r)
	})
}
