package riot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// O aprendizado por elo descobre os melhores jogadores de cada elo pela API da
// Riot (league-v4 e account-v1). O ranking não vem do LCU: a lista de rotas
// permitidas do LCU não tem rota de ranking, de propósito.

// ErrNaoEncontradoRiot é o 404 da Riot (conta que não existe mais, liga vazia).
var ErrNaoEncontradoRiot = errors.New("a API da Riot não encontrou o que foi pedido")

// EntradaLiga é um jogador numa liga (fila ranqueada solo).
type EntradaLiga struct {
	Puuid string `json:"puuid"`
	// SummonerID: a Riot está tirando o campo; fica só para conferência.
	SummonerID string `json:"summonerId"`
	Tier       string `json:"tier"`
	Rank       string `json:"rank"`
	LP         int    `json:"leaguePoints"`
	Vitorias   int    `json:"wins"`
	Derrotas   int    `json:"losses"`
	// Sinais da liga: FreshBlood = entrou há pouco nela; Veteran = muitas
	// partidas na mesma liga (travado); HotStreak = sequência de vitórias;
	// Inactive = parado (perdendo LP por inatividade).
	FreshBlood bool `json:"freshBlood"`
	Veteran    bool `json:"veteran"`
	HotStreak  bool `json:"hotStreak"`
	Inactive   bool `json:"inactive"`
}

// Divisoes são as divisões abaixo de Mestre, da mais alta para a mais baixa.
var Divisoes = []string{"I", "II", "III", "IV"}

// Conta é o Riot ID atual de um puuid (account-v1).
type Conta struct {
	Puuid string `json:"puuid"`
	Nome  string `json:"gameName"`
	Tag   string `json:"tagLine"`
}

// FilaSolo é a fila ranqueada solo/duo na league-v4.
const FilaSolo = "RANKED_SOLO_5x5"

// ligasApex: elo → caminho da liga inteira (Mestre para cima não tem divisão).
var ligasApex = map[string]string{
	"MASTER":      "masterleagues",
	"GRANDMASTER": "grandmasterleagues",
	"CHALLENGER":  "challengerleagues",
}

// EloApex diz se o elo é de liga única (Mestre, Grão-Mestre, Desafiante).
func EloApex(elo string) bool {
	_, ok := ligasApex[elo]
	return ok
}

// Liga devolve os jogadores de um elo na fila solo. Mestre para cima: a liga
// inteira (divisao e pagina são ignoradas). Abaixo: uma página (1, 2...; a
// Riot devolve ~205 por página, sem ordem garantida) da divisão (I a IV).
func (c *Cliente) Liga(ctx context.Context, reg Regiao, elo, divisao string, pagina int) ([]EntradaLiga, error) {
	if caminho, ok := ligasApex[elo]; ok {
		var liga struct {
			Tier    string        `json:"tier"`
			Entries []EntradaLiga `json:"entries"`
		}
		if err := c.pedeJSON(ctx, c.host(reg.Plataforma), "/lol/league/v4/"+caminho+"/by-queue/"+FilaSolo, &liga); err != nil {
			return nil, err
		}
		for i := range liga.Entries {
			if liga.Entries[i].Tier == "" {
				liga.Entries[i].Tier = elo
			}
		}
		return liga.Entries, nil
	}
	if pagina < 1 {
		pagina = 1
	}
	valida := false
	for _, d := range Divisoes {
		valida = valida || d == divisao
	}
	if !valida {
		return nil, fmt.Errorf("divisão inválida: %q", divisao)
	}
	var l []EntradaLiga
	err := c.pedeJSON(ctx, c.host(reg.Plataforma),
		fmt.Sprintf("/lol/league/v4/entries/%s/%s/%s?page=%d", FilaSolo, url.PathEscape(elo), divisao, pagina), &l)
	return l, err
}

// ContaPorPuuid devolve o Riot ID (nome e tag) de um puuid da Riot.
func (c *Cliente) ContaPorPuuid(ctx context.Context, reg Regiao, puuid string) (Conta, error) {
	var ct Conta
	err := c.pedeJSON(ctx, c.host(reg.Roteamento), "/riot/account/v1/accounts/by-puuid/"+url.PathEscape(puuid), &ct)
	return ct, err
}

// Confere diz se dá para usar a chave agora: nil, ErrSemChave (sem
// token.txt ou modo teste) ou ErrChaveRecusada (vencida). Sem rede: erro com
// frase para a pessoa.
func (c *Cliente) Confere(ctx context.Context) error {
	if c.chave() == "" || c.modoTeste {
		return ErrSemChave
	}
	e := c.Estado(ctx)
	switch {
	case e.Valida == nil:
		return errors.New(e.Detalhe)
	case !*e.Valida:
		return ErrChaveRecusada
	}
	return nil
}

// host troca o host da tabela pelo do teste (httptest), se houver.
func (c *Cliente) host(h string) string {
	if c.redireciona != "" {
		return c.redireciona
	}
	return h
}

// pedeJSON faz um GET na Riot respeitando o limite e decodifica a resposta.
// A chave vai só no cabeçalho; nenhuma mensagem de erro a contém.
func (c *Cliente) pedeJSON(ctx context.Context, host, caminho string, destino any) error {
	chave := c.chave()
	if chave == "" || c.modoTeste {
		return ErrSemChave
	}
	for tentativa := 0; tentativa < 4; tentativa++ {
		if err := c.limite().espera(ctx, host); err != nil {
			return err
		}
		ctx2, cancela := context.WithTimeout(ctx, 20*time.Second)
		req, err := http.NewRequestWithContext(ctx2, http.MethodGet, host+caminho, nil)
		if err != nil {
			cancela()
			return err
		}
		req.Header.Set("X-Riot-Token", chave)
		resp, err := c.http.Do(req)
		if err != nil {
			cancela()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return errors.New("sem conexão com a API da Riot")
		}
		corpo, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		resp.Body.Close()
		cancela()
		switch {
		case resp.StatusCode == http.StatusOK && err == nil:
			if err := json.Unmarshal(corpo, destino); err != nil {
				return fmt.Errorf("a API da Riot respondeu em formato inesperado (%s): %w", rotaSemID(caminho), err)
			}
			return nil
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			return ErrChaveRecusada
		case resp.StatusCode == http.StatusNotFound:
			return ErrNaoEncontradoRiot
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable:
			// Retry-After manda: a Riot conta o estouro contra a chave.
			s, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
			c.limite().bloqueia(host, time.Duration(max(1, min(s, 120)))*time.Second)
			continue
		default:
			return fmt.Errorf("a API da Riot respondeu %d (%s)", resp.StatusCode, rotaSemID(caminho))
		}
	}
	return errors.New("a API da Riot ficou no limite de pedidos; tente de novo em alguns minutos")
}

// rotaSemID tira o puuid e a query do caminho para a mensagem de erro.
func rotaSemID(caminho string) string {
	if i := strings.Index(caminho, "?"); i >= 0 {
		caminho = caminho[:i]
	}
	if strings.Contains(caminho, "/by-puuid/") {
		return caminho[:strings.Index(caminho, "/by-puuid/")+len("/by-puuid")]
	}
	return caminho
}

// --- Limite de pedidos -----------------------------------------------------------

// LimitesPedidos são os da chave de desenvolvimento, por host (cada plataforma e
// cada roteamento regional contam à parte): 20 por segundo e 100 a cada 2 min.
// Variável só para o teste encurtar.
var LimitesPedidos = []Limite{{N: 20, Janela: time.Second}, {N: 100, Janela: 2 * time.Minute}}

// Limite é "no máximo N pedidos por janela".
type Limite struct {
	N      int
	Janela time.Duration
}

type limitador struct {
	mu        sync.Mutex
	pedidos   map[string][]time.Time
	bloqueado map[string]time.Time
}

func (c *Cliente) limite() *limitador {
	c.ritmo.Lock()
	defer c.ritmo.Unlock()
	if c.lim == nil {
		c.lim = &limitador{pedidos: map[string][]time.Time{}, bloqueado: map[string]time.Time{}}
	}
	return c.lim
}

// espera dorme até o próximo pedido caber nos limites (ou o contexto acabar)
// e o registra.
func (l *limitador) espera(ctx context.Context, host string) error {
	for {
		l.mu.Lock()
		agora := time.Now()
		var ate time.Time
		if b := l.bloqueado[host]; b.After(agora) {
			ate = b
		}
		maiorJanela := time.Duration(0)
		for _, lim := range LimitesPedidos {
			maiorJanela = max(maiorJanela, lim.Janela)
		}
		ps := l.pedidos[host]
		for len(ps) > 0 && agora.Sub(ps[0]) >= maiorJanela {
			ps = ps[1:]
		}
		for _, lim := range LimitesPedidos {
			n := 0
			for i := len(ps) - 1; i >= 0 && agora.Sub(ps[i]) < lim.Janela; i-- {
				n++
			}
			if n >= lim.N {
				// O mais velho dentro da janela libera a vaga quando sair dela.
				libera := ps[len(ps)-n].Add(lim.Janela)
				if libera.After(ate) {
					ate = libera
				}
			}
		}
		if ate.IsZero() {
			l.pedidos[host] = append(ps, agora)
			l.mu.Unlock()
			return nil
		}
		l.pedidos[host] = ps
		l.mu.Unlock()
		select {
		case <-time.After(time.Until(ate) + time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (l *limitador) bloqueia(host string, d time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if ate := time.Now().Add(d); ate.After(l.bloqueado[host]) {
		l.bloqueado[host] = ate
	}
}
