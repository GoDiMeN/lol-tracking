package ia

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Falso é a IA dos testes e do LOL_TESTE=1: determinística, sem rede nem
// processo. Responde no formato que o app pede (o relatório em JSON, os
// pedidos de dado da conversa), montado a partir do próprio texto recebido,
// para a interface e os testes exercitarem o caminho inteiro.
type Falso struct {
	// Responde, se definido, substitui a resposta padrão (os testes usam).
	Responde func(p Pedido) (string, error)
	// Atraso entre os pedaços do fluxo (o modo teste usa um pouco, para a
	// tela mostrar o texto chegando).
	Atraso time.Duration
	// Chamadas: quantas vezes Conversa foi chamada (os testes conferem).
	Chamadas int
	// Pedidos: cópia de cada pedido recebido.
	Pedidos []Pedido

	mu sync.Mutex
}

// Estado: sempre disponível.
func (f *Falso) Estado(context.Context) Estado {
	return Estado{Disponivel: true, Implementacao: "falso", Versao: "IA falsa (modo teste)",
		Modelos: map[string]string{TipoAnalise: "falso-analise", TipoConversa: "falso-conversa"}}
}

// Conversa devolve a resposta em pedaços de até 40 caracteres.
func (f *Falso) Conversa(ctx context.Context, p Pedido, aoTrecho func(string)) (Resposta, error) {
	f.mu.Lock()
	f.Chamadas++
	f.Pedidos = append(f.Pedidos, p)
	f.mu.Unlock()
	inicio := time.Now()
	var texto string
	var err error
	if f.Responde != nil {
		texto, err = f.Responde(p)
	} else {
		texto = respostaFalsa(p)
	}
	res := Resposta{Modelo: "falso-" + p.Tipo, CaracteresEntrada: caracteresDoPedido(p)}
	if err != nil {
		return res, err
	}
	r := []rune(texto)
	for i := 0; i < len(r); i += 40 {
		if err := ctx.Err(); err != nil {
			return res, &erroIA{"a chamada à IA foi cancelada", err}
		}
		fim := min(i+40, len(r))
		if aoTrecho != nil {
			aoTrecho(string(r[i:fim]))
		}
		if f.Atraso > 0 {
			select {
			case <-ctx.Done():
				return res, &erroIA{"a chamada à IA foi cancelada", ctx.Err()}
			case <-time.After(f.Atraso):
			}
		}
	}
	res.Texto = texto
	res.CaracteresSaida = len(r)
	res.DuracaoMs = time.Since(inicio).Milliseconds()
	return res, nil
}

var (
	reInstante = regexp.MustCompile(`\b(\d{1,2}):([0-5]\d)\b`)
	reFoco     = regexp.MustCompile(`(?m)^FOCO: participante (\d+)`)
	reVozes    = regexp.MustCompile(`(?m)^VOZES COM MATERIAL: (.*)$`)
	rePartida  = regexp.MustCompile(`partida (\d{6,})`)
)

func respostaFalsa(p Pedido) string {
	ultima := ""
	if len(p.Mensagens) > 0 {
		ultima = p.Mensagens[len(p.Mensagens)-1].Texto
	}
	if p.Tipo == TipoAnalise {
		switch {
		case strings.Contains(p.Sistema, "RELATÓRIO DE REFERÊNCIA"):
			return referenciaFalsa(ultima)
		case strings.Contains(p.Sistema, "TAREFA: a SÍNTESE"):
			return sinteseFalsa(ultima)
		}
		return relatorioFalso(ultima)
	}
	todo := p.Sistema + "\n" + TextoDaConversa(p.Mensagens)
	// Conversa geral: cita a primeira partida do contexto.
	if strings.Contains(p.Sistema, "VÁRIAS PARTIDAS") {
		if m := rePartida.FindStringSubmatch(todo); m != nil {
			t := "0:00"
			if i := reInstante.FindString(todo[strings.Index(todo, m[0]):]); i != "" {
				t = i
			}
			return fmt.Sprintf("Resposta de teste (IA falsa). A partida que mais responde é a partida %s, %s: veja o que aconteceu ali.", m[1], t)
		}
		return "Resposta de teste (IA falsa). Nenhuma partida da base responde a essa pergunta."
	}
	// Conversa da partida: com instante na pergunta e sem os dados dele
	// ainda, pede o momento (o protocolo do servidor); com os dados, responde.
	if m := reInstante.FindString(ultima); m != "" && !strings.Contains(todo, "DADOS DO INSTANTE") {
		return "PEDIDO_MOMENTO t=" + m
	}
	if strings.Contains(todo, "DADOS DO INSTANTE") {
		t := reInstante.FindString(ultima)
		return fmt.Sprintf("Resposta de teste (IA falsa). Com os dados do instante %s em mãos: o jogador em foco estava longe do time.\nPRÓXIMA PERGUNTA: E o que fazer na próxima vez em %s?", t, t)
	}
	return "Resposta de teste (IA falsa). Pergunte sobre um instante (ex.: 8:48) para ver o pedido de dados."
}

// relatorioFalso monta um relatório válido com os instantes do pacote.
func relatorioFalso(texto string) string {
	foco := 1
	if m := reFoco.FindStringSubmatch(texto); m != nil {
		fmt.Sscan(m[1], &foco)
	}
	var vozes []string
	if m := reVozes.FindStringSubmatch(texto); m != nil {
		for _, v := range strings.Split(m[1], ",") {
			if v = strings.TrimSpace(v); v != "" && v != "nenhuma" {
				vozes = append(vozes, v)
			}
		}
	}
	var instantes []string
	vistos := map[string]bool{}
	for _, m := range reInstante.FindAllString(texto, -1) {
		if !vistos[m] && m != "0:00" {
			vistos[m] = true
			instantes = append(instantes, m)
		}
	}
	pega := func(i int) string {
		if len(instantes) == 0 {
			return "1:00"
		}
		return instantes[i%len(instantes)]
	}
	r := map[string]any{
		"resumo":  fmt.Sprintf("Relatório de teste (IA falsa) do participante %d. Os instantes vêm do pacote: %s.", foco, pega(0)),
		"padroes": []any{map[string]any{"texto": "Padrão de teste: isso aconteceu 2 vezes: " + pega(0) + ", " + pega(1) + ".", "instantes": []string{pega(0), pega(1)}}},
		"situacoes": []any{
			map[string]any{"t": pega(0), "titulo": "Situação de teste 1", "situacao": "Fatos da ficha no instante " + pega(0) + ".",
				"pergunta": "Devia ter entrado nessa luta?", "vozes": vozesFalsas(vozes, pega(0)), "leitura": "Leitura de teste (sugestão).",
				"proxima_vez": "Esperar o time.", "repeticoes": []string{pega(1)}, "custo": "~300 de ouro", "impacto": 300, "fonte": "S1",
				"quadro": map[string]any{"t": pega(0), "legenda": "Quadro de teste", "realce": []int{foco}, "regiao": "rio_baixo",
					"seta": map[string]any{"de": foco, "para": "base_azul"}}},
			map[string]any{"t": pega(1), "titulo": "Situação de teste 2", "situacao": "Outro fato em " + pega(1) + ".",
				"pergunta": "Recuar ou ficar?", "vozes": vozesFalsas(vozes, pega(1)), "leitura": "Recuar.", "proxima_vez": "Recuar antes.",
				"custo": "~150 de ouro", "impacto": 150, "fonte": "S2",
				"quadro": map[string]any{"t": pega(1), "legenda": "Segundo quadro", "realce": []int{foco}}},
		},
		"jogadores_chave": []any{map[string]any{"participante": foco, "papel": "jogador em foco", "por_que": "Teste em " + pega(0) + ".", "instantes": []string{pega(0)}, "fonte": "Jogadores-chave"}},
		"build":           []any{map[string]any{"t": pega(2), "item": "item de teste", "observacao": "Fato da build.", "alternativa": "Outro item.", "fonte": "Build"}},
		"janelas": []any{map[string]any{"t": pega(2), "tipo": "perdida", "fato": "Janela de teste em " + pega(2) + ".", "o_que_fazer": "Invadir.", "fonte": "Janelas",
			"quadro": map[string]any{"t": pega(2), "legenda": "Janela", "realce": []int{foco}, "regiao": "selva_vermelha_cima"}}},
		"visao": map[string]any{"texto": "Visão de teste.", "itens": []any{map[string]any{"t": pega(0), "fato": "Sentinela de teste.", "fonte": "Visão"}}},
	}
	b, _ := json.MarshalIndent(r, "", "  ")
	return string(b)
}

func vozesFalsas(vozes []string, t string) []any {
	vs := []any{}
	for _, v := range vozes {
		vs = append(vs, map[string]any{"voz": v, "resposta": "Pela referência dada ao app, em " + t + " o caminho seria outro.", "fonte": "material da voz"})
	}
	return vs
}

var (
	reReferencia = regexp.MustCompile(`(?m)^JOGADOR DE REFERÊNCIA: participante (\d+)`)
	reCodigoRel  = regexp.MustCompile(`(?m)^### (R\d+):`)
)

// referenciaFalsa: um relatório de referência válido com os instantes do texto.
func referenciaFalsa(texto string) string {
	foco := 1
	if m := reReferencia.FindStringSubmatch(texto); m != nil {
		fmt.Sscan(m[1], &foco)
	}
	var instantes []string
	vistos := map[string]bool{}
	for _, m := range reInstante.FindAllString(texto, -1) {
		if !vistos[m] && m != "0:00" {
			vistos[m] = true
			instantes = append(instantes, m)
		}
	}
	pega := func(i int) string {
		if len(instantes) == 0 {
			return "1:00"
		}
		return instantes[i%len(instantes)]
	}
	quadro := func(t string) map[string]any {
		return map[string]any{"t": t, "legenda": "Quadro de teste", "realce": []int{foco}, "regiao": "rio_cima"}
	}
	r := map[string]any{
		"resumo": fmt.Sprintf("Relatório de referência de teste (IA falsa) do participante %d, com instantes do pacote: %s.", foco, pega(0)),
		"fases": []any{map[string]any{"fase": "rotas", "plano": "Plano de teste.", "acoes": []any{
			map[string]any{"t": pega(0), "fez": "invadiu", "em_resposta_a": "o caçador inimigo do outro lado", "fonte": "L1"}}}},
		"decisoes": []any{
			map[string]any{"t": pega(0), "titulo": "Decisão de teste 1", "fatos": "Fatos em " + pega(0) + ".", "peso": "abriu o mapa",
				"regra": "se o caçador inimigo aparece no lado oposto e a onda está empurrada, então invada, porque não há resposta", "casos": []string{pega(1)},
				"fonte": "L1", "quadro": quadro(pega(0))},
			map[string]any{"t": pega(1), "titulo": "Decisão de teste 2", "fatos": "Fatos em " + pega(1) + ".", "peso": "dragão",
				"regra": "se o dragão nasce em 30 s e o topo inimigo está morto, então agrupe no rio, porque a luta é 5 contra 4", "fonte": "L2",
				"quadro": quadro(pega(1))},
			map[string]any{"t": pega(2), "titulo": "Decisão de teste 3", "fatos": "Fatos em " + pega(2) + ".", "peso": "torre",
				"regra": "se a onda chega à torre, então empurre junto, porque a torre cai", "fonte": "L3"},
		},
		"erros":             []any{map[string]any{"t": pega(3), "fato": "Morreu sozinho.", "custo": "~300", "licao": "Ter visão antes.", "fonte": "L4", "quadro": quadro(pega(3))}},
		"nao_fez":           []any{map[string]any{"texto": "Não morreu sozinho.", "numeros": "referência 0; usuário 3", "fonte": "tabela"}},
		"nao_da_para_saber": []string{"onde estava o caçador inimigo entre as leituras por minuto"},
	}
	b, _ := json.MarshalIndent(r, "", "  ")
	return string(b)
}

// sinteseFalsa: uma síntese que cita todos os relatórios na primeira regra e
// só o primeiro na segunda.
func sinteseFalsa(texto string) string {
	var cods []string
	for _, m := range reCodigoRel.FindAllStringSubmatch(texto, -1) {
		cods = append(cods, m[1])
	}
	if len(cods) == 0 {
		cods = []string{"R1"}
	}
	r := map[string]any{
		"resumo":       "Síntese de teste (IA falsa); amostra do texto.",
		"papel_estilo": "Papel de teste.",
		"regras": []any{
			map[string]any{"regra": "se o caçador inimigo aparece no lado oposto, então invada, porque não há resposta", "relatorios": cods, "fonte": "decisões"},
			map[string]any{"regra": "se a onda chega à torre, então empurre junto, porque a torre cai", "relatorios": cods[:1], "fonte": "decisão"},
		},
		"erros_comuns":   []any{map[string]any{"texto": "Morrer sozinho sem visão.", "relatorios": cods[:1]}},
		"mudancas_patch": []any{map[string]any{"texto": "Mudança de teste.", "base": "tabela"}},
		"o_que_falta":    []string{"mais partidas"},
	}
	b, _ := json.MarshalIndent(r, "", "  ")
	return string(b)
}
