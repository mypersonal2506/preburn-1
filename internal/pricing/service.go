package pricing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/catalogfiles"
	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/pricing/queries"
)

const (
	nameMaximumLength    = 200
	ruleSetHistoryWindow = 90 * 24 * time.Hour

	providerLocation   = "body.provider"
	modelLocation      = "body.model"
	attributesLocation = "body.attributes"
	usageLocation      = "body.usage"
)

// QuoteRequest is the usage of one provider request for Service.Quote.
// Usage holds API quantity strings.
type QuoteRequest struct {
	// Provider is the provider name, such as openai, 1 to 200 characters.
	Provider string
	// Model is the model name or one of its aliases, 1 to 200 characters.
	Model string
	// Attributes describe the request, such as its resolution. Known keys
	// follow the attribute vocabulary and other keys may hold any value.
	Attributes Attributes
	// Usage maps meter names to non-negative quantities with at most 6
	// decimals, such as "8.5".
	Usage map[string]string
}

// Service prices requests with the catalog rules, the overrides of each
// environment and the model aliases stored in Postgres, lists the catalog,
// and creates, changes and ends overrides. It keeps the RuleSet of each
// environment in its RuleSetCache. Create one with NewService. It is safe for
// concurrent use.
type Service struct {
	pool     *pgxpool.Pool
	queries  *queries.Queries
	cache    *cache.Client
	jobs     *river.Client[pgx.Tx]
	clock    clock.Clock
	files    catalogfiles.Catalog
	ruleSets *RuleSetCache
}

type problemList []httpapi.ProblemError

var (
	nameRule      = fmt.Sprintf("expected 1 to %d characters without control characters", nameMaximumLength)
	meterRule     = "expected a meter listed by GET /api/v1/pricing/meters"
	quantityRule  = "expected a non-negative quantity with at most 6 decimals, such as 8.5"
	usageCostRule = "expected usage whose cost fits within the amount range"
)

// NewService returns a Service that reads and writes pricing in pool,
// inserts the uncosted_rerate job of each override change with jobs, takes
// display names, default attributes and parameter mappings from files,
// publishes the pricing invalidation through cacheClient and reads time from
// timeSource. It only stores its arguments, so zero values serve route
// registration for the OpenAPI document.
func NewService(pool *pgxpool.Pool, cacheClient *cache.Client, jobs *river.Client[pgx.Tx], files catalogfiles.Catalog, timeSource clock.Clock) *Service {
	service := &Service{pool: pool, queries: queries.New(pool), cache: cacheClient, jobs: jobs, clock: timeSource, files: files}
	service.ruleSets = newRuleSetCache(service, timeSource)
	return service
}

// RuleSets returns the RuleSetCache of the service. Every override change
// clears it.
func (service *Service) RuleSets() *RuleSetCache {
	return service.ruleSets
}

// Quote rates request at the current time with the RuleSet of environment,
// as Rate does. An invalid request returns a 422 validation_failed problem
// that names every invalid field: body.provider, body.model,
// body.attributes.<key> for a known key whose value breaks the vocabulary,
// body.usage for a meter name outside the vocabulary, and
// body.usage.<meter> for a quantity that does not parse. A cost beyond the
// amount range returns the problem at body.usage.
func (service *Service) Quote(ctx context.Context, environment httpapi.Environment, request QuoteRequest) (RatedRequest, error) {
	usage, problems := quoteUsage(request)
	if len(problems) > 0 {
		return RatedRequest{}, httpapi.NewValidationProblem(problems...)
	}
	ruleSet, err := service.ruleSets.Get(ctx, environment)
	if err != nil {
		return RatedRequest{}, err
	}
	rated, err := Rate(RatingRequest{
		Provider:   request.Provider,
		Model:      request.Model,
		Attributes: request.Attributes,
		Usage:      usage,
		OccurredAt: service.clock.Now(),
	}, ruleSet)
	if errors.Is(err, money.ErrOverflow) {
		return RatedRequest{}, httpapi.NewValidationProblem(httpapi.ProblemError{Location: usageLocation, Message: usageCostRule})
	}
	return rated, err
}

func (service *Service) loadRuleSet(ctx context.Context, environment httpapi.Environment) (*RuleSet, error) {
	since := service.clock.Now().Add(-ruleSetHistoryWindow)
	ruleRows, err := service.queries.ListRatingRules(ctx, since)
	if err != nil {
		return nil, fmt.Errorf("list rating rules: %w", err)
	}
	overrideRows, err := service.queries.ListRatingOverrides(ctx, queries.ListRatingOverridesParams{
		Environment: queries.Environment(environment),
		Since:       since,
	})
	if err != nil {
		return nil, fmt.Errorf("list rating overrides of environment %s: %w", environment, err)
	}
	aliasRows, err := service.queries.ListProviderModelAliases(ctx)
	if err != nil {
		return nil, fmt.Errorf("list model aliases: %w", err)
	}
	rules := make([]Rule, 0, len(ruleRows))
	for _, row := range ruleRows {
		rule, err := ruleFromRow(row)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	overrides := make([]Override, 0, len(overrideRows))
	for _, row := range overrideRows {
		override, err := overrideFromRow(row)
		if err != nil {
			return nil, err
		}
		overrides = append(overrides, override)
	}
	aliases := make([]ModelAlias, 0, len(aliasRows))
	for _, row := range aliasRows {
		aliases = append(aliases, ModelAlias{Provider: row.Provider, Alias: row.Alias, Model: row.Model})
	}
	return NewRuleSet(rules, overrides, aliases)
}

func (service *Service) canonicalModel(ctx context.Context, provider, model string) (string, error) {
	aliased, err := service.queries.SelectAliasedModel(ctx, queries.SelectAliasedModelParams{Provider: provider, Alias: model})
	if errors.Is(err, pgx.ErrNoRows) {
		return model, nil
	}
	if err != nil {
		return "", fmt.Errorf("select alias %s of provider %s: %w", model, provider, err)
	}
	return aliased, nil
}

func ruleFromRow(row queries.ListRatingRulesRow) (Rule, error) {
	conditions, err := decodeConditions(row.Conditions)
	if err != nil {
		return Rule{}, fmt.Errorf("decode conditions of rule %s: %w", row.PricingRuleID, err)
	}
	return Rule{
		ID:               row.PricingRuleID,
		Provider:         row.Provider,
		Model:            row.Model,
		Meter:            Meter(row.Meter),
		Conditions:       conditions,
		UnitPrice:        money.UnitPrice{Nanos: row.UnitPriceNanos, UnitQuantity: row.UnitQuantity},
		MinimumCharge:    row.MinimumChargeNanos,
		BillingIncrement: row.BillingIncrementMicros,
		EffectiveFrom:    row.EffectiveFrom,
		EffectiveTo:      row.EffectiveTo,
		Status:           RuleStatus(row.Status),
		Source:           RuleSource(row.Source),
	}, nil
}

func decodeConditions(encoded []byte) (Attributes, error) {
	var conditions Attributes
	if err := json.Unmarshal(encoded, &conditions); err != nil {
		return nil, err
	}
	return conditions, nil
}

func attributesFromCatalog(values catalogfiles.Attributes) (Attributes, error) {
	attributes := make(Attributes, len(values))
	for key, value := range values {
		switch typed := value.(type) {
		case string:
			attributes[key] = StringAttribute(typed)
		case bool:
			attributes[key] = BooleanAttribute(typed)
		default:
			return nil, fmt.Errorf("attribute %s holds %T, want a string or a boolean", key, value)
		}
	}
	return attributes, nil
}

func quoteUsage(request QuoteRequest) (map[Meter]money.Quantity, problemList) {
	var problems problemList
	problems.name(request.Provider, providerLocation)
	problems.name(request.Model, modelLocation)
	problems.attributes(request.Attributes, attributesLocation)
	usage := make(map[Meter]money.Quantity, len(request.Usage))
	unknownMeter := false
	for _, name := range slices.Sorted(maps.Keys(request.Usage)) {
		meter, err := ParseMeter(name)
		if err != nil {
			unknownMeter = true
			continue
		}
		quantity, err := money.ParseQuantity(request.Usage[name])
		problems.require(err == nil, usageLocation+"."+name, quantityRule)
		usage[meter] = quantity
	}
	problems.require(!unknownMeter, usageLocation, meterRule)
	return usage, problems
}

func (problems *problemList) require(valid bool, location string, message string) {
	if !valid {
		problems.add(location, message)
	}
}

func (problems *problemList) add(location string, message string) {
	*problems = append(*problems, httpapi.ProblemError{Location: location, Message: message})
}

func (problems *problemList) name(value string, location string) {
	valid := strings.TrimSpace(value) != "" &&
		utf8.RuneCountInString(value) <= nameMaximumLength &&
		utf8.ValidString(value) &&
		!strings.ContainsFunc(value, unicode.IsControl)
	problems.require(valid, location, nameRule)
}

func (problems *problemList) attributes(attributes Attributes, location string) {
	for _, key := range slices.Sorted(maps.Keys(attributes)) {
		err := ValidateAttributes(Attributes{key: attributes[key]})
		if invalid, isInvalid := errors.AsType[*InvalidAttributeError](err); isInvalid {
			problems.add(location+"."+key, "expected "+invalid.Requirement)
		}
	}
}
