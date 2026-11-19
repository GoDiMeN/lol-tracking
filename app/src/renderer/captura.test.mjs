// O botão "Pausar captura"/"Retomar captura" da barra de título (captura.js).
import test from 'node:test';
import assert from 'node:assert/strict';
import { botaoDaCaptura } from './captura.js';

const rastreio = { tipo: 'rastreio', id: 9, game_id: 3305486988, esperando: null, pausando: false };

test('nada rodando nem pausado: o botão some', () => {
  assert.equal(botaoDaCaptura({ rodando: null, pausada: null }).visivel, false);
  assert.equal(botaoDaCaptura(null).visivel, false);
});

test('rodando: "Pausar captura", com o atalho na dica', () => {
  const b = botaoDaCaptura({ rodando: rastreio, pausada: null }, 'Ctrl+Alt+P');
  assert.deepEqual([b.visivel, b.texto, b.acao, b.classe], [true, 'Pausar captura', 'pausar', 'rodando']);
  assert.match(b.dica, /rastreio da partida 3305486988/);
  assert.match(b.dica, /Ctrl\+Alt\+P/);
});

test('esperando a pessoa: ainda pausa de vez, e diz que o replay está pausado esperando', () => {
  const b = botaoDaCaptura({ rodando: { ...rastreio, esperando: { motivo: 'x', ocioso_para_retomar_s: 5 } }, pausada: null });
  assert.equal(b.acao, 'pausar');
  assert.equal(b.classe, 'esperando');
  assert.match(b.dica, /parar de usar o computador/);
});

test('pausando: sem ação (o replay está fechando)', () => {
  const b = botaoDaCaptura({ rodando: { ...rastreio, pausando: true }, pausada: null });
  assert.equal(b.acao, null);
  assert.equal(b.texto, 'Pausando…');
});

test('pausada: "Retomar captura", com o motivo quando o motor pausou', () => {
  const b = botaoDaCaptura({ rodando: null, pausada: { tipo: 'coleta', id: 5, game_id: 0, motivo: 'a câmera não prende no jogador' } });
  assert.deepEqual([b.texto, b.acao, b.classe], ['Retomar captura', 'retomar', 'pausada']);
  assert.match(b.dica, /coleta 5 pausada: a câmera não prende/);
});
