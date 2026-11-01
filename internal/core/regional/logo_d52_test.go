package regional_test

// D52(e): the logo's stored name, the permission check before the body is read, and more
// failure modes that need no database.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"testing/iotest"

	"github.com/zedgama3/bms/internal/core/regional"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
)

// ---- permission before the body is read ---------------------------------------------------

type recordingBody struct {
	reads atomic.Int64
	r     io.Reader
}

func (b *recordingBody) Read(p []byte) (int, error) {
	b.reads.Add(1)
	return b.r.Read(p) //nolint:wrapcheck // test double
}

func TestSetLogo_NonAdmin_ReturnsPermissionWithoutReadingTheBody(t *testing.T) {
	u := newLogoUnit(t)
	for name, ctx := range map[string]context.Context{
		"signed in, no grants": asPerson(t.Context(), newPerson()),
		"read only":            asPerson(t.Context(), u.person(settings.PermissionRead)),
	} {
		t.Run(name, func(t *testing.T) {
			body := &recordingBody{r: bytes.NewReader(pngOf(t, 1))}
			_, err := u.svc.SetLogo(ctx, "logo.png", body)
			wantKind(t, err, errs.Permission)
			if n := body.reads.Load(); n != 0 {
				t.Errorf("the body was read %d times before the permission check", n)
			}
		})
	}
}

func TestPutLogo_NonAdmin_Returns403WithoutReadingTheBody(t *testing.T) {
	u := newLogoUnit(t)
	payload, ct := multipartOf(t, nil, "logo.png", "image/png", pngOf(t, 1))
	body := &recordingBody{r: bytes.NewReader(payload)}
	f := &failRec{}
	ctx := httpx.WithFailer(asPerson(t.Context(), newPerson()), f)
	req := httptest.NewRequestWithContext(ctx, http.MethodPut, "/api/regional/logo", body)
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	u.mux().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 (%v)", w.Code, f.last())
	}
	wantKind(t, f.last(), errs.Permission)
	if n := body.reads.Load(); n != 0 {
		t.Errorf("the request body was read %d times before the permission check", n)
	}
	if len(u.fs.Staged()) != 0 || len(u.fs.Files()) != 0 {
		t.Error("something was staged or attached")
	}
}

func TestPutLogo_NoActor_Returns401WithoutReadingTheBody(t *testing.T) {
	u := newLogoUnit(t)
	payload, ct := multipartOf(t, nil, "logo.png", "image/png", pngOf(t, 1))
	body := &recordingBody{r: bytes.NewReader(payload)}
	f := &failRec{}
	req := httptest.NewRequestWithContext(httpx.WithFailer(t.Context(), f), http.MethodPut, "/api/regional/logo", body)
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	u.mux().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
	if n := body.reads.Load(); n != 0 {
		t.Errorf("the request body was read %d times for an unauthenticated caller", n)
	}
}

// ---- size cap and body failures ------------------------------------------------------------

func TestSetLogo_MaxPlusOne_RejectedWhateverTheReadSize(t *testing.T) {
	data := padded(pngOf(t, 1), int(regional.MaxLogoBytes)+1)
	for name, wrap := range map[string]func(io.Reader) io.Reader{
		"plain":      func(r io.Reader) io.Reader { return r },
		"one byte":   iotest.OneByteReader,
		"data+error": iotest.DataErrReader,
	} {
		t.Run(name, func(t *testing.T) {
			u := newLogoUnit(t)
			_, err := u.svc.SetLogo(u.actx(), "logo.png", wrap(bytes.NewReader(data)))
			wantField(t, err, "file", "too_large")
			if len(u.fs.Staged()) != 0 || len(u.fs.Files()) != 0 {
				t.Error("something was staged or attached")
			}
		})
	}
}

func TestSetLogo_BodyFailures_KeepTheirKindAndStoreNothing(t *testing.T) {
	cases := []struct {
		name string
		body io.Reader
		kind errs.Kind
	}{
		{"timeout mid-stream", io.MultiReader(bytes.NewReader(pngOf(t, 1)[:20]), iotest.ErrReader(errs.E("test", errs.Timeout, errors.New("slow")))), errs.Timeout},
		{"unavailable mid-stream", io.MultiReader(bytes.NewReader(pngOf(t, 1)[:20]), iotest.ErrReader(errs.E("test", errs.Unavailable, errors.New("reset")))), errs.Unavailable},
		{"empty body", bytes.NewReader(nil), errs.Invalid},
		{"nil body", nil, errs.Invalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u := newLogoUnit(t)
			_, err := u.svc.SetLogo(u.actx(), "logo.png", tc.body)
			wantKind(t, err, tc.kind)
			if len(u.fs.Files()) != 0 {
				t.Error("a file was attached after a failed upload")
			}
			if _, _, d := u.spy.counts(); len(u.fs.Staged()) != 0 && d == 0 {
				t.Error("a staged object was left behind")
			}
		})
	}
}
