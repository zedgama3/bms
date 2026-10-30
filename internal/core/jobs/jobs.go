package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/core/jobs/internal/rules"
	"github.com/zedgama3/bms/internal/core/jobs/rows"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/retry"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// The limits and defaults the spec pins.
const (
	MinEvery            = time.Minute
	MaxEvery            = 168 * time.Hour
	MaxRunTimeout       = time.Hour
	MaxDelay            = 366 * 24 * time.Hour
	MaxKeyBytes         = 128
	DefaultPollInterval = 10 * time.Second
	DefaultLease        = 2 * time.Minute
	DefaultRunTimeout   = 5 * time.Minute
	DefaultWorkers      = 2
	MaxWorkers          = 32
	DoneTTL             = 7 * 24 * time.Hour
	MaxRecent           = 50
	MaxBatch            = 1000

	// MaxNameBytes is the longest job name (the signals.Name grammar, 96 bytes in all).
	MaxNameBytes = 96
	// MinTimeout is the shortest Decl.Timeout.
	MinTimeout = time.Second

	// NameRedeliver is the job that hands a stored signal delivery back to its subscriber.
	NameRedeliver Name = "signals.redeliver"
	// NamePurge is the job that deletes finished rows older than DoneTTL.
	NamePurge Name = "jobs.purge"
)

// DefaultRetry is the policy for Unavailable and Timeout: ten attempts in all, waits of
// about 1, 2, 4, 8, 16, 32, 60, 60, 60 minutes with full jitter (about four hours).
var DefaultRetry = retry.Policy{Initial: time.Minute, Max: time.Hour, Multiplier: 2, MaxAttempts: 10}

// The field codes jobs adds to the shell's vocabulary (validate has the rest).
const (
	codeRequired  = validate.CodeRequired
	codeMalformed = validate.CodeMalformed
	codeRange     = validate.CodeRange
	codeTooLong   = "too_long"
	codeScope     = rules.CodeScope
	codeUnknown   = "unknown"
	codeOnDemand  = "on_demand"
	codeRunning   = "running"
	codeStatus    = "status"
)

// ---- Name ------------------------------------------------------------------------------

// Name is a job's name, "<owner>.<ident>": the owner is a module name or a core area, the
// ident [a-z][a-z0-9]*(-[a-z0-9]+)*; at most 96 bytes (the signals.Name grammar). A scoped
// Registrar accepts only names whose Owner is its owner.
type Name string

var (
	identRE = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
	ownerRE = regexp.MustCompile(`^[a-z][a-z0-9]{1,31}$`)
	keyRE   = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)
)

// maxIdentBytes is the longest segment of a name, as for a signal name.
const maxIdentBytes = 64

// Validate fails with errs.Invalid (field "name": required, malformed, too_long).
func (n Name) Validate() error {
	const op = "jobs.Name.Validate"
	switch {
	case n == "":
		return fieldError(op, "name", codeRequired, nil, "must be set")
	case len(n) > MaxNameBytes:
		return fieldError(op, "name", codeTooLong, map[string]string{"max": fmt.Sprint(MaxNameBytes)},
			fmt.Sprintf("must be at most %d bytes", MaxNameBytes))
	}
	pub, rest, found := strings.Cut(string(n), ".")
	if !found || !validIdent(pub) || !validIdent(rest) {
		return fieldError(op, "name", codeMalformed, nil,
			"must be <owner>.<ident>, each of lower-case letters, digits and single hyphens")
	}
	return nil
}

func validIdent(s string) bool { return len(s) <= maxIdentBytes && identRE.MatchString(s) }

// Owner returns the part before the first dot, "" for an invalid name. Cannot fail.
func (n Name) Owner() string {
	if n.Validate() != nil {
		return ""
	}
	return rules.Owner(string(n))
}

// ---- IDs, arguments, statuses ------------------------------------------------------------

// JobID identifies one row of core.job.
type JobID = rows.ID

// Args are a job's arguments: record references only, as a signal's payload (signals
// decision 4), so nothing personal is ever queued, logged or shown. A handler reads the
// records the references name; it never trusts the arguments to be current.
type Args = signals.Payload

// Status is where a row is. The zero value is invalid.
type Status uint8

// The statuses.
const (
	Queued   Status = iota + 1 // waiting for run_at
	Running                    // leased by a process
	Done                       // the handler returned nil
	Failed                     // the handler failed finally, or gave up, or has no handler
	Canceled                   // an admin canceled it while it waited
)

var statusNames = [...]string{Queued: "queued", Running: "running", Done: "done", Failed: "failed", Canceled: "canceled"}

// String returns "queued", "running", "done", "failed", "canceled", "" for the zero value, or
// "Status(<n>)". Cannot fail.
func (s Status) String() string {
	switch {
	case s >= Queued && s <= Canceled:
		return statusNames[s]
	case s == 0:
		return ""
	default:
		return fmt.Sprintf("Status(%d)", uint8(s))
	}
}

// ParseStatus is the inverse of String. Fails errs.Invalid (field "status", code one_of).
func ParseStatus(s string) (Status, error) {
	for st := Queued; st <= Canceled; st++ {
		if statusNames[st] == s {
			return st, nil
		}
	}
	return 0, fieldError("jobs.ParseStatus", "status", validate.CodeOneOf, nil,
		"must be one of: queued, running, done, failed, canceled")
}

// MarshalText makes a Status a JSON string and a usable map key. Fails errs.Invalid for an
// invalid status.
func (s Status) MarshalText() ([]byte, error) {
	if s < Queued || s > Canceled {
		return nil, fieldError("jobs.Status.MarshalText", "status", validate.CodeOneOf, nil, "not a status")
	}
	return []byte(s.String()), nil
}

// UnmarshalText parses a status. Fails errs.Invalid.
func (s *Status) UnmarshalText(text []byte) error {
	st, err := ParseStatus(string(text))
	if err != nil {
		return err
	}
	*s = st
	return nil
}

// ---- Jobs, handlers, declarations --------------------------------------------------------

// Job is what a handler receives. Attempt is 1 for the first run of this row.
type Job struct {
	ID      JobID
	Name    Name
	Key     string // "" when the row has no dedupe key
	Args    Args
	Attempt int
	RunAt   time.Time
}

// Handler does one job's work. It runs on a worker goroutine of the writing copy, as the
// system (no session, no actor; InJob(ctx) is true; identity.PersonFrom is false), with a
// ctx that carries no transaction, a deadline of Decl.Timeout (default Options.RunTimeout)
// and the runner's cancellation (shutdown). It must be idempotent: the same row runs again
// after a crash, a lost lease, or a retry. Its error decides what happens next:
// Unavailable and Timeout retry with backoff; Canceled while ctx is done releases the row;
// db.ErrReadOnly releases it and pauses the runner; anything else fails the job. A panic is
// recovered, logged once with its stack, counted and fails the job. Nothing personal may be
// logged from a handler (go-conventions).
type Handler func(ctx context.Context, j Job) error

// Decl registers one job name with its handler.
type Decl struct {
	Name    Name
	Every   time.Duration // 0: on demand only (Enqueue; RunNow refused); else MinEvery..MaxEvery
	Timeout time.Duration // 0: Options.RunTimeout; else 1s..MaxRunTimeout
	Handler Handler
}

// Validate fails with errs.Invalid joining a validate.FieldError per problem: name,
// every (range), timeout (range), handler (required).
func (d Decl) Validate() error {
	v := validate.Errors{Op: "jobs.Decl.Validate"}
	if err := d.Name.Validate(); err != nil {
		v.Add(err)
	}
	if d.Every != 0 {
		v.Add(validate.Range("every", d.Every, MinEvery, MaxEvery))
	}
	if d.Timeout != 0 {
		v.Add(validate.Range("timeout", d.Timeout, MinTimeout, MaxRunTimeout))
	}
	if d.Handler == nil {
		v.Addf("handler", codeRequired, "must be set")
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// Request enqueues one deferred job.
type Request struct {
	Name Name
	// Key is the optional dedupe key, [A-Za-z0-9._:-]{1,MaxKeyBytes}; "" = none. Name the
	// record's own ID (the sign-up, the shift, the upload), never a person ID: keys appear in
	// GET /api/jobs/ and in logs, never on the screen (conformance rule jobs_key_not_person).
	Key   string
	Args  Args      // may be empty
	RunAt time.Time // zero = now; else at most MaxDelay ahead (the past is "now")
}

// Validate fails with errs.Invalid joining a validate.FieldError per problem: name, key
// (malformed, too_long), args (the payload's rules, under "args"), run_at (range, at most
// MaxDelay after the wall clock now). It uses the wall clock because a value type has no
// clock to take; the Runner checks run_at against its own clock, and jobstest.Fake against the
// clock it was given (ValidateAt).
func (r Request) Validate() error { return r.validate(time.Now()) }

// ValidateAt is Validate with now in place of the wall clock for the run_at bound. It is for
// fakes that keep their own clock (jobstest.Fake); callers outside tests use Validate.
func (r Request) ValidateAt(now time.Time) error { return r.validate(now) }

func (r Request) validate(now time.Time) error {
	v := validate.Errors{Op: "jobs.Request.Validate"}
	if err := r.Name.Validate(); err != nil {
		v.Add(err)
	}
	switch {
	case r.Key == "":
	case len(r.Key) > MaxKeyBytes:
		v.Add(errs.E("", errs.Invalid, &validate.FieldError{
			Field: "key", Code: codeTooLong, Params: map[string]string{"max": fmt.Sprint(MaxKeyBytes)},
			Reason: fmt.Sprintf("must be at most %d bytes", MaxKeyBytes),
		}))
	case !keyRE.MatchString(r.Key):
		v.Addf("key", codeMalformed, "must be letters, digits and . _ : -")
	}
	for _, fe := range validate.Fields(r.Args.Validate()) {
		field := "args" + strings.TrimPrefix(fe.Field, "payload")
		v.Add(errs.E("", errs.Invalid, &validate.FieldError{Field: field, Code: fe.Code, Params: fe.Params, Reason: fe.Reason}))
	}
	if !r.RunAt.IsZero() && r.RunAt.After(now.Add(MaxDelay)) {
		v.Add(errs.E("", errs.Invalid, &validate.FieldError{
			Field: "run_at", Code: codeRange,
			Params: map[string]string{"max": fmt.Sprint(MaxDelay)},
			Reason: "must be at most 366 days ahead",
		}))
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// ---- Registrar, InJob --------------------------------------------------------------------

// Registrar is what one owner holds: Deps.Jobs for a module, Scoped(area) for a core
// area. Safe for concurrent use.
type Registrar interface {
	// Register declares a job name and its handler, and, when d.Every > 0, that the runner
	// inserts its slot every d.Every. Called from Module.Start (no I/O, works on a
	// read-only copy); a name may be registered once per process. Fails errs.Invalid for
	// an invalid Decl or a Name whose Owner is not this scope's owner (field "name", code
	// "scope"), errs.Conflict when the name is already registered, and errs.Internal after
	// Run began.
	Register(d Decl) error

	// Enqueue stores one deferred job to run at r.RunAt. Inside a transaction (the ctx
	// Transact handed out) the row commits or rolls back with the caller's change; outside
	// one it is a standalone insert. With a Key the latest request wins: a Queued row with
	// the same (name, key) is moved to r.RunAt with r.Args, a Done, Failed or Canceled one is
	// reopened as Queued, and a Running one holds the request and is queued for it when the
	// run leaves running; the row's ID is returned. A handler may enqueue its own key to run
	// again; if that run then fails, its attempts count carries over (a request from any
	// other caller resets it), so a self-rescheduling failing job ends failed at the retry
	// limit (D52). Fails errs.Invalid for an invalid Request, a name outside the scope or
	// a name no handler was registered for in this process (code "unknown");
	// errs.Unavailable wrapping db.ErrReadOnly on a read-only copy; store kinds. It checks
	// no permission: the caller's use case is the authority.
	Enqueue(ctx context.Context, r Request) (JobID, error)
}

// InJob reports whether ctx is a handler's ctx. app's audit ActorFunc answers ActorSystem
// when it is true; authz has no actor and refuses permission-checked use cases, as inside a
// signal delivery. Cannot fail.
func InJob(ctx context.Context) bool { return rules.InJob(ctx) }

// Redeliverer hands a stored delivery back to the subscriber: the *signals.Dispatcher.
type Redeliverer interface {
	Redeliver(ctx context.Context, d signals.Delivery) error
}

// ---- Views -------------------------------------------------------------------------------

// Record is one row as the screen sees it. LogValue emits ID and name only.
type Record struct {
	ID         JobID
	Name       Name
	Key        string
	Status     Status
	Attempts   int
	RunAt      time.Time
	StartedAt  time.Time // zero until first leased
	FinishedAt time.Time // zero until final
	LastError  string    // a token: the errs kind in lower case, or panic, no_handler, lost_lease, gave_up
	Instance   string    // the copy that ran it last
	TraceID    string    // the enqueuer's trace, "" when none
}

// LogValue logs the ID and the name. Cannot fail.
func (r Record) LogValue() slog.Value {
	return slog.GroupValue(slog.String("id", r.ID.String()), slog.String("name", string(r.Name)))
}

// Registered is one name known to this process.
type Registered struct {
	Name    Name
	Owner   string
	Every   time.Duration // 0 for on demand
	Timeout time.Duration
	Last    *Record   // the newest row of this name, nil when none
	NextDue time.Time // the next slot, zero for on demand
}

// StatusView is what GET /api/jobs/ returns (Amendment J-W1: Recent became four lists). Each
// list is capped at MaxRecent by the store, so a long healthy queue cannot push a failure off
// the screen; the service sorts nothing.
type StatusView struct {
	ReadOnly   bool
	Instance   string // this process's Options.Instance; the screen marks a run by any other server
	Registered []Registered
	Counts     map[Status]int // every row by status
	Failed     []Record       // status failed; newest finished_at first
	Running    []Record       // status running; earliest started_at first
	Retrying   []Record       // status queued with last_error set; soonest run_at first
	Waiting    []Record       // status queued with last_error empty; soonest run_at first
}

// Ran counts one tick.
type Ran struct {
	Scheduled int // slots inserted
	Ran       int // handlers that returned nil
	Failed    int // final failures (including no_handler and gave_up)
	Retried   int // rows re-queued with backoff
	Released  int // rows given back (Canceled, read-only, lost lease)
}

// Route is the shape app converts (jobs cannot import core/module; audit decision 14).
type Route struct {
	Method, Pattern string
	Handler         http.Handler
}

// recordOf converts a stored row; a status the table cannot hold is a bug.
func recordOf(r rows.Job) (Record, error) {
	st, err := ParseStatus(r.Status)
	if err != nil {
		return Record{}, errs.E("jobs.recordOf", errs.Internal, err)
	}
	return Record{
		ID: r.ID, Name: Name(r.Name), Key: r.Key, Status: st, Attempts: r.Attempts, RunAt: r.RunAt,
		StartedAt: r.StartedAt, FinishedAt: r.FinishedAt, LastError: r.LastError, Instance: r.Instance,
		TraceID: r.TraceID,
	}, nil
}

// fieldError returns an errs.Invalid error carrying one FieldError.
func fieldError(op, field, code string, params map[string]string, reason string) error {
	return errs.E(op, errs.Invalid, &validate.FieldError{Field: field, Code: code, Params: params, Reason: reason})
}
