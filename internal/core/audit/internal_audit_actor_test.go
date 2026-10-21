package audit

import (
	"context"
	"testing"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// F13b: an inconsistent actor from the ActorFunc is a wiring bug (amendment 2026-10-20).
func TestResolveActor_ActorFuncReturnsBadActor_ReturnsInternal(t *testing.T) {
	l := readOnlyLog(t)
	p := model.NewID[model.PersonID]()
	for _, tc := range []struct {
		name string
		bad  Actor
	}{
		{"anonymous", Actor{Kind: ActorAnonymous}},
		{"zero actor", Actor{}},
		{"person without Person", Actor{Kind: ActorPerson}},
		{"operator with a Person", Actor{Kind: ActorOperator, Person: p}},
		{"operator superuser", Actor{Kind: ActorOperator, Superuser: true}},
		{"system with a Person", Actor{Kind: ActorSystem, Person: p}},
		{"system superuser", Actor{Kind: ActorSystem, Superuser: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l.opts.Actor = func(context.Context) (Actor, bool) { return tc.bad, true }
			if _, err := l.resolveActor(t.Context(), Actor{}); !errs.Is(err, errs.Internal) {
				t.Errorf("ActorFunc returning %+v: err = %v, want Internal", tc.bad, err)
			}
		})
	}
}

func TestResolveActor_ActorFuncReturnsSystem_IsTheSystem(t *testing.T) {
	l := readOnlyLog(t)
	l.opts.Actor = func(context.Context) (Actor, bool) {
		return Actor{Kind: ActorSystem, Titles: []string{"ignored"}}, true
	}
	got, err := l.resolveActor(t.Context(), Actor{})
	if err != nil {
		t.Fatalf("resolveActor: %v", err)
	}
	if got.Kind != ActorSystem || !got.Person.IsZero() || got.Superuser || len(got.Titles) != 0 {
		t.Errorf("actor = %+v, want the system with no person, no superuser, no titles", got)
	}
}
