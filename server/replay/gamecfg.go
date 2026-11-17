package replay

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SufixoBackup é o backup do game.cfg feito UMA vez, antes da primeira mudança.
const SufixoBackup = ".bak-loltracking"

// GameCfgDe deduz o game.cfg a partir do lockfile (os dois ficam na pasta de
// instalação: <pasta>\lockfile e <pasta>\Config\game.cfg).
func GameCfgDe(lockfile string) string {
	return filepath.Join(filepath.Dir(lockfile), "Config", "game.cfg")
}

// GaranteReplayAPI deixa EnableReplayApi=1 na seção [General] do game.cfg,
// sem tocar em mais nada (outras linhas, ordem, fim de linha). Sem a linha a
// porta 2999 não responde e o rastreio não tem como provar que é replay.
// Devolve se mudou. O jogo lê o arquivo ao abrir: por isso isto roda antes do watch.
func GaranteReplayAPI(caminho string) (bool, error) {
	b, err := os.ReadFile(caminho)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("game.cfg não encontrado em %s (o jogo já foi aberto alguma vez?)", caminho)
		}
		return false, err
	}
	texto := string(b)
	quebra := "\n"
	if strings.Contains(texto, "\r\n") {
		quebra = "\r\n"
	}
	linhas := strings.Split(texto, quebra)
	secao, general, achou := "", -1, false
	for i, l := range linhas {
		s := strings.TrimSpace(l)
		if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
			secao = s
			if strings.EqualFold(s, "[General]") && general < 0 {
				general = i
			}
			continue
		}
		if !strings.EqualFold(secao, "[General]") {
			continue
		}
		chave, valor, ok := strings.Cut(s, "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(chave), "EnableReplayApi") {
			continue
		}
		if strings.TrimSpace(valor) == "1" {
			return false, nil
		}
		linhas[i] = "EnableReplayApi=1"
		achou = true
		break
	}
	if !achou {
		if general < 0 {
			// Sem [General]: acrescenta a seção no fim.
			if n := len(linhas); n > 0 && linhas[n-1] == "" {
				linhas = append(linhas[:n-1], "[General]", "EnableReplayApi=1", "")
			} else {
				linhas = append(linhas, "[General]", "EnableReplayApi=1")
			}
		} else {
			linhas = append(linhas[:general+1], append([]string{"EnableReplayApi=1"}, linhas[general+1:]...)...)
		}
	}
	backup := caminho + SufixoBackup
	if _, err := os.Stat(backup); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(backup, b, 0o644); err != nil {
			return false, fmt.Errorf("backup do game.cfg: %w", err)
		}
	}
	if err := os.WriteFile(caminho, []byte(strings.Join(linhas, quebra)), 0o644); err != nil {
		return false, fmt.Errorf("gravar o game.cfg: %w", err)
	}
	return true, nil
}
