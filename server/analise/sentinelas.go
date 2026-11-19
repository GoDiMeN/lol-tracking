package analise

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"

	"loltracking/server/banco"
	"loltracking/server/rastreio"
)

// Sentinelas: a linha do tempo da Riot tem WARD_PLACED (creatorId, wardType) e
// WARD_KILL (killerId, wardType), SEM posição. A posição é ESTIMADA pelo lugar
// onde quem colocou estava no instante (amostras do passe dele): a sentinela
// cai a até AlcanceColocarSentinela dele, por isso o raio de incerteza. Quem não
// foi rastreado fica só com o instante. Sem a linha do tempo da Riot guardada,
// nada (a rota diz o motivo). Vocabulário em docs/arquitetura/dados.md.

func init() {
	rastreio.RegistraGerador("analise_sentinelas", geraSentinelas)
}

const (
	// AlcanceColocarSentinela: a sentinela é posta a até 600 do campeão
	// (League of Legends Wiki, Warding Totem / Control Ward, "target range 600",
	// conferido em 17/11/2026). Mais ~100 de erro da posição rastreada.
	AlcanceColocarSentinela = 600.0
	erroPosicaoSentinela    = 100.0
	// VelocidadeEstimativa: quando a amostra mais perto está a mais de 1,5 s, o
	// raio cresce com o que o jogador anda nesse tempo (~350 por segundo).
	VelocidadeEstimativa = 350.0
	// AlcanceDestruirSentinela: quem destrói bate na sentinela de perto (alcance
	// de 125 a ~650); o casamento por proximidade aceita até isso além do raio de
	// incerteza da sentinela.
	AlcanceDestruirSentinela = 800.0
)

// Duração por tipo (League of Legends Wiki, página "Ward", conferida em
// 17/11/2026, patch 25.S1/26.x): Totem Ward (YELLOW_TRINKET) 90–120 s pela média
// dos níveis de todos os campeões; Stealth Ward (SIGHT_WARD: item de suporte,
// Bloodsong, Runic Compass) 150 s; Control Ward e Farsight Ward (BLUE_TRINKET)
// sem prazo, até serem destruídas. Limites por jogador: 3 entre Totem e Stealth
// (a quarta derruba a mais velha), 1 de controle.
const (
	DuracaoStealthMs   = 150000
	DuracaoTotemMinMs  = 90000
	DuracaoTotemMaxMs  = 120000
	LimiteSentinelas   = 3
	LimiteDeControle   = 1
	TipoTotem          = "YELLOW_TRINKET"
	TipoStealth        = "SIGHT_WARD"
	TipoControle       = "CONTROL_WARD"
	TipoFarsight       = "BLUE_TRINKET"
	sentinelaColocada  = "sentinela_colocada"
	sentinelaDestruida = "sentinela_destruida"
)

// NomesSentinela: o nome na tela de cada wardType conhecido. Tipo fora daqui
// (UNDEFINED, boneco de campeão...) aparece com o tipo cru e não conta como
// sentinela comum.
var NomesSentinela = map[string]string{
	TipoTotem:    "amarela (Totem)",
	TipoStealth:  "invisível do item",
	TipoControle: "de controle",
	TipoFarsight: "azul (Farsight)",
}

// ContaComoSentinela é A regra de contagem, a mesma em todo lugar (base de
// fatos, indicadores, resumo por jogador e time, pacote de contexto da IA):
// só os quatro tipos conhecidos contam. O UNDEFINED da Riot (e outros tipos
// crus) continua na lista da partida, marcado comum=false, mas não é
// sentinela colocada nem destruída: um Kayn com 88 "sentinelas" UNDEFINED não
// pode virar 88 num lugar e 0 no outro.
func ContaComoSentinela(tipo string) bool { return NomesSentinela[tipo] != "" }

// eventoSentinela é um WARD_PLACED (colocada) ou WARD_KILL da Riot.
type eventoSentinela struct {
	tMs      int64
	quem     int
	tipo     string
	colocada bool
}

// sentinelasDosEventos tira as sentinelas dos eventos crus da Riot.
func sentinelasDosEventos(es []rastreio.EventoLT) []eventoSentinela {
	var l []eventoSentinela
	for _, e := range es {
		if e.Tipo != "WARD_PLACED" && e.Tipo != "WARD_KILL" {
			continue
		}
		var b struct {
			Criador  int    `json:"creatorId"`
			Matador  int    `json:"killerId"`
			WardType string `json:"wardType"`
		}
		json.Unmarshal(e.Bruto, &b)
		ev := eventoSentinela{tMs: e.TMs, tipo: b.WardType, colocada: e.Tipo == "WARD_PLACED", quem: b.Matador}
		if ev.colocada {
			ev.quem = b.Criador
		}
		if ev.quem <= 0 {
			continue // sem dono (participante 0): não dá para dizer de quem é
		}
		l = append(l, ev)
	}
	sort.SliceStable(l, func(a, b int) bool { return l[a].tMs < l[b].tMs })
	return l
}

// estimaPosicao: onde o jogador estava em t (rastreado) e o raio de incerteza.
func estimaPosicao(as []rastreio.Amostra, t int64) (Ponto, float64, bool) {
	if p, ok := PosicaoEm(as, t); ok {
		return p, AlcanceColocarSentinela + erroPosicaoSentinela, true
	}
	// A amostra viva mais perto a até 3 s.
	i := sort.Search(len(as), func(k int) bool { return as[k].TMs >= t })
	melhor, achou := int64(3001), -1
	for k := max(0, i-15); k < len(as) && k <= i+15; k++ {
		a := as[k]
		if a.Morto || a.X == nil || a.Y == nil {
			continue
		}
		d := a.TMs - t
		if d < 0 {
			d = -d
		}
		if d < melhor {
			melhor, achou = d, k
		}
	}
	if achou < 0 {
		return Ponto{}, 0, false
	}
	a := as[achou]
	return Ponto{*a.X, *a.Y}, AlcanceColocarSentinela + erroPosicaoSentinela + VelocidadeEstimativa*float64(melhor)/1000, true
}

// geraSentinelas põe na cadeia as sentinelas que o jogador colocou e destruiu.
func geraSentinelas(c *rastreio.ContextoCadeia) []rastreio.ItemCadeia {
	extra, ok := c.Extra[ExtraEventosRiot].([]rastreio.EventoLT)
	if !ok || len(c.Amostras) == 0 {
		return nil
	}
	ini, fim := c.Amostras[0].TMs, c.Amostras[len(c.Amostras)-1].TMs
	var itens []rastreio.ItemCadeia
	for _, e := range sentinelasDosEventos(extra) {
		if e.quem != c.ParticipanteID || e.tMs < ini || e.tMs > fim {
			continue
		}
		d := map[string]any{"tipo": e.tipo, "tipo_nome": nomeSentinela(e.tipo), "comum": NomesSentinela[e.tipo] != ""}
		if pt, raio, ok := estimaPosicao(c.Amostras, e.tMs); ok {
			z, _ := Zona(pt.X, pt.Y)
			d["x"], d["y"], d["raio"], d["zona"] = arr(pt.X), arr(pt.Y), arr(raio), z
			if e.colocada {
				d["metade_inimiga"] = metadeInimiga(pt, c.Time)
			}
		}
		tipo := sentinelaDestruida
		if e.colocada {
			tipo = sentinelaColocada
		}
		itens = append(itens, rastreio.ItemCadeia{TMs: e.tMs, Tipo: tipo, Dados: d})
	}
	return itens
}

func nomeSentinela(tipo string) string {
	if n, ok := NomesSentinela[tipo]; ok {
		return n
	}
	return tipo
}

// Sentinela é uma sentinela colocada (GET /api/partidas/{id}/sentinelas).
type Sentinela struct {
	ParticipanteID int    `json:"participante_id"`
	Time           int    `json:"time"`
	TMs            int64  `json:"t_ms"`
	Tipo           string `json:"tipo"`      // wardType cru da Riot
	TipoNome       string `json:"tipo_nome"` // nome na tela (o cru, se desconhecido)
	Comum          bool   `json:"comum"`     // um dos quatro tipos conhecidos
	Rastreado      bool   `json:"rastreado"`
	// Posição ESTIMADA (onde quem colocou estava) e o raio de incerteza; nulos
	// sem rastreio dele naquele instante.
	X             *float64 `json:"x"`
	Y             *float64 `json:"y"`
	Raio          *float64 `json:"raio"`
	Zona          string   `json:"zona"`
	MetadeInimiga *bool    `json:"metade_inimiga"`
	// AteMs: até quando ficou (estimado). Nulo = sem prazo conhecido: a de
	// controle e a azul não casadas com nenhuma destruição (pode estar ativa até o
	// fim) ou tipo desconhecido.
	AteMs        *int64 `json:"ate_ms"`
	Fim          string `json:"fim"`  // expirou, destruida, substituida, pode_estar_ativa, desconhecido
	Vida         string `json:"vida"` // a duração na frase da tela
	Aproximado   bool   `json:"aproximado"`
	DestruidaPor *int   `json:"destruida_por"`
}

// Destruicao é um WARD_KILL.
type Destruicao struct {
	ParticipanteID int    `json:"participante_id"`
	Time           int    `json:"time"`
	TMs            int64  `json:"t_ms"`
	Tipo           string `json:"tipo"`
	TipoNome       string `json:"tipo_nome"`
	// Casada: o índice (em `sentinelas`) da sentinela que ela derrubou, pelo
	// casamento por proximidade; nulo = não deu para dizer qual.
	Casada *int `json:"casada"`
}

// nivelMedio: a média dos níveis dos 10 no último quadro de minuto até t.
func nivelMedio(lt rastreio.LinhaDoTempo, t int64) float64 {
	var q *rastreio.QuadroLT
	for i := range lt.Quadros {
		if lt.Quadros[i].TMs <= t {
			q = &lt.Quadros[i]
		}
	}
	if q == nil || len(q.Jogadores) == 0 {
		return 1
	}
	s := 0
	for _, j := range q.Jogadores {
		s += j.Nivel
	}
	return float64(s) / float64(len(q.Jogadores))
}

// duracaoTotem: 90 s no nível médio 1 a 120 s no 18 (a Wiki dá só os extremos;
// a reta entre eles é estimativa).
func duracaoTotem(nivel float64) int64 {
	f := math.Max(0, math.Min(1, (nivel-1)/17))
	return int64(DuracaoTotemMinMs + f*float64(DuracaoTotemMaxMs-DuracaoTotemMinMs))
}

// montaSentinelas calcula a vida estimada de cada sentinela: prazo do tipo,
// limite por jogador e destruição casada por proximidade.
func montaSentinelas(eventos []eventoSentinela, timeDe map[int]int, amostras map[int][]rastreio.Amostra, lt rastreio.LinhaDoTempo, fimPartida int64) ([]Sentinela, []Destruicao) {
	sents := []Sentinela{}
	dests := []Destruicao{}
	natural := []int64{} // fim pelo prazo (-1 = sem prazo)
	for _, e := range eventos {
		if !e.colocada {
			continue
		}
		s := Sentinela{ParticipanteID: e.quem, Time: timeDe[e.quem], TMs: e.tMs, Tipo: e.tipo, TipoNome: nomeSentinela(e.tipo),
			Comum: NomesSentinela[e.tipo] != ""}
		if as := amostras[e.quem]; len(as) > 0 && e.tMs >= as[0].TMs && e.tMs <= as[len(as)-1].TMs {
			s.Rastreado = true
			if pt, raio, ok := estimaPosicao(as, e.tMs); ok {
				x, y, r := math.Round(pt.X), math.Round(pt.Y), math.Round(raio)
				s.X, s.Y, s.Raio = &x, &y, &r
				s.Zona, _ = Zona(pt.X, pt.Y)
				m := metadeInimiga(pt, s.Time)
				s.MetadeInimiga = &m
			}
		}
		fim := int64(-1)
		switch e.tipo {
		case TipoStealth:
			fim = e.tMs + DuracaoStealthMs
			s.Vida, s.Fim = "150 s", "expirou"
		case TipoTotem:
			nv := nivelMedio(lt, e.tMs)
			d := duracaoTotem(nv)
			fim = e.tMs + d
			s.Vida, s.Fim, s.Aproximado = fmt.Sprintf("~%d s (90–120 s pelo nível médio %.1f)", d/1000, nv), "expirou", true
		case TipoControle, TipoFarsight:
			s.Vida, s.Fim, s.Aproximado = "até ser destruída", "pode_estar_ativa", true
		default:
			s.Vida, s.Fim = "desconhecida (tipo fora da tabela)", "desconhecido"
		}
		natural = append(natural, fim)
		sents = append(sents, s)
	}
	fimDe := func(i int) int64 { // fim atual (-1 = sem fim)
		if sents[i].AteMs != nil {
			return *sents[i].AteMs
		}
		return natural[i]
	}
	viva := func(i int, t int64) bool {
		f := fimDe(i)
		return sents[i].TMs <= t && (f < 0 || f > t)
	}
	encerra := func(i int, t int64, motivo string) {
		v := t
		sents[i].AteMs, sents[i].Fim = &v, motivo
	}
	k := 0 // próxima sentinela colocada
	for _, e := range eventos {
		if e.colocada {
			// Limite por jogador: a mais velha viva do mesmo grupo cai.
			grupo := func(t string) int {
				switch t {
				case TipoTotem, TipoStealth:
					return 1
				case TipoControle:
					return 2
				}
				return 0
			}
			g := grupo(e.tipo)
			if g > 0 {
				lim := LimiteSentinelas
				if g == 2 {
					lim = LimiteDeControle
				}
				var vivas []int
				for i := 0; i < k; i++ {
					if sents[i].ParticipanteID == e.quem && grupo(sents[i].Tipo) == g && viva(i, e.tMs) {
						vivas = append(vivas, i)
					}
				}
				for len(vivas) >= lim {
					encerra(vivas[0], e.tMs, "substituida")
					vivas = vivas[1:]
				}
			}
			k++
			continue
		}
		d := Destruicao{ParticipanteID: e.quem, Time: timeDe[e.quem], TMs: e.tMs, Tipo: e.tipo, TipoNome: nomeSentinela(e.tipo)}
		// Candidatas: do mesmo tipo, do outro time, vivas agora.
		var cands []int
		for i := 0; i < k; i++ {
			if sents[i].Tipo == e.tipo && sents[i].Time != d.Time && viva(i, e.tMs) {
				cands = append(cands, i)
			}
		}
		achou := -1
		if pm, _, ok := estimaPosicao(amostras[e.quem], e.tMs); ok {
			melhor := math.Inf(1)
			for _, i := range cands {
				if sents[i].X == nil {
					continue
				}
				dd := dist(pm, Ponto{*sents[i].X, *sents[i].Y})
				if dd <= *sents[i].Raio+AlcanceDestruirSentinela && dd < melhor {
					melhor, achou = dd, i
				}
			}
		}
		if achou < 0 && len(cands) == 1 {
			achou = cands[0] // só uma possível
		}
		if achou >= 0 {
			encerra(achou, e.tMs, "destruida")
			sents[achou].Aproximado = true
			q := e.quem
			sents[achou].DestruidaPor = &q
			idx := achou
			d.Casada = &idx
		}
		dests = append(dests, d)
	}
	for i := range sents {
		if sents[i].AteMs == nil && natural[i] >= 0 {
			v := min(natural[i], max(fimPartida, sents[i].TMs))
			sents[i].AteMs = &v
		}
	}
	return sents, dests
}

// ResumoSentinelasTime e ResumoSentinelasJogador: as contagens da resposta.
type ResumoSentinelasTime struct {
	Time       int `json:"time"`
	Colocadas  int `json:"colocadas"`
	DeControle int `json:"de_controle"`
	Destruidas int `json:"destruidas"`
}

type ResumoSentinelasJogador struct {
	ParticipanteID int            `json:"participante_id"`
	Time           int            `json:"time"`
	Rastreado      bool           `json:"rastreado"`
	Colocadas      int            `json:"colocadas"`
	PorFase        map[string]int `json:"por_fase"`
	DeControle     int            `json:"de_controle"`
	Destruidas     int            `json:"destruidas"`
	// MetadeInimiga: só com posição estimada (nulo sem rastreio).
	MetadeInimiga *int `json:"metade_inimiga"`
	SemPosicao    int  `json:"sem_posicao"`
}

// SentinelasDaPartida é a resposta de GET /api/partidas/{id}/sentinelas.
type SentinelasDaPartida struct {
	GameID      int64                     `json:"game_id"`
	Riot        bool                      `json:"riot"`
	Motivo      string                    `json:"motivo"`
	Sentinelas  []Sentinela               `json:"sentinelas"`
	Destruicoes []Destruicao              `json:"destruicoes"`
	Times       []ResumoSentinelasTime    `json:"times"`
	Jogadores   []ResumoSentinelasJogador `json:"jogadores"`
	Regras      []string                  `json:"regras"`
}

// RegrasSentinelas: o que a tela mostra como "de onde vem".
var RegrasSentinelas = []string{
	"Posição ESTIMADA: onde quem colocou estava no instante (a sentinela cai a até 600 dele); o círculo é a incerteza.",
	"Duração (League of Legends Wiki, conferida em 17/11/2026): amarela 90–120 s pelo nível médio; do item 150 s; de controle e azul até serem destruídas.",
	"Destruição casada pela proximidade de quem destruiu (mesmo tipo, outro time): aproximada. De controle sem destruição casada pode estar ativa até o fim.",
	"Quem não foi rastreado: só o instante e a contagem, sem lugar.",
}

// sentinelas: a lista da partida (dados já carregados).
func (d *dadosPartida) sentinelas() ([]Sentinela, []Destruicao) {
	if d.sentCache != nil {
		return d.sentCache.s, d.sentCache.d
	}
	timeDe := map[int]int{}
	for _, p := range d.pt.Participantes {
		timeDe[p.ParticipanteID] = p.Time
	}
	s, ds := montaSentinelas(d.eventosSentinela, timeDe, d.amostras, d.lt, d.duracaoMs())
	d.sentCache = &cacheSentinelas{s, ds}
	return s, ds
}

type cacheSentinelas struct {
	s []Sentinela
	d []Destruicao
}

// LeSentinelas monta a resposta da rota (calculado na hora, não grava).
func LeSentinelas(db *banco.DB, gameID int64) (SentinelasDaPartida, error) {
	r := SentinelasDaPartida{GameID: gameID, Sentinelas: []Sentinela{}, Destruicoes: []Destruicao{}, Times: []ResumoSentinelasTime{},
		Jogadores: []ResumoSentinelasJogador{}, Regras: RegrasSentinelas}
	dp, err := carrega(db, gameID)
	if err != nil {
		return r, err
	}
	if dp.riot == nil {
		r.Motivo = "sem a linha do tempo da Riot guardada (precisa da chave da Riot na análise): sem sentinelas, nada é inventado"
		return r, nil
	}
	r.Riot = true
	r.Sentinelas, r.Destruicoes = dp.sentinelas()
	for _, tm := range []int{100, 200} {
		rt := ResumoSentinelasTime{Time: tm}
		for _, s := range r.Sentinelas {
			if s.Time == tm && ContaComoSentinela(s.Tipo) {
				rt.Colocadas++
				if s.Tipo == TipoControle {
					rt.DeControle++
				}
			}
		}
		for _, x := range r.Destruicoes {
			if x.Time == tm && ContaComoSentinela(x.Tipo) {
				rt.Destruidas++
			}
		}
		r.Times = append(r.Times, rt)
	}
	for _, p := range dp.pt.Participantes {
		r.Jogadores = append(r.Jogadores, dp.resumoSentinelas(p.ParticipanteID, p.Time))
	}
	return r, nil
}

func (d *dadosPartida) resumoSentinelas(pid, time int) ResumoSentinelasJogador {
	s, ds := d.sentinelas()
	j := ResumoSentinelasJogador{ParticipanteID: pid, Time: time, Rastreado: len(d.amostras[pid]) > 0, PorFase: map[string]int{}}
	for _, f := range Fases {
		j.PorFase[f] = 0
	}
	inimiga, comPos := 0, 0
	for _, x := range s {
		if x.ParticipanteID != pid || !ContaComoSentinela(x.Tipo) {
			continue
		}
		j.Colocadas++
		j.PorFase[Fase(x.TMs)]++
		if x.Tipo == TipoControle {
			j.DeControle++
		}
		if x.MetadeInimiga != nil {
			comPos++
			if *x.MetadeInimiga {
				inimiga++
			}
		} else {
			j.SemPosicao++
		}
	}
	if j.Rastreado {
		j.MetadeInimiga = &inimiga
	}
	for _, x := range ds {
		if x.ParticipanteID == pid && ContaComoSentinela(x.Tipo) {
			j.Destruidas++
		}
	}
	return j
}

// indicadoresSentinelas: sentinelas por fase, de controle, destruídas (os 10)
// e na metade inimiga (rastreados; aproximado: posição estimada).
func (d *dadosPartida) indicadoresSentinelas(c *coleta) {
	if d.riot == nil {
		return
	}
	j := d.resumoSentinelas(c.pid, 0)
	s, ds := d.sentinelas()
	mom := func(f func(Sentinela) bool) []Momento {
		l := []Momento{}
		for _, x := range s {
			if x.ParticipanteID == c.pid && ContaComoSentinela(x.Tipo) && f(x) {
				l = append(l, Momento{x.TMs, fmt.Sprintf("sentinela %s aos %s", x.TipoNome, MinSeg(x.TMs))})
			}
		}
		return l
	}
	for _, f := range Fases {
		ff := f
		c.conta("sentinelas_"+f, mom(func(x Sentinela) bool { return Fase(x.TMs) == ff }), false, nil)
	}
	c.conta("sentinelas_controle", mom(func(x Sentinela) bool { return x.Tipo == TipoControle }), false, nil)
	dest := []Momento{}
	for _, x := range ds {
		if x.ParticipanteID == c.pid && ContaComoSentinela(x.Tipo) {
			dest = append(dest, Momento{x.TMs, fmt.Sprintf("destruiu uma sentinela %s aos %s", x.TipoNome, MinSeg(x.TMs))})
		}
	}
	c.conta("sentinelas_destruidas", dest, false, nil)
	if j.Rastreado {
		c.conta("sentinelas_metade_inimiga", mom(func(x Sentinela) bool { return x.MetadeInimiga != nil && *x.MetadeInimiga }), true,
			map[string]any{"sem_posicao": j.SemPosicao})
	}
}
