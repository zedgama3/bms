package api_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/zedgama3/bms/internal/api"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
)

func TestListen_BadAddr_ReturnsInvalid(t *testing.T) {
	r := newRig(t, withAddr("nohost.invalid:80"))
	if err := r.opts.Validate(); err != nil {
		t.Fatalf("Validate must accept a syntactically valid host:port: %v", err)
	}
	if _, err := r.server().Listen(t.Context()); !errs.Is(err, errs.Invalid) {
		t.Fatalf("Listen = %v, want Invalid", err)
	}
}

func TestListenThenShutdown_WithoutServe_ReleasesPort(t *testing.T) {
	r := newRig(t, withAddr(freeAddr(t)))
	srv := r.server()
	a, err := srv.Listen(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Shutdown(t.Context()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	var lc net.ListenConfig
	l, err := lc.Listen(t.Context(), "tcp", a.String())
	if err != nil {
		t.Fatalf("port still held after Shutdown: %v", err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestErrors_4xxMessageNotOnSpan_5xxScrubbed(t *testing.T) {
	var cur error
	r := errRig(t, &cur, module.AccessPublic)
	cur = errs.E("alpha.op", errs.Invalid, errors.New("value "+marker+" is wrong"))
	r.do("GET", "/api/alpha/e")
	for _, s := range r.cap.Spans() {
		for k, v := range s.Attrs {
			if strings.Contains(v, marker) {
				t.Errorf("4xx message on span attr %s=%q", k, v)
			}
		}
		if strings.Contains(s.Name, marker) {
			t.Error("marker in span name")
		}
	}
	s, ok := spanNamed(r.cap, "GET /api/alpha/e")
	if !ok || s.ErrorKind != "invalid" {
		t.Errorf("span = %+v, want error kind invalid", s)
	}

	r2 := errRig(t, &cur, module.AccessPublic)
	cur = errs.E("alpha.op", errs.Internal, errors.New("kaboomtext for "+emailMarker))
	r2.do("GET", "/api/alpha/e")
	var found bool
	for _, sp := range r2.cap.Spans() {
		for _, v := range sp.Attrs {
			if strings.Contains(v, "kaboomtext") {
				found = true
			}
		}
	}
	if !found {
		t.Error("a 5xx span should record the scrubbed message")
	}
	if r2.cap.Contains(emailMarker) {
		t.Error("e-mail not scrubbed")
	}
}

func TestUI_EveryResponseCarriesCSP(t *testing.T) {
	fsys := uiFS()
	fsys["README"] = &fstest.MapFile{Data: []byte("<!doctype html><html><body>hi</body></html>")}
	fsys["pic.svg"] = &fstest.MapFile{Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`)}
	fsys["page.xhtml"] = &fstest.MapFile{Data: []byte(`<html xmlns="http://www.w3.org/1999/xhtml"></html>`)}
	r := newRig(t, func(o *api.Options) { o.UI = fsys })
	for _, p := range []string{"/README", "/pic.svg", "/page.xhtml", "/favicon.ico", "/robots.txt", "/assets/app-abc123.js", "/", "/people", "/missing.js"} {
		t.Run(p, func(t *testing.T) {
			rec := r.do("GET", p)
			if csp := rec.Header().Get("Content-Security-Policy"); csp != api.DefaultCSP {
				t.Errorf("status %d type %q: CSP = %q", rec.Code, rec.Header().Get("Content-Type"), csp)
			}
		})
	}
}

func TestRouter_PutOnTrailingSlashRoute_404LikeGet(t *testing.T) {
	set := setOf(t, "alpha", route("GET", "/x/", module.AccessPublic, textHandler("x")))
	r := newRig(t, withRoutes(set))
	wantProblem(t, r.do("GET", "/api/alpha/x"), 404, httpx.CodeNotFound)
	wantProblem(t, r.do("PUT", "/api/alpha/x"), 404, httpx.CodeNotFound)
	rec := r.do("PUT", "/api/alpha/x/")
	wantProblem(t, rec, 405, httpx.CodeMethodNotAllowed)
}

func TestRouter_EncodedSlashInPathValue(t *testing.T) {
	r := newRig(t, withRoutes(alphaSet(t)))
	rec := r.do("GET", "/api/alpha/things/a%2Fb")
	if rec.Code != 200 {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if got := decodeMap(t, rec.Body.Bytes())["id"]; got != "a/b" {
		t.Fatalf("id = %v, want a/b", got)
	}
}

type panicOnceDB struct {
	fakeDB
	mu2   sync.Mutex
	armed bool
}

func (d *panicOnceDB) Ping(ctx context.Context) error {
	d.mu2.Lock()
	p := d.armed
	d.armed = false
	d.mu2.Unlock()
	if p {
		panic("ping exploded")
	}
	return d.fakeDB.Ping(ctx)
}

func TestReadyz_PingPanics_DoesNotWedge(t *testing.T) {
	d := &panicOnceDB{armed: true}
	r := newRig(t, func(o *api.Options) { o.Database = d })
	h := r.server().Handler()
	call := func() int {
		done := make(chan int, 1)
		go func() {
			defer func() {
				if recover() != nil {
					done <- -1
				}
			}()
			done <- serve(t, h, "GET", "/readyz").Code
		}()
		select {
		case c := <-done:
			return c
		case <-t.Context().Done():
			t.Fatal("/readyz wedged")
			return 0
		}
	}
	if c := call(); c == 200 {
		t.Fatalf("first call after a panicking ping = 200")
	}
	if err := r.clk.Advance(time.Second); err != nil {
		t.Fatal(err)
	}
	if c := call(); c != 200 {
		t.Fatalf("later call = %d, want 200", c)
	}
	if d.pingCount() != 1 {
		t.Fatalf("successful pings = %d, want 1", d.pingCount())
	}
	// And concurrent waiters on a panicking flight are released.
	d.mu2.Lock()
	d.armed = true
	d.mu2.Unlock()
	if err := r.clk.Advance(time.Second); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer swallowPanic()
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, newRequest(t, "GET", "/readyz"))
		}()
	}
	wg.Wait()
}

func TestPipeline_ErrAbortHandler_Records499(t *testing.T) {
	set := setOf(t, "alpha", route("GET", "/abort", module.AccessPublic, func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))
	r := newRig(t, withRoutes(set))
	h := r.server().Handler()
	func() {
		defer swallowPanic()
		h.ServeHTTP(httptest.NewRecorder(), newRequest(t, "GET", "/api/alpha/abort"))
	}()
	s, ok := spanNamed(r.cap, "GET /api/alpha/abort")
	if !ok || s.Attrs["http.response.status_code"] != "499" {
		t.Fatalf("span = %+v, want status 499", s)
	}
	var seen bool
	for _, m := range r.cap.Measurements() {
		if m.Instrument != "http.server.request.duration" {
			continue
		}
		for _, v := range m.Attrs {
			seen = seen || v == "499"
		}
	}
	if !seen {
		t.Error("histogram sample lacks status 499")
	}
}

// swallowPanic is deferred directly, so recover stops the panic.
func swallowPanic() {
	if v := recover(); v != nil {
		return
	}
}
