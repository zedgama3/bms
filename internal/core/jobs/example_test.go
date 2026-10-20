package jobs_test

import (
	"context"
	"fmt"
	"time"

	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/core/jobs/jobstest"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/model"
)

// A module registers its jobs in Start, enqueues a deferred one from a use case with record
// references only, and tests its handler with jobstest.Fake.Run.
func ExampleRegistrar() {
	reg := jobstest.NewFake("volunteers")
	remind := func(ctx context.Context, j jobs.Job) error {
		// A handler re-reads the records its references name; it never trusts them to be current.
		fmt.Println("in job:", jobs.InJob(ctx), "attempt:", j.Attempt, "references:", len(j.Args))
		return nil
	}
	if err := reg.Register(jobs.Decl{Name: "volunteers.remind", Handler: remind, Timeout: time.Minute}); err != nil {
		fmt.Println(err)
		return
	}

	signup := model.NewID[model.ID[struct{}]]()
	_, err := reg.Enqueue(context.Background(), jobs.Request{
		Name:  "volunteers.remind",
		Key:   "signup-" + signup.String(),
		Args:  signals.NewPayload(signals.Ref("signup", signup)),
		RunAt: time.Now().Add(24 * time.Hour),
	})
	fmt.Println("enqueued:", len(reg.Enqueued()), err)

	// Another owner's name is refused, as the real Registrar refuses it.
	_, err = reg.Enqueue(context.Background(), jobs.Request{Name: "files.sweep"})
	fmt.Println("foreign name refused:", err != nil)

	err = reg.Run(context.Background(), "volunteers.remind", jobs.Job{Attempt: 1, Args: reg.Enqueued()[0].Args})
	fmt.Println("run:", err)
	// Output:
	// enqueued: 1 <nil>
	// foreign name refused: true
	// in job: true attempt: 1 references: 1
	// run: <nil>
}
