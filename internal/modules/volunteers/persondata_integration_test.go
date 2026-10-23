//go:build integration

package volunteers_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/volunteers"
	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
)

type dataWorld struct {
	r       *rig
	d       model.DepartmentID
	q       vol.Qualification
	lead    model.PersonID // verified, added, booked and created for the subject
	subject model.PersonID
	shift   vol.Shift
	rec     vol.Record
	pos     vol.Position
}

func newDataWorld(t *testing.T) dataWorld {
	t.Helper()
	r := newReal(t)
	d := r.department("Rangers")
	w := dataWorld{r: r, d: d, q: r.qual(d, "Radio"), lead: r.lead(d), subject: r.volunteer("Dandelion")}
	r.ppl.AddLegalName(w.subject, "Dana Underbough-Test")
	r.ppl.Add(w.lead, "Sage Leadfoot")
	w.pos = r.position(d, "Radio op", vol.AccessOpen, w.q.ID)
	var err error
	w.shift, err = r.svc.CreateShift(r.as(w.lead), d, vol.ShiftInput{Position: w.pos.ID, Label: "Morning", Starts: when(0), Ends: when(4), Zone: "UTC", Slots: 3})
	wantNoErr(t, err)
	_, err = r.svc.AddMember(r.as(w.lead), d, w.subject)
	wantNoErr(t, err)
	up, err := r.svc.UploadCertificate(r.as(w.subject), vol.UploadInput{Qualification: w.q.ID, AcquiredOn: jan1, Upload: upload("dana.pdf", pdf())})
	wantNoErr(t, err)
	w.rec, err = r.svc.VerifyRecord(r.as(w.lead), up.ID, vol.VerifyInput{})
	wantNoErr(t, err)
	_, err = r.svc.Assign(r.as(w.lead), w.shift.ID, vol.AssignInput{Person: w.subject})
	wantNoErr(t, err)
	wantNoErr(t, r.svc.SetListed(r.as(w.subject), false))
	return w
}

// F122
func TestExportPerson_ZeroID_ReturnsInvalid(t *testing.T) {
	r := newReal(t)
	_, err := r.mod.ExportPerson(ctxT(t), model.PersonID{})
	wantKind(t, err, errs.Invalid)
	_, err = r.mod.DeletePerson(ctxT(t), model.PersonID{})
	wantKind(t, err, errs.Invalid)
}

func TestExportPerson_ContainsEverythingTheModuleHolds(t *testing.T) {
	w := newDataWorld(t)
	exp, err := w.r.mod.ExportPerson(ctxT(t), w.subject)
	wantNoErr(t, err)
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(exp.Data, &doc); err != nil {
		t.Fatalf("export is not a JSON object: %v", err)
	}
	for _, key := range []string{"memberships", "listed", "signups", "qualification_records", "verified_for_others", "shifts_created"} {
		if _, ok := doc[key]; !ok {
			t.Errorf("export lacks %q", key)
		}
	}
	if string(doc["listed"]) != "false" {
		t.Errorf("listed = %s, want false", doc["listed"])
	}
	var signups, records, members []map[string]any
	wantNoErr(t, json.Unmarshal(doc["signups"], &signups))
	wantNoErr(t, json.Unmarshal(doc["qualification_records"], &records))
	wantNoErr(t, json.Unmarshal(doc["memberships"], &members))
	if len(signups) != 1 || len(records) != 1 || len(members) != 1 {
		t.Errorf("signups/records/memberships = %d/%d/%d, want 1/1/1", len(signups), len(records), len(members))
	}
	body := string(exp.Data)
	// Who booked or verified is shown as a role word, never a name or an ID of another person.
	for _, leak := range []string{"Sage Leadfoot", w.lead.String()} {
		if strings.Contains(body, leak) {
			t.Errorf("the export names another person (%q)", leak)
		}
	}
	if !strings.Contains(body, w.rec.Certificate.String()) {
		t.Error("the export lacks the certificate file ID (files exports the bytes)")
	}
	// The lead's export counts what they verified and created for others, and carries no one else's data.
	le, err := w.r.mod.ExportPerson(ctxT(t), w.lead)
	wantNoErr(t, err)
	var ld map[string]json.RawMessage
	wantNoErr(t, json.Unmarshal(le.Data, &ld))
	if string(ld["verified_for_others"]) != "1" || string(ld["shifts_created"]) != "1" {
		t.Errorf("lead's verified_for_others/shifts_created = %s/%s, want 1/1", ld["verified_for_others"], ld["shifts_created"])
	}
	if strings.Contains(string(le.Data), w.subject.String()) {
		t.Error("the lead's export contains the subject's ID")
	}
}

func TestExportPerson_UnknownPerson_IsEmptyObjectOrNothing(t *testing.T) {
	r := newReal(t)
	exp, err := r.mod.ExportPerson(ctxT(t), model.NewID[model.PersonID]())
	wantNoErr(t, err)
	if len(exp.Data) > 0 {
		var doc map[string]json.RawMessage
		wantNoErr(t, json.Unmarshal(exp.Data, &doc))
	}
}

func TestDeletePerson_RemovesTheirDataAndAnonymizesWhatTheyDidToOthers(t *testing.T) {
	w := newDataWorld(t)
	r := w.r
	del, err := r.mod.DeletePerson(ctxT(t), w.subject)
	wantNoErr(t, err)
	if del.Deleted < 3 { // sign-up, membership, record (+ the unlisted row and the file)
		t.Errorf("Deleted = %d, want at least the sign-up, the membership and the record", del.Deleted)
	}
	if len(del.Kept) != 0 {
		t.Errorf("Kept = %v, want none", del.Kept)
	}
	if n := len(r.fl.Files()); n != 0 {
		t.Errorf("%d certificate files remain after the person was deleted", n)
	}
	exp, err := r.mod.ExportPerson(ctxT(t), w.subject)
	wantNoErr(t, err)
	if len(exp.Data) > 0 {
		var doc map[string]json.RawMessage
		wantNoErr(t, json.Unmarshal(exp.Data, &doc))
		for _, k := range []string{"signups", "memberships", "qualification_records"} {
			if s := strings.TrimSpace(string(doc[k])); s != "" && s != "null" && s != "[]" {
				t.Errorf("after deletion %s = %s", k, s)
			}
		}
	}
	ro, err := r.svc.Roster(r.as(w.lead), w.shift.ID)
	wantNoErr(t, err)
	if len(ro.Entries) != 0 {
		t.Errorf("the deleted person is still on the roster")
	}
	// F124: a second run reports zero.
	again, err := r.mod.DeletePerson(ctxT(t), w.subject)
	wantNoErr(t, err)
	if again.Deleted != 0 || again.Anonymized != 0 || len(again.Kept) != 0 {
		t.Errorf("second DeletePerson = %+v, want zero", again)
	}
}

func TestDeletePerson_Lead_AnonymizesWithoutLosingOthersRecords(t *testing.T) {
	w := newDataWorld(t)
	r := w.r
	del, err := r.mod.DeletePerson(ctxT(t), w.lead)
	wantNoErr(t, err)
	if del.Anonymized < 1 {
		t.Errorf("Anonymized = %d, want the references the lead left on others' records", del.Anonymized)
	}
	// The subject's verified record still counts, though nobody is named as its verifier.
	recs, err := recordItems(r.svc.RecordsOf(r.as(w.subject), w.subject))
	wantNoErr(t, err)
	if len(recs) != 1 || !recs[0].VerifiedBy.IsZero() {
		t.Fatalf("records = %+v, want the subject's record kept with no verifier named", recs)
	}
	if !recs[0].Verified() {
		t.Error("the record stopped being verified when its verifier was deleted")
	}
	mine, err := scheduleItems(r.svc.MySchedule(r.as(w.subject), vol.ScheduleQuery{}))
	wantNoErr(t, err)
	if len(mine) != 1 {
		t.Errorf("the subject's booking disappeared with the lead (%d entries)", len(mine))
	}
	roster, err := r.svc.Roster(r.as(r.boss), w.shift.ID)
	wantNoErr(t, err)
	if len(roster.Entries) != 1 || !roster.Entries[0].BookedBy.IsZero() {
		t.Errorf("roster = %+v, want the booking kept with the booker anonymized", roster.Entries)
	}
	sh, err := r.svc.GetShift(r.as(r.boss), w.shift.ID)
	wantNoErr(t, err)
	if !sh.CreatedBy.IsZero() {
		t.Error("created_by still names the deleted lead")
	}
	again, err := r.mod.DeletePerson(ctxT(t), w.lead)
	wantNoErr(t, err)
	if again.Deleted != 0 || again.Anonymized != 0 {
		t.Errorf("second DeletePerson = %+v", again)
	}
}

func TestDeletePerson_WaiverIsAnonymizedToo(t *testing.T) {
	w := newDataWorld(t)
	r := w.r
	other := r.volunteer("Tumbleweed")
	_, err := r.svc.Assign(r.as(w.lead), w.shift.ID, vol.AssignInput{Person: other, Waive: true})
	wantNoErr(t, err)
	_, err = r.mod.DeletePerson(ctxT(t), w.lead)
	wantNoErr(t, err)
	ro, err := r.svc.Roster(r.as(r.boss), w.shift.ID)
	wantNoErr(t, err)
	found := false
	for _, e := range ro.Entries {
		if e.Person != other {
			continue
		}
		found = true
		if !e.WaivedBy.IsZero() {
			t.Error("waived_by still names the deleted lead")
		}
		if !e.Waived {
			t.Error("the waiver flag was lost with the deleted lead (D51 c)")
		}
	}
	if !found {
		t.Error("the waived person is missing from the roster")
	}
}

// F123
func TestDeletePerson_FilesFail_ReturnsError(t *testing.T) {
	w := newDataWorld(t)
	w.r.fl.Fail(errs.E("test.files", errs.Unavailable, errors.New("store down")))
	_, err := w.r.mod.DeletePerson(ctxT(t), w.subject)
	wantKind(t, err, errs.Unavailable)
	w.r.fl.Fail(nil)
	// Nothing was half-deleted: the person's data is still all there.
	recs, err := recordItems(w.r.svc.RecordsOf(w.r.as(w.subject), w.subject))
	wantNoErr(t, err)
	mine, err2 := scheduleItems(w.r.svc.MySchedule(w.r.as(w.subject), vol.ScheduleQuery{}))
	wantNoErr(t, err2)
	if len(recs) != 1 || len(mine) != 1 {
		t.Errorf("records/bookings = %d/%d after a failed delete, want 1/1 (people rolls back)", len(recs), len(mine))
	}
}

// ---- read-only copy, store failures, context ----

func TestWrites_ReadOnlyCopy_RealDatabase(t *testing.T) {
	w := newDataWorld(t)
	ro := newRig(t, opts{db: readOnlyDB{DB: w.r.db}, share: w.r})
	lead, subject := ro.as(w.lead), ro.as(w.subject)
	other := ro.volunteer("Tumbleweed")
	_ = other
	tests := []struct {
		name string
		call func() error
	}{
		{"SignUp", func() error { return errOf(ro.svc.SignUp(ro.as(ro.volunteer("")), w.shift.ID)) }},
		{"CancelSignup", func() error { return ro.svc.CancelSignup(subject, ro.bookingOf(w.subject, w.shift.ID)) }}, //nolint:contextcheck // the test helper builds its own context
		{"CancelShift", func() error { return ro.svc.CancelShift(lead, w.shift.ID) }},
		{"UpdateShift", func() error { return errOf(ro.svc.UpdateShift(lead, w.shift.ID, shiftInputOf(w.shift))) }},
		{"CreatePosition", func() error {
			return errOf(ro.svc.CreatePosition(lead, w.d, vol.PositionInput{Name: "New", Access: vol.AccessOpen}))
		}},
		{"ArchiveQualification", func() error { return ro.svc.ArchiveQualification(lead, w.q.ID) }},
		{"RemoveRecord", func() error { return ro.svc.RemoveRecord(lead, w.rec.ID) }},
		{"RemoveMember", func() error { return ro.svc.RemoveMember(lead, w.d, w.subject) }},
		{"Leave", func() error { return ro.svc.Leave(subject, w.d) }},
		{"SetListed", func() error { return ro.svc.SetListed(subject, true) }},
		{"UploadCertificate", func() error {
			return errOf(ro.svc.UploadCertificate(subject, vol.UploadInput{Qualification: w.q.ID, AcquiredOn: jan1, Upload: upload("c.pdf", pdf())}))
		}},
		{"DeletePerson", func() error { return errOf(ro.mod.DeletePerson(ctxT(t), w.subject)) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			wantKind(t, err, errs.Unavailable)
			if !isReadOnly(err) {
				t.Fatalf("%v does not wrap db.ErrReadOnly", err)
			}
		})
	}
	// Reads keep working.
	if _, err := ro.svc.Roster(lead, w.shift.ID); err != nil {
		t.Errorf("Roster on a read-only copy: %v", err)
	}
	if _, err := scheduleItems(ro.svc.MySchedule(subject, vol.ScheduleQuery{})); err != nil {
		t.Errorf("MySchedule on a read-only copy: %v", err)
	}
	if _, err := ro.mod.ExportPerson(ctxT(t), w.subject); err != nil {
		t.Errorf("ExportPerson on a read-only copy: %v", err)
	}
	// And nothing changed.
	if n, _, _ := exportCounts(t, w.r, w.subject); n != 1 {
		t.Errorf("sign-ups = %d after refused writes", n)
	}
}

func TestStoreFailure_PropagatesKind(t *testing.T) {
	w := newDataWorld(t)
	// db.None refuses every statement with Unavailable; Timeout and Canceled come from the
	// context tests below.
	for _, name := range []string{"unavailable"} {
		kind := errs.Unavailable
		t.Run(name, func(t *testing.T) {
			bad := newRig(t, opts{db: db.None(), share: w.r})
			ctx := bad.as(w.subject)
			reads := map[string]func() error{
				"ListShifts":         func() error { return errOf(shiftItems(bad.svc.ListShifts(ctx, vol.ShiftQuery{}))) },
				"GetShift":           func() error { return errOf(bad.svc.GetShift(ctx, w.shift.ID)) },
				"MySchedule":         func() error { return errOf(scheduleItems(bad.svc.MySchedule(ctx, vol.ScheduleQuery{}))) },
				"MyDepartments":      func() error { return errOf(bad.svc.MyDepartments(ctx)) },
				"Listed":             func() error { return errOf(bad.svc.Listed(ctx)) },
				"ListQualifications": func() error { return errOf(bad.svc.ListQualifications(ctx, vol.QualificationQuery{})) },
				"ListPositions":      func() error { return errOf(bad.svc.ListPositions(ctx, w.d, false)) },
				"RecordsOf":          func() error { return errOf(recordItems(bad.svc.RecordsOf(ctx, w.subject))) },
				"SignUp":             func() error { return errOf(bad.svc.SignUp(ctx, w.shift.ID)) },
				"SetListed":          func() error { return bad.svc.SetListed(ctx, true) },
				"ExportPerson":       func() error { return errOf(bad.mod.ExportPerson(ctxT(t), w.subject)) },
				"DeletePerson":       func() error { return errOf(bad.mod.DeletePerson(ctxT(t), w.subject)) },
			}
			for rname, call := range reads {
				t.Run(rname, func(t *testing.T) { wantKind(t, call(), kind) })
			}
		})
	}
}

func TestUseCases_CancelledContext_ReturnsCanceled(t *testing.T) {
	w := newDataWorld(t)
	ctx, cancel := context.WithCancel(w.r.as(w.subject))
	cancel()
	_, err := shiftItems(w.r.svc.ListShifts(ctx, vol.ShiftQuery{}))
	wantKind(t, err, errs.Canceled)
	_, err = scheduleItems(w.r.svc.MySchedule(ctx, vol.ScheduleQuery{}))
	wantKind(t, err, errs.Canceled)
	_, err = w.r.svc.SignUp(ctx, w.shift.ID)
	wantKind(t, err, errs.Canceled)
	_, err = w.r.mod.ExportPerson(ctx, w.subject)
	wantKind(t, err, errs.Canceled)
}

func TestUseCases_DeadlinePassed_ReturnsTimeout(t *testing.T) {
	w := newDataWorld(t)
	ctx, cancel := context.WithDeadline(w.r.as(w.subject), time.Now().Add(-time.Second))
	defer cancel()
	_, err := shiftItems(w.r.svc.ListShifts(ctx, vol.ShiftQuery{}))
	wantKind(t, err, errs.Timeout)
	_, err = scheduleItems(w.r.svc.MySchedule(ctx, vol.ScheduleQuery{}))
	wantKind(t, err, errs.Timeout)
	_, err = w.r.svc.SignUp(ctx, w.shift.ID)
	wantKind(t, err, errs.Timeout)
}

// ---- empty inputs and batch limits ----

func TestEmptyReads_ReturnEmptyNotErrors(t *testing.T) {
	r := newReal(t)
	p := r.volunteer("Dandelion")
	ctx := r.as(p)
	if got, err := shiftItems(r.svc.ListShifts(ctx, vol.ShiftQuery{})); err != nil || len(got) != 0 {
		t.Errorf("ListShifts on an empty database = %d, %v", len(got), err)
	}
	if got, err := scheduleItems(r.svc.MySchedule(ctx, vol.ScheduleQuery{})); err != nil || len(got) != 0 {
		t.Errorf("MySchedule = %d, %v", len(got), err)
	}
	if got, err := r.svc.MyDepartments(ctx); err != nil || len(got) != 0 {
		t.Errorf("MyDepartments = %d, %v", len(got), err)
	}
	if got, err := recordItems(r.svc.RecordsOf(ctx, p)); err != nil || len(got) != 0 {
		t.Errorf("RecordsOf = %d, %v", len(got), err)
	}
	if got, err := r.svc.ListQualifications(ctx, vol.QualificationQuery{}); err != nil || len(got) != 0 {
		t.Errorf("ListQualifications = %d, %v", len(got), err)
	}
	if got, err := r.svc.PendingRecords(r.as(r.boss), vol.PendingQuery{}); err != nil || len(got) != 0 {
		t.Errorf("PendingRecords = %d, %v", len(got), err)
	}
	if got, err := r.svc.Sections(ctx, nil); err != nil || len(got) != 0 {
		t.Errorf("Sections(nil) = %d, %v", len(got), err)
	}
}

func TestSections_BatchLimit(t *testing.T) {
	r := newReal(t)
	ctx := r.as(r.boss)
	ids := make([]model.PersonID, vol.MaxIDs)
	for i := range ids {
		ids[i] = model.NewID[model.PersonID]()
	}
	if _, err := r.svc.Sections(ctx, ids); err != nil {
		t.Errorf("exactly MaxIDs people: %v", err)
	}
	// The spec's failure table names no limit error for Sections; one more than MaxIDs
	// must at least not corrupt the answer.
	ids = append(ids, model.NewID[model.PersonID]())
	if _, err := r.svc.Sections(ctx, ids); err != nil && !errs.Is(err, errs.Invalid) {
		t.Errorf("MaxIDs+1 people: %v, want nil or Invalid", err)
	}
}

func TestModule_ExposesRoutesAndOpenAPI(t *testing.T) {
	r := newReal(t)
	rp, ok := r.mod.(interface{ OpenAPI() []byte })
	if !ok || len(rp.OpenAPI()) == 0 {
		t.Fatal("the module must describe its routes (module.APIDescriber)")
	}
	var doc map[string]any
	if err := json.Unmarshal(rp.OpenAPI(), &doc); err != nil {
		t.Fatalf("the OpenAPI fragment is not JSON: %v", err)
	}
	body := string(rp.OpenAPI())
	for _, schema := range []string{
		"VolunteersQualification", "VolunteersRecord", "VolunteersPosition", "VolunteersMember", "VolunteersShift",
		"VolunteersShiftSummary", "VolunteersSignup", "VolunteersRoster", "VolunteersScheduleEntry",
	} {
		if !strings.Contains(body, schema) {
			t.Errorf("the OpenAPI fragment lacks schema %s", schema)
		}
	}
	_ = volunteers.Manifest
}
