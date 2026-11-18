// Package banco abre o SQLite do LoL Tracking e guarda o pouco que é comum a
// todos os pacotes: o tipo do banco, o carimbo de data e o laço de migração.
// Não tem consulta de negócio: quem sabe ler `partidas` é o pacote partida,
// quem sabe de `jogadores` é o pacote jogador.
package banco

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	// O driver só é registrado aqui: dois registros do mesmo nome derrubam o
	// processo na subida. modernc.org/sqlite é Go puro (sem CGO, sem gcc).
	_ "modernc.org/sqlite"
)

// DB embute *sql.DB: o tipo próprio deixa claro nas assinaturas dos models
// que é o banco do app, sem esconder Query/Exec.
type DB struct {
	*sql.DB
	// Pasta é onde está o arquivo do banco: os arquivos pesados das partidas
	// (pacote arquivo) ficam ao lado dele.
	Pasta string
}

// Abre cria a pasta do arquivo e abre o SQLite. Os pragmas vão no DSN porque
// o database/sql tem um pool, e um PRAGMA rodado por Exec valeria só para a
// conexão que o executou.
//   - journal_mode=WAL: leitura (a lista de partidas) não espera escrita (o
//     download gravando a linha do tempo).
//   - busy_timeout=5000: dá tempo para a escrita concorrente terminar em vez
//     de devolver "database is locked" na hora.
//   - _txlock=immediate: a transação já pega a trava de escrita no BEGIN, e
//     duas transações não se travam ao promover leitura para escrita.
func Abre(caminho string) (*DB, error) {
	if dir := filepath.Dir(caminho); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("criar a pasta do banco %s: %w", dir, err)
		}
	}
	dsn := "file:" + filepath.ToSlash(caminho) +
		"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_txlock=immediate"
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("abrir o sqlite em %s: %w", caminho, err)
	}
	// O Ping roda os pragmas e acusa pasta sem permissão já na subida.
	if err := sqlDB.Ping(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("abrir o sqlite em %s: %w", caminho, err)
	}
	return &DB{DB: sqlDB, Pasta: filepath.Dir(caminho)}, nil
}

// AbreMemoria é o banco dos testes: um arquivo na pasta temporária do teste,
// e não ":memory:", porque cada conexão do pool teria a sua própria memória.
func AbreMemoria(dir string) (*DB, error) {
	return Abre(filepath.Join(dir, "teste.db"))
}

// Executa roda os comandos de uma migração em ordem. Cada pacote escreve o
// seu SQL com CREATE ... IF NOT EXISTS, então rodar de novo na subida não muda
// nada; coluna nova num pacote entra com AdicionaColuna.
func Executa(db *DB, comandos ...string) error {
	for _, c := range comandos {
		if _, err := db.Exec(c); err != nil {
			return fmt.Errorf("migração: %w", err)
		}
	}
	return nil
}

// AdicionaColuna acrescenta uma coluna se ela ainda não existe. O SQLite não
// tem ADD COLUMN IF NOT EXISTS, então consultamos o pragma antes.
func AdicionaColuna(db *DB, tabela, coluna, definicao string) error {
	linhas, err := db.Query("SELECT name FROM pragma_table_info(?)", tabela)
	if err != nil {
		return err
	}
	defer linhas.Close()
	for linhas.Next() {
		var nome string
		if err := linhas.Scan(&nome); err != nil {
			return err
		}
		if nome == coluna {
			return nil
		}
	}
	if err := linhas.Err(); err != nil {
		return err
	}
	_, err = db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", tabela, coluna, definicao))
	return err
}

// FormatoData é o ISO 8601 em UTC gravado no banco e devolvido no JSON: o
// `new Date()` do renderer lê sem biblioteca.
const FormatoData = "2006-01-02T15:04:05Z"

// Agora é o carimbo de data de toda linha gravada.
func Agora() string {
	return time.Now().UTC().Format(FormatoData)
}

// DeMilissegundos converte o carimbo em ms do LCU (gameCreation) para o
// formato do banco.
func DeMilissegundos(ms int64) string {
	return time.UnixMilli(ms).UTC().Format(FormatoData)
}

// NovoID devolve um id aleatório (UUID v4 em texto) para as execuções.
func NovoID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("banco: crypto/rand indisponível: " + err.Error())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}
