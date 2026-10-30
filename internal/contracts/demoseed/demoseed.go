// Package demoseed is the contract between the core demo seed and the modules that add
// their own demo data (demo.md, D53). It holds no implementation (07-abstractions,
// "contracts"), so a module imports this package and never core/demo. It imports only
// model and lib.
package demoseed

import (
	"context"
	"fmt"

	"github.com/zedgama3/bms/internal/model"
)

// Point is the extension point the core defines; a module lists it in
// Manifest.ContributesTo and returns a Provider from Contributions(). app declares it in
// Env.CoreExtensionPoints with Accepts = func(v any) bool { _, ok := v.(Provider); return ok }.
const Point = "demo.seed"

// Actor keys. Request.People holds a PersonID for each; Actors.As accepts each.
const (
	ActorParticipant   = "participant"    // base role only; booked on shifts
	ActorNewcomer      = "newcomer"       // base role only; empty profile (the first-run flow)
	ActorLead          = "lead"           // department-admin at Rangers, Gate, Greeters, Medical and DPW
	ActorCoordinator   = "coordinator"    // volunteer-coordinator
	ActorSteward       = "steward"        // department-steward
	ActorEventAdmin    = "event-admin"    // event-admin
	ActorSettingsAdmin = "settings-admin" // settings-admin
	ActorAccountAdmin  = "account-admin"  // account-admin
	ActorAccessAdmin   = "access-admin"   // access-admin
	ActorAuditReviewer = "audit-reviewer" // audit-reviewer
)

// ActorKeys lists the ten actor keys in the order the seed creates the accounts and the
// sign-in page lists them. Callers must not modify the slice.
var ActorKeys = []string{
	ActorParticipant, ActorNewcomer, ActorLead, ActorCoordinator, ActorSteward,
	ActorEventAdmin, ActorSettingsAdmin, ActorAccountAdmin, ActorAccessAdmin, ActorAuditReviewer,
}

// Department keys. Request.Departments holds a DepartmentID for each.
const (
	DeptRangers   = "rangers"
	DeptGate      = "gate"
	DeptGreeters  = "greeters"
	DeptMedical   = "medical"
	DeptDPW       = "dpw"
	DeptSanctuary = "sanctuary"
)

// DepartmentKeys lists the six department keys in the order the seed creates them.
// Callers must not modify the slice.
var DepartmentKeys = []string{DeptRangers, DeptGate, DeptGreeters, DeptMedical, DeptDPW, DeptSanctuary}

// WalkOns is how many walk-on people (no account) the core seeds; WalkOnKey(1..WalkOns)
// names them in Request.People.
const WalkOns = 12

// WalkOnKey returns "walkon-NN" (two digits, "walkon-01".."walkon-12"). It panics for n
// outside 1..WalkOns: that is a programming error in the caller, never an operator's input.
func WalkOnKey(n int) string {
	if n < 1 || n > WalkOns {
		panic(fmt.Sprintf("demoseed.WalkOnKey: %d is outside 1..%d", n, WalkOns))
	}
	return fmt.Sprintf("walkon-%02d", n)
}

// Request is what a Provider receives: the IDs the core created, by key, and a way to act.
// People holds the ten actors (by Actor* key) and the twelve walk-ons (by WalkOnKey).
type Request struct {
	Event       model.EventID
	Departments map[string]model.DepartmentID
	People      map[string]model.PersonID
	Actors      Actors
}

// Actors lets a provider run its own use cases as a demo account. As fails with
// errs.Invalid for an unknown key. The returned ctx carries that account's session for
// the demo's SessionLifetime; a provider calls As once per actor and reuses the ctx.
type Actors interface {
	As(ctx context.Context, key string) (context.Context, error)
}

// Result is what a provider reports: how many records it created.
type Result struct{ Items int }

// Provider seeds one module's demo data. It runs after the core's steps, under the demo's
// ContributorTimeout, against a database that holds no row of the module's yet. It uses
// the module's own services through the actors it is given, so every permission, audit
// and validation rule applies. It must create only data a Validate accepts and may not
// write anything about a real person. A failure fails the whole seed (demo.md decision
// 15); the operator reruns `bms demo reset`.
type Provider interface {
	Seed(ctx context.Context, req Request) (Result, error)
}
