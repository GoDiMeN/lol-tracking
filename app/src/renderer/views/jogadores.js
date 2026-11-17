// Tela Jogadores: busca por Riot ID, lista de partidas com o estado do replay, download com
// progresso ao vivo e o botão Rastrear (com a escolha dos jogadores, velocidade e trecho).
// O estado fica no módulo: voltar para esta tela mostra o mesmo jogador e as mesmas partidas.

import { api, acompanha } from '../api.js';
import { duracao, dataCurta, kda, titulo, pilulaReplay, contagem, plural } from '../formato.js';
import { estimativaRastreio, tempoHumano, podeRastrear, lerRelogio, relogio, ESTADOS_RASTREIO } from '../texto.js';
import { el, botao, link, pilula, retrato, barra, lembraJogador, ultimoJogador, preferencia, guardaPreferencia, toast } from '../ui.js';

const POR_PAGINA = 20;

const estado = {
  jogador: null,
  partidas: [],
  inicio: 0,
  temMais: false,
  carregando: false,
  aviso: '',
  avisoErro: false,
  rastreadas: new Map(), // game_id → {participantes, rastreados}
  rastreios: new Map(), // game_id → rastreio mais novo
};

let tela = null; // os nós da tela montada agora (null fora dela)
let ctxApp = null;

export async function desenha(raiz, rota, ctx) {
  ctxApp = ctx;
  tela = monta(raiz);
  desenhaCliente();
  const desliga = ctx.aoMudarEstado(() => {
    desenhaCliente();
    desenhaLista();
  });
  carregaRecentes();
  carregaRastreadas().then(desenhaLista);

  const pedido = rota.puuid || (!estado.jogador ? ultimoJogador() : null);
  if (pedido && pedido !== estado.jogador?.puuid) {
    abreJogador(pedido);
  } else {
    if (estado.jogador) tela.busca.value = estado.jogador.riot_id;
    avisa(estado.aviso, estado.avisoErro);
    desenhaLista();
  }
  return () => {
    desliga();
    tela = null;
  };
}

function monta(raiz) {
  const form = el('form', 'linha');
  form.autocomplete = 'off';
  const busca = el('input', 'campo');
  busca.name = 'busca';
  busca.placeholder = 'Nome#TAG (sem #, usa a tag da região)';
  busca.setAttribute('aria-label', 'Nome do jogador');
  busca.required = true;
  const botaoBuscar = el('button', 'btn pri', 'Buscar');
  botaoBuscar.type = 'submit';
  form.append(busca, botaoBuscar);
  form.addEventListener('submit', (ev) => {
    ev.preventDefault();
    const texto = busca.value.trim();
    if (texto) buscaTexto(texto);
  });

  const cliente = el('p', 'aviso-linha', '');
  cliente.hidden = true;
  const recentes = el('div', 'linha recentes');
  recentes.setAttribute('aria-label', 'Buscados recentemente');
  const aviso = el('p', 'aviso-linha');
  aviso.setAttribute('role', 'alert');
  aviso.hidden = true;
  const resumo = el('div', 'linha resumo');
  resumo.hidden = true;
  const lista = el('div', 'cartao');
  lista.hidden = true;
  const mais = botao('Carregar mais', '', () => carregaPartidas(true));
  const maisLinha = el('div', 'linha', [mais]);
  maisLinha.hidden = true;

  const h1 = el('h1', 'titulo-tela', 'Jogadores');
  raiz.append(h1, form, cliente, recentes, aviso, resumo, lista, maisLinha);
  return { busca, botaoBuscar, cliente, recentes, aviso, resumo, lista, mais, maisLinha };
}

function avisa(texto, erro = false) {
  estado.aviso = texto || '';
  estado.avisoErro = erro;
  if (!tela) return;
  tela.aviso.hidden = !texto;
  tela.aviso.textContent = texto || '';
  tela.aviso.classList.toggle('erro', erro);
}

// O aviso do cliente: com ele fechado (ou numa partida) dá para ver, mas não baixar nem rastrear.
function desenhaCliente() {
  if (!tela) return;
  const e = ctxApp.estado();
  const p = podeRastrear(e);
  tela.cliente.hidden = p.ok || !e;
  if (!e?.lcu?.conectado) {
    tela.cliente.textContent =
      'O cliente do LoL está fechado. Dá para ver as partidas já guardadas; para buscar jogador novo, baixar e rastrear, abra o cliente e entre na conta.';
  } else {
    tela.cliente.textContent = p.motivo;
  }
}

// --- Recentes --------------------------------------------------------------------------------

async function carregaRecentes() {
  try {
    const { jogadores } = await api.recentes();
    if (!tela) return;
    tela.recentes.replaceChildren(
      ...jogadores.map((j) =>
        botao(j.riot_id, '', () => {
          tela.busca.value = j.riot_id;
          abreJogador(j.puuid, j);
        }),
      ),
    );
  } catch {
    tela?.recentes.replaceChildren();
  }
}

async function carregaRastreadas() {
  try {
    const [{ partidas }, { rastreios }] = await Promise.all([api.rastreadas(), api.rastreios(100)]);
    estado.rastreadas = new Map(partidas.map((p) => [p.game_id, p]));
    estado.rastreios = new Map();
    for (const r of rastreios) if (!estado.rastreios.has(r.game_id)) estado.rastreios.set(r.game_id, r);
  } catch {
    /* sem a marcação, a lista continua útil */
  }
}

// --- Lista de partidas -----------------------------------------------------------------------

function desenhaResumo() {
  const r = tela.resumo;
  if (!estado.jogador) {
    r.hidden = true;
    return;
  }
  const c = contagem(estado.partidas);
  const partes = [el('b', '', estado.jogador.riot_id), el('span', '', plural(c.total, 'partida', 'partidas'))];
  if (c.daParaVer) partes.push(pilula(`${c.daParaVer} dá para ver`, 'ok'));
  if (c.antigas) partes.push(pilula(`${c.antigas} de patch antigo`, 'n'));
  const nRastreadas = estado.partidas.filter((p) => estado.rastreadas.has(p.game_id)).length;
  if (nRastreadas) partes.push(pilula(`${nRastreadas} rastreada${nRastreadas > 1 ? 's' : ''}`, 'ac'));
  r.replaceChildren(...partes);
  r.hidden = false;
}

function linhaPartida(p) {
  const linha = el('div', `partida ${p.venceu ? 'v' : 'd'}`);
  linha.dataset.id = String(p.game_id);
  linha.append(el('i'));

  const meio = el('div', 'com-retrato');
  meio.append(retrato({ campeao: p.campeao, campeao_id: p.campeao_id, time: 0 }, 32, 'neutro'));
  meio.append(el('div', '', [el('div', 't', titulo(p)), el('div', 'm', [p.fila_nome, duracao(p.duracao_s), dataCurta(p.criada_em)].join(' · '))]));
  linha.append(meio);
  linha.append(el('div', 'm m2', `${kda(p)} · ${p.cs} CS`));

  const acoes = el('div', 'linha acoes');
  const rastreada = estado.rastreadas.get(p.game_id);
  const rastreio = estado.rastreios.get(p.game_id);
  if (rastreio && (rastreio.estado === 'rodando' || rastreio.estado === 'pausado')) {
    const e = ESTADOS_RASTREIO[rastreio.estado];
    acoes.append(link(`${e.texto} ${Math.round((rastreio.progresso || 0) * 100)}%`, `#rastreio/${rastreio.id}`, `pill ${e.classe} pill-link`));
  } else if (rastreada) {
    acoes.append(pilula(`${rastreada.rastreados} de 10 rastreados`, 'ac'));
  }

  const pil = pilulaReplay(p.replay, p.patch);
  const pilulaEl = pilula(pil.texto, pil.classe, p.replay?.detalhe);
  acoes.append(pilulaEl);
  const e = ctxApp.estado();
  const pode = podeRastrear(e);
  if (p.replay?.estado === 'baixando') {
    acoes.append(barra((p.replay.progresso || 0) / 100, 'download do replay'));
  } else if (p.replay?.estado === 'baixavel') {
    const b = botao('Baixar', '', () => baixa(p));
    if (!e?.lcu?.conectado) {
      b.disabled = true;
      b.title = 'Abra o cliente do LoL para baixar.';
    }
    acoes.append(b);
  } else if (p.replay?.estado === 'baixado') {
    const b = botao('Rastrear', 'pri', () => abreDialogo(p));
    if (!pode.ok) {
      b.disabled = true;
      b.title = pode.motivo;
    }
    acoes.append(b);
  }
  if (rastreada) {
    const pid = null; // a tela da partida escolhe o jogador (o buscado, se jogou)
    acoes.append(link('Abrir', `#partida/${p.game_id}${pid ? `/${pid}` : ''}`, 'btn'));
  }
  linha.append(acoes);
  return linha;
}

function desenhaLista() {
  if (!tela) return;
  const lista = tela.lista;
  if (!estado.jogador) {
    lista.hidden = false;
    lista.replaceChildren(
      el('div', 'estado-tela vazio', [
        el('p', '', 'Digite o Riot ID de um jogador (Nome#TAG) para ver as partidas dele.'),
        el('p', 'sub', 'O replay só abre no patch em que a partida foi jogada; as de patch antigo aparecem, mas não dá para rastrear.'),
      ]),
    );
    tela.maisLinha.hidden = true;
    tela.resumo.hidden = true;
    return;
  }
  lista.hidden = false;
  if (!estado.partidas.length) {
    lista.replaceChildren(el('div', 'estado-tela vazio', estado.carregando ? 'Carregando partidas…' : 'Nenhuma partida encontrada.'));
  } else {
    lista.replaceChildren(...estado.partidas.map(linhaPartida));
  }
  tela.maisLinha.hidden = !estado.temMais;
  tela.mais.disabled = estado.carregando;
  desenhaResumo();
}

// Troca uma partida na lista e redesenha só a linha dela.
function trocaPartida(gameId, muda) {
  const i = estado.partidas.findIndex((p) => p.game_id === gameId);
  if (i < 0) return;
  estado.partidas[i] = muda(estado.partidas[i]);
  if (!tela) return;
  const velha = tela.lista.querySelector(`[data-id="${gameId}"]`);
  if (velha) velha.replaceWith(linhaPartida(estado.partidas[i]));
  desenhaResumo();
}

async function carregaPartidas(maisUma = false) {
  if (!estado.jogador || estado.carregando) return;
  estado.carregando = true;
  if (!maisUma) {
    estado.partidas = [];
    estado.inicio = 0;
  }
  desenhaLista();
  try {
    const pag = await api.partidas(estado.jogador.puuid, estado.inicio, POR_PAGINA);
    estado.partidas = estado.partidas.concat(pag.partidas);
    estado.inicio = pag.inicio + pag.quantidade;
    estado.temMais = pag.tem_mais;
    // Com o cliente fechado o aviso de cima já explica; o do servidor repetiria.
    avisa(pag.fonte === 'banco' && !ctxApp.estado()?.lcu?.conectado ? '' : pag.aviso || '');
  } catch (err) {
    avisa(err.message, true);
  } finally {
    estado.carregando = false;
    desenhaLista();
  }
}

function escolheJogador(jogador) {
  estado.jogador = jogador;
  lembraJogador(jogador.puuid);
  ctxApp.marca({ vista: 'jogadores', puuid: jogador.puuid });
  if (tela) tela.busca.value = jogador.riot_id;
}

async function buscaTexto(texto) {
  avisa('');
  tela.botaoBuscar.disabled = true;
  try {
    const { jogador } = await api.busca(texto);
    escolheJogador(jogador);
    carregaRecentes();
    await carregaPartidas();
  } catch (err) {
    avisa(err.status === 503 ? 'O cliente do LoL está fechado: abra o cliente para buscar um jogador novo.' : err.message, true);
  } finally {
    if (tela) tela.botaoBuscar.disabled = false;
  }
}

// abreJogador: um jogador já buscado antes (recentes, endereço): não precisa do cliente.
async function abreJogador(puuid, conhecido) {
  avisa('');
  try {
    const jogador = conhecido || (await api.jogador(puuid)).jogador;
    escolheJogador(jogador);
    await carregaPartidas();
  } catch (err) {
    avisa(err.status === 404 ? 'Esse jogador ainda não foi buscado neste computador.' : err.message, true);
    desenhaLista();
  }
}

// --- Download ---------------------------------------------------------------------------------

async function baixa(p) {
  trocaPartida(p.game_id, (x) => ({ ...x, replay: { ...x.replay, estado: 'baixando', progresso: 0 } }));
  let execucao;
  try {
    ({ execucao } = await api.baixa(p.game_id));
  } catch (err) {
    // 409: já há um download desta partida; acompanha o que existe.
    if (err.status === 409 && err.corpo?.execucao?.id) {
      execucao = err.corpo.execucao;
    } else {
      avisa(err.message, true);
      trocaPartida(p.game_id, (x) => ({ ...x, replay: p.replay }));
      return;
    }
  }
  let terminou = false;
  acompanha(execucao.id, {
    progresso: (r) => trocaPartida(p.game_id, (x) => ({ ...x, replay: r })),
    concluido: (d) => {
      terminou = true;
      trocaPartida(p.game_id, (x) => ({ ...x, replay: d.replay, tem_linha_do_tempo: d.linha_do_tempo }));
      toast('Replay baixado: já dá para rastrear.', 'ok');
    },
    aviso: (d) => avisa(d.texto),
    erro: (d) => {
      terminou = true;
      avisa(`O download não terminou: ${d.texto}`, true);
      trocaPartida(p.game_id, (x) => ({ ...x, replay: p.replay }));
    },
    fim: () => {
      if (!terminou) trocaPartida(p.game_id, (x) => ({ ...x, replay: p.replay }));
    },
  });
}

// --- Rastrear (diálogo) -------------------------------------------------------------------------

async function abreDialogo(p) {
  const dlg = el('dialog', 'dialogo');
  dlg.setAttribute('aria-labelledby', 'dlgTitulo');
  const h = el('h2', '', `Rastrear: ${titulo(p)}`);
  h.id = 'dlgTitulo';
  const sub = el('p', 'sub', `${p.fila_nome} · ${duracao(p.duracao_s)} · ${dataCurta(p.criada_em)}`);
  const corpo = el('div', 'dlg-corpo', [carregandoTexto()]);
  dlg.append(h, sub, corpo);
  document.body.append(dlg);
  dlg.addEventListener('close', () => dlg.remove());
  dlg.showModal();

  let partida;
  try {
    ({ partida } = await api.partida(p.game_id));
  } catch (err) {
    corpo.replaceChildren(el('p', 'aviso-linha erro', `Não deu para ler os 10 jogadores da partida: ${err.message}`), linhaBotoes(dlg));
    return;
  }

  const form = el('form', 'dlg-form');
  form.method = 'dialog';

  // Jogadores: os dois times lado a lado.
  const escolhidos = new Set(partida.participantes.map((x) => x.participante_id));
  const marcas = [];
  const times = el('div', 'dlg-times');
  for (const time of [100, 200]) {
    const fs = el('fieldset', `dlg-time ${time === 100 ? 'azul' : 'vermelho'}`);
    fs.append(el('legend', '', time === 100 ? 'Time azul' : 'Time vermelho'));
    for (const pt of partida.participantes.filter((x) => x.time === time)) {
      const lab = el('label', 'dlg-jogador');
      const cb = el('input');
      cb.type = 'checkbox';
      cb.checked = true;
      cb.value = String(pt.participante_id);
      cb.addEventListener('change', () => {
        if (cb.checked) escolhidos.add(pt.participante_id);
        else escolhidos.delete(pt.participante_id);
        atualiza();
      });
      marcas.push(cb);
      lab.append(cb, retrato(pt, 24), el('span', '', [el('b', '', pt.campeao), el('small', '', `${pt.nome}${pt.tag ? `#${pt.tag}` : ''}`)]));
      fs.append(lab);
    }
    times.append(fs);
  }
  const todos = botao('Todos', '', () => {
    for (const cb of marcas) {
      cb.checked = true;
      escolhidos.add(Number(cb.value));
    }
    atualiza();
  });
  const so = botao('Só o buscado', '', () => {
    const dono = partida.participantes.find((x) => x.puuid === estado.jogador?.puuid);
    for (const cb of marcas) {
      cb.checked = dono && Number(cb.value) === dono.participante_id;
      if (cb.checked) escolhidos.add(Number(cb.value));
      else escolhidos.delete(Number(cb.value));
    }
    atualiza();
  });

  // Velocidade.
  const vel = el('fieldset', 'dlg-vel');
  vel.append(el('legend', '', 'Velocidade do replay'));
  let velocidade = Number(preferencia('velocidade', '2')) || 2;
  for (const v of [2, 4, 8]) {
    const lab = el('label', 'opcao');
    const r = el('input');
    r.type = 'radio';
    r.name = 'velocidade';
    r.value = String(v);
    r.checked = v === velocidade;
    r.addEventListener('change', () => {
      velocidade = v;
      atualiza();
    });
    lab.append(r, el('span', '', `${v}×${v === 2 ? ' (recomendada)' : ''}`));
    vel.append(lab);
  }
  vel.append(el('p', 'sub', 'A 2× a leitura da tela foi medida e confere. Mais rápido termina antes, mas lê menos vezes por segundo de jogo.'));

  // Trecho.
  const trecho = el('fieldset', 'dlg-trecho');
  trecho.append(el('legend', '', 'Trecho (opcional)'));
  const de = el('input', 'campo curto');
  de.placeholder = '0:00';
  de.setAttribute('aria-label', 'De (minutos:segundos)');
  const ate = el('input', 'campo curto');
  ate.placeholder = duracao(p.duracao_s);
  ate.setAttribute('aria-label', 'Até (minutos:segundos)');
  for (const i of [de, ate]) i.addEventListener('input', atualiza);
  trecho.append(el('div', 'linha', [el('span', '', 'de'), de, el('span', '', 'até'), ate, el('span', 'sub', 'em minutos:segundos; vazio = a partida inteira')]));

  const estimativa = el('p', 'dlg-estimativa');
  estimativa.setAttribute('aria-live', 'polite');
  const erro = el('p', 'aviso-linha erro');
  erro.hidden = true;
  const lembrete = el('p', 'aviso-linha', 'O jogo vai abrir sozinho e precisa ficar visível na tela até o fim. Mexer no mouse não atrapalha; cobrir a janela do jogo, sim. Ao terminar, o replay fecha.');

  const confirmar = el('button', 'btn pri', 'Rastrear');
  confirmar.type = 'submit';
  const cancelar = botao('Cancelar', '', () => dlg.close());

  function trechoMs() {
    const d = de.value.trim() ? lerRelogio(de.value) : 0;
    const a = ate.value.trim() ? lerRelogio(ate.value) : 0;
    return { d, a };
  }

  function atualiza() {
    const { d, a } = trechoMs();
    let problema = '';
    if (d === null || a === null) problema = 'Escreva o trecho como 8:30 (minutos:segundos).';
    else if (a && a <= d) problema = 'O fim do trecho precisa vir depois do começo.';
    else if (d >= p.duracao_s * 1000) problema = `A partida tem ${duracao(p.duracao_s)}.`;
    else if (!escolhidos.size) problema = 'Escolha pelo menos um jogador.';
    confirmar.disabled = Boolean(problema);
    if (problema) {
      estimativa.textContent = problema;
      estimativa.classList.add('ruim');
      return;
    }
    estimativa.classList.remove('ruim');
    const s = estimativaRastreio({ duracaoS: p.duracao_s, jogadores: escolhidos.size, velocidade, deS: d / 1000, ateS: a / 1000 });
    const fim = a ? relogio(a) : duracao(p.duracao_s);
    estimativa.textContent = `${plural(escolhidos.size, 'jogador', 'jogadores')}, de ${relogio(d)} a ${fim}, a ${velocidade}×: leva ${tempoHumano(s)}.`;
  }

  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    const { d, a } = trechoMs();
    confirmar.disabled = true;
    erro.hidden = true;
    try {
      const corpo = { jogadores: [...escolhidos].sort((x, y) => x - y), velocidade };
      if (d) corpo.de_ms = d;
      if (a) corpo.ate_ms = a;
      const { rastreio } = await api.rastrear(p.game_id, corpo);
      guardaPreferencia('velocidade', velocidade);
      dlg.close();
      ctxApp.atualizaSeloRastreio();
      ctxApp.navega({ vista: 'rastreio', id: rastreio.id });
    } catch (err) {
      erro.hidden = false;
      erro.textContent = err.corpo?.rastreio_id
        ? `Já há um rastreio rodando (nº ${err.corpo.rastreio_id}). Espere ele terminar ou pause-o na Fila de rastreio.`
        : err.message;
      confirmar.disabled = false;
    }
  });

  form.append(
    el('div', 'linha dlg-sel', [el('span', 'rot2', 'Quem rastrear'), todos, so]),
    times,
    vel,
    trecho,
    estimativa,
    lembrete,
    erro,
    el('div', 'linha dlg-botoes', [cancelar, confirmar]),
  );
  corpo.replaceChildren(form);
  atualiza();
  confirmar.focus();
}

function carregandoTexto() {
  return el('p', 'sub', 'Lendo os 10 jogadores da partida…');
}

function linhaBotoes(dlg) {
  return el('div', 'linha dlg-botoes', [botao('Fechar', '', () => dlg.close())]);
}
