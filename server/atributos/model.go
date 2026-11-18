// Package atributos explica os atributos lidos da tela: quanto vem de cada
// dragão (medido no próprio dado, objetivos.go), os dados estáticos da versão
// da partida que o app usa para decompor o resto (base do campeão por nível,
// itens) e a build de cada jogador ao longo do tempo. Nada aqui grava: tudo é
// calculado na hora pelo que já está guardado. Rotas em contrato-http.md.
package atributos

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"loltracking/server/banco"
	"loltracking/server/campeao"
	"loltracking/server/partida"
	"loltracking/server/rastreio"
)

// Deps é o que o pacote precisa.
type Deps struct {
	DB        *banco.DB
	Estaticos campeao.FonteEstaticos
}

// ErrSemLinhaDoTempo: sem a linha do tempo não se sabe quando os dragões caíram.
var ErrSemLinhaDoTempo = errors.New("a partida ainda não tem a linha do tempo guardada; abra a partida com o cliente do LoL aberto")

// RespostaObjetivos é a resposta de GET .../bonus-objetivos.
type RespostaObjetivos struct {
	GameID int64    `json:"game_id"`
	Patch  string   `json:"patch"`
	Quedas []Queda  `json:"quedas"`
	Resumo []Resumo `json:"resumo"`
	Regras []string `json:"regras"`
}

// RegrasMedicao vão junto na resposta: quem lê o número sabe como saiu.
var RegrasMedicao = []string{
	"antes: o último trecho firme (mesmo valor por ≥ 1,5 s e ≥ 4 leituras) até 15 s antes da queda",
	"depois: o primeiro trecho firme acima do valor de antes e até 3 vezes o bônus da tabela, até 20 s depois (90 s para a velocidade de movimento, que só vale fora de luta)",
	"compra ou venda de item corta a janela; nível novo também (menos na velocidade de movimento); morto fica de fora",
	"por acúmulo: fração do total por dragão, contando os dragões do mesmo tipo que o time já tinha; velocidade de movimento sem o teto suave do jogo",
	"faixa: o que o arredondamento da tela (±0,5) permite; o resumo usa o cruzamento das faixas dos jogadores, ou a mediana se não se cruzam",
}

type dadosPartida struct {
	p        partida.Partida
	lt       rastreio.LinhaDoTempo
	amostras map[int][]rastreio.Amostra
	estados  map[int][]rastreio.EstadoJogador
}

func carrega(db *banco.DB, gameID int64, comAmostras bool) (dadosPartida, error) {
	var d dadosPartida
	p, err := partida.Le(db, gameID)
	if err != nil {
		return d, err
	}
	d.p = p
	bruto, err := partida.LeLinhaDoTempo(db, gameID)
	if errors.Is(err, partida.ErrNaoEncontrada) {
		return d, ErrSemLinhaDoTempo
	}
	if err != nil {
		return d, err
	}
	if d.lt, err = rastreio.LeLinhaDoTempo(bruto); err != nil {
		return d, err
	}
	if d.estados, err = rastreio.EstadosDaPartida(db, gameID); err != nil {
		return d, err
	}
	d.amostras = map[int][]rastreio.Amostra{}
	if comAmostras {
		for _, pt := range p.Participantes {
			ps, ok, err := rastreio.MelhorPasse(db, gameID, pt.ParticipanteID)
			if err != nil {
				return d, fmt.Errorf("ler o passe do participante %d: %w", pt.ParticipanteID, err)
			}
			if !ok {
				continue
			}
			if d.amostras[pt.ParticipanteID], err = rastreio.AmostrasDoPasse(db, ps); err != nil {
				return d, err
			}
		}
	}
	return d, nil
}

// MedeObjetivos mede o degrau de cada dragão elemental nos jogadores do time
// que o pegou (só os rastreados têm atributos lidos).
func MedeObjetivos(db *banco.DB, gameID int64) (RespostaObjetivos, error) {
	r := RespostaObjetivos{GameID: gameID, Quedas: []Queda{}, Resumo: []Resumo{}, Regras: RegrasMedicao}
	d, err := carrega(db, gameID, true)
	if err != nil {
		return r, err
	}
	r.Patch = d.p.Patch
	timeDe := map[int]int{}
	porTime := map[int][]int{}
	for _, pt := range d.p.Participantes {
		timeDe[pt.ParticipanteID] = pt.Time
		porTime[pt.Time] = append(porTime[pt.Time], pt.ParticipanteID)
	}
	contagem := map[[2]any]int{}
	for _, q := range DragoesDaLinha(d.lt, timeDe) {
		k := [2]any{q.Time, q.Sub}
		contagem[k]++
		if _, ok := Tabela[q.Sub]; !ok {
			// Oceano e quimtec dão o que a tela não lê (cura, tenacidade).
			continue
		}
		r.Quedas = append(r.Quedas, MedeQueda(q, contagem[k], porTime[q.Time], d.amostras, d.estados))
	}
	r.Resumo = Resume(r.Quedas)
	if r.Resumo == nil {
		r.Resumo = []Resumo{}
	}
	return r, nil
}

// RespostaEstaticos é a resposta de GET .../estaticos: o que o app precisa
// para decompor os atributos e descrever os itens da partida.
type RespostaEstaticos struct {
	GameID      int64  `json:"game_id"`
	VersaoJogo  string `json:"versao_jogo"`
	VersaoDados string `json:"versao_dados"`
	// Campeoes por participante (não por id de campeão: é como a tela pergunta).
	Campeoes map[int]campeao.AtributosCampeao `json:"campeoes"`
	Itens    map[int]campeao.Item             `json:"itens"`
	Avisos   []string                         `json:"avisos"`
	Fontes   []string                         `json:"fontes"`
}

// FontesEstaticos vão na resposta para a tela citar.
var FontesEstaticos = []string{
	"Data Dragon (ddragon.leagueoflegends.com): itens (nome, custo, atributos, texto) e base dos campeões",
	"CommunityDragon (raw.communitydragon.org/<patch>/game/data/characters): registro do campeão nos arquivos do jogo (crescimento por nível e razão da velocidade de ataque)",
	"crescimento por nível: base + crescimento × (n − 1) × (0,7025 + 0,0175 × (n − 1)) (wiki.leagueoflegends.com/en-us/Champion_statistic)",
}

// ErrSemEstaticos repassa o erro da fonte (503 na rota).
var ErrSemEstaticos = campeao.ErrSemEstaticos

// EstaticosDaPartida junta os dados estáticos da versão da partida: os 10
// campeões e os itens que aparecem no inventário de alguém (estado dos 10 ou
// itens finais).
func EstaticosDaPartida(ctx context.Context, dep Deps, gameID int64) (RespostaEstaticos, error) {
	r := RespostaEstaticos{GameID: gameID, Campeoes: map[int]campeao.AtributosCampeao{}, Itens: map[int]campeao.Item{},
		Avisos: []string{}, Fontes: FontesEstaticos}
	p, err := partida.Le(dep.DB, gameID)
	if err != nil {
		return r, err
	}
	r.VersaoJogo = p.Versao
	ids := []int{}
	for _, pt := range p.Participantes {
		ids = append(ids, pt.CampeaoID)
	}
	est, err := dep.Estaticos.Estaticos(ctx, p.Versao, ids)
	if err != nil {
		return r, err
	}
	r.VersaoDados = est.VersaoDados
	r.Avisos = append(r.Avisos, est.Avisos...)
	for _, pt := range p.Participantes {
		if c, ok := est.Campeoes[pt.CampeaoID]; ok {
			r.Campeoes[pt.ParticipanteID] = c
		}
	}
	usados := map[int]bool{}
	for _, pt := range p.Participantes {
		for _, id := range pt.Itens {
			usados[id] = true
		}
	}
	estados, err := rastreio.EstadosDaPartida(dep.DB, gameID)
	if err != nil {
		return r, err
	}
	for _, es := range estados {
		for _, e := range es {
			for _, s := range e.Slots {
				usados[s.ID] = true
			}
		}
	}
	semDado := 0
	for id := range usados {
		if id == 0 {
			continue
		}
		if it, ok := est.Itens[id]; ok {
			r.Itens[id] = it
		} else {
			semDado++
		}
	}
	if semDado > 0 {
		r.Avisos = append(r.Avisos, fmt.Sprintf("%d item(ns) da partida sem dado nesta versão (item de modo especial ou novo)", semDado))
	}
	return r, nil
}

// EventoBuild é uma compra ou venda.
type EventoBuild struct {
	TMs  int64  `json:"t_ms"`
	Tipo string `json:"tipo"`
	Item int    `json:"item_id"`
	Nome string `json:"nome"`
	// Custo: o custo total do item (nulo sem os dados estáticos).
	Custo *int `json:"custo"`
	// OuroItens: o custo somado do que ficou no inventário depois do evento.
	OuroItens *int `json:"ouro_itens"`
}

// BuildJogador é a build de um jogador ao longo da partida.
type BuildJogador struct {
	ParticipanteID int           `json:"participante_id"`
	Campeao        string        `json:"campeao"`
	Eventos        []EventoBuild `json:"eventos"`
	// Final: o inventário no último estado gravado.
	Final []EventoBuild `json:"final"`
}

// RespostaBuild é a resposta de GET .../build.
type RespostaBuild struct {
	GameID      int64          `json:"game_id"`
	VersaoDados string         `json:"versao_dados"`
	Jogadores   []BuildJogador `json:"jogadores"`
	Avisos      []string       `json:"avisos"`
}

// Build monta, do estado dos 10 (o mesmo dado de onde sai a cadeia, com os
// itens compra/venda), a ordem e o instante de cada compra e venda de cada
// jogador (pid 0 = os 10), com nome e custo da versão da partida. Sem os
// dados estáticos (offline na primeira vez), nome vazio e custo nulo.
func Build(ctx context.Context, dep Deps, gameID int64, pid int) (RespostaBuild, error) {
	r := RespostaBuild{GameID: gameID, Jogadores: []BuildJogador{}, Avisos: []string{}}
	p, err := partida.Le(dep.DB, gameID)
	if err != nil {
		return r, err
	}
	ids := []int{}
	for _, pt := range p.Participantes {
		ids = append(ids, pt.CampeaoID)
	}
	est, err := dep.Estaticos.Estaticos(ctx, p.Versao, ids)
	if err != nil {
		r.Avisos = append(r.Avisos, "sem os dados estáticos da versão: sem nome e custo dos itens ("+err.Error()+")")
	}
	r.VersaoDados = est.VersaoDados
	estados, err := rastreio.EstadosDaPartida(dep.DB, gameID)
	if err != nil {
		return r, err
	}
	custo := func(id int) *int {
		if it, ok := est.Itens[id]; ok {
			c := it.Custo
			return &c
		}
		return nil
	}
	for _, pt := range p.Participantes {
		if pid != 0 && pt.ParticipanteID != pid {
			continue
		}
		bj := BuildJogador{ParticipanteID: pt.ParticipanteID, Campeao: pt.Campeao, Eventos: []EventoBuild{}, Final: []EventoBuild{}}
		var ant map[int]int
		var ultimo map[int]int
		for _, e := range estados[pt.ParticipanteID] {
			agora := map[int]int{}
			for _, s := range e.Slots {
				if s.ID != 0 {
					agora[s.ID]++
				}
			}
			ultimo = agora
			if ant == nil {
				// O primeiro estado gravado: o que já estava lá conta como
				// comprado nesse instante (o passe pode começar depois do 0:00).
				ant = map[int]int{}
			}
			var evs []EventoBuild
			for id, n := range ant {
				for k := agora[id]; k < n; k++ {
					evs = append(evs, EventoBuild{TMs: e.TMs, Tipo: "venda", Item: id, Nome: est.Itens[id].Nome, Custo: custo(id)})
				}
			}
			for id, n := range agora {
				for k := ant[id]; k < n; k++ {
					evs = append(evs, EventoBuild{TMs: e.TMs, Tipo: "compra", Item: id, Nome: est.Itens[id].Nome, Custo: custo(id)})
				}
			}
			// Vendas antes das compras no mesmo instante (o componente sai quando o
			// item maior entra); depois por id, para a ordem ser estável.
			sort.SliceStable(evs, func(a, b int) bool {
				if evs[a].Tipo != evs[b].Tipo {
					return evs[a].Tipo == "venda"
				}
				return evs[a].Item < evs[b].Item
			})
			if len(evs) > 0 {
				total := ouroDe(agora, est)
				for i := range evs {
					evs[i].OuroItens = total
				}
				bj.Eventos = append(bj.Eventos, evs...)
			}
			ant = agora
		}
		ordem := []int{}
		for id := range ultimo {
			ordem = append(ordem, id)
		}
		sort.Ints(ordem)
		for _, id := range ordem {
			for k := 0; k < ultimo[id]; k++ {
				bj.Final = append(bj.Final, EventoBuild{Tipo: "item", Item: id, Nome: est.Itens[id].Nome, Custo: custo(id)})
			}
		}
		r.Jogadores = append(r.Jogadores, bj)
	}
	return r, nil
}

// ouroDe soma o custo dos itens do inventário (nulo sem os dados estáticos).
func ouroDe(inv map[int]int, est campeao.Estaticos) *int {
	if est.Itens == nil {
		return nil
	}
	s := 0
	for id, n := range inv {
		s += est.Itens[id].Custo * n
	}
	return &s
}
