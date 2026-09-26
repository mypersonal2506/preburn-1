package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/ledger/queries"
	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/pricing"
)

// DecisionSource says who decided the request a ledger entry records.
type DecisionSource string

const (
	// DecisionSourceServer marks the usage of a decision that a check made.
	DecisionSourceServer DecisionSource = "server"
	// DecisionSourceFallback marks usage that ran without a decision, such as
	// on the fallback outcome while Preburn was unreachable.
	DecisionSourceFallback DecisionSource = "fallback"
)

// Entry is a ledger entry: the usage of one provider request, its cost and
// the customer period it counts in.
type Entry struct {
	// ID is the entry's UUID, exposed with the prefix led.
	ID uuid.UUID
	// Environment is the environment the entry belongs to.
	Environment httpapi.Environment
	// CustomerID is the customer whose usage the entry records.
	CustomerID uuid.UUID
	// CustomerUserID is the customer user of the request, or nil.
	CustomerUserID *uuid.UUID
	// DecisionID is the decision the usage belongs to, or nil for usage that
	// ran without a decision.
	DecisionID *uuid.UUID
	// IdempotencyKey is unique per environment: decision:<decision UUID> for
	// the usage of a decision, the client's UUID for other usage.
	IdempotencyKey string
	// Feature is the feature the request served.
	Feature string
	// Provider is the provider the request ran on.
	Provider string
	// Model is the model the request ran on.
	Model string
	// Attributes are the attributes the usage was rated with. It is never
	// nil.
	Attributes pricing.Attributes
	// Usage is the quantity of each meter the request used. It is never nil.
	Usage map[pricing.Meter]money.Quantity
	// Rating is the priced usage: the cost status, the cost and one line per
	// priced or missing meter.
	Rating pricing.RatedRequest
	// DecisionSource says who decided the request.
	DecisionSource DecisionSource
	// PeriodStart is the start of the customer period the usage counts in.
	PeriodStart time.Time
	// PeriodEnd is the end of that customer period.
	PeriodEnd time.Time
	// CorrectionOf is the uncosted entry this entry prices, or nil.
	CorrectionOf *uuid.UUID
	// OccurredAt is when the request ran.
	OccurredAt time.Time
	// CreatedAt is when the entry was recorded.
	CreatedAt time.Time
}

type costBreakdownDocument struct {
	Lines []costLineDocument `json:"lines"`
}

type costLineDocument struct {
	Meter             pricing.Meter  `json:"meter"`
	QuantityMicros    money.Quantity `json:"quantity_micros"`
	UnitPriceNanos    money.Amount   `json:"unit_price_nanos"`
	UnitQuantity      int64          `json:"unit_quantity"`
	CostNanos         money.Amount   `json:"cost_nanos"`
	PricingRuleID     *uuid.UUID     `json:"pricing_rule_id"`
	PricingOverrideID *uuid.UUID     `json:"pricing_override_id"`
	Missing           bool           `json:"missing"`
}

// InsertEntry stores entry inside transaction and returns it as stored with
// duplicate false. When environment already holds an entry with
// entry.IdempotencyKey, InsertEntry changes nothing and returns that entry
// with duplicate true, even when its other fields differ from entry.
//
// The usage column maps each meter to its quantity in micro-units as a JSON
// integer. The cost_breakdown column holds {"lines": [...]} with one object
// per line of entry.Rating: meter, quantity_micros, unit_price_nanos,
// unit_quantity, cost_nanos, pricing_rule_id, pricing_override_id and
// missing, the prices and cost 0 on a missing line. cost_nanos is null when
// the rating is uncosted.
func InsertEntry(ctx context.Context, transaction pgx.Tx, entry Entry) (Entry, bool, error) {
	parameters, err := insertEntryParameters(entry)
	if err != nil {
		return Entry{}, false, err
	}
	store := queries.New(transaction)
	row, err := store.InsertLedgerEntry(ctx, parameters)
	if errors.Is(err, pgx.ErrNoRows) {
		existing, err := store.SelectLedgerEntryByIdempotencyKey(ctx, queries.SelectLedgerEntryByIdempotencyKeyParams{
			Environment:    parameters.Environment,
			IdempotencyKey: parameters.IdempotencyKey,
		})
		if err != nil {
			return Entry{}, false, fmt.Errorf("select ledger entry by idempotency key: %w", err)
		}
		stored, err := entryFromRow(existing)
		return stored, true, err
	}
	if err != nil {
		return Entry{}, false, fmt.Errorf("insert ledger entry: %w", err)
	}
	stored, err := entryFromRow(row)
	return stored, false, err
}

func insertEntryParameters(entry Entry) (queries.InsertLedgerEntryParams, error) {
	attributes, err := json.Marshal(entry.Attributes)
	if err != nil {
		return queries.InsertLedgerEntryParams{}, fmt.Errorf("encode attributes: %w", err)
	}
	usage, err := json.Marshal(entry.Usage)
	if err != nil {
		return queries.InsertLedgerEntryParams{}, fmt.Errorf("encode usage: %w", err)
	}
	breakdown := costBreakdownDocument{Lines: make([]costLineDocument, 0, len(entry.Rating.Lines))}
	for _, line := range entry.Rating.Lines {
		breakdown.Lines = append(breakdown.Lines, costLineDocument{
			Meter:             line.Meter,
			QuantityMicros:    line.Quantity,
			UnitPriceNanos:    line.UnitPrice.Nanos,
			UnitQuantity:      line.UnitPrice.UnitQuantity,
			CostNanos:         line.Cost,
			PricingRuleID:     line.RuleID,
			PricingOverrideID: line.OverrideID,
			Missing:           line.Missing,
		})
	}
	costBreakdown, err := json.Marshal(breakdown)
	if err != nil {
		return queries.InsertLedgerEntryParams{}, fmt.Errorf("encode cost breakdown: %w", err)
	}
	return queries.InsertLedgerEntryParams{
		LedgerEntryID:  entry.ID,
		Environment:    queries.Environment(entry.Environment),
		CustomerID:     entry.CustomerID,
		CustomerUserID: entry.CustomerUserID,
		DecisionID:     entry.DecisionID,
		IdempotencyKey: entry.IdempotencyKey,
		Feature:        entry.Feature,
		Provider:       entry.Provider,
		Model:          entry.Model,
		Attributes:     attributes,
		Usage:          usage,
		CostNanos:      (*int64)(entry.Rating.Cost),
		CostBreakdown:  costBreakdown,
		CostStatus:     string(entry.Rating.CostStatus),
		DecisionSource: string(entry.DecisionSource),
		PeriodStart:    entry.PeriodStart,
		PeriodEnd:      entry.PeriodEnd,
		CorrectionOf:   entry.CorrectionOf,
		OccurredAt:     entry.OccurredAt,
		CreatedAt:      entry.CreatedAt,
	}, nil
}

func entryFromRow(row queries.LedgerEntry) (Entry, error) {
	var attributes pricing.Attributes
	if err := json.Unmarshal(row.Attributes, &attributes); err != nil {
		return Entry{}, fmt.Errorf("decode attributes of ledger entry %s: %w", row.LedgerEntryID, err)
	}
	var usage map[pricing.Meter]money.Quantity
	if err := json.Unmarshal(row.Usage, &usage); err != nil {
		return Entry{}, fmt.Errorf("decode usage of ledger entry %s: %w", row.LedgerEntryID, err)
	}
	var breakdown costBreakdownDocument
	if err := json.Unmarshal(row.CostBreakdown, &breakdown); err != nil {
		return Entry{}, fmt.Errorf("decode cost breakdown of ledger entry %s: %w", row.LedgerEntryID, err)
	}
	rating := pricing.RatedRequest{
		CostStatus: pricing.CostStatus(row.CostStatus),
		Cost:       (*money.Amount)(row.CostNanos),
		Lines:      make([]pricing.RatedLine, 0, len(breakdown.Lines)),
	}
	for _, line := range breakdown.Lines {
		rating.Lines = append(rating.Lines, pricing.RatedLine{
			Meter:      line.Meter,
			Quantity:   line.QuantityMicros,
			UnitPrice:  money.UnitPrice{Nanos: line.UnitPriceNanos, UnitQuantity: line.UnitQuantity},
			Cost:       line.CostNanos,
			RuleID:     line.PricingRuleID,
			OverrideID: line.PricingOverrideID,
			Missing:    line.Missing,
		})
	}
	return Entry{
		ID:             row.LedgerEntryID,
		Environment:    httpapi.Environment(row.Environment),
		CustomerID:     row.CustomerID,
		CustomerUserID: row.CustomerUserID,
		DecisionID:     row.DecisionID,
		IdempotencyKey: row.IdempotencyKey,
		Feature:        row.Feature,
		Provider:       row.Provider,
		Model:          row.Model,
		Attributes:     attributes,
		Usage:          usage,
		Rating:         rating,
		DecisionSource: DecisionSource(row.DecisionSource),
		PeriodStart:    row.PeriodStart.UTC(),
		PeriodEnd:      row.PeriodEnd.UTC(),
		CorrectionOf:   row.CorrectionOf,
		OccurredAt:     row.OccurredAt.UTC(),
		CreatedAt:      row.CreatedAt.UTC(),
	}, nil
}
