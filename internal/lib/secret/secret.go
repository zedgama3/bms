// Package secret holds a string that must never be printed. Every formatting, logging and
// encoding path yields a marker instead of the value; only Value returns it. The package
// depends on the standard library and internal/lib/errs.
// Ported from Sojourn (D11).
package secret

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/zedgama3/bms/internal/lib/errs"
)

const (
	// Redacted is the text a set Secret prints, logs and marshals as.
	Redacted = "[redacted]"
	// Unset is the text an unset Secret prints, logs and marshals as.
	Unset = "[unset]"
)

// Secret holds a value that must never be printed. The zero value (nil pointer) is
// "unset". The value is held behind a pointer so that fmt's reflective fallbacks (a bad
// verb on a struct, %p) see a pointer, never the plaintext bytes. Every formatting path
// (fmt verbs, encoding/json, slog, templates) yields a marker.
//
// A Secret holds a pointer, so == compares identity: two New("x") are not equal, copies of
// one are. Compare Value() instead; never use == or reflect.DeepEqual on a Secret.
//
// Hold a Secret by value, not as a *Secret. slog calls LogValue through a nil *Secret and
// recovers the resulting panic, so a nil *Secret in a log call yields a "!PANIC" value
// rather than a marker; if a *Secret is unavoidable, nil-check it before logging.
type Secret struct{ p *string }

// New wraps v; an empty v yields the zero (unset) Secret. Cannot fail.
// Consumers: config (binding each secret key), tests that
// build a Config by hand.
func New(v string) Secret {
	if v == "" {
		return Secret{}
	}
	return Secret{p: &v}
}

// Value returns the secret, or "" when unset. This is the only way to read it. Cannot fail.
// Consumers: identity (hashing or comparing a password or token), notify's SMTP sender (the
// SMTP password, read once), db (building the connection string), app (passing config
// secrets to constructors) and config.Validate (length and charset checks).
func (s Secret) Value() string {
	if s.p == nil {
		return ""
	}
	return *s.p
}

// IsSet reports whether the secret is non-empty. Cannot fail.
// Consumers: config.Validate ("required when ..."), app.
func (s Secret) IsSet() bool { return s.p != nil && *s.p != "" }

// String returns Redacted when set and Unset when not. Cannot fail.
// Consumers: fmt (via Format), text/template, any caller that wants the marker.
func (s Secret) String() string {
	if s.IsSet() {
		return Redacted
	}
	return Unset
}

// GoString is String, so %#v on a Secret held in an interface cannot leak the value.
// Cannot fail. Consumer: fmt.
func (s Secret) GoString() string { return s.String() }

// MarshalText returns the marker as bytes and a nil error, so encoding/json, encoding/xml
// and any other TextMarshaler consumer emit the marker. It never returns the value. The
// encoded form of a Secret is for display only: see UnmarshalText for the consequence.
// Consumers: encoding/json (API responses and debug dumps that contain a Secret), slog's
// JSON handler (through LogValue first, MarshalText as the fallback).
func (s Secret) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

// LogValue returns the marker as a slog string value, so a slog handler never sees the
// Secret itself. Cannot fail. Consumers: log/slog, via slog.Any on a Secret.
func (s Secret) LogValue() slog.Value { return slog.StringValue(s.String()) }

// Format implements fmt.Formatter. fmt handles %T, %p and %w itself before it consults a
// Formatter. The pointer field is what protects %p and %w: %w on a non-error operand goes
// down fmt's reflective bad-verb path, which prints an address, never the value.
// For every other verb (including %+v, %#v, %d, %x and unknown verbs) Format writes the
// marker, formatted with fmt.FormatString(f, verb) where verb is 'q' for %q and 's'
// otherwise, so width, precision and flags apply and %q quotes. It never writes the value
// and never panics. A failed write to f is dropped: it has nowhere to be reported, and the
// value was never written, which is the only property that matters here.
// Consumer: fmt, and through it log/slog's text handler, errors built with fmt.Errorf, and
// test output.
func (s Secret) Format(f fmt.State, verb rune) {
	stringVerb := 's'
	if verb == 'q' {
		stringVerb = 'q' // keep the quotes the caller asked for
	}
	// The write error is checked and deliberately dropped; see the doc comment.
	if _, err := fmt.Fprintf(f, fmt.FormatString(f, stringVerb), s.String()); err != nil {
		return
	}
}

// UnmarshalText sets *s to text, verbatim: no trimming, no normalisation. Empty text yields
// the zero (unset) Secret, exactly like New(""). It fails with errs.Invalid when text equals
// Redacted or Unset, because that input is almost certainly a marshalled Secret being fed
// back in, and accepting it would turn a display artefact into a live password (a real value
// that is literally one of the two markers is refused too); and with errs.Internal when s
// is nil. On failure *s is unchanged. It never panics. The owner of the containing struct
// validates the result (length, UTF-8, NUL); this method does not.
// Consumer: config and settings loaders decoding JSON or text values with encoding/json.
func (s *Secret) UnmarshalText(text []byte) error {
	const op = "secret.Secret.UnmarshalText"
	if s == nil {
		return errs.E(op, errs.Internal, errors.New("nil receiver"))
	}
	switch string(text) {
	case Redacted, Unset:
		return errs.E(op, errs.Invalid, errors.New("text is a redaction marker, not a secret value"))
	}
	*s = New(string(text))
	return nil
}
