package identity_test

import (
	"context"
	"testing"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/signals/signalstest"
	"github.com/zedgama3/bms/internal/model"
)

func TestActor_NoSession_IsFalse(t *testing.T) {
	if _, ok := identity.Actor(t.Context()); ok {
		t.Error("Actor ok without a session")
	}
	if _, ok := identity.AuditActor(t.Context()); ok {
		t.Error("AuditActor ok without a session")
	}
}

func TestActor_FullSession_IsThePerson(t *testing.T) {
	for _, super := range []bool{false, true} {
		s := sessionFor(model.NewID[model.AccountID](), model.NewID[model.PersonID](), epoch)
		s.Superuser = super
		got, ok := identity.Actor(identity.WithSession(t.Context(), s))
		if want := (authz.Actor{Person: s.Person, Superuser: super}); !ok || got != want {
			t.Errorf("superuser=%v: Actor = %+v, %v; want %+v, true", super, got, ok, want)
		}
	}
}

// A restricted session (a temporary password) is no actor for authz, even a superuser's.
func TestActor_RestrictedSession_IsFalse(t *testing.T) {
	for _, super := range []bool{false, true} {
		s := sessionFor(model.NewID[model.AccountID](), model.NewID[model.PersonID](), epoch)
		s.PasswordChangeRequired = true
		s.Superuser = super
		if got, ok := identity.Actor(identity.WithSession(t.Context(), s)); ok {
			t.Errorf("superuser=%v: Actor = %+v, true for a restricted session, want false", super, got)
		}
	}
}

// For audit a restricted session is still the person who did it.
func TestAuditActor_RestrictedSession_IsThePerson(t *testing.T) {
	for _, super := range []bool{false, true} {
		s := sessionFor(model.NewID[model.AccountID](), model.NewID[model.PersonID](), epoch)
		s.PasswordChangeRequired = true
		s.Superuser = super
		got, ok := identity.AuditActor(identity.WithSession(t.Context(), s))
		want := audit.Actor{Kind: audit.ActorPerson, Person: s.Person, Superuser: super}
		if !ok || got.Kind != want.Kind || got.Person != want.Person || got.Superuser != want.Superuser {
			t.Errorf("superuser=%v: AuditActor = %+v, %v; want %+v, true", super, got, ok, want)
		}
	}
}

func TestAuditActor_FullSession_IsThePerson(t *testing.T) {
	s := sessionFor(model.NewID[model.AccountID](), model.NewID[model.PersonID](), epoch)
	s.Superuser = true
	got, ok := identity.AuditActor(identity.WithSession(t.Context(), s))
	if !ok || got.Kind != audit.ActorPerson || got.Person != s.Person || !got.Superuser {
		t.Errorf("AuditActor = %+v, %v", got, ok)
	}
	if err := got.Validate(); err != nil {
		t.Errorf("the actor must validate: %v", err)
	}
}

// Actor follows PersonFrom's rule: inside a signal delivery the person is not the actor.
func TestActor_InDelivery_IsFalse(t *testing.T) {
	s := sessionFor(model.NewID[model.AccountID](), model.NewID[model.PersonID](), epoch)
	bus, err := signalstest.NewFake("people", nil, []signals.Name{identity.SignalAccountCreated})
	wantNoErr(t, err)
	var ran, ok bool
	wantNoErr(t, bus.Subscribe(identity.SignalAccountCreated, func(ctx context.Context, _ signals.Signal) error {
		ran = true
		_, ok = identity.Actor(ctx)
		return nil
	}))
	sig := signals.Signal{
		ID: model.NewID[signals.SignalID](), Name: identity.SignalAccountCreated, At: epoch,
		Payload: signals.NewPayload(signals.Ref(identity.PayloadAccount, s.Account), signals.Ref(identity.PayloadPerson, s.Person)),
	}
	wantNoErr(t, bus.Deliver(identity.WithSession(t.Context(), s), sig))
	if !ran {
		t.Fatal("handler did not run")
	}
	if ok {
		t.Error("Actor ok inside a signal delivery")
	}
}
