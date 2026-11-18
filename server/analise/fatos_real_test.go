//go:build real

package analise

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"

	"loltracking/server/banco"
	"loltracking/server/campeao"
)

// TestRealFatos gera e imprime a base de fatos de uma partida num banco com os
// passes reais (uma CÓPIA; não abre o jogo nem chama o LCU):
// LOL_BANCO_REAL=<cópia>/lol.db, LOL_PARTIDA (padrão 3304217746),
// LOL_PARTICIPANTE (padrão 7) e LOL_CONTEXTO_SAIDA (arquivo para o pacote).
// Os estáticos vêm do cache em <pasta do banco>/estaticos.
func TestRealFatos(t *testing.T) {
	arq := os.Getenv("LOL_BANCO_REAL")
	if arq == "" {
		t.Skip("LOL_BANCO_REAL não definido")
	}
	db, err := banco.Abre(arq)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Migra(db); err != nil {
		t.Fatal(err)
	}
	g := int64(3304217746)
	if s := os.Getenv("LOL_PARTIDA"); s != "" {
		g, _ = strconv.ParseInt(s, 10, 64)
	}
	pid := 7
	if s := os.Getenv("LOL_PARTICIPANTE"); s != "" {
		pid, _ = strconv.Atoi(s)
	}
	d := Deps{DB: db, Estaticos: campeao.NovosEstaticos(filepath.Dir(arq))}
	if err := GeraFatosDaPartida(context.Background(), d, g); err != nil {
		t.Fatal(err)
	}
	r, err := LeFatos(db, g)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s", r.Texto)
	t.Logf("avisos: %v", r.Avisos)
	var chave []MomentoChave
	tipos := map[string]int{}
	for _, m := range r.Momentos {
		tipos[m.Tipo]++
		if m.Chave {
			chave = append(chave, m)
		}
	}
	t.Logf("momentos por tipo: %v", tipos)
	sort.SliceStable(chave, func(a, b int) bool { return chave[a].Impacto > chave[b].Impacto })
	for _, m := range chave {
		t.Logf("CHAVE impacto %d [%s] %s %s", m.Impacto, MinSeg(m.TMs), m.Tipo, m.Frase)
	}
	for _, m := range r.Momentos {
		if (m.Tipo == "janela_perdida" || m.Tipo == "janela_aproveitada") && m.ParticipanteID == pid {
			t.Logf("JANELA %s", linhaMomento(m))
		}
	}
	for _, j := range r.Jogadores {
		t.Logf("JOGADOR %d %s nota %d (%dº) comp %v ctx %+v", j.ParticipanteID, j.Campeao, j.Nota, j.PosicaoNota, j.Componentes, j.Contexto)
	}
	c, err := ContextoDaPartida(db, g, pid)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("contexto: %d caracteres (limite %d), cortadas %v", c.Caracteres, c.Limite, c.Cortadas)
	if s := os.Getenv("LOL_CONTEXTO_SAIDA"); s != "" {
		if err := os.WriteFile(s, []byte(c.Texto), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var tamP, tamJ, tamM, nM int
	db.QueryRow(`SELECT LENGTH(texto) + LENGTH(dados) + 200 FROM resumos_partida WHERE game_id = ?`, g).Scan(&tamP)
	db.QueryRow(`SELECT SUM(LENGTH(etiquetas) + LENGTH(dados) + LENGTH(nome) + LENGTH(puuid) + 250) FROM resumos_jogador WHERE game_id = ?`, g).Scan(&tamJ)
	db.QueryRow(`SELECT SUM(LENGTH(frase) + LENGTH(dados) + LENGTH(tags) + LENGTH(envolvidos) + 150), COUNT(*) FROM momentos_chave WHERE game_id = ?`, g).Scan(&tamM, &nM)
	t.Logf("tamanho aproximado no banco: partida %d B, jogadores %d B, momentos %d B (%d linhas)", tamP, tamJ, tamM, nM)
}
