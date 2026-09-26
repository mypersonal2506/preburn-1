package dashboard

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preburn/preburn/internal/dashboard/queries"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/policies"
)

const eventListingName = "dashboard_events"

// EventKind is the record behind an event of the dashboard event list.
type EventKind string

const (
	// EventKindDecision is a decision, at the time of its check.
	EventKindDecision EventKind = "decision"
	// EventKindLedgerEntry is a ledger entry, at the time its request ran.
	EventKindLedgerEntry EventKind = "ledger_entry"
)

// EventService reads the dashboard event list: recent decisions and ledger
// entries merged by time. Create one with NewEventService. It is safe for
// concurrent use.
type EventService struct {
	queries *queries.Queries
}

type eventCursorKey struct {
	OccurredAt time.Time `json:"occurred_at"`
	ID         uuid.UUID `json:"id"`
}

type activityEvent struct {
	occurredAt time.Time
	id         uuid.UUID
	response   ActivityEventResponse
}

var eventCursor = httpapi.NewCursor[eventCursorKey](eventListingName)

// NewEventService returns an EventService that reads pool. It only stores
// its argument, so a nil pool serves route registration for the OpenAPI
// document.
func NewEventService(pool *pgxpool.Pool) *EventService {
	return &EventService{queries: queries.New(pool)}
}

// List returns one page of the decisions and ledger entries of environment,
// newest first, and the cursor of the next page, which is empty on the last
// page. A decision takes the time of its check and a ledger entry the time
// its request ran, and events of the same time order by id from the highest.
// A limit of 0 selects httpapi.ListLimitDefault, and a limit outside 1 to
// httpapi.ListLimitMaximum returns a 422 validation_failed problem at
// query.limit. A cursor that List did not return for environment fails with
// httpapi.ErrInvalidCursor.
func (service *EventService) List(ctx context.Context, environment httpapi.Environment, cursor string, limit int) ([]ActivityEventResponse, string, error) {
	pageSize, err := httpapi.ParseLimit(limit)
	if err != nil {
		return nil, "", err
	}
	storeEnvironment := queries.Environment(environment)
	decisionParameters := queries.ListDecisionsParams{Environment: storeEnvironment, RowLimit: int64(pageSize) + 1}
	ledgerParameters := queries.ListLedgerEntryEventsParams{Environment: storeEnvironment, RowLimit: int64(pageSize) + 1}
	if cursor != "" {
		key, err := eventCursor.Decode(environment, cursor)
		if err != nil {
			return nil, "", err
		}
		decisionParameters.BeforeCreatedAt, decisionParameters.BeforeDecisionID = &key.OccurredAt, &key.ID
		ledgerParameters.BeforeOccurredAt, ledgerParameters.BeforeLedgerEntryID = &key.OccurredAt, &key.ID
	}
	decisionRows, err := service.queries.ListDecisions(ctx, decisionParameters)
	if err != nil {
		return nil, "", fmt.Errorf("list decision events of environment %s: %w", environment, err)
	}
	ledgerRows, err := service.queries.ListLedgerEntryEvents(ctx, ledgerParameters)
	if err != nil {
		return nil, "", fmt.Errorf("list ledger entry events of environment %s: %w", environment, err)
	}
	events := make([]activityEvent, 0, len(decisionRows)+len(ledgerRows))
	for _, row := range decisionRows {
		events = append(events, newDecisionEvent(row))
	}
	for _, row := range ledgerRows {
		events = append(events, newLedgerEntryEvent(row))
	}
	slices.SortFunc(events, func(left, right activityEvent) int {
		return cmp.Or(right.occurredAt.Compare(left.occurredAt), bytes.Compare(right.id[:], left.id[:]))
	})
	page := events[:min(len(events), pageSize)]
	responses := make([]ActivityEventResponse, len(page))
	for index, event := range page {
		responses[index] = event.response
	}
	if len(events) <= pageSize {
		return responses, "", nil
	}
	last := page[len(page)-1]
	nextCursor, err := eventCursor.Encode(environment, eventCursorKey{OccurredAt: last.occurredAt, ID: last.id})
	if err != nil {
		return nil, "", err
	}
	return responses, nextCursor, nil
}

func newDecisionEvent(row queries.ListDecisionsRow) activityEvent {
	decision := row.Decision
	outcome := policies.Outcome(decision.Outcome)
	return activityEvent{
		occurredAt: decision.CreatedAt,
		id:         decision.DecisionID,
		response: ActivityEventResponse{
			Kind:                EventKindDecision,
			ID:                  identifiers.Encode(identifiers.PrefixDecision, decision.DecisionID),
			OccurredAt:          decision.CreatedAt.UTC(),
			CustomerID:          identifiers.Encode(identifiers.PrefixCustomer, decision.CustomerID),
			CustomerExternalID:  row.ExternalID,
			CustomerDisplayName: row.DisplayName,
			Feature:             decision.Feature,
			Provider:            decision.Provider,
			Model:               decision.Model,
			Outcome:             &outcome,
			Cost:                formatOptionalAmount(decision.EstimatedCostNanos),
		},
	}
}

func newLedgerEntryEvent(row queries.ListLedgerEntryEventsRow) activityEvent {
	event := activityEvent{
		occurredAt: row.OccurredAt,
		id:         row.LedgerEntryID,
		response: ActivityEventResponse{
			Kind:                EventKindLedgerEntry,
			ID:                  identifiers.Encode(identifiers.PrefixLedgerEntry, row.LedgerEntryID),
			OccurredAt:          row.OccurredAt.UTC(),
			CustomerID:          identifiers.Encode(identifiers.PrefixCustomer, row.CustomerID),
			CustomerExternalID:  row.ExternalID,
			CustomerDisplayName: row.DisplayName,
			Feature:             row.Feature,
			Provider:            row.Provider,
			Model:               row.Model,
			Cost:                formatOptionalAmount(row.CostNanos),
		},
	}
	if row.DecisionID != nil {
		decisionID := identifiers.Encode(identifiers.PrefixDecision, *row.DecisionID)
		event.response.DecisionID = &decisionID
	}
	return event
}
