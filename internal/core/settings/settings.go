package settings

import (
	"crypto/sha256"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/settings/postgres"
	"github.com/zedgama3/bms/internal/model"
)

// Languages are the languages of the build. Callers must not modify it.
var Languages = []string{"en", "es"}

// CoreTerms are the terms the core lets a regional rename; areas declare more in
// Area.Terms (D57). A word two parts of bms need belongs here, as "lead" does. Callers
// must not modify it.
var CoreTerms = []string{"event", "department", "participant", "theme_camp", "lead"}

// coreArea is TermDoc.Area for a term of CoreTerms.
const coreArea = "core"

// formNames are the four forms of a term, in the order Forms lists them.
var formNames = []string{formSingular, formPlural, formSingularTitle, formPluralTitle}

// The four forms of a term.
const (
	formSingular      = "singular"
	formPlural        = "plural"
	formSingularTitle = "singular_title"
	formPluralTitle   = "plural_title"
)

// The constants that are not settings, pinned by TestConstants_Pinned.
const (
	MinContrast            = 4.5
	DefaultRefreshInterval = 30 * time.Second

	minRefreshInterval = time.Second
	maxRefreshInterval = 10 * time.Minute
)

// The permissions settings declares; app adds Permissions to corePerms.
const (
	PermissionRead   authz.Permission = "settings.read"   // GET / and GET /terms: every declared setting's value, the terms
	PermissionManage authz.Permission = "settings.manage" // PUT /values and PUT /terms/{lang}
)

// Permissions lists the declarations of the two permissions above, with their starter role
// (settings.md, "Permissions").
var Permissions = []authz.PermissionDecl{
	{
		Name: PermissionRead, Scope: authz.ScopeSite, Reaches: model.SensitivityInternal,
		StarterRoles: []string{authz.RoleSettingsAdmin},
	},
	{
		Name: PermissionManage, Scope: authz.ScopeSite, Reaches: model.SensitivityInternal,
		StarterRoles: []string{authz.RoleSettingsAdmin},
	},
}

// Set is the migration set app adds to coreSets.
var Set = db.Set{Name: "settings", Schema: "core", Files: postgres.Migrations}

// The audit actions settings records, and the record kinds they point at.
const (
	actionSettingChanged audit.Action = "settings.setting.changed"
	actionSettingReset   audit.Action = "settings.setting.reset"
	actionTermChanged    audit.Action = "settings.term.changed"
	actionTermReset      audit.Action = "settings.term.reset"

	kindSetting = "settings.setting"
	kindTerm    = "settings.term"
)

// Revision is the number every write bumps; a replica follows it.
type Revision int64

// IDOf derives the fixed audit reference for a key: the first 16 bytes of
// sha256("settings.setting:" + key). Cannot fail.
func IDOf(key string) [16]byte {
	return refOf(kindSetting + ":" + key)
}

func refOf(s string) [16]byte {
	sum := sha256.Sum256([]byte(s))
	return [16]byte(sum[:16])
}

// Forms are the four forms of one term in one language.
type Forms struct {
	Singular      string `json:"singular"`       // "department"
	Plural        string `json:"plural"`         // "departments"
	SingularTitle string `json:"singular_title"` // "Department"
	PluralTitle   string `json:"plural_title"`   // "Departments"
}

// Changes is one batch of value changes: a text form sets, nil resets to the default.
type Changes struct {
	Values map[string]*string `json:"values"` // full keys "<area>.<key>", 1..MaxBatch
}

// Document is what GET / returns to a settings admin.
type Document struct {
	Revision Revision  `json:"revision"`
	Areas    []AreaDoc `json:"areas"` // core areas, then modules in build order
}

// AreaDoc is one area of a Document.
type AreaDoc struct {
	Name     string       `json:"name"`
	Core     bool         `json:"core"`
	Settings []SettingDoc `json:"settings"`
}

// SettingDoc is one setting of a Document. A Secret's value is never in it.
type SettingDoc struct {
	Key       string   `json:"key"`     // full key
	Kind      string   `json:"kind"`    // Kind.String()
	Default   string   `json:"default"` // "" for a secret
	Value     *string  `json:"value"`   // nil = default applies; always nil for a secret
	SecretSet bool     `json:"secret_set"`
	Options   []string `json:"options,omitempty"`
	Stale     bool     `json:"stale"` // a stored value no longer parses (decision 5)
}

// TermsDocument is what GET /terms returns: every language, every term, default and override.
type TermsDocument struct {
	Languages []string                      `json:"languages"`
	Terms     map[string]map[string]TermDoc `json:"terms"` // lang -> term -> doc
}

// TermDoc is one term in one language.
type TermDoc struct {
	Area     string      `json:"area"` // "core" for CoreTerms, else the declaring area's name (D57)
	Default  Forms       `json:"default"`
	Override FormChanges `json:"override"` // nil fields = not overridden
}

// TermChanges is one batch of terminology changes for one language.
type TermChanges struct {
	Terms map[string]FormChanges `json:"terms"` // term -> forms; a nil field is unchanged
}

// FormChanges changes the forms of one term. "" is invalid; nil leaves a form unchanged.
type FormChanges struct {
	Singular      *string  `json:"singular,omitempty"`
	Plural        *string  `json:"plural,omitempty"`
	SingularTitle *string  `json:"singular_title,omitempty"`
	PluralTitle   *string  `json:"plural_title,omitempty"`
	Reset         []string `json:"reset,omitempty"` // forms to reset: "singular", ...
}
