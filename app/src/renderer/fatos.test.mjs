// Testes da lógica pura da base de fatos na tela (fatos.js).
import test from 'node:test';
import assert from 'node:assert/strict';

import { rotuloTipo, classeTipo, ouroCurto, momentosChave, segundoNoMapa, jogadoresChave, textoContexto } from './fatos.js';

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
