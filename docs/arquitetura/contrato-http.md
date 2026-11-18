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
| GET | `/api/dados` | `{"pasta" (absoluta), "banco" (arquivo), "bytes": {"banco" (com -wal/-shm), "imagens", "capturas", "outros", "total"}}`: onde o app guarda tudo e quanto ocupa (tela Configurações). 500 se a pasta não pôde ser lida. |
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
| POST | `/api/partidas/{id}/rastreio` `{"jogadores": [participante_id]?, "velocidade": 2?, "de_ms": 0?, "ate_ms": 0?, "visao": false?}` | `202 {"rastreio": Rastreio, "execucao": {"id"}}`. Padrão: os 10, velocidade 2 (0,5–8), a partida inteira (`de_ms`/`ate_ms` = trecho em ms de jogo; `ate_ms` 0 = até o fim). `visao: true` acrescenta, no fim e na mesma abertura do replay, os dois **passes de visão de time** (abaixo) no mesmo trecho, à velocidade da visão (8×). 400 participante inexistente/repetido, partida incompleta, velocidade fora ou trecho inválido; 404 partida não está no banco; 409 já há rastreio rodando, fase do cliente não permitida (só `None`/`WatchInProgress`), replay não baixado ou `visao` sem a leitura do minimapa. Importa os eventos da linha do tempo na hora. |
| GET | `/api/rastreios?limite=50` | `{"rastreios": [Rastreio]}`, do mais novo ao mais antigo (limite 1–500). |
| GET | `/api/rastreios/{id}` | `{"rastreio": Rastreio, "passes": [Passe]}`. 404. |
| POST | `/api/rastreios/{id}/pausar?quando=agora\|fim_do_passe` | 204 (também se já está pausado). `agora` (padrão): para no meio do passe, que guarda o último `t`; o replay fecha. `fim_do_passe`: o passe em andamento termina normalmente (compacta, gera a cadeia) e o rastreio fica `pausado` antes do próximo; se era o último passe, conclui. 400 `quando` desconhecido; 409 se terminou. |
| POST | `/api/rastreios/{id}/retomar` | `202 {"execucao": {"id"}}`. Continua os passes não concluídos do último `t` gravado (seek ~4 s antes + retrava). Vale para `pausado` e `falhou`; 409 nos outros estados ou com outro rodando. |
| POST | `/api/rastreios/{id}/cancelar` | 204. Passe começado é fechado (compactado) como `cancelado`; os pendentes viram `cancelado`. 409 se já concluiu. O `/api/execucoes/{id}/cancelar` da execução faz o mesmo. |
| GET | `/api/rastreios/{id}/eventos` | SSE da execução mais recente do rastreio (o mesmo de `/api/execucoes/{id}/eventos`). 404 se não houve execução neste processo. |
| GET | `/api/partidas/{id}/rastreio/repescagem` | `{"game_id", "limiar_ms", "passes": [{"passe_id", "participante_id", "buracos": [Buraco]}], "total", "execucao": {"id"}|null, "rodando"}`: os trechos sem amostra (buracos > `limiar_ms` com o jogador vivo, `dados.md`) que a repescagem tentaria agora em cada passe que vale (concluído, com bloco; só os que têm buraco) e a última execução de repescagem desta partida neste processo. 404 partida. |
| POST | `/api/partidas/{id}/rastreio/repescagem` `{"participantes": [participante_id]?, "velocidade": 2?}` | `202 {"execucao": {"id"}, "passes": [...como o GET], "total"}`. **Repescagem de passe já gravado**: abre o replay, repesca os buracos de cada passe (o mesmo laço do rastreio, com todas as regras: fase do gameflow, tecla só depois de a Replay API responder, replay fechado ao terminar/falhar/cancelar), fecha de novo os passes que ganharam trecho (amostras, estado dos 10 e cadeia, pelo fechamento normal, com a calibração gravada), pede a análise e fecha o replay. Ocupa a fila como um rastreio (com ela rodando, criar/retomar rastreio é 409 com `repescagem_de`). Cancelar: `/api/execucoes/{id}/cancelar`. 400 participante inexistente/repetido, velocidade fora; 404 partida; 409 nada a recuperar, fila ocupada, fase não permitida ou replay não baixado. Eventos: os do rastreio (`abrindo_replay {game_id, repescagem: true, buracos}`, `replay_aberto`, `trava`, `amostra` com `repescagem: true`, `fechando_replay`), mais `repescagem_inicio`, `repescagem_trecho`, `repescagem_fim` (abaixo), `passe_regravado {passe_id, participante_id, amostras, taxa, cadeia}`, `concluido {game_id, buracos, recuperados, sobraram}`, `cancelado {game_id}`, `erro`, `fim`. |

### Visão dos times (passes de visão)

Dois passes por partida, um com a névoa do replay na visão do time azul (tecla F1) e outro na do vermelho (F2), para saber
a cada instante **quem do outro time está à vista** daquele time. O jogo não informa visibilidade (o `playerlist` não tem
campo de visível): na visão de um time, o minimapa só desenha o retrato de um inimigo quando o time o enxerga, e mostra as
sentinelas desse time. O passe não segue ninguém nem trava câmera: aperta F1 (ou F2) uma vez, confere pelo painel "Fog of
War" do replay que a névoa mudou (lido da tela), toca a 8× capturando o painel e o minimapa 4 vezes por segundo de jogo, e
aperta F3 ("todos") no fim. As teclas passam pelas mesmas travas de todas (regra 3: só na janela do replay, só depois de a
Replay API responder, nunca com partida ao vivo). A decisão "visível / fora da visão / indeterminado" junta o minimapa com as
**posições rastreadas**: precisa dos jogadores rastreados; inimigo não rastreado é sempre indeterminado. Regras em `dados.md`
("Visão dos times").

| Método | Rota | Resposta |
|---|---|---|
| GET | `/api/partidas/{id}/visao` | `{"game_id", "disponivel": bool, "motivo" (frase quando não dá), "rastreados": [ids], "velocidade_padrao": 8, "estimativa_s" (os dois passes à velocidade padrão: 2 × (duração ÷ 8 + 10 s) + 40 s), "passes": [PasseVisao], "execucao": {"id"}\|null (a última dos passes avulsos, ou a do rastreio rodando um passe de visão), "rodando", "visao": VisaoDaPartida\|null}`. 404 partida. |
| POST | `/api/partidas/{id}/visao/rastreio` `{"velocidade": 8?, "de_ms": 0?, "ate_ms": 0?}` | `202 {"execucao": {"id"}, "passes": [PasseVisao], "estimativa_s"}`. **Os dois passes de visão avulsos**: abre o replay, roda a visão do azul e a do vermelho, fecha o replay (ao terminar, falhar ou cancelar). Passes avulsos que pararam no meio (servidor caiu, falha) são continuados; com trecho (`de_ms`/`ate_ms`), sempre passes novos. Ocupa a fila como um rastreio (com ela rodando, criar/retomar rastreio ou repescar é 409 com `visao_de`). Cancelar: `/api/execucoes/{id}/cancelar` (o passe começado fica `cancelado`; as capturas dele valem só se o time ainda não tinha passe de visão). 400 velocidade fora (0,5–8) ou trecho inválido; 404 partida; 409 sem jogadores rastreados, fila ocupada, fase não permitida, replay não baixado ou sem a leitura do minimapa. Eventos: `abrindo_replay {game_id, visao: true, passes}`, `replay_aberto`, `visao_inicio`, `nevoa`, `visao_amostra`, `visao_fim` (abaixo), `aviso_tela {passe_visao_id, erro}`, `fechando_replay`, `concluido {game_id}`, `cancelado {game_id}`, `aviso`, `erro`, `fim`. |

Eventos dos passes de visão (avulsos ou no fim de um rastreio): `visao_inicio {passe_visao_id, time, t_inicio_ms, t_fim_ms, duracao_ms, velocidade}`,
`nevoa {passe_visao_id, time, modo, ok}` (o painel conferiu a névoa do time), `visao_amostra {passe_visao_id, time, t_ms, modo, retratos, inimigos_a_vista, sentinelas, capturas, erros_leitura}` (no máximo 1 por segundo real),
`visao_fim {passe_visao_id, time, estado, capturas, capturas_modo_ok, taxa, estatisticas, sentinelas, aliados_achados}` (ou `{passe_visao_id, time, estado: "falhou", erro}`: a névoa não mudou depois de 3 teclas, ou sem a leitura do minimapa — o passe falha e a corrida segue).

`PasseVisao = {"id", "game_id", "rastreio_id" (0 = avulso), "time" (o observador: 100 azul, 200 vermelho), "ordem", "estado": "pendente"|"rodando"|"pausado"|"concluido"|"cancelado"|"falhou", "velocidade", "t_inicio_ms", "t_fim_ms" (0 = fim), "t_atual_ms", "duracao_ms", "capturas", "capturas_modo_ok" (o painel dizia a névoa do time), "taxa" (capturas por segundo de jogo)|null, "teclas", "iniciado_em", "terminado_em", "erro"}`

`VisaoDaPartida = {"game_id", "calculada_em", "tolerancia_px", "oclusao_px", "unidades_por_pixel", "times": [VisaoDoTime]}`, `VisaoDoTime = {"time" (observador), "passe_visao_id", "de_ms", "ate_ms", "capturas", "capturas_por_s", "modo_conferido" (fração das capturas com o painel certo), "aliados_achados" (fração dos retratos do próprio time achados no lugar rastreado: a conferência da leitura)|null, "jogadores": [VisibilidadeJogador] (os 5 do outro time), "sentinelas": [SentinelaLida] (as do observador), "aviso" (frase quando menos de 80% dos aliados foram achados no lugar: posição rastreada ou geometria não confere; vazio se tudo bem)}`, `VisibilidadeJogador = {"participante_id", "time", "rastreado", "visivel": [[de_ms, ate_ms]], "fora": [[...]], "indeterminado": [[...]], "motivos": {motivo: ms}, "pct_visivel"|null}` (morto e sem captura ficam fora das três listas; motivos: `painel`, `aliados`, `sem_posicao`, `anel_sem_dono`, `coberto`, `borda`), `SentinelaLida = {"x", "y", "de_ms", "ate_ms" (a última captura em que estava), "capturas", "semelhanca", "ate_o_fim"}`.

No modo teste (`LOL_TESTE=1`) o jogo falso desenha o painel e o minimapa de cada visão (retratos na cor do time de quem o time
enxerga — a menos de 1200 de um vivo do time ou de 900 de uma sentinela dele — e quatro sentinelas fixas), e a leitura do
minimapa é a de verdade.

No modo teste (`LOL_TESTE=1`) cada jogador perde a trava de 3:20 a 3:35 nas 3 primeiras passagens do replay por ali (o passe e as duas tentativas da repescagem no fim dele): o rastreio termina com 1 trecho sem amostra por passe, que a repescagem de passe gravado recupera. A leitura da tela é o `rastreio.LeitorFalso`: vida, recurso, recargas e barras inventadas a partir do tempo do jogo falso, para a interface ter o que mostrar sem o jogo.

Eventos SSE: `abrindo_replay {rastreio_id, game_id}`, `replay_aberto {duracao_ms, largura, altura}`,
`passe_inicio {passe_id, participante_id, nome, t_inicio_ms, t_fim_ms, duracao_ms}`, `trava {passe_id, tentativas, ok, motivo?}`,
`amostra {passe_id, participante_id, t_ms, x?, y?, origem, morto, trava_perdida, selecao, amostras, retravas, ms_leitura, nivel, cs, vida, vida_max, mana, mana_max, cd {q,w,e,r,d,f}}` (no máximo 1 por
segundo real; o que a tela não leu vem `null`: é a "última leitura" da tela de rastreio), `passe_fim {passe_id, participante_id, amostras, taxa, bytes, bytes_time, cadeia, calibracao {modo, pontos,
erro_mediano, erro_maximo}, estatisticas, repescagem: ResumoRepescagem}`, `repescagem_inicio {passe_id, participante_id, buracos: [Buraco], limite, quando}`, `repescagem_trecho {passe_id, participante_id, n, de_ms, ate_ms, tentativa, ok, motivo?}`, `repescagem_fim {passe_id, participante_id, buracos, recuperados, sobraram, restantes: [Buraco], quando}` (ao fim de cada passe, antes de fechá-lo, o motor repesca os buracos com o replay ainda aberto) (`estatisticas.ms_tela_mediana` = custo da leitura da tela), `aviso_tela {passe_id, erro}` (uma vez por
passe: a tela não é a que a leitura conhece — resolução diferente de 2560×1440 ou escala do HUD diferente da padrão; as colunas
da tela ficam nulas), `conferencia_riot {passe_id, n, iguais, maior_dif, campos}` (só com chave da Riot válida), `pausa_pedida {rastreio_id, quando: "fim_do_passe"}`, `pausado {rastreio_id, motivo?}`, `concluido {rastreio_id}`, `fechando_replay`,
`aviso`, `erro`, `fim`.

`Rastreio = {"id", "game_id", "estado": "rodando"|"pausado"|"concluido"|"cancelado"|"falhou", "velocidade", "t_inicio_ms", "t_fim_ms" (0 = fim da partida), "criado_em", "atualizado_em", "terminado_em", "erro", "passes_total", "passes_concluidos", "passe_atual": participante_id|null, "progresso": 0–1, "estimativa_s", "execucao": {"id"}|null, "com_visao", "passes_visao_total", "passes_visao_concluidos"}` (com passes de visão, o progresso conta cada um como um passe e a estimativa soma a duração deles à velocidade da visão)

`Passe = {"id", "rastreio_id", "game_id", "participante_id", "ordem", "estado": "pendente"|"rodando"|"pausado"|"concluido"|"cancelado"|"falhou", "t_atual_ms", "duracao_ms", "amostras", "tentativas_trava", "retravas", "amostras_sem_trava", "taxa" (amostras por segundo de jogo)|null, "calibracao_pontos", "erro_calibracao_mediano"|null, "erro_calibracao_maximo"|null, "iniciado_em", "terminado_em", "erro", "compactado": bool, "bytes", "conferencia_n"|null, "conferencia_iguais"|null, "conferencia_maior_dif"|null, "conferencia_riot": {n, iguais, maior_dif, minutos, campos}|null, "repescagem": ResumoRepescagem|null}` (conferência da tela com a Riot e repescagem: `dados.md`)

`ResumoRepescagem = {"quando": "fim_do_passe"|"depois", "buracos", "recuperados", "sobraram", "restantes": [Buraco], "feita_em"}`, `Buraco = {"de_ms", "ate_ms", "duracao_ms", "amostras" (sem posição de verdade dentro; 0 = vazio), "motivo"?}`. A `estimativa_s` do Rastreio inclui a repescagem dos passes que faltam (média de buracos dos passes já repescados, ou 0,6 por passe).

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

`Amostra = {"t_ms", "x", "y", "origem": "camera"|"morte"|"base"|"interpolada", "cam_x", "cam_z", "tela_x", "tela_y", "selecao_ok", "trava_perdida", "morto", "respawn", "nivel", "cs", "abates", "mortes", "assistencias", "ward", "dt_ms", "vida", "vida_max", "mana", "mana_max", "cd": {"q","w","e","r","d","f","p"}, "atributos": {"ad","ap","armadura","rm","vel_ataque","vel_mov"}}` — o que não foi lido vem `null`. Recargas em segundos: `0` pronta, `-1` não aprendida, `null` não lida (vocabulário em `dados.md`); `p` é a passiva (`0` na maioria dos campeões; segundos nos que têm recarga nela, como Vex e Garen).

`Quadro = {"t_ms", "jogadores": [{"participante_id", "x", "y", "origem" (da amostra; `interpolada` também no meio de um buraco de até 20 s, `BuracoCurtoMs`, entre duas leituras boas: a reta entre as duas, a tela diz "estimado entre duas leituras" — não atravessa morte, renascimento, volta à base nem salto maior que o que se anda; `linha_do_tempo` = interpolada nos quadros por minuto, para quem não foi rastreado ou no buraco longo/sem leitura de um lado), "rastreado", "morto", "respawn", "nivel", "abates", "mortes", "assistencias", "cs", "itens": [{"id","qtd"}], "vida_pct", "mana_pct" (quadros laterais da tela, 0–100), "vida", "vida_max", "mana", "mana_max", "cd", "atributos" (painel: só do jogador seguido no passe)}]}`

`Evento = {"id", "fonte": "linha_do_tempo"|"jogo", "tipo": "abate"|"monstro"|"estrutura"|"multiabate"|"primeiro_abate"|"ace"|"inicio"|"fim"|<outro em minúsculas>, "tipo_original", "t_ms", "quem_id", "quem_nome", "em_quem_id", "em_quem_nome", "assistencias": [ids], "x", "y", "confirmado_jogo": bool, "dados": <JSON cru da fonte>}`

Quando um passe fecha (concluído ou cancelado com amostras), a análise da partida é refeita sozinha em segundo plano (abaixo).

## Análise — tarefa 4

Tudo deduzido por regra (sem IA) e recalculável. Vocabulários e regras: `dados.md` (seções `cadeia` e `analise`); decisões em
`docs/decisoes/2026-11-17-zonas-do-mapa.md` e `docs/decisoes/2026-11-17-atividade-deduzida.md`. Tempos na query em segundos;
na resposta em ms.

| Método | Rota | Resposta |
|---|---|---|
| GET | `/api/mapa/zonas?celula=100` | `{"zonas": [InfoZona], "subzonas": {codigo: nome}, "grupos": [..], "limites": {"min": {x,y}, "max": {x,y}}, "grade": Grade, "imagem"}`. `celula` 25–1000 unidades. Para o mapa pintar/consultar a zona sem refazer a geometria. |
| GET | `/api/indicadores` | `{"indicadores": [DefIndicador], "atividades": [..], "fases": [..], "parametros": {...}}`: o vocabulário dos indicadores e os limiares em uso. |
| GET | `/api/partidas/{id}/analise` | `{"analise": Analise \| null}` (null = nunca analisada). |
| POST | `/api/partidas/{id}/analise` | Recalcula agora (síncrono, ~0,2 s com 2 passes de 15 min). `200 {"analise": Analise}`. 404 partida; 409 sem a linha do tempo do LCU guardada. Busca e guarda a linha do tempo da Riot na primeira vez, se a chave vale (falha da Riot vira `avisos`). |
| GET | `/api/partidas/{id}/qualidade` | `{"game_id", "total", "passes": [{"passe_id", "participante_id", "contagem": {tipo: n}, "total", "pontos": [{"tipo", "t_ms" (0 = o passe inteiro), "texto"}]}]}`: a checagem de qualidade do DADO, do armazenamento (arquivos em `dados/partidas/<id>/`) e da ANÁLISE (etiquetas) de cada passe, refeita em cada análise (ao fim de cada passe e no `POST .../analise`). Só aponta; não muda nada. Tipos e limiares em `dados.md` (`qualidade_passes`). Lista vazia = nunca conferida. |
| GET | `/api/partidas/{id}/abates` | `{"abates": [Retrato]}` em ordem de tempo (vazia se não analisada). |
| GET | `/api/partidas/{id}/abates/{n}` | `{"abate": Retrato}`. 404. |
| GET | `/api/partidas/{id}/indicadores?participante=` | `{"indicadores": [Indicador]}` (sem participante = os 10), na ordem do vocabulário. |
| GET | `/api/partidas/{id}/saidas-de-rota?participante=` | `{"saidas": [{"participante_id", "tipo": "saiu_da_rota"\|"entrou_em_rota", "t_ms", "dados"}]}` (dados = o item da cadeia). Só rastreados. |
| GET | `/api/partidas/{id}/atividades?de=0&ate=600&participante=` | `{"atividades": [Trecho], "de_ms", "ate_ms"}`: os trechos que tocam a janela, de todos os rastreados (ou de um). |
| GET | `/api/partidas/{id}/fases` | `{"fases": [{"fase": "rotas"\|"meio"\|"final", "de_ms", "ate_ms" (null = até o fim), "motivo"}], "duracao_ms"}`: as fases da partida pela MESMA regra da análise (atividade gankando/roaming × rotacionando): a fase de rotas termina quando cai a primeira torre (BUILDING_KILL de torre da linha do tempo do LCU), no máximo aos `FaseRotasMs` (14 min); o fim de jogo começa no marco fixo `FaseMeioMs` (25 min). `motivo` é a frase para a tela ("caiu a primeira torre aos 11:14", "marco fixo de 14 min: nenhuma torre caiu antes"). Fase que começaria depois do fim da partida não aparece. 404 partida; 409 sem a linha do tempo do LCU guardada. |
| GET | `/api/partidas/{id}/ondas` | `Ondas`: o estado da onda de tropas em cada rota, ESTIMADO pela posição dos jogadores rastreados da rota (nenhuma tropa é rastreada; regras em `dados.md`, "Ondas de tropas"). Calculado na hora pelo que está guardado (não grava nada, não depende de a análise ter rodado; os mesmos trechos que a análise põe na cadeia). Só até os 14 min (ou o fim do rastreio). 404 partida; 409 sem a linha do tempo do LCU guardada. |
| GET | `/api/partidas/{id}/sentinelas` | `Sentinelas`: as sentinelas colocadas (`WARD_PLACED`) e destruídas (`WARD_KILL`) da linha do tempo da Riot, com a posição ESTIMADA pelo lugar onde quem colocou estava (amostras do passe dele; a Riot não dá posição), o raio de incerteza e a vida estimada (prazo do tipo, limite por jogador, destruição casada por proximidade). Calculado na hora (não grava). Sem a linha do tempo da Riot guardada: `riot: false`, listas vazias e `motivo`. 404 partida; 409 sem a linha do tempo do LCU guardada. |
| GET | `/api/partidas/{id}/cs?participante=` | `{"jogadores": [{"participante_id", "divergencias", "minutos": [{"minuto", "t_ms", "cs_lcu", "cs_jogo", "diferenca", "divergente"}]}]}`: CS do jogo (estado dos 10, anda de 10 em 10) × linha do tempo, minuto a minuto, para quem tem estado gravado. |
| GET | `/api/partidas/{id}/bonus-objetivos` | `{"game_id", "patch", "quedas": [Queda], "resumo": [ResumoBonus], "regras": [texto]}`: o bônus de cada dragão elemental MEDIDO no próprio dado (pacote `atributos`): no instante da queda, o degrau de cada atributo de cada jogador rastreado do time que pegou. Oceano e quimtec não aparecem (dão o que a tela não lê). Calculado na hora (não grava). 404 partida; 409 sem a linha do tempo do LCU guardada. |
| GET | `/api/partidas/{id}/estaticos` | `{"game_id", "versao_jogo", "versao_dados" (Data Dragon, ex. "16.22.1"), "campeoes": {participante_id: AtributosCampeao}, "itens": {item_id: ItemEstatico}, "avisos": [texto], "fontes": [texto]}`: os dados estáticos da VERSÃO DA PARTIDA que a tela usa para decompor os atributos e descrever os itens (só os itens que aparecem na partida). Baixa uma vez por patch (Data Dragon + CommunityDragon) e guarda em `dados/estaticos/<versão>/`. 404 partida; 503 sem rede na primeira vez (ou no modo teste sem a versão). |
| GET | `/api/partidas/{id}/build?participante=` | `{"game_id", "versao_dados", "jogadores": [{"participante_id", "campeao", "eventos": [EventoBuild], "final": [EventoBuild]}], "avisos": [texto]}` (sem participante = os 10): a ordem e o instante de cada compra e venda, do estado dos 10 (o mesmo dado dos itens `compra`/`venda` da cadeia; `venda` = item que saiu: venda, consumo ou componente que virou item maior). O primeiro estado gravado conta como compra nesse instante. Sem os dados estáticos, `nome` vazio e `custo` nulo, com aviso. 404 partida. |
| GET | `/api/imagens/campeao/{campeao_id}` | PNG do retrato quadrado (Data Dragon, `img/champion/<chave>.png`), baixado uma vez e servido de `dados/imagens/`. 404 sem rede na primeira vez, id desconhecido ou modo teste; 400 id inválido. |
| GET | `/api/imagens/item/{item_id}` | PNG do ícone do item (Data Dragon `img/item/<id>.png`), mesmo cache. |

`Analise = {"game_id", "calculada_em", "rastreados": [ids], "passes", "riot": bool, "abates", "indicadores", "avisos": [texto], "ms"}`

`InfoZona = {"codigo", "nome", "tipo": "base"|"rota"|"rio"|"cova"|"selva"|"fora", "lado": "azul"|"vermelho"|"", "subzonas": [..], "cor"}`

`Grade = {"origem": {x,y}, "celula", "colunas", "n_linhas", "codigos": [zona], "linhas": ["AAB..."], "legenda"}` — `linhas[i][j]` é `'A'` + índice em `codigos` da célula de centro `(origem.x + (j+0,5)·celula, origem.y + (i+0,5)·celula)`; linha 0 = y mínimo (embaixo).

`Retrato = {"n", "t_ms", "vitima", "matador", "assistencias", "x", "y", "zona", "subzona", "aliados_da_vitima", "inimigos_da_vitima", "superioridade" (inimigos − (aliados+1); > 0 = vítima em inferioridade), "aliado_mais_proximo", "aproximado", "jogadores": [JogadorNoAbate], "riot": RiotNoAbate|null}`

`JogadorNoAbate = {"participante_id", "time", "papel": "vitima"|"matador"|"assistencia"|"", "x", "y", "origem": "rastreado"|"evento"|"morte"|"perto_do_abate"|"linha_do_tempo"|"", "aproximada", "zona", "subzona", "dist_vitima", "vivo": bool|null, "vida_pct", "mana_pct", "nivel", "itens": [{id,qtd}], "rastreado", "vida", "vida_max", "cd": {q,w,e,r,d,f,p}|null, "r_aprendido", "r_disponivel", "flash_disponivel", "passiva_disponivel", "aliados_perto", "inimigos_perto", "fora_da_rota_ms"}` (nulo = não se sabe)

`RiotNoAbate = {"recebido": [Dano], "causado": [Dano], "por_jogador": {pid: dano}, "ouro", "sequencia"}`, `Dano = {"participante_id", "nome", "habilidade" (spellName ou "ataque básico"), "slot", "basico", "tipo", "fisico", "magico", "verdadeiro", "total"}`

`Indicador = {"participante_id", "indicador", "valor"|null, "aproximado", "detalhe": {"momentos"?: [{t_ms, texto}], "cobertura_ms"?, ...}, "rotulo", "unidade", "fonte", "melhor": "maior"|"menor"|""}`

`DefIndicador = {"nome", "rotulo", "unidade", "fonte": "linha_do_tempo"|"riot"|"rastreio"|"retrato"|"estado"|"visao", "quem": "todos"|"rastreados"|"rotador"|"cacador", "melhor", "descricao"}`

`Ondas = {"saida_tropas_ms", "saida_medida" (falso = evento não gravado, 0:30 da regra), "fim_fase_rotas_ms", "ate_ms", "ondas": [{"n", "saida_ms", "canhao", "chegada_ms": {rota: ms}}], "rotas": [OndasDaRota], "regras" (fonte), "limites": [texto]}`, `OndasDaRota = {"rota", "rastreados": [ids], "eixo": {"rota", "comprimento", "encontro_s", "torres": [{"time", "ordem" (1 externa, 2 interna, 3 do inibidor), "s", "x", "y"}]}, "pontos": [{"t_ms", "s", "x", "y", "confianca": "alta"|"media"}]` (1 por segundo; buraco = ninguém da rota, posição desconhecida) `, "janelas": [{"onda", "canhao", "de_ms", "ate_ms", "s"|null, "confianca", "estado", "time", "avanco"|null, "sinais"}], "estados": [{"estado": "freeze"|"onda_na_torre"|"onda_voltando"|"empurrando"|"onda_parada", "de_ms", "ate_ms", "ondas", "time", "velocidade"?, "avanco_por_onda"?, "confianca", "s", "x", "y", "sinais"}], "perdidas": [{"participante_id", "onda", "canhao", "t_ms", "motivo": "morto"|"na_base"|"longe", "zona", "distancia", "onda_s", "posicao_onda", "x", "y"}]}`

`Sentinelas = {"game_id", "riot": bool, "motivo" (frase quando não há), "sentinelas": [Sentinela], "destruicoes": [Destruicao], "times": [{"time", "colocadas", "de_controle", "destruidas"}], "jogadores": [{"participante_id", "time", "rastreado", "colocadas", "por_fase": {"rotas", "meio", "final"}, "de_controle", "destruidas", "metade_inimiga" (null sem rastreio), "sem_posicao"}], "regras": [texto]}`, `Sentinela = {"participante_id", "time", "t_ms", "tipo" (wardType cru), "tipo_nome", "comum" (um dos 4 tipos conhecidos; falso = tipo cru como UNDEFINED), "rastreado", "x", "y", "raio" (null = sem lugar), "zona", "metade_inimiga", "ate_ms" (null = sem prazo conhecido), "fim": "expirou"|"destruida"|"substituida"|"pode_estar_ativa"|"desconhecido", "vida" (frase), "aproximado", "destruida_por"}`, `Destruicao = {"participante_id", "time", "t_ms", "tipo", "tipo_nome", "casada" (índice em `sentinelas` ou null)}`

`Queda = {"t_ms", "sub" (EARTH_DRAGON...), "time", "acumulo" (quantos do tipo o time tem depois desta), "atributos": [nome], "medidos", "degraus": [Degrau]}`, `Degrau = {"participante_id", "atributo": "ad"|"ap"|"armadura"|"rm"|"vel_ataque"|"vel_mov", "antes", "depois", "degrau", "percentual" (depois/antes − 1; velocidade de movimento sem o teto suave), "por_acumulo", "por_acumulo_min", "por_acumulo_max" (fração do total por dragão; faixa do arredondamento da tela), "antes_de_ms", "antes_ate_ms", "depois_de_ms", "depois_ate_ms", "confianca": "alta"|"media"|"nenhuma", "nota"}`, `ResumoBonus = {"sub", "atributo", "tabela", "medido"|null, "min", "max", "n", "confianca", "usado" (o que a tela usa), "fonte": "medido"|"tabela"}`. Regras da medição em `dados.md` ("Bônus dos objetivos").

`AtributosCampeao = {"chave", "nome", "ad", "ad_nivel", "armadura", "armadura_nivel", "rm", "rm_nivel", "vel_ataque", "vel_ataque_razao", "vel_ataque_nivel" (pontos % por nível), "vel_mov", "fonte": "jogo"|"data_dragon"}`, `ItemEstatico = {"nome", "custo" (total), "atributos": {"ad", "ap", "armadura", "rm", "vel_ataque_pct", "vel_mov", "vel_mov_pct", "vida", "mana"} (ausente = 0; % em fração), "linhas": [texto dos atributos como o jogo escreve], "descricao" (efeitos, sem marcação)}`, `EventoBuild = {"t_ms", "tipo": "compra"|"venda"|"item", "item_id", "nome", "custo"|null, "ouro_itens"|null (custo somado do inventário depois do evento)}`

`Trecho = {"participante_id", "t_ms", "fim_ms", "atividade" (vocabulário em `dados.md`, seção Atividade: morto, na_base, recall, dive, lutando, trocando, objetivo, split_push, torre, agrupado, invadindo, farmando, coletando_ouro, gankando, counter_gank, roaming, rotacionando, andando, parado), "com": [ids], "alvo"? (objetivo, torre, dive, split_push, invadindo), "farmando"? (coletando_ouro), "zona", "confianca": "alta"|"media"|"baixa", "sinais": [texto]}`

## Estudo — tarefa 4

| Método | Rota | Resposta |
|---|---|---|
| GET | `/api/estudos` | `{"estudos": [Estudo]}` (o mais mexido primeiro). |
| POST | `/api/estudos` `{"nome", "puuid", "campeao_id"?, "posicao"?}` | `201 {"estudo": Estudo}`. 400 sem nome/puuid ou posição inválida. |
| GET | `/api/estudos/{id}` | `{"estudo": Estudo, "partidas": [PartidaDoEstudo]}`. 404. |
| PATCH | `/api/estudos/{id}` `{"nome"?, "campeao_id"?, "posicao"?}` | `{"estudo": Estudo}`. |
| DELETE | `/api/estudos/{id}` | 204 (leva a lista junto). |
| GET | `/api/estudos/{id}/sugestoes` | `{"partidas": [PartidaDoEstudo + "tem_linha_do_tempo"]}`: partidas do jogador no banco, fora da lista, que cabem no filtro. |
| POST | `/api/estudos/{id}/partidas/{game_id}` `{"anotacao"?}` | `201 {"partida": PartidaDoEstudo}`. Resultado vem dos dados da partida. Analisa a partida se ainda não foi. 400 o jogador não está nela; 404 estudo/partida; 409 já está. |
| PUT | `/api/estudos/{id}/partidas/{game_id}` `{"anotacao"}` | `{"partida": PartidaDoEstudo}` (vazio apaga a anotação). 404 se não está na lista. |
| DELETE | `/api/estudos/{id}/partidas/{game_id}` | 204. |
| GET | `/api/estudos/{id}/comparacao` | `Comparacao`: para cada indicador com valor, vitórias × derrotas, ordenado pelo tamanho de efeito (insuficientes no fim). |
| GET | `/api/estudos/{id}/indicadores/{nome}` | `{"indicador": DefIndicador, "partidas": [{"game_id", "resultado", "participante_id", "criada_em", "campeao", "valor", "aproximado", "momentos": [{t_ms, texto}], "detalhe"}]}`: o que compõe o indicador em cada partida (ex.: "morreu fora da rota aos 8:48"). 400 indicador desconhecido. |

`Estudo = {"id", "nome", "puuid", "jogador": "Nome#tag", "campeao_id", "campeao", "posicao", "criado_em", "atualizado_em", "vitorias", "derrotas"}`

`PartidaDoEstudo = {"game_id", "resultado": "vitoria"|"derrota", "participante_id", "campeao_id", "campeao", "posicao", "abates", "mortes", "assistencias", "criada_em", "duracao_s", "anotacao", "adicionada_em", "atualizada_em", "fora_do_filtro", "analisada", "rastreado", "rastreados": [ids]}`

`Comparacao = {"estudo", "vitorias", "derrotas", "sem_analise": [game_id], "ressalva": texto, "linhas": [{"indicador", "rotulo", "unidade", "fonte", "melhor", "vitorias": Lado, "derrotas": Lado, "diferenca" (média V − média D), "efeito" (d de Cohen; null sem variação ou sem um lado), "insuficiente" (< 3 partidas num lado), "aproximado", "observacao"}]}`, `Lado = {"n", "media", "mediana", "min", "max", "desvio"}`. **Não é causa**: a `ressalva` vai sempre.

## Aprendizado por elo

Uma **coleta** monta sozinha um conjunto de partidas de referência por elo (fila ranqueada solo, 420) e rastreia cada uma.
É uma execução com eventos por SSE, **uma por vez** (com uma rodando, criar ou retomar outra é 409), e sobrevive a reinício
do servidor: `rodando` vira `pausado`. Passos: (1) **descoberta** — os melhores do elo pela API da Riot (a chave de
`token.txt`; nunca pelo LCU), o nome pela conta da Riot, o jogador e o histórico pelo LCU **pelo Riot ID** (o puuid da Riot não
é usado no LCU), escolha das partidas; (2) **rastreio** — uma partida por vez: baixa o replay se preciso, rastreia pelo motor
do `rastreio` (todas as regras dele valem: fase do gameflow, replay fechado ao terminar/falhar/cancelar) e segue. Partida que
falha (replay indisponível, outro patch...) fica com `erro` e a coleta segue.

**Região**: por enquanto só `BR1` (Brasil). A região da coleta precisa ser a do cliente do LoL (o replay só abre para
partidas da plataforma da conta logada): conferida antes de começar e a cada retomada.

| Método | Rota | Resposta |
|---|---|---|
| GET | `/api/aprendizado/regioes` | `{"regioes": [{"codigo": "BR1", "nome": "Brasil (BR)", "regiao_cliente": "BR", "habilitada": true}]}` |
| GET | `/api/aprendizado/coletas` | `{"coletas": [Coleta]}`, da mais nova à mais velha. |
| POST | `/api/aprendizado/coletas` `{"regiao": "BR1"?, "elos": [ELO]?, "partidas_por_jogador": 1?, "quem": "jogador"\|"todos"?, "velocidade": 4?, "so_descobrir": false?, "funcao": ""?}` | `202 {"coleta": Coleta, "execucao": {"id"}}`. `funcao` (`TOP JUNGLE MIDDLE BOTTOM UTILITY`, vazio = qualquer): só entra quem tem essa função principal; os outros contam em `outra_funcao`. Padrão: BR1, os 8 elos, 1 partida (1–3), só o jogador, 4× (0,5–8). `so_descobrir`: só a descoberta (jogadores com os números, a função principal e as partidas rastreáveis de cada um); nenhuma partida é escolhida nem lida inteira, nenhum replay é baixado ou aberto; a coleta termina `concluido` ao fim da descoberta. 400 região que não é a BR (`por enquanto só a região BR`), elo desconhecido/repetido, números fora; 409 sem chave da Riot ou chave recusada (renove `token.txt`), região do cliente diferente da coleta, outra coleta rodando; 503 cliente do LoL fechado. |
| GET | `/api/aprendizado/coletas/{id}` | `{"coleta": Coleta, "elos": [EloDaColeta], "jogadores": [JogadorDaColeta], "partidas": [PartidaDaColeta]}`. 404. |
| POST | `/api/aprendizado/coletas/{id}/pausar` | 204. **Pausa graciosa**: o passe em andamento termina normalmente (compactação e cadeia), o replay fecha e a coleta fica `pausado`; na descoberta, para depois do jogador em análise; no download, depois de baixar. `pausa_pedida` fica `true` até parar. Já pausada: 204. 409 se terminou. |
| POST | `/api/aprendizado/coletas/{id}/retomar` | `202 {"execucao": {"id"}}`. Continua do próximo jogador/partida/passe pendente. Vale para `pausado` e `falhou`. 409 nos outros estados, com outra coleta ou rastreio rodando, sem chave (se ainda falta descobrir) ou região diferente; 503 cliente fechado. |
| POST | `/api/aprendizado/coletas/{id}/cancelar` | 204. Para já (o passe em andamento é fechado como cancelado, como no rastreio); o que falta vira `cancelada`. 409 se já concluiu. |
| GET | `/api/aprendizado/coletas/{id}/eventos` | SSE da execução mais recente da coleta. 404 se não houve execução neste processo. |
| POST | `/api/aprendizado/jogadores/{id}/estudo` `{"por_lado": 3?, "nome"?}` | `201 {"estudo": Estudo, "funcao", "vitorias", "derrotas", "faltam_vitorias", "faltam_derrotas", "aviso"}`. Cria um **estudo** do jogador da coleta: do histórico do LCU (rota permitida), as ranqueadas solo **rastreáveis** (patch instalado, ≥ 15 min, sem rendição antecipada) na **função principal** dele; as `por_lado` (3–5) melhores vitórias pela pontuação da coleta e as `por_lado` derrotas mais recentes. Guarda a linha do tempo de cada uma (LCU) e põe no estudo por `estudo.AdicionaPartida` (que analisa). Com menos de 3 de um lado, cria assim mesmo e `aviso` diz quantas faltam para a comparação valer. 400 `por_lado` fora; 404 jogador; 503 cliente fechado. |
| GET | `/api/estudos/{id}/rastreio` | `{"coleta": Coleta\|null, "partidas": [PartidaDaColeta], "pendentes"}`: o último rastreio das partidas do estudo (é uma coleta com `estudo_id`, sem descoberta) e quantas partidas do estudo ainda não têm o jogador rastreado. 404 estudo. Servida pelo pacote `aprendizado`. |
| POST | `/api/estudos/{id}/rastreio` `{"velocidade": 4?}` | `202 {"rastreio": {...como o GET}, "execucao": {"id"}}`. Rastreia, **uma por vez**, as partidas do estudo em que o jogador ainda não foi rastreado (só ele: um passe por partida), baixando o replay quando precisa, pela mesma máquina da coleta (pausa graciosa ao fim do passe, retomada, cancelamento, SSE: use as rotas `/api/aprendizado/coletas/{coleta.id}/pausar|cancelar|eventos`). Se o último rastreio do estudo está `pausado`/`falhou`, retoma esse. Estimativa: duração de cada partida ÷ velocidade + 15 s (~7,5 min por partida de 30 min a 4×). 400 velocidade fora (0,5–8); 404 estudo; 409 sem partidas, todas já rastreadas, já rodando, outra coleta ou rastreio rodando; 503 cliente fechado. Não confere a região (as partidas vieram do próprio cliente). |
| GET | `/api/aprendizado/coletas/{id}/espaco` | `{"compactado", "cadeia", "retratos", "indicadores", "total"}` (bytes, aproximados: o tamanho dos dados nas linhas, sem a sobra do SQLite) `"passes", "partidas", "estimativa_total"` (bytes ao terminar, pela média dos passes já feitos ou, sem nenhum, pela medida de referência). |

`Coleta = {"id", "regiao", "regiao_nome", "elos": [ELO], "partidas_por_jogador", "quem", "velocidade", "so_descobrir", "funcao", "estudo_id" (0 = coleta do aprendizado; > 0 = rastreio das partidas desse estudo, fora de `GET .../coletas`), "estado": "rodando"|"pausado"|"concluido"|"cancelado"|"falhou", "fase": "descoberta"|"rastreio"|"fim", "pausa_pedida", "erro", "criado_em", "atualizado_em", "terminado_em", "jogadores", "jogadores_sem_partida", "partidas_total", "partidas_concluidas", "partidas_com_erro", "passes_total" (estimado enquanto há elo por descobrir), "passes_concluidos", "progresso": 0–1, "estimativa_s", "partida_atual": game_id|null, "rastreio_atual": id|null, "execucao": {"id"}|null}`

`ELO` = `SILVER GOLD PLATINUM EMERALD DIAMOND MASTER GRANDMASTER CHALLENGER` (nada abaixo de Prata).

`EloDaColeta = {"elo", "nome" (Prata...), "exige_mono" (Mestre para cima), "estado": "pendente"|"descobrindo"|"pronto", "candidatos_vistos", "jogadores", "descartes": {"inativo", "veterano", "poucas_partidas", "travado"` (da filtragem da liga) `, "nao_achado", "nao_mono", "sem_historico", "sem_partida"` (dos candidatos olhados; `sem_partida` = sem partida rastreável, não ocupa vaga; `outra_funcao` = função principal diferente da pedida) `}, "aviso", "partidas_total", "partidas_concluidas"}`

`JogadorDaColeta = {"id", "regiao", "elo", "posicao" (1 = primeiro na ordem de subida), "lp", "divisao" (I–IV; vazio de Mestre para cima), "vitorias", "derrotas", "taxa_vitoria" (0–1), "fresh_blood", "veteran", "hot_streak", "pontuacao_subida", "motivo" ("subindo: 63% de vitórias em 84 partidas, recém-chegado ao elo"), "nome", "tag", "riot_id", "campeao_id", "campeao", "fracao_campeao" (0–1, nas ranqueadas solo do histórico do LCU), "partidas_solo", "mono", "funcao": "TOP"|"JUNGLE"|"MIDDLE"|"BOTTOM"|"UTILITY"|"" (a posição mais jogada nas ranqueadas solo do histórico, pela ordem do participante), "fracao_funcao" (0–1), "vitorias_rastreaveis", "derrotas_rastreaveis" (no histórico, na função principal), "estado": "com_partidas"|"sem_partida"}` (`sem_partida` só em coletas antigas: hoje quem não tem partida rastreável não entra)

`PartidaDaColeta = {"id", "regiao", "jogador_id", "elo", "game_id", "participante_id", "campeao_id", "campeao", "abates", "mortes", "assistencias", "duracao_s", "pontuacao", "estado": "pendente"|"baixando"|"rastreando"|"pausada"|"concluida"|"erro"|"cancelada", "rastreio_id"|null, "passes_total", "passes_concluidos", "erro"}`

Eventos SSE: `fase {fase}`, `elo_inicio {elo}`, `candidato {elo, posicao, lp, divisao, vitorias, derrotas, pontuacao_subida, riot_id?, resultado: "aceito"|"nao_achado"|"nao_mono"|"sem_historico", campeao?, fracao?}`, `elo_fim {elo, jogadores, aviso?}`, `partida_inicio {partida_id, game_id, elo, riot_id}`, `download {game_id, estado, progresso}`, `rastreio {partida_id, rastreio_id, execucao}` (a tela pode seguir o SSE do rastreio), `partida_fim {partida_id, estado, erro?}`, `pausa_pedida`, `pausado {motivo?}`, `concluido`, `aviso`, `erro`, `fim`.

**Quem entra** não é quem tem mais LP, e sim quem tem o comportamento de quem **sobe** (o jogador travado no Ouro 1 a temporada
toda ensina a ficar no Ouro). A Riot não informa o elo anterior: o critério usa só o que cada entrada da league-v4 traz
(`server/aprendizado/subida.go`, limiares em constantes). Prata a Diamante: divisões I a IV, 2 páginas de cada (**aproximação**:
não é a divisão inteira); descarta `inactive`, `veteran`, menos de 30 partidas e o travado (mais de 250 partidas com menos de 53%
de vitórias); ordena pelo limite inferior de Wilson (95%) da taxa de vitória + 0,03 se `freshBlood` + 0,02 se `hotStreak`; LP só
desempata; os 10 primeiros achados no cliente **com partida rastreável** (quem não tem nenhuma vitória rastreável no histórico conta em `sem_partida` e a descoberta segue descendo, no máximo 60 olhados). Mestre, Grão-Mestre e Desafiante: a liga inteira, sem `inactive`, `veteran` no fim
da fila, a mesma pontuação, e só *mono champion* (nas ranqueadas solo do histórico do LCU, ≥ 10 partidas e ≥ 60% no mesmo
campeão), descendo até achar 10 (no máximo 60 candidatos olhados por elo). **Partidas**: fila 420, patch do jogo instalado (só essas têm replay), vitória, ≥ 15 min, sem
rendição antecipada; do mono, no campeão principal. Pontuação 0–100: 35% KDA (teto 10), 25% participação em abates, 20%
CS/min (teto 10), 20% dano a campeões/min (teto 1500). Ficam as `partidas_por_jogador` maiores.

## O que a tarefa 5 usa em cada tela

- **Partida rastreada**: `GET /api/partidas/{id}` (os 10), `GET .../analise` (e `POST` para refazer), `GET .../abates` (lista e
  retrato de cada abate: posição/zona/vida/recargas dos 10, superioridade, dano da Riot), `GET .../saidas-de-rota` (faixas no
  gráfico de vida), `GET .../amostras?participante=&passo=1` (vida ao longo da partida), `GET .../cadeia?participante=&tipos=`
  (mortes, habilidades, compras, mudou_zona), `GET .../indicadores?participante=`, `GET .../cs`, `GET /api/mapa/zonas` (mapa do
  momento), imagens.
- **Estudo**: `GET/POST /api/estudos`, `GET /api/estudos/{id}` (lista com anotações), `.../sugestoes`, `POST/PUT/DELETE
  .../partidas/{game_id}`, `GET .../comparacao` (tabela), `GET .../indicadores/{nome}` (exemplos ao clicar numa linha),
  `GET /api/indicadores` (rótulos/unidades).
- **Mapa com play** (tela cheia): `GET /api/mapa/zonas` (grade e cores), `GET .../estados?de=&ate=&passo=0.25` (os 10 por
  janela: posição, vida %, morto, itens; painel do seguido), `GET .../atividades?de=&ate=` (o rótulo de cada um no instante),
  `GET .../momento?t=` (clique: vida, mana, recargas, atributos, itens), `GET .../eventos`, `GET .../ondas` (caixa "ondas" e o placar), `GET .../visao` (caixa "visão": quem o time escolhido via em cada instante; e as sentinelas lidas do minimapa, que substituem as estimadas na caixa "sentinelas"), `GET .../estaticos` e `GET .../bonus-objetivos`
  (decomposição dos atributos, ícone do dragão, detalhe dos itens), `/api/imagens/campeao/{campeao_id}`
  e `/api/imagens/item/{id}`. A zona de cada um no instante: pela `grade` (ou pelos `mudou_zona` da cadeia).

## Previstas (não implementadas)

| Método | Rota | Ideia |
|---|---|---|
| POST | `/api/visao/ler` (multipart com a captura) | lê uma captura guardada e devolve o que reconheceu; para calibrar e testar sem o jogo (tarefa 3, não feita). |
