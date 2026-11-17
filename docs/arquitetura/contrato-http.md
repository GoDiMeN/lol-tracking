# Contrato HTTP

Servidor em `http://127.0.0.1:8770` (`LOL_PORTA`). Rota nova: primeiro a linha aqui, depois o código.

## Regras gerais

- JSON com chaves em português, `snake_case`. Datas em ISO 8601 UTC (`2026-11-15T21:34:15Z`).
- Erro é sempre `{"erro": "frase para a pessoa"}`. Status:
  - `400` pedido malformado; `404` não existe; `409` já está rodando (o corpo traz também `execucao`);
  - `500` erro nosso; `502` o cliente do LoL respondeu erro; `503` o cliente do LoL está fechado.
- Listas vêm envelopadas (`{"jogadores": [...]}`) e nunca `null`.
- Trabalho demorado é uma **execução**: a rota responde `202 {"execucao": {"id": "..."}}` e o progresso sai por SSE.
- Exceções ao JSON: `GET /` (a interface) e `GET /api/partidas/{id}/linha-do-tempo` (JSON cru do LCU).

## Infraestrutura

| Método | Rota | Resposta |
|---|---|---|
| GET | `/health` | `{"ok": true, "modo_teste": bool}`. Não toca no banco nem no LCU. |
| GET | `/` e arquivos | o renderer (`app/src/renderer`, `LOL_RENDERER`). `/api/*` desconhecida → 404 em JSON. |
| GET | `/api/execucoes/{id}/eventos` | SSE: `id:` sequencial, `event:` tipo, `data:` JSON. Repete o que já saiu (aceita `Last-Event-ID`) e segue ao vivo até `fim`. Execução terminada fica 30 min. 404 se não existe. |
| POST | `/api/execucoes/{id}/cancelar` | 204 (também se já terminou). O `fim` sai com `{"cancelada": true}`. |

Eventos comuns a toda execução: `erro {"texto"}` (seguido de `fim`), `aviso {"texto"}` (não encerra), `fim {}`.

## Estado — tarefa 1

| Método | Rota | Resposta |
|---|---|---|
| GET | `/api/estado` | `{"lcu": {"conectado", "erro"?}, "invocador": {"puuid","nome","tag"} \| null, "versao_jogo": "16.22.834.8910", "patch": "16.22", "fase": "None", "regiao": "BR", "replays_habilitados": bool, "pasta_replays": "...", "chave_riot": {"configurada", "valida": bool\|null, "detalhe"?, "verificada_em"?}, "modo_teste": bool}`. Sempre 200: cliente fechado vira `conectado: false` com a frase. |

## Jogadores — tarefa 1

| Método | Rota | Resposta |
|---|---|---|
| POST | `/api/jogadores/buscar` `{"texto": "Nome#TAG"}` | `{"jogador": Jogador}`. Sem `#`, usa a tag da região (`BR` → `br1`). 400 texto inválido, 404 Riot ID não existe, 503 cliente fechado. |
| GET | `/api/jogadores?limite=20` | `{"jogadores": [Jogador]}`, do buscado mais recente ao mais antigo (limite 1–200). |
| GET | `/api/jogadores/{puuid}` | `{"jogador": Jogador}`; 404 se nunca foi buscado. |

`Jogador = {"puuid", "nome", "tag", "riot_id": "Nome#tag", "buscas", "buscado_em"}`

## Partidas e replay — tarefa 1

| Método | Rota | Resposta |
|---|---|---|
| GET | `/api/jogadores/{puuid}/partidas?inicio=0&quantidade=20` | `{"partidas": [Resumo], "inicio", "quantidade", "tem_mais", "fonte": "cliente"\|"banco", "aviso"?}`. `quantidade` 1–50. Grava as partidas (os 10) e consulta o replay de cada uma. Com o cliente fechado responde do banco (`fonte: "banco"`). |
| GET | `/api/partidas/{id}` | `{"partida": Partida}` com os 10 `participantes`. Busca no LCU se ainda não está completa. 404 se não existe. |
| GET | `/api/partidas/{id}/replay` | `{"replay": Replay, "execucao"?: {"id"}}`: o estado agora, no cliente (apresenta a partida ao plugin se preciso). `execucao` aparece se há download rodando. |
| POST | `/api/partidas/{id}/replay/baixar` | `202 {"execucao": {"id"}}`. `409 {"erro", "execucao": {"id"}}` se já está baixando. Eventos: `progresso Replay` (a cada mudança), `concluido {"replay": Replay, "linha_do_tempo": bool}`, `aviso`, `erro`, `fim`. Recusa (evento `erro`) com partida ao vivo (`ReadyCheck`, `ChampSelect`, `GameStart`, `InProgress`, `Reconnect`), patch antigo ou replay indisponível. **Nunca abre o replay.** Cancelar só para de acompanhar; o cliente pode terminar o download. |
| GET | `/api/partidas/{id}/linha-do-tempo` | o JSON cru de `game-timelines` (`{"frames": [...]}`), guardado no download (ou na listagem, para replay já baixado). 404 se ainda não foi guardado. |

`Replay = {"estado": "baixavel"|"baixando"|"baixado"|"incompativel"|"indisponivel", "progresso": 0–100, "estado_lcu": "download"|"downloading"|"watch"|"incompatible"|..., "detalhe"?}`

`Resumo = {"game_id", "fila", "fila_nome", "modo", "versao", "patch", "duracao_s", "criada_em", "venceu", "campeao_id", "campeao", "posicao", "abates", "mortes", "assistencias", "cs", "ouro", "dano_campeoes", "completa", "tem_linha_do_tempo", "replay": Replay}` (do ponto de vista do jogador do `puuid`).

`Partida = {"game_id", "plataforma", "fila", "fila_nome", "modo", "tipo", "mapa", "versao", "patch", "duracao_s", "criada_em", "criada_em_ms", "time_vencedor": 100|200, "completa", "replay": Replay, "tem_linha_do_tempo", "participantes": [Participante]}`

`Participante` = uma linha da tabela `participantes` (ver `dados.md`), com `venceu`/`primeiro_abate` em booleano e `itens` como lista de 7 ids. `posicao` usa os nomes do Live Client Data: `TOP`, `JUNGLE`, `MIDDLE`, `BOTTOM`, `UTILITY` (vazio fora de Summoner's Rift).

---

## Previstas (não implementadas)

Rascunho para os próximos agentes; quem implementar ajusta aqui antes do código.

### Rastreio — tarefa 2

| Método | Rota | Ideia |
|---|---|---|
| POST | `/api/partidas/{id}/rastreio` `{"jogadores": [participante_id]?, "velocidade": 2}` | põe a partida na fila (padrão: os 10). `202 {"rastreio": Rastreio, "execucao": {"id"}}`. 409 se já há rastreio dela. Recusa se a fase não for `None`/`WatchInProgress`. |
| GET | `/api/rastreios` | `{"rastreios": [Rastreio]}`: a fila, com passe atual, progresso e estimativa. |
| GET | `/api/rastreios/{id}` | `{"rastreio": Rastreio, "passes": [Passe]}`. |
| POST | `/api/rastreios/{id}/pausar` · `/retomar` · `/cancelar` | 204. Pausar e cancelar **fecham o replay** (regra 5). |
| GET | `/api/rastreios/{id}/eventos` | SSE do rastreio: `passe_inicio`, `amostra` (a última leitura, resumida), `passe_fim`, `erro`, `fim`. |

### Leitura da tela — tarefa 3

| Método | Rota | Ideia |
|---|---|---|
| POST | `/api/visao/ler` (multipart com a captura) | lê uma captura guardada e devolve o que reconheceu; para calibrar e testar sem o jogo. |

### Partida rastreada e análise — tarefas 2 e 4

| Método | Rota | Ideia |
|---|---|---|
| GET | `/api/partidas/rastreadas` | `{"partidas": [...]}` com quantos dos 10 foram rastreados. |
| GET | `/api/partidas/{id}/amostras?participante=&de=&ate=` | as amostras (tempo, x, y, vida, mana, cooldowns...). |
| GET | `/api/partidas/{id}/eventos` | abates, objetivos, compras (Live Client + linha do tempo). |
| GET | `/api/partidas/{id}/momento?t=513` | onde e como estavam os 10 num instante (o mapa da tela). |
| GET | `/api/partidas/{id}/saidas-de-rota?participante=` | início, fim, para onde, vida ao sair. |
| GET | `/api/partidas/{id}/abates/{n}/retrato` | o retrato de cada abate. |
| PUT | `/api/partidas/{id}/anotacao` `{"texto"}` | anotação livre da partida. |

### Estudo — tarefa 4

| Método | Rota | Ideia |
|---|---|---|
| GET/POST | `/api/estudos` | lista / cria `{"puuid", "campeao_id"?, "nome"}`. |
| GET/DELETE | `/api/estudos/{id}` | |
| POST/DELETE | `/api/estudos/{id}/partidas/{game_id}` | adiciona / tira partida da lista. |
| GET | `/api/estudos/{id}/comparacao` | `{"linhas": [{"comportamento", "vitorias", "derrotas", "observacao"}]}`. |
