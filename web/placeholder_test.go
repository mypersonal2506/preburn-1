//go:build !embedweb

package web_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/preburn/preburn/web"
)

func TestPlaceholderServesNotBuiltPage(t *testing.T) {
	handler := web.Handler()

	for _, path := range []string{"/", "/customers/cus_1", "/index.html"} {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))

			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", recorder.Code)
			}
			if contentType := recorder.Header().Get("Content-Type"); contentType != "text/html; charset=utf-8" {
				t.Errorf("Content-Type = %q, want text/html; charset=utf-8", contentType)
			}
			if !strings.Contains(recorder.Body.String(), "Dashboard not built. Run make web-build.") {
				t.Errorf("body = %q, want the not built text", recorder.Body.String())
			}
		})
	}
}

func TestPlaceholderRejectsUnsafeMethods(t *testing.T) {
	recorder := httptest.NewRecorder()
	web.Handler().ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", nil))

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", recorder.Code)
	}
	if allow := recorder.Header().Get("Allow"); allow != "GET, HEAD" {
		t.Errorf("Allow = %q, want GET, HEAD", allow)
	}
}
