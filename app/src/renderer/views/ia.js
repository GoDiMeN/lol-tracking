// Relatório da partida com IA (#ia/game_id/participante[/versao]). A IA só entra aqui a pedido da
// pessoa (botão "Analisar com IA"): o servidor monta o texto de fatos da partida, chama o Claude
// Code da máquina sem ferramentas, valida a resposta e guarda por partida + jogador + versão. O
// relatório abre com as SITUAÇÕES que mais custaram, cada uma como uma consulta ao especialista
// (fatos, pergunta, o que cada voz com material diz, a leitura da IA, a próxima vez, as repetições),
// com o quadro do mapa daquele instante desenhado pelo app.

import { api, acompanha } from '../api.js';
import { posicao, duracao } from '../formato.js';
import { el, botao, link, pilula, cartao, retrato, carregando, erroNaTela, partidaComNomes, toast } from '../ui.js';
import { segmentos, rotaDoInstante, separaVozes, perguntaDaSituacao, linhasMaterial, estadoDaVoz, textoEloComparacao } from '../ia.js';
import { quadroDoMapa } from '../quadro-ia.js';
import { abreConversa } from '../conversa-ia.js';

export async function desenha(raiz, rota, ctx) {
  const gameId = rota.gameId;
  raiz.append(carregando('Carregando o relatório…'));
  let info;
  let estadoIA;
  let dados;
  let pid = rota.participante;
  try {
    info = await partidaComNomes(gameId);
    if (!pid || !info.porId[pid]) pid = info.partida.participantes[0].participante_id;
    [estadoIA, dados] = await Promise.all([api.estadoIA().catch(() => null), api.relatorioIA(gameId, pid, rota.versao || 0)]);
  } catch (err) {
    raiz.replaceChildren(el('h1', 'titulo-tela', 'Análise com IA'), erroNaTela(err));
    return;
  }
  const jog = info.porId[pid];
  let desliga = null;

  // --- Cabeçalho ---
  const cab = el('div', 'linha cab-tela');
  cab.append(
    link('← Partida', `#partida/${gameId}/${pid}`, 'voltar'),
    el('h1', 'titulo-tela inline', `Análise com IA · ${jog.campeao}${jog.posicao ? ` · ${posicao(jog.posicao)}` : ''} · ${jog.venceu ? 'vitória' : 'derrota'} · ${duracao(info.partida.duracao_s)}`),
  );
  const rel = dados.relatorio;
  const rodando = Boolean(dados.execucao);
  const analisar = botao(rel ? 'Refazer análise com IA' : 'Analisar com IA', rel ? '' : 'pri', () => comeca());
  analisar.disabled = rodando || !estadoIA?.disponivel;
  const conversar = botao('Conversar com a IA', '', () => abreConversa({ gameId, pid, info }));
  conversar.disabled = !estadoIA?.disponivel;
  const acoes = el('div', 'linha acoes-dir', [conversar, analisar]);
  if (dados.versoes.length > 1) {
    const sel = el('select', 'campo');
    sel.setAttribute('aria-label', 'Versão do relatório');
    for (const v of dados.versoes) {
      const o = el('option', '', `versão ${v.versao} · ${v.estado}${v.modelo ? ` · ${v.modelo}` : ''}`);
      o.value = String(v.versao);
      o.selected = rel && v.versao === rel.versao;
      sel.append(o);
    }
    sel.addEventListener('change', () => ctx.navega({ vista: 'ia', gameId, participante: pid, versao: Number(sel.value) }));
    acoes.prepend(sel);
  }
  cab.append(acoes);

  // --- Seletor de jogador ---
  const seletor = el('div', 'seletor');
  seletor.setAttribute('role', 'group');
  seletor.setAttribute('aria-label', 'Jogador em foco');
  for (const time of [100, 200]) {
    const g = el('div', `seletor-time ${time === 100 ? 'azul' : 'vermelho'}`);
    for (const pt of info.partida.participantes.filter((x) => x.time === time)) {
      const b = el('button', `chip-jogador${pt.participante_id === pid ? ' on' : ''}`);
      b.type = 'button';
      b.setAttribute('aria-pressed', String(pt.participante_id === pid));
      b.append(retrato(pt, 26), el('span', '', pt.campeao));
      b.addEventListener('click', () => ctx.navega({ vista: 'ia', gameId, participante: pt.participante_id }));
      g.append(b);
    }
    seletor.append(g);
  }

  const aviso = el('p', 'sub');
  if (!estadoIA) aviso.textContent = 'Não deu para saber se a IA está disponível.';
  else if (!estadoIA.disponivel) {
    aviso.textContent = estadoIA.motivo;
    aviso.className = 'tx-cr';
  } else {
    aviso.textContent = `${estadoIA.implementacao === 'falso' ? 'IA falsa (modo teste)' : `Claude Code ${estadoIA.versao}`} · modelo da análise: ${estadoIA.modelos?.analise || '—'} · a IA só lê o texto de fatos que o app monta; não tem ferramentas.`;
  }
  const corpo = el('div', 'relatorio-ia');
  const painelVozes = el('div', 'vozes-ia');
  raiz.replaceChildren(cab, seletor, aviso, painelVozes, corpo);
  desenhaVozes();

  if (rodando) segue(dados.execucao.id);
  else if (rel) desenhaRelatorio(corpo, rel);
  else corpo.append(vazioSemRelatorio());

  // --- Quem fala nesta análise: o elo de comparação (escolhido aqui, guardado por jogador; o
  // padrão vem da API da Riot quando há chave) e, por voz, o material ou o que falta. ---
  async function desenhaVozes() {
    painelVozes.replaceChildren(el('p', 'sub', 'Lendo as referências das vozes…'));
    let r;
    try {
      r = await api.referenciasIA(gameId, pid);
    } catch (err) {
      painelVozes.replaceChildren(el('p', 'tx-cr', `Referências das vozes: ${err.message}`));
      return;
    }
    const sel = el('select', 'campo');
    sel.setAttribute('aria-label', 'Comparar com o elo');
    const padrao = el('option', '', 'padrão (Riot ou coleta)');
    padrao.value = '';
    sel.append(padrao);
    for (const o of r.elo.opcoes) {
      const op = el('option', '', o.nome);
      op.value = o.elo;
      op.selected = r.elo.escolhido === o.elo;
      sel.append(op);
    }
    sel.addEventListener('change', async () => {
      sel.disabled = true;
      try {
        await api.defineEloIA(gameId, pid, sel.value);
        toast(sel.value ? 'Elo de comparação guardado para este jogador.' : 'Voltou ao elo padrão.');
      } catch (err) {
        toast(err.message, 'erro');
      }
      desenhaVozes();
    });
    const lista = el(
      'ul',
      'lista-ia',
      r.vozes.map((v) => {
        const e = estadoDaVoz(v);
        return el('li', '', [pilula(e.fala ? 'fala' : 'muda', e.fala ? 'ok' : 'n'), el('b', '', ` ${v.rotulo}: `), el('span', e.fala ? '' : 'sub', e.texto)]);
      }),
    );
    painelVozes.replaceChildren(
      cartao('Quem fala nesta análise', null, [
        el('div', 'pad', [
          el('div', 'linha', [el('label', '', 'Comparar com o elo: '), sel, el('span', 'sub', ` ${r.elo.nome ? `${r.elo.nome} · ` : ''}${textoEloComparacao(r.elo)}`)]),
          r.elo.aviso_riot ? el('p', 'sub', r.elo.aviso_riot) : null,
          lista,
        ]),
      ]),
    );
  }

  async function comeca() {
    analisar.disabled = true;
    try {
      const r = await api.analisaIA(gameId, pid);
      segue(r.execucao.id, r.relatorio.versao);
    } catch (err) {
      toast(err.status === 409 && err.corpo?.execucao ? 'A análise deste jogador já está rodando.' : err.message, 'erro');
      if (err.status === 409 && err.corpo?.execucao) segue(err.corpo.execucao.id);
      else analisar.disabled = false;
    }
  }

  // previa: o texto exato que sai da máquina (o servidor monta; nenhuma chamada à IA).
  function previa() {
    const pre = el('pre', 'previa-ia');
    const d = el('details', 'pad', [el('summary', '', 'ver o texto exato que vai para a IA'), pre]);
    d.addEventListener('toggle', () => {
      if (!d.open || pre.textContent) return;
      pre.textContent = 'montando…';
      api
        .previaIA(gameId, pid)
        .then((p) => {
          const mat = linhasMaterial(p.vozes).join('\n');
          pre.textContent = `=== MATERIAL ESCOLHIDO PARA AS VOZES ===\n${mat}\n\n=== PROMPT DE SISTEMA ===\n${p.sistema}\n\n=== MENSAGEM ===\n${p.mensagem}`;
        })
        .catch((err) => {
          pre.textContent = err.message;
        });
    });
    return d;
  }

  function vazioSemRelatorio() {
    return cartao('Ainda sem relatório para este jogador', null, [
      el('div', 'pad', [
        el('p', '', 'Clique em "Analisar com IA". O app monta o texto de fatos da partida e manda para o Claude Code desta máquina, que devolve as situações que mais custaram, cada uma como uma consulta ao especialista, com o quadro do mapa daquele instante.'),
        el('p', 'sub', 'Sai da máquina: os fatos desta partida (com os nomes de invocador), as fichas dos instantes, a visão e o material das vozes que existir na pasta de conhecimento. Nenhum arquivo, caminho de disco ou chave. Leva alguns minutos e gasta a cota do Claude Code.'),
      ]),
      previa(),
    ]);
  }

  function segue(execId) {
    analisar.disabled = true;
    const enviado = el('div', 'sub');
    const progresso = el('p', '', 'Montando o texto da partida…');
    const previa = el('pre', 'previa-ia');
    const det = el('details', 'pad', [el('summary', '', 'ver a resposta chegando (JSON cru)'), previa]);
    const cancela = botao('Cancelar', 'perigo', () => api.cancelaExecucao(execId).catch(() => {}));
    corpo.replaceChildren(cartao('Analisando com IA', cancela, [el('div', 'pad', [progresso, enviado]), det]));
    let chars = 0;
    desliga?.();
    desliga = acompanha(
      execId,
      {
        contexto: (d) => {
          const e = d.enviado;
          enviado.replaceChildren(
            el('div', '', `Enviado: ${(e.caracteres_sistema + e.caracteres_mensagem).toLocaleString('pt-BR')} caracteres (${e.secoes.join('; ')}).`),
            el('div', '', d.vozes_sem_material.length ? `Vozes sem material (o app escreve "sem dados de referência"): ${d.vozes_sem_material.map((v) => v.voz).join(', ')}.` : 'Todas as vozes têm material.'),
          );
          progresso.textContent = `Chamando o Claude Code (${d.modelo})…`;
        },
        na_fila: (d) => {
          progresso.textContent = d.texto;
        },
        inicio: () => {},
        trecho: (d) => {
          chars = d.caracteres;
          progresso.textContent = `A IA está escrevendo… ${chars.toLocaleString('pt-BR')} caracteres recebidos.`;
          previa.textContent += d.texto;
        },
        concluido: () => {},
        cancelado: () => {
          progresso.textContent = 'Cancelada.';
        },
        erro: (d) => {
          progresso.textContent = `Não deu certo: ${d.texto}`;
          progresso.className = 'tx-cr';
        },
        fim: () => {
          desliga = null;
          ctx.navega({ vista: 'ia', gameId, participante: pid }, { substitui: true });
        },
      },
      { textoQueda: 'a conexão com o servidor caiu durante a análise' },
    );
  }

  // --- Texto com links: instantes → mapa com play; "partida N" → a partida ---
  function texto(tag, classe, s) {
    const e = el(tag, classe);
    for (const x of segmentos(s, gameId)) {
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
  // Declarações de função (não const): o relatório é desenhado antes desta linha ser alcançada.
  function linkT(t, ms) {
    const a = link(t, rotaDoInstante(gameId, ms), 'num tempo');
    a.title = 'abrir o mapa com play neste instante';
    return a;
  }
  function fonte(f) {
    return f ? el('p', 'sub fonte-ia', `fonte: ${f}`) : null;
  }

  function desenhaRelatorio(alvo, r) {
    if (r.estado !== 'concluido' || !r.conteudo) {
      alvo.replaceChildren(
        cartao(`Versão ${r.versao}: ${r.estado}`, null, [
          el('div', 'pad', [el('p', 'tx-cr', r.erro || 'sem conteúdo'), el('p', 'sub', 'Clique em "Refazer análise com IA" para tentar de novo.')]),
        ]),
      );
      return;
    }
    const c = r.conteudo;
    const partes = [];
    partes.push(
      cartao('Resumo', `versão ${r.versao} · ${r.modelo || ''}`, [
        el('div', 'pad', [
          texto('p', '', c.resumo),
          c.padroes.length ? el('h3', 'sub-titulo', 'Padrões') : null,
          c.padroes.length ? el('ul', 'lista-ia', c.padroes.map((p) => texto('li', '', p.texto))) : null,
        ]),
      ]),
    );
    partes.push(el('h2', 'secao-ia', 'Situações que mais custaram'));
    if (!c.situacoes.length) partes.push(el('p', 'sub', 'A IA não devolveu nenhuma situação.'));
    c.situacoes.forEach((s, i) => partes.push(situacao(s, i)));
    if (c.janelas.length) {
      partes.push(el('h2', 'secao-ia', 'Janelas (invasão e roaming)'));
      const g = el('div', 'grade-janelas');
      for (const j of c.janelas) {
        g.append(
          el('section', 'cartao janela-ia', [
            j.quadro ? quadroDoMapa({ gameId, ms: j.quadro.t_ms, foco: pid, info, anotacao: j.quadro, legenda: j.quadro.legenda }) : null,
            el('div', 'pad', [
              el('div', 'linha', [linkT(j.t, j.t_ms), pilula(j.tipo === 'aproveitada' ? 'aproveitada' : 'perdida', j.tipo === 'aproveitada' ? 'ok' : 'at')]),
              texto('p', '', j.fato),
              j.o_que_fazer ? texto('p', '', `O que fazer: ${j.o_que_fazer}`) : null,
              fonte(j.fonte),
            ]),
          ]),
        );
      }
      partes.push(g);
    }
    const dupla = el('div', 'dupla-ia');
    dupla.append(
      cartao('Jogadores-chave', 'quem decidiu a partida', [
        el(
          'ul',
          'lista-ia pad',
          c.jogadores_chave.map((j) =>
            el('li', '', [
              el('b', j.time === 200 ? 'tm-v' : 'tm-a', j.campeao),
              el('span', '', ` · ${j.papel}`),
              texto('p', '', j.por_que),
              fonte(j.fonte),
            ]),
          ),
        ),
      ]),
      cartao('Build', 'onde cabia outro item', [
        c.build.length
          ? el('ul', 'lista-ia pad', c.build.map((b) => el('li', '', [linkT(b.t, b.t_ms), el('b', '', ` ${b.item}`), texto('p', '', b.observacao), b.alternativa ? texto('p', '', `Alternativa: ${b.alternativa}`) : null, fonte(b.fonte)])))
          : el('p', 'sub pad', 'Nada a apontar na build.'),
      ]),
    );
    partes.push(dupla);
    partes.push(
      cartao('Visão', 'sentinelas e quem estava à vista', [
        el('div', 'pad', [
          texto('p', '', c.visao.texto),
          c.visao.itens.length ? el('ul', 'lista-ia', c.visao.itens.map((v) => el('li', '', [linkT(v.t, v.t_ms), texto('span', '', ` ${v.fato}`), fonte(v.fonte)]))) : null,
        ]),
      ]),
    );
    partes.push(
      cartao('As vozes', 'cada uma só fala com material que o app lhe deu', [
        el('ul', 'lista-ia pad', c.vozes.map((v) => el('li', '', [el('b', '', v.rotulo), el('p', v.sem_dados ? 'sub' : '', v.texto)]))),
        el('p', 'sub pad', `Para ligar uma voz, ponha o arquivo Markdown dela em ${estadoIA?.conhecimento || 'dados/conhecimento'} (nomes em docs/arquitetura/contrato-http.md, seção IA) e refaça a análise.`),
      ]),
    );
    const env = r.enviado || {};
    const uso = r.uso || {};
    partes.push(
      cartao('O que saiu da máquina nesta análise', null, [
        el('div', 'pad', [
          el('p', '', `${((env.caracteres_sistema || 0) + (env.caracteres_mensagem || 0)).toLocaleString('pt-BR')} caracteres para o Claude Code: ${(env.secoes || []).join('; ')}.`),
          env.arquivos_conhecimento?.length ? el('p', 'sub', `Arquivos de conhecimento: ${env.arquivos_conhecimento.join(', ')}.`) : null,
          el('p', 'sub', `Voltaram ${(uso.caracteres_saida || 0).toLocaleString('pt-BR')} caracteres em ${((uso.duracao_ms || 0) / 1000).toFixed(0)} s · modelo ${uso.modelo || '—'}${uso.tokens_entrada ? ` · ${uso.tokens_entrada.toLocaleString('pt-BR')} tokens de entrada, ${uso.tokens_saida.toLocaleString('pt-BR')} de saída` : ''}${uso.custo_usd ? ` · custo estimado pelo Claude Code US$ ${uso.custo_usd.toFixed(2)}` : ''}.`),
          previa(),
          r.avisos.length ? el('details', '', [el('summary', 'sub', `${r.avisos.length} aviso(s) da validação`), el('ul', 'lista-ia', r.avisos.map((a) => el('li', 'sub', a)))]) : null,
        ]),
      ]),
    );
    alvo.replaceChildren(...partes);
  }

  function situacao(s, i) {
    const { com, sem } = separaVozes(s.vozes);
    const art = el('article', 'cartao situacao-ia');
    const perguntar = botao('Perguntar ao especialista', 'mini', () => abreConversa({ gameId, pid, info, pergunta: perguntaDaSituacao(s), enviaJa: false }), 'abre a conversa com esta situação escrita');
    perguntar.disabled = !estadoIA?.disponivel;
    const texto1 = el('div', 'situacao-texto', [
      el('div', 'linha', [el('span', 'situacao-n', String(i + 1)), linkT(s.t, s.t_ms), el('h3', '', s.titulo)]),
      s.custo ? texto('p', 'custo-ia', `Custo: ${s.custo}`) : null,
      el('h4', '', 'Situação'),
      texto('p', '', s.situacao),
      s.pergunta ? el('blockquote', 'pergunta-ia', [texto('span', '', s.pergunta)]) : null,
      el('h4', '', 'O que as vozes dizem'),
      com.length
        ? el('ul', 'lista-ia', com.map((v) => el('li', '', [el('b', '', v.rotulo), texto('p', '', v.resposta), fonte(v.fonte)])))
        : null,
      sem.length ? el('p', 'sub', `Sem dados de referência: ${sem.map((v) => v.rotulo.replace(/^O que (o |os |um )?/i, '')).join('; ')}.`) : null,
      s.leitura ? el('h4', '', 'Leitura da IA (sugestão, conhecimento geral)') : null,
      s.leitura ? texto('p', '', s.leitura) : null,
      s.proxima_vez ? el('p', 'proxima-vez', [el('b', '', 'Próxima vez: '), texto('span', '', s.proxima_vez)]) : null,
      s.repeticoes.length ? el('p', 'sub', ['Repetiu nesta partida: ', ...s.repeticoes.flatMap((t, k) => [k ? ', ' : '', link(t, rotaDoInstante(gameId, msDeT(t)), 'num')])]) : null,
      fonte(s.fonte),
      perguntar,
    ]);
    const q = s.quadro ? quadroDoMapa({ gameId, ms: s.quadro.t_ms, foco: pid, info, anotacao: s.quadro, legenda: s.quadro.legenda }) : el('div', 'sub', 'sem quadro');
    art.append(el('div', 'situacao-quadro', [q]), texto1);
    return art;
  }

  return () => desliga?.();
}

function msDeT(t) {
  const [m, s] = t.split(':').map(Number);
  return (m * 60 + s) * 1000;
}
