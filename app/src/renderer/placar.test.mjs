// Testes do placar dos times e dos bônus de objetivo (placar.js, bonus.js), com eventos no
// formato de GET /api/partidas/{id}/eventos (os da partida 3304217746, resumidos).
import test from 'node:test';
import assert from 'node:assert/strict';

import {
  preparaPlacar,
  placarNoInstante,
  ouroNoInstante,
  faseNoInstante,
  bonusComPrazo,
  iconeObjetivo,
  serieOuro,
  caminhosDiferenca,
  escalaOuro,
  serieAbates,
  caminhoDegraus,
  marcasDoEixo,
  objetivosEmAndamento,
  textoEmAndamento,
} from './placar.js';
import { bonusDoJogador, baraoNoMinuto, patchDaVersao } from './bonus.js';

const participantes = Array.from({ length: 10 }, (_, i) => ({ participante_id: i + 1, time: i < 5 ? 100 : 200 }));
const monstro = (t, quem, tipo, sub = '', ajuda = []) => ({
  tipo: 'monstro', t_ms: t, quem_id: quem, assistencias: ajuda, dados: { monsterType: tipo, monsterSubType: sub, assistingParticipantIds: ajuda },
});
const estrutura = (t, quem, dono, tipo = 'TOWER_BUILDING') => ({ tipo: 'estrutura', t_ms: t, quem_id: quem, dados: { teamId: dono, buildingType: tipo } });
const eventos = [
  { tipo: 'abate', t_ms: 120206, quem_id: 2, em_quem_id: 9 },
  monstro(511348, 2, 'DRAGON', 'EARTH_DRAGON'),
  monstro(517598, 8, 'HORDE'),
  estrutura(674337, 3, 200),
  // Dragão sem matador (killerId 0): o time é o de quem ajudou.
  monstro(834692, null, 'DRAGON', 'AIR_DRAGON', [2, 3, 5]),
  estrutura(1409814, 3, 200, 'INHIBITOR_BUILDING'),
  { tipo: 'abate', t_ms: 1500000, quem_id: 7, em_quem_id: 4 },
  monstro(1547778, 2, 'BARON_NASHOR'),
  { tipo: 'abate', t_ms: 1600000, quem_id: 8, em_quem_id: 3 },
];
const linhaDoTempo = {
  frames: [
    { timestamp: 0, participantFrames: Object.fromEntries(participantes.map((p) => [p.participante_id, { totalGold: 500 }])) },
    { timestamp: 60000, participantFrames: Object.fromEntries(participantes.map((p) => [p.participante_id, { totalGold: p.time === 100 ? 700 : 600 }])) },
  ],
};

test('placar: estruturas pelo dono, objetivos pelo matador ou por quem ajudou', () => {
  const prep = preparaPlacar({ participantes, eventos, linhaDoTempo });
  const a = placarNoInstante(prep, 900000);
  assert.equal(a[100].torres, 1, 'torre do vermelho (dono 200) é ponto do azul');
  assert.deepEqual(a[100].dragoes, ['EARTH_DRAGON', 'AIR_DRAGON']);
  assert.equal(a[200].vastilarvas, 1);
  assert.equal(a[100].abates, 1, 'sem quadro, os abates saem dos eventos');
  const b = placarNoInstante(prep, 1600000);
  assert.equal(b[100].inibidores, 1);
  assert.equal(b[100].barao, 1);
  assert.equal(b[100].barao_ate_ms, 1547778 + 180000);
  assert.equal(placarNoInstante(prep, 1547778 + 180000)[100].barao_ate_ms, null, 'acabou');
  // Com o quadro, os abates são a soma do K/D/A do quadro.
  const c = placarNoInstante(prep, 10, [{ participante_id: 1, abates: 3 }, { participante_id: 6, abates: 2 }]);
  assert.equal(c[100].abates, 3);
  assert.equal(c[200].abates, 2);
});

test('placar: ouro interpolado entre os minutos', () => {
  const prep = preparaPlacar({ participantes, eventos, linhaDoTempo });
  assert.deepEqual(ouroNoInstante(prep.ouro, 30000), { 100: 3000, 200: 2750 });
  assert.deepEqual(ouroNoInstante(prep.ouro, 999999), { 100: 3500, 200: 3000 }, 'depois do último, o último');
  assert.equal(ouroNoInstante([], 0), null);
});

test('placar: fase no instante', () => {
  const fases = [
    { fase: 'rotas', de_ms: 0, ate_ms: 674337 },
    { fase: 'meio', de_ms: 674337, ate_ms: 1500000 },
    { fase: 'final', de_ms: 1500000, ate_ms: null },
  ];
  assert.equal(faseNoInstante(fases, 674336).fase, 'rotas');
  assert.equal(faseNoInstante(fases, 674337).fase, 'meio');
  assert.equal(faseNoInstante(fases, 1700000).fase, 'final');
});

test('bônus com prazo: perde ao morrer; morto pouco antes = incerto', () => {
  const prep = preparaPlacar({ participantes, eventos, linhaDoTempo });
  assert.equal(bonusComPrazo(prep, 2, 1547778, 180000, 1550000), 'ativo');
  assert.equal(bonusComPrazo(prep, 3, 1547778, 180000, 1610000), 'perdeu', 'o 3 morreu aos 1600000');
  assert.equal(bonusComPrazo(prep, 4, 1547778, 180000, 1550000), 'incerto', 'o 4 morreu 48 s antes');
  assert.equal(bonusComPrazo(prep, 2, 1547778, 180000, 1547778 + 180000), 'acabou');
});

test('bônus: tabela do patch e impacto no jogador', () => {
  assert.equal(patchDaVersao('16.22'), '26.22');
  assert.equal(patchDaVersao('14.3'), '14.3');
  assert.deepEqual(baraoNoMinuto(25.8 * 60000), { ad: [16, 19], ap: [27, 32] });
  assert.deepEqual(baraoNoMinuto(45 * 60000), { ad: [48, 48], ap: [80, 80] });
  const j = { vida: 600, vida_max: 1000, atributos: { ad: 206, ap: 0 } };
  const placarTime = { dragoes: ['FIRE_DRAGON', 'FIRE_DRAGON', 'EARTH_DRAGON'], alma: null, vastilarvas: 0, arauto: 0 };
  const bl = bonusDoJogador({ j, placarTime });
  const inf = bl.find((b) => b.titulo.startsWith('Dragão infernal'));
  assert.equal(inf.efeito, '+6% de dano de ataque e de poder de habilidade');
  // 206 × 0,06 / 1,06 = 11,66
  assert.match(inf.impacto[0].texto, /^≈ 12 do seu dano de ataque/);
  assert.equal(inf.impacto.length, 1, 'PH 0: sem linha');
  const mon = bl.find((b) => b.titulo.startsWith('Dragão da montanha'));
  assert.equal(mon.impacto.length, 0);
  assert.ok(mon.aviso, 'sem número, com o motivo');
  const oce = bonusDoJogador({ j, placarTime: { dragoes: ['WATER_DRAGON'], vastilarvas: 0, arauto: 0 } })[0];
  assert.equal(oce.impacto[0].texto, '8 de vida a cada 5 s agora (perdeu 400)');
});

test('placar: ícones dos objetivos, um por tipo de dragão', () => {
  assert.equal(iconeObjetivo('dragao', 'FIRE_DRAGON'), 'assets/objetivos/dragao-infernal.svg');
  assert.equal(iconeObjetivo('dragao', 'CHEMTECH_DRAGON'), 'assets/objetivos/dragao-quimtec.svg');
  assert.equal(iconeObjetivo('dragao', 'X'), 'assets/objetivos/dragao.svg');
  assert.equal(iconeObjetivo('anciao'), 'assets/objetivos/dragao-anciao.svg');
  assert.equal(iconeObjetivo('barao'), 'assets/objetivos/barao.svg');
  assert.equal(iconeObjetivo('torre'), 'assets/objetivos/torre.svg');
});

test('placar: gráfico da diferença de ouro com o cruzamento do zero', () => {
  const serie = [
    { t_ms: 0, dif: 0 },
    { t_ms: 60000, dif: 1000 },
    { t_ms: 120000, dif: -1000 },
  ];
  assert.equal(escalaOuro(serie), 2000);
  assert.equal(escalaOuro([{ t_ms: 0, dif: -4300 }]), 5000);
  const c = caminhosDiferenca(serie, 120000, 120, 40, 2000);
  // Cruza o zero no meio entre 1 e 2 min (x = 90): o ponto exato entra nas duas áreas.
  assert.ok(c.acima.includes('L90,20') && c.abaixo.includes('L90,20'));
  assert.ok(c.acima.includes('L60,10'), c.acima); // +1000 de 2000 = um quarto da altura acima do zero
  assert.ok(c.abaixo.includes('L120,30'), c.abaixo);
  assert.equal(c.zero, 20);
  assert.equal(caminhosDiferenca([], 1, 10, 10, 1).acima, '');
  const prep = preparaPlacar({ participantes, eventos, linhaDoTempo });
  assert.deepEqual(serieOuro(prep), [
    { t_ms: 0, dif: 0 },
    { t_ms: 60000, dif: 500 },
  ]);
});

test('placar: abates por time em degraus e marcas do eixo', () => {
  const prep = preparaPlacar({ participantes, eventos, linhaDoTempo });
  const s = serieAbates(prep);
  assert.deepEqual(s[s.length - 1], { t_ms: 1600000, 100: 1, 200: 2 });
  const d = caminhoDegraus(s, 200, 1600000, 160, 20, 2);
  assert.ok(d.startsWith('M0,20') && d.endsWith('L160,0'), d);
  const m = marcasDoEixo(prep);
  assert.ok(m.some((x) => x.tipo === 'barao' && x.time === 100) && m.some((x) => x.tipo === 'inibidor'));
  assert.ok(m.every((x, i) => i === 0 || m[i - 1].t_ms <= x.t_ms));
});

test('placar: time fazendo objetivo junto (só leitura das atividades)', () => {
  const timeDe = Object.fromEntries(participantes.map((p) => [p.participante_id, p.time]));
  const tr = (pid, atividade, alvo) => ({ participante_id: pid, atividade, alvo });
  const g = objetivosEmAndamento(
    [tr(1, 'objetivo', 'barão'), tr(2, 'objetivo', 'barão'), tr(3, 'objetivo', 'barão'), tr(5, 'objetivo', 'barão'), tr(4, 'farmando'), tr(6, 'torre', 'torre do meio'), tr(7, 'torre', 'torre do meio'), tr(8, 'torre', 'torre do meio'), tr(9, 'objetivo', 'dragão infernal'), null],
    timeDe,
  );
  assert.deepEqual(g.map(textoEmAndamento), ['azul fazendo o barão · 4 jogadores', 'vermelho derrubando a torre do meio · 3 jogadores']);
  assert.equal(textoEmAndamento({ time: 100, atividade: 'objetivo', alvo: 'vastilarvas', n: 2 }), 'azul fazendo as vastilarvas · 2 jogadores');
});
