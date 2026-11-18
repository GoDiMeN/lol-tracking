//go:build real

package aprendizado

// Sonda de verdade, SÓ LEITURA: a descoberta de um ou dois elos com a API da
// Riot (token.txt) e o cliente do LoL aberto. Não cria rastreio, não consulta
// nem baixa replay, não abre o jogo: só league-v4, account-v1, e no LCU o
// alias, o histórico e games/{id}. O banco é uma CÓPIA (LOL_DADOS).
//
//   LOL_DADOS=<pasta com a cópia do lol.db> LOL_ELOS_SONDA=CHALLENGER,GOLD \
//     go test -tags real -run TestRealDescoberta -count=1 -v ./aprendizado/

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"loltracking/server/banco"
	"loltracking/server/campeao"
	"loltracking/server/lcu"
	"loltracking/server/partida"
	"loltracking/server/riot"
	"loltracking/server/web"
)

func TestRealDescoberta(t *testing.T) {
	dados := os.Getenv("LOL_DADOS")
	if dados == "" || strings.Contains(filepath.Clean(dados), filepath.Join("server", "dados")) {
		t.Skip("LOL_DADOS precisa apontar para uma CÓPIA do banco")
	}
	elos := strings.Split(os.Getenv("LOL_ELOS_SONDA"), ",")
	if elos[0] == "" {
		elos = []string{"CHALLENGER", "GOLD"}
	}
	db, err := banco.Abre(filepath.Join(dados, "lol.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, m := range []func(*banco.DB) error{partida.Migra, Migra} {
		if err := m(db); err != nil {
			t.Fatal(err)
		}
	}
	token := "../../token.txt"
	cli := lcu.NovoReal(`C:\Riot Games\League of Legends\lockfile`)
	r := riot.Novo(token, false)
	ctx, cancela := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancela()
	if err := r.Confere(ctx); err != nil {
		t.Fatalf("chave: %v", err)
	}
	reg, _ := riot.RegiaoPorCodigo("BR1")

	// Formato cru de uma entrada (os campos que a Riot manda de verdade).
	chave, _ := os.ReadFile(token)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, reg.Plataforma+"/lol/league/v4/entries/RANKED_SOLO_5x5/GOLD/II?page=1", nil)
	req.Header.Set("X-Riot-Token", strings.TrimSpace(string(chave)))
	if resp, err := http.DefaultClient.Do(req); err == nil {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var l []map[string]any
		json.Unmarshal(b, &l)
		if len(l) > 0 {
			chaves := []string{}
			for k := range l[0] {
				chaves = append(chaves, k)
			}
			t.Logf("entrada crua (GOLD II, %d na página, status %d): campos %v", len(l), resp.StatusCode, chaves)
		}
	}

	hub := web.NovoHub()
	k := &Coletor{d: Deps{DB: db, LCU: cli, Riot: r, Hub: hub,
		Partida: partida.Deps{DB: db, LCU: cli, Campeoes: campeao.NovoDataDragon(dados)}}, execucoes: map[int64]string{}}
	if err := k.confereRegiao(ctx, reg); err != nil {
		t.Fatalf("região: %v", err)
	}
	// Uma descoberta interrompida na cópia continua de onde parou (poupa a cota da chave).
	var id int64
	db.QueryRow(`SELECT id FROM aprendizado_coletas WHERE fase = 'descoberta' ORDER BY id DESC LIMIT 1`).Scan(&id)
	if id == 0 {
		if id, err = criaColeta(db, novaColeta{regiao: "BR1", elos: elos, ppj: 1, quem: QuemJogador, vel: 4}); err != nil {
			t.Fatal(err)
		}
	} else {
		t.Logf("continuando a descoberta da coleta %d", id)
	}
	col, _ := LeColeta(db, id)
	c := &corridaColeta{coletaID: id, ex: hub.Nova(), cancela: cancela, feito: make(chan struct{})}
	inicio := time.Now()
	if err := k.descobre(ctx, c, col, reg); err != nil {
		t.Errorf("descoberta: %v", err)
	}
	t.Logf("descoberta em %v", time.Since(inicio).Round(time.Second))
	d, err := k.Detalhe(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range d.Elos {
		t.Logf("%s: %d jogadores, %d candidatos olhados, descartes %v, aviso %q, %d partidas", e.Nome, e.Jogadores,
			e.CandidatosVistos, e.Descartes, e.Aviso, e.PartidasTotal)
	}
	for _, j := range d.Jogadores {
		// Sem nome do jogador no log: o elo, a posição e os números bastam.
		t.Logf("  %s %s #%d: %d PDL, %s; %s %.0f%% de %d (mono %v), %s", j.Elo, j.Divisao, j.Posicao, j.LP, j.Motivo,
			j.Campeao, j.FracaoCampeao*100, j.PartidasSolo, j.Mono, j.Estado)
	}
	for _, p := range d.Partidas {
		t.Logf("  partida %d (%s): %s %d/%d/%d %ds nota %.1f", p.GameID, p.Elo, p.Campeao, p.Abates, p.Mortes, p.Assistencias, p.DuracaoS, p.Pontuacao)
	}
	tipos := map[string]int{}
	for _, ev := range c.ex.Eventos() {
		if ev.Tipo == "candidato" {
			var x map[string]any
			json.Unmarshal(ev.Dados, &x)
			tipos[x["resultado"].(string)]++
		}
	}
	t.Logf("candidatos: %v", tipos)
}
