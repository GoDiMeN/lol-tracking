package rastreio

import (
	"encoding/json"
	"fmt"
	"sort"
)

// LinhaDoTempo é o que o rastreio usa de game-timelines/{id} do LCU: posição
// exata e totais de cada jogador por minuto, e os eventos.
type LinhaDoTempo struct {
	Quadros []QuadroLT
	Eventos []EventoLT
}

// QuadroLT é um quadro (de minuto em minuto).
type QuadroLT struct {
	TMs       int64
	Jogadores map[int]JogadorLT
}

// JogadorLT é o participantFrame.
type JogadorLT struct {
	X, Y      float64
	Nivel     int
	CS        int // minionsKilled + jungleMinionsKilled
	OuroAtual int
	OuroTotal int
	XP        int
}

// EventoLT é um evento da linha do tempo.
type EventoLT struct {
	Tipo         string
	TMs          int64
	Quem         int
	EmQuem       int
	Assistencias []int
	X, Y         *float64
	Bruto        json.RawMessage
}

// LeLinhaDoTempo interpreta o JSON cru.
func LeLinhaDoTempo(bruto []byte) (LinhaDoTempo, error) {
	var r struct {
		Frames []struct {
			Timestamp         int64 `json:"timestamp"`
			ParticipantFrames map[string]struct {
				ParticipantID       int `json:"participantId"`
				Level               int `json:"level"`
				MinionsKilled       int `json:"minionsKilled"`
				JungleMinionsKilled int `json:"jungleMinionsKilled"`
				CurrentGold         int `json:"currentGold"`
				TotalGold           int `json:"totalGold"`
				XP                  int `json:"xp"`
				Position            *struct {
					X float64 `json:"x"`
					Y float64 `json:"y"`
				} `json:"position"`
			} `json:"participantFrames"`
			Events []json.RawMessage `json:"events"`
		} `json:"frames"`
	}
	if err := json.Unmarshal(bruto, &r); err != nil {
		return LinhaDoTempo{}, fmt.Errorf("linha do tempo em formato inesperado: %w", err)
	}
	var lt LinhaDoTempo
	for _, f := range r.Frames {
		q := QuadroLT{TMs: f.Timestamp, Jogadores: map[int]JogadorLT{}}
		for _, pf := range f.ParticipantFrames {
			j := JogadorLT{Nivel: pf.Level, CS: pf.MinionsKilled + pf.JungleMinionsKilled, OuroAtual: pf.CurrentGold,
				OuroTotal: pf.TotalGold, XP: pf.XP}
			if pf.Position != nil {
				j.X, j.Y = pf.Position.X, pf.Position.Y
			}
			q.Jogadores[pf.ParticipantID] = j
		}
		lt.Quadros = append(lt.Quadros, q)
		for _, b := range f.Events {
			var e struct {
				Type      string `json:"type"`
				Timestamp int64  `json:"timestamp"`
				KillerID  int    `json:"killerId"`
				VictimID  int    `json:"victimId"`
				Assist    []int  `json:"assistingParticipantIds"`
				Position  *struct {
					X float64 `json:"x"`
					Y float64 `json:"y"`
				} `json:"position"`
			}
			if json.Unmarshal(b, &e) != nil {
				continue
			}
			ev := EventoLT{Tipo: e.Type, TMs: e.Timestamp, Quem: e.KillerID, EmQuem: e.VictimID, Assistencias: e.Assist,
				Bruto: append(json.RawMessage(nil), b...)}
			if e.Position != nil {
				x, y := e.Position.X, e.Position.Y
				ev.X, ev.Y = &x, &y
			}
			lt.Eventos = append(lt.Eventos, ev)
		}
	}
	sort.SliceStable(lt.Eventos, func(a, b int) bool { return lt.Eventos[a].TMs < lt.Eventos[b].TMs })
	return lt, nil
}

// MorteAntes é a última morte do participante com carimbo até t+folga (o
// carimbo do evento e o "isDead" do jogo diferem por fração de segundo).
func (lt LinhaDoTempo) MorteAntes(pid int, tMs int64) *EventoLT {
	var achou *EventoLT
	for i := range lt.Eventos {
		e := &lt.Eventos[i]
		if e.Tipo == "CHAMPION_KILL" && e.EmQuem == pid && e.TMs <= tMs+3000 {
			achou = e
		}
	}
	return achou
}

// PosicaoInterpolada é a posição do participante em t pelos quadros de minuto
// (grosseira: o mapa usa para quem não foi rastreado).
func (lt LinhaDoTempo) PosicaoInterpolada(pid int, tMs int64) (float64, float64, bool) {
	q := lt.Quadros
	for i := 1; i < len(q); i++ {
		if q[i].TMs >= tMs {
			a, okA := q[i-1].Jogadores[pid]
			b, okB := q[i].Jogadores[pid]
			if !okA || !okB {
				return 0, 0, false
			}
			f := float64(tMs-q[i-1].TMs) / float64(q[i].TMs-q[i-1].TMs)
			return a.X + (b.X-a.X)*f, a.Y + (b.Y-a.Y)*f, true
		}
	}
	return 0, 0, false
}

// Fontes (lugar de renascer) de cada time no mapa 11.
var fontes = map[string][2]float64{"ORDER": {400, 420}, "CHAOS": {14340, 14390}}

// FonteDoTime devolve a fonte pelo time do LCU (100/200).
func FonteDoTime(time int) [2]float64 {
	if time == 200 {
		return fontes["CHAOS"]
	}
	return fontes["ORDER"]
}
