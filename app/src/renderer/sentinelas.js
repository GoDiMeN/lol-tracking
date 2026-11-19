// Sentinelas no mapa com play (GET /api/partidas/{id}/sentinelas; regras em dados.md, "Sentinelas").
// Funções puras, testadas em sentinelas.test.mjs. A posição é ESTIMADA (onde quem colocou estava):
// a tela sempre diz isso, e quem não foi rastreado não aparece no mapa (só na contagem).

import { relogio } from './texto.js';

// vivaEm: a sentinela está no chão no instante t? `ate_ms` nulo = sem prazo conhecido (de controle
// não casada com nenhuma destruição: pode estar ativa até o fim).
export function vivaEm(s, t) {
  return s.t_ms <= t && (s.ate_ms === null || s.ate_ms === undefined || s.ate_ms > t);
}

// noMapa: as vivas em t que têm lugar estimado.
export function noMapa(lista, t) {
  return (lista || []).filter((s) => s.x !== null && s.x !== undefined && vivaEm(s, t));
}

// colocadasAte: quantas cada time colocou até t ({100: n, 200: n}). A regra da contagem é a do
// servidor (analise.ContaComoSentinela): tipo fora dos quatro conhecidos (comum: false, o UNDEFINED
// da Riot) aparece no mapa, mas não conta.
export function colocadasAte(lista, t) {
  const r = { 100: 0, 200: 0 };
  for (const s of lista || []) if (s.t_ms <= t && s.comum !== false && r[s.time] !== undefined) r[s.time]++;
  return r;
}

const FIM = {
  expirou: 'expira',
  destruida: 'destruída',
  substituida: 'derrubada por outra do mesmo jogador',
  pode_estar_ativa: 'sem destruição casada: pode estar ativa',
  desconhecido: 'duração desconhecida',
};

// tituloSentinela: "Leona colocou sentinela de controle aos 7:16 · posição estimada (raio ~700) ·
// fica até 15:43 (destruída por Leona, aproximado)".
export function tituloSentinela(s, nomes = {}) {
  const quem = nomes[s.participante_id] || `jogador ${s.participante_id}`;
  const partes = [`${quem} colocou sentinela ${s.tipo_nome || s.tipo} aos ${relogio(s.t_ms)}`];
  partes.push(`posição ESTIMADA (onde ${quem} estava; raio ~${Math.round(s.raio || 0)})`);
  let ate = s.ate_ms !== null && s.ate_ms !== undefined ? `até ${relogio(s.ate_ms)}` : 'sem prazo';
  ate += ` (${FIM[s.fim] || s.fim}`;
  if (s.fim === 'destruida' && s.destruida_por) ate += ` por ${nomes[s.destruida_por] || `jogador ${s.destruida_por}`}`;
  ate += s.aproximado ? ', aproximado)' : ')';
  partes.push(ate);
  if (s.vida) partes.push(`vida: ${s.vida}`);
  return partes.join(' · ');
}
