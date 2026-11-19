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

// textoContexto: quem o jogador era ANTES da partida (banco + histórico buscado), numa linha ('' = nada).
export function textoContexto(ctx, funcao, nomeFuncao = (f) => f) {
  if (!ctx) return '';
  const p = [];
  if (ctx.funcao_principal) {
    let t = `principal: ${nomeFuncao(ctx.funcao_principal)}`;
    if (ctx.fracao_principal > 0 && ctx.fracao_principal < 1) t += ` (${Math.round(ctx.fracao_principal * 100)}%)`;
    if (ctx.fora_da_funcao) t += ` (aqui fora dela, como ${nomeFuncao(funcao)})`;
    p.push(t);
  }
  if (ctx.mono) p.push(`mono do campeão (${ctx.campeao_vezes} de ${ctx.campeao_de})`);
  else if (ctx.sem_o_campeao) p.push(`nenhuma das últimas ${ctx.campeao_de} com ele`);
  else if (ctx.mono === false && ctx.campeao_de > 0) p.push(`${ctx.campeao_vezes} das últimas ${ctx.campeao_de} com o campeão`);
  if (ctx.ultimas > 0) p.push(`${ctx.vitorias_ultimas} vitória(s) nas últimas ${ctx.ultimas}`);
  if (ctx.sequencia > 1) p.push(`vinha de ${ctx.sequencia} vitórias seguidas`);
  if (ctx.sequencia < -1) p.push(`vinha de ${-ctx.sequencia} derrotas seguidas`);
  if (ctx.elo) p.push(`elo ${[ctx.elo.elo, ctx.elo.divisao].filter(Boolean).join(' ')}`);
  if (ctx.alcance) p.push(ctx.alcance);
  return p.join(' · ');
}

const NOME_FONTE = { cliente: 'cliente do LoL', riot: 'API da Riot' };

// estadoBuscaContexto: a linha da seção Jogadores-chave sobre o contexto buscado dos 10
// (GET /api/partidas/{id}/contexto-jogadores): o que já foi buscado, de onde e quando, e se dá
// para buscar agora (botão ligado e a dica dele).
export function estadoBuscaContexto(e) {
  if (!e) return { texto: '', pode: false, dica: '' };
  const total = (e.jogadores?.length || 0) + (e.faltam?.length || 0);
  const fontes = e.fontes || {};
  const pode = !!(fontes.cliente || fontes.riot);
  const dica = pode
    ? `busca o histórico de antes desta partida de cada um pelo ${fontes.cliente ? NOME_FONTE.cliente : NOME_FONTE.riot}`
    : [fontes.motivo_cliente, fontes.motivo_riot].filter(Boolean).join('; ');
  if (e.rodando) return { texto: 'buscando o contexto dos jogadores…', pode: false, dica };
  if (!e.jogadores?.length) return { texto: `contexto dos outros jogadores ainda não buscado (só o que o banco tem)`, pode, dica };
  const ultima = e.jogadores.reduce((a, j) => (j.buscado_em > a.buscado_em ? j : a), e.jogadores[0]);
  const de = `${NOME_FONTE[ultima.fonte] || ultima.fonte}, ${String(ultima.buscado_em).slice(0, 10)}`;
  const problemas = e.jogadores.filter((j) => j.estado !== 'ok').length;
  let texto = `contexto buscado de ${e.jogadores.length} de ${total} (${de})`;
  if (problemas) texto += ` · ${problemas} com histórico incompleto ou não achado`;
  return { texto, pode, dica };
}

// progressoBusca: o texto do andamento da busca a partir dos eventos ("3 de 10 jogadores").
export function progressoBusca(feitos, total, fonte) {
  return `buscando pelo ${NOME_FONTE[fonte] || fonte || '…'}: ${feitos} de ${total || 10} jogadores`;
}

const CLASSE_PAPEL = { carregou: 'ok', habilitou: 'ac', foi_carregado: 'n', afundou: 'cr', estavel: 'n' };
const CONFIANCA = { alta: 'alta', media: 'média', baixa: 'baixa' };

// papelNaTela: o papel do jogador (servidor: analise/papel.go) para a tabela dos jogadores-chave:
// a pílula (texto e cor), o estilo da função e a dica com os números, a confiança e o que não deu para
// avaliar. null quando a partida é de antes do papel.
export function papelNaTela(p) {
  if (!p || !p.papel) return null;
  const dica = [`${p.rotulo} (confiança ${CONFIANCA[p.confianca] || p.confianca}): ${(p.motivos || []).join(', ')}`];
  if (p.estilo) dica.push(`estilo ${p.estilo_rotulo} (confiança ${CONFIANCA[p.confianca_estilo] || p.confianca_estilo}): ${(p.motivos_estilo || []).join('; ')}`);
  else if ((p.motivos_estilo || []).length) dica.push(p.motivos_estilo.join('; '));
  if ((p.nao_avaliado || []).length) dica.push(`não avaliado: ${p.nao_avaliado.join('; ')}`);
  dica.push(`fonte: ${p.fonte}`);
  return { texto: p.rotulo, classe: CLASSE_PAPEL[p.papel] || 'n', estilo: p.estilo ? p.estilo_rotulo : '', dica: dica.join('\n') };
}
