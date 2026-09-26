package cache_test

import (
	"context"
	"log/slog"
	"strconv"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/redis/go-redis/v9"

	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/cache/cachetest"
	"github.com/preburn/preburn/internal/logging"
)

const (
	invalidationChannel        = "invalidate"
	receivedInvalidationBuffer = 16
)

func TestPublishedInvalidationReachesSubscriber(t *testing.T) {
	t.Parallel()
	client := cachetest.NewClient(t)
	received, _ := subscribe(t, client)
	waitForSubscriber(t, client)

	for _, want := range []cache.Invalidation{
		{Kind: cache.InvalidationKindPolicies, Environment: "live"},
		{Kind: cache.InvalidationKindCustomer, Environment: "test", ID: "cust_01jbvagescfn78y0938nkrkayd"},
		{Kind: cache.InvalidationKindAPIKey, Environment: "live", ID: "key_01jbvagescfn78y0938nkrkayd"},
	} {
		publish(t, client, want)
		assertReceived(t, received, want)
	}
}

func TestInvalidationTravelsAsJSON(t *testing.T) {
	t.Parallel()
	client := cachetest.NewClient(t)
	subscription := client.Redis().Subscribe(t.Context(), client.Key(invalidationChannel))
	t.Cleanup(func() {
		if err := subscription.Close(); err != nil {
			t.Errorf("close subscription: %v", err)
		}
	})
	if _, err := subscription.ReceiveTimeout(t.Context(), waitTimeout); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	publish(t, client, cache.Invalidation{Kind: cache.InvalidationKindWebhookEndpoints, Environment: "live", ID: "whe_01jbvagescfn78y0938nkrkayd"})

	received, err := subscription.ReceiveTimeout(t.Context(), waitTimeout)
	if err != nil {
		t.Fatalf("receive message: %v", err)
	}
	message, isMessage := received.(*redis.Message)
	if !isMessage {
		t.Fatalf("received %T, want *redis.Message", received)
	}
	want := `{"kind":"webhook_endpoints","environment":"live","id":"whe_01jbvagescfn78y0938nkrkayd"}`
	if message.Payload != want {
		t.Errorf("payload = %s, want %s", message.Payload, want)
	}
}

func TestSubscriberSkipsMalformedInvalidations(t *testing.T) {
	t.Parallel()
	logs := &lockedBuffer{}
	client := openClient(t, uniquePrefix(), logging.New(logs, slog.LevelDebug))
	received, _ := subscribe(t, client)
	waitForSubscriber(t, client)

	for _, payload := range []string{"not json", `{"kind":"unknown","environment":"live","id":""}`} {
		if err := client.Redis().Publish(t.Context(), client.Key(invalidationChannel), payload).Err(); err != nil {
			t.Fatalf("publish %q: %v", payload, err)
		}
	}
	want := cache.Invalidation{Kind: cache.InvalidationKindPlan, Environment: "live", ID: "pln_01jbvagescfn78y0938nkrkayd"}
	publish(t, client, want)

	assertReceived(t, received, want)
	malformed := string(logging.CacheInvalidationMalformed)
	assertEvents(t, logs, []string{malformed, malformed})
}

func TestSubscriberResubscribesAfterConnectionLoss(t *testing.T) {
	t.Parallel()
	logs := &lockedBuffer{}
	prefix := uniquePrefix()
	client := openClient(t, prefix, logging.New(logs, slog.LevelDebug))
	received, resubscriptions := subscribe(t, client)
	firstSubscriber := waitForSubscriberConnection(t, client, prefix, 0)

	before := cache.Invalidation{Kind: cache.InvalidationKindPricing, Environment: "live"}
	publish(t, client, before)
	assertReceived(t, received, before)

	killed, err := client.Redis().ClientKillByFilter(t.Context(), "ID", strconv.FormatInt(firstSubscriber, 10)).Result()
	if err != nil || killed != 1 {
		t.Fatalf("kill subscriber connection %d: killed %d, error %v", firstSubscriber, killed, err)
	}
	waitForSubscriberConnection(t, client, prefix, firstSubscriber)
	select {
	case <-resubscriptions:
	case <-time.After(waitTimeout):
		t.Fatalf("resubscribed not called within %s", waitTimeout)
	}

	after := cache.Invalidation{Kind: cache.InvalidationKindSettings, Environment: "test"}
	publish(t, client, after)
	assertReceived(t, received, after)
	assertEvents(t, logs, []string{string(logging.CacheInvalidationResubscribed)})
}

func subscribe(t *testing.T, client *cache.Client) (received <-chan cache.Invalidation, resubscriptions <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	invalidations := make(chan cache.Invalidation, receivedInvalidationBuffer)
	resubscribed := make(chan struct{}, receivedInvalidationBuffer)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		client.SubscribeInvalidations(ctx, func(invalidation cache.Invalidation) {
			select {
			case invalidations <- invalidation:
			case <-ctx.Done():
			}
		}, func() {
			select {
			case resubscribed <- struct{}{}:
			case <-ctx.Done():
			}
		})
	}()
	t.Cleanup(func() {
		cancel()
		<-stopped
	})
	return invalidations, resubscribed
}

func waitForSubscriber(t *testing.T, client *cache.Client) {
	t.Helper()
	channel := client.Key(invalidationChannel)
	waitFor(t, "subscriber on "+channel, func() bool {
		counts, err := client.Redis().PubSubNumSub(t.Context(), channel).Result()
		if err != nil {
			t.Fatalf("pubsub numsub: %v", err)
		}
		return counts[channel] == 1
	})
}

func waitForSubscriberConnection(t *testing.T, client *cache.Client, name string, previousID int64) int64 {
	t.Helper()
	var subscriberID int64
	waitFor(t, "subscriber connection named "+name, func() bool {
		for _, entry := range clientList(t, client) {
			if entry.name == name && entry.subscriptions == "1" && entry.id != previousID {
				subscriberID = entry.id
				return true
			}
		}
		return false
	})
	return subscriberID
}

func publish(t *testing.T, client *cache.Client, invalidation cache.Invalidation) {
	t.Helper()
	if err := client.PublishInvalidation(t.Context(), invalidation); err != nil {
		t.Fatalf("publish invalidation: %v", err)
	}
}

func assertReceived(t *testing.T, received <-chan cache.Invalidation, want cache.Invalidation) {
	t.Helper()
	select {
	case got := <-received:
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("invalidation mismatch (-want +got):\n%s", diff)
		}
	case <-time.After(waitTimeout):
		t.Fatalf("no invalidation within %s, want %+v", waitTimeout, want)
	}
}
