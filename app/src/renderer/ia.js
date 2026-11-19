// IA (só nas telas de relatório e conversa, a pedido da pessoa; o rastreio não usa IA). Funções
// puras, testadas em ia.test.mjs: ler instantes e citações de partida nos textos da IA para virar
// link, o Markdown simples da conversa e a montagem do QUADRO DO MAPA que o app desenha para cada
// situação (a IA só devolve o instante e uma anotação curta; quem desenha é o app, com os dados).

import { segundoNoMapa } from './fatos.js';
import { zonaNaGrade } from './mapa.js';

// msDe: "8:48" → 528000 (null se não é instante).
export function msDe(texto) {
  const m = /^\s*(\d{1,3}):([0-5]\d)\s*$/.exec(String(texto ?? ''));
  return m ? (Number(m[1]) * 60 + Number(m[2])) * 1000 : null;
}

// rotaDoInstante: o mapa com play alguns segundos antes do instante (para ver a aproximação).
export function rotaDoInstante(gameId, ms) {
  return `#replay/${gameId}/${segundoNoMapa(ms)}`;
}

// segmentos: corta o texto em pedaços para a tela transformar em link. Tipos: 'texto', 'instante'
// ({t, ms, gameId}) e 'partida' ({gameId}). "(partida 3304217746, 8:48)" vira instante daquela
// partida; um "8:48" solto vira instante da partida padrão (sem padrão: fica texto).
export function segmentos(texto, gameIdPadrao = null) {
  const s = String(texto ?? '');
  const re = /partida (\d{6,})(?:,\s*(\d{1,2}:[0-5]\d))?|\b(\d{1,2}:[0-5]\d)\b/g;
  const out = [];
  let ultimo = 0;
  let m;
  while ((m = re.exec(s))) {
    if (m.index > ultimo) out.push({ tipo: 'texto', v: s.slice(ultimo, m.index) });
    if (m[1]) {
      const gameId = Number(m[1]);
      out.push({ tipo: 'partida', v: `partida ${m[1]}`, gameId });
      if (m[2]) {
        out.push({ tipo: 'texto', v: ', ' });
        out.push({ tipo: 'instante', v: m[2], t: m[2], ms: msDe(m[2]), gameId });
      }
    } else if (gameIdPadrao) {
      out.push({ tipo: 'instante', v: m[3], t: m[3], ms: msDe(m[3]), gameId: gameIdPadrao });
    } else {
      out.push({ tipo: 'texto', v: m[3] });
    }
    ultimo = re.lastIndex;
  }
  if (ultimo < s.length) out.push({ tipo: 'texto', v: s.slice(ultimo) });
  // Junta textos vizinhos (fica mais simples de desenhar e de testar).
  return out.reduce((acc, x) => {
    const a = acc[acc.length - 1];
    if (a && a.tipo === 'texto' && x.tipo === 'texto') a.v += x.v;
    else acc.push({ ...x });
    return acc;
  }, []);
}

// blocosMarkdown: o Markdown simples que a IA usa na conversa, em blocos: {tipo: 'h'|'p'|'li',
// partes: [{texto, forte}]}. Sem HTML: a tela monta com textContent.
export function blocosMarkdown(texto) {
  const blocos = [];
  let par = [];
  const fechaPar = () => {
    if (par.length) blocos.push({ tipo: 'p', partes: negrito(par.join(' ')) });
    par = [];
  };
  for (const bruta of String(texto ?? '').split(/\r?\n/)) {
    const l = bruta.trim();
    if (!l) {
      fechaPar();
      continue;
    }
    const h = /^#{1,4}\s+(.*)$/.exec(l);
    const li = /^(?:[-*•]|\d+[.)])\s+(.*)$/.exec(l);
    if (h) {
      fechaPar();
      blocos.push({ tipo: 'h', partes: negrito(h[1]) });
    } else if (li) {
      fechaPar();
      blocos.push({ tipo: 'li', partes: negrito(li[1]) });
    } else {
      par.push(l);
    }
  }
  fechaPar();
  return blocos;
}

function negrito(s) {
  const partes = [];
  const re = /\*\*(.+?)\*\*/g;
  let ultimo = 0;
  let m;
  while ((m = re.exec(s))) {
    if (m.index > ultimo) partes.push({ texto: s.slice(ultimo, m.index), forte: false });
    partes.push({ texto: m[1], forte: true });
    ultimo = re.lastIndex;
  }
  if (ultimo < s.length) partes.push({ texto: s.slice(ultimo), forte: false });
  return partes;
}

// celulasDaZona: as células da grade (GET /api/mapa/zonas) de uma zona, em coordenadas do jogo.
export function celulasDaZona(grade, codigo) {
  const i = grade?.codigos?.indexOf(codigo) ?? -1;
  if (i < 0) return [];
  const letra = String.fromCharCode(65 + i);
  const out = [];
  grade.linhas.forEach((linha, li) => {
    for (let j = 0; j < linha.length; j++) {
      if (linha[j] === letra) {
        const x0 = grade.origem.x + j * grade.celula;
        const y0 = grade.origem.y + li * grade.celula;
        out.push({ x0, y0, x1: x0 + grade.celula, y1: y0 + grade.celula });
      }
    }
  });
  return out;
}

// centroDaZona: o ponto médio das células da zona (null se a zona não está na grade).
export function centroDaZona(grade, codigo) {
  const cs = celulasDaZona(grade, codigo);
  if (!cs.length) return null;
  let x = 0;
  let y = 0;
  for (const c of cs) {
    x += (c.x0 + c.x1) / 2;
    y += (c.y0 + c.y1) / 2;
  }
  return { x: x / cs.length, y: y / cs.length };
}

const temPos = (j) => j && j.x !== null && j.x !== undefined && j.y !== null && j.y !== undefined;

// montaQuadro: o que desenhar num instante. Entradas: o Quadro de GET .../momento (os 10), os
// trechos de atividade, as sentinelas (GET .../sentinelas), a visão dos times (GET .../visao, ou
// null), o participante em foco e o time dele, a anotação validada pelo servidor ({realce, seta,
// regiao, legenda}) e a grade das zonas. Saída: marcas (uma por jogador com posição), sentinelas,
// seta em coordenadas do jogo, região e o rótulo de atividade do jogador em foco.
export function montaQuadro({ quadro, atividades = [], sentinelas = [], visao = null, foco, timeFoco, anotacao = null, grade = null, t }) {
  const realce = new Set(anotacao?.realce || []);
  const estado = new Map();
  for (const v of visao?.times || []) {
    if (v.time !== timeFoco) continue;
    for (const j of v.jogadores || []) estado.set(j.participante_id, estadoEm(j, t));
  }
  const atv = new Map();
  for (const a of atividades) if (a.t_ms <= t && t < a.fim_ms) atv.set(a.participante_id, a);
  const marcas = [];
  for (const j of quadro?.jogadores || []) {
    if (!temPos(j)) continue;
    marcas.push({
      pid: j.participante_id,
      x: j.x,
      y: j.y,
      morto: Boolean(j.morto),
      aproximada: j.origem === 'linha_do_tempo',
      foco: j.participante_id === foco,
      realce: realce.has(j.participante_id),
      visao: estado.get(j.participante_id) || '',
      atividade: atv.get(j.participante_id) || null,
    });
  }
  // O foco por último: fica por cima.
  marcas.sort((a, b) => Number(a.foco) - Number(b.foco) || Number(a.realce) - Number(b.realce));
  const vivas = (sentinelas || []).filter((s) => s.x !== null && s.x !== undefined && s.t_ms <= t && (s.ate_ms === null || s.ate_ms === undefined || s.ate_ms > t));
  const ponto = (p) => {
    if (!p) return null;
    if (p.participante) {
      const m = marcas.find((x) => x.pid === p.participante);
      return m ? { x: m.x, y: m.y } : null;
    }
    if (p.zona) return centroDaZona(grade, p.zona);
    return null;
  };
  let seta = null;
  if (anotacao?.seta) {
    const de = ponto(anotacao.seta.de);
    const para = ponto(anotacao.seta.para);
    if (de && para) seta = { de, para };
  }
  return {
    marcas,
    sentinelas: vivas.map((s) => ({ x: s.x, y: s.y, time: s.time, tipo: s.tipo_nome || s.tipo })),
    seta,
    regiao: anotacao?.regiao && grade ? anotacao.regiao : '',
    atividadeFoco: atv.get(foco) || null,
    temVisao: estado.size > 0,
  };
}

// estadoEm (igual ao de visao.js, sem importar o DOM): visivel, fora, indeterminado, fora_do_mapa ou ''.
function estadoEm(j, t) {
  const dentro = (l) => (l || []).some(([de, ate]) => t >= de && t < ate);
  if (dentro(j.visivel)) return 'visivel';
  if (dentro(j.fora)) return 'fora';
  if (dentro(j.indeterminado)) return 'indeterminado';
  if (dentro(j.fora_do_mapa)) return 'fora_do_mapa';
  return '';
}

// zonaDoPonto: atalho para a legenda ("na rota do meio").
export const zonaDoPonto = (grade, x, y) => zonaNaGrade(grade, x, y);

// semMaterial: as vozes de uma situação que não tinham dados (para juntar numa linha só).
export function separaVozes(vozes) {
  const com = [];
  const sem = [];
  for (const v of vozes || []) (v.sem_dados ? sem : com).push(v);
  return { com, sem };
}

// linhasMaterial: o material que cada voz recebe neste pedido (da prévia ou do relatório),
// uma linha por voz: os temas escolhidos e por quê, ou o arquivo único, ou o que falta.
export function linhasMaterial(vozes) {
  return (vozes || []).map((v) => {
    if (v.temas?.length) {
      const temas = v.temas.map((t) => `${t.tema} (${t.motivos.join('; ')})`).join(', ');
      const fora = v.fora?.length ? `; fora do teto: ${v.fora.map((f) => f.split(':')[0]).join(', ')}` : '';
      return `${v.voz}: ${v.caracteres} caracteres, temas ${temas}${fora}`;
    }
    if (v.arquivos?.length || v.partidas) {
      const partes = [...(v.arquivos || [])];
      if (v.partidas) partes.push(`${v.partidas} partida(s) de referência`);
      return `${v.voz}: ${v.caracteres ?? 0} caracteres, ${partes.join(', ')}`;
    }
    return `${v.voz}: sem material (${v.falta || 'nada na pasta de conhecimento'})`;
  });
}

// perguntaDaSituacao: o texto que vai para a conversa quando a pessoa clica em "perguntar ao
// especialista" numa situação do relatório.
export function perguntaDaSituacao(s) {
  const partes = [`Situação aos ${s.t}: ${s.situacao}`];
  if (s.pergunta) partes.push(s.pergunta);
  partes.push('O que o especialista diz?');
  return partes.join('\n\n');
}

// estadoDaVoz: a linha da voz no painel "Quem fala nesta análise" (GET .../ia/referencias):
// fala (com o quê) ou muda (com o que falta para ela falar, ex. "faltam partidas de Esmeralda
// na selva: rode a coleta").
export function estadoDaVoz(v) {
  const partes = [];
  if (v.temas?.length) partes.push(`${v.temas.length} tema(s)`);
  else if (v.arquivos?.length) partes.push(v.arquivos.join(', '));
  if (v.partidas) partes.push(`referência: ${v.amostra || `${v.partidas} partida(s)`}`);
  if (partes.length) return { fala: true, texto: partes.join(' · ') + (v.falta ? ` (${v.falta})` : '') };
  return { fala: false, texto: `sem material: ${v.falta || 'nada na pasta de conhecimento'}` };
}

// textoEloComparacao: de onde veio o elo com que a voz dos melhores do elo compara.
export function textoEloComparacao(e) {
  if (!e) return '';
  const riot = e.riot?.elo ? `API da Riot: ${[e.riot.elo, e.riot.divisao].filter(Boolean).join(' ')} (${e.riot.consultado_em.slice(0, 10)})` : '';
  if (e.fonte === 'escolhido') return `escolhido por você${riot ? ` · ${riot}` : ''}`;
  if (e.fonte === 'riot') return `padrão pela ${riot}`;
  if (e.fonte === 'partida') return 'o elo da coleta desta partida';
  return 'elo não conhecido: escolha um para a voz dos melhores do elo comparar';
}
