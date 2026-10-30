package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// Server constants (api.md "Settings"): not configurable, each with a reason there.
const (
	readHeaderTimeout = 10 * time.Second
	idleTimeout       = 120 * time.Second
	writeTimeoutExtra = 15 * time.Second
	maxHeaderBytes    = 64 << 10
)

// Server is the process's HTTP server. Safe for concurrent use at all times: before
// Listen, while serving, and after Shutdown returns (Handler and OpenAPI keep answering;
// Listen fails; Serve and a second Shutdown return nil).
type Server struct {
	opts Options
	log  *slog.Logger
	hist telemetry.Histogram
	// apiErrors counts error responses by method, route and code (D54).
	apiErrors telemetry.Counter
	mux       *http.ServeMux
	known     map[string]bool        // every pattern registered through mount, and "/api/"
	owners    map[string]string      // pattern -> the set that owns it, for conflict messages
	limits    map[string]routeLimits // pattern -> the route's raised body cap and deadline
	trusted   trustedSet
	cop       *http.CrossOriginProtection
	csp       string
	ui        fs.FS
	noIndex   bool

	openapi    []byte
	etag       string
	adminGraph []byte
	modules    []byte // the body of GET /api/modules

	unmatched *route // the route that answers 404 for a path the mux would redirect
	ready     readiness
	misd      *misdirectLog

	warnedForwarded atomic.Bool // the one-per-process warning of decision 9 was written
	warnedDeadline  atomic.Bool // likewise for a connection without read deadlines

	// testRequestTimeout, when positive, replaces Options.RequestTimeout for the ctx and
	// connection read deadline. Internal tests set it after New.
	testRequestTimeout time.Duration

	draining atomic.Bool
	closed   atomic.Bool

	mu         sync.Mutex
	listener   net.Listener
	srv        *http.Server
	baseCancel context.CancelFunc
}

// New validates opts, builds the pipeline and the router, mounts every route set, checks
// every route's span name and route token against telemetry's grammar, marshals the static
// half of the admin modules response, assembles and checks the OpenAPI document, creates
// the request-duration histogram and returns a Server that is not yet listening. It does
// no network I/O. Fails with errs.Invalid (options, a route or fragment that is malformed
// or disagrees), errs.Conflict (two patterns or two schema names clash) or the meter's
// kind.
func New(opts Options) (*Server, error) {
	const op = "api.New"
	if err := opts.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	s := &Server{
		opts:    opts,
		log:     opts.Logger,
		mux:     http.NewServeMux(),
		known:   map[string]bool{},
		owners:  map[string]string{},
		limits:  map[string]routeLimits{},
		trusted: trustedSet(opts.TrustedProxies),
		cop:     http.NewCrossOriginProtection(),
		csp:     opts.ContentSecurityPolicy,
		ui:      opts.UI,
		noIndex: opts.NoIndex,
		misd:    newMisdirectLog(opts.Logger),
	}
	if s.csp == "" {
		s.csp = DefaultCSP
	}
	if opts.AllowPlaintext {
		s.log.Warn("http.allow_plaintext is on: any peer may reach this server over plain HTTP; use it for development only")
	}
	if loopbackBind(opts.Addr) && len(opts.TrustedProxies) > 0 && !anyLoopback(opts.TrustedProxies) {
		s.log.Warn("the server binds a loopback address but http.trusted_proxies lists no loopback address, so no listed proxy can connect; list 127.0.0.1/32 for a front end on this host")
	}
	hist, err := opts.Meter.Histogram("http.server.request.duration",
		telemetry.WithUnit("s"), telemetry.WithDescription("Duration of HTTP server requests"),
		telemetry.WithBuckets(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10))
	if err != nil {
		return nil, fmt.Errorf("%s: request duration histogram: %w", op, err)
	}
	s.hist = hist
	if s.apiErrors, err = opts.Meter.Counter("bms.api.errors",
		telemetry.WithUnit("{response}"), telemetry.WithDescription("Error responses by route and problem code")); err != nil {
		return nil, fmt.Errorf("%s: error counter: %w", op, err)
	}

	sets, err := s.mountAll()
	if err != nil {
		return nil, err
	}
	if err := s.mountFallbacks(); err != nil {
		return nil, err
	}
	if s.adminGraph, err = marshalAdminGraph(opts.Graph); err != nil {
		return nil, err
	}
	if s.modules, err = marshalModules(opts.Graph); err != nil {
		return nil, err
	}
	if s.openapi, err = assemble(opts.Version, sets); err != nil {
		return nil, err
	}
	s.etag = etagOf(s.openapi)
	s.mountRoot()

	baseCtx, cancel := context.WithCancel(context.Background()) //nolint:gocritic // the one base context of the server (decision 17); New takes no ctx
	s.baseCancel = cancel
	s.srv = &http.Server{
		Handler:           http.HandlerFunc(s.serveHTTP),
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
		WriteTimeout:      opts.RequestTimeout + writeTimeoutExtra,
		MaxHeaderBytes:    maxHeaderBytes,
		BaseContext:       func(net.Listener) context.Context { return baseCtx },
		ErrorLog:          slog.NewLogLogger(s.log.Handler(), slog.LevelWarn),
	}
	return s, nil
}

// loopbackBind reports whether addr binds only a loopback interface.
func loopbackBind(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil || host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	a, err := netip.ParseAddr(host)
	return err == nil && a.Unmap().IsLoopback()
}

func anyLoopback(ps []netip.Prefix) bool {
	for _, p := range ps {
		if p.Addr().Unmap().IsLoopback() {
			return true
		}
	}
	return false
}

// Handler returns the complete handler (pipeline, router, shell routes, UI), for httptest
// in tests and for app if it ever wraps it. Cannot fail.
func (s *Server) Handler() http.Handler { return http.HandlerFunc(s.serveHTTP) }

// OpenAPI returns the assembled document as JSON bytes, the same bytes
// GET /api/openapi.json serves. Cannot fail: New built it.
func (s *Server) OpenAPI() []byte { return append([]byte(nil), s.openapi...) }

// Listen binds Options.Addr and returns the bound address (so tests may bind ":0"). Fails
// with errs.Unavailable when the address is in use or cannot be bound, errs.Invalid when
// the address is malformed or does not resolve, errs.Conflict when already listening or
// when Shutdown has already been called.
func (s *Server) Listen(ctx context.Context) (net.Addr, error) {
	const op = "api.Server.Listen"
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.closed.Load():
		return nil, errs.E(op, errs.Conflict, errors.New("the server was shut down"))
	case s.listener != nil:
		return nil, errs.E(op, errs.Conflict, errors.New("the server is already listening"))
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", s.opts.Addr)
	if err != nil {
		return nil, errs.E(op, listenKind(err), fmt.Errorf("listen on %s: %w", s.opts.Addr, err))
	}
	s.listener = ln
	return ln.Addr(), nil
}

// listenKind: an address that cannot be parsed or resolved is Invalid; anything else (in
// use, no permission, not a local address) is Unavailable.
func listenKind(err error) errs.Kind {
	var addrErr *net.AddrError
	var dnsErr *net.DNSError
	var parseErr *net.ParseError
	switch {
	case errors.As(err, &addrErr), errors.As(err, &dnsErr), errors.As(err, &parseErr):
		return errs.Invalid
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return errs.KindOf(err)
	default:
		return errs.Unavailable
	}
}

// Serve accepts connections on the listener until Shutdown is called, then returns nil.
// Called after Shutdown it returns nil at once. It fails with errs.Internal if Listen was
// never called and with errs.Unavailable wrapping the server's error if accepting fails
// for any reason other than Shutdown.
func (s *Server) Serve() error {
	const op = "api.Server.Serve"
	s.mu.Lock()
	ln := s.listener
	s.mu.Unlock()
	if ln == nil {
		if s.closed.Load() {
			return nil
		}
		return errs.E(op, errs.Internal, errors.New("Serve was called before Listen"))
	}
	err := s.srv.Serve(ln)
	if err == nil || errors.Is(err, http.ErrServerClosed) || s.closed.Load() {
		return nil
	}
	return errs.E(op, errs.Unavailable, fmt.Errorf("accept connections: %w", err))
}

// Shutdown drains: /healthz and /readyz answer 503 at once, the listener closes, in-flight
// requests get Connection: close and run to completion within ctx; when ctx ends first,
// the base context is canceled so stuck handlers fail with Canceled (mapped to 503
// shutting_down) and Shutdown returns errs.Timeout or errs.Canceled. Handlers that ignore
// their ctx may still be running when Shutdown returns with that error. Idempotent: a
// second call returns nil. Before Listen it marks the server closed and returns nil.
func (s *Server) Shutdown(ctx context.Context) error {
	const op = "api.Server.Shutdown"
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	s.draining.Store(true)
	s.mu.Lock()
	listening := s.listener != nil
	s.mu.Unlock()
	if !listening {
		s.baseCancel()
		return nil
	}

	err := s.srv.Shutdown(ctx)
	s.baseCancel()       // stuck handlers (and the idle ones) see Canceled from here on
	s.closeListener(ctx) // a Listen that never reached Serve is unknown to http.Server
	if err != nil {
		kind := errs.KindOf(err)
		if kind != errs.Timeout && kind != errs.Canceled {
			kind = errs.Timeout
		}
		return errs.E(op, kind, fmt.Errorf("drain in-flight requests: %w", err))
	}
	return nil
}

// closeListener closes the bound socket. http.Server.Shutdown has usually closed it
// already; a listener that is closed is not an error.
func (s *Server) closeListener(ctx context.Context) {
	s.mu.Lock()
	ln := s.listener
	s.mu.Unlock()
	if ln == nil {
		return
	}
	if err := ln.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		s.log.WarnContext(ctx, "could not close the listener", slog.String("error.kind", errs.KindOf(err).String()))
	}
}

// ---- Admin and public module lists ----

type adminModulesResponse struct {
	Version string          `json:"version"`
	Graph   json.RawMessage `json:"graph"`
	Schema  []adminSchema   `json:"schema"`
}

type adminGraph struct {
	Modules  []adminModule  `json:"modules"`
	Edges    []adminEdge    `json:"edges"`
	Warnings []adminWarning `json:"warnings"`
}

type adminModule struct {
	Name          string   `json:"name"`
	Enabled       bool     `json:"enabled"`
	HasSchema     bool     `json:"has_schema"`
	Permissions   []string `json:"permissions"`
	Settings      []string `json:"settings"`
	Publishes     []string `json:"publishes"`
	Subscribes    []string `json:"subscribes"`
	Provides      []string `json:"provides"`
	Requires      []string `json:"requires"`
	Uses          []string `json:"uses"`
	Defines       []string `json:"defines"`
	ContributesTo []string `json:"contributes_to"`
}

type adminEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
	Via  string `json:"via"`
}

type adminWarning struct {
	Module  string `json:"module"`
	Message string `json:"message"`
}

type adminSchema struct {
	Set     string `json:"set"`
	Built   int    `json:"built"`
	Applied int    `json:"applied"`
	Current bool   `json:"current"`
}

func names[T ~string](in []T) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = string(v)
	}
	return out
}

// marshalAdminGraph builds the api-owned view of g and marshals it once (F52).
func marshalAdminGraph(g module.Graph) ([]byte, error) {
	out := adminGraph{
		Modules:  make([]adminModule, 0, len(g.Modules)),
		Edges:    make([]adminEdge, 0, len(g.Edges)),
		Warnings: make([]adminWarning, 0, len(g.Warnings)),
	}
	for _, n := range g.Modules {
		m := adminModule{
			Name: n.Name, Enabled: n.Enabled, HasSchema: n.HasSchema,
			Permissions:   make([]string, 0, len(n.Permissions)),
			Settings:      make([]string, 0, len(n.Settings)),
			Publishes:     names(n.Publishes),
			Subscribes:    names(n.Subscribes),
			Provides:      names(n.Provides),
			Requires:      names(n.Requires),
			Uses:          names(n.Uses),
			Defines:       names(n.Defines),
			ContributesTo: names(n.ContributesTo),
		}
		for _, p := range n.Permissions {
			m.Permissions = append(m.Permissions, string(p.Name))
		}
		for _, d := range n.Settings {
			m.Settings = append(m.Settings, d.Key)
		}
		out.Modules = append(out.Modules, m)
	}
	for _, e := range g.Edges {
		out.Edges = append(out.Edges, adminEdge{From: e.From, To: e.To, Kind: e.Kind.String(), Via: e.Via})
	}
	for _, w := range g.Warnings {
		out.Warnings = append(out.Warnings, adminWarning{Module: w.Module, Message: w.Message})
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, errs.E("api.New", errs.Internal, fmt.Errorf("marshal the module graph: %w", err))
	}
	return b, nil
}

// marshalModules builds the body of GET /api/modules: names and enablement only.
func marshalModules(g module.Graph) ([]byte, error) {
	type entry struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	body := struct {
		Modules []entry `json:"modules"`
	}{Modules: make([]entry, 0, len(g.Modules))}
	for _, n := range g.Modules {
		body.Modules = append(body.Modules, entry{Name: n.Name, Enabled: n.Enabled})
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, errs.E("api.New", errs.Internal, fmt.Errorf("marshal the module list: %w", err))
	}
	return b, nil
}
