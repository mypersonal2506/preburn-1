package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preburn/preburn/internal/dashboard/queries"
	"github.com/preburn/preburn/internal/decisions"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/policies"
	"github.com/preburn/preburn/internal/pricing"
)

const decisionListingName = "dashboard_decisions"

// DecisionFilter selects the decisions of the decision list. Each nil or
// empty field keeps every decision.
type DecisionFilter struct {
	// Outcome keeps the decisions with this outcome.
	Outcome *policies.Outcome
	// CustomerID keeps the decisions of this customer.
	CustomerID *uuid.UUID
	// Feature keeps the decisions for this feature.
	Feature string
	// PolicyID keeps the decisions this policy decided.
	PolicyID *uuid.UUID
}

// DecisionService reads the decision list and the decision details of the
// dashboard. Create one with NewDecisionService. It is safe for concurrent
// use.
type DecisionService struct {
	queries *queries.Queries
}

type decisionCursorKey struct {
	CreatedAt  time.Time `json:"created_at"`
	DecisionID uuid.UUID `json:"decision_id"`
}

var decisionCursor = httpapi.NewCursor[decisionCursorKey](decisionListingName)

// NewDecisionService returns a DecisionService that reads pool. It only
// stores its argument, so a nil pool serves route registration for the
// OpenAPI document.
func NewDecisionService(pool *pgxpool.Pool) *DecisionService {
	return &DecisionService{queries: queries.New(pool)}
}

// List returns one page of the decisions of environment that filter keeps,
// newest first with ties in decision id order from the highest, and the
// cursor of the next page, which is empty on the last page. A limit of 0
// selects httpapi.ListLimitDefault, and a limit outside 1 to
// httpapi.ListLimitMaximum returns a 422 validation_failed problem at
// query.limit. A cursor that List did not return for environment fails with
// httpapi.ErrInvalidCursor.
func (service *DecisionService) List(ctx context.Context, environment httpapi.Environment, filter DecisionFilter, cursor string, limit int) ([]DecisionResponse, string, error) {
	pageSize, err := httpapi.ParseLimit(limit)
	if err != nil {
		return nil, "", err
	}
	parameters := queries.ListDecisionsParams{
		Environment: queries.Environment(environment),
		CustomerID:  filter.CustomerID,
		PolicyID:    filter.PolicyID,
		RowLimit:    int64(pageSize) + 1,
	}
	if filter.Outcome != nil {
		outcome := string(*filter.Outcome)
		parameters.Outcome = &outcome
	}
	if filter.Feature != "" {
		parameters.Feature = &filter.Feature
	}
	if cursor != "" {
		key, err := decisionCursor.Decode(environment, cursor)
		if err != nil {
			return nil, "", err
		}
		parameters.BeforeCreatedAt, parameters.BeforeDecisionID = &key.CreatedAt, &key.DecisionID
	}
	rows, err := service.queries.ListDecisions(ctx, parameters)
	if err != nil {
		return nil, "", fmt.Errorf("list decisions of environment %s: %w", environment, err)
	}
	page := rows[:min(len(rows), pageSize)]
	responses := make([]DecisionResponse, len(page))
	for index, row := range page {
		responses[index] = newDecisionResponse(row.Decision, row.ExternalID, row.DisplayName)
	}
	if len(rows) <= pageSize {
		return responses, "", nil
	}
	last := page[len(page)-1].Decision
	nextCursor, err := decisionCursor.Encode(environment, decisionCursorKey{CreatedAt: last.CreatedAt, DecisionID: last.DecisionID})
	if err != nil {
		return nil, "", err
	}
	return responses, nextCursor, nil
}

// Decision returns the decision with decisionID in environment with the
// attributes, overrides and signals the check stored, its lifecycle times,
// and the ledger entries of its usage: the entry its report recorded and the
// corrections that price that entry, oldest first. It returns
// httpapi.ErrNotFound when environment has no such decision.
func (service *DecisionService) Decision(ctx context.Context, environment httpapi.Environment, decisionID uuid.UUID) (DecisionDetailResponse, error) {
	row, err := service.queries.SelectDecision(ctx, queries.SelectDecisionParams{Environment: queries.Environment(environment), DecisionID: decisionID})
	if errors.Is(err, pgx.ErrNoRows) {
		return DecisionDetailResponse{}, httpapi.ErrNotFound
	}
	if err != nil {
		return DecisionDetailResponse{}, fmt.Errorf("select decision %s: %w", decisionID, err)
	}
	decision := row.Decision
	detail := DecisionDetailResponse{
		DecisionResponse:       newDecisionResponse(decision, row.ExternalID, row.DisplayName),
		CustomerUserExternalID: row.CustomerUserExternalID,
		RequestedEstimatedCost: formatOptionalAmount(decision.RequestedEstimatedCostNanos),
		ReservedAmount:         money.FormatAmount(decision.ReservedNanos),
		EstimateBasis:          decisions.EstimateBasis(decision.EstimateBasis),
		PeriodStart:            decision.PeriodStart.UTC(),
		PeriodEnd:              decision.PeriodEnd.UTC(),
		ExpiresAt:              decision.ExpiresAt.UTC(),
	}
	if decision.MatchedPolicyVersion != nil {
		version := int64(*decision.MatchedPolicyVersion)
		detail.MatchedPolicyVersion = &version
	}
	if decision.SettledAt != nil {
		settledAt := decision.SettledAt.UTC()
		detail.SettledAt = &settledAt
	}
	if err := json.Unmarshal(decision.Attributes, &detail.Attributes); err != nil {
		return DecisionDetailResponse{}, fmt.Errorf("decode attributes of decision %s: %w", decisionID, err)
	}
	if err := json.Unmarshal(decision.Overrides, &detail.Overrides); err != nil {
		return DecisionDetailResponse{}, fmt.Errorf("decode overrides of decision %s: %w", decisionID, err)
	}
	if err := json.Unmarshal(decision.Signals, &detail.Signals); err != nil {
		return DecisionDetailResponse{}, fmt.Errorf("decode signals of decision %s: %w", decisionID, err)
	}
	if detail.LedgerEntries, err = service.ledgerEntries(ctx, environment, decision); err != nil {
		return DecisionDetailResponse{}, err
	}
	return detail, nil
}

func (service *DecisionService) ledgerEntries(ctx context.Context, environment httpapi.Environment, decision queries.Decision) ([]DecisionLedgerEntryResponse, error) {
	rows, err := service.queries.ListDecisionLedgerEntries(ctx, queries.ListDecisionLedgerEntriesParams{
		Environment: queries.Environment(environment),
		CustomerID:  decision.CustomerID,
		PeriodStart: decision.PeriodStart,
		DecisionID:  decision.DecisionID,
	})
	if err != nil {
		return nil, fmt.Errorf("list ledger entries of decision %s: %w", decision.DecisionID, err)
	}
	entries := make([]DecisionLedgerEntryResponse, len(rows))
	for index, row := range rows {
		var usage map[string]money.Quantity
		if err := json.Unmarshal(row.Usage, &usage); err != nil {
			return nil, fmt.Errorf("decode usage of ledger entry %s: %w", row.LedgerEntryID, err)
		}
		entries[index] = DecisionLedgerEntryResponse{
			ID:         identifiers.Encode(identifiers.PrefixLedgerEntry, row.LedgerEntryID),
			Provider:   row.Provider,
			Model:      row.Model,
			Usage:      make(map[string]string, len(usage)),
			Cost:       formatOptionalAmount(row.CostNanos),
			CostStatus: pricing.CostStatus(row.CostStatus),
			OccurredAt: row.OccurredAt.UTC(),
			CreatedAt:  row.CreatedAt.UTC(),
		}
		for meter, quantity := range usage {
			entries[index].Usage[meter] = money.FormatQuantity(quantity)
		}
		if row.CorrectionOf != nil {
			correctionOf := identifiers.Encode(identifiers.PrefixLedgerEntry, *row.CorrectionOf)
			entries[index].CorrectionOf = &correctionOf
		}
	}
	return entries, nil
}

func newDecisionResponse(decision queries.Decision, customerExternalID string, customerDisplayName *string) DecisionResponse {
	response := DecisionResponse{
		CustomerDecisionResponse: CustomerDecisionResponse{
			ID:                identifiers.Encode(identifiers.PrefixDecision, decision.DecisionID),
			Feature:           decision.Feature,
			RequestedProvider: decision.RequestedProvider,
			RequestedModel:    decision.RequestedModel,
			Provider:          decision.Provider,
			Model:             decision.Model,
			Outcome:           policies.Outcome(decision.Outcome),
			Reason:            policies.Reason(decision.Reason),
			EstimatedCost:     formatOptionalAmount(decision.EstimatedCostNanos),
			Status:            decision.Status,
			CreatedAt:         decision.CreatedAt.UTC(),
		},
		CustomerID:          identifiers.Encode(identifiers.PrefixCustomer, decision.CustomerID),
		CustomerExternalID:  customerExternalID,
		CustomerDisplayName: customerDisplayName,
	}
	if decision.MatchedPolicyID != nil {
		policyID := identifiers.Encode(identifiers.PrefixPolicy, *decision.MatchedPolicyID)
		response.MatchedPolicyID = &policyID
	}
	return response
}

func formatOptionalAmount(amount *money.Amount) *string {
	if amount == nil {
		return nil
	}
	formatted := money.FormatAmount(*amount)
	return &formatted
}
