// Capturas das telas para conferir com os olhos (tarefa 5). Abre uma janela do Electron própria,
// com pasta de usuário temporária (não mexe no app de ninguém), navega por cada tela contra um
// servidor já de pé e salva PNGs em docs/capturas-ui/.
//
//   cd app && npx electron scripts/capturas.mjs [nome...]
//
// CAPTURA_BASE  servidor com dados reais (padrão http://127.0.0.1:8790; ex.: LOL_DADOS=dados/ui-dev)
// CAPTURA_TESTE servidor no modo teste (padrão http://127.0.0.1:8791; LOL_TESTE=1), para a fila de
//               rastreio ao vivo e o diálogo Rastrear com o cliente "conectado".
// Sem nomes, tira todas. Nada aqui abre o LoL: só fala com os servidores dados.

import { app, BrowserWindow, nativeTheme } from 'electron';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const AQUI = path.dirname(fileURLToPath(import.meta.url));
const SAIDA = path.resolve(AQUI, '..', '..', 'docs', 'capturas-ui');
const BASE = process.env.CAPTURA_BASE || 'http://127.0.0.1:8790';
const TESTE = process.env.CAPTURA_TESTE || 'http://127.0.0.1:8791';
const G = 3304594274;

app.setPath('userData', fs.mkdtempSync(path.join(os.tmpdir(), 'loltracking-capturas-')));
app.commandLine.appendSwitch('force-device-scale-factor', '1');

const esperaMs = (ms) => new Promise((ok) => setTimeout(ok, ms));

// Cada captura: endereço, quanto esperar e, se preciso, um script a rodar antes.
const CAPTURAS = [
  { nome: '01-jogadores', url: `${BASE}/#jogadores/49bbe23f-20b4-5d49-bc5d-95a66c331afc`, espera: 4000 },
  {
    nome: '02-rastrear-dialogo',
    url: `${TESTE}/#jogadores/puuid-teste-0001`,
    prepara: 'busca',
    espera: 3000,
    antes: `(async () => {
      const b = [...document.querySelectorAll('button')].find((x) => x.textContent === 'Rastrear' && !x.disabled);
      if (b) b.click();
      await new Promise((ok) => setTimeout(ok, 2500));
      const v = document.querySelector('input[name=velocidade][value="4"]'); if (v) v.click();
      const c = [...document.querySelectorAll('.dlg-jogador input')]; c.slice(2).forEach((x) => x.click());
      const d = document.querySelectorAll('.dlg-trecho input'); if (d[1]) { d[1].value = '15:00'; d[1].dispatchEvent(new Event('input')); }
    })()`,
    depois: 800,
  },
  { nome: '03-rastreio-ao-vivo', url: `${TESTE}/#rastreio`, espera: 16000, prepara: 'rastreio' },
  { nome: '04-partida', url: `${BASE}/#partida/${G}/3/12`, espera: 3000 },
  { nome: '05-partida-detalhes', url: `${BASE}/#partida/${G}/3/12`, espera: 3000, altura: 2300 },
  { nome: '06-partida-leblanc-abate', url: `${BASE}/#partida/${G}/8/5`, espera: 3000 },
  { nome: '07-replay-2m27-abate-meio', url: `${BASE}/#replay/${G}/148.6`, espera: 3500 },
  {
    nome: '08-replay-8m42-saida-de-rota',
    url: `${BASE}/#replay/${G}/522`,
    confere: `JSON.stringify({p: document.querySelector('.rp-painel').hidden, n: document.querySelectorAll('.figura').length, t: document.querySelector('.rp-painel').textContent.slice(0,80)})`,
    espera: 3500,
    antes: `(() => { const f = [...document.querySelectorAll('.figura')][2]; if (f) f.click(); })()`,
    depois: 600,
  },
  {
    nome: '09-replay-8m48-barao-zonas',
    url: `${BASE}/#replay/${G}/528`,
    espera: 3500,
    antes: `(() => { const c = document.querySelector('.rp-topo input[type=checkbox]'); if (c) c.click(); })()`,
    depois: 600,
  },
  { nome: '10-estudo', url: `${BASE}/#estudo`, espera: 3000, prepara: 'estudo', altura: 1500 },
  { nome: '11-configuracoes', url: `${BASE}/#configuracoes`, espera: 2000 },
  {
    nome: '12-partida-tema-claro',
    url: `${BASE}/#partida/${G}/3/12`,
    espera: 3000,
    antes: `document.documentElement.dataset.theme = 'light'`,
    depois: 400,
  },
  {
    // Conferência sem imagem: tocar a 8× por 4 s atravessa a janela de 60 s (pré-busca) sem travar.
    nome: 'confere-replay-tocando',
    salva: false,
    url: `${BASE}/#replay/${G}/50`,
    espera: 2500,
    antes: `(() => { [...document.querySelectorAll('.rp-vels button')].find((b) => b.textContent === '8×').click(); document.querySelector('.rp-play').click(); })()`,
    depois: 4000,
    confere: `JSON.stringify({relogio: document.querySelector('.rp-relogio').textContent, aviso: document.querySelector('.rp-aviso').hidden ? '' : document.querySelector('.rp-aviso').textContent, visiveis: [...document.querySelectorAll('.figura')].filter((f) => !f.hidden).length})`,
  },
];

// Preparos que mexem no servidor (só nos de desenvolvimento, nunca no LoL de verdade).
async function prepara(qual, c) {
  if (qual === 'busca' || qual === 'rastreio') {
    await fetch(`${TESTE}/api/jogadores/buscar`, { method: 'POST', body: JSON.stringify({ texto: 'Jogador Teste#br1' }) });
    await fetch(`${TESTE}/api/jogadores/puuid-teste-0001/partidas?quantidade=3`);
  }
  if (qual === 'rastreio') {
    // Um rastreio novo no modo teste (jogo falso): 3 jogadores, os 3 primeiros minutos a 2×.
    const r = await fetch(`${TESTE}/api/partidas/3304789000/rastreio`, {
      method: 'POST',
      body: JSON.stringify({ jogadores: [3, 8, 1], velocidade: 2, ate_ms: 180000 }),
    });
    const corpo = await r.json();
    if (corpo.rastreio) c.url = `${TESTE}/#rastreio/${corpo.rastreio.id}`;
    else console.log('rastreio no modo teste:', corpo.erro);
  }
  if (qual === 'estudo') {
    const { estudos } = await (await fetch(`${BASE}/api/estudos`)).json();
    let id = estudos[0]?.id;
    if (!id) {
      const r = await fetch(`${BASE}/api/estudos`, {
        method: 'POST',
        body: JSON.stringify({ nome: 'Akshan no meio', puuid: '49bbe23f-20b4-5d49-bc5d-95a66c331afc', campeao_id: 166 }),
      });
      id = (await r.json()).estudo.id;
      const { partidas } = await (await fetch(`${BASE}/api/estudos/${id}/sugestoes`)).json();
      for (const p of partidas.slice(0, 6)) {
        await fetch(`${BASE}/api/estudos/${id}/partidas/${p.game_id}`, { method: 'POST', body: JSON.stringify({ anotacao: '' }) });
      }
      await fetch(`${BASE}/api/estudos/${id}/partidas/${G}`, {
        method: 'PUT',
        body: JSON.stringify({ anotacao: 'Saiu do meio aos 8:36 com o Barão vivo e morreu na cova.' }),
      });
    }
    c.url = `${BASE}/#estudo/${id}/mortes_fora_da_rota`;
  }
}

async function tira(win, c) {
  if (c.prepara) await prepara(c.prepara, c);
  win.setContentSize(1440, c.altura || 900);
  await win.loadURL('about:blank');
  await win.loadURL(c.url);
  await esperaMs(c.espera);
  if (c.antes) await win.webContents.executeJavaScript(c.antes, true);
  if (c.depois) await esperaMs(c.depois);
  if (c.confere) console.log('  confere', c.nome, await win.webContents.executeJavaScript(c.confere, true));
  if (c.rola) {
    await win.webContents.executeJavaScript(`document.querySelector('#conteudo').scrollTop = ${c.rola}`);
    await esperaMs(400);
  }
  if (c.salva === false) return console.log('ok', c.nome);
  const img = await win.webContents.capturePage();
  fs.writeFileSync(path.join(SAIDA, `${c.nome}.png`), img.toPNG());
  console.log('ok', c.nome);
}

app.whenReady().then(async () => {
  fs.mkdirSync(SAIDA, { recursive: true });
  // O protótipo é escuro; a captura do tema claro troca o tema na própria página.
  nativeTheme.themeSource = 'dark';
  const so = process.argv.slice(2).filter((a) => !a.startsWith('-') && !a.endsWith('.mjs'));
  const win = new BrowserWindow({
    width: 1440,
    height: 900,
    // Janela invisível (opacidade 0, sem foco, fora da barra de tarefas) mas desenhando de verdade:
    // escondida, o Chromium para de compor quadros e a captura sai com a tela velha.
    show: false,
    skipTaskbar: true,
    focusable: false,
    backgroundColor: '#0d1017',
    // Sem isto a janela escondida congela o requestAnimationFrame e a captura sai velha.
    webPreferences: { contextIsolation: true, sandbox: true, backgroundThrottling: false },
  });
  win.setOpacity(0);
  win.showInactive();
  win.webContents.on('console-message', (e) => {
    if (e.level === 'error' && !String(e.message).includes('Security Warning')) console.log('  [console]', e.message);
  });
  for (const c of CAPTURAS) {
    if (so.length && !so.some((s) => c.nome.includes(s))) continue;
    try {
      await tira(win, c);
    } catch (err) {
      console.log('falhou', c.nome, err.message);
    }
  }
  app.quit();
});
