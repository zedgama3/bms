package audit_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/signals/signalstest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

func TestScoped_BadArea_ReturnsInvalid(t *testing.T) {
	f := newFx(t)
	bad := []string{"", "a", "Identity", "1abc", "has-hyphen", "has_under", "dot.ted", "has space", strings.Repeat("a", 33)}
	for _, area := range bad {
		t.Run("area "+area, func(t *testing.T) {
			r, err := f.log.Scoped(area)
			wantKind(t, err, errs.Invalid)
			if r != nil {
				t.Error("Scoped returned a Recorder with its error")
			}
		})
	}
	for _, area := range []string{"ab", "identity", "people", "a1", strings.Repeat("a", 32)} {
		t.Run("valid "+area, func(t *testing.T) {
			_, err := f.log.Scoped(area)
			wantNoErr(t, err)
		})
	}
}

func TestScoped_AfterClose_ReturnsInternal(t *testing.T) {
	f := newFx(t)
	wantNoErr(t, f.log.Close(ctxT(t)))
	_, err := f.log.Scoped("identity")
	wantKind(t, err, errs.Internal)
}

func TestRecord_EmptyOrOversizedBatch_ReturnsInvalid(t *testing.T) {
	f := newFx(t)
	rec := f.scoped(t, "identity")
	over := make([]audit.Entry, audit.MaxBatch+1)
	for i := range over {
		over[i] = loginEntry()
	}
	for name, entries := range map[string][]audit.Entry{"none": nil, "empty slice": {}, "MaxBatch+1": over} {
		t.Run(name, func(t *testing.T) {
			err := rec.Record(ctxT(t), entries...)
			wantKind(t, err, errs.Invalid)
			wantField(t, err, "entries")
		})
	}
	if n := len(f.db.Calls()); n != 0 {
		t.Errorf("a rejected batch ran %d statements", n)
	}
}

func TestRecord_MaxBatch_IsOneMultiRowStatement(t *testing.T) {
	f := newFx(t)
	rec := f.scoped(t, "identity")
	batch := make([]audit.Entry, audit.MaxBatch)
	for i := range batch {
		batch[i] = loginEntry()
	}
	wantNoErr(t, rec.Record(ctxT(t), batch...))
	if n := len(f.db.Calls()); n != 1 {
		t.Fatalf("a batch ran %d statements, want one multi-row insert", n)
	}
}

func TestRecord_SingleEntry_IsOwnStatementOutsideTx(t *testing.T) {
	f := newFx(t)
	wantNoErr(t, f.scoped(t, "identity").Record(ctxT(t), loginEntry()))
	calls := f.db.Calls()
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	if calls[0].InTx {
		t.Error("a read-style entry outside a transaction ran inside one")
	}
	if !strings.Contains(strings.ToLower(calls[0].SQL), "audit_entry") {
		t.Error("the statement does not name core.audit_entry")
	}
}

func TestRecord_InsideWriteTx_JoinsTheCallersTransaction(t *testing.T) {
	f := newFx(t)
	rec := f.scoped(t, "identity")
	err := f.db.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
		return rec.Record(ctx, loginEntry(), readEntry(alice))
	})
	wantNoErr(t, err)
	calls := f.db.Calls()
	if len(calls) != 1 || !calls[0].InTx {
		t.Fatalf("calls = %+v, want one statement inside the caller's transaction", calls)
	}
}

func TestRecord_BadEntryInBatch_WritesNothing(t *testing.T) {
	f := newFx(t)
	rec := f.scoped(t, "identity")
	bad := loginEntry()
	bad.Outcome = 0
	err := rec.Record(ctxT(t), loginEntry(), bad, loginEntry())
	wantKind(t, err, errs.Invalid)
	wantField(t, err, "entries[1].outcome")
	if len(f.db.Calls()) != 0 {
		t.Error("a bad batch was partly written")
	}
	f.readOnly()
	err = rec.Record(ctxT(t), loginEntry(), bad)
	wantKind(t, err, errs.Invalid)
	if len(f.spoolFiles(t)) != 0 {
		t.Error("a bad batch was partly spooled")
	}
}

func TestRecord_BadAction_ReturnsInvalid(t *testing.T) {
	f := newFx(t)
	rec := f.scoped(t, "identity")
	for _, a := range []string{"", "identity", "identity.", "identity.Login", "identity.log in", "identity.1x"} {
		t.Run("action "+a, func(t *testing.T) {
			e := loginEntry()
			e.Action = audit.Action(a)
			err := rec.Record(ctxT(t), loginEntry(), e)
			wantKind(t, err, errs.Invalid)
			wantField(t, err, "entries[1].action")
		})
	}
}

func TestRecord_ForeignArea_ReturnsInvalid(t *testing.T) {
	f := newFx(t)
	rec := f.scoped(t, "identity")
	foreign := loginEntry()
	foreign.Action = "people.person.read"
	err := rec.Record(ctxT(t), loginEntry(), foreign)
	wantKind(t, err, errs.Invalid)
	wantField(t, err, "entries[1].action")
	if len(f.db.Calls()) != 0 {
		t.Error("the valid entry of a batch with a foreign action was written")
	}
	// An area that merely starts with the recorder's area is still foreign.
	prefix := loginEntry()
	prefix.Action = "identityx.login"
	wantKind(t, rec.Record(ctxT(t), prefix), errs.Invalid)
}

func TestRecord_ZeroOutcome_ReturnsInvalid(t *testing.T) {
	f := newFx(t)
	rec := f.scoped(t, "identity")
	for _, o := range []audit.Outcome{0, 4, 200} {
		e := loginEntry()
		e.Outcome = o
		err := rec.Record(ctxT(t), e)
		wantKind(t, err, errs.Invalid)
		wantField(t, err, "entries[0].outcome")
	}
}

func TestRecord_FieldsLevelMismatch_ReturnsInvalid(t *testing.T) {
	f := newFx(t)
	rec := f.scoped(t, "identity")
	tests := []struct {
		name  string
		field string
		mod   func(*audit.Entry)
	}{
		{"fields without level", "entries[0].level", func(e *audit.Entry) { e.Fields = []string{"email"} }},
		{"level without fields", "entries[0].level", func(e *audit.Entry) { e.Level = model.SensitivityPersonal }},
		{"bad field name", "entries[0].fields", func(e *audit.Entry) { e.Fields = []string{"Legal Name"}; e.Level = model.SensitivityPersonal }},
		{"duplicate field", "entries[0].fields", func(e *audit.Entry) { e.Fields = []string{"email", "email"}; e.Level = model.SensitivityPersonal }},
		{"too many fields", "entries[0].fields", func(e *audit.Entry) {
			for i := 0; i <= audit.MaxFields; i++ {
				e.Fields = append(e.Fields, "f"+string(rune('a'+i/26))+string(rune('a'+i%26)))
			}
			e.Level = model.SensitivityPersonal
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := loginEntry()
			tc.mod(&e)
			err := rec.Record(ctxT(t), e)
			wantKind(t, err, errs.Invalid)
			wantField(t, err, "entries[0].")
		})
	}
}

func TestRecord_BadReason_ReturnsInvalid(t *testing.T) {
	f := newFx(t)
	rec := f.scoped(t, "identity")
	for name, reason := range map[string]string{
		"too long":       strings.Repeat("a", audit.MaxReasonBytes+1),
		"invalid utf-8":  "a\xffb",
		"control":        "a\x01b",
		"newline":        "a\nb",
		"marker control": marker + "\x00",
	} {
		t.Run(name, func(t *testing.T) {
			e := loginEntry()
			e.Reason = reason
			err := rec.Record(ctxT(t), e)
			wantKind(t, err, errs.Invalid)
			wantField(t, err, "entries[0].reason")
			noMarker(t, err)
		})
	}
}

func TestRecord_InconsistentActor_ReturnsInvalid(t *testing.T) {
	f := newFx(t)
	rec := f.scoped(t, "identity")
	for name, a := range map[string]audit.Actor{
		"person kind without person": {Kind: audit.ActorPerson},
		"person with system kind":    {Kind: audit.ActorSystem, Person: alice},
		"person with zero kind":      {Person: alice},
		"superuser without person":   {Kind: audit.ActorOperator, Superuser: true},
		"superuser with zero kind":   {Superuser: true},
		"unknown kind":               {Kind: 9},
		"caller-set titles":          {Kind: audit.ActorPerson, Person: alice, Titles: []string{"Gate lead"}},
		"caller-set titles anon":     {Kind: audit.ActorAnonymous, Titles: []string{"x"}},
	} {
		t.Run(name, func(t *testing.T) {
			e := loginEntry()
			e.Actor = a
			err := rec.Record(ctxT(t), e)
			wantKind(t, err, errs.Invalid)
			wantField(t, err, "entries[0].actor")
		})
	}
}

func TestRecord_HalfRef_ReturnsInvalid(t *testing.T) {
	f := newFx(t)
	rec := f.scoped(t, "identity")
	for name, r := range map[string]audit.Ref{
		"kind only":   {Kind: "identity.account"},
		"id only":     {ID: [16]byte{1}},
		"bad grammar": {Kind: "Identity.Account", ID: [16]byte{1}},
		"no ident":    {Kind: "identity", ID: [16]byte{1}},
	} {
		t.Run(name, func(t *testing.T) {
			e := loginEntry()
			e.Record = r
			err := rec.Record(ctxT(t), e)
			wantKind(t, err, errs.Invalid)
			wantField(t, err, "entries[0].record")
		})
	}
}

func TestRecord_DBUnavailable_ReturnsUnavailable_NotSpooled(t *testing.T) {
	t.Run("db.None", func(t *testing.T) {
		f := newFx(t, func(o *audit.Options) { o.DB = db.None() })
		err := f.scoped(t, "identity").Record(ctxT(t), loginEntry())
		wantKind(t, err, errs.Unavailable)
		if errors.Is(err, db.ErrReadOnly) {
			t.Error("db.None is not read-only")
		}
		if len(f.spoolFiles(t)) != 0 {
			t.Error("an unreachable database must not spool")
		}
	})
	t.Run("statement fails", func(t *testing.T) {
		flaky := &flakyDB{}
		f := newFx(t, func(o *audit.Options) { o.DB = flaky })
		flaky.Fake = f.db
		flaky.setFailExec(errs.E("test", errs.Unavailable, errors.New("connection refused")))
		err := f.scoped(t, "identity").Record(ctxT(t), loginEntry())
		wantKind(t, err, errs.Unavailable)
		if len(f.spoolFiles(t)) != 0 {
			t.Error("a non-read-only failure was spooled")
		}
	})
	t.Run("other kinds pass through", func(t *testing.T) {
		flaky := &flakyDB{}
		f := newFx(t, func(o *audit.Options) { o.DB = flaky })
		flaky.Fake = f.db
		flaky.setFailExec(errs.E("test", errs.Conflict, errors.New("x")))
		err := f.scoped(t, "identity").Record(ctxT(t), loginEntry())
		wantKind(t, err, errs.Conflict)
		if len(f.spoolFiles(t)) != 0 {
			t.Error("a non-read-only failure was spooled")
		}
	})
}

func TestRecord_TitleSourceFails_ReturnsKind(t *testing.T) {
	f := newFx(t, func(*audit.Options) {})
	f.titles.err = errs.E("test.titles", errs.Unavailable, errors.New("authz down"))
	f.opts.Titles = f.titles
	log := f.build(t)
	rec, err := log.Scoped("identity")
	wantNoErr(t, err)
	e := loginEntry()
	e.Actor = person(alice)
	err = rec.Record(ctxT(t), e)
	wantKind(t, err, errs.Unavailable)
	if len(f.db.Calls()) != 0 || len(f.spoolFiles(t)) != 0 {
		t.Error("an entry whose titles failed was written")
	}
	f.titles.err = errs.E("test.titles", errs.Timeout, errors.New("slow"))
	wantKind(t, rec.Record(ctxT(t), e), errs.Timeout)
}

func TestRecord_CtxDone_ReturnsCanceled(t *testing.T) {
	f := newFx(t)
	rec := f.scoped(t, "identity")
	t.Run("canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		wantKind(t, rec.Record(ctx, loginEntry()), errs.Canceled)
	})
	t.Run("deadline passed", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
		defer cancel()
		wantKind(t, rec.Record(ctx, loginEntry()), errs.Timeout)
	})
	t.Run("read-only copy too", func(t *testing.T) {
		f.readOnly()
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		wantKind(t, rec.Record(ctx, loginEntry()), errs.Canceled)
		if len(f.spoolFiles(t)) != 0 {
			t.Error("a canceled Record spooled")
		}
	})
	if len(f.db.Calls()) != 0 {
		t.Error("a canceled Record ran a statement")
	}
}

func TestRecord_AfterClose_ReturnsUnavailable(t *testing.T) {
	f := newFx(t)
	rec := f.scoped(t, "identity")
	wantNoErr(t, f.log.Close(ctxT(t)))
	wantKind(t, rec.Record(ctxT(t), loginEntry()), errs.Unavailable)
	f.readOnly()
	wantKind(t, rec.Record(ctxT(t), loginEntry()), errs.Unavailable)
	if len(f.db.Calls()) != 0 {
		t.Error("a Record after Close ran a statement")
	}
}

func TestClose_IsIdempotent(t *testing.T) {
	f := newFx(t)
	wantNoErr(t, f.log.Close(ctxT(t)))
	wantNoErr(t, f.log.Close(ctxT(t)))
}

// --- stamping, resolved through the spool line the recorder writes ------------------------

func TestRecord_Stamps_IDTimeInstanceForTheWholeBatch(t *testing.T) {
	f := newFx(t)
	f.readOnly()
	rec := f.scoped(t, "identity")
	wantNoErr(t, rec.Record(ctxT(t), loginEntry(), loginEntry(), loginEntry()))
	lines := f.spooled(t)
	if len(lines) != 3 {
		t.Fatalf("spooled %d lines, want 3", len(lines))
	}
	ids := map[string]bool{}
	for _, l := range lines {
		id := str(l["id"])
		if _, err := model.ParseID[audit.RecordID](id); err != nil || id == "00000000-0000-0000-0000-000000000000" {
			t.Errorf("id %q is not a fresh UUID", id)
		}
		ids[id] = true
		if str(l["at"]) != epoch.UTC().Format(time.RFC3339Nano) {
			t.Errorf("at = %q, want the clock's instant %q", str(l["at"]), epoch.UTC().Format(time.RFC3339Nano))
		}
		if str(l["instance"]) != "test-copy" {
			t.Errorf("instance = %q", str(l["instance"]))
		}
		if l["v"] != float64(audit.SpoolVersion) {
			t.Errorf("v = %v", l["v"])
		}
	}
	if len(ids) != 3 {
		t.Error("entry ids are not unique")
	}
	advance(t, f.clk, time.Hour)
	wantNoErr(t, rec.Record(ctxT(t), loginEntry()))
	last := f.spooled(t)[3]
	if str(last["at"]) != epoch.Add(time.Hour).UTC().Format(time.RFC3339Nano) {
		t.Errorf("a later call carried at = %q", str(last["at"]))
	}
}

func TestRecord_Stamps_TraceID(t *testing.T) {
	f := newFx(t)
	f.readOnly()
	rec := f.scoped(t, "identity")
	wantNoErr(t, rec.Record(ctxT(t), loginEntry()))
	if _, ok := f.spooled(t)[0]["trace_id"]; ok {
		t.Error("trace_id must be omitted when ctx has no span")
	}
	ctx, span := f.opts.Tracer.Start(ctxT(t), "test.request")
	defer span.End()
	wantNoErr(t, rec.Record(ctx, loginEntry()))
	got := str(f.spooled(t)[1]["trace_id"])
	if len(got) != 32 || strings.ToLower(got) != got {
		t.Fatalf("trace_id = %q, want 32 lower-case hex", got)
	}
}

func TestRecord_ActorResolution(t *testing.T) {
	f := newFx(t)
	f.readOnly()
	rec := f.scoped(t, "identity")

	type want struct {
		kind      string
		person    model.PersonID
		superuser bool
	}
	tests := []struct {
		name  string
		ctx   func(context.Context) context.Context
		actor audit.Actor
		want  want
	}{
		{"session person", func(c context.Context) context.Context { return withSession(c, person(alice)) },
			audit.Actor{}, want{"person", alice, false}},
		{"session superuser is marked", func(c context.Context) context.Context { return withSession(c, superuser(alice)) },
			audit.Actor{}, want{"person", alice, true}},
		{"no session is anonymous", func(c context.Context) context.Context { return c },
			audit.Actor{}, want{"anonymous", model.PersonID{}, false}},
		{"explicit operator beats session", func(c context.Context) context.Context { return withSession(c, person(alice)) },
			audit.Actor{Kind: audit.ActorOperator}, want{"operator", model.PersonID{}, false}},
		{"explicit person beats session", func(c context.Context) context.Context { return withSession(c, person(alice)) },
			person(bob), want{"person", bob, false}},
		{"explicit superuser", func(c context.Context) context.Context { return c },
			superuser(carol), want{"person", carol, true}},
		{"explicit anonymous beats session", func(c context.Context) context.Context { return withSession(c, person(alice)) },
			audit.Actor{Kind: audit.ActorAnonymous}, want{"anonymous", model.PersonID{}, false}},
	}
	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := loginEntry()
			e.Actor = tc.actor
			wantNoErr(t, rec.Record(tc.ctx(ctxT(t)), e))
			a := obj(f.spooled(t)[i]["actor"])
			if str(a["kind"]) != tc.want.kind {
				t.Errorf("actor.kind = %q, want %q", str(a["kind"]), tc.want.kind)
			}
			if tc.want.person.IsZero() {
				if _, ok := a["person"]; ok {
					t.Error("actor.person must be omitted")
				}
			} else if str(a["person"]) != tc.want.person.String() {
				t.Errorf("actor.person = %q", str(a["person"]))
			}
			if got := a["superuser"] == true; got != tc.want.superuser {
				t.Errorf("actor.superuser = %v, want %v", got, tc.want.superuser)
			}
		})
	}
}

func TestRecord_InSignalDelivery_DefaultsToSystem(t *testing.T) {
	f := newFx(t)
	f.readOnly()
	rec := f.scoped(t, "identity")

	bus, err := signalstest.NewFake("hello", nil, []signals.Name{"people.person-created"})
	wantNoErr(t, err)
	var inHandler, explicit bool
	wantNoErr(t, bus.Subscribe("people.person-created", func(ctx context.Context, _ signals.Signal) error {
		inHandler = signals.InDelivery(ctx)
		e := loginEntry()
		if explicit {
			e.Actor = audit.Actor{Kind: audit.ActorOperator}
		}
		return rec.Record(ctx, e)
	}))
	sig := signals.Signal{ID: model.NewID[signals.SignalID](), Name: "people.person-created", At: epoch, Payload: signals.Payload{}}
	// A session is present in ctx (it must be ignored in a delivery), and an explicit actor
	// is still taken as given.
	wantNoErr(t, bus.Deliver(withSession(ctxT(t), person(alice)), sig))
	if !inHandler {
		t.Fatal("the handler's ctx is not InDelivery")
	}
	if got := str(obj(f.spooled(t)[0]["actor"])["kind"]); got != "system" {
		t.Errorf("actor in a signal delivery = %q, want system", got)
	}
	if _, ok := obj(f.spooled(t)[0]["actor"])["person"]; ok {
		t.Error("a system actor carries no person")
	}

	explicit = true
	wantNoErr(t, bus.Deliver(ctxT(t), sig))
	if got := str(obj(f.spooled(t)[1]["actor"])["kind"]); got != "operator" {
		t.Errorf("explicit actor in a delivery = %q, want operator", got)
	}
}

func TestRecord_Titles_FilledOncePerDistinctPersonPerCall(t *testing.T) {
	f := newFx(t)
	f.titles.titles[alice] = []string{"Gate lead", "Medic"}
	f.titles.titles[bob] = nil
	f.opts.Titles = f.titles
	f.readOnly()
	log := f.build(t)
	rec, err := log.Scoped("identity")
	wantNoErr(t, err)

	a, b := loginEntry(), loginEntry()
	a.Actor, b.Actor = person(alice), person(bob)
	wantNoErr(t, rec.Record(ctxT(t), a, a, b, a))
	if n := f.titles.callCount(); n != 2 {
		t.Errorf("TitlesOf called %d times for two distinct people in one call, want 2", n)
	}
	lines := f.spooled(t)
	for i, l := range lines {
		titles := list(obj(l["actor"])["titles"])
		if i == 2 { // bob holds none
			if len(titles) != 0 {
				t.Errorf("line %d: titles = %v, want none", i, titles)
			}
			continue
		}
		if len(titles) != 2 || titles[0] != "Gate lead" || titles[1] != "Medic" {
			t.Errorf("line %d: titles = %v", i, titles)
		}
	}
	// Another call asks again: titles are those held at that moment.
	wantNoErr(t, rec.Record(ctxT(t), a))
	if n := f.titles.callCount(); n != 3 {
		t.Errorf("TitlesOf calls after a second Record = %d, want 3", n)
	}
}

func TestRecord_Titles_NotAskedForNonPersonActors(t *testing.T) {
	f := newFx(t)
	f.opts.Titles = f.titles
	f.readOnly()
	log := f.build(t)
	rec, err := log.Scoped("identity")
	wantNoErr(t, err)
	for _, k := range []audit.ActorKind{audit.ActorAnonymous, audit.ActorOperator, audit.ActorSystem} {
		e := loginEntry()
		e.Actor = audit.Actor{Kind: k}
		wantNoErr(t, rec.Record(ctxT(t), e))
	}
	if n := f.titles.callCount(); n != 0 {
		t.Errorf("TitlesOf called %d times for non-person actors", n)
	}
	// With a nil source the titles stay empty (M1).
	g := newFx(t)
	g.readOnly()
	wantNoErr(t, g.scoped(t, "identity").Record(withSession(ctxT(t), person(alice)), loginEntry()))
	if titles := list(obj(g.spooled(t)[0]["actor"])["titles"]); len(titles) != 0 {
		t.Errorf("titles = %v with no TitleSource", titles)
	}
}

func TestRecord_ConcurrentCalls_AreSafe(t *testing.T) {
	t.Run("database path", func(t *testing.T) {
		f := newFx(t)
		rec := f.scoped(t, "identity")
		runConcurrent(t, 32, func() error { return rec.Record(t.Context(), loginEntry()) })
		if n := len(f.db.Calls()); n != 32 {
			t.Errorf("calls = %d, want 32", n)
		}
	})
	t.Run("spool path", func(t *testing.T) {
		f := newFx(t)
		f.readOnly()
		rec := f.scoped(t, "identity")
		runConcurrent(t, 32, func() error { return rec.Record(t.Context(), loginEntry(), readEntry(alice)) })
		lines := f.spooled(t) // every line complete and valid JSON: no interleaving
		if len(lines) != 64 {
			t.Fatalf("spooled %d lines, want 64", len(lines))
		}
		ids := map[string]bool{}
		for _, l := range lines {
			ids[str(l["id"])] = true
		}
		if len(ids) != 64 {
			t.Errorf("%d distinct ids, want 64", len(ids))
		}
	})
	t.Run("Scoped and Search alongside Record", func(t *testing.T) {
		f := newFx(t)
		rec := f.scoped(t, "identity")
		var wg sync.WaitGroup
		for range 8 {
			wg.Go(func() {
				if _, err := f.log.Scoped("people"); err != nil {
					t.Errorf("Scoped: %v", err)
				}
				if err := rec.Record(t.Context(), loginEntry()); err != nil {
					t.Errorf("Record: %v", err)
				}
			})
		}
		wg.Wait()
	})
}

func runConcurrent(t *testing.T, n int, fn func() error) {
	t.Helper()
	var wg sync.WaitGroup
	errc := make(chan error, n)
	start := make(chan struct{})
	for range n {
		wg.Go(func() {
			<-start
			errc <- fn()
		})
	}
	close(start)
	wg.Wait()
	close(errc)
	for err := range errc {
		if err != nil {
			t.Errorf("concurrent call failed (kind %v)", errs.KindOf(err))
		}
	}
}

func TestRecord_ControlCharactersInTitles_AreStripped_NeverFailTheRecord(t *testing.T) {
	f := newFx(t)
	f.titles.titles[alice] = []string{"Gate\x00lead", "\x01\x02\x7f", "Med\nic\t", "", "Plain"}
	f.opts.Titles = f.titles
	f.readOnly()
	log := f.build(t)
	rec, err := log.Scoped("identity")
	wantNoErr(t, err)
	e := loginEntry()
	e.Actor = person(alice)
	wantNoErr(t, rec.Record(ctxT(t), e))
	titles := list(obj(f.spooled(t)[0]["actor"])["titles"])
	var got []string
	for _, v := range titles {
		got = append(got, str(v))
	}
	if strings.Join(got, "|") != "Gatelead|Medic|Plain" {
		t.Errorf("titles = %q, want control characters removed and the all-control title dropped", got)
	}
	for _, s := range got {
		for _, r := range s {
			if r < 0x20 || r == 0x7f {
				t.Errorf("title %q still holds a control character", s)
			}
		}
	}

	// A person whose only title is control characters has no titles, and the write succeeds.
	f.titles.titles[bob] = []string{"\x00\x00"}
	e.Actor = person(bob)
	wantNoErr(t, rec.Record(ctxT(t), e))
	if got := list(obj(f.spooled(t)[1]["actor"])["titles"]); len(got) != 0 {
		t.Errorf("titles = %v, want none", got)
	}
}
