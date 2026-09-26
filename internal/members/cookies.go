package members

import (
	"net/http"
	"time"

	"github.com/preburn/preburn/internal/secrets"
)

const (
	// SessionCookieName is the name of the cookie that holds the session
	// token.
	SessionCookieName = "preburn_session"

	csrfCookieName      = "preburn_csrf"
	csrfHeader          = "X-CSRF-Token"
	cookiePath          = "/"
	sessionCookieMaxAge = int(sessionLifetime / time.Second)
	expiredCookieMaxAge = -1
)

// SessionCookies returns the cookies that sign a browser in with the session
// token: preburn_session holding token, HttpOnly, and preburn_csrf holding a
// new random token that the dashboard reads and sends back in the
// X-CSRF-Token header. Both have path /, SameSite=Lax and a Max-Age of the 30
// day session lifetime, and are Secure when secure is true, which is when
// PREBURN_PUBLIC_URL is https.
func SessionCookies(token string, secure bool) []http.Cookie {
	return []http.Cookie{
		sessionCookie(token, sessionCookieMaxAge, secure),
		csrfCookie(secrets.NewToken(), sessionCookieMaxAge, secure),
	}
}

// ExpiredSessionCookies returns the cookies that remove preburn_session and
// preburn_csrf from the browser.
func ExpiredSessionCookies(secure bool) []http.Cookie {
	return []http.Cookie{
		sessionCookie("", expiredCookieMaxAge, secure),
		csrfCookie("", expiredCookieMaxAge, secure),
	}
}

func sessionCookie(token string, maxAge int, secure bool) http.Cookie {
	return http.Cookie{ //nolint:gosec // G124: Secure follows PREBURN_PUBLIC_URL, so plain http installs keep their sessions.
		Name:     SessionCookieName,
		Value:    token,
		Path:     cookiePath,
		MaxAge:   maxAge,
		Secure:   secure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
}

func csrfCookie(token string, maxAge int, secure bool) http.Cookie {
	return http.Cookie{ //nolint:gosec // G124: the dashboard reads this cookie, and Secure follows PREBURN_PUBLIC_URL.
		Name:     csrfCookieName,
		Value:    token,
		Path:     cookiePath,
		MaxAge:   maxAge,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
}
