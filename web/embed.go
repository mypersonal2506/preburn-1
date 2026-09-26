package web

import "net/http"

const allowedMethods = "GET, HEAD"

// Handler serves the dashboard to GET and HEAD requests and answers 405 to
// every other method. The server mounts it on / next to the /api/ routes.
func Handler() http.Handler {
	dashboard := dashboardHandler()
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			writer.Header().Set("Allow", allowedMethods)
			http.Error(writer, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		dashboard.ServeHTTP(writer, request)
	})
}
