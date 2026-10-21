//go:build integration

package files_test

import (
	"context"
	"errors"
	"testing"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/objects"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// Helpers only the integration tests use.

type photoTag struct{}
type photoID = model.ID[photoTag]

// wantCode checks the kind and that some field error carries code (a Conflict such as quota
// or in_use names a code, and its field is the implementation's choice).
func codeErr(t testing.TB, err error, kind errs.Kind, code string) *validate.FieldError {
	t.Helper()
	wantKind(t, err, kind)
	var seen []string
	for _, fe := range validate.Fields(err) {
		if fe.Code == code {
			return fe
		}
		seen = append(seen, fe.Field+"/"+fe.Code)
	}
	t.Fatalf("no field error with code %q in %v (found %v)", code, err, seen)
	return nil
}

// wantCode is codeErr for callers that do not need the field error.
func wantCode(t testing.TB, err error, kind errs.Kind, code string) {
	t.Helper()
	if codeErr(t, err, kind, code) == nil {
		t.Fatal("unreachable")
	}
}

func asPerson(ctx context.Context, p model.PersonID) context.Context {
	return context.WithValue(ctx, actorKey{}, authz.Actor{Person: p})
}

func asSuperuser(ctx context.Context, p model.PersonID) context.Context {
	return context.WithValue(ctx, actorKey{}, authz.Actor{Person: p, Superuser: true})
}

func withDB(d db.DB) rigOption {
	return func(r *rig, o *files.Options) { o.DB, r.fake = d, nil }
}

// auditRef keeps signatures short.
type auditRef = audit.Ref

func audit0() audit.Ref { return audit.Ref{} }

func isMissing(err error) bool { return errors.Is(err, objects.ErrMissing) }

func photoRef() audit.Ref { return audit.RefOf(kindPhoto, model.NewID[photoID]()) }

func withInner(s objects.Store) rigOption {
	return func(r *rig, o *files.Options) {
		r.inner = s
		r.store = newFault(s)
		o.Objects = r.store
	}
}
