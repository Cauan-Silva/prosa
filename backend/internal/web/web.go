// Package web serve as telas (o React compilado, embutido no binário).
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// O Dockerfile copia o frontend/dist para cá antes de compilar. Sem ele (rodando o Go
// direto, com o Vite à parte), só existe o .gitkeep e a raiz mostra um aviso.
//
//go:embed all:dist
var arquivos embed.FS

// Telas devolve o handler que serve o site. Caminhos que não são arquivos (ex.: /sala/ABC123)
// recebem o index.html, para o roteador do React decidir o que mostrar.
func Telas() http.Handler {
	dist, _ := fs.Sub(arquivos, "dist")
	index, err := fs.ReadFile(dist, "index.html")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Write([]byte("API do Prosa no ar. As telas não foram compiladas: rode o front-end com \"npm run dev\" na pasta frontend."))
		})
	}
	arquivosEstaticos := http.FileServerFS(dist)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caminho := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if caminho != "" && caminho != "index.html" {
			if info, err := fs.Stat(dist, caminho); err == nil && !info.IsDir() {
				if strings.HasPrefix(caminho, "assets/") {
					// Os arquivos do Vite têm o hash no nome: podem ficar em cache para sempre.
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				arquivosEstaticos.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(index)
	})
}
