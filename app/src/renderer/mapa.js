// Coordenada do jogo ↔ pixel no map11.png (Data Dragon). Limites do mapa 11 medidos nas
// descobertas: min (−120, −120), max (14870, 14980); x cresce para a direita, y para CIMA (na tela
// o y inverte). Funções puras (mapa.test.mjs).

export const LIMITES = { min: { x: -120, y: -120 }, max: { x: 14870, y: 14980 } };
const LARG = LIMITES.max.x - LIMITES.min.x; // 14990
const ALT = LIMITES.max.y - LIMITES.min.y; // 15100

// paraTela: (x, y) do jogo → (px, py) num quadrado de `lado` pixels (o mapa é desenhado quadrado).
export function paraTela(x, y, lado) {
  return {
    px: ((x - LIMITES.min.x) / LARG) * lado,
    py: (1 - (y - LIMITES.min.y) / ALT) * lado,
  };
}

// paraJogo: o inverso (clique no mapa → coordenada do jogo).
export function paraJogo(px, py, lado) {
  return {
    x: (px / lado) * LARG + LIMITES.min.x,
    y: (1 - py / lado) * ALT + LIMITES.min.y,
  };
}

// Em porcentagem, para posicionar com left/top num contêiner de qualquer tamanho.
export function emPorcento(x, y) {
  const { px, py } = paraTela(x, y, 100);
  return { left: px, top: py };
}

// zonaNaGrade: o código da zona em (x, y) pela grade de GET /api/mapa/zonas (contrato: linha 0 =
// y mínimo, caractere 'A' + índice em `codigos`). Fora da grade: ''.
export function zonaNaGrade(grade, x, y) {
  if (!grade || x === null || y === null || x === undefined || y === undefined) return '';
  const j = Math.floor((x - grade.origem.x) / grade.celula);
  const i = Math.floor((y - grade.origem.y) / grade.celula);
  if (i < 0 || j < 0 || i >= grade.linhas.length || j >= grade.colunas) return '';
  const c = grade.linhas[i].charCodeAt(j) - 65;
  return grade.codigos[c] ?? '';
}

// distancia em unidades do mapa.
export function distancia(a, b) {
  return Math.hypot((a.x ?? 0) - (b.x ?? 0), (a.y ?? 0) - (b.y ?? 0));
}

// Figuras juntas no desenho (luta, rota com dois de cada lado). O afastamento por empurrão,
// quadro a quadro, tremia: a direção entre dois retratos quase no mesmo lugar é ruído e muda a
// cada quadro, e eles ficavam girando um em volta do outro (partida 3304217746, 5:59, e o morto
// empurrado pelos vivos aos 4:09). Agora quem está junto forma um GRUPO, e o grupo é desenhado
// em posições fixas em volta do centro dele (leque), numa ordem fixa: o desenho só muda quando
// o centro anda ou quando alguém entra ou sai do grupo.

const chavePar = (a, b) => (a < b ? `${a}|${b}` : `${b}|${a}`);

// agrupa: os grupos de pontos ({id, x, y}, em pixels, no lugar verdadeiro) que estão juntos.
// Histerese: um par entra no grupo a menos de `junta` pixels e só sai acima de `solta`
// (`ligados` = os pares ligados no quadro anterior, devolvidos para o próximo). Sem ela, dois
// retratos na fronteira entravam e saíam do grupo a cada quadro. Devolve {grupos: [[índices]]
// só com 2 ou mais, ligados: Set}. `escalaY` < 1 encurta a distância vertical: as etiquetas
// ficam EMBAIXO do retrato, então dois retratos um sobre o outro se cobrem mais longe do que
// dois lado a lado.
export function agrupa(pontos, { junta, solta, ligados = new Set(), escalaY = 1 }) {
  const pai = pontos.map((_, i) => i);
  const raiz = (i) => (pai[i] === i ? i : (pai[i] = raiz(pai[i])));
  const novos = new Set();
  for (let i = 0; i < pontos.length; i++) {
    for (let j = i + 1; j < pontos.length; j++) {
      const k = chavePar(pontos[i].id, pontos[j].id);
      const d = Math.hypot(pontos[j].x - pontos[i].x, (pontos[j].y - pontos[i].y) * escalaY);
      if (d < (ligados.has(k) ? solta : junta)) {
        novos.add(k);
        pai[raiz(i)] = raiz(j);
      }
    }
  }
  const porRaiz = new Map();
  pontos.forEach((_, i) => {
    const r = raiz(i);
    if (!porRaiz.has(r)) porRaiz.set(r, []);
    porRaiz.get(r).push(i);
  });
  return { grupos: [...porRaiz.values()].filter((g) => g.length > 1), ligados: novos };
}

// leque: as posições (relativas ao centro do grupo) de n figuras a `espaco` pixels da vizinha.
// Dois lado a lado; três ou mais num círculo começando pela esquerda. Ordem fixa: quem chama
// ordena os membros sempre do mesmo jeito (time, depois participante), e o azul fica à esquerda.
export function leque(n, espaco) {
  if (n <= 1) return [{ x: 0, y: 0 }];
  if (n === 2) return [{ x: -espaco / 2, y: 0 }, { x: espaco / 2, y: 0 }];
  const r = espaco / (2 * Math.sin(Math.PI / n));
  return Array.from({ length: n }, (_, i) => {
    const a = Math.PI + (i * 2 * Math.PI) / n;
    return { x: r * Math.cos(a), y: r * Math.sin(a) };
  });
}

// separa: afasta pontos que caíram um em cima do outro, sem passar de `maxDesvio` pixels do lugar
// verdadeiro. É para desenho PARADO (o retrato do abate): no mapa com play, que redesenha a cada
// quadro, o empurrão tremia, e lá vale agrupa + leque. Determinístico: pontos no mesmo lugar
// abrem pelo índice. Entrada e saída: [{x, y}].
export function separa(pontos, raio, { iteracoes = 8, maxDesvio = raio } = {}) {
  const r = pontos.map((p) => ({ x: p.x, y: p.y, ox: p.x, oy: p.y }));
  for (let it = 0; it < iteracoes; it++) {
    let mexeu = false;
    for (let i = 0; i < r.length; i++) {
      for (let j = i + 1; j < r.length; j++) {
        let dx = r[j].x - r[i].x;
        let dy = r[j].y - r[i].y;
        let d = Math.hypot(dx, dy);
        if (d >= raio) continue;
        if (d < 1e-6) {
          const a = (j * 2.399963) % (2 * Math.PI); // ângulo de ouro: leque determinístico
          dx = Math.cos(a);
          dy = Math.sin(a);
          d = 1e-6;
        } else {
          dx /= d;
          dy /= d;
        }
        const empurra = (raio - d) / 2;
        r[i].x -= dx * empurra;
        r[i].y -= dy * empurra;
        r[j].x += dx * empurra;
        r[j].y += dy * empurra;
        mexeu = true;
      }
    }
    for (const p of r) {
      const ddx = p.x - p.ox;
      const ddy = p.y - p.oy;
      const desvio = Math.hypot(ddx, ddy);
      if (desvio > maxDesvio) {
        p.x = p.ox + (ddx / desvio) * maxDesvio;
        p.y = p.oy + (ddy / desvio) * maxDesvio;
      }
    }
    if (!mexeu) break;
  }
  return r.map((p) => ({ x: p.x, y: p.y }));
}

// raioDoLeque: a distância do centro até as figuras do leque (0 para uma, espaco/2 para duas).
export function raioDoLeque(n, espaco) {
  if (n <= 1) return 0;
  if (n === 2) return espaco / 2;
  return espaco / (2 * Math.sin(Math.PI / n));
}

// Torres e inibidores do Summoner's Rift (mapa 11), nas coordenadas do jogo. As
// posições são as dos eventos BUILDING_KILL da linha do tempo (conferidas com
// as 14 estruturas derrubadas na partida 3304217746): é por elas que se sabe
// qual estrutura caiu.
export const ESTRUTURAS = [
  ...[
    [981, 10441], [1512, 6699], [1169, 4287], [5846, 6396], [5048, 4812], [3651, 3696],
    [10504, 1029], [6919, 1483], [4281, 1253], [1748, 2270], [2177, 1807],
  ].map(([x, y]) => ({ time: 100, tipo: 'torre', x, y })),
  ...[
    [4318, 13875], [7943, 13411], [10481, 13650], [8955, 8510], [9767, 10113], [11134, 11207],
    [13866, 4505], [13327, 8226], [13624, 10572], [12611, 13084], [13052, 12612],
  ].map(([x, y]) => ({ time: 200, tipo: 'torre', x, y })),
  ...[[1171, 3571], [3203, 3208], [3452, 1236]].map(([x, y]) => ({ time: 100, tipo: 'inibidor', x, y })),
  ...[[11261, 13676], [11598, 11667], [13604, 11316]].map(([x, y]) => ({ time: 200, tipo: 'inibidor', x, y })),
];

// imagemEstrutura: o desenho da estrutura (assets/estruturas, SVG original em 3/4: torre com
// plataforma, corpo, ameias e cristal; em ruínas, o toco cinza com escombros e a plataforma
// ainda na cor do time; inibidor, o cristal redondo). Um arquivo por time: a cor já vem nele.
export function imagemEstrutura(s, caida) {
  const time = s.time === 200 ? 'vermelho' : 'azul';
  const nome = s.tipo === 'torre' ? (caida ? 'torre-caida' : 'torre') : caida ? 'inibidor-caido' : 'inibidor';
  return `assets/estruturas/${nome}-${time}.svg`;
}

// Inibidor derrubado renasce depois de 5 minutos; torre não volta.
export const RENASCE_INIBIDOR_MS = 5 * 60 * 1000;

// estruturasCaidas devolve, para cada estrutura de ESTRUTURAS, se ela está no
// chão no instante t, pelos eventos de estrutura derrubada (x, y, t_ms).
export function estruturasCaidas(eventos, t) {
  return ESTRUTURAS.map((s) =>
    eventos.some((e) => {
      if (e.t_ms > t || e.x === null || e.x === undefined) return false;
      if (Math.hypot(e.x - s.x, e.y - s.y) > 350) return false;
      return s.tipo === 'torre' || t - e.t_ms < RENASCE_INIBIDOR_MS;
    }),
  );
}
