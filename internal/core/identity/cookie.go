package identity

import (
	"context"
	"net/http"
	"time"

	"github.com/zedgama3/bms/internal/lib/httpx"
)

// cookieName is the cookie a response from this request's scheme may set: the __Host-
// cookie, or the dev cookie when AllowPlaintext is set and the effective scheme is http
// (api decision 24). Never both on one response. secure is false only for the dev cookie.
func (s *Service) cookieName(ctx context.Context) (name string, secure bool) {
	if scheme, _ := httpx.SchemeFrom(ctx); s.allowPlaintext && scheme == "http" {
		return DevCookieName, false
	}
	return CookieName, true
}

// readToken returns the session token the request carries: the __Host- cookie, else the
// dev cookie when AllowPlaintext is set and the effective scheme is http (the stricter
// reading: the dev cookie is neither read nor written anywhere else). "" means none.
func (s *Service) readToken(ctx context.Context, r *http.Request) string {
	if c, err := r.Cookie(CookieName); err == nil && c.Value != "" {
		return c.Value
	}
	if name, _ := s.cookieName(ctx); name == DevCookieName {
		if c, err := r.Cookie(DevCookieName); err == nil && c.Value != "" {
			return c.Value
		}
	}
	return ""
}

// setCookie writes the session cookie for iss: Path=/, HttpOnly, SameSite=Lax, Secure
// (except the dev cookie), Max-Age = the lifetime.
func (s *Service) setCookie(w http.ResponseWriter, r *http.Request, iss Issued) {
	name, secure := s.cookieName(r.Context())
	maxAge := int(iss.Session.ExpiresAt.Sub(iss.Session.IssuedAt) / time.Second)
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure is false only for the dev cookie (api decision 24)
		Name: name, Value: iss.token, Path: "/", MaxAge: maxAge,
		Secure: secure, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
}

// clearCookie expires the session cookie: the same name and attributes with Max-Age=0.
func (s *Service) clearCookie(w http.ResponseWriter, r *http.Request) {
	name, secure := s.cookieName(r.Context())
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure is false only for the dev cookie (api decision 24)
		Name: name, Value: "", Path: "/", MaxAge: -1,
		Secure: secure, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
}
