package notify

import (
	"context"

	"github.com/zedgama3/bms/internal/model"
)

// Site is what every template may use: the regional's name, the public URL (no trailing
// slash), the default language and the terminology for a language (key -> word).
type Site struct {
	Name            string
	URL             string
	DefaultLanguage Language
	Terms           map[string]string
}

// SiteSource answers the Site for lang ("" = the default language). app implements it over
// settings' Regional and TermsFor.
type SiteSource interface {
	Site(ctx context.Context, lang Language) (Site, error)
}

// AddressSource answers a person's current address: identity's login address, else
// people's contact address; errs.NotFound when the person has none. Filled by app.
type AddressSource interface {
	AddressOf(ctx context.Context, person model.PersonID) (model.Email, error)
}

// LanguageSource answers a person's saved language, "" when unset or unknown. Filled by app
// over people.LanguageOf.
type LanguageSource interface {
	LanguageOf(ctx context.Context, person model.PersonID) (Language, error)
}

// NameSource answers a person's playa name, "" when unknown. Filled by app over
// people.PlayaNameOf.
type NameSource interface {
	PlayaNameOf(ctx context.Context, person model.PersonID) (string, error)
}

// Notifier is module.Deps.Notify, scoped to one owner. Send validates m, refuses a
// template of another owner (errs.Invalid, field "template", code "scope") or one not
// registered ("unknown"), renders one row per recipient in that recipient's language and
// inserts the rows on the transaction ctx carries (in a transaction of its own when it
// carries none). It never sends inline and never checks a permission. A row whose (owner,
// key, recipient) already exists is skipped silently. Fails errs.Unsupported wrapping
// ErrNotConfigured before looking at any recipient when SMTP or the sender is unset;
// errs.Unavailable wrapping db.ErrReadOnly on a read-only copy; errs.Internal inside a Read
// transaction; store kinds; the SiteSource's, LanguageSource's and NameSource's kinds.
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
