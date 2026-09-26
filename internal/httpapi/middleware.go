package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"runtime/debug"
	"strings"
	"time"

	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/logging"
)

const (
	requestIDHeader                = "X-Request-Id"
	requestIDMaximumLength         = 128
	apiPathPrefix                  = "/api/"
	dashboardContentSecurityPolicy = "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; font-src 'self'; connect-src 'self'; frame-ancestors 'none'"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

var requestIDCharacters = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)

// RequestID gives every request an id. It keeps the incoming X-Request-Id
// when that is 1 to 128 letters, digits, dots, underscores, colons or
// hyphens, and otherwise creates a UUID. It echoes the id in the X-Request-Id
// response header and passes on a copy of the request whose context carries
// the id, so every event logged with that context has a request_id attribute.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestID := request.Header.Get(requestIDHeader)
		if len(requestID) > requestIDMaximumLength || !requestIDCharacters.MatchString(requestID) {
			requestID = identifiers.New().String()
		}
		writer.Header().Set(requestIDHeader, requestID)
		next.ServeHTTP(writer, request.WithContext(logging.WithRequestID(request.Context(), requestID)))
	})
}

// SecurityHeaders sets X-Content-Type-Options nosniff and Referrer-Policy
// strict-origin-when-cross-origin on every response, and the dashboard
// Content-Security-Policy on every path outside /api/. The docs page at
// /api/docs sets its own policy that admits its renderer's CDN.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		header := writer.Header()
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		if !strings.HasPrefix(request.URL.Path, apiPathPrefix) {
			header.Set("Content-Security-Policy", dashboardContentSecurityPolicy)
		}
		next.ServeHTTP(writer, request)
	})
}

// AccessLog logs http.request_completed for every request with the method,
// the route pattern the ServeMux matched, the status and duration_seconds. It
// reads the pattern from its own request value after next returns, so every
// middleware between AccessLog and the ServeMux must pass that same value on
// and never a copy such as Request.WithContext returns.
func AccessLog(logger *logging.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: writer, status: http.StatusOK}
		next.ServeHTTP(recorder, request)
		logger.Info(request.Context(), logging.HTTPRequestCompleted,
			slog.String("method", request.Method),
			slog.String("route_pattern", request.Pattern),
			slog.Int("status", recorder.status),
			slog.Float64("duration_seconds", time.Since(started).Seconds()),
		)
	})
}

// Recover turns a panic in next into a 500 internal_error problem and logs
// http.panic_recovered with the panic value and the goroutine stack.
func Recover(logger *logging.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer recoverPanic(request.Context(), logger, writer)
		next.ServeHTTP(writer, request)
	})
}

func recoverPanic(ctx context.Context, logger *logging.Logger, writer http.ResponseWriter) {
	recovered := recover()
	if recovered == nil {
		return
	}
	logger.Error(ctx, logging.HTTPPanicRecovered,
		slog.String("panic", fmt.Sprint(recovered)),
		slog.String("stack", string(debug.Stack())),
	)
	writeProblem(ctx, logger, writer, newProblem(http.StatusInternalServerError, codeInternalError, internalErrorDetail))
}

// WriteHeader records status and writes it.
func (recorder *statusRecorder) WriteHeader(status int) {
	recorder.status = status
	recorder.ResponseWriter.WriteHeader(status)
}

// Unwrap returns the wrapped writer, so http.ResponseController and Huma
// reach its Flush and SetReadDeadline.
func (recorder *statusRecorder) Unwrap() http.ResponseWriter {
	return recorder.ResponseWriter
}
