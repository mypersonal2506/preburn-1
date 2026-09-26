package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/preburn/preburn/internal/logging"
)

const jsonContentType = "application/json"

func writeProblem(ctx context.Context, logger *logging.Logger, writer http.ResponseWriter, problem *Problem) {
	writeJSON(ctx, logger, writer, problemContentType, problem.Status, problem)
}

func writeJSON(ctx context.Context, logger *logging.Logger, writer http.ResponseWriter, contentType string, status int, body any) {
	writer.Header().Set("Content-Type", contentType)
	writer.WriteHeader(status)
	if err := json.NewEncoder(writer).Encode(body); err != nil {
		logger.Warn(ctx, logging.HTTPResponseWriteFailed, slog.String("error", err.Error()))
	}
}
