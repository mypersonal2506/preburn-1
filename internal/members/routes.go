package members

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/secrets"
)

const (
	authenticationTag   = "Authentication"
	passwordChangeLimit = "password_change"
)

// MemberResponse is the API representation of a member.
type MemberResponse struct {
	ID          string     `json:"id" doc:"Member id, such as mem_01jbvagescfn78y0938nkrkayd."`
	Email       string     `json:"email" doc:"Email address the member signs in with."`
	DisplayName string     `json:"display_name" doc:"Name the dashboard shows."`
	Status      Status     `json:"status" enum:"active,disabled" doc:"Only active members sign in."`
	HasPassword bool       `json:"has_password" doc:"False until the member accepts their invite and sets a password."`
	LastLoginAt *time.Time `json:"last_login_at" doc:"When the member last signed in with a password login, setup, or an invite or reset link. Null when they never did."`
	CreatedAt   time.Time  `json:"created_at" doc:"When the member was created."`
}

type authenticationRoutes struct {
	service *Service
}

type loginInput struct {
	httpapi.ClientDetails
	Body loginRequest
}

type loginRequest struct {
	Email    string `json:"email" doc:"Email address of the member."`
	Password string `json:"password" doc:"Password of the member."`
}

type sessionOutput struct {
	SetCookie []http.Cookie `header:"Set-Cookie" doc:"Session cookies, set when a session starts or is refreshed."`
	Body      MemberResponse
}

type logoutInput struct {
	SessionToken string `cookie:"preburn_session" doc:"Token of the session to end."`
}

type logoutOutput struct {
	SetCookie []http.Cookie `header:"Set-Cookie" doc:"Expired session cookies."`
}

type currentMemberInput struct {
	SessionToken string `cookie:"preburn_session" doc:"Token of the current session."`
	CSRFToken    string `cookie:"preburn_csrf" doc:"CSRF token of the current session, replaced when missing."`
}

type updateCurrentMemberInput struct {
	httpapi.ClientDetails
	Body updateCurrentMemberRequest
}

type updateCurrentMemberRequest struct {
	DisplayName *string                `json:"display_name,omitempty" doc:"New display name, 1 to 80 characters."`
	Password    *passwordChangeRequest `json:"password,omitempty" doc:"Password change. It starts a new session and ends every other session of the member."`
}

type passwordChangeRequest struct {
	Current string `json:"current" doc:"Current password of the member."`
	New     string `json:"new" doc:"New password, 12 to 256 characters."`
}

// NewMemberResponse returns the API representation of member.
func NewMemberResponse(member Member) MemberResponse {
	return MemberResponse{
		ID:          identifiers.Encode(identifiers.PrefixMember, member.ID),
		Email:       member.Email,
		DisplayName: member.DisplayName,
		Status:      member.Status,
		HasPassword: member.HasPassword,
		LastLoginAt: member.LastLoginAt,
		CreatedAt:   member.CreatedAt,
	}
}

// RegisterRoutes adds the authentication routes of service to api:
// POST /api/v1/auth/login on the public group, and POST /api/v1/auth/logout,
// GET /api/v1/auth/me and PATCH /api/v1/auth/me on the dashboard group.
// Handlers read service only when they run.
func RegisterRoutes(api *httpapi.API, service *Service) {
	routes := &authenticationRoutes{service: service}
	httpapi.Register(api, httpapi.RouteGroupPublic, huma.Operation{
		OperationID: "login",
		Method:      http.MethodPost,
		Path:        "/api/v1/auth/login",
		Summary:     "Log in",
		Description: "Checks the email and password, starts a session and sets the session cookies. Allows 10 attempts per 15 minutes per email and per client address.",
		Tags:        []string{authenticationTag},
	}, routes.login)
	httpapi.Register(api, httpapi.RouteGroupDashboard, huma.Operation{
		OperationID:   "logout",
		Method:        http.MethodPost,
		Path:          "/api/v1/auth/logout",
		Summary:       "Log out",
		Description:   "Ends the current session and removes the session cookies.",
		Tags:          []string{authenticationTag},
		DefaultStatus: http.StatusNoContent,
	}, routes.logout)
	httpapi.Register(api, httpapi.RouteGroupDashboard, huma.Operation{
		OperationID: "get-current-member",
		Method:      http.MethodGet,
		Path:        "/api/v1/auth/me",
		Summary:     "Get the signed-in member",
		Description: "Returns the member of the current session and renews the session cookies, with a new CSRF token when the browser has none.",
		Tags:        []string{authenticationTag},
	}, routes.currentMember)
	httpapi.Register(api, httpapi.RouteGroupDashboard, huma.Operation{
		OperationID: "update-current-member",
		Method:      http.MethodPatch,
		Path:        "/api/v1/auth/me",
		Summary:     "Update the signed-in member",
		Description: "Changes the display name or the password. A password change needs the current password, starts a new session and ends every other session of the member. Allows 10 password change attempts per 15 minutes per member.",
		Tags:        []string{authenticationTag},
	}, routes.updateCurrentMember)
}

func (routes *authenticationRoutes) login(ctx context.Context, input *loginInput) (*sessionOutput, error) {
	clientIP := input.ClientIP(routes.service.configuration.TrustedProxies)
	member, token, err := routes.service.Login(ctx, input.Body.Email, input.Body.Password, input.UserAgent(), clientIP)
	if err != nil {
		return nil, err
	}
	return &sessionOutput{
		SetCookie: SessionCookies(token, routes.service.configuration.SecureCookies()),
		Body:      NewMemberResponse(member),
	}, nil
}

func (routes *authenticationRoutes) logout(ctx context.Context, input *logoutInput) (*logoutOutput, error) {
	if err := routes.service.sessions.End(ctx, input.SessionToken); err != nil {
		return nil, err
	}
	return &logoutOutput{SetCookie: ExpiredSessionCookies(routes.service.configuration.SecureCookies())}, nil
}

func (routes *authenticationRoutes) currentMember(ctx context.Context, input *currentMemberInput) (*sessionOutput, error) {
	principal, err := httpapi.MemberPrincipalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	member, err := routes.service.Member(ctx, principal.MemberID)
	if err != nil {
		return nil, err
	}
	csrfToken := input.CSRFToken
	if csrfToken == "" {
		csrfToken = secrets.NewToken()
	}
	secure := routes.service.configuration.SecureCookies()
	return &sessionOutput{
		SetCookie: []http.Cookie{
			sessionCookie(input.SessionToken, sessionCookieMaxAge, secure),
			csrfCookie(csrfToken, sessionCookieMaxAge, secure),
		},
		Body: NewMemberResponse(member),
	}, nil
}

func (routes *authenticationRoutes) updateCurrentMember(ctx context.Context, input *updateCurrentMemberInput) (*sessionOutput, error) {
	principal, err := httpapi.MemberPrincipalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	update := AccountUpdate{
		DisplayName: input.Body.DisplayName,
		UserAgent:   input.UserAgent(),
		ClientIP:    input.ClientIP(routes.service.configuration.TrustedProxies),
	}
	if input.Body.Password != nil {
		if err := routes.service.countLoginAttempt(ctx, passwordChangeLimit, principal.MemberID.String()); err != nil {
			return nil, err
		}
		update.Password = &PasswordChange{Current: input.Body.Password.Current, New: input.Body.Password.New}
	}
	member, sessionToken, err := routes.service.UpdateAccount(ctx, principal.MemberID, update)
	if err != nil {
		return nil, err
	}
	output := &sessionOutput{Body: NewMemberResponse(member)}
	if sessionToken != "" {
		output.SetCookie = SessionCookies(sessionToken, routes.service.configuration.SecureCookies())
	}
	return output, nil
}
