# Atividade deduzida por regra, com motivo e confiança

Data: 2026-11-17 · Tarefa 4 (análise).

## Contexto

O dono quer saber quando o campeão está atacando/soltando magia e o quê. O replay não informa o alvo de cada ataque e ler a
memória do jogo é proibido (CLAUDE.md, regra 4). Uso de habilidade já se sabe com precisão (ícone pronto → recarga, tarefa 3).

## Decisão

- Um gerador da cadeia (`atividade`) classifica cada amostra por uma lista de regras em ordem (morto, na base, lutando,
  trocando, objetivo/torre, andando por deslocamento, farmando, andando) e junta em trechos. Cada trecho leva `sinais` (frases:
  "inimigo 8 a 379", "vida do 8 caindo 26%", "CS 6/min no minuto") e `confianca` (alta/média/baixa). Regra completa e
  limiares em `dados.md` e `server/analise/parametros.go`.
- "Perto de quem" só usa posição de **jogador rastreado** (amostras do passe dele). Por isso a cadeia de todos os passes da
  partida é refeita quando um passe novo fecha (`AoFecharPasse` → `Recalcula`).
- Nos retratos dos abates, quem não foi rastreado ganha posição aproximada: o local do abate em que matou/ajudou a até 8 s,
  senão a interpolação da linha do tempo (de minuto em minuto). Tudo marcado `aproximada`.
- "Quem bateu com o quê" exato só existe em volta das mortes: `victimDamageReceived` da linha do tempo da Riot (com chave).

## Limites (honestos)

- Com só 1 ou 2 rastreados, quase todo inimigo é invisível: luta contra não rastreado vira, no máximo, `trocando` com `com: []`
  e confiança baixa (rotador fora da rota com a vida caindo forte). Na partida de teste, a morte do Akshan aos 8:48 para
  Warwick e Teemo (não rastreados) aparece assim.
- `farmando` vem do CS por minuto da linha do tempo (o `creepScore` do jogo anda de 10 em 10): o minuto inteiro em que o CS
  subiu e ele estava parado na rota/selva vira farmando. Não distingue último golpe de andar entre as tropas.
- Dano de tropa e de monstro derruba a vida também: na rota a regra só chama de troca com um inimigo rastreado perto.
- `objetivo`/`torre` dependem do evento (monstro grande, torre, placa da Riot) acontecer até 40/20 s depois e a ≤ 1500.
- É dedução para estudo, não registro: a tela deve mostrar o motivo e a confiança junto.
