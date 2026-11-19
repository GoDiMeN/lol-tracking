package rastreio

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"loltracking/server/replay"
)

// tiposEventos: os tipos de evento de uma execução, com quantos de cada.
func (a *amb) tiposEventos(t *testing.T, exID string) map[string]int {
	t.Helper()
	ex, ok := a.hub.Busca(exID)
	if !ok {
		t.Fatalf("execução %s sumiu", exID)
	}
	tipos := map[string]int{}
	for _, ev := range ex.Eventos() {
		tipos[ev.Tipo]++
	}
	return tipos
}

// soTeclasPermitidas: nenhuma tecla fora da lista do CLAUDE.md saiu.
func soTeclasPermitidas(t *testing.T, j *replay.Falso) {
	t.Helper()
	for _, k := range j.Teclas {
		ok := false
		for _, p := range []string{"1", "2", "3", "4", "5", "Q", "W", "E", "R", "T", "F1", "F2", "F3"} {
			ok = ok || k == p
		}
		if !ok {
			t.Fatalf("tecla fora da lista: %q", k)
		}
	}
}

func (a *amb) diag(t *testing.T, p Passe) DiagnosticoPasse {
	t.Helper()
	var d DiagnosticoPasse
	if err := json.Unmarshal(p.Diagnostico, &d); err != nil {
		t.Fatalf("diagnóstico %s: %v", p.Diagnostico, err)
	}
	return d
}

// A pessoa começa a digitar em outra janela no meio do passe e a trava cai
// enquanto ela digita: o motor NÃO aperta tecla (não rouba o foco), pausa o
// replay, espera o computador ficar parado, volta do último ponto bom e
// segue. Sem lixo no dado (as amostras sem a câmera do começo da perda saem),
// sem buraco, e a taxa é a de sempre (a espera não conta).
func TestUsuarioDigitandoNoMeioDoPasse(t *testing.T) {
	var jogo *replay.Falso
	var rel *replay.RelogioFalso
	var uma sync.Once
	var usoDe, usoAte time.Time
	a := prepara(t, func(j *replay.Falso, c *Config) {
		jogo = j
		// O jogador 2 (índice 1) perde a câmera de 150 a 165 s na 1ª passagem.
		j.PerdasTrava = []replay.PerdaTravaFalsa{{Indice: 1, De: 150, Ate: 165, Vezes: 1}}
		c.DepoisDaAmostra = func(passeID int64, am Amostra) {
			if am.TMs > 145000 {
				uma.Do(func() {
					usoDe = rel.Agora()
					usoAte = usoDe.Add(20 * time.Second)
					jogo.UsuarioUsa(20 * time.Second)
				})
			}
		}
	})
	rel = a.rel
	r, exID, err := a.fila.Cria(context.Background(), partidaTeste, Pedido{Jogadores: []int{2}, Velocidade: 4})
	if err != nil {
		t.Fatal(err)
	}
	a.fila.Espera()
	r, _ = LeRastreio(a.db, r.ID)
	p := a.passes(t, r.ID)[0]
	if r.Estado != Concluido || p.Estado != Concluido || a.jogo.Aberto() {
		t.Fatalf("rastreio %s passe %s (%s) aberto=%v", r.Estado, p.Estado, p.Erro, a.jogo.Aberto())
	}
	// Nenhuma tecla enquanto a pessoa usava o computador (nem até ela parar
	// por OciosoParaRetomar).
	for i, em := range jogo.TeclasEm {
		if !em.Before(usoDe) && em.Before(usoAte.Add(OciosoParaRetomar)) {
			t.Fatalf("tecla %s saiu em %s, com a pessoa usando o computador (%s a %s)", jogo.Teclas[i], em, usoDe, usoAte)
		}
	}
	soTeclasPermitidas(t, jogo)
	tipos := a.tiposEventos(t, exID)
	if tipos["espera_usuario"] != 1 || tipos["retomada_usuario"] != 1 {
		t.Fatalf("eventos %v", tipos)
	}
	pg, _ := ListaAmostras(a.db, partidaTeste, 2, 0, 1e9, 0, 100000)
	maiorBuraco, sem := int64(0), 0
	for i, am := range pg.Amostras {
		if i > 0 {
			maiorBuraco = max(maiorBuraco, am.TMs-pg.Amostras[i-1].TMs)
		}
		if am.TravaPerdida {
			sem++
		}
	}
	t.Logf("%d amostras, %d sem trava, maior buraco %d ms, taxa %.2f", len(pg.Amostras), sem, maiorBuraco, *p.Taxa)
	if maiorBuraco > 2000 || sem > 2 || *p.Taxa < 3.8 || *p.Taxa > 4.2 {
		t.Fatalf("dado: %d sem trava, maior buraco %d, taxa %.2f", sem, maiorBuraco, *p.Taxa)
	}
	d := a.diag(t, p)
	if d.Esperas != 1 || d.RecusadasUso == 0 || d.EsperaS < 15 {
		t.Fatalf("diagnóstico %+v", d)
	}
}

// A pessoa usa o computador por mais que o teto: a captura pausa de vez (como
// o botão), com o motivo, o replay fecha e a fila lembra para "retomar".
func TestEsperaPassaDoTetoPausaACaptura(t *testing.T) {
	antes := TetoEsperaUsuario
	TetoEsperaUsuario = 30 * time.Second
	t.Cleanup(func() { TetoEsperaUsuario = antes })
	var jogo *replay.Falso
	var uma sync.Once
	a := prepara(t, func(j *replay.Falso, c *Config) {
		jogo = j
		j.PerdasTrava = []replay.PerdaTravaFalsa{{Indice: 1, De: 150, Ate: 165, Vezes: 1}}
		c.DepoisDaAmostra = func(passeID int64, am Amostra) {
			if am.TMs > 145000 {
				uma.Do(func() { jogo.UsuarioUsa(10 * time.Minute) })
			}
		}
	})
	r, _, err := a.fila.Cria(context.Background(), partidaTeste, Pedido{Jogadores: []int{2}, Velocidade: 4})
	if err != nil {
		t.Fatal(err)
	}
	a.fila.Espera()
	r, _ = LeRastreio(a.db, r.ID)
	p := a.passes(t, r.ID)[0]
	if r.Estado != Pausado || p.Estado != Pausado || !strings.Contains(r.Erro, "em uso") || a.jogo.Aberto() {
		t.Fatalf("rastreio %s (%s) passe %s aberto=%v", r.Estado, r.Erro, p.Estado, a.jogo.Aberto())
	}
	if pa, ok := a.fila.UltimaParada(); !ok || pa.Tipo != TipoRastreio || pa.ID != r.ID || pa.Motivo == "" {
		t.Fatalf("última parada %+v %v", pa, ok)
	}
	// O ponto guardado é o último bom (antes da perda): a retomada não deixa vazio.
	if p.TAtualMs > 151500 || p.TAtualMs < 145000 {
		t.Fatalf("t_atual %d", p.TAtualMs)
	}
	// Retoma (pela "última pausada"), já com o computador parado.
	jogo.UsosDoUsuario = nil
	if _, _, err := a.fila.RetomaParada(context.Background()); err != nil {
		t.Fatal(err)
	}
	a.fila.Espera()
	r, _ = LeRastreio(a.db, r.ID)
	if p = a.passes(t, r.ID)[0]; r.Estado != Concluido || p.Estado != Concluido {
		t.Fatalf("retomada: %s %s %s", r.Estado, p.Estado, p.Erro)
	}
	if _, ok := a.fila.UltimaParada(); ok {
		t.Fatal("a parada devia sumir depois de retomada")
	}
}

// Disjuntor: a tecla não prende mais (como nos passes 4, 5, 6 e 8 da
// 3305486988) com o computador parado. O passe não grava o resto da partida
// sem a câmera: dispara, espera (nada a esperar), tenta de novo do ponto bom,
// dispara de novo e a captura pausa com o motivo. O que ficou gravado é bom.
func TestDisjuntorParaOPasseEPausa(t *testing.T) {
	var jogo *replay.Falso
	a := prepara(t, func(j *replay.Falso, c *Config) {
		jogo = j
		// A partir de 100 s a tecla não prende e o jogador nunca está na tela da
		// câmera (nenhuma conferência passa por acaso).
		j.PerdasTrava = []replay.PerdaTravaFalsa{{Indice: 1, De: 100, Ate: 100000, Escondido: true}}
	})
	r, exID, err := a.fila.Cria(context.Background(), partidaTeste, Pedido{Jogadores: []int{2}, Velocidade: 4})
	if err != nil {
		t.Fatal(err)
	}
	a.fila.Espera()
	r, _ = LeRastreio(a.db, r.ID)
	p := a.passes(t, r.ID)[0]
	if r.Estado != Pausado || !strings.Contains(r.Erro, "não prende") || a.jogo.Aberto() {
		t.Fatalf("rastreio %s (%s) aberto=%v", r.Estado, r.Erro, a.jogo.Aberto())
	}
	tipos := a.tiposEventos(t, exID)
	if tipos["disjuntor"] != DisjuntorMaxDisparos || tipos["pausado"] != 1 {
		t.Fatalf("eventos %v", tipos)
	}
	// Poucas teclas (antes eram ~900 por passe) e nada sem a câmera gravado.
	if len(jogo.Teclas) > 2*(1+DisjuntorCiclos)*replay.MaxTentativasTrava*2 {
		t.Fatalf("%d teclas", len(jogo.Teclas))
	}
	soTeclasPermitidas(t, jogo)
	if p.AmostrasSemTrava != 0 || p.TAtualMs > 101000 {
		t.Fatalf("passe %+v", p)
	}
	d := a.diag(t, p)
	if d.Disjuntor != DisjuntorMaxDisparos || d.Conferencia[ConfForaDaTela]+d.Conferencia[ConfSelecaoVazia] == 0 || d.Enviadas == 0 {
		t.Fatalf("diagnóstico %+v", d)
	}
	// A causa passou (a tecla volta a prender): retomar completa o passe, bom.
	jogo.PerdasTrava = nil
	if _, err := a.fila.Retoma(r.ID); err != nil {
		t.Fatal(err)
	}
	a.fila.Espera()
	r, _ = LeRastreio(a.db, r.ID)
	if p = a.passes(t, r.ID)[0]; r.Estado != Concluido || p.Estado != Concluido || p.AmostrasSemTrava > 4 {
		t.Fatalf("retomada: %s %+v", r.Estado, p)
	}
}

// Passe ruim não vale: com o disjuntor desligado, a câmera fica fora do
// jogador em ~60% do passe. Ele fecha "ruim" (sem repescagem), não aparece
// como dado do jogador, e a fila o refaz UMA vez no fim; a refação sai boa.
func TestPasseRuimNaoValeEERefeitoUmaVez(t *testing.T) {
	desligaDisjuntor(t)
	a := prepara(t, func(j *replay.Falso, c *Config) {
		j.PerdasTrava = []replay.PerdaTravaFalsa{{Indice: 1, De: 60, Ate: 250, Vezes: 1}}
	})
	r, exID, err := a.fila.Cria(context.Background(), partidaTeste, Pedido{Jogadores: []int{2}, Velocidade: 4})
	if err != nil {
		t.Fatal(err)
	}
	a.fila.Espera()
	r, _ = LeRastreio(a.db, r.ID)
	ps := a.passes(t, r.ID)
	if r.Estado != Concluido || len(ps) != 2 {
		t.Fatalf("rastreio %s, %d passes", r.Estado, len(ps))
	}
	ruim, novo := ps[0], ps[1]
	if ruim.Estado != Ruim || !strings.HasPrefix(ruim.Erro, "passe ruim") || ruim.Repescagem != nil || !ruim.Compactado {
		t.Fatalf("passe ruim: %+v", ruim)
	}
	if novo.Estado != Concluido || novo.RefazDe != ruim.ID || novo.ParticipanteID != 2 {
		t.Fatalf("refação: %+v", novo)
	}
	if mp, ok, _ := MelhorPasse(a.db, partidaTeste, 2); !ok || mp.ID != novo.ID {
		t.Fatalf("melhor passe %d", mp.ID)
	}
	if cs, _ := PassesCompactados(a.db, partidaTeste); len(cs) != 1 || cs[0].ID != novo.ID {
		t.Fatalf("compactados %+v", cs)
	}
	tipos := a.tiposEventos(t, exID)
	if tipos["passe_ruim"] != 1 || tipos["refazendo"] != 1 || tipos["concluido"] != 1 {
		t.Fatalf("eventos %v", tipos)
	}
	if r.PassesConcluidos != 1 || r.Progresso != 1 {
		t.Fatalf("progresso %+v", r)
	}
}

// A refação também ruim fica ruim: não há terceira tentativa, e o resumo diz
// quem continua sem passe bom. O jogador fica como não rastreado.
func TestPasseRuimRefeitoSoUmaVez(t *testing.T) {
	desligaDisjuntor(t)
	a := prepara(t, func(j *replay.Falso, c *Config) {
		j.PerdasTrava = []replay.PerdaTravaFalsa{{Indice: 1, De: 60, Ate: 250}}
	})
	r, exID, err := a.fila.Cria(context.Background(), partidaTeste, Pedido{Jogadores: []int{2}, Velocidade: 4})
	if err != nil {
		t.Fatal(err)
	}
	a.fila.Espera()
	ps := a.passes(t, r.ID)
	if len(ps) != 2 || ps[0].Estado != Ruim || ps[1].Estado != Ruim || ps[1].RefazDe != ps[0].ID {
		t.Fatalf("passes %+v", ps)
	}
	if _, err := ListaAmostras(a.db, partidaTeste, 2, 0, 1e9, 0, 10); err != ErrSemAmostras {
		t.Fatalf("passe ruim virou dado: %v", err)
	}
	ex, _ := a.hub.Busca(exID)
	var fim map[string]any
	for _, ev := range ex.Eventos() {
		if ev.Tipo == "concluido" {
			json.Unmarshal(ev.Dados, &fim)
		}
	}
	if rs, _ := fim["ruins"].([]any); len(rs) != 1 || rs[0].(float64) != 2 {
		t.Fatalf("resumo %v", fim)
	}
	ar, _ := ARefazer(a.db, partidaTeste)
	if len(ar.Participantes) != 1 || ar.Participantes[0] != 2 || ar.RastreioPausado != nil {
		t.Fatalf("a refazer %+v", ar)
	}
}

// Pausado no meio de um trecho ruim: a retomada recomeça o passe do início em
// vez de deixar valer a parte gravada sem a câmera.
func TestRetomadaRecomecaPasseComParteRuim(t *testing.T) {
	desligaDisjuntor(t)
	var fila *Fila
	var rid int64
	var uma sync.Once
	a := prepara(t, func(j *replay.Falso, c *Config) {
		j.PerdasTrava = []replay.PerdaTravaFalsa{{Indice: 1, De: 20, Ate: 200, Vezes: 1}}
		c.DepoisDaAmostra = func(passeID int64, am Amostra) {
			if am.TMs > 150000 {
				uma.Do(func() { fila.PausaAgora() })
			}
		}
	})
	fila = a.fila
	r, _, err := a.fila.Cria(context.Background(), partidaTeste, Pedido{Jogadores: []int{2}, Velocidade: 4})
	if err != nil {
		t.Fatal(err)
	}
	rid = r.ID
	a.fila.Espera()
	if p := a.passes(t, rid)[0]; p.Estado != Pausado || p.AmostrasSemTrava < 200 {
		t.Fatalf("pausa: %+v", p)
	}
	exID, err := a.fila.Retoma(rid)
	if err != nil {
		t.Fatal(err)
	}
	a.fila.Espera()
	p := a.passes(t, rid)[0]
	if p.Estado != Concluido || p.AmostrasSemTrava > 4 {
		t.Fatalf("retomada: %+v", p)
	}
	pg, _ := ListaAmostras(a.db, partidaTeste, 2, 0, 1e9, 0, 100000)
	if pg.Amostras[0].TMs > 2500 {
		t.Fatalf("não recomeçou do início: primeira amostra em %d", pg.Amostras[0].TMs)
	}
	if a.tiposEventos(t, exID)["aviso"] == 0 {
		t.Fatal("sem o aviso de recomeço")
	}
}

// A pausa da captura (PausaAgora, a mesma do atalho global e do botão) e a
// retomada da última pausada: o passe continua do ponto, sem buraco.
func TestPausaAgoraERetomaAUltima(t *testing.T) {
	var fila *Fila
	var uma sync.Once
	a := prepara(t, func(j *replay.Falso, c *Config) {
		c.DepoisDaAmostra = func(passeID int64, am Amostra) {
			if am.TMs > 120000 {
				uma.Do(func() {
					cor, err := fila.PausaAgora()
					if err != nil || cor.Tipo != TipoRastreio || !cor.Pausando {
						panic("pausa agora falhou")
					}
				})
			}
		}
	})
	fila = a.fila
	if _, err := a.fila.PausaAgora(); err != ErrNadaRodando {
		t.Fatalf("pausar sem nada: %v", err)
	}
	r, _, err := a.fila.Cria(context.Background(), partidaTeste, Pedido{Jogadores: []int{2}, Velocidade: 4})
	if err != nil {
		t.Fatal(err)
	}
	a.fila.Espera()
	r, _ = LeRastreio(a.db, r.ID)
	pa, ok := a.fila.UltimaParada()
	if r.Estado != Pausado || !ok || pa.ID != r.ID || pa.Motivo != "" || a.jogo.Aberto() {
		t.Fatalf("pausa: %s %+v %v", r.Estado, pa, ok)
	}
	if _, _, err := a.fila.RetomaParada(context.Background()); err != nil {
		t.Fatal(err)
	}
	a.fila.Espera()
	r, _ = LeRastreio(a.db, r.ID)
	if r.Estado != Concluido {
		t.Fatalf("retomada %s", r.Estado)
	}
	pg, _ := ListaAmostras(a.db, partidaTeste, 2, 0, 1e9, 0, 100000)
	maior := int64(0)
	for i := 1; i < len(pg.Amostras); i++ {
		maior = max(maior, pg.Amostras[i].TMs-pg.Amostras[i-1].TMs)
	}
	if maior > 2000 {
		t.Fatalf("buraco de %d ms na retomada", maior)
	}
}

func desligaDisjuntor(t *testing.T) {
	c, f := DisjuntorCiclos, DisjuntorFracaoSemTrava
	DisjuntorCiclos, DisjuntorFracaoSemTrava = 1<<30, 2
	t.Cleanup(func() { DisjuntorCiclos, DisjuntorFracaoSemTrava = c, f })
}

// Reclassificação da subida: concluídos com as contagens do dado real dos
// passes 38–40 da 3305486988 viram "ruim"; os bons, não; é idempotente.
func TestReclassificaPassesRuins(t *testing.T) {
	a := prepara(t, nil)
	r, _, err := a.fila.Cria(context.Background(), partidaTeste, Pedido{Jogadores: []int{1, 2}, Velocidade: 4})
	if err != nil {
		t.Fatal(err)
	}
	a.fila.Espera()
	ps := a.passes(t, r.ID)
	// O passe 1 ganha as contagens do passe 38 (4 841 amostras, 3 773 sem trava, taxa 2,18).
	a.db.Exec(`UPDATE passes SET amostras = 4841, amostras_sem_trava = 3773, taxa = 2.18 WHERE id = ?`, ps[0].ID)
	feitos, err := ReclassificaPassesRuins(a.db, 4)
	if err != nil || len(feitos) != 1 || feitos[0].PasseID != ps[0].ID {
		t.Fatalf("%+v %v", feitos, err)
	}
	if !strings.Contains(feitos[0].Motivo, "78%") || !strings.Contains(feitos[0].Motivo, "taxa 2.18") {
		t.Fatalf("motivo %q", feitos[0].Motivo)
	}
	if feitos, _ = ReclassificaPassesRuins(a.db, 4); len(feitos) != 0 {
		t.Fatalf("de novo: %+v", feitos)
	}
	if _, err := ListaAmostras(a.db, partidaTeste, 1, 0, 1e9, 0, 10); err != ErrSemAmostras {
		t.Fatalf("o ruim continua valendo: %v", err)
	}
	if _, err := ListaAmostras(a.db, partidaTeste, 2, 0, 1e9, 0, 10); err != nil {
		t.Fatal(err)
	}
	// Refazer pela rota da partida: cria um rastreio novo só com o 1.
	code, corpo := a.pede(t, "GET", "/api/partidas/"+itoa(partidaTeste)+"/rastreio/ruins", "")
	if code != 200 || len(corpo["participantes"].([]any)) != 1 {
		t.Fatalf("ruins %d %v", code, corpo)
	}
	code, corpo = a.pede(t, "POST", "/api/partidas/"+itoa(partidaTeste)+"/rastreio/refazer", `{"velocidade": 4}`)
	if code != 202 {
		t.Fatalf("refazer %d %v", code, corpo)
	}
	a.fila.Espera()
	rid := int64(corpo["rastreio"].(map[string]any)["id"].(float64))
	novos := a.passes(t, rid)
	if len(novos) != 1 || novos[0].ParticipanteID != 1 || novos[0].Estado != Concluido || novos[0].RefazDe != ps[0].ID {
		t.Fatalf("refação %+v", novos)
	}
	if code, _ = a.pede(t, "POST", "/api/partidas/"+itoa(partidaTeste)+"/rastreio/refazer", `{}`); code != 409 {
		t.Fatalf("nada a refazer devia dar 409, deu %d", code)
	}
}

// "Refazer os passes ruins" com o rastreio da partida parado no meio (o caso da
// 3305486988: rastreio 9 pausado com passes por fazer): os passes novos entram
// no fim DESSE rastreio, que é retomado e termina tudo numa abertura só.
func TestRefazRuinsNoRastreioPausado(t *testing.T) {
	var fila *Fila
	var rid int64
	var uma sync.Once
	a := prepara(t, func(j *replay.Falso, c *Config) {
		c.DepoisDaAmostra = func(passeID int64, am Amostra) {
			if passeID == 2 && am.TMs > 50000 {
				uma.Do(func() { fila.PausaAgora() })
			}
		}
	})
	fila = a.fila
	r, _, err := a.fila.Cria(context.Background(), partidaTeste, Pedido{Jogadores: []int{1, 2}, Velocidade: 4})
	if err != nil {
		t.Fatal(err)
	}
	rid = r.ID
	a.fila.Espera()
	ps := a.passes(t, rid)
	if ps[0].Estado != Concluido || ps[1].Estado != Pausado {
		t.Fatalf("antes: %s %s", ps[0].Estado, ps[1].Estado)
	}
	marcaRuim(a.db, ps[0].ID, "passe ruim: teste")
	rs, _, ar, err := a.fila.RefazRuins(context.Background(), partidaTeste, 0)
	if err != nil || rs.ID != rid || len(ar.Participantes) != 1 || ar.Participantes[0] != 1 {
		t.Fatalf("refaz: %+v %+v %v", rs, ar, err)
	}
	a.fila.Espera()
	r, _ = LeRastreio(a.db, rid)
	ps = a.passes(t, rid)
	if r.Estado != Concluido || len(ps) != 3 || ps[1].Estado != Concluido || ps[2].ParticipanteID != 1 ||
		ps[2].Estado != Concluido || ps[2].RefazDe != ps[0].ID {
		t.Fatalf("depois: %s %+v", r.Estado, ps)
	}
}
