// Funções puras da lista de partidas rastreadas (rastreadas.test.mjs): a ORIGEM de cada partida
// (pesquisa = veio da busca de um jogador; estudo = coleta por elo, estudo, ranking), o jogador em
// foco de cada linha e os filtros da tela "Partidas rastreadas". Regra no servidor:
// server/rastreio/origem.go.

export const ROTAS_FILTRO = [
  ['', 'todas as rotas'],
  ['TOP', 'topo'],
  ['JUNGLE', 'selva'],
  ['MIDDLE', 'meio'],
  ['BOTTOM', 'atirador'],
  ['UTILITY', 'suporte'],
];

export const RESULTADOS_FILTRO = [
  ['', 'vitórias e derrotas'],
  ['vitoria', 'vitórias'],
  ['derrota', 'derrotas'],
];

// consultaRastreadas: "?origem=todas&puuid=..." (vazio = o padrão do servidor: só pesquisa).
export function consultaRastreadas({ origem = '', puuid = '' } = {}) {
  const q = [];
  if (origem) q.push(`origem=${encodeURIComponent(origem)}`);
  if (puuid) q.push(`puuid=${encodeURIComponent(puuid)}`);
  return q.length ? `?${q.join('&')}` : '';
}

// participanteEmFoco: o participante do ponto de vista da linha. O foco que o servidor mandou
// (o jogador pesquisado, ou a referência do estudo); sem ele, o primeiro rastreado; senão o 1.
export function participanteEmFoco(linha, participantes) {
  const lista = participantes || [];
  const porId = (id) => lista.find((p) => p.participante_id === id);
  return (
    (linha?.foco?.participante_id && porId(linha.foco.participante_id)) ||
    (linha?.participantes || []).map(porId).find(Boolean) ||
    lista[0] ||
    null
  );
}

// passaFiltros: a linha (com o participante em foco) passa no resultado e na rota escolhidos?
export function passaFiltros(foco, { resultado = '', rota = '' } = {}) {
  if (!foco) return !resultado && !rota;
  if (resultado === 'vitoria' && !foco.venceu) return false;
  if (resultado === 'derrota' && foco.venceu) return false;
  if (rota && foco.posicao !== rota) return false;
  return true;
}

// opcoesJogadores: as opções do filtro por jogador ("todos" primeiro, com o total).
export function opcoesJogadores(jogadores, total) {
  return [['', `todos os jogadores (${total})`], ...(jogadores || []).map((j) => [j.puuid, `${j.riot_id} (${j.partidas})`])];
}

// escolhaValida: a escolha guardada só vale se o jogador ainda está na lista.
export function escolhaValida(puuid, jogadores) {
  return puuid && (jogadores || []).some((j) => j.puuid === puuid) ? puuid : '';
}

// ehSoDeEstudo: a partida só entrou pelo estudo (não veio da busca de um jogador).
export function ehSoDeEstudo(linha) {
  const o = linha?.origens || [];
  return o.includes('estudo') && !o.includes('pesquisa');
}

// voltaDaPartida: de onde a pessoa veio. Partida só de estudo volta para o Estudo; as outras, para
// "Partidas rastreadas".
export function voltaDaPartida(linha) {
  return ehSoDeEstudo(linha) ? { texto: '← Estudo', href: '#estudo' } : { texto: '← Partidas rastreadas', href: '#partidas' };
}
