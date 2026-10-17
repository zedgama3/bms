package hello

// This file holds the test-only exports the black-box tests (package hello_test) use to
// reach the unexported seams, and a few white-box tests of helpers the spec names no test
// for. Everything here is compiled only into the test binary.

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/hello/greeting"
)

// FakeStore is an in-memory store. Fail makes one method return an error, to test the
// store-failure rows of the spec. Safe for concurrent use.
type FakeStore struct {
	mu   sync.Mutex
	rows []greeting.Greeting
	fail map[string]error
}

var _ store = (*FakeStore)(nil)

// NewFakeStore returns an empty FakeStore.
func NewFakeStore() *FakeStore { return &FakeStore{fail: map[string]error{}} }

// Fail makes method ("Insert", "CountByPerson", "ListByPerson" or "DeleteByPerson") return
// err from now on; a nil err clears it.
func (f *FakeStore) Fail(method string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err == nil {
		delete(f.fail, method)
		return
	}
	f.fail[method] = err
}

// Rows returns a copy of every stored greeting.
func (f *FakeStore) Rows() []greeting.Greeting {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.rows)
}

// Insert stores g; a duplicate ID is errs.Conflict.
func (f *FakeStore) Insert(_ context.Context, g greeting.Greeting) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail["Insert"]; err != nil {
		return err
	}
	for _, r := range f.rows {
		if r.ID == g.ID {
			return errs.E("hello.fake.Insert", errs.Conflict, errors.New("duplicate id"))
		}
	}
	f.rows = append(f.rows, g)
	return nil
}

// CountByPerson counts the person's rows.
func (f *FakeStore) CountByPerson(_ context.Context, person model.PersonID) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail["CountByPerson"]; err != nil {
		return 0, err
	}
	n := 0
	for _, r := range f.rows {
		if r.Person == person {
			n++
		}
	}
	return n, nil
}

// ListByPerson returns the person's rows in insertion order.
func (f *FakeStore) ListByPerson(_ context.Context, person model.PersonID) ([]greeting.Greeting, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail["ListByPerson"]; err != nil {
		return nil, err
	}
	out := []greeting.Greeting{}
	for _, r := range f.rows {
		if r.Person == person {
			out = append(out, r)
		}
	}
	return out, nil
}

// DeleteByPerson removes the person's rows and returns how many.
func (f *FakeStore) DeleteByPerson(_ context.Context, person model.PersonID) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail["DeleteByPerson"]; err != nil {
		return 0, err
	}
	before := len(f.rows)
	f.rows = slices.DeleteFunc(f.rows, func(r greeting.Greeting) bool { return r.Person == person })
	return before - len(f.rows), nil
}

// AllowAll is a permissionChecker that grants every permission.
type AllowAll struct{}

// Allow always returns nil.
func (AllowAll) Allow(context.Context, authz.Permission) error { return nil }

// DenyAll is a permissionChecker that refuses every permission with errs.Permission.
type DenyAll = denyAll

// Seams is the injectable part of the module: what New wires to the M0 defaults. A nil
// Actor means "no actor", a nil Allow means "deny", a nil Greeting means the declared
// default, and a nil Store means a fresh FakeStore.
type Seams struct {
	Store interface {
		Insert(ctx context.Context, g greeting.Greeting) error
		CountByPerson(ctx context.Context, person model.PersonID) (int, error)
		ListByPerson(ctx context.Context, person model.PersonID) ([]greeting.Greeting, error)
		DeleteByPerson(ctx context.Context, person model.PersonID) (int, error)
	}
	Actor func(ctx context.Context) (model.PersonID, bool)
	Allow interface {
		Allow(ctx context.Context, p authz.Permission) error
	}
	Greeting func(ctx context.Context) (string, error)
}

// NewWithSeams is New over injected seams (the production New cannot be given a fake
// store). It fails as newModule does, with errs.Invalid for a nil dependency in deps.
func NewWithSeams(deps module.Deps, s Seams) (*Module, error) {
	sm := seams{store: s.Store, actor: s.Actor, allow: s.Allow, greeting: s.Greeting}
	if sm.store == nil {
		sm.store = NewFakeStore()
	}
	if sm.actor == nil {
		sm.actor = noActor
	}
	if sm.allow == nil {
		sm.allow = denyAll{}
	}
	if sm.greeting == nil {
		sm.greeting = declaredDefault
	}
	return newModule(deps, sm)
}

// Service is the unexported service, for tests that call a use case directly.
type Service = service

// Service returns the module's service.
func (m *Module) Service() *Service { return m.svc }

// HandleGreeted calls the SignalGreeted subscriber directly.
func (m *Module) HandleGreeted(ctx context.Context, sig signals.Signal) error {
	return m.svc.handleGreeted(ctx, sig)
}

// Seen returns the counter's count.
func (m *Module) Seen() int { return m.svc.counter.Seen() }

func TestValidateGreeting_Table_AcceptsAndRejects(t *testing.T) {
	long := make([]byte, MaxGreetingBytes+1)
	for i := range long {
		long[i] = 'a'
	}
	tests := []struct {
		name string
		in   string
		ok   bool
	}{
		{"default", DefaultGreeting, true},
		{"padded", "  howdy  ", true},
		{"max", string(long[:MaxGreetingBytes]), true},
		{"empty", "", false},
		{"blank", "   ", false},
		{"too long", string(long), false},
		{"control", "he\tllo", false},
		{"newline inside", "he\nllo", false},
		{"bad utf8", "\xff\xfe", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateGreeting(tc.in)
			if tc.ok && err != nil {
				t.Fatalf("validateGreeting(%q) = %v, want nil", tc.in, err)
			}
			if !tc.ok && !errs.Is(err, errs.Invalid) {
				t.Fatalf("validateGreeting(%q) = %v, want Invalid", tc.in, err)
			}
		})
	}
}

func TestValidBannerKey_Table_ChecksPrefixAndIdent(t *testing.T) {
	tests := []struct {
		contributor, key string
		want             bool
	}{
		{"hello", "module.hello.banner.welcome", true},
		{"hello", "module.hello.banner.two-words", true},
		{"hello", "module.other.banner.welcome", false},
		{"hello", "module.hello.banner.", false},
		{"hello", "module.hello.banner.Welcome", false},
		{"hello", "module.hello.banner.a.b", false},
		{"hello", "module.hello.banner.-a", false},
		{"hello", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			if got := validBannerKey(tc.contributor, tc.key); got != tc.want {
				t.Fatalf("validBannerKey(%q, %q) = %v, want %v", tc.contributor, tc.key, got, tc.want)
			}
		})
	}
}

func TestCounter_RingForgetsOldestAfterSeenRing(t *testing.T) {
	c := newCounter()
	ids := make([]signals.SignalID, seenRing+1)
	for i := range ids {
		ids[i] = model.NewID[signals.SignalID]()
		c.record(ids[i])
	}
	if got := c.Seen(); got != seenRing+1 {
		t.Fatalf("Seen = %d, want %d", got, seenRing+1)
	}
	c.record(ids[len(ids)-1]) // still remembered: not counted again
	if got := c.Seen(); got != seenRing+1 {
		t.Fatalf("recent repeat counted: Seen = %d, want %d", got, seenRing+1)
	}
	c.record(ids[0]) // evicted: counted again
	if got := c.Seen(); got != seenRing+2 {
		t.Fatalf("evicted repeat not counted: Seen = %d, want %d", got, seenRing+2)
	}
}
