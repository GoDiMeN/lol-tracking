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

### 3.1 Calibração da posição (medida, 20 pontos, 2026-11-17)

Câmera presa em cada um dos 10 jogadores nos quadros de 04:00 e 14:00 da partida 3304594274, com o jogo **pausado**.
Dados brutos em `docs/calibracao/pontos.json`, recortes do minimapa em `docs/calibracao/*.jpg`, mapa vazio da Riot em `map11.png`.

- **`posição = (cam_x − 2, cam_z + 1291)`**. Deslocamento constante: não depende do jogador nem da região do mapa.
- 18 pontos válidos: erro mediano 74 unidades, médio 77, máximo 179 (mapa ≈ 15 000 de lado). Jogador parado: 5 a 8.
  O resíduo é o campeão andando entre o carimbo do quadro do cliente e a leitura (até ~0,1 s) e a suavização da câmera;
  dá para reduzir usando `screenPositionCenter` do `playerlist` (onde o campeão está na tela) para corrigir o atraso da câmera.
- Para desenhar no `map11.png` do Data Dragon: limites do mapa 11 `min (−120, −120)`, `max (14870, 14980)`, com o y invertido.
- **As 10 teclas selecionam o jogador certo** (`selectionName` conferido nas 20 leituras).
- **Defeito aberto: a tecla `T` (10º jogador).** Nas duas leituras o seletor mudou para o jogador certo, mas a câmera **não se moveu**
  (ficou na coordenada do jogador anterior). Não se sabe se é do `T` duplo, do jogo pausado ou do tempo de espera (700 ms).
  O motor **não pode confiar só no `selectionName`**: tem que conferir que a câmera foi até o jogador (por exemplo,
  `screenPositionCenter` dele perto do centro da tela) e tentar de novo se não foi.
- **A falha do `T` só acontece com o jogo pausado**: rodando a 2×, `T`,`T` travou na primeira tentativa.

### 3.2 Calibração com o replay rodando a 2× (medida, 2026-11-17)

10 min de jogo seguindo o jogador 3 (min 2–12) e 10 seguindo o 10 (min 12–22), ~4 leituras por segundo de jogo
(cada leitura: `playback`, `render`, `playerlist`, `playback`; tempo = média dos dois `playback`). 17 pontos com o jogador vivo,
comparados com os quadros por minuto da linha do tempo.

- Só o deslocamento fixo `(−2, +1291)`: erro mediano 94, máximo 235. A câmera **atrasa** em relação ao campeão em movimento.
- **Fórmula a usar** (tela 2560×1440, centro 1280×720), com `sx, sy = screenPositionCenter` do jogador:
  `x = cam_x + 0,2 + 1,374·(sx − 1280)` e `y = cam_z + 1249,6 − 1,768·(sy − 720)`. Erro mediano 29, máximo 95.
  Os coeficientes por pixel dependem da resolução: escalar por `largura/2560` e `altura/1440`, ou reajustar pela linha do tempo
  de cada partida (o motor deve refazer esse ajuste por partida e gravar o erro obtido).
- **Jogador morto:** `selectionName` fica `""`, a câmera sai dele (vai para outro lugar do mapa) e `screenPositionCenter` vira `3.4e38`
  ou um valor fora da tela. Ao renascer a seleção **volta sozinha**. Enquanto `isDead`, não usar a câmera: posição = local da morte
  (evento da linha do tempo) e, perto do respawn, a fonte do time.
- `screenPositionCenter` vem como **texto** `"x,y"` com ponto decimal.

### 3.3 Pedidos de produto que dependem destas medidas

- **Uso de habilidade:** transição pronto → recarga no ícone (Q, W, E, R, D, F) lido da tela = habilidade usada naquele instante.
- **Ataque e alvo:** não existe na API do replay e não se lê memória. Vira **atividade deduzida** por amostra:
  `farmando` (CS subindo, sem campeão inimigo perto), `trocando` com X (inimigo ao alcance e vida de um dos dois caindo, pelos
  quadros laterais), `objetivo`/`torre` (perto da estrutura + evento da linha do tempo), `andando`, `na_base`, `morto`.
- **CS:** `scores.creepScore` do `playerlist` a cada segundo, conferido a cada minuto contra `minionsKilled + jungleMinionsKilled`
  da linha do tempo; divergência gravada.
- **Mapa com play:** tela cheia, retrato do campeão (Data Dragon) + nome, play/pausa/velocidade/barra de tempo, atividade de cada um,
  clique no jogador abre vida, mana, recargas, atributos e itens do instante. Exige rota que devolva o estado dos 10 por janela de tempo.
- `POST /Exit` na porta 2999 devolve 204 e o jogo leva alguns segundos para fechar; esperar até 15 s antes de matar o processo.
  **Corrigido na seção 4.1:** o 204 vem, mas o jogo não fecha; quem fecha é o encerramento do processo depois dos 15 s.

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

## 4.1 O motor em Go contra o jogo (medido, 2026-11-17, tarefa 2)

Sondas `go test -tags real` em `server/replay` e `server/rastreio`, partida 3304594274, jogador 3 (GodiNzt/Akshan) e 10
(FabioBR/Lux). Cada execução abriu e fechou o replay; no fim, `League of Legends.exe` fechado e `LeagueClientUx.exe` aberto.

- **Tela.** Com o processo DPI-aware, a área cliente da janela do jogo é **2560×1440** (o `game.cfg` diz `Width=3840 Height=2160`,
  mas é o tamanho físico que a janela tem nesta máquina; o `screenPositionCenter` vem nesse mesmo espaço: centro ~(1280, 720)
  com a câmera presa). A captura GDI da área inteira saiu nítida (HUD, painel, quadros laterais, minimapa).
- **Custo de uma leitura** (playback, render, playerlist, playback): ~27 ms a 2× e 4×, ~32 ms a 8×. Cada pedido 3,5–5 ms.
  A captura da área inteira, em paralelo, ~24–28 ms. Abrir o replay até a API responder: ~6 s.
- **Taxa** (meta 4 por segundo de jogo): **3,98** a 2×, **3,96** a 4×, **3,67** a 8× (a 8× o ciclo de 31 ms já é o tempo da
  leitura; com a leitura da tela da tarefa 3 vai cair mais). A 8× o seek precisa de folga: o relógio anda 8 s de jogo por
  segundo real logo depois dele.
- **Teclas.** `3`,`3` e `T`,`T` travaram na 1ª tentativa com o jogo rodando (o `T` pausado continua não confiável — o motor
  despausa antes). A trava não caiu em 19 min de jogo seguidos, fora o recall e a morte.
- **Morte** (8:48, respawn 19,9 s): `isDead` vira verdadeiro na amostra de 8:48, `selectionName` fica `""`, a posição vem do
  evento (4843, 10410); ao renascer (9:08) a seleção **volta sozinha** e a primeira amostra já é de câmera, na fonte.
  Também volta sozinha quando a tecla foi apertada a 2 s da morte e a trava "pegou" no último instante (6:00 → 6:12).
- **Recall:** a câmera pula para a fonte com a seleção mantida; uma amostra sai "fora da tela" no salto (trava perdida de
  ~250 ms, não chega a retravar). Fonte pela câmera: ORDER ~(376–404, 567–595), CHAOS ~(14340, 14507).
- **`creepScore` do `playerlist` anda de 10 em 10** (30 → 40 → 50: é o CS arredondado para baixo na dezena). A conferência
  com a linha do tempo (`minionsKilled + jungleMinionsKilled`) tem de aceitar a dezena: 30 × 36 está certo.
- **`POST /Exit` responde 204 mas o jogo NÃO fecha** (diverge da seção 3.3): em 6 aberturas, o processo continuou vivo depois
  dos 15 s; numa medida dedicada, ainda vivo 46 s depois. O fechamento de verdade é o encerramento do processo
  `League of Legends.exe` (o cliente segue normal). A fase do gameflow lida com o jogo ainda aberto depois do `/Exit` foi
  `None`. A rota `/Exit` é a única "exit/quit/close" no openapi do jogo.
- **Posição.** Ajuste completo (4 coeficientes) com 9 quadros a 4× (min 20–30): **erro mediano 33, máximo 108**; a fórmula
  da 3.2 sem ajuste nos mesmos pontos deu mediano 31 e máximo 254 (o máximo é um quadro com o campeão a 355 px do centro,
  câmera atrasada), e só o deslocamento (−2, +1291) mediano 39 e máximo 415. Os por-pixel ajustados (1,18 e 1,12) saíram
  menores que os da 3.2 (1,37 e 1,77), mas vêm de poucos pontos longe do centro: o motor refaz por partida e grava o erro.
  Trechos curtos (2–3 quadros vivos) só ajustam o deslocamento: 16 (2 pontos, 2×), 72/92 (3 pontos, 8×).
- **Tamanho do bloco compactado:** 7,2 bytes por amostra (6 919 bytes, 956 amostras), 6,8 (16 249 bytes, 2 383 amostras).
- **Eventos.** A linha do tempo do LCU desta partida só tem `CHAMPION_KILL`, `ELITE_MONSTER_KILL` e `BUILDING_KILL` (sem
  itens nem wards, ao contrário do que a seção 2 sugeria); compras e vendas vêm do inventário do `playerlist` segundo a segundo.

## 5. Custo de tempo

Uma partida de 39 min, um passe por jogador a 2× = 19,5 min por jogador, **~3 h 15 min** os 10. A 8× = ~49 min.
A velocidade é configuração (padrão 2×, como pedido); a leitura da tela a 8× precisa ser validada antes de virar opção.
