package metrics

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const readHeaderTimeout = 10 * time.Second

// NewRegistry returns a registry with the Go runtime and process collectors
// registered.
func NewRegistry() *prometheus.Registry {
	registry := prometheus.NewRegistry()
	registry.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	return registry
}

// Serve answers GET /metrics on address with the metrics gathered from
// registry until ctx ends, then closes the server and returns nil. It returns
// an error when it cannot listen on address or the server stops on its own.
func Serve(ctx context.Context, address string, registry prometheus.Gatherer) error {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	server := &http.Server{Addr: address, Handler: mux, ReadHeaderTimeout: readHeaderTimeout}
	served := make(chan error, 1)
	go func() {
		served <- server.ListenAndServe()
	}()
	select {
	case err := <-served:
		return fmt.Errorf("serve metrics: %w", err)
	case <-ctx.Done():
		if err := server.Close(); err != nil {
			return fmt.Errorf("close metrics server: %w", err)
		}
		return nil
	}
}
