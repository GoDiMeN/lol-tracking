package analise

import (
	"math"
	"sort"
	"sync"

	"loltracking/server/rastreio"
)

// preparo é o que todos os geradores da análise usam de um contexto, calculado
// uma vez por contexto (os geradores são chamados um depois do outro com o
// mesmo *ContextoCadeia).
type preparo struct {
	c        *rastreio.ContextoCadeia
	zonas    []zonaAmostra
	trechos  []trechoZona
	estados  map[int][]rastreio.EstadoJogador // por participante, em ordem de tempo
	eventos  []rastreio.EventoLT              // linha do tempo do LCU + extras da Riot
	rota     string                           // rota do jogador ("" = caçador)
	inimigos []int
	aliados  []int
	// usos: instantes de uso de habilidade do jogador (calculado na primeira vez).
	usos   []int64
	usosOK bool
	// ondas: as ondas da partida (calculadas na primeira vez; ondas_cadeia.go).
	ondas *OndasDaPartida
}

// zonaAmostra é a zona de uma amostra viva com posição.
type zonaAmostra struct {
	i        int
	zona     string
	subzona  string
	temZona  bool
	vivo     bool
	tMs      int64
	ponto    Ponto
	temPonto bool
}

// trechoZona é um trecho contínuo numa zona, já com histerese. Zona "" =
// morto.
type trechoZona struct {
	ini, fim  int64
	zona, sub string
	iIni      int // índice da amostra de entrada
	iFim      int
	morto     bool
}

var (
	memoMu sync.Mutex
	memoC  *rastreio.ContextoCadeia
	memoP  *preparo
)

func prepara(c *rastreio.ContextoCadeia) *preparo {
	memoMu.Lock()
	defer memoMu.Unlock()
	if memoC == c && memoP != nil {
		return memoP
	}
	p := &preparo{c: c, rota: RotaDaPosicao(c.Posicao), estados: map[int][]rastreio.EstadoJogador{}}
	for _, e := range c.Estados {
		p.estados[e.ParticipanteID] = append(p.estados[e.ParticipanteID], e)
	}
	for k := range p.estados {
		s := p.estados[k]
		sort.SliceStable(s, func(a, b int) bool { return s[a].TMs < s[b].TMs })
	}
	p.eventos = append(p.eventos, c.Linha.Eventos...)
	if extra, ok := c.Extra[ExtraEventosRiot].([]rastreio.EventoLT); ok {
		for _, e := range extra {
			// Da Riot só o que o LCU não tem: placas de torre.
			if e.Tipo == "TURRET_PLATE_DESTROYED" {
				p.eventos = append(p.eventos, e)
			}
		}
	}
	sort.SliceStable(p.eventos, func(a, b int) bool { return p.eventos[a].TMs < p.eventos[b].TMs })
	for pid, t := range c.Times {
		if pid == c.ParticipanteID {
			continue
		}
		if t == c.Time {
			p.aliados = append(p.aliados, pid)
		} else {
			p.inimigos = append(p.inimigos, pid)
		}
	}
	sort.Ints(p.aliados)
	sort.Ints(p.inimigos)
	for i, a := range c.Amostras {
		za := zonaAmostra{i: i, tMs: a.TMs, vivo: !a.Morto}
		if a.X != nil && a.Y != nil {
			za.ponto, za.temPonto = Ponto{*a.X, *a.Y}, true
			if !a.Morto {
				za.zona, za.subzona = Zona(*a.X, *a.Y)
				za.temZona = true
			}
		}
		p.zonas = append(p.zonas, za)
	}
	p.trechos = trechosDeZona(p.zonas, Param.HistereseZonaMs)
	memoC, memoP = c, p
	return p
}

// trechosDeZona junta as amostras em trechos por zona. A troca só vale depois
// de histerese ms na zona nova, e o trecho novo começa quando ela começou.
// Morte fecha o trecho e abre um "morto"; depois dele (e no começo) a zona
// vale na hora.
func trechosDeZona(zs []zonaAmostra, histerese int64) []trechoZona {
	var ts []trechoZona
	var cur *trechoZona
	cand, candIni, candIdx := "", int64(0), 0
	fecha := func(fim int64, iFim int) {
		if cur != nil {
			cur.fim, cur.iFim = fim, iFim
			ts = append(ts, *cur)
			cur = nil
		}
	}
	for k, z := range zs {
		if !z.vivo {
			if cur != nil && cur.morto {
				continue
			}
			fecha(z.tMs, k)
			cur = &trechoZona{ini: z.tMs, iIni: k, morto: true}
			cand = ""
			continue
		}
		if !z.temZona {
			continue
		}
		if cur == nil || cur.morto {
			fecha(z.tMs, k)
			cur = &trechoZona{ini: z.tMs, zona: z.zona, sub: z.subzona, iIni: k}
			cand = ""
			continue
		}
		if z.zona == cur.zona {
			cand = ""
			continue
		}
		if z.zona != cand {
			cand, candIni, candIdx = z.zona, z.tMs, k
		}
		if z.tMs-candIni >= histerese {
			fecha(candIni, candIdx)
			cur = &trechoZona{ini: candIni, zona: cand, sub: zs[candIdx].subzona, iIni: candIdx}
			cand = ""
		}
	}
	if cur != nil && len(zs) > 0 {
		fecha(zs[len(zs)-1].tMs, len(zs)-1)
	}
	return ts
}

// PosicaoEm é a posição de um jogador em t pelas amostras dele: interpolada
// entre as vizinhas vivas a até 1,5 s; com só uma vizinha, ela se estiver a
// até 1 s. Morto: falso.
func PosicaoEm(as []rastreio.Amostra, t int64) (Ponto, bool) {
	i := sort.Search(len(as), func(k int) bool { return as[k].TMs >= t })
	var a, b *rastreio.Amostra
	if i > 0 && t-as[i-1].TMs <= 1500 {
		a = &as[i-1]
	}
	if i < len(as) && as[i].TMs-t <= 1500 {
		b = &as[i]
	}
	ok := func(s *rastreio.Amostra) bool { return s != nil && s.X != nil && s.Y != nil && !s.Morto }
	switch {
	case ok(a) && ok(b) && b.TMs > a.TMs:
		f := float64(t-a.TMs) / float64(b.TMs-a.TMs)
		return Ponto{*a.X + (*b.X-*a.X)*f, *a.Y + (*b.Y-*a.Y)*f}, true
	case ok(a) && t-a.TMs <= 1000:
		return Ponto{*a.X, *a.Y}, true
	case ok(b) && b.TMs-t <= 1000:
		return Ponto{*b.X, *b.Y}, true
	}
	return Ponto{}, false
}

// AmostraEm é a última amostra com t_ms ≤ t (a até `ate` ms), ou nil.
func AmostraEm(as []rastreio.Amostra, t, ate int64) *rastreio.Amostra {
	i := sort.Search(len(as), func(k int) bool { return as[k].TMs > t }) - 1
	if i < 0 || t-as[i].TMs > ate {
		return nil
	}
	return &as[i]
}

// EstadoEm é o último estado do jogador com t_ms ≤ t (a até `ate` ms), ou nil.
func EstadoEm(es []rastreio.EstadoJogador, t, ate int64) *rastreio.EstadoJogador {
	i := sort.Search(len(es), func(k int) bool { return es[k].TMs > t }) - 1
	if i < 0 || t-es[i].TMs > ate {
		return nil
	}
	return &es[i]
}

// vidaPctAmostra: a vida do painel em % (exata), ou nulo.
func vidaPctAmostra(a *rastreio.Amostra) *float64 {
	if a == nil || a.Vida == nil || a.VidaMax == nil || *a.VidaMax <= 0 {
		return nil
	}
	v := math.Round(*a.Vida / *a.VidaMax * 1000) / 10
	return &v
}

func manaPctAmostra(a *rastreio.Amostra) *float64 {
	if a == nil || a.Mana == nil || a.ManaMax == nil || *a.ManaMax <= 0 {
		return nil
	}
	v := math.Round(*a.Mana / *a.ManaMax * 1000) / 10
	return &v
}

// vidaPctEm: a vida do jogador seguido em t (painel; senão quadro lateral).
func (p *preparo) vidaPctEm(t int64) *float64 {
	if v := vidaPctAmostra(AmostraEm(p.c.Amostras, t, 1000)); v != nil {
		return v
	}
	if e := EstadoEm(p.estados[p.c.ParticipanteID], t, 2000); e != nil {
		return e.VidaPct
	}
	return nil
}

func (p *preparo) manaPctEm(t int64) *float64 {
	if v := manaPctAmostra(AmostraEm(p.c.Amostras, t, 1000)); v != nil {
		return v
	}
	if e := EstadoEm(p.estados[p.c.ParticipanteID], t, 2000); e != nil {
		return e.ManaPct
	}
	return nil
}

// posicaoDe: onde o participante pid estava em t, pelas amostras do passe dele
// (o próprio ou um dos Outros).
func (p *preparo) posicaoDe(pid int, t int64) (Ponto, bool) {
	if pid == p.c.ParticipanteID {
		return PosicaoEm(p.c.Amostras, t)
	}
	return PosicaoEm(p.c.Outros[pid], t)
}

// quedaDeVida: quanto a vida do pid caiu (pontos percentuais) nos últimos
// janela ms até t. Do seguido usa o painel (exato); dos outros, o quadro
// lateral. ok = havia leitura.
func (p *preparo) quedaDeVida(pid int, t, janela int64) (float64, bool) {
	if pid == p.c.ParticipanteID {
		as := p.c.Amostras
		i := sort.Search(len(as), func(k int) bool { return as[k].TMs > t }) - 1
		agora := vidaPctAmostra(AmostraEm(as, t, 600))
		if agora != nil {
			maior := *agora
			for k := i; k >= 0 && as[k].TMs >= t-janela; k-- {
				if v := vidaPctAmostra(&as[k]); v != nil && *v > maior {
					maior = *v
				}
			}
			return maior - *agora, true
		}
	}
	es := p.estados[pid]
	i := sort.Search(len(es), func(k int) bool { return es[k].TMs > t }) - 1
	if i < 0 || t-es[i].TMs > 1500 || es[i].VidaPct == nil {
		return 0, false
	}
	agora, maior := *es[i].VidaPct, *es[i].VidaPct
	for k := i; k >= 0 && es[k].TMs >= t-janela; k-- {
		if es[k].VidaPct != nil && *es[k].VidaPct > maior {
			maior = *es[k].VidaPct
		}
	}
	return maior - agora, true
}

// csPorMinuto: o ritmo de CS do jogador no minuto da linha do tempo que
// contém t (exato, minionsKilled + jungleMinionsKilled). ok = há os dois quadros.
func csPorMinuto(lt rastreio.LinhaDoTempo, pid int, t int64) (float64, bool) {
	q := lt.Quadros
	for k := 1; k < len(q); k++ {
		if q[k].TMs >= t && q[k-1].TMs <= t {
			a, okA := q[k-1].Jogadores[pid]
			b, okB := q[k].Jogadores[pid]
			if !okA || !okB || q[k].TMs <= q[k-1].TMs {
				return 0, false
			}
			return float64(b.CS-a.CS) / (float64(q[k].TMs-q[k-1].TMs) / 60000), true
		}
	}
	return 0, false
}

// CSNaLinha: o CS exato do pid no quadro do minuto m (0 se não há).
func CSNaLinha(lt rastreio.LinhaDoTempo, pid int, tMs int64) (rastreio.JogadorLT, bool) {
	for _, q := range lt.Quadros {
		if q.TMs >= tMs-1000 && q.TMs <= tMs+1000 {
			j, ok := q.Jogadores[pid]
			return j, ok
		}
	}
	return rastreio.JogadorLT{}, false
}

func contem(l []int, v int) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}
