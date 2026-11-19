package main

import (
	"log"
	"os"
	"path/filepath"
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
	// IA (só nas telas de análise e conversa, a pedido; o rastreio não usa):
	// "terminal" (o Claude Code da máquina, padrão) ou "falso" (padrão no
	// LOL_TESTE). LOL_IA.
	IA string
	// IAClaude: o executável do Claude Code (LOL_IA_CLAUDE; padrão "claude"
	// no PATH). IAModelo / IAModeloConversa: o modelo da análise e o da
	// conversa (LOL_IA_MODELO, LOL_IA_MODELO_CONVERSA; vazio = o padrão do
	// pacote ia).
	IAClaude         string
	IAModelo         string
	IAModeloConversa string
	// Conhecimento: a pasta dos arquivos das vozes do relatório
	// (LOL_CONHECIMENTO; padrão <Dados>/conhecimento).
	Conhecimento string
	// NotebookLMCaderno: o id do caderno do espelho do Especialista Lane
	// (LOL_NOTEBOOKLM_CADERNO; vazio = a tela informa). NLM: o comando da
	// ferramenta notebooklm-mcp-cli (LOL_NLM; padrão "nlm" no PATH), com
	// argumentos e aspas se precisar (`uvx --from notebooklm-mcp-cli nlm`); a
	// subida confere a versão e cai no uvx se o configurado não serve
	// (ia/especialista_nlm.go).
	NotebookLMCaderno string
	NLM               string
	// RankingFonte: de onde o ranking do Lane lê o histórico de cada candidato
	// (LOL_RANKING_FONTE): "lcu" (o cliente do LoL; padrão na máquina do dono, não
	// gasta a cota da match-v5) ou "riot" (só a API da Riot, para o servidor sem
	// cliente). O pedido de atualização pode escolher a outra.
	RankingFonte string
	// Estudo: a área de Estudo (coleta por elo, estudos, Especialista Lane,
	// ranking) existe neste servidor (LOL_ESTUDO=1, padrão na máquina do dono).
	// Desligada (LOL_ESTUDO=0), o menu esconde o Estudo e as rotas dele
	// respondem 404; as vozes da IA continuam usando as sínteses já prontas.
	// É a separação para o servidor com outras pessoas: o estudo é só do dono.
	Estudo bool
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
	cfg.IA = "terminal"
	if cfg.ModoTeste {
		cfg.IA = "falso"
	}
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("LOL_IA"))); v != "" {
		if v == "terminal" || v == "falso" {
			cfg.IA = v
		} else {
			log.Printf("AVISO: LOL_IA=%q desconhecida (terminal ou falso); usando %s", v, cfg.IA)
		}
	}
	cfg.IAClaude = strings.TrimSpace(os.Getenv("LOL_IA_CLAUDE"))
	cfg.IAModelo = strings.TrimSpace(os.Getenv("LOL_IA_MODELO"))
	cfg.IAModeloConversa = strings.TrimSpace(os.Getenv("LOL_IA_MODELO_CONVERSA"))
	cfg.Conhecimento = filepath.Join(cfg.Dados, "conhecimento")
	if c := strings.TrimSpace(os.Getenv("LOL_CONHECIMENTO")); c != "" {
		cfg.Conhecimento = c
	}
	cfg.NotebookLMCaderno = strings.TrimSpace(os.Getenv("LOL_NOTEBOOKLM_CADERNO"))
	cfg.NLM = strings.TrimSpace(os.Getenv("LOL_NLM"))
	cfg.RankingFonte = "lcu"
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("LOL_RANKING_FONTE"))); v != "" {
		if v == "lcu" || v == "riot" {
			cfg.RankingFonte = v
		} else {
			log.Printf("AVISO: LOL_RANKING_FONTE=%q desconhecida (lcu ou riot); usando lcu", v)
		}
	}
	cfg.Estudo = true
	if v := strings.TrimSpace(os.Getenv("LOL_ESTUDO")); v != "" {
		switch {
		case ligado(v):
		case desligado(v):
			cfg.Estudo = false
		default:
			log.Printf("AVISO: LOL_ESTUDO=%q desconhecido (0 ou 1); usando 1", v)
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

// desligado é o contrário de ligado (o documentado é "0").
func desligado(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "0", "false", "nao", "não", "no", "off":
		return true
	}
	return false
}
