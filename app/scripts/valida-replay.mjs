// npm run valida-replay -- <game_id> [--servidor http://127.0.0.1:8770] [--de 735] [--ate 760] [--antes]
// [--grava arquivo.json]
//
// Toca a partida inteira (ou o trecho), quadro a quadro, nas velocidades 1×, 2×, 4× e 8×, com as
// mesmas funções da tela (src/renderer/valida-desenho.js), e imprime os glitches do desenho.
// Busca os dados no servidor exatamente como a tela: janelas de 60 s, passo de 0,5 s.
// --antes: valida também o desenho antigo (leque em volta do centro do grupo e etiqueta de grupo),
// para o antes/depois. --grava: guarda as janelas buscadas (só os campos que o desenho usa), para
// virar trecho de teste.

import { writeFileSync } from 'node:fs';
import { validaDesenho, VELOCIDADES_VALIDADAS } from '../src/renderer/valida-desenho.js';
import { JANELA_MS, PASSO_S, atividadeNoInstante } from '../src/renderer/replay-dados.js';
import { agrupa, leque, raioDoLeque } from '../src/renderer/mapa.js';
import { relogio, atividadeCurta } from '../src/renderer/texto.js';

const args = process.argv.slice(2);
const opc = (nome, padrao) => {
  const i = args.indexOf(`--${nome}`);
  return i >= 0 ? args[i + 1] : padrao;
};
const gameId = args.find((a) => /^\d+$/.test(a));
if (!gameId) {
  console.error('uso: npm run valida-replay -- <game_id> [--servidor http://127.0.0.1:8770] [--de s] [--ate s] [--antes] [--grava arquivo.json]');
  process.exit(2);
}
const servidor = opc('servidor', process.env.LANE_SERVIDOR || 'http://127.0.0.1:8770');

async function pede(caminho) {
  const r = await fetch(servidor + caminho);
  if (!r.ok) throw new Error(`${caminho}: ${r.status} ${await r.text()}`);
  return r.json();
}

// Só o que o desenho usa: menos memória e um arquivo de teste pequeno.
export const enxuga = (q) => ({
  t_ms: q.t_ms,
  jogadores: q.jogadores.map((j) => ({ participante_id: j.participante_id, x: j.x, y: j.y, morto: j.morto, origem: j.origem, rastreado: j.rastreado })),
});
const enxugaAtv = (a) => ({ participante_id: a.participante_id, t_ms: a.t_ms, fim_ms: a.fim_ms, atividade: a.atividade, com: a.com, alvo: a.alvo, farmando: a.farmando });

// O desenho de ANTES (views/replay.js até 2026-11-17): grupo com histerese, leque em volta do
// centro do grupo, desvio suavizado sem velocidade máxima; de 3 para cima, etiqueta de grupo.
function criaArranjoLeque(ret) {
  let ligados = new Set();
  const desvios = new Map();
  return {
    passo(pontos, dt, { lado }) {
      const ag = agrupa(pontos, { junta: ret * 1.6, solta: ret * 2.1, ligados, escalaY: 0.6 });
      ligados = ag.ligados;
      const espaco = ret * 1.3;
      const alvos = new Map();
      const n = new Map();
      for (const g of ag.grupos) {
        const ms = g.map((i) => pontos[i]).sort((a, b) => (a.time === 200 ? 100 : 0) + a.id - ((b.time === 200 ? 100 : 0) + b.id));
        const borda = raioDoLeque(ms.length, espaco) + ret / 2 + 2;
        const prende = (v) => Math.min(lado - borda, Math.max(borda, v));
        const cx = prende(ms.reduce((s, m) => s + m.x, 0) / ms.length);
        const cy = prende(ms.reduce((s, m) => s + m.y, 0) / ms.length);
        const slots = leque(ms.length, espaco);
        ms.forEach((m, k) => {
          alvos.set(m.id, { dx: cx + slots[k].x - m.x, dy: cy + slots[k].y - m.y });
          n.set(m.id, ms.length);
        });
      }
      const f = 1 - Math.exp(-dt / 110);
      const r = new Map();
      for (const p of pontos) {
        const alvo = alvos.get(p.id) || { dx: 0, dy: 0 };
        const ant = desvios.get(p.id) || alvo;
        const d = { dx: ant.dx + (alvo.dx - ant.dx) * f, dy: ant.dy + (alvo.dy - ant.dy) * f };
        desvios.set(p.id, d);
        r.set(p.id, { ax: p.x, ay: p.y, rx: p.x + d.dx, ry: p.y + d.dy, escala: 1, n: n.get(p.id) || 1 });
      }
      for (const id of [...desvios.keys()]) if (!pontos.some((p) => p.id === id)) desvios.delete(id);
      return r;
    },
  };
}
// Antes: etiqueta só de rastreado, sem segurar nada; de 3 para cima a individual sumia e entrava a do grupo.
const rotuloAntes = (j, atividades, t, nomes, a) => {
  if (a && a.n >= 3 && !j.morto) return `grupo de ${a.n}`;
  if (j.morto) return 'morto';
  const tr = j.rastreado ? atividadeNoInstante(atividades, j.participante_id, t) : null;
  return tr ? atividadeCurta(tr, nomes) : '';
};

const { partida } = await pede(`/api/partidas/${gameId}`);
const duracaoMs = partida.duracao_s * 1000;
const times = Object.fromEntries(partida.participantes.map((p) => [p.participante_id, p.time]));
const nomes = Object.fromEntries(partida.participantes.map((p) => [p.participante_id, p.campeao]));
const de = Number(opc('de', 0)) * 1000;
const ate = Math.min(duracaoMs, Number(opc('ate', partida.duracao_s)) * 1000);
const janelas = new Map();
for (let ini = Math.floor(de / JANELA_MS) * JANELA_MS; ini < ate; ini += JANELA_MS) {
  const fim = Math.min(ini + JANELA_MS, duracaoMs);
  const [est, atv] = await Promise.all([
    pede(`/api/partidas/${gameId}/estados?de=${ini / 1000}&ate=${fim / 1000}&passo=${PASSO_S}`),
    pede(`/api/partidas/${gameId}/atividades?de=${ini / 1000}&ate=${fim / 1000}`).catch(() => ({ atividades: [] })),
  ]);
  janelas.set(ini, { quadros: est.quadros.map(enxuga), atividades: atv.atividades.map(enxugaAtv) });
}
const grava = opc('grava', '');
if (grava) {
  // Só o trecho pedido (com 1 s de folga de cada lado): o arquivo de teste fica pequeno.
  const corta = [...janelas].map(([ini, d]) => [
    ini,
    {
      quadros: d.quadros.filter((q) => q.t_ms >= de - 1000 && q.t_ms <= ate + 1000),
      atividades: d.atividades.filter((a) => a.fim_ms >= de - 5000 && a.t_ms <= ate + 5000),
    },
  ]);
  writeFileSync(grava, JSON.stringify({ game_id: Number(gameId), duracao_ms: duracaoMs, de, ate, times, nomes, janelas: corta }));
  console.log(`janelas guardadas em ${grava}`);
}

function imprime(titulo, res) {
  console.log(`\n${titulo}`);
  for (const v of VELOCIDADES_VALIDADAS) {
    const r = res[v];
    const tipos = Object.entries(r.contagem).map(([k, n]) => `${k} ${n}`).join(', ') || 'nenhum';
    const av = Object.entries(r.contagemAvisos).map(([k, n]) => `${k} ${n}`).join(', ') || 'nenhum';
    console.log(`  ${v}×: ${r.quadros} quadros de tela · glitches: ${r.glitches.length} (${tipos}) · avisos: ${av}`);
    for (const g of r.glitches.slice(0, 8)) console.log(`     ${relogio(g.t_ms)} ${g.tipo} ${g.ids.map((i) => nomes[i] || i).join(' × ')} (${g.valor})`);
    if (r.glitches.length > 8) console.log(`     … e mais ${r.glitches.length - 8}`);
  }
}

console.log(`Partida ${gameId} · ${relogio(de)}–${relogio(ate)} · servidor ${servidor}`);
if (args.includes('--antes')) imprime('ANTES (leque + etiqueta de grupo):', validaDesenho({ janelas, duracaoMs, times, nomes, de, ate, fabrica: criaArranjoLeque, rotulo: rotuloAntes }));
const depois = validaDesenho({ janelas, duracaoMs, times, nomes, de, ate });
imprime('AGORA (anel + traço + retrato deslocado):', depois);
const total = VELOCIDADES_VALIDADAS.reduce((s, v) => s + depois[v].glitches.length, 0);
process.exit(total ? 1 : 0);
