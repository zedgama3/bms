package api_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"

	"github.com/zedgama3/bms/internal/api"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
)

// freeAddr returns a loopback address that was free a moment ago.
func freeAddr(t *testing.T) string {
	t.Helper()
	var lc net.ListenConfig
	l, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	a := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return a
}

func withAddr(a string) func(*api.Options) { return func(o *api.Options) { o.Addr = a } }

// reply is what the tests keep of a real response.
type reply struct {
	status int
	close  bool // the server asked to close the connection
}

// get performs a real GET against base+path and closes the body.
func get(ctx context.Context, base, path string) (reply, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return reply{}, fmt.Errorf("request %s: %w", path, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return reply{}, fmt.Errorf("get %s: %w", path, err)
	}
	out := reply{status: resp.StatusCode, close: resp.Close}
	if err := resp.Body.Close(); err != nil {
		return out, fmt.Errorf("close %s: %w", path, err)
	}
	return out, nil
}

// serveAsync starts Serve and returns a channel carrying its result.
func serveAsync(srv *api.Server) <-chan error {
	ch := make(chan error, 1)
	go func() { ch <- srv.Serve() }()
	return ch
}

func TestListenServeShutdown_RoundTrip(t *testing.T) {
	r := newRig(t, withAddr(freeAddr(t)), withRoutes(alphaSet(t)))
	srv := r.server()
	a, err := srv.Listen(t.Context())
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	done := serveAsync(srv)
	resp, err := get(t.Context(), "http://"+a.String(), "/api/alpha/things/9")
	if err != nil {
		t.Fatal(err)
	}
	if resp.status != 200 {
		t.Fatalf("status = %d", resp.status)
	}
	if err := srv.Shutdown(t.Context()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Serve returned %v, want nil", err)
	}
	// Handler keeps answering after Shutdown.
	if rec := r.do("GET", "/api/alpha/things/9"); rec.Code != 200 {
		t.Fatalf("Handler after Shutdown = %d", rec.Code)
	}
	if len(r.server().OpenAPI()) == 0 {
		t.Fatal("OpenAPI empty after Shutdown")
	}
}

func TestListen_AddrInUse_ReturnsUnavailable(t *testing.T) {
	var lc net.ListenConfig
	l, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := l.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	r := newRig(t, withAddr(l.Addr().String()))
	if _, err := r.server().Listen(t.Context()); !errs.Is(err, errs.Unavailable) {
		t.Fatalf("Listen = %v, want Unavailable", err)
	}
}

func TestListen_Twice_ReturnsConflict(t *testing.T) {
	r := newRig(t, withAddr(freeAddr(t)))
	srv := r.server()
	if _, err := srv.Listen(t.Context()); err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() {
		if err := srv.Shutdown(context.WithoutCancel(t.Context())); err != nil {
			t.Errorf("Shutdown: %v", err)
		}
	})
	if _, err := srv.Listen(t.Context()); !errs.Is(err, errs.Conflict) {
		t.Fatalf("second Listen = %v, want Conflict", err)
	}
}

func TestListen_AfterShutdown_ReturnsConflict(t *testing.T) {
	r := newRig(t, withAddr(freeAddr(t)))
	srv := r.server()
	if err := srv.Shutdown(t.Context()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if _, err := srv.Listen(t.Context()); !errs.Is(err, errs.Conflict) {
		t.Fatalf("Listen after Shutdown = %v, want Conflict", err)
	}
}

func TestServe_WithoutListen_ReturnsInternal(t *testing.T) {
	r := newRig(t)
	if err := r.server().Serve(); !errs.Is(err, errs.Internal) {
		t.Fatalf("Serve = %v, want Internal", err)
	}
}

func TestServe_AfterShutdown_ReturnsNil(t *testing.T) {
	r := newRig(t)
	srv := r.server()
	if err := srv.Shutdown(t.Context()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if err := srv.Serve(); err != nil {
		t.Fatalf("Serve after Shutdown = %v, want nil", err)
	}
}

func TestShutdown_Twice_IsNil(t *testing.T) {
	r := newRig(t, withAddr(freeAddr(t)))
	srv := r.server()
	if _, err := srv.Listen(t.Context()); err != nil {
		t.Fatal(err)
	}
	done := serveAsync(srv)
	for i := 0; i < 2; i++ {
		if err := srv.Shutdown(t.Context()); err != nil {
			t.Fatalf("Shutdown #%d = %v", i+1, err)
		}
	}
	if err := <-done; err != nil {
		t.Fatalf("Serve = %v", err)
	}
}

func TestShutdown_BeforeListen_IsNil(t *testing.T) {
	r := newRig(t)
	if err := r.server().Shutdown(t.Context()); err != nil {
		t.Fatalf("Shutdown = %v", err)
	}
	if err := r.server().Shutdown(t.Context()); err != nil {
		t.Fatalf("second Shutdown = %v", err)
	}
}

func TestShutdown_InFlightOverDeadline_CancelsAndReturnsTimeout(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	clientDone := make(chan struct{})
	set := setOf(t, "alpha", route("GET", "/stuck", module.AccessPublic, func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release // ignores its ctx on purpose
		if err := httpx.WriteJSON(w, 200, map[string]bool{"late": true}); err != nil {
			return // the client may be gone; nothing to do
		}
	}))
	r := newRig(t, withAddr(freeAddr(t)), withRoutes(set))
	srv := r.server()
	a, err := srv.Listen(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	done := serveAsync(srv)
	go func() {
		defer close(clientDone)
		if _, err := get(t.Context(), "http://"+a.String(), "/api/alpha/stuck"); err != nil {
			return // the connection may be cut; only the server's behaviour is under test
		}
	}()
	<-entered
	ctx, cancel := context.WithCancel(t.Context())
	cancel() // the budget is already spent
	err = srv.Shutdown(ctx)
	if !errs.Is(err, errs.Timeout) && !errs.Is(err, errs.Canceled) {
		t.Fatalf("Shutdown = %v, want Timeout or Canceled", err)
	}
	// The handler is still blocked; releasing it now must not panic anything.
	close(release)
	<-clientDone
	if err := srv.Shutdown(t.Context()); err != nil {
		t.Fatalf("second Shutdown = %v, want nil", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Serve = %v", err)
	}
}

func TestShutdown_DrainsInFlightRequest(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	set := setOf(t, "alpha", route("GET", "/slow", module.AccessPublic, func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		if err := httpx.WriteJSON(w, 200, map[string]bool{"done": true}); err != nil {
			return
		}
	}))
	r := newRig(t, withAddr(freeAddr(t)), withRoutes(set))
	srv := r.server()
	a, err := srv.Listen(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	served := serveAsync(srv)

	type result struct {
		rep reply
		err error
	}
	got := make(chan result, 1)
	go func() {
		rep, err := get(t.Context(), "http://"+a.String(), "/api/alpha/slow")
		got <- result{rep: rep, err: err}
	}()
	<-entered

	shut := make(chan error, 1)
	go func() { shut <- srv.Shutdown(t.Context()) }()
	// Shutdown flips the health routes to draining before it waits; spin on the handler
	// (no sleeping) until it does.
	for {
		if rec := r.do("GET", "/healthz"); rec.Code == 503 {
			break
		}
		if t.Context().Err() != nil {
			t.Fatal("test context ended before draining began")
		}
	}
	close(release)
	res := <-got
	if res.err != nil {
		t.Fatalf("in-flight request failed: %v", res.err)
	}
	if res.rep.status != 200 {
		t.Fatalf("status = %d", res.rep.status)
	}
	if !res.rep.close {
		t.Error("response to an in-flight request during drain lacks Connection: close")
	}
	if err := <-shut; err != nil {
		t.Fatalf("Shutdown = %v", err)
	}
	if err := <-served; err != nil {
		t.Fatalf("Serve = %v", err)
	}
}
