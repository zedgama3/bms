package jobs_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// ---- Defer -------------------------------------------------------------------------------

func TestDefer_InvalidDelivery_ReturnsInvalid(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	bad := map[string]func(d *signals.Delivery){
		"zero attempt":     func(d *signals.Delivery) { d.Attempt = 0 },
		"negative attempt": func(d *signals.Delivery) { d.Attempt = -1 },
		"bad subscriber":   func(d *signals.Delivery) { d.Subscriber = "Not A Module" },
		"empty subscriber": func(d *signals.Delivery) { d.Subscriber = "" },
		"zero signal id":   func(d *signals.Delivery) { d.Signal.ID = signals.SignalID{} },
		"bad signal name":  func(d *signals.Delivery) { d.Signal.Name = "nodot" },
	}
	for name, mod := range bad {
		t.Run(name, func(t *testing.T) {
			d := delivery(1)
			mod(&d)
			wantKind(t, r.run.Defer(ctxT(t), d), errs.Invalid)
		})
	}
}

func TestDefer_ReadOnly_ReturnsErrReadOnly(t *testing.T) {
	r, _ := readOnlyRig(t)
	wantReadOnly(t, r.run.Defer(ctxT(t), delivery(1)))
}

func TestDefer_AfterClose_ReturnsUnavailable(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	wantNoErr(t, r.run.Close(ctxT(t)))
	wantKind(t, r.run.Defer(ctxT(t), delivery(1)), errs.Unavailable)
}

// ---- Run / Close / RunOnce ---------------------------------------------------------------

func TestRun_Twice_ReturnsInternal(t *testing.T) {
	r, _ := readOnlyRig(t)
	ctx, cancel := context.WithCancel(ctxT(t))
	done := r.startRun(ctx)
	wantKind(t, r.run.Run(ctxT(t)), errs.Internal)
	cancel()
	wantNoErr(t, <-done)
}

func TestRun_CtxEnds_ReturnsNil(t *testing.T) {
	r, _ := readOnlyRig(t)
	ctx, cancel := context.WithCancel(ctxT(t))
	done := r.startRun(ctx)
	cancel()
	wantNoErr(t, <-done)
}

func TestRun_AfterClose_ReturnsNil(t *testing.T) {
	r, _ := readOnlyRig(t)
	done := r.startRun(ctxT(t))
	wantNoErr(t, r.run.Close(ctxT(t)))
	wantNoErr(t, <-done)
}

func TestClose_Idempotent(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	wantNoErr(t, r.run.Close(ctxT(t)))
	wantNoErr(t, r.run.Close(ctxT(t)))
}

func TestRunOnce_ReadOnly_ReturnsErrReadOnly(t *testing.T) {
	r, d := readOnlyRig(t)
	ran, err := r.run.RunOnce(ctxT(t))
	wantReadOnly(t, err)
	if ran != (jobs.Ran{}) {
		t.Errorf("Ran = %+v, want zero", ran)
	}
	if n := len(d.Calls()); n != 0 {
		t.Errorf("a read-only RunOnce ran %d statements, want none", n)
	}
}

func TestRun_ReadOnlyLoop_RunsNothing(t *testing.T) {
	r, d := readOnlyRig(t)
	var c counter
	r.register(jobs.Decl{Name: "volunteers.sweep", Every: time.Minute, Handler: c.handler(nil)})
	ctx, cancel := context.WithCancel(ctxT(t))
	done := r.startRun(ctx)
	for range 5 {
		r.advance(jobs.DefaultPollInterval)
	}
	cancel()
	wantNoErr(t, <-done)
	if c.n() != 0 {
		t.Errorf("handler ran %d times on a read-only copy", c.n())
	}
	if n := len(d.Calls()); n != 0 {
		t.Errorf("the loop issued %d statements on a read-only copy, want none", n)
	}
}

// ---- admin methods: authentication and permissions (F20 to F22, F8) ----------------------

func adminRig(t *testing.T) *rig {
	t.Helper()
	r := newRig(t, dbtest.NewFake())
	r.register(jobs.Decl{Name: "volunteers.sweep", Every: time.Hour, Handler: nop})
	r.register(jobs.Decl{Name: "volunteers.remind", Handler: nop})
	return r
}

func TestStatus_NoActor_ReturnsUnauthenticated(t *testing.T) {
	r := adminRig(t)
	_, err := r.run.Status(ctxT(t))
	wantKind(t, err, errs.Unauthenticated)
}

func TestAdmin_NoActor_ReturnsUnauthenticated(t *testing.T) {
	r := adminRig(t)
	id := model.NewID[jobs.JobID]()
	_, err := r.run.RunNow(ctxT(t), "volunteers.sweep")
	wantKind(t, err, errs.Unauthenticated)
	wantKind(t, r.run.Retry(ctxT(t), id), errs.Unauthenticated)
	wantKind(t, r.run.Cancel(ctxT(t), id), errs.Unauthenticated)
}

func TestStatus_NoPermission_ReturnsPermission(t *testing.T) {
	r := adminRig(t)
	_, err := r.run.Status(r.holder())
	wantKind(t, err, errs.Permission)
	// jobs.manage does not imply jobs.read.
	_, err = r.run.Status(r.holder(permManage))
	wantKind(t, err, errs.Permission)
}

func TestRunNow_NoPermission_ReturnsPermission(t *testing.T) {
	r := adminRig(t)
	for name, ctx := range map[string]context.Context{
		"nothing":   r.holder(),
		"read only": r.holder(permRead), // jobs.read holders cannot RunNow
	} {
		t.Run(name, func(t *testing.T) {
			_, err := r.run.RunNow(ctx, "volunteers.sweep")
			wantKind(t, err, errs.Permission)
		})
	}
	if got := r.aud.Recorded(); len(got) != 0 {
		t.Errorf("a refused RunNow recorded %d audit entries", len(got))
	}
}

func TestRetry_NoPermission_ReturnsPermission(t *testing.T) {
	r := adminRig(t)
	id := model.NewID[jobs.JobID]()
	wantKind(t, r.run.Retry(r.holder(), id), errs.Permission)
	wantKind(t, r.run.Retry(r.holder(permRead), id), errs.Permission)
	if got := r.aud.Recorded(); len(got) != 0 {
		t.Errorf("a refused Retry recorded %d audit entries", len(got))
	}
}

func TestCancel_NoPermission_ReturnsPermission(t *testing.T) {
	r := adminRig(t)
	id := model.NewID[jobs.JobID]()
	wantKind(t, r.run.Cancel(r.holder(), id), errs.Permission)
	wantKind(t, r.run.Cancel(r.holder(permRead), id), errs.Permission)
	if got := r.aud.Recorded(); len(got) != 0 {
		t.Errorf("a refused Cancel recorded %d audit entries", len(got))
	}
}

func TestRunNow_UnknownName_ReturnsNotFound(t *testing.T) {
	r := adminRig(t)
	_, err := r.run.RunNow(r.holder(permManage), "volunteers.never")
	wantKind(t, err, errs.NotFound)
	_, err = r.run.RunNow(r.holder(permManage), "people.sweep") // another owner's, not registered here
	wantKind(t, err, errs.NotFound)
}

func TestRunNow_OnDemand_ReturnsInvalid(t *testing.T) {
	r := adminRig(t)
	_, err := r.run.RunNow(r.holder(permManage), "volunteers.remind")
	wantField(t, err, "name", "on_demand")
	_, err = r.run.RunNow(r.holder(permManage), jobs.NameRedeliver)
	wantField(t, err, "name", "on_demand")
}

func TestRunNow_ReadOnly_ReturnsErrReadOnly(t *testing.T) {
	r, _ := readOnlyRig(t)
	r.register(jobs.Decl{Name: "volunteers.sweep", Every: time.Hour, Handler: nop})
	_, err := r.run.RunNow(r.holder(permManage), "volunteers.sweep")
	wantReadOnly(t, err)
	if got := r.aud.Recorded(); len(got) != 0 {
		t.Errorf("a refused RunNow recorded %d audit entries", len(got))
	}
}

func TestRetryCancel_ReadOnly_ReturnsErrReadOnly(t *testing.T) {
	r, _ := readOnlyRig(t)
	id := model.NewID[jobs.JobID]()
	wantReadOnly(t, r.run.Retry(r.holder(permManage), id))
	wantReadOnly(t, r.run.Cancel(r.holder(permManage), id))
}

// ---- routes ------------------------------------------------------------------------------

func TestRoutes_AreTheFourSpecified(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	want := map[string]bool{
		"GET /":                       false,
		"POST /registered/{name}/run": false,
		"POST /queue/{job}/retry":     false,
		"POST /queue/{job}/cancel":    false,
	}
	for _, rt := range r.run.Routes() {
		k := rt.Method + " " + strings.Replace(rt.Pattern, "/{$}", "/", 1) // "/" and "/{$}" both name the root
		if _, ok := want[k]; !ok {
			t.Errorf("unexpected route %q", k)
		}
		want[k] = true
		if rt.Handler == nil {
			t.Errorf("route %q has no handler", k)
		}
	}
	for k, seen := range want {
		if !seen {
			t.Errorf("route %q is missing", k)
		}
	}
}

func TestOpenAPI_IsJSONWithPaths(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	b := r.run.OpenAPI()
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("OpenAPI is not a JSON object: %v", err)
	}
	if _, ok := doc["paths"]; !ok {
		t.Errorf("OpenAPI has no paths: %s", b)
	}
}

func serve(ctx context.Context, t *testing.T, r *rig, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	for _, rt := range r.run.Routes() {
		mux.Handle(rt.Method+" "+rt.Pattern, rt.Handler)
	}
	req := httptest.NewRequestWithContext(ctx, method, path, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

func TestRoutes_NoActor_Returns401(t *testing.T) {
	r := adminRig(t)
	id := model.NewID[jobs.JobID]().String()
	for _, c := range []struct{ method, path string }{
		{"GET", "/"},
		{"POST", "/registered/volunteers.sweep/run"},
		{"POST", "/queue/" + id + "/retry"},
		{"POST", "/queue/" + id + "/cancel"},
	} {
		if w := serve(ctxT(t), t, r, c.method, c.path); w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a session = %d, want 401", c.method, c.path, w.Code)
		}
	}
}

func TestRoutes_NoPermission_Returns403(t *testing.T) {
	r := adminRig(t)
	id := model.NewID[jobs.JobID]().String()
	if w := serve(r.holder(), t, r, "GET", "/"); w.Code != http.StatusForbidden {
		t.Errorf("GET / without jobs.read = %d, want 403", w.Code)
	}
	readOnly := r.holder(permRead)
	for _, c := range []struct{ method, path string }{
		{"POST", "/registered/volunteers.sweep/run"},
		{"POST", "/queue/" + id + "/retry"},
		{"POST", "/queue/" + id + "/cancel"},
	} {
		if w := serve(readOnly, t, r, c.method, c.path); w.Code != http.StatusForbidden {
			t.Errorf("%s %s with jobs.read only = %d, want 403", c.method, c.path, w.Code)
		}
	}
}

func TestRoutes_MalformedPathValues_Return400BeforeAnyLookup(t *testing.T) {
	r := adminRig(t)
	mgr := r.holder(permManage, permRead)
	for _, p := range []string{
		"/registered/Bad%20Name/run",
		"/registered/nodot/run",
		"/queue/not-a-uuid/retry",
		"/queue/not-a-uuid/cancel",
	} {
		if w := serve(mgr, t, r, "POST", p); w.Code != http.StatusBadRequest {
			t.Errorf("POST %s = %d, want 400 (body %q)", p, w.Code, strings.TrimSpace(w.Body.String()))
		}
	}
}
