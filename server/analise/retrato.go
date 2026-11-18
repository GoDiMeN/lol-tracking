package analise

import (
	"encoding/json"
	"math"
	"sort"

	"loltracking/server/partida"
	"loltracking/server/rastreio"
)

// dadosPartida é tudo o que a análise de uma partida lê, já decodificado.
type dadosPartida struct {
	pt          partida.Partida
	lt          rastreio.LinhaDoTempo
	amostras    map[int][]rastreio.Amostra       // dos rastreados (melhor passe)
	cadeias     map[int][]rastreio.ItemCadeia    // dos rastreados
	estados     map[int][]rastreio.EstadoJogador // os 10, de todos os passes
	riot        []abateRiot                      // nil = sem a linha do tempo da Riot
	quadrosRiot map[int64]map[int]statsRiot      // por minuto (t_ms do quadro) e participante
	wards       []wardRiot
	// eventosSentinela: WARD_PLACED e WARD_KILL da Riot (sentinelas.go); sentCache: a
	// lista montada na primeira vez.
	eventosSentinela []eventoSentinela
	sentCache        *cacheSentinelas
	// visao: o resultado dos passes de visão de time (nil = não rodaram).
	visao *rastreio.VisaoDaPartida
	// eventosRiot: todos os eventos da linha do tempo da Riot (compras de item,
	// placas), já lidos; nil sem ela.
	eventosRiot []rastreio.EventoLT
}

func (d *dadosPartida) participante(pid int) *partida.Participante {
	for i := range d.pt.Participantes {
		if d.pt.Participantes[i].ParticipanteID == pid {
			return &d.pt.Participantes[i]
		}
	}
	return nil
}

// FeiticoFlash é o id do Flash (summoner spell).
const FeiticoFlash = 4

// JogadorNoAbate é um dos 10 no instante de um abate.
type JogadorNoAbate struct {
	ParticipanteID int    `json:"participante_id"`
	Time           int    `json:"time"`
	Papel          string `json:"papel"` // vitima, matador, assistencia ou ""
	// Posição: "rastreado" (amostras do passe dele), "evento" (o local do
	// abate, para a vítima), "morte" (morto: onde morreu), "linha_do_tempo"
	// (interpolada nos quadros de minuto: aproximada) ou "" (sem posição).
	X          *float64 `json:"x"`
	Y          *float64 `json:"y"`
	Origem     string   `json:"origem"`
	Aproximada bool     `json:"aproximada"`
	Zona       string   `json:"zona"`
	Subzona    string   `json:"subzona"`
	DistVitima *float64 `json:"dist_vitima"`
	// Vivo: nulo = não se sabe (trecho sem estado gravado).
	Vivo    *bool    `json:"vivo"`
	VidaPct *float64 `json:"vida_pct"` // quadro lateral (os 10), a última leitura até o instante
	ManaPct *float64 `json:"mana_pct"`
	Nivel   int      `json:"nivel"`
	// Só de quem foi rastreado (painel do jogador seguido).
	Rastreado       bool               `json:"rastreado"`
	Vida            *float64           `json:"vida"`
	VidaMax         *float64           `json:"vida_max"`
	CD              *rastreio.Recargas `json:"cd"`
	RAprendido      *bool              `json:"r_aprendido"`
	RDisponivel     *bool              `json:"r_disponivel"` // nulo: não lido ou R ainda não aprendido
	FlashDisponivel *bool              `json:"flash_disponivel"`
	// Passiva: nulo = não lida (passe anterior à leitura dela). Só diz algo em
	// campeão cuja passiva tem recarga (Vex, Garen...); nos outros é sempre true.
	PassivaDisponivel *bool `json:"passiva_disponivel"`
	// Perto (a menos de Param.RaioSuperioridade), contando só quem tem posição.
	AliadosPerto  int `json:"aliados_perto"`
	InimigosPerto int `json:"inimigos_perto"`
	// Há quanto tempo estava fora da rota (rotador rastreado numa saída).
	ForaDaRotaMs *int64              `json:"fora_da_rota_ms"`
	Itens        []rastreio.SlotItem `json:"itens"`
}

// Retrato é o retrato de um abate: onde e como estava cada um dos 10.
type Retrato struct {
	N            int      `json:"n"` // 1, 2, ... na ordem do tempo
	TMs          int64    `json:"t_ms"`
	Vitima       int      `json:"vitima"`
	Matador      int      `json:"matador"`
	Assistencias []int    `json:"assistencias"`
	X            *float64 `json:"x"`
	Y            *float64 `json:"y"`
	Zona         string   `json:"zona"`
	Subzona      string   `json:"subzona"`
	// Em volta da vítima (a menos de RaioSuperioridade): os aliados dela e os
	// inimigos dela, vivos e com posição. Superioridade = inimigos − (aliados +
	// a própria vítima) (> 0: a vítima estava em inferioridade numérica).
	AliadosDaVitima   int              `json:"aliados_da_vitima"`
	InimigosDaVitima  int              `json:"inimigos_da_vitima"`
	Superioridade     int              `json:"superioridade"`
	AliadoMaisProximo *float64         `json:"aliado_mais_proximo"`
	Aproximado        bool             `json:"aproximado"`
	Jogadores         []JogadorNoAbate `json:"jogadores"`
	// Da linha do tempo da Riot (nulo sem ela): quem bateu na vítima, com o
	// quê e quanto; e em quem a vítima bateu.
	Riot *RiotNoAbate `json:"riot"`
}

func fp(v float64) *float64 { return &v }
func bp(v bool) *bool       { return &v }

// retratos monta um retrato por CHAMPION_KILL da linha do tempo do LCU.
func (d *dadosPartida) retratos() []Retrato {
	var lista []Retrato
	for _, e := range d.lt.Eventos {
		if e.Tipo != "CHAMPION_KILL" || e.EmQuem <= 0 {
			continue
		}
		lista = append(lista, d.retrato(len(lista)+1, e))
	}
	return lista
}

func (d *dadosPartida) retrato(n int, e rastreio.EventoLT) Retrato {
	t := e.TMs
	r := Retrato{N: n, TMs: t, Vitima: e.EmQuem, Matador: e.Quem, Assistencias: e.Assistencias, Jogadores: []JogadorNoAbate{}}
	if r.Assistencias == nil {
		r.Assistencias = []int{}
	}
	if e.X != nil {
		r.X, r.Y = fp(*e.X), fp(*e.Y)
		r.Zona, r.Subzona = Zona(*e.X, *e.Y)
	}
	for _, part := range d.pt.Participantes {
		pid := part.ParticipanteID
		j := JogadorNoAbate{ParticipanteID: pid, Time: part.Time, Itens: []rastreio.SlotItem{}}
		switch {
		case pid == e.EmQuem:
			j.Papel = "vitima"
		case pid == e.Quem:
			j.Papel = "matador"
		case contem(e.Assistencias, pid):
			j.Papel = "assistencia"
		}
		// Estado pelos quadros laterais / playerlist (os 10).
		if es := EstadoEm(d.estados[pid], t, 2500); es != nil {
			j.Vivo = bp(!es.Morto)
			j.VidaPct, j.ManaPct, j.Nivel = es.VidaPct, es.ManaPct, es.Nivel
			j.Itens = es.Itens()
		}
		// Posição e painel de quem foi rastreado.
		as := d.amostras[pid]
		if len(as) > 0 {
			if a := AmostraEm(as, t, 1500); a != nil {
				j.Rastreado = true
				if j.Vivo == nil {
					j.Vivo = bp(!a.Morto)
				}
				if j.Nivel == 0 {
					j.Nivel = a.Nivel
				}
				j.Vida, j.VidaMax = ultimoNaoNulo(as, t, func(a *rastreio.Amostra) *float64 { return a.Vida }),
					ultimoNaoNulo(as, t, func(a *rastreio.Amostra) *float64 { return a.VidaMax })
				cd := rastreio.Recargas{
					Q: ultimoNaoNulo(as, t, func(a *rastreio.Amostra) *float64 { return a.CD.Q }),
					W: ultimoNaoNulo(as, t, func(a *rastreio.Amostra) *float64 { return a.CD.W }),
					E: ultimoNaoNulo(as, t, func(a *rastreio.Amostra) *float64 { return a.CD.E }),
					R: ultimoNaoNulo(as, t, func(a *rastreio.Amostra) *float64 { return a.CD.R }),
					D: ultimoNaoNulo(as, t, func(a *rastreio.Amostra) *float64 { return a.CD.D }),
					F: ultimoNaoNulo(as, t, func(a *rastreio.Amostra) *float64 { return a.CD.F }),
					P: ultimoNaoNulo(as, t, func(a *rastreio.Amostra) *float64 { return a.CD.P }),
				}
				j.CD = &cd
				if cd.R != nil {
					j.RAprendido = bp(*cd.R >= 0)
					if *cd.R >= 0 {
						j.RDisponivel = bp(*cd.R == 0)
					}
				}
				if cd.P != nil {
					j.PassivaDisponivel = bp(*cd.P == 0)
				}
				var flash *float64
				if part.Feitico1 == FeiticoFlash {
					flash = cd.D
				} else if part.Feitico2 == FeiticoFlash {
					flash = cd.F
				}
				if flash != nil {
					j.FlashDisponivel = bp(*flash == 0)
				}
				if p, ok := PosicaoEm(as, t); ok && pid != e.EmQuem {
					j.X, j.Y, j.Origem = fp(p.X), fp(p.Y), "rastreado"
				} else if a.Morto && a.X != nil && pid != e.EmQuem {
					j.X, j.Y, j.Origem = fp(*a.X), fp(*a.Y), "morte"
				}
				j.ForaDaRotaMs = foraDaRota(d.cadeias[pid], t)
			}
		}
		if pid == e.EmQuem && e.X != nil {
			j.X, j.Y, j.Origem = fp(*e.X), fp(*e.Y), "evento"
			j.Vivo = bp(false)
		}
		if j.X == nil && j.Vivo != nil && !*j.Vivo {
			// Morto antes do abate: onde morreu (o último abate em que foi vítima).
			if m := d.lt.MorteAntes(pid, t-1); m != nil && m.X != nil {
				j.X, j.Y, j.Origem = fp(*m.X), fp(*m.Y), "morte"
			}
		}
		if j.X == nil && (j.Vivo == nil || *j.Vivo) {
			// Quem não foi rastreado mas matou, ajudou ou morreu num abate a até
			// 8 s daqui estava perto do lugar daquele abate: melhor que a
			// interpolação de minuto em minuto.
			if x, y, ok := d.pertoDeAbate(pid, t); ok {
				j.X, j.Y, j.Origem, j.Aproximada = fp(x), fp(y), "perto_do_abate", true
			}
		}
		if j.X == nil {
			if x, y, ok := d.lt.PosicaoInterpolada(pid, t); ok {
				j.X, j.Y, j.Origem, j.Aproximada = fp(x), fp(y), "linha_do_tempo", true
			}
		}
		if j.X != nil {
			j.Zona, j.Subzona = Zona(*j.X, *j.Y)
			if r.X != nil {
				j.DistVitima = fp(math.Round(math.Hypot(*j.X-*r.X, *j.Y-*r.Y)))
			}
		}
		r.Jogadores = append(r.Jogadores, j)
	}
	// Quem estava perto de quem (só vivos com posição; a vítima conta como
	// viva no instante em que morre).
	vivoOuVitima := func(j *JogadorNoAbate) bool {
		return j.X != nil && (j.Papel == "vitima" || j.Vivo == nil || *j.Vivo)
	}
	for a := range r.Jogadores {
		ja := &r.Jogadores[a]
		if !vivoOuVitima(ja) {
			continue
		}
		for b := range r.Jogadores {
			jb := &r.Jogadores[b]
			if a == b || !vivoOuVitima(jb) || jb.Origem == "morte" {
				continue
			}
			if math.Hypot(*ja.X-*jb.X, *ja.Y-*jb.Y) <= Param.RaioSuperioridade {
				if jb.Time == ja.Time {
					ja.AliadosPerto++
				} else {
					ja.InimigosPerto++
				}
			}
		}
	}
	for i := range r.Jogadores {
		j := &r.Jogadores[i]
		if j.Papel == "vitima" {
			r.AliadosDaVitima, r.InimigosDaVitima = j.AliadosPerto, j.InimigosPerto
			r.Superioridade = j.InimigosPerto - (j.AliadosPerto + 1)
			continue
		}
		if j.Aproximada && j.DistVitima != nil && *j.DistVitima <= Param.RaioSuperioridade*2 {
			// Uma posição aproximada perto da vítima pode mudar a contagem.
			r.Aproximado = true
		}
	}
	vit := d.participante(e.EmQuem)
	for i := range r.Jogadores {
		j := &r.Jogadores[i]
		if vit != nil && j.Time == vit.Time && j.ParticipanteID != e.EmQuem && j.DistVitima != nil && vivoOuVitima(j) && j.Origem != "morte" {
			if r.AliadoMaisProximo == nil || *j.DistVitima < *r.AliadoMaisProximo {
				r.AliadoMaisProximo = fp(*j.DistVitima)
				if j.Aproximada {
					r.Aproximado = true
				}
			}
		}
	}
	if d.riot != nil {
		r.Riot = d.riotDoAbate(e)
	}
	return r
}

// ultimoNaoNulo: o último valor lido até t, a até 3 s (a leitura da tela sai
// nula em fração das amostras).
func ultimoNaoNulo(as []rastreio.Amostra, t int64, campo func(*rastreio.Amostra) *float64) *float64 {
	i := sort.Search(len(as), func(k int) bool { return as[k].TMs > t }) - 1
	for k := i; k >= 0 && as[k].TMs >= t-3000; k-- {
		if as[k].Morto {
			// Depois de morto o painel some: vale o que se leu antes.
			continue
		}
		if v := campo(&as[k]); v != nil {
			return v
		}
	}
	return nil
}

// foraDaRota: se em t o jogador estava numa saída de rota (da cadeia), há
// quanto tempo.
func foraDaRota(cadeia []rastreio.ItemCadeia, t int64) *int64 {
	for _, it := range cadeia {
		if it.Tipo != "saiu_da_rota" || it.TMs > t {
			continue
		}
		fim := numero(it.Dados["fim_ms"])
		if float64(t) <= fim+1500 {
			v := t - it.TMs
			return &v
		}
	}
	return nil
}

func numero(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int64:
		return float64(n)
	case int:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	}
	return math.NaN()
}

// --- Riot ---------------------------------------------------------------------

// DanoRiot é uma linha de victimDamageReceived/Dealt.
type DanoRiot struct {
	ParticipanteID int    `json:"participante_id"`
	Nome           string `json:"nome"`       // campeão (ou "SRU_..." para torre/tropa/monstro)
	Habilidade     string `json:"habilidade"` // spellName (ex.: "akshanq") ou "ataque básico"
	Slot           int    `json:"slot"`       // spellSlot: 0 Q, 1 W, 2 E, 3 R, 4/5 feitiços; -1 desconhecido
	Basico         bool   `json:"basico"`
	Tipo           string `json:"tipo"` // OTHER, MINION, TOWER, MONSTER...
	Fisico         int    `json:"fisico"`
	Magico         int    `json:"magico"`
	Verdadeiro     int    `json:"verdadeiro"`
	Total          int    `json:"total"`
}

// RiotNoAbate é o que a Riot diz de um abate.
type RiotNoAbate struct {
	Recebido []DanoRiot `json:"recebido"`
	Causado  []DanoRiot `json:"causado"`
	// Por participante: total de dano que cada um causou na vítima.
	PorJogador map[int]int `json:"por_jogador"`
	Ouro       int         `json:"ouro"`
	Sequencia  int         `json:"sequencia"`
}

type abateRiot struct {
	tMs                int64
	quem, emQuem       int
	recebido, causado  []DanoRiot
	bounty, killStreak int
	// shutdown: o ouro a mais por encerrar a sequência da vítima (shutdownBounty).
	shutdown int
}

type statsRiot struct {
	Health, HealthMax, Power, PowerMax float64
	MinionsKilled, JungleMinionsKilled int
	TotalGold, XP                      int
	DanoCampeoes                       int
}

func leAbatesRiot(bruto []byte) []abateRiot {
	var cru struct {
		Info struct {
			Frames []struct {
				Events []json.RawMessage `json:"events"`
			} `json:"frames"`
		} `json:"info"`
	}
	if json.Unmarshal(bruto, &cru) != nil {
		return []abateRiot{}
	}
	lista := []abateRiot{}
	for _, f := range cru.Info.Frames {
		for _, b := range f.Events {
			var e struct {
				Type       string      `json:"type"`
				Timestamp  int64       `json:"timestamp"`
				KillerID   int         `json:"killerId"`
				VictimID   int         `json:"victimId"`
				Bounty     int         `json:"bounty"`
				KillStreak int         `json:"killStreakLength"`
				Shutdown   int         `json:"shutdownBounty"`
				Recebido   []danoBruto `json:"victimDamageReceived"`
				Causado    []danoBruto `json:"victimDamageDealt"`
			}
			if json.Unmarshal(b, &e) != nil || e.Type != "CHAMPION_KILL" {
				continue
			}
			lista = append(lista, abateRiot{tMs: e.Timestamp, quem: e.KillerID, emQuem: e.VictimID, bounty: e.Bounty,
				killStreak: e.KillStreak, shutdown: e.Shutdown, recebido: converteDano(e.Recebido), causado: converteDano(e.Causado)})
		}
	}
	return lista
}

type danoBruto struct {
	Basic          bool   `json:"basic"`
	MagicDamage    int    `json:"magicDamage"`
	Name           string `json:"name"`
	ParticipantID  int    `json:"participantId"`
	PhysicalDamage int    `json:"physicalDamage"`
	SpellName      string `json:"spellName"`
	SpellSlot      int    `json:"spellSlot"`
	TrueDamage     int    `json:"trueDamage"`
	Type           string `json:"type"`
}

func converteDano(l []danoBruto) []DanoRiot {
	r := []DanoRiot{}
	for _, d := range l {
		h := d.SpellName
		if d.Basic {
			h = "ataque básico"
		}
		r = append(r, DanoRiot{ParticipanteID: d.ParticipantID, Nome: d.Name, Habilidade: h, Slot: d.SpellSlot, Basico: d.Basic,
			Tipo: d.Type, Fisico: d.PhysicalDamage, Magico: d.MagicDamage, Verdadeiro: d.TrueDamage,
			Total: d.PhysicalDamage + d.MagicDamage + d.TrueDamage})
	}
	return r
}

func (d *dadosPartida) riotDoAbate(e rastreio.EventoLT) *RiotNoAbate {
	var melhor *abateRiot
	for i := range d.riot {
		a := &d.riot[i]
		if a.emQuem != e.EmQuem || a.quem != e.Quem {
			continue
		}
		if dt := a.tMs - e.TMs; dt >= -3000 && dt <= 3000 && (melhor == nil || math.Abs(float64(dt)) < math.Abs(float64(melhor.tMs-e.TMs))) {
			melhor = a
		}
	}
	if melhor == nil {
		return nil
	}
	r := &RiotNoAbate{Recebido: melhor.recebido, Causado: melhor.causado, PorJogador: map[int]int{}, Ouro: melhor.bounty,
		Sequencia: melhor.killStreak}
	for _, x := range melhor.recebido {
		if x.ParticipanteID > 0 {
			r.PorJogador[x.ParticipanteID] += x.Total
		}
	}
	return r
}

func leQuadrosRiot(bruto []byte) map[int64]map[int]statsRiot {
	var r struct {
		Info struct {
			Frames []struct {
				Timestamp         int64 `json:"timestamp"`
				ParticipantFrames map[string]struct {
					ParticipantID       int `json:"participantId"`
					MinionsKilled       int `json:"minionsKilled"`
					JungleMinionsKilled int `json:"jungleMinionsKilled"`
					TotalGold           int `json:"totalGold"`
					XP                  int `json:"xp"`
					DamageStats         struct {
						TotalDamageDoneToChampions int `json:"totalDamageDoneToChampions"`
					} `json:"damageStats"`
					ChampionStats struct {
						Health    float64 `json:"health"`
						HealthMax float64 `json:"healthMax"`
						Power     float64 `json:"power"`
						PowerMax  float64 `json:"powerMax"`
					} `json:"championStats"`
				} `json:"participantFrames"`
			} `json:"frames"`
		} `json:"info"`
	}
	m := map[int64]map[int]statsRiot{}
	if json.Unmarshal(bruto, &r) != nil {
		return m
	}
	for _, f := range r.Info.Frames {
		q := map[int]statsRiot{}
		for _, p := range f.ParticipantFrames {
			s := p.ChampionStats
			q[p.ParticipantID] = statsRiot{Health: s.Health, HealthMax: s.HealthMax, Power: s.Power, PowerMax: s.PowerMax,
				MinionsKilled: p.MinionsKilled, JungleMinionsKilled: p.JungleMinionsKilled, TotalGold: p.TotalGold, XP: p.XP,
				DanoCampeoes: p.DamageStats.TotalDamageDoneToChampions}
		}
		m[f.Timestamp] = q
	}
	return m
}

type wardRiot struct {
	tMs  int64
	quem int
	tipo string
}

// leWardsRiot: WARD_PLACED da linha do tempo da Riot (creatorId, wardType).
func leWardsRiot(bruto []byte) []wardRiot {
	var r struct {
		Info struct {
			Frames []struct {
				Events []struct {
					Type      string `json:"type"`
					Timestamp int64  `json:"timestamp"`
					CreatorID int    `json:"creatorId"`
					WardType  string `json:"wardType"`
				} `json:"events"`
			} `json:"frames"`
		} `json:"info"`
	}
	l := []wardRiot{}
	if json.Unmarshal(bruto, &r) != nil {
		return l
	}
	for _, f := range r.Info.Frames {
		for _, e := range f.Events {
			if e.Type == "WARD_PLACED" && e.CreatorID > 0 {
				l = append(l, wardRiot{e.Timestamp, e.CreatorID, e.WardType})
			}
		}
	}
	return l
}

// pertoDeAbate: a posição do CHAMPION_KILL mais perto no tempo (a até 8 s) em
// que pid matou, ajudou ou (depois de t) morreu.
func (d *dadosPartida) pertoDeAbate(pid int, t int64) (float64, float64, bool) {
	melhor := int64(8001)
	var x, y float64
	for _, e := range d.lt.Eventos {
		if e.Tipo != "CHAMPION_KILL" || e.X == nil {
			continue
		}
		dt := e.TMs - t
		if dt < 0 {
			dt = -dt
		}
		if dt >= melhor {
			continue
		}
		if e.Quem == pid || contem(e.Assistencias, pid) || (e.EmQuem == pid && e.TMs > t) {
			melhor, x, y = dt, *e.X, *e.Y
		}
	}
	return x, y, melhor <= 8000
}
