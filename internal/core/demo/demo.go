package demo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/zedgama3/bms/internal/contracts/demoseed"
	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/demo/postgres"
	"github.com/zedgama3/bms/internal/core/demo/rows"
	"github.com/zedgama3/bms/internal/core/departments"
	"github.com/zedgama3/bms/internal/core/events"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// Constants. Password and the addresses are public by design: they are printed on the
// sign-in page of a public demo. Nothing here is a secret.
const (
	Password = "burn-demo-password" //nolint:gosec // public by design: printed on the demo sign-in page (decision 13)
	Regional = "Ember Flats"        // the fictional regional (decision 12)
	Domain   = "demo.invalid"       // RFC 2606: can never be a real mailbox
	Zone     = "America/Chicago"    // the fictional regional's zone

	MaxUploadBytes = 2 << 20   // files cap in demo mode, whatever files.max_bytes says
	MaxQuotaBytes  = 256 << 20 // site-wide quota cap in demo mode
	MaxPerRecord   = 5         // per-record cap in demo mode
	MaxMail        = 200       // captured mail rows kept (oldest trimmed on insert)
	MaxMailList    = 50        // ListMail page
	SeedVersion    = 1         // bumped when the seed table changes shape

	DefaultContributorTimeout = 2 * time.Minute
	SessionLifetime           = time.Hour // the seed's sessions

	seedTimeout    = 10 * time.Minute
	dropTimeout    = 30 * time.Minute // bounds the drop and the migration of a reset
	minContributor = 10 * time.Second
	maxContributor = 30 * time.Minute
	maxURLBytes    = 256
)

// The codes of the Conflict errors Seed and Reset return, on the field "demo". app maps them
// to exit status 2.
const (
	CodeSeeded   = rows.CodeSeeded
	CodeNotEmpty = rows.CodeNotEmpty
	CodeNotDemo  = rows.CodeNotDemo
	// ConflictField is the field of those conflicts.
	ConflictField = rows.Field
)

// Set is the migration set app adds to coreSets, after files.
var Set = db.Set{Name: "demo", Schema: "core", Files: postgres.Migrations}

// Info is GET /api/demo. When Enabled is false every other field is zero and omitted.
type Info struct {
	Enabled  bool          `json:"enabled"`
	Regional string        `json:"regional,omitempty"`
	Accounts []AccountInfo `json:"accounts,omitempty"`
	Password string        `json:"password,omitempty"`
}

// AccountInfo is one row of the sign-in page's table. Roles are starter role keys, a
// department-scoped one as "<role>@<department key>" (e.g. "department-admin@rangers");
// the UI translates both through its catalogue.
type AccountInfo struct {
	Key   string   `json:"key"`
	Email string   `json:"email"`
	Roles []string `json:"roles"`
}

// Report is what Seed and Reset print: one Step per named step, in order.
type Report struct{ Steps []Step }

// Step is one named step of a seed or reset and how many records it touched.
type Step struct {
	Name  string // "accounts", "profiles", "grants", "departments", "event", "settings", "walk-ons", "module:<name>", "objects", "spool", "drop", "migrate"
	Items int
}

type mail struct{}

// MailID identifies a captured message.
type MailID = model.ID[mail]

// Mail is one captured notification. To, Subject and Body are personal (a self-sign-up
// may have typed a real address despite the banner); they live until trimmed or reset.
type Mail struct {
	ID         MailID      `json:"id"`
	To         model.Email `json:"to"`
	Subject    string      `json:"subject"`
	Body       string      `json:"body"`
	CapturedAt time.Time   `json:"captured_at"`
}

// MailQuery selects captured mail: Limit 1..MaxMailList, zero = MaxMailList. Newest first.
type MailQuery struct{ Limit int }

// Accounts is the seed's slice of identity.
type Accounts interface {
	// EnsureAccount is identity's writer-only method: it creates the account (or sets the
	// shared password on an existing one) with no actor.
	EnsureAccount(ctx context.Context, c identity.Credentials) (identity.AccountResult, error)
}

// Access is the seed's slice of authz: the signatures of authz.Service, which satisfies it
// directly.
type Access interface {
	// EnsureGrant is authz's writer-only bootstrap grant (no actor); it returns an existing
	// grant with false.
	EnsureGrant(ctx context.Context, in authz.GrantInput) (authz.Grant, bool, error)
	// Grant grants a role as the demo access-admin.
	Grant(ctx context.Context, in authz.GrantInput) (authz.Grant, error)
	// StarterRoleIDs turns starter role keys into IDs. It is authz's actor-less, read-only
	// lookup: the seed calls it before any demo account holds a role. NotFound for a key
	// that names no starter role.
	StarterRoleIDs(ctx context.Context, keys []string) (map[string]authz.RoleID, error)
}

// Addresses is identity's EmailOf: the login address of the viewer's account, the only
// address ListMail matches. NotFound (no account) gives an empty list.
type Addresses interface {
	EmailOf(ctx context.Context, person model.PersonID) (model.Email, error)
}

// Profiles is the seed's slice of people: the signatures of people.Service, which satisfies
// it directly.
type Profiles interface {
	// UpdateMe fills the signed-in account's own profile (creating a missing row).
	UpdateMe(ctx context.Context, in people.ProfileInput) (people.Profile, error)
	// Create makes a person without an account (a walk-on), as the demo account-admin.
	Create(ctx context.Context, in people.ProfileInput) (people.Profile, error)
}

// Departments is the seed's slice of departments.
type Departments interface {
	Create(ctx context.Context, in departments.CreateInput) (departments.Department, error)
}

// Events is the seed's slice of events.
type Events interface {
	Create(ctx context.Context, in events.CreateInput) (events.Created, error)
	AddScheduleItem(ctx context.Context, e model.EventID, in events.ScheduleInput) (events.ScheduleItem, error)
}

// Settings is the seed's slice of settings.
type Settings interface {
	Set(ctx context.Context, ch settings.Changes) (settings.Revision, error)
}

// Schema is app's migrate step (db.Migrator over every set, then authz.Seed), the same code
// path bms migrate runs.
type Schema interface {
	Migrate(ctx context.Context) error
}

// Objects empties the object store and the files temp directory: files.Service.Purge, which
// satisfies it directly.
type Objects interface {
	Purge(ctx context.Context) (deleted int, err error)
}

// Contributor pairs a module name with its provider (from Set.Contributors(demoseed.Point)).
type Contributor struct {
	Module   string
	Provider demoseed.Provider
}

// Options builds a Service. Validate fails errs.Invalid naming the field (demo.db,
// demo.clock, demo.logger, demo.tracer, demo.audit, demo.actor, demo.files, demo.accounts,
// demo.access, demo.addresses, demo.profiles, demo.departments, demo.events, demo.settings,
// demo.schema, demo.objects, demo.schemas, demo.data_dir, demo.contributor_timeout,
// demo.public_url). PublicURL may be empty.
type Options struct {
	Enabled            bool
	PublicURL          string
	Instance           string // the marker's instance name; "demo" when empty
	DB                 db.DB
	Clock              clock.Clock
	Logger             *slog.Logger
	Tracer             telemetry.Tracer
	Meter              telemetry.Meter // optional: without one nothing is counted
	Audit              audit.Recorder  // scoped "demo"
	Actor              authz.ActorFunc // ListMail's viewer (the signed-in person)
	Accounts           Accounts
	Access             Access
	Addresses          Addresses
	Profiles           Profiles
	Departments        Departments
	Events             Events
	Settings           Settings
	Schema             Schema
	Objects            Objects
	Schemas            []string // every migration set's schema, deduplicated ("core", "volunteers", ...)
	DataDir            string   // for the audit spool directory
	ContributorTimeout time.Duration
}

var instanceRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)

var schemaRE = regexp.MustCompile(`^[a-z][a-z0-9]{1,31}$`)

// Validate fails errs.Invalid joining one validate.FieldError per problem, naming the
// field. Messages never echo a value.
func (o Options) Validate() error {
	v := validate.Errors{Op: "demo.Options.Validate"}
	for _, d := range []struct {
		field string
		isNil bool
	}{
		{"demo.db", o.DB == nil},
		{"demo.clock", o.Clock == nil},
		{"demo.logger", o.Logger == nil},
		{"demo.tracer", o.Tracer == nil},
		{"demo.audit", o.Audit == nil},
		{"demo.actor", o.Actor == nil},
		{"demo.accounts", o.Accounts == nil},
		{"demo.access", o.Access == nil},
		{"demo.addresses", o.Addresses == nil},
		{"demo.profiles", o.Profiles == nil},
		{"demo.departments", o.Departments == nil},
		{"demo.events", o.Events == nil},
		{"demo.settings", o.Settings == nil},
		{"demo.schema", o.Schema == nil},
		{"demo.objects", o.Objects == nil},
	} {
		if d.isNil {
			v.Addf(d.field, validate.CodeRequired, "must be set")
		}
	}
	if len(o.Schemas) == 0 {
		v.Addf("demo.schemas", validate.CodeRequired, "must name at least one schema")
	}
	for _, s := range o.Schemas {
		if !schemaRE.MatchString(s) {
			v.Addf("demo.schemas", validate.CodeMalformed, "each name must be lower-case letters and digits, 2 to 32 bytes")
			break
		}
		if s == "public" {
			// A reset drops every named schema with CASCADE: never the database's default one.
			v.Addf("demo.schemas", validate.CodeOneOf, "must not name the public schema")
			break
		}
	}
	if o.DataDir == "" {
		v.Addf("demo.data_dir", validate.CodeRequired, "must be set")
	}
	if d := o.ContributorTimeout; d < minContributor || d > maxContributor {
		v.Addf("demo.contributor_timeout", validate.CodeRange, "must be between %s and %s", minContributor, maxContributor)
	}
	if o.Instance != "" && !instanceRE.MatchString(o.Instance) {
		v.Addf("demo.instance", validate.CodeMalformed, "must be lower-case letters, digits and hyphens, at most 64 bytes")
	}
	if o.PublicURL != "" && !validPublicURL(o.PublicURL) {
		v.Addf("demo.public_url", validate.CodeURL, "must be an http or https URL with a host, no query, fragment or user information, at most %d bytes", maxURLBytes)
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

func validPublicURL(s string) bool {
	if len(s) > maxURLBytes {
		return false
	}
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" &&
		u.User == nil && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && !strings.Contains(s, "#")
}

// store is the storage the service declares (D8); postgres.Store implements it. Every error
// is already classified by db.Wrap.
type store interface {
	// State returns the marker; NotFound when there is none.
	State(ctx context.Context) (rows.State, error)
	// BeginSeed writes the marker and checks the database is empty, in one Write
	// transaction: Conflict (demo, seeded|not_empty).
	BeginSeed(ctx context.Context, st rows.State) error
	MarkSeeded(ctx context.Context, at time.Time) error
	AddAccount(ctx context.Context, a rows.Account) error
	ListMail(ctx context.Context, address string, limit int) ([]rows.Mail, error)
	DropSchemas(ctx context.Context, schemas []string) error
}

var _ store = (*postgres.Store)(nil)

// Service is demo. Safe for concurrent use; nothing to close; no goroutines. Seed and Reset
// are meant to run one at a time from a command.
type Service struct {
	o       Options
	store   store
	metrics metrics

	mu           sync.Mutex
	contributors []Contributor
	linked       bool
}

// New validates o and builds the service over postgres.Store. No I/O: it works against
// db.None() and on a read-only copy. Fails errs.Invalid.
func New(o Options) (*Service, error) {
	const op = "demo.New"
	if err := o.Validate(); err != nil {
		return nil, err
	}
	st, err := postgres.NewStore(o.DB)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return newService(o, st)
}

// newService builds the service over st. Tests hand it a fake store.
func newService(o Options, st store) (*Service, error) {
	const op = "demo.New"
	if err := o.Validate(); err != nil {
		return nil, err
	}
	if st == nil {
		return nil, errs.E(op, errs.Invalid, errors.New("nil store"))
	}
	s := &Service{o: o, store: st}
	if err := s.metrics.init(o.Meter); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return s, nil
}

// Link gives the service the module contributors, once, from app's link step. Fails
// errs.Internal when called twice or given a nil provider, errs.Invalid for an empty module
// name.
func (s *Service) Link(contributors []Contributor) error {
	const op = "demo.Service.Link"
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.linked {
		return errs.E(op, errs.Internal, errors.New("Link called twice"))
	}
	for _, c := range contributors {
		switch {
		case c.Module == "":
			return errs.E(op, errs.Invalid, errors.New("a contributor has no module name"))
		case c.Provider == nil:
			return errs.E(op, errs.Internal, fmt.Errorf("module %s contributes a nil provider", c.Module))
		}
	}
	s.contributors = append([]Contributor(nil), contributors...)
	s.linked = true
	return nil
}

func (s *Service) contributorList() []Contributor {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Contributor(nil), s.contributors...)
}

// Enabled reports the switch. Cannot fail.
func (s *Service) Enabled() bool { return s.o.Enabled }

// Info builds GET /api/demo from constants. No I/O, cannot fail.
func (s *Service) Info() Info {
	if !s.o.Enabled {
		return Info{}
	}
	out := Info{Enabled: true, Regional: Regional, Password: Password, Accounts: make([]AccountInfo, len(accountTable))}
	for i, a := range accountTable {
		out.Accounts[i] = AccountInfo{Key: a.key, Email: a.key + "@" + Domain, Roles: append([]string(nil), a.roles...)}
	}
	return out
}

// say writes operator text to w (nothing when w is nil); a failed write to the terminal has
// nowhere to be reported, so it is dropped here, once.
func say(w io.Writer, format string, args ...any) {
	if w == nil {
		return
	}
	if _, err := fmt.Fprintf(w, format, args...); err != nil {
		return
	}
}
