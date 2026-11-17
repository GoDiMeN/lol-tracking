// Os dados do mapa com play: interpolação entre quadros e carregamento por janelas com pré-busca.
// Puro (replay-dados.test.mjs): quem busca na rede é a função `carrega` injetada.
//
// O servidor devolve os 10 de `passo` em `passo` (GET /api/partidas/{id}/estados?de=&ate=&passo=).
// A tela anda a 60 quadros por segundo; entre dois quadros do servidor a posição é interpolada.
// Uma partida de 39 min não cabe numa resposta (máx. 1200 quadros) nem precisa: busca-se a janela
// do instante atual e, perto do fim dela, a próxima.

export const JANELA_MS = 60000;
export const PASSO_S = 0.5;
export const PRE_BUSCA_MS = 20000;
export const MAX_JANELAS = 8;

// Acima desta velocidade entre dois quadros vizinhos não é andar, é salto: recall, renascer, Flash,
// e os saltos que voltam ou os clones (W da LeBlanc, Shaco, Neeko, Wukong — descobertas, seção 6).
// Também é salto qualquer troca de origem da posição (câmera → morte → base). No salto a figura
// fica onde estava até o instante do quadro novo e então troca (teleporte, sem deslizar).
export const VELOCIDADE_SALTO = 1200; // unidades por segundo
export const DISTANCIA_MINIMA_SALTO = 300; // abaixo disto nunca é salto (ruído da posição)

// achaIndice: o maior i com quadros[i].t_ms <= t (−1 se t é antes do primeiro). Busca binária.
export function achaIndice(quadros, t) {
  let lo = 0;
  let hi = quadros.length - 1;
  let r = -1;
  while (lo <= hi) {
    const m = (lo + hi) >> 1;
    if (quadros[m].t_ms <= t) {
      r = m;
      lo = m + 1;
    } else {
      hi = m - 1;
    }
  }
  return r;
}

const lerp = (a, b, f) => (a === null || a === undefined ? a : b === null || b === undefined ? a : a + (b - a) * f);

// interpolaJogador: o jogador entre o quadro a e o b, com f em [0, 1] e dt = distância em ms
// entre os quadros. Os campos que não são posição/barras vêm do quadro de trás (o último fato
// conhecido); a posição desliza, salvo salto.
export function interpolaJogador(a, b, f, dtMs) {
  if (!a) return b ? { ...b } : null;
  const r = { ...a };
  if (!b || a.x === null || a.x === undefined || b.x === null || b.x === undefined) return r;
  if (eSalto(a, b, dtMs)) return f >= 1 ? { ...b } : r;
  r.x = lerp(a.x, b.x, f);
  r.y = lerp(a.y, b.y, f);
  r.vida_pct = lerp(a.vida_pct, b.vida_pct, f);
  r.mana_pct = lerp(a.mana_pct, b.mana_pct, f);
  return r;
}

// eSalto: entre a e b o jogador não andou, saltou (ver VELOCIDADE_SALTO).
export function eSalto(a, b, dtMs) {
  // Entre "camera" e "interpolada" (trava perdida um instante) a posição é contínua: não é salto.
  const ORIGENS_SALTO = ['morte', 'base'];
  const trocou = a.origem && b.origem && a.origem !== b.origem && (ORIGENS_SALTO.includes(a.origem) || ORIGENS_SALTO.includes(b.origem));
  if (a.morto !== b.morto || trocou) return true;
  const d = Math.hypot(b.x - a.x, b.y - a.y);
  if (d <= DISTANCIA_MINIMA_SALTO) return false;
  return d > (VELOCIDADE_SALTO * Math.max(1, dtMs)) / 1000;
}

// quadroNoInstante: os 10 no instante t (ms), interpolados. null se não há quadros.
export function quadroNoInstante(quadros, t) {
  if (!quadros || !quadros.length) return null;
  let i = achaIndice(quadros, t);
  if (i < 0) i = 0;
  const a = quadros[i];
  const b = quadros[i + 1];
  if (!b || t <= a.t_ms) return { t_ms: t, jogadores: a.jogadores.map((j) => ({ ...j })) };
  const dt = b.t_ms - a.t_ms;
  const f = dt > 0 ? Math.min(1, Math.max(0, (t - a.t_ms) / dt)) : 0;
  const deB = new Map(b.jogadores.map((j) => [j.participante_id, j]));
  return { t_ms: t, jogadores: a.jogadores.map((j) => interpolaJogador(j, deB.get(j.participante_id), f, dt)) };
}

// atividadeNoInstante: o trecho de atividade do jogador que contém t (ou null).
export function atividadeNoInstante(trechos, pid, t) {
  let melhor = null;
  for (const tr of trechos || []) {
    if (tr.participante_id !== pid) continue;
    if (tr.t_ms <= t && t < tr.fim_ms) {
      if (!melhor || tr.t_ms > melhor.t_ms) melhor = tr;
    }
  }
  return melhor;
}

// eventosRecentes: os eventos de [t − janela, t] (para fazer surgir no mapa e sumir depois).
export function eventosRecentes(eventos, t, janelaMs) {
  const r = [];
  for (const e of eventos) {
    if (e.t_ms > t) break;
    if (e.t_ms >= t - janelaMs) r.push(e);
  }
  return r;
}

export const inicioDaJanela = (t, tamanho = JANELA_MS) => Math.max(0, Math.floor(t / tamanho) * tamanho);

// janelasNecessarias: a janela de t e, perto do fim dela (pré-busca), a seguinte; nunca além da
// duração da partida.
export function janelasNecessarias(t, duracaoMs, tamanho = JANELA_MS, preBusca = PRE_BUSCA_MS) {
  const ini = inicioDaJanela(Math.min(Math.max(0, t), Math.max(0, duracaoMs - 1)), tamanho);
  const r = [ini];
  const prox = ini + tamanho;
  if (prox < duracaoMs && t >= prox - preBusca) r.push(prox);
  return r;
}

// Janelas guarda as janelas carregadas (ou carregando) e descarta as mais longe do instante atual
// acima de `maximo`. `carrega(de_ms, ate_ms)` devolve uma promessa de {quadros, atividades}.
export class Janelas {
  constructor({ carrega, duracaoMs, tamanho = JANELA_MS, preBusca = PRE_BUSCA_MS, maximo = MAX_JANELAS }) {
    this.carrega = carrega;
    this.duracaoMs = duracaoMs;
    this.tamanho = tamanho;
    this.preBusca = preBusca;
    this.maximo = maximo;
    this.mapa = new Map(); // ini → {estado: 'carregando'|'pronto'|'erro', dados, promessa, erro}
  }

  // garante pede o que falta para o instante t e devolve a promessa da janela de t.
  garante(t) {
    const precisa = janelasNecessarias(t, this.duracaoMs, this.tamanho, this.preBusca);
    for (const ini of precisa) {
      const j = this.mapa.get(ini);
      if (j && j.estado !== 'erro') continue;
      const ate = Math.min(ini + this.tamanho, this.duracaoMs);
      const novo = { estado: 'carregando', dados: null, erro: null };
      novo.promessa = Promise.resolve()
        .then(() => this.carrega(ini, ate))
        .then(
          (dados) => {
            novo.estado = 'pronto';
            novo.dados = dados;
            return dados;
          },
          (erro) => {
            novo.estado = 'erro';
            novo.erro = erro;
            throw erro;
          },
        );
      // Erro fica guardado no estado; a promessa sem dono não pode virar "unhandled rejection".
      novo.promessa.catch(() => {});
      this.mapa.set(ini, novo);
    }
    this.descarta(t);
    return this.mapa.get(precisa[0]).promessa;
  }

  // dadosEm: os dados prontos da janela de t, ou null (ainda carregando, erro, nunca pedida).
  dadosEm(t) {
    const j = this.mapa.get(inicioDaJanela(Math.min(t, Math.max(0, this.duracaoMs - 1)), this.tamanho));
    return j && j.estado === 'pronto' ? j.dados : null;
  }

  estadoEm(t) {
    const j = this.mapa.get(inicioDaJanela(Math.min(t, Math.max(0, this.duracaoMs - 1)), this.tamanho));
    return j ? j.estado : 'ausente';
  }

  erroEm(t) {
    const j = this.mapa.get(inicioDaJanela(Math.min(t, Math.max(0, this.duracaoMs - 1)), this.tamanho));
    return j?.erro ?? null;
  }

  descarta(t) {
    if (this.mapa.size <= this.maximo) return;
    const longe = [...this.mapa.keys()].sort((a, b) => Math.abs(b - t) - Math.abs(a - t));
    for (const ini of longe) {
      if (this.mapa.size <= this.maximo) break;
      if (this.mapa.get(ini).estado === 'carregando') continue;
      this.mapa.delete(ini);
    }
  }
}
