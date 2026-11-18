// O processo principal do Lane (o app; o repositório ainda se chama lol-tracking): mínimo de propósito.
//
// A interface é servida pelo servidor Go em http://127.0.0.1:{porta}/. Este arquivo garante que o
// servidor esteja de pé (sobe um com `go run .` se o /health não responder), abre a janela apontada
// para ele e, ao sair, derruba o servidor que ELE subiu, nunca um que já estava rodando.

const path = require('node:path');
const { app, BrowserWindow, ipcMain, shell } = require('electron');
const servidor = require('./servidor');
const preferencias = require('./preferencias');

// LANE_DADOS_USUARIO: outra pasta de dados do usuário do Electron (preferências, cache, e a trava
// de instância única, que é por pasta). Serve para abrir uma segunda instância de teste sem
// derrubar a de uso (com LOL_PORTA de outro servidor). Precisa vir antes da trava.
if (process.env.LANE_DADOS_USUARIO) app.setPath('userData', path.resolve(process.env.LANE_DADOS_USUARIO));

const PORTA = servidor.portaDoAmbiente();
const BASE = servidor.urlBase(PORTA);
const SAUDE = `${BASE}/health`;

let janela = null;
// O servidor que este processo iniciou, ou null quando ele já estava de pé.
let nosso = null;

const ESPERA = `<!doctype html><html lang="pt-BR"><head><meta charset="utf-8"><title>Lane</title>
<style>body{margin:0;height:100vh;display:grid;place-items:center;background:#0d1017;color:#e8ecf3;font:14px system-ui,"Segoe UI",sans-serif;-webkit-app-region:drag}
small{display:block;color:#94a3b8;margin-top:6px}</style></head>
<body><div>Iniciando o Lane…<small>a primeira abertura compila o servidor e pode levar um minuto</small></div></body></html>`;

const dataUrl = (html) => `data:text/html;charset=utf-8,${encodeURIComponent(html)}`;

function criaJanela() {
  // Abre em tela cheia de verdade por padrão (Configurações → "abrir em tela cheia").
  const prefs = preferencias.le(app.getPath('userData'));
  janela = new BrowserWindow({
    fullscreen: prefs.tela_cheia,
    width: 1280,
    height: 820,
    minWidth: 900,
    minHeight: 600,
    title: 'Lane',
    // O ícone da janela e da barra de tarefas (a bússola com o cristal).
    icon: path.join(__dirname, '..', '..', 'assets', 'icone.ico'),
    // O --fundo escuro do protótipo: sem clarão branco antes da página pintar.
    backgroundColor: '#0d1017',
    autoHideMenuBar: true,
    // Sem a moldura do Windows: a barra de título é a do app, escura, igual à do workflow.
    frame: false,
    webPreferences: {
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
      preload: path.join(__dirname, 'preload.js'),
    },
  });
  // Link de fora abre no navegador do sistema; dentro da janela só vive o app.
  janela.webContents.setWindowOpenHandler(({ url }) => {
    if (/^https?:\/\//i.test(url)) shell.openExternal(url);
    return { action: 'deny' };
  });
  janela.webContents.on('will-navigate', (evento, url) => {
    if (url === BASE || url.startsWith(`${BASE}/`) || url.startsWith('data:')) return;
    evento.preventDefault();
  });
  // A página acompanha a tela cheia (o botão da barra de título e o do mapa com play mostram o
  // estado certo, venha a mudança de onde vier: botão, F11, Esc).
  const avisa = () => janela?.webContents.send('janela:tela-cheia', janela.isFullScreen());
  janela.on('enter-full-screen', avisa);
  janela.on('leave-full-screen', avisa);
  // F11 alterna a tela cheia em qualquer tela (a janela sem moldura não tem o menu que faria isso).
  // O Esc fica com a página: ela sai da tela cheia quando o Esc não foi usado para outra coisa
  // (fechar o painel do mapa com play, por exemplo).
  janela.webContents.on('before-input-event', (evento, entrada) => {
    if (entrada.type === 'keyDown' && entrada.key === 'F11' && !entrada.alt && !entrada.control && !entrada.meta) {
      evento.preventDefault();
      janela.setFullScreen(!janela.isFullScreen());
    }
  });
  janela.on('closed', () => {
    janela = null;
  });
}

// Devolve '' com o servidor de pé, ou o HTML da página de erro.
async function garanteServidor() {
  if (await servidor.estaDePe(SAUDE)) return '';
  nosso = servidor.inicia(servidor.planoDeDesenvolvimento(__dirname, PORTA));
  const ok = await servidor.espera(SAUDE, {
    tentativas: Math.ceil(servidor.ESPERA_MS / servidor.INTERVALO_MS),
    intervalo: servidor.INTERVALO_MS,
    desistir: () => nosso.morreu,
  });
  if (ok) return '';
  const msg = servidor.mensagemDeFalha({ codigoErro: nosso.codigoErro, porta: PORTA, morreu: nosso.morreu });
  servidor.encerra(nosso);
  return servidor.paginaDeErro(msg, nosso.linhas);
}

async function inicia() {
  criaJanela();
  janela.loadURL(dataUrl(ESPERA));
  const erro = await garanteServidor();
  if (!janela) return;
  await janela.loadURL(erro ? dataUrl(erro) : `${BASE}/`).catch(() => {});
}

// Os botões da barra de título (a moldura é do app).
ipcMain.handle('janela:minimiza', () => janela?.minimize());
ipcMain.handle('janela:maximiza', () => {
  if (!janela) return false;
  if (janela.isMaximized()) janela.unmaximize();
  else janela.maximize();
  return janela.isMaximized();
});
ipcMain.handle('janela:fecha', () => janela?.close());
// Tela cheia: um estado só, o da janela (o mapa com play usava a API de tela cheia da página, que
// brigava com a da janela). `quer` booleano = põe nesse estado; sem ele, alterna.
ipcMain.handle('janela:tela-cheia', (_e, quer) => {
  if (!janela) return false;
  janela.setFullScreen(typeof quer === 'boolean' ? quer : !janela.isFullScreen());
  return janela.isFullScreen();
});
ipcMain.handle('janela:esta-em-tela-cheia', () => Boolean(janela?.isFullScreen()));
// Preferências da janela (Configurações): lidas e gravadas aqui, valem na próxima abertura.
ipcMain.handle('preferencias:le', () => preferencias.le(app.getPath('userData')));
ipcMain.handle('preferencias:grava', (_e, mudanca) => preferencias.grava(app.getPath('userData'), mudanca));

// Sem isto o Windows agrupa a janela na barra de tarefas sob o ícone do Electron.
app.setAppUserModelId('br.nzt.lane');

// Uma instância só: a segunda tentaria subir outro servidor na mesma porta.
if (!app.requestSingleInstanceLock()) {
  app.quit();
} else {
  app.on('second-instance', () => {
    if (janela) {
      if (janela.isMinimized()) janela.restore();
      janela.focus();
    }
  });
  app.whenReady().then(inicia);
}

app.on('before-quit', () => servidor.encerra(nosso));
app.on('window-all-closed', () => app.quit());
