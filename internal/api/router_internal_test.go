package api

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestPatternMatches_Table(t *testing.T) {
	tests := []struct {
		pattern, path string
		want          bool
	}{
		{"GET /api/alpha/{$}", "/api/alpha/", true},
		{"GET /api/alpha/{$}", "/api/alpha", false},
		{"GET /api/alpha/{$}", "/api/alpha/x", false},
		{"GET /api/alpha/things/{id}", "/api/alpha/things/7", true},
		{"GET /api/alpha/things/{id}", "/api/alpha/things/", false},
		{"GET /api/alpha/things/{id}", "/api/alpha/things/7/8", false},
		{"GET /api/alpha/files/{path...}", "/api/alpha/files/a/b/c", true},
		{"GET /api/alpha/files/{path...}", "/api/alpha/files/", true},
		{"POST /api/alpha/shifts/bulk", "/api/alpha/shifts/bulk", true},
		{"/api/", "/api/anything/at/all", true},
		{"/api/", "/other", false},
	}
	for _, tc := range tests {
		t.Run(tc.pattern+" "+tc.path, func(t *testing.T) {
			if got := patternMatches(tc.pattern, tc.path); got != tc.want {
				t.Errorf("patternMatches(%q, %q) = %v, want %v", tc.pattern, tc.path, got, tc.want)
			}
		})
	}
}

func TestOpenAPIPath_Table(t *testing.T) {
	tests := []struct{ in, want string }{
		{"/x/{$}", "/x/"},
		{"/{$}", "/"},
		{"/x/{id}", "/x/{id}"},
		{"/files/{p...}", "/files/{p}"},
		{"/api/alpha/{$}", "/api/alpha/"},
	}
	for _, tc := range tests {
		if got := openAPIPath(tc.in); got != tc.want {
			t.Errorf("openAPIPath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCleanPath_AgreesWithServeMux(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ok/", func(http.ResponseWriter, *http.Request) {})
	for _, p := range []string{"/ok/", "/ok//x", "/ok/./x", "/ok/../ok/x", "//ok/x", "/ok/x/"} {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.test/", nil)
		req.URL.Path = p
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		redirected := rec.Code >= 300 && rec.Code < 400
		if got := cleanPath(p) != p; got != redirected {
			t.Errorf("cleanPath(%q) != path is %v, but the mux redirects: %v", p, got, redirected)
		}
	}
}

func TestDeriveClient_IPv6ZoneStripped(t *testing.T) {
	trusted := trustedSet{mustPrefix("10.0.0.0/8")}
	client, _ := deriveClient(mustAddr("10.0.0.1"), trusted, []string{"fe80::1%eth0"}, nil)
	if client.Zone() != "" || client.String() != "fe80::1" {
		t.Errorf("client = %q, want the address without its zone", client)
	}
}

func mustPrefix(s string) netip.Prefix { return netip.MustParsePrefix(s) }
func mustAddr(s string) netip.Addr     { return netip.MustParseAddr(s) }
