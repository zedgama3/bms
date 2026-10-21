//go:build integration

//nolint:gosec,wrapcheck // test code: errors come straight from the fake or store under test; fixtures do bounded arithmetic
package files_test

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/objects"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

func TestAttach_HappyPath_RowAuditAndObject(t *testing.T) {
	eachStore(t, func(t *testing.T, mk func(*testing.T) objects.Store) {
		r := newIRig(t, withInner(mk(t)))
		uploader, subject := r.person(), r.person()
		ctx := asPerson(ctxT(t), uploader)
		rec := qualRef()
		body := pdfDoc("")
		st := mustStage(t, r.vol, "Certificate.PDF", files.Documents, body)
		f, err := r.attach(ctx, r.vol, st, files.Attachment{Record: rec, Subject: subject, Level: model.SensitivityPersonal})
		wantNoErr(t, err)

		if err := f.Validate(); err != nil {
			t.Errorf("File.Validate: %v", err)
		}
		if f.ID.String() != string(st.Key) {
			t.Errorf("File.ID = %s, object key = %s; the key is the file ID (decision 5)", f.ID, st.Key)
		}
		if f.Record != rec || f.Subject != subject || f.UploadedBy != uploader || f.Level != model.SensitivityPersonal {
			t.Errorf("File = %+v", f)
		}
		if f.Name != "Certificate.pdf" || f.ContentType != "application/pdf" || f.Size != int64(len(body)) || f.SHA256 != sha256.Sum256(body) || f.Stripped {
			t.Errorf("File = %+v", f)
		}
		if !f.CreatedAt.Equal(r.clk.Now()) {
			t.Errorf("CreatedAt = %v, want the clock's %v", f.CreatedAt, r.clk.Now())
		}
		got, err := r.vol.Get(ctx, f.ID)
		wantNoErr(t, err)
		if got.ID != f.ID || got.Record != f.Record || got.Subject != f.Subject || got.UploadedBy != f.UploadedBy ||
			got.Level != f.Level || got.Name != f.Name || got.Size != f.Size || got.SHA256 != f.SHA256 {
			t.Errorf("Get = %+v, want %+v", got, f)
		}

		es := entriesWith(r.audit, "files.file.created")
		if len(es) != 1 {
			t.Fatalf("files.file.created entries = %d, want 1", len(es))
		}
		e := es[0]
		if e.Subject != subject || e.Level != model.SensitivityPersonal || e.Record.Kind != "files.file" || e.Record.ID != [16]byte(f.ID) {
			t.Errorf("audit entry = %+v", e)
		}
		if want := "record:" + kindQual + ":" + model.ID[qualTag](rec.ID).String(); e.Reason != want {
			t.Errorf("Reason = %q, want %q", e.Reason, want)
		}
		if r.count(`SELECT count(*) FROM core.file WHERE owner = $1 AND bucket = $1 AND object_key = $2 AND record_kind = $3`, ownerVol, f.ID.String(), kindQual) != 1 {
			t.Error("core.file row does not carry owner/bucket/object_key/record_kind as specified")
		}
		if r.tombstones() != 0 {
			t.Error("Attach wrote a tombstone")
		}
	})
}

func TestAttach_NoActor_UploadedByIsZero(t *testing.T) { // an operator command
	r := newIRig(t)
	f := r.upload(ctxT(t), r.vol, "a.png", files.Images, plainPNG(t), files.Attachment{Record: qualRef(), Level: model.SensitivityInternal})
	if !f.UploadedBy.IsZero() || !f.Subject.IsZero() {
		t.Errorf("UploadedBy/Subject = %v/%v, want zero", f.UploadedBy, f.Subject)
	}
	got, err := r.vol.Get(ctxT(t), f.ID)
	wantNoErr(t, err)
	if !got.UploadedBy.IsZero() {
		t.Error("Get: UploadedBy not zero")
	}
}

func TestAttach_Image_RecordsStripped(t *testing.T) {
	r := newIRig(t)
	f := r.upload(ctxT(t), r.vol, "p.png", files.Images, pngWithMetadata(t), files.Attachment{Record: qualRef(), Level: model.SensitivityInternal})
	if !f.Stripped {
		t.Error("File.Stripped = false for a stripped image")
	}
	if r.count(`SELECT count(*) FROM core.file WHERE id = $1 AND stripped`, [16]byte(f.ID)) != 1 {
		t.Error("the stripped flag was not stored")
	}
}

// D43: no bytes in PostgreSQL.
func TestSchema_NoFileContentInTheDatabase(t *testing.T) {
	r := newIRig(t)
	rows, err := r.pool.Query(ctxT(t), `SELECT table_name, column_name FROM information_schema.columns
		WHERE table_schema = 'core' AND table_name IN ('file', 'file_tombstone') AND data_type = 'bytea'`)
	wantNoErr(t, err)
	defer rows.Close()
	for rows.Next() {
		var table, col string
		wantNoErr(t, rows.Scan(&table, &col))
		if table != "file" || col != "sha256" {
			t.Errorf("bytea column %s.%s: bytes must never live in the database (D43)", table, col)
		}
	}
	wantNoErr(t, rows.Err())
	var oidLike int
	wantNoErr(t, r.pool.QueryRow(ctxT(t), `SELECT count(*) FROM pg_largeobject_metadata`).Scan(&oidLike))
	if oidLike != 0 {
		t.Errorf("large objects present: %d", oidLike)
	}
}

func TestSchema_TablesRejectBadRows(t *testing.T) {
	r := newIRig(t)
	good := func() []any {
		id := model.NewID[files.FileID]()
		rec := qualRef()
		return []any{[16]byte(id), ownerVol, kindQual, rec.ID, 3, "a.pdf", "application/pdf", 10, make([]byte, 32), "disk", ownerVol, id.String(), false, time.Now().UTC()}
	}
	const ins = `INSERT INTO core.file (id, owner, record_kind, record_id, level, name, content_type, size_bytes, sha256, store, bucket, object_key, stripped, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`
	if _, err := r.pool.Exec(ctxT(t), ins, good()...); err != nil {
		t.Fatalf("a good row was refused: %v", err)
	}
	mutations := map[string]func([]any){
		"level 0":          func(a []any) { a[4] = 0 },
		"level 5":          func(a []any) { a[4] = 5 },
		"size 0":           func(a []any) { a[7] = 0 },
		"sha256 too short": func(a []any) { a[8] = make([]byte, 31) },
		"name with slash":  func(a []any) { a[5] = "a/b.pdf" },
		"name with nul":    func(a []any) { a[5] = "a\x01b.pdf" },
	}
	for name, mut := range mutations {
		t.Run(name, func(t *testing.T) {
			a := good()
			mut(a)
			if _, err := r.pool.Exec(ctxT(t), ins, a...); err == nil {
				t.Error("the database accepted a row the spec's constraints forbid")
			}
		})
	}
	t.Run("duplicate object key", func(t *testing.T) {
		a := good()
		if _, err := r.pool.Exec(ctxT(t), ins, a...); err != nil {
			t.Fatal(err)
		}
		b := good()
		b[9], b[10], b[11] = a[9], a[10], a[11]
		if _, err := r.pool.Exec(ctxT(t), ins, b...); err == nil {
			t.Error("unique (store, bucket, object_key) not enforced")
		}
	})
}

// F13
func TestAttach_NoWriteTx_ReturnsInternal(t *testing.T) {
	r := newIRig(t)
	st := mustStage(t, r.vol, "a.png", files.Images, plainPNG(t))
	a := files.Attachment{Record: qualRef(), Level: model.SensitivityInternal}
	_, err := r.vol.Attach(ctxT(t), st, a)
	wantKind(t, err, errs.Internal)
	err = r.pool.Transact(ctxT(t), db.Read, func(ctx context.Context) error {
		_, aerr := r.vol.Attach(ctx, st, a)
		return aerr
	})
	wantKind(t, err, errs.Internal)
	if r.rows() != 0 {
		t.Error("a row was written outside a Write transaction")
	}
}

// F14
func TestAttach_ForeignRecord_ReturnsInvalidScope(t *testing.T) {
	r := newIRig(t)
	st := mustStage(t, r.vol, "a.png", files.Images, plainPNG(t))
	_, err := r.attach(ctxT(t), r.vol, st, files.Attachment{Record: photoRef(), Level: model.SensitivityInternal})
	wantField(t, err, errs.Invalid, "record", "scope")
	if r.rows() != 0 {
		t.Error("a foreign-area record got a row")
	}
}

// F15
func TestAttach_BadAttachment_ReturnsInvalid(t *testing.T) {
	r := newIRig(t)
	st := mustStage(t, r.vol, "a.png", files.Images, plainPNG(t))
	tests := []struct {
		name        string
		a           files.Attachment
		field, code string
	}{
		{"zero record", files.Attachment{Level: model.SensitivityInternal}, "record", "required"},
		{"malformed record", files.Attachment{Record: qualRef(), Level: 0}, "level", "one_of"},
		{"bad level", files.Attachment{Record: qualRef(), Level: 9}, "level", "one_of"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := r.attach(ctxT(t), r.vol, st, tc.a)
			wantField(t, err, errs.Invalid, tc.field, tc.code)
		})
	}
	if r.rows() != 0 {
		t.Error("a row was written for an invalid attachment")
	}
}

// F16
func TestAttach_ForeignStaged_ReturnsInvalid(t *testing.T) {
	r := newIRig(t)
	st := mustStage(t, r.inc, "a.png", files.Images, plainPNG(t)) // staged for incidents
	_, err := r.attach(ctxT(t), r.vol, st, files.Attachment{Record: qualRef(), Level: model.SensitivityInternal})
	wantField(t, err, errs.Invalid, "staged", "foreign")
	if r.rows() != 0 {
		t.Error("a row was written for another owner's Staged")
	}
}

func TestAttach_ExpiredStaged_ReturnsInvalid(t *testing.T) { // F16
	r := newIRig(t)
	st := mustStage(t, r.vol, "a.png", files.Images, plainPNG(t))
	r.clk.Advance(files.OrphanAge + time.Second) //nolint:errcheck,gosec // fake clock
	_, err := r.attach(ctxT(t), r.vol, st, files.Attachment{Record: qualRef(), Level: model.SensitivityInternal})
	wantField(t, err, errs.Invalid, "staged", "expired")
	if r.rows() != 0 {
		t.Error("a row was written for an expired Staged")
	}
}

func TestAttach_JustBeforeOrphanAge_Succeeds(t *testing.T) {
	r := newIRig(t)
	st := mustStage(t, r.vol, "a.png", files.Images, plainPNG(t))
	r.clk.Advance(files.OrphanAge - time.Minute) //nolint:errcheck,gosec // fake clock
	_, err := r.attach(ctxT(t), r.vol, st, files.Attachment{Record: qualRef(), Level: model.SensitivityInternal})
	wantNoErr(t, err)
}

// files.md F16 names only a foreign bucket and an expired Staged; the contract table lists
// "st from this owner's Stage within OrphanAge" as a precondition, and review decision 6
// forbids store I/O while the transaction is open. So Attach cannot tell a Staged whose
// object was never put (a well-formed key nobody staged) from a real one: that was an
// object-store Head, now removed. What it must still refuse without I/O is a Staged that is
// not even well formed: the zero value, or a key that is not a file ID.
func TestAttach_ForgedStaged_ReturnsInvalid(t *testing.T) {
	r := newIRig(t)
	st := mustStage(t, r.vol, "a.png", files.Images, plainPNG(t))
	a := files.Attachment{Record: qualRef(), Level: model.SensitivityInternal}

	_, err := r.attach(ctxT(t), r.vol, files.Staged{}, a)
	wantKind(t, err, errs.Invalid)

	bad := st
	bad.Key = "not-a-file-id"
	_, err = r.attach(ctxT(t), r.vol, bad, a)
	wantKind(t, err, errs.Invalid)

	if r.rows() != 0 {
		t.Error("a row exists for a Staged that was not well formed")
	}
}

// F17
func TestAttach_Twice_ReturnsInternal(t *testing.T) {
	r := newIRig(t)
	st := mustStage(t, r.vol, "a.png", files.Images, plainPNG(t))
	a := files.Attachment{Record: qualRef(), Level: model.SensitivityInternal}
	_, err := r.attach(ctxT(t), r.vol, st, a)
	wantNoErr(t, err)
	_, err = r.attach(ctxT(t), r.vol, st, files.Attachment{Record: qualRef(), Level: model.SensitivityInternal})
	wantKind(t, err, errs.Internal)
	if r.rows() != 1 {
		t.Errorf("rows = %d, want 1", r.rows())
	}
	if len(r.objectKeys(ownerVol)) != 1 {
		t.Error("the object was lost")
	}
}

// F18
func TestAttach_RecordFull_ReturnsConflictTooMany(t *testing.T) {
	r := newIRig(t, withPolicy(files.Policy{MaxBytes: 1 << 20, MaxPerRecord: 2}))
	rec := qualRef()
	at := files.Attachment{Record: rec, Level: model.SensitivityInternal}
	r.upload(ctxT(t), r.vol, "1.png", files.Images, plainPNG(t), at)
	r.upload(ctxT(t), r.vol, "2.png", files.Images, plainPNG(t), at)
	st := mustStage(t, r.vol, "3.png", files.Images, plainPNG(t))
	_, err := r.attach(ctxT(t), r.vol, st, at)
	fe := codeErr(t, err, errs.Conflict, "too_many")
	if fe.Params["max"] != "2" {
		t.Errorf("max param = %q, want 2", fe.Params["max"])
	}
	if r.rows() != 2 {
		t.Errorf("rows = %d, want 2", r.rows())
	}
	// Another record has its own count.
	r.upload(ctxT(t), r.vol, "x.png", files.Images, plainPNG(t), files.Attachment{Record: qualRef(), Level: model.SensitivityInternal})
	// Replace makes room: the old files go in the same transaction.
	at.Replace = true
	f, err := r.attach(ctxT(t), r.vol, st, at)
	wantNoErr(t, err)
	ids, err := r.vol.ByRecord(ctxT(t), rec)
	wantNoErr(t, err)
	if len(ids) != 1 || ids[0].ID != f.ID {
		t.Errorf("after Replace the record holds %d files, want only the new one", len(ids))
	}
	if r.tombstones() != 0 {
		// the AfterCommit removed the two replaced objects and their tombstones
		t.Errorf("tombstones outstanding = %d after a clean commit", r.tombstones())
	}
	if got := len(r.objectKeys(ownerVol)); got != 2 { // the new file and "x.png" of the other record
		t.Errorf("objects = %d, want 2 (replaced objects deleted after commit)", got)
	}
}

// F19
func TestAttach_OverQuota_ReturnsConflictQuota(t *testing.T) {
	pdf1, pdf2 := pdfOfSize(t, 3000), pdfOfSize(t, 3000)
	r := newIRig(t, withPolicy(files.Policy{MaxBytes: 1 << 20, QuotaBytes: 5999, MaxPerRecord: 20}))
	at := files.Attachment{Record: qualRef(), Level: model.SensitivityInternal}
	r.upload(ctxT(t), r.vol, "1.pdf", files.Documents, pdf1, at)
	st := mustStage(t, r.vol, "2.pdf", files.Documents, pdf2)
	_, err := r.attach(ctxT(t), r.vol, st, at)
	fe := codeErr(t, err, errs.Conflict, "quota")
	if fe.Params["max"] != "5999" {
		t.Errorf("max param = %q, want 5999", fe.Params["max"])
	}
	if r.rows() != 1 {
		t.Errorf("rows = %d, want 1", r.rows())
	}
	// Exactly at the quota is allowed.
	r.policy.set(files.Policy{MaxBytes: 1 << 20, QuotaBytes: 6000, MaxPerRecord: 20})
	_, err = r.attach(ctxT(t), r.vol, st, at)
	wantNoErr(t, err)
}

// Decision 26: the quota is site-wide, across every owner.
func TestAttach_OverQuota_CountsEveryOwner(t *testing.T) {
	r := newIRig(t, withPolicy(files.Policy{MaxBytes: 1 << 20, QuotaBytes: 4000, MaxPerRecord: 20}))
	r.upload(ctxT(t), r.vol, "1.pdf", files.Documents, pdfOfSize(t, 3000), files.Attachment{Record: qualRef(), Level: model.SensitivityInternal})
	st := mustStage(t, r.inc, "2.pdf", files.Documents, pdfOfSize(t, 1500))
	_, err := r.attach(ctxT(t), r.inc, st, files.Attachment{Record: photoRef(), Level: model.SensitivityPersonal})
	fe := codeErr(t, err, errs.Conflict, "quota")
	if fe.Params["max"] != "4000" {
		t.Errorf("max param = %q", fe.Params["max"])
	}
	// 3000 + 1000 = exactly the quota: allowed.
	st2 := mustStage(t, r.inc, "3.pdf", files.Documents, pdfOfSize(t, 1000))
	_, err = r.attach(ctxT(t), r.inc, st2, files.Attachment{Record: photoRef(), Level: model.SensitivityPersonal})
	wantNoErr(t, err)
	// The Stage pre-check sees the site-wide total too.
	_, err = stageBytes(ctxT(t), r.vol, "4.pdf", files.Documents, pdfDoc(""))
	wantCode(t, err, errs.Conflict, "quota")
}

// A row deleted but not yet reaped no longer counts (decision 26).
func TestQuota_DeletedButNotReaped_NoLongerCounts(t *testing.T) {
	r := newIRig(t, withPolicy(files.Policy{MaxBytes: 1 << 20, QuotaBytes: 3000, MaxPerRecord: 20}))
	f := r.upload(ctxT(t), r.vol, "1.pdf", files.Documents, pdfOfSize(t, 3000), files.Attachment{Record: qualRef(), Level: model.SensitivityInternal})
	r.store.fail("delete", unavailable("delete"))
	wantNoErr(t, r.del(ctxT(t), r.vol, f.ID))
	if r.tombstones() != 1 {
		t.Fatal("expected an outstanding tombstone")
	}
	mustStage(t, r.inc, "2.pdf", files.Documents, pdfOfSize(t, 2000))
}

// F9
func TestStage_AtQuota_ReturnsConflictQuota(t *testing.T) {
	r := newIRig(t, withPolicy(files.Policy{MaxBytes: 1 << 20, QuotaBytes: 3000, MaxPerRecord: 20}))
	at := files.Attachment{Record: qualRef(), Level: model.SensitivityInternal}
	r.upload(ctxT(t), r.vol, "1.pdf", files.Documents, pdfOfSize(t, 3000), at)
	_, err := stageBytes(ctxT(t), r.vol, "2.pdf", files.Documents, pdfDoc(""))
	fe := codeErr(t, err, errs.Conflict, "quota")
	if fe.Params["max"] != "3000" {
		t.Errorf("max param = %q", fe.Params["max"])
	}
	r.wantNoTempFiles()
	if got := len(r.objectKeys(ownerVol)); got != 1 {
		t.Errorf("objects = %d; the advisory pre-check must stop before the Put", got)
	}
	// Unlimited again: works.
	r.policy.set(files.Policy{MaxBytes: 1 << 20, QuotaBytes: 0, MaxPerRecord: 20})
	mustStage(t, r.vol, "2.pdf", files.Documents, pdfDoc(""))
}

func TestQuota_DeleteFreesSpace(t *testing.T) {
	r := newIRig(t, withPolicy(files.Policy{MaxBytes: 1 << 20, QuotaBytes: 3000, MaxPerRecord: 20}))
	rec := qualRef()
	f := r.upload(ctxT(t), r.vol, "1.pdf", files.Documents, pdfOfSize(t, 3000), files.Attachment{Record: rec, Level: model.SensitivityInternal})
	wantNoErr(t, r.del(ctxT(t), r.vol, f.ID))
	st, err := stageBytes(ctxT(t), r.vol, "2.pdf", files.Documents, pdfOfSize(t, 3000))
	wantNoErr(t, err)
	_, err = r.attach(ctxT(t), r.vol, st, files.Attachment{Record: rec, Level: model.SensitivityInternal})
	wantNoErr(t, err)
}

// F20
func TestAttach_UnknownSubject_ReturnsInvalid(t *testing.T) {
	r := newIRig(t)
	st := mustStage(t, r.vol, "a.png", files.Images, plainPNG(t))
	_, err := r.attach(ctxT(t), r.vol, st, files.Attachment{Record: qualRef(), Subject: newPerson(), Level: model.SensitivityPersonal})
	wantField(t, err, errs.Invalid, "subject", "unknown")
	if r.rows() != 0 {
		t.Error("a row was written for an unknown subject")
	}
	// A known subject works with the same Staged: the failed transaction left nothing behind.
	_, err = r.attach(ctxT(t), r.vol, st, files.Attachment{Record: qualRef(), Subject: r.person(), Level: model.SensitivityPersonal})
	wantNoErr(t, err)
}

// F21
func TestAttach_ReadOnly_ReturnsUnavailableReadOnly(t *testing.T) {
	r := newIRig(t)
	ro := r.readOnlyCopy()
	vol, err := ro.Scoped(ownerVol)
	wantNoErr(t, err)
	// Stage works on a read-only copy (no transaction); Attach fails from the row statement.
	st, err := stageBytes(ctxT(t), vol, "a.png", files.Images, plainPNG(t))
	wantNoErr(t, err)
	err = r.pool.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
		_, aerr := vol.Attach(ctx, st, files.Attachment{Record: qualRef(), Level: model.SensitivityInternal})
		return aerr
	})
	wantReadOnly(t, err)
	if r.rows() != 0 {
		t.Error("a row was written on a read-only copy")
	}
	// The handler's deferred Discard removes the object.
	wantNoErr(t, vol.Discard(ctxT(t), st))
	r.wantNoObjects(ownerVol)
}

// F22 and the Stage/Attach rollback orphan: the object stays until the sweep, after OrphanAge.
func TestAttach_AuditFails_RollsBackAndLeavesAnOrphanTheSweepRemoves(t *testing.T) {
	eachStore(t, func(t *testing.T, mk func(*testing.T) objects.Store) {
		r := newIRig(t, withInner(mk(t)))
		st := mustStage(t, r.vol, "a.png", files.Images, plainPNG(t))
		boom := errs.E("test.audit", errs.Unavailable, context.DeadlineExceeded)
		r.audit.Fail(boom)
		_, err := r.attach(ctxT(t), r.vol, st, files.Attachment{Record: qualRef(), Level: model.SensitivityInternal})
		wantKind(t, err, errs.Unavailable)
		r.audit.Fail(nil)
		if r.rows() != 0 {
			t.Fatalf("rows = %d; a failed audit Record must roll the row back", r.rows())
		}
		if len(r.objectKeys(ownerVol)) != 1 {
			t.Fatal("the staged object should still exist as an orphan")
		}

		r.clk.Advance(23 * time.Hour) //nolint:errcheck,gosec // fake clock
		got, err := r.svc.SweepOrphans(ctxT(t))
		wantNoErr(t, err)
		if got.Orphans != 0 || len(r.objectKeys(ownerVol)) != 1 {
			t.Fatalf("before OrphanAge the sweep removed %+v", got)
		}
		r.clk.Advance(2 * time.Hour) //nolint:errcheck,gosec // fake clock
		got, err = r.svc.SweepOrphans(ctxT(t))
		wantNoErr(t, err)
		if got.Orphans != 1 {
			t.Errorf("after OrphanAge swept %+v, want 1 orphan", got)
		}
		r.wantNoObjects(ownerVol)
		if r.rows() != 0 {
			t.Error("the sweep must never create or delete rows")
		}
	})
}

func TestAttach_TxFnFailsAfterAttach_RollsBackAndDiscardRemovesObject(t *testing.T) {
	r := newIRig(t)
	st := mustStage(t, r.vol, "a.png", files.Images, plainPNG(t))
	errAfter := errs.E("test.owner", errs.Conflict, context.Canceled)
	err := r.pool.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
		if _, aerr := r.vol.Attach(ctx, st, files.Attachment{Record: qualRef(), Level: model.SensitivityInternal}); aerr != nil {
			return aerr
		}
		return errAfter // the owner's own record write failed
	})
	if err == nil {
		t.Fatal("transaction succeeded")
	}
	if r.rows() != 0 {
		t.Errorf("rows = %d after rollback", r.rows())
	}
	// "finds no row": Discard after the rolled-back transaction deletes the object.
	wantNoErr(t, r.vol.Discard(ctxT(t), st))
	r.wantNoObjects(ownerVol)
}

// F24
func TestDiscard_AfterAttach_IsNoop(t *testing.T) {
	eachStore(t, func(t *testing.T, mk func(*testing.T) objects.Store) {
		r := newIRig(t, withInner(mk(t)))
		st := mustStage(t, r.vol, "a.pdf", files.Documents, pdfDoc(""))
		f, err := r.attach(ctxT(t), r.vol, st, files.Attachment{Record: qualRef(), Level: model.SensitivityInternal})
		wantNoErr(t, err)
		wantNoErr(t, r.vol.Discard(ctxT(t), st))
		wantNoErr(t, r.vol.Discard(ctxT(t), st))
		if len(r.objectKeys(ownerVol)) != 1 {
			t.Fatal("Discard after Attach removed the attached object")
		}
		_, c, err := r.vol.Open(ctxT(t), f.ID)
		wantNoErr(t, err)
		wantNoErr(t, c.Close())
	})
}

// Ownership (decision 1; F25, F26, F27, F35).
func TestOwnerScoping_OtherOwnersFileIsNotFound(t *testing.T) {
	r := newIRig(t)
	ctx := asPerson(ctxT(t), r.person())
	rec := qualRef()
	vf := r.uploadPDF(ctx, r.vol, rec, model.PersonID{}, model.SensitivityInternal, "")
	if len(r.objectKeys(ownerVol)) != 1 || len(r.objectKeys(ownerInc)) != 0 {
		t.Fatal("a file must be written to its owner's bucket only")
	}

	_, err := r.inc.Get(ctx, vf.ID)
	wantKind(t, err, errs.NotFound)
	_, _, err = r.inc.Open(ctx, vf.ID)
	wantKind(t, err, errs.NotFound)
	wantKind(t, r.del(ctx, r.inc, vf.ID), errs.NotFound)

	_, err = r.inc.ByRecord(ctx, rec) // F26: a ref outside the owner's area
	wantField(t, err, errs.Invalid, "record", "scope")

	if n, err := r.delByRecord(ctx, r.inc, rec); err == nil && n != 0 {
		t.Errorf("incidents deleted %d of volunteers' files", n)
	}
	if _, err := r.vol.Get(ctx, vf.ID); err != nil {
		t.Errorf("the owner lost its file: %v", err)
	}
	if r.rows() != 1 || r.tombstones() != 0 || len(r.objectKeys(ownerVol)) != 1 {
		t.Errorf("rows=%d tombstones=%d objects=%d; another owner's call changed state", r.rows(), r.tombstones(), len(r.objectKeys(ownerVol)))
	}
	// Unknown IDs are NotFound too, not an error that tells ownership apart.
	_, err = r.vol.Get(ctx, model.NewID[files.FileID]())
	wantKind(t, err, errs.NotFound)
	_, _, err = r.vol.Open(ctx, model.NewID[files.FileID]())
	wantKind(t, err, errs.NotFound)
	wantKind(t, r.del(ctx, r.vol, model.NewID[files.FileID]()), errs.NotFound)
}

func TestByRecord_OldestFirstAndEmpty(t *testing.T) {
	r := newIRig(t)
	rec := qualRef()
	empty, err := r.vol.ByRecord(ctxT(t), rec)
	wantNoErr(t, err)
	if len(empty) != 0 {
		t.Errorf("ByRecord of a record with no files = %v", empty)
	}
	var ids []files.FileID
	for i := range 3 {
		f := r.upload(ctxT(t), r.vol, "f.pdf", files.Documents, pdfDoc(string(rune('a'+i))+"\n"), files.Attachment{Record: rec, Level: model.SensitivityInternal})
		ids = append(ids, f.ID)
		r.clk.Advance(time.Minute) //nolint:errcheck,gosec // fake clock
	}
	other := r.upload(ctxT(t), r.vol, "o.pdf", files.Documents, pdfDoc("z\n"), files.Attachment{Record: qualRef(), Level: model.SensitivityInternal})
	got, err := r.vol.ByRecord(ctxT(t), rec)
	wantNoErr(t, err)
	if len(got) != 3 {
		t.Fatalf("ByRecord = %d files, want 3", len(got))
	}
	for i := range got {
		if got[i].ID != ids[i] {
			t.Errorf("position %d = %s, want %s (oldest first)", i, got[i].ID, ids[i])
		}
		if got[i].ID == other.ID {
			t.Error("another record's file listed")
		}
	}
	_, err = r.vol.ByRecord(ctxT(t), audit0())
	if err == nil {
		t.Error("ByRecord(zero ref) = nil error, want Invalid")
	} else {
		wantKind(t, err, errs.Invalid)
	}
}

func TestAttach_ConcurrentDistinctRecords_AllSucceed(t *testing.T) {
	r := newIRig(t)
	const n = 8
	errc := make(chan error, n)
	for i := range n {
		go func() {
			body := pdfDoc(string(rune('a'+i)) + "\n")
			st, err := stageBytes(ctxT(t), r.vol, "c.pdf", files.Documents, body)
			if err != nil {
				errc <- err
				return
			}
			_, err = r.attach(ctxT(t), r.vol, st, files.Attachment{Record: qualRef(), Level: model.SensitivityInternal})
			errc <- err
		}()
	}
	for range n {
		wantNoErr(t, <-errc)
	}
	if r.rows() != n {
		t.Errorf("rows = %d, want %d", r.rows(), n)
	}
}
