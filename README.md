# LoL Tracking

App desktop que rastreia replays do League of Legends: busca um jogador, lista as partidas com o estado do replay, baixa o
replay e assiste sozinho, um passe por jogador, gravando posição, estado dos 10 e eventos num SQLite local, mais vida, recurso, atributos e
recargas do jogador seguido e as barras dos 10 lidas da tela por gabarito (`server/visao`, só 2560×1440). Sem IA no rastreio. O rastreio
abre o replay, trava a câmera com as teclas de seleção de jogador e **fecha o jogo** ao terminar, pausar, cancelar ou falhar. Regras de segurança da conta e layout: [`CLAUDE.md`](CLAUDE.md). O que foi medido no jogo:
[`docs/arquitetura/descobertas-replay.md`](docs/arquitetura/descobertas-replay.md).

## Rodar

Precisa de Go 1.25 e Node 21+. O cliente do LoL aberto e logado (sem ele, o app abre e avisa).

```
cd server && go run .              # http://127.0.0.1:8770, banco em server/dados/lol.db
cd server && LOL_TESTE=1 go run .  # LCU e jogo falsos (vida, mana e recargas inventadas): nada toca no LoL nem na rede
cd app && npm install && npm start # janela Electron (sobe o servidor se ele não estiver de pé)
```

| variável | padrão | |
|---|---|---|
| `LOL_PORTA` | `8770` | |
| `LOL_DADOS` | `dados` (relativo a `server/`) | banco e cache |
| `LOL_TESTE` | — | `1` = modo teste |
| `LOL_LOCKFILE` | `C:\Riot Games\League of Legends\lockfile` | porta e senha do LCU |
| `LOL_TOKEN_RIOT` | `../token.txt` | chave opcional da API da Riot (nunca vai para o git) |
| `LOL_RENDERER` | `../app/src/renderer` | vazio = só a API |
| `LOL_GUARDA_CAPTURAS` | — | `1` = guarda 1 captura PNG + JSON da API a cada `LOL_CAPTURA_A_CADA` s de jogo em `dados/capturas/<game_id>/` (material da leitura da tela; ~3,4 MB cada) |
| `LOL_CAPTURA_A_CADA` | `30` | segundos de jogo entre capturas guardadas |

O rastreio mexe numa linha do `game.cfg` do jogo (`EnableReplayApi=1`, deduzido do `LOL_LOCKFILE`), com backup único em
`game.cfg.bak-loltracking`.

## Como usar (passo a passo)

1. Abra o cliente do LoL e entre na conta. Depois: `cd app && npm start` (a janela sobe o servidor sozinha se ele não estiver de pé).
   A barra de cima diz se o cliente está conectado; com ele fechado dá para ver o que já foi guardado, mas não buscar,
   baixar nem rastrear.
2. **Jogadores**: digite `Nome#TAG` e Buscar. Cada partida mostra o estado do replay; **Baixar** traz o replay (não abre o jogo).
3. Com o replay baixado, **Rastrear** abre a escolha: quais jogadores (os 10 de começo), velocidade (2× recomendada; 4× e 8×
   terminam antes) e, se quiser, só um trecho (`0:00` até `15:00`). O tempo estimado aparece antes de confirmar. O app recusa
   se houver partida ao vivo ou outro rastreio rodando.
4. **Fila de rastreio** mostra ao vivo: o progresso, cada jogador (pronto, rastreando com o tempo de jogo, na fila, erro), a
   última leitura (vida, mana, posição e zona, recargas) e avisos (tela que a leitura não conhece, câmera que saiu do jogador,
   conferência com a Riot). Pausar fecha o replay e guarda onde parou; Retomar continua dali; Cancelar guarda o que já foi lido.
   **O jogo precisa ficar visível na tela** enquanto roda.
5. **Partidas rastreadas → Partida**: o mapa no momento de cada abate com os 10 (rastreado com borda cheia, aproximado
   tracejado), onde cada um estava (zona, vida, R e Flash, há quanto tempo fora da rota, quantos contra quantos, e quem bateu
   com o quê quando há chave da Riot), a vida ao longo da partida com mortes e saídas de rota, a lista de saídas, a linha do
   tempo (filtros por tipo), os indicadores e o CS conferido. Clicar num horário abre o mapa com play ali.
6. **Mapa com play** (tela cheia): os 10 com o retrato do campeão e o nome, andando; espaço toca/pausa, ← → andam 5 s, F
   tela cheia, Esc fecha o painel e depois sai. Velocidade 1×–8×, barra de tempo com as marcas dos abates (cor do time de quem
   matou) e dos objetivos, opção de pintar as zonas. Clicar num jogador abre vida, mana, Q W E R e feitiços com recarga,
   atributos, itens, nível, K/D/A, CS, zona e a atividade deduzida (com o porquê).
7. **Estudo**: crie um estudo (jogador e, se quiser, campeão e posição para filtrar as sugestões), adicione partidas à mão,
   anote cada uma, e veja vitórias × derrotas por indicador (n, média, mediana, diferença; "insuficiente" com menos de 3 de um
   lado). Clicar numa linha mostra as partidas e os momentos que formam o número. **Não é causa**: é o que foi diferente.
8. **Configurações**: cliente do LoL, chave da Riot (válida ou não; a chave nunca aparece), pasta de dados e espaço usado,
   velocidade padrão e tema.

Capturas das telas com dados reais: [`docs/capturas-ui/`](docs/capturas-ui/). Para refazê-las (não abre o LoL): suba um servidor
com uma cópia do banco e outro no modo teste, e rode o script:

```
cp -r server/dados/analise-real server/dados/ui-dev
cd server && LOL_PORTA=8790 LOL_DADOS=dados/ui-dev LOL_LOCKFILE=nao-existe go run .   # dados reais, cliente "fechado"
cd server && LOL_PORTA=8791 LOL_TESTE=1 LOL_DADOS=/tmp/lol-teste go run .            # jogo falso para a fila ao vivo
cd app && npx electron scripts/capturas.mjs                                            # janela própria, PNGs em docs/capturas-ui/
```

## Testes

```
cd server && go test ./...                                         # sem o LoL (jogo falso com relógio virtual)
cd server && go test -tags real -run TestReal -count=1 -v -timeout 30m ./lcu/ ./partida/   # com o cliente aberto (só leitura)
cd server && go test -tags real -run TestRealJanela -count=1 -v ./replay/                  # ABRE e FECHA o replay (~30 s)
cd server && go test -tags real -run TestRealRastreio -count=1 -v -timeout 30m ./rastreio/ # ABRE e FECHA o replay 2× (~3 min); banco em dados/sonda-rastreio.db
cd app && npm test
```

As sondas do rastreio precisam da partida 3304594274 baixada; `TestRealVelocidades` (4× e 8×) e `TestRealCalibracao`
(10 min a 4×) são medidas extras.


## Análise e estudo (tarefa 4)

Quando um passe termina, a partida é analisada sozinha (`server/analise`, sem IA, só regra e conta): zona do mapa de cada
amostra (`docs/arquitetura/zonas.png`), saídas de rota (rotador) e visitas a rotas (caçador), estadias na base, atividade
deduzida (farmando, trocando com X, lutando, objetivo/torre, andando, na base, morto — com motivo e confiança), o retrato de
cada abate (onde e como estava cada um dos 10; com a chave da Riot, quem bateu com o quê) e os indicadores por jogador. Refazer:
`POST /api/partidas/{id}/analise`. O **estudo** (`server/estudo`) junta partidas de um jogador escolhidas à mão e compara os
indicadores nas vitórias × derrotas (média, mediana, tamanho de efeito; com menos de 3 partidas de um lado, "insuficiente").
Imagens do Data Dragon (retrato do campeão, ícone do item) ficam em cache em `dados/imagens/`. Rastrear só um trecho:
`{"de_ms": 0, "ate_ms": 900000}` no corpo do rastreio.

```
cd server && LOL_GERA_ZONAS=1 go test -run TestGeraImagemZonas ./analise/          # regera docs/arquitetura/zonas.png
cd server && LOL_BANCO_REAL=$PWD/dados/analise-real/lol.db go test -tags real -run TestRealRecalcula -v ./analise/  # análise de passes reais (não abre o jogo)
```

Rotas: [`docs/arquitetura/contrato-http.md`](docs/arquitetura/contrato-http.md). Tabelas: [`docs/arquitetura/dados.md`](docs/arquitetura/dados.md).
