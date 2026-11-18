// Package arquivo guarda em disco, fora do SQLite, o que é pesado numa partida
// rastreada: os passos (amostras e estado dos 10), a cadeia, os retratos dos
// abates e a linha do tempo da Riot. A base fica só com a leitura básica
// (partida, jogadores, passes, indicadores) e com a referência ao arquivo.
//
// Um diretório por partida, ao lado do lol.db:
//
//	<pasta do banco>/partidas/<game_id>/<nome>.zst
//
// Cada arquivo é o conteúdo inteiro comprimido com zstd no nível mais alto.
// Quem precisa do dado lê, descomprime na memória e usa; o que fica no disco
// está sempre comprimido. Medido na partida 3304217746 (10 rastreados): a
// cadeia cai de 1.150 KB (linhas no SQLite) para ~125 KB e os retratos de
// 610 KB para ~37 KB; zstd fica a poucos % do xz e abre bem mais rápido.
//
// A gravação é atômica (arquivo temporário + rename): um desligamento no meio
// deixa o arquivo antigo inteiro ou o novo inteiro, nunca metade.
package arquivo

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/klauspost/compress/zstd"

	"loltracking/server/banco"
)

// Extensao dos arquivos no disco.
const Extensao = ".zst"

var (
	umaVez      sync.Once
	compressor  *zstd.Encoder
	descompress *zstd.Decoder
	erroInicio  error
)

// Encoder e decoder do zstd são seguros para EncodeAll/DecodeAll concorrentes
// e caros de criar: um par para o processo.
func inicia() error {
	umaVez.Do(func() {
		compressor, erroInicio = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedBestCompression))
		if erroInicio != nil {
			return
		}
		descompress, erroInicio = zstd.NewReader(nil)
	})
	return erroInicio
}

// Comprime e Descomprime ficam expostas para quem quer medir.
func Comprime(dados []byte) ([]byte, error) {
	if err := inicia(); err != nil {
		return nil, err
	}
	return compressor.EncodeAll(dados, make([]byte, 0, len(dados)/4+64)), nil
}

func Descomprime(dados []byte) ([]byte, error) {
	if err := inicia(); err != nil {
		return nil, err
	}
	return descompress.DecodeAll(dados, nil)
}

// PastaDaPartida é o diretório dos arquivos da partida.
func PastaDaPartida(db *banco.DB, gameID int64) string {
	return filepath.Join(db.Pasta, "partidas", strconv.FormatInt(gameID, 10))
}

func caminho(db *banco.DB, gameID int64, nome string) (string, error) {
	if db == nil || db.Pasta == "" {
		return "", errors.New("arquivo: o banco não tem pasta de dados")
	}
	if nome == "" || strings.ContainsAny(nome, `/\:`) || strings.Contains(nome, "..") {
		return "", fmt.Errorf("arquivo: nome inválido %q", nome)
	}
	return filepath.Join(PastaDaPartida(db, gameID), nome+Extensao), nil
}

// Grava comprime e grava o conteúdo. Devolve os bytes que ficaram no disco.
func Grava(db *banco.DB, gameID int64, nome string, dados []byte) (int, error) {
	arq, err := caminho(db, gameID, nome)
	if err != nil {
		return 0, err
	}
	z, err := Comprime(dados)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(arq), 0o755); err != nil {
		return 0, fmt.Errorf("criar a pasta da partida: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(arq), ".tmp-*")
	if err != nil {
		return 0, err
	}
	nomeTmp := tmp.Name()
	_, err = tmp.Write(z)
	if err == nil {
		err = tmp.Sync()
	}
	if errFecha := tmp.Close(); err == nil {
		err = errFecha
	}
	if err == nil {
		err = os.Rename(nomeTmp, arq)
	}
	if err != nil {
		os.Remove(nomeTmp)
		return 0, fmt.Errorf("gravar %s: %w", arq, err)
	}
	return len(z), nil
}

// Le devolve o conteúdo descomprimido (ok falso se o arquivo não existe).
func Le(db *banco.DB, gameID int64, nome string) ([]byte, bool, error) {
	arq, err := caminho(db, gameID, nome)
	if err != nil {
		return nil, false, err
	}
	z, err := os.ReadFile(arq)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	dados, err := Descomprime(z)
	if err != nil {
		return nil, false, fmt.Errorf("descomprimir %s: %w", arq, err)
	}
	return dados, true, nil
}

// Existe diz se o arquivo está no disco.
func Existe(db *banco.DB, gameID int64, nome string) bool {
	arq, err := caminho(db, gameID, nome)
	if err != nil {
		return false
	}
	_, err = os.Stat(arq)
	return err == nil
}

// Apaga remove o arquivo (não existir não é erro).
func Apaga(db *banco.DB, gameID int64, nome string) error {
	arq, err := caminho(db, gameID, nome)
	if err != nil {
		return err
	}
	if err := os.Remove(arq); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Tamanhos devolve os bytes em disco de cada arquivo da partida (nome sem a
// extensão) e o total.
func Tamanhos(db *banco.DB, gameID int64) (map[string]int64, int64) {
	m := map[string]int64{}
	var total int64
	entradas, err := os.ReadDir(PastaDaPartida(db, gameID))
	if err != nil {
		return m, 0
	}
	for _, e := range entradas {
		if e.IsDir() || !strings.HasSuffix(e.Name(), Extensao) {
			continue
		}
		if info, err := e.Info(); err == nil {
			m[strings.TrimSuffix(e.Name(), Extensao)] = info.Size()
			total += info.Size()
		}
	}
	return m, total
}
