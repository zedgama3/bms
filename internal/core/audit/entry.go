package audit

import (
	"strconv"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// Entry is what a writer records. Names, IDs and levels only: never a value of a personal
// field (a legal name, an e-mail address) anywhere in it, Reason included.
type Entry struct {
	Action  Action
	Outcome Outcome
	Actor   Actor             // zero: resolved from ctx (decision 6)
	Subject model.PersonID    // the person the record is about; zero when none
	Record  Ref               // the record acted on; zero when none
	Fields  []string          // field names viewed or changed, as the owning spec names them ("legal_name"); "[a-z][a-z0-9_]*", unique, at most MaxFields
	Level   model.Sensitivity // the highest level among Fields; required iff Fields is non-empty, zero otherwise
	Reason  string            // what the person typed when a workflow asks why (break-glass, emergency lookup); at most MaxReasonBytes; valid UTF-8, no control characters
}

// Validate fails errs.Invalid joining a validate.FieldError per problem (fields "action",
// "outcome", "actor.*", "subject", "record", "fields", "level", "reason"). It does not
// check the area against a scope: Recorder.Record does. A caller-set Actor.Titles fails
// "actor.titles": only the recorder sets them. Messages never echo a value.
func (e Entry) Validate() error {
	v := validate.Errors{Op: "audit.Entry.Validate"}
	addAll(&v, "", e.Action.Validate())
	addAll(&v, "", e.Outcome.Validate())
	addAll(&v, "actor.", e.Actor.Validate())
	if len(e.Actor.Titles) > 0 {
		v.Addf("actor.titles", codeMismatch, "are set by the recorder, not by the caller")
	}
	addAll(&v, "record.", e.Record.Validate())
	e.validateFields(&v)
	e.validateReason(&v)
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// addAll copies the field errors of err into v under prefix. Params are kept.
func addAll(v *validate.Errors, prefix string, err error) {
	for _, fe := range validate.Fields(err) {
		v.Add(withField(fe, prefix+fe.Field))
	}
}

// withField returns an errs.Invalid error carrying fe under a new field name.
func withField(fe *validate.FieldError, field string) error {
	return errs.E("", errs.Invalid, &validate.FieldError{Field: field, Code: fe.Code, Params: fe.Params, Reason: fe.Reason})
}

func (e Entry) validateFields(v *validate.Errors) {
	if len(e.Fields) > MaxFields {
		v.Addf("fields", codeTooMany, "at most %d field names", MaxFields)
	}
	seen := make(map[string]struct{}, len(e.Fields))
	badName, dup := false, false
	for _, f := range e.Fields {
		if len(f) > MaxFieldBytes || !fieldRE.MatchString(f) {
			badName = true
			continue
		}
		if _, ok := seen[f]; ok {
			dup = true
		}
		seen[f] = struct{}{}
	}
	if badName {
		v.Addf("fields", codeMalformed, "each name must match [a-z][a-z0-9_]* and be at most %d bytes", MaxFieldBytes)
	}
	if dup {
		v.Addf("fields", codeDuplicate, "names must be unique")
	}
	switch {
	case len(e.Fields) > 0 && e.Level == 0:
		v.Addf("level", validate.CodeRequired, "must be set when fields are listed")
	case len(e.Fields) > 0:
		if e.Level.Validate() != nil {
			v.Addf("level", validate.CodeOneOf, "must be one of: public, internal, personal, sensitive")
		}
	case e.Level != 0:
		v.Addf("level", codeMismatch, "must be empty when no fields are listed")
	}
}

func (e Entry) validateReason(v *validate.Errors) {
	switch {
	case len(e.Reason) > MaxReasonBytes:
		v.Addf("reason", codeTooLong, "must be at most %d bytes", MaxReasonBytes)
	case !utf8.ValidString(e.Reason):
		v.Addf("reason", codeMalformed, "must be valid UTF-8")
	case hasControl(e.Reason):
		v.Addf("reason", codeMalformed, "must not contain control characters")
	}
}

func hasControl(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

type record struct{}

// RecordID identifies one stored entry.
type RecordID = model.ID[record]

// Record is one stored entry: the Entry as written plus what the recorder stamped.
type Record struct {
	ID       RecordID
	At       time.Time // when it was recorded (the recording copy's clock)
	Instance string    // which copy recorded it (config instance.name)
	TraceID  string    // 32 lower-case hex, or "" when there was no span
	Spooled  bool      // came through a spool (At is the original time)
	Entry              // Actor fully resolved (Kind never zero; Titles filled when known)
}

// validate checks a stored record: the stamped fields and the entry (whose Actor may now
// carry titles). Used by the importer on every spool line.
func (r Record) validate() error {
	v := validate.Errors{Op: "audit.Record.validate"}
	if r.ID.IsZero() {
		v.Addf("id", validate.CodeRequired, "must be set")
	}
	if r.At.IsZero() {
		v.Addf("at", validate.CodeRequired, "must be set")
	}
	if !instanceRE.MatchString(r.Instance) || len(r.Instance) > maxInstanceBytes {
		v.Addf("instance", codeMalformed, "must match the instance name grammar")
	}
	if r.TraceID != "" && !traceIDRE.MatchString(r.TraceID) {
		v.Addf("trace_id", codeMalformed, "must be 32 lower-case hex digits")
	}
	if !r.Actor.Kind.valid() {
		v.Addf("actor.kind", validate.CodeRequired, "must be set")
	}
	addAll(&v, "actor.", r.Actor.Validate())
	bare := r.Entry
	bare.Actor.Titles = nil
	addAll(&v, "", bare.Validate())
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// itoa is strconv.Itoa, for index-prefixed field names.
func itoa(i int) string { return strconv.Itoa(i) }
