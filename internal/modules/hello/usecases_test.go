package hello_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/signals/signalstest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/hello"
	"github.com/zedgama3/bms/internal/modules/hello/greeting"
)

func seedRows(t *testing.T, st *hello.FakeStore, person model.PersonID, n int) {
	t.Helper()
	for i := range n {
		g := greeting.Greeting{
			ID:        model.NewID[greeting.ID](),
			Person:    person,
			Text:      "hi",
			CreatedAt: epoch.Add(time.Duration(i) * time.Second),
		}
		if err := st.Insert(context.Background(), g); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
}

// ---- Greeting (GET /greeting) ----

func TestGreeting_Allowed_ReturnsOwnCountAndSeen(t *testing.T) {
	f := newFx(t, cfg{})
	f.start(t)
	seedRows(t, f.store, alice, 2)
	seedRows(t, f.store, bob, 5)
	f.deliver(t, alice)
	f.deliver(t, bob)
	f.deliver(t, alice)

	got, err := f.mod.Service().Greeting(ctxT(t))
	if err != nil {
		t.Fatalf("Greeting: %v", err)
	}
	if got.Greeting != "howdy" || got.Mine != 2 || got.Seen != 3 {
		t.Errorf("Greeting = %+v, want {howdy 2 3}", got)
	}
	sp := f.spansNamed("hello.greeting")
	if len(sp) != 1 || sp[0].Attrs["mine"] != "2" || sp[0].Attrs["seen"] != "3" {
		t.Errorf("spans = %+v, want one hello.greeting with mine=2 seen=3", sp)
	}
}

func TestGreeting_OtherActor_SeesOnlyOwnRows(t *testing.T) {
	f := newFx(t, cfg{seams: hello.Seams{Actor: actorIs(bob)}})
	seedRows(t, f.store, alice, 4)
	seedRows(t, f.store, bob, 1)
	got, err := f.mod.Service().Greeting(ctxT(t))
	if err != nil {
		t.Fatalf("Greeting: %v", err)
	}
	if got.Mine != 1 {
		t.Errorf("Mine = %d, want 1 (own rows only)", got.Mine)
	}
}

func TestGreeting_NoRows_ReturnsZero(t *testing.T) {
	f := newFx(t, cfg{})
	got, err := f.mod.Service().Greeting(ctxT(t))
	if err != nil {
		t.Fatalf("Greeting: %v", err)
	}
	if got.Mine != 0 || got.Seen != 0 {
		t.Errorf("Greeting = %+v, want zero counts", got)
	}
}

// F10
func TestGreeting_NoActor_ReturnsUnauthenticated(t *testing.T) {
	cases := []struct {
		name  string
		actor func(context.Context) (model.PersonID, bool)
	}{
		{"no actor", noActor},
		{"zero person reported as present", func(context.Context) (model.PersonID, bool) { return model.PersonID{}, true }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFx(t, cfg{seams: hello.Seams{Actor: tc.actor}})
			_, err := f.mod.Service().Greeting(ctxT(t))
			if !errs.Is(err, errs.Unauthenticated) {
				t.Fatalf("err = %v, want Unauthenticated", err)
			}
			if sp := f.spansNamed("hello.greeting"); len(sp) != 0 {
				t.Errorf("a span was opened before the actor check: %+v", sp)
			}
		})
	}
}

// F11
func TestGreeting_Denied_ReturnsPermission(t *testing.T) {
	f := newFx(t, cfg{seams: hello.Seams{Allow: hello.DenyAll{}}})
	_, err := f.mod.Service().Greeting(ctxT(t))
	if !errs.Is(err, errs.Permission) {
		t.Fatalf("err = %v, want Permission", err)
	}
	if sp := f.spansNamed("hello.greeting"); len(sp) != 0 {
		t.Errorf("a span was opened before the permission check: %+v", sp)
	}
}

func TestGreeting_ActorCheckedBeforePermission(t *testing.T) {
	c := grants() // grants nothing
	c.deny = errs.E("test", errs.Permission, errors.New("no"))
	f := newFx(t, cfg{seams: hello.Seams{Actor: noActor, Allow: c}})
	if _, err := f.mod.Service().Greeting(ctxT(t)); !errs.Is(err, errs.Unauthenticated) {
		t.Fatalf("err = %v, want Unauthenticated (actor first)", err)
	}
	if asked := c.askedFor(); len(asked) != 0 {
		t.Errorf("permission asked %v without an actor", asked)
	}
}

// F11: the M0 wiring (no actor, deny, declared default) fails closed.
func TestGreeting_M0Wiring_AlwaysDenies(t *testing.T) {
	f := newFx(t, cfg{seams: hello.Seams{Actor: noActor, Allow: hello.DenyAll{}}, declaredDefault: true})
	if _, err := f.mod.Service().Greeting(ctxT(t)); !errs.Is(err, errs.Unauthenticated) {
		t.Errorf("Greeting with no actor = %v, want Unauthenticated", err)
	}
	if _, err := f.mod.Service().Greet(ctxT(t)); !errs.Is(err, errs.Unauthenticated) {
		t.Errorf("Greet with no actor = %v, want Unauthenticated", err)
	}
	// with an actor present, the deny-all checker refuses both permissions and anything else
	d := hello.DenyAll{}
	for _, p := range []authz.Permission{hello.PermissionGreetingsRead, hello.PermissionGreetingsWrite, "other.thing.read"} {
		if err := d.Allow(ctxT(t), p); !errs.Is(err, errs.Permission) {
			t.Errorf("DenyAll.Allow(%s) = %v, want Permission", p, err)
		}
	}
}

func TestGreeting_ChecksReadPermissionOnly(t *testing.T) {
	c := grants(hello.PermissionGreetingsRead)
	f := newFx(t, cfg{seams: hello.Seams{Allow: c}})
	if _, err := f.mod.Service().Greeting(ctxT(t)); err != nil {
		t.Fatalf("Greeting: %v", err)
	}
	asked := c.askedFor()
	if len(asked) != 1 || asked[0] != hello.PermissionGreetingsRead {
		t.Errorf("asked %v, want [%s]", asked, hello.PermissionGreetingsRead)
	}
}

func TestPermissions_ReadAndWriteAreIndependent(t *testing.T) {
	denied := errs.E("test", errs.Permission, errors.New("no"))
	cases := []struct {
		name      string
		grant     authz.Permission
		wantRead  bool
		wantWrite bool
	}{
		{"read only", hello.PermissionGreetingsRead, true, false},
		{"write only", hello.PermissionGreetingsWrite, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := grants(tc.grant)
			c.deny = denied
			f := newFx(t, cfg{seams: hello.Seams{Allow: c}})
			_, rerr := f.mod.Service().Greeting(ctxT(t))
			_, werr := f.mod.Service().Greet(ctxT(t))
			if tc.wantRead != (rerr == nil) || (!tc.wantRead && !errs.Is(rerr, errs.Permission)) {
				t.Errorf("Greeting err = %v, wantOK = %v", rerr, tc.wantRead)
			}
			if tc.wantWrite != (werr == nil) || (!tc.wantWrite && !errs.Is(werr, errs.Permission)) {
				t.Errorf("Greet err = %v, wantOK = %v", werr, tc.wantWrite)
			}
			if !tc.wantWrite && (len(f.store.Rows()) != 0 || len(f.sig.Published()) != 0) {
				t.Errorf("a refused Greet left rows or signals behind")
			}
		})
	}
}

// F12
func TestGreeting_StoreFails_ReturnsKind(t *testing.T) {
	for _, kind := range []errs.Kind{errs.Unavailable, errs.Timeout, errs.Internal} {
		t.Run(kind.String(), func(t *testing.T) {
			f := newFx(t, cfg{})
			f.store.Fail("CountByPerson", errs.E("test.store", kind, errors.New("boom")))
			if _, err := f.mod.Service().Greeting(ctxT(t)); !errs.Is(err, kind) {
				t.Fatalf("err = %v, want %v", err, kind)
			}
		})
	}
}

func TestGreeting_SourceFails_ReturnsKind(t *testing.T) {
	f := newFx(t, cfg{seams: hello.Seams{Greeting: wordFails(errs.E("test", errs.Unavailable, errors.New("down")))}})
	if _, err := f.mod.Service().Greeting(ctxT(t)); !errs.Is(err, errs.Unavailable) {
		t.Fatalf("err = %v, want Unavailable", err)
	}
}

func TestGreeting_ReadOnlyDatabase_StillWorks(t *testing.T) {
	f := newFx(t, cfg{readOnly: true})
	seedRows(t, f.store, alice, 1)
	got, err := f.mod.Service().Greeting(ctxT(t))
	if err != nil {
		t.Fatalf("Greeting on a read-only copy: %v", err)
	}
	if got.Mine != 1 {
		t.Errorf("Mine = %d, want 1", got.Mine)
	}
}

// ---- Greet (POST /greetings) ----

func TestGreet_Allowed_SavesRowForActorAndPublishes(t *testing.T) {
	f := newFx(t, cfg{})
	f.start(t)
	got, err := f.mod.Service().Greet(ctxT(t))
	if err != nil {
		t.Fatalf("Greet: %v", err)
	}
	if got.ID.IsZero() || got.Person != alice || got.Text != "howdy" || !got.CreatedAt.Equal(epoch) {
		t.Errorf("Greet = %+v, want a row for alice with the source's word at the fake clock's time", got)
	}
	rows := f.store.Rows()
	if len(rows) != 1 || rows[0].ID != got.ID {
		t.Fatalf("stored rows = %+v, want the returned one", rows)
	}
	if err := rows[0].Validate(); err != nil {
		t.Errorf("stored row is invalid: %v", err)
	}
	pub := f.sig.Published()
	if len(pub) != 1 || pub[0].Name != hello.SignalGreeted {
		t.Fatalf("Published = %+v, want one %s", pub, hello.SignalGreeted)
	}
	who, err := signals.Get[model.PersonID](pub[0].Payload, hello.PayloadPerson)
	if err != nil || who != alice {
		t.Errorf("payload person = %v, %v; want alice", who, err)
	}
	if len(pub[0].Payload) != 1 {
		t.Errorf("payload has %d keys, want only %q (an ID, no word, no time)", len(pub[0].Payload), hello.PayloadPerson)
	}
	if sp := f.spansNamed("hello.greet"); len(sp) != 1 {
		t.Errorf("hello.greet spans = %d, want 1", len(sp))
	}
}

func TestGreet_EachCallSavesANewRow(t *testing.T) {
	f := newFx(t, cfg{})
	a, err := f.mod.Service().Greet(ctxT(t))
	if err != nil {
		t.Fatalf("Greet: %v", err)
	}
	b, err := f.mod.Service().Greet(ctxT(t))
	if err != nil {
		t.Fatalf("Greet: %v", err)
	}
	if a.ID == b.ID || len(f.store.Rows()) != 2 || len(f.sig.Published()) != 2 {
		t.Errorf("two Greet calls must save two rows and publish two signals")
	}
}

func TestGreet_PublishesInsideTransaction(t *testing.T) {
	f := newFx(t, cfg{wrapStore: func(d *dbtest.Fake, base *hello.FakeStore) interface {
		Insert(ctx context.Context, g greeting.Greeting) error
		CountByPerson(ctx context.Context, person model.PersonID) (int, error)
		ListByPerson(ctx context.Context, person model.PersonID) ([]greeting.Greeting, error)
		DeleteByPerson(ctx context.Context, person model.PersonID) (int, error)
	} {
		return &txStore{FakeStore: base, db: d}
	}})
	if _, err := f.mod.Service().Greet(ctxT(t)); err != nil {
		t.Fatalf("Greet: %v", err)
	}
	calls := f.db.Calls()
	if len(calls) != 1 || !calls[0].InTx {
		t.Fatalf("Calls = %+v, want one statement inside a transaction", calls)
	}
	if n := len(f.sig.Published()); n != 1 {
		t.Errorf("Published = %d, want 1", n)
	}
}

func TestGreet_SpanUnderParent(t *testing.T) {
	f := newFx(t, cfg{})
	ctx, parent := f.cap.Tracer("test").Start(ctxT(t), "test.parent")
	if _, err := f.mod.Service().Greet(ctx); err != nil {
		t.Fatalf("Greet: %v", err)
	}
	parent.End()
	var par, kid = f.cap.Spans(), f.spansNamed("hello.greet")
	if len(kid) != 1 {
		t.Fatalf("hello.greet spans = %d, want 1", len(kid))
	}
	if kid[0].ParentID == "" {
		t.Errorf("hello.greet is a root span; want a child of the caller's span")
	}
	for _, s := range par {
		if s.Name == "test.parent" && s.TraceID != kid[0].TraceID {
			t.Errorf("hello.greet is in trace %q, parent in %q", kid[0].TraceID, s.TraceID)
		}
	}
}

// F13
func TestGreet_NoActor_ReturnsUnauthenticated(t *testing.T) {
	f := newFx(t, cfg{seams: hello.Seams{Actor: noActor}})
	if _, err := f.mod.Service().Greet(ctxT(t)); !errs.Is(err, errs.Unauthenticated) {
		t.Fatalf("err = %v, want Unauthenticated", err)
	}
	if len(f.store.Rows()) != 0 || len(f.sig.Published()) != 0 {
		t.Errorf("rows or signals left behind")
	}
}

// F14
func TestGreet_Denied_ReturnsPermission(t *testing.T) {
	f := newFx(t, cfg{seams: hello.Seams{Allow: hello.DenyAll{}}})
	if _, err := f.mod.Service().Greet(ctxT(t)); !errs.Is(err, errs.Permission) {
		t.Fatalf("err = %v, want Permission", err)
	}
	if len(f.store.Rows()) != 0 || len(f.sig.Published()) != 0 || len(f.db.Calls()) != 0 {
		t.Errorf("a denied Greet touched the store, the database or the bus")
	}
	if sp := f.spansNamed("hello.greet"); len(sp) != 0 {
		t.Errorf("a span was opened before the permission check")
	}
}

func TestGreet_ChecksWritePermissionOnly(t *testing.T) {
	c := grants(hello.PermissionGreetingsWrite)
	f := newFx(t, cfg{seams: hello.Seams{Allow: c}})
	if _, err := f.mod.Service().Greet(ctxT(t)); err != nil {
		t.Fatalf("Greet: %v", err)
	}
	asked := c.askedFor()
	if len(asked) != 1 || asked[0] != hello.PermissionGreetingsWrite {
		t.Errorf("asked %v, want [%s]", asked, hello.PermissionGreetingsWrite)
	}
}

// F15
func TestGreet_ReadOnly_ReturnsUnavailableAndPublishesNothing(t *testing.T) {
	f := newFx(t, cfg{readOnly: true})
	f.start(t)
	_, err := f.mod.Service().Greet(ctxT(t))
	if !errs.Is(err, errs.Unavailable) || !errors.Is(err, db.ErrReadOnly) {
		t.Fatalf("err = %v, want Unavailable wrapping db.ErrReadOnly", err)
	}
	if len(f.sig.Published()) != 0 || len(f.store.Rows()) != 0 {
		t.Errorf("a read-only Greet published or stored something")
	}
}

// F16
func TestGreet_PublishFails_RollsBackAndReturnsKind(t *testing.T) {
	for _, kind := range []errs.Kind{errs.Invalid, errs.Unavailable, errs.Internal} {
		t.Run(kind.String(), func(t *testing.T) {
			f := newFx(t, cfg{wrapBus: func(b *signalstest.Fake) signals.Bus {
				return busFn{Bus: b, publish: func(context.Context, signals.Name, signals.Payload) error {
					return errs.E("test.bus", kind, errors.New("refused"))
				}}
			}})
			if _, err := f.mod.Service().Greet(ctxT(t)); !errs.Is(err, kind) {
				t.Fatalf("err = %v, want %v", err, kind)
			}
			if n := len(f.sig.Published()); n != 0 {
				t.Errorf("Published = %d after a refused Publish, want 0", n)
			}
		})
	}
}

// F17
func TestGreet_InsertFails_ReturnsKindAndPublishesNothing(t *testing.T) {
	for _, kind := range []errs.Kind{errs.Unavailable, errs.Invalid, errs.Conflict, errs.Timeout} {
		t.Run(kind.String(), func(t *testing.T) {
			f := newFx(t, cfg{})
			f.store.Fail("Insert", errs.E("test.store", kind, errors.New("boom")))
			if _, err := f.mod.Service().Greet(ctxT(t)); !errs.Is(err, kind) {
				t.Fatalf("err = %v, want %v", err, kind)
			}
			if n := len(f.sig.Published()); n != 0 {
				t.Errorf("Published = %d after a failed Insert, want 0", n)
			}
		})
	}
}

func TestGreet_SourceWordOutOfRange_ReturnsInvalidAndSavesNothing(t *testing.T) {
	long := make([]byte, hello.MaxGreetingBytes+1)
	for i := range long {
		long[i] = 'a'
	}
	cases := []struct {
		name string
		word string
	}{
		{"empty", ""},
		{"too long", string(long)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFx(t, cfg{seams: hello.Seams{Greeting: wordIs(tc.word)}})
			if _, err := f.mod.Service().Greet(ctxT(t)); !errs.Is(err, errs.Invalid) {
				t.Fatalf("err = %v, want Invalid", err)
			}
			if len(f.store.Rows()) != 0 || len(f.sig.Published()) != 0 {
				t.Errorf("rows or signals left behind")
			}
		})
	}
}

func TestGreet_MaxLengthWord_Succeeds(t *testing.T) {
	word := make([]byte, hello.MaxGreetingBytes)
	for i := range word {
		word[i] = 'b'
	}
	f := newFx(t, cfg{seams: hello.Seams{Greeting: wordIs(string(word))}})
	if _, err := f.mod.Service().Greet(ctxT(t)); err != nil {
		t.Fatalf("Greet with a %d-byte word: %v", len(word), err)
	}
}

func TestGreet_SourceFails_ReturnsKindAndSavesNothing(t *testing.T) {
	f := newFx(t, cfg{seams: hello.Seams{Greeting: wordFails(errs.E("test", errs.Timeout, errors.New("slow")))}})
	if _, err := f.mod.Service().Greet(ctxT(t)); !errs.Is(err, errs.Timeout) {
		t.Fatalf("err = %v, want Timeout", err)
	}
	if len(f.store.Rows()) != 0 || len(f.sig.Published()) != 0 {
		t.Errorf("rows or signals left behind")
	}
}

func TestGreet_Concurrent_SavesEveryRowOnce(t *testing.T) {
	f := newFx(t, cfg{})
	f.start(t)
	const n = 25
	var wg sync.WaitGroup
	errc := make(chan error, 2*n)
	for range n {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := f.mod.Service().Greet(ctxT(t)); err != nil {
				errc <- err
			}
		}()
		go func() {
			defer wg.Done()
			if _, err := f.mod.Service().Greeting(ctxT(t)); err != nil {
				errc <- err
			}
		}()
	}
	wg.Wait()
	close(errc)
	for err := range errc {
		t.Errorf("concurrent call: %v", err)
	}
	if got := len(f.store.Rows()); got != n {
		t.Errorf("rows = %d, want %d", got, n)
	}
	if got := len(f.sig.Published()); got != n {
		t.Errorf("published = %d, want %d", got, n)
	}
}

// ---- the subscriber ----

func TestHandleGreeted_Delivered_CountsOnce(t *testing.T) {
	f := newFx(t, cfg{})
	f.start(t)
	f.deliver(t, alice)
	if got := f.mod.Seen(); got != 1 {
		t.Fatalf("Seen = %d, want 1", got)
	}
}

// F18
func TestHandleGreeted_NoPerson_ReturnsInvalid(t *testing.T) {
	f := newFx(t, cfg{})
	f.start(t)
	s := greetedSignal(alice)
	s.Payload = signals.NewPayload(signals.Ref("shift", bob))
	if err := f.sig.Deliver(ctxT(t), s); !errs.Is(err, errs.Invalid) {
		t.Fatalf("err = %v, want Invalid", err)
	}
	if got := f.mod.Seen(); got != 0 {
		t.Errorf("Seen = %d after an invalid delivery, want 0", got)
	}
}

// F19
func TestHandleGreeted_CtxDone_ReturnsCanceled(t *testing.T) {
	f := newFx(t, cfg{})
	f.start(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := f.mod.HandleGreeted(ctx, greetedSignal(alice)); !errs.Is(err, errs.Canceled) {
		t.Fatalf("err = %v, want Canceled", err)
	}
	if got := f.mod.Seen(); got != 0 {
		t.Errorf("Seen = %d, want 0", got)
	}
}

// F20
func TestHandleGreeted_Duplicate_CountsOnce(t *testing.T) {
	f := newFx(t, cfg{})
	f.start(t)
	s := f.deliver(t, alice)
	for range 3 {
		if err := f.sig.Deliver(ctxT(t), s); err != nil {
			t.Fatalf("redelivery: %v", err)
		}
	}
	if got := f.mod.Seen(); got != 1 {
		t.Fatalf("Seen = %d after the same Signal.ID four times, want 1", got)
	}
	f.deliver(t, alice) // a different ID counts
	if got := f.mod.Seen(); got != 2 {
		t.Errorf("Seen = %d, want 2", got)
	}
}

func TestHandleGreeted_RingRemembersLast256(t *testing.T) {
	f := newFx(t, cfg{})
	f.start(t)
	const ring = 256
	sigs := make([]signals.Signal, 0, ring+1)
	for range ring + 1 {
		sigs = append(sigs, f.deliver(t, alice))
	}
	if got := f.mod.Seen(); got != ring+1 {
		t.Fatalf("Seen = %d, want %d", got, ring+1)
	}
	// the newest 256 are remembered: replaying them changes nothing
	for _, s := range sigs[1:] {
		if err := f.sig.Deliver(ctxT(t), s); err != nil {
			t.Fatalf("replay: %v", err)
		}
	}
	if got := f.mod.Seen(); got != ring+1 {
		t.Errorf("Seen = %d after replaying the last %d, want %d", got, ring, ring+1)
	}
}

func TestHandleGreeted_ReadOnlyDatabase_StillCounts(t *testing.T) {
	f := newFx(t, cfg{readOnly: true})
	f.start(t)
	f.deliver(t, alice)
	if got := f.mod.Seen(); got != 1 {
		t.Errorf("Seen = %d, want 1 (the handler touches no database)", got)
	}
	if calls := f.db.Calls(); len(calls) != 0 {
		t.Errorf("the handler ran %d statements", len(calls))
	}
}

func TestHandleGreeted_Concurrent_CountsEveryDistinctDelivery(t *testing.T) {
	f := newFx(t, cfg{})
	f.start(t)
	const n = 100
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := f.sig.Deliver(ctxT(t), greetedSignal(alice)); err != nil {
				t.Errorf("Deliver: %v", err)
			}
		}()
	}
	wg.Wait()
	if got := f.mod.Seen(); got != n {
		t.Errorf("Seen = %d, want %d", got, n)
	}
}

// A Greet followed by delivering what it published is the in-process equivalent of the
// dispatcher's after-commit delivery.
func TestGreet_ThenDeliverPublished_CountsIt(t *testing.T) {
	f := newFx(t, cfg{})
	f.start(t)
	if _, err := f.mod.Service().Greet(ctxT(t)); err != nil {
		t.Fatalf("Greet: %v", err)
	}
	for _, s := range f.sig.Published() {
		if err := f.sig.Deliver(ctxT(t), s); err != nil {
			t.Fatalf("Deliver: %v", err)
		}
	}
	got, err := f.mod.Service().Greeting(ctxT(t))
	if err != nil {
		t.Fatalf("Greeting: %v", err)
	}
	if got.Mine != 1 || got.Seen != 1 {
		t.Errorf("Greeting = %+v, want mine=1 seen=1", got)
	}
}
