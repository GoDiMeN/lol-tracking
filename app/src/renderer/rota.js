// A rota mora no hash, como no Make Livros: o app funciona igual servido pelo Go e o "voltar" do
// Electron leva à tela anterior sem servidor de rotas. Funções puras (rota.test.mjs).
//
//   #jogadores[/puuid]                   busca e partidas do jogador
//   #rastreio[/id]                       fila de rastreio (sem id: o rodando ou o mais novo)
//   #partidas                            partidas rastreadas
//   #partida/game_id[/participante[/n]]  partida rastreada (n = abate escolhido)
//   #estudo[/id[/indicador]]             estudos
//   #replay/game_id[/segundos]           mapa com play, tela cheia
//   #ia/game_id/participante[/versao]    relatório da partida com IA
//   #especialista[/relatorio/game_id/participante[/versao] | /sintese/patch/elo/rota]  Especialista Lane (Estudo)
//   #ranking[/patch/elo/recorte[/modo]]     ranking do Lane (Estudo); recorte = rota (JUNGLE) ou c<id do campeão>
//   #configuracoes

export const VISTAS = ['jogadores', 'rastreio', 'partidas', 'partida', 'estudo', 'especialista', 'ranking', 'replay', 'ia', 'configuracoes'];

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
  const [vista, a, b, c, d, e] = h.split('/').filter(Boolean).map(decodifica);
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
    case 'ia':
      if (inteiro(a) === null) return { vista: 'partidas' };
      return { vista, gameId: inteiro(a), participante: inteiro(b), versao: inteiro(c) };
    case 'especialista':
      if (a === 'relatorio' && inteiro(b) !== null && inteiro(c) !== null) return { vista, tipo: 'relatorio', gameId: inteiro(b), participante: inteiro(c), versao: inteiro(d) };
      if (a === 'sintese' && b && c && d) return { vista, tipo: 'sintese', patch: b, elo: c, rota: d, versao: inteiro(e) };
      return { vista, tipo: null };
    case 'ranking':
      return { vista, patch: a || null, elo: b || null, recorte: c || null, modo: d === 'melhor' || d === 'subindo' ? d : null };
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
    case 'ia':
      poe(r.gameId, r.participante, r.versao);
      break;
    case 'especialista':
      if (r.tipo === 'relatorio') poe('relatorio', r.gameId, r.participante, r.versao);
      else if (r.tipo === 'sintese') poe('sintese', r.patch, r.elo, r.rota, r.versao);
      break;
    case 'ranking':
      poe(r.patch, r.elo, r.recorte, r.modo);
      break;
    case 'replay':
      poe(r.gameId, r.t === null || r.t === undefined ? null : Math.round(r.t * 10) / 10);
      break;
  }
  return '#' + partes.map(encodeURIComponent).join('/');
}

// Qual item do menu fica aceso em cada vista.
export function itemDoMenu(vista) {
  if (vista === 'partida' || vista === 'replay' || vista === 'ia') return 'partidas';
  if (vista === 'especialista' || vista === 'ranking') return 'estudo';
  return vista;
}
