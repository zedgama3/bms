package identity

import (
	"encoding/base64"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/core/identity/account"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// Account is what an account admin (accounts.read) or the person sees. Never the hash.
type Account struct {
	ID                     model.AccountID `json:"id"`
	Person                 model.PersonID  `json:"person_id"`
	Email                  model.Email     `json:"email"` // personal; as entered
	Superuser              bool            `json:"superuser"`
	PasswordChangeRequired bool            `json:"password_change_required"`
	DisabledAt             time.Time       `json:"disabled_at,omitzero"`   // zero when enabled; personal
	LastLoginAt            time.Time       `json:"last_login_at,omitzero"` // zero when never; personal; updated only on a writing copy
	PasswordChangedAt      time.Time       `json:"password_changed_at"`
	CreatedAt              time.Time       `json:"created_at"`
	UpdatedAt              time.Time       `json:"updated_at"`
}

// accountOf copies the record without its hash and its generation.
func accountOf(r account.Record) Account {
	return Account{
		ID: r.ID, Person: r.Person, Email: r.Email, Superuser: r.Superuser,
		PasswordChangeRequired: r.PasswordChangeRequired,
		DisabledAt:             r.DisabledAt, LastLoginAt: r.LastLoginAt, PasswordChangedAt: r.PasswordChangedAt,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

// Validate fails errs.Invalid joining a validate.FieldError per problem (fields "id",
// "person", "email", "created_at", "updated_at", "password_changed_at"). Messages never
// echo a value.
func (a Account) Validate() error {
	v := validate.Errors{Op: "identity.Account.Validate"}
	if a.ID.IsZero() {
		v.Addf("id", codeRequired, "must be set")
	}
	if a.Person.IsZero() {
		v.Addf("person", codeRequired, "must be set")
	}
	if err := a.Email.Validate(); err != nil {
		v.Addf("email", codeMalformed, "must be a valid e-mail address")
	}
	if a.CreatedAt.IsZero() {
		v.Addf("created_at", codeRequired, "must be set")
	}
	if a.UpdatedAt.IsZero() {
		v.Addf("updated_at", codeRequired, "must be set")
	}
	if a.PasswordChangedAt.IsZero() {
		v.Addf("password_changed_at", codeRequired, "must be set")
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// LogValue renders IDs and booleans only, never Email (model.md decision 13).
func (a Account) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("account_id", a.ID.String()),
		slog.String("person_id", a.Person.String()),
		slog.Bool("superuser", a.Superuser),
		slog.Bool("disabled", !a.DisabledAt.IsZero()),
	)
}

// Credentials is the body of POST /sign-in and POST /sign-up.
type Credentials struct {
	Email    model.Email `json:"email"`
	Password string      `json:"password"`
}

// LogValue hides both fields, so a Credentials passed to a logger by mistake prints
// nothing personal.
func (Credentials) LogValue() slog.Value { return slog.StringValue(secret.Redacted) }

// PasswordChange is the body of POST /password.
type PasswordChange struct {
	Current string `json:"current_password"`
	New     string `json:"new_password"`
}

// LogValue hides both passwords.
func (PasswordChange) LogValue() slog.Value { return slog.StringValue(secret.Redacted) }

// AccountQuery selects accounts for the admin list. Email set: exactly that address
// (canonical match; zero or one result; After and Limit ignored). Otherwise newest first,
// keyset-paged on (created_at, id).
type AccountQuery struct {
	Email model.Email
	After Cursor
	Limit int // 0 = DefaultPageSize; at most MaxPageSize
}

// Validate fails errs.Invalid joining a validate.FieldError per problem: "email" (set but
// invalid; then After and Limit are not checked), "after" (not a cursor this service made),
// "limit" (negative or over MaxPageSize).
func (q AccountQuery) Validate() error {
	v := validate.Errors{Op: "identity.AccountQuery.Validate"}
	if q.Email != "" {
		// An address query returns zero or one account: After and Limit are ignored.
		if err := q.Email.Validate(); err != nil {
			v.Addf("email", codeMalformed, "must be a valid e-mail address")
		}
		return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
	}
	if q.After != "" {
		if _, err := q.After.key(); err != nil {
			v.Addf("after", codeMalformed, "is not a cursor from a previous page")
		}
	}
	if q.Limit < 0 || q.Limit > MaxPageSize {
		v.Add(validate.Range("limit", q.Limit, 0, MaxPageSize))
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// pageSize is Limit with the default applied.
func (q AccountQuery) pageSize() int {
	if q.Limit == 0 {
		return DefaultPageSize
	}
	return q.Limit
}

// Cursor is an opaque, URL-safe continuation token.
type Cursor string

// cursorOf encodes a list position: the creation time in microseconds (PostgreSQL's
// precision) and the account ID.
func cursorOf(k account.Key) Cursor {
	raw := strconv.FormatInt(k.CreatedAt.UnixMicro(), 10) + ":" + k.ID.String()
	return Cursor(base64.RawURLEncoding.EncodeToString([]byte(raw)))
}

func (c Cursor) key() (account.Key, error) {
	const op = "identity.Cursor.key"
	raw, err := base64.RawURLEncoding.Strict().DecodeString(string(c))
	if err != nil {
		return account.Key{}, errs.E(op, errs.Invalid, err)
	}
	ts, id, ok := strings.Cut(string(raw), ":")
	if !ok {
		return account.Key{}, errs.E(op, errs.Invalid, errBadCursor)
	}
	micro, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return account.Key{}, errs.E(op, errs.Invalid, errBadCursor)
	}
	aid, err := model.ParseID[model.AccountID](id)
	if err != nil || aid.IsZero() {
		return account.Key{}, errs.E(op, errs.Invalid, errBadCursor)
	}
	return account.Key{CreatedAt: time.UnixMicro(micro).UTC(), ID: aid}, nil
}

// AccountPage is one page of the admin list.
type AccountPage struct {
	Accounts []Account `json:"accounts"`
	Next     Cursor    `json:"next,omitempty"` // "" on the last page
}

// SessionInfo is the body of GET /session.
type SessionInfo struct {
	SignedIn               bool            `json:"signed_in"`
	PersonID               model.PersonID  `json:"person_id,omitzero"`
	AccountID              model.AccountID `json:"account_id,omitzero"`
	PlayaName              string          `json:"playa_name,omitempty"` // NameSource; "" in M1
	Superuser              bool            `json:"superuser"`
	PasswordChangeRequired bool            `json:"password_change_required"`
	ExpiresAt              time.Time       `json:"expires_at,omitzero"`
	SignupOpen             bool            `json:"signup_open"` // Policy.SignupMode == SignupOpen; also when signed out
	Permissions            []string        `json:"permissions"` // PermissionLister; [] in M1, never null
}

// Issued is a session the service started. The token is unexported: only the handlers in
// this package turn it into Set-Cookie, and nothing logs it.
type Issued struct {
	Session Session
	token   string
}

// LogValue prints the session's ID only, never the token.
func (i Issued) LogValue() slog.Value { return slog.StringValue(i.Session.ID.String()) }

// String prints the session's ID only, so fmt never reaches the unexported token.
func (i Issued) String() string { return "session " + i.Session.ID.String() }

// GoString is String, for %#v.
func (i Issued) GoString() string { return i.String() }

// SuperuserResult is what create-superuser prints from.
type SuperuserResult struct {
	Account  model.AccountID
	Person   model.PersonID
	Created  bool          // false: an existing superuser account was reset
	Password secret.Secret // shown once by the command; never logged
}

var errBadCursor = errors.New("not a cursor from a previous page")
