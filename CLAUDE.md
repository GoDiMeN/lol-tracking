# CLAUDE.md — LoL Tracking

App desktop que rastreia replays do League of Legends. A pessoa digita o nome de um jogador, escolhe uma partida, e o app
baixa o replay, abre o jogo e assiste sozinho, um passe por jogador (10 passes), gravando posição, vida, mana, cooldowns,
itens e eventos num SQLite local. Em cima disso há o **estudo**: listas manuais de partidas de um jogador (vitórias e
derrotas) comparadas para entender o que ele faz quando ganha e quando perde. O rastreio **não usa IA**: só as APIs locais
do jogo e leitura da tela por gabarito.

`lol-tracking.html` é o protótipo/plano publicado (artefato). `docs/arquitetura/descobertas-replay.md` tem tudo o que foi
medido no jogo de verdade: **leia antes de tocar em `lcu`, `replay` ou `visao`**.

## Regras de segurança da conta (invioláveis)

1. **Só replay.** Nada neste projeto (código, teste, sonda, agente) acessa outra parte do LoL. No LCU só são permitidas as
   rotas da lista em `server/lcu/permitidas.go`: leitura de invocador, histórico/linha do tempo de partida e `lol-replays`.
   É proibido tocar em lobby, fila, seleção de campeão, chat, loja, saque, configurações da conta, ou qualquer `POST/PUT/PATCH/DELETE`
   fora de `lol-replays`. O cliente HTTP do LCU recusa rota fora da lista **antes** de mandar o pedido, e isso tem teste.
2. **Nunca com partida ao vivo.** Se `GET /lol-gameflow/v1/gameflow-phase` não for `None`/`WatchInProgress`, o rastreio não começa e não manda tecla.
3. **Tecla só no replay.** Entrada de teclado só vai para a janela `League of Legends (TM) Client` e só depois de a Replay API
   (`/replay/playback`) responder, o que prova que é replay. Só as teclas de seleção de jogador (`1`–`5`, `Q W E R T`) e as da visão de time do replay (`F1` = névoa do time azul, `F2` = do vermelho, `F3` = todos; autorizadas pelo dono em 2026-11-17 para as passadas de visibilidade, conferidas no jogo). Nada de clique de mouse. A única ação de mouse permitida é **mover o cursor para o meio da janela do replay** antes de travar a câmera (com o cursor numa borda o jogo arrasta a câmera e a trava cai); nunca clicar.
4. **Não ler memória** do jogo nem injetar nada. Fontes: LCU, Replay API, Live Client Data, captura de tela.
5. **Replay não fica aberto à toa.** Todo fluxo que abre o replay fecha ao terminar, ao falhar e ao cancelar
   (`POST https://127.0.0.1:2999/Exit`, espera até 15 s, depois encerra o processo `League of Legends`; nunca o `LeagueClient*`).
   Sondas e testes reais também: abriu, usou, fechou.
6. **`token.txt`** (chave da Riot) nunca vai para o git nem para log. A chave é opcional.

## Idioma e estilo

Tudo em português do Brasil: comentários (explicam o **porquê**), erros, interface, docs, commits (conventional commits:
`feat(replay): …`). JSON com chaves em português; erro é sempre `{"erro": "..."}`. Segue as convenções do projeto irmão
`D:\EMP\Emp\NZT\Sistemas\Livros` (referência de estilo, não dependência; não copie código de IA de lá).

## Layout

```
server/      Go (stdlib + modernc.org/sqlite, sem CGO). MVC por função: cada função é um pacote com model.go e controller.go.
  banco/ web/ config   infraestrutura
  lcu/                 cliente do LCU (lockfile, lista de rotas permitidas)
  jogador/ partida/    busca de jogador, histórico, estado e download do replay
  replay/              Replay API + Live Client Data + janela do jogo (foco, tecla, captura)
  visao/               leitura da tela por gabarito
  rastreio/            fila dos passes, laço de amostragem, eventos SSE
  analise/ estudo/     zonas, saída de rota, retrato dos abates, listas e comparação
app/         Electron. src/main (mínimo) e src/renderer (HTML/CSS/JS puro, sem framework), servido pelo servidor em /.
docs/        arquitetura/ (descobertas, contrato HTTP, dados) e decisoes/.
```

## Comandos

```
cd server && go run .                    # http://127.0.0.1:8770, SQLite em server/dados/lol.db
cd server && go test ./...               # nunca toca no LoL de verdade (tudo com httptest e capturas guardadas)
cd server && LOL_TESTE=1 go run .        # modo teste: LCU e jogo falsos, determinísticos
cd server && go test -tags real -run TestReal -count=1 -v ./...   # sondas com o LoL de verdade (cliente aberto; abrem e FECHAM o replay)
cd app && npm install && npm start
cd app && npm test
```

## Arquitetura (resumo)

- Cada pacote exporta `Migra(db)` e `Registra(mux, deps…)`; `main.go` é o índice e o único com `log.Fatalf`.
- Model é dono do SQL e das regras; controller só traduz HTTP. Erros por `web.WriteErr`: 400, 404, 409, 500. Listas envelopadas, nunca `null`.
- Trabalho demorado (download, rastreio) é uma **execução** com eventos por SSE.
- Rota nova: primeiro a linha em `docs/arquitetura/contrato-http.md`, depois o código.
- Tudo que fala com o mundo de fora (LCU, jogo, tela, teclado) fica atrás de interface, com implementação falsa para teste.
