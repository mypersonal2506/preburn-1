//go:build embedweb

package web

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEmbeddedDashboard(t *testing.T) {
	files, err := fs.Sub(dist, distDirectory)
	if err != nil {
		t.Fatalf("open embedded dist: %v", err)
	}
	index, err := fs.ReadFile(files, indexFile)
	if err != nil {
		t.Fatalf("read embedded index: %v", err)
	}
	assets, err := fs.Glob(files, hashedAssetsPrefix+"*")
	if err != nil || len(assets) == 0 {
		t.Fatalf("find hashed assets: %v, found %d", err, len(assets))
	}
	asset, err := fs.ReadFile(files, assets[0])
	if err != nil {
		t.Fatalf("read asset %s: %v", assets[0], err)
	}
	tests := []struct {
		name         string
		path         string
		status       int
		cacheControl string
		body         string
	}{
		{name: "root", path: "/", status: http.StatusOK, cacheControl: "no-cache", body: string(index)},
		{name: "dashboard route", path: "/customers/cus_1", status: http.StatusOK, cacheControl: "no-cache", body: string(index)},
		{name: "hashed asset", path: "/" + assets[0], status: http.StatusOK, cacheControl: "public, max-age=31536000, immutable", body: string(asset)},
		{name: "missing hashed asset", path: "/" + hashedAssetsPrefix + "missing-0000.js", status: http.StatusNotFound, cacheControl: ""},
	}
	handler := Handler()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, test.path, nil))

			if recorder.Code != test.status {
				t.Fatalf("status = %d, want %d", recorder.Code, test.status)
			}
			if cacheControl := recorder.Header().Get("Cache-Control"); cacheControl != test.cacheControl {
				t.Errorf("Cache-Control = %q, want %q", cacheControl, test.cacheControl)
			}
			if test.body != "" && recorder.Body.String() != test.body {
				t.Errorf("body = %q, want %q", recorder.Body.String(), test.body)
			}
		})
	}
}
