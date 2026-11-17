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

## Previstas (tarefas 2–4, não criadas)

- `rastreios` (fila: partida, velocidade, estado, criado/terminado) e `passes` (rastreio, participante, estado, tempo de jogo alcançado).
- `amostras` (game_id, participante_id, t_ms, x, y, vida, vida_max, mana, mana_max, cooldowns, atributos, fonte) — muitas linhas; índice `(game_id, participante_id, t_ms)`.
- `estados_jogadores` (os 10 a 1/s pelo Live Client: nível, itens, placar, morto, respawn, vida % dos quadros laterais).
- `eventos` (game_id, t_ms, tipo, autor, alvo, assistentes, x, y, fonte).
- `saidas_de_rota`, `retratos_abate`, `estudos`, `estudo_partidas`, `anotacoes`.
