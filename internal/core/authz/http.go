package authz

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

// Route is one HTTP route of the access set; app converts it to module.Route (authz
// cannot import core/module). Every route is for signed-in people: the permission is
// checked inside the use case.
type Route struct {
	Method  string
	Pattern string // relative to /api/access
	Handler http.Handler
}

// Routes returns the twelve routes of the access set. Handlers are thin: parse, call the
// service, httpx.Fail on error. Cannot fail.
func (s *Service) Routes() []Route {
	return []Route{
		{Method: http.MethodGet, Pattern: "/permissions", Handler: s.listPermissionsHandler()},
		{Method: http.MethodGet, Pattern: "/roles", Handler: s.listRolesHandler()},
		{Method: http.MethodPost, Pattern: "/roles", Handler: s.createRoleHandler()},
		{Method: http.MethodGet, Pattern: "/roles/{role}", Handler: s.getRoleHandler()},
		{Method: http.MethodPut, Pattern: "/roles/{role}", Handler: s.updateRoleHandler()},
		{Method: http.MethodDelete, Pattern: "/roles/{role}", Handler: deleteHandler(roleID, s.DeleteRole)},
		{Method: http.MethodGet, Pattern: "/grants", Handler: s.listGrantsHandler()},
		{Method: http.MethodPost, Pattern: "/grants", Handler: s.grantHandler()},
		{Method: http.MethodDelete, Pattern: "/grants/{grant}", Handler: deleteHandler(grantID, s.Revoke)},
		{Method: http.MethodGet, Pattern: "/rules", Handler: s.listRulesHandler()},
		{Method: http.MethodPost, Pattern: "/rules", Handler: s.createRuleHandler()},
		{Method: http.MethodDelete, Pattern: "/rules/{rule}", Handler: deleteHandler(ruleID, s.DeleteRule)},
	}
}

// OpenAPI returns the embedded fragment for those routes. Cannot fail (a test proves it
// parses and names exactly the routes).
func (s *Service) OpenAPI() []byte { return append([]byte(nil), openAPIFragment...) }

// writeJSON writes v and leaves a transport error to the shell (it is already remembered
// on the span).
func writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	if err := httpx.WriteJSON(w, status, v); err != nil && !errs.Is(err, errs.Canceled) {
		httpx.Fail(w, r, err)
	}
}

// pathID reads the path value name as a typed ID (F62).
func pathID[T ~[16]byte](r *http.Request, name string) (T, error) {
	id, err := model.ParseID[T](r.PathValue(name))
	if err != nil {
		return T{}, fieldInvalid("authz.pathID", name, codeMalformed, "must be a UUID")
	}
	return id, nil
}

func roleID(r *http.Request) (RoleID, error)   { return pathID[RoleID](r, "role") }
func grantID(r *http.Request) (GrantID, error) { return pathID[GrantID](r, "grant") }
func ruleID(r *http.Request) (RuleID, error)   { return pathID[RuleID](r, "rule") }

func (s *Service) listPermissionsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		infos, err := s.ListPermissions(r.Context())
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		out := make([]wirePermission, len(infos))
		for i, in := range infos {
			out[i] = wirePermissionOf(in)
		}
		writeJSON(w, r, http.StatusOK, struct {
			Permissions []wirePermission `json:"permissions"`
		}{out})
	})
}

func (s *Service) listRolesHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		roles, err := s.ListRoles(r.Context())
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		out := make([]wireRole, len(roles))
		for i, ro := range roles {
			out[i] = wireRoleOf(ro)
		}
		writeJSON(w, r, http.StatusOK, struct {
			Roles []wireRole `json:"roles"`
		}{out})
	})
}

func (s *Service) createRoleHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body wireRoleInput
		if err := httpx.ReadJSON(r, &body); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		in, err := body.input("authz.createRoleHandler")
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		role, err := s.CreateRole(r.Context(), in)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusCreated, wireRoleOf(role))
	})
}

func (s *Service) getRoleHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := roleID(r)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		role, err := s.GetRole(r.Context(), id)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, wireRoleOf(role))
	})
}

func (s *Service) updateRoleHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := roleID(r)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		var body wireRoleInput
		if err := httpx.ReadJSON(r, &body); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		in, err := body.input("authz.updateRoleHandler")
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		role, err := s.UpdateRole(r.Context(), id, in)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, wireRoleOf(role))
	})
}

// deleteHandler answers 204 for a delete by path ID (roles, grants, rules).
func deleteHandler[T ~[16]byte](parse func(*http.Request) (T, error), do func(context.Context, T) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := parse(r)
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

// queryID reads the optional ID parameter name; absent is the zero ID.
func queryID[T ~[16]byte](vals url.Values, name string) (T, error) {
	raw := vals.Get(name)
	if raw == "" {
		return T{}, nil
	}
	id, err := model.ParseID[T](raw)
	if err != nil {
		return T{}, fieldInvalid("authz.parseGrantQuery", name, codeMalformed, "must be a UUID")
	}
	return id, nil
}

// parseGrantQuery reads ?person=&role= (an ID or a key)&department=&permission=&include_ended=&after=&limit=.
func parseGrantQuery(vals url.Values) (GrantQuery, error) {
	const op = "authz.parseGrantQuery"
	var (
		q   GrantQuery
		err error
	)
	if q.Person, err = queryID[model.PersonID](vals, "person"); err != nil {
		return GrantQuery{}, err
	}
	// ?role= is a role ID or a role key (department-admin); an ID wins when the text is one.
	if raw := vals.Get("role"); raw != "" {
		if q.Role, err = model.ParseID[RoleID](raw); err != nil {
			q.Role, q.RoleKey = RoleID{}, raw
		}
	}
	if q.Department, err = queryID[model.DepartmentID](vals, "department"); err != nil {
		return GrantQuery{}, err
	}
	q.Permission = Permission(vals.Get("permission"))
	if raw := vals.Get("include_ended"); raw != "" {
		if q.IncludeEnded, err = strconv.ParseBool(raw); err != nil {
			return GrantQuery{}, fieldInvalid(op, "include_ended", codeMalformed, "must be true or false")
		}
	}
	q.After = Cursor(vals.Get("after"))
	if raw := vals.Get("limit"); raw != "" {
		if q.Limit, err = strconv.Atoi(raw); err != nil {
			return GrantQuery{}, fieldInvalid(op, "limit", codeMalformed, "must be a whole number")
		}
	}
	return q, nil
}

func (s *Service) listGrantsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q, err := parseGrantQuery(r.URL.Query())
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		page, err := s.ListGrants(r.Context(), q)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		now := s.now()
		out := wireGrantPage{Grants: make([]wireGrant, len(page.Grants)), Next: page.Next}
		for i, g := range page.Grants {
			out.Grants[i] = s.wireGrantOf(g, now)
		}
		writeJSON(w, r, http.StatusOK, out)
	})
}

func (s *Service) grantHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body wireGrantInput
		if err := httpx.ReadJSON(r, &body); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		g, err := s.Grant(r.Context(), GrantInput{
			Person: body.PersonID, Role: body.RoleID, Department: body.DepartmentID, EndsAt: body.EndsAt,
		})
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusCreated, s.wireGrantOf(g, s.now()))
	})
}

func (s *Service) listRulesHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rules, err := s.ListRules(r.Context())
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		out := make([]wireRule, len(rules))
		for i, ru := range rules {
			out[i] = wireRuleOf(ru)
		}
		writeJSON(w, r, http.StatusOK, struct {
			Rules []wireRule `json:"rules"`
		}{out})
	})
}

func (s *Service) createRuleHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body wireRuleInput
		if err := httpx.ReadJSON(r, &body); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		in, err := body.input("authz.createRuleHandler")
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		rule, err := s.CreateRule(r.Context(), in)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusCreated, wireRuleOf(rule))
	})
}
