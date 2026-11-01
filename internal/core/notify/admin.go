package notify

import (
	"context"
	"errors"
	"slices"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/notify/message"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// require fails errs.Unauthenticated without a signed-in person and errs.Permission unless
// the actor holds the site-wide permission p. It returns the actor.
func (s *Service) require(ctx context.Context, op string, p authz.Permission) (authz.Actor, error) {
	a, ok := s.actorF(ctx)
	if !ok || (a.Person.IsZero() && !a.Superuser) {
		return authz.Actor{}, errs.E(op, errs.Unauthenticated, errors.New("no signed-in person"))
	}
	if err := s.authz.Allow(ctx, p); err != nil {
		return authz.Actor{}, wrap(op, err)
	}
	return a, nil
}

// checkLanguage fails errs.Invalid (field "language", code malformed) for "" or a bad tag.
func checkLanguage(op string, lang Language) error {
	if lang == "" || lang.Validate() != nil {
		return fieldError(op, "language", codeMalformed, nil, "must be a language tag such as en or es-MX")
	}
	return nil
}

func notFound(op, what string) error {
	return errs.E(op, errs.NotFound, errors.New("no such "+what))
}

// ---- templates --------------------------------------------------------------------------

// ListTemplates lists every registered template with its languages, then the overrides of
// templates nobody registers here (Available false). Needs notify.templates.manage. Fails
// errs.Unauthenticated, errs.Permission and store kinds.
func (s *Service) ListTemplates(ctx context.Context) (_ []TemplateInfo, err error) {
	const op = "notify.Service.ListTemplates"
	ctx, span := s.start(ctx, "list_templates")
	defer endSpan(span, &err)
	if _, err := s.require(ctx, op, PermissionTemplatesManage); err != nil {
		return nil, err
	}
	overrides, err := s.store.Overrides(ctx)
	if err != nil {
		return nil, wrap(op, err)
	}
	byName := map[string][]message.Override{}
	for _, o := range overrides {
		byName[o.Name] = append(byName[o.Name], o)
	}
	out := []TemplateInfo{}
	for _, name := range s.cat.names() {
		def, _ := s.cat.get(name)
		info := TemplateInfo{Name: name, Vars: slices.Clone(def.vars), Available: true}
		if info.Vars == nil {
			info.Vars = []string{}
		}
		langs := map[Language]*LanguageInfo{}
		for _, l := range def.languages() {
			langs[l] = &LanguageInfo{Language: l}
		}
		for _, o := range byName[string(name)] {
			li := langs[Language(o.Language)]
			if li == nil {
				li = &LanguageInfo{Language: Language(o.Language)}
				langs[li.Language] = li
			}
			li.Overridden, li.UpdatedAt = true, o.UpdatedAt
		}
		info.Languages = sortedLanguages(langs)
		out = append(out, info)
	}
	for _, name := range sortedKeys(byName) {
		if _, ok := s.cat.get(Template(name)); ok {
			continue
		}
		langs := map[Language]*LanguageInfo{}
		for _, o := range byName[name] {
			langs[Language(o.Language)] = &LanguageInfo{Language: Language(o.Language), Overridden: true, UpdatedAt: o.UpdatedAt}
		}
		out = append(out, TemplateInfo{Name: Template(name), Vars: []string{}, Languages: sortedLanguages(langs)})
	}
	return out, nil
}

func sortedLanguages(m map[Language]*LanguageInfo) []LanguageInfo {
	out := make([]LanguageInfo, 0, len(m))
	for _, li := range m {
		out = append(out, *li)
	}
	slices.SortFunc(out, func(a, b LanguageInfo) int { return compareString(string(a.Language), string(b.Language)) })
	return out
}

func compareString(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// GetTemplate returns the shipped text and the current text of one template in one language.
// Needs notify.templates.manage. Fails errs.Unauthenticated, errs.Permission, errs.Invalid
// (language, malformed), errs.NotFound (no such template or language) and store kinds.
func (s *Service) GetTemplate(ctx context.Context, name Template, lang Language) (_ TemplateView, err error) {
	const op = "notify.Service.GetTemplate"
	ctx, span := s.start(ctx, "get_template")
	defer endSpan(span, &err)
	if _, err := s.require(ctx, op, PermissionTemplatesManage); err != nil {
		return TemplateView{}, err
	}
	if err := checkLanguage(op, lang); err != nil {
		return TemplateView{}, err
	}
	def, sh, err := s.definitionOf(op, name, lang)
	if err != nil {
		return TemplateView{}, err
	}
	view := TemplateView{Name: name, Language: lang, Default: defaultText(def, sh), Vars: slices.Clone(def.vars)}
	view.Current = view.Default
	if view.Vars == nil {
		view.Vars = []string{}
	}
	o, oerr := s.store.Override(ctx, string(name), string(lang))
	switch {
	case oerr == nil:
		view.Current, view.Overridden = TemplateText{Subject: o.Subject, Body: o.Body}, true
	case errs.Is(oerr, errs.NotFound):
		if sh == nil {
			return TemplateView{}, notFound(op, "language")
		}
	default:
		return TemplateView{}, wrap(op, oerr)
	}
	return view, nil
}

// definitionOf finds a registered template; sh is its shipped text in lang, nil when it
// ships none (an override may still add the language). Fails errs.NotFound for the name.
func (s *Service) definitionOf(op string, name Template, lang Language) (*definition, *shipped, error) {
	def, ok := s.cat.get(name)
	if !ok {
		return nil, nil, notFound(op, "template")
	}
	return def, def.langs[lang], nil
}

// defaultText is the shipped text of sh, or the English text for a language the template
// ships none in (an override added for it shows the English default beside it).
func defaultText(def *definition, sh *shipped) TemplateText {
	if sh != nil {
		return sh.text
	}
	return def.langs[DefaultLanguage].text
}

// checkOverride validates the shape, parses and trial-renders an override's text.
func (s *Service) checkOverride(ctx context.Context, op string, def *definition, lang Language, t TemplateText) (Rendered, error) {
	if err := checkText(op, t); err != nil {
		return Rendered{}, err
	}
	c, err := compile(t)
	if err != nil {
		return Rendered{}, wrap(op, err)
	}
	site, err := s.siteOf(ctx, lang)
	if err != nil {
		return Rendered{}, seamError(op, "the site settings", err)
	}
	r, err := execute(c, placeholderData(def.vars, site), site.Terms)
	if err != nil {
		return Rendered{}, wrap(op, err)
	}
	return r, nil
}

// PutTemplate saves an override. The text is checked, parsed and rendered with placeholder
// values first; nothing is saved when any step fails. Needs notify.templates.manage and
// records notify.template.changed in the same transaction. Fails errs.Unauthenticated,
// errs.Permission, errs.Invalid (language malformed; subject or body: required, too_long,
// control_chars, parse), errs.NotFound, errs.Unavailable wrapping db.ErrReadOnly, the audit
// error and store kinds.
func (s *Service) PutTemplate(ctx context.Context, name Template, lang Language, t TemplateText) (_ TemplateView, err error) {
	const op = "notify.Service.PutTemplate"
	ctx, span := s.start(ctx, "put_template")
	defer endSpan(span, &err)
	a, err := s.require(ctx, op, PermissionTemplatesManage)
	if err != nil {
		return TemplateView{}, err
	}
	if err := checkLanguage(op, lang); err != nil {
		return TemplateView{}, err
	}
	def, sh, err := s.definitionOf(op, name, lang)
	if err != nil {
		return TemplateView{}, err
	}
	if _, err := s.checkOverride(ctx, op, def, lang, t); err != nil {
		return TemplateView{}, err
	}
	now := s.now()
	err = s.db.Transact(ctx, db.Write, func(ctx context.Context) error {
		o := message.Override{Name: string(name), Language: string(lang), Subject: t.Subject, Body: t.Body, UpdatedBy: a.Person, UpdatedAt: now}
		if err := s.store.PutOverride(ctx, o); err != nil {
			return err //nolint:wrapcheck // wrapped below
		}
		return s.audit.Record(ctx, audit.Entry{
			Action: actionTemplateChanged, Outcome: audit.Succeeded, Record: refOf(name, lang),
			Reason: "template:" + string(name) + ":" + string(lang),
		})
	})
	if err != nil {
		return TemplateView{}, wrap(op, err)
	}
	vars := slices.Clone(def.vars)
	if vars == nil {
		vars = []string{}
	}
	return TemplateView{Name: name, Language: lang, Default: defaultText(def, sh), Current: t, Overridden: true, Vars: vars}, nil
}

// ResetTemplate removes an override, back to the shipped text. Idempotent for a registered
// template (nil when there is none, nothing audited); the override of an unregistered name
// can be removed too. Needs notify.templates.manage and records notify.template.reset. Fails
// as PutTemplate, plus errs.NotFound for an unregistered name with no override.
func (s *Service) ResetTemplate(ctx context.Context, name Template, lang Language) (err error) {
	const op = "notify.Service.ResetTemplate"
	ctx, span := s.start(ctx, "reset_template")
	defer endSpan(span, &err)
	if _, err := s.require(ctx, op, PermissionTemplatesManage); err != nil {
		return err
	}
	if err := checkLanguage(op, lang); err != nil {
		return err
	}
	if err := name.Validate(); err != nil {
		return notFound(op, "template")
	}
	_, registered := s.cat.get(name)
	err = s.db.Transact(ctx, db.Write, func(ctx context.Context) error {
		removed, err := s.store.ResetOverride(ctx, string(name), string(lang))
		if err != nil {
			return err //nolint:wrapcheck // wrapped below
		}
		if !removed {
			if registered {
				return nil
			}
			return notFound(op, "template")
		}
		return s.audit.Record(ctx, audit.Entry{
			Action: actionTemplateReset, Outcome: audit.Succeeded, Record: refOf(name, lang),
			Reason: "template:" + string(name) + ":" + string(lang),
		})
	})
	return wrap(op, err)
}

// Preview renders t (not saved) with placeholder values: each declared variable stands for
// itself. Needs notify.templates.manage. Fails errs.Unauthenticated, errs.Permission,
// errs.NotFound, errs.Invalid as PutTemplate, and the kinds of Regional and Terms.
func (s *Service) Preview(ctx context.Context, name Template, lang Language, t TemplateText) (_ Rendered, err error) {
	const op = "notify.Service.Preview"
	ctx, span := s.start(ctx, "preview")
	defer endSpan(span, &err)
	if _, err := s.require(ctx, op, PermissionTemplatesManage); err != nil {
		return Rendered{}, err
	}
	if err := checkLanguage(op, lang); err != nil {
		return Rendered{}, err
	}
	def, _, err := s.definitionOf(op, name, lang)
	if err != nil {
		return Rendered{}, err
	}
	return s.checkOverride(ctx, op, def, lang, t)
}

// SendTest queues notify.test to the signed-in person through the normal queue and returns
// the message's ID. Needs notify.templates.manage and records notify.test.sent in the same
// transaction. A person without an address gets a row that ends skipped later (no_address). Fails
// errs.Unauthenticated, errs.Permission, errs.Unsupported (wrapping ErrNotConfigured, or a
// superuser, who has no address), errs.Unavailable wrapping db.ErrReadOnly, the audit error
// and store kinds.
func (s *Service) SendTest(ctx context.Context) (_ MessageID, err error) {
	const op = "notify.Service.SendTest"
	ctx, span := s.start(ctx, "send_test")
	defer endSpan(span, &err)
	a, err := s.require(ctx, op, PermissionTemplatesManage)
	if err != nil {
		return MessageID{}, err
	}
	if a.Person.IsZero() {
		return MessageID{}, errs.E(op, errs.Unsupported, errors.New("a superuser has no address to send a test to"))
	}
	var id MessageID
	owner := ownerNotify
	err = s.db.Transact(ctx, db.Write, func(ctx context.Context) error {
		ids, err := s.enqueue(ctx, &owner, "", Message{Template: "notify.test", To: []model.PersonID{a.Person}})
		if err != nil {
			return err //nolint:wrapcheck // wrapped below
		}
		if len(ids) != 1 {
			return errs.E(op, errs.Internal, errors.New("the test message was not queued"))
		}
		id = ids[0]
		return s.audit.Record(ctx, audit.Entry{
			Action: actionTestSent, Outcome: audit.Succeeded, Subject: a.Person, Record: audit.RefOf(kindMessage, id),
		})
	})
	if err != nil {
		return MessageID{}, wrap(op, err)
	}
	return id, nil
}

// ---- the outbox -------------------------------------------------------------------------

// Outbox returns the counts by status, whether e-mail is configured and the newest open
// rows (queued, sending, failed, skipped): never an address, subject or body. Needs
// notify.outbox.manage. Fails errs.Unauthenticated, errs.Permission, the settings Reader's kind
// and store kinds.
func (s *Service) Outbox(ctx context.Context) (_ OutboxView, err error) {
	const op = "notify.Service.Outbox"
	ctx, span := s.start(ctx, "outbox")
	defer endSpan(span, &err)
	if _, err := s.require(ctx, op, PermissionOutboxManage); err != nil {
		return OutboxView{}, err
	}
	configured, err := s.Configured(ctx)
	if err != nil {
		return OutboxView{}, wrap(op, err)
	}
	counts, err := s.store.CountByStatus(ctx)
	if err != nil {
		return OutboxView{}, wrap(op, err)
	}
	full := map[Status]int{Queued: 0, Sending: 0, StatusSent: 0, Failed: 0, Skipped: 0}
	for st, n := range counts {
		full[st] = n
	}
	rows, err := s.store.ListOutbox(ctx, MaxOutboxList)
	if err != nil {
		return OutboxView{}, wrap(op, err)
	}
	recent := make([]Record, len(rows))
	for i, r := range rows {
		recent[i] = recordOf(r)
	}
	return OutboxView{Configured: configured, Counts: full, Recent: recent}, nil
}

// CheckConnection is Ping behind notify.outbox.manage, for the outbox screen's "check
// connection". Fails errs.Unauthenticated, errs.Permission and as Ping.
func (s *Service) CheckConnection(ctx context.Context) error {
	if _, err := s.require(ctx, "notify.Service.CheckConnection", PermissionOutboxManage); err != nil {
		return err
	}
	return s.Ping(ctx)
}

// Retry requeues a Failed row that still has its text, with its attempts reset. Needs
// notify.outbox.manage and records notify.message.retried. Fails errs.Unauthenticated,
// errs.Permission, errs.NotFound, errs.Conflict (field status: the row is not Failed (a
// Skipped row included), or
// its text has expired), errs.Unavailable wrapping db.ErrReadOnly, the audit error and
// store kinds.
func (s *Service) Retry(ctx context.Context, id MessageID) (err error) {
	const op = "notify.Service.Retry"
	ctx, span := s.start(ctx, "retry")
	defer endSpan(span, &err)
	if _, err := s.require(ctx, op, PermissionOutboxManage); err != nil {
		return err
	}
	if id.IsZero() {
		return notFound(op, "message")
	}
	err = s.db.Transact(ctx, db.Write, func(ctx context.Context) error {
		ok, err := s.store.Retry(ctx, id, s.now())
		if err != nil {
			return err //nolint:wrapcheck // wrapped below
		}
		if !ok {
			return s.retryRefused(ctx, op, id)
		}
		return s.audit.Record(ctx, audit.Entry{
			Action: actionMessageRetried, Outcome: audit.Succeeded, Record: audit.RefOf(kindMessage, id),
		})
	})
	return wrap(op, err)
}

// retryRefused says why a retry changed nothing: NotFound, or Conflict on the status (code
// "expired" for a Failed row whose text is gone, "conflict" for any other status).
func (s *Service) retryRefused(ctx context.Context, op string, id MessageID) error {
	st, err := s.store.StatusOf(ctx, id)
	if err != nil {
		return err //nolint:wrapcheck // wrapped by the caller
	}
	if st.Status == Failed && (!st.HasText || !st.ExpiresAt.After(s.now())) {
		return conflict(op, "expired", "this message has expired")
	}
	return conflict(op, codeConflict, "only a failed message can be retried")
}

// Cancel deletes a Queued or Failed row. Needs notify.outbox.manage and records
// notify.message.canceled. Fails as Retry (errs.Conflict, field status, for a Sending, Sent
// or Skipped row).
func (s *Service) Cancel(ctx context.Context, id MessageID) (err error) {
	const op = "notify.Service.Cancel"
	ctx, span := s.start(ctx, "cancel")
	defer endSpan(span, &err)
	if _, err := s.require(ctx, op, PermissionOutboxManage); err != nil {
		return err
	}
	if id.IsZero() {
		return notFound(op, "message")
	}
	err = s.db.Transact(ctx, db.Write, func(ctx context.Context) error {
		ok, err := s.store.Cancel(ctx, id)
		if err != nil {
			return err //nolint:wrapcheck // wrapped below
		}
		if !ok {
			if _, err := s.store.StatusOf(ctx, id); err != nil {
				return err //nolint:wrapcheck // wrapped below
			}
			return conflict(op, codeConflict, "only a queued or failed message can be canceled")
		}
		return s.audit.Record(ctx, audit.Entry{
			Action: actionMessageCanceled, Outcome: audit.Succeeded, Record: audit.RefOf(kindMessage, id),
		})
	})
	return wrap(op, err)
}

func conflict(op, code, reason string) error {
	return errs.E(op, errs.Conflict, &validate.FieldError{Field: "status", Code: code, Reason: reason})
}
