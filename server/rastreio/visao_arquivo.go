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
//	                              (marcas como em textoMarcas), mais uma 5ª
//	                              coluna com os arcos desde 2026-11-18 (arquivo
//	                              velho tem 4). É o dado bruto: não depende
//	                              das posições rastreadas.
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

// prefixoGeometria abre a primeira linha do arquivo de capturas: a geometria
// do minimapa com que as marcas viraram posição do mapa
// ("#geometria \t x0 \t x_px \t y0 \t y_px"). Arquivo sem ela é de antes de
// 2026-11-18 (replay.GeometriaMinimapa20261117). Ao ler, as marcas voltam ao
// pixel pela geometria do arquivo e viram posição pela atual: o bruto não
// muda e a geometria nova vale para as capturas velhas.
const prefixoGeometria = "#geometria"

func textoCapturas(cs []CapturaVisao) []byte {
	var b bytes.Buffer
	g := replay.GeometriaMinimapaAtual
	fmt.Fprintf(&b, "%s\t%s\t%s\t%s\t%s\n", prefixoGeometria, strconv.FormatFloat(g.X0, 'f', -1, 64), strconv.FormatFloat(g.XPx, 'f', -1, 64),
		strconv.FormatFloat(g.Y0, 'f', -1, 64), strconv.FormatFloat(g.YPx, 'f', -1, 64))
	for _, c := range cs {
		b.WriteString(strconv.FormatInt(c.TMs, 10))
		b.WriteByte('\t')
		b.WriteString(c.Modo)
		b.WriteByte('\t')
		b.WriteString(textoMarcas(c.Retratos))
		b.WriteByte('\t')
		b.WriteString(textoMarcas(c.Sentinelas))
		b.WriteByte('\t')
		b.WriteString(textoMarcas(c.Arcos))
		b.WriteByte('\n')
	}
	return b.Bytes()
}

func capturasDoTexto(texto []byte) ([]CapturaVisao, error) {
	var cs []CapturaVisao
	g := replay.GeometriaMinimapa20261117
	for n, linha := range bytes.Split(texto, []byte{'\n'}) {
		if len(linha) == 0 {
			continue
		}
		p := bytes.Split(linha, []byte{'\t'})
		if string(p[0]) == prefixoGeometria {
			var v [4]float64
			if len(p) != 5 {
				return nil, fmt.Errorf("capturas da visão: linha %d: geometria fora do formato", n+1)
			}
			for k := range v {
				x, err := strconv.ParseFloat(string(p[k+1]), 64)
				if err != nil || (k%2 == 1 && x == 0) {
					return nil, fmt.Errorf("capturas da visão: linha %d: geometria fora do formato", n+1)
				}
				v[k] = x
			}
			g = replay.GeometriaMinimapa{X0: v[0], XPx: v[1], Y0: v[2], YPx: v[3]}
			continue
		}
		if len(p) != 4 && len(p) != 5 {
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
		if len(p) == 5 {
			if c.Arcos, err = marcasDoTexto(string(p[4])); err != nil {
				return nil, fmt.Errorf("capturas da visão: linha %d: %w", n+1, err)
			}
		}
		if g != replay.GeometriaMinimapaAtual {
			reprojeta(c.Retratos, g)
			reprojeta(c.Sentinelas, g)
			reprojeta(c.Arcos, g)
		}
		cs = append(cs, c)
	}
	return cs, nil
}

// reprojeta leva as marcas da geometria g para a atual (mesmo pixel).
func reprojeta(ms []MarcaMinimapa, g replay.GeometriaMinimapa) {
	for i := range ms {
		ms[i].X, ms[i].Y = g.ParaAtual(ms[i].X, ms[i].Y)
	}
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
	// PctIndeterminado e PctInferido: do tempo dos inimigos vivos e no mapa
	// com captura (visível + fora + indeterminado), quanto ficou
	// indeterminado e quanto foi herdado pela continuidade (em %).
	PctIndeterminado float64 `json:"pct_indeterminado"`
	PctInferido      float64 `json:"pct_inferido"`
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
	// ForaDoMapa: o jogo o tirou do mapa (VisForaDoMapa: Investida Alfa,
	// teleporte em trânsito): não é alvo de visão. Fora dos outros três.
	ForaDoMapa [][2]int64 `json:"fora_do_mapa"`
	// Inferido: os pedaços de Visivel e Fora que não foram lidos e sim
	// herdados pela continuidade (inferirTrechos): um trecho curto sem
	// leitura firme entre duas leituras iguais. Estão DENTRO de Visivel/Fora;
	// quem quer só leitura firme desconta estes.
	Inferido [][2]int64 `json:"inferido"`
	// PeloArcoMs: quanto do Visivel veio de um arco (anel parcial com a falta
	// embaixo de um retrato por cima), e não de um anel inteiro.
	PeloArcoMs int64 `json:"pelo_arco_ms"`
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
	}{{VisVisivel, v.Visivel}, {VisFora, v.Fora}, {VisIndeterminado, v.Indeterminado}, {VisForaDoMapa, v.ForaDoMapa}} {
		if dentro(l.is, t) {
			return l.e
		}
	}
	return ""
}

// InferidoEm diz se o estado no instante t foi herdado (Inferido).
func (v VisibilidadeJogador) InferidoEm(t int64) bool { return dentro(v.Inferido, t) }

// dentro: t cai num dos intervalos [de, ate) (em ordem e sem sobreposição).
func dentro(is [][2]int64, t int64) bool {
	i := sort.Search(len(is), func(k int) bool { return is[k][1] > t })
	return i < len(is) && is[i][0] <= t
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
	// foraDoMapa: por jogador, os trechos [de, ate) em que o jogo o tirou do
	// mapa (trechosForaDoMapa).
	foraDoMapa map[int][][2]int64
}

// trechosForaDoMapa: os trechos sem a câmera em que o JOGO tirou o jogador
// do mapa (descobertas 8.1): pelo menos 2 amostras seguidas sem leitura boa,
// vivo, e as primeiras (até AmostrasSelecionadoNoComeco) com a trava perdida
// e a seleção ainda nele. Na 3305449546: 101 trechos do Master Yi (a
// Investida Alfa, 1,0–1,6 s) e 13 de outros (teleportes de 2–7 s); a perda
// de trava solta (1 amostra) e a com a seleção fora não entram. Vai da
// primeira amostra sem câmera até a primeira leitura boa depois.
func trechosForaDoMapa(as []Amostra) [][2]int64 {
	var out [][2]int64
	for i := 0; i < len(as); {
		if LeituraBoa(as[i]) || as[i].Morto {
			i++
			continue
		}
		j := i
		for j < len(as) && !LeituraBoa(as[j]) && !as[j].Morto {
			j++
		}
		ok := j-i >= 2
		for k := i; ok && k < j && k < i+AmostrasSelecionadoNoComeco; k++ {
			ok = as[k].TravaPerdida && as[k].SelecaoOK
		}
		if ok {
			fim := as[j-1].TMs
			if j < len(as) {
				fim = as[j].TMs
			}
			out = append(out, [2]int64{as[i].TMs, fim})
		}
		i = j
	}
	return out
}

func carregaPosicoes(db *banco.DB, gameID int64) (fonteDePosicoes, error) {
	f := fonteDePosicoes{times: map[int]int{}, amostra: map[int][]Amostra{}, foraDoMapa: map[int][][2]int64{}}
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
			f.foraDoMapa[pid] = trechosForaDoMapa(as)
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
		if !p.TemPosicao && !p.Morto && dentro(f.foraDoMapa[pid], t) {
			p.ForaDoMapa = true
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

// CapturaClassificada: uma captura do passe de visão já decidida, com o
// trecho de tempo que ela vale e o que a continuidade precisa de cada
// inimigo.
type CapturaClassificada struct {
	De, Ate int64
	TMs     int64
	ModoOK  bool
	R       ClassificacaoCaptura
	// Inimigos do observador no instante (posição, morto, fora do mapa).
	Inimigos map[int]PosicaoNoInstante
	// AliadoMaisPerto: por inimigo com posição, a distância (unidades) ao
	// aliado vivo do observador mais perto (com posição); +Inf sem nenhum.
	AliadoMaisPerto map[int]float64
	// AliadoSemPosicao: algum aliado vivo do observador sem posição (pode
	// estar perto de qualquer um).
	AliadoSemPosicao bool
}

// classificaPasse aplica ClassificaCaptura em cada captura. Cada captura
// vale da metade do caminho até a anterior à metade do caminho até a
// seguinte (no máximo 500 ms para cada lado: captura isolada não cobre um
// buraco).
func classificaPasse(pv PasseVisao, cs []CapturaVisao, f fonteDePosicoes) []CapturaClassificada {
	var todos []int
	for pid := range f.times {
		todos = append(todos, pid)
	}
	sort.Ints(todos)
	modo := ModoDoTime(pv.Time)
	atraso := atrasoMinimapaJogoMs(pv.Velocidade)
	out := make([]CapturaClassificada, 0, len(cs))
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
		// O minimapa da captura mostra o jogo de AtrasoMinimapaMs (tempo real)
		// antes do carimbo: as posições rastreadas são as daquele instante.
		js := make([]PosicaoNoInstante, 0, len(todos))
		for _, pid := range todos {
			js = append(js, f.noInstante(pid, c.TMs-atraso))
		}
		ok := c.Modo == modo
		cc := CapturaClassificada{De: lo, Ate: hi, TMs: c.TMs, ModoOK: ok, Inimigos: map[int]PosicaoNoInstante{}, AliadoMaisPerto: map[int]float64{},
			R: ClassificaCaptura(LeituraMinimapa{Modo: c.Modo, Retratos: c.Retratos, Sentinelas: c.Sentinelas, Arcos: c.Arcos}, ok, pv.Time, js)}
		for _, j := range js {
			if j.Time == pv.Time && !j.Morto && !j.TemPosicao {
				cc.AliadoSemPosicao = true
			}
		}
		for _, j := range js {
			if j.Time == pv.Time {
				continue
			}
			cc.Inimigos[j.ParticipanteID] = j
			if !j.TemPosicao {
				continue
			}
			d := math.Inf(1)
			for _, a := range js {
				if a.Time == pv.Time && !a.Morto && a.TemPosicao {
					d = math.Min(d, math.Hypot(a.X-j.X, a.Y-j.Y))
				}
			}
			cc.AliadoMaisPerto[j.ParticipanteID] = d
		}
		out = append(out, cc)
	}
	return out
}

// Continuidade no tempo (inferirTrechos): um trecho indeterminado curto entre
// duas leituras firmes IGUAIS herda o estado delas, marcado como inferido.
// Nunca através de uma troca (visível de um lado e fora do outro) e nunca
// num trecho sem posição (não dá para conferir o alcance).
const (
	// InferenciaVisivelMaxMs: o maior trecho que herda "visível". Na
	// 3305449546, 68% (azul) e 48% (vermelho) do tempo indeterminado entre
	// dois visíveis estava em trechos de até 3 s.
	InferenciaVisivelMaxMs int64 = 3000
	// InferenciaCurtaMs: até isto (1–2 capturas) o visível é herdado sem
	// conferir o alcance.
	InferenciaCurtaMs int64 = 1000
	// AlcanceVisaoAliado: acima de InferenciaCurtaMs, o visível só é herdado
	// se em todas as capturas do trecho um aliado vivo do observador estava a
	// até isto (unidades): o campeão enxerga ~1 350; 150 de folga para o erro
	// da posição. O mato continua podendo esconder (por isso "inferido").
	AlcanceVisaoAliado = 1200.0
	// InferenciaForaMaxMs: o maior trecho que herda "fora".
	InferenciaForaMaxMs int64 = 3000
	// LongeDaVisao: o "fora" só é herdado se em todas as capturas do trecho o
	// jogador estava a mais disto de todo aliado vivo do observador (e nenhum
	// sem posição)...
	LongeDaVisao = 2000.0
	// LongeDaSentinela: ... e a mais disto de toda sentinela do observador
	// lida no minimapa (a sentinela enxerga ~900). Torres e tropas não entram
	// (não são lidas): por isso também "inferido".
	LongeDaSentinela = 1500.0
)

// estadoCap é o estado de um jogador numa captura.
type estadoCap struct {
	de, ate  int64
	estado   string
	motivo   string
	inferido bool
	arco     bool
	porOrdem bool
	k        int // índice da captura em ccs
}

// desfazForaPorOrdem: um trecho de "fora" que só saiu pela ordem de desenho
// (PorOrdem), curto (até InferenciaVisivelMaxMs) e com visível encostado dos
// dois lados volta a indeterminado ("coberto"). Na 3305449546 (visão azul),
// 25% das capturas desses "fora" caíam entre dois visíveis, a mesma taxa dos
// "fora" antigos perto de um aliado (23%), mas um "fora" de meio segundo no
// meio de uma luta é o anel que não foi achado, não o inimigo que sumiu.
func desfazForaPorOrdem(seq []estadoCap) {
	for i := 0; i < len(seq); {
		if seq[i].estado != VisFora || !seq[i].porOrdem {
			i++
			continue
		}
		j := i + 1
		for j < len(seq) && seq[j].estado == VisFora && seq[j].porOrdem && seq[j].de == seq[j-1].ate {
			j++
		}
		ini := i
		i = j
		if ini == 0 || j >= len(seq) || seq[ini-1].estado != VisVisivel || seq[j].estado != VisVisivel ||
			seq[ini-1].ate != seq[ini].de || seq[j].de != seq[j-1].ate || seq[j-1].ate-seq[ini].de > InferenciaVisivelMaxMs {
			continue
		}
		for k := ini; k < j; k++ {
			seq[k].estado, seq[k].motivo = VisIndeterminado, "coberto"
		}
	}
}

// inferirTrechos aplica a continuidade na sequência de um jogador (em ordem
// de tempo).
func inferirTrechos(seq []estadoCap, ccs []CapturaClassificada, pid int, sents []SentinelaLida) {
	for i := 0; i < len(seq); {
		if seq[i].estado != VisIndeterminado {
			i++
			continue
		}
		j := i + 1
		for j < len(seq) && seq[j].estado == VisIndeterminado && seq[j].de == seq[j-1].ate {
			j++
		}
		ini := i
		i = j
		if ini == 0 || j >= len(seq) || seq[ini-1].ate != seq[ini].de || seq[j].de != seq[j-1].ate {
			continue // sem leitura encostada de um dos lados
		}
		antes, depois := seq[ini-1], seq[j]
		if antes.estado != depois.estado || antes.inferido || depois.inferido {
			continue // troca, ou ponta herdada
		}
		run := seq[ini:j]
		dur := run[len(run)-1].ate - run[0].de
		pode := antes.estado == VisVisivel || antes.estado == VisFora
		for _, e := range run {
			if !pode {
				break
			}
			cc := ccs[e.k]
			pj := cc.Inimigos[pid]
			if e.motivo == "sem_posicao" || !pj.TemPosicao {
				pode = false
				break
			}
			d, ok := cc.AliadoMaisPerto[pid]
			if !ok {
				d = math.Inf(1)
			}
			if antes.estado == VisVisivel {
				pode = dur <= InferenciaVisivelMaxMs && (dur <= InferenciaCurtaMs || d <= AlcanceVisaoAliado)
			} else {
				pode = dur <= InferenciaForaMaxMs && !cc.AliadoSemPosicao && d > LongeDaVisao && !sentinelaPerto(sents, pj.X, pj.Y, cc.TMs)
			}
		}
		if !pode {
			continue
		}
		for k := range run {
			run[k].estado, run[k].inferido = antes.estado, true
		}
	}
}

func sentinelaPerto(ss []SentinelaLida, x, y float64, t int64) bool {
	for _, s := range ss {
		if s.DeMs <= t && t <= s.AteMs && math.Hypot(s.X-x, s.Y-y) <= LongeDaSentinela {
			return true
		}
	}
	return false
}

// deduzVisaoDoTime classifica as capturas, aplica a continuidade e junta em
// intervalos.
func deduzVisaoDoTime(pv PasseVisao, cs []CapturaVisao, f fonteDePosicoes) VisaoDoTime {
	v, _ := deduzVisaoComCapturas(pv, cs, f)
	return v
}

// deduzVisaoComCapturas é deduzVisaoDoTime devolvendo também as capturas
// classificadas e a sequência final de cada inimigo (para conferências).
func deduzVisaoComCapturas(pv PasseVisao, cs []CapturaVisao, f fonteDePosicoes) (VisaoDoTime, map[int][]estadoCap) {
	v := VisaoDoTime{Time: pv.Time, PasseVisaoID: pv.ID, Capturas: len(cs), Jogadores: []VisibilidadeJogador{}, Sentinelas: []SentinelaLida{}}
	if len(cs) == 0 {
		return v, nil
	}
	v.DeMs, v.AteMs = cs[0].TMs, cs[len(cs)-1].TMs
	if v.AteMs > v.DeMs {
		v.CapturasPorS = math.Round(float64(len(cs))/(float64(v.AteMs-v.DeMs)/1000)*100) / 100
	}
	var pids []int
	for pid, t := range f.times {
		if t != pv.Time {
			pids = append(pids, pid)
		}
	}
	sort.Ints(pids)
	modo := ModoDoTime(pv.Time)
	v.Sentinelas = acompanhaSentinelas(cs, pv.Time, modo)
	modoOK := 0
	for _, c := range cs {
		if c.Modo == modo {
			modoOK++
		}
	}
	v.ModoConferido = math.Round(float64(modoOK)/float64(len(cs))*1000) / 1000
	ccs := classificaPasse(pv, cs, f)
	aliVivos, aliAchados := 0, 0
	seqs := map[int][]estadoCap{}
	for k, cc := range ccs {
		aliVivos += cc.R.AliadosVivos
		aliAchados += cc.R.AliadosAchados
		for pid, e := range cc.R.Estados {
			seqs[pid] = append(seqs[pid], estadoCap{de: cc.De, ate: cc.Ate, estado: e, motivo: cc.R.Motivos[pid], arco: cc.R.PeloArco[pid],
				porOrdem: cc.R.PorOrdem[pid], k: k})
		}
	}
	if aliVivos > 0 {
		a := math.Round(float64(aliAchados)/float64(aliVivos)*1000) / 1000
		v.AliadosAchados = &a
		if a < AliadosMinimo {
			v.Aviso = fmt.Sprintf("só %.0f%% dos retratos do próprio time foram achados no minimapa onde os passes dos jogadores os põem: a posição rastreada (calibração) ou a geometria do minimapa não confere; os instantes sem os aliados no lugar ficaram indeterminados", a*100)
		}
	}
	var totDecidido, totIndet, totInfer int64
	for _, pid := range pids {
		_, rast := f.amostra[pid]
		j := VisibilidadeJogador{ParticipanteID: pid, Time: f.times[pid], Rastreado: rast, Visivel: [][2]int64{}, Fora: [][2]int64{},
			Indeterminado: [][2]int64{}, ForaDoMapa: [][2]int64{}, Inferido: [][2]int64{}, Motivos: map[string]int64{}}
		seq := seqs[pid]
		desfazForaPorOrdem(seq)
		inferirTrechos(seq, ccs, pid, v.Sentinelas)
		for _, e := range seq {
			switch e.estado {
			case VisVisivel:
				j.Visivel = juntaIntervalo(j.Visivel, e.de, e.ate)
				if e.arco {
					j.PeloArcoMs += e.ate - e.de
				}
			case VisFora:
				j.Fora = juntaIntervalo(j.Fora, e.de, e.ate)
			case VisForaDoMapa:
				j.ForaDoMapa = juntaIntervalo(j.ForaDoMapa, e.de, e.ate)
				continue
			default:
				j.Indeterminado = juntaIntervalo(j.Indeterminado, e.de, e.ate)
				j.Motivos[e.motivo] += e.ate - e.de
				totIndet += e.ate - e.de
				continue
			}
			totDecidido += e.ate - e.de
			if e.inferido {
				j.Inferido = juntaIntervalo(j.Inferido, e.de, e.ate)
				totInfer += e.ate - e.de
			}
		}
		vis, fora := somaIntervalos(j.Visivel), somaIntervalos(j.Fora)
		if vis+fora > 0 {
			p := math.Round(float64(vis)/float64(vis+fora)*1000) / 10
			j.PctVisivel = &p
		}
		v.Jogadores = append(v.Jogadores, j)
	}
	if tot := totDecidido + totIndet; tot > 0 {
		v.PctIndeterminado = math.Round(float64(totIndet)/float64(tot)*1000) / 10
		v.PctInferido = math.Round(float64(totInfer)/float64(tot)*1000) / 10
	}
	return v, seqs
}

// atrasoMinimapaJogoMs é replay.AtrasoMinimapaMs (tempo real) em ms de jogo
// à velocidade vel do passe de visão (0 = a padrão).
func atrasoMinimapaJogoMs(vel float64) int64 {
	if vel <= 0 {
		vel = VelocidadeVisaoPadrao
	}
	return int64(math.Round(replay.AtrasoMinimapaMs * vel))
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
