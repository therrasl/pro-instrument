package catalog

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed assets/tools/*.jpg
var toolAssets embed.FS

func toolAssetsHandler() http.Handler {
	assets, err := fs.Sub(toolAssets, "assets/tools")
	if err != nil {
		panic(err)
	}

	files := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Cache-Control", "public, max-age=86400")
		response.Header().Set("X-Content-Type-Options", "nosniff")
		http.StripPrefix("/static/tools/", files).ServeHTTP(response, request)
	})
}
