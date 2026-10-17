package api_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/zedgama3/bms/internal/api"
	"github.com/zedgama3/bms/internal/lib/httpx"
)

func uiFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":           {Data: []byte("<!doctype html><title>bms</title><div id=root></div>")},
		"assets/app-abc123.js": {Data: []byte("console.log(1)")},
		"favicon.ico":          {Data: []byte("ico")},
		"robots.txt":           {Data: []byte("User-agent: *")},
	}
}

func uiRig(t *testing.T, mut ...func(*api.Options)) *rig {
	t.Helper()
	return newRig(t, append([]func(*api.Options){func(o *api.Options) { o.UI = uiFS() }}, mut...)...)
}

func TestUI_HashedAssetImmutable(t *testing.T) {
	r := uiRig(t)
	rec := r.do("GET", "/assets/app-abc123.js")
	if rec.Code != 200 || rec.Body.String() != "console.log(1)" {
		t.Fatalf("%d %q", rec.Code, rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control = %q", cc)
	}
	for _, p := range []string{"/favicon.ico", "/robots.txt"} {
		rec := r.do("GET", p)
		if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-cache" {
			t.Errorf("%s: %d Cache-Control %q, want no-cache", p, rec.Code, rec.Header().Get("Cache-Control"))
		}
	}
}

func TestUI_IndexForAppPathWithCSPAndNoCache(t *testing.T) {
	r := uiRig(t)
	for _, p := range []string{"/people", "/people/7", "/some/deep/app/path", "/assets/"} {
		t.Run(p, func(t *testing.T) {
			rec := r.do("GET", p)
			if rec.Code != 200 || !strings.Contains(rec.Body.String(), "<div id=root>") {
				t.Fatalf("%d %q", rec.Code, rec.Body.String())
			}
			if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
				t.Errorf("Cache-Control = %q", cc)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
				t.Errorf("Content-Type = %q", ct)
			}
			if csp := rec.Header().Get("Content-Security-Policy"); csp != api.DefaultCSP {
				t.Errorf("CSP = %q", csp)
			}
		})
	}
}

func TestUI_IndexByNameCarriesCSP(t *testing.T) {
	r := uiRig(t)
	for _, p := range []string{"/", "/index.html"} {
		t.Run(p, func(t *testing.T) {
			rec := r.do("GET", p)
			if rec.Code >= 300 && rec.Code < 400 { // a clean-URL redirect is fine; follow it
				rec = r.do("GET", rec.Header().Get("Location"))
			}
			if rec.Code != 200 {
				t.Fatalf("status = %d", rec.Code)
			}
			if csp := rec.Header().Get("Content-Security-Policy"); csp != api.DefaultCSP {
				t.Errorf("CSP = %q", csp)
			}
		})
	}
	// HEAD too.
	rec := r.do("HEAD", "/people")
	if rec.Code != 200 || rec.Body.Len() != 0 || rec.Header().Get("Content-Security-Policy") == "" {
		t.Errorf("HEAD: %d %d bytes csp %q", rec.Code, rec.Body.Len(), rec.Header().Get("Content-Security-Policy"))
	}
}

func TestUI_PathTraversalIs404(t *testing.T) {
	fsys := uiFS()
	fsys["secret.txt"] = &fstest.MapFile{Data: []byte("TOPSECRET")}
	r := newRig(t, func(o *api.Options) { o.UI = fsys })
	// NUL bytes are refused outright.
	wantProblem(t, r.do("GET", "/a%00b.txt"), 404, httpx.CodeNotFound)
	for _, p := range []string{"/../etc/passwd", "/assets/../../etc/passwd", "/%2e%2e/etc/passwd", "/assets/%2e%2e/%2e%2e/etc/passwd", "/..%2fetc/passwd"} {
		t.Run(p, func(t *testing.T) {
			rec := r.do("GET", p)
			if strings.Contains(rec.Body.String(), "root:") {
				t.Fatalf("served a file outside the FS: %q", rec.Body.String())
			}
			switch rec.Code {
			case 404, 301, 302, 307, 308:
				if loc := rec.Header().Get("Location"); strings.Contains(loc, "..") {
					t.Fatalf("redirect to an unclean location %q", loc)
				}
			case 200:
				// An app path gets index.html; never anything else.
				if !strings.Contains(rec.Body.String(), "<div id=root>") {
					t.Fatalf("200 with %q", rec.Body.String())
				}
			default:
				t.Fatalf("status = %d", rec.Code)
			}
		})
	}
}

func TestUI_Table(t *testing.T) {
	t.Run("UI nil: non-API path is 404", func(t *testing.T) {
		r := newRig(t)
		for _, p := range []string{"/", "/people", "/index.html", "/assets/app.js"} {
			wantProblem(t, r.do("GET", p), 404, httpx.CodeNotFound)
		}
		if rec := r.do("GET", "/healthz"); rec.Code != 200 {
			t.Errorf("/healthz = %d", rec.Code)
		}
	})
	t.Run("extension with no file is 404", func(t *testing.T) {
		r := uiRig(t)
		for _, p := range []string{"/missing.js", "/assets/other-1.css", "/x/y/z.png"} {
			wantProblem(t, r.do("GET", p), 404, httpx.CodeNotFound)
		}
	})
	t.Run("non-GET is 405", func(t *testing.T) {
		r := uiRig(t)
		for _, m := range []string{"POST", "PUT", "DELETE", "PATCH"} {
			wantProblem(t, r.do(m, "/people"), 405, httpx.CodeMethodNotAllowed)
		}
	})
	t.Run("API paths never fall back to index.html", func(t *testing.T) {
		r := uiRig(t)
		rec := r.do("GET", "/api/nothing")
		wantProblem(t, rec, 404, httpx.CodeNotFound)
		if strings.Contains(rec.Body.String(), "<div") {
			t.Error("served index.html for an API path")
		}
	})
}

func TestUI_CustomCSP(t *testing.T) {
	const custom = "default-src 'none'"
	r := uiRig(t, func(o *api.Options) { o.ContentSecurityPolicy = custom })
	for _, p := range []string{"/", "/people", "/index.html"} {
		rec := r.do("GET", p)
		if rec.Code >= 300 && rec.Code < 400 {
			rec = r.do("GET", rec.Header().Get("Location"))
		}
		if got := rec.Header().Get("Content-Security-Policy"); got != custom {
			t.Errorf("%s: CSP = %q", p, got)
		}
	}
}

func TestUI_DefaultCSPHasObjectSrcNone(t *testing.T) {
	for _, want := range []string{"default-src 'self'", "object-src 'none'", "frame-ancestors 'none'", "base-uri 'none'", "form-action 'self'", "img-src 'self' data:"} {
		if !strings.Contains(api.DefaultCSP, want) {
			t.Errorf("DefaultCSP lacks %q", want)
		}
	}
	if strings.Contains(api.DefaultCSP, "unsafe-inline") || strings.Contains(api.DefaultCSP, "unsafe-eval") {
		t.Error("DefaultCSP allows inline or eval")
	}
	// JSON responses carry no CSP.
	r := uiRig(t)
	if got := r.do("GET", "/api/modules").Header().Get("Content-Security-Policy"); got != "" {
		t.Errorf("CSP on JSON: %q", got)
	}
	for _, p := range []string{"/missing.js", "/assets/app-abc123.js"} {
		if got := r.do("GET", p).Header().Get("Content-Security-Policy"); got != api.DefaultCSP {
			t.Errorf("%s: CSP = %q, want the default on every UI response", p, got)
		}
	}
	if rec := r.do("POST", "/people"); rec.Header().Get("Content-Security-Policy") != api.DefaultCSP {
		t.Errorf("UI 405 lacks the CSP")
	}
}
