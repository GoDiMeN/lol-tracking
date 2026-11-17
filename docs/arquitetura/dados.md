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
| `calibracao` | JSON `{x0, x_px, y0, y_px, cx, cy, largura, altura, modo, descartados}` da fórmula câmera → mapa (abaixo) |
| `calibracao_pontos`, `erro_calibracao_mediano`, `erro_calibracao_maximo` | o ajuste contra os quadros da linha do tempo (unidades do mapa) |
| `iniciado_em`, `terminado_em`, `erro` | |

### `amostras_brutas` (trabalho)
Uma linha por leitura do jogador seguido (~4 por segundo de jogo). Colunas = as do bloco `amostras` abaixo, mais `passe_id`,
`game_id`, `participante_id`; posições e tela em REAL. Vazia para todo passe fechado.

### `amostras_time_brutas` (trabalho)
O estado dos 10 a cada ~1 s de jogo, pelo `playerlist`: `t_ms, participante_id, nivel, abates, mortes, assistencias, cs, ward,
morto, respawn, itens` (JSON `[{id,qtd}×7]` por slot) e `vida_pct, mana_pct` (tarefa 3). Vazia para todo passe fechado.

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
`atr_vel_ataque` (÷100), `atr_vel_mov`. As da tela ficam todas nulas até a tarefa 3.

Colunas de `amostras_time` (ordenado por participante e tempo, para as corridas ficarem longas): `participante_id`, `t_ms`,
`nivel`, `abates`, `mortes`, `assistencias`, `cs`, `ward` (÷100), `morto`, `respawn` (÷100), `vida_pct`, `mana_pct` (÷10),
`item0`…`item6` e `qtd0`…`qtd6` (por slot; 0 = vazio).

Tamanho medido (sonda de 2026-11-17, partida real): ~7,2 bytes por amostra no bloco `amostras` (6 919 bytes para 956
amostras) e ~1,3 KB por 4 min do estado dos 10. Uma partida de 39 min × 10 passes ≈ 9 360 × 10 amostras ≈ **0,7 MB** de
amostras + ~0,1 MB de estado dos 10 (um por passe). O teste `TestTamanhoDoBloco` mede o mesmo com dados sintéticos
ruidosos (7,9 bytes/amostra).

## Previstas (tarefas 3–4, não criadas)

- `saidas_de_rota`, `retratos_abate`, `estudos`, `estudo_partidas`, `anotacoes` (as análises da tarefa 4 provavelmente viram
  tipos novos da `cadeia`).
