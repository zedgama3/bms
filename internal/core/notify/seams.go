package notify

import (
	"context"

	"github.com/zedgama3/bms/internal/core/regional"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/model"
)

// Site is what every template may use: the regional's name, the public URL (no trailing
// slash), the default language and the terminology for a language (key -> word). The Service
// builds it from Regional and Terms for each language it renders.
type Site struct {
	Name            string
	URL             string
	DefaultLanguage Language
	Terms           map[string]string
}

// Accounts is the identity.Lookup: the login address of a person's account. EmailOf fails
// errs.NotFound when the person has none. notify reads it only to deliver mail and never
// logs the address.
type Accounts interface {
	EmailOf(ctx context.Context, person model.PersonID) (model.Email, error)
}

// Profiles is the people.Lookup: the playa name, the saved language and the contact address
// of a person. Each fails errs.NotFound when the person has no profile (or, for
// ContactEmailOf, no contact address); notify reads ContactEmailOf only to deliver mail and
// never logs the address.
type Profiles interface {
	PlayaNameOf(ctx context.Context, person model.PersonID) (string, error)
	LanguageOf(ctx context.Context, person model.PersonID) (string, error)
	ContactEmailOf(ctx context.Context, person model.PersonID) (model.Email, error)
}

// RegionalSource is regional's Profile: the name, the public URL and the default language.
type RegionalSource interface {
	Profile(ctx context.Context) (regional.Profile, error)
}

// TermSource is settings' TermsFor: the regional's terminology in lang, by key.
type TermSource interface {
	TermsFor(ctx context.Context, lang string) (map[string]settings.Forms, error)
}

// Notifier is module.Deps.Notify, scoped to one owner. Send validates m, refuses a
// template of another owner (errs.Invalid, field "template", code "scope") or one not
// registered ("unknown"), renders one row per recipient in that recipient's language and
// inserts the rows on the transaction ctx carries (in a transaction of its own when it
// carries none). It never sends inline and never checks a permission. A row whose (owner,
// key, recipient) already exists is skipped silently. Fails errs.Unsupported wrapping
// ErrNotConfigured before looking at any recipient when SMTP or the sender is unset;
// errs.Unavailable wrapping db.ErrReadOnly on a read-only copy; errs.Internal inside a Read
// transaction; store kinds; the kinds of Regional, Terms and Profiles (a lookup that finds
// nobody is not a failure: see Profiles).
type Notifier interface {
	Send(ctx context.Context, m Message) error
	// Configured reports whether Send would enqueue now: the SMTP settings were read and
	// SMTPConfig.Configured() is true. It reads the settings snapshot (no SMTP connection)
	// and never looks at a recipient, so its answer cannot depend on who is being written
	// to. Fails with the settings Reader's kind only. Call it before work whose outcome must not
	// depend on the recipient.
	Configured(ctx context.Context) (bool, error)
}

// AddressSender sends one message to one explicit address instead of a person; m.To must be
// empty. The row stores the address until a final state. The language is the site default.
// Fails as Send, plus errs.Invalid for a malformed address or a non-empty To. Core only;
// never in module.Deps.
type AddressSender interface {
	SendTo(ctx context.Context, to model.Email, m Message) error
}
