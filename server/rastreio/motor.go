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
	"sort"
	"sync"
	"time"

	"loltracking/server/banco"
	"loltracking/server/replay"
	"loltracking/server/web"
)

// Config do motor.
type Config struct {
	// Velocidade do replay (padrão 2).
	Velocidade float64
	// AmostrasPorSegundo de JOGO (padrão 4). O motor espera entre leituras
	// para não passar disso; se a leitura for mais lenta, a taxa cai.
	AmostrasPorSegundo float64
	// IntervaloEstadoMs: de quanto em quanto (tempo de jogo) gravar o estado dos 10.
	IntervaloEstadoMs int64
	// Leitor da tela (tarefa 3). Nil = LeitorNulo.
	Leitor LeitorDeTela
	// Riot: linha do tempo da match-v5 para conferir a leitura da tela depois de
	// cada passe (opcional; nil ou sem chave = não confere).
	Riot FonteRiot
	// PastaCapturas: se não vazia, guarda 1 captura PNG (+ JSON com o que a API
	// dizia) a cada CapturaACadaS segundos de jogo — material de teste da tarefa 3.
	PastaCapturas string
	CapturaACadaS float64
	// DepoisDaAmostra é chamado a cada amostra (os testes usam para pausar no meio).
	DepoisDaAmostra func(passeID int64, a Amostra)
	// Minimapa: a leitura do minimapa do passe de visão de time (pacote
	// visao). Nil = os passes de visão são recusados.
	Minimapa LeitorDeMinimapa
	// VelocidadeVisao: a velocidade do replay nos passes de visão (padrão
	// VelocidadeVisaoPadrao, 4).
	VelocidadeVisao float64
}

// ComPadroes é a configuração com os padrões preenchidos (main usa a taxa
// pedida na reclassificação dos passes ruins).
func (c Config) ComPadroes() Config { return c.comPadroes() }

func (c Config) comPadroes() Config {
	if c.Velocidade <= 0 {
		c.Velocidade = 2
	}
	if c.AmostrasPorSegundo <= 0 {
		c.AmostrasPorSegundo = 4
	}
	if c.IntervaloEstadoMs <= 0 {
		c.IntervaloEstadoMs = 1000
	}
	if c.Leitor == nil {
		c.Leitor = LeitorNulo{}
	}
	if c.CapturaACadaS <= 0 {
		c.CapturaACadaS = 30
	}
	if c.VelocidadeVisao <= 0 {
		c.VelocidadeVisao = VelocidadeVisaoPadrao
	}
	return c
}

// Intervalos do motor (tempo real). Variáveis para o teste.
var (
	// IntervaloGravacao: de quanto em quanto o lote de amostras vai para o banco.
	IntervaloGravacao = time.Second
	// IntervaloEventoSSE: no máximo um evento "amostra" por este intervalo.
	IntervaloEventoSSE = time.Second
	// IntervaloFase: de quanto em quanto reconferir a fase do cliente (regra 2).
	IntervaloFase = 10 * time.Second
	// LimiteSemTravaMs: jogador vivo sem a câmera nele por mais que isso (tempo de
	// jogo) = retravar.
	LimiteSemTravaMs int64 = 1000
	// JanelaRenascerMs: depois de renascer, quanto tempo de jogo a câmera tem
	// para voltar sozinha antes de contar como trava perdida.
	JanelaRenascerMs int64 = 2000
	// RecuoRetomadaMs: quanto antes do ponto gravado o seek da retomada cai.
	RecuoRetomadaMs int64 = 4000
)

// ErrPausado e ErrCancelado são as causas do cancelamento do contexto.
var (
	ErrPausado   = errors.New("rastreio pausado")
	ErrCancelado = errors.New("rastreio cancelado")
)

// alvo é o jogador seguido no passe.
type alvo struct {
	pid  int
	nome string // como no playerlist (riotIdGameName)
	time int
}

// motor roda os passes de uma sessão de replay aberta.
type motor struct {
	db  *banco.DB
	s   *replay.Sessao
	d   replay.Deps
	cfg Config
	ex  *web.Execucao
	lt  LinhaDoTempo
	cal Calibracao
	// pidDe: nome no jogo → participante_id; ordem: índice no playerlist → participante_id.
	pidDe map[string]int
	ordem []int

	salvando sync.Mutex
	// diag: o diagnóstico do passe em andamento (uso.go); aoEsperar avisa a
	// fila de que o motor está esperando a pessoa (GET /api/captura).
	diag      DiagnosticoPasse
	aoEsperar func(*EsperaUsuario)
}

// leituraJogo é uma leitura completa do jogo.
type leituraJogo struct {
	p1, p2  replay.Playback
	r       replay.Render
	js      []replay.Jogador
	img     *image.RGBA
	tela    Leitura
	errTela error
	dur     time.Duration
	durCapt time.Duration
	durTela time.Duration
}

// le faz uma leitura: playback, (captura em paralelo) render, playerlist,
// playback. O tempo da amostra é a média dos dois playback (descobertas 3.2).
func (m *motor) le(ctx context.Context) (leituraJogo, error) {
	var l leituraJogo
	ini := time.Now()
	var err error
	if l.p1, err = m.d.API.Playback(ctx); err != nil {
		return l, err
	}
	type capt struct {
		img *image.RGBA
		dur time.Duration
	}
	ch := make(chan capt, 1)
	go func() {
		c := time.Now()
		img, err := m.d.Tela.Captura(image.Rectangle{})
		if err != nil {
			img = nil
		}
		ch <- capt{img, time.Since(c)}
	}()
	l.r, err = m.d.API.Render(ctx)
	if err == nil {
		l.js, err = m.d.API.Jogadores(ctx)
	}
	if err == nil {
		l.p2, err = m.d.API.Playback(ctx)
	}
	c := <-ch
	if err != nil {
		return l, err
	}
	l.img, l.durCapt = c.img, c.dur
	if l.img != nil {
		c := time.Now()
		if lt, err := m.cfg.Leitor.Le(l.img, m.s.Largura, m.s.Altura); err == nil {
			l.tela = lt
		} else {
			l.errTela = err
		}
		l.durTela = time.Since(c)
	}
	l.dur = time.Since(ini)
	return l, nil
}

// EstatPasse é o resumo de uma execução de passe (para o SSE e a sonda).
type EstatPasse struct {
	Amostras   int     `json:"amostras"`
	Retravas   int     `json:"retravas"`
	Tentativas int     `json:"tentativas_trava"`
	SemTrava   int     `json:"amostras_sem_trava"`
	Camera     int     `json:"amostras_camera"`
	Mortes     int     `json:"mortes_vistas"`
	TaxaJogo   float64 `json:"taxa"`
	MsLeitura  float64 `json:"ms_leitura_mediana"`
	MsCaptura  float64 `json:"ms_captura_mediana"`
	MsTela     float64 `json:"ms_tela_mediana"`
	// MsAPI: os pedidos à Replay API do ciclo (só no passe de visão).
	MsAPI          float64 `json:"ms_api_mediana,omitempty"`
	TIniMs, TFimMs int64
}

func medianaDur(v []time.Duration) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]time.Duration(nil), v...)
	sort.Slice(s, func(a, b int) bool { return s[a] < s[b] })
	return float64(s[len(s)/2].Microseconds()) / 1000
}

// esperaSeek espera o replay chegar ao tempo pedido.
func (m *motor) esperaSeek(ctx context.Context, alvoS float64) error {
	limite := m.d.Relogio.Agora().Add(15 * time.Second)
	for {
		pb, err := m.d.API.Playback(ctx)
		// O relógio volta a andar logo depois do seek: a 8× passa 8 s de jogo
		// por segundo real, então a folga cresce com a velocidade.
		folga := 3 + 4*m.cfg.Velocidade
		if err == nil && !pb.Buscando && pb.Tempo >= alvoS-1 && pb.Tempo <= alvoS+folga {
			return nil
		}
		if m.d.Relogio.Agora().After(limite) {
			return fmt.Errorf("o replay não chegou em %.0f s depois do seek", alvoS)
		}
		if err := m.d.Relogio.Dorme(ctx, 100*time.Millisecond); err != nil {
			return err
		}
	}
}

// rodaPasse segue um jogador de p.TAtualMs até fimMs (0 = fim da partida).
// Devolve a causa se o contexto foi cancelado (ErrPausado/ErrCancelado), ou o
// erro. As amostras ficam nas tabelas de trabalho; quem fecha é finalizaPasse.
func (m *motor) rodaPasse(ctx context.Context, p Passe, a alvo, fimMs int64) (EstatPasse, error) {
	mudaEstadoPasse(m.db, p.ID, Rodando, "")
	duracaoMs := int64(m.s.Duracao * 1000)
	m.db.Exec(`UPDATE passes SET duracao_ms = ? WHERE id = ?`, duracaoMs, p.ID)
	if fimMs <= 0 || fimMs > duracaoMs {
		fimMs = duracaoMs
	}
	inicio := p.TAtualMs
	m.ex.Emite("passe_inicio", map[string]any{"passe_id": p.ID, "participante_id": a.pid, "nome": a.nome,
		"t_inicio_ms": inicio, "t_fim_ms": fimMs, "duracao_ms": duracaoMs})
	pr := progresso{TAtualMs: inicio, Retravas: p.Retravas, Tentativas: p.TentativasTrava, SemTrava: p.AmostrasSemTrava}
	// O diagnóstico do passe (por que a trava cai) vai para o banco também
	// quando o passe para no meio: é o que diz a causa de um passe ruim.
	m.diag = DiagnosticoPasse{Conferencia: map[string]int{}}
	antes := m.s.Diagnostico()
	defer func() { gravaDiagnostico(m.db, p.ID, m.diag, m.s.Diagnostico().Menos(antes)) }()
	return m.segueComEspera(ctx, trecho{p: p, a: a, inicio: inicio, fim: fimMs, pr: &pr,
		destino: func(as []Amostra, es []EstadoJogador, pr progresso) error {
			return gravaBrutas(m.db, p.ID, p.GameID, a.pid, as, es, pr)
		},
		recua: func(ateMs int64) error { return recuaBrutas(m.db, p.ID, ateMs, &pr) }})
}

// trecho é um pedaço do replay que o motor segue e grava: o passe inteiro
// (rodaPasse) ou um buraco sendo repescado (repescagem.go).
type trecho struct {
	p Passe
	a alvo
	// inicio: só grava leituras depois deste t (o seek cai RecuoRetomadaMs
	// antes, para a trava já estar pronta); fim: para depois dele.
	inicio, fim int64
	pr          *progresso
	// destino recebe cada lote de amostras e estados (com o progresso de agora).
	destino func(as []Amostra, es []EstadoJogador, pr progresso) error
	// repescagem: o evento "amostra" sai marcado (a tela mostra que é repescagem).
	repescagem bool
	// recua apaga o que foi entregue depois de ateMs (o trecho sem a câmera
	// antes de uma espera pela pessoa ou de um disparo do disjuntor: uso.go),
	// para a retomada regravar dali em vez de deixar o vazio no dado.
	recua func(ateMs int64) error
}

// retrava é a retravada da câmera SEM parar de gravar: o motor aperta a tecla
// dupla (ApertaDuplo, ~0,3 s real) e volta a ler; a conferência é a das
// próprias leituras (ConfereTrava em cada amostra). Enquanto isso as amostras
// do jogador saem com trava_perdida e sem posição, e o estado dos 10 (que não
// depende da câmera) continua sendo gravado. Antes o motor parava em
// TravaCamera (~0,9 s real por tentativa, até 5): a 4× eram até 18 s de jogo
// sem nenhuma linha gravada.
type retrava struct {
	ativa      bool
	tentativas int
	esperando  bool
	teclaEm    time.Time
}

// passoRetrava avança a retravada depois de uma leitura: ok = a leitura já
// conferiu a trava; morto = o jogador morreu (a câmera volta sozinha ao
// renascer); selOK = a seleção do replay está no jogador.
//
// desistiu = o ciclo acabou as MaxTentativasTrava sem a câmera chegar (o
// disjuntor conta esses ciclos seguidos). motivo é o da última conferência que
// falhou (vai no evento "trava"). A tecla recusada porque a pessoa está usando
// o computador vira pedidoEspera (o passe espera: uso.go).
func (m *motor) passoRetrava(ctx context.Context, rt *retrava, p Passe, a alvo, ok, morto, selOK bool, motivo string, pr *progresso) (bool, error) {
	if !rt.ativa {
		return false, nil
	}
	if ok {
		rt.ativa = false
		m.ex.Emite("trava", map[string]any{"passe_id": p.ID, "tentativas": rt.tentativas, "ok": true})
		return false, nil
	}
	if morto {
		rt.ativa = false
		return false, nil
	}
	if rt.esperando {
		// Espera a câmera chegar até EsperaTrava. Mas se a seleção saiu do
		// jogador depois da tecla, ela não vai chegar sozinha: aperta de novo
		// já (na 3305449546 a tecla no meio do teleporte do Azir, 12:31,
		// deixou a seleção vazia até a tecla seguinte, 2,3 s de jogo depois;
		// descobertas 8).
		passou := m.d.Relogio.Agora().Sub(rt.teclaEm)
		if passou < replay.EsperaTrava && (selOK || passou < replay.EsperaMinimaTrava) {
			return false, nil
		}
	}
	if rt.tentativas >= replay.MaxTentativasTrava {
		rt.ativa = false
		m.ex.Emite("trava", map[string]any{"passe_id": p.ID, "tentativas": rt.tentativas, "ok": false,
			"motivo": fmt.Sprintf("%s %s (%s)", replay.ErrTravaFalhou, a.nome, motivo)})
		return true, nil
	}
	rt.tentativas++
	pr.Tentativas++
	err := m.s.ApertaDuplo(ctx, a.nome)
	switch {
	case err == nil:
		rt.esperando, rt.teclaEm = true, m.d.Relogio.Agora()
	case errors.Is(err, replay.ErrMorto):
		rt.ativa = false
	case errors.Is(err, replay.ErrUsuarioUsando):
		rt.tentativas--
		pr.Tentativas--
		return false, &pedidoEspera{motivo: err.Error()}
	default:
		if ctx.Err() != nil {
			return false, context.Cause(ctx)
		}
		return false, err
	}
	return false, nil
}

// segue é o laço de leitura: seek para perto de tr.inicio, trava a câmera e lê
// até tr.fim, entregando as amostras e o estado dos 10 a tr.destino.
//
// Devolve *pedidoEspera quando o trecho precisa parar e esperar (a pessoa está
// usando o computador, ou o disjuntor disparou): quem chama é segueComEspera
// (uso.go), que espera e chama de novo do ponto bom.
func (m *motor) segue(ctx context.Context, tr trecho) (st EstatPasse, errSegue error) {
	cfg := m.cfg
	p, a, inicio, fimMs, pr := tr.p, tr.a, tr.inicio, tr.fim, tr.pr
	st.TIniMs = inicio
	retravasAntes, tentativasAntes, semTravaAntes := pr.Retravas, pr.Tentativas, pr.SemTrava
	var durLeitura, durCaptura, durTela []time.Duration
	// O resumo sai também quando o trecho para no meio (espera, pausa): quem
	// soma as partes (segueComEspera) precisa das contas de cada uma.
	defer func() {
		st.TFimMs = max(pr.TAtualMs, st.TIniMs)
		st.Retravas, st.Tentativas, st.SemTrava = pr.Retravas-retravasAntes, pr.Tentativas-tentativasAntes, pr.SemTrava-semTravaAntes
		if st.TFimMs > st.TIniMs {
			st.TaxaJogo = float64(st.Amostras) / (float64(st.TFimMs-st.TIniMs) / 1000)
		}
		st.MsLeitura, st.MsCaptura, st.MsTela = medianaDur(durLeitura), medianaDur(durCaptura), medianaDur(durTela)
	}()
	// Disjuntor (uso.go): ciclos de retrava seguidos que acabaram sem a câmera,
	// e as amostras vivas / sem trava deste trecho. ultimoBom: a última amostra
	// com a posição de verdade (para a retomada recuar até ela).
	ciclosFalhos, vivas, semTravaTr := 0, 0, 0
	var primeiroMs, ultimoBom int64 = -1, -1
	pedeEspera := func(motivo string, disjuntor bool) error {
		pe := &pedidoEspera{motivo: motivo, disjuntor: disjuntor, ultimoBomMs: ultimoBom}
		return pe
	}

	// Na retomada o seek vai um pouco antes do ponto gravado: a trava (que cai
	// em todo seek) leva ~1 s real, e assim ela já está pronta ao passar pelo
	// ponto em que o passe parou. Leituras antes dele não são gravadas.
	alvoS := math.Max(0, float64(inicio-RecuoRetomadaMs)/1000)
	if _, err := m.d.API.MudaPlayback(ctx, replay.MudaPlayback{Tempo: &alvoS, Velocidade: replay.Ptr(cfg.Velocidade),
		Pausado: replay.Ptr(false)}); err != nil {
		return st, err
	}
	if err := m.esperaSeek(ctx, alvoS); err != nil {
		return st, err
	}

	var pendA []Amostra
	var pendE []EstadoJogador
	ultimaGravacao := time.Now()
	grava := func() error {
		if len(pendA) == 0 && len(pendE) == 0 {
			return nil
		}
		err := tr.destino(pendA, pendE, *pr)
		pendA, pendE = pendA[:0], pendE[:0]
		ultimaGravacao = time.Now()
		return err
	}
	defer func() {
		if err := grava(); err != nil {
			log.Printf("rastreio: gravar o último lote do passe %d: %v", p.ID, err)
		}
	}()

	precisaTravar := true
	var rt retrava
	var perdidaDesde, renascidoEm int64 = -1, -1
	ultimoEstado := int64(-1 << 40) // longe no passado, sem estourar na subtração
	ultimaCaptura := int64(-1 << 40)
	estavaMorto := false
	ultimoSSE := time.Time{}
	ultimaFase := m.d.Relogio.Agora()
	errosSeguidos := 0
	ultimoTempo, paradoDesde := -1.0, time.Time{}
	san := novoSaneador()
	avisouTela := false
	avisouPainel := false
	var semPainelDesde int64 = -1
	intervalo := time.Duration(float64(time.Second) / cfg.AmostrasPorSegundo / cfg.Velocidade)

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
		if precisaTravar {
			precisaTravar = false
			n, err := m.s.TravaCamera(ctx, a.nome)
			pr.Tentativas += n
			switch {
			case err == nil:
				m.ex.Emite("trava", map[string]any{"passe_id": p.ID, "tentativas": n, "ok": true})
			case errors.Is(err, replay.ErrMorto):
			case errors.Is(err, replay.ErrUsuarioUsando):
				// A pessoa está usando o computador: nada de roubar o foco. O
				// trecho espera (o replay pausa) e volta daqui.
				return st, pedeEspera(err.Error(), false)
			case errors.Is(err, replay.ErrTravaFalhou):
				ciclosFalhos++
				m.ex.Emite("trava", map[string]any{"passe_id": p.ID, "tentativas": n, "ok": false, "motivo": err.Error()})
			default:
				if ctx.Err() != nil {
					return st, context.Cause(ctx)
				}
				return st, err
			}
		}

		cicloIni := m.d.Relogio.Agora()
		l, err := m.le(ctx)
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
		durLeitura, durCaptura = append(durLeitura, l.dur), append(durCaptura, l.durCapt)
		tMs := int64(math.Round((l.p1.Tempo + l.p2.Tempo) / 2 * 1000))

		// O replay parou? (pausado pela pessoa, ou travado)
		if l.p2.Pausado {
			m.d.API.MudaPlayback(ctx, replay.MudaPlayback{Pausado: replay.Ptr(false)})
		}
		if l.p2.Tempo != ultimoTempo {
			ultimoTempo, paradoDesde = l.p2.Tempo, m.d.Relogio.Agora()
		} else if m.d.Relogio.Agora().Sub(paradoDesde) > 5*time.Second {
			if float64(fimMs)/1000-l.p2.Tempo < 2 {
				break
			}
			return st, fmt.Errorf("o replay parou em %.1f s e não andou mais", l.p2.Tempo)
		}
		if tMs > fimMs || l.p2.Tempo >= m.s.Duracao-0.05 {
			break
		}
		if inicio > 0 && tMs <= inicio {
			// Recuo da retomada: só trava, não grava.
			if falta := intervalo - m.d.Relogio.Agora().Sub(cicloIni); falta > 0 {
				if err := m.d.Relogio.Dorme(ctx, falta); err != nil {
					return st, err
				}
			}
			continue
		}

		var j *replay.Jogador
		for k := range l.js {
			if l.js[k].NomeDeSelecao() == a.nome {
				j = &l.js[k]
			}
		}
		if j == nil {
			return st, fmt.Errorf("o jogador %q sumiu do playerlist", a.nome)
		}
		if l.errTela != nil && !avisouTela {
			// A tela não é a que o leitor conhece (resolução, escala do HUD): avisa
			// uma vez por passe e segue com as colunas da tela nulas.
			avisouTela = true
			log.Printf("rastreio: passe %d: %v", p.ID, l.errTela)
			m.ex.Emite("aviso_tela", map[string]any{"passe_id": p.ID, "erro": l.errTela.Error()})
		}
		san.saneia(&l.tela, l.r.Selecao == a.nome, j.Morto, j.Nivel)
		if l.img != nil {
			durTela = append(durTela, l.durTela)
		}
		am := Amostra{TMs: tMs, Morto: j.Morto, Respawn: j.Renasce, Nivel: j.Nivel, CS: j.Placar.CS,
			Abates: j.Placar.Abates, Mortes: j.Placar.Mortes, Assistencias: j.Placar.Assistencias, Ward: j.Placar.Ward,
			DtMs: int64(math.Round((l.p2.Tempo - l.p1.Tempo) * 1000)), SelecaoOK: l.r.Selecao == a.nome,
			Vida: l.tela.Vida, VidaMax: l.tela.VidaMax, Mana: l.tela.Mana, ManaMax: l.tela.ManaMax, CD: l.tela.CD, Atr: l.tela.Atr}
		// Jogador vivo, câmera nele, captura feita e mesmo assim nenhuma vida lida
		// por PainelCobertoMs de jogo: algo está por cima do painel (outra janela)
		// ou o HUD mudou. Avisa uma vez; a posição não depende disso e continua.
		if l.img != nil && l.errTela == nil && am.SelecaoOK && !j.Morto && am.Vida == nil {
			if semPainelDesde < 0 {
				semPainelDesde = tMs
			} else if !avisouPainel && tMs-semPainelDesde >= PainelCobertoMs {
				avisouPainel = true
				msg := "o painel do jogador (canto inferior esquerdo do jogo) não está sendo lido: vida, recargas e atributos deste passe vão ficar vazios. Confira se há outra janela por cima do jogo."
				log.Printf("rastreio: passe %d: %s", p.ID, msg)
				m.ex.Emite("aviso_tela", map[string]any{"passe_id": p.ID, "erro": msg})
			}
		} else if am.Vida != nil {
			semPainelDesde = -1
		}
		cx, cz := l.r.CameraPosicao.X, l.r.CameraPosicao.Z
		am.CamX, am.CamZ = &cx, &cz
		if j.Centro.Valido {
			sx, sy := j.Centro.X, j.Centro.Y
			am.TelaX, am.TelaY = &sx, &sy
		}
		var ok bool
		motivoConf := ""
		if j.Morto {
			if !estavaMorto {
				st.Mortes++
			}
			am.Origem = OrigemMorte
			if e := m.lt.MorteAntes(a.pid, tMs); e != nil && e.X != nil {
				x, y := *e.X, *e.Y
				am.X, am.Y = &x, &y
			}
			perdidaDesde, renascidoEm = -1, -1
		} else {
			if estavaMorto {
				renascidoEm = tMs
			}
			ok, motivoConf = replay.ConfereTrava(l.r, l.js, a.nome, float64(m.s.Largura), float64(m.s.Altura))
			vivas++
			switch {
			case ok:
				am.Origem = OrigemCamera
				x, y := m.cal.Posicao(cx, cz, j.Centro.X, j.Centro.Y)
				am.X, am.Y = &x, &y
				perdidaDesde = -1
				ciclosFalhos = 0
				st.Camera++
			case renascidoEm >= 0 && tMs-renascidoEm <= JanelaRenascerMs:
				// Acabou de renascer: está na fonte; a seleção costuma voltar sozinha.
				am.Origem = OrigemBase
				f := FonteDoTime(a.time)
				am.X, am.Y = &f[0], &f[1]
			default:
				// Sem a câmera nele: a amostra sai sem posição e marcada (o
				// buraco fica explícito no dado; finalizaPasse interpola os
				// curtos e a repescagem refaz os longos).
				am.Origem = OrigemInterpolada
				am.TravaPerdida = true
				pr.SemTrava++
				semTravaTr++
				m.diag.anotaConferencia(motivoConf)
				if !rt.ativa {
					if perdidaDesde < 0 {
						perdidaDesde = tMs
					}
					if tMs-perdidaDesde > LimiteSemTravaMs {
						rt = retrava{ativa: true}
						pr.Retravas++
						perdidaDesde = -1
					}
				}
			}
		}
		desistiu, err := m.passoRetrava(ctx, &rt, p, a, am.Origem == OrigemCamera, j.Morto, am.SelecaoOK, motivoConf, pr)
		if err != nil {
			var pe *pedidoEspera
			if errors.As(err, &pe) {
				pe.ultimoBomMs = ultimoBom
			}
			return st, err
		}
		if desistiu {
			ciclosFalhos++
		}
		estavaMorto = j.Morto
		pendA = append(pendA, am)
		st.Amostras++
		pr.TAtualMs = tMs
		if primeiroMs < 0 {
			primeiroMs = tMs
		}
		if !am.TravaPerdida {
			ultimoBom = tMs
		}
		// Disjuntor: a trava não volta (K ciclos de retrava seguidos sem a
		// câmera, ou a fração sem trava alta depois de T s de jogo). Não gasta o
		// resto do passe produzindo lixo: para e espera (uso.go). Na repescagem
		// o trecho só termina (o buraco fica como estava).
		if motivo := motivoDisjuntor(ciclosFalhos, vivas, semTravaTr, tMs-primeiroMs); motivo != "" {
			m.ex.Emite("disjuntor", map[string]any{"passe_id": p.ID, "participante_id": a.pid, "t_ms": tMs, "motivo": motivo,
				"repescagem": tr.repescagem})
			if tr.repescagem {
				break
			}
			if err := grava(); err != nil {
				return st, fmt.Errorf("gravar amostras: %w", err)
			}
			return st, pedeEspera(motivo, true)
		}

		if tMs-ultimoEstado >= cfg.IntervaloEstadoMs {
			ultimoEstado = tMs
			for k, jj := range l.js {
				pid := m.pidDe[jj.NomeDeSelecao()]
				if pid == 0 && k < len(m.ordem) {
					pid = m.ordem[k]
				}
				e := EstadoJogador{TMs: tMs, ParticipanteID: pid, Nivel: jj.Nivel, Abates: jj.Placar.Abates,
					Mortes: jj.Placar.Mortes, Assistencias: jj.Placar.Assistencias, CS: jj.Placar.CS, Ward: jj.Placar.Ward,
					Morto: jj.Morto, Respawn: jj.Renasce}
				if k < 10 {
					e.VidaPct, e.ManaPct = l.tela.Quadros[k].VidaPct, l.tela.Quadros[k].ManaPct
				}
				for _, it := range jj.Itens {
					if it.Slot >= 0 && it.Slot < 7 {
						e.Slots[it.Slot] = SlotItem{ID: it.ID, Qtd: it.Quantidade}
					}
				}
				pendE = append(pendE, e)
			}
		}
		if cfg.PastaCapturas != "" && l.img != nil && float64(tMs-ultimaCaptura) >= cfg.CapturaACadaS*1000 {
			ultimaCaptura = tMs
			m.guardaCaptura(p, a, tMs, l)
		}
		if cfg.DepoisDaAmostra != nil {
			cfg.DepoisDaAmostra(p.ID, am)
		}
		if time.Since(ultimaGravacao) >= IntervaloGravacao || len(pendA) >= 200 {
			if err := grava(); err != nil {
				return st, fmt.Errorf("gravar amostras: %w", err)
			}
		}
		if time.Since(ultimoSSE) >= IntervaloEventoSSE {
			ultimoSSE = time.Now()
			ev := map[string]any{"passe_id": p.ID, "participante_id": a.pid, "t_ms": tMs, "origem": am.Origem, "repescagem": tr.repescagem,
				"morto": am.Morto, "selecao": l.r.Selecao, "amostras": p.Amostras + st.Amostras, "retravas": pr.Retravas,
				"ms_leitura": float64(l.dur.Microseconds()) / 1000}
			if am.X != nil {
				ev["x"], ev["y"] = math.Round(*am.X), math.Round(*am.Y)
			}
			// A "última leitura" da tela de rastreio: o que a tela leu agora
			// (nulo = não lido) e o placar do playerlist.
			ev["trava_perdida"], ev["nivel"], ev["cs"] = am.TravaPerdida, am.Nivel, am.CS
			ev["vida"], ev["vida_max"], ev["mana"], ev["mana_max"], ev["cd"] = am.Vida, am.VidaMax, am.Mana, am.ManaMax, am.CD
			m.ex.Emite("amostra", ev)
		}
		// Ritmo: no máximo AmostrasPorSegundo por segundo de jogo.
		if falta := intervalo - m.d.Relogio.Agora().Sub(cicloIni); falta > 0 {
			if err := m.d.Relogio.Dorme(ctx, falta); err != nil {
				return st, err
			}
		}
	}
	return st, nil
}

// guardaCaptura grava a captura em PNG e um JSON com o que a API dizia no
// instante (a "resposta certa" para testar a leitura da tela na tarefa 3).
// Em segundo plano e uma de cada vez: codificar PNG de 2560×1440 leva ~100 ms.
func (m *motor) guardaCaptura(p Passe, a alvo, tMs int64, l leituraJogo) {
	if !m.salvando.TryLock() {
		return
	}
	img := image.NewRGBA(l.img.Bounds())
	copy(img.Pix, l.img.Pix)
	info := map[string]any{"game_id": p.GameID, "participante_id": a.pid, "nome": a.nome, "t_ms": tMs,
		"largura": m.s.Largura, "altura": m.s.Altura, "selecao": l.r.Selecao, "jogadores": l.js}
	go func() {
		defer m.salvando.Unlock()
		dir := filepath.Join(m.cfg.PastaCapturas, fmt.Sprint(p.GameID))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Printf("rastreio: pasta de capturas: %v", err)
			return
		}
		base := filepath.Join(dir, fmt.Sprintf("p%02d_t%07d", a.pid, tMs))
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

// coletaEventos grava o eventdata do jogo (o que passou desde o último seek).
func (m *motor) coletaEventos(gameID int64) {
	ctx, cancela := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancela()
	evs, err := m.d.API.Eventos(ctx)
	if err != nil {
		log.Printf("rastreio: eventdata da partida %d: %v", gameID, err)
		return
	}
	if _, err := GravaEventosJogo(m.db, gameID, evs, m.pidDe); err != nil {
		log.Printf("rastreio: gravar eventos da partida %d: %v", gameID, err)
	}
}
