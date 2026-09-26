package jobs

import "github.com/riverqueue/river"

// Queue names. A worker client works every queue, each with its own fixed
// number of workers.
const (
	// QueueDefault holds short frequent jobs such as reservation expiry and
	// counter reconciliation.
	QueueDefault = "default"
	// QueueLedger holds rollup refreshes, usage estimates and re-rating.
	QueueLedger = "ledger"
	// QueueStripe holds Stripe event processing, syncs, backfills and price
	// refreshes.
	QueueStripe = "stripe"
	// QueueWebhooks holds webhook fanout and delivery.
	QueueWebhooks = "webhooks"
	// QueueSimulations holds simulation runs, one at a time.
	QueueSimulations = "simulations"
	// QueueImports holds demo data imports and removals, one at a time.
	QueueImports = "imports"
	// QueueMaintenance holds retention, cleanup and price catalog refreshes.
	QueueMaintenance = "maintenance"
)

func queueConfigurations() map[string]river.QueueConfig {
	return map[string]river.QueueConfig{
		QueueDefault:     {MaxWorkers: 10},
		QueueLedger:      {MaxWorkers: 10},
		QueueStripe:      {MaxWorkers: 4},
		QueueWebhooks:    {MaxWorkers: 10},
		QueueSimulations: {MaxWorkers: 1},
		QueueImports:     {MaxWorkers: 1},
		QueueMaintenance: {MaxWorkers: 2},
	}
}
