package ia

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"loltracking/server/analise"
	"loltracking/server/partida"
	"loltracking/server/rastreio"
)

// Fontes é de onde a IA tira o texto que manda: tudo do banco e dos arquivos
// da partida, pelos pacotes donos (analise, rastreio). Fica atrás de
// interface para os testes da IA não precisarem de uma partida rastreada.
type Fontes interface {
	// Partida: o essencial da base de fatos (participantes, duração, elo).
	Partida(ctx context.Context, gameID int64) (InfoPartida, error)
	// ContextoPartida: o pacote de contexto (Markdown) com um jogador em foco.
	ContextoPartida(ctx context.Context, gameID int64, pid int) (string, error)
	// Visao: as sentinelas e, se houver passe de visão de time, quando o
	// time do jogador em foco via quem o matou.
	Visao(ctx context.Context, gameID int64, pid int) (string, error)
	// Momento: os 10 num instante (o protocolo de pedido de dado da conversa).
	Momento(ctx context.Context, gameID int64, tMs int64) (string, error)
	// Fichas: a ficha de fatos de cada instante (os 10, onda, visão,
	// sentinelas e recargas do jogador em foco), para as situações.
	Fichas(ctx context.Context, gameID int64, pid int, instantes []int64) ([]string, error)
	// OutrasDoJogador: momentos do mesmo tipo/etiqueta em OUTRAS partidas do
	// jogador (só o banco), para juntar padrões.
	OutrasDoJogador(puuid, tipo, tag string, excluiGame int64) ([]analise.LinhaBusca, error)
	// VariasPartidas: uma linha por partida que casa (só o banco) e os ids.
	VariasPartidas(f analise.FiltroResumos) (string, []int64, error)
	// Campeoes: os campeões que existem na base de fatos (para ler a pergunta
	// da conversa geral).
	Campeoes() ([]string, error)
	// Indicadores: o jogador numa partida em números comparáveis (só o banco).
	Indicadores(gameID int64, pid int) (analise.IndicadoresJogador, error)
	// Referencias: os jogadores escolhidos pela coleta por elo que casam (só o
	// banco), para as vozes por dado.
	Referencias(f analise.FiltroReferencia) ([]analise.IndicadoresJogador, error)
}

// InfoPartida é o que a IA precisa saber da partida fora do texto.
type InfoPartida struct {
	GameID        int64
	DuracaoS      int
	Elo           string
	Patch         string
	Participantes []Participante
	// Momentos: todos os momentos da base de fatos (as situações saem deles).
	Momentos []analise.MomentoChave
}

// Participante: id, campeão e lado (para validar e desenhar os quadros).
type Participante struct {
	ID      int    `json:"participante_id"`
	Campeao string `json:"campeao"`
	Nome    string `json:"nome"`
	Funcao  string `json:"funcao"`
	Time    int    `json:"time"`
	Puuid   string `json:"-"`
	// Tag: a do Riot ID (nome#tag), para a consulta do elo na Riot.
	Tag string `json:"-"`
}

// Do devolve o participante (ok falso se não existe).
func (p InfoPartida) Do(id int) (Participante, bool) {
	for _, x := range p.Participantes {
		if x.ID == id {
			return x, true
		}
	}
	return Participante{}, false
}

// FontesBanco é a implementação de verdade.
type FontesBanco struct{ D analise.Deps }

func (f FontesBanco) Partida(ctx context.Context, gameID int64) (InfoPartida, error) {
	if err := analise.GaranteFatos(ctx, f.D, gameID); err != nil {
		return InfoPartida{}, err
	}
	r, err := analise.LeFatos(f.D.DB, gameID)
	if err != nil {
		return InfoPartida{}, err
	}
	info := InfoPartida{GameID: gameID, DuracaoS: r.DuracaoS, Elo: r.Elo, Patch: r.Patch, Momentos: r.Momentos}
	for _, j := range r.Jogadores {
		info.Participantes = append(info.Participantes, Participante{ID: j.ParticipanteID, Campeao: j.Campeao, Nome: j.Nome, Funcao: j.Funcao, Time: j.Time, Puuid: j.Puuid})
	}
	// A tag do Riot ID não está na base de fatos: vem da partida (banco).
	if pt, err := partida.Le(f.D.DB, gameID); err == nil {
		tags := map[int]string{}
		for _, p := range pt.Participantes {
			tags[p.ParticipanteID] = p.Tag
		}
		for i := range info.Participantes {
			info.Participantes[i].Tag = tags[info.Participantes[i].ID]
		}
	}
	sort.Slice(info.Participantes, func(a, b int) bool { return info.Participantes[a].ID < info.Participantes[b].ID })
	return info, nil
}

func (f FontesBanco) ContextoPartida(ctx context.Context, gameID int64, pid int) (string, error) {
	if err := analise.GaranteFatos(ctx, f.D, gameID); err != nil {
		return "", err
	}
	c, err := analise.ContextoDaPartida(f.D.DB, gameID, pid)
	return c.Texto, err
}

// Visao monta a seção "Visão" do pacote da análise.
func (f FontesBanco) Visao(ctx context.Context, gameID int64, pid int) (string, error) {
	r, err := analise.LeFatos(f.D.DB, gameID)
	if err != nil {
		return "", err
	}
	nomes := map[int]string{}
	timeDe := map[int]int{}
	for _, j := range r.Jogadores {
		nomes[j.ParticipanteID] = j.Campeao
		timeDe[j.ParticipanteID] = j.Time
	}
	var b strings.Builder
	b.WriteString("## Visão\n")
	s, err := analise.LeSentinelas(f.D.DB, gameID)
	switch {
	case err != nil:
		b.WriteString("- Sentinelas: não deu para ler (" + err.Error() + ").\n")
	case !s.Riot:
		b.WriteString("- Sentinelas: sem a linha do tempo da Riot nesta partida (" + s.Motivo + ").\n")
	default:
		b.WriteString("- Sentinelas (linha do tempo da Riot; posição ESTIMADA pelo lugar de quem colocou):\n")
		for _, t := range s.Times {
			fmt.Fprintf(&b, "  - time %s colocou %d (%d de controle), destruiu %d.\n", analise.NomeTime(t.Time), t.Colocadas, t.DeControle, t.Destruidas)
		}
		for _, j := range s.Jogadores {
			if j.ParticipanteID != pid {
				continue
			}
			fmt.Fprintf(&b, "  - %s colocou %d (rotas %d, meio %d, final %d), %d de controle, destruiu %d", nomes[pid], j.Colocadas,
				j.PorFase["rotas"], j.PorFase["meio"], j.PorFase["final"], j.DeControle, j.Destruidas)
			if j.MetadeInimiga != nil {
				fmt.Fprintf(&b, ", %d na metade inimiga", *j.MetadeInimiga)
			}
			b.WriteString(".\n")
		}
		n := 0
		for _, w := range s.Sentinelas {
			// A mesma regra da contagem (analise.ContaComoSentinela): o UNDEFINED não entra.
			if w.ParticipanteID != pid || n >= 20 || !analise.ContaComoSentinela(w.Tipo) {
				continue
			}
			n++
			lugar := "sem lugar (não rastreado no instante)"
			if w.Zona != "" {
				lugar = "em " + nomeZona(w.Zona)
			}
			fmt.Fprintf(&b, "  - [%s] %s colocou %s %s; %s.\n", analise.MinSeg(w.TMs), nomes[pid], strings.ToLower(w.TipoNome), lugar, w.Vida)
		}
	}
	v, ok, err := rastreio.LeVisao(f.D.DB, gameID)
	if err != nil || !ok || v == nil || len(v.Times) == 0 {
		b.WriteString("- Visão de time (o que cada time via no minimapa): esta partida NÃO tem o passe de visão de time; não dá para dizer quem estava à vista.\n")
		return b.String(), nil
	}
	meuTime := timeDe[pid]
	var dele *rastreio.VisaoDoTime
	for i := range v.Times {
		if v.Times[i].Time == meuTime {
			dele = &v.Times[i]
		}
	}
	if dele == nil {
		fmt.Fprintf(&b, "- Visão de time: há passe de visão, mas não do time %s.\n", analise.NomeTime(meuTime))
		return b.String(), nil
	}
	fmt.Fprintf(&b, "- Visão do time %s (passe de visão de time, lido do minimapa de %s a %s", analise.NomeTime(meuTime), analise.MinSeg(dele.DeMs), analise.MinSeg(dele.AteMs))
	if dele.Aviso != "" {
		b.WriteString("; AVISO: " + dele.Aviso)
	}
	b.WriteString("). Quanto do tempo cada inimigo esteve à vista do time:\n")
	for _, j := range dele.Jogadores {
		pct := "—"
		if j.PctVisivel != nil {
			pct = fmt.Sprintf("%.0f%%", *j.PctVisivel)
		}
		fmt.Fprintf(&b, "  - %s: %s do tempo decidido.\n", nomes[j.ParticipanteID], pct)
	}
	fmt.Fprintf(&b, "  - Leitura: %.0f%% do tempo dos inimigos ficou sem dar para saber (retrato coberto no minimapa); %.0f%% foi herdado por continuidade (trecho curto entre duas leituras iguais).\n",
		dele.PctIndeterminado, dele.PctInferido)
	// Nos abates de que ele participou: o atacante era visto pelo time da
	// vítima nos 15 s antes (sim desde quando / não / não dá para saber),
	// e antes de chegar à vítima: o que a base de fatos guardou no momento
	// (analise.LinhasVisaoDoAbate).
	for _, m := range r.Momentos {
		if m.Tipo != "abate" || !contemInt(m.Envolvidos, pid) {
			continue
		}
		ls := analise.LinhasVisaoDoAbate(m, nomes)
		if len(ls) == 0 {
			continue
		}
		quem := "Morte de " + nomes[m.ParticipanteID]
		if m.ParticipanteID != pid {
			quem = "Abate em " + nomes[m.ParticipanteID]
		}
		fmt.Fprintf(&b, "  - %s em [%s], visão nos %d s antes: %s.\n", quem, analise.MinSeg(m.TMs), analise.Param.VisaoAntesMs/1000, strings.Join(ls, "; "))
	}
	return b.String(), nil
}

// sobreposicao: quantos ms dos intervalos caem em [de, ate).
func sobreposicao(lista [][2]int64, de, ate int64) int64 {
	var n int64
	for _, iv := range lista {
		a, z := max(iv[0], de), min(iv[1], ate)
		if z > a {
			n += z - a
		}
	}
	return n
}

var nomesZona = map[string]string{"base_azul": "base azul", "base_vermelha": "base vermelha", "rota_topo": "rota do topo",
	"rota_meio": "rota do meio", "rota_baixo": "rota de baixo", "rio_cima": "rio de cima", "rio_baixo": "rio de baixo",
	"cova_barao": "cova do Barão", "cova_dragao": "cova do Dragão", "selva_azul_cima": "selva azul de cima",
	"selva_azul_baixo": "selva azul de baixo", "selva_vermelha_cima": "selva vermelha de cima",
	"selva_vermelha_baixo": "selva vermelha de baixo", "fora_do_mapa": "fora do mapa"}

// CodigosZona: o vocabulário de zonas dos quadros (na ordem).
var CodigosZona = []string{"base_azul", "base_vermelha", "rota_topo", "rota_meio", "rota_baixo", "rio_cima", "rio_baixo",
	"cova_barao", "cova_dragao", "selva_azul_cima", "selva_azul_baixo", "selva_vermelha_cima", "selva_vermelha_baixo"}

func nomeZona(c string) string {
	if n, ok := nomesZona[c]; ok {
		return n
	}
	return strings.ReplaceAll(c, "_", " ")
}

// Momento: os 10 no instante, em texto.
func (f FontesBanco) Momento(ctx context.Context, gameID int64, tMs int64) (string, error) {
	r, err := analise.LeFatos(f.D.DB, gameID)
	if err != nil {
		return "", err
	}
	if tMs < 0 || (r.DuracaoS > 0 && tMs > int64(r.DuracaoS)*1000) {
		return "", fmt.Errorf("o instante %s está fora da partida (0:00 a %s)", analise.MinSeg(tMs), analise.MinSeg(int64(r.DuracaoS)*1000))
	}
	qs, err := rastreio.ListaEstados(f.D.DB, gameID, tMs, tMs, 1000)
	if err != nil {
		return "", err
	}
	if len(qs) == 0 {
		return "", fmt.Errorf("sem estado no instante %s", analise.MinSeg(tMs))
	}
	atv, _ := analise.ListaAtividades(f.D.DB, gameID, 0, tMs, tMs)
	porPid := map[int]analise.TrechoAtividade{}
	for _, a := range atv {
		if a.TMs <= tMs && tMs < a.FimMs {
			porPid[a.ParticipanteID] = a
		}
	}
	jog := map[int]analise.ResumoJogador{}
	itens := map[int]string{}
	for _, j := range r.Jogadores {
		jog[j.ParticipanteID] = j
		for _, it := range j.Itens {
			if it.Nome != "" {
				itens[it.ItemID] = it.Nome
			}
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Os 10 no instante %s (posição: rastreada pelo app, ou interpolada da linha do tempo por minuto quando dito):\n", analise.MinSeg(tMs))
	for _, j := range qs[0].Jogadores {
		rj := jog[j.ParticipanteID]
		fmt.Fprintf(&b, "- %s (participante %d, time %s, %s): ", rj.Campeao, j.ParticipanteID, analise.NomeTime(rj.Time), analise.NomeFuncao(rj.Funcao))
		if j.Morto {
			fmt.Fprintf(&b, "MORTO (renasce em %.0f s); ", j.Respawn)
		} else if j.X != nil && j.Y != nil {
			z, _ := analise.Zona(*j.X, *j.Y)
			origem := "rastreada"
			if j.Origem == "linha_do_tempo" {
				origem = "aproximada pela linha do tempo por minuto"
			} else if j.Origem == "interpolada" {
				origem = "estimada entre duas leituras"
			}
			fmt.Fprintf(&b, "em %s (x %.0f, y %.0f; %s); ", nomeZona(z), *j.X, *j.Y, origem)
		}
		if j.VidaPct != nil {
			fmt.Fprintf(&b, "vida %.0f%%", *j.VidaPct)
			if j.Vida != nil && j.VidaMax != nil {
				fmt.Fprintf(&b, " (%.0f/%.0f)", *j.Vida, *j.VidaMax)
			}
			b.WriteString("; ")
		}
		if j.ManaPct != nil {
			fmt.Fprintf(&b, "mana %.0f%%; ", *j.ManaPct)
		}
		fmt.Fprintf(&b, "nível %d; %d/%d/%d; CS %d", j.Nivel, j.Abates, j.Mortes, j.Assistencias, j.CS)
		if a, ok := porPid[j.ParticipanteID]; ok {
			fmt.Fprintf(&b, "; atividade: %s", strings.ReplaceAll(a.Atividade, "_", " "))
			if a.Alvo != "" {
				b.WriteString(" (" + a.Alvo + ")")
			}
			if len(a.Com) > 0 {
				var com []string
				for _, c := range a.Com {
					com = append(com, jog[c].Campeao)
				}
				b.WriteString(" com " + strings.Join(com, ", "))
			}
		}
		var its []string
		for _, s := range j.Itens {
			if s.ID == 0 {
				continue
			}
			if n, ok := itens[s.ID]; ok {
				its = append(its, n)
			} else {
				its = append(its, fmt.Sprintf("item %d", s.ID))
			}
		}
		if len(its) > 0 {
			b.WriteString("; itens: " + strings.Join(its, ", "))
		}
		b.WriteString(".\n")
	}
	return b.String(), nil
}

func (f FontesBanco) VariasPartidas(fl analise.FiltroResumos) (string, []int64, error) {
	c, err := analise.ContextoDeVariasPartidas(f.D.DB, fl)
	if err != nil {
		return "", nil, err
	}
	ls, err := analise.BuscaResumos(f.D.DB, fl)
	if err != nil {
		return "", nil, err
	}
	var ids []int64
	visto := map[int64]bool{}
	for _, l := range ls {
		if !visto[l.GameID] {
			visto[l.GameID] = true
			ids = append(ids, l.GameID)
		}
	}
	return c.Texto, ids, nil
}

func (f FontesBanco) Indicadores(gameID int64, pid int) (analise.IndicadoresJogador, error) {
	return analise.IndicadoresDe(f.D.DB, gameID, pid)
}

func (f FontesBanco) Referencias(fl analise.FiltroReferencia) ([]analise.IndicadoresJogador, error) {
	return analise.Referencias(f.D.DB, fl)
}

func (f FontesBanco) Campeoes() ([]string, error) {
	linhas, err := f.D.DB.Query(`SELECT DISTINCT campeao FROM resumos_jogador WHERE campeao <> '' ORDER BY LENGTH(campeao) DESC`)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	var r []string
	for linhas.Next() {
		var c string
		if err := linhas.Scan(&c); err != nil {
			return nil, err
		}
		r = append(r, c)
	}
	return r, linhas.Err()
}

// msDeTexto: "8:48" → 528000 (ok falso se não é um instante).
func msDeTexto(s string) (int64, bool) {
	var m, sg int
	s = strings.TrimSpace(s)
	if n, err := fmt.Sscanf(s, "%d:%d", &m, &sg); err != nil || n != 2 || m < 0 || sg < 0 || sg > 59 || m > 120 {
		return 0, false
	}
	return int64(m)*60000 + int64(sg)*1000, true
}

// OutrasDoJogador: a busca da base de fatos com o puuid.
func (f FontesBanco) OutrasDoJogador(puuid, tipo, tag string, excluiGame int64) ([]analise.LinhaBusca, error) {
	if puuid == "" {
		return nil, nil
	}
	return analise.BuscaResumos(f.D.DB, analise.FiltroResumos{Puuid: puuid, Tipo: tipo, Tag: tag, ExcluiGame: excluiGame, Limite: 20})
}

// Fichas monta a ficha de cada instante. Ondas, sentinelas e visão são lidas
// uma vez para todas.
func (f FontesBanco) Fichas(ctx context.Context, gameID int64, pid int, instantes []int64) ([]string, error) {
	r, err := analise.LeFatos(f.D.DB, gameID)
	if err != nil {
		return nil, err
	}
	nomes := map[int]string{}
	timeDe := map[int]int{}
	for _, j := range r.Jogadores {
		nomes[j.ParticipanteID] = j.Campeao
		timeDe[j.ParticipanteID] = j.Time
	}
	ondas, errOndas := analise.LeOndas(f.D.DB, gameID)
	sent, errSent := analise.LeSentinelas(f.D.DB, gameID)
	visao, temVisao, _ := rastreio.LeVisao(f.D.DB, gameID)
	var fichas []string
	for _, t := range instantes {
		var b strings.Builder
		m, err := f.Momento(ctx, gameID, t)
		if err != nil {
			fmt.Fprintf(&b, "- Estado dos 10 em %s: não disponível (%v).\n", analise.MinSeg(t), err)
		} else {
			b.WriteString(m)
		}
		// Recargas do jogador em foco (só quando ele era o seguido no passe).
		if qs, err := rastreio.ListaEstados(f.D.DB, gameID, t, t, 1000); err == nil && len(qs) > 0 {
			for _, j := range qs[0].Jogadores {
				if j.ParticipanteID != pid {
					continue
				}
				var cd []string
				for _, x := range []struct {
					n string
					v *float64
				}{{"Q", j.CD.Q}, {"W", j.CD.W}, {"E", j.CD.E}, {"R", j.CD.R}, {"feitiço D", j.CD.D}, {"feitiço F", j.CD.F}} {
					if x.v != nil {
						if *x.v <= 0 {
							cd = append(cd, x.n+" pronta")
						} else {
							cd = append(cd, fmt.Sprintf("%s em %.0f s", x.n, *x.v))
						}
					}
				}
				if len(cd) > 0 {
					fmt.Fprintf(&b, "- Recargas de %s (lidas da tela): %s.\n", nomes[pid], strings.Join(cd, ", "))
				} else {
					fmt.Fprintf(&b, "- Recargas de %s: não lidas neste instante.\n", nomes[pid])
				}
			}
		}
		if errOndas == nil {
			var ls []string
			for _, ro := range ondas.Rotas {
				for _, e := range ro.Estados {
					if e.DeMs <= t && t < e.AteMs {
						quem := ""
						if e.Time != 0 {
							quem = " (time " + analise.NomeTime(e.Time) + ")"
						}
						ls = append(ls, fmt.Sprintf("%s: %s%s, confiança %s", ro.Rota, e.Estado, quem, e.Confianca))
					}
				}
			}
			if len(ls) > 0 {
				b.WriteString("- Ondas (deduzidas): " + strings.Join(ls, "; ") + ".\n")
			}
		}
		if errSent == nil && sent.Riot {
			cont := map[int]int{}
			var dele []string
			for _, w := range sent.Sentinelas {
				if w.TMs <= t && (w.AteMs == nil || *w.AteMs > t) {
					cont[w.Time]++
					if w.Zona != "" && len(dele) < 8 {
						dele = append(dele, fmt.Sprintf("%s de %s em %s", strings.ToLower(w.TipoNome), nomes[w.ParticipanteID], nomeZona(w.Zona)))
					}
				}
			}
			fmt.Fprintf(&b, "- Sentinelas no chão (estimado): azul %d, vermelho %d", cont[100], cont[200])
			if len(dele) > 0 {
				b.WriteString("; com lugar estimado: " + strings.Join(dele, "; "))
			}
			b.WriteString(".\n")
		}
		if temVisao && visao != nil {
			for _, vt := range visao.Times {
				if vt.Time != timeDe[pid] || t < vt.DeMs || t > vt.AteMs {
					continue
				}
				var ls []string
				for _, j := range vt.Jogadores {
					est := "sem leitura"
					switch j.EstadoEm(t) {
					case rastreio.VisVisivel:
						est = "à vista"
					case rastreio.VisFora:
						est = "fora da visão"
					case rastreio.VisIndeterminado:
						est = "visão incerta"
					case rastreio.VisForaDoMapa:
						est = "fora do mapa (intocável: Investida Alfa, teleporte)"
					}
					if j.InferidoEm(t) {
						est += " (herdado por continuidade, não lido)"
					}
					ls = append(ls, nomes[j.ParticipanteID]+" "+est)
				}
				fmt.Fprintf(&b, "- Visão do time %s (passe de visão de time): %s.\n", analise.NomeTime(vt.Time), strings.Join(ls, "; "))
			}
			// Abates perto do instante (até 5 s antes e 20 s depois): o atacante
			// era visto pelo time da vítima antes?
			for _, m := range r.Momentos {
				if m.Tipo != "abate" || m.TMs < t-5000 || m.TMs > t+20000 {
					continue
				}
				if ls := analise.LinhasVisaoDoAbate(m, nomes); len(ls) > 0 {
					fmt.Fprintf(&b, "- Visão antes do abate em %s [%s] (nos %d s antes): %s.\n", nomes[m.ParticipanteID], analise.MinSeg(m.TMs),
						analise.Param.VisaoAntesMs/1000, strings.Join(ls, "; "))
				}
			}
		} else {
			b.WriteString("- Visão de time: sem passe de visão nesta partida (não se sabe quem era visto).\n")
		}
		fichas = append(fichas, b.String())
	}
	return fichas, nil
}

func contemInt(l []int, v int) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}
