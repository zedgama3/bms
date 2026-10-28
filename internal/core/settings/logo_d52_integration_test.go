//go:build integration

package settings_test

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/lib/errs"
)

const uploadMarker = "Quincy-Test-Camp-Banner"

func wantLogoName(t *testing.T, f files.File) {
	t.Helper()
	if f.Name != "logo" && !strings.HasPrefix(f.Name, "logo.") {
		t.Errorf("stored name = %q, want \"logo\" (plus the kind's extension)", f.Name)
	}
	if strings.Contains(f.Name, uploadMarker) || strings.Contains(f.Name, "passwd") {
		t.Errorf("the upload's name reached the stored name: %q", f.Name)
	}
}

// D52(e): the stored name is "logo" whatever the upload was called (fake store and real store).
func TestSetLogo_StoredName_IsLogoWhateverTheUploadWasCalled(t *testing.T) {
	names := []string{uploadMarker + ".png", "../../etc/passwd.png", "", "a b c.PNG", "logo.jpg"}
	rigs := map[string]func(*testing.T) *logoI{
		"fake store": func(t *testing.T) *logoI { return newLogoI(t) },
		"real store": func(t *testing.T) *logoI { return newRealLogoI(t, files.DefaultPolicy()) },
	}
	for rn, mk := range rigs {
		for _, name := range names {
			t.Run(rn+"/"+name, func(t *testing.T) {
				r := mk(t)
				r.mustSetLogo(name, pngOf(t, 1))
				wantLogoName(t, r.onlyFile())
			})
		}
	}
}

func TestSetLogo_Replace_StoredNameStaysLogo(t *testing.T) {
	r := newLogoI(t)
	r.mustSetLogo("one.png", pngOf(t, 1))
	r.mustSetLogo(uploadMarker+".png", pngOf(t, 2))
	wantLogoName(t, r.onlyFile())
}

func TestPutLogo_StoredName_IsLogoNotTheMultipartFilename(t *testing.T) {
	r := newLogoI(t)
	body, ct := multipartOf(t, nil, uploadMarker+".png", "image/png", pngOf(t, 1))
	w, f := r.send(r.actx(), http.MethodPut, "/logo", body, ct)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (%v)", w.Code, f.last())
	}
	wantLogoName(t, r.onlyFile())
	if strings.Contains(w.Body.String(), uploadMarker) {
		t.Error("the response echoes the upload's file name")
	}
}

// The stored name is not a person's data either: nothing in core.file carries the upload name.
func TestSetLogo_RealStore_NoFileRowCarriesTheUploadName(t *testing.T) {
	r := newRealLogoI(t, files.DefaultPolicy())
	r.mustSetLogo(uploadMarker+".png", pngOf(t, 1))
	if n := r.count(`SELECT count(*) FROM core.file WHERE name ILIKE '%' || $1 || '%'`, "quincy"); n != 0 {
		t.Errorf("%d core.file rows carry the upload's name", n)
	}
}

// D52(e): the cap boundary against the real stack.
func TestSetLogo_CapBoundary_RealStore(t *testing.T) {
	r := newRealLogoI(t, files.DefaultPolicy())
	_, err := r.set("logo.png", padded(pngOf(t, 1), int(settings.MaxLogoBytes)+1))
	wantField(t, err, "file", "too_large")
	if r.fileRows() != 0 {
		t.Errorf("%d file rows after a too-large upload", r.fileRows())
	}
}

func TestSetLogo_CanceledContext_StoresNothing(t *testing.T) {
	r := newRealLogoI(t, files.DefaultPolicy())
	ctx, cancel := context.WithCancel(r.actx())
	cancel()
	_, err := r.svc.SetLogo(ctx, "logo.png", bytes.NewReader(pngOf(t, 1)))
	if err == nil {
		t.Fatal("SetLogo with a canceled context succeeded")
	}
	if !errs.Is(err, errs.Canceled) {
		t.Errorf("kind = %v, want Canceled (%v)", errs.KindOf(err), err)
	}
	if r.fileRows() != 0 {
		t.Error("a file row was written")
	}
}
