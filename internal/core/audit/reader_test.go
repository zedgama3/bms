package audit_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// readerFx is a Log whose reader may use both audit permissions, on a read-only copy so the
// log's own audit.searched / audit.exported entries land in the spool where a test can see them.
func readerFx(t *testing.T) *fx {
	t.Helper()
	f := newFx(t)
	f.perms.allow[audit.PermissionSearch] = true
	f.perms.allow[audit.PermissionExport] = true
	f.readOnly()
	return f
}

func asReviewer(ctx context.Context) context.Context { return withSession(ctx, person(carol)) }

func entriesNamed(t testing.TB, f *fx, action string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, l := range f.spooled(t) {
		if str(l["action"]) == action {
			out = append(out, l)
		}
	}
	return out
}

// --- Search -------------------------------------------------------------------------------

func TestSearch_NoActor_ReturnsUnauthenticated(t *testing.T) {
	f := readerFx(t)
	_, err := f.log.Search(ctxT(t), audit.Query{})
	wantKind(t, err, errs.Unauthenticated)
	if len(f.spoolLines(t)) != 0 {
		t.Error("an unauthenticated search left an entry")
	}
}

func TestSearch_NoPermission_ReturnsPermission(t *testing.T) {
	t.Run("checker denies", func(t *testing.T) {
		f := newFx(t) // checker allows nothing
		_, err := f.log.Search(asReviewer(ctxT(t)), audit.Query{})
		wantKind(t, err, errs.Permission)
		if got := f.perms.askedFor(); len(got) != 1 || got[0] != audit.PermissionSearch {
			t.Errorf("asked for %v, want [audit.search]", got)
		}
	})
	t.Run("export permission is not enough", func(t *testing.T) {
		f := newFx(t)
		f.perms.allow[audit.PermissionExport] = true
		_, err := f.log.Search(asReviewer(ctxT(t)), audit.Query{})
		wantKind(t, err, errs.Permission)
	})
	t.Run("nil checker fails closed", func(t *testing.T) {
		f := newFx(t, func(o *audit.Options) { o.Permissions = nil })
		_, err := f.log.Search(asReviewer(ctxT(t)), audit.Query{})
		wantKind(t, err, errs.Permission)
		_, err = f.log.Search(withSession(ctxT(t), superuser(carol)), audit.Query{})
		wantKind(t, err, errs.Permission)
	})
	t.Run("denied search is not answered with data and runs no query", func(t *testing.T) {
		flaky := &flakyDB{}
		f := newFx(t, func(o *audit.Options) { o.DB = flaky })
		flaky.Fake = f.db
		_, err := f.log.Search(asReviewer(ctxT(t)), audit.Query{})
		wantKind(t, err, errs.Permission)
		if flaky.queryCount() != 0 {
			t.Error("the store was queried before authorization")
		}
	})
}

func TestSearch_AuthorizesBeforeValidating(t *testing.T) {
	f := newFx(t)
	_, err := f.log.Search(asReviewer(ctxT(t)), audit.Query{Limit: -1})
	wantKind(t, err, errs.Permission)
	_, err = f.log.Search(ctxT(t), audit.Query{Limit: -1})
	wantKind(t, err, errs.Unauthenticated)
}

func TestSearch_Allowed_RecordsAuditSearchedExactlyOnce(t *testing.T) {
	f := readerFx(t)
	q := audit.Query{Subject: bob, Record: audit.RefOf("identity.account", model.AccountID{7})}
	page, err := f.log.Search(asReviewer(ctxT(t)), q)
	wantNoErr(t, err)
	if len(page.Records) != 0 || page.Next != "" {
		t.Errorf("page = %+v from an empty store", page)
	}
	got := entriesNamed(t, f, "audit.searched")
	if len(got) != 1 {
		t.Fatalf("audit.searched entries = %d, want exactly 1", len(got))
	}
	e := got[0]
	if str(e["outcome"]) != "succeeded" || str(e["subject"]) != bob.String() {
		t.Errorf("entry = %v", e)
	}
	if r := obj(e["record"]); str(r["kind"]) != "identity.account" || str(r["id"]) != (model.AccountID{7}).String() {
		t.Errorf("record = %v", r)
	}
	if a := obj(e["actor"]); str(a["person"]) != carol.String() || str(a["kind"]) != "person" {
		t.Errorf("actor = %v", a)
	}
	if got := f.perms.askedFor(); len(got) != 1 || got[0] != audit.PermissionSearch {
		t.Errorf("asked %v", got)
	}
	// Two searches, two entries.
	_, err = f.log.Search(asReviewer(ctxT(t)), audit.Query{})
	wantNoErr(t, err)
	if n := len(entriesNamed(t, f, "audit.searched")); n != 2 {
		t.Errorf("audit.searched after two searches = %d", n)
	}
}

func TestSearch_SuperuserActor_IsMarked(t *testing.T) {
	f := readerFx(t)
	_, err := f.log.Search(withSession(ctxT(t), superuser(carol)), audit.Query{})
	wantNoErr(t, err)
	got := entriesNamed(t, f, "audit.searched")
	if len(got) != 1 || obj(got[0]["actor"])["superuser"] != true {
		t.Errorf("superuser mark missing: %v", got)
	}
}

func TestSearch_BadQuery_ReturnsInvalid(t *testing.T) {
	f := readerFx(t)
	half := audit.Ref{Kind: "identity.account"}
	tests := []struct {
		name  string
		q     audit.Query
		field string
	}{
		{"negative limit", audit.Query{Limit: -1}, "limit"},
		{"limit over max", audit.Query{Limit: audit.MaxPageSize + 1}, "limit"},
		{"half ref kind", audit.Query{Record: half}, "record"},
		{"half ref id", audit.Query{Record: audit.Ref{ID: [16]byte{1}}}, "record"},
		{"bad area", audit.Query{Area: "Bad Area"}, "area"},
		{"bad action", audit.Query{Action: "not an action"}, "action"},
		{"within start only", audit.Query{Within: model.TimeRange{Start: epoch}}, "within"},
		{"within end only", audit.Query{Within: model.TimeRange{End: epoch}}, "within"},
		{"within inverted", audit.Query{Within: model.TimeRange{Start: epoch, End: epoch.Add(-time.Hour)}}, "within"},
		{"within empty", audit.Query{Within: model.TimeRange{Start: epoch, End: epoch}}, "within"},
		{"cursor garbage", audit.Query{After: "%%%not a cursor%%%"}, "after"},
		{"cursor from nowhere", audit.Query{After: audit.Cursor(strings.Repeat("A", 10))}, "after"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wantKind(t, tc.q.Validate(), errs.Invalid)
			wantField(t, tc.q.Validate(), tc.field)
			_, err := f.log.Search(asReviewer(ctxT(t)), tc.q)
			wantKind(t, err, errs.Invalid)
			wantField(t, err, tc.field)
		})
	}
	if n := len(f.spoolLines(t)); n != 0 {
		t.Errorf("invalid searches left %d audit entries (the entry is written after validation)", n)
	}
}

func TestQuery_Validate_Valid(t *testing.T) {
	for name, q := range map[string]audit.Query{
		"zero":             {},
		"limit max":        {Limit: audit.MaxPageSize},
		"limit one":        {Limit: 1},
		"all filters":      {Actor: alice, Subject: bob, Record: audit.RefOf("identity.account", model.AccountID{1}), Area: "identity", Action: "identity.login", Within: model.TimeRange{Start: epoch, End: epoch.Add(time.Hour)}, SuperuserOnly: true},
		"area with digits": {Area: "area1"},
	} {
		t.Run(name, func(t *testing.T) { wantNoErr(t, q.Validate()) })
	}
}

func TestSearch_StoreFails_ReturnsKind(t *testing.T) {
	for _, kind := range []errs.Kind{errs.Unavailable, errs.Timeout, errs.Internal} {
		t.Run(kind.String(), func(t *testing.T) {
			flaky := &flakyDB{}
			f := newFx(t, func(o *audit.Options) { o.DB = flaky })
			flaky.Fake = f.db
			f.perms.allow[audit.PermissionSearch] = true
			f.readOnly() // the entry goes to the spool, the query reaches the store
			flaky.mu.Lock()
			flaky.failQuery = errs.E("test.store", kind, errors.New("store failed"))
			flaky.mu.Unlock()
			_, err := f.log.Search(asReviewer(ctxT(t)), audit.Query{})
			wantKind(t, err, kind)
		})
	}
}

func TestSearch_RecordFails_ReturnsError(t *testing.T) {
	flaky := &flakyDB{}
	f := newFx(t, func(o *audit.Options) { o.DB = flaky })
	flaky.Fake = f.db
	f.perms.allow[audit.PermissionSearch] = true
	flaky.setFailExec(errs.E("test", errs.Unavailable, errors.New("down")))
	_, err := f.log.Search(asReviewer(ctxT(t)), audit.Query{})
	wantKind(t, err, errs.Unavailable)
	if flaky.queryCount() != 0 {
		t.Error("results were read although the search could not be recorded (no audit, no action)")
	}
}

func TestSearch_ReadOnlyCopy_Works_AndSpoolsItsOwnEntry(t *testing.T) {
	f := readerFx(t)
	_, err := f.log.Search(asReviewer(ctxT(t)), audit.Query{})
	wantNoErr(t, err)
	if n := len(entriesNamed(t, f, "audit.searched")); n != 1 {
		t.Errorf("spooled audit.searched = %d", n)
	}
}

func TestSearch_SpoolUnwritable_FailsTheSearch(t *testing.T) {
	f := newFx(t, func(o *audit.Options) { o.DataDir = "/proc/definitely/not/writable" })
	f.perms.allow[audit.PermissionSearch] = true
	f.readOnly()
	_, err := f.log.Search(asReviewer(ctxT(t)), audit.Query{})
	wantKind(t, err, errs.Unavailable)
}

func TestSearch_CtxDone(t *testing.T) {
	f := readerFx(t)
	ctx, cancel := context.WithCancel(asReviewer(t.Context()))
	cancel()
	_, err := f.log.Search(ctx, audit.Query{})
	wantKind(t, err, errs.Canceled)
}

// --- Export -------------------------------------------------------------------------------

type countingWriter struct {
	buf bytes.Buffer
	n   int
	err error
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.n++
	if w.err != nil {
		return 0, w.err
	}
	return w.buf.Write(p) //nolint:wrapcheck // test double
}

func TestExport_ZeroPerson_ReturnsInvalid(t *testing.T) {
	f := readerFx(t)
	var w countingWriter
	n, err := f.log.Export(asReviewer(ctxT(t)), model.PersonID{}, &w)
	wantKind(t, err, errs.Invalid)
	wantField(t, err, "person")
	if n != 0 || w.n != 0 {
		t.Errorf("wrote %d lines / %d writes", n, w.n)
	}
	if len(entriesNamed(t, f, "audit.exported")) != 0 {
		t.Error("an invalid export was recorded")
	}
}

func TestExport_NoPermission_ReturnsPermission(t *testing.T) {
	var w countingWriter
	t.Run("no actor", func(t *testing.T) {
		f := readerFx(t)
		_, err := f.log.Export(ctxT(t), bob, &w)
		wantKind(t, err, errs.Unauthenticated)
	})
	t.Run("checker denies", func(t *testing.T) {
		f := newFx(t)
		_, err := f.log.Export(asReviewer(ctxT(t)), bob, &w)
		wantKind(t, err, errs.Permission)
		if got := f.perms.askedFor(); len(got) != 1 || got[0] != audit.PermissionExport {
			t.Errorf("asked for %v, want [audit.export]", got)
		}
	})
	t.Run("search permission is not enough", func(t *testing.T) {
		f := newFx(t)
		f.perms.allow[audit.PermissionSearch] = true
		_, err := f.log.Export(asReviewer(ctxT(t)), bob, &w)
		wantKind(t, err, errs.Permission)
	})
	t.Run("nil checker fails closed", func(t *testing.T) {
		f := newFx(t, func(o *audit.Options) { o.Permissions = nil })
		_, err := f.log.Export(withSession(ctxT(t), superuser(carol)), bob, &w)
		wantKind(t, err, errs.Permission)
	})
	if w.n != 0 {
		t.Error("a refused export wrote")
	}
}

func TestExport_Allowed_RecordsAuditExportedExactlyOnce(t *testing.T) {
	f := readerFx(t)
	var w countingWriter
	n, err := f.log.Export(asReviewer(ctxT(t)), bob, &w)
	wantNoErr(t, err)
	if n != 0 || w.buf.Len() != 0 {
		t.Errorf("exported %d / %d bytes from an empty store", n, w.buf.Len())
	}
	got := entriesNamed(t, f, "audit.exported")
	if len(got) != 1 {
		t.Fatalf("audit.exported entries = %d, want 1", len(got))
	}
	if str(got[0]["subject"]) != bob.String() || str(got[0]["outcome"]) != "succeeded" {
		t.Errorf("entry = %v", got[0])
	}
	if a := obj(got[0]["actor"]); str(a["person"]) != carol.String() {
		t.Errorf("actor = %v", a)
	}
}

func TestExport_SuperuserActor_IsMarked(t *testing.T) {
	f := readerFx(t)
	_, err := f.log.Export(withSession(ctxT(t), superuser(carol)), bob, io.Discard)
	wantNoErr(t, err)
	got := entriesNamed(t, f, "audit.exported")
	if len(got) != 1 || obj(got[0]["actor"])["superuser"] != true {
		t.Errorf("superuser mark missing: %v", got)
	}
}

func TestExport_RecordFails_WritesNothing(t *testing.T) {
	flaky := &flakyDB{}
	f := newFx(t, func(o *audit.Options) { o.DB = flaky })
	flaky.Fake = f.db
	f.perms.allow[audit.PermissionExport] = true
	flaky.setFailExec(errs.E("test", errs.Unavailable, errors.New("down")))
	var w countingWriter
	_, err := f.log.Export(asReviewer(ctxT(t)), bob, &w)
	wantKind(t, err, errs.Unavailable)
	if w.n != 0 {
		t.Errorf("export wrote %d times before its entry was recorded", w.n)
	}
	if flaky.queryCount() != 0 {
		t.Error("rows were read before the export was recorded")
	}
}

func TestExport_CtxDone(t *testing.T) {
	f := readerFx(t)
	ctx, cancel := context.WithCancel(asReviewer(t.Context()))
	cancel()
	_, err := f.log.Export(ctx, bob, io.Discard)
	wantKind(t, err, errs.Canceled)
}

// --- CountAbout and Purge -------------------------------------------------------------------

func TestCountAbout_ZeroPerson_ReturnsInvalid(t *testing.T) {
	f := newFx(t)
	_, err := f.log.CountAbout(ctxT(t), model.PersonID{})
	wantKind(t, err, errs.Invalid)
}

func TestPurge_BadBefore_ReturnsInvalid(t *testing.T) {
	f := newFx(t)
	tests := map[string]time.Time{
		"zero":            {},
		"in the future":   epoch.Add(time.Second),
		"far future":      epoch.Add(100 * 365 * 24 * time.Hour),
		"zero in non-UTC": time.Time{}.In(time.FixedZone("x", 3600)),
	}
	for name, before := range tests {
		t.Run(name, func(t *testing.T) {
			n, err := f.log.Purge(ctxT(t), before)
			wantKind(t, err, errs.Invalid)
			wantField(t, err, "before")
			if n != 0 {
				t.Errorf("deleted %d", n)
			}
		})
	}
	if len(f.db.Calls()) != 0 {
		t.Error("a rejected purge ran statements")
	}
}

func TestPurge_FutureBefore_ReturnsInvalid(t *testing.T) {
	// The clock is injected: "future" is relative to Options.Clock, not the wall clock.
	f := newFx(t)
	_, err := f.log.Purge(ctxT(t), epoch.Add(time.Nanosecond))
	wantKind(t, err, errs.Invalid)
	if err := f.clk.Advance(time.Hour); err != nil {
		t.Fatal(err)
	}
	_, err = f.log.Purge(ctxT(t), epoch.Add(time.Nanosecond))
	if errs.Is(err, errs.Invalid) {
		t.Error("an instant that is now in the past was refused as future")
	}
}

func TestPurge_ReadOnly_ReturnsErrReadOnly(t *testing.T) {
	f := newFx(t)
	f.readOnly()
	_, err := f.log.Purge(ctxT(t), epoch.Add(-time.Hour))
	wantReadOnly(t, err)
}
