package main

import "testing"

func TestLoadConfigPadroes(t *testing.T) {
	for _, v := range []string{"LOL_PORTA", "LOL_DADOS", "LOL_TESTE", "LOL_LOCKFILE", "LOL_TOKEN_RIOT"} {
		t.Setenv(v, "")
	}
	cfg := LoadConfig()
	if cfg.Porta != "8770" || cfg.Dados != "dados" || cfg.ModoTeste || cfg.Lockfile != LockfilePadrao || cfg.TokenRiot != "../token.txt" {
		t.Fatalf("%+v", cfg)
	}
}

func TestLoadConfigVariaveis(t *testing.T) {
	t.Setenv("LOL_PORTA", "9001")
	t.Setenv("LOL_DADOS", "/tmp/x")
	t.Setenv("LOL_TESTE", "1")
	t.Setenv("LOL_LOCKFILE", "/tmp/lockfile")
	t.Setenv("LOL_TOKEN_RIOT", "/tmp/token.txt")
	t.Setenv("LOL_RENDERER", "")
	cfg := LoadConfig()
	if cfg.Porta != "9001" || cfg.Dados != "/tmp/x" || !cfg.ModoTeste || cfg.Lockfile != "/tmp/lockfile" ||
		cfg.TokenRiot != "/tmp/token.txt" || cfg.Renderer != "" {
		t.Fatalf("%+v", cfg)
	}
	t.Setenv("LOL_PORTA", "abc")
	if LoadConfig().Porta != PortaPadrao {
		t.Fatal("porta inválida deveria cair no padrão")
	}
}
