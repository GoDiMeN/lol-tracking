// Testes das funções puras da IA (ia.js): links de instantes e partidas, Markdown simples, zonas e
// a montagem do quadro do mapa de uma situação.
import test from 'node:test';
import assert from 'node:assert/strict';
import { msDe, rotaDoInstante, segmentos, blocosMarkdown, celulasDaZona, centroDaZona, montaQuadro, separaVozes, perguntaDaSituacao, linhasMaterial } from './ia.js';

test('msDe e rotaDoInstante', () => {
  assert.equal(msDe('8:48'), 528000);
  assert.equal(msDe('12:05'), 725000);
  assert.equal(msDe('8:60'), null);
  assert.equal(msDe('x'), null);
  assert.equal(rotaDoInstante(3304217746, 528000), '#replay/3304217746/523');
});

test('segmentos: instantes da partida padrão e citações de outra partida', () => {
  const s = segmentos('Aos 8:48 Vi entrou; veja (partida 3305449546, 12:18) e a partida 3304217746.', 3304217746);
  assert.deepEqual(
    s.map((x) => [x.tipo, x.v, x.gameId ?? null]),
    [
      ['texto', 'Aos ', null],
      ['instante', '8:48', 3304217746],
      ['texto', ' Vi entrou; veja (', null],
      ['partida', 'partida 3305449546', 3305449546],
      ['texto', ', ', null],
      ['instante', '12:18', 3305449546],
      ['texto', ') e a ', null],
      ['partida', 'partida 3304217746', 3304217746],
      ['texto', '.', null],
    ],
  );
  // Sem partida padrão (conversa geral) um instante solto fica texto.
  assert.deepEqual(segmentos('às 8:48'), [{ tipo: 'texto', v: 'às 8:48' }]);
});

test('blocosMarkdown: títulos, listas, parágrafos e negrito, sem HTML', () => {
  const b = blocosMarkdown('**Situação**: Vi sozinha\ncontinua aqui\n\n- um\n2. dois\n### Título\n<b>x</b>');
  assert.equal(b.length, 5);
  assert.deepEqual(b[0], { tipo: 'p', partes: [{ texto: 'Situação', forte: true }, { texto: ': Vi sozinha continua aqui', forte: false }] });
  assert.equal(b[1].tipo, 'li');
  assert.equal(b[2].partes[0].texto, 'dois');
  assert.equal(b[3].tipo, 'h');
  assert.equal(b[4].partes[0].texto, '<b>x</b>');
});

const grade = { origem: { x: 0, y: 0 }, celula: 100, colunas: 3, codigos: ['rio_baixo', 'rota_meio'], linhas: ['AAB', 'ABB'] };

test('zonas na grade', () => {
  assert.equal(celulasDaZona(grade, 'rio_baixo').length, 3);
  assert.deepEqual(centroDaZona(grade, 'rota_meio'), { x: (250 + 150 + 250) / 3, y: (50 + 150 + 150) / 3 });
  assert.equal(centroDaZona(grade, 'base_azul'), null);
});

test('montaQuadro: foco por cima, realce, visão do time do foco, sentinelas vivas, seta e região', () => {
  const quadro = {
    jogadores: [
      { participante_id: 7, x: 100, y: 100, origem: 'camera', morto: false },
      { participante_id: 2, x: 250, y: 150, origem: 'linha_do_tempo', morto: false },
      { participante_id: 3, x: null, y: null, morto: true },
      { participante_id: 9, x: 50, y: 50, origem: 'camera', morto: true },
    ],
  };
  const r = montaQuadro({
    quadro,
    t: 10000,
    foco: 7,
    timeFoco: 200,
    grade,
    atividades: [{ participante_id: 7, t_ms: 9000, fim_ms: 12000, atividade: 'gankando', com: [2] }],
    sentinelas: [
      { x: 10, y: 10, t_ms: 1000, ate_ms: 20000, time: 100, tipo_nome: 'Amarela' },
      { x: 10, y: 10, t_ms: 1000, ate_ms: 5000, time: 100 },
      { x: null, y: null, t_ms: 1000, ate_ms: null, time: 200 },
    ],
    visao: { times: [{ time: 200, jogadores: [{ participante_id: 2, visivel: [[0, 9000]], fora: [[9000, 20000]] }] }, { time: 100, jogadores: [{ participante_id: 7, visivel: [[0, 99999]] }] }] },
    anotacao: { realce: [2], seta: { de: { participante: 7 }, para: { zona: 'rota_meio' } }, regiao: 'rio_baixo' },
  });
  assert.deepEqual(r.marcas.map((m) => m.pid), [9, 2, 7]);
  const lee = r.marcas.find((m) => m.pid === 2);
  assert.equal(lee.realce, true);
  assert.equal(lee.aproximada, true);
  assert.equal(lee.visao, 'fora');
  assert.equal(r.marcas.find((m) => m.pid === 7).visao, '', 'a visão é a do time do foco sobre os inimigos');
  assert.equal(r.marcas.find((m) => m.pid === 9).morto, true);
  assert.equal(r.sentinelas.length, 1);
  assert.deepEqual(r.seta.de, { x: 100, y: 100 });
  assert.ok(r.seta.para.x > 100);
  assert.equal(r.regiao, 'rio_baixo');
  assert.equal(r.atividadeFoco.atividade, 'gankando');
  assert.equal(r.temVisao, true);
  // Seta para quem não tem posição: cai.
  assert.equal(montaQuadro({ quadro, t: 1, foco: 7, anotacao: { seta: { de: { participante: 3 }, para: { participante: 7 } } } }).seta, null);
});

test('separaVozes e perguntaDaSituacao', () => {
  const { com, sem } = separaVozes([{ voz: 'a', sem_dados: true }, { voz: 'b', sem_dados: false }]);
  assert.deepEqual([com.length, sem.length], [1, 1]);
  assert.equal(perguntaDaSituacao({ t: '8:48', situacao: 'Vi sozinha.', pergunta: 'Devia entrar?' }), 'Situação aos 8:48: Vi sozinha.\n\nDevia entrar?\n\nO que o especialista diz?');
});

test('linhasMaterial: temas com motivo, arquivo único e voz sem material', () => {
  const l = linhasMaterial([
    { voz: 'profissional', caracteres: 9000, temas: [{ tema: 'funcao-selva', motivos: ['função do jogador em foco (caçador)'] }, { tema: 'objetivos', motivos: ['antes_de_objetivo (S2)', 'objetivo (S4)'] }], fora: ['lutas: não coube no teto de 18000 caracteres'] },
    { voz: 'especialista_soloq', caracteres: 3000, temas: [], arquivos: ['especialista-solo-queue.md'] },
    { voz: 'mono', temas: [], arquivos: [], partidas: 0, falta: 'sem o arquivo mono-vi.md' },
  ]);
  assert.deepEqual(l, [
    'profissional: 9000 caracteres, temas funcao-selva (função do jogador em foco (caçador)), objetivos (antes_de_objetivo (S2); objetivo (S4)); fora do teto: lutas',
    'especialista_soloq: 3000 caracteres, especialista-solo-queue.md',
    'mono: sem material (sem o arquivo mono-vi.md)',
  ]);
  assert.deepEqual(linhasMaterial(undefined), []);
});

test('estadoDaVoz diz quem fala e o que falta', async () => {
  const { estadoDaVoz, textoEloComparacao } = await import('./ia.js');
  const fala = estadoDaVoz({ voz: 'melhores_do_elo', arquivos: [], temas: [], partidas: 1, amostra: '1 partida de 1 jogador de Platina II na selva', falta: '' });
  assert.equal(fala.fala, true);
  assert.equal(fala.texto, 'referência: 1 partida de 1 jogador de Platina II na selva');
  const muda = estadoDaVoz({ voz: 'esmeralda_recem', arquivos: [], temas: [], partidas: 0, falta: 'faltam partidas de Esmeralda na selva: rode a coleta por elo em Esmeralda' });
  assert.equal(muda.fala, false);
  assert.match(muda.texto, /faltam partidas de Esmeralda na selva/);
  assert.match(textoEloComparacao({ fonte: 'riot', riot: { elo: 'PLATINUM', divisao: 'II', consultado_em: '2026-11-18T10:00:00Z' } }), /Riot: PLATINUM II \(2026-11-18\)/);
  assert.match(textoEloComparacao({ fonte: '' }), /não conhecido/);
});
