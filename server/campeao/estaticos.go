package campeao

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Dados estáticos do jogo NA VERSÃO DA PARTIDA: atributos de base e crescimento
// por nível de cada campeão e o que cada item dá. Servem para decompor os
// atributos lidos da tela (base + itens + objetivos + o resto) e para mostrar
// o que cada item agrega.
//
// Fontes (públicas, sem chave; nada disso toca no cliente do LoL):
//   - Data Dragon (ddragon.leagueoflegends.com/cdn/<versão>/data/pt_BR/
//     champion.json e item.json): nomes, custo, atributos e texto dos itens,
//     atributos de base dos campeões.
//   - CommunityDragon (raw.communitydragon.org/<patch>/game/data/characters/
//     <campeão>/<campeão>.bin.json): o registro do campeão tirado dos arquivos
//     do próprio jogo. Usado porque o Data Dragon vem, desde o 16.5, com o
//     crescimento de dano de ataque por nível ZERADO em todos os campeões
//     (conferido em 17/11/2026: o 16.4.1 tem 3 para a Leona, o 16.5.1 em diante
//     tem 0, e a tela mostra a Leona ganhando ~3 de DA por nível). O registro do
//     jogo também traz a razão da velocidade de ataque, que o Data Dragon não tem.
//
// Cache em disco em <dados>/estaticos/<versão do Data Dragon>/ (já reduzido ao
// que usamos): baixa uma vez por patch e funciona offline depois.

// AtributosCampeao é a base no nível 1 e o crescimento por nível.
type AtributosCampeao struct {
	Chave          string  `json:"chave"`
	Nome           string  `json:"nome"`
	AD             float64 `json:"ad"`
	ADNivel        float64 `json:"ad_nivel"`
	Armadura       float64 `json:"armadura"`
	ArmaduraNivel  float64 `json:"armadura_nivel"`
	RM             float64 `json:"rm"`
	RMNivel        float64 `json:"rm_nivel"`
	VelAtaque      float64 `json:"vel_ataque"`
	VelAtaqueRazao float64 `json:"vel_ataque_razao"`
	// VelAtaqueNivel em pontos percentuais de velocidade de ataque bônus por nível.
	VelAtaqueNivel float64 `json:"vel_ataque_nivel"`
	VelMov         float64 `json:"vel_mov"`
	// Fonte: "jogo" (CommunityDragon, arquivos do jogo) ou "data_dragon".
	Fonte string `json:"fonte"`
}

// AtributosItem é o que o item dá (só os atributos que a tela lê, mais a vida
// e a mana, que também aparecem no texto). Percentuais em fração (0,4 = 40%).
type AtributosItem struct {
	AD           float64 `json:"ad,omitempty"`
	AP           float64 `json:"ap,omitempty"`
	Armadura     float64 `json:"armadura,omitempty"`
	RM           float64 `json:"rm,omitempty"`
	VelAtaquePct float64 `json:"vel_ataque_pct,omitempty"`
	VelMov       float64 `json:"vel_mov,omitempty"`
	VelMovPct    float64 `json:"vel_mov_pct,omitempty"`
	Vida         float64 `json:"vida,omitempty"`
	Mana         float64 `json:"mana,omitempty"`
}

// Item é um item na versão da partida.
type Item struct {
	Nome  string `json:"nome"`
	Custo int    `json:"custo"`
	// Atributos: o que entra na conta dos atributos lidos da tela.
	Atributos AtributosItem `json:"atributos"`
	// Linhas: os atributos como o jogo escreve ("55 de Dano de Ataque"),
	// inclusive os que a tela não lê (letalidade, aceleração...).
	Linhas []string `json:"linhas"`
	// Descricao: os efeitos (passivas, ativos), sem a marcação do jogo.
	Descricao string `json:"descricao"`
}

// Estaticos é o pacote de dados de uma versão.
type Estaticos struct {
	VersaoDados string                   `json:"versao_dados"`
	Campeoes    map[int]AtributosCampeao `json:"campeoes"`
	Itens       map[int]Item             `json:"itens"`
	Avisos      []string                 `json:"avisos"`
}

// FonteEstaticos é o que os outros pacotes usam. versaoJogo é o "16.22" (ou
// "16.22.834.8910") da partida; campeoes são os ids que interessam.
type FonteEstaticos interface {
	Estaticos(ctx context.Context, versaoJogo string, campeoes []int) (Estaticos, error)
}

// ErrSemEstaticos: sem rede na primeira vez e sem cache da versão.
var ErrSemEstaticos = errors.New("dados estáticos do jogo indisponíveis para esta versão (sem rede na primeira vez?)")

// EstaticosFixos é a fonte do modo teste e dos testes: nada sai para a rede.
type EstaticosFixos Estaticos

func (f EstaticosFixos) Estaticos(_ context.Context, _ string, _ []int) (Estaticos, error) {
	return Estaticos(f), nil
}

// BaseCommunityDragon é o endereço do CommunityDragon.
const BaseCommunityDragon = "https://raw.communitydragon.org"

// EstaticosRede baixa do Data Dragon e do CommunityDragon com cache em disco.
type EstaticosRede struct {
	baseDD, baseCD string
	dir            string
	http           *http.Client
	mu             sync.Mutex
	// memoria: o que já foi lido nesta execução, por versão do Data Dragon.
	memoria map[string]*Estaticos
	// falhou: quando a última ida à rede falhou, por versão do jogo (uma
	// tentativa a cada 10 min, como os nomes).
	falhou map[string]time.Time
}

// NovosEstaticos guarda o cache em <dirDados>/estaticos.
func NovosEstaticos(dirDados string) *EstaticosRede {
	return &EstaticosRede{
		baseDD: BaseDataDragon, baseCD: BaseCommunityDragon,
		dir:     filepath.Join(dirDados, "estaticos"),
		http:    &http.Client{Timeout: 20 * time.Second},
		memoria: map[string]*Estaticos{}, falhou: map[string]time.Time{},
	}
}

var rePatch = regexp.MustCompile(`^(\d+)\.(\d+)`)

// patchDe: "16.22.834.8910" → "16.22".
func patchDe(v string) string {
	m := rePatch.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return ""
	}
	return m[1] + "." + m[2]
}

func (e *EstaticosRede) Estaticos(ctx context.Context, versaoJogo string, campeoes []int) (Estaticos, error) {
	patch := patchDe(versaoJogo)
	if patch == "" {
		return Estaticos{}, fmt.Errorf("versão do jogo %q sem patch", versaoJogo)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	versao, err := e.versaoDados(ctx, patch)
	if err != nil {
		return Estaticos{}, err
	}
	est := e.memoria[versao]
	if est == nil {
		est, err = e.carrega(ctx, versao)
		if err != nil {
			e.falhou[patch] = time.Now()
			return Estaticos{}, err
		}
		e.memoria[versao] = est
	}
	// O registro do jogo de cada campeão pedido (um arquivo por campeão).
	for _, id := range campeoes {
		c, ok := est.Campeoes[id]
		if !ok || c.Fonte == "jogo" {
			continue
		}
		if novo, err := e.registroDoJogo(ctx, patch, versao, c); err == nil {
			est.Campeoes[id] = novo
		} else if !errors.Is(err, errJaTentou) {
			log.Printf("estáticos: registro do jogo de %s (%s): %v", c.Nome, patch, err)
		}
	}
	return copiaEstaticos(est, campeoes), nil
}

// copiaEstaticos: só os campeões pedidos (todos os itens) e os avisos que
// valem para eles.
func copiaEstaticos(est *Estaticos, campeoes []int) Estaticos {
	r := Estaticos{VersaoDados: est.VersaoDados, Campeoes: map[int]AtributosCampeao{}, Itens: est.Itens, Avisos: []string{}}
	semJogo := []string{}
	for _, id := range campeoes {
		if c, ok := est.Campeoes[id]; ok {
			r.Campeoes[id] = c
			if c.Fonte != "jogo" {
				semJogo = append(semJogo, c.Nome)
			}
		}
	}
	if len(semJogo) > 0 {
		sort.Strings(semJogo)
		r.Avisos = append(r.Avisos, "sem o registro do jogo para "+strings.Join(semJogo, ", ")+
			": base pelo Data Dragon, que vem com o crescimento de dano de ataque por nível zerado (o dano de ataque de base sai baixo nos níveis altos)")
	}
	r.Avisos = append(r.Avisos, est.Avisos...)
	return r
}

// versaoDados acha a versão do Data Dragon do patch ("16.22" → "16.22.1"):
// primeiro no cache em disco, depois na lista de versões.
func (e *EstaticosRede) versaoDados(ctx context.Context, patch string) (string, error) {
	if ents, err := os.ReadDir(e.dir); err == nil {
		for _, en := range ents {
			if en.IsDir() && patchDe(en.Name()) == patch && strings.HasPrefix(en.Name(), patch+".") {
				if _, err := os.Stat(filepath.Join(e.dir, en.Name(), "itens.json")); err == nil {
					return en.Name(), nil
				}
			}
		}
	}
	if t, ok := e.falhou[patch]; ok && time.Since(t) < 10*time.Minute {
		return "", ErrSemEstaticos
	}
	var versoes []string
	if err := e.pegaJSON(ctx, e.baseDD+"/api/versions.json", &versoes); err != nil {
		e.falhou[patch] = time.Now()
		return "", fmt.Errorf("%w: %v", ErrSemEstaticos, err)
	}
	for _, v := range versoes {
		if strings.HasPrefix(v, patch+".") {
			return v, nil
		}
	}
	e.falhou[patch] = time.Now()
	return "", fmt.Errorf("%w: o Data Dragon não tem o patch %s", ErrSemEstaticos, patch)
}

func (e *EstaticosRede) pegaJSON(ctx context.Context, url string, destino any) error {
	ctx, cancela := context.WithTimeout(ctx, 20*time.Second)
	defer cancela()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := e.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s respondeu %d", url, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(destino)
}

func leArquivoJSON(arq string, destino any) bool {
	b, err := os.ReadFile(arq)
	return err == nil && json.Unmarshal(b, destino) == nil
}

func gravaArquivoJSON(arq string, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(arq), 0o755); err != nil {
		log.Printf("estáticos: %v", err)
		return
	}
	// Temporário + renomear: um download cortado não vira cache quebrado.
	tmp := arq + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err == nil {
		os.Rename(tmp, arq)
	}
}

// carrega lê campeões e itens da versão do cache ou da rede. Chamado com e.mu.
func (e *EstaticosRede) carrega(ctx context.Context, versao string) (*Estaticos, error) {
	pasta := filepath.Join(e.dir, versao)
	est := &Estaticos{VersaoDados: versao, Campeoes: map[int]AtributosCampeao{}, Itens: map[int]Item{}}
	var camps map[string]AtributosCampeao
	var itens map[string]Item
	if !leArquivoJSON(filepath.Join(pasta, "campeoes.json"), &camps) {
		var r struct {
			Data map[string]struct {
				ID    string `json:"id"`
				Key   string `json:"key"`
				Name  string `json:"name"`
				Stats struct {
					MoveSpeed            float64 `json:"movespeed"`
					Armor                float64 `json:"armor"`
					ArmorPerLevel        float64 `json:"armorperlevel"`
					SpellBlock           float64 `json:"spellblock"`
					SpellBlockPerLevel   float64 `json:"spellblockperlevel"`
					AttackDamage         float64 `json:"attackdamage"`
					AttackDamagePerLevel float64 `json:"attackdamageperlevel"`
					AttackSpeedPerLevel  float64 `json:"attackspeedperlevel"`
					AttackSpeed          float64 `json:"attackspeed"`
				} `json:"stats"`
			} `json:"data"`
		}
		if err := e.pegaJSON(ctx, e.baseDD+"/cdn/"+versao+"/data/pt_BR/champion.json", &r); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrSemEstaticos, err)
		}
		camps = map[string]AtributosCampeao{}
		for k, c := range r.Data {
			chave := c.ID
			if chave == "" {
				chave = k
			}
			s := c.Stats
			camps[c.Key] = AtributosCampeao{Chave: chave, Nome: c.Name, AD: s.AttackDamage, ADNivel: s.AttackDamagePerLevel,
				Armadura: s.Armor, ArmaduraNivel: s.ArmorPerLevel, RM: s.SpellBlock, RMNivel: s.SpellBlockPerLevel,
				VelAtaque: s.AttackSpeed, VelAtaqueRazao: s.AttackSpeed, VelAtaqueNivel: s.AttackSpeedPerLevel,
				VelMov: s.MoveSpeed, Fonte: "data_dragon"}
		}
		gravaArquivoJSON(filepath.Join(pasta, "campeoes.json"), camps)
	}
	if !leArquivoJSON(filepath.Join(pasta, "itens.json"), &itens) {
		var r struct {
			Data map[string]struct {
				Name        string              `json:"name"`
				Description string              `json:"description"`
				Gold        struct{ Total int } `json:"gold"`
				Stats       map[string]float64  `json:"stats"`
			} `json:"data"`
		}
		if err := e.pegaJSON(ctx, e.baseDD+"/cdn/"+versao+"/data/pt_BR/item.json", &r); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrSemEstaticos, err)
		}
		itens = map[string]Item{}
		for k, it := range r.Data {
			linhas, desc := LimpaDescricao(it.Description)
			itens[k] = Item{Nome: it.Name, Custo: it.Gold.Total, Atributos: atributosDoItem(it.Stats, linhas), Linhas: linhas, Descricao: desc}
		}
		gravaArquivoJSON(filepath.Join(pasta, "itens.json"), itens)
	}
	for k, c := range camps {
		if id, err := strconv.Atoi(k); err == nil {
			// O registro do jogo baixado antes fica num arquivo próprio por campeão.
			var doJogo AtributosCampeao
			if leArquivoJSON(filepath.Join(pasta, "jogo", strings.ToLower(c.Chave)+".json"), &doJogo) && doJogo.Fonte == "jogo" {
				c = doJogo
			}
			est.Campeoes[id] = c
		}
	}
	for k, it := range itens {
		if id, err := strconv.Atoi(k); err == nil {
			est.Itens[id] = it
		}
	}
	return est, nil
}

var errJaTentou = errors.New("já tentou há pouco")

// registroDoJogo baixa o registro do campeão do CommunityDragon (arquivos do
// jogo do patch) e completa o que o Data Dragon tem errado. Chamado com e.mu.
func (e *EstaticosRede) registroDoJogo(ctx context.Context, patch, versao string, c AtributosCampeao) (AtributosCampeao, error) {
	chave := "jogo:" + patch + ":" + c.Chave
	if t, ok := e.falhou[chave]; ok && time.Since(t) < 10*time.Minute {
		return c, errJaTentou
	}
	nome := strings.ToLower(c.Chave)
	var bin map[string]json.RawMessage
	url := fmt.Sprintf("%s/%s/game/data/characters/%s/%s.bin.json", e.baseCD, patch, nome, nome)
	if err := e.pegaJSON(ctx, url, &bin); err != nil {
		e.falhou[chave] = time.Now()
		return c, err
	}
	novo, ok := DoRegistroDoJogo(bin, c)
	if !ok {
		e.falhou[chave] = time.Now()
		return c, fmt.Errorf("registro do campeão não achado em %s", url)
	}
	gravaArquivoJSON(filepath.Join(e.dir, versao, "jogo", nome+".json"), novo)
	return novo, nil
}

// DoRegistroDoJogo lê o CharacterRecord (".../CharacterRecords/Root") do
// .bin.json e devolve os atributos; campo ausente fica com o valor do Data
// Dragon (o arquivo do jogo omite o que é igual ao padrão), salvo a razão da
// velocidade de ataque, que sem o campo é a própria base.
func DoRegistroDoJogo(bin map[string]json.RawMessage, dd AtributosCampeao) (AtributosCampeao, bool) {
	var reg map[string]json.RawMessage
	for k, v := range bin {
		if strings.HasSuffix(k, "/CharacterRecords/Root") {
			if json.Unmarshal(v, &reg) == nil {
				break
			}
		}
	}
	if reg == nil {
		return dd, false
	}
	valor := func(campo string, padrao float64) float64 {
		var m struct {
			BaseValue *float64 `json:"baseValue"`
		}
		if b, ok := reg[campo]; ok && json.Unmarshal(b, &m) == nil && m.BaseValue != nil {
			// O arquivo guarda em float32 (4.799999713897705): arredonda a 4 casas.
			return float64(int64(*m.BaseValue*10000+0.5)) / 10000
		}
		return padrao
	}
	r := dd
	r.AD = valor("baseDamageModifiable", dd.AD)
	r.ADNivel = valor("damagePerLevelModifiable", 0)
	r.Armadura = valor("baseArmorModifiable", dd.Armadura)
	r.ArmaduraNivel = valor("armorPerLevelModifiable", dd.ArmaduraNivel)
	r.RM = valor("baseMR", dd.RM)
	r.RMNivel = valor("mrPerLevel", dd.RMNivel)
	r.VelAtaque = valor("attackSpeedModifiable", dd.VelAtaque)
	r.VelAtaqueRazao = valor("attackSpeedRatioModifiable", r.VelAtaque)
	r.VelAtaqueNivel = valor("attackSpeedPerLevelModifiable", dd.VelAtaqueNivel)
	r.VelMov = valor("baseMoveSpeedModifiable", dd.VelMov)
	r.Fonte = "jogo"
	return r, true
}

var (
	reStats  = regexp.MustCompile(`(?s)<stats>(.*?)</stats>`)
	reBR     = regexp.MustCompile(`(?i)<br\s*/?>`)
	reTag    = regexp.MustCompile(`<[^>]+>`)
	reEspaco = regexp.MustCompile(`[ \t]+`)
	reLinhas = regexp.MustCompile(`\n{3,}`)
	reNumero = regexp.MustCompile(`^([\d.,]+)(%?)\s*(?:de\s+)?(.+)$`)
)

// LimpaDescricao separa o texto do item do jogo em linhas de atributo (o
// bloco <stats>) e os efeitos, sem a marcação (<passive>, <keyword>...).
func LimpaDescricao(d string) ([]string, string) {
	linhas := []string{}
	if m := reStats.FindStringSubmatch(d); m != nil {
		for _, l := range reBR.Split(m[1], -1) {
			l = strings.TrimSpace(reEspaco.ReplaceAllString(html.UnescapeString(reTag.ReplaceAllString(l, "")), " "))
			if l != "" {
				linhas = append(linhas, l)
			}
		}
		d = strings.Replace(d, m[0], "", 1)
	}
	d = reBR.ReplaceAllString(d, "\n")
	// Passiva e ativo: o nome vira o começo da linha ("Galvanizar: ...").
	d = regexp.MustCompile(`</(passive|active)>\s*`).ReplaceAllString(d, ": ")
	d = html.UnescapeString(reTag.ReplaceAllString(d, ""))
	var out []string
	for _, l := range strings.Split(d, "\n") {
		out = append(out, strings.TrimSpace(reEspaco.ReplaceAllString(l, " ")))
	}
	desc := strings.TrimSpace(reLinhas.ReplaceAllString(strings.Join(out, "\n"), "\n\n"))
	return linhas, desc
}

// atributosDoItem: do objeto stats do Data Dragon; o que faltar nele e
// estiver no texto do jogo (linhas) vem do texto.
func atributosDoItem(stats map[string]float64, linhas []string) AtributosItem {
	a := AtributosItem{
		AD: stats["FlatPhysicalDamageMod"], AP: stats["FlatMagicDamageMod"], Armadura: stats["FlatArmorMod"],
		RM: stats["FlatSpellBlockMod"], VelAtaquePct: stats["PercentAttackSpeedMod"], VelMov: stats["FlatMovementSpeedMod"],
		VelMovPct: stats["PercentMovementSpeedMod"], Vida: stats["FlatHPPoolMod"], Mana: stats["FlatMPPoolMod"],
	}
	for _, l := range linhas {
		m := reNumero.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		v, err := strconv.ParseFloat(strings.Replace(m[1], ",", ".", 1), 64)
		if err != nil {
			continue
		}
		pct := m[2] == "%"
		nome := strings.ToLower(m[3])
		completa := func(campo *float64, val float64) {
			if *campo == 0 {
				*campo = val
			}
		}
		switch {
		case nome == "dano de ataque":
			completa(&a.AD, v)
		case nome == "poder de habilidade":
			completa(&a.AP, v)
		case nome == "armadura":
			completa(&a.Armadura, v)
		case nome == "resistência mágica":
			completa(&a.RM, v)
		case nome == "velocidade de ataque" && pct:
			completa(&a.VelAtaquePct, v/100)
		case nome == "velocidade de movimento" && pct:
			completa(&a.VelMovPct, v/100)
		case nome == "velocidade de movimento":
			completa(&a.VelMov, v)
		case nome == "vida":
			completa(&a.Vida, v)
		case nome == "mana":
			completa(&a.Mana, v)
		}
	}
	return a
}

// EstaticosDoTeste é a tabela fixa do modo teste: os campeões do LCU falso com
// a mesma base (números redondos, não os do jogo) e os itens que o jogo falso
// usa. Só para a tela ter o que mostrar sem rede.
func EstaticosDoTeste() EstaticosFixos {
	camps := map[int]AtributosCampeao{}
	for id, nome := range map[int]string{54: "Malphite", 11: "Master Yi", 166: "Akshan", 22: "Ashe", 147: "Seraphine",
		17: "Teemo", 19: "Warwick", 7: "LeBlanc", 901: "Smolder", 99: "Lux", 157: "Yasuo", 777: "Yone", 64: "Lee Sin",
		412: "Thresh", 51: "Caitlyn"} {
		camps[id] = AtributosCampeao{Chave: strings.ReplaceAll(nome, " ", ""), Nome: nome, AD: 60, ADNivel: 3, Armadura: 30,
			ArmaduraNivel: 4.5, RM: 32, RMNivel: 2.05, VelAtaque: 0.65, VelAtaqueRazao: 0.65, VelAtaqueNivel: 2.5, VelMov: 335,
			Fonte: "jogo"}
	}
	return EstaticosFixos{
		VersaoDados: "teste",
		Campeoes:    camps,
		Itens: map[int]Item{
			3031: {Nome: "Gume do Infinito", Custo: 3500, Atributos: AtributosItem{AD: 75},
				Linhas: []string{"75 de Dano de Ataque", "25% de Chance de Acerto Crítico"}, Descricao: "Perfeição: com 60% ou mais de chance de acerto crítico, recebe dano de acerto crítico."},
			3006: {Nome: "Grevas do Berserker", Custo: 1100, Atributos: AtributosItem{VelAtaquePct: 0.3, VelMov: 45},
				Linhas: []string{"30% de Velocidade de Ataque", "45 de Velocidade de Movimento"}},
			3340: {Nome: "Sentinela Invisível", Custo: 0, Linhas: []string{}, Descricao: "Ativo: posiciona uma Sentinela Invisível."},
		},
		Avisos: []string{"modo teste: base e itens são uma tabela fixa, não os do jogo"},
	}
}
