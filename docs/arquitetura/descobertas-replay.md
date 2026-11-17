# Descobertas: como o rastreio do replay funciona de verdade

Tudo aqui foi **medido nesta máquina em 2026-11-17**, cliente 16.22.834.8910, tela 2560×1440, região BR,
conta `GodiNzt#br1`. Onde está escrito "não verificado", é hipótese a confirmar antes de depender dela.

## 1. As três fontes de dados (nenhuma lê memória do jogo)

| fonte | onde | o que dá | autenticação |
|---|---|---|---|
| **LCU** (API local do cliente) | `https://127.0.0.1:<porta>` | jogador, histórico, dados finais da partida, linha do tempo por minuto, baixar e abrir replay | Basic `riot:<senha>`; porta e senha em `C:\Riot Games\League of Legends\lockfile` (`nome:pid:porta:senha:protocolo`) |
| **Replay API** (dentro do jogo) | `https://127.0.0.1:2999/replay/*` | tempo, velocidade, pausa, seek, câmera, seleção | nenhuma; precisa `EnableReplayApi=1` em `[General]` de `C:\Riot Games\League of Legends\Config\game.cfg` (já ligado; backup em `game.cfg.bak-loltracking`) |
| **Live Client Data** (dentro do jogo) | `https://127.0.0.1:2999/liveclientdata/*` | os 10 jogadores (itens, nível, placar, morto, respawn, runas, feitiços), eventos | nenhuma |
| **Tela** (captura + leitura por gabarito) | janela `League of Legends (TM) Client` | vida, mana, atributos e cooldowns do jogador selecionado; barras dos 10 nos quadros laterais | — |

Os certificados são autoassinados: o cliente HTTP ignora a verificação TLS **só** para `127.0.0.1`.
O `openapi` completo do jogo está em `GET https://127.0.0.1:2999/swagger/v3/openapi.json`.

A **API da Riot** (`token.txt`, `X-Riot-Token`) é opcional: em 2026-11-17 a chave devolveu
`401 Unknown apikey` (chave de desenvolvimento vence em 24 h). Nada do núcleo depende dela.

## 2. LCU: rotas verificadas

- `GET /lol-summoner/v1/current-summoner` → `gameName`, `tagLine`, `puuid`.
- `GET /lol-summoner/v1/alias/lookup?gameName=X&tagLine=Y` → `{"alias":{...},"puuid":"..."}`. É a busca por nome.
- `GET /lol-match-history/v1/products/lol/{puuid}/matches?begIndex=0&endIndex=20` → `games.games[]` com
  `gameId`, `queueId`, `gameMode`, `gameVersion`, `gameDuration` (s), `gameCreation` (ms), `gameCreationDate`,
  `participants[].stats.win`, `participants[].championId`, `teamId`, `participantIdentities[].player`.
  Funciona para **qualquer** puuid, não só o logado (`current-summoner` no lugar do puuid também vale).
- `GET /lol-match-history/v1/games/{gameId}` → a partida inteira, com os 10 `participantIdentities` (puuid, gameName, tagLine).
- `GET /lol-match-history/v1/game-timelines/{gameId}` → `frames[]` de 60 em 60 s com `participantFrames` (posição `x`,`y`,
  `currentGold`, `totalGold`, `xp`, `level`, `minionsKilled`, `jungleMinionsKilled`) e `events[]`
  (abates com posição, itens, wards, objetivos, level up). ~117 KB por partida. **Sem chave da Riot.**
- `GET /lol-replays/v1/configuration` → `gameVersion`, `isReplaysEnabled`...
- `GET /lol-replays/v1/rofls/path` → pasta dos `.rofl` (`C:/Users/luizg/OneDrive/Documentos/League of Legends/Replays`).
- `GET /lol-replays/v1/metadata/{gameId}` → `{"state": "...", "downloadProgress": n}`. Estados vistos:
  `download` (dá para baixar), `watch` (já baixado), `incompatible` (patch antigo: **replay só abre no patch em que foi jogado**),
  e 404 `Plugin found no local metadata` quando o cliente nunca viu a partida.
- `POST /lol-replays/v2/metadata/{gameId}/create` com
  `{"gameVersion":"16.22.834.8910","gameType":"MATCHED_GAME","queueId":420,"gameEnd":<gameCreation+duração*1000>}` → 204;
  depois o `GET metadata` passa a responder. (A `v1` desta rota não existe: 404 `Invalid URI format`.)
- `POST /lol-replays/v1/rofls/{gameId}/download` com `{"componentType":"replay-button_match-history"}` → 204; poll no metadata até `watch`.
  **Verificado com partida de outro jogador** (3304553006, em que a conta logada não jogou): baixou, 15,8 MB.
  A Riot anunciou em 2026 restrição de download a partidas próprias; hoje ainda funciona, pode mudar. Tratar o erro com mensagem clara.
- `POST /lol-replays/v1/rofls/{gameId}/watch` com o mesmo corpo → 204; o jogo abre e a Replay API responde em segundos.

## 3. Replay API: o que foi medido

- `GET /replay/playback` → `{length, paused, seeking, speed, time}`. `POST` aceita `time` (seek), `speed`, `paused`.
  `speed: 2.0` e `speed: 8.0` aceitos; a 8× mediu-se 16,3 s de jogo em 2 s reais.
- `GET /replay/render` → câmera (`cameraPosition{x,y,z}`, `cameraRotation`, `fieldOfView`, `cameraMode`, `cameraAttached`),
  `selectionName`, e as chaves de interface (`interfaceScoreboard`, `interfaceMinimap`, `interfaceFrames`, `interfaceChat`...).
- **A armadilha da "Directed Camera".** `POST /replay/render {"selectionName":"GodiNzt","cameraAttached":true}` devolve
  `cameraAttached:true`, mas o seletor da tela continua em "Directed Camera", a câmera fica pulando e `selectionName` volta a `""`.
  O que trava de verdade é a **tecla do jogador**: com a janela do jogo em foco, `1`–`5` selecionam o time azul (ORDER) e
  `Q W E R T` o vermelho (CHAOS), na ordem do `playerlist`; apertar **duas vezes** prende a câmera. Verificado com `3`,`3` →
  seletor "Akshan (GodiNzt)", `selectionName:"GodiNzt"` estável, câmera seguindo. As outras 9 teclas: não verificadas.
  A tecla foi enviada por `SendInput` com scancode (`KEYEVENTF_SCANCODE`), ~60 ms pressionada.
- A trava **sobrevive à morte** do jogador (a câmera fica nele na fonte) e **não sobrevive ao seek**: depois de qualquer
  `POST playback {time}` o `selectionName` volta a `""`. Regra: depois de seek, reapertar a tecla e conferir `selectionName`.
- Com a câmera presa, `cameraPosition` anda junto do campeão (ex.: `(6250, 1912, 5306)` com o Akshan no meio, `y` fixo em 1911,85,
  rotação `(0, 56, 0)`, FOV 40). A posição do campeão no mapa é `câmera + deslocamento fixo`. O deslocamento **não foi calibrado**:
  calibrar por mínimos quadrados contra as posições por minuto da linha do tempo do LCU (seção 2), que são exatas.
- `GET /liveclientdata/playerlist` → por jogador: `riotIdGameName`, `riotIdTagLine`, `championName`, `team` (`ORDER`/`CHAOS`),
  `position` (`TOP|JUNGLE|MIDDLE|BOTTOM|UTILITY`), `level`, `isDead`, `respawnTimer`, `items[]` (`itemID`, `slot`, `count`, `price`),
  `scores{kills,deaths,assists,creepScore,wardScore}`, `runes`, `summonerSpells`, e
  `screenPositionCenter`/`screenPositionBottom` (pixels na tela; `3.4e38` quando fora da tela).
- `GET /liveclientdata/eventdata` → `Events[]` (`GameStart`, `ChampionKill` com `KillerName`/`VictimName`/`Assisters`, `Multikill`,
  objetivos...). Só acumula o que foi tocado desde o último seek: um passe a partir de 0 pega tudo.
- `GET /liveclientdata/activeplayer` → 400 no replay ("Spectator mode doesn't currently support this feature"). Vida exata **não vem da API**.
- `GET /liveclientdata/gamestats` → `gameTime`, `mapNumber`.
- O jogo aceita `interfaceScoreboard:true` pela API: abre o placar (itens, K/D/A, CS dos 10) sem tecla.

## 4. Tela (referência 2560×1440, HUD em escala padrão)

- Painel do jogador selecionado, canto inferior esquerdo, aproximadamente `x 0–420, y 1100–1340`:
  barra verde com `vida / vida_max` (ex.: `1274 / 1274`), barra azul com `mana / mana_max`, barra roxa de XP, nível sobre o retrato,
  seis atributos (dano de ataque, poder de habilidade, armadura, resistência mágica, velocidade de ataque, velocidade de movimento),
  e a fileira de ícones: passiva, Q, W, E, R, e os dois feitiços. Ícone em recarga fica escuro com o **número de segundos** por cima
  (ex.: `62` no R, `151` no Incendiar); o número pequeno embaixo é o nível da habilidade.
- Quadros laterais (5 à esquerda, 5 à direita): retrato, nível, barra de vida (verde), barra de mana (azul), feitiços, indicador do R;
  morto mostra o contador de respawn em vermelho sobre o retrato. Dá a vida **em %** dos 10 a cada amostra, sem trocar de jogador.
- Topo: placar dos times, ouro, torres, dragões, relógio. Minimapa no canto inferior direito.
- O texto é nítido em resolução nativa: leitura por **gabarito de dígitos** (sem IA em tempo de execução).
- Captura por GDI (`BitBlt` da tela) funcionou com o jogo em janela sem borda. Tela cheia exclusiva: não verificado.

## 5. Custo de tempo

Uma partida de 39 min, um passe por jogador a 2× = 19,5 min por jogador, **~3 h 15 min** os 10. A 8× = ~49 min.
A velocidade é configuração (padrão 2×, como pedido); a leitura da tela a 8× precisa ser validada antes de virar opção.
