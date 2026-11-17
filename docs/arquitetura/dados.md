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

### `amostras_brutas` (trabalho)
Uma linha por leitura do jogador seguido (~4 por segundo de jogo). Colunas = as do bloco `amostras` abaixo, mais `passe_id`,
`game_id`, `participante_id`; posições e tela em REAL. Vazia para todo passe fechado.

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
| `recall` | `x, y`, `de_x`, `de_y` | salto > 3000 em < 3 s, vivo, chegando a < 1800 da fonte do time |
| `abate` / `assistencia` | `vitima`, `matador`, `x`, `y` | `CHAMPION_KILL` da linha do tempo dentro da janela do passe |
| `nivel` | `nivel` | subida de nível no `playerlist` |
| `compra` / `venda` | `item` | inventário segundo a segundo; `venda` = item que saiu (venda, consumo ou componente que virou item maior: a API não distingue) |
| `minuto` | `cs`, `cs_lcu`, `diferenca`, `ouro_total`, `ouro_atual`, `xp`, `nivel` | um por quadro da linha do tempo |
| `cs_divergente` | `cs`, `cs_lcu`, `diferenca` | o CS do jogo fora da dezena do CS da linha do tempo (com folga de 3): o `creepScore` do `playerlist` anda **de 10 em 10** |
| `trava_perdida` / `trava_retomada` | — | a câmera saiu do jogador vivo e voltou |
| `habilidade_usada` | `habilidade` (`q w e r d f`), `recarga_s` (a primeira recarga lida depois do uso), `x`, `y` | o ícone do painel passou de pronto (colorido) para em recarga (número), confirmado por 2 leituras seguidas com a recarga descendo; ou a recarga saltou mais de 3 s para cima (uso logo depois de voltar). Leitura nula não muda o estado; lacuna de mais de 3 s sem leitura esquece o estado; morto não conta. O uso real foi entre a última leitura pronta e esta (até 250 ms antes a 4 leituras/s) |
| `vida_baixa` | `vida`, `vida_max`, `pct`, `x`, `y` | vida do painel abaixo de 30% do máximo em 2 leituras seguidas, vindo de cima |
| `recuperou` | `vida`, `vida_max`, `pct`, `duracao_ms`, `x`, `y` | depois de `vida_baixa`, de volta a 50% em 2 leituras seguidas sem morrer no meio (morrendo, fica só a `morte`) |
| `mudou_zona` | `de`, `para`, `subzona`, `x`, `y` | tarefa 4 (`analise`): a zona confirmada mudou; só vale depois de `HistereseZonaMs` (3 s) na zona nova, e o item fica no instante em que ele entrou nela. Morte e renascimento não geram troca (há `morte`/`na_base`). Zonas: abaixo |
| `saiu_da_rota` | `rota`, `para` (primeira zona fora), `zonas` (as visitadas), `fim_ms`, `duracao_ms`, `fim_motivo` (`voltou` `morreu` `base` `fim_do_passe`), `fase` (`rotas` `meio` `final`), `fase_rotas` (bool, < 14 min), `vida_pct`, `mana_pct` (0,5 s antes de sair: painel, senão quadro lateral), `cs_perdido_est` (só na fase de rotas: ritmo de CS dos 2 min antes × duração; grosseiro), `aconteceu` (`[{tipo: abate\|assistencia\|morte\|objetivo, t_ms, evento, vitima?, matador?}]`), `desfecho` (`morte` > `abate` > `objetivo` > `nada`), `conta` (falso = foi andando para a base sem nada acontecer: não é saída "para jogar"), `x`, `y` | só rotador (posição TOP/MIDDLE/BOTTOM/UTILITY; BOTTOM e UTILITY = rota de baixo). Começa num trecho fora da rota logo depois de um trecho na rota; ≥ `SaidaMinimaMs` (6 s) fora, salvo se morreu |
| `voltou_para_rota` | `rota`, `inicio_ms`, `duracao_ms` | fim de uma saída que voltou para a rota |
| `entrou_em_rota` | `rota`, `trecho`, `fim_ms`, `duracao_ms`, `fase`, `fase_rotas`, `vida_pct`, `aconteceu`, `desfecho`, `x`, `y` | só caçador: ficou ≥ 6 s numa rota (gank/visita) |
| `na_base` | `fim_ms`, `duracao_ms`, `chegada` (`inicio` `renasceu` `recall` `andando`) | cada estadia viva na própria base; recall = salto de > 3000 em < 3 s logo antes |
| `atividade` | `atividade`, `fim_ms`, `duracao_ms`, `com` (participantes), `zona`, `sinais` (até 6 frases: por que), `confianca` (`alta` `media` `baixa`) | trechos de atividade DEDUZIDA (não é registro de golpe; `docs/decisoes/2026-11-17-atividade-deduzida.md`). Vocabulário e regra abaixo |

**Atividade** (a primeira regra que couber, a cada amostra; trechos iguais seguidos se juntam; `andando`/`farmando` de menos de 3 s
é absorvido pelo trecho anterior):

1. `morto` — morto no jogo (alta).
2. `na_base` — na própria base (alta).
3. `lutando` — com um inimigo **rastreado** a ≤ `AlcanceLuta` (1400) e mais alguém (2 inimigos, ou um aliado rastreado perto), e a vida de alguém caindo ≥ 4 pontos percentuais em 2 s (do seguido pelo painel; dos outros pelo quadro lateral). `com` = os envolvidos.
4. `trocando` — o inimigo rastreado mais perto a ≤ `AlcanceTroca` (1000) e a vida de um dos dois caindo (alta com o painel lido). Também, com `com: []` e confiança baixa: rotador fora da rota com a própria vida caindo ≥ 8 pontos em 2 s sem inimigo rastreado perto (quem bateu não foi rastreado).
5. `objetivo` / `torre` — a ≤ 1500 de onde caiu um monstro grande (até 40 s depois) ou uma torre/placa (até 20 s depois), derrubado pelo time dele (ou com ele). Placas vêm da linha do tempo da Riot, se guardada.
6. `andando` — deslocou mais de 1000 em linha reta nos últimos 4 s (média).
7. `farmando` — na rota, selva ou rio, com o CS da linha do tempo subindo ≥ 3/min naquele minuto (média; baixa se ninguém mais foi rastreado).
8. `andando` — nenhum outro sinal (baixa).

**Zonas** (`analise.Zona(x, y)`, fronteiras em `server/analise/fronteiras.go`, conferência em `docs/arquitetura/zonas.png`):
`base_azul`/`base_vermelha` (subzona `fonte` ou `base`), `rota_topo`/`rota_meio`/`rota_baixo` (subzona `lado_azul`, `centro`,
`lado_vermelho`: divididas pelas torres externas), `rio_cima`/`rio_baixo` (`rio`), `cova_barao`/`cova_dragao` (`cova` a ≤ 750 do
monstro, `arredores` a ≤ 1300), `selva_azul_cima`, `selva_azul_baixo`, `selva_vermelha_cima`, `selva_vermelha_baixo` (`selva`),
`fora_do_mapa`. Ordem de decisão: base, rota (a mais perto), cova, rio, selva (pelo lado do rio e da diagonal do meio).
Do ponto de vista de um jogador (`GrupoRelativo`): `rota_propria`, `outra_rota`, `rio`, `covas`, `selva_propria`,
`selva_inimiga`, `base_propria`, `base_inimiga`.

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
vizinhas com posição, até 10 s de cada lado; `trava_perdida = 1`).

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
`mana`, `mana_max`, `cd_q` `cd_w` `cd_e` `cd_r` `cd_d` `cd_f` (÷10, segundos), `atr_ad`, `atr_ap`, `atr_armadura`, `atr_rm`,
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
`saidas_rota_ate14_n`, `saidas_rota_ate14_tempo_s`, `cs_perdido_saidas_est`, `mortes_fora_da_rota`, `mortes_saida_vida_baixa`
(rotador rastreado); `visitas_rota_ate14_n`, `visitas_rota_ate14_com_abate` (caçador rastreado); `mortes`, `mortes_ate14`,
`abates`, `assistencias`, `participacao_abates`, `cs_10`, `cs_15`, `cs_por_min`, `ouro_10/15`, `xp_10/15`, `dif_cs_10/15`,
`dif_ouro_10/15`, `dif_xp_10/15` (contra o adversário direto: mesma posição no outro time) — linha do tempo, os 10;
`dano_campeoes_10/15`, `wards_ate14` — Riot, os 10; `mortes_com_r_em_recarga`, `mortes_com_flash_em_recarga` (só mortes com a
recarga lida), `dist_aliado_mais_proximo_morte`, `mortes_em_inferioridade` — retrato; `tempo_morto_s`, `tempo_vida_baixa_s`,
`cs_divergencias` — estado dos 10 no trecho rastreado; `habilidades_por_min`, `recalls` — rastreados;
`pct_<grupo>_<fase>` (grupo relativo de zona: `rota_propria outra_rota rio covas selva_propria selva_inimiga base_propria
base_inimiga`; fase `rotas` < 14 min, `meio` 14–25, `final`) e `pct_atv_<atividade>_<fase>` — rastreados, só fase com ≥ 1 min
rastreado.

### `analises`
Uma linha por partida analisada: `game_id` PK, `calculada_em`, `rastreados` (JSON), `passes`, `riot` (0/1), `abates`,
`indicadores`, `avisos` (JSON), `ms`.

Parâmetros das regras (limiares) em `server/analise/parametros.go` (`analise.Param`, também em `GET /api/indicadores`).

## estudo

### `estudos`
`id`, `nome`, `puuid` (o jogador), `campeao_id` (0 = qualquer), `posicao` ('' = qualquer) — filtros só para sugerir partidas e
marcar `fora_do_filtro`; a lista é escolhida à mão —, `criado_em`, `atualizado_em`.

### `estudo_partidas`
PK `(estudo_id, game_id)`; `resultado` (`vitoria`/`derrota`, dos dados da partida, não da pessoa), `anotacao` (texto livre),
`adicionada_em`, `atualizada_em`. Ao entrar, a partida é analisada se ainda não foi (os indicadores da linha do tempo saem
mesmo sem rastreio).
