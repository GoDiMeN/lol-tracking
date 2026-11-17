// Fila de rastreio: o rastreio em andamento (ou o escolhido) ao vivo por SSE — progresso geral, os
// passes por jogador, a última leitura da tela, avisos e a conferência com a Riot — com pausar,
// retomar e cancelar. Sem id na rota: o que está rodando, senão o mais novo.

import { api, acompanha } from '../api.js';
import { dataCurta, duracao, plural } from '../formato.js';
import { zonaNaGrade } from '../mapa.js';
import {
  relogio,
  numero,
  tempoHumano,
  nomeZona,
  recarga,
  pilulaPasse,
  progressoPasse,
  ESTADOS_RASTREIO,
  podeRastrear,
} from '../texto.js';
import {
  el,
  botao,
  link,
  pilula,
  cartao,
  retrato,
  barra,
  carregando,
  vazio,
  erroNaTela,
  partidaComNomes,
  participantePrincipal,
  confirmaNoBotao,
  toast,
} from '../ui.js';

const TECLAS = ['1', '2', '3', '4', '5', 'Q', 'W', 'E', 'R', 'T'];

let grade = null;

export async function desenha(raiz, rota, ctx) {
  raiz.append(el('h1', 'titulo-tela', 'Fila de rastreio'), carregando('Carregando a fila…'));
  let rastreios;
  try {
    ({ rastreios } = await api.rastreios(50));
  } catch (err) {
    raiz.replaceChildren(el('h1', 'titulo-tela', 'Fila de rastreio'), erroNaTela(err));
    return;
  }
  if (!grade) api.zonas().then((z) => (grade = z.grade)).catch(() => {});

  let id = rota.id;
  if (!id) {
    const r = rastreios.find((x) => x.estado === 'rodando') || rastreios[0];
    id = r?.id;
    if (id) ctx.marca({ vista: 'rastreio', id });
  }
  if (!id) {
    raiz.replaceChildren(
      el('h1', 'titulo-tela', 'Fila de rastreio'),
      vazio('Nenhum rastreio ainda. Em Jogadores, escolha uma partida com o replay baixado e clique em Rastrear.', link('Ir para Jogadores', '#jogadores', 'btn pri')),
    );
    return;
  }
  return montaDetalhe(raiz, id, rastreios, ctx);
}

async function montaDetalhe(raiz, id, rastreios, ctx) {
  const cab = el('div', 'linha cab-tela');
  const geral = el('div', 'linha geral');
  const passesEl = el('div', 'cartao lista-passes');
  const leitura = el('div', 'kv');
  const leituraDir = el('span', 'dir', '');
  const cartaoLeitura = cartao('Última leitura', leituraDir, leitura);
  const avisosEl = el('ul', 'avisos');
  const cartaoAvisos = cartao('Avisos', null, avisosEl);
  cartaoAvisos.hidden = true;
  const fixo = el(
    'p',
    'nota',
    'O jogo precisa ficar visível na tela enquanto o rastreio roda. Mexer no mouse não atrapalha; cobrir a janela do jogo, minimizar ou trocar a resolução, sim. Ao terminar, pausar ou cancelar, o replay fecha sozinho.',
  );
  const outros = el('div', 'cartao');

  raiz.replaceChildren(
    el('h1', 'titulo-tela', 'Fila de rastreio'),
    cab,
    geral,
    passesEl,
    el('div', 'grade-2', [cartaoLeitura, cartaoAvisos]),
    fixo,
    cartao('Rastreios', null, outros),
  );

  let r = null;
  let passes = [];
  let info = null; // partidaComNomes
  let parar = null; // desliga o SSE
  let execucaoLigada = null;
  let ultima = null; // a última amostra recebida
  const avisos = [];
  let vivo = true;

  function avisa(texto, classe = 'at') {
    if (avisos.some((a) => a.texto === texto)) return;
    avisos.push({ texto, classe, quando: new Date() });
    desenhaAvisos();
  }

  // Avisos de passagem ("abrindo o replay…") somem quando o passo seguinte chega.
  function tiraPassageiros() {
    for (let i = avisos.length - 1; i >= 0; i--) if (avisos[i].classe === 'n') avisos.splice(i, 1);
    desenhaAvisos();
  }

  function desenhaAvisos() {
    cartaoAvisos.hidden = !avisos.length;
    avisosEl.replaceChildren(...avisos.slice(-8).map((a) => el('li', a.classe, a.texto)));
  }

  function nomeDe(pid) {
    const pt = info?.porId[pid];
    return pt ? `${pt.campeao} · ${pt.nome}` : `jogador ${pid}`;
  }

  function desenhaCabecalho() {
    const e = ESTADOS_RASTREIO[r.estado] || { classe: 'n', texto: r.estado };
    const partes = [];
    if (info) {
      const dono = participantePrincipal(info.partida, passes.map((p) => p.participante_id));
      const res = dono.venceu ? 'vitória' : 'derrota';
      partes.push(el('b', '', `${dono.campeao} · ${res} · ${duracao(info.partida.duracao_s)}`));
      partes.push(el('span', 'sub', dataCurta(info.partida.criada_em)));
    } else {
      partes.push(el('b', '', `Partida ${r.game_id}`));
    }
    partes.push(pilula(r.estado === 'rodando' ? `rastreando a ${numero(r.velocidade)}×` : e.texto, e.classe));
    const acoes = el('div', 'linha acoes-dir');
    if (r.estado === 'rodando') {
      acoes.append(botao('Pausar', '', () => acao('pausa')));
    }
    if (r.estado === 'pausado' || r.estado === 'falhou') {
      const b = botao('Retomar', 'pri', () => acao('retoma'));
      const pode = podeRastrear(ctx.estado());
      if (!pode.ok) {
        b.disabled = true;
        b.title = pode.motivo;
      }
      acoes.append(b);
    }
    if (r.estado === 'rodando' || r.estado === 'pausado' || r.estado === 'falhou') {
      const c = botao('Cancelar', 'perigo');
      c.addEventListener('click', () => confirmaNoBotao(c, 'Cancelar mesmo?', () => acao('cancela')));
      acoes.append(c);
    }
    if (passes.some((p) => p.estado === 'concluido' || p.amostras > 0)) {
      acoes.append(link('Abrir a partida', `#partida/${r.game_id}`, 'btn'));
    }
    cab.replaceChildren(...partes, acoes);
  }

  function desenhaGeral() {
    const trecho = r.t_inicio_ms || r.t_fim_ms ? ` · trecho ${relogio(r.t_inicio_ms)}–${r.t_fim_ms ? relogio(r.t_fim_ms) : 'fim'}` : '';
    const partes = [
      el('span', 'num', `${r.passes_concluidos} de ${plural(r.passes_total, 'jogador', 'jogadores')}`),
      barra(r.progresso, 'progresso do rastreio', 'larga'),
    ];
    if (r.estado === 'rodando' && r.estimativa_s > 0) partes.push(el('span', 'num', `faltam ${tempoHumano(r.estimativa_s)}`));
    partes.push(el('span', 'sub', `${Math.round((r.progresso || 0) * 100)}%${trecho}`));
    geral.replaceChildren(...partes);
    if (r.erro) avisa(r.erro, 'cr');
  }

  function linhaPasse(p) {
    const pt = info?.porId[p.participante_id];
    const tecla = TECLAS[p.participante_id - 1] || String(p.participante_id);
    const linha = el('div', 'passe');
    linha.dataset.passe = String(p.id);
    linha.append(el('span', `tc ${pt?.time === 200 ? 'tm-v' : 'tm-a'}`, tecla));
    const nome = el('div', 'com-retrato', [
      pt ? retrato(pt, 26) : null,
      el('div', '', [el('div', '', nomeDe(p.participante_id)), el('div', 'm', detalhePasse(p))]),
    ]);
    linha.append(nome);
    const fr = progressoPasse(p, r);
    linha.append(barra(fr, `progresso de ${nomeDe(p.participante_id)}`, p.estado === 'concluido' ? 'ok' : ''));
    const fim = el('span', 'fim');
    if (p.estado === 'rodando') fim.append(el('span', 'num', relogio(p.t_atual_ms)));
    else {
      const pil = pilulaPasse(p);
      fim.append(pilula(pil.texto, pil.classe, p.erro || undefined));
    }
    linha.append(fim);
    return linha;
  }

  function detalhePasse(p) {
    const partes = [];
    if (p.amostras) partes.push(`${numero(p.amostras)} leituras`);
    if (p.taxa) partes.push(`${numero(p.taxa)} por segundo de jogo`);
    if (p.retravas) partes.push(`câmera refeita ${p.retravas}×`);
    if (p.conferencia_n) partes.push(`Riot: ${p.conferencia_iguais} de ${p.conferencia_n} iguais`);
    if (p.erro) partes.push(p.erro);
    return partes.join(' · ') || (p.estado === 'pendente' ? 'esperando a vez' : '');
  }

  function desenhaPasses() {
    passesEl.replaceChildren(...passes.map(linhaPasse));
  }

  function desenhaLeitura() {
    if (!ultima) {
      leituraDir.textContent = '';
      leitura.replaceChildren(el('span', 'sub span2', r.estado === 'rodando' ? 'Esperando a primeira leitura…' : 'Nenhuma leitura ao vivo agora.'));
      return;
    }
    const a = ultima;
    const cd = a.cd || {};
    const zona = a.x !== undefined && a.x !== null && grade ? nomeZona(zonaNaGrade(grade, a.x, a.y)) : '';
    leituraDir.textContent = `${nomeDe(a.participante_id)} · ${relogio(a.t_ms)}`;
    const vida = a.vida !== null && a.vida !== undefined ? `${numero(Math.round(a.vida))} / ${numero(a.vida_max)}` : 'não lida';
    const mana = a.mana !== null && a.mana !== undefined ? `${numero(Math.round(a.mana))} / ${numero(a.mana_max)}` : 'não lida';
    const pos = a.morto ? 'morto' : a.x !== undefined && a.x !== null ? `x ${numero(a.x)} · y ${numero(a.y)}${zona ? ` · ${zona}` : ''}` : 'sem posição';
    const recs = el('b', 'recargas', ['q', 'w', 'e', 'r', 'd', 'f'].map((k) => {
      const rc = recarga(cd[k]);
      const s = el('span', `rc ${rc.classe}`, `${k.toUpperCase()} ${rc.texto === 'pronta' ? '0' : rc.texto.replace(' s', '')}`);
      s.title = `${k.toUpperCase()}: ${rc.titulo}`;
      return s;
    }));
    leitura.replaceChildren(
      el('span', '', 'Vida'),
      el('b', '', vida),
      el('span', '', 'Mana'),
      el('b', '', mana),
      el('span', '', 'Posição'),
      el('b', '', pos),
      el('span', '', 'Recargas'),
      recs,
      el('span', '', 'Nível e CS'),
      el('b', '', `${a.nivel ?? '—'} · ${a.cs ?? '—'} CS`),
      el('span', '', 'Câmera'),
      el('b', a.trava_perdida ? 'tx-at' : '', a.trava_perdida ? 'perdeu o jogador (refazendo)' : 'no jogador'),
    );
  }

  function desenhaOutros(lista) {
    if (!lista.length) {
      outros.replaceChildren(el('div', 'estado-tela vazio', 'Nenhum outro rastreio.'));
      return;
    }
    outros.replaceChildren(
      ...lista.slice(0, 12).map((x) => {
        const e = ESTADOS_RASTREIO[x.estado] || { classe: 'n', texto: x.estado };
        const a = link('', `#rastreio/${x.id}`, `linha-lista${x.id === id ? ' atual' : ''}`);
        a.append(
          el('span', 'num', `nº ${x.id}`),
          el('span', '', `partida ${x.game_id}`),
          el('span', 'sub', `${x.passes_concluidos}/${x.passes_total} jogadores · ${dataCurta(x.criado_em)}`),
          pilula(e.texto, e.classe),
        );
        if (x.id === id) a.setAttribute('aria-current', 'true');
        return a;
      }),
    );
  }

  async function recarrega() {
    try {
      const d = await api.rastreio(id);
      if (!vivo) return;
      r = d.rastreio;
      passes = d.passes;
      if (!info) info = await partidaComNomes(r.game_id).catch(() => null);
      if (!vivo) return;
      desenhaCabecalho();
      desenhaGeral();
      desenhaPasses();
      desenhaLeitura();
      for (const p of passes) {
        if (p.estado === 'falhou' && p.erro) avisa(`${nomeDe(p.participante_id)}: ${p.erro}`, 'cr');
        const conf = p.conferencia_riot;
        if (conf && conf.n && conf.iguais < conf.n) {
          avisa(`${nomeDe(p.participante_id)}: a leitura da tela bateu com a Riot em ${conf.iguais} de ${conf.n} conferências (maior diferença ${numero(conf.maior_dif)}).`, 'at');
        }
      }
      ligaSSE();
    } catch (err) {
      if (!vivo) return;
      if (!r) passesEl.replaceChildren(erroNaTela(err, err.status === 404 ? 'Esse rastreio não existe. Escolha outro na lista abaixo.' : undefined));
    }
  }

  function ligaSSE() {
    const ex = r.execucao?.id;
    if (!ex || ex === execucaoLigada) return;
    execucaoLigada = ex;
    parar?.();
    parar = acompanha(
      ex,
      {
        abrindo_replay: () => avisa('Abrindo o replay no jogo…', 'n'),
        replay_aberto: (d) => {
          tiraPassageiros();
          if (d.largura && (d.largura !== 2560 || d.altura !== 1440)) {
            avisa(`O jogo abriu em ${d.largura}×${d.altura}. A leitura de vida, mana e recargas só conhece 2560×1440: a posição continua sendo gravada.`, 'at');
          }
        },
        passe_inicio: () => recarrega(),
        trava: (d) => {
          if (!d.ok) avisa(`A câmera não travou no jogador${d.motivo ? `: ${d.motivo}` : ''}. Tentando de novo.`, 'at');
        },
        amostra: (d) => {
          if (!ultima) tiraPassageiros();
          ultima = d;
          const p = passes.find((x) => x.id === d.passe_id);
          if (p) {
            p.t_atual_ms = d.t_ms;
            p.amostras = d.amostras;
            p.retravas = d.retravas;
            p.estado = 'rodando';
            const velha = passesEl.querySelector(`[data-passe="${p.id}"]`);
            if (velha) velha.replaceWith(linhaPasse(p));
          }
          desenhaLeitura();
        },
        passe_fim: () => recarrega(),
        aviso_tela: (d) => {
          const nome = nomeDe(passes.find((p) => p.id === d.passe_id)?.participante_id);
          avisa(`${nome}: a leitura da tela não reconheceu o jogo (precisa de 2560×1440 com o HUD na escala padrão). Vida, mana e recargas ficam em branco neste jogador; a posição continua.`, 'at');
        },
        conferencia_riot: () => recarrega(),
        pausado: (d) => {
          if (d.motivo) avisa(`Pausado: ${d.motivo}`, 'at');
          recarrega();
        },
        concluido: () => {
          toast('Rastreio concluído. A análise da partida já foi refeita.', 'ok');
          recarrega();
        },
        fechando_replay: () => avisa('Fechando o replay…', 'n'),
        aviso: (d) => avisa(d.texto, 'at'),
        erro: (d) => avisa(d.texto, 'cr'),
        fim: () => {
          execucaoLigada = null;
          ctx.atualizaSeloRastreio();
          recarrega();
        },
      },
      { textoQueda: 'A conexão ao vivo com o servidor caiu; a tela segue atualizando a cada poucos segundos.' },
    );
  }

  async function acao(qual) {
    try {
      if (qual === 'pausa') {
        await api.pausa(id);
        toast('Pausado. O replay fecha; dá para retomar depois do ponto onde parou.');
      } else if (qual === 'retoma') {
        await api.retoma(id);
        toast('Retomando: o replay vai abrir de novo.');
      } else {
        await api.cancela(id);
        toast('Rastreio cancelado. O que já foi lido fica guardado.');
      }
    } catch (err) {
      toast(err.message, 'erro');
    }
    ctx.atualizaSeloRastreio();
    await recarrega();
  }

  // A última leitura gravada (para quem abre a tela no meio, ou depois): o passe rodando ou o
  // último com leituras.
  async function buscaUltimaLeitura() {
    const p = passes.find((x) => x.estado === 'rodando') || [...passes].reverse().find((x) => x.amostras > 0);
    if (!p || !info) return;
    try {
      const t = Math.max(0, (p.t_atual_ms || 0) / 1000);
      const d = await api.amostras(r.game_id, p.participante_id, Math.max(0, t - 3), t + 1, 0);
      const a = d.amostras.at(-1);
      if (a && !ultima && vivo) {
        ultima = { ...a, participante_id: p.participante_id, passe_id: p.id };
        desenhaLeitura();
      }
    } catch {
      /* sem leitura guardada: a tela espera a próxima ao vivo */
    }
  }

  desenhaOutros(rastreios);
  await recarrega();
  buscaUltimaLeitura();

  // Ao vivo é pelo SSE; o intervalo só segura a tela se a conexão cair.
  const intervalo = setInterval(() => {
    if (r && (r.estado === 'rodando' || !execucaoLigada)) recarrega();
  }, 5000);
  const desliga = ctx.aoMudarEstado(() => r && desenhaCabecalho());

  return () => {
    vivo = false;
    clearInterval(intervalo);
    desliga();
    parar?.();
  };
}
