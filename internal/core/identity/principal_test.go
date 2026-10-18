package identity_test

import (
	"context"
	"testing"

	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/signals/signalstest"
	"github.com/zedgama3/bms/internal/model"
)

func TestSessionFrom_NoSession_False(t *testing.T) {
	if _, ok := identity.SessionFrom(t.Context()); ok {
		t.Error("SessionFrom ok without a session")
	}
	if _, ok := identity.PersonFrom(t.Context()); ok {
		t.Error("PersonFrom ok without a session")
	}
}

func TestWithSession_RoundTrip(t *testing.T) {
	s := sessionFor(model.NewID[model.AccountID](), model.NewID[model.PersonID](), epoch)
	ctx := identity.WithSession(t.Context(), s)
	got, ok := identity.SessionFrom(ctx)
	if !ok || got != s {
		t.Fatalf("SessionFrom = %+v, %v", got, ok)
	}
	p, ok := identity.PersonFrom(ctx)
	if !ok || p != s.Person {
		t.Fatalf("PersonFrom = %v, %v", p, ok)
	}
}

func TestWithSession_InvalidSession_PersonFromStillReturnsIt(t *testing.T) {
	s := identity.Session{Person: model.NewID[model.PersonID]()} // invalid: no ID, account or times
	p, ok := identity.PersonFrom(identity.WithSession(t.Context(), s))
	if !ok || p != s.Person {
		t.Fatalf("PersonFrom = %v, %v", p, ok)
	}
}

// F55
func TestPersonFrom_Restricted_False(t *testing.T) {
	s := sessionFor(model.NewID[model.AccountID](), model.NewID[model.PersonID](), epoch)
	s.PasswordChangeRequired = true
	ctx := identity.WithSession(t.Context(), s)
	if _, ok := identity.PersonFrom(ctx); ok {
		t.Error("PersonFrom ok for a restricted session")
	}
	got, ok := identity.SessionFrom(ctx)
	if !ok || !got.PasswordChangeRequired {
		t.Errorf("SessionFrom must still return the restricted session: %+v, %v", got, ok)
	}
}

// F54
func TestPersonFrom_InDelivery_False(t *testing.T) {
	s := sessionFor(model.NewID[model.AccountID](), model.NewID[model.PersonID](), epoch)
	bus, err := signalstest.NewFake("people", nil, []signals.Name{identity.SignalAccountCreated})
	wantNoErr(t, err)
	var inHandler, ok bool
	var sessionVisible bool
	wantNoErr(t, bus.Subscribe(identity.SignalAccountCreated, func(ctx context.Context, _ signals.Signal) error {
		inHandler = signals.InDelivery(ctx)
		_, ok = identity.PersonFrom(ctx)
		_, sessionVisible = identity.SessionFrom(ctx)
		return nil
	}))
	sig := signals.Signal{
		ID:   model.NewID[signals.SignalID](),
		Name: identity.SignalAccountCreated,
		At:   epoch,
		Payload: signals.NewPayload(
			signals.Ref(identity.PayloadAccount, s.Account),
			signals.Ref(identity.PayloadPerson, s.Person),
		),
	}
	wantNoErr(t, bus.Deliver(identity.WithSession(t.Context(), s), sig))
	if !inHandler {
		t.Fatal("handler not in delivery")
	}
	if ok {
		t.Error("PersonFrom ok inside a signal delivery")
	}
	_ = sessionVisible // SessionFrom's behaviour in delivery is not specified
}
