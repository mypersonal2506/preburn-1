package cachetest

import (
	"crypto/rand"
	"log/slog"
	"net/url"
	"os"
	"testing"

	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/config"
	"github.com/preburn/preburn/internal/logging"
)

const redisURLVariable = "PREBURN_TEST_REDIS_URL"

// NewClient returns a client connected to PREBURN_TEST_REDIS_URL whose keys
// start with test:<random>:. It logs every event to the test output and
// closes when the test ends. It leaves its keys behind, because the test
// Valkey runs on tmpfs and is discarded. It fails the test if the variable is
// unset or the server does not answer.
func NewClient(t testing.TB) *cache.Client {
	t.Helper()
	value := os.Getenv(redisURLVariable)
	if value == "" {
		t.Fatalf("%s is not set, run the tests with make test-go", redisURLVariable)
	}
	redisURL, err := url.Parse(value)
	if err != nil {
		t.Fatalf("parse %s: %v", redisURLVariable, err)
	}
	configuration := config.Config{RedisURL: redisURL, RedisKeyPrefix: "test:" + rand.Text() + ":"}
	client, err := cache.Open(t.Context(), configuration, logging.New(t.Output(), slog.LevelDebug))
	if err != nil {
		t.Fatalf("open cache client: %v", err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("close cache client: %v", err)
		}
	})
	return client
}
