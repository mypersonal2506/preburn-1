package decisions_test

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/cache/cachetest"
	"github.com/preburn/preburn/internal/decisions"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/signals"
)

const (
	chatFeature  = "chat"
	imageFeature = "image"
	daySeconds   = 24 * 60 * 60
)

type counterFixture struct {
	cache       *cache.Client
	counters    *decisions.Counters
	customerID  uuid.UUID
	periodStart time.Time
	periodEnd   time.Time
	expiresAt   time.Time
}

func TestReserveIncrementsCounterAndWritesReservation(t *testing.T) {
	t.Parallel()
	fixture := newCounterFixture(t)
	chatRequest := fixture.request(chatFeature, 1500, decisions.Ceilings{})
	imageRequest := fixture.request(imageFeature, 200, decisions.Ceilings{})
	imageRequest.ExpiresAt = fixture.expiresAt.Add(time.Minute)

	for _, request := range []decisions.ReserveRequest{chatRequest, imageRequest} {
		result, err := fixture.counters.Reserve(t.Context(), request)
		if err != nil || result != decisions.ReserveResultReserved {
			t.Fatalf("reserve %s = %q err=%v, want reserved", request.Feature, result, err)
		}
	}

	fixture.assertCounter(t, map[string]string{
		"reserved": "1700", "count": "2",
		"reserved:chat": "1500", "count:chat": "1",
		"reserved:image": "200", "count:image": "1",
	})
	fixture.assertExpireAt(t, fixture.counterKey(), fixture.periodEnd.Unix()+7*daySeconds)
	fixture.assertHash(t, fixture.counters.ReservationKey(chatRequest.DecisionID), map[string]string{
		"counter_key": fixture.counterKey(), "feature": chatFeature, "amount": "1500",
		"status": "reserved", "expires_at_unix": strconv.FormatInt(fixture.expiresAt.Unix()+1, 10),
	})
	fixture.assertExpireAt(t, fixture.counters.ReservationKey(chatRequest.DecisionID), fixture.expiresAt.Unix()+daySeconds)
	members, err := fixture.cache.Redis().ZRangeWithScores(t.Context(), fixture.cache.Key("reservations_expiring"), 0, -1).Result()
	if err != nil {
		t.Fatalf("read reservations_expiring: %v", err)
	}
	want := []redis.Z{
		{Score: float64(fixture.expiresAt.Unix() + 1), Member: chatRequest.DecisionID.String()},
		{Score: float64(fixture.expiresAt.Unix() + 61), Member: imageRequest.DecisionID.String()},
	}
	if diff := cmp.Diff(want, members); diff != "" {
		t.Errorf("reservations_expiring mismatch (-want +got):\n%s", diff)
	}
}

func TestReserveDeniesAboveCeilings(t *testing.T) {
	t.Parallel()
	reserve := (*decisions.Counters).Reserve
	countOnly := (*decisions.Counters).CountOnly
	testCases := []struct {
		name     string
		call     func(*decisions.Counters, context.Context, decisions.ReserveRequest) (decisions.ReserveResult, error)
		amount   money.Amount
		ceilings decisions.Ceilings
		want     decisions.ReserveResult
	}{
		{name: "no ceilings", call: reserve, amount: 100, want: decisions.ReserveResultReserved},
		{name: "allowance reached exactly", call: reserve, amount: 100, ceilings: decisions.Ceilings{Allowance: amountPointer(1100)}, want: decisions.ReserveResultReserved},
		{name: "allowance exceeded", call: reserve, amount: 100, ceilings: decisions.Ceilings{Allowance: amountPointer(1099)}, want: decisions.ReserveResultDeniedAllowance},
		{name: "negative allowance", call: reserve, amount: 0, ceilings: decisions.Ceilings{Allowance: amountPointer(-1)}, want: decisions.ReserveResultDeniedAllowance},
		{name: "feature amount reached exactly", call: reserve, amount: 100, ceilings: decisions.Ceilings{FeatureAmount: amountPointer(1000)}, want: decisions.ReserveResultReserved},
		{name: "feature amount exceeded", call: reserve, amount: 100, ceilings: decisions.Ceilings{FeatureAmount: amountPointer(999)}, want: decisions.ReserveResultDeniedLimit},
		{name: "feature count reached exactly", call: reserve, amount: 100, ceilings: decisions.Ceilings{FeatureCount: countPointer(3)}, want: decisions.ReserveResultReserved},
		{name: "feature count exceeded", call: reserve, amount: 100, ceilings: decisions.Ceilings{FeatureCount: countPointer(2)}, want: decisions.ReserveResultDeniedLimit},
		{name: "allowance checked before feature limits", call: reserve, amount: 100, ceilings: decisions.Ceilings{Allowance: amountPointer(1099), FeatureAmount: amountPointer(999), FeatureCount: countPointer(2)}, want: decisions.ReserveResultDeniedAllowance},
		{name: "total amount reached exactly", call: reserve, amount: 100, ceilings: decisions.Ceilings{TotalAmount: amountPointer(1100)}, want: decisions.ReserveResultReserved},
		{name: "total amount exceeded", call: reserve, amount: 100, ceilings: decisions.Ceilings{TotalAmount: amountPointer(1099), FeatureAmount: amountPointer(1000)}, want: decisions.ReserveResultDeniedLimit},
		{name: "total count reached exactly", call: reserve, amount: 100, ceilings: decisions.Ceilings{TotalCount: countPointer(4)}, want: decisions.ReserveResultReserved},
		{name: "total count exceeded", call: reserve, amount: 100, ceilings: decisions.Ceilings{TotalCount: countPointer(3), FeatureCount: countPointer(3)}, want: decisions.ReserveResultDeniedLimit},
		{name: "allowance checked before total limits", call: reserve, amount: 100, ceilings: decisions.Ceilings{Allowance: amountPointer(1099), TotalAmount: amountPointer(1099)}, want: decisions.ReserveResultDeniedAllowance},
		{name: "count only without ceilings", call: countOnly, amount: 0, want: decisions.ReserveResultCounted},
		{name: "count only above feature count", call: countOnly, amount: 0, ceilings: decisions.Ceilings{FeatureCount: countPointer(2)}, want: decisions.ReserveResultDeniedLimit},
		{name: "count only above total count", call: countOnly, amount: 0, ceilings: decisions.Ceilings{TotalCount: countPointer(3)}, want: decisions.ReserveResultDeniedLimit},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			fixture := newCounterFixture(t)
			fixture.settleUnreserved(t, 600, 1)
			fixture.reserve(t, chatFeature, 300)
			fixture.reserve(t, imageFeature, 100)
			before := fixture.counterFields(t)
			request := fixture.request(chatFeature, testCase.amount, testCase.ceilings)

			result, err := testCase.call(fixture.counters, t.Context(), request)
			if err != nil {
				t.Fatalf("reserve: %v", err)
			}
			if result != testCase.want {
				t.Errorf("result = %q, want %q", result, testCase.want)
			}
			if result == decisions.ReserveResultDeniedAllowance || result == decisions.ReserveResultDeniedLimit {
				fixture.assertCounter(t, before)
				fixture.assertHash(t, fixture.counters.ReservationKey(request.DecisionID), map[string]string{})
				fixture.assertNotExpiring(t, request.DecisionID)
			}
		})
	}
}

func TestReserveComparesAmountsAbove2To53Exactly(t *testing.T) {
	t.Parallel()
	fixture := newCounterFixture(t)
	fixture.settleUnreserved(t, 999_999_999_999_999_000, 0)
	ceiling := amountPointer(999_999_999_999_999_999)

	result, err := fixture.counters.Reserve(t.Context(), fixture.request(chatFeature, 1000, decisions.Ceilings{Allowance: ceiling}))
	if err != nil || result != decisions.ReserveResultDeniedAllowance {
		t.Errorf("reserve one nano above the ceiling = %q err=%v, want denied_allowance", result, err)
	}
	result, err = fixture.counters.Reserve(t.Context(), fixture.request(chatFeature, 1000, decisions.Ceilings{TotalAmount: ceiling}))
	if err != nil || result != decisions.ReserveResultDeniedLimit {
		t.Errorf("reserve one nano above the total ceiling = %q err=%v, want denied_limit", result, err)
	}
	result, err = fixture.counters.Reserve(t.Context(), fixture.request(chatFeature, 999, decisions.Ceilings{Allowance: ceiling}))
	if err != nil || result != decisions.ReserveResultReserved {
		t.Errorf("reserve up to the ceiling = %q err=%v, want reserved", result, err)
	}
	fixture.assertCounter(t, map[string]string{
		"settled": "999999999999999000", "settled:chat": "999999999999999000",
		"reserved": "999", "reserved:chat": "999", "count": "1", "count:chat": "1",
	})
}

func TestReserveNeverExceedsCeilingUnderConcurrency(t *testing.T) {
	t.Parallel()
	const (
		reservers = 100
		ceiling   = 37
	)
	fixture := newCounterFixture(t)
	results := make(chan decisions.ReserveResult, reservers)
	var waitGroup sync.WaitGroup
	for range reservers {
		waitGroup.Go(func() {
			result, err := fixture.counters.Reserve(t.Context(), fixture.request(chatFeature, 1, decisions.Ceilings{Allowance: amountPointer(ceiling)}))
			if err != nil {
				t.Errorf("reserve: %v", err)
			}
			results <- result
		})
	}
	waitGroup.Wait()
	close(results)

	counts := map[decisions.ReserveResult]int{}
	for result := range results {
		counts[result]++
	}
	want := map[decisions.ReserveResult]int{decisions.ReserveResultReserved: ceiling, decisions.ReserveResultDeniedAllowance: reservers - ceiling}
	if diff := cmp.Diff(want, counts); diff != "" {
		t.Errorf("results mismatch (-want +got):\n%s", diff)
	}
	fixture.assertCounter(t, map[string]string{"reserved": "37", "reserved:chat": "37", "count": "37", "count:chat": "37"})
}

func TestCountOnlyIncrementsOnlyCounts(t *testing.T) {
	t.Parallel()
	fixture := newCounterFixture(t)
	request := fixture.request(chatFeature, 0, decisions.Ceilings{})

	result, err := fixture.counters.CountOnly(t.Context(), request)
	if err != nil || result != decisions.ReserveResultCounted {
		t.Fatalf("count only = %q err=%v, want counted", result, err)
	}

	fixture.assertCounter(t, map[string]string{"count": "1", "count:chat": "1"})
	fixture.assertExpireAt(t, fixture.counterKey(), fixture.periodEnd.Unix()+7*daySeconds)
	fixture.assertHash(t, fixture.counters.ReservationKey(request.DecisionID), map[string]string{})
	fixture.assertNotExpiring(t, request.DecisionID)
}

func TestSettleMovesReservedToSettled(t *testing.T) {
	t.Parallel()
	fixture := newCounterFixture(t)
	decisionID := fixture.reserve(t, chatFeature, 1500)
	fixture.reserve(t, imageFeature, 200)
	settlement := fixture.beginChange(t, 1200, 0)

	if err := fixture.counters.Settle(t.Context(), decisionID, settlement); err != nil {
		t.Fatalf("settle: %v", err)
	}

	settled := map[string]string{
		"settled": "1200", "reserved": "200", "count": "2",
		"settled:chat": "1200", "reserved:chat": "0", "count:chat": "1",
		"reserved:image": "200", "count:image": "1",
	}
	fixture.assertCounter(t, settled)
	fixture.assertStatus(t, decisionID, decisions.ReservationStatusSettled)
	fixture.assertNotExpiring(t, decisionID)
	fixture.assertOpenReservations(t, 1)
	fixture.assertNoPendingChanges(t)

	if err := fixture.counters.Settle(t.Context(), decisionID, settlement); !errors.Is(err, decisions.ErrChangeNotPending) {
		t.Errorf("settle again = %v, want ErrChangeNotPending", err)
	}
	result, err := fixture.counters.Release(t.Context(), decisionID, decisions.ReservationStatusReleased)
	if err != nil {
		t.Fatalf("release after settle: %v", err)
	}
	if diff := cmp.Diff(decisions.TransitionResult{Applied: false, Status: decisions.ReservationStatusSettled}, result); diff != "" {
		t.Errorf("release after settle result mismatch (-want +got):\n%s", diff)
	}
	fixture.assertCounter(t, settled)
}

func TestSettleAfterReleaseAddsTheCostWithoutReservation(t *testing.T) {
	t.Parallel()
	for _, status := range []decisions.ReservationStatus{decisions.ReservationStatusExpired, decisions.ReservationStatusReleased} {
		t.Run(string(status), func(t *testing.T) {
			t.Parallel()
			fixture := newCounterFixture(t)
			decisionID := fixture.reserve(t, chatFeature, 1500)
			fixture.reserve(t, chatFeature, 100)

			result, err := fixture.counters.Release(t.Context(), decisionID, status)
			if err != nil {
				t.Fatalf("release: %v", err)
			}
			if diff := cmp.Diff(decisions.TransitionResult{Applied: true, Status: status}, result); diff != "" {
				t.Errorf("release result mismatch (-want +got):\n%s", diff)
			}
			fixture.assertCounter(t, map[string]string{"reserved": "100", "reserved:chat": "100", "count": "2", "count:chat": "2"})
			fixture.assertStatus(t, decisionID, status)
			fixture.assertNotExpiring(t, decisionID)
			fixture.assertOpenReservations(t, 1)

			if err := fixture.counters.Settle(t.Context(), decisionID, fixture.beginChange(t, 1400, 0)); err != nil {
				t.Fatalf("settle after release: %v", err)
			}
			fixture.assertCounter(t, map[string]string{
				"settled": "1400", "settled:chat": "1400",
				"reserved": "100", "reserved:chat": "100",
				"count": "2", "count:chat": "2",
			})
			fixture.assertStatus(t, decisionID, status)
		})
	}
}

func TestReleaseOfUnknownReservationIsANoop(t *testing.T) {
	t.Parallel()
	fixture := newCounterFixture(t)

	result, err := fixture.counters.Release(t.Context(), uuid.New(), decisions.ReservationStatusExpired)
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if diff := cmp.Diff(decisions.TransitionResult{Applied: false, Status: decisions.ReservationStatusMissing}, result); diff != "" {
		t.Errorf("release result mismatch (-want +got):\n%s", diff)
	}
}

func TestSettlementsNeedAPendingChange(t *testing.T) {
	t.Parallel()
	fixture := newCounterFixture(t)
	decisionID := fixture.reserve(t, chatFeature, 1500)
	before := fixture.counterFields(t)
	settlement := decisions.Settlement{
		Environment: httpapi.EnvironmentTest,
		CustomerID:  fixture.customerID,
		PeriodStart: fixture.periodStart,
		PeriodEnd:   fixture.periodEnd,
		ChangeID:    uuid.New(),
		Feature:     chatFeature,
		Amount:      700,
	}

	if err := fixture.counters.Settle(t.Context(), decisionID, settlement); !errors.Is(err, decisions.ErrChangeNotPending) {
		t.Errorf("settle = %v, want ErrChangeNotPending", err)
	}
	if err := fixture.counters.SettleUnreserved(t.Context(), settlement); !errors.Is(err, decisions.ErrChangeNotPending) {
		t.Errorf("settle unreserved = %v, want ErrChangeNotPending", err)
	}

	fixture.assertCounter(t, before)
	fixture.assertStatus(t, decisionID, decisions.ReservationStatusReserved)
}

func TestChecksAndSettlementsRegisterPendingChanges(t *testing.T) {
	t.Parallel()
	fixture := newCounterFixture(t)
	decidedAt := fixture.periodStart.Add(time.Minute + 250*time.Millisecond)
	reserved := fixture.request(chatFeature, 1500, decisions.Ceilings{})
	reserved.DecidedAt = decidedAt
	counted := fixture.request(chatFeature, 0, decisions.Ceilings{})
	counted.DecidedAt = decidedAt
	startedAt := decidedAt.Add(time.Second)
	settlement := decisions.Settlement{
		Environment: httpapi.EnvironmentTest,
		CustomerID:  fixture.customerID,
		PeriodStart: fixture.periodStart,
		PeriodEnd:   fixture.periodEnd,
		ChangeID:    uuid.New(),
		Feature:     chatFeature,
		Amount:      300,
	}

	if result, err := fixture.counters.Reserve(t.Context(), reserved); err != nil || result != decisions.ReserveResultReserved {
		t.Fatalf("reserve = %q err=%v, want reserved", result, err)
	}
	if result, err := fixture.counters.CountOnly(t.Context(), counted); err != nil || result != decisions.ReserveResultCounted {
		t.Fatalf("count only = %q err=%v, want counted", result, err)
	}
	if err := fixture.counters.BeginChange(t.Context(), settlement, startedAt); err != nil {
		t.Fatalf("begin change: %v", err)
	}

	want := []redis.Z{
		{Score: float64(decidedAt.UnixMilli()), Member: counted.DecisionID.String()},
		{Score: float64(decidedAt.UnixMilli()), Member: reserved.DecisionID.String()},
		{Score: float64(startedAt.UnixMilli()), Member: settlement.ChangeID.String()},
	}
	if diff := cmp.Diff(want, fixture.pendingChanges(t), cmpopts.SortSlices(func(left, right redis.Z) bool {
		return left.Member.(string) < right.Member.(string)
	})); diff != "" {
		t.Errorf("pending changes mismatch (-want +got):\n%s", diff)
	}
	fixture.assertExpireAt(t, fixture.counterKey()+":pending", fixture.periodEnd.Unix()+7*daySeconds)

	for _, request := range []decisions.ReserveRequest{reserved, counted} {
		if err := fixture.counters.EndChange(t.Context(), request); err != nil {
			t.Fatalf("end change: %v", err)
		}
	}
	if err := fixture.counters.SettleUnreserved(t.Context(), settlement); err != nil {
		t.Fatalf("settle unreserved: %v", err)
	}
	fixture.assertNoPendingChanges(t)
}

func TestCounterScriptsRefuseWhileCountersAreNotReady(t *testing.T) {
	t.Parallel()
	fixture := newCounterFixture(t)
	decisionID := fixture.reserve(t, chatFeature, 1500)
	settlement := fixture.beginChange(t, 1200, 1)
	before := fixture.counterFields(t)
	deleteCountersMarker(t, fixture.cache)

	_, reserveErr := fixture.counters.Reserve(t.Context(), fixture.request(chatFeature, 100, decisions.Ceilings{}))
	_, countErr := fixture.counters.CountOnly(t.Context(), fixture.request(chatFeature, 0, decisions.Ceilings{}))
	_, releaseErr := fixture.counters.Release(t.Context(), decisionID, decisions.ReservationStatusReleased)
	beginErr := fixture.counters.BeginChange(t.Context(), settlement, fixture.periodStart)
	settleErr := fixture.counters.Settle(t.Context(), decisionID, settlement)

	for name, err := range map[string]error{"reserve": reserveErr, "count only": countErr, "release": releaseErr, "begin change": beginErr, "settle": settleErr} {
		if !errors.Is(err, decisions.ErrCountersNotReady) {
			t.Errorf("%s = %v, want ErrCountersNotReady", name, err)
		}
	}
	fixture.assertCounter(t, before)
	fixture.assertStatus(t, decisionID, decisions.ReservationStatusReserved)
	fixture.assertNoPendingChanges(t)
	if err := fixture.counters.SetReady(t.Context(), fixture.periodStart); err != nil {
		t.Fatalf("set ready: %v", err)
	}
	if err := fixture.counters.SettleUnreserved(t.Context(), settlement); !errors.Is(err, decisions.ErrChangeNotPending) {
		t.Errorf("settle unreserved of a change dropped while not ready = %v, want ErrChangeNotPending", err)
	}
}

func TestReleaseRejectsStatusesOtherThanReleasedAndExpired(t *testing.T) {
	t.Parallel()
	fixture := newCounterFixture(t)
	decisionID := fixture.reserve(t, chatFeature, 1500)

	for _, status := range []decisions.ReservationStatus{decisions.ReservationStatusReserved, decisions.ReservationStatusSettled, decisions.ReservationStatusMissing, "unknown"} {
		if _, err := fixture.counters.Release(t.Context(), decisionID, status); err == nil {
			t.Errorf("release with status %q returned no error", status)
		}
	}
	fixture.assertStatus(t, decisionID, decisions.ReservationStatusReserved)
}

func TestSettleUnreservedAddsSettledAndCount(t *testing.T) {
	t.Parallel()
	fixture := newCounterFixture(t)

	fixture.settleUnreserved(t, 700, 1)
	fixture.settleUnreserved(t, 300, 0)

	fixture.assertCounter(t, map[string]string{"settled": "1000", "settled:chat": "1000", "count": "1", "count:chat": "1"})
	fixture.assertExpireAt(t, fixture.counterKey(), fixture.periodEnd.Unix()+7*daySeconds)
	fixture.assertNoPendingChanges(t)
}

func TestExpiredReservationsReturnsDueDecisionsEarliestFirst(t *testing.T) {
	t.Parallel()
	fixture := newCounterFixture(t)
	start := fixture.periodStart
	var decisionIDs []uuid.UUID
	for _, expiresAt := range []time.Time{start.Add(3 * time.Minute), start.Add(time.Minute), start.Add(2*time.Minute + 500*time.Millisecond)} {
		request := fixture.request(chatFeature, 100, decisions.Ceilings{})
		request.ExpiresAt = expiresAt
		if result, err := fixture.counters.Reserve(t.Context(), request); err != nil || result != decisions.ReserveResultReserved {
			t.Fatalf("reserve = %q err=%v, want reserved", result, err)
		}
		decisionIDs = append(decisionIDs, request.DecisionID)
	}

	testCases := []struct {
		name  string
		now   time.Time
		limit int
		want  []uuid.UUID
	}{
		{name: "none due", now: start.Add(time.Minute - time.Second), limit: 10, want: []uuid.UUID{}},
		{name: "due at its expiry", now: start.Add(time.Minute), limit: 10, want: []uuid.UUID{decisionIDs[1]}},
		{name: "partial second rounds up", now: start.Add(2*time.Minute + 999*time.Millisecond), limit: 10, want: []uuid.UUID{decisionIDs[1]}},
		{name: "all due", now: start.Add(time.Hour), limit: 10, want: []uuid.UUID{decisionIDs[1], decisionIDs[2], decisionIDs[0]}},
		{name: "limited", now: start.Add(time.Hour), limit: 2, want: []uuid.UUID{decisionIDs[1], decisionIDs[2]}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := fixture.counters.ExpiredReservations(t.Context(), testCase.now, testCase.limit)
			if err != nil {
				t.Fatalf("expired reservations: %v", err)
			}
			if diff := cmp.Diff(testCase.want, got); diff != "" {
				t.Errorf("expired reservations mismatch (-want +got):\n%s", diff)
			}
		})
	}
	if _, err := fixture.counters.ExpiredReservations(t.Context(), start.Add(time.Hour), 0); err == nil {
		t.Error("expired reservations with limit 0 returned no error")
	}
}

func TestOverwriteSetsFieldsAndExpiryAndDropsPendingChanges(t *testing.T) {
	t.Parallel()
	fixture := newCounterFixture(t)
	fixture.reserve(t, chatFeature, 1500)
	settlement := fixture.beginChange(t, 1500, 0)

	fields := map[string]int64{"reserved": 0, "reserved:chat": 0, "settled": 1500, "settled:chat": 1500}
	if err := fixture.counters.Overwrite(t.Context(), fixture.counterKey(), fields, fixture.periodEnd); err != nil {
		t.Fatalf("overwrite: %v", err)
	}

	fixture.assertCounter(t, map[string]string{
		"reserved": "0", "reserved:chat": "0", "settled": "1500", "settled:chat": "1500", "count": "1", "count:chat": "1",
	})
	fixture.assertExpireAt(t, fixture.counterKey(), fixture.periodEnd.Unix()+7*daySeconds)
	fixture.assertNoPendingChanges(t)
	if err := fixture.counters.SettleUnreserved(t.Context(), settlement); !errors.Is(err, decisions.ErrChangeNotPending) {
		t.Errorf("settle unreserved after the overwrite = %v, want ErrChangeNotPending", err)
	}
}

func TestRestoreReservationWritesReservationWithoutCounting(t *testing.T) {
	t.Parallel()
	fixture := newCounterFixture(t)
	request := fixture.request(chatFeature, 1500, decisions.Ceilings{})

	if err := fixture.counters.RestoreReservation(t.Context(), request); err != nil {
		t.Fatalf("restore reservation: %v", err)
	}

	fixture.assertCounter(t, map[string]string{})
	key := fixture.counters.ReservationKey(request.DecisionID)
	fixture.assertHash(t, key, map[string]string{
		"counter_key": fixture.counterKey(), "feature": chatFeature, "amount": "1500",
		"status": "reserved", "expires_at_unix": strconv.FormatInt(fixture.expiresAt.Unix()+1, 10),
	})
	fixture.assertExpireAt(t, key, fixture.expiresAt.Unix()+daySeconds)
	fixture.assertOpenReservations(t, 1)
	got, err := fixture.counters.ExpiredReservations(t.Context(), fixture.expiresAt.Add(time.Second), 10)
	if err != nil {
		t.Fatalf("expired reservations: %v", err)
	}
	if diff := cmp.Diff([]uuid.UUID{request.DecisionID}, got); diff != "" {
		t.Errorf("expired reservations mismatch (-want +got):\n%s", diff)
	}
}

func TestSnapshotReadsTotalsAndOneFeature(t *testing.T) {
	t.Parallel()
	fixture := newCounterFixture(t)

	empty, err := fixture.counters.Snapshot(t.Context(), httpapi.EnvironmentTest, fixture.customerID, fixture.periodStart, chatFeature)
	if err != nil {
		t.Fatalf("snapshot of a missing counter: %v", err)
	}
	if diff := cmp.Diff(signals.CounterSnapshot{Features: map[string]signals.FeatureCounter{chatFeature: {}}}, empty); diff != "" {
		t.Errorf("missing counter snapshot mismatch (-want +got):\n%s", diff)
	}

	fixture.settleUnreserved(t, 600, 1)
	fixture.reserve(t, chatFeature, 1500)
	fixture.reserve(t, imageFeature, 200)
	snapshot, err := fixture.counters.Snapshot(t.Context(), httpapi.EnvironmentTest, fixture.customerID, fixture.periodStart, chatFeature)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	want := signals.CounterSnapshot{
		Settled:  600,
		Reserved: 1700,
		Count:    3,
		Features: map[string]signals.FeatureCounter{chatFeature: {Settled: 600, Reserved: 1500, Count: 2}},
	}
	if diff := cmp.Diff(want, snapshot); diff != "" {
		t.Errorf("snapshot mismatch (-want +got):\n%s", diff)
	}
}

func TestSnapshotManyReadsEveryFeatureOfEachCounter(t *testing.T) {
	t.Parallel()
	fixture := newCounterFixture(t)
	fixture.settleUnreserved(t, 600, 1)
	fixture.reserve(t, chatFeature, 1500)
	fixture.reserve(t, imageFeature, 200)
	idleCustomerID := uuid.New()

	snapshots, err := fixture.counters.SnapshotMany(t.Context(), httpapi.EnvironmentTest, map[uuid.UUID]time.Time{
		fixture.customerID: fixture.periodStart,
		idleCustomerID:     fixture.periodStart,
	})
	if err != nil {
		t.Fatalf("snapshot many: %v", err)
	}
	want := map[uuid.UUID]signals.CounterSnapshot{
		fixture.customerID: {
			Settled:  600,
			Reserved: 1700,
			Count:    3,
			Features: map[string]signals.FeatureCounter{
				chatFeature:  {Settled: 600, Reserved: 1500, Count: 2},
				imageFeature: {Reserved: 200, Count: 1},
			},
		},
		idleCustomerID: {Features: map[string]signals.FeatureCounter{}},
	}
	if diff := cmp.Diff(want, snapshots); diff != "" {
		t.Errorf("snapshots mismatch (-want +got):\n%s", diff)
	}
}

func TestSnapshotRejectsMalformedCounters(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name  string
		field string
		value string
	}{
		{name: "value that is not an integer", field: "settled:chat", value: "1.5"},
		{name: "unknown field", field: "spent:chat", value: "1"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			fixture := newCounterFixture(t)
			if err := fixture.cache.Redis().HSet(t.Context(), fixture.counterKey(), testCase.field, testCase.value).Err(); err != nil {
				t.Fatalf("write counter field: %v", err)
			}

			if _, err := fixture.counters.SnapshotMany(t.Context(), httpapi.EnvironmentTest, map[uuid.UUID]time.Time{fixture.customerID: fixture.periodStart}); err == nil {
				t.Error("snapshot many of a malformed counter returned no error")
			}
		})
	}
}

func TestReadyMarker(t *testing.T) {
	t.Parallel()
	fixture := newCounterFixture(t)
	deleteCountersMarker(t, fixture.cache)
	rebuiltAt := time.Date(2026, time.September, 26, 5, 0, 0, 123_000_000, time.UTC)

	ready, err := fixture.counters.IsReady(t.Context())
	if err != nil || ready {
		t.Fatalf("is ready before set = %t err=%v, want false", ready, err)
	}
	if err := fixture.counters.SetReady(t.Context(), rebuiltAt); err != nil {
		t.Fatalf("set ready: %v", err)
	}
	ready, err = fixture.counters.IsReady(t.Context())
	if err != nil || !ready {
		t.Fatalf("is ready after set = %t err=%v, want true", ready, err)
	}
	value, err := fixture.cache.Redis().Get(t.Context(), fixture.cache.Key("counters_ready")).Result()
	if err != nil || value != "2026-09-26T05:00:00.123Z" {
		t.Errorf("counters_ready = %q err=%v, want the rebuild time", value, err)
	}
}

func TestScriptsSurviveScriptFlush(t *testing.T) {
	// Not parallel: SCRIPT FLUSH empties the shared script cache, and a flush
	// between another test's reload and its retry would fail that test.
	fixture := newCounterFixture(t)
	if err := fixture.cache.LoadScripts(t.Context(), fixture.counters.Scripts()...); err != nil {
		t.Fatalf("load scripts: %v", err)
	}
	if err := fixture.cache.Redis().ScriptFlush(t.Context()).Err(); err != nil {
		t.Fatalf("script flush: %v", err)
	}

	settledID := fixture.reserve(t, chatFeature, 1500)
	releasedID := fixture.reserve(t, chatFeature, 100)
	if err := fixture.counters.Settle(t.Context(), settledID, fixture.beginChange(t, 1200, 0)); err != nil {
		t.Fatalf("settle after flush: %v", err)
	}
	if err := fixture.cache.Redis().ScriptFlush(t.Context()).Err(); err != nil {
		t.Fatalf("script flush: %v", err)
	}
	if result, err := fixture.counters.Release(t.Context(), releasedID, decisions.ReservationStatusReleased); err != nil || !result.Applied {
		t.Fatalf("release after flush = %+v err=%v, want applied", result, err)
	}
	if err := fixture.cache.Redis().ScriptFlush(t.Context()).Err(); err != nil {
		t.Fatalf("script flush: %v", err)
	}
	fixture.settleUnreserved(t, 300, 1)

	fixture.assertCounter(t, map[string]string{"settled": "1500", "settled:chat": "1500", "reserved": "0", "reserved:chat": "0", "count": "3", "count:chat": "3"})
}

func newCounterFixture(t *testing.T) counterFixture {
	t.Helper()
	cacheClient := cachetest.NewClient(t)
	periodStart := time.Now().UTC().Truncate(time.Hour)
	fixture := counterFixture{
		cache:       cacheClient,
		counters:    decisions.NewCounters(cacheClient),
		customerID:  uuid.New(),
		periodStart: periodStart,
		periodEnd:   periodStart.AddDate(0, 1, 0),
		expiresAt:   periodStart.Add(10*time.Minute + 500*time.Millisecond),
	}
	if err := fixture.counters.SetReady(t.Context(), periodStart); err != nil {
		t.Fatalf("set counters ready: %v", err)
	}
	return fixture
}

func (fixture counterFixture) request(feature string, amount money.Amount, ceilings decisions.Ceilings) decisions.ReserveRequest {
	return decisions.ReserveRequest{
		Environment: httpapi.EnvironmentTest,
		CustomerID:  fixture.customerID,
		PeriodStart: fixture.periodStart,
		PeriodEnd:   fixture.periodEnd,
		DecisionID:  uuid.New(),
		Feature:     feature,
		Amount:      amount,
		ExpiresAt:   fixture.expiresAt,
		Ceilings:    ceilings,
	}
}

func (fixture counterFixture) reserve(t *testing.T, feature string, amount money.Amount) uuid.UUID {
	t.Helper()
	request := fixture.request(feature, amount, decisions.Ceilings{})
	result, err := fixture.counters.Reserve(t.Context(), request)
	if err != nil || result != decisions.ReserveResultReserved {
		t.Fatalf("reserve %s %d = %q err=%v, want reserved", feature, amount, result, err)
	}
	if err := fixture.counters.EndChange(t.Context(), request); err != nil {
		t.Fatalf("end change: %v", err)
	}
	return request.DecisionID
}

func (fixture counterFixture) beginChange(t *testing.T, amount money.Amount, countIncrement int64) decisions.Settlement {
	t.Helper()
	settlement := decisions.Settlement{
		Environment:    httpapi.EnvironmentTest,
		CustomerID:     fixture.customerID,
		PeriodStart:    fixture.periodStart,
		PeriodEnd:      fixture.periodEnd,
		ChangeID:       uuid.New(),
		Feature:        chatFeature,
		Amount:         amount,
		CountIncrement: countIncrement,
	}
	if err := fixture.counters.BeginChange(t.Context(), settlement, fixture.periodStart); err != nil {
		t.Fatalf("begin change: %v", err)
	}
	return settlement
}

func (fixture counterFixture) settleUnreserved(t *testing.T, amount money.Amount, countIncrement int64) {
	t.Helper()
	if err := fixture.counters.SettleUnreserved(t.Context(), fixture.beginChange(t, amount, countIncrement)); err != nil {
		t.Fatalf("settle unreserved: %v", err)
	}
}

func (fixture counterFixture) counterKey() string {
	return fixture.counters.CounterKey(httpapi.EnvironmentTest, fixture.customerID, fixture.periodStart)
}

func (fixture counterFixture) counterFields(t *testing.T) map[string]string {
	t.Helper()
	fields, err := fixture.cache.Redis().HGetAll(t.Context(), fixture.counterKey()).Result()
	if err != nil {
		t.Fatalf("read counter: %v", err)
	}
	return fields
}

func (fixture counterFixture) assertCounter(t *testing.T, want map[string]string) {
	t.Helper()
	fixture.assertHash(t, fixture.counterKey(), want)
}

func (fixture counterFixture) assertHash(t *testing.T, key string, want map[string]string) {
	t.Helper()
	fields, err := fixture.cache.Redis().HGetAll(t.Context(), key).Result()
	if err != nil {
		t.Fatalf("read %s: %v", key, err)
	}
	if diff := cmp.Diff(want, fields); diff != "" {
		t.Errorf("%s mismatch (-want +got):\n%s", key, diff)
	}
}

func (fixture counterFixture) assertExpireAt(t *testing.T, key string, wantUnix int64) {
	t.Helper()
	expireAt, err := fixture.cache.Redis().ExpireTime(t.Context(), key).Result()
	if err != nil {
		t.Fatalf("read expiry of %s: %v", key, err)
	}
	if got := int64(expireAt / time.Second); got != wantUnix {
		t.Errorf("%s expires at %d, want %d", key, got, wantUnix)
	}
}

func (fixture counterFixture) assertStatus(t *testing.T, decisionID uuid.UUID, want decisions.ReservationStatus) {
	t.Helper()
	status, err := fixture.cache.Redis().HGet(t.Context(), fixture.counters.ReservationKey(decisionID), "status").Result()
	if err != nil || decisions.ReservationStatus(status) != want {
		t.Errorf("reservation status = %q err=%v, want %q", status, err, want)
	}
}

func (fixture counterFixture) pendingChanges(t *testing.T) []redis.Z {
	t.Helper()
	changes, err := fixture.cache.Redis().ZRangeWithScores(t.Context(), fixture.counterKey()+":pending", 0, -1).Result()
	if err != nil {
		t.Fatalf("read pending changes: %v", err)
	}
	return changes
}

func (fixture counterFixture) assertNoPendingChanges(t *testing.T) {
	t.Helper()
	if changes := fixture.pendingChanges(t); len(changes) != 0 {
		t.Errorf("pending changes = %v, want none", changes)
	}
}

func (fixture counterFixture) assertOpenReservations(t *testing.T, want int64) {
	t.Helper()
	open, err := fixture.cache.Redis().SCard(t.Context(), fixture.counterKey()+":reservations").Result()
	if err != nil || open != want {
		t.Errorf("open reservations = %d err=%v, want %d", open, err, want)
	}
}

func (fixture counterFixture) assertNotExpiring(t *testing.T, decisionID uuid.UUID) {
	t.Helper()
	err := fixture.cache.Redis().ZScore(t.Context(), fixture.cache.Key("reservations_expiring"), decisionID.String()).Err()
	if !errors.Is(err, redis.Nil) {
		t.Errorf("reservations_expiring score of %s err=%v, want no member", decisionID, err)
	}
}

func amountPointer(amount money.Amount) *money.Amount {
	return &amount
}

func countPointer(count int64) *int64 {
	return &count
}
