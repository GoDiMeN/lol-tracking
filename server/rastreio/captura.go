package rastreio

import (
	"context"
	"errors"
	"fmt"
	"log"

	"loltracking/server/banco"
)

// A captura da fila (2026-11-18, pedido do dono: "coloque algo para pausar
// quando quero falar com você"). O que estiver rodando na fila (rastreio,
// repescagem de passe gravado ou passes de visão avulsos) para AGORA com a
// mesma pausa de sempre (o replay fecha; o passe guarda o último t e a
// retomada continua do ponto, sem buraco: TestPausaERetoma), e a fila lembra
// o que parou para "retomar a última pausada". A coleta por elo é do pacote
// aprendizado, que embrulha um rastreio desta fila: quem junta as duas é o
// pacote captura.

// Tipos de corrida.
const (
	TipoRastreio   = "rastreio"
	TipoRepescagem = "repescagem"
	TipoVisao      = "visao"
)

// Corrida é o que está rodando na fila agora.
type Corrida struct {
	Tipo   string  `json:"tipo"`
	ID     int64   `json:"id"`
	GameID int64   `json:"game_id"`
	Exec   *IDExec `json:"execucao"`
	// Esperando: o motor está esperando a pessoa parar de usar o computador.
	Esperando *EsperaUsuario `json:"esperando"`
	// Pausando: a pausa foi pedida e a corrida está fechando o replay.
	Pausando bool `json:"pausando"`
}

// Parada é uma corrida que parou pausada (pelo botão/atalho, ou pelo motor:
// o teto da espera, o disjuntor) e dá para retomar.
type Parada struct {
	Tipo   string `json:"tipo"`
	ID     int64  `json:"id"`
	GameID int64  `json:"game_id"`
	Motivo string `json:"motivo"`
	Em     string `json:"em"`
	// O pedido da repescagem/visão, para começar de novo.
	repescagem PedidoRepescagem
	visao      PedidoVisao
}

func (f *Fila) anotaParada(p Parada) {
	if p.Em == "" {
		p.Em = banco.Agora()
	}
	f.mu.Lock()
	if c := f.atual; c != nil {
		p.repescagem, p.visao = c.pedidoRepescagem, c.pedidoVisao
	}
	f.ultima = &p
	f.mu.Unlock()
}

// CorridaAtual diz o que está rodando (false = nada).
func (f *Fila) CorridaAtual() (Corrida, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.atual
	if c == nil {
		return Corrida{}, false
	}
	r := Corrida{Tipo: TipoRastreio, ID: c.rastreioID, Exec: &IDExec{ID: c.ex.ID()}, Esperando: f.esperando.Load(), Pausando: c.pausando.Load()}
	switch {
	case c.repescagemDe != 0:
		r.Tipo, r.ID, r.GameID = TipoRepescagem, c.repescagemDe, c.repescagemDe
	case c.visaoDe != 0:
		r.Tipo, r.ID, r.GameID = TipoVisao, c.visaoDe, c.visaoDe
	default:
		var g int64
		f.d.DB.QueryRow(`SELECT game_id FROM rastreios WHERE id = ?`, c.rastreioID).Scan(&g)
		r.GameID = g
	}
	return r, true
}

// UltimaParada é a última corrida que parou pausada neste processo (false =
// nenhuma, ou ela já voltou a rodar ou terminou de outro jeito).
func (f *Fila) UltimaParada() (Parada, bool) {
	f.mu.Lock()
	u := f.ultima
	f.mu.Unlock()
	if u == nil {
		return Parada{}, false
	}
	if u.Tipo == TipoRastreio {
		r, err := LeRastreio(f.d.DB, u.ID)
		if err != nil || (r.Estado != Pausado && r.Estado != Falhou) {
			return Parada{}, false
		}
	}
	return *u, true
}

// ErrNadaRodando: pausar sem nada na fila.
var ErrNadaRodando = errors.New("nada está sendo capturado agora")

// PausaAgora para o que estiver rodando na fila, agora: o rastreio fica
// pausado (o replay fecha, o passe guarda o ponto); a repescagem e a visão
// avulsa param (a visão fica pausada e continua do ponto; a repescagem recomeça
// o plano, que é refeito pelo que ficou gravado). Espera a corrida parar.
func (f *Fila) PausaAgora() (Corrida, error) {
	f.mu.Lock()
	c := f.atual
	f.mu.Unlock()
	cor, ok := f.CorridaAtual()
	if c == nil || !ok {
		return Corrida{}, ErrNadaRodando
	}
	// Não espera parar (fechar o replay leva até ~20 s): a corrida termina
	// sozinha e anota a parada; até lá aparece como "pausando".
	c.pausando.Store(true)
	c.cancela(ErrPausado)
	cor.Pausando, cor.Esperando = true, nil
	return cor, nil
}

// RetomaParada recomeça a última corrida pausada. Devolve a execução.
func (f *Fila) RetomaParada(ctx context.Context) (Parada, string, error) {
	p, ok := f.UltimaParada()
	if !ok {
		return Parada{}, "", &ErroEstado{"não há captura pausada para retomar"}
	}
	var exID string
	var err error
	switch p.Tipo {
	case TipoRastreio:
		exID, err = f.Retoma(p.ID)
	case TipoRepescagem:
		exID, _, err = f.Repesca(ctx, p.GameID, p.repescagem)
	case TipoVisao:
		exID, _, err = f.RastreiaVisao(ctx, p.GameID, PedidoVisao{Velocidade: p.visao.Velocidade})
	default:
		err = fmt.Errorf("tipo de captura desconhecido: %s", p.Tipo)
	}
	if err != nil {
		return p, "", err
	}
	f.mu.Lock()
	if f.ultima != nil && f.ultima.Tipo == p.Tipo && f.ultima.ID == p.ID {
		f.ultima = nil
	}
	f.mu.Unlock()
	return p, exID, nil
}

// EsqueceParada: a coleta retomou o rastreio por conta própria.
func (f *Fila) EsqueceParada() {
	f.mu.Lock()
	f.ultima = nil
	f.mu.Unlock()
}

// recomecaSeRuim: o passe parado no meio (pausa, falha, servidor caído) cuja
// parte já gravada cai no critério de passe ruim recomeça do início: as linhas
// de trabalho saem e o t volta ao começo do trecho. A parte boa continua do
// ponto (seek RecuoRetomadaMs antes do último t, como sempre).
func (f *Fila) recomecaSeRuim(ex interface{ Emite(string, any) }, p Passe, r Rastreio) Passe {
	if p.Estado == Pendente || p.Amostras == 0 {
		return p
	}
	as, err := leBrutas(f.d.DB, p.ID, -1<<62, 1<<62)
	if err != nil || len(as) == 0 {
		return p
	}
	av := AvaliaAmostras(as, f.d.Config.AmostrasPorSegundo)
	if !av.Ruim {
		return p
	}
	tx, err := f.d.DB.Begin()
	if err != nil {
		return p
	}
	defer tx.Rollback()
	tx.Exec(`DELETE FROM amostras_brutas WHERE passe_id = ?`, p.ID)
	tx.Exec(`DELETE FROM amostras_time_brutas WHERE passe_id = ?`, p.ID)
	tx.Exec(`UPDATE passes SET amostras = 0, amostras_sem_trava = 0, retravas = 0, tentativas_trava = 0, t_atual_ms = ?, erro = '' WHERE id = ?`,
		r.TInicioMs, p.ID)
	if err := tx.Commit(); err != nil {
		log.Printf("rastreio: recomeçar o passe %d: %v", p.ID, err)
		return p
	}
	msg := fmt.Sprintf("o passe %d (participante %d) parou com a parte gravada ruim (%s): recomeça do início", p.ID, p.ParticipanteID, av.Motivo)
	log.Printf("rastreio %d: %s", r.ID, msg)
	ex.Emite("aviso", map[string]string{"texto": msg})
	np, err := LePasse(f.d.DB, p.ID)
	if err != nil {
		return p
	}
	return np
}

// RefazRuins é o "refazer N passes ruins" da tela da partida: põe um passe
// novo para cada jogador que só tem passe ruim (ARefazer) no rastreio parado
// da partida (que é retomado, e continua os passes que faltavam nele) ou num
// rastreio novo. Devolve o rastreio e a execução.
func (f *Fila) RefazRuins(ctx context.Context, gameID int64, velocidade float64) (Rastreio, string, PassesARefazer, error) {
	ar, err := ARefazer(f.d.DB, gameID)
	if err != nil {
		return Rastreio{}, "", ar, err
	}
	if len(ar.Participantes) == 0 && ar.RastreioPausado == nil {
		return Rastreio{}, "", ar, &ErroEstado{"nenhum passe ruim para refazer nesta partida"}
	}
	if ar.RastreioPausado == nil {
		r, exID, err := f.Cria(ctx, gameID, Pedido{Jogadores: ar.Participantes, Velocidade: velocidade})
		if err == nil {
			marcaRefacoes(f.d.DB, r.ID, ar.Ruins)
		}
		return r, exID, ar, err
	}
	rid := *ar.RastreioPausado
	r, err := LeRastreio(f.d.DB, rid)
	if err != nil {
		return Rastreio{}, "", ar, err
	}
	f.mu.Lock()
	ocupado := f.atual != nil
	var errOcu error
	if ocupado {
		errOcu = f.ocupadoTravado()
	}
	f.mu.Unlock()
	if ocupado {
		return r, "", ar, errOcu
	}
	refaz := map[int]int64{}
	for _, p := range ar.Ruins {
		refaz[p.ParticipanteID] = p.ID
	}
	if err := acrescentaPasses(f.d.DB, rid, gameID, r.TInicioMs, ar.Participantes, refaz); err != nil {
		return r, "", ar, err
	}
	if velocidade > 0 && velocidade != r.Velocidade {
		f.d.DB.Exec(`UPDATE rastreios SET velocidade = ? WHERE id = ?`, velocidade, rid)
	}
	exID, err := f.Retoma(rid)
	if err != nil {
		return r, "", ar, err
	}
	r, _ = LeRastreio(f.d.DB, rid)
	return r, exID, ar, nil
}

// marcaRefacoes liga os passes do rastreio novo aos ruins que eles refazem.
func marcaRefacoes(db *banco.DB, rastreioID int64, ruins []Passe) {
	for _, p := range ruins {
		db.Exec(`UPDATE passes SET refaz_de = ? WHERE rastreio_id = ? AND participante_id = ? AND refaz_de = 0`, p.ID, rastreioID, p.ParticipanteID)
	}
}
