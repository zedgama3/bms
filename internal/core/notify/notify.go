package notify

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/core/notify/channel"
	"github.com/zedgama3/bms/internal/core/notify/message"
	"github.com/zedgama3/bms/internal/model"
)

// The channel seam, defined in notify/channel and re-exported so callers see one package.
type (
	// Address is a mailbox: an optional display name and an address.
	Address = channel.Address
	// Envelope is one rendered message for one address.
	Envelope = channel.Envelope
	// Channel carries one envelope; see channel.Channel for its failure kinds.
	Channel = channel.Channel
	// ChannelFactory builds the channel for the current SMTP settings.
	ChannelFactory = channel.Factory
	// SMTPConfig is the current SMTP settings.
	SMTPConfig = channel.SMTPConfig
	// Security is how a connection is protected.
	Security = channel.Security
)

// The connection securities.
const (
	StartTLS = channel.StartTLS
	TLS      = channel.TLS
	None     = channel.None
)

// Errors a channel or the service reports; check with errors.Is.
var (
	// ErrRejected marks a permanent rejection (errs.Invalid).
	ErrRejected = channel.ErrRejected
	// ErrNotConfigured marks "no SMTP host or sender address is set" (errs.Unsupported).
	ErrNotConfigured = channel.ErrNotConfigured
	// ErrAuth marks a refused authentication (errs.Unavailable, token "auth").
	ErrAuth = channel.ErrAuth
	// ErrTemporary marks a transient 4xx reply (errs.Unavailable, token "smtp_4xx").
	ErrTemporary = channel.ErrTemporary
)

// MessageID identifies one outbox row.
type MessageID = message.ID

// Status is where an outbox row is in its life.
type Status = message.Status

// The statuses. The spec also names the sent status Sent, which is the name of the result
// struct of SendOnce, so the sent status is StatusSent.
const (
	Queued     = message.Queued
	Sending    = message.Sending
	StatusSent = message.Sent
	Failed     = message.Failed
)

// ParseStatus is the inverse of Status.String. Fails errs.Invalid.
func ParseStatus(s string) (Status, error) {
	st, err := message.ParseStatus(s)
	if err != nil {
		return 0, fmt.Errorf("notify.ParseStatus: %w", err)
	}
	return st, nil
}

// The limits and defaults the spec pins.
const (
	MaxRecipients  = 100
	MaxVars        = 32
	MaxVarKeyBytes = 32
	MaxVarBytes    = 1024
	MaxLinkBytes   = 1024
	MaxKeyBytes    = 128
	// MaxTemplateBytes is the size of a template file or of an override's body.
	MaxTemplateBytes = 16384
	// MaxSubjectBytes is the longest rendered subject, and the size of a Subject: header.
	MaxSubjectBytes = 256
	// MaxBodyBytes is the longest rendered body.
	MaxBodyBytes = 65536
	// MaxAttempts is how many times a row is leased before it fails for good.
	MaxAttempts = 8
	// MaxBatch is how many rows one SendOnce leases.
	MaxBatch = 50
	// MaxOutboxList is how many recent rows Outbox returns.
	MaxOutboxList = 50

	DefaultPollInterval = 5 * time.Second
	DefaultLease        = 2 * time.Minute
	DefaultSendTimeout  = 30 * time.Second
	PurgeInterval       = time.Hour
	// MaxAge is the longest a row may wait with its text, and the default.
	MaxAge       = 7 * 24 * time.Hour
	MinRetention = 7 * 24 * time.Hour
	// MaxRetention is the longest retention settings allows (100 years).
	MaxRetention = 876000 * time.Hour

	TemplateFileExt = ".tmpl"
	DefaultLanguage = "en"
)

// Record is an outbox row as the admin screen sees it: never an address or a body. Subject
// is empty in Outbox.
type Record struct {
	ID            MessageID      `json:"id"`
	Owner         string         `json:"owner"`
	Template      Template       `json:"template"`
	Person        model.PersonID `json:"person_id,omitzero"` // zero for an explicit address
	Language      Language       `json:"language"`
	Status        Status         `json:"status"`
	Attempts      int            `json:"attempts"`
	NextAttemptAt time.Time      `json:"next_attempt_at,omitzero"`
	LeasedUntil   time.Time      `json:"leased_until,omitzero"`
	CreatedAt     time.Time      `json:"created_at"`
	ExpiresAt     time.Time      `json:"expires_at,omitzero"`
	SentAt        time.Time      `json:"sent_at,omitzero"`
	FailedAt      time.Time      `json:"failed_at,omitzero"`
	LastError     string         `json:"last_error,omitempty"` // a token
	Subject       string         `json:"subject,omitempty"`
}

// LogValue logs the ID only. Cannot fail.
func (r Record) LogValue() slog.Value { return slog.StringValue(r.ID.String()) }

func recordOf(r message.Record) Record {
	return Record{
		ID: r.ID, Owner: r.Owner, Template: Template(r.Template), Person: r.Person,
		Language: Language(r.Language), Status: r.Status, Attempts: r.Attempts,
		NextAttemptAt: r.NextAttemptAt, LeasedUntil: r.LeasedUntil, CreatedAt: r.CreatedAt,
		SentAt: r.SentAt, FailedAt: r.FailedAt, ExpiresAt: r.ExpiresAt, LastError: r.LastError,
	}
}

// Rendered is a subject and a body, as Preview returns them.
type Rendered struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// TemplateText is the text of one template in one language.
type TemplateText struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// LanguageInfo is one language of a template: shipped, and overridden or not.
type LanguageInfo struct {
	Language   Language  `json:"language"`
	Overridden bool      `json:"overridden"`
	UpdatedAt  time.Time `json:"updated_at,omitzero"`
}

// TemplateInfo is one template of the list. Available is false for an override whose
// template no module or core area registers in this installation.
type TemplateInfo struct {
	Name      Template       `json:"name"`
	Vars      []string       `json:"vars"`
	Languages []LanguageInfo `json:"languages"`
	Available bool           `json:"available"`
}

// TemplateView is one template in one language: the shipped default and the current text.
type TemplateView struct {
	Name       Template     `json:"name"`
	Language   Language     `json:"language"`
	Default    TemplateText `json:"default"`
	Current    TemplateText `json:"current"`
	Overridden bool         `json:"overridden"`
	Vars       []string     `json:"vars"`
}

// OutboxView is what the outbox screen shows.
type OutboxView struct {
	Configured bool           `json:"configured"`
	Counts     map[Status]int `json:"counts"`
	Recent     []Record       `json:"recent"`
}

// Sent counts what one SendOnce did.
type Sent struct {
	Sent    int // delivered
	Retried int // requeued with a backoff
	Failed  int // final failures (rejected, no address, attempts exhausted)
	Skipped int // left alone: SMTP is not configured
}

// Route is the shape app converts to module.Route. Pattern is relative to /api/notify.
// Every route needs a signed-in person; the permission is checked inside the use case.
type Route struct {
	Method, Pattern string
	Handler         http.Handler
}

// primary returns the primary subtag of a language tag: "es-MX" is "es".
func primary(l Language) Language {
	p, _, _ := strings.Cut(string(l), "-")
	return Language(p)
}
