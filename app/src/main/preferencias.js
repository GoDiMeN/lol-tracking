// Preferências da janela guardadas do lado do processo principal: o main precisa saber ANTES de
// criar a janela se ela abre em tela cheia, e o localStorage da página só existe depois. Um JSON
// pequeno na pasta de dados do usuário do Electron (app.getPath('userData')/janela.json). Puro o
// bastante para testar com node (preferencias.test.mjs): só fs e path.

const fs = require('node:fs');
const path = require('node:path');
const { ATALHO_PADRAO, atalhoValido } = require('./pausa');

const ARQUIVO = 'janela.json';

// O padrão decidido pelo dono (2026-11-18): abrir MAXIMIZADO, não em tela cheia de verdade (a barra
// de tarefas e a barra de título continuam à vista). A tela cheia segue no botão ⛶ e no F11, e
// quem quiser abrir nela marca em Configurações. atalho_pausa: o atalho global que pausa/retoma a
// captura (pausa.js), também mudado em Configurações.
const PADRAO = Object.freeze({ tela_cheia: false, atalho_pausa: ATALHO_PADRAO });

// le: as preferências do arquivo; arquivo ausente, ilegível ou com valor de tipo errado = padrão
// (uma preferência quebrada não pode impedir o app de abrir).
function le(dir) {
  const r = { ...PADRAO };
  try {
    const dados = JSON.parse(fs.readFileSync(path.join(dir, ARQUIVO), 'utf8'));
    if (typeof dados?.tela_cheia === 'boolean') r.tela_cheia = dados.tela_cheia;
    if (atalhoValido(dados?.atalho_pausa)) r.atalho_pausa = dados.atalho_pausa;
  } catch {
    /* sem arquivo (primeira vez) ou quebrado: fica o padrão */
  }
  return r;
}

// grava: mistura `mudanca` no que está guardado (só as chaves conhecidas, com o tipo certo) e grava
// num temporário renomeado, para um desligamento no meio não deixar o arquivo pela metade.
function grava(dir, mudanca) {
  const r = le(dir);
  if (typeof mudanca?.tela_cheia === 'boolean') r.tela_cheia = mudanca.tela_cheia;
  if (atalhoValido(mudanca?.atalho_pausa)) r.atalho_pausa = mudanca.atalho_pausa;
  fs.mkdirSync(dir, { recursive: true });
  const arq = path.join(dir, ARQUIVO);
  fs.writeFileSync(`${arq}.tmp`, JSON.stringify(r, null, 2));
  fs.renameSync(`${arq}.tmp`, arq);
  return r;
}

module.exports = { le, grava, PADRAO, ARQUIVO };
