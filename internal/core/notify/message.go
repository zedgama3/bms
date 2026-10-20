package notify

import (
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

const (
	maxTemplateNameBytes = 96
	maxOwnerBytes        = 32
	maxLanguageBytes     = 35
	identPattern         = `[a-z][a-z0-9]*(?:-[a-z0-9]+)*`
)

var (
	identRE    = regexp.MustCompile(`^` + identPattern + `$`)
	templateRE = regexp.MustCompile(`^(` + identPattern + `)\.(` + identPattern + `)$`)
	languageRE = regexp.MustCompile(`^[a-z]{2,3}(-[A-Za-z0-9]{1,8})*$`)
	keyRE      = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)
	varKeyRE   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
)

// Template names a message template, "<owner>.<ident>" (the signals.Name grammar: owner and
// ident [a-z][a-z0-9]*(-[a-z0-9]+)*, at most 96 bytes in all). Owner is a module name or a
// core area.
type Template string

// Validate fails errs.Invalid (field "template"): required when empty, malformed otherwise.
func (t Template) Validate() error {
	const op = "notify.Template.Validate"
	switch {
	case t == "":
		return fieldError(op, "template", codeRequired, nil, "must be set")
	case len(t) > maxTemplateNameBytes || len(t.Owner()) > maxOwnerBytes || !templateRE.MatchString(string(t)):
		return fieldError(op, "template", codeMalformed, nil, "must be <owner>.<name>, lower-case letters, digits and single hyphens")
	}
	return nil
}

// Owner returns the text before the first dot ("" when there is none). Cannot fail.
func (t Template) Owner() string {
	o, _, _ := strings.Cut(string(t), ".")
	return o
}

// Language is "" (the site default) or a BCP 47 tag matching people's grammar
// ^[a-z]{2,3}(-[A-Za-z0-9]{1,8})*$, at most 35 bytes.
type Language string

// Validate fails errs.Invalid (field "language", code malformed). "" is valid.
func (l Language) Validate() error {
	if l == "" {
		return nil
	}
	if len(l) > maxLanguageBytes || !languageRE.MatchString(string(l)) {
		return fieldError("notify.Language.Validate", "language", codeMalformed, nil, "must be a language tag such as en or es-MX")
	}
	return nil
}

// Message is what a caller sends. Vars carry nothing sensitive and nothing personal about
// anyone but the recipient. Link is a path under the site URL ("/volunteers/shifts/..."),
// never a full URL.
type Message struct {
	Template Template
	To       []model.PersonID  // 1..MaxRecipients, no duplicates, no zero ID (Send); empty for SendTo
	Key      string            // optional; [A-Za-z0-9._:-]{1,128}; unique per (owner, key, recipient)
	Vars     map[string]string // at most MaxVars; keys [a-z][a-z0-9_]{0,31}; values valid UTF-8, no control characters but \n and \t, at most MaxVarBytes
	Link     string            // "" or a path: starts with "/", not "//", no control characters, at most MaxLinkBytes
	// TTL is how long the row may wait, text and all: 0 means MaxAge; above MaxAge is
	// invalid. A message carrying a one-time link passes the link's own lifetime. A row not
	// sent by then ends as failed ("expired") and its text is cleared.
	TTL time.Duration
}

// Validate fails errs.Invalid joining one validate.FieldError per problem: "template"
// (required, malformed), "to" (required, too_many {max}, malformed, duplicate), "key"
// (too_long {max}, malformed), "vars" (too_many {max}, malformed), "vars.<key>" (malformed,
// too_long {max}, control_chars), "link" (malformed, too_long {max}, control_chars), "ttl" (range). The
// messages never echo a value.
func (m Message) Validate() error { return m.validate("notify.Message.Validate", true) }

// validate checks m; needTo is false for SendTo, which takes no recipient list.
func (m Message) validate(op string, needTo bool) error {
	v := validate.Errors{Op: op}
	for _, fe := range validate.Fields(m.Template.Validate()) {
		addField(&v, fe.Field, fe.Code, fe.Params, "%s", fe.Reason)
	}
	if needTo {
		m.validateTo(&v)
	} else if len(m.To) > 0 {
		addField(&v, "to", codeMalformed, nil, "must be empty when sending to an address")
	}
	m.validateKey(&v)
	m.validateVars(&v)
	m.validateLink(&v)
	if m.TTL < 0 || m.TTL > MaxAge {
		addField(&v, "ttl", validate.CodeRange, map[string]string{"min": "0s", "max": MaxAge.String()}, "must be between 0 and %s", MaxAge)
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

func (m Message) validateTo(v *validate.Errors) {
	switch {
	case len(m.To) == 0:
		addField(v, "to", codeRequired, nil, "at least one recipient is required")
		return
	case len(m.To) > MaxRecipients:
		addField(v, "to", codeTooMany, maxParam(MaxRecipients), "at most %d recipients", MaxRecipients)
		return
	}
	seen := make(map[model.PersonID]struct{}, len(m.To))
	zero, dup := false, false
	for _, p := range m.To {
		if p.IsZero() {
			zero = true
			continue
		}
		if _, ok := seen[p]; ok {
			dup = true
		}
		seen[p] = struct{}{}
	}
	if zero {
		addField(v, "to", codeMalformed, nil, "a recipient is the zero ID")
	}
	if dup {
		addField(v, "to", codeDuplicate, nil, "a recipient is listed twice")
	}
}

func (m Message) validateKey(v *validate.Errors) {
	switch {
	case m.Key == "":
	case len(m.Key) > MaxKeyBytes:
		addField(v, "key", codeTooLong, maxParam(MaxKeyBytes), "must be at most %d bytes", MaxKeyBytes)
	case !keyRE.MatchString(m.Key):
		addField(v, "key", codeMalformed, nil, "may hold letters, digits and . _ : - only")
	}
}

func (m Message) validateVars(v *validate.Errors) {
	if len(m.Vars) > MaxVars {
		addField(v, "vars", codeTooMany, maxParam(MaxVars), "at most %d variables", MaxVars)
		return
	}
	keys := make([]string, 0, len(m.Vars))
	for k := range m.Vars {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	badKey := false
	for _, k := range keys {
		if !varKeyRE.MatchString(k) {
			badKey = true // a caller-chosen name is never echoed (privacy)
			continue
		}
		field := "vars." + k
		val := m.Vars[k]
		switch {
		case len(val) > MaxVarBytes:
			addField(v, field, codeTooLong, maxParam(MaxVarBytes), "must be at most %d bytes", MaxVarBytes)
		case !utf8.ValidString(val):
			addField(v, field, codeMalformed, nil, "must be valid UTF-8")
		case strings.IndexFunc(val, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }) >= 0:
			addField(v, field, codeControl, nil, "must not contain control characters")
		}
	}
	if badKey {
		addField(v, "vars", codeMalformed, nil, "every name must be lower-case letters, digits and underscores, starting with a letter, at most %d bytes", MaxVarKeyBytes)
	}
}

func (m Message) validateLink(v *validate.Errors) {
	l := m.Link
	switch {
	case l == "":
	case len(l) > MaxLinkBytes:
		addField(v, "link", codeTooLong, maxParam(MaxLinkBytes), "must be at most %d bytes", MaxLinkBytes)
	case !utf8.ValidString(l):
		addField(v, "link", codeMalformed, nil, "must be valid UTF-8")
	case strings.IndexFunc(l, unicode.IsControl) >= 0:
		addField(v, "link", codeControl, nil, "must not contain control characters")
	case l[0] != '/' || strings.HasPrefix(l, "//") || strings.Contains(l, `\`):
		addField(v, "link", codeMalformed, nil, "must be a path starting with a single /")
	}
}

// clone returns a deep copy of m, so a queued message does not alias the caller's maps.
func (m Message) clone() Message {
	m.To = slices.Clone(m.To)
	if m.Vars != nil {
		vars := make(map[string]string, len(m.Vars))
		for k, v := range m.Vars {
			vars[k] = v
		}
		m.Vars = vars
	}
	return m
}
