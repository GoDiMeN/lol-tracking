// Partida rastreada: cabeçalho, seletor de jogador, o mapa no momento de cada abate com os 10 e
// "onde cada um estava", a vida ao longo da partida (mortes e saídas de rota), a lista de saídas de
// rota, a linha do tempo da cadeia reduzida, os indicadores e a conferência do CS.

import { api, imagemItem } from '../api.js';
import { duracao, dataCurta, posicao } from '../formato.js';
import { emPorcento, separa } from '../mapa.js';
import {
  relogio,
  numero,
  nomeZona,
  valorIndicador,
  itemCadeia,
  GRUPOS_CADEIA,
  grupoDoTipo,
  desfecho,
  fimDaSaida,
  resumoDano,
  superioridade,
  origemPosicao,
} from '../texto.js';
import {
  el,
  botao,
  link,
  pilula,
  cartao,
  retrato,
  carregando,
  vazio,
  erroNaTela,
  partidaComNomes,
  participantePrincipal,
  toast,
} from '../ui.js';

const SVG = 'http://www.w3.org/2000/svg';
const svg = (tag, attrs = {}) => {
  const e = document.createElementNS(SVG, tag);
  for (const [k, v] of Object.entries(attrs)) e.setAttribute(k, String(v));
  return e;
};

// Os grupos da linha do tempo ligados de começo (os outros enchem a lista).
const GRUPOS_PADRAO = new Set(['lutas', 'rota', 'vida', 'base', 'itens']);
const gruposLigados = new Set(GRUPOS_PADRAO);
let soDoJogador = true;

export async function desenha(raiz, rota, ctx) {
  const gameId = rota.gameId;
  raiz.append(carregando('Carregando a partida…'));
  let info;
  let analise;
  let abates;
  let rastreadas;
  try {
    [info, { analise }, { abates }, { partidas: rastreadas }] = await Promise.all([
      partidaComNomes(gameId),
      api.analise(gameId),
      api.abates(gameId),
      api.rastreadas(),
    ]);
  } catch (err) {
    raiz.replaceChildren(
      el('h1', 'titulo-tela', 'Partida'),
      erroNaTela(err, err.status === 404 ? 'Essa partida não está guardada neste computador. Abra pela lista de Partidas rastreadas.' : undefined),
    );
    return;
  }
  const p = info.partida;
  const rastreados = rastreadas.find((x) => x.game_id === gameId)?.participantes || analise?.rastreados || [];
  let pid = rota.participante;
  if (!pid || !info.porId[pid]) {
    const dono = participantePrincipal(p, rastreados);
    pid = rastreados.includes(dono.participante_id) ? dono.participante_id : rastreados[0] || dono.participante_id;
    ctx.marca({ vista: 'partida', gameId, participante: pid, abate: rota.abate });
  }
  const jog = info.porId[pid];
  const ehRastreado = rastreados.includes(pid);

  // --- Cabeçalho ---
  const cab = el('div', 'linha cab-tela');
  cab.append(
    link('← Partidas rastreadas', '#partidas', 'voltar'),
    el('h1', 'titulo-tela inline', `${jog.campeao}${jog.posicao ? ` · ${posicao(jog.posicao)}` : ''} · ${jog.venceu ? 'vitória' : 'derrota'} · ${duracao(p.duracao_s)}`),
    pilula(`${rastreados.length} de 10 rastreados`, rastreados.length === 10 ? 'ok' : 'ac', 'quantos jogadores tiveram a partida assistida pelo app'),
  );
  if (analise?.riot) cab.lastChild.after(pilula('com dados da Riot', 'n', 'a linha do tempo da Riot trouxe o dano de cada abate'));
  const acoes = el('div', 'linha acoes-dir', [
    el('span', 'sub', `${dataCurta(p.criada_em)} · ${p.fila_nome}`),
    link('Mapa com play', `#replay/${gameId}`, 'btn pri'),
  ]);
  const refazer = botao('Refazer análise', '', async () => {
    refazer.disabled = true;
    refazer.textContent = 'Refazendo…';
    try {
      await api.reanalisa(gameId);
      toast('Análise refeita.', 'ok');
      ctx.navega({ vista: 'partida', gameId, participante: pid }, { substitui: true });
    } catch (err) {
      toast(err.status === 409 ? 'Falta a linha do tempo desta partida: baixe o replay de novo em Jogadores.' : err.message, 'erro');
      refazer.disabled = false;
      refazer.textContent = 'Refazer análise';
    }
  });
  acoes.append(refazer);
  cab.append(acoes);

  // --- Seletor de jogador ---
  const seletor = el('div', 'seletor');
  seletor.setAttribute('role', 'group');
  seletor.setAttribute('aria-label', 'Jogador');
  for (const time of [100, 200]) {
    const g = el('div', `seletor-time ${time === 100 ? 'azul' : 'vermelho'}`);
    for (const pt of p.participantes.filter((x) => x.time === time)) {
      const b = el('button', `chip-jogador${pt.participante_id === pid ? ' on' : ''}${rastreados.includes(pt.participante_id) ? ' rastreado' : ''}`);
      b.type = 'button';
      b.setAttribute('aria-pressed', String(pt.participante_id === pid));
      b.title = `${pt.campeao} · ${pt.nome}${rastreados.includes(pt.participante_id) ? ' (rastreado)' : ' (não rastreado: só os dados por minuto)'}`;
      b.append(retrato(pt, 26), el('span', '', pt.campeao));
      b.addEventListener('click', () => ctx.navega({ vista: 'partida', gameId, participante: pt.participante_id, abate: rota.abate }));
      g.append(b);
    }
    seletor.append(g);
  }

  const partes = [cab, seletor];
  if (analise?.avisos?.length) partes.push(el('ul', 'avisos', analise.avisos.map((a) => el('li', 'at', a))));
  if (!analise) {
    partes.push(
      vazio(
        'Esta partida ainda não foi analisada (a análise roda sozinha quando um jogador termina de ser rastreado).',
        botao('Analisar agora', 'pri', () => refazer.click()),
      ),
    );
    raiz.replaceChildren(...partes);
    return;
  }

  // --- Abates: mapa + onde cada um estava ---
  const secAbates = el('div', 'grade-abate');
  partes.push(secAbates);

  // --- Vida, saídas, cadeia, indicadores, CS: por jogador ---
  const secVida = el('div');
  const secSaidas = el('div');
  const secCadeia = el('div');
  const secInd = el('div');
  const secCS = el('div');
  partes.push(secVida, el('div', 'grade-2', [secSaidas, secCS]), secCadeia, secInd);
  raiz.replaceChildren(...partes);

  desenhaAbates(secAbates, { gameId, info, abates, pid, rota, ctx, rastreados });

  if (!ehRastreado) {
    secVida.append(
      cartao(
        `${jog.campeao} · vida ao longo da partida`,
        null,
        el('div', 'estado-tela vazio', 'Este jogador não foi rastreado: sem vida, saídas de rota nem habilidades. Os indicadores abaixo vêm da linha do tempo (por minuto).'),
      ),
    );
  }

  const [ind, saidas, amostras, cadeia, cs] = await Promise.allSettled([
    api.indicadores(gameId, pid),
    ehRastreado ? api.saidas(gameId, pid) : Promise.resolve({ saidas: [] }),
    ehRastreado ? api.amostras(gameId, pid, 0, p.duracao_s, 1) : Promise.resolve(null),
    ehRastreado ? api.cadeia(gameId, pid) : Promise.resolve(null),
    api.cs(gameId, pid),
  ]);

  const nomes = info.nomes;
  if (ehRastreado) {
    if (amostras.status === 'fulfilled') {
      const mortes = cadeia.status === 'fulfilled' ? cadeia.value.itens.filter((i) => i.tipo === 'morte') : [];
      secVida.append(graficoVida(jog, amostras.value.amostras, mortes, saidas.status === 'fulfilled' ? saidas.value.saidas : [], gameId, p.duracao_s));
    } else {
      secVida.append(cartao('Vida ao longo da partida', null, erroNaTela(amostras.reason)));
    }
    secSaidas.append(
      saidas.status === 'fulfilled' ? listaSaidas(saidas.value.saidas, gameId, jog) : cartao('Saídas de rota', null, erroNaTela(saidas.reason)),
    );
    secCadeia.append(cadeia.status === 'fulfilled' ? linhaDoTempo(cadeia.value.itens, nomes, gameId) : cartao('Linha do tempo', null, erroNaTela(cadeia.reason)));
  } else {
    secSaidas.append(cartao('Saídas de rota', null, el('div', 'estado-tela vazio', 'Só para jogadores rastreados.')));
  }
  secInd.append(ind.status === 'fulfilled' ? indicadores(ind.value.indicadores, gameId) : cartao('Indicadores', null, erroNaTela(ind.reason)));
  secCS.append(cs.status === 'fulfilled' ? conferenciaCS(cs.value.jogadores.find((j) => j.participante_id === pid)) : cartao('CS conferido', null, erroNaTela(cs.reason)));
}

// --- Abates ------------------------------------------------------------------------------------

function desenhaAbates(sec, { gameId, info, abates, pid, rota, ctx, rastreados }) {
  if (!abates.length) {
    sec.replaceChildren(cartao('Mapa no momento dos abates', null, el('div', 'estado-tela vazio', 'Nenhum abate nesta partida (ou a análise não achou a linha do tempo).')));
    return;
  }
  const doJogador = (a) => a.vitima === pid || a.matador === pid || (a.assistencias || []).includes(pid);
  const lista = () => (soDoJogador ? abates.filter(doJogador) : abates);
  let atual = abates.find((a) => a.n === rota.abate) || lista()[0] || abates[0];

  const mapaCartao = el('section', 'cartao');
  const painel = el('section', 'cartao');
  sec.replaceChildren(mapaCartao, painel);

  function escolhe(a) {
    atual = a;
    ctx.marca({ vista: 'partida', gameId, participante: pid, abate: a.n });
    desenha();
  }

  function desenha() {
    const l = lista();
    const i = l.findIndex((a) => a.n === atual.n);
    const ant = botao('←', 'icone', () => escolhe(l[i - 1]), 'abate anterior');
    ant.disabled = i <= 0;
    ant.setAttribute('aria-label', 'Abate anterior');
    const prox = botao('→', 'icone', () => escolhe(l[i + 1]), 'próximo abate');
    prox.disabled = i < 0 || i >= l.length - 1;
    prox.setAttribute('aria-label', 'Próximo abate');
    const sel = el('select', 'campo seletor-abate');
    sel.setAttribute('aria-label', 'Escolher o abate');
    for (const a of l) {
      const o = el('option', '', `${relogio(a.t_ms)} · ${info.nomes[a.matador] || '—'} abateu ${info.nomes[a.vitima] || '—'}`);
      o.value = String(a.n);
      o.selected = a.n === atual.n;
      sel.append(o);
    }
    if (i < 0) {
      const o = el('option', '', `${relogio(atual.t_ms)} · ${info.nomes[atual.matador] || '—'} abateu ${info.nomes[atual.vitima] || '—'}`);
      o.value = String(atual.n);
      o.selected = true;
      sel.prepend(o);
    }
    sel.addEventListener('change', () => escolhe(abates.find((a) => a.n === Number(sel.value))));
    const filtro = el('label', 'check');
    const cb = el('input');
    cb.type = 'checkbox';
    cb.checked = soDoJogador;
    cb.addEventListener('change', () => {
      soDoJogador = cb.checked;
      const nl = lista();
      if (nl.length && !nl.some((a) => a.n === atual.n)) atual = nl[0];
      desenha();
    });
    filtro.append(cb, el('span', '', `só os de ${info.porId[pid].campeao}`));

    const cab = el('div', 'cab', [
      el('h2', '', `Mapa no momento ${relogio(atual.t_ms)}`),
      el('span', 'dir', `abate ${atual.n} de ${abates.length}`),
    ]);
    const nav = el('div', 'linha nav-abate', [ant, sel, prox, filtro]);
    mapaCartao.replaceChildren(cab, nav, mapaDoAbate(atual, info), legendaMapa(), link('Ver no mapa com play', `#replay/${gameId}/${Math.max(0, Math.round(atual.t_ms / 1000) - 8)}`, 'btn mini'));
    painel.replaceChildren(...painelAbate(atual, info, rastreados));
  }
  desenha();
}

function mapaDoAbate(a, info) {
  const m = el('div', 'mapa-quadro');
  const img = el('img');
  img.src = 'assets/map11.png';
  img.alt = '';
  m.append(img);
  m.setAttribute('role', 'img');
  const vit = info.porId[a.vitima];
  const mat = info.porId[a.matador];
  m.setAttribute('aria-label', `Mapa com os dez jogadores quando ${mat?.campeao || 'alguém'} abateu ${vit?.campeao || 'alguém'}, em ${nomeZona(a.zona)}`);
  // A vítima por último: fica por cima.
  const jogs = [...a.jogadores].sort((x, y) => (x.papel === 'vitima') - (y.papel === 'vitima') || (x.papel === 'matador') - (y.papel === 'matador'));
  // Quem está no mesmo lugar (a luta) abre um pouco para não sumir um debaixo do outro.
  const comPos = jogs.filter((j) => j.x !== null && j.y !== null && j.x !== undefined);
  const lugar = separa(comPos.map((j) => {
    const p = emPorcento(j.x, j.y);
    return { x: p.left, y: p.top };
  }), 6, { maxDesvio: 5 });
  for (const [k, j] of comPos.entries()) {
    const pt = info.porId[j.participante_id];
    const { x: left, y: top } = lugar[k];
    const marca = el('span', `marca ${j.aproximada ? 'aproximada' : 'exata'} ${j.papel || ''} ${j.vivo === false && j.papel !== 'vitima' ? 'morto' : ''}`);
    marca.style.left = `${left}%`;
    marca.style.top = `${top}%`;
    marca.append(retrato(pt, 26));
    if (j.papel === 'vitima') marca.append(el('i', 'x', '✕'));
    marca.title = `${pt.campeao} · ${pt.nome}${j.papel ? ` · ${papel(j.papel)}` : ''} · ${origemPosicao(j.origem)}`;
    m.append(marca);
  }
  return m;
}

function legendaMapa() {
  return el('div', 'chave', [
    el('span', '', [el('i', 'k-exata'), 'posição rastreada']),
    el('span', '', [el('i', 'k-aprox'), 'aproximada (por minuto)']),
    el('span', '', [el('i', 'k-vitima', '✕'), 'quem morreu']),
  ]);
}

const papel = (p) => ({ vitima: 'morreu', matador: 'matou', assistencia: 'ajudou' })[p] || '';

function painelAbate(a, info, rastreados) {
  const cab = el('div', 'cab', [el('h2', '', 'Onde cada um estava'), el('span', 'dir', relogio(a.t_ms))]);
  const vit = info.porId[a.vitima];
  const resumo = el('div', 'resumo-abate');
  const linha1 = `${info.nomes[a.matador] || 'alguém'} abateu ${vit?.campeao || 'alguém'} em ${nomeZona(a.zona)}`;
  resumo.append(el('b', '', linha1), el('span', 'sub', superioridade(a)));
  if (a.aliado_mais_proximo) resumo.append(el('span', 'sub', `aliado mais perto da vítima a ${numero(Math.round(a.aliado_mais_proximo))} unidades`));
  const vitJ = a.jogadores.find((j) => j.papel === 'vitima');
  const matJ = a.jogadores.find((j) => j.papel === 'matador');
  if (vitJ && matJ && !matJ.aproximada && matJ.dist_vitima > 900) {
    // Dano à distância é normal (habilidade de longo alcance, dano atrasado, salto que volta).
    resumo.append(el('span', 'sub', `${info.nomes[a.matador]} estava a ${numero(Math.round(matJ.dist_vitima))} da vítima: dano à distância${a.riot ? ' (veja abaixo de onde veio)' : ''}.`));
  }

  const kv = el('div', 'onde');
  const ordem = [...a.jogadores].sort((x, y) => {
    const peso = (j) => (j.papel === 'vitima' ? 0 : j.papel === 'matador' ? 1 : j.papel === 'assistencia' ? 2 : 3);
    return peso(x) - peso(y) || x.time - y.time || x.participante_id - y.participante_id;
  });
  for (const j of ordem) {
    const pt = info.porId[j.participante_id];
    const nome = el('span', `quem ${j.time === 200 ? 'tm-v' : 'tm-a'}`, [retrato(pt, 20), el('span', '', `${pt.campeao}${j.papel ? ` · ${papel(j.papel)}` : ''}`)]);
    const partes = [];
    if (j.vivo === false && j.papel !== 'vitima') partes.push('morto');
    else {
      if (j.zona) partes.push(nomeZona(j.zona));
      if (j.vida !== null && j.vida !== undefined && j.vida_max) partes.push(`${Math.round((j.vida / j.vida_max) * 100)}% de vida`);
      else if (j.vida_pct !== null && j.vida_pct !== undefined) partes.push(`${Math.round(j.vida_pct)}% de vida`);
    }
    if (j.rastreado) {
      if (j.r_aprendido === false) partes.push('sem R');
      else if (j.r_disponivel === true) partes.push('R pronto');
      else if (j.r_disponivel === false) partes.push(`R em ${Math.round(j.cd?.r ?? 0)} s`);
      if (j.passiva_disponivel === false) partes.push(`passiva em ${Math.round(j.cd?.p ?? 0)} s`);
      if (j.flash_disponivel === true) partes.push('Flash pronto');
      else if (j.flash_disponivel === false) partes.push('sem Flash');
      if (j.fora_da_rota_ms) partes.push(`fora da rota há ${Math.round(j.fora_da_rota_ms / 1000)} s`);
    }
    if (j.aproximada) partes.push('posição aproximada');
    const valor = el('b', j.aproximada ? 'aprox' : '', partes.join(' · ') || '—');
    kv.append(nome, valor);
  }
  const blocos = [cab, resumo, kv];
  if (a.riot) {
    const dano = resumoDano(a.riot.recebido, info.nomes);
    if (dano.length) {
      const l = el('div', 'dano');
      l.append(el('h3', '', `Quem bateu em ${vit?.campeao || 'quem morreu'} (Riot)`));
      for (const d of dano) {
        const pt = info.porId[d.participante_id];
        l.append(
          el('div', 'dano-linha', [
            el('span', `quem ${pt?.time === 200 ? 'tm-v' : pt ? 'tm-a' : ''}`, d.nome),
            el('span', 'num', numero(d.total)),
            el('span', 'sub', d.partes),
          ]),
        );
      }
      blocos.push(l);
    }
  }
  if (!rastreados.length) blocos.push(el('p', 'sub', 'Ninguém foi rastreado: as posições são as de minuto em minuto.'));
  return blocos;
}

// --- Vida ao longo da partida -------------------------------------------------------------------

function graficoVida(jog, amostras, mortes, saidas, gameId) {
  const comVida = amostras.filter((a) => a.vida !== null && a.vida_max);
  const titulo = `${jog.campeao} · vida ao longo da partida`;
  if (!amostras.length) return cartao(titulo, null, el('div', 'estado-tela vazio', 'Sem leituras gravadas para este jogador.'));
  const t0 = 0;
  const t1 = Math.max(amostras.at(-1).t_ms, ...saidas.map((s) => s.dados?.fim_ms || 0));
  const L = 1000;
  const A = 150;
  const topo = 8;
  const base = 120;
  const x = (t) => ((t - t0) / Math.max(1, t1 - t0)) * L;
  const y = (pct) => base - (pct / 100) * (base - topo);
  const g = svg('svg', { viewBox: `0 0 ${L} ${A}`, role: 'img', class: 'grafico', preserveAspectRatio: 'none' });
  g.setAttribute('aria-label', `Vida de ${jog.campeao} de ${relogio(t0)} a ${relogio(t1)}, com ${mortes.length} mortes e ${saidas.length} saídas de rota`);

  for (const s of saidas.filter((s) => s.tipo === 'saiu_da_rota')) {
    const fim = s.dados?.fim_ms || s.t_ms;
    const r = svg('rect', { x: x(s.t_ms), y: topo, width: Math.max(2, x(fim) - x(s.t_ms)), height: base - topo, class: 'faixa-saida' });
    const tt = svg('title');
    tt.textContent = `saiu da rota às ${relogio(s.t_ms)} por ${Math.round((fim - s.t_ms) / 1000)} s · ${desfecho(s.dados?.desfecho)}`;
    r.append(tt);
    g.append(r);
  }
  for (const pct of [50, 100]) g.append(svg('line', { x1: 0, x2: L, y1: y(pct), y2: y(pct), class: 'grade-linha' }));
  g.append(svg('line', { x1: 0, x2: L, y1: base, y2: base, class: 'eixo' }));

  // A linha quebra onde não há leitura (morto, painel não lido) por mais de 3 s.
  let d = '';
  let ant = null;
  for (const a of comVida) {
    const px = x(a.t_ms).toFixed(1);
    const py = y((a.vida / a.vida_max) * 100).toFixed(1);
    d += !ant || a.t_ms - ant.t_ms > 3000 ? `M${px} ${py}` : `L${px} ${py}`;
    ant = a;
  }
  g.append(svg('path', { d, class: 'linha-vida' }));

  for (const m of mortes) {
    const c = svg('circle', { cx: x(m.t_ms), cy: base, r: 5, class: 'morte' });
    const tt = svg('title');
    tt.textContent = `morreu às ${relogio(m.t_ms)}`;
    c.append(tt);
    g.append(c);
  }
  const passo = t1 > 1200000 ? 300000 : t1 > 600000 ? 180000 : 60000;
  for (let t = 0; t <= t1; t += passo) {
    const tx = svg('text', { x: Math.min(L - 30, x(t) + 2), y: A - 6, class: 'rotulo-eixo' });
    tx.textContent = relogio(t);
    g.append(tx);
  }
  const lado = el('div', 'linha legenda-grafico', [
    el('span', '', [el('i', 'k-linha'), 'vida (painel do jogo)']),
    el('span', '', [el('i', 'k-faixa'), 'fora da rota']),
    el('span', '', [el('i', 'k-morte'), 'morte']),
    comVida.length < amostras.length * 0.5 ? el('span', 'sub', 'a vida só aparece onde a tela foi lida') : null,
  ]);
  const c = cartao(titulo, `${relogio(t0)}–${relogio(t1)}`, [el('div', 'grafico-caixa', [g]), lado]);
  // Clique no gráfico: abre o mapa com play naquele instante.
  g.addEventListener('click', (ev) => {
    const r = g.getBoundingClientRect();
    const t = t0 + ((ev.clientX - r.left) / r.width) * (t1 - t0);
    location.hash = `#replay/${gameId}/${Math.max(0, Math.round(t / 1000))}`;
  });
  g.style.cursor = 'pointer';
  return c;
}

// --- Saídas de rota -----------------------------------------------------------------------------

function listaSaidas(saidas, gameId, jog) {
  const so = saidas.filter((s) => s.tipo === 'saiu_da_rota' || s.tipo === 'entrou_em_rota');
  if (!so.length) {
    return cartao('Saídas de rota', null, el('div', 'estado-tela vazio', `${jog.campeao} não saiu da rota no trecho rastreado (ou é o caçador e não visitou rotas).`));
  }
  const tab = el('table', 'tabela');
  tab.append(
    el('thead', '', [el('tr', '', ['Quando', 'Por', 'Para onde', 'Vida', 'Como terminou', ''].map((h) => el('th', '', h)))]),
  );
  const tb = el('tbody');
  for (const s of so) {
    const d = s.dados || {};
    const visita = s.tipo === 'entrou_em_rota';
    const tr = el('tr', d.conta === false ? 'apagada' : '');
    tr.append(
      el('td', 'num', relogio(s.t_ms)),
      el('td', 'num', `${Math.round((d.duracao_ms || 0) / 1000)} s`),
      el('td', '', visita ? `visitou a ${nomeZona(d.rota)}` : nomeZona(d.para) + (d.zonas?.length > 1 ? ` e mais ${d.zonas.length - 1}` : '')),
      el('td', 'num', d.vida_pct !== null && d.vida_pct !== undefined ? `${Math.round(d.vida_pct)}%` : '—'),
      el('td', d.desfecho === 'morte' ? 'tx-cr' : '', `${desfecho(d.desfecho)}${d.fim_motivo && d.fim_motivo !== 'voltou' && !(d.fim_motivo === 'morreu' && d.desfecho === 'morte') ? ` · ${fimDaSaida(d.fim_motivo)}` : ''}`),
      el('td', '', [link('ver', `#replay/${gameId}/${Math.max(0, Math.round(s.t_ms / 1000) - 3)}`, 'btn mini')]),
    );
    if (d.conta === false) tr.title = 'foi andando para a base sem nada acontecer: não conta como saída para jogar';
    tb.append(tr);
  }
  tab.append(tb);
  const n = so.filter((s) => s.dados?.conta !== false).length;
  return cartao('Saídas de rota', `${n} que contam`, el('div', 'rolar', [tab]));
}

// --- Linha do tempo da cadeia -------------------------------------------------------------------

function linhaDoTempo(itens, nomes, gameId) {
  const corpo = el('div');
  const filtros = el('div', 'linha filtros');
  filtros.setAttribute('role', 'group');
  filtros.setAttribute('aria-label', 'O que mostrar na linha do tempo');
  const contagem = {};
  for (const it of itens) {
    const g = grupoDoTipo(it.tipo);
    contagem[g] = (contagem[g] || 0) + 1;
  }
  for (const g of GRUPOS_CADEIA) {
    if (!contagem[g.id]) continue;
    const b = el('button', `filtro${gruposLigados.has(g.id) ? ' on' : ''}`, `${g.rotulo} ${contagem[g.id]}`);
    b.type = 'button';
    b.setAttribute('aria-pressed', String(gruposLigados.has(g.id)));
    b.addEventListener('click', () => {
      if (gruposLigados.has(g.id)) gruposLigados.delete(g.id);
      else gruposLigados.add(g.id);
      b.classList.toggle('on');
      b.setAttribute('aria-pressed', String(gruposLigados.has(g.id)));
      lista();
    });
    filtros.append(b);
  }
  const ol = el('ol', 'cadeia');
  function lista() {
    const vis = itens.filter((it) => gruposLigados.has(grupoDoTipo(it.tipo)));
    if (!vis.length) {
      ol.replaceChildren(el('li', 'sub', 'Nada com esses filtros.'));
      return;
    }
    ol.replaceChildren(
      ...vis.slice(0, 600).map((it) => {
        const li = el('li', `item-cadeia g-${grupoDoTipo(it.tipo)} t-${it.tipo}`);
        const t = link(relogio(it.t_ms), `#replay/${gameId}/${Math.max(0, Math.round(it.t_ms / 1000) - 2)}`, 'num tempo');
        t.title = 'ver no mapa com play';
        li.append(t, el('span', 'marca-tipo'), el('span', '', itemCadeia(it, nomes)));
        if ((it.tipo === 'compra' || it.tipo === 'venda') && it.dados?.item) {
          const img = el('img', 'item-icone');
          img.src = imagemItem(it.dados.item);
          img.alt = `item ${it.dados.item}`;
          img.title = `item ${it.dados.item}`;
          img.addEventListener('error', () => img.replaceWith(el('span', 'sub', `item ${it.dados.item}`)));
          li.append(img);
        }
        return li;
      }),
    );
    if (vis.length > 600) ol.append(el('li', 'sub', `e mais ${vis.length - 600}…`));
  }
  lista();
  corpo.append(filtros, ol);
  return cartao('Linha do tempo', 'o que o rastreio guardou, em ordem', corpo);
}

// --- Indicadores --------------------------------------------------------------------------------

function indicadores(lista, gameId) {
  const comValor = lista.filter((i) => i.valor !== null);
  if (!comValor.length) return cartao('Indicadores', null, el('div', 'estado-tela vazio', 'Sem indicadores para este jogador.'));
  const grupos = [
    { rotulo: 'Rota e mortes', f: (i) => /saidas|mortes|visitas|cs_perdido|abates|assist|participacao|dist_aliado|recalls|habilidades|tempo_/.test(i.indicador) && !i.indicador.startsWith('pct_') },
    { rotulo: 'Ouro, CS e XP', f: (i) => /^(cs_|ouro_|xp_|dif_|dano_|wards)/.test(i.indicador) },
    { rotulo: 'Onde passou o tempo', f: (i) => i.indicador.startsWith('pct_') && !i.indicador.startsWith('pct_atv_') },
    { rotulo: 'O que fazia', f: (i) => i.indicador.startsWith('pct_atv_') },
  ];
  const usados = new Set();
  const corpo = el('div', 'grade-ind');
  for (const g of grupos) {
    const itens = comValor.filter((i) => !usados.has(i.indicador) && g.f(i));
    if (!itens.length) continue;
    for (const i of itens) usados.add(i.indicador);
    const bloco = el('div', 'bloco-ind', [el('h3', '', g.rotulo)]);
    const dl = el('div', 'kv');
    for (const i of itens) {
      const rot = el('span', '', i.rotulo);
      if (i.aproximado) rot.append(el('small', 'aprox-marca', ' aprox.'));
      const v = el('b', '', valorIndicador(i.valor, i.unidade));
      dl.append(rot, v);
      const mom = i.detalhe?.momentos;
      if (mom?.length) {
        const m = el(
          'span',
          'momentos span2',
          mom.slice(0, 6).map((x) => link(x.texto, `#replay/${gameId}/${Math.max(0, Math.round(x.t_ms / 1000) - 5)}`)),
        );
        dl.append(m);
      }
    }
    bloco.append(dl);
    corpo.append(bloco);
  }
  return cartao('Indicadores', '“aprox.” = rastreio parcial ou posição aproximada', corpo);
}

// --- CS -----------------------------------------------------------------------------------------

function conferenciaCS(j) {
  if (!j || !j.minutos.length) {
    return cartao('CS conferido', null, el('div', 'estado-tela vazio', 'Sem estado gravado para conferir o CS deste jogador.'));
  }
  const tab = el('table', 'tabela compacta');
  tab.append(el('thead', '', [el('tr', '', ['Min', 'Cliente', 'Jogo', 'Dif.'].map((h) => el('th', '', h)))]));
  const tb = el('tbody');
  for (const m of j.minutos) {
    tb.append(
      el('tr', m.divergente ? 'divergente' : '', [
        el('td', 'num', String(m.minuto)),
        el('td', 'num', String(m.cs_lcu)),
        el('td', 'num', m.cs_jogo === null ? '—' : String(m.cs_jogo)),
        el('td', 'num', m.diferenca === null ? '—' : String(m.diferenca)),
      ]),
    );
  }
  tab.append(tb);
  const dir = j.divergencias ? `${j.divergencias} minuto${j.divergencias > 1 ? 's' : ''} fora` : 'tudo confere';
  return cartao('CS conferido', dir, [
    el('p', 'sub pad', 'O CS do jogo anda de 10 em 10; diferença de até 10 (mais 3 de folga) é normal.'),
    el('div', 'rolar curto', [tab]),
  ]);
}
