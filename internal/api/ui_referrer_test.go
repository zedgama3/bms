package api_test

import (
	"strings"
	"testing"
)

// The document for the password-reset page sends no referrer; every other document keeps
// the site default (api.md pipeline step 4, F60; D40).
func TestUI_ReferrerPolicy_ByPath_Table(t *testing.T) {
	r := uiRig(t)
	const def = "strict-origin-when-cross-origin"
	tests := []struct {
		name, path, want string
	}{
		{"reset page", "/accounts/password-reset", "no-referrer"},
		{"reset page with token", "/accounts/password-reset?token=abc", "no-referrer"},
		{"reset page trailing slash", "/accounts/password-reset/", "no-referrer"},
		{"sign-in page", "/accounts/sign-in", def},
		{"home", "/", def},
		{"a longer path", "/accounts/password-reset/extra", def},
		{"a static file", "/robots.txt", def},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := r.do("GET", tc.path)
			if rec.Code >= 300 && rec.Code < 400 {
				rec = r.do("GET", rec.Header().Get("Location"))
			}
			if rec.Code != 200 {
				t.Fatalf("status = %d", rec.Code)
			}
			if got := rec.Header().Get("Referrer-Policy"); got != tc.want {
				t.Errorf("Referrer-Policy = %q, want %q", got, tc.want)
			}
		})
	}
}

// F60 (D40, api.md pipeline step 4): the document for /accounts/password-reset carries
// Referrer-Policy: no-referrer, so the one-time token in its URL is never sent as a referrer.

func TestUIHandler_PasswordResetPage_NoReferrer(t *testing.T) {
	r := uiRig(t)
	tests := []struct{ name, method, path string }{
		{"GET", "GET", "/accounts/password-reset"},
		{"GET with token", "GET", "/accounts/password-reset?token=invented-token-0001"},
		{"GET with empty token", "GET", "/accounts/password-reset?token="},
		{"HEAD", "HEAD", "/accounts/password-reset"},
		{"HEAD with token", "HEAD", "/accounts/password-reset?token=invented-token-0002"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := r.do(tc.method, tc.path)
			if rec.Code != 200 {
				t.Fatalf("status = %d", rec.Code)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
				t.Fatalf("Content-Type = %q, want the document", ct)
			}
			if got := rec.Header().Values("Referrer-Policy"); len(got) != 1 || got[0] != "no-referrer" {
				t.Errorf("Referrer-Policy = %q, want exactly [no-referrer]", got)
			}
			if rec.Header().Get("Content-Security-Policy") == "" {
				t.Error("the document lost its CSP")
			}
		})
	}
}

func TestUIHandler_OtherPath_KeepsDefault(t *testing.T) {
	r := uiRig(t)
	const def = "strict-origin-when-cross-origin"
	paths := []string{
		"/", "/accounts/sign-in", "/accounts/password-reset-other", "/accounts/password-reset/extra",
		"/people", "/robots.txt", "/assets/app-abc123.js", "/missing.js",
		"/api/accounts/password-reset", "/api/accounts/password-reset/complete",
	}
	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			rec := r.do("GET", p)
			if got := rec.Header().Values("Referrer-Policy"); len(got) != 1 || got[0] != def {
				t.Errorf("status %d, Referrer-Policy = %q, want exactly [%s]", rec.Code, got, def)
			}
		})
	}
}
