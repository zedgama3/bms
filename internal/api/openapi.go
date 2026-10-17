package api

import (
	"bytes"
	_ "embed" // the shell's own OpenAPI fragment
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/zedgama3/bms/internal/lib/errs"
)

// baseFragment documents the shell's own routes and holds the schemas every set may
// reference (Problem, Field, ID, Sensitivity, TimeRange, AdminModulesResponse, ...).
//
//go:embed openapi_base.json
var baseFragment []byte

const (
	schemaRefPrefix = "#/components/schemas/"
	openAPIVersion  = "3.1.0"
)

// operationMethods are the keys of an OpenAPI path item that hold an operation.
var operationMethods = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true, "options": true, "head": true, "patch": true, "trace": true,
}

// fragment is one parsed set fragment.
type fragment struct {
	set     string
	abs     bool
	base    bool
	paths   map[string]map[string]any // the fragment's own keys (relative for a module set)
	schemas map[string]any
	tags    []any
	doc     map[string]any
}

// assemble merges the shell's base fragment and every set's fragment into one OpenAPI 3.1
// document and proves that routes and fragments agree (api.md decision 11, F14 to F17).
func assemble(version string, sets []setDoc) ([]byte, error) {
	const op = "api.New"
	frags := make([]*fragment, 0, len(sets))
	for i, sd := range sets {
		if len(sd.fragment) == 0 {
			if len(sd.routes) > 0 {
				return nil, newInvalid(op, "api.routes", "required", "set %q has routes and no OpenAPI fragment", sd.name)
			}
			continue
		}
		f, err := parseFragment(sd, i == 0)
		if err != nil {
			return nil, err
		}
		frags = append(frags, f)
		if err := checkAgreement(sd, f); err != nil {
			return nil, err
		}
	}

	paths := map[string]any{}
	schemas := map[string]any{}
	owner := map[string]string{} // schema name -> set ("" is the base)
	var tags []any
	seenTag := map[string]bool{}
	for _, f := range frags {
		for key, item := range f.paths {
			full := key
			if !f.abs {
				full = apiPrefix + f.set + key
			}
			if _, dup := paths[full]; dup {
				return nil, conflict(op, "set %q documents path %q that another set already documents", f.set, full)
			}
			paths[full] = item
		}
		for name, sch := range f.schemas {
			if prev, dup := owner[name]; dup {
				return nil, conflict(op, "schema %q is defined by set %q and by set %q; prefix schema names with the module name",
					name, ownerName(prev), f.set)
			}
			owner[name] = baseOwner(f)
			schemas[name] = sch
		}
		for _, t := range f.tags {
			if m, ok := t.(map[string]any); ok {
				if n, ok := m["name"].(string); ok && n != "" {
					if seenTag[n] {
						continue
					}
					seenTag[n] = true
				}
			}
			tags = append(tags, t)
		}
	}
	for _, f := range frags {
		if err := checkRefs(f, owner); err != nil {
			return nil, err
		}
	}
	addDefaultResponses(paths)

	doc := map[string]any{
		"openapi":    openAPIVersion,
		"info":       map[string]any{"title": "BMS API", "version": version},
		"paths":      paths,
		"components": map[string]any{"schemas": schemas},
	}
	if len(tags) > 0 {
		doc["tags"] = tags
	}
	out, err := json.Marshal(doc)
	if err != nil {
		return nil, errs.E(op, errs.Internal, fmt.Errorf("marshal the OpenAPI document: %w", err))
	}
	return out, nil
}

func baseOwner(f *fragment) string {
	if f.base {
		return ""
	}
	return f.set
}

func ownerName(o string) string {
	if o == "" {
		return shellSetName
	}
	return o
}

// parseFragment reads one fragment and checks its shape (F16).
func parseFragment(sd setDoc, base bool) (*fragment, error) {
	const op = "api.New"
	dec := json.NewDecoder(bytes.NewReader(sd.fragment))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil || doc == nil {
		return nil, newInvalid(op, "api.routes", "malformed", "set %q: the OpenAPI fragment is not a JSON object", sd.name)
	}
	if _, err := dec.Token(); err == nil {
		return nil, newInvalid(op, "api.routes", "malformed", "set %q: the OpenAPI fragment has data after the object", sd.name)
	}
	f := &fragment{set: sd.name, abs: sd.abs, base: base, doc: doc, paths: map[string]map[string]any{}}

	if raw, ok := doc["paths"]; ok {
		paths, ok := raw.(map[string]any)
		if !ok {
			return nil, newInvalid(op, "api.routes", "malformed", "set %q: \"paths\" is not an object", sd.name)
		}
		for key, item := range paths {
			if !strings.HasPrefix(key, "/") {
				return nil, newInvalid(op, "api.routes", "malformed", "set %q: path %q must start with \"/\"", sd.name, key)
			}
			m, ok := item.(map[string]any)
			if !ok {
				return nil, newInvalid(op, "api.routes", "malformed", "set %q: path %q is not an object", sd.name, key)
			}
			f.paths[key] = m
		}
	}
	if raw, ok := doc["components"]; ok {
		comps, ok := raw.(map[string]any)
		if !ok {
			return nil, newInvalid(op, "api.routes", "malformed", "set %q: \"components\" is not an object", sd.name)
		}
		if rawSch, ok := comps["schemas"]; ok {
			sch, ok := rawSch.(map[string]any)
			if !ok {
				return nil, newInvalid(op, "api.routes", "malformed", "set %q: \"components.schemas\" is not an object", sd.name)
			}
			f.schemas = sch
		}
	}
	if raw, ok := doc["tags"]; ok {
		tags, ok := raw.([]any)
		if !ok {
			return nil, newInvalid(op, "api.routes", "malformed", "set %q: \"tags\" is not an array", sd.name)
		}
		f.tags = tags
	}
	return f, nil
}

// checkAgreement proves that every route has an operation at its method and translated
// path, and every operation has a route (F15). A "head" operation is checked against the
// GET route; one is never required.
func checkAgreement(sd setDoc, f *fragment) error {
	const op = "api.New"
	routes := map[opKey]bool{}
	for _, r := range sd.routes {
		routes[r] = true
	}
	for _, r := range sd.routes {
		if item, ok := f.paths[r.path]; !ok || item[r.method] == nil {
			return newInvalid(op, "api.routes", "mismatch", "set %q: route %s %s has no operation in the OpenAPI fragment",
				sd.name, strings.ToUpper(r.method), r.path)
		}
	}
	keys := make([]string, 0, len(f.paths))
	for k := range f.paths {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, p := range keys {
		methods := make([]string, 0, len(f.paths[p]))
		for m := range f.paths[p] {
			if operationMethods[m] {
				methods = append(methods, m)
			}
		}
		sort.Strings(methods)
		for _, m := range methods {
			want := m
			if m == "head" {
				want = "get"
			}
			if !routes[opKey{want, p}] {
				return newInvalid(op, "api.routes", "mismatch", "set %q: the OpenAPI fragment documents %s %s, which has no route",
					sd.name, strings.ToUpper(m), p)
			}
		}
	}
	return nil
}

// checkRefs requires every $ref in f to be local, to resolve against the merged schemas,
// and to name a schema f's own set or the base defines (F16).
func checkRefs(f *fragment, owner map[string]string) error {
	const op = "api.New"
	var failed error
	walkRefs(f.doc, func(ref string) {
		if failed != nil {
			return
		}
		name, ok := strings.CutPrefix(ref, schemaRefPrefix)
		if !ok || name == "" || strings.Contains(name, "/") {
			failed = newInvalid(op, "api.routes", "malformed", "set %q: $ref %q is not local to #/components/schemas/", f.set, ref)
			return
		}
		o, found := owner[name]
		switch {
		case !found:
			failed = newInvalid(op, "api.routes", "malformed", "set %q: $ref %q does not resolve", f.set, ref)
		case o != "" && o != f.set:
			failed = newInvalid(op, "api.routes", "malformed",
				"set %q: $ref %q names a schema of set %q; a set may reference only its own and the base schemas", f.set, ref, o)
		}
	})
	return failed
}

func walkRefs(v any, fn func(ref string)) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if s, ok := t[k].(string); ok && k == "$ref" {
				fn(s)
				continue
			}
			walkRefs(t[k], fn)
		}
	case []any:
		for _, e := range t {
			walkRefs(e, fn)
		}
	}
}

// defaultResponse refers to Problem, the body of every error.
func defaultResponse() map[string]any {
	return map[string]any{
		"description": "An error. The body is a Problem.",
		"content": map[string]any{
			"application/problem+json": map[string]any{
				"schema": map[string]any{"$ref": schemaRefPrefix + "Problem"},
			},
		},
	}
}

// addDefaultResponses adds a "default" response to every operation that lacks one.
func addDefaultResponses(paths map[string]any) {
	for _, item := range paths {
		pi, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for m, opv := range pi {
			if !operationMethods[m] {
				continue
			}
			o, ok := opv.(map[string]any)
			if !ok {
				continue
			}
			resp, ok := o["responses"].(map[string]any)
			if !ok {
				resp = map[string]any{}
				o["responses"] = resp
			}
			if _, has := resp["default"]; !has {
				resp["default"] = defaultResponse()
			}
		}
	}
}
