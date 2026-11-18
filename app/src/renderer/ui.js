// Peças de interface compartilhadas pelas telas. Dependem do DOM só quando chamadas.
//
// Todo texto que vem do servidor entra por textContent: nome de jogador é texto livre de terceiros.

import { api, imagemCampeao } from './api.js';

export const $ = (id) => document.getElementById(id);

// el('div', 'classe', 'texto') ou el('div', 'classe', [filhos]).
export function el(tag, classe, conteudo) {
  const e = document.createElement(tag);
  if (classe) e.className = classe;
  if (Array.isArray(conteudo)) e.append(...conteudo.filter((c) => c !== null && c !== undefined && c !== false));
  else if (conteudo !== undefined && conteudo !== null) e.textContent = String(conteudo);
  return e;
}

export function botao(texto, classe, aoClicar, titulo) {
  const b = el('button', `btn ${classe || ''}`.trim(), texto);
  b.type = 'button';
  if (titulo) b.title = titulo;
  if (aoClicar) b.addEventListener('click', aoClicar);
  return b;
}

export function link(texto, href, classe) {
  const a = el('a', classe || '', texto);
  a.href = href;
  return a;
}

export function pilula(texto, classe = 'n', titulo) {
  const p = el('span', `pill ${classe}`, texto);
  if (titulo) p.title = titulo;
  return p;
}

export function cartao(titulo, direita, corpo, classe = '') {
  const c = el('section', `cartao ${classe}`.trim());
  if (titulo !== null) {
    const cab = el('div', 'cab');
    cab.append(typeof titulo === 'string' ? el('h2', '', titulo) : titulo);
    if (direita) cab.append(typeof direita === 'string' ? el('span', 'dir', direita) : el('span', 'dir', [direita]));
    c.append(cab);
  }
  if (corpo) c.append(...(Array.isArray(corpo) ? corpo : [corpo]));
  return c;
}

// Estados de tela: carregando, vazio e erro escritos para quem joga.
export const carregando = (texto = 'Carregando…') => el('div', 'estado-tela carregando', texto);
export const vazio = (texto, acao) => el('div', 'estado-tela vazio', [el('p', '', texto), acao || null]);
export function erroNaTela(err, oQueFazer) {
  const msg = err?.status === 0 ? 'O servidor do Lane não respondeu.' : `Não deu certo: ${err?.message || err}.`;
  return el('div', 'estado-tela erro', [
    el('p', '', msg),
    el('p', 'sub', oQueFazer || (err?.status === 0 ? 'Feche e abra o app de novo.' : 'Tente de novo; se repetir, recarregue a tela (Ctrl+R).')),
  ]);
}

// Retrato do campeão com a borda da cor do time. Sem imagem (modo teste, sem rede na 1ª vez), a
// inicial do campeão fica no lugar.
export function retrato(participante, tamanho = 28, classe = '') {
  const r = el('span', `retrato ${participante?.time === 200 ? 'vermelho' : 'azul'} ${classe}`.trim());
  r.style.width = `${tamanho}px`;
  r.style.height = `${tamanho}px`;
  const ini = el('b', '', (participante?.campeao || '?').slice(0, 1));
  r.append(ini);
  if (participante?.campeao_id) {
    const img = new Image();
    img.alt = '';
    img.decoding = 'async';
    img.addEventListener('load', () => r.classList.add('com-imagem'));
    img.addEventListener('error', () => img.remove());
    img.src = imagemCampeao(participante.campeao_id);
    r.append(img);
  }
  return r;
}

// Toasts empilhados (no máximo 3).
export function toast(texto, tipo = '') {
  const caixa = $('toasts');
  if (!caixa) return;
  const t = el('div', `toast ${tipo}`.trim(), texto);
  t.setAttribute('role', tipo === 'erro' ? 'alert' : 'status');
  caixa.append(t);
  while (caixa.children.length > 3) caixa.firstElementChild.remove();
  setTimeout(() => t.remove(), tipo === 'erro' ? 7000 : 4000);
}

// Confirmação no próprio botão (sem confirm()): o primeiro clique pergunta, o segundo em 4 s faz.
export function confirmaNoBotao(b, pergunta, acao) {
  if (b.dataset.confirmando === '1') {
    clearTimeout(Number(b.dataset.timer));
    b.dataset.confirmando = '';
    b.textContent = b.dataset.rotulo;
    b.classList.remove('confirmando');
    acao();
    return;
  }
  b.dataset.rotulo = b.textContent;
  b.dataset.confirmando = '1';
  b.textContent = pergunta;
  b.classList.add('confirmando');
  b.dataset.timer = String(
    setTimeout(() => {
      b.dataset.confirmando = '';
      b.textContent = b.dataset.rotulo;
      b.classList.remove('confirmando');
    }, 4000),
  );
}

// barra de progresso acessível (0–1).
export function barra(fracao, rotulo, classe = '') {
  const b = el('div', `barra ${classe}`.trim());
  b.setAttribute('role', 'progressbar');
  b.setAttribute('aria-label', rotulo);
  b.setAttribute('aria-valuemin', '0');
  b.setAttribute('aria-valuemax', '100');
  const pct = Math.round(Math.min(1, Math.max(0, fracao || 0)) * 100);
  b.setAttribute('aria-valuenow', String(pct));
  const i = el('b');
  i.style.width = `${pct}%`;
  b.append(i);
  return b;
}

export function mudaBarra(b, fracao) {
  const pct = Math.round(Math.min(1, Math.max(0, fracao || 0)) * 100);
  b.setAttribute('aria-valuenow', String(pct));
  b.firstElementChild.style.width = `${pct}%`;
}

// --- Cache das partidas (os 10 participantes mudam pouco; várias telas pedem a mesma) -----------

const partidas = new Map();

// partidaComNomes: {partida, porId: {pid: participante}, nomes: {pid: campeão}}.
export async function partidaComNomes(gameId) {
  if (!partidas.has(gameId)) {
    const p = api.partida(gameId).then(({ partida }) => {
      const porId = {};
      const nomes = {};
      for (const pt of partida.participantes) {
        porId[pt.participante_id] = pt;
        nomes[pt.participante_id] = pt.campeao;
      }
      return { partida, porId, nomes };
    });
    p.catch(() => partidas.delete(gameId));
    partidas.set(gameId, p);
  }
  return partidas.get(gameId);
}

// O último jogador buscado: as telas de partida mostram "do ponto de vista dele" quando ele jogou.
const CHAVE_JOGADOR = 'loltracking.jogador';
export function lembraJogador(puuid) {
  try {
    localStorage.setItem(CHAVE_JOGADOR, puuid);
  } catch {
    /* sem armazenamento: só perde a conveniência */
  }
}
export function ultimoJogador() {
  try {
    return localStorage.getItem(CHAVE_JOGADOR);
  } catch {
    return null;
  }
}

// Preferências pequenas desta máquina (velocidade padrão do rastreio, tema).
export function preferencia(chave, padrao) {
  try {
    const v = localStorage.getItem(`loltracking.${chave}`);
    return v === null ? padrao : v;
  } catch {
    return padrao;
  }
}
export function guardaPreferencia(chave, valor) {
  try {
    localStorage.setItem(`loltracking.${chave}`, String(valor));
  } catch {
    /* idem */
  }
}

// O participante "dono" da partida para os títulos: o último jogador buscado, se jogou; senão o
// primeiro rastreado; senão o 1.
export function participantePrincipal(partida, rastreados = []) {
  const puuid = ultimoJogador();
  const dele = partida.participantes.find((p) => p.puuid && p.puuid === puuid);
  if (dele) return dele;
  const r = partida.participantes.find((p) => rastreados.includes(p.participante_id));
  return r || partida.participantes[0];
}

// Tema: escuro por padrão (o do workflow); claro só se escolhido em Configurações. Não segue o
// Windows: com o sistema em modo claro o app abria claro, e o desenho dele é o escuro.
export const TEMA_PADRAO = 'escuro';
export function aplicaTema(tema) {
  document.documentElement.dataset.theme = tema === 'claro' ? 'light' : 'dark';
}
