// O botão "Pausar captura" / "Retomar captura" da barra de título (pedido do dono em 2026-11-18:
// pausar na hora em que quer usar o computador). Puro: recebe o estado de GET /api/captura e diz o
// que o botão mostra; app.js liga ao DOM. Testado em captura.test.mjs.

const NOMES = { rastreio: 'rastreio', coleta: 'coleta', repescagem: 'repescagem', visao: 'visão dos times' };

function oQue(c) {
  if (!c) return 'captura';
  const nome = NOMES[c.tipo] || 'captura';
  return c.game_id ? `${nome} da partida ${c.game_id}` : `${nome} ${c.id}`;
}

// botaoDaCaptura → { visivel, texto, acao: 'pausar'|'retomar'|null, classe, dica }
export function botaoDaCaptura(estado, atalho = 'Ctrl+Alt+P') {
  const r = estado?.rodando;
  if (r) {
    if (r.pausando) {
      return { visivel: true, texto: 'Pausando…', acao: null, classe: 'pausando', dica: `${oQue(r)}: fechando o replay` };
    }
    if (r.esperando) {
      return {
        visivel: true,
        texto: 'Pausar captura',
        acao: 'pausar',
        classe: 'esperando',
        dica: `${oQue(r)} está esperando você parar de usar o computador (replay pausado; volta sozinho ${r.esperando.ocioso_para_retomar_s || 5} s depois). Pausar de vez: clique ou ${atalho}.`,
      };
    }
    return { visivel: true, texto: 'Pausar captura', acao: 'pausar', classe: 'rodando', dica: `${oQue(r)} rodando. Pausar agora (o replay fecha): clique ou ${atalho}.` };
  }
  const p = estado?.pausada;
  if (p) {
    return {
      visivel: true,
      texto: 'Retomar captura',
      acao: 'retomar',
      classe: 'pausada',
      dica: `${oQue(p)} pausada${p.motivo ? `: ${p.motivo}` : ''}. Retomar: clique ou ${atalho}.`,
    };
  }
  return { visivel: false, texto: '', acao: null, classe: '', dica: '' };
}
