// A rota mora no hash, como no Make Livros: o app funciona igual servido pelo Go e o "voltar" do
// Electron leva à tela anterior sem servidor de rotas. Funções puras (rota.test.mjs).
//
//   #jogadores[/puuid]                   busca e partidas do jogador
//   #rastreio[/id]                       fila de rastreio (sem id: o rodando ou o mais novo)
//   #partidas                            partidas rastreadas
//   #partida/game_id[/participante[/n]]  partida rastreada (n = abate escolhido)
//   #estudo[/id[/indicador]]             estudos
//   #replay/game_id[/segundos]           mapa com play, tela cheia
//   #configuracoes

export const VISTAS = ['jogadores', 'rastreio', 'partidas', 'partida', 'estudo', 'replay', 'configuracoes'];

function decodifica(p) {
  try {
    return decodeURIComponent(p);
  } catch {
    return p;
  }
}

const inteiro = (v) => (v !== undefined && /^\d+$/.test(v) ? Number(v) : null);
const numero = (v) => (v !== undefined && /^\d+(\.\d+)?$/.test(v) ? Number(v) : null);

export function lerRota(hash) {
  const h = String(hash ?? '').replace(/^#\/?/, '').trim();
  if (!h) return { vista: 'jogadores' };
  const [vista, a, b, c] = h.split('/').filter(Boolean).map(decodifica);
  switch (vista) {
    case 'jogadores':
      return { vista, puuid: a || null };
    case 'rastreio':
      return { vista, id: inteiro(a) };
    case 'partida':
      if (inteiro(a) === null) return { vista: 'partidas' };
      return { vista, gameId: inteiro(a), participante: inteiro(b), abate: inteiro(c) };
    case 'estudo':
      return { vista, id: inteiro(a), indicador: a && b ? b : null };
    case 'replay':
      if (inteiro(a) === null) return { vista: 'partidas' };
      return { vista, gameId: inteiro(a), t: numero(b) };
    case 'partidas':
    case 'configuracoes':
      return { vista };
  }
  return { vista: 'jogadores' };
}

export function montaRota(r) {
  const partes = [r.vista];
  const poe = (...vs) => {
    for (const v of vs) {
      if (v === null || v === undefined || v === '') return;
      partes.push(String(v));
    }
  };
  switch (r.vista) {
    case 'jogadores':
      poe(r.puuid);
      break;
    case 'rastreio':
      poe(r.id);
      break;
    case 'partida':
      poe(r.gameId, r.participante, r.abate);
      break;
    case 'estudo':
      poe(r.id, r.indicador);
      break;
    case 'replay':
      poe(r.gameId, r.t === null || r.t === undefined ? null : Math.round(r.t * 10) / 10);
      break;
  }
  return '#' + partes.map(encodeURIComponent).join('/');
}

// Qual item do menu fica aceso em cada vista.
export function itemDoMenu(vista) {
  if (vista === 'partida' || vista === 'replay') return 'partidas';
  return vista;
}
