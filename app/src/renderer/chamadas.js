// Onde desenhar cada retrato no mapa com play: o "rótulo com linha de chamada" dos mapas de
// transmissão. Puro (chamadas.test.mjs, valida-desenho.js): a tela e o validador usam ESTA função.
//
// Por que mudou (partida 3304217746, 12:28, 14:24, 19:16): o leque em volta do centro do grupo
// tirava o retrato de cima do jogador sem mostrar onde ele estava de verdade; dois jogadores de
// times opostos que se cruzavam entravam e saíam do grupo e os retratos pulavam ou trocavam de
// lado, e numa luta o retrato caía em cima da parede. Agora:
//   - jogador sozinho: retrato em cima do lugar verdadeiro, sem anel nem traço;
//   - quando dois ou mais retratos colidiriam: cada um ganha um ANEL no lugar exato (a verdade) e o
//     retrato é afastado para FORA do miolo do grupo (na direção que sai do centro dos anéis), o
//     mínimo para não cobrir os outros retratos nem os anéis, ligado ao anel por um traço;
//   - estabilidade: cada membro guarda um ÂNGULO enquanto o grupo existe; os ângulos só giram
//     devagar e nunca passam um pelo outro (ninguém troca de lado); o deslocamento do retrato em
//     relação ao anel é suavizado no tempo de tela e tem velocidade máxima, então entrar e sair do
//     modo "com traço" é um deslizamento, nunca um pulo;
//   - muitos juntos (5 ou mais) diminuem um pouco os retratos.

import { agrupa } from './mapa.js';

// Parâmetros, em múltiplos do lado do retrato (`ret`, pixels) e em ms de TELA.
export const PARAMS = {
  junta: 1.05, // dois retratos a menos disso (com a etiqueta embaixo) colidem
  solta: 1.4, // e só deixam de colidir acima disso (histerese)
  escalaY: 0.7, // a etiqueta embaixo deixa o retrato "mais alto": encurta a distância vertical
  espaco: 1.3, // distância mínima entre os centros de dois retratos vizinhos no grupo
  folgaAnel: 0.62, // distância mínima entre o centro do retrato e qualquer anel do grupo
  tau: 140, // ms: constante de tempo da suavização do deslocamento
  vmax: 5, // ret por segundo de tela: velocidade máxima do retrato em relação ao anel
  giro: 2.2, // rad por segundo de tela: quanto o ângulo guardado pode girar
  maxDesvio: 3.2, // ret: o deslocamento nunca passa disso
  encolhe: 1.5, // ret: o raio do grupo só encolhe quando fica menor que o anterior mais que isso
  zonaMorta: 0, // ret: no grupo, diferença até o alvo menor que isso não move o retrato (testado 0,12: piorou a 8×)
};

// escalaDoGrupo: com muitos juntos os retratos diminuem um pouco (cabe mais sem afastar tanto).
export function escalaDoGrupo(n) {
  if (n >= 8) return 0.74;
  if (n >= 5) return 0.86;
  return 1;
}

const difAng = (a, b) => {
  let d = a - b;
  while (d > Math.PI) d -= 2 * Math.PI;
  while (d < -Math.PI) d += 2 * Math.PI;
  return d;
};

// espalha: ângulos (já em ordem circular) afastados até `gap` entre vizinhos, SEM trocar a ordem:
// cada passo empurra os dois de um par apertado para longe um do outro, no máximo metade da
// folga que cada um tem do outro lado. Devolve os ângulos novos na mesma ordem.
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
        // Folga do outro lado de cada um: empurrar além dela passaria o vizinho de lá.
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

// difAngPos: quanto girar de a para chegar em b no sentido positivo, em [0, 2π).
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
//   dtTela: ms de tela desde o quadro anterior; reinicia: salto de tempo (barra arrastada):
//   cada retrato vai direto para o lugar novo.
// Devolve Map id → {ax, ay (o anel = o lugar verdadeiro), rx, ry (o retrato), escala, traco (0–1:
// quanto mostrar anel e traço), n (tamanho do grupo; 1 = sozinho)}.
export function criaArranjo({ ret, params = PARAMS }) {
  const P = params;
  let ligados = new Set();
  const est = new Map(); // id → {ox, oy, ang (null fora de grupo), esc}

  function passo(pontos, dtTela, { lado = Infinity, reinicia = false, velocidade = 1 } = {}) {
    if (reinicia) {
      ligados = new Set();
    }
    const ag = agrupa(pontos, { junta: ret * P.junta, solta: ret * P.solta, ligados, escalaY: P.escalaY });
    ligados = ag.ligados;
    const alvo = new Map(); // id → {ox, oy, esc, n}
    for (const g of ag.grupos) {
      const ms = g.map((i) => pontos[i]);
      const n = ms.length;
      const esc = escalaDoGrupo(n);
      const cx = ms.reduce((s, m) => s + m.x, 0) / n;
      const cy = ms.reduce((s, m) => s + m.y, 0) / n;
      // Ângulo de cada um: o guardado (estável) girando devagar para a direção verdadeira; quem
      // acabou de entrar começa na direção dele a partir do centro (ou, no miolo, pelo time:
      // azul à esquerda, vermelho à direita).
      const maxGiro = reinicia ? Math.PI : (P.giro * Math.max(0, dtTela)) / 1000;
      const antigos = [];
      const angs = ms.map((m, i) => {
        const dx = m.x - cx;
        const dy = m.y - cy;
        const longe = Math.hypot(dx, dy) > ret * 0.25;
        const real = longe ? Math.atan2(dy, dx) : null;
        const e = est.get(m.id);
        if (e?.ang === null || e?.ang === undefined || reinicia) {
          return real ?? (m.time === 200 ? 0 : Math.PI) + ((m.id % 5) - 2) * 0.35;
        }
        antigos.push(i);
        if (real === null) return e.ang;
        const d = difAng(real, e.ang);
        return e.ang + Math.max(-maxGiro, Math.min(maxGiro, d));
      });
      // Quem já estava no grupo não pode passar pelo vizinho ao girar (seria trocar de lado): se o
      // giro deste quadro mudaria a ordem circular deles, ninguém gira neste quadro.
      if (antigos.length >= 3) {
        const porAntigo = antigos.slice().sort((a, b) => norm(est.get(ms[a].id).ang) - norm(est.get(ms[b].id).ang));
        const base = angs[porAntigo[0]];
        let ant = -1;
        let ok = true;
        for (const i of porAntigo) {
          const rel = norm(angs[i] - base);
          if (rel < ant) ok = false;
          ant = rel;
        }
        if (!ok) for (const i of antigos) angs[i] = est.get(ms[i].id).ang;
      }
      // Ordem circular pelos ângulos guardados; o desempate é o id (determinístico).
      const ordem = ms.map((_, i) => i).sort((a, b) => norm(angs[a]) - norm(angs[b]) || ms[a].id - ms[b].id);
      const esp = ret * esc * P.espaco;
      // O espalhamento dos ângulos supõe o grupo compacto (o pior caso): no grupo comprido de uma
      // luta espalhada ele só afasta demais, nunca de menos.
      const R = raioMinimo(n, esp);
      const gap = 2 * Math.asin(Math.min(1, esp / (2 * Math.max(R, esp / 2))));
      const espalhados = espalha(ordem.map((i) => norm(angs[i])), gap);
      // O retrato sai do PRÓPRIO anel, na direção guardada, só até passar dos outros anéis do
      // grupo (a folga); depois, enquanto cobre outro retrato, os dois se afastam mais, cada um
      // pela sua direção. Local: na luta de 8 espalhada, cada traço fica curto (antes, um círculo
      // em volta do grupo inteiro esticava os traços e eles se cruzavam).
      const folga = ret * esc * P.folgaAnel;
      const raios = ordem.map((i, k) => {
        const m = ms[i];
        const ux = Math.cos(espalhados[k]);
        const uy = Math.sin(espalhados[k]);
        let L = folga;
        for (let passe = 0; passe < 3; passe++) {
          for (const o of ms) {
            if (o === m) continue;
            const wx = o.x - m.x;
            const wy = o.y - m.y;
            const pr = wx * ux + wy * uy;
            const d2 = wx * wx + wy * wy - pr * pr;
            if (d2 >= folga * folga) continue;
            const h = Math.sqrt(folga * folga - d2);
            if (L > pr - h && L < pr + h) L = pr + h;
          }
        }
        return { m, ux, uy, L, a: espalhados[k] };
      });
      for (let it = 0; it < 8; it++) {
        let mexeu = false;
        for (let i = 0; i < raios.length; i++) {
          for (let k = i + 1; k < raios.length; k++) {
            const A = raios[i];
            const B = raios[k];
            const d = Math.hypot(B.m.x + B.L * B.ux - (A.m.x + A.L * A.ux), B.m.y + B.L * B.uy - (A.m.y + A.L * A.uy));
            if (d >= esp * 0.95) continue;
            const falta = Math.min(ret * 0.5, (esp - d) / 2);
            A.L += falta;
            B.L += falta;
            mexeu = true;
          }
        }
        if (!mexeu) break;
      }
      for (const z of raios) {
        const m = z.m;
        // Histerese: o traço cresce na hora e só encolhe quando ficou bem menor (o zigue-zague
        // do dado virava tremor a 4× e 8×).
        const Lant = est.get(m.id)?.L;
        if (!reinicia && Lant !== undefined && Lant > z.L && Lant - z.L < ret * P.encolhe) z.L = Lant;
        const { ux, uy, L, a } = z;
        const R = L;
        let tx = m.x + L * ux;
        let ty = m.y + L * uy;
        // Dentro do mapa (o retrato e a etiqueta embaixo).
        const meia = (ret * esc) / 2 + 2;
        tx = Math.min(lado - meia, Math.max(meia, tx));
        ty = Math.min(lado - ret * esc * 1.1, Math.max(meia, ty));
        let ox = tx - m.x;
        let oy = ty - m.y;
        const dist = Math.hypot(ox, oy);
        const lim = ret * P.maxDesvio;
        if (dist > lim) {
          ox = (ox / dist) * lim;
          oy = (oy / dist) * lim;
        }
        alvo.set(m.id, { ox, oy, esc, n, ang: a, L: R });
      }
    }
    // Acima de 2× o dado zigue-zagueia mais rápido na tela: a suavização fica mais lenta junto
    // (só o deslocamento em relação ao anel; o grupo inteiro andando não atrasa nada).
    const tau = P.tau * Math.max(1, velocidade / 2);
    const f = reinicia ? 1 : 1 - Math.exp(-Math.max(0, dtTela) / tau);
    const passoMax = reinicia ? Infinity : (P.vmax * ret * Math.max(0, dtTela)) / 1000;
    const r = new Map();
    const vistos = new Set();
    for (const p of pontos) {
      vistos.add(p.id);
      const a = alvo.get(p.id) || { ox: 0, oy: 0, esc: 1, n: 1, ang: null, L: undefined };
      // Quem aparece agora (início, renascer, fim de buraco no dado) começa em cima do anel e
      // desliza: entrar no grupo é sempre transição. Com salto no tempo, vai direto.
      const e = est.get(p.id) || (reinicia ? { ox: a.ox, oy: a.oy, ang: null, esc: a.esc } : { ox: 0, oy: 0, ang: null, esc: 1 });
      let dx = (a.ox - e.ox) * f;
      let dy = (a.oy - e.oy) * f;
      // Zona morta no grupo: a diferença pequena até o alvo não move o retrato (o alvo treme
      // junto com o dado; o retrato não precisa). Fora do grupo vai até zero, em cima do anel.
      if (a.n >= 2 && !reinicia && Math.hypot(a.ox - e.ox, a.oy - e.oy) < ret * P.zonaMorta) {
        dx = 0;
        dy = 0;
      }
      const d = Math.hypot(dx, dy);
      if (d > passoMax) {
        dx = (dx / d) * passoMax;
        dy = (dy / d) * passoMax;
      }
      const novo = { ox: e.ox + dx, oy: e.oy + dy, ang: a.ang, esc: e.esc + (a.esc - e.esc) * f, L: a.L, pox: e.ox, poy: e.oy, prx: e.rx, pry: e.ry };
      est.set(p.id, novo);
      r.set(p.id, { ax: p.x, ay: p.y, rx: p.x + novo.ox, ry: p.y + novo.oy, escala: novo.esc, traco: 0, n: a.n });
    }
    // Ninguém troca de lado de um quadro para o outro: se o passo deste quadro inverteria o
    // lado entre dois retratos próximos, os dois ficam com o deslocamento de antes (andam só
    // com o anel). Salto do tempo (reinicia) não conta.
    if (!reinicia) {
      const ids = [...r.keys()];
      for (let i = 0; i < ids.length; i++) {
        for (let k = i + 1; k < ids.length; k++) {
          const A = r.get(ids[i]);
          const B = r.get(ids[k]);
          const ea = est.get(ids[i]);
          const eb = est.get(ids[k]);
          if (ea.prx === undefined || eb.prx === undefined) continue;
          const v0x = eb.prx - ea.prx;
          const v0y = eb.pry - ea.pry;
          const v1x = B.rx - A.rx;
          const v1y = B.ry - A.ry;
          if (Math.hypot(v0x, v0y) < 2 || Math.hypot(v0x, v0y) > ret * 1.8 || v0x * v1x + v0y * v1y >= 0) continue;
          for (const [id, s] of [
            [ids[i], ea],
            [ids[k], eb],
          ]) {
            s.ox = s.pox;
            s.oy = s.poy;
            const o = r.get(id);
            o.rx = o.ax + s.ox;
            o.ry = o.ay + s.oy;
          }
        }
      }
    }
    for (const [id, o] of r) {
      const s = est.get(id);
      s.rx = o.rx;
      s.ry = o.ry;
      o.traco = Math.min(1, Math.hypot(s.ox, s.oy) / (ret * 0.3));
    }
    // Quem sumiu (morreu, ficou sem posição) recomeça sem deslocamento quando voltar.
    for (const id of [...est.keys()]) if (!vistos.has(id)) est.delete(id);
    return r;
  }

  return { passo };
}

const norm = (a) => {
  let x = a % (2 * Math.PI);
  if (x < 0) x += 2 * Math.PI;
  return x;
};
