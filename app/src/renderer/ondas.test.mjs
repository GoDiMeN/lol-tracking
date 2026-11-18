// Testes das ondas no mapa com play (ondas.js), no formato de GET /api/partidas/{id}/ondas
// (números da partida 3304217746, topo, resumidos).
import test from 'node:test';
import assert from 'node:assert/strict';

import { pontoNoInstante, trechoNoInstante, ondaNoInstante, textoEstado, textoOnda, ondasNoInstante, textoCurto } from './ondas.js';

const dados = {
  ate_ms: 840000,
  ondas: [
    { n: 1, canhao: false, chegada_ms: { rota_topo: 62800, rota_meio: 52800, rota_baixo: 62800 } },
    { n: 2, canhao: false, chegada_ms: { rota_topo: 86300, rota_meio: 82800, rota_baixo: 86300 } },
    { n: 3, canhao: true, chegada_ms: { rota_topo: 116600, rota_meio: 112800, rota_baixo: 116600 } },
  ],
  rotas: [
    {
      rota: 'rota_topo',
      rastreados: [1, 6],
      pontos: [
        { t_ms: 100000, x: 1000, y: 12000, confianca: 'alta' },
        { t_ms: 101000, x: 1100, y: 12100, confianca: 'alta' },
        { t_ms: 110000, x: 1500, y: 12500, confianca: 'media' },
      ],
      estados: [
        { estado: 'freeze', de_ms: 268293, ate_ms: 419901, time: 100, confianca: 'alta' },
        { estado: 'empurrando', de_ms: 86365, ate_ms: 147008, time: 100, velocidade: 'lenta' },
      ],
    },
  ],
};

test('pontoNoInstante interpola entre vizinhos e não inventa no buraco', () => {
  const ps = dados.rotas[0].pontos;
  assert.deepEqual(pontoNoInstante(ps, 100500), { x: 1050, y: 12050, confianca: 'alta' });
  assert.equal(pontoNoInstante(ps, 105500), null); // 9 s sem ponto: ninguém da rota
  assert.deepEqual(pontoNoInstante(ps, 111500), { x: 1500, y: 12500, confianca: 'media' });
  assert.equal(pontoNoInstante(ps, 90000), null);
  assert.equal(pontoNoInstante([], 1), null);
});

test('trecho e onda do instante', () => {
  assert.equal(trechoNoInstante(dados.rotas[0].estados, 300000).estado, 'freeze');
  assert.equal(trechoNoInstante(dados.rotas[0].estados, 200000), null);
  assert.equal(ondaNoInstante(dados.ondas, 'rota_topo', 60000), null);
  assert.equal(ondaNoInstante(dados.ondas, 'rota_topo', 120000).n, 3);
  assert.equal(textoOnda(ondaNoInstante(dados.ondas, 'rota_topo', 120000)), 'onda 3 · canhão');
  assert.equal(textoOnda(null), 'antes da 1ª onda');
});

test('textos dos estados', () => {
  assert.equal(textoEstado({ estado: 'freeze', time: 100 }), 'freeze do azul');
  assert.equal(textoEstado({ estado: 'onda_na_torre', time: 200 }), 'na torre vermelha');
  assert.equal(textoEstado({ estado: 'onda_voltando', time: 100 }), 'voltando da torre azul');
  assert.equal(textoEstado({ estado: 'empurrando', time: 200, velocidade: 'rapida' }), 'vermelho empurrando rápido');
  assert.equal(textoEstado({ estado: 'onda_parada', time: 0 }), 'parada no meio');
  assert.equal(textoEstado(null), 'sem estimativa');
  assert.equal(textoCurto({ estado: 'onda_na_torre' }), 'na torre');
});

test('ondasNoInstante dá uma linha por rota, mesmo sem dados da rota', () => {
  const l = ondasNoInstante(dados, 100500);
  assert.deepEqual(l.map((x) => x.nome), ['topo', 'meio', 'baixo']);
  assert.equal(l[0].trecho.estado, 'empurrando');
  assert.equal(l[0].ponto.x, 1050);
  assert.equal(l[1].ponto, null);
  assert.equal(l[1].onda.n, 2);
  // Depois de até onde se estima: nada.
  assert.equal(ondasNoInstante(dados, 900000)[0].trecho, null);
});

test('itens de onda da cadeia em frase', async () => {
  const { itemCadeia } = await import('./texto.js');
  assert.equal(
    itemCadeia({ tipo: 'freeze', dados: { time: 100, a_favor: true, duracao_ms: 151608 } }),
    'onda freeze do azul por 2:32 · a favor (estimada)',
  );
  assert.equal(itemCadeia({ tipo: 'onda_perdida', dados: { onda: 10, canhao: false, motivo: 'na_base' } }), 'perdeu a onda 10: na base');
});
