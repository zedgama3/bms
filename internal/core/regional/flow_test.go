package regional_test

// The logo's happy paths and the public document over the unit rig (moved from settings'
// white-box logo_internal_test.go, which drove them through an in-memory store; here the
// rig is regional's own: filestest.Fake behind a spy, dbtest.Fake, a revision box and a
// settingstest reader). The paths that need a real database are in logo_integration_test.go.

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/zedgama3/bms/internal/core/regional"
	"github.com/zedgama3/bms/internal/lib/httpx"
)

func TestSetLogo_Happy_AttachesAuditsAndMovesTheDocument(t *testing.T) {
	r := newRig(t)
	ctx := r.actx()
	before, err := r.svc.Public(ctx)
	if err != nil || before.Logo != "" {
		t.Fatalf("Public before = %q, %v; want no logo", before.Logo, err)
	}
	res, err := r.svc.SetLogo(ctx, "Our Logo.PNG", bytes.NewReader(pngOf(t, 10)))
	if err != nil {
		t.Fatalf("SetLogo: %v", err)
	}
	fl := r.fs.Files()
	if len(fl) != 1 || fl[0].Record != regional.LogoRef || fl[0].ContentType != "image/png" {
		t.Fatalf("files = %+v, want one png on LogoRef", fl)
	}
	if res.Logo != fl[0].ID.String() || res.Revision != 8 {
		t.Errorf("result = %+v, want logo %s revision 8 (the bumped one)", res, fl[0].ID)
	}
	if len(r.fs.Staged()) != 0 {
		t.Errorf("staged objects left: %d", len(r.fs.Staged()))
	}
	after, err := r.svc.Public(ctx)
	if err != nil || after.Logo != res.Logo {
		t.Errorf("Public.Logo = %q, %v; want %q", after.Logo, err, res.Logo)
	}
}

// Audit: the entry is recorded under regional's own area (boundaries decision 13), and the
// old settings.* name is gone.
func TestSetLogo_RecordsRegionalLogoChanged(t *testing.T) {
	r := newRig(t)
	res, err := r.svc.SetLogo(r.actx(), "a.png", bytes.NewReader(pngOf(t, 10)))
	wantNoErr(t, err)
	es := r.rec.Recorded()
	if got := actionsOf(es); len(got) != 1 || got[0] != "regional.logo.changed" {
		t.Fatalf("audit actions = %v, want [regional.logo.changed]", got)
	}
	if es[0].Record != regional.LogoRef || es[0].Reason != "file:"+res.Logo || len(es[0].Fields) != 1 || es[0].Fields[0] != "logo" {
		t.Errorf("entry = %+v", es[0])
	}
}

func TestRemoveLogo_RecordsRegionalLogoRemoved(t *testing.T) {
	r := newRig(t)
	set, err := r.svc.SetLogo(r.actx(), "a.png", bytes.NewReader(pngOf(t, 10)))
	wantNoErr(t, err)
	_, err = r.svc.RemoveLogo(r.actx())
	wantNoErr(t, err)
	es := r.rec.Recorded()
	got := actionsOf(es)
	if len(got) != 2 || got[0] != "regional.logo.changed" || got[1] != "regional.logo.removed" {
		t.Fatalf("audit actions = %v, want [regional.logo.changed regional.logo.removed]", got)
	}
	if es[1].Record != regional.LogoRef || es[1].Reason != "file:"+set.Logo || len(es[1].Fields) != 1 || es[1].Fields[0] != "logo" {
		t.Errorf("removed entry = %+v", es[1])
	}
	for _, a := range got {
		if strings.HasPrefix(a, "settings.") {
			t.Errorf("audit action %q is still under settings", a)
		}
	}
}

func TestSetLogo_Replace_LeavesOneFile(t *testing.T) {
	r := newRig(t)
	ctx := r.actx()
	first, err := r.svc.SetLogo(ctx, "a.png", bytes.NewReader(pngOf(t, 1)))
	wantNoErr(t, err)
	second, err := r.svc.SetLogo(ctx, "b.png", bytes.NewReader(pngOf(t, 200)))
	wantNoErr(t, err)
	if first.Logo == second.Logo {
		t.Errorf("first %+v, second %+v; want a new ID", first, second)
	}
	if fl := r.fs.Files(); len(fl) != 1 || fl[0].ID.String() != second.Logo {
		t.Errorf("files = %d, want only the second", len(fl))
	}
	if p, err := r.svc.Public(ctx); err != nil || p.Logo != second.Logo {
		t.Errorf("Public.Logo = %q, %v", p.Logo, err)
	}
}

func TestRemoveLogo_Happy_ThenNoop(t *testing.T) {
	r := newRig(t)
	ctx := r.actx()
	_, err := r.svc.SetLogo(ctx, "a.png", bytes.NewReader(pngOf(t, 5)))
	wantNoErr(t, err)
	res, err := r.svc.RemoveLogo(ctx)
	if err != nil || res.Logo != "" || res.Revision != 9 {
		t.Fatalf("RemoveLogo = %+v, %v; want an empty logo at revision 9", res, err)
	}
	if len(r.fs.Files()) != 0 {
		t.Errorf("files remain after removal")
	}
	if p, err := r.svc.Public(ctx); err != nil || p.Logo != "" {
		t.Errorf("Public.Logo = %q, %v; want none", p.Logo, err)
	}
	entries := len(r.rec.Recorded())
	again, err := r.svc.RemoveLogo(ctx)
	if err != nil || again.Logo != "" || again.Revision != res.Revision {
		t.Errorf("second RemoveLogo = %+v, %v; want a no-op at revision %d", again, err, res.Revision)
	}
	if len(r.rec.Recorded()) != entries {
		t.Errorf("a no-op RemoveLogo recorded %v", actionsOf(r.rec.Recorded()[entries:]))
	}
}

// A change to a regional.* value leaves the logo in the document.
func TestPublic_AfterASettingChange_KeepsTheLogo(t *testing.T) {
	r := newRig(t)
	ctx := r.actx()
	set, err := r.svc.SetLogo(ctx, "a.png", bytes.NewReader(pngOf(t, 5)))
	wantNoErr(t, err)
	r.rd.Set("name", "Quillfeather Regional")
	r.rev.set(8, nil)
	p, err := r.svc.Public(ctx)
	if err != nil || p.Logo != set.Logo || p.Name != "Quillfeather Regional" {
		t.Errorf("Public = logo %q name %q, %v; want logo %q name %q", p.Logo, p.Name, err, set.Logo, "Quillfeather Regional")
	}
}

func TestOpenLogo_Happy_ReturnsTheBytes(t *testing.T) {
	r := newRig(t)
	set, err := r.svc.SetLogo(r.actx(), "a.png", bytes.NewReader(pngOf(t, 33)))
	wantNoErr(t, err)
	// No session: the logo is public.
	f, c, err := r.svc.OpenLogo(t.Context())
	if err != nil {
		t.Fatalf("OpenLogo: %v", err)
	}
	got, rerr := io.ReadAll(c)
	if cerr := c.Close(); rerr != nil || cerr != nil {
		t.Fatalf("read %v, close %v", rerr, cerr)
	}
	if f.ID.String() != set.Logo || len(got) == 0 {
		t.Errorf("OpenLogo = file %s with %d bytes, want file %s", f.ID, len(got), set.Logo)
	}
}

func TestLogoRoutes_PutGetDelete_RoundTrip(t *testing.T) {
	r := newRig(t)
	data := pngOf(t, 77)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	wantNoErr(t, mw.WriteField("note", "ignored"))
	fw, err := mw.CreateFormFile("file", "logo.png")
	wantNoErr(t, err)
	_, err = fw.Write(data)
	wantNoErr(t, err)
	wantNoErr(t, mw.Close())
	do := func(method, path string, rd io.Reader, ctype string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(httpx.WithFailer(r.actx(), &failRec{}), method, path, rd)
		if ctype != "" {
			req.Header.Set("Content-Type", ctype)
		}
		w := httptest.NewRecorder()
		r.mux().ServeHTTP(w, req)
		return w
	}

	put := do(http.MethodPut, "/api/regional/logo", &body, mw.FormDataContentType())
	if put.Code != http.StatusOK {
		t.Fatalf("PUT status = %d: %s", put.Code, put.Body.String())
	}
	get := do(http.MethodGet, "/api/regional/logo?v=x", nil, "")
	if get.Code != http.StatusOK || get.Header().Get("Content-Type") != "image/png" || get.Body.Len() == 0 {
		t.Fatalf("GET = %d %q %d bytes", get.Code, get.Header().Get("Content-Type"), get.Body.Len())
	}
	if cc := get.Header().Get("Cache-Control"); cc != "" {
		t.Errorf("Cache-Control = %q, want none", cc)
	}
	del := do(http.MethodDelete, "/api/regional/logo", nil, "")
	if del.Code != http.StatusOK {
		t.Fatalf("DELETE status = %d: %s", del.Code, del.Body.String())
	}
	if len(r.fs.Files()) != 0 {
		t.Errorf("files remain after DELETE")
	}
	// The old paths are gone: nothing serves /api/settings/logo from here.
	if w := do(http.MethodGet, "/api/settings/logo", nil, ""); w.Code != http.StatusNotFound && w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /api/settings/logo = %d on regional's mux, want it unrouted", w.Code)
	}
}

func TestLogoRoute_BodyCapAndTimeout(t *testing.T) {
	r := newRig(t)
	for _, rt := range r.svc.Routes() {
		if rt.Pattern != "/logo" {
			continue
		}
		switch rt.Method {
		case http.MethodPut:
			if rt.MaxBodyBytes != 2*regional.MaxLogoBytes || rt.Timeout <= 0 || rt.Public {
				t.Errorf("PUT /logo = %+v", rt)
			}
		case http.MethodGet:
			if !rt.Public || rt.Timeout <= 0 {
				t.Errorf("GET /logo = %+v", rt)
			}
		}
	}
}

// The ETag of GET /public follows the settings revision and the logo.
func TestPublic_ETag_FollowsTheRevisionAndTheLogo(t *testing.T) {
	r := newRig(t)
	get := func(inm string) (int, string) {
		var hdr []string
		if inm != "" {
			hdr = []string{"If-None-Match", inm}
		}
		w, _ := r.do(t.Context(), http.MethodGet, "/public", "", hdr...)
		return w.Code, w.Header().Get("ETag")
	}
	code, etag0 := get("")
	if code != http.StatusOK || etag0 != `"regional-7"` {
		t.Fatalf("GET /public = %d, ETag %q, want \"regional-7\" (the revision alone)", code, etag0)
	}
	if code, _ := get(etag0); code != http.StatusNotModified {
		t.Errorf("matching If-None-Match = %d, want 304", code)
	}
	r.rev.set(8, nil) // a settings write
	if code, etag := get(etag0); code != http.StatusOK || etag == etag0 {
		t.Errorf("after a revision change: %d, ETag %q (was %q)", code, etag, etag0)
	}
	_, etag1 := get("")
	_, err := r.svc.SetLogo(r.actx(), "a.png", bytes.NewReader(pngOf(t, 1)))
	wantNoErr(t, err)
	if code, etag := get(etag1); code != http.StatusOK || etag != `"regional-9"` {
		t.Errorf("after SetLogo: %d, ETag %q (was %q), want \"regional-9\"", code, etag, etag1)
	}
}

// The logo's file ID is read from the files store once per revision, not on every request
// (A2, point 1): Public is a cheap boot document.
func TestPublic_LogoLookup_OncePerRevision(t *testing.T) {
	r := newRig(t)
	ctx := r.actx()
	for range 5 {
		ignore(r.svc.Public(ctx))
	}
	if n := r.spy.byRecords(); n != 1 {
		t.Errorf("%d ByRecord calls for five reads at one revision, want 1", n)
	}
	r.rev.set(8, nil) // a settings write, here or on another instance
	for range 5 {
		ignore(r.svc.Public(ctx))
	}
	if n := r.spy.byRecords(); n != 2 {
		t.Errorf("%d ByRecord calls after one revision change, want 2", n)
	}
}

// The cache never trusts a lower revision: after a restore the revision read is below the
// cached one, so the held ID is dropped and the store is asked again (A2, point 1).
func TestPublic_RevisionMovesBack_LooksUpAgain(t *testing.T) {
	r := newRig(t)
	ctx := r.actx()
	a := r.seedLogo(t, "a.png", pngOf(t, 1))
	p, err := r.svc.Public(ctx) // revision 7
	wantNoErr(t, err)
	if p.Logo != a.ID.String() || r.spy.byRecords() != 1 {
		t.Fatalf("Logo %q after %d lookups; want %s after 1", p.Logo, r.spy.byRecords(), a.ID)
	}
	b := r.seedLogo(t, "b.png", pngOf(t, 2)) // the newest file wins
	r.rev.set(5, nil)                        // a database restore: the revision went back
	p, err = r.svc.Public(ctx)
	wantNoErr(t, err)
	if p.Logo != b.ID.String() {
		t.Errorf("Logo = %q after the revision moved back, want the store's answer %s", p.Logo, b.ID)
	}
	if n := r.spy.byRecords(); n != 2 {
		t.Errorf("%d lookups after the revision moved back, want 2", n)
	}
}

// A read that began at an older revision and finishes late must not overwrite the newer
// cache entry: the next read at the newer revision is served from the cache.
func TestPublic_SlowOlderRead_DoesNotOverwriteNewerCache(t *testing.T) {
	r := newRig(t)
	ctx := r.actx()
	old := r.seedLogo(t, "old.png", pngOf(t, 1))
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	r.spy.setByRecHook(func() {
		first := false
		once.Do(func() { first = true })
		if first { // only the slow reader blocks
			close(entered)
			<-release
		}
	})
	r.rev.set(5, nil)
	type res struct {
		p   regional.Public
		err error
	}
	slow := make(chan res, 1)
	go func() {
		p, err := r.svc.Public(ctx) // reads revision 5, then waits inside ByRecord
		slow <- res{p, err}
	}()
	<-entered
	newer := r.seedLogo(t, "new.png", pngOf(t, 2))
	r.rev.set(8, nil)
	p, err := r.svc.Public(ctx) // a fast reader at revision 8 fills the cache
	wantNoErr(t, err)
	if p.Logo != newer.ID.String() {
		t.Fatalf("fast reader Logo = %q, want %s", p.Logo, newer.ID)
	}
	close(release)
	if got := <-slow; got.err != nil {
		t.Fatalf("slow reader: %v", got.err)
	}
	before := r.spy.byRecords()
	p, err = r.svc.Public(ctx) // revision 8 again
	wantNoErr(t, err)
	if p.Logo != newer.ID.String() {
		t.Errorf("Logo = %q after the slow reader finished, want the newer %s", p.Logo, newer.ID)
	}
	if n := r.spy.byRecords(); n != before {
		t.Errorf("%d lookups for a read at the cached revision, want none: the slow read overwrote the newer cache", n-before)
	}
	_ = old
}
