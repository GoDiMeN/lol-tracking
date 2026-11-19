// Partidas rastreadas: só as que vieram da busca de um jogador (origem "pesquisa"; as do estudo
// ficam na área de Estudo), cada uma do ponto de vista do jogador pesquisado, com filtro por
// jogador, resultado e rota (a escolha fica guardada) e atalho para a análise e o mapa com play.

import { api } from '../api.js';
import { duracao, dataCurta, kda, titulo } from '../formato.js';
import { el, link, botao, pilula, retrato, carregando, vazio, erroNaTela, partidaComNomes, preferencia, guardaPreferencia } from '../ui.js';
import { abreConversa } from '../conversa-ia.js';
import { participanteEmFoco, passaFiltros, opcoesJogadores, escolhaValida, ROTAS_FILTRO, RESULTADOS_FILTRO } from '../rastreadas.js';

// Os dados de cada partida (nomes, campeões) mudam pouco: guardados entre trocas de filtro.
const infos = new Map();

async function infoDe(gameId) {
  if (!infos.has(gameId)) {
    infos.set(
      gameId,
      partidaComNomes(gameId).catch((err) => {
        infos.delete(gameId);
        throw err;
      }),
    );
  }
  return infos.get(gameId);
}

function seletor(opcoes, valor, rotulo, aoMudar) {
  const s = el('select', 'campo');
  s.setAttribute('aria-label', rotulo);
  s.title = rotulo;
  for (const [v, texto] of opcoes) {
    const o = el('option', '', texto);
    o.value = v;
    s.append(o);
  }
  s.value = valor;
  s.addEventListener('change', () => aoMudar(s.value));
  return s;
}

export async function desenha(raiz) {
  // Conversa geral com a IA no canto superior direito: o servidor escolhe as partidas que
  // respondem à pergunta só pelo banco (base de fatos), e só entre as partidas pesquisadas.
  const geral = botao('Conversar com a IA', '', () => abreConversa({ gameId: 0 }), 'perguntar sobre as partidas rastreadas (o servidor escolhe as partidas pelo banco)');
  const h1 = el('div', 'linha cab-tela', [el('h1', 'titulo-tela inline', 'Partidas rastreadas'), el('div', 'linha acoes-dir', [geral])]);
  raiz.append(h1, carregando('Carregando as partidas rastreadas…'));
  let todas;
  try {
    todas = await api.rastreadas();
  } catch (err) {
    raiz.replaceChildren(h1, erroNaTela(err));
    return;
  }
  if (!todas.partidas.length) {
    raiz.replaceChildren(
      h1,
      vazio('Nenhuma partida rastreada ainda. Em Jogadores, busque um jogador, escolha uma partida com o replay baixado e clique em Rastrear.', link('Ir para Jogadores', '#jogadores', 'btn pri')),
    );
    return;
  }

  const filtro = {
    jogador: escolhaValida(preferencia('partidas.jogador', ''), todas.jogadores),
    resultado: preferencia('partidas.resultado', ''),
    rota: preferencia('partidas.rota', ''),
  };
  const muda = (chave) => (v) => {
    filtro[chave] = v;
    guardaPreferencia(`partidas.${chave}`, v);
    atualiza();
  };
  const barra = el('div', 'linha filtros-partidas', [
    seletor(opcoesJogadores(todas.jogadores, todas.partidas.length), filtro.jogador, 'Jogador pesquisado', muda('jogador')),
    seletor(RESULTADOS_FILTRO, filtro.resultado, 'Resultado do jogador', muda('resultado')),
    seletor(ROTAS_FILTRO, filtro.rota, 'Rota do jogador', muda('rota')),
  ]);
  const conta = el('span', 'sub', '');
  barra.append(conta);
  const lista = el('div', 'cartao');
  raiz.replaceChildren(
    h1,
    el('p', 'sub', 'As partidas que você rastreou a partir da busca de um jogador, do ponto de vista dele. Abra uma para ver o retrato de cada abate, a vida ao longo do jogo e as saídas de rota, ou assista no mapa com play.'),
    barra,
    lista,
  );

  let geracao = 0;
  async function atualiza() {
    const minha = ++geracao;
    lista.replaceChildren(carregando('Carregando…'));
    let partidas = todas.partidas;
    if (filtro.jogador) {
      try {
        ({ partidas } = await api.rastreadas({ puuid: filtro.jogador }));
      } catch (err) {
        if (minha === geracao) lista.replaceChildren(erroNaTela(err));
        return;
      }
    }
    const linhas = await Promise.all(
      partidas.map(async (r) => {
        try {
          const info = await infoDe(r.game_id);
          const foco = participanteEmFoco(r, info.partida.participantes);
          return passaFiltros(foco, filtro) ? linha(r, info, foco) : null;
        } catch {
          return el('div', 'partida', [el('i'), el('div', 't', `Partida ${r.game_id}`), el('div', 'm', 'não deu para ler os dados desta partida')]);
        }
      }),
    );
    if (minha !== geracao) return;
    const visiveis = linhas.filter(Boolean);
    conta.textContent = visiveis.length === 1 ? '1 partida' : `${visiveis.length} partidas`;
    if (!visiveis.length) {
      lista.replaceChildren(el('p', 'sub pad', 'Nenhuma partida com esses filtros.'));
      return;
    }
    lista.replaceChildren(...visiveis);
  }
  await atualiza();
}

function linha(r, info, foco) {
  const p = info.partida;
  const l = el('div', `partida ${foco.venceu ? 'v' : 'd'}`);
  l.append(el('i'));
  const quem = r.foco?.riot_id ? r.foco.riot_id : '';
  l.append(
    el('div', 'com-retrato', [
      retrato(foco, 32),
      el('div', '', [
        el('div', 't', titulo(foco)),
        el('div', 'm', [quem, p.fila_nome, duracao(p.duracao_s), dataCurta(p.criada_em)].filter(Boolean).join(' · ')),
      ]),
    ]),
  );
  // A tira mostra quem foi rastreado, com o jogador em foco destacado.
  const rastreados = el('div', 'rastreados');
  for (const pid of r.participantes) {
    const ret = retrato(info.porId[pid], 22, pid === foco.participante_id ? 'foco' : '');
    if (pid === foco.participante_id) ret.title = 'jogador em foco';
    rastreados.append(ret);
  }
  l.append(el('div', 'm m2', [el('div', '', `${kda(foco)} · ${foco.cs} CS`), rastreados]));
  l.append(
    el('div', 'linha acoes', [
      pilula(`${r.rastreados} de 10 rastreados`, r.rastreados === 10 ? 'ok' : 'ac'),
      link('Mapa com play', `#replay/${r.game_id}`, 'btn'),
      link('Abrir', `#partida/${r.game_id}/${foco.participante_id}`, 'btn pri'),
    ]),
  );
  return l;
}
