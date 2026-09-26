package revenue

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/customers"
	"github.com/preburn/preburn/internal/customerstate"
	"github.com/preburn/preburn/internal/database"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/ledger"
	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/revenue/queries"
)

const (
	periodMaximumDays            = 400
	periodMaximumDuration        = periodMaximumDays * 24 * time.Hour
	sourceReferenceMaximumLength = 200
	listingName                  = "revenue_entries"
	amountRule                   = "expected a non-negative amount"
	amountFormatRule             = "expected a USD amount with at most 9 decimals, such as 30.00"

	kindLocation            = "body.kind"
	amountLocation          = "body.amount"
	periodEndLocation       = "body.period_end"
	sourceReferenceLocation = "body.source_reference"
)

// Kind is the kind of a revenue entry, which sets the sign of its amount in
// net revenue.
type Kind string

const (
	// KindSubscription is revenue from a subscription. It adds to net revenue,
	// and its period is a billing period of the customer when it is not empty.
	KindSubscription Kind = "subscription"
	// KindAdjustment is any other revenue. It adds to net revenue.
	KindAdjustment Kind = "adjustment"
	// KindStripeFee is a payment processing fee. It subtracts from net
	// revenue.
	KindStripeFee Kind = "stripe_fee"
	// KindRefund is money returned to the customer. It subtracts from net
	// revenue.
	KindRefund Kind = "refund"
	// KindCreditNote is a credit issued against an invoice. It subtracts from
	// net revenue.
	KindCreditNote Kind = "credit_note"
)

// Source is where a revenue entry came from.
type Source string

const (
	// SourceAPI entries come from POST /api/v1/revenue.
	SourceAPI Source = "api"
	// SourceStripe entries come from the Stripe connector.
	SourceStripe Source = "stripe"
	// SourceImport entries come from a data import.
	SourceImport Source = "import"
)

// Entry is a recorded revenue entry.
type Entry struct {
	// ID is the entry's UUID, exposed with the prefix rev.
	ID uuid.UUID
	// Environment is the environment the entry belongs to.
	Environment httpapi.Environment
	// CustomerID is the UUID of the entry's customer.
	CustomerID uuid.UUID
	// CustomerExternalID is the id the operator's system gives the customer.
	CustomerExternalID string
	// Kind sets the sign of Amount in net revenue.
	Kind Kind
	// Amount is the non-negative amount of the entry.
	Amount money.Amount
	// PeriodStart is the start of the period the entry belongs to, in UTC.
	PeriodStart time.Time
	// PeriodEnd is the end of the period, in UTC. It equals PeriodStart for a
	// one-time line.
	PeriodEnd time.Time
	// Source is where the entry came from.
	Source Source
	// SourceReference is the id of the entry in its source, unique per
	// environment, source and kind.
	SourceReference string
	// OccurredAt is when the revenue was recognized, in UTC.
	OccurredAt time.Time
	// CreatedAt is when the entry was recorded, in UTC.
	CreatedAt time.Time
}

// RecordInput is a revenue entry for Service.Record.
type RecordInput struct {
	// CustomerExternalID names the customer by the id the operator's system
	// gives it, 1 to 128 letters, digits or the characters . _ : @ -.
	CustomerExternalID string
	// Kind is KindSubscription, KindAdjustment, KindStripeFee, KindRefund or
	// KindCreditNote.
	Kind Kind
	// Amount is not negative.
	Amount money.Amount
	// PeriodStart is the start of the period the entry belongs to.
	PeriodStart time.Time
	// PeriodEnd is no earlier than PeriodStart and at most 400 days after it.
	PeriodEnd time.Time
	// SourceReference is 1 to 200 characters.
	SourceReference string
	// OccurredAt is when the revenue was recognized, or nil for the time of
	// the call.
	OccurredAt *time.Time
}

// ListFilter narrows Service.List. An empty field keeps every entry.
type ListFilter struct {
	// CustomerExternalID keeps the entries of the customer with this external
	// id.
	CustomerExternalID string
	// Kind keeps the entries of this kind.
	Kind Kind
}

// Service records and lists revenue entries. Create one with NewService. It
// is safe for concurrent use.
type Service struct {
	pool      *pgxpool.Pool
	queries   *queries.Queries
	customers *customers.Cache
	states    *customerstate.Cache
	cache     *cache.Client
	clock     clock.Clock
}

type listPosition struct {
	CreatedAt      time.Time `json:"created_at"`
	RevenueEntryID uuid.UUID `json:"revenue_entry_id"`
}

var (
	listCursor          = httpapi.NewCursor[listPosition](listingName)
	kindRule            = fmt.Sprintf("expected %s, %s, %s, %s or %s", KindSubscription, KindAdjustment, KindStripeFee, KindRefund, KindCreditNote)
	periodRule          = fmt.Sprintf("expected a period end no earlier than the period start and at most %d days after it", periodMaximumDays)
	sourceReferenceRule = fmt.Sprintf("expected 1 to %d characters", sourceReferenceMaximumLength)
)

// NewService returns a Service that stores entries in pool, ensures customers
// through customerCache, removes changed customers from stateCache, publishes
// the customer invalidation through cacheClient and reads time from
// timeSource. stateCache must be the customer state cache the checks of the
// process read. NewService only stores its arguments, so zero values serve
// route registration for the OpenAPI document.
func NewService(pool *pgxpool.Pool, customerCache *customers.Cache, stateCache *customerstate.Cache, cacheClient *cache.Client, timeSource clock.Clock) *Service {
	return &Service{
		pool:      pool,
		queries:   queries.New(pool),
		customers: customerCache,
		states:    stateCache,
		cache:     cacheClient,
		clock:     timeSource,
	}
}

// Record stores input as a revenue entry of source in environment and returns
// it with duplicate false. It creates the customer input names when it is
// missing, so a new customer follows the default plan of environment. The
// transaction that inserts the entry also refreshes the customer's period
// rollups around the entry's period start with ledger.RefreshRollups. After
// the commit Record removes the customer from the service's customer state
// cache and publishes the customer invalidation, which clears it in the other
// processes. A failed publish returns its error with the entry committed, and
// a retry returns the entry as a duplicate.
//
// When environment already has an entry with source, input.Kind and
// input.SourceReference, Record changes nothing and returns that entry with
// duplicate true, even when its other fields differ from input. An input
// outside the rules of RecordInput returns a 422 validation_failed problem
// that names every invalid field, at body.kind, body.amount, body.period_end
// and body.source_reference, before the customer is looked up. An invalid
// external id then returns one at body.customer_id.
func (service *Service) Record(ctx context.Context, environment httpapi.Environment, source Source, input RecordInput) (Entry, bool, error) {
	if problems := recordProblems(input); len(problems) > 0 {
		return Entry{}, false, httpapi.NewValidationProblem(problems...)
	}
	customer, err := service.customers.Ensure(ctx, environment, input.CustomerExternalID)
	if err != nil {
		return Entry{}, false, err
	}
	now := service.clock.Now()
	occurredAt := now
	if input.OccurredAt != nil {
		occurredAt = *input.OccurredAt
	}
	parameters := queries.InsertRevenueEntryParams{
		RevenueEntryID:  identifiers.New(),
		Environment:     queries.Environment(environment),
		CustomerID:      customer.ID,
		PeriodStart:     input.PeriodStart,
		PeriodEnd:       input.PeriodEnd,
		Kind:            string(input.Kind),
		AmountNanos:     input.Amount,
		Source:          string(source),
		SourceReference: input.SourceReference,
		OccurredAt:      occurredAt,
		CreatedAt:       now,
	}
	var entry Entry
	var duplicate bool
	err = database.InTransaction(ctx, service.pool, func(ctx context.Context, transaction pgx.Tx) error {
		var err error
		entry, duplicate, err = insertEntry(ctx, transaction, parameters, customer.ExternalID)
		return err
	})
	if err != nil {
		return Entry{}, false, err
	}
	if duplicate {
		return entry, true, nil
	}
	invalidation := cache.Invalidation{
		Kind:        cache.InvalidationKindCustomer,
		Environment: string(environment),
		ID:          identifiers.Encode(identifiers.PrefixCustomer, customer.ID),
	}
	service.states.Invalidate(invalidation)
	if err := service.cache.PublishInvalidation(ctx, invalidation); err != nil {
		return Entry{}, false, err
	}
	return entry, false, nil
}

// List returns one page of the revenue entries of environment that match
// filter, newest first, and the cursor of the next page, which is empty on
// the last page. An empty cursor starts at the newest entry. A limit of 0
// selects httpapi.ListLimitDefault, and a limit outside 1 to
// httpapi.ListLimitMaximum returns a 422 validation_failed problem at
// query.limit. A cursor that List did not return for environment fails with
// httpapi.ErrInvalidCursor.
func (service *Service) List(ctx context.Context, environment httpapi.Environment, filter ListFilter, cursor string, limit int) ([]Entry, string, error) {
	pageSize, err := httpapi.ParseLimit(limit)
	if err != nil {
		return nil, "", err
	}
	parameters := queries.ListRevenueEntriesParams{Environment: queries.Environment(environment), RowLimit: int64(pageSize) + 1}
	if filter.CustomerExternalID != "" {
		parameters.CustomerExternalID = &filter.CustomerExternalID
	}
	if filter.Kind != "" {
		kind := string(filter.Kind)
		parameters.Kind = &kind
	}
	if cursor != "" {
		position, err := listCursor.Decode(environment, cursor)
		if err != nil {
			return nil, "", err
		}
		parameters.AfterCreatedAt = &position.CreatedAt
		parameters.AfterRevenueEntryID = &position.RevenueEntryID
	}
	rows, err := service.queries.ListRevenueEntries(ctx, parameters)
	if err != nil {
		return nil, "", fmt.Errorf("list revenue entries of environment %s: %w", environment, err)
	}
	listed := make([]Entry, 0, len(rows))
	for _, row := range rows {
		listed = append(listed, entryFromRow(row.RevenueEntry, row.ExternalID))
	}
	if len(listed) <= pageSize {
		return listed, "", nil
	}
	listed = listed[:pageSize]
	last := listed[pageSize-1]
	nextCursor, err := listCursor.Encode(environment, listPosition{CreatedAt: last.CreatedAt, RevenueEntryID: last.ID})
	if err != nil {
		return nil, "", err
	}
	return listed, nextCursor, nil
}

func insertEntry(ctx context.Context, transaction pgx.Tx, parameters queries.InsertRevenueEntryParams, customerExternalID string) (Entry, bool, error) {
	store := queries.New(transaction)
	row, err := store.InsertRevenueEntry(ctx, parameters)
	if errors.Is(err, pgx.ErrNoRows) {
		return existingEntry(ctx, store, parameters)
	}
	if err != nil {
		return Entry{}, false, fmt.Errorf("insert revenue entry: %w", err)
	}
	if err := ledger.RefreshRollups(ctx, transaction, httpapi.Environment(row.Environment), row.CustomerID, row.PeriodStart); err != nil {
		return Entry{}, false, fmt.Errorf("refresh rollups of customer %s: %w", row.CustomerID, err)
	}
	return entryFromRow(row, customerExternalID), false, nil
}

func existingEntry(ctx context.Context, store *queries.Queries, parameters queries.InsertRevenueEntryParams) (Entry, bool, error) {
	row, err := store.SelectRevenueEntryBySourceReference(ctx, queries.SelectRevenueEntryBySourceReferenceParams{
		Environment:     parameters.Environment,
		Source:          parameters.Source,
		Kind:            parameters.Kind,
		SourceReference: parameters.SourceReference,
	})
	if err != nil {
		return Entry{}, false, fmt.Errorf("select revenue entry by source reference: %w", err)
	}
	return entryFromRow(row.RevenueEntry, row.ExternalID), true, nil
}

func recordProblems(input RecordInput) []httpapi.ProblemError {
	var problems []httpapi.ProblemError
	if !input.Kind.valid() {
		problems = append(problems, httpapi.ProblemError{Location: kindLocation, Message: kindRule})
	}
	if input.Amount < 0 {
		problems = append(problems, httpapi.ProblemError{Location: amountLocation, Message: amountRule})
	}
	if input.PeriodEnd.Before(input.PeriodStart) || input.PeriodEnd.Sub(input.PeriodStart) > periodMaximumDuration {
		problems = append(problems, httpapi.ProblemError{Location: periodEndLocation, Message: periodRule})
	}
	if length := utf8.RuneCountInString(input.SourceReference); length < 1 || length > sourceReferenceMaximumLength {
		problems = append(problems, httpapi.ProblemError{Location: sourceReferenceLocation, Message: sourceReferenceRule})
	}
	return problems
}

func (kind Kind) valid() bool {
	switch kind {
	case KindSubscription, KindAdjustment, KindStripeFee, KindRefund, KindCreditNote:
		return true
	}
	return false
}

func entryFromRow(row queries.RevenueEntry, customerExternalID string) Entry {
	return Entry{
		ID:                 row.RevenueEntryID,
		Environment:        httpapi.Environment(row.Environment),
		CustomerID:         row.CustomerID,
		CustomerExternalID: customerExternalID,
		Kind:               Kind(row.Kind),
		Amount:             row.AmountNanos,
		PeriodStart:        row.PeriodStart.UTC(),
		PeriodEnd:          row.PeriodEnd.UTC(),
		Source:             Source(row.Source),
		SourceReference:    row.SourceReference,
		OccurredAt:         row.OccurredAt.UTC(),
		CreatedAt:          row.CreatedAt.UTC(),
	}
}
