package members

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/clock"
)

const rateLimitKeyPrefix = "rate"

// RateLimiter counts attempts in fixed time windows in Redis, one counter per
// limit, subject and window. Create one with NewRateLimiter. It is safe for
// concurrent use.
type RateLimiter struct {
	cache *cache.Client
	clock clock.Clock
}

// NewRateLimiter returns a RateLimiter that keeps its counters in cacheClient
// and reads time from timeSource.
func NewRateLimiter(cacheClient *cache.Client, timeSource clock.Clock) *RateLimiter {
	return &RateLimiter{cache: cacheClient, clock: timeSource}
}

// Allow counts one attempt of subject, such as an email address, against the
// limit named limitName, which admits maximum attempts per window. Windows
// start at whole multiples of window since the Unix epoch. allowed reports
// whether the attempt is within the limit, and when it is not, retryAfter is
// the time until the window ends, rounded up to whole seconds. The counter
// is the key rate:{limitName}:{hex SHA-256 of subject}:{window start Unix
// seconds}, which expires when the window ends. A maximum below 1 or a
// window that is not a positive whole number of seconds returns an error.
func (limiter *RateLimiter) Allow(ctx context.Context, limitName string, subject string, maximum int, window time.Duration) (allowed bool, retryAfter time.Duration, err error) {
	if maximum < 1 || window < time.Second || window%time.Second != 0 {
		return false, 0, fmt.Errorf("invalid rate limit %s maximum=%d window=%s", limitName, maximum, window)
	}
	now := limiter.clock.Now()
	windowSeconds := int64(window / time.Second)
	windowStart := now.Unix() - now.Unix()%windowSeconds
	untilWindowEnd := time.Unix(windowStart+windowSeconds, 0).Sub(now)
	untilWindowEnd = time.Duration(math.Ceil(untilWindowEnd.Seconds())) * time.Second
	subjectHash := sha256.Sum256([]byte(subject))
	key := limiter.cache.Key(rateLimitKeyPrefix, limitName, hex.EncodeToString(subjectHash[:]), strconv.FormatInt(windowStart, 10))
	pipeline := limiter.cache.Redis().TxPipeline()
	count := pipeline.Incr(ctx, key)
	pipeline.Expire(ctx, key, untilWindowEnd)
	if _, err := pipeline.Exec(ctx); err != nil {
		return false, 0, fmt.Errorf("count attempt of rate limit %s: %w", limitName, err)
	}
	if count.Val() > int64(maximum) {
		return false, untilWindowEnd, nil
	}
	return true, 0, nil
}
