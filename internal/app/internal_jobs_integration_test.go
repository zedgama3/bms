//go:build integration

package app

import (
	"context"
	"errors"
	"net/url"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// itDBPool is itDB that also returns a pool on the throw-away database, for reading rows.
func itDBPool(t *testing.T) (*url.URL, *db.Pool) {
	t.Helper()
	pool := dbtest.TempDB(t)
	var name string
	if err := pool.QueryRow(t.Context(), "SELECT current_database()").Scan(&name); err != nil {
		t.Fatalf("current_database: %v", err)
	}
	u, err := url.Parse(os.Getenv("BMS_TEST_DB_DSN"))
	if err != nil {
		t.Fatal("BMS_TEST_DB_DSN is not a URL")
	}
	u.Path = "/" + name
	return u, pool
}

func itJobRows(ctx context.Context, t *testing.T, pool *db.Pool, name string) (total, done int) {
	t.Helper()
	err := pool.QueryRow(ctx,
		`SELECT count(*), count(*) FILTER (WHERE status = 'done') FROM core.job WHERE name = $1`, name).Scan(&total, &done)
	if err != nil {
		t.Fatalf("count %s rows: %v", name, err)
	}
	return total, done
}

// jobs.md test plan, end to end: serve on a temporary database runs jobs.purge and audit.purge
// once at start (the first slot of each hourly job), and not again within the hour.
func TestServe_StartsJobRunner_RunsPurgesOnce(t *testing.T) {
	u, pool := itDBPool(t)
	env, addr := itEnv(t, u.String())
	s := itServe(t, env, addr)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	for _, name := range []string{"jobs.purge", "audit.purge"} {
		for {
			if _, done := itJobRows(ctx, t, pool, name); done == 1 {
				break
			}
			if err := clock.Wait(ctx, clock.Real(), 50*time.Millisecond); err != nil {
				total, done := itJobRows(t.Context(), t, pool, name)
				t.Fatalf("%s never ran: %d rows, %d done: %v", name, total, done, err)
			}
		}
	}
	// Let any further tick pass; an hourly job has one slot in this hour.
	if err := clock.Wait(ctx, clock.Real(), 1500*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"jobs.purge", "audit.purge"} {
		if total, done := itJobRows(ctx, t, pool, name); total != 1 || done != 1 {
			t.Errorf("%s: %d rows, %d done, want exactly one run", name, total, done)
		}
	}
	if r := s.stop(t); r.code != ExitOK {
		t.Fatalf("status = %d, want ExitOK\n%s", r.code, r.errOut)
	}
}

// jobs.md "Lifecycle": bms check lists each registered job with its owner and schedule, and
// reads no job rows.
func TestCheck_ListsRegisteredJobs(t *testing.T) {
	env, _ := itEnv(t, itDB(t).String())
	if r := itMain(t.Context(), env, "migrate"); r.code != ExitOK {
		t.Fatalf("migrate: %d\n%s\n%s", r.code, r.out, r.errOut)
	}
	r := itMain(t.Context(), env, "check")
	if r.code != ExitOK {
		t.Fatalf("check = %d\n%s\n%s", r.code, r.out, r.errOut)
	}
	_, after, ok := strings.Cut(r.out, "jobs\n")
	if !ok {
		t.Fatalf("no jobs heading in:\n%s", r.out)
	}
	for _, want := range []string{"jobs.purge  owner jobs  every 1h0m0s", "audit.purge  owner audit  every 1h0m0s", "signals.redeliver"} {
		if !strings.Contains(after, want) {
			t.Errorf("jobs section lacks %q:\n%s", want, after)
		}
	}
}

// sigModule is a do-nothing module whose Start is the test's: the publisher keeps its Bus and database
// for the test, the subscriber subscribes with the test's handler.
type sigModule struct {
	start func(context.Context) error
}

func (m sigModule) Start(ctx context.Context) error { return m.start(ctx) }
func (sigModule) Stop(context.Context) error        { return nil }

func (sigModule) ExportPerson(context.Context, model.PersonID) (module.Export, error) {
	return module.Export{}, nil
}

func (sigModule) DeletePerson(context.Context, model.PersonID) (module.Deletion, error) {
	return module.Deletion{}, nil
}

// signals.md and boundaries.md step 11: through app's real wiring (the dispatcher takes
// jobs.NewDeferrer directly), a delivery that fails Unavailable once leaves exactly one
// signals.redeliver row, and one run of the job runner redelivers it: the subscriber's second
// call succeeds and the row ends done.
func TestServe_FailedDelivery_IsDeferredAndRedelivered(t *testing.T) {
	const sigName = signals.Name("zqpub.thing-happened")
	var (
		calls atomic.Int64
		bus   signals.Bus
		dbc   db.DB
		jobsR *jobs.Runner
		ready = make(chan struct{})
	)
	reg := module.NewRegistry()
	wantAdd := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("registry Add: %v", err)
		}
	}
	wantAdd(reg.Add(module.Manifest{Name: "zqpub", Publishes: []signals.Name{sigName}},
		func(_ context.Context, d module.Deps) (module.Module, error) {
			bus, dbc = d.Signals, d.DB
			return sigModule{start: func(context.Context) error { close(ready); return nil }}, nil
		}))
	wantAdd(reg.Add(module.Manifest{Name: "zqsub", Subscribes: []signals.Name{sigName}},
		func(_ context.Context, d module.Deps) (module.Module, error) {
			return sigModule{start: func(context.Context) error {
				return d.Signals.Subscribe(sigName, func(context.Context, signals.Signal) error { //nolint:wrapcheck // test
					if calls.Add(1) == 1 {
						return errs.E("test.subscriber", errs.Unavailable, errors.New("down"))
					}
					return nil
				})
			}}, nil
		}))

	// The real core parts, with the jobs part's link also keeping the runner for the test.
	parts := slices.Clone(coreParts)
	for i := range parts {
		if parts[i].name != jobsPart.name {
			continue
		}
		orig := parts[i].link
		parts[i].link = func(ctx context.Context, a *assembly) error {
			if err := orig(ctx, a); err != nil {
				return err
			}
			jobsR = a.jobs
			return nil
		}
	}

	u, pool := itDBPool(t)
	env, _ := itEnv(t, u.String())
	if r := itMain(t.Context(), env, "migrate"); r.code != ExitOK {
		t.Fatalf("migrate: %d\n%s\n%s", r.code, r.out, r.errOut)
	}
	clk := clock.NewFake(time.Now())
	var out, errOut lockedBuf
	p := &process{
		args: []string{"serve"}, environ: env, lookup: lookupIn(env), stdout: &out, stderr: &errOut,
		clock: clk, registry: reg, parts: parts, version: "dev",
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan int, 1)
	go func() { done <- runServe(ctx, p) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			t.Error("serve did not stop")
		}
	})

	wctx, wcancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer wcancel()
	select {
	case <-ready:
	case code := <-done:
		t.Fatalf("serve exited with %d before it started\n%s\n%s", code, &out, &errOut)
	case <-wctx.Done():
		t.Fatalf("serve never started\n%s", &errOut)
	}
	if bus == nil || dbc == nil || jobsR == nil {
		t.Fatalf("wiring not captured: bus=%v db=%v jobs=%v", bus != nil, dbc != nil, jobsR != nil)
	}

	pub := bus
	err := dbc.Transact(wctx, db.Write, func(ctx context.Context) error {
		return pub.Publish(ctx, sigName, signals.NewPayload(signals.Ref("person", model.NewID[model.PersonID]())))
	})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("subscriber ran %d times after publish, want 1", got)
	}
	total, done0 := itJobRows(wctx, t, pool, "signals.redeliver")
	if total != 1 || done0 != 0 {
		t.Fatalf("signals.redeliver rows = %d (%d done), want exactly one queued", total, done0)
	}

	if err := clk.Advance(2 * time.Hour); err != nil {
		t.Fatalf("advance: %v", err)
	}
	if _, err := jobsR.RunOnce(wctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	// The runner's own loop may have taken the row at the same moment; wait for it to end.
	for {
		if _, d := itJobRows(wctx, t, pool, "signals.redeliver"); d == 1 {
			break
		}
		if wctx.Err() != nil {
			t.Fatal("the redelivery row never ended done")
		}
		runtime.Gosched()
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("subscriber ran %d times, want 2 (the redelivery succeeded)", got)
	}
	if total, _ := itJobRows(wctx, t, pool, "signals.redeliver"); total != 1 {
		t.Errorf("signals.redeliver rows = %d, want 1", total)
	}
}
