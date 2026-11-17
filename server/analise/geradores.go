package analise

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"loltracking/server/rastreio"
)

// Os geradores da análise acrescentam à cadeia reduzida (rastreio.RegistraGerador):
// mudou_zona, saiu_da_rota/voltou_para_rota (rotador), entrou_em_rota
// (caçador), na_base e atividade. Vocabulário em docs/arquitetura/dados.md.

func init() {
	rastreio.RegistraGerador("analise_zona", geraMudouZona)
	rastreio.RegistraGerador("analise_rota", geraRota)
	rastreio.RegistraGerador("analise_base", geraNaBase)
	rastreio.RegistraGerador("analise_atividade", geraAtividade)
}

// ExtraEventosRiot: chave de ContextoCadeia.Extra com os eventos da linha do
// tempo da Riot ([]rastreio.EventoLT), quando guardada.
const ExtraEventosRiot = "eventos_riot"

func arr(v float64) int64 { return int64(math.Round(v)) }

func geraMudouZona(c *rastreio.ContextoCadeia) []rastreio.ItemCadeia {
	p := prepara(c)
	var itens []rastreio.ItemCadeia
	ant := ""
	for _, t := range p.trechos {
		if t.morto {
			continue
		}
		if ant != "" && t.zona != ant {
			z := p.zonas[t.iIni]
			d := map[string]any{"de": ant, "para": t.zona, "subzona": t.sub}
			if z.temPonto {
				d["x"], d["y"] = arr(z.ponto.X), arr(z.ponto.Y)
			}
			itens = append(itens, rastreio.ItemCadeia{TMs: t.ini, Tipo: "mudou_zona", Dados: d})
		}
		ant = t.zona
	}
	return itens
}

// aconteceu lista o que o jogador fez/sofreu em [ini, fim]: abate,
// assistencia, morte, objetivo (monstro ou estrutura em que ele participou, ou
// do time dele a até RaioObjetivo de onde ele estava).
func (p *preparo) aconteceu(ini, fim int64) (lista []map[string]any, desfecho string) {
	pid := p.c.ParticipanteID
	tem := map[string]bool{}
	for _, e := range p.eventos {
		if e.TMs < ini || e.TMs > fim {
			continue
		}
		var tipo string
		switch e.Tipo {
		case "CHAMPION_KILL":
			switch {
			case e.EmQuem == pid:
				tipo = "morte"
			case e.Quem == pid:
				tipo = "abate"
			case contem(e.Assistencias, pid):
				tipo = "assistencia"
			}
		case "ELITE_MONSTER_KILL", "BUILDING_KILL", "TURRET_PLATE_DESTROYED":
			if e.Quem == pid || contem(e.Assistencias, pid) {
				tipo = "objetivo"
			} else if e.X != nil && e.Quem > 0 && p.c.Times[e.Quem] == p.c.Time {
				if pos, ok := p.posicaoDe(pid, e.TMs); ok && dist(pos, Ponto{*e.X, *e.Y}) < Param.RaioObjetivo {
					tipo = "objetivo"
				}
			}
		}
		if tipo == "" {
			continue
		}
		tem[tipo] = true
		it := map[string]any{"tipo": tipo, "t_ms": e.TMs, "evento": strings.ToLower(e.Tipo)}
		if e.Tipo == "CHAMPION_KILL" {
			it["vitima"], it["matador"] = e.EmQuem, e.Quem
		}
		lista = append(lista, it)
	}
	switch {
	case tem["morte"]:
		desfecho = "morte"
	case tem["abate"] || tem["assistencia"]:
		desfecho = "abate"
	case tem["objetivo"]:
		desfecho = "objetivo"
	default:
		desfecho = "nada"
	}
	if lista == nil {
		lista = []map[string]any{}
	}
	return lista, desfecho
}

// csPerdidoEstimado: ritmo de CS dos 2 min (linha do tempo) antes da saída ×
// duração. Só na fase de rotas; grosseiro (não desconta o CS feito fora).
func (p *preparo) csPerdidoEstimado(ini, dur int64) *float64 {
	if ini >= Param.FaseRotasMs {
		return nil
	}
	pid := p.c.ParticipanteID
	q := p.c.Linha.Quadros
	k := sort.Search(len(q), func(i int) bool { return q[i].TMs > ini }) - 1
	if k < 2 {
		return nil
	}
	a, okA := q[k-2].Jogadores[pid]
	b, okB := q[k].Jogadores[pid]
	if !okA || !okB {
		return nil
	}
	ritmo := float64(b.CS-a.CS) / (float64(q[k].TMs-q[k-2].TMs) / 60000)
	v := math.Round(ritmo*float64(dur)/60000*10) / 10
	return &v
}

// geraRota: para o rotador, as saídas da rota; para o caçador, as entradas em
// rota (gank/visita).
func geraRota(c *rastreio.ContextoCadeia) []rastreio.ItemCadeia {
	p := prepara(c)
	if c.Posicao == "" {
		return nil
	}
	if p.rota == "" {
		return p.entradasEmRota()
	}
	return p.saidasDaRota()
}

// basePropria é a zona da base do time do jogador.
func (p *preparo) basePropria() string {
	if p.c.Time == 200 {
		return ZBaseVermelha
	}
	return ZBaseAzul
}

func (p *preparo) saidasDaRota() []rastreio.ItemCadeia {
	var itens []rastreio.ItemCadeia
	ts := p.trechos
	for k := 0; k < len(ts); k++ {
		t := ts[k]
		// Uma saída começa num trecho vivo fora da rota logo depois de um
		// trecho na rota.
		if t.morto || t.zona == p.rota || t.zona == p.basePropria() || k == 0 || ts[k-1].morto || ts[k-1].zona != p.rota {
			continue
		}
		ini := t.ini
		fim, motivo := t.fim, "fim_do_passe"
		var zonas []string
		j := k
		for ; j < len(ts); j++ {
			u := ts[j]
			if u.morto {
				fim, motivo = u.ini, "morreu"
				break
			}
			if u.zona == p.rota {
				fim, motivo = u.ini, "voltou"
				break
			}
			if u.zona == p.basePropria() {
				fim, motivo = u.ini, "base"
				break
			}
			if !contemS(zonas, u.zona) {
				zonas = append(zonas, u.zona)
			}
			fim = u.fim
		}
		dur := fim - ini
		k = j - 1
		if dur < Param.SaidaMinimaMs && motivo != "morreu" {
			continue
		}
		ac, desfecho := p.aconteceu(ini, fim+2000)
		// Ir andando para a base sem nada acontecer não é sair para jogar.
		conta := !(motivo == "base" && desfecho == "nada")
		antes := ini - 500
		d := map[string]any{
			"rota": p.rota, "para": zonas[0], "zonas": zonas, "fim_ms": fim, "duracao_ms": dur, "fim_motivo": motivo,
			"fase": Fase(ini), "fase_rotas": ini < Param.FaseRotasMs, "vida_pct": p.vidaPctEm(antes), "mana_pct": p.manaPctEm(antes),
			"cs_perdido_est": p.csPerdidoEstimado(ini, dur), "aconteceu": ac, "desfecho": desfecho, "conta": conta,
		}
		if z := p.zonas[t.iIni]; z.temPonto {
			d["x"], d["y"] = arr(z.ponto.X), arr(z.ponto.Y)
		}
		itens = append(itens, rastreio.ItemCadeia{TMs: ini, Tipo: "saiu_da_rota", Dados: d})
		if motivo == "voltou" {
			itens = append(itens, rastreio.ItemCadeia{TMs: fim, Tipo: "voltou_para_rota", Dados: map[string]any{
				"rota": p.rota, "inicio_ms": ini, "duracao_ms": dur}})
		}
	}
	return itens
}

func contemS(l []string, v string) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}

func (p *preparo) entradasEmRota() []rastreio.ItemCadeia {
	var itens []rastreio.ItemCadeia
	for _, t := range p.trechos {
		if t.morto || TipoZona(t.zona) != "rota" {
			continue
		}
		dur := t.fim - t.ini
		if dur < Param.SaidaMinimaMs {
			continue
		}
		ac, desfecho := p.aconteceu(t.ini, t.fim+2000)
		d := map[string]any{"rota": t.zona, "trecho": t.sub, "fim_ms": t.fim, "duracao_ms": dur, "fase": Fase(t.ini),
			"fase_rotas": t.ini < Param.FaseRotasMs, "vida_pct": p.vidaPctEm(t.ini - 500), "aconteceu": ac, "desfecho": desfecho}
		if z := p.zonas[t.iIni]; z.temPonto {
			d["x"], d["y"] = arr(z.ponto.X), arr(z.ponto.Y)
		}
		itens = append(itens, rastreio.ItemCadeia{TMs: t.ini, Tipo: "entrou_em_rota", Dados: d})
	}
	return itens
}

// geraNaBase: cada estadia viva na própria base, com como chegou.
func geraNaBase(c *rastreio.ContextoCadeia) []rastreio.ItemCadeia {
	p := prepara(c)
	var itens []rastreio.ItemCadeia
	for k, t := range p.trechos {
		if t.morto || t.zona != p.basePropria() {
			continue
		}
		chegada := "andando"
		switch {
		case k == 0:
			chegada = "inicio"
		case p.trechos[k-1].morto:
			chegada = "renasceu"
		default:
			// Recall: a posição salta mais de 3000 em menos de 3 s logo antes.
			for i := t.iIni; i > 0 && i > t.iIni-40; i-- {
				a, b := p.zonas[i-1], p.zonas[i]
				if a.temPonto && b.temPonto && a.vivo && b.vivo && b.tMs-a.tMs < 3000 && dist(a.ponto, b.ponto) > 3000 {
					chegada = "recall"
					break
				}
			}
		}
		itens = append(itens, rastreio.ItemCadeia{TMs: t.ini, Tipo: "na_base", Dados: map[string]any{
			"fim_ms": t.fim, "duracao_ms": t.fim - t.ini, "chegada": chegada}})
	}
	return itens
}

// --- atividade ---------------------------------------------------------------

// Atividades (vocabulário).
const (
	AMorto    = "morto"
	ANaBase   = "na_base"
	ALutando  = "lutando"
	ATrocando = "trocando"
	AObjetivo = "objetivo"
	ATorre    = "torre"
	AFarmando = "farmando"
	AAndando  = "andando"
)

// Atividades é o vocabulário, na ordem de prioridade da regra.
var Atividades = []string{AMorto, ANaBase, ALutando, ATrocando, AObjetivo, ATorre, AFarmando, AAndando}

type rotuloAtv struct {
	atv       string
	com       []int
	sinais    []string
	confianca string
	zona      string
}

func (r rotuloAtv) chave() string { return r.atv + fmt.Sprint(r.com) }

// classifica decide a atividade de uma amostra (a regra; docs/decisoes).
func (p *preparo) classifica(k int) rotuloAtv {
	c := p.c
	z := p.zonas[k]
	t := z.tMs
	if !z.vivo {
		return rotuloAtv{atv: AMorto, confianca: "alta", sinais: []string{"morto no jogo"}}
	}
	if !z.temZona {
		return rotuloAtv{atv: AAndando, confianca: "baixa", sinais: []string{"sem posição"}}
	}
	if z.zona == p.basePropria() {
		return rotuloAtv{atv: ANaBase, confianca: "alta", sinais: []string{"na base"}, zona: z.zona}
	}
	// Quem está perto (só quem foi rastreado tem posição de verdade).
	type perto struct {
		pid int
		d   float64
	}
	var inim, alia []perto
	for _, pid := range p.inimigos {
		if q, ok := p.posicaoDe(pid, t); ok {
			if d := dist(z.ponto, q); d <= Param.AlcanceLuta {
				inim = append(inim, perto{pid, d})
			}
		}
	}
	for _, pid := range p.aliados {
		if q, ok := p.posicaoDe(pid, t); ok {
			if d := dist(z.ponto, q); d <= Param.AlcanceLuta {
				alia = append(alia, perto{pid, d})
			}
		}
	}
	sort.Slice(inim, func(a, b int) bool { return inim[a].d < inim[b].d })
	queda, temVida := p.quedaDeVida(c.ParticipanteID, t, Param.JanelaVidaMs)
	euCaindo := temVida && queda >= Param.QuedaVidaPct
	if len(inim) > 0 {
		var sinais []string
		alguemCaindo := euCaindo
		if euCaindo {
			sinais = append(sinais, fmt.Sprintf("vida caindo %.0f%% em %.0f s", queda, float64(Param.JanelaVidaMs)/1000))
		}
		envolvidos := []int{}
		for _, e := range inim {
			envolvidos = append(envolvidos, e.pid)
			sinais = append(sinais, fmt.Sprintf("inimigo %d a %.0f", e.pid, e.d))
			if q, ok := p.quedaDeVida(e.pid, t, Param.JanelaVidaMs); ok && q >= Param.QuedaVidaPct {
				alguemCaindo = true
				sinais = append(sinais, fmt.Sprintf("vida do %d caindo %.0f%%", e.pid, q))
			}
		}
		for _, a := range alia {
			sinais = append(sinais, fmt.Sprintf("aliado %d a %.0f", a.pid, a.d))
		}
		if alguemCaindo {
			conf := "media"
			if temVida {
				conf = "alta"
			}
			if len(inim) >= 2 || len(alia) >= 1 {
				com := append([]int(nil), envolvidos...)
				for _, a := range alia {
					com = append(com, a.pid)
				}
				return rotuloAtv{atv: ALutando, com: com, sinais: sinais, confianca: conf, zona: z.zona}
			}
			if inim[0].d <= Param.AlcanceTroca {
				return rotuloAtv{atv: ATrocando, com: []int{inim[0].pid}, sinais: sinais, confianca: conf, zona: z.zona}
			}
		}
	}
	// Vida caindo forte fora da rota sem inimigo rastreado perto: troca com
	// alguém que não foi rastreado (só para rotador: o caçador apanha dos
	// monstros da selva o tempo todo; na rota quem bate pode ser a tropa).
	if euCaindo && queda >= 2*Param.QuedaVidaPct && p.rota != "" && TipoZona(z.zona) != "rota" {
		return rotuloAtv{atv: ATrocando, com: []int{}, confianca: "baixa", zona: z.zona, sinais: []string{
			fmt.Sprintf("vida caindo %.0f%% em %.0f s fora da rota", queda, float64(Param.JanelaVidaMs)/1000),
			"nenhum inimigo rastreado perto (quem bateu não foi rastreado)"}}
	}
	// Objetivo/torre: perto de onde um caiu, até JanelaObjetivoMs antes.
	for _, e := range p.eventos {
		if e.TMs < t || e.TMs > t+Param.JanelaObjetivoMs || e.X == nil {
			continue
		}
		var atv string
		switch e.Tipo {
		case "ELITE_MONSTER_KILL":
			atv = AObjetivo
		case "BUILDING_KILL", "TURRET_PLATE_DESTROYED":
			atv = ATorre
			if e.TMs > t+Param.JanelaObjetivoMs/2 {
				continue // torre cai mais rápido que monstro grande
			}
		default:
			continue
		}
		// Só objetivo do próprio time (ou de que ele participou): perto da
		// torre dele caindo ele está defendendo, não derrubando.
		if e.Quem != c.ParticipanteID && !contem(e.Assistencias, c.ParticipanteID) && (e.Quem <= 0 || c.Times[e.Quem] != c.Time) {
			continue
		}
		if d := dist(z.ponto, Ponto{*e.X, *e.Y}); d <= Param.RaioObjetivo {
			return rotuloAtv{atv: atv, confianca: "media", zona: z.zona, sinais: []string{
				fmt.Sprintf("%s a %.0f, %.0f s depois", strings.ToLower(e.Tipo), d, float64(e.TMs-t)/1000)}}
		}
	}
	tipo := TipoZona(z.zona)
	if tipo == "rota" || tipo == "selva" || tipo == "rio" {
		if r, ok := csPorMinuto(c.Linha, c.ParticipanteID, t); ok && r >= Param.CSMinimoPorMin {
			conf := "media"
			if len(c.Outros) == 0 {
				// Sem ninguém rastreado não dá para dizer que não havia inimigo perto.
				conf = "baixa"
			}
			return rotuloAtv{atv: AFarmando, confianca: conf, zona: z.zona, sinais: []string{
				fmt.Sprintf("CS %.0f/min no minuto (linha do tempo)", r), "nenhum inimigo rastreado perto"}}
		}
	}
	return rotuloAtv{atv: AAndando, confianca: "baixa", zona: z.zona, sinais: []string{"nenhum outro sinal"}}
}

type trechoAtv struct {
	ini, fim int64
	r        rotuloAtv
}

func geraAtividade(c *rastreio.ContextoCadeia) []rastreio.ItemCadeia {
	p := prepara(c)
	var ts []trechoAtv
	for k := range p.zonas {
		r := p.classifica(k)
		t := p.zonas[k].tMs
		if n := len(ts); n > 0 && ts[n-1].r.chave() == r.chave() && t-ts[n-1].fim <= 3000 {
			ts[n-1].fim = t
			ts[n-1].r.sinais = juntaSinais(ts[n-1].r.sinais, r.sinais)
			if r.confianca == "alta" {
				ts[n-1].r.confianca = "alta"
			}
			continue
		}
		if n := len(ts); n > 0 {
			ts[n-1].fim = t
		}
		ts = append(ts, trechoAtv{ini: t, fim: t, r: r})
	}
	// Trecho curto de andando/farmando é absorvido pelo anterior.
	var lim []trechoAtv
	for _, t := range ts {
		curto := t.fim-t.ini < Param.TrechoMinimoMs && (t.r.atv == AAndando || t.r.atv == AFarmando)
		if n := len(lim); n > 0 && (curto || lim[n-1].r.chave() == t.r.chave()) {
			lim[n-1].fim = t.fim
			continue
		}
		lim = append(lim, t)
	}
	// O primeiro, se curto, vai para o seguinte.
	if len(lim) > 1 && lim[0].fim-lim[0].ini < Param.TrechoMinimoMs && (lim[0].r.atv == AAndando || lim[0].r.atv == AFarmando) {
		lim[1].ini = lim[0].ini
		lim = lim[1:]
	}
	itens := make([]rastreio.ItemCadeia, 0, len(lim))
	for _, t := range lim {
		com := t.r.com
		if com == nil {
			com = []int{}
		}
		sinais := t.r.sinais
		if len(sinais) > 6 {
			sinais = sinais[:6]
		}
		itens = append(itens, rastreio.ItemCadeia{TMs: t.ini, Tipo: "atividade", Dados: map[string]any{
			"atividade": t.r.atv, "fim_ms": t.fim, "duracao_ms": t.fim - t.ini, "com": com, "zona": t.r.zona,
			"sinais": sinais, "confianca": t.r.confianca}})
	}
	return itens
}

// juntaSinais guarda os sinais distintos pelo começo (sem os números que
// mudam a cada amostra), no máximo 6.
func juntaSinais(a, b []string) []string {
	visto := map[string]bool{}
	for _, s := range a {
		visto[cabeca(s)] = true
	}
	for _, s := range b {
		if len(a) >= 6 {
			break
		}
		if !visto[cabeca(s)] {
			visto[cabeca(s)] = true
			a = append(a, s)
		}
	}
	return a
}

func cabeca(s string) string {
	if i := strings.Index(s, " a "); i > 0 {
		return s[:i]
	}
	if i := strings.IndexAny(s, "0123456789"); i > 0 {
		return s[:i]
	}
	return s
}
