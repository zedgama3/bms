package jobs

import (
	"log/slog"
	"regexp"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/retry"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// Bounds of the timing and count options.
const (
	minPollInterval = time.Second
	maxPollInterval = 5 * time.Minute
	minLease        = 30 * time.Second
	maxLease        = 10 * time.Minute
	maxAttemptsCap  = 100
)

var instanceRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// maxInstanceBytes is the longest instance name, as config and core.job.instance hold it.
const maxInstanceBytes = 64

// Options configure the runner. Zero durations and counts take the defaults.
type Options struct {
	DB     db.DB
	Clock  clock.Clock
	Logger *slog.Logger
	Tracer telemetry.Tracer
	Meter  telemetry.Meter

	Audit audit.Recorder   // scoped "jobs"
	Authz authz.Authorizer // the admin use cases' permission checks
	Actor authz.ActorFunc  // the session's person, for the admin use cases

	Redeliverer Redeliverer // the dispatcher; required

	Instance     string        // config Instance.Name; recorded on each run
	PollInterval time.Duration // DefaultPollInterval; 1s..5m
	Lease        time.Duration // DefaultLease; 30s..10m; renewed every Lease/3
	RunTimeout   time.Duration // DefaultRunTimeout; 1s..MaxRunTimeout
	Workers      int           // DefaultWorkers; 1..MaxWorkers
	Retry        retry.Policy  // zero = DefaultRetry; MaxAttempts 1..100
}

// Validate fails errs.Invalid joining one validate.FieldError per problem: a nil
// dependency (fields jobs.db, jobs.clock, jobs.logger, jobs.tracer, jobs.meter,
// jobs.audit, jobs.authz, jobs.actor, jobs.redeliverer), jobs.instance empty or malformed,
// and jobs.poll_interval, jobs.lease, jobs.run_timeout, jobs.workers and jobs.retry out of
// range.
func (o Options) Validate() error {
	v := validate.Errors{Op: "jobs.Options.Validate"}
	for _, d := range []struct {
		field string
		isNil bool
	}{
		{"jobs.db", o.DB == nil},
		{"jobs.clock", o.Clock == nil},
		{"jobs.logger", o.Logger == nil},
		{"jobs.tracer", o.Tracer == nil},
		{"jobs.meter", o.Meter == nil},
		{"jobs.audit", o.Audit == nil},
		{"jobs.authz", o.Authz == nil},
		{"jobs.actor", o.Actor == nil},
		{"jobs.redeliverer", o.Redeliverer == nil},
	} {
		if d.isNil {
			v.Addf(d.field, codeRequired, "must be set")
		}
	}
	if len(o.Instance) == 0 || len(o.Instance) > maxInstanceBytes || !instanceRE.MatchString(o.Instance) {
		v.Addf("jobs.instance", codeMalformed, "must be 1 to %d characters of lower-case letters, digits and hyphens", maxInstanceBytes)
	}
	if o.PollInterval != 0 {
		v.Add(validate.Range("jobs.poll_interval", o.PollInterval, minPollInterval, maxPollInterval))
	}
	if o.Lease != 0 {
		v.Add(validate.Range("jobs.lease", o.Lease, minLease, maxLease))
	}
	if o.RunTimeout != 0 {
		v.Add(validate.Range("jobs.run_timeout", o.RunTimeout, MinTimeout, MaxRunTimeout))
	}
	if o.Workers != 0 {
		v.Add(validate.Range("jobs.workers", o.Workers, 1, MaxWorkers))
	}
	if o.Retry != (retry.Policy{}) {
		if err := o.Retry.Validate(); err != nil {
			v.Addf("jobs.retry", codeMalformed, "%s", err.Error())
		} else if o.Retry.MaxAttempts < 1 || o.Retry.MaxAttempts > maxAttemptsCap {
			v.Addf("jobs.retry", codeRange, "max attempts must be 1 to %d, got %d", maxAttemptsCap, o.Retry.MaxAttempts)
		}
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}
