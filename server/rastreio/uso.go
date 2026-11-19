package rastreio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"loltracking/server/banco"
	"loltracking/server/replay"
)

// A pessoa usando o computador, e o disjuntor (2026-11-18). Na partida
// 3305486988 (rastreio 9, 4×) os passes dos participantes 4, 5, 6 e 8 saíram
// com a câmera fora do jogador em ~70% das amostras: desde a primeira amostra
// do passe 4, a seleção do replay nunca ficou no jogador (selecao_ok falso em
// todas as amostras sem trava) e a câmera pulava pelo mapa (a "Directed
// Camera"); 844–933 teclas duplas por passe, todas sem efeito, e o passe 8
// destravou sozinho aos 20:36 de jogo. O motor não desistia porque cada ciclo
// de retravada (MaxTentativasTrava) acabava e outro começava 1 s depois, e
// fechava o passe como concluído. Agora:
//   - a tecla não sai se a pessoa está usando o computador (replay.Presenca):
//     o trecho espera, com o replay pausado, até o computador ficar parado;
//   - o disjuntor para o trecho quando a trava não volta, em vez de gravar o
//     resto do passe sem a câmera;
//   - o passe que mesmo assim sai ruim não vale (qualidade_passe.go).

// Limiares da espera e do disjuntor (variáveis para o teste).
var (
	// OciosoParaRetomar (M): o computador parado (sem entrada da pessoa) por
	// isso = o trecho volta a andar.
	OciosoParaRetomar = 5 * time.Second
	// TetoEsperaUsuario: esperando a pessoa por mais que isso, a captura pausa
	// de vez (como o botão "Pausar captura") e avisa. Antes disso o replay fica
	// aberto e pausado (regra 5: não fica aberto à toa).
	TetoEsperaUsuario = 3 * time.Minute
	// IntervaloEsperaUsuario: de quanto em quanto conferir se o computador parou.
	IntervaloEsperaUsuario = 500 * time.Millisecond
	// DisjuntorCiclos (K): ciclos de retravada seguidos que acabam sem a
	// câmera no jogador (cada um são MaxTentativasTrava teclas duplas). Um
	// teleporte (~6 s) gasta no máximo um ou dois (descobertas 8.1).
	DisjuntorCiclos = 3
	// DisjuntorFracaoSemTrava (X): fração das amostras vivas sem a câmera...
	DisjuntorFracaoSemTrava = 0.30
	// DisjuntorAposMs (T): ...a partir deste tanto de jogo no trecho. Os passes
	// bons têm 0,3–4,5% (o Master Yi, com a Investida Alfa, 4,5%).
	DisjuntorAposMs int64 = 120000
	// DisjuntorMaxDisparos: disparos seguidos (sem a câmera no jogador entre
	// eles) que pausam a captura de vez. O primeiro espera o computador ficar
	// parado e tenta de novo do último ponto bom.
	DisjuntorMaxDisparos = 2
	// DisjuntorMaxPorTrecho: disparos no mesmo trecho, mesmo com câmera entre
	// eles (trava que pega e cai de novo sem parar).
	DisjuntorMaxPorTrecho = 6
	// DisjuntorProgressoMs: quanto o último ponto bom precisa andar (jogo)
	// entre dois disparos para o segundo não contar como "seguido".
	DisjuntorProgressoMs int64 = 30000
)

// pedidoEspera é o "pare e espere" que segue devolve (não é falha).
type pedidoEspera struct {
	motivo    string
	disjuntor bool
	// ultimoBomMs: a última amostra com a posição de verdade (-1 = nenhuma no
	// trecho): a retomada recua até ela e regrava dali.
	ultimoBomMs int64
}

func (e *pedidoEspera) Error() string { return "esperando: " + e.motivo }

// EsperaUsuario é o que GET /api/captura mostra enquanto o motor espera.
type EsperaUsuario struct {
	Motivo    string `json:"motivo"`
	Disjuntor bool   `json:"disjuntor"`
	Desde     string `json:"desde"`
	TetoS     int    `json:"teto_s"`
	OciosoS   int    `json:"ocioso_para_retomar_s"`
}

// pausaDoMotor é a pausa que o próprio motor decide (o teto da espera, o
// disjuntor de novo): vale como ErrPausado (o rastreio fica pausado e retoma
// do ponto), com a frase do motivo.
func pausaDoMotor(msg string) error { return fmt.Errorf("%w: %s", ErrPausado, msg) }

// MotivoPausa tira a frase de uma pausa do motor ("" = pausa pedida).
func MotivoPausa(err error) string {
	if err == nil || errors.Is(err, ErrPausado) && err.Error() == ErrPausado.Error() {
		return ""
	}
	return strings.TrimPrefix(err.Error(), ErrPausado.Error()+": ")
}

// motivoDisjuntor devolve a frase quando o disjuntor dispara ("" = não).
func motivoDisjuntor(ciclosFalhos, vivas, semTrava int, jogoMs int64) string {
	if ciclosFalhos >= DisjuntorCiclos {
		return fmt.Sprintf("a câmera não prendeu no jogador em %d ciclos de retravada seguidos (%d teclas duplas)", ciclosFalhos,
			ciclosFalhos*replay.MaxTentativasTrava)
	}
	if jogoMs >= DisjuntorAposMs && vivas >= 40 && float64(semTrava)/float64(vivas) > DisjuntorFracaoSemTrava {
		return fmt.Sprintf("%.0f%% das amostras vivas sem a câmera no jogador em %.0f s de jogo", 100*float64(semTrava)/float64(vivas),
			float64(jogoMs)/1000)
	}
	return ""
}

// segueComEspera é segue com a espera pela pessoa e o disjuntor: quando segue
// pede para parar, recua o que foi gravado sem a câmera desde a última
// amostra boa, pausa o replay, espera o computador ficar parado
// (OciosoParaRetomar) e chama segue de novo dali (seek RecuoRetomadaMs antes,
// retrava: o mesmo caminho da retomada, sem buraco). O relógio do trecho (a
// taxa) é o do jogo: a espera não conta, o replay estava pausado.
func (m *motor) segueComEspera(ctx context.Context, tr trecho) (EstatPasse, error) {
	total := EstatPasse{TIniMs: tr.inicio}
	seguidos, noTrecho := 0, 0
	var bomNoDisparo int64 = -1
	for {
		st, err := m.segue(ctx, tr)
		total = somaEstat(total, st)
		var pe *pedidoEspera
		if !errors.As(err, &pe) {
			return total, err
		}
		// Recua o vazio do fim (amostras sem a câmera depois da última boa): a
		// retomada regrava dali, e a pausa de vez também guarda esse ponto.
		ate := max(pe.ultimoBomMs, tr.inicio)
		if tr.recua != nil && ate < tr.pr.TAtualMs {
			if err := tr.recua(ate); err != nil {
				return total, fmt.Errorf("recuar o trecho sem a câmera: %w", err)
			}
		}
		tr.pr.TAtualMs = ate
		if pe.disjuntor {
			// Só conta como "voltou a funcionar" se o ponto bom andou de verdade
			// desde o disparo anterior (a trava logo depois do seek pega antes de
			// o problema voltar e não prova nada).
			if bomNoDisparo >= 0 && ate-bomNoDisparo >= DisjuntorProgressoMs {
				seguidos = 0
			}
			bomNoDisparo = ate
			seguidos++
			noTrecho++
			m.diag.Disjuntor++
			if seguidos >= DisjuntorMaxDisparos || noTrecho >= DisjuntorMaxPorTrecho {
				msg := fmt.Sprintf("a câmera não prende no jogador (%s); a captura pausou para não gravar lixo. "+
					"Retome quando o computador estiver livre; se continuar, veja o diagnóstico do passe", pe.motivo)
				m.ex.Emite("aviso", map[string]string{"texto": msg})
				return total, pausaDoMotor(msg)
			}
		}
		if err := m.esperaUsuario(ctx, tr.p.GameID, pe); err != nil {
			return total, err
		}
		tr.inicio = max(tr.inicio, tr.pr.TAtualMs)
	}
}

func somaEstat(a, b EstatPasse) EstatPasse {
	a.Amostras += b.Amostras
	a.Retravas += b.Retravas
	a.Tentativas += b.Tentativas
	a.SemTrava += b.SemTrava
	a.Camera += b.Camera
	a.Mortes += b.Mortes
	if b.TFimMs > a.TFimMs {
		a.TFimMs = b.TFimMs
	}
	if b.MsLeitura > 0 {
		a.MsLeitura, a.MsCaptura, a.MsTela = b.MsLeitura, b.MsCaptura, b.MsTela
	}
	if a.TFimMs > a.TIniMs {
		a.TaxaJogo = float64(a.Amostras) / (float64(a.TFimMs-a.TIniMs) / 1000)
	}
	return a
}

// esperaUsuario pausa o replay (Replay API, não precisa de foco nem de tecla)
// e espera o computador ficar parado por OciosoParaRetomar. Sem Presenca, ou
// com a pessoa já parada (o disjuntor), volta logo. Passou do teto: a captura
// pausa de vez (pausaDoMotor). Antes de pausar guarda o eventdata do jogo: ele
// só tem o que passou desde o último seek, e a volta é um seek.
func (m *motor) esperaUsuario(ctx context.Context, gameID int64, pe *pedidoEspera) error {
	m.coletaEventos(gameID)
	if _, err := m.d.API.MudaPlayback(ctx, replay.MudaPlayback{Pausado: replay.Ptr(true)}); err != nil {
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}
		return err
	}
	ini := m.d.Relogio.Agora()
	e := &EsperaUsuario{Motivo: pe.motivo, Disjuntor: pe.disjuntor, Desde: ini.UTC().Format(time.RFC3339),
		TetoS: int(TetoEsperaUsuario / time.Second), OciosoS: int(OciosoParaRetomar / time.Second)}
	m.ex.Emite("espera_usuario", e)
	log.Printf("rastreio: esperando: %s", pe.motivo)
	if m.aoEsperar != nil {
		m.aoEsperar(e)
		defer m.aoEsperar(nil)
	}
	pres := m.d.Presenca
	for {
		agora := m.d.Relogio.Agora()
		parado := replay.OciosoDesde(pres, agora) >= OciosoParaRetomar && (pres == nil || !pres.ModificadorPressionado())
		if parado {
			break
		}
		if agora.Sub(ini) >= TetoEsperaUsuario {
			msg := fmt.Sprintf("o computador ficou em uso por mais de %s com o passe esperando; a captura pausou. Retome quando quiser (Ctrl+Alt+P, o botão \"Retomar captura\" ou a bandeja)",
				TetoEsperaUsuario.Round(time.Second))
			m.ex.Emite("aviso", map[string]string{"texto": msg})
			return pausaDoMotor(msg)
		}
		if err := m.d.Relogio.Dorme(ctx, IntervaloEsperaUsuario); err != nil {
			return err
		}
	}
	esperou := m.d.Relogio.Agora().Sub(ini)
	m.diag.Esperas++
	m.diag.EsperaS += esperou.Seconds()
	m.ex.Emite("retomada_usuario", map[string]any{"esperou_s": esperou.Seconds(), "disjuntor": pe.disjuntor})
	return nil
}

// recuaBrutas apaga as linhas de trabalho do passe depois de ateMs e acerta os
// contadores do passe (e o progresso em memória).
func recuaBrutas(db *banco.DB, passeID, ateMs int64, pr *progresso) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM amostras_brutas WHERE passe_id = ? AND t_ms > ?`, passeID, ateMs); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM amostras_time_brutas WHERE passe_id = ? AND t_ms > ?`, passeID, ateMs); err != nil {
		return err
	}
	var n, sem int
	if err := tx.QueryRow(`SELECT COUNT(*), COALESCE(SUM(trava_perdida), 0) FROM amostras_brutas WHERE passe_id = ?`, passeID).Scan(&n, &sem); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE passes SET amostras = ?, amostras_sem_trava = ?, t_atual_ms = ? WHERE id = ?`, n, sem, ateMs, passeID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	pr.SemTrava, pr.TAtualMs = sem, ateMs
	return nil
}

// DiagnosticoPasse é o "por que a trava caiu" de um passe (coluna
// passes.diagnostico, JSON): as teclas (quantas saíram, quantas vezes o jogo
// não estava na frente, de que programa era a janela da frente, quantas foram
// recusadas porque a pessoa usava o computador), o motivo de cada amostra
// viva sem a câmera (seleção vazia, seleção em outro jogador, fora da tela),
// as esperas pela pessoa e os disparos do disjuntor.
type DiagnosticoPasse struct {
	replay.DiagTeclas
	Conferencia map[string]int `json:"conferencia"`
	Esperas     int            `json:"esperas_usuario"`
	EsperaS     float64        `json:"espera_usuario_s"`
	Disjuntor   int            `json:"disjuntor"`
}

// Categorias do motivo da conferência (ConfereTrava) numa amostra sem trava.
const (
	ConfSelecaoVazia = "selecao_vazia" // a seleção do replay está vazia (câmera dirigida/livre)
	ConfSelecaoOutro = "selecao_outro" // a seleção está em outro jogador
	ConfForaDaTela   = "fora_da_tela"  // a seleção é ele, mas o centro dele não está na tela
	ConfOutro        = "outro"
)

func (d *DiagnosticoPasse) anotaConferencia(motivo string) {
	if d.Conferencia == nil {
		d.Conferencia = map[string]int{}
	}
	switch {
	case motivo == "fora da tela":
		d.Conferencia[ConfForaDaTela]++
	case motivo == `seleção ""`:
		d.Conferencia[ConfSelecaoVazia]++
	case strings.HasPrefix(motivo, "seleção"):
		d.Conferencia[ConfSelecaoOutro]++
	default:
		d.Conferencia[ConfOutro]++
	}
}

// soma junta o diagnóstico gravado (passe retomado) com o novo.
func (d DiagnosticoPasse) soma(o DiagnosticoPasse) DiagnosticoPasse {
	r := DiagnosticoPasse{DiagTeclas: d.DiagTeclas.Soma(o.DiagTeclas), Conferencia: map[string]int{},
		Esperas: d.Esperas + o.Esperas, EsperaS: d.EsperaS + o.EsperaS, Disjuntor: d.Disjuntor + o.Disjuntor}
	for k, v := range d.Conferencia {
		r.Conferencia[k] += v
	}
	for k, v := range o.Conferencia {
		r.Conferencia[k] += v
	}
	return r
}

// gravaDiagnostico soma o diagnóstico deste trecho ao gravado no passe.
func gravaDiagnostico(db *banco.DB, passeID int64, d DiagnosticoPasse, teclas replay.DiagTeclas) {
	d.DiagTeclas = teclas
	var velho string
	db.QueryRow(`SELECT diagnostico FROM passes WHERE id = ?`, passeID).Scan(&velho)
	if velho != "" {
		var v DiagnosticoPasse
		if json.Unmarshal([]byte(velho), &v) == nil {
			d = v.soma(d)
		}
	}
	if d.FrenteAntes == nil {
		d.FrenteAntes = map[string]int{}
	}
	if d.Conferencia == nil {
		d.Conferencia = map[string]int{}
	}
	if _, err := db.Exec(`UPDATE passes SET diagnostico = ? WHERE id = ?`, jsonTexto(d), passeID); err != nil {
		log.Printf("rastreio: gravar o diagnóstico do passe %d: %v", passeID, err)
	}
}
