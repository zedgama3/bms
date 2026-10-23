//go:build integration

package volunteers_test

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/volunteers"
	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
)

var jan1 = vol.Date{Year: 2026, Month: time.January, Day: 1}

// pdf is the smallest body files sniffs as a PDF.
func pdf() []byte { return []byte("%PDF-1.4\n1 0 obj<<>>endobj\ntrailer<<>>\n%%EOF\n") }

func upload(name string, body []byte) *files.Multipart {
	return &files.Multipart{Name: name, DeclaredType: "application/pdf", Body: bytes.NewReader(body)}
}

// ---- catalogue (F2-F11) ----

// F4
func TestCreateQualification_ArchivedDepartment_ReturnsConflict(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	r.archiveDepartment(d)
	_, err := r.svc.CreateQualification(r.as(r.boss), vol.QualificationInput{Department: d, Name: "Radio"})
	wantConflict(t, err, "archived")
}

func TestCreateQualification_UnknownDepartment_ReturnsNotFound(t *testing.T) {
	r := newReal(t)
	_, err := r.svc.CreateQualification(r.as(r.boss), vol.QualificationInput{Department: model.NewID[model.DepartmentID](), Name: "Radio"})
	wantKind(t, err, errs.NotFound)
}

// F5
func TestCreateQualification_InvalidInput_Table(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	tests := []struct {
		name string
		in   vol.QualificationInput
	}{
		{"empty name", vol.QualificationInput{Department: d}},
		{"name too long", vol.QualificationInput{Department: d, Name: strings.Repeat("x", vol.MaxNameBytes+1)}},
		{"control chars", vol.QualificationInput{Department: d, Name: "bad\x07"}},
		{"validity negative", vol.QualificationInput{Department: d, Name: "ok", ValidityDays: -1}},
		{"validity too long", vol.QualificationInput{Department: d, Name: "ok", ValidityDays: vol.MaxValidityDays + 1}},
		{"description too long", vol.QualificationInput{Department: d, Name: "ok", Description: strings.Repeat("d", vol.MaxDescriptionBytes+1)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := r.svc.CreateQualification(r.as(r.boss), tc.in)
			wantKind(t, err, errs.Invalid)
		})
	}
}

// F6
func TestCreateQualification_DuplicateName_ReturnsConflict(t *testing.T) {
	r := newReal(t)
	d, d2 := r.department("Rangers"), r.department("Greeters")
	r.qual(d, "Radio licence")
	_, err := r.svc.CreateQualification(r.as(r.boss), vol.QualificationInput{Department: d, Name: "RADIO LICENCE"})
	wantConflict(t, err, "taken")
	// The same name in another department, and org-wide, is a different scope.
	if _, err := r.svc.CreateQualification(r.as(r.boss), vol.QualificationInput{Department: d2, Name: "Radio licence"}); err != nil {
		t.Errorf("same name in another department: %v", err)
	}
	r.qual(model.DepartmentID{}, "Over 18")
	_, err = r.svc.CreateQualification(r.as(r.boss), vol.QualificationInput{Name: "over 18"})
	wantConflict(t, err, "taken")
}

func TestCreateQualification_DepartmentLeadAndSteward_Succeed(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	lead := r.lead(d)
	q, err := r.svc.CreateQualification(r.as(lead), vol.QualificationInput{Department: d, Name: "Ranger training"})
	wantNoErr(t, err)
	if q.Department != d || q.ID.IsZero() {
		t.Errorf("created %+v", q)
	}
	steward := r.person("")
	r.az.Grant(steward, pQualManage)
	org, err := r.svc.CreateQualification(r.as(steward), vol.QualificationInput{Name: "Over 18"})
	wantNoErr(t, err)
	if !org.Department.IsZero() {
		t.Errorf("an org-wide qualification carries department %v", org.Department)
	}
	if len(r.audited("volunteers.qualification.created")) != 2 {
		t.Errorf("want two volunteers.qualification.created entries, got %d", len(r.audited("volunteers.qualification.created")))
	}
}

// F7
func TestUpdateQualification_Unknown_ReturnsNotFound(t *testing.T) {
	r := newReal(t)
	_, err := r.svc.UpdateQualification(r.as(r.boss), model.NewID[vol.QualificationID](), vol.QualificationInput{Name: "x"})
	wantKind(t, err, errs.NotFound)
}

func TestUpdateQualification_NotLead_ReturnsPermission(t *testing.T) {
	r := newReal(t)
	d, other := r.department("Rangers"), r.department("Greeters")
	q := r.qual(d, "Radio")
	_, err := r.svc.UpdateQualification(r.as(r.lead(other)), q.ID, vol.QualificationInput{Department: d, Name: "Radio 2"})
	wantKind(t, err, errs.Permission)
}

// F8
func TestUpdateQualification_ChangesDepartment_ReturnsInvalid(t *testing.T) {
	r := newReal(t)
	d, other := r.department("Rangers"), r.department("Greeters")
	q := r.qual(d, "Radio")
	_, err := r.svc.UpdateQualification(r.as(r.boss), q.ID, vol.QualificationInput{Department: other, Name: "Radio"})
	wantInvalid(t, err, "scope")
	_, err = r.svc.UpdateQualification(r.as(r.boss), q.ID, vol.QualificationInput{Name: "Radio"}) // to org-wide
	wantInvalid(t, err, "scope")
}

func TestUpdateQualification_ChangesNameAndValidity(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	q := r.qual(d, "Radio")
	got, err := r.svc.UpdateQualification(r.as(r.lead(d)), q.ID, vol.QualificationInput{Department: d, Name: "Radio licence", ValidityDays: 365})
	wantNoErr(t, err)
	if got.Name != "Radio licence" || got.ValidityDays != 365 || got.Department != d {
		t.Errorf("updated = %+v", got)
	}
}

// F9
func TestUpdateQualification_Archived_ReturnsConflict(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	q := r.qual(d, "Radio")
	wantNoErr(t, r.svc.ArchiveQualification(r.as(r.boss), q.ID))
	_, err := r.svc.UpdateQualification(r.as(r.boss), q.ID, vol.QualificationInput{Department: d, Name: "Radio again"})
	wantConflict(t, err, "archived")
}

// F10
func TestArchiveQualification_NotLead_ReturnsPermission(t *testing.T) {
	r := newReal(t)
	d, other := r.department("Rangers"), r.department("Greeters")
	q := r.qual(d, "Radio")
	wantKind(t, r.svc.ArchiveQualification(r.as(r.lead(other)), q.ID), errs.Permission)
	wantKind(t, r.svc.ArchiveQualification(r.as(r.volunteer("Tumbleweed")), q.ID), errs.Permission)
	wantKind(t, r.svc.ArchiveQualification(r.as(r.boss), model.NewID[vol.QualificationID]()), errs.NotFound)
}

// F11
func TestArchiveQualification_Twice_ReturnsConflict(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	q := r.qual(d, "Radio")
	wantNoErr(t, r.svc.ArchiveQualification(r.as(r.lead(d)), q.ID))
	wantConflict(t, r.svc.ArchiveQualification(r.as(r.boss), q.ID), "archived")
	if len(r.audited("volunteers.qualification.archived")) != 1 {
		t.Error("want exactly one volunteers.qualification.archived entry")
	}
}

func TestListQualifications_Visibility(t *testing.T) {
	r := newReal(t)
	d, other := r.department("Rangers"), r.department("Greeters")
	org := r.qual(model.DepartmentID{}, "Over 18")
	mine := r.qual(d, "Radio")
	gone := r.qual(d, "Old course")
	wantNoErr(t, r.svc.ArchiveQualification(r.as(r.boss), gone.ID))
	plain := r.as(r.person("Tumbleweed"))

	got, err := r.svc.ListQualifications(plain, vol.QualificationQuery{})
	wantNoErr(t, err)
	ids := map[vol.QualificationID]bool{}
	for _, q := range got {
		ids[q.ID] = true
	}
	if !ids[org.ID] || !ids[mine.ID] {
		t.Errorf("a signed-in person must see org-wide and department entries, got %d", len(got))
	}
	if ids[gone.ID] {
		t.Error("an archived qualification is listed without IncludeArchived")
	}
	// Archived ones only inside the manage extent.
	if all, err := r.svc.ListQualifications(plain, vol.QualificationQuery{IncludeArchived: true}); err == nil {
		for _, q := range all {
			if !q.ArchivedAt.IsZero() {
				t.Error("archived entry leaked to a person without qualifications.manage")
			}
		}
	} else {
		wantKind(t, err, errs.Permission)
	}
	all, err := r.svc.ListQualifications(r.as(r.lead(d)), vol.QualificationQuery{Department: d, IncludeArchived: true})
	wantNoErr(t, err)
	found := false
	for _, q := range all {
		found = found || q.ID == gone.ID
	}
	if !found {
		t.Error("the department's lead does not see its archived qualification")
	}
	// A lead of another department sees nothing archived of d.
	if all, err := r.svc.ListQualifications(r.as(r.lead(other)), vol.QualificationQuery{Department: d, IncludeArchived: true}); err == nil {
		for _, q := range all {
			if q.ID == gone.ID {
				t.Error("a lead of another department sees d's archived entry")
			}
		}
	} else {
		wantKind(t, err, errs.Permission)
	}
}

// ---- records (F12-F20) ----

// F12
func TestRecordsOf_OtherPersonNoPermission_ReturnsPermission(t *testing.T) {
	r := newReal(t)
	someone, nosy := r.person("Tumbleweed"), r.volunteer("Nosy Parker")
	_, err := recordItems(r.svc.RecordsOf(r.as(nosy), someone))
	wantKind(t, err, errs.Permission)
}

// F13
func TestRecordsOf_FiltersToExtent(t *testing.T) {
	r := newReal(t)
	a, b := r.department("Rangers"), r.department("Greeters")
	qa, qb, qo := r.qual(a, "Radio"), r.qual(b, "Door duty"), r.qual(model.DepartmentID{}, "Over 18")
	p := r.person("Tumbleweed")
	for _, q := range []vol.QualificationID{qa.ID, qb.ID, qo.ID} {
		r.verified(p, q)
	}
	own, err := recordItems(r.svc.RecordsOf(r.as(p), p))
	wantNoErr(t, err)
	if len(own) != 3 {
		t.Errorf("a person sees %d of their own records, want all 3", len(own))
	}
	viewer := r.person("")
	r.az.GrantIn(viewer, pQualRead, a)
	seen, err := recordItems(r.svc.RecordsOf(r.as(viewer), p))
	wantNoErr(t, err)
	got := map[vol.QualificationID]bool{}
	for _, rec := range seen {
		got[rec.Qualification] = true
	}
	if !got[qa.ID] || !got[qo.ID] || got[qb.ID] || len(seen) != 2 {
		t.Errorf("a reader at Rangers saw %v; want Rangers' and the org-wide one only", got)
	}
}

// F14
func TestRecordQualification_NotDepartmentLead_ReturnsPermission(t *testing.T) {
	r := newReal(t)
	d, other := r.department("Rangers"), r.department("Greeters")
	q := r.qual(d, "Radio")
	_, err := r.svc.RecordQualification(r.as(r.lead(other)), vol.RecordInput{Qualification: q.ID, Person: r.person("Tumbleweed"), AcquiredOn: jan1})
	wantKind(t, err, errs.Permission)
}

// F15
func TestRecordQualification_OrgWideNoLeadAnywhere_ReturnsPermission(t *testing.T) {
	r := newReal(t)
	q := r.qual(model.DepartmentID{}, "Over 18")
	_, err := r.svc.RecordQualification(r.as(r.volunteer("Nosy Parker")), vol.RecordInput{Qualification: q.ID, Person: r.person("Tumbleweed"), AcquiredOn: jan1})
	wantKind(t, err, errs.Permission)
	// Holding only the read permission is not enough either.
	reader := r.person("")
	r.az.GrantIn(reader, pQualRead, r.department("Rangers"))
	_, err = r.svc.RecordQualification(r.as(reader), vol.RecordInput{Qualification: q.ID, Person: r.person("Tumbleweed"), AcquiredOn: jan1})
	wantKind(t, err, errs.Permission)
}

// F16 (D41)
func TestRecordQualification_OrgWideAnyLead_Succeeds(t *testing.T) {
	r := newReal(t)
	elsewhere := r.department("Greeters")
	q := r.qual(model.DepartmentID{}, "Over 18")
	lead, p := r.lead(elsewhere), r.person("Tumbleweed")
	rec, err := r.svc.RecordQualification(r.as(lead), vol.RecordInput{Qualification: q.ID, Person: p, AcquiredOn: jan1})
	wantNoErr(t, err)
	if !rec.Verified() || rec.VerifiedBy != lead || rec.Person != p {
		t.Errorf("record = %+v, want verified by the lead for the person", rec)
	}
	sigs := r.published(volunteers.SignalQualificationVerified)
	if len(sigs) != 1 {
		t.Fatalf("published %d qualification-verified signals, want 1", len(sigs))
	}
	if _, ok := sigs[0].Payload["department"]; ok {
		t.Error("the department key must be omitted for an org-wide qualification")
	}
	for _, k := range []string{"record", "qualification", "person"} {
		if _, ok := sigs[0].Payload[k]; !ok {
			t.Errorf("payload lacks %q", k)
		}
	}
}

func TestRecordQualification_DepartmentLead_PublishesDepartment(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	q := r.qual(d, "Radio")
	_, err := r.svc.RecordQualification(r.as(r.lead(d)), vol.RecordInput{Qualification: q.ID, Person: r.person("Tumbleweed"), AcquiredOn: jan1})
	wantNoErr(t, err)
	sigs := r.published(volunteers.SignalQualificationVerified)
	if len(sigs) != 1 {
		t.Fatalf("published %d signals, want 1", len(sigs))
	}
	if _, ok := sigs[0].Payload["department"]; !ok {
		t.Error("the payload lacks department for a department-specific qualification")
	}
}

// F17
func TestRecordQualification_Self_ReturnsInvalid(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	q := r.qual(d, "Radio")
	lead := r.lead(d)
	_, err := r.svc.RecordQualification(r.as(lead), vol.RecordInput{Qualification: q.ID, Person: lead, AcquiredOn: jan1})
	wantInvalid(t, err, "self")
}

// F18
func TestRecordQualification_ArchivedQualification_ReturnsConflict(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	q := r.qual(d, "Radio")
	wantNoErr(t, r.svc.ArchiveQualification(r.as(r.boss), q.ID))
	_, err := r.svc.RecordQualification(r.as(r.lead(d)), vol.RecordInput{Qualification: q.ID, Person: r.person("Tumbleweed"), AcquiredOn: jan1})
	wantConflict(t, err, "archived")
	_, err = r.svc.RecordQualification(r.as(r.boss), vol.RecordInput{Qualification: model.NewID[vol.QualificationID](), Person: r.person("Tumbleweed"), AcquiredOn: jan1})
	wantKind(t, err, errs.NotFound)
}

// F19
func TestRecordQualification_Dates_Table(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	q := r.qual(d, "Radio")
	p := r.person("Tumbleweed")
	tests := []struct {
		name       string
		acq, exp   vol.Date
		kind       errs.Kind
		wantOK     bool
		wantCodeIs string
	}{
		{"ok", jan1, vol.Date{Year: 2027, Month: 1, Day: 1}, 0, true, ""},
		{"expires before acquired", jan1, vol.Date{Year: 2025, Month: 12, Day: 31}, errs.Invalid, false, "range"},
		{"acquired in the future", vol.Date{Year: 2027, Month: 6, Day: 1}, vol.Date{}, errs.Invalid, false, "range"},
		{"malformed acquired", vol.Date{Year: 2026, Month: 2, Day: 30}, vol.Date{}, errs.Invalid, false, "malformed"},
		{"malformed expires", jan1, vol.Date{Year: 2026, Month: 14, Day: 1}, errs.Invalid, false, "malformed"},
		{"acquired missing", vol.Date{}, vol.Date{}, errs.Invalid, false, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			person := p
			if tc.wantOK {
				person = r.person("Dandelion")
			}
			_, err := r.svc.RecordQualification(r.as(r.lead(d)), vol.RecordInput{Qualification: q.ID, Person: person, AcquiredOn: tc.acq, ExpiresOn: tc.exp})
			if tc.wantOK {
				wantNoErr(t, err)
				return
			}
			wantKind(t, err, tc.kind)
			if tc.wantCodeIs != "" {
				if got := codeOf(err); got != tc.wantCodeIs {
					t.Errorf("code = %q, want %q", got, tc.wantCodeIs)
				}
			}
		})
	}
}

// F20
func TestRecordQualification_UnknownPerson_ReturnsInvalid(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	q := r.qual(d, "Radio")
	_, err := r.svc.RecordQualification(r.as(r.lead(d)), vol.RecordInput{Qualification: q.ID, Person: model.NewID[model.PersonID](), AcquiredOn: jan1})
	wantInvalid(t, err, "unknown")
	if fieldOf(err) != "person" {
		t.Errorf("field = %q, want person", fieldOf(err))
	}
}

func TestRecordQualification_DefaultsExpiryFromCatalogueValidity(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	q, err := r.svc.CreateQualification(r.as(r.boss), vol.QualificationInput{Department: d, Name: "First aid", ValidityDays: 365})
	wantNoErr(t, err)
	rec, err := r.svc.RecordQualification(r.as(r.lead(d)), vol.RecordInput{Qualification: q.ID, Person: r.person("Tumbleweed"), AcquiredOn: jan1})
	wantNoErr(t, err)
	if want := jan1.AddDays(365); rec.ExpiresOn != want {
		t.Errorf("ExpiresOn = %+v, want AcquiredOn + 365 days = %+v", rec.ExpiresOn, want)
	}
}

// ---- upload (F21-F27) ----

func uploadScenario(t *testing.T) (*rig, vol.Qualification, model.DepartmentID, model.PersonID) {
	t.Helper()
	r := newReal(t)
	d := r.department("Rangers")
	q := r.qual(d, "Radio")
	return r, q, d, r.volunteer("Tumbleweed")
}

func TestUploadCertificate_StoresPendingRecordWithPersonalCertificate(t *testing.T) {
	r, q, _, p := uploadScenario(t)
	rec, err := r.svc.UploadCertificate(r.as(p), vol.UploadInput{Qualification: q.ID, AcquiredOn: jan1, Upload: upload("radio.pdf", pdf())})
	wantNoErr(t, err)
	if rec.Verified() || !rec.VerifiedBy.IsZero() || rec.Person != p || rec.Certificate.IsZero() {
		t.Fatalf("record = %+v, want a pending record of the actor with a certificate", rec)
	}
	fs := r.fl.Files()
	if len(fs) != 1 {
		t.Fatalf("%d files stored, want 1", len(fs))
	}
	f := fs[0]
	if f.Record.Kind != "volunteers.qualification-record" || f.Record.ID != [16]byte(rec.ID) {
		t.Errorf("file record = %+v, want volunteers.qualification-record %s", f.Record, rec.ID)
	}
	if f.Subject != p || f.Level != model.SensitivityPersonal || f.ID != rec.Certificate {
		t.Errorf("file subject/level = %v/%v, want the person and personal", f.Subject, f.Level)
	}
	if len(r.fl.Staged()) != 0 {
		t.Error("a staged upload was left behind")
	}
	if len(r.audited("volunteers.qualification-record.created")) != 1 {
		t.Error("want one qualification-record.created audit entry")
	}
}

// F21
func TestUploadCertificate_SettingOff_ReturnsUnsupported(t *testing.T) {
	r, q, _, p := uploadScenario(t)
	r.set.Set(volunteers.SettingSelfUpload, "false")
	_, err := r.svc.UploadCertificate(r.as(p), vol.UploadInput{Qualification: q.ID, AcquiredOn: jan1, Upload: upload("radio.pdf", pdf())})
	wantKind(t, err, errs.Unsupported)
	if len(r.fl.Files()) != 0 || len(r.fl.Staged()) != 0 {
		t.Error("a file was kept although self-upload is off")
	}
}

// F22
func TestUploadCertificate_NoFile_ReturnsInvalid(t *testing.T) {
	r, q, _, p := uploadScenario(t)
	_, err := r.svc.UploadCertificate(r.as(p), vol.UploadInput{Qualification: q.ID, AcquiredOn: jan1})
	wantKind(t, err, errs.Invalid)
	_, err = r.svc.UploadCertificate(r.as(p), vol.UploadInput{Qualification: q.ID, AcquiredOn: jan1, Upload: upload("empty.pdf", nil)})
	wantKind(t, err, errs.Invalid)
}

// F23
func TestUploadCertificate_WrongType_ReturnsInvalid(t *testing.T) {
	r, q, _, p := uploadScenario(t)
	_, err := r.svc.UploadCertificate(r.as(p), vol.UploadInput{Qualification: q.ID, AcquiredOn: jan1, Upload: upload("notes.txt", []byte("just some text, not a certificate"))})
	wantKind(t, err, errs.Invalid)
	if len(r.fl.Files()) != 0 || len(r.fl.Staged()) != 0 {
		t.Error("a rejected upload left bytes behind")
	}
	recs, err := recordItems(r.svc.RecordsOf(r.as(p), p))
	wantNoErr(t, err)
	if len(recs) != 0 {
		t.Errorf("%d records created by a rejected upload", len(recs))
	}
}

// F24
func TestUploadCertificate_PendingExists_ReturnsConflict(t *testing.T) {
	r, q, _, p := uploadScenario(t)
	in := func() vol.UploadInput {
		return vol.UploadInput{Qualification: q.ID, AcquiredOn: jan1, Upload: upload("radio.pdf", pdf())}
	}
	_, err := r.svc.UploadCertificate(r.as(p), in())
	wantNoErr(t, err)
	_, err = r.svc.UploadCertificate(r.as(p), in())
	wantConflict(t, err, "pending_exists")
	if n := len(r.fl.Files()); n != 1 {
		t.Errorf("%d files after the refused second upload, want 1", n)
	}
	if len(r.fl.Staged()) != 0 {
		t.Error("the refused upload was not discarded")
	}
}

// F25
func TestUploadCertificate_Archived_ReturnsConflict(t *testing.T) {
	r, q, _, p := uploadScenario(t)
	wantNoErr(t, r.svc.ArchiveQualification(r.as(r.boss), q.ID))
	_, err := r.svc.UploadCertificate(r.as(p), vol.UploadInput{Qualification: q.ID, AcquiredOn: jan1, Upload: upload("radio.pdf", pdf())})
	wantConflict(t, err, "archived")
	if len(r.fl.Staged()) != 0 || len(r.fl.Files()) != 0 {
		t.Error("an upload for an archived qualification left bytes behind")
	}
}

// F26
func TestUploadCertificate_FilesFails_RollsBack(t *testing.T) {
	r, q, _, p := uploadScenario(t)
	r.fl.Fail(errs.E("test.files", errs.Unavailable, errors.New("store down")))
	_, err := r.svc.UploadCertificate(r.as(p), vol.UploadInput{Qualification: q.ID, AcquiredOn: jan1, Upload: upload("radio.pdf", pdf())})
	wantKind(t, err, errs.Unavailable)
	r.fl.Fail(nil)
	recs, err := recordItems(r.svc.RecordsOf(r.as(p), p))
	wantNoErr(t, err)
	if len(recs) != 0 {
		t.Errorf("%d records survive a failed upload, want 0", len(recs))
	}
}

// D40/D41: an upload never counts until a lead verifies it.
func TestUploadCertificate_NeverCounts(t *testing.T) {
	r, q, d, p := uploadScenario(t)
	pos := r.position(d, "Radio op", vol.AccessOpen, q.ID)
	sh := r.shift(d, pos.ID, model.EventID{}, when(0), 4, 2)
	_, err := r.svc.UploadCertificate(r.as(p), vol.UploadInput{Qualification: q.ID, AcquiredOn: jan1, Upload: upload("radio.pdf", pdf())})
	wantNoErr(t, err)
	_, err = r.svc.SignUp(r.as(p), sh.ID)
	wantConflict(t, err, "unqualified")
	// The uploader cannot verify it either, even holding the lead permission (no self-attestation).
	r.az.GrantIn(p, pQualVerify, d)
	recs, err := recordItems(r.svc.RecordsOf(r.as(p), p))
	wantNoErr(t, err)
	_, err = r.svc.VerifyRecord(r.as(p), recs[0].ID, vol.VerifyInput{})
	wantInvalid(t, err, "self")
	_, err = r.svc.SignUp(r.as(p), sh.ID)
	wantConflict(t, err, "unqualified")
}

// ---- verify, reject, remove (F28-F38) ----

func pendingRecord(t *testing.T, r *rig, q vol.Qualification, p model.PersonID) vol.Record {
	t.Helper()
	rec, err := r.svc.UploadCertificate(r.as(p), vol.UploadInput{Qualification: q.ID, AcquiredOn: jan1, Upload: upload("radio.pdf", pdf())})
	if err != nil {
		t.Fatalf("UploadCertificate: %v", err)
	}
	return rec
}

func TestVerifyRecord_Lead_VerifiesNotifiesAndPublishes(t *testing.T) {
	r, q, d, p := uploadScenario(t)
	rec := pendingRecord(t, r, q, p)
	lead := r.lead(d)
	got, err := r.svc.VerifyRecord(r.as(lead), rec.ID, vol.VerifyInput{AcquiredOn: vol.Date{Year: 2026, Month: 2, Day: 2}})
	wantNoErr(t, err)
	if !got.Verified() || got.VerifiedBy != lead || got.AcquiredOn != (vol.Date{Year: 2026, Month: 2, Day: 2}) {
		t.Errorf("verified = %+v, want verified by the lead with the corrected acquired date", got)
	}
	if got.ExpiresOn != rec.ExpiresOn {
		t.Errorf("a zero VerifyInput date must keep what the person entered; ExpiresOn %v -> %v", rec.ExpiresOn, got.ExpiresOn)
	}
	sent := r.mail.Sent()
	if len(sent) != 1 || string(sent[0].Template) != "volunteers.qualification-verified" || len(sent[0].To) != 1 || sent[0].To[0] != p {
		t.Fatalf("sent = %+v, want one qualification-verified message to the person", sent)
	}
	if sent[0].Key != "verified:"+rec.ID.String() {
		t.Errorf("notification key = %q", sent[0].Key)
	}
	if len(r.published(volunteers.SignalQualificationVerified)) != 1 {
		t.Error("no qualification-verified signal")
	}
	if len(r.audited("volunteers.qualification-record.verified")) != 1 {
		t.Error("no qualification-record.verified audit entry")
	}
	// The record now counts for eligibility.
	pos := r.position(d, "Radio op", vol.AccessOpen, q.ID)
	r.book(p, r.shift(d, pos.ID, model.EventID{}, when(0), 4, 1).ID)
}

// F28
func TestVerifyRecord_WrongDepartmentLead_ReturnsPermission(t *testing.T) {
	r, q, _, p := uploadScenario(t)
	rec := pendingRecord(t, r, q, p)
	other := r.department("Greeters")
	_, err := r.svc.VerifyRecord(r.as(r.lead(other)), rec.ID, vol.VerifyInput{})
	if !errs.Is(err, errs.Permission) {
		t.Fatalf("VerifyRecord by another department's lead = %v, want Permission (D41)", err)
	}
	_, err = r.svc.VerifyRecord(r.as(p), rec.ID, vol.VerifyInput{})
	if !errs.Is(err, errs.Permission) {
		t.Fatalf("VerifyRecord by the participant = %v, want Permission", err)
	}
}

// D41: any lead may verify an org-wide qualification.
func TestVerifyRecord_OrgWideAnyLead_Succeeds(t *testing.T) {
	r := newReal(t)
	q := r.qual(model.DepartmentID{}, "Over 18")
	p := r.volunteer("Tumbleweed")
	rec := pendingRecord(t, r, q, p)
	got, err := r.svc.VerifyRecord(r.as(r.lead(r.department("Greeters"))), rec.ID, vol.VerifyInput{})
	wantNoErr(t, err)
	if !got.Verified() {
		t.Error("not verified")
	}
}

// F29
func TestVerifyRecord_Unknown_ReturnsNotFound(t *testing.T) {
	r := newReal(t)
	_, err := r.svc.VerifyRecord(r.as(r.lead(r.department("Rangers"))), model.NewID[vol.RecordID](), vol.VerifyInput{})
	wantKind(t, err, errs.NotFound)
}

// F30
func TestVerifyRecord_AlreadyVerified_ReturnsConflict(t *testing.T) {
	r, q, d, p := uploadScenario(t)
	rec := pendingRecord(t, r, q, p)
	lead := r.lead(d)
	_, err := r.svc.VerifyRecord(r.as(lead), rec.ID, vol.VerifyInput{})
	wantNoErr(t, err)
	_, err = r.svc.VerifyRecord(r.as(lead), rec.ID, vol.VerifyInput{})
	wantConflict(t, err, "status")
}

// F31
func TestVerifyRecord_OwnRecord_ReturnsInvalid(t *testing.T) {
	r, q, d, p := uploadScenario(t)
	rec := pendingRecord(t, r, q, p)
	r.az.GrantIn(p, pQualVerify, d)
	_, err := r.svc.VerifyRecord(r.as(p), rec.ID, vol.VerifyInput{})
	wantInvalid(t, err, "self")
}

// F32
func TestVerifyRecord_NotifyUnsupported_Succeeds(t *testing.T) {
	r, q, d, p := uploadScenario(t)
	rec := pendingRecord(t, r, q, p)
	r.mail.SetConfigured(false)
	got, err := r.svc.VerifyRecord(r.as(r.lead(d)), rec.ID, vol.VerifyInput{})
	wantNoErr(t, err)
	if !got.Verified() {
		t.Error("record not verified")
	}
}

// F33
func TestRejectRecord_NotLead_ReturnsPermission(t *testing.T) {
	r, q, _, p := uploadScenario(t)
	rec := pendingRecord(t, r, q, p)
	wantKind(t, r.svc.RejectRecord(r.as(r.lead(r.department("Greeters"))), rec.ID), errs.Permission)
	wantKind(t, r.svc.RejectRecord(r.as(p), rec.ID), errs.Permission)
}

func TestRejectRecord_Lead_DeletesRecordAndCertificateAndNotifies(t *testing.T) {
	r, q, d, p := uploadScenario(t)
	rec := pendingRecord(t, r, q, p)
	wantNoErr(t, r.svc.RejectRecord(r.as(r.lead(d)), rec.ID))
	if n := len(r.fl.Files()); n != 0 {
		t.Errorf("%d certificate files left after a rejection, want 0", n)
	}
	recs, err := recordItems(r.svc.RecordsOf(r.as(p), p))
	wantNoErr(t, err)
	if len(recs) != 0 {
		t.Error("the rejected record still exists")
	}
	sent := r.mail.Sent()
	if len(sent) != 1 || string(sent[0].Template) != "volunteers.qualification-rejected" || sent[0].To[0] != p {
		t.Errorf("sent = %+v, want one qualification-rejected message to the person", sent)
	}
	if len(r.audited("volunteers.qualification-record.rejected")) != 1 {
		t.Error("no qualification-record.rejected audit entry")
	}
	// The person may now upload again (no pending record remains).
	pendingRecord(t, r, q, p)
}

// F34
func TestRejectRecord_Verified_ReturnsConflict(t *testing.T) {
	r, q, d, p := uploadScenario(t)
	rec := pendingRecord(t, r, q, p)
	lead := r.lead(d)
	_, err := r.svc.VerifyRecord(r.as(lead), rec.ID, vol.VerifyInput{})
	wantNoErr(t, err)
	wantConflict(t, r.svc.RejectRecord(r.as(lead), rec.ID), "status")
}

// F35
func TestRejectRecord_FilesFails_RollsBack(t *testing.T) {
	r, q, d, p := uploadScenario(t)
	rec := pendingRecord(t, r, q, p)
	r.fl.Fail(errs.E("test.files", errs.Unavailable, errors.New("store down")))
	wantKind(t, r.svc.RejectRecord(r.as(r.lead(d)), rec.ID), errs.Unavailable)
	r.fl.Fail(nil)
	recs, err := recordItems(r.svc.RecordsOf(r.as(p), p))
	wantNoErr(t, err)
	if len(recs) != 1 {
		t.Fatalf("%d records after a failed rejection, want the pending one to remain", len(recs))
	}
	if len(r.mail.Sent()) != 0 {
		t.Error("a rejection notice was sent although the rejection rolled back")
	}
}

// F36
func TestRemoveRecord_OwnVerified_ReturnsPermission(t *testing.T) {
	r, q, d, p := uploadScenario(t)
	rec := r.verified(p, q.ID)
	_ = d
	wantKind(t, r.svc.RemoveRecord(r.as(p), rec.ID), errs.Permission)
}

func TestRemoveRecord_OwnPending_DeletesWithCertificate(t *testing.T) {
	r, q, _, p := uploadScenario(t)
	rec := pendingRecord(t, r, q, p)
	wantNoErr(t, r.svc.RemoveRecord(r.as(p), rec.ID))
	if len(r.fl.Files()) != 0 {
		t.Error("the certificate outlived its record")
	}
	if len(r.audited("volunteers.qualification-record.deleted")) != 1 {
		t.Error("no qualification-record.deleted audit entry")
	}
}

func TestRemoveRecord_LeadRemovesVerified(t *testing.T) {
	r, q, d, p := uploadScenario(t)
	rec := r.verified(p, q.ID)
	wantNoErr(t, r.svc.RemoveRecord(r.as(r.lead(d)), rec.ID))
	recs, err := recordItems(r.svc.RecordsOf(r.as(p), p))
	wantNoErr(t, err)
	if len(recs) != 0 {
		t.Error("record not removed")
	}
}

// F37
func TestRemoveRecord_OtherNoPermission_ReturnsNotFound(t *testing.T) {
	r, q, _, p := uploadScenario(t)
	rec := pendingRecord(t, r, q, p)
	wantKind(t, r.svc.RemoveRecord(r.as(r.volunteer("Nosy Parker")), rec.ID), errs.NotFound)
	wantKind(t, r.svc.RemoveRecord(r.as(r.lead(r.department("Greeters"))), rec.ID), errs.NotFound)
}

// F38
func TestRemoveRecord_Unknown_ReturnsNotFound(t *testing.T) {
	r := newReal(t)
	wantKind(t, r.svc.RemoveRecord(r.as(r.boss), model.NewID[vol.RecordID]()), errs.NotFound)
}

func TestPendingRecords_ListsOnlyWhatTheActorMayVerify(t *testing.T) {
	r := newReal(t)
	a, b := r.department("Rangers"), r.department("Greeters")
	qa, qb, qo := r.qual(a, "Radio"), r.qual(b, "Door duty"), r.qual(model.DepartmentID{}, "Over 18")
	p := r.volunteer("Tumbleweed")
	recA := pendingRecord(t, r, qa, p)
	pendingRecord(t, r, qb, p)
	recO := pendingRecord(t, r, qo, p)
	r.verified(r.volunteer("Dandelion"), qa.ID) // verified: not pending

	got, err := r.svc.PendingRecords(r.as(r.lead(a)), vol.PendingQuery{})
	wantNoErr(t, err)
	ids := map[vol.RecordID]bool{}
	for _, rec := range got {
		ids[rec.ID] = true
		if rec.Verified() {
			t.Error("a verified record is in the pending queue")
		}
	}
	if !ids[recA.ID] || !ids[recO.ID] || len(got) != 2 {
		t.Errorf("lead of Rangers sees %d pending records, want Rangers' and the org-wide one", len(got))
	}
	byDept, err := r.svc.PendingRecords(r.as(r.lead(a)), vol.PendingQuery{Department: a})
	wantNoErr(t, err)
	hasA := false
	for _, rec := range byDept {
		hasA = hasA || rec.ID == recA.ID
		if rec.Qualification == qb.ID {
			t.Error("a record of another department's qualification is in the filtered queue")
		}
	}
	if !hasA {
		t.Error("the department filter dropped the department's own pending record")
	}
	_, err = r.svc.PendingRecords(r.as(r.lead(a)), vol.PendingQuery{Department: b})
	if err == nil {
		t.Error("a lead listing another department's queue got no error")
	} else if !errs.Is(err, errs.Permission) && !errs.Is(err, errs.NotFound) {
		t.Errorf("err = %v, want Permission or NotFound", err)
	}
}

// ---- certificates (F40-F43) ----

// F40
func TestOpenCertificate_None_ReturnsNotFound(t *testing.T) {
	r, q, d, p := uploadScenario(t)
	rec := r.verified(p, q.ID) // recorded by a lead: no certificate
	_, _, err := r.svc.OpenCertificate(r.as(r.lead(d)), rec.ID)
	wantKind(t, err, errs.NotFound)
	_, _, err = r.svc.OpenCertificate(r.as(p), rec.ID)
	wantKind(t, err, errs.NotFound)
}

// F41
func TestOpenCertificate_OtherNoPermission_ReturnsNotFound(t *testing.T) {
	r, q, _, p := uploadScenario(t)
	rec := pendingRecord(t, r, q, p)
	_, _, err := r.svc.OpenCertificate(r.as(r.volunteer("Nosy Parker")), rec.ID)
	wantKind(t, err, errs.NotFound)
	_, _, err = r.svc.OpenCertificate(r.as(r.lead(r.department("Greeters"))), rec.ID)
	wantKind(t, err, errs.NotFound)
}

func TestOpenCertificate_OwnerOpensWithoutAudit(t *testing.T) {
	r, q, _, p := uploadScenario(t)
	rec := pendingRecord(t, r, q, p)
	f, c, err := r.svc.OpenCertificate(r.as(p), rec.ID)
	wantNoErr(t, err)
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Errorf("closing the certificate: %v", err)
		}
	})
	body, err := io.ReadAll(c)
	wantNoErr(t, err)
	if !bytes.Equal(body, pdf()) || f.ID != rec.Certificate {
		t.Error("the owner did not get their own certificate back")
	}
	if n := len(r.audited("volunteers.qualification-record.read")); n != 0 {
		t.Errorf("the owner's own read wrote %d qualification-record.read entries, want 0", n)
	}
}

func TestOpenCertificate_LeadAndReader_AuditedPersonal(t *testing.T) {
	r, q, d, p := uploadScenario(t)
	rec := pendingRecord(t, r, q, p)
	reader := r.person("")
	r.az.GrantIn(reader, pQualRead, d)
	for _, who := range []model.PersonID{r.lead(d), reader} {
		_, c, err := r.svc.OpenCertificate(r.as(who), rec.ID)
		wantNoErr(t, err)
		wantNoErr(t, c.Close())
	}
	reads := r.audited("volunteers.qualification-record.read")
	if len(reads) != 2 {
		t.Fatalf("%d qualification-record.read entries, want 2", len(reads))
	}
	for _, e := range reads {
		if e.Subject != p || e.Level != model.SensitivityPersonal || len(e.Fields) != 1 || e.Fields[0] != "certificate" {
			t.Errorf("entry = %+v, want subject the person, level personal, fields [certificate]", e)
		}
	}
}

// F42
func TestOpenCertificate_AuditFails_ReturnsError(t *testing.T) {
	r, q, d, p := uploadScenario(t)
	rec := pendingRecord(t, r, q, p)
	r.aud.Fail(errs.E("test.audit", errs.Unavailable, errors.New("audit down")))
	_, c, err := r.svc.OpenCertificate(r.as(r.lead(d)), rec.ID)
	wantKind(t, err, errs.Unavailable)
	if c != nil {
		t.Error("content was returned although the audit record failed")
		wantNoErr(t, c.Close())
	}
}

// F43 (files.ErrBlobMissing is not exported by files; the kind is what the caller handles)
func TestOpenCertificate_BlobMissing_ReturnsUnavailable(t *testing.T) {
	r, q, d, p := uploadScenario(t)
	rec := pendingRecord(t, r, q, p)
	r.fl.Fail(errs.E("test.files", errs.Unavailable, errors.New("blob missing")))
	_, _, err := r.svc.OpenCertificate(r.as(r.lead(d)), rec.ID)
	wantKind(t, err, errs.Unavailable)
}
