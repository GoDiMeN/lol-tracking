// Estudo: listas de partidas de um jogador escolhidas à mão, com anotação por partida, e a
// comparação dos indicadores nas vitórias × derrotas. Clicar num indicador mostra as partidas e
// os momentos que o compõem, com atalho para o mapa com play.

import { api } from '../api.js';
import { duracao, dataCurta, kda, posicao, POSICOES } from '../formato.js';
import { relogio, valorIndicador, diferenca } from '../texto.js';
import { el, botao, link, pilula, cartao, retrato, carregando, vazio, erroNaTela, confirmaNoBotao, toast } from '../ui.js';

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
  raiz.replaceChildren(h1, explica, caixa, formNovo(jogadores, ctx));
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

  raiz.replaceChildren(cab, explica, cartao('Partidas do estudo', botaoSug, lista), sug, compCartao, composicao);
  if (!partidas.length) sugestoes(sug, estudo, ctx);
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

