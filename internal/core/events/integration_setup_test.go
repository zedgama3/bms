//go:build integration

package events_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/contracts/eventsetup"
	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/events"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

func moduleOf(rs []events.SetupResult) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Module)
	}
	return out
}

func setupEntries(r *irig) []audit.Entry { return entriesOf(r.rec.Recorded(), "events.setup.ran") }

// setupMeasures returns the outcome attribute of every bms.events.setup measurement for module.
func setupMeasures(r *irig, module string) []string {
	var out []string
	for _, m := range r.cap.Measurements() {
		if strings.Contains(m.Instrument, "events.setup") && m.Attrs["module"] == module {
			out = append(out, m.Attrs["outcome"])
		}
	}
	return out
}

// F56
func TestCreate_NoCopy_RunsNoProvider(t *testing.T) {
	a, b := provider("alpha", nil, 3), provider("beta", nil, 4)
	r := newIRig(t, []events.SetupProvider{a.sp(), b.sp()})
	c, err := r.svc.Create(r.sctx(), validCreate("Burn 2027"))
	wantNoErr(t, err)
	if len(c.Setup) != 0 {
		t.Errorf("Setup = %v, want empty", c.Setup)
	}
	if a.calls() != 0 || b.calls() != 0 {
		t.Errorf("providers ran without CopyFrom: %d, %d", a.calls(), b.calls())
	}
	if len(setupEntries(r)) != 0 {
		t.Error("events.setup.ran recorded without a copy")
	}
}

func TestCreate_CopyFromWithNoProviders_ReturnsEmptySetup(t *testing.T) {
	r := newIRig(t, nil)
	src := r.mk("Burn 2026")
	in := validCreate("Burn 2027")
	in.CopyFrom = src.ID
	c, err := r.svc.Create(r.sctx(), in)
	wantNoErr(t, err)
	if len(c.Setup) != 0 || c.Setup == nil {
		t.Errorf("Setup = %#v, want empty and not nil", c.Setup)
	}
	// The signal carries copied-from.
	var found bool
	for _, s := range sigsOf(r.bus.Published(), events.SignalCreated) {
		if id, err := payloadEvent(t, s, events.PayloadEvent); err == nil && id == c.Event.ID {
			cf, err := payloadEvent(t, s, events.PayloadCopiedFrom)
			if err != nil || cf != src.ID {
				t.Errorf("copied-from = %v, %v; want %v", cf, err, src.ID)
			}
			found = true
		}
	}
	if !found {
		t.Error("no created signal for the new event")
	}
}

// Spec test plan: TestCreate_Setup_RunsProvidersInLinkOrderWithTimeout.
func TestCreate_Setup_RunsProvidersInLinkOrderWithTimeout(t *testing.T) {
	log := &callLog{}
	var r *irig
	var seenEvent, seenSignal, seenPerson bool
	var deadlines []time.Duration
	mkProv := func(name string, items int) *fakeProvider {
		return &fakeProvider{module: name, log: log, fn: func(ctx context.Context, req eventsetup.Request) (eventsetup.Result, error) {
			// The event is committed before any provider runs, and its signal is out.
			if _, err := r.svc.Get(ctx, req.Event); err == nil {
				seenEvent = true
			}
			for _, s := range sigsOf(r.bus.Published(), events.SignalCreated) {
				if id, err := payloadEvent(t, s, events.PayloadEvent); err == nil && id == req.Event {
					seenSignal = true
				}
			}
			if p, ok := identity.PersonFrom(ctx); ok && p == r.steward {
				seenPerson = true
			}
			dl, ok := ctx.Deadline()
			if !ok {
				t.Errorf("%s: the provider's ctx has no deadline", name)
			} else {
				deadlines = append(deadlines, time.Until(dl))
			}
			return eventsetup.Result{Items: items}, nil
		}}
	}
	a, b, c := mkProv("alpha", 12), mkProv("beta", 0), mkProv("gamma", 3)
	r = newIRig(t, []events.SetupProvider{a.sp(), b.sp(), c.sp()}, func(o *events.Options) { o.SetupTimeout = 30 * time.Second })
	src := r.mk("Burn 2026")

	in := validCreate("Burn 2027")
	in.CopyFrom = src.ID
	created, err := r.svc.Create(r.sctx(), in)
	wantNoErr(t, err)

	if got := log.list(); !slices.Equal(got, []string{"alpha", "beta", "gamma"}) {
		t.Errorf("provider order = %v, want link order", got)
	}
	if got := moduleOf(created.Setup); !slices.Equal(got, []string{"alpha", "beta", "gamma"}) {
		t.Errorf("Setup modules = %v", got)
	}
	wantItems := []int{12, 0, 3}
	for i, s := range created.Setup {
		if s.Items != wantItems[i] || s.Failed || s.Code != "" {
			t.Errorf("Setup[%d] = %+v", i, s)
		}
	}
	for _, p := range []*fakeProvider{a, b, c} {
		if p.calls() != 1 {
			t.Errorf("%s called %d times, want once", p.module, p.calls())
			continue
		}
		req := p.reqs[0]
		if req.Event != created.Event.ID || req.CopyFrom != src.ID {
			t.Errorf("%s got %+v, want event %v from %v", p.module, req, created.Event.ID, src.ID)
		}
	}
	if !seenEvent {
		t.Error("the event was not readable when a provider ran: it must be committed first")
	}
	if !seenSignal {
		t.Error("events.event-created was not out when a provider ran")
	}
	if !seenPerson {
		t.Error("the provider did not run in the caller's ctx (identity.PersonFrom)")
	}
	for _, d := range deadlines {
		if d <= 0 || d > 30*time.Second {
			t.Errorf("provider deadline in %v, want within (0, SetupTimeout]", d)
		}
	}

	// One audit entry for the whole run, Succeeded, with the source in the reason.
	es := setupEntries(r)
	if len(es) != 1 {
		t.Fatalf("%d events.setup.ran entries, want 1", len(es))
	}
	if es[0].Outcome != audit.Succeeded || es[0].Reason != "copy_from:"+src.ID.String() || len(es[0].Fields) != 0 {
		t.Errorf("setup.ran = outcome %v reason %q fields %v", es[0].Outcome, es[0].Reason, es[0].Fields)
	}
	if !isRef(es[0], "events.event", [16]byte(created.Event.ID)) {
		t.Errorf("setup.ran record = %+v, want the new event", es[0].Record)
	}
	for _, m := range []string{"alpha", "beta", "gamma"} {
		if got := setupMeasures(r, m); !slices.Equal(got, []string{"ok"}) {
			t.Errorf("metric outcomes for %s = %v, want [ok]", m, got)
		}
	}
	// The copy source may be closed.
	closed := r.mkClosed("Burn 2025")
	in2 := validCreate("Burn 2028")
	in2.CopyFrom = closed.ID
	if _, err := r.svc.Create(r.sctx(), in2); err != nil {
		t.Errorf("copying from a closed event: %v", err)
	}
}

// F52, and the reported Code for each kind.
func TestCreate_ProviderFails_ReportsAndKeepsEvent(t *testing.T) {
	failing := func(name string, err error) *fakeProvider {
		return &fakeProvider{module: name, fn: func(context.Context, eventsetup.Request) (eventsetup.Result, error) {
			return eventsetup.Result{}, err
		}}
	}
	tests := []struct {
		name string
		err  error
		code string
	}{
		{"unavailable", errs.E("test", errs.Unavailable, errors.New("down")), "unavailable"},
		{"not found", errs.E("test", errs.NotFound, errors.New("gone")), "not_found"},
		{"invalid", errs.E("test", errs.Invalid, errors.New("bad")), "invalid"},
		{"unclassified", errors.New("plain"), "internal"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bad := failing("alpha", tc.err)
			good := provider("beta", nil, 5)
			r := newIRig(t, []events.SetupProvider{bad.sp(), good.sp()})
			src := r.mk("Burn 2026")
			in := validCreate("Burn 2027")
			in.CopyFrom = src.ID
			c, err := r.svc.Create(r.sctx(), in)
			wantNoErr(t, err)
			if len(c.Setup) != 2 {
				t.Fatalf("Setup = %+v", c.Setup)
			}
			if !c.Setup[0].Failed || c.Setup[0].Module != "alpha" || c.Setup[0].Code != tc.code {
				t.Errorf("Setup[0] = %+v, want failed with code %q", c.Setup[0], tc.code)
			}
			if c.Setup[1].Failed || c.Setup[1].Items != 5 {
				t.Errorf("Setup[1] = %+v: a later provider must still run", c.Setup[1])
			}
			if good.calls() != 1 {
				t.Errorf("the provider after the failure ran %d times", good.calls())
			}
			// The event stands.
			if _, err := r.svc.Get(r.sctx(), c.Event.ID); err != nil {
				t.Errorf("the event is gone after a provider failure: %v", err)
			}
			// The run is audited as Failed, naming the module that failed.
			es := setupEntries(r)
			if len(es) != 1 || es[0].Outcome != audit.Failed || !slices.Equal(es[0].Fields, []string{"alpha"}) {
				t.Errorf("setup.ran = %+v", es)
			}
			if got := setupMeasures(r, "alpha"); len(got) != 1 || got[0] == "ok" {
				t.Errorf("metric outcomes for the failing module = %v", got)
			}
		})
	}
}

// F53: a blocking provider is cut off by SetupTimeout and reported.
func TestCreate_ProviderTimesOut_Reported(t *testing.T) {
	slow := &fakeProvider{module: "slow", fn: func(ctx context.Context, _ eventsetup.Request) (eventsetup.Result, error) {
		<-ctx.Done()
		return eventsetup.Result{}, ctx.Err() //nolint:wrapcheck // the raw context error is the point
	}}
	after := provider("after", nil, 2)
	r := newIRig(t, []events.SetupProvider{slow.sp(), after.sp()}, func(o *events.Options) { o.SetupTimeout = time.Second })
	src := r.mk("Burn 2026")
	in := validCreate("Burn 2027")
	in.CopyFrom = src.ID
	c, err := r.svc.Create(r.sctx(), in)
	wantNoErr(t, err)
	if len(c.Setup) != 2 || !c.Setup[0].Failed || c.Setup[0].Code != "timeout" {
		t.Fatalf("Setup = %+v, want the first failed with code timeout", c.Setup)
	}
	if c.Setup[1].Failed || after.calls() != 1 {
		t.Errorf("the provider after the timeout: %+v, calls %d", c.Setup[1], after.calls())
	}
	if _, err := r.svc.Get(r.sctx(), c.Event.ID); err != nil {
		t.Errorf("the event is gone after a provider timeout: %v", err)
	}
	if got := setupMeasures(r, "slow"); !slices.Equal(got, []string{"timeout"}) {
		t.Errorf("metric outcomes = %v, want [timeout]", got)
	}
}

// A provider that reports Timeout itself (not through the context) is also "timeout".
func TestCreate_ProviderReportsTimeoutKind_Reported(t *testing.T) {
	p := &fakeProvider{module: "alpha", fn: func(context.Context, eventsetup.Request) (eventsetup.Result, error) {
		return eventsetup.Result{}, errs.E("test", errs.Timeout, errors.New("slow"))
	}}
	r := newIRig(t, []events.SetupProvider{p.sp()})
	in := validCreate("Burn 2027")
	in.CopyFrom = r.mk("Burn 2026").ID
	c, err := r.svc.Create(r.sctx(), in)
	wantNoErr(t, err)
	if len(c.Setup) != 1 || !c.Setup[0].Failed || c.Setup[0].Code != "timeout" {
		t.Errorf("Setup = %+v", c.Setup)
	}
}

// F54
func TestCreate_ProviderPanics_Recovered(t *testing.T) {
	boom := &fakeProvider{module: "boom", fn: func(context.Context, eventsetup.Request) (eventsetup.Result, error) {
		panic("provider bug")
	}}
	after := provider("after", nil, 1)
	r := newIRig(t, []events.SetupProvider{boom.sp(), after.sp()})
	in := validCreate("Burn 2027")
	in.CopyFrom = r.mk("Burn 2026").ID
	c, err := r.svc.Create(r.sctx(), in)
	wantNoErr(t, err)
	if len(c.Setup) != 2 || !c.Setup[0].Failed || c.Setup[0].Code != "internal" {
		t.Fatalf("Setup = %+v, want the first failed with code internal", c.Setup)
	}
	if c.Setup[1].Failed || after.calls() != 1 {
		t.Errorf("the provider after the panic: %+v, calls %d", c.Setup[1], after.calls())
	}
	if _, err := r.svc.Get(r.sctx(), c.Event.ID); err != nil {
		t.Errorf("the event is gone after a provider panic: %v", err)
	}
	// The panic value never reaches the result the admin sees.
	raw, merr := json.Marshal(c)
	wantNoErr(t, merr)
	if strings.Contains(string(raw), "provider bug") || strings.Contains(c.Setup[0].Code, "provider bug") {
		t.Error("the panic value leaked into the result")
	}
}

// F55
func TestCreate_ProviderPermission_Reported(t *testing.T) {
	denied := &fakeProvider{module: "registrations", fn: func(context.Context, eventsetup.Request) (eventsetup.Result, error) {
		return eventsetup.Result{}, errs.E("test", errs.Permission, errors.New("not allowed to copy"))
	}}
	r := newIRig(t, []events.SetupProvider{denied.sp()})
	in := validCreate("Burn 2027")
	in.CopyFrom = r.mk("Burn 2026").ID
	c, err := r.svc.Create(r.sctx(), in)
	wantNoErr(t, err)
	if len(c.Setup) != 1 || !c.Setup[0].Failed || c.Setup[0].Code != "permission" || c.Setup[0].Module != "registrations" {
		t.Errorf("Setup = %+v", c.Setup)
	}
	if got := setupMeasures(r, "registrations"); !slices.Equal(got, []string{"permission"}) {
		t.Errorf("metric outcomes = %v, want [permission]", got)
	}
}

// F64: the audit write after the providers fails; the event and the copies stand.
func TestCreate_SetupAuditFails_ReturnsErrorEventExists(t *testing.T) {
	boom := errs.E("test.audit", errs.Unavailable, errors.New("audit down"))
	p := provider("alpha", nil, 2)
	r := newIRig(t, []events.SetupProvider{p.sp()})
	src := r.mk("Burn 2026")
	r.gate.failAction("events.setup.ran", boom)
	in := validCreate("Burn 2027")
	in.CopyFrom = src.ID
	_, err := r.svc.Create(r.sctx(), in)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the audit error", err)
	}
	wantKind(t, err, errs.Unavailable)
	if p.calls() != 1 {
		t.Errorf("provider calls = %d, want 1", p.calls())
	}
	got, err := r.svc.List(r.sctx(), events.ListQuery{})
	wantNoErr(t, err)
	if !slices.Contains(names(got), "Burn 2027") {
		t.Errorf("events = %v: the event must exist (a reload shows it)", names(got))
	}
	if len(sigsOf(r.bus.Published(), events.SignalCreated)) != 2 {
		t.Errorf("created signals = %d, want 2 (the source and the new event)", len(sigsOf(r.bus.Published(), events.SignalCreated)))
	}
}

// ---- RunSetup --------------------------------------------------------------------------

func TestRunSetup_CallsEachProviderOnce_RecordsOneEntry(t *testing.T) {
	log := &callLog{}
	a, b := provider("alpha", log, 4), provider("beta", log, 0)
	r := newIRig(t, []events.SetupProvider{a.sp(), b.sp()})
	src := r.mk("Burn 2026")
	target := r.mk("Burn 2027")
	e0, s0 := r.mark()

	res, err := r.svc.RunSetup(r.sctx(), target.ID, src.ID)
	wantNoErr(t, err)
	if len(res) != 2 || res[0].Module != "alpha" || res[0].Items != 4 || res[1].Module != "beta" || res[1].Items != 0 {
		t.Errorf("results = %+v", res)
	}
	if a.calls() != 1 || b.calls() != 1 {
		t.Errorf("calls = %d, %d; want 1 each", a.calls(), b.calls())
	}
	if req := a.reqs[0]; req.Event != target.ID || req.CopyFrom != src.ID {
		t.Errorf("request = %+v", req)
	}
	es := r.rec.Recorded()[e0:]
	if len(es) != 1 || es[0].Action != "events.setup.ran" || es[0].Reason != "copy_from:"+src.ID.String() ||
		!isRef(es[0], "events.event", [16]byte(target.ID)) {
		t.Fatalf("audit = %+v", es)
	}
	if len(r.bus.Published()) != s0 {
		t.Error("RunSetup published a signal")
	}
	// Again: idempotence is the providers' job, but the service calls them again.
	if _, err := r.svc.RunSetup(r.sctx(), target.ID, src.ID); err != nil {
		t.Fatalf("second RunSetup: %v", err)
	}
	if a.calls() != 2 || b.calls() != 2 {
		t.Errorf("calls after a second run = %d, %d; want 2 each", a.calls(), b.calls())
	}
}

func TestRunSetup_ProviderFailure_ReportedNotReturned(t *testing.T) {
	bad := &fakeProvider{module: "alpha", fn: func(context.Context, eventsetup.Request) (eventsetup.Result, error) {
		return eventsetup.Result{}, errs.E("test", errs.Permission, errors.New("no"))
	}}
	r := newIRig(t, []events.SetupProvider{bad.sp()})
	src, target := r.mk("Burn 2026"), r.mk("Burn 2027")
	res, err := r.svc.RunSetup(r.sctx(), target.ID, src.ID)
	wantNoErr(t, err)
	if len(res) != 1 || !res[0].Failed || res[0].Code != "permission" {
		t.Errorf("results = %+v", res)
	}
	es := setupEntries(r)
	if len(es) != 1 || es[0].Outcome != audit.Failed {
		t.Errorf("setup.ran = %+v", es)
	}
}

// F63
func TestRunSetup_Closed_ReturnsConflict(t *testing.T) {
	p := provider("alpha", nil, 1)
	r := newIRig(t, []events.SetupProvider{p.sp()})
	src := r.mk("Burn 2026")
	target := r.mkClosed("Burn 2027")
	_, err := r.svc.RunSetup(r.sctx(), target.ID, src.ID)
	wantCoded(t, err, errs.Conflict, "", "closed")
	if p.calls() != 0 {
		t.Error("a provider ran for a closed event")
	}
}

func TestRunSetup_UnknownFrom_ReturnsInvalid(t *testing.T) {
	p := provider("alpha", nil, 1)
	r := newIRig(t, []events.SetupProvider{p.sp()})
	target := r.mk("Burn 2027")
	_, err := r.svc.RunSetup(r.sctx(), target.ID, newEvent())
	wantField(t, err, "copy_from", "unknown")
	if p.calls() != 0 {
		t.Error("a provider ran for an unknown source")
	}
}

func TestRunSetup_UnknownEvent_ReturnsNotFound(t *testing.T) {
	r := newIRig(t, nil)
	src := r.mk("Burn 2026")
	_, err := r.svc.RunSetup(r.sctx(), newEvent(), src.ID)
	wantKind(t, err, errs.NotFound)
}

func TestRunSetup_WithoutManage_ReturnsPermission(t *testing.T) {
	p := provider("alpha", nil, 1)
	r := newIRig(t, []events.SetupProvider{p.sp()})
	src, target := r.mk("Burn 2026"), r.mk("Burn 2027")
	_, err := r.svc.RunSetup(asPerson(ctxT(t), newPerson()), target.ID, src.ID)
	wantKind(t, err, errs.Permission)
	if p.calls() != 0 {
		t.Error("a provider ran for a refused call")
	}
	_, err = r.svc.RunSetup(asSuper(ctxT(t), newPerson()), target.ID, src.ID)
	wantNoErr(t, err)
}

// F65
func TestRunSetup_NoProviders_Empty(t *testing.T) {
	r := newIRig(t, nil)
	src, target := r.mk("Burn 2026"), r.mk("Burn 2027")
	res, err := r.svc.RunSetup(r.sctx(), target.ID, src.ID)
	wantNoErr(t, err)
	if len(res) != 0 {
		t.Errorf("results = %+v, want none", res)
	}
}

// F64, RunSetup half
func TestRunSetup_AuditFails_ReturnsError(t *testing.T) {
	boom := errs.E("test.audit", errs.Unavailable, errors.New("audit down"))
	p := provider("alpha", nil, 1)
	r := newIRig(t, []events.SetupProvider{p.sp()})
	src, target := r.mk("Burn 2026"), r.mk("Burn 2027")
	r.gate.failAction("events.setup.ran", boom)
	_, err := r.svc.RunSetup(r.sctx(), target.ID, src.ID)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the audit error", err)
	}
	if p.calls() != 1 {
		t.Errorf("provider calls = %d, want 1", p.calls())
	}
}

// A cancelled caller's ctx stops the providers: each provider's ctx derives from it, and
// none runs with a live ctx once the caller has given up.
func TestRunSetup_CancelledContext_StopsProviders(t *testing.T) {
	t.Run("cancelled before the call: no provider runs", func(t *testing.T) {
		a, b := provider("alpha", nil, 1), provider("beta", nil, 1)
		r := newIRig(t, []events.SetupProvider{a.sp(), b.sp()})
		src, target := r.mk("Burn 2026"), r.mk("Burn 2027")
		ctx, cancel := context.WithCancel(r.sctx())
		cancel()
		_, err := r.svc.RunSetup(ctx, target.ID, src.ID)
		wantKind(t, err, errs.Canceled)
		if a.calls() != 0 || b.calls() != 0 {
			t.Errorf("providers ran for a cancelled caller: %d, %d", a.calls(), b.calls())
		}
	})
	t.Run("cancelled by a provider: the next one does not run live", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		var r *irig
		first := &fakeProvider{module: "alpha", fn: func(pctx context.Context, _ eventsetup.Request) (eventsetup.Result, error) {
			cancel()
			<-pctx.Done() // the provider's ctx must derive from the caller's
			return eventsetup.Result{}, pctx.Err()
		}}
		second := &fakeProvider{module: "beta", fn: func(pctx context.Context, _ eventsetup.Request) (eventsetup.Result, error) {
			if pctx.Err() == nil {
				t.Error("a provider ran with a live ctx after the caller cancelled")
			}
			return eventsetup.Result{}, pctx.Err()
		}}
		r = newIRig(t, []events.SetupProvider{first.sp(), second.sp()})
		src, target := r.mk("Burn 2026"), r.mk("Burn 2027")
		ctx = asPerson(ctx, r.steward)
		done := make(chan struct{})
		var err error
		go func() {
			defer close(done)
			_, err = r.svc.RunSetup(ctx, target.ID, src.ID)
		}()
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			t.Fatal("RunSetup did not return after the caller cancelled")
		}
		if first.calls() != 1 {
			t.Errorf("first provider calls = %d", first.calls())
		}
		if err != nil && !errs.Is(err, errs.Canceled) {
			t.Errorf("kind = %v, want Canceled or success with a failed result", errs.KindOf(err))
		}
	})
}

// Service-level sanity for a typed zero: a zero CopyFrom ID is never copied.
func TestCreate_ZeroCopyFrom_IsNoCopy(t *testing.T) {
	p := provider("alpha", nil, 1)
	r := newIRig(t, []events.SetupProvider{p.sp()})
	in := validCreate("Burn 2027")
	in.CopyFrom = model.EventID{}
	c, err := r.svc.Create(r.sctx(), in)
	wantNoErr(t, err)
	if len(c.Setup) != 0 || p.calls() != 0 {
		t.Errorf("Setup = %+v, calls %d", c.Setup, p.calls())
	}
}
