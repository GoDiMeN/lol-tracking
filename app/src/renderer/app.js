// A tela Jogadores: busca por Riot ID, lista de partidas com o estado do replay e o download com
// progresso ao vivo. As outras telas chegam nas próximas tarefas.
//
// Todo texto que vem do servidor entra por textContent: nome de jogador é texto livre de terceiros.

import { api, acompanha } from './api.js';
import { duracao, dataCurta, kda, titulo, pilulaReplay, contagem, plural } from './formato.js';

const POR_PAGINA = 20;
const $ = (id) => document.getElementById(id);

const estado = { jogador: null, partidas: [], inicio: 0, temMais: false, carregando: false };

function el(tag, classe, texto) {
  const e = document.createElement(tag);
  if (classe) e.className = classe;
  if (texto !== undefined) e.textContent = texto;
  return e;
}

function avisa(texto, erro = false) {
  const a = $('aviso');
  a.hidden = !texto;
  a.textContent = texto || '';
  a.classList.toggle('erro', erro);
}

// --- Barra de título -------------------------------------------------------------------------

async function atualizaStatus() {
  try {
    const e = await api.estado();
    const ponto = $('statusPonto');
    if (e.lcu.conectado) {
      ponto.className = 'ponto ok';
      const quem = e.invocador ? `${e.invocador.nome}#${e.invocador.tag}` : '';
      const partes = ['cliente do LoL conectado', quem, e.patch];
      if (e.fase && e.fase !== 'None') partes.push(e.fase);
      if (e.modo_teste) partes.push('modo teste');
      $('statusTexto').textContent = partes.filter(Boolean).join(' · ');
    } else {
      ponto.className = 'ponto cr';
      $('statusTexto').textContent = e.lcu.erro || 'cliente do LoL desconectado';
    }
  } catch (err) {
    $('statusPonto').className = 'ponto cr';
    $('statusTexto').textContent = err.message;
  }
}

// --- Recentes --------------------------------------------------------------------------------

async function carregaRecentes() {
  const caixa = $('recentes');
  try {
    const { jogadores } = await api.recentes();
    caixa.replaceChildren(
      ...jogadores.map((j) => {
        const b = el('button', 'btn', j.riot_id);
        b.type = 'button';
        b.addEventListener('click', () => {
          $('busca').value = j.riot_id;
          busca(j.riot_id);
        });
        return b;
      }),
    );
  } catch {
    caixa.replaceChildren();
  }
}

// --- Lista de partidas -----------------------------------------------------------------------

function desenhaResumo() {
  const r = $('resumo');
  if (!estado.jogador) {
    r.hidden = true;
    return;
  }
  const c = contagem(estado.partidas);
  const partes = [el('b', '', estado.jogador.riot_id), el('span', '', plural(c.total, 'partida', 'partidas'))];
  if (c.daParaVer) partes.push(el('span', 'pill ok', `${c.daParaVer} dá para ver`));
  if (c.antigas) partes.push(el('span', 'pill n', `${c.antigas} de patch antigo`));
  r.replaceChildren(...partes);
  r.hidden = false;
}

function linhaPartida(p) {
  const linha = el('div', `partida ${p.venceu ? 'v' : 'd'}`);
  linha.dataset.id = String(p.game_id);
  linha.append(el('i'));

  const meio = el('div');
  meio.append(el('div', 't', titulo(p)));
  meio.append(el('div', 'm', [p.fila_nome, duracao(p.duracao_s), dataCurta(p.criada_em)].join(' · ')));
  linha.append(meio);

  linha.append(el('div', 'm m2', `${kda(p)} · ${p.cs} CS`));

  const acoes = el('div', 'linha acoes');
  const pil = pilulaReplay(p.replay, p.patch);
  const pilula = el('span', `pill ${pil.classe}`, pil.texto);
  if (p.replay?.detalhe) pilula.title = p.replay.detalhe;
  acoes.append(pilula);
  if (p.replay?.estado === 'baixando') {
    const barra = el('div', 'barra');
    barra.setAttribute('role', 'progressbar');
    barra.setAttribute('aria-label', 'download do replay');
    barra.setAttribute('aria-valuemin', '0');
    barra.setAttribute('aria-valuemax', '100');
    barra.setAttribute('aria-valuenow', String(Math.round(p.replay.progresso || 0)));
    const b = el('b');
    b.style.width = `${Math.min(100, p.replay.progresso || 0)}%`;
    barra.append(b);
    acoes.append(barra);
  } else if (p.replay?.estado === 'baixavel') {
    const botao = el('button', 'btn', 'Baixar');
    botao.type = 'button';
    botao.addEventListener('click', () => baixa(p));
    acoes.append(botao);
  }
  linha.append(acoes);
  return linha;
}

function desenhaLista() {
  const lista = $('lista');
  if (!estado.jogador) {
    lista.hidden = true;
    return;
  }
  lista.hidden = false;
  if (!estado.partidas.length) {
    lista.replaceChildren(el('div', 'vazio', estado.carregando ? 'Carregando partidas…' : 'Nenhuma partida encontrada.'));
  } else {
    lista.replaceChildren(...estado.partidas.map(linhaPartida));
  }
  $('maisLinha').hidden = !estado.temMais;
  $('botaoMais').disabled = estado.carregando;
  desenhaResumo();
}

// Troca uma partida na lista e redesenha só a linha dela.
function trocaPartida(gameId, muda) {
  const i = estado.partidas.findIndex((p) => p.game_id === gameId);
  if (i < 0) return;
  estado.partidas[i] = muda(estado.partidas[i]);
  const velha = $('lista').querySelector(`[data-id="${gameId}"]`);
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
    avisa(pag.aviso || '');
  } catch (err) {
    avisa(err.message, true);
  } finally {
    estado.carregando = false;
    desenhaLista();
  }
}

async function busca(texto) {
  avisa('');
  $('botaoBuscar').disabled = true;
  try {
    const { jogador } = await api.busca(texto);
    estado.jogador = jogador;
    carregaRecentes();
    await carregaPartidas();
  } catch (err) {
    avisa(err.message, true);
  } finally {
    $('botaoBuscar').disabled = false;
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
    },
    aviso: (d) => avisa(d.texto),
    erro: (d) => {
      terminou = true;
      avisa(`Partida ${p.game_id}: ${d.texto}`, true);
      trocaPartida(p.game_id, (x) => ({ ...x, replay: p.replay }));
    },
    fim: () => {
      // Fim sem conclusão nem erro (cancelado): volta ao estado anterior.
      if (!terminou) trocaPartida(p.game_id, (x) => ({ ...x, replay: p.replay }));
    },
  });
}

// --- Arranque ---------------------------------------------------------------------------------

$('formBusca').addEventListener('submit', (ev) => {
  ev.preventDefault();
  const texto = $('busca').value.trim();
  if (texto) busca(texto);
});
$('botaoMais').addEventListener('click', () => carregaPartidas(true));

atualizaStatus();
carregaRecentes();
// O cliente do LoL abre e fecha enquanto o app está aberto: a barra de título acompanha.
setInterval(atualizaStatus, 15000);
