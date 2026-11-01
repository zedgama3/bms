package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/notify/message"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// scoped is a Notifier bound to one owner.
type scoped struct {
	s     *Service
	owner string
}

// Scoped returns the Notifier for one owner (module.Deps.Notify, the core parts' lazy
// adapters). Fails errs.Invalid for a malformed owner and errs.Internal after Run started.
func (s *Service) Scoped(owner string) (Notifier, error) {
	const op = "notify.Service.Scoped"
	if s.running.Load() {
		return nil, errs.E(op, errs.Internal, errors.New("Scoped after Run"))
	}
	if err := checkOwner(op, owner); err != nil {
		return nil, err
	}
	return scoped{s: s, owner: owner}, nil
}

// Send implements Notifier for the owner.
func (n scoped) Send(ctx context.Context, m Message) error {
	_, err := n.s.enqueue(ctx, &n.owner, "", m)
	return err
}

// Configured implements Notifier.
func (n scoped) Configured(ctx context.Context) (bool, error) { return n.s.Configured(ctx) }

// Configured reports whether Send would enqueue now: the SMTP settings were read and a host and
// a sender are set. It reads the settings (no SMTP connection) and looks at no recipient.
// Fails with the settings Reader's kind.
func (s *Service) Configured(ctx context.Context) (bool, error) {
	cfg, err := SMTPFromSettings(ctx, s.settings)
	if err != nil {
		return false, seamError("notify.Service.Configured", "the SMTP settings", err)
	}
	return cfg.Configured(), nil
}

// SendTo implements AddressSender: one message to one explicit address, in the site's
// default language. The row stores the address until it ends. Fails as Send, plus
// errs.Invalid (field "to") for a malformed address or a non-empty m.To.
func (s *Service) SendTo(ctx context.Context, to model.Email, m Message) error {
	const op = "notify.Service.SendTo"
	if err := to.Validate(); err != nil {
		return fieldError(op, "to", codeMalformed, nil, "must be an e-mail address")
	}
	_, err := s.enqueue(ctx, nil, to, m)
	return err
}

// target is one row to write: a person or an explicit address.
type target struct {
	person model.PersonID
	addr   model.Email
}

// enqueue is Send and SendTo. With owner nil the owner is the template's own (SendTo is
// unscoped). It returns the IDs of the rows inserted.
func (s *Service) enqueue(ctx context.Context, owner *string, addr model.Email, m Message) (ids []MessageID, err error) {
	const op = "notify.Service.Send"
	ctx, span := s.start(ctx, "send")
	defer endSpan(span, &err)

	if err := m.validate(op, addr == ""); err != nil {
		return nil, err
	}
	if mode, in := db.TxMode(ctx); in && mode == db.Read {
		return nil, errs.E(op, errs.Internal, errors.New("Send called inside a read transaction"))
	}
	configured, err := s.Configured(ctx)
	if err != nil {
		return nil, wrap(op, err)
	}
	if !configured {
		return nil, errs.E(op, errs.Unsupported, ErrNotConfigured)
	}
	if s.db.ReadOnly() {
		return nil, readOnlyErr(op)
	}
	who := m.Template.Owner()
	if owner != nil && *owner != who {
		return nil, fieldError(op, "template", codeScope, nil, "the template belongs to another owner")
	}
	def, ok := s.cat.get(m.Template)
	if !ok {
		return nil, fieldError(op, "template", codeUnknown, nil, "no such template is registered")
	}
	span.SetAttrs(telemetry.Token("template", string(m.Template)))
	m = m.clone()

	targets := make([]target, 0, len(m.To)+1)
	if addr != "" {
		targets = append(targets, target{addr: addr})
	}
	for _, p := range m.To {
		targets = append(targets, target{person: p})
	}
	span.SetAttrs(telemetry.Int("recipients", len(targets)))

	now := s.now()
	ttl := m.TTL
	if ttl == 0 {
		ttl = MaxAge
	}
	rc := newRenderCache(s, def)
	recs := make([]message.Record, 0, len(targets))
	for _, t := range targets {
		var (
			pl   Language
			name string
		)
		if !t.person.IsZero() {
			if pl, err = s.languages.LanguageOf(ctx, t.person); err != nil {
				return nil, seamError(op, "the language lookup", err)
			}
			if pl.Validate() != nil {
				pl = ""
			}
			if name, err = s.names.PlayaNameOf(ctx, t.person); err != nil {
				return nil, seamError(op, "the name lookup", err)
			}
		}
		r, lang, err := rc.render(ctx, m, pl, name)
		if err != nil {
			return nil, wrap(op, err)
		}
		rec := message.Record{
			ID: model.NewID[message.ID](), Owner: who, Template: string(m.Template), Key: m.Key,
			Person: t.person, Address: string(t.addr), Language: string(lang),
			Subject: r.Subject, Body: r.Body, CreatedAt: now, ExpiresAt: now.Add(ttl),
		}
		recs = append(recs, rec)
	}

	err = s.db.Transact(ctx, db.Write, func(ctx context.Context) error {
		inserted := 0
		for _, rec := range recs {
			ok, err := s.store.Enqueue(ctx, rec, now)
			if err != nil {
				return err //nolint:wrapcheck // wrapped below, once
			}
			if ok {
				inserted++
				ids = append(ids, rec.ID)
			}
		}
		if err := db.AfterCommit(ctx, func(ctx context.Context) { s.metrics.enqueue(ctx, who, inserted) }); err != nil {
			return fmt.Errorf("counting: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, wrap(op, err)
	}
	return ids, nil
}

// ---- rendering at enqueue ----------------------------------------------------------------

// renderCache memoizes, within one Send, the sites and the chosen templates by language.
type renderCache struct {
	s      *Service
	def    *definition
	sites  map[Language]Site
	chosen map[Language]*chosenTemplate // nil value = no template in that language
}

type chosenTemplate struct {
	c *compiled
}

func newRenderCache(s *Service, def *definition) *renderCache {
	return &renderCache{s: s, def: def, sites: map[Language]Site{}, chosen: map[Language]*chosenTemplate{}}
}

func (rc *renderCache) siteFor(ctx context.Context, lang Language) (Site, error) {
	if st, ok := rc.sites[lang]; ok {
		return st, nil
	}
	st, err := rc.s.site.Site(ctx, lang)
	if err != nil {
		return Site{}, seamError("notify.Site", "the site settings", err)
	}
	st.URL = strings.TrimRight(st.URL, "/")
	rc.sites[lang] = st
	return st, nil
}

// pick returns the template to use for lang: the override when there is one, else the
// shipped default, else nothing.
func (rc *renderCache) pick(ctx context.Context, lang Language) (*compiled, error) {
	if ch, ok := rc.chosen[lang]; ok {
		if ch == nil {
			return nil, nil
		}
		return ch.c, nil
	}
	c, err := rc.s.compiledFor(ctx, rc.def, lang)
	if err != nil {
		return nil, err
	}
	if c == nil {
		rc.chosen[lang] = nil
		return nil, nil
	}
	rc.chosen[lang] = &chosenTemplate{c: c}
	return c, nil
}

// compiledFor returns the compiled template of def in lang: an override that still parses
// and runs, else the shipped default; nil when neither exists.
func (s *Service) compiledFor(ctx context.Context, def *definition, lang Language) (*compiled, error) {
	o, err := s.store.Override(ctx, string(def.name), string(lang))
	switch {
	case err == nil:
		c, cerr := compile(TemplateText{Subject: o.Subject, Body: o.Body})
		if cerr == nil {
			if _, rerr := execute(c, placeholderData(def.vars, Site{}), nil); rerr == nil {
				return c, nil
			}
		}
		s.logger.WarnContext(ctx, "notify: an override does not run; using the shipped text",
			slog.String("template", string(def.name)), slog.String("language", string(lang)))
	case errs.Is(err, errs.NotFound):
	default:
		return nil, wrap("notify.override", err)
	}
	if sh, ok := def.langs[lang]; ok {
		return sh.c, nil
	}
	return nil, nil
}

// render renders m for one recipient whose saved language is personLang ("" when unknown).
// It returns the text and the language it is in.
func (rc *renderCache) render(ctx context.Context, m Message, personLang Language, playaName string) (Rendered, Language, error) {
	site, err := rc.siteFor(ctx, personLang)
	if err != nil {
		return Rendered{}, "", err
	}
	for _, lang := range candidates(personLang, site.DefaultLanguage) {
		c, err := rc.pick(ctx, lang)
		if err != nil {
			return Rendered{}, "", err
		}
		if c == nil {
			continue
		}
		if lang != personLang {
			if site, err = rc.siteFor(ctx, lang); err != nil {
				return Rendered{}, "", err
			}
		}
		link := ""
		if m.Link != "" {
			link = site.URL + m.Link
		}
		data := renderData{
			Site: siteData{Name: site.Name, URL: site.URL}, Recipient: recipientData{PlayaName: playaName},
			Link: link, Vars: m.Vars,
		}
		r, err := execute(c, data, site.Terms)
		return r, lang, err
	}
	// Register guarantees an "en" file, so this is a broken invariant.
	return Rendered{}, "", errs.E("notify.render", errs.Internal, errors.New("no template in any candidate language"))
}
