# Bloco compactado + cadeia reduzida no lugar das amostras cruas

Data: 2026-11-17 · Tarefa 2 (rastreio) · Pedido do dono do projeto.

## Contexto

Um passe a 4 amostras por segundo de jogo dá ~9 400 leituras por jogador numa partida de 39 min (~94 000 os 10), com ~35
colunas cada, a maioria nula ou quase constante. Guardar isso como linhas no SQLite custaria dezenas de MB por partida e não
é o que as telas usam: a tela normal conta "como foi o jogo" (mortes, abates, itens, trajeto), e o mapa com play precisa de
todas as posições, mas lidas em bloco por janela de tempo.

## Decisão

- Durante o passe as leituras vão para tabelas **de trabalho** (`amostras_brutas`, `amostras_time_brutas`), só para pausar
  e retomar.
- Ao fechar o passe, na mesma transação: **bloco compactado** (`passes_compactos`, formato colunar próprio: delta, delta² ou
  corridas por coluna, zigzag + varint, máscara de nulos em corridas, DEFLATE nível 9 por cima, cabeçalho com versão e
  colunas nomeadas) + **cadeia reduzida** (`cadeia`, itens com tipo de vocabulário aberto) e as linhas de trabalho somem.
- As rotas de amostras e do estado dos 10 decodificam o bloco (cache em memória de 24 passes); a cadeia tem rota própria.
- A cadeia sai do bloco já decodificado, e `RegeraCadeia(passe)` a refaz com os geradores registrados: a tarefa 4 acrescenta
  tipos sem reabrir o replay.

## Por quê

- **Tamanho:** ~7 bytes por amostra medidos numa partida real (0,7 MB por partida inteira, os 10 passes), contra centenas
  de bytes por linha no SQLite com índice.
- **Nada se perde do que foi lido:** posição e câmera em unidade inteira do mapa, tela em décimo de pixel, tempos em ms,
  respawn e ward em centésimos, recargas em décimos de segundo. O ida e volta tem teste, inclusive nulos.
- **Evolui sem migração:** coluna é achada pelo nome; blob antigo sem a coluna lê como nulo. A tarefa 3 só acrescenta as
  definições das colunas da tela.
- **Sem dependência nova:** `compress/flate` da stdlib. O zstd puro-Go (`klauspost/compress`) não foi medido; com o corpo já
  em delta+varint o que sobra para o compressor é pouco, e a meta de tamanho já foi atingida.

## Consequências

- Ler uma amostra isolada exige decodificar o passe inteiro (~10 ms); por isso o cache.
- Passe pausado continua nas tabelas de trabalho até ser retomado ou cancelado (cancelar também compacta).
- Mudar a codificação de uma coluna existente exige subir `VersaoFormato` e manter o decodificador antigo.
