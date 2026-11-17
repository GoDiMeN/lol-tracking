// Os dados da análise e do rastreio em frases e números para a tela (tarefa 5). Funções puras,
// testadas em texto.test.mjs: nada aqui toca no DOM nem na rede. Vocabulário em
// docs/arquitetura/dados.md; a tela fala a língua de quem joga ("saiu da rota", não o código).

import { duracao } from './formato.js';

// relogio: ms de jogo → "8:48" (o relógio do jogo).
export function relogio(ms) {
  return duracao((Number(ms) || 0) / 1000);
}

// lerRelogio: "8:48" (ou "15" = minutos) → ms; null se não dá para ler.
export function lerRelogio(texto) {
  const t = String(texto ?? '').trim();
  if (!t) return null;
  const m = /^(\d{1,3})(?::([0-5]?\d))?$/.exec(t);
  if (!m) return null;
  if (m[2] === undefined) return Number(m[1]) * 60000;
  return (Number(m[1]) * 60 + Number(m[2])) * 1000;
}

const NUM = new Intl.NumberFormat('pt-BR', { maximumFractionDigits: 1 });
const NUM2 = new Intl.NumberFormat('pt-BR', { maximumFractionDigits: 2 });

// numero: 1173 → "1.173"; 3.456 → "3,5". Nulo vira "—".
export function numero(v, casas = 1) {
  if (v === null || v === undefined || v === '' || Number.isNaN(Number(v))) return '—';
  return (casas === 2 ? NUM2 : NUM).format(Number(v));
}

// tempoHumano: segundos → "~2 h 21 min", "~45 min", "~30 s" (estimativas).
export function tempoHumano(segundos) {
  const s = Math.max(0, Math.round(Number(segundos) || 0));
  if (s < 60) return `~${s} s`;
  const min = Math.round(s / 60);
  if (min < 60) return `~${min} min`;
  const h = Math.floor(min / 60);
  const resto = min % 60;
  return resto ? `~${h} h ${resto} min` : `~${h} h`;
}

// Abrir o replay, travar a câmera e fechar custam alguns segundos por passe além do tempo de jogo.
export const SOBRA_POR_PASSE_S = 15;

// estimativaRastreio: quanto tempo de relógio leva rastrear `jogadores` num trecho a `velocidade`.
export function estimativaRastreio({ duracaoS, jogadores, velocidade, deS = 0, ateS = 0 }) {
  const fim = ateS > 0 ? Math.min(ateS, duracaoS) : duracaoS;
  const trecho = Math.max(0, fim - Math.max(0, deS));
  return jogadores * (trecho / Math.max(0.5, velocidade) + SOBRA_POR_PASSE_S);
}

export const ZONAS = {
  base_azul: 'base azul',
  base_vermelha: 'base vermelha',
  rota_topo: 'rota do topo',
  rota_meio: 'rota do meio',
  rota_baixo: 'rota de baixo',
  rio_cima: 'rio de cima',
  rio_baixo: 'rio de baixo',
  cova_barao: 'cova do Barão',
  cova_dragao: 'cova do Dragão',
  selva_azul_cima: 'selva azul de cima',
  selva_azul_baixo: 'selva azul de baixo',
  selva_vermelha_cima: 'selva vermelha de cima',
  selva_vermelha_baixo: 'selva vermelha de baixo',
  fora_do_mapa: 'fora do mapa',
};

export function nomeZona(codigo) {
  return ZONAS[codigo] || (codigo ? String(codigo).replaceAll('_', ' ') : '');
}

// Feitiços de invocador (ids do LCU) com o nome do cliente em português.
export const FEITICOS = {
  1: 'Purificar',
  3: 'Exaustão',
  4: 'Flash',
  6: 'Fantasma',
  7: 'Curar',
  11: 'Golpear',
  12: 'Teleporte',
  13: 'Clarividência',
  14: 'Incendiar',
  21: 'Barreira',
  32: 'Bola de Neve',
};

export function nomeFeitico(id) {
  return FEITICOS[id] || (id ? `feitiço ${id}` : '');
}

// recarga: uma recarga lida (segundos). 0 pronta, −1 não aprendida, nulo não lida.
export function recarga(v) {
  if (v === null || v === undefined) return { texto: '?', classe: 'nl', titulo: 'não lida' };
  if (v < 0) return { texto: '—', classe: 'na', titulo: 'ainda não aprendida' };
  if (v === 0) return { texto: 'pronta', classe: 'ok', titulo: 'pronta' };
  return { texto: `${v < 10 ? numero(v) : Math.round(v)} s`, classe: 'cd', titulo: `em recarga: ${numero(v)} s` };
}

// Atividade deduzida (dados.md): o rótulo curto.
export const ATIVIDADES = {
  morto: 'morto',
  na_base: 'na base',
  lutando: 'lutando',
  trocando: 'trocando',
  objetivo: 'objetivo',
  torre: 'na torre',
  farmando: 'farmando',
  andando: 'andando',
};

export function juntaNomes(nomes) {
  if (nomes.length <= 1) return nomes.join('');
  return `${nomes.slice(0, -1).join(', ')} e ${nomes[nomes.length - 1]}`;
}

// atividadeTexto: "trocando com LeBlanc", "lutando com Warwick e Teemo".
export function atividadeTexto(trecho, nomes = {}) {
  if (!trecho) return '';
  const base = ATIVIDADES[trecho.atividade] || trecho.atividade;
  const com = (trecho.com || []).map((id) => nomes[id] || `jogador ${id}`);
  if ((trecho.atividade === 'trocando' || trecho.atividade === 'lutando') && com.length) {
    return `${base} com ${juntaNomes(com)}`;
  }
  return base;
}

const DESFECHOS = { morte: 'morreu', abate: 'abate', objetivo: 'objetivo', nada: 'nada aconteceu' };
const FIM_SAIDA = { voltou: 'voltou para a rota', morreu: 'morreu', base: 'foi para a base', fim_do_passe: 'fim do trecho rastreado' };

export const desfecho = (d) => DESFECHOS[d] || d || '';
export const fimDaSaida = (m) => FIM_SAIDA[m] || m || '';

const HABILIDADE = { q: 'Q', w: 'W', e: 'E', r: 'R', d: 'o feitiço D', f: 'o feitiço F' };

// Os tipos da cadeia reduzida que a linha do tempo mostra, em grupos filtráveis.
export const GRUPOS_CADEIA = [
  { id: 'lutas', rotulo: 'abates e mortes', tipos: ['abate', 'assistencia', 'morte', 'renascimento'] },
  { id: 'rota', rotulo: 'saídas de rota', tipos: ['saiu_da_rota', 'voltou_para_rota', 'entrou_em_rota'] },
  { id: 'habilidades', rotulo: 'habilidades', tipos: ['habilidade_usada', 'passiva_em_recarga'] },
  { id: 'vida', rotulo: 'vida baixa', tipos: ['vida_baixa', 'recuperou'] },
  { id: 'base', rotulo: 'base e recall', tipos: ['na_base', 'recall'] },
  { id: 'itens', rotulo: 'itens e nível', tipos: ['compra', 'venda', 'nivel'] },
  { id: 'zonas', rotulo: 'mudou de zona', tipos: ['mudou_zona'] },
  { id: 'atividade', rotulo: 'atividade', tipos: ['atividade'] },
  { id: 'cs', rotulo: 'CS por minuto', tipos: ['minuto', 'cs_divergente'] },
  { id: 'camera', rotulo: 'câmera', tipos: ['trava_perdida', 'trava_retomada'] },
];

export const grupoDoTipo = (tipo) => GRUPOS_CADEIA.find((g) => g.tipos.includes(tipo))?.id || '';

const segs = (ms) => {
  const s = Math.round((Number(ms) || 0) / 1000);
  return s >= 90 ? duracao(s) : `${s} s`;
};

// itemCadeia: um item da cadeia reduzida em frase. `nomes` = {participante_id: campeão}.
export function itemCadeia(item, nomes = {}) {
  const d = item.dados || {};
  const n = (id) => nomes[id] || `jogador ${id}`;
  switch (item.tipo) {
    case 'abate':
      return `abateu ${n(d.vitima)}`;
    case 'assistencia':
      return `ajudou ${n(d.matador)} a abater ${n(d.vitima)}`;
    case 'morte':
      return d.matador ? `morreu para ${n(d.matador)}${d.respawn_s ? ` (volta em ${Math.round(d.respawn_s)} s)` : ''}` : 'morreu';
    case 'renascimento':
      return 'renasceu';
    case 'recall':
      return 'voltou para a base (recall)';
    case 'nivel':
      return `subiu para o nível ${d.nivel}`;
    case 'compra':
      return 'comprou';
    case 'venda':
      return 'saiu do inventário';
    case 'minuto':
      return `CS ${d.cs ?? '?'} no jogo e ${d.cs_lcu ?? '?'} no cliente · ouro ${numero(d.ouro_total)}`;
    case 'cs_divergente':
      return `CS diferente do cliente (${d.cs} no jogo, ${d.cs_lcu} no cliente)`;
    case 'trava_perdida':
      return 'a câmera saiu do jogador';
    case 'trava_retomada':
      return 'a câmera voltou ao jogador';
    case 'habilidade_usada':
      return `usou ${HABILIDADE[d.habilidade] || d.habilidade}${d.recarga_s ? ` (recarga de ${Math.round(d.recarga_s)} s)` : ''}`;
    case 'passiva_em_recarga':
      return `a passiva entrou em recarga${d.recarga_s ? ` (${Math.round(d.recarga_s)} s)` : ''}`;
    case 'vida_baixa':
      return `ficou com pouca vida (${Math.round(d.pct ?? 0)}%)`;
    case 'recuperou':
      return `recuperou a vida (${Math.round(d.pct ?? 0)}%)`;
    case 'mudou_zona':
      return `entrou em ${nomeZona(d.para)}`;
    case 'saiu_da_rota': {
      const vida = d.vida_pct !== null && d.vida_pct !== undefined ? ` com ${Math.round(d.vida_pct)}% de vida` : '';
      return `saiu da ${nomeZona(d.rota)} para ${nomeZona(d.para)}${vida}, por ${segs(d.duracao_ms)} · ${desfecho(d.desfecho)}`;
    }
    case 'voltou_para_rota':
      return `voltou para a ${nomeZona(d.rota)} depois de ${segs(d.duracao_ms)}`;
    case 'entrou_em_rota':
      return `visitou a ${nomeZona(d.rota)} por ${segs(d.duracao_ms)} · ${desfecho(d.desfecho)}`;
    case 'na_base':
      return `na base por ${segs(d.duracao_ms)}`;
    case 'atividade':
      return `${atividadeTexto(d, nomes)} por ${segs(d.duracao_ms)}${d.zona ? ` · ${nomeZona(d.zona)}` : ''}`;
  }
  return String(item.tipo).replaceAll('_', ' ');
}

// valorIndicador: o valor com a unidade do vocabulário (GET /api/indicadores).
export function valorIndicador(valor, unidade) {
  if (valor === null || valor === undefined) return '—';
  switch (unidade) {
    case '%':
      return `${numero(valor)}%`;
    case 's':
      return valor >= 90 ? duracao(valor) : `${numero(valor)} s`;
    case 'por_min':
      return `${numero(valor, 2)}/min`;
    case 'unidades':
      return numero(Math.round(valor));
    default:
      return numero(valor);
  }
}

// diferenca: "+3,2" / "−1.150" (com o sinal de menos tipográfico).
export function diferenca(v, unidade) {
  if (v === null || v === undefined) return '—';
  const s = valorIndicador(Math.abs(v), unidade);
  if (Math.abs(v) < 1e-9) return s;
  return (v > 0 ? '+' : '−') + s;
}

// bytes: 3571712 → "3,4 MB".
export function bytes(n) {
  const v = Number(n) || 0;
  if (v < 1024) return `${v} B`;
  if (v < 1024 * 1024) return `${numero(v / 1024)} KB`;
  if (v < 1024 ** 3) return `${numero(v / 1024 / 1024)} MB`;
  return `${numero(v / 1024 ** 3, 2)} GB`;
}

// pilulaPasse: o estado de um passe na fila de rastreio.
export function pilulaPasse(passe) {
  switch (passe.estado) {
    case 'concluido':
      return { classe: 'ok', texto: 'pronto' };
    case 'rodando':
      return { classe: 'ac', texto: 'rastreando' };
    case 'pendente':
      return { classe: 'n', texto: 'na fila' };
    case 'pausado':
      return { classe: 'at', texto: 'pausado' };
    case 'cancelado':
      return { classe: 'n', texto: 'cancelado' };
    case 'falhou':
      return { classe: 'cr', texto: 'erro' };
  }
  return { classe: 'n', texto: passe.estado || '' };
}

export const ESTADOS_RASTREIO = {
  rodando: { classe: 'ac', texto: 'rastreando' },
  pausado: { classe: 'at', texto: 'pausado' },
  concluido: { classe: 'ok', texto: 'concluído' },
  cancelado: { classe: 'n', texto: 'cancelado' },
  falhou: { classe: 'cr', texto: 'parou com erro' },
};

// progressoPasse: 0–1 do passe dentro do trecho do rastreio.
export function progressoPasse(passe, rastreio) {
  if (passe.estado === 'concluido') return 1;
  const ini = rastreio?.t_inicio_ms || 0;
  const fim = rastreio?.t_fim_ms > 0 ? rastreio.t_fim_ms : passe.duracao_ms || 0;
  if (fim <= ini) return 0;
  return Math.min(1, Math.max(0, ((passe.t_atual_ms || 0) - ini) / (fim - ini)));
}

// Fases do cliente em que há partida de verdade (CLAUDE.md, regra 2: nada de rastreio).
export const FASES_AO_VIVO = ['ReadyCheck', 'ChampSelect', 'GameStart', 'InProgress', 'Reconnect'];
const FASES_OK = ['None', 'WatchInProgress', ''];

// podeRastrear: {ok, motivo} a partir de GET /api/estado.
export function podeRastrear(estado) {
  if (!estado) return { ok: false, motivo: 'Não deu para saber o estado do cliente do LoL.' };
  if (!estado.lcu?.conectado) return { ok: false, motivo: 'Abra o cliente do LoL e entre na conta para rastrear.' };
  if (FASES_AO_VIVO.includes(estado.fase)) return { ok: false, motivo: 'Você está numa partida: o rastreio só roda fora de jogo.' };
  if (!FASES_OK.includes(estado.fase)) return { ok: false, motivo: 'Saia da fila ou do lobby no cliente do LoL para rastrear.' };
  return { ok: true, motivo: '' };
}

// resumoDano: o que a vítima recebeu (Riot), por quem bateu, com as habilidades em Q W E R.
const SLOTS = ['Q', 'W', 'E', 'R'];
export function resumoDano(recebido, nomes = {}) {
  const por = new Map();
  for (const d of recebido || []) {
    const quem = d.participante_id || 0;
    if (!por.has(quem)) {
      por.set(quem, { participante_id: quem, nome: quem ? nomes[quem] || d.nome : 'torres e monstros', total: 0, partes: new Map() });
    }
    const p = por.get(quem);
    p.total += d.total || 0;
    let rotulo;
    if (d.basico) rotulo = 'ataques';
    else if (d.slot >= 0 && d.slot <= 3) rotulo = SLOTS[d.slot];
    else rotulo = 'outros';
    p.partes.set(rotulo, (p.partes.get(rotulo) || 0) + (d.total || 0));
  }
  return [...por.values()]
    .filter((p) => p.total > 0)
    .sort((a, b) => b.total - a.total)
    .map((p) => ({
      participante_id: p.participante_id,
      nome: p.nome,
      total: p.total,
      partes: [...p.partes.entries()]
        .sort((a, b) => b[1] - a[1])
        .map(([r, v]) => `${r} ${numero(v)}`)
        .join(' · '),
    }));
}

// superioridade: a frase do retrato ("3 contra 1", "em igualdade").
export function superioridade(r) {
  const aliados = (r.aliados_da_vitima ?? 0) + 1;
  const inimigos = r.inimigos_da_vitima ?? 0;
  if (inimigos > aliados) return `vítima em desvantagem: ${inimigos} contra ${aliados}`;
  if (inimigos < aliados) return `vítima em vantagem: ${aliados} contra ${inimigos}`;
  return `em igualdade: ${aliados} contra ${inimigos}`;
}

// origemPosicao: de onde veio a posição de um jogador num retrato/quadro.
export function origemPosicao(origem) {
  switch (origem) {
    case 'rastreado':
    case 'camera':
      return 'rastreada';
    case 'evento':
      return 'local do abate';
    case 'morte':
      return 'onde morreu';
    case 'base':
      return 'na fonte';
    case 'perto_do_abate':
      return 'perto do abate (aproximada)';
    case 'linha_do_tempo':
      return 'aproximada (posição por minuto)';
    case 'interpolada':
      return 'entre duas leituras';
  }
  return 'sem posição';
}

// Eventos da partida no mapa e na barra de tempo.
export function eventoTexto(e, nomes = {}) {
  const d = e.dados || {};
  const quem = nomes[e.quem_id] || e.quem_nome;
  const emQuem = nomes[e.em_quem_id] || e.em_quem_nome;
  switch (e.tipo) {
    case 'abate':
      return `${quem || 'alguém'} abateu ${emQuem || 'alguém'}`;
    case 'monstro': {
      const m = { DRAGON: 'Dragão', BARON_NASHOR: 'Barão', RIFTHERALD: 'Arauto', HORDE: 'Vastilarvas', ATAKHAN: 'Atakhan' }[d.monsterType];
      return `${quem || 'alguém'} pegou ${m || 'um monstro grande'}`;
    }
    case 'estrutura':
      return `${quem || 'o time'} derrubou ${d.buildingType === 'INHIBITOR_BUILDING' ? 'um inibidor' : 'uma torre'}`;
    case 'multiabate':
      return `${quem || ''} fez um multiabate`;
    case 'primeiro_abate':
      return `${quem || ''}: primeiro abate`;
    case 'ace':
      return 'time inteiro abatido';
  }
  return '';
}
