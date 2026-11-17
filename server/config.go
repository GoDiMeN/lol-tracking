package main

import (
	"log"
	"os"
	"strconv"
	"strings"
)

// Config é tudo o que o processo precisa saber antes de subir. Lida uma vez:
// os.Getenv espalhado pelos handlers faria o servidor mudar de comportamento
// conforme a hora em que a variável foi trocada.
type Config struct {
	// Porta em 127.0.0.1 (LOL_PORTA).
	Porta string
	// Dados é a pasta do lol.db e do cache de campeões (LOL_DADOS).
	Dados string
	// ModoTeste liga o LCU falso e o mapa fixo de campeões (LOL_TESTE=1): nada
	// toca no cliente do LoL nem na rede.
	ModoTeste bool
	// Lockfile é onde o cliente do LoL escreve porta e senha (LOL_LOCKFILE).
	Lockfile string
	// TokenRiot é o arquivo da chave opcional da API da Riot (LOL_TOKEN_RIOT).
	TokenRiot string
	// Renderer é a pasta servida em "/" (LOL_RENDERER; vazio = só a API).
	Renderer string
	// GuardaCapturas guarda 1 captura PNG (+ JSON da API) a cada
	// CapturaACadaS segundos de jogo em <Dados>/capturas (LOL_GUARDA_CAPTURAS=1,
	// LOL_CAPTURA_A_CADA): material de teste da leitura da tela.
	GuardaCapturas bool
	CapturaACadaS  float64
}

// PortaPadrao também está em app/src/main/servidor.js: mexeu aqui, mexa lá.
const PortaPadrao = "8770"

// LockfilePadrao é o caminho da instalação padrão do LoL no Windows.
const LockfilePadrao = `C:\Riot Games\League of Legends\lockfile`

// LoadConfig lê as variáveis LOL_*. Valor inválido cai no padrão com aviso:
// travar a subida por uma porta digitada errado deixaria a pessoa sem tela.
func LoadConfig() Config {
	cfg := Config{
		Porta:     PortaPadrao,
		Dados:     "dados",
		ModoTeste: ligado(os.Getenv("LOL_TESTE")),
		Lockfile:  LockfilePadrao,
		TokenRiot: "../token.txt",
		Renderer:  "../app/src/renderer",
	}
	if p := strings.TrimSpace(os.Getenv("LOL_PORTA")); p != "" {
		if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
			log.Printf("AVISO: LOL_PORTA=%q inválida; usando %s", p, PortaPadrao)
		} else {
			cfg.Porta = strconv.Itoa(n)
		}
	}
	if d := strings.TrimSpace(os.Getenv("LOL_DADOS")); d != "" {
		cfg.Dados = d
	}
	if l := strings.TrimSpace(os.Getenv("LOL_LOCKFILE")); l != "" {
		cfg.Lockfile = l
	}
	if t := strings.TrimSpace(os.Getenv("LOL_TOKEN_RIOT")); t != "" {
		cfg.TokenRiot = t
	}
	cfg.GuardaCapturas = ligado(os.Getenv("LOL_GUARDA_CAPTURAS"))
	cfg.CapturaACadaS = 30
	if s := strings.TrimSpace(os.Getenv("LOL_CAPTURA_A_CADA")); s != "" {
		if v, err := strconv.ParseFloat(s, 64); err != nil || v < 1 {
			log.Printf("AVISO: LOL_CAPTURA_A_CADA=%q inválido (segundos ≥ 1); usando 30", s)
		} else {
			cfg.CapturaACadaS = v
		}
	}
	// LookupEnv: "definida e vazia" (não servir a interface) é diferente de
	// "não definida" (servir a pasta padrão).
	if r, definida := os.LookupEnv("LOL_RENDERER"); definida {
		cfg.Renderer = strings.TrimSpace(r)
	}
	return cfg
}

// ligado aceita as grafias que alguém digita num terminal; o documentado é "1".
func ligado(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "sim", "yes", "on":
		return true
	}
	return false
}
