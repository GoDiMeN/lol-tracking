package rastreio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"sort"
	"sync"
	"time"

	"loltracking/server/banco"
	"loltracking/server/riot"
)

// Conferência da leitura da tela contra a API da Riot (opcional). A linha do
// tempo da match-v5 tem, por jogador e por minuto, championStats (health,
// healthMax, power, powerMax, attackDamage, armor...). Depois de um passe, as
// leituras da tela nos minutos cheios são comparadas com esses valores e o
// resultado fica no passe (conferencia_*). Sem chave (ou chave vencida), pula
// em silêncio: nada do núcleo depende disso.

// FonteRiot dá o JSON cru da linha do tempo da match-v5 (riot.Cliente).
type FonteRiot interface {
	LinhaDoTempo(ctx context.Context, gameID int64) ([]byte, error)
}

// ConfCampo é o resultado de um campo.
type ConfCampo struct {
	N        int     `json:"n"`
	Iguais   int     `json:"iguais"`
	MaiorDif float64 `json:"maior_dif"`
}

// Conferencia é o resultado de um passe.
type Conferencia struct {
	N        int                   `json:"n"`
	Iguais   int                   `json:"iguais"`
	MaiorDif float64               `json:"maior_dif"`
	Minutos  int                   `json:"minutos"`
	Campos   map[string]*ConfCampo `json:"campos"`
}

type statsRiot struct {
	Health, HealthMax, Power, PowerMax, AttackDamage, AbilityPower, Armor, MagicResist, AttackSpeed, MovementSpeed float64
}

type linhaRiot struct {
	Info struct {
		Frames []struct {
			Timestamp         int64 `json:"timestamp"`
			ParticipantFrames map[string]struct {
				Level         int `json:"level"`
				ChampionStats struct {
					Health        float64 `json:"health"`
					HealthMax     float64 `json:"healthMax"`
					Power         float64 `json:"power"`
					PowerMax      float64 `json:"powerMax"`
					AttackDamage  float64 `json:"attackDamage"`
					AbilityPower  float64 `json:"abilityPower"`
					Armor         float64 `json:"armor"`
					MagicResist   float64 `json:"magicResist"`
					AttackSpeed   float64 `json:"attackSpeed"`
					MovementSpeed float64 `json:"movementSpeed"`
				} `json:"championStats"`
			} `json:"participantFrames"`
		} `json:"frames"`
	} `json:"info"`
}

// Tolerâncias: os máximos e atributos têm de bater exatos (a velocidade de
// ataque, ao centésimo); vida e recurso atuais mudam em combate entre o quadro
// da Riot e a amostra mais perto (até 250 ms), então ±3% do máximo.
const tolAtual = 0.03

// ConfereComRiot compara as amostras do passe com a linha do tempo da Riot.
func ConfereComRiot(bruto []byte, pid int, as []Amostra) (Conferencia, error) {
	var lt linhaRiot
	if err := json.Unmarshal(bruto, &lt); err != nil {
		return Conferencia{}, fmt.Errorf("linha do tempo da Riot: %w", err)
	}
	c := Conferencia{Campos: map[string]*ConfCampo{}}
	compara := func(nome string, lido *float64, riot, tol float64) {
		if lido == nil {
			return
		}
		cc := c.Campos[nome]
		if cc == nil {
			cc = &ConfCampo{}
			c.Campos[nome] = cc
		}
		d := math.Abs(*lido - riot)
		cc.N++
		c.N++
		if d <= tol+1e-9 {
			cc.Iguais++
			c.Iguais++
		}
		cc.MaiorDif = math.Max(cc.MaiorDif, math.Round(d*100)/100)
		c.MaiorDif = math.Max(c.MaiorDif, cc.MaiorDif)
	}
	chave := fmt.Sprint(pid)
	for _, q := range lt.Info.Frames {
		pf, ok := q.ParticipantFrames[chave]
		if !ok || q.Timestamp <= 0 {
			continue
		}
		// Amostra mais perto do quadro (até 500 ms), viva e com o painel lido.
		i := sort.Search(len(as), func(k int) bool { return as[k].TMs >= q.Timestamp })
		var a *Amostra
		for _, k := range []int{i - 1, i} {
			if k >= 0 && k < len(as) && math.Abs(float64(as[k].TMs-q.Timestamp)) <= 500 && !as[k].Morto &&
				(a == nil || math.Abs(float64(as[k].TMs-q.Timestamp)) < math.Abs(float64(a.TMs-q.Timestamp))) {
				a = &as[k]
			}
		}
		// O quadro da Riot pode cair num instante de subida de nível: o painel
		// já mostra os valores do nível novo ou ainda do velho. Pula.
		if a == nil || (a.Nivel > 0 && pf.Level != a.Nivel) {
			continue
		}
		s := pf.ChampionStats
		c.Minutos++
		compara("vida_max", a.VidaMax, s.HealthMax, 0)
		compara("vida", a.Vida, s.Health, tolAtual*s.HealthMax)
		compara("mana_max", a.ManaMax, s.PowerMax, 0)
		compara("mana", a.Mana, s.Power, tolAtual*s.PowerMax)
		compara("atr_ad", a.Atr.AD, s.AttackDamage, 0)
		compara("atr_ap", a.Atr.AP, s.AbilityPower, 0)
		compara("atr_armadura", a.Atr.Armadura, s.Armor, 0)
		compara("atr_rm", a.Atr.RM, s.MagicResist, 0)
		compara("atr_vel_ataque", a.Atr.VelAtaque, s.AttackSpeed/100, 0.01)
		compara("atr_vel_mov", a.Atr.VelMov, s.MovementSpeed, 0)
	}
	return c, nil
}

// cache da linha do tempo da Riot por partida (um pedido por partida, não por passe).
var (
	cacheRiotMu sync.Mutex
	cacheRiot   = map[int64][]byte{}
)

// conferePasse busca a linha do tempo (se há fonte e chave), compara e grava
// no passe. Erro de chave/rede: só log, sem a chave.
func conferePasse(db *banco.DB, fonte FonteRiot, p Passe) (*Conferencia, error) {
	if fonte == nil {
		return nil, nil
	}
	cacheRiotMu.Lock()
	bruto, ok := cacheRiot[p.GameID]
	cacheRiotMu.Unlock()
	if !ok {
		ctx, cancela := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancela()
		b, err := fonte.LinhaDoTempo(ctx, p.GameID)
		if err != nil {
			return nil, err
		}
		bruto = b
		cacheRiotMu.Lock()
		cacheRiot[p.GameID] = b
		cacheRiotMu.Unlock()
	}
	// Relido: o passe acabou de ser compactado.
	if p2, err := LePasse(db, p.ID); err == nil {
		p = p2
	}
	as, _, err := amostrasDoPasse(db, p)
	if err != nil {
		return nil, err
	}
	c, err := ConfereComRiot(bruto, p.ParticipanteID, as)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`UPDATE passes SET conferencia_riot = ?, conferencia_n = ?, conferencia_iguais = ?,
		conferencia_maior_dif = ? WHERE id = ?`, jsonTexto(c), c.N, c.Iguais, c.MaiorDif, p.ID); err != nil {
		return nil, err
	}
	return &c, nil
}

func logConferencia(p Passe, err error) {
	if err != nil && !errors.Is(err, riot.ErrSemChave) {
		log.Printf("rastreio: conferência com a Riot do passe %d pulada: %v", p.ID, err)
	}
}
