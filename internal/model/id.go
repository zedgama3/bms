package model

import (
	"crypto/rand"
	"encoding/hex"
	"errors"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// Entity marks what an ID identifies. It is used only as a type argument: declare an empty
// struct per record kind and alias ID[thatStruct]. Distinct markers make distinct ID types,
// so the compiler rejects a PersonID where a DepartmentID is expected.
type Entity interface{ ~struct{} }

// ID is the identifier of one record: a 128-bit UUID (RFC 9562). The zero value means
// "no record" and is how an optional reference is left empty. The underlying type is
// [16]byte so the storage seam can convert it to and from a PostgreSQL uuid column without
// model knowing any driver.
type ID[E Entity] [16]byte

// The core's shared keys (D19). Modules refer to people, departments and events by these
// and never by name. A person is not an account: PersonID and AccountID are different
// types, and modules use PersonID only.
type (
	// PersonID identifies a person (a profile), not a login.
	PersonID = ID[person]
	// AccountID identifies a login account.
	AccountID = ID[account]
	// DepartmentID identifies a department.
	DepartmentID = ID[department]
	// EventID identifies an event.
	EventID = ID[event]
)

type (
	person     struct{}
	account    struct{}
	department struct{}
	event      struct{}
)

// Codes set by this file, for the web shell to translate.
const (
	codeUUID = "uuid" // ParseID, UnmarshalText
)

const uuidReason = "must be a UUID in the form xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"

// NewID returns a fresh version-4 UUID as T, from crypto/rand. It cannot fail: on Go 1.24+
// crypto/rand.Read never returns an error. Safe for concurrent use.
//
//	id := model.NewID[model.PersonID]()
func NewID[T ~[16]byte]() T {
	var b [16]byte
	// Go 1.24+: Read never returns an error; it crashes the program if the OS source fails.
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40 // version 4
	b[8] = b[8]&0x3f | 0x80 // variant 10
	return T(b)
}

// ParseID parses the canonical 36-character form 8-4-4-4-12 of hexadecimal digits, in
// either case, and returns it as T. It accepts no other form (no braces, no urn: prefix,
// no 32-digit form, no surrounding space). It fails with errs.Invalid, and the error never
// echoes s. The nil UUID parses; ID.Validate rejects it.
//
//	id, err := model.ParseID[model.EventID](r.PathValue("event"))
func ParseID[T ~[16]byte](s string) (T, error) {
	var zero T
	b, ok := parseUUID(s)
	if !ok {
		return zero, fieldErr("model.ParseID", "id", codeUUID, nil, uuidReason)
	}
	return T(b), nil
}

// parseUUID decodes the canonical form, reporting whether s was exactly that.
func parseUUID(s string) ([16]byte, bool) {
	var out [16]byte
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return out, false
	}
	// Byte offsets of each group in s, and where it lands in out.
	groups := [...]struct{ from, to, n int }{{0, 0, 4}, {9, 4, 2}, {14, 6, 2}, {19, 8, 2}, {24, 10, 6}}
	for _, g := range groups {
		if _, err := hex.Decode(out[g.to:g.to+g.n], []byte(s[g.from:g.from+2*g.n])); err != nil {
			return [16]byte{}, false
		}
	}
	return out, true
}

// String returns the canonical lower-case 36-character form. Cannot fail.
func (id ID[E]) String() string {
	var buf [36]byte
	hex.Encode(buf[0:8], id[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], id[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], id[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], id[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:36], id[10:16])
	return string(buf[:])
}

// IsZero reports whether id is the zero (nil) UUID, that is, no record. Cannot fail.
func (id ID[E]) IsZero() bool { return id == ID[E]{} }

// Validate fails with errs.Invalid if id is zero. Any other 128-bit value is accepted, so
// IDs imported from another system (Q-IMPORT, Q-TRUST) need not be version 4.
func (id ID[E]) Validate() error {
	if id.IsZero() {
		return fieldErr("model.ID.Validate", "id", validate.CodeRequired, nil, "must be set")
	}
	return nil
}

// MarshalText returns String() as bytes and a nil error, so encoding/json emits the
// canonical string. Cannot fail.
func (id ID[E]) MarshalText() ([]byte, error) { return []byte(id.String()), nil }

// UnmarshalText parses text as ParseID does and stores the result. On failure *id is
// unchanged. It fails with errs.Invalid for bad text and errs.Internal for a nil receiver.
// encoding/json never calls it for null, so a JSON null leaves the field unchanged (zero
// in a fresh struct); use the omitzero tag to leave a zero ID out of a response.
func (id *ID[E]) UnmarshalText(text []byte) error {
	if id == nil {
		return errs.E("model.ID.UnmarshalText", errs.Internal, errors.New("nil receiver"))
	}
	b, ok := parseUUID(string(text))
	if !ok {
		return fieldErr("model.ID.UnmarshalText", "id", codeUUID, nil, uuidReason)
	}
	*id = ID[E](b)
	return nil
}
