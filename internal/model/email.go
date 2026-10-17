package model

import (
	"log/slog"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// Email is an e-mail address as the person gave it. It is personal data (privacy-data):
// hold it only where the owning spec says, and log it never. slog.Any on an Email logs a
// marker (see LogValue); slog.String(k, string(e)) would not, so don't write that.
// Build one with ParseEmail or validate a converted string with Validate; the zero value ""
// means "none" and fails Validate, so an optional address is validated only when set.
type Email string

const (
	emailMaxLen    = 254
	emailMaxLocal  = 64
	emailForbidden = `<>,;"`

	codeEmail = "email" // every shape failure except empty
)

// ParseEmail trims surrounding space and checks shape: non-empty; at most 254 bytes;
// exactly one "@"; a local part of 1 to 64 bytes; a domain of at least 1 byte that neither
// starts nor ends with "."; valid UTF-8; and no whitespace, control characters, Unicode
// format characters (category Cf: zero-width space, bidi overrides) or any of < > , ; "
// anywhere. Deliverability is not checked here (notify proves it by sending). It fails
// with errs.Invalid wrapping a validate.FieldError whose Reason never echoes the input.
func ParseEmail(s string) (Email, error) {
	e := Email(strings.TrimSpace(s))
	if err := e.check("model.ParseEmail"); err != nil {
		return "", err
	}
	return e, nil
}

// Validate applies ParseEmail's rules to e as it is (no trimming) and fails with errs.Invalid
// if any is broken.
func (e Email) Validate() error { return e.check("model.Email.Validate") }

func (e Email) check(op string) error {
	s := string(e)
	bad := func(format string, args ...any) error {
		return fieldErr(op, "email", codeEmail, nil, format, args...)
	}
	switch {
	case s == "":
		return fieldErr(op, "email", validate.CodeRequired, nil, "must not be empty")
	case len(s) > emailMaxLen:
		return bad("must be at most %d bytes", emailMaxLen)
	}
	if !utf8.ValidString(s) {
		return bad("must be valid UTF-8")
	}
	if strings.IndexFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) ||
			strings.ContainsRune(emailForbidden, r)
	}) >= 0 {
		return bad("must not contain whitespace, control or format characters, or any of %s", emailForbidden)
	}
	if strings.Count(s, "@") != 1 {
		return bad("must contain exactly one @")
	}
	local, domain, _ := strings.Cut(s, "@")
	switch {
	case local == "":
		return bad("must have a name before the @")
	case len(local) > emailMaxLocal:
		return bad("name before the @ must be at most %d bytes", emailMaxLocal)
	case domain == "":
		return bad("must have a domain after the @")
	case strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, "."):
		return bad("domain must not start or end with a dot")
	}
	return nil
}

// Canonical returns the address lower-cased, the key under which addresses are unique
// ignoring case (D21). Cannot fail.
func (e Email) Canonical() Email { return Email(strings.ToLower(string(e))) }

// LogValue returns secret.Redacted when e is non-empty and secret.Unset when it is empty,
// so slog never prints an address. Cannot fail.
func (e Email) LogValue() slog.Value {
	if e == "" {
		return slog.StringValue(secret.Unset)
	}
	return slog.StringValue(secret.Redacted)
}
