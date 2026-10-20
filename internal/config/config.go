// Package config loads the process configuration from the environment and validates it,
// reporting every problem at once. It depends on the standard library and internal/lib
// only. cmd/bms and app are its only importers; the core and the modules receive typed
// options from app.
package config

import (
	"log/slog"
	"net/netip"
	"path/filepath"
	"time"

	"github.com/zedgama3/bms/internal/lib/secret"
)

// Prefix is the start of every environment variable this package reads. Any variable
// whose name starts with it (ignoring case) must be a known key, or Load fails (F11);
// variables starting with TestPrefix are left alone.
const (
	Prefix     = "BMS_"
	TestPrefix = "BMS_TEST_" // read by integration tests, never by Load
)

// MaxSecretFileBytes caps a *_FILE read and the secrets file. A larger file fails Load (F9).
const MaxSecretFileBytes = 64 << 10

// The generated-secrets file (decision 7). bms migrate creates it under Data.Dir with
// crypto/rand material, mode 0600, and never overwrites it; Load reads it. It is a JSON
// object; unknown fields are preserved by migrate and ignored by Load, so a rollback to an
// older binary still starts. Today it holds two fields.
const (
	SecretsFileName                = "secrets.json"         // default: filepath.Join(Data.Dir, SecretsFileName)
	SecretsFieldSessionKey         = "session_key"          // string; the material Session.Key holds
	SecretsFieldSettingsSealingKey = "settings_sealing_key" // string; the material Settings.SealingKey holds (D39)
)

// Config is the whole process configuration. It is a value: immutable once Load returns,
// safe to copy, share, marshal and log (every credential is a secret.Secret). The zero
// value is not usable; start from Default() or Load.
type Config struct {
	Instance Instance
	DB       DB
	HTTP     HTTP
	Session  Session
	Settings Settings
	Secrets  Secrets
	Log      Log
	Data     Data
	Modules  Modules
}

// Instance identifies this process among the copies of one installation (D27: on-site and
// off-site copies share a database but are different processes).
type Instance struct {
	// Name is recorded by audit as "which copy" (09 §8) and by telemetry as a resource
	// attribute. Grammar: "[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?" (1..64). Default "main".
	Name string
}

// DB is what db needs to open the pool. app copies it into db's own options type.
type DB struct {
	// URL is the PostgreSQL connection URL: postgres:// or postgresql:// with a host. It is
	// a secret because it carries the password. Required. BMS_DB_URL or BMS_DB_URL_FILE.
	URL secret.Secret
	// MaxConns is the pool's upper bound. Default 10; 1..1000.
	MaxConns int
	// ConnectTimeout bounds the initial connect at start-up, retries included. Default 30s;
	// 1s..10m.
	ConnectTimeout time.Duration
	// QueryTimeout is the deadline a caller derives for one statement when it has no
	// tighter one. Default 10s; 100ms..10m.
	QueryTimeout time.Duration
}

// HTTP is what the api shell needs to listen. TLS is offloaded to a front end; decision
// 16 states the conditions and the rule Validate enforces on Addr, TrustedProxies and
// AllowPlaintext (F28).
type HTTP struct {
	// Addr is the listen address, "host:port" or ":port". Default "127.0.0.1:8080"
	// (loopback: safe without a front end). Binding anything else needs TrustedProxies or
	// AllowPlaintext (F28).
	Addr string
	// RequestTimeout is the deadline of one request. Default 30s; 1s..10m.
	RequestTimeout time.Duration
	// ShutdownTimeout is how long in-flight requests get to finish at stop (09 §1).
	// Default 15s; 1s..5m.
	ShutdownTimeout time.Duration
	// MaxBodyBytes caps a request body (JSON API; file uploads are files' later concern).
	// Default 1048576 (1 MiB); 1024..1073741824.
	MaxBodyBytes int64
	// TrustedProxies lists the networks whose X-Forwarded-For and X-Forwarded-Proto are
	// believed (D15: an optional front end). Empty, the default, trusts none. At most 64
	// prefixes; a bare address means that one address. Non-empty, it is also what makes a
	// non-loopback Addr acceptable: the plaintext hop ends at these proxies. A prefix of
	// zero bits (0.0.0.0/0, ::/0) is refused (F30): it would trust every peer's headers
	// and switch off the api's peer check. A front end on the same host is listed as
	// 127.0.0.1/32 (or ::1/128); otherwise all its clients share the loopback address.
	TrustedProxies []netip.Prefix
	// AllowPlaintext permits a non-loopback Addr with no TrustedProxies: plain HTTP
	// reachable from the network, for development only. Default false. The api shell logs
	// a warning at start when it is true.
	AllowPlaintext bool
}

// Session is what identity needs to sign session tokens without a database write (09 §2).
type Session struct {
	// Key is the signing key material; identity derives purpose-specific keys from it.
	// Sources, in this order: BMS_SESSION_KEY, BMS_SESSION_KEY_FILE, the secrets file's
	// "session_key" (decision 7); more than one source set is an error (F7, F27). Unset
	// when no source has it (the secrets file is missing or lacks the field): Validate
	// accepts that, and the command that needs the key refuses to run (F22b in app). When
	// set: 32..1024 bytes, no NUL. The same value on every copy of one installation.
	Key secret.Secret
}

// Settings is what the settings service needs to seal secret values (D39).
type Settings struct {
	// SealingKey is the material settings derives its AES-256-GCM key from (HKDF-SHA256)
	// to seal Secret values such as the SMTP password. Sources, in this order:
	// BMS_SETTINGS_SEALING_KEY, BMS_SETTINGS_SEALING_KEY_FILE, the secrets file's
	// "settings_sealing_key"; more than one source set is an error (F7, F27 shape).
	// Unset when no source has it: Validate accepts that and no command refuses to run
	// (decision 17). When set: 32..1024 bytes, no NUL. The same value on every copy.
	SealingKey secret.Secret
}

// Secrets says where the generated-secrets file is.
type Secrets struct {
	// File is the path of the secrets file. Default filepath.Join(Data.Dir, SecretsFileName),
	// resolved at Load after data.dir. BMS_SECRETS_FILE. The file may be absent (then
	// Session.Key is unset); when present it must be a regular file readable by its owner
	// only (mode 0600 on Unix; F24).
	File string
}

// LogFormat is how telemetry writes log lines to stdout.
type LogFormat string

// The two log formats.
const (
	LogJSON LogFormat = "json" // default; D20
	LogText LogFormat = "text" // for a developer's terminal
)

// Log is what telemetry needs to build the logger.
type Log struct {
	// Level is the minimum level written: "debug", "info", "warn" or "error". Default info.
	Level slog.Level
	// Format is "json" or "text". Default json.
	Format LogFormat
}

// Data is where this process keeps local files: the secrets file (decision 7), the audit
// spool on a read-only copy (09 §8), later files' local-disk storage.
type Data struct {
	// Dir is a directory path, absolute or relative to the working directory. Default
	// "data". bms migrate creates it (mode 0700); other consumers check it at Start. Load
	// only uses it to resolve Secrets.File.
	Dir string
}

// Modules is the opt-out list of module-contract decision 7.
type Modules struct {
	// Disabled names compiled-in modules not to run. Default empty: every compiled-in
	// module runs. Items are unique and non-empty; app checks each is registered.
	Disabled []string
}

// Default returns a Config with every default applied, every required value unset and
// Session.Key unset. Default().Validate() therefore fails on the required key (db.url)
// only; tests set it. Cannot fail. Consumers: Load (its starting point), tests in app and
// config.
func Default() Config {
	const dataDir = "data"
	return Config{
		Instance: Instance{Name: "main"},
		DB: DB{
			MaxConns:       10,
			ConnectTimeout: 30 * time.Second,
			QueryTimeout:   10 * time.Second,
		},
		HTTP: HTTP{
			Addr:            "127.0.0.1:8080",
			RequestTimeout:  30 * time.Second,
			ShutdownTimeout: 15 * time.Second,
			MaxBodyBytes:    1 << 20,
		},
		Secrets: Secrets{File: filepath.Join(dataDir, SecretsFileName)},
		Log:     Log{Level: slog.LevelInfo, Format: LogJSON},
		Data:    Data{Dir: dataDir},
	}
}
