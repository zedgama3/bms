package identity

import (
	"context"
	_ "embed" // the OpenAPI fragment
	"net/http"
	"net/url"
	"strconv"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/model"
)

//go:embed openapi.json
var openAPIFragment []byte

// Routes returns the thirteen routes of the accounts set. Handlers are thin: parse, call the
// service, set the cookie, httpx.Fail on error. Cannot fail.
func (s *Service) Routes() []httpx.Route {
	return []httpx.Route{
		{Method: http.MethodGet, Pattern: "/session", Public: true, Handler: s.sessionHandler()},
		{Method: http.MethodPost, Pattern: "/sign-up", Public: true, Handler: s.signUpHandler()},
		{Method: http.MethodPost, Pattern: "/sign-in", Public: true, Handler: s.signInHandler()},
		{Method: http.MethodPost, Pattern: "/sign-out", Public: true, Handler: s.signOutHandler()},
		{Method: http.MethodPost, Pattern: "/password", Public: true, Handler: s.passwordHandler()},
		{Method: http.MethodPost, Pattern: "/password-reset", Public: true, Handler: s.resetRequestHandler()},
		{Method: http.MethodPost, Pattern: "/password-reset/complete", Public: true, Handler: s.resetCompleteHandler()},
		{Method: http.MethodPost, Pattern: "/sessions/end-others", Handler: s.endOthersHandler()},
		{Method: http.MethodGet, Pattern: "/", Handler: s.listHandler()},
		{Method: http.MethodGet, Pattern: "/{account}", Handler: s.getHandler()},
		{Method: http.MethodPost, Pattern: "/{account}/disable", Handler: s.accountHandler(s.DisableAccount)},
		{Method: http.MethodPost, Pattern: "/{account}/enable", Handler: s.accountHandler(s.EnableAccount)},
		{Method: http.MethodPost, Pattern: "/{account}/temporary-password", Handler: s.temporaryPasswordHandler()},
	}
}

// OpenAPI returns the embedded fragment for those routes. Cannot fail (a test proves it
// parses and names exactly the routes).
func (s *Service) OpenAPI() []byte { return append([]byte(nil), openAPIFragment...) }

// writeJSON writes v and, as every handler does for a failed write, leaves the transport
// error to the shell (it is already remembered on the span).
func writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	if err := httpx.WriteJSON(w, status, v); err != nil && !errs.Is(err, errs.Canceled) {
		httpx.Fail(w, r, err)
	}
}

func noStore(w http.ResponseWriter) { w.Header().Set("Cache-Control", "no-store") }

func (s *Service) sessionHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info, err := s.SessionInfo(r.Context())
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		noStore(w)
		writeJSON(w, r, http.StatusOK, info)
	})
}

// issuing wraps a use case that starts a session: it sets the cookie and answers status
// with no body, since the shell refreshes GET /session afterwards.
func (s *Service) issuing(status int, do func(r *http.Request) (Issued, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		iss, err := do(r)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		s.setCookie(w, r, iss)
		noStore(w)
		w.WriteHeader(status)
	})
}

func (s *Service) signUpHandler() http.Handler {
	return s.issuing(http.StatusCreated, func(r *http.Request) (Issued, error) {
		var c Credentials
		if err := httpx.ReadJSON(r, &c); err != nil {
			return Issued{}, err //nolint:wrapcheck // httpx's own kinds
		}
		return s.SignUp(r.Context(), c)
	})
}

func (s *Service) signInHandler() http.Handler {
	return s.issuing(http.StatusNoContent, func(r *http.Request) (Issued, error) {
		var c Credentials
		if err := httpx.ReadJSON(r, &c); err != nil {
			return Issued{}, err //nolint:wrapcheck // httpx's own kinds
		}
		return s.SignIn(r.Context(), c)
	})
}

func (s *Service) passwordHandler() http.Handler {
	return s.issuing(http.StatusNoContent, func(r *http.Request) (Issued, error) {
		var c PasswordChange
		if err := httpx.ReadJSON(r, &c); err != nil {
			return Issued{}, err //nolint:wrapcheck // httpx's own kinds
		}
		return s.ChangePassword(r.Context(), c)
	})
}

// resetRequestHandler answers 202 with no body for every address alike (identity.md,
// "Uniform answers"); a failure goes to the shell's mapper.
func (s *Service) resetRequestHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body ResetRequest
		if err := httpx.ReadJSON(r, &body); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		if err := s.RequestPasswordReset(r.Context(), body.Email); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		noStore(w)
		w.WriteHeader(http.StatusAccepted)
	})
}

// resetCompleteHandler answers 204. It sets no cookie: the person signs in afterwards
// (decision 20).
func (s *Service) resetCompleteHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body ResetCompletion
		if err := httpx.ReadJSON(r, &body); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		if err := s.CompletePasswordReset(r.Context(), body.Token, body.NewPassword); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		noStore(w)
		w.WriteHeader(http.StatusNoContent)
	})
}

func (s *Service) endOthersHandler() http.Handler {
	return s.issuing(http.StatusNoContent, func(r *http.Request) (Issued, error) {
		return s.EndOtherSessions(r.Context())
	})
}

// signOutHandler clears the cookie first, then records the logout (decision 10): a failed
// audit write still leaves the browser signed out, while the response is the audit error.
func (s *Service) signOutHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.clearCookie(w, r)
		noStore(w)
		if err := s.SignOut(r.Context()); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// accountID reads the {account} path value (F52).
func accountID(r *http.Request) (model.AccountID, error) {
	id, err := model.ParseID[model.AccountID](r.PathValue("account"))
	if err != nil {
		return model.AccountID{}, fieldInvalid("identity.accountID", "account", codeMalformed, "must be a UUID")
	}
	return id, nil
}

// parseListQuery reads ?email=&after=&limit=, reporting a bad number as field "limit".
func parseListQuery(vals url.Values) (AccountQuery, error) {
	q := AccountQuery{Email: model.Email(vals.Get("email")), After: Cursor(vals.Get("after"))}
	if raw := vals.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return AccountQuery{}, fieldInvalid("identity.parseListQuery", "limit", codeMalformed, "must be a whole number")
		}
		q.Limit = n
	}
	return q, nil
}

func (s *Service) listHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q, err := parseListQuery(r.URL.Query())
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		page, err := s.ListAccounts(r.Context(), q)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, page)
	})
}

func (s *Service) getHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := accountID(r)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		a, err := s.GetAccount(r.Context(), id)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, a)
	})
}

// accountHandler answers 204 for an admin change that returns nothing (disable, enable).
func (s *Service) accountHandler(do func(ctx context.Context, id model.AccountID) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := accountID(r)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		if err := do(r.Context(), id); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// temporaryPasswordBody is the body of POST /{account}/temporary-password.
type temporaryPasswordBody struct {
	Password string `json:"password"`
}

func (s *Service) temporaryPasswordHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := accountID(r)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		pw, err := s.SetTemporaryPassword(r.Context(), id)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		noStore(w)
		writeJSON(w, r, http.StatusOK, temporaryPasswordBody{Password: pw.Value()})
	})
}
