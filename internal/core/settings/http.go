package settings

import (
	_ "embed" // the OpenAPI fragment
	"net/http"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
)

//go:embed openapi.json
var openAPIFragment []byte

// Routes returns the routes of the settings set, relative to /api/settings: GET /, PUT
// /values, GET /terms and PUT /terms/{lang}. The permission of each is checked inside the
// use case; the public document, the stylesheet and the logo are regional's routes.
// Handlers are thin: parse, call the service, httpx.Fail on error. Cannot fail.
func (s *Service) Routes() []httpx.Route {
	return []httpx.Route{
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
