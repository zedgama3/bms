package notify

import (
	_ "embed" // the OpenAPI fragment
	"net/http"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/model"
)

//go:embed openapi.json
var openAPIFragment []byte

// Routes returns the routes of the notify set, relative to /api/notify. Every route needs a
// signed-in person; the permission is checked inside the use case. Handlers are thin: parse,
// call the service, httpx.Fail on error. Cannot fail.
func (s *Service) Routes() []httpx.Route {
	return []httpx.Route{
		{Method: http.MethodGet, Pattern: "/templates", Handler: s.listTemplatesHandler()},
		{Method: http.MethodGet, Pattern: "/templates/{name}/{lang}", Handler: s.getTemplateHandler()},
		{Method: http.MethodPut, Pattern: "/templates/{name}/{lang}", Handler: s.putTemplateHandler()},
		{Method: http.MethodDelete, Pattern: "/templates/{name}/{lang}", Handler: s.resetTemplateHandler()},
		{Method: http.MethodPost, Pattern: "/templates/{name}/{lang}/preview", Handler: s.previewHandler()},
		{Method: http.MethodPost, Pattern: "/test", Handler: s.testHandler()},
		{Method: http.MethodGet, Pattern: "/outbox", Handler: s.outboxHandler()},
		{Method: http.MethodPost, Pattern: "/outbox/check", Handler: s.checkHandler()},
		{Method: http.MethodPost, Pattern: "/outbox/{message}/retry", Handler: s.retryHandler()},
		{Method: http.MethodPost, Pattern: "/outbox/{message}/cancel", Handler: s.cancelHandler()},
	}
}

// OpenAPI returns the embedded fragment for those routes. Cannot fail.
func (*Service) OpenAPI() []byte { return append([]byte(nil), openAPIFragment...) }

func writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	if err := httpx.WriteJSON(w, status, v); err != nil && !errs.Is(err, errs.Canceled) {
		httpx.Fail(w, r, err)
	}
}

func noContent(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) }

// templateKey reads {name} and {lang} from the path.
func templateKey(r *http.Request) (Template, Language) {
	return Template(r.PathValue("name")), Language(r.PathValue("lang"))
}

func (s *Service) listTemplatesHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		out, err := s.ListTemplates(r.Context())
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, out)
	})
}

func (s *Service) getTemplateHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name, lang := templateKey(r)
		out, err := s.GetTemplate(r.Context(), name, lang)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, out)
	})
}

func (s *Service) putTemplateHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body TemplateText
		if err := httpx.ReadJSON(r, &body); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		name, lang := templateKey(r)
		out, err := s.PutTemplate(r.Context(), name, lang, body)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, out)
	})
}

func (s *Service) resetTemplateHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name, lang := templateKey(r)
		if err := s.ResetTemplate(r.Context(), name, lang); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		noContent(w)
	})
}

func (s *Service) previewHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body TemplateText
		if err := httpx.ReadJSON(r, &body); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		name, lang := templateKey(r)
		out, err := s.Preview(r.Context(), name, lang, body)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, out)
	})
}

func (s *Service) testHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := s.SendTest(r.Context())
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusAccepted, struct {
			ID MessageID `json:"id"`
		}{id})
	})
}

func (s *Service) outboxHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		out, err := s.Outbox(r.Context())
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, out)
	})
}

func (s *Service) checkHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := s.CheckConnection(r.Context()); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		noContent(w)
	})
}

// messageID reads {message} from the path; a malformed ID is NotFound-shaped Invalid.
func messageID(r *http.Request) (MessageID, error) {
	id, err := model.ParseID[MessageID](r.PathValue("message"))
	if err != nil {
		return MessageID{}, err //nolint:wrapcheck // ParseID returns errs.Invalid
	}
	return id, nil
}

func (s *Service) retryHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := messageID(r)
		if err == nil {
			err = s.Retry(r.Context(), id)
		}
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		noContent(w)
	})
}

func (s *Service) cancelHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := messageID(r)
		if err == nil {
			err = s.Cancel(r.Context(), id)
		}
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		noContent(w)
	})
}
