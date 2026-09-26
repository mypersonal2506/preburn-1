package httpapi

import (
	"context"
	"net/http"
	"reflect"
	"sync"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/preburn/preburn/internal/logging"
)

const (
	apiTitle                    = "Preburn"
	apiLicense                  = "Apache-2.0"
	openAPIPath                 = "/api/v1/openapi"
	docsPath                    = "/api/docs"
	schemaReferencePrefix       = "#/components/schemas/"
	defaultResponseKey          = "default"
	errorResponseDescription    = "Error"
	requestBodyMaximumBytes     = 1 << 20
	routeGroupMetadataKey       = "route_group"
	inputTypeMetadataKey        = "input_type"
	environmentQueryMetadataKey = "environment_query"
	apiKeySecurityScheme        = "api_key"
	memberSessionSecurityScheme = "member_session"
	sessionCookieName           = "preburn_session"
)

// RouteGroup names the callers a route admits. Every route declares one when
// it registers, and the Authenticator checks the caller against it.
type RouteGroup string

const (
	// RouteGroupPublic routes take no credentials, such as setup and login.
	RouteGroupPublic RouteGroup = "public"
	// RouteGroupRuntime routes take a runtime or admin API key, such as check
	// and report.
	RouteGroupRuntime RouteGroup = "runtime"
	// RouteGroupAdmin routes take an admin API key or a member session, such
	// as configuration and reads.
	RouteGroupAdmin RouteGroup = "admin"
	// RouteGroupDashboard routes take a member session only, such as members,
	// API keys and settings.
	RouteGroupDashboard RouteGroup = "dashboard"
)

// API is the Preburn HTTP API: a Huma API on a ServeMux, the logger of
// internal errors, and the Authenticator of non-public routes. Create one with
// NewAPI and add routes with Register.
type API struct {
	humaAPI       huma.API
	logger        *logging.Logger
	authenticator Authenticator
}

// NamedSchema is implemented by request and response body types whose Go
// name does not describe them outside their package, such as
// signals.Response. The OpenAPI document names the schema of such a type
// SchemaName, and every other schema after its Go type name. A type that
// embeds a NamedSchema type inherits its SchemaName, so it declares its own.
// Two types with one schema name panic when their routes register.
type NamedSchema interface {
	// SchemaName returns the name of the type's schema in the OpenAPI
	// document, such as SignalsResponse.
	SchemaName() string
}

var (
	installProblemConstructor = sync.OnceFunc(func() {
		huma.NewError = newHumaProblem
		huma.NewErrorWithContext = newRequestProblem
	})
	namedSchemaType = reflect.TypeFor[NamedSchema]()
)

// NewAPI returns an API that serves its routes on mux. The OpenAPI 3.1
// document, titled Preburn with version and the Apache-2.0 license, is served
// at /api/v1/openapi.json and the docs page at /api/docs. Requests to routes
// outside RouteGroupPublic are authenticated by authenticator. NewAPI installs
// the problem constructors as Huma's process-wide error constructors, so
// validation failures become problems with code validation_failed whose
// locations stop at map fields.
func NewAPI(mux *http.ServeMux, version string, logger *logging.Logger, authenticator Authenticator) *API {
	installProblemConstructor()
	config := huma.DefaultConfig(apiTitle, version)
	config.CreateHooks = nil
	config.Components.Schemas = huma.NewMapRegistry(schemaReferencePrefix, schemaName)
	config.SchemasPath = ""
	config.OpenAPIPath = openAPIPath
	config.DocsPath = docsPath
	config.Info.License = &huma.License{Name: apiLicense, Identifier: apiLicense}
	config.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		apiKeySecurityScheme: {
			Type:        "http",
			Scheme:      "bearer",
			Description: "Runtime or admin API key sent as a bearer token.",
		},
		memberSessionSecurityScheme: {
			Type:        "apiKey",
			In:          "cookie",
			Name:        sessionCookieName,
			Description: "Member session cookie. Unsafe methods also send the X-CSRF-Token header.",
		},
	}
	api := &API{humaAPI: humago.New(mux, config), logger: logger, authenticator: authenticator}
	api.humaAPI.UseMiddleware(api.authenticate)
	return api
}

// Register adds operation to api with handler, and is the only way routes
// register besides RegisterEventStream. It records group and the input type
// in operation.Metadata, which callers leave unset, documents the group's
// security schemes and the Problem schema as the default response of the
// operation, and limits request bodies to 1 MiB unless operation sets
// MaxBodyBytes. Before handler runs on a route outside RouteGroupPublic, a
// path parameter, query parameter or body field holding text Postgres cannot
// store, such as a NUL character or an unpaired surrogate, returns a 422
// validation_failed problem at that field, and at the whole field for a map.
// Public routes take credentials, whose handlers answer a malformed value
// like a wrong one, so they check their own input. An error from handler
// becomes a problem: a CodedError in its chain becomes that problem, any
// other error a 500 internal_error logged as http.internal_error with the
// request id. Once the client closed the request, any error becomes a 499
// client_closed_request logged at info level as http.request_canceled.
// Register panics on an unknown group.
func Register[Input, Output any](api *API, group RouteGroup, operation huma.Operation, handler func(ctx context.Context, input *Input) (*Output, error)) {
	operation.Metadata = map[string]any{}
	register(api, group, operation, handler)
}

// RegisterEventStream adds operation like Register, for a route that streams
// server-sent events to the browser's EventSource, which cannot send
// headers. Before the Authenticator runs, it sets the X-Preburn-Environment
// header of the request to the value of the environment query parameter,
// replacing any header the request sent, so a member session acts in the
// environment the query names. A missing, repeated or unknown environment
// returns a 422 validation_failed problem at query.environment before
// authentication.
func RegisterEventStream[Input, Output any](api *API, group RouteGroup, operation huma.Operation, handler func(ctx context.Context, input *Input) (*Output, error)) {
	operation.Metadata = map[string]any{environmentQueryMetadataKey: true}
	register(api, group, operation, handler)
}

// JSONResponse returns the OpenAPI response with description whose
// application/json body has the schema of Body, for an operation that answers
// a status other than its DefaultStatus with its output body, such as the 200
// of a create that found an earlier resource. The operation declares it under
// that status in its Responses.
func JSONResponse[Body any](api *API, description string) *huma.Response {
	return &huma.Response{
		Description: description,
		Content:     map[string]*huma.MediaType{jsonContentType: {Schema: api.schemaReference(reflect.TypeFor[Body]())}},
	}
}

func register[Input, Output any](api *API, group RouteGroup, operation huma.Operation, handler func(ctx context.Context, input *Input) (*Output, error)) {
	operation.Metadata[routeGroupMetadataKey] = group
	operation.Metadata[inputTypeMetadataKey] = reflect.TypeFor[Input]()
	operation.Security = group.securityRequirements()
	if operation.Responses == nil {
		operation.Responses = map[string]*huma.Response{}
	}
	operation.Responses[defaultResponseKey] = &huma.Response{
		Description: errorResponseDescription,
		Content:     map[string]*huma.MediaType{problemContentType: {Schema: api.schemaReference(reflect.TypeFor[Problem]())}},
	}
	if operation.MaxBodyBytes == 0 {
		operation.MaxBodyBytes = requestBodyMaximumBytes
	}
	huma.Register(api.humaAPI, operation, func(ctx context.Context, input *Input) (*Output, error) {
		if group != RouteGroupPublic {
			if err := requireStorableText(input); err != nil {
				return nil, api.problemFor(ctx, err)
			}
		}
		output, err := handler(ctx, input)
		if err != nil {
			return nil, api.problemFor(ctx, err)
		}
		return output, nil
	})
}

func (api *API) schemaReference(schemaType reflect.Type) *huma.Schema {
	return api.humaAPI.OpenAPI().Components.Schemas.Schema(schemaType, true, "")
}

func schemaName(schemaType reflect.Type, hint string) string {
	for schemaType.Kind() == reflect.Pointer {
		schemaType = schemaType.Elem()
	}
	if schemaType.Implements(namedSchemaType) {
		return reflect.Zero(schemaType).Interface().(NamedSchema).SchemaName()
	}
	return huma.DefaultSchemaNamer(schemaType, hint)
}

func (group RouteGroup) securityRequirements() []map[string][]string {
	apiKey := map[string][]string{apiKeySecurityScheme: {}}
	memberSession := map[string][]string{memberSessionSecurityScheme: {}}
	switch group {
	case RouteGroupPublic:
		return nil
	case RouteGroupRuntime:
		return []map[string][]string{apiKey}
	case RouteGroupAdmin:
		return []map[string][]string{apiKey, memberSession}
	case RouteGroupDashboard:
		return []map[string][]string{memberSession}
	}
	panic("unknown route group " + string(group))
}
