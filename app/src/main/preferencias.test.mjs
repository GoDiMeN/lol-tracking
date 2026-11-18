// Testes das preferências da janela (preferencias.js): padrão tela cheia, arquivo quebrado, gravação.
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const prefs = require('./preferencias.js');

const pasta = () => fs.mkdtempSync(path.join(os.tmpdir(), 'lane-prefs-'));

test('sem arquivo: abre em tela cheia (padrão)', () => {
  assert.deepEqual(prefs.le(pasta()), { tela_cheia: true });
});

test('grava e lê de volta; chave desconhecida e tipo errado são ignorados', () => {
  const d = pasta();
  assert.deepEqual(prefs.grava(d, { tela_cheia: false, outra: 1 }), { tela_cheia: false });
  assert.deepEqual(prefs.le(d), { tela_cheia: false });
  prefs.grava(d, { tela_cheia: 'sim' });
  assert.deepEqual(prefs.le(d), { tela_cheia: false });
  assert.ok(!fs.existsSync(path.join(d, `${prefs.ARQUIVO}.tmp`)));
});

test('arquivo quebrado: cai no padrão sem erro', () => {
  const d = pasta();
  fs.writeFileSync(path.join(d, prefs.ARQUIVO), '{tela_cheia: nao');
  assert.deepEqual(prefs.le(d), { tela_cheia: true });
});
