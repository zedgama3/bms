package privacy

import (
	"encoding"
	"encoding/json"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/model"
)

// auditNameRE restates audit.Entry.Fields' grammar (privacy-tags A1.8): audit does not
// export it, and a field name that breaks it would fail the first audited read.
var auditNameRE = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

var (
	jsonMarshalerType = reflect.TypeFor[json.Marshaler]()
	textMarshalerType = reflect.TypeFor[encoding.TextMarshaler]()
)

// isLeaf reports whether t or a pointer to it marshals itself, so its fields are not the wire
// shape (time.Time, model.ID). It is the test for a top-level view, which Filter and the
// HTTP layer reach through a pointer, and for an embedded type, whose methods are promoted to
// the embedding struct.
func isLeaf(t reflect.Type) bool {
	return marshalsByValue(t) || marshalsByPointerOnly(t)
}

// marshalsByValue reports whether t itself implements json.Marshaler or
// encoding.TextMarshaler. A nested type is a leaf only then: encoding/json uses a
// pointer-receiver marshaler only on an addressable value (a slice element, a field of an
// addressable struct), never for a map value or a value marshaled by value.
func marshalsByValue(t reflect.Type) bool {
	return t.Implements(jsonMarshalerType) || t.Implements(textMarshalerType)
}

// marshalsByPointerOnly reports whether *t implements a marshaler that t does not: what
// encoding/json does with it depends on whether the value is addressable, so a nested type
// like that has no fixed wire shape.
func marshalsByPointerOnly(t reflect.Type) bool {
	if t.Kind() == reflect.Pointer || marshalsByValue(t) {
		return false
	}
	pt := reflect.PointerTo(t)
	return pt.Implements(jsonMarshalerType) || pt.Implements(textMarshalerType)
}

// shapeOf strips pointers, slices, arrays and maps from t. It returns the struct type inside
// (nil when there is none, or when a type on the way marshals itself by value), and whether
// a type on the way marshals itself through its pointer only (ptrOnly; the walk stops there).
func shapeOf(t reflect.Type) (st reflect.Type, ptrOnly bool) {
	for {
		if marshalsByValue(t) {
			return nil, false
		}
		if marshalsByPointerOnly(t) {
			return nil, true
		}
		switch t.Kind() { //nolint:exhaustive // only the containers and a struct lead anywhere
		case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
			t = t.Elem()
		case reflect.Struct:
			return t, false
		default:
			return nil, false
		}
	}
}

// isValidJSONName restates encoding/json's isValidTag: a json tag name it rejects is ignored,
// and the field is named by its Go name (an embedded struct is promoted).
func isValidJSONName(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		switch {
		case strings.ContainsRune("!#$%&()*+-./:;<=>?@[]^_{|}~ ", c):
		case !unicode.IsLetter(c) && !unicode.IsDigit(c):
			return false
		}
	}
	return true
}

// problem is one finding about a type, relative to the type: field is the Go field path
// ("Name", "Embedded.Name") or "" for the type itself.
type problem struct {
	field  string
	code   string
	reason string
	params map[string]string
}

// field is one wire field of a type, flattened as encoding/json promotes embedded fields.
type field struct {
	index    []int // path from the struct, through embedded fields
	goPath   string
	jsonName string
	level    model.Sensitivity // 0 when the tag is missing or broken
	group    string
	// omitOK is whether the json tag carries the option the field's kind needs to drop a
	// zero value: omitzero for a Struct or Array kind, omitempty or omitzero for the rest.
	omitOK bool
	// omitWanted names the option omitOK lacks, for the problem's reason.
	omitWanted string
	// unsettable marks a named field of an unexported embedded struct: it is on the wire but
	// reflect cannot set it.
	unsettable bool
	typ        reflect.Type
}

// plan is the parsed tags of one struct type. Any problem in it makes Filter fail closed.
type plan struct {
	fields    []field
	problems  []problem
	auditable int
}

// plans caches parsePlan by reflect.Type; the cache is read-mostly and safe for concurrent use.
var plans sync.Map // reflect.Type -> *plan

// planOf returns the plan of struct type t, parsing it once.
func planOf(t reflect.Type) *plan {
	if p, ok := plans.Load(t); ok {
		return p.(*plan) //nolint:forcetypeassert,errcheck // only *plan is ever stored
	}
	p, _ := plans.LoadOrStore(t, parsePlan(t))
	return p.(*plan) //nolint:forcetypeassert,errcheck // only *plan is ever stored
}

// parsePlan reads the privacy and json tags of every exported wire field of t (a struct).
func parsePlan(t reflect.Type) *plan {
	p := &plan{}
	if isLeaf(t) {
		// The view marshals itself (directly or by a promoted method), so its tags are not
		// its wire shape.
		p.problems = append(p.problems, problem{code: codeMarshaler,
			reason: "the type marshals itself (json.Marshaler or encoding.TextMarshaler, directly or promoted), so its tags are not its wire shape; tag the fields of a plain struct"})
		return p
	}
	p.collect(t, nil, "", map[reflect.Type]bool{t: true})
	seen := map[string]string{}
	for _, f := range p.fields {
		if f.group == "" {
			continue
		}
		if other, dup := seen[f.jsonName]; dup {
			p.problems = append(p.problems, problem{field: f.goPath, code: codeDuplicate,
				reason: "the JSON name is also used by " + other})
		}
		seen[f.jsonName] = f.goPath
	}
	if p.auditable > audit.MaxFields {
		p.problems = append(p.problems, problem{code: codeTooMany,
			reason: "more auditable fields than one audit entry can name", params: map[string]string{"max": itoa(audit.MaxFields)}})
	}
	return p
}

// collect appends t's fields, entering embedded structs. path is the Go path so far.
func (p *plan) collect(t reflect.Type, index []int, path string, entering map[reflect.Type]bool) {
	for i := range t.NumField() {
		sf := t.Field(i)
		// This follows encoding/json's typeFields: an anonymous field of (pointer to) struct
		// type is kept even when unexported; any other unexported field is ignored.
		et := sf.Type
		if sf.Anonymous {
			if et.Kind() == reflect.Pointer {
				et = et.Elem()
			}
			if !sf.IsExported() && et.Kind() != reflect.Struct {
				continue
			}
		} else if !sf.IsExported() {
			continue
		}
		jsonTag, hasJSON := sf.Tag.Lookup("json")
		name, opts, _ := strings.Cut(jsonTag, ",")
		if !isValidJSONName(name) {
			name = "" // encoding/json ignores an invalid name
		}
		if hasJSON && jsonTag == "-" {
			continue // not on the wire
		}
		idx := append(append([]int(nil), index...), i)
		goPath := joinPath(path, sf.Name)

		// An untagged embedded struct is promoted on the wire (a nil embedded pointer emits
		// nothing); a tagged one is a field of its own.
		if sf.Anonymous && name == "" && et.Kind() == reflect.Struct {
			if isLeaf(sf.Type) {
				// The embedding struct does not marshal itself (Check would have stopped at the
				// type otherwise), so the embedded marshaler's method was not promoted: two
				// embedded marshalers at the same depth cancel each other, and the encoder then
				// walks both structs. That shape is not mirrored; it is refused.
				p.problems = append(p.problems, problem{field: goPath, code: codeMarshaler,
					reason: "ambiguous promoted marshaler: two embedded types at the same depth marshal themselves, so neither method is promoted; tag the embedded struct with a json name or embed one"})
				continue
			}
			if entering[et] {
				continue
			}
			entering[et] = true
			p.collect(et, idx, goPath, entering)
			delete(entering, et)
			continue
		}
		jsonName := name
		if jsonName == "" {
			jsonName = sf.Name
		}
		f := field{index: idx, goPath: goPath, jsonName: jsonName, typ: sf.Type}
		f.unsettable = sf.Anonymous && !sf.IsExported()
		var omitEmpty, omitZero bool
		for _, o := range strings.Split(opts, ",") {
			switch o {
			case "omitempty":
				omitEmpty = true
			case "omitzero":
				omitZero = true
			}
		}
		// encoding/json's omitempty never omits a struct (time.Time included) and omits an
		// array only at length zero; omitzero drops any zero value.
		switch sf.Type.Kind() { //nolint:exhaustive // only the kinds omitempty cannot drop are special
		case reflect.Struct, reflect.Array:
			f.omitOK = omitZero
			f.omitWanted = "omitzero (omitempty never omits a struct and omits an array only at length zero)"
		default:
			f.omitOK = omitEmpty || omitZero
			f.omitWanted = "omitempty or omitzero"
		}
		if hasInterface(sf.Type) {
			p.problems = append(p.problems, problem{field: goPath, code: codeDynamic,
				reason: "an interface-kind field has no static shape; use one tagged struct per shape, or json.RawMessage tagged as a whole"})
		}
		if _, ptrOnly := shapeOf(sf.Type); ptrOnly {
			p.problems = append(p.problems, problem{field: goPath, code: codeMarshaler,
				reason: "pointer-receiver marshaler: encoding/json uses it only on an addressable value, so the wire shape varies; use a value receiver"})
		}
		p.parseTag(&f, sf)
		p.fields = append(p.fields, f)
	}
}

// hasInterface reports whether t is an interface, or a pointer, slice, array or map of one
// (a type that marshals itself, json.RawMessage for one, is a leaf and has no shape to see).
func hasInterface(t reflect.Type) bool {
	for {
		if t.Kind() == reflect.Interface {
			return true
		}
		if isLeaf(t) {
			return false
		}
		switch t.Kind() { //nolint:exhaustive // only the containers lead to another type
		case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
			t = t.Elem()
		default:
			return false
		}
	}
}

// parseTag reads the privacy tag of sf into f and records what is wrong with it.
func (p *plan) parseTag(f *field, sf reflect.StructField) {
	bad := func(code, reason string) {
		p.problems = append(p.problems, problem{field: f.goPath, code: code, reason: reason})
	}
	tag, ok := sf.Tag.Lookup("privacy")
	if !ok {
		bad(codeRequired, "has no privacy tag")
		return
	}
	levelName, rest, hasRest := strings.Cut(tag, ",")
	level, err := model.ParseSensitivity(levelName)
	if err != nil {
		bad(codeOneOf, "the privacy level must be one of: public, internal, personal, sensitive")
		return
	}
	f.level = level
	if hasRest {
		key, value, found := strings.Cut(rest, "=")
		if !found || key != "group" || value == "" || strings.Contains(value, ",") || !identRE.MatchString(value) {
			bad(codeMalformed, "the privacy tag is <level>[,group=<name>]")
			return
		}
		f.group = value
	}
	// Every field that can be hidden carries the omit option its kind needs, so a hidden
	// field is absent on the wire, never a placeholder: a non-public field (an anonymous
	// reader loses it) and a grouped field of any level (the regional's policy can hide it).
	// Only an ungrouped public field is never hidden.
	if level != model.SensitivityPublic || f.group != "" {
		if !f.omitOK {
			p.problems = append(p.problems, problem{field: f.goPath, code: codeNotOmitable,
				reason: "a non-public field's json tag needs " + f.omitWanted + " so a hidden field is absent, never a placeholder"})
		}
		if f.unsettable {
			p.problems = append(p.problems, problem{field: f.goPath, code: codeUnsettable,
				reason: "a named unexported embedded struct cannot be set, so it cannot be hidden; export the type, drop the json name or tag it public"})
		}
	}
	if f.group == "" {
		if level >= model.SensitivityPersonal {
			bad(codeUngrouped, "a personal or sensitive field must name a group")
		}
		return
	}
	if level >= model.SensitivityPersonal {
		p.auditable++
		switch {
		case len(f.jsonName) > audit.MaxFieldBytes:
			p.problems = append(p.problems, problem{field: f.goPath, code: codeTooLong,
				reason: "the JSON name is too long for an audit entry", params: map[string]string{"max": itoa(audit.MaxFieldBytes)}})
		case !auditNameRE.MatchString(f.jsonName):
			p.problems = append(p.problems, problem{field: f.goPath, code: codeMalformed,
				reason: "the JSON name of an audited field must match [a-z][a-z0-9_]*"})
		}
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
