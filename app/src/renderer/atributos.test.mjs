// Testes da decomposição dos atributos (atributos.js), com os números da Vi aos 25:34 da partida
// 3304217746 (base do registro do jogo 16.22, itens do Data Dragon 16.22.1, infernal medido).
import test from 'node:test';
import assert from 'node:assert/strict';

import { crescimento, baseNoNivel, decompoe, textoObjetivo, porAcumulo, comTeto, adversarioDeRota, ouroDosItens } from './atributos.js';
import { bonusDoJogador } from './bonus.js';

const vi = { ad: 61, ad_nivel: 3.9, armadura: 30, armadura_nivel: 4.7, rm: 32, rm_nivel: 2.05, vel_ataque: 0.644, vel_ataque_razao: 0.644, vel_ataque_nivel: 2, vel_mov: 340 };
const itens = {
  3047: { nome: 'Botas Galvanizadas de Aço', custo: 1200, atributos: { armadura: 25, vel_mov: 45 } },
  6692: { nome: 'Eclipse', custo: 2900, atributos: { ad: 60 } },
  1036: { nome: 'Espada Longa', custo: 350, atributos: { ad: 10 } },
  6610: { nome: 'Céu Dividido', custo: 3100, atributos: { ad: 40, vida: 400 } },
  3085: { nome: 'Furacão de Runaan', custo: 2650, atributos: { vel_ataque_pct: 0.4, vel_mov_pct: 0.05 } },
};
const resumo = [{ sub: 'FIRE_DRAGON', atributo: 'ad', medido: 0.0313, confianca: 'alta', n: 9 }];

test('crescimento por nível: a fórmula do jogo, não linear', () => {
  assert.equal(crescimento(1), 0);
  assert.ok(Math.abs(crescimento(2) - 0.72) < 1e-9);
  assert.ok(Math.abs(crescimento(18) - 17) < 1e-9, 'no 18 o total é 17 vezes o crescimento');
  // Leona (60 + 3/nível) no 4: 66,8 (a tela mostrou 67).
  assert.equal(Math.round(baseNoNivel({ ...vi, ad: 60, ad_nivel: 3 }, 4).ad), 67);
});

test('decomposição do dano de ataque da Vi aos 25:34: fecha com sobra pequena', () => {
  const d = decompoe({ atr: 'ad', lido: 230, c: vi, nivel: 14, ids: [3047, 6692, 1036, 6610], itens, dragoes: ['FIRE_DRAGON'], resumo });
  assert.equal(Math.round(d.partes[0].valor), 108);
  assert.equal(d.partes[1].valor, 110);
  const inf = d.partes.find((p) => p.objetivo);
  assert.equal(Math.round(inf.valor), 7); // 230 × 0,0313 / 1,0313
  assert.match(inf.detalhe, /medido/);
  assert.ok(d.sobra > 4 && d.sobra < 6);
  assert.equal(d.nao_explicado, 0);
  assert.equal(textoObjetivo('Dano de ataque', d), 'Dano de ataque = 230, dos quais ~7 do dragão infernal');
});

test('decomposição: sobra negativa vira "não explicado"', () => {
  const d = decompoe({ atr: 'armadura', lido: 100, c: vi, nivel: 14, ids: [3047], itens, dragoes: [], resumo });
  assert.equal(d.sobra, 0);
  assert.ok(d.nao_explicado > 11 && d.nao_explicado < 12);
});

test('velocidade de ataque: base + razão × bônus; de movimento: teto suave e nuvens só fora de combate', () => {
  const d = decompoe({ atr: 'vel_ataque', lido: 1.3, c: vi, nivel: 14, ids: [3085], itens, dragoes: [], resumo });
  assert.ok(Math.abs(d.partes[0].valor - 0.8) < 0.01);
  assert.ok(Math.abs(d.partes[1].valor - 0.644 * 0.4) < 1e-9);
  assert.equal(comTeto(433.65).toFixed(1), '429.9');
  const fora = decompoe({ atr: 'vel_mov', lido: 404, c: vi, nivel: 14, ids: [3047], itens, dragoes: ['AIR_DRAGON'], resumo });
  const nuvens = fora.partes.find((p) => p.objetivo);
  assert.equal(nuvens.ativo, true);
  assert.equal(Math.round(nuvens.valor), 19);
  const luta = decompoe({ atr: 'vel_mov', lido: 385, c: vi, nivel: 14, ids: [3047], itens, dragoes: ['AIR_DRAGON'], resumo });
  assert.equal(luta.partes.find((p) => p.objetivo).valor, 0);
  assert.match(textoObjetivo('Vel. de movimento', luta), /0 agora/);
});

test('medido × tabela, adversário de rota e ouro', () => {
  assert.equal(porAcumulo(resumo, 'FIRE_DRAGON', 'ad').origem, 'medido');
  assert.deepEqual(porAcumulo(resumo, 'EARTH_DRAGON', 'rm'), { p: 0.05, origem: 'tabela', confianca: '', medidas: 0 });
  const ps = [
    { participante_id: 2, time: 100, posicao: 'JUNGLE', campeao: 'Shaco' },
    { participante_id: 7, time: 200, posicao: 'JUNGLE', campeao: 'Vi' },
  ];
  assert.equal(adversarioDeRota(ps, 7).campeao, 'Shaco');
  assert.equal(ouroDosItens([3047, 6692, 1036, 6610], itens), 7550);
});

test('bônus do jogador: montanha com a medição ganha número', () => {
  const j = { atributos: { armadura: 131, rm: 62 } };
  const medidos = [
    { sub: 'EARTH_DRAGON', atributo: 'armadura', medido: 0.0507, confianca: 'alta' },
    { sub: 'EARTH_DRAGON', atributo: 'rm', medido: 0.0479, confianca: 'alta' },
  ];
  const [b] = bonusDoJogador({ j, placarTime: { dragoes: ['EARTH_DRAGON'], vastilarvas: 0, arauto: 0 }, medidos });
  assert.equal(b.impacto[0].texto, '≈ 6 da sua armadura (131) vem daí (medido nesta partida)');
  assert.match(b.efeito, /medido nesta partida/);
});
