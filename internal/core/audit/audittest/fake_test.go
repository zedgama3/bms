package audittest_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/audit/audittest"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

var alice = model.PersonID{0x21, 0x5c, 0x90, 0x14, 0xa3, 0x77, 0x4e, 0x02, 0xb1, 0x6d, 0x48, 0xee, 0x0f, 0x39, 0xc5, 0x86}

func login() audit.Entry { return audit.Entry{Action: "identity.login", Outcome: audit.Succeeded} }

// real builds the real recorder for an area over a writable dbtest.Fake.
func realRecorder(t *testing.T, area string) audit.Recorder {
	t.Helper()
	c := telemetrytest.New(t)
	l, err := audit.New(audit.Options{
		DB:       dbtest.NewFake(),
		Clock:    clock.NewFake(clock.Real().Now()),
		Logger:   c.Logger(),
		Tracer:   c.Tracer("audit"),
		Instance: "test-copy",
		DataDir:  t.TempDir(),
		Actor:    func(context.Context) (audit.Actor, bool) { return audit.Actor{}, false },
	})
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	t.Cleanup(func() { _ = l.Close(context.Background()) }) //nolint:errcheck // idempotent cleanup
	r, err := l.Scoped(area)
	if err != nil {
		t.Fatalf("Scoped: %v", err)
	}
	return r
}

func TestNewFake_BadArea_Panics(t *testing.T) {
	for _, area := range []string{"", "a", "Identity", "has-hyphen", strings.Repeat("a", 33)} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("NewFake(%q) did not panic", area)
				}
			}()
			audittest.NewFake(area)
		}()
	}
	audittest.NewFake("identity") // must not panic
}

func TestFake_MatchesRealValidation(t *testing.T) {
	long := make([]audit.Entry, audit.MaxBatch+1)
	for i := range long {
		long[i] = login()
	}
	with := func(mod func(*audit.Entry)) audit.Entry { e := login(); mod(&e); return e }
	tests := []struct {
		name    string
		entries []audit.Entry
		kind    errs.Kind // errs.Internal here means "must succeed"
	}{
		{"valid", []audit.Entry{login()}, errs.Internal},
		{"valid batch", []audit.Entry{login(), login()}, errs.Internal},
		{"valid full", []audit.Entry{{
			Action: "identity.account.read", Outcome: audit.Succeeded, Subject: alice,
			Record: audit.RefOf("identity.account", model.AccountID{1}),
			Fields: []string{"email"}, Level: model.SensitivityPersonal, Reason: "needed",
		}}, errs.Internal},
		{"no entries", nil, errs.Invalid},
		{"over MaxBatch", long, errs.Invalid},
		{"bad action", []audit.Entry{with(func(e *audit.Entry) { e.Action = "Identity.Login" })}, errs.Invalid},
		{"foreign area", []audit.Entry{with(func(e *audit.Entry) { e.Action = "people.person.read" })}, errs.Invalid},
		{"foreign area second", []audit.Entry{login(), with(func(e *audit.Entry) { e.Action = "people.person.read" })}, errs.Invalid},
		{"zero outcome", []audit.Entry{with(func(e *audit.Entry) { e.Outcome = 0 })}, errs.Invalid},
		{"fields without level", []audit.Entry{with(func(e *audit.Entry) { e.Fields = []string{"email"} })}, errs.Invalid},
		{"level without fields", []audit.Entry{with(func(e *audit.Entry) { e.Level = model.SensitivityPersonal })}, errs.Invalid},
		{"duplicate field", []audit.Entry{with(func(e *audit.Entry) {
			e.Fields, e.Level = []string{"email", "email"}, model.SensitivityPersonal
		})}, errs.Invalid},
		{"long reason", []audit.Entry{with(func(e *audit.Entry) { e.Reason = strings.Repeat("a", audit.MaxReasonBytes+1) })}, errs.Invalid},
		{"control in reason", []audit.Entry{with(func(e *audit.Entry) { e.Reason = "a\x00" })}, errs.Invalid},
		{"person actor without person", []audit.Entry{with(func(e *audit.Entry) { e.Actor = audit.Actor{Kind: audit.ActorPerson} })}, errs.Invalid},
		{"superuser operator", []audit.Entry{with(func(e *audit.Entry) { e.Actor = audit.Actor{Kind: audit.ActorOperator, Superuser: true} })}, errs.Invalid},
		{"caller titles", []audit.Entry{with(func(e *audit.Entry) {
			e.Actor = audit.Actor{Kind: audit.ActorPerson, Person: alice, Titles: []string{"x"}}
		})}, errs.Invalid},
		{"half ref", []audit.Entry{with(func(e *audit.Entry) { e.Record = audit.Ref{Kind: "identity.account"} })}, errs.Invalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			realErr := realRecorder(t, "identity").Record(t.Context(), tc.entries...)
			fake := audittest.NewFake("identity")
			fakeErr := fake.Record(t.Context(), tc.entries...)
			if tc.kind == errs.Internal {
				if realErr != nil || fakeErr != nil {
					t.Fatalf("real err %v, fake err %v; both must accept", errs.KindOf(realErr), errs.KindOf(fakeErr))
				}
				if len(fake.Recorded()) != len(tc.entries) {
					t.Errorf("Recorded = %d, want %d", len(fake.Recorded()), len(tc.entries))
				}
				return
			}
			if !errs.Is(realErr, tc.kind) || !errs.Is(fakeErr, tc.kind) {
				t.Fatalf("real kind %v, fake kind %v, want both %v", errs.KindOf(realErr), errs.KindOf(fakeErr), tc.kind)
			}
			if n := len(fake.Recorded()); n != 0 {
				t.Errorf("a rejected batch kept %d entries", n)
			}
		})
	}
}

func TestFake_RecordedIsACopyInCallOrder(t *testing.T) {
	f := audittest.NewFake("identity")
	a := login()
	b := login()
	b.Action = "identity.logout"
	b.Fields, b.Level = []string{"email"}, model.SensitivityPersonal
	if err := f.Record(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	if err := f.Record(t.Context(), b); err != nil {
		t.Fatal(err)
	}
	got := f.Recorded()
	if len(got) != 2 || got[0].Action != "identity.login" || got[1].Action != "identity.logout" {
		t.Fatalf("Recorded = %+v", got)
	}
	got[0].Action = "identity.tampered"
	got[1].Fields[0] = "tampered"
	again := f.Recorded()
	if again[0].Action != "identity.login" || again[1].Fields[0] != "email" {
		t.Error("Recorded shares storage with the caller")
	}
	// Mutating an entry after Record must not change what was kept.
	b.Fields[0] = "changed"
	if f.Recorded()[1].Fields[0] != "email" {
		t.Error("Fake kept a reference to the caller's slice")
	}
}

func TestFake_Fail_FailsNextCallsUntilCleared(t *testing.T) {
	f := audittest.NewFake("identity")
	boom := errs.E("test", errs.Unavailable, errors.New("disk full"))
	f.Fail(boom)
	for range 2 {
		err := f.Record(t.Context(), login())
		if !errors.Is(err, boom) || !errs.Is(err, errs.Unavailable) {
			t.Fatalf("Record with Fail set: kind %v", errs.KindOf(err))
		}
	}
	if len(f.Recorded()) != 0 {
		t.Error("a failed Record kept its entries")
	}
	f.Fail(nil)
	if err := f.Record(t.Context(), login()); err != nil {
		t.Fatalf("after Fail(nil): kind %v", errs.KindOf(err))
	}
	if len(f.Recorded()) != 1 {
		t.Error("entry not kept after the failure was cleared")
	}
}

func TestFake_Fail_DoesNotMaskInvalid(t *testing.T) {
	f := audittest.NewFake("identity")
	f.Fail(errs.E("test", errs.Unavailable, errors.New("x")))
	bad := login()
	bad.Outcome = 0
	if err := f.Record(t.Context(), bad); err == nil {
		t.Fatal("no error")
	}
}

func TestFake_ConcurrentUse(t *testing.T) {
	f := audittest.NewFake("identity")
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			if err := f.Record(t.Context(), login()); err != nil {
				t.Errorf("Record: %v", err)
			}
			_ = f.Recorded()
			f.Fail(nil)
		})
	}
	wg.Wait()
	if n := len(f.Recorded()); n != 32 {
		t.Errorf("Recorded = %d, want 32", n)
	}
}

var _ audit.Recorder = (*audittest.Fake)(nil)
