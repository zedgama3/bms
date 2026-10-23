package vol

import (
	"encoding/base64"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// DefaultLimit is Page.Limit when the caller gives none.
const DefaultLimit = 100

// Cursor is an opaque position in one of the three long lists (ShiftList, ScheduleList,
// MemberViewList): base64url of "<sort key RFC3339Nano UTC>|<id>", the sort key being starts
// for shifts and schedules and created_at for members. Clients pass it back unread; a cursor
// that does not decode, or whose ID is not a valid model.ID, is errs.Invalid (code malformed)
// on "after". The zero Cursor is the first page (and, as Next, "no more").
type Cursor string

// IsZero reports whether c is empty (the first page, or no next page). Cannot fail.
func (c Cursor) IsZero() bool { return c == "" }

// NewCursor returns the cursor that resumes after the row with sort key key and ID id.
// Cannot fail.
func NewCursor[T ~[16]byte](key time.Time, id T) Cursor {
	raw := key.UTC().Format(time.RFC3339Nano) + "|" + model.ID[struct{}](id).String()
	return Cursor(base64.RawURLEncoding.EncodeToString([]byte(raw)))
}

// DecodeCursor splits c into its sort key and ID. A zero Cursor yields a zero key and ID with no
// error. Fails errs.Invalid (code malformed, field "after") when c does not decode or its ID
// is not a valid model.ID.
func DecodeCursor[T ~[16]byte](c Cursor) (time.Time, T, error) {
	var zero T
	if c.IsZero() {
		return time.Time{}, zero, nil
	}
	bad := func() (time.Time, T, error) {
		return time.Time{}, zero, FieldProblem("vol.DecodeCursor", "after", validate.CodeMalformed, "is not a cursor this list gave", nil)
	}
	raw, err := base64.RawURLEncoding.DecodeString(string(c))
	if err != nil {
		return bad()
	}
	key, id, found := strings.Cut(string(raw), "|")
	if !found {
		return bad()
	}
	at, err := time.Parse(time.RFC3339Nano, key)
	if err != nil {
		return bad()
	}
	parsed, err := model.ParseID[T](id)
	if err != nil || parsed == zero {
		return bad()
	}
	return at.UTC(), parsed, nil
}

// Page is the paging every long list shares: Limit rows (1..MaxList, default DefaultLimit)
// after the row the cursor names. The service fetches Limit + 1 rows: when the extra row
// exists, Next is the cursor of the last returned row, else it is zero. Next is computed from
// the fetched rows before any filtering done in Go, so a page may be shorter than Limit but a
// cursor never skips a row.
type Page struct {
	Limit int    `json:"limit,omitempty"` // 1..MaxList; 0 = DefaultLimit
	After Cursor `json:"after,omitempty"` // zero = the first page
}

// Size returns the effective page size (DefaultLimit for a zero Limit). Cannot fail.
func (p Page) Size() int {
	if p.Limit == 0 {
		return DefaultLimit
	}
	return p.Limit
}

// Validate fails errs.Invalid on "limit" (range) and "after" (malformed). Cannot otherwise fail.
func (p Page) Validate() error {
	acc := validate.Errors{Op: "vol.Page.Validate"}
	if p.Limit != 0 {
		acc.Add(validate.Range("limit", p.Limit, 1, MaxList))
	}
	if _, _, err := DecodeCursor[[16]byte](p.After); err != nil {
		acc.Addf("after", validate.CodeMalformed, "is not a cursor this list gave")
	}
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}
