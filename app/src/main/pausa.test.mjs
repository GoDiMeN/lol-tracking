// Testes da pausa da captura pelo processo principal (pausa.js), sem abrir janela: o atalho global
// (com um globalShortcut falso), a rota que ele chama, o título e as notificações.
import test from 'node:test';
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const pausa = require('./pausa.js');

// Um servidor falso: GET /api/captura e POST /api/captura/alternar, com o estado em memória.
function servidorFalso(inicial) {
  const s = { estado: inicial, pedidos: [] };
  s.fetch = async (url, opcoes = {}) => {
    const caminho = new URL(url).pathname;
    s.pedidos.push(`${opcoes.method || 'GET'} ${caminho}`);
    const resp = (status, corpo) => ({ ok: status < 400, status, json: async () => corpo });
    if (caminho === '/api/captura' && !opcoes.method) return resp(200, s.estado);
    if (caminho === '/api/captura/alternar' && opcoes.method === 'POST') {
      if (s.estado.rodando) {
        const c = { ...s.estado.rodando };
        s.estado = { rodando: null, pausada: { ...c, motivo: '' } };
        return resp(200, { acao: 'pausou', captura: c, execucao: null });
      }
      if (s.estado.pausada) {
        const c = { ...s.estado.pausada, motivo: '' };
        s.estado = { rodando: c, pausada: null };
        return resp(200, { acao: 'retomou', captura: c, execucao: { id: 'e1' } });
      }
      return resp(409, { erro: 'não há captura pausada para retomar' });
    }
    return resp(404, { erro: 'rota' });
  };
  return s;
}

function atalhosFalsos({ ocupados = [] } = {}) {
  const g = { registrados: new Map() };
  g.register = (acc, fn) => {
    if (ocupados.includes(acc)) return false;
    g.registrados.set(acc, fn);
    return true;
  };
  g.unregister = (acc) => g.registrados.delete(acc);
  g.isRegistered = (acc) => g.registrados.has(acc);
  g.aperta = async (acc) => g.registrados.get(acc)();
  return g;
}

function notificacoesFalsas() {
  const lista = [];
  class N {
    constructor(o) {
      this.o = o;
    }
    show() {
      lista.push(this.o);
    }
    static isSupported() {
      return true;
    }
  }
  return { N, lista };
}

const rastreio = { tipo: 'rastreio', id: 9, game_id: 3305486988, execucao: { id: 'e0' }, esperando: null, pausando: false };

test('atalhoValido: precisa de Ctrl ou Alt e de uma tecla conhecida', () => {
  for (const ok of ['Control+Alt+P', 'Ctrl+Shift+F9', 'Alt+Pause', 'CommandOrControl+Alt+1']) assert.ok(pausa.atalhoValido(ok), ok);
  for (const ruim of ['P', 'Shift+P', 'Control+Control+P', 'Control+', 'Control+Alt+Ç', '', null, 'Banana+P']) {
    assert.ok(!pausa.atalhoValido(ruim), String(ruim));
  }
  assert.equal(pausa.legivel('Control+Alt+P'), 'Ctrl+Alt+P');
});

test('o atalho global pausa a captura em andamento e, de novo, retoma; título e avisos acompanham', async () => {
  const srv = servidorFalso({ rodando: rastreio, pausada: null });
  const g = atalhosFalsos();
  const { N, lista } = notificacoesFalsas();
  const titulos = [];
  const c = pausa.criaControle({ globalShortcut: g, Notification: N, fetch: srv.fetch, base: 'http://127.0.0.1:8770',
    aoMudar: (e) => titulos.push(pausa.titulo(e)) });
  assert.deepEqual(c.registra(), { ok: true, atalho: 'Control+Alt+P' });
  await c.atualiza();
  assert.equal(titulos.at(-1), 'Lane — capturando');

  await g.aperta('Control+Alt+P');
  assert.ok(srv.pedidos.includes('POST /api/captura/alternar'));
  assert.equal(titulos.at(-1), 'Lane — captura PAUSADA');
  assert.equal(lista.at(-1).title, 'Captura pausada');
  assert.match(lista.at(-1).body, /rastreio da partida 3305486988 pausou/);
  assert.match(lista.at(-1).body, /Ctrl\+Alt\+P/);

  await g.aperta('Control+Alt+P');
  assert.equal(titulos.at(-1), 'Lane — capturando');
  assert.equal(lista.at(-1).title, 'Captura retomada');

  // Nada rodando nem pausado: a frase do servidor vira aviso, sem quebrar.
  srv.estado = { rodando: null, pausada: null };
  await c.atualiza();
  await g.aperta('Control+Alt+P');
  assert.equal(lista.at(-1).body, 'não há captura pausada para retomar');
  c.solta();
  assert.equal(g.registrados.size, 0);
});

test('trocar o atalho: o novo entra e o velho sai; ocupado ou inválido mantém o velho', () => {
  const g = atalhosFalsos({ ocupados: ['Control+Alt+F12'] });
  const c = pausa.criaControle({ globalShortcut: g, fetch: async () => ({ ok: false }), base: 'http://x' });
  c.registra();
  assert.equal(c.registra('Control+Shift+F9').ok, true);
  assert.deepEqual([...g.registrados.keys()], ['Control+Shift+F9']);
  const ocupado = c.registra('Control+Alt+F12');
  assert.equal(ocupado.ok, false);
  assert.match(ocupado.erro, /já é usado/);
  assert.equal(c.registra('P').ok, false);
  assert.deepEqual([...g.registrados.keys()], ['Control+Shift+F9']);
});

test('avisos: espera pela pessoa e pausa decidida pelo motor (com o motivo); sem repetir', () => {
  const rodando = { rodando: rastreio, pausada: null };
  const esperando = { rodando: { ...rastreio, esperando: { motivo: 'teclado usado há 0,4 s' } }, pausada: null };
  const pausadaMotor = { rodando: null, pausada: { ...rastreio, motivo: 'a câmera não prende no jogador' } };
  assert.equal(pausa.aviso(rodando, esperando, 'Control+Alt+P').titulo, 'Captura esperando você');
  const n = pausa.aviso(esperando, pausadaMotor, 'Control+Alt+P');
  assert.equal(n.titulo, 'Captura pausada');
  assert.match(n.corpo, /a câmera não prende no jogador/);
  assert.equal(pausa.aviso(rodando, rodando, 'Control+Alt+P'), null);
  // Pausa pedida: avisa no "pausando"; quando termina de fechar, não avisa de novo.
  const pausando = { rodando: { ...rastreio, pausando: true }, pausada: null };
  assert.equal(pausa.aviso(rodando, pausando, 'x').titulo, 'Captura pausada');
  assert.equal(pausa.aviso(pausando, { rodando: null, pausada: rastreio }, 'x'), null);
  assert.equal(pausa.titulo(esperando), 'Lane — captura esperando você parar de usar o computador');
  assert.equal(pausa.situacao(null), 'sem_servidor');
});
