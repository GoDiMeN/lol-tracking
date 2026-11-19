package ia

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"loltracking/server/riot"
)

// riotFalsa conta os pedidos e responde o elo (ou o erro) combinado.
type riotFalsa struct {
	pedidos atomic.Int32
	ent     riot.EntradaLiga
	tem     bool
	err     error
}

func (r *riotFalsa) EloSoloPorRiotID(ctx context.Context, nome, tag string) (riot.EntradaLiga, bool, error) {
	r.pedidos.Add(1)
	return r.ent, r.tem, r.err
}

// ambienteElo: a partida do dono (sem elo da coleta), com referências.
func ambienteElo(t *testing.T, rf *riotFalsa) ambienteTeste {
	t.Helper()
	a := preparaServico(t, nil, t.TempDir(), true)
	a.s.Fontes = fontesFalsas{refs: true, semElo: true}
	if rf != nil {
		a.s.Riot = rf
	}
	return a
}

func vozDe(t *testing.T, m map[string]any, id string) map[string]any {
	t.Helper()
	for _, v := range m["vozes"].([]any) {
		if v.(map[string]any)["voz"] == id {
			return v.(map[string]any)
		}
	}
	t.Fatalf("voz %s não veio: %v", id, m)
	return nil
}

// Sem elo da partida, sem chave e sem escolha: a voz dos melhores do elo diz o
// que falta; escolhido "Platina" na tela, ela tem a comparação com o caçador
// de Platina II, com a amostra escrita; o Challenger diz o que falta.
func TestEloEscolhidoNaTelaLigaAVozDosMelhores(t *testing.T) {
	a := ambienteElo(t, &riotFalsa{err: riot.ErrSemChave})
	cod, m := a.pede(t, "GET", "/api/partidas/3304217746/ia/referencias?participante=7", nil)
	if cod != 200 {
		t.Fatalf("%d %v", cod, m)
	}
	elo := m["elo"].(map[string]any)
	if elo["elo"] != "" || !strings.Contains(elo["aviso_riot"].(string), "sem chave da Riot") || len(elo["opcoes"].([]any)) != 10 {
		t.Fatalf("elo: %v", elo)
	}
	if v := vozDe(t, m, "melhores_do_elo"); v["partidas"].(float64) != 0 || !strings.Contains(v["falta"].(string), "comparar com o elo") {
		t.Fatalf("melhores sem elo: %v", v)
	}
	// Mono: a referência do mesmo campeão (Vi) existe no falso.
	if v := vozDe(t, m, "mono"); v["partidas"].(float64) != 1 {
		t.Fatalf("mono: %v", v)
	}
	if c, x := a.pede(t, "PUT", "/api/partidas/3304217746/ia/elo", map[string]any{"participante": 7, "elo": "lua"}); c != 400 {
		t.Fatalf("elo inválido: %d %v", c, x)
	}
	cod, m = a.pede(t, "PUT", "/api/partidas/3304217746/ia/elo", map[string]any{"participante": 7, "elo": "platinum"})
	if cod != 200 || m["elo"].(map[string]any)["fonte"] != "escolhido" || m["elo"].(map[string]any)["nome"] != "Platina" {
		t.Fatalf("%d %v", cod, m)
	}
	v := vozDe(t, m, "melhores_do_elo")
	if v["partidas"].(float64) != 1 || v["amostra"] != "1 partida de 1 jogador de Platina II na selva" || v["falta"] != "" {
		t.Fatalf("melhores com elo: %v", v)
	}
	if c := vozDe(t, m, "challenger"); !strings.Contains(c["falta"].(string), "faltam partidas de Desafiante na selva: rode a coleta") {
		t.Fatalf("challenger: %v", c)
	}
	// A prévia leva a linha do elo, a tabela lado a lado e a comparação por situação.
	_, pv := a.pede(t, "GET", "/api/partidas/3304217746/ia/previa?participante=7", nil)
	msg := pv["mensagem"].(string)
	for _, x := range []string{"ELO DE COMPARAÇÃO: Platina (escolhido", "### Voz melhores_do_elo", "AMOSTRA: 1 partida de 1 jogador de Platina II na selva. AMOSTRA PEQUENA",
		"| mortes sozinho (aliado longe) | 3 | 4 |", "FOCO: Vi (esta partida)", "ref. 1: Kayn, Platina II, partida 3305481832",
		"- S1 [5:00] janela_perdida: janelas aproveitadas / perdidas: Vi (foco) 0 / 2 · ref. 1 (Kayn) 10 / 1",
		"mortes sozinho na partida: Vi (foco) 3 · ref. 1 (Kayn) 4", "VOZES POR DADO"} {
		if !strings.Contains(msg+pv["sistema"].(string), x) {
			t.Fatalf("faltou %q:\n%s", x, msg)
		}
	}
	if strings.Contains(msg, "Nome7") && strings.Contains(msg, "ref") && strings.Contains(msg, "Nuremberg") {
		t.Fatal("nome de invocador da referência não sai")
	}
	// Voltar ao padrão: "" apaga a escolha.
	if _, m = a.pede(t, "PUT", "/api/partidas/3304217746/ia/elo", map[string]any{"participante": 7, "elo": ""}); m["elo"].(map[string]any)["elo"] != "" {
		t.Fatalf("limpar: %v", m)
	}
}

// Com chave: a Riot preenche o padrão, guarda com a data e não é perguntada de
// novo dentro da validade; a escolha da tela vence a Riot.
func TestEloPelaRiotGuardadoEEscolhaVence(t *testing.T) {
	rf := &riotFalsa{ent: riot.EntradaLiga{Tier: "EMERALD", Rank: "IV", LP: 12, Fila: riot.FilaSolo}, tem: true}
	a := ambienteElo(t, rf)
	_, m := a.pede(t, "GET", "/api/partidas/3304217746/ia/referencias?participante=7", nil)
	elo := m["elo"].(map[string]any)
	if elo["fonte"] != "riot" || elo["nome"] != "Esmeralda IV" || elo["riot"].(map[string]any)["consultado_em"] == "" {
		t.Fatalf("riot: %v", elo)
	}
	a.pede(t, "GET", "/api/partidas/3304217746/ia/referencias?participante=7", nil)
	if rf.pedidos.Load() != 1 {
		t.Fatalf("o elo guardado devia valer: %d pedidos", rf.pedidos.Load())
	}
	var guardado string
	a.db.QueryRow(`SELECT elo_riot FROM ia_elos_jogador WHERE puuid = 'p7'`).Scan(&guardado)
	if guardado != "EMERALD" {
		t.Fatalf("guardado: %q", guardado)
	}
	_, m = a.pede(t, "PUT", "/api/partidas/3304217746/ia/elo", map[string]any{"participante": 7, "elo": "PLATINUM"})
	if e := m["elo"].(map[string]any); e["fonte"] != "escolhido" || e["elo"] != "PLATINUM" || e["riot"].(map[string]any)["elo"] != "EMERALD" {
		t.Fatalf("escolha vence: %v", e)
	}
}

// Chave vencida: aviso na tela, guardado, e sem nova pergunta por 10 min.
func TestEloRiotChaveVencidaNaoInsiste(t *testing.T) {
	rf := &riotFalsa{err: riot.ErrChaveRecusada}
	a := ambienteElo(t, rf)
	for i := 0; i < 2; i++ {
		_, m := a.pede(t, "GET", "/api/partidas/3304217746/ia/referencias?participante=7", nil)
		if e := m["elo"].(map[string]any); !strings.Contains(e["aviso_riot"].(string), "vence em 24 h") || e["elo"] != "" {
			t.Fatalf("aviso: %v", e)
		}
	}
	if rf.pedidos.Load() != 1 {
		t.Fatalf("depois do erro, espera: %d pedidos", rf.pedidos.Load())
	}
}

// Função sem referência: a voz não compara com outra função e diz o que falta.
func TestVozPorDadoSemReferenciaDaFuncao(t *testing.T) {
	a := ambienteElo(t, nil)
	_, m := a.pede(t, "PUT", "/api/partidas/3304217746/ia/elo", map[string]any{"participante": 1, "elo": "PLATINUM"})
	v := vozDe(t, m, "melhores_do_elo")
	if v["partidas"].(float64) != 0 || !strings.Contains(v["falta"].(string), "nenhuma no topo (a voz não compara com outra função)") ||
		!strings.Contains(v["falta"].(string), "faltam vitórias de Platina no topo") {
		t.Fatalf("topo: %v", v)
	}
}

// A resposta da voz por dado sem o tamanho da amostra ganha a amostra na frente.
func TestValidaEscreveAAmostra(t *testing.T) {
	info, _ := fontesFalsas{}.Partida(context.Background(), jogoTeste)
	vozes := []MaterialVoz{{Voz: Vozes[3], Texto: "material", Partidas: 1, Amostra: "1 partida de 1 jogador de Platina II na selva"},
		{Voz: Vozes[2], Texto: "material", Partidas: 1, Amostra: "1 partida de 1 jogador de Platina II na selva"}}
	resp := `{"resumo": "ok", "situacoes": [{"t": "8:48", "titulo": "A", "vozes": [
		{"voz": "melhores_do_elo", "resposta": "Os melhores morrem menos sozinhos."},
		{"voz": "mono", "resposta": "Na única partida de referência, a Vi morreu 4 vezes sozinha."}]}]}`
	c, avisos, err := ValidaRelatorio(resp, info, vozes)
	if err != nil {
		t.Fatal(err)
	}
	vs := c.Situacoes[0].Vozes
	if !strings.HasPrefix(vs[0].Resposta, "(Amostra: 1 partida de 1 jogador de Platina II na selva.) Os melhores") ||
		strings.HasPrefix(vs[1].Resposta, "(Amostra") || len(avisos) != 1 {
		t.Fatalf("%+v %v", vs, avisos)
	}
	if !strings.Contains(c.Vozes[0].Texto, "amostra: 1 partida de 1 jogador") {
		t.Fatalf("vozes: %+v", c.Vozes)
	}
}

func TestNomeElo(t *testing.T) {
	for in, quer := range map[string]string{"PLATINUM II": "Platina II", "challenger": "Desafiante", "": "", "XYZ": "XYZ"} {
		if g := NomeElo(in); g != quer {
			t.Fatalf("%q: %q", in, g)
		}
	}
}
