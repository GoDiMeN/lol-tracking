// Funções puras da tela do RANKING do Lane (views/ranking.js). Testadas em ranking.test.mjs.

export const ELOS = ['SILVER', 'GOLD', 'PLATINUM', 'EMERALD', 'DIAMOND', 'MASTER', 'GRANDMASTER', 'CHALLENGER'];
export const NOME_ELO = {
  SILVER: 'Prata', GOLD: 'Ouro', PLATINUM: 'Platina', EMERALD: 'Esmeralda', DIAMOND: 'Diamante',
  MASTER: 'Mestre', GRANDMASTER: 'Grão-Mestre', CHALLENGER: 'Desafiante',
};
export const ROTAS = ['TOP', 'JUNGLE', 'MIDDLE', 'BOTTOM', 'UTILITY'];
export const NOME_ROTA = { TOP: 'topo', JUNGLE: 'selva', MIDDLE: 'meio', BOTTOM: 'atirador', UTILITY: 'suporte' };
export const MODOS = { subindo: 'subindo', melhor: 'melhor' };

// O recorte na rota: "JUNGLE" (rota) ou "c64" (campeão 64).
export function lerRecorte(texto) {
  const t = String(texto ?? '').trim();
  if (/^c\d+$/.test(t)) return { rota: '', campeao: Number(t.slice(1)) };
  if (ROTAS.includes(t.toUpperCase())) return { rota: t.toUpperCase(), campeao: 0 };
  return { rota: 'JUNGLE', campeao: 0 };
}

export function textoRecorte(r) {
  return r.campeao ? `c${r.campeao}` : r.rota;
}

// A consulta de GET /api/rankings/recorte.
export function consultaRecorte({ patch, elo, recorte, modo }) {
  const r = lerRecorte(recorte);
  const alvo = r.campeao ? `campeao=${r.campeao}` : `rota=${encodeURIComponent(r.rota)}`;
  return `patch=${encodeURIComponent(patch)}&elo=${encodeURIComponent(elo)}&${alvo}&modo=${encodeURIComponent(modo || 'subindo')}`;
}

// O nome do recorte de um cabeçalho: "selva" ou "Lee Sin".
export function nomeDoRanking(rk) {
  if (!rk) return '';
  return rk.tipo === 'campeao' ? rk.campeao || `campeão ${rk.campeao_id}` : NOME_ROTA[rk.rota] || rk.rota;
}

const MOTIVOS = {
  inativo: 'inativos (parados)',
  nao_achado: 'não achados (conta trocada ou sumiu)',
  sem_historico: 'histórico não veio',
  sem_partida_no_patch: 'sem ranqueada solo no patch',
  sem_o_recorte: 'não jogaram o recorte no patch',
  poucas_no_recorte: 'menos de 3 partidas no recorte',
};

// "12 não jogaram a selva no patch, 4 não achados..." na ordem de mais para menos.
export function textoFora(fora) {
  const l = Object.entries(fora || {}).filter(([, n]) => n > 0);
  l.sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]));
  return l.map(([k, n]) => `${n} ${MOTIVOS[k] || k}`).join(' · ');
}

export function formataNota(n) {
  return Number(n || 0).toLocaleString('pt-BR', { minimumFractionDigits: 1, maximumFractionDigits: 1 });
}

// Os selos de uma posição.
export function selosPosicao(p) {
  const s = [];
  if (p.empatado) s.push({ texto: 'empate', classe: 'at' });
  if (p.amostra_pequena) s.push({ texto: 'amostra pequena', classe: 'at' });
  if (p.numeros?.hot_streak) s.push({ texto: 'sequência', classe: 'ok' });
  if (p.numeros?.fresh_blood) s.push({ texto: 'recém-chegado', classe: 'ac' });
  if (p.numeros?.veteran) s.push({ texto: 'veterano', classe: 'n' });
  return s;
}

// O texto da liga: "Platina II 45 PDL" ou "1234 PDL".
export function textoLiga(elo, n) {
  if (!n) return '';
  return n.divisao ? `${NOME_ELO[elo] || elo} ${n.divisao} ${n.lp} PDL` : `${n.lp} PDL`;
}

export const ESTADOS_ATUALIZACAO = {
  rodando: { texto: 'rodando', classe: 'ac' },
  pausado: { texto: 'pausada', classe: 'at' },
  concluido: { texto: 'concluída', classe: 'ok' },
  cancelado: { texto: 'cancelada', classe: 'n' },
  falhou: { texto: 'parou com erro', classe: 'cr' },
};

// "Ouro · 16.23 · 12 de 60 candidatos · 230 pedidos (40 do cache)".
export function resumoAtualizacao(a) {
  const ped = (a.pedidos_liga || 0) + (a.pedidos_conta || 0) + (a.pedidos_ids || 0) + (a.pedidos_partidas || 0);
  const partes = [NOME_ELO[a.elo] || a.elo, a.patch || 'patch a descobrir', `fonte ${a.fonte}`];
  if (a.candidatos) partes.push(`${a.candidatos_vistos} de ${a.candidatos} candidatos`);
  partes.push(`${ped} pedidos à Riot${a.do_cache ? ` (${a.do_cache} do cache)` : ''}`);
  return partes.join(' · ');
}

// O corpo de "rastrear os N primeiros" (POST /api/aprendizado/coletas): sempre os 10 de cada partida.
export function corpoRastrear({ elo, patch, modo, primeiros, rotas }) {
  return { elos: [elo], funcoes: rotas, quem: 'todos', ranking: { patch, modo, primeiros } };
}

// A diferença de posição na comparação: "▲ 3", "▼ 2", "=".
export function deltaPosicao(a, b) {
  if (!a || !b) return '';
  if (a === b) return '=';
  return a > b ? `▲ ${a - b}` : `▼ ${b - a}`;
}
