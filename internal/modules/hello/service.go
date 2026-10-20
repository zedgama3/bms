package hello

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/zedgama3/bms/internal/contracts/greeter"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/hello/greeting"
)

// store is what the service needs from storage. postgres.Store implements it. No SQL or
// driver types here (D8).
type store interface {
	Insert(ctx context.Context, g greeting.Greeting) error
	CountByPerson(ctx context.Context, person model.PersonID) (int, error)
	ListByPerson(ctx context.Context, person model.PersonID) ([]greeting.Greeting, error)
	DeleteByPerson(ctx context.Context, person model.PersonID) (int, error)
}

// actorFunc reads the signed-in person from ctx: identity.PersonFrom (M1), which is false
// with no session, for a restricted session and inside a signal delivery.
type actorFunc func(ctx context.Context) (model.PersonID, bool)

// permissionChecker decides one permission for the actor in ctx; the same shape as
// api.PermissionChecker and authz's checker (M2). M0: denies everything with
// errs.Permission.
type permissionChecker interface {
	Allow(ctx context.Context, p authz.Permission) error
}

// greetingSource returns the current greeting word. M0: DefaultGreeting. M2:
// Deps.Settings for SettingGreeting.
type greetingSource func(ctx context.Context) (string, error)

// PublicGreeting is GET /public-greeting's body.
type PublicGreeting struct {
	Greeting string   `json:"greeting"`
	Banners  []Banner `json:"banners"` // never null: an empty array when nobody contributes
}

// Banner is one contribution, as the screen needs it.
type Banner struct {
	Module string `json:"module"` // Contribution.Module
	Key    string `json:"key"`    // greeter.Banner.Key
}

// Greeting is GET /greeting's body.
type Greeting struct {
	Greeting string `json:"greeting"`
	Mine     int    `json:"mine"` // the actor's own rows
	Seen     int    `json:"seen"` // hello.greeted deliveries this process counted since Start
}

type service struct {
	store    store
	actor    actorFunc
	allow    permissionChecker
	greeting greetingSource
	links    *module.Links
	bus      signals.Bus
	notify   notify.Notifier
	db       db.DB
	tracer   telemetry.Tracer
	logger   *slog.Logger
	clock    clock.Clock
	counter  *counter
}

// endSpan records *err on span (when set) and ends it. Use with defer and a named result.
func endSpan(span telemetry.Span, err *error) {
	if *err != nil {
		span.RecordError(*err)
	}
	span.End()
}

// authorize is the first step of every signed-in use case: the actor, then the permission.
func (s *service) authorize(ctx context.Context, op string, p authz.Permission) (model.PersonID, error) {
	actor, ok := s.actor(ctx)
	if !ok || actor.IsZero() {
		return model.PersonID{}, errs.E(op, errs.Unauthenticated, errNoActor)
	}
	if err := s.allow.Allow(ctx, p); err != nil {
		return model.PersonID{}, fmt.Errorf("%s: %w", op, err)
	}
	return actor, nil
}

// banners returns every contribution to PointBanner. Fails errs.Internal when Links
// refuses the point (a bug: it is declared) or a value is not a greeter.Banner (Build's
// Accepts check makes that impossible).
func (s *service) banners() ([]Banner, error) {
	const op = "hello.banners"
	cs, err := s.links.Contributors(PointBanner)
	if err != nil {
		return nil, errs.E(op, errs.Internal, err)
	}
	out := make([]Banner, 0, len(cs))
	for _, c := range cs {
		b, ok := c.Value.(greeter.Banner)
		if !ok {
			return nil, errs.E(op, errs.Internal, fmt.Errorf("a contribution from module %s is not a greeter.Banner", c.Module))
		}
		out = append(out, Banner{Module: c.Module, Key: b.Key})
	}
	return out, nil
}

// PublicGreeting is GET /public-greeting: the greeting word and the banners every
// contributor registered. No actor, no permission: nothing about a person is read.
// Span "hello.public-greeting" with Int("banners"). Fails with the greetingSource's kind
// or errs.Internal when Links refuses PointBanner (a bug: it is declared).
func (s *service) PublicGreeting(ctx context.Context) (out PublicGreeting, err error) {
	const op = "hello.PublicGreeting"
	ctx, span := s.tracer.Start(ctx, "hello.public-greeting")
	defer endSpan(span, &err)
	word, err := s.greeting(ctx)
	if err != nil {
		return PublicGreeting{}, fmt.Errorf("%s: %w", op, err)
	}
	banners, err := s.banners()
	if err != nil {
		return PublicGreeting{}, fmt.Errorf("%s: %w", op, err)
	}
	span.SetAttrs(telemetry.Int("banners", len(banners)))
	return PublicGreeting{Greeting: word, Banners: banners}, nil
}

// Greeting is GET /greeting: the greeting word, how many times the actor has greeted
// (own rows only) and how many hello.greeted deliveries this process has seen. Needs an
// actor (errs.Unauthenticated) and PermissionGreetingsRead (errs.Permission). Span
// "hello.greeting" with Int("mine"), Int("seen"). Store failures keep db's kind.
func (s *service) Greeting(ctx context.Context) (out Greeting, err error) {
	const op = "hello.Greeting"
	actor, err := s.authorize(ctx, op, PermissionGreetingsRead)
	if err != nil {
		return Greeting{}, err
	}
	ctx, span := s.tracer.Start(ctx, "hello.greeting")
	defer endSpan(span, &err)
	word, err := s.greeting(ctx)
	if err != nil {
		return Greeting{}, fmt.Errorf("%s: %w", op, err)
	}
	mine, err := s.store.CountByPerson(ctx, actor)
	if err != nil {
		return Greeting{}, fmt.Errorf("%s: count: %w", op, err)
	}
	seen := s.counter.Seen()
	span.SetAttrs(telemetry.Int("mine", mine), telemetry.Int("seen", seen))
	return Greeting{Greeting: word, Mine: mine, Seen: seen}, nil
}

// Greet is POST /greetings: in one db.Transact(Write), insert a greeting for the actor
// and publish SignalGreeted {person: actor}; delivery happens after commit. Needs an actor
// (errs.Unauthenticated) and PermissionGreetingsWrite (errs.Permission). Span
// "hello.greet". On a read-only copy fails errs.Unavailable wrapping db.ErrReadOnly; a
// refused Publish rolls the insert back and its kind is returned. After the Publish, still
// inside the transaction, it queues TemplateGreeted to the actor through Deps.Notify: when
// e-mail is not set up (errs.Unsupported wrapping notify.ErrNotConfigured) the greeting is
// kept and one debug line says so; any other Send error rolls the insert back, publishes
// nothing and is returned with its kind.
func (s *service) Greet(ctx context.Context) (g greeting.Greeting, err error) {
	const op = "hello.Greet"
	actor, err := s.authorize(ctx, op, PermissionGreetingsWrite)
	if err != nil {
		return greeting.Greeting{}, err
	}
	ctx, span := s.tracer.Start(ctx, "hello.greet")
	defer endSpan(span, &err)
	word, err := s.greeting(ctx)
	if err != nil {
		return greeting.Greeting{}, fmt.Errorf("%s: %w", op, err)
	}
	g = greeting.Greeting{
		ID:        model.NewID[greeting.ID](),
		Person:    actor,
		Text:      word,
		CreatedAt: s.clock.Now().UTC().Truncate(time.Microsecond),
	}
	if err := g.Validate(); err != nil {
		return greeting.Greeting{}, fmt.Errorf("%s: %w", op, err)
	}
	err = s.db.Transact(ctx, db.Write, func(ctx context.Context) error {
		if err := s.store.Insert(ctx, g); err != nil {
			return fmt.Errorf("insert: %w", err)
		}
		if err := s.bus.Publish(ctx, SignalGreeted, greetedPayload(actor)); err != nil {
			return fmt.Errorf("publish: %w", err)
		}
		if err := s.notifyGreeted(ctx, actor, g.ID, word); err != nil {
			return fmt.Errorf("notify: %w", err)
		}
		return nil
	})
	if err != nil {
		return greeting.Greeting{}, fmt.Errorf("%s: %w", op, err)
	}
	return g, nil
}

// notifyGreeted queues the TemplateGreeted notice to the greeter inside Greet's
// transaction. "E-mail is not set up" is not the greeter's problem: it is logged once at
// debug level (no ID of anything) and returns nil; every other Send error is returned.
func (s *service) notifyGreeted(ctx context.Context, actor model.PersonID, id greeting.ID, word string) error {
	err := s.notify.Send(ctx, notify.Message{
		Template: TemplateGreeted,
		To:       []model.PersonID{actor},
		Key:      "greeted:" + id.String(),
		Vars:     map[string]string{"word": word},
		Link:     "/hello/",
	})
	if errors.Is(err, notify.ErrNotConfigured) {
		s.logger.DebugContext(ctx, "greeting saved without a notice", slog.String("notify", "off"))
		return nil
	}
	if err != nil {
		return fmt.Errorf("send %s: %w", TemplateGreeted, err)
	}
	return nil
}
