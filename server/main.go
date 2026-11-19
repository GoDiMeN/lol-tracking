// O servidor Go do LoL Tracking. Este arquivo é só o índice: lê a
// configuração, abre o banco, escolhe as implementações de verdade ou falsas
// (LOL_TESTE), manda cada pacote migrar o que é seu, deixa cada um registrar
// as próprias rotas e escuta. Pacote novo = Migra na lista e Registra abaixo.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"loltracking/server/analise"
	"loltracking/server/aprendizado"
	"loltracking/server/atributos"
	"loltracking/server/banco"
	"loltracking/server/campeao"
	"loltracking/server/captura"
	"loltracking/server/contexto"
	"loltracking/server/estado"
	"loltracking/server/estudo"
	"loltracking/server/ia"
	"loltracking/server/jogador"
	"loltracking/server/lcu"
	"loltracking/server/partida"
	"loltracking/server/ranking"
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
	// Dados estáticos da versão da partida (base dos campeões, itens): no modo
	// teste, uma tabela fixa pequena.
	var estaticos campeao.FonteEstaticos
	if cfg.ModoTeste {
		cliLCU = lcu.NovoFalso()
		nomes = campeao.Fixo(lcu.CampeoesFalsos)
		estaticos = campeao.EstaticosDoTeste()
	} else {
		cliLCU = lcu.NovoReal(cfg.Lockfile)
		dd = campeao.NovoDataDragon(cfg.Dados)
		nomes = dd
		estaticos = campeao.NovosEstaticos(cfg.Dados)
	}
	chaveRiot := riot.Novo(cfg.TokenRiot, cfg.ModoTeste)

	// O jogo (replay): de verdade, ou o falso que "abre" quando o LCU falso
	// recebe o watch, com os participantes da partida pedida.
	var depsReplay replay.Deps
	if cfg.ModoTeste {
		jogo := replay.NovoFalso(replay.RelogioReal{})
		// Cada jogador perde a trava de 3:20 a 3:35 nas 3 primeiras passagens
		// (o passe e as 2 tentativas da repescagem ao fim dele): o rastreio
		// termina com 1 trecho sem amostra, que a repescagem de passe gravado
		// (botão da tela da partida) recupera. É o caso da Leona da 3304217746.
		jogo.PerdasTrava = []replay.PerdaTravaFalsa{{Indice: -1, De: 200, Ate: 215, Vezes: 3}}
		// Sentinelas do minimapa falso (passe de visão de time): duas de cada
		// time, no rio e na selva, com prazo como as amarelas.
		jogo.Sentinelas = []replay.SentinelaFalsa{{Time: 100, X: 5200, Y: 9200, De: 70, Ate: 190}, {Time: 100, X: 9600, Y: 5500, De: 300, Ate: 420},
			{Time: 200, X: 9700, Y: 4800, De: 120, Ate: 240}, {Time: 200, X: 4300, Y: 10200, De: 380, Ate: 500}}
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
		depsReplay = replay.Deps{LCU: cliLCU, API: jogo, Jogo: jogo, Teclado: jogo, Tela: jogo, Presenca: jogo}
	} else {
		janela := replay.NovaJanela()
		// Presenca: a pessoa usando o computador (GetLastInputInfo, sem ler
		// tecla nenhuma): o motor não rouba o foco nem manda tecla nessa hora.
		depsReplay = replay.Deps{LCU: cliLCU, API: replay.NovaAPI(replay.EnderecoPadrao), Jogo: janela, Teclado: janela,
			Tela: janela, GameCfg: replay.GameCfgDe(cfg.Lockfile), Presenca: janela}
	}
	cfgRastreio := rastreio.Config{CapturaACadaS: cfg.CapturaACadaS}
	leitor, err := visao.Novo()
	if err != nil {
		log.Fatalf("gabaritos da leitura da tela: %v", err)
	}
	// A leitura do minimapa dos passes de visão de time é a de verdade nos dois
	// modos: o jogo falso desenha o minimapa na geometria e nas cores medidas.
	cfgRastreio.Minimapa = leitor
	if cfg.ModoTeste {
		// Tela inventada a partir do tempo do jogo falso: a interface mostra a
		// "última leitura" e o mapa com play sem o jogo de verdade.
		cfgRastreio.Leitor = rastreio.LeitorFalso{}
	} else {
		// Leitura da tela por gabarito (tarefa 3).
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
		{"ranking", ranking.Migra},
		{"aprendizado", aprendizado.Migra},
		{"ia", ia.Migra},
	}
	for _, m := range migracoes {
		if err := m.migra(db); err != nil {
			log.Fatalf("módulo %s: %v", m.nome, err)
		}
	}
	// A origem das partidas rastreadas antes de ela existir (rastreio/origem.go):
	// depois de TODAS as migrações, porque lê as tabelas do estudo e da coleta.
	// O invocador da conta do cliente desempata quem foi o pesquisado; com o
	// cliente fechado, vale o buscado por último antes do rastreio.
	{
		conta := ""
		ctx, cancela := context.WithTimeout(context.Background(), 3*time.Second)
		if inv, err := cliLCU.InvocadorAtual(ctx); err == nil {
			conta = inv.Puuid
		}
		cancela()
		feitas, err := rastreio.PreencheOrigens(db, conta)
		if err != nil {
			log.Printf("AVISO: preencher a origem das partidas rastreadas: %v", err)
		}
		for _, f := range feitas {
			log.Printf("origem da partida %d: %s (%s) %s", f.GameID, f.Origem, f.Motivo, f.Puuid)
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
	estado.Registra(mux, cliLCU, chaveRiot, cfg.ModoTeste, cfg.Estudo)
	estado.RegistraDados(mux, cfg.Dados, arquivoBanco)
	jogador.Registra(mux, db, cliLCU)
	partida.Registra(mux, partida.Deps{DB: db, LCU: cliLCU, Campeoes: nomes}, hub)
	// A análise refaz a partida quando um passe fecha (em segundo plano). Os
	// estáticos dão nome e custo aos itens na base de fatos.
	depsAnalise := analise.Deps{DB: db, Riot: chaveRiot, Estaticos: estaticos, SemEstudo: !cfg.Estudo}
	fila := rastreio.NovaFila(rastreio.Deps{DB: db, LCU: cliLCU, Replay: depsReplay, Hub: hub, Config: cfgRastreio,
		AoFecharPasse: analise.RecalculaEmSegundoPlano(depsAnalise), SemEstudo: !cfg.Estudo})
	rastreio.Registra(mux, fila)
	analise.Registra(mux, depsAnalise)
	// Passe ruim não vale (rastreio/qualidade_passe.go): os já gravados que caem
	// no critério viram "ruim" na subida, e a análise das partidas deles é
	// refeita sem eles (em segundo plano).
	if feitos, err := rastreio.ReclassificaPassesRuins(db, cfgRastreio.ComPadroes().AmostrasPorSegundo); err != nil {
		log.Printf("AVISO: reclassificar os passes ruins: %v", err)
	} else {
		rastreio.LogReclassificados(feitos)
		refeitas := map[int64]bool{}
		recalcula := analise.RecalculaEmSegundoPlano(depsAnalise)
		for _, p := range feitos {
			if !refeitas[p.GameID] {
				refeitas[p.GameID] = true
				recalcula(p.GameID)
			}
		}
	}
	// A base de fatos das partidas analisadas antes dela (ou de regra antiga):
	// em segundo plano, lendo os arquivos de cada partida uma vez.
	analise.PreencheFatosEmSegundoPlano(depsAnalise)
	// O Estudo (estudos, coleta por elo, ranking, Especialista Lane) só existe
	// com LOL_ESTUDO=1 (padrão). Desligado, as rotas dele não são penduradas:
	// caem no 404 de /api/ (web.Static). Lista em contrato-http.md, "Interruptor do Estudo".
	if cfg.Estudo {
		estudo.Registra(mux, depsAnalise)
	}
	// Aprendizado por elo: o ranking vem da API da Riot (no modo teste, a
	// falsa casada com os jogadores de referência do LCU falso).
	var fonteRiot aprendizado.FonteRiot = chaveRiot
	var riotRanking ranking.FonteRiotAPI = chaveRiot
	if cfg.ModoTeste {
		falsa := aprendizado.NovoRiotFalso()
		fonteRiot, riotRanking = falsa, falsa
	}
	// O ranking do Lane (Estudo → Ranking): a liga pela Riot e o histórico de
	// cada candidato pelo cliente do LoL ou pela match-v5 (LOL_RANKING_FONTE).
	atualizador := ranking.Novo(ranking.Deps{DB: db, Riot: riotRanking, LCU: cliLCU, Hub: hub, FontePadrao: cfg.RankingFonte,
		Campeoes: nomes, PatchAtual: func(ctx context.Context) (string, error) {
			c, err := cliLCU.ConfiguracaoReplays(ctx)
			return lcu.PatchDe(c.VersaoJogo), err
		}})
	coletor := aprendizado.Novo(aprendizado.Deps{DB: db, LCU: cliLCU, Riot: fonteRiot, Fila: fila,
		Partida: partida.Deps{DB: db, LCU: cliLCU, Campeoes: nomes}, Hub: hub, Analise: depsAnalise})
	// Pausar/retomar a captura em andamento, seja qual for (o atalho global,
	// o botão da barra de título e a bandeja do Electron).
	captura.Registra(mux, &captura.Servico{Fila: fila, Coletas: coletor})
	if cfg.Estudo {
		ranking.Registra(mux, atualizador)
		aprendizado.Registra(mux, coletor)
	} else {
		log.Printf("ESTUDO DESLIGADO (LOL_ESTUDO=0): estudos, coleta por elo, ranking e Especialista Lane respondem 404")
	}
	atributos.Registra(mux, atributos.Deps{DB: db, Estaticos: estaticos})
	// A IA: só nas telas de análise e conversa, a pedido da pessoa. Lê só o
	// texto que o app monta da base de fatos; nenhuma ferramenta.
	var cliIA ia.Cliente
	if cfg.IA == "falso" {
		cliIA = &ia.Falso{Atraso: 20 * time.Millisecond}
	} else {
		cliIA = ia.NovoTerminal(cfg.IAClaude, cfg.IAModelo, cfg.IAModeloConversa)
	}
	svcIA := ia.NovoServico(db, cliIA, ia.FontesBanco{D: depsAnalise}, hub, cfg.Conhecimento)
	// O elo do jogador em foco (voz dos melhores do elo) pela API da Riot,
	// quando há chave; no modo teste o cliente nunca sai para a rede.
	svcIA.Riot = chaveRiot
	// O espelho do Especialista Lane no NotebookLM (a pedido, pela ferramenta
	// nlm da máquina); no modo teste, um caderno falso que não sai da máquina.
	if cfg.ModoTeste {
		svcIA.Caderno = &ia.CadernoFalso{}
	} else {
		// Detecta em segundo plano qual `nlm` funciona (o do PATH/LOL_NLM ou o
		// uvx): a subida não espera o uvx baixar.
		nlm := ia.NovoNLM(cfg.NLM)
		nlm.Inicia()
		svcIA.Caderno = nlm
	}
	svcIA.CadernoPadrao = cfg.NotebookLMCaderno
	svcIA.SemEstudo = !cfg.Estudo
	// O contexto dos 10 jogadores de uma partida (o histórico de antes dela),
	// a pedido: pelo cliente do LoL ou pela match-v5 (no modo teste, a Riot
	// falsa), e antes da análise com IA quando falta.
	var riotContexto contexto.FonteRiotAPI = chaveRiot
	if cfg.ModoTeste {
		riotContexto = aprendizado.NovoRiotFalso()
	}
	svcContexto := contexto.Novo(contexto.Deps{DB: db, LCU: cliLCU, Riot: riotContexto, Hub: hub, Analise: depsAnalise})
	contexto.Registra(mux, svcContexto)
	svcIA.ContextoAntes = svcContexto.GaranteParaIA
	ia.Registra(mux, svcIA)
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
	log.Printf("Lane ouvindo em http://%s (dados em %s)", endereco, cfg.Dados)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("subir o servidor em %s: %v", endereco, err)
	}
}
