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

## Rastreio — tarefa 2

Um rastreio = até 10 passes (um por jogador) em sequência, na mesma abertura do replay. **Um por vez**: com um rodando,
criar ou retomar outro é 409 (o corpo traz `rastreio_id` e `execucao` do que está rodando). Toda execução **fecha o replay**
ao terminar, pausar, cancelar, falhar ou estourar (regra 5).

| Método | Rota | Resposta |
|---|---|---|
| POST | `/api/partidas/{id}/rastreio` `{"jogadores": [participante_id]?, "velocidade": 2?}` | `202 {"rastreio": Rastreio, "execucao": {"id"}}`. Padrão: os 10, velocidade 2 (0,5–8). 400 participante inexistente/repetido, partida incompleta ou velocidade fora; 404 partida não está no banco; 409 já há rastreio rodando, fase do cliente não permitida (só `None`/`WatchInProgress`) ou replay não baixado. Importa os eventos da linha do tempo na hora. |
| GET | `/api/rastreios?limite=50` | `{"rastreios": [Rastreio]}`, do mais novo ao mais antigo (limite 1–500). |
| GET | `/api/rastreios/{id}` | `{"rastreio": Rastreio, "passes": [Passe]}`. 404. |
| POST | `/api/rastreios/{id}/pausar` | 204 (também se já está pausado). O passe guarda o último `t`; o replay fecha. 409 se terminou. |
| POST | `/api/rastreios/{id}/retomar` | `202 {"execucao": {"id"}}`. Continua os passes não concluídos do último `t` gravado (seek ~4 s antes + retrava). Vale para `pausado` e `falhou`; 409 nos outros estados ou com outro rodando. |
| POST | `/api/rastreios/{id}/cancelar` | 204. Passe começado é fechado (compactado) como `cancelado`; os pendentes viram `cancelado`. 409 se já concluiu. O `/api/execucoes/{id}/cancelar` da execução faz o mesmo. |
| GET | `/api/rastreios/{id}/eventos` | SSE da execução mais recente do rastreio (o mesmo de `/api/execucoes/{id}/eventos`). 404 se não houve execução neste processo. |

Eventos SSE: `abrindo_replay {rastreio_id, game_id}`, `replay_aberto {duracao_ms, largura, altura}`,
`passe_inicio {passe_id, participante_id, nome, t_inicio_ms, t_fim_ms, duracao_ms}`, `trava {passe_id, tentativas, ok, motivo?}`,
`amostra {passe_id, participante_id, t_ms, x?, y?, origem, morto, selecao, amostras, retravas, ms_leitura}` (no máximo 1 por
segundo real), `passe_fim {passe_id, participante_id, amostras, taxa, bytes, bytes_time, cadeia, calibracao {modo, pontos,
erro_mediano, erro_maximo}, estatisticas}` (`estatisticas.ms_tela_mediana` = custo da leitura da tela), `aviso_tela {passe_id, erro}` (uma vez por
passe: a tela não é a que a leitura conhece — resolução diferente de 2560×1440 ou escala do HUD diferente da padrão; as colunas
da tela ficam nulas), `conferencia_riot {passe_id, n, iguais, maior_dif, campos}` (só com chave da Riot válida), `pausado {rastreio_id, motivo?}`, `concluido {rastreio_id}`, `fechando_replay`,
`aviso`, `erro`, `fim`.

`Rastreio = {"id", "game_id", "estado": "rodando"|"pausado"|"concluido"|"cancelado"|"falhou", "velocidade", "t_inicio_ms", "t_fim_ms" (0 = fim da partida), "criado_em", "atualizado_em", "terminado_em", "erro", "passes_total", "passes_concluidos", "passe_atual": participante_id|null, "progresso": 0–1, "estimativa_s", "execucao": {"id"}|null}`

`Passe = {"id", "rastreio_id", "game_id", "participante_id", "ordem", "estado": "pendente"|"rodando"|"pausado"|"concluido"|"cancelado"|"falhou", "t_atual_ms", "duracao_ms", "amostras", "tentativas_trava", "retravas", "amostras_sem_trava", "taxa" (amostras por segundo de jogo)|null, "calibracao_pontos", "erro_calibracao_mediano"|null, "erro_calibracao_maximo"|null, "iniciado_em", "terminado_em", "erro", "compactado": bool, "bytes", "conferencia_n"|null, "conferencia_iguais"|null, "conferencia_maior_dif"|null, "conferencia_riot": {n, iguais, maior_dif, minutos, campos}|null}` (conferência da tela com a Riot: `dados.md`)

### Partida rastreada — tarefa 2

Leem do **bloco compactado** do passe (decodificado uma vez, cache em memória) ou, com o passe em andamento, da tabela de
trabalho. O passe que representa um jogador numa partida é o concluído mais recente; senão o mais recente com amostras.
Tempos na query em **segundos** (aceita fração); na resposta em ms.

| Método | Rota | Resposta |
|---|---|---|
| GET | `/api/partidas/rastreadas` | `{"partidas": [{"game_id", "participantes": [ids], "rastreados"}]}`: partidas com passe concluído. |
| GET | `/api/partidas/{id}/amostras?participante=3&de=0&ate=600&passo=0&limite=5000` | `{"participante_id", "passe_id", "fonte": "compactado"|"trabalho", "de_ms", "ate_ms", "passo_ms", "amostras": [Amostra], "proximo_ms": n|null}`. `passo` > 0 = a primeira amostra de cada intervalo (decimação). Página: pedir de novo com `de = proximo_ms/1000`. 404 se o jogador não foi rastreado. |
| GET | `/api/partidas/{id}/estados?de=0&ate=60&passo=1` | `{"quadros": [Quadro], "de_ms", "ate_ms", "passo_ms"}`: os 10 de `passo` em `passo` (mín. 0,1 s, máx. 1200 quadros). É o que o mapa com play usa. |
| GET | `/api/partidas/{id}/momento?t=513` | `{"quadro": Quadro}`: os 10 num instante. |
| GET | `/api/partidas/{id}/eventos?tipo=abate` | `{"eventos": [Evento]}` em ordem de tempo (as duas fontes, sem abate/monstro/estrutura duplicado). |
| GET | `/api/partidas/{id}/cadeia?participante=3&tipos=morte,abate` | `{"participante_id", "passe_id", "itens": [{"t_ms", "tipo", "dados"}]}`: a cadeia reduzida (tipos em `dados.md`). 404 se não há passe fechado. |

`Amostra = {"t_ms", "x", "y", "origem": "camera"|"morte"|"base"|"interpolada", "cam_x", "cam_z", "tela_x", "tela_y", "selecao_ok", "trava_perdida", "morto", "respawn", "nivel", "cs", "abates", "mortes", "assistencias", "ward", "dt_ms", "vida", "vida_max", "mana", "mana_max", "cd": {"q","w","e","r","d","f"}, "atributos": {"ad","ap","armadura","rm","vel_ataque","vel_mov"}}` — o que não foi lido vem `null`. Recargas em segundos: `0` pronta, `-1` não aprendida, `null` não lida (vocabulário em `dados.md`).

`Quadro = {"t_ms", "jogadores": [{"participante_id", "x", "y", "origem" (da amostra, ou "linha_do_tempo" = interpolada nos quadros por minuto para quem não foi rastreado), "rastreado", "morto", "respawn", "nivel", "abates", "mortes", "assistencias", "cs", "itens": [{"id","qtd"}], "vida_pct", "mana_pct" (quadros laterais da tela, 0–100), "vida", "vida_max", "mana", "mana_max", "cd", "atributos" (painel: só do jogador seguido no passe)}]}`

`Evento = {"id", "fonte": "linha_do_tempo"|"jogo", "tipo": "abate"|"monstro"|"estrutura"|"multiabate"|"primeiro_abate"|"ace"|"inicio"|"fim"|<outro em minúsculas>, "tipo_original", "t_ms", "quem_id", "quem_nome", "em_quem_id", "em_quem_nome", "assistencias": [ids], "x", "y", "confirmado_jogo": bool, "dados": <JSON cru da fonte>}`

---

## Previstas (não implementadas)

Rascunho para os próximos agentes; quem implementar ajusta aqui antes do código.

### Leitura da tela — tarefa 3

| Método | Rota | Ideia |
|---|---|---|
| POST | `/api/visao/ler` (multipart com a captura) | lê uma captura guardada e devolve o que reconheceu; para calibrar e testar sem o jogo. |

### Análise — tarefa 4

| Método | Rota | Ideia |
|---|---|---|
| GET | `/api/partidas/{id}/saidas-de-rota?participante=` | início, fim, para onde, vida ao sair (provavelmente vira tipo da cadeia). |
| GET | `/api/partidas/{id}/abates/{n}/retrato` | o retrato de cada abate. |
| PUT | `/api/partidas/{id}/anotacao` `{"texto"}` | anotação livre da partida. |

### Estudo — tarefa 4

| Método | Rota | Ideia |
|---|---|---|
| GET/POST | `/api/estudos` | lista / cria `{"puuid", "campeao_id"?, "nome"}`. |
| GET/DELETE | `/api/estudos/{id}` | |
| POST/DELETE | `/api/estudos/{id}/partidas/{game_id}` | adiciona / tira partida da lista. |
| GET | `/api/estudos/{id}/comparacao` | `{"linhas": [{"comportamento", "vitorias", "derrotas", "observacao"}]}`. |
