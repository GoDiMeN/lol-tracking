// Cartão "Aprendizado por elo" da tela Estudo: começa, acompanha (SSE), pausa, retoma e cancela a
// coleta que monta e rastreia sozinha partidas de referência por elo. Mostra o progresso geral e
// por elo, os jogadores escolhidos (com o porquê), as partidas, o espaço ocupado e os avisos.

import { api, acompanha } from '../api.js';
import { duracao, kda, plural, posicao } from '../formato.js';
import { tempoHumano, bytes, numero } from '../texto.js';
import {
  ELOS,
  nomeElo,
  estimativaColeta,
  ESTADOS_COLETA,
  ESTADOS_PARTIDA,
  ESTADOS_ELO,
  textoDescartes,
  porcento,
  FUNCOES,
  filtraFuncao,
  destaques,
  numerosLiga,
} from '../aprendizado.js';
import { el, botao, link, pilula, cartao, barra, carregando, erroNaTela, confirmaNoBotao, toast, retrato } from '../ui.js';

const EXPLICA =
  'Monta e rastreia sozinho partidas de referência de cada elo (ranqueada solo), para depois uma IA aprender o que os jogadores de cada elo fazem. ' +
  'Entram os que estão subindo, não os que mais pontuam: a Riot não informa o elo anterior, então o critério é a taxa de vitória e os sinais da liga ' +
  '(recém-chegado, sequência de vitórias, sem estar travado há muito tempo). De Prata a Diamante é uma aproximação: só as primeiras páginas de cada divisão. ' +
  'De Mestre para cima, só quem joga quase sempre o mesmo campeão.';

// A dica do dono: o cartão também serve para achar UM jogador de referência e estudá-lo a fundo.
const DICA =
  'Dica: dá para estudar a fundo um jogador de referência, por exemplo o Platina que mais está subindo ou um Esmeralda que acabou de chegar ao elo. ' +
  'Comece com "só descobrir jogadores" marcando os elos que interessam, escolha o jogador na lista (dá para filtrar pela função, como selva) ' +
  'e use "Criar estudo": o app junta as melhores vitórias e as derrotas mais recentes dele e compara as duas.';

const LEMBRETE ='O jogo precisa ficar aberto e visível na tela enquanto a coleta rastreia. Pausar termina o jogador em andamento e para; dá para retomar depois.';

// montaCartao devolve {el, limpa}: o cartão e a função que desliga SSE e intervalo ao sair da tela.
export function montaCartao(ctx) {
  const corpo = el('div', 'aprendizado');
  const dir = el('span', 'linha');
  const raiz = cartao('Aprendizado por elo', dir, corpo, 'cartao-aprendizado');
  let vivo = true;
  let d = null; // {coleta, elos, jogadores, partidas}
  let espaco = null;
  let parar = null;
  let execucaoLigada = null;
  let pendente = null;
  let carregado = false;
  // Filtro da lista de jogadores por função ('' = todas); sobrevive às recargas.
  let filtroFuncao = '';

  async function recarrega() {
    try {
      const { coletas } = await api.coletas();
      if (!vivo) return;
      const atual = coletas.find((c) => c.estado === 'rodando') || coletas[0];
      carregado = true;
      if (!atual) {
        d = null;
        desenha();
        return;
      }
      [d, espaco] = await Promise.all([api.coleta(atual.id), api.espacoColeta(atual.id).catch(() => null)]);
      if (!vivo) return;
      desenha();
      ligaSSE();
    } catch (err) {
      if (vivo && !d) corpo.replaceChildren(erroNaTela(err));
    }
  }

  // Os eventos chegam em rajadas (um por candidato): uma recarga a cada segundo basta.
  function recarregaLogo() {
    if (pendente) return;
    pendente = setTimeout(() => {
      pendente = null;
      recarrega();
    }, 1000);
  }

  function ligaSSE() {
    const ex = d?.coleta.execucao?.id;
    if (!ex || ex === execucaoLigada || d.coleta.estado !== 'rodando') return;
    execucaoLigada = ex;
    parar?.();
    const qualquer = () => recarregaLogo();
    parar = acompanha(
      ex,
      {
        fase: qualquer,
        elo_inicio: qualquer,
        candidato: qualquer,
        elo_fim: qualquer,
        partida_inicio: qualquer,
        download: qualquer,
        rastreio: qualquer,
        partida_fim: qualquer,
        pausa_pedida: qualquer,
        pausado: (x) => {
          toast(x.motivo ? `Coleta pausada: ${x.motivo}` : 'Coleta pausada. Dá para retomar de onde parou.', x.motivo ? 'erro' : '');
          recarregaLogo();
        },
        concluido: () => {
          toast('Coleta concluída.', 'ok');
          recarregaLogo();
        },
        aviso: (x) => toast(x.texto, 'erro'),
        erro: (x) => toast(x.texto, 'erro'),
        fim: () => {
          execucaoLigada = null;
          ctx.atualizaSeloRastreio();
          recarregaLogo();
        },
      },
      { textoQueda: 'A conexão ao vivo caiu; o cartão segue atualizando a cada poucos segundos.' },
    );
  }

  async function acao(qual) {
    const id = d.coleta.id;
    try {
      if (qual === 'pausa') {
        await api.pausaColeta(id);
        toast('Pausa pedida: o jogador em andamento termina e a coleta para.');
      } else if (qual === 'retoma') {
        await api.retomaColeta(id);
        toast('Retomando a coleta de onde parou.');
      } else {
        await api.cancelaColeta(id);
        toast('Coleta cancelada. O que já foi rastreado fica guardado.');
      }
    } catch (err) {
      toast(err.message, 'erro');
    }
    ctx.atualizaSeloRastreio();
    recarrega();
  }

  function avisos() {
    const e = ctx.estado();
    const l = [];
    if (e && !e.lcu?.conectado) l.push(el('p', 'aviso-linha erro', 'O cliente do LoL está fechado: abra o cliente e entre na conta para coletar.'));
    else if (e?.regiao && e.regiao !== 'BR') l.push(el('p', 'aviso-linha erro', `A conta logada é da região ${e.regiao}: a coleta é do Brasil (BR) e o replay só abre para partidas da região da conta.`));
    const ch = e?.chave_riot;
    if (e && !e.modo_teste && ch && !ch.configurada) l.push(el('p', 'aviso-linha', 'Sem chave da API da Riot (token.txt): a coleta precisa dela para achar os jogadores de cada elo.'));
    else if (ch?.valida === false) l.push(el('p', 'aviso-linha erro', 'A chave da Riot venceu (dura 24 h): gere outra em developer.riotgames.com e troque o token.txt.'));
    l.push(el('p', 'nota', LEMBRETE));
    return el('div', 'pilha-avisos', l);
  }

  function desenha() {
    const regiao = pilula('Brasil (BR)', 'n', 'Por enquanto só a região BR; outras regiões virão depois.');
    dir.replaceChildren(regiao);
    const partes = [el('p', 'sub pad', EXPLICA), el('p', 'dica pad', DICA), avisos()];
    if (!d) {
      partes.push(el('div', 'linha pad', [botao('Começar', 'pri', () => abreDialogo(ctx, recarrega))]));
      corpo.replaceChildren(...partes);
      return;
    }
    const c = d.coleta;
    const est = ESTADOS_COLETA[c.estado] || { classe: 'n', texto: c.estado };
    const acoes = el('div', 'linha acoes-dir');
    if (c.estado === 'rodando') {
      const b = botao(c.pausa_pedida ? 'Pausando…' : 'Pausar', '', () => acao('pausa'), 'Termina o jogador em andamento e para');
      b.disabled = c.pausa_pedida;
      acoes.append(b);
    }
    if (c.estado === 'pausado' || c.estado === 'falhou') acoes.append(botao('Retomar', 'pri', () => acao('retoma')));
    if (c.estado === 'rodando' || c.estado === 'pausado' || c.estado === 'falhou') {
      const x = botao('Cancelar', 'perigo');
      x.addEventListener('click', () => confirmaNoBotao(x, 'Cancelar mesmo?', () => acao('cancela')));
      acoes.append(x);
    }
    if (c.estado === 'concluido' || c.estado === 'cancelado') acoes.append(botao('Nova coleta', 'pri', () => abreDialogo(ctx, recarrega)));
    if (c.rastreio_atual) acoes.append(link('Ver o rastreio', `#rastreio/${c.rastreio_atual}`, 'btn'));

    const fase = c.fase === 'descoberta' ? 'procurando jogadores' : c.fase === 'rastreio' ? 'rastreando partidas' : 'fim';
    const nomesElos = c.elos.map(nomeElo).join(', ');
    const soFuncao = c.funcao ? ` · só ${posicao(c.funcao)}` : '';
    const opcoes = c.so_descobrir
      ? `${nomesElos}${soFuncao} · só descobrir jogadores (nada é rastreado) · ${c.regiao_nome}`
      : `${c.elos.length} elos · ${plural(c.partidas_por_jogador, 'partida', 'partidas')} por jogador · ${c.quem === 'todos' ? 'os 10 de cada partida' : 'só o jogador'} · ${numero(c.velocidade)}× · ${c.regiao_nome}`;
    partes.push(el('div', 'linha pad', [el('b', '', `Coleta nº ${c.id}`), pilula(est.texto, est.classe), el('span', 'sub', `${fase} · ${opcoes}`), acoes]));
    if (!c.so_descobrir) {
      partes.push(
        el('div', 'linha geral pad', [
          el('span', 'num', `${c.passes_concluidos} de ${plural(c.passes_total, 'jogador rastreado', 'jogadores a rastrear')}`),
          barra(c.progresso, 'progresso da coleta', 'larga'),
          el('span', 'sub', `${Math.round((c.progresso || 0) * 100)}%`),
          c.estimativa_s > 0 ? el('span', 'num', `faltam ${tempoHumano(c.estimativa_s)}`) : null,
        ]),
      );
    }
    if (c.pausa_pedida) partes.push(el('p', 'aviso-linha', 'Pausa pedida: termina o jogador em andamento (com a compactação) e para.'));
    if (c.erro) partes.push(el('p', `aviso-linha${c.estado === 'falhou' ? ' erro' : ''}`, c.erro));
    const mudaFiltro = (f) => {
      filtroFuncao = f;
      desenha();
    };
    partes.push(tabelaElos(d), tabelaJogadores(d, filtroFuncao, mudaFiltro, ctx));
    if (!c.so_descobrir) partes.push(listaPartidas(d), quadroEspaco(espaco));
    corpo.replaceChildren(...partes);
  }

  corpo.replaceChildren(carregando('Carregando a coleta…'));
  recarrega();
  const intervalo = setInterval(() => {
    if (d?.coleta.estado === 'rodando') recarrega();
  }, 5000);
  // Os avisos dependem do estado do cliente (aberto, região, chave): redesenha quando ele muda.
  const desliga = ctx.aoMudarEstado(() => carregado && vivo && desenha());

  return {
    el: raiz,
    limpa() {
      vivo = false;
      clearInterval(intervalo);
      clearTimeout(pendente);
      desliga();
      parar?.();
    },
  };
}

function tabelaElos(d) {
  const tab = el('table', 'tabela compacta');
  tab.append(el('thead', '', [el('tr', '', [el('th', '', 'Elo'), el('th', '', 'Situação'), el('th', 'num', 'Jogadores'), el('th', 'num', 'Partidas'), el('th', '', 'Deixados de fora')])]));
  const tb = el('tbody');
  for (const e of d.elos) {
    const s = ESTADOS_ELO[e.estado] || { classe: 'n', texto: e.estado };
    const fora = textoDescartes(e.descartes);
    tb.append(
      el('tr', '', [
        el('td', '', [el('b', '', e.nome), e.exige_mono ? el('small', 'sub', ' · um campeão só') : null]),
        el('td', '', [pilula(s.texto, s.classe), e.aviso ? el('small', 'tx-at', ` ${e.aviso}`) : null]),
        el('td', 'num', `${e.jogadores}/10`),
        el('td', 'num', `${e.partidas_concluidas}/${e.partidas_total}`),
        el('td', 'sub', fora ? `${fora} (de ${e.candidatos_vistos} olhados)` : e.candidatos_vistos ? `${e.candidatos_vistos} olhados` : ''),
      ]),
    );
  }
  tab.append(tb);
  return cartao('Por elo', null, el('div', 'rolar', [tab]));
}

function tabelaJogadores(d, filtro, mudaFiltro, ctx) {
  if (!d.jogadores.length) return cartao('Jogadores', null, el('div', 'estado-tela vazio', 'Nenhum jogador escolhido ainda.'));
  // Filtro por função: o caso de uso é achar "o caçador que mais sobe".
  const sel = el('select', 'campo mini');
  sel.id = 'aprFiltroFuncao';
  sel.setAttribute('aria-label', 'Filtrar os jogadores pela função');
  sel.append(Object.assign(el('option', '', 'todas as funções'), { value: '' }));
  for (const [v, t] of FUNCOES) sel.append(Object.assign(el('option', '', t), { value: v }));
  sel.value = filtro;
  sel.addEventListener('change', () => {
    mudaFiltro(sel.value);
    document.getElementById('aprFiltroFuncao')?.focus();
  });
  const js = filtraFuncao(d.jogadores, filtro);
  const marca = destaques(js);
  const tab = el('table', 'tabela compacta');
  tab.append(
    el('thead', '', [
      el('tr', '', [
        el('th', '', 'Elo'),
        el('th', '', 'Jogador'),
        el('th', 'num', 'Liga (V · D · taxa)'),
        el('th', '', 'Função'),
        el('th', 'num', 'Rastreáveis'),
        el('th', '', 'Campeão mais jogado'),
        el('th', '', ''),
      ]),
    ]),
  );
  const tb = el('tbody');
  for (const j of js) {
    const elo = `${nomeElo(j.elo)}${j.divisao ? ` ${j.divisao}` : ''} · ${numero(j.lp)} PDL`;
    const m = marca.get(j.id) || {};
    const sinais = el('div', 'linha sinais', [
      m.maisSobe ? pilula('o que mais está subindo', 'ok', 'o primeiro na ordem de subida deste elo (na lista mostrada)') : null,
      m.recemChegado ? pilula('recém-chegado que mais sobe', 'ac', 'o primeiro com o sinal freshBlood da Riot neste elo') : j.fresh_blood ? pilula('recém-chegado', 'n', 'sinal freshBlood da Riot: chegou há pouco ao elo') : null,
      j.hot_streak ? pilula('sequência de vitórias', 'n', 'sinal hotStreak da Riot') : null,
    ]);
    const rastreaveis = j.vitorias_rastreaveis + j.derrotas_rastreaveis;
    const criar =
      j.estado === 'sem_partida'
        ? null
        : botao('Criar estudo', 'mini', () => abreDialogoEstudo(ctx, j), 'Junta as melhores vitórias e as derrotas mais recentes dele num estudo');
    tb.append(
      el('tr', j.estado === 'sem_partida' ? 'apagada' : '', [
        el('td', '', [Object.assign(el('span', 'num', `${j.posicao}º`), { title: 'posição na ordem de subida (contando os deixados de fora)' }), el('span', '', ` ${elo}`)]),
        el('td', '', [el('b', '', j.riot_id), sinais, el('small', 'sub', j.motivo || '')]),
        el('td', 'num', numerosLiga(j)),
        el('td', '', j.funcao ? `${posicao(j.funcao)}${j.fracao_funcao ? ` · ${porcento(j.fracao_funcao)}` : ''}` : '—'),
        el(
          'td',
          'num',
          rastreaveis ? `${j.vitorias_rastreaveis} V · ${j.derrotas_rastreaveis} D` : '—',
        ),
        el('td', '', [
          j.campeao_id ? retrato({ campeao: j.campeao, campeao_id: j.campeao_id, time: 0 }, 20, 'neutro') : null,
          el('span', '', ` ${j.campeao || '—'}${j.partidas_solo ? ` · ${porcento(j.fracao_campeao)} de ${j.partidas_solo}` : ''}`),
          j.mono ? pilula('um campeão só', 'ac') : null,
        ]),
        el('td', '', [j.estado === 'sem_partida' ? pilula('sem partida rastreável', 'n', 'nenhuma vitória de 15 min ou mais no patch atual') : criar]),
      ]),
    );
  }
  tab.append(tb);
  const sub = `${js.length}${filtro ? ` de ${d.jogadores.length}` : ''} · na ordem de subida de cada elo · rastreáveis = ranqueadas solo do patch atual, 15 min ou mais, na função principal`;
  const corpo = el('div', '', [el('div', 'linha pad', [el('span', 'sub', 'Função:'), sel]), el('div', 'rolar', [tab])]);
  if (!js.length) corpo.append(el('div', 'estado-tela vazio', 'Nenhum jogador desta função.'));
  return cartao('Jogadores', sub, corpo);
}

// --- Diálogo de criar estudo --------------------------------------------------------------------

function abreDialogoEstudo(ctx, j) {
  const dlg = el('dialog', 'dialogo');
  dlg.setAttribute('aria-labelledby', 'dlgEstTitulo');
  const h = el('h2', '', `Criar estudo de ${j.riot_id}`);
  h.id = 'dlgEstTitulo';
  const form = el('form', 'dlg-form');
  form.method = 'dialog';
  const n = el('select', 'campo');
  for (const k of [3, 4, 5]) n.append(Object.assign(el('option', '', `${k} vitórias e ${k} derrotas`), { value: String(k) }));
  const nome = el('input', 'campo');
  nome.placeholder = `${nomeElo(j.elo)} · ${j.riot_id}${j.funcao ? ` · ${posicao(j.funcao)}` : ''}`;
  const tem = `No histórico há ${plural(j.vitorias_rastreaveis, 'vitória rastreável', 'vitórias rastreáveis')} e ${plural(j.derrotas_rastreaveis, 'derrota rastreável', 'derrotas rastreáveis')}${j.funcao ? ` na ${posicao(j.funcao)}` : ''}.`;
  const erro = el('p', 'aviso-linha erro');
  erro.hidden = true;
  const ok = el('button', 'btn pri', 'Criar estudo');
  ok.type = 'submit';
  form.append(
    el('fieldset', 'opcoes', [el('legend', '', 'Quantas partidas de cada lado'), n, el('p', 'sub', 'As melhores vitórias (pela nota da partida) e as derrotas mais recentes, ranqueada solo do patch atual, 15 min ou mais, na função principal dele. A comparação precisa de pelo menos 3 de cada lado.')]),
    el('fieldset', 'opcoes', [el('legend', '', 'Nome (opcional)'), nome]),
    el('p', 'sub', tem),
    erro,
    el('div', 'linha dlg-botoes', [botao('Cancelar', '', () => dlg.close()), ok]),
  );
  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    ok.disabled = true;
    ok.textContent = 'Lendo o histórico…';
    erro.hidden = true;
    try {
      const r = await api.estudoDoJogador(j.id, { por_lado: Number(n.value), nome: nome.value.trim() });
      dlg.close();
      toast(r.aviso ? `Estudo criado com ${r.vitorias} V e ${r.derrotas} D: ${r.aviso}.` : `Estudo criado com ${r.vitorias} vitórias e ${r.derrotas} derrotas.`, r.aviso ? 'erro' : 'ok');
      ctx.navega({ vista: 'estudo', id: r.estudo.id });
    } catch (err) {
      erro.hidden = false;
      erro.textContent = err.message;
      ok.disabled = false;
      ok.textContent = 'Criar estudo';
    }
  });
  dlg.append(h, form);
  document.body.append(dlg);
  dlg.addEventListener('close', () => dlg.remove());
  dlg.showModal();
}

function listaPartidas(d) {
  if (!d.partidas.length) return cartao('Partidas', null, el('div', 'estado-tela vazio', 'Nenhuma partida escolhida ainda.'));
  const nomes = new Map(d.jogadores.map((j) => [j.id, j.riot_id]));
  const tab = el('table', 'tabela compacta');
  tab.append(el('thead', '', [el('tr', '', [el('th', '', 'Elo'), el('th', '', 'Jogador'), el('th', '', 'Partida'), el('th', 'num', 'Nota'), el('th', '', 'Situação'), el('th', '', '')])]));
  const tb = el('tbody');
  for (const p of d.partidas) {
    const s = ESTADOS_PARTIDA[p.estado] || { classe: 'n', texto: p.estado };
    const passes = p.passes_total > 1 ? ` (${p.passes_concluidos}/${p.passes_total})` : '';
    tb.append(
      el('tr', '', [
        el('td', '', nomeElo(p.elo)),
        el('td', '', nomes.get(p.jogador_id) || ''),
        el('td', '', `${p.campeao} · ${kda(p)} · ${duracao(p.duracao_s)}`),
        el('td', 'num', numero(p.pontuacao)),
        el('td', '', [pilula(s.texto + passes, s.classe, p.erro || undefined), p.erro ? el('small', 'sub', ` ${p.erro}`) : null]),
        el('td', '', p.estado === 'concluida' ? link('Abrir', `#partida/${p.game_id}/${p.participante_id}`, 'btn mini') : null),
      ]),
    );
  }
  tab.append(tb);
  const c = d.coleta;
  return cartao('Partidas', `${c.partidas_concluidas} de ${c.partidas_total} prontas${c.partidas_com_erro ? ` · ${c.partidas_com_erro} não deram` : ''}`, el('div', 'rolar', [tab]));
}

function quadroEspaco(e) {
  if (!e) return null;
  const kv = el('div', 'kv');
  kv.append(
    el('span', '', 'Leituras compactadas'),
    el('b', '', bytes(e.compactado)),
    el('span', '', 'Cadeia (o resumo de cada jogador)'),
    el('b', '', bytes(e.cadeia)),
    el('span', '', 'Retratos dos abates e indicadores'),
    el('b', '', bytes(e.retratos + e.indicadores)),
    el('span', '', 'Total agora'),
    el('b', '', bytes(e.total)),
    el('span', '', 'Ao terminar (estimativa)'),
    el('b', '', `~${bytes(e.estimativa_total)}`),
  );
  return cartao('Espaço no disco', `${plural(e.passes, 'jogador rastreado', 'jogadores rastreados')}`, kv);
}

// --- Diálogo de começar -------------------------------------------------------------------------

function abreDialogo(ctx, aoComecar) {
  const dlg = el('dialog', 'dialogo');
  dlg.setAttribute('aria-labelledby', 'dlgAprTitulo');
  const h = el('h2', '', 'Começar o aprendizado por elo');
  h.id = 'dlgAprTitulo';
  const form = el('form', 'dlg-form');
  form.method = 'dialog';

  const reg = el('fieldset', 'opcoes');
  const sel = el('select', 'campo');
  sel.append(Object.assign(el('option', '', 'Brasil (BR)'), { value: 'BR1' }));
  sel.disabled = true;
  sel.setAttribute('aria-label', 'Região');
  reg.append(el('legend', '', 'Região'), sel, el('p', 'sub', 'Por enquanto só o Brasil; outras regiões virão depois. A conta logada no cliente precisa ser do Brasil.'));

  const elosFs = el('fieldset', 'dlg-vel');
  elosFs.append(el('legend', '', 'Elos'));
  const marcados = new Set(ELOS.map((e) => e.elo));
  for (const e of ELOS) {
    const lab = el('label', 'opcao');
    const cb = el('input');
    cb.type = 'checkbox';
    cb.checked = true;
    cb.addEventListener('change', () => {
      if (cb.checked) marcados.add(e.elo);
      else marcados.delete(e.elo);
      atualiza();
    });
    lab.append(cb, el('span', '', e.nome));
    elosFs.append(lab);
  }
  elosFs.append(el('p', 'sub', '10 jogadores por elo. De Mestre para cima, só quem joga quase sempre o mesmo campeão.'));

  const ppj = el('select', 'campo');
  for (const n of [1, 2, 3]) ppj.append(Object.assign(el('option', '', plural(n, 'partida', 'partidas')), { value: String(n) }));
  ppj.addEventListener('change', atualiza);
  const quantas = el('fieldset', 'opcoes', [el('legend', '', 'Partidas por jogador'), ppj, el('p', 'sub', 'As melhores vitórias do patch atual (só elas têm replay).')]);

  const quemFs = el('fieldset', 'dlg-vel');
  quemFs.append(el('legend', '', 'Quem rastrear em cada partida'));
  let quem = 'jogador';
  for (const [v, t] of [
    ['jogador', 'Só o jogador escolhido'],
    ['todos', 'Os 10 (dez vezes mais demorado)'],
  ]) {
    const lab = el('label', 'opcao');
    const r = el('input');
    r.type = 'radio';
    r.name = 'quem';
    r.value = v;
    r.checked = v === quem;
    r.addEventListener('change', () => {
      quem = v;
      atualiza();
    });
    lab.append(r, el('span', '', t));
    quemFs.append(lab);
  }

  const velFs = el('fieldset', 'dlg-vel');
  velFs.append(el('legend', '', 'Velocidade do replay'));
  let velocidade = 4;
  for (const v of [2, 4, 8]) {
    const lab = el('label', 'opcao');
    const r = el('input');
    r.type = 'radio';
    r.name = 'vel';
    r.value = String(v);
    r.checked = v === velocidade;
    r.addEventListener('change', () => {
      velocidade = v;
      atualiza();
    });
    lab.append(r, el('span', '', `${v}×${v === 4 ? ' (padrão)' : ''}`));
    velFs.append(lab);
  }

  // Só descobrir: acha os jogadores (com os números e a função de cada um) e para; nada é rastreado.
  let soDescobrir = false;
  const soFs = el('fieldset', 'dlg-vel');
  const soLab = el('label', 'opcao');
  const soCb = el('input');
  soCb.type = 'checkbox';
  soCb.addEventListener('change', () => {
    soDescobrir = soCb.checked;
    for (const f of [quantas, quemFs, velFs]) f.hidden = soDescobrir;
    atualiza();
  });
  soLab.append(soCb, el('span', '', 'Só descobrir jogadores (não baixa nem rastreia nada)'));
  // Só uma função (ex.: selva): a descoberta pula quem joga outra, até achar 10 desta.
  const funcaoSel = el('select', 'campo');
  funcaoSel.setAttribute('aria-label', 'Só jogadores desta função');
  funcaoSel.append(Object.assign(el('option', '', 'qualquer função'), { value: '' }));
  for (const [v, t] of FUNCOES) funcaoSel.append(Object.assign(el('option', '', `só ${t}`), { value: v }));
  soFs.append(
    el('legend', '', 'O que fazer'),
    soLab,
    el('label', 'campo-rotulado', [el('span', 'sub', 'Função principal dos jogadores'), funcaoSel]),
    el('p', 'sub', 'Bom para escolher um jogador e criar um estudo dele: a lista mostra os números da liga, a função e quantas partidas dá para rastrear.'),
  );

  const estimativa = el('p', 'dlg-estimativa');
  estimativa.setAttribute('aria-live', 'polite');
  const erro = el('p', 'aviso-linha erro');
  erro.hidden = true;
  const confirmar = el('button', 'btn pri', 'Começar');
  confirmar.type = 'submit';
  const cancelar = botao('Cancelar', '', () => dlg.close());

  function atualiza() {
    if (!marcados.size) {
      estimativa.textContent = 'Escolha pelo menos um elo.';
      estimativa.classList.add('ruim');
      confirmar.disabled = true;
      return;
    }
    estimativa.classList.remove('ruim');
    confirmar.disabled = false;
    if (soDescobrir) {
      estimativa.textContent = `Só procura os jogadores de ${plural(marcados.size, 'elo', 'elos')} (até 60 olhados em cada): alguns minutos, sem abrir o jogo.`;
      return;
    }
    const { passes, segundos } = estimativaColeta({ elos: [...marcados], partidasPorJogador: Number(ppj.value), quem, velocidade });
    estimativa.textContent = `Até ${plural(passes, 'jogador rastreado', 'jogadores rastreados')}: leva ${tempoHumano(segundos)} (~${numero(7.5 * (4 / velocidade))} min cada), com o jogo aberto.`;
  }

  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    confirmar.disabled = true;
    erro.hidden = true;
    try {
      await api.criaColeta({
        regiao: 'BR1',
        elos: ELOS.map((e) => e.elo).filter((e) => marcados.has(e)),
        partidas_por_jogador: Number(ppj.value),
        quem,
        velocidade,
        so_descobrir: soDescobrir,
        funcao: funcaoSel.value,
      });
      dlg.close();
      toast(soDescobrir ? 'Procurando os jogadores de cada elo (nada será rastreado).' : 'Coleta começou: primeiro ela procura os jogadores de cada elo.');
      ctx.atualizaSeloRastreio();
      aoComecar();
    } catch (err) {
      erro.hidden = false;
      erro.textContent = err.message;
      confirmar.disabled = false;
    }
  });

  form.append(reg, elosFs, soFs, quantas, quemFs, velFs, estimativa, el('p', 'aviso-linha', LEMBRETE), erro, el('div', 'linha dlg-botoes', [cancelar, confirmar]));
  dlg.append(h, el('p', 'sub', 'A coleta procura os jogadores, escolhe as partidas e rastreia uma por vez.'), form);
  document.body.append(dlg);
  dlg.addEventListener('close', () => dlg.remove());
  dlg.showModal();
  atualiza();
}
