package demo

import (
	_ "embed" // the OpenAPI fragment
	"net/http"
	"strconv"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/lib/validate"
)

//go:embed openapi.json
var openAPIFragment []byte

// Route is one HTTP route of the demo set; app converts it to module.Route (demo cannot
// import core/module). Public routes need no session; the others need one and nothing else.
type Route struct {
	Method  string
	Pattern string // relative to /api/demo
	Public  bool
	Handler http.Handler
}

// Routes returns GET / (public: the banner data) and GET /mail (signed-in: the viewer's own
// captured mail). Cannot fail.
func (s *Service) Routes() []Route {
	return []Route{
		{Method: http.MethodGet, Pattern: "/", Public: true, Handler: s.infoHandler()},
		{Method: http.MethodGet, Pattern: "/mail", Handler: s.mailHandler()},
	}
}

// OpenAPI returns the embedded fragment for those routes. Cannot fail.
func (*Service) OpenAPI() []byte { return append([]byte(nil), openAPIFragment...) }

func writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	if err := httpx.WriteJSON(w, status, v); err != nil && !errs.Is(err, errs.Canceled) {
		httpx.Fail(w, r, err)
	}
}

func (s *Service) infoHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, r, http.StatusOK, s.Info())
	})
}

func (s *Service) mailHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q MailQuery
		if raw := r.URL.Query().Get("limit"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil {
				httpx.Fail(w, r, errs.E("demo.mailHandler", errs.Invalid, &validate.FieldError{
					Field: "limit", Code: validate.CodeRange, Reason: "must be a whole number",
				}))
				return
			}
			q.Limit = n
		}
		mails, err := s.ListMail(r.Context(), q)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, r, http.StatusOK, struct {
			Mail []Mail `json:"mail"`
		}{mails})
	})
}
