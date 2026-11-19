package analise

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// O PAPEL DO JOGADOR NA PARTIDA: COMO ele jogou, não só quanto. Por regra,
// explicável, sem IA, só com o que a análise já tem: o fim de jogo do LCU
// (ouro, dano, abates, mortes, visão, tempo de controle), a linha do tempo por
// minuto (ouro e POSIÇÃO dos 10, para todos), os abates com o local, os
// objetivos com quem participou e, para quem foi rastreado, o tempo por
// atividade. Duas camadas:
//
//   - o PAPEL (igual para todas as funções): carregou, habilitou, foi
//     carregado, afundou, estável;
//   - o ESTILO (os critérios mudam com a função): topo ilha/split × agrupou
//     cedo × jogou fraco de lado × dominou a rota; meio roaming × assassino de
//     picks × ficou na rota × dominou a rota; selva ganks × power farm ×
//     invasão × objetivos; atirador escala × dominou a rota; suporte
//     roaming/visão × com o atirador × engajador.
//
// Cada um sai com os números que o justificam, um grau de confiança e o que
// não pôde ser avaliado. O que depende de os OUTROS estarem rastreados (luta,
// gank, roaming, split push e agrupado pela etiqueta) só usa a etiqueta com os
// 10 rastreados; nos outros casos (a coleta rastreia um só) usa a posição por
// minuto da linha do tempo, que todo mundo tem, e diz que usou. Regras e
// limiares: ParamPapel; vocabulário em docs/arquitetura/dados.md ("Papel do
// jogador").

// PapelJogador é o papel de um jogador na partida (resumos_jogador: colunas
// papel e estilo; o resto em dados.papel).
type PapelJogador struct {
	// Papel: carregou, habilitou, foi_carregado, afundou, estavel.
	Papel  string `json:"papel"`
	Rotulo string `json:"rotulo"`
	// Estilo: o da função (ilha_split, agrupou_cedo, fraco_de_lado,
	// dominou_rota, roaming, assassino_picks, ficou_na_rota, ganks, power_farm,
	// invasao, objetivos, escala, roaming_visao, com_atirador, engajador) ou
	// "" quando nenhum critério passou.
	Estilo       string `json:"estilo"`
	EstiloRotulo string `json:"estilo_rotulo"`
	// Tambem: os outros estilos que também passaram (mais fracos).
	Tambem []string `json:"tambem"`
	// Confianca: alta, media ou baixa (do papel; o do estilo em ConfiancaEstilo).
	Confianca       string `json:"confianca"`
	ConfiancaEstilo string `json:"confianca_estilo"`
	// Motivos: os números que justificam o papel; MotivosEstilo, o estilo.
	Motivos       []string `json:"motivos"`
	MotivosEstilo []string `json:"motivos_estilo"`
	// NaoAvaliado: o que não deu para avaliar e por quê.
	NaoAvaliado []string `json:"nao_avaliado"`
	// Fonte: de onde vieram os números.
	Fonte   string       `json:"fonte"`
	Numeros NumerosPapel `json:"numeros"`
	// Frase: a linha curta ("carregou: 38% do ouro do time, 41% do dano; estilo
	// ganks: 6 abates em rota antes dos 14 min").
	Frase string `json:"frase"`
}

// NumerosPapel são os números de onde o papel sai (% = do time dele).
type NumerosPapel struct {
	FatiaOuro          int  `json:"fatia_ouro"`
	FatiaDano          int  `json:"fatia_dano"`
	ParticipacaoAbates int  `json:"participacao_abates"` // (abates + assistências) ÷ abates do time
	FatiaAbates        int  `json:"fatia_abates"`        // abates dele ÷ abates do time
	FatiaMortes        int  `json:"fatia_mortes"`
	FatiaVisao         int  `json:"fatia_visao"`
	FatiaControle      int  `json:"fatia_controle"`      // tempo de controle (CC) aplicado
	FatiaDanoRecebido  int  `json:"fatia_dano_recebido"` // dano recebido + mitigado
	DifOuro10          *int `json:"dif_ouro_10"`
	DifOuro15          *int `json:"dif_ouro_15"`
	DifOuro20          *int `json:"dif_ouro_20"`
	DifOuroFim         *int `json:"dif_ouro_fim"`
	MortesCaras        int  `json:"mortes_caras"`
	MortesRotas        int  `json:"mortes_rotas"`
	ObjetivosParticip  int  `json:"objetivos_participou"`
	ObjetivosTime      int  `json:"objetivos_time"`
	// Monstros épicos (dragão, arauto, barão, vastilarvas, atakhan) do time e
	// quantos ele ajudou.
	EpicosParticipou int `json:"epicos_participou"`
	EpicosTime       int `json:"epicos_time"`
	// Posição por minuto (linha do tempo), fase das rotas (min 2 a 14):
	// % dos minutos na própria rota, fora dela (outra rota, rio, selva) e na
	// selva inimiga; com o atirador = % dos minutos a menos de
	// ParamPapel.DistanciaAtirador do atirador do time (suporte).
	MinutosRotas     int `json:"minutos_rotas"`
	PctRotaPropria   int `json:"pct_rota_propria"`
	PctForaDaRota    int `json:"pct_fora_da_rota"`
	PctSelvaInimiga  int `json:"pct_selva_inimiga"` // min 2 a 25
	PctComAtirador   int `json:"pct_com_atirador"`
	MinutosDepois    int `json:"minutos_depois"`      // depois dos 14 min
	PctSozinhoNoLado int `json:"pct_sozinho_no_lado"` // numa rota lateral, aliado mais perto além de DistanciaSozinhoLado
	PctAgrupado      int `json:"pct_agrupado"`        // com 2+ aliados a menos de DistanciaGrupo
	AbatesForaRota   int `json:"abates_fora_rota"`    // abates/assistências antes dos 14 min fora da própria rota (para quem é de rota)
	AbatesEmRota     int `json:"abates_em_rota"`      // caçador: abates/assistências nas rotas antes dos 14 min (ganks que mataram)
	AbatesNaRota     int `json:"abates_na_rota"`      // abates/assistências na própria rota antes dos 14 min
	PicksIsolados    int `json:"picks_isolados"`      // abates DELE em vítima sozinha (aliado mais perto além de DistanciaSozinho)
	// Mortes dele numa luta que o time venceu: na janela de ±ParamPapel.JanelaLutaMs
	// em volta da morte, o time dele matou pelo menos tantos quantos perdeu.
	MortesEmLutaVencida int     `json:"mortes_em_luta_vencida"`
	CSSelva15           *int    `json:"cs_selva_15"` // caçador: CS aos 15
	DifCS15             *int    `json:"dif_cs_15"`
	SentinelasPorMin    float64 `json:"sentinelas_por_min"`
	// Etiquetas (só rastreado): segundos e % do tempo nas atividades que
	// importam ao estilo; vazio sem rastreio.
	Atividades map[string]int `json:"atividades"`
}

// Limiares do papel (escolhidos à mão contra as 5 partidas analisadas em
// 2026-11-18; ver docs/arquitetura/dados.md, "Papel do jogador").
var ParamPapel = struct {
	// Carregou: pelo menos CarregouSinais de 4 sinais.
	CarregouFatiaOuro, CarregouFatiaDano, CarregouFatiaAbates int
	CarregouCrescimento                                       int // dif. de ouro no fim − aos 15, e no fim ≥ isto
	CarregouSinais                                            int
	// Afundou: pelo menos AfundouSinais de 3.
	AfundouFatiaMortes, AfundouMortesCaras, AfundouQueda int
	AfundouSinais                                        int
	// Afundou também exige KDA abaixo disto e não terminar à frente do
	// adversário por AfundouAFrenteMax ou mais (quem morre muito mas produz não afundou).
	AfundouKDAMax     float64
	AfundouAFrenteMax int
	// Habilitou: participação alta e pouca fatia de ouro, mais visão ou objetivos.
	HabilitouParticipacao, HabilitouFatiaOuroMax, HabilitouFatiaVisao, HabilitouObjetivosPct int
	// Foi carregado: venceu com participação e dano baixos.
	CarregadoParticipacaoMax, CarregadoFatiaDanoMax int
	// Estilos.
	DistanciaSozinhoLado, DistanciaGrupo, DistanciaAtirador            float64
	SplitPct, AgrupadoPct, FracoDifMax, FracoMortesRotasMax            int
	DominouDif15, DominouDif10                                         int
	RoamingPct, RoamingAbates, PicksMin, FicouNaRotaPct                int
	GanksMin, PowerFarmDifCS, InvasaoPct, EpicosPct, EpicosMin         int
	EscalaCrescimento, EscalaFatiaDano                                 int
	VisaoPct, ComAtiradorPct, EngajadorControle, EngajadorParticipacao int
	// Linha de frente que venceu (o tanque do topo que morre na frente): com
	// pelo menos LinhaFrenteControle % do controle do time OU
	// LinhaFrenteRecebido % do dano recebido/mitigado, participação ≥
	// LinhaFrenteParticipacao e menos de AfundouFatiaMortes % das mortes do
	// time, não afundou nem foi carregado: sai "estável", com confiança média se
	// metade das mortes (ou mais) foram em luta que o time venceu, baixa se não.
	LinhaFrenteControle, LinhaFrenteRecebido, LinhaFrenteParticipacao int
	LinhaFrenteRecebidoMin                                            int // dano recebido/mitigado mínimo, sempre
	JanelaLutaMs                                                      int64
	// Objetivos do caçador: os épicos sozinhos não bastam (o caçador dá o
	// Golpear em quase todos). Exige participação nos objetivos do time (torres
	// incluídas) de ObjetivosCacadorPct % ou mais, ou, rastreado, a etiqueta
	// objetivo em ObjetivoTempoPct % ou mais do tempo dele.
	ObjetivosCacadorPct, ObjetivoTempoPct int
}{
	CarregouFatiaOuro: 24, CarregouFatiaDano: 25, CarregouFatiaAbates: 27, CarregouCrescimento: 1000, CarregouSinais: 3,
	AfundouFatiaMortes: 25, AfundouMortesCaras: 5, AfundouQueda: 1500, AfundouSinais: 2, AfundouKDAMax: 2, AfundouAFrenteMax: 1000,
	HabilitouParticipacao: 55, HabilitouFatiaOuroMax: 19, HabilitouFatiaVisao: 25, HabilitouObjetivosPct: 50,
	CarregadoParticipacaoMax: 45, CarregadoFatiaDanoMax: 18,
	DistanciaSozinhoLado: 5000, DistanciaGrupo: 2000, DistanciaAtirador: 2000,
	SplitPct: 35, AgrupadoPct: 35, FracoDifMax: -700, FracoMortesRotasMax: 2,
	DominouDif15: 1000, DominouDif10: 600,
	RoamingPct: 30, RoamingAbates: 2, PicksMin: 3, FicouNaRotaPct: 70,
	GanksMin: 4, PowerFarmDifCS: 15, InvasaoPct: 20, EpicosPct: 85, EpicosMin: 5,
	EscalaCrescimento: 1500, EscalaFatiaDano: 28,
	VisaoPct: 30, ComAtiradorPct: 60, EngajadorControle: 35, EngajadorParticipacao: 55,
	LinhaFrenteControle: 30, LinhaFrenteRecebido: 25, LinhaFrenteParticipacao: 25, LinhaFrenteRecebidoMin: 20, JanelaLutaMs: 15000,
	ObjetivosCacadorPct: 65, ObjetivoTempoPct: 8,
}

var rotulosPapel = map[string]string{"carregou": "carregou", "habilitou": "habilitou", "foi_carregado": "foi carregado",
	"afundou": "afundou", "estavel": "estável"}

var rotulosEstilo = map[string]string{"ilha_split": "ilha / split push", "agrupou_cedo": "agrupou cedo",
	"fraco_de_lado": "jogou fraco de lado (absorveu a desvantagem)", "dominou_rota": "dominou a rota", "roaming": "roaming e prioridade",
	"assassino_picks": "assassino de picks", "ficou_na_rota": "ficou na rota", "ganks": "ganks", "power_farm": "power farm",
	"invasao": "invasão / controle da selva", "objetivos": "objetivos", "escala": "escala (cresceu no fim)",
	"roaming_visao": "roaming e visão", "com_atirador": "ficou com o atirador", "engajador": "engajador"}

// RotuloPapel e RotuloEstilo: o nome na tela.
func RotuloPapel(p string) string  { return rotulosPapel[p] }
func RotuloEstilo(e string) string { return rotulosEstilo[e] }

// PapeisValidos e EstilosValidos: o vocabulário (a busca valida o filtro).
var PapeisValidos = []string{"carregou", "habilitou", "foi_carregado", "afundou", "estavel"}

func pctI(a, tot int) int {
	if tot <= 0 {
		return 0
	}
	return int(math.Round(float64(a) / float64(tot) * 100))
}

// difEmMin: ouro dele − do adversário direto no quadro do minuto (nil sem quadro).
func (f *fatos) difEmMin(pid, adv, m int) *int {
	if adv <= 0 {
		return nil
	}
	q := f.dp.quadroEm(m)
	if q == nil {
		return nil
	}
	a, okA := q.Jogadores[pid]
	b, okB := q.Jogadores[adv]
	if !okA || !okB {
		return nil
	}
	return ip(a.OuroTotal - b.OuroTotal)
}

// difNoFim: no último quadro.
func (f *fatos) difNoFim(pid, adv int) *int {
	if adv <= 0 || len(f.dp.lt.Quadros) == 0 {
		return nil
	}
	q := f.dp.lt.Quadros[len(f.dp.lt.Quadros)-1]
	a, okA := q.Jogadores[pid]
	b, okB := q.Jogadores[adv]
	if !okA || !okB {
		return nil
	}
	return ip(a.OuroTotal - b.OuroTotal)
}

// sinal conta um critério: passou e a frase com o número.
type sinalPapel struct {
	ok    bool
	frase string
}

func conta(ss []sinalPapel) (int, []string) {
	n := 0
	var fr []string
	for _, s := range ss {
		if s.ok {
			n++
			fr = append(fr, s.frase)
		}
	}
	return n, fr
}

// papeis calcula o papel de cada jogador (depois de jogadores e notas).
func (f *fatos) papeis(js []ResumoJogador) {
	type tot struct {
		ouro, abates, mortes, visao, cc int
		dano, recebido                  int64
	}
	times := map[int]*tot{100: {}, 200: {}}
	for _, p := range f.dp.pt.Participantes {
		t := times[p.Time]
		if t == nil {
			continue
		}
		t.ouro += p.Ouro
		t.abates += p.Abates
		t.mortes += p.Mortes
		t.visao += p.Visao
		t.cc += p.TempoCC
		t.dano += p.DanoCampeoes
		t.recebido += p.DanoRecebido + p.DanoMitigado
	}
	rastreados := len(f.dp.amostras)
	for i := range js {
		j := &js[i]
		p := f.dp.participante(j.ParticipanteID)
		t := times[j.Time]
		if p == nil || t == nil {
			continue
		}
		n := NumerosPapel{FatiaOuro: pctI(p.Ouro, t.ouro), FatiaDano: pct(p.DanoCampeoes, t.dano),
			ParticipacaoAbates: pctI(p.Abates+p.Assistencias, t.abates), FatiaAbates: pctI(p.Abates, t.abates),
			FatiaMortes: pctI(p.Mortes, t.mortes), FatiaVisao: pctI(p.Visao, t.visao), FatiaControle: pctI(p.TempoCC, t.cc),
			FatiaDanoRecebido: pct(p.DanoRecebido+p.DanoMitigado, t.recebido),
			DifOuro10:         j.DifOuro10, DifOuro15: j.DifOuro15, MortesCaras: j.MortesCaras, MortesRotas: j.MortesRotas,
			ObjetivosParticip: j.ObjetivosParticipou, ObjetivosTime: j.ObjetivosTime, DifCS15: j.DifCS15, Atividades: map[string]int{}}
		n.DifOuro20 = f.difEmMin(j.ParticipanteID, j.Adversario, 20)
		n.DifOuroFim = f.difNoFim(j.ParticipanteID, j.Adversario)
		if f.dp.pt.DuracaoS > 0 {
			n.SentinelasPorMin = math.Round(float64(j.Sentinelas)/(float64(f.dp.pt.DuracaoS)/60)*100) / 100
		}
		for _, o := range f.objs {
			if o.tipo == "objetivo" && o.time == j.Time {
				n.EpicosTime++
				if contem(o.participantes, j.ParticipanteID) {
					n.EpicosParticipou++
				}
			}
		}
		f.posicoesDoPapel(j, &n)
		f.abatesDoPapel(j, &n)
		n.MortesEmLutaVencida = f.mortesEmLutaVencida(j.ParticipanteID, j.Time)
		if q := f.dp.quadroEm(15); q != nil && j.Funcao == "JUNGLE" {
			if eu, ok := q.Jogadores[j.ParticipanteID]; ok {
				n.CSSelva15 = ip(eu.CS)
			}
		}
		pp := PapelJogador{Numeros: n, Tambem: []string{}, NaoAvaliado: []string{}, Motivos: []string{}, MotivosEstilo: []string{}}
		if j.Rastreado {
			for _, a := range []string{ASplitPush, AAgrupado, ARoaming, AGankando, AInvadindo, AFarmando, AObjetivo, AColetando} {
				if s := j.Etiquetas["total"][a]; s > 0 {
					n.Atividades[a] = s
				}
			}
			pp.Numeros.Atividades = n.Atividades
			pp.Fonte = fmt.Sprintf("fim de jogo, linha do tempo por minuto e rastreio (%d de 10 rastreados)", rastreados)
		} else {
			pp.Fonte = "fim de jogo e linha do tempo por minuto (jogador não rastreado)"
		}
		f.classificaPapel(j, &pp)
		f.classificaEstilo(j, &pp, rastreados)
		if !j.Rastreado {
			pp.NaoAvaliado = append(pp.NaoAvaliado, "tempo por atividade (farmando, objetivo, invadindo...): o jogador não foi rastreado; o estilo sai só da posição por minuto e dos abates")
		} else if rastreados < 10 {
			pp.NaoAvaliado = append(pp.NaoAvaliado, fmt.Sprintf("luta, troca, gank, roaming, split push e agrupado pela etiqueta: precisam dos outros rastreados (%d de 10); usada a posição por minuto da linha do tempo (1 ponto por minuto)", rastreados))
		}
		if j.Adversario == 0 || n.DifOuro15 == nil {
			pp.NaoAvaliado = append(pp.NaoAvaliado, "diferença de rota: sem adversário direto com quadro aos 10/15")
		}
		pp.Frase = frasePapel(pp)
		j.Papel = pp
	}
}

// posicoesDoPapel: a posição por minuto (todos os 10 têm) em grupos relativos.
func (f *fatos) posicoesDoPapel(j *ResumoJogador, n *NumerosPapel) {
	rota := RotaDaPosicao(j.Funcao)
	var atirador int
	for _, p := range f.dp.pt.Participantes {
		if p.Time == j.Time && p.Posicao == "BOTTOM" {
			atirador = p.ParticipanteID
		}
	}
	var mr, propria, fora, comAt, md, sozinho, agrupado, mi, inimiga int
	for _, q := range f.dp.lt.Quadros {
		m := int((q.TMs + 30000) / 60000)
		eu, ok := q.Jogadores[j.ParticipanteID]
		if !ok || m < 2 {
			continue
		}
		z, _ := Zona(eu.X, eu.Y)
		g := GrupoRelativo(z, rota, j.Time)
		if g == "base_propria" || g == "fora" {
			continue // na base (morto ou comprando): não conta para onde ele jogou
		}
		if m <= 25 {
			mi++
			if g == "selva_inimiga" {
				inimiga++
			}
		}
		if q.TMs < Param.FaseRotasMs {
			mr++
			switch {
			case g == "rota_propria":
				propria++
			case rota != "":
				fora++
			}
			if at, ok := q.Jogadores[atirador]; ok && atirador != j.ParticipanteID && math.Hypot(at.X-eu.X, at.Y-eu.Y) < ParamPapel.DistanciaAtirador {
				comAt++
			}
			continue
		}
		md++
		perto, maisPerto := 0, math.Inf(1)
		for _, p := range f.dp.pt.Participantes {
			if p.Time != j.Time || p.ParticipanteID == j.ParticipanteID {
				continue
			}
			o, ok := q.Jogadores[p.ParticipanteID]
			if !ok {
				continue
			}
			if za, _ := Zona(o.X, o.Y); GrupoRelativo(za, "", p.Time) == "base_propria" {
				continue
			}
			d := math.Hypot(o.X-eu.X, o.Y-eu.Y)
			maisPerto = math.Min(maisPerto, d)
			if d < ParamPapel.DistanciaGrupo {
				perto++
			}
		}
		if perto >= 2 {
			agrupado++
		}
		if (z == ZRotaTopo || z == ZRotaBaixo) && maisPerto > ParamPapel.DistanciaSozinhoLado {
			sozinho++
		}
	}
	n.MinutosRotas, n.PctRotaPropria, n.PctForaDaRota = mr, pctI(propria, mr), pctI(fora, mr)
	n.PctComAtirador = pctI(comAt, mr)
	n.PctSelvaInimiga = pctI(inimiga, mi)
	n.MinutosDepois, n.PctSozinhoNoLado, n.PctAgrupado = md, pctI(sozinho, md), pctI(agrupado, md)
}

// abatesDoPapel: abates e assistências dele antes dos 14 min por lugar, e os
// picks (abates dele em vítima sozinha).
func (f *fatos) abatesDoPapel(j *ResumoJogador, n *NumerosPapel) {
	rota := RotaDaPosicao(j.Funcao)
	pid := j.ParticipanteID
	for _, rt := range f.retratos {
		dele := rt.Matador == pid || contem(rt.Assistencias, pid)
		if !dele {
			continue
		}
		if rt.Matador == pid && f.mortes[rt.N] != nil && contem2(f.mortes[rt.N].tags, "sozinho") {
			n.PicksIsolados++
		}
		if rt.TMs >= Param.FaseRotasMs || rt.Zona == "" {
			continue
		}
		emRota := TipoZona(rt.Zona) == "rota"
		switch {
		case j.Funcao == "JUNGLE":
			if emRota {
				n.AbatesEmRota++
			}
		case rt.Zona == rota:
			n.AbatesNaRota++
		default:
			n.AbatesForaRota++
		}
	}
}

// mortesEmLutaVencida: em quantas mortes dele o time venceu a luta (na janela
// de ±JanelaLutaMs, o time dele matou pelo menos tantos quantos perdeu, a
// morte dele inclusa). Morrer na frente numa troca que o time ganhou é o
// trabalho do tanque, não "entregar".
func (f *fatos) mortesEmLutaVencida(pid, time int) int {
	n := 0
	for _, m := range f.retratos {
		if m.Vitima != pid {
			continue
		}
		ganhou, perdeu := 0, 0
		for _, o := range f.retratos {
			if o.TMs < m.TMs-ParamPapel.JanelaLutaMs || o.TMs > m.TMs+ParamPapel.JanelaLutaMs {
				continue
			}
			if f.timeDe[o.Vitima] == time {
				perdeu++
			} else if f.timeDe[o.Vitima] != 0 {
				ganhou++
			}
		}
		if ganhou >= perdeu {
			n++
		}
	}
	return n
}

// linhaDeFrente: o jogador segurou a frente de uma vitória (controle ou dano
// recebido, participação) sem ser quem mais morreu no time. O resultado das
// lutas em que ele morreu decide a confiança (classificaPapel).
func linhaDeFrente(j *ResumoJogador, n NumerosPapel) bool {
	P := ParamPapel
	// O dano recebido mínimo separa o tanque de quem só tem lentidão (a Ashe do
	// meio da 3305449546: 49% do controle do time com 12% do dano recebido).
	return j.Venceu && n.FatiaDanoRecebido >= P.LinhaFrenteRecebidoMin &&
		(n.FatiaControle >= P.LinhaFrenteControle || n.FatiaDanoRecebido >= P.LinhaFrenteRecebido) &&
		n.ParticipacaoAbates >= P.LinhaFrenteParticipacao && n.FatiaMortes < P.AfundouFatiaMortes
}

func contem2(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func difTxt(p *int) string {
	if p == nil {
		return "—"
	}
	return sinalOuro(*p)
}

// classificaPapel: carregou → afundou → habilitou → foi carregado → estável.
func (f *fatos) classificaPapel(j *ResumoJogador, pp *PapelJogador) {
	n, P := pp.Numeros, ParamPapel
	crescente := n.DifOuroFim != nil && n.DifOuro15 != nil && *n.DifOuroFim >= P.CarregouCrescimento && *n.DifOuroFim > *n.DifOuro15
	carry := []sinalPapel{
		{n.FatiaOuro >= P.CarregouFatiaOuro, fmt.Sprintf("%d%% do ouro do time", n.FatiaOuro)},
		{n.FatiaDano >= P.CarregouFatiaDano, fmt.Sprintf("%d%% do dano a campeões do time", n.FatiaDano)},
		{n.FatiaAbates >= P.CarregouFatiaAbates, fmt.Sprintf("%d%% dos abates do time (%d)", n.FatiaAbates, j.Abates)},
		{crescente, fmt.Sprintf("vantagem crescente sobre o adversário direto: %s aos 15 → %s no fim", difTxt(n.DifOuro15), difTxt(n.DifOuroFim))},
	}
	queda := n.DifOuroFim != nil && *n.DifOuroFim <= -P.AfundouQueda && (n.DifOuro15 == nil || *n.DifOuroFim < *n.DifOuro15)
	afunda := []sinalPapel{
		{n.FatiaMortes >= P.AfundouFatiaMortes, fmt.Sprintf("%d%% das mortes do time (%d)", n.FatiaMortes, j.Mortes)},
		{n.MortesCaras >= P.AfundouMortesCaras, fmt.Sprintf("%d mortes caras", n.MortesCaras)},
		{queda, fmt.Sprintf("desvantagem que cresceu: %s aos 15 → %s no fim", difTxt(n.DifOuro15), difTxt(n.DifOuroFim))},
	}
	objPct := pctI(n.ObjetivosParticip, n.ObjetivosTime)
	habil := []sinalPapel{
		{n.FatiaVisao >= P.HabilitouFatiaVisao, fmt.Sprintf("%d%% da visão do time", n.FatiaVisao)},
		{objPct >= P.HabilitouObjetivosPct, fmt.Sprintf("em %d de %d objetivos do time (%d%%)", n.ObjetivosParticip, n.ObjetivosTime, objPct)},
		{j.Assistencias >= 2*max(1, j.Abates), fmt.Sprintf("%d assistências para %d abates", j.Assistencias, j.Abates)},
	}
	kda := float64(j.Abates+j.Assistencias) / math.Max(1, float64(j.Mortes))
	nc, frC := conta(carry)
	na, frA := conta(afunda)
	nh, frH := conta(habil)
	part := fmt.Sprintf("participou de %d%% dos abates do time", n.ParticipacaoAbates)
	ouro := fmt.Sprintf("%d%% do ouro do time", n.FatiaOuro)
	conf := func(ok, total, minimo int) string {
		switch {
		case ok >= total || ok >= minimo+1:
			return "alta"
		case ok >= minimo:
			return "media"
		}
		return "baixa"
	}
	// A linha de frente de uma vitória (o tanque do topo que morre na frente) não
	// afunda nem é carregada só pelas mortes e pelo ouro: o trabalho dela é esse.
	frente := linhaDeFrente(j, n)
	fraseFrente := fmt.Sprintf("linha de frente da vitória: %d%% do controle do time, %d%% do dano recebido/mitigado, %d de %d mortes em lutas que o time venceu",
		n.FatiaControle, n.FatiaDanoRecebido, n.MortesEmLutaVencida, j.Mortes)
	switch {
	case nc >= P.CarregouSinais:
		pp.Papel, pp.Motivos, pp.Confianca = "carregou", append(frC, part), conf(nc, 4, P.CarregouSinais)
	case na >= P.AfundouSinais && kda < P.AfundouKDAMax && (n.DifOuroFim == nil || *n.DifOuroFim < P.AfundouAFrenteMax) && !frente:
		pp.Papel, pp.Motivos, pp.Confianca = "afundou", append(frA, strings.Replace(fmt.Sprintf("KDA %.1f", kda), ".", ",", 1), part), conf(na, 3, P.AfundouSinais)
	case n.ParticipacaoAbates >= P.HabilitouParticipacao && n.FatiaOuro <= P.HabilitouFatiaOuroMax && nh >= 1:
		pp.Papel, pp.Motivos, pp.Confianca = "habilitou", append([]string{part, ouro}, frH...), conf(nh, 3, 1)
	case frente:
		pp.Papel, pp.Confianca = "estavel", "media"
		if 2*n.MortesEmLutaVencida < j.Mortes {
			// Morreu mais sozinho ou em luta perdida do que trocando: não é
			// "afundou" (a vitória teve a frente dele), mas a confiança cai.
			pp.Confianca = "baixa"
		}
		pp.Motivos = []string{fraseFrente, part, ouro}
		if na >= P.AfundouSinais {
			// Os sinais de "afundou" passaram; a linha de frente os explica.
			pp.Motivos = append(pp.Motivos, "sinais de afundou explicados pela linha de frente: "+strings.Join(frA, ", "))
		}
	case j.Venceu && n.ParticipacaoAbates < P.CarregadoParticipacaoMax && n.FatiaDano < P.CarregadoFatiaDanoMax:
		pp.Papel, pp.Confianca = "foi_carregado", "media"
		pp.Motivos = []string{"venceu", part, fmt.Sprintf("%d%% do dano a campeões do time", n.FatiaDano), ouro}
		if n.ParticipacaoAbates < P.CarregadoParticipacaoMax-10 {
			pp.Confianca = "alta"
		}
	default:
		pp.Papel, pp.Confianca = "estavel", "media"
		pp.Motivos = []string{part, ouro, fmt.Sprintf("%d%% do dano", n.FatiaDano), fmt.Sprintf("%d%% das mortes do time", n.FatiaMortes),
			fmt.Sprintf("rota %s aos 15, %s no fim", difTxt(n.DifOuro15), difTxt(n.DifOuroFim))}
		// Perto de um limiar (um sinal a menos que carregar/afundar): confiança baixa.
		if nc == P.CarregouSinais-1 || na == P.AfundouSinais-1 {
			pp.Confianca = "baixa"
		}
	}
	pp.Rotulo = rotulosPapel[pp.Papel]
}

// estiloCand é um estilo candidato com a força (valor ÷ limiar; ≥ 1 passa).
type estiloCand struct {
	nome    string
	forca   float64
	motivos []string
	// soPosicao: saiu só da posição por minuto (sem a etiqueta).
	soPosicao bool
}

func razao(v, lim float64) float64 {
	if lim == 0 {
		return 0
	}
	return v / lim
}

// classificaEstilo: os critérios da função; vence o de maior força.
func (f *fatos) classificaEstilo(j *ResumoJogador, pp *PapelJogador, rastreados int) {
	n, P := pp.Numeros, ParamPapel
	comEtiqueta := j.Rastreado && rastreados >= 10
	total := 0
	for _, s := range j.Etiquetas["total"] {
		total += s
	}
	etq := func(a string) (int, int) { s := j.Etiquetas["total"][a]; return s, pctI(s, total) }
	var cs []estiloCand
	dif15 := 0
	if n.DifOuro15 != nil {
		dif15 = *n.DifOuro15
	}
	dif10 := 0
	if n.DifOuro10 != nil {
		dif10 = *n.DifOuro10
	}
	dominou := estiloCand{"dominou_rota", math.Max(razao(float64(dif15), float64(P.DominouDif15)), razao(float64(dif10), float64(P.DominouDif10))),
		[]string{fmt.Sprintf("ouro contra o adversário direto: %s aos 10, %s aos 15", difTxt(n.DifOuro10), difTxt(n.DifOuro15))}, false}
	if n.DifOuro15 == nil {
		dominou.forca = 0
	}
	switch j.Funcao {
	case "TOP":
		split := estiloCand{"ilha_split", razao(float64(n.PctSozinhoNoLado), float64(P.SplitPct)),
			[]string{fmt.Sprintf("%d%% dos minutos depois dos 14 numa rota lateral com o aliado mais perto a mais de %.0f (%d min)",
				n.PctSozinhoNoLado, P.DistanciaSozinhoLado, n.MinutosDepois)}, !comEtiqueta}
		if s, p := etq(ASplitPush); comEtiqueta && s > 0 {
			split.forca = math.Max(split.forca, razao(float64(p), 15))
			split.motivos = append(split.motivos, fmt.Sprintf("etiqueta split push: %s (%d%% da partida)", segundosTexto(int64(s)*1000), p))
		}
		agr := estiloCand{"agrupou_cedo", razao(float64(n.PctAgrupado), float64(P.AgrupadoPct)),
			[]string{fmt.Sprintf("%d%% dos minutos depois dos 14 com 2+ aliados a menos de %.0f", n.PctAgrupado, P.DistanciaGrupo)}, !comEtiqueta}
		if n.PctSozinhoNoLado >= P.SplitPct {
			agr.forca = 0
		}
		fraco := estiloCand{nome: "fraco_de_lado"}
		if n.DifOuro15 != nil && dif15 <= P.FracoDifMax && n.MortesRotas <= P.FracoMortesRotasMax && n.FatiaMortes <= 22 {
			fraco.forca = razao(float64(-dif15), float64(-P.FracoDifMax))
			fraco.motivos = []string{fmt.Sprintf("atrás na rota (%s aos 15) sem entregar: %d morte(s) até os 14 min, %d%% das mortes do time",
				difTxt(n.DifOuro15), n.MortesRotas, n.FatiaMortes)}
		}
		cs = append(cs, split, agr, fraco, dominou)
	case "MIDDLE":
		roam := estiloCand{nome: "roaming", soPosicao: !comEtiqueta}
		if n.AbatesForaRota >= P.RoamingAbates {
			roam.forca = razao(float64(n.PctForaDaRota), float64(P.RoamingPct))
		}
		roam.motivos = []string{fmt.Sprintf("%d%% dos minutos 2–14 fora da rota do meio; %d abate(s)/assistência(s) fora dela antes dos 14 min",
			n.PctForaDaRota, n.AbatesForaRota)}
		if s, p := etq(ARoaming); comEtiqueta && s > 0 {
			roam.motivos = append(roam.motivos, fmt.Sprintf("etiqueta roaming: %s (%d%%)", segundosTexto(int64(s)*1000), p))
		}
		picks := estiloCand{"assassino_picks", 0, []string{fmt.Sprintf("%d abate(s) em vítima sozinha (de %d abates)", n.PicksIsolados, j.Abates)}, false}
		if j.Abates > 0 && pctI(n.PicksIsolados, j.Abates) >= 40 {
			picks.forca = razao(float64(n.PicksIsolados), float64(P.PicksMin))
		}
		ficou := estiloCand{nome: "ficou_na_rota", motivos: []string{fmt.Sprintf("%d%% dos minutos 2–14 na rota do meio; %d abate(s)/assistência(s) fora dela",
			n.PctRotaPropria, n.AbatesForaRota)}}
		if n.AbatesForaRota < P.RoamingAbates {
			ficou.forca = razao(float64(n.PctRotaPropria), float64(P.FicouNaRotaPct))
		}
		cs = append(cs, picks, roam, dominou, ficou)
	case "JUNGLE":
		ganks := estiloCand{"ganks", razao(float64(n.AbatesEmRota), float64(P.GanksMin)),
			[]string{fmt.Sprintf("%d abate(s)/assistência(s) nas rotas antes dos 14 min", n.AbatesEmRota)}, false}
		if s, p := etq(AGankando); comEtiqueta && s > 0 {
			ganks.motivos = append(ganks.motivos, fmt.Sprintf("etiqueta gankando: %s (%d%%)", segundosTexto(int64(s)*1000), p))
		}
		farm := estiloCand{nome: "power_farm"}
		if n.DifCS15 != nil && n.AbatesEmRota <= 2 {
			farm.forca = razao(float64(*n.DifCS15), float64(P.PowerFarmDifCS))
			farm.motivos = []string{fmt.Sprintf("CS %s contra o caçador inimigo aos 15 (%s), só %d abate(s) nas rotas antes dos 14",
				difTxt(n.DifCS15), numOuTraco(n.CSSelva15), n.AbatesEmRota)}
		}
		inv := estiloCand{"invasao", razao(float64(n.PctSelvaInimiga), float64(P.InvasaoPct)),
			[]string{fmt.Sprintf("%d%% dos minutos 2–25 na selva inimiga", n.PctSelvaInimiga)}, false}
		if s, p := etq(AInvadindo); j.Rastreado && s > 0 {
			// Invadir não depende dos outros: vale com um só rastreado.
			inv.forca = math.Max(inv.forca, razao(float64(p), 10))
			inv.motivos = append(inv.motivos, fmt.Sprintf("etiqueta invadindo: %s (%d%% da partida)", segundosTexto(int64(s)*1000), p))
		}
		// Objetivos: os épicos sozinhos não bastam (o caçador dá o Golpear em
		// quase todos: é o esperado da função). Precisa também participar acima do
		// esperado nos objetivos do time (torres incluídas) ou, rastreado, passar
		// tempo relevante em objetivo.
		objPct := pctI(n.ObjetivosParticip, n.ObjetivosTime)
		sObj, pObj := etq(AObjetivo)
		obj := estiloCand{nome: "objetivos", motivos: []string{fmt.Sprintf("em %d de %d monstros épicos do time; em %d de %d objetivos do time (%d%%)",
			n.EpicosParticipou, n.EpicosTime, n.ObjetivosParticip, n.ObjetivosTime, objPct)}}
		if j.Rastreado && sObj > 0 {
			obj.motivos = append(obj.motivos, fmt.Sprintf("etiqueta objetivo: %s (%d%% do tempo dele)", segundosTexto(int64(sObj)*1000), pObj))
		}
		acimaDoEsperado := objPct >= P.ObjetivosCacadorPct || (j.Rastreado && pObj >= P.ObjetivoTempoPct)
		if n.EpicosParticipou >= P.EpicosMin && acimaDoEsperado {
			obj.forca = razao(float64(pctI(n.EpicosParticipou, n.EpicosTime)), float64(P.EpicosPct))
		}
		cs = append(cs, ganks, farm, inv, obj)
	case "BOTTOM":
		escala := estiloCand{nome: "escala"}
		if n.DifOuroFim != nil && n.DifOuro15 != nil && dif15 < P.DominouDif15 {
			cresc := *n.DifOuroFim - dif15
			if n.FatiaDano >= P.EscalaFatiaDano {
				escala.forca = razao(float64(cresc), float64(P.EscalaCrescimento))
			}
			escala.motivos = []string{fmt.Sprintf("%s aos 15 → %s no fim contra o atirador inimigo; %d%% do dano do time",
				difTxt(n.DifOuro15), difTxt(n.DifOuroFim), n.FatiaDano)}
		}
		if n.AbatesNaRota+n.AbatesForaRota > 0 {
			dominou.motivos = append(dominou.motivos, fmt.Sprintf("%d abate(s)/assistência(s) na rota de baixo antes dos 14 min", n.AbatesNaRota))
		}
		cs = append(cs, dominou, escala)
	case "UTILITY":
		rv := estiloCand{nome: "roaming_visao", soPosicao: !comEtiqueta}
		if n.FatiaVisao >= P.VisaoPct {
			rv.forca = razao(float64(n.PctForaDaRota), float64(P.RoamingPct))
		}
		rv.motivos = []string{fmt.Sprintf("%d%% da visão do time (%.2f sentinelas/min); %d%% dos minutos 2–14 fora da rota de baixo",
			n.FatiaVisao, n.SentinelasPorMin, n.PctForaDaRota)}
		com := estiloCand{"com_atirador", razao(float64(n.PctComAtirador), float64(P.ComAtiradorPct)),
			[]string{fmt.Sprintf("%d%% dos minutos 2–14 a menos de %.0f do atirador do time", n.PctComAtirador, P.DistanciaAtirador)}, true}
		if s, p := etq(AColetando); j.Rastreado && s > 0 {
			com.motivos = append(com.motivos, fmt.Sprintf("etiqueta coletando ouro (ao lado de quem farma): %s (%d%%)", segundosTexto(int64(s)*1000), p))
		}
		eng := estiloCand{nome: "engajador", motivos: []string{fmt.Sprintf("%d%% do tempo de controle aplicado pelo time, %d%% do dano recebido/mitigado, participou de %d%% dos abates",
			n.FatiaControle, n.FatiaDanoRecebido, n.ParticipacaoAbates)}}
		if n.ParticipacaoAbates >= P.EngajadorParticipacao {
			eng.forca = razao(float64(n.FatiaControle), float64(P.EngajadorControle))
		}
		cs = append(cs, eng, rv, com)
	default:
		pp.NaoAvaliado = append(pp.NaoAvaliado, "estilo: sem função conhecida nesta partida")
		return
	}
	sort.SliceStable(cs, func(a, b int) bool { return cs[a].forca > cs[b].forca })
	for _, c := range cs {
		if c.forca < 1 {
			break
		}
		if pp.Estilo == "" {
			pp.Estilo, pp.MotivosEstilo = c.nome, c.motivos
			pp.ConfiancaEstilo = "media"
			if c.forca >= 1.4 {
				pp.ConfiancaEstilo = "alta"
			}
			// Só pela posição por minuto (sem etiqueta dos 10): no máximo média.
			if c.soPosicao && pp.ConfiancaEstilo == "alta" {
				pp.ConfiancaEstilo = "media"
			}
			continue
		}
		pp.Tambem = append(pp.Tambem, c.nome)
	}
	if pp.Estilo == "" {
		pp.ConfiancaEstilo = "baixa"
		// O mais perto de passar, para dizer por que nenhum passou.
		if len(cs) > 0 && len(cs[0].motivos) > 0 {
			pp.MotivosEstilo = append([]string{"nenhum critério da função passou; o mais perto: " + rotulosEstilo[cs[0].nome]}, cs[0].motivos...)
		}
	}
	pp.EstiloRotulo = rotulosEstilo[pp.Estilo]
}

// frasePapel: "carregou (alta): 38% do ouro do time, 41% do dano...; estilo ganks: ...".
func frasePapel(pp PapelJogador) string {
	s := pp.Rotulo
	if len(pp.Motivos) > 0 {
		m := pp.Motivos
		if len(m) > 3 {
			m = m[:3]
		}
		s += ": " + strings.Join(m, ", ")
	}
	if pp.Estilo != "" {
		s += "; estilo " + pp.EstiloRotulo
		if len(pp.MotivosEstilo) > 0 {
			s += ": " + pp.MotivosEstilo[0]
		}
	}
	return s
}

// nuncaNil: listas vazias em vez de null (resumo de antes do papel).
func (p *PapelJogador) nuncaNil() {
	if p.Motivos == nil {
		p.Motivos = []string{}
	}
	if p.MotivosEstilo == nil {
		p.MotivosEstilo = []string{}
	}
	if p.Tambem == nil {
		p.Tambem = []string{}
	}
	if p.NaoAvaliado == nil {
		p.NaoAvaliado = []string{}
	}
	if p.Numeros.Atividades == nil {
		p.Numeros.Atividades = map[string]int{}
	}
}

// linhaPapel: o papel com a confiança, os números e o que não deu para avaliar
// (o pacote de contexto da IA).
func linhaPapel(p PapelJogador) string {
	if p.Papel == "" {
		return ""
	}
	s := fmt.Sprintf("%s (confiança %s): %s", p.Rotulo, nomeConfianca(p.Confianca), strings.Join(p.Motivos, ", "))
	if p.Estilo != "" {
		s += fmt.Sprintf(". Estilo da função: %s (confiança %s): %s", p.EstiloRotulo, nomeConfianca(p.ConfiancaEstilo), strings.Join(p.MotivosEstilo, "; "))
		if len(p.Tambem) > 0 {
			var t []string
			for _, e := range p.Tambem {
				t = append(t, rotulosEstilo[e])
			}
			s += " (também: " + strings.Join(t, ", ") + ")"
		}
	} else {
		s += ". Estilo da função: nenhum critério passou"
	}
	if len(p.NaoAvaliado) > 0 {
		s += ". Não avaliado: " + strings.Join(p.NaoAvaliado, "; ")
	}
	return s + "."
}

func nomeConfianca(c string) string {
	if c == "media" {
		return "média"
	}
	return c
}
