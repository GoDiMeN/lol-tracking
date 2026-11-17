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
};

// acompanha liga o SSE de uma execução. `ao` é {tipo: fn(dados)}; devolve a função que desliga.
// O "fim" fecha a conexão: sem isso o EventSource reconectaria sozinho para sempre.
export function acompanha(execucaoId, ao) {
  const fonte = new EventSource(`/api/execucoes/${encodeURIComponent(execucaoId)}/eventos`);
  for (const tipo of ['progresso', 'concluido', 'aviso', 'erro', 'fim']) {
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
    ao.erro?.({ texto: 'a conexão com o servidor caiu durante o download' });
    ao.fim?.({});
  };
  return () => fonte.close();
}
