// Configurações: o estado do cliente do LoL, a chave da Riot (válida ou não; a chave nunca
// aparece), a pasta de dados e quanto ocupa, a velocidade padrão do rastreio e o tema.

import { api } from '../api.js';
import { dataCurta } from '../formato.js';
import { bytes } from '../texto.js';
import { el, cartao, pilula, preferencia, guardaPreferencia, aplicaTema, TEMA_PADRAO, erroNaTela, toast } from '../ui.js';

export async function desenha(raiz, _rota, ctx) {
  const h1 = el('h1', 'titulo-tela', 'Configurações');
  const cliente = el('div', 'kv');
  const dados = el('div', 'kv');
  raiz.append(h1, el('div', 'grade-2', [cartao('Cliente do LoL e chave da Riot', null, cliente), cartao('Dados', null, dados)]), preferencias());

  function desenhaCliente(e) {
    if (!e) {
      cliente.replaceChildren(el('span', 'span2 sub', 'O servidor do Lane não respondeu.'));
      return;
    }
    const linhas = [
      ['Cliente do LoL', e.lcu.conectado ? pilula('conectado', 'ok') : pilula('fechado', 'cr')],
      ['Conta', e.invocador ? `${e.invocador.nome}#${e.invocador.tag}` : '—'],
      ['Versão do jogo', e.patch ? `patch ${e.patch}` : '—'],
      ['Replays', e.lcu.conectado ? (e.replays_habilitados ? 'habilitados' : 'desligados no cliente') : '—'],
      ['Pasta dos replays', e.pasta_replays || '—'],
    ];
    const ch = e.chave_riot || {};
    let chave;
    if (!ch.configurada) chave = pilula('sem chave', 'n', 'opcional: sem ela não há o dano de cada abate nem a conferência com a Riot');
    else if (ch.valida === true) chave = pilula('válida', 'ok');
    else if (ch.valida === false) chave = pilula('não aceita pela Riot', 'cr');
    else chave = pilula('não conferida', 'n');
    linhas.push(['Chave da API da Riot', chave]);
    if (ch.detalhe) linhas.push(['', ch.detalhe]);
    if (ch.verificada_em) linhas.push(['Conferida em', dataCurta(ch.verificada_em)]);
    if (e.modo_teste) linhas.push(['Modo', pilula('modo teste: cliente e jogo falsos', 'at')]);
    cliente.replaceChildren(...linhas.flatMap(([k, v]) => [el('span', '', k), typeof v === 'string' ? el('b', 'quebra', v) : el('b', '', [v])]));
    cliente.append(
      el(
        'p',
        'span2 sub',
        'A chave da Riot fica no arquivo token.txt ao lado do projeto e nunca aparece na tela. Chave de desenvolvimento vence em 24 horas; o app funciona sem ela.',
      ),
    );
  }
  desenhaCliente(ctx.estado());
  const desliga = ctx.aoMudarEstado(desenhaCliente);

  try {
    const d = await api.dados();
    dados.replaceChildren(
      el('span', '', 'Pasta'),
      el('b', 'quebra', d.pasta),
      el('span', '', 'Banco de dados'),
      el('b', '', `${bytes(d.bytes.banco)} (${d.banco})`),
      el('span', '', 'Imagens dos campeões e itens'),
      el('b', '', bytes(d.bytes.imagens)),
      el('span', '', 'Capturas da tela guardadas'),
      el('b', '', bytes(d.bytes.capturas)),
      el('span', '', 'Outros'),
      el('b', '', bytes(d.bytes.outros)),
      el('span', '', 'Total'),
      el('b', '', bytes(d.bytes.total)),
      el('p', 'span2 sub', 'Os replays (.rofl) ficam na pasta do cliente do LoL, não aqui. Para guardar uma cópia de tudo, copie a pasta acima com o app fechado.'),
    );
  } catch (err) {
    dados.replaceChildren(el('div', 'span2', [erroNaTela(err)]));
  }
  return desliga;
}

function preferencias() {
  const corpo = el('div', 'prefs');
  const vel = el('fieldset', 'opcoes');
  vel.append(el('legend', '', 'Velocidade padrão do rastreio'));
  const atual = preferencia('velocidade', '2');
  for (const v of ['2', '4', '8']) {
    const r = el('input');
    r.type = 'radio';
    r.name = 'vel-padrao';
    r.value = v;
    r.checked = v === atual;
    r.addEventListener('change', () => {
      guardaPreferencia('velocidade', v);
      toast(`Velocidade padrão: ${v}×.`, 'ok');
    });
    vel.append(el('label', 'opcao', [r, el('span', '', `${v}×${v === '2' ? ' (recomendada)' : ''}`)]));
  }
  vel.append(el('p', 'sub', 'A 2× a leitura da tela foi conferida com o jogo; 4× e 8× terminam antes, mas leem menos vezes por segundo de jogo.'));

  const tema = el('fieldset', 'opcoes');
  tema.append(el('legend', '', 'Tema'));
  // Preferência antiga "auto" (igual ao Windows) deixou de existir: cai no padrão.
  const guardado = preferencia('tema', TEMA_PADRAO);
  const t = guardado === 'claro' ? 'claro' : 'escuro';
  for (const [v, rot] of [
    ['escuro', 'escuro (padrão)'],
    ['claro', 'claro'],
  ]) {
    const r = el('input');
    r.type = 'radio';
    r.name = 'tema';
    r.value = v;
    r.checked = v === t;
    r.addEventListener('change', () => {
      guardaPreferencia('tema', v);
      aplicaTema(v);
    });
    tema.append(el('label', 'opcao', [r, el('span', '', rot)]));
  }
  corpo.append(vel, tema);
  return cartao('Preferências deste computador', 'guardadas ao mudar', corpo);
}
