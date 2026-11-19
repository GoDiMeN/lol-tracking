package analise

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"loltracking/server/campeao"
	"loltracking/server/rastreio"
)

// O cálculo da base de fatos sobre os dados já carregados da partida.

// objetivoEv é um objetivo, torre ou inibidor derrubado (linha do tempo do LCU).
type objetivoEv struct {
	t     int64
	tipo  string // objetivo, torre, inibidor
	nome  string // nomeDoAlvo: "dragão infernal", "torre do topo"
	sub   string // DRAGON, BARON_NASHOR... ou a torre (OUTER_TURRET...)
	time  int    // quem pegou/derrubou
	autor int
	// participantes: quem matou e quem ajudou (>0), do evento.
	participantes []int
	x, y          *float64
	valor         int
	rota          string
}

// trechoSimples é um trecho de atividade da cadeia.
type trechoSimples struct {
	ini, fim int64
	atv      string
	alvo     string
	zona     string
}

// infoMorte é o que se calcula de cada abate (retrato n).
type infoMorte struct {
	custo, ouroAbate, negado int
	mortoMs                  int64
	estimado                 bool
	tags                     []string
	cara                     bool
	objetivoDepois           *objetivoEv
	luta                     int // o n da luta (momento) de que faz parte; 0 = nenhuma
}

type fatos struct {
	dp       *dadosPartida
	retratos []Retrato
	est      *campeao.Estaticos
	curva    []PontoOuro
	timeDe   map[int]int
	posDe    map[int]string
	objs     []objetivoEv
	placas   map[int]int // por time que derrubou
	trechos  map[int][]trechoSimples
	mortes   map[int]*infoMorte // por n do retrato
	ms       []MomentoChave
	// selva: os tempos dos acampamentos e épicos do patch da partida.
	selva        TemposSelvaPatch
	selvaCoberta bool
}

func novoFatos(dp *dadosPartida, retratos []Retrato, est *campeao.Estaticos) *fatos {
	f := &fatos{dp: dp, retratos: retratos, est: est, timeDe: map[int]int{}, posDe: map[int]string{}, placas: map[int]int{},
		trechos: map[int][]trechoSimples{}, mortes: map[int]*infoMorte{}}
	f.selva, f.selvaCoberta = TemposDaSelva(dp.pt.Patch)
	for _, p := range dp.pt.Participantes {
		f.timeDe[p.ParticipanteID] = p.Time
		f.posDe[p.ParticipanteID] = p.Posicao
	}
	f.curva = curvaDeOuro(dp.lt, f.timeDe)
	f.objs = f.objetivosDaLinha()
	for _, e := range dp.eventosRiot {
		if e.Tipo != "TURRET_PLATE_DESTROYED" {
			continue
		}
		var b struct {
			Time int `json:"teamId"`
		}
		json.Unmarshal(e.Bruto, &b)
		if b.Time == 100 || b.Time == 200 {
			f.placas[300-b.Time]++
		}
	}
	for pid, cad := range dp.cadeias {
		for _, it := range cad {
			if it.Tipo != "atividade" {
				continue
			}
			t := trechoSimples{ini: it.TMs, fim: int64(numero(it.Dados["fim_ms"]))}
			t.atv, _ = it.Dados["atividade"].(string)
			t.alvo, _ = it.Dados["alvo"].(string)
			t.zona, _ = it.Dados["zona"].(string)
			f.trechos[pid] = append(f.trechos[pid], t)
		}
		sort.Slice(f.trechos[pid], func(a, b int) bool { return f.trechos[pid][a].ini < f.trechos[pid][b].ini })
	}
	for _, rt := range retratos {
		f.mortes[rt.N] = f.calculaMorte(rt)
	}
	return f
}

// --- Ouro ------------------------------------------------------------------------------

// curvaDeOuro: o ouro total de cada time em cada quadro de minuto.
func curvaDeOuro(lt rastreio.LinhaDoTempo, timeDe map[int]int) []PontoOuro {
	c := []PontoOuro{}
	for _, q := range lt.Quadros {
		p := PontoOuro{TMs: q.TMs}
		for pid, j := range q.Jogadores {
			switch timeDe[pid] {
			case 100:
				p.Azul += j.OuroTotal
			case 200:
				p.Vermelho += j.OuroTotal
			}
		}
		p.Diferenca = p.Azul - p.Vermelho
		c = append(c, p)
	}
	sort.Slice(c, func(a, b int) bool { return c[a].TMs < c[b].TMs })
	return c
}

// difEm: a vantagem de ouro (azul − vermelho) em t, interpolada entre os quadros.
func difEm(c []PontoOuro, t int64) (int, bool) {
	if len(c) == 0 {
		return 0, false
	}
	if t <= c[0].TMs {
		return c[0].Diferenca, true
	}
	for i := 1; i < len(c); i++ {
		if c[i].TMs >= t {
			a, b := c[i-1], c[i]
			fr := float64(t-a.TMs) / float64(max(1, b.TMs-a.TMs))
			return int(math.Round(float64(a.Diferenca) + float64(b.Diferenca-a.Diferenca)*fr)), true
		}
	}
	return c[len(c)-1].Diferenca, true
}

// viradaDeOuro: a maior vantagem de cada lado e quantas vezes o time na frente
// trocou (vantagem de pelo menos 1 000 para valer como "na frente").
func viradaDeOuro(c []PontoOuro) (maxAz int, tAz int64, maxVe int, tVe int64, viradas int) {
	lider := 0
	for _, p := range c {
		if p.Diferenca > maxAz {
			maxAz, tAz = p.Diferenca, p.TMs
		}
		if -p.Diferenca > maxVe {
			maxVe, tVe = -p.Diferenca, p.TMs
		}
		agora := 0
		switch {
		case p.Diferenca >= 1000:
			agora = 100
		case p.Diferenca <= -1000:
			agora = 200
		}
		if agora != 0 {
			if lider != 0 && agora != lider {
				viradas++
			}
			lider = agora
		}
	}
	return
}

// valorJogadorEm interpola um valor dos quadros de minuto (ouro total ou CS)
// do participante em t.
func (f *fatos) valorJogadorEm(pid int, t int64, campo func(rastreio.JogadorLT) int) (float64, bool) {
	q := f.dp.lt.Quadros
	for i := 1; i < len(q); i++ {
		if q[i].TMs >= t {
			a, okA := q[i-1].Jogadores[pid]
			b, okB := q[i].Jogadores[pid]
			if !okA || !okB {
				return 0, false
			}
			fr := float64(t-q[i-1].TMs) / float64(max(1, q[i].TMs-q[i-1].TMs))
			if fr < 0 {
				fr = 0
			}
			return float64(campo(a)) + float64(campo(b)-campo(a))*fr, true
		}
	}
	if n := len(q); n > 0 {
		if j, ok := q[n-1].Jogadores[pid]; ok {
			return float64(campo(j)), true
		}
	}
	return 0, false
}

func ouroTotal(j rastreio.JogadorLT) int { return j.OuroTotal }
func csTotal(j rastreio.JogadorLT) int   { return j.CS }

// renasce: quando o jogador volta a viver depois de morrer em t, pelo estado
// dos 10 (todos os passes). Sem estado perto: t + MorteEstimadaMs (estimado).
func (f *fatos) renasce(pid int, t int64) (int64, bool) {
	es := f.dp.estados[pid]
	i := sort.Search(len(es), func(k int) bool { return es[k].TMs > t })
	viuMorto := false
	for k := i; k < len(es) && es[k].TMs <= t+150000; k++ {
		if k > i && es[k].TMs-es[k-1].TMs > 5000 {
			break
		}
		if es[k].Morto {
			viuMorto = true
			continue
		}
		if viuMorto {
			return es[k].TMs, false
		}
		if es[k].TMs > t+5000 {
			break
		}
	}
	return t + Param.MorteEstimadaMs, true
}

// --- Atividade --------------------------------------------------------------------------

// atividadeEm: a etiqueta do jogador em t (rastreados; "" sem trecho).
func (f *fatos) atividadeEm(pid int, t int64) trechoSimples {
	for _, tr := range f.trechos[pid] {
		if tr.ini <= t && t < tr.fim {
			return tr
		}
	}
	return trechoSimples{}
}

// briga: etiquetas de briga ou morte (a atividade prévia passa por cima delas).
func briga(a string) bool {
	return a == ALutando || a == ATrocando || a == ADive || a == AMorto || a == ""
}

// atividadePrevia: a última etiqueta fora de briga até JanelaAtividadePreviaMs
// antes de t (o que ele estava fazendo antes de a luta começar).
func (f *fatos) atividadePrevia(pid int, t int64) string {
	ts := f.trechos[pid]
	for i := len(ts) - 1; i >= 0; i-- {
		tr := ts[i]
		if tr.ini >= t {
			continue
		}
		if tr.fim < t-Param.JanelaAtividadePreviaMs {
			break
		}
		if !briga(tr.atv) {
			return tr.atv
		}
	}
	return ""
}

// tempoPorAtividade soma as etiquetas do jogador em [de, ate).
func (f *fatos) tempoPorAtividade(pid int, de, ate int64) map[string]int64 {
	m := map[string]int64{}
	for _, tr := range f.trechos[pid] {
		a, b := max(tr.ini, de), min(tr.fim, ate)
		if b > a {
			m[tr.atv] += b - a
		}
	}
	return m
}

// --- Objetivos ----------------------------------------------------------------------------

func (f *fatos) objetivosDaLinha() []objetivoEv {
	var l []objetivoEv
	rotaDe := map[string]string{"TOP_LANE": ZRotaTopo, "MID_LANE": ZRotaMeio, "BOT_LANE": ZRotaBaixo}
	for _, e := range f.dp.lt.Eventos {
		if e.Tipo != "ELITE_MONSTER_KILL" && e.Tipo != "BUILDING_KILL" {
			continue
		}
		var b struct {
			Monstro   string `json:"monsterType"`
			Sub       string `json:"monsterSubType"`
			Estrutura string `json:"buildingType"`
			Torre     string `json:"towerType"`
			Rota      string `json:"laneType"`
			Dono      int    `json:"teamId"`
			Matador   int    `json:"killerTeamId"`
		}
		json.Unmarshal(e.Bruto, &b)
		o := objetivoEv{t: e.TMs, nome: nomeDoAlvo(e), autor: e.Quem, x: e.X, y: e.Y, rota: rotaDe[b.Rota]}
		if e.Quem > 0 {
			o.participantes = append(o.participantes, e.Quem)
		}
		for _, a := range e.Assistencias {
			if a > 0 && !contem(o.participantes, a) {
				o.participantes = append(o.participantes, a)
			}
		}
		if e.Tipo == "BUILDING_KILL" {
			if b.Dono != 100 && b.Dono != 200 {
				continue
			}
			o.time = 300 - b.Dono
			o.sub = b.Torre
			if b.Estrutura == "INHIBITOR_BUILDING" {
				o.tipo, o.valor, o.sub = "inibidor", Param.OuroInibidor, "INHIBITOR"
			} else {
				o.tipo, o.valor = "torre", Param.OuroTorre
			}
			// Na estrutura derrubada só vale quem é do time que derrubou (a tropa
			// e o executado não contam como participante).
			var ps []int
			for _, p := range o.participantes {
				if f.timeDe[p] == o.time {
					ps = append(ps, p)
				}
			}
			o.participantes = ps
		} else {
			o.tipo, o.sub = "objetivo", b.Monstro
			switch b.Monstro {
			case "DRAGON":
				o.valor = Param.OuroDragao
				if b.Sub == "ELDER_DRAGON" {
					o.valor = Param.OuroAnciao
				}
				o.sub = "DRAGON:" + b.Sub
			case "BARON_NASHOR":
				o.valor = Param.OuroBarao
			case "RIFTHERALD":
				o.valor = Param.OuroArauto
			case "HORDE":
				o.valor = Param.OuroVastilarva
			case "ATAKHAN":
				o.valor = Param.OuroAtakhan
			default:
				o.valor = Param.OuroVastilarva
			}
			// O time de quem matou; sem matador (killerId 0, como nas nuvens da
			// 3304217746), o da maioria de quem ajudou; senão o killerTeamId.
			o.time = f.timeDe[e.Quem]
			if o.time == 0 {
				conta := map[int]int{}
				for _, a := range e.Assistencias {
					conta[f.timeDe[a]]++
				}
				if conta[100] > conta[200] {
					o.time = 100
				} else if conta[200] > conta[100] {
					o.time = 200
				}
			}
			if o.time == 0 && (b.Matador == 100 || b.Matador == 200) {
				o.time = b.Matador
			}
			if o.time == 0 {
				continue
			}
			// A linha do tempo põe em "ajudou" quem bateu no monstro dos DOIS times
			// (a Vi roubando o infernal aos 24:25 com o Shaco na lista): só conta
			// quem é do time que pegou.
			var ps []int
			for _, p := range o.participantes {
				if f.timeDe[p] == o.time {
					ps = append(ps, p)
				}
			}
			o.participantes = ps
		}
		l = append(l, o)
	}
	return l
}

func (f *fatos) times() []ResumoTime {
	var r []ResumoTime
	for _, tm := range []int{100, 200} {
		t := ResumoTime{Time: tm, Venceu: f.dp.pt.TimeVencedor == tm, Dragoes: []string{}, Placas: f.placas[tm]}
		for _, rt := range f.retratos {
			if f.timeDe[rt.Vitima] == 300-tm {
				t.Abates++
			}
		}
		if n := len(f.curva); n > 0 {
			t.OuroFinal = f.curva[n-1].Azul
			if tm == 200 {
				t.OuroFinal = f.curva[n-1].Vermelho
			}
		}
		for _, o := range f.objs {
			if o.time != tm {
				continue
			}
			switch {
			case o.tipo == "torre":
				t.Torres++
			case o.tipo == "inibidor":
				t.Inibidores++
			case o.sub == "DRAGON:ELDER_DRAGON":
				t.Anciao++
			case strings.HasPrefix(o.sub, "DRAGON"):
				t.Dragoes = append(t.Dragoes, o.nome)
			case o.sub == "BARON_NASHOR":
				t.Baroes++
			case o.sub == "RIFTHERALD":
				t.Arautos++
			case o.sub == "HORDE":
				t.Vastilarvas++
			case o.sub == "ATAKHAN":
				t.Atakhan++
			}
		}
		r = append(r, t)
	}
	return r
}

// --- Mortes --------------------------------------------------------------------------------

// ouroDoAbate: bounty + shutdown da Riot para o abate; sem a Riot, o padrão.
func (f *fatos) ouroDoAbate(rt Retrato) (int, bool) {
	var melhor *abateRiot
	for i := range f.dp.riot {
		a := &f.dp.riot[i]
		if a.emQuem != rt.Vitima || a.quem != rt.Matador {
			continue
		}
		if dt := a.tMs - rt.TMs; dt >= -3000 && dt <= 3000 && (melhor == nil || absI(dt) < absI(melhor.tMs-rt.TMs)) {
			melhor = a
		}
	}
	if melhor == nil {
		return Param.OuroAbatePadrao, true
	}
	return melhor.bounty + melhor.shutdown, false
}

// calculaMorte: o custo da morte para o time da vítima (ouro do abate + o que
// ela deixou de ganhar morta) e as marcas de "morte cara".
func (f *fatos) calculaMorte(rt Retrato) *infoMorte {
	im := &infoMorte{}
	im.ouroAbate, im.estimado = f.ouroDoAbate(rt)
	volta, est := f.renasce(rt.Vitima, rt.TMs)
	im.mortoMs = volta - rt.TMs
	im.estimado = im.estimado || est
	// Ritmo de ouro dos minutos antes (ouro passivo incluso: é o que ele perde).
	de := max(0, rt.TMs-Param.RitmoOuroJanelaMs)
	if a, okA := f.valorJogadorEm(rt.Vitima, de, ouroTotal); okA {
		if b, okB := f.valorJogadorEm(rt.Vitima, rt.TMs, ouroTotal); okB && rt.TMs > de {
			im.negado = int(math.Round((b - a) / float64(rt.TMs-de) * float64(im.mortoMs)))
		}
	}
	im.custo = im.ouroAbate + max(0, im.negado)
	var eu *JogadorNoAbate
	for i := range rt.Jogadores {
		if rt.Jogadores[i].ParticipanteID == rt.Vitima {
			eu = &rt.Jogadores[i]
		}
	}
	if rt.AliadoMaisProximo != nil && *rt.AliadoMaisProximo > Param.DistanciaSozinho {
		im.tags = append(im.tags, "sozinho")
		im.cara = true
	}
	if rt.Superioridade > 0 {
		im.tags = append(im.tags, "inferioridade")
	}
	// Sem troca: o time da vítima não matou ninguém na mesma briga (até
	// JanelaLutaTimeMs antes ou depois).
	tv := f.timeDe[rt.Vitima]
	semTroca := true
	for _, o := range f.retratos {
		if o.TMs >= rt.TMs-Param.JanelaLutaTimeMs && o.TMs <= rt.TMs+Param.JanelaLutaTimeMs && f.timeDe[o.Vitima] == 300-tv {
			semTroca = false
			break
		}
	}
	if semTroca {
		im.tags = append(im.tags, "sem_troca")
	}
	recarga := false
	if eu != nil {
		if eu.RDisponivel != nil && !*eu.RDisponivel {
			im.tags = append(im.tags, "r_em_recarga")
			recarga = true
		}
		if eu.FlashDisponivel != nil && !*eu.FlashDisponivel {
			im.tags = append(im.tags, "flash_em_recarga")
			recarga = true
		}
		if eu.PassivaDisponivel != nil && !*eu.PassivaDisponivel && f.passivaComRecarga(rt.Vitima) {
			im.tags = append(im.tags, "passiva_em_recarga")
			recarga = true
		}
		if eu.ForaDaRotaMs != nil {
			im.tags = append(im.tags, "fora_da_rota")
		}
	}
	// Recarga sozinha não faz a morte cara: o R e o Flash estavam em recarga em
	// ~90% das mortes da 3304217746 (gasto na própria luta). Conta quando a
	// morte foi sem troca (morreu sem as ferramentas e não levou ninguém).
	if recarga && semTroca {
		im.cara = true
	}
	// O inimigo levou objetivo ou torre logo depois (até JanelaMorteObjetivoMs).
	for i := range f.objs {
		o := &f.objs[i]
		if o.time == 300-tv && o.t >= rt.TMs && o.t <= rt.TMs+Param.JanelaMorteObjetivoMs {
			im.objetivoDepois = o
			im.tags = append(im.tags, "antes_de_objetivo")
			im.cara = true
			im.custo += int(math.Round(float64(o.valor) * Param.PesoObjetivoNaMorte))
			break
		}
	}
	return im
}

// passivaComRecarga: a passiva do campeão entrou em recarga alguma vez no
// passe (na maioria dos campeões o ícone nunca apaga).
func (f *fatos) passivaComRecarga(pid int) bool {
	for i := range f.dp.amostras[pid] {
		if p := f.dp.amostras[pid][i].CD.P; p != nil && *p > 0 {
			return true
		}
	}
	return false
}

// --- Nomes --------------------------------------------------------------------------------

func (f *fatos) campeao(pid int) string {
	if p := f.dp.participante(pid); p != nil && p.Campeao != "" {
		return p.Campeao
	}
	return fmt.Sprintf("jogador %d", pid)
}

// quem: "Shaco (caçador azul)".
func (f *fatos) quem(pid int) string {
	return fmt.Sprintf("%s (%s %s)", f.campeao(pid), NomeFuncao(f.posDe[pid]), NomeTime(f.timeDe[pid]))
}

func (f *fatos) nomes(ps []int) string {
	var l []string
	for _, p := range ps {
		l = append(l, f.campeao(p))
	}
	return juntaE(l)
}

// juntaE: "a, b e c".
func juntaE(l []string) string {
	switch len(l) {
	case 0:
		return ""
	case 1:
		return l[0]
	}
	return strings.Join(l[:len(l)-1], ", ") + " e " + l[len(l)-1]
}

func nomeZona(z string) string {
	if n := Info(z).Nome; n != "" {
		return n
	}
	return z
}

var nomesAtividade = map[string]string{AMorto: "morto", ANaBase: "na base", ALutando: "lutando", ATrocando: "trocando",
	AObjetivo: "em objetivo", ATorre: "em torre", AFarmando: "farmando", AColetando: "coletando ouro", AGankando: "gankando",
	ACounterGank: "em counter gank", ARoaming: "em roaming", ARotacionando: "rotacionando", AAndando: "andando", AParado: "parado",
	ARecall: "em recall", ADive: "em dive", AInvadindo: "invadindo", ASplitPush: "em split push", AAgrupado: "agrupado"}

// NomeAtividade: "gankando" → "gankando"; "na_base" → "na base".
func NomeAtividade(a string) string {
	if n, ok := nomesAtividade[a]; ok {
		return n
	}
	return a
}

func segundosTexto(ms int64) string {
	s := (ms + 500) / 1000
	if s >= 60 {
		if s%60 == 0 {
			return fmt.Sprintf("%d min", s/60)
		}
		return fmt.Sprintf("%d min %d s", s/60, s%60)
	}
	return fmt.Sprintf("%d s", s)
}
