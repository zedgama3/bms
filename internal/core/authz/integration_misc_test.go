//go:build integration

package authz_test

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/authz/authztest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// ---- concurrency -----------------------------------------------------------------------

func TestGrant_ConcurrentDuplicates_ExactlyOneSucceeds(t *testing.T) {
	r := newRig(t)
	ro := r.mkRole(authz.ScopeSite, pMedRead)
	p := r.person()
	const n = 8
	var ok, dup atomic.Int32
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := r.svc.Grant(r.sctx(), authz.GrantInput{Person: p, Role: ro.ID})
			switch {
			case err == nil:
				ok.Add(1)
			case errs.Is(err, errs.Conflict):
				dup.Add(1)
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 1 || dup.Load() != n-1 {
		t.Fatalf("%d succeeded, %d conflicted; want 1 and %d", ok.Load(), dup.Load(), n-1)
	}
	if len(r.grantsOf(p)) != 1 {
		t.Error("more than one row")
	}
}

func TestRevoke_TwoLastAdminsConcurrently_ExactlyOneSucceeds(t *testing.T) {
	for i := range 5 {
		t.Run("round", func(t *testing.T) {
			r := newRig(t)
			a, b := r.admin(), r.admin()
			ga, gb := r.grantsOf(a)[0], r.grantsOf(b)[0]
			var wg sync.WaitGroup
			errsOut := make([]error, 2)
			for k, g := range []authz.Grant{ga, gb} {
				wg.Add(1)
				go func() {
					defer wg.Done()
					errsOut[k] = r.svc.Revoke(r.sctx(), g.ID)
				}()
			}
			wg.Wait()
			okN := 0
			for _, e := range errsOut {
				if e == nil {
					okN++
				} else {
					wantCode(t, e, errs.Conflict, "last_access_admin")
				}
			}
			if okN != 1 {
				t.Fatalf("round %d: %d revocations succeeded, want exactly 1", i, okN)
			}
		})
	}
}

func TestDecisions_Concurrent_RaceFree(t *testing.T) {
	r := newRig(t)
	p := r.holder(pMedRead)
	d := r.deptHolder(r.d1, pShiftManage)
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 10 {
				if err := r.svc.Allow(r.pctx(p), pMedRead); err != nil {
					t.Error(err)
				}
				if err := r.svc.AllowIn(r.pctx(d), pShiftManage, r.d1); err != nil {
					t.Error(err)
				}
				if _, err := r.svc.ExtentOf(r.pctx(d), pShiftManage); err != nil {
					t.Error(err)
				}
				if _, err := r.svc.TitlesOf(ctxT(t), p); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
}

// ---- store failures for the decision methods ---------------------------------------------

func TestDecisions_StoreFails_ReturnsStoreKind(t *testing.T) {
	r := newRig(t)
	p := r.holder(pMedRead)
	for name, injected := range map[string]error{"down": errDown, "timeout": errTimeout} {
		t.Run(name, func(t *testing.T) {
			r.fdb.set(func(f *faultDB) { f.readErr = injected })
			t.Cleanup(func() { r.fdb.set(func(f *faultDB) { f.readErr = nil }) })
			want := errs.KindOf(injected)
			wantKind(t, r.svc.Allow(r.pctx(p), pMedRead), want)
			wantKind(t, r.svc.AllowIn(r.pctx(p), pShiftManage, r.d1), want)
			_, err := r.svc.ExtentOf(r.pctx(p), pShiftManage)
			wantKind(t, err, want)
			_, err = r.svc.PermissionsOf(r.pctx(p), p)
			wantKind(t, err, want)
			_, err = r.svc.TitlesOf(ctxT(t), p)
			wantKind(t, err, want)
			// a store failure is never a silent denial or allowance
			if errs.Is(r.svc.Allow(r.pctx(p), pMedRead), errs.Permission) {
				t.Error("store failure reported as Permission")
			}
		})
	}
}

func TestDecisions_CancelledContext_ReturnsCanceled(t *testing.T) {
	r := newRig(t)
	p := r.holder(pMedRead)
	ctx, cancel := context.WithCancel(r.pctx(p))
	cancel()
	wantKind(t, r.svc.Allow(ctx, pMedRead), errs.Canceled)
}

func TestDecisions_ReadOnlyCopy_StillDecides(t *testing.T) {
	r := newRig(t)
	p := r.holder(pMedRead)
	r.fdb.set(func(f *faultDB) { f.ro = true })
	wantNoErr(t, r.svc.Allow(r.pctx(p), pMedRead))
	_, err := r.svc.ListGrants(r.sctx(), authz.GrantQuery{})
	wantNoErr(t, err)
	_, err = r.svc.TitlesOf(ctxT(t), p)
	wantNoErr(t, err)
	_, err = r.svc.PermissionsOf(r.pctx(p), p)
	wantNoErr(t, err)
}

func TestDecisions_CountedByOutcome(t *testing.T) {
	r := newRig(t)
	p := r.holder(pMedRead)
	wantNoErr(t, r.svc.Allow(r.pctx(p), pMedRead))
	wantKind(t, r.svc.Allow(r.pctx(p), pMedWrite), errs.Permission)
	seen := map[string]bool{}
	for _, m := range r.cap.Measurements() {
		if m.Instrument == "bms.authz.decisions" {
			seen[m.Attrs["outcome"]] = true
		}
	}
	if !seen["allowed"] || !seen["denied"] {
		t.Errorf("outcomes seen = %v, want allowed and denied", seen)
	}
}

// ---- the fake behaves like the service ---------------------------------------------------

func TestFake_MatchesService(t *testing.T) {
	r := newRig(t)
	fake := authztest.NewFake()
	type scenario struct {
		name  string
		who   model.PersonID
		super bool
	}
	siteHolder := r.holder(pShiftManage)
	fake.Grant(siteHolder, pShiftManage)
	deptHolder := r.deptHolder(r.d1, pShiftManage)
	fake.GrantIn(deptHolder, pShiftManage, r.d1)
	nobody := r.person()
	super := r.su
	fake.Superuser(super)
	scenarios := []scenario{{"site", siteHolder, false}, {"department", deptHolder, false}, {"nobody", nobody, false}, {"superuser", super, true}}
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			ctx := as(ctxT(t), sc.who)
			if sc.super {
				ctx = asSuper(ctxT(t), sc.who)
			}
			fctx := authztest.WithActor(ctxT(t), authz.Actor{Person: sc.who, Superuser: sc.super})
			same := func(what string, a, b error) {
				if errs.KindOf(a) != errs.KindOf(b) {
					t.Errorf("%s: service %v, fake %v", what, errs.KindOf(a), errs.KindOf(b))
				}
			}
			same("Allow", r.svc.Allow(ctx, pShiftManage), fake.Allow(fctx, pShiftManage))
			for _, d := range []model.DepartmentID{r.d1, r.d2} {
				same("AllowIn", r.svc.AllowIn(ctx, pShiftManage, d), fake.AllowIn(fctx, pShiftManage, d))
			}
			same("AllowIn zero", r.svc.AllowIn(ctx, pShiftManage, model.DepartmentID{}), fake.AllowIn(fctx, pShiftManage, model.DepartmentID{}))
			se, err1 := r.svc.ExtentOf(ctx, pShiftManage)
			fe, err2 := fake.ExtentOf(fctx, pShiftManage)
			same("ExtentOf", err1, err2)
			if se.Site != fe.Site || se.IsZero() != fe.IsZero() || se.Covers(r.d1) != fe.Covers(r.d1) || se.Covers(r.d2) != fe.Covers(r.d2) {
				t.Errorf("ExtentOf: service %+v, fake %+v", se, fe)
			}
		})
	}
	t.Run("zero person, not superuser", func(t *testing.T) {
		zero := authz.Actor{}
		sctx := withActor(ctxT(t), zero)
		fctx := authztest.WithActor(ctxT(t), zero)
		wantKind(t, r.svc.Allow(sctx, pShiftManage), errs.Unauthenticated)
		wantKind(t, fake.Allow(fctx, pShiftManage), errs.Unauthenticated)
		wantKind(t, r.svc.AllowIn(sctx, pShiftManage, r.d1), errs.Unauthenticated)
		wantKind(t, fake.AllowIn(fctx, pShiftManage, r.d1), errs.Unauthenticated)
		_, err := r.svc.ExtentOf(sctx, pShiftManage)
		wantKind(t, err, errs.Unauthenticated)
		_, err = fake.ExtentOf(fctx, pShiftManage)
		wantKind(t, err, errs.Unauthenticated)
	})
	t.Run("no actor", func(t *testing.T) {
		same := errs.KindOf(r.svc.Allow(ctxT(t), pShiftManage)) == errs.KindOf(fake.Allow(ctxT(t), pShiftManage))
		if !same {
			t.Error("kinds differ without an actor")
		}
	})
}

// ---- handlers --------------------------------------------------------------------------

// F63
func TestHandlers_Permission_Returns403(t *testing.T) {
	r := newRig(t)
	mux := serve(t, r.svc)
	p := r.person()
	id := model.NewID[authz.RoleID]().String()
	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/api/access/permissions", ""},
		{"GET", "/api/access/roles", ""},
		{"GET", "/api/access/roles/" + id, ""},
		{"POST", "/api/access/roles", `{"name":"x","scope":"site"}`},
		{"DELETE", "/api/access/roles/" + id, ""},
		{"GET", "/api/access/grants", ""},
		{"GET", "/api/access/rules", ""},
		{"POST", "/api/access/rules", `{}`},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			w := doReq(mux, r.pctx(p), tc.method, tc.path, tc.body)
			if w.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403; body %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestHandlers_NotFound_Returns404(t *testing.T) {
	r := newRig(t)
	mux := serve(t, r.svc)
	id := model.NewID[authz.RoleID]().String()
	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/access/roles/" + id},
		{"DELETE", "/api/access/roles/" + id},
		{"DELETE", "/api/access/grants/" + id},
		{"DELETE", "/api/access/rules/" + id},
	} {
		w := doReq(mux, r.sctx(), tc.method, tc.path, "")
		if w.Code != http.StatusNotFound {
			t.Errorf("%s %s = %d, want 404", tc.method, tc.path, w.Code)
		}
	}
}

func TestHandlers_Reads_Return200ForAuthorised(t *testing.T) {
	r := newRig(t)
	mux := serve(t, r.svc)
	for _, path := range []string{"/api/access/permissions", "/api/access/roles", "/api/access/grants", "/api/access/rules"} {
		w := doReq(mux, r.sctx(), "GET", path, "")
		if w.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200; body %s", path, w.Code, w.Body.String())
		}
	}
}

func TestHandlers_BadQuery_Returns400(t *testing.T) {
	r := newRig(t)
	mux := serve(t, r.svc)
	for _, q := range []string{"?limit=0x", "?limit=9999", "?after=%25%25", "?person=not-a-uuid"} {
		w := doReq(mux, r.sctx(), "GET", "/api/access/grants"+q, "")
		if w.Code != http.StatusBadRequest {
			t.Errorf("GET grants%s = %d, want 400", q, w.Code)
		}
	}
}

func TestHandlers_ReadOnly_Returns503(t *testing.T) {
	r := newRig(t)
	mux := serve(t, r.svc)
	r.fdb.set(func(f *faultDB) { f.ro = true })
	w := doReq(mux, r.sctx(), "POST", "/api/access/roles", `{"name":"ro","scope":"site"}`)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body %s", w.Code, w.Body.String())
	}
}
