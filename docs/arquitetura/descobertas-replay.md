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

## 4.2 Leitura do HUD por gabarito (medida, 2026-11-17, tarefa 3)

Pacote `server/visao`. Coordenadas em `regioes.go` (referência 2560×1440, HUD na escala padrão), medidas em 63 capturas
da partida 3304594274 (Akshan e Lux).

- **Painel** (só aparece com a câmera presa num jogador vivo; morto com seleção vazia ou câmera dirigida = sem painel). Texto
  `vida / máx` em fonte pequena (glifos de 10 px, linhas 1142–1151, x 102–235); recurso em fonte maior (14 px, 1170–1183). A
  barra `/` do recurso às vezes não passa do limiar (sobre o azul-claro): a divisão cai no maior espaço. Nível no canto do
  retrato (64–98 × 1190–1213). Atributos em duas colunas (x 266–313 e 336–380; linhas 1133, 1160, 1185), cinza ou amarelo
  (bônus): limiar por luminância. Linhas separadoras escuras e uniformes em y 1160 e 1189 (x 106–230) = painel presente.
- **Ícones** Q W E R D F em x 72, 124, 175, 227, 293, 340 (43 px), y 1225–1268. **Pronto = colorido**; em recarga, bloqueado
  ou não aprendido o ícone é **cinza puro** (r = g = b): fração de pixels saturados no miolo ≥ 0,47 nos prontos e 0,00 nos
  cinzas (limiar 0,12). Número da recarga no meio (linhas ~1242–1253), branco com contorno preto, com decimal abaixo de 1 s
  (`0.3`). O relógio de recarga desenha traços brancos de 1 px a partir do centro, mas o contorno preto separa os dígitos
  deles (componentes conexos com limiar 160). Nível da habilidade embaixo (`0` = não aprendida). **Ícone cinza sem número**
  acontece com habilidade/feitiço bloqueado por outra ação (ex.: Q e Incendiar do Akshan durante o E; Flash às vezes):
  sai nulo, não 0.
- **Quadros laterais**: barras de 54 px (x 42–95 à esquerda, 2463–2516 à direita; vida em y 275–282, recurso 287–294,
  +137 por quadro), enchem da esquerda nos dois lados, vazio = `0x131313`. Dano recente = pedaço vermelho, não conta.
  **Morto** = retrato em cinza + número vermelho puro (`#ff0000`) por cima; só o vermelho não basta (o retrato do Warwick tem
  vermelho). O número é o `respawnTimer` arredondado para cima. Disponibilidade do R e dos feitiços nos quadros **não lida**
  (o ícone tem o relógio por cima mas sem número; deixado de fora).
- Acerto nas 63 capturas (rótulos a olho em `server/visao/testdata/rotulos.txt`): todos os campos 100% menos `cd_f` 98,3%
  (1 nulo); **0 leituras erradas**. Com os gabaritos gerados só da metade das capturas, a outra metade também dá 0 erradas (os
  nulos vêm de dígito que não estava na metade: nível 18, nível de habilidade 0). Custo: **~1,4 ms por leitura** (benchmark).
- **Sonda real** (`go test -tags real -run TestRealTela ./visao/`, leitor ligado, capturas a cada 5 s): Akshan min 6–10 a
  2×: 955 amostras, **3,98/s de jogo**, leitura da tela **1,56 ms** (mediana; leitura completa 29,5 ms). Com a câmera nele e
  vivo (827 amostras): vida/vida_max/mana/atributos 99,9%, vel. movimento 99,5%, R 100%, D 99,6%, E 99,2%, F 98,8%, W 98,3%,
  Q 92,0% (o resto é ícone bloqueado sem número: Q durante o E do Akshan); quadros laterais 100%. 31 itens de cadeia
  (`habilidade_usada`, `vida_baixa` a 9,5% um segundo antes da morte de 8:48); conferidos nas capturas: Q usado em 6:50,9 com
  7 s → captura de 6:55,4 mostra 2; Incendiar em 8:44,9 com 180 → 179 em 8:45,9. Akshan a 8× (min 16–17): **3,50/s**
  (leitura completa 34 ms, tela 2,1 ms; o ciclo a 8× é de 31 ms, então a taxa já é limitada pela leitura). Master Yi, Teemo e
  Warwick 30 s cada a 2×: 3,74–3,98/s, campos do painel 95–100% (Flash bloqueado no Yi: 86,6%), sem ajuste por campeão.
  Habilidades de vários tempos geram dois usos (R do Akshan: canal 0,3 s e depois 96 s; E: 0,5 s e 18 s): é o que o HUD mostra.
- **Conferência com a Riot** (match-v5 `championStats` por minuto): a Riot **trunca** (1721) e o HUD **arredonda** (1722):
  máximos e atributos conferem com ±1. O `attackSpeed` da Riot não está na unidade do HUD (Warwick 164 × 1,05 na tela): fica
  de fora. Na sonda (com as regras finais): **54 comparações, 50 iguais**. Os 4 diferentes são todos de um mesmo minuto de
  luta do Akshan (min 8, a 1 min da morte): vida 184, mana 68, velocidade de movimento 62 e dano de ataque 4 — valores que
  mudam em fração de segundo (a amostra é de até 250 ms antes/depois) ou por efeito temporário. `vida_max`, `mana_max`,
  armadura, RM e PH bateram em todos os minutos. O Teemo estava morto no quadro de 13:00 (0 comparações).
- **Limites:** só 2560×1440 foi medido (outra resolução → erro `ErrResolucao`, colunas nulas e evento `aviso_tela`); escala
  do HUD diferente da padrão tira tudo do lugar (nenhum marco fixo encontrado → `ErrHUD`, mesmo aviso); idioma do cliente não
  importa (só dígitos). O dígito `9` do nível do campeão e alguns níveis de habilidade só existem nos gabaritos se apareceram
  nas capturas: dígito sem gabarito sai nulo, nunca errado (regerar com `LOL_GERA_GABARITOS=1` e mais capturas).

## 4.3 Análise contra o dado real (medido, 2026-11-17, tarefa 4)

Rastreio pela API do servidor (porta de teste, banco em `server/dados/analise-real/`): partida 3304594274, participantes 3
(Akshan, GodiNzt) e 8 (LeBlanc), `{"velocidade": 4, "ate_ms": 900000}`. Dois passes de 0 a 15:00: 3 537 e 3 536 amostras, 3,97
por segundo de jogo, calibração com erro mediano 13 e 18 (máximo 27 e 37). Conferência com a Riot: 115/117 e 123/126. O replay
fechou sozinho; no fim `League of Legends.exe` não estava rodando (o `LeagueClient*` continuou). `Recalcula`: 157 ms (103
abates, 310 indicadores).

- **Saídas de rota do Akshan** (até 14 min, que contam): 8, 113 s fora. A das 8:36: meio → selva azul de cima (8:36) → rio de
  cima (8:40) → arredores da cova do Barão (8:43) → morto às 8:48 em (4843, 10410), `fim_motivo: morreu`, `desfecho: morte`,
  saiu com 92% de vida e 34% de mana. Bate com a história: Warwick matou o Master Yi na cova às 8:33 e o Malphite às 8:43
  (Teemo ajudando), e o Akshan foi lá e morreu; a Riot mostra o Yi apanhando de `SRU_Horde` (eram as larvas do Vazio).
  Outras saídas: as de 0:30/0:45 (antes das tropas), as curtas para o rio no centro (7–12 s, nada aconteceu) e a das 13:11
  para a selva azul atrás do Warwick (abate às 13:20). As 7:13 e 13:30 terminaram em recall dentro do rio/selva.
- **Zonas** a olho: as mortes dos 3 no Barão (`cova_barao/cova`), as do meio em `rota_meio/centro`, a da LeBlanc às 14:05 em
  `selva_azul_baixo` (perseguida para dentro da selva azul), base/fonte nos renascimentos. Os 20 pontos da calibração caem na
  rota certa (teste).
- **Retrato dos abates** (8:48): vítima pelo evento; LeBlanc rastreada a 1 616 (rio de cima, 92,6%, R pronto, Flash em
  recarga); Warwick e Teemo (não rastreados) postos no local do abate (`perto_do_abate`) → 2 inimigos × 0 aliados; Malphite e
  Yi mortos no próprio local de morte. Akshan com 121/1274 de vida, R em recarga (usado entre 8:33 e 8:43), Flash pronto, fora
  da rota havia 8 s. Riot: Teemo Q/E/R/básico, Warwick básicos/Q/passiva. Aos 2:27 o abate do Akshan na LeBlanc: Incendiar,
  Q, básicos, passiva e E (o "quem bateu com o quê" só existe em volta das mortes).
- **Posição dos rastreados × linha do tempo**: nos abates em que os dois foram rastreados (2:27/2:28, 4:16, 5:58, 12:45),
  a amostra do matador fica a 75–640 do local do evento da vítima em 4 deles (coerente com o alcance) e a 1 226 no de 4:16
  (LeBlanc; não conferido na captura se ela já tinha se afastado ou se é erro de posição).
- **CS**: estado dos 10 × linha do tempo, minutos 1–15: **0 divergências** nos 10 (o `creepScore` arredondado na dezena).
  CS aos 10: Akshan 59, LeBlanc 57; aos 15: 99 × 86.
- **Atividade**: trocas curtas Akshan × LeBlanc repetidas na rota (alta: os dois rastreados, vida caindo nos dois), farmando o
  resto do tempo de rota, `andando` quando atravessa o mapa, na base, morto. **Não bateu / limite:** a luta no Barão (8:43–8:48)
  só aparece como `trocando` sem alvo e confiança baixa, porque Warwick e Teemo não foram rastreados; antes disso, de 8:00 a
  8:36, ele aparece `farmando` (o CS dele subiu naquele minuto); a placa de torre conta como `torre` para quem está perto.

## 5. Custo de tempo

Uma partida de 39 min, um passe por jogador a 2× = 19,5 min por jogador, **~3 h 15 min** os 10. A 8× = ~49 min.
A velocidade é configuração (padrão 2×, como pedido); a leitura da tela a 8× precisa ser validada antes de virar opção.

## 6. Clones, saltos e dano à distância (aviso do dono, 2026-11-17)

Campeões com **clone ou disfarce** (LeBlanc, Shaco, Neeko, Wukong) e com **salto que volta** (W da LeBlanc) enganam qualquer
leitura feita de fora. A regra do projeto: **a posição de um jogador só vale quando vem do passe dele** (câmera travada nele
pela tecla do jogador); nunca deduzir posição pelo que aparece no minimapa ou na tela de outro jogador.

O que foi conferido no dado real (LeBlanc, partida 3304594274, abate das 4:16):
- O "matador a 1 226 da vítima" **não era erro**. A amostra a amostra mostra o W dela: avança ~500 em 0,5 s (249,7 → 250,0 s),
  volta ~435 num único passo (250,7 s) e o W entra em recarga de 11 s. O abate sai 5,5 s depois, com ela já se afastando;
  a API da Riot lista o dano dela na vítima: Q, W, E (corrente, dano atrasado), ataque básico e Incendiar.
- Nesse trecho a vida dela caiu a 8 (o clone da passiva dispara abaixo de 40%) e a câmera **continuou contínua**, sem pulo para
  o clone. Vale para a LeBlanc nesta partida; **Shaco, Neeko e Wukong não foram testados**.

Consequências para o código:
- Distância grande entre matador e vítima é normal (dano atrasado, habilidade de longo alcance, salto de volta, Incendiar):
  não tratar como erro nem descartar; o retrato deve dizer de onde veio o dano quando a Riot informa.
- Salto de posição de centenas de unidades entre duas amostras é legítimo para campeões com deslocamento; não suavizar nem
  descartar como ruído no bloco compactado. No replay virtual, não interpolar em linha reta um salto maior que o que a
  velocidade de movimento permite: mostrar como teleporte.
- **Shaco testado (2026-11-17, partida 3304217746, participante 2, min 5–20 a 4×):** passe concluído sem retrava
  (10 amostras sem trava em 3 566), calibração de 12 pontos com erro mediano 17 e máximo 47, conferência com a Riot 36 de 36.
  O motor detectou 2 usos do R (7:05 e 9:36). Na captura de 9:39 (`docs/calibracao/clone/shaco_0939_clone.png`) aparecem os dois:
  o verdadeiro com nome, nível e contorno de seleção, e o clone colado atrás com uma barra de vida menor, **sem nome e sem nível**;
  a câmera e o painel ficaram no verdadeiro. É **uma** ocorrência observada. O dono informa que o clone também leva o texto
  "Clone" sobre a cabeça; nessa captura o texto não aparece (as barras estão sobrepostas).
- Ainda a testar (Neeko, Wukong, e mais ocorrências do Shaco): a trava da câmera fica no campeão verdadeiro quando o clone nasce?
  o `selectionName` e o painel continuam sendo do jogador? a Neeko disfarçada muda o retrato dos quadros laterais?

## 7. Painel coberto, cursor na borda, recursos diferentes e passiva (2026-11-17)

- **Janela por cima do jogo.** A captura era da tela (BitBlt): com outra janela na frente do jogo, o leitor lia a outra janela.
  Foi o que zerou vida/recargas do passe do Garen na partida 3304217746 (183 leituras em 1541 amostras vivas) e 100 s do passe
  de teste da Vex. Agora a captura pede a imagem à própria janela: `PrintWindow(hwnd, dc, PW_CLIENTONLY|PW_RENDERFULLCONTENT)`.
  Medido com a tela inteira coberta: imagem viva do replay, ~20 ms a 2560×1440 (igual ao BitBlt), sem derrubar a seleção, o replay
  segue andando. Sem a flag 2 a imagem vem preta. Passe de teste com uma janela sobre o painel: 119 de 120 amostras vivas lidas,
  taxa 3,97/s. Se o PrintWindow falhar ou vier preto (janela minimizada), cai no BitBlt da tela.
- **Aviso de painel não lido.** Jogador vivo e seguido sem vida lida por 30 s de jogo → evento `aviso_tela` (uma vez por passe).
- **Cursor na borda.** Com o jogo na frente e o cursor numa borda da tela, o jogo arrasta a câmera e a trava cai sem parar
  (32 tentativas de trava e 7 retravas em 140 s de jogo; taxa caiu para 1,07/s). `Foco()` agora põe o cursor no meio da janela
  do replay quando ele está a menos de 15% de uma borda. Só move; nunca clica. Com isso: 1 tentativa, 0 retravas.
- **Campeão sem recurso (Garen)**: o painel tem o mesmo formato, com a barra de recurso vazia; mana sai nula e o resto é lido.
- **Energia e fúria** (Akali, Lee Sin, Zed, Renekton; Kayn com mana): 15 capturas conferidas a olho, tudo certo. Conferência com
  a Riot num passe a 4×: Akali 81/81, Renekton 95/97 (as 2 diferenças são a fúria, até 20 pontos), Vex 143/144.
- **Nível 9**: faltava o gabarito do dígito; gerado com capturas do Garen e conferido em Akali e Lee Sin.
- **Passiva**: o ícone da passiva (x 20–63, primeiro da fileira) mostra a recarga do mesmo jeito que as habilidades. Lido como
  `cd_p`. Vex: 25/22/19 s conforme o nível, 31 entradas em recarga em 19 min. Garen também tem (regeneração interrompida por dano).
  Na cadeia vira `passiva_em_recarga`; no retrato do abate, `passiva_disponivel`.
## 8. Buracos, retrava e repescagem (2026-11-18; medido no dado, NÃO no jogo)

- **Os buracos da 3304217746** (checagem de qualidade, antes desta mudança): Leona 14,8 s aos 16:16, Orianna 7,4 s (2:29),
  3,9 s (8:01) e 3,9 s (19:20), Garen 3,8–3,9 s (13:06 e 23:50). Os de ~3,9 s são **uma** tentativa de trava a 4×: a
  tentativa antiga eram duas teclas (2 × ~60 ms pressionada + `EntreTeclas` 150 ms), `EsperaTrava` 600 ms parados e ~10
  pedidos de ~4 ms à API e ao LCU, ~0,92 s real = **~3,7 s de jogo a 4×** sem nenhuma linha gravada; até 5 tentativas = 18 s.
- **Retrava sem parar de gravar** (`motor.passoRetrava`): no meio do passe o motor só bloqueia na tecla dupla
  (`ApertaDuplo`, ~0,3 s real = ~1,2 s de jogo a 4×) e volta a ler; a conferência é a das próprias leituras. As amostras
  saem com `trava_perdida` e sem posição, o estado dos 10 continua. Uma tentativa que pega passa a custar ~1,2 s de jogo de
  "vazio" em vez de 3,7 s; uma que falha, ~1,2 s de vazio + 2,4 s gravados sem posição (antes 3,7 s de vazio).
- **`TravaCamera`** (a trava do começo do passe e da repescagem, ainda bloqueante): confere de 100 em 100 ms a partir de
  150 ms depois da 2ª tecla, até 600 ms, e sai na primeira conferência boa. Tentativa que pega: de ~0,92 s para ~0,45 s real
  (no jogo falso, com a câmera chegando na hora). A mesma conferência de antes (seleção, vivo, centro dentro da tela).
- **Não verificado no jogo (seção 8)**: se o duplo continua prendendo quando a 2ª tecla sai dentro do laço (o tempo entre as duas é o
  mesmo, 150 ms, mas agora há leituras antes e depois); quanto tempo a câmera de fato leva para chegar depois da tecla (o
  polling sai antes se ela chega antes de 600 ms). A Orianna 2:29 e o Garen 13:06 saem da fonte e aparecem a 8 600–9 500
  unidades em 4–7 s: é teleporte (o mapa não interpola esses).

### 8.1 O que o jogo esconde (2026-11-18, partida 3305449546, os 10 passes a 4×; medido no DADO gravado, não no jogo)

Primeiro teste completo com o jogo de verdade: a repescagem recuperou 2 de 8 buracos. Os 6 que sobraram, todos com o motivo
"o trecho regravado ainda ficou N s sem a câmera" e N ≈ a duração original, têm a mesma assinatura nas amostras: **a seleção
continua no jogador** (`selecao_ok` em todas as primeiras amostras sem trava; a conferência falha por "fora da tela") e é o
**jogo** que tira o campeão da tela. A repescagem regravou o mesmo vazio porque ele se repete a cada passagem do replay.

| jogador | instante | buraco | o que fazia (amostras e linha do tempo) | causa |
|---|---|---|---|---|
| Warwick (1) | 41:04 | 4,4 s | renasceu 41:00 na fonte azul; Q/W/E/R ilegíveis (ícone cinza sem número) parado na fonte de 41:01,9 a 41:04,6; some 2,8 s depois de começar; reaparece em (13 522, 9 380), a 15 800 unidades | teleporte |
| Master Yi (2) | 17:30 | 4,2 s | `ITEM_DESTROYED` do Olho do Arauto (3513) às 17:23,7; parado 4,4 s em (13 797, 6 419), a rota de baixo vermelha logo depois da torre externa cair (17:20); Q/W/E/D ilegíveis; some e anda devagar (vel. 182–224) 1 580 unidades até (13 431, 7 957), na direção da torre interna; o Q fica ilegível o tempo todo (não é a Investida Alfa, que mostraria a recarga). Ícones ilegíveis 0,75 s antes de sumir | montado no Arauto do Vale (hipótese pela linha do tempo; não visto na tela) |
| Ashe (3) | 3:21 | 6,3 s | na fonte azul; ícones ilegíveis 2,8 s parada; reaparece no meio (5 798, 6 566), a 7 500 unidades, com o D em 299 s | Teleporte (D) |
| Morgana (6) | 15:42 | 4,0 s | na rota de cima (1 772, 10 709), o F saindo da recarga (5…0) e logo ilegível 2,75 s parada; reaparece em (13 758, 4 567), a 13 500 unidades | teleporte |
| Azir (8) | 4:07 | 6,4 s | na fonte vermelha; ícones ilegíveis 3,0 s; reaparece no meio (8 485, 8 221), D em 299 s | Teleporte (D) |
| Azir (8) | 12:30 | 6,7 s | renasceu 12:27; ícones ilegíveis 3,0 s; reaparece no meio (8 796, 8 891), D em 246 s; a tecla da retravada no meio do teleporte deixou a seleção **vazia** (10 amostras) até a tecla seguinte | Teleporte (D) |

- **Teleporte**: o campeão some da tela ~2,75–3,0 s depois de os ícones ficarem ilegíveis (a canalização) e reaparece a
  7 500–15 800 unidades. Teclas apertadas enquanto ele está sumido não prendem (em todos os 5 casos a primeira tecla, ~1 s
  depois de sumir, falhou; a que prendeu foi a de depois de ele reaparecer). Os teleportes mais curtos da partida ficaram
  abaixo do limiar (Ashe 11:08 e 18:37 com 2,9 s, Morgana 26:42 com 2,6 s): o tempo sumido em si já passa de ~2,5 s, então um
  teleporte quase nunca fecha abaixo dos 3 s mesmo com a retravada na hora. Warwick e Morgana (depois dos 14 min) não mostraram
  recarga no F depois do salto: o ícone do Teleporte Liberado não foi conferido.
- **As 407 amostras sem trava do passe 2 (Master Yi)**: 4 delas são do Arauto; o resto são ~100 trechos de 3–4 amostras
  (1,0–1,3 s), a cada 10–30 s o jogo inteiro, nos campos da selva e nas lutas. **Todos começam no instante em que o Q sai de
  pronto para 16–20 s de recarga com a mana caindo 50–68: é a Investida Alfa**, em que o Yi fica intocável e some. A seleção fica
  nele e a câmera **volta sozinha** quando ele reaparece (nenhuma tecla: a retravada só começa com 1 s sem trava). O mapa
  interpola esses trechos (andou pouco).
- **Mudanças no código** (`rastreio/buracos.go`, `repescagem.go`, `repescagem_fila.go`, `motor.go`, `analise/qualidade.go`):
  - `ClassificaBuracos` marca o buraco como `nao_selecionavel` quando as 3 primeiras amostras sem a câmera ainda têm a seleção
    nele e (a) as leituras boas das pontas estão mais longe do que se anda (a mesma conta de `PodeInterpolar`) sem a de depois
    ser a fonte (teleporte), ou (b) uma repescagem já regravou o trecho e a perda voltou (inclusive pelo motivo antigo, que é
    o caso dos 6 desta partida). Os 6 saem `nao_selecionavel` no dado gravado (5 pelas duas regras, o Arauto só pela (b)).
  - A repescagem dá **uma** tentativa a esse buraco; se a perda volta com a seleção nele, para (sem a tentativa extra) e grava o
    restante com o tipo e o motivo "o jogo o tira da tela aqui". O plano da repescagem de passe gravado não inclui mais o que
    já foi regravado e repetiu (nesta partida o plano fica vazio).
  - Na checagem de qualidade vira `nao_selecionavel`, informativo, fora do total (nesta partida o total cai de 8 para 2: os
    dois `salto` que sobram são o teleporte curto da Morgana às 26:42 e um salto de 2 986 unidades em 2,7 s da Kai'Sa às 8:37,
    não investigado). O mapa liga as pontas quando `PodeInterpolar` deixa (o Arauto, sim; teleporte, não: mostra o salto).
  - Retravada mais cedo: se depois da tecla a seleção sai do jogador (o caso do Azir às 12:30), o motor aperta de novo depois de
    `EsperaMinimaTrava` em vez de esperar o resto de `EsperaTrava` (a câmera não chega sozinha sem a seleção). No Azir seriam até
    ~2 s de jogo a menos; nos outros teleportes a seleção ficou nele e nada muda.
- **Não verificado no jogo**: o Arauto (só pela linha do tempo e pela velocidade); se a tecla apertada com o campeão sumido
  sempre limpa a seleção ou só às vezes (1 de 5 aqui); se apertar de novo logo depois da seleção cair não gasta as 5
  tentativas antes de o teleporte terminar; se a câmera volta sozinha depois do teleporte com a seleção mantida (no Warwick e
  na Morgana voltou 1,3–1,8 s depois da primeira tecla, sem a segunda: não dá para separar).

## 9. Visão de time: névoa por time e o minimapa (2026-11-17/18; 3 capturas do jogo, o resto NÃO verificado no jogo)

Medido pelo dono em 2026-11-17 na partida 3304217746, 2560×1440, com uma sonda PowerShell (scancodes `0x3B`/`0x3C`/`0x3D`,
captura por `PrintWindow`): replay em 10:00 a 1×, 3 s rodando, pausado; uma captura do minimapa (recorte 360×365 a partir de
(2195, 1070)) e do painel (760×150 a partir de (0, 1290)) antes das teclas e depois de cada uma. As capturas estão em
`server/visao/testdata/minimapa/` (com as posições rastreadas dos 10 no instante em `posicoes_603.json`).

- **Teclas**: `F1` põe a névoa do replay na visão do time azul, `F2` na do vermelho, `F3` em "todos". O painel do replay
  (canto inferior esquerdo) mostra "Fog of War: Blue | Red | All": "Blue" em azul vivo (`40c1ff`), "Red" em vermelho (`ff3333`),
  "All" em branco, sobre `182726`; a leitura conta os pixels de cada cor na caixa (x 226–300, y 1353–1376). A Replay API
  (`/replay/render`) só tem o booleano `fogOfWar` (já vinha `true`): **o time da névoa só se confere pela tela**.
- **O jogo não informa visibilidade**: `/liveclientdata/playerlist` tem posição na tela, vida, itens, e nenhum campo de visível.
- **O minimapa na visão de um time**: os retratos do próprio time aparecem sempre; o de um inimigo só quando o time o enxerga;
  só as sentinelas do time aparecem. Retrato = imagem redonda com um anel de ~2 px na cor do time (azul `0099e0`, vermelho
  `e83d3d`), raio 14,5 px; as torres e inibidores têm outras cores (azul `389bba`, vermelho `7c2221`) e formas; as tropas são
  pontos de ~4 px. A captura foi em **10:03,0** (não 10:00: o seek a 1× andou 3 s), confirmado pelo melhor ajuste.
- **Geometria** (a primeira; **substituída em 9.1**): `px = 2206,79 + 0,022681·x`, `py = 1423,53 − 0,023068·y` (tela 2560×1440),
  ajustada nos 6 retratos achados contra as posições rastreadas: erro quadrático médio **1,05 px** (máximo 2,0 px, o da Diana,
  meio coberto pela Orianna); 1 px ≈ 44 unidades. Os limites do mapa 11 do map11.png esticados no quadrado preto do minimapa
  (x 2206–2546, y 1083–1423) dão 2,9 px (máximo 4,1): o minimapa do jogo tem a escala vertical ~1,7% maior.
- **Leitura dos retratos** (`server/visao/minimapa.go`): votação de círculos nas duas cores + fração do anel presente (≥ 0,62 de
  64 direções) + miolo sem a cor. Nas 4 capturas: todos os retratos vivos achados (a Diana com 0,83), nenhum falso (os círculos dos
  inibidores azuis chegam a 0,52), ~3,3 ms por leitura.
- **Decisão nas capturas** (com as posições rastreadas): visão azul — Garen e Orianna à vista, Vi e Senna fora (Draven morto);
  aliados 3 de 3. Visão vermelha — Diana e Yunara à vista, Leona fora (Ambessa e Shaco mortos); aliados 3 de 4. Bate com o que
  se vê nas capturas.
- **A Senna fora do lugar (aberto)**: em "todos" e na visão vermelha o retrato da Senna aparece em (14 234, 11 735) — na rota de
  baixo perto do inibidor vermelho —, a ~7 800 unidades de onde o passe dela E o quadro de 10:00 da linha do tempo do LCU a põem
  (13 815, 6 463 → 13 726, 5 150 em 10:03), e nada aparece no lugar rastreado. É onde ela estaria ~15 s antes, voltando da fonte.
  Não se sabe se é o minimapa atrasando um retrato depois do seek, um efeito da Senna (névoa/camuflagem do E) ou outra coisa.
  Por isso a decisão nunca dá "fora" para um inimigo quando sobra anel inimigo que não casa com ninguém e todos têm posição,
  e confere os aliados (que têm de aparecer sempre) em toda captura.
- **Sentinelas**: marcador "olho com asas" de ~12×10 px; vermelho cheio (`de2429`), azul só no contorno (`2880ba`, miolo
  azul-escuro). Lidas nas capturas: na visão azul, 2 azuis (rio do meio (6402, 8325) e rio de baixo (12222, 4401)); na vermelha,
  2 vermelhas inteiras (rio do meio (6711, 8736) e rio de baixo (12222, 4510)). Ficam de fora: uma vermelha meio coberta pelo
  retrato da Vi e um marcador vermelho de outro desenho no rio de cima (tipo não identificado). Em "todos", a azul do rio de
  baixo some embaixo da vermelha. Duas tropas encostadas também parecem o marcador na tolerância de 1 px: o tamanho (12–14 px)
  é o que separa.
- **NÃO verificado no jogo (até 2026-11-17; ver 9.1)** (o passe de visão foi feito e testado só com o jogo falso): se o painel aparece e muda com o replay
  rodando a 8×; se a névoa sobrevive ao seek; se apertar F1 duas vezes continua em "Blue"; se o minimapa a 8× desenha os retratos
  no lugar certo (atraso de desenho); a confiabilidade da leitura em mais situações (lutas com 3+ retratos encostados, base,
  borda do mapa, sentinela de controle e outros marcadores, retrato do Shaco/clone, Neeko); o erro da geometria longe dos 6
  pontos medidos (cantos do mapa).

### 9.1 Os dois passes de visão da 3305449546 (2026-11-18; refeito no DADO, sem o jogo)

Os passes a 8× (5 533 capturas na visão azul, 5 557 na vermelha, 41 min) com os 10 rastreados a 4×. O painel conferiu a névoa
em 98% das capturas (as outras ~110 de cada passe, com o painel ilegível). Tudo abaixo é o bruto (`visao-p<n>-capturas.tsv`)
refeito com as posições rastreadas, sem abrir o replay.

- **Geometria refeita** com todos os pares "anel do próprio time achado ↔ posição rastreada daquele aliado no mesmo instante"
  (casados a ≤ 8 px, iterando o ajuste; fora a 2 500 unidades das fontes, onde a posição "base" é a fonte fixa): **38 480
  pares** (18 368 azuis, 20 112 vermelhos), o mapa inteiro. Por mínimos quadrados, eixo a eixo:
  `px = 2205,37 + 0,022861·x`, `py = 1422,54 − 0,022893·y` (era `2206,79 + 0,022681·x`, `1423,53 − 0,023068·y`). As duas
  escalas saem iguais a 0,1% (o minimapa é quadrado); a velha errava ~0,8% em cada eixo (até ~3 px nos cantos), por ter vindo
  de 6 pontos perto do centro de uma captura.
  - Resíduo nos mesmos pares: **nova 1,68 px (rms), mediana 0,71, 95% abaixo de 3,5**; a velha 1,93 / 1,20 / 3,7 — e 2,45 /
    1,81 / 4,5 como era usada (sem a correção do atraso, abaixo).
  - **É linear**: o resíduo médio em cada nono do mapa (grade de 5 000 unidades) fica entre −0,2 e +0,7 px (o maior, +0,7 px
    vertical, no canto da base azul); um ajuste com termos quadráticos e cruzados baixa o rms só de 1,68 para 1,66 px.
  - Nas 3 capturas pausadas de 2026-11-17 (sem atraso nenhum), a geometria nova põe os 6 retratos a 0,4–1,7 px das posições
    rastreadas (os testes de `visao` continuam passando).
- **Atraso**: deslocando o instante da posição rastreada de −1 s a +1 s, o erro é mínimo com a posição de **175–200 ms de
  jogo ANTES** do carimbo da captura (mediana 0,71 px contra 1,54 px sem deslocar; ±100 ms em volta do mínimo pioram 0,1–0,2
  px; só os pares de quem andava: 0,73 contra 1,85). A 8× isso é ~22 ms reais, pouco mais de um quadro a 60 Hz: o PrintWindow
  devolve o último quadro composto. Fica `replay.AtrasoMinimapaMs` = 22 ms reais × a velocidade do passe (hipótese: tempo real;
  só 8× foi medido) e a decisão usa as posições daquele instante.
- **Aliados achados e indeterminado** (visão azul / vermelha):

  | | aliados achados | indeterminado |
  |---|---|---|
  | antes (geometria velha, sem atraso) | 78,4% / 86,6% | 14,5% / 13,3% |
  | geometria nova + atraso | 78,5% / 87,0% | 12,2% / 13,1% |
  | + aliados conferíveis e oclusão a 20 px (o que ficou) | **96,2% / 95,3%** | **10,7% / 16,6%** |

  A geometria quase não mexeu nos aliados: o que faltava não era geometria. Dos aliados vivos com posição não achados, a
  maioria tinha **outro campeão encostado**: achados por distância do vizinho mais perto (azul / vermelho): 0–12 px 57–62% /
  69–89%, 12–16 px 67% / 81%, 16–20 px 77% / 90%, 20–24 px 90% / 95%, ≥ 24 px 94–98%; a menos de 2 500 da própria fonte, 48% /
  66% (a posição "base" é a fonte fixa e os retratos se juntam). Sem vizinho e longe da fonte, só ~1–1,7% sem anel nenhum. Por
  isso a conferência passou a contar só os aliados **conferíveis** (sem outro campeão a < 20 px, longe da fonte) e a oclusão
  ("coberto") passou de 12 para 20 px, contando também o lugar dos aliados do observador (desenhados sempre, achados ou não):
  com 12 px, um inimigo a 12–20 px de outro retrato e sem anel saía "fora" quando 10–33% das vezes o anel estava só embaixo.
  O motivo `aliados` caiu de 7,4% / 4,0% para 1,0% / 1,4% do tempo; `borda` de 2,1% para 0,2% (a geometria velha punha as
  posições perto demais da borda); `coberto` subiu de 4,3% / 7,6% para 9,0% / 13,3% (a Kai'Sa, sempre junto da Nami e na
  frente da dupla vermelha, ficou 28,5% indeterminada para o vermelho). Menos "fora" errado em troca de mais indeterminado.
- **Taxa** (2,25 capturas por segundo de jogo nos dois, não 4): o intervalo entre capturas foi de **454 ms de jogo** na
  mediana (10% abaixo de 399, 10% acima de 494) = **~57 ms reais por ciclo** a 8×, regular (não são paradas soltas). O que dá
  para pôr no ciclo sem o jogo: os dois `playback` ~9 ms (4.1), o PrintWindow da janela inteira ~20 ms (7), a conversão
  BGRA→RGBA da janela inteira + o recorte 3,3 ms (benchmark), a leitura do minimapa 2,8 ms (benchmark): ~35 ms; os ~20 ms que
  faltam não dá para medir sem o jogo (provavelmente a captura mais lenta com o jogo a 8×: em 4.1 a leitura a 8× já era 5 ms
  mais lenta que a 4×). A 8× o ciclo pedido é de 31 ms: não cabe nem sem a leitura. **O padrão passou para 4×**
  (`VelocidadeVisaoPadrao`; ciclo pedido de 62,5 ms, acima dos ~57 medidos: deve dar ~3,9–4 por segundo), o dobro do tempo do
  passe (~10 min por time numa partida de 41 min, em vez de ~5). O passe agora mede e loga no fim o ciclo e as partes
  (`playback`, captura, leitura) e manda no `visao_fim`.
- **Três casos conferidos no dado** (com o que ficou):
  1. **Gank do Master Yi na rota de baixo (abate na Yunara às 5:52)**, visão vermelha: de 5:30 a 5:40 ele está no rio/selva de
     baixo, a 1 500–2 100 da dupla vermelha e a ~4 000 da sentinela vermelha mais perto: **"fora"** em todas as capturas
     (nenhum anel azul a menos de 30 px dele). Às 5:41 aparece 1 s (anel a 1,7 px; a 1 100 da Poppy) e volta a "fora"; da
     aproximação ao abate (5:43–5:54) fica **indeterminado**: anel encostado na Poppy/Yunara (`coberto`), a Investida Alfa às 5:49
     (`sem_posicao`) e um anel azul sem dono do outro lado do mapa (`anel_sem_dono`). Coerente: estava fora antes de aparecer,
     e o mergulho em si não dá para decidir pelo minimapa.
  2. **Inimigo perto de sentinela**: a ≤ 600 unidades de uma sentinela do observador lida no minimapa, os inimigos ficaram
     visíveis em 100 de 128 segundos (visão azul) e 105 de 166 (vermelha); "fora" em 15 e 29 (8 deles no último segundo da
     sentinela: o inimigo chegou e ela sumiu, quase sempre limpa por ele), o resto indeterminado. "Fora" ao lado de uma
     sentinela pode ser legítimo (mato: a sentinela fora do mato não vê dentro), não conferido na tela.
  3. **Azir renascendo e teleportando (12:27–12:42)**, visão azul: na fonte vermelha, "fora" (12:28 e 12:30; 12:29
     indeterminado por um anel vermelho sem dono); durante o teleporte (12:31–12:36) indeterminado por `sem_posicao` (é o buraco
     `nao_selecionavel` da 8.1); ao chegar no meio (8 796, 8 891) às 12:37, **"visível"** (anel a 0,3–0,9 px) mesmo com o azul mais
     perto a 2 400 e a sentinela azul a 2 600 — provavelmente as tropas azuis da rota (não conferido).
- **NÃO verificado no jogo**: a taxa a 4× (a próxima passada loga o ciclo); se o atraso do minimapa é tempo real (a 4× seriam
  ~88 ms de jogo) ou de jogo; os ~20 ms do ciclo a 8× que não aparecem nas partes medidas; os casos 2 e 3 na tela.

### 9.2 Onde o indeterminado cai e o que dá para decidir (2026-11-18; partida 3305449546, só no DADO, sem o jogo)

Medido com o bruto dos dois passes (5 533 + 5 557 capturas) e as posições dos 10 passes; nada de jogo aberto.

- **Onde cai (antes)**: azul 10,7% do tempo dos inimigos (coberto 9,0, aliados 1,0, borda 0,2, anel sem dono 0,2, sem
  posição 0,2); vermelho 16,6% (coberto 13,3, aliados 1,4, sem posição 1,2 — 97 s do Master Yi —, anel sem dono 0,3, borda
  0,2, painel 0,2). Por distância ao aliado do observador mais perto: a menos de 600 unidades, 24% (azul) e **92%**
  (vermelho); 600–1 200, 16% / 35%; 2 000–3 000, 10% / 7%; acima de 3 000, 4–5%. Por distância (px) ao campeão mais perto:
  < 8 px 32% / 41%; 16–20 px 16% / 27%; ≥ 20 px 1–4%. **Em volta dos eventos**: −20 s a +5 s de cada abate (atacantes vistos
  pelo time da vítima) 31% (coberto 27,6%); separado, −20 a −5 s 24% e −5 a +5 s **42%**; trechos `lutando` 29,5%,
  `gankando` 14,6%, `dive` 13,5%.
- **Ordem de desenho**: os retratos do VERMELHO são desenhados por cima dos do azul, nas duas visões
  (`rastreio.TimeDesenhadoPorCima`). Visão azul: aliado azul a < 14 px de um vermelho à vista é achado em 10% (756
  capturas), o vermelho ao lado de um azul em 81%; visão vermelha: o vermelho ao lado de um azul à vista em 100% (72), o azul
  ao lado de um vermelho em 8% (917). Nas capturas de 2026-11-17 a Diana (azul) é a que sai "meio coberta" pela Orianna
  (vermelha). Dentro do mesmo time a ordem não é firme (o de número maior fica por cima em 63–97% dos pares). Consequência: um
  inimigo vermelho (visão azul) só pode ser escondido por outro vermelho; o azul (visão vermelha), por qualquer retrato. A
  decisão só usa a regra quando, perto do inimigo, só há aliados do observador (sem colega dele a < 20 px); o "fora" que sai
  só por ela e cai, curto, entre dois visíveis volta a indeterminado (25% dessas capturas estavam entre dois visíveis, a
  mesma taxa dos "fora" antigos perto de um aliado, 23%).
- **Fora do mapa** (`VisForaDoMapa`): trecho de 2+ amostras sem câmera com a trava perdida e a seleção no jogador (101 trechos do
  Master Yi de 1,0–1,6 s, a Investida Alfa; 13 teleportes de outros). Na visão azul, com o Yi nesses trechos, o número de anéis
  azuis = aliados vivos em 3% das capturas (29% com ele à vista): o anel dele some do minimapa. Vira estado próprio, fora da
  conta do indeterminado.
- **Continuidade** (`rastreio.inferirTrechos`, marcado `inferido`): indeterminado entre dois visíveis encostados, até 3 s, com
  aliado do observador a até 1 200 em todas as capturas (ou até 1 s sem conferir); entre dois "fora", até 3 s, a mais de 2 000
  de todo aliado do observador (nenhum sem posição) e a mais de 1 500 das sentinelas lidas. Nunca através de uma troca, nunca
  sem posição. Torres e tropas não entram (não são lidas): por isso é "inferido".
- **Arcos** (anéis parciais): o bruto até aqui só guardava os anéis com cobertura ≥ 0,62 (centro e cobertura), então nesta
  partida não há como atribuir os retratos encostados pelos arcos. A partir de agora o passe grava também os arcos (cobertura
  0,30–0,62) e, nos dois, a máscara das 64 direções em que o anel apareceu (5ª coluna do TSV, `:hex` na marca); a decisão dá
  "visível" a um inimigo sem anel inteiro quando há um arco a ≤ 6 px do lugar dele e ≥ 85% das direções que faltam caem
  embaixo de um retrato que pode estar por cima (`rastreio.faltaExplicada`). Testado só com imagem sintética (o desenho do jogo
  falso); nas 4 capturas de 2026-11-17 a leitura acha 0 arcos.
- **Depois** (refeito na cópia): azul 10,7% → **7,4%** indeterminado (2,8% herdado), vermelho 16,6% → **13,1%** (2,6% herdado; 1,0%
  fora do mapa). Em volta dos abates, −20 a −5 s 24% → 19%, −5 a +5 s 42% → 33%. O que sobra no vermelho é quase todo o azul
  embaixo de um vermelho perto da luta (coberto 11,5%): é o caso que os arcos vão resolver nos próximos passes.
- **Pergunta dos abates** (`analise.VisaoAntes`, 81 abates): nos 15 s antes, o matador sai "sim" em 77 e "não dá para saber" em
  4, antes e depois (atacantes: 168 sim, 3 não, 12 não dá); a janela de 15 s quase sempre pega o atacante já lutando. "Antes de
  chegar a 1 400 da vítima" (67 matadores com a chegada calculada): firme em 63 → 64 (48 sim, 15 → 16 não); todos os
  atacantes 133 → 136 de 141; caçador inimigo 39 → 41 de 43.
- **Não verificado no jogo**: a ordem de desenho fora desta partida (outra resolução, outro patch, campeão selecionado); se o
  Yi some mesmo do minimapa na Investida Alfa (só pela contagem de anéis); a leitura dos arcos em retratos de verdade (o anel
  do jogo tem contorno e brilho que o sintético não tem); o "fora" pela ordem de desenho em mato (o inimigo ao lado de um aliado
  e escondido).

## 10. Os passes ruins da 3305486988 e a pessoa usando o computador (2026-11-18; medido no DADO, não no jogo)

Rastreio 9, 4×, 9 passes. Os passes 1–3 (participantes 1, 2, 3) saíram bons: taxa 3,98–3,99, 0,3% sem trava, 1–5 teclas
duplas, Riot 252/252 a 270/270. Os passes 38, 39, 40 (participantes 4, 5, 6) fecharam **concluídos** com 66–78% das amostras
sem a câmera, taxa 2,18–2,35, 844–933 teclas duplas, 0 pontos de calibração; o 41 (participante 8) foi pausado no meio igual.
O dono estava usando o computador e viu o replay "aleatório, sem escolher personagem".

- **Desde a primeira amostra** do passe 38 (0:20 de jogo), e nos 39, 40 e no 41 até 20:36: nenhuma amostra com a câmera, e
  `selecao_ok` falso em **todas** as amostras sem trava (a seleção do replay nunca foi o jogador). A câmera (nas linhas de
  trabalho do 41, que guardam `cam_x/cam_z`) ficou parada em (8268, 4673) os primeiros 13 s e depois pulou pelo mapa —
  (7 400, 6 100) → (12 224, 982) → (2 705, 11 507) em poucos segundos: é a "Directed Camera" (seção 3), o que o dono viu.
- A cada minuto de jogo, ~28 buracos de ~1,3 s entre amostras (~37 s de 60): o motor parado em `ApertaDuplo` (~0,33 s real
  = 1,3 s de jogo a 4×). Isso é a taxa de 2,2: metade do tempo de jogo passou com o motor apertando tecla.
- O passe 41 **destravou sozinho** aos 20:36,6 de jogo (≈ 22:29:16 no relógio), na primeira tecla depois, e ficou travado até a
  pausa (22:29:52), sem nenhuma retravada. A última tecla que funcionou antes foi a da repescagem do passe 37 (~21:28,
  `selecao_ok` em todas as 15 amostras do trecho regravado); a primeira que falhou, a do começo do passe 38 (21:28:17).
- **Por que 900 tentativas sem desistir:** `passoRetrava` desistia depois de `MaxTentativasTrava` (5), mas o laço começava
  outra retravada 1 s depois (`LimiteSemTravaMs`), para sempre: 187 retravadas × 5 = 935 ≈ 933. E o passe fechava como
  concluído qualquer que fosse o dado; a repescagem ainda gastava 7–12 trechos nele.
- **O que o dado diz e o que não diz.** As teclas saíram (o `Foco` não falhou: falha de foco derruba o passe com erro, e
  nenhum passe falhou) e não fizeram efeito no jogo; não é a pessoa apertando 1–5/QWERT no replay (isso seguraria a câmera
  em outro jogador, não a deixaria dirigida e pulando). A hipótese que sobra é a de o jogo não receber a tecla enquanto a
  pessoa usava outra janela (o `SetForegroundWindow` com `AttachThreadInput` põe o jogo "na frente" mas o jogo não trata a
  entrada injetada; ou a entrada da pessoa e a do app se misturam), e de ter voltado a funcionar quando ela parou ou clicou no
  replay. **Não dá para ter certeza com o que foi gravado** (nada sobre a janela da frente, a ociosidade da pessoa nem o
  nome da seleção). A partir de agora cada passe grava `passes.diagnostico` (dados.md): quantas teclas saíram, quantas
  vezes o jogo não estava na frente, de que programa era a janela da frente antes de cada tecla, quantas foram recusadas
  porque a pessoa usava o computador, e o motivo de cada amostra viva sem a câmera (seleção vazia / em outro / fora da tela).
- **O que mudou** (código em `replay/presenca.go`, `rastreio/uso.go`, `rastreio/qualidade_passe.go`, `server/captura`):
  tecla nenhuma (nem o foco) com a pessoa usando o computador (entrada há < 3 s pelo `GetLastInputInfo`, descontadas as do
  app, ou modificador apertado): o trecho espera com o replay pausado; disjuntor que para o trecho e pausa a captura; passe
  ruim que não vale e é refeito uma vez; pausa/retomada da captura por atalho global, botão e bandeja. Pelo critério novo,
  no banco real só os passes 38, 39 e 40 (de 40 concluídos) são ruins; nenhum outro concluído passa de 5% sem trava ou fica
  abaixo de 3,6 de taxa.
- **NÃO verificado no jogo:** se o `GetLastInputInfo` conta o `SetCursorPos` do `Foco` (descontado de todo jeito); se a tecla
  volta a funcionar sozinha quando a pessoa para (o caso do 41 sugere que sim); se pausar e despausar o replay pela API mexe
  na seleção (a volta faz seek e retrava de todo jeito); o atalho global com o jogo em tela cheia exclusiva.
