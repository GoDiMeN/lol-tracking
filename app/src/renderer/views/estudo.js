// Estudo: listas de partidas de um jogador escolhidas à mão, com anotação por partida, e a
// comparação dos indicadores nas vitórias × derrotas. Clicar num indicador mostra as partidas e
// os momentos que o compõem, com atalho para o mapa com play.

import { api, acompanha } from '../api.js';
import { duracao, dataCurta, kda, posicao, plural, POSICOES } from '../formato.js';
import { relogio, valorIndicador, diferenca, tempoHumano } from '../texto.js';
import { el, botao, link, pilula, cartao, retrato, barra, carregando, vazio, erroNaTela, confirmaNoBotao, toast } from '../ui.js';
import { ESTADOS_COLETA, estimativaEstudo } from '../aprendizado.js';
import { montaCartao, botaoRefazRuins } from './aprendizado.js';
import { montaCartao as cartaoEspecialista } from './especialista.js';
import { montaCartao as cartaoRanking } from './ranking.js';

export async function desenha(raiz, rota, ctx) {
  if (rota.id) return detalhe(raiz, rota, ctx);
  return lista(raiz, ctx);
}

// --- Lista e criação ----------------------------------------------------------------------------

async function lista(raiz, ctx) {
  const h1 = el('h1', 'titulo-tela', 'Estudo');
  raiz.append(h1, carregando('Carregando os estudos…'));
  let estudos;
  let jogadores;
  try {
    [{ estudos }, { jogadores }] = await Promise.all([api.estudos(), api.jogadores()]);
  } catch (err) {
    raiz.replaceChildren(h1, erroNaTela(err));
    return;
  }
  const explica = el(
    'p',
    'sub',
    'Um estudo junta partidas de um jogador escolhidas por você. O app compara, com conta e não com IA, o que foi diferente nas que ele ganhou e nas que perdeu.',
  );
  const caixa = el('div', 'cartao');
  if (!estudos.length) {
    caixa.append(el('div', 'estado-tela vazio', 'Nenhum estudo ainda. Crie o primeiro abaixo.'));
  } else {
    for (const e of estudos) {
      const a = link('', `#estudo/${e.id}`, 'linha-lista');
      a.append(
        el('b', '', e.nome),
        el('span', 'sub', e.jogador + (e.campeao ? ` · ${e.campeao}` : '') + (e.posicao ? ` · ${posicao(e.posicao)}` : '')),
        pilula(`${e.vitorias} vitórias`, 'ok'),
        pilula(`${e.derrotas} derrotas`, 'cr'),
      );
      caixa.append(a);
    }
  }
  const aprendizado = montaCartao(ctx);
  const especialista = cartaoEspecialista();
  const rankingDoLane = cartaoRanking();
  raiz.replaceChildren(h1, explica, aprendizado.el, rankingDoLane.el, especialista.el, caixa, formNovo(jogadores, ctx));
  return () => {
    aprendizado.limpa?.();
    especialista.limpa();
    rankingDoLane.limpa();
  };
}

function formNovo(jogadores, ctx) {
  const f = el('form', 'cartao form-estudo');
  f.append(el('div', 'cab', [el('h2', '', 'Novo estudo')]));
  if (!jogadores.length) {
    f.append(el('div', 'estado-tela vazio', [el('p', '', 'Primeiro busque um jogador em Jogadores.'), link('Ir para Jogadores', '#jogadores', 'btn')]));
    return f;
  }
  const nome = el('input', 'campo');
  nome.required = true;
  nome.placeholder = 'ex.: Akshan no meio';
  const jog = el('select', 'campo');
  for (const j of jogadores) {
    const o = el('option', '', j.riot_id);
    o.value = j.puuid;
    jog.append(o);
  }
  const camp = el('select', 'campo');
  const pos = el('select', 'campo');
  pos.append(Object.assign(el('option', '', 'qualquer posição'), { value: '' }));
  for (const [k, v] of Object.entries(POSICOES)) pos.append(Object.assign(el('option', '', v), { value: k }));

  async function campeoes() {
    camp.replaceChildren(Object.assign(el('option', '', 'qualquer campeão'), { value: '0' }));
    try {
      const { partidas } = await api.partidas(jog.value, 0, 50);
      const vistos = new Map();
      for (const p of partidas) vistos.set(p.campeao_id, p.campeao);
      for (const [id, n] of [...vistos].sort((a, b) => a[1].localeCompare(b[1]))) camp.append(Object.assign(el('option', '', n), { value: String(id) }));
    } catch {
      /* sem as partidas, o filtro de campeão fica em "qualquer" */
    }
  }
  jog.addEventListener('change', campeoes);
  campeoes();

  const campo = (rot, c) => {
    const l = el('label', 'campo-rotulado', [el('span', '', rot), c]);
    return l;
  };
  const ok = el('button', 'btn pri', 'Criar estudo');
  ok.type = 'submit';
  const erro = el('p', 'aviso-linha erro');
  erro.hidden = true;
  f.append(
    el('div', 'grade-form', [campo('Nome', nome), campo('Jogador', jog), campo('Campeão (sugestões)', camp), campo('Posição (sugestões)', pos)]),
    el('p', 'sub pad', 'Campeão e posição só filtram as sugestões; você escolhe as partidas à mão.'),
    erro,
    el('div', 'linha pad', [ok]),
  );
  f.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    ok.disabled = true;
    try {
      const { estudo } = await api.criaEstudo({ nome: nome.value.trim(), puuid: jog.value, campeao_id: Number(camp.value) || 0, posicao: pos.value });
      ctx.navega({ vista: 'estudo', id: estudo.id });
    } catch (err) {
      erro.hidden = false;
      erro.textContent = err.message;
      ok.disabled = false;
    }
  });
  return f;
}

// --- Um estudo ----------------------------------------------------------------------------------

async function detalhe(raiz, rota, ctx) {
  const id = rota.id;
  raiz.append(carregando('Carregando o estudo…'));
  let estudo;
  let partidas;
  let comp;
  try {
    [{ estudo, partidas }, comp] = await Promise.all([api.estudo(id), api.comparacao(id)]);
  } catch (err) {
    raiz.replaceChildren(
      link('← Estudos', '#estudo', 'voltar'),
      erroNaTela(err, err.status === 404 ? 'Esse estudo não existe mais.' : undefined),
    );
    return;
  }

  const cab = el('div', 'linha cab-tela', [
    link('← Estudos', '#estudo', 'voltar'),
    el('h1', 'titulo-tela inline', `Estudo: ${estudo.nome}`),
    pilula(`${estudo.vitorias} vitórias`, 'ok'),
    pilula(`${estudo.derrotas} derrotas`, 'cr'),
  ]);
  const apagar = botao('Apagar estudo', 'perigo');
  apagar.addEventListener('click', () =>
    confirmaNoBotao(apagar, 'Apagar mesmo?', async () => {
      try {
        await api.apagaEstudo(id);
        ctx.navega({ vista: 'estudo' });
      } catch (err) {
        toast(err.message, 'erro');
      }
    }),
  );
  const filtro = [estudo.jogador, estudo.campeao || 'qualquer campeão', estudo.posicao ? posicao(estudo.posicao) : 'qualquer posição'].join(' · ');
  cab.append(el('div', 'linha acoes-dir', [el('span', 'sub', filtro), apagar]));

  const explica = el('p', 'sub', 'Você escolhe as partidas à mão. A comparação é conta, não IA: mostra o que foi diferente, não o porquê.');

  // Comparação + composição.
  const compCartao = tabelaComparacao(comp, id, rota.indicador, ctx);
  const composicao = el('div');
  // Vindo de um clique na tabela (o endereço muda), rola até a composição; abrindo direto, não.
  if (rota.indicador) mostraComposicao(composicao, id, rota.indicador, document.activeElement?.tagName === 'TR');

  // Partidas.
  const lista = listaPartidas(estudo, partidas, ctx);
  const sug = el('div');
  const botaoSug = botao('Adicionar partidas', 'pri', () => sugestoes(sug, estudo, ctx));

  const rastreio = cartaoRastreio(estudo, partidas, ctx);
  raiz.replaceChildren(cab, explica, rastreio.el, cartao('Partidas do estudo', botaoSug, lista), sug, compCartao, composicao);
  if (!partidas.length) sugestoes(sug, estudo, ctx);
  return rastreio.limpa;
}

// --- Rastrear as partidas do estudo ---------------------------------------------------------------

// cartaoRastreio: um botão que rastreia, uma por vez, as partidas em que o jogador do estudo ainda não
// foi rastreado (só ele), com progresso e estimativa. Por dentro é uma coleta do aprendizado sem
// descoberta: pausar e cancelar usam as rotas da coleta. Devolve {el, limpa}.
function cartaoRastreio(estudo, partidas, ctx) {
  const corpo = el('div', 'rastreio-estudo');
  const raiz = cartao('Rastrear as partidas deste estudo', null, corpo);
  let vivo = true;
  let r = null;
  let parar = null;
  let ligada = null;
  let pendente = null;

  async function recarrega() {
    try {
      r = await api.rastreioEstudo(estudo.id);
      if (!vivo) return;
      desenha();
      liga();
    } catch (err) {
      if (vivo) corpo.replaceChildren(erroNaTela(err));
    }
  }
  const logo = () => {
    if (pendente) return;
    pendente = setTimeout(() => {
      pendente = null;
      recarrega();
    }, 1000);
  };

  function liga() {
    const ex = r?.coleta?.execucao?.id;
    if (!ex || ex === ligada || r.coleta.estado !== 'rodando') return;
    ligada = ex;
    parar?.();
    parar = acompanha(
      ex,
      {
        partida_inicio: logo,
        download: logo,
        rastreio: logo,
        partida_fim: logo,
        pausa_pedida: logo,
        pausado: (x) => {
          toast(x.motivo ? `Rastreio do estudo pausado: ${x.motivo}` : 'Rastreio do estudo pausado. Dá para retomar.', x.motivo ? 'erro' : '');
          logo();
        },
        concluido: () => {
          toast('Partidas do estudo rastreadas.', 'ok');
          // A comparação e as marcas "rastreado" mudaram: redesenha a tela inteira.
          ctx.navega({ vista: 'estudo', id: estudo.id }, { substitui: true });
        },
        aviso: (x) => toast(x.texto, 'erro'),
        erro: (x) => toast(x.texto, 'erro'),
        fim: () => {
          ligada = null;
          ctx.atualizaSeloRastreio();
          logo();
        },
      },
      { textoQueda: 'A conexão ao vivo caiu; o cartão segue atualizando a cada poucos segundos.' },
    );
  }

  async function acao(qual) {
    try {
      if (qual === 'comeca') await api.rastreiaEstudo(estudo.id, { velocidade: 4 });
      else if (qual === 'pausa') await api.pausaColeta(r.coleta.id);
      else await api.cancelaColeta(r.coleta.id);
    } catch (err) {
      toast(err.message, 'erro');
    }
    ctx.atualizaSeloRastreio();
    recarrega();
  }

  function desenha() {
    const c = r.coleta;
    const naoRastreadas = r.pendentes;
    const partes = [];
    const rodando = c?.estado === 'rodando';
    const parado = c && (c.estado === 'pausado' || c.estado === 'falhou');
    if (!rodando && !parado) {
      if (!partidas.length) {
        partes.push(el('p', 'sub pad', 'Adicione partidas ao estudo para rastrear.'));
      } else if (!naoRastreadas) {
        partes.push(el('p', 'sub pad', 'Todas as partidas do estudo já têm o jogador rastreado.'));
      } else {
        partes.push(
          el('p', 'sub pad', `${plural(naoRastreadas, 'partida ainda não rastreada', 'partidas ainda não rastreadas')}. O app baixa o replay se precisar, abre o jogo e segue só o jogador do estudo, uma partida por vez; o replay fecha ao fim de cada uma.`),
          el('div', 'linha pad', [
            botao('Rastrear as partidas deste estudo', 'pri', () => acao('comeca')),
            el('span', 'num', `${tempoHumano(estimativaEstudo(naoRastreadas, 4))} a 4× (~7,5 min por partida)`),
          ]),
          el('p', 'nota pad', 'O jogo precisa ficar aberto e visível na tela. Pausar termina a partida em andamento e para; dá para retomar.'),
        );
      }
    }
    if (c) {
      const est = rodando ? { classe: 'ac', texto: 'rastreando' } : ESTADOS_COLETA[c.estado] || { classe: 'n', texto: c.estado };
      const acoes = el('div', 'linha acoes-dir');
      if (rodando) {
        const b = botao(c.pausa_pedida ? 'Pausando…' : 'Pausar', '', () => acao('pausa'), 'Termina a partida em andamento e para');
        b.disabled = c.pausa_pedida;
        acoes.append(b);
      }
      if (parado) acoes.append(botao('Retomar', 'pri', () => acao('comeca')));
      if (rodando || parado) {
        const x = botao('Cancelar', 'perigo');
        x.addEventListener('click', () => confirmaNoBotao(x, 'Cancelar mesmo?', () => acao('cancela')));
        acoes.append(x);
      }
      if (c.rastreio_atual) acoes.append(link('Ver o rastreio', `#rastreio/${c.rastreio_atual}`, 'btn'));
      partes.push(
        el('div', 'linha geral pad', [
          pilula(est.texto, est.classe),
          el('span', 'num', `${c.partidas_concluidas} de ${plural(c.partidas_total, 'partida', 'partidas')}${c.partidas_incompletas ? ` · ${c.partidas_incompletas} incompletas` : ''}${c.partidas_com_erro ? ` · ${c.partidas_com_erro} não deram` : ''}`),
          barra(c.progresso, 'progresso do rastreio do estudo', 'larga'),
          el('span', 'sub', `${Math.round((c.progresso || 0) * 100)}%`),
          c.estimativa_s > 0 ? el('span', 'num', `faltam ${tempoHumano(c.estimativa_s)}`) : null,
          acoes,
        ]),
      );
      if (c.erro) partes.push(el('p', `aviso-linha${c.estado === 'falhou' ? ' erro' : ''}`, c.erro));
      for (const p of r.partidas) {
        if (p.estado === 'erro') partes.push(el('p', 'aviso-linha', `Partida ${p.game_id} (${p.campeao}): ${p.erro}`));
        // Sobrou passe ruim (também na refação automática): a partida não vale até refazer.
        if (p.estado === 'incompleta' && p.passes_ruins > 0) {
          partes.push(
            el('div', 'linha aviso-linha', [
              el('span', '', `Partida ${p.game_id} (${p.campeao}) incompleta: ${p.erro}`),
              botaoRefazRuins(p, () => {
                ctx.atualizaSeloRastreio();
                recarrega();
              }),
            ]),
          );
        }
      }
    }
    corpo.replaceChildren(...partes);
  }

  corpo.replaceChildren(carregando('Carregando…'));
  recarrega();
  const intervalo = setInterval(() => {
    if (r?.coleta?.estado === 'rodando') recarrega();
  }, 5000);
  return {
    el: raiz,
    limpa() {
      vivo = false;
      clearInterval(intervalo);
      clearTimeout(pendente);
      parar?.();
    },
  };
}

function tabelaComparacao(comp, id, escolhido, ctx) {
  const corpo = el('div');
  corpo.append(el('p', 'ressalva', comp.ressalva));
  if (comp.sem_analise?.length) {
    corpo.append(el('p', 'aviso-linha', `${comp.sem_analise.length} partida(s) ainda sem análise (falta a linha do tempo: baixe o replay em Jogadores). Elas não entram na conta.`));
  }
  if (!comp.linhas.length) {
    corpo.append(el('div', 'estado-tela vazio', 'Ainda não há o que comparar: adicione partidas analisadas, de preferência vitórias e derrotas.'));
    return cartao('Vitórias × derrotas', null, corpo);
  }
  const tab = el('table', 'tabela comparacao');
  tab.append(
    el('thead', '', [
      el('tr', '', [
        el('th', '', 'Comportamento'),
        el('th', 'num', 'Vitórias'),
        el('th', 'num', 'Derrotas'),
        el('th', 'num', 'Diferença'),
        el('th', '', ''),
      ]),
    ]),
  );
  const tb = el('tbody');
  // A lista inteira passa de 50 linhas: as primeiras (as de maior efeito) ficam à vista, o resto abre num botão.
  const VISIVEIS = 12;
  const escondidas = [];
  for (const [i, l] of comp.linhas.entries()) {
    const tr = el('tr', `clicavel${l.insuficiente ? ' insuficiente' : ''}${l.indicador === escolhido ? ' escolhido' : ''}`);
    if (i >= VISIVEIS && l.indicador !== escolhido) {
      tr.hidden = true;
      escondidas.push(tr);
    }
    tr.tabIndex = 0;
    tr.setAttribute('role', 'link');
    tr.setAttribute('aria-label', `${l.rotulo}: ver as partidas e os momentos`);
    const lado = (s) => {
      const td = el('td', 'num');
      if (!s || !s.n) {
        td.textContent = '—';
        return td;
      }
      td.append(el('b', '', valorIndicador(s.media, l.unidade)), el('small', '', ` med. ${valorIndicador(s.mediana, l.unidade)} · n ${s.n}`));
      return td;
    };
    const obs = el('td', 'obs');
    if (l.insuficiente) obs.append(pilula('insuficiente', 'n', 'menos de 3 partidas num dos lados'));
    if (l.aproximado) obs.append(pilula('aprox.', 'at', 'rastreio parcial ou posição aproximada em alguma partida'));
    if (l.observacao) obs.append(el('span', 'sub', ` ${l.observacao}`));
    tr.append(el('td', '', l.rotulo), lado(l.vitorias), lado(l.derrotas), el('td', 'num', diferenca(l.diferenca, l.unidade)), obs);
    const abre = () => ctx.navega({ vista: 'estudo', id, indicador: l.indicador });
    tr.addEventListener('click', abre);
    tr.addEventListener('keydown', (ev) => {
      if (ev.key === 'Enter' || ev.key === ' ') {
        ev.preventDefault();
        abre();
      }
    });
    tb.append(tr);
  }
  tab.append(tb);
  corpo.append(el('div', 'rolar', [tab]));
  if (escondidas.length) {
    const mais = botao(`Mostrar os outros ${escondidas.length} indicadores`, 'mini', () => {
      for (const tr of escondidas) tr.hidden = false;
      mais.remove();
    });
    corpo.append(el('div', 'pad', [mais]));
  }
  return cartao('Vitórias × derrotas', `${comp.vitorias} V · ${comp.derrotas} D · clique numa linha para ver de onde vem`, corpo);
}

async function mostraComposicao(caixa, id, nome, rola = false) {
  caixa.replaceChildren(carregando('Carregando as partidas deste indicador…'));
  try {
    const { indicador, partidas } = await api.composicao(id, nome);
    const corpo = el('div', 'composicao');
    corpo.append(el('p', 'sub pad', indicador.descricao || ''));
    for (const p of partidas) {
      const bloco = el('div', `comp-partida ${p.resultado === 'vitoria' ? 'v' : 'd'}`);
      bloco.append(
        el('div', 'linha', [
          el('b', '', `${p.campeao} · ${p.resultado === 'vitoria' ? 'vitória' : 'derrota'}`),
          el('span', 'sub', dataCurta(p.criada_em)),
          el('span', 'num valor', valorIndicador(p.valor, indicador.unidade)),
          p.aproximado ? pilula('aprox.', 'at') : null,
          link('abrir a partida', `#partida/${p.game_id}/${p.participante_id}`, 'btn mini'),
        ]),
      );
      if (p.momentos?.length) {
        bloco.append(
          el(
            'ul',
            'momentos',
            p.momentos.map((m) => el('li', '', [link(`${relogio(m.t_ms)} · ${m.texto}`, `#replay/${p.game_id}/${Math.max(0, Math.round(m.t_ms / 1000) - 5)}`)])),
          ),
        );
      }
      corpo.append(bloco);
    }
    if (!partidas.length) corpo.append(el('div', 'estado-tela vazio', 'Nenhuma partida do estudo tem este indicador.'));
    const fechar = link('fechar', `#estudo/${id}`, 'btn mini');
    caixa.replaceChildren(cartao(indicador.rotulo, fechar, corpo));
    if (rola) caixa.scrollIntoView({ block: 'nearest' });
  } catch (err) {
    caixa.replaceChildren(cartao('Indicador', null, erroNaTela(err)));
  }
}

function listaPartidas(estudo, partidas, ctx) {
  if (!partidas.length) return el('div', 'estado-tela vazio', 'Nenhuma partida ainda. Escolha nas sugestões abaixo.');
  const box = el('div');
  for (const p of partidas) {
    const linha = el('div', `partida-estudo ${p.resultado === 'vitoria' ? 'v' : 'd'}`);
    linha.append(el('i'));
    const info = el('div', 'com-retrato', [
      retrato({ campeao: p.campeao, campeao_id: p.campeao_id, time: 0 }, 30, 'neutro'),
      el('div', '', [
        el('div', 't', `${p.campeao}${p.posicao ? ` · ${posicao(p.posicao)}` : ''} · ${p.resultado === 'vitoria' ? 'vitória' : 'derrota'}`),
        el('div', 'm', `${dataCurta(p.criada_em)} · ${duracao(p.duracao_s)} · ${kda(p)}`),
      ]),
    ]);
    const marcas = el('div', 'linha');
    if (p.rastreado) marcas.append(pilula(`rastreado${p.rastreados.length ? ` (${p.rastreados.length} de 10)` : ''}`, 'ac'));
    else marcas.append(pilula('não rastreado', 'n', 'os indicadores vêm só da linha do tempo'));
    if (!p.analisada) marcas.append(pilula('sem análise', 'at', 'falta a linha do tempo: baixe o replay em Jogadores'));
    if (p.fora_do_filtro) marcas.append(pilula('fora do filtro', 'n'));
    const nota = el('textarea', 'campo anotacao');
    nota.rows = 2;
    nota.placeholder = 'Anotação: o que você viu nesta partida';
    nota.value = p.anotacao || '';
    nota.setAttribute('aria-label', `Anotação da partida de ${dataCurta(p.criada_em)}`);
    let salvo = nota.value;
    nota.addEventListener('blur', async () => {
      if (nota.value === salvo) return;
      try {
        await api.anota(estudo.id, p.game_id, nota.value);
        salvo = nota.value;
        toast('Anotação guardada.', 'ok');
      } catch (err) {
        toast(`A anotação não foi guardada: ${err.message}`, 'erro');
      }
    });
    const tirar = botao('Tirar', 'mini');
    tirar.addEventListener('click', () =>
      confirmaNoBotao(tirar, 'Tirar mesmo?', async () => {
        try {
          await api.tiraPartida(estudo.id, p.game_id);
          ctx.navega({ vista: 'estudo', id: estudo.id }, { substitui: true });
        } catch (err) {
          toast(err.message, 'erro');
        }
      }),
    );
    const acoes = el('div', 'linha acoes', [
      p.rastreado ? link('Mapa com play', `#replay/${p.game_id}`, 'btn mini') : null,
      p.rastreado ? link('Abrir', `#partida/${p.game_id}/${p.participante_id}`, 'btn mini') : null,
      tirar,
    ]);
    linha.append(el('div', 'pe-info', [info, marcas]), nota, acoes);
    box.append(linha);
  }
  return box;
}

async function sugestoes(caixa, estudo, ctx) {
  caixa.replaceChildren(carregando('Procurando partidas do jogador…'));
  try {
    const { partidas } = await api.sugestoes(estudo.id);
    if (!partidas.length) {
      caixa.replaceChildren(
        cartao(
          'Sugestões',
          null,
          el('div', 'estado-tela vazio', 'Nenhuma partida guardada deste jogador fora da lista. Em Jogadores, busque o jogador para trazer mais partidas.'),
        ),
      );
      return;
    }
    const box = el('div');
    for (const p of partidas) {
      const linha = el('div', `partida ${p.resultado === 'vitoria' ? 'v' : 'd'}`);
      linha.append(
        el('i'),
        el('div', 'com-retrato', [
          retrato({ campeao: p.campeao, campeao_id: p.campeao_id, time: 0 }, 28, 'neutro'),
          el('div', '', [
            el('div', 't', `${p.campeao}${p.posicao ? ` · ${posicao(p.posicao)}` : ''} · ${p.resultado === 'vitoria' ? 'vitória' : 'derrota'}`),
            el('div', 'm', `${dataCurta(p.criada_em)} · ${duracao(p.duracao_s)} · ${kda(p)}`),
          ]),
        ]),
        el('div', 'm m2', [
          p.rastreado ? pilula('rastreado', 'ac') : null,
          p.tem_linha_do_tempo ? null : pilula('sem linha do tempo', 'at', 'sem ela não há indicadores: baixe o replay em Jogadores'),
        ]),
      );
      const b = botao('Adicionar', '', async () => {
        b.disabled = true;
        try {
          await api.poePartida(estudo.id, p.game_id, '');
          ctx.navega({ vista: 'estudo', id: estudo.id }, { substitui: true });
        } catch (err) {
          toast(err.message, 'erro');
          b.disabled = false;
        }
      });
      linha.append(el('div', 'linha acoes', [b]));
      box.append(linha);
    }
    caixa.replaceChildren(cartao('Sugestões', `${partidas.length} partidas do jogador fora da lista`, box));
  } catch (err) {
    caixa.replaceChildren(cartao('Sugestões', null, erroNaTela(err)));
  }
}

