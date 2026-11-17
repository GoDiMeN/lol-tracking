// Garantir que o servidor Go do LoL Tracking esteja de pé.
//
// O app é só a janela: quem fala com o cliente do LoL e guarda as partidas é o servidor em
// 127.0.0.1:{porta}. Aqui ficam as decisões (que porta, como subir, como derrubar), em funções puras
// testadas sem Electron (servidor.test.mjs); a parte que mexe em processo é curta e fica no fim.

const path = require('node:path');
const { spawn, spawnSync } = require('node:child_process');

// A mesma de server/config.go (PortaPadrao). Mexeu lá, mexa aqui.
const PORTA_PADRAO = 8770;

// O primeiro `go run .` compila o SQLite em Go puro e passa fácil de um minuto.
const ESPERA_MS = 180000;
const INTERVALO_MS = 500;
// Teto de cada pergunta ao /health: porta que aceita e não responde não pode travar a espera.
const PRAZO_PERGUNTA_MS = 2000;

function portaDoAmbiente(env = process.env) {
  const texto = String(env.LOL_PORTA ?? '').trim();
  if (!/^\d+$/.test(texto)) return PORTA_PADRAO;
  const n = Number(texto);
  return n >= 1 && n <= 65535 ? n : PORTA_PADRAO;
}

// 127.0.0.1 e nunca "localhost": no Windows o localhost pode ir para ::1, e o servidor só escuta em IPv4.
function urlBase(porta) {
  return `http://127.0.0.1:${porta}`;
}

// estaDePe pergunta ao /health se o NOSSO servidor está lá: precisa ser o JSON {"ok": true}, e não
// qualquer 200 (outro programa na mesma porta não serve).
async function estaDePe(url, fetchFn = globalThis.fetch) {
  try {
    const r = await fetchFn(url, { signal: AbortSignal.timeout(PRAZO_PERGUNTA_MS) });
    if (!r || !r.ok) return false;
    const corpo = await r.json();
    return corpo?.ok === true;
  } catch {
    return false;
  }
}

// espera repete estaDePe até dar certo, acabar o prazo ou `desistir()` dizer que não adianta
// (o processo que subimos morreu).
async function espera(url, { tentativas, intervalo, fetchFn = globalThis.fetch, desistir = () => false }) {
  for (let i = 0; i < tentativas; i++) {
    if (desistir()) return false;
    if (await estaDePe(url, fetchFn)) return true;
    if (i < tentativas - 1) await new Promise((ok) => setTimeout(ok, intervalo));
  }
  return false;
}

// planoDeDesenvolvimento: `go run .` em server/, com a porta e a pasta do renderer absolutas
// (o padrão do servidor é relativo ao cwd dele).
function planoDeDesenvolvimento(dirMain, porta, env = process.env) {
  return {
    comando: 'go',
    args: ['run', '.'],
    cwd: path.resolve(dirMain, '..', '..', '..', 'server'),
    env: { ...env, LOL_PORTA: String(porta), LOL_RENDERER: path.resolve(dirMain, '..', 'renderer') },
  };
}

// comoEncerrar: o `go run` roda o servidor como processo filho; matar só o `go` deixaria a porta
// presa. No Windows, taskkill /T leva a árvore; fora dele, o grupo do processo (pid negativo).
function comoEncerrar(pid, plataforma) {
  if (plataforma === 'win32') return { comando: 'taskkill', args: ['/pid', String(pid), '/T', '/F'] };
  return { sinal: 'SIGTERM', alvo: -pid };
}

function mensagemDeFalha({ codigoErro, porta, morreu }) {
  if (codigoErro === 'ENOENT') {
    return 'O Go não foi encontrado no PATH. Instale o Go ou suba o servidor à mão: cd server && go run .';
  }
  if (morreu) {
    return `O servidor fechou logo ao iniciar. Confira se a porta ${porta} está livre (LOL_PORTA troca a porta) e veja as linhas abaixo.`;
  }
  return `O servidor não respondeu em ${urlBase(porta)}/health. Rode à mão para ver o erro: cd server && go run .`;
}

function escapa(texto) {
  return String(texto ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]);
}

// paginaDeErro é a janela de quando o servidor não sobe. Sem script: vai por data: URL.
function paginaDeErro(mensagem, linhas = []) {
  const pre = linhas.length ? `<pre>${escapa(linhas.join('\n'))}</pre>` : '';
  return `<!doctype html><html lang="pt-BR"><head><meta charset="utf-8"><title>LoL Tracking</title>
<style>body{margin:0;padding:28px;background:#0d1017;color:#e8ecf3;font:14px/1.5 system-ui,"Segoe UI",sans-serif}
h1{-webkit-app-region:drag}
h1{font-size:17px;color:#f85149;margin:0 0 10px}pre{background:#12161f;border:1px solid #242e3d;border-radius:8px;padding:12px;
white-space:pre-wrap;font:12px/1.4 ui-monospace,Consolas,monospace;color:#94a3b8;max-height:240px;overflow:auto}</style></head>
<body><h1>Não foi possível iniciar o LoL Tracking</h1><p>${escapa(mensagem)}</p>${pre}</body></html>`;
}

// --- Processo de verdade ------------------------------------------------------------------------

const LINHAS_GUARDADAS = 30;

function inicia(plano, plataforma = process.platform, spawnFn = spawn) {
  const estado = { filho: null, morreu: false, codigoErro: '', linhas: [] };
  const guarda = (dados) => {
    for (const linha of String(dados).split(/\r?\n/)) {
      if (!linha.trim()) continue;
      estado.linhas.push(linha);
      if (estado.linhas.length > LINHAS_GUARDADAS) estado.linhas.shift();
    }
  };
  const filho = spawnFn(plano.comando, plano.args, {
    cwd: plano.cwd,
    env: plano.env,
    windowsHide: true,
    detached: plataforma !== 'win32',
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  estado.filho = filho;
  filho.stdout?.on('data', guarda);
  filho.stderr?.on('data', guarda);
  filho.on('error', (err) => {
    estado.morreu = true;
    estado.codigoErro = err.code || '';
    guarda(err.message);
  });
  filho.on('exit', () => {
    estado.morreu = true;
  });
  return estado;
}

// encerra derruba só o servidor que NÓS subimos. Síncrono: roda no before-quit.
function encerra(estado, plataforma = process.platform) {
  const filho = estado?.filho;
  if (!filho || estado.morreu || !filho.pid) return;
  const c = comoEncerrar(filho.pid, plataforma);
  try {
    if (c.comando) spawnSync(c.comando, c.args, { windowsHide: true, stdio: 'ignore' });
    else process.kill(c.alvo, c.sinal);
  } catch {
    // Já tinha saído.
  }
}

module.exports = {
  PORTA_PADRAO,
  ESPERA_MS,
  INTERVALO_MS,
  portaDoAmbiente,
  urlBase,
  estaDePe,
  espera,
  planoDeDesenvolvimento,
  comoEncerrar,
  mensagemDeFalha,
  escapa,
  paginaDeErro,
  inicia,
  encerra,
};
