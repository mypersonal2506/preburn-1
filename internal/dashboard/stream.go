package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/decisions"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/logging"
	"github.com/preburn/preburn/internal/policies"
)

const (
	// StreamHeartbeatInterval is how often an open decision stream sends a
	// comment line, so proxies keep the connection open while no decision
	// arrives.
	StreamHeartbeatInterval = 15 * time.Second

	streamReadCount        = 100
	streamMinimumBlock     = time.Millisecond
	emptyStreamID          = "0-0"
	eventStreamContentType = "text/event-stream"
	decisionEventName      = "decision"
	heartbeatComment       = ": heartbeat\n\n"
)

// SessionChecker checks that the member session of a request is still live
// without extending it. members.SessionAuthenticator implements it.
type SessionChecker interface {
	// CheckSession returns httpapi.ErrAuthenticationRequired when request
	// no longer presents a live session of an active member, and never
	// records a use of the session.
	CheckSession(ctx context.Context, request *http.Request) error
}

// StreamService relays the decision stream of each environment to the
// dashboard as server-sent events. Its blocking stream reads hold
// connections of its own client, never those of the check path. Before each
// heartbeat it checks the stream's member session again without extending
// it, so a stream ends within one heartbeat interval of its session ending,
// such as by a logout or a removal of the member, and an idle open stream
// never keeps a session alive. Create one with NewStreamService. It is safe
// for concurrent use.
type StreamService struct {
	cache             *cache.Client
	sessions          SessionChecker
	logger            *logging.Logger
	heartbeatInterval time.Duration
	stopped           chan struct{}
	stopOnce          sync.Once
}

type streamEntryReader struct {
	values  map[string]any
	missing []string
}

// NewStreamService returns a StreamService that reads streams through
// streamClient, the client cache.OpenStreamClient opens, checks the session
// of each open stream with sessions, logs failed streams to logger and sends
// a heartbeat every heartbeatInterval, which is StreamHeartbeatInterval
// outside tests. It only stores its arguments, so zero values serve route
// registration for the OpenAPI document.
func NewStreamService(streamClient *cache.Client, sessions SessionChecker, logger *logging.Logger, heartbeatInterval time.Duration) *StreamService {
	return &StreamService{
		cache:             streamClient,
		sessions:          sessions,
		logger:            logger,
		heartbeatInterval: heartbeatInterval,
		stopped:           make(chan struct{}),
	}
}

// Stop ends every open stream, and every stream opened later, within one
// heartbeat interval. The api process registers it with
// http.Server.RegisterOnShutdown, because Shutdown leaves open requests
// running and would wait for the streams until its deadline.
func (service *StreamService) Stop() {
	service.stopOnce.Do(func() { close(service.stopped) })
}

func (service *StreamService) startID(ctx context.Context, environment httpapi.Environment, lastEventID string) (string, error) {
	if lastEventID != "" {
		return lastEventID, nil
	}
	newest, err := service.cache.Redis().XRevRangeN(ctx, decisions.StreamKey(service.cache, environment), "+", "-", 1).Result()
	if err != nil {
		return "", fmt.Errorf("read newest decision stream entry of environment %s: %w", environment, err)
	}
	if len(newest) == 0 {
		return emptyStreamID, nil
	}
	return newest[0].ID, nil
}

func (service *StreamService) relay(ctx context.Context, request *http.Request, environment httpapi.Environment, afterID string, writer http.ResponseWriter) {
	controller := http.NewResponseController(writer)
	writer.Header().Set("Content-Type", eventStreamContentType)
	writer.Header().Set("Cache-Control", "no-cache")
	// nginx buffers proxied responses unless told otherwise, which holds every event back.
	writer.Header().Set("X-Accel-Buffering", "no")
	writer.WriteHeader(http.StatusOK)
	if err := controller.Flush(); err != nil {
		return
	}
	key := decisions.StreamKey(service.cache, environment)
	nextHeartbeat := time.Now().Add(service.heartbeatInterval)
	for {
		select {
		case <-ctx.Done():
			return
		case <-service.stopped:
			return
		default:
		}
		streams, err := service.cache.Redis().XRead(ctx, &redis.XReadArgs{
			Streams: []string{key, afterID},
			Count:   streamReadCount,
			// BLOCK 0 waits forever, so the wait never drops below a millisecond.
			Block: max(time.Until(nextHeartbeat), streamMinimumBlock),
		}).Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			service.fail(ctx, environment, fmt.Errorf("read decision stream: %w", err))
			return
		}
		for _, stream := range streams {
			for _, message := range stream.Messages {
				event, err := newDecisionEventResponse(message.Values)
				if err != nil {
					service.fail(ctx, environment, fmt.Errorf("decode decision stream entry %s: %w", message.ID, err))
					return
				}
				data, err := json.Marshal(event)
				if err != nil {
					service.fail(ctx, environment, fmt.Errorf("encode decision event %s: %w", message.ID, err))
					return
				}
				if _, err := fmt.Fprintf(writer, "id: %s\nevent: %s\ndata: %s\n\n", message.ID, decisionEventName, data); err != nil {
					return
				}
				afterID = message.ID
			}
		}
		if !time.Now().Before(nextHeartbeat) {
			if err := service.sessions.CheckSession(ctx, request); err != nil {
				if !errors.Is(err, httpapi.ErrAuthenticationRequired) {
					service.fail(ctx, environment, fmt.Errorf("check session of open stream: %w", err))
				}
				return
			}
			if _, err := io.WriteString(writer, heartbeatComment); err != nil {
				return
			}
			nextHeartbeat = time.Now().Add(service.heartbeatInterval)
		}
		if err := controller.Flush(); err != nil {
			return
		}
	}
}

func (service *StreamService) fail(ctx context.Context, environment httpapi.Environment, err error) {
	if ctx.Err() != nil {
		return
	}
	service.logger.Warn(ctx, logging.DashboardStreamFailed,
		slog.String("environment", string(environment)),
		slog.String("error", err.Error()),
	)
}

func newDecisionEventResponse(values map[string]any) (DecisionEventResponse, error) {
	entry := streamEntryReader{values: values}
	event := DecisionEventResponse{
		ID:                  entry.text("decision_id"),
		CustomerID:          entry.text("customer_id"),
		CustomerExternalID:  entry.text("customer_external_id"),
		CustomerDisplayName: entry.optionalText("customer_display_name"),
		Feature:             entry.text("feature"),
		RequestedModel:      entry.text("requested_model"),
		Model:               entry.text("model"),
		Outcome:             policies.Outcome(entry.text("outcome")),
		Reason:              policies.Reason(entry.text("reason")),
		EstimatedCost:       entry.optionalText("estimated_cost"),
		MatchedPolicyID:     entry.optionalText("matched_policy_id"),
	}
	createdAt := entry.text("created_at")
	if len(entry.missing) > 0 {
		return DecisionEventResponse{}, fmt.Errorf("missing fields %v", entry.missing)
	}
	var err error
	if event.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return DecisionEventResponse{}, fmt.Errorf("parse created_at: %w", err)
	}
	return event, nil
}

func (entry *streamEntryReader) text(name string) string {
	text, isText := entry.values[name].(string)
	if !isText {
		entry.missing = append(entry.missing, name)
	}
	return text
}

func (entry *streamEntryReader) optionalText(name string) *string {
	text := entry.text(name)
	if text == "" {
		return nil
	}
	return &text
}
