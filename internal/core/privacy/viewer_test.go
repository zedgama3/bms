package privacy_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/authz/authztest"
	"github.com/zedgama3/bms/internal/core/privacy"
	"github.com/zedgama3/bms/internal/core/privacy/privacytest"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/core/settings/settingstest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// countingAuthz wraps an Authorizer, counts the calls and can answer errors per permission.
type countingAuthz struct {
	inner authz.Authorizer

	mu     sync.Mutex
	extent map[authz.Permission]int
	other  int
	fail   map[authz.Permission]error
}

func newCounting(inner authz.Authorizer) *countingAuthz {
	return &countingAuthz{inner: inner, extent: map[authz.Permission]int{}, fail: map[authz.Permission]error{}}
}

func (c *countingAuthz) calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := c.other
	for _, k := range c.extent {
		n += k
	}
	return n
}

func (c *countingAuthz) Allow(ctx context.Context, p authz.Permission) error {
	c.mu.Lock()
	c.other++
	c.mu.Unlock()
	if err := c.inner.Allow(ctx, p); err != nil {
		return fmt.Errorf("countingAuthz.Allow: %w", err)
	}
	return nil
}

func (c *countingAuthz) AllowIn(ctx context.Context, p authz.Permission, d model.DepartmentID) error {
	c.mu.Lock()
	c.other++
	c.mu.Unlock()
	if err := c.inner.AllowIn(ctx, p, d); err != nil {
		return fmt.Errorf("countingAuthz.AllowIn: %w", err)
	}
	return nil
}

func (c *countingAuthz) ExtentOf(ctx context.Context, p authz.Permission) (authz.Extent, error) {
	c.mu.Lock()
	c.extent[p]++
	err := c.fail[p]
	c.mu.Unlock()
	if err != nil {
		return authz.Extent{}, err
	}
	e, err := c.inner.ExtentOf(ctx, p)
	if err != nil {
		return authz.Extent{}, fmt.Errorf("countingAuthz.ExtentOf: %w", err)
	}
	return e, nil
}

func TestViewerOf_NoActor_ReturnsUnauthenticated(t *testing.T) { // F31
	tests := map[string][]privacy.Group{"with groups": allGroups, "no groups": nil}
	for name, groups := range tests {
		t.Run(name, func(t *testing.T) {
			az := newCounting(authztest.NewFake())
			// Even a ctx that carries an actor does not help: the person argument is zero.
			_, err := privacy.ViewerOf(actorCtx(carol), az, model.PersonID{}, groups)
			wantKind(t, err, errs.Unauthenticated)
			if n := az.calls(); n != 0 {
				t.Errorf("authz was called %d times; a zero person fails before any call", n)
			}
		})
	}
}

func TestViewerOf_NotHeld_IsAbsent(t *testing.T) { // F32
	fake := authztest.NewFake()
	fake.Grant(carol, permContacts) // medical and roster not held
	az := newCounting(fake)
	// A second group sharing permContacts: both must be present or absent together.
	shared := privacy.Group{
		Name: "contact2", Permission: permContacts, Default: privacy.AudienceHolders,
		Options: []privacy.Audience{privacy.AudienceNobody, privacy.AudienceHolders},
	}
	groups := append(append([]privacy.Group{}, allGroups...), shared)
	v, err := privacy.ViewerOf(actorCtx(carol), az, carol, groups)
	if err != nil {
		t.Fatalf("a permission not held is not an error: %v", err)
	}
	if v.Person != carol {
		t.Errorf("Person = %v, want carol", v.Person)
	}
	for _, name := range []string{"contact", "contact2"} {
		if e, ok := v.Extents[name]; !ok || !e.Site {
			t.Errorf("Extents[%q] = %+v, %v; want a site extent", name, e, ok)
		}
	}
	for _, name := range []string{"medical", "roster"} {
		if _, ok := v.Extents[name]; ok {
			t.Errorf("Extents[%q] is present although the permission is not held", name)
		}
	}
	// A holder of nothing: every group absent.
	v, err = privacy.ViewerOf(actorCtx(bob), az, bob, groups)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range groups {
		if e, ok := v.Extents[g.Name]; ok && !e.IsZero() {
			t.Errorf("Extents[%q] = %+v for a person holding nothing", g.Name, e)
		}
	}
}

func TestViewerOf_PermissionErrorMeansNotHeld(t *testing.T) { // F32 (the Permission kind from ExtentOf)
	az := newCounting(authztest.NewFake())
	az.fail[permContacts] = errs.E("test", errs.Permission, errors.New("not held"))
	v, err := privacy.ViewerOf(actorCtx(carol), az, carol, allGroups)
	if err != nil {
		t.Fatalf("a Permission answer is not an error: %v", err)
	}
	if _, ok := v.Extents["contact"]; ok {
		t.Error("contact must be absent when ExtentOf answered Permission")
	}
}

func TestViewerOf_AuthzError_Propagates(t *testing.T) { // F33
	for _, kind := range []errs.Kind{errs.Invalid, errs.Unavailable, errs.Timeout, errs.Canceled, errs.Internal} {
		t.Run(kind.String(), func(t *testing.T) {
			az := newCounting(authztest.NewFake())
			az.fail[permMedical] = errs.E("test", kind, errors.New("boom"))
			_, err := privacy.ViewerOf(actorCtx(carol), az, carol, allGroups)
			wantKind(t, err, kind)
		})
	}
}

func TestViewerOf_NeverPermission(t *testing.T) { // spec: never Permission
	az := newCounting(authztest.NewFake())
	for _, p := range []authz.Permission{permContacts, permMedical, permRoster} {
		az.fail[p] = errs.E("test", errs.Permission, errors.New("no"))
	}
	if _, err := privacy.ViewerOf(actorCtx(carol), az, carol, allGroups); errs.Is(err, errs.Permission) {
		t.Fatalf("ViewerOf returned Permission: %v", err)
	}
}

func TestViewerOf_DistinctPermissionsOnce(t *testing.T) { // F34
	fake := authztest.NewFake()
	fake.GrantIn(carol, permContacts, deptA)
	az := newCounting(fake)
	a := privacy.Group{Name: "alpha", Permission: permContacts, Default: privacy.AudienceHolders,
		Options: []privacy.Audience{privacy.AudienceNobody, privacy.AudienceHolders}}
	b := privacy.Group{Name: "beta", Permission: permContacts, Default: privacy.AudienceHolders,
		Options: []privacy.Audience{privacy.AudienceNobody, privacy.AudienceHolders}}
	v, err := privacy.ViewerOf(actorCtx(carol), az, carol, []privacy.Group{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if n := az.extent[permContacts]; n != 1 {
		t.Errorf("ExtentOf called %d times for one permission, want 1", n)
	}
	for _, name := range []string{"alpha", "beta"} {
		e, ok := v.Extents[name]
		if !ok || !e.Covers(deptA) || e.Covers(deptB) {
			t.Errorf("Extents[%q] = %+v, %v; want department A only", name, e, ok)
		}
	}
}

func TestViewerOf_NoGroups_NoAuthzCall(t *testing.T) { // F39
	az := newCounting(authztest.NewFake())
	for name, groups := range map[string][]privacy.Group{"nil": nil, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			v, err := privacy.ViewerOf(context.Background(), az, bob, groups)
			if err != nil {
				t.Fatal(err)
			}
			if v.Person != bob || len(v.Extents) != 0 {
				t.Errorf("viewer = %+v, want only Person", v)
			}
			if n := az.calls(); n != 0 {
				t.Errorf("authz called %d times", n)
			}
		})
	}
}

func TestViewerOf_CancelledContext_IsNotSwallowed(t *testing.T) {
	az := newCounting(authztest.NewFake())
	ctx, cancel := context.WithCancel(actorCtx(carol))
	cancel()
	az.fail[permContacts] = errs.E("test", errs.Canceled, context.Canceled)
	_, err := privacy.ViewerOf(ctx, az, carol, allGroups)
	wantKind(t, err, errs.Canceled)
}

func TestFilter_ZeroViewer_PublicOnly_AllPolicies(t *testing.T) { // F40
	policies := []privacy.Audience{
		privacy.AudienceNobody, privacy.AudienceSelf, privacy.AudienceHolders, privacy.AudienceSignedIn, "booked",
	}
	for _, a := range policies {
		t.Run(string(a), func(t *testing.T) {
			rec := newRecord()
			rel := privacy.Relation{Subject: alice, Department: deptA, Also: []privacy.Audience{"booked"}}
			res, err := privacy.Filter(&rec, uniform(a), privacy.Viewer{}, rel)
			if err != nil {
				t.Fatalf("Filter: %v", err)
			}
			if rec.Pub != "pub" {
				t.Error("public field must be kept")
			}
			if rec.Int != "" || rec.Phone != nil || rec.Dx != nil || rec.Note != nil || rec.Nick != nil {
				t.Errorf("non-public data reached an anonymous viewer: %+v", rec)
			}
			if res.Audited() || len(res.Fields) != 0 {
				t.Errorf("Result = %+v, want empty", res)
			}
			body, err := json.Marshal(rec)
			if err != nil {
				t.Fatal(err)
			}
			privacytest.AssertHidden(t, body, "int", "phone", "dx", "note", "nick")
			privacytest.AssertPresent(t, body, "pub")
		})
	}
}

// failingReader is a settings.Reader whose Text fails with a kind.
type failingReader struct {
	settings.Reader
	err error
}

func (f failingReader) Text(context.Context, string) (string, error) { return "", f.err }

func TestPolicyOf_ReaderError_Propagates(t *testing.T) { // F35
	for _, kind := range []errs.Kind{errs.Invalid, errs.Unavailable, errs.Timeout, errs.Canceled} {
		t.Run(kind.String(), func(t *testing.T) {
			r := failingReader{err: errs.E("test", kind, errors.New("boom"))}
			pol, err := privacy.PolicyOf(context.Background(), r, allGroups)
			wantKind(t, err, kind)
			if len(pol) != 0 {
				t.Errorf("a failed read returns no policy: %v", pol)
			}
		})
	}
}

func TestPolicyOf_UndeclaredSetting_ReturnsInvalid(t *testing.T) {
	// A reader scoped to an area that never appended privacy.Settings(groups).
	r := settingstest.NewFake("probe", nil)
	_, err := privacy.PolicyOf(context.Background(), r, allGroups)
	wantKind(t, err, errs.Invalid)
}

func TestPolicyOf_DefaultsAndChosenValues(t *testing.T) {
	decls, err := privacy.Settings(allGroups)
	if err != nil {
		t.Fatal(err)
	}
	r := settingstest.NewFake("probe", decls)
	pol, err := privacy.PolicyOf(context.Background(), r, allGroups)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range allGroups {
		if pol[g.Name] != g.Default {
			t.Errorf("policy[%s] = %q, want default %q", g.Name, pol[g.Name], g.Default)
		}
	}
	// A change applies at once: PolicyOf never caches.
	r.Set("visibility_contact", string(privacy.AudienceSignedIn))
	pol, err = privacy.PolicyOf(context.Background(), r, allGroups)
	if err != nil {
		t.Fatal(err)
	}
	if pol["contact"] != privacy.AudienceSignedIn {
		t.Errorf("policy[contact] = %q after Set, want signed_in", pol["contact"])
	}
	if pol["medical"] != gMedical.Default {
		t.Errorf("an unrelated group changed: %q", pol["medical"])
	}
	empty, err := privacy.PolicyOf(context.Background(), r, nil)
	if err != nil || len(empty) != 0 {
		t.Errorf("no groups: %v, %v", empty, err)
	}
}

// F36 is a settings decision 5 behavior (the reader returns the default for a stale value).
func TestPolicyOf_StaleValue_UsesDefault(t *testing.T) { // F36
	t.Skip("settingstest.Fake refuses to hold a value outside Options (Set marks the fake misused), " +
		"so the stale-value path is the settings service's to test; PolicyOf only forwards Reader.Text")
}

func TestPolicyOf_NilReader_ReturnsInternal(t *testing.T) { // F35a
	pol, err := privacy.PolicyOf(context.Background(), nil, allGroups)
	wantKind(t, err, errs.Internal)
	if len(pol) != 0 {
		t.Errorf("a failed call returns no policy: %v", pol)
	}
	// No groups does not excuse a nil reader: it is a wiring bug either way.
	if _, err := privacy.PolicyOf(context.Background(), nil, nil); !errs.Is(err, errs.Internal) {
		t.Errorf("nil reader, no groups: %v, want Internal", err)
	}
}
