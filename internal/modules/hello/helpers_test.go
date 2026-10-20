package hello_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/contracts/greeter"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/notify/notifytest"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/signals/signalstest"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/hello"
	"github.com/zedgama3/bms/internal/modules/hello/greeting"
)

var (
	// alice and bob are invented people.
	alice = model.PersonID{0x21, 0x5c, 0x90, 0x14, 0xa3, 0x77, 0x4e, 0x02, 0xb1, 0x6d, 0x48, 0xee, 0x0f, 0x39, 0xc5, 0x86}
	bob   = model.PersonID{0x62, 0x0b, 0xd3, 0x58, 0x1e, 0x9a, 0x41, 0x7c, 0x85, 0x30, 0xf2, 0x6b, 0xa9, 0x14, 0x07, 0xdd}

	epoch = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
)

const testTimeout = 30 * time.Second

func ctxT(t testing.TB) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	t.Cleanup(cancel)
	return ctx
}

// actorIs returns an actorFunc that always reports p.
func actorIs(p model.PersonID) func(context.Context) (model.PersonID, bool) {
	return func(context.Context) (model.PersonID, bool) { return p, true }
}

func noActor(context.Context) (model.PersonID, bool) { return model.PersonID{}, false }

func wordIs(w string) func(context.Context) (string, error) {
	return func(context.Context) (string, error) { return w, nil }
}

func wordFails(err error) func(context.Context) (string, error) {
	return func(context.Context) (string, error) { return "", err }
}

// checker is a permissionChecker that grants a fixed set and records what it was asked.
type checker struct {
	mu      sync.Mutex
	granted map[authz.Permission]bool
	asked   []authz.Permission
	deny    error
}

func grants(ps ...authz.Permission) *checker {
	c := &checker{granted: map[authz.Permission]bool{}}
	for _, p := range ps {
		c.granted[p] = true
	}
	return c
}

func (c *checker) Allow(_ context.Context, p authz.Permission) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.asked = append(c.asked, p)
	if c.granted[p] {
		return nil
	}
	return c.deny
}

func (c *checker) askedFor() []authz.Permission {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]authz.Permission(nil), c.asked...)
}

// txStore wraps a FakeStore and issues one Exec on the fake database per Insert, so the
// test can read Call.InTx.
type txStore struct {
	*hello.FakeStore
	db *dbtest.Fake
}

func (s *txStore) Insert(ctx context.Context, g greeting.Greeting) error {
	if _, err := s.db.Exec(ctx, "INSERT hello.greeting"); err != nil {
		return fmt.Errorf("exec: %w", err)
	}
	return s.FakeStore.Insert(ctx, g) //nolint:wrapcheck // test fake: the kind must reach the caller unchanged
}

// busFn wraps a signals.Bus and lets a test refuse Publish or Subscribe.
type busFn struct {
	signals.Bus
	publish   func(ctx context.Context, name signals.Name, p signals.Payload) error
	subscribe func(name signals.Name, h signals.Handler) error
}

func (b busFn) Publish(ctx context.Context, name signals.Name, p signals.Payload) error {
	if b.publish != nil {
		if err := b.publish(ctx, name, p); err != nil {
			return err
		}
	}
	return b.Bus.Publish(ctx, name, p) //nolint:wrapcheck // test wrapper: the bus error must reach the caller unchanged
}

func (b busFn) Subscribe(name signals.Name, h signals.Handler) error {
	if b.subscribe != nil {
		if err := b.subscribe(name, h); err != nil {
			return err
		}
	}
	return b.Bus.Subscribe(name, h) //nolint:wrapcheck // test wrapper: the bus error must reach the caller unchanged
}

// rogue is a stand-in module that contributes one banner to hello's extension point.
type rogue struct{ key string }

func (rogue) Start(context.Context) error { return nil }
func (rogue) Stop(context.Context) error  { return nil }
func (rogue) ExportPerson(context.Context, model.PersonID) (module.Export, error) {
	return module.Export{}, nil
}
func (rogue) DeletePerson(context.Context, model.PersonID) (module.Deletion, error) {
	return module.Deletion{}, nil
}
func (r rogue) Contributions() map[module.ExtensionPoint]any {
	return map[module.ExtensionPoint]any{hello.PointBanner: greeter.Banner{Key: r.key}}
}

var rogueManifest = module.Manifest{Name: "rogue", ContributesTo: []module.ExtensionPoint{hello.PointBanner}}

// cfg says how a fixture differs from the default (alice signed in, everything allowed).
type cfg struct {
	seams    hello.Seams
	readOnly bool
	wrapBus  func(*signalstest.Fake) signals.Bus
	// wrapStore, when set, builds the store from the fixture's database fake and a fresh
	// FakeStore.
	wrapStore func(*dbtest.Fake, *hello.FakeStore) interface {
		Insert(ctx context.Context, g greeting.Greeting) error
		CountByPerson(ctx context.Context, person model.PersonID) (int, error)
		ListByPerson(ctx context.Context, person model.PersonID) ([]greeting.Greeting, error)
		DeleteByPerson(ctx context.Context, person model.PersonID) (int, error)
	}
	declaredDefault bool   // leave the greeting source at the module's M0 default
	rogueKey        string // when set, a module "rogue" contributes this banner key
}

// fx is one built hello module with its fakes.
type fx struct {
	mod   *hello.Module
	set   *module.Set
	plan  *module.Plan
	db    *dbtest.Fake
	sig   *signalstest.Fake
	mail  *notifytest.Fake
	cap   *telemetrytest.Capture
	clk   *clock.Fake
	store *hello.FakeStore
}

type noSchema struct{}

func (noSchema) CheckSchema(context.Context, string, fs.FS) error { return nil }

func newFx(t *testing.T, c cfg) *fx {
	t.Helper()
	f := &fx{cap: telemetrytest.New(t), clk: clock.NewFake(epoch), db: dbtest.NewFake(), mail: notifytest.NewFake(hello.Manifest.Name)}
	sig, err := signalstest.NewFake(hello.Manifest.Name, hello.Manifest.Publishes, hello.Manifest.Subscribes)
	if err != nil {
		t.Fatalf("signalstest.NewFake: %v", err)
	}
	f.sig = sig
	f.db.SetReadOnly(c.readOnly)
	s := c.seams
	if s.Store == nil {
		f.store = hello.NewFakeStore()
		s.Store = f.store
		if c.wrapStore != nil {
			s.Store = c.wrapStore(f.db, f.store)
		}
	} else if existing, ok := s.Store.(*hello.FakeStore); ok {
		f.store = existing
	}
	if s.Actor == nil {
		s.Actor = actorIs(alice)
	}
	if s.Allow == nil {
		s.Allow = hello.AllowAll{}
	}
	if s.Greeting == nil && !c.declaredDefault {
		s.Greeting = wordIs("howdy")
	}
	reg := module.NewRegistry()
	env := module.Env{Enabled: []string{hello.Manifest.Name}}
	factory := func(_ context.Context, deps module.Deps) (module.Module, error) {
		m, err := hello.NewWithSeams(deps, s)
		if err != nil {
			return nil, fmt.Errorf("NewWithSeams: %w", err)
		}
		f.mod = m
		return m, nil
	}
	if err := reg.Add(hello.Manifest, factory); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if c.rogueKey != "" {
		key := c.rogueKey
		if err := reg.Add(rogueManifest, func(context.Context, module.Deps) (module.Module, error) { return rogue{key: key}, nil }); err != nil {
			t.Fatalf("Add rogue: %v", err)
		}
		env.Enabled = append(env.Enabled, rogueManifest.Name)
	}
	plan, err := reg.Resolve(env)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	f.plan = plan
	provision := func(_ context.Context, m module.Manifest) (module.Deps, error) {
		d := module.Deps{
			Logger: f.cap.Logger(),
			Tracer: f.cap.Tracer("module." + m.Name),
			Clock:  f.clk,
			DB:     dbtest.NewFake(),
			Notify: notifytest.NewFake(m.Name),
		}
		if m.Name != hello.Manifest.Name {
			bus, err := signalstest.NewFake(m.Name, m.Publishes, m.Subscribes)
			if err != nil {
				return module.Deps{}, fmt.Errorf("signalstest.NewFake: %w", err)
			}
			d.Signals = bus
		} else {
			d.DB = f.db
			d.Notify = f.mail
			if c.wrapBus != nil {
				d.Signals = c.wrapBus(f.sig)
			} else {
				d.Signals = f.sig
			}
		}
		return d, nil
	}
	set, err := plan.Build(ctxT(t), noSchema{}, provision)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	f.set = set
	if f.mod == nil {
		t.Fatal("the factory did not run")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()
		if err := f.mod.Stop(ctx); err != nil {
			t.Errorf("Stop at cleanup: %v", err)
		}
	})
	return f
}

// start starts the module directly (not the whole Set).
func (f *fx) start(t *testing.T) {
	t.Helper()
	if err := f.mod.Start(ctxT(t)); err != nil {
		t.Fatalf("Start: %v", err)
	}
}

// deliver hands the handler a hello.greeted signal for person with a fresh ID.
func (f *fx) deliver(t *testing.T, person model.PersonID) signals.Signal {
	t.Helper()
	s := greetedSignal(person)
	if err := f.sig.Deliver(ctxT(t), s); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	return s
}

func greetedSignal(person model.PersonID) signals.Signal {
	return signals.Signal{
		ID:      model.NewID[signals.SignalID](),
		Name:    hello.SignalGreeted,
		At:      epoch,
		Payload: signals.NewPayload(signals.Ref(hello.PayloadPerson, person)),
	}
}

// spansNamed returns the captured spans with the given name.
func (f *fx) spansNamed(name string) []telemetry.SpanData {
	var out []telemetry.SpanData
	for _, s := range f.cap.Spans() {
		if s.Name == name {
			out = append(out, s)
		}
	}
	return out
}

func mustJSONObject(t *testing.T, b []byte) map[string]json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("not a JSON object: %v (%q)", err, b)
	}
	return m
}
