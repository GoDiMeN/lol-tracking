package partida

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"loltracking/server/lcu"
	"loltracking/server/web"
)

// Estados do replay na linguagem do app (ver Replay).
const (
	Baixavel     = "baixavel"
	Baixando     = "baixando"
	Baixado      = "baixado"
	Incompativel = "incompativel"
	Indisponivel = "indisponivel"
)

// Tempos do download. Variáveis só para o teste encurtar.
var (
	// IntervaloPoll: de quanto em quanto o metadata é consultado durante o download.
	IntervaloPoll = time.Second
	// PrazoDownload: um replay tem ~15 MB; 15 min cobre conexão ruim com folga.
	PrazoDownload = 15 * time.Minute
	// EsperaCriacao: pausa entre consultas logo depois do create, enquanto o
	// plugin ainda não respondeu ou está em "checking".
	EsperaCriacao = 300 * time.Millisecond
)

// traduz converte o estado do LCU para o do app.
func traduz(m lcu.MetadataReplay) Replay {
	r := Replay{EstadoLCU: m.Estado, Progresso: m.Progresso}
	switch m.Estado {
	case "download", "retryDownload":
		r.Estado = Baixavel
	case "downloading", "checking":
		r.Estado = Baixando
	case "watch":
		r.Estado, r.Progresso = Baixado, 100
	case "incompatible":
		r.Estado = Incompativel
		r.Detalhe = "o replay é de outro patch e só abre na versão em que a partida foi jogada"
	default:
		r.Estado = Indisponivel
		r.Detalhe = "o cliente do LoL não oferece este replay (estado \"" + m.Estado + "\")"
	}
	return r
}

// infoReplay é o que o create do metadata precisa saber da partida.
type infoReplay struct {
	GameID     int64
	Versao     string
	Tipo       string
	Fila       int
	CriadaEmMs int64
	DuracaoS   int
}

func leInfo(d Deps, gameID int64) (infoReplay, error) {
	i := infoReplay{GameID: gameID}
	err := d.DB.QueryRow(`SELECT versao, tipo, fila, criada_em_ms, duracao_s FROM partidas WHERE game_id = ?`, gameID).
		Scan(&i.Versao, &i.Tipo, &i.Fila, &i.CriadaEmMs, &i.DuracaoS)
	return i, err
}

// garantePartida busca a partida inteira no LCU se ela ainda não está completa
// no banco.
func garantePartida(ctx context.Context, d Deps, gameID int64) error {
	if estaCompleta(d.DB, gameID) {
		return nil
	}
	bruto, err := d.LCU.Partida(ctx, gameID)
	if errors.Is(err, lcu.ErrNaoEncontrado) {
		return ErrNaoEncontrada
	}
	if err != nil {
		return err
	}
	return Grava(ctx, d, bruto, true)
}

// ConsultaReplay diz o estado do replay de uma partida (que precisa estar no
// banco). Quando o cliente responde 404 ("Plugin found no local metadata"), a
// partida é apresentada ao plugin pelo v2/metadata/{id}/create e consultada de
// novo, como o próprio cliente faz ao abrir o histórico. Partida de outro patch
// não é apresentada: o replay dela não abre de jeito nenhum, e assim o app não
// escreve à toa no cliente.
func ConsultaReplay(ctx context.Context, d Deps, gameID int64, versaoCliente string) (Replay, error) {
	info, err := leInfo(d, gameID)
	if err != nil {
		return Replay{}, fmt.Errorf("partida %d não está no banco: %w", gameID, err)
	}
	m, err := d.LCU.MetadataReplay(ctx, gameID)
	if err == nil {
		return traduz(m), nil
	}
	if !errors.Is(err, lcu.ErrNaoEncontrado) {
		return Replay{}, err
	}
	if versaoCliente != "" && lcu.PatchDe(info.Versao) != lcu.PatchDe(versaoCliente) {
		return Replay{Estado: Incompativel, Detalhe: fmt.Sprintf("replay do patch %s; o jogo instalado é %s",
			lcu.PatchDe(info.Versao), lcu.PatchDe(versaoCliente))}, nil
	}
	tipo := info.Tipo
	if tipo == "" {
		tipo = "MATCHED_GAME"
	}
	cria := lcu.CriaMetadata{VersaoJogo: info.Versao, TipoJogo: tipo, FilaID: info.Fila,
		FimMs: info.CriadaEmMs + int64(info.DuracaoS)*1000}
	if err := d.LCU.CriaMetadataReplay(ctx, gameID, cria); err != nil {
		return Replay{}, fmt.Errorf("apresentar a partida %d ao plugin de replays: %w", gameID, err)
	}
	for tentativa := 0; tentativa < 6; tentativa++ {
		m, err = d.LCU.MetadataReplay(ctx, gameID)
		if err == nil && m.Estado != "checking" {
			return traduz(m), nil
		}
		if err != nil && !errors.Is(err, lcu.ErrNaoEncontrado) {
			return Replay{}, err
		}
		select {
		case <-ctx.Done():
			return Replay{}, ctx.Err()
		case <-time.After(EsperaCriacao):
		}
	}
	if err == nil {
		return traduz(m), nil
	}
	return Replay{Estado: Indisponivel, Detalhe: "o cliente do LoL não reconheceu o replay desta partida"}, nil
}

// guardaLinhaDoTempo busca e grava a linha do tempo, se ainda não há.
func guardaLinhaDoTempo(ctx context.Context, d Deps, gameID int64) error {
	if temLinhaDoTempo(d.DB, gameID) {
		return nil
	}
	bruto, err := d.LCU.LinhaDoTempo(ctx, gameID)
	if err != nil {
		return err
	}
	return GravaLinhaDoTempo(d.DB, gameID, bruto)
}

// fasesAoVivo são as fases do gameflow em que há partida de verdade em jogo
// ou para começar. Download de replay nessas horas disputaria rede e disco com
// a partida; o app espera.
var fasesAoVivo = map[string]bool{
	"ReadyCheck": true, "ChampSelect": true, "GameStart": true, "InProgress": true, "Reconnect": true,
}

// MsgPartidaAoVivo é a frase da recusa por partida ao vivo.
const MsgPartidaAoVivo = "há uma partida em andamento no cliente do LoL; baixe o replay depois que ela terminar"

// Baixa é o trabalho da execução de download. Eventos:
//   - "progresso" {"estado","progresso","estado_lcu"} a cada mudança;
//   - "concluido" {"replay": Replay, "linha_do_tempo": bool};
//   - "erro" {"texto"} e "fim".
func Baixa(ctx context.Context, d Deps, ex *web.Execucao, gameID int64) {
	defer ex.Fecha()
	falha := func(msg string, err error) {
		if err != nil {
			log.Printf("partida: download do replay %d: %s: %v", gameID, msg, err)
			if _, frase, ok := lcu.StatusHTTP(err); ok {
				msg = frase
			}
		}
		ex.Falha(msg)
	}

	fase, err := d.LCU.FaseGameflow(ctx)
	if err != nil {
		falha("não foi possível falar com o cliente do LoL", err)
		return
	}
	if fasesAoVivo[fase] {
		falha(MsgPartidaAoVivo, nil)
		return
	}
	if err := garantePartida(ctx, d, gameID); err != nil {
		if errors.Is(err, ErrNaoEncontrada) {
			falha("a partida não foi encontrada no cliente do LoL", nil)
			return
		}
		falha("não foi possível ler a partida no cliente do LoL", err)
		return
	}
	cfg, err := d.LCU.ConfiguracaoReplays(ctx)
	if err != nil {
		falha("não foi possível ler a configuração de replays do cliente", err)
		return
	}
	r, err := ConsultaReplay(ctx, d, gameID, cfg.VersaoJogo)
	if err != nil {
		falha("não foi possível consultar o replay", err)
		return
	}
	ex.Emite("progresso", r)
	switch r.Estado {
	case Baixado:
		conclui(ctx, d, ex, gameID, r)
		return
	case Incompativel, Indisponivel:
		GravaReplay(d.DB, gameID, r)
		falha("este replay não pode ser baixado: "+r.Detalhe, nil)
		return
	case Baixavel:
		if err := d.LCU.BaixaReplay(ctx, gameID); err != nil {
			// A Riot anunciou em 2026 que vai limitar o download a partidas da
			// própria conta; se a recusa vier, é provavelmente isso.
			falha("o cliente do LoL recusou o download (a Riot pode ter limitado o download a partidas da sua conta)", err)
			return
		}
	}
	// Baixando (o pedido acima, ou um download que já estava em andamento).
	prazo := time.NewTimer(PrazoDownload)
	defer prazo.Stop()
	ultimo := r
	for {
		select {
		case <-ctx.Done():
			// Cancelar para de acompanhar; o cliente do LoL continua baixando
			// (o LCU não tem rota de cancelar download, e a lista não deixaria).
			ex.Emite("aviso", map[string]string{"texto": "acompanhamento cancelado; o cliente do LoL pode terminar o download sozinho"})
			return
		case <-prazo.C:
			falha("o download passou de 15 minutos sem terminar", nil)
			return
		case <-time.After(IntervaloPoll):
		}
		m, err := d.LCU.MetadataReplay(ctx, gameID)
		if err != nil {
			if ctx.Err() != nil {
				continue
			}
			falha("o cliente do LoL parou de responder durante o download", err)
			return
		}
		r = traduz(m)
		if r != ultimo {
			ex.Emite("progresso", r)
			GravaReplay(d.DB, gameID, r)
			ultimo = r
		}
		switch r.Estado {
		case Baixado:
			conclui(ctx, d, ex, gameID, r)
			return
		case Baixando:
		case Baixavel:
			// Voltou para "download": o cliente desistiu (retryDownload).
			falha("o download não terminou; tente de novo", nil)
			return
		default:
			GravaReplay(d.DB, gameID, r)
			falha("o download falhou: "+r.Detalhe, nil)
			return
		}
	}
}

// conclui grava o estado final e a linha do tempo. A linha do tempo é o que o
// rastreio usa para calibrar a posição; se falhar, o replay continua baixado
// e o evento diz que ela faltou.
func conclui(ctx context.Context, d Deps, ex *web.Execucao, gameID int64, r Replay) {
	GravaReplay(d.DB, gameID, r)
	linha := true
	if err := guardaLinhaDoTempo(ctx, d, gameID); err != nil {
		log.Printf("partida: linha do tempo de %d: %v", gameID, err)
		ex.Emite("aviso", map[string]string{"texto": "o replay foi baixado, mas a linha do tempo não veio do cliente"})
		linha = false
	}
	ex.Emite("concluido", map[string]any{"replay": r, "linha_do_tempo": linha})
}
