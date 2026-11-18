package rastreio

import (
	"encoding/json"
	"math"
	"sort"
	"strings"

	"loltracking/server/banco"
)

// Buracos são os trechos de um passe em que o jogador estava vivo e o motor
// não tem a posição DE VERDADE dele: ou não há amostra nenhuma (o motor parou
// para retravar, antes de a retravada passar a gravar sem posição), ou há
// amostras com a trava perdida (sem a câmera nele). É a mesma regra para a
// checagem de qualidade (analise, tipo "buraco"), para a repescagem ao fim do
// passe e para a rota de repescagem de um passe já gravado.

// Limites da repescagem (motor.go e repescagem.go).
var (
	// LimiarRepescagemMs: buraco com o jogador vivo maior que isto (tempo de
	// jogo) é repescado. Abaixo disso a interpolação entre as duas leituras boas
	// (finalizaPasse) erra pouco: a ~350 unidades/s, 3 s andando são ~1 000. O
	// pedido era começar em 4 s, mas na 3304217746 cinco dos oito buracos têm
	// 3,8–3,9 s (uma tentativa de trava a 4×): com 4 s nenhum deles seria
	// repescado. 3 s é também o BuracoMs da checagem de qualidade.
	LimiarRepescagemMs int64 = 3000
	// MaxRepescagensPorPasse: no máximo tantos buracos repescados por passe (cada
	// um custa um seek, uma trava e o trecho de novo; o passe da Orianna na
	// 3304217746 tinha 3). O que passar fica registrado como restante.
	MaxRepescagensPorPasse = 8
	// TentativasExtrasRepescagem: quantas vezes a mais tentar um buraco que não
	// fechou na primeira repescagem.
	TentativasExtrasRepescagem = 1
	// FolgaRepescagemMs: o trecho regravado começa e termina tanto antes e
	// depois do buraco (as leituras boas dos dois lados são refeitas também, e o
	// trecho novo substitui o velho inteiro).
	FolgaRepescagemMs int64 = 2000
	// SobraRepescagemS: tempo real fixo de uma repescagem além do trecho (seek,
	// esperar o replay chegar, travar a câmera). Só para a estimativa.
	SobraRepescagemS = 3.0
	// BuracoTipicoMs e BuracosEsperadosPorPasse: para a estimativa de um passe
	// que ainda não rodou (na 3304217746: 6 buracos em 10 passes, o maior 14,8 s).
	BuracoTipicoMs           int64 = 8000
	BuracosEsperadosPorPasse       = 0.6
)

// Buraco é um trecho sem a posição de verdade do jogador vivo. DeMs é a última
// leitura boa antes (ou a primeira amostra do passe) e AteMs a primeira leitura
// boa depois (ou a última amostra). Amostras = quantas amostras sem posição de
// verdade há dentro (0 = vazio de verdade, o motor não gravou nada).
type Buraco struct {
	DeMs     int64  `json:"de_ms"`
	AteMs    int64  `json:"ate_ms"`
	Amostras int    `json:"amostras"`
	Motivo   string `json:"motivo,omitempty"`
	// Tipo: "" (buraco comum: a câmera saiu do jogador e dá para refazer) ou
	// BuracoNaoSelecionavel (ClassificaBuracos).
	Tipo string `json:"tipo,omitempty"`
}

// BuracoNaoSelecionavel: trecho em que o JOGO tira o jogador da tela com a
// seleção nele (descobertas 8, partida 3305449546): teleporte (o feitiço,
// ~3 s depois de começar a canalizar o campeão some e reaparece a milhares
// de unidades), montar no Arauto do Vale, Investida Alfa do Master Yi. Não
// é defeito da captura: a repescagem não insiste e a checagem de qualidade
// não conta como buraco.
const BuracoNaoSelecionavel = "nao_selecionavel"

// prefixoMotivoReproduzido é o começo do motivo que a repescagem gravava
// antes do tipo existir quando o trecho regravado continuava sem a câmera:
// é a prova de que o trecho se repete (ClassificaBuracos).
const prefixoMotivoReproduzido = "o trecho regravado ainda ficou"

// DuracaoMs do buraco.
func (b Buraco) DuracaoMs() int64 { return b.AteMs - b.DeMs }

// MarshalJSON acrescenta duracao_ms (a tela não precisa fazer a conta).
func (b Buraco) MarshalJSON() ([]byte, error) {
	type semMetodo Buraco
	return json.Marshal(struct {
		semMetodo
		DuracaoMs int64 `json:"duracao_ms"`
	}{semMetodo(b), b.DuracaoMs()})
}

// LeituraBoa: a amostra traz a posição de verdade do jogador (câmera nele,
// morto no local da morte, ou na fonte logo depois de renascer).
func LeituraBoa(a Amostra) bool {
	if a.TravaPerdida {
		return false
	}
	return a.Origem == OrigemCamera || a.Origem == OrigemMorte || a.Origem == OrigemBase
}

// Buracos acha os buracos maiores que limiarMs nas amostras (em ordem de
// tempo). Entre duas leituras boas com o jogador morto nas duas não é buraco
// (morto, a posição é o local da morte de qualquer jeito).
func Buracos(as []Amostra, limiarMs int64) []Buraco {
	var bs []Buraco
	ult := -1 // última leitura boa
	semPos := 0
	for i := range as {
		if !LeituraBoa(as[i]) {
			semPos++
			continue
		}
		switch {
		case ult < 0:
			if as[i].TMs-as[0].TMs > limiarMs && !as[i].Morto {
				bs = append(bs, Buraco{DeMs: as[0].TMs, AteMs: as[i].TMs, Amostras: semPos})
			}
		case as[i].TMs-as[ult].TMs > limiarMs && !(as[ult].Morto && as[i].Morto):
			bs = append(bs, Buraco{DeMs: as[ult].TMs, AteMs: as[i].TMs, Amostras: semPos})
		}
		ult, semPos = i, 0
	}
	if n := len(as); n > 0 && ult >= 0 && ult < n-1 && as[n-1].TMs-as[ult].TMs > limiarMs && !as[ult].Morto {
		bs = append(bs, Buraco{DeMs: as[ult].TMs, AteMs: as[n-1].TMs, Amostras: semPos})
	}
	if len(as) > 0 && ult < 0 && as[len(as)-1].TMs-as[0].TMs > limiarMs {
		bs = append(bs, Buraco{DeMs: as[0].TMs, AteMs: as[len(as)-1].TMs, Amostras: semPos})
	}
	return bs
}

// fechou diz se as amostras regravadas cobrem o buraco b sem deixar, dentro
// dele, outro buraco maior que o limiar.
func fechou(novas []Amostra, b Buraco, limiarMs int64) (bool, int64) {
	if len(novas) == 0 {
		return false, b.DuracaoMs()
	}
	if novas[0].TMs > b.DeMs+limiarMs || novas[len(novas)-1].TMs < b.AteMs-limiarMs {
		return false, b.DuracaoMs()
	}
	pior := int64(0)
	for _, r := range Buracos(novas, limiarMs) {
		if r.AteMs > b.DeMs && r.DeMs < b.AteMs {
			pior = max(pior, r.DuracaoMs())
		}
	}
	return pior == 0, pior
}

// ClassificaBuracos marca com BuracoNaoSelecionavel os buracos bs (achados
// em as, em ordem de tempo, do jogador do time 100/200) em que é o jogo que
// tira o jogador da tela. Vale quando a perda da câmera começou com a seleção
// nele (a câmera não foi para outro lugar: o campeão sumiu dela) e
//   - a repescagem já regravou o trecho e a perda se repetiu (rep: um
//     restante que cruza o buraco, com o tipo ou com o motivo antigo); ou
//   - as duas leituras boas em volta estão mais longe uma da outra do que se
//     anda no tempo do buraco e a de depois não é a fonte do time: teleporte
//     (na 3305449546, todos os buracos de 4–7 s que sobraram, de 7 500 a
//     15 800 unidades; a volta à base não entra: a câmera vai junto).
func ClassificaBuracos(as []Amostra, bs []Buraco, time int, rep *ResumoRepescagem) {
	for i := range bs {
		if !selecionadoNoComeco(as, bs[i]) {
			continue
		}
		if reproduzido(bs[i], rep) || teleporte(as, bs[i], time) {
			bs[i].Tipo = BuracoNaoSelecionavel
		}
	}
}

// AmostrasSelecionadoNoComeco: quantas das primeiras amostras sem a câmera de
// um buraco precisam ainda ter a seleção no jogador (~0,75 s de jogo a 4 por
// segundo, antes da primeira tecla da retravada). Nos 6 buracos da
// 3305449546 a seleção ficou nele pelo menos nas 5 primeiras.
const AmostrasSelecionadoNoComeco = 3

// selecionadoNoComeco: as primeiras amostras sem a câmera dentro do buraco
// ainda tinham a seleção no jogador. Buraco vazio ou com menos amostras que
// isso não diz nada.
func selecionadoNoComeco(as []Amostra, b Buraco) bool {
	i := sort.Search(len(as), func(k int) bool { return as[k].TMs > b.DeMs })
	n := 0
	for ; i < len(as) && as[i].TMs < b.AteMs && n < AmostrasSelecionadoNoComeco; i++ {
		if !as[i].TravaPerdida {
			continue
		}
		if !as[i].SelecaoOK {
			return false
		}
		n++
	}
	return n == AmostrasSelecionadoNoComeco
}

// reproduzido: a repescagem já regravou um trecho que cruza b e a câmera
// saiu de novo.
func reproduzido(b Buraco, rep *ResumoRepescagem) bool {
	if rep == nil {
		return false
	}
	for _, r := range rep.Restantes {
		if r.DeMs < b.AteMs && b.DeMs < r.AteMs && (r.Tipo == BuracoNaoSelecionavel || strings.HasPrefix(r.Motivo, prefixoMotivoReproduzido)) {
			return true
		}
	}
	return false
}

// teleporte: as leituras boas nas pontas do buraco estão mais longe do que se
// anda no tempo dele (a mesma conta de PodeInterpolar), sem morte no meio e
// sem a de depois ser a fonte do time.
func teleporte(as []Amostra, b Buraco, time int) bool {
	ia := sort.Search(len(as), func(k int) bool { return as[k].TMs >= b.DeMs })
	iz := sort.Search(len(as), func(k int) bool { return as[k].TMs >= b.AteMs })
	if ia >= len(as) || iz >= len(as) || as[ia].TMs != b.DeMs || as[iz].TMs != b.AteMs {
		return false
	}
	a, z := as[ia], as[iz]
	if !temPos(a) || !temPos(z) || a.Morto || z.Morto || a.Mortes != z.Mortes {
		return false
	}
	if time == 100 || time == 200 {
		f := FonteDoTime(time)
		if math.Hypot(*z.X-f[0], *z.Y-f[1]) < raioFonte {
			return false
		}
	}
	d := math.Hypot(*z.X-*a.X, *z.Y-*a.Y)
	return d > VelocidadeMaxInterpolacao*float64(z.TMs-a.TMs)/1000+FolgaSaltoInterpolacao
}

// repetiu: no trecho regravado novas, a câmera saiu de novo dentro de b, com
// a seleção no jogador no começo da perda (ClassificaBuracos).
func repetiu(novas []Amostra, b Buraco) bool {
	for _, r := range Buracos(novas, LimiarRepescagemMs) {
		if r.AteMs > b.DeMs && r.DeMs < b.AteMs && selecionadoNoComeco(novas, r) {
			return true
		}
	}
	return false
}

// ResumoRepescagem é o que a repescagem de um passe fez (coluna repescagem de
// passes, evento passe_fim e repescagem_fim). Quando: "fim_do_passe" (o motor,
// com o replay ainda aberto) ou "depois" (a rota de repescagem de um passe já
// gravado).
type ResumoRepescagem struct {
	Quando      string   `json:"quando"`
	Buracos     int      `json:"buracos"`
	Recuperados int      `json:"recuperados"`
	Sobraram    int      `json:"sobraram"`
	Restantes   []Buraco `json:"restantes"`
	FeitaEm     string   `json:"feita_em"`
}

func gravaResumoRepescagem(db *banco.DB, passeID int64, r ResumoRepescagem) error {
	if r.Restantes == nil {
		r.Restantes = []Buraco{}
	}
	r.Sobraram = len(r.Restantes)
	if r.FeitaEm == "" {
		r.FeitaEm = banco.Agora()
	}
	_, err := db.Exec(`UPDATE passes SET repescagem = ? WHERE id = ?`, jsonTexto(r), passeID)
	return err
}

// --- buraco curto no mapa -----------------------------------------------------

var (
	// BuracoCurtoMs: buraco (tempo de jogo) entre duas leituras boas que o mapa
	// com play e o fechamento do passe preenchem em linha reta, com origem
	// "interpolada". Mais longo que isso, o jogador pode ter ido a qualquer
	// lugar: o mapa cai na posição por minuto da linha do tempo.
	BuracoCurtoMs int64 = 20000
	// VelocidadeMaxInterpolacao e FolgaSaltoInterpolacao (unidades/s e
	// unidades): duas leituras mais longe uma da outra que isto × o tempo + a
	// folga não foram andando (teleporte, recall que não chegou a ser visto):
	// não se liga em linha reta (descobertas 6). Campeão rápido com botas anda
	// ~450–550; a folga cobre um Flash (400) e um avanço.
	VelocidadeMaxInterpolacao = 700.0
	FolgaSaltoInterpolacao    = 1000.0
)

// PodeInterpolar diz se dá para ligar a e b (duas leituras com posição) em
// linha reta: os dois vivos, sem morte entre eles (o placar de mortes não
// mudou), sem chegada à fonte vinda de fora dela (volta à base: o salto é
// real) e sem salto maior que o que se anda no intervalo. time 100/200 (0 =
// não confere a fonte).
func PodeInterpolar(a, b Amostra, time int) bool {
	if !temPos(a) || !temPos(b) || a.Morto || b.Morto || a.Mortes != b.Mortes || b.TMs <= a.TMs {
		return false
	}
	if time == 100 || time == 200 {
		f := FonteDoTime(time)
		da, db := math.Hypot(*a.X-f[0], *a.Y-f[1]), math.Hypot(*b.X-f[0], *b.Y-f[1])
		if db < raioFonte && da >= raioFonte {
			return false
		}
	}
	d := math.Hypot(*b.X-*a.X, *b.Y-*a.Y)
	return d <= VelocidadeMaxInterpolacao*float64(b.TMs-a.TMs)/1000+FolgaSaltoInterpolacao
}

// estimativaRepescagemS: tempo real esperado da repescagem de um passe com n
// buracos de BuracoTipicoMs, à velocidade vel.
func estimativaRepescagemS(n, vel float64) float64 {
	if vel <= 0 || n <= 0 {
		return 0
	}
	trecho := float64(RecuoRetomadaMs+2*FolgaRepescagemMs+BuracoTipicoMs) / 1000 / vel
	return math.Round(n*(trecho+SobraRepescagemS)*10) / 10
}
