// Pausar e retomar a captura em andamento sem depender da janela do Lane (pedido do dono em
// 2026-11-18: "coloque algo para pausar quando quero falar com você"). Um atalho GLOBAL do sistema
// (padrão Ctrl+Alt+P, configurável), um item na bandeja e o estado no título da janela, tudo pela
// rota POST /api/captura/alternar do servidor (pausa o que estiver rodando — rastreio, coleta,
// repescagem ou passe de visão — ou retoma a última pausada).
//
// Este arquivo não importa o Electron: recebe globalShortcut, Notification, o fetch e o relógio por
// parâmetro, para testar com node (pausa.test.mjs) sem abrir janela.

const ATALHO_PADRAO = 'Control+Alt+P';

// Teclas que o Electron aceita num accelerator (o suficiente para um atalho de pausa).
const MODIFICADORES = new Set(['Control', 'Ctrl', 'Alt', 'Shift', 'CommandOrControl', 'CmdOrCtrl', 'Super', 'Meta', 'AltGr']);
const TECLA = /^([A-Z0-9]|F([1-9]|1[0-9]|2[0-4])|Space|Pause|Insert|Home|End|PageUp|PageDown|Plus|Minus)$/;

// atalhoValido: "Mod+...+Tecla" com pelo menos um modificador que não seja só Shift (um atalho
// global sem Ctrl/Alt roubaria a tecla de tudo o que a pessoa digita) e exatamente uma tecla.
function atalhoValido(texto) {
  if (typeof texto !== 'string') return false;
  const partes = texto.split('+').map((p) => p.trim());
  if (partes.length < 2 || partes.some((p) => !p)) return false;
  const tecla = partes[partes.length - 1];
  const mods = partes.slice(0, -1);
  if (!TECLA.test(tecla.length === 1 ? tecla.toUpperCase() : tecla)) return false;
  if (!mods.every((m) => MODIFICADORES.has(m))) return false;
  if (new Set(mods).size !== mods.length) return false;
  return mods.some((m) => m !== 'Shift');
}

// legivel: "Control+Alt+P" → "Ctrl+Alt+P" (o que aparece no aviso, na bandeja e em Configurações).
function legivel(atalho) {
  return String(atalho || '')
    .split('+')
    .map((p) => (p === 'Control' || p === 'CommandOrControl' || p === 'CmdOrCtrl' ? 'Ctrl' : p))
    .join('+');
}

const NOMES = { rastreio: 'rastreio', coleta: 'coleta por elo', repescagem: 'repescagem', visao: 'passes de visão' };

// descreve: "rastreio da partida 3305486988".
function descreve(c) {
  if (!c) return 'captura';
  const nome = NOMES[c.tipo] || 'captura';
  if (c.tipo === 'coleta') return `${nome} nº ${c.id}${c.game_id ? ` (partida ${c.game_id})` : ''}`;
  return c.game_id ? `${nome} da partida ${c.game_id}` : nome;
}

// situacao: o estado da captura em uma palavra, a partir de GET /api/captura.
//   'rodando' | 'esperando' (o motor esperando a pessoa parar de usar o computador) | 'pausando'
//   | 'pausada' | 'parada' (nada rodando nem pausado) | 'sem_servidor'
function situacao(estado) {
  if (!estado) return 'sem_servidor';
  const r = estado.rodando;
  if (r) {
    if (r.pausando) return 'pausando';
    if (r.esperando) return 'esperando';
    return 'rodando';
  }
  return estado.pausada ? 'pausada' : 'parada';
}

// titulo: o título da janela (barra de tarefas, Alt+Tab) com o estado da captura.
function titulo(estado) {
  switch (situacao(estado)) {
    case 'rodando':
      return 'Lane — capturando';
    case 'esperando':
      return 'Lane — captura esperando você parar de usar o computador';
    case 'pausando':
      return 'Lane — pausando a captura…';
    case 'pausada':
      return 'Lane — captura PAUSADA';
    default:
      return 'Lane';
  }
}

// aviso: a notificação do sistema para uma mudança de situação (null = nada a avisar). `antes` e
// `agora` são estados de GET /api/captura; `atalho` aparece no texto para a pessoa saber como voltar.
function aviso(antes, agora, atalho) {
  const a = situacao(antes);
  const b = situacao(agora);
  if (a === b || b === 'sem_servidor' || a === 'sem_servidor') return null;
  const tecla = legivel(atalho);
  // Pausa pedida: avisa no "pausando" (na hora); pausa do motor (espera além do teto, disjuntor):
  // vai direto de rodando/esperando para pausada, e avisa com o motivo.
  if ((b === 'pausando' && a !== 'pausada') || (b === 'pausada' && a !== 'pausando')) {
    const c = agora.pausada || agora.rodando;
    const motivo = agora.pausada?.motivo;
    return {
      titulo: 'Captura pausada',
      corpo: `${descreve(c)} pausou${motivo ? `: ${motivo}` : ''}. O replay fecha; para continuar: ${tecla}, o botão "Retomar captura" ou a bandeja.`,
    };
  }
  if (b === 'esperando') {
    return {
      titulo: 'Captura esperando você',
      corpo: `O computador está em uso: o replay ficou pausado e volta sozinho quando você parar. Para pausar de vez: ${tecla}.`,
    };
  }
  if (b === 'rodando' && (a === 'pausada' || a === 'parada' || a === 'pausando')) {
    return { titulo: 'Captura retomada', corpo: `${descreve(agora.rodando)} voltou a rodar. Para pausar: ${tecla}.` };
  }
  return null;
}

// controle: liga o atalho global, acompanha GET /api/captura e chama `aoMudar(estado)` (título, bandeja,
// página) a cada mudança, com a notificação do sistema nas transições.
//   deps = { globalShortcut, Notification (classe; pode faltar), fetch, base (http://127.0.0.1:porta),
//            aoMudar(estado), aoErro(msg) }
function criaControle(deps) {
  let atalho = null;
  let ultimo = null;
  let pedindo = false;

  async function le() {
    try {
      const r = await deps.fetch(`${deps.base}/api/captura`);
      if (!r.ok) return null;
      return await r.json();
    } catch {
      return null;
    }
  }

  function notifica(n) {
    if (!n || !deps.Notification) return;
    try {
      if (deps.Notification.isSupported && !deps.Notification.isSupported()) return;
      new deps.Notification({ title: n.titulo, body: n.corpo, silent: false }).show();
    } catch {
      /* sem notificação no sistema: o título e o botão continuam mostrando */
    }
  }

  // atualiza: lê o estado e avisa se mudou (o main chama de tempos em tempos e depois de cada ação).
  async function atualiza() {
    const agora = await le();
    const n = aviso(ultimo, agora, atalho || ATALHO_PADRAO);
    const mudou = JSON.stringify(agora) !== JSON.stringify(ultimo);
    ultimo = agora;
    notifica(n);
    if (mudou && deps.aoMudar) deps.aoMudar(agora);
    return agora;
  }

  // alterna: o que o atalho e a bandeja fazem. Erro do servidor (nada a retomar, cliente fechado...)
  // vira notificação com a frase dele.
  async function alterna() {
    if (pedindo) return null;
    pedindo = true;
    try {
      const r = await deps.fetch(`${deps.base}/api/captura/alternar`, { method: 'POST' });
      let corpo = null;
      try {
        corpo = await r.json();
      } catch {
        corpo = null;
      }
      if (!r.ok) {
        const msg = corpo?.erro || `o servidor respondeu ${r.status}`;
        notifica({ titulo: 'Lane', corpo: msg });
        if (deps.aoErro) deps.aoErro(msg);
        return null;
      }
      await atualiza();
      return corpo;
    } catch {
      notifica({ titulo: 'Lane', corpo: 'O servidor do Lane não respondeu.' });
      return null;
    } finally {
      pedindo = false;
    }
  }

  // registra: troca o atalho global. Devolve {ok, atalho, erro}. O anterior só sai se o novo pegou.
  function registra(novo) {
    const quer = novo || ATALHO_PADRAO;
    if (!atalhoValido(quer)) return { ok: false, atalho, erro: `atalho inválido: ${quer} (use Ctrl ou Alt + uma tecla)` };
    if (quer === atalho && deps.globalShortcut.isRegistered(quer)) return { ok: true, atalho };
    let ok = false;
    try {
      // A promessa volta para quem chamou (o Electron ignora; o teste espera por ela).
      ok = deps.globalShortcut.register(quer, () => alterna());
    } catch {
      ok = false;
    }
    if (!ok) return { ok: false, atalho, erro: `o atalho ${legivel(quer)} já é usado por outro programa` };
    if (atalho && atalho !== quer) deps.globalShortcut.unregister(atalho);
    atalho = quer;
    return { ok: true, atalho };
  }

  return {
    registra,
    alterna,
    atualiza,
    atalho: () => atalho,
    estado: () => ultimo,
    solta: () => {
      if (atalho) deps.globalShortcut.unregister(atalho);
      atalho = null;
    },
  };
}

module.exports = { ATALHO_PADRAO, atalhoValido, legivel, descreve, situacao, titulo, aviso, criaControle };
