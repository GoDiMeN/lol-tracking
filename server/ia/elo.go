package ia

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"loltracking/server/analise"
	"loltracking/server/banco"
	"loltracking/server/riot"
)

// O ELO DE COMPARAÇÃO do jogador em foco: com que elo a voz "o que os
// melhores do elo fazem" compara. A partida do dono não tem elo na base de
// fatos (só a coleta por elo preenche resumos_partida.elo), então o app o
// descobre por jogador, nesta ordem:
//
//  1. o elo que a pessoa escolheu na tela da análise ("comparar com o elo"),
//     guardado por jogador (puuid do LCU);
//  2. o elo da fila solo pela API da Riot (league-v4), quando há chave:
//     guardado com a data e consultado de novo depois de ValidadeEloRiot;
//  3. o elo da partida, quando ela é da coleta por elo.
//
// Sem nenhum dos três, a voz diz o que falta. Nada aqui fala com o LCU.

// ValidadeEloRiot: o elo guardado da Riot vale um dia (o elo muda devagar e a
// chave de desenvolvimento tem cota).
const ValidadeEloRiot = 24 * time.Hour

// esperaErroRiot: depois de um erro da Riot, não pergunta de novo por 10 min
// (cada abertura da tela não pode gastar a cota de uma chave vencida).
const esperaErroRiot = 10 * time.Minute

// FonteEloRiot é a API da Riot (riot.Cliente); nil = não consulta.
type FonteEloRiot interface {
	EloSoloPorRiotID(ctx context.Context, nome, tag string) (riot.EntradaLiga, bool, error)
}

// OpcaoElo é um elo da lista da tela.
type OpcaoElo struct {
	Elo  string `json:"elo"`
	Nome string `json:"nome"`
}

// OpcoesElo: os elos da fila solo, do mais baixo ao mais alto.
var OpcoesElo = []OpcaoElo{{"IRON", "Ferro"}, {"BRONZE", "Bronze"}, {"SILVER", "Prata"}, {"GOLD", "Ouro"}, {"PLATINUM", "Platina"},
	{"EMERALD", "Esmeralda"}, {"DIAMOND", "Diamante"}, {"MASTER", "Mestre"}, {"GRANDMASTER", "Grão-Mestre"}, {"CHALLENGER", "Desafiante"}}

// NomeElo: "PLATINUM II" → "Platina II"; "CHALLENGER" → "Desafiante".
func NomeElo(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	cod, resto, _ := strings.Cut(s, " ")
	for _, o := range OpcoesElo {
		if strings.EqualFold(o.Elo, cod) {
			if resto != "" {
				return o.Nome + " " + resto
			}
			return o.Nome
		}
	}
	return s
}

// eloValido diz se o código está na lista.
func eloValido(s string) bool {
	for _, o := range OpcoesElo {
		if o.Elo == s {
			return true
		}
	}
	return false
}

// EloRiot é o elo guardado da última consulta à Riot.
type EloRiot struct {
	Elo          string `json:"elo"` // "" = a conta não tem ranqueada solo
	Divisao      string `json:"divisao"`
	PDL          int    `json:"pdl"`
	Vitorias     int    `json:"vitorias"`
	Derrotas     int    `json:"derrotas"`
	ConsultadoEm string `json:"consultado_em"`
}

// EloComparacao é a resposta da tela (GET .../ia/referencias).
type EloComparacao struct {
	// Elo: o código em uso ("PLATINUM"; "" = não se sabe); Nome: na tela.
	Elo  string `json:"elo"`
	Nome string `json:"nome"`
	// Fonte: escolhido, riot, partida ou "".
	Fonte       string     `json:"fonte"`
	Escolhido   string     `json:"escolhido"`
	EscolhidoEm string     `json:"escolhido_em"`
	Riot        *EloRiot   `json:"riot"`
	AvisoRiot   string     `json:"aviso_riot"`
	EloPartida  string     `json:"elo_partida"`
	Opcoes      []OpcaoElo `json:"opcoes"`
}

func migraElo(db *banco.DB) error {
	return banco.Executa(db, `CREATE TABLE IF NOT EXISTS ia_elos_jogador (
		puuid          TEXT PRIMARY KEY,
		nome           TEXT NOT NULL DEFAULT '',
		tag            TEXT NOT NULL DEFAULT '',
		elo_escolhido  TEXT NOT NULL DEFAULT '',
		escolhido_em   TEXT NOT NULL DEFAULT '',
		elo_riot       TEXT NOT NULL DEFAULT '',
		divisao_riot   TEXT NOT NULL DEFAULT '',
		pdl_riot       INTEGER NOT NULL DEFAULT 0,
		vitorias_riot  INTEGER NOT NULL DEFAULT 0,
		derrotas_riot  INTEGER NOT NULL DEFAULT 0,
		consultado_em  TEXT NOT NULL DEFAULT '',
		erro_riot      TEXT NOT NULL DEFAULT '',
		erro_em        TEXT NOT NULL DEFAULT ''
	)`)
}

type linhaElo struct {
	escolhido, escolhidoEm string
	riot                   EloRiot
	erro, erroEm           string
}

func leLinhaElo(db *banco.DB, puuid string) (linhaElo, bool, error) {
	var l linhaElo
	err := db.QueryRow(`SELECT elo_escolhido, escolhido_em, elo_riot, divisao_riot, pdl_riot, vitorias_riot, derrotas_riot, consultado_em,
		erro_riot, erro_em FROM ia_elos_jogador WHERE puuid = ?`, puuid).Scan(&l.escolhido, &l.escolhidoEm, &l.riot.Elo, &l.riot.Divisao,
		&l.riot.PDL, &l.riot.Vitorias, &l.riot.Derrotas, &l.riot.ConsultadoEm, &l.erro, &l.erroEm)
	if errors.Is(err, sql.ErrNoRows) {
		return l, false, nil
	}
	return l, err == nil, err
}

// DefineElo guarda o elo escolhido na tela para o jogador ("" = volta ao padrão).
func DefineElo(db *banco.DB, p Participante, elo string) error {
	elo = strings.ToUpper(strings.TrimSpace(elo))
	if elo != "" && !eloValido(elo) {
		return &ErrPedido{"elo inválido: use um de IRON, BRONZE, SILVER, GOLD, PLATINUM, EMERALD, DIAMOND, MASTER, GRANDMASTER, CHALLENGER (ou vazio)"}
	}
	if p.Puuid == "" {
		return &ErrPedido{"este jogador não tem identificador (puuid) na partida"}
	}
	quando := ""
	if elo != "" {
		quando = banco.Agora()
	}
	_, err := db.Exec(`INSERT INTO ia_elos_jogador (puuid, nome, tag, elo_escolhido, escolhido_em) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (puuid) DO UPDATE SET elo_escolhido = excluded.elo_escolhido, escolhido_em = excluded.escolhido_em,
		nome = excluded.nome, tag = excluded.tag`, p.Puuid, p.Nome, p.Tag, elo, quando)
	return err
}

func antigo(quando string, d time.Duration) bool {
	t, err := time.Parse("2006-01-02T15:04:05Z", quando)
	return err != nil || time.Since(t) > d
}

// avisoErroRiot: a frase da tela para cada erro da Riot (sem a chave e sem o Riot ID).
func avisoErroRiot(err error) string {
	switch {
	case errors.Is(err, riot.ErrSemChave):
		return "sem chave da Riot (token.txt): escolha o elo à mão"
	case errors.Is(err, riot.ErrChaveRecusada):
		return "a Riot recusou a chave (a de desenvolvimento vence em 24 h): troque o token.txt ou escolha o elo à mão"
	case errors.Is(err, riot.ErrNaoEncontradoRiot):
		return "a Riot não achou este Riot ID (nome#tag mudou?): escolha o elo à mão"
	case errors.Is(err, context.DeadlineExceeded):
		return "a API da Riot não respondeu a tempo: escolha o elo à mão ou tente de novo"
	}
	return "não deu para consultar a Riot (" + err.Error() + "): escolha o elo à mão"
}

// EloDeComparacao resolve o elo do jogador em foco (escolhido > Riot >
// partida). Com consulta verdadeiro e uma FonteEloRiot, pergunta à Riot quando
// o guardado venceu (e guarda a resposta com a data); erro da Riot nunca
// derruba: vira aviso.
func (s *Servico) EloDeComparacao(ctx context.Context, info InfoPartida, foco Participante, consulta bool) EloComparacao {
	e := EloComparacao{EloPartida: info.Elo, Opcoes: OpcoesElo}
	var l linhaElo
	if foco.Puuid != "" {
		if x, ok, err := leLinhaElo(s.DB, foco.Puuid); err == nil && ok {
			l = x
		}
	}
	if l.riot.ConsultadoEm != "" {
		r := l.riot
		e.Riot = &r
	}
	switch {
	case s.Riot == nil:
		e.AvisoRiot = "sem a consulta à Riot neste modo: escolha o elo à mão"
	case foco.Puuid == "" || foco.Nome == "" || foco.Tag == "":
		e.AvisoRiot = "sem o Riot ID deste jogador na partida: escolha o elo à mão"
	case !consulta:
	default:
		if r, aviso := ConsultaEloRiot(ctx, s.DB, s.Riot, foco.Puuid, foco.Nome, foco.Tag); r != nil {
			e.Riot = r
		} else {
			e.AvisoRiot = aviso
		}
	}
	if e.Riot != nil && e.Riot.Elo == "" && e.AvisoRiot == "" {
		e.AvisoRiot = "a Riot diz que este jogador não tem ranqueada solo nesta temporada: escolha o elo à mão"
	}
	e.Escolhido, e.EscolhidoEm = l.escolhido, l.escolhidoEm
	switch {
	case l.escolhido != "":
		e.Elo, e.Nome, e.Fonte = l.escolhido, NomeElo(l.escolhido), "escolhido"
	case e.Riot != nil && e.Riot.Elo != "":
		e.Elo, e.Nome, e.Fonte = e.Riot.Elo, NomeElo(strings.TrimSpace(e.Riot.Elo+" "+e.Riot.Divisao)), "riot"
	case info.Elo != "":
		e.Elo, e.Nome, e.Fonte = strings.ToUpper(info.Elo), NomeElo(info.Elo), "partida"
	}
	return e
}

// ConsultaEloRiot: o elo da fila solo de um jogador (puuid do LCU e Riot ID)
// pela API da Riot, com o cache de ia_elos_jogador: o guardado vale
// ValidadeEloRiot; depois de um erro, espera esperaErroRiot antes de perguntar
// de novo. Devolve o elo (Elo "" = sem ranqueada solo) ou nil e o aviso para a
// tela. Também é o que o contexto dos jogadores (server/contexto) usa para os 10.
func ConsultaEloRiot(ctx context.Context, db *banco.DB, fonte FonteEloRiot, puuid, nome, tag string) (*EloRiot, string) {
	if fonte == nil {
		return nil, "sem a consulta à Riot neste modo"
	}
	if puuid == "" || nome == "" || tag == "" {
		return nil, "sem o Riot ID deste jogador na partida"
	}
	l, _, _ := leLinhaElo(db, puuid)
	if l.riot.ConsultadoEm != "" && !antigo(l.riot.ConsultadoEm, ValidadeEloRiot) {
		r := l.riot
		return &r, ""
	}
	if l.erroEm != "" && !antigo(l.erroEm, esperaErroRiot) {
		return nil, l.erro
	}
	c2, cancela := context.WithTimeout(ctx, 12*time.Second)
	ent, tem, err := fonte.EloSoloPorRiotID(c2, nome, tag)
	cancela()
	agora := banco.Agora()
	if err != nil {
		aviso := avisoErroRiot(err)
		if !errors.Is(err, riot.ErrSemChave) {
			_, _ = db.Exec(`INSERT INTO ia_elos_jogador (puuid, nome, tag, erro_riot, erro_em) VALUES (?, ?, ?, ?, ?)
				ON CONFLICT (puuid) DO UPDATE SET erro_riot = excluded.erro_riot, erro_em = excluded.erro_em`, puuid, nome, tag, aviso, agora)
		}
		return nil, aviso
	}
	r := EloRiot{ConsultadoEm: agora}
	if tem {
		r.Elo, r.Divisao, r.PDL, r.Vitorias, r.Derrotas = ent.Tier, ent.Rank, ent.LP, ent.Vitorias, ent.Derrotas
		if riot.EloApex(r.Elo) {
			r.Divisao = ""
		}
	}
	_, _ = db.Exec(`INSERT INTO ia_elos_jogador (puuid, nome, tag, elo_riot, divisao_riot, pdl_riot, vitorias_riot, derrotas_riot, consultado_em,
		erro_riot, erro_em) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, '', '')
		ON CONFLICT (puuid) DO UPDATE SET elo_riot = excluded.elo_riot, divisao_riot = excluded.divisao_riot, pdl_riot = excluded.pdl_riot,
		vitorias_riot = excluded.vitorias_riot, derrotas_riot = excluded.derrotas_riot, consultado_em = excluded.consultado_em,
		erro_riot = '', erro_em = '', nome = excluded.nome, tag = excluded.tag`,
		puuid, nome, tag, r.Elo, r.Divisao, r.PDL, r.Vitorias, r.Derrotas, r.ConsultadoEm)
	return &r, ""
}

// fonteEloIA: o elo da Riot que ia_elos_jogador guardou, para a base de fatos
// (analise.RegistraFonteElo): só leitura, nenhum pedido. O elo ESCOLHIDO na
// tela não entra (é o elo de comparação, não o do jogador).
type fonteEloIA struct{}

func init() { analise.RegistraFonteElo(fonteEloIA{}) }

func (fonteEloIA) EloDoJogador(db *banco.DB, puuid string) (analise.EloConhecido, bool) {
	var e analise.EloConhecido
	var quando string
	if puuid == "" {
		return e, false
	}
	if err := db.QueryRow(`SELECT elo_riot, divisao_riot, pdl_riot, vitorias_riot, derrotas_riot, consultado_em FROM ia_elos_jogador
		WHERE puuid = ? AND consultado_em != '' AND elo_riot != ''`, puuid).Scan(&e.Elo, &e.Divisao, &e.PDL, &e.Vitorias, &e.Derrotas, &quando); err != nil {
		return e, false
	}
	e.Fonte = "API da Riot (league-v4), consultado em " + quando + " (o de hoje, não o do dia da partida)"
	return e, true
}

func (fonteEloIA) EloDaPartida(db *banco.DB, gameID int64) (string, bool) { return "", false }

// textoElo: a linha "ELO DE COMPARAÇÃO" do pedido (de onde veio o elo).
func textoElo(e EloComparacao) string {
	switch e.Fonte {
	case "escolhido":
		return e.Nome + " (escolhido pela pessoa na tela da análise)"
	case "riot":
		return e.Nome + " (fila solo pela API da Riot, consultado em " + e.Riot.ConsultadoEm + ")"
	case "partida":
		return e.Nome + " (o elo da coleta por elo desta partida)"
	}
	return "não conhecido (a voz dos melhores do elo fica sem referência)"
}

// ReferenciasDoJogador: o elo de comparação e o estado de cada voz (o que
// tem e o que falta para falar), sem montar o pedido nem chamar a IA.
func (s *Servico) ReferenciasDoJogador(ctx context.Context, gameID int64, pid int) (EloComparacao, []MaterialVoz, error) {
	info, err := s.Fontes.Partida(ctx, gameID)
	if err != nil {
		return EloComparacao{}, nil, err
	}
	foco, ok := info.Do(pid)
	if !ok {
		return EloComparacao{}, nil, &ErrPedido{"o participante não está na partida"}
	}
	elo := s.EloDeComparacao(ctx, info, foco, true)
	return elo, s.materialVozes(info, foco, elo), nil
}

// DefineEloDoJogador guarda o elo escolhido para o jogador em foco da partida.
func (s *Servico) DefineEloDoJogador(ctx context.Context, gameID int64, pid int, elo string) error {
	info, err := s.Fontes.Partida(ctx, gameID)
	if err != nil {
		return err
	}
	foco, ok := info.Do(pid)
	if !ok {
		return &ErrPedido{"o participante não está na partida"}
	}
	return DefineElo(s.DB, foco, elo)
}
