package notify

import (
	"crypto/sha256"
	"embed"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/notify/channel"
	"github.com/zedgama3/bms/internal/core/notify/postgres"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

//go:embed templates/*.tmpl
var embeddedTemplates embed.FS

// Templates holds notify's own template, notify.test (en, es), in the layout Register reads.
var Templates fs.FS

func init() {
	sub, err := fs.Sub(embeddedTemplates, "templates")
	if err != nil {
		panic("notify: embedded templates missing: " + err.Error())
	}
	Templates = sub
}

// Set is the migration set app adds to its core sets.
var Set = db.Set{Name: "notify", Schema: "core", Files: postgres.Migrations}

// The permissions notify declares; app adds Permissions to its core permissions.
const (
	PermissionTemplatesManage authz.Permission = "notify.templates.manage" // templates and the test message
	PermissionOutboxManage    authz.Permission = "notify.outbox.manage"    // the outbox, retry, cancel, check connection
)

// Permissions lists the declarations of the two permissions above (notify.md, "Permissions").
var Permissions = []authz.PermissionDecl{
	{
		Name: PermissionTemplatesManage, Scope: authz.ScopeSite, Reaches: model.SensitivityInternal,
		StarterRoles: []string{authz.RoleSettingsAdmin},
	},
	{
		Name: PermissionOutboxManage, Scope: authz.ScopeSite, Reaches: model.SensitivityInternal,
		StarterRoles: []string{authz.RoleSettingsAdmin},
	},
}

// The audit actions notify records, and the record kinds they point at.
const (
	actionTemplateChanged audit.Action = "notify.template.changed"
	actionTemplateReset   audit.Action = "notify.template.reset"
	actionTestSent        audit.Action = "notify.test.sent"
	actionMessageRetried  audit.Action = "notify.message.retried"
	actionMessageCanceled audit.Action = "notify.message.canceled"

	kindTemplate = "notify.template"
	kindMessage  = "notify.message"
)

// refOf derives the fixed audit reference of a template in a language: the first 16 bytes of
// sha256("notify.template:" + name + ":" + lang).
func refOf(name Template, lang Language) audit.Ref {
	sum := sha256.Sum256([]byte(kindTemplate + ":" + string(name) + ":" + string(lang)))
	return audit.Ref{Kind: kindTemplate, ID: [16]byte(sum[:16])}
}

// ---- settings ---------------------------------------------------------------------------

// Settings declares the notify.* keys for the settings core area "notify" (notify.md,
// "Settings"; D39). app passes them to settings.New as Area{Name: "notify", Core: true} and
// implements SMTPSource and RetentionSource over the area's scoped Reader. Titles and
// descriptions are the catalogue keys core.notify.setting.<key>.title|description|option.<opt>.
var Settings = []settings.Decl{
	{Key: "smtp_host", Kind: settings.Text, Check: checkHost},
	{Key: "smtp_port", Kind: settings.Int, Default: "587", Check: checkPort},
	{Key: "smtp_security", Kind: settings.Enum, Default: "starttls", Options: []string{"starttls", "tls", "none"}},
	{Key: "smtp_username", Kind: settings.Text, Check: checkMax(channel.MaxUsernameBytes)},
	{Key: "smtp_password", Kind: settings.Secret, Check: checkMax(channel.MaxPasswordBytes)},
	{Key: "sender_address", Kind: settings.Text, Check: checkAddress},
	{Key: "sender_name", Kind: settings.Text, Check: checkSenderName},
	{Key: "reply_to", Kind: settings.Text, Check: checkAddress},
	{Key: "alert_addresses", Kind: settings.Text, Check: checkAlertAddresses},
	{Key: "retention", Kind: settings.Duration, Default: "0s", Check: checkRetention},
}

func ruleErr(code string, params map[string]string, reason string) error {
	return fieldError("", "", code, params, reason)
}

func checkHost(v string) error {
	if v != "" && !validHost(v) {
		return ruleErr(codeMalformed, nil, "must be a host name or an IP address")
	}
	return nil
}

func validHost(h string) bool {
	c := channel.SMTPConfig{Host: h, Port: 25, Security: channel.None}
	for _, fe := range validate.Fields(c.Validate()) {
		if fe.Field == "notify.smtp_host" {
			return false
		}
	}
	return true
}

func checkPort(v string) error {
	n, err := strconv.Atoi(v)
	if err != nil {
		return ruleErr(codeMalformed, nil, "must be a number")
	}
	if n < 1 || n > 65535 {
		return ruleErr(validate.CodeRange, map[string]string{"min": "1", "max": "65535"}, "must be between 1 and 65535")
	}
	return nil
}

func checkMax(limit int) func(string) error {
	return func(v string) error {
		if len(v) > limit {
			return ruleErr(codeTooLong, maxParam(limit), "is too long")
		}
		return nil
	}
}

// ParseAddress reads a settings value that holds one address ("" = none). Fails
// errs.Invalid (malformed).
func ParseAddress(v string) (model.Email, error) {
	if v == "" {
		return "", nil
	}
	e, err := model.ParseEmail(v)
	if err != nil {
		return "", ruleErr(codeMalformed, nil, "must be an e-mail address")
	}
	return e, nil
}

func checkAddress(v string) error {
	_, err := ParseAddress(v)
	return err
}

func checkSenderName(v string) error {
	switch {
	case len(v) > channel.MaxSenderName:
		return ruleErr(codeTooLong, maxParam(channel.MaxSenderName), "is too long")
	case strings.IndexFunc(v, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0:
		return ruleErr(codeControl, nil, "must not contain control characters")
	}
	return nil
}

// ParseAddresses reads the comma-separated notify.alert_addresses value. Fails errs.Invalid
// (too_many or malformed).
func ParseAddresses(v string) ([]model.Email, error) {
	if strings.TrimSpace(v) == "" {
		return nil, nil
	}
	parts := strings.Split(v, ",")
	if len(parts) > channel.MaxAlertAddresses {
		return nil, ruleErr(codeTooMany, maxParam(channel.MaxAlertAddresses), "too many addresses")
	}
	out := make([]model.Email, 0, len(parts))
	for _, p := range parts {
		e, err := model.ParseEmail(p)
		if err != nil {
			return nil, ruleErr(codeMalformed, nil, "every entry must be an e-mail address")
		}
		out = append(out, e)
	}
	return out, nil
}

func checkAlertAddresses(v string) error {
	_, err := ParseAddresses(v)
	return err
}

func checkRetention(v string) error {
	d, err := time.ParseDuration(v)
	if err != nil {
		return ruleErr(codeMalformed, nil, "must be a duration such as 168h")
	}
	if d != 0 && (d < MinRetention || d > MaxRetention) {
		return ruleErr(validate.CodeRange, map[string]string{"min": MinRetention.String(), "max": MaxRetention.String()}, "must be 0 or between 7 days and 100 years")
	}
	return nil
}
