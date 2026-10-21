package privacy

import (
	"fmt"
	"reflect"
	"sync"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// Check parses every view (a struct value or pointer; the module's response types) and
// fails errs.Invalid listing every problem: an exported field without a privacy tag whose
// json tag is not "-"; a level that is not public|internal|personal|sensitive; a group
// name not in groups; a personal or sensitive field with no group (code ungrouped); a
// field that is not public or is in a group (only an ungrouped public field is exempt, because
// it is never hidden) whose json tag lacks the omit option its kind needs
// (code not_omittable; the reason names the option): omitzero for a Struct or Array kind
// (time.Time, model.ID and nested structs included, because omitempty never omits a
// struct and omits an array only at length zero), omitempty or omitzero for every other
// kind; an auditable (personal or sensitive) field whose JSON name is longer than
// audit.MaxFieldBytes (too_long) or does not match [a-z][a-z0-9_]* (malformed); more
// auditable fields than audit.MaxFields (too_many); a view that is not a struct
// (not_struct); a type that implements json.Marshaler or encoding.TextMarshaler itself,
// directly or by promotion, so its tags are not its wire shape (marshaler); a wire field of
// interface kind, directly or under pointers, slices, arrays or maps (dynamic); a named
// unexported embedded struct (or pointer to one) that is not public or is in a group, which
// Filter cannot set (unsettable). Fields are collected as encoding/json collects them: an
// embedded struct or pointer to struct, exported or not, is promoted when its json tag
// names nothing. Named and embedded struct fields, slice, array and map elements and
// pointers are walked, so a nested response type is checked through its container; a
// nested type implementing json.Marshaler or encoding.TextMarshaler is a leaf, but only when
// the value type implements it: a marshaler with a pointer receiver on a nested type is
// refused (marshaler), because encoding/json uses it only on an addressable value; so are two
// embedded marshalers at the same depth (marshaler; neither method is promoted). A json tag
// name encoding/json rejects is ignored, as it does: the field is named by its Go name. The
// groups themselves are validated too (fields groups[j].<field>). Field paths read
// views[i].<Field>, with nested names joined by dots. Consumers: Manifest.Validate;
// moduletest views_tagged.
func Check(views []any, groups []Group) error {
	acc := validate.Errors{Op: "privacy.Check"}
	validateGroups(&acc, groups)
	known := groupNames(groups)
	for i, v := range views {
		prefix := fmt.Sprintf("views[%d]", i)
		t := structTypeOf(v)
		if t == nil {
			acc.Addf(prefix, codeNotStruct, "a view must be a struct or a pointer to one")
			continue
		}
		walk(t, prefix, map[reflect.Type]bool{},
			func(p *plan, path string) {
				for _, pr := range p.problems {
					field := path
					if pr.field != "" {
						field = joinPath(path, pr.field)
					}
					acc.Add(errs.E("", errs.Invalid, &validate.FieldError{Field: field, Code: pr.code, Reason: pr.reason, Params: pr.params}))
				}
			},
			func(f *field, fpath string) {
				if f.group != "" && !known[f.group] {
					acc.Addf(fpath, codeUnknown, "the field names a group that is not declared")
				}
			})
	}
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// structTypeOf returns the struct type of v (a value or a pointer to one), nil otherwise.
func structTypeOf(v any) reflect.Type {
	if v == nil {
		return nil
	}
	t := reflect.TypeOf(v)
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	return t
}

// wireStruct returns the struct type a field of type t puts on the wire through pointers,
// slices, arrays and maps, nil when there is none, when a type on the way marshals itself
// (a leaf), or when one does through its pointer only (the field gets a marshaler problem).
func wireStruct(t reflect.Type) reflect.Type {
	st, _ := shapeOf(t)
	return st
}

// walk is the one traversal of a view's types, shared by Check (every problem, with the path
// of each) and deepProblem (whether Filter may filter the type), so the two cannot drift. It
// calls onPlan with the plan of struct type t under path, then for each field of the plan
// calls onField and descends into the struct the field puts on the wire. active holds the
// types being walked, so a recursive type is walked once down any chain.
func walk(t reflect.Type, path string, active map[reflect.Type]bool,
	onPlan func(p *plan, path string), onField func(f *field, fpath string)) {
	if active[t] {
		return // a recursive type: already being walked above
	}
	active[t] = true
	defer delete(active, t)
	p := planOf(t)
	onPlan(p, path)
	for i := range p.fields {
		f := &p.fields[i]
		fpath := joinPath(path, f.goPath)
		onField(f, fpath)
		if nested := wireStruct(f.typ); nested != nil {
			walk(nested, fpath, active, onPlan, onField)
		}
	}
}

// typeProblem is the first problem found in a view type or a type nested in it.
type typeProblem struct {
	path string // Go field path from the view, "" for the view itself
	problem
	found bool
}

// deepProblems caches deepProblem by view type.
var deepProblems sync.Map // reflect.Type -> *typeProblem

// deepProblem returns the first problem Check would report for struct type t, including those
// of nested types, which Filter cannot see through its shallow plan. Cached per type.
func deepProblem(t reflect.Type) *typeProblem {
	if c, ok := deepProblems.Load(t); ok {
		return c.(*typeProblem) //nolint:forcetypeassert,errcheck // only *typeProblem is ever stored
	}
	res := &typeProblem{}
	walk(t, "", map[reflect.Type]bool{},
		func(p *plan, path string) {
			if res.found || len(p.problems) == 0 {
				return
			}
			pr := p.problems[0]
			*res = typeProblem{path: joinPath(path, pr.field), problem: pr, found: true}
		},
		func(*field, string) {})
	c, _ := deepProblems.LoadOrStore(t, res)
	return c.(*typeProblem) //nolint:forcetypeassert,errcheck // only *typeProblem is ever stored
}

// GroupLevels returns, for each group named by a tag in views, the highest level among its
// fields. Types that are not structs are skipped (Check reports them). Manifest.Validate
// uses it to hold a group's holders permission to a Reaches at least that level. Cannot fail.
func GroupLevels(views []any) map[string]model.Sensitivity {
	out := map[string]model.Sensitivity{}
	seen := map[reflect.Type]bool{}
	var visit func(t reflect.Type)
	visit = func(t reflect.Type) {
		if seen[t] {
			return
		}
		seen[t] = true
		for _, f := range planOf(t).fields {
			if f.group != "" && f.level > out[f.group] {
				out[f.group] = f.level
			}
			if nested := wireStruct(f.typ); nested != nil {
				visit(nested)
			}
		}
	}
	for _, v := range views {
		if t := structTypeOf(v); t != nil {
			visit(t)
		}
	}
	return out
}
