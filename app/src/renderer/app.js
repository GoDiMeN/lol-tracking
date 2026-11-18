// Arranque: barra de título (estado do cliente do LoL), rota no hash e a tela de cada rota.
// Cada tela é um módulo em views/ com `desenha(raiz, rota, ctx)`, que pode devolver a função que
// limpa o que ela ligou (SSE, intervalos, teclado) ao sair.

import { api } from './api.js';
import { lerRota, montaRota, itemDoMenu } from './rota.js';
import { $, el, preferencia, aplicaTema, TEMA_PADRAO } from './ui.js';
import { desenha as jogadores } from './views/jogadores.js';
import { desenha as rastreio } from './views/rastreio.js';
import { desenha as partidas } from './views/partidas.js';
import { desenha as partida } from './views/partida.js';
import { desenha as estudo } from './views/estudo.js';
import { desenha as replay } from './views/replay.js';
import { desenha as configuracoes } from './views/configuracoes.js';

const VISTAS = { jogadores, rastreio, partidas, partida, estudo, replay, configuracoes };

// O estado do cliente é de todas as telas: a barra de título, o botão Rastrear, as Configurações.
let ultimoEstado = null;
const ouvintesEstado = new Set();

const ctx = {
  estado: () => ultimoEstado,
  // aoMudarEstado(fn) → função que desliga.
  aoMudarEstado(fn) {
    ouvintesEstado.add(fn);
    return () => ouvintesEstado.delete(fn);
  },
  atualizaEstado: () => atualizaStatus(),
  navega(rota, { substitui = false } = {}) {
    const hash = montaRota(rota);
    if (substitui) {
      history.replaceState(null, '', hash);
      roteia();
    } else if (location.hash === hash) {
      roteia();
    } else {
      location.hash = hash;
    }
  },
  // Só troca o endereço (a tela já mostra aquilo): o "voltar" continua funcionando.
  marca(rota) {
    history.replaceState(null, '', montaRota(rota));
  },
  atualizaSeloRastreio: () => atualizaSelo(),
};

async function atualizaStatus() {
  const ponto = $('statusPonto');
  const texto = $('statusTexto');
  try {
    const e = await api.estado();
    ultimoEstado = e;
    if (e.lcu.conectado) {
      ponto.className = 'ponto ok';
      const quem = e.invocador ? `${e.invocador.nome}#${e.invocador.tag}` : '';
      const partes = ['cliente do LoL conectado', quem, e.patch];
      if (e.fase && e.fase !== 'None') partes.push(e.fase === 'WatchInProgress' ? 'assistindo replay' : 'em partida ou fila');
      if (e.modo_teste) partes.push('modo teste');
      texto.textContent = partes.filter(Boolean).join(' · ');
    } else {
      ponto.className = 'ponto cr';
      texto.textContent = e.modo_teste ? 'modo teste · cliente falso fechado' : 'cliente do LoL fechado';
      texto.title = e.lcu.erro || '';
    }
  } catch (err) {
    ultimoEstado = null;
    ponto.className = 'ponto cr';
    texto.textContent = err.status === 0 ? 'o servidor do Lane não respondeu' : err.message;
  }
  for (const fn of ouvintesEstado) fn(ultimoEstado);
}

// O selo no menu: há rastreio rodando?
async function atualizaSelo() {
  const selo = $('seloRastreio');
  try {
    const { rastreios } = await api.rastreios(5);
    const rodando = rastreios.find((r) => r.estado === 'rodando');
    selo.hidden = !rodando;
    selo.textContent = rodando ? `${Math.round((rodando.progresso || 0) * 100)}%` : '';
    selo.title = rodando ? 'rastreio em andamento' : '';
  } catch {
    selo.hidden = true;
  }
}

let limpa = null;
let geracao = 0;

async function roteia() {
  const rota = lerRota(location.hash);
  const minha = ++geracao;
  if (typeof limpa === 'function') {
    try {
      limpa();
    } catch {
      /* a tela que sai não pode travar a que entra */
    }
  }
  limpa = null;

  const cheia = rota.vista === 'replay';
  document.body.classList.toggle('modo-cheia', cheia);
  $('janela').hidden = cheia;
  $('cheia').hidden = !cheia;
  const raiz = cheia ? $('cheia') : $('conteudo');
  $('conteudo').replaceChildren();
  $('cheia').replaceChildren();

  for (const a of document.querySelectorAll('.menu a[data-menu]')) {
    const on = a.dataset.menu === itemDoMenu(rota.vista);
    a.classList.toggle('on', on);
    if (on) a.setAttribute('aria-current', 'page');
    else a.removeAttribute('aria-current');
  }

  try {
    const r = await VISTAS[rota.vista](raiz, rota, ctx);
    if (minha !== geracao) {
      // O usuário já foi para outra tela enquanto esta carregava.
      if (typeof r === 'function') r();
      return;
    }
    limpa = r;
  } catch (err) {
    if (minha !== geracao) return;
    raiz.replaceChildren(el('div', 'estado-tela erro', [el('p', '', `Esta tela não abriu: ${err.message}`), el('p', 'sub', 'Recarregue com Ctrl+R.')]));
    console.error(err);
  }
}

aplicaTema(preferencia('tema', TEMA_PADRAO));

// Botões da barra de título: só existem dentro do app (no navegador a ponte não está lá).
if (window.janelaApp) {
  $('janelaBotoes').hidden = false;
  $('winMin').addEventListener('click', () => window.janelaApp.minimiza());
  $('winMax').addEventListener('click', () => window.janelaApp.alternaMaximizada());
  $('winFecha').addEventListener('click', () => window.janelaApp.fecha());
  $('titlebar').addEventListener('dblclick', (e) => {
    if (!e.target.closest('button')) window.janelaApp.alternaMaximizada();
  });
}

window.addEventListener('hashchange', roteia);
atualizaStatus().then(roteia);
atualizaSelo();
// O cliente do LoL abre e fecha enquanto o app está aberto: a barra de título acompanha.
setInterval(atualizaStatus, 15000);
setInterval(atualizaSelo, 10000);
