package analise

import (
	"encoding/json"
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
	// AColetando: o suporte ao lado de um aliado que está farmando. Ele não
	// mata tropa: está ali recolhendo o ouro do item de suporte e a experiência.
	AColetando = "coletando_ouro"
	// AGankando: o caçador indo para uma rota onde, em seguida, entra em luta.
	// ARoaming: quem é de rota, fora da própria rota, indo para uma luta.
	// Os dois só são marcados olhando para trás: o trecho "andando" que termina
	// numa luta vira a aproximação (rotulaAproximacoes).
	AGankando = "gankando"
	ARoaming  = "roaming"
	// ACounterGank: o caçador que chega a uma luta de rota para responder ao gank
	// do caçador inimigo (os dois se aproximam da mesma luta; quem chega DEPOIS
	// ao contato com um inimigo de rota é o counter gank). `com` = o caçador
	// inimigo que estava gankando. Só caçador e só na fase de rotas; quem é de
	// rota e chega para responder continua "roaming".
	ACounterGank = "counter_gank"
	// ARotacionando: depois da fase de rotas, indo para uma luta. Gank e roaming
	// são da fase de rotas (os dois cadernos de especialistas dizem o mesmo:
	// ela acaba quando cai a primeira torre, entre 10 e 14 min).
	ARotacionando = "rotacionando"
	AAndando      = "andando"
	// AParado: quase não saiu do lugar e não há nenhum outro sinal.
	AParado = "parado"
)

// ItensDeSuporte: o item de suporte e as evoluções dele (Atlas do Mundo,
// Bússola Rúnica, Recompensa dos Mundos e as cinco formas finais). É o item que
// rende ouro ao lado de quem farma; quem o vende deixa de "coletar ouro".
// Conferido na partida 3304217746: Leona e Senna com 3865 no início e 3876 depois.
var ItensDeSuporte = map[int]bool{3865: true, 3866: true, 3867: true, 3869: true, 3870: true, 3871: true, 3876: true, 3877: true}

// temItemDeSuporte diz se o jogador seguido tem o item de suporte no instante t
// (pelo inventário do playerlist; sem estado gravado perto de t, falso).
func (p *preparo) temItemDeSuporte(t int64) bool {
	e := EstadoEm(p.estados[p.c.ParticipanteID], t, 2000)
	if e == nil {
		return false
	}
	for _, s := range e.Slots {
		if ItensDeSuporte[s.ID] {
			return true
		}
	}
	return false
}

// Atividades é o vocabulário, na ordem de prioridade da regra.
var Atividades = []string{AMorto, ANaBase, ALutando, ATrocando, AObjetivo, ATorre, AFarmando, AColetando, AGankando, ACounterGank, ARoaming, ARotacionando, AAndando, AParado}

type rotuloAtv struct {
	atv       string
	com       []int
	sinais    []string
	confianca string
	zona      string
	// alvo: qual objetivo ou estrutura ("dragão infernal", "torre do meio").
	alvo string
	// farmando: em "coletando_ouro", o suporte também está farmando (o CS dele
	// sobe no minuto). Um campo, não outra atividade do vocabulário.
	farmando bool
}

func (r rotuloAtv) chave() string { return r.atv + r.alvo + fmt.Sprint(r.com) + fmt.Sprint(r.farmando) }

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
			alvo := nomeDoAlvo(e)
			return rotuloAtv{atv: atv, alvo: alvo, confianca: "media", zona: z.zona, sinais: []string{
				fmt.Sprintf("%s a %.0f, cai %.0f s depois", alvo, d, float64(e.TMs-t)/1000)}}
		}
	}
	// Limpando um acampamento da selva: perto do centro dele, sem inimigo
	// rastreado por perto e com uma habilidade usada agora. Vem antes da regra
	// do deslocamento porque o caçador limpa em poucos segundos e já sai andando.
	if len(inim) == 0 {
		if !p.usosOK {
			p.usos, p.usosOK = usosDeHabilidade(c.Amostras), true
		}
		if ac, d, ok := acampamentoPerto(z.ponto); ok && usouHabilidadePerto(p.usos, t, Param.JanelaAcampamentoMs) {
			return rotuloAtv{atv: AFarmando, confianca: "media", zona: z.zona, sinais: []string{
				fmt.Sprintf("habilidade usada a %.0f do acampamento %s", d, ac.Nome), "nenhum inimigo rastreado perto"}}
		}
	}
	// Quem atravessa o mapa não está farmando: deslocamento em linha reta nos
	// últimos JanelaDeslocamentoMs acima de DeslocamentoAndando = andando.
	if q, ok := PosicaoEm(c.Amostras, t-Param.JanelaDeslocamentoMs); ok {
		if d := dist(z.ponto, q); d > Param.DeslocamentoAndando {
			return rotuloAtv{atv: AAndando, confianca: "media", zona: z.zona, sinais: []string{
				fmt.Sprintf("deslocou %.0f em %.0f s", d, float64(Param.JanelaDeslocamentoMs)/1000)}}
		}
	}
	tipo := TipoZona(z.zona)
	// Quem tem o item de suporte, ao lado de um aliado rastreado que está
	// farmando NAQUELE momento: coletando ouro, não farmando nem andando. O
	// aliado andando, na base ou apanhando não conta, mesmo com o CS dele subindo
	// no minuto. Vale pelo item, não pela posição: vendeu o item, acabou.
	if p.temItemDeSuporte(t) && (tipo == "rota" || tipo == "selva" || tipo == "rio") {
		for _, a := range alia {
			if a.d > Param.AlcanceTroca {
				continue
			}
			if r, ok := p.aliadoFarmando(a.pid, t); ok {
				rot := rotuloAtv{atv: AColetando, com: []int{a.pid}, confianca: "media", zona: z.zona, sinais: []string{
					fmt.Sprintf("aliado %d a %.0f farmando (CS %.0f/min no minuto)", a.pid, a.d, r), "nenhuma luta por perto"}}
				// O suporte que também está matando tropa (o CS DELE sobe no minuto,
				// mesma regra do "farmando") continua "coletando ouro", com o farm
				// marcado: sem isso o farm dele sumia da etiqueta.
				if meu, ok := csPorMinuto(c.Linha, c.ParticipanteID, t); ok && meu >= Param.CSMinimoPorMin {
					rot.farmando = true
					rot.sinais = append(rot.sinais, fmt.Sprintf("o próprio CS %.0f/min no minuto (linha do tempo)", meu))
				}
				return rot
			}
		}
	}
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
	// Sem nenhum outro sinal: parado, se quase não saiu do lugar na janela;
	// senão andando (devagar ou em volta).
	if q, ok := PosicaoEm(c.Amostras, t-Param.JanelaDeslocamentoMs); ok {
		if d := dist(z.ponto, q); d <= Param.DeslocamentoParado {
			return rotuloAtv{atv: AParado, confianca: "baixa", zona: z.zona, sinais: []string{
				fmt.Sprintf("deslocou só %.0f em %.0f s", d, float64(Param.JanelaDeslocamentoMs)/1000), "nenhum outro sinal"}}
		}
	}
	return rotuloAtv{atv: AAndando, confianca: "baixa", zona: z.zona, sinais: []string{"nenhum outro sinal"}}
}

// aliadoFarmando diz se o aliado rastreado está farmando no instante t, pelas
// mesmas condições que dariam "farmando" a ele: parado (não deslocou mais que
// DeslocamentoAndando na janela), numa rota, na selva ou no rio, com a vida
// sem cair, e com o CS da linha do tempo subindo no minuto. Devolve o ritmo
// de CS. Sem isso o suporte aparecia "coletando ouro" ao lado de um atirador
// que só estava andando (partida 3304217746, 5:44).
func (p *preparo) aliadoFarmando(pid int, t int64) (float64, bool) {
	agora, ok := p.posicaoDe(pid, t)
	if !ok {
		return 0, false
	}
	zona, _ := Zona(agora.X, agora.Y)
	if tipo := TipoZona(zona); tipo != "rota" && tipo != "selva" && tipo != "rio" {
		return 0, false
	}
	antes, ok := p.posicaoDe(pid, t-Param.JanelaDeslocamentoMs)
	if !ok || dist(agora, antes) > Param.DeslocamentoAndando {
		return 0, false
	}
	if q, tem := p.quedaDeVida(pid, t, Param.JanelaVidaMs); tem && q >= Param.QuedaVidaPct {
		return 0, false
	}
	r, ok := csPorMinuto(p.c.Linha, pid, t)
	if !ok || r < Param.CSMinimoPorMin {
		return 0, false
	}
	return r, true
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
		curto := t.fim-t.ini < Param.TrechoMinimoMs && (t.r.atv == AAndando || t.r.atv == AFarmando || t.r.atv == AColetando || t.r.atv == AParado)
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
	lim = p.rotulaAproximacoes(lim)
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
		dados := map[string]any{
			"atividade": t.r.atv, "fim_ms": t.fim, "duracao_ms": t.fim - t.ini, "com": com, "zona": t.r.zona,
			"sinais": sinais, "confianca": t.r.confianca}
		if t.r.alvo != "" {
			dados["alvo"] = t.r.alvo
		}
		if t.r.atv == AColetando {
			dados["farmando"] = t.r.farmando
		}
		itens = append(itens, rastreio.ItemCadeia{TMs: t.ini, Tipo: "atividade", Dados: dados})
	}
	return itens
}

// fimDaFaseDeRotas: ver FimDaFaseDeRotas.
func (p *preparo) fimDaFaseDeRotas() int64 {
	t, _ := FimDaFaseDeRotas(p.eventos)
	return t
}

// FimDaFaseDeRotas é quando cai a primeira torre da partida, no máximo aos
// FaseRotasMs (14 min): é o marco que as fontes dos dois cadernos dão para o
// fim da fase de rotas e o começo do meio de jogo. Exportada para a rota
// /fases mostrar na tela o MESMO instante que a regra da atividade usa.
// torreMs é o instante da primeira torre (-1 se nenhuma caiu).
func FimDaFaseDeRotas(eventos []rastreio.EventoLT) (fimMs, torreMs int64) {
	for _, e := range eventos {
		if e.Tipo != "BUILDING_KILL" {
			continue
		}
		var b struct {
			Estrutura string `json:"buildingType"`
		}
		json.Unmarshal(e.Bruto, &b)
		if b.Estrutura == "TOWER_BUILDING" {
			return min(e.TMs, Param.FaseRotasMs), e.TMs
		}
	}
	return Param.FaseRotasMs, -1
}

// TrechoFase é uma fase da partida para a tela (GET /api/partidas/{id}/fases).
type TrechoFase struct {
	Fase   string `json:"fase"`
	DeMs   int64  `json:"de_ms"`
	AteMs  *int64 `json:"ate_ms"`
	Motivo string `json:"motivo"`
}

// FasesDaPartida divide a partida em rotas / meio / final: rotas até
// FimDaFaseDeRotas, final a partir do marco fixo FaseMeioMs. Fase que
// começaria depois do fim da partida fica de fora.
func FasesDaPartida(eventos []rastreio.EventoLT, duracaoMs int64) []TrechoFase {
	fimRotas, torre := FimDaFaseDeRotas(eventos)
	motivo := fmt.Sprintf("caiu a primeira torre aos %s", relogio(torre))
	if torre < 0 || torre > Param.FaseRotasMs {
		motivo = fmt.Sprintf("marco fixo de %s: nenhuma torre caiu antes", relogio(Param.FaseRotasMs))
	}
	fimMeio := max(Param.FaseMeioMs, fimRotas)
	fases := []TrechoFase{{Fase: "rotas", DeMs: 0, AteMs: &fimRotas, Motivo: "começo da partida"}}
	if duracaoMs <= 0 || fimRotas < duracaoMs {
		fases = append(fases, TrechoFase{Fase: "meio", DeMs: fimRotas, AteMs: &fimMeio, Motivo: motivo})
	}
	if duracaoMs <= 0 || fimMeio < duracaoMs {
		fases = append(fases, TrechoFase{Fase: "final", DeMs: fimMeio, Motivo: fmt.Sprintf("marco fixo de %s", relogio(fimMeio))})
	}
	return fases
}

// relogio: ms de jogo → "11:14".
func relogio(ms int64) string {
	s := ms / 1000
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// rotulaAproximacoes olha para trás: um trecho "andando" que termina numa luta
// ou troca com inimigo vira a aproximação. Para o caçador, chegando numa rota,
// é "gankando"; para quem é de rota, fora da própria rota, é "roaming". Só a
// parte final do trecho conta (até JanelaAproximacaoMs antes da luta), e só se
// nela o jogador de fato chegou perto (AproximacaoMinima). É o caso da Vi aos
// 3:42 da partida 3304217746: 21 s indo do lobo até o meio, e a luta começa.
func (p *preparo) rotulaAproximacoes(ts []trechoAtv) []trechoAtv {
	var saida []trechoAtv
	for i, t := range ts {
		luta := (t.r.atv == ALutando || t.r.atv == ATrocando) && len(t.r.com) > 0
		n := len(saida)
		if !luta || n == 0 || saida[n-1].r.atv != AAndando || t.ini-saida[n-1].fim > 3000 || i == 0 {
			saida = append(saida, t)
			continue
		}
		atv := ""
		switch {
		case t.ini >= p.fimDaFaseDeRotas():
			atv = ARotacionando
		case p.rota == "" && TipoZona(t.r.zona) == "rota":
			atv = AGankando
		case p.rota != "" && t.r.zona != p.rota:
			atv = ARoaming
		}
		alvo, ok := PosicaoEm(p.c.Amostras, t.ini)
		if atv == "" || !ok {
			saida = append(saida, t)
			continue
		}
		ini := max(saida[n-1].ini, t.ini-Param.JanelaAproximacaoMs)
		de, ok := PosicaoEm(p.c.Amostras, ini)
		// Rápido demais para ser andando: teleporte ou volta à base no meio.
		if !ok || dist(de, alvo) < Param.AproximacaoMinima || dist(de, alvo) > Param.VelocidadeMaxima*float64(t.ini-ini)/1000 {
			saida = append(saida, t)
			continue
		}
		// Só inimigos na lista de "com quem": a luta pode ter aliados junto.
		var inimigos []int
		for _, pid := range t.r.com {
			if contem(p.inimigos, pid) {
				inimigos = append(inimigos, pid)
			}
		}
		if len(inimigos) == 0 {
			saida = append(saida, t)
			continue
		}
		sinais := []string{
			fmt.Sprintf("percorreu %.0f em %.0f s até a luta em %s", dist(de, alvo), float64(t.ini-ini)/1000, t.r.zona),
			"a luta começou em seguida"}
		if atv == AGankando {
			atv, inimigos, sinais = p.ganhoOuCounter(t.ini, t.r.zona, inimigos, sinais)
		}
		aprox := trechoAtv{ini: ini, fim: t.ini, r: rotuloAtv{atv: atv, com: inimigos, confianca: "media", zona: t.r.zona, sinais: sinais}}
		if ini <= saida[n-1].ini {
			saida[n-1] = aprox // o trecho andando inteiro era a aproximação
		} else {
			saida[n-1].fim = ini
			saida = append(saida, aprox)
		}
		saida = append(saida, t)
	}
	return saida
}

// ganhoOuCounter decide, para o caçador que chega a uma luta de rota (gank),
// quem é o alvo e se, na verdade, ele está respondendo ao gank do caçador
// inimigo (counter gank). Partida 3304217746, 3:42–4:03: Vi e Shaco chegam ao
// meio para a mesma luta e os dois saíam "gankando".
//
//   - O alvo do gank são os inimigos de ROTA da luta, não o caçador inimigo que
//     chegou junto (a Vi gankando a Diana, não "Diana e Shaco").
//   - Se um caçador inimigo da luta também veio andando de longe (mesmo teste
//     da aproximação: percorreu ≥ AproximacaoMinima na janela), quem chega
//     DEPOIS ao primeiro contato com um inimigo na zona da luta (contatoNaLuta)
//     é o counter gank, com
//     `com` = o caçador que gankou. O contato é medido do mesmo jeito para os
//     dois, pelas posições (as etiquetas do outro não estão no contexto), então
//     o passe de cada um chega à mesma conclusão. Chegaram juntos (menos de
//     MargemCounterMs): os dois ficam gankando.
func (p *preparo) ganhoOuCounter(tLuta int64, zona string, inimigos []int, sinais []string) (string, []int, []string) {
	eu := p.c.ParticipanteID
	var deRota, cacadores []int
	for _, pid := range inimigos {
		if p.c.Posicoes[pid] == "JUNGLE" {
			cacadores = append(cacadores, pid)
		} else {
			deRota = append(deRota, pid)
		}
	}
	de := tLuta - Param.JanelaAproximacaoMs
	ate := tLuta + Param.JanelaContatoDepoisMs
	for _, ej := range cacadores {
		a, okA := p.posicaoDe(ej, de)
		b, okB := p.posicaoDe(ej, tLuta)
		if !okA || !okB || dist(a, b) < Param.AproximacaoMinima {
			continue // ele já estava ali: não veio responder nem gankar
		}
		meu, ok1 := p.contatoNaLuta(eu, zona, de, ate)
		dele, ok2 := p.contatoNaLuta(ej, zona, de, ate)
		if !ok1 || !ok2 {
			continue
		}
		if meu-dele >= Param.MargemCounterMs {
			return ACounterGank, []int{ej}, append(sinais, fmt.Sprintf(
				"o caçador inimigo %d chegou ao contato %s s antes (%s × %s): respondeu ao gank dele",
				ej, segundosDec(meu-dele), relogioDec(dele), relogioDec(meu)))
		}
		if dele-meu >= Param.MargemCounterMs {
			sinais = append(sinais, fmt.Sprintf("o caçador inimigo %d chegou ao contato %s s depois (counter gank)", ej, segundosDec(dele-meu)))
		}
	}
	if len(deRota) > 0 {
		return AGankando, deRota, sinais
	}
	return AGankando, inimigos, sinais
}

// contatoNaLuta: o primeiro instante em [de, ate] (de 250 em 250 ms) em que o
// participante pid está a até AlcanceTroca de um inimigo dele que está na zona
// da luta, pelas posições rastreadas. Qualquer inimigo conta, inclusive o
// caçador: na partida 3304217746 o Shaco chega ao meio depois de a Orianna
// morrer, e o primeiro contato dele lá é a própria Vi. A zona tira o que veio
// antes em outro lugar (o Shaco trocando com o Garen no topo aos 3:39).
func (p *preparo) contatoNaLuta(pid int, zona string, de, ate int64) (int64, bool) {
	time := p.c.Times[pid]
	var alvos []int
	for q, tq := range p.c.Times {
		if tq != time && q != pid {
			alvos = append(alvos, q)
		}
	}
	sort.Ints(alvos)
	for t := de; t <= ate; t += 250 {
		eu, ok := p.posicaoDe(pid, t)
		if !ok {
			continue
		}
		for _, q := range alvos {
			o, ok := p.posicaoDe(q, t)
			if !ok || dist(eu, o) > Param.AlcanceTroca {
				continue
			}
			if z, _ := Zona(o.X, o.Y); z == zona {
				return t, true
			}
		}
	}
	return 0, false
}

// segundosDec: ms → "5,8" (vírgula decimal, como o resto da tela).
func segundosDec(ms int64) string {
	return strings.Replace(fmt.Sprintf("%.1f", float64(ms)/1000), ".", ",", 1)
}

// relogioDec: ms → "4:03,1".
func relogioDec(ms int64) string {
	return fmt.Sprintf("%d:%02d,%d", ms/60000, (ms/1000)%60, (ms%1000)/100)
}

// nomeDoAlvo diz qual objetivo ou estrutura é o do evento, pelos campos crus da
// linha do tempo (monsterType/monsterSubType, buildingType/laneType).
func nomeDoAlvo(e rastreio.EventoLT) string {
	var b struct {
		Monstro   string `json:"monsterType"`
		Sub       string `json:"monsterSubType"`
		Estrutura string `json:"buildingType"`
		Rota      string `json:"laneType"`
	}
	json.Unmarshal(e.Bruto, &b)
	rota := map[string]string{"TOP_LANE": " do topo", "MID_LANE": " do meio", "BOT_LANE": " de baixo"}[b.Rota]
	switch e.Tipo {
	case "ELITE_MONSTER_KILL":
		switch b.Monstro {
		case "DRAGON":
			if s, ok := map[string]string{"FIRE_DRAGON": "infernal", "WATER_DRAGON": "do oceano", "EARTH_DRAGON": "da montanha",
				"AIR_DRAGON": "das nuvens", "HEXTECH_DRAGON": "hextec", "CHEMTECH_DRAGON": "quimtec", "ELDER_DRAGON": "ancião"}[b.Sub]; ok {
				return "dragão " + s
			}
			return "dragão"
		case "BARON_NASHOR":
			return "barão"
		case "RIFTHERALD":
			return "arauto"
		case "HORDE":
			return "vastilarvas"
		case "ATAKHAN":
			return "atakhan"
		}
		return "monstro grande"
	case "TURRET_PLATE_DESTROYED":
		return "placa da torre" + rota
	case "BUILDING_KILL":
		if b.Estrutura == "INHIBITOR_BUILDING" {
			return "inibidor" + rota
		}
		return "torre" + rota
	}
	return strings.ToLower(e.Tipo)
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
