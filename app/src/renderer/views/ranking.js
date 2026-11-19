// O RANKING DO LANE (área de Estudo): por patch × elo × rota (e campeão: "especialistas de X"), os
// melhores jogadores para o estudo, com a nota aberta em componentes. A fórmula fica a um clique e
// cada célula diz de onde saiu. "Atualizar ranking" é uma execução (pausável, retomável) que gasta
// pedidos à Riot (o orçamento aparece antes); "rastrear os N primeiros" cria a coleta por elo a
// partir do ranking, sempre com os 10 de cada partida.
//
//   #ranking[/patch/elo/recorte[/modo]]   recorte = JUNGLE... ou c<id do campeão>

import { api, acompanha, imagemCampeao } from '../api.js';
import { el, botao, link, pilula, cartao, carregando, erroNaTela, toast } from '../ui.js';
import {
  ELOS, NOME_ELO, ROTAS, NOME_ROTA, lerRecorte, consultaRecorte, nomeDoRanking, textoFora, formataNota, selosPosicao,
  textoLiga, ESTADOS_ATUALIZACAO, resumoAtualizacao, corpoRastrear, deltaPosicao,
} from '../ranking.js';

const EXPLICA =
  'O ranking é nosso e aberto: a nota é a soma dos componentes da tabela (cada um de 0 a 1, vezes o peso), com os números de ' +
  'onde saiu cada um. "Subindo" é quem ganha agora e dá sinais de subida (o critério do estudo: quem tem a ação de subir, não ' +
  'quem está travado no elo); "melhor" pesa a posição na liga e a consistência. Cada patch tem o seu ranking: compare dois ' +
  'para ver se o comportamento mudou. Os dados vêm só da API da Riot e do histórico (cliente do LoL ou match-v5).';

// --- Cartão na lista de Estudo -----------------------------------------------------------------

export function montaCartao() {
  const corpo = el('div', 'pad');
  const raiz = cartao('Ranking do Lane', link('Abrir', '#ranking', 'btn mini'), corpo, 'cartao-ranking');
  corpo.append(el('p', 'sub', 'Lendo…'));
  let vivo = true;
  api
    .rankings()
    .then((ind) => {
      if (!vivo) return;
      corpo.replaceChildren(
        el('p', 'sub', 'Os melhores de cada elo e de cada rota (e por campeão), por patch, com a fórmula à mostra.'),
        ind.patches.length
          ? el('p', '', `Patches com ranking: ${ind.patches.join(', ')}${ind.elos.length ? ` · ${ind.patch}: ${ind.elos.map((e) => NOME_ELO[e]).join(', ')}` : ''}`)
          : el('p', 'sub', 'Ainda sem ranking: abra e atualize o de um elo.'),
      );
    })
    .catch((err) => vivo && corpo.replaceChildren(erroNaTela(err)));
  return { el: raiz, limpa: () => (vivo = false) };
}

// --- Tela --------------------------------------------------------------------------------------

export async function desenha(raiz, rota, ctx) {
  const cab = el('div', 'linha cab-tela', [link('← Estudo', '#estudo', 'voltar'), el('h1', 'titulo-tela inline', 'Ranking do Lane')]);
  raiz.append(cab, carregando('Carregando o ranking…'));
  const estado = {
    patch: rota.patch || '',
    elo: rota.elo && NOME_ELO[rota.elo] ? rota.elo : 'CHALLENGER',
    recorte: rota.recorte || 'JUNGLE',
    modo: rota.modo || 'subindo',
    comparar: '',
  };
  let vivo = true;
  const desligas = new Set();
  const seguidas = new Set();
  const progresso = el('div', 'progresso-esp');
  let formula = null;

  async function recarrega() {
    let ind;
    let ats;
    try {
      [ind, ats] = await Promise.all([api.rankings(estado.patch, estado.elo), api.atualizacoesRanking()]);
      if (!formula) formula = await api.formulaRanking();
    } catch (err) {
      if (vivo) raiz.replaceChildren(cab, erroNaTela(err));
      return;
    }
    if (!vivo) return;
    if (!estado.patch && ind.patch) {
      estado.patch = ind.patch;
      ind = await api.rankings(estado.patch, estado.elo).catch(() => ind);
    }
    ctx.marca({ vista: 'ranking', patch: estado.patch, elo: estado.elo, recorte: estado.recorte, modo: estado.modo });
    let dados = null;
    let erroRk = null;
    if (estado.patch) {
      try {
        dados = await api.rankingRecorte(consultaRecorte(estado));
      } catch (err) {
        if (err.status !== 404) erroRk = err;
      }
    }
    let comparacao = null;
    if (dados && estado.comparar && estado.comparar !== estado.patch) {
      const r = lerRecorte(estado.recorte);
      const alvo = r.campeao ? `campeao=${r.campeao}` : `rota=${r.rota}`;
      comparacao = await api
        .comparaRanking(`elo=${estado.elo}&${alvo}&patch_a=${encodeURIComponent(estado.comparar)}&patch_b=${encodeURIComponent(estado.patch)}&modo=${estado.modo}&topo=10`)
        .catch((err) => ({ erro: err }));
    }
    if (!vivo) return;
    const partes = [cab, progresso, el('p', 'sub', EXPLICA), filtros(ind)];
    if (erroRk) partes.push(erroNaTela(erroRk));
    partes.push(cartaoAcoes(ind, dados, ats.fonte_padrao));
    partes.push(dados ? cartaoRanking(dados) : semRanking());
    if (dados) partes.push(cartaoFormula(dados));
    if (dados) partes.push(cartaoComparacao(ind, comparacao));
    partes.push(cartaoAtualizacoes(ats.atualizacoes));
    raiz.replaceChildren(...partes);
    for (const a of ats.atualizacoes) if (a.execucao) segue(a.execucao.id, `atualização ${a.id} (${NOME_ELO[a.elo]})`);
  }

  function filtros(ind) {
    const muda = (campo) => (ev) => {
      estado[campo] = ev.target.value;
      recarrega();
    };
    const sel = (opcoes, valor, campo, rotulo) => {
      const s = el('select', 'campo', opcoes.map(([v, t]) => {
        const o = el('option', '', t);
        o.value = v;
        o.selected = v === valor;
        return o;
      }));
      s.setAttribute('aria-label', rotulo);
      s.addEventListener('change', muda(campo));
      return el('label', 'campo-rotulado', [rotulo, s]);
    };
    const patches = ind.patches.length ? ind.patches : [estado.patch || ''];
    const comRanking = new Set(ind.elos);
    const recortes = ROTAS.map((r) => [r, NOME_ROTA[r]]);
    for (const rk of ind.recortes.filter((x) => x.tipo === 'campeao')) recortes.push([`c${rk.campeao_id}`, `${rk.campeao} (${rk.amostra})`]);
    return el('div', 'linha filtros-ranking', [
      sel(patches.map((p) => [p, p || '—']), estado.patch, 'patch', 'Patch'),
      sel(ELOS.map((e) => [e, `${NOME_ELO[e]}${comRanking.has(e) ? ' ✓' : ''}`]), estado.elo, 'elo', 'Elo'),
      sel(recortes, estado.recorte, 'recorte', 'Rota ou campeão'),
      sel([['subindo', 'subindo'], ['melhor', 'melhor']], estado.modo, 'modo', 'Ordem'),
    ]);
  }

  function semRanking() {
    const r = lerRecorte(estado.recorte);
    const oQue = r.campeao ? `do campeão ${r.campeao}` : `de ${NOME_ROTA[r.rota]}`;
    return cartao('Sem ranking', null, [
      el('p', 'sub pad', estado.patch
        ? `Ainda não há ranking ${oQue} em ${NOME_ELO[estado.elo]} no patch ${estado.patch}. Use "Atualizar ranking" acima.`
        : 'Ainda não há ranking nenhum. Escolha o elo e use "Atualizar ranking" acima.'),
    ]);
  }

  // --- Ações: atualizar e rastrear -------------------------------------------------------------

  function cartaoAcoes(ind, dados, fontePadrao) {
    const corpo = el('div', 'pad acoes-ranking');
    // Atualizar: fonte, candidatos, partidas; o orçamento antes de gastar.
    const fonte = el('select', 'campo', [['lcu', 'cliente do LoL (lcu)'], ['riot', 'só a API da Riot (match-v5)']].map(([v, t]) => {
      const o = el('option', '', t);
      o.value = v;
      o.selected = v === (fontePadrao || 'lcu');
      return o;
    }));
    const cand = el('input', 'campo curto');
    cand.type = 'number';
    cand.min = 1;
    cand.max = 300;
    cand.value = 60;
    const parts = el('input', 'campo curto');
    parts.type = 'number';
    parts.min = 3;
    parts.max = 40;
    parts.value = 20;
    const orc = el('p', 'sub', '');
    const mostraOrcamento = () =>
      api
        .orcamentoRanking(estado.elo, fonte.value, Number(cand.value), Number(parts.value))
        .then((o) => (orc.textContent = `Orçamento: ${o.texto}.`))
        .catch((err) => (orc.textContent = err.message));
    for (const c of [fonte, cand, parts]) c.addEventListener('change', mostraOrcamento);
    mostraOrcamento();
    const atualiza = botao('Atualizar ranking', 'pri', async () => {
      atualiza.disabled = true;
      try {
        const r = await api.atualizaRanking({ elo: estado.elo, patch: estado.patch, fonte: fonte.value, candidatos: Number(cand.value), partidas_por_jogador: Number(parts.value) });
        segue(r.execucao.id, `atualização ${r.atualizacao.id} (${NOME_ELO[estado.elo]})`);
      } catch (err) {
        toast(err.message, 'erro');
      } finally {
        atualiza.disabled = false;
      }
    }, 'Lê a liga e o histórico dos candidatos e recalcula os rankings das 5 rotas e dos campeões deste elo no patch');

    // Rastrear os N primeiros de cada rota marcada (os 10 de cada partida).
    const n = el('select', 'campo', [1, 2, 3, 4].map((v) => {
      const o = el('option', '', String(v));
      o.value = v;
      o.selected = v === 3;
      return o;
    }));
    const marcas = ROTAS.map((r) => {
      const c = el('input');
      c.type = 'checkbox';
      c.value = r;
      c.checked = ['JUNGLE', 'MIDDLE', 'TOP'].includes(r);
      return el('label', 'marca-rota', [c, ` ${NOME_ROTA[r]}`]);
    });
    const rastreia = botao('Rastrear os N primeiros', '', async () => {
      const rotas = marcas.map((m) => m.querySelector('input')).filter((c) => c.checked).map((c) => c.value);
      if (!rotas.length) return toast('Marque pelo menos uma rota.', 'erro');
      rastreia.disabled = true;
      try {
        const r = await api.criaColeta(corpoRastrear({ elo: estado.elo, patch: estado.patch, modo: estado.modo, primeiros: Number(n.value), rotas }));
        toast(`Coleta ${r.coleta.id} criada a partir do ranking (${r.coleta.quem === 'todos' ? 'os 10 de cada partida' : r.coleta.quem}). Acompanhe em Estudo → Aprendizado por elo.`);
      } catch (err) {
        toast(err.message, 'erro');
      } finally {
        rastreia.disabled = false;
      }
    }, 'Cria a coleta por elo com os primeiros do ranking de cada rota marcada; rastreia os 10 jogadores de cada partida (abre o replay)');
    if (!ind.elos.includes(estado.elo)) rastreia.disabled = true;

    corpo.append(
      el('div', 'linha', [el('b', '', `Atualizar ${NOME_ELO[estado.elo]}${estado.patch ? ` · ${estado.patch}` : ''}`),
        el('label', 'campo-rotulado', ['Fonte do histórico', fonte]), el('label', 'campo-rotulado', ['Candidatos', cand]),
        el('label', 'campo-rotulado', ['Partidas por jogador', parts]), atualiza]),
      orc,
      el('div', 'linha', [el('b', '', 'Rastrear'), el('label', 'campo-rotulado', ['N primeiros', n]), ...marcas, rastreia]),
      el('p', 'sub', `Rastreia, de ${NOME_ELO[estado.elo]}, os N primeiros (ordem "${estado.modo}") de cada rota marcada, com os 10 jogadores de cada partida (regra do estudo). ` +
        'Precisa do ranking destas rotas no patch instalado; quem não tem replay no patch é pulado e o próximo entra.'),
    );
    return cartao('Atualizar e rastrear', null, corpo);
  }

  // --- A tabela ----------------------------------------------------------------------------------

  function cartaoRanking(d) {
    const rk = d.ranking;
    const titulo = `${NOME_ELO[rk.elo]} · ${nomeDoRanking(rk)} · ${rk.patch} · ordem "${d.modo}"`;
    const pesos = d.formula.pesos;
    const cabeca = el('tr', '', [
      el('th', 'num', '#'), el('th', '', 'Jogador'), el('th', 'num', 'Nota'),
      ...pesos.map((p) => {
        const th = el('th', 'num', p.rotulo);
        th.title = `${p.como} (peso ${String(p.peso).replace('.', ',')})`;
        return th;
      }),
      el('th', '', 'Por quê'),
    ]);
    const linhas = d.posicoes.map((p) => {
      const comps = new Map(p.componentes.map((c) => [c.nome, c]));
      const jog = el('td', 'jogador-ranking', [
        p.numeros.campeao_id ? el('img', 'retrato-mini') : null,
        el('div', '', [
          el('b', '', p.nome ? `${p.nome}#${p.tag}` : p.puuid.slice(0, 10)),
          el('div', 'sub', [textoLiga(rk.elo, p.numeros), p.campeao ? ` · ${rk.tipo === 'campeao' ? NOME_ROTA[p.numeros.rota] || '' : p.campeao}` : ''].join('')),
          el('div', 'linha', selosPosicao(p).map((s) => pilula(s.texto, s.classe))),
        ]),
      ]);
      const img = jog.querySelector('img');
      if (img) {
        img.src = imagemCampeao(p.numeros.campeao_id);
        img.alt = '';
        img.width = 24;
        img.height = 24;
      }
      return el('tr', '', [
        el('td', 'num', String(p.posicao)),
        jog,
        el('td', 'num nota-ranking', formataNota(p.nota)),
        ...pesos.map((w) => {
          const c = comps.get(w.componente);
          const td = el('td', `num comp${c && c.contribuicao < 0 ? ' neg' : ''}`, c ? formataNota(c.contribuicao) : '—');
          if (c) td.title = `${c.rotulo}: ${c.cru} → valor ${c.valor.toLocaleString('pt-BR')} × peso ${String(c.peso).replace('.', ',')} × 100`;
          return td;
        }),
        el('td', 'frase-ranking', p.frase),
      ]);
    });
    const tabela = el('table', 'tabela compacta tabela-ranking', [el('thead', '', [cabeca]), el('tbody', '', linhas)]);
    const fora = textoFora(rk.fora);
    return cartao(titulo, pilula(`${rk.amostra} no ranking · regra v${rk.versao_regra}`, 'n'), [
      el('p', 'sub pad', `O que foi olhado: ${rk.descricao_amostra}.`),
      fora ? el('p', 'sub pad', `Ficaram de fora: ${fora}.`) : null,
      d.posicoes.length
        ? el('div', 'rolagem-x', [tabela])
        : el('p', 'sub pad', 'Ninguém entrou neste recorte (veja quem ficou de fora acima).'),
      el('p', 'sub pad', 'Cada coluna de componente é a contribuição em pontos (passe o mouse para ver o número cru, o valor e o peso).'),
    ]);
  }

  function cartaoFormula(d) {
    const det = el('details', 'formula-ranking');
    det.append(el('summary', '', 'Como esta nota é calculada'));
    det.append(el('p', 'formula-texto', d.formula.texto));
    det.append(el('table', 'tabela compacta', [
      el('thead', '', [el('tr', '', [el('th', '', 'Componente'), el('th', 'num', 'Peso'), el('th', '', 'Como')])]),
      el('tbody', '', d.formula.pesos.map((p) => el('tr', '', [el('td', '', p.rotulo), el('td', 'num', String(p.peso).replace('.', ',')), el('td', '', p.como)]))),
    ]));
    if (formula?.regras) det.append(el('ul', 'regras-ranking', formula.regras.map((r) => el('li', '', r))));
    det.append(el('p', 'sub', `Versão da regra ${formula?.versao_regra ?? d.ranking.versao_regra}. Os mesmos números estão em server/ranking/parametros.go e em docs/arquitetura/dados.md.`));
    return cartao(null, null, [el('div', 'pad', [det])]);
  }

  // --- Comparar dois patches -----------------------------------------------------------------------

  function cartaoComparacao(ind, cmp) {
    const outros = ind.patches.filter((p) => p !== estado.patch);
    const s = el('select', 'campo', [['', 'escolha o outro patch'], ...outros.map((p) => [p, p])].map(([v, t]) => {
      const o = el('option', '', t);
      o.value = v;
      o.selected = v === estado.comparar;
      return o;
    }));
    s.addEventListener('change', () => {
      estado.comparar = s.value;
      recarrega();
    });
    const corpo = [el('div', 'linha pad', [el('span', 'sub', `Comparar o topo 10 do ${estado.patch} com o de`), s])];
    if (!outros.length) corpo.push(el('p', 'sub pad', 'Só há ranking deste patch. Atualize o ranking no próximo patch para comparar.'));
    if (cmp?.erro) corpo.push(el('p', 'sub pad', cmp.erro.status === 404 ? `Não há ranking deste recorte no ${estado.comparar}.` : cmp.erro.message));
    else if (cmp) {
      const item = (x, lado) => el('li', '', `${x.nome ? `${x.nome}#${x.tag}` : x.puuid.slice(0, 10)} (${lado === 'b' ? `${x.posicao_b}º` : `era ${x.posicao_a}º`})`);
      corpo.push(
        el('div', 'grade-2 pad', [
          el('div', '', [el('b', '', `Entraram (${cmp.entraram.length})`), el('ul', '', cmp.entraram.map((x) => item(x, 'b')))]),
          el('div', '', [el('b', '', `Saíram (${cmp.sairam.length})`), el('ul', '', cmp.sairam.map((x) => item(x, 'a')))]),
          el('div', '', [el('b', '', `Ficaram (${cmp.ficaram.length})`), el('ul', '', cmp.ficaram.map((x) => el('li', '', `${x.nome}#${x.tag}: ${x.posicao_a}º → ${x.posicao_b}º ${deltaPosicao(x.posicao_a, x.posicao_b)}`)))]),
          el('div', '', [el('b', '', cmp.presenca_de === 'rota' ? 'Rotas no topo' : 'Campeões no topo'),
            el('ul', '', cmp.presenca.map((p) => el('li', '', `${p.nome || p.chave}: ${p.a} → ${p.b}`)))]),
        ]),
        ...cmp.avisos.map((a) => el('p', 'sub pad', a)),
      );
    }
    return cartao('Comparar patches', null, corpo);
  }

  // --- Atualizações e progresso -------------------------------------------------------------------

  function cartaoAtualizacoes(l) {
    if (!l.length) return cartao('Atualizações', null, [el('p', 'sub pad', 'Nenhuma atualização ainda.')]);
    const linhas = l.slice(0, 8).map((a) => {
      const e = ESTADOS_ATUALIZACAO[a.estado] || { texto: a.estado, classe: 'n' };
      const acoes = el('span', 'linha');
      if (a.estado === 'rodando') acoes.append(botao('Pausar', 'mini', () => api.pausaAtualizacao(a.id).then(() => toast('Pausa pedida: termina o candidato em andamento.')).catch((err) => toast(err.message, 'erro'))));
      if (a.estado === 'pausado' || a.estado === 'falhou') {
        acoes.append(botao('Retomar', 'mini pri', async () => {
          try {
            const r = await api.retomaAtualizacao(a.id);
            segue(r.execucao.id, `atualização ${a.id} (${NOME_ELO[a.elo]})`);
          } catch (err) {
            toast(err.message, 'erro');
          }
        }));
      }
      if (a.estado !== 'concluido' && a.estado !== 'cancelado') acoes.append(botao('Cancelar', 'mini perigo', () => api.cancelaAtualizacao(a.id).then(recarrega).catch((err) => toast(err.message, 'erro'))));
      return el('div', 'linha-lista', [
        el('b', '', `nº ${a.id}`), el('span', 'sub', resumoAtualizacao(a)), a.erro ? el('span', 'tx-cr', a.erro) : null, pilula(e.texto, e.classe), acoes,
      ]);
    });
    return cartao('Atualizações', null, linhas);
  }

  function segue(execId, oQue) {
    if (seguidas.has(execId)) return;
    seguidas.add(execId);
    const linha = el('p', 'sub', `${oQue}: começando…`);
    const caixa = el('div', 'linha', [linha]);
    progresso.append(caixa);
    const d = acompanha(
      execId,
      {
        fase: (x) => (linha.textContent = `${oQue}: ${{ liga: 'lendo a liga', historicos: 'lendo os históricos', calculo: 'calculando' }[x.fase] || x.fase}…`),
        liga: (x) => (linha.textContent = `${oQue}: ${x.entradas} jogadores na liga lida (${x.paginas} página(s)), ${x.candidatos} candidatos`),
        candidato: (x) => (linha.textContent = `${oQue}: candidato ${x.ordem} de ${x.total}${x.riot_id ? ` (${x.riot_id}, ${x.partidas} partidas no patch)` : ` (${x.resultado})`}`),
        aviso: (x) => toast(x.texto),
        pausado: () => (linha.textContent = `${oQue}: pausada.`),
        cancelado: () => (linha.textContent = `${oQue}: cancelada.`),
        concluido: (x) => toast(`${oQue}: pronto, ${x.rankings} ranking(s).`),
        erro: (x) => {
          linha.textContent = `${oQue}: ${x.texto}`;
          linha.className = 'tx-cr';
        },
        fim: () => {
          desligas.delete(d);
          setTimeout(() => caixa.remove(), 8000);
          setTimeout(() => vivo && recarrega(), 300);
        },
      },
      { textoQueda: 'a conexão com o servidor caiu' },
    );
    desligas.add(d);
  }

  await recarrega();
  return () => {
    vivo = false;
    for (const d of desligas) d();
  };
}
