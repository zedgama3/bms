package audit

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// Codes set by this package, for the web shell to translate.
const (
	codeMalformed = validate.CodeMalformed
	codeTooLong   = "too_long"
	codeTooMany   = "too_many"
	codeDuplicate = "duplicate"
	codeMismatch  = "mismatch"
)

// identRE is one segment of an action or a record kind (module-contract's ident grammar).
const identPattern = `[a-z][a-z0-9]*(-[a-z0-9]+)*`

var (
	// dottedRE is "<area>.<ident>(.<ident>)*": at least two segments.
	dottedRE = regexp.MustCompile(`^` + identPattern + `(\.` + identPattern + `)+$`)
	// refKindRE is a record kind: the area has no hyphen (core.audit_entry's CHECK is narrower than an action's).
	refKindRE = regexp.MustCompile(`^[a-z][a-z0-9]*(\.` + identPattern + `)+$`)
	// areaRE is a scope's area, as app.Scoped takes it.
	areaRE = regexp.MustCompile(`^[a-z][a-z0-9]{1,31}$`)
	// searchAreaRE is the area filter of a Query: any area an action can start with.
	searchAreaRE = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
	fieldRE      = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	instanceRE   = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
	traceIDRE    = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

func validDotted(s string) bool { return len(s) <= MaxActionBytes && dottedRE.MatchString(s) }

// fieldErr builds an errs.Invalid error carrying one validate.FieldError, for the checks
// that return a single problem.
func fieldErr(op, field, code, format string, args ...any) error {
	v := validate.Errors{Op: op}
	v.Addf(field, code, format, args...)
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// Action names what happened: "<area>.<ident>(.<ident>)*", where area is the module or
// core area that records it and each ident is "[a-z][a-z0-9]*(-[a-z0-9]+)*"; at most
// MaxActionBytes. Convention (not enforced): past tense for changes
// ("identity.account.disabled"), "read" for views ("people.profile.read"), and a
// distinct action per failure the reviewer cares about ("identity.login-failed").
type Action string

// Validate fails errs.Invalid (field "action") on bad grammar; never echoes the value.
func (a Action) Validate() error {
	if validDotted(string(a)) {
		return nil
	}
	return fieldErr("audit.Action.Validate", "action", codeMalformed,
		"must be <area>.<ident>(.<ident>)*, lower-case letters, digits and single hyphens, at most %d bytes", MaxActionBytes)
}

// Area returns the text before the first dot ("" if none). Cannot fail.
func (a Action) Area() string {
	area, _, found := strings.Cut(string(a), ".")
	if !found {
		return ""
	}
	return area
}

// Outcome says how the action ended. The zero value is invalid.
type Outcome uint8

// The outcomes.
const (
	Succeeded Outcome = iota + 1 // the action happened
	Denied                       // refused by authorization or policy (a failed login counts as Failed)
	Failed                       // attempted and did not happen for another reason
)

var outcomeNames = [...]string{Succeeded: "succeeded", Denied: "denied", Failed: "failed"}

func (o Outcome) valid() bool { return o >= Succeeded && o <= Failed }

// String returns "succeeded", "denied", "failed" or "Outcome(<n>)". Cannot fail.
func (o Outcome) String() string {
	if o.valid() {
		return outcomeNames[o]
	}
	return "Outcome(" + strconv.Itoa(int(o)) + ")"
}

// Validate fails errs.Invalid (field "outcome") unless o is one of the three.
func (o Outcome) Validate() error {
	if o.valid() {
		return nil
	}
	return fieldErr("audit.Outcome.Validate", "outcome", validate.CodeOneOf, "must be one of: succeeded, denied, failed")
}

// ParseOutcome returns the outcome named by s, exactly as String spells it. Fails
// errs.Invalid; the error never echoes s.
func ParseOutcome(s string) (Outcome, error) {
	for o := Succeeded; o <= Failed; o++ {
		if outcomeNames[o] == s {
			return o, nil
		}
	}
	return 0, fieldErr("audit.ParseOutcome", "outcome", validate.CodeOneOf, "must be one of: succeeded, denied, failed")
}

// ActorKind says what kind of actor acted. The zero value means "resolve from ctx".
type ActorKind uint8

// The actor kinds.
const (
	ActorPerson    ActorKind = iota + 1 // a signed-in person (Person set)
	ActorAnonymous                      // a request with no session (failed login, public route)
	ActorOperator                       // a bms command run on the host (create-superuser, export, delete)
	ActorSystem                         // a signal handler or a job
)

var actorKindNames = [...]string{
	ActorPerson: "person", ActorAnonymous: "anonymous", ActorOperator: "operator", ActorSystem: "system",
}

func (k ActorKind) valid() bool { return k >= ActorPerson && k <= ActorSystem }

// String returns "person", "anonymous", "operator", "system", "" for the zero value, or
// "ActorKind(<n>)". Cannot fail.
func (k ActorKind) String() string {
	switch {
	case k.valid():
		return actorKindNames[k]
	case k == 0:
		return ""
	default:
		return "ActorKind(" + strconv.Itoa(int(k)) + ")"
	}
}

func parseActorKind(s string) (ActorKind, bool) {
	for k := ActorPerson; k <= ActorSystem; k++ {
		if actorKindNames[k] == s {
			return k, true
		}
	}
	return 0, false
}

// Actor is who acted. A writer sets Kind, Person and Superuser only when it knows better
// than the request context (login; commands); it never sets Titles (the recorder does,
// through TitleSource; a caller-set Titles fails Invalid in Entry.Validate).
type Actor struct {
	Kind      ActorKind
	Person    model.PersonID // required iff Kind == ActorPerson
	Superuser bool           // only with ActorPerson: the break-glass account (D23)
	Titles    []string       // role or position titles at that moment (D22); set by the recorder
}

// Validate fails errs.Invalid joining a validate.FieldError per problem (fields "kind",
// "person", "superuser", "titles"): Kind unknown; Person set without ActorPerson or
// missing with it; Superuser without ActorPerson; Titles longer than MaxTitles or an item
// empty or over MaxTitleBytes. The zero Actor is valid (meaning "resolve").
func (a Actor) Validate() error {
	v := validate.Errors{Op: "audit.Actor.Validate"}
	if a.Kind != 0 && !a.Kind.valid() {
		v.Addf("kind", validate.CodeOneOf, "must be one of: person, anonymous, operator, system")
	}
	person := a.Kind == ActorPerson
	switch {
	case person && a.Person.IsZero():
		v.Addf("person", validate.CodeRequired, "must be set for a person")
	case !person && !a.Person.IsZero():
		v.Addf("person", codeMismatch, "may be set only for a person")
	}
	if a.Superuser && !person {
		v.Addf("superuser", codeMismatch, "may be set only for a person")
	}
	if len(a.Titles) > MaxTitles {
		v.Addf("titles", codeTooMany, "at most %d titles", MaxTitles)
	}
	for _, t := range a.Titles {
		if t == "" || len(t) > MaxTitleBytes {
			v.Addf("titles", codeTooLong, "each title must be 1 to %d bytes", MaxTitleBytes)
			break
		}
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// Ref points at the record acted on: Kind is "<area>.<ident>" (the owning spec's table or
// type name, for example "identity.account", "people.person"); ID the record's UUID. The
// zero Ref means "no record" (a failed login for an unknown address, a search).
type Ref struct {
	Kind string
	ID   [16]byte
}

// RefOf builds a Ref from any model.ID. Cannot fail; validation happens in Entry.Validate.
func RefOf[T ~[16]byte](kind string, id T) Ref { return Ref{Kind: kind, ID: [16]byte(id)} }

// IsZero reports Kind == "" && ID == zero. Cannot fail.
func (r Ref) IsZero() bool { return r.Kind == "" && r.ID == [16]byte{} }

// Validate fails errs.Invalid (fields "kind", "id"): Kind without ID, ID without Kind, or
// a Kind outside the "<area>.<ident>" grammar. The zero Ref is valid.
func (r Ref) Validate() error {
	if r.IsZero() {
		return nil
	}
	v := validate.Errors{Op: "audit.Ref.Validate"}
	switch {
	case r.Kind == "":
		v.Addf("kind", validate.CodeRequired, "must be set with an id")
	case len(r.Kind) > MaxActionBytes || !refKindRE.MatchString(r.Kind):
		v.Addf("kind", codeMalformed, "must be <area>.<ident>, lower-case letters, digits and single hyphens, at most %d bytes", MaxActionBytes)
	}
	if r.ID == ([16]byte{}) {
		v.Addf("id", validate.CodeRequired, "must be set with a kind")
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}
