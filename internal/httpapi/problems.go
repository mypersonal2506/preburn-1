package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"reflect"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/preburn/preburn/internal/logging"
)

const (
	problemContentType        = "application/problem+json"
	problemTypeBase           = "https://github.com/preburn/preburn/blob/main/docs/errors.md#"
	internalErrorDetail       = "internal error"
	validationDetail          = "validation failed"
	clientClosedRequestDetail = "the client closed the request"
	clientClosedRequestStatus = 499
	bodyLocation              = "body"
	undecodableBodyMessage    = "request body does not match the request schema"
	retryAfterHeader          = "Retry-After"
)

const (
	codeValidationFailed         = "validation_failed"
	codeNotFound                 = "not_found"
	codeAuthenticationRequired   = "authentication_required"
	codeScopeForbidden           = "scope_forbidden"
	codeCSRFInvalid              = "csrf_invalid"
	codeEnvironmentHeaderInvalid = "environment_header_invalid"
	codeRateLimited              = "rate_limited"
	codeInvalidCursor            = "invalid_cursor"
	codePayloadTooLarge          = "payload_too_large"
	codeClientClosedRequest      = "client_closed_request"
	codeInternalError            = "internal_error"
)

// Problem is the body of every error response, an RFC 9457 problem details
// object sent as application/problem+json. Code is the stable error code and
// Type links to its entry in docs/errors.md. Errors lists the invalid fields
// of a validation failure by location and message, never with the rejected
// values.
type Problem struct {
	Type   string         `json:"type" doc:"Link to the documentation of the error code."`
	Title  string         `json:"title" doc:"Text of the HTTP status."`
	Status int            `json:"status" doc:"HTTP status code."`
	Detail string         `json:"detail" doc:"Explanation of this occurrence of the error."`
	Code   string         `json:"code" doc:"Stable error code, such as validation_failed."`
	Errors []ProblemError `json:"errors,omitempty" nullable:"false" doc:"Invalid fields of the request."`

	headers http.Header
}

// ProblemError locates one invalid field of a request, such as body.name or
// query.limit, and states what is wrong with it.
type ProblemError struct {
	Location string `json:"location" doc:"Where the invalid field is, such as body.name or query.limit."`
	Message  string `json:"message" doc:"What is wrong with the field."`
}

// CodedError is an error that maps to its own problem. When the chain of an
// error returned by a handler or an Authenticator holds a CodedError, the
// response is a problem with its status, its code, and its message as the
// detail. The message reaches the client, so it never holds request values or
// secrets.
type CodedError interface {
	error
	// ProblemStatus returns the HTTP status of the problem.
	ProblemStatus() int
	// ProblemCode returns the stable error code of the problem.
	ProblemCode() string
}

type codedError struct {
	status  int
	code    string
	message string
}

// RateLimitedError is the CodedError for a request over a rate limit: 429
// rate_limited. The problem response carries a Retry-After header holding
// RetryAfter rounded up to whole seconds.
type RateLimitedError struct {
	// RetryAfter is how long until the limit admits the next attempt.
	RetryAfter time.Duration
}

// ErrNotFound is the CodedError for a resource or path that does not exist:
// 404 not_found.
var ErrNotFound error = &codedError{
	status:  http.StatusNotFound,
	code:    codeNotFound,
	message: "not found",
}

var unreadableBodyMessages = map[int]string{
	http.StatusBadRequest:           "request body is not valid JSON",
	http.StatusUnsupportedMediaType: "request content type is not supported",
}

// Error returns the problem's detail.
func (problem *Problem) Error() string {
	return problem.Detail
}

// GetStatus returns the problem's HTTP status. Huma writes a returned error
// that has this method with its status.
func (problem *Problem) GetStatus() int {
	return problem.Status
}

// ContentType returns application/problem+json, the content type Huma writes
// a problem with.
func (*Problem) ContentType(string) string {
	return problemContentType
}

// GetHeaders returns the headers the problem response carries, such as
// Retry-After for rate_limited, or nil. Huma writes them with the problem.
func (problem *Problem) GetHeaders() http.Header {
	return problem.headers
}

// Error returns the message sent as the problem's detail.
func (coded *codedError) Error() string {
	return coded.message
}

// ProblemStatus returns the HTTP status of the problem.
func (coded *codedError) ProblemStatus() int {
	return coded.status
}

// ProblemCode returns the stable error code of the problem.
func (coded *codedError) ProblemCode() string {
	return coded.code
}

// Error returns the message sent as the problem's detail.
func (*RateLimitedError) Error() string {
	return "rate limit exceeded"
}

// ProblemStatus returns 429.
func (*RateLimitedError) ProblemStatus() int {
	return http.StatusTooManyRequests
}

// ProblemCode returns rate_limited.
func (*RateLimitedError) ProblemCode() string {
	return codeRateLimited
}

// NewCodedError returns a CodedError that becomes a problem with status, the
// stable code and message as its detail. Domain packages declare their errors
// with it, such as a 409 member_email_taken. The message reaches the client,
// so it never holds request values or secrets.
func NewCodedError(status int, code string, message string) error {
	return &codedError{status: status, code: code, message: message}
}

// NewValidationProblem returns the 422 validation_failed problem that lists
// fieldErrors, each naming one invalid field by location, such as
// body.email, and stating what is wrong with it. The messages reach the
// client, so they never hold the rejected values.
func NewValidationProblem(fieldErrors ...ProblemError) *Problem {
	problem := newProblem(http.StatusUnprocessableEntity, codeValidationFailed, validationDetail)
	problem.Errors = fieldErrors
	return problem
}

// NewCodedValidationProblem returns a 422 problem with the stable code, detail
// and fieldErrors, for invalid input that a domain answers with its own code,
// such as policy_invalid. Each field error names one invalid field by
// location, such as body.when.all[1].value, and states what is wrong with
// it. The detail and messages reach the client, so they never hold the
// rejected values.
func NewCodedValidationProblem(code string, detail string, fieldErrors ...ProblemError) *Problem {
	problem := newProblem(http.StatusUnprocessableEntity, code, detail)
	problem.Errors = fieldErrors
	return problem
}

// NotFoundHandler answers every request with the ErrNotFound problem. The
// server mounts it on /api/, so an API path that no route matches gets a
// problem instead of the dashboard.
func (api *API) NotFoundHandler() http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writeProblem(request.Context(), api.logger, writer, api.problemFor(request.Context(), ErrNotFound))
	})
}

func (api *API) problemFor(ctx context.Context, err error) *Problem {
	if errors.Is(ctx.Err(), context.Canceled) {
		api.logger.Info(ctx, logging.HTTPRequestCanceled, slog.String("error", err.Error()))
		return newProblem(clientClosedRequestStatus, codeClientClosedRequest, clientClosedRequestDetail)
	}
	if problem, isProblem := errors.AsType[*Problem](err); isProblem {
		return problem
	}
	if coded, isCoded := errors.AsType[CodedError](err); isCoded {
		problem := newProblem(coded.ProblemStatus(), coded.ProblemCode(), coded.Error())
		if limited, isLimited := coded.(*RateLimitedError); isLimited {
			retryAfterSeconds := int64(math.Ceil(limited.RetryAfter.Seconds()))
			problem.headers = http.Header{retryAfterHeader: {strconv.FormatInt(retryAfterSeconds, 10)}}
		}
		return problem
	}
	api.logger.Error(ctx, logging.HTTPInternalError, slog.String("error", err.Error()))
	return newProblem(http.StatusInternalServerError, codeInternalError, internalErrorDetail)
}

func newRequestProblem(ctx huma.Context, status int, message string, causes ...error) huma.StatusError {
	problem := newHumaProblem(status, message, causes...).(*Problem)
	inputType := ctx.Operation().Metadata[inputTypeMetadataKey].(reflect.Type)
	for index := range problem.Errors {
		problem.Errors[index].Location = declaredLocation(inputType, problem.Errors[index].Location)
	}
	return problem
}

func newHumaProblem(status int, message string, causes ...error) huma.StatusError {
	if status >= http.StatusInternalServerError {
		return newProblem(status, codeInternalError, internalErrorDetail)
	}
	code := codeValidationFailed
	if status == http.StatusRequestEntityTooLarge {
		code = codePayloadTooLarge
	}
	problem := newProblem(status, code, message)
	for _, cause := range causes {
		problem.Errors = append(problem.Errors, problemError(status, cause))
	}
	return problem
}

func problemError(status int, cause error) ProblemError {
	var detailer huma.ErrorDetailer
	if !errors.As(cause, &detailer) {
		return ProblemError{Message: cause.Error()}
	}
	detail := detailer.ErrorDetail()
	causeError := ProblemError{Location: detail.Location, Message: detail.Message}
	if causeError.Location != bodyLocation {
		return causeError
	}
	if unreadableBodyMessage, unreadable := unreadableBodyMessages[status]; unreadable {
		causeError.Message = unreadableBodyMessage
	}
	if _, rawBody := detail.Value.(string); rawBody && status == http.StatusUnprocessableEntity {
		causeError.Message = undecodableBodyMessage
	}
	return causeError
}

func validationProblem(location string, message string) *Problem {
	return NewValidationProblem(ProblemError{Location: location, Message: message})
}

func newProblem(status int, code string, detail string) *Problem {
	return &Problem{
		Type:   problemTypeBase + code,
		Title:  http.StatusText(status),
		Status: status,
		Detail: detail,
		Code:   code,
	}
}
