// O processo principal do LoL Tracking: mínimo de propósito.
//
// A interface é servida pelo servidor Go em http://127.0.0.1:{porta}/. Este arquivo garante que o
// servidor esteja de pé (sobe um com `go run .` se o /health não responder), abre a janela apontada
// para ele e, ao sair, derruba o servidor que ELE subiu, nunca um que já estava rodando.

const path = require('node:path');
const { app, BrowserWindow, ipcMain, shell } = require('electron');
const servidor = require('./servidor');

const PORTA = servidor.portaDoAmbiente();
const BASE = servidor.urlBase(PORTA);
const SAUDE = `${BASE}/health`;

let janela = null;
// O servidor que este processo iniciou, ou null quando ele já estava de pé.
let nosso = null;

const ESPERA = `<!doctype html><html lang="pt-BR"><head><meta charset="utf-8"><title>LoL Tracking</title>
<style>body{margin:0;height:100vh;display:grid;place-items:center;background:#0d1017;color:#e8ecf3;font:14px system-ui,"Segoe UI",sans-serif;-webkit-app-region:drag}
small{display:block;color:#94a3b8;margin-top:6px}</style></head>
<body><div>Iniciando o LoL Tracking…<small>a primeira abertura compila o servidor e pode levar um minuto</small></div></body></html>`;

const dataUrl = (html) => `data:text/html;charset=utf-8,${encodeURIComponent(html)}`;

function criaJanela() {
  janela = new BrowserWindow({
    width: 1280,
    height: 820,
    minWidth: 900,
    minHeight: 600,
    title: 'LoL Tracking',
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
