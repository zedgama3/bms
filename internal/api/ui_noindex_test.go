package api_test

import (
	"testing"

	"github.com/zedgama3/bms/internal/api"
)

const noIndexValue = "noindex, nofollow"

// F63 (D53): with Options.NoIndex every UI-handler response carries X-Robots-Tag.
func TestUI_NoIndex_SetsRobotsHeader(t *testing.T) {
	r := uiRig(t, func(o *api.Options) { o.NoIndex = true })
	paths := []string{"/", "/index.html", "/people", "/some/deep/app/path", "/assets/app-abc123.js", "/favicon.ico", "/robots.txt", "/accounts/password-reset"}
	for _, p := range paths {
		for _, method := range []string{"GET", "HEAD"} {
			t.Run(method+" "+p, func(t *testing.T) {
				rec := r.do(method, p)
				if rec.Code >= 300 && rec.Code < 400 {
					rec = r.do(method, rec.Header().Get("Location"))
				}
				if rec.Code != 200 {
					t.Fatalf("status = %d", rec.Code)
				}
				if got := rec.Header().Get("X-Robots-Tag"); got != noIndexValue {
					t.Errorf("X-Robots-Tag = %q, want %q", got, noIndexValue)
				}
			})
		}
	}
}

// F64 (D53): without NoIndex there is no such header, and /api/ JSON never has it.
func TestUI_NoIndexOff_NoHeader(t *testing.T) {
	r := uiRig(t)
	for _, p := range []string{"/", "/people", "/assets/app-abc123.js", "/robots.txt"} {
		t.Run(p, func(t *testing.T) {
			rec := r.do("GET", p)
			if rec.Code >= 300 && rec.Code < 400 {
				rec = r.do("GET", rec.Header().Get("Location"))
			}
			if got := rec.Header().Get("X-Robots-Tag"); got != "" {
				t.Errorf("X-Robots-Tag = %q, want none", got)
			}
		})
	}
}

func TestAPI_NoIndexOn_JSONRoutesNeverCarryHeader(t *testing.T) {
	r := uiRig(t, func(o *api.Options) { o.NoIndex = true })
	for _, p := range []string{"/api/openapi.json", "/api/does-not-exist", "/healthz", "/readyz"} {
		t.Run(p, func(t *testing.T) {
			rec := r.do("GET", p)
			if got := rec.Header().Get("X-Robots-Tag"); got != "" {
				t.Errorf("%s: X-Robots-Tag = %q, want none on non-UI responses", p, got)
			}
		})
	}
}
