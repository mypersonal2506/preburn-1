package tracing_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-cmp/cmp"
	"go.opentelemetry.io/otel"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"

	"github.com/preburn/preburn/internal/logging"
	"github.com/preburn/preburn/internal/tracing"
)

const (
	testServiceName     = "preburn"
	testVersion         = "1.2.3"
	testSpanName        = "tracing.test"
	collectorBufferSize = 16
)

type httpExport struct {
	Path        string
	ContentType string
}

type grpcCollector struct {
	collectortrace.UnimplementedTraceServiceServer
	exports chan *collectortrace.ExportTraceServiceRequest
}

func TestSetupWithoutEndpointInstallsNothing(t *testing.T) {
	configure(t, "", "", "")
	providerBefore := otel.GetTracerProvider()

	shutdown, err := tracing.Setup(t.Context(), discardLogger(), testServiceName, testVersion)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if err := shutdown(t.Context()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	if otel.GetTracerProvider() != providerBefore {
		t.Error("Setup replaced the global tracer provider")
	}
	mux := http.NewServeMux()
	if tracing.Middleware(mux) != http.Handler(mux) {
		t.Error("Middleware wrapped the handler while tracing is off")
	}
}

func TestSetupExportsOverHTTP(t *testing.T) {
	tests := []struct {
		name            string
		protocol        string
		wantContentType string
	}{
		{name: "default protocol", protocol: "", wantContentType: "application/x-protobuf"},
		{name: "http/protobuf", protocol: "http/protobuf", wantContentType: "application/x-protobuf"},
		{name: "http/json", protocol: "http/json", wantContentType: "application/json"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			exports := make(chan httpExport, collectorBufferSize)
			collector := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				exports <- httpExport{Path: request.URL.Path, ContentType: request.Header.Get("Content-Type")}
				writer.WriteHeader(http.StatusOK)
			}))
			t.Cleanup(collector.Close)
			configure(t, collector.URL, test.protocol, "")

			exportOneSpan(t, discardLogger())

			want := httpExport{Path: "/v1/traces", ContentType: test.wantContentType}
			if diff := cmp.Diff(want, receive(t, exports)); diff != "" {
				t.Errorf("export mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSetupExportsRouteSpansOverGRPC(t *testing.T) {
	tests := []struct {
		name           string
		protocol       string
		tracesProtocol string
	}{
		{name: "protocol grpc", protocol: "grpc"},
		{name: "traces protocol grpc overrides protocol", protocol: "http/protobuf", tracesProtocol: "grpc"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			collector, endpoint := startGRPCCollector(t)
			configure(t, endpoint, test.protocol, test.tracesProtocol)

			shutdown, err := tracing.Setup(t.Context(), discardLogger(), testServiceName, testVersion)
			if err != nil {
				t.Fatalf("Setup: %v", err)
			}
			mux := http.NewServeMux()
			mux.HandleFunc("GET /plans/{plan_id}", func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(http.StatusNoContent)
			})
			tracing.Middleware(mux).ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/plans/plan_123", nil))
			if err := shutdown(t.Context()); err != nil {
				t.Fatalf("shutdown: %v", err)
			}

			serviceNames, spanNames := exportedNames(receive(t, collector.exports))
			if diff := cmp.Diff([]string{testServiceName}, serviceNames); diff != "" {
				t.Errorf("service.name mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff([]string{"GET /plans/{plan_id}"}, spanNames); diff != "" {
				t.Errorf("span names mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSetupLetsEnvironmentOverrideServiceName(t *testing.T) {
	collector, endpoint := startGRPCCollector(t)
	configure(t, endpoint, "grpc", "")
	t.Setenv("OTEL_SERVICE_NAME", "preburn-worker")

	exportOneSpan(t, discardLogger())

	serviceNames, _ := exportedNames(receive(t, collector.exports))
	if diff := cmp.Diff([]string{"preburn-worker"}, serviceNames); diff != "" {
		t.Errorf("service.name mismatch (-want +got):\n%s", diff)
	}
}

func TestSetupRejectsUnsupportedProtocol(t *testing.T) {
	configure(t, "http://127.0.0.1:4318", "http/xml", "")
	providerBefore := otel.GetTracerProvider()

	_, err := tracing.Setup(t.Context(), discardLogger(), testServiceName, testVersion)

	if !errors.Is(err, tracing.ErrUnsupportedProtocol) {
		t.Fatalf("Setup error = %v, want ErrUnsupportedProtocol", err)
	}
	if otel.GetTracerProvider() != providerBefore {
		t.Error("Setup replaced the global tracer provider")
	}
}

func TestSetupLogsExportFailures(t *testing.T) {
	collector := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(collector.Close)
	configure(t, collector.URL, "http/protobuf", "")
	var output bytes.Buffer

	exportOneSpan(t, logging.New(&output, slog.LevelDebug))

	var line struct {
		Level   string `json:"level"`
		Message string `json:"msg"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(output.Bytes(), &line); err != nil {
		t.Fatalf("decode log line %q: %v", output.String(), err)
	}
	if line.Level != slog.LevelError.String() || line.Message != string(logging.TracingSDKError) || line.Error == "" {
		t.Errorf("log line = %q, want one error event %s with an error attribute", output.String(), logging.TracingSDKError)
	}
}

func (collector *grpcCollector) Export(_ context.Context, request *collectortrace.ExportTraceServiceRequest) (*collectortrace.ExportTraceServiceResponse, error) {
	collector.exports <- request
	return &collectortrace.ExportTraceServiceResponse{}, nil
}

func startGRPCCollector(t *testing.T) (collector *grpcCollector, endpoint string) {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	collector = &grpcCollector{exports: make(chan *collectortrace.ExportTraceServiceRequest, collectorBufferSize)}
	server := grpc.NewServer()
	collectortrace.RegisterTraceServiceServer(server, collector)
	served := make(chan error, 1)
	go func() {
		served <- server.Serve(listener)
	}()
	t.Cleanup(func() {
		server.Stop()
		if err := <-served; err != nil {
			t.Errorf("serve collector: %v", err)
		}
	})
	return collector, "http://" + listener.Addr().String()
}

func configure(t *testing.T, endpoint, protocol, tracesProtocol string) {
	t.Helper()
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", endpoint)
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", protocol)
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", tracesProtocol)
}

func exportOneSpan(t *testing.T, logger *logging.Logger) {
	t.Helper()
	shutdown, err := tracing.Setup(t.Context(), logger, testServiceName, testVersion)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	_, span := otel.Tracer(testSpanName).Start(t.Context(), testSpanName)
	span.End()
	if err := shutdown(t.Context()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func receive[Export any](t *testing.T, exports <-chan Export) Export {
	t.Helper()
	if len(exports) == 0 {
		t.Fatal("collector received no export before shutdown returned")
	}
	return <-exports
}

func exportedNames(export *collectortrace.ExportTraceServiceRequest) (serviceNames, spanNames []string) {
	for _, resourceSpans := range export.GetResourceSpans() {
		for _, attribute := range resourceSpans.GetResource().GetAttributes() {
			if attribute.GetKey() == "service.name" {
				serviceNames = append(serviceNames, attribute.GetValue().GetStringValue())
			}
		}
		for _, scopeSpans := range resourceSpans.GetScopeSpans() {
			for _, span := range scopeSpans.GetSpans() {
				spanNames = append(spanNames, span.GetName())
			}
		}
	}
	return serviceNames, spanNames
}

func discardLogger() *logging.Logger {
	return logging.New(io.Discard, slog.LevelDebug)
}
