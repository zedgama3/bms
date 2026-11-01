package audit

import (
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// Query selects entries. Every field is optional; empty means "any". Results are ordered
// newest first (At desc, ID desc) and paged by keyset.
type Query struct {
	Actor         model.PersonID
	Subject       model.PersonID
	Record        Ref
	Area          string          // Action.Area() == Area
	Action        Action          // exact action
	Within        model.TimeRange // [Start, End); zero means any time
	SuperuserOnly bool
	After         Cursor // Page.Next of the previous page
	Limit         int    // 0 = DefaultPageSize; at most MaxPageSize
}

// Validate fails errs.Invalid with field errors ("record", "area", "action", "within",
// "after", "limit"). Messages never echo a value.
func (q Query) Validate() error {
	v := validate.Errors{Op: "audit.Query.Validate"}
	if q.Record.Validate() != nil {
		v.Addf("record", codeMalformed, "must be a kind and an id together, the kind as <area>.<ident>")
	}
	if q.Area != "" && (len(q.Area) > 32 || !searchAreaRE.MatchString(q.Area)) {
		v.Addf("area", codeMalformed, "must be a lower-case area name")
	}
	if q.Action != "" {
		if q.Action.Validate() != nil {
			v.Addf("action", codeMalformed, "must be <area>.<ident>(.<ident>)*")
		} else if q.Area != "" && q.Action.Area() != q.Area {
			v.Addf("action", codeMismatch, "must belong to the area filter")
		}
	}
	if q.Within != (model.TimeRange{}) && q.Within.Validate() != nil {
		v.Addf("within", validate.CodeRange, "must have a start before its end, both set")
	}
	if q.After != "" {
		if _, _, err := q.After.Position(); err != nil {
			v.Addf("after", codeMalformed, "is not a cursor from a previous page")
		}
	}
	if q.Limit < 0 || q.Limit > MaxPageSize {
		v.Addf("limit", validate.CodeRange, "must be between 0 and %d", MaxPageSize)
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// PageSize is Limit with the default applied. Pure.
func (q Query) PageSize() int {
	if q.Limit == 0 {
		return DefaultPageSize
	}
	return q.Limit
}

// Cursor is an opaque, URL-safe continuation token (At and ID of the last record).
type Cursor string

// NewCursor returns the continuation token for the record at (at, id): what auditlog hands
// back as Page.Next. Pure.
func NewCursor(at time.Time, id RecordID) Cursor {
	raw := strconv.FormatInt(at.UnixMicro(), 10) + "." + id.String()
	return Cursor(base64.RawURLEncoding.EncodeToString([]byte(raw)))
}

// Position returns the cursor's position. The error says nothing about the input.
func (c Cursor) Position() (time.Time, RecordID, error) {
	raw, err := base64.RawURLEncoding.DecodeString(string(c))
	if err != nil {
		return time.Time{}, RecordID{}, errors.New("malformed cursor")
	}
	micro, id, ok := strings.Cut(string(raw), ".")
	if !ok {
		return time.Time{}, RecordID{}, errors.New("malformed cursor")
	}
	n, err := strconv.ParseInt(micro, 10, 64)
	if err != nil {
		return time.Time{}, RecordID{}, errors.New("malformed cursor")
	}
	rid, err := model.ParseID[RecordID](id)
	if err != nil {
		return time.Time{}, RecordID{}, errors.New("malformed cursor")
	}
	return time.UnixMicro(n).UTC(), rid, nil
}

// Page is one page of results. Next is "" on the last page.
type Page struct {
	Records []Record
	Next    Cursor
}
