import test from 'node:test';
import assert from 'node:assert/strict';
import { vivaEm, noMapa, colocadasAte, tituloSentinela } from './sentinelas.js';

const lista = [
  { participante_id: 7, time: 200, t_ms: 166737, tipo: 'YELLOW_TRINKET', tipo_nome: 'amarela (Totem)', x: 11627, y: 6588, raio: 700, ate_ms: 258000, fim: 'expirou', aproximado: true },
  { participante_id: 7, time: 200, t_ms: 436474, tipo: 'CONTROL_WARD', tipo_nome: 'de controle', x: 6251, y: 10993, raio: 700, ate_ms: 943000, fim: 'destruida', aproximado: true, destruida_por: 5 },
  { participante_id: 5, time: 100, t_ms: 500000, tipo: 'CONTROL_WARD', tipo_nome: 'de controle', x: null, y: null, raio: null, ate_ms: null, fim: 'pode_estar_ativa', aproximado: true },
  { participante_id: 3, time: 100, t_ms: 600000, tipo: 'CONTROL_WARD', tipo_nome: 'de controle', x: 1, y: 2, raio: 700, ate_ms: null, fim: 'pode_estar_ativa', aproximado: true },
];

test('viva até o fim estimado; sem prazo fica viva', () => {
  assert.equal(vivaEm(lista[0], 200000), true);
  assert.equal(vivaEm(lista[0], 258000), false);
  assert.equal(vivaEm(lista[3], 9e9), true);
  assert.equal(vivaEm(lista[0], 100000), false);
});

test('no mapa só as vivas com lugar estimado', () => {
  assert.deepEqual(noMapa(lista, 700000).map((s) => s.participante_id), [7, 3]);
  assert.deepEqual(noMapa(null, 0), []);
});

test('colocadas por time até o instante', () => {
  assert.deepEqual(colocadasAte(lista, 550000), { 100: 1, 200: 2 });
  // O UNDEFINED da Riot (comum: false) aparece no mapa, mas não conta (a regra do servidor).
  assert.deepEqual(colocadasAte([...lista, { t_ms: 1000, time: 100, tipo: 'UNDEFINED', comum: false }], 550000), { 100: 1, 200: 2 });
});

test('título diz quem, quando e que a posição é estimada', () => {
  const t = tituloSentinela(lista[1], { 7: 'Vi', 5: 'Leona' });
  assert.match(t, /^Vi colocou sentinela de controle aos 7:16/);
  assert.match(t, /posição ESTIMADA/);
  assert.match(t, /até 15:43 \(destruída por Leona, aproximado\)/);
});
