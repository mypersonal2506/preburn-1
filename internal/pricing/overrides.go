package pricing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/database"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/pricing/queries"
)

const (
	nativeUnitMaximumLength = 40
	overrideListingName     = "pricing_overrides"

	typeLocation             = "body.type"
	meterLocation            = "body.meter"
	conditionsLocation       = "body.conditions"
	unitPriceLocation        = "body.unit_price"
	unitQuantityLocation     = "body.unit_quantity"
	nativeUnitLocation       = "body.native_unit"
	nativeUnitPriceLocation  = "body.native_unit_price"
	minimumChargeLocation    = "body.minimum_charge"
	billingIncrementLocation = "body.billing_increment"
	effectiveToLocation      = "body.effective_to"
)

const (
	// OverrideTypeAdjustment marks an override whose provider, model, meter
	// and conditions equal an open catalog rule, whose price it replaces.
	OverrideTypeAdjustment OverrideType = "adjustment"
	// OverrideTypeStandalone marks an override that no open catalog rule
	// equals, matched before the catalog as a rule of its own.
	OverrideTypeStandalone OverrideType = "standalone"
)

// OverrideType tells how an override prices. It is derived from the open
// catalog rules, never stored, so a standalone override becomes an
// adjustment once the catalog gains an equal rule.
type OverrideType string

// OverrideRecord is a stored override with its derived type, its USD price
// and its timestamps.
type OverrideRecord struct {
	// Override holds the stored fields of the override.
	Override
	// Type is the override's type against the open catalog rules.
	Type OverrideType
	// EffectiveUnitPrice is the USD price, as Override.EffectiveUnitPrice
	// returns it.
	EffectiveUnitPrice money.UnitPrice
	// CreatedAt is when the override was created, in UTC.
	CreatedAt time.Time
	// UpdatedAt is when the override last changed, in UTC.
	UpdatedAt time.Time
}

// CreateOverrideInput is a new override for Service.CreateOverride. Prices
// hold API decimal strings and quantities API quantity strings.
type CreateOverrideInput struct {
	// Type, when set, is the type the caller expects. Nil lets the service
	// derive it.
	Type *OverrideType
	// Provider is the provider name, 1 to 200 characters.
	Provider string
	// Model is the model name or one of its aliases, 1 to 200 characters.
	// An alias is stored as the model it names.
	Model string
	// Meter is the name of the meter the override prices.
	Meter string
	// Conditions are the attributes a request must contain. Known keys
	// follow the attribute vocabulary. Nil means none.
	Conditions Attributes
	// UnitPrice is the non-negative price of UnitQuantity units in USD, or in
	// the native unit when NativeUnit is set, such as "0.12".
	UnitPrice string
	// UnitQuantity is the number of whole units UnitPrice pays for, 1 or
	// more.
	UnitQuantity int64
	// NativeUnit is the label of the unit UnitPrice is expressed in, such as
	// credits, 1 to 40 characters. It needs NativeUnitPrice.
	NativeUnit *string
	// NativeUnitPrice is the non-negative USD price of one native unit. It
	// needs NativeUnit.
	NativeUnitPrice *string
	// MinimumCharge is the non-negative lowest USD cost of a meter line.
	MinimumCharge *string
	// BillingIncrement is the positive quantity usage is rounded up to a
	// multiple of, such as "1".
	BillingIncrement *string
}

// UpdateOverrideInput is a change to an override that has not ended, for
// Service.UpdateOverride. Every field except EffectiveTo is a price field.
// A nil pointer or a zero NullableChange keeps the stored value. The values
// follow the rules of CreateOverrideInput, and after the change NativeUnit
// and NativeUnitPrice are both set or both clear.
type UpdateOverrideInput struct {
	// UnitPrice replaces the unit price.
	UnitPrice *string
	// UnitQuantity replaces the unit quantity.
	UnitQuantity *int64
	// NativeUnit replaces or clears the native unit label.
	NativeUnit NullableChange[string]
	// NativeUnitPrice replaces or clears the USD price of one native unit.
	NativeUnitPrice NullableChange[string]
	// MinimumCharge replaces or clears the minimum charge.
	MinimumCharge NullableChange[string]
	// BillingIncrement replaces or clears the billing increment.
	BillingIncrement NullableChange[string]
	// EffectiveTo replaces the end of the override with a time no earlier
	// than now and after its start, or clears it so the override never ends.
	// When the change opens a successor, the end applies to the successor
	// and must be after now.
	EffectiveTo NullableChange[time.Time]
}

type overridePosition struct {
	CreatedAt         time.Time `json:"created_at"`
	PricingOverrideID uuid.UUID `json:"pricing_override_id"`
}

// ErrRuleExistsUseAdjustment is the error for a standalone override whose
// provider, model, meter and conditions equal an open catalog rule: 409
// pricing_rule_exists_use_adjustment.
var ErrRuleExistsUseAdjustment = httpapi.NewCodedError(http.StatusConflict, "pricing_rule_exists_use_adjustment",
	"a catalog rule has this provider, model, meter and conditions, so the override must be an adjustment")

// ErrOverrideEnded is the error for a change to an override that has ended,
// which stays as history: 409 pricing_override_ended.
var ErrOverrideEnded = httpapi.NewCodedError(http.StatusConflict, "pricing_override_ended", "the override has ended and can no longer change")

var (
	overrideCursor       = httpapi.NewCursor[overridePosition](overrideListingName)
	typeRule             = fmt.Sprintf("expected %s or %s", OverrideTypeAdjustment, OverrideTypeStandalone)
	adjustmentRule       = "expected standalone, since no catalog rule has this provider, model, meter and conditions"
	unitPriceRule        = "expected a non-negative price with at most 9 decimals, such as 0.12"
	unitQuantityRule     = "expected 1 or more"
	nativeUnitRule       = fmt.Sprintf("expected 1 to %d characters without control characters, set together with native_unit_price", nativeUnitMaximumLength)
	nativeUnitPriceRule  = "expected a non-negative USD price with at most 9 decimals, set together with native_unit"
	minimumChargeRule    = "expected a non-negative USD amount with at most 9 decimals, such as 0.35"
	billingIncrementRule = "expected a positive quantity with at most 6 decimals, such as 1"
	effectiveToRule      = "expected a time no earlier than now and after effective_from"
	effectivePriceRule   = "expected unit_price times native_unit_price to fit within the amount range"
)

// CreateOverride adds an override to environment in effect from now, stores
// an alias model as the model it names, derives its type from the open
// catalog rules, inserts the uncosted_rerate job of environment in the same
// transaction, clears the service's RuleSetCache and publishes the pricing
// invalidation. An invalid input returns a 422 validation_failed problem that
// names every invalid field: body.provider, body.model, body.meter,
// body.conditions.<key>, body.unit_price, body.unit_quantity,
// body.native_unit, body.native_unit_price, body.minimum_charge and
// body.billing_increment. An input Type of standalone where an open catalog
// rule equals the override returns ErrRuleExistsUseAdjustment, and adjustment
// where none does returns the problem at body.type.
func (service *Service) CreateOverride(ctx context.Context, environment httpapi.Environment, input CreateOverrideInput) (OverrideRecord, error) {
	override, problems := createdOverride(environment, input)
	if len(problems) > 0 {
		return OverrideRecord{}, httpapi.NewValidationProblem(problems...)
	}
	model, err := service.canonicalModel(ctx, override.Provider, override.Model)
	if err != nil {
		return OverrideRecord{}, err
	}
	override.Model = model
	conditions, err := json.Marshal(override.Conditions)
	if err != nil {
		return OverrideRecord{}, fmt.Errorf("encode override conditions: %w", err)
	}
	adjusts, err := service.queries.CatalogRuleExists(ctx, queries.CatalogRuleExistsParams{
		Provider:   override.Provider,
		Model:      override.Model,
		Meter:      string(override.Meter),
		Conditions: conditions,
	})
	if err != nil {
		return OverrideRecord{}, fmt.Errorf("find catalog rule of %s/%s: %w", override.Provider, override.Model, err)
	}
	if input.Type != nil && *input.Type == OverrideTypeStandalone && adjusts {
		return OverrideRecord{}, ErrRuleExistsUseAdjustment
	}
	if input.Type != nil && *input.Type == OverrideTypeAdjustment && !adjusts {
		return OverrideRecord{}, httpapi.NewValidationProblem(httpapi.ProblemError{Location: typeLocation, Message: adjustmentRule})
	}
	override.ID = identifiers.New()
	var row queries.PricingOverride
	err = database.InTransaction(ctx, service.pool, func(ctx context.Context, transaction pgx.Tx) error {
		var err error
		row, err = insertOverride(ctx, service.queries.WithTx(transaction), override, conditions, service.clock.Now())
		if err != nil {
			return err
		}
		return service.insertUncostedRerate(ctx, transaction, environment)
	})
	if err != nil {
		return OverrideRecord{}, err
	}
	record, err := recordFromRow(row, adjusts)
	if err != nil {
		return OverrideRecord{}, err
	}
	if err := service.publishChange(ctx, environment, record.ID); err != nil {
		return OverrideRecord{}, err
	}
	return record, nil
}

// UpdateOverride applies input to the override with overrideID in
// environment under its row lock, inserts the uncosted_rerate job of
// environment in the same transaction, clears the service's RuleSetCache and
// publishes the pricing invalidation. Price history never changes: when input
// changes a price field of an override that started before now, it ends the
// override at now and returns a successor with a new id, the changed fields
// and the end, in effect from now, so usage before now keeps its price. A
// change to the end alone, or any change before the override starts, updates
// it in place. It returns httpapi.ErrNotFound when environment has no such
// override, ErrOverrideEnded when the override has ended, which includes an
// override a successor replaced, and a 422 validation_failed problem at the
// locations of CreateOverride for the fields input holds, plus
// body.effective_to for an end before now.
func (service *Service) UpdateOverride(ctx context.Context, environment httpapi.Environment, overrideID uuid.UUID, input UpdateOverrideInput) (OverrideRecord, error) {
	var record OverrideRecord
	err := database.InTransaction(ctx, service.pool, func(ctx context.Context, transaction pgx.Tx) error {
		transactionQueries := service.queries.WithTx(transaction)
		current, err := lockOverride(ctx, transactionQueries, environment, overrideID)
		if err != nil {
			return err
		}
		now := service.clock.Now()
		if current.EffectiveTo != nil && !current.EffectiveTo.After(now) {
			return ErrOverrideEnded
		}
		updated, problems := updatedOverride(current, input, now)
		if len(problems) > 0 {
			return httpapi.NewValidationProblem(problems...)
		}
		if opensSuccessor(current, updated, now) {
			record, err = startSuccessor(ctx, transactionQueries, current, updated, now)
		} else {
			record, err = writeOverride(ctx, transactionQueries, updated, now)
		}
		if err != nil {
			return err
		}
		return service.insertUncostedRerate(ctx, transaction, environment)
	})
	if err != nil {
		return OverrideRecord{}, err
	}
	if err := service.publishChange(ctx, environment, record.ID); err != nil {
		return OverrideRecord{}, err
	}
	return record, nil
}

// EndOverride sets the end of the override with overrideID in environment
// to now under its row lock, inserts the uncosted_rerate job of environment
// in the same transaction, clears the service's RuleSetCache and publishes
// the pricing invalidation. The row stays, and an override ended at the
// instant it started keeps an empty window that prices nothing. An override
// that has already ended keeps its end, inserts no job and publishes nothing. It returns httpapi.ErrNotFound
// when environment has no such override.
func (service *Service) EndOverride(ctx context.Context, environment httpapi.Environment, overrideID uuid.UUID) error {
	ended := false
	err := database.InTransaction(ctx, service.pool, func(ctx context.Context, transaction pgx.Tx) error {
		transactionQueries := service.queries.WithTx(transaction)
		current, err := lockOverride(ctx, transactionQueries, environment, overrideID)
		if err != nil {
			return err
		}
		now := service.clock.Now()
		if current.EffectiveTo != nil && !current.EffectiveTo.After(now) {
			return nil
		}
		current.EffectiveTo = &now
		ended = true
		if _, err := writeOverride(ctx, transactionQueries, current, now); err != nil {
			return err
		}
		return service.insertUncostedRerate(ctx, transaction, environment)
	})
	if err != nil || !ended {
		return err
	}
	return service.publishChange(ctx, environment, overrideID)
}

// GetOverride returns the override with overrideID in environment, or
// httpapi.ErrNotFound when environment has no such override.
func (service *Service) GetOverride(ctx context.Context, environment httpapi.Environment, overrideID uuid.UUID) (OverrideRecord, error) {
	row, err := service.queries.SelectPricingOverride(ctx, queries.SelectPricingOverrideParams{
		Environment:       queries.Environment(environment),
		PricingOverrideID: overrideID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return OverrideRecord{}, httpapi.ErrNotFound
	}
	if err != nil {
		return OverrideRecord{}, fmt.Errorf("select pricing override %s: %w", overrideID, err)
	}
	records, err := withTypes(ctx, service.queries, []queries.PricingOverride{row})
	if err != nil {
		return OverrideRecord{}, err
	}
	return records[0], nil
}

// ListOverrides returns one page of the overrides of environment, newest
// first, and the cursor of the next page, which is empty on the last page.
// It leaves out the overrides that have ended unless includeEnded is true. A
// limit of 0 selects httpapi.ListLimitDefault, and a limit outside 1 to
// httpapi.ListLimitMaximum returns a 422 validation_failed problem at
// query.limit. A cursor that ListOverrides did not return for environment
// fails with httpapi.ErrInvalidCursor.
func (service *Service) ListOverrides(ctx context.Context, environment httpapi.Environment, includeEnded bool, cursor string, limit int) ([]OverrideRecord, string, error) {
	pageSize, err := httpapi.ParseLimit(limit)
	if err != nil {
		return nil, "", err
	}
	parameters := queries.ListPricingOverridesParams{
		Environment:  queries.Environment(environment),
		IncludeEnded: includeEnded,
		At:           service.clock.Now(),
		RowLimit:     int64(pageSize) + 1,
	}
	if cursor != "" {
		position, err := overrideCursor.Decode(environment, cursor)
		if err != nil {
			return nil, "", err
		}
		parameters.AfterCreatedAt = &position.CreatedAt
		parameters.AfterPricingOverrideID = &position.PricingOverrideID
	}
	rows, err := service.queries.ListPricingOverrides(ctx, parameters)
	if err != nil {
		return nil, "", fmt.Errorf("list pricing overrides of environment %s: %w", environment, err)
	}
	nextCursor := ""
	if len(rows) > pageSize {
		rows = rows[:pageSize]
		last := rows[pageSize-1]
		nextCursor, err = overrideCursor.Encode(environment, overridePosition{CreatedAt: last.CreatedAt, PricingOverrideID: last.PricingOverrideID})
		if err != nil {
			return nil, "", err
		}
	}
	records, err := withTypes(ctx, service.queries, rows)
	if err != nil {
		return nil, "", err
	}
	return records, nextCursor, nil
}

func (service *Service) publishChange(ctx context.Context, environment httpapi.Environment, overrideID uuid.UUID) error {
	service.ruleSets.Clear()
	return service.cache.PublishInvalidation(ctx, cache.Invalidation{
		Kind:        cache.InvalidationKindPricing,
		Environment: string(environment),
		ID:          identifiers.Encode(identifiers.PrefixPricingOverride, overrideID),
	})
}

func startSuccessor(ctx context.Context, transactionQueries *queries.Queries, current, updated Override, now time.Time) (OverrideRecord, error) {
	current.EffectiveTo = &now
	if _, err := writeOverride(ctx, transactionQueries, current, now); err != nil {
		return OverrideRecord{}, err
	}
	updated.ID = identifiers.New()
	conditions, err := json.Marshal(updated.Conditions)
	if err != nil {
		return OverrideRecord{}, fmt.Errorf("encode override conditions: %w", err)
	}
	row, err := insertOverride(ctx, transactionQueries, updated, conditions, now)
	if err != nil {
		return OverrideRecord{}, err
	}
	records, err := withTypes(ctx, transactionQueries, []queries.PricingOverride{row})
	if err != nil {
		return OverrideRecord{}, err
	}
	return records[0], nil
}

func insertOverride(ctx context.Context, overrideQueries *queries.Queries, override Override, conditions []byte, now time.Time) (queries.PricingOverride, error) {
	parameters := queries.InsertPricingOverrideParams{
		PricingOverrideID:      override.ID,
		Environment:            queries.Environment(override.Environment),
		Provider:               override.Provider,
		Model:                  override.Model,
		Meter:                  string(override.Meter),
		Conditions:             conditions,
		UnitPriceNanos:         override.UnitPrice.Nanos,
		UnitQuantity:           override.UnitPrice.UnitQuantity,
		MinimumChargeNanos:     override.MinimumCharge,
		BillingIncrementMicros: override.BillingIncrement,
		CreatedAt:              now,
		EffectiveTo:            override.EffectiveTo,
	}
	if override.NativeUnit != nil {
		parameters.NativeUnit = &override.NativeUnit.Label
		parameters.NativeUnitPriceNanos = &override.NativeUnit.Price
	}
	row, err := overrideQueries.InsertPricingOverride(ctx, parameters)
	if err != nil {
		return queries.PricingOverride{}, fmt.Errorf("insert pricing override: %w", err)
	}
	return row, nil
}

func writeOverride(ctx context.Context, transactionQueries *queries.Queries, override Override, now time.Time) (OverrideRecord, error) {
	parameters := queries.UpdatePricingOverrideParams{
		UnitPriceNanos:         override.UnitPrice.Nanos,
		UnitQuantity:           override.UnitPrice.UnitQuantity,
		MinimumChargeNanos:     override.MinimumCharge,
		BillingIncrementMicros: override.BillingIncrement,
		EffectiveTo:            override.EffectiveTo,
		UpdatedAt:              now,
		PricingOverrideID:      override.ID,
	}
	if override.NativeUnit != nil {
		parameters.NativeUnit = &override.NativeUnit.Label
		parameters.NativeUnitPriceNanos = &override.NativeUnit.Price
	}
	row, err := transactionQueries.UpdatePricingOverride(ctx, parameters)
	if err != nil {
		return OverrideRecord{}, fmt.Errorf("update pricing override %s: %w", override.ID, err)
	}
	records, err := withTypes(ctx, transactionQueries, []queries.PricingOverride{row})
	if err != nil {
		return OverrideRecord{}, err
	}
	return records[0], nil
}

func lockOverride(ctx context.Context, transactionQueries *queries.Queries, environment httpapi.Environment, overrideID uuid.UUID) (Override, error) {
	row, err := transactionQueries.SelectPricingOverrideForUpdate(ctx, queries.SelectPricingOverrideForUpdateParams{
		Environment:       queries.Environment(environment),
		PricingOverrideID: overrideID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Override{}, httpapi.ErrNotFound
	}
	if err != nil {
		return Override{}, fmt.Errorf("select pricing override %s for update: %w", overrideID, err)
	}
	return overrideFromRow(row)
}

func withTypes(ctx context.Context, overrideQueries *queries.Queries, rows []queries.PricingOverride) ([]OverrideRecord, error) {
	overrideIDs := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		overrideIDs = append(overrideIDs, row.PricingOverrideID)
	}
	adjustmentIDs, err := overrideQueries.ListAdjustmentOverrideIDs(ctx, overrideIDs)
	if err != nil {
		return nil, fmt.Errorf("list adjustment overrides: %w", err)
	}
	records := make([]OverrideRecord, 0, len(rows))
	for _, row := range rows {
		record, err := recordFromRow(row, slices.Contains(adjustmentIDs, row.PricingOverrideID))
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func recordFromRow(row queries.PricingOverride, adjusts bool) (OverrideRecord, error) {
	override, err := overrideFromRow(row)
	if err != nil {
		return OverrideRecord{}, err
	}
	effectiveUnitPrice, err := override.EffectiveUnitPrice()
	if err != nil {
		return OverrideRecord{}, fmt.Errorf("price override %s: %w", row.PricingOverrideID, err)
	}
	record := OverrideRecord{
		Override:           override,
		Type:               OverrideTypeStandalone,
		EffectiveUnitPrice: effectiveUnitPrice,
		CreatedAt:          row.CreatedAt.UTC(),
		UpdatedAt:          row.UpdatedAt.UTC(),
	}
	if adjusts {
		record.Type = OverrideTypeAdjustment
	}
	return record, nil
}

func overrideFromRow(row queries.PricingOverride) (Override, error) {
	conditions, err := decodeConditions(row.Conditions)
	if err != nil {
		return Override{}, fmt.Errorf("decode conditions of override %s: %w", row.PricingOverrideID, err)
	}
	override := Override{
		ID:               row.PricingOverrideID,
		Environment:      httpapi.Environment(row.Environment),
		Provider:         row.Provider,
		Model:            row.Model,
		Meter:            Meter(row.Meter),
		Conditions:       conditions,
		UnitPrice:        money.UnitPrice{Nanos: row.UnitPriceNanos, UnitQuantity: row.UnitQuantity},
		MinimumCharge:    row.MinimumChargeNanos,
		BillingIncrement: row.BillingIncrementMicros,
		EffectiveFrom:    row.EffectiveFrom.UTC(),
	}
	if row.EffectiveTo != nil {
		effectiveTo := row.EffectiveTo.UTC()
		override.EffectiveTo = &effectiveTo
	}
	if row.NativeUnit != nil {
		override.NativeUnit = &NativeUnit{Label: *row.NativeUnit, Price: *row.NativeUnitPriceNanos}
	}
	return override, nil
}

func createdOverride(environment httpapi.Environment, input CreateOverrideInput) (Override, problemList) {
	var problems problemList
	override := Override{Environment: environment, Provider: input.Provider, Model: input.Model, Conditions: input.Conditions}
	if override.Conditions == nil {
		override.Conditions = Attributes{}
	}
	problems.require(input.Type == nil || *input.Type == OverrideTypeAdjustment || *input.Type == OverrideTypeStandalone, typeLocation, typeRule)
	problems.name(input.Provider, providerLocation)
	problems.name(input.Model, modelLocation)
	meter, err := ParseMeter(input.Meter)
	problems.require(err == nil, meterLocation, meterRule)
	override.Meter = meter
	problems.attributes(override.Conditions, conditionsLocation)
	override.UnitPrice.Nanos = problems.unitPrice(input.UnitPrice)
	override.UnitPrice.UnitQuantity = input.UnitQuantity
	problems.require(input.UnitQuantity >= 1, unitQuantityLocation, unitQuantityRule)
	override.NativeUnit = problems.nativeUnit(input.NativeUnit, input.NativeUnitPrice)
	override.MinimumCharge = problems.optionalAmount(input.MinimumCharge, minimumChargeLocation, minimumChargeRule)
	override.BillingIncrement = problems.billingIncrement(input.BillingIncrement)
	problems.effectivePrice(override)
	return override, problems
}

func updatedOverride(current Override, input UpdateOverrideInput, now time.Time) (Override, problemList) {
	var problems problemList
	updated := current
	if input.UnitPrice != nil {
		updated.UnitPrice.Nanos = problems.unitPrice(*input.UnitPrice)
	}
	if input.UnitQuantity != nil {
		updated.UnitPrice.UnitQuantity = *input.UnitQuantity
		problems.require(*input.UnitQuantity >= 1, unitQuantityLocation, unitQuantityRule)
	}
	var nativeUnit, nativeUnitPrice *string
	if current.NativeUnit != nil {
		storedPrice := money.FormatAmount(current.NativeUnit.Price)
		nativeUnit = &current.NativeUnit.Label
		nativeUnitPrice = &storedPrice
	}
	if input.NativeUnit.Replace {
		nativeUnit = input.NativeUnit.Value
	}
	if input.NativeUnitPrice.Replace {
		nativeUnitPrice = input.NativeUnitPrice.Value
	}
	updated.NativeUnit = problems.nativeUnit(nativeUnit, nativeUnitPrice)
	if input.MinimumCharge.Replace {
		updated.MinimumCharge = problems.optionalAmount(input.MinimumCharge.Value, minimumChargeLocation, minimumChargeRule)
	}
	if input.BillingIncrement.Replace {
		updated.BillingIncrement = problems.billingIncrement(input.BillingIncrement.Value)
	}
	if input.EffectiveTo.Replace {
		updated.EffectiveTo = input.EffectiveTo.Value
		start := current.EffectiveFrom
		if opensSuccessor(current, updated, now) {
			start = now
		}
		validEnd := updated.EffectiveTo == nil || (!updated.EffectiveTo.Before(now) && updated.EffectiveTo.After(start))
		problems.require(validEnd, effectiveToLocation, effectiveToRule)
	}
	problems.effectivePrice(updated)
	return updated, problems
}

func opensSuccessor(current, updated Override, now time.Time) bool {
	samePrices := current.UnitPrice == updated.UnitPrice &&
		equalOptional(current.NativeUnit, updated.NativeUnit) &&
		equalOptional(current.MinimumCharge, updated.MinimumCharge) &&
		equalOptional(current.BillingIncrement, updated.BillingIncrement)
	return !samePrices && current.EffectiveFrom.Before(now)
}

func (problems *problemList) unitPrice(value string) money.Amount {
	nanos, err := money.ParseNonNegativeAmount(value)
	problems.require(err == nil, unitPriceLocation, unitPriceRule)
	return nanos
}

func (problems *problemList) optionalAmount(value *string, location string, rule string) *money.Amount {
	if value == nil {
		return nil
	}
	amount, err := money.ParseNonNegativeAmount(*value)
	if err != nil {
		problems.add(location, rule)
		return nil
	}
	return &amount
}

func (problems *problemList) billingIncrement(value *string) *money.Quantity {
	if value == nil {
		return nil
	}
	increment, err := money.ParseQuantity(*value)
	if err != nil || increment == 0 {
		problems.add(billingIncrementLocation, billingIncrementRule)
		return nil
	}
	return &increment
}

func (problems *problemList) nativeUnit(label *string, price *string) *NativeUnit {
	validLabel := label != nil && validNativeUnit(*label)
	parsedPrice := problems.optionalAmount(price, nativeUnitPriceLocation, nativeUnitPriceRule)
	problems.require(validLabel || (label == nil && price == nil), nativeUnitLocation, nativeUnitRule)
	problems.require(price != nil || label == nil, nativeUnitPriceLocation, nativeUnitPriceRule)
	if !validLabel || parsedPrice == nil {
		return nil
	}
	return &NativeUnit{Label: *label, Price: *parsedPrice}
}

func (problems *problemList) effectivePrice(override Override) {
	if len(*problems) > 0 {
		return
	}
	_, err := override.EffectiveUnitPrice()
	problems.require(err == nil, unitPriceLocation, effectivePriceRule)
}

func validNativeUnit(label string) bool {
	return strings.TrimSpace(label) != "" &&
		utf8.RuneCountInString(label) <= nativeUnitMaximumLength &&
		utf8.ValidString(label) &&
		!strings.ContainsFunc(label, unicode.IsControl)
}
