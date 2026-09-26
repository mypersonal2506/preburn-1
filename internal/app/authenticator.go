package app

import (
	"context"
	"net/http"

	"github.com/preburn/preburn/internal/apikeys"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/members"
)

type requestAuthenticator struct {
	apiKeys  *apikeys.Authenticator
	sessions *members.SessionAuthenticator
}

func newRequestAuthenticator(application *App) requestAuthenticator {
	return requestAuthenticator{
		apiKeys:  application.APIKeys.Authenticator(),
		sessions: members.NewSessionAuthenticator(application.Members.Sessions()),
	}
}

// Authenticate resolves a request with an Authorization header of the Bearer
// scheme in any letter case through the API key authenticator, so a bad key
// never falls back to a session cookie. Every other request goes to the
// member session authenticator, including one whose Authorization header
// has another scheme, such as the Basic credentials a reverse proxy in front
// of the dashboard adds. The session authenticator answers
// httpapi.ErrAuthenticationRequired when the request has no session cookie.
func (authenticator requestAuthenticator) Authenticate(ctx context.Context, request *http.Request, group httpapi.RouteGroup) (httpapi.Principal, error) {
	if apikeys.HasBearerAuthorization(request) {
		return authenticator.apiKeys.Authenticate(ctx, request, group)
	}
	return authenticator.sessions.Authenticate(ctx, request, group)
}
