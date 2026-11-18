package partida

// NomeDaFila traduz o queueId para o nome que a tela mostra. Fila que não está
// aqui aparece como "fila <id>": melhor do que esconder a partida.
func NomeDaFila(id int) string {
	nomes := map[int]string{
		0:    "personalizada",
		400:  "normal (escolha)",
		420:  "ranqueada solo",
		430:  "normal às cegas",
		440:  "ranqueada flex",
		450:  "ARAM",
		480:  "partida rápida",
		490:  "partida rápida",
		700:  "Clash",
		830:  "contra IA (introdução)",
		840:  "contra IA (iniciante)",
		850:  "contra IA (intermediário)",
		900:  "ARURF",
		1700: "Arena",
		1710: "Arena",
		1900: "URF",
		2400: "ARAM: Caos",
	}
	if n, ok := nomes[id]; ok {
		return n
	}
	return "fila " + itoa(int64(id))
}

// posicoesPorOrdem é a ordem dos participantes dentro de cada time no LCU em
// Summoner's Rift: topo, selva, meio, atirador, suporte. Os campos lane/role
// do LCU NÃO servem (na 3304594274 o Malphite do topo veio como JUNGLE e o
// Akshan do meio, no histórico, como BOTTOM/CARRY); a ordem bateu com o que se
// viu no replay. Os nomes são os do `position` do Live Client Data, para o
// rastreio (tarefa 2) cruzar sem tradução.
var posicoesPorOrdem = []string{"TOP", "JUNGLE", "MIDDLE", "BOTTOM", "UTILITY"}

// PosicaoDoParticipante é a posição pelo participantId sozinho (1–5 o time
// azul, 6–10 o vermelho, na mesma ordem de posicoesPorOrdem), para quando só o
// item do histórico está à mão (ele traz só o participante dono). Mesma regra
// de participantesDe; "" fora de Summoner's Rift ou com id fora de 1–10.
func PosicaoDoParticipante(mapa, participanteID int) string {
	if mapa != 11 || participanteID < 1 || participanteID > 10 {
		return ""
	}
	return posicoesPorOrdem[(participanteID-1)%5]
}
