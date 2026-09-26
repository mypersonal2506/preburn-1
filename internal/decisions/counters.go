package decisions

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/signals"
)

const (
	counterKeyName              = "counter"
	reservationKeyName          = "reservation"
	reservationsExpiringKeyName = "reservations_expiring"
	countersReadyKeyName        = "counters_ready"
	pendingChangesKeySuffix     = ":pending"
	openReservationsKeySuffix   = ":reservations"

	fieldSettled          = "settled"
	fieldReserved         = "reserved"
	fieldCount            = "count"
	featureFieldSeparator = ":"

	reservationFieldCounterKey    = "counter_key"
	reservationFieldFeature       = "feature"
	reservationFieldAmount        = "amount"
	reservationFieldStatus        = "status"
	reservationFieldExpiresAtUnix = "expires_at_unix"

	counterRetention     = 7 * 24 * time.Hour
	reservationRetention = 24 * time.Hour

	reserveScriptName          = "reserve"
	settleScriptName           = "settle"
	releaseScriptName          = "release"
	settleUnreservedScriptName = "settle_unreserved"
	beginChangeScriptName      = "begin_change"

	reserveModeReserve   = "reserve"
	reserveModeCountOnly = "count_only"
	noCeiling            = ""
	replyNoop            = "noop"
	replyNotReady        = "not_ready"
	replyNotPending      = "not_pending"
	replySettled         = "settled"
	replyPending         = "pending"
	replyPartSeparator   = "_"
	minimumExpiredLimit  = 1
)

// ReserveResult is what the reserve script did for Reserve or CountOnly.
type ReserveResult string

const (
	// ReserveResultReserved means Reserve added the amount to the reserved
	// fields, added one to the count fields and wrote the reservation.
	ReserveResultReserved ReserveResult = "reserved"
	// ReserveResultCounted means CountOnly added one to the count fields.
	ReserveResultCounted ReserveResult = "counted"
	// ReserveResultDeniedAllowance means settled plus reserved plus the amount
	// would exceed the allowance ceiling. Nothing changed.
	ReserveResultDeniedAllowance ReserveResult = "denied_allowance"
	// ReserveResultDeniedLimit means the feature, or the counter across every
	// feature, would exceed its amount or count ceiling. Nothing changed.
	ReserveResultDeniedLimit ReserveResult = "denied_limit"
)

// ReservationStatus is the status field of a reservation hash.
type ReservationStatus string

const (
	// ReservationStatusReserved is a reservation that still holds its amount.
	ReservationStatusReserved ReservationStatus = "reserved"
	// ReservationStatusSettled is a reservation that Settle replaced with the
	// actual amount.
	ReservationStatusSettled ReservationStatus = "settled"
	// ReservationStatusReleased is a reservation released without usage.
	ReservationStatusReleased ReservationStatus = "released"
	// ReservationStatusExpired is a reservation released by the expiry job.
	ReservationStatusExpired ReservationStatus = "expired"
	// ReservationStatusMissing reports that no reservation hash exists, because
	// the decision never reserved or its hash expired.
	ReservationStatusMissing ReservationStatus = "missing"
)

// Counters keeps the customer period counters, reservations and the
// counters_ready marker of the Redis contract. Counter values are integers:
// nanos for amounts, plain numbers for counts. Create one with NewCounters.
// It is safe for concurrent use.
//
// Next to each counter it keeps two sets under the counter key with a suffix.
// counter:...:reservations holds the ids of the counter's reservations that
// are still reserved. counter:...:pending holds the counter's pending
// changes, scored by the Unix milliseconds they started at: a change is a
// write that reaches Postgres and the counter at different moments, so
// counters_reconcile leaves a counter alone while one is pending. A check's
// reservation or count is pending from the reserve script until EndChange,
// after its decision is stored. A ledger entry's settlement is pending from
// BeginChange, before its transaction commits, until Settle or
// SettleUnreserved applies it.
//
// Every script that changes a counter first checks the counters_ready
// marker and returns ErrCountersNotReady without a change while it is
// missing, so nothing lands in a counter that a rebuild is writing.
type Counters struct {
	cache                  *cache.Client
	reserveScript          *cache.Script
	settleScript           *cache.Script
	releaseScript          *cache.Script
	settleUnreservedScript *cache.Script
	beginChangeScript      *cache.Script
}

// Ceilings are the upper bounds a Reserve or CountOnly call must stay
// within. A nil field has no ceiling.
type Ceilings struct {
	// Allowance bounds settled plus reserved across every feature, including
	// the new amount.
	Allowance *money.Amount
	// FeatureAmount bounds settled plus reserved of the feature, including the
	// new amount.
	FeatureAmount *money.Amount
	// FeatureCount bounds the count of the feature, including this call.
	FeatureCount *int64
	// TotalAmount bounds settled plus reserved across every feature,
	// including the new amount, like Allowance, but a denial is a limit
	// denial.
	TotalAmount *money.Amount
	// TotalCount bounds the count across every feature, including this
	// call.
	TotalCount *int64
}

// ReserveRequest is one Reserve, CountOnly or RestoreReservation call.
type ReserveRequest struct {
	// Environment is the environment of the counter.
	Environment httpapi.Environment
	// CustomerID is the customer the counter belongs to.
	CustomerID uuid.UUID
	// PeriodStart is the start of the customer period.
	PeriodStart time.Time
	// PeriodEnd is the end of the customer period. The counter expires seven
	// days after it.
	PeriodEnd time.Time
	// DecisionID names the reservation.
	DecisionID uuid.UUID
	// Feature is the feature the request belongs to.
	Feature string
	// Amount is the amount to reserve. CountOnly checks the ceilings with it
	// but adds it nowhere.
	Amount money.Amount
	// ExpiresAt is when the reservation expires. The reservation hash expires
	// one day after it. CountOnly ignores it.
	ExpiresAt time.Time
	// DecidedAt is when the check decided. Reserve and CountOnly register the
	// decision as a pending change of the counter that started then.
	// RestoreReservation ignores it.
	DecidedAt time.Time
	// Ceilings bound the counter. RestoreReservation ignores them.
	Ceilings Ceilings
}

// Settlement is the usage of one ledger entry that Settle or
// SettleUnreserved adds to a counter.
type Settlement struct {
	// Environment is the environment of the counter.
	Environment httpapi.Environment
	// CustomerID is the customer the counter belongs to.
	CustomerID uuid.UUID
	// PeriodStart is the start of the customer period.
	PeriodStart time.Time
	// PeriodEnd is the end of the customer period. The counter expires seven
	// days after it.
	PeriodEnd time.Time
	// ChangeID is the ledger entry's id, the pending change that BeginChange
	// registers and the settlement finishes.
	ChangeID uuid.UUID
	// Feature is the feature the usage belongs to.
	Feature string
	// Amount is added to settled and to the feature's settled field.
	Amount money.Amount
	// CountIncrement is what SettleUnreserved adds to count and to the
	// feature's count field: 1 for a client fallback that never checked, 0
	// for a decision that already counted and for a correction. Settle adds
	// nothing to the counts.
	CountIncrement int64
}

// TransitionResult is the outcome of Release.
type TransitionResult struct {
	// Applied reports whether the reservation was reserved and moved to
	// Status.
	Applied bool
	// Status is the reservation's status after the call. When Applied is
	// false it is the status the reservation already had, or
	// ReservationStatusMissing.
	Status ReservationStatus
}

//go:embed scripts/reserve.lua
var reserveSource string

//go:embed scripts/settle.lua
var settleSource string

//go:embed scripts/release.lua
var releaseSource string

//go:embed scripts/settle_unreserved.lua
var settleUnreservedSource string

//go:embed scripts/begin_change.lua
var beginChangeSource string

var (
	// ErrCountersNotReady is the error of a counter script or readiness check
	// while the counters_ready marker is missing.
	ErrCountersNotReady = errors.New("counters_ready marker is missing")
	// ErrChangeNotPending is the error of Settle and SettleUnreserved when
	// their change is not pending, because BeginChange never registered it or
	// counters_reconcile or a rebuild dropped it. The counter is left for
	// counters_reconcile to repair from Postgres.
	ErrChangeNotPending = errors.New("counter change is not pending")
)

// NewCounters returns Counters that keep their keys in cacheClient.
func NewCounters(cacheClient *cache.Client) *Counters {
	return &Counters{
		cache:                  cacheClient,
		reserveScript:          cache.NewScript(reserveScriptName, reserveSource),
		settleScript:           cache.NewScript(settleScriptName, settleSource),
		releaseScript:          cache.NewScript(releaseScriptName, releaseSource),
		settleUnreservedScript: cache.NewScript(settleUnreservedScriptName, settleUnreservedSource),
		beginChangeScript:      cache.NewScript(beginChangeScriptName, beginChangeSource),
	}
}

// Scripts returns the reserve, settle, release, settle_unreserved and
// begin_change scripts for cache.Client.LoadScripts at startup.
func (counters *Counters) Scripts() []*cache.Script {
	return []*cache.Script{counters.reserveScript, counters.settleScript, counters.releaseScript, counters.settleUnreservedScript, counters.beginChangeScript}
}

// CounterKey returns the key of the counter hash of customerID's period that
// starts at periodStart: counter:{environment}:{customer_id}:{period start
// Unix seconds}, after the key prefix.
func (counters *Counters) CounterKey(environment httpapi.Environment, customerID uuid.UUID, periodStart time.Time) string {
	return counters.cache.Key(counterKeyName, string(environment), customerID.String(), formatInteger(periodStart.Unix()))
}

// ReservationKey returns the key of the reservation hash of decisionID:
// reservation:{decision_id}, after the key prefix.
func (counters *Counters) ReservationKey(decisionID uuid.UUID) string {
	return counters.cache.Key(reservationKeyName, decisionID.String())
}

// Snapshot reads the counter of customerID's period that starts at
// periodStart: the totals and the fields of feature, which is always in
// Features. Missing fields read as zero.
func (counters *Counters) Snapshot(ctx context.Context, environment httpapi.Environment, customerID uuid.UUID, periodStart time.Time, feature string) (signals.CounterSnapshot, error) {
	fieldNames := []string{
		fieldSettled, fieldReserved, fieldCount,
		featureField(fieldSettled, feature), featureField(fieldReserved, feature), featureField(fieldCount, feature),
	}
	values, err := counters.cache.Redis().HMGet(ctx, counters.CounterKey(environment, customerID, periodStart), fieldNames...).Result()
	if err != nil {
		return signals.CounterSnapshot{}, fmt.Errorf("read counter: %w", err)
	}
	fields := make(map[string]string, len(fieldNames))
	for index, value := range values {
		if text, isString := value.(string); isString {
			fields[fieldNames[index]] = text
		}
	}
	snapshot, err := parseCounter(fields)
	if err != nil {
		return signals.CounterSnapshot{}, err
	}
	if _, found := snapshot.Features[feature]; !found {
		snapshot.Features[feature] = signals.FeatureCounter{}
	}
	return snapshot, nil
}

// SnapshotMany reads the counters of several customers in one pipeline.
// periodStarts maps each customer id to the start of its period. Every
// customer gets a snapshot with the totals and every feature its counter
// holds, zero for a counter that does not exist.
func (counters *Counters) SnapshotMany(ctx context.Context, environment httpapi.Environment, periodStarts map[uuid.UUID]time.Time) (map[uuid.UUID]signals.CounterSnapshot, error) {
	pipeline := counters.cache.Redis().Pipeline()
	commands := make(map[uuid.UUID]*redis.MapStringStringCmd, len(periodStarts))
	for customerID, periodStart := range periodStarts {
		commands[customerID] = pipeline.HGetAll(ctx, counters.CounterKey(environment, customerID, periodStart))
	}
	if _, err := pipeline.Exec(ctx); err != nil {
		return nil, fmt.Errorf("read counters: %w", err)
	}
	snapshots := make(map[uuid.UUID]signals.CounterSnapshot, len(commands))
	for customerID, command := range commands {
		snapshot, err := parseCounter(command.Val())
		if err != nil {
			return nil, fmt.Errorf("customer %s: %w", customerID, err)
		}
		snapshots[customerID] = snapshot
	}
	return snapshots, nil
}

// Reserve runs the reserve script in reserve mode. Unless a ceiling denies
// it, it adds request.Amount to reserved and to the feature's reserved field,
// adds one to count and to the feature's count field, writes the reservation
// hash, adds the decision to the counter's open reservations and to
// reservations_expiring, and registers the decision as a pending change of
// the counter until EndChange, all in one step.
func (counters *Counters) Reserve(ctx context.Context, request ReserveRequest) (ReserveResult, error) {
	return counters.runReserve(ctx, reserveModeReserve, request)
}

// CountOnly runs the reserve script in count_only mode, for deny decisions
// and uncosted zero reservations. Unless a ceiling denies it, it adds one to
// count and to the feature's count field, registers the decision as a
// pending change of the counter until EndChange and writes no reservation.
func (counters *Counters) CountOnly(ctx context.Context, request ReserveRequest) (ReserveResult, error) {
	return counters.runReserve(ctx, reserveModeCountOnly, request)
}

// EndChange removes the pending change that Reserve or CountOnly registered
// for request, once the check stored its decision or gave up on it.
func (counters *Counters) EndChange(ctx context.Context, request ReserveRequest) error {
	counterKey := counters.CounterKey(request.Environment, request.CustomerID, request.PeriodStart)
	if err := counters.cache.Redis().ZRem(ctx, counterKey+pendingChangesKeySuffix, request.DecisionID.String()).Err(); err != nil {
		return fmt.Errorf("end counter change: %w", err)
	}
	return nil
}

// BeginChange registers settlement.ChangeID as a pending change of the
// settlement's counter that started at startedAt. A report or correction
// calls it inside the transaction that stores its ledger entry, before the
// commit, and runs Settle or SettleUnreserved after it.
func (counters *Counters) BeginChange(ctx context.Context, settlement Settlement, startedAt time.Time) error {
	counterKey := counters.CounterKey(settlement.Environment, settlement.CustomerID, settlement.PeriodStart)
	reply, err := counters.cache.RunScript(ctx, counters.beginChangeScript,
		[]string{counterKey + pendingChangesKeySuffix, counters.cache.Key(countersReadyKeyName)},
		settlement.ChangeID.String(), formatInteger(startedAt.UnixMilli()), formatUnix(counterExpiry(settlement.PeriodEnd)))
	if err != nil {
		return err
	}
	return expectReply(beginChangeScriptName, reply, replyPending)
}

// Settle finishes the pending change settlement.ChangeID with the usage of
// the reservation of decisionID. While the reservation is reserved it
// subtracts the reserved amount from the reserved fields, marks the
// reservation settled and removes it from the open reservations and from
// reservations_expiring. Either way it adds settlement.Amount to the settled
// fields, and the counter then expires seven days after the period end. It
// returns ErrChangeNotPending and changes nothing when the change is not
// pending.
func (counters *Counters) Settle(ctx context.Context, decisionID uuid.UUID, settlement Settlement) error {
	counterKey := counters.CounterKey(settlement.Environment, settlement.CustomerID, settlement.PeriodStart)
	reply, err := counters.cache.RunScript(ctx, counters.settleScript,
		[]string{
			counters.ReservationKey(decisionID),
			counters.cache.Key(reservationsExpiringKeyName),
			counterKey,
			counterKey + pendingChangesKeySuffix,
			counterKey + openReservationsKeySuffix,
			counters.cache.Key(countersReadyKeyName),
		},
		settlement.ChangeID.String(), settlement.Feature, formatInteger(settlement.Amount), formatUnix(counterExpiry(settlement.PeriodEnd)))
	if err != nil {
		return err
	}
	return expectReply(settleScriptName, reply, replySettled)
}

// Release subtracts the reserved amount of decisionID from the reserved
// fields, sets the reservation to nextStatus and removes it from the open
// reservations and from reservations_expiring. nextStatus must be
// ReservationStatusReleased or ReservationStatusExpired. Any other status
// returns an error. A reservation that is not reserved is left alone and
// reported with Applied false.
func (counters *Counters) Release(ctx context.Context, decisionID uuid.UUID, nextStatus ReservationStatus) (TransitionResult, error) {
	if nextStatus != ReservationStatusReleased && nextStatus != ReservationStatusExpired {
		return TransitionResult{}, fmt.Errorf("release status %q is neither released nor expired", nextStatus)
	}
	reply, err := counters.cache.RunScript(ctx, counters.releaseScript,
		[]string{counters.ReservationKey(decisionID), counters.cache.Key(reservationsExpiringKeyName), counters.cache.Key(countersReadyKeyName)},
		string(nextStatus), openReservationsKeySuffix)
	if err != nil {
		return TransitionResult{}, err
	}
	return parseTransition(releaseScriptName, reply)
}

// SettleUnreserved finishes the pending change settlement.ChangeID with
// usage that holds no reservation, for client fallbacks and corrections: it
// adds settlement.Amount to settled and the feature's settled field and
// settlement.CountIncrement to count and the feature's count field. The
// counter then expires seven days after the period end. It returns
// ErrChangeNotPending and changes nothing when the change is not pending.
func (counters *Counters) SettleUnreserved(ctx context.Context, settlement Settlement) error {
	counterKey := counters.CounterKey(settlement.Environment, settlement.CustomerID, settlement.PeriodStart)
	reply, err := counters.cache.RunScript(ctx, counters.settleUnreservedScript,
		[]string{counterKey, counterKey + pendingChangesKeySuffix, counters.cache.Key(countersReadyKeyName)},
		settlement.ChangeID.String(), settlement.Feature, formatInteger(settlement.Amount), formatInteger(settlement.CountIncrement),
		formatUnix(counterExpiry(settlement.PeriodEnd)))
	if err != nil {
		return err
	}
	return expectReply(settleUnreservedScriptName, reply, replySettled)
}

// ExpiredReservations returns the decision ids in reservations_expiring whose
// expiry is at or before now, earliest first, at most limit of them. They
// stay in the set until Release or Settle removes them. A limit below 1
// returns an error.
func (counters *Counters) ExpiredReservations(ctx context.Context, now time.Time, limit int) ([]uuid.UUID, error) {
	if limit < minimumExpiredLimit {
		return nil, fmt.Errorf("expired reservations limit %d is below %d", limit, minimumExpiredLimit)
	}
	members, err := counters.cache.Redis().ZRangeArgs(ctx, redis.ZRangeArgs{
		Key:     counters.cache.Key(reservationsExpiringKeyName),
		Start:   "-inf",
		Stop:    now.Unix(),
		ByScore: true,
		Count:   int64(limit),
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("read expired reservations: %w", err)
	}
	decisionIDs := make([]uuid.UUID, 0, len(members))
	for _, member := range members {
		decisionID, err := uuid.Parse(member)
		if err != nil {
			return nil, fmt.Errorf("parse expiring reservation %q: %w", member, err)
		}
		decisionIDs = append(decisionIDs, decisionID)
	}
	return decisionIDs, nil
}

// Overwrite sets each field of the counter hash at key, a key from
// CounterKey, to its value and makes the counter expire seven days after
// periodEnd. Other fields keep their values. It also drops the counter's
// pending changes, so a settlement registered before the values were read
// never applies on top of them. The bootstrap rebuild uses it.
func (counters *Counters) Overwrite(ctx context.Context, key string, fields map[string]int64, periodEnd time.Time) error {
	values := make([]any, 0, 2*len(fields))
	for field, value := range fields {
		values = append(values, field, value)
	}
	pipeline := counters.cache.Redis().TxPipeline()
	pipeline.Del(ctx, key+pendingChangesKeySuffix)
	pipeline.HSet(ctx, key, values...)
	pipeline.ExpireAt(ctx, key, counterExpiry(periodEnd))
	if _, err := pipeline.Exec(ctx); err != nil {
		return fmt.Errorf("overwrite counter: %w", err)
	}
	return nil
}

// RestoreReservation writes the reservation hash of request and adds it to
// the counter's open reservations and to reservations_expiring, exactly as
// Reserve does, without checking ceilings or changing the counter. The
// bootstrap rebuild uses it for decisions that are still reserved.
func (counters *Counters) RestoreReservation(ctx context.Context, request ReserveRequest) error {
	reservationKey := counters.ReservationKey(request.DecisionID)
	counterKey := counters.CounterKey(request.Environment, request.CustomerID, request.PeriodStart)
	expiresAtUnix := unixRoundedUp(request.ExpiresAt)
	pipeline := counters.cache.Redis().TxPipeline()
	pipeline.HSet(ctx, reservationKey,
		reservationFieldCounterKey, counterKey,
		reservationFieldFeature, request.Feature,
		reservationFieldAmount, int64(request.Amount),
		reservationFieldStatus, string(ReservationStatusReserved),
		reservationFieldExpiresAtUnix, expiresAtUnix,
	)
	pipeline.ExpireAt(ctx, reservationKey, reservationExpiry(request.ExpiresAt))
	pipeline.SAdd(ctx, counterKey+openReservationsKeySuffix, request.DecisionID.String())
	pipeline.ExpireAt(ctx, counterKey+openReservationsKeySuffix, counterExpiry(request.PeriodEnd))
	pipeline.ZAdd(ctx, counters.cache.Key(reservationsExpiringKeyName), redis.Z{Score: float64(expiresAtUnix), Member: request.DecisionID.String()})
	if _, err := pipeline.Exec(ctx); err != nil {
		return fmt.Errorf("restore reservation: %w", err)
	}
	return nil
}

// SetReady sets the counters_ready marker to rebuiltAt, the time the
// bootstrap rebuild finished.
func (counters *Counters) SetReady(ctx context.Context, rebuiltAt time.Time) error {
	if err := counters.cache.Redis().Set(ctx, counters.cache.Key(countersReadyKeyName), rebuiltAt.UTC().Format(time.RFC3339Nano), 0).Err(); err != nil {
		return fmt.Errorf("set counters ready: %w", err)
	}
	return nil
}

// IsReady reports whether the counters_ready marker exists.
func (counters *Counters) IsReady(ctx context.Context) (bool, error) {
	existing, err := counters.cache.Redis().Exists(ctx, counters.cache.Key(countersReadyKeyName)).Result()
	if err != nil {
		return false, fmt.Errorf("read counters ready: %w", err)
	}
	return existing == 1, nil
}

func (counters *Counters) runReserve(ctx context.Context, mode string, request ReserveRequest) (ReserveResult, error) {
	counterKey := counters.CounterKey(request.Environment, request.CustomerID, request.PeriodStart)
	keys := []string{
		counterKey,
		counters.ReservationKey(request.DecisionID),
		counters.cache.Key(reservationsExpiringKeyName),
		counters.cache.Key(countersReadyKeyName),
		counterKey + pendingChangesKeySuffix,
		counterKey + openReservationsKeySuffix,
	}
	reply, err := counters.cache.RunScript(ctx, counters.reserveScript, keys,
		mode,
		request.DecisionID.String(),
		request.Feature,
		formatInteger(request.Amount),
		formatCeiling(request.Ceilings.Allowance),
		formatCeiling(request.Ceilings.FeatureAmount),
		formatCeiling(request.Ceilings.FeatureCount),
		formatInteger(unixRoundedUp(request.ExpiresAt)),
		formatUnix(counterExpiry(request.PeriodEnd)),
		formatUnix(reservationExpiry(request.ExpiresAt)),
		formatCeiling(request.Ceilings.TotalAmount),
		formatCeiling(request.Ceilings.TotalCount),
		formatInteger(request.DecidedAt.UnixMilli()),
	)
	if err != nil {
		return "", err
	}
	parts, err := replyParts(reserveScriptName, reply)
	if err != nil {
		return "", err
	}
	if len(parts) == 1 && parts[0] == replyNotReady {
		return "", ErrCountersNotReady
	}
	result := ReserveResult(strings.Join(parts, replyPartSeparator))
	switch result {
	case ReserveResultReserved, ReserveResultCounted, ReserveResultDeniedAllowance, ReserveResultDeniedLimit:
		return result, nil
	}
	return "", fmt.Errorf("unexpected %s script reply %q", reserveScriptName, parts)
}

func parseCounter(fields map[string]string) (signals.CounterSnapshot, error) {
	var totals signals.FeatureCounter
	features := make(map[string]signals.FeatureCounter)
	for field, text := range fields {
		name, feature, isFeatureField := strings.Cut(field, featureFieldSeparator)
		counter := totals
		if isFeatureField {
			counter = features[feature]
		}
		if err := setCounterField(&counter, name, text); err != nil {
			return signals.CounterSnapshot{}, fmt.Errorf("parse counter field %s: %w", field, err)
		}
		if isFeatureField {
			features[feature] = counter
		} else {
			totals = counter
		}
	}
	return signals.CounterSnapshot{Settled: totals.Settled, Reserved: totals.Reserved, Count: totals.Count, Features: features}, nil
}

func setCounterField(counter *signals.FeatureCounter, name string, text string) error {
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return err
	}
	switch name {
	case fieldSettled:
		counter.Settled = money.Amount(value)
	case fieldReserved:
		counter.Reserved = money.Amount(value)
	case fieldCount:
		counter.Count = value
	default:
		return fmt.Errorf("unknown counter field %s", name)
	}
	return nil
}

func parseTransition(scriptName string, reply any) (TransitionResult, error) {
	parts, err := replyParts(scriptName, reply)
	if err != nil {
		return TransitionResult{}, err
	}
	if len(parts) == 1 && parts[0] == replyNotReady {
		return TransitionResult{}, ErrCountersNotReady
	}
	if len(parts) == 1 {
		return TransitionResult{Applied: true, Status: ReservationStatus(parts[0])}, nil
	}
	if len(parts) == 2 && parts[0] == replyNoop {
		return TransitionResult{Applied: false, Status: ReservationStatus(parts[1])}, nil
	}
	return TransitionResult{}, fmt.Errorf("unexpected %s script reply %q", scriptName, parts)
}

func expectReply(scriptName string, reply any, want string) error {
	parts, err := replyParts(scriptName, reply)
	if err != nil {
		return err
	}
	if len(parts) != 1 {
		return fmt.Errorf("unexpected %s script reply %q", scriptName, parts)
	}
	switch parts[0] {
	case want:
		return nil
	case replyNotReady:
		return ErrCountersNotReady
	case replyNotPending:
		return ErrChangeNotPending
	}
	return fmt.Errorf("unexpected %s script reply %q", scriptName, parts)
}

func replyParts(scriptName string, reply any) ([]string, error) {
	values, isList := reply.([]any)
	if !isList {
		return nil, fmt.Errorf("unexpected %s script reply %v", scriptName, reply)
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		part, isString := value.(string)
		if !isString {
			return nil, fmt.Errorf("unexpected %s script reply %v", scriptName, reply)
		}
		parts = append(parts, part)
	}
	return parts, nil
}

func featureField(name string, feature string) string {
	return name + featureFieldSeparator + feature
}

func counterExpiry(periodEnd time.Time) time.Time {
	return periodEnd.Add(counterRetention)
}

func reservationExpiry(expiresAt time.Time) time.Time {
	return expiresAt.Add(reservationRetention)
}

// unixRoundedUp returns the first whole Unix second at or after moment.
// Rounding down would let ExpiredReservations return a reservation up to a
// second before its expires_at.
func unixRoundedUp(moment time.Time) int64 {
	seconds := moment.Unix()
	if moment.Nanosecond() > 0 {
		seconds++
	}
	return seconds
}

func formatUnix(moment time.Time) string {
	return formatInteger(moment.Unix())
}

func formatCeiling[Integer ~int64](ceiling *Integer) string {
	if ceiling == nil {
		return noCeiling
	}
	return formatInteger(*ceiling)
}

func formatInteger[Integer ~int64](value Integer) string {
	return strconv.FormatInt(int64(value), 10)
}
