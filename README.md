# LoL Tracking

App desktop que rastreia replays do League of Legends: busca um jogador, lista as partidas com o estado do replay, baixa o
replay e assiste sozinho, um passe por jogador, gravando posição, estado dos 10 e eventos num SQLite local, mais vida, recurso, atributos e
recargas do jogador seguido e as barras dos 10 lidas da tela por gabarito (`server/visao`, só 2560×1440). Sem IA no rastreio. O rastreio, a análise e o estudo só existem pela API por enquanto (as telas são da
tarefa 5): `POST /api/partidas/{id}/rastreio` abre o replay, trava a câmera com as teclas de seleção de jogador e **fecha o
jogo** ao terminar, pausar, cancelar ou falhar. Regras de segurança da conta e layout: [`CLAUDE.md`](CLAUDE.md). O que foi medido no jogo:
[`docs/arquitetura/descobertas-replay.md`](docs/arquitetura/descobertas-replay.md).

## Rodar

Precisa de Go 1.25 e Node 21+. O cliente do LoL aberto e logado (sem ele, o app abre e avisa).

```
cd server && go run .              # http://127.0.0.1:8770, banco em server/dados/lol.db
cd server && LOL_TESTE=1 go run .  # LCU falso, nada toca no LoL nem na rede
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
