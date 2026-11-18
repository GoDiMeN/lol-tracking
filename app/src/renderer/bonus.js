// Os bônus que os objetivos neutros dão ao time e quanto cada um representa para UM jogador,
// com os atributos lidos da tela naquele instante. Puro (placar.test.mjs).
//
// VALORES: dependem do patch. Pesquisados em 17/11/2026 para o patch 26.22 (a versão do jogo
// que o LCU grava é "16.22": desde 2025 a Riot chama o patch pelo ano, 16.x = 26.x). O 26.22 não
// mexe em objetivos (wiki.leagueoflegends.com/en-us/V26.22); a última revisão grande foi o 26.1
// (leagueoflegends.com/en-us/news/game-updates/patch-26-1-notes/). Fontes de cada linha:
//   dragões e almas: wiki.leagueoflegends.com/en-us/Dragon_Slayer (valores do V13.20)
//   Barão: wiki.leagueoflegends.com/en-us/Hand_of_Baron (DA/PH do V9.2, duração do V9.23)
//   Ancião: wiki.leagueoflegends.com/en-us/Aspect_of_the_Dragon (V9.24b)
//   vastilarvas: wiki.leagueoflegends.com/en-us/Touch_of_the_Void e /Hunger_of_the_Void
//   Arauto: wiki.leagueoflegends.com/en-us/Rift_Herald; Atakhan: removido no 26.1.
// Onde a fonte não deixa o número seguro, `por_acumulo` fica nulo e a tela mostra só a
// descrição. Patch novo = conferir esta tabela de novo.
//
// MEDIDO: infernal, montanha e nuvens também são medidos no próprio dado (o degrau do atributo
// no instante da queda, GET /api/partidas/{id}/bonus-objetivos). Com a medição (`medidos` em
// bonusDoJogador), o número do jogador sai dela; sem ela, cai na tabela; sem nenhum dos dois, só
// a descrição.

export const PATCH_DOS_BONUS = '26.22';

// patchDaVersao: "16.22" (versão do jogo, campo `patch` da partida) → "26.22".
export function patchDaVersao(v) {
  const m = /^(\d+)\.(\d+)/.exec(String(v || ''));
  if (!m) return '';
  const maior = Number(m[1]);
  return maior >= 15 && maior < 25 ? `${maior + 10}.${m[2]}` : `${maior}.${m[2]}`;
}

// Acúmulos de dragão elemental (um por dragão do time, soma linear, até 4).
export const BONUS_DRAGAO = {
  FIRE_DRAGON: {
    nome: 'Dragão infernal',
    efeito: (n) => `+${3 * n}% de dano de ataque e de poder de habilidade`,
    // % do TOTAL (multiplicativo): a parte do bônus no valor atual é atual × p/(1+p).
    impacto: (n, j, m) => [
      parteDe('dano de ataque', j.atributos?.ad, (m.ad ?? 0.03) * n, m.ad !== undefined),
      parteDe('poder de habilidade', j.atributos?.ap, (m.ap ?? 0.03) * n, m.ap !== undefined),
    ],
  },
  WATER_DRAGON: {
    nome: 'Dragão do oceano',
    efeito: (n) => `recupera ${2 * n}% da vida perdida a cada 5 s`,
    impacto: (n, j) => {
      if (j.vida === null || j.vida === undefined || !j.vida_max) return [null];
      const perdida = Math.max(0, j.vida_max - j.vida);
      return [{ texto: `${Math.round(perdida * 0.02 * n)} de vida a cada 5 s agora (perdeu ${Math.round(perdida)})`, exato: true }];
    },
  },
  EARTH_DRAGON: {
    nome: 'Dragão da montanha',
    efeito: (n) => `+${5 * n}% de armadura e de resistência mágica`,
    // A fonte não diz se é do total ou só do bônus; o degrau medido na 3304217746 (≈ 5% do
    // TOTAL) resolve: com a medição, há número; sem ela, só a descrição.
    impacto: (n, j, m) =>
      m.armadura === undefined && m.rm === undefined
        ? [null]
        : [parteDe('armadura', j.atributos?.armadura, (m.armadura ?? m.rm) * n, true), parteDe('resistência mágica', j.atributos?.rm, (m.rm ?? m.armadura) * n, true)],
    sem_numero: 'a fonte não diz se o % é sobre o total ou só sobre o bônus, e não houve medição confiável nesta partida',
  },
  AIR_DRAGON: {
    nome: 'Dragão das nuvens',
    efeito: (n) => `+${5 * n}% de velocidade de movimento fora de combate (e de resistência a lentidão)`,
    // Soma com os outros % de velocidade do campeão, que a tela não separa; com a medição do
    // degrau (primeiro trecho fora de luta depois da queda) dá uma estimativa.
    impacto: (n, j, m) => {
      if (m.vel_mov === undefined) return [null];
      const v = j.atributos?.vel_mov;
      if (v === null || v === undefined || !(v > 0)) return [null];
      return [{ texto: `fora de combate, ≈ ${Math.round((v * m.vel_mov * n) / (1 + m.vel_mov * n))} da sua velocidade (${Math.round(v)}) vem daí (medido nesta partida)`, exato: false }];
    },
    sem_numero: 'soma com os outros % de velocidade do campeão, que a tela não separa, e não houve medição confiável nesta partida',
  },
  HEXTECH_DRAGON: {
    nome: 'Dragão hextec',
    efeito: (n) => `+${5 * n} de aceleração de habilidade e +${5 * n}% de velocidade de ataque bônus`,
    impacto: (n) => [{ texto: `+${5 * n} de aceleração de habilidade (valor fixo)`, exato: true }],
  },
  CHEMTECH_DRAGON: {
    nome: 'Dragão quimtec',
    efeito: (n) => `+${6 * n}% de tenacidade e de poder de cura e escudo`,
    impacto: () => [null],
    sem_numero: 'tenacidade e cura/escudo não aparecem nos atributos lidos da tela',
  },
};

// Almas (com o 4º dragão elemental do time).
export const ALMAS = {
  FIRE_DRAGON: { nome: 'Alma infernal', efeito: 'a cada 3 s, o próximo golpe explode: 100 + 22,5% do DA bônus + 13,5% do PH + 2,75% da vida bônus', ap: 0.135, base: 100 },
  WATER_DRAGON: { nome: 'Alma do oceano', efeito: 'causar dano cura 150 + 26% do DA bônus + 17% do PH + 7% da vida bônus e devolve 100 + 3,5% da mana máxima, em 4 s (30% contra tropas e monstros)', ap: 0.17, base: 150 },
  EARTH_DRAGON: { nome: 'Alma da montanha', efeito: 'depois de 5 s sem tomar dano, escudo de 220 + 16% do DA bônus + 12% do PH + 12% da vida bônus', ap: 0.12, base: 220 },
  AIR_DRAGON: { nome: 'Alma das nuvens', efeito: '+15% de velocidade de movimento; 60% por 6 s depois da ultimate (recarga de 30 s)' },
  HEXTECH_DRAGON: { nome: 'Alma hextec', efeito: 'golpes causam 25–53 de dano verdadeiro (pelo nível) e uma lentidão que salta para até 3 inimigos (recarga de 8 s)' },
  CHEMTECH_DRAGON: { nome: 'Alma quimtec', efeito: 'abaixo de 50% da vida: +13% de dano causado e 13% de redução de dano' },
};

// Barão: DA e PH fixados no minuto em que ele morre (tabela da wiki, de 2 em 2 minutos).
export const TABELA_BARAO = [
  [20, 12, 20], [22, 14, 23], [24, 16, 27], [26, 19, 32], [28, 22, 37], [30, 26, 43],
  [32, 30, 50], [34, 34, 57], [36, 39, 65], [38, 43, 72], [40, 48, 80],
];

// baraoNoMinuto: {ad: [min, max], ap: [min, max]} para o Barão morto em t. Entre dois pontos da
// tabela a fonte não dá a conta: devolve os dois vizinhos como faixa.
export function baraoNoMinuto(tMs) {
  const m = tMs / 60000;
  const tab = TABELA_BARAO;
  if (m <= tab[0][0]) return { ad: [tab[0][1], tab[0][1]], ap: [tab[0][2], tab[0][2]] };
  for (let i = 1; i < tab.length; i++) {
    if (m < tab[i][0]) return { ad: [tab[i - 1][1], tab[i][1]], ap: [tab[i - 1][2], tab[i][2]] };
  }
  const u = tab[tab.length - 1];
  return { ad: [u[1], u[1]], ap: [u[2], u[2]] };
}

function parteDe(nome, atual, p, medido = false) {
  if (atual === null || atual === undefined || !(atual > 0)) return null;
  const artigo = nome === 'resistência mágica' || nome === 'armadura' ? 'da sua' : 'do seu';
  return { texto: `≈ ${Math.round((atual * p) / (1 + p))} ${artigo} ${nome} (${Math.round(atual)}) vem daí${medido ? ' (medido nesta partida)' : ''}`, exato: false };
}

// medidosDoTipo: {atributo: fração por acúmulo} do resumo da medição (só o medido com confiança).
export function medidosDoTipo(medidos, sub) {
  const r = {};
  for (const m of medidos || []) {
    if (m.sub === sub && m.medido !== null && m.medido !== undefined && m.confianca !== 'nenhuma') r[m.atributo] = m.medido;
  }
  return r;
}

const NOME_ATR = { ad: 'DA', ap: 'PH', armadura: 'armadura', rm: 'RM', vel_mov: 'velocidade', vel_ataque: 'vel. de ataque' };

const faixa = ([a, b]) => (a === b ? `${a}` : `${a}–${b}`);

// bonusDoJogador: a lista de blocos {titulo, efeito, impacto: [{texto, exato}], aviso} para o
// jogador `j` (do quadro de estados) do time `time`, com o placar do time em t (placar.js) e o
// estado do Barão/Ancião dele ('ativo' | 'perdeu' | 'nao_recebeu' | 'incerto').
export function bonusDoJogador({ j, placarTime, estadoBarao = null, estadoAnciao = null, tBarao = null, medidos = null }) {
  const blocos = [];
  const porTipo = {};
  for (const s of placarTime.dragoes) porTipo[s] = (porTipo[s] || 0) + 1;
  for (const [sub, n] of Object.entries(porTipo)) {
    const b = BONUS_DRAGAO[sub];
    if (!b) {
      blocos.push({ titulo: `Dragão (${sub || 'tipo desconhecido'}) × ${n}`, efeito: 'tipo sem valor na tabela', impacto: [] });
      continue;
    }
    const m = medidosDoTipo(medidos, sub);
    const imp = b.impacto(n, j, m).filter(Boolean);
    const medida = Object.entries(m).map(([a, p]) => `${NOME_ATR[a] || a} +${(p * 100).toLocaleString('pt-BR', { maximumFractionDigits: 2 })}%`);
    const efeito = b.efeito(n) + (medida.length ? ` · medido nesta partida, por dragão: ${medida.join(', ')}` : '');
    blocos.push({ titulo: `${b.nome} × ${n}`, efeito, impacto: imp, aviso: imp.length ? '' : b.sem_numero || 'sem número para este jogador' });
  }
  if (placarTime.alma) {
    const a = ALMAS[placarTime.alma];
    if (a) {
      const imp = [];
      const ap = j.atributos?.ap;
      if (a.ap && ap !== null && ap !== undefined) {
        imp.push({ texto: `${a.base} + ${Math.round(ap * a.ap)} pelo seu PH (${Math.round(ap)}), mais a parte do DA e da vida bônus, que a tela não separa`, exato: false });
      }
      blocos.push({ titulo: a.nome, efeito: a.efeito, impacto: imp, aviso: imp.length ? '' : 'sem número para este jogador' });
    }
  }
  if (estadoBarao && estadoBarao !== 'acabou' && tBarao !== null) {
    const v = baraoNoMinuto(tBarao);
    const efeito = `+${faixa(v.ad)} de dano de ataque e +${faixa(v.ap)} de poder de habilidade por 3 min, recall pela metade do tempo e tropas por perto mais fortes`;
    const estado = { ativo: 'ativo', perdeu: 'perdeu (morreu depois)', nao_recebeu: 'não recebeu', incerto: 'pode estar ativo (morreu pouco antes do Barão)' }[estadoBarao];
    blocos.push({
      titulo: `Barão: ${estado}`,
      efeito,
      impacto: estadoBarao === 'ativo' || estadoBarao === 'incerto' ? [{ texto: `+${faixa(v.ad)} DA e +${faixa(v.ap)} PH já estão nos atributos acima`, exato: false }] : [],
      aviso: v.ad[0] === v.ad[1] ? '' : 'a fonte dá a tabela de 2 em 2 minutos: a faixa são os dois pontos vizinhos',
    });
  }
  if (estadoAnciao && estadoAnciao !== 'acabou') {
    const estado = { ativo: 'ativo', perdeu: 'perdeu (morreu depois)', nao_recebeu: 'não recebeu', incerto: 'pode estar ativo' }[estadoAnciao];
    blocos.push({ titulo: `Dragão ancião: ${estado}`, efeito: 'por 150 s, golpes queimam (dano verdadeiro, cresce com o tempo de jogo) e executam campeões abaixo de 20% da vida', impacto: [] });
  }
  if (placarTime.vastilarvas > 0) {
    const n = placarTime.vastilarvas;
    blocos.push({
      titulo: `Vastilarvas × ${n}`,
      efeito: `ataques em estruturas queimam com dano verdadeiro (mais com mais acúmulos)${n >= 3 ? '; com 3, invoca um ácaro do Vazio ao lutar com estruturas' : ''}`,
      impacto: [],
      aviso: 'o valor por acúmulo não ficou seguro na fonte: sem número',
    });
  }
  if (placarTime.arauto > 0) {
    blocos.push({ titulo: 'Arauto do Vale', efeito: 'não dá bônus ao campeão: só o Olho do Arauto, para investir numa torre', impacto: [] });
  }
  return blocos;
}
