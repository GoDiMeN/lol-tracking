// O placar dos times no mapa com play: abates, ouro, estruturas e objetivos de cada time no
// instante t. Puro (placar.test.mjs): recebe o que a tela já buscou (participantes, eventos de
// GET /api/partidas/{id}/eventos, a linha do tempo crua do LCU) e não toca no DOM nem na rede.
// Nada é inventado: o que não tem fonte fica nulo e a tela mostra "—".

export const TIMES = [100, 200];

// Tipos do LCU (monsterSubType) → nome curto e nome na tela.
export const DRAGOES = {
  FIRE_DRAGON: { curto: 'inf', nome: 'infernal' },
  WATER_DRAGON: { curto: 'oce', nome: 'do oceano' },
  EARTH_DRAGON: { curto: 'mon', nome: 'da montanha' },
  AIR_DRAGON: { curto: 'nuv', nome: 'das nuvens' },
  HEXTECH_DRAGON: { curto: 'hex', nome: 'hextec' },
  CHEMTECH_DRAGON: { curto: 'qui', nome: 'quimtec' },
};

// preparaPlacar lê uma vez o que não muda com o tempo: o time de cada um, os objetivos e as
// estruturas em ordem, as mortes e o ouro dos times por minuto.
export function preparaPlacar({ participantes = [], eventos = [], linhaDoTempo = null }) {
  const timeDe = {};
  for (const p of participantes) timeDe[p.participante_id] = p.time;
  // O time de quem pegou: o de quem deu o último golpe; se foi um monstro/tropa (killerId 0),
  // o dos que ajudaram (a linha do tempo diz quem ajudou mesmo sem matador).
  const timeDoEvento = (e) => {
    if (e.quem_id && timeDe[e.quem_id]) return timeDe[e.quem_id];
    const aj = e.assistencias || e.dados?.assistingParticipantIds || [];
    for (const a of aj) if (timeDe[a]) return timeDe[a];
    return null;
  };
  const objetivos = [];
  const estruturas = [];
  const mortes = [];
  for (const e of eventos) {
    const d = e.dados || {};
    if (e.tipo === 'monstro') {
      let tipo = { DRAGON: 'dragao', BARON_NASHOR: 'barao', RIFTHERALD: 'arauto', HORDE: 'vastilarvas', ATAKHAN: 'atakhan' }[d.monsterType];
      if (!tipo) continue;
      if (tipo === 'dragao' && d.monsterSubType === 'ELDER_DRAGON') tipo = 'anciao';
      objetivos.push({ t_ms: e.t_ms, tipo, sub: d.monsterSubType || '', time: timeDoEvento(e), quem_id: e.quem_id || null });
    } else if (e.tipo === 'estrutura') {
      // teamId é o time DONO da estrutura derrubada: o ponto é do outro.
      const dono = d.teamId === 100 || d.teamId === 200 ? d.teamId : null;
      estruturas.push({
        t_ms: e.t_ms,
        tipo: d.buildingType === 'INHIBITOR_BUILDING' ? 'inibidor' : 'torre',
        dono,
        time: dono ? 300 - dono : timeDoEvento(e),
        rota: d.laneType || '',
        nivel: d.towerType || '',
      });
    } else if (e.tipo === 'abate') {
      mortes.push({ t_ms: e.t_ms, pid: e.em_quem_id, quem_id: e.quem_id || null });
    }
  }
  const porT = (a, b) => a.t_ms - b.t_ms;
  objetivos.sort(porT);
  estruturas.sort(porT);
  mortes.sort(porT);
  return { timeDe, objetivos, estruturas, mortes, ouro: ouroPorMinuto(linhaDoTempo, timeDe) };
}

// ouroPorMinuto: [{t_ms, 100: soma, 200: soma}] pelos quadros da linha do tempo (totalGold de
// cada um). Sem a linha do tempo, lista vazia.
export function ouroPorMinuto(linhaDoTempo, timeDe) {
  const r = [];
  for (const f of linhaDoTempo?.frames || []) {
    const soma = { 100: 0, 200: 0 };
    let tem = false;
    for (const [pid, pf] of Object.entries(f.participantFrames || {})) {
      const time = timeDe[pid];
      if (!time || typeof pf.totalGold !== 'number') continue;
      soma[time] += pf.totalGold;
      tem = true;
    }
    if (tem) r.push({ t_ms: f.timestamp, 100: soma[100], 200: soma[200] });
  }
  return r.sort((a, b) => a.t_ms - b.t_ms);
}

// ouroNoInstante: o ouro dos dois times em t, interpolado em linha reta entre os dois minutos
// vizinhos (o ouro só existe por minuto). Depois do último quadro, o último. null sem dado.
export function ouroNoInstante(ouro, t) {
  if (!ouro.length) return null;
  if (t <= ouro[0].t_ms) return { 100: ouro[0][100], 200: ouro[0][200] };
  for (let i = 1; i < ouro.length; i++) {
    const a = ouro[i - 1];
    const b = ouro[i];
    if (t <= b.t_ms) {
      const f = (t - a.t_ms) / Math.max(1, b.t_ms - a.t_ms);
      return { 100: a[100] + (b[100] - a[100]) * f, 200: a[200] + (b[200] - a[200]) * f };
    }
  }
  const u = ouro[ouro.length - 1];
  return { 100: u[100], 200: u[200] };
}

// Duração dos bônus com prazo (docs: valores em bonus.js, com a fonte).
export const DURACAO_BARAO_MS = 180000;
export const DURACAO_ANCIAO_MS = 150000;

// placarNoInstante: o placar dos dois times em t. `jogadores` = os 10 do quadro de estados
// mais perto de t (K/D/A de cada um); sem ele, os abates saem dos eventos.
export function placarNoInstante(prep, t, jogadores = null) {
  const vazio = () => ({
    abates: 0,
    cs: null,
    nivel_medio: null,
    ouro: null,
    torres: 0,
    inibidores: 0,
    dragoes: [],
    alma: null,
    anciao: 0,
    vastilarvas: 0,
    arauto: 0,
    barao: 0,
    atakhan: 0,
    barao_ate_ms: null,
    anciao_ate_ms: null,
  });
  const r = { 100: vazio(), 200: vazio(), sem_time: 0 };
  if (jogadores && jogadores.length) {
    // CS e nível também só existem no quadro (sem ele ficam nulos: "—").
    for (const time of TIMES) {
      r[time].cs = 0;
      r[time].nivel_medio = null;
    }
    const niveis = { 100: [], 200: [] };
    for (const j of jogadores) {
      const time = prep.timeDe[j.participante_id];
      if (!time) continue;
      r[time].abates += j.abates || 0;
      r[time].cs += j.cs || 0;
      if (j.nivel) niveis[time].push(j.nivel);
    }
    for (const time of TIMES) {
      if (niveis[time].length) r[time].nivel_medio = niveis[time].reduce((s, n) => s + n, 0) / niveis[time].length;
    }
  } else {
    for (const m of prep.mortes) {
      if (m.t_ms > t) break;
      const time = prep.timeDe[m.quem_id];
      if (time) r[time].abates++;
    }
  }
  const ouro = ouroNoInstante(prep.ouro, t);
  if (ouro) {
    r[100].ouro = ouro[100];
    r[200].ouro = ouro[200];
  }
  for (const s of prep.estruturas) {
    if (s.t_ms > t) break;
    if (!s.time) continue;
    if (s.tipo === 'torre') r[s.time].torres++;
    else r[s.time].inibidores++;
  }
  for (const o of prep.objetivos) {
    if (o.t_ms > t) break;
    if (!o.time) {
      r.sem_time++;
      continue;
    }
    const x = r[o.time];
    switch (o.tipo) {
      case 'dragao':
        x.dragoes.push(o.sub);
        // A alma vem com o 4º dragão elemental do time, do tipo do mapa (o do 4º).
        if (x.dragoes.length === 4) x.alma = o.sub;
        break;
      case 'anciao':
        x.anciao++;
        x.anciao_ate_ms = o.t_ms + DURACAO_ANCIAO_MS;
        break;
      case 'barao':
        x.barao++;
        x.barao_ate_ms = o.t_ms + DURACAO_BARAO_MS;
        break;
      default:
        x[o.tipo]++;
    }
  }
  for (const time of TIMES) {
    if (r[time].barao_ate_ms !== null && r[time].barao_ate_ms <= t) r[time].barao_ate_ms = null;
    if (r[time].anciao_ate_ms !== null && r[time].anciao_ate_ms <= t) r[time].anciao_ate_ms = null;
  }
  return r;
}

// --- Ícones dos objetivos (assets/objetivos: SVG originais, não a arte do jogo) ---------------

const ICONE_DRAGAO = {
  FIRE_DRAGON: 'dragao-infernal',
  WATER_DRAGON: 'dragao-oceano',
  EARTH_DRAGON: 'dragao-montanha',
  AIR_DRAGON: 'dragao-nuvens',
  HEXTECH_DRAGON: 'dragao-hextec',
  CHEMTECH_DRAGON: 'dragao-quimtec',
  ELDER_DRAGON: 'dragao-anciao',
};

// iconeObjetivo: o arquivo do ícone de um objetivo ('torre', 'inibidor', 'dragao' com o subtipo
// do LCU, 'anciao', 'vastilarvas', 'arauto', 'barao'). Dragão sem tipo conhecido: o genérico.
export function iconeObjetivo(tipo, sub = '') {
  if (tipo === 'dragao') return `assets/objetivos/${ICONE_DRAGAO[sub] || 'dragao'}.svg`;
  if (tipo === 'anciao') return 'assets/objetivos/dragao-anciao.svg';
  return `assets/objetivos/${tipo}.svg`;
}

// --- Gráficos do placar (desenhados em SVG à mão: o projeto não usa biblioteca) -------------

// serieOuro: a diferença de ouro azul − vermelho em cada minuto da linha do tempo.
export function serieOuro(prep) {
  return prep.ouro.map((o) => ({ t_ms: o.t_ms, dif: o[100] - o[200] }));
}

// caminhosDiferenca: os caminhos SVG da área acima de zero (azul na frente) e abaixo (vermelho),
// num retângulo larg × alt com o zero no meio e ±maxAbs nas bordas. Onde a linha cruza o zero,
// entra o ponto exato do cruzamento (a cor não vaza para o outro lado).
export function caminhosDiferenca(serie, duracaoMs, larg, alt, maxAbs) {
  const zero = alt / 2;
  if (!serie.length || !(maxAbs > 0)) return { acima: '', abaixo: '', linha: '', zero };
  const X = (t) => (Math.min(duracaoMs, Math.max(0, t)) / Math.max(1, duracaoMs)) * larg;
  const Y = (d) => zero - (Math.max(-maxAbs, Math.min(maxAbs, d)) / maxAbs) * (alt / 2);
  const pts = [];
  for (let i = 0; i < serie.length; i++) {
    const p = serie[i];
    if (i > 0) {
      const a = serie[i - 1];
      if ((a.dif > 0 && p.dif < 0) || (a.dif < 0 && p.dif > 0)) {
        const f = a.dif / (a.dif - p.dif);
        pts.push({ x: X(a.t_ms + (p.t_ms - a.t_ms) * f), d: 0 });
      }
    }
    pts.push({ x: X(p.t_ms), d: p.dif });
  }
  const r = (v) => Math.round(v * 10) / 10;
  const area = (sinal) => {
    let s = `M${r(pts[0].x)},${r(zero)}`;
    for (const p of pts) s += ` L${r(p.x)},${r(Y(sinal > 0 ? Math.max(0, p.d) : Math.min(0, p.d)))}`;
    return `${s} L${r(pts[pts.length - 1].x)},${r(zero)} Z`;
  };
  const linha = pts.map((p, i) => `${i ? 'L' : 'M'}${r(p.x)},${r(Y(p.d))}`).join(' ');
  return { acima: area(1), abaixo: area(-1), linha, zero };
}

// escalaOuro: o maior |diferença| arredondado para cima em mil (mínimo 2 mil: um gráfico de
// partida equilibrada não vira ruído ampliado).
export function escalaOuro(serie) {
  const m = Math.max(0, ...serie.map((s) => Math.abs(s.dif)));
  return Math.max(2000, Math.ceil(m / 1000) * 1000);
}

// serieAbates: os abates acumulados de cada time a cada abate (degraus), começando em 0.
export function serieAbates(prep) {
  const r = [{ t_ms: 0, 100: 0, 200: 0 }];
  const c = { 100: 0, 200: 0 };
  for (const m of prep.mortes) {
    const time = prep.timeDe[m.quem_id];
    if (!time) continue;
    c[time]++;
    r.push({ t_ms: m.t_ms, 100: c[100], 200: c[200] });
  }
  return r;
}

// caminhoDegraus: a linha em degraus de uma série acumulada, até o fim da partida.
export function caminhoDegraus(serie, chave, duracaoMs, larg, alt, max) {
  const X = (t) => (Math.min(duracaoMs, Math.max(0, t)) / Math.max(1, duracaoMs)) * larg;
  const Y = (v) => alt - (v / Math.max(1, max)) * alt;
  const r = (v) => Math.round(v * 10) / 10;
  let s = '';
  let ant = 0;
  for (const p of serie) {
    s += s ? ` L${r(X(p.t_ms))},${r(Y(ant))} L${r(X(p.t_ms))},${r(Y(p[chave]))}` : `M${r(X(p.t_ms))},${r(Y(p[chave]))}`;
    ant = p[chave];
  }
  return `${s} L${r(larg)},${r(Y(ant))}`;
}

// marcasDoEixo: objetivos e estruturas para marcar no eixo do tempo do gráfico.
export function marcasDoEixo(prep) {
  return [
    ...prep.objetivos.map((o) => ({ t_ms: o.t_ms, time: o.time, tipo: o.tipo, sub: o.sub })),
    ...prep.estruturas.map((s) => ({ t_ms: s.t_ms, time: s.time, tipo: s.tipo, sub: '' })),
  ].sort((a, b) => a.t_ms - b.t_ms);
}

// --- Objetivo em andamento (das atividades: nenhuma regra nova) --------------------------------

// objetivosEmAndamento: os times com dois ou mais jogadores fazendo o MESMO objetivo (ou na
// mesma torre) agora. `trechos` = o trecho de atividade de cada jogador no instante.
export function objetivosEmAndamento(trechos, timeDe) {
  const grupos = new Map();
  for (const tr of trechos) {
    if (!tr || (tr.atividade !== 'objetivo' && tr.atividade !== 'torre') || !tr.alvo) continue;
    const time = timeDe[tr.participante_id];
    if (!time) continue;
    const k = `${time}|${tr.atividade}|${tr.alvo}`;
    if (!grupos.has(k)) grupos.set(k, { time, atividade: tr.atividade, alvo: tr.alvo, n: 0, ids: [] });
    const g = grupos.get(k);
    g.n++;
    g.ids.push(tr.participante_id);
  }
  return [...grupos.values()].filter((g) => g.n >= 2).sort((a, b) => b.n - a.n || a.time - b.time);
}

// textoEmAndamento: "azul fazendo o barão · 4 jogadores", "vermelho na torre do meio · 3 jogadores".
export function textoEmAndamento(g) {
  const time = g.time === 200 ? 'vermelho' : 'azul';
  const o = g.atividade === 'objetivo' ? `fazendo ${artigo(g.alvo)}${g.alvo}` : `derrubando ${artigo(g.alvo)}${g.alvo}`;
  return `${time} ${o} · ${g.n} jogadores`;
}
// Os alvos vêm do servidor ("barão", "dragão infernal", "vastilarvas", "torre do meio", "placa da
// torre de baixo", "inibidor de baixo").
const artigo = (alvo) => {
  if (/^(as|os|a|o) /.test(alvo)) return '';
  if (alvo.startsWith('vastilarvas')) return 'as ';
  if (alvo.startsWith('torre') || alvo.startsWith('placa')) return 'a ';
  return 'o ';
};

// faseNoInstante: a fase (GET /api/partidas/{id}/fases) que contém t, ou null.
export function faseNoInstante(fases, t) {
  for (const f of fases || []) {
    if (t >= f.de_ms && (f.ate_ms === null || f.ate_ms === undefined || t < f.ate_ms)) return f;
  }
  return null;
}

export const NOMES_FASE = { rotas: 'fase de rotas', meio: 'meio de jogo', final: 'fim de jogo' };

// bonusComPrazo: se o jogador `pid` tem o bônus que o time pegou em `tObjetivo` e dura
// `duracao` ms, no instante t. Só recebe quem estava vivo na hora, e perde ao morrer (Barão e
// Ancião funcionam assim). Pelas mortes dos eventos: 'ativo', 'perdeu' (morreu depois),
// 'incerto' (morreu pouco antes do objetivo: pode ter estado morto na hora), 'acabou'.
export const JANELA_MORTO_NA_HORA_MS = 75000; // tempo de renascer do meio/fim de jogo, folgado
export function bonusComPrazo(prep, pid, tObjetivo, duracao, t) {
  if (t >= tObjetivo + duracao) return 'acabou';
  let ultimaMorteAntes = null;
  for (const m of prep.mortes) {
    if (m.pid !== pid) continue;
    if (m.t_ms <= tObjetivo) ultimaMorteAntes = m.t_ms;
    else if (m.t_ms <= t) return 'perdeu';
  }
  // Os eventos não dizem quando cada um renasceu: quem morreu pouco antes do objetivo pode
  // estar morto na hora (e não recebe). Aí não dá para afirmar.
  if (ultimaMorteAntes !== null && tObjetivo - ultimaMorteAntes < JANELA_MORTO_NA_HORA_MS) return 'incerto';
  return 'ativo';
}
