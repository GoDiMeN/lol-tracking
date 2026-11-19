package rastreio

import (
	"fmt"
	"log"
	"sort"
	"strings"

	"loltracking/server/banco"
)

// Passe ruim não vale (2026-11-18). Os passes 38, 39 e 40 da 3305486988
// fecharam como concluídos com 71–78% das amostras sem a câmera no jogador,
// taxa 2,18–2,35 (pedido: 4 por segundo de jogo) e nenhum ponto de calibração;
// a análise e a base de fatos usavam aquilo como o jogo do jogador. Agora o
// passe que cai num destes critérios fecha como "ruim": o bloco fica (para
// diagnóstico), mas nada o lê como dado do jogador (melhorPasse, estado dos
// 10, cadeia, análise), a repescagem não roda nele e a fila o refaz uma vez.

// Limiares do critério.
const (
	// FracaoSemTravaRuim (X): amostras vivas sem a câmera no jogador acima
	// disso. Bons: 0,3% (3305486988) a 4,5% (o Master Yi da 3305449546).
	FracaoSemTravaRuim = 0.25
	// TaxaMinimaRelativa (Y): a taxa (amostras por segundo de jogo) abaixo
	// disso × a pedida (4) = 3,0. Medido: 3,98 a 2× e 4×, 3,5–3,67 a 8×; os
	// ruins tinham 2,18–2,35 (o motor parado nas teclas).
	TaxaMinimaRelativa = 0.75
	// ConferenciaMinimaN e ConferenciaMinimaFracao (Z): conferido com a Riot
	// em pelo menos N valores, menos de Z deles iguais. Bons: 252/252, 265/270.
	ConferenciaMinimaN      = 10
	ConferenciaMinimaFracao = 0.5
	// MinAmostrasParaJulgar: passe curto demais não é julgado.
	MinAmostrasParaJulgar = 40
	// MaxRefacoesAutomaticas: quantas vezes a fila refaz sozinha o passe ruim
	// de um jogador no mesmo rastreio (no fim dos outros passes).
	MaxRefacoesAutomaticas = 1
)

// Ruim é o estado do passe que fechou e não vale (fim de linha, como
// concluído e cancelado).
const Ruim = "ruim"

// AvaliacaoPasse é o resultado do critério.
type AvaliacaoPasse struct {
	Ruim           bool    `json:"ruim"`
	Motivo         string  `json:"motivo"`
	Vivas          int     `json:"vivas"`
	SemTrava       int     `json:"sem_trava"`
	FracaoSemTrava float64 `json:"fracao_sem_trava"`
	Taxa           float64 `json:"taxa"`
}

// AvaliaContagem aplica o critério às contagens do passe. alvoTaxa = as
// amostras por segundo pedidas (0 = não confere a taxa). confN/confIguais nil
// = não conferido com a Riot.
func AvaliaContagem(vivas, semTrava int, taxa, alvoTaxa float64, confN, confIguais *int64) AvaliacaoPasse {
	av := AvaliacaoPasse{Vivas: vivas, SemTrava: semTrava, Taxa: taxa}
	if vivas > 0 {
		av.FracaoSemTrava = float64(semTrava) / float64(vivas)
	}
	var motivos []string
	if vivas >= MinAmostrasParaJulgar && av.FracaoSemTrava > FracaoSemTravaRuim {
		motivos = append(motivos, fmt.Sprintf("%.0f%% das amostras vivas sem a câmera no jogador (limite %.0f%%)",
			100*av.FracaoSemTrava, 100*FracaoSemTravaRuim))
	}
	if alvoTaxa > 0 && vivas >= MinAmostrasParaJulgar && taxa > 0 && taxa < TaxaMinimaRelativa*alvoTaxa {
		motivos = append(motivos, fmt.Sprintf("taxa %.2f por segundo de jogo (mínimo %.2f)", taxa, TaxaMinimaRelativa*alvoTaxa))
	}
	if confN != nil && confIguais != nil && *confN >= ConferenciaMinimaN && float64(*confIguais) < ConferenciaMinimaFracao*float64(*confN) {
		motivos = append(motivos, fmt.Sprintf("conferência com a Riot %d de %d", *confIguais, *confN))
	}
	if len(motivos) > 0 {
		av.Ruim = true
		av.Motivo = "passe ruim: " + strings.Join(motivos, "; ")
	}
	return av
}

// AvaliaAmostras aplica o critério às amostras do passe (sem a conferência,
// que vem depois do fechamento).
func AvaliaAmostras(as []Amostra, alvoTaxa float64) AvaliacaoPasse {
	vivas, sem := 0, 0
	for _, a := range as {
		if a.Morto {
			continue
		}
		vivas++
		if a.TravaPerdida {
			sem++
		}
	}
	return AvaliaContagem(vivas, sem, taxaDe(as), alvoTaxa, nil, nil)
}

// marcaRuim muda um passe fechado para "ruim" com o motivo.
func marcaRuim(db *banco.DB, passeID int64, motivo string) error {
	_, err := db.Exec(`UPDATE passes SET estado = ?, erro = ? WHERE id = ?`, Ruim, motivo, passeID)
	esqueceCache(passeID)
	return err
}

// PasseReclassificado é um passe que a reclassificação da subida mudou.
type PasseReclassificado struct {
	PasseID        int64  `json:"passe_id"`
	GameID         int64  `json:"game_id"`
	ParticipanteID int    `json:"participante_id"`
	Motivo         string `json:"motivo"`
}

// ReclassificaPassesRuins percorre os passes concluídos com bloco e muda para
// "ruim" os que caem no critério (pelas contagens gravadas: amostras, sem
// trava, taxa e conferência; as amostras gravadas incluem as mortas, então a
// fração aqui é um pouco MENOR que a do fechamento: nunca marca a mais).
// alvoTaxa: as amostras por segundo pedidas. Roda a cada subida e é
// idempotente (só mexe em concluído).
func ReclassificaPassesRuins(db *banco.DB, alvoTaxa float64) ([]PasseReclassificado, error) {
	ps, err := lePasses(db, `WHERE p.estado = 'concluido' AND EXISTS (SELECT 1 FROM passes_compactos c WHERE c.passe_id = p.id)
		ORDER BY p.id`)
	if err != nil {
		return nil, err
	}
	var feitos []PasseReclassificado
	for _, p := range ps {
		taxa := 0.0
		if p.Taxa != nil {
			taxa = *p.Taxa
		}
		av := AvaliaContagem(p.Amostras, p.AmostrasSemTrava, taxa, alvoTaxa, p.ConferenciaN, p.ConferenciaIguais)
		if !av.Ruim {
			continue
		}
		if err := marcaRuim(db, p.ID, av.Motivo); err != nil {
			return feitos, err
		}
		feitos = append(feitos, PasseReclassificado{PasseID: p.ID, GameID: p.GameID, ParticipanteID: p.ParticipanteID, Motivo: av.Motivo})
	}
	return feitos, nil
}

// PassesARefazer são os jogadores da partida cujo passe ruim não tem passe
// bom depois: o "refazer N passes ruins" da tela da partida.
type PassesARefazer struct {
	GameID        int64   `json:"game_id"`
	Ruins         []Passe `json:"ruins"`
	Participantes []int   `json:"participantes"`
	// RastreioPausado: o rastreio parado da partida em que os passes novos
	// entram (e que a ação retoma); nulo = a ação cria um rastreio novo.
	RastreioPausado *int64 `json:"rastreio_pausado"`
}

// ARefazer monta a lista: para cada participante, o passe ruim mais recente,
// se ele não tem passe concluído nem um passe novo ainda por rodar.
func ARefazer(db *banco.DB, gameID int64) (PassesARefazer, error) {
	r := PassesARefazer{GameID: gameID, Ruins: []Passe{}, Participantes: []int{}}
	ps, err := PassesDaPartida(db, gameID)
	if err != nil {
		return r, err
	}
	bom, pendente := map[int]bool{}, map[int]bool{}
	ruim := map[int]Passe{}
	for _, p := range ps {
		switch p.Estado {
		case Concluido:
			bom[p.ParticipanteID] = true
		case Ruim:
			ruim[p.ParticipanteID] = p
		case Pendente, Pausado, Rodando, Falhou:
			pendente[p.ParticipanteID] = true
		}
	}
	for pid, p := range ruim {
		if !bom[pid] && !pendente[pid] {
			r.Ruins = append(r.Ruins, p)
			r.Participantes = append(r.Participantes, pid)
		}
	}
	sort.Ints(r.Participantes)
	var rid int64
	if err := db.QueryRow(`SELECT id FROM rastreios WHERE game_id = ? AND estado IN ('pausado','falhou') ORDER BY id DESC LIMIT 1`,
		gameID).Scan(&rid); err == nil {
		r.RastreioPausado = &rid
	}
	return r, nil
}

// acrescentaPasses põe passes novos (pendentes) no fim do rastreio. refazDe:
// participante → o passe ruim que o novo refaz (0 = nenhum).
func acrescentaPasses(db *banco.DB, rastreioID, gameID int64, tIni int64, pids []int, refazDe map[int]int64) error {
	var ordem int
	db.QueryRow(`SELECT COALESCE(MAX(ordem), -1) FROM passes WHERE rastreio_id = ?`, rastreioID).Scan(&ordem)
	for _, pid := range pids {
		ordem++
		if _, err := db.Exec(`INSERT INTO passes (rastreio_id, game_id, participante_id, ordem, t_atual_ms, refaz_de) VALUES (?, ?, ?, ?, ?, ?)`,
			rastreioID, gameID, pid, ordem, tIni, refazDe[pid]); err != nil {
			return err
		}
	}
	return nil
}

// refacoesAutomaticas cria, no fim do rastreio, um passe novo para cada passe
// ruim dele que ainda pode ser refeito (MaxRefacoesAutomaticas: um passe que
// já é refação não é refeito de novo). Devolve os participantes.
func refacoesAutomaticas(db *banco.DB, r Rastreio) ([]int, error) {
	ps, err := lePasses(db, `WHERE p.rastreio_id = ? ORDER BY p.ordem`, r.ID)
	if err != nil {
		return nil, err
	}
	refeito := map[int64]bool{}
	cadeia := map[int64]int{} // passe → quantas refações já houve até ele
	for _, p := range ps {
		if p.RefazDe != 0 {
			refeito[p.RefazDe] = true
			cadeia[p.ID] = cadeia[p.RefazDe] + 1
		}
	}
	var pids []int
	refaz := map[int]int64{}
	vistos := map[int]bool{}
	for _, p := range ps {
		if p.Estado != Ruim || refeito[p.ID] || cadeia[p.ID] >= MaxRefacoesAutomaticas || vistos[p.ParticipanteID] {
			continue
		}
		vistos[p.ParticipanteID] = true
		pids = append(pids, p.ParticipanteID)
		refaz[p.ParticipanteID] = p.ID
	}
	if len(pids) == 0 {
		return nil, nil
	}
	return pids, acrescentaPasses(db, r.ID, r.GameID, r.TInicioMs, pids, refaz)
}

// LogReclassificados escreve no log o que a subida mudou.
func LogReclassificados(feitos []PasseReclassificado) {
	for _, f := range feitos {
		log.Printf("rastreio: passe %d (partida %d, participante %d) reclassificado como ruim: %s", f.PasseID, f.GameID, f.ParticipanteID, f.Motivo)
	}
}
