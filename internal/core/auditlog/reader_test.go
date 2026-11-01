package auditlog_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/auditlog"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// The reader tests are audit's TestSearch_*, TestExport_* and TestCountAbout_* moved. Audit.md
// F15-F22 name the Record rows; the reading rows are F23-F27 in the same spec. The service
// has no spool of its own to inspect, so an entry is seen as the statement that wrote it.

// --- Search -------------------------------------------------------------------------------

func TestSearch_NoActor_ReturnsUnauthenticated(t *testing.T) {
	f := readerFx(t)
	_, err := f.svc.Search(ctxT(t), audit.Query{})
	wantKind(t, err, errs.Unauthenticated)
	if n := len(f.recorded("audit.searched")); n != 0 {
		t.Errorf("an unauthenticated search left %d entries", n)
	}
}

func TestSearch_NoPermission_ReturnsPermission(t *testing.T) {
	t.Run("nothing granted", func(t *testing.T) {
		f := newFx(t)
		_, err := f.svc.Search(asReviewer(ctxT(t)), audit.Query{})
		wantKind(t, err, errs.Permission)
	})
	t.Run("export permission is not enough", func(t *testing.T) {
		f := newFx(t)
		f.authz.Grant(carol, auditlog.PermissionExport)
		_, err := f.svc.Search(asReviewer(ctxT(t)), audit.Query{})
		wantKind(t, err, errs.Permission)
	})
	t.Run("search granted to someone else", func(t *testing.T) {
		f := newFx(t)
		f.authz.Grant(bob, auditlog.PermissionSearch)
		_, err := f.svc.Search(asReviewer(ctxT(t)), audit.Query{})
		wantKind(t, err, errs.Permission)
	})
	t.Run("denied search runs no query and leaves no entry", func(t *testing.T) {
		f := newFx(t)
		_, err := f.svc.Search(asReviewer(ctxT(t)), audit.Query{})
		wantKind(t, err, errs.Permission)
		if f.db.queryCount() != 0 {
			t.Error("the store was queried before authorization")
		}
		if n := len(f.recorded("audit.searched")); n != 0 {
			t.Errorf("a denied search left %d entries", n)
		}
	})
}

func TestSearch_Allowed_PersonWithPermission_Succeeds(t *testing.T) {
	f := newFx(t)
	f.authz.Grant(carol, auditlog.PermissionSearch)
	_, err := f.svc.Search(asReviewer(ctxT(t)), audit.Query{})
	wantNoErr(t, err)
}

func TestSearch_AuthorizesBeforeValidating(t *testing.T) {
	f := newFx(t)
	_, err := f.svc.Search(asReviewer(ctxT(t)), audit.Query{Limit: -1})
	wantKind(t, err, errs.Permission)
	_, err = f.svc.Search(ctxT(t), audit.Query{Limit: -1})
	wantKind(t, err, errs.Unauthenticated)
}

func TestSearch_Allowed_RecordsAuditSearchedExactlyOnce(t *testing.T) {
	f := readerFx(t)
	q := audit.Query{Subject: bob, Record: audit.RefOf("identity.account", model.AccountID{7})}
	page, err := f.svc.Search(asReviewer(ctxT(t)), q)
	wantNoErr(t, err)
	if len(page.Records) != 0 || page.Next != "" {
		t.Errorf("page = %+v from an empty store", page)
	}
	if n := len(f.recorded("audit.searched")); n != 1 {
		t.Fatalf("audit.searched entries = %d, want exactly 1", n)
	}
	e := f.recorded("audit.searched")[0]
	if e.Outcome != audit.Succeeded || e.Subject != bob || e.Record != q.Record {
		t.Errorf("entry = %+v, want succeeded about bob and the account", e)
	}
	// Two searches, two entries.
	_, err = f.svc.Search(asReviewer(ctxT(t)), audit.Query{})
	wantNoErr(t, err)
	if n := len(f.recorded("audit.searched")); n != 2 {
		t.Errorf("audit.searched after two searches = %d", n)
	}
}

// The actor (and its superuser mark) is the Recorder's to resolve from ctx (audit decision 6);
// the service must not set one itself.
func TestSearch_SuperuserActor_Succeeds_LeavesActorToTheRecorder(t *testing.T) {
	f := newFx(t)
	f.authz.Superuser(carol)
	_, err := f.svc.Search(withSession(ctxT(t), superuser(carol)), audit.Query{})
	wantNoErr(t, err)
	got := f.recorded("audit.searched")
	if len(got) != 1 || got[0].Actor.Kind != 0 {
		t.Errorf("entries = %+v: the entry must name no actor", got)
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
			_, err := f.svc.Search(asReviewer(ctxT(t)), tc.q)
			wantKind(t, err, errs.Invalid)
			wantField(t, err, tc.field)
		})
	}
	if n := len(f.recorded("audit.searched")); n != 0 {
		t.Errorf("invalid searches left %d audit entries (the entry is written after validation)", n)
	}
}

func TestSearch_StoreFails_ReturnsKind(t *testing.T) {
	for _, kind := range []errs.Kind{errs.Unavailable, errs.Timeout, errs.Internal} {
		t.Run(kind.String(), func(t *testing.T) {
			f := readerFx(t)
			f.db.setFailQuery(errs.E("test.store", kind, errors.New("store failed")))
			_, err := f.svc.Search(asReviewer(ctxT(t)), audit.Query{})
			wantKind(t, err, kind)
		})
	}
}

func TestSearch_RecordFails_ReturnsError(t *testing.T) {
	f := readerFx(t)
	f.rec.Fail(errs.E("test", errs.Unavailable, errors.New("down")))
	_, err := f.svc.Search(asReviewer(ctxT(t)), audit.Query{})
	wantKind(t, err, errs.Unavailable)
	if f.db.queryCount() != 0 {
		t.Error("results were read although the search could not be recorded (no audit, no action)")
	}
}

func TestSearch_ReadOnlyCopy_Works(t *testing.T) {
	f := readerFx(t)
	f.fake.SetReadOnly(true)
	_, err := f.svc.Search(asReviewer(ctxT(t)), audit.Query{})
	wantNoErr(t, err)
}

func TestSearch_CtxDone(t *testing.T) {
	f := readerFx(t)
	ctx, cancel := context.WithCancel(asReviewer(t.Context()))
	cancel()
	_, err := f.svc.Search(ctx, audit.Query{})
	wantKind(t, err, errs.Canceled)
}

func TestSearch_DeadlinePassed_ReturnsTimeout(t *testing.T) {
	f := readerFx(t)
	ctx, cancel := context.WithDeadline(asReviewer(t.Context()), time.Now().Add(-time.Second))
	defer cancel()
	_, err := f.svc.Search(ctx, audit.Query{})
	wantKind(t, err, errs.Timeout)
}

func TestSearch_Concurrent_IsSafe(t *testing.T) {
	f := readerFx(t)
	var wg sync.WaitGroup
	errc := make(chan error, 16)
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.svc.Search(asReviewer(ctxT(t)), audit.Query{})
			errc <- err
		}()
	}
	wg.Wait()
	close(errc)
	for err := range errc {
		wantNoErr(t, err)
	}
	if n := len(f.recorded("audit.searched")); n != 16 {
		t.Errorf("audit.searched entries = %d, want 16", n)
	}
}

// --- Export -------------------------------------------------------------------------------

type countingWriter struct {
	buf bytes.Buffer
	n   int
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.n++
	return w.buf.Write(p) //nolint:wrapcheck // test double
}

func TestExport_ZeroPerson_ReturnsInvalid(t *testing.T) {
	f := readerFx(t)
	var w countingWriter
	n, err := f.svc.Export(asReviewer(ctxT(t)), model.PersonID{}, &w)
	wantKind(t, err, errs.Invalid)
	wantField(t, err, "person")
	if n != 0 || w.n != 0 {
		t.Errorf("wrote %d lines / %d writes", n, w.n)
	}
	if len(f.recorded("audit.exported")) != 0 {
		t.Error("an invalid export was recorded")
	}
}

func TestExport_NoPermission_ReturnsPermission(t *testing.T) {
	var w countingWriter
	t.Run("no actor", func(t *testing.T) {
		f := readerFx(t)
		_, err := f.svc.Export(ctxT(t), bob, &w)
		wantKind(t, err, errs.Unauthenticated)
	})
	t.Run("nothing granted", func(t *testing.T) {
		f := newFx(t)
		_, err := f.svc.Export(asReviewer(ctxT(t)), bob, &w)
		wantKind(t, err, errs.Permission)
		if len(f.recorded("audit.exported")) != 0 || f.db.queryCount() != 0 {
			t.Error("a refused export recorded or read")
		}
	})
	t.Run("search permission is not enough", func(t *testing.T) {
		f := newFx(t)
		f.authz.Grant(carol, auditlog.PermissionSearch)
		_, err := f.svc.Export(asReviewer(ctxT(t)), bob, &w)
		wantKind(t, err, errs.Permission)
	})
	if w.n != 0 {
		t.Error("a refused export wrote")
	}
}

func TestExport_Allowed_PersonWithPermission_Succeeds(t *testing.T) {
	f := newFx(t)
	f.authz.Grant(carol, auditlog.PermissionExport)
	_, err := f.svc.Export(asReviewer(ctxT(t)), bob, io.Discard)
	wantNoErr(t, err)
}

func TestExport_Allowed_RecordsAuditExportedExactlyOnce(t *testing.T) {
	f := readerFx(t)
	var w countingWriter
	n, err := f.svc.Export(asReviewer(ctxT(t)), bob, &w)
	wantNoErr(t, err)
	if n != 0 || w.buf.Len() != 0 {
		t.Errorf("exported %d / %d bytes from an empty store", n, w.buf.Len())
	}
	if c := len(f.recorded("audit.exported")); c != 1 {
		t.Fatalf("audit.exported entries = %d, want 1", c)
	}
	e := f.recorded("audit.exported")[0]
	if e.Outcome != audit.Succeeded || e.Subject != bob {
		t.Errorf("entry = %+v, want succeeded about bob", e)
	}
}

func TestExport_SuperuserActor_Succeeds_LeavesActorToTheRecorder(t *testing.T) {
	f := newFx(t)
	f.authz.Superuser(carol)
	_, err := f.svc.Export(withSession(ctxT(t), superuser(carol)), bob, io.Discard)
	wantNoErr(t, err)
	got := f.recorded("audit.exported")
	if len(got) != 1 || got[0].Actor.Kind != 0 {
		t.Errorf("entries = %+v: the entry must name no actor", got)
	}
}

func TestExport_RecordFails_WritesNothing(t *testing.T) {
	f := readerFx(t)
	f.rec.Fail(errs.E("test", errs.Unavailable, errors.New("down")))
	var w countingWriter
	_, err := f.svc.Export(asReviewer(ctxT(t)), bob, &w)
	wantKind(t, err, errs.Unavailable)
	if w.n != 0 {
		t.Errorf("export wrote %d times before its entry was recorded", w.n)
	}
	if f.db.queryCount() != 0 {
		t.Error("rows were read before the export was recorded")
	}
}

func TestExport_CtxDone(t *testing.T) {
	f := readerFx(t)
	ctx, cancel := context.WithCancel(asReviewer(t.Context()))
	cancel()
	_, err := f.svc.Export(ctx, bob, io.Discard)
	wantKind(t, err, errs.Canceled)
}

// --- CountAbout ---------------------------------------------------------------------------

func TestCountAbout_ZeroPerson_ReturnsInvalid(t *testing.T) {
	f := newFx(t)
	_, err := f.svc.CountAbout(ctxT(t), model.PersonID{})
	wantKind(t, err, errs.Invalid)
}

func TestCountAbout_CtxDone(t *testing.T) {
	f := newFx(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := f.svc.CountAbout(ctx, bob)
	wantKind(t, err, errs.Canceled)
}
