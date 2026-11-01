package authztest_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/authz/authztest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

const (
	pRead   authz.Permission = "dept.shifts.read"
	pManage authz.Permission = "dept.shifts.manage"
	pOther  authz.Permission = "med.reports.read"
)

func wantKind(t testing.TB, err error, kind errs.Kind) {
	t.Helper()
	if err == nil {
		t.Fatalf("got nil error, want %v", kind)
	}
	if !errs.Is(err, kind) {
		t.Fatalf("kind = %v (%v), want %v", errs.KindOf(err), err, kind)
	}
}

func actor(ctx context.Context, p model.PersonID) context.Context {
	return authztest.WithActor(ctx, authz.Actor{Person: p})
}

func TestFake_IsAnAuthorizer(_ *testing.T) {
	var _ authz.Authorizer = authztest.NewFake()
}

func TestFake_DeniesEverythingByDefault(t *testing.T) {
	f := authztest.NewFake()
	p, d := model.NewID[model.PersonID](), model.NewID[model.DepartmentID]()
	ctx := actor(t.Context(), p)
	wantKind(t, f.Allow(ctx, pRead), errs.Permission)
	wantKind(t, f.AllowIn(ctx, pRead, d), errs.Permission)
	e, err := f.ExtentOf(ctx, pRead)
	if err != nil || !e.IsZero() {
		t.Fatalf("ExtentOf = %+v, %v; want zero extent and nil", e, err)
	}
}

func TestFake_NoActor_ReturnsUnauthenticated(t *testing.T) {
	f := authztest.NewFake()
	wantKind(t, f.Allow(t.Context(), pRead), errs.Unauthenticated)
	wantKind(t, f.AllowIn(t.Context(), pRead, model.NewID[model.DepartmentID]()), errs.Unauthenticated)
	_, err := f.ExtentOf(t.Context(), pRead)
	wantKind(t, err, errs.Unauthenticated)
}

func TestFake_Grant_AllowsSiteWideOnlyThatPerson(t *testing.T) {
	f := authztest.NewFake()
	alice, bob := model.NewID[model.PersonID](), model.NewID[model.PersonID]()
	d := model.NewID[model.DepartmentID]()
	f.Grant(alice, pRead)
	if err := f.Allow(actor(t.Context(), alice), pRead); err != nil {
		t.Fatal(err)
	}
	wantKind(t, f.Allow(actor(t.Context(), alice), pManage), errs.Permission)
	wantKind(t, f.Allow(actor(t.Context(), bob), pRead), errs.Permission)
	if err := f.AllowIn(actor(t.Context(), alice), pRead, d); err != nil {
		t.Fatalf("site-wide grant must cover any department: %v", err)
	}
	e, err := f.ExtentOf(actor(t.Context(), alice), pRead)
	if err != nil || !e.Site || len(e.Departments) != 0 {
		t.Fatalf("ExtentOf = %+v, %v; want Site", e, err)
	}
}

func TestFake_GrantIn_AllowsOnlyThatDepartment(t *testing.T) {
	f := authztest.NewFake()
	alice := model.NewID[model.PersonID]()
	d1, d2 := model.NewID[model.DepartmentID](), model.NewID[model.DepartmentID]()
	f.GrantIn(alice, pManage, d1)
	ctx := actor(t.Context(), alice)
	wantKind(t, f.Allow(ctx, pManage), errs.Permission)
	if err := f.AllowIn(ctx, pManage, d1); err != nil {
		t.Fatal(err)
	}
	wantKind(t, f.AllowIn(ctx, pManage, d2), errs.Permission)
	e, err := f.ExtentOf(ctx, pManage)
	if err != nil || e.Site || !e.Covers(d1) || e.Covers(d2) {
		t.Fatalf("ExtentOf = %+v, %v", e, err)
	}
}

func TestFake_GrantIn_TwoDepartments_ExtentSortedUnique(t *testing.T) {
	f := authztest.NewFake()
	alice := model.NewID[model.PersonID]()
	d1, d2 := model.NewID[model.DepartmentID](), model.NewID[model.DepartmentID]()
	f.GrantIn(alice, pManage, d1)
	f.GrantIn(alice, pManage, d2)
	f.GrantIn(alice, pManage, d1)
	e, err := f.ExtentOf(actor(t.Context(), alice), pManage)
	if err != nil || len(e.Departments) != 2 {
		t.Fatalf("ExtentOf = %+v, %v; want two departments", e, err)
	}
	if string(e.Departments[0][:]) >= string(e.Departments[1][:]) {
		t.Fatalf("departments not sorted: %v", e.Departments)
	}
}

func TestFake_Superuser_AllowsEverything(t *testing.T) {
	f := authztest.NewFake()
	root := model.NewID[model.PersonID]()
	f.Superuser(root)
	ctx := actor(t.Context(), root)
	if err := f.Allow(ctx, pOther); err != nil {
		t.Fatal(err)
	}
	if err := f.AllowIn(ctx, pOther, model.NewID[model.DepartmentID]()); err != nil {
		t.Fatal(err)
	}
	e, err := f.ExtentOf(ctx, pOther)
	if err != nil || !e.Site {
		t.Fatalf("ExtentOf = %+v, %v; want Site", e, err)
	}
}

func TestFake_AllowIn_ZeroDepartment_ReturnsInvalid(t *testing.T) {
	f := authztest.NewFake()
	p := model.NewID[model.PersonID]()
	f.Grant(p, pRead)
	wantKind(t, f.AllowIn(actor(t.Context(), p), pRead, model.DepartmentID{}), errs.Invalid)
}

func TestFake_ConcurrentUse_RaceFree(t *testing.T) {
	f := authztest.NewFake()
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p := model.NewID[model.PersonID]()
			f.Grant(p, pRead)
			f.GrantIn(p, pManage, model.NewID[model.DepartmentID]())
			if err := f.Allow(actor(t.Context(), p), pRead); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}

func TestFake_TitlesOf_SortedAndCappedLikeTheLookup(t *testing.T) {
	f := authztest.NewFake()
	p := model.NewID[model.PersonID]()
	f.SetTitles(p, "Medic", "Gate lead", "", strings.Repeat("x", audit.MaxTitleBytes+1), "Art liaison")
	got, err := f.TitlesOf(t.Context(), p)
	if err != nil {
		t.Fatalf("TitlesOf: %v", err)
	}
	want := []string{"Art liaison", "Gate lead", "Medic"}
	if !slices.Equal(got, want) {
		t.Fatalf("titles = %v, want %v", got, want)
	}

	many := make([]string, audit.MaxTitles+4)
	for i := range many {
		many[i] = fmt.Sprintf("Title %02d", len(many)-i)
	}
	f.SetTitles(p, many...)
	got, err = f.TitlesOf(t.Context(), p)
	if err != nil {
		t.Fatalf("TitlesOf: %v", err)
	}
	if len(got) != audit.MaxTitles || !slices.IsSorted(got) || got[0] != "Title 01" {
		t.Fatalf("got %d titles %v, want %d sorted from Title 01", len(got), got, audit.MaxTitles)
	}

	other, err := f.TitlesOf(t.Context(), model.NewID[model.PersonID]())
	if err != nil {
		t.Fatalf("TitlesOf: %v", err)
	}
	if other != nil {
		t.Errorf("person without titles got %v, want nil", other)
	}
}
