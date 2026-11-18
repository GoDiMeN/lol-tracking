package arquivo

import (
	"bytes"
	"strings"
	"testing"

	"loltracking/server/banco"
)

func TestGravaLeApaga(t *testing.T) {
	db, err := banco.AbreMemoria(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	dados := []byte(strings.Repeat("12345\tmudou_zona\t{\"de\":\"rota_meio\",\"para\":\"rio\"}\n", 400))
	n, err := Grava(db, 42, "passe-1-cadeia.tsv", dados)
	if err != nil {
		t.Fatal(err)
	}
	if n <= 0 || n >= len(dados)/10 {
		t.Fatalf("texto repetido devia comprimir bem: %d de %d bytes", n, len(dados))
	}
	lido, ok, err := Le(db, 42, "passe-1-cadeia.tsv")
	if err != nil || !ok || !bytes.Equal(lido, dados) {
		t.Fatalf("leitura diferente da gravação: ok=%v err=%v", ok, err)
	}
	// Regravar troca o conteúdo inteiro.
	if _, err := Grava(db, 42, "passe-1-cadeia.tsv", []byte("novo")); err != nil {
		t.Fatal(err)
	}
	lido, _, _ = Le(db, 42, "passe-1-cadeia.tsv")
	if string(lido) != "novo" {
		t.Fatalf("regravação: %q", lido)
	}
	m, total := Tamanhos(db, 42)
	if len(m) != 1 || total != m["passe-1-cadeia.tsv"] || total == 0 {
		t.Fatalf("tamanhos: %v total %d", m, total)
	}
	if _, ok, err := Le(db, 42, "nao-existe"); ok || err != nil {
		t.Fatalf("arquivo que não existe: ok=%v err=%v", ok, err)
	}
	if err := Apaga(db, 42, "passe-1-cadeia.tsv"); err != nil {
		t.Fatal(err)
	}
	if Existe(db, 42, "passe-1-cadeia.tsv") {
		t.Fatal("o arquivo continuou no disco depois de apagado")
	}
	if err := Apaga(db, 42, "passe-1-cadeia.tsv"); err != nil {
		t.Fatalf("apagar o que não existe não é erro: %v", err)
	}
}

func TestNomeInvalido(t *testing.T) {
	db, err := banco.AbreMemoria(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, nome := range []string{"", "../fora", `a\b`, "a/b", "c:x"} {
		if _, err := Grava(db, 1, nome, []byte("x")); err == nil {
			t.Errorf("nome %q devia ser recusado", nome)
		}
	}
}
