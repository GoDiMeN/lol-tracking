// Ponte mínima entre a página e a janela. A moldura é desenhada pelo app (frame: false, como no
// workflow), então os três botões do canto precisam de um caminho até o processo principal. Só
// isso passa por aqui (mais a tela cheia e as preferências da janela): a página continua sem Node e
// sem acesso a disco.

const { contextBridge, ipcRenderer } = require('electron');

contextBridge.exposeInMainWorld('janelaApp', {
  minimiza: () => ipcRenderer.invoke('janela:minimiza'),
  alternaMaximizada: () => ipcRenderer.invoke('janela:maximiza'),
  fecha: () => ipcRenderer.invoke('janela:fecha'),
  // Tela cheia da janela (um estado só para o app inteiro, inclusive o mapa com play).
  telaCheia: (quer) => ipcRenderer.invoke('janela:tela-cheia', quer),
  estaEmTelaCheia: () => ipcRenderer.invoke('janela:esta-em-tela-cheia'),
  aoMudarTelaCheia: (fn) => {
    const ouvinte = (_e, ligada) => fn(Boolean(ligada));
    ipcRenderer.on('janela:tela-cheia', ouvinte);
    return () => ipcRenderer.removeListener('janela:tela-cheia', ouvinte);
  },
  // Preferências guardadas pelo processo principal ({tela_cheia}).
  preferencias: () => ipcRenderer.invoke('preferencias:le'),
  guardaPreferencias: (mudanca) => ipcRenderer.invoke('preferencias:grava', mudanca),
});
