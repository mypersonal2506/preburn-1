package logging

import (
	"context"
	"io"
	"log/slog"
	"regexp"
	"slices"
)

const (
	requestIDAttributeKey = "request_id"
	redactedValue         = "[redacted]"
)

// Logger writes events as JSON lines through a *slog.Logger. It is safe for
// concurrent use.
type Logger struct {
	base *slog.Logger
}

type requestIDContextKey struct{}

var sensitiveKeyPattern = regexp.MustCompile(`(?i)(^|_)(passwords?|secrets?|token|api_key)(_|$)`)

// New returns a Logger that writes one JSON object per event to writer and
// drops events below level. Each object holds the time in UTC RFC 3339, the
// level, the event as msg, and the attributes.
func New(writer io.Writer, level slog.Level) *Logger {
	handler := slog.NewJSONHandler(writer, &slog.HandlerOptions{Level: level, ReplaceAttr: replaceAttribute})
	return &Logger{base: slog.New(handler)}
}

// WithRequestID returns a copy of ctx that carries requestID. Every event
// logged with that context, or one derived from it, has a request_id
// attribute.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDContextKey{}, requestID)
}

// Debug logs event with attributes at the debug level.
func (logger *Logger) Debug(ctx context.Context, event Event, attributes ...slog.Attr) {
	logger.log(ctx, slog.LevelDebug, event, attributes)
}

// Info logs event with attributes at the info level.
func (logger *Logger) Info(ctx context.Context, event Event, attributes ...slog.Attr) {
	logger.log(ctx, slog.LevelInfo, event, attributes)
}

// Warn logs event with attributes at the warn level.
func (logger *Logger) Warn(ctx context.Context, event Event, attributes ...slog.Attr) {
	logger.log(ctx, slog.LevelWarn, event, attributes)
}

// Error logs event with attributes at the error level.
func (logger *Logger) Error(ctx context.Context, event Event, attributes ...slog.Attr) {
	logger.log(ctx, slog.LevelError, event, attributes)
}

func (logger *Logger) log(ctx context.Context, level slog.Level, event Event, attributes []slog.Attr) {
	if requestID, found := ctx.Value(requestIDContextKey{}).(string); found {
		attributes = append([]slog.Attr{slog.String(requestIDAttributeKey, requestID)}, attributes...)
	}
	logger.base.LogAttrs(ctx, level, string(event), attributes...)
}

func replaceAttribute(groups []string, attribute slog.Attr) slog.Attr {
	if sensitiveKeyPattern.MatchString(attribute.Key) || slices.ContainsFunc(groups, sensitiveKeyPattern.MatchString) {
		return slog.String(attribute.Key, redactedValue)
	}
	if attribute.Value.Kind() == slog.KindTime {
		return slog.Time(attribute.Key, attribute.Value.Time().UTC())
	}
	return attribute
}
