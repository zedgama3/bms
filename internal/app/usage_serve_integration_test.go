//go:build integration

package app

import (
	"errors"
	"io/fs"
	"net/http"
	"testing"
	"testing/fstest"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/web"
)

// F41 (D54): a missing client manifest warns once and serve starts, with empty allowlists.
func TestServe_ClientManifestMissing_WarnsAndStarts(t *testing.T) {
	oldFS, oldManifest := webFS, webManifest
	webFS = func() (fs.FS, error) {
		return fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<!doctype html><title>x</title>")}}, nil
	}
	webManifest = func() (web.Manifest, error) {
		return web.Manifest{}, errs.E("web.ClientManifest", errs.NotFound, errors.New("none"))
	}
	t.Cleanup(func() { webFS, webManifest = oldFS, oldManifest })

	env, addr := itEnv(t, itDB(t).String())
	s := itServe(t, env, addr)
	if code, _ := itGet(t.Context(), t, addr, "/api/modules"); code != http.StatusOK {
		t.Errorf("GET /api/modules = %d, want 200", code)
	}
	r := s.stop(t)
	if r.code != ExitOK {
		t.Fatalf("status = %d, want ExitOK\n%s", r.code, r.errOut)
	}
	recs := itRecords(t, r.out)
	if n := itCount(recs, "WARN", "client manifest"); n != 1 {
		t.Errorf("%d client manifest warnings, want exactly 1\n%s", n, r.out)
	}
}
