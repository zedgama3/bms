package notify

import (
	"log/slog"
	"regexp"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// Bounds of the timing options.
const (
	minPollInterval = time.Second
	maxPollInterval = 5 * time.Minute
	minLease        = 30 * time.Second
	maxLease        = 30 * time.Minute
	minSendTimeout  = 5 * time.Second
	maxSendTimeout  = 5 * time.Minute
)

var hostnameRE = regexp.MustCompile(`^[a-z0-9.-]{1,253}$`)

// Options builds a Service. Audit is scoped "notify" by app.
type Options struct {
	DB     db.DB
	Clock  clock.Clock
	Logger *slog.Logger
	Tracer telemetry.Tracer
	// Meter is optional: without one the service counts nothing.
	Meter telemetry.Meter
	Audit audit.Recorder // scoped "notify"
	Authz authz.Authorizer
	Actor authz.ActorFunc
	SMTP  SMTPSource
	Site  SiteSource
	// Retention is optional: nil keeps status rows forever.
	Retention RetentionSource
	Addresses AddressSource
	Languages LanguageSource
	Names     NameSource
	// Channel builds the channel for the current SMTP settings; nil means the SMTP channel
	// of notify/smtp, built with SendTimeout, Hostname and Clock.
	Channel      ChannelFactory
	PollInterval time.Duration // 1s..5m; DefaultPollInterval when 0
	Lease        time.Duration // 30s..30m; DefaultLease when 0; raised to twice SendTimeout when shorter
	SendTimeout  time.Duration // 5s..5m; DefaultSendTimeout when 0 (one SMTP conversation)
	Hostname     string        // for Message-ID and EHLO; [a-z0-9.-]{1,253}
}

// Validate fails errs.Invalid joining one validate.FieldError per problem: a nil dependency
// (fields notify.db, notify.clock, notify.logger, notify.tracer, notify.audit,
// notify.authz, notify.actor, notify.smtp, notify.site, notify.addresses, notify.languages,
// notify.names), notify.poll_interval, notify.lease and notify.send_timeout
// outside their ranges, and notify.hostname that is empty or malformed.
func (o Options) Validate() error {
	v := validate.Errors{Op: "notify.Options.Validate"}
	for _, d := range []struct {
		field string
		isNil bool
	}{
		{"notify.db", o.DB == nil},
		{"notify.clock", o.Clock == nil},
		{"notify.logger", o.Logger == nil},
		{"notify.tracer", o.Tracer == nil},
		{"notify.audit", o.Audit == nil},
		{"notify.authz", o.Authz == nil},
		{"notify.actor", o.Actor == nil},
		{"notify.smtp", o.SMTP == nil},
		{"notify.site", o.Site == nil},
		{"notify.addresses", o.Addresses == nil},
		{"notify.languages", o.Languages == nil},
		{"notify.names", o.Names == nil},
	} {
		if d.isNil {
			v.Addf(d.field, codeRequired, "must be set")
		}
	}
	if o.PollInterval != 0 {
		v.Add(validate.Range("notify.poll_interval", o.PollInterval, minPollInterval, maxPollInterval))
	}
	if o.Lease != 0 {
		v.Add(validate.Range("notify.lease", o.Lease, minLease, maxLease))
	}
	if o.SendTimeout != 0 {
		v.Add(validate.Range("notify.send_timeout", o.SendTimeout, minSendTimeout, maxSendTimeout))
	}
	if !hostnameRE.MatchString(o.Hostname) {
		v.Addf("notify.hostname", codeMalformed, "must be 1 to 253 characters of lower-case letters, digits, dots and hyphens")
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}
