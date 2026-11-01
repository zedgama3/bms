package audit

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// Limits. Bytes, not characters, to match the CHECK constraints.
const (
	MaxBatch              = 1000             // entries per Record call
	MaxFields             = 64               // field names per entry
	MaxFieldBytes         = 64               // one field name
	MaxReasonBytes        = 256              // Entry.Reason
	MaxTitles             = 16               // Actor.Titles
	MaxTitleBytes         = 64               // one title
	MaxActionBytes        = 96               // Action
	MaxPageSize           = 200              // Query.Limit
	DefaultPageSize       = 50               // Query.Limit when 0
	SpoolDirName          = "audit/spool"    // under Options.DataDir
	SpoolVersion          = 1                // "v" on every spool line
	DefaultImportInterval = 30 * time.Second // Options.ImportInterval when 0

	maxInstanceBytes = 64
	minImportEvery   = time.Second
	maxImportEvery   = 10 * time.Minute
)

// Recorder is what writers hold: module.Deps.Audit, and the value each core part receives.
// Every implementation is scoped to one area.
type Recorder interface {
	// Record writes every entry or none. Each entry's Action must belong to the recorder's
	// area (F9). Entries for a change are recorded inside the caller's Write transaction
	// (the ctx carries it) and commit or roll back with it; entries for a read are recorded
	// outside any transaction, after the data is loaded and before it is returned (decision
	// 2). On a read-only copy entries go to the spool (decision 4). A failure means the
	// caller's use case must fail (decision 3). Fails errs.Invalid (F7-F14), errs.Internal
	// (F15, F21), errs.Unavailable (F16, F18, F22), errs.Timeout/Canceled (F20), or the
	// TitleSource's kind (F19). Safe for concurrent use.
	Record(ctx context.Context, entries ...Entry) error
}

// ActorFunc reads the signed-in actor from ctx. identity owns the session; app wraps its
// ctx accessor: Kind ActorPerson with Person and Superuser set, ok true; Kind ActorOperator
// (nothing else set) while a bms command runs with no session; or ok false when there is no
// session. Any other answer fails the Record with errs.Internal. Must not do I/O.
type ActorFunc func(ctx context.Context) (Actor, bool)

// TitleSource returns the role or position titles person holds now, for D22. authz
// implements it in M2; nil in M1 (titles stay empty). Returns nil, nil for a person with
// no titles. Fails with the store's kind.
type TitleSource interface {
	TitlesOf(ctx context.Context, person model.PersonID) ([]string, error)
}

// Options builds the log. app copies config.Instance.Name and config.Data.Dir in.
type Options struct {
	DB             db.DB
	Clock          clock.Clock
	Logger         *slog.Logger
	Tracer         telemetry.Tracer
	Meter          telemetry.Meter // optional: nil records no metrics
	Instance       string          // config instance.name grammar: 1..64, [a-z0-9]([a-z0-9-]*[a-z0-9])?
	DataDir        string          // the spool lives in filepath.Join(DataDir, SpoolDirName)
	Actor          ActorFunc       // required
	Titles         TitleSource     // the authz.Lookup; required
	ImportInterval time.Duration   // 0 = DefaultImportInterval; else 1s..10m
}

// Validate fails errs.Invalid joining a validate.FieldError per problem: "audit.db",
// "audit.clock", "audit.logger", "audit.tracer", "audit.actor" (nil), "audit.titles" (nil),
// "instance.name",
// "data.dir" (empty or NUL), "audit.import_interval".
func (o Options) Validate() error {
	v := validate.Errors{Op: "audit.Options.Validate"}
	if o.DB == nil {
		v.Addf("audit.db", validate.CodeRequired, "must be set")
	}
	if o.Clock == nil {
		v.Addf("audit.clock", validate.CodeRequired, "must be set")
	}
	if o.Logger == nil {
		v.Addf("audit.logger", validate.CodeRequired, "must be set")
	}
	if o.Tracer == nil {
		v.Addf("audit.tracer", validate.CodeRequired, "must be set")
	}
	if o.Actor == nil {
		v.Addf("audit.actor", validate.CodeRequired, "must be set")
	}
	if o.Titles == nil {
		v.Addf("audit.titles", validate.CodeRequired, "must be set")
	}
	if len(o.Instance) > maxInstanceBytes || !instanceRE.MatchString(o.Instance) {
		v.Addf("instance.name", codeMalformed, "must be 1 to %d characters of a-z, 0-9 and hyphens, not starting or ending with a hyphen", maxInstanceBytes)
	}
	if o.DataDir == "" || strings.ContainsRune(o.DataDir, 0) {
		v.Addf("data.dir", validate.CodeRequired, "must be a non-empty path without NUL")
	}
	if o.ImportInterval != 0 {
		v.Add(validate.Range("audit.import_interval", o.ImportInterval, minImportEvery, maxImportEvery))
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// interval is the import interval with the default applied.
func (o Options) interval() time.Duration {
	if o.ImportInterval == 0 {
		return DefaultImportInterval
	}
	return o.ImportInterval
}
