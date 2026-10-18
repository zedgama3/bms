package identity_test

import (
	"context"
	"fmt"
	"time"

	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/model"
)

// A service reads who is acting with PersonFrom. Tests put a session in the context with
// WithSession; only Authenticate does so in production.
func ExamplePersonFrom() {
	ctx := context.Background()
	if _, ok := identity.PersonFrom(ctx); !ok {
		fmt.Println("anonymous")
	}

	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	person := model.NewID[model.PersonID]()
	ctx = identity.WithSession(ctx, identity.Session{
		ID:        model.NewID[identity.SessionID](),
		Account:   model.NewID[model.AccountID](),
		Person:    person,
		IssuedAt:  now,
		ExpiresAt: now.Add(time.Hour),
	})
	got, ok := identity.PersonFrom(ctx)
	fmt.Println(ok, got == person)
	// Output:
	// anonymous
	// true true
}

// A session that must change its password is signed in to the accounts routes only, so
// PersonFrom refuses it while SessionFrom still returns it.
func ExampleSessionFrom_restricted() {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	ctx := identity.WithSession(context.Background(), identity.Session{
		ID:                     model.NewID[identity.SessionID](),
		Account:                model.NewID[model.AccountID](),
		Person:                 model.NewID[model.PersonID](),
		PasswordChangeRequired: true,
		IssuedAt:               now,
		ExpiresAt:              now.Add(time.Hour),
	})
	_, person := identity.PersonFrom(ctx)
	sess, signedIn := identity.SessionFrom(ctx)
	fmt.Println(person, signedIn, sess.PasswordChangeRequired)
	// Output:
	// false true true
}

// DefaultPolicy is what a deployment gets until settings supplies its own.
func ExampleDefaultPolicy() {
	p := identity.DefaultPolicy()
	fmt.Println(p.SignupMode, p.SessionLifetime, p.SuperuserSessionLifetime, p.PasswordMinLength)
	fmt.Println(p.LoginFailureLimit, p.ClientFailureLimit, p.LoginFailureWindow, p.Validate() == nil)
	// Output:
	// open 720h0m0s 1h0m0s 12
	// 10 100 15m0s true
}
