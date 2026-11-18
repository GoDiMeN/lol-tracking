// A base de fatos da partida na tela (servidor: analise/fatos*.go): a lista curta dos momentos-chave,
// por impacto, e os jogadores-chave com a nota de impacto e os componentes. Só lógica pura (testável
// no node); quem desenha é views/partida.js.

const TIPOS = {
  abate: 'Abate',
  luta: 'Luta de time',
  objetivo: 'Objetivo',
  torre: 'Torre',
  inibidor: 'Inibidor',
  freeze: 'Freeze',
  janela_perdida: 'Janela perdida',
  janela_aproveitada: 'Janela aproveitada',
};

// rotuloTipo: o nome do tipo de momento na tela.
export function rotuloTipo(tipo, subtipo = '') {
  if (tipo === 'abate' && subtipo === 'morte_cara') return 'Morte cara';
  return TIPOS[tipo] || tipo;
}

// classeTipo: a cor da pílula (a do resto do app: ok, at, cr, ac, n).
export function classeTipo(tipo, subtipo = '') {
  if (tipo === 'janela_perdida' || subtipo === 'morte_cara') return 'cr';
  if (tipo === 'janela_aproveitada') return 'ok';
  if (tipo === 'luta') return 'at';
  if (tipo === 'objetivo' || tipo === 'torre' || tipo === 'inibidor') return 'ac';
  return 'n';
}

// ouroCurto: 1234 → "1,2 mil"; −850 → "−850"; comSinal põe o "+".
export function ouroCurto(v, comSinal = false) {
  if (v === null || v === undefined || Number.isNaN(Number(v))) return '—';
  const n = Math.round(Number(v));
  const abs = Math.abs(n);
  const corpo = abs >= 1000 ? `${(abs / 1000).toFixed(1).replace('.', ',')} mil` : String(abs);
  if (n < 0) return `−${corpo}`;
  return comSinal && n > 0 ? `+${corpo}` : corpo;
}

// momentosChave: os da lista curta (chave), do maior impacto para o menor; empate pelo instante.
export function momentosChave(resumo) {
  const l = (resumo?.momentos || []).filter((m) => m.chave);
  return l.sort((a, b) => b.impacto - a.impacto || a.t_ms - b.t_ms);
}

// segundoNoMapa: o instante (s) em que o mapa com play abre para mostrar o momento: alguns segundos
// antes, para ver a aproximação.
export function segundoNoMapa(tMs, antesS = 5) {
  return Math.max(0, Math.round((Number(tMs) || 0) / 1000) - antesS);
}

// jogadoresChave: os 10 na ordem da nota (posicao_nota 1 primeiro).
export function jogadoresChave(resumo) {
  return [...(resumo?.jogadores || [])].sort((a, b) => a.posicao_nota - b.posicao_nota);
}

// textoContexto: o que o banco sabe do jogador fora da partida, numa linha ('' = nada).
export function textoContexto(ctx, funcao, nomeFuncao = (f) => f) {
  if (!ctx) return '';
  const p = [];
  if (ctx.funcao_principal) {
    let t = `principal: ${nomeFuncao(ctx.funcao_principal)}`;
    if (ctx.fora_da_funcao) t += ` (aqui fora dela, como ${nomeFuncao(funcao)})`;
    p.push(t);
  }
  if (ctx.ultimas > 0) p.push(`${ctx.vitorias_ultimas} vitória(s) nas últimas ${ctx.ultimas}`);
  if (ctx.sequencia > 1) p.push(`vinha de ${ctx.sequencia} vitórias seguidas`);
  if (ctx.sequencia < -1) p.push(`vinha de ${-ctx.sequencia} derrotas seguidas`);
  if (ctx.elo) p.push(`elo ${[ctx.elo.elo, ctx.elo.divisao].filter(Boolean).join(' ')}`);
  return p.join(' · ');
}
