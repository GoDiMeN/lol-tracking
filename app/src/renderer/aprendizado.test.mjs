// Contas e textos do aprendizado por elo (sem DOM).

import test from 'node:test';
import assert from 'node:assert/strict';

import { ELOS, nomeElo, estimativaColeta, textoDescartes, porcento, destaques, filtraFuncao, numerosLiga, estimativaEstudo } from './aprendizado.js';

test('aprendizado: os 8 elos de Prata a Desafiante, mono de Mestre para cima', () => {
  assert.equal(ELOS.length, 8);
  assert.equal(ELOS[0].elo, 'SILVER');
  assert.deepEqual(
    ELOS.filter((e) => e.mono).map((e) => e.elo),
    ['MASTER', 'GRANDMASTER', 'CHALLENGER'],
  );
  assert.equal(nomeElo('GRANDMASTER'), 'Grão-Mestre');
  assert.equal(nomeElo('X'), 'X');
});

test('aprendizado: estimativa de 7,5 min por passe a 4×', () => {
  // 8 elos × 10 jogadores × 1 partida × só o jogador = 80 passes × 7,5 min = 10 h.
  assert.deepEqual(estimativaColeta({ elos: ELOS.map((e) => e.elo) }), { passes: 80, segundos: 80 * 450 });
  // Os 10 de cada partida, 2 partidas, a 8×: 2 elos × 10 × 2 × 10 = 400 passes, metade do tempo cada.
  assert.deepEqual(estimativaColeta({ elos: 2, partidasPorJogador: 2, quem: 'todos', velocidade: 8 }), { passes: 400, segundos: 400 * 225 });
  assert.equal(estimativaColeta({ elos: [] }).passes, 0);
});

test('aprendizado: destaques por elo, filtro por função e números da liga', () => {
  const js = [
    { id: 1, elo: 'PLATINUM', posicao: 1, funcao: 'TOP', fresh_blood: false },
    { id: 2, elo: 'PLATINUM', posicao: 2, funcao: 'JUNGLE', fresh_blood: true },
    { id: 3, elo: 'PLATINUM', posicao: 3, funcao: 'JUNGLE', fresh_blood: true },
    { id: 4, elo: 'EMERALD', posicao: 4, funcao: 'JUNGLE', fresh_blood: false },
  ];
  const d = destaques(js);
  assert.deepEqual(d.get(1), { maisSobe: true, recemChegado: false });
  assert.deepEqual(d.get(2), { maisSobe: false, recemChegado: true });
  assert.deepEqual(d.get(3), { maisSobe: false, recemChegado: false });
  assert.equal(d.get(4).maisSobe, true);
  // Filtrado pela selva: o primeiro caçador vira "o que mais está subindo" entre os caçadores.
  const selva = filtraFuncao(js, 'JUNGLE');
  assert.deepEqual(selva.map((j) => j.id), [2, 3, 4]);
  assert.deepEqual(destaques(selva).get(2), { maisSobe: true, recemChegado: true });
  assert.equal(filtraFuncao(js, '').length, 4);
  assert.equal(numerosLiga({ vitorias: 53, derrotas: 31, taxa_vitoria: 0.631 }), '53 V · 31 D · 63%');
  // 6 partidas de estudo a 4×: 45 min.
  assert.equal(estimativaEstudo(6), 6 * 450);
  assert.equal(estimativaEstudo(6, 8), 6 * 225);
});

test('aprendizado: textos dos descartes e da fração', () => {
  assert.equal(textoDescartes({ sem_partida: 2 }), '2 sem partida rastreável');
  assert.equal(textoDescartes({ nao_mono: 3, inativo: 1, veterano: 0 }), '1 parado · 3 não jogam um campeão só');
  assert.equal(textoDescartes({}), '');
  assert.equal(porcento(0.754), '75%');
});
