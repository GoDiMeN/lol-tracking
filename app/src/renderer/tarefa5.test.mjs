// Testes da lógica pura da tarefa 5: rota, mapa (coordenada → pixel, zona pela grade), dados do
// mapa com play (interpolação, janelas com pré-busca) e textos.
import test from 'node:test';
import assert from 'node:assert/strict';

import { lerRota, montaRota, itemDoMenu } from './rota.js';
import { paraTela, paraJogo, emPorcento, zonaNaGrade, LIMITES } from './mapa.js';
import {
  achaIndice,
  interpolaJogador,
  eSalto,
  quadroNoInstante,
  atividadeNoInstante,
  eventosRecentes,
  janelasNecessarias,
  inicioDaJanela,
  Janelas,
} from './replay-dados.js';
import {
  relogio,
  lerRelogio,
  numero,
  tempoHumano,
  estimativaRastreio,
  nomeZona,
  recarga,
  atividadeTexto,
  itemCadeia,
  valorIndicador,
  diferenca,
  bytes,
  pilulaPasse,
  progressoPasse,
  podeRastrear,
  resumoDano,
  superioridade,
  grupoDoTipo,
} from './texto.js';

// --- rota ---------------------------------------------------------------------------------------

test('rota: ida e volta de cada vista', () => {
  const casos = [
    { vista: 'jogadores', puuid: null },
    { vista: 'jogadores', puuid: 'abc-1' },
    { vista: 'rastreio', id: 7 },
    { vista: 'partida', gameId: 3304594274, participante: 3, abate: 12 },
    { vista: 'estudo', id: 2, indicador: 'mortes_fora_da_rota' },
    { vista: 'replay', gameId: 3304594274, t: 528 },
    { vista: 'configuracoes' },
    { vista: 'partidas' },
  ];
  for (const c of casos) {
    const volta = lerRota(montaRota(c));
    for (const [k, v] of Object.entries(c)) assert.equal(volta[k], v, `${montaRota(c)} → ${k}`);
  }
});

test('rota: endereços tortos caem em lugar seguro', () => {
  assert.equal(lerRota('').vista, 'jogadores');
  assert.equal(lerRota('#nada').vista, 'jogadores');
  assert.equal(lerRota('#partida/xyz').vista, 'partidas');
  assert.equal(lerRota('#replay').vista, 'partidas');
  assert.deepEqual(lerRota('#partida/10'), { vista: 'partida', gameId: 10, participante: null, abate: null });
  assert.equal(lerRota('#replay/10/88.5').t, 88.5);
  assert.equal(itemDoMenu('replay'), 'partidas');
  assert.equal(itemDoMenu('estudo'), 'estudo');
});

// --- mapa ---------------------------------------------------------------------------------------

test('mapa: cantos e y invertido', () => {
  const a = paraTela(LIMITES.min.x, LIMITES.min.y, 512);
  assert.deepEqual([a.px, a.py], [0, 512]); // canto de baixo à esquerda (base azul)
  const b = paraTela(LIMITES.max.x, LIMITES.max.y, 512);
  assert.deepEqual([b.px, b.py], [512, 0]);
  const c = paraTela(7375, 7430, 1000); // centro do mapa
  assert.ok(Math.abs(c.px - 500) < 1 && Math.abs(c.py - 500) < 1, JSON.stringify(c));
  const ida = paraJogo(...Object.values(paraTela(5000, 11000, 800)), 800);
  assert.ok(Math.abs(ida.x - 5000) < 1e-6 && Math.abs(ida.y - 11000) < 1e-6);
  const p = emPorcento(14870, -120);
  assert.deepEqual([p.left, p.top], [100, 100]);
});

test('mapa: zona pela grade do servidor', () => {
  const grade = { origem: { x: 0, y: 0 }, celula: 100, colunas: 3, codigos: ['rota_meio', 'rio_cima'], linhas: ['AAB', 'BBA'] };
  assert.equal(zonaNaGrade(grade, 50, 50), 'rota_meio');
  assert.equal(zonaNaGrade(grade, 250, 50), 'rio_cima');
  assert.equal(zonaNaGrade(grade, 50, 150), 'rio_cima'); // linha 1 = y de 100 a 200
  assert.equal(zonaNaGrade(grade, 350, 50), '');
  assert.equal(zonaNaGrade(grade, null, 50), '');
  assert.equal(zonaNaGrade(null, 1, 1), '');
});

// --- mapa com play ------------------------------------------------------------------------------

const q = (t, x, extra = {}) => ({ t_ms: t, jogadores: [{ participante_id: 3, x, y: 1000, morto: false, vida_pct: 100, ...extra }] });

test('replay: busca binária do quadro', () => {
  const qs = [q(0, 0), q(500, 0), q(1000, 0)];
  assert.equal(achaIndice(qs, -1), -1);
  assert.equal(achaIndice(qs, 0), 0);
  assert.equal(achaIndice(qs, 499), 0);
  assert.equal(achaIndice(qs, 500), 1);
  assert.equal(achaIndice(qs, 9999), 2);
});

test('replay: interpola andando e pula no salto', () => {
  const a = { participante_id: 3, x: 1000, y: 1000, morto: false, vida_pct: 80, nivel: 3 };
  const meio = interpolaJogador(a, { ...a, x: 1200, vida_pct: 60, nivel: 4 }, 0.5, 500);
  assert.equal(meio.x, 1100);
  assert.equal(meio.vida_pct, 70);
  assert.equal(meio.nivel, 3, 'o resto vem do quadro de trás');
  // Recall: 10 000 unidades em meio segundo não desliza; fica até o quadro novo e troca.
  const longe = { ...a, x: 11000 };
  assert.equal(interpolaJogador(a, longe, 0.3, 500).x, 1000);
  assert.equal(interpolaJogador(a, longe, 0.99, 500).x, 1000);
  assert.equal(interpolaJogador(a, longe, 1, 500).x, 11000);
  // W da LeBlanc no dado real (descobertas, seção 6): ~435 de volta num passo de 250 ms = teleporte.
  assert.equal(eSalto(a, { ...a, x: 1435 }, 250), true);
  assert.equal(eSalto(a, { ...a, x: 1180 }, 500), false, 'andar a 360/s desliza');
  assert.equal(eSalto(a, { ...a, x: 1250 }, 100), false, 'abaixo de 300 unidades é ruído');
  // Troca de origem da posição (câmera → morte) também é salto.
  assert.equal(eSalto({ ...a, origem: 'camera' }, { ...a, x: 1100, origem: 'morte' }, 500), true);
  assert.equal(interpolaJogador({ ...a, origem: 'camera' }, { ...a, x: 1100, origem: 'base' }, 0.5, 500).x, 1000);
  // Sem posição de um lado: fica no que se sabe.
  assert.equal(interpolaJogador(a, { ...a, x: null }, 0.5, 500).x, 1000);
  const qd = quadroNoInstante([q(0, 0), q(1000, 400)], 250);
  assert.equal(qd.jogadores[0].x, 100);
  assert.equal(quadroNoInstante([q(0, 0), q(1000, 400)], 5000).jogadores[0].x, 400);
  assert.equal(quadroNoInstante([], 10), null);
});

test('replay: atividade e eventos no instante', () => {
  const trechos = [
    { participante_id: 3, t_ms: 0, fim_ms: 100, atividade: 'farmando' },
    { participante_id: 3, t_ms: 100, fim_ms: 200, atividade: 'trocando', com: [8] },
    { participante_id: 8, t_ms: 0, fim_ms: 200, atividade: 'andando' },
  ];
  assert.equal(atividadeNoInstante(trechos, 3, 150).atividade, 'trocando');
  assert.equal(atividadeNoInstante(trechos, 3, 250), null);
  const evs = [{ t_ms: 10 }, { t_ms: 50 }, { t_ms: 90 }, { t_ms: 200 }];
  assert.deepEqual(eventosRecentes(evs, 95, 50).map((e) => e.t_ms), [50, 90]);
});

test('replay: janelas e pré-busca', () => {
  assert.equal(inicioDaJanela(125000), 120000);
  assert.deepEqual(janelasNecessarias(125000, 2342000), [120000]);
  assert.deepEqual(janelasNecessarias(165000, 2342000), [120000, 180000], 'a 15 s do fim pede a próxima');
  assert.deepEqual(janelasNecessarias(2335000, 2342000), [2280000, 2340000], 'a última janela é curta');
  assert.deepEqual(janelasNecessarias(9e9, 2342000), [2340000], 'não passa do fim da partida');
});

test('replay: cache de janelas carrega uma vez, pré-busca e descarta as longe', async () => {
  const pedidos = [];
  const j = new Janelas({
    duracaoMs: 600000,
    maximo: 3,
    carrega: async (de, ate) => {
      pedidos.push([de, ate]);
      return { quadros: [q(de, 0), q(ate, 0)], atividades: [] };
    },
  });
  assert.equal(j.dadosEm(1000), null);
  await j.garante(1000);
  await j.garante(2000);
  assert.deepEqual(pedidos, [[0, 60000]]);
  assert.ok(j.dadosEm(30000));
  await j.garante(50000); // pré-busca da próxima
  assert.deepEqual(pedidos.at(-1), [60000, 120000]);
  for (const t of [130000, 190000, 250000, 310000]) await j.garante(t);
  assert.ok(j.mapa.size <= 3, `guardou ${j.mapa.size}`);
  assert.equal(j.dadosEm(1000), null, 'a primeira janela foi descartada');
  // A última janela é cortada no fim da partida.
  await j.garante(599000);
  assert.deepEqual(pedidos.at(-1), [540000, 600000]);
});

test('replay: erro de carga fica marcado e é tentado de novo', async () => {
  let falha = true;
  const j = new Janelas({
    duracaoMs: 120000,
    carrega: async () => {
      if (falha) throw new Error('caiu');
      return { quadros: [], atividades: [] };
    },
  });
  await assert.rejects(j.garante(0));
  assert.equal(j.estadoEm(0), 'erro');
  falha = false;
  await j.garante(0);
  assert.equal(j.estadoEm(0), 'pronto');
});

// --- textos -------------------------------------------------------------------------------------

test('texto: relógio, números e tempo', () => {
  assert.equal(relogio(528054), '8:48');
  assert.equal(lerRelogio('8:48'), 528000);
  assert.equal(lerRelogio('15'), 900000);
  assert.equal(lerRelogio('x'), null);
  assert.equal(lerRelogio(''), null);
  assert.equal(numero(1173), '1.173');
  assert.equal(numero(3.456), '3,5');
  assert.equal(numero(null), '—');
  assert.equal(tempoHumano(30), '~30 s');
  assert.equal(tempoHumano(2700), '~45 min');
  assert.equal(tempoHumano(8460), '~2 h 21 min');
  assert.equal(bytes(3571712), '3,4 MB');
  assert.equal(bytes(512), '512 B');
});

test('texto: estimativa do rastreio', () => {
  // 39 min, 10 jogadores a 2×: ~3 h 15 min de jogo + a sobra por passe.
  const s = estimativaRastreio({ duracaoS: 2342, jogadores: 10, velocidade: 2 });
  assert.equal(s, 10 * (1171 + 15));
  // Trecho de 0 a 15 min, 2 jogadores a 4×.
  assert.equal(estimativaRastreio({ duracaoS: 2342, jogadores: 2, velocidade: 4, ateS: 900 }), 2 * (225 + 15));
});

test('texto: recargas, zonas e atividade', () => {
  assert.equal(recarga(0).texto, 'pronta');
  assert.equal(recarga(-1).classe, 'na');
  assert.equal(recarga(null).texto, '?');
  assert.equal(recarga(62).texto, '62 s');
  assert.equal(recarga(4.5).texto, '4,5 s');
  assert.equal(nomeZona('cova_barao'), 'cova do Barão');
  assert.equal(nomeZona(''), '');
  const nomes = { 8: 'LeBlanc', 7: 'Warwick', 6: 'Teemo' };
  assert.equal(atividadeTexto({ atividade: 'trocando', com: [8] }, nomes), 'trocando com LeBlanc');
  assert.equal(atividadeTexto({ atividade: 'lutando', com: [7, 6, 8] }, nomes), 'lutando com Warwick, Teemo e LeBlanc');
  assert.equal(atividadeTexto({ atividade: 'na_base', com: [] }), 'na base');
  assert.equal(atividadeTexto({ atividade: 'counter_gank', com: [7] }, nomes), 'counter gank em Warwick');
  assert.equal(atividadeTexto({ atividade: 'coletando_ouro', com: [7], farmando: true }, nomes), 'coletando ouro e farmando');
  assert.equal(atividadeTexto({ atividade: 'coletando_ouro', com: [7], farmando: false }, nomes), 'coletando ouro');
});

test('texto: itens da cadeia em frase de jogador', () => {
  const nomes = { 8: 'LeBlanc', 7: 'Warwick' };
  assert.equal(itemCadeia({ tipo: 'abate', dados: { vitima: 8 } }, nomes), 'abateu LeBlanc');
  assert.equal(itemCadeia({ tipo: 'morte', dados: { matador: 7, respawn_s: 12.4 } }, nomes), 'morreu para Warwick (volta em 12 s)');
  assert.equal(
    itemCadeia({ tipo: 'saiu_da_rota', dados: { rota: 'rota_meio', para: 'rio_baixo', vida_pct: 84.1, duracao_ms: 19915, desfecho: 'morte' } }),
    'saiu da rota do meio para rio de baixo com 84% de vida, por 20 s · morreu',
  );
  assert.equal(itemCadeia({ tipo: 'habilidade_usada', dados: { habilidade: 'r', recarga_s: 99.6 } }), 'usou R (recarga de 100 s)');
  assert.equal(itemCadeia({ tipo: 'tipo_novo', dados: {} }), 'tipo novo');
  assert.equal(grupoDoTipo('voltou_para_rota'), 'rota');
});

test('texto: indicadores e diferenças', () => {
  assert.equal(valorIndicador(50, '%'), '50%');
  assert.equal(valorIndicador(112.63, 's'), '1:53');
  assert.equal(valorIndicador(30.9, 's'), '30,9 s');
  assert.equal(valorIndicador(7.714, 'por_min'), '7,71/min');
  assert.equal(valorIndicador(3057.88, 'unidades'), '3.058');
  assert.equal(valorIndicador(null, 'n'), '—');
  assert.equal(diferenca(-1150, 'unidades'), '−1.150');
  assert.equal(diferenca(3.2, 'n'), '+3,2');
  assert.equal(diferenca(0, 'n'), '0');
});

test('texto: fila de rastreio', () => {
  assert.deepEqual(pilulaPasse({ estado: 'concluido' }), { classe: 'ok', texto: 'pronto' });
  assert.equal(pilulaPasse({ estado: 'pendente' }).texto, 'na fila');
  assert.equal(pilulaPasse({ estado: 'falhou' }).classe, 'cr');
  const r = { t_inicio_ms: 0, t_fim_ms: 900000 };
  assert.equal(progressoPasse({ estado: 'rodando', t_atual_ms: 450000 }, r), 0.5);
  assert.equal(progressoPasse({ estado: 'rodando', t_atual_ms: 450000, duracao_ms: 1800000 }, { t_inicio_ms: 0, t_fim_ms: 0 }), 0.25);
  assert.equal(progressoPasse({ estado: 'concluido' }, r), 1);
});

test('texto: quando dá para rastrear', () => {
  assert.equal(podeRastrear({ lcu: { conectado: true }, fase: 'None' }).ok, true);
  assert.equal(podeRastrear({ lcu: { conectado: true }, fase: 'WatchInProgress' }).ok, true);
  assert.match(podeRastrear({ lcu: { conectado: false } }).motivo, /Abra o cliente/);
  assert.match(podeRastrear({ lcu: { conectado: true }, fase: 'InProgress' }).motivo, /partida/);
  assert.match(podeRastrear({ lcu: { conectado: true }, fase: 'Lobby' }).motivo, /lobby/);
  assert.equal(podeRastrear(null).ok, false);
});

test('texto: dano da Riot por quem bateu e superioridade', () => {
  const r = resumoDano(
    [
      { participante_id: 7, nome: 'Warwick', basico: true, slot: 69, total: 100 },
      { participante_id: 7, nome: 'Warwick', basico: false, slot: 0, total: 113 },
      { participante_id: 7, nome: 'Warwick', basico: false, slot: -1, habilidade: 'burning', total: 57 },
      { participante_id: 0, nome: 'SRU_Krug', basico: true, slot: -1, total: 20 },
      { participante_id: 6, nome: 'Teemo', basico: false, slot: 3, total: 0 },
    ],
    { 7: 'Warwick' },
  );
  assert.equal(r.length, 2);
  assert.deepEqual(r[0], { participante_id: 7, nome: 'Warwick', total: 270, partes: 'Q 113 · ataques 100 · outros 57' });
  assert.equal(r[1].nome, 'torres e monstros');
  assert.equal(superioridade({ aliados_da_vitima: 0, inimigos_da_vitima: 3 }), 'vítima em desvantagem: 3 contra 1');
  assert.equal(superioridade({ aliados_da_vitima: 1, inimigos_da_vitima: 2 }), 'em igualdade: 2 contra 2');
});

test('mapa: separa figuras sobrepostas sem passar do desvio máximo', async () => {
  const { separa } = await import('./mapa.js');
  const r = separa([{ x: 100, y: 100 }, { x: 100, y: 100 }, { x: 500, y: 500 }], 40);
  assert.ok(Math.hypot(r[0].x - r[1].x, r[0].y - r[1].y) >= 39, JSON.stringify(r));
  assert.deepEqual(r[2], { x: 500, y: 500 }, 'quem está longe não mexe');
  const t = separa([{ x: 0, y: 0 }, { x: 1, y: 0 }, { x: 0, y: 1 }, { x: 1, y: 1 }], 100, { maxDesvio: 20 });
  for (const p of t) assert.ok(Math.hypot(p.x, p.y) <= 22, 'no máximo 20 px do lugar');
  assert.deepEqual(separa([{ x: 1, y: 1 }, { x: 1, y: 1 }], 30), separa([{ x: 1, y: 1 }, { x: 1, y: 1 }], 30), 'determinístico');
});

test('mapa: agrupa quem está junto, com histerese, e o leque fica fixo', async () => {
  const { agrupa, leque, raioDoLeque } = await import('./mapa.js');
  const pts = [{ id: 1, x: 100, y: 100 }, { id: 2, x: 120, y: 100 }, { id: 3, x: 500, y: 500 }];
  const a = agrupa(pts, { junta: 40, solta: 60 });
  assert.deepEqual(a.grupos, [[0, 1]], 'quem está longe não entra');
  // Afastaram para 50: fora de "junta", mas o par já estava ligado e só solta acima de 60.
  const b = agrupa([{ id: 1, x: 100, y: 100 }, { id: 2, x: 150, y: 100 }], { junta: 40, solta: 60, ligados: a.ligados });
  assert.equal(b.grupos.length, 1, 'histerese segura o grupo');
  const c = agrupa([{ id: 1, x: 100, y: 100 }, { id: 2, x: 150, y: 100 }], { junta: 40, solta: 60 });
  assert.equal(c.grupos.length, 0, 'sem ligação anterior, 50 não junta');
  // Cadeia: 1 perto de 2, 2 perto de 3 = um grupo só.
  assert.deepEqual(agrupa([{ id: 1, x: 0, y: 0 }, { id: 2, x: 30, y: 0 }, { id: 3, x: 60, y: 0 }], { junta: 40, solta: 60 }).grupos, [[0, 1, 2]]);
  const l2 = leque(2, 40);
  assert.deepEqual(l2, [{ x: -20, y: 0 }, { x: 20, y: 0 }], 'dois lado a lado');
  const l5 = leque(5, 40);
  for (let i = 0; i < 5; i++) {
    const p = l5[i];
    const q = l5[(i + 1) % 5];
    assert.ok(Math.abs(Math.hypot(q.x - p.x, q.y - p.y) - 40) < 1e-6, 'vizinhas a 40 px');
    assert.ok(Math.abs(Math.hypot(p.x, p.y) - raioDoLeque(5, 40)) < 1e-6);
  }
  assert.deepEqual(leque(5, 40), l5, 'determinístico');
});

test('texto: eventos com o nome do campeão', async () => {
  const { eventoTexto } = await import('./texto.js');
  const nomes = { 3: 'Akshan', 8: 'LeBlanc' };
  assert.equal(eventoTexto({ tipo: 'abate', quem_id: 3, em_quem_id: 8, quem_nome: 'GodiNzt' }, nomes), 'Akshan abateu LeBlanc');
  assert.equal(eventoTexto({ tipo: 'monstro', quem_id: 2, quem_nome: 'Tião', dados: { monsterType: 'BARON_NASHOR' } }), 'Tião pegou Barão');
  assert.equal(eventoTexto({ tipo: 'estrutura', quem_id: 3, dados: { buildingType: 'TOWER_BUILDING' } }, nomes), 'Akshan derrubou uma torre');
});
