package apikeys

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
)

const apiKeysTag = "API keys"

// APIKeyResponse is the API representation of an API key. It never holds the
// secret.
type APIKeyResponse struct {
	ID             string     `json:"id" doc:"API key id, such as key_01jbvagescfn78y0938nkrkayd. It is not the secret."`
	Name           string     `json:"name" doc:"Name that tells keys apart."`
	Scope          Scope      `json:"scope" enum:"runtime,admin" doc:"Runtime keys call the runtime routes. Admin keys also call the admin routes."`
	SecretLastFour string     `json:"secret_last_four" doc:"Last four characters of the secret."`
	Status         Status     `json:"status" enum:"active,disabled" doc:"Only active keys authenticate."`
	LastUsedAt     *time.Time `json:"last_used_at" doc:"When the key last authenticated a request, updated at most once a minute. Null before its first use."`
	CreatedAt      time.Time  `json:"created_at" doc:"When the key was created."`
}

// CreatedAPIKeyResponse is the API representation of a new API key with its
// secret, which no later response holds.
type CreatedAPIKeyResponse struct {
	APIKeyResponse
	Secret string `json:"secret" doc:"Secret to send as a bearer token. It appears only in this response."`
}

type apiKeyRoutes struct {
	service *Service
}

type listAPIKeysInput struct {
	Cursor string `query:"cursor" doc:"Cursor from next_cursor of the previous page. Omit it for the first page."`
	Limit  int    `query:"limit" doc:"Page size from 1 to 100, 50 when omitted."`
}

type createAPIKeyInput struct {
	Body createAPIKeyRequest
}

type createAPIKeyRequest struct {
	Name  string `json:"name" doc:"Name that tells keys apart, 1 to 80 characters."`
	Scope Scope  `json:"scope" enum:"runtime,admin" doc:"Runtime keys call the runtime routes. Admin keys also call the admin routes."`
}

type createAPIKeyOutput struct {
	Body CreatedAPIKeyResponse
}

type revokeAPIKeyInput struct {
	APIKeyID string `path:"api_key_id" doc:"API key id, such as key_01jbvagescfn78y0938nkrkayd."`
}

// RegisterRoutes adds the API key routes of service to api on the dashboard
// group: GET /api/v1/api-keys, POST /api/v1/api-keys and
// DELETE /api/v1/api-keys/{api_key_id}. They act in the environment of the
// member session. Handlers read service only when they run.
func RegisterRoutes(api *httpapi.API, service *Service) {
	routes := &apiKeyRoutes{service: service}
	httpapi.Register(api, httpapi.RouteGroupDashboard, huma.Operation{
		OperationID: "list-api-keys",
		Method:      http.MethodGet,
		Path:        "/api/v1/api-keys",
		Summary:     "List API keys",
		Description: "Returns the active and disabled API keys of the environment, newest first.",
		Tags:        []string{apiKeysTag},
	}, routes.list)
	httpapi.Register(api, httpapi.RouteGroupDashboard, huma.Operation{
		OperationID:   "create-api-key",
		Method:        http.MethodPost,
		Path:          "/api/v1/api-keys",
		Summary:       "Create an API key",
		Description:   "Creates an API key in the environment and returns its secret. The secret appears only in this response.",
		Tags:          []string{apiKeysTag},
		DefaultStatus: http.StatusCreated,
	}, routes.create)
	httpapi.Register(api, httpapi.RouteGroupDashboard, huma.Operation{
		OperationID:   "revoke-api-key",
		Method:        http.MethodDelete,
		Path:          "/api/v1/api-keys/{api_key_id}",
		Summary:       "Revoke an API key",
		Description:   "Disables the API key, so requests that send it get 401 authentication_required.",
		Tags:          []string{apiKeysTag},
		DefaultStatus: http.StatusNoContent,
	}, routes.revoke)
}

func (routes *apiKeyRoutes) list(ctx context.Context, input *listAPIKeysInput) (*httpapi.Page[APIKeyResponse], error) {
	environment, err := httpapi.EnvironmentFromContext(ctx)
	if err != nil {
		return nil, err
	}
	keys, nextCursor, err := routes.service.List(ctx, environment, input.Cursor, input.Limit)
	if err != nil {
		return nil, err
	}
	responses := make([]APIKeyResponse, 0, len(keys))
	for _, key := range keys {
		responses = append(responses, newAPIKeyResponse(key))
	}
	return httpapi.NewPage(responses, nextCursor), nil
}

func (routes *apiKeyRoutes) create(ctx context.Context, input *createAPIKeyInput) (*createAPIKeyOutput, error) {
	principal, err := httpapi.MemberPrincipalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	key, secret, err := routes.service.Create(ctx, principal.Environment, input.Body.Name, input.Body.Scope, &principal.MemberID)
	if err != nil {
		return nil, err
	}
	return &createAPIKeyOutput{Body: CreatedAPIKeyResponse{APIKeyResponse: newAPIKeyResponse(key), Secret: secret}}, nil
}

func (routes *apiKeyRoutes) revoke(ctx context.Context, input *revokeAPIKeyInput) (*struct{}, error) {
	environment, err := httpapi.EnvironmentFromContext(ctx)
	if err != nil {
		return nil, err
	}
	apiKeyID, err := identifiers.Decode(identifiers.PrefixAPIKey, input.APIKeyID)
	if err != nil {
		return nil, httpapi.ErrNotFound
	}
	if err := routes.service.Revoke(ctx, environment, apiKeyID); err != nil {
		return nil, err
	}
	return &struct{}{}, nil
}

func newAPIKeyResponse(key APIKey) APIKeyResponse {
	return APIKeyResponse{
		ID:             identifiers.Encode(identifiers.PrefixAPIKey, key.ID),
		Name:           key.Name,
		Scope:          key.Scope,
		SecretLastFour: key.SecretLastFour,
		Status:         key.Status,
		LastUsedAt:     key.LastUsedAt,
		CreatedAt:      key.CreatedAt,
	}
}
