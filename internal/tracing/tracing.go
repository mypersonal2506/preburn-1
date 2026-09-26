package tracing

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"

	"github.com/preburn/preburn/internal/logging"
)

const (
	endpointVariable       = "OTEL_EXPORTER_OTLP_ENDPOINT"
	protocolVariable       = "OTEL_EXPORTER_OTLP_PROTOCOL"
	tracesProtocolVariable = "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL"
	grpcProtocol           = "grpc"
	httpProtobufProtocol   = "http/protobuf"
	httpJSONProtocol       = "http/json"
	serverOperation        = "http.server"
)

// ErrUnsupportedProtocol means the OTLP protocol variable holds a value other
// than grpc, http/protobuf or http/json.
var ErrUnsupportedProtocol = errors.New("unsupported OTLP protocol")

// Setup installs the global tracer provider, propagator and error handler when
// OTEL_EXPORTER_OTLP_ENDPOINT is set, and returns the provider's shutdown,
// which exports the spans still queued. Spans carry serviceName and version as
// service.name and service.version unless OTEL_SERVICE_NAME or
// OTEL_RESOURCE_ATTRIBUTES override them. SDK errors, such as failed exports,
// are logged to logger as TracingSDKError. When the variable is unset, Setup
// installs nothing and returns a shutdown that does nothing.
func Setup(ctx context.Context, logger *logging.Logger, serviceName, version string) (shutdown func(context.Context) error, err error) {
	if !enabled() {
		return shutdownNothing, nil
	}
	exporter, err := newExporter(ctx)
	if err != nil {
		return nil, err
	}
	serviceResource, err := resource.New(ctx,
		resource.WithTelemetrySDK(),
		resource.WithAttributes(semconv.ServiceName(serviceName), semconv.ServiceVersion(version)),
		resource.WithFromEnv(),
	)
	if err != nil {
		return nil, fmt.Errorf("build tracing resource: %w", err)
	}
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		logger.Error(ctx, logging.TracingSDKError, slog.Any("error", err))
	}))
	provider := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter), sdktrace.WithResource(serviceResource))
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	return provider.Shutdown, nil
}

// Middleware returns handler wrapped in a span per request when
// OTEL_EXPORTER_OTLP_ENDPOINT is set, and handler itself otherwise. Spans are
// named after the method and the matched Request.Pattern. Call it after Setup,
// so the span goes to the provider Setup installed.
func Middleware(handler http.Handler) http.Handler {
	if !enabled() {
		return handler
	}
	return otelhttp.NewHandler(handler, serverOperation)
}

func enabled() bool {
	return os.Getenv(endpointVariable) != ""
}

func newExporter(ctx context.Context) (*otlptrace.Exporter, error) {
	protocol := cmp.Or(os.Getenv(tracesProtocolVariable), os.Getenv(protocolVariable), httpProtobufProtocol)
	switch protocol {
	case grpcProtocol:
		return otlptracegrpc.New(ctx)
	case httpProtobufProtocol, httpJSONProtocol:
		return otlptracehttp.New(ctx)
	default:
		return nil, fmt.Errorf("%w %q, want %s, %s or %s", ErrUnsupportedProtocol, protocol, grpcProtocol, httpProtobufProtocol, httpJSONProtocol)
	}
}

func shutdownNothing(context.Context) error {
	return nil
}
