package metrics_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/preburn/preburn/internal/metrics"
)

const (
	requestsMetricName  = "preburn_http_requests_total"
	durationsMetricName = "preburn_http_request_duration_seconds"
)

func TestMiddlewareLabelsByRoutePattern(t *testing.T) {
	registry := prometheus.NewRegistry()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /plans/{plan_id}", func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusCreated)
	})
	handler := metrics.NewHTTP(registry).Middleware(mux)

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/plans/plan_123", nil))

	wantRequests := map[string]float64{"method=GET route=/plans/{plan_id} status=201": 1}
	if diff := cmp.Diff(wantRequests, gatherSeries(t, registry, requestsMetricName)); diff != "" {
		t.Errorf("%s mismatch (-want +got):\n%s", requestsMetricName, diff)
	}
	wantDurations := map[string]float64{"method=GET route=/plans/{plan_id}": 1}
	if diff := cmp.Diff(wantDurations, gatherSeries(t, registry, durationsMetricName)); diff != "" {
		t.Errorf("%s sample count mismatch (-want +got):\n%s", durationsMetricName, diff)
	}
}

func TestMiddlewareBoundsLabelValues(t *testing.T) {
	registry := prometheus.NewRegistry()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /plans/{plan_id}", func(http.ResponseWriter, *http.Request) {})
	mux.HandleFunc("/healthz", func(writer http.ResponseWriter, _ *http.Request) {
		if _, err := writer.Write([]byte("ok")); err != nil {
			t.Errorf("write body: %v", err)
		}
		writer.WriteHeader(http.StatusTeapot)
	})
	handler := metrics.NewHTTP(registry).Middleware(mux)

	for _, request := range []*http.Request{
		httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/plans/plan_123", nil),
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/missing/one", nil),
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/missing/two", nil),
		httptest.NewRequestWithContext(t.Context(), "PURGE", "/healthz", nil),
	} {
		handler.ServeHTTP(httptest.NewRecorder(), request)
	}

	want := map[string]float64{
		"method=POST route=unmatched status=405": 1,
		"method=GET route=unmatched status=404":  2,
		"method=other route=/healthz status=200": 1,
	}
	if diff := cmp.Diff(want, gatherSeries(t, registry, requestsMetricName)); diff != "" {
		t.Errorf("%s mismatch (-want +got):\n%s", requestsMetricName, diff)
	}
}

func TestMiddlewareKeepsResponseControllerAccess(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /events", func(writer http.ResponseWriter, _ *http.Request) {
		if err := http.NewResponseController(writer).Flush(); err != nil {
			t.Errorf("flush through the middleware: %v", err)
		}
	})
	recorder := httptest.NewRecorder()

	metrics.NewHTTP(prometheus.NewRegistry()).Middleware(mux).ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/events", nil))

	if !recorder.Flushed {
		t.Error("response was not flushed")
	}
}

func gatherSeries(t *testing.T, registry prometheus.Gatherer, familyName string) map[string]float64 {
	t.Helper()
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	series := map[string]float64{}
	for _, family := range families {
		if family.GetName() != familyName {
			continue
		}
		for _, metric := range family.GetMetric() {
			var labels []string
			for _, label := range metric.GetLabel() {
				labels = append(labels, label.GetName()+"="+label.GetValue())
			}
			value := metric.GetCounter().GetValue()
			if family.GetType() == dto.MetricType_HISTOGRAM {
				value = float64(metric.GetHistogram().GetSampleCount())
			}
			series[strings.Join(labels, " ")] = value
		}
	}
	return series
}
