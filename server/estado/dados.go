package estado

import (
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"

	"loltracking/server/web"
)

// Dados é a resposta de GET /api/dados: onde o app guarda tudo e quanto ocupa.
// A tela de Configurações mostra isso para a pessoa saber o que apagar (ou
// copiar) sem abrir o explorador.
type Dados struct {
	Pasta string `json:"pasta"`
	Banco string `json:"banco"`
	Bytes struct {
		Banco    int64 `json:"banco"`
		Imagens  int64 `json:"imagens"`
		Capturas int64 `json:"capturas"`
		Outros   int64 `json:"outros"`
		Total    int64 `json:"total"`
	} `json:"bytes"`
}

// MedeDados soma os arquivos da pasta. O banco conta com o -wal e o -shm (o
// SQLite guarda ali o que ainda não juntou ao arquivo principal).
func MedeDados(pasta, arquivoBanco string) (Dados, error) {
	d := Dados{Pasta: pasta, Banco: arquivoBanco}
	if abs, err := filepath.Abs(pasta); err == nil {
		d.Pasta = abs
	}
	err := filepath.WalkDir(pasta, func(caminho string, e fs.DirEntry, err error) error {
		if err != nil {
			// Arquivo que sumiu no meio (o SQLite apaga o -shm): segue.
			return nil
		}
		if e.IsDir() {
			return nil
		}
		info, err := e.Info()
		if err != nil {
			return nil
		}
		n := info.Size()
		rel, _ := filepath.Rel(pasta, caminho)
		rel = filepath.ToSlash(rel)
		switch {
		case rel == arquivoBanco || rel == arquivoBanco+"-wal" || rel == arquivoBanco+"-shm":
			d.Bytes.Banco += n
		case strings.HasPrefix(rel, "imagens/"):
			d.Bytes.Imagens += n
		case strings.HasPrefix(rel, "capturas/"):
			d.Bytes.Capturas += n
		default:
			d.Bytes.Outros += n
		}
		d.Bytes.Total += n
		return nil
	})
	return d, err
}

// RegistraDados pendura GET /api/dados.
func RegistraDados(mux *http.ServeMux, pasta, arquivoBanco string) {
	mux.HandleFunc("GET /api/dados", func(w http.ResponseWriter, r *http.Request) {
		d, err := MedeDados(pasta, arquivoBanco)
		if err != nil {
			web.WriteErr(w, http.StatusInternalServerError, "não foi possível medir a pasta de dados: "+err.Error())
			return
		}
		web.WriteJSON(w, http.StatusOK, d)
	})
}
