// Package rows holds the row types people's service and its PostgreSQL store share. It is a
// leaf: it imports model only, because people imports postgres to build its store and so
// postgres cannot import people. Statuses and kinds travel as the words the tables hold,
// purposes as plain strings.
package rows

import (
	"time"

	"github.com/zedgama3/bms/internal/model"
)

type (
	contact struct{}
)

// ContactID identifies an emergency contact. people re-exports it under the same name.
type ContactID = model.ID[contact]

// Person is a row of core.person. ContactEmail is stored as entered; "" is NULL. A zero
// CreatedBy is NULL.
type Person struct {
	ID           model.PersonID
	PlayaName    string
	Pronouns     string
	Language     string
	LegalName    string
	Phone        string
	ContactEmail string
	// PlayaNameKey is people.NameKey(PlayaName) as the service computed it. Writes carry
	// it with the name; only GetForUpdate reads it back (the permanence check compares
	// against the stored key). Never logged: a key is a name.
	PlayaNameKey string
	CreatedBy    model.PersonID
	CreatedAt    time.Time
	UpdatedAt    time.Time
	// NamePromptedAt is core.person.name_prompted_at: when the person was last asked for a
	// playa name (decisions 34, 35). Zero is NULL. Only ExportPerson reads it; no other
	// statement selects, writes or clears it except Store.MarkNamePrompted.
	NamePromptedAt time.Time
}

// Contact is a row of core.person_emergency_contact. Position is 1-based and gapless after
// every store write.
type Contact struct {
	ID           ContactID
	Person       model.PersonID
	Name         string
	Phone        string
	Relationship string
	Position     int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Consent is a row of core.person_consent. A zero RecordedBy is NULL.
type Consent struct {
	Person     model.PersonID
	Purpose    string
	Granted    bool
	RecordedBy model.PersonID
	ChangedAt  time.Time
}

// SearchQuery is one search. Q is the text as the person typed it, trimmed; the SQL
// compares case-insensitively and treats it as a substring, never as a pattern. Limit is the
// number of rows wanted (the store asks for no more). LegalName, Phone and ContactEmail
// select the personal columns that also count; the playa name always does.
type SearchQuery struct {
	Q            string
	Limit        int
	LegalName    bool
	Phone        bool
	ContactEmail bool
}

// Match is one search result. Legal, Phone and Email say which personal columns matched Q
// (always false for a column the query did not select).
type Match struct {
	ID        model.PersonID
	PlayaName string
	Pronouns  string
	Legal     bool
	Phone     bool
	Email     bool
}

// PersonPatch is a partial write of a person: a nil field is left as it is on an existing
// row and is empty on a new one; a pointer to "" clears the field. ContactEmail "" stores
// NULL.
type PersonPatch struct {
	ID        model.PersonID
	PlayaName *string
	// PlayaNameKey is set together with PlayaName (people.NameKey of it) and nil when
	// PlayaName is.
	PlayaNameKey *string
	Pronouns     *string
	Language     *string
	LegalName    *string
	Phone        *string
	ContactEmail *string
}
