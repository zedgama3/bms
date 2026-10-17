package module_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// recordingSet builds alpha, beta, gamma (in that order, independent) recording Start/Stop.
func recordingSet(t *testing.T, mods map[string]*plain) (*module.Set, *recorder) {
	t.Helper()
	rec := &recorder{}
	var es []entry
	for _, n := range []string{"alpha", "beta", "gamma"} {
		p := mods[n]
		if p == nil {
			p = &plain{}
		}
		p.name, p.rec = n, rec
		es = append(es, e(mf(n), constFactory(p)))
	}
	return mustBuild(t, reg(t, es...), enabled("alpha", "beta", "gamma")), rec
}

func TestSetStartStop_OrderAndReverse(t *testing.T) {
	s, rec := recordingSet(t, nil)
	ctx := t.Context()
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	want := []string{"start:alpha", "start:beta", "start:gamma", "stop:gamma", "stop:beta", "stop:alpha"}
	if got := rec.entries(); !slices.Equal(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
}

func TestSetStop_WithoutStart_IsNil(t *testing.T) {
	s, rec := recordingSet(t, nil)
	if err := s.Stop(t.Context()); err != nil {
		t.Fatalf("Stop without Start = %v", err)
	}
	if got := rec.entries(); len(got) != 0 {
		t.Fatalf("Stop called modules that were never started: %v", got)
	}
}

func TestSetStop_Twice_IsNil(t *testing.T) {
	s, rec := recordingSet(t, nil)
	ctx := t.Context()
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	n := len(rec.entries())
	if err := s.Stop(ctx); err != nil {
		t.Fatalf("second Stop = %v", err)
	}
	if got := len(rec.entries()); got != n {
		t.Fatalf("second Stop called modules again (%d -> %d entries)", n, got)
	}
}

func TestSetStart_ModuleFails_StopsEarlierInReverse(t *testing.T) {
	s, rec := recordingSet(t, map[string]*plain{
		"beta": {startErr: errs.E("beta.Start", errs.Unavailable, errBoom)},
	})
	err := s.Start(t.Context())
	wantKind(t, err, errs.Unavailable)
	want := []string{"start:alpha", "start:beta", "stop:alpha"}
	if got := rec.entries(); !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v (gamma never started; only alpha stopped)", got, want)
	}
}

func TestSetStart_FirstModuleFails_StopsNothing(t *testing.T) {
	s, rec := recordingSet(t, map[string]*plain{
		"alpha": {startErr: errs.E("alpha.Start", errs.Timeout, errBoom)},
	})
	wantKind(t, s.Start(t.Context()), errs.Timeout)
	if got := rec.entries(); !slices.Equal(got, []string{"start:alpha"}) {
		t.Fatalf("got %v", got)
	}
}

func TestSetStart_ModuleFails_StopErrorsAreJoined(t *testing.T) {
	s, rec := recordingSet(t, map[string]*plain{
		"alpha": {stopErr: errs.E("alpha.Stop", errs.Conflict, errBoom)},
		"beta":  {startErr: errs.E("beta.Start", errs.Unavailable, errBoom)},
	})
	err := s.Start(t.Context())
	wantKind(t, err, errs.Unavailable)
	if got := rec.entries(); !slices.Contains(got, "stop:alpha") {
		t.Fatalf("alpha was not stopped: %v", got)
	}
	if !errorsContainKind(err, errs.Conflict) {
		t.Errorf("the failing Stop's error was dropped: %v", err)
	}
}

// errorsContainKind reports whether any error in err's tree has the given kind.
func errorsContainKind(err error, k errs.Kind) bool {
	if err == nil {
		return false
	}
	if errs.Is(err, k) {
		return true
	}
	switch u := err.(type) { //nolint:errorlint // walking the tree by hand
	case interface{ Unwrap() []error }:
		for _, c := range u.Unwrap() {
			if errorsContainKind(c, k) {
				return true
			}
		}
	case interface{ Unwrap() error }:
		return errorsContainKind(u.Unwrap(), k)
	}
	return false
}

func TestSetStart_Twice_ReturnsConflict(t *testing.T) {
	s, rec := recordingSet(t, nil)
	ctx := t.Context()
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	wantKind(t, s.Start(ctx), errs.Conflict)
	if n := count(rec.entries(), "start:alpha"); n != 1 {
		t.Fatalf("alpha started %d times", n)
	}
}

func TestSetStop_ContinuesPastFailuresAndJoins(t *testing.T) {
	s, rec := recordingSet(t, map[string]*plain{
		"gamma": {stopErr: errs.E("gamma.Stop", errs.Unavailable, errBoom)},
		"beta":  {stopErr: errs.E("beta.Stop", errs.Timeout, errBoom)},
	})
	ctx := t.Context()
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	err := s.Stop(ctx)
	if err == nil {
		t.Fatal("Stop = nil, want the joined failures")
	}
	for _, k := range []errs.Kind{errs.Unavailable, errs.Timeout} {
		if !errorsContainKind(err, k) {
			t.Errorf("joined error lacks a %v failure: %v", k, err)
		}
	}
	got := rec.entries()
	if want := []string{"stop:gamma", "stop:beta", "stop:alpha"}; !slices.Equal(got[3:], want) {
		t.Fatalf("stops = %v, want all three in reverse order", got[3:])
	}
}

func TestSetStop_ContextAlreadyDone_StillAttemptsEveryModule(t *testing.T) {
	s, rec := recordingSet(t, nil)
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_ = s.Stop(ctx) //nolint:errcheck // the result depends on the modules; this test checks only that each one was called
	if got := rec.entries(); len(got) != 6 {
		t.Fatalf("with a done ctx Stop called %d times in total, want 6: %v", len(got), got)
	}
}

func TestSetRoutesAndPersonData_InBuildOrder(t *testing.T) {
	frag := func(n string) []byte { return []byte(`{"tag":"` + n + `"}`) }
	r := reg(t,
		e(mf("gamma"), constFactory(&apiMod{plain: &plain{}, routes: []module.Route{route("GET", "/g")}, openapi: frag("gamma")})),
		e(mf("alpha"), constFactory(&apiMod{plain: &plain{}, routes: []module.Route{route("GET", "/a"), route("POST", "/a")}, openapi: frag("alpha")})),
		e(mf("beta"), okFactory("beta")), // no routes: not listed
	)
	s := mustBuild(t, r, enabled("alpha", "beta", "gamma"))

	rs := s.Routes()
	if len(rs) != 2 || rs[0].Module != "alpha" || rs[1].Module != "gamma" {
		t.Fatalf("Routes() = %+v, want alpha then gamma only", rs)
	}
	if len(rs[0].Routes) != 2 || string(rs[0].OpenAPI) != string(frag("alpha")) {
		t.Fatalf("alpha routes = %+v", rs[0])
	}

	var pd []string
	for _, p := range s.PersonData() {
		if p.PersonData == nil {
			t.Errorf("module %s has nil PersonData", p.Module)
		}
		pd = append(pd, p.Module)
	}
	if !slices.Equal(pd, []string{"alpha", "beta", "gamma"}) {
		t.Fatalf("PersonData() order = %v", pd)
	}
	var mods []string
	for _, b := range s.Modules() {
		mods = append(mods, b.Manifest.Name)
	}
	if !slices.Equal(mods, pd) {
		t.Fatalf("Modules() = %v", mods)
	}
}

func TestSetRoutes_SafeForConcurrentUse(t *testing.T) {
	s := mustBuild(t, simple(t, mf("alpha"), mf("beta")), enabled("alpha", "beta"))
	done := make(chan struct{})
	for range 8 {
		go func() {
			defer func() { done <- struct{}{} }()
			s.Modules()
			s.Routes()
			s.PersonData()
		}()
	}
	for range 8 {
		<-done
	}
}

func TestSetContributors_NotCorePoint_ReturnsInvalid(t *testing.T) {
	core := module.ExtensionPointDecl{Name: "people.person-page", Accepts: acceptsString}
	r := reg(t,
		e(mf("alpha", defines("alpha.page")), okFactory("alpha")),
		e(mf("beta", contributes("people.person-page", "alpha.page")),
			constFactory(&contribMod{&plain{}, map[module.ExtensionPoint]any{"people.person-page": "x", "alpha.page": "y"}})),
	)
	s := mustBuild(t, r, module.Env{Enabled: []string{"alpha", "beta"}, CoreExtensionPoints: []module.ExtensionPointDecl{core}})

	got, err := s.Contributors("people.person-page")
	if err != nil || len(got) != 1 || got[0].Module != "beta" || got[0].Value != "x" {
		t.Fatalf("core point: %+v, %v", got, err)
	}
	_, err = s.Contributors("alpha.page") // module-defined: goes through Links, not Set
	wantKind(t, err, errs.Invalid)
	_, err = s.Contributors("nosuch.point")
	wantKind(t, err, errs.Invalid)
}

func TestSetContributors_CorePointWithNoContributors_IsEmpty(t *testing.T) {
	core := module.ExtensionPointDecl{Name: "people.person-page", Accepts: acceptsString}
	s := mustBuild(t, simple(t, mf("alpha")), module.Env{Enabled: []string{"alpha"}, CoreExtensionPoints: []module.ExtensionPointDecl{core}})
	got, err := s.Contributors("people.person-page")
	if err != nil || len(got) != 0 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

// ctxProbe is a module whose Start and Stop can act on the context they are given.
type ctxProbe struct {
	*plain
	onStart func(context.Context) error
	onStop  func(context.Context) error
}

func (p *ctxProbe) Start(ctx context.Context) error {
	if err := p.plain.Start(ctx); err != nil {
		return err
	}
	if p.onStart != nil {
		return p.onStart(ctx)
	}
	return nil
}

func (p *ctxProbe) Stop(ctx context.Context) error {
	if err := p.plain.Stop(ctx); err != nil {
		return err
	}
	if p.onStop != nil {
		return p.onStop(ctx)
	}
	return nil
}

func TestStartRollbackTimeout_IsTenSeconds(t *testing.T) {
	if module.StartRollbackTimeout != 10*time.Second {
		t.Fatalf("StartRollbackTimeout = %v, want 10s", module.StartRollbackTimeout)
	}
}

// F37 (amended): the rollback runs under a fresh deadline, not the caller's spent one.
func TestSetStart_ModuleFailsOnExpiredCtx_RollbackStillWaits(t *testing.T) {
	rec := &recorder{}
	var stopErr error
	var stopHadDeadline bool
	var stopRemaining time.Duration
	alpha := &ctxProbe{plain: &plain{name: "alpha", rec: rec}}
	alpha.onStop = func(ctx context.Context) error {
		stopErr = ctx.Err()
		var dl time.Time
		dl, stopHadDeadline = ctx.Deadline()
		stopRemaining = time.Until(dl)
		return errs.E("alpha.Stop", errs.KindOf(stopErr), stopErr)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	beta := &ctxProbe{plain: &plain{name: "beta", rec: rec}}
	beta.onStart = func(c context.Context) error {
		cancel() // the caller's start budget is spent
		return errs.E("beta.Start", errs.Timeout, c.Err())
	}
	gamma := &plain{name: "gamma", rec: rec}
	s := mustBuild(t, reg(t, e(mf("alpha"), constFactory(alpha)), e(mf("beta"), constFactory(beta)),
		e(mf("gamma"), constFactory(gamma))), enabled("alpha", "beta", "gamma"))

	err := s.Start(ctx)
	wantKind(t, err, errs.Timeout)
	if want := []string{"start:alpha", "start:beta", "stop:alpha"}; !slices.Equal(rec.entries(), want) {
		t.Fatalf("got %v, want %v", rec.entries(), want)
	}
	if stopErr != nil {
		t.Fatalf("the rollback Stop saw a done context (%v); it must run under a fresh one", stopErr)
	}
	if !stopHadDeadline || stopRemaining <= 0 || stopRemaining > module.StartRollbackTimeout {
		t.Fatalf("rollback context deadline: has=%v remaining=%v, want within (0, %v]", stopHadDeadline, stopRemaining, module.StartRollbackTimeout)
	}
}

// The rollback context keeps the caller's values (WithoutCancel), only its cancellation goes.
func TestSetStart_Rollback_KeepsCallerValues(t *testing.T) {
	type key struct{}
	rec := &recorder{}
	var got any
	alpha := &ctxProbe{plain: &plain{name: "alpha", rec: rec}}
	alpha.onStop = func(ctx context.Context) error { got = ctx.Value(key{}); return nil }
	beta := &plain{name: "beta", rec: rec, startErr: errs.E("beta.Start", errs.Unavailable, errBoom)}
	s := mustBuild(t, reg(t, e(mf("alpha"), constFactory(alpha)), e(mf("beta"), constFactory(beta))), enabled("alpha", "beta"))
	wantKind(t, s.Start(context.WithValue(t.Context(), key{}, "v")), errs.Unavailable)
	if got != "v" {
		t.Fatalf("rollback context lost the caller's values: %v", got)
	}
}
