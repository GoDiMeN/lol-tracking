package analise

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"

	"loltracking/server/arquivo"
	"loltracking/server/banco"
	"loltracking/server/rastreio"
)

// ErrNaoAnalisada: a partida ainda não passou por Recalcula.
var ErrNaoAnalisada = errors.New("a partida ainda não foi analisada")

// ListaRetratos devolve os retratos dos abates da última análise.
func ListaRetratos(db *banco.DB, gameID int64) ([]Retrato, error) {
	l := []Retrato{}
	texto, ok, err := arquivo.Le(db, gameID, arquivoRetratos)
	if err != nil || !ok {
		return l, err
	}
	for _, linha := range bytes.Split(texto, []byte{'\n'}) {
		if len(linha) == 0 {
			continue
		}
		var r Retrato
		if err := json.Unmarshal(linha, &r); err != nil {
			return nil, err
		}
		l = append(l, r)
	}
	return l, nil
}

// LeRetrato devolve o retrato do abate n.
func LeRetrato(db *banco.DB, gameID int64, n int) (Retrato, error) {
	l, err := ListaRetratos(db, gameID)
	if err != nil {
		return Retrato{}, err
	}
	for _, r := range l {
		if r.N == n {
			return r, nil
		}
	}
	return Retrato{}, ErrNaoAnalisada
}

// IndicadorComDef é o indicador com o rótulo e a unidade (para a tela).
type IndicadorComDef struct {
	Indicador
	Rotulo  string `json:"rotulo"`
	Unidade string `json:"unidade"`
	Fonte   string `json:"fonte"`
	Melhor  string `json:"melhor"`
}

// ListaIndicadores devolve os indicadores da partida (pid 0 = os 10), na ordem
// do vocabulário.
func ListaIndicadores(db *banco.DB, gameID int64, pid int) ([]IndicadorComDef, error) {
	q := `SELECT participante_id, indicador, valor, aproximado, detalhe FROM indicadores WHERE game_id = ?`
	args := []any{gameID}
	if pid > 0 {
		q += ` AND participante_id = ?`
		args = append(args, pid)
	}
	linhas, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	ordem := map[string]int{}
	for i, d := range Vocabulario {
		ordem[d.Nome] = i
	}
	l := []IndicadorComDef{}
	for linhas.Next() {
		var in IndicadorComDef
		var v sql.NullFloat64
		var det string
		if err := linhas.Scan(&in.ParticipanteID, &in.Indicador.Indicador, &v, &in.Aproximado, &det); err != nil {
			return nil, err
		}
		if v.Valid {
			x := v.Float64
			in.Valor = &x
		}
		json.Unmarshal([]byte(det), &in.Detalhe)
		if in.Detalhe == nil {
			in.Detalhe = map[string]any{}
		}
		d := defPorNome[in.Indicador.Indicador]
		in.Rotulo, in.Unidade, in.Fonte, in.Melhor = d.Rotulo, d.Unidade, d.Fonte, d.Melhor
		l = append(l, in)
	}
	sort.SliceStable(l, func(a, b int) bool {
		if l[a].ParticipanteID != l[b].ParticipanteID {
			return l[a].ParticipanteID < l[b].ParticipanteID
		}
		return ordem[l[a].Indicador.Indicador] < ordem[l[b].Indicador.Indicador]
	})
	return l, linhas.Err()
}

// ValorIndicador é um valor de um indicador numa partida (para o estudo).
type ValorIndicador struct {
	GameID         int64          `json:"game_id"`
	ParticipanteID int            `json:"participante_id"`
	Valor          *float64       `json:"valor"`
	Aproximado     bool           `json:"aproximado"`
	Detalhe        map[string]any `json:"detalhe"`
}

// IndicadoresDoJogador devolve, para cada partida pedida, os indicadores do
// participante daquela partida: partida → nome → valor.
func IndicadoresDoJogador(db *banco.DB, pidPorPartida map[int64]int) (map[int64]map[string]ValorIndicador, error) {
	r := map[int64]map[string]ValorIndicador{}
	for g, pid := range pidPorPartida {
		linhas, err := db.Query(`SELECT indicador, valor, aproximado, detalhe FROM indicadores WHERE game_id = ? AND participante_id = ?`, g, pid)
		if err != nil {
			return nil, err
		}
		m := map[string]ValorIndicador{}
		for linhas.Next() {
			var nome, det string
			var v sql.NullFloat64
			vi := ValorIndicador{GameID: g, ParticipanteID: pid}
			if err := linhas.Scan(&nome, &v, &vi.Aproximado, &det); err != nil {
				linhas.Close()
				return nil, err
			}
			if v.Valid {
				x := v.Float64
				vi.Valor = &x
			}
			json.Unmarshal([]byte(det), &vi.Detalhe)
			m[nome] = vi
		}
		linhas.Close()
		r[g] = m
	}
	return r, nil
}

// Trecho de atividade de um jogador (da cadeia).
type TrechoAtividade struct {
	ParticipanteID int      `json:"participante_id"`
	TMs            int64    `json:"t_ms"`
	FimMs          int64    `json:"fim_ms"`
	Atividade      string   `json:"atividade"`
	Com            []int    `json:"com"`
	Zona           string   `json:"zona"`
	Confianca      string   `json:"confianca"`
	Sinais         []string `json:"sinais"`
	// Alvo: qual objetivo ou estrutura (só em "objetivo" e "torre").
	Alvo string `json:"alvo,omitempty"`
	// Farmando: só em "coletando_ouro": o suporte também está farmando.
	Farmando *bool `json:"farmando,omitempty"`
}

// ListaAtividades devolve os trechos de atividade que tocam [de, ate] dos
// rastreados (pid 0 = todos).
func ListaAtividades(db *banco.DB, gameID int64, pid int, de, ate int64) ([]TrechoAtividade, error) {
	l := []TrechoAtividade{}
	for p := 1; p <= 10; p++ {
		if pid > 0 && p != pid {
			continue
		}
		rc, err := rastreio.ListaCadeia(db, gameID, p, map[string]bool{"atividade": true})
		if errors.Is(err, rastreio.ErrSemAmostras) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, it := range rc.Itens {
			fim := int64(numero(it.Dados["fim_ms"]))
			if fim < de || it.TMs > ate {
				continue
			}
			t := TrechoAtividade{ParticipanteID: p, TMs: it.TMs, FimMs: fim, Com: []int{}, Sinais: []string{}}
			t.Atividade, _ = it.Dados["atividade"].(string)
			t.Zona, _ = it.Dados["zona"].(string)
			t.Confianca, _ = it.Dados["confianca"].(string)
			t.Alvo, _ = it.Dados["alvo"].(string)
			if f, ok := it.Dados["farmando"].(bool); ok {
				t.Farmando = &f
			}
			if c, ok := it.Dados["com"].([]any); ok {
				for _, x := range c {
					t.Com = append(t.Com, int(numero(x)))
				}
			}
			if s, ok := it.Dados["sinais"].([]any); ok {
				for _, x := range s {
					if str, ok := x.(string); ok {
						t.Sinais = append(t.Sinais, str)
					}
				}
			}
			l = append(l, t)
		}
	}
	sort.SliceStable(l, func(a, b int) bool { return l[a].TMs < l[b].TMs })
	return l, nil
}

// Saida é uma saída de rota (rotador) ou visita a rota (caçador), da cadeia.
type Saida struct {
	ParticipanteID int            `json:"participante_id"`
	Tipo           string         `json:"tipo"` // saiu_da_rota ou entrou_em_rota
	TMs            int64          `json:"t_ms"`
	Dados          map[string]any `json:"dados"`
}

// ListaSaidas devolve as saídas de rota e visitas dos rastreados (pid 0 = todos).
func ListaSaidas(db *banco.DB, gameID int64, pid int) ([]Saida, error) {
	l := []Saida{}
	for p := 1; p <= 10; p++ {
		if pid > 0 && p != pid {
			continue
		}
		rc, err := rastreio.ListaCadeia(db, gameID, p, map[string]bool{"saiu_da_rota": true, "entrou_em_rota": true})
		if errors.Is(err, rastreio.ErrSemAmostras) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, it := range rc.Itens {
			l = append(l, Saida{ParticipanteID: p, Tipo: it.Tipo, TMs: it.TMs, Dados: it.Dados})
		}
	}
	sort.SliceStable(l, func(a, b int) bool { return l[a].TMs < l[b].TMs })
	return l, nil
}

// CSDoJogador é a conferência do CS de um jogador.
type CSDoJogador struct {
	ParticipanteID int       `json:"participante_id"`
	Minutos        []LinhaCS `json:"minutos"`
	Divergencias   int       `json:"divergencias"`
}

// ConfereCS faz a conferência do CS de todos (pid 0) ou de um.
func ConfereCS(db *banco.DB, gameID int64, pid int) ([]CSDoJogador, error) {
	dp, err := carrega(db, gameID)
	if err != nil {
		return nil, err
	}
	l := []CSDoJogador{}
	for _, p := range dp.pt.Participantes {
		if pid > 0 && p.ParticipanteID != pid {
			continue
		}
		c := CSDoJogador{ParticipanteID: p.ParticipanteID, Minutos: dp.ConferenciaCS(p.ParticipanteID)}
		for _, m := range c.Minutos {
			if m.Divergente {
				c.Divergencias++
			}
		}
		l = append(l, c)
	}
	return l, nil
}
