package metrics

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	unmatchedRouteLabel = "unmatched"
	otherMethodLabel    = "other"
)

// HTTP holds preburn_http_requests_total{route,method,status} and
// preburn_http_request_duration_seconds{route,method}.
type HTTP struct {
	requests  *prometheus.CounterVec
	durations *prometheus.HistogramVec
}

type statusRecorder struct {
	http.ResponseWriter
	status        int
	headerWritten bool
}

// NewHTTP registers the HTTP request metrics on registry. It panics when they
// are already registered there, which is a wiring bug.
func NewHTTP(registry prometheus.Registerer) *HTTP {
	httpMetrics := &HTTP{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "preburn_http_requests_total",
			Help: "HTTP requests by route pattern, method and response status.",
		}, []string{"route", "method", "status"}),
		durations: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "preburn_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds by route pattern and method.",
			Buckets: prometheus.DefBuckets,
		}, []string{"route", "method"}),
	}
	registry.MustRegister(httpMetrics.requests, httpMetrics.durations)
	return httpMetrics
}

// Middleware counts and times every request handler serves. The route label
// is the path of the matched Request.Pattern without its method, or unmatched
// when no pattern matched. Methods outside the standard nine are labelled
// other, so clients cannot grow the label set. Wrap the http.ServeMux directly,
// as the package documentation explains.
func (httpMetrics *HTTP) Middleware(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: writer, status: http.StatusOK}
		handler.ServeHTTP(recorder, request)
		route := routeLabel(request.Pattern)
		method := methodLabel(request.Method)
		httpMetrics.requests.WithLabelValues(route, method, strconv.Itoa(recorder.status)).Inc()
		httpMetrics.durations.WithLabelValues(route, method).Observe(time.Since(started).Seconds())
	})
}

func (recorder *statusRecorder) WriteHeader(status int) {
	if !recorder.headerWritten && status >= http.StatusOK {
		recorder.status = status
		recorder.headerWritten = true
	}
	recorder.ResponseWriter.WriteHeader(status)
}

func (recorder *statusRecorder) Write(body []byte) (int, error) {
	recorder.headerWritten = true
	return recorder.ResponseWriter.Write(body)
}

func (recorder *statusRecorder) Unwrap() http.ResponseWriter {
	return recorder.ResponseWriter
}

func routeLabel(pattern string) string {
	if pattern == "" {
		return unmatchedRouteLabel
	}
	methodEnd := strings.IndexAny(pattern, " \t")
	if methodEnd < 0 {
		return pattern
	}
	return strings.TrimLeft(pattern[methodEnd:], " \t")
}

func methodLabel(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace:
		return method
	default:
		return otherMethodLabel
	}
}
