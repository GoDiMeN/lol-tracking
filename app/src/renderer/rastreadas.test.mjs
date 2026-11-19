// Testes das funções puras da lista de partidas rastreadas (rastreadas.js).
import test from 'node:test';
import assert from 'node:assert/strict';
import { consultaRastreadas, participanteEmFoco, passaFiltros, opcoesJogadores, escolhaValida, ehSoDeEstudo, voltaDaPartida } from './rastreadas.js';

const participantes = [
  { participante_id: 1, campeao: 'Ambessa', posicao: 'TOP', venceu: true },
  { participante_id: 7, campeao: 'Vi', posicao: 'JUNGLE', venceu: false },
];

test('consulta da lista', () => {
  assert.equal(consultaRastreadas(), '');
  assert.equal(consultaRastreadas({ origem: 'todas' }), '?origem=todas');
  assert.equal(consultaRastreadas({ puuid: 'a b' }), '?puuid=a%20b');
});

test('o foco é o jogador pesquisado, não o participante 1', () => {
  assert.equal(participanteEmFoco({ foco: { participante_id: 7 }, participantes: [1, 7] }, participantes).campeao, 'Vi');
  // Sem foco: o primeiro rastreado; sem rastreado conhecido: o primeiro participante.
  assert.equal(participanteEmFoco({ foco: null, participantes: [7] }, participantes).campeao, 'Vi');
  assert.equal(participanteEmFoco({ foco: null, participantes: [] }, participantes).campeao, 'Ambessa');
  assert.equal(participanteEmFoco(null, []), null);
});

test('filtros de resultado e rota olham o jogador em foco', () => {
  const vi = participantes[1];
  assert.equal(passaFiltros(vi, {}), true);
  assert.equal(passaFiltros(vi, { resultado: 'derrota' }), true);
  assert.equal(passaFiltros(vi, { resultado: 'vitoria' }), false);
  assert.equal(passaFiltros(vi, { rota: 'JUNGLE' }), true);
  assert.equal(passaFiltros(vi, { rota: 'TOP' }), false);
  assert.equal(passaFiltros(null, { rota: 'TOP' }), false);
});

test('filtro por jogador', () => {
  const jogs = [{ puuid: 'x', riot_id: 'GoDiMeN#BR1', partidas: 1 }];
  assert.deepEqual(opcoesJogadores(jogs, 2), [['', 'todos os jogadores (2)'], ['x', 'GoDiMeN#BR1 (1)']]);
  assert.equal(escolhaValida('x', jogs), 'x');
  assert.equal(escolhaValida('sumiu', jogs), '');
  assert.equal(escolhaValida('', jogs), '');
});

test('partida só de estudo volta para o Estudo', () => {
  assert.equal(ehSoDeEstudo({ origens: ['estudo'] }), true);
  assert.equal(ehSoDeEstudo({ origens: ['pesquisa', 'estudo'] }), false);
  assert.equal(ehSoDeEstudo(undefined), false);
  assert.deepEqual(voltaDaPartida({ origens: ['estudo'] }), { texto: '← Estudo', href: '#estudo' });
  assert.deepEqual(voltaDaPartida({ origens: ['pesquisa'] }), { texto: '← Partidas rastreadas', href: '#partidas' });
});
