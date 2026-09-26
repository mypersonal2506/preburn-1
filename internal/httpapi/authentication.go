package httpapi

import (
	"context"
	"maps"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

const (
	environmentQueryParameter = "environment"
	environmentQueryLocation  = "query.environment"
	environmentQueryRule      = "expected test or live"
)

// Principal is the authenticated caller of a request, such as an API key or
// a member session. The Authenticator decides its concrete type, and handlers
// read it with PrincipalFromContext. The principals of Preburn implement
// EnvironmentPrincipal, so handlers read their environment with
// EnvironmentFromContext.
type Principal any

// Authenticator resolves the principal of each request to a route outside
// RouteGroupPublic.
type Authenticator interface {
	// Authenticate returns the principal that request presents for a route of
	// group. An error ends the request with a problem: a CodedError such as
	// ErrAuthenticationRequired becomes that problem, any other error a 500
	// internal_error, and any error once the client closed the request a 499
	// client_closed_request.
	Authenticate(ctx context.Context, request *http.Request, group RouteGroup) (Principal, error)
}

// RejectingAuthenticator is the Authenticator that rejects every request with
// ErrAuthenticationRequired, so only public routes answer.
type RejectingAuthenticator struct{}

type principalContextKey struct{}

// ErrAuthenticationRequired is the CodedError for a request with no or
// invalid credentials: 401 authentication_required. Unknown and revoked
// credentials get the same problem.
var ErrAuthenticationRequired error = &codedError{
	status:  http.StatusUnauthorized,
	code:    codeAuthenticationRequired,
	message: "authentication required",
}

// ErrScopeForbidden is the CodedError for valid credentials on a route whose
// group does not admit them, such as a member session on a runtime route or a
// runtime key on an admin route: 403 scope_forbidden.
var ErrScopeForbidden error = &codedError{
	status:  http.StatusForbidden,
	code:    codeScopeForbidden,
	message: "credentials do not grant access to this route",
}

// ErrCSRFInvalid is the CodedError for a member session request with an
// unsafe method whose X-CSRF-Token header is missing or differs from its
// preburn_csrf cookie: 403 csrf_invalid.
var ErrCSRFInvalid error = &codedError{
	status:  http.StatusForbidden,
	code:    codeCSRFInvalid,
	message: "CSRF token is missing or does not match",
}

// Authenticate returns ErrAuthenticationRequired.
func (RejectingAuthenticator) Authenticate(context.Context, *http.Request, RouteGroup) (Principal, error) {
	return nil, ErrAuthenticationRequired
}

// PrincipalFromContext returns the principal the Authenticator resolved for
// the request of ctx, or nil on a route of RouteGroupPublic.
func PrincipalFromContext(ctx context.Context) Principal {
	return ctx.Value(principalContextKey{})
}

func (api *API) authenticate(ctx huma.Context, next func(huma.Context)) {
	group := ctx.Operation().Metadata[routeGroupMetadataKey].(RouteGroup)
	if group == RouteGroupPublic {
		next(ctx)
		return
	}
	request, writer := humago.Unwrap(ctx)
	principal, err := api.principal(ctx, request, group)
	if err != nil {
		problem := api.problemFor(ctx.Context(), err)
		maps.Copy(writer.Header(), problem.headers)
		writeProblem(ctx.Context(), api.logger, writer, problem)
		return
	}
	next(huma.WithValue(ctx, principalContextKey{}, principal))
}

func (api *API) principal(ctx huma.Context, request *http.Request, group RouteGroup) (Principal, error) {
	if _, fromQuery := ctx.Operation().Metadata[environmentQueryMetadataKey]; fromQuery {
		values := request.URL.Query()[environmentQueryParameter]
		if len(values) != 1 {
			return nil, validationProblem(environmentQueryLocation, environmentQueryRule)
		}
		environment, err := ParseEnvironment(values[0])
		if err != nil {
			return nil, validationProblem(environmentQueryLocation, environmentQueryRule)
		}
		request.Header.Set(EnvironmentHeader, string(environment))
	}
	return api.authenticator.Authenticate(ctx.Context(), request, group)
}
