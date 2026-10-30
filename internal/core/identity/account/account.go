// Package account holds the record types identity's service and its PostgreSQL store share.
// It is a leaf: it imports model and lib only, because identity imports postgres to build
// its store and so postgres cannot import identity.
package account

import (
	"time"

	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// Record is the row of core.account as the store and the service share it. The hash is a
// secret.Secret so no log, span or error can print it. A list or an export leaves
// PasswordHash unset: the hash is read only to verify a password.
type Record struct {
	ID                     model.AccountID
	Person                 model.PersonID
	Email                  model.Email
	PasswordHash           secret.Secret // PHC string "$argon2id$v=19$m=19456,t=2,p=1$<salt>$<hash>"
	Superuser              bool
	PasswordChangeRequired bool
	Generation             int32 // session_generation
	DisabledAt             time.Time
	LastLoginAt            time.Time
	PasswordChangedAt      time.Time
	CreatedAt, UpdatedAt   time.Time
}

// Validate is what the service runs before it writes a record. It fails errs.Invalid
// joining a validate.FieldError per problem (fields "id", "person", "email",
// "password_hash", "generation", "password_changed_at", "created_at", "updated_at"). Messages
// never echo a value.
func (r Record) Validate() error {
	v := validate.Errors{Op: "account.Record.Validate"}
	if r.ID.IsZero() {
		v.Addf("id", validate.CodeRequired, "must be set")
	}
	if r.Person.IsZero() {
		v.Addf("person", validate.CodeRequired, "must be set")
	}
	if err := r.Email.Validate(); err != nil {
		v.Addf("email", validate.CodeMalformed, "must be a valid e-mail address")
	}
	if !r.PasswordHash.IsSet() {
		v.Addf("password_hash", validate.CodeRequired, "must be set")
	}
	if r.Generation < 0 {
		v.Addf("generation", validate.CodeRange, "must not be negative")
	}
	if r.PasswordChangedAt.IsZero() {
		v.Addf("password_changed_at", validate.CodeRequired, "must be set")
	}
	if r.CreatedAt.IsZero() {
		v.Addf("created_at", validate.CodeRequired, "must be set")
	}
	if r.UpdatedAt.IsZero() {
		v.Addf("updated_at", validate.CodeRequired, "must be set")
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// State is the per-request read Authenticate makes: enough to decide whether a token still
// stands, and nothing personal.
type State struct {
	Person                 model.PersonID
	Superuser              bool
	PasswordChangeRequired bool
	Generation             int32
	Disabled               bool
}

// Key is a position in the admin list, newest first: the keyset (created_at, id) of the
// last row of a page.
type Key struct {
	CreatedAt time.Time
	ID        model.AccountID
}

// IsZero reports the first page: no position yet.
func (k Key) IsZero() bool { return k.CreatedAt.IsZero() && k.ID.IsZero() }

// Superuser is what the superuser upsert reports: the account it created or reset.
type Superuser struct {
	ID      model.AccountID
	Person  model.PersonID
	Created bool // false: an existing superuser account was reset
}

// Ensured is what the EnsureAccount upsert reports: the account it created or reset.
type Ensured struct {
	ID      model.AccountID
	Person  model.PersonID
	Created bool // false: an existing participant account's password was replaced
}
