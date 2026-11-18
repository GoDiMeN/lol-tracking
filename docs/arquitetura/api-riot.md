# API da Riot (opcional): o que ela acrescenta

Medido em 2026-11-17 com a chave de desenvolvimento de `token.txt` (vence em 24 h; regerada leva ~1 min para ser aceita,
até lá responde `401 Unknown apikey`). Cabeçalho `X-Riot-Token`. Limites: 20 pedidos/s e 100 a cada 2 min, por rota regional.
Nada do núcleo depende dela: sem chave válida o app usa só o LCU.

- `GET https://americas.api.riotgames.com/riot/account/v1/accounts/by-riot-id/{nome}/{tag}` → `puuid` (é o mesmo puuid do LCU? **não verificado**; não misturar sem conferir).
- `GET .../lol/match/v5/matches/by-puuid/{puuid}/ids?start=0&count=100` → ids `BR1_<gameId>`. Para `GodiNzt#br1` devolveu 19,
  as mesmas 19 do LCU: é o histórico todo, não um corte.
- `GET .../lol/match/v5/matches/BR1_{gameId}/timeline` → ~986 KB (a do LCU tem ~117 KB). Quadros de 60 s. A mais que o LCU:
  - por jogador e por minuto: `championStats` (`health`, `healthMax`, `power`, `powerMax`, `attackDamage`, `abilityPower`, `armor`,
    `magicResist`, `attackSpeed`, `movementSpeed`, `abilityHaste`...), `damageStats` (dano causado/recebido, total e a campeões,
    por tipo), `goldPerSecond`, `timeEnemySpentControlled`. Serve para **conferir a leitura da tela** (vida e atributos) a cada minuto.
  - eventos: `WARD_PLACED`, `WARD_KILL`, `ITEM_PURCHASED/SOLD/DESTROYED/UNDO`, `SKILL_LEVEL_UP`, `LEVEL_UP`, `CHAMPION_KILL`,
    `CHAMPION_SPECIAL_KILL`, `TURRET_PLATE_DESTROYED`, `BUILDING_KILL`, `ELITE_MONSTER_KILL`, `DRAGON_SOUL_GIVEN`, `GAME_END`.
  - cada `CHAMPION_KILL` traz `position`, `bounty`, `shutdownBounty`, `killStreakLength` e, principalmente,
    `victimDamageReceived[]` e `victimDamageDealt[]`: **quem bateu na vítima, com qual habilidade (`spellName`, `spellSlot`) ou
    ataque básico (`basic`), e quanto de dano físico/mágico/verdadeiro**. É o único registro exato de "quem atacou quem com o quê",
    e só existe em torno das mortes.
- A chave nunca vai para log, resposta HTTP nem git.

## Aprendizado por elo (league-v4 e account-v1)

Os hosts por região ficam na tabela `server/riot/regioes.go` (por enquanto só `BR1`: plataforma `br1`, roteamento `americas`).
O ranking **nunca** vem do LCU (a lista de rotas permitidas não tem rota de ranking).

- `GET {plataforma}/lol/league/v4/{challenger|grandmaster|master}leagues/by-queue/RANKED_SOLO_5x5` → `{tier, entries: [...]}`
  (a liga inteira).
- `GET {plataforma}/lol/league/v4/entries/RANKED_SOLO_5x5/{TIER}/{I..IV}?page=N` → `[...]` (~205 por página, sem ordem).
- Cada entrada: `puuid`, `leaguePoints`, `rank`, `wins`, `losses`, `veteran`, `inactive`, `freshBlood`, `hotStreak`
  (e `summonerId`, que a Riot está tirando). O app usa `puuid` e os sinais para o critério de subida (`aprendizado/subida.go`).
- `GET {roteamento}/riot/account/v1/accounts/by-puuid/{puuid}` → `{puuid, gameName, tagLine}`. O jogador é achado no LCU pelo
  Riot ID (`gameName#tagLine`); o puuid da Riot não é usado no LCU.
- Limite (`riot/liga.go`): por host, 20 pedidos/s e 100 a cada 2 min, contados no cliente; 429/503 obedecem `Retry-After`
  (até 4 tentativas). Uma coleta dos 8 elos gasta ~8 pedidos de liga por elo abaixo de Mestre, 1 por elo do ápice e 1 de conta
  por candidato olhado (10 a 60 por elo).
