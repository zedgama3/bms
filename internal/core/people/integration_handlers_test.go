//go:build integration

package people_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

func ok2xx(t *testing.T, w *httptest.ResponseRecorder, f *failRec) {
	t.Helper()
	if f.count() != 0 || w.Code < 200 || w.Code > 299 {
		t.Fatalf("status %d, failure %v", w.Code, f.last())
	}
}

func TestHandlers_MeRoundTrip(t *testing.T) {
	r := newIRig(t, false)
	r.link()
	me := newPerson()
	ctx := asPerson(ctxT(t), me)

	r.spy.reset()
	w, f := r.do(ctx, "GET", "/me", "")
	ok2xx(t, w, f)
	if c := r.spy.counts(); c.WriteTx != 0 {
		t.Errorf("GET /me ran %d write transactions", c.WriteTx)
	}
	if r.rowExists(me) {
		t.Error("GET /me created the row")
	}

	w, f = r.do(ctx, "PUT", "/me", `{"playa_name":"Dusty Ember","pronouns":"they/them"}`)
	ok2xx(t, w, f)
	if !r.rowExists(me) {
		t.Fatal("PUT /me did not create the row")
	}
	w, f = r.do(ctx, "GET", "/me", "")
	ok2xx(t, w, f)
	if !strings.Contains(w.Body.String(), "Dusty Ember") {
		t.Errorf("GET /me = %s, want the saved name", w.Body.String())
	}
	// Own ID behaves as /me, with no permission.
	w, f = r.do(ctx, "GET", "/"+me.String(), "")
	ok2xx(t, w, f)
	if !strings.Contains(w.Body.String(), "Dusty Ember") {
		t.Errorf("GET /{own id} = %s", w.Body.String())
	}
	// A bad field value is a 400 with a field error.
	w, f = r.do(ctx, "PUT", "/me", `{"phone":"call me"}`)
	if f.count() != 1 {
		t.Fatalf("status %d, no failure", w.Code)
	}
	wantField(t, f.last(), "phone", "malformed")
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestHandlers_Contacts_Consents_Requests_Self(t *testing.T) {
	r := newIRig(t, false)
	r.link()
	me := r.insertPerson(seed{Playa: "Dusty Ember"})
	ctx := asPerson(ctxT(t), me)

	w, f := r.do(ctx, "POST", "/me/contacts", `{"name":"Orville Tansy","phone":"+1 555 0188","relationship":"friend"}`)
	ok2xx(t, w, f)
	if r.contactRows(me) != 1 {
		t.Fatal("contact not added")
	}
	prof, err := r.svc.Me(ctx)
	wantNoErr(t, err)
	cid := prof.Contacts[0].ID
	w, f = r.do(ctx, "PUT", "/me/contacts/"+cid.String(), `{"name":"Orville T.","phone":"+1 555 0188"}`)
	ok2xx(t, w, f)
	w, f = r.do(ctx, "DELETE", "/me/contacts/"+cid.String(), "")
	ok2xx(t, w, f)
	if r.contactRows(me) != 0 {
		t.Error("contact not removed")
	}

	w, f = r.do(ctx, "PUT", "/me/consents/volunteers.share-contact", `{"granted":true}`)
	ok2xx(t, w, f)
	ok, err := r.svc.ConsentOf(ctxT(t), me, "volunteers.share-contact")
	wantNoErr(t, err)
	if !ok {
		t.Error("consent not recorded")
	}

	w, f = r.do(ctx, "POST", "/me/requests", `{"kind":"delete","note":"please"}`)
	ok2xx(t, w, f)
	w, f = r.do(ctx, "GET", "/me/requests", "")
	ok2xx(t, w, f)
	if !strings.Contains(w.Body.String(), "please") {
		t.Errorf("GET /me/requests = %s", w.Body.String())
	}
	reqs, err := r.svc.MyRequests(ctx)
	wantNoErr(t, err)
	w, f = r.do(ctx, "POST", "/me/requests/"+reqs[0].ID.String()+"/withdraw", "")
	ok2xx(t, w, f)
	// A second open request conflicts through HTTP as 409.
	w, f = r.do(ctx, "POST", "/me/requests", `{"kind":"delete","note":""}`)
	ok2xx(t, w, f)
	w, f = r.do(ctx, "POST", "/me/requests", `{"kind":"delete","note":""}`)
	if f.count() != 1 {
		t.Fatalf("status %d without a failure", w.Code)
	}
	wantKind(t, f.last(), errs.Conflict)
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", w.Code)
	}
}

func TestHandlers_Names_ReturnsKnownNames(t *testing.T) {
	r := newIRig(t, false)
	a := r.insertPerson(seed{Playa: "Dusty Ember"})
	b := r.insertPerson(seed{Playa: "Pell Mallow"})
	w, f := r.do(asPerson(ctxT(t), newPerson()), "GET", fmt.Sprintf("/names?ids=%s,%s,%s", a, b, newPerson()), "")
	ok2xx(t, w, f)
	body := w.Body.String()
	if !json.Valid([]byte(body)) {
		t.Fatalf("body is not JSON: %s", body)
	}
	for _, want := range []string{a.String(), "Dusty Ember", b.String(), "Pell Mallow"} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q: %s", want, body)
		}
	}
}

func TestHandlers_Search_Results(t *testing.T) {
	r := newIRig(t, false)
	r.insertPerson(seed{Playa: "Dusty Ember"})
	_, ctx := r.admin(permDirectory)
	w, f := r.do(ctx, "GET", "/search?q=ember&limit=5", "")
	ok2xx(t, w, f)
	if !strings.Contains(w.Body.String(), "Dusty Ember") {
		t.Errorf("body = %s", w.Body.String())
	}
}

func TestHandlers_Export_Attachment_AndCooldown(t *testing.T) {
	r := newExportFixture(t)
	ctx := asPerson(ctxT(t), r.me)
	w, f := r.do(ctx, "GET", "/me/export", "")
	ok2xx(t, w, f)
	want := fmt.Sprintf(`attachment; filename="bms-export-%s.json"`, r.me)
	if cd := w.Header().Get("Content-Disposition"); cd != want {
		t.Errorf("Content-Disposition = %q, want %q", cd, want)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil || string(doc["format"]) != "1" {
		t.Errorf("body is not the export document: %v", err)
	}
	w, f = r.do(ctx, "GET", "/me/export", "")
	if f.count() != 1 {
		t.Fatalf("status %d: the second download inside the cooldown was served", w.Code)
	}
	wantRateLimited(t, f.last())
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", w.Code)
	}
	// An admin's route is not limited and has the same attachment name.
	_, actx := r.admin(permExport)
	for range 2 {
		w, f = r.do(actx, "GET", "/"+r.me.String()+"/export", "")
		ok2xx(t, w, f)
		if cd := w.Header().Get("Content-Disposition"); cd != want {
			t.Errorf("admin Content-Disposition = %q", cd)
		}
	}
}

func TestHandlers_ManageRoutes(t *testing.T) {
	r := newIRig(t, true)
	r.link()
	_, mctx := r.admin(permManage, permDirectory, permContacts, permDelete, permExport)

	w, f := r.do(mctx, "POST", "/", `{"playa_name":"Walk On","legal_name":"Marguerite Quill"}`)
	ok2xx(t, w, f)
	var created struct {
		Person struct {
			ID string `json:"id"`
		} `json:"person"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil || created.Person.ID == "" {
		// The profile's JSON shape is not fixed by the spec; fall back to a lookup.
		got, serr := r.svc.Search(mctx, people.SearchQuery{Q: "walk on"})
		wantNoErr(t, serr)
		if len(got) != 1 {
			t.Fatalf("created person not found: %s", w.Body.String())
		}
		created.Person.ID = got[0].ID.String()
	}
	pid, err := model.ParseID[model.PersonID](created.Person.ID)
	wantNoErr(t, err)

	w, f = r.do(mctx, "PUT", "/"+pid.String(), `{"pronouns":"she/her"}`)
	ok2xx(t, w, f)
	w, f = r.do(mctx, "GET", "/"+pid.String(), "")
	ok2xx(t, w, f)
	w, f = r.do(mctx, "POST", "/"+pid.String()+"/contacts", `{"name":"Orville Tansy","phone":"+1 555 0188"}`)
	ok2xx(t, w, f)
	w, f = r.do(mctx, "PUT", "/"+pid.String()+"/consents/volunteers.share-contact", `{"granted":true}`)
	ok2xx(t, w, f)

	u := r.signUp("route.link", true)
	w, f = r.do(mctx, "POST", "/"+pid.String()+"/link-account", `{"email":"`+string(u.email)+`"}`)
	ok2xx(t, w, f)
	if r.accountRows(pid) != 1 {
		t.Error("link-account did not link")
	}
	w, f = r.do(mctx, "POST", "/"+pid.String()+"/link-account", `{"email":"nobody.here@example.org"}`)
	if f.count() != 1 {
		t.Fatalf("status %d without a failure", w.Code)
	}
	wantKind(t, f.last(), errs.Conflict) // pid has an account now
}

func TestHandlers_RequestsScreenRoutes_AndDeleteReport(t *testing.T) {
	f := newDelFixture(t, false)
	other := f.insertPerson(seed{Playa: "Pell Mallow"})
	octx := asPerson(ctxT(t), other)
	req, err := f.svc.RequestDeletion(octx, "")
	wantNoErr(t, err)

	w, fl := f.do(f.ctx, "GET", "/requests", "")
	ok2xx(t, w, fl)
	if !strings.Contains(w.Body.String(), req.ID.String()) {
		t.Errorf("GET /requests = %s", w.Body.String())
	}
	w, fl = f.do(f.ctx, "GET", "/requests?status=declined&person="+other.String(), "")
	ok2xx(t, w, fl)

	w, fl = f.do(f.ctx, "POST", "/requests/"+req.ID.String()+"/decline", `{"note":"not now"}`)
	ok2xx(t, w, fl)

	// Fulfil the fixture's own request: the response is the deletion report.
	reqs, err := f.svc.ListRequests(f.ctx, people.RequestQuery{})
	wantNoErr(t, err)
	if len(reqs) != 1 {
		t.Fatalf("%d open requests, want the fixture's", len(reqs))
	}
	w, fl = f.do(f.ctx, "POST", "/requests/"+reqs[0].ID.String()+"/fulfil", "")
	ok2xx(t, w, fl)
	assertReportJSON(t, w.Body.Bytes(), f.target)

	// DELETE /{person} returns the report as well.
	f2 := newDelFixture(t, false)
	w, fl = f2.do(f2.ctx, "DELETE", "/"+f2.target.String(), "")
	ok2xx(t, w, fl)
	assertReportJSON(t, w.Body.Bytes(), f2.target)
}

func assertReportJSON(t *testing.T, body []byte, person model.PersonID) {
	t.Helper()
	var rep struct {
		Person   string                     `json:"person"`
		People   *int                       `json:"people"`
		Identity *int                       `json:"identity"`
		Access   *int                       `json:"access"`
		Modules  map[string]json.RawMessage `json:"modules"`
		Kept     []struct {
			Kind  string `json:"kind"`
			Count int    `json:"count"`
		} `json:"kept"`
	}
	if err := json.Unmarshal(body, &rep); err != nil {
		t.Fatalf("report is not JSON: %v\n%s", err, body)
	}
	if rep.Person != person.String() || rep.People == nil || rep.Identity == nil || rep.Access == nil {
		t.Errorf("report = %s", body)
	}
	if _, ok := rep.Modules["alpha"]; !ok {
		t.Errorf("report lacks module alpha: %s", body)
	}
	found := false
	for _, k := range rep.Kept {
		if k.Kind == "audit.entry" && k.Count == 7 {
			found = true
		}
	}
	if !found {
		t.Errorf("report lacks kept audit.entry 7: %s", body)
	}
}
