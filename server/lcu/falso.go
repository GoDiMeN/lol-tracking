package lcu

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// Falso é o LCU dos testes e do LOL_TESTE=1: determinístico, sem rede, e com o
// mesmo formato de JSON que o cliente de verdade devolveu em 2026-11-17
// (docs/arquitetura/descobertas-replay.md). Inclusive as manias medidas:
//   - o histórico ignora begIndex/endIndex e devolve tudo o que tem;
//   - o histórico traz só o participante dono, a partida inteira vem de games/{id};
//   - metadata de replay responde 404 até a partida ser apresentada (create).
//
// Dois jogadores existem: "Jogador Teste#br1" (o logado) e "Rival#br1". Cada um
// tem 25 partidas: as 12 mais novas no 16.22 (o patch "instalado"), 2 no 16.21
// e o resto no 16.10. A segunda partida do logado já está baixada.
type Falso struct {
	mu sync.Mutex
	// Fechado simula o cliente do LoL fechado: tudo devolve ErrClienteFechado.
	Fechado bool
	// Fase é o que FaseGameflow devolve ("None" se vazio).
	Fase string
	// PassoDownload é quanto o progresso anda a cada consulta do metadata.
	PassoDownload float64
	replays       map[int64]*MetadataReplay
	// Chamadas registra as ações (create/download/watch) para os testes conferirem.
	Chamadas []string
}

// VersaoFalsa é a versão do jogo "instalado" no cliente falso.
const VersaoFalsa = "16.22.834.8910"

// Jogadores do cliente falso.
var (
	FalsoLogado = Invocador{Puuid: "puuid-teste-0001", Nome: "Jogador Teste", Tag: "br1"}
	FalsoRival  = Invocador{Puuid: "puuid-teste-0002", Nome: "Rival", Tag: "br1"}
)

const partidasPorJogador = 25

// NovoFalso cria o cliente falso no estado inicial.
func NovoFalso() *Falso {
	f := &Falso{PassoDownload: 25, replays: map[int64]*MetadataReplay{}}
	// A segunda partida do logado já foi baixada antes (estado "watch").
	f.replays[idFalso(0, 1)] = &MetadataReplay{Estado: "watch", Progresso: 100}
	return f
}

func (f *Falso) confere() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Fechado {
		return ErrClienteFechado
	}
	return nil
}

func donoFalso(puuid string) (int, bool) {
	switch puuid {
	case FalsoLogado.Puuid, "current-summoner":
		return 0, true
	case FalsoRival.Puuid:
		return 1, true
	}
	return 0, false
}

// idFalso: o gameId da i-ésima partida (0 = mais nova) do dono.
func idFalso(dono, i int) int64 {
	return 3304790000 - int64(dono)*100000 - int64(i)*1000
}

// deIDFalso faz o caminho inverso; ok=false para id que não é do falso.
func deIDFalso(id int64) (dono, i int, ok bool) {
	for d := 0; d < 2; d++ {
		delta := idFalso(d, 0) - id
		if delta >= 0 && delta%1000 == 0 && delta/1000 < partidasPorJogador {
			return d, int(delta / 1000), true
		}
	}
	return 0, 0, false
}

func versaoFalsa(i int) string {
	switch {
	case i < 12:
		return VersaoFalsa
	case i < 14:
		return "16.21.829.8620"
	}
	return "16.10.776.5552"
}

// CampeoesFalsos é o mapa id→nome que o modo teste usa no lugar do Data Dragon.
var CampeoesFalsos = map[int]string{
	54: "Malphite", 11: "Master Yi", 166: "Akshan", 22: "Ashe", 147: "Seraphine",
	17: "Teemo", 19: "Warwick", 7: "LeBlanc", 901: "Smolder", 99: "Lux",
	157: "Yasuo", 777: "Yone", 64: "Lee Sin", 412: "Thresh", 51: "Caitlyn",
}

var ordemCampeoes = []int{54, 11, 166, 22, 147, 17, 19, 7, 901, 99, 157, 777, 64, 412, 51}

// partidaFalsa monta a partida inteira no formato do LCU.
func partidaFalsa(dono, i int) map[string]any {
	id := idFalso(dono, i)
	donoInv := FalsoLogado
	if dono == 1 {
		donoInv = FalsoRival
	}
	slot := i%10 + 1
	vencedor := 100
	if i%3 == 0 {
		vencedor = 200
	}
	fila, modo, mapa := 420, "CLASSIC", 11
	if i%5 == 4 {
		fila, modo, mapa = 450, "ARAM", 12
	}
	duracao := 1500 + (i*137)%1000
	criacao := int64(1794800000000) - int64(i)*43200000 - int64(dono)*3600000
	var participantes, identidades []any
	for k := 1; k <= 10; k++ {
		time := 100
		if k > 5 {
			time = 200
		}
		campeao := ordemCampeoes[(i+k)%len(ordemCampeoes)]
		if k == slot && dono == 0 && i < 3 {
			campeao = 166 // o logado joga de Akshan nas três mais novas
		}
		cs := 20 + (i*7+k*13)%180
		participantes = append(participantes, map[string]any{
			"participantId": k, "teamId": time, "championId": campeao, "spell1Id": 4, "spell2Id": 14,
			"timeline": map[string]any{"participantId": k, "lane": "NONE", "role": "NONE"},
			"stats": map[string]any{
				"participantId": k, "win": time == vencedor,
				"kills": (i + k*3) % 12, "deaths": (i*2 + k) % 9, "assists": (i + k*5) % 15,
				"totalMinionsKilled": cs, "neutralMinionsKilled": (k * 11) % 40,
				"goldEarned": 7000 + cs*40 + k*100, "champLevel": 10 + (i+k)%8,
				"totalDamageDealtToChampions": 9000 + (i*311+k*577)%20000,
				"totalDamageTaken":            12000 + (i*97+k*431)%15000,
				"visionScore":                 5 + (i+k*7)%40, "wardsPlaced": 3 + (i+k)%12, "wardsKilled": (i + k) % 5,
				"item0": 3031, "item1": 3006, "item2": 0, "item3": 0, "item4": 0, "item5": 0, "item6": 3340,
				"largestMultiKill": 1 + (i+k)%3, "firstBloodKill": i == 0 && k == 1,
			},
		})
		jog := map[string]any{"gameName": fmt.Sprintf("Jogador %d", k), "tagLine": "br1",
			"puuid": fmt.Sprintf("puuid-falso-%d-%d", id, k), "platformId": "BR1"}
		if k == slot {
			jog["gameName"], jog["tagLine"], jog["puuid"] = donoInv.Nome, donoInv.Tag, donoInv.Puuid
		}
		identidades = append(identidades, map[string]any{"participantId": k, "player": jog})
	}
	vit := func(t int) string {
		if t == vencedor {
			return "Win"
		}
		return "Fail"
	}
	return map[string]any{
		"gameId": id, "platformId": "BR1", "gameCreation": criacao, "gameDuration": duracao,
		"queueId": fila, "mapId": mapa, "seasonId": 0, "gameVersion": versaoFalsa(i),
		"gameMode": modo, "gameType": "MATCHED_GAME", "endOfGameResult": "GameComplete",
		"teams":        []any{map[string]any{"teamId": 100, "win": vit(100)}, map[string]any{"teamId": 200, "win": vit(200)}},
		"participants": participantes, "participantIdentities": identidades,
	}
}

func (f *Falso) InvocadorAtual(ctx context.Context) (Invocador, error) {
	if err := f.confere(); err != nil {
		return Invocador{}, err
	}
	return FalsoLogado, nil
}

func (f *Falso) BuscaAlias(ctx context.Context, nome, tag string) (Invocador, error) {
	if err := f.confere(); err != nil {
		return Invocador{}, err
	}
	for _, inv := range []Invocador{FalsoLogado, FalsoRival} {
		if strings.EqualFold(inv.Nome, nome) && strings.EqualFold(inv.Tag, tag) {
			return inv, nil
		}
	}
	return Invocador{}, ErrNaoEncontrado
}

func (f *Falso) Historico(ctx context.Context, puuid string, inicio, fim int) (json.RawMessage, error) {
	if err := f.confere(); err != nil {
		return nil, err
	}
	dono, ok := donoFalso(puuid)
	if !ok {
		return json.RawMessage(`{"games":{"gameCount":0,"gameIndexBegin":0,"gameIndexEnd":0,"games":[]},"platformId":"BR1"}`), nil
	}
	var jogos []any
	for i := 0; i < partidasPorJogador; i++ {
		p := partidaFalsa(dono, i)
		// Como no LCU de verdade: no histórico só vem o participante dono.
		slot := i % 10
		p["participants"] = []any{p["participants"].([]any)[slot]}
		p["participantIdentities"] = []any{p["participantIdentities"].([]any)[slot]}
		delete(p, "teams")
		jogos = append(jogos, p)
	}
	return json.Marshal(map[string]any{"platformId": "BR1", "games": map[string]any{
		"gameCount": len(jogos), "gameIndexBegin": 0, "gameIndexEnd": len(jogos), "games": jogos}})
}

func (f *Falso) Partida(ctx context.Context, gameID int64) (json.RawMessage, error) {
	if err := f.confere(); err != nil {
		return nil, err
	}
	dono, i, ok := deIDFalso(gameID)
	if !ok {
		return nil, ErrNaoEncontrado
	}
	return json.Marshal(partidaFalsa(dono, i))
}

func (f *Falso) LinhaDoTempo(ctx context.Context, gameID int64) (json.RawMessage, error) {
	if err := f.confere(); err != nil {
		return nil, err
	}
	dono, i, ok := deIDFalso(gameID)
	if !ok {
		return nil, ErrNaoEncontrado
	}
	duracao := partidaFalsa(dono, i)["gameDuration"].(int)
	var quadros []any
	for m := 0; m <= duracao/60+1; m++ {
		pf := map[string]any{}
		for k := 1; k <= 10; k++ {
			pf[fmt.Sprint(k)] = map[string]any{"participantId": k, "level": 1 + m/3, "currentGold": 500 + m*10,
				"totalGold": 500 + m*400, "xp": m * 600, "minionsKilled": m * 7, "jungleMinionsKilled": 0,
				"position": map[string]any{"x": 500 + (k*1300+m*97)%14000, "y": 500 + (k*700+m*131)%14000}}
		}
		eventos := []any{}
		if m > 0 && m%4 == 0 {
			eventos = append(eventos, map[string]any{"type": "CHAMPION_KILL", "timestamp": m*60000 - 1500,
				"killerId": m%10 + 1, "victimId": (m+5)%10 + 1, "assistingParticipantIds": []int{},
				"position": map[string]any{"x": 7000, "y": 7000}})
		}
		quadros = append(quadros, map[string]any{"timestamp": m * 60000, "participantFrames": pf, "events": eventos})
	}
	return json.Marshal(map[string]any{"frames": quadros})
}

func (f *Falso) FaseGameflow(ctx context.Context) (string, error) {
	if err := f.confere(); err != nil {
		return "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Fase == "" {
		return "None", nil
	}
	return f.Fase, nil
}

func (f *Falso) Regiao(ctx context.Context) (Regiao, error) {
	if err := f.confere(); err != nil {
		return Regiao{}, err
	}
	return Regiao{Regiao: "BR", Locale: "pt_BR"}, nil
}

func (f *Falso) ConfiguracaoReplays(ctx context.Context) (ConfigReplays, error) {
	if err := f.confere(); err != nil {
		return ConfigReplays{}, err
	}
	return ConfigReplays{VersaoJogo: VersaoFalsa, Habilitado: true}, nil
}

func (f *Falso) PastaReplays(ctx context.Context) (string, error) {
	if err := f.confere(); err != nil {
		return "", err
	}
	return "C:/LoL Tracking Falso/Replays", nil
}

func (f *Falso) MetadataReplay(ctx context.Context, gameID int64) (MetadataReplay, error) {
	if err := f.confere(); err != nil {
		return MetadataReplay{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.replays[gameID]
	if !ok {
		return MetadataReplay{}, fmt.Errorf("%w (Plugin found no local metadata)", ErrNaoEncontrado)
	}
	// Cada consulta durante o download anda um passo: o poll do servidor vê o
	// progresso subir até "watch".
	if m.Estado == "downloading" {
		m.Progresso += f.PassoDownload
		if m.Progresso >= 100 {
			m.Progresso, m.Estado = 100, "watch"
		}
	}
	return *m, nil
}

func (f *Falso) CriaMetadataReplay(ctx context.Context, gameID int64, c CriaMetadata) error {
	if err := f.confere(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Chamadas = append(f.Chamadas, fmt.Sprintf("create %d", gameID))
	if _, _, ok := deIDFalso(gameID); !ok {
		return &ErroLCU{Status: 400, Mensagem: "partida desconhecida"}
	}
	if _, ok := f.replays[gameID]; ok {
		return nil
	}
	estado := "download"
	if PatchDe(c.VersaoJogo) != PatchDe(VersaoFalsa) {
		estado = "incompatible"
	}
	f.replays[gameID] = &MetadataReplay{Estado: estado}
	return nil
}

func (f *Falso) BaixaReplay(ctx context.Context, gameID int64) error {
	if err := f.confere(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Chamadas = append(f.Chamadas, fmt.Sprintf("download %d", gameID))
	m, ok := f.replays[gameID]
	if !ok {
		return fmt.Errorf("%w (Plugin found no local metadata)", ErrNaoEncontrado)
	}
	if m.Estado != "download" {
		return &ErroLCU{Status: 400, Mensagem: "replay não está disponível para download"}
	}
	m.Estado, m.Progresso = "downloading", 0
	return nil
}

func (f *Falso) AssisteReplay(ctx context.Context, gameID int64) error {
	if err := f.confere(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Chamadas = append(f.Chamadas, fmt.Sprintf("watch %d", gameID))
	return nil
}

// PatchDe tira "16.22" de "16.22.834.8910": replay só abre no mesmo patch.
func PatchDe(versao string) string {
	p := strings.SplitN(versao, ".", 3)
	if len(p) < 2 {
		return versao
	}
	return p[0] + "." + p[1]
}
