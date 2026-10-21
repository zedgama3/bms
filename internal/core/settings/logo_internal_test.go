package settings

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/audit/audittest"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/authz/authztest"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/files/filestest"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/model"
)

// logoRig is a service over the in-memory store and a files fake, with a person who holds
// settings.manage. Every value is invented.
type logoRig struct {
	*Service
	store *memStore
	fs    *filestest.Fake
	rec   *audittest.Fake
	ctx   context.Context
}

func newLogoRig(t *testing.T, withFiles bool) *logoRig {
	t.Helper()
	c := telemetrytest.New(t)
	person := model.NewID[model.PersonID]()
	az := authztest.NewFake()
	actor := func(context.Context) (authz.Actor, bool) { return authz.Actor{Person: person}, true }
	az.SetActor(actor)
	az.Grant(person, PermissionManage)
	st := newMemStore(7)
	rec := audittest.NewFake("settings")
	fs := filestest.NewFake("settings")
	o := Options{
		DB: dbtest.NewFake(), Clock: clock.NewFake(time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)),
		Logger: c.Logger(), Tracer: c.Tracer("settings"), Audit: rec, Authz: az, Actor: actor,
	}
	if withFiles {
		o.Files = fs
	}
	svc, err := newService(o, st)
	if err != nil {
		t.Fatalf("newService: %v", err)
	}
	return &logoRig{Service: svc, store: st, fs: fs, rec: rec, ctx: t.Context()}
}

// tinyPNG is a small valid PNG; shade makes two logos differ in bytes.
func tinyPNG(t *testing.T, shade uint8) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for x := range 4 {
		img.Set(x, x, color.RGBA{R: shade, A: 255})
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return b.Bytes()
}

func actionsOfEntries(es []audit.Entry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = string(e.Action)
	}
	return out
}

func TestSetLogo_Happy_AttachesAuditsAndMovesTheSnapshot(t *testing.T) {
	r := newLogoRig(t, true)
	before, err := r.Public(r.ctx)
	if err != nil || before.Logo != "" {
		t.Fatalf("Public before = %q, %v; want no logo", before.Logo, err)
	}
	res, err := r.SetLogo(r.ctx, "Our Logo.PNG", bytes.NewReader(tinyPNG(t, 10)))
	if err != nil {
		t.Fatalf("SetLogo: %v", err)
	}
	fl := r.fs.Files()
	if len(fl) != 1 || fl[0].Record != LogoRef || fl[0].ContentType != "image/png" {
		t.Fatalf("files = %+v, want one png on LogoRef", fl)
	}
	if res.Logo != fl[0].ID.String() || res.Revision != 8 {
		t.Errorf("result = %+v, want logo %s revision 8", res, fl[0].ID)
	}
	if len(r.fs.Staged()) != 0 {
		t.Errorf("staged objects left: %d", len(r.fs.Staged()))
	}
	after, rev, err := r.publicAt(r.ctx)
	if err != nil || after.Logo != res.Logo {
		t.Errorf("Public.Logo = %q, %v; want %q", after.Logo, err, res.Logo)
	}
	if rev != 8 {
		t.Errorf("the ETag revision = %d, want 8", rev)
	}
	es := r.rec.Recorded()
	if got := actionsOfEntries(es); len(got) != 1 || got[0] != "settings.logo.changed" {
		t.Fatalf("audit actions = %v", got)
	}
	if es[0].Record != LogoRef || es[0].Reason != "file:"+res.Logo || len(es[0].Fields) != 1 || es[0].Fields[0] != "logo" {
		t.Errorf("entry = %+v", es[0])
	}
}

func TestSetLogo_Replace_LeavesOneFile(t *testing.T) {
	r := newLogoRig(t, true)
	first, err := r.SetLogo(r.ctx, "a.png", bytes.NewReader(tinyPNG(t, 1)))
	if err != nil {
		t.Fatalf("first SetLogo: %v", err)
	}
	second, err := r.SetLogo(r.ctx, "b.png", bytes.NewReader(tinyPNG(t, 200)))
	if err != nil {
		t.Fatalf("second SetLogo: %v", err)
	}
	if first.Logo == second.Logo || second.Revision != first.Revision+1 {
		t.Errorf("first %+v, second %+v; want a new ID and the next revision", first, second)
	}
	if fl := r.fs.Files(); len(fl) != 1 || fl[0].ID.String() != second.Logo {
		t.Errorf("files = %d, want only the second", len(fl))
	}
	if p, err := r.Public(r.ctx); err != nil || p.Logo != second.Logo {
		t.Errorf("Public.Logo = %q, %v", p.Logo, err)
	}
}

func TestRemoveLogo_Happy_ThenNoop(t *testing.T) {
	r := newLogoRig(t, true)
	set, err := r.SetLogo(r.ctx, "a.png", bytes.NewReader(tinyPNG(t, 5)))
	if err != nil {
		t.Fatalf("SetLogo: %v", err)
	}
	res, err := r.RemoveLogo(r.ctx)
	if err != nil || res.Logo != "" || res.Revision != set.Revision+1 {
		t.Fatalf("RemoveLogo = %+v, %v; want empty logo, revision %d", res, err, set.Revision+1)
	}
	if len(r.fs.Files()) != 0 {
		t.Errorf("files remain after removal")
	}
	if p, err := r.Public(r.ctx); err != nil || p.Logo != "" {
		t.Errorf("Public.Logo = %q, %v; want none", p.Logo, err)
	}
	again, err := r.RemoveLogo(r.ctx)
	if err != nil || again.Logo != "" || again.Revision != res.Revision {
		t.Errorf("second RemoveLogo = %+v, %v; want a no-op at revision %d", again, err, res.Revision)
	}
	want := []string{"settings.logo.changed", "settings.logo.removed"}
	got := actionsOfEntries(r.rec.Recorded())
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("audit actions = %v, want %v", got, want)
	}
}

func TestSet_AfterSetLogo_KeepsTheLogoInTheSnapshot(t *testing.T) {
	r := newLogoRig(t, true)
	set, err := r.SetLogo(r.ctx, "a.png", bytes.NewReader(tinyPNG(t, 5)))
	if err != nil {
		t.Fatalf("SetLogo: %v", err)
	}
	v := "Quillfeather Regional"
	if _, err := r.Set(r.ctx, Changes{Values: map[string]*string{"regional.name": &v}}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	p, err := r.Public(r.ctx)
	if err != nil || p.Logo != set.Logo || p.Name != v {
		t.Errorf("Public = logo %q name %q, %v; want logo %q name %q", p.Logo, p.Name, err, set.Logo, v)
	}
}

func TestOpenLogo_Happy_ReturnsTheBytes(t *testing.T) {
	r := newLogoRig(t, true)
	data := tinyPNG(t, 33)
	set, err := r.SetLogo(r.ctx, "a.png", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("SetLogo: %v", err)
	}
	f, c, err := r.OpenLogo(r.ctx)
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

func TestPublic_WithoutFiles_HasNoLogo(t *testing.T) {
	r := newLogoRig(t, false)
	p, err := r.Public(r.ctx)
	if err != nil || p.Logo != "" {
		t.Errorf("Public.Logo = %q, %v; want none", p.Logo, err)
	}
}

func TestLogoRoutes_PutGetDelete_RoundTrip(t *testing.T) {
	r := newLogoRig(t, true)
	mux := http.NewServeMux()
	for _, rt := range r.Routes() {
		mux.Handle(rt.Method+" /api/settings"+rt.Pattern, rt.Handler)
	}
	data := tinyPNG(t, 77)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField("note", "ignored"); err != nil {
		t.Fatalf("WriteField: %v", err)
	}
	fw, err := mw.CreateFormFile("file", "logo.png")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatalf("part write: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("multipart close: %v", err)
	}
	do := func(method, path string, rd io.Reader, ctype string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(r.ctx, method, path, rd)
		if ctype != "" {
			req.Header.Set("Content-Type", ctype)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w
	}

	put := do(http.MethodPut, "/api/settings/logo", &body, mw.FormDataContentType())
	if put.Code != http.StatusOK {
		t.Fatalf("PUT status = %d: %s", put.Code, put.Body.String())
	}
	get := do(http.MethodGet, "/api/settings/logo?v=x", nil, "")
	if get.Code != http.StatusOK || get.Header().Get("Content-Type") != "image/png" || get.Body.Len() == 0 {
		t.Fatalf("GET = %d %q %d bytes", get.Code, get.Header().Get("Content-Type"), get.Body.Len())
	}
	if cc := get.Header().Get("Cache-Control"); cc != "" {
		t.Errorf("Cache-Control = %q, want none", cc)
	}
	del := do(http.MethodDelete, "/api/settings/logo", nil, "")
	if del.Code != http.StatusOK {
		t.Fatalf("DELETE status = %d: %s", del.Code, del.Body.String())
	}
	if len(r.fs.Files()) != 0 {
		t.Errorf("files remain after DELETE")
	}
}

func TestLogoRoute_BodyCapAndTimeout(t *testing.T) {
	r := newLogoRig(t, true)
	for _, rt := range r.Routes() {
		if rt.Pattern != "/logo" {
			continue
		}
		switch rt.Method {
		case http.MethodPut:
			if rt.MaxBodyBytes != 2*MaxLogoBytes || rt.Timeout <= 0 || rt.Public {
				t.Errorf("PUT /logo = %+v", rt)
			}
		case http.MethodGet:
			if !rt.Public || rt.Timeout <= 0 {
				t.Errorf("GET /logo = %+v", rt)
			}
		}
	}
}
