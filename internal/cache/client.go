package cache

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/redis/go-redis/v9"

	"github.com/preburn/preburn/internal/config"
	"github.com/preburn/preburn/internal/logging"
)

const (
	keySeparator = ":"
	noRetries    = -1
)

// Client runs commands against Redis or Valkey over its own connection pool.
// Every key it builds starts with the configured prefix. It is safe for
// concurrent use.
type Client struct {
	redis         *redis.Client
	keyPrefix     string
	logger        *logging.Logger
	scriptsLoaded atomic.Bool
}

// Open connects to configuration.RedisURL with a new connection pool and
// checks the connection with PING. Keys from the client start with
// configuration.RedisKeyPrefix. The client never re-sends a command whose
// reply was lost to a timeout or a closed connection, because the server may
// have run it already. Such a command returns an error with an unknown
// outcome.
func Open(ctx context.Context, configuration config.Config, logger *logging.Logger) (*Client, error) {
	options, err := redis.ParseURL(configuration.RedisURL.String())
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}
	options.MaxRetries = noRetries
	redisClient := redis.NewClient(options)
	if err := redisClient.Ping(ctx).Err(); err != nil {
		return nil, errors.Join(fmt.Errorf("ping redis: %w", err), redisClient.Close())
	}
	return &Client{redis: redisClient, keyPrefix: configuration.RedisKeyPrefix, logger: logger}, nil
}

// OpenStreamClient opens a second client, with a connection pool of its own,
// for blocking stream reads such as XREAD BLOCK. Open dashboard streams hold
// its connections, never the ones the check path uses. It connects like Open.
func OpenStreamClient(ctx context.Context, configuration config.Config, logger *logging.Logger) (*Client, error) {
	return Open(ctx, configuration, logger)
}

// Key returns the key prefix followed by parts joined with colons. Every Redis
// key and channel name in Preburn is built with Key.
func (client *Client) Key(parts ...string) string {
	return client.keyPrefix + strings.Join(parts, keySeparator)
}

// Redis returns the go-redis client for commands this package does not wrap.
// Every key passed to it comes from Key.
func (client *Client) Redis() *redis.Client {
	return client.redis
}

// Close closes the connection pool. Commands after Close fail.
func (client *Client) Close() error {
	return client.redis.Close()
}
