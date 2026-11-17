// Ponte mínima entre a página e a janela. A moldura é desenhada pelo app (frame: false, como no
// workflow), então os três botões do canto precisam de um caminho até o processo principal. Só
// isso passa por aqui: a página continua sem Node e sem acesso a disco.

const { contextBridge, ipcRenderer } = require('electron');

contextBridge.exposeInMainWorld('janelaApp', {
  minimiza: () => ipcRenderer.invoke('janela:minimiza'),
  alternaMaximizada: () => ipcRenderer.invoke('janela:maximiza'),
  fecha: () => ipcRenderer.invoke('janela:fecha'),
});
