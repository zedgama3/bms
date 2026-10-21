package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
)

const (
	apiPrefix    = "/api/"
	maxSpanName  = 128
	shellSetName = "core"
)

// fallbackMethods are probed, in this order, to build Allow (decision 3).
var fallbackMethods = []string{
	http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete,
}

// opKey is one route as OpenAPI names it: the lower-case method and the OpenAPI path, which
// for a module's set is relative to /api/<name>.
type opKey struct{ method, path string }

// setDoc is what OpenAPI assembly needs from one mounted set.
type setDoc struct {
	name     string
	abs      bool // paths are absolute (the shell's own set)
	fragment []byte
	routes   []opKey
}

// mountAll mounts the shell's own set, then every set in Options.Routes, and returns what
// the OpenAPI assembly needs.
func (s *Server) mountAll() ([]setDoc, error) {
	docs := []setDoc{{name: shellSetName, abs: true, fragment: baseFragment}}
	shell := &docs[0]
	for _, sr := range s.shellRoutes() {
		if err := s.mount(shellSetName, sr.method, sr.path, sr.access, sr.handler, routeLimits{}); err != nil {
			return nil, err
		}
		shell.routes = append(shell.routes, opKey{strings.ToLower(sr.method), openAPIPath(sr.path)})
	}
	for _, set := range s.opts.Routes {
		doc := setDoc{name: set.Module, fragment: set.OpenAPI}
		prefix := apiPrefix + set.Module
		for _, rt := range set.Routes {
			if err := rt.Validate(); err != nil {
				return nil, fmt.Errorf("api.New: set %s: %w", set.Module, err)
			}
			full := prefix + rt.Pattern
			if err := checkEscapes(set.Module, rt, full); err != nil {
				return nil, err
			}
			if err := s.mount(set.Module, rt.Method, full, rt.Access, rt.Handler, routeLimits{maxBody: rt.MaxBodyBytes, timeout: rt.Timeout}); err != nil {
				return nil, err
			}
			doc.routes = append(doc.routes, opKey{strings.ToLower(rt.Method), openAPIPath(rt.Pattern)})
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

// checkEscapes refuses a route that would mount outside /api/<name>/ (F13): "." or ".."
// segments, or an empty one.
func checkEscapes(set string, rt module.Route, full string) error {
	prefix := apiPrefix + set + "/"
	bad := !strings.HasPrefix(full, prefix) || strings.Contains(rt.Pattern, "//")
	for seg := range strings.SplitSeq(rt.Pattern, "/") {
		if seg == "." || seg == ".." {
			bad = true
		}
	}
	if bad {
		return newInvalid("api.New", "api.routes", "malformed",
			"set %q route %s %q would mount outside /api/%s/", set, rt.Method, rt.Pattern, set)
	}
	return nil
}

// routeToken renders a mux path as the route token: a trailing {$} becomes the "/" it
// stands for.
func routeToken(path string) string { return strings.TrimSuffix(path, "{$}") }

var wildcardRE = regexp.MustCompile(`\{([^}]+)\.\.\.\}`)

// openAPIPath translates a ServeMux path or pattern to its OpenAPI form: a trailing {$}
// is dropped, {p...} becomes {p}.
func openAPIPath(p string) string {
	return wildcardRE.ReplaceAllString(routeToken(p), "{$1}")
}

// mount registers one route with the per-route layers around it. path is the full mux
// path (full); a trailing "/" is made exact by appending {$}.
func (s *Server) mount(set, method, full string, access module.Access, h http.Handler, lim routeLimits) error {
	const op = "api.New"
	if strings.HasSuffix(full, "/") {
		full += "{$}"
	}
	token := routeToken(full)
	name := method + " " + token
	if !telemetry.IsToken(token) || len(name) > maxSpanName {
		return newInvalid(op, "api.routes", "malformed",
			"set %q route %s %q does not fit the span name grammar or exceeds %d bytes", set, method, token, maxSpanName)
	}
	rt := &route{set: set, pattern: method + " " + full, token: token, access: access, handler: h}
	if err := s.register(rt); err != nil {
		return err
	}
	if lim != (routeLimits{}) {
		s.limits[rt.pattern] = lim
	}
	return nil
}

// register adds rt to the mux, turning ServeMux's panic into an error (F12).
func (s *Server) register(rt *route) (err error) {
	const op = "api.New"
	defer func() {
		rec := recover()
		if rec == nil {
			return
		}
		msg := fmt.Sprint(rec)
		if strings.Contains(msg, "conflicts with") || strings.Contains(msg, "already") {
			err = conflict(op, "set %q pattern %q conflicts with %s", rt.set, rt.pattern, s.describeConflict(msg))
			return
		}
		err = newInvalid(op, "api.routes", "malformed", "set %q pattern %q is not valid: %s", rt.set, rt.pattern, firstLine(msg))
	}()
	s.mux.Handle(rt.pattern, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.serveRoute(rt, w, r)
	}))
	s.known[rt.pattern] = true
	s.owners[rt.pattern] = rt.set
	return nil
}

var quotedRE = regexp.MustCompile(`"([^"]+)"`)

// describeConflict names the earlier pattern ServeMux reported and the set that owns it.
func (s *Server) describeConflict(msg string) string {
	for _, m := range quotedRE.FindAllStringSubmatch(msg, -1) {
		if set, ok := s.owners[m[1]]; ok {
			return fmt.Sprintf("pattern %q of set %q", m[1], set)
		}
	}
	return "an earlier pattern"
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// mountFallbacks registers the one method-less "/api/" handler that answers 404 or 405,
// and prepares the route used for paths the mux would redirect.
func (s *Server) mountFallbacks() error {
	probe := &route{set: shellSetName, pattern: apiPrefix, token: unmatchedRoute, access: module.AccessPublic, unmatched: true,
		handler: http.HandlerFunc(s.fallback)}
	if err := s.register(probe); err != nil {
		return err
	}
	s.unmatched = &route{set: shellSetName, pattern: apiPrefix, token: unmatchedRoute, access: module.AccessPublic, unmatched: true,
		handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			httpx.Fail(w, r, errs.E("api.router", errs.NotFound, errNoRoute))
		})}
	return nil
}

var errNoRoute = errors.New("no such route")

// fallback answers 405 with Allow when the path exists for other methods, else 404, by
// probing the mux with each method (decision 3).
func (s *Server) fallback(w http.ResponseWriter, r *http.Request) {
	var allowed []string
	for _, m := range fallbackMethods {
		probe := r.Clone(r.Context())
		probe.Method = m
		_, pattern := s.mux.Handler(probe)
		// patternMatches rejects the mux's redirect to a trailing-slash twin, whose pattern
		// is reported but does not match the path as requested.
		if s.known[pattern] && pattern != apiPrefix && patternMatches(pattern, r.URL.EscapedPath()) {
			allowed = append(allowed, m)
		}
	}
	if len(allowed) == 0 {
		httpx.Fail(w, r, errs.E("api.router", errs.NotFound, errNoRoute))
		return
	}
	httpx.Fail(w, r, shell("api.router", errs.Invalid, http.StatusMethodNotAllowed, httpx.CodeMethodNotAllowed,
		"method not allowed", http.Header{"Allow": {strings.Join(allowed, ", ")}}))
}

// cleanPath is net/http's own cleaning, to recognise a path the mux would redirect.
func cleanPath(p string) string {
	if p == "" {
		return "/"
	}
	if p[0] != '/' {
		p = "/" + p
	}
	np := path.Clean(p)
	if p[len(p)-1] == '/' && np != "/" {
		if len(p) == len(np)+1 && strings.HasPrefix(p, np) {
			np = p
		} else {
			np += "/"
		}
	}
	return np
}

// dispatch hands an /api/ request to the mux, except one the mux would answer with a
// redirect, which becomes a JSON 404 (decision 3, F41).
func (s *Server) dispatch(w http.ResponseWriter, r *http.Request) {
	p := r.URL.EscapedPath() // what the mux itself cleans and matches
	if p == "/api" || strings.HasPrefix(p, apiPrefix) {
		redirect := p == "/api" || cleanPath(p) != p
		if !redirect {
			_, pattern := s.mux.Handler(r)
			// The mux reports a redirect with the pattern of its target, so the pattern
			// must also match the path as requested.
			redirect = !s.known[pattern] || !patternMatches(pattern, p)
		}
		if redirect {
			s.serveRoute(s.unmatched, w, r)
			return
		}
	}
	s.mux.ServeHTTP(w, r)
}

// patternMatches reports whether a registered ServeMux pattern (an optional method, then a
// path with {name}, {name...} and a trailing {$}) matches the request path as written.
// dispatch uses it to tell a real match from the mux's redirect to a trailing-slash twin.
func patternMatches(pattern, reqPath string) bool {
	if _, rest, ok := strings.Cut(pattern, " "); ok {
		pattern = rest
	}
	if strings.HasSuffix(pattern, "/") && !strings.HasSuffix(pattern, "{$}") { // a subtree
		return strings.HasPrefix(reqPath, pattern)
	}
	ps := strings.Split(pattern, "/")
	rs := strings.Split(reqPath, "/") // escaped path: a segment is unescaped before it is compared
	for i, seg := range rs {
		if u, err := url.PathUnescape(seg); err == nil {
			rs[i] = u
		}
	}
	for i, seg := range ps {
		switch {
		case seg == "{$}" && i == len(ps)-1:
			return len(rs) == i+1 && rs[i] == ""
		case strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "...}"):
			return i < len(rs)
		case i >= len(rs):
			return false
		case strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}"):
			if rs[i] == "" {
				return false
			}
		case seg != rs[i]:
			return false
		}
	}
	return len(ps) == len(rs)
}

// limitsFor returns the raises of the route a request will reach, if it has any: the mux
// is asked which pattern matches, as dispatch does.
func (s *Server) limitsFor(r *http.Request) (routeLimits, bool) {
	if len(s.limits) == 0 {
		return routeLimits{}, false
	}
	p := r.URL.EscapedPath()
	if !strings.HasPrefix(p, apiPrefix) || cleanPath(p) != p {
		return routeLimits{}, false
	}
	_, pattern := s.mux.Handler(r)
	lim, ok := s.limits[pattern]
	if !ok || !patternMatches(pattern, p) {
		return routeLimits{}, false
	}
	return lim, true
}
