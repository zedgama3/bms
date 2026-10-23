package vol

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// The field codes this module adds to the shell's vocabulary (validate has required,
// range, one_of and malformed). The web shell translates a code under field.<code>.
const (
	CodeTooLong      = "too_long"      // params: max
	CodeControlChars = "control_chars" // a control character in free text
	CodeTooMany      = "too_many"      // params: max
	CodeUnknown      = "unknown"       // a reference to something that does not exist
	CodeSelf         = "self"          // an action aimed at oneself
	CodeScope        = "scope"         // a reference to the wrong department
	CodeHidden       = "hidden"        // the field is not changeable here
	CodeDuplicate    = "duplicate"     // a repeated item
)

// Conflict codes (errs.Conflict wrapping a validate.FieldError with one of these).
const (
	ConflictTaken         = "taken"
	ConflictArchived      = "archived"
	ConflictCanceled      = "canceled"
	ConflictClosed        = "closed"
	ConflictFull          = "full"
	ConflictOverlap       = "overlap" // params: shift
	ConflictUnqualified   = "unqualified"
	ConflictNotMember     = "not_member"
	ConflictStarted       = "started"
	ConflictEnded         = "ended"
	ConflictCutoff        = "cutoff"
	ConflictDuplicate     = "duplicate"
	ConflictStatus        = "status"
	ConflictInUse         = "in_use"
	ConflictPendingExists = "pending_exists"
	ConflictUnnamed       = "unnamed" // the person booking has no playa name yet
)

// FieldProblem returns an errs.Invalid error carrying one validate.FieldError.
func FieldProblem(op, field, code, reason string, params map[string]string) error {
	return errs.E(op, errs.Invalid, &validate.FieldError{Field: field, Code: code, Params: params, Reason: reason})
}

// ConflictProblem returns an errs.Conflict error carrying one validate.FieldError, so the
// shell can show the code under the field.
func ConflictProblem(op, field, code, reason string, params map[string]string) error {
	return errs.E(op, errs.Conflict, &validate.FieldError{Field: field, Code: code, Params: params, Reason: reason})
}

func maxParam(n int) map[string]string { return map[string]string{"max": fmt.Sprint(n)} }

// checkText records the problems with a free-text field: trimmed length within [lo, hi]
// bytes, valid UTF-8, no control characters (a newline and a tab are allowed in multi-line
// text when multiline is true).
func checkText(acc *validate.Errors, field, v string, lo, hi int, multiline bool) {
	t := strings.TrimSpace(v)
	switch {
	case !utf8.ValidString(t):
		acc.Addf(field, validate.CodeMalformed, "must be valid UTF-8")
		return
	case len(t) < lo:
		acc.Addf(field, validate.CodeRequired, "is required")
		return
	case len(t) > hi:
		acc.Add(FieldProblem("", field, CodeTooLong, fmt.Sprintf("must be at most %d bytes", hi), maxParam(hi)))
		return
	}
	bad := strings.ContainsFunc(t, func(r rune) bool {
		if multiline && (r == '\n' || r == '\t' || r == '\r') {
			return false
		}
		return unicode.IsControl(r)
	})
	if bad {
		acc.Addf(field, CodeControlChars, "must not contain control characters")
	}
}

// checkDate records an impossible date under field.
func checkDate(acc *validate.Errors, field string, d Date) {
	if d.Validate() != nil {
		acc.Addf(field, validate.CodeMalformed, "must be a real calendar date")
	}
}

// ValidZone reports whether name is an IANA zone time.LoadLocation knows.
func ValidZone(name string) bool {
	if name == "" || len(name) > 64 || name == "Local" {
		return false
	}
	_, err := time.LoadLocation(name)
	return err == nil
}

// ConflictCode returns the code of the first validate.FieldError in err, "" when it has none.
func ConflictCode(err error) string {
	if fes := validate.Fields(err); len(fes) > 0 {
		return fes[0].Code
	}
	return ""
}
