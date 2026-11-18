// Onde desenhar cada retrato no mapa com play: o "rótulo com linha de chamada" dos mapas de
// transmissão. Puro (valida-desenho.test.mjs, valida-desenho.js): a tela e o validador usam ESTA
// função.
//
// Regras do dono (2026-11-17, noite; 7:28 da 3304217746: Senna e Draven um em cima do outro na rota
// de baixo, "fica toda hora girando"). O desenho de antes guardava um ÂNGULO por jogador que girava
// devagar atrás da direção real: os dois retratos orbitavam os anéis, e com o deslocamento curto
// (~0,8 retrato) cada retrato ficava em cima do anel do outro, que por isso não aparecia. Agora:
//   1. sozinho: retrato em cima do lugar verdadeiro, sem anel nem traço;
//   2. COLISÃO = retrato sobre retrato OU retrato sobre anel (o lugar real de qualquer jogador vivo,
//      inclusive o dele mesmo quando deslocado). Quem colide sai do lugar: ganha um ANEL no lugar
//      exato e o retrato vai para fora, ligado ao anel por um traço;
//   3. a DIREÇÃO é escolhida uma vez, ao sair, olhando os vizinhos (retratos e anéis, onde estão e
//      para onde estão andando: o lado com menos chance de bater em alguém), e fica TRAVADA: o
//      retrato só muda de direção se a posição travada passar a colidir de verdade por um tempo, e
//      nunca antes de TrocaMinimaMs de tela da escolha anterior;
//   4. HISTERESE NO TEMPO: depois que a colisão acaba, o retrato continua deslocado por SeguraMs de
//      TELA (vale igual a 1× e a 8×) e só volta se o lugar verdadeiro estiver livre com folga maior
//      que a de entrada, agora e no futuro próximo (pelas velocidades);
//   5. o deslocamento é um VETOR em relação ao anel: o jogador anda e o retrato translada junto,
//      sem girar em volta do anel; o comprimento muda devagar e só encolhe com sobra.

// Parâmetros, em múltiplos do lado do retrato (`ret`, pixels) e em ms de TELA.
export const PARAMS = {
  junta: 1.05, // dois retratos (no lugar real) a menos disso colidem (distância com escalaY)
  solta: 1.8, // para VOLTAR ao lugar real, os outros retratos a mais disso
  escalaY: 0.7, // a etiqueta embaixo deixa o retrato "mais alto": encurta a distância vertical
  espaco: 1.15, // distância mínima entre os centros de dois retratos deslocados
  folgaAnel: 0.62, // metade do retrato + o anel: retrato mais perto que isso de um anel o cobre
  folgaAnelSai: 0.95, // para voltar, todo anel a mais que isso do lugar real
  rotulo: 0.5, // a etiqueta embaixo do retrato também cobre anel (altura, em ret)
  minDesvio: 0.85, // ret: o retrato deslocado fica no mínimo a isso do próprio anel
  maxDesvio: 4.5, // ret: o deslocamento nunca passa disso (cinco na fonte, no canto do mapa, precisam)
  tau: 260, // ms de tela: constante de tempo da suavização do deslocamento
  vmax: 4, // ret por segundo de tela: velocidade máxima do retrato em relação ao anel
  vmin: 2.5, // ret por segundo de tela: e a mínima, enquanto não chegou
  seguraMs: 4000, // depois da última colisão, fica deslocado por isso (tela)
  reincideMs: 8000, // voltou há menos disso e bateu de novo: segura o dobro
  longe: 2.2, // ret: para voltar, nenhum anel a menos disso (com escalaY)
  previsaoMs: 1200, // ms de tela à frente para "vai bater" (pelas velocidades dos anéis)
  voltaMs: 2400, // para voltar ao lugar real, ele tem de seguir livre por isso à frente
  bloqueioMs: 200, // a posição travada colidindo por isso → escolhe outra direção
  cresce: 0.8, // ret: precisar crescer mais que isso de uma vez conta como bloqueada
  trocaMinimaMs: 3000, // entre duas TROCAS de direção de um jogador, no mínimo isso
  encolhe: 0.5, // ret: o comprimento só encolhe quando o necessário fica menor por mais que isso
  encolheMs: 2000, // ... por isso de tela seguidos, e livre também daqui a previsaoMs
  direcoes: 16, // direções candidatas
  antecipaMs: 600, // ms de tela: o lugar do retrato tem de seguir livre por isso à frente
  sobra: 0, // ret: ao crescer, cresce isso além do necessário (0,4 dava trancos)
  zonaMorta: 0.2, // ret: mudança do comprimento menor que isso não move o retrato
};

// escalaDoGrupo: com muitos juntos os retratos diminuem um pouco (cabe mais sem afastar tanto).
export function escalaDoGrupo(n) {
  if (n >= 8) return 0.74;
  if (n >= 5) return 0.86;
  return 1;
}

// espalha: ângulos (já em ordem circular) afastados até `gap` entre vizinhos, SEM trocar a ordem.
// (Não é mais usado pelo arranjo; fica para quem precisar de um leque estável.)
export function espalha(angulos, gap, iteracoes = 40) {
  const n = angulos.length;
  const a = angulos.slice();
  if (n < 2) return a;
  for (let it = 0; it < iteracoes; it++) {
    let mexeu = false;
    for (let i = 0; i < n; i++) {
      const j = (i + 1) % n;
      const g = difAngPos(a[j], a[i]);
      if (g >= gap - 1e-6) continue;
      const falta = gap - g;
      let mi = falta / 2;
      let mj = falta / 2;
      if (n >= 3) {
        const folgaI = difAngPos(a[i], a[(i - 1 + n) % n]) / 2;
        const folgaJ = difAngPos(a[(j + 1) % n], a[j]) / 2;
        mi = Math.max(0, Math.min(mi, folgaI));
        mj = Math.max(0, Math.min(mj, folgaJ));
      }
      a[i] -= mi;
      a[j] += mj;
      if (mi + mj > 1e-6) mexeu = true;
    }
    if (!mexeu) break;
  }
  return a;
}

function difAngPos(b, a) {
  let d = (b - a) % (2 * Math.PI);
  if (d < 0) d += 2 * Math.PI;
  return d;
}

// raioMinimo: o raio do círculo de retratos em que n retratos cabem a `espaco` px um do outro.
export function raioMinimo(n, espaco) {
  if (n <= 1) return 0;
  if (n === 2) return espaco / 2;
  return espaco / (2 * Math.sin(Math.PI / n));
}

// criaArranjo devolve o objeto com o estado entre quadros. `passo(pontos, dtTela, {lado, reinicia})`:
//   pontos: os VIVOS com posição, [{id, x, y, time}] em pixels (lugar verdadeiro);
//   dtTela: ms de tela desde o quadro anterior; reinicia: salto de tempo (barra arrastada): cada
//   retrato vai direto para o lugar novo.
// Devolve Map id → {ax, ay (o anel = o lugar verdadeiro), rx, ry (o retrato), escala, traco (0–1:
// quanto mostrar anel e traço), n (quantos estão juntos; 1 = sozinho)}.
export function criaArranjo({ ret, params = PARAMS }) {
  const P = params;
  let relogio = 0; // ms de tela
  // id → {ox, oy (deslocamento atual), desloc (bool), ux, uy (direção travada), L (comprimento
  // alvo), ate (segura até), trocaEm, bloqueadoDesde, recolhe, ax, ay, vx, vy (velocidade do anel px/ms),
  // esc, rx, ry}
  const est = new Map();

  // Distância "de retrato" (a etiqueta embaixo encurta a vertical).
  const dRet = (ax, ay, bx, by) => Math.hypot(bx - ax, (by - ay) * P.escalaY);
  // O retrato em (px, py), lado s, cobre o anel em (ax, ay)? Quadrado do retrato com a folga do
  // anel e, embaixo, a etiqueta.
  const cobreAnel = (px, py, s, ax, ay, folga = P.folgaAnel) => {
    const meia = (folga * s) / 1;
    return Math.abs(ax - px) < meia && ay - py > -meia && ay - py < meia + P.rotulo * s;
  };

  function passo(pontos, dtTela, { lado = Infinity, reinicia = false, velocidade = 1 } = {}) {
    const dt = Math.max(0, dtTela);
    relogio += dt;
    const agora = relogio;
    if (reinicia) est.clear();
    const porId = new Map(pontos.map((p) => [p.id, p]));
    // Velocidade de cada anel (px por ms de tela), suavizada: para "para onde está andando".
    for (const p of pontos) {
      let e = est.get(p.id);
      if (!e) {
        e = { ox: 0, oy: 0, desloc: false, ux: 0, uy: 0, L: 0, ate: 0, trocaEm: -Infinity, bloqueadoDesde: null, encolheDesde: null, ax: p.x, ay: p.y, vx: 0, vy: 0, esc: 1 };
        est.set(p.id, e);
      } else if (dt > 0) {
        const vx = (p.x - e.ax) / dt;
        const vy = (p.y - e.ay) / dt;
        // Salto do dado (base, renascer, teleporte): não é velocidade.
        const salto = Math.hypot(p.x - e.ax, p.y - e.ay) > ret;
        const k = 1 - Math.exp(-dt / 300);
        if (!salto) {
          e.vx += (vx - e.vx) * k;
          e.vy += (vy - e.vy) * k;
        }
      }
      e.ax = p.x;
      e.ay = p.y;
    }
    // Quem sumiu (morreu, ficou sem posição) recomeça sem deslocamento quando voltar.
    for (const id of [...est.keys()]) if (!porId.has(id)) est.delete(id);
    const ids = pontos.map((p) => p.id).sort((a, b) => a - b);
    const s = ret; // lado do retrato (a escala do grupo é só um enfeite)

    // Onde está o retrato de cada um agora (o do quadro anterior; quem não estava, no anel).
    const ret0 = new Map(ids.map((id) => {
      const e = est.get(id);
      return [id, { x: e.ax + e.ox, y: e.ay + e.oy }];
    }));

    // 1. Colisão no lugar real: retrato (em cima do anel) contra os retratos de quem está no lugar
    //    real e contra os anéis de todos.
    const colideEmCasa = (id, folgaRet, folgaAnel, h = 0) => {
      const e = est.get(id);
      const x = e.ax + e.vx * h;
      const y = e.ay + e.vy * h;
      for (const o of ids) {
        if (o === id) continue;
        const eo = est.get(o);
        // Retrato deslocado de outro não tira ninguém do lugar: quem está deslocado é que desvia
        // (senão o retrato de um empurrava o outro para fora assim que ele voltava). Conta o
        // retrato de quem está no lugar real e o anel de todos.
        if (!eo.desloc && dRet(x, y, eo.ax + eo.vx * h, eo.ay + eo.vy * h) < folgaRet * s) return true;
        if (cobreAnel(x, y, s, eo.ax + eo.vx * h, eo.ay + eo.vy * h, folgaAnel)) return true;
      }
      return false;
    };

    // O retrato de outro jogador como obstáculo: o decidido neste quadro; quem está no lugar real,
    // lá. Quem está deslocado e ainda não foi decidido (prioridade menor: deslocou depois) não
    // conta: ele é que desvia. Sem essa ordem, dois retratos do mesmo lado se empurravam até o
    // máximo (4:25 da 3304217746, Senna e Draven).
    const obstaculo = (o) => alvos.get(o) || (est.get(o).desloc ? null : ret0.get(o));

    // Algum anel a menos de "longe" dele (em h ms de tela)? Acelerado, o jogo anda mais por segundo
    // de tela e quem se afastou volta mais cedo (em tela): a distância cresce com a raiz da velocidade.
    const longe = P.longe * s * Math.sqrt(Math.max(1, velocidade));
    const perto = (id, h) => {
      const e = est.get(id);
      for (const o of ids) {
        if (o === id) continue;
        const eo = est.get(o);
        if (dRet(e.ax + e.vx * h, e.ay + e.vy * h, eo.ax + eo.vx * h, eo.ay + eo.vy * h) < longe) return true;
      }
      return false;
    };

    // 2. O comprimento mínimo, numa direção, para o retrato não colidir com nada (os retratos dos
    //    outros onde estão ou vão estar, todos os anéis, o próprio anel, a borda do mapa).
    const comprimento = (id, ux, uy, h = 0, desde = P.minDesvio * s) => {
      const e = est.get(id);
      const meia = s / 2 + 2;
      for (let L = desde; L <= P.maxDesvio * s + 1e-6; L += 0.1 * s) {
        const x = e.ax + e.vx * h + L * ux;
        const y = e.ay + e.vy * h + L * uy;
        if (x < meia || x > lado - meia || y < meia || y > lado - s * 1.1) continue;
        if (cobreAnel(x, y, s, e.ax + e.vx * h, e.ay + e.vy * h)) continue;
        let ok = true;
        for (const o of ids) {
          if (o === id) continue;
          const eo = est.get(o);
          const r = obstaculo(o);
          if ((r && dRet(x, y, r.x + eo.vx * h, r.y + eo.vy * h) < P.espaco * s * 0.95) || cobreAnel(x, y, s, eo.ax + eo.vx * h, eo.ay + eo.vy * h)) {
            ok = false;
            break;
          }
        }
        if (ok) return L;
      }
      return null;
    };

    // O lugar a L na direção (ux, uy) está livre (borda, próprio anel, outros retratos e anéis)?
    const livreEm = (id, ux, uy, L) => {
      const e = est.get(id);
      const x = e.ax + L * ux;
      const y = e.ay + L * uy;
      const meia = s / 2 + 2;
      if (x < meia || x > lado - meia || y < meia || y > lado - s * 1.1) return false;
      if (cobreAnel(x, y, s, e.ax, e.ay)) return false;
      for (const o of ids) {
        if (o === id) continue;
        const eo = est.get(o);
        const r = obstaculo(o);
        if ((r && dRet(x, y, r.x, r.y) < P.espaco * s * 0.95) || cobreAnel(x, y, s, eo.ax, eo.ay)) return false;
      }
      return true;
    };

    // 3. Escolha da direção: a de menor custo entre as candidatas (comprimento necessário agora e
    //    daqui a pouco, gente na direção, borda); o time só desempata (azul à esquerda).
    const escolhe = (id) => {
      const e = est.get(id);
      const p = porId.get(id);
      let melhor = null;
      for (let k = 0; k < P.direcoes; k++) {
        const a = (2 * Math.PI * k) / P.direcoes;
        const ux = Math.cos(a);
        const uy = Math.sin(a);
        const L0 = comprimento(id, ux, uy, 0);
        if (L0 === null) continue;
        const L1 = comprimento(id, ux, uy, P.previsaoMs);
        let custo = L0 / s + (L1 === null ? 2.5 : 0.6 * (L1 / s));
        // Gente para aquele lado (agora e onde vai estar): retratos e anéis a até 4 retratos.
        for (const o of ids) {
          if (o === id) continue;
          const eo = est.get(o);
          for (const h of [0, P.previsaoMs]) {
            const wx = eo.ax + eo.vx * h - (e.ax + e.vx * h);
            const wy = eo.ay + eo.vy * h - (e.ay + e.vy * h);
            const d = Math.hypot(wx, wy);
            if (d < 1e-6 || d > 4 * s) continue;
            const cos = (wx * ux + wy * uy) / d;
            if (cos > 0) custo += 0.9 * cos * (1 - d / (4 * s));
          }
        }
        // Para onde ele mesmo anda: o retrato à frente do anel fica no caminho de quem vem.
        const v = Math.hypot(e.vx, e.vy);
        if (v > 1e-3) custo += 0.2 * Math.max(0, (e.vx * ux + e.vy * uy) / v);
        const pref = p.time === 200 ? 0 : Math.PI;
        custo += 0.15 * (1 - Math.cos(a - pref));
        if (!melhor || custo < melhor.custo - 1e-9) melhor = { custo, ux, uy, L: L0 };
      }
      if (melhor) return melhor;
      // Nenhuma direção livre (muita gente junto): a que bate em menos coisas no comprimento máximo.
      let menos = null;
      for (let k = 0; k < P.direcoes; k++) {
        const a = (2 * Math.PI * k) / P.direcoes;
        const ux = Math.cos(a);
        const uy = Math.sin(a);
        const L = P.maxDesvio * s;
        const x = Math.min(lado - s / 2 - 2, Math.max(s / 2 + 2, e.ax + L * ux));
        const y = Math.min(lado - s * 1.1, Math.max(s / 2 + 2, e.ay + L * uy));
        let n = 0;
        for (const o of ids) {
          if (o === id) continue;
          const eo = est.get(o);
          const r = obstaculo(o);
          if (r && dRet(x, y, r.x, r.y) < P.espaco * s) n++;
          if (cobreAnel(x, y, s, eo.ax, eo.ay)) n++;
        }
        const custo = n + 0.15 * (1 - Math.cos(a - (p.time === 200 ? 0 : Math.PI)));
        if (!menos || custo < menos.custo) menos = { custo, ux: (x - e.ax) / Math.max(1, Math.hypot(x - e.ax, y - e.ay)), uy: (y - e.ay) / Math.max(1, Math.hypot(x - e.ax, y - e.ay)), L: Math.hypot(x - e.ax, y - e.ay) };
      }
      return menos;
    };

    // O comprimento que o retrato segue: o calculado muda de 0,1 em 0,1 retrato conforme os vizinhos
    // andam; crescer vale na hora (é para sair de cima de alguém), encolher menos que zonaMorta não
    // move o retrato (era o vaivém de 1–2 px a 2×).
    const visivel = (e) => {
      if (e.Lv === undefined || reinicia || e.L > e.Lv || e.Lv - e.L > P.zonaMorta * s) e.Lv = e.L;
      return e.Lv;
    };

    // 4. Estados: entrar, ficar (segurando), voltar. Primeiro as transições de todos; depois cada
    // um decide o lugar, nesta ordem de prioridade: quem está no lugar real, quem deslocou antes,
    // quem desloca agora.
    const alvos = new Map(); // id → {x, y} retrato decidido neste quadro
    const novos = [];
    for (const id of ids) {
      const e = est.get(id);
      const bate = colideEmCasa(id, P.junta, P.folgaAnel);
      // Quem voltou há pouco e já bate de novo (dois da rota trocando golpes, indo e vindo) segura
      // o dobro: voltar e sair de novo a cada segundo era o pisca-pisca.
      if (bate) e.ate = Math.max(e.ate, agora + (e.voltouEm !== undefined && agora - e.voltouEm < P.reincideMs ? 2 * P.seguraMs : P.seguraMs));
      if (!e.desloc && bate) {
        novos.push(id);
      } else if (e.desloc && !bate && agora >= e.ate) {
        // Volta só com folga maior que a de entrada, agora e no futuro próximo.
        // E ninguém (anel) a menos de "longe" dele, agora e daqui a previsaoMs.
        let livre = !colideEmCasa(id, P.solta, P.folgaAnelSai, 0) && !perto(id, 0) && !perto(id, P.previsaoMs);
        for (let h = 400; livre && h <= P.voltaMs; h += 400) livre = !colideEmCasa(id, h <= P.previsaoMs ? P.solta : P.junta, h <= P.previsaoMs ? P.folgaAnelSai : P.folgaAnel, h);
        if (livre) {
          e.desloc = false;
          e.voltouEm = agora;
        }
        else e.ate = agora + 500; // tenta de novo daqui a pouco
      }
    }
    for (const id of ids) if (!est.get(id).desloc && !novos.includes(id)) alvos.set(id, { x: est.get(id).ax, y: est.get(id).ay });
    const ordem = ids.filter((id) => est.get(id).desloc).sort((a, b) => est.get(a).desde - est.get(b).desde || a - b);
    for (const id of novos) {
      ordem.push(id);
    }
    for (const id of ordem) {
      const e = est.get(id);
      if (!e.desloc) {
        // Deslocando agora: escolhe a direção vendo todos os que já decidiram.
        const m = escolhe(id);
        if (!m) {
          alvos.set(id, { x: e.ax, y: e.ay });
          continue;
        }
        Object.assign(e, { desloc: true, desde: agora, ux: m.ux, uy: m.uy, L: m.L, bloqueadoDesde: null, recolhe: false });
      }
      // Livre agora E daqui a antecipaMs: quem vem chegando faz o retrato abrir caminho antes, devagar,
      // em vez de pular na última hora (o tranco virava tremor a 2×).
      const futuroLivre = () => comprimento(id, e.ux, e.uy, P.antecipaMs, e.L) === e.L;
      if (e.desloc && !e.recolhe && livreEm(id, e.ux, e.uy, e.L) && futuroLivre()) {
        // O lugar atual continua livre: fica. Só encolhe se um lugar mais perto do anel ficar livre
        // por EncolheMs seguidos (o vão entre dois vizinhos que abre e fecha não puxa o retrato).
        const Lmin = comprimento(id, e.ux, e.uy, 0);
        const L1 = Lmin === null ? null : comprimento(id, e.ux, e.uy, P.previsaoMs);
        if (Lmin !== null && L1 !== null && Math.max(Lmin, L1) < e.L - P.encolhe * s) {
          if (e.encolheDesde === null) e.encolheDesde = agora;
          if (agora - e.encolheDesde >= P.encolheMs) {
            e.L = Math.max(Lmin, L1);
            e.encolheDesde = null;
          }
        } else e.encolheDesde = null;
        e.bloqueadoDesde = null;
        alvos.set(id, { x: e.ax + e.ux * visivel(e), y: e.ay + e.uy * visivel(e) });
      } else if (e.desloc) {
        e.encolheDesde = null;
        // O lugar atual bate: primeiro o livre mais perto PARA FORA dele; só sem esse, para dentro.
        let L = comprimento(id, e.ux, e.uy, 0, Math.max(P.minDesvio * s, e.L));
        if (L === null || L > e.L + P.cresce * s) L = comprimento(id, e.ux, e.uy, 0) ?? L;
        if (L !== null) {
          const Lf = comprimento(id, e.ux, e.uy, P.antecipaMs, L);
          if (Lf !== null && Lf <= L + P.cresce * s) L = Lf;
        }
        // Bloqueada: a direção travada não tem lugar livre, ou só muito mais longe (o retrato
        // atravessaria os outros para chegar lá).
        const bloqueada = L === null || L > e.L + P.cresce * s;
        if (bloqueada) {
          if (e.bloqueadoDesde === null) e.bloqueadoDesde = agora;
          // Escolhe outra direção depois de bloqueioMs, se a última TROCA foi há TrocaMinimaMs ou
          // mais (a primeira escolha não conta: sair é uma decisão, trocar é outra).
          if (agora - e.bloqueadoDesde >= P.bloqueioMs && agora - e.trocaEm >= P.trocaMinimaMs) {
            const m = escolhe(id);
            const mesma = m && m.ux * e.ux + m.uy * e.uy > Math.cos(Math.PI / 3);
            // Só troca se o lado novo for bem mais perto (ou se o atual nem tem lugar).
            if (m && !mesma && (L === null || m.L < L - P.encolhe * s)) {
              // Recolhe até perto do anel e só então sai pelo lado novo: a troca é uma, não um giro.
              Object.assign(e, { ux: m.ux, uy: m.uy, L: m.L, trocaEm: agora, bloqueadoDesde: null, recolhe: true });
              L = m.L;
            }
          }
          if (L === null) L = e.L;
          if (L > e.L) e.L = L; // sem troca possível agora: cresce (é melhor que ficar em cima)
        } else {
          e.bloqueadoDesde = null;
          // Crescendo, cresce com sobra: quem empurra costuma continuar empurrando, e crescer de
          // 0,1 em 0,1 a cada quadro fazia o retrato andar aos trancos (tremor a 2×).
          e.L = L > e.L ? Math.min(P.maxDesvio * s, L + P.sobra * s) : L;
        }
        alvos.set(id, { x: e.ax + e.ux * visivel(e), y: e.ay + e.uy * visivel(e) });
      } else {
        alvos.set(id, { x: e.ax, y: e.ay });
      }
    }

    // 5. O deslocamento anda até o alvo: suave e com velocidade máxima (em relação ao anel; o anel
    //    andando leva o retrato junto, sem atraso). Com salto de tempo, vai direto.
    const f = reinicia ? 1 : 1 - Math.exp(-dt / P.tau);
    const passoMax = reinicia ? Infinity : (P.vmax * ret * dt) / 1000;
    const r = new Map();
    for (const id of ids) {
      const e = est.get(id);
      if (e.recolhe && Math.hypot(e.ox, e.oy) < 0.3 * ret) e.recolhe = false;
      const tx = e.desloc && !e.recolhe ? e.ux * visivel(e) : 0;
      const ty = e.desloc && !e.recolhe ? e.uy * visivel(e) : 0;
      let dx = (tx - e.ox) * f;
      let dy = (ty - e.oy) * f;
      // Velocidade mínima: a cauda da exponencial deixava o retrato quase meio segundo em cima do
      // próprio anel ao voltar (e em cima do anel do vizinho ao sair).
      const falta = Math.hypot(tx - e.ox, ty - e.oy);
      // Só para entrar e sair (perto do anel); longe dele, a exponencial pura acompanha o vizinho
      // sem trancos.
      // Em cima de um anel (o próprio ou de outro) também: sair de cima é urgente.
      const emCima = ids.some((o) => cobreAnel(e.ax + e.ox, e.ay + e.oy, s, est.get(o).ax, est.get(o).ay));
      const perto = (tx === 0 && ty === 0) || Math.hypot(e.ox, e.oy) < P.minDesvio * s || (emCima && Math.hypot(e.ox, e.oy) > 0.05 * ret);
      const minimo = perto ? Math.min(falta, reinicia ? Infinity : (P.vmin * ret * dt) / 1000) : 0;
      if (Math.hypot(dx, dy) < minimo && falta > 0) {
        dx = ((tx - e.ox) / falta) * minimo;
        dy = ((ty - e.oy) / falta) * minimo;
      }
      const d = Math.hypot(dx, dy);
      if (d > passoMax) {
        dx = (dx / d) * passoMax;
        dy = (dy / d) * passoMax;
      }
      e.ox += dx;
      e.oy += dy;
    }
    // Quantos estão juntos (para a escala e o texto curto): os anéis a até 2 retratos dele, se ele
    // está deslocado.
    for (const id of ids) {
      const e = est.get(id);
      let n = 1;
      if (e.desloc || Math.hypot(e.ox, e.oy) > 0.05 * ret) {
        for (const o of ids) if (o !== id && Math.hypot(est.get(o).ax - e.ax, est.get(o).ay - e.ay) < 2 * ret) n++;
      }
      const alvoEsc = escalaDoGrupo(n);
      e.esc = reinicia ? alvoEsc : e.esc + (alvoEsc - e.esc) * (1 - Math.exp(-dt / 400));
      const off = Math.hypot(e.ox, e.oy);
      r.set(id, { ax: e.ax, ay: e.ay, rx: e.ax + e.ox, ry: e.ay + e.oy, escala: e.esc, traco: Math.min(1, off / (ret * 0.3)), n });
    }
    return r;
  }

  return { passo };
}
