package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/netip"
	"runtime/debug"
	"time"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
)

// serveHTTP is the outer half of the middleware chain (api.md "The middleware chain"):
// recovery, the peer check, the client address and scheme, security headers, the body cap,
// the request deadline, draining, and dispatch to the router.
func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	rw := &respWriter{ResponseWriter: w, s: s, scheme: "http"}
	defer func() {
		if rec := recover(); rec != nil {
			s.recoverOuter(rw, r, rec)
		}
	}()

	s.setStaticHeaders(rw, r)

	peerPort, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		s.writeOuter(rw, r, errs.E("api.pipeline", errs.Internal, errBadRemoteAddr))
		return
	}
	peer := peerPort.Addr().Unmap()

	if s.misdirected(peer, r) {
		s.misd.note(r.Context(), peer, s.opts.Clock.Now())
		s.writeOuterProblem(rw, http.StatusForbidden, httpx.CodeMisdirected)
		return
	}

	client, scheme := deriveClient(peer, s.trusted, r.Header.Values("X-Forwarded-For"), r.Header.Values("X-Forwarded-Proto"))
	if !s.trusted.contains(peer) && peer.IsLoopback() && len(r.Header.Values("X-Forwarded-For")) > 0 {
		if s.warnedForwarded.CompareAndSwap(false, true) {
			s.log.WarnContext(r.Context(), "a loopback peer that is not in http.trusted_proxies sent X-Forwarded-For; its headers are ignored. If a front end runs on this host, list 127.0.0.1/32")
		}
	}
	rw.scheme = scheme
	if scheme == "https" {
		rw.Header().Set("Strict-Transport-Security", "max-age=31536000")
	}
	ctx := httpx.WithScheme(httpx.WithClientAddr(r.Context(), client), scheme)

	r.Body = http.MaxBytesReader(w, r.Body, s.opts.MaxBodyBytes)

	ctx, cancel, ok := s.withDeadline(ctx, rw, r)
	defer cancel()
	if !ok {
		return
	}
	r = r.WithContext(ctx)

	if s.draining.Load() {
		rw.Header().Set("Connection", "close")
	}
	s.dispatch(rw, r)
}

var errBadRemoteAddr = errors.New("the request's RemoteAddr is not host:port")

// misdirected reports whether the peer check (decision 9) refuses this request.
func (s *Server) misdirected(peer netip.Addr, r *http.Request) bool {
	if len(s.opts.TrustedProxies) == 0 || s.opts.AllowPlaintext {
		return false
	}
	if p := r.URL.Path; p == "/healthz" || p == "/readyz" {
		return false
	}
	return !peer.IsLoopback() && !s.trusted.contains(peer)
}

// setStaticHeaders sets the headers every response carries (chain step 4). The CSP is
// added by whatever writes text/html.
func (s *Server) setStaticHeaders(w *respWriter, r *http.Request) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	h.Set("Cross-Origin-Opener-Policy", "same-origin")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	if len(r.URL.Path) >= len(apiPrefix) && r.URL.Path[:len(apiPrefix)] == apiPrefix {
		h.Set("Cache-Control", "no-store")
	}
	if s.opts.Database.ReadOnly() {
		h.Set("X-Read-Only", "true")
	}
}

// withDeadline gives the request its deadline on the ctx and on the connection's read
// side (decision 18, F28, F53). ok is false when it answered 500 instead.
func (s *Server) withDeadline(ctx context.Context, rw *respWriter, r *http.Request) (context.Context, context.CancelFunc, bool) {
	timeout := s.opts.RequestTimeout
	if s.testRequestTimeout > 0 {
		timeout = s.testRequestTimeout
	}
	deadline := time.Now().Add(timeout)
	ctx, cancel := context.WithDeadline(ctx, deadline)
	err := http.NewResponseController(rw).SetReadDeadline(deadline)
	switch {
	case err == nil:
	case errors.Is(err, http.ErrNotSupported):
		if s.warnedDeadline.CompareAndSwap(false, true) {
			s.log.DebugContext(ctx, "the connection does not support read deadlines; only the ctx deadline applies")
		}
	default:
		s.writeOuter(rw, r, errs.E("api.pipeline", errs.Internal, err))
		return ctx, cancel, false
	}
	return ctx, cancel, true
}

// writeOuter answers an error found before routing: there is no span or mapper yet, so
// the shell writes the problem and the one log record itself.
func (s *Server) writeOuter(rw *respWriter, r *http.Request, err error) {
	o := s.classify(err)
	if werr := writeProblem(rw, problemFor(o, err, ""), o.extra); werr != nil {
		rw.noteWriteError(werr)
	}
	level := slog.LevelDebug
	if o.status >= http.StatusInternalServerError {
		level = slog.LevelError
	}
	s.log.LogAttrs(r.Context(), level, "request failed",
		slog.String("error.kind", errs.KindOf(err).String()),
		slog.String("op", opOf(err)),
		slog.Int("status", o.status),
	)
}

// writeOuterProblem writes a problem with a shell code before routing.
func (s *Server) writeOuterProblem(rw *respWriter, status int, code string) {
	if werr := writeProblem(rw, httpx.Problem{Status: status, Code: code}, nil); werr != nil {
		rw.noteWriteError(werr)
	}
}

// recoverOuter is the outer recovery (chain step 1): a panic below becomes 500 internal if
// nothing was written, with an error log carrying the stack. http.ErrAbortHandler is
// re-panicked unchanged: it is net/http's quiet way to drop a connection.
func (s *Server) recoverOuter(rw *respWriter, r *http.Request, rec any) {
	if rec == http.ErrAbortHandler { //nolint:errorlint // net/http compares the panic value itself
		panic(rec)
	}
	err := errs.E("api.panic", errs.Internal, panicError{rec})
	if !rw.wroteHeader {
		s.writeOuterProblem(rw, http.StatusInternalServerError, errs.Internal.String())
	}
	s.log.LogAttrs(r.Context(), slog.LevelError, "request panicked",
		slog.String("error.kind", errs.Internal.String()),
		slog.String("op", "api.panic"),
		slog.Int("status", http.StatusInternalServerError),
		slog.String("error.message", capText(err.Error())),
		slog.String("stack", string(debug.Stack())),
	)
}
