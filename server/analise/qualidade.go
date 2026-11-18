package analise

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"

	"loltracking/server/banco"
	"loltracking/server/rastreio"
)

// Checagem de qualidade do DADO gravado ("leitura básica", pedido do dono em 2026-11-17: validar
// depois que a captura termina). Roda em cada Recalcula (ao fim de cada passe e no POST
// .../analise) e guarda, por passe, a contagem por tipo e os instantes (no máximo
// MaxPontosQualidade). Não muda nada no dado: só aponta o que conferir. Vocabulário em dados.md.

// Tipos de ponto da checagem.
const (
	QSalto         = "salto"             // a posição pulou sem ser morte, renascer, volta à base
	QBuraco        = "buraco"            // buraco de tempo sem amostra
	QForaDoMapa    = "fora_do_mapa"      // posição fora dos limites do mapa
	QParado        = "parado"            // vivo, longe da base, no MESMO ponto exato por tempo demais
	QNivelVoltou   = "nivel_voltou"      // nível andando para trás
	QCSVoltou      = "cs_voltou"         // CS andando para trás
	QTaxaBaixa     = "taxa_baixa"        // passe com menos amostras por segundo de jogo que o mínimo
	QRetravas      = "retravas"          // a câmera perdeu a trava muitas vezes
	QCalibracao    = "calibracao"        // erro de calibração da posição acima do normal
	QConferencia   = "conferencia"       // a leitura da tela discorda da Riot acima do normal
	QPasseRepetido = "passe_repetido"    // dois passes concluídos do mesmo jogador na partida
	QTrabalho      = "trabalho_sobrando" // amostras de trabalho sobrando de passe já fechado
)

// Limiares (medidos na 3304217746: calibração mediana 17–34 unidades; a conferência com a Riot
// discorda em 0–2% das amostras num passe bom).
const (
	MaxPontosQualidade = 50
	SaltoVelocidade    = 1200.0 // unidades/s: acima disto entre duas amostras não é andar
	// SaltoMinimo: avanços, Flash e o R da Diana/Vi chegam a ~1 000 unidades num quarto de
	// segundo (com 300, a 3304217746 tinha 280 "saltos", quase todos habilidade). Acima disto,
	// fora da base e sem morte, é salto que não se explica.
	SaltoMinimo       = 1500.0
	BuracoMs          = 3000
	ParadoMs          = 20000
	TaxaMinima        = 3.0
	RetravasPorMinuto = 2.0
	CalibracaoMaxima  = 150.0
	// ConferenciaMaxima: fração das amostras conferidas que discordam da Riot.
	ConferenciaMaxima = 0.05
	mapaMin, mapaMaxX  = -120.0, 14870.0
	mapaMaxY           = 14980.0
)

// Fontes de cada time (onde se renasce e para onde se volta).
// (RaioBase é o da base inteira, fronteiras.go: quem pula para dentro dela voltou à base.)
var fonte = map[int][2]float64{100: {FonteAzul.X, FonteAzul.Y}, 200: {FonteVermelha.X, FonteVermelha.Y}}

// PontoQualidade é um instante para conferir.
type PontoQualidade struct {
	Tipo  string `json:"tipo"`
	TMs   int64  `json:"t_ms"`
	Texto string `json:"texto"`
}

// QualidadePasse é o resultado de um passe.
type QualidadePasse struct {
	PasseID        int64            `json:"passe_id"`
	ParticipanteID int              `json:"participante_id"`
	Contagem       map[string]int   `json:"contagem"`
	Pontos         []PontoQualidade `json:"pontos"`
	Total          int              `json:"total"`
}

func perto(x, y float64, p [2]float64, r float64) bool { return math.Hypot(x-p[0], y-p[1]) < r }

// ConfereQualidade olha as amostras de um passe (do jogador do `time`) e os números do passe.
func ConfereQualidade(p rastreio.Passe, time int, as []rastreio.Amostra) QualidadePasse {
	q := QualidadePasse{PasseID: p.ID, ParticipanteID: p.ParticipanteID, Contagem: map[string]int{}, Pontos: []PontoQualidade{}}
	add := func(tipo string, t int64, texto string) {
		q.Contagem[tipo]++
		q.Total++
		if len(q.Pontos) < MaxPontosQualidade {
			q.Pontos = append(q.Pontos, PontoQualidade{Tipo: tipo, TMs: t, Texto: texto})
		}
	}
	base := fonte[time]
	var ant *rastreio.Amostra
	paradoDesde := int64(-1)
	paradoAnotado := false
	for i := range as {
		a := &as[i]
		temPos := a.X != nil && a.Y != nil
		if temPos && (*a.X < mapaMin || *a.X > mapaMaxX || *a.Y < mapaMin || *a.Y > mapaMaxY) {
			add(QForaDoMapa, a.TMs, fmt.Sprintf("posição (%.0f, %.0f) fora do mapa", *a.X, *a.Y))
		}
		if ant != nil {
			dt := a.TMs - ant.TMs
			if dt > BuracoMs {
				add(QBuraco, ant.TMs, fmt.Sprintf("%.1f s sem amostra", float64(dt)/1000))
			}
			if a.Nivel > 0 && ant.Nivel > 0 && a.Nivel < ant.Nivel {
				add(QNivelVoltou, a.TMs, fmt.Sprintf("nível %d → %d", ant.Nivel, a.Nivel))
			}
			if a.CS > 0 && ant.CS > 0 && a.CS < ant.CS {
				add(QCSVoltou, a.TMs, fmt.Sprintf("CS %d → %d", ant.CS, a.CS))
			}
			if temPos && ant.X != nil && ant.Y != nil && !a.Morto && !ant.Morto && dt > 0 {
				d := math.Hypot(*a.X-*ant.X, *a.Y-*ant.Y)
				legit := a.Origem == "base" || ant.Origem == "base" || a.Origem == "morte" || ant.Origem == "morte" ||
					perto(*a.X, *a.Y, base, RaioBase) || perto(*ant.X, *ant.Y, base, RaioBase) // recall, renascer, teleporte saindo da base
				if d > SaltoMinimo && d > SaltoVelocidade*float64(dt)/1000 && !legit {
					add(QSalto, a.TMs, fmt.Sprintf("pulou %.0f unidades em %.1f s", d, float64(dt)/1000))
				}
			}
			// Vivo, longe da base, no mesmo ponto exato: trava perdida que não foi marcada.
			mesmo := temPos && ant.X != nil && ant.Y != nil && *a.X == *ant.X && *a.Y == *ant.Y
			if mesmo && !a.Morto && !perto(*a.X, *a.Y, base, RaioBase) {
				if paradoDesde < 0 {
					paradoDesde = ant.TMs
				}
				if !paradoAnotado && a.TMs-paradoDesde >= ParadoMs {
					add(QParado, paradoDesde, fmt.Sprintf("vivo no mesmo ponto exato por %.0f s ou mais", float64(a.TMs-paradoDesde)/1000))
					paradoAnotado = true
				}
			} else {
				paradoDesde, paradoAnotado = -1, false
			}
		}
		ant = a
	}
	if p.Taxa != nil && *p.Taxa < TaxaMinima && p.Amostras > 0 {
		add(QTaxaBaixa, 0, fmt.Sprintf("%.1f amostras por segundo de jogo (mínimo %.0f)", *p.Taxa, TaxaMinima))
	}
	if min := float64(p.DuracaoMs) / 60000; min > 0 && float64(p.Retravas)/min > RetravasPorMinuto {
		add(QRetravas, 0, fmt.Sprintf("%d retravas da câmera em %.0f min", p.Retravas, min))
	}
	if p.ErroCalibracaoMediano != nil && *p.ErroCalibracaoMediano > CalibracaoMaxima {
		add(QCalibracao, 0, fmt.Sprintf("erro mediano da calibração %.0f unidades", *p.ErroCalibracaoMediano))
	}
	if p.ConferenciaN != nil && p.ConferenciaIguais != nil && *p.ConferenciaN > 0 {
		fora := float64(*p.ConferenciaN-*p.ConferenciaIguais) / float64(*p.ConferenciaN)
		if fora > ConferenciaMaxima {
			add(QConferencia, 0, fmt.Sprintf("a leitura da tela discorda da Riot em %.0f%% das %d amostras conferidas", fora*100, *p.ConferenciaN))
		}
	}
	return q
}

// MigraQualidade cria a tabela (chamada por Migra).
func migraQualidade(db *banco.DB) error {
	return banco.Executa(db, `CREATE TABLE IF NOT EXISTS qualidade_passes (
		passe_id        INTEGER PRIMARY KEY,
		game_id         INTEGER NOT NULL,
		participante_id INTEGER NOT NULL,
		contagem        TEXT NOT NULL,
		pontos          TEXT NOT NULL,
		total           INTEGER NOT NULL,
		calculada_em    TEXT NOT NULL
	)`, `CREATE INDEX IF NOT EXISTS qualidade_passes_partida ON qualidade_passes (game_id)`)
}

// confereQualidadeDaPartida roda a checagem de cada passe compactado e as checagens de
// armazenamento da partida, e grava.
func confereQualidadeDaPartida(db *banco.DB, gameID int64, passes []rastreio.Passe, timeDe map[int]int) error {
	agora := banco.Agora()
	// Passe repetido: vale o concluído mais recente (MelhorPasse); os outros são apontados.
	concluidos := map[int][]rastreio.Passe{}
	for _, p := range passes {
		if p.Estado == rastreio.Concluido {
			concluidos[p.ParticipanteID] = append(concluidos[p.ParticipanteID], p)
		}
	}
	for _, p := range passes {
		as, err := rastreio.AmostrasDoPasse(db, p)
		if err != nil {
			return fmt.Errorf("amostras do passe %d: %w", p.ID, err)
		}
		q := ConfereQualidade(p, timeDe[p.ParticipanteID], as)
		if l := concluidos[p.ParticipanteID]; len(l) > 1 && p.Estado == rastreio.Concluido {
			vale, _, _ := rastreio.MelhorPasse(db, gameID, p.ParticipanteID)
			if vale.ID != p.ID {
				q.Contagem[QPasseRepetido]++
				q.Total++
				q.Pontos = append(q.Pontos, PontoQualidade{Tipo: QPasseRepetido, Texto: fmt.Sprintf("há outro passe concluído deste jogador; vale o passe %d", vale.ID)})
			}
		}
		var sobra int
		db.QueryRow(`SELECT COUNT(*) FROM amostras_brutas WHERE passe_id = ?`, p.ID).Scan(&sobra)
		if sobra > 0 && p.Compactado {
			q.Contagem[QTrabalho]++
			q.Total++
			q.Pontos = append(q.Pontos, PontoQualidade{Tipo: QTrabalho, Texto: fmt.Sprintf("%d amostras de trabalho sobrando de passe já fechado", sobra)})
		}
		sort.SliceStable(q.Pontos, func(a, b int) bool { return q.Pontos[a].TMs < q.Pontos[b].TMs })
		c, _ := json.Marshal(q.Contagem)
		pt, _ := json.Marshal(q.Pontos)
		if _, err := db.Exec(`INSERT INTO qualidade_passes (passe_id, game_id, participante_id, contagem, pontos, total, calculada_em)
			VALUES (?,?,?,?,?,?,?) ON CONFLICT (passe_id) DO UPDATE SET contagem = excluded.contagem, pontos = excluded.pontos,
			total = excluded.total, calculada_em = excluded.calculada_em`, p.ID, gameID, p.ParticipanteID, string(c), string(pt), q.Total, agora); err != nil {
			return err
		}
	}
	return nil
}

// QualidadeDaPartida é a resposta de GET /api/partidas/{id}/qualidade.
type QualidadeDaPartida struct {
	GameID int64            `json:"game_id"`
	Total  int              `json:"total"`
	Passes []QualidadePasse `json:"passes"`
}

// LeQualidade devolve o que a última checagem achou (lista vazia = nunca conferida ou sem passe).
func LeQualidade(db *banco.DB, gameID int64) (QualidadeDaPartida, error) {
	r := QualidadeDaPartida{GameID: gameID, Passes: []QualidadePasse{}}
	linhas, err := db.Query(`SELECT passe_id, participante_id, contagem, pontos, total FROM qualidade_passes WHERE game_id = ?
		ORDER BY participante_id, passe_id`, gameID)
	if err != nil {
		return r, err
	}
	defer linhas.Close()
	for linhas.Next() {
		var q QualidadePasse
		var c, p string
		if err := linhas.Scan(&q.PasseID, &q.ParticipanteID, &c, &p, &q.Total); err != nil {
			return r, err
		}
		json.Unmarshal([]byte(c), &q.Contagem)
		json.Unmarshal([]byte(p), &q.Pontos)
		r.Total += q.Total
		r.Passes = append(r.Passes, q)
	}
	return r, linhas.Err()
}
