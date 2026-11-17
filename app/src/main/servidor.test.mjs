import test from 'node:test';
import assert from 'node:assert/strict';
import path from 'node:path';
import { EventEmitter } from 'node:events';

import servidor from './servidor.js';

test('porta: padrão 8770 e LOL_PORTA válida', () => {
  assert.equal(servidor.portaDoAmbiente({}), 8770);
  assert.equal(servidor.portaDoAmbiente({ LOL_PORTA: '9001' }), 9001);
  assert.equal(servidor.portaDoAmbiente({ LOL_PORTA: 'abc' }), 8770);
  assert.equal(servidor.portaDoAmbiente({ LOL_PORTA: '70000' }), 8770);
  assert.equal(servidor.urlBase(8770), 'http://127.0.0.1:8770');
});

const resposta = (corpo, ok = true) => ({ ok, json: async () => corpo });

test('estaDePe só aceita o JSON do nosso /health', async () => {
  assert.equal(await servidor.estaDePe('x', async () => resposta({ ok: true })), true);
  assert.equal(await servidor.estaDePe('x', async () => resposta({ outro: 1 })), false);
  assert.equal(await servidor.estaDePe('x', async () => resposta({ ok: true }, false)), false);
  assert.equal(await servidor.estaDePe('x', async () => { throw new Error('ECONNREFUSED'); }), false);
});

test('espera tenta de novo e desiste quando o processo morreu', async () => {
  let n = 0;
  const fetchFn = async () => { n++; if (n < 3) throw new Error('recusada'); return resposta({ ok: true }); };
  assert.equal(await servidor.espera('x', { tentativas: 5, intervalo: 1, fetchFn }), true);
  assert.equal(n, 3);
  let chamadas = 0;
  const nunca = async () => { chamadas++; throw new Error('x'); };
  assert.equal(await servidor.espera('x', { tentativas: 5, intervalo: 1, fetchFn: nunca, desistir: () => true }), false);
  assert.equal(chamadas, 0);
});

test('plano de desenvolvimento roda go run . em server/ com o renderer absoluto', () => {
  const dirMain = path.join('C:', 'proj', 'app', 'src', 'main');
  const p = servidor.planoDeDesenvolvimento(dirMain, 8770, { PATH: 'x' });
  assert.equal(p.comando, 'go');
  assert.deepEqual(p.args, ['run', '.']);
  assert.equal(p.cwd, path.resolve(dirMain, '..', '..', '..', 'server'));
  assert.equal(p.env.LOL_PORTA, '8770');
  assert.equal(p.env.LOL_RENDERER, path.resolve(dirMain, '..', 'renderer'));
  assert.equal(p.env.PATH, 'x');
});

test('encerrar leva a árvore do go run', () => {
  assert.deepEqual(servidor.comoEncerrar(42, 'win32'), { comando: 'taskkill', args: ['/pid', '42', '/T', '/F'] });
  assert.deepEqual(servidor.comoEncerrar(42, 'linux'), { sinal: 'SIGTERM', alvo: -42 });
});

test('mensagens de falha dizem o que fazer, e a página escapa a saída', () => {
  assert.match(servidor.mensagemDeFalha({ codigoErro: 'ENOENT', porta: 8770 }), /Go não foi encontrado/);
  assert.match(servidor.mensagemDeFalha({ morreu: true, porta: 8770 }), /8770/);
  const html = servidor.paginaDeErro('falhou', ['<script>x</script>']);
  assert.ok(!html.includes('<script>x'));
  assert.ok(html.includes('&lt;script&gt;'));
});

test('inicia guarda as últimas linhas e marca a morte', () => {
  const filho = new EventEmitter();
  filho.stdout = new EventEmitter();
  filho.stderr = new EventEmitter();
  filho.pid = 1;
  const estado = servidor.inicia({ comando: 'go', args: [], cwd: '.', env: {} }, 'win32', () => filho);
  filho.stdout.emit('data', 'linha 1\nlinha 2\n');
  filho.emit('exit', 1);
  assert.deepEqual(estado.linhas, ['linha 1', 'linha 2']);
  assert.equal(estado.morreu, true);
});
