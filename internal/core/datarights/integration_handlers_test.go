//go:build integration

package datarights_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/datarights"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

func ok2xx(t *testing.T, w *httptest.ResponseRecorder, f *failRec) {
	t.Helper()
	if f.count() != 0 || w.Code < 200 || w.Code > 299 {
		t.Fatalf("status %d, failure %v", w.Code, f.last())
	}
}

// The self-service request routes round trip (moved from people's TestHandlers_Contacts_Consents_Requests_Self).
func TestHandlers_Requests_Self(t *testing.T) {
	r := newIRig(t, false)
	r.link()
	me := r.insertPerson(seed{Playa: "Dusty Ember"})
	ctx := asPerson(ctxT(t), me)

	w, f := r.do(ctx, "POST", "/me/requests", `{"kind":"delete","note":"please"}`)
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
		w, f = r.do(actx, "GET", "/people/"+r.me.String()+"/export", "")
		ok2xx(t, w, f)
		if cd := w.Header().Get("Content-Disposition"); cd != want {
			t.Errorf("admin Content-Disposition = %q", cd)
		}
	}
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
	reqs, err := f.svc.ListRequests(f.ctx, datarights.RequestQuery{})
	wantNoErr(t, err)
	if len(reqs) != 1 {
		t.Fatalf("%d open requests, want the fixture's", len(reqs))
	}
	w, fl = f.do(f.ctx, "POST", "/requests/"+reqs[0].ID.String()+"/fulfil", "")
	ok2xx(t, w, fl)
	assertReportJSON(t, w.Body.Bytes(), f.target)

	// DELETE /{person} returns the report as well.
	f2 := newDelFixture(t, false)
	w, fl = f2.do(f2.ctx, "DELETE", "/people/"+f2.target.String(), "")
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
