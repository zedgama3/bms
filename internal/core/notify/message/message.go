// Package message holds the record types notify's service shares with its postgres store. It
// is a leaf: it imports model and lib only, because notify imports postgres to build its
// store and so postgres cannot import the root.
package message

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

type entity struct{}

// ID identifies one outbox row. notify re-exports it as MessageID.
type ID = model.ID[entity]

// Status is where a row is in its life. The zero value is invalid.
type Status int

// The statuses, in the order of a row's life.
const (
	Queued  Status = iota + 1 // waiting for its due time
	Sending                   // leased by a sender
	Sent                      // delivered; text and address cleared
	Failed                    // gave up; waits for an admin to retry or cancel it
)

// String returns "queued", "sending", "sent", "failed" or "Status(<n>)". Cannot fail.
func (s Status) String() string {
	switch s {
	case Queued:
		return "queued"
	case Sending:
		return "sending"
	case Sent:
		return "sent"
	case Failed:
		return "failed"
	default:
		return fmt.Sprintf("Status(%d)", int(s))
	}
}

// ParseStatus is the inverse of String. Fails errs.Invalid (field "status", code one_of).
func ParseStatus(s string) (Status, error) {
	for st := Queued; st <= Failed; st++ {
		if st.String() == s {
			return st, nil
		}
	}
	v := validate.Errors{Op: "notify.ParseStatus"}
	v.Addf("status", validate.CodeOneOf, "must be one of: queued, sending, sent, failed")
	return 0, v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// Valid reports whether s is one of the four statuses. Cannot fail.
func (s Status) Valid() bool { return s >= Queued && s <= Failed }

// MarshalText implements encoding.TextMarshaler, so a Status is a JSON string and a map key.
// Fails errs.Invalid for an invalid status.
func (s Status) MarshalText() ([]byte, error) {
	if !s.Valid() {
		return nil, errs.E("notify.Status.MarshalText", errs.Invalid, fmt.Errorf("invalid status %d", int(s)))
	}
	return []byte(s.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler. Fails errs.Invalid.
func (s *Status) UnmarshalText(text []byte) error {
	st, err := ParseStatus(strings.TrimSpace(string(text)))
	if err != nil {
		return err
	}
	*s = st
	return nil
}

// Record is one row of core.notify_message. Address, Subject and Body are personal data:
// they are set while the row waits and cleared by the store once it is Sent. A zero Person
// is NULL (an explicit address); a zero time is NULL; an empty Key is NULL.
type Record struct {
	ID            ID
	Owner         string
	Template      string
	Key           string
	Person        model.PersonID
	Address       string // personal
	Language      string
	Subject       string // personal
	Body          string // personal
	Status        Status
	Attempts      int
	NextAttemptAt time.Time
	LeasedUntil   time.Time
	CreatedAt     time.Time
	ExpiresAt     time.Time // text is cleared and the row ends at this time
	SentAt        time.Time
	FailedAt      time.Time
	LastError     string // a token, never text from a server
}

// LogValue logs the ID only: a Record holds an address, a subject and a body. Cannot fail.
func (r Record) LogValue() slog.Value { return slog.StringValue(r.ID.String()) }

// Override is one row of core.notify_template: a settings admin's wording for one language.
type Override struct {
	Name      string
	Language  string
	Subject   string
	Body      string
	UpdatedBy model.PersonID // zero = NULL
	UpdatedAt time.Time
}

// Exported is the part of a row a person's export carries; Subject is empty once the row
// is final.
type Exported struct {
	ID        ID
	Template  string
	Status    Status
	Language  string
	CreatedAt time.Time
	SentAt    time.Time // zero when not sent
	Subject   string
}

// State is what StatusOf reports about a row: enough to say why a retry or a cancel changed
// nothing.
type State struct {
	Status    Status
	ExpiresAt time.Time
	HasText   bool // the row still holds its subject, body and any address
}
