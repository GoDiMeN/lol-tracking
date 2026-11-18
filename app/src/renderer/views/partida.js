// Partida rastreada: cabeçalho, seletor de jogador, o mapa no momento de cada abate com os 10 e
// "onde cada um estava", a vida ao longo da partida (mortes e saídas de rota), a lista de saídas de
// rota, o farm (CS) por minuto contra o adversário de rota, a linha do tempo da cadeia reduzida, os
// indicadores e a conferência do CS.

import { api, imagemItem, acompanha } from '../api.js';
import { duracao, dataCurta, posicao } from '../formato.js';
import { emPorcento, separa } from '../mapa.js';
import {
  relogio,
  numero,
  nomeZona,
  valorIndicador,
  itemCadeia,
  GRUPOS_CADEIA,
  grupoDoTipo,
  desfecho,
  fimDaSaida,
  resumoDano,
  superioridade,
  origemPosicao,
  tempoHumano,
} from '../texto.js';
import {
  el,
  botao,
  link,
  pilula,
  cartao,
  retrato,
  carregando,
  vazio,
  erroNaTela,
  partidaComNomes,
  participantePrincipal,
  toast,
} from '../ui.js';
import { MARGEM, escala, serieCS, csNoInstante, perdidasDoJogador, tetoRedondo } from '../grafico-tempo.js';
import { adversarioDeRota } from '../atributos.js';

const SVG = 'http://www.w3.org/2000/svg';
const svg = (tag, attrs = {}) => {
  const e = document.createElementNS(SVG, tag);
  for (const [k, v] of Object.entries(attrs)) e.setAttribute(k, String(v));
  return e;
};

// Os grupos da linha do tempo ligados de começo (os outros enchem a lista).
const GRUPOS_PADRAO = new Set(['lutas', 'rota', 'vida', 'base', 'itens']);
const gruposLigados = new Set(GRUPOS_PADRAO);
let soDoJogador = true;

export async function desenha(raiz, rota, ctx) {
  const gameId = rota.gameId;
  raiz.append(carregando('Carregando a partida…'));
  let info;
  let analise;
  let abates;
  let rastreadas;
  try {
    [info, { analise }, { abates }, { partidas: rastreadas }] = await Promise.all([
      partidaComNomes(gameId),
      api.analise(gameId),
      api.abates(gameId),
      api.rastreadas(),
    ]);
  } catch (err) {
    raiz.replaceChildren(
      el('h1', 'titulo-tela', 'Partida'),
      erroNaTela(err, err.status === 404 ? 'Essa partida não está guardada neste computador. Abra pela lista de Partidas rastreadas.' : undefined),
    );
    return;
  }
  const p = info.partida;
  const rastreados = rastreadas.find((x) => x.game_id === gameId)?.participantes || analise?.rastreados || [];
  let pid = rota.participante;
  if (!pid || !info.porId[pid]) {
    const dono = participantePrincipal(p, rastreados);
    pid = rastreados.includes(dono.participante_id) ? dono.participante_id : rastreados[0] || dono.participante_id;
    ctx.marca({ vista: 'partida', gameId, participante: pid, abate: rota.abate });
  }
  const jog = info.porId[pid];
  const ehRastreado = rastreados.includes(pid);

  // --- Cabeçalho ---
  const cab = el('div', 'linha cab-tela');
  cab.append(
    link('← Partidas rastreadas', '#partidas', 'voltar'),
    el('h1', 'titulo-tela inline', `${jog.campeao}${jog.posicao ? ` · ${posicao(jog.posicao)}` : ''} · ${jog.venceu ? 'vitória' : 'derrota'} · ${duracao(p.duracao_s)}`),
    pilula(`${rastreados.length} de 10 rastreados`, rastreados.length === 10 ? 'ok' : 'ac', 'quantos jogadores tiveram a partida assistida pelo app'),
  );
  if (analise?.riot) cab.lastChild.after(pilula('com dados da Riot', 'n', 'a linha do tempo da Riot trouxe o dano de cada abate'));
  const acoes = el('div', 'linha acoes-dir', [
    el('span', 'sub', `${dataCurta(p.criada_em)} · ${p.fila_nome}`),
    link('Mapa com play', `#replay/${gameId}`, 'btn pri'),
  ]);
  const refazer = botao('Refazer análise', '', async () => {
    refazer.disabled = true;
    refazer.textContent = 'Refazendo…';
    try {
      await api.reanalisa(gameId);
      toast('Análise refeita.', 'ok');
      ctx.navega({ vista: 'partida', gameId, participante: pid }, { substitui: true });
    } catch (err) {
      toast(err.status === 409 ? 'Falta a linha do tempo desta partida: baixe o replay de novo em Jogadores.' : err.message, 'erro');
      refazer.disabled = false;
      refazer.textContent = 'Refazer análise';
    }
  });
  acoes.append(refazer);
  cab.append(acoes);

  // --- Seletor de jogador ---
  const seletor = el('div', 'seletor');
  seletor.setAttribute('role', 'group');
  seletor.setAttribute('aria-label', 'Jogador');
  for (const time of [100, 200]) {
    const g = el('div', `seletor-time ${time === 100 ? 'azul' : 'vermelho'}`);
    for (const pt of p.participantes.filter((x) => x.time === time)) {
      const b = el('button', `chip-jogador${pt.participante_id === pid ? ' on' : ''}${rastreados.includes(pt.participante_id) ? ' rastreado' : ''}`);
      b.type = 'button';
      b.setAttribute('aria-pressed', String(pt.participante_id === pid));
      b.title = `${pt.campeao} · ${pt.nome}${rastreados.includes(pt.participante_id) ? ' (rastreado)' : ' (não rastreado: só os dados por minuto)'}`;
      b.append(retrato(pt, 26), el('span', '', pt.campeao));
      b.addEventListener('click', () => ctx.navega({ vista: 'partida', gameId, participante: pt.participante_id, abate: rota.abate }));
      g.append(b);
    }
    seletor.append(g);
  }

  // Selo da checagem de qualidade do dado (servidor: analise/qualidade.go). Não segura a tela:
  // chega depois, e a lista leva a cada instante no mapa com play.
  const selo = el('span');
  cab.children[2]?.after(selo);
  const listaQualidade = el('div');
  api
    .qualidade(gameId)
    .then((q) => {
      if (!q.passes.length) return;
      const nomeDe = Object.fromEntries(p.participantes.map((x) => [x.participante_id, x.campeao]));
      selo.replaceWith(
        q.total
          ? pilula(`${q.total} ${q.total === 1 ? 'ponto' : 'pontos'} para conferir`, 'at', 'a checagem do dado gravado achou instantes para conferir (lista abaixo)')
          : pilula('sem glitches no dado', 'ok', 'a checagem do dado gravado não achou salto, buraco, posição fora do mapa nem leitura estranha'),
      );
      if (!q.total) return;
      const det = el('details', 'qualidade cartao');
      det.append(el('summary', '', `Qualidade do dado: ${q.total} ${q.total === 1 ? 'ponto' : 'pontos'} para conferir`));
      const ul = el('ul', 'qualidade-lista');
      for (const ps of q.passes) {
        for (const pt of ps.pontos) {
          const quem = nomeDe[ps.participante_id] || `jogador ${ps.participante_id}`;
          const texto = `${quem} · ${pt.t_ms ? relogio(pt.t_ms) : 'passe inteiro'} · ${pt.texto}`;
          ul.append(el('li', '', [pt.t_ms ? link(texto, `#replay/${gameId}/${Math.max(0, Math.round(pt.t_ms / 1000) - 5)}`) : el('span', '', texto)]));
        }
      }
      det.append(ul);
      listaQualidade.replaceChildren(det);
    })
    .catch(() => selo.remove());

  // Repescagem (servidor: rastreio/repescagem_fila.go): trechos com o jogador vivo sem a posição
  // de verdade. O botão abre o replay, volta a cada trecho, trava a câmera, grava de novo e fecha
  // o replay; o progresso vem pelo SSE da execução. Some quando não há o que recuperar.
  const repescagem = el('span', 'repescagem');
  selo.after(repescagem);
  const plural = (n) => `${n} ${n === 1 ? 'trecho' : 'trechos'} sem amostra`;
  const segueRepescagem = (exId, total, botaoR) => {
    const estado = el('span', 'sub', 'abrindo o replay…');
    estado.setAttribute('role', 'status');
    repescagem.replaceChildren(estado);
    let n = 0;
    acompanha(
      exId,
      {
        replay_aberto: () => (estado.textContent = `recuperando ${plural(total)}…`),
        repescagem_trecho: (d) => {
          if (d.tentativa === 1) n++;
          estado.textContent = `trecho ${Math.min(n, total)} de ${total} (${relogio(d.de_ms)})${d.ok ? ' recuperado' : ': não fechou'}…`;
        },
        fechando_replay: () => (estado.textContent = 'fechando o replay…'),
        concluido: (d) => {
          const sobra = d.sobraram ? ` (${d.sobraram} não ${d.sobraram === 1 ? 'fechou' : 'fecharam'})` : '';
          toast(`Recuperados ${d.recuperados} de ${d.buracos} trechos sem amostra${sobra}.`, d.sobraram ? '' : 'ok');
          ctx.navega({ vista: 'partida', gameId, participante: pid }, { substitui: true });
        },
        cancelado: () => {
          toast('Repescagem cancelada; o replay foi fechado.');
          repescagem.replaceChildren(botaoR);
          botaoR.disabled = false;
        },
        erro: (d) => {
          toast(d.texto || 'a repescagem falhou', 'erro');
          repescagem.replaceChildren(botaoR);
          botaoR.disabled = false;
        },
      },
      { textoQueda: 'a conexão com o servidor caiu durante a repescagem' },
    );
  };
  api
    .planoRepescagem(gameId)
    .then((pl) => {
      if (pl.rodando && pl.execucao) {
        segueRepescagem(pl.execucao.id, pl.total, el('span'));
        return;
      }
      if (!pl.total) return;
      const dica =
        'abre o replay, volta a cada trecho em que a câmera perdeu o jogador, grava de novo e fecha o replay (o cliente do LoL precisa estar aberto e fora de partida)';
      const b = botao(
        `recuperar ${plural(pl.total)}`,
        '',
        async () => {
          b.disabled = true;
          try {
            const r = await api.repesca(gameId);
            segueRepescagem(r.execucao.id, r.total, b);
          } catch (err) {
            toast(err.message, 'erro');
            b.disabled = false;
          }
        },
        dica,
      );
      repescagem.replaceChildren(b);
    })
    .catch(() => repescagem.remove());

  // Visão dos times (servidor: rastreio/visao_fila.go): dois passes com a névoa do replay na visão
  // de cada time (F1, F2), lendo do minimapa quem do outro time estava à vista. Precisa de jogador
  // rastreado (a decisão usa a posição deles); o resultado aparece na caixa "visão" do mapa com play.
  const visaoEl = el('span', 'repescagem');
  repescagem.after(visaoEl);
  const segueVisao = (exId, botaoV) => {
    const estado = el('span', 'sub', 'abrindo o replay…');
    estado.setAttribute('role', 'status');
    visaoEl.replaceChildren(estado);
    const nomeTime = (t) => (t === 200 ? 'vermelho' : 'azul');
    acompanha(
      exId,
      {
        replay_aberto: () => (estado.textContent = 'replay aberto…'),
        visao_inicio: (d) => (estado.textContent = `visão do time ${nomeTime(d.time)}: conferindo a névoa…`),
        visao_amostra: (d) => (estado.textContent = `visão do time ${nomeTime(d.time)}: ${relogio(d.t_ms)} (${d.inimigos_a_vista} à vista)`),
        visao_fim: (d) => {
          if (d.estado === 'falhou') toast(`Visão do time ${nomeTime(d.time)}: ${d.erro}`, 'erro');
        },
        fechando_replay: () => (estado.textContent = 'fechando o replay…'),
        concluido: () => {
          toast('Visão dos times rastreada: ligue a caixa "visão" no mapa com play.', 'ok');
          ctx.navega({ vista: 'partida', gameId, participante: pid }, { substitui: true });
        },
        cancelado: () => {
          toast('Rastreio da visão cancelado; o replay foi fechado.');
          visaoEl.replaceChildren(botaoV);
          botaoV.disabled = false;
        },
        erro: (d) => {
          toast(d.texto || 'o rastreio da visão falhou', 'erro');
          visaoEl.replaceChildren(botaoV);
          botaoV.disabled = false;
        },
      },
      { textoQueda: 'a conexão com o servidor caiu durante o rastreio da visão' },
    );
  };
  api
    .visao(gameId)
    .then((v) => {
      if (v.rodando && v.execucao) {
        segueVisao(v.execucao.id, el('span'));
        return;
      }
      if (!v.disponivel) {
        visaoEl.remove();
        return;
      }
      const feito = Boolean(v.visao?.times?.length);
      const dica = `abre o replay e faz 2 passes a ${v.velocidade_padrao}×, um com a névoa na visão de cada time (teclas F1 e F2 do replay), lendo do minimapa quem do outro time estava à vista; leva ${tempoHumano(v.estimativa_s)} e fecha o replay (o cliente do LoL precisa estar aberto e fora de partida)`;
      const b = botao(
        feito ? 'rastrear a visão de novo' : `rastrear a visão dos times (${tempoHumano(v.estimativa_s)})`,
        '',
        async () => {
          b.disabled = true;
          try {
            const r = await api.rastreiaVisao(gameId);
            segueVisao(r.execucao.id, b);
          } catch (err) {
            toast(err.message, 'erro');
            b.disabled = false;
          }
        },
        dica,
      );
      const partesV = [b];
      if (feito) {
        const times = v.visao.times.map((x) => (x.time === 200 ? 'vermelho' : 'azul')).join(' e ');
        const avisos = v.visao.times.filter((x) => x.aviso).map((x) => `visão do time ${x.time === 200 ? 'vermelho' : 'azul'}: ${x.aviso}`);
        partesV.unshift(
          avisos.length
            ? pilula(`visão dos times: ${times} (conferir)`, 'at', avisos.join(' · '))
            : pilula(`visão dos times: ${times}`, 'ok', 'os passes de visão já rodaram: no mapa com play, a caixa "visão" mostra quem cada time via'),
        );
      }
      visaoEl.replaceChildren(...partesV);
    })
    .catch(() => visaoEl.remove());

  const partes = [cab, seletor, listaQualidade];
  if (analise?.avisos?.length) partes.push(el('ul', 'avisos', analise.avisos.map((a) => el('li', 'at', a))));
  if (!analise) {
    partes.push(
      vazio(
        'Esta partida ainda não foi analisada (a análise roda sozinha quando um jogador termina de ser rastreado).',
        botao('Analisar agora', 'pri', () => refazer.click()),
      ),
    );
    raiz.replaceChildren(...partes);
    return;
  }

  // --- Abates: mapa + onde cada um estava ---
  const secAbates = el('div', 'grade-abate');
  partes.push(secAbates);

  // --- Vida, saídas, cadeia, indicadores, CS: por jogador ---
  const secVida = el('div', 'graficos-tempo');
  const secSaidas = el('div');
  const secCadeia = el('div');
  const secInd = el('div');
  const secCS = el('div');
  partes.push(secVida, el('div', 'grade-2 saidas-cs', [secSaidas, secCS]), secCadeia, secInd);
  raiz.replaceChildren(...partes);

  desenhaAbates(secAbates, { gameId, info, abates, pid, rota, ctx, rastreados });

  if (!ehRastreado) {
    secVida.append(
      cartao(
        `${jog.campeao} · vida ao longo da partida`,
        null,
        el('div', 'estado-tela vazio', 'Este jogador não foi rastreado: sem vida, saídas de rota nem habilidades. Os indicadores abaixo vêm da linha do tempo (por minuto).'),
      ),
    );
  }

  const [ind, saidas, amostras, cadeia, cs, lt, ondas] = await Promise.allSettled([
    api.indicadores(gameId, pid),
    ehRastreado ? api.saidas(gameId, pid) : Promise.resolve({ saidas: [] }),
    ehRastreado ? api.amostras(gameId, pid, 0, p.duracao_s, 1) : Promise.resolve(null),
    ehRastreado ? api.cadeia(gameId, pid) : Promise.resolve(null),
    api.cs(gameId, pid),
    api.linhaDoTempo(gameId),
    api.ondas(gameId),
  ]);

  const nomes = info.nomes;
  // Vida e farm na MESMA escala de tempo (0 até o fim da partida) e com a guia do mouse junta.
  const t1 = p.duracao_s * 1000;
  const cursor = cursorDoTempo();
  const mortes = cadeia.status === 'fulfilled' && cadeia.value ? cadeia.value.itens.filter((i) => i.tipo === 'morte') : [];
  const listaSaidasRota = saidas.status === 'fulfilled' ? saidas.value.saidas : [];
  if (ehRastreado) {
    if (amostras.status === 'fulfilled') {
      secVida.append(graficoVida(jog, amostras.value.amostras, mortes, listaSaidasRota, gameId, t1, cursor));
    } else {
      secVida.append(cartao('Vida ao longo da partida', null, erroNaTela(amostras.reason)));
    }
  }
  if (lt.status === 'fulfilled') {
    const adv = adversarioDeRota(p.participantes, pid);
    const perdidas = ondas.status === 'fulfilled' ? perdidasDoJogador(ondas.value, pid) : [];
    secVida.append(
      graficoFarm(jog, adv, serieCS(lt.value, pid), adv ? serieCS(lt.value, adv.participante_id) : [], mortes, listaSaidasRota, perdidas, gameId, t1, cursor),
    );
  } else {
    secVida.append(cartao('Farm (CS) ao longo da partida', null, erroNaTela(lt.reason)));
  }
  if (ehRastreado) {
    secSaidas.append(
      saidas.status === 'fulfilled' ? listaSaidas(saidas.value.saidas, gameId, jog) : cartao('Saídas de rota', null, erroNaTela(saidas.reason)),
    );
    secCadeia.append(cadeia.status === 'fulfilled' ? linhaDoTempo(cadeia.value.itens, nomes, gameId) : cartao('Linha do tempo', null, erroNaTela(cadeia.reason)));
  } else {
    secSaidas.append(cartao('Saídas de rota', null, el('div', 'estado-tela vazio', 'Só para jogadores rastreados.')));
  }
  secInd.append(ind.status === 'fulfilled' ? indicadores(ind.value.indicadores, gameId) : cartao('Indicadores', null, erroNaTela(ind.reason)));
  secCS.append(cs.status === 'fulfilled' ? conferenciaCS(cs.value.jogadores.find((j) => j.participante_id === pid)) : cartao('CS conferido', null, erroNaTela(cs.reason)));
}

// --- Abates ------------------------------------------------------------------------------------

function desenhaAbates(sec, { gameId, info, abates, pid, rota, ctx, rastreados }) {
  if (!abates.length) {
    sec.replaceChildren(cartao('Mapa no momento dos abates', null, el('div', 'estado-tela vazio', 'Nenhum abate nesta partida (ou a análise não achou a linha do tempo).')));
    return;
  }
  const doJogador = (a) => a.vitima === pid || a.matador === pid || (a.assistencias || []).includes(pid);
  const lista = () => (soDoJogador ? abates.filter(doJogador) : abates);
  let atual = abates.find((a) => a.n === rota.abate) || lista()[0] || abates[0];

  const mapaCartao = el('section', 'cartao');
  const painel = el('section', 'cartao');
  sec.replaceChildren(mapaCartao, painel);

  function escolhe(a) {
    atual = a;
    ctx.marca({ vista: 'partida', gameId, participante: pid, abate: a.n });
    desenha();
  }

  function desenha() {
    const l = lista();
    const i = l.findIndex((a) => a.n === atual.n);
    const ant = botao('←', 'icone', () => escolhe(l[i - 1]), 'abate anterior');
    ant.disabled = i <= 0;
    ant.setAttribute('aria-label', 'Abate anterior');
    const prox = botao('→', 'icone', () => escolhe(l[i + 1]), 'próximo abate');
    prox.disabled = i < 0 || i >= l.length - 1;
    prox.setAttribute('aria-label', 'Próximo abate');
    const sel = el('select', 'campo seletor-abate');
    sel.setAttribute('aria-label', 'Escolher o abate');
    for (const a of l) {
      const o = el('option', '', `${relogio(a.t_ms)} · ${info.nomes[a.matador] || '—'} abateu ${info.nomes[a.vitima] || '—'}`);
      o.value = String(a.n);
      o.selected = a.n === atual.n;
      sel.append(o);
    }
    if (i < 0) {
      const o = el('option', '', `${relogio(atual.t_ms)} · ${info.nomes[atual.matador] || '—'} abateu ${info.nomes[atual.vitima] || '—'}`);
      o.value = String(atual.n);
      o.selected = true;
      sel.prepend(o);
    }
    sel.addEventListener('change', () => escolhe(abates.find((a) => a.n === Number(sel.value))));
    const filtro = el('label', 'check');
    const cb = el('input');
    cb.type = 'checkbox';
    cb.checked = soDoJogador;
    cb.addEventListener('change', () => {
      soDoJogador = cb.checked;
      const nl = lista();
      if (nl.length && !nl.some((a) => a.n === atual.n)) atual = nl[0];
      desenha();
    });
    filtro.append(cb, el('span', '', `só os de ${info.porId[pid].campeao}`));

    const cab = el('div', 'cab', [
      el('h2', '', `Mapa no momento ${relogio(atual.t_ms)}`),
      el('span', 'dir', `abate ${atual.n} de ${abates.length}`),
    ]);
    const nav = el('div', 'linha nav-abate', [ant, sel, prox, filtro]);
    mapaCartao.replaceChildren(cab, nav, mapaDoAbate(atual, info), legendaMapa(), link('Ver no mapa com play', `#replay/${gameId}/${Math.max(0, Math.round(atual.t_ms / 1000) - 8)}`, 'btn mini'));
    painel.replaceChildren(...painelAbate(atual, info, rastreados));
  }
  desenha();
}

function mapaDoAbate(a, info) {
  const m = el('div', 'mapa-quadro');
  const img = el('img');
  img.src = 'assets/map11.png';
  img.alt = '';
  m.append(img);
  m.setAttribute('role', 'img');
  const vit = info.porId[a.vitima];
  const mat = info.porId[a.matador];
  m.setAttribute('aria-label', `Mapa com os dez jogadores quando ${mat?.campeao || 'alguém'} abateu ${vit?.campeao || 'alguém'}, em ${nomeZona(a.zona)}`);
  // A vítima por último: fica por cima.
  const jogs = [...a.jogadores].sort((x, y) => (x.papel === 'vitima') - (y.papel === 'vitima') || (x.papel === 'matador') - (y.papel === 'matador'));
  // Quem está no mesmo lugar (a luta) abre um pouco para não sumir um debaixo do outro.
  const comPos = jogs.filter((j) => j.x !== null && j.y !== null && j.x !== undefined);
  const lugar = separa(comPos.map((j) => {
    const p = emPorcento(j.x, j.y);
    return { x: p.left, y: p.top };
  }), 6, { maxDesvio: 5 });
  for (const [k, j] of comPos.entries()) {
    const pt = info.porId[j.participante_id];
    const { x: left, y: top } = lugar[k];
    const marca = el('span', `marca ${j.aproximada ? 'aproximada' : 'exata'} ${j.papel || ''} ${j.vivo === false && j.papel !== 'vitima' ? 'morto' : ''}`);
    marca.style.left = `${left}%`;
    marca.style.top = `${top}%`;
    marca.append(retrato(pt, 26));
    if (j.papel === 'vitima') marca.append(el('i', 'x', '✕'));
    marca.title = `${pt.campeao} · ${pt.nome}${j.papel ? ` · ${papel(j.papel)}` : ''} · ${origemPosicao(j.origem)}`;
    m.append(marca);
  }
  return m;
}

function legendaMapa() {
  return el('div', 'chave', [
    el('span', '', [el('i', 'k-exata'), 'posição rastreada']),
    el('span', '', [el('i', 'k-aprox'), 'aproximada (por minuto)']),
    el('span', '', [el('i', 'k-vitima', '✕'), 'quem morreu']),
  ]);
}

const papel = (p) => ({ vitima: 'morreu', matador: 'matou', assistencia: 'ajudou' })[p] || '';

function painelAbate(a, info, rastreados) {
  const cab = el('div', 'cab', [el('h2', '', 'Onde cada um estava'), el('span', 'dir', relogio(a.t_ms))]);
  const vit = info.porId[a.vitima];
  const resumo = el('div', 'resumo-abate');
  const linha1 = `${info.nomes[a.matador] || 'alguém'} abateu ${vit?.campeao || 'alguém'} em ${nomeZona(a.zona)}`;
  resumo.append(el('b', '', linha1), el('span', 'sub', superioridade(a)));
  if (a.aliado_mais_proximo) resumo.append(el('span', 'sub', `aliado mais perto da vítima a ${numero(Math.round(a.aliado_mais_proximo))} unidades`));
  const vitJ = a.jogadores.find((j) => j.papel === 'vitima');
  const matJ = a.jogadores.find((j) => j.papel === 'matador');
  if (vitJ && matJ && !matJ.aproximada && matJ.dist_vitima > 900) {
    // Dano à distância é normal (habilidade de longo alcance, dano atrasado, salto que volta).
    resumo.append(el('span', 'sub', `${info.nomes[a.matador]} estava a ${numero(Math.round(matJ.dist_vitima))} da vítima: dano à distância${a.riot ? ' (veja abaixo de onde veio)' : ''}.`));
  }

  const kv = el('div', 'onde');
  const ordem = [...a.jogadores].sort((x, y) => {
    const peso = (j) => (j.papel === 'vitima' ? 0 : j.papel === 'matador' ? 1 : j.papel === 'assistencia' ? 2 : 3);
    return peso(x) - peso(y) || x.time - y.time || x.participante_id - y.participante_id;
  });
  for (const j of ordem) {
    const pt = info.porId[j.participante_id];
    const nome = el('span', `quem ${j.time === 200 ? 'tm-v' : 'tm-a'}`, [retrato(pt, 20), el('span', '', `${pt.campeao}${j.papel ? ` · ${papel(j.papel)}` : ''}`)]);
    const partes = [];
    if (j.vivo === false && j.papel !== 'vitima') partes.push('morto');
    else {
      if (j.zona) partes.push(nomeZona(j.zona));
      if (j.vida !== null && j.vida !== undefined && j.vida_max) partes.push(`${Math.round((j.vida / j.vida_max) * 100)}% de vida`);
      else if (j.vida_pct !== null && j.vida_pct !== undefined) partes.push(`${Math.round(j.vida_pct)}% de vida`);
    }
    if (j.rastreado) {
      if (j.r_aprendido === false) partes.push('sem R');
      else if (j.r_disponivel === true) partes.push('R pronto');
      else if (j.r_disponivel === false) partes.push(`R em ${Math.round(j.cd?.r ?? 0)} s`);
      if (j.passiva_disponivel === false) partes.push(`passiva em ${Math.round(j.cd?.p ?? 0)} s`);
      if (j.flash_disponivel === true) partes.push('Flash pronto');
      else if (j.flash_disponivel === false) partes.push('sem Flash');
      if (j.fora_da_rota_ms) partes.push(`fora da rota há ${Math.round(j.fora_da_rota_ms / 1000)} s`);
    }
    if (j.aproximada) partes.push('posição aproximada');
    const valor = el('b', j.aproximada ? 'aprox' : '', partes.join(' · ') || '—');
    kv.append(nome, valor);
  }
  const blocos = [cab, resumo, kv];
  if (a.riot) {
    const dano = resumoDano(a.riot.recebido, info.nomes);
    if (dano.length) {
      const l = el('div', 'dano');
      l.append(el('h3', '', `Quem bateu em ${vit?.campeao || 'quem morreu'} (Riot)`));
      for (const d of dano) {
        const pt = info.porId[d.participante_id];
        l.append(
          el('div', 'dano-linha', [
            el('span', `quem ${pt?.time === 200 ? 'tm-v' : pt ? 'tm-a' : ''}`, d.nome),
            el('span', 'num', numero(d.total)),
            el('span', 'sub', d.partes),
          ]),
        );
      }
      blocos.push(l);
    }
  }
  if (!rastreados.length) blocos.push(el('p', 'sub', 'Ninguém foi rastreado: as posições são as de minuto em minuto.'));
  return blocos;
}

// --- Vida e farm ao longo da partida ------------------------------------------------------------

// cursorDoTempo: o instante sob o mouse, compartilhado pelos gráficos de vida e de farm (mesma
// escala de tempo): passar o mouse num mostra a guia nos dois.
function cursorDoTempo() {
  const ouvintes = new Set();
  return {
    ouve: (f) => ouvintes.add(f),
    move: (t) => ouvintes.forEach((f) => f(t)),
  };
}

// graficoNoTamanho: um SVG desenhado no tamanho real da caixa (1 unidade = 1 px) e redesenhado
// quando ela muda. `desenha(svg, largura, altura)` monta o conteúdo; devolve o que o gráfico
// precisa guardar (a escala). A altura vem do CSS (cresce com a janela, com teto).
function graficoNoTamanho(rotulo, desenha) {
  const caixa = el('div', 'grafico-caixa');
  const g = svg('svg', { role: 'img', class: 'grafico' });
  g.setAttribute('aria-label', rotulo);
  caixa.append(g);
  const estado = { largura: 0, altura: 0, escala: null };
  const redesenha = () => {
    const largura = Math.round(caixa.clientWidth);
    const altura = Math.round(caixa.clientHeight);
    if (largura < 50 || altura < 50 || (largura === estado.largura && altura === estado.altura)) return;
    estado.largura = largura;
    estado.altura = altura;
    g.setAttribute('width', String(largura));
    g.setAttribute('height', String(altura));
    g.setAttribute('viewBox', `0 0 ${largura} ${altura}`);
    g.replaceChildren();
    estado.escala = desenha(g, largura, altura);
  };
  new ResizeObserver(redesenha).observe(caixa);
  return { caixa, g, estado, redesenha };
}

// eixoDoTempo: grade vertical leve e os rótulos (0:00, 5:00...) embaixo, em px de verdade.
function eixoDoTempo(g, esc, t1, altura) {
  const base = altura - MARGEM.base;
  for (let t = 0; t <= t1; t += esc.passo) {
    const px = esc.x(t);
    g.append(svg('line', { x1: px, x2: px, y1: MARGEM.topo, y2: base, class: 'grade-tempo' }));
    const tx = svg('text', { x: px, y: altura - 6, class: 'rotulo-eixo', 'text-anchor': t === 0 ? 'start' : 'middle' });
    tx.textContent = relogio(t);
    g.append(tx);
  }
  g.append(svg('line', { x1: MARGEM.esq, x2: esc.x(t1), y1: base, y2: base, class: 'eixo' }));
}

// marcasComuns: as faixas de saída de rota (atrás) e as mortes (no eixo), iguais nos dois gráficos.
function faixasDeSaida(g, esc, saidas, altura) {
  const base = altura - MARGEM.base;
  for (const s of saidas.filter((s) => s.tipo === 'saiu_da_rota')) {
    const fim = s.dados?.fim_ms || s.t_ms;
    const r = svg('rect', { x: esc.x(s.t_ms), y: MARGEM.topo, width: Math.max(2, esc.x(fim) - esc.x(s.t_ms)), height: base - MARGEM.topo, class: 'faixa-saida' });
    const tt = svg('title');
    tt.textContent = `saiu da rota às ${relogio(s.t_ms)} por ${Math.round((fim - s.t_ms) / 1000)} s · ${desfecho(s.dados?.desfecho)}`;
    r.append(tt);
    g.append(r);
  }
}
function marcasDeMorte(g, esc, mortes, altura) {
  for (const m of mortes) {
    const c = svg('circle', { cx: esc.x(m.t_ms), cy: altura - MARGEM.base, r: 5, class: 'morte' });
    const tt = svg('title');
    tt.textContent = `morreu às ${relogio(m.t_ms)}`;
    c.append(tt);
    g.append(c);
  }
}

// interativo: guia vertical sincronizada, texto do instante e clique → mapa com play.
function interativo(gr, { t1, cursor, gameId, texto, valor }) {
  const guia = () => gr.g.querySelector('.guia');
  gr.g.style.cursor = 'pointer';
  const tDoMouse = (ev) => {
    const r = gr.g.getBoundingClientRect();
    return gr.estado.escala ? Math.max(0, Math.min(t1, gr.estado.escala.tDoX(ev.clientX - r.left))) : null;
  };
  gr.g.addEventListener('mousemove', (ev) => cursor.move(tDoMouse(ev)));
  gr.g.addEventListener('mouseleave', () => cursor.move(null));
  gr.g.addEventListener('click', (ev) => {
    const t = tDoMouse(ev);
    if (t !== null) location.hash = `#replay/${gameId}/${Math.max(0, Math.round(t / 1000))}`;
  });
  cursor.ouve((t) => {
    let l = guia();
    if (t === null || !gr.estado.escala) {
      if (l) l.remove();
      valor.textContent = texto(null);
      return;
    }
    if (!l) {
      l = svg('line', { class: 'guia', y1: MARGEM.topo, y2: gr.estado.altura - MARGEM.base });
      gr.g.append(l);
    }
    const px = gr.estado.escala.x(t);
    l.setAttribute('x1', px);
    l.setAttribute('x2', px);
    valor.textContent = texto(t);
  });
}

function graficoVida(jog, amostras, mortes, saidas, gameId, t1, cursor) {
  const comVida = amostras.filter((a) => a.vida !== null && a.vida_max);
  const titulo = `${jog.campeao} · vida ao longo da partida`;
  if (!amostras.length) return cartao(titulo, null, el('div', 'estado-tela vazio', 'Sem leituras gravadas para este jogador.'));
  const gr = graficoNoTamanho(`Vida de ${jog.campeao} ao longo da partida, com ${mortes.length} mortes e ${saidas.length} saídas de rota`, (g, W, H) => {
    const esc = escala(t1, W);
    const base = H - MARGEM.base;
    const y = (pct) => base - (pct / 100) * (base - MARGEM.topo);
    faixasDeSaida(g, esc, saidas, H);
    for (const pct of [50, 100]) {
      g.append(svg('line', { x1: MARGEM.esq, x2: W - MARGEM.dir, y1: y(pct), y2: y(pct), class: 'grade-linha' }));
      const r = svg('text', { x: MARGEM.esq - 6, y: y(pct) + 4, class: 'rotulo-eixo', 'text-anchor': 'end' });
      r.textContent = `${pct}%`;
      g.append(r);
    }
    eixoDoTempo(g, esc, t1, H);
    // A linha quebra onde não há leitura (morto, painel não lido) por mais de 3 s.
    let d = '';
    let ant = null;
    for (const a of comVida) {
      const px = esc.x(a.t_ms).toFixed(1);
      const py = y((a.vida / a.vida_max) * 100).toFixed(1);
      d += !ant || a.t_ms - ant.t_ms > 3000 ? `M${px} ${py}` : `L${px} ${py}`;
      ant = a;
    }
    g.append(svg('path', { d, class: 'linha-vida' }));
    marcasDeMorte(g, esc, mortes, H);
    return esc;
  });
  const valor = el('span', 'sub num valor-grafico');
  const vidaEm = (t) => {
    let melhor = null;
    for (const a of comVida) {
      if (Math.abs(a.t_ms - t) <= 1500 && (!melhor || Math.abs(a.t_ms - t) < Math.abs(melhor.t_ms - t))) melhor = a;
      if (a.t_ms > t + 1500) break;
    }
    return melhor;
  };
  interativo(gr, {
    t1,
    cursor,
    gameId,
    valor,
    texto: (t) => {
      if (t === null) return 'passe o mouse para ver o instante; clique para abrir no mapa com play';
      const a = vidaEm(t);
      return `${relogio(t)} · ${a ? `vida ${Math.round((a.vida / a.vida_max) * 100)}% (${numero(Math.round(a.vida), 0)} / ${numero(a.vida_max, 0)})` : 'vida não lida'}`;
    },
  });
  valor.textContent = 'passe o mouse para ver o instante; clique para abrir no mapa com play';
  const lado = el('div', 'linha legenda-grafico', [
    el('span', '', [el('i', 'k-linha'), 'vida (painel do jogo)']),
    el('span', '', [el('i', 'k-faixa'), 'fora da rota']),
    el('span', '', [el('i', 'k-morte'), 'morte']),
    comVida.length < amostras.length * 0.5 ? el('span', 'sub', 'a vida só aparece onde a tela foi lida') : null,
    valor,
  ]);
  return cartao(titulo, `0:00–${relogio(t1)}`, [gr.caixa, lado]);
}

// graficoFarm: o CS do jogador (e do adversário direto de rota) de minuto em minuto, pela linha do
// tempo do cliente (exata; o CS do jogo anda de 10 em 10 e fica só na conferência), na mesma escala
// de tempo do gráfico de vida, com as mesmas marcas (saídas, mortes) e as ondas perdidas.
function graficoFarm(jog, adv, serieEu, serieAdv, mortes, saidas, perdidas, gameId, t1, cursor) {
  const titulo = `${jog.campeao} · farm (CS) ao longo da partida`;
  if (!serieEu.length) return cartao(titulo, null, el('div', 'estado-tela vazio', 'Sem a linha do tempo desta partida: sem o CS por minuto.'));
  const max = tetoRedondo(Math.max(...serieEu.map((s) => s.cs), ...serieAdv.map((s) => s.cs)));
  const gr = graficoNoTamanho(`CS de ${jog.campeao}${adv ? ` e de ${adv.campeao}` : ''} ao longo da partida`, (g, W, H) => {
    const esc = escala(t1, W);
    const base = H - MARGEM.base;
    const y = (cs) => base - (cs / max) * (base - MARGEM.topo);
    faixasDeSaida(g, esc, saidas, H);
    for (const v of [max / 2, max]) {
      g.append(svg('line', { x1: MARGEM.esq, x2: W - MARGEM.dir, y1: y(v), y2: y(v), class: 'grade-linha' }));
      const r = svg('text', { x: MARGEM.esq - 6, y: y(v) + 4, class: 'rotulo-eixo', 'text-anchor': 'end' });
      r.textContent = String(Math.round(v));
      g.append(r);
    }
    eixoDoTempo(g, esc, t1, H);
    const caminho = (s) => s.map((p, i) => `${i ? 'L' : 'M'}${esc.x(p.t_ms).toFixed(1)} ${y(p.cs).toFixed(1)}`).join('');
    if (serieAdv.length) g.append(svg('path', { d: caminho(serieAdv), class: 'linha-cs adv' }));
    g.append(svg('path', { d: caminho(serieEu), class: 'linha-cs' }));
    for (const o of perdidas) {
      const px = esc.x(o.t_ms);
      const m = svg('path', { d: `M${px - 5} ${MARGEM.topo} L${px + 5} ${MARGEM.topo} L${px} ${MARGEM.topo + 8} Z`, class: 'onda-perdida' });
      const tt = svg('title');
      tt.textContent = `onda ${o.onda}${o.canhao ? ' (com canhão)' : ''} perdida às ${relogio(o.t_ms)}: ${{ morto: 'morto', na_base: 'na base', longe: 'longe da onda' }[o.motivo] || o.motivo}${o.zona ? ` (${nomeZona(o.zona)})` : ''}`;
      m.append(tt);
      g.append(m);
    }
    marcasDeMorte(g, esc, mortes, H);
    return esc;
  });
  const valor = el('span', 'sub num valor-grafico');
  const texto = (t) => {
    if (t === null) return 'passe o mouse para ver o CS e o CS por minuto';
    const eu = csNoInstante(serieEu, t);
    const ele = adv ? csNoInstante(serieAdv, t) : null;
    const fmt = (c) => `${Math.round(c.cs)} CS${c.por_min !== null ? ` (${numero(c.por_min)}/min)` : ''}`;
    return `${relogio(t)} · ${jog.campeao} ${fmt(eu)}${ele ? ` · ${adv.campeao} ${fmt(ele)}` : ''}`;
  };
  interativo(gr, { t1, cursor, gameId, valor, texto });
  valor.textContent = texto(null);
  const ultimo = serieEu.at(-1);
  const lado = el('div', 'linha legenda-grafico', [
    el('span', '', [el('i', 'k-cs'), jog.campeao]),
    adv && serieAdv.length ? el('span', '', [el('i', 'k-cs adv'), `${adv.campeao} (adversário de rota)`]) : null,
    el('span', '', [el('i', 'k-faixa'), 'fora da rota']),
    el('span', '', [el('i', 'k-morte'), 'morte']),
    el('span', '', [el('i', 'k-onda'), `onda perdida${perdidas.length ? ` (${perdidas.length})` : ' (nenhuma estimada)'}`]),
    valor,
  ]);
  return cartao(titulo, `${ultimo.cs} CS · ${numero(ultimo.cs / Math.max(1, ultimo.t_ms / 60000))}/min`, [gr.caixa, lado]);
}

// --- Saídas de rota -----------------------------------------------------------------------------

function listaSaidas(saidas, gameId, jog) {
  const so = saidas.filter((s) => s.tipo === 'saiu_da_rota' || s.tipo === 'entrou_em_rota');
  if (!so.length) {
    return cartao('Saídas de rota', null, el('div', 'estado-tela vazio', `${jog.campeao} não saiu da rota no trecho rastreado (ou é o caçador e não visitou rotas).`));
  }
  const tab = el('table', 'tabela');
  tab.append(
    el('thead', '', [el('tr', '', ['Quando', 'Por', 'Para onde', 'Vida', 'Como terminou', ''].map((h) => el('th', h === 'Para onde' ? 'larga' : '', h)))]),
  );
  const tb = el('tbody');
  for (const s of so) {
    const d = s.dados || {};
    const visita = s.tipo === 'entrou_em_rota';
    const tr = el('tr', d.conta === false ? 'apagada' : '');
    tr.append(
      el('td', 'num', relogio(s.t_ms)),
      el('td', 'num', `${Math.round((d.duracao_ms || 0) / 1000)} s`),
      el('td', '', visita ? `visitou a ${nomeZona(d.rota)}` : nomeZona(d.para) + (d.zonas?.length > 1 ? ` e mais ${d.zonas.length - 1}` : '')),
      el('td', 'num', d.vida_pct !== null && d.vida_pct !== undefined ? `${Math.round(d.vida_pct)}%` : '—'),
      el('td', d.desfecho === 'morte' ? 'tx-cr' : '', `${desfecho(d.desfecho)}${d.fim_motivo && d.fim_motivo !== 'voltou' && !(d.fim_motivo === 'morreu' && d.desfecho === 'morte') ? ` · ${fimDaSaida(d.fim_motivo)}` : ''}`),
      el('td', '', [link('ver', `#replay/${gameId}/${Math.max(0, Math.round(s.t_ms / 1000) - 3)}`, 'btn mini')]),
    );
    if (d.conta === false) tr.title = 'foi andando para a base sem nada acontecer: não conta como saída para jogar';
    tb.append(tr);
  }
  tab.append(tb);
  const n = so.filter((s) => s.dados?.conta !== false).length;
  return cartao('Saídas de rota', `${n} que contam`, el('div', 'rolar', [tab]));
}

// --- Linha do tempo da cadeia -------------------------------------------------------------------

function linhaDoTempo(itens, nomes, gameId) {
  const corpo = el('div');
  const filtros = el('div', 'linha filtros');
  filtros.setAttribute('role', 'group');
  filtros.setAttribute('aria-label', 'O que mostrar na linha do tempo');
  const contagem = {};
  for (const it of itens) {
    const g = grupoDoTipo(it.tipo);
    contagem[g] = (contagem[g] || 0) + 1;
  }
  for (const g of GRUPOS_CADEIA) {
    if (!contagem[g.id]) continue;
    const b = el('button', `filtro${gruposLigados.has(g.id) ? ' on' : ''}`, `${g.rotulo} ${contagem[g.id]}`);
    b.type = 'button';
    b.setAttribute('aria-pressed', String(gruposLigados.has(g.id)));
    b.addEventListener('click', () => {
      if (gruposLigados.has(g.id)) gruposLigados.delete(g.id);
      else gruposLigados.add(g.id);
      b.classList.toggle('on');
      b.setAttribute('aria-pressed', String(gruposLigados.has(g.id)));
      lista();
    });
    filtros.append(b);
  }
  const ol = el('ol', 'cadeia');
  function lista() {
    const vis = itens.filter((it) => gruposLigados.has(grupoDoTipo(it.tipo)));
    if (!vis.length) {
      ol.replaceChildren(el('li', 'sub', 'Nada com esses filtros.'));
      return;
    }
    ol.replaceChildren(
      ...vis.slice(0, 600).map((it) => {
        const li = el('li', `item-cadeia g-${grupoDoTipo(it.tipo)} t-${it.tipo}`);
        const t = link(relogio(it.t_ms), `#replay/${gameId}/${Math.max(0, Math.round(it.t_ms / 1000) - 2)}`, 'num tempo');
        t.title = 'ver no mapa com play';
        li.append(t, el('span', 'marca-tipo'), el('span', '', itemCadeia(it, nomes)));
        if ((it.tipo === 'compra' || it.tipo === 'venda') && it.dados?.item) {
          const img = el('img', 'item-icone');
          img.src = imagemItem(it.dados.item);
          img.alt = `item ${it.dados.item}`;
          img.title = `item ${it.dados.item}`;
          img.addEventListener('error', () => img.replaceWith(el('span', 'sub', `item ${it.dados.item}`)));
          li.append(img);
        }
        return li;
      }),
    );
    if (vis.length > 600) ol.append(el('li', 'sub', `e mais ${vis.length - 600}…`));
  }
  lista();
  corpo.append(filtros, ol);
  return cartao('Linha do tempo', 'o que o rastreio guardou, em ordem', corpo);
}

// --- Indicadores --------------------------------------------------------------------------------

function indicadores(lista, gameId) {
  const comValor = lista.filter((i) => i.valor !== null);
  if (!comValor.length) return cartao('Indicadores', null, el('div', 'estado-tela vazio', 'Sem indicadores para este jogador.'));
  const grupos = [
    { rotulo: 'Rota e mortes', f: (i) => /saidas|mortes|visitas|cs_perdido|abates|assist|participacao|dist_aliado|recalls|habilidades|tempo_/.test(i.indicador) && !i.indicador.startsWith('pct_') },
    { rotulo: 'Ouro, CS e XP', f: (i) => /^(cs_|ouro_|xp_|dif_|dano_|wards)/.test(i.indicador) },
    { rotulo: 'Onde passou o tempo', f: (i) => i.indicador.startsWith('pct_') && !i.indicador.startsWith('pct_atv_') },
    { rotulo: 'O que fazia', f: (i) => i.indicador.startsWith('pct_atv_') },
  ];
  const usados = new Set();
  const corpo = el('div', 'grade-ind');
  for (const g of grupos) {
    const itens = comValor.filter((i) => !usados.has(i.indicador) && g.f(i));
    if (!itens.length) continue;
    for (const i of itens) usados.add(i.indicador);
    const bloco = el('div', 'bloco-ind', [el('h3', '', g.rotulo)]);
    const dl = el('div', 'kv');
    for (const i of itens) {
      const rot = el('span', '', i.rotulo);
      if (i.aproximado) rot.append(el('small', 'aprox-marca', ' aprox.'));
      const v = el('b', '', valorIndicador(i.valor, i.unidade));
      dl.append(rot, v);
      const mom = i.detalhe?.momentos;
      if (mom?.length) {
        const m = el(
          'span',
          'momentos span2',
          mom.slice(0, 6).map((x) => link(x.texto, `#replay/${gameId}/${Math.max(0, Math.round(x.t_ms / 1000) - 5)}`)),
        );
        dl.append(m);
      }
    }
    bloco.append(dl);
    corpo.append(bloco);
  }
  return cartao('Indicadores', '“aprox.” = rastreio parcial ou posição aproximada', corpo);
}

// --- CS -----------------------------------------------------------------------------------------

function conferenciaCS(j) {
  if (!j || !j.minutos.length) {
    return cartao('CS conferido', null, el('div', 'estado-tela vazio', 'Sem estado gravado para conferir o CS deste jogador.'));
  }
  const tab = el('table', 'tabela compacta');
  tab.append(el('thead', '', [el('tr', '', ['Min', 'Cliente', 'Jogo', 'Dif.'].map((h) => el('th', '', h)))]));
  const tb = el('tbody');
  for (const m of j.minutos) {
    tb.append(
      el('tr', m.divergente ? 'divergente' : '', [
        el('td', 'num', String(m.minuto)),
        el('td', 'num', String(m.cs_lcu)),
        el('td', 'num', m.cs_jogo === null ? '—' : String(m.cs_jogo)),
        el('td', 'num', m.diferenca === null ? '—' : String(m.diferenca)),
      ]),
    );
  }
  tab.append(tb);
  const dir = j.divergencias ? `${j.divergencias} minuto${j.divergencias > 1 ? 's' : ''} fora` : 'tudo confere';
  return cartao('CS conferido', dir, [
    el('p', 'sub pad', 'O CS do jogo anda de 10 em 10; diferença de até 10 (mais 3 de folga) é normal.'),
    el('div', 'rolar curto', [tab]),
  ]);
}
