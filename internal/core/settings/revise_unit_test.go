package settings

import (
	"context"
	"errors"
	"testing"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
)

// The happy path of Revise over the in-memory store (boundaries A2, point 1), with no
// database: a reported change bumps the revision and the local snapshot carries it at once;
// no change leaves both alone; fn's error leaves both alone and keeps its kind.
func TestRevise_Changed_BumpsAndInstallsSnapshot(t *testing.T) {
	st := newMemStore(7)
	s := newD40Svc(t, st, secret.Secret{})
	ctx := t.Context()
	if got, err := s.Revision(ctx); err != nil || got != 7 {
		t.Fatalf("Revision = %d, %v; want 7", got, err)
	}
	ran := 0
	rev, err := s.Revise(ctx, func(context.Context) (bool, error) { ran++; return true, nil })
	if err != nil || rev != 8 || ran != 1 {
		t.Fatalf("Revise = %d, %v (ran %d); want 8", rev, err, ran)
	}
	if got := s.snap.Load().revision; got != 8 {
		t.Errorf("the snapshot carries revision %d, want the bumped 8 at once", got)
	}
	if got, err := s.Revision(ctx); err != nil || got != 8 {
		t.Errorf("Revision after Revise = %d, %v; want 8", got, err)
	}

	rev, err = s.Revise(ctx, func(context.Context) (bool, error) { return false, nil })
	if err != nil || rev != 8 || st.rev != 8 {
		t.Errorf("an unchanged Revise = %d, %v (store %d); want the current 8", rev, err, st.rev)
	}

	boom := errs.E("test.fn", errs.Conflict, errors.New("fn failed"))
	_, err = s.Revise(ctx, func(context.Context) (bool, error) { return true, boom })
	kindOf(t, err, errs.Conflict)
	if st.rev != 8 || s.snap.Load().revision != 8 {
		t.Errorf("a failed Revise left revision %d / snapshot %d", st.rev, s.snap.Load().revision)
	}
}
