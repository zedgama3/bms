//go:build integration

//nolint:wrapcheck // test code: errors come straight from the fake or store under test; fixtures do bounded arithmetic
package files_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/objects"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

type exportDoc struct {
	About    []map[string]any `json:"about"`
	Uploaded []map[string]any `json:"uploaded"`
}

func export(t *testing.T, r *irig, p model.PersonID) exportDoc {
	t.Helper()
	raw, err := r.svc.ExportPerson(ctxT(t), p)
	wantNoErr(t, err)
	var d exportDoc
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("export is not the documented JSON: %v", err)
	}
	return d
}

func TestExportPerson_AboutHasContent_UploadedHasMetadataOnly(t *testing.T) {
	r := newIRig(t)
	me, other := r.person(), r.person()
	body := pdfDoc("% about me\n")
	rec := qualRef()
	about := r.upload(asPerson(ctxT(t), other), r.vol, "my cert.pdf", files.Documents, body,
		files.Attachment{Record: rec, Subject: me, Level: model.SensitivityPersonal})
	// A file I uploaded about someone else, and one I uploaded about nobody.
	aboutOther := r.upload(asPerson(ctxT(t), me), r.vol, "theirs.pdf", files.Documents, pdfDoc("theirs\n"),
		files.Attachment{Record: qualRef(), Subject: other, Level: model.SensitivityPersonal})
	nobody := r.upload(asPerson(ctxT(t), me), r.inc, "scene.png", files.Images, plainPNG(t),
		files.Attachment{Record: photoRef(), Level: model.SensitivityInternal})
	// A stranger's file must not appear.
	r.upload(asPerson(ctxT(t), other), r.vol, "stranger.pdf", files.Documents, pdfDoc("stranger\n"),
		files.Attachment{Record: qualRef(), Subject: other, Level: model.SensitivityPersonal})
	auditBefore := len(r.audit.Recorded())

	d := export(t, r, me)

	if len(d.About) != 1 {
		t.Fatalf("about = %d, want 1", len(d.About))
	}
	a := d.About[0]
	if a["id"] != about.ID.String() || a["name"] != "my cert.pdf" || a["content_type"] != "application/pdf" || a["size"] != float64(len(body)) {
		t.Errorf("about entry = %v", a)
	}
	sum := sha256.Sum256(body)
	if a["sha256"] != hex.EncodeToString(sum[:]) {
		t.Errorf("sha256 = %v, want hex", a["sha256"])
	}
	if rm, ok := a["record"].(map[string]any); !ok || rm["kind"] != kindQual {
		t.Errorf("record = %v", a["record"])
	}
	content, _ := a["content"].(string) //nolint:errcheck // checked below
	dec, err := base64.StdEncoding.DecodeString(content)
	if err != nil || !bytes.Equal(dec, body) {
		t.Errorf("content is not the file's bytes in base64 (%v)", err)
	}
	if a["created_at"] == nil {
		t.Error("created_at missing")
	}

	if len(d.Uploaded) != 2 {
		t.Fatalf("uploaded = %d, want 2", len(d.Uploaded))
	}
	byID := map[string]map[string]any{}
	for _, u := range d.Uploaded {
		byID[u["id"].(string)] = u //nolint:forcetypeassert,errcheck // test
		if _, has := u["content"]; has {
			t.Error("an uploaded entry carries content; metadata only")
		}
		for _, k := range []string{"id", "record", "content_type", "size", "sha256", "created_at"} {
			if _, ok := u[k]; !ok {
				t.Errorf("uploaded entry lacks %s: %v", k, u)
			}
		}
	}
	// Decision 27.
	if u := byID[aboutOther.ID.String()]; u == nil {
		t.Error("the file about someone else is missing from uploaded")
	} else if _, has := u["name"]; has {
		t.Error("uploaded entry about someone else carries name; the key must be absent (decision 27)")
	}
	if u := byID[nobody.ID.String()]; u == nil {
		t.Error("the subjectless file is missing from uploaded")
	} else if u["name"] != "scene.png" {
		t.Errorf("subjectless internal file name = %v, want scene.png", u["name"])
	}
	if len(r.audit.Recorded()) != auditBefore {
		t.Error("ExportPerson recorded audit entries; the caller (people) audits the export")
	}
}

func TestExportPerson_UploadedAboutOther_OmitsName(t *testing.T) {
	r := newIRig(t)
	me, other := r.person(), r.person()
	for _, lvl := range []model.Sensitivity{model.SensitivityPublic, model.SensitivityInternal, model.SensitivityPersonal, model.SensitivitySensitive} {
		r.upload(asPerson(ctxT(t), me), r.vol, nameMarker+".pdf", files.Documents, pdfDoc(lvl.String()+"\n"),
			files.Attachment{Record: qualRef(), Subject: other, Level: lvl})
	}
	d := export(t, r, me)
	if len(d.Uploaded) != 4 {
		t.Fatalf("uploaded = %d, want 4", len(d.Uploaded))
	}
	for _, u := range d.Uploaded {
		if _, has := u["name"]; has {
			t.Errorf("entry %v carries name although the file is about someone else (any level)", u["id"])
		}
	}
	raw, err := r.svc.ExportPerson(ctxT(t), me)
	wantNoErr(t, err)
	if bytes.Contains(raw, []byte(nameMarker)) {
		t.Error("another person's file name appears in the export")
	}
}

func TestExportPerson_UploadedNoSubjectInternal_KeepsName(t *testing.T) {
	r := newIRig(t)
	me := r.person()
	cases := []struct {
		lvl      model.Sensitivity
		wantName bool
	}{
		{model.SensitivityPublic, true}, {model.SensitivityInternal, true},
		{model.SensitivityPersonal, false}, {model.SensitivitySensitive, false},
	}
	want := map[string]bool{}
	for _, c := range cases {
		f := r.upload(asPerson(ctxT(t), me), r.vol, "n-"+c.lvl.String()+".pdf", files.Documents, pdfDoc(c.lvl.String()+"\n"),
			files.Attachment{Record: qualRef(), Level: c.lvl})
		want[f.ID.String()] = c.wantName
	}
	d := export(t, r, me)
	for _, u := range d.Uploaded {
		_, has := u["name"]
		if has != want[u["id"].(string)] { //nolint:forcetypeassert,errcheck // test
			t.Errorf("entry %v: name present = %v, want %v", u["id"], has, want[u["id"].(string)]) //nolint:forcetypeassert,errcheck // test
		}
	}
	if len(d.Uploaded) != 4 {
		t.Errorf("uploaded = %d", len(d.Uploaded))
	}
}

func TestExportPerson_OwnUploadAboutSelf_IsInAboutOnly(t *testing.T) {
	r := newIRig(t)
	me := r.person()
	f := r.upload(asPerson(ctxT(t), me), r.vol, "mine.pdf", files.Documents, pdfDoc("self\n"), files.Attachment{Record: qualRef(), Subject: me, Level: model.SensitivityPersonal})
	d := export(t, r, me)
	if len(d.About) != 1 || d.About[0]["id"] != f.ID.String() || len(d.Uploaded) != 0 {
		t.Errorf("about=%d uploaded=%d; a file about the uploader belongs to about only", len(d.About), len(d.Uploaded))
	}
}

func TestExportPerson_Empty_HasBothKeys(t *testing.T) {
	r := newIRig(t)
	raw, err := r.svc.ExportPerson(ctxT(t), r.person())
	wantNoErr(t, err)
	var m map[string]json.RawMessage
	wantNoErr(t, json.Unmarshal(raw, &m))
	for _, k := range []string{"about", "uploaded"} {
		if _, ok := m[k]; !ok {
			t.Errorf("export lacks key %q: %s", k, raw)
		}
	}
}

// F45
func TestExportPerson_ObjectMissing_ReturnsUnavailable(t *testing.T) {
	r := newIRig(t)
	me := r.person()
	f := r.upload(ctxT(t), r.vol, "m.pdf", files.Documents, pdfDoc("m\n"), files.Attachment{Record: qualRef(), Subject: me, Level: model.SensitivityPersonal})
	wantNoErr(t, r.inner.Delete(ctxT(t), ownerVol, objects.Key(f.ID.String())))
	_, err := r.svc.ExportPerson(ctxT(t), me)
	wantKind(t, err, errs.Unavailable)
	if !isMissing(err) {
		t.Errorf("err %v does not wrap objects.ErrMissing", err)
	}
}

// ---- DeletePerson ----

func TestDeletePerson_SubjectFileRemains_ReturnsConflictInUse(t *testing.T) { // F46
	r := newIRig(t)
	me, uploader := r.person(), r.person()
	r.upload(asPerson(ctxT(t), uploader), r.vol, "m.pdf", files.Documents, pdfDoc("m\n"), files.Attachment{Record: qualRef(), Subject: me, Level: model.SensitivityPersonal})
	// the person also uploaded something about another person; it must not be anonymised when the delete fails
	mine := r.upload(asPerson(ctxT(t), me), r.vol, "o.pdf", files.Documents, pdfDoc("o\n"), files.Attachment{Record: qualRef(), Subject: uploader, Level: model.SensitivityPersonal})

	err := r.pool.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
		_, derr := r.svc.DeletePerson(ctx, me)
		return derr
	})
	wantCode(t, err, errs.Conflict, "in_use")
	got, gerr := r.vol.Get(ctxT(t), mine.ID)
	wantNoErr(t, gerr)
	if got.UploadedBy != me {
		t.Error("the rolled-back deletion still cleared uploaded_by")
	}
	// The foreign key is the second backstop.
	if _, err := r.pool.Exec(ctxT(t), `DELETE FROM core.person WHERE id = $1`, [16]byte(me)); err == nil {
		t.Error("core.person row deleted although files reference it (ON DELETE RESTRICT)")
	}
}

func TestDeletePerson_AnonymisesUploads(t *testing.T) {
	r := newIRig(t)
	me, other := r.person(), r.person()
	f1 := r.upload(asPerson(ctxT(t), me), r.vol, "1.pdf", files.Documents, pdfDoc("1\n"), files.Attachment{Record: qualRef(), Subject: other, Level: model.SensitivityPersonal})
	f2 := r.upload(asPerson(ctxT(t), me), r.inc, "2.png", files.Images, plainPNG(t), files.Attachment{Record: photoRef(), Level: model.SensitivityInternal})
	f3 := r.upload(asPerson(ctxT(t), other), r.vol, "3.pdf", files.Documents, pdfDoc("3\n"), files.Attachment{Record: qualRef(), Subject: other, Level: model.SensitivityPersonal})

	var d files.Deletion
	err := r.pool.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
		var derr error
		d, derr = r.svc.DeletePerson(ctx, me)
		return derr
	})
	wantNoErr(t, err)
	if d.Anonymized != 2 || d.Deleted != 0 {
		t.Errorf("Deletion = %+v, want Anonymized 2, Deleted 0", d)
	}
	for _, f := range []struct {
		st files.Store
		id files.FileID
		by model.PersonID
	}{{r.vol, f1.ID, model.PersonID{}}, {r.inc, f2.ID, model.PersonID{}}, {r.vol, f3.ID, other}} {
		got, err := f.st.Get(ctxT(t), f.id)
		wantNoErr(t, err)
		if got.UploadedBy != f.by {
			t.Errorf("file %s UploadedBy = %v, want %v", f.id, got.UploadedBy, f.by)
		}
	}
	// Idempotent.
	err = r.pool.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
		var derr error
		d, derr = r.svc.DeletePerson(ctx, me)
		return derr
	})
	wantNoErr(t, err)
	if d.Anonymized != 0 {
		t.Errorf("second Deletion = %+v", d)
	}
	// Files have no uploader left in the audit-free metadata: the person row can go.
	if _, err := r.pool.Exec(ctxT(t), `DELETE FROM core.person WHERE id = $1`, [16]byte(me)); err != nil {
		t.Errorf("the person row cannot be deleted after anonymisation: %v", err)
	}
}

func TestDeletePerson_OwnerDeletedSubjectFilesFirst_Succeeds(t *testing.T) {
	r := newIRig(t)
	me := r.person()
	rec := qualRef()
	r.upload(ctxT(t), r.vol, "m.pdf", files.Documents, pdfDoc("m\n"), files.Attachment{Record: rec, Subject: me, Level: model.SensitivityPersonal})
	err := r.pool.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
		if _, derr := r.vol.DeleteByRecord(ctx, rec); derr != nil { // the module's DeletePerson ran first
			return derr
		}
		_, derr := r.svc.DeletePerson(ctx, me)
		return derr
	})
	wantNoErr(t, err)
	// The bytes are gone after commit (or retried by Reap): no tombstone outstanding at the end.
	if _, rerr := r.svc.Reap(ctxT(t)); rerr != nil {
		t.Fatal(rerr)
	}
	if r.tombstones() != 0 || len(r.objectKeys(ownerVol)) != 0 {
		t.Errorf("tombstones=%d objects=%d; the person's bytes must be gone", r.tombstones(), len(r.objectKeys(ownerVol)))
	}
}

func TestDeletePerson_StoreDown_StillSucceedsAndReapFinishes(t *testing.T) {
	r := newIRig(t)
	me := r.person()
	rec := qualRef()
	r.upload(ctxT(t), r.vol, "m.pdf", files.Documents, pdfDoc("m\n"), files.Attachment{Record: rec, Subject: me, Level: model.SensitivityPersonal})
	r.store.down()
	err := r.pool.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
		if _, derr := r.vol.DeleteByRecord(ctx, rec); derr != nil {
			return derr
		}
		_, derr := r.svc.DeletePerson(ctx, me)
		return derr
	})
	wantNoErr(t, err)
	if r.tombstones() != 1 {
		t.Fatalf("tombstones = %d, want 1 outstanding while the store is down", r.tombstones())
	}
	r.store.up()
	_, err = r.svc.Reap(ctxT(t))
	wantNoErr(t, err)
	if r.tombstones() != 0 || len(r.objectKeys(ownerVol)) != 0 {
		t.Error("Reap did not finish the person's deletion")
	}
}

func TestDeletePerson_NoWriteTx_ReturnsInternalWithRows(t *testing.T) { // F47
	r := newIRig(t)
	me := r.person()
	f := r.upload(asPerson(ctxT(t), me), r.vol, "o.pdf", files.Documents, pdfDoc("o\n"), files.Attachment{Record: qualRef(), Level: model.SensitivityInternal})
	_, err := r.svc.DeletePerson(ctxT(t), me)
	wantKind(t, err, errs.Internal)
	got, gerr := r.vol.Get(ctxT(t), f.ID)
	wantNoErr(t, gerr)
	if got.UploadedBy != me {
		t.Error("uploaded_by changed outside a transaction")
	}
}

func TestDeletePerson_ReadOnly_ReturnsUnavailable(t *testing.T) { // F47
	r := newIRig(t)
	me := r.person()
	r.upload(asPerson(ctxT(t), me), r.vol, "o.pdf", files.Documents, pdfDoc("o\n"), files.Attachment{Record: qualRef(), Level: model.SensitivityInternal})
	ro := r.readOnlyCopy()
	err := r.pool.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
		_, derr := ro.DeletePerson(ctx, me)
		return derr
	})
	wantReadOnly(t, err)
}
