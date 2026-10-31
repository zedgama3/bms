package app

// Logo wiring (settings.md amendment 2026-10-21, L9 and Proposed change 14).

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
)

func TestLazyFilesStore_BeforeFiles_ReturnsUnavailable(t *testing.T) {
	l := lazyFilesStore{a: &assembly{}} // a.files is nil: files' part has not run
	ctx := context.Background()
	var staged files.Staged
	var id files.FileID

	tests := []struct {
		name string
		call func() error
	}{
		{"Stage", func() error {
			_, err := l.Stage(ctx, files.Upload{Name: "logo.png", Accept: files.Brand, Body: bytes.NewReader([]byte("x"))})
			return err
		}},
		{"Attach", func() error {
			_, err := l.Attach(ctx, staged, files.Attachment{Record: settings.LogoRef})
			return err
		}},
		{"Discard", func() error { return l.Discard(ctx, staged) }},
		{"Get", func() error { _, err := l.Get(ctx, id); return err }},
		{"ByRecord", func() error { _, err := l.ByRecord(ctx, settings.LogoRef); return err }},
		{"Open", func() error {
			_, c, err := l.Open(ctx, id)
			if c != nil {
				return errors.Join(err, c.Close())
			}
			return err
		}},
		{"Delete", func() error { return l.Delete(ctx, id) }},
		{"DeleteByRecord", func() error { _, err := l.DeleteByRecord(ctx, settings.LogoRef); return err }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errs.Is(err, errs.Unavailable) {
				t.Errorf("kind = %v (%v), want Unavailable", errs.KindOf(err), err)
			}
		})
	}
}

// Once files is built the lazy store is files scoped "settings": a lookup of the logo answers
// an empty list, not an error, and a record of another area is refused by the scope.
func TestLazyFilesStore_AfterFiles_IsScopedSettings(t *testing.T) {
	a := builtCore(t)
	if a.files == nil {
		t.Skip("files was not built in this assembly")
	}
	l := lazyFilesStore{a: a}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// db.None() refuses every statement, so the lookup fails with a store kind (never
	// Internal or Invalid): the adapter reached files' scoped store for "settings".
	_, err := l.ByRecord(ctx, settings.LogoRef)
	if err != nil && (errs.Is(err, errs.Invalid) || errs.Is(err, errs.Internal)) {
		t.Errorf("ByRecord(LogoRef) = %v: the scoped store rejected the settings record", err)
	}
}

func TestSettingsRoutes_CarryBodyCapAndTimeout(t *testing.T) {
	in := []httpx.Route{
		{Method: http.MethodGet, Pattern: "/public", Public: true, Handler: http.NotFoundHandler()},
		{Method: http.MethodPut, Pattern: "/logo", Handler: http.NotFoundHandler(), MaxBodyBytes: 2 * settings.MaxLogoBytes, Timeout: files.UploadTimeout},
		{Method: http.MethodGet, Pattern: "/logo", Public: true, Handler: http.NotFoundHandler(), Timeout: files.ServeTimeout},
	}
	set, err := module.CoreRoutes("settings", in, nil)
	if err != nil {
		t.Fatalf("CoreRoutes: %v", err)
	}
	out := set.Routes
	if len(out) != len(in) {
		t.Fatalf("%d routes out, %d in", len(out), len(in))
	}
	if out[0].MaxBodyBytes != 0 || out[0].Timeout != 0 {
		t.Errorf("a route without limits got %d / %v", out[0].MaxBodyBytes, out[0].Timeout)
	}
	if out[1].MaxBodyBytes != 2*settings.MaxLogoBytes || out[1].Timeout != files.UploadTimeout || out[1].Access == module.AccessPublic {
		t.Errorf("PUT /logo = %+v", out[1])
	}
	if out[2].Timeout != files.ServeTimeout || out[2].Access != module.AccessPublic {
		t.Errorf("GET /logo = %+v", out[2])
	}
}

// The assembled settings routes include the three logo routes with the spec's limits.
func TestBuiltSettingsRoutes_IncludeTheLogo(t *testing.T) {
	a := builtCore(t)
	var found int
	for _, rs := range a.routeSets {
		if rs.Module != "settings" {
			continue
		}
		for _, r := range rs.Routes {
			if r.Pattern != "/logo" {
				continue
			}
			found++
			switch r.Method {
			case http.MethodPut:
				if r.MaxBodyBytes != 2*settings.MaxLogoBytes || r.Timeout != files.UploadTimeout {
					t.Errorf("PUT /logo limits = %d / %v", r.MaxBodyBytes, r.Timeout)
				}
			case http.MethodGet:
				if r.Access != module.AccessPublic {
					t.Error("GET /logo is not public")
				}
			}
		}
	}
	if found != 3 {
		t.Errorf("%d /logo routes in the settings route set, want 3", found)
	}
}
