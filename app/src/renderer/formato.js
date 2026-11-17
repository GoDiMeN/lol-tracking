// Formatação do que o servidor devolve para o que a tela mostra. Funções puras, testadas em
// formato.test.mjs: nada aqui toca no DOM nem na rede.

const MESES = ['jan', 'fev', 'mar', 'abr', 'mai', 'jun', 'jul', 'ago', 'set', 'out', 'nov', 'dez'];

const dois = (n) => String(n).padStart(2, '0');

// duracao: segundos → "39:02" (ou "1:02:03" acima de uma hora).
export function duracao(segundos) {
  const s = Math.max(0, Math.round(Number(segundos) || 0));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  return h > 0 ? `${h}:${dois(m)}:${dois(s % 60)}` : `${m}:${dois(s % 60)}`;
}

// dataCurta: ISO em UTC → "15 nov 18:34" no fuso de quem está olhando; com o ano quando não é o atual.
export function dataCurta(iso, agora = new Date()) {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  const ano = d.getFullYear() !== agora.getFullYear() ? ` ${d.getFullYear()}` : '';
  return `${d.getDate()} ${MESES[d.getMonth()]}${ano} ${dois(d.getHours())}:${dois(d.getMinutes())}`;
}

// As posições vêm com os nomes do Live Client Data (TOP, JUNGLE...).
export const POSICOES = { TOP: 'topo', JUNGLE: 'selva', MIDDLE: 'meio', BOTTOM: 'atirador', UTILITY: 'suporte' };

export function posicao(codigo) {
  return POSICOES[codigo] || '';
}

export function kda(p) {
  return `${p.abates ?? 0}/${p.mortes ?? 0}/${p.assistencias ?? 0}`;
}

// titulo: "Akshan · meio · derrota" (sem a posição quando não há, como no ARAM).
export function titulo(p) {
  return [p.campeao, posicao(p.posicao), p.venceu ? 'vitória' : 'derrota'].filter(Boolean).join(' · ');
}

// pilulaReplay: a pílula do estado do replay, com a classe de cor do protótipo.
export function pilulaReplay(replay, patch) {
  const r = replay || {};
  switch (r.estado) {
    case 'baixado':
      return { classe: 'ok', texto: 'baixado' };
    case 'baixavel':
      return { classe: 'ac', texto: 'dá para baixar' };
    case 'baixando':
      return { classe: 'ac', texto: `baixando ${Math.round(r.progresso || 0)}%` };
    case 'incompativel':
      return { classe: 'n', texto: patch ? `patch ${patch}` : 'patch antigo' };
    case 'indisponivel':
      return { classe: 'cr', texto: 'indisponível' };
  }
  return { classe: 'n', texto: 'sem estado' };
}

// contagem: o resumo em cima da lista ("19 partidas · 12 dá para ver · 7 de patch antigo").
export function contagem(partidas) {
  let daParaVer = 0;
  let antigas = 0;
  for (const p of partidas) {
    const e = p.replay?.estado;
    if (e === 'baixado' || e === 'baixavel' || e === 'baixando') daParaVer++;
    else if (e === 'incompativel') antigas++;
  }
  return { total: partidas.length, daParaVer, antigas };
}

// plural simples para os rótulos.
export function plural(n, um, varios) {
  return `${n} ${n === 1 ? um : varios}`;
}
