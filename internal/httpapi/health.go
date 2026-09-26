package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/preburn/preburn/internal/logging"
)

const readinessCheckTimeout = 2 * time.Second

// ReadinessCheck is one dependency that /readyz verifies, such as the
// database. Check returns nil when the dependency is usable. Its context ends
// after 2 seconds.
type ReadinessCheck struct {
	Name  string
	Check func(ctx context.Context) error
}

type healthResponse struct {
	Status string `json:"status"`
}

type readinessResponse struct {
	Status        string   `json:"status"`
	FailingChecks []string `json:"failing_checks"`
}

// RegisterHealthRoutes adds the health routes to mux. GET /healthz always
// answers 200. GET /readyz runs checks at the same time, answers 200 when all
// pass and 503 when any fails, and names the failing checks in
// failing_checks. Each failure is logged as http.readiness_check_failed.
func RegisterHealthRoutes(mux *http.ServeMux, logger *logging.Logger, checks []ReadinessCheck) {
	mux.HandleFunc("GET /healthz", func(writer http.ResponseWriter, request *http.Request) {
		writeJSON(request.Context(), logger, writer, jsonContentType, http.StatusOK, healthResponse{Status: "ok"})
	})
	mux.HandleFunc("GET /readyz", func(writer http.ResponseWriter, request *http.Request) {
		failing := failingChecks(request.Context(), logger, checks)
		status, readiness := http.StatusOK, "ready"
		if len(failing) > 0 {
			status, readiness = http.StatusServiceUnavailable, "not_ready"
		}
		writeJSON(request.Context(), logger, writer, jsonContentType, status, readinessResponse{Status: readiness, FailingChecks: failing})
	})
}

func failingChecks(ctx context.Context, logger *logging.Logger, checks []ReadinessCheck) []string {
	failed := make([]bool, len(checks))
	var running sync.WaitGroup
	for index, check := range checks {
		running.Go(func() {
			checkContext, cancel := context.WithTimeout(ctx, readinessCheckTimeout)
			defer cancel()
			if err := check.Check(checkContext); err != nil {
				logger.Warn(ctx, logging.HTTPReadinessCheckFailed, slog.String("check", check.Name), slog.String("error", err.Error()))
				failed[index] = true
			}
		})
	}
	running.Wait()
	failing := []string{}
	for index, check := range checks {
		if failed[index] {
			failing = append(failing, check.Name)
		}
	}
	return failing
}
