# Errors

Every error response has the content type `application/problem+json`:

```json
{
  "type": "https://github.com/preburn/preburn/blob/main/docs/errors.md#validation_failed",
  "title": "Unprocessable Entity",
  "status": 422,
  "detail": "validation failed",
  "code": "validation_failed",
  "errors": [
    {"location": "body.name", "message": "expected 1 to 80 characters without control characters"}
  ]
}
```

- `code` is stable within API v1. Match on it, not on `title` or `detail`.
- `type` links to the section of this page named after the code.
- `errors` lists the invalid fields when there are any. `location` names the field, such as `body.name`, `query.limit` or `path.external_id`.
- Error responses never repeat the values the request sent.

## validation_failed

Status 422, or 400, 408 or 415 when the request body cannot be read as JSON, arrives too slowly or has another content type.

The request breaks a rule of the API. Each entry of `errors` names one invalid field and the rule it breaks. Fix those fields and send the request again.

## not_found

Status 404.

The resource does not exist in the environment of the request, or no API route has this path and method. An id from the other environment also returns it.

## authentication_required

Status 401.

The request has no credentials, or its API key or session is unknown, revoked or expired. Unknown and revoked API keys get the same answer. Send `Authorization: Bearer <key>` with an active key, or sign in again.

## scope_forbidden

Status 403.

The credentials are valid but the route's group does not admit them. Runtime keys reach only the runtime routes, such as customer upserts. Admin keys also reach configuration and reads. Members, API keys and settings need a member session from the dashboard.

## csrf_invalid

Status 403.

A member session request with an unsafe method (`POST`, `PUT`, `PATCH` or `DELETE`) has no `X-CSRF-Token` header, or the header differs from the `preburn_csrf` cookie. Send the cookie's value in the header.

## environment_header_invalid

Status 422.

A member session request has no `X-Preburn-Environment` header, or its value is not `test` or `live`. API keys belong to one environment and need no header.

## invalid_cursor

Status 422.

The `cursor` query parameter was not returned by this listing in this environment. Start again without a cursor and follow the `next_cursor` values of the responses.

## rate_limited

Status 429.

Too many attempts in the current window, such as 10 sign-in attempts in 15 minutes. The `Retry-After` header holds the seconds to wait before the next attempt.

## payload_too_large

Status 413.

The request body is larger than 1 MiB.

## client_closed_request

Status 499.

The client closed the connection before Preburn answered, for example an SDK check that reached its timeout and fell back, or a dashboard page that reloaded. No client receives this answer. It appears in the access log and the request metrics, and the server logs `http.request_canceled` at info level. Many of them on `POST /api/v1/check` mean checks take longer than the SDK waits.

## internal_error

Status 500.

Preburn failed to handle the request. The server logs the error with the request id, which the `X-Request-Id` response header carries. Send that id with a bug report.

## login_failed

Status 401.

The email or the password is wrong. The answer does not say which, and it is the same for an email without a member.

## link_expired

Status 410.

The invite or password reset link has expired, was used already, was replaced by a newer link, or belongs to a disabled member. Ask a member to create a new link, or run `preburn admin reset-password`.

## last_member

Status 409.

A member tried to remove themselves, or the last member who can sign in. Another member must do it, and at least one member who can sign in must remain.

## member_email_taken

Status 409.

Another member has this email.

## member_disabled

Status 409.

The member is disabled, so no reset link can be created for them.

## setup_not_available

Status 409.

Setup is already complete. Sign in instead, or use `preburn admin reset-password` to regain access.

## setup_token_invalid

Status 403.

The setup token does not match the latest setup link. The api process logs a new link each time it starts while setup is pending, and `preburn admin setup-link` prints one. Use the newest link.

## plan_not_found

Status 422.

The plan id does not name an active plan of the request's environment. Customer upserts and the default plan setting return it for an unknown plan, a plan that is not active, a malformed id and a plan of the other environment. Plan-level policies and policy previews return it for an unknown plan, a plan that is not active and a plan of the other environment. A policy keeps a plan that was archived after the policy named it.

## plan_name_taken

Status 409.

Another plan of the environment has this name. Plan names are unique per environment.

## plan_is_default

Status 409.

The plan is the default plan of its environment, so it cannot be archived. Choose another default plan in the settings first.

## pricing_rule_exists_use_adjustment

Status 409.

A standalone override was requested, but a catalog rule has the same provider, model, meter and conditions. Create the override without a `type`, or with `adjustment`, and it replaces that rule's price.

## pricing_override_ended

Status 409.

The override has ended, so it can no longer change. Ended overrides stay as the price history of the requests they priced. A price change to an override that has started ends it and returns a successor with a new id, so a later change to the old id also returns this code. Change the successor, or create a new override.

## policy_invalid

Status 422.

The policy document breaks a policy rule. Each entry of `errors` names one field by its JSON path, such as `body.when.all[1].value` or `body.action.overrides.duration`, and the rule it breaks. Fix those fields and send the document again. A plan-level policy whose plan is missing from the environment or archived returns `plan_not_found` instead.

## preview_too_large

Status 422.

The environment has more than 50,000 active customers, the most a policy preview evaluates. Creating the policy is not affected.

## environment_too_large

Status 422.

The environment has more than 50,000 active customers, the most the dashboard overview and customer list evaluate. The customer detail and the admin customer routes are not affected.

## counters_unavailable

Status 503.

A check could not read or reserve the customer's counter in Valkey within 150 ms, or the counters were being rebuilt from Postgres after Valkey lost them, and no decision was stored. The SDK falls back. Check that Valkey is running and reachable from the api process. After a Valkey restart without persistence, checks answer this code until the rebuild finishes and `/readyz` answers 200 again.

## database_unavailable

Status 503.

A check could not read from or write to Postgres. The SDK falls back. Check that Postgres is running and reachable from the api process.

## decision_not_reportable

Status 409.

The report names a decision that was denied, so its request never ran and it reserved nothing. Report usage only for decisions whose outcome is allow, route or cap.
