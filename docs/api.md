# API

The Preburn API serves JSON under `/api/v1` on the api process, the same address as the dashboard. The running server publishes its OpenAPI 3.1 document at `/api/v1/openapi.json` and a reference page at `/api/docs`. Each release attaches the same document to its GitHub release, and `api/openapi.json` in this repository matches the code of each commit.

Within `/api/v1` changes are additive: new endpoints, new optional request fields and new response fields. Breaking changes wait for `/api/v2`. Clients should ignore response fields and enum values they do not know.

## Authentication

### API keys

Services authenticate with an API key in the `Authorization` header:

```
Authorization: Bearer pb_test_runtime_...
```

A key has the form `pb_<environment>_<scope>_<32 characters>` and acts in its environment only. Create keys on the dashboard's API keys page or with `preburn admin api-key create`. The secret is shown once. Preburn stores only its SHA-256 hash and last four characters, and revoking a key rejects it from the next request on. `last_used_at` updates at most once a minute.

| Scope | Reaches |
|---|---|
| `runtime` | The runtime routes: check, report, release, customer upserts and revenue writes. Give this scope to your application. |
| `admin` | The runtime routes, plus configuration and reads: plans, policies, pricing, customers and revenue lists. Use it for scripts and CI. |

### Member sessions

The dashboard signs in with email and password and holds a session in the `preburn_session` cookie. Session requests also send:

- `X-Preburn-Environment: test` or `live`, on every request.
- `X-CSRF-Token` with the value of the `preburn_csrf` cookie, on `POST`, `PUT`, `PATCH` and `DELETE`.

Sessions last 30 days after their last use. The decision stream, `GET /api/v1/dashboard/decisions/stream`, takes the environment from the `environment` query parameter instead of the header, because browsers cannot add headers to an event stream.

### Route groups

| Group | Runtime key | Admin key | Session |
|---|---|---|---|
| Runtime | yes | yes | no |
| Admin | no | yes | yes |
| Dashboard | no | no | yes |
| Public | no credentials needed | | |

Missing or invalid credentials answer 401 `authentication_required`, the same for unknown and revoked keys. Valid credentials on a route outside their group answer 403 `scope_forbidden`. Only an `Authorization` header with the `Bearer` scheme is read as an API key. A request with any other scheme, such as the Basic credentials of a reverse proxy, is treated as a session request.

## Requests

- Bodies are JSON, at most 1 MiB. A larger body answers 413 `payload_too_large`.
- Field names are snake_case. Unknown fields answer 422 `validation_failed`.
- Amounts, quantities and ratios are decimal strings, never JSON numbers. See [Money, quantities and time](concepts.md#money-quantities-and-time).
- Timestamps are RFC 3339 and answered in UTC.
- Text that Postgres cannot store, such as a NUL character, answers 422 at its field.
- Send `X-Request-Id` to name a request in the logs, 1 to 128 letters, digits or `.`, `_`, `:`, `-`. Every response carries `X-Request-Id`, generated when the request had none. Quote it in bug reports.

## Errors

Every error is an `application/problem+json` document with a stable `code`, the HTTP `status`, a `detail` and, for invalid fields, an `errors` list of `{location, message}`. Details never repeat the values the request sent. [errors.md](errors.md) lists every code.

```json
{
  "type": "https://github.com/preburn/preburn/blob/main/docs/errors.md#validation_failed",
  "title": "Unprocessable Entity",
  "status": 422,
  "detail": "validation failed",
  "code": "validation_failed",
  "errors": [{"location": "query.limit", "message": "expected limit from 1 to 100"}]
}
```

## Lists

List endpoints take `limit` (default 50, at most 100) and `cursor`, and answer:

```json
{"items": [...], "next_cursor": "eyJ..."}
```

Pass `next_cursor` as `cursor` to read the next page. It is null on the last page. A cursor works only for the listing and the environment that returned it, and any other answers 422 `invalid_cursor`.

## Idempotency

| Call | Repeating it |
|---|---|
| `POST /api/v1/report` with `decision_id` | Stores one ledger entry per decision. A repeat answers 202 with the first entry and `duplicate: true`. |
| `POST /api/v1/report` with `decision_source: "fallback"` | Stores one ledger entry per `idempotency_key`, a UUID the client generates. |
| `POST /api/v1/reports` | Each report behaves as a single report. |
| `POST /api/v1/release` | Releases once. A repeat answers 200 and changes nothing. |
| `POST /api/v1/revenue` | Stores one entry per kind and `source_reference`. The first call answers 201, a repeat 200 with the stored entry and `duplicate: true`. |
| `PUT /api/v1/customers/{external_id}` | Replaces the whole customer, so a repeat stores the same state. Fields left out are cleared. |
| `DELETE /api/v1/pricing/overrides/{pricing_override_id}` | Ends the override once. A repeat answers 204. |

Retry any of these after a timeout or a 5xx answer. `POST /api/v1/check` is not idempotent: every call makes a new decision and reservation, so release a decision whose call never ran.

## Rate limits

Sign-in allows 10 attempts per 15 minutes for each email and for each client address, and password changes 10 attempts per 15 minutes for each member. Past the limit the answer is 429 `rate_limited` with `Retry-After` in seconds. Behind a reverse proxy, set `PREBURN_TRUSTED_PROXIES` so the client address is the visitor's, not the proxy's. Other routes have no rate limit.

## Check, report and release

The decision lifecycle is described in [Concepts](concepts.md#decision-lifecycle).

```sh
curl -sS "$PREBURN_BASE_URL/api/v1/check" \
  -H "Authorization: Bearer $PREBURN_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "customer_id": "customer_42",
    "feature": "text_to_video",
    "provider": "fal_ai",
    "model": "fal-ai/veo3.1/fast",
    "attributes": {"resolution": "1080p", "audio": true},
    "usage_estimate": {"output_seconds": "8"}
  }'
```

- A check takes at most 32 attributes. `usage_ceiling` has the format of `usage_estimate` and is the most the call can use. Hard policies reserve its cost.
- `POST /api/v1/report` takes `decision_source: "server"` with the `decision_id`, or `decision_source: "fallback"` with `idempotency_key`, `customer_id`, `feature`, `provider` and `model`, plus `usage`, optional `attributes` and optional `occurred_at`. It answers 202.
- `POST /api/v1/reports` takes `{"reports": [...]}` with at most 500 reports and answers 202 with one result per report, at the report's index. Each result has the `status` the report alone would get, and the ledger entry or the error. More than 500 reports answer 422 and store nothing.
- A `Preburn-Dropped-Reports: <n>` header on a report request adds `n` to the environment's dropped report count of the day, which the dashboard overview shows. The SDK sends it after dropping buffered reports.
- `POST /api/v1/release` takes `{"decision_id": "dec_..."}`.

A check answers 503 `counters_unavailable` or `database_unavailable` when Valkey or Postgres fails, and the SDK falls back. Reports that fail with a 5xx can be sent again.

## Endpoints

| Method and path | Group | Purpose |
|---|---|---|
| `GET /healthz` | public | The process is up. |
| `GET /readyz` | public | Postgres, Valkey, the scripts and the counters are ready. |
| `GET /api/v1/openapi.json`, `GET /api/docs` | public | The OpenAPI document and its reference page. |
| `GET /api/v1/setup/status` | public | Whether setup is pending. |
| `POST /api/v1/setup` | public | Create the first member with the setup token. |
| `POST /api/v1/auth/login` | public | Start a session. |
| `POST /api/v1/auth/logout` | dashboard | End the session. |
| `GET, PATCH /api/v1/auth/me` | dashboard | The current member, change name or password. |
| `POST /api/v1/auth/links/inspect`, `POST /api/v1/auth/links/consume` | public | Invite and password reset links. |
| `GET, POST /api/v1/members`, `DELETE /api/v1/members/{member_id}`, `POST /api/v1/members/{member_id}/reset-link` | dashboard | Members. |
| `GET, POST /api/v1/api-keys`, `DELETE /api/v1/api-keys/{api_key_id}` | dashboard | API keys. Delete revokes. |
| `GET, PATCH /api/v1/settings` | dashboard | The installation name, and the environment's default plan and Stripe customer metadata key. |
| `POST /api/v1/check` | runtime | Decide and reserve. |
| `POST /api/v1/report`, `POST /api/v1/reports` | runtime | Record usage. |
| `POST /api/v1/release` | runtime | Release a reservation. |
| `PUT /api/v1/customers/{external_id}` | runtime | Create or replace a customer. |
| `POST /api/v1/revenue` | runtime | Record revenue. |
| `GET /api/v1/revenue` | admin | List revenue entries. |
| `GET /api/v1/customers`, `GET /api/v1/customers/{external_id}` | admin | Customers with the signals of their current period. |
| `GET, POST /api/v1/plans`, `GET, PATCH /api/v1/plans/{plan_id}` | admin | Plans. |
| `GET, POST /api/v1/policies`, `GET, PATCH /api/v1/policies/{policy_id}` | admin | Policies. See [policies.md](policies.md). |
| `POST /api/v1/policies/preview` | admin | Count the customers a draft policy matches. |
| `GET /api/v1/policies/parameter-mappings` | admin | The parameters overrides can set, per model. |
| `GET /api/v1/pricing/meters`, `GET /api/v1/pricing/models`, `GET /api/v1/pricing/model-attributes` | admin | Browse the catalog. |
| `GET, POST /api/v1/pricing/overrides`, `GET, PATCH, DELETE /api/v1/pricing/overrides/{pricing_override_id}` | admin | Pricing overrides. Delete ends one. |
| `POST /api/v1/pricing/quote` | admin | Price a request without storing it. |
| `GET /api/v1/pricing/uncosted` | admin | Uncosted usage by provider, model and meter. |
| `GET /api/v1/dashboard/...` | dashboard | The dashboard's reads: overview, customers, decisions, the decision stream, events, onboarding and features. |

The OpenAPI document describes every field of every request and response.
