package notify

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"sort"
	"strings"
	"sync"
	"text/template"
	"text/template/parse"
	"unicode"
	"unicode/utf8"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// maxSubjectSourceBytes is the longest subject template an override may hold (the column's
// limit); a shipped file's Subject: header is held to MaxSubjectBytes.
const maxSubjectSourceBytes = 1024

// compiled is a subject and a body template, parsed. Funcs are bound at execution.
type compiled struct {
	subject *template.Template
	body    *template.Template
}

// shipped is one language of a registered template: its source and its parse.
type shipped struct {
	text TemplateText
	c    *compiled
}

// definition is a registered template: the var names its owner passes and its shipped
// languages.
type definition struct {
	name  Template
	vars  []string
	langs map[Language]*shipped
}

func (d *definition) languages() []Language {
	out := make([]Language, 0, len(d.langs))
	for l := range d.langs {
		out = append(out, l)
	}
	slices.Sort(out)
	return out
}

// catalogue holds the registered templates, by name and by owner.
type catalogue struct {
	mu     sync.RWMutex
	owners map[string]struct{}
	defs   map[Template]*definition
}

func newCatalogue() *catalogue {
	return &catalogue{owners: map[string]struct{}{}, defs: map[Template]*definition{}}
}

func (c *catalogue) get(name Template) (*definition, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	d, ok := c.defs[name]
	return d, ok
}

// names returns the registered names, sorted.
func (c *catalogue) names() []Template {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Template, 0, len(c.defs))
	for n := range c.defs {
		out = append(out, n)
	}
	slices.Sort(out)
	return out
}

// ---- parsing ----------------------------------------------------------------------------

// termStub is the placeholder func bound at parse time; execution binds the real one.
func termStub(string) string { return "" }

func newTemplate(name, src string) (*template.Template, error) {
	t, err := template.New(name).Option("missingkey=zero").Funcs(template.FuncMap{"term": termStub}).Parse(src)
	if err != nil {
		return nil, err //nolint:wrapcheck // the caller re-fields it
	}
	if len(t.Templates()) > 1 {
		return nil, errors.New("define, block and template are not allowed")
	}
	if err := onlyTerm(t); err != nil {
		return nil, err
	}
	return t, nil
}

// maxRangeDepth is how deeply range may nest.
const maxRangeDepth = 2

// rangesOverField reports whether a range walks a single field (.Vars): never a number, a
// variable or a computed value, which could loop without end.
func rangesOverField(p *parse.PipeNode) bool {
	if p == nil || len(p.Cmds) != 1 || len(p.Cmds[0].Args) != 1 {
		return false
	}
	_, ok := p.Cmds[0].Args[0].(*parse.FieldNode)
	return ok
}

// onlyTerm rejects any function but term, including the builtins (printf, index, call and
// the rest): templates are wording, not programs.
func onlyTerm(t *template.Template) error {
	var bad error
	var walk func(n parse.Node, loops int)
	walk = func(n parse.Node, loops int) {
		if bad != nil || n == nil {
			return
		}
		switch n := n.(type) {
		case *parse.ListNode:
			if n == nil {
				return
			}
			for _, c := range n.Nodes {
				walk(c, loops)
			}
		case *parse.ActionNode:
			walk(n.Pipe, loops)
		case *parse.PipeNode:
			if n == nil {
				return
			}
			for _, c := range n.Cmds {
				walk(c, loops)
			}
		case *parse.CommandNode:
			for _, a := range n.Args {
				walk(a, loops)
			}
		case *parse.IfNode:
			walk(n.Pipe, loops)
			walk(n.List, loops)
			walk(n.ElseList, loops)
		case *parse.RangeNode:
			loc, _ := t.ErrorContext(n)
			switch {
			case loops >= maxRangeDepth:
				bad = fmt.Errorf("template: %s: range is nested deeper than %d", loc, maxRangeDepth)
			case !rangesOverField(n.Pipe):
				bad = fmt.Errorf("template: %s: range may only walk a field such as .Vars", loc)
			}
			walk(n.Pipe, loops+1)
			walk(n.List, loops+1)
			walk(n.ElseList, loops)
		case *parse.WithNode:
			walk(n.Pipe, loops)
			walk(n.List, loops)
			walk(n.ElseList, loops)
		case *parse.ChainNode:
			walk(n.Node, loops)
		case *parse.TemplateNode:
			loc, _ := t.ErrorContext(n)
			bad = fmt.Errorf("template: %s: define, block and template are not allowed", loc)
		case *parse.IdentifierNode:
			if n.Ident != "term" {
				loc, _ := t.ErrorContext(n)
				bad = fmt.Errorf("template: %s: function %q is not allowed; only term is", loc, n.Ident)
			}
		}
	}
	if t.Tree != nil {
		walk(t.Root, 0)
	}
	return bad
}

// compile parses a subject and a body, failing errs.Invalid on the field "subject" or
// "body" with code parse. The reason is the parser's message with its line.
func compile(text TemplateText) (*compiled, error) {
	const op = "notify.compile"
	st, err := newTemplate("subject", text.Subject)
	if err != nil {
		return nil, fieldError(op, "subject", codeParse, nil, err.Error())
	}
	bt, err := newTemplate("body", text.Body)
	if err != nil {
		return nil, fieldError(op, "body", codeParse, nil, err.Error())
	}
	return &compiled{subject: st, body: bt}, nil
}

// checkText validates the shape of an override's text (not its syntax): required, size and
// control characters. Fails errs.Invalid joining one FieldError per problem.
func checkText(op string, t TemplateText) error {
	v := validate.Errors{Op: op}
	switch {
	case strings.TrimSpace(t.Subject) == "":
		addField(&v, "subject", codeRequired, nil, "must be set")
	case len(t.Subject) > maxSubjectSourceBytes:
		addField(&v, "subject", codeTooLong, maxParam(maxSubjectSourceBytes), "must be at most %d bytes", maxSubjectSourceBytes)
	case !utf8.ValidString(t.Subject):
		addField(&v, "subject", codeMalformed, nil, "must be valid UTF-8")
	case strings.IndexFunc(t.Subject, unicode.IsControl) >= 0:
		addField(&v, "subject", codeControl, nil, "must be one line without control characters")
	}
	switch {
	case strings.TrimSpace(t.Body) == "":
		addField(&v, "body", codeRequired, nil, "must be set")
	case len(t.Body) > MaxTemplateBytes:
		addField(&v, "body", codeTooLong, maxParam(MaxTemplateBytes), "must be at most %d bytes", MaxTemplateBytes)
	case !utf8.ValidString(t.Body):
		addField(&v, "body", codeMalformed, nil, "must be valid UTF-8")
	case strings.IndexFunc(t.Body, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' && r != '\r' }) >= 0:
		addField(&v, "body", codeControl, nil, "must not contain control characters")
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// parseFile reads one template file: headers, a blank line, the body.
func parseFile(raw []byte) (text TemplateText, vars []string, err error) {
	if len(raw) > MaxTemplateBytes {
		return TemplateText{}, nil, fmt.Errorf("larger than %d bytes", MaxTemplateBytes)
	}
	if !utf8.Valid(raw) {
		return TemplateText{}, nil, errors.New("not valid UTF-8")
	}
	src := strings.ReplaceAll(string(raw), "\r\n", "\n")
	head, body, found := strings.Cut(src, "\n\n")
	if !found {
		return TemplateText{}, nil, errors.New("no blank line between the headers and the body")
	}
	seenSubject, seenVars := false, false
	for _, line := range strings.Split(head, "\n") {
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			return TemplateText{}, nil, errors.New("a header line has no colon")
		}
		val = strings.TrimSpace(val)
		switch {
		case strings.EqualFold(key, "Subject") && !seenSubject:
			seenSubject, text.Subject = true, val
		case strings.EqualFold(key, "Vars") && !seenVars:
			seenVars = true
			if val == "" {
				continue
			}
			for _, name := range strings.Split(val, ",") {
				name = strings.TrimSpace(name)
				if !varKeyRE.MatchString(name) || slices.Contains(vars, name) {
					return TemplateText{}, nil, errors.New("the Vars header holds a malformed or repeated name")
				}
				vars = append(vars, name)
			}
		default:
			return TemplateText{}, nil, errors.New("an unknown or repeated header")
		}
	}
	text.Body = strings.TrimLeft(body, "\n")
	if text.Subject == "" {
		return TemplateText{}, nil, errors.New("the Subject header is required")
	}
	if len(text.Subject) > MaxSubjectBytes {
		return TemplateText{}, nil, fmt.Errorf("the Subject header is longer than %d bytes", MaxSubjectBytes)
	}
	if strings.TrimSpace(text.Body) == "" {
		return TemplateText{}, nil, errors.New("the body is empty")
	}
	return text, vars, nil
}

// splitFileName splits "<ident>.<lang>.tmpl".
func splitFileName(name string) (ident string, lang Language, ok bool) {
	base, found := strings.CutSuffix(name, TemplateFileExt)
	if !found {
		return "", "", false
	}
	ident, l, found := strings.Cut(base, ".")
	if !found || !identRE.MatchString(ident) || l == "" || Language(l).Validate() != nil {
		return "", "", false
	}
	return ident, Language(l), true
}

// Register adds an owner's default templates: files "<ident>.<lang>.tmpl" at the root of
// defaults, each with a Subject: header, an optional Vars: header, a blank line and the
// body. Every ident needs an "en" file. A nil defaults registers nothing (the owner still
// counts as registered). Fails errs.Invalid (owner, file name, header, parse error, size,
// subject length, a missing en), naming the file; errs.Conflict when owner was registered
// before; errs.Internal after Run started. Nothing is registered on failure.
func (s *Service) Register(owner string, defaults fs.FS) error {
	const op = "notify.Service.Register"
	if s.running.Load() {
		return errs.E(op, errs.Internal, errors.New("Register after Run"))
	}
	if len(owner) > maxOwnerBytes || !identRE.MatchString(owner) {
		return fieldError(op, "owner", codeMalformed, nil, "must be lower-case letters, digits and single hyphens")
	}
	defs, err := s.loadDefaults(op, owner, defaults)
	if err != nil {
		return err
	}
	c := s.cat
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, dup := c.owners[owner]; dup {
		return errs.E(op, errs.Conflict, errors.New("the owner is already registered"))
	}
	c.owners[owner] = struct{}{}
	for _, d := range defs {
		c.defs[d.name] = d
	}
	return nil
}

func (s *Service) loadDefaults(op, owner string, defaults fs.FS) ([]*definition, error) {
	if defaults == nil {
		return nil, nil
	}
	entries, err := fs.ReadDir(defaults, ".")
	if err != nil {
		return nil, errs.E(op, errs.Invalid, fmt.Errorf("reading the templates: %w", err))
	}
	byIdent := map[string]*definition{}
	var order []string
	bad := func(file string, why error) error {
		return fieldError(op, "templates", codeMalformed, nil, fmt.Sprintf("file %q: %s", file, why.Error()))
	}
	for _, e := range entries {
		file := e.Name()
		ident, lang, ok := splitFileName(file)
		if e.IsDir() || !ok {
			return nil, bad(file, errors.New("the name must be <name>.<language>.tmpl"))
		}
		raw, err := fs.ReadFile(defaults, file)
		if err != nil {
			return nil, bad(file, errors.New("cannot be read"))
		}
		text, vars, err := parseFile(raw)
		if err != nil {
			return nil, bad(file, err)
		}
		c, err := compile(text)
		if err != nil {
			return nil, bad(file, firstReason(err))
		}
		name := Template(owner + "." + ident)
		if err := name.Validate(); err != nil {
			return nil, bad(file, errors.New("the template name is malformed"))
		}
		d := byIdent[ident]
		if d == nil {
			d = &definition{name: name, vars: vars, langs: map[Language]*shipped{}}
			byIdent[ident] = d
			order = append(order, ident)
		}
		if !slices.Equal(d.vars, vars) && len(d.vars) > 0 && len(vars) > 0 {
			return nil, bad(file, errors.New("the Vars header differs from another language's"))
		}
		if len(d.vars) == 0 {
			d.vars = vars
		}
		d.langs[lang] = &shipped{text: text, c: c}
	}
	out := make([]*definition, 0, len(order))
	for _, ident := range order {
		d := byIdent[ident]
		if _, ok := d.langs["en"]; !ok {
			return nil, bad(ident+".en"+TemplateFileExt, errors.New("every template needs an en file"))
		}
		// A trial render with placeholders catches a template that parses but cannot run.
		for _, l := range d.languages() {
			sh := d.langs[l]
			if _, err := execute(sh.c, placeholderData(d.vars, Site{Name: "Example", URL: "https://example.org"}), nil); err != nil {
				return nil, bad(ident+"."+string(l)+TemplateFileExt, firstReason(err))
			}
		}
		out = append(out, d)
	}
	return out, nil
}

// firstReason returns the reason of the first FieldError in err, else err itself.
func firstReason(err error) error {
	if fes := validate.Fields(err); len(fes) > 0 {
		return errors.New(fes[0].Field + ": " + fes[0].Reason)
	}
	return err
}

// ---- rendering --------------------------------------------------------------------------

type siteData struct{ Name, URL string }
type recipientData struct{ PlayaName string }

// renderData is the data a template sees: .Site.Name, .Site.URL, .Recipient.PlayaName,
// .Link and .Vars.<key>.
type renderData struct {
	Site      siteData
	Recipient recipientData
	Link      string
	Vars      map[string]string
}

// placeholderData is what Preview and the trial render use: each var name is its own value.
func placeholderData(vars []string, site Site) renderData {
	m := make(map[string]string, len(vars))
	for _, v := range vars {
		m[v] = v
	}
	return renderData{
		Site:      siteData{Name: site.Name, URL: site.URL},
		Recipient: recipientData{PlayaName: "Sample Playa Name"},
		Link:      site.URL + "/example",
		Vars:      m,
	}
}

// limitedWriter fails once more than max bytes were written.
type limitedWriter struct {
	buf bytes.Buffer
	max int
}

var errTooLong = errors.New("the rendered text is too long")

func (w *limitedWriter) Write(p []byte) (int, error) {
	if w.buf.Len()+len(p) > w.max {
		return 0, errTooLong
	}
	return w.buf.Write(p) //nolint:wrapcheck // bytes.Buffer never fails
}

// execute renders c with terms bound to the term func. The result is cleaned: the subject is
// one line without control characters; the body keeps tab, newline and CR only. Fails
// errs.Invalid (field "template", code too_long) for an oversize result and (subject or
// body, code parse) when execution fails.
func execute(c *compiled, data renderData, terms map[string]string) (Rendered, error) {
	const op = "notify.execute"
	funcs := template.FuncMap{"term": func(key string) string {
		if w, ok := terms[key]; ok && w != "" {
			return w
		}
		return key
	}}
	run := func(t *template.Template, field string, limit int) (string, error) {
		clone, err := t.Clone()
		if err != nil {
			return "", errs.E(op, errs.Internal, err)
		}
		w := &limitedWriter{max: limit * 4} // generous: cleaning shortens, the check below is exact
		if err := clone.Funcs(funcs).Execute(w, data); err != nil {
			if errors.Is(err, errTooLong) {
				return "", fieldError(op, "template", codeTooLong, maxParam(limit), "the rendered "+field+" is too long")
			}
			return "", fieldError(op, field, codeParse, nil, err.Error())
		}
		return w.buf.String(), nil
	}
	subj, err := run(c.subject, "subject", MaxSubjectBytes)
	if err != nil {
		return Rendered{}, err
	}
	body, err := run(c.body, "body", MaxBodyBytes)
	if err != nil {
		return Rendered{}, err
	}
	subj = cleanSubject(subj)
	body = cleanBody(body)
	switch {
	case subj == "":
		return Rendered{}, fieldError(op, "subject", codeRequired, nil, "the rendered subject is empty")
	case len(subj) > MaxSubjectBytes:
		return Rendered{}, fieldError(op, "template", codeTooLong, maxParam(MaxSubjectBytes), "the rendered subject is too long")
	case strings.TrimSpace(body) == "":
		return Rendered{}, fieldError(op, "body", codeRequired, nil, "the rendered body is empty")
	case len(body) > MaxBodyBytes:
		return Rendered{}, fieldError(op, "template", codeTooLong, maxParam(MaxBodyBytes), "the rendered body is too long")
	}
	return Rendered{Subject: subj, Body: body}, nil
}

func cleanSubject(s string) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

func cleanBody(s string) string {
	s = strings.ToValidUTF8(s, "")
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' && r != '\r' {
			return -1
		}
		return r
	}, s)
}

// candidates lists the languages to try for a recipient: their own tag, its primary
// subtag, the site default and its primary subtag, then en.
func candidates(personLang, siteDefault Language) []Language {
	var out []Language
	for _, l := range []Language{personLang, primary(personLang), siteDefault, primary(siteDefault), DefaultLanguage} {
		if l != "" && !slices.Contains(out, l) {
			out = append(out, l)
		}
	}
	return out
}

// sortedKeys is a helper for stable output.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
