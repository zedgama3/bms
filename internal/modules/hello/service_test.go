package hello_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/signals/signalstest"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/modules/hello"
)

func rawDeps(t *testing.T) module.Deps {
	t.Helper()
	d, _, _ := rawFakes(t)
	return d
}

func rawFakes(t *testing.T) (module.Deps, *dbtest.Fake, *signalstest.Fake) {
	t.Helper()
	fdb := dbtest.NewFake()
	c := telemetrytest.New(t)
	sig, err := signalstest.NewFake(hello.Manifest.Name, hello.Manifest.Publishes, hello.Manifest.Subscribes)
	if err != nil {
		t.Fatalf("signalstest.NewFake: %v", err)
	}
	return module.Deps{
		Logger:  c.Logger(),
		Tracer:  c.Tracer("module.hello"),
		Clock:   clock.NewFake(epoch),
		DB:      fdb,
		Signals: sig,
		Links:   &module.Links{},
	}, fdb, sig
}

// F1
func TestNew_NilDependency_ReturnsInvalid(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*module.Deps)
	}{
		{"logger", func(d *module.Deps) { d.Logger = nil }},
		{"tracer", func(d *module.Deps) { d.Tracer = nil }},
		{"clock", func(d *module.Deps) { d.Clock = nil }},
		{"db", func(d *module.Deps) { d.DB = nil }},
		{"signals", func(d *module.Deps) { d.Signals = nil }},
		{"links", func(d *module.Deps) { d.Links = nil }},
		{"all", func(d *module.Deps) { *d = module.Deps{} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := rawDeps(t)
			tc.mut(&d)
			m, err := hello.New(ctxT(t), d)
			if !errs.Is(err, errs.Invalid) {
				t.Fatalf("New err = %v, want Invalid", err)
			}
			if m != nil {
				t.Errorf("New returned a module with an error")
			}
		})
	}
}

func TestNew_AllDependencies_BuildsWithoutTouchingTheDatabase(t *testing.T) {
	d, fdb, sig := rawFakes(t)
	m, err := hello.New(ctxT(t), d)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if m == nil {
		t.Fatal("New returned nil")
	}
	if calls := fdb.Calls(); len(calls) != 0 {
		t.Errorf("New ran %d statements; it must read nothing (app decision 7)", len(calls))
	}
	if hs := sig.Handlers(); len(hs) != 0 {
		t.Errorf("New subscribed %v; only Start may", hs)
	}
}

func TestNew_ReadOnlyDatabase_Succeeds(t *testing.T) {
	d, fdb, _ := rawFakes(t)
	fdb.SetReadOnly(true)
	if _, err := hello.New(ctxT(t), d); err != nil {
		t.Fatalf("New on a read-only copy: %v", err)
	}
}

// ---- Start and Stop ----

// F3
func TestStart_Twice_ReturnsConflict(t *testing.T) {
	f := newFx(t, cfg{})
	f.start(t)
	if err := f.mod.Start(ctxT(t)); !errs.Is(err, errs.Conflict) {
		t.Fatalf("second Start err = %v, want Conflict", err)
	}
}

func TestStart_SubscribesOnlyDeclaredNames_AndWritesNothing(t *testing.T) {
	f := newFx(t, cfg{})
	f.start(t)
	got := f.sig.Handlers()
	if len(got) != 1 || got[0] != hello.SignalGreeted {
		t.Errorf("Handlers = %v, want [%s]", got, hello.SignalGreeted)
	}
	if calls := f.db.Calls(); len(calls) != 0 {
		t.Errorf("Start ran %d statements; it must write nothing", len(calls))
	}
}

func TestStart_ReadOnlyDatabase_Succeeds(t *testing.T) {
	f := newFx(t, cfg{readOnly: true})
	f.start(t)
}

// F4
func TestStart_SubscribeFails_ReturnsBusKind(t *testing.T) {
	for _, kind := range []errs.Kind{errs.Invalid, errs.Conflict, errs.Internal} {
		t.Run(kind.String(), func(t *testing.T) {
			f := newFx(t, cfg{wrapBus: func(b *signalstest.Fake) signals.Bus {
				return busFn{Bus: b, subscribe: func(signals.Name, signals.Handler) error {
					return errs.E("test.bus", kind, errors.New("refused"))
				}}
			}})
			err := f.mod.Start(ctxT(t))
			if !errs.Is(err, kind) {
				t.Fatalf("Start err = %v, want %v", err, kind)
			}
			if err := f.mod.Stop(ctxT(t)); err != nil {
				t.Errorf("Stop after a failed Start = %v, want nil", err)
			}
		})
	}
}

func TestStart_HandlerAlreadyRegistered_ReturnsConflict(t *testing.T) {
	f := newFx(t, cfg{})
	if err := f.sig.Subscribe(hello.SignalGreeted, func(context.Context, signals.Signal) error { return nil }); err != nil {
		t.Fatalf("pre-subscribe: %v", err)
	}
	if err := f.mod.Start(ctxT(t)); !errs.Is(err, errs.Conflict) {
		t.Fatalf("Start err = %v, want Conflict", err)
	}
}

// F5
func TestStart_BannerKeyWrongPrefix_ReturnsInvalid(t *testing.T) {
	cases := []struct {
		name string
		key  string
		ok   bool
	}{
		{"own prefix", "module.rogue.banner.hi", true},
		{"hyphenated ident", "module.rogue.banner.big-news", true},
		{"another module's prefix", "module.hello.banner.welcome", false},
		{"empty ident", "module.rogue.banner.", false},
		{"no module segment", "banner.hi", false},
		{"wrong middle segment", "module.rogue.nav.hi", false},
		{"prefix only", "module.rogue", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFx(t, cfg{rogueKey: tc.key})
			err := f.mod.Start(ctxT(t))
			if tc.ok {
				if err != nil {
					t.Fatalf("Start: %v", err)
				}
				return
			}
			if !errs.Is(err, errs.Invalid) {
				t.Fatalf("Start err = %v, want Invalid", err)
			}
			if got := f.sig.Handlers(); len(got) != 0 {
				t.Errorf("a failed Start left handlers %v", got)
			}
		})
	}
}

// F6
func TestStop_CtxDone_ReturnsTimeout(t *testing.T) {
	t.Run("canceled", func(t *testing.T) {
		f := newFx(t, cfg{})
		f.start(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := f.mod.Stop(ctx); !errs.Is(err, errs.Canceled) {
			t.Fatalf("Stop err = %v, want Canceled", err)
		}
	})
	t.Run("deadline passed", func(t *testing.T) {
		f := newFx(t, cfg{})
		f.start(t)
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		defer cancel()
		if err := f.mod.Stop(ctx); !errs.Is(err, errs.Timeout) {
			t.Fatalf("Stop err = %v, want Timeout", err)
		}
	})
}

// F7
func TestStop_WithoutStart_IsNil(t *testing.T) {
	f := newFx(t, cfg{})
	if err := f.mod.Stop(ctxT(t)); err != nil {
		t.Fatalf("Stop without Start = %v, want nil", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := f.mod.Stop(ctx); err != nil {
		t.Fatalf("Stop without Start on a cancelled ctx = %v, want nil", err)
	}
}

func TestStop_Twice_IsNil(t *testing.T) {
	f := newFx(t, cfg{})
	f.start(t)
	for i := range 2 {
		if err := f.mod.Stop(ctxT(t)); err != nil {
			t.Fatalf("Stop #%d = %v, want nil", i+1, err)
		}
	}
}

// ---- PublicGreeting ----

func TestPublicGreeting_NoActorNoPermission_ReturnsWordAndOwnBanner(t *testing.T) {
	f := newFx(t, cfg{seams: hello.Seams{Actor: noActor, Allow: hello.DenyAll{}}})
	f.start(t)
	got, err := f.mod.Service().PublicGreeting(ctxT(t))
	if err != nil {
		t.Fatalf("PublicGreeting: %v", err)
	}
	if got.Greeting != "howdy" {
		t.Errorf("Greeting = %q, want the source's word", got.Greeting)
	}
	want := hello.Banner{Module: "hello", Key: "module.hello.banner.welcome"}
	if len(got.Banners) != 1 || got.Banners[0] != want {
		t.Errorf("Banners = %+v, want [%+v]", got.Banners, want)
	}
	if sp := f.spansNamed("hello.public-greeting"); len(sp) != 1 || sp[0].Attrs["banners"] != "1" {
		t.Errorf("spans = %+v, want one hello.public-greeting with banners=1", sp)
	}
}

func TestPublicGreeting_OtherModuleContributes_ListsBoth(t *testing.T) {
	f := newFx(t, cfg{rogueKey: "module.rogue.banner.hi"})
	f.start(t)
	got, err := f.mod.Service().PublicGreeting(ctxT(t))
	if err != nil {
		t.Fatalf("PublicGreeting: %v", err)
	}
	have := map[hello.Banner]bool{}
	for _, b := range got.Banners {
		have[b] = true
	}
	for _, want := range []hello.Banner{
		{Module: "hello", Key: "module.hello.banner.welcome"},
		{Module: "rogue", Key: "module.rogue.banner.hi"},
	} {
		if !have[want] {
			t.Errorf("Banners %+v lacks %+v", got.Banners, want)
		}
	}
	if len(got.Banners) != 2 {
		t.Errorf("len(Banners) = %d, want 2", len(got.Banners))
	}
	if sp := f.spansNamed("hello.public-greeting"); len(sp) != 1 || sp[0].Attrs["banners"] != "2" {
		t.Errorf("spans = %+v, want banners=2", sp)
	}
}

// F8
func TestPublicGreeting_SourceFails_ReturnsKind(t *testing.T) {
	for _, kind := range []errs.Kind{errs.Unavailable, errs.Timeout, errs.Internal} {
		t.Run(kind.String(), func(t *testing.T) {
			f := newFx(t, cfg{seams: hello.Seams{Greeting: wordFails(errs.E("test.source", kind, errors.New("down")))}})
			f.start(t)
			if _, err := f.mod.Service().PublicGreeting(ctxT(t)); !errs.Is(err, kind) {
				t.Fatalf("err = %v, want %v", err, kind)
			}
		})
	}
}

// F9
func TestPublicGreeting_LinksRefuses_ReturnsInternal(t *testing.T) {
	m, err := hello.NewWithSeams(rawDeps(t), hello.Seams{})
	if err != nil {
		t.Fatalf("NewWithSeams: %v", err)
	}
	if _, err := m.Service().PublicGreeting(ctxT(t)); !errs.Is(err, errs.Internal) {
		t.Fatalf("err = %v, want Internal", err)
	}
}

func TestPublicGreeting_DefaultSource_ReturnsDeclaredDefault(t *testing.T) {
	f := newFx(t, cfg{declaredDefault: true})
	f.start(t)
	got, err := f.mod.Service().PublicGreeting(ctxT(t))
	if err != nil {
		t.Fatalf("PublicGreeting: %v", err)
	}
	if got.Greeting != hello.DefaultGreeting {
		t.Errorf("Greeting = %q, want %q", got.Greeting, hello.DefaultGreeting)
	}
}

func TestPublicGreeting_CancelledContextWithDefaultSource_ReturnsCanceled(t *testing.T) {
	f := newFx(t, cfg{declaredDefault: true})
	f.start(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.mod.Service().PublicGreeting(ctx); !errs.Is(err, errs.Canceled) {
		t.Fatalf("err = %v, want Canceled", err)
	}
}

func TestStop_Twice_SecondWithDoneCtx_IsNil(t *testing.T) {
	f := newFx(t, cfg{})
	f.start(t)
	if err := f.mod.Stop(ctxT(t)); err != nil {
		t.Fatalf("first Stop = %v, want nil", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := f.mod.Stop(ctx); err != nil {
		t.Fatalf("second Stop with a cancelled ctx = %v, want nil (already stopped)", err)
	}
}
