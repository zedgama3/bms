package config

import (
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"regexp"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

var instanceNameRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)

// problem is one failure, already shaped as a validate.FieldError named by variable.
type problem struct {
	field string
	err   error
}

// newProblem builds a problem whose reason must not contain any value the operator typed.
func newProblem(field, code, format string, args ...any) problem {
	return problem{field: field, err: errs.E("", errs.Invalid, &validate.FieldError{
		Field: field, Code: code, Reason: fmt.Sprintf(format, args...),
	})}
}

// collector gathers at most one problem per variable, in the order they are added.
type collector struct {
	probs []problem
	seen  map[string]bool
}

func (c *collector) add(field string, err error) {
	if err == nil || c.seen[field] {
		return
	}
	if c.seen == nil {
		c.seen = make(map[string]bool)
	}
	c.seen[field] = true
	c.probs = append(c.probs, problem{field: field, err: err})
}

func (c *collector) addf(field, code, format string, args ...any) {
	if c.seen[field] {
		return
	}
	p := newProblem(field, code, format, args...)
	c.add(field, p.err)
}

// Validate checks every section's ranges and shapes (F14–F21, F28) and fails with
// errs.Invalid joining one validate.FieldError per problem, Field being the variable's Env
// name. An unset Session.Key passes (decision 7). It does no I/O: it does not open
// Data.Dir or Secrets.File, resolve HTTP.Addr or connect to DB.URL.
// Consumers: Load; app's tests building a Config by hand.
func (c Config) Validate() error {
	acc := validate.Errors{Op: "config.Validate"}
	for _, p := range c.problems() {
		acc.Add(p.err)
	}
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// problems returns every rule the Config breaks, at most one per variable, in catalogue order.
func (c Config) problems() []problem {
	var col collector
	c.validateInstance(&col)
	c.validateDB(&col)
	c.validateHTTP(&col)
	c.validateSession(&col)
	c.validateSecrets(&col)
	c.validateLog(&col)
	c.validateData(&col)
	c.validateModules(&col)
	return col.probs
}

func envOf(name string) string {
	for _, e := range catalogue {
		if e.Name == name {
			return e.Env
		}
	}
	// A name missing from the catalogue is a bug in this file; the test for the catalogue
	// covers every name used below.
	return Prefix + strings.ToUpper(strings.ReplaceAll(name, ".", "_"))
}

func (c Config) validateInstance(col *collector) {
	env := envOf("instance.name")
	if !instanceNameRE.MatchString(c.Instance.Name) {
		col.addf(env, codeFormat,
			"must be 1 to %d lower-case letters, digits or hyphens, starting and ending with a letter or digit", maxInstanceName)
	}
}

func (c Config) validateDB(col *collector) {
	env := envOf("db.url")
	switch u := c.DB.URL.Value(); {
	case !c.DB.URL.IsSet():
		col.addf(env, validate.CodeRequired, "required")
	case strings.ContainsRune(u, 0):
		col.addf(env, codeFormat, "must not contain a NUL byte")
	case len(u) > maxDBURLBytes:
		col.addf(env, codeLength, "must be at most %d bytes", maxDBURLBytes)
	default:
		col.add(env, validate.URL(env, u, "postgres", "postgresql"))
	}
	col.add(envOf("db.max_conns"), validate.Range(envOf("db.max_conns"), c.DB.MaxConns, 1, 1000))
	col.add(envOf("db.connect_timeout"),
		validate.Range(envOf("db.connect_timeout"), c.DB.ConnectTimeout, time.Second, 10*time.Minute))
	col.add(envOf("db.query_timeout"),
		validate.Range(envOf("db.query_timeout"), c.DB.QueryTimeout, 100*time.Millisecond, 10*time.Minute))
}

func (c Config) validateHTTP(col *collector) {
	h := c.HTTP
	addrEnv := envOf("http.addr")
	addrErr := validate.ListenAddr(addrEnv, h.Addr)
	col.add(addrEnv, addrErr)
	if addrErr == nil && len(h.TrustedProxies) == 0 && !h.AllowPlaintext && !isLoopbackBind(h.Addr) {
		col.addf(addrEnv, codeConflict,
			"binds a non-loopback address with no trusted proxy: put a TLS front end in front and list it in "+
				"%s, bind 127.0.0.1, or set %s=true for development",
			envOf("http.trusted_proxies"), envOf("http.allow_plaintext"))
	}
	col.add(envOf("http.request_timeout"),
		validate.Range(envOf("http.request_timeout"), h.RequestTimeout, time.Second, 10*time.Minute))
	col.add(envOf("http.shutdown_timeout"),
		validate.Range(envOf("http.shutdown_timeout"), h.ShutdownTimeout, time.Second, 5*time.Minute))
	col.add(envOf("http.max_body_bytes"),
		validate.Range(envOf("http.max_body_bytes"), h.MaxBodyBytes, 1024, 1<<30))

	proxEnv := envOf("http.trusted_proxies")
	if len(h.TrustedProxies) > maxListItems {
		col.addf(proxEnv, codeTooMany, "must list at most %d prefixes or addresses", maxListItems)
	}
	for _, p := range h.TrustedProxies {
		switch {
		case !p.IsValid():
			col.addf(proxEnv, codeFormat, "each item must be a CIDR prefix such as 10.0.0.0/8 or an address")
		case p.Bits() == 0:
			col.addf(proxEnv, codeTrustsAll,
				"trusts every peer: list the front end's addresses or networks")
		}
	}
}

// isLoopbackBind reports whether the host of a listen address is loopback by literal text:
// an IP in 127.0.0.0/8 or ::1, or the name localhost. The empty host (all interfaces) is
// not loopback. No lookup is made.
func isLoopbackBind(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil || host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	return ip.Unmap().IsLoopback()
}

func (c Config) validateSession(col *collector) {
	if !c.Session.Key.IsSet() {
		return
	}
	env := envOf("session.key")
	k := c.Session.Key.Value()
	switch {
	case strings.ContainsRune(k, 0):
		col.addf(env, codeFormat, "must not contain a NUL byte")
	case len(k) < minSessionBytes:
		col.addf(env, codeLength, "must be at least %d bytes", minSessionBytes)
	case len(k) > maxSessionBytes:
		col.addf(env, codeLength, "must be at most %d bytes", maxSessionBytes)
	}
}

func (c Config) validateSecrets(col *collector) {
	validatePath(col, envOf("secrets.file"), c.Secrets.File)
}

func (c Config) validateData(col *collector) {
	validatePath(col, envOf("data.dir"), c.Data.Dir)
}

func validatePath(col *collector, env, path string) {
	switch {
	case strings.TrimSpace(path) == "":
		col.add(env, validate.NonEmpty(env, path))
	case strings.ContainsRune(path, 0):
		col.addf(env, codeFormat, "must not contain a NUL byte")
	}
}

func (c Config) validateLog(col *collector) {
	levelEnv, formatEnv := envOf("log.level"), envOf("log.format")
	switch c.Log.Level {
	case slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError:
	default:
		col.addf(levelEnv, validate.CodeOneOf, "must be one of: %s", logLevelOptions)
	}
	switch c.Log.Format {
	case LogJSON, LogText:
	default:
		col.addf(formatEnv, validate.CodeOneOf, "must be one of: %s", logFormatOptions)
	}
}

func (c Config) validateModules(col *collector) {
	env := envOf("modules.disabled")
	items := c.Modules.Disabled
	if len(items) > maxListItems {
		col.addf(env, codeTooMany, "must list at most %d modules", maxListItems)
	}
	seen := make(map[string]bool, len(items))
	for _, it := range items {
		name := strings.TrimSpace(it)
		switch {
		case name == "":
			col.addf(env, validate.CodeRequired, "must not contain an empty item")
		case seen[name]:
			col.addf(env, codeDuplicate, "must not name a module twice")
		}
		seen[name] = true
	}
}
