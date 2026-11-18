package analise

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// Janelas de oportunidade: o adversário direto morreu ou foi para a base, e
// por um tempo a área dele fica sem dono. Para o caçador, a janela vai da
// morte (ou da ida à base) do caçador inimigo até ele voltar à PRÓPRIA selva;
// para quem é de rota, até o adversário voltar à rota. O que se afirma:
//
//   - FATO: o instante da morte (linha do tempo), o renascer (estado dos 10) e
//     a volta (posição rastreada do inimigo), o que o jogador fez na janela
//     (etiquetas de atividade dele), objetivos, placas e abates na janela;
//   - ESTIMATIVA: a volta do inimigo não rastreado (renascer + VoltaSelvaMs /
//     VoltaRotaMs), o estado de cada acampamento (a limpeza é deduzida de quem
//     foi rastreado: perto do acampamento usando habilidade; quem não foi
//     rastreado não limpa nada no dado) e o valor em ouro da janela.
//
// Cada janela vira um momento `janela_perdida` ou `janela_aproveitada`.

// TempoAcampamento: quando o acampamento nasce, em quanto volta depois de
// limpo e o ouro equivalente. Tempos da League of Legends Wiki ("Jungling",
// temporada 2025: buffs 1:30 e 5 min; pequenos 1:30 e 2:15; caranguejo 3:30 e
// 2:30), escritos de memória e NÃO conferidos nesta tarefa nem no patch da
// partida: a conferir. O ouro é equivalente escolhido à mão.
type TempoAcampamento struct {
	PrimeiroMs int64
	VoltaMs    int64
	Ouro       int
}

// TemposAcampamento por tipo (a primeira palavra do nome do acampamento).
var TemposAcampamento = map[string]TempoAcampamento{
	"azul":       {90000, 300000, 100},
	"vermelho":   {90000, 300000, 100},
	"sapo":       {90000, 135000, 80},
	"lobos":      {90000, 135000, 85},
	"aves":       {90000, 135000, 85},
	"krugues":    {90000, 135000, 100},
	"caranguejo": {210000, 150000, 55},
}

func tipoAcampamento(nome string) string {
	if i := strings.Index(nome, " "); i > 0 {
		return nome[:i]
	}
	return nome
}

// ladoAcampamento: 100 para "(lado azul)", 200 para "(lado vermelho)", 0 no rio.
func ladoAcampamento(nome string) int {
	switch {
	case strings.Contains(nome, "lado azul"):
		return 100
	case strings.Contains(nome, "lado vermelho"):
		return 200
	}
	return 0
}

// limpeza: um acampamento limpo (estimado) por quem estava lá.
type limpeza struct {
	acamp int // índice em Acampamentos
	t     int64
	quem  int
}

// limpezas deduz, das amostras de quem foi rastreado, quando cada acampamento
// foi limpo: perto do centro (RaioAcampamento) usando habilidade (a regra do
// "farmando" na selva). Usos seguidos a menos de LimpezaAcampamentoMs são a
// mesma limpeza, carimbada no último.
func (f *fatos) limpezas() []limpeza {
	var brutas []limpeza
	pids := make([]int, 0, len(f.dp.amostras))
	for pid := range f.dp.amostras {
		pids = append(pids, pid)
	}
	sort.Ints(pids)
	for _, pid := range pids {
		as := f.dp.amostras[pid]
		usos := usosDeHabilidade(as)
		for i := range as {
			a := &as[i]
			if a.Morto || a.X == nil || a.Y == nil {
				continue
			}
			c, _, ok := acampamentoPerto(Ponto{*a.X, *a.Y})
			if !ok || !usouHabilidadePerto(usos, a.TMs, Param.JanelaAcampamentoMs) {
				continue
			}
			for k := range Acampamentos {
				if Acampamentos[k].Nome == c.Nome {
					brutas = append(brutas, limpeza{acamp: k, t: a.TMs, quem: pid})
				}
			}
		}
	}
	sort.SliceStable(brutas, func(a, b int) bool {
		if brutas[a].acamp != brutas[b].acamp {
			return brutas[a].acamp < brutas[b].acamp
		}
		return brutas[a].t < brutas[b].t
	})
	var l []limpeza
	for _, b := range brutas {
		if n := len(l); n > 0 && l[n-1].acamp == b.acamp && b.t-l[n-1].t <= Param.LimpezaAcampamentoMs {
			l[n-1].t = b.t
			continue
		}
		l = append(l, b)
	}
	return l
}

// EstadoAcampamento: o que se estima de um acampamento numa janela.
type EstadoAcampamento struct {
	Nome   string `json:"nome"`
	Estado string `json:"estado"` // vivo, nasce_na_janela, morto
	// DesdeMs/NasceMs: vivo desde / nasce em (estimado pelo tempo de volta).
	NasceMs int64 `json:"nasce_ms"`
	// UltimaLimpezaMs e Por: a última limpeza vista antes da janela (nulo =
	// nenhuma vista: quem limpou pode não ter sido rastreado).
	UltimaLimpezaMs *int64 `json:"ultima_limpeza_ms,omitempty"`
	Por             int    `json:"por,omitempty"`
	// LimpoNaJanelaPor: quem limpou dentro da janela (0 = ninguém visto).
	LimpoNaJanelaMs  *int64 `json:"limpo_na_janela_ms,omitempty"`
	LimpoNaJanelaPor int    `json:"limpo_na_janela_por,omitempty"`
	Ouro             int    `json:"ouro"`
	// x, y: o centro (para a distância de quem estava na janela).
	ponto Ponto
}

func (f *fatos) estadoAcampamentos(lado int, ini, fim int64, ls []limpeza) []EstadoAcampamento {
	var r []EstadoAcampamento
	for k, a := range Acampamentos {
		if ladoAcampamento(a.Nome) != lado {
			continue
		}
		tp := TemposAcampamento[tipoAcampamento(a.Nome)]
		e := EstadoAcampamento{Nome: a.Nome, Ouro: tp.Ouro, NasceMs: tp.PrimeiroMs, ponto: a.Ponto}
		for _, l := range ls {
			if l.acamp != k {
				continue
			}
			if l.t < ini {
				t := l.t
				e.UltimaLimpezaMs, e.Por, e.NasceMs = &t, l.quem, l.t+tp.VoltaMs
			} else if l.t <= fim && e.LimpoNaJanelaMs == nil {
				t := l.t
				e.LimpoNaJanelaMs, e.LimpoNaJanelaPor = &t, l.quem
			}
		}
		switch {
		case e.NasceMs <= ini:
			e.Estado = "vivo"
		case e.NasceMs <= fim:
			e.Estado = "nasce_na_janela"
		default:
			e.Estado = "morto"
		}
		r = append(r, e)
	}
	return r
}

// volta: quando o jogador volta à área dele (selva própria para o caçador,
// rota própria para os outros) depois de desde, pelas amostras dele. Sem
// rastreio: desde + VoltaSelvaMs / VoltaRotaMs (estimado).
func (f *fatos) volta(pid int, desde, teto int64) (int64, bool) {
	cacador := f.posDe[pid] == "JUNGLE"
	as := f.dp.amostras[pid]
	if len(as) > 0 {
		rota := RotaDaPosicao(f.posDe[pid])
		i := sort.Search(len(as), func(k int) bool { return as[k].TMs >= desde })
		for k := i; k < len(as) && as[k].TMs <= teto; k++ {
			a := &as[k]
			if a.Morto || a.X == nil || a.Y == nil {
				continue
			}
			z, _ := Zona(*a.X, *a.Y)
			g := GrupoRelativo(z, rota, f.timeDe[pid])
			if (cacador && g == "selva_propria") || (!cacador && g == "rota_propria") {
				return a.TMs, false
			}
		}
		// Rastreado e não voltou até o teto: a janela vai até o teto (fato).
		if cobertura(as, desde, teto) > (teto-desde)*8/10 {
			return teto, false
		}
	}
	if cacador {
		return min(desde+Param.VoltaSelvaMs, teto), true
	}
	return min(desde+Param.VoltaRotaMs, teto), true
}

// janelaBruta: a ausência de um jogador (morto ou na base).
type janelaBruta struct {
	ausente     int
	ini, fim    int64
	motivo      string // morte, base
	renasceu    int64
	fimEstimado bool
	retrato     int
	zonaDaMorte string
}

func (f *fatos) ausencias() []janelaBruta {
	var l []janelaBruta
	fimPartida := f.dp.duracaoMs()
	for _, rt := range f.retratos {
		pid := rt.Vitima
		volta, est := f.renasce(pid, rt.TMs)
		teto := min(rt.TMs+Param.JanelaMaximaMs, fimPartida)
		fim, est2 := f.volta(pid, volta, teto)
		if fim < volta {
			fim = min(volta, teto)
		}
		l = append(l, janelaBruta{ausente: pid, ini: rt.TMs, fim: fim, motivo: "morte", renasceu: volta, fimEstimado: est || est2,
			retrato: rt.N, zonaDaMorte: rt.Zona})
	}
	// Idas à base (recall e na_base) de quem foi rastreado, fora das janelas de morte.
	pids := make([]int, 0, len(f.trechos))
	for pid := range f.trechos {
		pids = append(pids, pid)
	}
	sort.Ints(pids)
	for _, pid := range pids {
		for _, t := range f.trechos[pid] {
			if t.atv != ARecall && t.atv != ANaBase {
				continue
			}
			dentro := false
			for _, j := range l {
				if j.ausente == pid && t.ini >= j.ini && t.ini <= j.fim {
					dentro = true
					break
				}
			}
			if dentro {
				continue
			}
			teto := min(t.ini+Param.JanelaMaximaMs, fimPartida)
			fim, est := f.volta(pid, t.fim, teto)
			// Recall seguido de na_base: um trecho só (o na_base começa onde o recall acaba).
			if n := len(l); n > 0 && l[n-1].ausente == pid && l[n-1].motivo == "base" && t.ini <= l[n-1].fim+1000 {
				l[n-1].fim, l[n-1].fimEstimado = max(l[n-1].fim, fim), est
				continue
			}
			l = append(l, janelaBruta{ausente: pid, ini: t.ini, fim: fim, motivo: "base", renasceu: t.fim, fimEstimado: est})
		}
	}
	sort.SliceStable(l, func(a, b int) bool { return l[a].ini < l[b].ini })
	return l
}

// mortoNoTrecho: quanto do trecho o jogador passou morto (estado dos 10).
func (f *fatos) mortoNoTrecho(pid int, ini, fim int64) int64 {
	es := f.dp.estados[pid]
	var tot int64
	for i := 0; i+1 < len(es); i++ {
		if !es[i].Morto {
			continue
		}
		a, b := max(es[i].TMs, ini), min(es[i+1].TMs, fim)
		if b > a && es[i+1].TMs-es[i].TMs <= 3000 {
			tot += b - a
		}
	}
	return tot
}

// AtividadeNaJanela: o tempo de cada etiqueta do jogador na janela.
type AtividadeNaJanela struct {
	Atividade string `json:"atividade"`
	Ms        int64  `json:"ms"`
}

// janelas: as janelas de oportunidade de cada jogador (o adversário direto
// ausente), como momentos.
func (f *fatos) janelas() []MomentoChave {
	var l []MomentoChave
	ls := f.limpezas()
	for _, jb := range f.ausencias() {
		x := jb.ausente
		j := f.dp.adversario(x)
		if j == 0 || jb.fim-jb.ini < Param.JanelaMinimaMs || jb.ini < Param.InicioJanelasMs {
			continue
		}
		if f.mortoNoTrecho(j, jb.ini, jb.fim)*2 > jb.fim-jb.ini {
			// Os dois mortos (ou ele mais morto que vivo): não era janela para ele.
			continue
		}
		if m, ok := f.momentoJanela(jb, j, ls); ok {
			l = append(l, m)
		}
	}
	return l
}

func (f *fatos) momentoJanela(jb janelaBruta, j int, ls []limpeza) (MomentoChave, bool) {
	x := jb.ausente
	cacador := f.posDe[x] == "JUNGLE"
	tj, tx := f.timeDe[j], f.timeDe[x]
	// Sempre estimado: o valor da janela (acampamentos vivos, ondas) é dedução,
	// mesmo quando o instante e o que ele fez são fato.
	m := MomentoChave{TMs: jb.ini, FimMs: jb.fim, DuracaoMs: jb.fim - jb.ini, ParticipanteID: j, Envolvidos: []int{j, x},
		Estimado: true, Dados: map[string]any{"ausente": x, "motivo": jb.motivo, "renasceu_ms": jb.renasceu,
			"fim_estimado": jb.fimEstimado}}
	if cacador {
		m.Subtipo = "selva"
	} else {
		m.Subtipo = "rota"
		m.Rota = RotaDaPosicao(f.posDe[j])
	}
	// O que ele fez (fato quando rastreado).
	rastreado := len(f.dp.amostras[j]) > 0
	atv := f.tempoPorAtividade(j, jb.ini, jb.fim)
	var lista []AtividadeNaJanela
	for a, ms := range atv {
		lista = append(lista, AtividadeNaJanela{a, ms})
	}
	sort.Slice(lista, func(a, b int) bool {
		if lista[a].Ms != lista[b].Ms {
			return lista[a].Ms > lista[b].Ms
		}
		return lista[a].Atividade < lista[b].Atividade
	})
	m.Dados["atividades"] = atv
	m.Dados["rastreado"] = rastreado
	// Onde ele estava quando a janela abriu (fato, se rastreado).
	var pos Ponto
	temPos := false
	if p, ok := PosicaoEm(f.dp.amostras[j], jb.ini); ok {
		pos, temPos = p, true
		z, _ := Zona(p.X, p.Y)
		m.Dados["zona_inicio"] = z
	}
	if len(lista) > 0 {
		m.Atividade = lista[0].Atividade
	}
	// O que aconteceu na janela com ele (fato: linha do tempo).
	var feitos []string
	usou := false
	for _, o := range f.objs {
		if o.time == tj && o.t >= jb.ini && o.t <= jb.fim {
			if contem(o.participantes, j) {
				usou = true
				feitos = append(feitos, fmt.Sprintf("%s aos %s", o.nome, MinSeg(o.t)))
			}
		}
	}
	for _, rt := range f.retratos {
		if rt.TMs >= jb.ini && rt.TMs <= jb.fim && rt.TMs > jb.ini && (rt.Matador == j || contem(rt.Assistencias, j)) {
			usou = true
			feitos = append(feitos, fmt.Sprintf("abate em %s aos %s", f.campeao(rt.Vitima), MinSeg(rt.TMs)))
		}
	}
	placas := 0
	if !cacador {
		for _, e := range f.dp.eventosRiot {
			if e.Tipo != "TURRET_PLATE_DESTROYED" || e.TMs < jb.ini || e.TMs > jb.fim {
				continue
			}
			var b struct {
				Time int    `json:"teamId"`
				Rota string `json:"laneType"`
			}
			json.Unmarshal(e.Bruto, &b)
			rotaDe := map[string]string{"TOP_LANE": ZRotaTopo, "MID_LANE": ZRotaMeio, "BOT_LANE": ZRotaBaixo}
			if b.Time == tx && rotaDe[b.Rota] == m.Rota {
				placas++
			}
		}
		if placas > 0 {
			usou = true
			feitos = append(feitos, fmt.Sprintf("%d placa(s) da torre", placas))
		}
	}
	var valor int
	var acamps []EstadoAcampamento
	if cacador {
		acamps = f.estadoAcampamentos(tx, jb.ini, jb.fim, ls)
		limpouNaJanela := 0
		for _, a := range acamps {
			if a.Estado != "morto" && a.LimpoNaJanelaPor != j {
				valor += a.Ouro
			}
			if a.LimpoNaJanelaPor == j {
				limpouNaJanela++
			}
		}
		m.Dados["acampamentos"] = acamps
		if temPos {
			melhor := -1.0
			nome := ""
			for _, a := range acamps {
				if a.Estado == "morto" {
					continue
				}
				if d := dist(pos, a.ponto); melhor < 0 || d < melhor {
					melhor, nome = d, a.Nome
				}
			}
			if melhor >= 0 {
				m.Dados["dist_acampamento_vivo"] = math.Round(melhor)
				m.Dados["acampamento_vivo_perto"] = nome
			}
		}
		if limpouNaJanela > 0 {
			usou = true
			feitos = append(feitos, fmt.Sprintf("limpou %d acampamento(s) da selva inimiga", limpouNaJanela))
		}
		if atv[AInvadindo] >= Param.UsoMinimoJanelaMs || atv[AObjetivo] >= Param.UsoMinimoJanelaMs {
			usou = true
		}
	} else {
		ondas := int(math.Ceil(float64(jb.fim-jb.ini) / float64(Param.IntervaloOndasMs)))
		valor = ondas * Param.OuroOnda
		if jb.ini < Param.FaseRotasMs && f.torreExternaDePe(m.Rota, tx, jb.ini) {
			valor += Param.OuroPlaca
			m.Dados["placas_disponiveis"] = true
		}
		if atv[ATorre]+atv[ADive] >= Param.UsoMinimoJanelaMs || atv[AObjetivo] >= Param.UsoMinimoJanelaMs {
			usou = true
		}
	}
	m.Dados["valor"] = valor
	if len(feitos) > 0 {
		m.Dados["feitos"] = feitos
	}
	if usou {
		m.Tipo, m.Time = "janela_aproveitada", tj
		custoPara(&m, tx, valor)
	} else {
		m.Tipo, m.Time = "janela_perdida", tx
		custoPara(&m, tj, valor)
	}
	m.Zona = jb.zonaDaMorte
	// A frase é a mesma linha curta do pacote de contexto (fatos_contexto.go),
	// montada dos dados como eles ficam no banco (JSON de ida e volta): a tela,
	// a busca por texto e o contexto leem o mesmo texto.
	nomes := map[int]string{}
	for _, p := range f.dp.pt.Participantes {
		nomes[p.ParticipanteID] = p.Campeao
	}
	copia := m
	copia.Dados = jsonMapa(jsonTexto(m.Dados))
	l := linhaJanela(copia, nomes)
	if k := strings.Index(l, "] "); k >= 0 {
		l = l[k+2:]
	}
	m.Frase = l
	return m, true
}

// torreExternaDePe: a torre externa da rota do time ainda de pé em t.
func (f *fatos) torreExternaDePe(rota string, time int, t int64) bool {
	for _, o := range f.objs {
		if o.tipo == "torre" && o.rota == rota && o.time == 300-time && o.sub == "OUTER_TURRET" && o.t <= t {
			return false
		}
	}
	return true
}
