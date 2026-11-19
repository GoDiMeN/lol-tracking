package ia

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"loltracking/server/analise"
	"loltracking/server/banco"
)

// O ESPECIALISTA LANE. A coleta por elo rastreia partidas de jogadores de
// referência (o "melhor do elo" escolhido pela coleta, ou o aproveitado na
// partida de outro). O especialista transforma essas partidas em conhecimento:
//
//  1. RELATÓRIO DE REFERÊNCIA (especialista_relatorio.go): por partida +
//     jogador de referência, a IA lê só os fatos que o app monta e escreve o
//     plano de jogo por fase (o que ele fez e em resposta a quê), as decisões
//     que mais pesaram com a REGRA tirada de cada uma, os erros dele e o que
//     ele não fez que os jogadores do usuário fazem (só com dado). Guardado no
//     banco (partida + jogador + versão) e em Markdown na pasta de conhecimento.
//  2. SÍNTESE por patch × elo × rota (especialista_sintese.go): junta os
//     relatórios do recorte; quem decide se uma regra "se repete" é o app
//     (contando jogadores distintos), não a IA. Vira o arquivo de conhecimento
//     conhecimento/especialista-lane/<patch>/<elo>-<rota>.md (+ indice.md) e
//     traz a comparação com outro patch, que só afirma mudança acima de
//     AmostraMinimaPatch partidas por lado.
//  3. A VOZ "especialista_lane" da análise e da conversa lê a síntese do
//     patch/elo/rota do jogador em foco (conhecimento.go, materialEspecialista).
//  4. O ESPELHO no NotebookLM (especialista_espelho.go) manda os Markdown ao
//     caderno, a pedido.
//
// O relatório nasce e mora no app; o NotebookLM é cópia para ler e conversar.

// PastaEspecialista: a subpasta da pasta de conhecimento.
const PastaEspecialista = "especialista-lane"

func migraEspecialista(db *banco.DB) error {
	if err := banco.Executa(db,
		`CREATE TABLE IF NOT EXISTS especialista_relatorios (
			id                 INTEGER PRIMARY KEY AUTOINCREMENT,
			game_id            INTEGER NOT NULL,
			participante_id    INTEGER NOT NULL,
			versao             INTEGER NOT NULL,
			estado             TEXT NOT NULL,
			erro               TEXT NOT NULL DEFAULT '',
			criado_em          TEXT NOT NULL,
			terminado_em       TEXT NOT NULL DEFAULT '',
			modelo             TEXT NOT NULL DEFAULT '',
			implementacao      TEXT NOT NULL DEFAULT '',
			versao_prompt      INTEGER NOT NULL DEFAULT 0,
			patch              TEXT NOT NULL DEFAULT '',
			elo                TEXT NOT NULL DEFAULT '',
			elo_tier           TEXT NOT NULL DEFAULT '',
			funcao             TEXT NOT NULL DEFAULT '',
			campeao            TEXT NOT NULL DEFAULT '',
			puuid              TEXT NOT NULL DEFAULT '',
			venceu             INTEGER NOT NULL DEFAULT 0,
			rastreados         INTEGER NOT NULL DEFAULT 0,
			conteudo           TEXT NOT NULL DEFAULT '',
			bruto              TEXT NOT NULL DEFAULT '',
			avisos             TEXT NOT NULL DEFAULT '[]',
			enviado            TEXT NOT NULL DEFAULT '{}',
			arquivo            TEXT NOT NULL DEFAULT '',
			duracao_ms         INTEGER NOT NULL DEFAULT 0,
			caracteres_entrada INTEGER NOT NULL DEFAULT 0,
			caracteres_saida   INTEGER NOT NULL DEFAULT 0,
			tokens_entrada     INTEGER NOT NULL DEFAULT 0,
			tokens_saida       INTEGER NOT NULL DEFAULT 0,
			custo_usd          REAL NOT NULL DEFAULT 0,
			UNIQUE (game_id, participante_id, versao)
		)`,
		`CREATE INDEX IF NOT EXISTS especialista_relatorios_recorte ON especialista_relatorios (patch, elo_tier, funcao)`,
		`CREATE TABLE IF NOT EXISTS especialista_sinteses (
			id                 INTEGER PRIMARY KEY AUTOINCREMENT,
			patch              TEXT NOT NULL,
			elo                TEXT NOT NULL,
			funcao             TEXT NOT NULL,
			versao             INTEGER NOT NULL,
			estado             TEXT NOT NULL,
			erro               TEXT NOT NULL DEFAULT '',
			criado_em          TEXT NOT NULL,
			terminado_em       TEXT NOT NULL DEFAULT '',
			modelo             TEXT NOT NULL DEFAULT '',
			implementacao      TEXT NOT NULL DEFAULT '',
			versao_prompt      INTEGER NOT NULL DEFAULT 0,
			relatorios         TEXT NOT NULL DEFAULT '[]',
			partidas           INTEGER NOT NULL DEFAULT 0,
			jogadores          INTEGER NOT NULL DEFAULT 0,
			conteudo           TEXT NOT NULL DEFAULT '',
			bruto              TEXT NOT NULL DEFAULT '',
			avisos             TEXT NOT NULL DEFAULT '[]',
			enviado            TEXT NOT NULL DEFAULT '{}',
			arquivo            TEXT NOT NULL DEFAULT '',
			duracao_ms         INTEGER NOT NULL DEFAULT 0,
			caracteres_entrada INTEGER NOT NULL DEFAULT 0,
			caracteres_saida   INTEGER NOT NULL DEFAULT 0,
			tokens_entrada     INTEGER NOT NULL DEFAULT 0,
			tokens_saida       INTEGER NOT NULL DEFAULT 0,
			custo_usd          REAL NOT NULL DEFAULT 0,
			UNIQUE (patch, elo, funcao, versao)
		)`,
		`CREATE TABLE IF NOT EXISTS especialista_espelho (
			caderno     TEXT NOT NULL,
			chave       TEXT NOT NULL,
			tipo        TEXT NOT NULL,
			versao      INTEGER NOT NULL DEFAULT 0,
			fonte_id    TEXT NOT NULL DEFAULT '',
			titulo      TEXT NOT NULL DEFAULT '',
			caracteres  INTEGER NOT NULL DEFAULT 0,
			enviado_em  TEXT NOT NULL DEFAULT '',
			erro        TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (caderno, chave)
		)`,
		`CREATE TABLE IF NOT EXISTS especialista_config (
			chave TEXT PRIMARY KEY,
			valor TEXT NOT NULL DEFAULT ''
		)`,
	); err != nil {
		return err
	}
	agora := banco.Agora()
	for _, t := range []string{"especialista_relatorios", "especialista_sinteses"} {
		if _, err := db.Exec(`UPDATE `+t+` SET estado = 'falhou', erro = 'o servidor fechou no meio da chamada', terminado_em = ?
			WHERE estado = 'rodando'`, agora); err != nil {
			return err
		}
	}
	return nil
}

// --- Recorte -------------------------------------------------------------------

// Recorte: patch × elo (a liga, sem divisão: "PLATINUM") × rota (função).
type Recorte struct {
	Patch  string `json:"patch"`
	Elo    string `json:"elo"`
	Funcao string `json:"rota"`
}

// ErrRecorte: patch, elo ou rota inválidos (400).
func validaRecorte(r Recorte) (Recorte, error) {
	r.Patch = strings.TrimSpace(r.Patch)
	r.Elo = TierDe(r.Elo)
	r.Funcao = strings.ToUpper(strings.TrimSpace(r.Funcao))
	if !patchValido(r.Patch) {
		return r, &ErrPedido{"patch inválido (ex.: 16.23)"}
	}
	if !eloValido(r.Elo) {
		return r, &ErrPedido{"elo inválido (ex.: PLATINUM)"}
	}
	if _, ok := funcaoNaFrase[r.Funcao]; !ok {
		return r, &ErrPedido{"rota inválida (TOP, JUNGLE, MIDDLE, BOTTOM ou UTILITY)"}
	}
	return r, nil
}

func patchValido(p string) bool {
	partes := strings.Split(p, ".")
	if len(partes) != 2 {
		return false
	}
	for _, x := range partes {
		if _, err := strconv.Atoi(x); err != nil || x == "" {
			return false
		}
	}
	return true
}

// TierDe: "PLATINUM II" → "PLATINUM"; "" fica "".
func TierDe(elo string) string {
	f := strings.Fields(strings.ToUpper(strings.TrimSpace(elo)))
	if len(f) == 0 {
		return ""
	}
	return f[0]
}

// ComparaPatch: -1, 0, 1 numérico ("16.9" < "16.10").
func ComparaPatch(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// Nome: "16.23 · Platina · selva".
func (r Recorte) Nome() string {
	return r.Patch + " · " + NomeElo(r.Elo) + " · " + nomeRota(r.Funcao)
}

func nomeRota(f string) string {
	switch f {
	case "TOP":
		return "topo"
	case "JUNGLE":
		return "selva"
	case "MIDDLE":
		return "meio"
	case "BOTTOM":
		return "atirador"
	case "UTILITY":
		return "suporte"
	}
	return "sem rota"
}

// ArquivoSintese: o caminho relativo à pasta de conhecimento.
func (r Recorte) ArquivoSintese() string {
	return PastaEspecialista + "/" + r.Patch + "/" + slug(r.Elo) + "-" + slug(r.Funcao) + ".md"
}

func arquivoRelatorio(patch, tier, funcao, campeao string, gameID int64, pid int) string {
	return fmt.Sprintf("%s/%s/relatorios/%s-%s-%s-%d-p%d.md", PastaEspecialista, patch, slug(tier), slug(funcao), slug(campeao), gameID, pid)
}

// escreveConhecimento grava um arquivo da pasta de conhecimento (LF, UTF-8).
func (s *Servico) escreveConhecimento(rel, texto string) error {
	if s.Conhecimento == "" {
		return errors.New("sem pasta de conhecimento")
	}
	destino := filepath.Join(s.Conhecimento, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(destino), 0o755); err != nil {
		return err
	}
	texto = strings.ReplaceAll(texto, "\r\n", "\n")
	tmp := destino + ".tmp"
	if err := os.WriteFile(tmp, []byte(texto), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, destino)
}

// --- Painel (GET /api/especialista) -------------------------------------------------

// ReferenciaPainel: uma partida de referência e o estado do relatório dela.
type ReferenciaPainel struct {
	GameID         int64               `json:"game_id"`
	ParticipanteID int                 `json:"participante_id"`
	Campeao        string              `json:"campeao"`
	Funcao         string              `json:"rota"`
	Elo            string              `json:"elo"`
	EloTier        string              `json:"elo_tier"`
	Patch          string              `json:"patch"`
	Venceu         bool                `json:"venceu"`
	Rastreados     int                 `json:"rastreados"`
	DuracaoS       int                 `json:"duracao_s"`
	CriadaEm       string              `json:"criada_em"`
	Papel          string              `json:"papel"`
	Relatorio      *VersaoEspecialista `json:"relatorio"`
	Execucao       any                 `json:"execucao"`
}

// VersaoEspecialista: a última versão de um relatório ou síntese.
type VersaoEspecialista struct {
	ID       int64  `json:"id"`
	Versao   int    `json:"versao"`
	Estado   string `json:"estado"`
	CriadoEm string `json:"criado_em"`
	Arquivo  string `json:"arquivo"`
}

// RecortePainel: um patch × elo × rota.
type RecortePainel struct {
	Recorte
	Nome       string         `json:"nome"`
	Partidas   int            `json:"partidas"`
	Jogadores  int            `json:"jogadores"`
	Vitorias   int            `json:"vitorias"`
	Relatorios int            `json:"relatorios"`
	Sintese    *SintesePainel `json:"sintese"`
	Execucao   any            `json:"execucao"`
}

// SintesePainel: a síntese do recorte (a última) e se ficou para trás.
type SintesePainel struct {
	VersaoEspecialista
	RelatoriosUsados int  `json:"relatorios_usados"`
	Desatualizada    bool `json:"desatualizada"`
}

// Painel é a resposta de GET /api/especialista.
type Painel struct {
	Referencias []ReferenciaPainel `json:"referencias"`
	Recortes    []RecortePainel    `json:"recortes"`
	Espelho     EstadoEspelho      `json:"espelho"`
	Pasta       string             `json:"pasta"`
}

// referenciasDaColeta: todos os jogadores de referência (só o banco).
func (s *Servico) referenciasDaColeta() ([]analise.IndicadoresJogador, error) {
	return s.Fontes.Referencias(analise.FiltroReferencia{Limite: 200})
}

// ultimoRelatorio: a última versão (qualquer estado) e a última concluída.
func ultimoRelatorioEsp(db *banco.DB, gameID int64, pid int) (*VersaoEspecialista, error) {
	var v VersaoEspecialista
	err := db.QueryRow(`SELECT id, versao, estado, criado_em, arquivo FROM especialista_relatorios WHERE game_id = ? AND participante_id = ?
		ORDER BY versao DESC LIMIT 1`, gameID, pid).Scan(&v.ID, &v.Versao, &v.Estado, &v.CriadoEm, &v.Arquivo)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &v, err
}

// relatoriosConcluidosDoRecorte: o id da última versão concluída de cada
// partida + jogador do recorte.
func relatoriosConcluidosDoRecorte(db *banco.DB, r Recorte) ([]int64, error) {
	linhas, err := db.Query(`SELECT MAX(id) FROM especialista_relatorios WHERE patch = ? AND elo_tier = ? AND funcao = ? AND estado = 'concluido'
		GROUP BY game_id, participante_id ORDER BY game_id, participante_id`, r.Patch, r.Elo, r.Funcao)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	var ids []int64
	for linhas.Next() {
		var id int64
		if err := linhas.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, linhas.Err()
}

func chaveRelEsp(gameID int64, pid int) string { return fmt.Sprintf("er:%d:%d", gameID, pid) }
func chaveSintese(r Recorte) string            { return "es:" + r.Patch + ":" + r.Elo + ":" + r.Funcao }

// MontaPainel: as partidas de referência, os recortes e o espelho.
func (s *Servico) MontaPainel(ctx context.Context) (Painel, error) {
	p := Painel{Referencias: []ReferenciaPainel{}, Recortes: []RecortePainel{}, Pasta: PastaEspecialista + "/"}
	refs, err := s.referenciasDaColeta()
	if err != nil {
		return p, err
	}
	type acum struct {
		partidas, jogadores map[string]bool
		vitorias            int
	}
	porRecorte := map[Recorte]*acum{}
	for _, x := range refs {
		rp := ReferenciaPainel{GameID: x.GameID, ParticipanteID: x.ParticipanteID, Campeao: x.Campeao, Funcao: x.Funcao, Elo: x.Elo,
			EloTier: TierDe(x.Elo), Patch: x.Patch, Venceu: x.Venceu, Rastreados: x.RastreadosNaPartida, DuracaoS: x.DuracaoS,
			CriadaEm: x.CriadaEm, Papel: x.Papel.Frase}
		if rp.EloTier == "" {
			rp.EloTier = TierDe(x.EloPartida)
		}
		if rp.Relatorio, err = ultimoRelatorioEsp(s.DB, x.GameID, x.ParticipanteID); err != nil {
			return p, err
		}
		rp.Execucao = execucaoJSON(s.execucaoDe(chaveRelEsp(x.GameID, x.ParticipanteID)))
		p.Referencias = append(p.Referencias, rp)
		if x.Patch == "" || x.Funcao == "" || rp.EloTier == "" {
			continue
		}
		k := Recorte{Patch: x.Patch, Elo: rp.EloTier, Funcao: x.Funcao}
		a := porRecorte[k]
		if a == nil {
			a = &acum{partidas: map[string]bool{}, jogadores: map[string]bool{}}
			porRecorte[k] = a
		}
		a.partidas[fmt.Sprint(x.GameID)] = true
		quem := x.Puuid
		if quem == "" {
			quem = fmt.Sprint(x.GameID, ":", x.ParticipanteID)
		}
		a.jogadores[quem] = true
		if x.Venceu {
			a.vitorias++
		}
	}
	// Recortes que só têm síntese (as referências saíram do banco) também aparecem.
	if linhas, err := s.DB.Query(`SELECT DISTINCT patch, elo, funcao FROM especialista_sinteses`); err == nil {
		for linhas.Next() {
			var k Recorte
			if linhas.Scan(&k.Patch, &k.Elo, &k.Funcao) == nil && porRecorte[k] == nil {
				porRecorte[k] = &acum{partidas: map[string]bool{}, jogadores: map[string]bool{}}
			}
		}
		linhas.Close()
	}
	for k, a := range porRecorte {
		rp := RecortePainel{Recorte: k, Nome: k.Nome(), Partidas: len(a.partidas), Jogadores: len(a.jogadores), Vitorias: a.vitorias}
		ids, err := relatoriosConcluidosDoRecorte(s.DB, k)
		if err != nil {
			return p, err
		}
		rp.Relatorios = len(ids)
		sv, ok, err := LeSintese(s.DB, k, 0)
		if err != nil {
			return p, err
		}
		if ok {
			sp := &SintesePainel{VersaoEspecialista: VersaoEspecialista{ID: sv.ID, Versao: sv.Versao, Estado: sv.Estado, CriadoEm: sv.CriadoEm,
				Arquivo: sv.Arquivo}, RelatoriosUsados: len(sv.Relatorios)}
			sp.Desatualizada = sv.Estado == "concluido" && !mesmosIDs(sv.Relatorios, ids)
			rp.Sintese = sp
		}
		rp.Execucao = execucaoJSON(s.execucaoDe(chaveSintese(k)))
		p.Recortes = append(p.Recortes, rp)
	}
	sort.Slice(p.Recortes, func(i, j int) bool {
		a, b := p.Recortes[i], p.Recortes[j]
		if c := ComparaPatch(a.Patch, b.Patch); c != 0 {
			return c > 0
		}
		if a.Elo != b.Elo {
			return a.Elo < b.Elo
		}
		return a.Funcao < b.Funcao
	})
	p.Espelho = s.EstadoDoEspelho(ctx, false)
	return p, nil
}

func mesmosIDs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	x := append([]int64{}, a...)
	y := append([]int64{}, b...)
	sort.Slice(x, func(i, j int) bool { return x[i] < x[j] })
	sort.Slice(y, func(i, j int) bool { return y[i] < y[j] })
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

// --- Configuração guardada (o caderno do espelho) ---------------------------------

func leConfig(db *banco.DB, chave string) string {
	var v string
	_ = db.QueryRow(`SELECT valor FROM especialista_config WHERE chave = ?`, chave).Scan(&v)
	return v
}

func gravaConfig(db *banco.DB, chave, valor string) error {
	_, err := db.Exec(`INSERT INTO especialista_config (chave, valor) VALUES (?, ?)
		ON CONFLICT (chave) DO UPDATE SET valor = excluded.valor`, chave, valor)
	return err
}

// jsonOuVazio: para as colunas JSON.
func jsonOuVazio(v any, vazio string) string {
	b, err := json.Marshal(v)
	if err != nil || string(b) == "null" {
		return vazio
	}
	return string(b)
}
