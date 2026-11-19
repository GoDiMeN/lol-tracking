// Contas e textos do aprendizado por elo, sem DOM (testáveis no node). A tela está em
// views/aprendizado.js; as regras do servidor, em docs/arquitetura/contrato-http.md.

export const ELOS = [
  { elo: 'SILVER', nome: 'Prata' },
  { elo: 'GOLD', nome: 'Ouro' },
  { elo: 'PLATINUM', nome: 'Platina' },
  { elo: 'EMERALD', nome: 'Esmeralda' },
  { elo: 'DIAMOND', nome: 'Diamante' },
  { elo: 'MASTER', nome: 'Mestre', mono: true },
  { elo: 'GRANDMASTER', nome: 'Grão-Mestre', mono: true },
  { elo: 'CHALLENGER', nome: 'Desafiante', mono: true },
];

export const nomeElo = (elo) => ELOS.find((e) => e.elo === elo)?.nome || elo;

// Medida de referência: um passe leva ~7,5 min a 4× (partida de ~30 min + abrir e fechar o replay).
export const MINUTOS_POR_PASSE_A_4X = 7.5;
export const JOGADORES_POR_ELO = 10;

// estimativaColeta: passes e segundos de uma coleta nova (antes de descobrir os jogadores).
export function estimativaColeta({ elos, partidasPorJogador = 1, quem = 'jogador', velocidade = 4, jogadoresPorElo = JOGADORES_POR_ELO }) {
  const n = Array.isArray(elos) ? elos.length : Number(elos) || 0;
  const passes = n * (Number(jogadoresPorElo) || JOGADORES_POR_ELO) * partidasPorJogador * (quem === 'todos' ? 10 : 1);
  const segundos = passes * MINUTOS_POR_PASSE_A_4X * 60 * (4 / (Number(velocidade) || 4));
  return { passes, segundos };
}

export const ESTADOS_COLETA = {
  rodando: { classe: 'ac', texto: 'coletando' },
  pausado: { classe: 'at', texto: 'pausada' },
  concluido: { classe: 'ok', texto: 'concluída' },
  cancelado: { classe: 'n', texto: 'cancelada' },
  falhou: { classe: 'cr', texto: 'parou com erro' },
};

export const ESTADOS_PARTIDA = {
  pendente: { classe: 'n', texto: 'na fila' },
  baixando: { classe: 'ac', texto: 'baixando o replay' },
  rastreando: { classe: 'ac', texto: 'rastreando' },
  pausada: { classe: 'at', texto: 'pausada' },
  concluida: { classe: 'ok', texto: 'pronta' },
  // Sobrou passe ruim (também na refação automática): não vale até refazer.
  incompleta: { classe: 'at', texto: 'incompleta' },
  erro: { classe: 'cr', texto: 'não deu' },
  cancelada: { classe: 'n', texto: 'cancelada' },
};

// textoRefazer: o rótulo do botão da partida incompleta (o mesmo "refazer N passes ruins" da tela
// da partida).
export function textoRefazer(n) {
  return `refazer ${n} ${n === 1 ? 'passe ruim' : 'passes ruins'}`;
}

export const ESTADOS_ELO = {
  pendente: { classe: 'n', texto: 'esperando' },
  descobrindo: { classe: 'ac', texto: 'procurando jogadores' },
  pronto: { classe: 'ok', texto: 'jogadores escolhidos' },
};

const DESCARTES = {
  inativo: ['parado', 'parados'],
  veterano: ['travado há muito tempo na liga', 'travados há muito tempo na liga'],
  poucas_partidas: ['com poucas partidas', 'com poucas partidas'],
  travado: ['com muitas partidas e pouca vitória', 'com muitas partidas e pouca vitória'],
  nao_achado: ['não achado no cliente', 'não achados no cliente'],
  nao_mono: ['não joga um campeão só', 'não jogam um campeão só'],
  sem_historico: ['sem histórico', 'sem histórico'],
  sem_partida: ['sem partida rastreável', 'sem partida rastreável'],
  outra_funcao: ['de outra função', 'de outra função'],
  rota_completa: ['de rota já completa', 'de rota já completa'],
};

// estimativaEstudo: segundos para rastrear n partidas de um estudo (só o jogador: um passe por partida).
export function estimativaEstudo(n, velocidade = 4) {
  return (Number(n) || 0) * MINUTOS_POR_PASSE_A_4X * 60 * (4 / (Number(velocidade) || 4));
}

// Funções (posições) na ordem do mapa, com o nome da tela.
export const FUNCOES = [
  ['TOP', 'topo'],
  ['JUNGLE', 'selva'],
  ['MIDDLE', 'meio'],
  ['BOTTOM', 'atirador'],
  ['UTILITY', 'suporte'],
];

// filtraFuncao: os jogadores de uma função ('' = todos).
export const filtraFuncao = (jogadores, funcao) => (funcao ? jogadores.filter((j) => j.funcao === funcao) : jogadores);

// destaques: em cada elo, entre os jogadores dados (já filtrados), quem é "o que mais está subindo" (o
// primeiro na ordem de subida) e "o recém-chegado que mais sobe" (o primeiro com freshBlood). Devolve
// Map id → {maisSobe, recemChegado}.
export function destaques(jogadores) {
  const m = new Map();
  const primeiro = new Map();
  const primeiroNovo = new Map();
  for (const j of jogadores) {
    if (j.estado === 'sem_partida') continue;
    const p = primeiro.get(j.elo);
    if (!p || j.posicao < p.posicao) primeiro.set(j.elo, j);
    if (j.fresh_blood) {
      const q = primeiroNovo.get(j.elo);
      if (!q || j.posicao < q.posicao) primeiroNovo.set(j.elo, j);
    }
  }
  for (const j of jogadores) m.set(j.id, { maisSobe: primeiro.get(j.elo) === j, recemChegado: primeiroNovo.get(j.elo) === j });
  return m;
}

// numerosLiga: "53 V · 31 D · 63%".
export function numerosLiga(j) {
  return `${j.vitorias} V · ${j.derrotas} D · ${porcento(j.taxa_vitoria)}`;
}

// textoDescartes: "3 não jogam um campeão só · 1 parado" (só os que aconteceram).
export function textoDescartes(descartes = {}) {
  return Object.entries(DESCARTES)
    .filter(([k]) => descartes[k] > 0)
    .map(([k, [um, varios]]) => `${descartes[k]} ${descartes[k] === 1 ? um : varios}`)
    .join(' · ');
}

// fracao: 0,75 → "75%".
export const porcento = (f) => `${Math.round((Number(f) || 0) * 100)}%`;

// matrizRotas: a matriz elo × rota da coleta (servidor: elos[].funcoes, achados/alvo por rota). Com cota,
// as colunas são as rotas pedidas; sem cota, as cinco (alvo 0: só os achados). Cada célula: texto
// "1/2" (+ " · 1 aprov." quando houve aproveitado) e se a cota encheu.
export function matrizRotas(d) {
  const comCota = (d?.coleta?.funcoes || []).length > 0;
  const colunas = comCota ? d.coleta.funcoes : FUNCOES.map(([f]) => f);
  const linhas = (d?.elos || []).map((e) => {
    const por = new Map((e.funcoes || []).map((c) => [c.funcao, c]));
    const celulas = colunas.map((f) => {
      const c = por.get(f) || { achados: 0, alvo: 0, aproveitados: 0 };
      const base = comCota ? `${c.achados}/${c.alvo}` : String(c.achados);
      return { funcao: f, texto: c.aproveitados ? `${base} · ${c.aproveitados} aprov.` : base, cheia: comCota && c.achados >= c.alvo };
    });
    return { elo: e.elo, nome: e.nome || nomeElo(e.elo), celulas, aproveitados: e.aproveitados || 0 };
  });
  return { colunas, comCota, linhas };
}

// pedidoRotas: o pedaço do POST da coleta que diz as rotas. Nenhuma marcada = sem cota (qualquer
// função até jogadores_por_elo); com rotas, a cota de jogadoresPorRota em cada uma.
export function pedidoRotas(rotas, jogadoresPorRota) {
  const l = FUNCOES.map(([f]) => f).filter((f) => rotas.has(f));
  if (!l.length) return {};
  return { funcoes: l, jogadores_por_funcao: Number(jogadoresPorRota) || 1 };
}

// textoPontuacao: "71,5 = 61,5 do jogador + 10 de 1 aproveitável" (os dois componentes da partida).
export function textoPontuacao(p) {
  const n = (v) => String(Math.round((Number(v) || 0) * 10) / 10).replace('.', ',');
  if (!p?.aproveitaveis) return n(p?.pontuacao);
  return `${n(p.pontuacao)} = ${n(p.pontuacao_jogador)} do jogador + ${n(p.pontuacao_aproveitaveis)} de ${p.aproveitaveis} ${p.aproveitaveis === 1 ? 'aproveitável' : 'aproveitáveis'}`;
}
