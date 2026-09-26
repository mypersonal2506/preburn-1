// Package tracing exports OpenTelemetry traces over OTLP when
// OTEL_EXPORTER_OTLP_ENDPOINT is set, and does nothing when it is unset or
// empty. The standard OTEL_* variables configure the exporter, the sampler and
// the resource. OTEL_EXPORTER_OTLP_TRACES_PROTOCOL, or else
// OTEL_EXPORTER_OTLP_PROTOCOL, selects gRPC (grpc) or HTTP (http/protobuf, the
// default, or http/json). Incoming and outgoing requests carry W3C trace
// context and baggage.
package tracing
