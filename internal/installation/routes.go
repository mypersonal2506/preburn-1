package installation

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/members"
)

const setupTag = "Setup"

type setupRoutes struct {
	service *Service
}

type setupStatusOutput struct {
	Body setupStatusResponse
}

type setupStatusResponse struct {
	SetupRequired bool `json:"setup_required" doc:"True until the first member is created through setup."`
}

type completeSetupInput struct {
	httpapi.ClientDetails
	Body completeSetupRequest
}

type completeSetupRequest struct {
	Token       string `json:"token" doc:"Token from the fragment of the latest setup link."`
	Email       string `json:"email" doc:"Email address of the first member."`
	DisplayName string `json:"display_name" doc:"Name the dashboard shows, 1 to 80 characters."`
	Password    string `json:"password" doc:"Password of the first member, 12 to 256 characters."`
}

type completeSetupOutput struct {
	SetCookie []http.Cookie `header:"Set-Cookie" doc:"Session cookies of the first member."`
	Body      members.MemberResponse
}

// RegisterRoutes adds the setup routes of service to api, both on the public
// group: GET /api/v1/setup/status and POST /api/v1/setup. Handlers read
// service only when they run.
func RegisterRoutes(api *httpapi.API, service *Service) {
	routes := &setupRoutes{service: service}
	httpapi.Register(api, httpapi.RouteGroupPublic, huma.Operation{
		OperationID: "get-setup-status",
		Method:      http.MethodGet,
		Path:        "/api/v1/setup/status",
		Summary:     "Get the setup status",
		Description: "Tells whether the installation still waits for its first member.",
		Tags:        []string{setupTag},
	}, routes.status)
	httpapi.Register(api, httpapi.RouteGroupPublic, huma.Operation{
		OperationID: "complete-setup",
		Method:      http.MethodPost,
		Path:        "/api/v1/setup",
		Summary:     "Complete setup",
		Description: "Creates the first member with the token of the latest setup link, completes setup, starts a session and sets the session cookies. After setup it returns 409 setup_not_available.",
		Tags:        []string{setupTag},
	}, routes.completeSetup)
}

func (routes *setupRoutes) status(ctx context.Context, _ *struct{}) (*setupStatusOutput, error) {
	setupRequired, err := routes.service.Status(ctx)
	if err != nil {
		return nil, err
	}
	return &setupStatusOutput{Body: setupStatusResponse{SetupRequired: setupRequired}}, nil
}

func (routes *setupRoutes) completeSetup(ctx context.Context, input *completeSetupInput) (*completeSetupOutput, error) {
	member, err := routes.service.CompleteSetup(ctx, CompleteSetupInput{
		Token:       input.Body.Token,
		Email:       input.Body.Email,
		DisplayName: input.Body.DisplayName,
		Password:    input.Body.Password,
	})
	if err != nil {
		return nil, err
	}
	clientIP := input.ClientIP(routes.service.configuration.TrustedProxies)
	token, err := routes.service.members.Sessions().Start(ctx, member.ID, input.UserAgent(), clientIP)
	if err != nil {
		return nil, err
	}
	return &completeSetupOutput{
		SetCookie: members.SessionCookies(token, routes.service.configuration.SecureCookies()),
		Body:      members.NewMemberResponse(member),
	}, nil
}
