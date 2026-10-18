package identity

import (
	"context"
	"fmt"
	"io"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// The lines `bms create-superuser <email>` prints, exactly (identity.md "The command").
const (
	linePasswordPrefix = "Password (shown once, not stored anywhere else): "
	lineSuperuserHint  = "Sign in at /sign-in and keep this password in your password manager."
	lineCreated        = "Superuser account created."
	lineReset          = "Superuser account reset; its sessions have ended."
)

// CreateSuperuser is the logic of `bms create-superuser <email>`, for app to call after it
// has built the assembly and parsed the argument. It runs EnsureSuperuser and prints to out
// exactly: "Superuser account created." or "Superuser account reset; its sessions have
// ended.", then "Password (shown once, not stored anywhere else): <password>", then
// "Sign in at /sign-in and keep this password in your password manager." It never prints
// the address (the operator typed it) and prints nothing when it fails.
//
// The returned error keeps EnsureSuperuser's kind so app can choose the exit code: Invalid
// (a bad address) and Conflict (a participant account holds it; tell the operator to use
// another address) exit 2; Unavailable wrapping db.ErrReadOnly ("this copy is read-only;
// run it against the writing copy") and everything else exit 1. A failed write to out is
// errs.Internal after the account was already created or reset.
func (s *Service) CreateSuperuser(ctx context.Context, out io.Writer, email string) error {
	const op = "identity.Service.CreateSuperuser"
	if out == nil {
		return errs.E(op, errs.Invalid, errNilWriter)
	}
	res, err := s.EnsureSuperuser(ctx, model.Email(email))
	if err != nil {
		return wrap(op, err)
	}
	headline := lineReset
	if res.Created {
		headline = lineCreated
	}
	if _, err := fmt.Fprintf(out, "%s\n%s%s\n%s\n", headline, linePasswordPrefix, res.Password.Value(), lineSuperuserHint); err != nil {
		return errs.E(op, errs.Internal, fmt.Errorf("write the result: %w", err))
	}
	return nil
}
