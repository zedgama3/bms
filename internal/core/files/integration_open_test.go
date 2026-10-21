//go:build integration

//nolint:wrapcheck // test code: errors come straight from the fake or store under test; fixtures do bounded arithmetic
package files_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/objects"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

func readAll(t testing.TB, c files.Content) []byte {
	t.Helper()
	b, err := io.ReadAll(c)
	if err != nil {
		t.Fatalf("read content: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("close content: %v", err)
	}
	return b
}

// Decisions 10 and 11: who must be present and when a read is audited.
func TestOpen_LevelsActorsAndAudit(t *testing.T) {
	subject, other := newPerson(), newPerson()
	type who int
	const (
		nobody who = iota
		theSubject
		someoneElse
		aSuperuser
	)
	tests := []struct {
		name      string
		level     model.Sensitivity
		who       who
		wantKind  errs.Kind // 0 = success
		wantAudit int
	}{
		{"public no actor", model.SensitivityPublic, nobody, 0, 0},
		{"public other", model.SensitivityPublic, someoneElse, 0, 0},
		{"internal no actor", model.SensitivityInternal, nobody, 0, 0},
		{"internal other", model.SensitivityInternal, someoneElse, 0, 0},
		{"personal no actor", model.SensitivityPersonal, nobody, errs.Unauthenticated, 0},
		{"personal the subject", model.SensitivityPersonal, theSubject, 0, 0},
		{"personal someone else", model.SensitivityPersonal, someoneElse, 0, 1},
		{"personal superuser", model.SensitivityPersonal, aSuperuser, 0, 1},
		{"sensitive no actor", model.SensitivitySensitive, nobody, errs.Unauthenticated, 0},
		{"sensitive the subject", model.SensitivitySensitive, theSubject, 0, 1},
		{"sensitive someone else", model.SensitivitySensitive, someoneElse, 0, 1},
		{"sensitive superuser", model.SensitivitySensitive, aSuperuser, 0, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newIRig(t)
			// The people exist so the row's foreign key to core.person holds.
			for _, p := range []model.PersonID{subject, other} {
				if _, err := r.pool.Exec(ctxT(t), `INSERT INTO core.person (id, playa_name, legal_name, created_at, updated_at) VALUES ($1, 'P', '', now(), now()) ON CONFLICT DO NOTHING`, [16]byte(p)); err != nil {
					t.Fatal(err)
				}
			}
			body := pdfDoc("% " + tc.name + "\n")
			f := r.upload(ctxT(t), r.vol, "c.pdf", files.Documents, body, files.Attachment{Record: qualRef(), Subject: subject, Level: tc.level})
			before := len(r.audit.Recorded())

			ctx := ctxT(t)
			switch tc.who {
			case theSubject:
				ctx = asPerson(ctx, subject)
			case someoneElse:
				ctx = asPerson(ctx, other)
			case aSuperuser:
				ctx = asSuperuser(ctx, other)
			}
			got, c, err := r.vol.Open(ctx, f.ID)
			if tc.wantKind != 0 {
				wantKind(t, err, tc.wantKind)
				if c != nil {
					t.Error("content returned with an error")
				}
				if n := len(entriesWith(r.audit, "files.file.read")); n != 0 {
					t.Errorf("read audited %d times although nothing was read", n)
				}
				return
			}
			wantNoErr(t, err)
			if got.ID != f.ID {
				t.Errorf("File.ID = %s", got.ID)
			}
			if !bytes.Equal(readAll(t, c), body) {
				t.Error("content differs")
			}
			reads := entriesWith(r.audit, "files.file.read")
			if len(reads) != tc.wantAudit {
				t.Fatalf("files.file.read entries = %d, want %d (recorded %d new entries)", len(reads), tc.wantAudit, len(r.audit.Recorded())-before)
			}
			if tc.wantAudit == 1 {
				e := reads[0]
				if len(e.Fields) != 1 || e.Fields[0] != "content" {
					t.Errorf("Fields = %v, want [content]", e.Fields)
				}
				if e.Level != tc.level || e.Subject != subject {
					t.Errorf("entry level/subject = %v/%v", e.Level, e.Subject)
				}
				if e.Reason == "" {
					t.Error("Reason is empty; it names the owning record")
				}
			}
		})
	}
}

// F28 for a file with no subject: still needs an actor at personal/sensitive.
func TestOpen_SensitiveNoActor_ReturnsUnauthenticated(t *testing.T) {
	r := newIRig(t)
	f := r.uploadPDF(ctxT(t), r.vol, qualRef(), model.PersonID{}, model.SensitivitySensitive, "")
	_, c, err := r.vol.Open(ctxT(t), f.ID)
	wantKind(t, err, errs.Unauthenticated)
	if c != nil {
		t.Error("bytes opened for no actor")
	}
	if r.count(`SELECT count(*) FROM core.file`) != 1 {
		t.Error("the file disappeared")
	}
}

// F29: audit before the bytes; a failing Record fails the read.
func TestOpen_AuditFails_ReturnsErrorNoContent(t *testing.T) {
	r := newIRig(t)
	subject := r.person()
	f := r.uploadPDF(ctxT(t), r.vol, qualRef(), subject, model.SensitivitySensitive, "")
	boom := errs.E("test.audit", errs.Unavailable, errors.New("audit store down"))
	r.audit.Fail(boom)
	_, c, err := r.vol.Open(asPerson(ctxT(t), r.person()), f.ID)
	wantKind(t, err, errs.Unavailable)
	if c != nil {
		t.Error("content returned although the read could not be audited")
	}
	r.audit.Fail(nil)
	if n := len(entriesWith(r.audit, "files.file.read")); n != 0 {
		t.Errorf("a failed Record left %d entries", n)
	}
}

func TestOpen_AuditRecordedBeforeContentIsReturned_ObjectMissingStillAudited(t *testing.T) {
	r := newIRig(t)
	f := r.uploadPDF(ctxT(t), r.vol, qualRef(), r.person(), model.SensitivitySensitive, "")
	wantNoErr(t, r.inner.Delete(ctxT(t), ownerVol, objects.Key(f.ID.String())))
	_, _, err := r.vol.Open(asPerson(ctxT(t), r.person()), f.ID)
	wantKind(t, err, errs.Unavailable)
	if n := len(entriesWith(r.audit, "files.file.read")); n != 1 {
		t.Errorf("read attempts audited = %d, want 1 (the attempt is still a read attempt)", n)
	}
}

// F30 and decision 17: a row whose object is gone is reported, never deleted.
func TestOpen_ObjectMissing_ReturnsUnavailableSentinel(t *testing.T) {
	eachStore(t, func(t *testing.T, mk func(*testing.T) objects.Store) {
		r := newIRig(t, withInner(mk(t)))
		f := r.uploadPDF(ctxT(t), r.vol, qualRef(), model.PersonID{}, model.SensitivityInternal, "")
		wantNoErr(t, r.inner.Delete(ctxT(t), ownerVol, objects.Key(f.ID.String())))

		_, c, err := r.vol.Open(ctxT(t), f.ID)
		wantKind(t, err, errs.Unavailable)
		if !errors.Is(err, objects.ErrMissing) {
			t.Errorf("err %v does not wrap objects.ErrMissing", err)
		}
		if c != nil {
			t.Error("content returned for a missing object")
		}
		if r.rows() != 1 || r.tombstones() != 0 {
			t.Errorf("rows=%d tombstones=%d; a missing object must never delete the row (decision 17)", r.rows(), r.tombstones())
		}
		if _, err := r.vol.Get(ctxT(t), f.ID); err != nil {
			t.Errorf("metadata read failed although only the bytes are missing: %v", err)
		}
		list, err := r.vol.ByRecord(ctxT(t), f.Record)
		if err != nil || len(list) != 1 {
			t.Errorf("ByRecord = %d files, %v; the copy must still know the file exists", len(list), err)
		}
	})
}

// F31
func TestOpen_OtherStore_ReturnsUnavailable(t *testing.T) {
	r := newIRig(t)
	r.store.kind = objects.Disk
	f := r.uploadPDF(ctxT(t), r.vol, qualRef(), model.PersonID{}, model.SensitivityInternal, "")
	// A process configured for a different store sees the row but cannot serve the bytes.
	other := newFault(r.inner)
	other.kind = objects.S3
	o := r.opts
	o.Objects = other
	svc, err := files.New(o)
	wantNoErr(t, err)
	st, err := svc.Scoped(ownerVol)
	wantNoErr(t, err)
	_, c, err := st.Open(ctxT(t), f.ID)
	wantKind(t, err, errs.Unavailable)
	if c != nil {
		t.Error("content returned from a store this process is not configured for")
	}
	if other.count("get") != 0 {
		t.Error("the wrong store was asked for the object")
	}
	if _, err := st.Get(ctxT(t), f.ID); err != nil {
		t.Errorf("metadata should still read: %v", err)
	}
}

// F32 and "Store unreachable".
func TestOpen_StoreUnreachable_ReturnsUnavailable(t *testing.T) {
	r := newIRig(t)
	subject := r.person()
	f := r.uploadPDF(ctxT(t), r.vol, qualRef(), subject, model.SensitivitySensitive, "")
	r.store.down()

	_, c, err := r.vol.Open(asPerson(ctxT(t), r.person()), f.ID)
	wantKind(t, err, errs.Unavailable)
	if errors.Is(err, objects.ErrMissing) {
		t.Error("an unreachable store must not look like a missing object")
	}
	if c != nil {
		t.Error("content returned")
	}
	if n := len(entriesWith(r.audit, "files.file.read")); n != 1 {
		t.Errorf("audit entries = %d, want 1 (the attempt is audited)", n)
	}
	// Metadata keeps working while the store is down.
	if _, err := r.vol.Get(ctxT(t), f.ID); err != nil {
		t.Errorf("Get: %v", err)
	}
	if l, err := r.vol.ByRecord(ctxT(t), f.Record); err != nil || len(l) != 1 {
		t.Errorf("ByRecord: %d, %v", len(l), err)
	}
	// Stage fails Unavailable before anything is written.
	_, err = stageBytes(ctxT(t), r.vol, "x.pdf", files.Documents, pdfDoc("x\n"))
	wantKind(t, err, errs.Unavailable)
	r.wantNoTempFiles()
	// Export refuses to ship an incomplete document; Ping reports it.
	_, err = r.svc.ExportPerson(ctxT(t), subject)
	wantKind(t, err, errs.Unavailable)
	wantKind(t, r.svc.Ping(ctxT(t)), errs.Unavailable)
	wantNoErr(t, r.svc.Start(ctxT(t)))
	// Reap and the sweep fail Unavailable (jobs retries them).
	_, err = r.svc.SweepOrphans(ctxT(t))
	wantKind(t, err, errs.Unavailable)
	// Back up: everything works again.
	r.store.up()
	_, c, err = r.vol.Open(asPerson(ctxT(t), r.person()), f.ID)
	wantNoErr(t, err)
	wantNoErr(t, c.Close())
}

// F33
func TestOpen_InReadTx_ReturnsInternal(t *testing.T) {
	r := newIRig(t)
	f := r.uploadPDF(ctxT(t), r.vol, qualRef(), r.person(), model.SensitivitySensitive, "")
	err := r.pool.Transact(asPerson(ctxT(t), r.person()), db.Read, func(ctx context.Context) error {
		_, c, oerr := r.vol.Open(ctx, f.ID)
		if c != nil {
			_ = c.Close() //nolint:errcheck,gosec // test
		}
		return oerr
	})
	wantKind(t, err, errs.Internal)
}

func TestOpen_ContentIsSeekableForRanges(t *testing.T) {
	eachStore(t, func(t *testing.T, mk func(*testing.T) objects.Store) {
		r := newIRig(t, withInner(mk(t)))
		body := pdfOfSize(t, 200<<10)
		f := r.upload(ctxT(t), r.vol, "big.pdf", files.Documents, body, files.Attachment{Record: qualRef(), Level: model.SensitivityInternal})
		_, c, err := r.vol.Open(ctxT(t), f.ID)
		wantNoErr(t, err)
		defer c.Close() //nolint:errcheck,gosec // test
		if pos, err := c.Seek(1000, io.SeekStart); err != nil || pos != 1000 {
			t.Fatalf("Seek = %d, %v", pos, err)
		}
		buf := make([]byte, 500)
		if _, err := io.ReadFull(c, buf); err != nil || !bytes.Equal(buf, body[1000:1500]) {
			t.Errorf("ranged read differs (%v)", err)
		}
	})
}

// Open then Serve, the owner's content route end to end.
func TestOpenServe_EndToEnd(t *testing.T) {
	r := newIRig(t)
	body := pdfDoc("% route\n")
	subject := r.person()
	f := r.upload(ctxT(t), r.vol, nameMarker+".pdf", files.Documents, body, files.Attachment{Record: qualRef(), Subject: subject, Level: model.SensitivityPersonal})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/volunteers/cert", nil)
	ctx := asPerson(req.Context(), r.person())
	file, c, err := r.vol.Open(ctx, f.ID)
	wantNoErr(t, err)
	defer c.Close() //nolint:errcheck,gosec // test
	rec := httptest.NewRecorder()
	files.Serve(rec, req.WithContext(ctx), file, c)
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), body) {
		t.Fatalf("status %d, %d bytes", rec.Code, rec.Body.Len())
	}
	if rec.Header().Get("Cache-Control") != "" {
		t.Errorf("Cache-Control = %q; Serve sets none (decision 25)", rec.Header().Get("Cache-Control"))
	}
	if rec.Header().Get("Content-Type") != "application/pdf" {
		t.Errorf("Content-Type = %q", rec.Header().Get("Content-Type"))
	}
	if n := len(entriesWith(r.audit, "files.file.read")); n != 1 {
		t.Errorf("audit entries = %d, want 1 (someone other than the subject read a personal file)", n)
	}
}

// Read-only and on-site copies: every read works, writes fail Unavailable wrapping ErrReadOnly.
func TestReadOnlyCopy_ReadsWorkAndWritesFail(t *testing.T) {
	r := newIRig(t)
	subject := r.person()
	rec := qualRef()
	f := r.uploadPDF(ctxT(t), r.vol, rec, subject, model.SensitivityPersonal, "")
	ro := r.readOnlyCopy()
	vol, err := ro.Scoped(ownerVol)
	wantNoErr(t, err)

	got, err := vol.Get(ctxT(t), f.ID)
	wantNoErr(t, err)
	if got.ID != f.ID {
		t.Error("Get on a read-only copy returned the wrong file")
	}
	list, err := vol.ByRecord(ctxT(t), rec)
	if err != nil || len(list) != 1 {
		t.Errorf("ByRecord = %d, %v", len(list), err)
	}
	// A lead can still view a certificate on site; the audit entry is recorded (spooled by
	// the real recorder).
	_, c, err := vol.Open(asPerson(ctxT(t), r.person()), f.ID)
	wantNoErr(t, err)
	wantNoErr(t, c.Close())
	if n := len(entriesWith(r.audit, "files.file.read")); n != 1 {
		t.Errorf("read audited %d times on the read-only copy", n)
	}

	// Writes.
	inTx := func(fn func(ctx context.Context) error) error {
		return r.pool.Transact(ctxT(t), db.Write, fn)
	}
	wantReadOnly(t, inTx(func(ctx context.Context) error { return vol.Delete(ctx, f.ID) }))
	wantReadOnly(t, inTx(func(ctx context.Context) error { _, err := vol.DeleteByRecord(ctx, rec); return err }))
	wantReadOnly(t, inTx(func(ctx context.Context) error { _, err := ro.DeletePerson(ctx, subject); return err }))
	_, err = ro.Reap(ctxT(t))
	wantReadOnly(t, err)
	_, err = ro.SweepOrphans(ctxT(t))
	wantReadOnly(t, err)
	if r.rows() != 1 || r.tombstones() != 0 {
		t.Errorf("rows=%d tombstones=%d; the read-only copy changed data", r.rows(), r.tombstones())
	}
}
