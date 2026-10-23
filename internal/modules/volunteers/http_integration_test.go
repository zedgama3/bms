//go:build integration

package volunteers_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/privacy/privacytest"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/volunteers"
	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
)

// HTTP tests over a real database: the handlers, status codes and bodies the spec promises
// (module-volunteers.md, "Routes" and the failure-mode table). Every person is invented.

func decodeBody[T any](t testing.TB, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("body is not the expected JSON (%v): %q", err, rec.Body.String())
	}
	return v
}

// wantCode checks that p carries a field problem with the given code (whatever field the
// service attached it to) and returns it.
func wantCode(t testing.TB, p httpx.Problem, code string) httpx.Field {
	t.Helper()
	for _, f := range p.Fields {
		if f.Code == code {
			return f
		}
	}
	t.Fatalf("problem %+v has no field problem with code %q", p, code)
	return httpx.Field{}
}

func wantStatus(t testing.TB, rec *httptest.ResponseRecorder, status int) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d (body %q)", rec.Code, status, rec.Body.String())
	}
}

// multipartBody builds a form with the text fields first, then (when file is not nil) the
// file part named "file".
func multipartBody(t testing.TB, fields [][2]string, fileName, partType string, file []byte) (*bytes.Reader, string) {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	for _, kv := range fields {
		if err := mw.WriteField(kv[0], kv[1]); err != nil {
			t.Fatalf("WriteField: %v", err)
		}
	}
	if file != nil {
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", `form-data; name="file"; filename="`+fileName+`"`)
		if partType != "" {
			h.Set("Content-Type", partType)
		}
		pw, err := mw.CreatePart(h)
		if err != nil {
			t.Fatalf("CreatePart: %v", err)
		}
		if _, err := pw.Write(file); err != nil {
			t.Fatalf("part write: %v", err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("multipart close: %v", err)
	}
	return bytes.NewReader(b.Bytes()), mw.FormDataContentType()
}

// ---- {"items": [...]} wrapping ----

func TestHTTP_Lists_AreWrappedInItems(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	q := r.qual(s.dept, "Radio")
	v := r.volunteer("Dandelion")
	r.member(s.dept, v)
	r.book(v, s.sh.ID)
	if _, err := r.svc.UploadCertificate(r.as(v), vol.UploadInput{Qualification: q.ID, AcquiredOn: jan1, Upload: upload("c.pdf", pdf())}); err != nil {
		t.Fatalf("UploadCertificate: %v", err)
	}
	d := s.dept.String()
	for _, tc := range []struct {
		name, path string
		as         model.PersonID
	}{
		{"shifts", "/shifts", v},
		{"qualifications", "/qualifications", v},
		{"positions", "/departments/" + d + "/positions", v},
		{"members", "/departments/" + d + "/members", s.lead},
		{"my departments", "/me/departments", v},
		{"my schedule", "/me/schedule", v},
		{"records of a person", "/people/" + v.String() + "/qualifications", v},
		{"pending records", "/qualification-records?department=" + d, s.lead},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := r.call(tc.as, "GET", tc.path, nil)
			wantStatus(t, rec, http.StatusOK)
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				t.Errorf("Content-Type = %q", ct)
			}
			var top map[string]json.RawMessage
			if err := json.Unmarshal(rec.Body.Bytes(), &top); err != nil {
				t.Fatalf("body is not a JSON object: %v (%q)", err, rec.Body.String())
			}
			raw, ok := top["items"]
			for k := range top { // items, plus next on the paged lists and self_upload on one's own records
				if k != "items" && k != "next" && k != "self_upload" {
					t.Fatalf("body keys = %v, want items (and next or self_upload)", keysOf(top))
				}
			}
			if !ok {
				t.Fatalf("body keys = %v, want items", keysOf(top))
			}
			var items []json.RawMessage
			if err := json.Unmarshal(raw, &items); err != nil || len(items) == 0 {
				t.Fatalf("items = %s (%v), want a non-empty array", raw, err)
			}
		})
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// F156: an empty list is {"items":[]}: never {}, never null, never a bare array. The table
// covers every list route.
func TestListHandlers_Empty_EncodeEmptyItems(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	v := r.volunteer("Dandelion")
	lead := r.lead(d)
	for _, tc := range []struct {
		name, path string
		as         model.PersonID
	}{
		{"shifts", "/shifts", v},
		{"my schedule upcoming", "/me/schedule", v},
		{"my schedule past", "/me/schedule?past=true", v},
		{"my departments", "/me/departments", v},
		{"qualifications", "/qualifications", v},
		{"positions", "/departments/" + d.String() + "/positions", v},
		{"members", "/departments/" + d.String() + "/members", lead},
		{"records of a person", "/people/" + v.String() + "/qualifications", v},
		{"records of a person, as a reader", "/people/" + v.String() + "/qualifications", lead},
		{"pending records", "/qualification-records?department=" + d.String(), lead},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := r.call(tc.as, "GET", tc.path, nil)
			wantStatus(t, rec, http.StatusOK)
			var top map[string]json.RawMessage
			if err := json.Unmarshal(rec.Body.Bytes(), &top); err != nil {
				t.Fatalf("body is not a JSON object: %v (%q)", err, rec.Body.String())
			}
			if got := strings.TrimSpace(string(top["items"])); got != "[]" {
				t.Errorf("items = %q in %q, want []", got, rec.Body.String())
			}
			if _, ok := top["next"]; ok {
				t.Errorf("an empty list carries next: %q", rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "null") {
				t.Errorf("body carries null: %q", rec.Body.String())
			}
		})
	}
}

// ---- status codes by kind ----

func TestHTTP_Statuses_ByKind(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	stranger := r.person("Quillfeather") // no shifts.signup
	v := r.volunteer("Dandelion")
	unknown := model.NewID[vol.ShiftID]().String()

	t.Run("401 no session", func(t *testing.T) {
		wantProblem(t, r.call(model.PersonID{}, "GET", "/shifts/"+s.sh.ID.String(), nil), http.StatusUnauthorized, "unauthenticated")
	})
	t.Run("403 permission: sign-up without shifts.signup", func(t *testing.T) {
		wantProblem(t, r.call(stranger, "POST", "/shifts/"+s.sh.ID.String()+"/signups", nil), http.StatusForbidden, "permission")
	})
	t.Run("403 permission: create a shift as a plain volunteer", func(t *testing.T) {
		in := shiftInputOf(s.sh)
		wantProblem(t, r.call(v, "POST", "/departments/"+s.dept.String()+"/shifts", in), http.StatusForbidden, "permission")
	})
	t.Run("403 permission: roster as a volunteer", func(t *testing.T) {
		wantProblem(t, r.call(v, "GET", "/shifts/"+s.sh.ID.String()+"/roster", nil), http.StatusForbidden, "permission")
	})
	t.Run("404 not found: unknown shift", func(t *testing.T) {
		wantProblem(t, r.call(v, "GET", "/shifts/"+unknown, nil), http.StatusNotFound, "not_found")
		wantProblem(t, r.call(v, "POST", "/shifts/"+unknown+"/signups", nil), http.StatusNotFound, "not_found")
	})
	t.Run("404 not found: another person's signup", func(t *testing.T) {
		su := r.book(r.volunteer("Tumbleweed"), s.sh.ID)
		wantProblem(t, r.call(v, "DELETE", "/signups/"+su.ID.String(), nil), http.StatusNotFound, "not_found")
	})
	t.Run("400 invalid: shift input out of range, with field codes", func(t *testing.T) {
		in := shiftInputOf(s.sh)
		in.Ends = in.Starts.Add(-time.Hour)
		in.Slots = 0
		p := wantProblem(t, r.call(s.lead, "POST", "/departments/"+s.dept.String()+"/shifts", in), http.StatusBadRequest, "invalid")
		if len(p.Fields) == 0 {
			t.Fatalf("no field problems in %+v", p)
		}
	})
	t.Run("201 created, 200 updated, 204 canceled", func(t *testing.T) {
		in := shiftInputOf(s.sh)
		in.Label = "Afternoon dirt"
		in.Starts, in.Ends = when(24), when(28)
		rec := r.call(s.lead, "POST", "/departments/"+s.dept.String()+"/shifts", in)
		wantStatus(t, rec, http.StatusCreated)
		created := decodeBody[vol.Shift](t, rec)
		if created.ID.IsZero() || created.Label != "Afternoon dirt" {
			t.Fatalf("created = %+v", created)
		}
		in.Label = "Afternoon dirt, renamed"
		rec = r.call(s.lead, "PUT", "/shifts/"+created.ID.String(), in)
		wantStatus(t, rec, http.StatusOK)
		if got := decodeBody[vol.Shift](t, rec); got.Label != in.Label {
			t.Errorf("updated label = %q", got.Label)
		}
		rec = r.call(s.lead, "POST", "/shifts/"+created.ID.String()+"/cancel", nil)
		wantStatus(t, rec, http.StatusNoContent)
		if rec.Body.Len() != 0 {
			t.Errorf("204 carried a body: %q", rec.Body.String())
		}
		wantProblem(t, r.call(s.lead, "POST", "/shifts/"+created.ID.String()+"/cancel", nil), http.StatusConflict, "conflict")
	})
	t.Run("sign-up 201 then duplicate 409, cancel 204", func(t *testing.T) {
		p := r.volunteer("Marigold")
		rec := r.call(p, "POST", "/shifts/"+s.sh.ID.String()+"/signups", nil)
		wantStatus(t, rec, http.StatusCreated)
		su := decodeBody[vol.Signup](t, rec)
		if su.Person != p || su.Shift != s.sh.ID || su.Status != vol.SignupBooked {
			t.Fatalf("signup = %+v", su)
		}
		wantCode(t, wantProblem(t, r.call(p, "POST", "/shifts/"+s.sh.ID.String()+"/signups", nil), http.StatusConflict, "conflict"), "duplicate")
		wantStatus(t, r.call(p, "DELETE", "/signups/"+su.ID.String(), nil), http.StatusNoContent)
	})
}

func TestHTTP_ReadOnlyCopy_WritesAre503(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	v := r.volunteer("Dandelion")
	ro := newRig(t, opts{db: readOnlyDB{r.db}, real: true, share: r})
	rec := ro.call(v, "POST", "/shifts/"+s.sh.ID.String()+"/signups", nil)
	wantStatus(t, rec, http.StatusServiceUnavailable)
	// A read still works on the same copy.
	wantStatus(t, ro.call(v, "GET", "/shifts/"+s.sh.ID.String(), nil), http.StatusOK)
}

// ---- conflict field codes ----

func TestHTTP_SignUp_ConflictCodes(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	q := r.qual(d, "Radio")
	open := r.position(d, "Dirt crew", vol.AccessOpen)
	radio := r.position(d, "Radio op", vol.AccessOpen, q.ID)
	inner := r.position(d, "Inner circle", vol.AccessMembers)

	t.Run("full", func(t *testing.T) {
		sh := r.shift(d, open.ID, model.EventID{}, when(100), 2, 1)
		r.book(r.volunteer("Dandelion"), sh.ID)
		p := wantProblem(t, r.call(r.volunteer("Tumbleweed"), "POST", "/shifts/"+sh.ID.String()+"/signups", nil), http.StatusConflict, "conflict")
		wantField(t, p, "shift", "full")
	})
	t.Run("overlap names the clashing shift", func(t *testing.T) {
		a := r.shift(d, open.ID, model.EventID{}, when(10), 4, 3)
		b := r.shift(d, open.ID, model.EventID{}, when(12), 4, 3)
		p := r.volunteer("Marigold")
		r.book(p, a.ID)
		pr := wantProblem(t, r.call(p, "POST", "/shifts/"+b.ID.String()+"/signups", nil), http.StatusConflict, "conflict")
		f := wantField(t, pr, "shift", "overlap")
		if f.Params["shift"] != a.ID.String() {
			t.Errorf("overlap params = %v, want shift %s", f.Params, a.ID)
		}
	})
	t.Run("unqualified lists the missing qualifications", func(t *testing.T) {
		sh := r.shift(d, radio.ID, model.EventID{}, when(30), 2, 3)
		p := r.volunteer("Fennel")
		pr := wantProblem(t, r.call(p, "POST", "/shifts/"+sh.ID.String()+"/signups", nil), http.StatusConflict, "conflict")
		f := wantCode(t, pr, "unqualified")
		if f.Params["missing"] != q.ID.String() {
			t.Errorf("unqualified params = %v, want missing %s", f.Params, q.ID)
		}
	})
	t.Run("not_member", func(t *testing.T) {
		sh := r.shift(d, inner.ID, model.EventID{}, when(40), 2, 3)
		lead := r.volunteeringLead(d)
		wantCode(t, wantProblem(t, r.call(lead, "POST", "/shifts/"+sh.ID.String()+"/signups", nil), http.StatusConflict, "conflict"), "not_member")
	})
}

// The sign-up body is ignored: only Assign waives (D42).
func TestHTTP_SignUp_IgnoresAWaiveField(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	q := r.qual(d, "Radio")
	radio := r.position(d, "Radio op", vol.AccessOpen, q.ID)
	sh := r.shift(d, radio.ID, model.EventID{}, when(30), 2, 3)
	p := r.volunteer("Fennel")
	for _, body := range []any{`{"waive":true}`, `{"waive":true,"person":"` + p.String() + `"}`, map[string]any{"waive": true, "waived_by": p.String()}} {
		wantCode(t, wantProblem(t, r.call(p, "POST", "/shifts/"+sh.ID.String()+"/signups", body), http.StatusConflict, "conflict"), "unqualified")
	}
	sched, err := scheduleItems(r.svc.MySchedule(r.as(p), vol.ScheduleQuery{}))
	wantNoErr(t, err)
	if len(sched) != 0 {
		t.Errorf("a sign-up with waive:true booked the person (%d bookings)", len(sched))
	}
	// A lead who is not a member of nothing special: even a lead signing themself up cannot waive.
	lead := r.volunteeringLead(d)
	wantCode(t, wantProblem(t, r.call(lead, "POST", "/shifts/"+sh.ID.String()+"/signups", `{"waive":true}`), http.StatusConflict, "conflict"), "unqualified")
}

func TestHTTP_Assign_WaiveOnlyThroughAssign(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	q := r.qual(d, "Radio")
	radio := r.position(d, "Radio op", vol.AccessOpen, q.ID)
	sh := r.shift(d, radio.ID, model.EventID{}, when(30), 2, 3)
	lead := r.lead(d)
	p := r.person("Fennel")
	path := "/shifts/" + sh.ID.String() + "/assign"

	wantCode(t, wantProblem(t, r.call(lead, "POST", path, vol.AssignInput{Person: p}), http.StatusConflict, "conflict"), "unqualified")
	wantProblem(t, r.call(r.volunteer("Marigold"), "POST", path, vol.AssignInput{Person: p, Waive: true}), http.StatusForbidden, "permission")
	wantField(t, wantProblem(t, r.call(lead, "POST", path, vol.AssignInput{}), http.StatusBadRequest, "invalid"), "person", "required")

	rec := r.call(lead, "POST", path, vol.AssignInput{Person: p, Waive: true})
	wantStatus(t, rec, http.StatusCreated)
	su := decodeBody[vol.Signup](t, rec)
	if su.Person != p || su.WaivedBy != lead {
		t.Errorf("signup = %+v, want person %v waived by %v", su, p, lead)
	}
	ro := r.call(lead, "GET", "/shifts/"+sh.ID.String()+"/roster", nil)
	wantStatus(t, ro, http.StatusOK)
	roster := decodeBody[vol.Roster](t, ro)
	if len(roster.Entries) != 1 || roster.Entries[0].WaivedBy != lead {
		t.Errorf("roster = %+v, want the waiver flagged", roster.Entries)
	}
}

// ---- certificate upload and serve ----

func uploadWorld(t *testing.T) (*rig, vol.Qualification, model.DepartmentID, model.PersonID) {
	t.Helper()
	return uploadScenario(t)
}

func (r *rig) upload(p model.PersonID, fields [][2]string, name, partType string, data []byte) *httptest.ResponseRecorder {
	r.t.Helper()
	body, ct := multipartBody(r.t, fields, name, partType, data)
	return r.callRaw(p, "POST", "/me/qualifications", body, ct)
}

func TestHTTP_UploadCertificate_Multipart_StoresAPendingRecord(t *testing.T) {
	r, q, _, p := uploadWorld(t)
	rec := r.upload(p, [][2]string{
		{"qualification", q.ID.String()}, {"acquired_on", "2026-01-01"}, {"expires_on", "2027-01-01"},
	}, "radio.pdf", "application/pdf", pdf())
	wantStatus(t, rec, http.StatusCreated)
	got := decodeBody[vol.Record](t, rec)
	if got.Person != p || got.Qualification != q.ID || got.Certificate.IsZero() || !got.VerifiedBy.IsZero() {
		t.Fatalf("record = %+v, want a pending record of the uploader with a certificate", got)
	}
	if got.AcquiredOn != (vol.Date{Year: 2026, Month: time.January, Day: 1}) || got.ExpiresOn != (vol.Date{Year: 2027, Month: time.January, Day: 1}) {
		t.Errorf("dates = %v / %v", got.AcquiredOn, got.ExpiresOn)
	}
	if n := len(r.fl.Files()); n != 1 {
		t.Errorf("%d files stored, want 1", n)
	}
	// The body names the file by ID only: no file name, no bytes.
	if s := rec.Body.String(); strings.Contains(s, "radio.pdf") || strings.Contains(s, "%PDF") {
		t.Errorf("response leaks the upload: %s", s)
	}
}

func TestHTTP_UploadCertificate_Refusals(t *testing.T) {
	r, q, _, p := uploadWorld(t)
	good := func(extra ...[2]string) [][2]string {
		return append([][2]string{{"qualification", q.ID.String()}, {"acquired_on", "2026-01-01"}}, extra...)
	}
	tests := []struct {
		name     string
		fields   [][2]string
		file     []byte
		partType string
		fileName string
		status   int
		code     string
		field    string
		fcode    string
	}{
		{"qualification not a UUID", [][2]string{{"qualification", "xyz"}, {"acquired_on", "2026-01-01"}}, pdf(), "application/pdf", "c.pdf", 400, "invalid", "qualification", "malformed"},
		{"acquired_on malformed", [][2]string{{"qualification", q.ID.String()}, {"acquired_on", "01/02/2026"}}, pdf(), "application/pdf", "c.pdf", 400, "invalid", "acquired_on", "malformed"},
		{"expires_on malformed", good([2]string{"expires_on", "next year"}), pdf(), "application/pdf", "c.pdf", 400, "invalid", "expires_on", "malformed"},
		{"expires before acquired", good([2]string{"expires_on", "2025-01-01"}), pdf(), "application/pdf", "c.pdf", 400, "invalid", "", ""},
		{"no file part", good(), nil, "", "", 400, "invalid", "file", "required"},
		{"not a certificate type", good(), []byte("just some text, not a certificate"), "text/plain", "notes.txt", 400, "invalid", "", ""},
		{"empty file", good(), []byte{}, "application/pdf", "empty.pdf", 400, "invalid", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := r.upload(p, tc.fields, tc.fileName, tc.partType, tc.file)
			pr := wantProblem(t, rec, tc.status, tc.code)
			if tc.field != "" {
				wantField(t, pr, tc.field, tc.fcode)
			}
			if len(r.fl.Files()) != 0 || len(r.fl.Staged()) != 0 {
				t.Errorf("a refused upload left bytes behind (%d files, %d staged)", len(r.fl.Files()), len(r.fl.Staged()))
			}
		})
	}
	t.Run("body over the route cap is 413", func(t *testing.T) {
		body, ct := multipartBody(t, good(), "c.pdf", "application/pdf", append(pdf(), make([]byte, 4096)...))
		req := httptest.NewRequestWithContext(asPerson(ctxT(t), p), "POST", apiBase+"/me/qualifications", body)
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()
		req.Body = http.MaxBytesReader(rec, req.Body, 512)
		r.routeMux().ServeHTTP(rec, req)
		wantProblem(t, rec, http.StatusRequestEntityTooLarge, httpx.CodePayloadTooLarge)
		if len(r.fl.Files()) != 0 {
			t.Error("an over-cap upload was stored")
		}
	})
	t.Run("second pending upload is a conflict", func(t *testing.T) {
		wantStatus(t, r.upload(p, good(), "c.pdf", "application/pdf", pdf()), http.StatusCreated)
		wantCode(t, wantProblem(t, r.upload(p, good(), "c.pdf", "application/pdf", pdf()), http.StatusConflict, "conflict"), "pending_exists")
	})
}

func TestHTTP_UploadCertificate_SelfUploadOff_Is501(t *testing.T) {
	r, q, _, p := uploadWorld(t)
	r.set.Set(volunteers.SettingSelfUpload, "false")
	rec := r.upload(p, [][2]string{{"qualification", q.ID.String()}, {"acquired_on", "2026-01-01"}}, "c.pdf", "application/pdf", pdf())
	wantProblem(t, rec, http.StatusNotImplemented, "unsupported")
	if len(r.fl.Files()) != 0 {
		t.Error("a file was kept although self-upload is off")
	}
}

func TestHTTP_UploadCertificate_NoSession_Is401(t *testing.T) {
	r, q, _, _ := uploadWorld(t)
	rec := r.upload(model.PersonID{}, [][2]string{{"qualification", q.ID.String()}}, "c.pdf", "application/pdf", pdf())
	wantProblem(t, rec, http.StatusUnauthorized, "unauthenticated")
}

func TestHTTP_Certificate_ServedWithSafeHeaders(t *testing.T) {
	r, q, d, p := uploadWorld(t)
	up := r.upload(p, [][2]string{{"qualification", q.ID.String()}, {"acquired_on", "2026-01-01"}}, "radio card.pdf", "application/pdf", pdf())
	wantStatus(t, up, http.StatusCreated)
	rc := decodeBody[vol.Record](t, up)
	path := "/qualification-records/" + rc.ID.String() + "/certificate"

	t.Run("the person: 200, headers, bytes, no audit", func(t *testing.T) {
		rec := r.call(p, "GET", path, nil)
		wantStatus(t, rec, http.StatusOK)
		h := rec.Header()
		if ct := h.Get("Content-Type"); ct != "application/pdf" {
			t.Errorf("Content-Type = %q", ct)
		}
		if cd := h.Get("Content-Disposition"); !strings.HasPrefix(cd, "inline") || !strings.Contains(cd, "radio") {
			t.Errorf("Content-Disposition = %q", cd)
		}
		if csp := h.Get("Content-Security-Policy"); csp != "default-src 'none'" {
			t.Errorf("Content-Security-Policy = %q", csp)
		}
		if etag := h.Get("ETag"); !strings.HasPrefix(etag, `"`) || len(etag) < 10 {
			t.Errorf("ETag = %q", etag)
		}
		if cc := h.Get("Cache-Control"); strings.Contains(cc, "public") {
			t.Errorf("Cache-Control = %q on a personal file", cc)
		}
		if !bytes.Equal(rec.Body.Bytes(), pdf()) {
			t.Errorf("served %d bytes, want the upload (%d)", rec.Body.Len(), len(pdf()))
		}
		if n := len(r.audited("volunteers.qualification-record.read")); n != 0 {
			t.Errorf("a self read wrote %d audit entries", n)
		}
	})
	t.Run("a lead with qualifications.read: 200 and audited", func(t *testing.T) {
		lead := r.lead(d)
		rec := r.call(lead, "GET", path, nil)
		wantStatus(t, rec, http.StatusOK)
		a := r.audited("volunteers.qualification-record.read")
		if len(a) != 1 || a[0].Subject != p || len(a[0].Fields) != 1 || a[0].Fields[0] != "certificate" {
			t.Fatalf("audit = %+v, want one read of the person's certificate", a)
		}
	})
	t.Run("Range is honoured", func(t *testing.T) {
		ctx := asPerson(ctxT(t), p)
		req := httptest.NewRequestWithContext(ctx, "GET", apiBase+path, nil)
		req.Header.Set("Range", "bytes=0-3")
		rec := httptest.NewRecorder()
		r.routeMux().ServeHTTP(rec, req)
		wantStatus(t, rec, http.StatusPartialContent)
		if rec.Body.String() != "%PDF" {
			t.Errorf("range body = %q", rec.Body.String())
		}
	})
	t.Run("another volunteer: 404, never 403", func(t *testing.T) {
		wantProblem(t, r.call(r.volunteer("Marigold"), "GET", path, nil), http.StatusNotFound, "not_found")
	})
	t.Run("a lead of another department: 404", func(t *testing.T) {
		other := r.lead(r.department("Greeters"))
		wantProblem(t, r.call(other, "GET", path, nil), http.StatusNotFound, "not_found")
	})
	t.Run("no session: 401", func(t *testing.T) {
		wantProblem(t, r.call(model.PersonID{}, "GET", path, nil), http.StatusUnauthorized, "unauthenticated")
	})
	t.Run("unknown record: 404", func(t *testing.T) {
		wantProblem(t, r.call(p, "GET", "/qualification-records/"+model.NewID[vol.RecordID]().String()+"/certificate", nil), http.StatusNotFound, "not_found")
	})
}

func TestHTTP_Certificate_RecordWithoutOne_Is404(t *testing.T) {
	r, q, d, p := uploadWorld(t)
	lead := r.lead(d)
	rec := r.call(lead, "POST", "/people/"+p.String()+"/qualifications", vol.RecordInput{Qualification: q.ID, Person: p, AcquiredOn: jan1})
	wantStatus(t, rec, http.StatusCreated)
	rc := decodeBody[vol.Record](t, rec)
	wantProblem(t, r.call(p, "GET", "/qualification-records/"+rc.ID.String()+"/certificate", nil), http.StatusNotFound, "not_found")
}

// ---- nothing hidden leaks into a body ----

func TestHTTP_Bodies_NeverLeakHiddenFields(t *testing.T) {
	w := newMatesWorld(t, false)
	viewer := w.r.volunteer("Marigold")
	w.r.ppl.AddLegalName(viewer, "Viv Viewer-Test")
	legal := []string{legalAlden, legalBriar, legalShy, legalNone, "Viv Viewer-Test", "Quillfeather", "Underbough"}
	hidden := []string{"legal_name", "pronouns", "email", "phone", "person", "person_id", "address", "emergency"}

	check := func(t *testing.T, rec *httptest.ResponseRecorder, names ...string) {
		t.Helper()
		wantStatus(t, rec, http.StatusOK)
		privacytest.AssertHidden(t, rec.Body.Bytes(), names...)
		for _, l := range legal {
			if strings.Contains(rec.Body.String(), l) {
				t.Errorf("body contains %q: %s", l, rec.Body.String())
			}
		}
	}
	t.Run("a shift mate list shows playa names only", func(t *testing.T) {
		w.r.set.Set(volunteers.SettingVisibilityShiftMates, "signed_in")
		rec := w.r.call(viewer, "GET", "/shifts/"+w.shift.ID.String(), nil)
		check(t, rec, hidden...)
		privacytest.AssertPresent(t, rec.Body.Bytes(), "mates")
		s := decodeBody[vol.ShiftSummary](t, rec)
		if s.Mates == nil || len(s.Mates.Names) != 2 || s.Mates.Unlisted != 2 {
			t.Errorf("mates = %+v, want two playa names and two unlisted", s.Mates)
		}
	})
	t.Run("the shift list never carries mates", func(t *testing.T) {
		rec := w.r.call(viewer, "GET", "/shifts?department="+w.dept.String(), nil)
		check(t, rec, append([]string{"mates", "names"}, hidden...)...)
	})
	t.Run("sign-up and schedule bodies", func(t *testing.T) {
		rec := w.r.call(viewer, "POST", "/shifts/"+w.shift.ID.String()+"/signups", nil)
		wantStatus(t, rec, http.StatusCreated)
		privacytest.AssertHidden(t, rec.Body.Bytes(), "legal_name", "email", "phone", "pronouns")
		check(t, w.r.call(viewer, "GET", "/me/schedule", nil), "legal_name", "email", "phone", "pronouns", "mates")
	})
	t.Run("policy nobody hides the whole section", func(t *testing.T) {
		w.r.set.Set(volunteers.SettingVisibilityShiftMates, "nobody")
		check(t, w.r.call(viewer, "GET", "/shifts/"+w.shift.ID.String(), nil), append([]string{"mates", "names", "unlisted"}, hidden...)...)
	})
	t.Run("a department list for a volunteer", func(t *testing.T) {
		check(t, w.r.call(viewer, "GET", "/departments/"+w.dept.String()+"/positions", nil), hidden...)
		wantProblem(t, w.r.call(viewer, "GET", "/departments/"+w.dept.String()+"/members", nil), http.StatusForbidden, "permission")
	})
}

func TestHTTP_Roster_LegalNameOnlyForTheAudience(t *testing.T) {
	w := newRosterWorld(t)
	path := "/shifts/" + w.shift.ID.String() + "/roster"

	plain := w.r.call(w.plainLead, "GET", path, nil)
	wantStatus(t, plain, http.StatusOK)
	privacytest.AssertHidden(t, plain.Body.Bytes(), "legal_name")
	for _, l := range []string{legalAlden, legalBriar, legalShy} {
		if strings.Contains(plain.Body.String(), l) {
			t.Errorf("a plain lead's roster body contains a legal name %q", l)
		}
	}
	if n := len(w.r.audited("volunteers.roster.read")); n != 0 {
		t.Errorf("%d roster.read entries for a body without legal names", n)
	}

	holder := w.r.call(w.siteHolder, "GET", path, nil)
	wantStatus(t, holder, http.StatusOK)
	ro := decodeBody[vol.Roster](t, holder)
	if len(ro.Entries) != 4 {
		t.Fatalf("%d entries", len(ro.Entries))
	}
	named := 0
	for _, e := range ro.Entries {
		if e.LegalName != nil {
			named++
		}
	}
	if named != 3 || !strings.Contains(holder.Body.String(), `"legal_name"`) {
		t.Errorf("a contacts.read holder's roster shows %d legal names, want the three people knows", named)
	}
	if n := len(w.r.audited("volunteers.roster.read")); n == 0 {
		t.Error("legal names were returned without a roster.read audit entry")
	}

	wantProblem(t, w.r.call(w.a, "GET", path, nil), http.StatusForbidden, "permission")
	wantProblem(t, w.r.call(model.PersonID{}, "GET", path, nil), http.StatusUnauthorized, "unauthenticated")
}

// ---- listing preference and membership round trips ----

func TestHTTP_Listing_RoundTrip(t *testing.T) {
	r := newReal(t)
	p := r.volunteer("Dandelion")
	rec := r.call(p, "GET", "/me/listing", nil)
	wantStatus(t, rec, http.StatusOK)
	if !decodeBody[vol.Listing](t, rec).Listed {
		t.Errorf("default listing = %s, want listed", rec.Body.String())
	}
	wantStatus(t, r.call(p, "PUT", "/me/listing", vol.ListingInput{Listed: false}), http.StatusOK)
	rec = r.call(p, "GET", "/me/listing", nil)
	if decodeBody[vol.Listing](t, rec).Listed {
		t.Errorf("listing after PUT false = %s", rec.Body.String())
	}
	wantProblem(t, r.call(model.PersonID{}, "PUT", "/me/listing", vol.ListingInput{Listed: true}), http.StatusUnauthorized, "unauthenticated")
}

func TestHTTP_Members_AddRemoveLeave(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	lead := r.lead(d)
	p := r.volunteer("Dandelion")
	base := "/departments/" + d.String() + "/members"
	wantProblem(t, r.call(p, "POST", base, vol.MemberInput{Person: p}), http.StatusForbidden, "permission")
	wantStatus(t, r.call(lead, "POST", base, vol.MemberInput{Person: p}), http.StatusCreated)
	wantField(t, wantProblem(t, r.call(lead, "POST", base, vol.MemberInput{Person: p}), http.StatusConflict, "conflict"), "person", "duplicate")
	wantField(t, wantProblem(t, r.call(lead, "POST", base, vol.MemberInput{}), http.StatusBadRequest, "invalid"), "person", "required")
	wantStatus(t, r.call(p, "DELETE", "/me/departments/"+d.String(), nil), http.StatusNoContent)
	wantProblem(t, r.call(p, "DELETE", "/me/departments/"+d.String(), nil), http.StatusNotFound, "not_found")
	wantProblem(t, r.call(lead, "DELETE", base+"/"+p.String(), nil), http.StatusNotFound, "not_found")
}
