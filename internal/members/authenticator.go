package members

import (
	"context"
	"crypto/subtle"
	"net/http"

	"github.com/preburn/preburn/internal/httpapi"
)

// SessionAuthenticator is the httpapi.Authenticator of member sessions. It
// admits the admin and dashboard route groups. Create one with
// NewSessionAuthenticator.
type SessionAuthenticator struct {
	sessions *SessionStore
}

// NewSessionAuthenticator returns a SessionAuthenticator that looks sessions
// up in sessions.
func NewSessionAuthenticator(sessions *SessionStore) *SessionAuthenticator {
	return &SessionAuthenticator{sessions: sessions}
}

// Authenticate returns the httpapi.MemberPrincipal of the session in the
// preburn_session cookie of request. It checks, in order:
//
//   - the cookie names a live session of an active member, or
//     httpapi.ErrAuthenticationRequired
//   - group is not httpapi.RouteGroupRuntime, or httpapi.ErrScopeForbidden
//   - on methods other than GET, HEAD and OPTIONS, the X-CSRF-Token header
//     equals the preburn_csrf cookie, compared in constant time, or
//     httpapi.ErrCSRFInvalid
//   - the X-Preburn-Environment header names an environment, or
//     httpapi.ErrEnvironmentHeaderInvalid
func (authenticator *SessionAuthenticator) Authenticate(ctx context.Context, request *http.Request, group httpapi.RouteGroup) (httpapi.Principal, error) {
	cookie, err := request.Cookie(SessionCookieName)
	if err != nil {
		return nil, httpapi.ErrAuthenticationRequired
	}
	member, err := authenticator.sessions.Authenticate(ctx, cookie.Value)
	if err != nil {
		return nil, err
	}
	if group == httpapi.RouteGroupRuntime {
		return nil, httpapi.ErrScopeForbidden
	}
	if !isSafeMethod(request.Method) && !hasValidCSRFToken(request) {
		return nil, httpapi.ErrCSRFInvalid
	}
	environment, err := httpapi.EnvironmentFromHeader(request.Header)
	if err != nil {
		return nil, err
	}
	return httpapi.MemberPrincipal{MemberID: member.ID, Environment: environment}, nil
}

// CheckSession returns httpapi.ErrAuthenticationRequired when request no
// longer presents a live session of an active member in its preburn_session
// cookie. It checks the session with SessionStore.Check, so it never extends
// the session.
func (authenticator *SessionAuthenticator) CheckSession(ctx context.Context, request *http.Request) error {
	cookie, err := request.Cookie(SessionCookieName)
	if err != nil {
		return httpapi.ErrAuthenticationRequired
	}
	return authenticator.sessions.Check(ctx, cookie.Value)
}

func isSafeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

func hasValidCSRFToken(request *http.Request) bool {
	cookie, err := request.Cookie(csrfCookieName)
	header := request.Header.Get(csrfHeader)
	return err == nil && header != "" && subtle.ConstantTimeCompare([]byte(header), []byte(cookie.Value)) == 1
}
