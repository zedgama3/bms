package settings

import (
	_ "embed" // the OpenAPI fragment
	"net/http"
	"strconv"
	"strings"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
)

//go:embed openapi.json
var openAPIFragment []byte

// setCaching is the one place the caching of GET /public and GET /theme.css is decided: a
// strong ETag derived from the revision, so a write moves it and a changed document never
// matches. No handler sets Cache-Control: the /api/ no-store rule stays on both routes
// (settings.md amendment 2026-10-19), and a 304 to a matching If-None-Match saves the body.
func setCaching(h http.Header, revision int64) {
	h.Set("ETag", etagOf(revision))
}

// etagOf is the entity tag of the public documents at a revision.
func etagOf(revision int64) string { return `"settings-` + strconv.FormatInt(revision, 10) + `"` }

// notModified reports whether the request's If-None-Match names the entity tag of revision
// (a list of tags, weak ones, or "*").
func notModified(r *http.Request, revision int64) bool {
	want := etagOf(revision)
	for _, field := range r.Header.Values("If-None-Match") {
		for _, tag := range strings.Split(field, ",") {
			tag = strings.TrimPrefix(strings.TrimSpace(tag), "W/")
			if tag == "*" || tag == want {
				return true
			}
		}
	}
	return false
}

// Routes returns the routes of the settings set, relative to /api/settings. The first two
// are public (no session); the permission of the rest is checked inside the use case.
// Handlers are thin: parse, call the service, httpx.Fail on error. Cannot fail.
func (s *Service) Routes() []Route {
	return []Route{
		{Method: http.MethodGet, Pattern: "/public", Public: true, Handler: s.publicHandler()},
		{Method: http.MethodGet, Pattern: "/theme.css", Public: true, Handler: s.themeHandler()},
		{Method: http.MethodGet, Pattern: "/", Handler: s.documentHandler()},
		{Method: http.MethodPut, Pattern: "/values", Handler: s.setHandler()},
		{Method: http.MethodGet, Pattern: "/terms", Handler: s.termsHandler()},
		{Method: http.MethodPut, Pattern: "/terms/{lang}", Handler: s.setTermsHandler()},
	}
}

// OpenAPI returns the embedded fragment for those routes. Cannot fail (a test proves it
// parses and names exactly the routes).
func (*Service) OpenAPI() []byte { return append([]byte(nil), openAPIFragment...) }

// writeJSON writes v and leaves a transport error to the shell (it is already remembered
// on the span).
func writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	if err := httpx.WriteJSON(w, status, v); err != nil && !errs.Is(err, errs.Canceled) {
		httpx.Fail(w, r, err)
	}
}

func (s *Service) publicHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, rev, err := s.publicAt(r.Context())
		if err != nil {
			httpx.Fail(w, r, wrap("settings.publicHandler", err))
			return
		}
		setCaching(w.Header(), rev)
		if notModified(r, rev) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		writeJSON(w, r, http.StatusOK, p)
	})
}

func (s *Service) themeHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		css, rev, err := s.themeAt(r.Context())
		if err != nil {
			httpx.Fail(w, r, wrap("settings.themeHandler", err))
			return
		}
		setCaching(w.Header(), rev)
		if notModified(r, rev) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		h := w.Header()
		h.Set("Content-Type", "text/css; charset=utf-8")
		h.Set("Content-Length", strconv.Itoa(len(css)))
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(css)); err != nil {
			return // the client is gone; the shell remembers the write error
		}
	})
}

func (s *Service) documentHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		doc, err := s.Document(r.Context())
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, doc)
	})
}

// revisionBody is what a successful write answers.
type revisionBody struct {
	Revision Revision `json:"revision"`
}

func (s *Service) setHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body Changes
		if err := httpx.ReadJSON(r, &body); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		rev, err := s.Set(r.Context(), body)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, revisionBody{rev})
	})
}

func (s *Service) termsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		doc, err := s.Terms(r.Context())
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, doc)
	})
}

func (s *Service) setTermsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body TermChanges
		if err := httpx.ReadJSON(r, &body); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		rev, err := s.SetTerms(r.Context(), r.PathValue("lang"), body)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, revisionBody{rev})
	})
}
