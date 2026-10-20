package config

import (
	"errors"
	"log/slog"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// Key describes one configuration value. Keys() is the single source of truth: Load
// reads exactly these variables, Validate reports problems under Env, bms check and the
// operator documentation list them. Env is derived from Name: Prefix plus Name upper-cased
// with "." replaced by "_" (db.max_conns -> BMS_DB_MAX_CONNS).
type Key struct {
	Name      string // dotted: "db.url"
	Env       string // "BMS_DB_URL"
	Default   string // text form of the default; "" when Required or Generated
	Required  bool   // Load fails when neither Env nor Env+"_FILE" is set (F1)
	Secret    bool   // held in secret.Secret; Env+"_FILE" accepted; never echoed
	Generated bool   // also read from the secrets file; bms migrate creates it when absent
	Doc       string // one English line for operators, e.g. "PostgreSQL connection URL"
}

// Keys returns the catalogue in the order of the sections below, as a copy. Cannot fail.
// Consumers: Load (internally), cmd/bms (`bms check` and usage text), the docs test.
func Keys() []Key {
	out := make([]Key, len(catalogue))
	for i, e := range catalogue {
		out[i] = e.Key
	}
	return out
}

// Problem codes this package sets beyond the validate.Code* constants. They are stable
// strings the web shell and `bms check` may translate.
const (
	codeFormat     = "format"           // a text form that is not the key's grammar
	codeDuplicate  = "duplicate"        // a list item repeated
	codeTooMany    = "too_many"         // a list longer than its cap
	codeLength     = "length"           // a string shorter or longer than its bounds
	codeConflict   = "conflict"         // two sources, or a combination that is refused
	codeUnknown    = "unknown_variable" // a BMS_* variable no key reads
	codeUnreadable = "unreadable"       // a file that cannot be opened or read
	codeTooLarge   = "too_large"        // a file over MaxSecretFileBytes
	codeFilePerms  = "file_permissions" // a secrets file open to group or others
	codeTrustsAll  = "trusts_every_peer"
)

// Limits shared by Load's parsers and Validate. One spelling.
const (
	maxListItems    = 64
	maxDBURLBytes   = 4096
	minSessionBytes = 32
	maxSessionBytes = 1024
	minSealingBytes = 32
	maxSealingBytes = 1024
	maxInstanceName = 64
)

// parseFunc stores the text form of one key in c. It returns ("", "") on success, else the
// problem's code and a reason that never contains the text.
type parseFunc func(c *Config, text string) (code, reason string)

// entry is one catalogue row: the exported Key and how to read it.
type entry struct {
	Key
	parse parseFunc
}

// catalogue is the single source of truth behind Keys, Load and Default's consistency test.
var catalogue = []entry{
	newEntry(Key{Name: "instance.name", Default: "main",
		Doc: "name of this copy of the installation, recorded by audit and telemetry"},
		func(c *Config, t string) (string, string) { c.Instance.Name = t; return "", "" }),
	newEntry(Key{Name: "db.url", Required: true, Secret: true,
		Doc: "PostgreSQL connection URL (postgres:// or postgresql://)"},
		func(c *Config, t string) (string, string) { c.DB.URL = secret.New(t); return "", "" }),
	newEntry(Key{Name: "db.max_conns", Default: "10", Doc: "upper bound of the database connection pool"},
		intParser(func(c *Config) *int { return &c.DB.MaxConns })),
	newEntry(Key{Name: "db.connect_timeout", Default: "30s", Doc: "how long start-up may take to connect to the database"},
		durationParser(func(c *Config) *time.Duration { return &c.DB.ConnectTimeout })),
	newEntry(Key{Name: "db.query_timeout", Default: "10s", Doc: "default deadline of one database statement"},
		durationParser(func(c *Config) *time.Duration { return &c.DB.QueryTimeout })),
	newEntry(Key{Name: "http.addr", Default: "127.0.0.1:8080",
		Doc: "listen address, host:port or :port; a non-loopback host needs trusted proxies or allow_plaintext"},
		func(c *Config, t string) (string, string) { c.HTTP.Addr = t; return "", "" }),
	newEntry(Key{Name: "http.request_timeout", Default: "30s", Doc: "deadline of one HTTP request"},
		durationParser(func(c *Config) *time.Duration { return &c.HTTP.RequestTimeout })),
	newEntry(Key{Name: "http.shutdown_timeout", Default: "15s", Doc: "how long in-flight requests get to finish at stop"},
		durationParser(func(c *Config) *time.Duration { return &c.HTTP.ShutdownTimeout })),
	newEntry(Key{Name: "http.max_body_bytes", Default: "1048576", Doc: "largest accepted request body, in bytes"},
		int64Parser(func(c *Config) *int64 { return &c.HTTP.MaxBodyBytes })),
	newEntry(Key{Name: "http.trusted_proxies",
		Doc: "comma-separated networks (CIDR) or addresses whose X-Forwarded-* headers are believed"},
		parseProxies),
	newEntry(Key{Name: "http.allow_plaintext", Default: "false",
		Doc: "permit a non-loopback bind with no trusted proxy (development only)"},
		boolParser(func(c *Config) *bool { return &c.HTTP.AllowPlaintext })),
	newEntry(Key{Name: "session.key", Secret: true, Generated: true,
		Doc: "session signing key; generated by bms migrate into the secrets file when not set here"},
		func(c *Config, t string) (string, string) { c.Session.Key = secret.New(t); return "", "" }),
	newEntry(Key{Name: "settings.sealing_key", Secret: true, Generated: true,
		Doc: "key that seals settings' secret values; generated by bms migrate into the secrets file when not set here"},
		func(c *Config, t string) (string, string) { c.Settings.SealingKey = secret.New(t); return "", "" }),
	newEntry(Key{Name: "secrets.file", Default: "<data.dir>/" + SecretsFileName,
		Doc: "path of the generated-secrets file"},
		func(c *Config, t string) (string, string) { c.Secrets.File = t; return "", "" }),
	newEntry(Key{Name: "log.level", Default: "info", Doc: "minimum log level: debug, info, warn or error"},
		parseLogLevel),
	newEntry(Key{Name: "log.format", Default: "json", Doc: "log line format: json or text"},
		parseLogFormat),
	newEntry(Key{Name: "data.dir", Default: "data", Doc: "directory for local files, including the secrets file"},
		func(c *Config, t string) (string, string) { c.Data.Dir = t; return "", "" }),
	newEntry(Key{Name: "modules.disabled",
		Doc: "comma-separated names of compiled-in modules not to run"},
		parseModulesDisabled),
}

func newEntry(k Key, parse parseFunc) entry {
	k.Env = Prefix + strings.ToUpper(strings.ReplaceAll(k.Name, ".", "_"))
	return entry{Key: k, parse: parse}
}

// fileSuffix is appended to a secret key's Env for its file form.
const fileSuffix = "_FILE"

// order maps every variable the package reads to its catalogue position, so problems can be
// reported in catalogue order, one family (BMS_X and BMS_X_FILE) together.
var order = func() map[string]int {
	m := make(map[string]int, len(catalogue)*2)
	for i, e := range catalogue {
		m[e.Env] = i
		if e.Secret {
			m[e.Env+fileSuffix] = i
		}
	}
	return m
}()

// positionOf returns the catalogue position of field, or len(catalogue) for a field that
// is no known variable (an unknown BMS_* name), which therefore sorts last.
func positionOf(field string) int {
	if i, ok := order[field]; ok {
		return i
	}
	return len(catalogue)
}

func intParser(p func(*Config) *int) parseFunc {
	return func(c *Config, t string) (string, string) {
		n, err := strconv.ParseInt(t, 10, strconv.IntSize)
		if err != nil {
			return codeFormat, "must be a base-10 integer"
		}
		*p(c) = int(n)
		return "", ""
	}
}

func int64Parser(p func(*Config) *int64) parseFunc {
	return func(c *Config, t string) (string, string) {
		n, err := strconv.ParseInt(t, 10, 64)
		if err != nil {
			return codeFormat, "must be a base-10 integer"
		}
		*p(c) = n
		return "", ""
	}
}

func durationParser(p func(*Config) *time.Duration) parseFunc {
	return func(c *Config, t string) (string, string) {
		d, err := time.ParseDuration(t)
		if err != nil {
			return codeFormat, "must be a duration with a unit, such as 500ms, 30s or 5m"
		}
		*p(c) = d
		return "", ""
	}
}

func boolParser(p func(*Config) *bool) parseFunc {
	return func(c *Config, t string) (string, string) {
		switch t {
		case "true":
			*p(c) = true
		case "false":
			*p(c) = false
		default:
			return codeFormat, "must be true or false"
		}
		return "", ""
	}
}

var logLevels = map[string]slog.Level{
	"debug": slog.LevelDebug,
	"info":  slog.LevelInfo,
	"warn":  slog.LevelWarn,
	"error": slog.LevelError,
}

const (
	logLevelOptions  = "debug, info, warn, error"
	logFormatOptions = "json, text"
)

func parseLogLevel(c *Config, t string) (string, string) {
	l, ok := logLevels[t]
	if !ok {
		return validate.CodeOneOf, "must be one of: " + logLevelOptions
	}
	c.Log.Level = l
	return "", ""
}

func parseLogFormat(c *Config, t string) (string, string) {
	switch LogFormat(t) {
	case LogJSON, LogText:
		c.Log.Format = LogFormat(t)
		return "", ""
	}
	return validate.CodeOneOf, "must be one of: " + logFormatOptions
}

// parseModulesDisabled splits the list and leaves the shape (empty, duplicate, too many) to
// Validate, which sees the same items a hand-built Config would carry.
func parseModulesDisabled(c *Config, t string) (string, string) {
	parts := strings.Split(t, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	c.Modules.Disabled = parts
	return "", ""
}

// parseProxies reads prefixes and bare addresses. Whether the list is too long, or holds a
// prefix of zero bits, is Validate's to say.
func parseProxies(c *Config, t string) (string, string) {
	parts := strings.Split(t, ",")
	out := make([]netip.Prefix, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return codeFormat, "must not contain an empty item"
		}
		p, err := parsePrefixOrAddr(part)
		if err != nil {
			return codeFormat, "each item must be a CIDR prefix such as 10.0.0.0/8 or an address"
		}
		out = append(out, p)
	}
	c.HTTP.TrustedProxies = out
	return "", ""
}

var errZone = errors.New("address zone not allowed")

func parsePrefixOrAddr(s string) (netip.Prefix, error) {
	if strings.Contains(s, "/") {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return netip.Prefix{}, err //nolint:wrapcheck // dropped by the caller; it echoes the input
		}
		return p, nil
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, err //nolint:wrapcheck // dropped by the caller; it echoes the input
	}
	if a.Zone() != "" {
		return netip.Prefix{}, errZone
	}
	return netip.PrefixFrom(a, a.BitLen()), nil
}
