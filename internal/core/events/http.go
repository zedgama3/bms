package events

import (
	"context"
	_ "embed" // the OpenAPI fragment
	"net/http"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/model"
)

//go:embed openapi.json
var openAPIFragment []byte

// Routes returns the routes of the events set. Handlers are thin: parse, call the service,
// httpx.Fail on error. Cannot fail.
func (s *Service) Routes() []httpx.Route {
	return []httpx.Route{
		{Method: http.MethodGet, Pattern: "/", Handler: s.listHandler()},
		{Method: http.MethodPost, Pattern: "/", Handler: s.createHandler()},
		{Method: http.MethodGet, Pattern: "/kinds", Handler: s.listKindsHandler()},
		{Method: http.MethodPost, Pattern: "/kinds", Handler: s.createKindHandler()},
		{Method: http.MethodPut, Pattern: "/kinds/{kind}", Handler: s.updateKindHandler()},
		{Method: http.MethodGet, Pattern: "/{event}", Handler: s.getHandler()},
		{Method: http.MethodPut, Pattern: "/{event}", Handler: s.updateHandler()},
		{Method: http.MethodPost, Pattern: "/{event}/close", Handler: s.flipHandler(s.Close)},
		{Method: http.MethodPost, Pattern: "/{event}/reopen", Handler: s.flipHandler(s.Reopen)},
		{Method: http.MethodPost, Pattern: "/{event}/setup", Handler: s.setupHandler()},
		{Method: http.MethodPost, Pattern: "/{event}/schedule", Handler: s.addItemHandler()},
		{Method: http.MethodPut, Pattern: "/{event}/schedule/{item}", Handler: s.updateItemHandler()},
		{Method: http.MethodDelete, Pattern: "/{event}/schedule/{item}", Handler: s.removeItemHandler()},
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

// pathID reads the path value name as a typed ID.
func pathID[T ~[16]byte](r *http.Request, name string) (T, error) {
	id, err := model.ParseID[T](r.PathValue(name))
	if err != nil {
		return T{}, fieldInvalid("events.pathID", name, codeMalformed, "must be a UUID")
	}
	return id, nil
}

// do wraps a handler body: it reads the {event} path value, runs fn and answers fn's error.
func do(fn func(w http.ResponseWriter, r *http.Request, event model.EventID) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID[model.EventID](r, "event")
		if err == nil {
			err = fn(w, r, id)
		}
		if err != nil {
			httpx.Fail(w, r, err)
		}
	})
}

func (s *Service) listHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q ListQuery
		if raw := r.URL.Query().Get("status"); raw != "" {
			st, err := ParseStatus(raw)
			if err != nil {
				httpx.Fail(w, r, err)
				return
			}
			q.Status = st
		}
		es, err := s.List(r.Context(), q)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, struct {
			Events []Event `json:"events"`
		}{es})
	})
}

func (s *Service) createHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body CreateInput
		if err := httpx.ReadJSON(r, &body); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		c, err := s.Create(r.Context(), body)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusCreated, c)
	})
}

func (s *Service) getHandler() http.Handler {
	return do(func(w http.ResponseWriter, r *http.Request, id model.EventID) error {
		d, err := s.Get(r.Context(), id)
		if err != nil {
			return err
		}
		writeJSON(w, r, http.StatusOK, d)
		return nil
	})
}

func (s *Service) updateHandler() http.Handler {
	return do(func(w http.ResponseWriter, r *http.Request, id model.EventID) error {
		var body UpdateInput
		if err := httpx.ReadJSON(r, &body); err != nil {
			return err //nolint:wrapcheck // httpx's own kinds
		}
		e, err := s.Update(r.Context(), id, body)
		if err != nil {
			return err
		}
		writeJSON(w, r, http.StatusOK, e)
		return nil
	})
}

// flipHandler serves POST /{event}/close and /reopen.
func (s *Service) flipHandler(fn func(ctx context.Context, id model.EventID) (Event, error)) http.Handler {
	return do(func(w http.ResponseWriter, r *http.Request, id model.EventID) error {
		e, err := fn(r.Context(), id)
		if err != nil {
			return err
		}
		writeJSON(w, r, http.StatusOK, e)
		return nil
	})
}

// setupBody is the body of POST /{event}/setup.
type setupBody struct {
	CopyFrom model.EventID `json:"copy_from"`
}

func (s *Service) setupHandler() http.Handler {
	return do(func(w http.ResponseWriter, r *http.Request, id model.EventID) error {
		var body setupBody
		if err := httpx.ReadJSON(r, &body); err != nil {
			return err //nolint:wrapcheck // httpx's own kinds
		}
		res, err := s.RunSetup(r.Context(), id, body.CopyFrom)
		if err != nil {
			return err
		}
		writeJSON(w, r, http.StatusOK, struct {
			Setup []SetupResult `json:"setup"`
		}{res})
		return nil
	})
}

func (s *Service) listKindsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ks, err := s.ListKinds(r.Context())
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, struct {
			Kinds []Kind `json:"kinds"`
		}{ks})
	})
}

func (s *Service) createKindHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body KindInput
		if err := httpx.ReadJSON(r, &body); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		k, err := s.CreateKind(r.Context(), body)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusCreated, k)
	})
}

func (s *Service) updateKindHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body KindInput
		if err := httpx.ReadJSON(r, &body); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		k, err := s.UpdateKind(r.Context(), r.PathValue("kind"), body)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, k)
	})
}

func (s *Service) addItemHandler() http.Handler {
	return do(func(w http.ResponseWriter, r *http.Request, id model.EventID) error {
		var body ScheduleInput
		if err := httpx.ReadJSON(r, &body); err != nil {
			return err //nolint:wrapcheck // httpx's own kinds
		}
		it, err := s.AddScheduleItem(r.Context(), id, body)
		if err != nil {
			return err
		}
		writeJSON(w, r, http.StatusCreated, it)
		return nil
	})
}

func (s *Service) updateItemHandler() http.Handler {
	return do(func(w http.ResponseWriter, r *http.Request, id model.EventID) error {
		item, err := pathID[ScheduleItemID](r, "item")
		if err != nil {
			return err
		}
		var body ScheduleInput
		if err := httpx.ReadJSON(r, &body); err != nil {
			return err //nolint:wrapcheck // httpx's own kinds
		}
		it, err := s.UpdateScheduleItem(r.Context(), id, item, body)
		if err != nil {
			return err
		}
		writeJSON(w, r, http.StatusOK, it)
		return nil
	})
}

func (s *Service) removeItemHandler() http.Handler {
	return do(func(w http.ResponseWriter, r *http.Request, id model.EventID) error {
		item, err := pathID[ScheduleItemID](r, "item")
		if err != nil {
			return err
		}
		if err := s.RemoveScheduleItem(r.Context(), id, item); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	})
}
