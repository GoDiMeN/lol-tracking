import test from 'node:test';
import assert from 'node:assert/strict';

import { duracao, dataCurta, posicao, kda, titulo, pilulaReplay, contagem, plural } from './formato.js';

test('duracao', () => {
  assert.equal(duracao(2342), '39:02');
  assert.equal(duracao(59), '0:59');
  assert.equal(duracao(3723), '1:02:03');
  assert.equal(duracao(undefined), '0:00');
});

test('dataCurta usa o fuso local e mostra o ano só quando muda', () => {
  const d = new Date(2026, 10, 15, 18, 34);
  assert.equal(dataCurta(d.toISOString(), new Date(2026, 10, 17)), '15 nov 18:34');
  assert.equal(dataCurta(new Date(2025, 0, 2, 3, 4).toISOString(), new Date(2026, 10, 17)), '2 jan 2025 03:04');
  assert.equal(dataCurta('lixo'), '');
});

test('título da partida como no protótipo', () => {
  assert.equal(titulo({ campeao: 'Akshan', posicao: 'MIDDLE', venceu: false }), 'Akshan · meio · derrota');
  assert.equal(titulo({ campeao: 'Lux', posicao: '', venceu: true }), 'Lux · vitória');
  assert.equal(posicao('UTILITY'), 'suporte');
  assert.equal(kda({ abates: 2, mortes: 9, assistencias: 4 }), '2/9/4');
});

test('pílula do replay', () => {
  assert.deepEqual(pilulaReplay({ estado: 'baixado' }), { classe: 'ok', texto: 'baixado' });
  assert.deepEqual(pilulaReplay({ estado: 'baixavel' }), { classe: 'ac', texto: 'dá para baixar' });
  assert.deepEqual(pilulaReplay({ estado: 'baixando', progresso: 39.6 }), { classe: 'ac', texto: 'baixando 40%' });
  assert.deepEqual(pilulaReplay({ estado: 'incompativel' }, '16.21'), { classe: 'n', texto: 'patch 16.21' });
  assert.equal(pilulaReplay({ estado: 'indisponivel' }).classe, 'cr');
  assert.equal(pilulaReplay(null).texto, 'sem estado');
});

test('contagem e plural', () => {
  const c = contagem([
    { replay: { estado: 'baixado' } },
    { replay: { estado: 'baixavel' } },
    { replay: { estado: 'incompativel' } },
    { replay: { estado: 'indisponivel' } },
  ]);
  assert.deepEqual(c, { total: 4, daParaVer: 2, antigas: 1 });
  assert.equal(plural(1, 'partida', 'partidas'), '1 partida');
  assert.equal(plural(19, 'partida', 'partidas'), '19 partidas');
});
