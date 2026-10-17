package db

import (
	"net/url"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// MinServerVersion is the oldest PostgreSQL server Open accepts, as server_version_num.
// PostgreSQL 16 (decision 20).
const MinServerVersion = 160000

// Bounds mirror config.md's catalogue for db.max_conns, db.connect_timeout and
// db.query_timeout.
const (
	minMaxConns       = 1
	maxMaxConns       = 1000
	minConnectTimeout = time.Second
	maxConnectTimeout = 10 * time.Minute
	minQueryTimeout   = 100 * time.Millisecond
	maxQueryTimeout   = 10 * time.Minute
)

// Options is what Open needs. app copies config.DB into it (config.md decision 12), so db
// is built in tests with no environment at all.
type Options struct {
	// URL is the PostgreSQL connection URL (postgres:// or postgresql:// with a host). It
	// is read once, with Value(), inside Open, and never appears in an error or a log.
	URL secret.Secret
	// MaxConns is the pool's upper bound. 1..1000.
	MaxConns int
	// ConnectTimeout bounds the initial connect in Open, retries included. 1s..10m.
	ConnectTimeout time.Duration
	// QueryTimeout is the longest one statement may run when the caller's context allows
	// more. 100ms..10m.
	QueryTimeout time.Duration
	// Tracer, when set, gives every statement a client span. nil means no statement
	// spans. app passes tel.Tracer("db").
	Tracer telemetry.Tracer
	// Meter, when set, registers the pool gauges. nil means no gauges. app passes
	// tel.Meter("db").
	Meter telemetry.Meter
}

// Validate fails with errs.Invalid joining one validate.FieldError per problem, fields
// "db.url", "db.max_conns", "db.connect_timeout", "db.query_timeout": URL unset or not an
// absolute postgres/postgresql URL with a host (never echoed), a number or duration out
// of range (the same bounds as config.md's catalogue). Tracer and Meter are not checked:
// nil is a valid value for each (tests and `bms migrate` run without them).
func (o Options) Validate() error {
	v := validate.Errors{Op: "db.Options.Validate"}
	switch {
	case !o.URL.IsSet():
		v.Addf("db.url", validate.CodeRequired, "must be set")
	default:
		v.Add(validateURL(o.URL.Value()))
	}
	v.Add(validate.Range("db.max_conns", o.MaxConns, minMaxConns, maxMaxConns))
	v.Add(validate.Range("db.connect_timeout", o.ConnectTimeout, minConnectTimeout, maxConnectTimeout))
	v.Add(validate.Range("db.query_timeout", o.QueryTimeout, minQueryTimeout, maxQueryTimeout))
	return v.Err() //nolint:wrapcheck // validate.Errors.Err already wraps with errs.E
}

// validateURL checks the scheme and the host without echoing the URL.
func validateURL(raw string) error {
	u, perr := url.Parse(raw)
	bad := perr != nil
	if !bad {
		bad = u.Hostname() == "" || (!strings.EqualFold(u.Scheme, "postgres") && !strings.EqualFold(u.Scheme, "postgresql"))
	}
	if bad {
		return errs.E("db.Options.Validate", errs.Invalid, &validate.FieldError{
			Field:  "db.url",
			Code:   validate.CodeURL,
			Params: map[string]string{"schemes": "postgres, postgresql"},
			Reason: "must be an absolute postgres:// or postgresql:// URL with a host",
		})
	}
	return nil
}
