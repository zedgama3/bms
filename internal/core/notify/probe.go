package notify

import (
	"context"
	"errors"
	"fmt"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/notify/postgres"
	"github.com/zedgama3/bms/internal/core/notify/smtp"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// EmailState is what Probe found, for bms check's e-mail lines. It holds no e-mail address,
// user name or password: the host, port and security are what `check` prints.
type EmailState struct {
	// Known is true when the SMTP settings could be read. When false, every other field is
	// zero and Probe's error is the settings Reader's.
	Known bool
	// Configured is whether a host and a sender address are set (SMTPConfig.Configured).
	Configured bool
	// Host, Port and Security describe the connection; set when Known.
	Host     string
	Port     int
	Security Security
	// Outbox counts the outbox rows by status; nil when OutboxErr is set. A status with no
	// rows is absent.
	Outbox    map[Status]int
	OutboxErr error
	// Pinged is true when a conversation with the server was tried (Configured). PingErr is
	// nil when the server accepted it; its kind is the channel's.
	Pinged  bool
	PingErr error
}

// Probe is bms check's e-mail line over a real database: whether SMTP is configured and how
// (read through r, a Reader scoped "notify"), the outbox counts (read through its own store
// on d) and, when SMTP is configured, whether the server accepts a conversation. It sends
// nothing and never returns an address, a user name or a password.
//
// It returns the state as far as it got together with the error of what failed: the Reader's
// kind when the settings cannot be read (nothing else is tried), the store's kind for the
// outbox (a schema behind, or the database down, is errs.Unavailable) and the channel's kind
// for the conversation, joined when more than one failed. hostname is the name given in EHLO
// (app passes the instance name, as the sender does); empty means smtp's default. Fails
// errs.Invalid for a nil d or r.
func Probe(ctx context.Context, d db.DB, r settings.Reader, hostname string) (EmailState, error) {
	const op = "notify.Probe"
	if d == nil || r == nil {
		return EmailState{}, errs.E(op, errs.Invalid, errors.New("nil dependency"))
	}
	st, err := postgres.NewStore(d)
	if err != nil {
		return EmailState{}, fmt.Errorf("%s: %w", op, err)
	}
	cfg, err := smtpFromSettings(ctx, r)
	if err != nil {
		return EmailState{}, fmt.Errorf("%s: the SMTP settings: %w", op, err)
	}
	out := EmailState{Known: true, Configured: cfg.Configured(), Host: cfg.Host, Port: cfg.Port, Security: cfg.Security}
	var failed []error
	if out.Outbox, out.OutboxErr = st.CountByStatus(ctx); out.OutboxErr != nil {
		out.Outbox = nil
		out.OutboxErr = schemaBehind(op, out.OutboxErr)
		failed = append(failed, fmt.Errorf("%s: the outbox: %w", op, out.OutboxErr))
	}
	if out.Configured {
		out.Pinged = true
		ch, err := smtp.New(cfg, smtp.Options{Hostname: hostname})
		if err == nil {
			pctx, cancel := context.WithTimeout(ctx, DefaultSendTimeout)
			defer cancel()
			err = ch.Ping(pctx)
		}
		if err != nil {
			out.PingErr = channelError(op, err)
			failed = append(failed, out.PingErr)
		}
	}
	return out, errors.Join(failed...)
}

// schemaBehind reclassifies the store's error as errs.Unavailable when the server says the
// outbox's table or column, or its schema, does not exist: the database is older than this
// binary, and `bms migrate` is the cure, so check prints "outbox: unavailable: unavailable" rather than a bug's "internal". Any
// other error keeps its kind. The chain is kept.
func schemaBehind(op string, err error) error {
	var pe *db.Error
	if errors.As(err, &pe) {
		switch pe.Code {
		case "42P01", "42703", "3F000": // undefined_table, undefined_column, invalid_schema_name
			return errs.E(op, errs.Unavailable, err)
		}
	}
	return err
}
