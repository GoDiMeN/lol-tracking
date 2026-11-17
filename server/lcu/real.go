package lcu

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Real é o cliente de verdade. Não guarda porta nem senha: o lockfile é lido a
// cada pedido, porque os dois mudam toda vez que o cliente do LoL reinicia, e
// um app aberto o dia inteiro veria o cliente reiniciar.
type Real struct {
	lockfile string
	http     *http.Client
}

// NovoReal cria o cliente que lê o lockfile no caminho dado.
func NovoReal(lockfile string) *Real {
	return &Real{lockfile: lockfile, http: clienteLoopback()}
}

// clienteLoopback é o http.Client que só conecta em 127.0.0.1. O certificado
// do LCU é autoassinado, então a verificação TLS é desligada; isso só é
// aceitável porque o discador recusa qualquer endereço que não seja o
// loopback, e assim a conexão sem verificação nunca sai da máquina. Sem proxy
// pelo mesmo motivo.
func clienteLoopback() *http.Client {
	discador := &net.Dialer{Timeout: 3 * time.Second}
	return &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			Proxy: nil,
			DialContext: func(ctx context.Context, rede, endereco string) (net.Conn, error) {
				host, _, err := net.SplitHostPort(endereco)
				if err != nil || host != "127.0.0.1" {
					return nil, fmt.Errorf("lcu: conexão recusada para %q: o LCU só é chamado em 127.0.0.1", endereco)
				}
				return discador.DialContext(ctx, rede, endereco)
			},
			// #nosec: ver o comentário acima; o discador garante o loopback.
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
			MaxIdleConnsPerHost: 4,
			IdleConnTimeout:     30 * time.Second,
		},
	}
}

// credenciais lê "nome:pid:porta:senha:protocolo" do lockfile.
func (c *Real) credenciais() (porta int, senha string, err error) {
	b, err := os.ReadFile(c.lockfile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, "", ErrClienteFechado
		}
		return 0, "", fmt.Errorf("ler o lockfile do LoL (%s): %w", c.lockfile, err)
	}
	partes := strings.Split(strings.TrimSpace(string(b)), ":")
	if len(partes) < 5 {
		return 0, "", fmt.Errorf("lockfile do LoL em formato inesperado (%d campos)", len(partes))
	}
	porta, err = strconv.Atoi(partes[2])
	if err != nil || porta < 1 || porta > 65535 {
		return 0, "", fmt.Errorf("lockfile do LoL com porta inválida")
	}
	return porta, partes[3], nil
}

// pede é o único caminho de saída para o LCU. A primeira coisa que faz é
// conferir a lista de rotas permitidas: rota fora dela não chega nem a ler o
// lockfile.
func (c *Real) pede(ctx context.Context, metodo, caminho string, query url.Values, corpo any) ([]byte, error) {
	if !Permitida(metodo, caminho) {
		return nil, &ErroRotaProibida{Metodo: metodo, Caminho: caminho}
	}
	porta, senha, err := c.credenciais()
	if err != nil {
		return nil, err
	}
	u := "https://127.0.0.1:" + strconv.Itoa(porta) + caminho
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var leitor io.Reader
	if corpo != nil {
		b, err := json.Marshal(corpo)
		if err != nil {
			return nil, err
		}
		leitor = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, metodo, u, leitor)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth("riot", senha)
	req.Header.Set("Accept", "application/json")
	if corpo != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		// Lockfile que sobrou de um cliente que fechou mal: a porta não atende.
		if errors.Is(err, syscall.ECONNREFUSED) || strings.Contains(err.Error(), "connectex") {
			return nil, ErrClienteFechado
		}
		return nil, fmt.Errorf("falar com o cliente do LoL: %w", err)
	}
	defer resp.Body.Close()
	dados, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, fmt.Errorf("ler a resposta do cliente do LoL: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%w (%s)", ErrNaoEncontrado, mensagemLCU(dados))
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &ErroLCU{Status: resp.StatusCode, Mensagem: mensagemLCU(dados)}
	}
	return dados, nil
}

// mensagemLCU tira o "message" do corpo de erro do LCU.
func mensagemLCU(dados []byte) string {
	var e struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(dados, &e) == nil && e.Message != "" {
		return e.Message
	}
	s := strings.TrimSpace(string(dados))
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

func (c *Real) pedeJSON(ctx context.Context, caminho string, query url.Values, destino any) error {
	b, err := c.pede(ctx, http.MethodGet, caminho, query, nil)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, destino); err != nil {
		return fmt.Errorf("resposta inesperada do cliente do LoL em %s: %w", caminho, err)
	}
	return nil
}

func (c *Real) InvocadorAtual(ctx context.Context) (Invocador, error) {
	var r struct {
		Puuid    string `json:"puuid"`
		GameName string `json:"gameName"`
		TagLine  string `json:"tagLine"`
	}
	if err := c.pedeJSON(ctx, "/lol-summoner/v1/current-summoner", nil, &r); err != nil {
		return Invocador{}, err
	}
	return Invocador{Puuid: r.Puuid, Nome: r.GameName, Tag: r.TagLine}, nil
}

func (c *Real) BuscaAlias(ctx context.Context, nome, tag string) (Invocador, error) {
	var r struct {
		Puuid string `json:"puuid"`
		Alias struct {
			GameName string `json:"gameName"`
			TagLine  string `json:"tagLine"`
		} `json:"alias"`
	}
	q := url.Values{"gameName": {nome}, "tagLine": {tag}}
	if err := c.pedeJSON(ctx, "/lol-summoner/v1/alias/lookup", q, &r); err != nil {
		return Invocador{}, err
	}
	if r.Puuid == "" {
		return Invocador{}, ErrNaoEncontrado
	}
	inv := Invocador{Puuid: r.Puuid, Nome: r.Alias.GameName, Tag: r.Alias.TagLine}
	if inv.Nome == "" {
		inv.Nome, inv.Tag = nome, tag
	}
	return inv, nil
}

func (c *Real) Historico(ctx context.Context, puuid string, inicio, fim int) (json.RawMessage, error) {
	q := url.Values{"begIndex": {strconv.Itoa(inicio)}, "endIndex": {strconv.Itoa(fim)}}
	return c.pede(ctx, http.MethodGet, "/lol-match-history/v1/products/lol/"+puuid+"/matches", q, nil)
}

func (c *Real) Partida(ctx context.Context, gameID int64) (json.RawMessage, error) {
	return c.pede(ctx, http.MethodGet, "/lol-match-history/v1/games/"+strconv.FormatInt(gameID, 10), nil, nil)
}

func (c *Real) LinhaDoTempo(ctx context.Context, gameID int64) (json.RawMessage, error) {
	return c.pede(ctx, http.MethodGet, "/lol-match-history/v1/game-timelines/"+strconv.FormatInt(gameID, 10), nil, nil)
}

func (c *Real) FaseGameflow(ctx context.Context) (string, error) {
	var fase string
	err := c.pedeJSON(ctx, "/lol-gameflow/v1/gameflow-phase", nil, &fase)
	return fase, err
}

func (c *Real) Regiao(ctx context.Context) (Regiao, error) {
	var r Regiao
	err := c.pedeJSON(ctx, "/riotclient/region-locale", nil, &r)
	return r, err
}

func (c *Real) ConfiguracaoReplays(ctx context.Context) (ConfigReplays, error) {
	var r ConfigReplays
	err := c.pedeJSON(ctx, "/lol-replays/v1/configuration", nil, &r)
	return r, err
}

func (c *Real) PastaReplays(ctx context.Context) (string, error) {
	var p string
	err := c.pedeJSON(ctx, "/lol-replays/v1/rofls/path", nil, &p)
	return p, err
}

func (c *Real) MetadataReplay(ctx context.Context, gameID int64) (MetadataReplay, error) {
	var m MetadataReplay
	err := c.pedeJSON(ctx, "/lol-replays/v1/metadata/"+strconv.FormatInt(gameID, 10), nil, &m)
	return m, err
}

func (c *Real) CriaMetadataReplay(ctx context.Context, gameID int64, m CriaMetadata) error {
	_, err := c.pede(ctx, http.MethodPost, "/lol-replays/v2/metadata/"+strconv.FormatInt(gameID, 10)+"/create", nil, m)
	return err
}

// corpoBotao é o que o próprio cliente manda quando a pessoa clica no botão do
// histórico; o plugin exige o componentType.
var corpoBotao = map[string]string{"componentType": "replay-button_match-history"}

func (c *Real) BaixaReplay(ctx context.Context, gameID int64) error {
	_, err := c.pede(ctx, http.MethodPost, "/lol-replays/v1/rofls/"+strconv.FormatInt(gameID, 10)+"/download", nil, corpoBotao)
	return err
}

func (c *Real) AssisteReplay(ctx context.Context, gameID int64) error {
	_, err := c.pede(ctx, http.MethodPost, "/lol-replays/v1/rofls/"+strconv.FormatInt(gameID, 10)+"/watch", nil, corpoBotao)
	return err
}
