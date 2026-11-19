// Testes das funções puras do Ranking do Lane (ranking.js) e da rota #ranking.
import test from 'node:test';
import assert from 'node:assert/strict';
import { lerRecorte, textoRecorte, consultaRecorte, nomeDoRanking, textoFora, formataNota, selosPosicao, textoLiga, resumoAtualizacao, corpoRastrear, deltaPosicao } from './ranking.js';
import { lerRota, montaRota, itemDoMenu } from './rota.js';

test('recorte: rota ou campeão', () => {
  assert.deepEqual(lerRecorte('JUNGLE'), { rota: 'JUNGLE', campeao: 0 });
  assert.deepEqual(lerRecorte('middle'), { rota: 'MIDDLE', campeao: 0 });
  assert.deepEqual(lerRecorte('c64'), { rota: '', campeao: 64 });
  assert.deepEqual(lerRecorte('lixo'), { rota: 'JUNGLE', campeao: 0 });
  assert.equal(textoRecorte({ rota: '', campeao: 64 }), 'c64');
  assert.equal(consultaRecorte({ patch: '16.23', elo: 'PLATINUM', recorte: 'c64', modo: 'melhor' }), 'patch=16.23&elo=PLATINUM&campeao=64&modo=melhor');
  assert.equal(consultaRecorte({ patch: '16.23', elo: 'GOLD', recorte: 'TOP' }), 'patch=16.23&elo=GOLD&rota=TOP&modo=subindo');
  assert.equal(nomeDoRanking({ tipo: 'rota', rota: 'UTILITY' }), 'suporte');
  assert.equal(nomeDoRanking({ tipo: 'campeao', campeao_id: 64, campeao: 'Lee Sin' }), 'Lee Sin');
});

test('textos da tabela', () => {
  assert.equal(textoFora({ nao_achado: 2, sem_o_recorte: 9, inativo: 0 }), '9 não jogaram o recorte no patch · 2 não achados (conta trocada ou sumiu)');
  assert.equal(textoFora({}), '');
  assert.equal(formataNota(57.256), '57,3');
  assert.deepEqual(selosPosicao({ empatado: true, amostra_pequena: true, numeros: { hot_streak: true } }).map((s) => s.texto), ['empate', 'amostra pequena', 'sequência']);
  assert.equal(textoLiga('PLATINUM', { divisao: 'II', lp: 45 }), 'Platina II 45 PDL');
  assert.equal(textoLiga('CHALLENGER', { divisao: '', lp: 1234 }), '1234 PDL');
  assert.equal(deltaPosicao(5, 2), '▲ 3');
  assert.equal(deltaPosicao(1, 4), '▼ 3');
  assert.equal(deltaPosicao(2, 2), '=');
  assert.equal(
    resumoAtualizacao({ elo: 'GOLD', patch: '16.23', fonte: 'riot', candidatos: 60, candidatos_vistos: 12, pedidos_liga: 8, pedidos_ids: 12, pedidos_partidas: 200, do_cache: 40 }),
    'Ouro · 16.23 · fonte riot · 12 de 60 candidatos · 220 pedidos à Riot (40 do cache)',
  );
});

test('rastrear a partir do ranking: sempre os 10 de cada partida', () => {
  assert.deepEqual(corpoRastrear({ elo: 'CHALLENGER', patch: '16.23', modo: 'subindo', primeiros: 3, rotas: ['JUNGLE', 'MIDDLE'] }), {
    elos: ['CHALLENGER'], funcoes: ['JUNGLE', 'MIDDLE'], quem: 'todos', ranking: { patch: '16.23', modo: 'subindo', primeiros: 3 },
  });
});

test('rota #ranking', () => {
  const r = { vista: 'ranking', patch: '16.23', elo: 'PLATINUM', recorte: 'c64', modo: 'melhor' };
  assert.equal(montaRota(r), '#ranking/16.23/PLATINUM/c64/melhor');
  assert.deepEqual(lerRota(montaRota(r)), r);
  assert.deepEqual(lerRota('#ranking'), { vista: 'ranking', patch: null, elo: null, recorte: null, modo: null });
  assert.equal(lerRota('#ranking/16.23/GOLD/TOP/qualquer').modo, null);
  assert.equal(itemDoMenu('ranking'), 'estudo');
});
