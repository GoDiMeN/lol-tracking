//go:build real

package rastreio

// Sonda do motor com o jogo de verdade (cliente aberto e logado, partida
// 3304594274 baixada). Cada rastreio ABRE E FECHA o replay. Banco em
// dados/sonda-rastreio.db e capturas em dados/capturas (material da tarefa 3).
//
//	cd server && go test -tags real -run TestRealRastreio -count=1 -v -timeout 30m ./rastreio/
//
// TestRealVelocidades é a medida curta a 4× e 8×.

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"loltracking/server/banco"
	"loltracking/server/campeao"
	"loltracking/server/lcu"
	"loltracking/server/partida"
	"loltracking/server/replay"
	"loltracking/server/web"
)

const partidaReal = 3304594274

type sondaReal struct {
	db   *banco.DB
	fila *Fila
	hub  *web.Hub
	lt   LinhaDoTempo
}

func preparaReal(t *testing.T) *sondaReal {
	lock := os.Getenv("LOL_LOCKFILE")
	if lock == "" {
		lock = `C:\Riot Games\League of Legends\lockfile`
	}
	if _, err := os.Stat(lock); err != nil {
		t.Skip("cliente do LoL fechado")
	}
	// Um -timeout curto mata o processo sem rodar os defers: o replay ficaria aberto (regra 5).
	if prazo, ok := t.Deadline(); ok && time.Until(prazo) < 12*time.Minute {
		t.Skip("rode as sondas do rastreio com -timeout 30m (um timeout no meio deixaria o replay aberto)")
	}
	db, err := banco.Abre(filepath.Join("..", "dados", "sonda-rastreio.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	partida.Migra(db)
	Migra(db)
	cli := lcu.NovoReal(lock)
	ctx := context.Background()
	bruto, err := cli.Partida(ctx, partidaReal)
	if err != nil {
		t.Fatal(err)
	}
	if err := partida.Grava(ctx, partida.Deps{DB: db, LCU: cli, Campeoes: campeao.NovoDataDragon(filepath.Join("..", "dados"))}, bruto, true); err != nil {
		t.Fatal(err)
	}
	tl, err := cli.LinhaDoTempo(ctx, partidaReal)
	if err != nil {
		t.Fatal(err)
	}
	partida.GravaLinhaDoTempo(db, partidaReal, tl)
	lt, _ := LeLinhaDoTempo(tl)
	j := replay.NovaJanela()
	hub := web.NovoHub()
	cfg := Config{PastaCapturas: filepath.Join("..", "dados", "capturas"), CapturaACadaS: 20}
	f := NovaFila(Deps{DB: db, LCU: cli, Hub: hub, Config: cfg, Replay: replay.Deps{LCU: cli,
		API: replay.NovaAPI(replay.EnderecoPadrao), Jogo: j, Teclado: j, Tela: j, GameCfg: replay.GameCfgDe(lock)}})
	return &sondaReal{db: db, fila: f, hub: hub, lt: lt}
}

// roda um rastreio e devolve o passe e os eventos da execução.
func (s *sondaReal) roda(t *testing.T, ped Pedido) (Passe, []web.Evento) {
	inicio := time.Now()
	r, exID, err := s.fila.Cria(context.Background(), partidaReal, ped)
	if err != nil {
		t.Fatal(err)
	}
	s.fila.Espera()
	ex, _ := s.hub.Busca(exID)
	evs := ex.Eventos()
	r, _ = LeRastreio(s.db, r.ID)
	ps, _ := lePasses(s.db, `WHERE p.rastreio_id = ?`, r.ID)
	t.Logf("rastreio %d (%v, %.0fx, %s→%s): %s %q em %s", r.ID, ped.Jogadores, r.Velocidade, ms(ped.TInicioMs), ms(ped.TFimMs),
		r.Estado, r.Erro, time.Since(inicio).Round(time.Second))
	for _, e := range evs {
		switch e.Tipo {
		case "passe_fim", "trava", "erro", "aviso", "replay_aberto":
			t.Logf("  %s %s", e.Tipo, e.Dados)
		}
	}
	confereFechado(t)
	if len(ps) == 0 {
		t.Fatal("sem passe")
	}
	return ps[0], evs
}

func ms(v int64) string { return fmt.Sprintf("%d:%02d", v/60000, v/1000%60) }

// confereFechado: o jogo fechou e o cliente continua aberto.
func confereFechado(t *testing.T) {
	out, _ := exec.Command("tasklist", "/FO", "CSV", "/NH").Output()
	jogo, cliente := false, false
	for _, l := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(l, `"League of Legends.exe"`) {
			jogo = true
		}
		if strings.HasPrefix(l, `"LeagueClientUx.exe"`) {
			cliente = true
		}
	}
	t.Logf("  processos: League of Legends.exe=%v LeagueClientUx.exe=%v", jogo, cliente)
	if jogo {
		t.Errorf("o jogo ficou aberto")
	}
	if !cliente {
		t.Errorf("o cliente do LoL não está mais aberto")
	}
}

// relata imprime taxa, calibração (ajustada x padrão), tamanho e o que houve na morte.
func (s *sondaReal) relata(t *testing.T, p Passe) {
	pg, err := ListaAmostras(s.db, partidaReal, p.ParticipanteID, 0, math.MaxInt64/2, 0, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	as := pg.Amostras
	t.Logf("  passe %d: %d amostras, taxa %.2f/s de jogo, tentativas de trava %d, retravas %d, sem trava %d, %d bytes (%.2f B/amostra)",
		p.ID, p.Amostras, val(p.Taxa), p.TentativasTrava, p.Retravas, p.AmostrasSemTrava, p.Bytes, float64(p.Bytes)/float64(max(1, p.Amostras)))
	var cal map[string]any
	json.Unmarshal([]byte(p.Calibracao), &cal)
	t.Logf("  calibração %v: %d pontos, erro mediano %.0f, máximo %.0f", cal, p.CalibracaoPontos, val(p.ErroCalibracaoMediano), val(p.ErroCalibracaoMaximo))
	larg, _ := cal["largura"].(float64)
	alt, _ := cal["altura"].(float64)
	pts := PontosDoPasse(as, s.lt, p.ParticipanteID)
	if len(pts) > 0 {
		e := errosDe(CalibracaoPadrao(int(larg), int(alt)), pts)
		e0 := errosDe(Calibracao{X0: -2, Y0: 1291}, pts)
		t.Logf("  mesmos pontos com a fórmula da seção 3.2 sem ajuste: mediano %.0f máx %.0f; só deslocamento (−2,+1291): mediano %.0f máx %.0f",
			mediana(e), maximo(e), mediana(e0), maximo(e0))
		for _, pc := range pts {
			t.Logf("    quadro %s: tela (%.0f,%.0f) linha (%.0f,%.0f)", ms(pc.TMs), pc.SX, pc.SY, pc.TlX, pc.TlY)
		}
	}
	// Transições de origem/seleção (mostra a morte e o renascimento).
	ant := ""
	for _, a := range as {
		chave := fmt.Sprintf("%s morto=%v sel=%v perdida=%v", a.Origem, a.Morto, a.SelecaoOK, a.TravaPerdida)
		if chave != ant {
			pos := "-"
			if a.X != nil {
				pos = fmt.Sprintf("(%.0f,%.0f)", *a.X, *a.Y)
			}
			t.Logf("    %s %s %s respawn %.1f", ms(a.TMs), chave, pos, a.Respawn)
			ant = chave
		}
	}
	rc, _ := ListaCadeia(s.db, partidaReal, p.ParticipanteID, nil)
	tipos := map[string]int{}
	for _, it := range rc.Itens {
		tipos[it.Tipo]++
		if it.Tipo != "ponto" && it.Tipo != "minuto" {
			b, _ := json.Marshal(it.Dados)
			t.Logf("    cadeia %s %s %s", ms(it.TMs), it.Tipo, b)
		}
	}
	t.Logf("  cadeia: %v", tipos)
}

func val(p *float64) float64 {
	if p == nil {
		return math.NaN()
	}
	return *p
}

func TestRealRastreio(t *testing.T) {
	s := preparaReal(t)
	// GodiNzt (Akshan, participante 3), min 6 a 10 a 2×: morre em 8:48.
	p, _ := s.roda(t, Pedido{Jogadores: []int{3}, Velocidade: 2, TInicioMs: 6 * 60000, TFimMs: 10 * 60000})
	s.relata(t, p)
	// FabioBR (Lux, participante 10, tecla T), 1 min a 2×.
	p, _ = s.roda(t, Pedido{Jogadores: []int{10}, Velocidade: 2, TInicioMs: 12 * 60000, TFimMs: 13 * 60000})
	s.relata(t, p)
}

// TestRealVelocidades: ~30 s reais a 4× e a 8×, só para medir a taxa e o erro
// (o padrão continua 2×).
func TestRealVelocidades(t *testing.T) {
	s := preparaReal(t)
	if os.Getenv("LOL_SONDA_SO_8X") != "1" {
		p, _ := s.roda(t, Pedido{Jogadores: []int{3}, Velocidade: 4, TInicioMs: 14 * 60000, TFimMs: 16 * 60000})
		s.relata(t, p)
	}
	p, _ := s.roda(t, Pedido{Jogadores: []int{3}, Velocidade: 8, TInicioMs: 16 * 60000, TFimMs: 20 * 60000})
	s.relata(t, p)
}

// TestRealCalibracao: 10 min de jogo a 4× (~2,5 min reais) para ter quadros
// suficientes para o ajuste completo da posição.
func TestRealCalibracao(t *testing.T) {
	s := preparaReal(t)
	p, _ := s.roda(t, Pedido{Jogadores: []int{3}, Velocidade: 4, TInicioMs: 20 * 60000, TFimMs: 30*60000 + 2000})
	s.relata(t, p)
}
