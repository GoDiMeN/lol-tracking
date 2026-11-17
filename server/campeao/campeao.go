// Package campeao traduz o championId do LCU para o nome do campeão.
//
// O LCU devolve só o número. O nome vem do Data Dragon (o CDN público de dados
// estáticos da Riot, sem chave), baixado uma vez e guardado em
// dados/campeoes.json. Sem rede, o app continua: o nome vira "campeão 166".
// No modo teste o mapa é fixo e nada sai para a rede.
package campeao

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// Nomes é o que os outros pacotes usam.
type Nomes interface {
	Nome(ctx context.Context, id int) string
}

// Reserva é o nome usado quando o id não é conhecido.
func Reserva(id int) string {
	return "campeão " + strconv.Itoa(id)
}

// Fixo é um mapa pronto (modo teste e testes).
type Fixo map[int]string

func (f Fixo) Nome(_ context.Context, id int) string {
	if n, ok := f[id]; ok {
		return n
	}
	return Reserva(id)
}

// BaseDataDragon é o endereço do Data Dragon.
const BaseDataDragon = "https://ddragon.leagueoflegends.com"

// DataDragon busca os nomes no Data Dragon com cache em disco.
type DataDragon struct {
	base    string
	arquivo string
	http    *http.Client

	mu sync.Mutex
	// carregou: já tentou ler o arquivo de cache nesta execução.
	carregou bool
	nomes    map[int]string
	chaves   map[int]string // id → chave do Data Dragon ("MonkeyKing"), para a imagem
	versao   string
	// ultimaTentativa: quando foi a última ida à rede. Um id desconhecido (campeão
	// novo, cache vazio) dispara uma atualização, mas no máximo uma a cada 10 min,
	// para uma lista de 20 partidas offline não virar 20 tentativas.
	ultimaTentativa time.Time
}

// NovoDataDragon guarda o cache em <dirDados>/campeoes.json.
func NovoDataDragon(dirDados string) *DataDragon {
	return &DataDragon{
		base:    BaseDataDragon,
		arquivo: filepath.Join(dirDados, "campeoes.json"),
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

type cache struct {
	Versao string            `json:"versao"`
	Nomes  map[string]string `json:"nomes"`
	Chaves map[string]string `json:"chaves,omitempty"`
}

func (d *DataDragon) Nome(ctx context.Context, id int) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.carregou {
		d.carregou = true
		d.leCache()
	}
	if n, ok := d.nomes[id]; ok {
		return n
	}
	if time.Since(d.ultimaTentativa) > 10*time.Minute {
		d.ultimaTentativa = time.Now()
		if err := d.atualiza(ctx); err != nil {
			log.Printf("campeões: Data Dragon indisponível, usando o id: %v", err)
		}
	}
	if n, ok := d.nomes[id]; ok {
		return n
	}
	return Reserva(id)
}

func (d *DataDragon) leCache() {
	b, err := os.ReadFile(d.arquivo)
	if err != nil {
		return
	}
	var c cache
	if json.Unmarshal(b, &c) != nil {
		return
	}
	d.nomes, d.chaves = map[int]string{}, map[int]string{}
	for k, v := range c.Nomes {
		if id, err := strconv.Atoi(k); err == nil {
			d.nomes[id] = v
		}
	}
	for k, v := range c.Chaves {
		if id, err := strconv.Atoi(k); err == nil {
			d.chaves[id] = v
		}
	}
	d.versao = c.Versao
}

func (d *DataDragon) pegaJSON(ctx context.Context, url string, destino any) error {
	ctx, cancela := context.WithTimeout(ctx, 10*time.Second)
	defer cancela()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s respondeu %d", url, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(destino)
}

// atualiza baixa a versão mais nova e a lista de campeões. Chamado com d.mu.
func (d *DataDragon) atualiza(ctx context.Context) error {
	var versoes []string
	if err := d.pegaJSON(ctx, d.base+"/api/versions.json", &versoes); err != nil {
		return err
	}
	if len(versoes) == 0 {
		return fmt.Errorf("lista de versões vazia")
	}
	var r struct {
		Data map[string]struct {
			ID   string `json:"id"`
			Key  string `json:"key"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := d.pegaJSON(ctx, d.base+"/cdn/"+versoes[0]+"/data/pt_BR/champion.json", &r); err != nil {
		return err
	}
	nomes, chaves := map[int]string{}, map[int]string{}
	c := cache{Versao: versoes[0], Nomes: map[string]string{}, Chaves: map[string]string{}}
	for k, ch := range r.Data {
		if id, err := strconv.Atoi(ch.Key); err == nil && ch.Name != "" {
			nomes[id] = ch.Name
			c.Nomes[ch.Key] = ch.Name
			// A chave da imagem é o "id" do campeão no Data Dragon (o nome do
			// objeto em data): "MonkeyKing" para o Wukong.
			chave := ch.ID
			if chave == "" {
				chave = k
			}
			chaves[id] = chave
			c.Chaves[ch.Key] = chave
		}
	}
	if len(nomes) == 0 {
		return fmt.Errorf("lista de campeões vazia")
	}
	d.nomes, d.chaves, d.versao = nomes, chaves, versoes[0]
	if b, err := json.Marshal(c); err == nil {
		os.MkdirAll(filepath.Dir(d.arquivo), 0o755)
		if err := os.WriteFile(d.arquivo, b, 0o644); err != nil {
			log.Printf("campeões: não gravei o cache: %v", err)
		}
	}
	return nil
}
