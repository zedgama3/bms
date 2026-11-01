// Package demotest gives tests fakes for core/demo's seams and for the demoseed.Actors a
// module's provider receives. Nothing here touches a database. All names, addresses and
// numbers are invented.
package demotest

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/zedgama3/bms/internal/contracts/demoseed"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/departments"
	"github.com/zedgama3/bms/internal/core/events"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// FakeActors implements demoseed.Actors without a database. Every actor key has a fixed
// account and person, so a test can predict who acted: As returns ctx carrying a session
// for that person (identity.PersonFrom reads it). Safe for concurrent use.
type FakeActors struct {
	mu   sync.Mutex
	used []string
}

var _ demoseed.Actors = (*FakeActors)(nil)

// NewFakeActors returns the fake. Cannot fail.
func NewFakeActors() *FakeActors { return &FakeActors{} }

// idOf builds a fixed non-zero UUID from a namespace byte and an index.
func idOf(ns byte, i int) [16]byte {
	return [16]byte{0xde, 0x30, ns, byte(i & 0xff), 0x40, 0x00, 0x80, 0x00, 0, 0, 0, 0, 0, 0, 0, byte((i + 1) & 0xff)}
}

func actorIndex(key string) (int, bool) {
	for i, k := range demoseed.ActorKeys {
		if k == key {
			return i, true
		}
	}
	return 0, false
}

// Person returns the fixed person of an actor key; false for an unknown key.
func (*FakeActors) Person(key string) (model.PersonID, bool) {
	i, ok := actorIndex(key)
	if !ok {
		return model.PersonID{}, false
	}
	return model.PersonID(idOf(1, i)), true
}

// Account returns the fixed account of an actor key; false for an unknown key.
func (*FakeActors) Account(key string) (model.AccountID, bool) {
	i, ok := actorIndex(key)
	if !ok {
		return model.AccountID{}, false
	}
	return model.AccountID(idOf(2, i)), true
}

// As returns ctx carrying the session of the named actor, valid for demo's SessionLifetime
// from the real clock. Fails errs.Invalid for an unknown key.
func (f *FakeActors) As(ctx context.Context, key string) (context.Context, error) {
	person, ok := f.Person(key)
	if !ok {
		return nil, errs.E("demotest.FakeActors.As", errs.Invalid, &validate.FieldError{Field: "actor", Code: validate.CodeOneOf, Reason: "unknown demo actor"})
	}
	acc, _ := f.Account(key)
	f.mu.Lock()
	f.used = append(f.used, key)
	f.mu.Unlock()
	now := time.Now()
	return identity.WithSession(ctx, identity.Session{
		ID: model.NewID[identity.SessionID](), Account: acc, Person: person, IssuedAt: now, ExpiresAt: now.Add(time.Hour),
	}), nil
}

// Used returns the keys As was called with, in order.
func (f *FakeActors) Used() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.used...)
}

// Request returns a complete demoseed.Request: a fixed event, the six departments, the ten
// actors and the twelve walk-ons, each with a fixed ID, and f as the Actors. The IDs are not
// stored anywhere: a provider run against fakes sees them as it would see real ones.
func (f *FakeActors) Request() demoseed.Request {
	req := demoseed.Request{
		Event:       model.EventID(idOf(3, 0)),
		Departments: map[string]model.DepartmentID{},
		People:      map[string]model.PersonID{},
		Actors:      f,
	}
	for i, k := range demoseed.DepartmentKeys {
		req.Departments[k] = model.DepartmentID(idOf(4, i))
	}
	for _, k := range demoseed.ActorKeys {
		req.People[k], _ = f.Person(k)
	}
	for n := 1; n <= demoseed.WalkOns; n++ {
		req.People[demoseed.WalkOnKey(n)] = model.PersonID(idOf(5, n))
	}
	return req
}

// ---- seams ----------------------------------------------------------------------------

// FakeAccounts is demo.Accounts: it returns a fixed account for each actor address.
type FakeAccounts struct {
	Err    error
	mu     sync.Mutex
	Emails []string
}

// EnsureAccount records the address and returns the fixed account of its actor key.
func (f *FakeAccounts) EnsureAccount(_ context.Context, c identity.Credentials) (identity.AccountResult, error) {
	if f.Err != nil {
		return identity.AccountResult{}, f.Err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Emails = append(f.Emails, string(c.Email))
	i := len(f.Emails) - 1
	return identity.AccountResult{Account: model.AccountID(idOf(2, i)), Person: model.PersonID(idOf(1, i)), Created: true}, nil
}

// FakeAccess is demo.Access: it knows the starter roles and records the grants.
type FakeAccess struct {
	Err     error
	mu      sync.Mutex
	Granted []authz.GrantInput
	Ensured []authz.GrantInput
}

// StarterRoleKeys are the roles ListRoles reports.
var StarterRoleKeys = []string{
	authz.RoleParticipant, authz.RoleAccountAdmin, authz.RoleAccessAdmin, authz.RoleDepartmentSteward,
	authz.RoleEventAdmin, authz.RoleSettingsAdmin, authz.RoleAuditReviewer, authz.RoleDepartmentAdmin,
	authz.RoleVolunteerCoordinator,
}

// EnsureGrant records the input and returns it as a new grant.
func (f *FakeAccess) EnsureGrant(_ context.Context, in authz.GrantInput) (authz.Grant, bool, error) {
	if f.Err != nil {
		return authz.Grant{}, false, f.Err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Ensured = append(f.Ensured, in)
	return authz.Grant{Person: in.Person, Role: in.Role, Department: in.Department}, true, nil
}

// Grant records the input and returns it as a new grant.
func (f *FakeAccess) Grant(_ context.Context, in authz.GrantInput) (authz.Grant, error) {
	if f.Err != nil {
		return authz.Grant{}, f.Err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Granted = append(f.Granted, in)
	return authz.Grant{Person: in.Person, Role: in.Role, Department: in.Department}, nil
}

// StarterRoleIDs returns a fixed ID for each requested key in StarterRoleKeys, and
// errs.NotFound for any other key.
func (f *FakeAccess) StarterRoleIDs(_ context.Context, keys []string) (map[string]authz.RoleID, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	out := make(map[string]authz.RoleID, len(keys))
	for _, k := range keys {
		i := slices.Index(StarterRoleKeys, k)
		if i < 0 {
			return nil, errs.E("demotest.FakeAccess.StarterRoleIDs", errs.NotFound, fmt.Errorf("no starter role %q", k))
		}
		out[k] = authz.RoleID(idOf(6, i))
	}
	return out, nil
}

// FakeAddresses is demo.Addresses over a map of login addresses.
type FakeAddresses struct {
	mu sync.Mutex
	by map[model.PersonID]model.Email
}

// Set sets person's login address.
func (f *FakeAddresses) Set(person model.PersonID, email model.Email) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.by == nil {
		f.by = map[model.PersonID]model.Email{}
	}
	f.by[person] = email
}

// EmailOf returns the address or errs.NotFound.
func (f *FakeAddresses) EmailOf(_ context.Context, person model.PersonID) (model.Email, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e, ok := f.by[person]; ok {
		return e, nil
	}
	return "", errs.E("demotest.FakeAddresses.EmailOf", errs.NotFound, fmt.Errorf("no account"))
}

// FakeProfiles is demo.Profiles: it returns a person and records the inputs.
type FakeProfiles struct {
	Err     error
	mu      sync.Mutex
	Updated []people.ProfileInput
	Created []people.ProfileInput
}

// UpdateMe records the input and returns the acting person.
func (f *FakeProfiles) UpdateMe(ctx context.Context, in people.ProfileInput) (people.Profile, error) {
	if f.Err != nil {
		return people.Profile{}, f.Err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Updated = append(f.Updated, in)
	p, _ := identity.PersonFrom(ctx)
	return people.Profile{Person: people.Person{ID: p}}, nil
}

// Create records the input and returns a new person.
func (f *FakeProfiles) Create(_ context.Context, in people.ProfileInput) (people.Profile, error) {
	if f.Err != nil {
		return people.Profile{}, f.Err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Created = append(f.Created, in)
	return people.Profile{Person: people.Person{ID: model.PersonID(idOf(7, len(f.Created)))}}, nil
}

// FakeDepartments is demo.Departments.
type FakeDepartments struct {
	Err     error
	mu      sync.Mutex
	Created []departments.CreateInput
}

// Create records the input and returns a department with a fixed ID.
func (f *FakeDepartments) Create(_ context.Context, in departments.CreateInput) (departments.Department, error) {
	if f.Err != nil {
		return departments.Department{}, f.Err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Created = append(f.Created, in)
	return departments.Department{ID: model.DepartmentID(idOf(4, len(f.Created)-1)), Name: in.Name}, nil
}

// FakeEvents is demo.Events.
type FakeEvents struct {
	Err      error
	mu       sync.Mutex
	Created  []events.CreateInput
	Schedule []events.ScheduleInput
}

// Create records the input and returns an event with a fixed ID.
func (f *FakeEvents) Create(_ context.Context, in events.CreateInput) (events.Created, error) {
	if f.Err != nil {
		return events.Created{}, f.Err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Created = append(f.Created, in)
	return events.Created{Event: events.Event{ID: model.EventID(idOf(3, 0)), Name: in.Name, Dates: in.Dates, Zone: in.Zone}}, nil
}

// AddScheduleItem records the input.
func (f *FakeEvents) AddScheduleItem(_ context.Context, e model.EventID, in events.ScheduleInput) (events.ScheduleItem, error) {
	if f.Err != nil {
		return events.ScheduleItem{}, f.Err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Schedule = append(f.Schedule, in)
	return events.ScheduleItem{Event: e, Key: in.Key, Label: in.Label, Starts: in.Starts, Ends: in.Ends}, nil
}

// FakeSettings is demo.Settings.
type FakeSettings struct {
	Err     error
	mu      sync.Mutex
	Changes []settings.Changes
}

// Set records the changes.
func (f *FakeSettings) Set(_ context.Context, ch settings.Changes) (settings.Revision, error) {
	if f.Err != nil {
		return 0, f.Err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Changes = append(f.Changes, ch)
	return settings.Revision(len(f.Changes)), nil
}

// FakeSchema is demo.Schema: it counts migrations.
type FakeSchema struct {
	Err   error
	mu    sync.Mutex
	Count int
}

// Migrate counts the call.
func (f *FakeSchema) Migrate(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Count++
	return f.Err
}

// FakeObjects is demo.Objects: it counts purges and reports Deleted objects each time.
type FakeObjects struct {
	Err     error
	Deleted int
	mu      sync.Mutex
	Count   int
}

// Purge counts the call.
func (f *FakeObjects) Purge(context.Context) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Count++
	if f.Err != nil {
		return 0, f.Err
	}
	return f.Deleted, nil
}
