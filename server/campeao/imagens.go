package campeao

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"loltracking/server/web"
)

// Imagens do Data Dragon (retrato quadrado do campeão e ícone do item) com
// cache em disco: baixa uma vez e serve do nosso servidor, para a tela
// funcionar offline depois da primeira vez. Rotas em contrato-http.md.

// ErrSemImagem: não há a imagem nem no cache nem na rede.
var ErrSemImagem = errors.New("imagem indisponível")

// garanteChaves carrega o cache e, se ele não tem a chave da imagem do id
// (cache antigo, campeão novo), atualiza (no máximo uma ida à rede a cada 10 min).
func (d *DataDragon) chaveEVersao(ctx context.Context, id int) (string, string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.carregou {
		d.carregou = true
		d.leCache()
	}
	if c, ok := d.chaves[id]; ok && d.versao != "" {
		return c, d.versao, nil
	}
	if time.Since(d.ultimaTentativa) > 10*time.Minute {
		d.ultimaTentativa = time.Now()
		if err := d.atualiza(ctx); err != nil {
			return "", "", err
		}
	}
	if c, ok := d.chaves[id]; ok {
		return c, d.versao, nil
	}
	return "", "", ErrSemImagem
}

func (d *DataDragon) versaoAtual(ctx context.Context) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.carregou {
		d.carregou = true
		d.leCache()
	}
	if d.versao == "" && time.Since(d.ultimaTentativa) > 10*time.Minute {
		d.ultimaTentativa = time.Now()
		if err := d.atualiza(ctx); err != nil {
			return "", err
		}
	}
	if d.versao == "" {
		return "", ErrSemImagem
	}
	return d.versao, nil
}

// Imagens serve as imagens. dd nil (modo teste) = sempre 404: nada sai para a rede.
type Imagens struct {
	dd  *DataDragon
	dir string
	mu  sync.Mutex // um download por vez (a tela pede 10 retratos de uma vez)
}

// NovasImagens guarda as imagens em <dirDados>/imagens/{campeao,item}/<id>.png.
func NovasImagens(dd *DataDragon, dirDados string) *Imagens {
	return &Imagens{dd: dd, dir: filepath.Join(dirDados, "imagens")}
}

// RegistraImagens pendura GET /api/imagens/campeao/{id} e /api/imagens/item/{id}.
func RegistraImagens(mux *http.ServeMux, im *Imagens) {
	mux.HandleFunc("GET /api/imagens/campeao/{id}", func(w http.ResponseWriter, r *http.Request) { im.serve(w, r, "campeao") })
	mux.HandleFunc("GET /api/imagens/item/{id}", func(w http.ResponseWriter, r *http.Request) { im.serve(w, r, "item") })
}

func (im *Imagens) serve(w http.ResponseWriter, r *http.Request, tipo string) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 || id > 1000000 {
		web.WriteErr(w, http.StatusBadRequest, "id inválido")
		return
	}
	arq := filepath.Join(im.dir, tipo, strconv.Itoa(id)+".png")
	if _, err := os.Stat(arq); err != nil {
		if err := im.baixa(r.Context(), tipo, id, arq); err != nil {
			if !errors.Is(err, ErrSemImagem) {
				log.Printf("imagens: %s %d: %v", tipo, id, err)
			}
			web.WriteErr(w, http.StatusNotFound, "imagem indisponível (sem rede na primeira vez, ou id desconhecido)")
			return
		}
	}
	// A imagem de um id não muda dentro de uma versão: o navegador guarda.
	w.Header().Set("Cache-Control", "max-age=604800")
	w.Header().Set("Content-Type", "image/png")
	http.ServeFile(w, r, arq)
}

func (im *Imagens) baixa(ctx context.Context, tipo string, id int, arq string) error {
	if im.dd == nil {
		return ErrSemImagem
	}
	im.mu.Lock()
	defer im.mu.Unlock()
	if _, err := os.Stat(arq); err == nil {
		return nil // outro pedido baixou enquanto este esperava
	}
	var url string
	switch tipo {
	case "campeao":
		chave, versao, err := im.dd.chaveEVersao(ctx, id)
		if err != nil {
			return err
		}
		url = fmt.Sprintf("%s/cdn/%s/img/champion/%s.png", im.dd.base, versao, chave)
	default:
		versao, err := im.dd.versaoAtual(ctx)
		if err != nil {
			return err
		}
		url = fmt.Sprintf("%s/cdn/%s/img/item/%d.png", im.dd.base, versao, id)
	}
	ctx, cancela := context.WithTimeout(ctx, 15*time.Second)
	defer cancela()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := im.dd.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ErrSemImagem
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s respondeu %d", url, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(arq), 0o755); err != nil {
		return err
	}
	// Grava num temporário e renomeia: um download cortado não vira imagem quebrada no cache.
	tmp := arq + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, arq)
}
