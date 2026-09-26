# Configuration

Preburn reads its configuration from environment variables when a process starts. An empty variable counts as unset and takes its default. Invalid configuration stops the process with exit code 2 and one line per problem.

## Preburn variables

| Variable | Default | Meaning |
|---|---|---|
| `PREBURN_DATABASE_URL` | required | Postgres URL with the scheme `postgres` or `postgresql`, such as `postgres://preburn:secret@postgres:5432/preburn?sslmode=disable`. Connections always use the UTC time zone. |
| `PREBURN_DATABASE_MAXIMUM_CONNECTIONS` | `10` | Postgres connections each process keeps in its pool. |
| `PREBURN_REDIS_URL` | required | Valkey or Redis URL with the scheme `redis` or `rediss`, such as `redis://valkey:6379`. |
| `PREBURN_REDIS_KEY_PREFIX` | `preburn:` | Prefix of every key, so one Valkey can serve several installations. |
| `PREBURN_SECRET_KEY` | required | 32 random bytes in standard base64. `preburn secret-key` prints a new one. See [Secret key](self-hosting.md#secret-key). |
| `PREBURN_PUBLIC_URL` | `http://localhost:8080` | Address people use to reach the dashboard. Setup, invite and reset links start with it, and an `https` URL marks the cookies `Secure`. |
| `PREBURN_HTTP_ADDRESS` | `:8080` | Listen address of the api process, in host:port form. |
| `PREBURN_METRICS_ADDRESS` | `:9090` | Listen address of the Prometheus metrics of each process, in host:port form. |
| `PREBURN_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error`. |
| `PREBURN_TRUSTED_PROXIES` | empty | Comma-separated CIDRs of reverse proxies whose `X-Forwarded-For` header is trusted, such as `172.18.0.1/32`. See [TLS and a reverse proxy](self-hosting.md#tls-and-a-reverse-proxy). |
| `PREBURN_DECISION_RETENTION_DAYS` | `90` | Days decisions are kept. The worker deletes older ones every day at 03:00 UTC. Ledger and revenue entries are kept forever. |
| `PREBURN_PRICING_LITELLM_REFRESH` | `false` | `true` downloads LiteLLM's current model price list every day at 05:00 UTC. With `false` Preburn makes no outbound calls, and prices change when you upgrade. |
| `PREBURN_WEBHOOKS_ALLOW_PRIVATE_NETWORKS` | `false` | Reserved for webhooks, which arrive in a later release. |
| `PREBURN_STRIPE_API_BASE` | `https://api.stripe.com` | Reserved for the Stripe connector, which arrives in a later release. The development stack points it at stripe-mock. |

Booleans take `true` or `false`, and whole numbers must be at least 1.

### Decision retention and counts

Keep `PREBURN_DECISION_RETENTION_DAYS` at least as long as your longest customer period, 31 days for monthly plans. `period_decision_count` and the count limits of cap policies come from the stored decisions whenever the worker repairs the counters or rebuilds them after Valkey lost its data. A retention shorter than a period lowers those counts, and a count limit then admits more requests than it should. Costs are not affected, because they come from the ledger.

## Tracing variables

Tracing is off until `OTEL_EXPORTER_OTLP_ENDPOINT` is set. Then every process exports OpenTelemetry traces over OTLP with the service name `preburn`, and the standard `OTEL_*` variables configure the exporter, the sampler and the resource.

| Variable | Meaning |
|---|---|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Collector address, such as `http://otel-collector:4318`. |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | `http/protobuf` (default), `http/json` or `grpc`. `OTEL_EXPORTER_OTLP_TRACES_PROTOCOL` takes precedence. |
| `OTEL_EXPORTER_OTLP_HEADERS` | Headers for the collector, such as an authorization token. |

Incoming requests with a `traceparent` header join the caller's trace.

## Compose variables

`compose.yaml` reads these from `.env` in addition to the Preburn variables:

| Variable | Default | Meaning |
|---|---|---|
| `PREBURN_VERSION` | `latest` | Image tag of `ghcr.io/preburn/preburn`. Pin a release, such as `0.1.0`, to upgrade on your own schedule. |
| `PREBURN_PORT` | `8080` | Port on `127.0.0.1` that serves the dashboard and the API. |
| `POSTGRES_PASSWORD` | `preburn` | Password of the bundled Postgres. It takes effect only when the Postgres volume is created, so set it before the first start. Use letters and digits, because it goes into the database URL unescaped. |

Compose sets `PREBURN_DATABASE_URL` and `PREBURN_REDIS_URL` to the bundled Postgres and Valkey. It passes the other variables of `.env.example`, plus `OTEL_EXPORTER_OTLP_PROTOCOL` and `OTEL_EXPORTER_OTLP_HEADERS`, to the Preburn containers, and no others. `PREBURN_HTTP_ADDRESS`, `PREBURN_METRICS_ADDRESS` and `PREBURN_STRIPE_API_BASE` keep their defaults under Compose, and `PREBURN_PORT` changes the published port.

To use your own Postgres or Valkey, set `PREBURN_DATABASE_URL` and `PREBURN_REDIS_URL` in the `environment` of the `migrate`, `api` and `worker` services, for example through a `compose.override.yaml`.
