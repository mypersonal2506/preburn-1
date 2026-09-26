//go:build !embedweb

package web

import (
	"io"
	"net/http"
)

const placeholderPage = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Preburn</title>
</head>
<body>
<p>Dashboard not built. Run make web-build.</p>
</body>
</html>
`

func dashboardHandler() http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		writer.Header().Set("Cache-Control", "no-cache")
		_, _ = io.WriteString(writer, placeholderPage)
	})
}
