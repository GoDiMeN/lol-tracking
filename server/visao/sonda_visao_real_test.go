//go:build real

package visao

// Sonda dos passes de visão de time com o jogo de verdade. ABRE E FECHA o
// replay uma vez (os dois passes na mesma abertura). Precisa do cliente do LoL
// aberto, da partida com o replay baixado (estado "watch") e de uma CÓPIA da
// pasta de dados em que a partida já tem os jogadores rastreados (a decisão de
// visibilidade usa as posições dos passes): nada é escrito na pasta de
// verdade.
//
//	$env:LOL_DADOS = "C:\caminho\da\copia"   # com lol.db* e a pasta partidas
//	cd server
//	go test -tags real -run TestRealVisao -count=1 -v -timeout 30m ./visao/
//
// Opcionais: LOL_PARTIDA (padrão 3304217746), LOL_DE e LOL_ATE (segundos de
// jogo; padrão 540 e 660, os 2 min em volta das capturas de 10:03),
// LOL_VELOCIDADE (padrão 8), LOL_CAPTURA_A_CADA (s de jogo entre capturas
// guardadas; padrão 2).
//
// Deixa em <LOL_DADOS>/capturas/<partida>/:
//   - visao100_t<ms>.png / visao200_t<ms>.png: o recorte do passe (painel
//     "Fog of War" + minimapa, y 1070–1440 da tela), e o .json ao lado com o
//     que a leitura achou (modo, retratos, sentinelas);
//   - visao<time>_t<ms>_posicoes.json: as posições rastreadas dos 10 no mesmo
//     instante e a decisão (visível / fora / indeterminado, com o motivo);
//   - o visao.json da partida fica em <LOL_DADOS>/partidas/<partida>/visao.json.zst.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"loltracking/server/banco"
	"loltracking/server/lcu"
	"loltracking/server/rastreio"
	"loltracking/server/replay"
	"loltracking/server/web"
)

func envInt(nome string, padrao int64) int64 {
	if v, err := strconv.ParseInt(os.Getenv(nome), 10, 64); err == nil {
		return v
	}
	return padrao
}

func TestRealVisao(t *testing.T) {
	lock := os.Getenv("LOL_LOCKFILE")
	if lock == "" {
		lock = `C:\Riot Games\League of Legends\lockfile`
	}
	if _, err := os.Stat(lock); err != nil {
		t.Skip("cliente do LoL fechado")
	}
	dados := os.Getenv("LOL_DADOS")
	if dados == "" {
		t.Skip("LOL_DADOS precisa apontar para uma CÓPIA da pasta de dados (com a partida rastreada)")
	}
	if abs, _ := filepath.Abs(dados); strings.EqualFold(abs, mustAbs(filepath.Join("..", "dados"))) {
		t.Fatal("LOL_DADOS aponta para a pasta de dados de verdade; use uma cópia")
	}
	if prazo, ok := t.Deadline(); ok && time.Until(prazo) < 10*time.Minute {
		t.Skip("rode com -timeout 30m (um timeout no meio deixaria o replay aberto)")
	}
	gameID := envInt("LOL_PARTIDA", 3304217746)
	de, ate := envInt("LOL_DE", 540)*1000, envInt("LOL_ATE", 660)*1000
	vel := float64(envInt("LOL_VELOCIDADE", 8))
	cada := float64(envInt("LOL_CAPTURA_A_CADA", 2))

	db, err := banco.Abre(filepath.Join(dados, "lol.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := rastreio.Migra(db); err != nil {
		t.Fatal(err)
	}
	leitor, err := Novo()
	if err != nil {
		t.Fatal(err)
	}
	cli := lcu.NovoReal(lock)
	j := replay.NovaJanela()
	hub := web.NovoHub()
	pasta := filepath.Join(dados, "capturas")
	f := rastreio.NovaFila(rastreio.Deps{DB: db, LCU: cli, Hub: hub,
		Config: rastreio.Config{Leitor: leitor, Minimapa: leitor, PastaCapturas: pasta, CapturaACadaS: cada},
		Replay: replay.Deps{LCU: cli, API: replay.NovaAPI(replay.EnderecoPadrao), Jogo: j, Teclado: j, Tela: j, GameCfg: replay.GameCfgDe(lock)}})

	inicio := time.Now()
	exID, pvs, err := f.RastreiaVisao(context.Background(), gameID, rastreio.PedidoVisao{Velocidade: vel, TInicioMs: de, TFimMs: ate})
	if err != nil {
		t.Fatal(err)
	}
	f.Espera()
	t.Logf("passes de visão da partida %d, %s → %s a %.0f×: %s", gameID, ms(de), ms(ate), vel, time.Since(inicio).Round(time.Second))
	if ex, ok := hub.Busca(exID); ok {
		for _, e := range ex.Eventos() {
			switch e.Tipo {
			case "visao_inicio", "nevoa", "visao_fim", "aviso", "aviso_tela", "erro", "cancelado":
				t.Logf("  %s %s", e.Tipo, e.Dados)
			}
		}
	}
	confereFechado(t)
	for _, p := range pvs {
		p, _ = rastreio.LePasseVisao(db, p.ID)
		taxa := 0.0
		if p.Taxa != nil {
			taxa = *p.Taxa
		}
		t.Logf("  passe de visão %d (time %d): %s %q, %d capturas (%.2f por s de jogo), painel conferido em %d, %d teclas",
			p.ID, p.Time, p.Estado, p.Erro, p.Capturas, taxa, p.CapturasModoOK, p.Teclas)
	}
	v, ok, err := rastreio.LeVisao(db, gameID)
	if err != nil || !ok {
		t.Fatalf("visao.json: %v %v", ok, err)
	}
	for _, vt := range v.Times {
		ali := -1.0
		if vt.AliadosAchados != nil {
			ali = *vt.AliadosAchados
		}
		t.Logf("visão %d: %d capturas, %.2f/s, painel %.1f%%, aliados achados %.1f%%", vt.Time, vt.Capturas, vt.CapturasPorS,
			vt.ModoConferido*100, ali*100)
		for _, jj := range vt.Jogadores {
			pct := "—"
			if jj.PctVisivel != nil {
				pct = strconv.FormatFloat(*jj.PctVisivel, 'f', 1, 64) + "%"
			}
			t.Logf("  participante %d: à vista %d trechos, fora %d, indeterminado %d (%v); %% à vista %s", jj.ParticipanteID,
				len(jj.Visivel), len(jj.Fora), len(jj.Indeterminado), jj.Motivos, pct)
		}
		for _, s := range vt.Sentinelas {
			t.Logf("  sentinela (%.0f, %.0f) de %s a %s, %d capturas", s.X, s.Y, ms(s.DeMs), ms(s.AteMs), s.Capturas)
		}
	}
	// As posições dos 10 e a decisão em cada captura guardada.
	posicoes, err := rastreio.PosicoesDaPartida(db, gameID)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond) // a última captura é gravada em segundo plano
	dir := filepath.Join(pasta, strconv.FormatInt(gameID, 10))
	arqs, _ := filepath.Glob(filepath.Join(dir, "visao*_t*.json"))
	n := 0
	for _, arq := range arqs {
		if strings.HasSuffix(arq, "_posicoes.json") {
			continue
		}
		b, err := os.ReadFile(arq)
		if err != nil {
			continue
		}
		var info struct {
			Time       int                      `json:"time"`
			TMs        int64                    `json:"t_ms"`
			Modo       string                   `json:"modo"`
			Retratos   []rastreio.MarcaMinimapa `json:"retratos"`
			Sentinelas []rastreio.MarcaMinimapa `json:"sentinelas"`
		}
		if json.Unmarshal(b, &info) != nil {
			continue
		}
		js := posicoes(info.TMs)
		r := rastreio.ClassificaCaptura(rastreio.LeituraMinimapa{Modo: info.Modo, Retratos: info.Retratos}, info.Modo == rastreio.ModoDoTime(info.Time), info.Time, js)
		saida := map[string]any{"t_ms": info.TMs, "time_observador": info.Time, "jogadores": js, "estados": r.Estados, "motivos": r.Motivos,
			"aliados_vivos": r.AliadosVivos, "aliados_achados": r.AliadosAchados, "aneis_sem_dono": r.Sobras}
		ob, _ := json.MarshalIndent(saida, "", "  ")
		os.WriteFile(strings.TrimSuffix(arq, ".json")+"_posicoes.json", ob, 0o644)
		n++
	}
	t.Logf("%d capturas guardadas em %s (cada uma com o _posicoes.json)", n, dir)
}

func mustAbs(p string) string {
	a, _ := filepath.Abs(p)
	return a
}
