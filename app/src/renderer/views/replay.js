// Mapa com play (tela cheia): Summoner's Rift com os 10 como o retrato do campeão (borda da cor do
// time) e o nome do jogador, andando conforme os dados; play/pausa, velocidade, barra de tempo com
// os abates e objetivos, atividade de cada um e eventos surgindo no mapa. Clicar num jogador abre
// o painel com vida, mana, recargas, atributos, itens, nível, K/D/A, CS e zona naquele instante.
//
// Os dados vêm em janelas de 60 s (replay-dados.js) com pré-busca da seguinte; entre dois quadros
// do servidor (0,5 s) a posição é interpolada, salvo salto (teleporte: recall, W da LeBlanc...).
// Teclas: espaço play/pausa, ← → ±5 s, F (ou F11) tela cheia, Esc fecha o painel / sai da tela
// cheia / sai.

import { api, imagemItem } from '../api.js';
import { duracao } from '../formato.js';
import { emPorcento, paraTela, zonaNaGrade, ESTRUTURAS, estruturasCaidas, imagemEstrutura } from '../mapa.js';
import { Janelas, quadroNoInstante, atividadeNoInstante, atividadeEstavel, criaRotulador, eventosRecentes, achaIndice, PASSO_S } from '../replay-dados.js';
import { criaArranjo } from '../chamadas.js';
import { etiquetaDoJogador, textoNoMapa } from '../valida-desenho.js';
import { relogio, numero, nomeZona, recarga, atividadeTexto, atividadeCurta, nomeFeitico, eventoTexto, origemPosicao } from '../texto.js';
import {
  preparaPlacar,
  placarNoInstante,
  faseNoInstante,
  bonusComPrazo,
  NOMES_FASE,
  DRAGOES,
  DURACAO_BARAO_MS,
  DURACAO_ANCIAO_MS,
  iconeObjetivo,
  serieOuro,
  caminhosDiferenca,
  escalaOuro,
  serieAbates,
  caminhoDegraus,
  marcasDoEixo,
  objetivosEmAndamento,
  textoEmAndamento,
  ouroNoInstante,
} from '../placar.js';
import { bonusDoJogador, patchDaVersao, PATCH_DOS_BONUS, baraoNoMinuto } from '../bonus.js';
import { ATRIBUTOS, decompoe, textoObjetivo, formataAtributo, idsDoInventario, ouroDosItens, adversarioDeRota } from '../atributos.js';
import { el, botao, link, retrato, partidaComNomes, participantePrincipal, erroNaTela, preferencia, guardaPreferencia } from '../ui.js';
import { ondasNoInstante, textoEstado, textoOnda, textoCurto } from '../ondas.js';
import * as telaCheia from '../telacheia.js';
import { noMapa, colocadasAte, tituloSentinela } from '../sentinelas.js';
import { estadosNoInstante, sentinelasLidasEm, temSentinelasLidas, tituloSentinelaLida, tituloEstado, TEXTO_ESTADO } from '../visao.js';

const VELOCIDADES = [1, 2, 4, 8];
const SVG = 'http://www.w3.org/2000/svg';
const EVENTO_MS = 4000; // quanto tempo (de jogo) um abate/objetivo fica no mapa
const HABILIDADE_MS = 1500;

export async function desenha(raiz, rota, ctx) {
  const gameId = rota.gameId;
  raiz.append(el('div', 'estado-tela carregando', 'Abrindo o mapa…'));
  let info;
  let eventos;
  let zonas;
  let rastreadas;
  let fases;
  let linhaDoTempo;
  let ondas;
  let sentinelas;
  let visaoResp;
  try {
    // Fases e linha do tempo são do placar: faltando (partida sem a linha do tempo guardada),
    // o placar mostra "—" no que depende delas e o resto da tela segue. As ondas também: sem
    // elas, a caixa "ondas" fica desabilitada.
    [info, { eventos }, zonas, { partidas: rastreadas }, fases, linhaDoTempo, ondas, sentinelas, visaoResp] = await Promise.all([
      partidaComNomes(gameId),
      api.eventos(gameId),
      api.zonas(),
      api.rastreadas(),
      api.fases(gameId).then((r) => r.fases).catch(() => null),
      api.linhaDoTempo(gameId).catch(() => null),
      api.ondas(gameId).catch(() => null),
      // Sentinelas: sem elas (sem a linha do tempo da Riot), a caixa fica desabilitada com o motivo.
      api.sentinelas(gameId).catch(() => null),
      // Visão dos times (passes de visão): sem ela, a caixa "visão" fica desabilitada.
      api.visao(gameId).catch(() => null),
    ]);
  } catch (err) {
    raiz.replaceChildren(el('div', 'replay-erro', [erroNaTela(err), link('← Voltar', `#partida/${gameId}`, 'btn')]));
    return;
  }
  const p = info.partida;
  const duracaoMs = p.duracao_s * 1000;
  const rastreados = rastreadas.find((x) => x.game_id === gameId)?.participantes || [];
  const dono = participantePrincipal(p, rastreados);
  const grade = zonas.grade;
  const nomes = info.nomes;

  // Habilidades usadas (só dos rastreados) viram balões nos retratos.
  const habilidades = [];
  await Promise.all(
    rastreados.map(async (pid) => {
      try {
        const { itens } = await api.cadeia(gameId, pid, 'habilidade_usada');
        for (const it of itens) habilidades.push({ ...it, participante_id: pid });
      } catch {
        /* sem cadeia: sem os balões */
      }
    }),
  );
  habilidades.sort((a, b) => a.t_ms - b.t_ms);
  const evMapa = eventos.filter((e) => ['abate', 'monstro', 'estrutura'].includes(e.tipo) && e.x !== null && e.x !== undefined);

  // --- Estrutura da tela ---
  const sair = link('← Voltar', `#partida/${gameId}/${rastreados.includes(dono.participante_id) ? dono.participante_id : rastreados[0] || ''}`, 'btn');
  const titulo = el('div', 'rp-titulo', [
    el('b', '', `${dono.campeao} · ${dono.venceu ? 'vitória' : 'derrota'} · ${duracao(p.duracao_s)}`),
    el('span', 'sub', 'mapa com play'),
  ]);
  const cbZonas = el('input');
  cbZonas.type = 'checkbox';
  const zonasLab = el('label', 'check', [cbZonas, el('span', '', 'zonas')]);
  // As etiquetas de atividade em cima do mapa podem tapar o que acontece: dá para
  // desligar, e a lista ao lado continua mostrando o que cada um está fazendo.
  // Torres e inibidores, inteiros ou no chão conforme o tempo. Desligado por padrão.
  const cbTorres = el('input');
  cbTorres.type = 'checkbox';
  cbTorres.checked = preferencia('replay.torres', '0') === '1';
  const torresLab = el('label', 'check', [cbTorres, el('span', '', 'torres')]);
  const cbEtiquetas = el('input');
  cbEtiquetas.type = 'checkbox';
  cbEtiquetas.checked = preferencia('replay.etiquetas', '1') === '1';
  const etiquetasLab = el('label', 'check', [cbEtiquetas, el('span', '', 'atividade no mapa')]);
  // Ondas de tropas: a posição ESTIMADA da onda em cada rota (pelos jogadores da rota; nenhuma
  // tropa é rastreada) e o estado dela. Desligado por padrão.
  const cbOndas = el('input');
  cbOndas.type = 'checkbox';
  cbOndas.checked = Boolean(ondas) && preferencia('replay.ondas', '0') === '1';
  cbOndas.disabled = !ondas;
  const ondasLab = el('label', 'check', [cbOndas, el('span', '', 'ondas')]);
  ondasLab.title = ondas
    ? `Onde está a onda de tropas em cada rota, estimada pela posição dos jogadores rastreados da rota. ${(ondas.limites || []).join(' ')}`
    : 'Sem as ondas desta partida (falta a linha do tempo guardada?).';
  // Sentinelas: cada uma viva no instante, no lugar ESTIMADO (onde quem colocou estava), com o
  // círculo da incerteza. Se os passes de visão leram sentinelas do minimapa, elas substituem as
  // estimadas (lugar exato, de quando apareceram a quando sumiram). Desligado por padrão.
  const visaoDados = visaoResp?.visao || null;
  const sentLidas = temSentinelasLidas(visaoDados);
  const temSentinelas = Boolean(sentinelas?.riot) || sentLidas;
  const cbSent = el('input');
  cbSent.type = 'checkbox';
  cbSent.checked = temSentinelas && preferencia('replay.sentinelas', '0') === '1';
  cbSent.disabled = !temSentinelas;
  const sentLab = el('label', 'check', [cbSent, el('span', '', 'sentinelas')]);
  sentLab.title = sentLidas
    ? 'Sentinelas lidas no minimapa pelos passes de visão: lugar exato, de quando o marcador apareceu a quando sumiu (só o marcador comum foi medido).'
    : temSentinelas
      ? `Sentinelas vivas no instante, na cor do time. ${(sentinelas.regras || []).join(' ')}`
      : `Sem sentinelas: ${sentinelas?.motivo || 'não deu para ler as sentinelas desta partida.'}`;
  // Visão: com a visão de um time ligada, os inimigos que aquele time NÃO via no instante ficam
  // esmaecidos (o anel continua no lugar real) e os indeterminados ganham um "?".
  const selVisao = el('select', 'rp-sel-visao');
  selVisao.setAttribute('aria-label', 'Visão de um time');
  for (const [v, rot] of [['', 'desligada'], ['100', 'do time azul'], ['200', 'do time vermelho']]) {
    const o = el('option', '', rot);
    o.value = v;
    o.disabled = Boolean(v) && !visaoDados?.times?.some((x) => String(x.time) === v);
    selVisao.append(o);
  }
  const temVisao = Boolean(visaoDados?.times?.length);
  selVisao.disabled = !temVisao;
  selVisao.value = temVisao ? preferencia('replay.visao', '') : '';
  if (selVisao.selectedOptions[0]?.disabled) selVisao.value = '';
  const visaoLab = el('label', 'check', [el('span', '', 'visão'), selVisao]);
  visaoLab.title = temVisao
    ? 'Quem do outro time o time escolhido via em cada instante (lido do minimapa nos passes de visão). Esmaecido = fora da visão (o anel marca o lugar real); "?" = não deu para decidir.'
    : visaoResp?.disponivel
      ? 'Sem a visão dos times: na tela da partida, use "rastrear a visão dos times".'
      : `Sem a visão dos times${visaoResp?.motivo ? `: ${visaoResp.motivo}` : '.'}`;
  let observador = Number(selVisao.value) || 0;
  const ajuda = el('span', 'sub rp-teclas', 'espaço play/pausa · ← → 5 s · F tela cheia · Esc fecha o painel / sai');
  // Tela cheia de verdade: o MESMO estado do app inteiro (telacheia.js: a da janela no Electron,
  // a mesma do F11 e da barra de título; a da página no navegador). A tecla F quase ninguém achava.
  const botaoCheia = botao('⛶ tela cheia', 'rp-cheia', () => telaCheia.alterna());
  botaoCheia.title = 'Tela cheia (F ou F11)';
  const desligaCheia = telaCheia.aoMudar((ligada) => {
    botaoCheia.textContent = ligada ? '⛶ sair da tela cheia' : '⛶ tela cheia';
    botaoCheia.setAttribute('aria-pressed', String(ligada));
  });
  const topo = el('header', 'rp-topo', [sair, titulo, zonasLab, torresLab, ondasLab, sentLab, visaoLab, etiquetasLab, ajuda, botaoCheia]);

  const palco = el('div', 'rp-palco');
  const mapa = el('div', 'rp-mapa');
  const img = el('img', 'rp-fundo');
  img.src = 'assets/map11.png';
  img.alt = "Mapa de Summoner's Rift";
  const canvas = el('canvas', 'rp-zonas');
  canvas.hidden = true;
  const camadaEv = el('div', 'rp-eventos');
  const camadaTorres = el('div', 'rp-torres');
  camadaTorres.hidden = !cbTorres.checked;
  const torres = ESTRUTURAS.map((s) => {
    const { left, top } = emPorcento(s.x, s.y);
    const d = el('i', `torre ${s.tipo} ${s.time === 200 ? 'vermelho' : 'azul'}`);
    d.style.left = `${left}%`;
    d.style.top = `${top}%`;
    d.title = `${s.tipo === 'torre' ? 'Torre' : 'Inibidor'} do time ${s.time === 200 ? 'vermelho' : 'azul'}`;
    const im = el('img');
    im.src = imagemEstrutura(s, false);
    im.alt = '';
    im.draggable = false;
    d.append(im);
    camadaTorres.append(d);
    return d;
  });
  // Um marcador por rota. A camada fica POR BAIXO dos retratos: os jogadores da rota estão em
  // volta da onda, e o marcador não pode tapá-los.
  const camadaOndas = el('div', 'rp-ondas');
  camadaOndas.hidden = !cbOndas.checked;
  const marcasOnda = new Map();
  for (const r of ['rota_topo', 'rota_meio', 'rota_baixo']) {
    const m = el('div', 'onda-marca', [el('i'), el('span', 'onda-rot')]);
    m.hidden = true;
    camadaOndas.append(m);
    marcasOnda.set(r, m);
  }
  const camadaSent = el('div', 'rp-sentinelas');
  camadaSent.hidden = !cbSent.checked;
  const camadaJog = el('div', 'rp-jogadores');
  // Traços e anéis das chamadas: SVG nas mesmas coordenadas em pixels das figuras, por baixo delas.
  const svgCham = document.createElementNS(SVG, 'svg');
  svgCham.setAttribute('class', 'rp-chamadas');
  svgCham.setAttribute('aria-hidden', 'true');
  const camadaTracos = document.createElementNS(SVG, 'g');
  const camadaAneis = document.createElementNS(SVG, 'g');
  svgCham.append(camadaTracos, camadaAneis);
  camadaJog.append(svgCham);
  const aviso = el('div', 'rp-aviso');
  aviso.hidden = true;
  // Os eventos ficam por cima das figuras: o ✕ do abate cai justo onde está a vítima.
  mapa.append(img, canvas, camadaTorres, camadaSent, camadaOndas, camadaJog, camadaEv, aviso);
  palco.append(mapa);

  const legenda = el('div', 'rp-legenda', [
    el('span', '', [el('i', 'k-exata'), 'rastreado']),
    el('span', '', [el('i', 'k-aprox'), 'posição aproximada (por minuto) ou estimada entre duas leituras']),
    el('span', '', [el('i', 'k-morto'), 'morto']),
  ]);
  if (!rastreados.length) legenda.append(el('span', 'sub', 'ninguém rastreado nesta partida: todos aproximados'));

  const painel = el('aside', 'rp-painel');
  painel.setAttribute('aria-live', 'polite');

  const play = botao('▶', 'rp-play', () => alterna());
  play.setAttribute('aria-label', 'Tocar');
  const relogioEl = el('span', 'num rp-relogio', '0:00');
  const total = el('span', 'num sub', `/ ${duracao(p.duracao_s)}`);
  const trilha = el('div', 'rp-trilha');
  const marcas = el('div', 'rp-marcas');
  const barra = el('input', 'rp-barra');
  barra.type = 'range';
  barra.min = '0';
  barra.max = String(p.duracao_s);
  barra.step = '0.1';
  barra.setAttribute('aria-label', 'Tempo da partida');
  trilha.append(marcas, barra);
  const vels = el('div', 'rp-vels');
  vels.setAttribute('role', 'group');
  vels.setAttribute('aria-label', 'Velocidade');
  const botoesVel = VELOCIDADES.map((v) => {
    const b = botao(`${v}×`, 'mini', () => mudaVelocidade(v));
    b.setAttribute('aria-pressed', 'false');
    vels.append(b);
    return b;
  });
  const status = el('span', 'sub rp-status');
  // A legenda vai na barra de baixo (não numa faixa própria): a altura é toda do mapa.
  const controles = el('footer', 'rp-controles', [play, relogioEl, total, trilha, vels, status, legenda]);

  // Placar dos times no espaço vazio à esquerda do mapa (some em janela estreita).
  const placar = el('aside', 'rp-placar');
  placar.setAttribute('aria-label', 'Placar dos times');
  const corpo = el('div', 'rp-corpo', [placar, palco, painel]);
  const tela = el('div', 'replay', [topo, corpo, controles]);
  tela.tabIndex = -1;
  raiz.replaceChildren(tela);
  tela.focus();
  const aplicaEtiquetas = () => tela.classList.toggle('sem-etiquetas', !cbEtiquetas.checked);
  cbEtiquetas.addEventListener('change', () => {
    guardaPreferencia('replay.etiquetas', cbEtiquetas.checked ? '1' : '0');
    aplicaEtiquetas();
  });
  aplicaEtiquetas();
  cbTorres.addEventListener('change', () => {
    guardaPreferencia('replay.torres', cbTorres.checked ? '1' : '0');
    camadaTorres.hidden = !cbTorres.checked;
  });
  cbSent.addEventListener('change', () => {
    guardaPreferencia('replay.sentinelas', cbSent.checked ? '1' : '0');
    camadaSent.hidden = !cbSent.checked;
  });
  cbOndas.addEventListener('change', () => {
    guardaPreferencia('replay.ondas', cbOndas.checked ? '1' : '0');
    camadaOndas.hidden = !cbOndas.checked;
  });
  selVisao.addEventListener('change', () => {
    guardaPreferencia('replay.visao', selVisao.value);
    observador = Number(selVisao.value) || 0;
    ultimoPainel = 0;
  });

  // Marcas na barra de tempo: abates (cor do time de quem matou) e objetivos.
  for (const e of eventos) {
    if (!['abate', 'monstro', 'estrutura'].includes(e.tipo)) continue;
    const m = el('i', `marca-t ${e.tipo} ${info.porId[e.quem_id]?.time === 200 ? 'vermelho' : 'azul'}`);
    m.style.left = `${(e.t_ms / duracaoMs) * 100}%`;
    m.title = `${relogio(e.t_ms)} · ${eventoTexto(e, nomes)}`;
    marcas.append(m);
  }
  // Viradas de fase (servidor, a mesma regra da atividade): uma marca alta na barra.
  for (const f of fases || []) {
    if (f.fase === 'rotas') continue;
    const m = el('i', `marca-fase ${f.fase}`);
    m.style.left = `${(f.de_ms / duracaoMs) * 100}%`;
    m.title =
      f.fase === 'meio'
        ? `${relogio(f.de_ms)} · fim da fase de rotas: ${f.motivo}`
        : `${relogio(f.de_ms)} · começo do fim de jogo: ${f.motivo}`;
    marcas.append(m);
  }
  const prepPlacar = preparaPlacar({ participantes: p.participantes, eventos, linhaDoTempo });
  const patchJogo = patchDaVersao(p.patch);

  // --- Estado ---
  let t = Math.min(duracaoMs, Math.max(0, (rota.t ?? 0) * 1000));
  let tocando = false;
  let vel = 1;
  let escolhido = null;
  let ultimoQuadro = null;
  let ultimoPainel = 0;
  let ultimoBarra = 0;
  let vivo = true;
  let lado = 600; // lado do mapa em pixels (ajusta)
  let ret = 36; // lado do retrato em pixels
  let antes = performance.now();
  // O arranjo dos retratos (chamadas.js) guarda entre quadros o deslocamento de cada retrato e o
  // lugar dele no grupo; é recriado quando o tamanho do retrato muda (janela redimensionada).
  let arranjo = criaArranjo({ ret });
  let retDoArranjo = ret;
  let reiniciaDesvios = true;
  // Painel do jogador: o que está aberto (decomposição de um atributo, detalhe de um item) fica
  // aqui, fora do DOM, porque o painel é redesenhado enquanto o tempo anda.
  let atrAberto = null;
  let itemAberto = null;
  let assinaturaPainel = '';
  // Dados estáticos da versão da partida (base dos campeões, itens) e o bônus dos dragões medido
  // no degrau: chegam depois, sem segurar a tela; até lá o painel mostra só os números lidos.
  let estaticos = null;
  let bonusMedido = null;
  api
    .estaticos(gameId)
    .then((r) => {
      estaticos = r;
      assinaturaPainel = '';
    })
    .catch(() => {});
  api
    .bonusObjetivos(gameId)
    .then((r) => {
      bonusMedido = r;
      assinaturaPainel = '';
    })
    .catch(() => {});
  let dtTela = 16; // ms de tela desde o quadro anterior (suavização do deslocamento)
  // Jogador destacado (mouse no retrato, no ponto ou na linha da lista).
  let destacado = null;
  // Segura o detalhe da etiqueta (com quem luta) para ela não piscar.
  const rotulador = criaRotulador();

  const janelas = new Janelas({
    duracaoMs,
    carrega: async (de, ate) => {
      const [est, atv] = await Promise.all([
        api.estados(gameId, de / 1000, ate / 1000, PASSO_S),
        api.atividades(gameId, de / 1000, ate / 1000).catch(() => ({ atividades: [] })),
      ]);
      return { quadros: est.quadros, atividades: atv.atividades };
    },
  });

  // Figuras dos 10.
  const figuras = new Map();
  for (const pt of p.participantes) {
    const f = el('button', `figura ${pt.time === 200 ? 'vermelho' : 'azul'}`);
    f.type = 'button';
    f.setAttribute('aria-label', `${pt.campeao} · ${pt.nome}: abrir detalhes`);
    const ret = retrato(pt, 40);
    const nome = el('span', 'f-nome', pt.nome);
    const atv = el('span', 'f-atv');
    const hab = el('span', 'f-hab');
    hab.hidden = true;
    // Ponto da atividade: canto de baixo à esquerda (o balão da habilidade fica em cima à direita).
    const ponto = el('i', 'f-ponto');
    ponto.hidden = true;
    // "?" da caixa visão: não deu para decidir se o time escolhido via este jogador.
    const duvida = el('span', 'f-duvida', '?');
    duvida.hidden = true;
    f.append(el('span', 'f-ret', [ret, hab, ponto, duvida]), nome, atv);
    f.addEventListener('click', (ev) => {
      ev.stopPropagation();
      escolhido = escolhido === pt.participante_id ? null : pt.participante_id;
      ultimoPainel = 0;
      desenhaPainel(true);
    });
    f.addEventListener('mouseenter', () => destaca(pt.participante_id));
    f.addEventListener('mouseleave', () => destaca(null));
    camadaJog.append(f);
    // Anel (o lugar verdadeiro) e traço até o retrato, na camada SVG por baixo dos retratos.
    const cor = pt.time === 200 ? 'vermelho' : 'azul';
    const traco = document.createElementNS(SVG, 'line');
    traco.setAttribute('class', `ch-traco ${cor}`);
    traco.style.display = 'none';
    const anel = document.createElementNS(SVG, 'circle');
    anel.setAttribute('class', `ch-anel ${cor}`);
    anel.setAttribute('r', '4.5');
    anel.style.display = 'none';
    camadaTracos.append(traco);
    camadaAneis.append(anel);
    figuras.set(pt.participante_id, { f, atv, hab, ponto, duvida, anel, traco, pt });
  }

  // destaca: o jogador sob o mouse (retrato, ponto ou linha da lista) fica marcado nos dois lugares.
  function destaca(pid) {
    if (destacado === pid) return;
    destacado = pid;
    for (const [id, fg] of figuras) {
      fg.f.classList.toggle('destacado', id === pid);
      fg.anel.classList.toggle('destacado', id === pid);
    }
    for (const l of painel.querySelectorAll('.rp-linha')) l.classList.toggle('destacado', Number(l.dataset.pid) === pid);
  }

  function mudaVelocidade(v) {
    vel = v;
    botoesVel.forEach((b, i) => {
      b.classList.toggle('on', VELOCIDADES[i] === v);
      b.setAttribute('aria-pressed', String(VELOCIDADES[i] === v));
    });
  }
  mudaVelocidade(1);

  function alterna() {
    if (t >= duracaoMs) t = 0;
    tocando = !tocando;
    play.textContent = tocando ? '❚❚' : '▶';
    play.setAttribute('aria-label', tocando ? 'Pausar' : 'Tocar');
    if (!tocando) ctx.marca({ vista: 'replay', gameId, t: Math.round(t / 100) / 10 });
  }

  function vaiPara(novo) {
    t = Math.min(duracaoMs, Math.max(0, novo));
    ultimoPainel = 0;
    reiniciaDesvios = true; // salto no tempo: as figuras vão direto para o lugar novo
    if (!tocando) ctx.marca({ vista: 'replay', gameId, t: Math.round(t / 100) / 10 });
  }

  barra.addEventListener('input', () => vaiPara(Number(barra.value) * 1000));

  // --- Zonas ---
  let zonasDesenhadas = false;
  function desenhaZonas() {
    if (zonasDesenhadas) return;
    zonasDesenhadas = true;
    canvas.width = grade.colunas;
    canvas.height = grade.linhas.length;
    const c = canvas.getContext('2d');
    const cores = Object.fromEntries(zonas.zonas.map((z) => [z.codigo, z.cor]));
    for (let i = 0; i < grade.linhas.length; i++) {
      const linha = grade.linhas[i];
      for (let j = 0; j < linha.length; j++) {
        const cod = grade.codigos[linha.charCodeAt(j) - 65];
        if (!cod || cod === 'fora_do_mapa') continue;
        c.fillStyle = cores[cod] || '#000';
        c.fillRect(j, grade.linhas.length - 1 - i, 1, 1);
      }
    }
  }
  cbZonas.addEventListener('change', () => {
    if (cbZonas.checked) desenhaZonas();
    canvas.hidden = !cbZonas.checked;
  });

  // --- Eventos no mapa ---
  const evNoMapa = new Map();
  function desenhaEventos() {
    const vis = eventosRecentes(evMapa, t, EVENTO_MS);
    const chaves = new Set();
    for (const e of vis) {
      const k = `e${e.id}`;
      chaves.add(k);
      if (evNoMapa.has(k)) continue;
      const { left, top } = emPorcento(e.x, e.y);
      // Eventos no mesmo lugar (dois abates na mesma luta): o rótulo do seguinte sobe uma linha.
      const vizinhos = vis.filter((o) => o !== e && o.t_ms <= e.t_ms && evNoMapa.has(`e${o.id}`) && Math.hypot(o.x - e.x, o.y - e.y) < 900).length;
      const m = el('div', `ev-mapa ${e.tipo} ${info.porId[e.quem_id]?.time === 200 ? 'vermelho' : 'azul'}`, [
        el('i', '', e.tipo === 'abate' ? '✕' : e.tipo === 'monstro' ? '◆' : '▲'),
        el('span', '', eventoTexto(e, nomes)),
      ]);
      m.style.left = `${left}%`;
      m.style.top = `${top}%`;
      m.style.setProperty('--n', String(vizinhos));
      camadaEv.append(m);
      evNoMapa.set(k, m);
    }
    for (const [k, m] of evNoMapa) {
      if (!chaves.has(k)) {
        m.remove();
        evNoMapa.delete(k);
      }
    }
  }

  // Torres e inibidores: no chão se já houve o evento de derrubada naquele lugar.
  const evEstruturas = eventos.filter((e) => e.tipo === 'estrutura');
  function desenhaTorres() {
    if (camadaTorres.hidden) return;
    estruturasCaidas(evEstruturas, t).forEach((caida, i) => {
      if (torres[i].classList.contains('caida') !== caida) {
        torres[i].classList.toggle('caida', caida);
        torres[i].firstChild.src = imagemEstrutura(ESTRUTURAS[i], caida);
        torres[i].title = torres[i].title.replace(/ \(no chão\)$/, '') + (caida ? ' (no chão)' : '');
      }
    });
  }

  // Ondas: o marcador na posição estimada (sem ponto perto do instante = ninguém da rota lá:
  // o marcador some, não se inventa). Cor pelo estado; confiança média = tracejado.
  function desenhaOndas() {
    if (camadaOndas.hidden || !ondas) return;
    for (const l of ondasNoInstante(ondas, t)) {
      const m = marcasOnda.get(l.rota);
      if (!l.ponto) {
        m.hidden = true;
        continue;
      }
      m.hidden = false;
      const { left, top } = emPorcento(l.ponto.x, l.ponto.y);
      m.style.left = `${left}%`;
      m.style.top = `${top}%`;
      const classe = `onda-marca ${l.trecho?.estado || 'sem-estado'} ${l.trecho?.time === 200 ? 'vermelho' : l.trecho?.time === 100 ? 'azul' : ''} conf-${l.ponto.confianca}`;
      if (m.className !== classe) m.className = classe;
      const rot = textoCurto(l.trecho);
      if (m.lastChild.textContent !== rot) m.lastChild.textContent = rot;
      m.title = `Onda ${l.nome === 'baixo' ? 'de' : 'do'} ${l.nome} · ${textoOnda(l.onda)} · ${textoEstado(l.trecho)} · confiança ${l.ponto.confianca === 'alta' ? 'alta (os dois lados na rota)' : 'média (um lado só)'} · estimada pela posição dos jogadores, sem contar tropas`;
    }
  }

  // Sentinelas vivas: uma marca por sentinela (criada uma vez, mostrada/escondida conforme o tempo).
  const marcasSent = new Map();
  let assinaturaSent = '';
  function desenhaSentinelas() {
    if (camadaSent.hidden || !temSentinelas) return;
    if (sentLidas) {
      desenhaSentinelasLidas();
      return;
    }
    const vivas = noMapa(sentinelas.sentinelas, t);
    const ass = vivas.map((s) => s.t_ms + ':' + s.participante_id).join(',');
    if (ass === assinaturaSent) return;
    assinaturaSent = ass;
    const ficam = new Set();
    for (const s of vivas) {
      const k = `${s.participante_id}:${s.t_ms}`;
      ficam.add(k);
      if (marcasSent.has(k)) continue;
      const { left, top } = emPorcento(s.x, s.y);
      const diam = ((2 * (s.raio || 0)) / 14990) * 100;
      const cor = s.time === 200 ? 'vermelho' : 'azul';
      const m = el('div', `sent ${cor}${s.tipo === 'CONTROL_WARD' ? ' controle' : ''}${s.comum ? '' : ' desconhecida'}`, [el('i', 'sent-raio'), el('i', 'sent-ponto')]);
      m.style.left = `${left}%`;
      m.style.top = `${top}%`;
      m.style.width = `${diam}%`;
      m.style.height = `${diam}%`;
      m.lastChild.title = tituloSentinela(s, nomes);
      camadaSent.append(m);
      marcasSent.set(k, m);
    }
    for (const [k, m] of marcasSent) {
      if (!ficam.has(k)) {
        m.remove();
        marcasSent.delete(k);
      }
    }
  }

  // Sentinelas lidas do minimapa (passes de visão): lugar exato, sem o círculo da incerteza.
  function desenhaSentinelasLidas() {
    const vivas = sentinelasLidasEm(visaoDados, t);
    const ass = vivas.map((s) => `${s.time}:${s.de_ms}:${s.x}`).join(',');
    if (ass === assinaturaSent) return;
    assinaturaSent = ass;
    const ficam = new Set();
    for (const s of vivas) {
      const k = `lida:${s.time}:${s.de_ms}:${s.x}:${s.y}`;
      ficam.add(k);
      if (marcasSent.has(k)) continue;
      const { left, top } = emPorcento(s.x, s.y);
      const m = el('div', `sent lida ${s.time === 200 ? 'vermelho' : 'azul'}`, [el('i', 'sent-ponto')]);
      m.style.left = `${left}%`;
      m.style.top = `${top}%`;
      m.lastChild.title = tituloSentinelaLida(s);
      camadaSent.append(m);
      marcasSent.set(k, m);
    }
    for (const [k, m] of marcasSent) {
      if (!ficam.has(k)) {
        m.remove();
        marcasSent.delete(k);
      }
    }
  }

  // Balão da habilidade usada: a última de cada rastreado nos últimos HABILIDADE_MS.
  function desenhaHabilidades() {
    const ate = achaIndice(habilidades, t);
    const ultima = new Map();
    for (let i = ate; i >= 0; i--) {
      const h = habilidades[i];
      if (t - h.t_ms > HABILIDADE_MS) break;
      if (!ultima.has(h.participante_id)) ultima.set(h.participante_id, h);
    }
    for (const [pid, fg] of figuras) {
      const h = ultima.get(pid);
      fg.hab.hidden = !h;
      if (h) fg.hab.textContent = String(h.dados?.habilidade || '').toUpperCase();
    }
  }

  // --- Quadro ---
  function desenhaQuadro() {
    const dados = janelas.dadosEm(t);
    if (!dados) {
      const est = janelas.estadoEm(t);
      aviso.hidden = false;
      aviso.textContent =
        est === 'erro' ? `Não deu para carregar este trecho: ${janelas.erroEm(t)?.message || 'erro'}. Arraste a barra para tentar de novo.` : 'Carregando este trecho…';
      status.textContent = 'carregando…';
      return false;
    }
    aviso.hidden = true;
    status.textContent = '';
    const q = quadroNoInstante(dados.quadros, t);
    if (!q) return true;
    ultimoQuadro = { dados, q };
    // Onde desenhar (chamadas.js, a mesma função que o validador do desenho usa): sozinho, o
    // retrato fica em cima do lugar verdadeiro; quando retratos colidiriam, cada um ganha um ANEL
    // no lugar exato e o retrato vai para fora do grupo, ligado ao anel por um traço. O morto fica
    // fora da conta: parado onde morreu, por baixo dos vivos.
    const vivos = q.jogadores.filter((j) => j.x !== null && j.x !== undefined && !j.morto);
    const px = vivos.map((j) => {
      const p = paraTela(j.x, j.y, lado);
      return { id: j.participante_id, x: p.px, y: p.py, time: info.porId[j.participante_id]?.time || 100 };
    });
    const desenho = arranjo.passo(px, dtTela, { lado, reinicia: reiniciaDesvios, velocidade: tocando ? vel : 1 });
    reiniciaDesvios = false;
    const etiquetasLigadas = cbEtiquetas.checked;
    // Visão do time escolhido no instante: participante → visivel | fora | indeterminado | ''.
    const estVisao = observador ? estadosNoInstante(visaoDados, observador, t) : null;
    for (const j of q.jogadores) {
      const fg = figuras.get(j.participante_id);
      if (!fg) continue;
      const ev = (!j.morto && estVisao?.get(j.participante_id)) || '';
      fg.f.classList.toggle('fora-da-visao', ev === 'fora');
      fg.f.classList.toggle('visao-incerta', ev === 'indeterminado');
      fg.duvida.hidden = ev !== 'indeterminado';
      if (fg.ev !== ev) {
        fg.ev = ev;
        fg.duvida.title = tituloEstado(ev, observador);
      }
      if (j.x === null || j.x === undefined) {
        fg.f.hidden = true;
        fg.anel.style.display = 'none';
        fg.traco.style.display = 'none';
        continue;
      }
      fg.f.hidden = false;
      let d = desenho.get(j.participante_id);
      if (!d) {
        const p = paraTela(j.x, j.y, lado);
        d = { ax: p.px, ay: p.py, rx: p.px, ry: p.py, escala: 1, traco: 0 };
      }
      fg.f.style.left = `${d.rx}px`;
      fg.f.style.top = `${d.ry}px`;
      fg.f.style.setProperty('--ret', `${Math.round(ret * d.escala * 10) / 10}px`);
      // Anel e traço: a verdade. Aparecem junto com o deslocamento (transição contínua). Fora da
      // visão do time escolhido, o anel fica sempre (o retrato esmaece, o lugar real continua).
      const foraVisao = ev === 'fora';
      const mostra = !j.morto && d.traco > 0.02;
      fg.anel.style.display = mostra || foraVisao ? '' : 'none';
      fg.traco.style.display = mostra ? '' : 'none';
      fg.anel.classList.toggle('fora-da-visao', foraVisao);
      if (mostra || foraVisao) {
        fg.anel.setAttribute('cx', d.ax.toFixed(1));
        fg.anel.setAttribute('cy', d.ay.toFixed(1));
        fg.anel.style.opacity = String(foraVisao ? Math.max(0.9, d.traco) : d.traco);
      }
      if (mostra) {
        fg.traco.setAttribute('x1', d.ax.toFixed(1));
        fg.traco.setAttribute('y1', d.ay.toFixed(1));
        fg.traco.setAttribute('x2', d.rx.toFixed(1));
        fg.traco.setAttribute('y2', d.ry.toFixed(1));
        fg.traco.style.opacity = String(d.traco * 0.9);
      }
      // Estimada entre duas leituras (buraco curto, servidor: rastreio/consulta.go): o mesmo
      // estilo do aproximado; a legenda e o painel dizem de onde veio.
      const aprox = !j.rastreado || j.origem === 'linha_do_tempo' || j.origem === 'interpolada';
      fg.f.classList.toggle('aprox', aprox);
      fg.f.classList.toggle('morto', Boolean(j.morto));
      fg.f.classList.toggle('escolhido', escolhido === j.participante_id);
      fg.f.classList.toggle('destacado', destacado === j.participante_id);
      fg.f.classList.toggle('em-grupo', !j.morto && (d.n || 1) >= 2);
      fg.anel.classList.toggle('destacado', destacado === j.participante_id);
      // Uma etiqueta por jogador, embaixo do retrato. Sem as piscadas de 1 s do dado
      // (atividadeEstavel); encurtada quando o texto inteiro é comprido.
      // A mesma regra do validador (valida-desenho.js): etiqueta + segurar o detalhe 1,5 s.
      const et = etiquetaDoJogador(j, dados.atividades, t, nomes);
      const tr = et.tr;
      const texto = rotulador(j.participante_id, textoNoMapa(et, j.morto ? 1 : d.n || 1), et.chave, t);
      const inteiro = et.inteiro;
      if (fg.atv.textContent !== texto) {
        fg.atv.textContent = texto;
        fg.f.title = `${fg.pt.campeao} · ${fg.pt.nome}${inteiro ? ` · ${inteiro}` : ''}`;
      }
      fg.atv.className = `f-atv ${j.morto ? 'morto' : tr?.atividade || ''}`;
      fg.atv.hidden = !texto;
      // O ponto da cor da atividade: o texto inteiro NÃO está escrito no mapa (encurtado ou
      // com as etiquetas desligadas). Passar o mouse destaca a linha da lista.
      const temPonto = Boolean(tr) && (!etiquetasLigadas || texto !== inteiro);
      fg.ponto.hidden = !temPonto;
      if (temPonto) {
        fg.ponto.className = `f-ponto ${tr.atividade}`;
        if (fg.ponto.title !== inteiro) fg.ponto.title = inteiro;
      }
    }
    return true;
  }

  // --- Painel do jogador ---
  function desenhaPainel(forca = false) {
    const agora = performance.now();
    if (!forca && agora - ultimoPainel < 250) return;
    ultimoPainel = agora;
    // Nada mudou (pausado, mesmo quadro): não redesenha, para o mouse em cima de um item ou de
    // um ícone não perder a dica a cada 250 ms.
    const assinatura = [escolhido, Math.round(t), ultimoQuadro?.q?.t_ms, atrAberto, itemAberto, Boolean(estaticos), Boolean(bonusMedido), Boolean(ultimoQuadro?.dados), observador].join('|');
    if (!forca && assinatura === assinaturaPainel) return;
    assinaturaPainel = assinatura;
    painel.classList.toggle('lista', !escolhido);
    if (!escolhido) {
      desenhaLista();
      return;
    }
    const pt = info.porId[escolhido];
    const dados = ultimoQuadro?.dados;
    // Números do último quadro do servidor (o fato), não do interpolado.
    let j = null;
    if (dados) {
      const i = Math.max(0, achaIndice(dados.quadros, t));
      j = dados.quadros[i]?.jogadores.find((x) => x.participante_id === escolhido) || null;
    }
    const fechar = botao('✕', 'icone', () => {
      escolhido = null;
      desenhaPainel(true);
    });
    fechar.setAttribute('aria-label', 'Fechar o painel');
    const cab = el('div', 'rp-p-cab', [
      retrato(pt, 44),
      el('div', '', [el('b', pt.time === 200 ? 'tm-v' : 'tm-a', pt.campeao), el('div', 'sub', `${pt.nome}${pt.tag ? `#${pt.tag}` : ''}`)]),
      fechar,
    ]);
    const blocos = [cab];
    if (!j) {
      blocos.push(el('p', 'sub', 'Sem dados neste instante.'));
      painel.replaceChildren(...blocos);
      return;
    }
    const rastreado = j.rastreado;
    blocos.push(el('p', 'sub', `${relogio(t)} · ${rastreado ? 'rastreado' : 'não rastreado'} · posição ${origemPosicao(j.origem)}`));

    // Vida e mana: número do painel (rastreado) ou % das barras laterais.
    const barraV = (rot, atual, max, pct, classe) => {
      const temNum = atual !== null && atual !== undefined && max;
      const fr = temNum ? atual / max : pct !== null && pct !== undefined ? pct / 100 : null;
      const b = el('div', `rp-barra-v ${classe}`);
      const i = el('b');
      i.style.width = `${fr === null ? 0 : Math.round(Math.min(1, Math.max(0, fr)) * 100)}%`;
      b.append(i);
      const txt = temNum ? `${numero(Math.round(atual))} / ${numero(max)}` : fr !== null ? `${Math.round(fr * 100)}%` : 'não lida';
      return el('div', 'rp-vm', [el('span', '', rot), b, el('span', 'num', txt)]);
    };
    blocos.push(
      j.morto
        ? el('p', 'tx-cr', `Morto${j.respawn ? ` · volta em ${Math.ceil(j.respawn)} s` : ''}`)
        : el('div', '', [barraV('Vida', j.vida, j.vida_max, j.vida_pct, 'vida'), barraV('Mana', j.mana, j.mana_max, j.mana_pct, 'mana')]),
    );

    // Q W E R e feitiços.
    const cd = j.cd || {};
    const habs = el('div', 'rp-habs');
    for (const k of ['q', 'w', 'e', 'r', 'd', 'f']) {
      const rc = recarga(cd[k]);
      const rot = k === 'd' ? nomeFeitico(pt.feitico1) : k === 'f' ? nomeFeitico(pt.feitico2) : k.toUpperCase();
      const c = el('div', `rp-hab ${rc.classe}`, [el('b', '', k === 'd' || k === 'f' ? k.toUpperCase() : rot), el('span', '', rc.texto)]);
      c.title = `${rot}: ${rc.titulo}`;
      if (k === 'd' || k === 'f') c.append(el('small', '', rot));
      habs.append(c);
    }
    // Passiva: só aparece quando está em recarga (Vex, Garen...); na maioria
    // dos campeões ela nunca entra em recarga e seria só ruído.
    if (typeof cd.p === 'number' && cd.p > 0) {
      const rc = recarga(cd.p);
      const c = el('div', `rp-hab ${rc.classe}`, [el('b', '', 'P'), el('span', '', rc.texto), el('small', '', 'Passiva')]);
      c.title = `Passiva: ${rc.titulo}`;
      habs.prepend(c);
    }
    blocos.push(el('h3', '', 'Habilidades e feitiços'), habs);
    if (!rastreado) blocos.push(el('p', 'sub', 'Recargas, vida em número e atributos só existem para quem foi rastreado.'));

    // Nível, K/D/A, CS, zona.
    const zona = nomeZona(zonaNaGrade(grade, j.x, j.y));
    const kv = el('div', 'kv compacto', [
      el('span', '', 'Nível'),
      el('b', '', String(j.nivel ?? '—')),
      el('span', '', 'K/D/A'),
      el('b', '', `${j.abates ?? 0}/${j.mortes ?? 0}/${j.assistencias ?? 0}`),
      el('span', '', 'CS'),
      el('b', '', String(j.cs ?? '—')),
      el('span', '', 'Zona'),
      el('b', '', zona || '—'),
    ]);
    blocos.push(kv);

    const at = j.atributos || {};
    if (rastreado && Object.values(at).some((v) => v !== null && v !== undefined)) {
      blocos.push(el('h3', '', 'Atributos'), blocoAtributos(pt, j));
    }

    blocos.push(el('h3', '', 'Itens'), ...blocoItens(pt, j));
    blocos.push(...blocoObjetivos(pt, j));

    const tr = rastreado ? atividadeNoInstante(ultimoQuadro.dados.atividades, escolhido, t) : null;
    if (tr) {
      blocos.push(
        el('h3', '', 'Atividade (deduzida)'),
        el('p', '', `${atividadeTexto(tr, nomes)} · confiança ${tr.confianca}`),
        el('ul', 'sinais', (tr.sinais || []).map((s) => el('li', 'sub', s))),
      );
    }
    painel.replaceChildren(...blocos);
  }

  // Objetivos do time do jogador no instante: placar (dragões) e o estado do Barão/Ancião.
  function objetivosDoTime(pt) {
    const pl = placarNoInstante(prepPlacar, t)[pt.time];
    const ultimo = (tipo) => {
      let r = null;
      for (const o of prepPlacar.objetivos) if (o.t_ms <= t && o.time === pt.time && o.tipo === tipo) r = o.t_ms;
      return r;
    };
    const tBarao = ultimo('barao');
    const tAnciao = ultimo('anciao');
    return {
      pl,
      tBarao,
      estadoBarao: tBarao === null ? null : bonusComPrazo(prepPlacar, pt.participante_id, tBarao, DURACAO_BARAO_MS, t),
      estadoAnciao: tAnciao === null ? null : bonusComPrazo(prepPlacar, pt.participante_id, tAnciao, DURACAO_ANCIAO_MS, t),
    };
  }

  // Atributos: um por linha; os que têm bônus de objetivo do time ganham o ícone do dragão (a
  // dica diz quanto do número vem dele). Clicar abre a decomposição: base do campeão no nível +
  // itens + objetivos + "runas e efeitos temporários" (a sobra), ou "não explicado" se a conta
  // passar do lido.
  function blocoAtributos(pt, j) {
    const at = j.atributos || {};
    const obj = objetivosDoTime(pt);
    // Só o Barão certo entra na conta ('incerto' = morreu perto do abate: pode não ter recebido).
    const barao = obj.estadoBarao === 'ativo' ? baraoNoMinuto(obj.tBarao) : null;
    const c = estaticos?.campeoes?.[pt.participante_id];
    const ids = idsDoInventario(j.itens);
    const caixa = el('div', 'rp-atr');
    for (const a of ATRIBUTOS) {
      const lido = at[a.chave];
      const d =
        c && lido !== null && lido !== undefined
          ? decompoe({ atr: a.chave, lido, c, nivel: j.nivel || 1, ids, itens: estaticos.itens, dragoes: obj.pl.dragoes, resumo: bonusMedido?.resumo, barao })
          : null;
      const icones = el('span', 'rp-atr-icones');
      const dica = textoObjetivo(a.nome, d);
      for (const p of d?.partes.filter((x) => x.objetivo && (x.valor > 0 || x.ativo === false)) || []) {
        const i = el('img', `rp-atr-icone${p.ativo === false ? ' inativo' : ''}`);
        i.src = iconeObjetivo(p.sub === 'BARON' ? 'barao' : 'dragao', p.sub);
        i.alt = p.rotulo;
        i.draggable = false;
        icones.append(i);
      }
      const linha = el('button', `rp-atr-linha${atrAberto === a.chave ? ' aberto' : ''}`, [el('span', 'rp-atr-nome', a.nome), icones, el('b', 'num', numero(lido, a.casas))]);
      linha.type = 'button';
      linha.title = dica ? `${dica}\n(clique para a conta inteira)` : d ? 'Clique para ver de onde vem o número' : estaticos ? '' : 'Sem os dados da versão da partida (sem rede na primeira vez?)';
      linha.setAttribute('aria-expanded', String(atrAberto === a.chave));
      linha.disabled = !d;
      linha.addEventListener('click', () => {
        atrAberto = atrAberto === a.chave ? null : a.chave;
        desenhaPainel(true);
      });
      caixa.append(linha);
      if (atrAberto === a.chave && d) caixa.append(decomposicao(d));
    }
    if (!estaticos) caixa.append(el('p', 'sub', 'Sem os dados da versão da partida: sem a decomposição.'));
    return caixa;
  }

  function decomposicao(d) {
    const f = (v) => formataAtributo(v, d.casas);
    const linhas = el('div', 'rp-decomp');
    for (const p of d.partes) {
      const r = el('div', `rp-dc${p.objetivo ? ' obj' : ''}`, [el('span', '', p.rotulo), el('b', 'num', `${p.valor >= 0 ? '+' : ''}${f(p.valor)}`)]);
      if (p.detalhe) r.append(el('small', 'sub', p.detalhe));
      linhas.append(r);
    }
    if (!d.nao_explicado) {
      const r = el('div', 'rp-dc sobra', [el('span', '', 'runas e efeitos temporários'), el('b', 'num', `+${f(d.sobra)}`)]);
      r.append(el('small', 'sub', 'o que sobra: runas, fragmentos, acúmulos, elixires, passivas de item e de campeão que dão atributo conforme a situação'));
      linhas.append(r);
    } else {
      const r = el('div', 'rp-dc nao-expl', [el('span', '', 'não explicado'), el('b', 'num', `−${f(d.nao_explicado)}`)]);
      r.append(el('small', 'sub', 'a conta passa do número lido: redução vinda de inimigo, dado da versão diferente do jogo ou leitura errada'));
      linhas.append(r);
    }
    linhas.append(el('div', 'rp-dc total', [el('span', '', '= lido da tela'), el('b', 'num', f(d.lido))]));
    for (const av of d.avisos) linhas.append(el('small', 'sub', av));
    linhas.append(el('small', 'sub', `Base e itens: dados do jogo da versão ${estaticos?.versao_dados || '?'} (Data Dragon e CommunityDragon).`));
    return linhas;
  }

  // Itens: passar o mouse mostra nome, custo e atributos; clicar fixa o detalhe embaixo (com o
  // texto dos efeitos). Embaixo dos ícones, o ouro nos itens e a diferença para o adversário de rota.
  function blocoItens(pt, j) {
    const itens = (j.itens || []).filter((x) => x && x.id);
    const caixaItens = el('div', 'rp-itens');
    const est = estaticos?.itens || {};
    for (const it of itens) {
      const dado = est[it.id];
      const b = el('button', `rp-item${itemAberto === it.id ? ' aberto' : ''}`);
      b.type = 'button';
      const im = el('img');
      im.src = imagemItem(it.id);
      im.alt = dado?.nome || `item ${it.id}`;
      im.addEventListener('error', () => im.replaceWith(el('span', 'item-vazio', String(it.id))));
      b.append(im);
      const qtd = it.qtd > 1 ? ` × ${it.qtd}` : '';
      b.title = dado
        ? `${dado.nome}${qtd} · ${numero(dado.custo, 0)} de ouro${dado.linhas?.length ? `\n${dado.linhas.join('\n')}` : ''}\n(clique para os efeitos)`
        : `item ${it.id}${qtd}`;
      b.setAttribute('aria-pressed', String(itemAberto === it.id));
      b.addEventListener('click', () => {
        itemAberto = itemAberto === it.id ? null : it.id;
        desenhaPainel(true);
      });
      caixaItens.append(b);
    }
    if (!itens.length) caixaItens.append(el('span', 'sub', 'sem itens'));
    const saida = [caixaItens];
    if (estaticos) {
      const ouro = ouroDosItens(idsDoInventario(j.itens), est);
      const linha = el('p', 'rp-ouro-itens', [el('span', 'sub', 'Ouro nos itens '), el('b', 'num', numero(ouro, 0))]);
      const adv = adversarioDeRota(p.participantes, pt.participante_id);
      const dados = ultimoQuadro?.dados;
      const q = dados ? dados.quadros[Math.max(0, achaIndice(dados.quadros, t))] : null;
      const ja = adv && q?.jogadores.find((x) => x.participante_id === adv.participante_id);
      if (ja) {
        const dif = ouro - ouroDosItens(idsDoInventario(ja.itens), est);
        linha.append(
          el('span', 'sub', ` · contra ${adv.campeao} `),
          el('b', `num ${dif > 0 ? 'tx-ok' : dif < 0 ? 'tx-cr' : ''}`, `${dif > 0 ? '+' : dif < 0 ? '−' : '±'}${numero(Math.abs(dif), 0)}`),
        );
        linha.title = `Custo somado dos itens no inventário agora, contra o adversário direto de rota (${adv.campeao}, mesma posição).`;
      }
      saida.push(linha);
    }
    if (itemAberto && !itens.some((x) => x.id === itemAberto)) itemAberto = null;
    const aberto = itemAberto ? est[itemAberto] : null;
    if (aberto) {
      const det = el('div', 'rp-item-det', [el('div', 'rp-item-det-cab', [el('b', '', aberto.nome), el('span', 'num sub', `${numero(aberto.custo, 0)} de ouro`)])]);
      if (aberto.linhas?.length) det.append(el('ul', 'rp-item-linhas', aberto.linhas.map((l) => el('li', '', l))));
      det.append(aberto.descricao ? el('p', 'rp-item-desc', aberto.descricao) : el('p', 'sub', 'Sem efeito além dos atributos.'));
      det.append(el('small', 'sub', 'Texto do jogo (Data Dragon): os números que dependem de nível ou de atributo não vêm no texto.'));
      saida.push(det);
    }
    return saida;
  }

  // "Objetivos do time": cada bônus que o time do jogador tem agora, o que ele dá e quanto
  // isso é para ESTE jogador, pelos atributos lidos da tela (que já incluem o bônus: a parte
  // de um bônus de p% no valor atual é atual × p/(1+p)). Valores da tabela de bonus.js.
  function blocoObjetivos(pt, j) {
    const obj = objetivosDoTime(pt);
    const blocos = bonusDoJogador({
      j,
      placarTime: obj.pl,
      tBarao: obj.tBarao,
      estadoBarao: obj.estadoBarao,
      estadoAnciao: obj.estadoAnciao,
      medidos: bonusMedido?.resumo,
    });
    const fonte =
      patchJogo === PATCH_DOS_BONUS
        ? `Valores do patch ${PATCH_DOS_BONUS} (League of Legends Wiki e notas de patch da Riot, conferidos em 17/11/2026). Números do jogador são estimativa.`
        : `Valores do patch ${PATCH_DOS_BONUS}; esta partida é do ${patchJogo || '?'}: podem ter mudado. Números do jogador são estimativa.`;
    const saida = [el('h3', '', 'Objetivos do time')];
    if (!blocos.length) {
      saida.push(el('p', 'sub', 'O time não tem nenhum bônus de objetivo neste instante.'));
      return saida;
    }
    for (const b of blocos) {
      const cx = el('div', 'rp-bonus', [el('b', '', b.titulo), el('span', '', b.efeito)]);
      for (const i of b.impacto) cx.append(el('span', `rp-bonus-imp${i.exato ? ' exato' : ''}`, `para ele: ${i.texto}`));
      if (b.aviso) cx.append(el('span', 'sub', b.aviso));
      saida.push(cx);
    }
    if (!j.rastreado) saida.push(el('p', 'sub', 'Sem atributos lidos (não rastreado): só o que o bônus dá.'));
    saida.push(el('p', 'sub rp-bonus-fonte', fonte));
    return saida;
  }

  // Sem ninguém escolhido, o painel mostra os 10: o que cada um está fazendo,
  // vida, nível e placar. É o lugar das informações que não cabem no mapa.
  function desenhaLista() {
    const dados = ultimoQuadro?.dados;
    const blocos = [el('p', 'sub', `${relogio(t)} · clique num jogador para ver habilidades, atributos e itens`)];
    if (!dados) {
      painel.replaceChildren(...blocos, el('p', 'sub', 'Carregando…'));
      return;
    }
    const q = dados.quadros[Math.max(0, achaIndice(dados.quadros, t))];
    const estVisao = observador ? estadosNoInstante(visaoDados, observador, t) : null;
    if (observador) blocos.push(el('p', 'sub', `visão do time ${observador === 200 ? 'vermelho' : 'azul'}: quem do outro time ele via agora`));
    for (const time of [100, 200]) {
      blocos.push(el('h3', time === 200 ? 'tm-v' : 'tm-a', time === 200 ? 'Time vermelho' : 'Time azul'));
      for (const pt of p.participantes.filter((x) => x.time === time)) {
        const j = q?.jogadores.find((x) => x.participante_id === pt.participante_id);
        const tr = j?.rastreado ? atividadeNoInstante(dados.atividades, pt.participante_id, t) : null;
        const texto = !j ? 'sem dados' : j.morto ? `morto${j.respawn ? ` · ${Math.ceil(j.respawn)} s` : ''}` : tr ? atividadeTexto(tr, nomes) : j.rastreado ? '—' : 'não rastreado';
        const fr = !j || j.morto ? 0 : j.vida !== null && j.vida !== undefined && j.vida_max ? j.vida / j.vida_max : j.vida_pct !== null && j.vida_pct !== undefined ? j.vida_pct / 100 : null;
        const vida = el('div', 'rp-barra-v vida');
        const cheio = el('b');
        cheio.style.width = `${fr === null ? 0 : Math.round(Math.min(1, Math.max(0, fr)) * 100)}%`;
        vida.append(cheio);
        // Caixa visão: "à vista" / "fora da visão" / "visão incerta" para os inimigos do time escolhido.
        const ev = (!j?.morto && estVisao?.get(pt.participante_id)) || '';
        const tagVisao = ev ? el('span', `rp-visao ${ev}`, TEXTO_ESTADO[ev]) : null;
        if (tagVisao) tagVisao.title = tituloEstado(ev, observador);
        const linha = el('button', `rp-linha${j?.morto ? ' morto' : ''}${ev === 'fora' ? ' fora-da-visao' : ''}`, [
          retrato(pt, 30),
          el('div', 'rp-l-meio', [
            el('div', 'rp-l-nome', [el('b', '', pt.campeao), el('span', 'sub', pt.nome), ...(tagVisao ? [tagVisao] : [])]),
            el('span', `f-atv ${j?.morto ? 'morto' : tr?.atividade || ''}`, texto),
            vida,
          ]),
          el('div', 'rp-l-num num', [el('b', '', `nv ${j?.nivel ?? '—'}`), el('span', 'sub', `${j?.abates ?? 0}/${j?.mortes ?? 0}/${j?.assistencias ?? 0}`)]),
        ]);
        linha.type = 'button';
        // Mouse na linha destaca o retrato e o anel no mapa (e vice-versa).
        linha.dataset.pid = String(pt.participante_id);
        linha.classList.toggle('destacado', destacado === pt.participante_id);
        linha.addEventListener('mouseenter', () => destaca(pt.participante_id));
        linha.addEventListener('mouseleave', () => destaca(null));
        linha.addEventListener('click', () => {
          escolhido = pt.participante_id;
          desenhaPainel(true);
        });
        blocos.push(linha);
      }
    }
    painel.replaceChildren(...blocos);
  }

  // --- Placar dos times ---
  const mil = (v) => (v === null || v === undefined ? '—' : `${numero(v / 1000)} mil`);
  const temAtakhan = prepPlacar.objetivos.some((o) => o.tipo === 'atakhan');
  // icone: o ícone de um objetivo (assets/objetivos), com o nome no título e no texto alternativo.
  const icone = (tipo, sub, titulo, classe = '') => {
    const i = el('img', `pl-icone ${classe}`);
    i.src = iconeObjetivo(tipo, sub);
    i.alt = titulo;
    i.title = titulo;
    i.draggable = false;
    return i;
  };
  const graficos = montaGraficos();

  // Gráficos do placar, à mão em SVG (sem biblioteca: CSP 'self'): a diferença de ouro por minuto
  // (área azul acima de zero, vermelha abaixo; marcas de objetivo e torre no eixo) e os abates
  // de cada time em degraus. A marca do play anda; o valor do instante sob o mouse vai no título.
  function montaGraficos() {
    const L = 240;
    const H = 64;
    const HA = 40;
    const svgEl = (tag, attrs = {}) => {
      const e = document.createElementNS(SVG, tag);
      for (const [k, v] of Object.entries(attrs)) e.setAttribute(k, String(v));
      return e;
    };
    const serie = serieOuro(prepPlacar);
    const max = escalaOuro(serie);
    const c = caminhosDiferenca(serie, duracaoMs, L, H, max);
    const g1 = svgEl('svg', { viewBox: `0 0 ${L} ${H + 8}`, class: 'pl-graf', preserveAspectRatio: 'none', role: 'img', 'aria-label': 'Diferença de ouro ao longo da partida' });
    g1.append(
      svgEl('line', { x1: 0, x2: L, y1: c.zero, y2: c.zero, class: 'pg-zero' }),
      svgEl('line', { x1: 0, x2: L, y1: 1, y2: 1, class: 'pg-grade' }),
      svgEl('line', { x1: 0, x2: L, y1: H - 1, y2: H - 1, class: 'pg-grade' }),
      svgEl('path', { d: c.acima, class: 'pg-azul' }),
      svgEl('path', { d: c.abaixo, class: 'pg-vermelho' }),
      svgEl('path', { d: c.linha, class: 'pg-linha' }),
    );
    for (const m of marcasDoEixo(prepPlacar)) {
      const x = (m.t_ms / Math.max(1, duracaoMs)) * L;
      const grande = m.tipo !== 'torre';
      const r = svgEl('rect', { x: x - (grande ? 1.5 : 0.75), y: H + 1, width: grande ? 3 : 1.5, height: grande ? 6 : 4, class: `pg-marca ${m.time === 200 ? 'vermelho' : m.time === 100 ? 'azul' : ''}` });
      const tt = svgEl('title');
      tt.textContent = `${relogio(m.t_ms)} · ${m.tipo === 'dragao' ? `dragão ${DRAGOES[m.sub]?.nome || ''}` : m.tipo} · ${m.time === 200 ? 'vermelho' : m.time === 100 ? 'azul' : '?'}`;
      r.append(tt);
      g1.append(r);
    }
    const play1 = svgEl('line', { x1: 0, x2: 0, y1: 0, y2: H, class: 'pg-play' });
    g1.append(play1);
    const ab = serieAbates(prepPlacar);
    const maxAb = Math.max(1, ...ab.map((s) => Math.max(s[100], s[200])));
    const g2 = svgEl('svg', { viewBox: `0 0 ${L} ${HA}`, class: 'pl-graf abates', preserveAspectRatio: 'none', role: 'img', 'aria-label': 'Abates de cada time ao longo da partida' });
    g2.append(
      svgEl('line', { x1: 0, x2: L, y1: HA - 0.5, y2: HA - 0.5, class: 'pg-grade' }),
      svgEl('path', { d: caminhoDegraus(ab, 100, duracaoMs, L, HA - 2, maxAb), class: 'pg-deg azul', transform: 'translate(0,1)' }),
      svgEl('path', { d: caminhoDegraus(ab, 200, duracaoMs, L, HA - 2, maxAb), class: 'pg-deg vermelho', transform: 'translate(0,1)' }),
    );
    const play2 = svgEl('line', { x1: 0, x2: 0, y1: 0, y2: HA, class: 'pg-play' });
    g2.append(play2);
    const valor = el('span', 'sub num pl-graf-valor');
    const titulo = (ev, svg) => {
      const r = svg.getBoundingClientRect();
      const tt = ((ev.clientX - r.left) / Math.max(1, r.width)) * duracaoMs;
      const o = ouroNoInstante(prepPlacar.ouro, tt);
      const k = [...ab].reverse().find((s) => s.t_ms <= tt) || ab[0];
      const dif = o ? o[100] - o[200] : null;
      const txt = `${relogio(tt)} · ${dif === null ? 'ouro —' : Math.abs(dif) < 50 ? 'ouro empatado' : `${dif > 0 ? 'azul' : 'vermelho'} +${numero(Math.abs(dif) / 1000)} mil`} · abates ${k[100]} × ${k[200]}`;
      svg.setAttribute('aria-description', txt);
      valor.textContent = txt;
    };
    for (const s of [g1, g2]) {
      s.addEventListener('mousemove', (ev) => titulo(ev, s));
      s.addEventListener('mouseleave', () => (valor.textContent = ''));
    }
    const caixa = el('div', 'pl-graficos', [
      el('div', 'pl-graf-cab', [el('b', '', 'Diferença de ouro'), el('span', 'sub num', `±${numero(max / 1000)} mil`)]),
      g1,
      el('div', 'pl-graf-cab', [el('b', '', 'Abates por time'), el('span', 'sub num', `até ${maxAb}`)]),
      g2,
      valor,
    ]);
    return {
      el: caixa,
      atualiza(tt) {
        const x = ((tt / Math.max(1, duracaoMs)) * L).toFixed(1);
        for (const pl of [play1, play2]) {
          pl.setAttribute('x1', x);
          pl.setAttribute('x2', x);
        }
      },
    };
  }
  let ultimoPlacar = 0;
  function desenhaPlacar(forca = false) {
    const agora = performance.now();
    // Escondido pelo CSS (janela estreita): não monta nada.
    if ((!forca && agora - ultimoPlacar < 250) || !placar.getClientRects().length) return;
    ultimoPlacar = agora;
    const dados = ultimoQuadro?.dados;
    const q = dados ? dados.quadros[Math.max(0, achaIndice(dados.quadros, t))] : null;
    const pl = placarNoInstante(prepPlacar, t, q?.jogadores);
    const a = pl[100];
    const v = pl[200];
    const fase = faseNoInstante(fases, t);
    const blocos = [];
    if (fase) {
      const desde = fase.de_ms > 0 ? `desde ${relogio(fase.de_ms)} · ${fase.motivo}` : 'até cair a primeira torre (no máximo 14:00)';
      blocos.push(el('div', `pl-fase ${fase.fase}`, [el('b', '', NOMES_FASE[fase.fase] || fase.fase), el('span', 'sub', desde)]));
    } else {
      blocos.push(el('div', 'pl-fase', [el('b', '', 'fase: —'), el('span', 'sub', 'sem a linha do tempo da partida')]));
    }
    const linha = (rot, va, vv, { titulo = '', classe = '' } = {}) => {
      const r = el('div', `pl-linha ${classe}`, [el('span', 'num tm-a', va), el('span', 'pl-rot', rot), el('span', 'num tm-v', vv)]);
      if (titulo) r.title = titulo;
      return r;
    };
    blocos.push(el('div', 'pl-cab', [el('b', 'tm-a', 'Azul'), el('span'), el('b', 'tm-v', 'Vermelho')]));
    blocos.push(linha('abates', String(a.abates), String(v.abates), { classe: 'grande' }));
    blocos.push(linha('ouro', mil(a.ouro), mil(v.ouro), { titulo: 'Ouro total do time: linha do tempo do cliente, por minuto, interpolado entre um minuto e outro.' }));
    // Diferença de ouro: barra que cresce para o lado de quem está na frente.
    if (a.ouro !== null && v.ouro !== null) {
      const dif = a.ouro - v.ouro;
      const fr = Math.min(1, Math.abs(dif) / 10000); // 10 mil de diferença = barra cheia
      const barra = el('div', 'pl-dif');
      const b = el('i', dif >= 0 ? 'azul' : 'vermelho');
      b.style.width = `${fr * 50}%`;
      barra.append(b);
      const txt = Math.abs(dif) < 50 ? 'ouro empatado' : `${dif > 0 ? 'azul' : 'vermelho'} +${numero(Math.abs(dif) / 1000)} mil`;
      blocos.push(el('div', 'pl-difbloco', [barra, el('span', `sub num ${dif > 0 ? 'tm-a' : dif < 0 ? 'tm-v' : ''}`, txt)]));
    }
    const ou = (x, f) => (x === null || x === undefined ? '—' : f(x));
    blocos.push(linha('CS', ou(a.cs, numero), ou(v.cs, numero), { titulo: 'Tropas e monstros abatidos pelo time (soma do quadro de estados).' }));
    blocos.push(linha('nível médio', ou(a.nivel_medio, (n) => numero(n)), ou(v.nivel_medio, (n) => numero(n))));
    // Sentinelas colocadas por time até agora (linha do tempo da Riot).
    if (temSentinelas) {
      const sc = colocadasAte(sentinelas.sentinelas, t);
      blocos.push(linha('sentinelas', String(sc[100]), String(sc[200]), { titulo: 'Sentinelas colocadas pelo time até agora (WARD_PLACED da linha do tempo da Riot, qualquer tipo).' }));
    } else {
      blocos.push(linha('sentinelas', '—', '—', { titulo: `Sem sentinelas: ${sentinelas?.motivo || 'não deu para ler'}` }));
    }
    // Gráficos (montados uma vez; aqui só anda a marca do play).
    blocos.push(graficos.el);
    graficos.atualiza(t);
    // Time fazendo objetivo junto agora (das atividades; some quando acaba).
    if (dados) {
      const trs = p.participantes.map((x) => atividadeEstavel(dados.atividades, x.participante_id, t));
      for (const g of objetivosEmAndamento(trs.filter(Boolean), prepPlacar.timeDe)) {
        const tipo = /barão/.test(g.alvo) ? 'barao' : /arauto/.test(g.alvo) ? 'arauto' : /vastilarvas/.test(g.alvo) ? 'vastilarvas' : /dragão/.test(g.alvo) ? 'dragao' : /inibidor/.test(g.alvo) ? 'inibidor' : 'torre';
        const sub = { infernal: 'FIRE_DRAGON', oceano: 'WATER_DRAGON', montanha: 'EARTH_DRAGON', nuvens: 'AIR_DRAGON', hextec: 'HEXTECH_DRAGON', quimtec: 'CHEMTECH_DRAGON', ancião: 'ELDER_DRAGON' };
        const chave = Object.keys(sub).find((k) => g.alvo.includes(k));
        blocos.push(el('div', `pl-andamento ${g.time === 200 ? 'vermelho' : 'azul'}`, [icone(tipo, chave ? sub[chave] : '', g.alvo), el('span', '', textoEmAndamento(g))]));
      }
    }
    blocos.push(el('h4', 'pl-sec', 'Objetivos'));
    // Uma linha por objetivo: o ícone no meio, a quantidade de cada time ao lado.
    const linhaIcone = (tipo, va, vv, titulo, classe = '') => {
      const r = el('div', `pl-linha pl-ico ${classe}`, [el('span', 'num tm-a', va), icone(tipo, '', titulo), el('span', 'num tm-v', vv)]);
      r.title = titulo;
      return r;
    };
    blocos.push(linhaIcone('torre', String(a.torres), String(v.torres), 'Torres derrubadas pelo time (eventos da linha do tempo).'));
    blocos.push(linhaIcone('inibidor', String(a.inibidores), String(v.inibidores), 'Inibidores derrubados pelo time (renascem em 5 min).'));
    // Dragões na ordem em que caíram, cada um com o ícone do tipo e o nome no título.
    const fichas = (lista, lado) => {
      const c = el('span', `pl-drags ${lado}`);
      if (!lista.length) c.append(el('span', 'num', '0'));
      for (const s of lista) c.append(icone('dragao', s, `Dragão ${DRAGOES[s]?.nome || s}`, 'pequeno'));
      return c;
    };
    blocos.push(el('div', 'pl-linha drags', [fichas(a.dragoes, 'tm-a'), icone('dragao', '', 'Dragões elementais, na ordem em que caíram'), fichas(v.dragoes, 'tm-v')]));
    if (a.alma || v.alma) {
      const alma = (s) => (s ? `alma ${DRAGOES[s]?.nome || s}` : '—');
      blocos.push(linha('alma', alma(a.alma), alma(v.alma), { classe: 'texto' }));
    }
    if (a.anciao || v.anciao) {
      const anc = (x) => (x.anciao ? `${x.anciao}${x.anciao_ate_ms ? ` · até ${relogio(x.anciao_ate_ms)}` : ''}` : '0');
      blocos.push(linhaIcone('anciao', anc(a), anc(v), 'Dragão ancião (o bônus dura 2:30)'));
    }
    blocos.push(linhaIcone('vastilarvas', String(a.vastilarvas), String(v.vastilarvas), 'Vastilarvas'));
    blocos.push(linhaIcone('arauto', String(a.arauto), String(v.arauto), 'Arauto'));
    const bar = (x) => (x.barao ? `${x.barao}${x.barao_ate_ms ? ` · até ${relogio(x.barao_ate_ms)}` : ''}` : '0');
    blocos.push(
      linhaIcone('barao', bar(a), bar(v), 'Barão: o bônus dura 3 min a partir do abate e só para quem estava vivo; quem morre perde.', a.barao_ate_ms || v.barao_ate_ms ? 'ativo' : ''),
    );
    // O Atakhan saiu do jogo no 26.1: a linha só aparece se a partida (antiga) teve um.
    if (temAtakhan) blocos.push(linha('atakhan', String(a.atakhan), String(v.atakhan)));
    // Ondas: uma linha por rota com o estado de agora (estimado; ver ondas.js e o servidor).
    if (ondas) {
      blocos.push(el('h4', 'pl-sec', 'Ondas (estimadas)'));
      for (const l of ondasNoInstante(ondas, t)) {
        const classe = l.trecho ? `${l.trecho.estado} ${l.trecho.time === 200 ? 'tm-v' : l.trecho.time === 100 ? 'tm-a' : ''}` : 'sub';
        const estado = l.rastreados ? textoEstado(l.trecho) : 'ninguém da rota rastreado';
        const r = el('div', 'pl-onda', [el('span', 'pl-onda-rota', l.nome), el('span', `pl-onda-est ${classe}`, estado), el('span', 'sub num', textoOnda(l.onda))]);
        r.title = l.trecho ? `${l.nome}: ${textoEstado(l.trecho)} · confiança ${l.trecho.confianca} · ${(l.trecho.sinais || []).join('; ')}` : `${l.nome}: sem estimativa neste instante`;
        blocos.push(r);
      }
      const pl = el('p', 'sub pl-nota', 'Sem contar tropas: "devagar/rápido" é só a velocidade do avanço; depois da fase de rotas só enquanto alguém farma na rota.');
      pl.title = (ondas.limites || []).join(' ');
      blocos.push(pl);
    }
    const notas = ['Ouro por minuto (linha do tempo), interpolado.'];
    if (pl.sem_time) notas.push(`${pl.sem_time} objetivo(s) sem time identificado.`);
    blocos.push(el('p', 'sub pl-nota', notas.join(' ')));
    placar.replaceChildren(...blocos);
  }

  // --- Laço ---
  function quadro(agora) {
    if (!vivo) return;
    const dt = Math.min(250, agora - antes);
    antes = agora;
    dtTela = dt;
    janelas.garante(t);
    const pronto = desenhaQuadro();
    if (tocando && pronto) {
      t += dt * vel;
      if (t >= duracaoMs) {
        t = duracaoMs;
        alterna();
      }
    }
    desenhaEventos();
    desenhaTorres();
    desenhaOndas();
    desenhaSentinelas();
    desenhaHabilidades();
    desenhaPainel();
    desenhaPlacar();
    relogioEl.textContent = relogio(t);
    if (agora - ultimoBarra > 100 && document.activeElement !== barra) {
      barra.value = String(t / 1000);
      ultimoBarra = agora;
    }
    requestAnimationFrame(quadro);
  }
  requestAnimationFrame(quadro);

  // Tamanho do mapa: o maior quadrado que cabe no palco.
  const ajusta = () => {
    const r = palco.getBoundingClientRect();
    lado = Math.max(200, Math.floor(Math.min(r.width - 12, r.height - 12)));
    ret = Math.round(Math.min(58, Math.max(30, lado * 0.052)));
    mapa.style.width = `${lado}px`;
    mapa.style.height = `${lado}px`;
    mapa.style.setProperty('--ret', `${ret}px`);
    svgCham.setAttribute('viewBox', `0 0 ${lado} ${lado}`);
    if (ret !== retDoArranjo) {
      // O arranjo mede tudo em múltiplos do retrato: recomeça no tamanho novo, sem deslizar.
      arranjo = criaArranjo({ ret });
      retDoArranjo = ret;
      reiniciaDesvios = true;
    }
  };
  const obs = new ResizeObserver(ajusta);
  obs.observe(palco);
  ajusta();

  // --- Teclado ---
  function tecla(ev) {
    if (ev.target instanceof HTMLInputElement && ev.target.type !== 'range' && ev.target.type !== 'checkbox') return;
    if (ev.key === ' ' || ev.code === 'Space') {
      ev.preventDefault();
      alterna();
    } else if (ev.key === 'ArrowLeft' || ev.key === 'ArrowRight') {
      ev.preventDefault();
      vaiPara(t + (ev.key === 'ArrowLeft' ? -5000 : 5000));
    } else if (ev.key === 'f' || ev.key === 'F') {
      ev.preventDefault();
      telaCheia.alterna();
    } else if (ev.key === 'Escape') {
      // Painel aberto: o Esc fecha o painel. Sem painel e em tela cheia: o Esc fica para o app sair
      // da tela cheia (app.js). Fora da tela cheia: sai do mapa com play. Quem usa marca o evento.
      if (escolhido) {
        ev.preventDefault();
        escolhido = null;
        desenhaPainel(true);
      } else if (!telaCheia.estaLigada()) {
        ev.preventDefault();
        location.hash = sair.getAttribute('href');
      }
    }
  }
  document.addEventListener('keydown', tecla);
  mapa.addEventListener('click', () => {
    if (escolhido) {
      escolhido = null;
      desenhaPainel(true);
    }
  });

  return () => {
    vivo = false;
    obs.disconnect();
    document.removeEventListener('keydown', tecla);
    // A tela cheia continua ao sair do mapa: é a do app inteiro, não só desta tela.
    desligaCheia();
  };
}
