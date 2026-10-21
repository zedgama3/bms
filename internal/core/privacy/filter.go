package privacy

import (
	"reflect"
	"slices"

	"github.com/zedgama3/bms/internal/model"
)

// Filter zeroes in place every field of *v the viewer may not see and returns what to
// audit. Shallow: it decides the top-level and embedded fields of *v (exported or not,
// through a non-nil embedded pointer); a nested record with its own subject is filtered by
// its own call before its container. v must be a non-nil pointer to a struct that Check
// accepts: errs.Internal otherwise, with *v untouched and nothing returned, for any problem
// Check would report for the type (an untagged field, a broken tag, a non-public field
// without the omit option its kind needs, a self-marshaling type, an interface-kind field
// and the rest), for a group missing from pol, and for a hidden field that cannot be set.
// Any of these means the module skipped Check. Filter writes through a non-nil embedded
// pointer, so a pointee shared between two records (or with a cache) is changed for all of
// them: a response owns its values and never embeds a pointer to a shared struct.
// Consumer: every service use case that returns a response type. Filter has no I/O and
// never reads, copies or logs a hidden value.
func Filter(v any, pol Policy, viewer Viewer, rel Relation) (Result, error) {
	const op = "privacy.Filter"
	rv := reflect.ValueOf(v)
	if !rv.IsValid() || rv.Kind() != reflect.Pointer || rv.IsNil() || rv.Elem().Kind() != reflect.Struct {
		return Result{}, internalErr(op, "v must be a non-nil pointer to a struct, got %T", v)
	}
	root := rv.Elem()
	p := planOf(root.Type())
	// Any problem Check would report for the type, or for a type nested in it, means the module
	// skipped Check, and there is none it is safe to filter through (not_omittable would ship
	// a placeholder, and a nested untagged field ships in full).
	if dp := deepProblem(root.Type()); dp.found {
		return Result{}, internalErr(op, "%s: %s: %s: %s", root.Type(), dp.code, dp.path, dp.reason)
	}
	// Decide everything before changing anything, so a failure leaves *v untouched.
	keep := make([]bool, len(p.fields))
	for i, f := range p.fields {
		if f.group == "" {
			keep[i] = f.level == model.SensitivityPublic || viewer.signedIn()
		} else {
			aud, ok := pol[f.group]
			if !ok {
				return Result{}, internalErr(op, "%s: the policy has no group %q", root.Type(), f.group)
			}
			keep[i] = visible(aud, f.group, viewer, rel)
		}
		if keep[i] {
			continue
		}
		// A field that must be hidden but cannot be set (an unexported embedded struct with
		// a json name) cannot be hidden: fail closed before changing anything.
		if fv, ok := fieldValue(root, f.index); ok && !fv.CanSet() {
			return Result{}, internalErr(op, "%s: %s must be hidden but cannot be set", root.Type(), f.goPath)
		}
	}
	var res Result
	self := viewer.isSubject(rel)
	for i, f := range p.fields {
		fv, ok := fieldValue(root, f.index)
		if !ok {
			continue // a nil embedded pointer: nothing is on the wire
		}
		if !keep[i] {
			fv.SetZero()
			continue
		}
		if f.group == "" || !model.Sensitivity.ReadIsAudited(f.level, self) {
			continue
		}
		if !slices.Contains(res.Fields, f.jsonName) {
			res.Fields = append(res.Fields, f.jsonName)
		}
		res.Level = max(res.Level, f.level)
	}
	return res, nil
}

// fieldValue follows index from root through embedded fields, and reports false when an
// embedded pointer on the way is nil.
func fieldValue(root reflect.Value, index []int) (reflect.Value, bool) {
	v := root
	for n, i := range index {
		if n > 0 && v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return reflect.Value{}, false
			}
			v = v.Elem()
		}
		v = v.Field(i)
	}
	return v, true
}
