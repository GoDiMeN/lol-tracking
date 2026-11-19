// O PAINEL DE CONVERSA COM A IA: sobre uma partida (com o jogador em foco; o contexto dela já vai
// junto) ou geral (o servidor escolhe as partidas pelo banco). Fica à direita, por cima da tela;
// o histórico é do servidor (uma conversa por linha em ia_conversas), a resposta chega em fluxo
// (SSE). Instantes viram link para o mapa com play e podem abrir o quadro do mapa ali mesmo; a
// IA pode pedir dados de um instante (o servidor atende e anexa) e propõe a próxima pergunta.

import { api, acompanha } from './api.js';
import { el, botao, toast, ultimoJogador } from './ui.js';
import { blocosMarkdown, segmentos, rotaDoInstante } from './ia.js';
import { quadroDoMapa } from './quadro-ia.js';
import { relogio } from './texto.js';

let aberto = null;

// fechaConversa: fecha o painel aberto (ao trocar de tela, por exemplo).
export function fechaConversa() {
  if (!aberto) return;
  aberto.desliga?.();
  aberto.raiz.remove();
  document.removeEventListener('keydown', aberto.tecla, true);
  aberto = null;
}

// abreConversa: abre o painel. `info` = partidaComNomes(gameId) na conversa de partida.
// `pergunta` (opcional) já vai escrita (o "perguntar ao especialista" do relatório).
export async function abreConversa({ gameId = 0, pid = 0, info = null, pergunta = '', enviaJa = false } = {}) {
  fechaConversa();
  const raiz = el('aside', 'painel-ia');
  raiz.setAttribute('role', 'dialog');
  raiz.setAttribute('aria-label', 'Conversa com a IA');
  const estado = { conversa: null, desliga: null, execucao: null };
  const tecla = (ev) => {
    if (ev.key === 'Escape' && !ev.defaultPrevented) {
      ev.preventDefault();
      fechaConversa();
    }
  };
  aberto = { raiz, tecla, desliga: () => estado.desliga?.() };
  document.addEventListener('keydown', tecla, true);
  window.addEventListener('hashchange', fechaConversa, { once: true });

  const foco = info?.porId?.[pid];
  const titulo = gameId ? `Conversa com a IA · ${foco?.campeao || `jogador ${pid}`}` : 'Conversa geral com a IA';
  const seletor = el('select', 'campo seletor-conversa');
  seletor.setAttribute('aria-label', 'Conversas anteriores');
  const nova = botao('Nova conversa', 'mini', () => novaConversa());
  const fecha = botao('✕', 'icone', fechaConversa, 'fechar (Esc)');
  fecha.setAttribute('aria-label', 'Fechar a conversa');
  const cab = el('div', 'painel-ia-cab', [el('h2', '', titulo), el('div', 'linha', [seletor, nova, fecha])]);
  const aviso = el('p', 'sub painel-ia-aviso');
  const lista = el('div', 'painel-ia-lista');
  lista.setAttribute('aria-live', 'polite');
  const caixa = el('textarea', 'campo painel-ia-caixa');
  caixa.rows = 3;
  caixa.placeholder = gameId ? 'Pergunte sobre a partida (ex.: por que eu morri aos 8:48?)' : 'Pergunte sobre as suas partidas (ex.: onde eu mais morro de Vi antes dos 10 min?)';
  caixa.value = pergunta;
  const envia = botao('Perguntar', 'pri', () => manda());
  const para = botao('Parar', 'perigo', () => estado.execucao && api.cancelaExecucao(estado.execucao).catch(() => {}));
  para.hidden = true;
  caixa.addEventListener('keydown', (ev) => {
    if (ev.key === 'Enter' && (ev.ctrlKey || ev.metaKey)) {
      ev.preventDefault();
      manda();
    }
  });
  const privacidade = el(
    'p',
    'sub painel-ia-privacidade',
    gameId
      ? 'Sai da máquina, para o Claude Code: o texto de fatos desta partida (com os nomes de invocador), o histórico desta conversa, a pergunta e os dados de instante que a IA pedir. Nenhum arquivo, caminho de disco ou chave.'
      : 'Sai da máquina, para o Claude Code: uma linha por partida escolhida pelo banco, o histórico desta conversa e a pergunta. Nenhum arquivo, caminho de disco ou chave.',
  );
  raiz.append(cab, aviso, lista, el('div', 'painel-ia-envio', [caixa, el('div', 'linha', [envia, para, el('span', 'sub', 'Ctrl+Enter')])]), privacidade);
  document.body.append(raiz);
  caixa.focus();

  api
    .estadoIA()
    .then((e) => {
      if (!e.disponivel) {
        aviso.textContent = e.motivo;
        aviso.classList.add('tx-cr');
        envia.disabled = true;
      } else {
        aviso.textContent = `${e.implementacao === 'falso' ? 'IA falsa (modo teste)' : `Claude Code ${e.versao}`} · modelo da conversa: ${e.modelos?.conversa || '—'}`;
      }
    })
    .catch(() => {});

  async function carregaLista(escolhe) {
    const { conversas } = await api.conversasIA(gameId, pid);
    seletor.replaceChildren(...conversas.map((c) => {
      const o = el('option', '', `${c.titulo || 'sem título'} · ${c.mensagens} msg`);
      o.value = String(c.id);
      return o;
    }));
    seletor.hidden = !conversas.length;
    const id = escolhe || conversas[0]?.id;
    if (id) {
      seletor.value = String(id);
      await abre(id);
    } else {
      estado.conversa = null;
      lista.replaceChildren(el('p', 'sub painel-ia-vazio', gameId ? 'Nenhuma conversa sobre esta partida ainda.' : 'Nenhuma conversa geral ainda.'));
    }
  }
  seletor.addEventListener('change', () => abre(Number(seletor.value)));

  async function novaConversa() {
    const { conversa } = await api.criaConversaIA(gameId, pid);
    await carregaLista(conversa.id);
    caixa.focus();
    return conversa;
  }

  async function abre(id) {
    const r = await api.conversaIA(id);
    estado.conversa = r.conversa;
    desenhaMensagens(r.mensagens);
    if (r.execucao) segue(r.execucao.id, null);
  }

  function desenhaMensagens(ms) {
    lista.replaceChildren(...ms.map((m, i) => bolha(m, i === ms.length - 1)));
    if (!ms.length) lista.append(el('p', 'sub painel-ia-vazio', 'Faça a primeira pergunta.'));
    lista.scrollTop = lista.scrollHeight;
  }

  // textoComLinks: um bloco de texto com os instantes e partidas virando link (e o botão do quadro).
  function textoComLinks(alvo, texto, forte) {
    const pai = forte ? el('strong') : alvo;
    for (const s of segmentos(texto, gameId || null)) {
      if (s.tipo === 'texto') pai.append(document.createTextNode(s.v));
      else if (s.tipo === 'partida') {
        const a = el('a', '', s.v);
        a.href = `#partida/${s.gameId}`;
        pai.append(a);
      } else {
        const a = el('a', 'num', s.v);
        a.href = rotaDoInstante(s.gameId, s.ms);
        a.title = 'abrir o mapa com play neste instante';
        pai.append(a);
        if (gameId && s.gameId === gameId && info) {
          const b = el('button', 'btn-quadro', '▣');
          b.type = 'button';
          b.title = `ver o quadro do mapa aos ${s.v}`;
          b.setAttribute('aria-label', b.title);
          b.addEventListener('click', () => {
            const bloco = b.closest('.bolha');
            const ja = bloco.querySelector(`.quadro-ia[data-t="${s.ms}"]`);
            if (ja) ja.remove();
            else {
              const q = quadroDoMapa({ gameId, ms: s.ms, foco: pid, info });
              q.dataset.t = String(s.ms);
              bloco.append(q);
            }
          });
          pai.append(b);
        }
      }
    }
    if (forte) alvo.append(pai);
  }

  function markdown(alvo, texto) {
    let ul = null;
    for (const b of blocosMarkdown(texto)) {
      const e = b.tipo === 'h' ? el('h3') : b.tipo === 'li' ? el('li') : el('p');
      for (const p of b.partes) textoComLinks(e, p.texto, p.forte);
      if (b.tipo === 'li') {
        if (!ul) {
          ul = el('ul');
          alvo.append(ul);
        }
        ul.append(e);
      } else {
        ul = null;
        alvo.append(e);
      }
    }
  }

  function bolha(m, ultima) {
    if (m.papel === 'dados') {
      const p = m.dados?.pedido;
      const d = el('details', 'bolha dados');
      d.append(el('summary', '', p?.tipo === 'partida' ? `o app anexou o pacote da partida ${p.game_id} (a IA pediu)` : `o app anexou os dados do instante ${p ? relogio(p.t_ms) : ''} (a IA pediu)`), el('pre', '', m.texto));
      return d;
    }
    const b = el('div', `bolha ${m.papel}`);
    if (m.papel === 'usuario') {
      b.append(el('p', '', m.texto));
      return b;
    }
    if (m.estado !== 'ok') {
      b.append(el('p', 'tx-cr', m.estado === 'cancelada' ? 'Resposta cancelada.' : `A IA não respondeu: ${m.erro}`));
      return b;
    }
    if (m.dados?.filtro) b.append(el('p', 'sub', `Partidas escolhidas pelo banco · filtro: ${m.dados.filtro}`));
    markdown(b, m.texto);
    const rodape = [m.modelo, m.duracao_ms ? `${(m.duracao_ms / 1000).toFixed(1)} s` : '', m.caracteres_entrada ? `${m.caracteres_entrada.toLocaleString('pt-BR')} caracteres enviados` : '']
      .filter(Boolean)
      .join(' · ');
    if (rodape) b.append(el('p', 'sub bolha-rodape', rodape));
    if (ultima && m.dados?.proxima_pergunta) {
      const chip = botao(m.dados.proxima_pergunta, 'mini proxima-pergunta', () => {
        caixa.value = m.dados.proxima_pergunta;
        manda();
      }, 'a IA sugere esta pergunta');
      b.append(chip);
    }
    return b;
  }

  async function manda() {
    const texto = caixa.value.trim();
    if (!texto || envia.disabled) return;
    envia.disabled = true;
    try {
      if (!estado.conversa) await novaConversa();
      const r = await api.perguntaIA(estado.conversa.id, texto, gameId ? '' : ultimoJogador() || '');
      caixa.value = '';
      lista.querySelector('.painel-ia-vazio')?.remove();
      lista.append(bolha(r.pergunta, false));
      segue(r.execucao.id, r.pergunta);
    } catch (err) {
      toast(err.message, 'erro');
      envia.disabled = false;
    }
  }

  function segue(execId) {
    estado.execucao = execId;
    envia.disabled = true;
    para.hidden = false;
    const viva = el('div', 'bolha assistente viva');
    const notas = el('div', 'sub');
    const corpo = el('p', 'texto-vivo', '');
    viva.append(notas, corpo, el('span', 'sub digitando', 'a IA está respondendo…'));
    lista.append(viva);
    lista.scrollTop = lista.scrollHeight;
    const nota = (t) => notas.append(el('div', '', t));
    estado.desliga?.();
    estado.desliga = acompanha(
      execId,
      {
        na_fila: (d) => nota(d.texto),
        partidas: (d) => nota(`Partidas escolhidas pelo banco: ${d.partidas.length} · filtro: ${d.filtro}`),
        pedido: (d) => nota(d.tipo === 'partida' ? `A IA pediu o pacote da partida ${d.game_id}; o app anexou.` : `A IA pediu os dados do instante ${relogio(d.t_ms)}; o app anexou.`),
        recomeca: () => {
          corpo.textContent = '';
        },
        trecho: (d) => {
          corpo.textContent += d.texto;
          lista.scrollTop = lista.scrollHeight;
        },
        concluido: () => {},
        cancelado: () => {},
        erro: (d) => nota(`Erro: ${d.texto}`),
        fim: () => {
          estado.execucao = null;
          para.hidden = true;
          envia.disabled = false;
          // Recarrega a lista também: o título e a contagem mudam com a primeira resposta.
          if (estado.conversa) carregaLista(estado.conversa.id).catch(() => {});
        },
      },
      { textoQueda: 'a conexão com o servidor caiu durante a resposta' },
    );
  }

  try {
    await carregaLista();
    if (pergunta && enviaJa) manda();
  } catch (err) {
    lista.replaceChildren(el('p', 'tx-cr', `Não abriu a conversa: ${err.message}`));
  }
}
