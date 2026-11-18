// Validador do desenho do mapa com play sobre trechos REAIS guardados da partida 3304217746
// (testdata, gravados com `npm run valida-replay -- 3304217746 --de .. --ate .. --grava ..`):
// 12:15–12:40 (Diana e Draven se cruzando), 14:15–14:35 (Yunara na beira de um grupo), 19:10–19:25
// (luta do dragão) e 25:40–25:50 (o barão). E as peças puras: arranjo, espalhamento, trechos.

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

// A 4× e 8× o dado anda 2 a 4 quadros de tela entre dois quadros do servidor; numa luta de 8 ainda
// sobram poucos tremores (medidos na partida inteira: 1 a 1×, 4 a 2×, 7 a 4×, 17 a 8×; antes
// do anel e traço eram 1399, 1314, 1238 e 1128). O teto aqui é o medido hoje em cada trecho:
// subir é regressão. A 1× e 2×, zero.
const TETO = { cruzamento: [0, 0], 'beira-do-grupo': [0, 0], 'luta-do-dragao': [2, 0], barao: [1, 0] };

for (const [nome, [teto4, teto8]] of Object.entries(TETO)) {
  test(`desenho: trecho real "${nome}" sem glitches a 1× e 2× (4× e 8× no teto medido)`, () => {
    const d = carrega(nome);
    const r = validaDesenho({ janelas: d.janelas, duracaoMs: d.duracao_ms, times: d.times, nomes: d.nomes, de: d.de, ate: d.ate });
    for (const v of [1, 2]) assert.deepEqual(r[v].glitches, [], `${nome} a ${v}×`);
    assert.ok(r[4].glitches.length <= teto4, `${nome} a 4×: ${JSON.stringify(r[4].glitches)}`);
    assert.ok(r[8].glitches.length <= teto8, `${nome} a 8×: ${JSON.stringify(r[8].glitches)}`);
    assert.ok(r[1].quadros > 300);
  });
}

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
  // Separados de novo: o retrato volta para cima do anel deslizando.
  for (let i = 0; i < 200; i++) r = a.passo([{ id: 3, x: 300, y: 300, time: 100 }, { id: 9, x: 600, y: 300, time: 200 }], 16, { lado: 900 });
  assert.ok(Math.abs(r.get(3).rx - 300) < 0.5 && r.get(3).traco < 0.05);
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
