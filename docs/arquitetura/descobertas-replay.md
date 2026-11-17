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
