package analise

import (
	"encoding/json"
	"fmt"

	"loltracking/server/banco"
	"loltracking/server/partida"
	"loltracking/server/rastreio"
)

// Checagem de qualidade da ANÁLISE (a segunda metade de qualidade.go): o dado gravado pode
// estar bom e a dedução feita em cima dele, não. Roda no fim de Recalcula, com as cadeias já
// refeitas e os retratos montados, e acrescenta pontos ao resultado de cada passe. Só aponta.

// Limiares da checagem da análise.
const (
	// PiscaJanelaMs e PiscaTrocas: a atividade troca PiscaTrocas vezes ou mais dentro de
	// PiscaJanelaMs (trechos seguidos com a mesma atividade e só o "com" diferente não contam).
	PiscaJanelaMs = 10000
	PiscaTrocas   = 4
	// FracaoImpossivel: fração das amostras do trecho que contradiz a etiqueta para apontar.
	FracaoImpossivel = 0.5
	// CSMinimoFuncao: abaixo disso de CS do tipo "errado" não se aponta (poucos abates de tropa
	// do caçador que passa na rota, ou o caranguejo do atirador, são normais).
	CSMinimoFuncao = 20
	// FolgaAproxMs: a luta precisa começar até isso depois do fim da aproximação.
	FolgaAproxMs = 3000
)

// confereAnaliseDaPartida acrescenta as checagens da análise a cada passe já conferido e grava.
func confereAnaliseDaPartida(db *banco.DB, gameID int64, dp *dadosPartida, retratos []Retrato, passes []rastreio.Passe, qs map[int64]*QualidadePasse) error {
	funcao := funcaoIncoerente(db, gameID, dp)
	for _, p := range passes {
		q, ok := qs[p.ID]
		if !ok || q.Contagem[QArquivo] > 0 {
			continue
		}
		as, err := rastreio.AmostrasDoPasse(db, p)
		if err != nil {
			continue
		}
		cad, err := rastreio.CadeiaDoPasse(db, p)
		if err != nil {
			continue
		}
		part := dp.participante(p.ParticipanteID)
		time := 0
		if part != nil {
			time = part.Time
		}
		confereEtiquetas(q, cad, as, time)
		confereOndas(q, cad, dp)
		for _, f := range funcao[p.ParticipanteID] {
			q.add(QFuncaoIncoerente, f.TMs, f.Texto)
		}
		for _, rt := range retratos {
			if rt.Aproximado && (rt.Vitima == p.ParticipanteID || rt.Matador == p.ParticipanteID) {
				papel := "abateu"
				if rt.Vitima == p.ParticipanteID {
					papel = "morreu"
				}
				q.add(QRetratoAproximado, rt.TMs, fmt.Sprintf("%s aos %s: o retrato tem posição aproximada de alguém (não rastreado ou só por minuto)", papel, MinSeg(rt.TMs)))
			}
		}
	}
	return gravaQualidade(db, gameID, qs)
}

// trechoDaCadeia: um item "atividade" da cadeia.
type trechoDaCadeia struct {
	ini, fim int64
	atv      string
}

func atividadesDaCadeia(cad []rastreio.ItemCadeia) []trechoDaCadeia {
	var l []trechoDaCadeia
	for _, it := range cad {
		if it.Tipo != "atividade" {
			continue
		}
		a, _ := it.Dados["atividade"].(string)
		l = append(l, trechoDaCadeia{ini: it.TMs, fim: int64(numero(it.Dados["fim_ms"])), atv: a})
	}
	return l
}

// confereEtiquetas: etiqueta impossível, etiqueta piscando e aproximação sem luta.
func confereEtiquetas(q *QualidadePasse, cad []rastreio.ItemCadeia, as []rastreio.Amostra, time int) {
	ts := atividadesDaCadeia(cad)
	base := Ponto{CantoBaseAzul.X, CantoBaseAzul.Y}
	if time == 200 {
		base = espelho(CantoBaseAzul)
	}
	for _, t := range ts {
		var n, mortos, foraBase int
		for i := range as {
			a := as[i]
			if a.TMs < t.ini || a.TMs >= t.fim {
				continue
			}
			n++
			if a.Morto {
				mortos++
			} else if a.X != nil && a.Y != nil && dist(Ponto{*a.X, *a.Y}, base) >= RaioBase {
				foraBase++
			}
		}
		if n == 0 {
			continue
		}
		fr := func(k int) float64 { return float64(k) / float64(n) }
		switch {
		case t.atv == AMorto && fr(n-mortos) > FracaoImpossivel:
			q.add(QEtiquetaImpossivel, t.ini, fmt.Sprintf("\"morto\" de %s a %s com o jogador vivo em %.0f%% das amostras", MinSeg(t.ini), MinSeg(t.fim), fr(n-mortos)*100))
		case t.atv != AMorto && fr(mortos) > FracaoImpossivel:
			q.add(QEtiquetaImpossivel, t.ini, fmt.Sprintf("\"%s\" de %s a %s com o jogador morto em %.0f%% das amostras", t.atv, MinSeg(t.ini), MinSeg(t.fim), fr(mortos)*100))
		case t.atv == ANaBase && fr(foraBase) > FracaoImpossivel:
			q.add(QEtiquetaImpossivel, t.ini, fmt.Sprintf("\"na base\" de %s a %s longe da base em %.0f%% das amostras", MinSeg(t.ini), MinSeg(t.fim), fr(foraBase)*100))
		}
	}
	// Piscando: só as trocas de atividade (não as de "com").
	var trocas []int64
	for i := 1; i < len(ts); i++ {
		if ts[i].atv != ts[i-1].atv {
			trocas = append(trocas, ts[i].ini)
		}
	}
	for i := 0; i+PiscaTrocas-1 < len(trocas); {
		j := i + PiscaTrocas - 1
		if trocas[j]-trocas[i] <= PiscaJanelaMs {
			k := j
			for k+1 < len(trocas) && trocas[k+1]-trocas[i] <= PiscaJanelaMs {
				k++
			}
			q.add(QEtiquetaPiscando, trocas[i], fmt.Sprintf("%d trocas de atividade em %s s (%s a %s)", k-i+1,
				segundosDec(trocas[k]-trocas[i]), MinSeg(trocas[i]), MinSeg(trocas[k])))
			i = k + 1
			continue
		}
		i++
	}
	// Aproximação sem luta em seguida.
	for i, t := range ts {
		if t.atv != AGankando && t.atv != ARoaming && t.atv != ARotacionando && t.atv != ACounterGank {
			continue
		}
		ok := false
		for j := i + 1; j < len(ts) && ts[j].ini <= t.fim+FolgaAproxMs; j++ {
			if combate(ts[j].atv) {
				ok = true
				break
			}
		}
		if !ok {
			q.add(QAproxSemLuta, t.ini, fmt.Sprintf("\"%s\" de %s a %s sem luta, troca ou dive até %d s depois", t.atv, MinSeg(t.ini), MinSeg(t.fim), FolgaAproxMs/1000))
		}
	}
}

// confereOndas: estado de onda num trecho em que nenhum jogador rastreado daquela rota esteve
// nela (vivo, na zona da rota). A posição da onda sai deles: sem ninguém, o estado foi esticado.
func confereOndas(q *QualidadePasse, cad []rastreio.ItemCadeia, dp *dadosPartida) {
	for _, it := range cad {
		switch it.Tipo {
		case EOndaFreeze, EOndaNaTorre, EOndaVoltando, EOndaEmpurra, EOndaParada:
		default:
			continue
		}
		rota, _ := it.Dados["rota"].(string)
		fim := int64(numero(it.Dados["fim_ms"]))
		presente := false
		for _, part := range dp.pt.Participantes {
			if !daRota(rota, part.Posicao) {
				continue
			}
			for _, a := range dp.amostras[part.ParticipanteID] {
				if a.TMs < it.TMs || a.TMs > fim || a.Morto || a.X == nil || a.Y == nil {
					continue
				}
				if z, _ := Zona(*a.X, *a.Y); z == rota {
					presente = true
					break
				}
			}
			if presente {
				break
			}
		}
		if !presente {
			q.add(QOndaSemJogador, it.TMs, fmt.Sprintf("onda \"%s\" na %s de %s a %s sem nenhum jogador rastreado da rota nela",
				it.Tipo, Info(rota).Nome, MinSeg(it.TMs), MinSeg(fim)))
		}
	}
}

// funcaoIncoerente: pelo CS de tropa × monstro da linha do tempo do LCU no último quadro da
// fase de rotas (até 14 min): caçador com mais CS de rota que de selva, ou quem é de rota com
// mais de selva que de rota. Por participante (vale para todos os passes dele).
func funcaoIncoerente(db *banco.DB, gameID int64, dp *dadosPartida) map[int][]Momento {
	r := map[int][]Momento{}
	bruto, err := partida.LeLinhaDoTempo(db, gameID)
	if err != nil {
		return r
	}
	var lt struct {
		Frames []struct {
			Timestamp         int64 `json:"timestamp"`
			ParticipantFrames map[string]struct {
				ParticipantID       int `json:"participantId"`
				MinionsKilled       int `json:"minionsKilled"`
				JungleMinionsKilled int `json:"jungleMinionsKilled"`
			} `json:"participantFrames"`
		} `json:"frames"`
	}
	if json.Unmarshal(bruto, &lt) != nil || len(lt.Frames) == 0 {
		return r
	}
	k := 0
	for i, f := range lt.Frames {
		if f.Timestamp <= Param.FaseRotasMs+1000 {
			k = i
		}
	}
	f := lt.Frames[k]
	for _, pf := range f.ParticipantFrames {
		part := dp.participante(pf.ParticipantID)
		if part == nil || part.Posicao == "" {
			continue
		}
		rota, selva := pf.MinionsKilled, pf.JungleMinionsKilled
		switch {
		case part.Posicao == "JUNGLE" && rota > selva && rota >= CSMinimoFuncao:
			r[pf.ParticipantID] = append(r[pf.ParticipantID], Momento{f.Timestamp, fmt.Sprintf(
				"caçador com %d de CS de rota e %d de selva aos %s", rota, selva, MinSeg(f.Timestamp))})
		case part.Posicao != "JUNGLE" && selva > rota && selva >= CSMinimoFuncao:
			r[pf.ParticipantID] = append(r[pf.ParticipantID], Momento{f.Timestamp, fmt.Sprintf(
				"%s com %d de CS de selva e %d de rota aos %s", part.Posicao, selva, rota, MinSeg(f.Timestamp))})
		}
	}
	return r
}
