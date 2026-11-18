// Ondas de tropas no mapa com play: funções puras sobre GET /api/partidas/{id}/ondas (o servidor
// deduz tudo; aqui só se acha o que vale num instante e se escreve em pt-BR). Sem DOM e sem rede
// (ondas.test.mjs).

export const ROTAS_ONDA = [
  { rota: 'rota_topo', nome: 'topo' },
  { rota: 'rota_meio', nome: 'meio' },
  { rota: 'rota_baixo', nome: 'baixo' },
];

const COR = { 100: 'azul', 200: 'vermelho' };
const TORRE = { 100: 'torre azul', 200: 'torre vermelha' };

// Até onde um ponto estimado vale para o instante (o servidor manda um por segundo; mais longe
// que isso é buraco: ninguém da rota estava lá e a posição é desconhecida).
export const FOLGA_PONTO_MS = 3000;

// pontoNoInstante: a posição estimada da onda em t, interpolada entre os dois pontos vizinhos
// (lista em ordem de tempo). Sem ponto a até FOLGA_PONTO_MS: null.
export function pontoNoInstante(pontos, t) {
  if (!pontos?.length) return null;
  let lo = 0;
  let hi = pontos.length - 1;
  while (lo < hi) {
    const m = (lo + hi + 1) >> 1;
    if (pontos[m].t_ms <= t) lo = m;
    else hi = m - 1;
  }
  const a = pontos[lo].t_ms <= t ? pontos[lo] : null;
  const b = a ? pontos[lo + 1] : pontos[0];
  if (a && b && b.t_ms - a.t_ms <= 2 * FOLGA_PONTO_MS && t <= b.t_ms) {
    const f = b.t_ms > a.t_ms ? (t - a.t_ms) / (b.t_ms - a.t_ms) : 0;
    return {
      x: a.x + (b.x - a.x) * f,
      y: a.y + (b.y - a.y) * f,
      confianca: a.confianca === 'alta' && b.confianca === 'alta' ? 'alta' : 'media',
    };
  }
  const perto = [a, b].filter(Boolean).sort((p, q) => Math.abs(p.t_ms - t) - Math.abs(q.t_ms - t))[0];
  if (!perto || Math.abs(perto.t_ms - t) > FOLGA_PONTO_MS) return null;
  return { x: perto.x, y: perto.y, confianca: perto.confianca };
}

// trechoNoInstante: o estado (trecho de janelas) que vale em t, ou null.
export function trechoNoInstante(estados, t) {
  return (estados || []).find((e) => e.de_ms <= t && t < e.ate_ms) || null;
}

// ondaNoInstante: a última onda que já chegou ao ponto de encontro da rota em t ({n, canhao}).
export function ondaNoInstante(ondas, rota, t) {
  let r = null;
  for (const o of ondas || []) {
    const c = o.chegada_ms?.[rota];
    if (c === undefined || c > t) break;
    r = o;
  }
  return r;
}

// textoEstado: o estado em frase curta ("freeze do azul", "na torre vermelha",
// "azul empurrando rápido").
export function textoEstado(trecho) {
  if (!trecho) return 'sem estimativa';
  const cor = COR[trecho.time];
  switch (trecho.estado) {
    case 'freeze':
      return `freeze do ${cor || '?'}`;
    case 'onda_na_torre':
      return `na ${TORRE[trecho.time] || 'torre'}`;
    case 'onda_voltando':
      return `voltando da ${TORRE[trecho.time] || 'torre'}`;
    case 'empurrando':
      return `${cor || '?'} empurrando${trecho.velocidade === 'rapida' ? ' rápido' : trecho.velocidade === 'lenta' ? ' devagar' : ''}`;
    case 'onda_parada':
      return cor ? `parada perto da ${TORRE[trecho.time]}` : 'parada no meio';
  }
  return String(trecho.estado || '').replaceAll('_', ' ');
}

// textoCurto: a etiqueta pequena do marcador no mapa.
export function textoCurto(trecho) {
  if (!trecho) return '';
  return { freeze: 'freeze', onda_na_torre: 'na torre', onda_voltando: 'voltando', empurrando: 'empurrando', onda_parada: 'parada' }[trecho.estado] || '';
}

// textoOnda: "onda 9 · canhão".
export function textoOnda(onda) {
  if (!onda) return 'antes da 1ª onda';
  return `onda ${onda.n}${onda.canhao ? ' · canhão' : ''}`;
}

// ondasNoInstante: o que desenhar em t, uma entrada por rota.
export function ondasNoInstante(dados, t) {
  if (!dados) return [];
  return ROTAS_ONDA.map(({ rota, nome }) => {
    const r = (dados.rotas || []).find((x) => x.rota === rota);
    const onda = ondaNoInstante(dados.ondas, rota, t);
    // Depois de até onde se estima (14 min ou o fim do rastreio) não há nada.
    const dentro = t <= (dados.ate_ms ?? Infinity);
    const ponto = r && dentro ? pontoNoInstante(r.pontos, t) : null;
    const trecho = r && dentro ? trechoNoInstante(r.estados, t) : null;
    return { rota, nome, onda, ponto, trecho, rastreados: r?.rastreados?.length || 0 };
  });
}
