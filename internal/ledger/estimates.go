package ledger

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preburn/preburn/internal/database"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/ledger/queries"
)

const (
	usageEstimateWindow         = 30 * 24 * time.Hour
	usageEstimateMinimumSamples = 50
)

func refreshUsageEstimates(ctx context.Context, pool *pgxpool.Pool, environment httpapi.Environment, now time.Time) error {
	return database.InTransaction(ctx, pool, func(ctx context.Context, transaction pgx.Tx) error {
		store := queries.New(transaction)
		if err := store.DeleteUsageEstimates(ctx, queries.Environment(environment)); err != nil {
			return fmt.Errorf("delete usage estimates: %w", err)
		}
		err := store.InsertUsageEstimates(ctx, queries.InsertUsageEstimatesParams{
			Environment:        queries.Environment(environment),
			Since:              now.Add(-usageEstimateWindow),
			MinimumSampleCount: usageEstimateMinimumSamples,
			RefreshedAt:        now,
		})
		if err != nil {
			return fmt.Errorf("insert usage estimates: %w", err)
		}
		return nil
	})
}
