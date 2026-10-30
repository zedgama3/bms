package settings

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// Kind is the type of a setting's value. The zero value is invalid.
type Kind uint8

// The setting kinds.
const (
	Bool     Kind = iota + 1 // "true" or "false"
	Int                      // a base-10 integer (int64)
	Duration                 // time.ParseDuration form ("72h"); days are written as hours
	Text                     // free text, at most MaxTextBytes
	Enum                     // exactly one of Options
	Secret                   // free text, sealed at rest, never returned; Default must be ""
)

// String returns "bool", "int", "duration", "text", "enum", "secret" or "Kind(<n>)". Cannot fail.
func (k Kind) String() string {
	switch k {
	case Bool:
		return "bool"
	case Int:
		return "int"
	case Duration:
		return "duration"
	case Text:
		return "text"
	case Enum:
		return "enum"
	case Secret:
		return "secret"
	default:
		return "Kind(" + strconv.Itoa(int(k)) + ")"
	}
}

func (k Kind) valid() bool { return k >= Bool && k <= Secret }

// Field codes settings adds to the shell's vocabulary (validate has the rest).
const (
	codeRequired  = validate.CodeRequired
	codeMalformed = validate.CodeMalformed
	codeOneOf     = validate.CodeOneOf
	codeTooLong   = "too_long"      // params: max
	codeTooMany   = "too_many"      // params: max
	codeControl   = "control_chars" // a control character in a text value
	codeUnknown   = "unknown"       // an undeclared key, term, form or zone
	codeRejected  = "rejected"      // a Check's reason
	codeURL       = validate.CodeURL
	codeContrast  = "contrast" // params: ratio, min
	codeConflict  = "conflict" // a form both set and reset
	codeFormat    = "format"   // a term key that is not an ident (D57)
	codeReserved  = "reserved" // a term key that is a CoreTerms name (D57)
	codeDuplicate = "duplicate"
)

// Limits, in bytes (they match the CHECK constraints), and bounds.
const (
	MaxBatch     = 200  // keys in one Set
	MaxTextBytes = 4096 // a Text or Secret value
	MaxNameBytes = 80   // regional.name
	MaxURLBytes  = 256  // regional.public_url
	MaxTermBytes = 60   // one form of one term

	maxIdentBytes = 64
)

// keyRE is the key grammar: one ident of underscored lower-case segments. Module-contract
// uses underscores because an ICU argument allows no hyphen.
var keyRE = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)

// optionRE is the grammar of an enum option: an ident whose segments are joined by a
// single hyphen or underscore.
var optionRE = regexp.MustCompile(`^[a-z][a-z0-9]*([-_][a-z0-9]+)*$`)

// areaRE is the grammar of an area name: letters and digits, as module names are.
var areaRE = regexp.MustCompile(`^[a-z][a-z0-9]*$`)

const maxAreaBytes = 32

func isKey(s string) bool    { return len(s) <= maxIdentBytes && keyRE.MatchString(s) }
func isOption(s string) bool { return len(s) <= maxIdentBytes && optionRE.MatchString(s) }

// Decl declares one setting of an area. Its title and description are message keys
// (module-contract decision 8): module.<area>.setting.<key>.title for a module,
// core.<area>.setting.<key>.title for a core area; an Enum's options
// module.<area>.setting.<key>.option.<opt>.
type Decl struct {
	Key     string   // one ident: [a-z][a-z0-9]*(_[a-z0-9]+)*, at most 64 bytes
	Kind    Kind     // how Default and every value are parsed
	Default string   // text form; parses as Kind and passes Check; "" for Secret
	Options []string // Enum only: at least two idents, no duplicates
	// Check is an optional extra rule on the text value, pure and safe for concurrent use.
	// It may return validate.FieldErrors (re-fielded under the setting key, codes kept)
	// or any error (code "rejected", reason = err.Error()).
	Check func(value string) error `json:"-"`
}

// Validate fails errs.Invalid joining one validate.FieldError per problem, on the fields
// "key", "kind", "options" and "default": Key not an ident; Kind invalid; Options given
// for a kind other than Enum, or fewer than two, not idents or duplicated for Enum;
// Default not parsable as Kind or rejected by Check; a Secret with a Default. Messages
// never echo a value.
func (d Decl) Validate() error {
	acc := validate.Errors{Op: "settings.Decl.Validate"}
	if !isKey(d.Key) {
		acc.Addf("key", codeMalformed, "must be lower-case letters and digits in snake_case, starting with a letter, at most %d bytes", maxIdentBytes)
	}
	if !d.Kind.valid() {
		acc.Addf("kind", codeOneOf, "must be one of: bool, int, duration, text, enum, secret")
	}
	optionsOK := d.checkOptions(&acc)
	switch {
	case !d.Kind.valid() || !optionsOK:
	case d.Kind == Secret:
		if d.Default != "" {
			acc.Addf("default", codeMalformed, "a secret setting has no default")
		}
	default:
		if problem := shapeOf(d, d.Default); problem.code != "" {
			acc.Add(errs.E("", errs.Invalid, &validate.FieldError{
				Field: "default", Code: problem.code, Params: problem.params, Reason: problem.reason,
			}))
		} else if d.Check != nil {
			if err := d.Check(d.Default); err != nil {
				acc.Addf("default", codeRejected, "rejected by the setting's own rule: %s", err.Error())
			}
		}
	}
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// checkOptions records the problems with Options and reports whether they are usable.
func (d Decl) checkOptions(acc *validate.Errors) bool {
	if !d.Kind.valid() {
		return false
	}
	if d.Kind != Enum {
		if len(d.Options) > 0 {
			acc.Addf("options", "unexpected", "only an enum setting has options")
		}
		return true
	}
	ok := true
	if len(d.Options) < 2 {
		acc.Addf("options", codeRequired, "an enum setting needs at least two options")
		ok = false
	}
	seen := make(map[string]struct{}, len(d.Options))
	badName, dup := false, false
	for _, o := range d.Options {
		if !isOption(o) {
			badName = true
		}
		if _, found := seen[o]; found {
			dup = true
		}
		seen[o] = struct{}{}
	}
	if badName {
		acc.Addf("options", codeMalformed, "every option must be lower-case letters and digits, joined by single hyphens or underscores, at most %d bytes", maxIdentBytes)
		ok = false
	}
	if dup {
		acc.Addf("options", "duplicate", "options must not repeat")
		ok = false
	}
	return ok
}

// Area is one owner of settings: a module (Core false) or a core area (Core true).
type Area struct {
	Name  string // a module name or a core area name; "regional" is this package's own
	Core  bool
	Decls []Decl
	// Terms are the words this area lets a regional rename (D57). Keys are one flat
	// namespace across areas and CoreTerms.
	Terms []TermDecl
	// Check is an optional cross-field rule over the area's effective values (every
	// declared key, defaults filled in, bare keys; a Secret's value is always ""). Pure.
	// Field names it returns are bare keys and are prefixed with "<area>." by Set.
	Check func(values map[string]string) error `json:"-"`
}

// Validate fails errs.Invalid joining one validate.FieldError per problem: the name is not
// an ident ("name"), a Decl is invalid ("decls[i].<field>") or two Decls share a key
// ("decls[i].key"), a TermDecl is invalid ("terms[i].<field>", including key "reserved")
// or two TermDecls share a key ("terms[i].key", "duplicate").
func (a Area) Validate() error {
	acc := validate.Errors{Op: "settings.Area.Validate"}
	if len(a.Name) > maxAreaBytes || !areaRE.MatchString(a.Name) {
		acc.Addf("name", codeMalformed, "must be lower-case letters and digits, starting with a letter, at most %d bytes", maxAreaBytes)
	}
	seen := make(map[string]struct{}, len(a.Decls))
	for i, d := range a.Decls {
		prefix := fmt.Sprintf("decls[%d]", i)
		for _, fe := range validate.Fields(d.Validate()) {
			acc.Addf(prefix+"."+fe.Field, fe.Code, "%s", fe.Reason)
		}
		if _, dup := seen[d.Key]; dup {
			acc.Addf(prefix+".key", "duplicate", "declared more than once")
		}
		seen[d.Key] = struct{}{}
	}
	seenTerm := make(map[string]struct{}, len(a.Terms))
	for i, t := range a.Terms {
		prefix := fmt.Sprintf("terms[%d]", i)
		for _, fe := range validate.Fields(t.Validate()) {
			acc.Addf(prefix+"."+fe.Field, fe.Code, "%s", fe.Reason)
		}
		if _, dup := seenTerm[t.Key]; dup {
			acc.Addf(prefix+".key", codeDuplicate, "declared more than once")
		}
		seenTerm[t.Key] = struct{}{}
	}
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// maxTermKeyBytes is the longest term key.
const maxTermKeyBytes = 32

// TermDecl declares a word a regional may rename (D57). Key is the term's identity
// everywhere: core.terminology.term, TermsDocument, the shell's {term_<key>[_plural|_title|
// _plural_title]} and notify's {{term "<key>"}}. A language without an entry in Defaults
// uses the "en" entry.
type TermDecl struct {
	Key      string           // [a-z][a-z0-9]*(_[a-z0-9]+)*, at most 32 bytes; not in CoreTerms
	Defaults map[string]Forms // per language in Languages; "en" required, every form set
}

// Validate fails errs.Invalid joining one validate.FieldError per problem: key "format"
// (not an ident, or over 32 bytes) or "reserved" (a CoreTerms name); defaults.<lang>
// "unknown" for a language not in Languages; defaults.en "required" when "en" is missing;
// defaults.<lang>.<form> "required", "too_long {max}", "control_chars" or "malformed" for
// a form that is empty after trimming, over MaxTermBytes, with a control character or not
// UTF-8. Messages never echo a value.
func (d TermDecl) Validate() error {
	acc := validate.Errors{Op: "settings.TermDecl.Validate"}
	switch {
	case len(d.Key) > maxTermKeyBytes || !keyRE.MatchString(d.Key):
		acc.Addf("key", codeFormat, "must be lower-case letters and digits in snake_case, starting with a letter, at most %d bytes", maxTermKeyBytes)
	case slices.Contains(CoreTerms, d.Key):
		acc.Addf("key", codeReserved, "is a core term; a word two parts need is declared once, in CoreTerms")
	}
	if _, ok := d.Defaults[fallbackLanguage]; !ok {
		acc.Addf("defaults."+fallbackLanguage, codeRequired, "the %s defaults are required", fallbackLanguage)
	}
	langs := make([]string, 0, len(d.Defaults))
	for lang := range d.Defaults {
		langs = append(langs, lang)
	}
	slices.Sort(langs)
	for _, lang := range langs {
		if !validLanguage(lang) {
			acc.Addf("defaults."+safeField(lang), codeUnknown, "no such language")
			continue
		}
		f := d.Defaults[lang]
		for _, form := range formNames {
			v := f.formValue(form)
			if pr := termShape(strings.TrimSpace(v)); pr.code != "" {
				acc.Add(fieldError("defaults."+lang+"."+form, pr.code, pr.params, pr.reason))
			}
		}
	}
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// ---- value grammar -----------------------------------------------------------------------

// problem is one reason a text value does not fit its Decl; code "" means none.
type problem struct {
	code   string
	params map[string]string
	reason string
}

// shapeOf checks v (already trimmed where the kind trims) against d's kind and options.
// The reason never echoes v.
func shapeOf(d Decl, v string) problem {
	switch d.Kind {
	case Bool:
		if v != "true" && v != "false" {
			return problem{code: codeMalformed, reason: `must be "true" or "false"`}
		}
	case Int:
		if _, err := strconv.ParseInt(v, 10, 64); err != nil {
			return problem{code: codeMalformed, reason: "must be a base-10 integer"}
		}
	case Duration:
		if _, err := time.ParseDuration(v); err != nil {
			return problem{code: codeMalformed, reason: `must be a duration such as "72h"`}
		}
	case Text, Secret:
		return textShape(v, MaxTextBytes, true)
	case Enum:
		for _, o := range d.Options {
			if o == v {
				return problem{}
			}
		}
		return problem{
			code: codeOneOf, params: map[string]string{"options": strings.Join(d.Options, ", ")},
			reason: "must be one of the options",
		}
	}
	return problem{}
}

// textShape checks UTF-8, control characters (newline and tab pass when multiline) and
// the byte limit.
func textShape(v string, limit int, multiline bool) problem {
	switch {
	case !utf8.ValidString(v):
		return problem{code: codeMalformed, reason: "must be valid UTF-8"}
	case strings.IndexFunc(v, func(r rune) bool {
		if multiline && (r == '\n' || r == '\t') {
			return false
		}
		return unicode.IsControl(r)
	}) >= 0:
		return problem{code: codeControl, reason: "must not contain control characters"}
	case len(v) > limit:
		return tooLong(limit)
	}
	return problem{}
}

func tooLong(limit int) problem {
	return problem{
		code: codeTooLong, params: map[string]string{"max": strconv.Itoa(limit)},
		reason: fmt.Sprintf("must be at most %d bytes", limit),
	}
}

// sameValue reports whether a and b, both valid for d, are one value: numbers and
// durations compare by what they parse to, the rest by text.
func sameValue(d Decl, a, b string) bool {
	switch d.Kind {
	case Int:
		x, ex := strconv.ParseInt(a, 10, 64)
		y, ey := strconv.ParseInt(b, 10, 64)
		return ex == nil && ey == nil && x == y
	case Duration:
		x, ex := time.ParseDuration(a)
		y, ey := time.ParseDuration(b)
		return ex == nil && ey == nil && x == y
	default:
		return a == b
	}
}

// canonical returns the text form stored for v: an Int is written in plain base 10 and
// everything else as given.
func canonical(d Decl, v string) string {
	if d.Kind == Int {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return strconv.FormatInt(n, 10)
		}
	}
	return v
}

// fieldError returns an errs.Invalid error carrying one FieldError.
func fieldError(field, code string, params map[string]string, reason string) error {
	return errs.E("", errs.Invalid, &validate.FieldError{Field: field, Code: code, Params: params, Reason: reason})
}

// secretRejected is the fixed reason given for a Secret's Check failure: the Check saw the
// plaintext, so nothing it says may reach a response, a span or a log.
const secretRejected = "the value was rejected"

// checkError converts a Check's error into field errors under field: FieldErrors keep
// their code, params and reason; any other error becomes code "rejected" with its text. With
// secretValue the reasons are replaced by a fixed text and only the codes and params stay,
// because a Check of a Secret saw the plaintext and its message may echo it.
func checkError(field string, err error, secretValue bool) []error {
	fes := validate.Fields(err)
	if len(fes) == 0 {
		reason := secretRejected
		if !secretValue {
			reason = err.Error()
		}
		return []error{fieldError(field, codeRejected, nil, reason)}
	}
	out := make([]error, len(fes))
	for i, fe := range fes {
		reason := fe.Reason
		if secretValue {
			reason = secretRejected
		}
		out[i] = fieldError(field, fe.Code, fe.Params, reason)
	}
	return out
}
