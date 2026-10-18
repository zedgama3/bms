//go:build integration

package hello_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/modules/hello"
	"github.com/zedgama3/bms/internal/modules/hello/postgres"
)

// realFx is hello over a real pool, a real signals.Dispatcher scoped to hello, the real
// postgres store and a fake actor with an allow-all checker.
type realFx struct {
	mod  *hello.Module
	pool *db.Pool
	disp *signals.Dispatcher
	cap  *telemetrytest.Capture
}

func newRealFx(t *testing.T, wrap func(signals.Bus) signals.Bus) *realFx {
	t.Helper()
	pool := dbtest.TempDB(t)
	mig, err := db.NewMigrator(pool, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewMigrator: %v", err)
	}
	// hello's foreign key needs core.person: migrate the core sets first, in db.md order.
	if _, err := mig.Migrate(ctxT(t), []db.Set{identity.Set, people.Set, {Name: "hello", Schema: "hello", Files: postgres.Migrations}}); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := pool.Exec(ctxT(t), "INSERT INTO core.person (id, created_at, updated_at) VALUES ($1, now(), now())", [16]byte(alice)); err != nil {
		t.Fatalf("inserting the person: %v", err)
	}
	c := telemetrytest.New(t)
	disp, err := signals.New(signals.Options{
		Logger:         c.Logger(),
		Tracer:         c.Tracer("signals"),
		Meter:          c.Meter("signals"),
		Clock:          clock.Real(),
		HandlerTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("signals.New: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()
		if err := disp.Close(ctx); err != nil {
			t.Errorf("dispatcher Close: %v", err)
		}
	})
	r := &realFx{pool: pool, disp: disp, cap: c}
	store, err := postgres.NewStore(pool)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	reg := module.NewRegistry()
	factory := func(_ context.Context, deps module.Deps) (module.Module, error) {
		m, err := hello.NewWithSeams(deps, hello.Seams{
			Store: store, Actor: actorIs(alice), Allow: hello.AllowAll{}, Greeting: wordIs("howdy"),
		})
		if err != nil {
			return nil, fmt.Errorf("NewWithSeams: %w", err)
		}
		r.mod = m
		return m, nil
	}
	if err := reg.Add(hello.Manifest, factory); err != nil {
		t.Fatalf("Add: %v", err)
	}
	plan, err := reg.Resolve(module.Env{Enabled: []string{"hello"}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	provision := func(_ context.Context, m module.Manifest) (module.Deps, error) {
		bus, err := disp.Scope(m.Name, m.Publishes, m.Subscribes)
		if err != nil {
			return module.Deps{}, fmt.Errorf("Scope: %w", err)
		}
		if wrap != nil {
			bus = wrap(bus)
		}
		return module.Deps{
			Logger: c.Logger(), Tracer: c.Tracer("module." + m.Name), Clock: clock.Real(),
			DB: pool, Signals: bus,
		}, nil
	}
	if _, err := plan.Build(ctxT(t), noSchema{}, provision); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := r.mod.Start(ctxT(t)); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()
		if err := r.mod.Stop(ctx); err != nil {
			t.Errorf("Stop: %v", err)
		}
	})
	return r
}

func TestGreet_EndToEnd_DeliversInSameTrace(t *testing.T) {
	r := newRealFx(t, nil)
	got, err := r.mod.Service().Greet(ctxT(t))
	if err != nil {
		t.Fatalf("Greet: %v", err)
	}
	rows, err := r.mod.ExportPerson(ctxT(t), alice)
	if err != nil {
		t.Fatalf("ExportPerson: %v", err)
	}
	if !jsonContains(rows.Data, got.ID.String()) {
		t.Errorf("the committed row %s is not in the export %s", got.ID, rows.Data)
	}
	if n := r.mod.Seen(); n != 1 {
		t.Fatalf("Seen = %d, want 1 (delivered after commit)", n)
	}
	var greetTrace string
	for _, s := range r.cap.Spans() {
		if s.Name == "hello.greet" {
			greetTrace = s.TraceID
		}
	}
	if greetTrace == "" {
		t.Fatal("no hello.greet span")
	}
	found := false
	for _, s := range r.cap.Spans() {
		if s.Name == "deliver:hello.greeted" && s.TraceID == greetTrace {
			found = true
		}
	}
	if !found {
		t.Errorf("no deliver:hello.greeted span in trace %s", greetTrace)
	}
	mine, err := r.mod.Service().Greeting(ctxT(t))
	if err != nil || mine.Mine != 1 || mine.Seen != 1 {
		t.Errorf("Greeting = %+v, %v; want mine=1 seen=1", mine, err)
	}
}

func TestGreet_PublishFails_RowRolledBackInRealDatabase(t *testing.T) {
	r := newRealFx(t, func(b signals.Bus) signals.Bus {
		return busFn{Bus: b, publish: func(context.Context, signals.Name, signals.Payload) error {
			return errs.E("test.bus", errs.Internal, errors.New("refused"))
		}}
	})
	if _, err := r.mod.Service().Greet(ctxT(t)); !errs.Is(err, errs.Internal) {
		t.Fatalf("Greet err = %v, want Internal", err)
	}
	g, err := r.mod.Service().Greeting(ctxT(t))
	if err != nil || g.Mine != 0 {
		t.Errorf("Greeting = %+v, %v; the insert must roll back with the failed Publish", g, err)
	}
	if n := r.mod.Seen(); n != 0 {
		t.Errorf("Seen = %d, want 0", n)
	}
}

func TestPersonData_RealDatabase_ExportAndDelete(t *testing.T) {
	r := newRealFx(t, nil)
	for range 3 {
		if _, err := r.mod.Service().Greet(ctxT(t)); err != nil {
			t.Fatalf("Greet: %v", err)
		}
	}
	del, err := r.mod.DeletePerson(ctxT(t), alice)
	if err != nil || del.Deleted != 3 {
		t.Fatalf("DeletePerson = %+v, %v; want 3 deleted", del, err)
	}
	del, err = r.mod.DeletePerson(ctxT(t), alice)
	if err != nil || del.Deleted != 0 {
		t.Errorf("second DeletePerson = %+v, %v; want 0", del, err)
	}
}

func jsonContains(b []byte, s string) bool { return strings.Contains(string(b), s) }
