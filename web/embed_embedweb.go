//go:build embedweb

package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

const (
	distDirectory          = "dist"
	indexFile              = "index.html"
	hashedAssetsPrefix     = "assets/"
	immutableCacheControl  = "public, max-age=31536000, immutable"
	revalidateCacheControl = "no-cache"
)

//go:embed all:dist
var dist embed.FS

func dashboardHandler() http.Handler {
	files, err := fs.Sub(dist, distDirectory)
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServerFS(files)
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		name := strings.TrimPrefix(path.Clean(request.URL.Path), "/")
		switch {
		case strings.HasPrefix(name, hashedAssetsPrefix):
			if isFile(files, name) {
				writer.Header().Set("Cache-Control", immutableCacheControl)
			}
			fileServer.ServeHTTP(writer, request)
		case isFile(files, name):
			writer.Header().Set("Cache-Control", revalidateCacheControl)
			fileServer.ServeHTTP(writer, request)
		default:
			writer.Header().Set("Cache-Control", revalidateCacheControl)
			http.ServeFileFS(writer, request, files, indexFile)
		}
	})
}

func isFile(files fs.FS, name string) bool {
	info, err := fs.Stat(files, name)
	return err == nil && info.Mode().IsRegular()
}
