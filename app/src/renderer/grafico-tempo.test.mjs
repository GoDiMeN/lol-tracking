// Testes das contas dos gráficos ao longo da partida (grafico-tempo.js).
import test from 'node:test';
import assert from 'node:assert/strict';

import { serieCS, csNoInstante, perdidasDoJogador, escala, MARGEM, tetoRedondo } from './grafico-tempo.js';

const lt = {
  frames: [
    { timestamp: 0, participantFrames: { 1: { minionsKilled: 0, jungleMinionsKilled: 0 } } },
    { timestamp: 60000, participantFrames: { 1: { minionsKilled: 4, jungleMinionsKilled: 2 } } },
    { timestamp: 120000, participantFrames: { 1: { minionsKilled: 14, jungleMinionsKilled: 2 } } },
  ],
};

test('CS por minuto da linha do tempo (tropas + monstros) e interpolado no instante', () => {
  const s = serieCS(lt, 1);
  assert.deepEqual(s.map((x) => x.cs), [0, 6, 16]);
  assert.deepEqual(serieCS(lt, 2), []);
  const c = csNoInstante(s, 90000);
  assert.equal(c.cs, 11);
  assert.ok(Math.abs(c.por_min - 11 / 1.5) < 1e-9);
  assert.equal(csNoInstante(s, 0).por_min, null);
});

test('escala em px de verdade: margens fixas e marcas a cada ≥ 70 px', () => {
  const e = escala(1800000, 1000);
  assert.equal(e.x(0), MARGEM.esq);
  assert.equal(e.x(1800000), 1000 - MARGEM.dir);
  assert.ok(Math.abs(e.tDoX(e.x(600000)) - 600000) < 1e-6);
  assert.equal(e.passo, 300000); // 30 min em ~950 px: 1 e 2 min ficariam apertados
  assert.equal(escala(1800000, 2400).passo, 60000);
  assert.equal(escala(1800000, 1600).passo, 120000);
  assert.equal(tetoRedondo(172), 200);
});

test('ondas perdidas do jogador, de todas as rotas, em ordem', () => {
  const ondas = {
    rotas: [
      { rota: 'rota_topo', perdidas: [{ participante_id: 1, t_ms: 500 }, { participante_id: 6, t_ms: 100 }] },
      { rota: 'rota_meio', perdidas: [{ participante_id: 1, t_ms: 200 }] },
    ],
  };
  assert.deepEqual(perdidasDoJogador(ondas, 1).map((p) => [p.t_ms, p.rota]), [[200, 'rota_meio'], [500, 'rota_topo']]);
  assert.deepEqual(perdidasDoJogador(null, 1), []);
});
