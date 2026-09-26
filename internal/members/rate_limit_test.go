package members_test

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"testing"
	"time"

	"github.com/preburn/preburn/internal/cache/cachetest"
	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/members"
)

const (
	limitName      = "login_email"
	limitMaximum   = 10
	limitWindow    = 15 * time.Minute
	limitedSubject = "sam@example.com"
)

func TestRateLimiterAllowsMaximumPerWindow(t *testing.T) {
	t.Parallel()
	cacheClient := cachetest.NewClient(t)
	manualClock := clock.NewManual(testStart)
	limiter := members.NewRateLimiter(cacheClient, manualClock)

	for attempt := 1; attempt <= limitMaximum; attempt++ {
		allowed, retryAfter, err := limiter.Allow(t.Context(), limitName, limitedSubject, limitMaximum, limitWindow)
		if err != nil || !allowed || retryAfter != 0 {
			t.Fatalf("attempt %d allowed=%t retry_after=%s err=%v, want allowed", attempt, allowed, retryAfter, err)
		}
	}
	assertDenied(t, limiter, limitedSubject, limitWindow)

	allowed, _, err := limiter.Allow(t.Context(), limitName, "jordan@example.com", limitMaximum, limitWindow)
	if err != nil || !allowed {
		t.Errorf("other subject allowed=%t err=%v, want allowed", allowed, err)
	}
	allowed, _, err = limiter.Allow(t.Context(), "login_client_ip", limitedSubject, limitMaximum, limitWindow)
	if err != nil || !allowed {
		t.Errorf("other limit allowed=%t err=%v, want allowed", allowed, err)
	}

	manualClock.Advance(5 * time.Minute)
	assertDenied(t, limiter, limitedSubject, 10*time.Minute)

	manualClock.Advance(10 * time.Minute)
	allowed, _, err = limiter.Allow(t.Context(), limitName, limitedSubject, limitMaximum, limitWindow)
	if err != nil || !allowed {
		t.Errorf("next window allowed=%t err=%v, want allowed", allowed, err)
	}
}

func TestRateLimiterKeyExpiresWithWindow(t *testing.T) {
	t.Parallel()
	cacheClient := cachetest.NewClient(t)
	manualClock := clock.NewManual(testStart.Add(5*time.Minute + 500*time.Millisecond))
	limiter := members.NewRateLimiter(cacheClient, manualClock)

	if _, _, err := limiter.Allow(t.Context(), limitName, limitedSubject, limitMaximum, limitWindow); err != nil {
		t.Fatalf("allow: %v", err)
	}

	subjectHash := sha256.Sum256([]byte(limitedSubject))
	key := cacheClient.Key("rate", limitName, hex.EncodeToString(subjectHash[:]), strconv.FormatInt(testStart.Unix(), 10))
	count, err := cacheClient.Redis().Get(t.Context(), key).Int()
	if err != nil || count != 1 {
		t.Fatalf("counter %s = %d err=%v, want 1", key, count, err)
	}
	timeToLive, err := cacheClient.Redis().TTL(t.Context(), key).Result()
	if err != nil {
		t.Fatalf("read time to live: %v", err)
	}
	if want := 10 * time.Minute; timeToLive != want {
		t.Errorf("time to live = %s, want %s", timeToLive, want)
	}
}

func TestRateLimiterRoundsRetryAfterUp(t *testing.T) {
	t.Parallel()
	cacheClient := cachetest.NewClient(t)
	manualClock := clock.NewManual(testStart.Add(500 * time.Millisecond))
	limiter := members.NewRateLimiter(cacheClient, manualClock)

	for range limitMaximum {
		if _, _, err := limiter.Allow(t.Context(), limitName, limitedSubject, limitMaximum, limitWindow); err != nil {
			t.Fatalf("allow: %v", err)
		}
	}

	assertDenied(t, limiter, limitedSubject, limitWindow)
}

func TestRateLimiterRejectsInvalidLimits(t *testing.T) {
	t.Parallel()
	limiter := members.NewRateLimiter(cachetest.NewClient(t), clock.NewManual(testStart))
	tests := []struct {
		name    string
		maximum int
		window  time.Duration
	}{
		{name: "zero maximum", maximum: 0, window: limitWindow},
		{name: "zero window", maximum: limitMaximum, window: 0},
		{name: "fractional window", maximum: limitMaximum, window: 1500 * time.Millisecond},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := limiter.Allow(t.Context(), limitName, limitedSubject, test.maximum, test.window); err == nil {
				t.Error("Allow returned no error")
			}
		})
	}
}

func assertDenied(t *testing.T, limiter *members.RateLimiter, subject string, wantRetryAfter time.Duration) {
	t.Helper()
	allowed, retryAfter, err := limiter.Allow(t.Context(), limitName, subject, limitMaximum, limitWindow)
	if err != nil || allowed || retryAfter != wantRetryAfter {
		t.Errorf("allowed=%t retry_after=%s err=%v, want denied with retry_after=%s", allowed, retryAfter, err, wantRetryAfter)
	}
}
