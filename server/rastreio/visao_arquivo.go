package rastreio

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"

	"loltracking/server/arquivo"
	"loltracking/server/banco"
	"loltracking/server/replay"
)

// O que o passe de visão deixa, em arquivo (pacote arquivo, um diretório por
// partida; a base guarda só o estado do passe):
//
//	visao-p<passe>-capturas.tsv  as capturas lidas, uma por linha:
//	                              "t_ms \t modo \t retratos \t sentinelas"
//	                              (marcas como em textoMarcas). É o dado bruto:
//	                              não depende das posições rastreadas.
//	visao.json                    o resultado (VisaoDaPartida): para cada time
//	                              observador, por inimigo, os intervalos
//	                              visível / fora da visão / indeterminado, e as
//	                              sentinelas lidas. DERIVADO das capturas e das
//	                              posições rastreadas: RecalculaVisao refaz
//	                              (no fim do passe de visão e em cada análise,
//	                              porque um passe de jogador novo ou repescado
//	                              muda as posições).

// NomeVisao é o arquivo do resultado.
const NomeVisao = "visao.json"

func nomeCapturasVisao(passeID int64) string { return fmt.Sprintf("visao-p%d-capturas.tsv", passeID) }

func textoCapturas(cs []CapturaVisao) []byte {
	var b bytes.Buffer
	for _, c := range cs {
		b.WriteString(strconv.FormatInt(c.TMs, 10))
		b.WriteByte('\t')
		b.WriteString(c.Modo)
		b.WriteByte('\t')
		b.WriteString(textoMarcas(c.Retratos))
		b.WriteByte('\t')
		b.WriteString(textoMarcas(c.Sentinelas))
		b.WriteByte('\n')
	}
	return b.Bytes()
}

func capturasDoTexto(texto []byte) ([]CapturaVisao, error) {
	var cs []CapturaVisao
	for n, linha := range bytes.Split(texto, []byte{'\n'}) {
		if len(linha) == 0 {
			continue
		}
		p := bytes.Split(linha, []byte{'\t'})
		if len(p) != 4 {
			return nil, fmt.Errorf("capturas da visão: linha %d fora do formato", n+1)
		}
		t, err := strconv.ParseInt(string(p[0]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("capturas da visão: linha %d: %w", n+1, err)
		}
		c := CapturaVisao{TMs: t, Modo: string(p[1])}
		if c.Retratos, err = marcasDoTexto(string(p[2])); err != nil {
			return nil, fmt.Errorf("capturas da visão: linha %d: %w", n+1, err)
		}
		if c.Sentinelas, err = marcasDoTexto(string(p[3])); err != nil {
			return nil, fmt.Errorf("capturas da visão: linha %d: %w", n+1, err)
		}
		cs = append(cs, c)
	}
	return cs, nil
}

// VisaoDaPartida é o visao.json (e a resposta de GET .../visao).
type VisaoDaPartida struct {
	GameID      int64  `json:"game_id"`
	CalculadaEm string `json:"calculada_em"`
	// Tolerâncias usadas na decisão (pixels do minimapa) e a escala.
	ToleranciaPx     float64       `json:"tolerancia_px"`
	OclusaoPx        float64       `json:"oclusao_px"`
	UnidadesPorPixel float64       `json:"unidades_por_pixel"`
	Times            []VisaoDoTime `json:"times"`
}

// VisaoDoTime é o resultado da visão de um time (o observador).
type VisaoDoTime struct {
	Time         int     `json:"time"`
	PasseVisaoID int64   `json:"passe_visao_id"`
	DeMs         int64   `json:"de_ms"`
	AteMs        int64   `json:"ate_ms"`
	Capturas     int     `json:"capturas"`
	CapturasPorS float64 `json:"capturas_por_s"`
	// ModoConferido: fração das capturas em que o painel dizia a névoa certa.
	ModoConferido float64 `json:"modo_conferido"`
	// AliadosAchados: fração dos retratos do PRÓPRIO time achados no lugar
	// rastreado (no próprio time o retrato aparece sempre): a conferência da
	// leitura. Nulo sem aliado rastreado.
	AliadosAchados *float64 `json:"aliados_achados"`
	// Jogadores: os 5 do outro time, como esse time os via.
	Jogadores  []VisibilidadeJogador `json:"jogadores"`
	Sentinelas []SentinelaLida       `json:"sentinelas"`
	// Aviso: frase quando a conferência pelos aliados ficou abaixo de
	// AliadosMinimo (a posição rastreada ou a geometria não confere).
	Aviso string `json:"aviso"`
}

// AliadosMinimo: abaixo desta fração de retratos do próprio time achados no
// lugar rastreado, o resultado do passe sai com aviso.
const AliadosMinimo = 0.8

// VisibilidadeJogador: por inimigo, intervalos [de, ate) em ms de jogo. Morto
// e sem captura ficam fora dos três.
type VisibilidadeJogador struct {
	ParticipanteID int        `json:"participante_id"`
	Time           int        `json:"time"`
	Rastreado      bool       `json:"rastreado"`
	Visivel        [][2]int64 `json:"visivel"`
	Fora           [][2]int64 `json:"fora"`
	Indeterminado  [][2]int64 `json:"indeterminado"`
	// Motivos: ms indeterminados por motivo (painel, aliados, sem_posicao,
	// anel_sem_dono, coberto, borda).
	Motivos map[string]int64 `json:"motivos"`
	// PctVisivel: visível ÷ (visível + fora), em %; nulo sem tempo decidido.
	PctVisivel *float64 `json:"pct_visivel"`
}

// SentinelaLida é uma sentinela do time observador achada no minimapa.
type SentinelaLida struct {
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	DeMs       int64   `json:"de_ms"`
	AteMs      int64   `json:"ate_ms"`
	Capturas   int     `json:"capturas"`
	Semelhanca float64 `json:"semelhanca"`
	// AteOFim: ainda estava lá na última captura do passe.
	AteOFim bool `json:"ate_o_fim"`
}

// EstadoEm devolve o estado no instante t ("" = sem captura ou morto).
func (v VisibilidadeJogador) EstadoEm(t int64) string {
	for _, l := range []struct {
		e  string
		is [][2]int64
	}{{VisVisivel, v.Visivel}, {VisFora, v.Fora}, {VisIndeterminado, v.Indeterminado}} {
		i := sort.Search(len(l.is), func(k int) bool { return l.is[k][1] > t })
		if i < len(l.is) && l.is[i][0] <= t {
			return l.e
		}
	}
	return ""
}

// Jogador devolve a visibilidade do participante para este observador.
func (v VisaoDoTime) Jogador(pid int) (VisibilidadeJogador, bool) {
	for _, j := range v.Jogadores {
		if j.ParticipanteID == pid {
			return j, true
		}
	}
	return VisibilidadeJogador{}, false
}

// DoObservador devolve a visão do time observador (ok falso se não há).
func (v *VisaoDaPartida) DoObservador(time int) (VisaoDoTime, bool) {
	if v == nil {
		return VisaoDoTime{}, false
	}
	for _, t := range v.Times {
		if t.Time == time {
			return t, true
		}
	}
	return VisaoDoTime{}, false
}

// LeVisao devolve o visao.json da partida (ok falso se não há).
func LeVisao(db *banco.DB, gameID int64) (*VisaoDaPartida, bool, error) {
	b, ok, err := arquivo.Le(db, gameID, NomeVisao)
	if err != nil || !ok {
		return nil, false, err
	}
	var v VisaoDaPartida
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, false, fmt.Errorf("ler %s: %w", NomeVisao, err)
	}
	return &v, true, nil
}

// melhorPasseVisao: o passe de visão que vale para o time — o concluído mais
// recente com arquivo; senão o cancelado mais recente com arquivo.
func melhorPasseVisao(db *banco.DB, gameID int64, time int) (PasseVisao, bool, error) {
	ps, err := lePassesVisao(db, `WHERE game_id = ? AND time = ? AND estado IN ('concluido','cancelado') AND capturas > 0
		ORDER BY (estado = 'concluido') DESC, id DESC`, gameID, time)
	if err != nil {
		return PasseVisao{}, false, err
	}
	for _, p := range ps {
		if arquivo.Existe(db, gameID, nomeCapturasVisao(p.ID)) {
			return p, true, nil
		}
	}
	return PasseVisao{}, false, nil
}

// fonteDePosicoes: o que se sabe de cada jogador ao longo da partida.
type fonteDePosicoes struct {
	times   map[int]int
	amostra map[int][]Amostra
	estados map[int][]EstadoJogador
}

func carregaPosicoes(db *banco.DB, gameID int64) (fonteDePosicoes, error) {
	f := fonteDePosicoes{times: map[int]int{}, amostra: map[int][]Amostra{}}
	linhas, err := db.Query(`SELECT participante_id, time FROM participantes WHERE game_id = ?`, gameID)
	if err != nil {
		return f, err
	}
	for linhas.Next() {
		var pid, t int
		if linhas.Scan(&pid, &t) == nil {
			f.times[pid] = t
		}
	}
	linhas.Close()
	for pid := range f.times {
		p, ok, err := melhorPasse(db, gameID, pid)
		if err != nil {
			return f, err
		}
		if !ok {
			continue
		}
		as, _, err := amostrasDoPasse(db, p)
		if err != nil {
			return f, err
		}
		if len(as) > 0 {
			f.amostra[pid] = as
		}
	}
	f.estados, err = EstadosDaPartida(db, gameID)
	return f, err
}

// PosicoesDaPartida devolve uma função que diz, para um instante, o que se
// sabe dos 10 (posição rastreada boa, morto) — a mesma entrada que a decisão
// de visibilidade usa. Para a sonda real e para conferências.
func PosicoesDaPartida(db *banco.DB, gameID int64) (func(t int64) []PosicaoNoInstante, error) {
	f, err := carregaPosicoes(db, gameID)
	if err != nil {
		return nil, err
	}
	var pids []int
	for pid := range f.times {
		pids = append(pids, pid)
	}
	sort.Ints(pids)
	return func(t int64) []PosicaoNoInstante {
		js := make([]PosicaoNoInstante, 0, len(pids))
		for _, pid := range pids {
			js = append(js, f.noInstante(pid, t))
		}
		return js
	}, nil
}

// boaPosicao: amostra com posição de verdade (nada estimado num buraco).
func boaPosicao(a Amostra) bool {
	return a.X != nil && a.Y != nil && !a.TravaPerdida &&
		(a.Origem == OrigemCamera || a.Origem == OrigemBase || a.Origem == OrigemMorte)
}

// noInstante monta o que se sabe do jogador pid no instante t.
func (f fonteDePosicoes) noInstante(pid int, t int64) PosicaoNoInstante {
	p := PosicaoNoInstante{ParticipanteID: pid, Time: f.times[pid]}
	if as := f.amostra[pid]; len(as) > 0 {
		i := sort.Search(len(as), func(k int) bool { return as[k].TMs >= t })
		// Morto: a amostra mais perto (até 1,5 s).
		perto := -1
		for _, k := range []int{i - 1, i} {
			if k >= 0 && k < len(as) && (perto < 0 || abs64(as[k].TMs-t) < abs64(as[perto].TMs-t)) {
				perto = k
			}
		}
		if perto >= 0 && abs64(as[perto].TMs-t) <= 1500 {
			p.Morto = as[perto].Morto
		}
		switch {
		case i > 0 && i < len(as) && boaPosicao(as[i-1]) && boaPosicao(as[i]) && as[i].TMs-as[i-1].TMs <= 1000 &&
			as[i-1].Morto == as[i].Morto:
			a, b := as[i-1], as[i]
			k := 0.0
			if b.TMs > a.TMs {
				k = float64(t-a.TMs) / float64(b.TMs-a.TMs)
			}
			p.X, p.Y, p.TemPosicao = *a.X+k*(*b.X-*a.X), *a.Y+k*(*b.Y-*a.Y), true
		case perto >= 0 && boaPosicao(as[perto]) && abs64(as[perto].TMs-t) <= 300:
			p.X, p.Y, p.TemPosicao = *as[perto].X, *as[perto].Y, true
		}
		if perto >= 0 && abs64(as[perto].TMs-t) <= 1500 {
			return p
		}
	}
	// Não rastreado (ou sem amostra perto): o morto vem do estado dos 10.
	if es := f.estados[pid]; len(es) > 0 {
		i := sort.Search(len(es), func(k int) bool { return es[k].TMs >= t })
		for _, k := range []int{i - 1, i} {
			if k >= 0 && k < len(es) && abs64(es[k].TMs-t) <= 1500 {
				p.Morto = es[k].Morto
				break
			}
		}
	}
	return p
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// deduzVisaoDoTime aplica ClassificaCaptura em cada captura e junta em
// intervalos. Cada captura vale da metade do caminho até a anterior à metade
// do caminho até a seguinte (no máximo 500 ms para cada lado: captura isolada
// não cobre um buraco).
func deduzVisaoDoTime(pv PasseVisao, cs []CapturaVisao, f fonteDePosicoes) VisaoDoTime {
	v := VisaoDoTime{Time: pv.Time, PasseVisaoID: pv.ID, Capturas: len(cs), Jogadores: []VisibilidadeJogador{}, Sentinelas: []SentinelaLida{}}
	if len(cs) == 0 {
		return v
	}
	v.DeMs, v.AteMs = cs[0].TMs, cs[len(cs)-1].TMs
	if v.AteMs > v.DeMs {
		v.CapturasPorS = math.Round(float64(len(cs))/(float64(v.AteMs-v.DeMs)/1000)*100) / 100
	}
	var pids, todos []int
	for pid, t := range f.times {
		todos = append(todos, pid)
		if t != pv.Time {
			pids = append(pids, pid)
		}
	}
	sort.Ints(pids)
	sort.Ints(todos)
	porPid := map[int]*VisibilidadeJogador{}
	for _, pid := range pids {
		_, rast := f.amostra[pid]
		j := &VisibilidadeJogador{ParticipanteID: pid, Time: f.times[pid], Rastreado: rast, Visivel: [][2]int64{}, Fora: [][2]int64{},
			Indeterminado: [][2]int64{}, Motivos: map[string]int64{}}
		porPid[pid] = j
	}
	modo := ModoDoTime(pv.Time)
	modoOK, aliVivos, aliAchados := 0, 0, 0
	for k, c := range cs {
		lo, hi := c.TMs-500, c.TMs+500
		if k > 0 {
			lo = max(lo, (cs[k-1].TMs+c.TMs)/2)
		}
		if k+1 < len(cs) {
			hi = min(hi, (c.TMs+cs[k+1].TMs)/2)
		}
		if hi <= lo {
			continue
		}
		ok := c.Modo == modo
		if ok {
			modoOK++
		}
		js := make([]PosicaoNoInstante, 0, len(todos))
		for _, pid := range todos {
			js = append(js, f.noInstante(pid, c.TMs))
		}
		r := ClassificaCaptura(LeituraMinimapa{Modo: c.Modo, Retratos: c.Retratos, Sentinelas: c.Sentinelas}, ok, pv.Time, js)
		aliVivos += r.AliadosVivos
		aliAchados += r.AliadosAchados
		for pid, e := range r.Estados {
			j := porPid[pid]
			if j == nil {
				continue
			}
			switch e {
			case VisVisivel:
				j.Visivel = juntaIntervalo(j.Visivel, lo, hi)
			case VisFora:
				j.Fora = juntaIntervalo(j.Fora, lo, hi)
			default:
				j.Indeterminado = juntaIntervalo(j.Indeterminado, lo, hi)
				j.Motivos[r.Motivos[pid]] += hi - lo
			}
		}
	}
	v.ModoConferido = math.Round(float64(modoOK)/float64(len(cs))*1000) / 1000
	if aliVivos > 0 {
		a := math.Round(float64(aliAchados)/float64(aliVivos)*1000) / 1000
		v.AliadosAchados = &a
		if a < AliadosMinimo {
			v.Aviso = fmt.Sprintf("só %.0f%% dos retratos do próprio time foram achados no minimapa onde os passes dos jogadores os põem: a posição rastreada (calibração) ou a geometria do minimapa não confere; os instantes sem os aliados no lugar ficaram indeterminados", a*100)
		}
	}
	for _, pid := range pids {
		j := porPid[pid]
		vis, fora := somaIntervalos(j.Visivel), somaIntervalos(j.Fora)
		if vis+fora > 0 {
			p := math.Round(float64(vis)/float64(vis+fora)*1000) / 10
			j.PctVisivel = &p
		}
		v.Jogadores = append(v.Jogadores, *j)
	}
	v.Sentinelas = acompanhaSentinelas(cs, pv.Time, modo)
	return v
}

// juntaIntervalo acrescenta [lo, hi), emendando no último se encostar.
func juntaIntervalo(is [][2]int64, lo, hi int64) [][2]int64 {
	if n := len(is); n > 0 && is[n-1][1] >= lo {
		is[n-1][1] = max(is[n-1][1], hi)
		return is
	}
	return append(is, [2]int64{lo, hi})
}

func somaIntervalos(is [][2]int64) int64 {
	var s int64
	for _, i := range is {
		s += i[1] - i[0]
	}
	return s
}

// Acompanhamento das sentinelas lidas: a mesma sentinela é a marca do time a
// até SentinelaMesmaPx da média das anteriores, vista de novo em até
// SentinelaSumicoMs. Enquanto um retrato está em cima do lugar dela (o
// retrato é desenhado por cima do marcador) o prazo não corre. Vale a partir
// de SentinelaMinCapturas capturas (uma leitura solta não vira sentinela).
const (
	SentinelaMesmaPx     = 3.0
	SentinelaSumicoMs    = 3000
	SentinelaMinCapturas = 3
	// SentinelaCobertaPx: retrato a menos disto do lugar esconde o marcador.
	SentinelaCobertaPx = replay.RaioRetratoMinimapa + 6
)

func acompanhaSentinelas(cs []CapturaVisao, time int, modo string) []SentinelaLida {
	type trilha struct {
		sx, sy   float64
		n        int
		de, ate  int64
		vivaAte  int64 // último instante em que foi vista ou estava coberta
		maxSemel float64
	}
	var ts []*trilha
	ultima := int64(0)
	for _, c := range cs {
		if c.Modo != modo {
			continue
		}
		ultima = c.TMs
		// Trilha não vista agora mas com um retrato em cima: continua viva.
		for _, t := range ts {
			if c.TMs-t.vivaAte > SentinelaSumicoMs {
				continue
			}
			if pertoDeAlgum(c.Retratos, t.sx/float64(t.n), t.sy/float64(t.n), SentinelaCobertaPx) {
				t.vivaAte = c.TMs
			}
		}
		for _, m := range c.Sentinelas {
			if m.Time != time {
				continue
			}
			var melhor *trilha
			melhorD := math.Inf(1)
			for _, t := range ts {
				if c.TMs-t.vivaAte > SentinelaSumicoMs {
					continue
				}
				if d := replay.DistanciaMinimapa(m.X, m.Y, t.sx/float64(t.n), t.sy/float64(t.n)); d <= SentinelaMesmaPx && d < melhorD {
					melhor, melhorD = t, d
				}
			}
			if melhor == nil {
				melhor = &trilha{de: c.TMs}
				ts = append(ts, melhor)
			}
			melhor.sx += m.X
			melhor.sy += m.Y
			melhor.n++
			melhor.ate, melhor.vivaAte = c.TMs, c.TMs
			melhor.maxSemel = math.Max(melhor.maxSemel, m.Cobertura)
		}
	}
	out := []SentinelaLida{}
	for _, t := range ts {
		if t.n < SentinelaMinCapturas {
			continue
		}
		out = append(out, SentinelaLida{X: math.Round(t.sx / float64(t.n)), Y: math.Round(t.sy / float64(t.n)), DeMs: t.de, AteMs: t.ate,
			Capturas: t.n, Semelhanca: t.maxSemel, AteOFim: ultima-t.ate <= 1000})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].DeMs < out[b].DeMs })
	return out
}

// RecalculaVisao refaz o visao.json da partida com as capturas dos passes de
// visão que valem e as posições rastreadas de agora. Sem passe de visão,
// apaga o arquivo (nada a mostrar). Devolve o resultado (nil sem passe).
func RecalculaVisao(db *banco.DB, gameID int64) (*VisaoDaPartida, error) {
	v := &VisaoDaPartida{GameID: gameID, CalculadaEm: banco.Agora(), ToleranciaPx: ToleranciaRetratoPx, OclusaoPx: OclusaoPx,
		UnidadesPorPixel: math.Round(replay.UnidadesPorPixelMinimapa()*10) / 10, Times: []VisaoDoTime{}}
	var f fonteDePosicoes
	carregou := false
	for _, time := range []int{100, 200} {
		pv, ok, err := melhorPasseVisao(db, gameID, time)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		cru, ok, err := arquivo.Le(db, gameID, nomeCapturasVisao(pv.ID))
		if err != nil || !ok {
			return nil, fmt.Errorf("ler as capturas do passe de visão %d: %v", pv.ID, err)
		}
		cs, err := capturasDoTexto(cru)
		if err != nil {
			return nil, err
		}
		if !carregou {
			if f, err = carregaPosicoes(db, gameID); err != nil {
				return nil, err
			}
			carregou = true
		}
		v.Times = append(v.Times, deduzVisaoDoTime(pv, cs, f))
	}
	if len(v.Times) == 0 {
		return nil, arquivo.Apaga(db, gameID, NomeVisao)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if _, err := arquivo.Grava(db, gameID, NomeVisao, b); err != nil {
		return nil, err
	}
	return v, nil
}

// finalizaPasseVisao fecha o passe: as capturas de trabalho viram o arquivo
// (concluído sempre substitui; cancelado só entra se o time ainda não tem
// passe de visão com arquivo), as linhas de trabalho somem e o resultado é
// refeito. Devolve o passe fechado.
func finalizaPasseVisao(db *banco.DB, p PasseVisao, estado, erro string) (PasseVisao, error) {
	cs, err := leCapturasBrutas(db, p.ID)
	if err != nil {
		return p, err
	}
	grava := len(cs) > 0
	if grava && estado != Concluido {
		if _, tem, err := melhorPasseVisao(db, p.GameID, p.Time); err != nil {
			return p, err
		} else if tem {
			grava = false
		}
	}
	if grava {
		if _, err := arquivo.Grava(db, p.GameID, nomeCapturasVisao(p.ID), textoCapturas(cs)); err != nil {
			return p, err
		}
	}
	var taxa *float64
	if len(cs) > 1 && cs[len(cs)-1].TMs > cs[0].TMs {
		v := math.Round(float64(len(cs))/(float64(cs[len(cs)-1].TMs-cs[0].TMs)/1000)*100) / 100
		taxa = &v
	}
	modoOK := 0
	for _, c := range cs {
		if c.Modo == ModoDoTime(p.Time) {
			modoOK++
		}
	}
	// Sem arquivo o passe não vale (melhorPasseVisao exige o arquivo), mas os
	// números ficam: são o que aconteceu.
	if _, err := db.Exec(`UPDATE passes_visao SET capturas = ?, capturas_modo_ok = ?, taxa = ? WHERE id = ?`, len(cs), modoOK, taxa, p.ID); err != nil {
		return p, err
	}
	if err := mudaEstadoPasseVisao(db, p.ID, estado, erro); err != nil {
		return p, err
	}
	if _, err := db.Exec(`DELETE FROM visao_capturas_brutas WHERE passe_id = ?`, p.ID); err != nil {
		return p, err
	}
	if grava && estado == Concluido {
		// Os arquivos de passes de visão anteriores do mesmo time não valem mais.
		if antigos, err := lePassesVisao(db, `WHERE game_id = ? AND time = ? AND id <> ?`, p.GameID, p.Time, p.ID); err == nil {
			for _, a := range antigos {
				arquivo.Apaga(db, p.GameID, nomeCapturasVisao(a.ID))
			}
		}
	}
	if _, err := RecalculaVisao(db, p.GameID); err != nil {
		return p, fmt.Errorf("refazer a visão da partida: %w", err)
	}
	return LePasseVisao(db, p.ID)
}
