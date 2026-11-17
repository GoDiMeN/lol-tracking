// Partidas rastreadas: as partidas com pelo menos um passe concluído, com atalho para a análise e
// para o mapa com play.

import { api } from '../api.js';
import { duracao, dataCurta, kda, titulo } from '../formato.js';
import { el, link, pilula, retrato, carregando, vazio, erroNaTela, partidaComNomes, participantePrincipal } from '../ui.js';

export async function desenha(raiz) {
  const h1 = el('h1', 'titulo-tela', 'Partidas rastreadas');
  raiz.append(h1, carregando('Carregando as partidas rastreadas…'));
  let partidas;
  try {
    ({ partidas } = await api.rastreadas());
  } catch (err) {
    raiz.replaceChildren(h1, erroNaTela(err));
    return;
  }
  if (!partidas.length) {
    raiz.replaceChildren(
      h1,
      vazio('Nenhuma partida rastreada ainda. Em Jogadores, escolha uma partida com o replay baixado e clique em Rastrear.', link('Ir para Jogadores', '#jogadores', 'btn pri')),
    );
    return;
  }
  const lista = el('div', 'cartao');
  raiz.replaceChildren(h1, el('p', 'sub', 'Abra uma partida para ver o retrato de cada abate, a vida ao longo do jogo e as saídas de rota, ou assista no mapa com play.'), lista);
  const linhas = await Promise.all(
    partidas.map(async (r) => {
      try {
        const info = await partidaComNomes(r.game_id);
        return linha(r, info);
      } catch {
        return el('div', 'partida', [el('i'), el('div', 't', `Partida ${r.game_id}`), el('div', 'm', 'não deu para ler os dados desta partida')]);
      }
    }),
  );
  lista.replaceChildren(...linhas);
}

function linha(r, info) {
  const p = info.partida;
  const dono = participantePrincipal(p, r.participantes);
  const l = el('div', `partida ${dono.venceu ? 'v' : 'd'}`);
  l.append(el('i'));
  l.append(
    el('div', 'com-retrato', [
      retrato(dono, 32),
      el('div', '', [
        el('div', 't', titulo({ ...dono, venceu: dono.venceu })),
        el('div', 'm', [p.fila_nome, duracao(p.duracao_s), dataCurta(p.criada_em)].join(' · ')),
      ]),
    ]),
  );
  const rastreados = el('div', 'rastreados');
  for (const pid of r.participantes) rastreados.append(retrato(info.porId[pid], 22));
  l.append(el('div', 'm m2', [el('div', '', `${kda(dono)} · ${dono.cs} CS`), rastreados]));
  l.append(
    el('div', 'linha acoes', [
      pilula(`${r.rastreados} de 10 rastreados`, r.rastreados === 10 ? 'ok' : 'ac'),
      link('Mapa com play', `#replay/${r.game_id}`, 'btn'),
      link('Abrir', `#partida/${r.game_id}/${r.participantes.includes(dono.participante_id) ? dono.participante_id : r.participantes[0]}`, 'btn pri'),
    ]),
  );
  return l;
}
