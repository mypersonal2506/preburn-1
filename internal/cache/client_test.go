package cache_test

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/json"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/config"
	"github.com/preburn/preburn/internal/logging"
)

const (
	testRedisURLVariable = "PREBURN_TEST_REDIS_URL"
	waitTimeout          = 10 * time.Second
	pollInterval         = 20 * time.Millisecond
)

type lockedBuffer struct {
	mutex    sync.Mutex
	contents bytes.Buffer
}

type clientListEntry struct {
	id            int64
	name          string
	subscriptions string
}

func TestKeyJoinsPartsAfterPrefix(t *testing.T) {
	t.Parallel()
	client := openClient(t, "preburn:", logging.New(t.Output(), slog.LevelDebug))

	tests := []struct {
		name  string
		parts []string
		want  string
	}{
		{name: "no parts", parts: nil, want: "preburn:"},
		{name: "one part", parts: []string{"counters_ready"}, want: "preburn:counters_ready"},
		{
			name:  "several parts",
			parts: []string{"counter", "live", "cust_01jbvagescfn78y0938nkrkayd", "1790380800"},
			want:  "preburn:counter:live:cust_01jbvagescfn78y0938nkrkayd:1790380800",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := client.Key(test.parts...); got != test.want {
				t.Errorf("Key(%q) = %q, want %q", test.parts, got, test.want)
			}
		})
	}
}

func TestStreamClientUsesItsOwnPool(t *testing.T) {
	t.Parallel()
	client := openClient(t, uniquePrefix(), logging.New(t.Output(), slog.LevelDebug))
	configuration := config.Config{RedisURL: testRedisURL(t), RedisKeyPrefix: client.Key()}
	stream, err := cache.OpenStreamClient(t.Context(), configuration, logging.New(t.Output(), slog.LevelDebug))
	if err != nil {
		t.Fatalf("open stream client: %v", err)
	}
	t.Cleanup(func() {
		if err := stream.Close(); err != nil {
			t.Errorf("close stream client: %v", err)
		}
	})

	if stream.Redis() == client.Redis() {
		t.Fatal("stream client shares the go-redis client of the main client")
	}
	if got, want := stream.Key("decisions", "live"), client.Key("decisions", "live"); got != want {
		t.Errorf("stream Key = %q, want %q", got, want)
	}
	if err := stream.Redis().Ping(t.Context()).Err(); err != nil {
		t.Errorf("ping through stream client: %v", err)
	}
}

func TestOpenFailsWhenServerIsUnreachable(t *testing.T) {
	t.Parallel()
	unreachable := &url.URL{Scheme: "redis", Host: "127.0.0.1:1"}

	_, err := cache.Open(t.Context(), config.Config{RedisURL: unreachable, RedisKeyPrefix: uniquePrefix()}, logging.New(t.Output(), slog.LevelDebug))

	if err == nil || !strings.Contains(err.Error(), "ping redis") {
		t.Errorf("Open error = %v, want a ping redis error", err)
	}
}

func (buffer *lockedBuffer) Write(data []byte) (int, error) {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	return buffer.contents.Write(data)
}

func (buffer *lockedBuffer) events(t *testing.T) []string {
	t.Helper()
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	var events []string
	scanner := bufio.NewScanner(bytes.NewReader(buffer.contents.Bytes()))
	for scanner.Scan() {
		var line struct {
			Message string `json:"msg"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			t.Fatalf("decode log line %q: %v", scanner.Text(), err)
		}
		events = append(events, line.Message)
	}
	return events
}

func openClient(t *testing.T, keyPrefix string, logger *logging.Logger) *cache.Client {
	t.Helper()
	redisURL := testRedisURL(t)
	query := redisURL.Query()
	query.Set("client_name", keyPrefix)
	redisURL.RawQuery = query.Encode()
	client, err := cache.Open(t.Context(), config.Config{RedisURL: redisURL, RedisKeyPrefix: keyPrefix}, logger)
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

func testRedisURL(t *testing.T) *url.URL {
	t.Helper()
	value := os.Getenv(testRedisURLVariable)
	if value == "" {
		t.Fatalf("%s is not set, run the tests with make test-go", testRedisURLVariable)
	}
	redisURL, err := url.Parse(value)
	if err != nil {
		t.Fatalf("parse %s: %v", testRedisURLVariable, err)
	}
	return redisURL
}

func uniquePrefix() string {
	return "test:" + rand.Text() + ":"
}

func clientList(t *testing.T, client *cache.Client) []clientListEntry {
	t.Helper()
	listing, err := client.Redis().ClientList(t.Context()).Result()
	if err != nil {
		t.Fatalf("client list: %v", err)
	}
	var entries []clientListEntry
	for line := range strings.Lines(listing) {
		fields := map[string]string{}
		for field := range strings.FieldsSeq(line) {
			name, value, _ := strings.Cut(field, "=")
			fields[name] = value
		}
		id, err := strconv.ParseInt(fields["id"], 10, 64)
		if err != nil {
			t.Fatalf("parse client id in %q: %v", line, err)
		}
		entries = append(entries, clientListEntry{id: id, name: fields["name"], subscriptions: fields["sub"]})
	}
	return entries
}

func waitFor(t *testing.T, description string, condition func() bool) {
	t.Helper()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	deadline := time.After(waitTimeout)
	for !condition() {
		select {
		case <-ticker.C:
		case <-deadline:
			t.Fatalf("%s: not reached within %s", description, waitTimeout)
		}
	}
}

func assertEvents(t *testing.T, logs *lockedBuffer, want []string) {
	t.Helper()
	if diff := cmp.Diff(want, logs.events(t)); diff != "" {
		t.Errorf("logged events mismatch (-want +got):\n%s", diff)
	}
}
