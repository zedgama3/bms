package app

import (
	"context"

	"github.com/zedgama3/bms/internal/model"
)

// The adapters in this file are the seams of parts built before the part they wait for, which
// the seam table in plan/07-abstractions.md lists: they hold small values that read the
// assembly's fields when called and answer "nothing yet" until the later part has set them.
// They do no I/O of their own.

// lazyNames is identity's NameSource over a.names, which people's part sets: until then the
// playa name is "".
type lazyNames struct{ a *assembly }

// PlayaNameOf asks the source when there is one, else returns "".
func (l lazyNames) PlayaNameOf(ctx context.Context, person model.PersonID) (string, error) {
	if l.a.names == nil {
		return "", nil
	}
	return l.a.names.PlayaNameOf(ctx, person) //nolint:wrapcheck // the source's own error
}
