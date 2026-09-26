// Package logging writes structured JSON logs with log/slog. The message of
// every log line is an Event from the registry in events.go, and attributes are
// snake_case key-value pairs. A context from WithRequestID adds a request_id
// attribute. The value of any attribute whose key or enclosing group contains
// password, secret, token or api_key is written as [redacted].
package logging
