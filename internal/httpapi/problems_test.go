package httpapi_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/go-cmp/cmp"

	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/logging"
)

const rejectedSecret = "sk_secret_value_that_must_not_echo"

type createThingInput struct {
	Body createThingBody
}

type createThingBody struct {
	Name   string `json:"name" maxLength:"8"`
	APIKey string `json:"api_key" pattern:"^pb_"`
}

type thingOutput struct {
	Body thingBody
}

type thingBody struct {
	Name string `json:"name"`
}

type thingPathInput struct {
	ThingID string `path:"thing_id"`
}

type emptyInput struct{}

type thingNotFoundError struct{}

type cancelingAuthenticator struct {
	cancel context.CancelFunc
}

func (thingNotFoundError) Error() string {
	return "thing not found"
}

func (thingNotFoundError) ProblemStatus() int {
	return http.StatusNotFound
}

func (thingNotFoundError) ProblemCode() string {
	return "not_found"
}

func (authenticator cancelingAuthenticator) Authenticate(ctx context.Context, _ *http.Request, _ httpapi.RouteGroup) (httpapi.Principal, error) {
	authenticator.cancel()
	return nil, fmt.Errorf("select session: %w", ctx.Err())
}

func TestValidationFailureReturnsProblemWithoutValues(t *testing.T) {
	server := newTestServer(t, httpapi.RejectingAuthenticator{})
	httpapi.Register(server.api, httpapi.RouteGroupPublic, huma.Operation{
		OperationID: "create-thing",
		Method:      http.MethodPost,
		Path:        "/api/v1/things",
	}, func(context.Context, *createThingInput) (*thingOutput, error) {
		t.Error("handler ran for an invalid body")
		return &thingOutput{}, nil
	})

	recorder := server.serve(newRequest(t, http.MethodPost, "/api/v1/things", `{"name":"`+rejectedSecret+`","api_key":"`+rejectedSecret+`"}`))

	problem := assertProblem(t, recorder, http.StatusUnprocessableEntity, "validation_failed")
	var locations []string
	for _, fieldError := range problem["errors"].([]any) {
		entry := fieldError.(map[string]any)
		locations = append(locations, entry["location"].(string))
		if message, isString := entry["message"].(string); !isString || message == "" {
			t.Errorf("error %v has no message", entry)
		}
	}
	if diff := cmp.Diff([]string{"body.api_key", "body.name"}, locations); diff != "" {
		t.Errorf("locations mismatch (-want +got):\n%s", diff)
	}
	if containsKey(problem, "value") {
		t.Errorf("problem has a value key: %s", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), rejectedSecret) {
		t.Errorf("problem echoes the rejected value: %s", recorder.Body.String())
	}
}

func TestUnreadableBodyProblemsDoNotEchoRequest(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		status      int
		message     string
	}{
		{
			name:        "unsupported content type",
			contentType: "text/" + rejectedSecret,
			body:        `{"name":"a"}`,
			status:      http.StatusUnsupportedMediaType,
			message:     "request content type is not supported",
		},
		{
			name:        "malformed JSON",
			contentType: "application/json",
			body:        `{"name":` + rejectedSecret + `}`,
			status:      http.StatusBadRequest,
			message:     "request body is not valid JSON",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newTestServer(t, httpapi.RejectingAuthenticator{})
			httpapi.Register(server.api, httpapi.RouteGroupPublic, huma.Operation{
				OperationID: "create-thing",
				Method:      http.MethodPost,
				Path:        "/api/v1/things",
			}, func(context.Context, *createThingInput) (*thingOutput, error) {
				t.Error("handler ran for an unreadable body")
				return &thingOutput{}, nil
			})
			request := newRequest(t, http.MethodPost, "/api/v1/things", test.body)
			request.Header.Set("Content-Type", test.contentType)

			recorder := server.serve(request)

			problem := assertProblem(t, recorder, test.status, "validation_failed")
			want := []any{map[string]any{"location": "body", "message": test.message}}
			if diff := cmp.Diff(want, problem["errors"]); diff != "" {
				t.Errorf("errors mismatch (-want +got):\n%s", diff)
			}
			if strings.Contains(recorder.Body.String(), rejectedSecret) {
				t.Errorf("problem echoes the request: %s", recorder.Body.String())
			}
		})
	}
}

func TestCodedErrorReturnsItsProblem(t *testing.T) {
	server := newTestServer(t, httpapi.RejectingAuthenticator{})
	httpapi.Register(server.api, httpapi.RouteGroupPublic, huma.Operation{
		OperationID: "get-thing",
		Method:      http.MethodGet,
		Path:        "/api/v1/things/{thing_id}",
	}, func(context.Context, *thingPathInput) (*thingOutput, error) {
		return nil, fmt.Errorf("load thing: %w", thingNotFoundError{})
	})

	recorder := server.serve(newRequest(t, http.MethodGet, "/api/v1/things/thing_1", ""))

	problem := assertProblem(t, recorder, http.StatusNotFound, "not_found")
	if problem["detail"] != "thing not found" {
		t.Errorf("detail = %v, want the coded error's message", problem["detail"])
	}
	if records := server.records(t, logging.HTTPInternalError); len(records) != 0 {
		t.Errorf("logged %d internal errors for a coded error", len(records))
	}
}

func TestPlainErrorReturnsInternalErrorAndLogsIt(t *testing.T) {
	server := newTestServer(t, httpapi.RejectingAuthenticator{})
	cause := errors.New("connection refused host=database.internal")
	httpapi.Register(server.api, httpapi.RouteGroupPublic, huma.Operation{
		OperationID: "list-things",
		Method:      http.MethodGet,
		Path:        "/api/v1/things",
	}, func(context.Context, *emptyInput) (*thingOutput, error) {
		return nil, fmt.Errorf("query things: %w", cause)
	})

	recorder := server.serve(newRequest(t, http.MethodGet, "/api/v1/things", ""))

	problem := assertProblem(t, recorder, http.StatusInternalServerError, "internal_error")
	if strings.Contains(recorder.Body.String(), "database.internal") {
		t.Errorf("problem leaks the error: %s", recorder.Body.String())
	}
	if problem["detail"] == "" {
		t.Error("detail is empty")
	}
	records := server.records(t, logging.HTTPInternalError)
	if len(records) != 1 {
		t.Fatalf("logged %d internal errors, want 1", len(records))
	}
	if records[0]["request_id"] != recorder.Header().Get("X-Request-Id") {
		t.Errorf("request_id = %v, want %s", records[0]["request_id"], recorder.Header().Get("X-Request-Id"))
	}
	if records[0]["error"] != "query things: connection refused host=database.internal" {
		t.Errorf("error = %v, want the wrapped cause", records[0]["error"])
	}
	if records[0]["level"] != "ERROR" {
		t.Errorf("level = %v, want ERROR", records[0]["level"])
	}
}

func TestCanceledRequestAnswersClientClosedRequest(t *testing.T) {
	tests := []struct {
		name      string
		group     httpapi.RouteGroup
		failure   func(ctx context.Context) error
		wantError string
	}{
		{
			name:      "plain error",
			group:     httpapi.RouteGroupPublic,
			failure:   func(ctx context.Context) error { return fmt.Errorf("list things: %w", ctx.Err()) },
			wantError: "list things: context canceled",
		},
		{
			name:      "coded error",
			group:     httpapi.RouteGroupPublic,
			failure:   func(context.Context) error { return fmt.Errorf("load thing: %w", httpapi.ErrNotFound) },
			wantError: "load thing: not found",
		},
		{
			name:      "authenticator error",
			group:     httpapi.RouteGroupRuntime,
			wantError: "select session: context canceled",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			server := newTestServer(t, cancelingAuthenticator{cancel: cancel})
			httpapi.Register(server.api, test.group, huma.Operation{
				OperationID: "list-things",
				Method:      http.MethodGet,
				Path:        "/api/v1/things",
			}, func(ctx context.Context, _ *emptyInput) (*thingOutput, error) {
				cancel()
				return nil, test.failure(ctx)
			})

			recorder := server.serve(newRequest(t, http.MethodGet, "/api/v1/things", "").WithContext(ctx))

			assertProblem(t, recorder, 499, "client_closed_request")
			if records := server.records(t, logging.HTTPInternalError); len(records) != 0 {
				t.Errorf("logged %d internal errors, want none", len(records))
			}
			records := server.records(t, logging.HTTPRequestCanceled)
			if len(records) != 1 {
				t.Fatalf("logged %d canceled requests, want 1", len(records))
			}
			if records[0]["level"] != "INFO" || records[0]["error"] != test.wantError {
				t.Errorf("canceled request record = %v, want level INFO and error %q", records[0], test.wantError)
			}
			if completed := server.records(t, logging.HTTPRequestCompleted); len(completed) != 1 || completed[0]["status"] != float64(499) {
				t.Errorf("request completed records = %v, want one with status 499", completed)
			}
		})
	}
}

func TestOversizedBodyReturnsPayloadTooLarge(t *testing.T) {
	server := newTestServer(t, httpapi.RejectingAuthenticator{})
	httpapi.Register(server.api, httpapi.RouteGroupPublic, huma.Operation{
		OperationID: "create-thing",
		Method:      http.MethodPost,
		Path:        "/api/v1/things",
	}, func(context.Context, *createThingInput) (*thingOutput, error) {
		t.Error("handler ran for an oversized body")
		return &thingOutput{}, nil
	})
	body := `{"name":"` + strings.Repeat("a", 1<<20) + `"}`

	recorder := server.serve(newRequest(t, http.MethodPost, "/api/v1/things", body))

	assertProblem(t, recorder, http.StatusRequestEntityTooLarge, "payload_too_large")
}

func TestNotFoundHandlerReturnsNotFoundProblem(t *testing.T) {
	server := newTestServer(t, httpapi.RejectingAuthenticator{})
	server.mux.Handle("/api/", server.api.NotFoundHandler())
	const unknownPath = "/api/v1/unknown_resource_name"

	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, "PROPFIND"} {
		t.Run(method, func(t *testing.T) {
			recorder := server.serve(newRequest(t, method, unknownPath, ""))

			problem := assertProblem(t, recorder, http.StatusNotFound, "not_found")
			if problem["detail"] != "not found" {
				t.Errorf("detail = %v, want not found", problem["detail"])
			}
			if strings.Contains(recorder.Body.String(), "unknown_resource_name") {
				t.Errorf("problem echoes the path: %s", recorder.Body.String())
			}
		})
	}
	if !errors.Is(fmt.Errorf("load thing: %w", httpapi.ErrNotFound), httpapi.ErrNotFound) {
		t.Error("wrapped ErrNotFound does not match ErrNotFound")
	}
}

func containsKey(value any, key string) bool {
	switch value := value.(type) {
	case map[string]any:
		for name, nested := range value {
			if name == key || containsKey(nested, key) {
				return true
			}
		}
	case []any:
		for _, nested := range value {
			if containsKey(nested, key) {
				return true
			}
		}
	}
	return false
}

func TestNewCodedErrorReturnsItsProblem(t *testing.T) {
	emailTaken := httpapi.NewCodedError(http.StatusConflict, "member_email_taken", "email is taken")
	server := newTestServer(t, httpapi.RejectingAuthenticator{})
	httpapi.Register(server.api, httpapi.RouteGroupPublic, huma.Operation{
		OperationID: "create-thing",
		Method:      http.MethodPost,
		Path:        "/api/v1/things",
	}, func(context.Context, *emptyInput) (*thingOutput, error) {
		return nil, fmt.Errorf("create member: %w", emailTaken)
	})

	recorder := server.serve(newRequest(t, http.MethodPost, "/api/v1/things", ""))

	problem := assertProblem(t, recorder, http.StatusConflict, "member_email_taken")
	if problem["detail"] != "email is taken" {
		t.Errorf("detail = %v, want email is taken", problem["detail"])
	}
	if !errors.Is(fmt.Errorf("create member: %w", emailTaken), emailTaken) {
		t.Error("wrapped coded error does not match itself")
	}
}

func TestNewValidationProblemListsEveryField(t *testing.T) {
	server := newTestServer(t, httpapi.RejectingAuthenticator{})
	httpapi.Register(server.api, httpapi.RouteGroupPublic, huma.Operation{
		OperationID: "create-thing",
		Method:      http.MethodPost,
		Path:        "/api/v1/things",
	}, func(context.Context, *emptyInput) (*thingOutput, error) {
		return nil, httpapi.NewValidationProblem(
			httpapi.ProblemError{Location: "body.email", Message: "expected an email address"},
			httpapi.ProblemError{Location: "body.password", Message: "expected 12 to 256 characters"},
		)
	})

	recorder := server.serve(newRequest(t, http.MethodPost, "/api/v1/things", ""))

	problem := assertProblem(t, recorder, http.StatusUnprocessableEntity, "validation_failed")
	want := []any{
		map[string]any{"location": "body.email", "message": "expected an email address"},
		map[string]any{"location": "body.password", "message": "expected 12 to 256 characters"},
	}
	if diff := cmp.Diff(want, problem["errors"]); diff != "" {
		t.Errorf("errors mismatch (-want +got):\n%s", diff)
	}
}

func TestNewCodedValidationProblemCarriesItsCode(t *testing.T) {
	server := newTestServer(t, httpapi.RejectingAuthenticator{})
	httpapi.Register(server.api, httpapi.RouteGroupPublic, huma.Operation{
		OperationID: "create-thing",
		Method:      http.MethodPost,
		Path:        "/api/v1/things",
	}, func(context.Context, *emptyInput) (*thingOutput, error) {
		return nil, fmt.Errorf("check thing: %w", httpapi.NewCodedValidationProblem("thing_invalid", "thing document is invalid",
			httpapi.ProblemError{Location: "body.when.all[0].value", Message: "expected an amount"},
		))
	})

	recorder := server.serve(newRequest(t, http.MethodPost, "/api/v1/things", ""))

	problem := assertProblem(t, recorder, http.StatusUnprocessableEntity, "thing_invalid")
	if problem["detail"] != "thing document is invalid" {
		t.Errorf("detail = %v, want thing document is invalid", problem["detail"])
	}
	want := []any{map[string]any{"location": "body.when.all[0].value", "message": "expected an amount"}}
	if diff := cmp.Diff(want, problem["errors"]); diff != "" {
		t.Errorf("errors mismatch (-want +got):\n%s", diff)
	}
}

func TestRateLimitedErrorSetsRetryAfter(t *testing.T) {
	tests := []struct {
		name       string
		retryAfter time.Duration
		want       string
	}{
		{name: "whole seconds", retryAfter: 900 * time.Second, want: "900"},
		{name: "rounded up", retryAfter: 61500 * time.Millisecond, want: "62"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newTestServer(t, httpapi.RejectingAuthenticator{})
			httpapi.Register(server.api, httpapi.RouteGroupPublic, huma.Operation{
				OperationID: "create-thing",
				Method:      http.MethodPost,
				Path:        "/api/v1/things",
			}, func(context.Context, *emptyInput) (*thingOutput, error) {
				return nil, fmt.Errorf("log in: %w", &httpapi.RateLimitedError{RetryAfter: test.retryAfter})
			})

			recorder := server.serve(newRequest(t, http.MethodPost, "/api/v1/things", ""))

			assertProblem(t, recorder, http.StatusTooManyRequests, "rate_limited")
			if retryAfter := recorder.Header().Values("Retry-After"); !slices.Equal(retryAfter, []string{test.want}) {
				t.Errorf("Retry-After = %v, want [%s]", retryAfter, test.want)
			}
		})
	}
}

func TestRateLimitedErrorFromAuthenticatorSetsRetryAfter(t *testing.T) {
	server := newTestServer(t, &recordingAuthenticator{err: &httpapi.RateLimitedError{RetryAfter: 30 * time.Second}})
	httpapi.Register(server.api, httpapi.RouteGroupRuntime, huma.Operation{
		OperationID: "get-thing",
		Method:      http.MethodGet,
		Path:        "/api/v1/things",
	}, func(context.Context, *emptyInput) (*thingOutput, error) {
		t.Error("handler ran after the authenticator failed")
		return &thingOutput{}, nil
	})
	request := newRequest(t, http.MethodGet, "/api/v1/things", "")
	request.Header.Set("Authorization", "Bearer test")

	recorder := server.serve(request)

	assertProblem(t, recorder, http.StatusTooManyRequests, "rate_limited")
	if retryAfter := recorder.Header().Get("Retry-After"); retryAfter != "30" {
		t.Errorf("Retry-After = %q, want 30", retryAfter)
	}
}
