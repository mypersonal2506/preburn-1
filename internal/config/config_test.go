package config_test

import (
	"bytes"
	"encoding/base64"
	"log/slog"
	"maps"
	"net/netip"
	"net/url"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/preburn/preburn/internal/config"
)

const (
	testDatabaseURL = "postgres://preburn@postgres:5432/preburn?sslmode=disable"
	testRedisURL    = "redis://valkey:6379/0"
)

func TestLoadAppliesDefaults(t *testing.T) {
	tests := []struct {
		name      string
		variables map[string]string
	}{
		{name: "only required variables set", variables: requiredVariables()},
		{name: "optional variables empty", variables: with(requiredVariables(), map[string]string{
			"PREBURN_DATABASE_MAXIMUM_CONNECTIONS":    "",
			"PREBURN_REDIS_KEY_PREFIX":                "",
			"PREBURN_PUBLIC_URL":                      "",
			"PREBURN_HTTP_ADDRESS":                    "",
			"PREBURN_METRICS_ADDRESS":                 "",
			"PREBURN_LOG_LEVEL":                       "",
			"PREBURN_TRUSTED_PROXIES":                 "",
			"PREBURN_DECISION_RETENTION_DAYS":         "",
			"PREBURN_PRICING_LITELLM_REFRESH":         "",
			"PREBURN_WEBHOOKS_ALLOW_PRIVATE_NETWORKS": "",
			"PREBURN_STRIPE_API_BASE":                 "",
		})},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := config.Load(lookupFrom(test.variables))
			if err != nil {
				t.Fatalf("load: %v", err)
			}

			want := config.Config{
				DatabaseURL:                  mustParseURL(t, testDatabaseURL),
				DatabaseMaximumConnections:   10,
				RedisURL:                     mustParseURL(t, testRedisURL),
				RedisKeyPrefix:               "preburn:",
				SecretKey:                    testSecretKey(),
				PublicURL:                    mustParseURL(t, "http://localhost:8080"),
				HTTPAddress:                  ":8080",
				MetricsAddress:               ":9090",
				LogLevel:                     slog.LevelInfo,
				TrustedProxies:               nil,
				DecisionRetentionDays:        90,
				PricingLiteLLMRefresh:        false,
				WebhooksAllowPrivateNetworks: false,
				StripeAPIBase:                mustParseURL(t, "https://api.stripe.com"),
			}
			if diff := cmp.Diff(want, got, configComparers()...); diff != "" {
				t.Errorf("config mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestLoadReadsEveryVariable(t *testing.T) {
	variables := with(requiredVariables(), map[string]string{
		"PREBURN_DATABASE_MAXIMUM_CONNECTIONS":    "25",
		"PREBURN_REDIS_KEY_PREFIX":                "staging:",
		"PREBURN_PUBLIC_URL":                      "https://preburn.example.com",
		"PREBURN_HTTP_ADDRESS":                    "0.0.0.0:8000",
		"PREBURN_METRICS_ADDRESS":                 "127.0.0.1:9100",
		"PREBURN_LOG_LEVEL":                       "debug",
		"PREBURN_TRUSTED_PROXIES":                 "10.0.0.0/8, 192.168.1.0/24,fd00::/8",
		"PREBURN_DECISION_RETENTION_DAYS":         "30",
		"PREBURN_PRICING_LITELLM_REFRESH":         "true",
		"PREBURN_WEBHOOKS_ALLOW_PRIVATE_NETWORKS": "true",
		"PREBURN_STRIPE_API_BASE":                 "http://stripe-mock:12111",
	})

	got, err := config.Load(lookupFrom(variables))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	want := config.Config{
		DatabaseURL:                mustParseURL(t, testDatabaseURL),
		DatabaseMaximumConnections: 25,
		RedisURL:                   mustParseURL(t, testRedisURL),
		RedisKeyPrefix:             "staging:",
		SecretKey:                  testSecretKey(),
		PublicURL:                  mustParseURL(t, "https://preburn.example.com"),
		HTTPAddress:                "0.0.0.0:8000",
		MetricsAddress:             "127.0.0.1:9100",
		LogLevel:                   slog.LevelDebug,
		TrustedProxies: []netip.Prefix{
			netip.MustParsePrefix("10.0.0.0/8"),
			netip.MustParsePrefix("192.168.1.0/24"),
			netip.MustParsePrefix("fd00::/8"),
		},
		DecisionRetentionDays:        30,
		PricingLiteLLMRefresh:        true,
		WebhooksAllowPrivateNetworks: true,
		StripeAPIBase:                mustParseURL(t, "http://stripe-mock:12111"),
	}
	if diff := cmp.Diff(want, got, configComparers()...); diff != "" {
		t.Errorf("config mismatch (-want +got):\n%s", diff)
	}
}

func TestLoadReportsMissingRequiredVariable(t *testing.T) {
	tests := []struct {
		name     string
		variable string
		setEmpty bool
	}{
		{name: "database URL unset", variable: "PREBURN_DATABASE_URL"},
		{name: "redis URL unset", variable: "PREBURN_REDIS_URL"},
		{name: "secret key unset", variable: "PREBURN_SECRET_KEY"},
		{name: "database URL empty", variable: "PREBURN_DATABASE_URL", setEmpty: true},
		{name: "redis URL empty", variable: "PREBURN_REDIS_URL", setEmpty: true},
		{name: "secret key empty", variable: "PREBURN_SECRET_KEY", setEmpty: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			variables := requiredVariables()
			delete(variables, test.variable)
			if test.setEmpty {
				variables[test.variable] = ""
			}

			assertProblems(t, variables, test.variable+" is required")
		})
	}
}

func TestLoadReportsInvalidValue(t *testing.T) {
	tests := []struct {
		name     string
		variable string
		value    string
		want     string
	}{
		{
			name:     "database URL without scheme",
			variable: "PREBURN_DATABASE_URL",
			value:    "localhost:5432/preburn",
			want:     "PREBURN_DATABASE_URL must be a URL with scheme postgres or postgresql and a host",
		},
		{
			name:     "database URL with other scheme",
			variable: "PREBURN_DATABASE_URL",
			value:    "mysql://preburn@mysql:3306/preburn",
			want:     "PREBURN_DATABASE_URL must be a URL with scheme postgres or postgresql and a host",
		},
		{
			name:     "database URL that does not parse",
			variable: "PREBURN_DATABASE_URL",
			value:    "postgres://preburn:secret@[::1/preburn",
			want:     "PREBURN_DATABASE_URL must be a URL with scheme postgres or postgresql and a host",
		},
		{
			name:     "redis URL without host",
			variable: "PREBURN_REDIS_URL",
			value:    "redis:///0",
			want:     "PREBURN_REDIS_URL must be a URL with scheme redis or rediss and a host",
		},
		{
			name:     "redis URL as host and port",
			variable: "PREBURN_REDIS_URL",
			value:    "valkey:6379",
			want:     "PREBURN_REDIS_URL must be a URL with scheme redis or rediss and a host",
		},
		{
			name:     "public URL without scheme",
			variable: "PREBURN_PUBLIC_URL",
			value:    "preburn.example.com",
			want:     "PREBURN_PUBLIC_URL must be a URL with scheme http or https and a host",
		},
		{
			name:     "stripe API base with other scheme",
			variable: "PREBURN_STRIPE_API_BASE",
			value:    "ftp://stripe-mock:12111",
			want:     "PREBURN_STRIPE_API_BASE must be a URL with scheme http or https and a host",
		},
		{
			name:     "secret key of 16 bytes",
			variable: "PREBURN_SECRET_KEY",
			value:    base64.StdEncoding.EncodeToString(make([]byte, 16)),
			want:     "PREBURN_SECRET_KEY must be 32 bytes of standard base64",
		},
		{
			name:     "secret key of 33 bytes",
			variable: "PREBURN_SECRET_KEY",
			value:    base64.StdEncoding.EncodeToString(make([]byte, 33)),
			want:     "PREBURN_SECRET_KEY must be 32 bytes of standard base64",
		},
		{
			name:     "secret key in URL-safe base64",
			variable: "PREBURN_SECRET_KEY",
			value:    base64.URLEncoding.EncodeToString(bytes.Repeat([]byte{0xff}, 32)),
			want:     "PREBURN_SECRET_KEY must be 32 bytes of standard base64",
		},
		{
			name:     "secret key that is not base64",
			variable: "PREBURN_SECRET_KEY",
			value:    "not base64 at all!",
			want:     "PREBURN_SECRET_KEY must be 32 bytes of standard base64",
		},
		{
			name:     "trusted proxy with prefix length out of range",
			variable: "PREBURN_TRUSTED_PROXIES",
			value:    "10.0.0.0/8,10.1.0.0/33",
			want:     `PREBURN_TRUSTED_PROXIES entry "10.1.0.0/33" must be a CIDR`,
		},
		{
			name:     "trusted proxy without prefix length",
			variable: "PREBURN_TRUSTED_PROXIES",
			value:    "10.0.0.1",
			want:     `PREBURN_TRUSTED_PROXIES entry "10.0.0.1" must be a CIDR`,
		},
		{
			name:     "trusted proxies with empty entry",
			variable: "PREBURN_TRUSTED_PROXIES",
			value:    "10.0.0.0/8,",
			want:     `PREBURN_TRUSTED_PROXIES entry "" must be a CIDR`,
		},
		{
			name:     "retention days zero",
			variable: "PREBURN_DECISION_RETENTION_DAYS",
			value:    "0",
			want:     "PREBURN_DECISION_RETENTION_DAYS must be a whole number from 1 to 106751",
		},
		{
			name:     "retention days negative",
			variable: "PREBURN_DECISION_RETENTION_DAYS",
			value:    "-7",
			want:     "PREBURN_DECISION_RETENTION_DAYS must be a whole number from 1 to 106751",
		},
		{
			name:     "retention days not a number",
			variable: "PREBURN_DECISION_RETENTION_DAYS",
			value:    "ninety",
			want:     "PREBURN_DECISION_RETENTION_DAYS must be a whole number from 1 to 106751",
		},
		{
			name:     "retention days above the duration range",
			variable: "PREBURN_DECISION_RETENTION_DAYS",
			value:    "106752",
			want:     "PREBURN_DECISION_RETENTION_DAYS must be a whole number from 1 to 106751",
		},
		{
			name:     "maximum connections above the pool size range",
			variable: "PREBURN_DATABASE_MAXIMUM_CONNECTIONS",
			value:    "2147483648",
			want:     "PREBURN_DATABASE_MAXIMUM_CONNECTIONS must be a whole number from 1 to 2147483647",
		},
		{
			name:     "maximum connections zero",
			variable: "PREBURN_DATABASE_MAXIMUM_CONNECTIONS",
			value:    "0",
			want:     "PREBURN_DATABASE_MAXIMUM_CONNECTIONS must be a whole number from 1 to 2147483647",
		},
		{
			name:     "maximum connections fractional",
			variable: "PREBURN_DATABASE_MAXIMUM_CONNECTIONS",
			value:    "2.5",
			want:     "PREBURN_DATABASE_MAXIMUM_CONNECTIONS must be a whole number from 1 to 2147483647",
		},
		{
			name:     "unknown log level",
			variable: "PREBURN_LOG_LEVEL",
			value:    "verbose",
			want:     "PREBURN_LOG_LEVEL must be one of debug, info, warn, error",
		},
		{
			name:     "log level in upper case",
			variable: "PREBURN_LOG_LEVEL",
			value:    "INFO",
			want:     "PREBURN_LOG_LEVEL must be one of debug, info, warn, error",
		},
		{
			name:     "HTTP address without port",
			variable: "PREBURN_HTTP_ADDRESS",
			value:    "8080",
			want:     "PREBURN_HTTP_ADDRESS must be a listen address in host:port form with a port from 0 to 65535",
		},
		{
			name:     "HTTP address with port out of range",
			variable: "PREBURN_HTTP_ADDRESS",
			value:    ":99999",
			want:     "PREBURN_HTTP_ADDRESS must be a listen address in host:port form with a port from 0 to 65535",
		},
		{
			name:     "metrics address with service name port",
			variable: "PREBURN_METRICS_ADDRESS",
			value:    "localhost:http",
			want:     "PREBURN_METRICS_ADDRESS must be a listen address in host:port form with a port from 0 to 65535",
		},
		{
			name:     "metrics address without port",
			variable: "PREBURN_METRICS_ADDRESS",
			value:    "localhost",
			want:     "PREBURN_METRICS_ADDRESS must be a listen address in host:port form with a port from 0 to 65535",
		},
		{
			name:     "pricing refresh not a boolean",
			variable: "PREBURN_PRICING_LITELLM_REFRESH",
			value:    "yes",
			want:     "PREBURN_PRICING_LITELLM_REFRESH must be true or false",
		},
		{
			name:     "private networks not a boolean",
			variable: "PREBURN_WEBHOOKS_ALLOW_PRIVATE_NETWORKS",
			value:    "sometimes",
			want:     "PREBURN_WEBHOOKS_ALLOW_PRIVATE_NETWORKS must be true or false",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			variables := with(requiredVariables(), map[string]string{test.variable: test.value})

			assertProblems(t, variables, test.want)
		})
	}
}

func TestLoadReportsEveryProblemAtOnce(t *testing.T) {
	variables := with(requiredVariables(), map[string]string{
		"PREBURN_SECRET_KEY": base64.StdEncoding.EncodeToString(make([]byte, 5)),
		"PREBURN_LOG_LEVEL":  "trace",
	})
	delete(variables, "PREBURN_DATABASE_URL")

	assertProblems(t, variables,
		"PREBURN_DATABASE_URL is required",
		"PREBURN_SECRET_KEY must be 32 bytes of standard base64",
		"PREBURN_LOG_LEVEL must be one of debug, info, warn, error",
	)
}

func TestSecureCookies(t *testing.T) {
	tests := []struct {
		name      string
		publicURL string
		want      bool
	}{
		{name: "http public URL", publicURL: "http://localhost:8080", want: false},
		{name: "https public URL", publicURL: "https://preburn.example.com", want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			variables := with(requiredVariables(), map[string]string{"PREBURN_PUBLIC_URL": test.publicURL})
			loaded, err := config.Load(lookupFrom(variables))
			if err != nil {
				t.Fatalf("load: %v", err)
			}

			if got := loaded.SecureCookies(); got != test.want {
				t.Errorf("SecureCookies() = %t, want %t", got, test.want)
			}
		})
	}
}

func assertProblems(t *testing.T, variables map[string]string, want ...string) {
	t.Helper()
	_, err := config.Load(lookupFrom(variables))
	if err == nil {
		t.Fatal("load: got no error")
	}
	if diff := cmp.Diff(want, strings.Split(err.Error(), "\n")); diff != "" {
		t.Errorf("problems mismatch (-want +got):\n%s", diff)
	}
}

func requiredVariables() map[string]string {
	secretKey := testSecretKey()
	return map[string]string{
		"PREBURN_DATABASE_URL": testDatabaseURL,
		"PREBURN_REDIS_URL":    testRedisURL,
		"PREBURN_SECRET_KEY":   base64.StdEncoding.EncodeToString(secretKey[:]),
	}
}

func with(variables, overrides map[string]string) map[string]string {
	maps.Copy(variables, overrides)
	return variables
}

func lookupFrom(variables map[string]string) func(name string) (string, bool) {
	return func(name string) (string, bool) {
		value, found := variables[name]
		return value, found
	}
}

func testSecretKey() [32]byte {
	var secretKey [32]byte
	for index := range secretKey {
		secretKey[index] = byte(index)
	}
	return secretKey
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse URL %q: %v", raw, err)
	}
	return parsed
}

func configComparers() []cmp.Option {
	return []cmp.Option{
		cmp.Comparer(func(left, right url.URL) bool { return left.String() == right.String() }),
		cmp.Comparer(func(left, right netip.Prefix) bool { return left == right }),
	}
}
