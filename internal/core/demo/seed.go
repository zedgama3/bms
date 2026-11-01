package demo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/contracts/demoseed"
	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/demo/rows"
	"github.com/zedgama3/bms/internal/core/departments"
	"github.com/zedgama3/bms/internal/core/events"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

const (
	actionSeedRan  audit.Action = "demo.seed.ran"
	actionResetRan audit.Action = "demo.reset.ran"
	auditReason                 = "command"
)

// instanceName is the marker's instance when Options.Instance is empty.
const instanceName = "demo"

// Seed builds the fictional regional (demo.md "The seed"): the marker, the ten accounts,
// their profiles, grants, the six departments, the upcoming event, the regional settings,
// twelve walk-ons and then each module's own data through demoseed.Point. It acts as the
// demo accounts through the services; only EnsureAccount and one EnsureGrant are actor-less.
// It prints one line per step and the account table to out, records demo.seed.ran as the
// operator and counts bms.demo.seeds{outcome}.
//
// Fails errs.Unsupported when demo is not enabled, errs.Unavailable wrapping db.ErrReadOnly
// on a read-only copy, errs.Conflict (field demo, code seeded or not_empty), errs.Permission
// when a demo actor lacks a permission (the starter roles or the seed table are wrong),
// errs.Timeout when a contributor exceeds ContributorTimeout, errs.Canceled, the recorder's
// kind, and any seam's or contributor's kind wrapped with the step. A failed step leaves the
// marker in "seeding": `bms demo reset` is the recovery.
func (s *Service) Seed(ctx context.Context, out io.Writer) (rep Report, err error) {
	const op = "demo.Service.Seed"
	if !s.o.Enabled {
		return Report{}, errs.E(op, errs.Unsupported, errors.New("demo mode is off; set BMS_DEMO_ENABLED=true"))
	}
	if s.o.DB.ReadOnly() {
		return Report{}, errs.E(op, errs.Unavailable, db.ErrReadOnly)
	}
	ctx, cancel := context.WithTimeout(ctx, seedTimeout)
	defer cancel()
	ctx, span := s.o.Tracer.Start(ctx, "demo.seed")
	defer func() {
		if err != nil {
			span.RecordError(err)
		}
		span.End()
		add(s.metrics.seeds, ctx, outcomeAttr(err))
	}()

	r := &run{s: s, out: out, accounts: map[string]identity.AccountResult{}, people: map[string]model.PersonID{}, depts: map[string]model.DepartmentID{}}
	r.now = s.o.Clock.Now()
	if err := s.begin(ctx, r.now); err != nil {
		return Report{}, err
	}
	steps := []struct {
		name string
		fn   func(context.Context) (int, error)
	}{
		{"accounts", r.accountsStep},
		{"profiles", r.profilesStep},
		{"grants", r.grantsStep},
		{"departments", r.departmentsStep},
		{"event", r.eventStep},
		{"settings", r.settingsStep},
		{"walk-ons", r.walkOnsStep},
	}
	for _, st := range steps {
		if err := r.step(ctx, st.name, st.fn); err != nil {
			return Report{Steps: r.steps}, err
		}
	}
	for _, c := range s.contributorList() {
		if err := r.step(ctx, "module:"+c.Module, r.contributorStep(c)); err != nil {
			return Report{Steps: r.steps}, err
		}
	}
	if err := checkCtx(op, ctx); err != nil {
		return Report{Steps: r.steps}, err
	}
	if err := s.store.MarkSeeded(ctx, s.o.Clock.Now()); err != nil {
		return Report{Steps: r.steps}, fmt.Errorf("%s: mark seeded: %w", op, err)
	}
	rep = Report{Steps: r.steps}
	if err := s.recordRan(ctx, actionSeedRan, rep); err != nil {
		return rep, err
	}
	s.printAccounts(out)
	return rep, nil
}

// begin writes the marker (step 0): Conflict seeded or not_empty, the store's kinds.
func (s *Service) begin(ctx context.Context, now time.Time) error {
	const op = "demo.Service.Seed"
	if err := checkCtx(op, ctx); err != nil {
		return err
	}
	inst := s.o.Instance
	if inst == "" {
		inst = instanceName
	}
	if err := s.store.BeginSeed(ctx, rows.State{SeedVersion: SeedVersion, Instance: inst, StartedAt: now.UTC()}); err != nil {
		return fmt.Errorf("%s: marker: %w", op, err)
	}
	return nil
}

// checkCtx turns an ended ctx into errs.Canceled or errs.Timeout.
func checkCtx(op string, ctx context.Context) error { //nolint:revive // ctx is checked, not passed on
	err := ctx.Err()
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.DeadlineExceeded):
		return errs.E(op, errs.Timeout, err)
	default:
		return errs.E(op, errs.Canceled, err)
	}
}

var nonFieldRE = regexp.MustCompile(`[^a-z0-9_]`)

// recordRan records the operator's "ran" entry, with the step names as the fields.
func (s *Service) recordRan(ctx context.Context, action audit.Action, rep Report) error {
	fields := make([]string, 0, len(rep.Steps))
	seen := map[string]bool{}
	for _, st := range rep.Steps {
		f := nonFieldRE.ReplaceAllString(strings.ToLower(st.Name), "_")
		if len(f) > audit.MaxFieldBytes || seen[f] {
			continue
		}
		seen[f] = true
		fields = append(fields, f)
	}
	e := audit.Entry{
		Action:  action,
		Outcome: audit.Succeeded,
		Actor:   audit.Actor{Kind: audit.ActorOperator},
		Fields:  fields,
		Level:   model.SensitivityInternal,
		Reason:  auditReason,
	}
	if len(fields) == 0 {
		e.Level = 0
	}
	if err := s.o.Audit.Record(ctx, e); err != nil {
		return fmt.Errorf("demo.Service.recordRan: %w", err)
	}
	return nil
}

// printAccounts prints the account table: addresses and the password, nothing else.
func (s *Service) printAccounts(out io.Writer) {
	say(out, "\ndemo accounts (every password is the same):\n")
	for _, a := range accountTable {
		say(out, "  %s@%s\n", a.key, Domain)
	}
	say(out, "password: %s\n", Password)
}

// run is the state of one seed.
type run struct {
	s        *Service
	out      io.Writer
	now      time.Time
	steps    []Step
	accounts map[string]identity.AccountResult // by actor key
	people   map[string]model.PersonID         // actors and walk-ons, by key
	depts    map[string]model.DepartmentID
	event    model.EventID
	roles    map[string]authz.RoleID
}

// step runs fn under its own span, wraps its error with the step's name, records the Step,
// logs the count and prints the line.
func (r *run) step(ctx context.Context, name string, fn func(context.Context) (int, error)) (err error) {
	if err := checkCtx("demo.Service.Seed", ctx); err != nil {
		return err
	}
	ctx, span := r.s.o.Tracer.Start(ctx, "demo.seed.step")
	span.SetAttrs(telemetry.Token("step", name))
	defer func() {
		if err != nil {
			span.RecordError(err)
		}
		span.End()
	}()
	n, err := fn(ctx)
	if err != nil {
		return fmt.Errorf("demo seed step %s: %w", name, err)
	}
	r.steps = append(r.steps, Step{Name: name, Items: n})
	r.s.o.Logger.InfoContext(ctx, "demo seed step", slog.String("step", name), slog.Int("items", n))
	say(r.out, "%-18s %d\n", name, n)
	return nil
}

// As builds the session of the named demo account and returns ctx carrying it
// (demoseed.Actors). Fails errs.Invalid for an unknown key.
func (r *run) As(ctx context.Context, key string) (context.Context, error) {
	acc, ok := r.accounts[key]
	if !ok {
		return nil, errs.E("demo.Actors.As", errs.Invalid, &validate.FieldError{Field: "actor", Code: validate.CodeOneOf, Reason: "unknown demo actor"})
	}
	now := r.s.o.Clock.Now()
	return identity.WithSession(ctx, identity.Session{
		ID:        model.NewID[identity.SessionID](),
		Account:   acc.Account,
		Person:    acc.Person,
		IssuedAt:  now,
		ExpiresAt: now.Add(SessionLifetime),
	}), nil
}

// asKey is As for a key the seed itself knows exists, wrapped with the key.
func (r *run) asKey(ctx context.Context, key string) (context.Context, error) {
	c, err := r.As(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("actor %s: %w", key, err)
	}
	return c, nil
}

var _ demoseed.Actors = (*run)(nil)

// accountsStep creates the ten accounts and records them as demo accounts.
func (r *run) accountsStep(ctx context.Context) (int, error) {
	for i, a := range accountTable {
		res, err := r.s.o.Accounts.EnsureAccount(ctx, identity.Credentials{Email: model.Email(a.key + "@" + Domain), Password: Password})
		if err != nil {
			return i, fmt.Errorf("account %s: %w", a.key, err)
		}
		r.accounts[a.key] = res
		r.people[a.key] = res.Person
		if err := r.s.store.AddAccount(ctx, rows.Account{
			Account: res.Account, Person: res.Person, Key: a.key, Email: a.key + "@" + Domain, Position: i,
		}); err != nil {
			return i, fmt.Errorf("account %s: record: %w", a.key, err)
		}
	}
	return len(accountTable), nil
}

func input(p profile) people.ProfileInput {
	in := people.ProfileInput{PlayaName: strp(p.playa), Pronouns: strp(p.pronouns)}
	if p.legal != "" {
		in.LegalName = strp(p.legal)
	}
	if p.phone != "" {
		in.Phone = strp(p.phone)
	}
	return in
}

// profilesStep fills each account's own profile as that account. The newcomer keeps an
// empty one, to show the first-run prompt.
func (r *run) profilesStep(ctx context.Context) (int, error) {
	n := 0
	for _, a := range accountTable {
		if a.profile == nil {
			continue
		}
		actx, err := r.asKey(ctx, a.key)
		if err != nil {
			return n, err
		}
		if _, err := r.s.o.Profiles.UpdateMe(actx, input(*a.profile)); err != nil {
			return n, fmt.Errorf("profile %s: %w", a.key, err)
		}
		n++
	}
	return n, nil
}

// grantsStep makes the one actor-less grant (access-admin) and then the others as the
// access-admin.
func (r *run) grantsStep(ctx context.Context) (int, error) {
	keys := []string{authz.RoleAccessAdmin, authz.RoleDepartmentAdmin}
	for _, g := range grantTable {
		if !slices.Contains(keys, g.role) {
			keys = append(keys, g.role)
		}
	}
	ids, err := r.s.o.Access.StarterRoleIDs(ctx, keys)
	if err != nil {
		return 0, fmt.Errorf("starter roles: %w", err)
	}
	r.roles = ids
	role := func(key string) (authz.RoleID, error) {
		id, ok := r.roles[key]
		if !ok {
			return authz.RoleID{}, errs.E("demo.Service.Seed", errs.NotFound, fmt.Errorf("starter role %s is missing", key))
		}
		return id, nil
	}
	adminRole, err := role(authz.RoleAccessAdmin)
	if err != nil {
		return 0, err
	}
	if _, _, err := r.s.o.Access.EnsureGrant(ctx, authz.GrantInput{Person: r.people[demoseed.ActorAccessAdmin], Role: adminRole}); err != nil {
		return 0, fmt.Errorf("bootstrap grant %s: %w", demoseed.ActorAccessAdmin, err)
	}
	n := 1
	actx, err := r.asKey(ctx, demoseed.ActorAccessAdmin)
	if err != nil {
		return n, err
	}
	for _, g := range grantTable {
		id, err := role(g.role)
		if err != nil {
			return n, err
		}
		if _, err := r.s.o.Access.Grant(actx, authz.GrantInput{Person: r.people[g.key], Role: id}); err != nil {
			return n, fmt.Errorf("grant %s: %w", g.key, err)
		}
		n++
	}
	return n, nil
}

// addGrants adds n to the count of the grants step, whose line was printed before the lead's
// department grants could be made (the departments did not exist yet): the Report counts
// every grant the seed made.
func (r *run) addGrants(n int) {
	for i := range r.steps {
		if r.steps[i].Name == "grants" {
			r.steps[i].Items += n
			return
		}
	}
}

// departmentsStep creates the six departments as the steward and then the lead's role at
// five of them as the access-admin.
func (r *run) departmentsStep(ctx context.Context) (int, error) {
	actx, err := r.asKey(ctx, demoseed.ActorSteward)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, d := range departmentTable {
		created, err := r.s.o.Departments.Create(actx, departments.CreateInput{
			Name: d.name, Description: d.description, ContactEmail: d.key + "@" + Domain,
		})
		if err != nil {
			return n, fmt.Errorf("department %s: %w", d.key, err)
		}
		r.depts[d.key] = created.ID
		n++
	}
	role, ok := r.roles[authz.RoleDepartmentAdmin]
	if !ok {
		return n, errs.E("demo.Service.Seed", errs.NotFound, fmt.Errorf("starter role %s is missing", authz.RoleDepartmentAdmin))
	}
	actx, err = r.asKey(ctx, demoseed.ActorAccessAdmin)
	if err != nil {
		return n, err
	}
	// The lead leads the five departments that get volunteer data (amendment D1.1);
	// Sanctuary stays without a lead on purpose.
	granted := 0
	for _, d := range leadDepartments {
		if _, err := r.s.o.Access.Grant(actx, authz.GrantInput{
			Person: r.people[demoseed.ActorLead], Role: role, Department: r.depts[d],
		}); err != nil {
			return n, fmt.Errorf("grant %s at %s: %w", demoseed.ActorLead, d, err)
		}
		granted++
		r.addGrants(1)
	}
	say(r.out, "%-18s %d\n", "lead grants", granted)
	return n, nil
}

// EventDates returns the dates of the seeded event as seen from now in the named zone:
// 10:00 on the first Thursday at least 21 days after the day of now, to 12:00 the following
// Monday. The core's event step uses it with Zone; moduletest's demoseed_provider_valid uses
// it with "UTC" for the fake event it registers, so a provider is always given dates. Fails
// errs.Invalid for a zone time.LoadLocation does not know.
func EventDates(now time.Time, zone string) (model.TimeRange, error) {
	start, at, err := eventDates(now, zone)
	if err != nil {
		return model.TimeRange{}, err
	}
	return model.TimeRange{Start: start, End: at(4, 12, 0)}, nil
}

// eventDates returns the event's start and a function for the local time of day, days after
// the start's day.
func eventDates(now time.Time, zone string) (start time.Time, at func(days, hour, minute int) time.Time, err error) {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return time.Time{}, nil, errs.E("demo.EventDates", errs.Invalid, fmt.Errorf("zone %s: %w", zone, err))
	}
	local := now.In(loc)
	day := time.Date(local.Year(), local.Month(), local.Day()+21, 0, 0, 0, 0, loc)
	for day.Weekday() != time.Thursday {
		day = time.Date(day.Year(), day.Month(), day.Day()+1, 0, 0, 0, 0, loc)
	}
	at = func(days, hour, minute int) time.Time {
		return time.Date(day.Year(), day.Month(), day.Day()+days, hour, minute, 0, 0, loc)
	}
	return at(0, 10, 0), at, nil
}

// eventStep creates the upcoming event and its schedule as the event-admin.
func (r *run) eventStep(ctx context.Context) (int, error) {
	start, at, err := eventDates(r.now, Zone)
	if err != nil {
		return 0, err
	}
	actx, err := r.asKey(ctx, demoseed.ActorEventAdmin)
	if err != nil {
		return 0, err
	}
	created, err := r.s.o.Events.Create(actx, events.CreateInput{
		Name:  fmt.Sprintf("%s %d", Regional, start.Year()),
		Kind:  events.KindBurn,
		Zone:  Zone,
		Dates: model.TimeRange{Start: start, End: at(4, 12, 0)},
	})
	if err != nil {
		return 0, fmt.Errorf("event: %w", err)
	}
	r.event = created.Event.ID
	items := []events.ScheduleInput{
		{Key: "gate-open", Label: "Gate hours", Starts: at(0, 10, 0), Ends: at(3, 18, 0)},
		{Key: "effigy-burn", Label: "Effigy burn", Starts: at(2, 21, 0)},
		{Key: "temple-burn", Label: "Temple burn", Starts: at(3, 20, 30)},
		{Key: "exodus", Label: "Exodus deadline", Starts: at(4, 12, 0)},
	}
	n := 1
	for _, it := range items {
		if _, err := r.s.o.Events.AddScheduleItem(actx, r.event, it); err != nil {
			return n, fmt.Errorf("schedule item %s: %w", it.Key, err)
		}
		n++
	}
	return n, nil
}

// settingsStep writes the regional's identity and the SMTP values (which only make
// notify.Configured true) as the settings-admin, in one batch.
func (r *run) settingsStep(ctx context.Context) (int, error) {
	actx, err := r.asKey(ctx, demoseed.ActorSettingsAdmin)
	if err != nil {
		return 0, err
	}
	vals := map[string]*string{
		"regional.name":         strp(Regional),
		"regional.language":     strp("en"),
		"regional.timezone":     strp(Zone),
		"notify.smtp_host":      strp(Domain),
		"notify.sender_address": strp("noreply@" + Domain),
		"notify.sender_name":    strp(Regional + " (demo)"),
	}
	if r.s.o.PublicURL != "" {
		vals["regional.public_url"] = strp(r.s.o.PublicURL)
	}
	if _, err := r.s.o.Settings.Set(actx, settings.Changes{Values: vals}); err != nil {
		return 0, fmt.Errorf("set: %w", err)
	}
	return len(vals), nil
}

// walkOnsStep creates the twelve people without an account as the account-admin.
func (r *run) walkOnsStep(ctx context.Context) (int, error) {
	actx, err := r.asKey(ctx, demoseed.ActorAccountAdmin)
	if err != nil {
		return 0, err
	}
	for n := 1; n <= demoseed.WalkOns; n++ {
		p, err := r.s.o.Profiles.Create(actx, input(walkOn(n)))
		if err != nil {
			return n - 1, fmt.Errorf("walk-on %02d: %w", n, err)
		}
		r.people[demoseed.WalkOnKey(n)] = p.Person.ID
	}
	return demoseed.WalkOns, nil
}

// contributorStep runs one module's provider under ContributorTimeout.
func (r *run) contributorStep(c Contributor) func(context.Context) (int, error) {
	return func(ctx context.Context) (int, error) {
		cctx, cancel := context.WithTimeout(ctx, r.s.o.ContributorTimeout)
		defer cancel()
		req := demoseed.Request{
			Event:       r.event,
			Departments: maps.Clone(r.depts),
			People:      maps.Clone(r.people),
			Actors:      r,
		}
		res, err := c.Provider.Seed(cctx, req)
		if cerr := cctx.Err(); errors.Is(cerr, context.DeadlineExceeded) && ctx.Err() == nil {
			return 0, errs.E("demo.Service.Seed", errs.Timeout, fmt.Errorf("module %s: exceeded %s: %w", c.Module, r.s.o.ContributorTimeout, cerr))
		}
		if err != nil {
			return 0, fmt.Errorf("module %s: %w", c.Module, err)
		}
		return res.Items, nil
	}
}
