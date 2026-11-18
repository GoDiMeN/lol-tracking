// Decomposição dos atributos lidos da tela (painel do jogador no mapa com play): base do
// campeão no nível + itens do inventário + objetivos (dragões medidos no próprio dado, Barão)
// + a SOBRA, que a tela chama de "runas e efeitos temporários" (runas, fragmentos, acúmulos,
// elixires, passivas de item e de campeão que dão atributo conforme a situação). Se a conta
// passar do lido (sobra negativa), a diferença aparece como "não explicado". Puro
// (atributos.test.mjs).
//
// Dados: GET /api/partidas/{id}/estaticos (base e crescimento dos campeões, itens, na versão da
// partida) e GET /api/partidas/{id}/bonus-objetivos (o bônus de cada dragão medido no degrau).

import { BONUS_DRAGAO } from './bonus.js';

export const ATRIBUTOS = [
  { chave: 'ad', nome: 'Dano de ataque', casas: 0 },
  { chave: 'ap', nome: 'Poder de habilidade', casas: 0 },
  { chave: 'armadura', nome: 'Armadura', casas: 0 },
  { chave: 'rm', nome: 'Resist. mágica', casas: 0 },
  { chave: 'vel_ataque', nome: 'Vel. de ataque', casas: 2 },
  { chave: 'vel_mov', nome: 'Vel. de movimento', casas: 0 },
];

// Que dragão mexe em que atributo, e o valor por acúmulo da tabela (reserva da medição). Os
// mesmos números do servidor (server/atributos/objetivos.go, Tabela) e de bonus.js.
export const DRAGAO_ATRIBUTO = {
  FIRE_DRAGON: { ad: 0.03, ap: 0.03 },
  EARTH_DRAGON: { armadura: 0.05, rm: 0.05 },
  AIR_DRAGON: { vel_mov: 0.05 },
  HEXTECH_DRAGON: { vel_ataque: 0.05 },
};

// crescimento: o fator do crescimento por nível. Fórmula oficial do jogo
// (wiki.leagueoflegends.com/en-us/Champion_statistic, "Growth statistic"):
// atributo = base + crescimento × (n − 1) × (0,7025 + 0,0175 × (n − 1)). Não é linear: o
// ganho por nível sobe de 72% do "crescimento" no nível 2 a ~100% no meio e ~128% no 18.
export function crescimento(nivel) {
  const n = Math.max(1, Math.min(18, Number(nivel) || 1));
  return (n - 1) * (0.7025 + 0.0175 * (n - 1));
}

// Teto suave da velocidade de movimento (wiki.leagueoflegends.com/en-us/Movement_speed): acima
// de 415 conta 80% do que passa; acima de 490, 50%. O mesmo do servidor (ComTeto).
export function comTeto(v) {
  if (v <= 415) return v;
  if (v <= 490) return 415 + (v - 415) * 0.8;
  return 415 + 75 * 0.8 + (v - 490) * 0.5;
}

// baseNoNivel: os atributos de base do campeão `c` (AtributosCampeao) no nível n. A velocidade
// de ataque: o crescimento por nível é velocidade de ataque BÔNUS (%), que multiplica a razão:
// base + razão × bônus (a razão é a própria base na maioria dos campeões).
export function baseNoNivel(c, nivel) {
  const g = crescimento(nivel);
  return {
    ad: c.ad + c.ad_nivel * g,
    ap: 0,
    armadura: c.armadura + c.armadura_nivel * g,
    rm: c.rm + c.rm_nivel * g,
    vel_ataque: c.vel_ataque + (c.vel_ataque_razao || c.vel_ataque) * ((c.vel_ataque_nivel * g) / 100),
    vel_mov: c.vel_mov,
  };
}

// somaItens: o que os itens do inventário dão (ids com repetição; item sem dado conta como 0 e
// vai na lista `sem_dado`).
export function somaItens(ids, itens) {
  const s = { ad: 0, ap: 0, armadura: 0, rm: 0, vel_ataque_pct: 0, vel_mov: 0, vel_mov_pct: 0, ouro: 0, sem_dado: [] };
  for (const id of ids) {
    const it = itens?.[id];
    if (!it) {
      s.sem_dado.push(id);
      continue;
    }
    const a = it.atributos || {};
    s.ad += a.ad || 0;
    s.ap += a.ap || 0;
    s.armadura += a.armadura || 0;
    s.rm += a.rm || 0;
    s.vel_ataque_pct += a.vel_ataque_pct || 0;
    s.vel_mov += a.vel_mov || 0;
    s.vel_mov_pct += a.vel_mov_pct || 0;
    s.ouro += it.custo || 0;
  }
  return s;
}

// idsDoInventario: [{id, qtd}] do quadro → ids (um por slot, como a build no servidor).
export const idsDoInventario = (lista) => (lista || []).filter((x) => x && x.id).map((x) => x.id);

// porAcumulo: o valor por dragão que a tela usa para (sub, atributo): o medido no degrau
// (resumo de GET .../bonus-objetivos) quando houver; senão o da tabela. Devolve também a origem.
export function porAcumulo(resumo, sub, atr) {
  const m = (resumo || []).find((r) => r.sub === sub && r.atributo === atr);
  if (m && m.medido !== null && m.medido !== undefined && m.confianca !== 'nenhuma') {
    return { p: m.medido, origem: 'medido', confianca: m.confianca, medidas: m.n };
  }
  const tab = DRAGAO_ATRIBUTO[sub]?.[atr];
  if (tab !== undefined) return { p: tab, origem: 'tabela', confianca: '', medidas: 0 };
  return null;
}

// objetivosDoAtributo: os dragões do time (contagem por tipo) que mexem no atributo, com o valor
// por acúmulo (medido ou tabela).
export function objetivosDoAtributo(atr, dragoes, resumo) {
  const r = [];
  const porTipo = {};
  for (const s of dragoes || []) porTipo[s] = (porTipo[s] || 0) + 1;
  for (const [sub, n] of Object.entries(porTipo)) {
    if (DRAGAO_ATRIBUTO[sub]?.[atr] === undefined) continue;
    const pa = porAcumulo(resumo, sub, atr);
    r.push({ sub, n, nome: BONUS_DRAGAO[sub]?.nome || sub, ...pa });
  }
  return r;
}

const parte = (rotulo, valor, detalhe = '', extra = {}) => ({ rotulo, valor, detalhe, ...extra });

// decompoe: a decomposição de um atributo `atr` lido (`lido`) para o campeão `c` no nível, com o
// inventário (ids), os itens estáticos, os dragões do time (lista de subtipos, na ordem), o
// resumo da medição e o Barão ({ad: [min,max], ap: [min,max]} se ativo, senão null).
// Devolve {atributo, lido, partes: [{rotulo, valor, detalhe, objetivo?, sub?}], sobra, nao_explicado,
// avisos}. `sobra` ≥ 0 = "runas e efeitos temporários"; `nao_explicado` > 0 quando a conta passa
// do lido.
export function decompoe({ atr, lido, c, nivel, ids, itens, dragoes, resumo, barao = null, emCombate = null }) {
  const casas = atr === 'vel_ataque' ? 2 : 0;
  const tolerancia = atr === 'vel_ataque' ? 0.005 : 0.5; // a tela arredonda
  const r = { atributo: atr, lido, partes: [], sobra: 0, nao_explicado: 0, avisos: [], casas };
  if (lido === null || lido === undefined || !c) return null;
  const base = baseNoNivel(c, nivel);
  const s = somaItens(ids, itens);
  if (s.sem_dado.length) r.avisos.push(`${s.sem_dado.length} item(ns) sem dado na versão: contados como 0`);
  const objs = objetivosDoAtributo(atr, dragoes, resumo);
  let explicado = 0;
  const soma = (p) => {
    r.partes.push(p);
    explicado += p.valor;
  };
  if (atr === 'ad' || atr === 'ap' || atr === 'armadura' || atr === 'rm') {
    soma(parte(`base do campeão no nível ${nivel}`, base[atr], atr === 'ap' ? 'campeão não tem poder de habilidade de base' : ''));
    soma(parte('itens', s[atr]));
    if (barao && (atr === 'ad' || atr === 'ap')) {
      const [a, b] = barao[atr];
      soma(parte('Barão', (a + b) / 2, a === b ? '' : `entre ${a} e ${b} (a fonte dá de 2 em 2 minutos)`, { objetivo: true, sub: 'BARON' }));
    }
    // Dragões: % do TOTAL (somam entre si: 1 + k·p). A parte deles no valor lido é
    // lido × k·p / (1 + Σ k·p).
    const somaP = objs.reduce((t, o) => t + o.n * o.p, 0);
    for (const o of objs) {
      const v = (lido * o.n * o.p) / (1 + somaP);
      soma(parte(`${o.nome} × ${o.n}`, v, textoP(o), { objetivo: true, sub: o.sub }));
    }
  } else if (atr === 'vel_ataque') {
    const razao = c.vel_ataque_razao || c.vel_ataque;
    soma(parte(`base do campeão no nível ${nivel}`, base.vel_ataque, `${fmt(c.vel_ataque, 3)} + crescimento por nível`));
    soma(parte('itens', razao * s.vel_ataque_pct, `+${Math.round(s.vel_ataque_pct * 100)}% de velocidade de ataque bônus × ${fmt(razao, 3)}`));
    for (const o of objs) {
      soma(parte(`${o.nome} × ${o.n}`, razao * o.n * o.p, `${textoP(o)} de velocidade de ataque bônus`, { objetivo: true, sub: o.sub }));
    }
  } else if (atr === 'vel_mov') {
    const bruto = (base.vel_mov + s.vel_mov) * (1 + s.vel_mov_pct);
    const comItens = comTeto(bruto);
    soma(parte('base do campeão', base.vel_mov));
    soma(parte('itens', comItens - base.vel_mov, s.vel_mov_pct ? `+${s.vel_mov} e +${Math.round(s.vel_mov_pct * 100)}%` : ''));
    for (const o of objs) {
      const com = comTeto((base.vel_mov + s.vel_mov) * (1 + s.vel_mov_pct + o.n * o.p));
      const v = com - comItens;
      // Só vale fora de combate: conta se o lido está mais perto do valor com o bônus.
      const ativo = emCombate === null ? Math.abs(lido - com) <= Math.abs(lido - comItens) : !emCombate;
      soma(parte(`${o.nome} × ${o.n}`, ativo ? v : 0, `${textoP(o)}; só fora de combate (${ativo ? 'valendo neste instante' : `agora não: seria +${fmt(v, 0)}`})`, { objetivo: true, sub: o.sub, potencial: v, ativo }));
    }
    r.avisos.push('teto suave do jogo aplicado (acima de 415 conta 80%, acima de 490 conta 50%)');
  } else {
    return null;
  }
  const dif = lido - explicado;
  if (dif >= -tolerancia) r.sobra = Math.max(0, dif);
  else r.nao_explicado = -dif;
  return r;
}

const fmt = (v, casas = 0) => (Math.round(v * 10 ** casas) / 10 ** casas).toLocaleString('pt-BR', { minimumFractionDigits: casas, maximumFractionDigits: casas });

function textoP(o) {
  const pct = (x) => `${(x * 100).toLocaleString('pt-BR', { maximumFractionDigits: 2 })}%`;
  return o.origem === 'medido'
    ? `+${pct(o.p)} por dragão, medido no degrau desta partida (${o.n > 1 ? `${o.n} acúmulos` : '1 acúmulo'})`
    : `+${pct(o.p)} por dragão, valor da tabela (sem medição confiável nesta partida)`;
}

// textoObjetivo: a frase do ícone no atributo: "Armadura = 62, dos quais ~3 vêm do dragão da
// montanha (+5,07% medido)". Só com parte de objetivo > 0.
export function textoObjetivo(nomeAtr, d) {
  if (!d) return '';
  const objs = d.partes.filter((p) => p.objetivo && (p.valor > 0 || p.ativo === false));
  if (!objs.length) return '';
  const lido = fmt(d.lido, d.casas);
  const pedacos = objs.map((p) => {
    const v = p.ativo === false ? `0 agora (até ~${fmt(p.potencial, d.casas)} fora de combate)` : `~${fmt(p.valor, d.casas)}`;
    return `${v} ${p.sub === 'BARON' ? 'do Barão' : `do ${p.rotulo.replace(/ × \d+$/, '').replace(/^Dragão/, 'dragão')}`}`;
  });
  return `${nomeAtr} = ${lido}, dos quais ${pedacos.join(' e ')}`;
}

export { fmt as formataAtributo };

// ouroDosItens: o custo somado do inventário (ids).
export const ouroDosItens = (ids, itens) => somaItens(ids, itens).ouro;

// adversarioDeRota: o participante do outro time na mesma posição (TOP/JUNGLE/MIDDLE/BOTTOM/
// UTILITY); null se a posição não é conhecida.
export function adversarioDeRota(participantes, pid) {
  const eu = participantes.find((x) => x.participante_id === pid);
  if (!eu?.posicao) return null;
  return participantes.find((x) => x.time !== eu.time && x.posicao === eu.posicao) || null;
}
