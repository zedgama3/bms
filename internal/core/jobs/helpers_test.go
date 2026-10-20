package jobs_test

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit/audittest"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/authz/authztest"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// epoch is the fake clock's start: on the hour, so a 1h slot is exactly now.
var epoch = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

const (
	permRead   = authz.Permission(jobs.PermissionRead)
	permManage = authz.Permission(jobs.PermissionManage)
)

func newPerson() model.PersonID { return model.NewID[model.PersonID]() }

func ctxT(t testing.TB) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// ---- error assertions --------------------------------------------------------------------

func wantKind(t testing.TB, err error, kind errs.Kind) {
	t.Helper()
	if err == nil {
		t.Fatalf("got nil error, want kind %v", kind)
	}
	if !errs.Is(err, kind) {
		t.Fatalf("kind = %v (%v), want %v", errs.KindOf(err), err, kind)
	}
}

func wantNoErr(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func hasField(err error, field, code string) bool {
	for _, fe := range validate.Fields(err) {
		if (field == "" || fe.Field == field) && (code == "" || fe.Code == code) {
			return true
		}
	}
	return false
}

func describe(err error) []string {
	var out []string
	for _, fe := range validate.Fields(err) {
		out = append(out, fe.Field+"/"+fe.Code)
	}
	return out
}

// wantField checks Invalid carrying a field error for field and code ("" matches any).
func wantField(t testing.TB, err error, field, code string) {
	t.Helper()
	wantKind(t, err, errs.Invalid)
	if !hasField(err, field, code) {
		t.Fatalf("no field error %q/%q in %v", field, code, describe(err))
	}
}

// wantReadOnly checks Unavailable wrapping db.ErrReadOnly.
func wantReadOnly(t testing.TB, err error) {
	t.Helper()
	wantKind(t, err, errs.Unavailable)
	if !isReadOnly(err) {
		t.Fatalf("error %v does not wrap db.ErrReadOnly", err)
	}
}

// ---- actors ------------------------------------------------------------------------------

type actorKey struct{}

func actorFn(ctx context.Context) (authz.Actor, bool) {
	a, ok := ctx.Value(actorKey{}).(authz.Actor)
	return a, ok
}

func as(ctx context.Context, p model.PersonID) context.Context {
	return context.WithValue(ctx, actorKey{}, authz.Actor{Person: p})
}

// ---- fakes -------------------------------------------------------------------------------

// fakeRedeliverer records what the runner hands back and answers from fn.
type fakeRedeliverer struct {
	mu    sync.Mutex
	calls []signals.Delivery
	fn    func(signals.Delivery) error
}

func (f *fakeRedeliverer) Redeliver(_ context.Context, d signals.Delivery) error {
	f.mu.Lock()
	f.calls = append(f.calls, d)
	fn := f.fn
	f.mu.Unlock()
	if fn != nil {
		return fn(d)
	}
	return nil
}

// delivery is a failed delivery of an invented signal to subscriber "people".
func delivery(attempt int) signals.Delivery {
	return signals.Delivery{
		Signal: signals.Signal{
			ID:      model.NewID[signals.SignalID](),
			Name:    "identity.account-created",
			At:      epoch,
			Payload: signals.NewPayload(signals.Ref("person", newPerson())),
		},
		Subscriber: "people",
		Attempt:    attempt,
	}
}

// ---- the rig -----------------------------------------------------------------------------

type rig struct {
	t    *testing.T
	clk  *clock.Fake
	cap  *telemetrytest.Capture
	aud  *audittest.Fake
	az   *authztest.Fake
	red  *fakeRedeliverer
	opts jobs.Options
	run  *jobs.Runner
}

func newRigOptions(t *testing.T, d db.DB) (*rig, jobs.Options) {
	t.Helper()
	c := telemetrytest.New(t)
	r := &rig{
		t: t, clk: clock.NewFake(epoch), cap: c,
		aud: audittest.NewFake("jobs"), az: authztest.NewFake(), red: &fakeRedeliverer{},
	}
	r.az.SetActor(actorFn)
	o := jobs.Options{
		DB: d, Clock: r.clk, Logger: c.Logger(), Tracer: c.Tracer("jobs"), Meter: c.Meter("jobs"),
		Audit: r.aud, Authz: r.az, Actor: actorFn, Redeliverer: r.red,
		Instance: "test-a",
	}
	return r, o
}

// newRig builds the runner over d; Close runs at cleanup.
func newRig(t *testing.T, d db.DB, mods ...func(*jobs.Options)) *rig {
	t.Helper()
	r, o := newRigOptions(t, d)
	for _, m := range mods {
		m(&o)
	}
	run, err := jobs.New(o)
	if err != nil {
		t.Fatalf("jobs.New: %v", err)
	}
	r.run, r.opts = run, o
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := run.Close(ctx); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return r
}

func (r *rig) scoped(owner string) jobs.Registrar {
	r.t.Helper()
	s, err := r.run.Scoped(owner)
	if err != nil {
		r.t.Fatalf("Scoped(%q): %v", owner, err)
	}
	return s
}

// register declares d under its own owner.
func (r *rig) register(d jobs.Decl) jobs.Registrar {
	r.t.Helper()
	s := r.scoped(d.Name.Owner())
	wantNoErr(r.t, s.Register(d))
	return s
}

func nop(context.Context, jobs.Job) error { return nil }

// holder returns a ctx whose actor holds perms.
func (r *rig) holder(perms ...authz.Permission) context.Context {
	p := newPerson()
	for _, perm := range perms {
		r.az.Grant(p, perm)
	}
	return as(ctxT(r.t), p)
}

func (r *rig) advance(d time.Duration) {
	r.t.Helper()
	wantNoErr(r.t, r.clk.Advance(d))
}

// eventually spins (yielding, no sleeps) until cond holds or ten real seconds pass.
func eventually(t testing.TB, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		runtime.Gosched()
	}
}

// startRun starts Run on its own goroutine and waits until Register is frozen. The returned
// channel receives Run's result.
func (r *rig) startRun(ctx context.Context) <-chan error {
	r.t.Helper()
	done := make(chan error, 1)
	go func() { done <- r.run.Run(ctx) }()
	probe, err := r.run.Scoped("probe")
	wantNoErr(r.t, err)
	i := 0
	eventually(r.t, "Run to freeze Register", func() bool {
		i++
		err := probe.Register(jobs.Decl{Name: jobs.Name(fmt.Sprintf("probe.n%d", i)), Handler: nop})
		return err != nil
	})
	return done
}

// counter is a handler call counter.
type counter struct {
	mu   sync.Mutex
	jobs []jobs.Job
}

func (c *counter) handler(err error) jobs.Handler {
	return func(_ context.Context, j jobs.Job) error {
		c.mu.Lock()
		c.jobs = append(c.jobs, j)
		c.mu.Unlock()
		return err
	}
}

func (c *counter) n() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.jobs)
}
