package ia

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	"loltracking/server/banco"
	"loltracking/server/web"
)

// O ESPELHO NO NOTEBOOKLM. O relatório e a síntese moram no app (banco +
// Markdown na pasta de conhecimento); o caderno do NotebookLM é uma cópia
// para o dono ler e conversar. Só a pedido (botão), pela ferramenta de linha
// de comando `nlm` (notebooklm-mcp-cli) que já está logada na máquina.
//
// O que sai da máquina: o título padronizado e o TEXTO do Markdown de cada
// relatório/síntese (campeões, elo, patch, número da partida, instantes,
// regras; os nomes de invocador já saem trocados pelo campeão). Nada de
// caminho de disco, banco, chave ou token.
//
// Sem duplicar: guardamos o id da fonte por caderno + chave; quando o
// relatório (ou a síntese) ganha versão nova, a fonte nova entra e a antiga
// sai. Mesma versão já enviada = nada a fazer.

// Caderno é o NotebookLM (ou o falso dos testes).
type Caderno interface {
	// Disponivel: a ferramenta existe? (não testa o login: isso custa uma ida à rede)
	Disponivel() (bool, string)
	// Adiciona uma fonte de texto e devolve o id dela.
	Adiciona(ctx context.Context, caderno, titulo, texto string) (string, error)
	// Remove uma fonte.
	Remove(ctx context.Context, caderno, fonteID string) error
}

// ChaveConfigCaderno: o id do caderno guardado pela tela (vence o da variável).
const ChaveConfigCaderno = "notebooklm_caderno"

// LimiteTextoLinha: acima disto o texto vai por arquivo temporário (a linha
// de comando do Windows tem teto de ~32 mil caracteres).
const LimiteTextoLinha = 20000

var reUUID = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

// FerramentaDescrita: o caderno que sabe dizer qual ferramenta está usando
// (o NLM de verdade; o falso não precisa).
type FerramentaDescrita interface {
	Ferramenta() InfoFerramenta
}

// CadernoFalso: os testes e o LOL_TESTE.
type CadernoFalso struct {
	mu        sync.Mutex
	Fontes    map[string]string // id → título
	Textos    map[string]string
	Removidas []string
	Semente   int
	Falha     error
	SemNLM    bool
}

func (f *CadernoFalso) Disponivel() (bool, string) {
	if f.SemNLM {
		return false, "a ferramenta `nlm` (notebooklm-mcp-cli) não foi encontrada nesta máquina"
	}
	return true, ""
}

func (f *CadernoFalso) Adiciona(ctx context.Context, caderno, titulo, texto string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Falha != nil {
		return "", f.Falha
	}
	if f.Fontes == nil {
		f.Fontes, f.Textos = map[string]string{}, map[string]string{}
	}
	f.Semente++
	id := fmt.Sprintf("00000000-0000-0000-0000-%012d", f.Semente)
	f.Fontes[id], f.Textos[id] = titulo, texto
	return id, nil
}

func (f *CadernoFalso) Remove(ctx context.Context, caderno, fonteID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.Fontes, fonteID)
	f.Removidas = append(f.Removidas, fonteID)
	return nil
}

// --- Estado -------------------------------------------------------------------------

// ItemEspelho: um relatório ou síntese e o estado dele no caderno.
type ItemEspelho struct {
	Chave     string `json:"chave"`
	Tipo      string `json:"tipo"` // relatorio, sintese
	Titulo    string `json:"titulo"`
	Versao    int    `json:"versao"`
	Arquivo   string `json:"arquivo"`
	FonteID   string `json:"fonte_id"`
	Enviada   int    `json:"versao_enviada"`
	EnviadoEm string `json:"enviado_em"`
	Erro      string `json:"erro"`
	// Estado: enviado (a versão atual está lá), desatualizado (há versão nova), pendente (nunca foi).
	Estado string `json:"estado"`
	texto  func() (string, error)
}

// EstadoEspelho: GET /api/especialista/espelho.
type EstadoEspelho struct {
	Caderno    string `json:"caderno"`
	Ferramenta bool   `json:"ferramenta"`
	// Comando e Versao: qual `nlm` vale (detectado na subida, especialista_nlm.go):
	// "nlm" do PATH, o de LOL_NLM ou "uvx --from notebooklm-mcp-cli nlm".
	// Origem: "configurado", "path" ou "uvx".
	Comando   string        `json:"comando"`
	Versao    string        `json:"versao"`
	Origem    string        `json:"origem"`
	Motivo    string        `json:"motivo"`
	Enviados  int           `json:"enviados"`
	Pendentes int           `json:"pendentes"`
	Itens     []ItemEspelho `json:"itens"`
	// ComoExportar: o que fazer à mão sem a ferramenta (os arquivos já estão na pasta).
	ComoExportar string `json:"como_exportar"`
	SaiDaMaquina string `json:"sai_da_maquina"`
	Execucao     any    `json:"execucao"`
}

// CadernoAtual: o da tela (banco) ou o da configuração.
func (s *Servico) CadernoAtual() string {
	if v := strings.TrimSpace(leConfig(s.DB, ChaveConfigCaderno)); v != "" {
		return v
	}
	return strings.TrimSpace(s.CadernoPadrao)
}

// DefineCaderno guarda o id do caderno ("" volta ao da configuração).
func (s *Servico) DefineCaderno(id string) error {
	id = strings.TrimSpace(id)
	if id != "" && !reUUID.MatchString(id) {
		return &ErrPedido{"o id do caderno do NotebookLM é um UUID (o que aparece na URL do caderno)"}
	}
	return gravaConfig(s.DB, ChaveConfigCaderno, strings.ToLower(id))
}

// itensEspelho: o último relatório concluído de cada partida + jogador e a
// última síntese concluída de cada recorte.
func (s *Servico) itensEspelho(caderno string) ([]ItemEspelho, error) {
	var itens []ItemEspelho
	linhas, err := s.DB.Query(`SELECT MAX(id) FROM especialista_relatorios WHERE estado = 'concluido' GROUP BY game_id, participante_id ORDER BY game_id`)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for linhas.Next() {
		var id int64
		if linhas.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	linhas.Close()
	for _, id := range ids {
		r, err := leRelEspPorID(s.DB, id)
		if err != nil {
			return nil, err
		}
		rr := r
		itens = append(itens, ItemEspelho{Chave: fmt.Sprintf("relatorio:%d:%d", r.GameID, r.ParticipanteID), Tipo: "relatorio",
			Titulo: TituloReferencia(r), Versao: r.Versao, Arquivo: r.Arquivo, texto: func() (string, error) {
				if t, err := s.leConhecimento(rr.Arquivo); err == nil {
					return t, nil
				}
				return "", errors.New("o Markdown do relatório não está na pasta de conhecimento: gere de novo")
			}})
	}
	linhas, err = s.DB.Query(`SELECT patch, elo, funcao, MAX(versao) FROM especialista_sinteses WHERE estado = 'concluido' GROUP BY patch, elo, funcao`)
	if err != nil {
		return nil, err
	}
	type rv struct {
		r Recorte
		v int
	}
	var rs []rv
	for linhas.Next() {
		var x rv
		if linhas.Scan(&x.r.Patch, &x.r.Elo, &x.r.Funcao, &x.v) == nil {
			rs = append(rs, x)
		}
	}
	linhas.Close()
	for _, x := range rs {
		sv, ok, err := LeSintese(s.DB, x.r, x.v)
		if err != nil || !ok {
			continue
		}
		svc := sv
		itens = append(itens, ItemEspelho{Chave: "sintese:" + x.r.Patch + ":" + x.r.Elo + ":" + x.r.Funcao, Tipo: "sintese", Titulo: TituloSintese(x.r),
			Versao: sv.Versao, Arquivo: sv.Arquivo, texto: func() (string, error) {
				if t, err := s.leConhecimento(svc.Arquivo); err == nil {
					return t, nil
				}
				if svc.Conteudo != nil {
					return MarkdownSintese(svc), nil
				}
				return "", errors.New("a síntese não tem conteúdo")
			}})
	}
	for i := range itens {
		it := &itens[i]
		it.Estado = "pendente"
		if caderno == "" {
			continue
		}
		var v int
		err := s.DB.QueryRow(`SELECT versao, fonte_id, enviado_em, erro FROM especialista_espelho WHERE caderno = ? AND chave = ?`, caderno, it.Chave).
			Scan(&v, &it.FonteID, &it.EnviadoEm, &it.Erro)
		if err != nil {
			continue
		}
		it.Enviada = v
		switch {
		case it.FonteID != "" && v == it.Versao:
			it.Estado = "enviado"
		case it.FonteID != "":
			it.Estado = "desatualizado"
		}
	}
	return itens, nil
}

func (s *Servico) leConhecimento(rel string) (string, error) {
	if rel == "" || s.Conhecimento == "" {
		return "", errors.New("sem arquivo")
	}
	b, err := os.ReadFile(filepath.Join(s.Conhecimento, filepath.FromSlash(rel)))
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n"), nil
}

const textoSaiDaMaquina = "Para o NotebookLM (Google) sai só o título padronizado e o texto Markdown de cada relatório de referência e síntese: " +
	"campeões, elo, patch, número da partida (pública), instantes e regras. Os nomes de invocador saem trocados pelo campeão. " +
	"Nenhum caminho de disco, banco, chave ou token."

// EstadoDoEspelho monta o estado (sem ir à rede).
func (s *Servico) EstadoDoEspelho(ctx context.Context, _ bool) EstadoEspelho {
	e := EstadoEspelho{Caderno: s.CadernoAtual(), Itens: []ItemEspelho{}, SaiDaMaquina: textoSaiDaMaquina}
	aviso := ""
	if s.Caderno == nil {
		e.Motivo = "o espelho no NotebookLM não está ligado neste servidor"
	} else {
		e.Ferramenta, e.Motivo = s.Caderno.Disponivel()
		if d, ok := s.Caderno.(FerramentaDescrita); ok {
			f := d.Ferramenta()
			e.Comando, e.Versao, e.Origem, aviso = f.Comando, f.Versao, f.Origem, f.Aviso
		}
	}
	if e.Caderno == "" && e.Motivo == "" {
		e.Motivo = "informe o id do caderno do NotebookLM (o UUID da URL do caderno)"
	}
	// Disponível com ressalva (versão velha sem alternativa): o motivo avisa.
	if e.Motivo == "" {
		e.Motivo = aviso
	}
	pasta := filepath.Join(s.Conhecimento, PastaEspecialista)
	e.ComoExportar = "Sem a ferramenta: os arquivos Markdown já estão em " + pasta + " (sínteses em <patch>/<elo>-<rota>.md, relatórios em " +
		"<patch>/relatorios/). No NotebookLM, abra o caderno, \"Adicionar fontes\" → \"Texto copiado\" (ou envie o arquivo .md) e use como " +
		"título o da lista abaixo. Para instalar a ferramenta: `uv tool install notebooklm-mcp-cli` e `nlm login`."
	itens, err := s.itensEspelho(e.Caderno)
	if err != nil {
		e.Motivo = "não deu para ler os relatórios: " + err.Error()
		return e
	}
	e.Itens = web.NuncaNil(itens)
	for _, it := range itens {
		if it.Estado == "enviado" {
			e.Enviados++
		} else {
			e.Pendentes++
		}
	}
	e.Execucao = execucaoJSON(s.execucaoDe("espelho"))
	return e
}

// ErrEspelho: sem caderno ou sem ferramenta (409).
type ErrEspelho struct{ Msg string }

func (e *ErrEspelho) Error() string { return e.Msg }

// EnviaEspelho manda ao caderno o que está pendente ou desatualizado.
func (s *Servico) EnviaEspelho(ctx context.Context) (*web.Execucao, error) {
	caderno := s.CadernoAtual()
	if s.Caderno == nil {
		return nil, &ErrEspelho{"o espelho no NotebookLM não está ligado neste servidor"}
	}
	if ok, motivo := s.Caderno.Disponivel(); !ok {
		return nil, &ErrEspelho{motivo}
	}
	if caderno == "" {
		return nil, &ErrEspelho{"informe o id do caderno do NotebookLM antes de enviar"}
	}
	e := s.Hub.Nova()
	if x := s.ocupa("espelho", e); x != nil {
		e.Fecha()
		return nil, &ErrRodando{"o envio ao NotebookLM já está rodando", x}
	}
	ctxTrab, cancela := context.WithCancel(context.Background())
	e.AoCancelar(cancela)
	go func() {
		defer cancela()
		defer s.libera("espelho", e)
		defer e.Fecha()
		s.rodaEspelho(ctxTrab, e, caderno)
	}()
	return e, nil
}

func (s *Servico) rodaEspelho(ctx context.Context, e *web.Execucao, caderno string) {
	itens, err := s.itensEspelho(caderno)
	if err != nil {
		e.Falha("não deu para ler os relatórios: " + err.Error())
		return
	}
	enviados, iguais, falhas := 0, 0, 0
	e.Emite("inicio", map[string]any{"caderno": caderno, "itens": len(itens), "sai_da_maquina": textoSaiDaMaquina})
	for _, it := range itens {
		if ctx.Err() != nil {
			e.Emite("cancelado", map[string]any{})
			return
		}
		if it.Estado == "enviado" {
			iguais++
			e.Emite("fonte", map[string]any{"chave": it.Chave, "titulo": it.Titulo, "acao": "igual", "fonte_id": it.FonteID})
			continue
		}
		grava := func(fonte, erro string, versao int) {
			if _, err := s.DB.Exec(`INSERT INTO especialista_espelho (caderno, chave, tipo, versao, fonte_id, titulo, caracteres, enviado_em, erro)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT (caderno, chave) DO UPDATE SET versao = excluded.versao, fonte_id = excluded.fonte_id, titulo = excluded.titulo,
				caracteres = excluded.caracteres, enviado_em = excluded.enviado_em, erro = excluded.erro`,
				caderno, it.Chave, it.Tipo, versao, fonte, it.Titulo, 0, banco.Agora(), erro); err != nil {
				log.Printf("especialista: gravar o espelho %s: %v", it.Chave, err)
			}
		}
		texto, err := it.texto()
		if err != nil {
			falhas++
			e.Emite("fonte", map[string]any{"chave": it.Chave, "titulo": it.Titulo, "acao": "falhou", "erro": err.Error()})
			continue
		}
		novo, err := s.Caderno.Adiciona(ctx, caderno, it.Titulo, texto)
		if err != nil {
			falhas++
			// Mantém a fonte antiga (se havia) e anota o erro.
			grava(it.FonteID, err.Error(), it.Enviada)
			e.Emite("fonte", map[string]any{"chave": it.Chave, "titulo": it.Titulo, "acao": "falhou", "erro": err.Error()})
			continue
		}
		acao, erroRemove := "enviada", ""
		if it.FonteID != "" && it.FonteID != novo {
			acao = "substituida"
			if err := s.Caderno.Remove(ctx, caderno, it.FonteID); err != nil {
				erroRemove = "a fonte nova entrou, mas a antiga (" + it.FonteID + ") não saiu: " + err.Error()
			}
		}
		grava(novo, erroRemove, it.Versao)
		if _, err := s.DB.Exec(`UPDATE especialista_espelho SET caracteres = ? WHERE caderno = ? AND chave = ?`, utf8.RuneCountInString(texto), caderno, it.Chave); err != nil {
			log.Printf("especialista: espelho: %v", err)
		}
		enviados++
		e.Emite("fonte", map[string]any{"chave": it.Chave, "titulo": it.Titulo, "acao": acao, "fonte_id": novo, "anterior": it.FonteID,
			"caracteres": utf8.RuneCountInString(texto), "erro": erroRemove})
	}
	e.Emite("concluido", map[string]any{"enviados": enviados, "iguais": iguais, "falhas": falhas})
}
