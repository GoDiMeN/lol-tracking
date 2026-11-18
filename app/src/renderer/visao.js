// Visão dos times (GET /api/partidas/{id}/visao; regras em dados.md, "Visão dos times"): dois passes
// por partida, um com a névoa do replay na visão de cada time, que dizem quem do outro time estava à
// vista em cada instante (lido do minimapa). Funções puras, testadas em visao.test.mjs.

import { relogio } from './texto.js';

// A velocidade padrão do passe de visão e as folgas da estimativa: as mesmas do servidor
// (rastreio/visao_fila.go): por passe, o seek, a tecla e a conferência do painel; abrir e fechar o
// replay, uma vez.
export const VELOCIDADE_VISAO = 8;
export const FOLGA_PASSE_VISAO_S = 10;
export const FOLGA_ABRIR_REPLAY_S = 40;

function trechoS({ duracaoS, deS = 0, ateS = 0 }) {
  const fim = ateS > 0 ? Math.min(ateS, duracaoS) : duracaoS;
  return Math.max(0, fim - Math.max(0, deS));
}

// estimativaVisao: os dois passes avulsos (abre e fecha o replay só para eles).
export function estimativaVisao({ duracaoS, deS = 0, ateS = 0, velocidade = VELOCIDADE_VISAO }) {
  return 2 * (trechoS({ duracaoS, deS, ateS }) / Math.max(0.5, velocidade) + FOLGA_PASSE_VISAO_S) + FOLGA_ABRIR_REPLAY_S;
}

// estimativaVisaoNoRastreio: o que os dois passes acrescentam no fim de um rastreio (o replay já
// está aberto).
export function estimativaVisaoNoRastreio({ duracaoS, deS = 0, ateS = 0, velocidade = VELOCIDADE_VISAO }) {
  return 2 * (trechoS({ duracaoS, deS, ateS }) / Math.max(0.5, velocidade) + FOLGA_PASSE_VISAO_S);
}

// dentro: o instante t cai num dos intervalos [de, ate)? (em ordem de tempo: busca binária)
function dentro(lista, t) {
  let lo = 0;
  let hi = (lista || []).length - 1;
  while (lo <= hi) {
    const m = (lo + hi) >> 1;
    const [de, ate] = lista[m];
    if (t < de) hi = m - 1;
    else if (t >= ate) lo = m + 1;
    else return true;
  }
  return false;
}

// estadoEm: 'visivel', 'fora', 'indeterminado' ou '' (sem captura ou morto) para um jogador.
export function estadoEm(j, t) {
  if (!j) return '';
  if (dentro(j.visivel, t)) return 'visivel';
  if (dentro(j.fora, t)) return 'fora';
  if (dentro(j.indeterminado, t)) return 'indeterminado';
  return '';
}

// doObservador: a visão do time observador (100/200), ou null.
export function doObservador(visao, time) {
  return (visao?.times || []).find((v) => v.time === time) || null;
}

// estadosNoInstante: participante → estado, dos inimigos do observador no instante.
export function estadosNoInstante(visao, observador, t) {
  const m = new Map();
  const v = doObservador(visao, observador);
  for (const j of v?.jogadores || []) m.set(j.participante_id, estadoEm(j, t));
  return m;
}

// sentinelasLidasEm: as sentinelas lidas do minimapa no chão em t (de_ms ≤ t ≤ ate_ms), dos dois
// times, cada uma com o `time` dono (o observador do passe em que foi lida).
export function sentinelasLidasEm(visao, t) {
  const out = [];
  for (const v of visao?.times || []) {
    for (const s of v.sentinelas || []) if (s.de_ms <= t && t <= s.ate_ms) out.push({ ...s, time: v.time });
  }
  return out;
}

// temSentinelasLidas: algum passe de visão leu sentinela?
export function temSentinelasLidas(visao) {
  return (visao?.times || []).some((v) => (v.sentinelas || []).length > 0);
}

export const TEXTO_ESTADO = { visivel: 'à vista', fora: 'fora da visão', indeterminado: 'visão incerta' };

export const MOTIVOS = {
  painel: 'o painel do replay não mostrava a névoa do time',
  aliados: 'o minimapa não mostrava os aliados no lugar',
  sem_posicao: 'sem a posição rastreada dele',
  anel_sem_dono: 'um retrato do time dele fora do lugar esperado',
  coberto: 'outro retrato em cima do lugar dele',
  borda: 'na borda do minimapa',
};

// tituloEstado: a dica do retrato/linha na caixa "visão".
export function tituloEstado(estado, observador) {
  const quem = observador === 200 ? 'o time vermelho' : 'o time azul';
  if (estado === 'visivel') return `À vista: ${quem} via este jogador neste instante (lido do minimapa na visão dele).`;
  if (estado === 'fora') return `Fora da visão: ${quem} NÃO via este jogador neste instante; o anel marca onde ele estava de verdade.`;
  if (estado === 'indeterminado') return `Visão incerta: não deu para decidir se ${quem} via este jogador (retrato coberto, sem posição rastreada ou leitura duvidosa).`;
  return '';
}

// tituloSentinelaLida: "Sentinela do time azul lida no minimapa de 7:16 a 9:12 (480 capturas)".
export function tituloSentinelaLida(s) {
  const time = s.time === 200 ? 'vermelho' : 'azul';
  const fim = s.ate_o_fim ? 'até o fim do passe' : `até ${relogio(s.ate_ms)}`;
  return `Sentinela do time ${time} lida no minimapa (passe de visão): de ${relogio(s.de_ms)} ${fim}, lugar exato · ${s.capturas} capturas`;
}
