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

func TestHandlers_Contacts_Consents_Self(t *testing.T) {
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
