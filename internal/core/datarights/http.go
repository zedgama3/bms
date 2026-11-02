package datarights

import (
	_ "embed" // the OpenAPI fragment
	"net/http"
	"strconv"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/model"
)

//go:embed openapi.json
var openAPIFragment []byte

// Routes returns the routes of the datarights set, relative to /api/datarights (people.md,
// "Routes", moved here by boundaries step 12). Handlers are thin: parse, call the service,
// httpx.Fail on error. Cannot fail.
func (s *Service) Routes() []httpx.Route {
	return []httpx.Route{
		{Method: http.MethodGet, Pattern: "/me/export", Handler: s.exportHandler(s.self)},
		{Method: http.MethodGet, Pattern: "/me/requests", Handler: s.myRequestsHandler()},
		{Method: http.MethodPost, Pattern: "/me/requests", Handler: s.requestDeletionHandler()},
		{Method: http.MethodPost, Pattern: "/me/requests/{request}/withdraw", Handler: s.withdrawHandler()},
		{Method: http.MethodGet, Pattern: "/requests", Handler: s.listRequestsHandler()},
		{Method: http.MethodPost, Pattern: "/requests/{request}/decline", Handler: s.declineHandler()},
		{Method: http.MethodPost, Pattern: "/requests/{request}/fulfil", Handler: s.fulfilHandler()},
		{Method: http.MethodGet, Pattern: "/people/{person}/export", Handler: s.exportHandler(pathPerson)},
		{Method: http.MethodDelete, Pattern: "/people/{person}", Handler: s.deleteHandler()},
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

// personSource says which person a route is about: the signed-in one (/me) or the path's.
type personSource func(r *http.Request) (model.PersonID, error)

// self is the signed-in person.
func (s *Service) self(r *http.Request) (model.PersonID, error) {
	a, err := s.actorPerson(r.Context(), "datarights.self")
	if err != nil {
		return model.PersonID{}, err
	}
	return a.Person, nil
}

// pathID reads the path value name as a typed ID.
func pathID[T ~[16]byte](r *http.Request, name string) (T, error) {
	id, err := model.ParseID[T](r.PathValue(name))
	if err != nil {
		return T{}, fieldInvalid("datarights.pathID", name, codeMalformed, "must be a UUID")
	}
	return id, nil
}

func pathPerson(r *http.Request) (model.PersonID, error) { return pathID[model.PersonID](r, "person") }

// handle wraps a handler body: it resolves the person, runs do and answers do's error.
func handle(src personSource, do func(w http.ResponseWriter, r *http.Request, person model.PersonID) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		person, err := src(r)
		if err == nil {
			err = do(w, r, person)
		}
		if err != nil {
			httpx.Fail(w, r, err)
		}
	})
}

// exportHandler serves the export as an attachment named after the person.
func (s *Service) exportHandler(src personSource) http.Handler {
	return handle(src, func(w http.ResponseWriter, r *http.Request, person model.PersonID) error {
		doc, err := s.Export(r.Context(), person)
		if err != nil {
			return err
		}
		h := w.Header()
		h.Set("Content-Type", "application/json")
		h.Set("Content-Disposition", `attachment; filename="bms-export-`+person.String()+`.json"`)
		h.Set("Content-Length", strconv.Itoa(len(doc)))
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(doc); err != nil {
			return nil //nolint:nilerr // the client went away; the shell remembers the write error on the span
		}
		return nil
	})
}

func (s *Service) myRequestsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rs, err := s.MyRequests(r.Context())
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, wireRequests(rs))
	})
}

func wireRequests(rs []Request) struct {
	Requests []wireRequest `json:"requests"`
} {
	out := make([]wireRequest, len(rs))
	for i, r := range rs {
		out[i] = wireRequestOf(r)
	}
	return struct {
		Requests []wireRequest `json:"requests"`
	}{out}
}

func (s *Service) requestDeletionHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body wireRequestInput
		if err := httpx.ReadJSON(r, &body); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		if err := requestKind("datarights.requestDeletionHandler", body.Kind); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		req, err := s.RequestDeletion(r.Context(), body.Note)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusCreated, wireRequestOf(req))
	})
}

func (s *Service) withdrawHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID[RequestID](r, "request")
		if err == nil {
			err = s.WithdrawRequest(r.Context(), id)
		}
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func (s *Service) deleteHandler() http.Handler {
	return handle(pathPerson, func(w http.ResponseWriter, r *http.Request, person model.PersonID) error {
		rep, err := s.Delete(r.Context(), person)
		if err != nil {
			return err
		}
		writeJSON(w, r, http.StatusOK, wireReportOf(rep))
		return nil
	})
}

func (s *Service) listRequestsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var (
			q   RequestQuery
			err error
		)
		vals := r.URL.Query()
		if raw := vals.Get("status"); raw != "" {
			if q.Status, err = ParseRequestStatus(raw); err != nil {
				httpx.Fail(w, r, err)
				return
			}
		}
		if raw := vals.Get("person"); raw != "" {
			if q.Person, err = model.ParseID[model.PersonID](raw); err != nil {
				httpx.Fail(w, r, fieldInvalid("datarights.listRequestsHandler", "person", codeMalformed, "must be a UUID"))
				return
			}
		}
		rs, err := s.ListRequests(r.Context(), q)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, wireRequests(rs))
	})
}

func (s *Service) declineHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID[RequestID](r, "request")
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		var body wireDecisionInput
		if err := httpx.ReadJSON(r, &body); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		req, err := s.DeclineRequest(r.Context(), id, body.Note)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, wireRequestOf(req))
	})
}

func (s *Service) fulfilHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID[RequestID](r, "request")
		var rep DeletionReport
		if err == nil {
			rep, err = s.FulfilRequest(r.Context(), id)
		}
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, wireReportOf(rep))
	})
}
