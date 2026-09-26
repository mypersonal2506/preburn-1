package decisions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/preburn/preburn/internal/database"
	"github.com/preburn/preburn/internal/decisions/queries"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/ledger"
	"github.com/preburn/preburn/internal/logging"
)

const decisionStatusReleased = "released"

// ReleaseResult is the decision a release left behind: the body of the 200
// answer to POST /api/v1/release.
type ReleaseResult struct {
	DecisionID string `json:"decision_id" doc:"Decision id, such as dec_01jbvagescfn78y0938nkrkayd."`
	Status     string `json:"status" enum:"released,settled,expired,unreserved" doc:"Status of the decision after the release: released when this or an earlier release freed its reservation, settled when a report recorded its usage, expired when its hold time passed first, unreserved when it was denied and reserved nothing."`
}

// Release frees the reservation of the decision with decisionID in
// environment. It marks a reserved decision released and inserts the rollup
// refresh job of the decision's customer period, since a released decision
// was never reported, in one transaction. After the commit the release
// script frees the reservation with status released. The script runs again
// for a decision that is already released, where it changes nothing unless
// an earlier release could not reach Redis. A decision in any other status is
// left alone. The result holds the decision's status afterwards, so a
// repeated release returns the same result. A Redis failure or a missing
// counters_ready marker logs decisions.release_deferred and Release still
// succeeds, because counters_reconcile or the expiry job frees the
// reservation. A decision missing from environment returns
// httpapi.ErrNotFound.
func (service *ReportService) Release(ctx context.Context, environment httpapi.Environment, decisionID uuid.UUID) (ReleaseResult, error) {
	var status string
	err := database.InTransaction(ctx, service.pool, func(ctx context.Context, transaction pgx.Tx) error {
		store := service.queries.WithTx(transaction)
		decided, err := store.LockDecision(ctx, queries.LockDecisionParams{Environment: queries.Environment(environment), DecisionID: decisionID})
		if errors.Is(err, pgx.ErrNoRows) {
			return httpapi.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock decision %s: %w", decisionID, err)
		}
		status = decided.Status
		if status != decisionStatusReserved {
			return nil
		}
		if err := store.MarkDecisionReleased(ctx, decisionID); err != nil {
			return fmt.Errorf("mark decision %s released: %w", decisionID, err)
		}
		refresh := ledger.NewRollupRefreshArgs(environment, decided.CustomerID, decided.PeriodStart)
		if _, err := service.jobs.InsertTx(ctx, transaction, refresh, nil); err != nil {
			return fmt.Errorf("insert rollup refresh: %w", err)
		}
		status = decisionStatusReleased
		return nil
	})
	if err != nil {
		return ReleaseResult{}, err
	}
	if status == decisionStatusReleased {
		service.releaseReservation(ctx, environment, decisionID)
	}
	return ReleaseResult{DecisionID: identifiers.Encode(identifiers.PrefixDecision, decisionID), Status: status}, nil
}

func (service *ReportService) releaseReservation(ctx context.Context, environment httpapi.Environment, decisionID uuid.UUID) {
	countersContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), countersTimeout)
	defer cancel()
	if _, err := service.counters.Release(countersContext, decisionID, ReservationStatusReleased); err != nil {
		service.logger.Warn(ctx, logging.DecisionsReleaseDeferred,
			slog.String("environment", string(environment)),
			slog.String("decision_id", identifiers.Encode(identifiers.PrefixDecision, decisionID)),
			slog.String("error", err.Error()),
		)
	}
}
