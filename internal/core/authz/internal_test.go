package authz

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit/audittest"
	"github.com/zedgama3/bms/internal/core/authz/rows"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/signals/signalstest"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// Everything here is invented: people and departments are random IDs, permissions belong
// to pretend modules.

const (
	pShiftManage Permission = "dept.shifts.manage"
	pShiftRead   Permission = "dept.shifts.read"
	pMedRead     Permission = "med.reports.read"
)

// fakeStore answers the queries a test sets up and records what it was asked. It embeds
// the store interface, so a method a test did not expect panics on the nil interface.
type fakeStore struct {
	store
	held    []rows.Held
	rules   []rows.HeldRule
	grants  []rows.Grant
	filter  rows.GrantFilter
	seedK   []string
	seedP   []string
	queries int
}

func (f *fakeStore) EffectivePermissions(context.Context, model.PersonID, time.Time) ([]rows.Held, error) {
	f.queries++
	return f.held, nil
}

func (f *fakeStore) EffectiveFor(_ context.Context, _ model.PersonID, _ time.Time, p string) ([]rows.Held, error) {
	f.queries++
	var out []rows.Held
	for _, h := range f.held {
		if h.Permission == p {
			out = append(out, h)
		}
	}
	return out, nil
}

func (f *fakeStore) RulesHeldBy(context.Context, model.PersonID, time.Time) ([]rows.HeldRule, error) {
	f.queries++
	return f.rules, nil
}

func (f *fakeStore) ListGrants(_ context.Context, q rows.GrantFilter) ([]rows.Grant, error) {
	f.filter = q
	return f.grants, nil
}

func (f *fakeStore) LockInvariants(context.Context) error { return nil }

func (f *fakeStore) SeedStarterPermissions(_ context.Context, keys, perms []string, _ time.Time) (int, error) {
	f.seedK, f.seedP = keys, perms
	return len(keys), nil
}

type ctxActor struct{}

func actorOf(ctx context.Context) (Actor, bool) {
	a, ok := ctx.Value(ctxActor{}).(Actor)
	return a, ok
}

func asPerson(p model.PersonID) context.Context {
	return context.WithValue(context.Background(), ctxActor{}, Actor{Person: p})
}

func declaredForTests() []PermissionDecl {
	return append(slices.Clone(Permissions),
		PermissionDecl{Name: pShiftManage, Scope: ScopeDepartment, Reaches: model.SensitivityInternal},
		PermissionDecl{Name: pShiftRead, Scope: ScopeDepartment, Reaches: model.SensitivityInternal},
		PermissionDecl{Name: pMedRead, Scope: ScopeSite, Reaches: model.SensitivitySensitive},
	)
}

func newTestService(t *testing.T, st store) *Service {
	t.Helper()
	c := telemetrytest.New(t)
	bus, err := signalstest.NewFake("access", []signals.Name{SignalGrantCreated, SignalGrantRevoked}, nil)
	if err != nil {
		t.Fatalf("signalstest.NewFake: %v", err)
	}
	s, err := newService(Options{
		DB: dbtest.NewFake(), Clock: clock.NewFake(time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)),
		Logger: c.Logger(), Tracer: c.Tracer("authz"), Audit: audittest.NewFake("access"), Signals: bus,
		Actor: actorOf, Declared: declaredForTests(),
	}, st)
	if err != nil {
		t.Fatalf("newService: %v", err)
	}
	return s
}

func TestLoad_RuleRemovesThePermissionInEveryExtent(t *testing.T) {
	d1 := model.NewID[model.DepartmentID]()
	fs := &fakeStore{
		held: []rows.Held{
			{Permission: string(pShiftManage)},                 // site-wide
			{Permission: string(pShiftManage), Department: d1}, // and for one department
			{Permission: string(pShiftRead), Department: d1},
		},
		rules: []rows.HeldRule{{ID: model.NewID[rows.RuleID](), Role: model.NewID[rows.RoleID](), Permission: string(pShiftManage)}},
	}
	s := newTestService(t, fs)
	acc, err := s.load(t.Context(), model.NewID[model.PersonID](), "")
	if err != nil {
		t.Fatal(err)
	}
	if !acc.extent(pShiftManage).IsZero() {
		t.Errorf("forbidden permission survives: %+v", acc.extent(pShiftManage))
	}
	if !acc.at(pShiftRead, d1) || acc.site(pShiftRead) {
		t.Errorf("unrelated permission changed: %+v", acc.extent(pShiftRead))
	}
}

func TestLoad_NothingHeld_SkipsTheRulesQuery(t *testing.T) {
	fs := &fakeStore{}
	s := newTestService(t, fs)
	if _, err := s.load(t.Context(), model.NewID[model.PersonID](), ""); err != nil {
		t.Fatal(err)
	}
	if fs.queries != 1 {
		t.Errorf("%d statements for an empty person, want 1", fs.queries)
	}
}

func TestForbiddenBy_LevelRuleCoversDeclaredAtOrAbove(t *testing.T) {
	s := newTestService(t, &fakeStore{})
	got, err := s.forbiddenBy(rows.HeldRule{Reaches: "sensitive"})
	if err != nil || !slices.Equal(got, []string{string(pMedRead)}) {
		t.Errorf("sensitive rule forbids %v (%v), want only %s", got, err, pMedRead)
	}
	internal, err := s.forbiddenBy(rows.HeldRule{Reaches: "internal"})
	if err != nil || !slices.Contains(internal, string(pShiftRead)) || !slices.Contains(internal, string(pMedRead)) {
		t.Errorf("internal rule forbids %v (%v), want shifts and med reports", internal, err)
	}
}

func TestForbiddenBy_UnreadableLevel_ReturnsInternal(t *testing.T) {
	s := newTestService(t, &fakeStore{})
	if _, err := s.forbiddenBy(rows.HeldRule{Reaches: "no such level"}); !errs.Is(err, errs.Internal) {
		t.Errorf("forbiddenBy = %v, want Internal", err)
	}
	fs := &fakeStore{
		held:  []rows.Held{{Permission: string(pMedRead)}},
		rules: []rows.HeldRule{{ID: model.NewID[rows.RuleID](), Role: model.NewID[rows.RoleID](), Reaches: "no such level"}},
	}
	s = newTestService(t, fs)
	me := model.NewID[model.PersonID]()
	if err := s.Allow(asPerson(me), pMedRead); !errs.Is(err, errs.Internal) {
		t.Errorf("Allow with a broken rule = %v, want Internal (fail closed)", err)
	}
}

func TestLoad_SiteOnlyPermissionHeldForADepartment_IsDropped(t *testing.T) {
	d1 := model.NewID[model.DepartmentID]()
	fs := &fakeStore{held: []rows.Held{
		{Permission: string(pMedRead), Department: d1}, // declared site-only, held for a department
		{Permission: string(pShiftRead), Department: d1},
	}}
	s := newTestService(t, fs)
	me := model.NewID[model.PersonID]()
	ctx := asPerson(me)
	if err := s.AllowIn(ctx, pMedRead, d1); !errs.Is(err, errs.Permission) {
		t.Errorf("AllowIn(site-only permission) = %v, want Permission", err)
	}
	if ext, err := s.ExtentOf(ctx, pMedRead); err != nil || !ext.IsZero() {
		t.Errorf("ExtentOf = %+v, %v; want zero", ext, err)
	}
	got, err := s.PermissionsOf(ctx, me)
	want := []string{string(pShiftRead) + "@" + d1.String()}
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("PermissionsOf = %v, %v; want %v", got, err, want)
	}
}

func TestCheckSeparation_UndeclaredNamesHaveNoEffect(t *testing.T) {
	rule := rows.HeldRule{ID: model.NewID[rows.RuleID](), Role: model.NewID[rows.RoleID](), Permission: "ghost.thing.do"}
	fs := &fakeStore{held: []rows.Held{{Permission: "ghost.thing.do"}, {Permission: string(pShiftRead)}}, rules: []rows.HeldRule{rule}}
	s := newTestService(t, fs)
	if err := s.checkSeparation(t.Context(), "test", model.NewID[model.PersonID]()); err != nil {
		t.Errorf("an undeclared name caused a conflict: %v", err)
	}
	rule.Permission = string(pShiftRead)
	fs.rules = []rows.HeldRule{rule}
	if err := s.checkSeparation(t.Context(), "test", model.NewID[model.PersonID]()); !errs.Is(err, errs.Conflict) {
		t.Errorf("a declared forbidden name = %v, want Conflict", err)
	}
}

func TestPermissionsOf_FormatsSiteAndDepartmentEntries(t *testing.T) {
	d1, d2 := model.NewID[model.DepartmentID](), model.NewID[model.DepartmentID]()
	fs := &fakeStore{held: []rows.Held{
		{Permission: string(pMedRead)},
		{Permission: string(pShiftRead), Department: d2},
		{Permission: string(pShiftRead), Department: d1},
		{Permission: string(pShiftManage)}, {Permission: string(pShiftManage), Department: d1},
		{Permission: "ghost.thing.do"}, // not declared: no effect
	}}
	s := newTestService(t, fs)
	me := model.NewID[model.PersonID]()
	got, err := s.PermissionsOf(asPerson(me), me)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{string(pMedRead), string(pShiftManage), string(pShiftRead) + "@" + d1.String(), string(pShiftRead) + "@" + d2.String()}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("PermissionsOf = %v, want %v", got, want)
	}
}

func TestPermissionsOf_NobodyHoldsAnything_ReturnsEmptyNotNil(t *testing.T) {
	s := newTestService(t, &fakeStore{})
	me := model.NewID[model.PersonID]()
	got, err := s.PermissionsOf(asPerson(me), me)
	if err != nil || got == nil || len(got) != 0 {
		t.Errorf("PermissionsOf = %#v, %v; want an empty non-nil slice", got, err)
	}
}

func TestSeed_SiteOnlyPermissionIsNotAddedToTheDepartmentRole(t *testing.T) {
	fs := &fakeStore{}
	s := newTestService(t, fs)
	_, err := s.Seed(t.Context(), []PermissionDecl{
		{Name: pMedRead, Scope: ScopeSite, Reaches: model.SensitivitySensitive, StarterRoles: []string{RoleAuditReviewer, RoleDepartmentAdmin}},
		{Name: pShiftRead, Scope: ScopeDepartment, Reaches: model.SensitivityInternal, StarterRoles: []string{RoleDepartmentAdmin}},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantK := []string{RoleAuditReviewer, RoleDepartmentAdmin}
	wantP := []string{string(pMedRead), string(pShiftRead)}
	if !slices.Equal(fs.seedK, wantK) || !slices.Equal(fs.seedP, wantP) {
		t.Errorf("seeded %v / %v, want %v / %v", fs.seedK, fs.seedP, wantK, wantP)
	}
}

func TestSeed_BadDeclaration_ReturnsInvalid(t *testing.T) {
	s := newTestService(t, &fakeStore{})
	_, err := s.Seed(t.Context(), []PermissionDecl{{Name: "bad"}})
	if !errs.Is(err, errs.Invalid) {
		t.Errorf("Seed = %v, want Invalid", err)
	}
}

func TestListGrants_PageSizeAndNextCursor(t *testing.T) {
	base := time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)
	var gs []rows.Grant
	for i := range 4 { // limit 3 plus the extra row
		gs = append(gs, rows.Grant{
			ID: model.NewID[rows.GrantID](), Person: model.NewID[model.PersonID](), Role: model.NewID[rows.RoleID](),
			CreatedAt: base.Add(-time.Duration(i) * time.Minute),
		})
	}
	fs := &fakeStore{grants: gs}
	s := newTestService(t, fs)
	page, err := s.ListGrants(context.WithValue(t.Context(), ctxActor{}, Actor{Person: model.NewID[model.PersonID](), Superuser: true}), GrantQuery{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Grants) != 3 || page.Next.IsZero() {
		t.Fatalf("page = %d grants, next %q; want 3 and a cursor", len(page.Grants), page.Next)
	}
	if fs.filter.Limit != 4 || !fs.filter.WholeSite {
		t.Errorf("filter = %+v, want limit 4 and the whole site", fs.filter)
	}
	at, id, err := page.Next.position()
	if err != nil || id != gs[2].ID || !at.Equal(gs[2].CreatedAt) {
		t.Errorf("cursor points at %v %v (%v), want the third grant", at, id, err)
	}
}

func TestCursor_RoundTrip(t *testing.T) {
	g := Grant{ID: model.NewID[GrantID](), CreatedAt: time.Date(2026, 10, 18, 9, 30, 15, 123456000, time.UTC)}
	c := cursorOf(g)
	back, err := ParseCursor(c.String())
	if err != nil || back != c {
		t.Fatalf("ParseCursor(%q) = %q, %v", c, back, err)
	}
	at, id, err := c.position()
	if err != nil || id != g.ID || !at.Equal(g.CreatedAt) {
		t.Errorf("position = %v %v %v, want %v %v", at, id, err, g.CreatedAt, g.ID)
	}
}
