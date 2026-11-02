package people

import (
	_ "embed" // the OpenAPI fragment
	"net/http"
	"strconv"
	"strings"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/model"
)

//go:embed openapi.json
var openAPIFragment []byte

// Routes returns the routes of the people set (people.md, "Routes"). Handlers are thin:
// parse, call the service, httpx.Fail on error. Cannot fail.
func (s *Service) Routes() []httpx.Route {
	return []httpx.Route{
		{Method: http.MethodGet, Pattern: "/me", Handler: s.meHandler()},
		{Method: http.MethodPut, Pattern: "/me", Handler: s.updateHandler(s.self)},
		{Method: http.MethodPost, Pattern: "/me/contacts", Handler: s.addContactHandler(s.self)},
		{Method: http.MethodPut, Pattern: "/me/contacts/{contact}", Handler: s.updateContactHandler(s.self)},
		{Method: http.MethodDelete, Pattern: "/me/contacts/{contact}", Handler: s.removeContactHandler(s.self)},
		{Method: http.MethodPut, Pattern: "/me/consents/{purpose}", Handler: s.consentHandler(s.self)},
		{Method: http.MethodGet, Pattern: "/search", Handler: s.searchHandler()},
		{Method: http.MethodGet, Pattern: "/names", Handler: s.namesHandler()},
		{Method: http.MethodPost, Pattern: "/", Handler: s.createHandler()},
		{Method: http.MethodGet, Pattern: "/{person}", Handler: s.getHandler()},
		{Method: http.MethodPut, Pattern: "/{person}", Handler: s.updateHandler(pathPerson)},
		{Method: http.MethodPost, Pattern: "/{person}/contacts", Handler: s.addContactHandler(pathPerson)},
		{Method: http.MethodPut, Pattern: "/{person}/contacts/{contact}", Handler: s.updateContactHandler(pathPerson)},
		{Method: http.MethodDelete, Pattern: "/{person}/contacts/{contact}", Handler: s.removeContactHandler(pathPerson)},
		{Method: http.MethodPut, Pattern: "/{person}/consents/{purpose}", Handler: s.consentHandler(pathPerson)},
		{Method: http.MethodPost, Pattern: "/{person}/link-account", Handler: s.linkHandler()},
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
	a, err := s.actorPerson(r.Context(), "people.self")
	if err != nil {
		return model.PersonID{}, err
	}
	return a.Person, nil
}

// pathID reads the path value name as a typed ID.
func pathID[T ~[16]byte](r *http.Request, name string) (T, error) {
	id, err := model.ParseID[T](r.PathValue(name))
	if err != nil {
		return T{}, fieldInvalid("people.pathID", name, codeMalformed, "must be a UUID")
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

func (s *Service) meHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := s.Me(r.Context())
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, wireProfileOf(p))
	})
}

func (s *Service) getHandler() http.Handler {
	return handle(pathPerson, func(w http.ResponseWriter, r *http.Request, person model.PersonID) error {
		p, err := s.Get(r.Context(), person)
		if err != nil {
			return err
		}
		writeJSON(w, r, http.StatusOK, wireProfileOf(p))
		return nil
	})
}

func (in wireProfileInput) input() ProfileInput { return ProfileInput(in) }

// updateHandler serves PUT /me and PUT /{person}: Update treats your own ID as UpdateMe.
func (s *Service) updateHandler(src personSource) http.Handler {
	return handle(src, func(w http.ResponseWriter, r *http.Request, person model.PersonID) error {
		var body wireProfileInput
		if err := httpx.ReadJSON(r, &body); err != nil {
			return err //nolint:wrapcheck // httpx's own kinds
		}
		p, err := s.Update(r.Context(), person, body.input())
		if err != nil {
			return err
		}
		writeJSON(w, r, http.StatusOK, wireProfileOf(p))
		return nil
	})
}

func (s *Service) createHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body wireProfileInput
		if err := httpx.ReadJSON(r, &body); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		p, err := s.Create(r.Context(), body.input())
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusCreated, wireProfileOf(p))
	})
}

func (s *Service) addContactHandler(src personSource) http.Handler {
	return handle(src, func(w http.ResponseWriter, r *http.Request, person model.PersonID) error {
		var body wireContactInput
		if err := httpx.ReadJSON(r, &body); err != nil {
			return err //nolint:wrapcheck // httpx's own kinds
		}
		c, err := s.AddContact(r.Context(), person, ContactInput(body))
		if err != nil {
			return err
		}
		writeJSON(w, r, http.StatusCreated, wireContactOf(c))
		return nil
	})
}

func (s *Service) updateContactHandler(src personSource) http.Handler {
	return handle(src, func(w http.ResponseWriter, r *http.Request, person model.PersonID) error {
		id, err := pathID[ContactID](r, "contact")
		if err != nil {
			return err
		}
		var body wireContactInput
		if err := httpx.ReadJSON(r, &body); err != nil {
			return err //nolint:wrapcheck // httpx's own kinds
		}
		c, err := s.UpdateContact(r.Context(), person, id, ContactInput(body))
		if err != nil {
			return err
		}
		writeJSON(w, r, http.StatusOK, wireContactOf(c))
		return nil
	})
}

func (s *Service) removeContactHandler(src personSource) http.Handler {
	return handle(src, func(w http.ResponseWriter, r *http.Request, person model.PersonID) error {
		id, err := pathID[ContactID](r, "contact")
		if err != nil {
			return err
		}
		if err := s.RemoveContact(r.Context(), person, id); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	})
}

func (s *Service) consentHandler(src personSource) http.Handler {
	return handle(src, func(w http.ResponseWriter, r *http.Request, person model.PersonID) error {
		var body wireConsentInput
		if err := httpx.ReadJSON(r, &body); err != nil {
			return err //nolint:wrapcheck // httpx's own kinds
		}
		c, err := s.SetConsent(r.Context(), person, Purpose(r.PathValue("purpose")), body.Granted)
		if err != nil {
			return err
		}
		writeJSON(w, r, http.StatusOK, wireConsentOf(c))
		return nil
	})
}

func (s *Service) searchHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := SearchQuery{Q: r.URL.Query().Get("q")}
		if raw := r.URL.Query().Get("limit"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil {
				httpx.Fail(w, r, fieldInvalid("people.searchHandler", "limit", codeMalformed, "must be a whole number"))
				return
			}
			q.Limit = n
		}
		ms, err := s.Search(r.Context(), q)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		out := make([]wireMatch, len(ms))
		for i, m := range ms {
			out[i] = wireMatch(m)
		}
		writeJSON(w, r, http.StatusOK, struct {
			Matches []wireMatch `json:"matches"`
		}{out})
	})
}

func (s *Service) namesHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ids []model.PersonID
		if raw := r.URL.Query().Get("ids"); raw != "" {
			for _, part := range strings.Split(raw, ",") {
				id, err := model.ParseID[model.PersonID](strings.TrimSpace(part))
				if err != nil {
					httpx.Fail(w, r, fieldInvalid("people.namesHandler", "ids", codeMalformed, "must be comma-separated UUIDs"))
					return
				}
				ids = append(ids, id)
			}
		}
		names, err := s.NamesOf(r.Context(), ids)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, struct {
			Names map[model.PersonID]string `json:"names"`
		}{names})
	})
}

func (s *Service) linkHandler() http.Handler {
	return handle(pathPerson, func(w http.ResponseWriter, r *http.Request, person model.PersonID) error {
		var body wireLinkInput
		if err := httpx.ReadJSON(r, &body); err != nil {
			return err //nolint:wrapcheck // httpx's own kinds
		}
		email, err := model.ParseEmail(body.Email)
		if err != nil {
			return fieldInvalid("people.linkHandler", "email", codeMalformed, "must be a valid e-mail address")
		}
		res, err := s.LinkAccount(r.Context(), person, email)
		if err != nil {
			return err
		}
		writeJSON(w, r, http.StatusOK, wireLinkResult{AccountID: res.Account, OrphanID: res.Orphan, OrphanDeleted: res.OrphanDeleted})
		return nil
	})
}
