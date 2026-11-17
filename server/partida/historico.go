package partida

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"

	"loltracking/server/lcu"
)

// Pagina é uma página da lista de partidas de um jogador.
type Pagina struct {
	Partidas   []Resumo `json:"partidas"`
	Inicio     int      `json:"inicio"`
	Quantidade int      `json:"quantidade"`
	TemMais    bool     `json:"tem_mais"`
	// Fonte: "cliente" (lista viva do LCU) ou "banco" (cliente fechado: o que
	// já estava guardado, com o último estado de replay visto).
	Fonte string `json:"fonte"`
	Aviso string `json:"aviso,omitempty"`
}

// MaxPorPagina limita a página: cada partida nova custa um games/{id} e uma
// consulta de replay ao cliente.
const MaxPorPagina = 50

// paralelo é quantos pedidos ao LCU correm juntos ao montar a página.
const paralelo = 4

// ListaDoJogador monta a página de partidas do jogador.
//
// O histórico do LCU (16.22) IGNORA begIndex/endIndex e devolve tudo o que tem
// (~20 partidas). O app pede o intervalo certo mesmo assim (se um dia o LCU
// respeitar, a resposta já vem cortada) e corta do lado de cá.
//
// Para cada partida da página: grava a partida inteira (games/{id}, os 10) se
// ainda não estava completa, consulta o estado do replay (apresentando a
// partida ao plugin quando preciso) e, se o replay já está baixado, guarda a
// linha do tempo que faltar.
func ListaDoJogador(ctx context.Context, d Deps, puuid string, inicio, quantidade int) (Pagina, error) {
	if inicio < 0 || quantidade < 1 || quantidade > MaxPorPagina {
		return Pagina{}, &ErroInvalido{fmt.Sprintf("inicio precisa ser ≥ 0 e quantidade de 1 a %d", MaxPorPagina)}
	}
	pag := Pagina{Inicio: inicio, Quantidade: quantidade, Fonte: "cliente", Partidas: []Resumo{}}

	bruto, err := d.LCU.Historico(ctx, puuid, inicio, inicio+quantidade+1)
	if errors.Is(err, lcu.ErrClienteFechado) {
		return doBanco(d, puuid, inicio, quantidade)
	}
	if err != nil {
		return Pagina{}, err
	}
	var h struct {
		Games struct {
			GameIndexBegin int               `json:"gameIndexBegin"`
			Games          []json.RawMessage `json:"games"`
		} `json:"games"`
	}
	if err := json.Unmarshal(bruto, &h); err != nil {
		return Pagina{}, fmt.Errorf("histórico do LCU em formato inesperado: %w", err)
	}
	jogos := h.Games.Games
	desloc := 0
	if inicio > 0 && h.Games.GameIndexBegin == inicio {
		desloc = inicio // o LCU respeitou o intervalo
	}
	de := inicio - desloc
	pag.TemMais = len(jogos) > de+quantidade
	if de >= len(jogos) {
		return pag, nil
	}
	pagina := jogos[de:min(de+quantidade, len(jogos))]

	cfg, err := d.LCU.ConfiguracaoReplays(ctx)
	if err != nil {
		return Pagina{}, err
	}

	ids := make([]int64, len(pagina))
	erros := make([]error, len(pagina))
	sem := make(chan struct{}, paralelo)
	var wg sync.WaitGroup
	for i, item := range pagina {
		j, err := leJogo(item)
		if err != nil {
			erros[i] = err
			continue
		}
		ids[i] = j.GameID
		wg.Add(1)
		go func(i int, item json.RawMessage, id int64) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			erros[i] = preparaPartida(ctx, d, item, id, cfg.VersaoJogo)
		}(i, item, j.GameID)
	}
	wg.Wait()
	for i, id := range ids {
		if erros[i] != nil {
			// Cliente fechado no meio da página vale para a página inteira; outro
			// erro (uma partida que o LCU recusou) só fica no log.
			if errors.Is(erros[i], lcu.ErrClienteFechado) {
				return Pagina{}, erros[i]
			}
			log.Printf("partida: preparar %d: %v", id, erros[i])
		}
		if id == 0 {
			continue
		}
		if r, err := resumoDe(d.DB, id, puuid); err == nil {
			pag.Partidas = append(pag.Partidas, r)
		}
	}
	return pag, nil
}

// preparaPartida grava a partida inteira (ou, se games/{id} falhar, o item do
// histórico) e atualiza o estado do replay.
func preparaPartida(ctx context.Context, d Deps, item json.RawMessage, id int64, versaoCliente string) error {
	if !estaCompleta(d.DB, id) {
		inteira, err := d.LCU.Partida(ctx, id)
		if err == nil {
			err = Grava(ctx, d, inteira, true)
		}
		if err != nil {
			if errors.Is(err, lcu.ErrClienteFechado) {
				return err
			}
			log.Printf("partida: games/%d indisponível, guardando só o item do histórico: %v", id, err)
			if err := Grava(ctx, d, item, false); err != nil {
				return err
			}
		}
	}
	r, err := ConsultaReplay(ctx, d, id, versaoCliente)
	if err != nil {
		if !errors.Is(err, lcu.ErrClienteFechado) {
			GravaReplay(d.DB, id, Replay{Estado: Indisponivel})
		}
		return err
	}
	if err := GravaReplay(d.DB, id, r); err != nil {
		return err
	}
	if r.Estado == Baixado {
		if err := guardaLinhaDoTempo(ctx, d, id); err != nil {
			log.Printf("partida: linha do tempo de %d: %v", id, err)
		}
	}
	return nil
}

// doBanco é a lista quando o cliente do LoL está fechado.
func doBanco(d Deps, puuid string, inicio, quantidade int) (Pagina, error) {
	pag := Pagina{Inicio: inicio, Quantidade: quantidade, Fonte: "banco", Partidas: []Resumo{},
		Aviso: "o cliente do LoL está fechado: mostrando as partidas já guardadas, com o último estado de replay visto"}
	ids, err := partidasNoBanco(d.DB, puuid)
	if err != nil {
		return pag, err
	}
	if inicio >= len(ids) {
		return pag, nil
	}
	ate := min(inicio+quantidade, len(ids))
	pag.TemMais = len(ids) > ate
	for _, id := range ids[inicio:ate] {
		if r, err := resumoDe(d.DB, id, puuid); err == nil {
			pag.Partidas = append(pag.Partidas, r)
		}
	}
	return pag, nil
}

// LePartida devolve a partida inteira, buscando no LCU se ainda não está
// completa no banco. Com o cliente fechado, serve o que houver no banco.
func LePartida(ctx context.Context, d Deps, gameID int64) (Partida, error) {
	if err := garantePartida(ctx, d, gameID); err != nil && !errors.Is(err, lcu.ErrClienteFechado) {
		return Partida{}, err
	}
	return Le(d.DB, gameID)
}
