// O ESPECIALISTA LANE (área de Estudo): as partidas de referência da coleta por elo viram relatórios
// de referência (o que o jogador fez e em resposta a quê, as decisões que pesaram com a regra tirada
// de cada uma, os erros dele) e, por patch × elo × rota, uma síntese que é o material da voz
// "Especialista Lane" na análise e na conversa. Tudo a pedido (cada relatório e cada síntese é uma
// chamada à IA). O NotebookLM é espelho: o botão manda os Markdown ao caderno.
//
//   #especialista                                      painel: recortes, partidas, espelho
//   #especialista/relatorio/game/participante[/versao] um relatório de referência
//   #especialista/sintese/patch/elo/rota[/versao]      a síntese de um recorte

import { api, acompanha } from '../api.js';
import { duracao } from '../formato.js';
import { el, botao, link, pilula, cartao, carregando, erroNaTela, toast, partidaComNomes } from '../ui.js';
import { segmentos, rotaDoInstante } from '../ia.js';
import { quadroDoMapa } from '../quadro-ia.js';
import { nomeEloEsp, nomeRotaEsp, nomeRecorte, estadoRelatorio, estadoSintese, avisoRastreados, rotuloRegra, resumoEspelho, porRecorte } from '../especialista.js';

export async function desenha(raiz, rota, ctx) {
  if (rota.tipo === 'relatorio') return relatorio(raiz, rota, ctx);
  if (rota.tipo === 'sintese') return sintese(raiz, rota, ctx);
  return painel(raiz, ctx);
}

const EXPLICA =
  'Cada partida da coleta por elo tem um jogador de referência (o escolhido pela coleta). O relatório de referência conta o que ele fez e em resposta a quê, ' +
  'as decisões que mais pesaram com a regra tirada de cada uma e os erros dele, só com os fatos que o app calculou. A síntese junta os relatórios de um patch, ' +
  'elo e rota: regras que aparecem em mais de um jogador e hipóteses de um caso só, sempre com o tamanho da amostra. A síntese é o material da voz ' +
  '"Especialista Lane" na análise com IA. Cada relatório e cada síntese é uma chamada ao Claude Code (gasta cota): nada roda sozinho.';

// --- Cartão na lista de Estudo -----------------------------------------------------------------

export function montaCartao() {
  const corpo = el('div', 'pad');
  const raiz = cartao('Especialista Lane', link('Abrir', '#especialista', 'btn mini'), corpo, 'cartao-especialista');
  corpo.append(el('p', 'sub', 'Lendo…'));
  let vivo = true;
  api
    .especialista()
    .then((p) => {
      if (!vivo) return;
      const linhas = p.recortes.map((r) => {
        const e = estadoSintese(r);
        const a = link('', `#especialista`, 'linha-lista');
        a.append(el('b', '', nomeRecorte(r)), el('span', 'sub', `${r.partidas} partida(s), ${r.relatorios} relatório(s)`), pilula(e.texto, e.classe));
        return a;
      });
      corpo.replaceChildren(
        el('p', 'sub', 'Relatórios de referência e sínteses por patch, elo e rota, feitos a partir das partidas da coleta por elo.'),
        linhas.length ? el('div', '', linhas) : el('p', 'sub', 'Ainda sem partidas de referência: rode a coleta por elo.'),
        el('p', 'sub', resumoEspelho(p.espelho)),
      );
    })
    .catch((err) => vivo && corpo.replaceChildren(erroNaTela(err)));
  return { el: raiz, limpa: () => (vivo = false) };
}

// --- Painel ------------------------------------------------------------------------------------

async function painel(raiz, ctx) {
  const cab = el('div', 'linha cab-tela', [link('← Estudo', '#estudo', 'voltar'), el('h1', 'titulo-tela inline', 'Especialista Lane')]);
  raiz.append(cab, carregando('Carregando o Especialista Lane…'));
  const desligas = new Set();
  let vivo = true;

  async function recarrega() {
    let p;
    let estadoIA;
    try {
      [p, estadoIA] = await Promise.all([api.especialista(), api.estadoIA().catch(() => null)]);
    } catch (err) {
      raiz.replaceChildren(cab, erroNaTela(err));
      return;
    }
    if (!vivo) return;
    const iaOk = Boolean(estadoIA?.disponivel);
    const partes = [cab, progresso, el('p', 'sub', EXPLICA)];
    if (!iaOk) partes.push(el('p', 'tx-cr', estadoIA?.motivo || 'Não deu para saber se a IA está disponível.'));
    const grupos = porRecorte(p.referencias);
    if (!p.recortes.length) partes.push(cartao('Sem partidas de referência', null, [el('p', 'sub pad', 'Rode a coleta por elo (Estudo → Aprendizado por elo) para ter jogadores de referência.')]));
    for (const rec of p.recortes) partes.push(cartaoRecorte(rec, grupos.get(`${rec.patch}|${rec.elo}|${rec.rota}`) || [], iaOk));
    const semRecorte = p.referencias.filter((r) => !r.patch || !r.rota || !r.elo_tier);
    if (semRecorte.length) partes.push(cartao('Partidas de referência sem patch, elo ou rota', null, [tabelaReferencias(semRecorte, iaOk)]));
    partes.push(cartaoEspelho(p.espelho));
    raiz.replaceChildren(...partes);
    for (const r of p.referencias) if (r.execucao) segue(r.execucao.id, `relatório de ${r.campeao} (partida ${r.game_id})`);
    for (const r of p.recortes) if (r.execucao) segue(r.execucao.id, `síntese ${nomeRecorte(r)}`);
    if (p.espelho.execucao) segue(p.espelho.execucao.id, 'envio ao NotebookLM');
  }

  // Uma linha de progresso por execução, no topo; ao fim, recarrega o painel.
  const progresso = el('div', 'progresso-esp');
  const seguidas = new Set();
  function segue(execId, oQue) {
    if (seguidas.has(execId)) return;
    seguidas.add(execId);
    const linha = el('p', 'sub', `${oQue}: começando…`);
    const cancela = botao('Cancelar', 'mini perigo', () => api.cancelaExecucao(execId).catch(() => {}));
    const caixa = el('div', 'linha', [linha, cancela]);
    progresso.append(caixa);
    const d = acompanha(
      execId,
      {
        contexto: (x) => {
          const e = x.enviado || {};
          linha.textContent = `${oQue}: ${((e.caracteres_sistema || 0) + (e.caracteres_mensagem || 0)).toLocaleString('pt-BR')} caracteres para a IA (${x.modelo || 'modelo padrão'})…`;
        },
        na_fila: (x) => (linha.textContent = `${oQue}: ${x.texto}`),
        trecho: (x) => (linha.textContent = `${oQue}: a IA está escrevendo… ${(x.caracteres || 0).toLocaleString('pt-BR')} caracteres`),
        fonte: (x) => (linha.textContent = `${oQue}: ${x.titulo} — ${x.acao}${x.erro ? ` (${x.erro})` : ''}`),
        concluido: () => toast(`${oQue}: pronto.`),
        cancelado: () => (linha.textContent = `${oQue}: cancelado.`),
        erro: (x) => {
          linha.textContent = `${oQue}: ${x.texto}`;
          linha.className = 'tx-cr';
        },
        fim: () => {
          desligas.delete(d);
          cancela.remove();
          setTimeout(() => caixa.remove(), 8000);
          setTimeout(() => vivo && recarrega(), 400);
        },
      },
      { textoQueda: 'a conexão com o servidor caiu' },
    );
    desligas.add(d);
  }

  function cartaoRecorte(rec, refs, iaOk) {
    const e = estadoSintese(rec);
    const dir = el('span', 'linha');
    if (rec.sintese?.estado === 'concluido') dir.append(link('Ler síntese', `#especialista/sintese/${rec.patch}/${rec.elo}/${rec.rota}`, 'btn mini'));
    const refaz = botao(rec.sintese ? 'Refazer síntese' : 'Fazer síntese', 'mini pri', async () => {
      refaz.disabled = true;
      try {
        const r = await api.refazSinteseEsp(rec);
        segue(r.execucao.id, `síntese ${nomeRecorte(rec)}`);
      } catch (err) {
        toast(err.message, 'erro');
        refaz.disabled = false;
      }
    }, 'uma chamada à IA: junta os relatórios concluídos deste recorte');
    refaz.disabled = !iaOk || !rec.relatorios || Boolean(rec.execucao);
    dir.append(refaz);
    const titulo = el('div', '', [
      el('h2', '', rec.nome || nomeRecorte(rec)),
      el('span', 'sub', ` ${rec.partidas} partida(s), ${rec.jogadores} jogador(es), ${rec.vitorias} vitória(s), ${rec.relatorios} relatório(s) concluído(s) `),
      pilula(e.texto, e.classe),
    ]);
    return cartao(titulo, dir, [tabelaReferencias(refs, iaOk)]);
  }

  function tabelaReferencias(refs, iaOk) {
    if (!refs.length) return el('p', 'sub pad', 'Nenhuma partida de referência neste recorte (a síntese ficou de antes).');
    const tab = el('table', 'tabela compacta');
    tab.append(el('thead', '', [el('tr', '', ['Campeão', 'Elo', 'Resultado', 'Rastreados', 'Papel', 'Relatório', ''].map((t) => el('th', '', t)))]));
    const corpo = el('tbody');
    for (const r of refs) {
      const e = estadoRelatorio(r);
      const acoes = el('div', 'linha');
      if (r.relatorio?.estado === 'concluido') acoes.append(link('Ler', `#especialista/relatorio/${r.game_id}/${r.participante_id}`, 'btn mini'));
      if (e.botao) {
        const b = botao(e.botao, r.relatorio ? 'mini' : 'mini pri', async () => {
          b.disabled = true;
          try {
            const x = await api.geraRelatorioEsp(r.game_id, r.participante_id);
            segue(x.execucao.id, `relatório de ${r.campeao} (partida ${r.game_id})`);
          } catch (err) {
            toast(err.message, 'erro');
            b.disabled = false;
          }
        }, 'uma chamada à IA, só com os fatos desta partida');
        b.disabled = !iaOk;
        acoes.append(b);
      }
      acoes.append(link('Mapa', `#replay/${r.game_id}`, 'btn mini'), link('Partida', `#partida/${r.game_id}/${r.participante_id}`, 'btn mini'));
      const aviso = avisoRastreados(r.rastreados);
      corpo.append(
        el('tr', '', [
          el('td', '', [el('b', '', r.campeao), el('div', 'sub', `partida ${r.game_id} · ${duracao(r.duracao_s)}`)]),
          el('td', '', nomeEloEsp(r.elo)),
          el('td', '', [pilula(r.venceu ? 'vitória' : 'derrota', r.venceu ? 'ok' : 'cr')]),
          el('td', '', [el('span', '', `${r.rastreados} de 10`), aviso ? el('div', 'sub', 'posição dos outros por minuto') : null]),
          el('td', 'sub', r.papel || '—'),
          el('td', '', [pilula(e.texto, e.classe)]),
          el('td', '', [acoes]),
        ]),
      );
    }
    tab.append(corpo);
    return el('div', 'rolagem-x', [tab]);
  }

  function cartaoEspelho(e) {
    const campo = el('input', 'campo');
    campo.value = e.caderno || '';
    campo.placeholder = 'id do caderno (o UUID da URL do NotebookLM)';
    campo.setAttribute('aria-label', 'Id do caderno do NotebookLM');
    const guarda = botao('Guardar', 'mini', async () => {
      try {
        await api.defineCadernoEsp(campo.value.trim());
        toast('Caderno guardado.');
        recarrega();
      } catch (err) {
        toast(err.message, 'erro');
      }
    });
    const envia = botao('Enviar ao NotebookLM', 'pri', async () => {
      envia.disabled = true;
      try {
        const r = await api.enviaEspelhoEsp();
        segue(r.execucao.id, 'envio ao NotebookLM');
      } catch (err) {
        toast(err.message, 'erro');
        envia.disabled = false;
      }
    }, 'manda os relatórios e as sínteses novos ou refeitos; os já enviados não se repetem');
    envia.disabled = !e.ferramenta || !e.caderno || !e.pendentes || Boolean(e.execucao);
    const itens = el(
      'ul',
      'lista-ia',
      e.itens.map((it) =>
        el('li', '', [
          pilula(it.estado === 'enviado' ? 'no caderno' : it.estado === 'desatualizado' ? `v${it.versao_enviada} no caderno, há v${it.versao}` : 'não enviado', it.estado === 'enviado' ? 'ok' : 'at'),
          el('span', '', ` ${it.titulo}`),
          it.fonte_id ? el('span', 'sub', ` · fonte ${it.fonte_id}`) : null,
          it.erro ? el('div', 'tx-cr', it.erro) : null,
        ]),
      ),
    );
    return cartao('Espelho no NotebookLM', resumoEspelho(e), [
      el('div', 'pad', [
        el('p', 'sub', 'O relatório nasce e mora no app; o caderno é cópia para ler e conversar no NotebookLM.'),
        el('div', 'linha', [campo, guarda, envia]),
        e.motivo ? el('p', e.ferramenta ? 'sub' : 'tx-cr', e.motivo) : null,
        // Qual `nlm` vale (detectado na subida do servidor: o do PATH, o de LOL_NLM ou o uvx).
        e.comando ? el('p', 'sub', `ferramenta: ${e.comando}${e.versao ? ` (versão ${e.versao})` : ''}`) : null,
        e.itens.length ? itens : el('p', 'sub', 'Nada para enviar ainda: gere relatórios e sínteses.'),
        el('p', 'sub', e.sai_da_maquina),
        el('details', '', [el('summary', 'sub', 'exportar à mão'), el('p', 'sub', e.como_exportar)]),
      ]),
    ]);
  }

  await recarrega();
  return () => {
    vivo = false;
    for (const d of desligas) d();
  };
}

// --- Texto com instantes -----------------------------------------------------------------------

function textoCom(tag, classe, s, gameId) {
  const e = el(tag, classe);
  for (const x of segmentos(s || '', gameId)) {
    if (x.tipo === 'texto') e.append(document.createTextNode(x.v));
    else if (x.tipo === 'partida') e.append(link(x.v, `#partida/${x.gameId}`));
    else {
      const a = link(x.v, rotaDoInstante(x.gameId, x.ms), 'num');
      a.title = 'abrir o mapa com play neste instante';
      e.append(a);
    }
  }
  return e;
}

const fonte = (f) => (f ? el('p', 'sub fonte-ia', `fonte: ${f}`) : null);

function previaDetalhe(busca) {
  const pre = el('pre', 'previa-ia');
  const d = el('details', 'pad', [el('summary', '', 'ver o texto exato que vai para a IA'), pre]);
  d.addEventListener('toggle', () => {
    if (!d.open || pre.textContent) return;
    pre.textContent = 'montando…';
    busca()
      .then((p) => (pre.textContent = `=== PROMPT DE SISTEMA ===\n${p.sistema}\n\n=== MENSAGEM ===\n${p.mensagem}`))
      .catch((err) => (pre.textContent = err.message));
  });
  return d;
}

function usoTexto(r) {
  const env = r.enviado || {};
  const uso = r.uso || {};
  return `${((env.caracteres_sistema || 0) + (env.caracteres_mensagem || 0)).toLocaleString('pt-BR')} caracteres para o Claude Code (${(env.secoes || []).join('; ')}). ` +
    `Voltaram ${(uso.caracteres_saida || 0).toLocaleString('pt-BR')} caracteres em ${((uso.duracao_ms || 0) / 1000).toFixed(0)} s · modelo ${uso.modelo || '—'}` +
    `${uso.tokens_entrada ? ` · ${uso.tokens_entrada.toLocaleString('pt-BR')} tokens de entrada, ${uso.tokens_saida.toLocaleString('pt-BR')} de saída` : ''}.`;
}

// Segue uma execução no corpo e, no fim, recarrega a rota.
function seguindo(corpo, execId, ctx, rota, oQue) {
  const linha = el('p', '', `${oQue}: montando o texto…`);
  const cancela = botao('Cancelar', 'perigo', () => api.cancelaExecucao(execId).catch(() => {}));
  corpo.replaceChildren(cartao(oQue, cancela, [el('div', 'pad', [linha])]));
  return acompanha(execId, {
    contexto: () => (linha.textContent = `${oQue}: chamando o Claude Code…`),
    na_fila: (x) => (linha.textContent = x.texto),
    trecho: (x) => (linha.textContent = `A IA está escrevendo… ${(x.caracteres || 0).toLocaleString('pt-BR')} caracteres recebidos.`),
    erro: (x) => {
      linha.textContent = `Não deu certo: ${x.texto}`;
      linha.className = 'tx-cr';
    },
    fim: () => ctx.navega({ ...rota, versao: null }, { substitui: true }),
  });
}

// --- Relatório de referência --------------------------------------------------------------------

async function relatorio(raiz, rota, ctx) {
  const { gameId, participante: pid } = rota;
  raiz.append(carregando('Carregando o relatório de referência…'));
  let dados;
  let info;
  let estadoIA;
  try {
    [dados, info, estadoIA] = await Promise.all([api.relatorioEsp(gameId, pid, rota.versao || 0), partidaComNomes(gameId), api.estadoIA().catch(() => null)]);
  } catch (err) {
    raiz.replaceChildren(el('h1', 'titulo-tela', 'Relatório de referência'), erroNaTela(err));
    return;
  }
  const r = dados.relatorio;
  let desliga = null;
  const titulo = r ? `${r.campeao} · ${nomeEloEsp(r.elo)} · ${nomeRotaEsp(r.rota)} · patch ${r.patch} · ${r.venceu ? 'vitória' : 'derrota'}` : `partida ${gameId}`;
  const refaz = botao(r ? 'Refazer relatório' : 'Gerar relatório', r ? '' : 'pri', async () => {
    refaz.disabled = true;
    try {
      const x = await api.geraRelatorioEsp(gameId, pid);
      desliga = seguindo(corpo, x.execucao.id, ctx, rota, 'Relatório de referência');
    } catch (err) {
      toast(err.message, 'erro');
      refaz.disabled = false;
    }
  });
  refaz.disabled = !estadoIA?.disponivel || Boolean(dados.execucao);
  const acoes = el('div', 'linha acoes-dir', [link('Mapa com play', `#replay/${gameId}`, 'btn'), link('Partida', `#partida/${gameId}/${pid}`, 'btn'), refaz]);
  if (dados.versoes.length > 1) {
    const sel = el('select', 'campo');
    sel.setAttribute('aria-label', 'Versão do relatório');
    for (const v of dados.versoes) {
      const o = el('option', '', `versão ${v.versao} · ${v.estado}`);
      o.value = String(v.versao);
      o.selected = r && v.versao === r.versao;
      sel.append(o);
    }
    sel.addEventListener('change', () => ctx.navega({ ...rota, versao: Number(sel.value) }));
    acoes.prepend(sel);
  }
  const cab = el('div', 'linha cab-tela', [link('← Especialista Lane', '#especialista', 'voltar'), el('h1', 'titulo-tela inline', `Relatório de referência · ${titulo}`), acoes]);
  const corpo = el('div', 'relatorio-ia');
  raiz.replaceChildren(cab, corpo);
  if (dados.execucao) {
    desliga = seguindo(corpo, dados.execucao.id, ctx, rota, 'Relatório de referência');
    return () => desliga?.();
  }
  if (!r) {
    corpo.append(cartao('Ainda sem relatório', null, [el('div', 'pad', [el('p', '', 'Clique em "Gerar relatório".')]), previaDetalhe(() => api.previaRelatorioEsp(gameId, pid))]));
    return;
  }
  if (r.estado !== 'concluido' || !r.conteudo) {
    corpo.append(cartao(`Versão ${r.versao}: ${r.estado}`, null, [el('p', 'tx-cr pad', r.erro || 'sem conteúdo')]));
    return;
  }
  const c = r.conteudo;
  const q = c.quem;
  const quadro = (x) => (x ? quadroDoMapa({ gameId, ms: x.t_ms, foco: pid, info, anotacao: x, legenda: x.legenda }) : el('div', 'sub', 'sem quadro'));
  const linkT = (t, ms) => {
    const a = link(t, rotaDoInstante(gameId, ms), 'num tempo');
    a.title = 'abrir o mapa com play neste instante';
    return a;
  };
  const partes = [];
  const aviso = avisoRastreados(q.rastreados);
  partes.push(
    cartao('Quem', `versão ${r.versao} · ${r.modelo || ''}`, [
      el('div', 'pad', [
        el('p', '', `${q.campeao} (${nomeRotaEsp(q.rota)}, time ${q.time === 200 ? 'vermelho' : 'azul'}), ${nomeEloEsp(q.elo)}, patch ${q.patch}, ${q.venceu ? 'vitória' : 'derrota'} em ${duracao(q.duracao_s)}.`),
        q.frase_papel ? el('p', 'sub', `Papel e estilo (por regra): ${q.frase_papel}`) : null,
        aviso ? el('p', 'tx-at', `Atenção: ${aviso}.`) : null,
        textoCom('p', '', c.resumo, gameId),
      ]),
    ]),
  );
  if (c.fases.length) {
    partes.push(el('h2', 'secao-ia', 'Plano de jogo por fase'));
    const nomes = { rotas: 'Fase de rotas', meio: 'Meio de jogo', final: 'Final de jogo' };
    for (const f of c.fases) {
      partes.push(
        cartao(nomes[f.fase] || f.fase, null, [
          el('div', 'pad', [
            textoCom('p', '', f.plano, gameId),
            el('ul', 'lista-ia', f.acoes.map((a) => el('li', '', [linkT(a.t, a.t_ms), textoCom('span', '', ` ${a.fez}`, gameId), a.em_resposta_a ? textoCom('p', 'sub', `Em resposta a: ${a.em_resposta_a}`, gameId) : null]))),
          ]),
        ]),
      );
    }
  }
  partes.push(el('h2', 'secao-ia', 'Decisões que mais pesaram'));
  c.decisoes.forEach((d, i) => {
    partes.push(
      el('article', 'cartao situacao-ia', [
        el('div', 'situacao-quadro', [quadro(d.quadro)]),
        el('div', 'situacao-texto', [
          el('div', 'linha', [el('span', 'situacao-n', String(i + 1)), linkT(d.t, d.t_ms), el('h3', '', d.titulo)]),
          textoCom('p', '', d.fatos, gameId),
          d.peso ? textoCom('p', 'sub', `Por que pesou: ${d.peso}`, gameId) : null,
          el('blockquote', 'pergunta-ia', [el('b', '', 'Regra: '), textoCom('span', '', d.regra, gameId)]),
          el('p', '', [pilula(d.hipotese ? 'hipótese: um caso só' : `${d.casos.length} casos nesta partida`, d.hipotese ? 'at' : 'ok'), el('span', 'sub', ` ${d.casos.join(', ')}`)]),
          fonte(d.fonte),
        ]),
      ]),
    );
  });
  if (c.erros.length) {
    partes.push(el('h2', 'secao-ia', 'Erros dele'));
    for (const e of c.erros) {
      partes.push(
        el('article', 'cartao situacao-ia', [
          el('div', 'situacao-quadro', [quadro(e.quadro)]),
          el('div', 'situacao-texto', [
            el('div', 'linha', [linkT(e.t, e.t_ms), e.custo ? el('span', 'custo-ia', ` Custo: ${e.custo}`) : null]),
            textoCom('p', '', e.fato, gameId),
            e.licao ? el('p', 'proxima-vez', [el('b', '', 'Lição: '), textoCom('span', '', e.licao, gameId)]) : null,
            fonte(e.fonte),
          ]),
        ]),
      );
    }
  }
  partes.push(
    cartao('O que ele não fez que os jogadores do usuário fazem', null, [
      el('div', 'pad', [
        el('p', 'sub', c.comparacao),
        c.nao_fez.length ? el('ul', 'lista-ia', c.nao_fez.map((n) => el('li', '', [textoCom('span', '', n.texto, gameId), n.numeros ? el('div', 'sub', n.numeros) : null]))) : el('p', 'sub', 'Nada a apontar.'),
      ]),
    ]),
  );
  if (c.nao_da_para_saber.length) partes.push(cartao('O que os dados não dizem', null, [el('ul', 'lista-ia pad', c.nao_da_para_saber.map((n) => el('li', '', n)))]));
  partes.push(
    cartao('O que saiu da máquina', null, [
      el('div', 'pad', [
        el('p', 'sub', usoTexto(r)),
        r.arquivo ? el('p', 'sub', `Markdown: conhecimento/${r.arquivo}`) : null,
        previaDetalhe(() => api.previaRelatorioEsp(gameId, pid)),
        r.avisos.length ? el('details', '', [el('summary', 'sub', `${r.avisos.length} aviso(s) da validação`), el('ul', 'lista-ia', r.avisos.map((a) => el('li', 'sub', a)))]) : null,
      ]),
    ]),
  );
  corpo.replaceChildren(...partes);
  return () => desliga?.();
}

// --- Síntese ------------------------------------------------------------------------------------

async function sintese(raiz, rota, ctx) {
  const rec = { patch: rota.patch, elo: rota.elo, rota: rota.rota };
  raiz.append(carregando('Carregando a síntese…'));
  let dados;
  let estadoIA;
  try {
    [dados, estadoIA] = await Promise.all([api.sinteseEsp(rec, rota.versao || 0), api.estadoIA().catch(() => null)]);
  } catch (err) {
    raiz.replaceChildren(el('h1', 'titulo-tela', 'Síntese'), erroNaTela(err));
    return;
  }
  let desliga = null;
  const s = dados.sintese;
  const refaz = botao(s ? 'Refazer síntese' : 'Fazer síntese', '', async () => {
    refaz.disabled = true;
    try {
      const x = await api.refazSinteseEsp(rec);
      desliga = seguindo(corpo, x.execucao.id, ctx, rota, 'Síntese');
    } catch (err) {
      toast(err.message, 'erro');
      refaz.disabled = false;
    }
  });
  refaz.disabled = !estadoIA?.disponivel || Boolean(dados.execucao);
  const acoes = el('div', 'linha acoes-dir', [refaz]);
  if (dados.versoes.length > 1) {
    const sel = el('select', 'campo');
    sel.setAttribute('aria-label', 'Versão da síntese');
    for (const v of dados.versoes) {
      const o = el('option', '', `versão ${v.versao} · ${v.estado}`);
      o.value = String(v.versao);
      o.selected = s && v.versao === s.versao;
      sel.append(o);
    }
    sel.addEventListener('change', () => ctx.navega({ ...rota, versao: Number(sel.value) }));
    acoes.prepend(sel);
  }
  const cab = el('div', 'linha cab-tela', [link('← Especialista Lane', '#especialista', 'voltar'), el('h1', 'titulo-tela inline', `Síntese · ${dados.recorte.nome}`), acoes]);
  const corpo = el('div', 'relatorio-ia');
  raiz.replaceChildren(cab, corpo);
  if (dados.execucao) {
    desliga = seguindo(corpo, dados.execucao.id, ctx, rota, 'Síntese');
    return () => desliga?.();
  }
  const comp = tabelaComparacao(dados.comparacao);
  if (!s || s.estado !== 'concluido' || !s.conteudo) {
    corpo.append(
      cartao(s ? `Versão ${s.versao}: ${s.estado}` : 'Ainda sem síntese', null, [el('p', s ? 'tx-cr pad' : 'pad', s?.erro || 'Clique em "Fazer síntese".'), previaDetalhe(() => api.previaSinteseEsp(rec))]),
      comp,
    );
    return;
  }
  const c = s.conteudo;
  const porCodigo = Object.fromEntries(c.fontes.map((f) => [f.codigo, f]));
  const fontesDe = (x) =>
    el('span', 'sub', [
      ' — ',
      ...x.relatorios.flatMap((cod, k) => {
        const f = porCodigo[cod];
        return [k ? '; ' : '', f ? link(`${f.campeao}, partida ${f.game_id}`, `#especialista/relatorio/${f.game_id}/${f.participante_id}`) : cod];
      }),
    ]);
  const regra = (x) => el('li', '', [pilula(rotuloRegra(x), x.jogadores >= 2 ? 'ok' : 'at'), el('span', '', ` ${x.texto}`), fontesDe(x)]);
  corpo.replaceChildren(
    cartao('Amostra', `versão ${s.versao} · ${s.modelo || ''}`, [
      el('div', 'pad', [el('p', 'tx-at', c.amostra), el('p', '', c.resumo), el('p', 'sub', `Arquivo de conhecimento (material da voz Especialista Lane): conhecimento/${s.arquivo}`)]),
    ]),
    cartao('Papel e estilo', null, [
      el('div', 'pad', [
        el('p', 'sub', `Contagem do app: ${Object.entries(c.contagem.papeis).map(([k, v]) => `${k} ${v}`).join(', ') || '—'}; estilos: ${Object.entries(c.contagem.estilos).map(([k, v]) => `${k} ${v}`).join(', ') || '—'} (de ${c.contagem.total}).`),
        el('p', '', c.papel_estilo),
      ]),
    ]),
    cartao('Regras que se repetem (mais de um jogador)', null, [c.regras.length ? el('ul', 'lista-ia pad', c.regras.map(regra)) : el('p', 'sub pad', `Nenhuma ainda: com ${s.jogadores} jogador(es), nenhuma regra apareceu em dois jogadores.`)]),
    cartao('Hipóteses (um caso só)', null, [c.caso_unico.length ? el('ul', 'lista-ia pad', c.caso_unico.map(regra)) : el('p', 'sub pad', 'Nenhuma.')]),
    c.erros_comuns.length ? cartao('Erros que aparecem', null, [el('ul', 'lista-ia pad', c.erros_comuns.map(regra))]) : null,
    tabelaComparacao(c.comparacao, c.mudancas_patch),
    c.o_que_falta.length ? cartao('O que a amostra ainda não diz', null, [el('ul', 'lista-ia pad', c.o_que_falta.map((o) => el('li', '', o)))]) : null,
    cartao('O que saiu da máquina', null, [
      el('div', 'pad', [
        el('p', 'sub', usoTexto(s)),
        previaDetalhe(() => api.previaSinteseEsp(rec)),
        s.avisos.length ? el('details', '', [el('summary', 'sub', `${s.avisos.length} aviso(s) da validação`), el('ul', 'lista-ia', s.avisos.map((a) => el('li', 'sub', a)))]) : null,
      ]),
    ]),
  );
  return () => desliga?.();
}

function tabelaComparacao(comp, mudancas = []) {
  if (!comp) return null;
  const corpo = [el('p', comp.suficiente ? 'sub' : 'tx-at', comp.outro ? (comp.suficiente ? `Amostra suficiente: ${comp.partidas_patch} × ${comp.partidas_outro} partidas (mínimo ${comp.minimo} por lado).` : `Amostra insuficiente: ${comp.partidas_patch} partida(s) neste patch e ${comp.partidas_outro} no ${comp.outro}; o mínimo para afirmar mudança é ${comp.minimo} por lado. Os números são crus.`) : comp.texto)];
  if (comp.linhas.length) {
    const tab = el('table', 'tabela compacta');
    tab.append(el('thead', '', [el('tr', '', ['Indicador', `Patch ${comp.patch}`, `Patch ${comp.outro}`, 'Leitura'].map((t) => el('th', '', t)))]));
    tab.append(
      el(
        'tbody',
        '',
        comp.linhas.map((l) =>
          el('tr', '', [el('td', '', l.indicador), el('td', 'num', `${l.patch} (n=${l.n_patch})`), el('td', 'num', `${l.outro} (n=${l.n_outro})`), el('td', '', [pilula(l.leitura, l.leitura === 'mudou' ? 'at' : 'n')])]),
        ),
      ),
    );
    corpo.push(el('div', 'rolagem-x', [tab]));
  }
  if (mudancas.length) corpo.push(el('ul', 'lista-ia', mudancas.map((m) => el('li', '', `${m.texto} (${m.base})`))));
  if (comp.regras_outro?.length) corpo.push(el('details', '', [el('summary', 'sub', `regras da síntese do patch ${comp.outro}`), el('ul', 'lista-ia', comp.regras_outro.map((x) => el('li', 'sub', x)))]));
  return cartao(comp.outro ? `Comparação com o patch ${comp.outro}` : 'Comparação entre patches', null, [el('div', 'pad', corpo)]);
}
