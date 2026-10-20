package identity

import (
	"embed"
	"io/fs"
)

//go:embed templates/*.tmpl
var embeddedTemplates embed.FS

// Templates holds identity's e-mail templates as notify's Register reads them: the files
// password-reset.{en,es}.tmpl (variable lifetime_minutes: how long the link works, a bare
// whole number such as "60"; each language's template words the unit) and
// superuser-signed-in.{en,es}.tmpl (no variables; the link is the audit log), at the root.
// The owner is "identity": app registers it with notify.Register("identity", identity.Templates).
var Templates fs.FS

func init() {
	sub, err := fs.Sub(embeddedTemplates, "templates")
	if err != nil {
		panic("identity: embedded templates missing: " + err.Error())
	}
	Templates = sub
}
