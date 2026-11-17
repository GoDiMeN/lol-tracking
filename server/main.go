// O servidor Go do LoL Tracking. Este arquivo é só o índice: lê a
// configuração, abre o banco, escolhe as implementações de verdade ou falsas
// (LOL_TESTE), manda cada pacote migrar o que é seu, deixa cada um registrar
// as próprias rotas e escuta. Pacote novo = Migra na lista e Registra abaixo.
package main

import (
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"loltracking/server/analise"
	"loltracking/server/banco"
	"loltracking/server/campeao"
	"loltracking/server/estado"
	"loltracking/server/estudo"
	"loltracking/server/jogador"
	"loltracking/server/lcu"
	"loltracking/server/partida"
	"loltracking/server/rastreio"
	"loltracking/server/replay"
	"loltracking/server/riot"
	"loltracking/server/visao"
	"loltracking/server/web"
)

func main() {
	cfg := LoadConfig()

	// O modo teste tem banco próprio: os dados do LCU falso não se misturam com
	// as partidas de verdade.
	arquivoBanco := "lol.db"
	if cfg.ModoTeste {
		arquivoBanco = "lol-teste.db"
	}
	db, err := banco.Abre(filepath.Join(cfg.Dados, arquivoBanco))
	if err != nil {
		// log.Fatalf só aqui: é o único lugar onde "não dá para continuar" é verdade.
		log.Fatalf("banco de dados: %v", err)
	}
	defer db.Close()

	// Tudo o que fala com o mundo de fora, escolhido uma vez.
	var cliLCU lcu.Cliente
	var nomes campeao.Nomes
	// dd nil no modo teste: as imagens respondem 404 e nada sai para a rede.
	var dd *campeao.DataDragon
	if cfg.ModoTeste {
		cliLCU = lcu.NovoFalso()
		nomes = campeao.Fixo(lcu.CampeoesFalsos)
	} else {
		cliLCU = lcu.NovoReal(cfg.Lockfile)
		dd = campeao.NovoDataDragon(cfg.Dados)
		nomes = dd
	}
	chaveRiot := riot.Novo(cfg.TokenRiot, cfg.ModoTeste)

	// O jogo (replay): de verdade, ou o falso que "abre" quando o LCU falso
	// recebe o watch, com os participantes da partida pedida.
	var depsReplay replay.Deps
	if cfg.ModoTeste {
		jogo := replay.NovoFalso(replay.RelogioReal{})
		cliLCU.(*lcu.Falso).AoAssistir = func(gameID int64) {
			if p, err := partida.Le(db, gameID); err == nil {
				var elenco []replay.JogadorFalso
				for _, pt := range p.Participantes {
					t := "ORDER"
					if pt.Time == 200 {
						t = "CHAOS"
					}
					elenco = append(elenco, replay.JogadorFalso{Nome: pt.Nome, Campeao: pt.Campeao, Time: t})
				}
				jogo.ConfiguraPartida(elenco, float64(p.DuracaoS))
			}
			jogo.AbreReplay()
		}
		depsReplay = replay.Deps{LCU: cliLCU, API: jogo, Jogo: jogo, Teclado: jogo, Tela: jogo}
	} else {
		janela := replay.NovaJanela()
		depsReplay = replay.Deps{LCU: cliLCU, API: replay.NovaAPI(replay.EnderecoPadrao), Jogo: janela, Teclado: janela,
			Tela: janela, GameCfg: replay.GameCfgDe(cfg.Lockfile)}
	}
	cfgRastreio := rastreio.Config{CapturaACadaS: cfg.CapturaACadaS}
	if cfg.ModoTeste {
		// Tela inventada a partir do tempo do jogo falso: a interface mostra a
		// "última leitura" e o mapa com play sem o jogo de verdade.
		cfgRastreio.Leitor = rastreio.LeitorFalso{}
	} else {
		// Leitura da tela por gabarito (tarefa 3). No modo teste a tela é falsa:
		// fica o LeitorNulo e as colunas da tela saem nulas.
		leitor, err := visao.Novo()
		if err != nil {
			log.Fatalf("gabaritos da leitura da tela: %v", err)
		}
		cfgRastreio.Leitor = leitor
		cfgRastreio.Riot = chaveRiot
	}
	if cfg.GuardaCapturas {
		cfgRastreio.PastaCapturas = filepath.Join(cfg.Dados, "capturas")
	}

	// Ordem de migração = ordem de dependência lógica (não há FOREIGN KEY entre
	// pacotes).
	migracoes := []struct {
		nome  string
		migra func(*banco.DB) error
	}{
		{"jogador", jogador.Migra},
		{"partida", partida.Migra},
		{"rastreio", rastreio.Migra},
		{"analise", analise.Migra},
		{"estudo", estudo.Migra},
	}
	for _, m := range migracoes {
		if err := m.migra(db); err != nil {
			log.Fatalf("módulo %s: %v", m.nome, err)
		}
	}

	hub := web.NovoHub()
	mux := http.NewServeMux()

	// /health não toca no banco nem no LCU: é o "o processo está de pé?" que o
	// Electron pergunta antes de abrir a janela.
	modoTeste := cfg.ModoTeste
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		web.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "modo_teste": modoTeste})
	})

	web.RegistraExecucoes(mux, hub)
	estado.Registra(mux, cliLCU, chaveRiot, cfg.ModoTeste)
	estado.RegistraDados(mux, cfg.Dados, arquivoBanco)
	jogador.Registra(mux, db, cliLCU)
	partida.Registra(mux, partida.Deps{DB: db, LCU: cliLCU, Campeoes: nomes}, hub)
	// A análise refaz a partida quando um passe fecha (em segundo plano).
	depsAnalise := analise.Deps{DB: db, Riot: chaveRiot}
	rastreio.Registra(mux, rastreio.NovaFila(rastreio.Deps{DB: db, LCU: cliLCU, Replay: depsReplay, Hub: hub, Config: cfgRastreio,
		AoFecharPasse: analise.RecalculaEmSegundoPlano(depsAnalise)}))
	analise.Registra(mux, depsAnalise)
	estudo.Registra(mux, depsAnalise)
	campeao.RegistraImagens(mux, campeao.NovasImagens(dd, cfg.Dados))

	if cfg.Renderer != "" {
		if _, err := os.Stat(cfg.Renderer); errors.Is(err, os.ErrNotExist) {
			log.Printf("AVISO: LOL_RENDERER=%s não existe; só a API vai responder", cfg.Renderer)
		}
	}
	mux.Handle("/", web.Static(cfg.Renderer))

	if cfg.ModoTeste {
		log.Printf("MODO TESTE: LCU falso, nada toca no cliente do LoL nem na rede")
	}

	// Só 127.0.0.1: o servidor fala com o cliente do LoL da pessoa e não pode
	// ficar exposto a quem estiver na mesma rede.
	endereco := "127.0.0.1:" + cfg.Porta
	srv := &http.Server{
		Addr:    endereco,
		Handler: mux,
		// Só o cabeçalho tem prazo: WriteTimeout cortaria os SSE.
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("LoL Tracking ouvindo em http://%s (dados em %s)", endereco, cfg.Dados)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("subir o servidor em %s: %v", endereco, err)
	}
}
