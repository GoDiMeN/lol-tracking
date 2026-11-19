package ia

import (
	"context"
	"sync"

	"loltracking/server/banco"
	"loltracking/server/web"
)

// Migra cria as tabelas da IA: os relatórios (por partida + jogador +
// versão) e as conversas com as mensagens. Relatório ou mensagem que estava
// "rodando" quando o servidor caiu vira "falhou" na subida: a chamada morreu
// com o processo.
func Migra(db *banco.DB) error {
	if err := banco.Executa(db,
		`CREATE TABLE IF NOT EXISTS ia_relatorios (
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
			conteudo           TEXT NOT NULL DEFAULT '',
			bruto              TEXT NOT NULL DEFAULT '',
			avisos             TEXT NOT NULL DEFAULT '[]',
			vozes              TEXT NOT NULL DEFAULT '[]',
			enviado            TEXT NOT NULL DEFAULT '{}',
			duracao_ms         INTEGER NOT NULL DEFAULT 0,
			caracteres_entrada INTEGER NOT NULL DEFAULT 0,
			caracteres_saida   INTEGER NOT NULL DEFAULT 0,
			tokens_entrada     INTEGER NOT NULL DEFAULT 0,
			tokens_saida       INTEGER NOT NULL DEFAULT 0,
			custo_usd          REAL NOT NULL DEFAULT 0,
			UNIQUE (game_id, participante_id, versao)
		)`,
		`CREATE TABLE IF NOT EXISTS ia_conversas (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			game_id         INTEGER NOT NULL DEFAULT 0,
			participante_id INTEGER NOT NULL DEFAULT 0,
			titulo          TEXT NOT NULL DEFAULT '',
			criada_em       TEXT NOT NULL,
			atualizada_em   TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS ia_conversas_partida ON ia_conversas (game_id, participante_id)`,
		`CREATE TABLE IF NOT EXISTS ia_mensagens (
			id                 INTEGER PRIMARY KEY AUTOINCREMENT,
			conversa_id        INTEGER NOT NULL,
			papel              TEXT NOT NULL,
			texto              TEXT NOT NULL DEFAULT '',
			estado             TEXT NOT NULL DEFAULT 'ok',
			erro               TEXT NOT NULL DEFAULT '',
			criada_em          TEXT NOT NULL,
			modelo             TEXT NOT NULL DEFAULT '',
			duracao_ms         INTEGER NOT NULL DEFAULT 0,
			caracteres_entrada INTEGER NOT NULL DEFAULT 0,
			caracteres_saida   INTEGER NOT NULL DEFAULT 0,
			partidas           TEXT NOT NULL DEFAULT '[]',
			dados              TEXT NOT NULL DEFAULT '{}'
		)`,
		`CREATE INDEX IF NOT EXISTS ia_mensagens_conversa ON ia_mensagens (conversa_id, id)`,
	); err != nil {
		return err
	}
	if err := migraElo(db); err != nil {
		return err
	}
	if err := migraEspecialista(db); err != nil {
		return err
	}
	agora := banco.Agora()
	if _, err := db.Exec(`UPDATE ia_relatorios SET estado = 'falhou', erro = 'o servidor fechou no meio da chamada', terminado_em = ?
		WHERE estado = 'rodando'`, agora); err != nil {
		return err
	}
	_, err := db.Exec(`UPDATE ia_mensagens SET estado = 'falhou', erro = 'o servidor fechou no meio da chamada' WHERE estado = 'rodando'`)
	return err
}

// Servico junta o que as telas de IA precisam.
type Servico struct {
	DB     *banco.DB
	IA     *Fila
	Fontes Fontes
	Hub    *web.Hub
	// Conhecimento: a pasta dos arquivos das vozes (conhecimento.go).
	Conhecimento string
	// Riot: o elo da fila solo do jogador em foco pela API da Riot (elo.go);
	// nil = não consulta (só o elo escolhido na tela e o da coleta).
	Riot FonteEloRiot
	// Caderno: o NotebookLM do espelho do Especialista Lane (nil = desligado);
	// CadernoPadrao: o id do caderno da configuração (a tela pode trocar).
	Caderno       Caderno
	CadernoPadrao string
	// SemEstudo: o interruptor LOL_ESTUDO desligado. As rotas do Especialista
	// Lane respondem 404; as vozes continuam usando as sínteses já prontas.
	SemEstudo bool
	// ContextoAntes: antes de montar o pedido da análise, garante o contexto
	// dos 10 jogadores (server/contexto busca o histórico de quem falta, com
	// os eventos na execução da IA); devolve se buscou (a partida é relida).
	// nil = não busca (testes da IA, e o pacote contexto importa este).
	ContextoAntes func(ctx context.Context, gameID int64, emite func(tipo string, dados any)) bool

	mu sync.Mutex
	// rodando: relatório (game:pid) ou conversa (c:id) → execução.
	rodando map[string]*web.Execucao
}

// NovoServico monta o serviço.
func NovoServico(db *banco.DB, c Cliente, f Fontes, hub *web.Hub, conhecimento string) *Servico {
	fila, ok := c.(*Fila)
	if !ok {
		fila = NovaFila(c)
	}
	return &Servico{DB: db, IA: fila, Fontes: f, Hub: hub, Conhecimento: conhecimento, rodando: map[string]*web.Execucao{}}
}

// ocupa marca a chave como rodando; devolve a execução que já roda, se há.
func (s *Servico) ocupa(chave string, e *web.Execucao) *web.Execucao {
	s.mu.Lock()
	defer s.mu.Unlock()
	if x, ok := s.rodando[chave]; ok && !x.Fechada() {
		return x
	}
	s.rodando[chave] = e
	return nil
}

func (s *Servico) libera(chave string, e *web.Execucao) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rodando[chave] == e {
		delete(s.rodando, chave)
	}
}

func (s *Servico) execucaoDe(chave string) *web.Execucao {
	s.mu.Lock()
	defer s.mu.Unlock()
	if x, ok := s.rodando[chave]; ok && !x.Fechada() {
		return x
	}
	return nil
}
