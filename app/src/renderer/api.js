// A única porta do renderer para o servidor Go. As rotas estão em docs/arquitetura/contrato-http.md;
// mudou lá, muda aqui. A página é servida pelo próprio servidor, então a API tem a mesma origem.

export class ErroApi extends Error {
  constructor(status, mensagem, corpo) {
    super(mensagem);
    this.status = status;
    this.corpo = corpo;
  }
}

export async function pede(metodo, caminho, corpo, fetchFn = globalThis.fetch) {
  const opcoes = { method: metodo, headers: {} };
  if (corpo !== undefined) {
    opcoes.headers['Content-Type'] = 'application/json';
    opcoes.body = JSON.stringify(corpo);
  }
  let r;
  try {
    r = await fetchFn(caminho, opcoes);
  } catch {
    throw new ErroApi(0, 'o servidor do LoL Tracking não respondeu');
  }
  if (r.status === 204) return null;
  let dados = null;
  try {
    dados = await r.json();
  } catch {
    dados = null;
  }
  if (!r.ok) throw new ErroApi(r.status, dados?.erro || `o servidor respondeu ${r.status}`, dados);
  return dados;
}

export const api = {
  estado: () => pede('GET', '/api/estado'),
  recentes: () => pede('GET', '/api/jogadores?limite=8'),
  busca: (texto) => pede('POST', '/api/jogadores/buscar', { texto }),
  partidas: (puuid, inicio, quantidade) =>
    pede('GET', `/api/jogadores/${encodeURIComponent(puuid)}/partidas?inicio=${inicio}&quantidade=${quantidade}`),
  baixa: (gameId) => pede('POST', `/api/partidas/${gameId}/replay/baixar`),
  dados: () => pede('GET', '/api/dados'),
  jogadores: () => pede('GET', '/api/jogadores?limite=50'),
  jogador: (puuid) => pede('GET', `/api/jogadores/${encodeURIComponent(puuid)}`),

  // Partida e rastreio (tarefa 2)
  partida: (gameId) => pede('GET', `/api/partidas/${gameId}`),
  rastreadas: () => pede('GET', '/api/partidas/rastreadas'),
  rastrear: (gameId, corpo) => pede('POST', `/api/partidas/${gameId}/rastreio`, corpo),
  rastreios: (limite = 50) => pede('GET', `/api/rastreios?limite=${limite}`),
  rastreio: (id) => pede('GET', `/api/rastreios/${id}`),
  pausa: (id) => pede('POST', `/api/rastreios/${id}/pausar`),
  retoma: (id) => pede('POST', `/api/rastreios/${id}/retomar`),
  cancela: (id) => pede('POST', `/api/rastreios/${id}/cancelar`),
  amostras: (gameId, pid, de, ate, passo) =>
    pede('GET', `/api/partidas/${gameId}/amostras?participante=${pid}&de=${de}&ate=${ate}&passo=${passo}&limite=20000`),
  estados: (gameId, de, ate, passo) => pede('GET', `/api/partidas/${gameId}/estados?de=${de}&ate=${ate}&passo=${passo}`),
  momento: (gameId, t) => pede('GET', `/api/partidas/${gameId}/momento?t=${t}`),
  eventos: (gameId) => pede('GET', `/api/partidas/${gameId}/eventos`),
  cadeia: (gameId, pid, tipos = '') =>
    pede('GET', `/api/partidas/${gameId}/cadeia?participante=${pid}${tipos ? `&tipos=${tipos}` : ''}`),

  // Análise (tarefa 4)
  zonas: () => pede('GET', '/api/mapa/zonas'),
  vocabulario: () => pede('GET', '/api/indicadores'),
  analise: (gameId) => pede('GET', `/api/partidas/${gameId}/analise`),
  reanalisa: (gameId) => pede('POST', `/api/partidas/${gameId}/analise`),
  abates: (gameId) => pede('GET', `/api/partidas/${gameId}/abates`),
  indicadores: (gameId, pid) => pede('GET', `/api/partidas/${gameId}/indicadores?participante=${pid}`),
  saidas: (gameId, pid) => pede('GET', `/api/partidas/${gameId}/saidas-de-rota?participante=${pid}`),
  atividades: (gameId, de, ate) => pede('GET', `/api/partidas/${gameId}/atividades?de=${de}&ate=${ate}`),
  cs: (gameId, pid) => pede('GET', `/api/partidas/${gameId}/cs?participante=${pid}`),

  // Estudo (tarefa 4)
  estudos: () => pede('GET', '/api/estudos'),
  criaEstudo: (corpo) => pede('POST', '/api/estudos', corpo),
  estudo: (id) => pede('GET', `/api/estudos/${id}`),
  mudaEstudo: (id, corpo) => pede('PATCH', `/api/estudos/${id}`, corpo),
  apagaEstudo: (id) => pede('DELETE', `/api/estudos/${id}`),
  sugestoes: (id) => pede('GET', `/api/estudos/${id}/sugestoes`),
  poePartida: (id, gameId, anotacao) => pede('POST', `/api/estudos/${id}/partidas/${gameId}`, { anotacao }),
  anota: (id, gameId, anotacao) => pede('PUT', `/api/estudos/${id}/partidas/${gameId}`, { anotacao }),
  tiraPartida: (id, gameId) => pede('DELETE', `/api/estudos/${id}/partidas/${gameId}`),
  comparacao: (id) => pede('GET', `/api/estudos/${id}/comparacao`),
  composicao: (id, nome) => pede('GET', `/api/estudos/${id}/indicadores/${encodeURIComponent(nome)}`),
};

export const imagemCampeao = (id) => `/api/imagens/campeao/${Number(id) || 0}`;
export const imagemItem = (id) => `/api/imagens/item/${Number(id) || 0}`;

// acompanha liga o SSE de uma execução. `ao` é {tipo: fn(dados)}; devolve a função que desliga.
// O "fim" fecha a conexão: sem isso o EventSource reconectaria sozinho para sempre.
// Os tipos ouvidos são os de `ao` mais os comuns (aviso, erro, fim): serve ao download e ao rastreio.
export function acompanha(execucaoId, ao, { textoQueda = 'a conexão com o servidor caiu durante o download' } = {}) {
  const fonte = new EventSource(`/api/execucoes/${encodeURIComponent(execucaoId)}/eventos`);
  const tipos = new Set([...Object.keys(ao), 'aviso', 'erro', 'fim']);
  for (const tipo of tipos) {
    fonte.addEventListener(tipo, (ev) => {
      let dados = {};
      try {
        dados = JSON.parse(ev.data);
      } catch {
        /* evento sem corpo legível: segue com {} */
      }
      if (tipo === 'fim') fonte.close();
      ao[tipo]?.(dados);
    });
  }
  // Erro de conexão antes do "fim" (servidor caiu, execução expirou): avisa uma vez e para.
  fonte.onerror = () => {
    if (fonte.readyState === EventSource.CLOSED) return;
    fonte.close();
    ao.erro?.({ texto: textoQueda });
    ao.fim?.({});
  };
  return () => fonte.close();
}
