// Funções puras do Especialista Lane (views/especialista.js): nomes do recorte (patch × elo × rota),
// o estado de cada relatório de referência e síntese e o resumo do espelho no NotebookLM.
// Testes em especialista.test.mjs.

export const NOMES_ELO = {
  IRON: 'Ferro',
  BRONZE: 'Bronze',
  SILVER: 'Prata',
  GOLD: 'Ouro',
  PLATINUM: 'Platina',
  EMERALD: 'Esmeralda',
  DIAMOND: 'Diamante',
  MASTER: 'Mestre',
  GRANDMASTER: 'Grão-Mestre',
  CHALLENGER: 'Desafiante',
};

export const NOMES_ROTA = { TOP: 'topo', JUNGLE: 'selva', MIDDLE: 'meio', BOTTOM: 'atirador', UTILITY: 'suporte' };

// "PLATINUM II" → "Platina II".
export function nomeEloEsp(elo) {
  const [cod, ...resto] = String(elo || '').trim().split(/\s+/);
  if (!cod) return '';
  const n = NOMES_ELO[cod.toUpperCase()] || cod;
  return [n, ...resto].join(' ');
}

export const nomeRotaEsp = (r) => NOMES_ROTA[r] || 'sem rota';

// "16.23 · Platina · selva".
export function nomeRecorte(r) {
  return `${r.patch} · ${nomeEloEsp(r.elo)} · ${nomeRotaEsp(r.rota)}`;
}

// O estado do relatório de uma partida de referência, para a lista: texto, classe da pílula e o
// rótulo do botão.
export function estadoRelatorio(ref) {
  if (ref.execucao) return { texto: 'gerando…', classe: 'at', botao: null };
  const r = ref.relatorio;
  if (!r) return { texto: 'sem relatório', classe: 'n', botao: 'Gerar relatório' };
  switch (r.estado) {
    case 'concluido':
      return { texto: `relatório v${r.versao}`, classe: 'ok', botao: 'Refazer' };
    case 'rodando':
      return { texto: 'gerando…', classe: 'at', botao: null };
    case 'cancelado':
      return { texto: `v${r.versao} cancelado`, classe: 'n', botao: 'Gerar de novo' };
  }
  return { texto: `v${r.versao} falhou`, classe: 'cr', botao: 'Gerar de novo' };
}

// O estado da síntese de um recorte.
export function estadoSintese(rec) {
  if (rec.execucao) return { texto: 'síntese rodando…', classe: 'at' };
  const s = rec.sintese;
  if (!s) return { texto: rec.relatorios ? 'sem síntese' : 'sem relatório para sintetizar', classe: 'n' };
  if (s.estado !== 'concluido') return { texto: `síntese v${s.versao}: ${s.estado}`, classe: s.estado === 'rodando' ? 'at' : 'cr' };
  if (s.desatualizada) return { texto: `síntese v${s.versao} desatualizada (usou ${s.relatorios_usados} de ${rec.relatorios})`, classe: 'at' };
  return { texto: `síntese v${s.versao} (${s.relatorios_usados} relatório${s.relatorios_usados === 1 ? '' : 's'})`, classe: 'ok' };
}

// O aviso de quantos foram rastreados ("" com os 10).
export function avisoRastreados(n) {
  if (n >= 10) return '';
  return `${n} de 10 rastreados: a posição dos outros vem da linha do tempo da Riot (por minuto)`;
}

// "em 2 jogadores · 2 partidas" ou "HIPÓTESE · 1 jogador".
export function rotuloRegra(r) {
  const p = r.partidas?.length || 0;
  if ((r.jogadores || 0) >= 2) return `em ${r.jogadores} jogadores · ${p} partida${p === 1 ? '' : 's'}`;
  return 'HIPÓTESE · 1 jogador';
}

// O resumo do espelho para o cabeçalho do cartão.
export function resumoEspelho(e) {
  if (!e) return 'espelho: sem estado';
  if (!e.caderno) return 'NotebookLM: informe o caderno';
  if (!e.ferramenta) return 'NotebookLM: sem a ferramenta nlm (exporte à mão)';
  if (!e.itens.length) return 'NotebookLM: nada para enviar ainda';
  if (!e.pendentes) return `NotebookLM: ${e.enviados} no caderno, tudo em dia`;
  return `NotebookLM: ${e.enviados} no caderno, ${e.pendentes} para enviar`;
}

// Agrupa as partidas de referência por recorte (chave "patch|elo|rota").
export function porRecorte(referencias) {
  const m = new Map();
  for (const r of referencias) {
    const k = `${r.patch}|${r.elo_tier}|${r.rota}`;
    if (!m.has(k)) m.set(k, []);
    m.get(k).push(r);
  }
  return m;
}
