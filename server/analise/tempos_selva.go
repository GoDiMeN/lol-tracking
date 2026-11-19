package analise

import (
	"strconv"
	"strings"
)

// OS TEMPOS DA SELVA, num lugar só e por patch: quando cada acampamento e cada
// monstro épico nasce pela primeira vez e em quanto volta depois de morto. A
// partida sabe o seu patch ("16.22", que a Riot chama de 26.22) e usa a linha
// da tabela que cobre esse patch (TemposDaSelva).
//
// Fontes (consultadas em 18/11/2026):
//   - [W] League of Legends Wiki, infobox e "Patch History" de cada monstro:
//     https://wiki.leagueoflegends.com/en-us/Blue_Sentinel (editada 26/03/2026),
//     .../Red_Brambleback (26/03/2026), .../Gromp (29/09/2026),
//     .../Greater_Murk_Wolf (26/03/2026), .../Crimson_Raptor (26/03/2026),
//     .../Ancient_Krug (22/07/2026), .../Rift_Scuttler, .../Voidgrub
//     (18/11/2026), .../Rift_Herald, .../Baron_Nashor, .../Atakhan e
//     .../Dragon_pit_(League_of_Legends).
//   - [N] Notas do patch 26.1 da Riot (07/01/2026):
//     https://www.leagueoflegends.com/en-us/news/game-updates/patch-26-1-notes/
//     ("Murk Wolf, Blue Sentinel, Red Brambleback, Raptor Camps: Spawn at 55
//     seconds"; "Krug, Gromp Camps: Spawn at 1:07"; "River Scuttler Camps:
//     Spawn at 2:55"; "Baron Nashor: 25:00 ⇒ 20:00"; "Atakhan has been removed";
//     "Epic Monster spawn times are unchanged unless noted above").
//   - [R] https://www.riftpatchnotes.com/lol/system/jungle: de 26.1 a 26.23
//     só o 26.1 mexeu nos tempos dos acampamentos.
//
// Conferido contra o dado em 18/11/2026 (TestRealTemposSelva, as 10 partidas
// com linha do tempo da Riot, patches 16.22 e 16.23):
//   - épicos: nenhum abate antes do primeiro nascimento (primeiro dragão 6:05,
//     vastilarvas 8:09, arauto 15:30, barão 21:49) e nenhum intervalo entre
//     dois abates menor que o renascimento (dragão ≥ 5:06, barão ≥ 6:11, ancião
//     6:20 depois do último dragão). O dado só limita por cima: o renascimento
//     não passa de 5:06 (dragão) e 6:11 (barão);
//   - acampamentos: os 20 caçadores têm 8–12 de CS de selva no quadro de 2:00
//     e dois deles 1–2 já no de 1:00 (impossível com o nascimento a 1:30); as
//     primeiras limpezas deduzidas do rastreio caem entre 1:00 e 1:40 (krugues
//     1:31–1:54, antes do 1:42 antigo) e as do caranguejo entre 3:02 e 3:22
//     (antes do 3:30 antigo). Sapo e krugues do próprio caçador voltam com
//     intervalo ≥ 2:04 (perto do 2:15); a série do azul do Master Yi da
//     3305449546 (6:23, 5:53, 5:35, 5:53) é coerente com 5:00. A dedução da
//     limpeza (perto do centro usando habilidade) também pega quem só passa,
//     por isso há intervalos curtos que não contradizem a tabela.
//
// O OURO de cada acampamento NÃO é tempo de jogo: é o equivalente escolhido à
// mão para pôr a janela em ouro (não conferido em fonte).

// TempoAcampamento: quando o acampamento nasce, em quanto volta depois de
// limpo e o ouro equivalente.
type TempoAcampamento struct {
	PrimeiroMs int64
	VoltaMs    int64
	Ouro       int
}

// TempoEpico: o primeiro nascimento e o renascimento de um monstro épico.
// VoltaMs 0 = não renasce. AposAlmaMs (só o ancião): ele nasce isso depois do
// dragão que deu a alma a um time, e não num instante fixo.
type TempoEpico struct {
	PrimeiroMs int64
	VoltaMs    int64
	AposAlmaMs int64
	// SomeMs: some de vez nesse instante (vastilarvas, arauto); 0 = não some.
	SomeMs int64
}

// TemposSelvaPatch: a linha da tabela de um intervalo de patches.
type TemposSelvaPatch struct {
	// Patches: o intervalo no nome da Riot ("26.1 em diante"); DesdePatch e
	// AtePatch no número do cliente, como a partida guarda ("16.1"; "" = sem fim).
	Patches      string
	DesdePatch   string
	AtePatch     string
	Acampamentos map[string]TempoAcampamento // pela primeira palavra do nome do acampamento
	Epicos       map[string]TempoEpico       // dragao, anciao, vastilarvas, arauto, barao, atakhan
	Fonte        string
}

// ouroAcampamento: o equivalente em ouro (escolhido à mão; igual em todo patch).
var ouroAcampamento = map[string]int{"azul": 100, "vermelho": 100, "sapo": 80, "lobos": 85, "aves": 85, "krugues": 100, "caranguejo": 55}

func acampamentosCom(buffs, pequenos, sapoKrugues, caranguejo int64) map[string]TempoAcampamento {
	t := func(tipo string, primeiro, volta int64) TempoAcampamento {
		return TempoAcampamento{PrimeiroMs: primeiro, VoltaMs: volta, Ouro: ouroAcampamento[tipo]}
	}
	return map[string]TempoAcampamento{
		// Renascimento: buffs 5:00 e pequenos 2:15 desde o V11.10 [W]; o
		// caranguejo 2:30 desde o V8.11 [W]. Nenhum patch de 2025/2026 mudou.
		"azul":       t("azul", buffs, 300000),
		"vermelho":   t("vermelho", buffs, 300000),
		"lobos":      t("lobos", pequenos, 135000),
		"aves":       t("aves", pequenos, 135000),
		"sapo":       t("sapo", sapoKrugues, 135000),
		"krugues":    t("krugues", sapoKrugues, 135000),
		"caranguejo": t("caranguejo", caranguejo, 150000),
	}
}

// TabelaTemposSelva: da mais nova para a mais velha.
var TabelaTemposSelva = []TemposSelvaPatch{
	{
		Patches: "26.1 em diante", DesdePatch: "16.1",
		// V26.01 [W][N]: buffs, lobos e aves 0:55 (eram 1:30); sapo e krugues
		// 1:07 (eram 1:42); caranguejo 2:55 (era 3:30).
		Acampamentos: acampamentosCom(55000, 55000, 67000, 175000),
		Epicos: map[string]TempoEpico{
			// Dragão: nasce aos 5:00 e volta 5 min depois de morto [W Dragon pit];
			// o 26.1 não mudou ("unchanged unless noted") [N].
			"dragao": {PrimeiroMs: 300000, VoltaMs: 300000},
			// Ancião: 6 min depois do dragão da alma e 6 min de renascimento
			// (V14.3, correção do V14.2) [W Dragon pit].
			"anciao": {AposAlmaMs: 360000, VoltaMs: 360000},
			// Vastilarvas: um grupo de três aos 8:00, sem renascer (V25.09),
			// somem aos 14:45 [W Voidgrub].
			"vastilarvas": {PrimeiroMs: 480000, SomeMs: 885000},
			// Arauto: 15:00 (V25.09), sem renascer (V14.1), some aos 19:45 [W Rift_Herald].
			"arauto": {PrimeiroMs: 900000, SomeMs: 1185000},
			// Barão: 20:00 (V26.01, era 25:00) e 6:00 de renascimento (V8.23) [W][N].
			"barao": {PrimeiroMs: 1200000, VoltaMs: 360000},
			// Atakhan: removido no V26.01 [W Atakhan][N].
		},
		Fonte: "Wiki (infobox e Patch History de cada monstro) e notas do patch 26.1, consultadas em 18/11/2026",
	},
	{
		Patches: "25.9 a 25.24", DesdePatch: "15.9", AtePatch: "15.24",
		// Antes do V26.01 [W]: buffs, lobos e aves 1:30 (V7.22/V8.24); sapo e
		// krugues 1:42 (V7.22); caranguejo 3:30 (V12.22).
		Acampamentos: acampamentosCom(90000, 90000, 102000, 210000),
		Epicos: map[string]TempoEpico{
			"dragao":      {PrimeiroMs: 300000, VoltaMs: 300000},
			"anciao":      {AposAlmaMs: 360000, VoltaMs: 360000},
			"vastilarvas": {PrimeiroMs: 480000, SomeMs: 885000},
			// Arauto: some aos 24:45 enquanto o barão nascia aos 25:00 (V25.S1.1) [W Rift_Herald].
			"arauto": {PrimeiroMs: 900000, SomeMs: 1485000},
			// Barão a 25:00 no ano do Atakhan (V25.S1.1) [W Baron_Nashor].
			"barao": {PrimeiroMs: 1500000, VoltaMs: 360000},
			// Atakhan: 20:00, uma vez [W Atakhan, infobox histórica].
			"atakhan": {PrimeiroMs: 1200000},
		},
		Fonte: "Wiki (Patch History de cada monstro), consultada em 18/11/2026",
	},
	{
		Patches: "25.1 a 25.8", DesdePatch: "15.1", AtePatch: "15.8",
		Acampamentos: acampamentosCom(90000, 90000, 102000, 210000),
		Epicos: map[string]TempoEpico{
			"dragao": {PrimeiroMs: 300000, VoltaMs: 300000},
			"anciao": {AposAlmaMs: 360000, VoltaMs: 360000},
			// Vastilarvas aos 6:00 com renascimento de 4 min até o V25.09 [W Voidgrub].
			"vastilarvas": {PrimeiroMs: 360000, VoltaMs: 240000, SomeMs: 825000},
			// Arauto aos 16:00 (V25.S1.1; 15:00 no V25.09) [W Rift_Herald].
			"arauto":  {PrimeiroMs: 960000, SomeMs: 1485000},
			"barao":   {PrimeiroMs: 1500000, VoltaMs: 360000},
			"atakhan": {PrimeiroMs: 1200000},
		},
		Fonte: "Wiki (Patch History de cada monstro), consultada em 18/11/2026; o fim das vastilarvas antes do V25.09 não foi conferido",
	},
}

// numeroPatch: "16.22" → (16, 22); "16.22.834.8910" também. ok falso se não der.
func numeroPatch(p string) (int, int, bool) {
	partes := strings.SplitN(p, ".", 3)
	if len(partes) < 2 {
		return 0, 0, false
	}
	a, e1 := strconv.Atoi(partes[0])
	b, e2 := strconv.Atoi(partes[1])
	return a, b, e1 == nil && e2 == nil
}

func patchMenor(a, b string) bool {
	a1, a2, _ := numeroPatch(a)
	b1, b2, _ := numeroPatch(b)
	return a1 < b1 || (a1 == b1 && a2 < b2)
}

// TemposDaSelva: a linha da tabela do patch da partida ("16.22"). Patch vazio
// ou ilegível usa a mais nova; patch fora da tabela (mais velho que ela) usa a
// mais velha. coberto diz se o patch está dentro de alguma linha.
func TemposDaSelva(patch string) (TemposSelvaPatch, bool) {
	if _, _, ok := numeroPatch(patch); !ok {
		return TabelaTemposSelva[0], false
	}
	for _, l := range TabelaTemposSelva {
		if patchMenor(patch, l.DesdePatch) {
			continue
		}
		if l.AtePatch != "" && patchMenor(l.AtePatch, patch) {
			continue
		}
		return l, true
	}
	return TabelaTemposSelva[len(TabelaTemposSelva)-1], false
}

// primeiroAcampamentoMs: o primeiro nascimento mais cedo dos acampamentos de
// um lado (antes disso não há o que roubar: não há janela de selva).
func (t TemposSelvaPatch) primeiroAcampamentoMs() int64 {
	m := int64(-1)
	for tipo, a := range t.Acampamentos {
		if tipo == "caranguejo" {
			continue // fica no rio, não é de um lado
		}
		if m < 0 || a.PrimeiroMs < m {
			m = a.PrimeiroMs
		}
	}
	return m
}

// tipoEpico: o objetivo da linha do tempo ("objetivo", "DRAGON:FIRE_DRAGON")
// na chave de TemposSelvaPatch.Epicos; "" se não for monstro épico.
func tipoEpico(tipo, sub string) string {
	if tipo != "objetivo" {
		return ""
	}
	switch {
	case sub == "DRAGON:ELDER_DRAGON":
		return "anciao"
	case strings.HasPrefix(sub, "DRAGON"):
		return "dragao"
	case sub == "BARON_NASHOR":
		return "barao"
	case sub == "RIFTHERALD":
		return "arauto"
	case sub == "HORDE":
		return "vastilarvas"
	case sub == "ATAKHAN":
		return "atakhan"
	}
	return ""
}
