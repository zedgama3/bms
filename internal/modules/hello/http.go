package hello

import (
	"net/http"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
)

// The handlers are thin: call the service, write JSON, and on error hand it to
// httpx.Fail. None reads the body, parses a path ID, starts a goroutine or changes state
// on GET.

func (m *Module) publicGreeting() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		out, err := m.svc.PublicGreeting(r.Context())
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		if err := httpx.WriteJSON(w, http.StatusOK, out); err != nil {
			failUnlessGone(w, r, err)
		}
	})
}

func (m *Module) greeting() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		out, err := m.svc.Greeting(r.Context())
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		if err := httpx.WriteJSON(w, http.StatusOK, out); err != nil {
			failUnlessGone(w, r, err)
		}
	})
}

func (m *Module) greet() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		out, err := m.svc.Greet(r.Context())
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		if err := httpx.WriteJSON(w, http.StatusCreated, out); err != nil {
			failUnlessGone(w, r, err)
		}
	})
}

// failUnlessGone answers a WriteJSON failure: errs.Canceled means the client is gone (the
// shell already recorded it), and any other kind (an encoding bug, errs.Internal) is
// answered with httpx.Fail, which WriteJSON guarantees came before any byte was written.
func failUnlessGone(w http.ResponseWriter, r *http.Request, err error) {
	if errs.Is(err, errs.Canceled) {
		return
	}
	httpx.Fail(w, r, err)
}
