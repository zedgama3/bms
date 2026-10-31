package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/core/notify/channel"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// notifyPart is core part 9: the e-mail outbox, the SMTP channel and the templates every
// core area and module sends through (notify.md; app.md "How later core services plug in",
// row 9). It is built after settings, identity, people and authz, whose seams it takes;
// identity and people, built earlier, reach it through the lazy adapters below.
var notifyPart = part{
	name:    "notify",
	declare: declareNotify,
	build:   buildNotify,
	link:    linkNotify,
}

// notifyArea is the route set, audit scope and settings area of notify: /api/notify.
const notifyArea = "notify"

// notifySettings is the settings area of notify's keys, notify.*.
const notifySettings = notifyArea

// declareNotify adds the notify migration set and permissions. No I/O.
func declareNotify(a *assembly) error {
	if err := a.addCoreSet(notify.Set); err != nil {
		return err
	}
	a.corePerms = append(a.corePerms, notify.Permissions...)
	return nil
}

// notifyOwners are the core areas that hold a Notifier before notify exists: identity (the
// password reset and the superuser alert) and people (the deletion-request notice). Each is
// scoped in build, because Scoped fails once Run has started.
var notifyOwners = []string{accountsOwner, peopleArea}

// accountsOwner is the owner identity's templates and messages carry: "identity" (the
// settings and route area of identity is "accounts").
const accountsOwner = "identity"

// buildNotify constructs the service (no I/O: it succeeds against db.None() and on a
// read-only copy), registers the core areas' templates, scopes the Notifiers the earlier
// parts use, mounts the routes and registers the sender loop as a runnable.
func buildNotify(_ context.Context, a *assembly) error {
	const op = "app.notify.build"
	if a.deps.audit == nil || a.deps.authz == nil || a.settings == nil || a.identity == nil || a.people == nil {
		return errs.E(op, errs.Internal, errors.New("notify is built after audit, settings, identity, authz and people"))
	}
	rec, err := a.deps.audit.Scoped(notifyArea)
	if err != nil {
		return fmt.Errorf("%s: audit scope: %w", op, err)
	}
	reader, err := a.settings.Scoped(notifySettings)
	if err != nil {
		return fmt.Errorf("%s: settings scope: %w", op, err)
	}
	tr, m, err := a.scopes("notify")
	if err != nil {
		return err
	}
	svc, err := notify.New(notify.Options{
		DB:        a.dbc,
		Clock:     a.clock,
		Logger:    a.component("notify"),
		Tracer:    tr,
		Meter:     m,
		Audit:     rec,
		Authz:     a.deps.authz,
		Actor:     identity.Actor,
		SMTP:      smtpSource{r: reader},
		Site:      siteSource{svc: a.settings},
		Retention: retentionSource{r: reader},
		Addresses: addressSource{accounts: a.identity, contacts: a.people},
		Languages: languageSource{p: a.people},
		Names:     nameSource{p: a.people},
		Hostname:  a.cfg.Instance.Name,
		Channel:   demoChannelFactory(a), // nil unless demo mode: notify then sends over SMTP
	})
	if err != nil {
		return err //nolint:wrapcheck // notify.New names itself
	}
	for _, t := range []struct {
		owner string
		files fs.FS
	}{{accountsOwner, identity.Templates}, {peopleArea, people.Templates}} {
		if err := svc.Register(t.owner, t.files); err != nil {
			return fmt.Errorf("%s: templates of %s: %w", op, t.owner, err)
		}
	}
	a.notifiers = make(map[string]notify.Notifier, len(notifyOwners))
	for _, owner := range notifyOwners {
		if a.notifiers[owner], err = svc.Scoped(owner); err != nil {
			return fmt.Errorf("%s: scope %s: %w", op, owner, err)
		}
	}
	a.notify = svc
	a.deps.notify = svc
	if err := a.addCoreRoutes(notifyArea, svc.Routes(), svc.OpenAPI()); err != nil {
		return err
	}
	r := &notifyRunner{svc: svc, logger: a.component("notify")}
	a.runnables = append(a.runnables, runnable{name: "notify", start: r.start, stop: r.stop})
	return nil
}

// linkNotify registers the template defaults of every built module that ships any. It runs
// after module Build, so a module's templates are registered before Run starts; a bad
// template fails the build with errs.Invalid naming the file. The outbox joins a person's
// export and deletion in linkPeople, which calls people.Link once with every source.
func linkNotify(_ context.Context, a *assembly) error {
	const op = "app.notify.link"
	if a.notify == nil || a.plan == nil {
		return errs.E(op, errs.Internal, errors.New("notify is linked after it is built and the modules are resolved"))
	}
	for _, m := range a.plan.Order() {
		if m.Templates == nil {
			continue
		}
		if err := a.notify.Register(m.Name, m.Templates); err != nil {
			return fmt.Errorf("%s: templates of module %s: %w", op, m.Name, err)
		}
	}
	return nil
}

// senderLoop is notify.Service's Run and Close, the two calls the runnable makes.
type senderLoop interface {
	Run(ctx context.Context) error
	Close(ctx context.Context) error
}

var _ senderLoop = (*notify.Service)(nil)

// notifyRunner runs notify.Service.Run in a goroutine of its own: start launches it, stop
// closes the service (which cancels a batch in flight and gives its rows back) and waits.
type notifyRunner struct {
	svc    senderLoop
	logger *slog.Logger

	cancel context.CancelFunc
	done   chan struct{}
}

// start launches Run. The goroutine exits when stop ends it; a failure (Run fails only when
// started twice) is logged there, once.
func (r *notifyRunner) start(ctx context.Context) error {
	// Run outlives start's own deadline: it stops on stop.
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	r.cancel = cancel
	r.done = make(chan struct{})
	go func() {
		defer close(r.done)
		if err := r.svc.Run(runCtx); err != nil {
			r.logger.ErrorContext(runCtx, "the notify sender stopped",
				slog.String("error.kind", errs.KindOf(err).String()))
		}
	}()
	return nil
}

// stop ends Run and waits for it. It fails with ctx's kind when ctx ends first.
func (r *notifyRunner) stop(ctx context.Context) error {
	const op = "app.notify.runner.stop"
	if r.cancel == nil {
		return nil
	}
	if err := r.svc.Close(ctx); err != nil {
		r.cancel()
		return fmt.Errorf("%s: %w", op, err)
	}
	r.cancel()
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return errs.E(op, errs.KindOf(ctx.Err()), fmt.Errorf("the notify sender did not stop: %w", ctx.Err()))
	}
}

// ---- adapters over settings ------------------------------------------------------------

// smtpSource is notify's SMTPSource over the "notify" area's scoped Reader. The password
// is the sealed smtp_password, unsealed per read and never logged.
type smtpSource struct{ r settings.Reader }

var _ notify.SMTPSource = smtpSource{}

// SMTP reads the notify.* SMTP keys. It fails with the Reader's kinds, and errs.Invalid for
// a stored value notify cannot read (settings validated it, so that is a bug).
func (s smtpSource) SMTP(ctx context.Context) (notify.SMTPConfig, error) {
	const op = "app.smtpSource.SMTP"
	var (
		cfg  notify.SMTPConfig
		text = func(key string) (string, error) {
			v, err := s.r.Text(ctx, key)
			if err != nil {
				return "", fmt.Errorf("%s: notify.%s: %w", op, key, err)
			}
			return v, nil
		}
		err error
	)
	if cfg.Host, err = text("smtp_host"); err != nil {
		return notify.SMTPConfig{}, err
	}
	port, err := s.r.Int(ctx, "smtp_port")
	if err != nil {
		return notify.SMTPConfig{}, fmt.Errorf("%s: notify.smtp_port: %w", op, err)
	}
	cfg.Port = int(port)
	sec, err := text("smtp_security")
	if err != nil {
		return notify.SMTPConfig{}, err
	}
	if cfg.Security, err = channel.ParseSecurity(sec); err != nil {
		return notify.SMTPConfig{}, fmt.Errorf("%s: %w", op, err)
	}
	if cfg.Username, err = text("smtp_username"); err != nil {
		return notify.SMTPConfig{}, err
	}
	if cfg.Password, err = s.r.Secret(ctx, "smtp_password"); err != nil {
		return notify.SMTPConfig{}, fmt.Errorf("%s: notify.smtp_password: %w", op, err)
	}
	sender, err := text("sender_address")
	if err != nil {
		return notify.SMTPConfig{}, err
	}
	if cfg.Sender.Email, err = notify.ParseAddress(sender); err != nil {
		return notify.SMTPConfig{}, fmt.Errorf("%s: notify.sender_address: %w", op, err)
	}
	if cfg.Sender.Name, err = text("sender_name"); err != nil {
		return notify.SMTPConfig{}, err
	}
	reply, err := text("reply_to")
	if err != nil {
		return notify.SMTPConfig{}, err
	}
	replyTo, err := notify.ParseAddress(reply)
	if err != nil {
		return notify.SMTPConfig{}, fmt.Errorf("%s: notify.reply_to: %w", op, err)
	}
	if replyTo != "" {
		cfg.ReplyTo = &notify.Address{Email: replyTo}
	}
	alerts, err := text("alert_addresses")
	if err != nil {
		return notify.SMTPConfig{}, err
	}
	if cfg.AlertAddresses, err = notify.ParseAddresses(alerts); err != nil {
		return notify.SMTPConfig{}, fmt.Errorf("%s: notify.alert_addresses: %w", op, err)
	}
	return cfg, nil
}

// retentionSource is notify's RetentionSource over the "notify" area's scoped Reader.
type retentionSource struct{ r settings.Reader }

var _ notify.RetentionSource = retentionSource{}

// Retention reads notify.retention. It fails with the Reader's kinds.
func (s retentionSource) Retention(ctx context.Context) (time.Duration, error) {
	d, err := s.r.Duration(ctx, "retention")
	if err != nil {
		return 0, fmt.Errorf("app.retentionSource.Retention: notify.retention: %w", err)
	}
	return d, nil
}

// regionalSource is the part of *settings.Service siteSource reads.
type regionalSource interface {
	Regional(ctx context.Context) (settings.Regional, error)
	TermsFor(ctx context.Context, lang string) (map[string]settings.Forms, error)
}

var _ regionalSource = (*settings.Service)(nil)

// siteSource is notify's SiteSource over settings' Regional and TermsFor.
type siteSource struct{ svc regionalSource }

var _ notify.SiteSource = siteSource{}

// Site answers the regional's name, public URL, default language and the terms in lang: ""
// and a language the build does not ship (neither itself nor its primary subtag) use the
// regional's default language. Each term is under its singular key, and under key_plural,
// key_title and key_plural_title for its other forms. It fails with the settings' kinds.
func (s siteSource) Site(ctx context.Context, lang notify.Language) (notify.Site, error) {
	const op = "app.siteSource.Site"
	reg, err := s.svc.Regional(ctx)
	if err != nil {
		return notify.Site{}, fmt.Errorf("%s: %w", op, err)
	}
	use := string(lang)
	if !slices.Contains(settings.Languages, use) {
		use, _, _ = strings.Cut(use, "-")
	}
	if !slices.Contains(settings.Languages, use) {
		use = reg.Language
	}
	forms, err := s.svc.TermsFor(ctx, use)
	if err != nil {
		return notify.Site{}, fmt.Errorf("%s: terms in %s: %w", op, use, err)
	}
	terms := make(map[string]string, 4*len(forms))
	for key, f := range forms {
		terms[key] = f.Singular
		terms[key+"_plural"] = f.Plural
		terms[key+"_title"] = f.SingularTitle
		terms[key+"_plural_title"] = f.PluralTitle
	}
	return notify.Site{Name: reg.Name, URL: reg.PublicURL, DefaultLanguage: notify.Language(reg.Language), Terms: terms}, nil
}

// ---- adapters over identity and people --------------------------------------------------

// accountEmails is identity's EmailOf and contactEmails people's ContactEmailOf: the two
// places an address is.
type (
	accountEmails interface {
		EmailOf(ctx context.Context, person model.PersonID) (model.Email, error)
	}
	contactEmails interface {
		ContactEmailOf(ctx context.Context, person model.PersonID) (model.Email, error)
	}
)

var (
	_ accountEmails = (*identity.Service)(nil)
	_ contactEmails = (*people.Service)(nil)
)

// addressSource is notify's AddressSource: the login address of the person's account, else
// the contact address of the profile.
type addressSource struct {
	accounts accountEmails
	contacts contactEmails
}

var _ notify.AddressSource = addressSource{}

// AddressOf asks identity first. A person without an account (NotFound) or an account with
// no address is asked of people. It fails errs.NotFound when neither holds one (notify then
// ends the message as undeliverable) and with the store kinds otherwise.
func (s addressSource) AddressOf(ctx context.Context, person model.PersonID) (model.Email, error) {
	const op = "app.addressSource.AddressOf"
	to, err := s.accounts.EmailOf(ctx, person)
	switch {
	case err == nil && to != "":
		return to, nil
	case err != nil && !errs.Is(err, errs.NotFound):
		return "", fmt.Errorf("%s: account: %w", op, err)
	}
	to, err = s.contacts.ContactEmailOf(ctx, person)
	if err != nil {
		return "", fmt.Errorf("%s: contact: %w", op, err)
	}
	return to, nil
}

// languageSource is notify's LanguageSource over people's LanguageOf: a person with no
// profile has no language.
type languageSource struct {
	p interface {
		LanguageOf(ctx context.Context, person model.PersonID) (string, error)
	}
}

var _ notify.LanguageSource = languageSource{}

// LanguageOf returns the saved language tag, "" for NotFound, and the store's kinds
// otherwise.
func (s languageSource) LanguageOf(ctx context.Context, person model.PersonID) (notify.Language, error) {
	l, err := s.p.LanguageOf(ctx, person)
	switch {
	case errs.Is(err, errs.NotFound):
		return "", nil
	case err != nil:
		return "", fmt.Errorf("app.languageSource.LanguageOf: %w", err)
	}
	return notify.Language(l), nil
}

// nameSource is notify's NameSource over people's PlayaNameOf: a person with no profile has
// no name.
type nameSource struct {
	p interface {
		PlayaNameOf(ctx context.Context, person model.PersonID) (string, error)
	}
}

var _ notify.NameSource = nameSource{}

// PlayaNameOf returns the playa name, "" for NotFound, and the store's kinds otherwise.
func (s nameSource) PlayaNameOf(ctx context.Context, person model.PersonID) (string, error) {
	n, err := s.p.PlayaNameOf(ctx, person)
	switch {
	case errs.Is(err, errs.NotFound):
		return "", nil
	case err != nil:
		return "", fmt.Errorf("app.nameSource.PlayaNameOf: %w", err)
	}
	return n, nil
}

// ---- the lazy Notifier ------------------------------------------------------------------

// lazyNotifier is the Notifier of a part built before notify (identity, people): it resolves
// the owner's Notifier, which buildNotify scoped, when called. Until notify is built Send
// fails errs.Unsupported wrapping notify.ErrNotConfigured and Configured is false, which is
// what both callers read as "no e-mail here". It does no I/O of its own.
type lazyNotifier struct {
	a     *assembly
	owner string
}

var _ notify.Notifier = lazyNotifier{}

func (l lazyNotifier) resolve() (notify.Notifier, bool) {
	n, ok := l.a.notifiers[l.owner]
	return n, ok
}

// Send forwards to the owner's Notifier.
func (l lazyNotifier) Send(ctx context.Context, m notify.Message) error {
	n, ok := l.resolve()
	if !ok {
		return errs.E("app.lazyNotifier.Send", errs.Unsupported, notify.ErrNotConfigured)
	}
	return n.Send(ctx, m) //nolint:wrapcheck // the notifier's own error
}

// Configured forwards to the owner's Notifier.
func (l lazyNotifier) Configured(ctx context.Context) (bool, error) {
	n, ok := l.resolve()
	if !ok {
		return false, nil
	}
	return n.Configured(ctx) //nolint:wrapcheck // the notifier's own error
}

// ---- the outbox in a person's export and deletion ----------------------------------------

// notifyData adapts notify's ExportPerson and DeletePerson to people's PersonData (people
// cannot import notify's twin types, and notify cannot import people).
type notifyData struct{ svc personOutbox }

// personOutbox is notify.Service's ExportPerson and DeletePerson.
type personOutbox interface {
	ExportPerson(ctx context.Context, person model.PersonID) (json.RawMessage, error)
	DeletePerson(ctx context.Context, person model.PersonID) (int, error)
}

var (
	_ personOutbox      = (*notify.Service)(nil)
	_ people.PersonData = notifyData{}
)

// notifySource is the "notify" source of people's export and deletion: the outbox rows of
// the person.
func notifySource(svc personOutbox) people.Source {
	return people.Source{Name: "notify", Exporter: notifyData{svc: svc}}
}

// ExportPerson is notify's export (the person's outbox rows, never a body or an address) as
// people's Export.
func (n notifyData) ExportPerson(ctx context.Context, person model.PersonID) (people.Export, error) {
	raw, err := n.svc.ExportPerson(ctx, person)
	if err != nil {
		return people.Export{}, err //nolint:wrapcheck // people names the source
	}
	return people.Export{Data: raw}, nil
}

// DeletePerson deletes the person's outbox rows and counts them.
func (n notifyData) DeletePerson(ctx context.Context, person model.PersonID) (people.ModuleDeletion, error) {
	deleted, err := n.svc.DeletePerson(ctx, person)
	if err != nil {
		return people.ModuleDeletion{}, err //nolint:wrapcheck // people names the source
	}
	return people.ModuleDeletion{Deleted: deleted}, nil
}
