// Gráficos ao longo da partida (tela da partida: vida e farm) na MESMA escala de tempo, desenhados
// em SVG no tamanho real em pixels e redesenhados quando a caixa muda de tamanho. Antes o SVG tinha
// viewBox fixo com preserveAspectRatio="none": em tela larga (2560 px) o navegador esticava tudo,
// inclusive o texto dos eixos ("0:00" com as letras alargadas). Agora 1 unidade = 1 px e o texto
// fica sempre no tamanho normal. As contas (séries, escala) são puras (grafico-tempo.test.mjs).

// Margens em px: iguais nos dois gráficos para o mesmo instante cair no mesmo x.
export const MARGEM = { esq: 40, dir: 12, topo: 8, base: 22 };

// serieCS: o CS (tropas + monstros) de um participante em cada quadro da linha do tempo do cliente
// (de minuto em minuto, exato). `linhaDoTempo` = GET /api/partidas/{id}/linha-do-tempo.
export function serieCS(linhaDoTempo, pid) {
  const r = [];
  for (const f of linhaDoTempo?.frames || []) {
    const pf = f.participantFrames?.[String(pid)];
    if (!pf) continue;
    r.push({ t_ms: f.timestamp, cs: (pf.minionsKilled || 0) + (pf.jungleMinionsKilled || 0) });
  }
  return r;
}

// csNoInstante: o CS interpolado entre dois minutos (para a dica ao passar o mouse) e o CS por
// minuto até ali.
export function csNoInstante(serie, t) {
  if (!serie.length) return null;
  let i = 0;
  while (i < serie.length - 1 && serie[i + 1].t_ms <= t) i++;
  const a = serie[i];
  const b = serie[i + 1];
  const cs = b && t > a.t_ms ? a.cs + ((b.cs - a.cs) * (t - a.t_ms)) / Math.max(1, b.t_ms - a.t_ms) : a.cs;
  const min = t / 60000;
  return { cs, por_min: min > 0.5 ? cs / min : null };
}

// perdidasDoJogador: as ondas perdidas do participante (GET .../ondas, `rotas[].perdidas`).
export function perdidasDoJogador(ondas, pid) {
  const r = [];
  for (const rota of ondas?.rotas || []) for (const p of rota.perdidas || []) if (p.participante_id === pid) r.push({ ...p, rota: rota.rota });
  return r.sort((a, b) => a.t_ms - b.t_ms);
}

// escala: as funções de x (tempo → px) e o passo das marcas do eixo, para uma largura em px.
export function escala(t1, largura) {
  const L = Math.max(1, largura - MARGEM.esq - MARGEM.dir);
  const x = (t) => MARGEM.esq + (Math.max(0, Math.min(t1, t)) / Math.max(1, t1)) * L;
  const tDoX = (px) => ((px - MARGEM.esq) / L) * t1;
  // Uma marca a cada 1, 2, 5 ou 10 min, a que deixe ≥ 70 px entre os rótulos.
  const passos = [60000, 120000, 300000, 600000];
  const passo = passos.find((p) => (p / Math.max(1, t1)) * L >= 70) || 600000;
  return { x, tDoX, passo, L };
}

// teto "redondo" para o eixo y (CS): 50, 100, 150, ...
export const tetoRedondo = (v, de = 50) => Math.max(de, Math.ceil(v / de) * de);
