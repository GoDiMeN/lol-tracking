# Dados (SQLite)

Arquivo: `server/dados/lol.db` (`LOL_DADOS`); no modo teste, `lol-teste.db` na mesma pasta, para os dados do LCU falso não se
misturarem com os de verdade. Cache dos nomes de campeões: `dados/campeoes.json` (Data Dragon, `pt_BR`).

Cada pacote é dono das suas tabelas: cria em `Migra(db)` com `CREATE ... IF NOT EXISTS` (coluna nova em tabela existente
entra com `banco.AdicionaColuna`) e é o único que escreve nelas. Não há `FOREIGN KEY` entre pacotes; a ordem das migrações
no `main.go` segue a dependência lógica. Datas em texto ISO 8601 UTC (`banco.Agora()`); `game_id` é o `gameId` do LCU.

## jogador

### `jogadores`
Quem já foi buscado.

| coluna | tipo | |
|---|---|---|
| `puuid` | TEXT PK | |
| `nome`, `tag` | TEXT | Riot ID mais recente (o puuid não muda; o nome pode) |
| `buscas` | INTEGER | quantas vezes foi buscado |
| `buscado_em` | TEXT | última busca |
| `ordem` | INTEGER | contador da última busca (desempata buscas no mesmo segundo) |
| `criado_em` | TEXT | |

## partida

### `partidas`
Uma linha por partida vista.

| coluna | tipo | |
|---|---|---|
| `game_id` | INTEGER PK | |
| `plataforma` | TEXT | `BR1` |
| `fila` | INTEGER | `queueId` (420 ranqueada solo...) |
| `modo`, `tipo` | TEXT | `gameMode` (`CLASSIC`, `ARAM`), `gameType` (`MATCHED_GAME`) |
| `mapa` | INTEGER | 11 = Summoner's Rift |
| `versao` | TEXT | `16.22.834.8910`; o patch (`16.22`) decide se o replay ainda abre |
| `duracao_s` | INTEGER | |
| `criada_em`, `criada_em_ms` | TEXT, INTEGER | `gameCreation` |
| `time_vencedor` | INTEGER | 100 (azul/ORDER) ou 200 (vermelho/CHAOS); 0 desconhecido |
| `completa` | INTEGER | 1 = veio de `games/{id}` com os 10; 0 = só o item do histórico (só o dono) |
| `estado_replay`, `progresso_replay`, `estado_replay_lcu`, `detalhe_replay`, `estado_replay_em` | | último estado de replay visto (cache para o cliente fechado) |
| `bruto` | TEXT | JSON inteiro do LCU (`games/{id}`) |
| `gravada_em`, `atualizada_em` | TEXT | |

### `participantes`
Os 10 de cada partida, com os dados finais. PK `(game_id, participante_id)`; índice em `(puuid, game_id)`.

| coluna | |
|---|---|
| `participante_id` | 1–5 time 100, 6–10 time 200, na ordem topo, selva, meio, atirador, suporte |
| `puuid`, `nome`, `tag` | identidade (`participantIdentities`) |
| `campeao_id`, `campeao` | id e nome (Data Dragon; `campeão 166` se offline) |
| `time` | 100/200 |
| `posicao` | `TOP JUNGLE MIDDLE BOTTOM UTILITY` (nomes do Live Client Data), **derivada da ordem dentro do time**; vazia fora do mapa 11 |
| `rota_lcu`, `papel_lcu` | `timeline.lane/role` crus do LCU. **Não confiáveis** (Malphite do topo veio `JUNGLE`) |
| `venceu` | 0/1 |
| `abates mortes assistencias` | |
| `cs`, `cs_rota`, `cs_selva` | `cs = totalMinionsKilled + neutralMinionsKilled` |
| `nivel ouro ouro_gasto` | |
| `dano_campeoes`, `dano_fisico_campeoes`, `dano_magico_campeoes`, `dano_verdadeiro_campeoes`, `dano_total`, `dano_recebido`, `dano_mitigado`, `dano_objetivos`, `dano_torres`, `cura_total` | |
| `tempo_cc` | `timeCCingOthers` (s) |
| `visao wards_colocadas wards_destruidas sentinelas_compradas` | |
| `torres inibidores` | abatidos pelo jogador |
| `maior_multiabate maior_sequencia duplos triplos quadras pentas primeiro_abate maior_tempo_vivo` | |
| `feitico1 feitico2` | ids dos feitiços |
| `itens` | JSON `[item0..item6]` (item6 = amuleto/trinket) |
| `runa_principal estilo_primario estilo_secundario` | `perk0`, `perkPrimaryStyle`, `perkSubStyle` |

O resto (runas completas, `playerScore*`, `bans`, objetivos por time) fica em `partidas.bruto`.

### `linhas_do_tempo`
`game-timelines/{id}`: quadros de 60 em 60 s com `participantFrames` (posição exata, ouro, XP, CS, nível) e `events`
(abates com posição, itens, wards, objetivos). Guardada no download do replay, ou na listagem quando o replay já estava baixado.
É a referência da calibração da posição (tarefa 2) e da fonte de eventos.

| coluna | tipo | |
|---|---|---|
| `game_id` | INTEGER PK | |
| `quadros` | INTEGER | quantos `frames` |
| `bruto` | TEXT | JSON inteiro (~100 KB) |
| `gravada_em` | TEXT | |

## rastreio

Um **rastreio** é uma fila de até 10 **passes** (um por jogador) de uma partida. Durante o passe as leituras vão para duas
tabelas **de trabalho** (`amostras_brutas`, `amostras_time_brutas`), que existem só para pausar e retomar. Ao fechar o passe
(`finalizaPasse`, concluído ou cancelado) elas viram, **na mesma transação**, o bloco compactado (`passes_compactos`) e a
cadeia reduzida (`cadeia`), e as linhas de trabalho do passe são apagadas. Decisão: `docs/decisoes/2026-11-17-bloco-compactado-e-cadeia.md`.

### `rastreios`

| coluna | tipo | |
|---|---|---|
| `id` | INTEGER PK | |
| `game_id` | INTEGER | |
| `estado` | TEXT | `rodando pausado concluido cancelado falhou` (na subida do servidor, `rodando` vira `pausado`) |
| `velocidade` | REAL | do replay (padrão 2) |
| `t_inicio_ms`, `t_fim_ms` | INTEGER | janela de jogo (0/0 = a partida inteira; a sonda usa trechos) |
| `criado_em`, `atualizado_em`, `terminado_em` | TEXT | |
| `erro` | TEXT | frase para a pessoa |

### `passes`

| coluna | |
|---|---|
| `id`, `rastreio_id`, `game_id`, `participante_id`, `ordem` | |
| `estado` | `pendente rodando pausado concluido cancelado falhou` |
| `t_atual_ms` | último tempo de jogo gravado: a retomada continua daqui |
| `duracao_ms` | duração do replay (`playback.length`) |
| `amostras`, `amostras_sem_trava` | quantas leituras; quantas com a trava perdida (jogador vivo sem a câmera nele) |
| `tentativas_trava`, `retravas` | teclas duplas usadas; quantas vezes a trava caiu e foi refeita |
| `taxa` | amostras por segundo de jogo (meta 4) |
| `conferencia_n`, `conferencia_iguais`, `conferencia_maior_dif`, `conferencia_riot` | conferência da leitura da tela com a linha do tempo da match-v5 da Riot (opcional, só com chave válida; nulos sem ela): nos quadros por minuto, amostra a ≤ 500 ms, viva e com o mesmo nível; máximos e atributos têm de bater exatos (velocidade de ataque ±0,01), vida e recurso atuais ±3% do máximo. `conferencia_riot` é o JSON por campo `{n, iguais, maior_dif, minutos, campos: {vida: {n, iguais, maior_dif}, ...}}` |
| `calibracao` | JSON `{x0, x_px, y0, y_px, cx, cy, largura, altura, modo, descartados}` da fórmula câmera → mapa (abaixo) |
| `calibracao_pontos`, `erro_calibracao_mediano`, `erro_calibracao_maximo` | o ajuste contra os quadros da linha do tempo (unidades do mapa) |
| `iniciado_em`, `terminado_em`, `erro` | |
| `repescagem` | JSON da última repescagem dos buracos do passe (vazio = nunca rodou): `{quando: "fim_do_passe"|"depois", buracos, recuperados, sobraram, restantes: [{de_ms, ate_ms, duracao_ms, amostras, motivo}], feita_em}`. Regras em "Buracos e repescagem" abaixo |

### `amostras_brutas` (trabalho)
Uma linha por leitura do jogador seguido (~4 por segundo de jogo). Colunas = as do bloco `amostras` abaixo, mais `passe_id`,
`game_id`, `participante_id`; posições e tela em REAL. Vazia para todo passe fechado (exceto durante a repescagem de um passe gravado, que carrega o bloco de volta nela; se o processo morrer no meio, a subida apaga essas linhas e o bloco antigo continua valendo).

### `amostras_time_brutas` (trabalho)
O estado dos 10 a cada ~1 s de jogo, pelo `playerlist`: `t_ms, participante_id, nivel, abates, mortes, assistencias, cs, ward,
morto, respawn, itens` (JSON `[{id,qtd}×7]` por slot) e `vida_pct, mana_pct` (tarefa 3: largura das barras dos quadros laterais, 0–100 com uma casa; `mana_pct` nulo quando a barra está vazia, porque não dá para distinguir 0 de mana de campeão sem recurso). Vazia para todo passe fechado.

### `passes_compactos`
PK `(passe_id, conteudo)`. `conteudo` = `amostras` ou `amostras_time`; `versao` (do formato), `n` (linhas), `t_ini_ms`,
`t_fim_ms`, `bytes`, `blob`, `gravado_em`.

### `cadeia`
`id, passe_id, game_id, participante_id, t_ms, tipo, dados` (JSON pequeno). Índices `(passe_id, t_ms)` e
`(game_id, participante_id, t_ms)`. Gerada do bloco por geradores registráveis (`rastreio.RegistraGerador`); refeita por
`rastreio.RegeraCadeia(db, passe_id)`. **Vocabulário aberto** (a tarefa 4 acrescenta tipos):

| tipo | dados | de onde |
|---|---|---|
| `ponto` | `x, y, origem` | trajeto simplificado por Douglas-Peucker (tolerância `ToleranciaTrajetoPadrao` = 150), por trecho vivo; os cortes (morte, renascimento, recall, buraco > 3 s) são sempre mantidos |
| `morte` | `x, y` (do evento), `matador`, `assistencias`, `respawn_s` | `isDead` passando a verdadeiro |
| `renascimento` | `x, y` | `isDead` passando a falso |
| `recall` | `x, y`, `de_x`, `de_y` | a definição única `rastreio.ChegadasPorRecall`: vivo, surge a < 1800 da fonte do time vindo de fora dela, a > 3000 de um ponto onde estava até 3 s antes (não precisa ser a amostra anterior: a câmera às vezes grava um ponto no meio do caminho), sem morte nesse meio e sem estar morto até `RenascerAtrasadoMs` (5 s) antes (é o renascer com a câmera atrasada) |
| `abate` / `assistencia` | `vitima`, `matador`, `x`, `y` | `CHAMPION_KILL` da linha do tempo dentro da janela do passe |
| `nivel` | `nivel` | subida de nível no `playerlist` |
| `compra` / `venda` | `item` | inventário segundo a segundo; `venda` = item que saiu (venda, consumo ou componente que virou item maior: a API não distingue) |
| `minuto` | `cs`, `cs_lcu`, `diferenca`, `ouro_total`, `ouro_atual`, `xp`, `nivel` | um por quadro da linha do tempo |
| `cs_divergente` | `cs`, `cs_lcu`, `diferenca` | o CS do jogo fora da dezena do CS da linha do tempo (com folga de 3): o `creepScore` do `playerlist` anda **de 10 em 10** |
| `trava_perdida` / `trava_retomada` | — | a câmera saiu do jogador vivo e voltou |
| `habilidade_usada` | `habilidade` (`q w e r d f`), `recarga_s` (a primeira recarga lida depois do uso), `x`, `y` | o ícone do painel passou de pronto (colorido) para em recarga (número), confirmado por 2 leituras seguidas com a recarga descendo; ou a recarga saltou mais de 3 s para cima (uso logo depois de voltar). Leitura nula não muda o estado; lacuna de mais de 3 s sem leitura esquece o estado; morto não conta. O uso real foi entre a última leitura pronta e esta (até 250 ms antes a 4 leituras/s) |
| `passiva_em_recarga` | `recarga_s`, `x`, `y` | o ícone da passiva passou de aceso para em recarga (mesma regra de `habilidade_usada`). O sentido depende do campeão: na Vex é o medo gasto, no Garen é ter levado dano. |
| `vida_baixa` | `vida`, `vida_max`, `pct`, `x`, `y` | vida do painel abaixo de 30% do máximo em 2 leituras seguidas, vindo de cima |
| `recuperou` | `vida`, `vida_max`, `pct`, `duracao_ms`, `x`, `y` | depois de `vida_baixa`, de volta a 50% em 2 leituras seguidas sem morrer no meio (morrendo, fica só a `morte`) |
| `mudou_zona` | `de`, `para`, `subzona`, `x`, `y` | tarefa 4 (`analise`): a zona confirmada mudou; só vale depois de `HistereseZonaMs` (3 s) na zona nova, e o item fica no instante em que ele entrou nela. Morte e renascimento não geram troca (há `morte`/`na_base`). Zonas: abaixo |
| `saiu_da_rota` | `rota`, `para` (primeira zona fora), `zonas` (as visitadas), `fim_ms`, `duracao_ms`, `fim_motivo` (`voltou` `morreu` `base` `fim_do_passe`), `fase` (`rotas` `meio` `final`), `fase_rotas` (bool, < 14 min), `vida_pct`, `mana_pct` (0,5 s antes de sair: painel, senão quadro lateral), `cs_perdido_est` (só na fase de rotas: ritmo de CS dos 2 min antes × duração; grosseiro), `aconteceu` (`[{tipo: abate\|assistencia\|morte\|objetivo, t_ms, evento, vitima?, matador?}]`), `desfecho` (`morte` > `abate` > `objetivo` > `nada`), `conta` (falso = foi andando para a base sem nada acontecer: não é saída "para jogar"), `x`, `y` | só rotador (posição TOP/MIDDLE/BOTTOM/UTILITY; BOTTOM e UTILITY = rota de baixo). Começa num trecho fora da rota logo depois de um trecho na rota; ≥ `SaidaMinimaMs` (6 s) fora, salvo se morreu |
| `voltou_para_rota` | `rota`, `inicio_ms`, `duracao_ms` | fim de uma saída que voltou para a rota |
| `entrou_em_rota` | `rota`, `trecho`, `fim_ms`, `duracao_ms`, `fase`, `fase_rotas`, `vida_pct`, `aconteceu`, `desfecho`, `x`, `y` | só caçador: ficou ≥ 6 s numa rota (gank/visita) |
| `na_base` | `fim_ms`, `duracao_ms`, `chegada` (`inicio` `renasceu` `recall` `andando`) | cada estadia viva na própria base; recall = uma chegada de `ChegadasPorRecall` até 40 amostras antes da entrada no trecho ou até 3 s depois dela; morto até 5 s antes = `renasceu` |
| `atividade` | `atividade`, `fim_ms`, `duracao_ms`, `com` (participantes), `zona`, `sinais` (até 6 frases: por que), `confianca` (`alta` `media` `baixa`), `alvo` (`objetivo`/`torre`: o objetivo; `dive`: a torre; `split_push` vindo de torre: a torre; `invadindo`: o acampamento, se limpando um), `farmando` (só `coletando_ouro`) | trechos de atividade DEDUZIDA (não é registro de golpe; `docs/decisoes/2026-11-17-atividade-deduzida.md`). Vocabulário e regra abaixo |
| `sentinela_colocada` | `tipo` (wardType cru da Riot), `tipo_nome`, `comum` (um dos 4 tipos conhecidos), `x`, `y`, `raio`, `zona`, `metade_inimiga` (sem posição, só os três primeiros) | `WARD_PLACED` da linha do tempo da Riot com `creatorId` = ele, dentro da janela do passe. A Riot não dá posição: `x`, `y` = onde ELE estava (amostras), `raio` = 600 (alcance de colocar) + 100 (erro), maior se a amostra mais perto passa de 1,5 s (+350 por segundo). Ver "Sentinelas" abaixo |
| `sentinela_destruida` | `tipo`, `tipo_nome`, `comum`, `x`, `y`, `raio`, `zona` (onde ele estava) | `WARD_KILL` com `killerId` = ele |
| `ficou_visivel` | `time_observador` (o time inimigo), `ate_ms`, `duracao_ms` (quanto ficou à vista), `x`, `y` | passe de visão do time inimigo (`visao.json`): de fora da visão para à vista. Indeterminado de até 2 s no meio não conta; "fora" de menos de 500 ms entre dois trechos à vista também não; um buraco sem captura (morto, fora do passe) zera |
| `sumiu_da_visao` | `time_observador`, `visivel_desde_ms`, `duracao_ms`, `x`, `y` | o contrário: de à vista para fora da visão |

**Atividade** (a primeira regra que couber, a cada amostra; trechos iguais seguidos se juntam; `andando`/`farmando`/`coletando_ouro`/`parado`/`invadindo`/`agrupado` de menos de 3 s
é absorvido pelo trecho anterior; trecho de `lutando`/`trocando`/`dive` de menos de 3 s grudado num `dive` fica no anterior, com os inimigos dos dois; **histerese** entre atividades vizinhas da mesma briga — `lutando`, `trocando`, `dive`, `torre`: trocar de uma para outra só vale se a nova durar ≥ `HistereseAtividadeMs` (4 s), senão fica na anterior com os envolvidos das duas; a entrada na primeira não espera). Ordem de prioridade final:

1. `morto` — morto no jogo (alta).
2. `na_base` — na própria base (alta).
3. `recall` — a canalização que terminou com ele surgindo na própria fonte (alta): uma chegada de `rastreio.ChegadasPorRecall` (a mesma do item `recall` e do `na_base`) e, antes dele, um trecho parado (sem sair mais que `RaioCanalizacao`, 200, do ponto de onde sumiu) de pelo menos `CanalizacaoMinimaMs` (3,5 s; com o Barão o recall leva 4 s). O trecho vai do começo da parada (no máximo `DuracaoRecallMs`, 8 s, + 0,5 s antes do salto: a canalização dura 8 s) até a chegada. Canalização interrompida (saiu andando, apanhou, não foi para a base) não tem salto e não vira recall. Partida 3304217746: Diana 27:46,8–27:55,1; Ambessa 11:17–11:26.
4. `dive` — o que seria `lutando` ou `trocando` (abaixo) com ele a ≤ `AlcanceTorre` (900: 750 de alcance a partir da borda da torre, mais o raio da torre e o do campeão) do centro de uma torre INIMIGA ainda de pé (`TorresDasRotas` + `TorresDoNexo`; a queda é o `BUILDING_KILL` casado pela posição, a até 1000). `com` = só os inimigos; `alvo` = a torre (`torre externa do topo`...). A tela: "dive em Garen".
5. `lutando` — com um inimigo **rastreado** a ≤ `AlcanceLuta` (1400) e mais alguém (2 inimigos, ou um aliado rastreado perto), e a vida de alguém caindo ≥ 4 pontos percentuais em 2 s (do seguido pelo painel; dos outros pelo quadro lateral). `com` = os envolvidos.
6. `trocando` — o inimigo rastreado mais perto a ≤ `AlcanceTroca` (1000) e a vida de um dos dois caindo (alta com o painel lido). Também, com `com: []` e confiança baixa: rotador fora da rota com a própria vida caindo ≥ 8 pontos em 2 s sem inimigo rastreado perto (quem bateu não foi rastreado; esse nunca vira `dive`).
7. `objetivo` / `torre` — a ≤ 1500 de onde caiu um monstro grande (até 40 s depois) ou uma torre/placa (até 20 s depois), derrubado pelo time dele (ou com ele). Placas vêm da linha do tempo da Riot, se guardada. A `torre` pode virar `split_push` (regra 13).
8. `agrupado` — ele e pelo menos três aliados vivos e rastreados, todos a ≤ `DistanciaAgrupado` (2000) uns dos outros (dois a dois), sem luta nem objetivo (essas ganham). `com` = os aliados do maior grupo (média).
9. `invadindo` — na selva do time inimigo (`selva_vermelha_*` para o azul e vice-versa), sem luta. Limpando um acampamento de lá (a regra 10 com o acampamento na selva inimiga): `alvo` = o acampamento, e a tela diz "invadindo (lobos (lado azul))". Sem acampamento: andando, parado ou farmando por lá. Atravessar a selva inimiga a caminho de uma luta continua sendo aproximação (abaixo).
10. `farmando` — a ≤ `RaioAcampamento` (800) do centro de um acampamento da própria selva ou do rio (`analise/acampamentos.go`), sem inimigo rastreado perto e com uma habilidade ou feitiço usado até 3 s antes ou depois (média). Vem antes do deslocamento: o caçador limpa em segundos e já sai andando.
11. `andando` — deslocou mais de 1000 em linha reta nos últimos 4 s (média).
12. `coletando_ouro` — só para quem tem o item de suporte no inventário naquele instante (`ItensDeSuporte`: 3865 e evoluções; vendeu, deixa de valer): na rota, selva ou rio, com um aliado rastreado a ≤ 1000 que está farmando naquele momento (parado, sem a vida cair, e com o CS da linha do tempo subindo ≥ 3/min no minuto) (média; `com` = o aliado). Campo `farmando` (bool, só nesta atividade): verdadeiro quando o CS do PRÓPRIO suporte também sobe ≥ `CSMinimoPorMin` (3/min) naquele minuto (a mesma regra do `farmando`); a tela diz "coletando ouro e farmando". Não é outra atividade: o trecho muda quando o campo muda.
13. `farmando` — na rota, selva ou rio, com o CS da linha do tempo subindo ≥ 3/min naquele minuto (média; baixa se ninguém mais foi rastreado). **`split_push`**: o `farmando` desta regra ou a `torre` da regra 7, depois do fim da fase de rotas, numa rota LATERAL (topo ou baixo), além do ponto de encontro das torres externas (onde o rio cruza a rota) para o lado inimigo, sozinho (nenhum aliado a menos de `AlcanceLuta`) e com pelo menos três aliados mortos ou a mais de `DistanciaSplitAliados` (5000). Aliado sem posição não conta como longe.
14. `parado` — nenhum outro sinal e deslocou ≤ `DeslocamentoParado` (250) nos últimos 4 s (baixa).
15. `andando` — nenhum outro sinal (baixa).

Depois de montados os trechos (`rotulaAproximacoes`): o deslocamento (trechos `andando` e `invadindo` sem acampamento, seguidos) que termina numa luta, troca ou dive com inimigo vira a aproximação, até 25 s antes dela e só se nesse tempo o jogador percorreu ≥ 1500 até o lugar da luta: `gankando` para o caçador chegando numa rota, `roaming` para quem é de rota e está fora da própria rota (`com` = os inimigos da luta), `rotacionando` para qualquer um depois do fim da fase de rotas (primeira torre, no máximo 14 min; `GET /api/partidas/{id}/fases`). No `gankando`, `com` = só os inimigos de ROTA da luta (o caçador inimigo que chegou junto sai da lista; se não houver nenhum de rota, ficam todos). **`counter_gank`**: se um caçador inimigo da mesma luta também veio andando de longe (percorreu ≥ 1500 nos 25 s antes da luta) e chegou ao primeiro contato DEPOIS do outro, ele é o counter gank (`com` = o caçador que gankou). Contato = o primeiro instante, de 25 s antes da luta até 5 s depois (`JanelaContatoDepoisMs`), a ≤ `AlcanceTroca` (1000) de um inimigo que está na zona da luta, pelas posições rastreadas (o contexto de um passe não tem as etiquetas dos outros); diferença menor que `MargemCounterMs` (0,5 s) = chegaram juntos, os dois ficam `gankando`. Só caçador e só na fase de rotas: quem é de rota e chega para responder continua `roaming`. Partida 3304217746: a Vi encosta na Diana aos 3:56,6 e o Shaco na Vi aos 4:02,4 → Vi `gankando` Diana, Shaco `counter_gank` em Vi. `objetivo` e `torre` levam `alvo` (ex.: `dragão infernal`, `torre do meio`, `placa da torre de baixo`).

**Sentinelas** (`server/analise/sentinelas.go`; `GET /api/partidas/{id}/sentinelas`, itens `sentinela_colocada`/`sentinela_destruida`, indicadores `sentinelas_*`). Só com a linha do tempo da Riot guardada (sem ela: nada, e a tela diz o motivo). Posição ESTIMADA como no item da cadeia (quem não foi rastreado: só instante e contagem). Vida (League of Legends Wiki, "Ward", conferida em 17/11/2026): `YELLOW_TRINKET` (amarela, Totem) 90–120 s pela média dos níveis dos 10 no último quadro de minuto (a reta entre os dois extremos é estimativa: `aproximado`); `SIGHT_WARD` (do item de suporte) 150 s; `CONTROL_WARD` e `BLUE_TRINKET` até serem destruídas. Limites por jogador: 3 entre amarela e do item (a quarta derruba a mais velha: `substituida`), 1 de controle. Um `WARD_KILL` derruba a sentinela viva do mesmo tipo, do outro time, mais perto de onde quem destruiu estava (a até o raio dela + 800); sem posição de quem destruiu, só se houver uma candidata (`destruida`, `aproximado`). De controle/azul sem destruição casada: `pode_estar_ativa` (`ate_ms` nulo). Outro `wardType` (`UNDEFINED`, bonecos de campeão) aparece com o tipo cru, `comum: false`, vida `desconhecido`. Conferência na 3304217746: a pontuação de visão do dono (campo `ward` das amostras) NÃO muda quando a sentinela é colocada; ela sobe quando a sentinela ACABA — a até ~2 s do `ate_ms` estimado em 58 de 62 amarelas que expiraram e 14 de 16 do item, e a até 0,3 s em todas as 30 destruídas e nas 12 substituídas.

**Zonas** (`analise.Zona(x, y)`, fronteiras em `server/analise/fronteiras.go`, conferência em `docs/arquitetura/zonas.png`):
`base_azul`/`base_vermelha` (subzona `fonte` ou `base`), `rota_topo`/`rota_meio`/`rota_baixo` (subzona `lado_azul`, `centro`,
`lado_vermelho`: divididas pelas torres externas), `rio_cima`/`rio_baixo` (`rio`), `cova_barao`/`cova_dragao` (`cova` a ≤ 750 do
monstro, `arredores` a ≤ 1300), `selva_azul_cima`, `selva_azul_baixo`, `selva_vermelha_cima`, `selva_vermelha_baixo` (`selva`),
`fora_do_mapa`. Ordem de decisão: base, rota (a mais perto), cova, rio, selva (pelo lado do rio e da diagonal do meio).
Do ponto de vista de um jogador (`GrupoRelativo`): `rota_propria`, `outra_rota`, `rio`, `covas`, `selva_propria`,
`selva_inimiga`, `base_propria`, `base_inimiga`.

**Ondas de tropas** (`server/analise/ondas.go`, `ondas_relogio.go`; nenhuma tropa é rastreada: tudo sai do que já está gravado).
O **relógio** numera as ondas a partir da saída das tropas (evento `MinionsSpawning` do jogo em `eventos`; sem ele, 0:30, a regra
do patch 26.x): de 30 em 30 s até 14:00, de 25 em 25 s até 30:00, de 20 em 20 s depois; canhão na 3ª onda e a cada 3 ondas até
14 min, a cada 2 até 25 min, toda onda depois (`RegrasOndas`, com a fonte no comentário). A chegada ao ponto de encontro de cada
rota sai da velocidade das tropas (350; nas laterais com o bônus da 2ª onda até 14:00) e de uma distância ESTIMADA. O **eixo**
de cada rota (`EixoRota`) é a coordenada `s` da base azul (0) à vermelha, sobre `RotaTopo` (topo), o espelho dela (baixo) e
`RotaMeio`; as torres (`TorresDasRotas`) ficam no eixo. A **posição da onda** em cada segundo é a dos jogadores RASTREADOS da
rota (BOTTOM e UTILITY = baixo), vivos, na zona da rota, parados (sem andar > 1000 em 4 s) e fora de luta (sem abate a < 2000 em
± 8 s, sem inimigo de fora da rota a < 1400): com os dois lados, o meio entre eles (confiança `alta`); com um lado só, o jogador
mais `MeiaDistanciaOnda` (500) para o lado do outro (`media`); sem ninguém, desconhecida. Mediana em 10 s. Depois do fim da fase
de rotas só conta quem está farmando (CS ≥ 3/min no minuto), e nunca depois dos 14 min. Cada **janela de onda** vai da chegada de
uma onda à da seguinte; a posição dela é a mediana (com ≥ 30% da janela coberta). **Estados** (itens da cadeia de cada jogador
da rota; `time` 100/200, `a_favor` do ponto de vista dele):

| tipo | quando | `time` |
|---|---|---|
| `onda_na_torre` | onda a ≤ `AlcanceTorreOnda` (1000) da torre de pé mais avançada de um time, ou placa/torre daquela rota caindo na janela | dono da torre |
| `onda_voltando` | logo depois de `onda_na_torre` (ou já voltando), a onda se afasta daquela torre ≥ 450 por onda | dono da torre |
| `empurrando` | a onda anda ≥ `AvancoEmpurrando` (450) por onda; `velocidade` `lenta` ou `rapida` (≥ `AvancoRapido`, 1200), `avanco_por_onda` | quem empurra |
| `freeze` | ≥ `OndasFreeze` (2) ondas seguidas numa faixa de ≤ `FaixaFreeze` (900), andando ≤ meia faixa, fora do alcance da torre, a ≥ `DeslocamentoFreeze` (400) do ponto de encontro para o lado de um time, com um rastreado de rota desse time presente e farmando (CS ≥ 3/min no trecho) | quem segura (o da torre perto) |
| `onda_parada` | parada como acima, mas no meio da rota ou sem o jogador daquele lado farmando; ou uma janela solta que andou < 450 | lado de que está mais perto (0 = meio) |

Dados dos estados: `rota`, `fim_ms`, `duracao_ms`, `ondas` (números), `time`, `a_favor`, `confianca`, `s`, `x`, `y`, `sinais`,
`fase_rotas`, e `velocidade`/`avanco_por_onda` nos que andam. **`onda_perdida`** (só para quem é de rota, não o suporte, e só na
fase de rotas): a onda chegou ao ponto de encontro e o jogador não passou a ≤ `RaioPresencaOnda` (1800) dela, vivo e na rota, em
`JanelaPresencaOndaMs` (25 s); não conta se a onda estava batendo na torre do inimigo (sair depois do crash é o certo). Dados:
`rota`, `onda`, `canhao`, `motivo` (`morto`, `na_base`, `longe`), `zona`, `distancia` (da onda), `onda_s`, `posicao_onda`
(`estimada` ou `encontro` = sem estimativa, o ponto de encontro natural), `x`, `y`. **Limites**: sem contar tropas não se sabe o
tamanho da onda ("devagar/rápido" é só a velocidade do avanço); depois da fase de rotas a estimativa perde sentido.

### Visão dos times: `passes_visao`, `visao_capturas_brutas` e os arquivos

Dois **passes de visão** por partida (um por time observador), sem seguir ninguém: a névoa do replay na visão do time (F1
azul, F2 vermelho, conferida pelo painel "Fog of War"), o replay a 8× e uma captura do painel + minimapa a cada 250 ms de jogo
(`rastreio/visao_passe.go`; medidas e geometria em `descobertas-replay.md`, seção 9).

`passes_visao`: `id`, `game_id`, `rastreio_id` (0 = avulso, pela rota da visão; > 0 = pedido junto com o rastreio, roda no
fim dele), `time` (observador 100/200), `ordem`, `estado` (`pendente rodando pausado concluido cancelado falhou`; na subida do
servidor `rodando` vira `pausado`), `velocidade` (padrão 8), `t_inicio_ms`, `t_fim_ms` (0 = fim), `t_atual_ms` (a retomada
continua daqui), `duracao_ms`, `capturas`, `capturas_modo_ok` (o painel dizia a névoa do time), `taxa` (capturas por segundo de
jogo), `teclas` (F1/F2 e o F3 do fim), `iniciado_em`, `terminado_em`, `erro`.

`visao_capturas_brutas` (trabalho, só durante o passe): `passe_id`, `t_ms` (média dos dois `playback` em volta da captura),
`modo` (`azul vermelho todos` ou vazio = o painel não foi lido), `retratos`, `sentinelas` (marcas `time:x:y:cobertura`
separadas por `;`, posição já no mapa). Captura em que a leitura do minimapa falhou não entra (o trecho fica sem cobertura).

Arquivos (pacote `arquivo`, em `dados/partidas/<id>/`):

| arquivo | conteúdo |
|---|---|
| `visao-p<passe>-capturas.tsv.zst` | as capturas lidas do passe, `t_ms \t modo \t retratos \t sentinelas`. É o dado bruto (não depende das posições). Passe **concluído** sempre grava e apaga o arquivo dos passes de visão anteriores do mesmo time; **cancelado** só grava se o time ainda não tinha nenhum. O passe que vale para um time é o concluído mais recente com arquivo (senão o cancelado mais recente com arquivo). |
| `visao.json.zst` | `VisaoDaPartida` (contrato): para cada time observador, por inimigo, os intervalos `visivel` / `fora` / `indeterminado` e as sentinelas lidas. **Derivado**: `rastreio.RecalculaVisao` refaz no fim de cada passe de visão e no começo de cada `analise.Recalcula` (passe de jogador novo ou repescado muda as posições). Sem passe de visão que valha, o arquivo some. |

**A decisão por captura** (`rastreio.ClassificaCaptura`; tolerâncias em pixels do minimapa, 1 px ≈ 44 unidades):
- painel sem a névoa do time → os inimigos ficam `indeterminado` (motivo `painel`);
- menos da metade dos aliados vivos rastreados com o retrato achado no lugar (com um aliado só: ele não achado; no próprio
  time o retrato aparece sempre) → todos `indeterminado` (`aliados`: minimapa coberto, geometria, instante ou calibração do passe
  errados — sem isso, um inimigo com a posição rastreada deslocada sairia "fora"). Com menos de 80% de aliados achados no passe
  inteiro, o resultado do time sai com `aviso`;
- inimigo morto não conta (fica fora das listas); inimigo sem posição rastreada boa no instante (não rastreado, buraco,
  posição interpolada) → `indeterminado` (`sem_posicao`);
- anel colorido do time inimigo a ≤ `ToleranciaRetratoPx` (6 px) da posição rastreada → `visivel` (cada anel casa com um só,
  do par mais perto ao mais longe);
- sem anel no lugar: `indeterminado` se sobra anel inimigo sem dono e todos os inimigos vivos têm posição (o anel é de um deles,
  fora do lugar: `anel_sem_dono`), ou se um anel sem dono está a < 25 px dele (`anel_sem_dono`), ou se outro retrato qualquer está
  a < `OclusaoPx` (12 px) do lugar dele (`coberto`: o de cima esconde o de baixo), ou se ele está a < 8 px da borda do minimapa
  (`borda`); senão `fora`.

Cada captura vale da metade do caminho até a anterior à metade do caminho até a seguinte (no máximo 500 ms para cada lado). A
posição rastreada no instante: entre duas amostras boas (câmera, base ou morte, sem trava perdida) a até 1 s uma da outra, a reta
entre elas; senão a boa mais perto a até 300 ms. Morto: a amostra mais perto (1,5 s); sem amostra, o estado dos 10.

**Sentinelas lidas**: o marcador do time observador, mesma sentinela = a até 3 px da média das anteriores, vista de novo em até
3 s (enquanto um retrato está em cima do lugar dela o prazo não corre); vale com ≥ 3 capturas. `de_ms`/`ate_ms` = primeira e
última captura em que apareceu (com retrato em cima no começo ou no fim, sai atrasado/adiantado). Só o marcador "olho com asas"
medido; outros marcadores (sentinela de controle, de outro tipo) não foram medidos e não são lidos.

### `eventos`
Os eventos da partida, das duas fontes: `linha_do_tempo` (importada ao criar o rastreio) e `jogo` (`eventdata`, coletado no
fim de cada passe — só o que passou desde o último seek). `tipo` normalizado (`abate monstro estrutura multiabate
primeiro_abate ace inicio fim` ou o original em minúsculas), `tipo_original`, `t_ms`, `quem_id`/`quem_nome`,
`em_quem_id`/`em_quem_nome`, `assistencias` (JSON de ids), `x`, `y` (só a linha do tempo tem), `confirmado_jogo`, `bruto`.
**Sem duplicar:** `abate`, `monstro` e `estrutura` do jogo que a linha do tempo já tem (mesmo tipo a ±3 s; no abate, mesma
vítima) não viram linha: a da linha do tempo fica com `confirmado_jogo = 1`. `UNIQUE (game_id, fonte, tipo_original, t_ms,
quem_nome, em_quem_nome)` torna a importação idempotente.

### A posição (fórmula da câmera)

`x = cam_x + x0 + x_px·(sx − cx)` e `y = cam_z + y0 − y_px·(sy − cy)`, com `(sx, sy)` = `screenPositionCenter` do jogador.
Partida da seção 3.2 das descobertas (`0,2 / 1,374 / 1249,6 / 1,768` a 2560×1440, por-pixel escalado pela tela) e refeita
**por passe** contra os quadros por minuto da linha do tempo (exclui quadro com o jogador morto a ±3 s): com 8 pontos ou
mais, os quatro coeficientes; com menos, só `x0`/`y0`. Ponto acima de 3× a mediana e de 250 é descartado e o ajuste refeito.

`origem` da amostra: `camera` (a fórmula), `morte` (morto: local do evento de morte), `base` (renasceu há < 2 s e a câmera
ainda não voltou: a fonte do time, ORDER (404, 571), CHAOS (14340, 14507)), `interpolada` (vivo sem a trava: entre as
vizinhas com posição, até `BuracoCurtoMs`/2 = 10 s de cada lado, e só se `PodeInterpolar`, senão fica a posição da mais próxima; `trava_perdida = 1`). Enquanto o motor tenta retravar ele continua gravando: a amostra do jogador sai com `trava_perdida = 1` e sem posição no trabalho (o estado dos 10 segue normal).

#### Buracos e repescagem

**Buraco** (`rastreio.Buracos`, a mesma regra para a checagem de qualidade e para a repescagem): trecho com o jogador vivo entre duas **leituras boas** (origem `camera`, `morte` ou `base`, sem `trava_perdida`) mais longe que o limiar, haja amostras sem posição no meio ou nenhuma. Morto nas duas pontas não é buraco. **Repescagem**: ao fim de cada passe, com o replay ainda aberto, cada buraco > `LimiarRepescagemMs` (3 s) é refeito: seek para `de − FolgaRepescagemMs (2 s) − RecuoRetomadaMs (4 s)`, trava, grava de `de − 2 s` a `ate + 2 s` e, se o trecho novo não tem buraco dentro do velho, troca as linhas de trabalho daquele intervalo (amostras e estado dos 10). No máximo `MaxRepescagensPorPasse` (8) buracos por passe e `TentativasExtrasRepescagem` (1) tentativa a mais por buraco; o que não fecha fica em `restantes` com o motivo. A repescagem de passe gravado (`POST .../rastreio/repescagem`) faz o mesmo com o bloco carregado de volta no trabalho e fecha o passe de novo com a calibração gravada (as amostras velhas não têm as colunas da câmera).

**`PodeInterpolar(a, b)`** (posição em linha reta entre duas leituras): os dois vivos, o placar de mortes igual (não morreu no meio), sem chegada à fonte vinda de fora dela (volta à base) e distância ≤ `VelocidadeMaxInterpolacao` (700/s) × intervalo + `FolgaSaltoInterpolacao` (1000; um Flash e um avanço). Teleporte saindo da base cai aqui (Orianna 2:29 e Garen 13:06 da 3304217746: 8 600–9 500 unidades em 4–7 s).

### Formato do bloco compactado (versão 1)

Um blob por conteúdo: `"LTC"` + byte do compressor (`1` = DEFLATE, `compress/flate` nível 9) + o corpo comprimido.

```
corpo   = uvarint versão (1), uvarint n_linhas, uvarint n_colunas, coluna × n_colunas
coluna  = texto nome
          byte tipo            0 inteiro, 1 texto
          uvarint escala       inteiro: valor real = inteiro / escala (1, 10, 100); texto: 0
          [texto: uvarint n_dicionário, texto × n]   o valor da linha é o índice no dicionário
          byte nulos           0 nenhum, 1 todos (fim da coluna), 2 corridas
          [corridas: uvarint n, uvarint × n]  comprimentos alternados, começando por "não nulo"
          byte codificação     0 delta, 1 corridas (valor, repetições), 2 delta² (delta do delta)
          uvarint tamanho, dados   só os valores não nulos, zigzag + varint
texto   = uvarint tamanho + bytes UTF-8
```

A codificação de cada coluna é a menor das três, escolhida na hora. A coluna é achada **pelo nome**: coluna nova (a tarefa 3
acrescenta as da tela) não muda a versão nem quebra blob antigo — quem lê um blob sem ela recebe tudo nulo. Mudar a
codificação de algo que já existe muda a versão.

Colunas de `amostras` (ordem de tempo): `t_ms`, `x`, `y` (inteiro), `origem` (texto), `cam_x`, `cam_z` (inteiro),
`tela_x`, `tela_y` (÷10, décimo de pixel), `selecao_ok`, `trava_perdida`, `morto` (0/1), `respawn` (÷100), `nivel`, `cs`,
`abates`, `mortes`, `assistencias`, `ward` (÷100), `dt_ms` (janela entre os dois `playback` da leitura), `vida`, `vida_max`,
`mana`, `mana_max`, `cd_q` `cd_w` `cd_e` `cd_r` `cd_d` `cd_f` `cd_p` (÷10, segundos; `cd_p` é a passiva, nula em blob anterior a ela), `atr_ad`, `atr_ap`, `atr_armadura`, `atr_rm`,
`atr_vel_ataque` (÷100), `atr_vel_mov`. As da tela (tarefa 3, `server/visao`) são nulas quando não lidas com confiança, com a câmera fora do jogador, com ele morto ou quando o nível do painel não é o do `playerlist` (painel de outro jogador); `vida_max`, `mana_max` e atributos só entram depois de 2 leituras iguais (ou mudança pequena) — `rastreio/sanidade.go`. `mana`/`mana_max` é o recurso da barra azul do painel (mana, energia, fúria...: o que a barra mostrar em texto); nulo se não há texto (campeão sem recurso). Nas recargas: `0` = pronta, `-1` = habilidade não aprendida (nível 0), nulo = não lida (inclusive ícone escurecido sem número: bloqueado por outra ação). Blob anterior à tarefa 3 lê tudo isso como nulo.

Colunas de `amostras_time` (ordenado por participante e tempo, para as corridas ficarem longas): `participante_id`, `t_ms`,
`nivel`, `abates`, `mortes`, `assistencias`, `cs`, `ward` (÷100), `morto`, `respawn` (÷100), `vida_pct`, `mana_pct` (÷10),
`item0`…`item6` e `qtd0`…`qtd6` (por slot; 0 = vazio).

Tamanho medido (sonda de 2026-11-17, partida real): ~7,2 bytes por amostra no bloco `amostras` (6 919 bytes para 956
amostras) e ~1,3 KB por 4 min do estado dos 10. Uma partida de 39 min × 10 passes ≈ 9 360 × 10 amostras ≈ **0,7 MB** de
amostras + ~0,1 MB de estado dos 10 (um por passe). O teste `TestTamanhoDoBloco` mede o mesmo com dados sintéticos
ruidosos (7,9 bytes/amostra).

## analise

Tudo aqui é **recalculável**: `analise.Recalcula(partida)` (chamado sozinho quando um passe fecha, e por
`POST /api/partidas/{id}/analise`) refaz a cadeia de todos os passes da partida (com o contexto de agora: os outros rastreados,
a linha do tempo da Riot), e apaga e regrava retratos e indicadores. Rodar de novo dá o mesmo resultado (teste
`TestRecalculaIdempotenteERetrato`). A exceção é `linhas_do_tempo_riot`, cópia do que a Riot devolveu (não muda).

### `linhas_do_tempo_riot`
`game_id` PK, `bruto` (JSON da match-v5 timeline, ~1 MB), `gravada_em`. Buscada uma vez, na primeira análise com chave válida;
sem chave a análise segue sem ela (sem dano por abate, placas de torre, sentinelas e dano a campeões por minuto).

### `retratos_abate`
Um por `CHAMPION_KILL` da linha do tempo do LCU. PK `(game_id, n)` (n = 1, 2... na ordem do tempo), `t_ms`, `vitima`, `matador`,
`dados` (o `Retrato` inteiro em JSON, ver `contrato-http.md`), `calculado_em`.

Por jogador (os 10): posição (`origem`: `rastreado` = amostra do passe dele; `evento` = o local do abate, para a vítima; `morte` =
estava morto, onde morreu; `perto_do_abate` = não rastreado mas matou/ajudou/morreu num abate a ≤ 8 s, posição desse abate;
`linha_do_tempo` = interpolada entre os quadros de minuto — **aproximada**, pode errar milhares de unidades), zona, distância até a
vítima, vivo (estado dos 10), `vida_pct`/`mana_pct` (quadro lateral), nível, itens; dos rastreados também vida exata, recargas,
`r_aprendido`, `r_disponivel`, `flash_disponivel` (o slot do Flash pelo `feitico1/2` = 4) e `fora_da_rota_ms`. Aliados e inimigos a
≤ `RaioSuperioridade` (1500) de cada um. `riot` (com a linha do tempo da Riot): `victimDamageReceived/Dealt` (quem bateu, com qual
habilidade — `spellName`/`spellSlot`, ou ataque básico — e quanto), `por_jogador`, `ouro`, `sequencia`.

### `indicadores`
PK `(game_id, participante_id, indicador)`; `valor` (nulo = não deu para calcular), `aproximado` (rastreio parcial — cobertura
< 90% da janela — ou posição aproximada), `detalhe` (JSON: `momentos` `[{t_ms, texto}]` quando o indicador é contagem de coisas,
`cobertura_ms`, etc.), `calculado_em`. Índice `(indicador, game_id)`.

Vocabulário (`analise.Vocabulario`, também em `GET /api/indicadores` com rótulo, unidade, fonte e direção "melhor"):
`saidas_rota_ate14_n`, `saidas_rota_ate14_tempo_s`, `cs_perdido_saidas_est`, `mortes_fora_da_rota`, `mortes_saida_vida_baixa`,
`freeze_a_favor_s`, `freeze_contra_s`, `ondas_na_torre_inimiga_n`, `ondas_perdidas_rotas_n` (este não vale para o suporte; os
quatro das ondas são sempre aproximados e só da fase de rotas) (rotador rastreado); `visitas_rota_ate14_n`, `visitas_rota_ate14_com_abate` (caçador rastreado); `mortes`, `mortes_ate14`,
`abates`, `assistencias`, `participacao_abates`, `cs_10`, `cs_15`, `cs_por_min`, `ouro_10/15`, `xp_10/15`, `dif_cs_10/15`,
`dif_ouro_10/15`, `dif_xp_10/15` (contra o adversário direto: mesma posição no outro time) — linha do tempo, os 10;
`dano_campeoes_10/15`, `wards_ate14`, `sentinelas_rotas`, `sentinelas_meio`, `sentinelas_final` (marcos fixos de 14 e 25 min), `sentinelas_controle`, `sentinelas_destruidas` — Riot, os 10; `sentinelas_metade_inimiga` (pela posição estimada; sempre aproximado) — Riot, rastreados; `mortes_com_r_em_recarga`, `mortes_com_flash_em_recarga`, `mortes_com_passiva_em_recarga`, `abates_com_passiva_em_recarga` (os dois da passiva só existem se ela entrou em recarga alguma vez no passe) (só mortes com a
recarga lida), `dist_aliado_mais_proximo_morte`, `mortes_em_inferioridade` — retrato; `tempo_morto_s`, `tempo_vida_baixa_s`,
`cs_divergencias` — estado dos 10 no trecho rastreado; `habilidades_por_min`, `recalls` — rastreados;
`pct_visivel_inimigo_<fase>` (tempo à vista do time inimigo ÷ (à vista + fora), sem o indeterminado; aproximado com mais de 10% indeterminado; fase com ≥ 1 min decidido) e `ganks_fora_da_visao_n` (caçador: trechos `gankando` que começaram com ele fora da visão do inimigo; o detalhe traz os vistos e os indeterminados) — passe de visão, rastreados (`analise/visao.go`);
`pct_<grupo>_<fase>` (grupo relativo de zona: `rota_propria outra_rota rio covas selva_propria selva_inimiga base_propria
base_inimiga`; fase `rotas` < 14 min, `meio` 14–25, `final`) e `pct_atv_<atividade>_<fase>` — rastreados, só fase com ≥ 1 min
rastreado.

### `analises`
Uma linha por partida analisada: `game_id` PK, `calculada_em`, `rastreados` (JSON), `passes`, `riot` (0/1), `abates`,
`indicadores`, `avisos` (JSON), `ms`.

Parâmetros das regras (limiares) em `server/analise/parametros.go` (`analise.Param`, também em `GET /api/indicadores`).

## Qualidade do dado (`analise/qualidade.go`)

### `qualidade_passes`
PK `passe_id`; `game_id`, `participante_id`, `contagem` (JSON `{tipo: n}`), `pontos` (JSON, no máximo 50:
`[{tipo, t_ms, texto}]`, `t_ms` 0 = o passe inteiro), `total`, `calculada_em`. Refeita em cada `Recalcula` (fim de passe e
`POST .../analise`). Tipos e limiares (medidos na 3304217746):

| tipo | quando |
|---|---|
| `salto` | duas amostras vivas com mais de 1 500 unidades E mais de 1 200 unidades/s entre elas, nenhuma na base (raio da base de `fronteiras.go`) nem com origem `base`/`morte`. Avanços, Flash e o R da Diana/Vi ficam abaixo de 1 500. |
| `buraco` | mais de 3 s (`BuracoMs`) com o jogador vivo sem leitura boa (sem amostra, ou só amostras com a trava perdida): `rastreio.Buracos`, a mesma regra da repescagem. |
| `fora_do_mapa` | posição fora de (−120…14 870, −120…14 980). |
| `parado` | vivo, fora da base, no MESMO ponto exato por 20 s ou mais (trava perdida que não foi marcada). |
| `nivel_voltou`, `cs_voltou` | nível ou CS menor que na amostra anterior. |
| `taxa_baixa` | menos de 3 amostras por segundo de jogo no passe. |
| `retravas` | mais de 2 retravas da câmera por minuto de jogo. |
| `calibracao` | erro mediano da calibração da posição acima de 150 unidades. |
| `conferencia` | a leitura da tela discorda da Riot em mais de 5% das amostras conferidas. |
| `passe_repetido` | há outro passe concluído do mesmo jogador na partida; o texto diz qual vale (o de `MelhorPasse`). |
| `trabalho_sobrando` | linhas em `amostras_brutas` de um passe já compactado. |
| `arquivo_ilegivel` | passe registrado em `passes_compactos` com um arquivo (`passe-<id>-amostras.ltc`, `-amostras_time.ltc`, `-cadeia.tsv`) faltando, ou que não descomprime/decodifica, em `dados/partidas/<id>/`. Com ele o resto da checagem do passe não roda, e a análise segue sem refazer a cadeia dele (vira aviso). |
| `passe_sem_bloco` | passe `concluido` com amostras e nenhum arquivo registrado. |

Checagens da **análise** (`analise/qualidade_analise.go`, no fim de cada `Recalcula`, depois dos retratos):

| tipo | quando |
|---|---|
| `etiqueta_impossivel` | mais da metade das amostras do trecho contradiz a etiqueta: qualquer atividade (não `morto`) com o jogador morto; `morto` com ele vivo; `na_base` com ele fora do raio da base. |
| `etiqueta_piscando` | 4 trocas de ATIVIDADE ou mais em 10 s (trechos seguidos com a mesma atividade e outro `com` não contam); um ponto por surto. |
| `aproximacao_sem_luta` | `gankando`/`roaming`/`rotacionando`/`counter_gank` sem `lutando`/`trocando`/`dive` começando até 3 s depois do fim. |
| `funcao_incoerente` | no último quadro da linha do tempo do LCU até os 14 min: caçador com mais CS de tropa que de monstro, ou quem é de rota (inclusive o suporte) com mais de monstro que de tropa, com pelo menos 20 do tipo errado. |
| `retrato_aproximado` | abate em que ele matou ou morreu com o retrato marcado `aproximado` (alguém sem posição rastreada). |
| `onda_sem_jogador` | estado de onda (`freeze`, `onda_na_torre`...) num trecho em que nenhum jogador rastreado daquela rota esteve vivo na zona dela. |

## Bônus dos objetivos e dados estáticos (`atributos/`, `campeao/estaticos.go`)

Nada é gravado no banco: `GET .../bonus-objetivos` mede na hora e `GET .../estaticos` lê o cache em disco
`dados/estaticos/<versão do Data Dragon>/` (`campeoes.json`, `itens.json` e `jogo/<campeão>.json`, já reduzidos).

**Medição do degrau** (por dragão elemental e jogador rastreado do time que pegou; oceano e quimtec ficam de fora):
antes = o último trecho firme (mesmo valor por ≥ 1,5 s e ≥ 4 leituras) até 15 s antes do carimbo; depois = o primeiro
trecho firme ACIMA do valor de antes e até 3× o bônus da tabela, até 20 s depois (90 s na velocidade de movimento, que só
vale fora de combate: o primeiro trecho firme mais rápido é o primeiro fora de luta). Compra/venda corta a janela (colada na
queda, ±0,3 s, não mede); nível novo também, menos na velocidade de movimento. `por_acumulo` = fração do total por dragão,
contando os do mesmo tipo que o time já tinha ((1+k·p)/(1+(k−1)·p)); a faixa é a do arredondamento da tela (±0,5;
±0,005 na velocidade de ataque); a velocidade de movimento é comparada sem o teto suave. O resumo por (tipo, atributo) é o
meio do cruzamento das faixas (confiança alta) ou a mediana (média). Partida 3304217746 (16.22): montanha 5,07% de armadura
e 4,79% de RM, nuvens 4,86%, infernal 3,13% de DA (tabela: 5%, 5%, 3%).

**Decomposição** (app, `atributos.js`): base do campeão no nível (`base + crescimento × (n−1) × (0,7025 + 0,0175·(n−1))`)
+ itens (Data Dragon `stats`, completado pelo texto `<stats>`) + objetivos (dragões: lido × k·p/(1+Σk·p); Barão ativo: a
tabela de `bonus.js`) + sobra ("runas e efeitos temporários"); sobra negativa além do arredondamento = "não explicado".
Velocidade de ataque: base + razão × bônus%; velocidade de movimento: (base + fixo) × (1 + %) com o teto suave.
A base vem do registro do campeão nos arquivos do jogo (CommunityDragon): o Data Dragon do 16.5 em diante tem o
crescimento de dano de ataque por nível zerado em todos os campeões.

## aprendizado

Coletas de partidas de referência por elo (contrato: "Aprendizado por elo"). Região por enquanto só `BR1`.

### `aprendizado_coletas`
`id`, `regiao` (`BR1`), `elos` (JSON, do menor para o maior), `partidas_por_jogador` (1–3), `quem` (`jogador`/`todos`),
`velocidade`, `estado` (`rodando pausado concluido cancelado falhou`; na subida do servidor `rodando` vira `pausado`), `fase`
(`descoberta rastreio fim`), `pausa_pedida` (0/1: pausa graciosa pedida e ainda não alcançada), `erro`, `criado_em`,
`atualizado_em`, `terminado_em`, `so_descobrir` (0/1: só acha os jogadores, termina sem rastrear), `funcao` (vazio ou a
função principal exigida dos jogadores; os outros contam no descarte `outra_funcao`), `estudo_id` (0; > 0 = o
rastreio das partidas desse estudo: sem elos nem jogadores, começa na fase `rastreio`, `regiao` vazia, fora da lista do
cartão).

### `aprendizado_elos`
PK `(coleta_id, elo)`; `ordem`, `estado` (`pendente descobrindo pronto`), `candidatos_vistos` (quantos da lista ordenada já foram
olhados: a retomada continua dali), `descartes` (JSON `{inativo, veterano, poucas_partidas, travado, nao_achado, nao_mono,
sem_historico, sem_partida}`; `sem_partida` = sem vitória rastreável no histórico, não ocupa vaga), `aviso` (ex.: achou menos de 10).

### `aprendizado_jogadores`
`id`, `coleta_id`, `regiao`, `elo`, `posicao` (na ordem de subida), `lp`, `divisao`, `vitorias`, `derrotas`, `taxa_vitoria`,
`fresh_blood`, `veteran`, `hot_streak`, `pontuacao_subida` (os números da league-v4 que justificaram a escolha), `puuid_riot`
(da API da Riot; **nunca usado no LCU**), `nome`, `tag`, `puuid_lcu` (achado no LCU pelo Riot ID), `campeao_id`, `campeao`,
`fracao_campeao`, `partidas_solo`, `mono`, `funcao` (`TOP JUNGLE MIDDLE BOTTOM UTILITY` ou vazio: a posição mais jogada nas
ranqueadas solo do histórico, pelo participantId do item), `fracao_funcao`, `vitorias_rastreaveis`, `derrotas_rastreaveis`
(ranqueada solo do patch instalado, ≥ 15 min, sem rendição antecipada, na função principal), `estado`
(`com_partidas`/`sem_partida`; `sem_partida` só em coletas antigas), `criado_em`. `UNIQUE (coleta_id, puuid_riot)`.

### `aprendizado_partidas`
`id`, `coleta_id`, `jogador_id`, `regiao`, `elo`, `game_id`, `participante_id`, `campeao_id`, `campeao`, `abates`, `mortes`,
`assistencias`, `duracao_s`, `pontuacao` (0–100), `ordem`, `estado` (`pendente baixando rastreando pausada concluida erro
cancelada`), `rastreio_id` (o rastreio do pacote `rastreio`), `erro`, `atualizado_em`. `UNIQUE (coleta_id, game_id,
participante_id)`; com `quem = todos` a mesma partida não entra duas vezes na coleta. Na coleta de um estudo, `jogador_id` é 0 e
`elo` vazio (o jogador é o do estudo).

O espaço da coleta (`GET .../espaco`) soma `passes_compactos.bytes` e a cadeia dos rastreios dela e os retratos e indicadores
das partidas dela (texto das colunas + estimativa dos números; sem a sobra do SQLite).

## estudo

### `estudos`
`id`, `nome`, `puuid` (o jogador), `campeao_id` (0 = qualquer), `posicao` ('' = qualquer) — filtros só para sugerir partidas e
marcar `fora_do_filtro`; a lista é escolhida à mão —, `criado_em`, `atualizado_em`.

### `estudo_partidas`
PK `(estudo_id, game_id)`; `resultado` (`vitoria`/`derrota`, dos dados da partida, não da pessoa), `anotacao` (texto livre),
`adicionada_em`, `atualizada_em`. Ao entrar, a partida é analisada se ainda não foi (os indicadores da linha do tempo saem
mesmo sem rastreio).
