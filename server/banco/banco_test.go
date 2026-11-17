package banco

import (
	"path/filepath"
	"testing"
)

func TestAbreCriaPastaEMigra(t *testing.T) {
	db, err := Abre(filepath.Join(t.TempDir(), "sub", "lol.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Executa(db, `CREATE TABLE IF NOT EXISTS x (a INTEGER)`); err != nil {
		t.Fatal(err)
	}
	// Rodar de novo não pode falhar: é o que acontece a cada subida.
	if err := Executa(db, `CREATE TABLE IF NOT EXISTS x (a INTEGER)`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := AdicionaColuna(db, "x", "b", "TEXT NOT NULL DEFAULT ''"); err != nil {
			t.Fatalf("rodada %d: %v", i, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO x (a, b) VALUES (1, 'ok')`); err != nil {
		t.Fatal(err)
	}
}

func TestDatas(t *testing.T) {
	if got := DeMilissegundos(0); got != "1970-01-01T00:00:00Z" {
		t.Fatalf("DeMilissegundos(0) = %s", got)
	}
	if len(NovoID()) != 36 {
		t.Fatal("NovoID com tamanho errado")
	}
}
