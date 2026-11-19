// O QUADRO DO MAPA de uma situação do relatório da IA (ou de um instante da conversa): uma imagem
// parada do nosso mapa com play naquele instante, desenhada pelo app a partir dos dados — os 10
// (retrato, morto, posição aproximada), as sentinelas no chão, a etiqueta de atividade do jogador
// em foco, a visão do time dele (quem do outro time estava fora da visão) e a anotação da IA
// (realce, seta, região). A IA não gera imagem: só diz o instante e a anotação. Clique abre o mapa
// com play no instante. A montagem é pura (ia.js, montaQuadro); aqui só busca e desenha.

import { api, pede } from './api.js';
import { emPorcento, separa } from './mapa.js';
import { el, retrato } from './ui.js';
import { relogio, atividadeCurta, nomeZona } from './texto.js';
import { montaQuadro, celulasDaZona, rotaDoInstante } from './ia.js';

const SVG = 'http://www.w3.org/2000/svg';
const svg = (tag, attrs = {}) => {
  const e = document.createElementNS(SVG, tag);
  for (const [k, v] of Object.entries(attrs)) e.setAttribute(k, String(v));
  return e;
};

// Cache por partida do que não muda com o instante (uma busca por partida, não por quadro).
const porPartida = new Map();
let gradeZonas = null;

function dadosDaPartida(gameId) {
  if (!porPartida.has(gameId)) {
    const p = Promise.all([
      api.sentinelas(gameId).then((s) => s.sentinelas || []).catch(() => []),
      api.visao(gameId).then((v) => v.visao || null).catch(() => null),
    ]).then(([sentinelas, visao]) => ({ sentinelas, visao }));
    porPartida.set(gameId, p);
  }
  return porPartida.get(gameId);
}

// Grade grossa (250 unidades): a região da anotação vira algumas centenas de retângulos, não milhares.
function grade() {
  if (!gradeZonas) gradeZonas = pede('GET', '/api/mapa/zonas?celula=250').then((z) => z.grade).catch(() => null);
  return gradeZonas;
}

const VISAO = { fora: 'fora da visão do time', indeterminado: 'visão incerta', visivel: 'à vista do time', fora_do_mapa: 'fora do mapa (intocável)' };

// quadroDoMapa: devolve o elemento na hora (com "carregando") e preenche quando os dados chegam.
// `info` = partidaComNomes(gameId); `anotacao` = o quadro validado pelo servidor (ou null).
export function quadroDoMapa({ gameId, ms, foco, info, anotacao = null, legenda = '' }) {
  const fig = el('figure', 'quadro-ia');
  const ligacao = el('a', 'quadro-ia-mapa');
  ligacao.href = rotaDoInstante(gameId, ms);
  ligacao.title = 'abrir o mapa com play neste instante';
  const mapa = el('div', 'mapa-quadro');
  const img = el('img');
  img.src = 'assets/map11.png';
  img.alt = '';
  mapa.append(img, el('div', 'quadro-ia-carregando', 'carregando o quadro…'));
  ligacao.append(mapa);
  const cap = el('figcaption', '', [el('b', 'num', relogio(ms)), legenda ? el('span', '', ` · ${legenda}`) : null]);
  fig.append(ligacao, cap);
  desenha(mapa, cap, { gameId, ms, foco, info, anotacao }).catch(() => {
    mapa.querySelector('.quadro-ia-carregando')?.replaceChildren('sem dados deste instante');
  });
  return fig;
}

async function desenha(mapa, cap, { gameId, ms, foco, info, anotacao }) {
  const s = ms / 1000;
  const [{ quadro }, atv, partida, g] = await Promise.all([
    api.momento(gameId, s),
    api.atividades(gameId, Math.max(0, s - 1), s + 1).catch(() => ({ atividades: [] })),
    dadosDaPartida(gameId),
    grade(),
  ]);
  const timeFoco = info.porId[foco]?.time;
  const q = montaQuadro({ quadro, atividades: atv.atividades, sentinelas: partida.sentinelas, visao: partida.visao, foco, timeFoco, anotacao, grade: g, t: ms });
  mapa.querySelector('.quadro-ia-carregando')?.remove();
  const sobre = svg('svg', { class: 'quadro-ia-svg', viewBox: '0 0 100 100', preserveAspectRatio: 'none', 'aria-hidden': 'true' });
  const defs = svg('defs');
  const ponta = svg('marker', { id: `seta-${gameId}-${ms}`, viewBox: '0 0 10 10', refX: 8, refY: 5, markerWidth: 4, markerHeight: 4, orient: 'auto-start-reverse' });
  ponta.append(svg('path', { d: 'M0 0 L10 5 L0 10 z', class: 'quadro-ia-ponta' }));
  defs.append(ponta);
  sobre.append(defs);
  if (q.regiao) {
    for (const c of celulasDaZona(g, q.regiao)) {
      const a = emPorcento(c.x0, c.y1);
      const b = emPorcento(c.x1, c.y0);
      sobre.append(svg('rect', { x: a.left, y: a.top, width: b.left - a.left, height: b.top - a.top, class: 'quadro-ia-regiao' }));
    }
  }
  for (const w of q.sentinelas) {
    const p = emPorcento(w.x, w.y);
    sobre.append(svg('circle', { cx: p.left, cy: p.top, r: 1.1, class: `quadro-ia-sentinela ${w.time === 200 ? 'vermelho' : 'azul'}` }));
  }
  if (q.seta) {
    const a = emPorcento(q.seta.de.x, q.seta.de.y);
    const b = emPorcento(q.seta.para.x, q.seta.para.y);
    sobre.append(svg('line', { x1: a.left, y1: a.top, x2: b.left, y2: b.top, class: 'quadro-ia-seta', 'marker-end': `url(#seta-${gameId}-${ms})` }));
  }
  mapa.append(sobre);
  const descr = [];
  // Quem está no mesmo lugar (a luta) abre um pouco, como no retrato do abate: sem isso um retrato
  // some debaixo do outro.
  const lugares = separa(q.marcas.map((m) => {
    const p = emPorcento(m.x, m.y);
    return { x: p.left, y: p.top };
  }), 6, { maxDesvio: 5 });
  for (const [k, m] of q.marcas.entries()) {
    const pt = info.porId[m.pid];
    const p = { left: lugares[k].x, top: lugares[k].y };
    const classes = ['marca', m.aproximada ? 'aproximada' : 'exata', m.morto ? 'morto' : '', m.foco ? 'foco' : '', m.realce ? 'realce' : '', m.visao === 'fora' ? 'fora-da-visao' : ''];
    const marca = el('span', classes.filter(Boolean).join(' '));
    marca.style.left = `${p.left}%`;
    marca.style.top = `${p.top}%`;
    marca.append(retrato(pt, m.foco ? 26 : 22));
    if (m.morto) marca.append(el('i', 'x', '✕'));
    if (m.foco && m.atividade) marca.append(el('span', 'quadro-ia-rotulo', atividadeCurta(m.atividade, info.nomes)));
    marca.title = `${pt?.campeao || m.pid}${m.morto ? ' · morto' : ''}${m.aproximada ? ' · posição aproximada' : ''}${m.visao ? ` · ${VISAO[m.visao]}` : ''}`;
    mapa.append(marca);
    if (m.realce) descr.push(pt?.campeao);
  }
  mapa.setAttribute('role', 'img');
  mapa.setAttribute('aria-label', `Mapa aos ${relogio(ms)}${descr.length ? `, destaque em ${descr.join(', ')}` : ''}${q.regiao ? `, região ${nomeZona(q.regiao)}` : ''}`);
  const chave = [];
  if (q.temVisao) chave.push('apagado = fora da visão do time');
  if (q.sentinelas.length) chave.push(`${q.sentinelas.length} sentinela(s) no chão (posição estimada)`);
  if (q.regiao) chave.push(nomeZona(q.regiao));
  if (chave.length) cap.append(el('span', 'sub quadro-ia-chave', ` (${chave.join('; ')})`));
}
