// Package volunteers is the first feature module: it staffs a department's shifts with
// volunteers who hold what the position needs (plan/design/module-volunteers.md). One
// *Service holds six parts: the qualification catalogue, verified qualification records,
// positions, membership, shifts and sign-ups.
package volunteers

import (
	"context"
	"embed" // the OpenAPI fragment and the templates
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/zedgama3/bms/internal/contracts/demoseed"
	"github.com/zedgama3/bms/internal/contracts/eventsetup"
	"github.com/zedgama3/bms/internal/contracts/membership"
	"github.com/zedgama3/bms/internal/contracts/personpage"
	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/departments"
	"github.com/zedgama3/bms/internal/core/events"
	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/core/privacy"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/volunteers/postgres"
	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
)

// The permissions the module declares (module-volunteers.md, "Permissions").
const (
	// PermissionSignUp lets a person book themself into a shift. Scope site.
	PermissionSignUp = vol.PermissionSignUp
	// PermissionShiftsManage creates, changes, cancels and copies shifts, assigns people
	// and cancels anyone's sign-up. Scope department.
	PermissionShiftsManage = vol.PermissionShiftsManage
	// PermissionRostersRead reads a shift's roster and a department's members. Scope department.
	PermissionRostersRead = vol.PermissionRostersRead
	// PermissionPositionsManage manages positions and their requirements. Scope department.
	PermissionPositionsManage = vol.PermissionPositionsManage
	// PermissionMembersManage adds and removes members. Scope department.
	PermissionMembersManage = vol.PermissionMembersManage
	// PermissionQualificationsManage manages the catalogue (a department's entries at the
	// department; org-wide entries site-wide). Scope department.
	PermissionQualificationsManage = vol.PermissionQualificationsManage
	// PermissionQualificationsRead reads another person's records and certificate. Scope department.
	PermissionQualificationsRead = vol.PermissionQualificationsRead
	// PermissionQualificationsVerify records, verifies, rejects and removes records. Scope department.
	PermissionQualificationsVerify = vol.PermissionQualificationsVerify
)

// The setting keys (bare; settings stores them as "volunteers.<key>").
const (
	SettingReminderLead         = "reminder_lead"
	SettingCancelCutoff         = "cancel_cutoff"
	SettingExpiryNoticeDays     = "expiry_notice_days"
	SettingSelfUpload           = "self_upload"
	SettingVisibilityShiftMates = "visibility_shift_mates"
	SettingVisibilityLegalName  = "visibility_legal_name"
	SettingRetention            = "retention"
)

// The signals the module publishes, and their payload keys. A payload carries record
// references only.
const (
	SignalSignupBooked          = "volunteers.signup-booked"
	SignalSignupCanceled        = "volunteers.signup-canceled"
	SignalQualificationVerified = "volunteers.qualification-verified"

	PayloadSignup        = "signup"
	PayloadShift         = "shift"
	PayloadPerson        = "person"
	PayloadDepartment    = "department"
	PayloadRecord        = "record"
	PayloadQualification = "qualification"
)

// The jobs the module registers.
const (
	JobShiftReminder = "volunteers.shift-reminder" // deferred, keyed by the sign-up
	JobExpiryScan    = "volunteers.expiry-scan"    // daily
	JobPurge         = "volunteers.purge"          // daily
	JobCancelShifts  = "volunteers.cancel-shifts"  // on demand, from the two signal handlers
)

// The notification templates (templates/<ident>.<lang>.tmpl).
const (
	TemplateShiftReminder         = "volunteers.shift-reminder"
	TemplateQualificationExpiring = "volunteers.qualification-expiring"
	TemplateQualificationVerified = "volunteers.qualification-verified"
	TemplateQualificationRejected = "volunteers.qualification-rejected"
	TemplateSignupRemoved         = "volunteers.signup-removed"
)

// RecordKind is the files record kind of a qualification record's certificate.
const RecordKind = "volunteers.qualification-record"

// Manifest declares the module to the core. It carries no text (module-contract decision 8).
var Manifest = module.Manifest{
	Name:       "volunteers",
	Migrations: postgres.Migrations,
	Templates:  templates,
	Permissions: []authz.PermissionDecl{
		{Name: PermissionSignUp, Scope: authz.ScopeSite, Reaches: model.SensitivityInternal,
			StarterRoles: []string{authz.RoleParticipant}},
		{Name: PermissionShiftsManage, Scope: authz.ScopeDepartment, Reaches: model.SensitivityInternal,
			StarterRoles: []string{authz.RoleDepartmentAdmin}},
		{Name: PermissionRostersRead, Scope: authz.ScopeDepartment, Reaches: model.SensitivityInternal,
			StarterRoles: []string{authz.RoleDepartmentAdmin, authz.RoleVolunteerCoordinator}},
		{Name: PermissionPositionsManage, Scope: authz.ScopeDepartment, Reaches: model.SensitivityInternal,
			StarterRoles: []string{authz.RoleDepartmentAdmin}},
		{Name: PermissionMembersManage, Scope: authz.ScopeDepartment, Reaches: model.SensitivityInternal,
			StarterRoles: []string{authz.RoleDepartmentAdmin}},
		{Name: PermissionQualificationsManage, Scope: authz.ScopeDepartment, Reaches: model.SensitivityInternal,
			StarterRoles: []string{authz.RoleDepartmentAdmin, authz.RoleDepartmentSteward}},
		{Name: PermissionQualificationsRead, Scope: authz.ScopeDepartment, Reaches: model.SensitivityPersonal,
			StarterRoles: []string{authz.RoleDepartmentAdmin, authz.RoleVolunteerCoordinator}},
		{Name: PermissionQualificationsVerify, Scope: authz.ScopeDepartment, Reaches: model.SensitivityPersonal,
			StarterRoles: []string{authz.RoleDepartmentAdmin}}, // D41: never participant
	},
	Settings:      manifestSettings(),
	Publishes:     []signals.Name{SignalSignupBooked, SignalSignupCanceled, SignalQualificationVerified},
	Subscribes:    []signals.Name{departments.SignalArchived, events.SignalClosed},
	ContributesTo: []module.ExtensionPoint{personpage.Point, eventsetup.Point, membership.Point, demoseed.Point},
	Views:         vol.Views(),
	Groups:        vol.Groups,
}

func init() { module.Register(Manifest, New) }

//go:embed openapi.json
var openAPIFragment []byte

//go:embed templates/*.tmpl
var embeddedTemplates embed.FS

// templates is Manifest.Templates: the files of templates/, rooted at it.
var templates = mustSub(embeddedTemplates, "templates")

// mustSub returns the sub-tree of fsys at dir. It panics only when the embedded directory is
// missing, which is a build-time bug, never a run-time input.
func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic("volunteers: embedded " + dir + " missing: " + err.Error())
	}
	return sub
}

// manifestSettings returns the seven settings: five of its own and the two visibility
// settings of the privacy groups.
func manifestSettings() []module.SettingDecl {
	vis, err := privacy.Settings(vol.Groups)
	if err != nil {
		panic("volunteers: the privacy groups are invalid: " + err.Error()) // a build-time bug
	}
	out := []module.SettingDecl{
		{Key: SettingReminderLead, Kind: module.SettingDuration, Default: "24h", Check: checkReminderLead},
		{Key: SettingCancelCutoff, Kind: module.SettingDuration, Default: "0", Check: checkCancelCutoff},
		{Key: SettingExpiryNoticeDays, Kind: module.SettingInt, Default: "30", Check: checkExpiryNoticeDays},
		{Key: SettingSelfUpload, Kind: module.SettingBool, Default: "true"},
	}
	out = append(out, vis...)
	return append(out, module.SettingDecl{Key: SettingRetention, Kind: module.SettingDuration, Default: "0", Check: checkRetention})
}

func parseDuration(field, value string) (time.Duration, error) {
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, fieldProblem("volunteers.setting", field, validate.CodeMalformed, "must be a duration such as 24h", nil)
	}
	return d, nil
}

// checkReminderLead: 0 (no reminders) or 1h..168h.
func checkReminderLead(value string) error {
	d, err := parseDuration(SettingReminderLead, value)
	if err != nil {
		return err
	}
	if d == 0 {
		return nil
	}
	return validate.Range(SettingReminderLead, d, time.Hour, 168*time.Hour) //nolint:wrapcheck // validate.Range carries errs.Invalid
}

// checkCancelCutoff: 0..168h.
func checkCancelCutoff(value string) error {
	d, err := parseDuration(SettingCancelCutoff, value)
	if err != nil {
		return err
	}
	return validate.Range(SettingCancelCutoff, d, 0, 168*time.Hour) //nolint:wrapcheck // validate.Range carries errs.Invalid
}

// checkExpiryNoticeDays: 0 (no notice)..365.
func checkExpiryNoticeDays(value string) error {
	var n int
	if _, err := fmt.Sscanf(value, "%d", &n); err != nil {
		return fieldProblem("volunteers.setting", SettingExpiryNoticeDays, validate.CodeMalformed, "must be a whole number", nil)
	}
	return validate.Range(SettingExpiryNoticeDays, n, 0, 365) //nolint:wrapcheck // validate.Range carries errs.Invalid
}

// checkRetention: 0 (forever) or 8760h..876000h, as audit.retention.
func checkRetention(value string) error {
	d, err := parseDuration(SettingRetention, value)
	if err != nil {
		return err
	}
	if d == 0 {
		return nil
	}
	return validate.Range(SettingRetention, d, 8760*time.Hour, 876000*time.Hour) //nolint:wrapcheck // validate.Range carries errs.Invalid
}

var (
	errNoActor = errors.New("no signed-in person in the request context")
	errStarted = errors.New("the module is already started")
	errNilDep  = errors.New("nil dependency")
)

// actorFunc reads the signed-in person from ctx: identity.PersonFrom, which is false with no
// session, for a restricted session and inside a signal delivery or a job.
type actorFunc func(ctx context.Context) (model.PersonID, bool)

// Service is the volunteers module. It implements module.Module, module.RouteProvider,
// module.APIDescriber, module.Contributor (personpage.Provider as Sections, eventsetup.Provider
// as SetUp, membership.Provider as DepartmentsOf, demoseed.Provider through demoSeeder) and
// module.PersonData, and the six use-case groups of the spec (Qualifications, Records, Positions, Members, Shifts, Signups). It is
// safe for concurrent use; Stop is a no-op.
type Service struct {
	store    store
	actor    actorFunc
	db       db.DB
	az       authz.Authorizer
	people   people.Directory
	depts    departments.Directory
	events   events.Directory
	settings settings.Reader
	notify   notify.Notifier
	jobs     jobs.Registrar
	files    files.Store
	audit    audit.Recorder
	bus      signals.Bus
	logger   *slog.Logger
	tracer   telemetry.Tracer
	clock    clock.Clock

	mu      sync.Mutex // guards started
	started bool
	mailOff sync.Once // "e-mail is not set up" is logged once

	legalNamesRefused sync.Once // "people refused the legal names" is logged once
}

// New builds the module from deps. It reads nothing from the database (app.md decision 7)
// and starts nothing. Fails errs.Invalid naming every nil dependency among Logger, Tracer,
// Clock, DB, Signals, Audit, Authz, People, Departments, Events, Settings, Notify, Jobs and
// Files, or when the store cannot be built. On error the returned module is nil.
func New(_ context.Context, deps module.Deps) (module.Module, error) {
	const op = "volunteers.New"
	if err := checkDeps(op, deps); err != nil {
		return nil, err
	}
	st, err := postgres.NewStore(deps.DB)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	s, err := newService(deps, st, identity.PersonFrom)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// newService assembles the service over a store and an actor seam (tests inject fakes).
func newService(deps module.Deps, st store, actor actorFunc) (*Service, error) {
	const op = "volunteers.newService"
	if err := checkDeps(op, deps); err != nil {
		return nil, err
	}
	if st == nil || actor == nil {
		return nil, errs.E(op, errs.Invalid, fmt.Errorf("%w: store or actor", errNilDep))
	}
	return &Service{
		store: st, actor: actor, db: deps.DB, az: deps.Authz, people: deps.People, depts: deps.Departments,
		events: deps.Events, settings: deps.Settings, notify: deps.Notify, jobs: deps.Jobs, files: deps.Files,
		audit: deps.Audit, bus: deps.Signals, logger: deps.Logger, tracer: deps.Tracer, clock: deps.Clock,
	}, nil
}

// checkDeps fails errs.Invalid naming every nil dependency.
func checkDeps(op string, d module.Deps) error {
	var nils []string
	for _, c := range []struct {
		name string
		nil  bool
	}{
		{"Logger", d.Logger == nil}, {"Tracer", d.Tracer == nil}, {"Clock", d.Clock == nil}, {"DB", d.DB == nil},
		{"Signals", d.Signals == nil}, {"Audit", d.Audit == nil}, {"Authz", d.Authz == nil}, {"People", d.People == nil},
		{"Departments", d.Departments == nil}, {"Events", d.Events == nil}, {"Settings", d.Settings == nil},
		{"Notify", d.Notify == nil}, {"Jobs", d.Jobs == nil}, {"Files", d.Files == nil},
	} {
		if c.nil {
			nils = append(nils, c.name)
		}
	}
	if len(nils) > 0 {
		return errs.E(op, errs.Invalid, fmt.Errorf("%w: %s", errNilDep, strings.Join(nils, ", ")))
	}
	return nil
}

// Start subscribes to departments.department-archived and events.event-closed and registers
// the four jobs. It performs no I/O and never writes, so it succeeds on a read-only copy.
// Fails errs.Conflict when called twice, and with the kind of whatever Subscribe or Register
// refused.
func (s *Service) Start(ctx context.Context) error {
	const op = "volunteers.Start"
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return errs.E(op, errs.Conflict, errStarted)
	}
	if err := s.bus.Subscribe(departments.SignalArchived, s.onDepartmentArchived); err != nil {
		return fmt.Errorf("%s: subscribe %s: %w", op, departments.SignalArchived, err)
	}
	if err := s.bus.Subscribe(events.SignalClosed, s.onEventClosed); err != nil {
		return fmt.Errorf("%s: subscribe %s: %w", op, events.SignalClosed, err)
	}
	for _, d := range []jobs.Decl{
		{Name: JobShiftReminder, Timeout: 30 * time.Second, Handler: s.runShiftReminder},
		{Name: JobExpiryScan, Every: 24 * time.Hour, Timeout: 5 * time.Minute, Handler: s.runExpiryScan},
		{Name: JobPurge, Every: 24 * time.Hour, Timeout: 5 * time.Minute, Handler: s.runPurge},
		{Name: JobCancelShifts, Timeout: 5 * time.Minute, Handler: s.runCancelShifts},
	} {
		if err := s.jobs.Register(d); err != nil {
			return fmt.Errorf("%s: register %s: %w", op, d.Name, err)
		}
	}
	s.started = true
	s.logger.DebugContext(ctx, "volunteers started")
	return nil
}

// Stop ends nothing: the module runs no goroutines. It fails errs.Canceled or errs.Timeout
// only when ctx is already done, as the module contract asks; otherwise nil.
func (s *Service) Stop(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return errs.E("volunteers.Stop", errs.KindOf(err), err)
	}
	return nil
}

// OpenAPI returns the embedded openapi.json (api.md decision 11).
func (s *Service) OpenAPI() []byte { return append([]byte(nil), openAPIFragment...) }

// Contributions contributes the person-page section, the event set-up provider, the
// membership provider and the demo seed provider (demo.go).
func (s *Service) Contributions() map[module.ExtensionPoint]any {
	return map[module.ExtensionPoint]any{
		personpage.Point: personpage.Provider(s),
		eventsetup.Point: eventsetup.Provider(s),
		membership.Point: membership.Provider(s),
		demoseed.Point:   demoseed.Provider(demoSeeder{s: s}),
	}
}

var (
	_ module.Module        = (*Service)(nil)
	_ module.RouteProvider = (*Service)(nil)
	_ module.APIDescriber  = (*Service)(nil)
	_ module.Contributor   = (*Service)(nil)
)
