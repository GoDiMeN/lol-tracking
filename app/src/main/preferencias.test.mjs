// Testes das preferências da janela (preferencias.js): padrão maximizado (sem tela cheia), arquivo quebrado, gravação,
// e o atalho global de pausar a captura.
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const prefs = require('./preferencias.js');

const pasta = () => fs.mkdtempSync(path.join(os.tmpdir(), 'lane-prefs-'));

test('sem arquivo: abre maximizado, sem tela cheia, atalho Ctrl+Alt+P (padrão)', () => {
  assert.deepEqual(prefs.le(pasta()), { tela_cheia: false, atalho_pausa: 'Control+Alt+P' });
});

test('grava e lê de volta; chave desconhecida e tipo errado são ignorados', () => {
  const d = pasta();
  assert.deepEqual(prefs.grava(d, { tela_cheia: true, outra: 1 }), { tela_cheia: true, atalho_pausa: 'Control+Alt+P' });
  assert.deepEqual(prefs.le(d), { tela_cheia: true, atalho_pausa: 'Control+Alt+P' });
  prefs.grava(d, { tela_cheia: 'sim' });
  assert.deepEqual(prefs.le(d), { tela_cheia: true, atalho_pausa: 'Control+Alt+P' });
  assert.ok(!fs.existsSync(path.join(d, `${prefs.ARQUIVO}.tmp`)));
});

test('atalho de pausa: grava o válido, ignora o inválido (sem Ctrl/Alt, tecla estranha)', () => {
  const d = pasta();
  assert.equal(prefs.grava(d, { atalho_pausa: 'Control+Shift+F9' }).atalho_pausa, 'Control+Shift+F9');
  assert.equal(prefs.grava(d, { atalho_pausa: 'P' }).atalho_pausa, 'Control+Shift+F9');
  assert.equal(prefs.grava(d, { atalho_pausa: 'Shift+P' }).atalho_pausa, 'Control+Shift+F9');
  assert.equal(prefs.grava(d, { atalho_pausa: 'Control+Alt+Ç' }).atalho_pausa, 'Control+Shift+F9');
  assert.equal(prefs.le(d).atalho_pausa, 'Control+Shift+F9');
});

test('arquivo quebrado: cai no padrão sem erro', () => {
  const d = pasta();
  fs.writeFileSync(path.join(d, prefs.ARQUIVO), '{tela_cheia: sim');
  assert.deepEqual(prefs.le(d), { tela_cheia: false, atalho_pausa: 'Control+Alt+P' });
});
