// Testes das funções puras do Especialista Lane (especialista.js) e da rota #especialista.
import test from 'node:test';
import assert from 'node:assert/strict';
import { nomeEloEsp, nomeRecorte, estadoRelatorio, estadoSintese, avisoRastreados, rotuloRegra, resumoEspelho, porRecorte } from './especialista.js';
import { lerRota, montaRota, itemDoMenu } from './rota.js';

test('nomes do recorte', () => {
  assert.equal(nomeEloEsp('PLATINUM II'), 'Platina II');
  assert.equal(nomeEloEsp('CHALLENGER'), 'Desafiante');
  assert.equal(nomeEloEsp(''), '');
  assert.equal(nomeRecorte({ patch: '16.23', elo: 'PLATINUM', rota: 'JUNGLE' }), '16.23 · Platina · selva');
});

test('estado do relatório e da síntese', () => {
  assert.equal(estadoRelatorio({ relatorio: null }).botao, 'Gerar relatório');
  assert.deepEqual(estadoRelatorio({ relatorio: { estado: 'concluido', versao: 2 } }), { texto: 'relatório v2', classe: 'ok', botao: 'Refazer' });
  assert.equal(estadoRelatorio({ relatorio: { estado: 'falhou', versao: 1 } }).classe, 'cr');
  assert.equal(estadoRelatorio({ execucao: { id: 'x' }, relatorio: null }).botao, null);
  assert.equal(estadoSintese({ relatorios: 0, sintese: null }).texto, 'sem relatório para sintetizar');
  assert.equal(estadoSintese({ relatorios: 3, sintese: { estado: 'concluido', versao: 1, relatorios_usados: 2, desatualizada: true } }).texto, 'síntese v1 desatualizada (usou 2 de 3)');
  assert.equal(estadoSintese({ relatorios: 1, sintese: { estado: 'concluido', versao: 1, relatorios_usados: 1 } }).texto, 'síntese v1 (1 relatório)');
});

test('rastreados, regra e espelho', () => {
  assert.equal(avisoRastreados(10), '');
  assert.match(avisoRastreados(1), /^1 de 10 rastreados/);
  assert.equal(rotuloRegra({ jogadores: 2, partidas: [1, 2] }), 'em 2 jogadores · 2 partidas');
  assert.equal(rotuloRegra({ jogadores: 1, partidas: [1] }), 'HIPÓTESE · 1 jogador');
  assert.equal(resumoEspelho({ caderno: '', ferramenta: true, itens: [] }), 'NotebookLM: informe o caderno');
  assert.equal(resumoEspelho({ caderno: 'x', ferramenta: false, itens: [] }), 'NotebookLM: sem a ferramenta nlm (exporte à mão)');
  assert.equal(resumoEspelho({ caderno: 'x', ferramenta: true, itens: [{}], enviados: 1, pendentes: 1 }), 'NotebookLM: 1 no caderno, 1 para enviar');
  const g = porRecorte([{ patch: '16.23', elo_tier: 'PLATINUM', rota: 'JUNGLE' }, { patch: '16.23', elo_tier: 'PLATINUM', rota: 'JUNGLE' }, { patch: '16.22', elo_tier: 'GOLD', rota: 'TOP' }]);
  assert.equal(g.get('16.23|PLATINUM|JUNGLE').length, 2);
});

test('rota #especialista', () => {
  assert.deepEqual(lerRota('#especialista'), { vista: 'especialista', tipo: null });
  assert.deepEqual(lerRota('#especialista/relatorio/3305481832/7'), { vista: 'especialista', tipo: 'relatorio', gameId: 3305481832, participante: 7, versao: null });
  assert.deepEqual(lerRota('#especialista/sintese/16.23/PLATINUM/JUNGLE'), { vista: 'especialista', tipo: 'sintese', patch: '16.23', elo: 'PLATINUM', rota: 'JUNGLE', versao: null });
  assert.equal(montaRota({ vista: 'especialista', tipo: 'relatorio', gameId: 1, participante: 7, versao: 2 }), '#especialista/relatorio/1/7/2');
  assert.equal(montaRota({ vista: 'especialista', tipo: 'sintese', patch: '16.23', elo: 'PLATINUM', rota: 'JUNGLE' }), '#especialista/sintese/16.23/PLATINUM/JUNGLE');
  assert.equal(itemDoMenu('especialista'), 'estudo');
});
