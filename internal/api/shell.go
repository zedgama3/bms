package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/telemetry/intake"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
)

const (
	readinessTTL     = time.Second
	readinessTimeout = 2 * time.Second
	clientErrorsCap  = 64 << 10
)

// shellRoute is one of the shell's own routes, mounted at an absolute path (decision 20).
type shellRoute struct {
	method, path string
	access       module.Access
	handler      http.Handler
}

func (s *Server) shellRoutes() []shellRoute {
	return []shellRoute{
		{http.MethodGet, "/api/modules", module.AccessPublic, http.HandlerFunc(s.handleModules)},
		{http.MethodGet, "/api/admin/modules", module.AccessSignedIn, http.HandlerFunc(s.handleAdminModules)},
		{http.MethodPost, "/api/client-errors", module.AccessPublic, http.HandlerFunc(s.handleClientErrors)},
		{http.MethodGet, "/api/openapi.json", module.AccessPublic, http.HandlerFunc(s.handleOpenAPI)},
	}
}

// mountRoot registers /healthz and /readyz (no span, no sample) and the UI at "/".
func (s *Server) mountRoot() {
	s.mux.Handle("/healthz", http.HandlerFunc(s.handleHealthz))
	s.mux.Handle("/readyz", http.HandlerFunc(s.handleReadyz))
	s.mux.Handle("/", http.HandlerFunc(s.serveUI))
}

// healthGate answers 405 for a method that is not GET or HEAD and 503 while draining. It
// reports true when the caller should go on.
func (s *Server) healthGate(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeNotAllowed(w, "GET, HEAD")
		return false
	}
	if s.draining.Load() {
		s.writeStatusBody(w, r, http.StatusServiceUnavailable, map[string]string{"status": "draining"})
		return false
	}
	return true
}

func writeNotAllowed(w http.ResponseWriter, allow string) {
	if err := writeProblem(w, httpx.Problem{Status: http.StatusMethodNotAllowed, Code: httpx.CodeMethodNotAllowed},
		http.Header{"Allow": {allow}}); err != nil {
		return // the client is gone; the wrapper kept the error
	}
}

// writeStatusBody writes a small JSON body with status. A failed write is the client
// leaving; the wrapper remembers it.
func (s *Server) writeStatusBody(w http.ResponseWriter, r *http.Request, status int, v any) {
	if err := httpx.WriteJSON(w, status, v); err != nil {
		s.log.DebugContext(r.Context(), "could not write the response", slog.String("error.kind", errs.KindOf(err).String()))
	}
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if !s.healthGate(w, r) {
		return
	}
	s.writeStatusBody(w, r, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if !s.healthGate(w, r) {
		return
	}
	if err := s.ready.check(r.Context(), s); err != nil {
		s.writeStatusBody(w, r, http.StatusServiceUnavailable, map[string]string{"database": "unavailable"})
		return
	}
	mode := "read-write"
	if s.opts.Database.ReadOnly() {
		mode = "read-only"
	}
	s.writeStatusBody(w, r, http.StatusOK, map[string]string{"database": "ok", "mode": mode})
}

// readiness shares one Ping among concurrent probes and reuses its outcome for a second
// (decision 22).
type readiness struct {
	mu     sync.Mutex
	valid  bool
	at     time.Time
	err    error
	flight *pingFlight
}

type pingFlight struct {
	done chan struct{}
	err  error
}

// check returns the outcome of a Ping at most one second old, running one if needed.
func (rd *readiness) check(ctx context.Context, s *Server) error {
	rd.mu.Lock()
	if rd.valid && s.opts.Clock.Since(rd.at) < readinessTTL {
		err := rd.err
		rd.mu.Unlock()
		return err
	}
	if f := rd.flight; f != nil {
		rd.mu.Unlock()
		select {
		case <-f.done:
			return f.err
		case <-ctx.Done():
			return errs.E("api.readyz", errs.KindOf(ctx.Err()), ctx.Err())
		}
	}
	f := &pingFlight{done: make(chan struct{}), err: errs.E("api.readyz", errs.Internal, errors.New("the database check did not complete"))}
	rd.flight = f
	rd.mu.Unlock()
	// However Ping ends, even by panicking, the flight is cleared and its waiters released.
	defer func() {
		rd.mu.Lock()
		rd.valid, rd.at, rd.err, rd.flight = true, s.opts.Clock.Now(), f.err, nil
		rd.mu.Unlock()
		close(f.done)
	}()

	// The ping serves every waiting probe, so it does not end with the leader's request.
	pctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), readinessTimeout)
	defer cancel()
	f.err = s.opts.Database.Ping(pctx)
	if f.err != nil {
		s.log.WarnContext(ctx, "readiness check failed", slog.String("error.kind", errs.KindOf(f.err).String()),
			slog.String("op", opOf(f.err)))
	}
	return f.err
}

func (s *Server) handleModules(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Cache-Control", "no-cache")
	h.Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(s.modules); err != nil {
		s.log.DebugContext(r.Context(), "could not write the response", slog.String("error.kind", errs.KindOf(err).String()))
	}
}

func (s *Server) handleAdminModules(w http.ResponseWriter, r *http.Request) {
	const op = "api.handleAdminModules"
	ctx := r.Context()
	if s.opts.Permissions == nil {
		httpx.Fail(w, r, errs.E(op, errs.Permission, errors.New("no permission checker is configured; nobody holds "+string(PermissionModulesRead))))
		return
	}
	if err := s.opts.Permissions.Allow(ctx, PermissionModulesRead); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	status, err := s.opts.Schema(ctx)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	body := adminModulesResponse{Version: s.opts.Version, Graph: s.adminGraph, Schema: make([]adminSchema, 0, len(status))}
	for _, st := range status {
		body.Schema = append(body.Schema, adminSchema{Set: st.Set, Built: st.Built, Applied: st.Applied, Current: st.Err == nil})
	}
	if err := httpx.WriteJSON(w, http.StatusOK, body); err != nil {
		s.log.DebugContext(ctx, "could not write the response", slog.String("error.kind", errs.KindOf(err).String()))
	}
}

func (s *Server) handleClientErrors(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, clientErrorsCap)
	var batch intake.Batch
	if err := httpx.ReadJSON(r, &batch); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	from, ok := httpx.ClientAddrFrom(r.Context())
	if !ok {
		httpx.Fail(w, r, errs.E("api.handleClientErrors", errs.Internal, errors.New("no client address in the request context")))
		return
	}
	sub := intake.Submission{From: from, Person: actorFrom(r.Context()), UserAgent: r.UserAgent(), Batch: batch}
	if err := s.opts.Intake.Accept(r.Context(), sub); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func etagOf(b []byte) string {
	sum := sha256.Sum256(b)
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("ETag", s.etag)
	h.Set("Cache-Control", "no-cache")
	if etagMatches(r.Header.Values("If-None-Match"), s.etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(s.openapi); err != nil {
		s.log.DebugContext(r.Context(), "could not write the response", slog.String("error.kind", errs.KindOf(err).String()))
	}
}

// etagMatches reports whether an If-None-Match header lists etag (weak or strong) or "*".
func etagMatches(values []string, etag string) bool {
	for _, v := range values {
		for part := range strings.SplitSeq(v, ",") {
			part = strings.TrimPrefix(strings.TrimSpace(part), "W/")
			if part == "*" || part == etag {
				return true
			}
		}
	}
	return false
}
