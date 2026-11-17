# Zonas do mapa por geometria simples, num arquivo só

Data: 2026-11-17 · Tarefa 4 (análise).

## Contexto

Saída de rota, "onde estava cada um" no abate e o % do tempo por zona precisam dizer, para um ponto (x, y) do jogo, se é rota
(e que trecho), rio, cova, selva de que lado ou base. A Riot não publica os polígonos do Summoner's Rift.

## Decisão

- `analise.Zona(x, y)` com vocabulário fixo (14 zonas + subzonas) e geometria simples: quarto de disco para as bases,
  distância a polilinhas para as rotas e o rio (com meia-largura), círculos para as covas, e o resto é selva pelo lado do rio
  e da diagonal do meio. Ordem: base, rota, cova, rio, selva.
- Só o lado azul é desenhado; o vermelho é o espelho por ponto (`Simetria − p`, medido nos pares de torres). Rota de baixo =
  rota do topo espelhada; rio de baixo = rio de cima espelhado.
- Trechos de rota (lado azul / centro / lado vermelho) divididos pelas torres externas.
- Todas as fronteiras em `server/analise/fronteiras.go`. Conferência visual em `docs/arquitetura/zonas.png`
  (`LOL_GERA_ZONAS=1 go test -run TestGeraImagemZonas ./analise/`), e teste com torres, covas, campos da selva e os 20 pontos
  reais da calibração (aos 4 min todo rotador na própria rota).

## Por quê

- Determinístico, barato (~µs por ponto) e fácil de ajustar olhando a imagem; o erro da posição (~30–100 unidades) é bem menor
  que a largura das faixas.
- A tela recebe a mesma divisão como grade (`GET /api/mapa/zonas`) e não refaz a geometria.

## Consequências / limites

- As faixas são aproximações: a meia-largura do meio (850) foi alargada porque os rotadores do meio encostam no rio perto do
  centro e isso gerava "saídas" de poucos segundos; mesmo assim há saídas curtas para o rio (6–12 s) que são de verdade ou
  borda. O canto das rotas laterais (onde o rio desemboca) conta como rota.
- Outro mapa (ARAM, mapa 12) não tem zonas: só o 11.
