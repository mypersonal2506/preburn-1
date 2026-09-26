package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/redis/go-redis/v9"

	"github.com/preburn/preburn/internal/logging"
)

// InvalidationKind names the kind of cached data an Invalidation marks stale.
type InvalidationKind string

const (
	// InvalidationKindPolicies marks cached policies stale.
	InvalidationKindPolicies InvalidationKind = "policies"
	// InvalidationKindCustomer marks a cached customer stale.
	InvalidationKindCustomer InvalidationKind = "customer"
	// InvalidationKindPlan marks a cached plan stale.
	InvalidationKindPlan InvalidationKind = "plan"
	// InvalidationKindPricing marks cached pricing stale.
	InvalidationKindPricing InvalidationKind = "pricing"
	// InvalidationKindSettings marks cached settings stale.
	InvalidationKindSettings InvalidationKind = "settings"
	// InvalidationKindAPIKey marks a cached API key stale.
	InvalidationKindAPIKey InvalidationKind = "api_key"
	// InvalidationKindWebhookEndpoints marks cached webhook endpoints stale.
	InvalidationKindWebhookEndpoints InvalidationKind = "webhook_endpoints"
)

const invalidationChannel = "invalidate"

// Invalidation tells every process that cached data of one kind in one
// environment is stale. It travels as JSON on the invalidate channel.
type Invalidation struct {
	// Kind is the kind of data that changed.
	Kind InvalidationKind `json:"kind"`
	// Environment is the environment the data belongs to.
	Environment string `json:"environment"`
	// ID identifies the changed entity, or is empty when the whole kind changed.
	ID string `json:"id"`
}

// PublishInvalidation announces invalidation to every process subscribed with
// SubscribeInvalidations, including this one.
func (client *Client) PublishInvalidation(ctx context.Context, invalidation Invalidation) error {
	payload, err := json.Marshal(invalidation)
	if err != nil {
		return fmt.Errorf("encode invalidation: %w", err)
	}
	if err := client.redis.Publish(ctx, client.Key(invalidationChannel), payload).Err(); err != nil {
		return fmt.Errorf("publish invalidation: %w", err)
	}
	return nil
}

// SubscribeInvalidations calls handler with each invalidation published on
// the invalidate channel, one at a time on the calling goroutine, until ctx
// ends. After a lost connection it resubscribes, logs
// cache.invalidation_resubscribed and calls resubscribed. Invalidations
// published while it was disconnected are lost, so resubscribed clears every
// local cache the handler maintains. It logs
// messages that are not valid invalidations as cache.invalidation_malformed
// and skips them.
func (client *Client) SubscribeInvalidations(ctx context.Context, handler func(Invalidation), resubscribed func()) {
	channel := client.Key(invalidationChannel)
	subscription := client.redis.Subscribe(ctx, channel)
	defer func() { _ = subscription.Close() }()
	messages := subscription.ChannelWithSubscriptions()
	subscribed := false
	for {
		select {
		case <-ctx.Done():
			return
		case received, open := <-messages:
			if !open {
				return
			}
			switch message := received.(type) {
			case *redis.Subscription:
				if subscribed {
					client.logger.Warn(ctx, logging.CacheInvalidationResubscribed, slog.String("channel", channel))
					resubscribed()
				}
				subscribed = true
			case *redis.Message:
				client.handleInvalidation(ctx, message.Payload, handler)
			}
		}
	}
}

func (client *Client) handleInvalidation(ctx context.Context, payload string, handler func(Invalidation)) {
	invalidation, err := decodeInvalidation(payload)
	if err != nil {
		client.logger.Warn(ctx, logging.CacheInvalidationMalformed, slog.String("error", err.Error()))
		return
	}
	handler(invalidation)
}

func decodeInvalidation(payload string) (Invalidation, error) {
	var invalidation Invalidation
	if err := json.Unmarshal([]byte(payload), &invalidation); err != nil {
		return Invalidation{}, fmt.Errorf("decode invalidation: %w", err)
	}
	if !invalidation.Kind.known() {
		return Invalidation{}, fmt.Errorf("unknown invalidation kind %q", invalidation.Kind)
	}
	return invalidation, nil
}

func (kind InvalidationKind) known() bool {
	switch kind {
	case InvalidationKindPolicies, InvalidationKindCustomer, InvalidationKindPlan, InvalidationKindPricing,
		InvalidationKindSettings, InvalidationKindAPIKey, InvalidationKindWebhookEndpoints:
		return true
	}
	return false
}
