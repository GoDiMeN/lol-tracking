// Testes da lógica pura da base de fatos na tela (fatos.js).
import test from 'node:test';
import assert from 'node:assert/strict';

import {
  rotuloTipo,
  classeTipo,
  ouroCurto,
  momentosChave,
  segundoNoMapa,
  jogadoresChave,
  textoContexto,
  papelNaTela,
  estadoBuscaContexto,
  progressoBusca,
} from './fatos.js';

test('momentos-chave: só os da lista curta, do maior impacto para o menor', () => {
  const resumo = {
    momentos: [
      { n: 1, t_ms: 136000, tipo: 'abate', impacto: 520, chave: true },
      { n: 2, t_ms: 500000, tipo: 'luta', impacto: 4367, chave: true },
      { n: 3, t_ms: 600000, tipo: 'abate', impacto: 9000, chave: false },
      { n: 4, t_ms: 100000, tipo: 'objetivo', impacto: 520, chave: true },
    ],
  };
  assert.deepEqual(
    momentosChave(resumo).map((m) => m.n),
    [2, 4, 1],
  );
  assert.deepEqual(momentosChave(null), []);
});

test('rótulo e cor do tipo de momento', () => {
  assert.equal(rotuloTipo('abate', 'morte_cara'), 'Morte cara');
  assert.equal(rotuloTipo('janela_perdida'), 'Janela perdida');
  assert.equal(rotuloTipo('novo'), 'novo');
  assert.equal(classeTipo('janela_perdida'), 'cr');
  assert.equal(classeTipo('janela_aproveitada'), 'ok');
  assert.equal(classeTipo('torre'), 'ac');
  assert.equal(classeTipo('abate'), 'n');
});

test('ouro curto com sinal e o instante do mapa', () => {
  assert.equal(ouroCurto(1234), '1,2 mil');
  assert.equal(ouroCurto(-850), '−850');
  assert.equal(ouroCurto(850, true), '+850');
  assert.equal(ouroCurto(-4022, true), '−4,0 mil');
  assert.equal(ouroCurto(null), '—');
  assert.equal(segundoNoMapa(136000), 131);
  assert.equal(segundoNoMapa(2000), 0);
});

test('jogadores-chave na ordem da nota e o contexto numa linha', () => {
  const r = { jogadores: [{ campeao: 'Vi', posicao_nota: 8 }, { campeao: 'Leona', posicao_nota: 1 }] };
  assert.deepEqual(
    jogadoresChave(r).map((j) => j.campeao),
    ['Leona', 'Vi'],
  );
  const ctx = { funcao_principal: 'MIDDLE', fora_da_funcao: true, ultimas: 10, vitorias_ultimas: 5, sequencia: -3, elo: null };
  assert.equal(
    textoContexto(ctx, 'JUNGLE', (f) => ({ MIDDLE: 'meio', JUNGLE: 'caçador' })[f]),
    'principal: meio (aqui fora dela, como caçador) · 5 vitória(s) nas últimas 10 · vinha de 3 derrotas seguidas',
  );
  assert.equal(textoContexto({ ultimas: 0, sequencia: 0 }, 'TOP'), '');
});

test('contexto dos 10: fração da função, campeão e o alcance do histórico buscado', () => {
  const nome = (f) => ({ MIDDLE: 'meio', UTILITY: 'suporte' })[f];
  const sup = { funcao_principal: 'MIDDLE', fracao_principal: 0.8, fora_da_funcao: true, mono: false, sem_o_campeao: true, campeao_vezes: 0,
    campeao_de: 10, ultimas: 10, vitorias_ultimas: 8, sequencia: 5, elo: { elo: 'EMERALD', divisao: 'II' } };
  assert.equal(
    textoContexto(sup, 'UTILITY', nome),
    'principal: meio (80%) (aqui fora dela, como suporte) · nenhuma das últimas 10 com ele · 8 vitória(s) nas últimas 10 · vinha de 5 vitórias seguidas · elo EMERALD II',
  );
  const mono = { funcao_principal: 'MIDDLE', fracao_principal: 1, mono: true, campeao_vezes: 9, campeao_de: 10, ultimas: 10, vitorias_ultimas: 4,
    sequencia: 1, alcance: 'só 3 partida(s) de antes ao alcance do cliente do LoL' };
  assert.equal(textoContexto(mono, 'MIDDLE', nome), 'principal: meio · mono do campeão (9 de 10) · 4 vitória(s) nas últimas 10 · só 3 partida(s) de antes ao alcance do cliente do LoL');
});

test('estado da busca do contexto: o que foi buscado, de onde, e se dá para buscar', () => {
  const nada = estadoBuscaContexto({ jogadores: [], faltam: [1, 2, 3, 4, 5, 6, 7, 8, 9, 10], fontes: { cliente: false, riot: false, motivo_cliente: 'o cliente do LoL está fechado', motivo_riot: 'sem chave da Riot (token.txt)' } });
  assert.equal(nada.pode, false);
  assert.equal(nada.dica, 'o cliente do LoL está fechado; sem chave da Riot (token.txt)');
  assert.match(nada.texto, /ainda não buscado/);
  const feito = estadoBuscaContexto({
    jogadores: [
      { fonte: 'riot', buscado_em: '2026-11-18T10:00:00Z', estado: 'ok' },
      { fonte: 'riot', buscado_em: '2026-11-18T10:00:05Z', estado: 'nao_alcanca' },
    ],
    faltam: [],
    fontes: { cliente: false, riot: true },
  });
  assert.equal(feito.pode, true);
  assert.equal(feito.texto, 'contexto buscado de 2 de 2 (API da Riot, 2026-11-18) · 1 com histórico incompleto ou não achado');
  assert.equal(estadoBuscaContexto({ rodando: true, jogadores: [], faltam: [], fontes: { cliente: true } }).pode, false);
  assert.equal(progressoBusca(3, 10, 'cliente'), 'buscando pelo cliente do LoL: 3 de 10 jogadores');
});

test('papelNaTela: pílula, estilo e a dica com números, confiança e o que não deu para avaliar', () => {
  const p = papelNaTela({
    papel: 'carregou', rotulo: 'carregou', confianca: 'media', motivos: ['38% do ouro do time'], estilo: 'ganks', estilo_rotulo: 'ganks',
    confianca_estilo: 'alta', motivos_estilo: ['6 abate(s)/assistência(s) nas rotas antes dos 14 min'], nao_avaliado: ['luta pela etiqueta'], fonte: 'fim de jogo',
  });
  assert.equal(p.texto, 'carregou');
  assert.equal(p.classe, 'ok');
  assert.equal(p.estilo, 'ganks');
  assert.match(p.dica, /confiança média\): 38% do ouro do time/);
  assert.match(p.dica, /não avaliado: luta pela etiqueta/);
  assert.equal(papelNaTela(null), null);
  assert.equal(papelNaTela({ papel: '' }), null);
});
