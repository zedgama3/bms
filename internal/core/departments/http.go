package departments

import (
	"context"
	_ "embed" // the OpenAPI fragment
	"net/http"
	"strconv"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/model"
)

//go:embed openapi.json
var openAPIFragment []byte

// Route is one HTTP route of the departments set; app converts it to module.Route
// (departments cannot import core/module). Every route is for signed-in people: the
// permission is checked inside the use case.
type Route struct {
	Method  string
	Pattern string // relative to /api/departments
	Handler http.Handler
}

// Routes returns the routes of the departments set. Handlers are thin: parse, call the
// service, httpx.Fail on error. Cannot fail.
func (s *Service) Routes() []Route {
	return []Route{
		{Method: http.MethodGet, Pattern: "/", Handler: s.listHandler()},
		{Method: http.MethodPost, Pattern: "/", Handler: s.createHandler()},
		{Method: http.MethodGet, Pattern: "/{department}", Handler: s.getHandler()},
		{Method: http.MethodPut, Pattern: "/{department}", Handler: s.updateHandler()},
		{Method: http.MethodPost, Pattern: "/{department}/archive", Handler: s.flipHandler(s.Archive)},
		{Method: http.MethodPost, Pattern: "/{department}/restore", Handler: s.flipHandler(s.Restore)},
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

// pathDepartment reads the {department} path value.
func pathDepartment(r *http.Request) (model.DepartmentID, error) {
	id, err := model.ParseID[model.DepartmentID](r.PathValue("department"))
	if err != nil {
		return model.DepartmentID{}, fieldInvalid("departments.pathDepartment", "department", codeMalformed, "must be a UUID")
	}
	return id, nil
}

func (s *Service) listHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q ListQuery
		if raw := r.URL.Query().Get("include_archived"); raw != "" {
			b, err := strconv.ParseBool(raw)
			if err != nil {
				httpx.Fail(w, r, fieldInvalid("departments.listHandler", "include_archived", codeMalformed, "must be true or false"))
				return
			}
			q.IncludeArchived = b
		}
		ds, err := s.List(r.Context(), q)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, struct {
			Departments []Department `json:"departments"`
		}{ds})
	})
}

func (s *Service) createHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body CreateInput
		if err := httpx.ReadJSON(r, &body); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		d, err := s.Create(r.Context(), body)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusCreated, d)
	})
}

func (s *Service) getHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := pathDepartment(r)
		var d Department
		if err == nil {
			d, err = s.Get(r.Context(), id)
		}
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, d)
	})
}

func (s *Service) updateHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := pathDepartment(r)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		var body UpdateInput
		if err := httpx.ReadJSON(r, &body); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		d, err := s.Update(r.Context(), id, body)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, d)
	})
}

// flipHandler serves POST /{department}/archive and /restore.
func (s *Service) flipHandler(do func(ctx context.Context, id model.DepartmentID) (Department, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := pathDepartment(r)
		var d Department
		if err == nil {
			d, err = do(r.Context(), id)
		}
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, d)
	})
}
