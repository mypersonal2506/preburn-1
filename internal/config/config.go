package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	required                     = ""
	secretKeyLength              = 32
	maximumDecisionRetentionDays = int(math.MaxInt64 / int64(24*time.Hour))
)

// Config holds the validated PREBURN_ environment variables. Every field of a
// Config that Load returned without error is set.
type Config struct {
	// DatabaseURL is PREBURN_DATABASE_URL, the Postgres URL. Required.
	DatabaseURL *url.URL
	// DatabaseMaximumConnections is PREBURN_DATABASE_MAXIMUM_CONNECTIONS, the
	// pool size per process. Default 10.
	DatabaseMaximumConnections int32
	// RedisURL is PREBURN_REDIS_URL, the Redis or Valkey URL. Required.
	RedisURL *url.URL
	// RedisKeyPrefix is PREBURN_REDIS_KEY_PREFIX, the prefix of every key.
	// Default "preburn:".
	RedisKeyPrefix string
	// SecretKey is PREBURN_SECRET_KEY decoded from standard base64, the key
	// that encrypts stored secrets. Required.
	SecretKey [secretKeyLength]byte
	// PublicURL is PREBURN_PUBLIC_URL, the base for setup, link and webhook
	// URLs. Default http://localhost:8080.
	PublicURL *url.URL
	// HTTPAddress is PREBURN_HTTP_ADDRESS, the API listen address. Default ":8080".
	HTTPAddress string
	// MetricsAddress is PREBURN_METRICS_ADDRESS, the metrics listen address.
	// Default ":9090".
	MetricsAddress string
	// LogLevel is PREBURN_LOG_LEVEL, one of debug, info, warn and error.
	// Default info.
	LogLevel slog.Level
	// TrustedProxies is PREBURN_TRUSTED_PROXIES, the comma-separated CIDRs
	// whose X-Forwarded-For header is trusted. Default none.
	TrustedProxies []netip.Prefix
	// DecisionRetentionDays is PREBURN_DECISION_RETENTION_DAYS, the number of
	// days decisions are kept. Default 90.
	DecisionRetentionDays int
	// PricingLiteLLMRefresh is PREBURN_PRICING_LITELLM_REFRESH, which turns on
	// refreshing model prices from LiteLLM. Default false.
	PricingLiteLLMRefresh bool
	// WebhooksAllowPrivateNetworks is PREBURN_WEBHOOKS_ALLOW_PRIVATE_NETWORKS,
	// which lets webhook endpoints resolve to private network addresses.
	// Default false.
	WebhooksAllowPrivateNetworks bool
	// StripeAPIBase is PREBURN_STRIPE_API_BASE, the Stripe API base URL, which
	// points at stripe-mock in development. Default https://api.stripe.com.
	StripeAPIBase *url.URL
}

type environment struct {
	lookup   func(name string) (string, bool)
	problems []error
}

// Load reads every PREBURN_ variable through lookup, applies the defaults and
// validates the values. Production passes os.LookupEnv. An empty variable
// counts as unset. The error lists every problem on its own line, in the
// order of the Config fields, and never contains a variable's value except a
// rejected trusted proxy entry.
func Load(lookup func(name string) (string, bool)) (Config, error) {
	environment := environment{lookup: lookup}
	config := Config{
		DatabaseURL:                  environment.url("PREBURN_DATABASE_URL", required, "postgres", "postgresql"),
		DatabaseMaximumConnections:   environment.connectionCount("PREBURN_DATABASE_MAXIMUM_CONNECTIONS", "10"),
		RedisURL:                     environment.url("PREBURN_REDIS_URL", required, "redis", "rediss"),
		RedisKeyPrefix:               environment.value("PREBURN_REDIS_KEY_PREFIX", "preburn:"),
		SecretKey:                    environment.secretKey("PREBURN_SECRET_KEY"),
		PublicURL:                    environment.url("PREBURN_PUBLIC_URL", "http://localhost:8080", "http", "https"),
		HTTPAddress:                  environment.address("PREBURN_HTTP_ADDRESS", ":8080"),
		MetricsAddress:               environment.address("PREBURN_METRICS_ADDRESS", ":9090"),
		LogLevel:                     environment.logLevel("PREBURN_LOG_LEVEL", "info"),
		TrustedProxies:               environment.prefixes("PREBURN_TRUSTED_PROXIES"),
		DecisionRetentionDays:        environment.positiveInteger("PREBURN_DECISION_RETENTION_DAYS", "90", maximumDecisionRetentionDays),
		PricingLiteLLMRefresh:        environment.boolean("PREBURN_PRICING_LITELLM_REFRESH", "false"),
		WebhooksAllowPrivateNetworks: environment.boolean("PREBURN_WEBHOOKS_ALLOW_PRIVATE_NETWORKS", "false"),
		StripeAPIBase:                environment.url("PREBURN_STRIPE_API_BASE", "https://api.stripe.com", "http", "https"),
	}
	if err := errors.Join(environment.problems...); err != nil {
		return Config{}, err
	}
	return config, nil
}

// SecureCookies reports whether cookies need the Secure flag, which is when
// the public URL scheme is https.
func (config Config) SecureCookies() bool {
	return config.PublicURL.Scheme == "https"
}

func (environment *environment) value(name, fallback string) string {
	if value, _ := environment.lookup(name); value != "" {
		return value
	}
	if fallback == required {
		environment.report("%s is required", name)
	}
	return fallback
}

func (environment *environment) url(name, fallback string, schemes ...string) *url.URL {
	value := environment.value(name, fallback)
	if value == "" {
		return nil
	}
	parsed, err := url.Parse(value)
	if err != nil || !slices.Contains(schemes, parsed.Scheme) || parsed.Host == "" {
		environment.report("%s must be a URL with scheme %s and a host", name, strings.Join(schemes, " or "))
		return nil
	}
	return parsed
}

func (environment *environment) secretKey(name string) [secretKeyLength]byte {
	value := environment.value(name, required)
	if value == "" {
		return [secretKeyLength]byte{}
	}
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(decoded) != secretKeyLength {
		environment.report("%s must be %d bytes of standard base64", name, secretKeyLength)
		return [secretKeyLength]byte{}
	}
	return [secretKeyLength]byte(decoded)
}

func (environment *environment) positiveInteger(name, fallback string, maximum int) int {
	number, err := strconv.Atoi(environment.value(name, fallback))
	if err != nil || number < 1 || number > maximum {
		environment.report("%s must be a whole number from 1 to %d", name, maximum)
		return 0
	}
	return number
}

func (environment *environment) connectionCount(name, fallback string) int32 {
	number, err := strconv.ParseInt(environment.value(name, fallback), 10, 32)
	if err != nil || number < 1 {
		environment.report("%s must be a whole number from 1 to %d", name, math.MaxInt32)
		return 0
	}
	return int32(number)
}

func (environment *environment) address(name, fallback string) string {
	value := environment.value(name, fallback)
	_, port, err := net.SplitHostPort(value)
	if err == nil {
		_, err = strconv.ParseUint(port, 10, 16)
	}
	if err != nil {
		environment.report("%s must be a listen address in host:port form with a port from 0 to 65535", name)
		return ""
	}
	return value
}

func (environment *environment) logLevel(name, fallback string) slog.Level {
	switch environment.value(name, fallback) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	}
	environment.report("%s must be one of debug, info, warn, error", name)
	return 0
}

func (environment *environment) prefixes(name string) []netip.Prefix {
	value, _ := environment.lookup(name)
	if value == "" {
		return nil
	}
	var prefixes []netip.Prefix
	for part := range strings.SplitSeq(value, ",") {
		entry := strings.TrimSpace(part)
		prefix, err := netip.ParsePrefix(entry)
		if err != nil {
			environment.report("%s entry %q must be a CIDR", name, entry)
			continue
		}
		prefixes = append(prefixes, prefix)
	}
	return prefixes
}

func (environment *environment) boolean(name, fallback string) bool {
	enabled, err := strconv.ParseBool(environment.value(name, fallback))
	if err != nil {
		environment.report("%s must be true or false", name)
		return false
	}
	return enabled
}

func (environment *environment) report(format string, arguments ...any) {
	environment.problems = append(environment.problems, fmt.Errorf(format, arguments...))
}
