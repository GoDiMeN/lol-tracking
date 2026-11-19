package rastreio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"
	"time"

	"loltracking/server/replay"
)

// O laço do passe de visão de time. Diferente do passe de jogador: não segue
// ninguém, não trava câmera e só aperta duas teclas (F1 ou F2 no começo, F3 no
// fim), as duas pelo caminho de toda tecla (replay.Sessao.Tecla: lista,
// prova de replay, fase do gameflow, foco).

// Padrões do passe de visão.
const (
	// VelocidadeVisaoPadrao: só o minimapa importa e ele não depende da
	// câmera, então o replay pode correr — até onde o ciclo deixa. Era 8×,
	// contando com um ciclo de ~31 ms; no jogo de verdade (3305449546,
	// descobertas 9) o ciclo a 8× deu ~57 ms (mediana; 2 playback ~9 ms, o
	// PrintWindow da janela inteira ~20 ms + ~3 ms de conversão e recorte, a
	// leitura ~3 ms, o resto não medido sem o jogo) e a taxa caiu para 2,25
	// capturas por segundo de jogo. A 4× o ciclo pedido é de 62,5 ms: cabe,
	// e a taxa volta às 4 por segundo planejadas (não medido no jogo: a
	// primeira passada a 4× confere, pelo log do fim do passe).
	VelocidadeVisaoPadrao = 4.0
	// CapturasPorSegundoVisao: capturas por segundo de JOGO. A 4 por segundo
	// (uma a cada 250 ms de jogo) uma aparição de meio segundo (um inimigo
	// cruzando a borda de uma sentinela, um Flash para fora do mato) cai em
	// pelo menos duas capturas, e a entrada/saída da visão fica com erro de
	// até 125 ms. Com o ciclo de ~57 ms medido, 4 por segundo só cabem até
	// 4× (VelocidadeVisaoPadrao).
	CapturasPorSegundoVisao = 4.0
)

// Tempos do passe de visão (tempo real). Variáveis para o teste.
var (
	// EsperaPainelNevoa: depois da tecla, até quanto esperar o painel mudar.
	EsperaPainelNevoa = 1200 * time.Millisecond
	// IntervaloPainelNevoa: de quanto em quanto reler o painel nessa espera.
	IntervaloPainelNevoa = 200 * time.Millisecond
	// MaxTeclasNevoa: quantas vezes apertar a tecla do time se o painel não
	// mudar (a tecla pode se perder se o jogo estava ocupado).
	MaxTeclasNevoa = 3
	// LimiteSemModoMs: tempo de jogo com o painel dizendo outra névoa antes de
	// apertar de novo (uma vez por vez, até MaxTeclasNevoa no passe).
	LimiteSemModoMs int64 = 3000
)

// ErrNevoa: o painel não mostrou a névoa do time depois das teclas.
var ErrNevoa = errors.New("a névoa do replay não mudou para a visão do time")

// ErrSemLeitorMinimapa: o servidor subiu sem a leitura do minimapa.
var ErrSemLeitorMinimapa = errors.New("sem a leitura do minimapa (pacote visao): o passe de visão não roda")

// regiaoVisao é o recorte que o passe captura, na tela do jogo.
func (m *motor) regiaoVisao() image.Rectangle {
	r := replay.RegiaoVisao
	if m.s.Largura == replay.LarguraRef && m.s.Altura == replay.AlturaRef {
		return r
	}
	// Outra tela: o mesmo pedaço proporcional (a leitura recusa de todo jeito,
	// mas a captura não pode sair da janela).
	s := float64(m.s.Altura) / replay.AlturaRef
	return image.Rect(0, int(float64(r.Min.Y)*s), m.s.Largura, m.s.Altura)
}

// leituraVisao é uma captura do passe de visão.
type leituraVisao struct {
	tMs   int64
	pb    replay.Playback
	img   *image.RGBA
	lt    LeituraMinimapa
	errLe error // a captura ou a leitura não deu (a tela não é a conhecida)
	// Onde o tempo do ciclo foi (tempo real): os dois playback, a captura e a
	// leitura do minimapa (descobertas 9: a 8× o ciclo deu ~57 ms).
	durAPI, durCapt, durLe time.Duration
}

// capturaVisao captura o recorte e lê o minimapa. O tempo é a média dos dois
// playback em volta da captura (como nas amostras). Erro = a Replay API.
func (m *motor) capturaVisao(ctx context.Context) (leituraVisao, error) {
	var l leituraVisao
	t0 := time.Now()
	p1, err := m.d.API.Playback(ctx)
	if err != nil {
		return l, err
	}
	t1 := time.Now()
	reg := m.regiaoVisao()
	img, errCapt := m.d.Tela.Captura(reg)
	t2 := time.Now()
	if l.pb, err = m.d.API.Playback(ctx); err != nil {
		return l, err
	}
	t3 := time.Now()
	l.durAPI, l.durCapt = t1.Sub(t0)+t3.Sub(t2), t2.Sub(t1)
	l.tMs = int64(math.Round((p1.Tempo + l.pb.Tempo) / 2 * 1000))
	if errCapt != nil {
		l.errLe = errCapt
		return l, nil
	}
	l.img = img
	l.lt, l.errLe = m.cfg.Minimapa.LeMinimapa(img, reg, m.s.Largura, m.s.Altura)
	l.durLe = time.Since(t3)
	return l, nil
}

// guardaCapturaVisao grava a captura do passe de visão (PNG do recorte) e um
// JSON com o que a leitura achou — material para conferir a leitura no jogo de
// verdade (a sonda TestRealVisao liga com PastaCapturas). Em segundo plano e
// uma por vez, como guardaCaptura.
func (m *motor) guardaCapturaVisao(p PasseVisao, l leituraVisao) {
	if l.img == nil || !m.salvando.TryLock() {
		return
	}
	img := image.NewRGBA(l.img.Bounds())
	copy(img.Pix, l.img.Pix)
	info := map[string]any{"game_id": p.GameID, "passe_visao_id": p.ID, "time": p.Time, "t_ms": l.tMs, "regiao": m.regiaoVisao().String(),
		"largura": m.s.Largura, "altura": m.s.Altura, "modo": l.lt.Modo, "retratos": l.lt.Retratos, "sentinelas": l.lt.Sentinelas, "arcos": l.lt.Arcos}
	go func() {
		defer m.salvando.Unlock()
		dir := filepath.Join(m.cfg.PastaCapturas, fmt.Sprint(p.GameID))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Printf("rastreio: pasta de capturas: %v", err)
			return
		}
		base := filepath.Join(dir, fmt.Sprintf("visao%d_t%07d", p.Time, l.tMs))
		f, err := os.Create(base + ".png")
		if err != nil {
			log.Printf("rastreio: captura: %v", err)
			return
		}
		png.Encode(f, img)
		f.Close()
		b, _ := json.MarshalIndent(info, "", "  ")
		os.WriteFile(base+".json", b, 0o644)
	}()
}

// ligaNevoa aperta a tecla do time e confere pelo painel que a névoa mudou.
// Aperta de novo (até MaxTeclasNevoa) se o painel não mudar no prazo.
func (m *motor) ligaNevoa(ctx context.Context, time int, teclas *int) error {
	quer := ModoDoTime(time)
	lido := ""
	for tentativa := 1; tentativa <= MaxTeclasNevoa; tentativa++ {
		if err := m.s.MudaVisao(ctx, time); errors.Is(err, replay.ErrUsuarioUsando) {
			// A pessoa está usando o computador: espera ela parar (replay
			// pausado), despausa e tenta de novo, sem contar a tentativa.
			if err := m.esperaUsuario(ctx, m.s.GameID, &pedidoEspera{motivo: err.Error(), ultimoBomMs: -1}); err != nil {
				return err
			}
			if _, err := m.d.API.MudaPlayback(ctx, replay.MudaPlayback{Pausado: replay.Ptr(false)}); err != nil {
				return err
			}
			tentativa--
			continue
		} else if err != nil {
			return err
		}
		*teclas++
		limite := m.d.Relogio.Agora().Add(EsperaPainelNevoa)
		for {
			if err := m.d.Relogio.Dorme(ctx, IntervaloPainelNevoa); err != nil {
				return err
			}
			l, err := m.capturaVisao(ctx)
			if err != nil {
				return err
			}
			if l.errLe == nil || l.lt.Modo != "" {
				lido = l.lt.Modo
			}
			if l.lt.Modo == quer {
				return nil
			}
			if m.d.Relogio.Agora().After(limite) {
				break
			}
		}
	}
	if lido == "" {
		lido = "ilegível"
	}
	return fmt.Errorf("%w %d (o painel \"Fog of War\" do replay mostra %s depois de %d tecla(s)); confira se o painel do replay está à mostra",
		ErrNevoa, time, lido, MaxTeclasNevoa)
}

// devolveNevoa aperta F3 (todos) no fim do passe. Falha só vira aviso: o
// replay fecha logo depois de todo jeito.
func (m *motor) devolveNevoa(ctx context.Context, p PasseVisao, teclas *int) {
	if err := m.s.MudaVisao(ctx, 0); err != nil {
		log.Printf("rastreio: passe de visão %d: devolver a névoa para todos: %v", p.ID, err)
		m.ex.Emite("aviso", map[string]string{"texto": "não deu para devolver a névoa do replay para \"todos\": " + err.Error()})
		return
	}
	*teclas++
	m.db.Exec(`UPDATE passes_visao SET teclas = ? WHERE id = ?`, *teclas, p.ID)
}

// rodaPasseVisao toca o replay de p.TAtualMs até o fim (ou p.TFimMs) na visão
// do time e grava as capturas lidas na tabela de trabalho. Quem fecha o passe
// é finalizaPasseVisao.
func (m *motor) rodaPasseVisao(ctx context.Context, p PasseVisao) (EstatPasse, error) {
	var st EstatPasse
	if m.cfg.Minimapa == nil {
		return st, ErrSemLeitorMinimapa
	}
	mudaEstadoPasseVisao(m.db, p.ID, Rodando, "")
	duracaoMs := int64(m.s.Duracao * 1000)
	m.db.Exec(`UPDATE passes_visao SET duracao_ms = ? WHERE id = ?`, duracaoMs, p.ID)
	fimMs := p.TFimMs
	if fimMs <= 0 || fimMs > duracaoMs {
		fimMs = duracaoMs
	}
	vel := p.Velocidade
	if vel <= 0 {
		vel = VelocidadeVisaoPadrao
	}
	inicio := max(p.TAtualMs, p.TInicioMs)
	st.TIniMs = inicio
	m.ex.Emite("visao_inicio", map[string]any{"passe_visao_id": p.ID, "time": p.Time, "t_inicio_ms": inicio, "t_fim_ms": fimMs,
		"duracao_ms": duracaoMs, "velocidade": vel})

	alvoS := math.Max(0, float64(inicio-1000)/1000)
	if _, err := m.d.API.MudaPlayback(ctx, replay.MudaPlayback{Tempo: &alvoS, Velocidade: replay.Ptr(vel), Pausado: replay.Ptr(false)}); err != nil {
		return st, err
	}
	velAntes := m.cfg.Velocidade
	m.cfg.Velocidade = vel // esperaSeek usa a velocidade para a folga
	err := m.esperaSeek(ctx, alvoS)
	m.cfg.Velocidade = velAntes
	if err != nil {
		return st, err
	}
	teclas := p.Teclas
	if err := m.ligaNevoa(ctx, p.Time, &teclas); err != nil {
		return st, err
	}
	m.ex.Emite("nevoa", map[string]any{"passe_visao_id": p.ID, "time": p.Time, "modo": ModoDoTime(p.Time), "ok": true})
	defer m.devolveNevoa(context.WithoutCancel(ctx), p, &teclas)

	var pend []CapturaVisao
	total, modoOK := p.Capturas, p.CapturasModoOK
	tAtual := p.TAtualMs
	ultimaGravacao := time.Now()
	grava := func() error {
		if len(pend) == 0 {
			m.db.Exec(`UPDATE passes_visao SET teclas = ? WHERE id = ?`, teclas, p.ID)
			return nil
		}
		err := gravaCapturasBrutas(m.db, p, pend, tAtual, total, modoOK, teclas)
		pend = pend[:0]
		ultimaGravacao = time.Now()
		return err
	}
	defer func() {
		if err := grava(); err != nil {
			log.Printf("rastreio: gravar as últimas capturas do passe de visão %d: %v", p.ID, err)
		}
	}()

	intervalo := time.Duration(float64(time.Second) / CapturasPorSegundoVisao / vel)
	ultimaFase := m.d.Relogio.Agora()
	ultimoSSE := time.Time{}
	errosSeguidos, errosTela := 0, 0
	avisouTela := false
	var semModoDesde int64 = -1
	ultimoTempo, paradoDesde := -1.0, m.d.Relogio.Agora()
	var durLeitura, durAPI, durCapt, durLe []time.Duration
	retratosInimigos := 0
	ultimaCaptura := int64(-1 << 40)

	for {
		if ctx.Err() != nil {
			return st, context.Cause(ctx)
		}
		if m.d.Relogio.Agora().Sub(ultimaFase) > IntervaloFase {
			ultimaFase = m.d.Relogio.Agora()
			fase, err := m.d.LCU.FaseGameflow(ctx)
			if err == nil && !replay.FasePermitida(fase) {
				return st, &replay.ErroFase{Fase: fase}
			}
		}
		cicloIni := m.d.Relogio.Agora()
		ini := time.Now()
		l, err := m.capturaVisao(ctx)
		tMs, pb, lt, errLe := l.tMs, l.pb, l.lt, l.errLe
		if err != nil {
			if ctx.Err() != nil {
				return st, context.Cause(ctx)
			}
			errosSeguidos++
			if errosSeguidos > 20 {
				return st, fmt.Errorf("o replay parou de responder: %w", err)
			}
			if err := m.d.Relogio.Dorme(ctx, 250*time.Millisecond); err != nil {
				return st, err
			}
			continue
		}
		errosSeguidos = 0
		durLeitura = append(durLeitura, time.Since(ini))
		durAPI, durCapt = append(durAPI, l.durAPI), append(durCapt, l.durCapt)
		if l.img != nil {
			durLe = append(durLe, l.durLe)
		}
		if pb.Pausado {
			m.d.API.MudaPlayback(ctx, replay.MudaPlayback{Pausado: replay.Ptr(false)})
		}
		if pb.Tempo != ultimoTempo {
			ultimoTempo, paradoDesde = pb.Tempo, m.d.Relogio.Agora()
		} else if m.d.Relogio.Agora().Sub(paradoDesde) > 5*time.Second {
			if float64(fimMs)/1000-pb.Tempo < 2 {
				break
			}
			return st, fmt.Errorf("o replay parou em %.1f s e não andou mais", pb.Tempo)
		}
		if tMs > fimMs || pb.Tempo >= m.s.Duracao-0.05 {
			break
		}
		if tMs <= inicio {
			if falta := intervalo - m.d.Relogio.Agora().Sub(cicloIni); falta > 0 {
				if err := m.d.Relogio.Dorme(ctx, falta); err != nil {
					return st, err
				}
			}
			continue
		}
		if errLe != nil {
			errosTela++
			if !avisouTela {
				avisouTela = true
				log.Printf("rastreio: passe de visão %d: %v", p.ID, errLe)
				m.ex.Emite("aviso_tela", map[string]any{"passe_visao_id": p.ID, "erro": errLe.Error()})
			}
			// Captura sem leitura não entra: o trecho fica sem cobertura (nem
			// visível nem fora), em vez de virar "fora da visão" por engano.
		} else {
			c := CapturaVisao{TMs: tMs, Modo: lt.Modo, Retratos: lt.Retratos, Sentinelas: lt.Sentinelas, Arcos: lt.Arcos}
			pend = append(pend, c)
			total++
			st.Amostras++
			if lt.Modo == ModoDoTime(p.Time) {
				modoOK++
				semModoDesde = -1
			} else {
				// O painel saiu da névoa do time (a pessoa mexeu, o jogo voltou ao
				// padrão): aperta de novo, com limite.
				if semModoDesde < 0 {
					semModoDesde = tMs
				} else if tMs-semModoDesde >= LimiteSemModoMs && teclas < MaxTeclasNevoa*2 {
					semModoDesde = -1
					m.ex.Emite("aviso", map[string]string{"texto": fmt.Sprintf("o painel do replay saiu da visão do time %d; apertando a tecla de novo", p.Time)})
					if err := m.s.MudaVisao(ctx, p.Time); errors.Is(err, replay.ErrUsuarioUsando) {
						// A pessoa está usando o computador: não aperta agora (as capturas
						// fora da névoa do time não contam como visão), tenta depois.
						m.ex.Emite("aviso", map[string]string{"texto": "a tecla da névoa espera: " + err.Error()})
					} else if err != nil {
						return st, err
					} else {
						teclas++
					}
				}
			}
			retratosInimigos = 0
			for _, r := range lt.Retratos {
				if r.Time != p.Time {
					retratosInimigos++
				}
			}
			if m.cfg.PastaCapturas != "" && float64(tMs-ultimaCaptura) >= m.cfg.CapturaACadaS*1000 {
				ultimaCaptura = tMs
				m.guardaCapturaVisao(p, l)
			}
		}
		tAtual = tMs
		if time.Since(ultimaGravacao) >= IntervaloGravacao || len(pend) >= 200 {
			if err := grava(); err != nil {
				return st, fmt.Errorf("gravar as capturas da visão: %w", err)
			}
		}
		if time.Since(ultimoSSE) >= IntervaloEventoSSE {
			ultimoSSE = time.Now()
			m.ex.Emite("visao_amostra", map[string]any{"passe_visao_id": p.ID, "time": p.Time, "t_ms": tMs, "modo": lt.Modo,
				"retratos": len(lt.Retratos), "inimigos_a_vista": retratosInimigos, "sentinelas": len(lt.Sentinelas),
				"capturas": total, "erros_leitura": errosTela})
		}
		if falta := intervalo - m.d.Relogio.Agora().Sub(cicloIni); falta > 0 {
			if err := m.d.Relogio.Dorme(ctx, falta); err != nil {
				return st, err
			}
		}
	}
	st.TFimMs = tAtual
	if st.TFimMs > st.TIniMs {
		st.TaxaJogo = float64(st.Amostras) / (float64(st.TFimMs-st.TIniMs) / 1000)
	}
	st.MsLeitura, st.MsAPI, st.MsCaptura, st.MsTela = medianaDur(durLeitura), medianaDur(durAPI), medianaDur(durCapt), medianaDur(durLe)
	log.Printf("rastreio: passe de visão %d a %g×: %.2f capturas por segundo de jogo; ciclo mediano %.1f ms (playback %.1f, captura %.1f, leitura do minimapa %.1f)",
		p.ID, vel, st.TaxaJogo, st.MsLeitura, st.MsAPI, st.MsCaptura, st.MsTela)
	return st, nil
}
