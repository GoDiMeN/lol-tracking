// Validador do DESENHO do mapa com play: toca a partida quadro a quadro, nas velocidades da tela,
// com as MESMAS funções que a tela usa para decidir onde cada retrato e cada etiqueta vão
// (quadroNoInstante, criaArranjo, atividadeNoInstante, atividadeCurta, a escolha da janela), e
// devolve os glitches. Puro: roda no Node (valida-desenho.test.mjs e scripts/valida-replay.mjs).
//
// O que conta como glitch (pedido do dono, 2026-11-17):
//   - anel_fora: o anel (a verdade) não está no lugar do dado;
//   - pulo: o retrato andou, em relação ao anel, mais do que a velocidade máxima da acomodação
//     (o anel pode saltar quando o próprio dado salta: volta à base, renascer, teleporte);
//   - troca_de_lado: dois retratos vizinhos trocaram de lado de um quadro para o seguinte;
//   - tremor: o deslocamento do retrato inverte de direção 3 vezes ou mais em 1 s de tela;
//   - morto_se_mexendo: o desenho do morto andou;
//   - desvio_acima: o retrato longe do anel acima do limite;
//   - etiqueta_piscando: a etiqueta trocou e voltou ao texto anterior em menos de 1 s de jogo.
// Aviso (contado à parte, não é pulo): etiquetas_cobrindo, duas etiquetas uma em cima da outra.

import { quadroNoInstante, atividadeEstavel, inicioDaJanela, criaRotulador, JANELA_MS } from './replay-dados.js';
import { paraTela } from './mapa.js';
import { criaArranjo, PARAMS } from './chamadas.js';
import { atividadeCurta, atividadeTexto, ATIVIDADES } from './texto.js';

export const VELOCIDADES_VALIDADAS = [1, 2, 4, 8];
const QUADRO_TELA_MS = 1000 / 60;

// etiquetaDoJogador: a etiqueta que a tela põe embaixo do retrato, antes de segurar o detalhe
// (criaRotulador): {texto, chave (a atividade), inteiro, tr}. A atividade vale mesmo num quadro
// solto "não rastreado" (8:03 da 3304217746: meio segundo sem trava apagava a etiqueta).
export function etiquetaDoJogador(j, atividades, t, nomes) {
  if (j.morto) return { texto: 'morto', chave: 'morto', inteiro: 'morto', tr: null };
  const tr = atividadeEstavel(atividades, j.participante_id, t);
  if (!tr) return { texto: '', chave: '', inteiro: '', tr: null };
  return { texto: atividadeCurta(tr, nomes), chave: tr.atividade, inteiro: atividadeTexto(tr, nomes), tr };
}

// textoNoMapa: no grupo de 3 ou mais o mapa não tem lugar para "lutando com Orianna, Yunara e
// Leona": fica o rótulo curto (a atividade, ou o NOME do objetivo/torre). O texto inteiro vai no
// título e no ponto da cor da atividade.
export function textoNoMapa(et, n) {
  if (!et.tr || n < 3) return et.texto;
  if ((et.tr.atividade === 'objetivo' || et.tr.atividade === 'torre') && et.tr.alvo) return et.tr.alvo;
  return ATIVIDADES[et.tr.atividade] || et.tr.atividade;
}

// rotuloDoJogador: o texto sem segurar (o desenho de antes usava assim).
export const rotuloDoJogador = (j, atividades, t, nomes) => etiquetaDoJogador(j, atividades, t, nomes).texto;

// dadosDaJanela: como a tela, os quadros da janela de t (cada janela foi pedida à parte).
function dadosDaJanela(janelas, t, duracaoMs) {
  return janelas.get(inicioDaJanela(Math.min(t, Math.max(0, duracaoMs - 1)), JANELA_MS)) || null;
}

// validaDesenho: janelas = Map(início da janela em ms → {quadros, atividades}) como a tela carrega.
// `fabrica` e `rotulo` existem para comparar com o desenho antigo (scripts/valida-replay.mjs, o
// "antes"); a tela usa os padrões.
export function validaDesenho({
  janelas,
  duracaoMs,
  times = {},
  nomes = {},
  de = 0,
  ate = duracaoMs,
  ret = 36,
  lado = 900,
  velocidades = VELOCIDADES_VALIDADAS,
  fabrica = (r) => criaArranjo({ ret: r }),
  rotulo = null,
}) {
  const resultado = {};
  for (const v of velocidades) resultado[v] = validaUmaVelocidade({ janelas, duracaoMs, times, nomes, de, ate, ret, lado, v, fabrica, rotulo });
  return resultado;
}

function validaUmaVelocidade({ janelas, duracaoMs, times, nomes, de, ate, ret, lado, v, fabrica, rotulo }) {
  const arranjo = fabrica(ret);
  // Padrão: a regra da tela (etiqueta + segurar o detalhe).
  const segura = criaRotulador();
  const rotuloFn =
    rotulo ||
    ((j, atividades, t, nomes, a) => {
      const e = etiquetaDoJogador(j, atividades, t, nomes);
      return segura(j.participante_id, textoNoMapa(e, a?.n || 1), e.chave, t);
    });
  const glitches = [];
  const avisos = [];
  const abertos = new Map(); // chave tipo|ids → último t (junta quadros seguidos num episódio só)
  const anota = (lista, tipo, t, ids, valor) => {
    const k = `${tipo}|${ids.join(',')}`;
    const ult = abertos.get(k);
    abertos.set(k, t);
    if (ult !== undefined && t - ult <= 250 * v) return; // mesmo episódio
    lista.push({ tipo, t_ms: Math.round(t), ids, valor: Math.round(valor * 100) / 100 });
  };
  const ant = new Map(); // id → {ax, ay, rx, ry, ox, oy, morto, dox, doy}
  const rotulos = new Map(); // id → {texto, desde, anterior}
  const inversoes = new Map(); // id → [t de tela das inversões]
  const passoMax = (PARAMS.vmax * ret * QUADRO_TELA_MS) / 1000;
  let quadros = 0;
  let tela = 0;
  for (let t = de; t <= ate; t += QUADRO_TELA_MS * v) {
    tela += QUADRO_TELA_MS;
    const dados = dadosDaJanela(janelas, t, duracaoMs);
    if (!dados) continue;
    const q = quadroNoInstante(dados.quadros, t);
    if (!q) continue;
    quadros++;
    const vivos = [];
    for (const j of q.jogadores) {
      if (j.x === null || j.x === undefined || j.morto) continue;
      const p = paraTela(j.x, j.y, lado);
      vivos.push({ id: j.participante_id, x: p.px, y: p.py, time: times[j.participante_id] || 100 });
    }
    const arr = arranjo.passo(vivos, QUADRO_TELA_MS, { lado, velocidade: v });
    const agora = new Map();
    for (const j of q.jogadores) {
      if (j.x === null || j.x === undefined) continue;
      const id = j.participante_id;
      const p = paraTela(j.x, j.y, lado);
      const a = arr.get(id) || { ax: p.px, ay: p.py, rx: p.px, ry: p.py, escala: 1 };
      const cur = { ax: a.ax, ay: a.ay, rx: a.rx, ry: a.ry, ox: a.rx - a.ax, oy: a.ry - a.ay, morto: Boolean(j.morto), esc: a.escala, n: a.n || 1 };
      agora.set(id, cur);
      if (Math.hypot(a.ax - p.px, a.ay - p.py) > 0.5) anota(glitches, 'anel_fora', t, [id], Math.hypot(a.ax - p.px, a.ay - p.py));
      if (Math.hypot(cur.ox, cur.oy) > ret * PARAMS.maxDesvio + 0.5) anota(glitches, 'desvio_acima', t, [id], Math.hypot(cur.ox, cur.oy) / ret);
      const pr = ant.get(id);
      if (pr) {
        // Morto deslizando (o salto do morto para a base no renascimento é do dado, não conta).
        const dm = Math.hypot(cur.rx - pr.rx, cur.ry - pr.ry);
        if (pr.morto && cur.morto && dm > 0.5 && dm < ret * 0.8) anota(glitches, 'morto_se_mexendo', t, [id], dm);
        if (!pr.morto && !cur.morto) {
          const dox = cur.ox - pr.ox;
          const doy = cur.oy - pr.oy;
          const d = Math.hypot(dox, doy);
          if (d > passoMax * 1.02 + 0.5) anota(glitches, 'pulo', t, [id], d);
          // Tremor: o RETRATO (no grupo) indo e voltando sem o anel ter ido e voltado: quem
          // inventou o vaivém foi o desenho, não o dado.
          const drx = cur.rx - pr.rx;
          const dry = cur.ry - pr.ry;
          const dax = cur.ax - pr.ax;
          const day = cur.ay - pr.ay;
          // O anel virou (mais de 90°) nos últimos quadros: o dado zigue-zagueou entre dois
          // quadros do servidor; o retrato vai junto um pouco depois (a suavização), e isso não é
          // tremor do desenho.
          const virou = pr.dax !== undefined && dax * pr.dax + day * pr.day < 0 && Math.hypot(dax, day) > 0.2;
          cur.virouEm = virou ? tela : pr.virouEm;
          const anelVoltou = cur.virouEm !== undefined && tela - cur.virouEm < 200;
          if (cur.n >= 2 && pr.n >= 2 && !anelVoltou && Math.hypot(drx, dry) > 0.5 && Math.hypot(pr.drx || 0, pr.dry || 0) > 0.5 && drx * pr.drx + dry * pr.dry < 0) {
            const l = (inversoes.get(id) || []).filter((x) => tela - x < 1000);
            l.push(tela);
            inversoes.set(id, l);
            if (l.length >= 3) anota(glitches, 'tremor', t, [id], l.length);
          }
          Object.assign(cur, { dox, doy, drx, dry, dax, day });
        }
      }
      // Etiqueta: piscar é voltar ao texto anterior em menos de 1 s de jogo.
      const texto = rotuloFn(j, dados.atividades, t, nomes, a);
      const r = rotulos.get(id);
      if (!r) rotulos.set(id, { texto, desde: t, anterior: null, antesDesde: t });
      else if (r.texto !== texto) {
        if (texto === r.anterior && t - r.desde < 1000) anota(glitches, 'etiqueta_piscando', t, [id], t - r.desde);
        rotulos.set(id, { texto, desde: t, anterior: r.texto });
      }
      cur.texto = texto;
    }
    // Troca de lado: dois retratos vivos perto um do outro com o vetor entre eles invertido.
    const ids = [...agora.keys()].filter((id) => !agora.get(id).morto && ant.has(id) && !ant.get(id).morto);
    for (let i = 0; i < ids.length; i++) {
      for (let k = i + 1; k < ids.length; k++) {
        const a1 = agora.get(ids[i]);
        const b1 = agora.get(ids[k]);
        const a0 = ant.get(ids[i]);
        const b0 = ant.get(ids[k]);
        const v1x = b1.rx - a1.rx;
        const v1y = b1.ry - a1.ry;
        const v0x = b0.rx - a0.rx;
        const v0y = b0.ry - a0.ry;
        const perto = Math.hypot(v1x, v1y) < ret * 1.6 && Math.hypot(v0x, v0y) < ret * 1.6;
        // O dado saltou (base, renascer): não conta.
        const saltou = Math.hypot(a1.ax - a0.ax, a1.ay - a0.ay) > ret || Math.hypot(b1.ax - b0.ax, b1.ay - b0.ay) > ret;
        if (perto && !saltou && v1x * v0x + v1y * v0y < 0 && Math.hypot(v0x, v0y) > 2) anota(glitches, 'troca_de_lado', t, [ids[i], ids[k]], Math.hypot(v1x - v0x, v1y - v0y));
      }
    }
    // Aviso: etiquetas uma em cima da outra (largura estimada do texto).
    const caixas = [];
    for (const [id, c] of agora) {
      if (!c.texto) continue;
      const larg = c.texto.length * 6 * (c.esc || 1) + 8;
      const topo = c.ry + (ret * (c.esc || 1)) / 2 + 2;
      caixas.push({ id, x0: c.rx - larg / 2, x1: c.rx + larg / 2, y0: topo, y1: topo + 14 });
    }
    for (let i = 0; i < caixas.length; i++) {
      for (let k = i + 1; k < caixas.length; k++) {
        const a = caixas[i];
        const b = caixas[k];
        const w = Math.min(a.x1, b.x1) - Math.max(a.x0, b.x0);
        const h = Math.min(a.y1, b.y1) - Math.max(a.y0, b.y0);
        if (w > 0 && h > 0 && w * h > 0.25 * Math.min((a.x1 - a.x0) * 14, (b.x1 - b.x0) * 14)) anota(avisos, 'etiquetas_cobrindo', t, [a.id, b.id], (w * h) / 14);
      }
    }
    for (const [id, c] of agora) ant.set(id, c);
    for (const id of [...ant.keys()]) if (!agora.has(id)) ant.delete(id);
  }
  const conta = (l) => l.reduce((m, g) => ((m[g.tipo] = (m[g.tipo] || 0) + 1), m), {});
  return { velocidade: v, quadros, glitches, contagem: conta(glitches), avisos, contagemAvisos: conta(avisos) };
}
