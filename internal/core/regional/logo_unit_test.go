package regional_test

// Failure-mode, permission and contract tests for the logo (settings.md, amendment
// 2026-10-21, F37 to F54). These need no database: they prove what happens before the store
// is reached, and the read side over filestest.Fake. The paths that commit are in
// logo_integration_test.go.

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/objects"
	"github.com/zedgama3/bms/internal/core/regional"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
)

// ---- constants ---------------------------------------------------------------------------

func TestMaxLogoBytes_Pinned(t *testing.T) {
	if regional.MaxLogoBytes != 2097152 {
		t.Errorf("MaxLogoBytes = %d, want 2097152", regional.MaxLogoBytes)
	}
}

func TestLogoRef_Pinned(t *testing.T) {
	sum := sha256.Sum256([]byte("settings.logo"))
	if regional.LogoRef.Kind != "settings.logo" || files.LogoRecordKind != "settings.logo" {
		t.Errorf("LogoRef.Kind = %q", regional.LogoRef.Kind)
	}
	if regional.LogoRef.ID != [16]byte(sum[:16]) {
		t.Errorf("LogoRef.ID = %x, want %x", regional.LogoRef.ID, sum[:16])
	}
	if err := regional.LogoRef.Validate(); err != nil {
		t.Errorf("LogoRef.Validate: %v", err)
	}
}

// ---- routes ------------------------------------------------------------------------------

func TestLogoRoutes_AccessAndLimits(t *testing.T) {
	u := newLogoUnit(t)
	get, put, del := logoRoute(t, u.svc, http.MethodGet), logoRoute(t, u.svc, http.MethodPut), logoRoute(t, u.svc, http.MethodDelete)
	if !get.Public {
		t.Error("GET /logo must be public")
	}
	if put.Public || del.Public {
		t.Error("PUT and DELETE /logo must not be public")
	}
	if put.MaxBodyBytes != 2*regional.MaxLogoBytes {
		t.Errorf("PUT MaxBodyBytes = %d, want %d", put.MaxBodyBytes, 2*regional.MaxLogoBytes)
	}
	if put.Timeout != files.UploadTimeout {
		t.Errorf("PUT Timeout = %v, want %v", put.Timeout, files.UploadTimeout)
	}
	if get.Timeout != files.ServeTimeout {
		t.Errorf("GET Timeout = %v, want %v", get.Timeout, files.ServeTimeout)
	}
}

func TestOpenAPI_NamesTheLogoRoutes(t *testing.T) {
	u := newLogoUnit(t)
	var doc struct {
		Paths      map[string]map[string]json.RawMessage `json:"paths"`
		Components struct {
			Schemas map[string]struct {
				Required []string `json:"required"`
			} `json:"schemas"`
		} `json:"components"`
	}
	wantNoErr(t, json.Unmarshal(u.svc.OpenAPI(), &doc))
	ops := doc.Paths["/logo"]
	for _, m := range []string{"get", "put", "delete"} {
		if _, ok := ops[m]; !ok {
			t.Errorf("OpenAPI /logo has no %s", m)
		}
	}
	// The fragment prefixes every schema with the route set's name (api decision 33): now
	// "Regional"; a bare name is also accepted.
	if _, ok := doc.Components.Schemas["RegionalLogoResult"]; !ok {
		if _, ok := doc.Components.Schemas["LogoResult"]; !ok {
			t.Error("OpenAPI has no LogoResult schema")
		}
	}
	pub, ok := doc.Components.Schemas["RegionalPublic"]
	if !ok {
		pub = doc.Components.Schemas["Public"]
	}
	found := false
	for _, r := range pub.Required {
		found = found || r == "logo"
	}
	if !found {
		t.Error("the Public schema does not require logo")
	}
}

// ---- F40 is gone: Options.Files is required, so a nil store is TestNew_NilFiles_ReturnsInvalid (B9).

func TestSetLogo_NoActor_ReturnsUnauthenticated(t *testing.T) {
	u := newLogoUnit(t)
	for name, ctx := range map[string]func() error{
		"no session": func() error {
			return errOf(u.svc.SetLogo(t.Context(), "logo.png", bytes.NewReader(pngOf(t, 1))))
		},
		"restricted session": func() error {
			return errOf(u.svc.SetLogo(asRestricted(t.Context(), u.admin), "logo.png", bytes.NewReader(pngOf(t, 1))))
		},
	} {
		t.Run(name, func(t *testing.T) { wantKind(t, ctx(), errs.Unauthenticated) })
	}
	if st, _, _ := u.spy.counts(); st != 0 {
		t.Errorf("Stage called %d times for an unauthenticated caller", st)
	}
	u.noStatements(t, "an unauthenticated SetLogo")
}

func TestRemoveLogo_NoActor_ReturnsUnauthenticated(t *testing.T) {
	u := newLogoUnit(t)
	wantKind(t, errOf(u.svc.RemoveLogo(t.Context())), errs.Unauthenticated)
	wantKind(t, errOf(u.svc.RemoveLogo(asRestricted(t.Context(), u.admin))), errs.Unauthenticated)
	u.noStatements(t, "an unauthenticated RemoveLogo")
}

func TestSetLogo_NoPermission_ReturnsPermission(t *testing.T) {
	u := newLogoUnit(t)
	readOnlyAdmin := u.person(settings.PermissionRead) // read is not manage
	nobody := newPerson()
	for name, p := range map[string]func() error{
		"settings.read only": func() error {
			return errOf(u.svc.SetLogo(asPerson(t.Context(), readOnlyAdmin), "logo.png", bytes.NewReader(pngOf(t, 1))))
		},
		"no grants": func() error {
			return errOf(u.svc.SetLogo(asPerson(t.Context(), nobody), "logo.png", bytes.NewReader(pngOf(t, 1))))
		},
	} {
		t.Run(name, func(t *testing.T) { wantKind(t, p(), errs.Permission) })
	}
	if st, _, _ := u.spy.counts(); st != 0 {
		t.Errorf("Stage called %d times without permission", st)
	}
	if len(u.fs.Staged()) != 0 || len(u.fs.Files()) != 0 {
		t.Error("something was staged or attached without permission")
	}
	u.noStatements(t, "SetLogo without permission")
}

func TestRemoveLogo_NoPermission_ReturnsPermission(t *testing.T) {
	u := newLogoUnit(t)
	u.seedLogo(t, "logo.png", pngOf(t, 1))
	wantKind(t, errOf(u.svc.RemoveLogo(asPerson(t.Context(), u.person(settings.PermissionRead)))), errs.Permission)
	wantKind(t, errOf(u.svc.RemoveLogo(asPerson(t.Context(), newPerson()))), errs.Permission)
	if len(u.fs.Files()) != 1 {
		t.Errorf("the logo was removed without permission: %d files", len(u.fs.Files()))
	}
}

// The permission check comes before the read-only check: a person who may not manage learns
// Permission, not the state of the copy.
func TestLogo_ReadOnlyAndNoPermission_ReturnsPermission(t *testing.T) {
	u := newLogoUnit(t)
	u.fake.SetReadOnly(true)
	nobody := asPerson(t.Context(), newPerson())
	wantKind(t, errOf(u.svc.SetLogo(nobody, "logo.png", bytes.NewReader(pngOf(t, 1)))), errs.Permission)
	wantKind(t, errOf(u.svc.RemoveLogo(nobody)), errs.Permission)
}

// ---- F41: read-only ----------------------------------------------------------------------

func TestSetLogo_ReadOnly_ReturnsUnavailableBeforeBodyRead(t *testing.T) {
	u := newLogoUnit(t)
	u.fake.SetReadOnly(true)
	body := newCountingBody(pngOf(t, 1))
	_, err := u.svc.SetLogo(u.actx(), "logo.png", body)
	wantKind(t, err, errs.Unavailable)
	if !errors.Is(err, db.ErrReadOnly) {
		t.Errorf("error does not wrap db.ErrReadOnly: %v", err)
	}
	if body.reads.Load() != 0 {
		t.Errorf("the body was read %d times on a read-only copy", body.reads.Load())
	}
	if st, _, _ := u.spy.counts(); st != 0 || len(u.fs.Staged()) != 0 {
		t.Errorf("Stage reached on a read-only copy (%d calls, %d staged)", st, len(u.fs.Staged()))
	}
	u.noStatements(t, "SetLogo on a read-only copy")
}

func TestRemoveLogo_ReadOnly_ReturnsUnavailable(t *testing.T) {
	u := newLogoUnit(t)
	u.seedLogo(t, "logo.png", pngOf(t, 1))
	u.fake.SetReadOnly(true)
	_, err := u.svc.RemoveLogo(u.actx())
	wantKind(t, err, errs.Unavailable)
	if !errors.Is(err, db.ErrReadOnly) {
		t.Errorf("error does not wrap db.ErrReadOnly: %v", err)
	}
	if len(u.fs.Files()) != 1 {
		t.Error("the logo was removed on a read-only copy")
	}
}

func TestPutLogo_ReadOnly_Returns503WithoutStaging(t *testing.T) {
	u := newLogoUnit(t)
	u.fake.SetReadOnly(true)
	body, ct := multipartOf(t, nil, "logo.png", "image/png", pngOf(t, 1))
	w, f := u.send(u.actx(), http.MethodPut, "/logo", body, ct)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
	if !errors.Is(f.last(), db.ErrReadOnly) {
		t.Errorf("handler error does not wrap db.ErrReadOnly: %v", f.last())
	}
	if len(u.fs.Staged()) != 0 {
		t.Error("an object was staged on a read-only copy")
	}
}

// ---- F42: the counting reader ------------------------------------------------------------

func TestSetLogo_OverMaxLogoBytes_ReturnsInvalidTooLarge(t *testing.T) {
	head := pngOf(t, 1)
	tests := []struct {
		name string
		size int
	}{
		{"one byte over the cap", int(regional.MaxLogoBytes) + 1},
		{"far over the cap", 8 << 20},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u := newLogoUnit(t)
			body := newCountingBody(padded(head, tc.size))
			_, err := u.svc.SetLogo(u.actx(), "logo.png", body)
			wantField(t, err, "file", "too_large")
			if got := paramOf(err, "file", "too_large", "max"); got != "2097152" {
				t.Errorf("max param = %q, want 2097152", got)
			}
			if len(u.fs.Staged()) != 0 || len(u.fs.Files()) != 0 {
				t.Errorf("staged %d, attached %d after a too-large upload", len(u.fs.Staged()), len(u.fs.Files()))
			}
			// The reader stops at the byte past the cap; it does not drain the rest.
			if served := body.bytes.Load(); served > regional.MaxLogoBytes+1+32<<10 {
				t.Errorf("the body was read to %d bytes; the cap is %d", served, regional.MaxLogoBytes)
			}
			u.noStatements(t, "a too-large logo")
		})
	}
}

// ---- F43, F44 (the unclassifiable), required --------------------------------------------

func TestSetLogo_UnacceptedType_ReturnsInvalidType(t *testing.T) {
	tests := []struct {
		name string
		file string
		data []byte
	}{
		{"svg named png", "logo.png", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="4" height="4"><rect width="4" height="4"/></svg>`)},
		{"svg with a prolog and a script", "logo.svg", []byte("<?xml version=\"1.0\"?>\n<svg xmlns=\"http://www.w3.org/2000/svg\"><script>alert(1)</script></svg>")},
		{"gif", "logo.gif", append([]byte("GIF89a"), make([]byte, 40)...)},
		{"pdf named png", "logo.png", []byte("%PDF-1.4\n1 0 obj<<>>endobj\n%%EOF")},
		{"html named png", "logo.png", []byte("<!doctype html><html><body>hi</body></html>")},
		{"plain text named png", "logo.png", []byte("just some words, not an image")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u := newLogoUnit(t)
			_, err := u.svc.SetLogo(u.actx(), tc.file, bytes.NewReader(tc.data))
			wantField(t, err, "file", "type")
			if len(u.fs.Staged()) != 0 || len(u.fs.Files()) != 0 {
				t.Error("an unaccepted file was staged or attached")
			}
			u.noStatements(t, "an unaccepted type")
		})
	}
}

// A declared image/png on the part never rescues an SVG: the sniff decides (files decision 7).
func TestPutLogo_SVGDeclaredAsPNG_ReturnsInvalidType(t *testing.T) {
	u := newLogoUnit(t)
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	body, ct := multipartOf(t, nil, "logo.png", "image/png", svg)
	w, f := u.send(u.actx(), http.MethodPut, "/logo", body, ct)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	wantField(t, f.last(), "file", "type")
	if len(u.fs.Files()) != 0 || len(u.fs.Staged()) != 0 {
		t.Error("the SVG reached the store")
	}
}

func TestSetLogo_EmptyAndNilBody_ReturnsInvalidRequired(t *testing.T) {
	u := newLogoUnit(t)
	t.Run("zero bytes", func(t *testing.T) {
		wantField(t, errOf(u.svc.SetLogo(u.actx(), "logo.png", bytes.NewReader(nil))), "file", "required")
	})
	t.Run("nil reader", func(t *testing.T) {
		wantField(t, errOf(u.svc.SetLogo(u.actx(), "logo.png", nil)), "file", "required")
	})
	if len(u.fs.Staged()) != 0 || len(u.fs.Files()) != 0 {
		t.Error("something was staged for an empty upload")
	}
}

// ---- F46: the object store is unavailable ------------------------------------------------

func TestSetLogo_StoreUnavailable_ReturnsUnavailable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		kind errs.Kind
	}{
		{"unavailable", errs.E("test", errs.Unavailable, errors.New("store is down")), errs.Unavailable},
		{"timeout", errs.E("test", errs.Timeout, errors.New("too slow")), errs.Timeout},
		{"canceled", errs.E("test", errs.Canceled, errors.New("gave up")), errs.Canceled},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u := newLogoUnit(t)
			u.fs.Fail(tc.err)
			wantKind(t, errOf(u.svc.SetLogo(u.actx(), "logo.png", bytes.NewReader(pngOf(t, 1)))), tc.kind)
			if len(u.rec.Recorded()) != 0 {
				t.Errorf("audit entries after a failed Stage: %v", actionsOf(u.rec.Recorded()))
			}
			u.fs.Fail(nil)
			if len(u.fs.Files()) != 0 || len(u.fs.Staged()) != 0 {
				t.Error("something was written despite the failure")
			}
			u.noStatements(t, "a failed Stage")
		})
	}
}

// ---- F37: the PUT handler's own refusals -------------------------------------------------

func TestPutLogo_NotMultipart_Returns415(t *testing.T) {
	u := newLogoUnit(t)
	for name, ct := range map[string]string{
		"json":        "application/json",
		"no type":     "",
		"urlencoded":  "application/x-www-form-urlencoded",
		"no boundary": "multipart/form-data",
	} {
		t.Run(name, func(t *testing.T) {
			w, f := u.send(u.actx(), http.MethodPut, "/logo", []byte(`{"file":"x"}`), ct)
			err := f.last()
			wantKind(t, err, errs.Invalid)
			if !errors.Is(err, httpx.ErrUnsupportedMediaType) {
				t.Errorf("error does not wrap httpx.ErrUnsupportedMediaType: %v", err)
			}
			_ = w
		})
	}
	if st, _, _ := u.spy.counts(); st != 0 {
		t.Errorf("Stage called %d times for a non-multipart body", st)
	}
}

func TestPutLogo_NoFilePart_ReturnsInvalidRequired(t *testing.T) {
	u := newLogoUnit(t)
	for name, fields := range map[string][][2]string{
		"no parts at all":  nil,
		"only text fields": {{"note", "hello"}},
	} {
		t.Run(name, func(t *testing.T) {
			body, ct := multipartOf(t, fields, "", "", nil)
			w, f := u.send(u.actx(), http.MethodPut, "/logo", body, ct)
			if w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", w.Code)
			}
			wantField(t, f.last(), "file", "required")
		})
	}
	if st, _, _ := u.spy.counts(); st != 0 {
		t.Errorf("Stage called %d times with no file part", st)
	}
}

func TestPutLogo_NoActor_ReturnsUnauthenticated(t *testing.T) {
	u := newLogoUnit(t)
	body, ct := multipartOf(t, nil, "logo.png", "image/png", pngOf(t, 1))
	w, f := u.send(t.Context(), http.MethodPut, "/logo", body, ct)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (%v)", w.Code, f.last())
	}
	wantKind(t, f.last(), errs.Unauthenticated)
}

func TestPutLogo_NoPermission_Returns403(t *testing.T) {
	u := newLogoUnit(t)
	body, ct := multipartOf(t, nil, "logo.png", "image/png", pngOf(t, 1))
	w, f := u.send(asPerson(t.Context(), newPerson()), http.MethodPut, "/logo", body, ct)
	if w.Code != http.StatusForbidden {
		t.Errorf("PUT status = %d, want 403", w.Code)
	}
	wantKind(t, f.last(), errs.Permission)
	w, f = u.send(asPerson(t.Context(), newPerson()), http.MethodDelete, "/logo", nil, "")
	if w.Code != http.StatusForbidden {
		t.Errorf("DELETE status = %d, want 403", w.Code)
	}
	wantKind(t, f.last(), errs.Permission)
}

// ---- F51 to F53: reading the logo --------------------------------------------------------

func TestGetLogo_None_ReturnsNotFound(t *testing.T) {
	u := newLogoUnit(t)
	_, c, err := u.svc.OpenLogo(t.Context()) // public: no actor at all
	closeContent(t, c)
	wantKind(t, err, errs.NotFound)
	w, f := u.send(t.Context(), http.MethodGet, "/logo", nil, "")
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (%v)", w.Code, f.last())
	}
	u.noStatements(t, "OpenLogo")
}

func TestGetLogo_ObjectMissing_ReturnsUnavailable(t *testing.T) {
	u := newLogoUnit(t)
	u.seedLogo(t, "logo.png", pngOf(t, 1))
	u.spy.setOpenErr(errs.E("test", errs.Unavailable, objects.ErrMissing))
	_, c, err := u.svc.OpenLogo(t.Context())
	closeContent(t, c)
	wantKind(t, err, errs.Unavailable)
	if !errors.Is(err, objects.ErrMissing) {
		t.Errorf("error does not wrap objects.ErrMissing: %v", err)
	}
	w, _ := u.send(t.Context(), http.MethodGet, "/logo", nil, "")
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

func TestGetLogo_ServesTheLogoWithoutCacheControl(t *testing.T) {
	u := newLogoUnit(t)
	data := pngOf(t, 9)
	seeded := u.seedLogo(t, "logo.png", data)

	_, c, err := u.svc.OpenLogo(t.Context())
	wantNoErr(t, err)
	closeContent(t, c)

	// No actor, and the shell's ?v=<id> query, both fine.
	w, f := u.send(t.Context(), http.MethodGet, "/logo?v="+seeded.ID.String(), nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (%v)", w.Code, f.last())
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q", ct)
	}
	if !bytes.Equal(w.Body.Bytes(), data) {
		t.Error("the body is not the logo's bytes")
	}
	if cc := w.Header().Get("Cache-Control"); cc != "" {
		t.Errorf("Cache-Control = %q; the handler must set none (files decision 25)", cc)
	}
	if w.Header().Get("ETag") == "" {
		t.Error("no ETag")
	}
	if csp := w.Header().Get("Content-Security-Policy"); csp != "default-src 'none'" {
		t.Errorf("Content-Security-Policy = %q", csp)
	}
	if u.az != nil && len(u.rec.Recorded()) != 0 {
		t.Errorf("a public logo read was audited: %v", actionsOf(u.rec.Recorded()))
	}
}

func TestGetLogo_SetsNoCacheControl(t *testing.T) {
	u := newLogoUnit(t)
	u.seedLogo(t, "logo.png", pngOf(t, 2))
	w, _ := u.send(t.Context(), http.MethodGet, "/logo", nil, "")
	if _, present := w.Header()["Cache-Control"]; present {
		t.Errorf("Cache-Control present: %q", w.Header().Values("Cache-Control"))
	}
}

func TestGetLogo_IfNoneMatch_Returns304(t *testing.T) {
	u := newLogoUnit(t)
	u.seedLogo(t, "logo.png", pngOf(t, 3))
	first, _ := u.send(t.Context(), http.MethodGet, "/logo", nil, "")
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag on the first response")
	}
	w, _ := u.send(t.Context(), http.MethodGet, "/logo", nil, "", "If-None-Match", etag)
	if w.Code != http.StatusNotModified {
		t.Errorf("status = %d, want 304", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Errorf("a 304 carried %d body bytes", w.Body.Len())
	}
	w, _ = u.send(t.Context(), http.MethodGet, "/logo", nil, "", "If-None-Match", `"some-other-tag"`)
	if w.Code != http.StatusOK {
		t.Errorf("a different tag: status = %d, want 200", w.Code)
	}
}

// OpenLogo serves the newest file of the record (settings.md, OpenLogo step).
func TestOpenLogo_SeveralFiles_ServesTheNewest(t *testing.T) {
	u := newLogoUnit(t)
	u.seedLogo(t, "old.png", pngOf(t, 10))
	want := u.seedLogo(t, "new.png", pngOf(t, 200))
	f, c, err := u.svc.OpenLogo(t.Context())
	wantNoErr(t, err)
	defer func() { wantNoErr(t, c.Close()) }()
	if f.ID != want.ID {
		t.Errorf("OpenLogo returned file %s, want the newest %s", f.ID, want.ID)
	}
}

// ---- F54 is in the integration file (it needs the first snapshot load) -----------------
