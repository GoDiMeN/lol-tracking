// Mapa com play (tela cheia): Summoner's Rift com os 10 como o retrato do campeão (borda da cor do
// time) e o nome do jogador, andando conforme os dados; play/pausa, velocidade, barra de tempo com
// os abates e objetivos, atividade de cada um e eventos surgindo no mapa. Clicar num jogador abre
// o painel com vida, mana, recargas, atributos, itens, nível, K/D/A, CS e zona naquele instante.
//
// Os dados vêm em janelas de 60 s (replay-dados.js) com pré-busca da seguinte; entre dois quadros
// do servidor (0,5 s) a posição é interpolada, salvo salto (teleporte: recall, W da LeBlanc...).
// Teclas: espaço play/pausa, ← → ±5 s, F tela cheia, Esc fecha o painel / sai.

import { api, imagemItem } from '../api.js';
import { duracao } from '../formato.js';
import { emPorcento, paraTela, zonaNaGrade, separa } from '../mapa.js';
import { Janelas, quadroNoInstante, atividadeNoInstante, eventosRecentes, achaIndice, PASSO_S } from '../replay-dados.js';
import { relogio, numero, nomeZona, recarga, atividadeTexto, nomeFeitico, eventoTexto, origemPosicao } from '../texto.js';
import { el, botao, link, retrato, partidaComNomes, participantePrincipal, erroNaTela } from '../ui.js';

const VELOCIDADES = [1, 2, 4, 8];
const EVENTO_MS = 4000; // quanto tempo (de jogo) um abate/objetivo fica no mapa
const HABILIDADE_MS = 1500;

export async function desenha(raiz, rota, ctx) {
  const gameId = rota.gameId;
  raiz.append(el('div', 'estado-tela carregando', 'Abrindo o mapa…'));
  let info;
  let eventos;
  let zonas;
  let rastreadas;
  try {
    [info, { eventos }, zonas, { partidas: rastreadas }] = await Promise.all([
      partidaComNomes(gameId),
      api.eventos(gameId),
      api.zonas(),
      api.rastreadas(),
    ]);
  } catch (err) {
    raiz.replaceChildren(el('div', 'replay-erro', [erroNaTela(err), link('← Voltar', `#partida/${gameId}`, 'btn')]));
    return;
  }
  const p = info.partida;
  const duracaoMs = p.duracao_s * 1000;
  const rastreados = rastreadas.find((x) => x.game_id === gameId)?.participantes || [];
  const dono = participantePrincipal(p, rastreados);
  const grade = zonas.grade;
  const nomes = info.nomes;

  // Habilidades usadas (só dos rastreados) viram balões nos retratos.
  const habilidades = [];
  await Promise.all(
    rastreados.map(async (pid) => {
      try {
        const { itens } = await api.cadeia(gameId, pid, 'habilidade_usada');
        for (const it of itens) habilidades.push({ ...it, participante_id: pid });
      } catch {
        /* sem cadeia: sem os balões */
      }
    }),
  );
  habilidades.sort((a, b) => a.t_ms - b.t_ms);
  const evMapa = eventos.filter((e) => ['abate', 'monstro', 'estrutura'].includes(e.tipo) && e.x !== null && e.x !== undefined);

  // --- Estrutura da tela ---
  const sair = link('← Voltar', `#partida/${gameId}/${rastreados.includes(dono.participante_id) ? dono.participante_id : rastreados[0] || ''}`, 'btn');
  const titulo = el('div', 'rp-titulo', [
    el('b', '', `${dono.campeao} · ${dono.venceu ? 'vitória' : 'derrota'} · ${duracao(p.duracao_s)}`),
    el('span', 'sub', 'mapa com play'),
  ]);
  const cbZonas = el('input');
  cbZonas.type = 'checkbox';
  const zonasLab = el('label', 'check', [cbZonas, el('span', '', 'zonas')]);
  const ajuda = el('span', 'sub rp-teclas', 'espaço play/pausa · ← → 5 s · F tela cheia · Esc sai');
  const topo = el('header', 'rp-topo', [sair, titulo, zonasLab, ajuda]);

  const palco = el('div', 'rp-palco');
  const mapa = el('div', 'rp-mapa');
  const img = el('img', 'rp-fundo');
  img.src = 'assets/map11.png';
  img.alt = "Mapa de Summoner's Rift";
  const canvas = el('canvas', 'rp-zonas');
  canvas.hidden = true;
  const camadaEv = el('div', 'rp-eventos');
  const camadaJog = el('div', 'rp-jogadores');
  const aviso = el('div', 'rp-aviso');
  aviso.hidden = true;
  // Os eventos ficam por cima das figuras: o ✕ do abate cai justo onde está a vítima.
  mapa.append(img, canvas, camadaJog, camadaEv, aviso);
  palco.append(mapa);

  const legenda = el('div', 'rp-legenda', [
    el('span', '', [el('i', 'k-exata'), 'rastreado']),
    el('span', '', [el('i', 'k-aprox'), 'posição aproximada (por minuto)']),
    el('span', '', [el('i', 'k-morto'), 'morto']),
  ]);
  if (!rastreados.length) legenda.append(el('span', 'sub', 'ninguém rastreado nesta partida: todos aproximados'));

  const painel = el('aside', 'rp-painel');
  painel.hidden = true;
  painel.setAttribute('aria-live', 'polite');

  const play = botao('▶', 'rp-play', () => alterna());
  play.setAttribute('aria-label', 'Tocar');
  const relogioEl = el('span', 'num rp-relogio', '0:00');
  const total = el('span', 'num sub', `/ ${duracao(p.duracao_s)}`);
  const trilha = el('div', 'rp-trilha');
  const marcas = el('div', 'rp-marcas');
  const barra = el('input', 'rp-barra');
  barra.type = 'range';
  barra.min = '0';
  barra.max = String(p.duracao_s);
  barra.step = '0.1';
  barra.setAttribute('aria-label', 'Tempo da partida');
  trilha.append(marcas, barra);
  const vels = el('div', 'rp-vels');
  vels.setAttribute('role', 'group');
  vels.setAttribute('aria-label', 'Velocidade');
  const botoesVel = VELOCIDADES.map((v) => {
    const b = botao(`${v}×`, 'mini', () => mudaVelocidade(v));
    b.setAttribute('aria-pressed', 'false');
    vels.append(b);
    return b;
  });
  const status = el('span', 'sub rp-status');
  const controles = el('footer', 'rp-controles', [play, relogioEl, total, trilha, vels, status]);

  const corpo = el('div', 'rp-corpo', [palco, painel]);
  const tela = el('div', 'replay', [topo, corpo, legenda, controles]);
  tela.tabIndex = -1;
  raiz.replaceChildren(tela);
  tela.focus();

  // Marcas na barra de tempo: abates (cor do time de quem matou) e objetivos.
  for (const e of eventos) {
    if (!['abate', 'monstro', 'estrutura'].includes(e.tipo)) continue;
    const m = el('i', `marca-t ${e.tipo} ${info.porId[e.quem_id]?.time === 200 ? 'vermelho' : 'azul'}`);
    m.style.left = `${(e.t_ms / duracaoMs) * 100}%`;
    m.title = `${relogio(e.t_ms)} · ${eventoTexto(e, nomes)}`;
    marcas.append(m);
  }

  // --- Estado ---
  let t = Math.min(duracaoMs, Math.max(0, (rota.t ?? 0) * 1000));
  let tocando = false;
  let vel = 1;
  let escolhido = null;
  let ultimoQuadro = null;
  let ultimoPainel = 0;
  let ultimoBarra = 0;
  let vivo = true;
  let lado = 600; // lado do mapa em pixels (ajusta)
  let ret = 36; // lado do retrato em pixels
  let antes = performance.now();

  const janelas = new Janelas({
    duracaoMs,
    carrega: async (de, ate) => {
      const [est, atv] = await Promise.all([
        api.estados(gameId, de / 1000, ate / 1000, PASSO_S),
        api.atividades(gameId, de / 1000, ate / 1000).catch(() => ({ atividades: [] })),
      ]);
      return { quadros: est.quadros, atividades: atv.atividades };
    },
  });

  // Figuras dos 10.
  const figuras = new Map();
  for (const pt of p.participantes) {
    const f = el('button', `figura ${pt.time === 200 ? 'vermelho' : 'azul'}`);
    f.type = 'button';
    f.setAttribute('aria-label', `${pt.campeao} · ${pt.nome}: abrir detalhes`);
    const ret = retrato(pt, 40);
    const nome = el('span', 'f-nome', pt.nome);
    const atv = el('span', 'f-atv');
    const hab = el('span', 'f-hab');
    hab.hidden = true;
    f.append(el('span', 'f-ret', [ret, hab]), nome, atv);
    f.addEventListener('click', (ev) => {
      ev.stopPropagation();
      escolhido = escolhido === pt.participante_id ? null : pt.participante_id;
      ultimoPainel = 0;
      desenhaPainel(true);
    });
    camadaJog.append(f);
    figuras.set(pt.participante_id, { f, atv, hab, pt });
  }

  function mudaVelocidade(v) {
    vel = v;
    botoesVel.forEach((b, i) => {
      b.classList.toggle('on', VELOCIDADES[i] === v);
      b.setAttribute('aria-pressed', String(VELOCIDADES[i] === v));
    });
  }
  mudaVelocidade(1);

  function alterna() {
    if (t >= duracaoMs) t = 0;
    tocando = !tocando;
    play.textContent = tocando ? '❚❚' : '▶';
    play.setAttribute('aria-label', tocando ? 'Pausar' : 'Tocar');
    if (!tocando) ctx.marca({ vista: 'replay', gameId, t: Math.round(t / 100) / 10 });
  }

  function vaiPara(novo) {
    t = Math.min(duracaoMs, Math.max(0, novo));
    ultimoPainel = 0;
    if (!tocando) ctx.marca({ vista: 'replay', gameId, t: Math.round(t / 100) / 10 });
  }

  barra.addEventListener('input', () => vaiPara(Number(barra.value) * 1000));

  // --- Zonas ---
  let zonasDesenhadas = false;
  function desenhaZonas() {
    if (zonasDesenhadas) return;
    zonasDesenhadas = true;
    canvas.width = grade.colunas;
    canvas.height = grade.linhas.length;
    const c = canvas.getContext('2d');
    const cores = Object.fromEntries(zonas.zonas.map((z) => [z.codigo, z.cor]));
    for (let i = 0; i < grade.linhas.length; i++) {
      const linha = grade.linhas[i];
      for (let j = 0; j < linha.length; j++) {
        const cod = grade.codigos[linha.charCodeAt(j) - 65];
        if (!cod || cod === 'fora_do_mapa') continue;
        c.fillStyle = cores[cod] || '#000';
        c.fillRect(j, grade.linhas.length - 1 - i, 1, 1);
      }
    }
  }
  cbZonas.addEventListener('change', () => {
    if (cbZonas.checked) desenhaZonas();
    canvas.hidden = !cbZonas.checked;
  });

  // --- Eventos no mapa ---
  const evNoMapa = new Map();
  function desenhaEventos() {
    const vis = eventosRecentes(evMapa, t, EVENTO_MS);
    const chaves = new Set();
    for (const e of vis) {
      const k = `e${e.id}`;
      chaves.add(k);
      if (evNoMapa.has(k)) continue;
      const { left, top } = emPorcento(e.x, e.y);
      // Eventos no mesmo lugar (dois abates na mesma luta): o rótulo do seguinte sobe uma linha.
      const vizinhos = vis.filter((o) => o !== e && o.t_ms <= e.t_ms && evNoMapa.has(`e${o.id}`) && Math.hypot(o.x - e.x, o.y - e.y) < 900).length;
      const m = el('div', `ev-mapa ${e.tipo} ${info.porId[e.quem_id]?.time === 200 ? 'vermelho' : 'azul'}`, [
        el('i', '', e.tipo === 'abate' ? '✕' : e.tipo === 'monstro' ? '◆' : '▲'),
        el('span', '', eventoTexto(e, nomes)),
      ]);
      m.style.left = `${left}%`;
      m.style.top = `${top}%`;
      m.style.setProperty('--n', String(vizinhos));
      camadaEv.append(m);
      evNoMapa.set(k, m);
    }
    for (const [k, m] of evNoMapa) {
      if (!chaves.has(k)) {
        m.remove();
        evNoMapa.delete(k);
      }
    }
  }

  // Balão da habilidade usada: a última de cada rastreado nos últimos HABILIDADE_MS.
  function desenhaHabilidades() {
    const ate = achaIndice(habilidades, t);
    const ultima = new Map();
    for (let i = ate; i >= 0; i--) {
      const h = habilidades[i];
      if (t - h.t_ms > HABILIDADE_MS) break;
      if (!ultima.has(h.participante_id)) ultima.set(h.participante_id, h);
    }
    for (const [pid, fg] of figuras) {
      const h = ultima.get(pid);
      fg.hab.hidden = !h;
      if (h) fg.hab.textContent = String(h.dados?.habilidade || '').toUpperCase();
    }
  }

  // --- Quadro ---
  function desenhaQuadro() {
    const dados = janelas.dadosEm(t);
    if (!dados) {
      const est = janelas.estadoEm(t);
      aviso.hidden = false;
      aviso.textContent =
        est === 'erro' ? `Não deu para carregar este trecho: ${janelas.erroEm(t)?.message || 'erro'}. Arraste a barra para tentar de novo.` : 'Carregando este trecho…';
      status.textContent = 'carregando…';
      return false;
    }
    aviso.hidden = true;
    status.textContent = '';
    const q = quadroNoInstante(dados.quadros, t);
    if (!q) return true;
    ultimoQuadro = { dados, q };
    // Onde desenhar: o lugar verdadeiro, afastando um pouco quem caiu em cima de outro.
    const vis = q.jogadores.filter((j) => j.x !== null && j.x !== undefined);
    const px = vis.map((j) => {
      const p = paraTela(j.x, j.y, lado);
      return { x: p.px, y: p.py };
    });
    const desenho = new Map(separa(px, ret * 1.15, { maxDesvio: ret * 1.3 }).map((p, i) => [vis[i].participante_id, p]));
    for (const j of q.jogadores) {
      const fg = figuras.get(j.participante_id);
      if (!fg) continue;
      if (j.x === null || j.x === undefined) {
        fg.f.hidden = true;
        continue;
      }
      fg.f.hidden = false;
      const d = desenho.get(j.participante_id);
      fg.f.style.left = `${d.x}px`;
      fg.f.style.top = `${d.y}px`;
      const aprox = !j.rastreado || j.origem === 'linha_do_tempo';
      fg.f.classList.toggle('aprox', aprox);
      fg.f.classList.toggle('morto', Boolean(j.morto));
      fg.f.classList.toggle('escolhido', escolhido === j.participante_id);
      const tr = j.rastreado ? atividadeNoInstante(dados.atividades, j.participante_id, t) : null;
      const texto = j.morto ? 'morto' : tr ? atividadeTexto(tr, nomes) : '';
      if (fg.atv.textContent !== texto) fg.atv.textContent = texto;
      fg.atv.className = `f-atv ${j.morto ? 'morto' : tr?.atividade || ''}`;
      fg.atv.hidden = !texto;
    }
    return true;
  }

  // --- Painel do jogador ---
  function desenhaPainel(forca = false) {
    if (!escolhido) {
      painel.hidden = true;
      return;
    }
    const agora = performance.now();
    if (!forca && agora - ultimoPainel < 250) return;
    ultimoPainel = agora;
    painel.hidden = false;
    const pt = info.porId[escolhido];
    const dados = ultimoQuadro?.dados;
    // Números do último quadro do servidor (o fato), não do interpolado.
    let j = null;
    if (dados) {
      const i = Math.max(0, achaIndice(dados.quadros, t));
      j = dados.quadros[i]?.jogadores.find((x) => x.participante_id === escolhido) || null;
    }
    const fechar = botao('✕', 'icone', () => {
      escolhido = null;
      desenhaPainel(true);
    });
    fechar.setAttribute('aria-label', 'Fechar o painel');
    const cab = el('div', 'rp-p-cab', [
      retrato(pt, 44),
      el('div', '', [el('b', pt.time === 200 ? 'tm-v' : 'tm-a', pt.campeao), el('div', 'sub', `${pt.nome}${pt.tag ? `#${pt.tag}` : ''}`)]),
      fechar,
    ]);
    const blocos = [cab];
    if (!j) {
      blocos.push(el('p', 'sub', 'Sem dados neste instante.'));
      painel.replaceChildren(...blocos);
      return;
    }
    const rastreado = j.rastreado;
    blocos.push(el('p', 'sub', `${relogio(t)} · ${rastreado ? 'rastreado' : 'não rastreado'} · posição ${origemPosicao(j.origem)}`));

    // Vida e mana: número do painel (rastreado) ou % das barras laterais.
    const barraV = (rot, atual, max, pct, classe) => {
      const temNum = atual !== null && atual !== undefined && max;
      const fr = temNum ? atual / max : pct !== null && pct !== undefined ? pct / 100 : null;
      const b = el('div', `rp-barra-v ${classe}`);
      const i = el('b');
      i.style.width = `${fr === null ? 0 : Math.round(Math.min(1, Math.max(0, fr)) * 100)}%`;
      b.append(i);
      const txt = temNum ? `${numero(Math.round(atual))} / ${numero(max)}` : fr !== null ? `${Math.round(fr * 100)}%` : 'não lida';
      return el('div', 'rp-vm', [el('span', '', rot), b, el('span', 'num', txt)]);
    };
    blocos.push(
      j.morto
        ? el('p', 'tx-cr', `Morto${j.respawn ? ` · volta em ${Math.ceil(j.respawn)} s` : ''}`)
        : el('div', '', [barraV('Vida', j.vida, j.vida_max, j.vida_pct, 'vida'), barraV('Mana', j.mana, j.mana_max, j.mana_pct, 'mana')]),
    );

    // Q W E R e feitiços.
    const cd = j.cd || {};
    const habs = el('div', 'rp-habs');
    for (const k of ['q', 'w', 'e', 'r', 'd', 'f']) {
      const rc = recarga(cd[k]);
      const rot = k === 'd' ? nomeFeitico(pt.feitico1) : k === 'f' ? nomeFeitico(pt.feitico2) : k.toUpperCase();
      const c = el('div', `rp-hab ${rc.classe}`, [el('b', '', k === 'd' || k === 'f' ? k.toUpperCase() : rot), el('span', '', rc.texto)]);
      c.title = `${rot}: ${rc.titulo}`;
      if (k === 'd' || k === 'f') c.append(el('small', '', rot));
      habs.append(c);
    }
    blocos.push(el('h3', '', 'Habilidades e feitiços'), habs);
    if (!rastreado) blocos.push(el('p', 'sub', 'Recargas, vida em número e atributos só existem para quem foi rastreado.'));

    // Nível, K/D/A, CS, zona.
    const zona = nomeZona(zonaNaGrade(grade, j.x, j.y));
    const kv = el('div', 'kv compacto', [
      el('span', '', 'Nível'),
      el('b', '', String(j.nivel ?? '—')),
      el('span', '', 'K/D/A'),
      el('b', '', `${j.abates ?? 0}/${j.mortes ?? 0}/${j.assistencias ?? 0}`),
      el('span', '', 'CS'),
      el('b', '', String(j.cs ?? '—')),
      el('span', '', 'Zona'),
      el('b', '', zona || '—'),
    ]);
    blocos.push(kv);

    const at = j.atributos || {};
    if (rastreado && Object.values(at).some((v) => v !== null && v !== undefined)) {
      blocos.push(
        el('h3', '', 'Atributos'),
        el('div', 'kv compacto', [
          el('span', '', 'Dano de ataque'),
          el('b', '', numero(at.ad)),
          el('span', '', 'Poder de habilidade'),
          el('b', '', numero(at.ap)),
          el('span', '', 'Armadura'),
          el('b', '', numero(at.armadura)),
          el('span', '', 'Resist. mágica'),
          el('b', '', numero(at.rm)),
          el('span', '', 'Vel. de ataque'),
          el('b', '', numero(at.vel_ataque, 2)),
          el('span', '', 'Vel. de movimento'),
          el('b', '', numero(at.vel_mov)),
        ]),
      );
    }

    const itens = (j.itens || []).filter((x) => x && x.id);
    const caixaItens = el('div', 'rp-itens');
    for (const it of itens) {
      const im = el('img');
      im.src = imagemItem(it.id);
      im.alt = `item ${it.id}`;
      im.title = `item ${it.id}${it.qtd > 1 ? ` × ${it.qtd}` : ''}`;
      im.addEventListener('error', () => im.replaceWith(el('span', 'item-vazio', String(it.id))));
      caixaItens.append(im);
    }
    if (!itens.length) caixaItens.append(el('span', 'sub', 'sem itens'));
    blocos.push(el('h3', '', 'Itens'), caixaItens);

    const tr = rastreado ? atividadeNoInstante(ultimoQuadro.dados.atividades, escolhido, t) : null;
    if (tr) {
      blocos.push(
        el('h3', '', 'Atividade (deduzida)'),
        el('p', '', `${atividadeTexto(tr, nomes)} · confiança ${tr.confianca}`),
        el('ul', 'sinais', (tr.sinais || []).map((s) => el('li', 'sub', s))),
      );
    }
    painel.replaceChildren(...blocos);
  }

  // --- Laço ---
  function quadro(agora) {
    if (!vivo) return;
    const dt = Math.min(250, agora - antes);
    antes = agora;
    janelas.garante(t);
    const pronto = desenhaQuadro();
    if (tocando && pronto) {
      t += dt * vel;
      if (t >= duracaoMs) {
        t = duracaoMs;
        alterna();
      }
    }
    desenhaEventos();
    desenhaHabilidades();
    desenhaPainel();
    relogioEl.textContent = relogio(t);
    if (agora - ultimoBarra > 100 && document.activeElement !== barra) {
      barra.value = String(t / 1000);
      ultimoBarra = agora;
    }
    requestAnimationFrame(quadro);
  }
  requestAnimationFrame(quadro);

  // Tamanho do mapa: o maior quadrado que cabe no palco.
  const ajusta = () => {
    const r = palco.getBoundingClientRect();
    lado = Math.max(200, Math.floor(Math.min(r.width - 20, r.height - 20)));
    ret = Math.round(Math.min(58, Math.max(30, lado * 0.052)));
    mapa.style.width = `${lado}px`;
    mapa.style.height = `${lado}px`;
    mapa.style.setProperty('--ret', `${ret}px`);
  };
  const obs = new ResizeObserver(ajusta);
  obs.observe(palco);
  ajusta();

  // --- Teclado ---
  function tecla(ev) {
    if (ev.target instanceof HTMLInputElement && ev.target.type !== 'range' && ev.target.type !== 'checkbox') return;
    if (ev.key === ' ' || ev.code === 'Space') {
      ev.preventDefault();
      alterna();
    } else if (ev.key === 'ArrowLeft' || ev.key === 'ArrowRight') {
      ev.preventDefault();
      vaiPara(t + (ev.key === 'ArrowLeft' ? -5000 : 5000));
    } else if (ev.key === 'f' || ev.key === 'F') {
      ev.preventDefault();
      if (document.fullscreenElement) document.exitFullscreen?.();
      else document.documentElement.requestFullscreen?.().catch(() => {});
    } else if (ev.key === 'Escape') {
      if (document.fullscreenElement) return; // o próprio navegador sai da tela cheia
      if (escolhido) {
        escolhido = null;
        desenhaPainel(true);
      } else {
        location.hash = sair.getAttribute('href');
      }
    }
  }
  document.addEventListener('keydown', tecla);
  mapa.addEventListener('click', () => {
    if (escolhido) {
      escolhido = null;
      desenhaPainel(true);
    }
  });

  return () => {
    vivo = false;
    obs.disconnect();
    document.removeEventListener('keydown', tecla);
    if (document.fullscreenElement) document.exitFullscreen?.().catch(() => {});
  };
}
