package module

import (
	"fmt"
	"io/fs"
	"regexp"
	"strings"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// nameRE is the module name grammar: letters and digits only, 2 to 32 bytes, so one name
// fits the Go package, the PostgreSQL schema, the route prefix, the permission and signal
// prefix, the web folder and the message-key segment without translation (decision 10).
var nameRE = regexp.MustCompile(`^[a-z][a-z0-9]{1,31}$`)

// coreAreas are the names of the core's areas. A module may not take one, and the core's
// signals and permissions are prefixed by one.
var coreAreas = []string{
	"core", "people", "accounts", "access", "identity", "authz", "departments", "events",
	"signals", "notify", "audit", "files", "jobs", "settings", "telemetry", "db", "module",
	"modules",
}

// reservedNames are the names a module may not take besides the core areas: the shell
// packages and PostgreSQL's own schemas. Anything starting with "pg" is reserved too.
var reservedNames = []string{
	"api", "app", "config", "web", "admin", "client", "public", "information", "regional",
}

// IsCoreArea reports whether name is one of the core's areas, the names a module may not
// take (Manifest.Validate) and the names of core route sets and parts. Pure; cannot fail.
func IsCoreArea(name string) bool {
	for _, a := range coreAreas {
		if a == name {
			return true
		}
	}
	return false
}

func isReserved(name string) bool {
	if IsCoreArea(name) || strings.HasPrefix(name, "pg") {
		return true
	}
	for _, r := range reservedNames {
		if r == name {
			return true
		}
	}
	return false
}

// Manifest is everything the core needs to know about a module without building it. It
// is a plain value with no dependencies, so `bms migrate`, `bms check` and the admin
// graph work before any module is constructed. Every human-readable text about it lives
// in the module's message catalogue under keys derived from these names (decision 8).
type Manifest struct {
	// Name is the module's identity everywhere: Go package, PostgreSQL schema, route
	// prefix /api/<name>/, permission and signal prefix, web folder, message-key segment.
	// Format: "[a-z][a-z0-9]{1,31}" (letters and digits only, so one name fits every
	// place without translation) and not a reserved name.
	Name string

	// Migrations holds postgres/migrations/*.sql (go:embed), applied by `bms migrate` and
	// checked by `db` at start-up (D17). nil means the module has no schema; then it is
	// neither migrated nor checked.
	Migrations fs.FS

	// Permissions this module declares. Each name starts with "<Name>.". authz turns them
	// into what a regional can grant (09 §4).
	Permissions []authz.PermissionDecl

	// Settings this module declares. Keys are stored as "<Name>.<Key>" by settings.
	Settings []SettingDecl

	// Publishes and Subscribes are signal names (D24). A published name starts with
	// "<Name>."; a subscribed name is any valid name, declared by the core or by a module
	// compiled in.
	Publishes  []signals.Name
	Subscribes []signals.Name

	// Provides, Requires and Uses are capabilities (D19). Requires: start-up fails
	// without an enabled provider. Uses: optional; Links.Capability returns NotFound and
	// the module hides the feature.
	Provides []Capability
	Requires []Capability
	Uses     []Capability

	// Defines are extension points this module owns; their names start with "<Name>.".
	// ContributesTo are points, owned by the core or another module, this module
	// contributes to.
	Defines       []ExtensionPointDecl
	ContributesTo []ExtensionPoint
}

// Validate checks the manifest alone (not the graph) and fails with errs.Invalid joining
// one validate.FieldError per problem, with fields such as "name", "permissions[2].name",
// "settings[0].default", "publishes[1]", "requires[0]", "defines[0].name" and
// "contributesTo[1]" (F1 to F10). It never reads Migrations.
func (m Manifest) Validate() error {
	acc := validate.Errors{Op: "module.Manifest.Validate"}
	switch {
	case !nameRE.MatchString(m.Name):
		acc.Addf("name", codeMalformed,
			"must be 2 to 32 characters of a-z and 0-9, starting with a letter")
	case isReserved(m.Name):
		acc.Addf("name", "reserved", "is reserved for the core, the shell or PostgreSQL")
	}
	m.validatePermissions(&acc)
	m.validateSettings(&acc)
	m.validateSignals(&acc)
	m.validateCapabilities(&acc)
	m.validatePoints(&acc)
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

func (m Manifest) validatePermissions(acc *validate.Errors) {
	seen := make(map[authz.Permission]struct{}, len(m.Permissions))
	for i, p := range m.Permissions {
		prefix := fmt.Sprintf("permissions[%d]", i)
		asNested(acc, prefix, p.Validate())
		if p.Name.Validate() == nil && p.Name.Module() != m.Name {
			acc.Addf(prefix+".name", "prefix", "must start with %q", m.Name+".")
		}
		if _, dup := seen[p.Name]; dup {
			acc.Addf(prefix+".name", "duplicate", "declared more than once")
		}
		seen[p.Name] = struct{}{}
	}
}

func (m Manifest) validateSettings(acc *validate.Errors) {
	seen := make(map[string]struct{}, len(m.Settings))
	for i, s := range m.Settings {
		prefix := fmt.Sprintf("settings[%d]", i)
		asNested(acc, prefix, s.Validate())
		if _, dup := seen[s.Key]; dup {
			acc.Addf(prefix+".key", "duplicate", "declared more than once")
		}
		seen[s.Key] = struct{}{}
	}
}

func (m Manifest) validateSignals(acc *validate.Errors) {
	seen := make(map[signals.Name]struct{}, len(m.Publishes))
	for i, n := range m.Publishes {
		field := fmt.Sprintf("publishes[%d]", i)
		asField(acc, field, n.Validate())
		if n.Validate() == nil && n.Publisher() != m.Name {
			acc.Addf(field, "prefix", "must start with %q", m.Name+".")
		}
		if _, dup := seen[n]; dup {
			acc.Addf(field, "duplicate", "declared more than once")
		}
		seen[n] = struct{}{}
	}
	seen = make(map[signals.Name]struct{}, len(m.Subscribes))
	for i, n := range m.Subscribes {
		field := fmt.Sprintf("subscribes[%d]", i)
		asField(acc, field, n.Validate())
		if _, dup := seen[n]; dup {
			acc.Addf(field, "duplicate", "declared more than once")
		}
		seen[n] = struct{}{}
	}
}

func (m Manifest) validateCapabilities(acc *validate.Errors) {
	provides := make(map[Capability]struct{}, len(m.Provides))
	for i, c := range m.Provides {
		field := fmt.Sprintf("provides[%d]", i)
		asField(acc, field, c.Validate())
		if _, dup := provides[c]; dup {
			acc.Addf(field, "duplicate", "declared more than once")
		}
		provides[c] = struct{}{}
	}
	requires := make(map[Capability]struct{}, len(m.Requires))
	for i, c := range m.Requires {
		field := fmt.Sprintf("requires[%d]", i)
		asField(acc, field, c.Validate())
		if _, dup := requires[c]; dup {
			acc.Addf(field, "duplicate", "declared more than once")
		}
		if _, ok := provides[c]; ok {
			acc.Addf(field, "conflict", "a module cannot require a capability it provides")
		}
		requires[c] = struct{}{}
	}
	uses := make(map[Capability]struct{}, len(m.Uses))
	for i, c := range m.Uses {
		field := fmt.Sprintf("uses[%d]", i)
		asField(acc, field, c.Validate())
		if _, dup := uses[c]; dup {
			acc.Addf(field, "duplicate", "declared more than once")
		}
		if _, ok := requires[c]; ok {
			acc.Addf(field, "conflict", "a capability is either required or used, not both")
		}
		if _, ok := provides[c]; ok {
			acc.Addf(field, "conflict", "a module cannot use a capability it provides")
		}
		uses[c] = struct{}{}
	}
}

func (m Manifest) validatePoints(acc *validate.Errors) {
	seen := make(map[ExtensionPoint]struct{}, len(m.Defines))
	for i, d := range m.Defines {
		prefix := fmt.Sprintf("defines[%d]", i)
		asNested(acc, prefix, d.Validate())
		if d.Name.Validate() == nil && d.Name.Definer() != m.Name {
			acc.Addf(prefix+".name", "prefix", "must start with %q", m.Name+".")
		}
		if _, dup := seen[d.Name]; dup {
			acc.Addf(prefix+".name", "duplicate", "declared more than once")
		}
		seen[d.Name] = struct{}{}
	}
	contrib := make(map[ExtensionPoint]struct{}, len(m.ContributesTo))
	for i, p := range m.ContributesTo {
		field := fmt.Sprintf("contributesTo[%d]", i)
		asField(acc, field, p.Validate())
		if _, dup := contrib[p]; dup {
			acc.Addf(field, "duplicate", "declared more than once")
		}
		contrib[p] = struct{}{}
	}
}
