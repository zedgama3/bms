package app

import (
	"context"
	"reflect"
	"testing"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/core/jobs/jobstest"
	"github.com/zedgama3/bms/internal/model"
)

// F34 (app.md amendment 2026-10-20, D44): inside a job the audit actor is the system, ahead
// of the session and the operator.
func TestAuditActor_InJob_IsTheSystem(t *testing.T) {
	person := model.NewID[model.PersonID]()
	session := identity.Session{Person: person}
	system := audit.Actor{Kind: audit.ActorSystem}

	// A ctx marked as a job's, taken from a handler run by the fake.
	fake := jobstest.NewFake("alpha")
	var jobCtx context.Context
	if err := fake.Register(jobs.Decl{Name: "alpha.sweep", Handler: func(ctx context.Context, _ jobs.Job) error {
		jobCtx = ctx
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	if err := fake.Run(context.Background(), "alpha.sweep", jobs.Job{}); err != nil {
		t.Fatal(err)
	}
	if jobCtx == nil || !jobs.InJob(jobCtx) {
		t.Fatal("the fake's handler ctx is not InJob")
	}
	sessionJobCtx := identity.WithSession(jobCtx, session)

	for _, tc := range []struct {
		name     string
		operator bool
		ctx      context.Context
	}{
		{"job ctx alone", false, jobCtx},
		{"job ctx with a session", false, sessionJobCtx},
		{"job ctx while the operator is set", true, jobCtx},
		{"job ctx with a session while the operator is set", true, sessionJobCtx},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &assembly{operator: tc.operator}
			got, ok := a.auditActor(tc.ctx)
			if !ok || !reflect.DeepEqual(got, system) {
				t.Errorf("auditActor = %+v, %v; want %+v, true (no person, no superuser)", got, ok, system)
			}
		})
	}
}
