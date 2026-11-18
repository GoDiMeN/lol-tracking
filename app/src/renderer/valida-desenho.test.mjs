// Validador do desenho do mapa com play sobre trechos REAIS guardados da partida 3304217746
// (testdata, gravados com `npm run valida-replay -- 3304217746 --de .. --ate .. --grava ..`):
// 12:15–12:40 (Diana e Draven se cruzando), 14:15–14:35 (Yunara na beira de um grupo), 19:10–19:25
// (luta do dragão), 25:40–25:50 (o barão) e 7:20–7:40 (rota de baixo: Senna e Draven um em cima do
// outro, Yunara e Leona ao lado; a captura do dono aos 7:28). Regravados em 2026-11-17 com as
// etiquetas novas (recall, dive, invadindo, split push, agrupado): só as atividades mudaram.
// E as peças puras: arranjo, espalhamento, trechos.

import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

import { validaDesenho } from './valida-desenho.js';
import { criaArranjo, espalha, raioMinimo, escalaDoGrupo } from './chamadas.js';
import { estabilizaTrechos, atividadeEstavel, criaRotulador } from './replay-dados.js';
import { atividadeCurta } from './texto.js';

const carrega = (nome) => {
  const d = JSON.parse(readFileSync(new URL(`./testdata/replay-3304217746-${nome}.json`, import.meta.url), 'utf8'));
  return { ...d, janelas: new Map(d.janelas) };
};

// Teto de glitches por trecho a 1×, 2×, 4× e 8×: o medido hoje (subir é regressão). Na partida
// inteira, com o desenho de direção travada e histerese no tempo (2026-11-17, noite): 35, 53, 55 e
// 66 glitches a 1×/2×/4×/8× contando os três novos (retrato sobre anel, troca de direção, volta e
// sai); o desenho anterior dava 1665, 1488, 1261 e 802 nas mesmas regras.
const TETO = {
  cruzamento: [0, 1, 0, 1],
  'beira-do-grupo': [0, 2, 2, 1],
  'luta-do-dragao': [2, 2, 0, 0],
  barao: [0, 0, 0, 0],
  'rota-de-baixo': [0, 0, 1, 0],
};

for (const [nome, tetos] of Object.entries(TETO)) {
  test(`desenho: trecho real "${nome}" no teto medido de glitches`, () => {
    const d = carrega(nome);
    const r = validaDesenho({ janelas: d.janelas, duracaoMs: d.duracao_ms, times: d.times, nomes: d.nomes, de: d.de, ate: d.ate });
    [1, 2, 4, 8].forEach((v, i) => assert.ok(r[v].glitches.length <= tetos[i], `${nome} a ${v}×: ${JSON.stringify(r[v].glitches)}`));
    assert.ok(r[1].quadros > 300);
  });
}

test('desenho: 7:28 da rota de baixo: Senna e Draven com o anel à vista, sem troca de direção nem pisca', () => {
  const d = carrega('rota-de-baixo');
  const r = validaDesenho({ janelas: d.janelas, duracaoMs: d.duracao_ms, times: d.times, nomes: d.nomes, de: d.de, ate: d.ate });
  for (const v of [1, 2, 4, 8]) {
    const ruins = r[v].glitches.filter((g) => ['troca_de_direcao', 'volta_e_sai'].includes(g.tipo) || (g.tipo === 'retrato_sobre_anel' && g.ids.some((i) => i === 9 || i === 10)));
    assert.deepEqual(ruins, [], `a ${v}×`);
  }
});

test('desenho: o anel fica no lugar verdadeiro e o retrato sozinho em cima dele', () => {
  const a = criaArranjo({ ret: 36 });
  const r = a.passo([{ id: 1, x: 100, y: 100, time: 100 }, { id: 2, x: 400, y: 400, time: 200 }], 16, { lado: 900 });
  assert.deepEqual([r.get(1).rx, r.get(1).ry, r.get(1).traco], [100, 100, 0]);
  assert.equal(r.get(2).n, 1);
});

test('desenho: dois juntos saem para lados opostos, devagar, e voltam sem pulo', () => {
  const a = criaArranjo({ ret: 36 });
  let r;
  let antes = { x: 300, y: 300 };
  for (let i = 0; i < 120; i++) {
    r = a.passo([{ id: 3, x: 300, y: 300, time: 100 }, { id: 9, x: 310, y: 300, time: 200 }], 16, { lado: 900 });
    const p = r.get(3);
    // Nunca mais que a velocidade máxima (5 ret/s → 3 px por quadro de 16 ms).
    assert.ok(Math.hypot(p.rx - antes.x, p.ry - antes.y) <= 36 * 5 * 0.016 + 1e-6);
    antes = { x: p.rx, y: p.ry };
  }
  const azul = r.get(3);
  const verm = r.get(9);
  assert.equal(azul.n, 2);
  assert.ok(azul.rx < azul.ax && verm.rx > verm.ax, 'azul à esquerda, vermelho à direita (para fora do miolo)');
  assert.equal(azul.ax, 300);
  assert.ok(azul.traco > 0.9);
  // Separados de novo: segura o deslocamento por seguraMs de tela (sem voltar antes) e depois
  // volta para cima do anel deslizando.
  for (let i = 0; i < 150; i++) {
    r = a.passo([{ id: 3, x: 300, y: 300, time: 100 }, { id: 9, x: 600, y: 300, time: 200 }], 16, { lado: 900 });
    assert.ok(r.get(3).traco > 0.9, 'continua deslocado logo depois de separar');
  }
  for (let i = 0; i < 300; i++) r = a.passo([{ id: 3, x: 300, y: 300, time: 100 }, { id: 9, x: 600, y: 300, time: 200 }], 16, { lado: 900 });
  assert.ok(Math.abs(r.get(3).rx - 300) < 0.5 && r.get(3).traco < 0.05);
});

test('desenho: deslocado, o retrato anda junto com o anel (translada, não gira)', () => {
  const a = criaArranjo({ ret: 36 });
  let r;
  for (let i = 0; i < 60; i++) r = a.passo([{ id: 3, x: 300, y: 300, time: 100 }, { id: 9, x: 305, y: 300, time: 200 }], 16, { lado: 900 });
  const off0 = [r.get(3).rx - r.get(3).ax, r.get(3).ry - r.get(3).ay];
  // Os dois andam juntos para cima: o deslocamento (vetor) não muda.
  for (let i = 1; i <= 60; i++) r = a.passo([{ id: 3, x: 300, y: 300 - i, time: 100 }, { id: 9, x: 305, y: 300 - i, time: 200 }], 16, { lado: 900 });
  const off1 = [r.get(3).rx - r.get(3).ax, r.get(3).ry - r.get(3).ay];
  assert.ok(Math.hypot(off1[0] - off0[0], off1[1] - off0[1]) < 2, `${off0} → ${off1}`);
});

test('desenho: espalha afasta sem trocar a ordem; raio e escala do grupo', () => {
  const a = espalha([0, 0.1, 0.2, 3], 1.2);
  for (let i = 1; i < 3; i++) assert.ok(((a[i] - a[i - 1] + 2 * Math.PI) % (2 * Math.PI)) > 1.19 || a[i] > a[i - 1]);
  assert.ok(a[0] < a[1] && a[1] < a[2]);
  assert.equal(raioMinimo(1, 40), 0);
  assert.equal(raioMinimo(2, 40), 20);
  assert.ok(raioMinimo(8, 40) > 50);
  assert.deepEqual([escalaDoGrupo(3), escalaDoGrupo(6), escalaDoGrupo(9)], [1, 0.86, 0.74]);
});

test('etiquetas: cadeia de piscadas some, mudança de verdade fica; o detalhe segura 1,5 s', () => {
  // 25:42 da 3304217746: barão · trocando · barão · trocando · barão, 0,25 s cada.
  const tr = (t, fim, atividade, alvo) => ({ participante_id: 5, t_ms: t, fim_ms: fim, atividade, alvo });
  const l = [tr(0, 5000, 'objetivo', 'barão'), tr(5000, 5260, 'trocando'), tr(5260, 5500, 'objetivo', 'barão'), tr(5500, 5760, 'trocando'), tr(5760, 9000, 'objetivo', 'barão'), tr(9000, 12000, 'andando')];
  const e = estabilizaTrechos(l);
  assert.deepEqual(e.map((x) => [x.t_ms, x.fim_ms, x.atividade]), [[0, 9000, 'objetivo'], [9000, 12000, 'andando']]);
  assert.equal(atividadeEstavel(l, 5, 5600).alvo, 'barão');
  assert.equal(atividadeEstavel(l, 5, 9500).atividade, 'andando');
  // Encurtado, o objetivo fica com o nome dele.
  assert.equal(atividadeCurta({ atividade: 'objetivo', alvo: 'barão' }, {}, 5), 'barão');
  const seg = criaRotulador();
  assert.equal(seg(1, 'lutando com Vi', 'lutando', 0), 'lutando com Vi');
  assert.equal(seg(1, 'lutando com Vi e Leona', 'lutando', 300), 'lutando com Vi');
  assert.equal(seg(1, 'lutando com Vi e Leona', 'lutando', 1600), 'lutando com Vi e Leona');
  assert.equal(seg(1, 'farmando', 'farmando', 1700), 'farmando');
});
