package identity

import (
	"context"
	"fmt"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// Guard says which accounts are shared and must stay usable by everyone (D53, demo mode).
// Options.Guard is nil outside demo mode, which protects nothing. Protected fails with the
// store's kind (Unavailable); identity then fails the use case, so a broken guard never
// opens a protected account (closed). core/demo's service implements it.
type Guard interface {
	Protected(ctx context.Context, a model.AccountID) (bool, error)
}

// codeProtected is the field code of a change refused by the Guard.
const codeProtected = "protected"

// checkProtected asks the Guard about a and returns a Permission error (field account, code
// protected) when it is protected, the Guard's own error (wrapped, kind kept) when it
// fails, and nil otherwise, always nil without a Guard.
func (s *Service) checkProtected(ctx context.Context, op string, a model.AccountID) error {
	if s.guard == nil {
		return nil
	}
	p, err := s.guard.Protected(ctx, a)
	if err != nil {
		return fmt.Errorf("%s: guard: %w", op, err)
	}
	if p {
		return errProtected(op)
	}
	return nil
}

// errProtected is the refusal of a change to a protected account.
func errProtected(op string) error {
	return errs.E(op, errs.Permission, &validate.FieldError{
		Field: "account", Code: codeProtected, Reason: "this account is shared and cannot be changed",
	})
}
