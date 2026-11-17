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

// separa: afasta figuras que caíram uma em cima da outra no desenho (luta no meio do mapa), sem
// mexer no dado: cada ponto anda no máximo `maxDesvio` pixels do lugar verdadeiro. Determinístico:
// pontos no mesmo lugar abrem em leque pelo índice. Entrada e saída em pixels: [{x, y}].
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
          const ang = (j * 2.399) % (2 * Math.PI);
          dx = Math.cos(ang);
          dy = Math.sin(ang);
          d = 1;
        } else {
          dx /= d;
          dy /= d;
        }
        const empurra = (raio - Math.min(d, raio)) / 2;
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
