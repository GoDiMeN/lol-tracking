import test from 'node:test';
import assert from 'node:assert/strict';
import {
  estimativaVisao,
  estimativaVisaoNoRastreio,
  estadoEm,
  estadosNoInstante,
  sentinelasLidasEm,
  temSentinelasLidas,
  tituloEstado,
  tituloSentinelaLida,
} from './visao.js';

const visao = {
  times: [
    {
      time: 100,
      jogadores: [
        { participante_id: 6, visivel: [[0, 1000], [5000, 6000]], fora: [[1000, 5000]], indeterminado: [[6000, 7000]] },
        { participante_id: 7, visivel: [], fora: [[0, 9000]], indeterminado: [] },
      ],
      sentinelas: [{ x: 6402, y: 8325, de_ms: 2000, ate_ms: 8000, capturas: 24, ate_o_fim: false }],
    },
    { time: 200, jogadores: [{ participante_id: 1, visivel: [[0, 9000]], fora: [], indeterminado: [] }], sentinelas: [] },
  ],
};

test('estimativa: 2 passes a 8× mais as folgas (como o servidor)', () => {
  // 30 min a 8×: 2 × (225 + 10) + 40 = 510 s.
  assert.equal(estimativaVisao({ duracaoS: 1800 }), 510);
  assert.equal(estimativaVisaoNoRastreio({ duracaoS: 1800 }), 470);
  assert.equal(estimativaVisao({ duracaoS: 1800, deS: 540, ateS: 660, velocidade: 4 }), 2 * (30 + 10) + 40);
});

test('estado no instante: intervalos [de, ate)', () => {
  const j = visao.times[0].jogadores[0];
  assert.equal(estadoEm(j, 0), 'visivel');
  assert.equal(estadoEm(j, 1000), 'fora');
  assert.equal(estadoEm(j, 5999), 'visivel');
  assert.equal(estadoEm(j, 6500), 'indeterminado');
  assert.equal(estadoEm(j, 7000), '');
  assert.equal(estadoEm(null, 0), '');
});

test('estados dos inimigos do observador', () => {
  const m = estadosNoInstante(visao, 100, 3000);
  assert.equal(m.get(6), 'fora');
  assert.equal(m.get(7), 'fora');
  assert.equal(m.has(1), false);
  assert.equal(estadosNoInstante(visao, 200, 3000).get(1), 'visivel');
  assert.equal(estadosNoInstante(null, 100, 0).size, 0);
});

test('sentinelas lidas no instante, com o time dono', () => {
  assert.equal(temSentinelasLidas(visao), true);
  assert.equal(temSentinelasLidas({ times: [{ sentinelas: [] }] }), false);
  assert.deepEqual(sentinelasLidasEm(visao, 1000), []);
  const s = sentinelasLidasEm(visao, 3000);
  assert.equal(s.length, 1);
  assert.equal(s[0].time, 100);
  assert.match(tituloSentinelaLida(s[0]), /time azul lida no minimapa .* de 0:02 até 0:08/);
});

test('dicas dizem de quem é a visão', () => {
  assert.match(tituloEstado('fora', 200), /time vermelho NÃO via/);
  assert.match(tituloEstado('indeterminado', 100), /incerta/);
  assert.equal(tituloEstado('', 100), '');
});
