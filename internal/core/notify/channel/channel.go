// Package channel holds the types notify and its channels share: the envelope, the Channel
// seam, the SMTP configuration and the errors a channel reports. It is a leaf (model and lib
// only) so that notify can use the SMTP channel without an import cycle; notify re-exports
// every name here as an alias, and callers use them as notify.Channel, notify.Envelope and
// so on.
package channel

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"strconv"

	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// Address is a mailbox: an optional display name and an address.
type Address struct {
	Name  string // display name; may be ""
	Email model.Email
}

// Envelope is one rendered message for one address. MessageID is "<id>@<hostname>" without
// angle brackets.
type Envelope struct {
	From      Address
	ReplyTo   *Address
	To        model.Email
	Subject   string
	Body      string // text/plain, UTF-8
	MessageID string
}

// LogValue logs the redaction marker: an Envelope holds an address, a subject and a body.
// Cannot fail.
func (Envelope) LogValue() slog.Value { return slog.StringValue(secret.Redacted) }

// Channel carries one envelope. Send fails errs.Invalid wrapping ErrRejected when the
// server refuses the message or the recipient permanently (5xx), errs.Unavailable when it
// cannot connect, authenticate (wrapping ErrAuth) or gets a transient reply (wrapping
// ErrTemporary), and errs.Timeout or errs.Canceled from ctx. Ping connects, greets,
// authenticates and quits, with the same kinds. A Channel is cheap to construct and does no
// I/O until Send or Ping. Errors never carry text from the server, which may echo an
// address.
type Channel interface {
	Send(ctx context.Context, e Envelope) error
	Ping(ctx context.Context) error
}

// Factory builds the channel for the current SMTP settings. Fails errs.Invalid when cfg
// does not validate. notify re-exports it as ChannelFactory.
type Factory func(cfg SMTPConfig) (Channel, error)

// ErrRejected marks a permanent rejection (errs.Invalid). Check with errors.Is.
var ErrRejected = errors.New("message rejected by the server")

// ErrNotConfigured marks "no SMTP host or sender address is set" (errs.Unsupported).
var ErrNotConfigured = errors.New("e-mail is not configured")

// ErrAuth marks a refused or impossible authentication (errs.Unavailable): the sender
// records the token "auth".
var ErrAuth = errors.New("the server refused authentication")

// ErrTemporary marks a transient 4xx reply (errs.Unavailable): the sender records the token
// "smtp_4xx".
var ErrTemporary = errors.New("the server asked to try again later")

// Security is how a connection is protected. The zero value is invalid.
type Security int

// The connection securities.
const (
	StartTLS Security = iota + 1 // plaintext greeting, then STARTTLS (required)
	TLS                          // implicit TLS from the first byte
	None                         // no encryption: a local relay or Mailpit
)

// String returns "starttls", "tls", "none" or "Security(<n>)". Cannot fail.
func (s Security) String() string {
	switch s {
	case StartTLS:
		return "starttls"
	case TLS:
		return "tls"
	case None:
		return "none"
	default:
		return "Security(" + strconv.Itoa(int(s)) + ")"
	}
}

// ParseSecurity is the inverse of String. Fails errs.Invalid (field "notify.smtp_security").
func ParseSecurity(s string) (Security, error) {
	for _, v := range []Security{StartTLS, TLS, None} {
		if v.String() == s {
			return v, nil
		}
	}
	v := validate.Errors{Op: "notify.ParseSecurity"}
	v.Addf("notify.smtp_security", validate.CodeOneOf, "must be one of: starttls, tls, none")
	return 0, v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// Limits of the SMTP settings.
const (
	MaxAlertAddresses = 10
	MaxHostBytes      = 253
	MaxUsernameBytes  = 255
	MaxPasswordBytes  = 1024
	MaxSenderName     = 100
)

// SMTPConfig is the current SMTP settings, as app reads them from settings.
type SMTPConfig struct {
	Host           string // "" means not configured
	Port           int    // 1..65535
	Security       Security
	Username       string        // "" means no auth
	Password       secret.Secret // unset means no auth
	Sender         Address       // required to send
	ReplyTo        *Address
	AlertAddresses []model.Email // at most MaxAlertAddresses; the maintainers' addresses for the superuser alert
}

// Configured reports Host != "" and Sender.Email set. Cannot fail.
func (c SMTPConfig) Configured() bool { return c.Host != "" && c.Sender.Email != "" }

// Validate fails errs.Invalid with the fields "notify.smtp_host", "notify.smtp_port",
// "notify.smtp_security", "notify.smtp_username", "notify.sender_address",
// "notify.reply_to" and "notify.alert_addresses". An empty Host is valid (not configured).
// Messages never echo a value.
func (c SMTPConfig) Validate() error {
	v := validate.Errors{Op: "notify.SMTPConfig.Validate"}
	if c.Host != "" && !validHost(c.Host) {
		v.Addf("notify.smtp_host", validate.CodeMalformed, "must be a host name or an IP address of at most %d bytes", MaxHostBytes)
	}
	if c.Port < 1 || c.Port > 65535 {
		v.Addf("notify.smtp_port", validate.CodeRange, "must be between 1 and 65535")
	}
	if c.Security < StartTLS || c.Security > None {
		v.Addf("notify.smtp_security", validate.CodeOneOf, "must be one of: starttls, tls, none")
	}
	if len(c.Username) > MaxUsernameBytes {
		v.Addf("notify.smtp_username", "too_long", "must be at most %d bytes", MaxUsernameBytes)
	}
	if c.Sender.Email != "" && c.Sender.Email.Validate() != nil {
		v.Addf("notify.sender_address", validate.CodeMalformed, "must be an e-mail address")
	}
	if c.Sender.Email != "" && !validName(c.Sender.Name) {
		v.Addf("notify.sender_address", validate.CodeMalformed, "the display name must have no control characters and at most %d bytes", MaxSenderName)
	}
	if c.ReplyTo != nil && (c.ReplyTo.Email.Validate() != nil || !validName(c.ReplyTo.Name)) {
		v.Addf("notify.reply_to", validate.CodeMalformed, "must be an e-mail address")
	}
	if len(c.AlertAddresses) > MaxAlertAddresses {
		v.Addf("notify.alert_addresses", "too_many", "at most %d addresses", MaxAlertAddresses)
	}
	for _, a := range c.AlertAddresses {
		if a.Validate() != nil {
			v.Addf("notify.alert_addresses", validate.CodeMalformed, "every entry must be an e-mail address")
			break
		}
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

func validName(s string) bool {
	if len(s) > MaxSenderName {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// validHost accepts an IP address or a DNS name (letters, digits, hyphens, dots).
func validHost(h string) bool {
	if len(h) > MaxHostBytes {
		return false
	}
	if net.ParseIP(h) != nil {
		return true
	}
	for i := 0; i < len(h); i++ {
		c := h[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
		case c == '.' && i > 0 && i < len(h)-1 && h[i-1] != '.':
		default:
			return false
		}
	}
	return true
}
