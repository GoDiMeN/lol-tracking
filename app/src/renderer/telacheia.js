// Tela cheia: UM estado só para o app inteiro. Dentro do Electron é a tela cheia da janela
// (window.janelaApp, preload.js), a mesma do F11 e da preferência "abrir em tela cheia"; no
// navegador comum (o app servido em http://127.0.0.1:8770/ sem a janela) cai na API de tela cheia
// da página. Antes o mapa com play usava sempre a da página, e as duas brigavam.

const ponte = () => (typeof window !== 'undefined' ? window.janelaApp : null);

let ligada = false;
const ouvintes = new Set();
const avisa = (v) => {
  ligada = Boolean(v);
  for (const f of ouvintes) f(ligada);
};

if (typeof window !== 'undefined') {
  const j = ponte();
  if (j?.aoMudarTelaCheia) {
    j.aoMudarTelaCheia(avisa);
    j.estaEmTelaCheia?.().then(avisa).catch(() => {});
  } else if (typeof document !== 'undefined') {
    document.addEventListener('fullscreenchange', () => avisa(Boolean(document.fullscreenElement)));
  }
}

// estaLigada: o estado conhecido agora.
export const estaLigada = () => ligada;

// aoMudar(fn): fn(ligada) a cada mudança (e já com o estado atual); devolve a função que desliga.
export function aoMudar(fn) {
  ouvintes.add(fn);
  fn(ligada);
  return () => ouvintes.delete(fn);
}

// alterna / poe(quer): liga ou desliga.
export function poe(quer) {
  const j = ponte();
  if (j?.telaCheia) return j.telaCheia(quer).then(avisa).catch(() => {});
  if (quer && !document.fullscreenElement) return document.documentElement.requestFullscreen?.().catch(() => {});
  if (!quer && document.fullscreenElement) return document.exitFullscreen?.().catch(() => {});
  return Promise.resolve();
}
export const alterna = () => poe(!ligada);
