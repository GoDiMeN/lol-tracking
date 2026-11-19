// Arranque: barra de título (estado do cliente do LoL), rota no hash e a tela de cada rota.
// Cada tela é um módulo em views/ com `desenha(raiz, rota, ctx)`, que pode devolver a função que
// limpa o que ela ligou (SSE, intervalos, teclado) ao sair.

import { api } from './api.js';
import { lerRota, montaRota, itemDoMenu } from './rota.js';
import { $, el, preferencia, aplicaTema, TEMA_PADRAO, toast } from './ui.js';
import { desenha as jogadores } from './views/jogadores.js';
import { desenha as rastreio } from './views/rastreio.js';
import { desenha as partidas } from './views/partidas.js';
import { desenha as partida } from './views/partida.js';
import { desenha as estudo } from './views/estudo.js';
import { desenha as replay } from './views/replay.js';
import { desenha as configuracoes } from './views/configuracoes.js';
import { desenha as ia } from './views/ia.js';
import { desenha as especialista } from './views/especialista.js';
import { desenha as ranking } from './views/ranking.js';
import * as telaCheia from './telacheia.js';
import { botaoDaCaptura } from './captura.js';

const VISTAS = { jogadores, rastreio, partidas, partida, estudo, especialista, ranking, replay, ia, configuracoes };

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
    // O Estudo só existe neste servidor com LOL_ESTUDO=1: desligado, o item some do menu.
    const itemEstudo = document.querySelector('.menu a[data-menu="estudo"]');
    if (itemEstudo) itemEstudo.hidden = e.estudo === false;
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

// O botão "Pausar captura"/"Retomar captura" da barra de título (captura.js). O estado vem de
// GET /api/captura a cada 3 s e, dentro do app, também do processo principal (que acompanha o
// atalho global Ctrl+Alt+P e a bandeja) na hora em que muda.
let atalhoPausa = 'Ctrl+Alt+P';
function mostraCaptura(estado) {
  const bt = $('capturaBtn');
  const b = botaoDaCaptura(estado, atalhoPausa);
  bt.hidden = !b.visivel;
  bt.textContent = b.texto;
  bt.title = b.dica;
  bt.className = `captura-btn ${b.classe}`;
  bt.disabled = !b.acao;
  bt.dataset.acao = b.acao || '';
}
async function atualizaCaptura() {
  try {
    mostraCaptura(await api.captura());
  } catch {
    mostraCaptura(null);
  }
}
$('capturaBtn').addEventListener('click', async () => {
  const bt = $('capturaBtn');
  const acao = bt.dataset.acao;
  if (!acao) return;
  bt.disabled = true;
  try {
    if (acao === 'pausar') {
      await api.pausaCaptura();
      toast('Captura pausada: o replay está fechando. Para continuar, "Retomar captura".', 'ok');
    } else {
      await api.retomaCaptura();
      toast('Captura retomada.', 'ok');
    }
  } catch (err) {
    toast(err.message, 'erro');
  }
  await atualizaCaptura();
  window.janelaApp?.captura?.atualiza?.();
  atualizaSelo();
});
if (window.janelaApp?.captura) {
  window.janelaApp.captura.aoMudar((estado) => mostraCaptura(estado));
  window.janelaApp.captura
    .atalho()
    .then((a) => {
      if (a) atalhoPausa = a.replace(/Control|CommandOrControl|CmdOrCtrl/g, 'Ctrl');
    })
    .catch(() => {});
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
  // A tela atual no atributo: o CSS limita a largura das telas de lista e formulário em monitor
  // largo (2560 px), sem mexer na da partida, que usa a largura nos gráficos.
  $('conteudo').dataset.vista = rota.vista;
  $('conteudo').replaceChildren();
  $('cheia').replaceChildren();

  for (const a of document.querySelectorAll('.menu a[data-menu]')) {
    const on = a.dataset.menu === itemDoMenu(rota.vista);
    a.classList.toggle('on', on);
    if (on) a.setAttribute('aria-current', 'page');
    else a.removeAttribute('aria-current');
  }

  // Estudo desligado no servidor: as telas dele não abrem (as rotas respondem 404).
  if (ultimoEstado?.estudo === false && itemDoMenu(rota.vista) === 'estudo') {
    raiz.replaceChildren(el('div', 'estado-tela vazio', 'O Estudo não está disponível neste servidor.'));
    return;
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
  telaCheia.aoMudar((ligada) => {
    const bt = $('winCheia');
    const rot = ligada ? 'Sair da tela cheia (F11)' : 'Tela cheia (F11)';
    bt.title = rot;
    bt.setAttribute('aria-label', rot.replace(' (F11)', ''));
    bt.setAttribute('aria-pressed', String(ligada));
  });
  $('winCheia').addEventListener('click', () => telaCheia.alterna());
  $('titlebar').addEventListener('dblclick', (e) => {
    if (!e.target.closest('button')) window.janelaApp.alternaMaximizada();
  });
}

// Esc sai da tela cheia quando a tela não usou o Esc para outra coisa (o mapa com play fecha o
// painel primeiro; um diálogo aberto fecha o diálogo). No window: roda depois dos ouvintes das telas.
window.addEventListener('keydown', (ev) => {
  // F11 dentro do app: o processo principal já pega a tecla antes da página (before-input-event);
  // se ela chegar aqui mesmo assim (tecla sintética, foco em outro lugar), a página alterna.
  if (ev.key === 'F11' && window.janelaApp && !ev.defaultPrevented) {
    ev.preventDefault();
    telaCheia.alterna();
    return;
  }
  if (ev.key !== 'Escape' || ev.defaultPrevented || !telaCheia.estaLigada()) return;
  if (document.querySelector('dialog[open]')) return;
  telaCheia.poe(false);
});

window.addEventListener('hashchange', roteia);
atualizaStatus().then(roteia);
atualizaSelo();
atualizaCaptura();
setInterval(atualizaCaptura, 3000);
// O cliente do LoL abre e fecha enquanto o app está aberto: a barra de título acompanha.
setInterval(atualizaStatus, 15000);
setInterval(atualizaSelo, 10000);
