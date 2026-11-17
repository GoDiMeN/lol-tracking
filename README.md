# LoL Tracking

App desktop que rastreia replays do League of Legends: busca um jogador, lista as partidas com o estado do replay, baixa o
replay e (nas próximas tarefas) assiste sozinho gravando posição, vida, cooldowns e eventos num SQLite local. Sem IA no
rastreio. Regras de segurança da conta e layout: [`CLAUDE.md`](CLAUDE.md). O que foi medido no jogo:
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

## Testes

```
cd server && go test ./...                                         # sem o LoL
cd server && go test -tags real -run TestReal -count=1 -v ./...    # com o cliente aberto (só leitura)
cd app && npm test
```

Rotas: [`docs/arquitetura/contrato-http.md`](docs/arquitetura/contrato-http.md). Tabelas: [`docs/arquitetura/dados.md`](docs/arquitetura/dados.md).
