package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/lib/errs"
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

// lazyFilesStore is settings' files.Store (scoped "settings", for the logo) over a.files,
// which files' part sets after settings is built: each method resolves the scoped store when
// called and, until files is built, fails errs.Unavailable. It does no I/O of its own.
type lazyFilesStore struct{ a *assembly }

var _ files.Store = lazyFilesStore{}

// filesOwner is the files owner and bucket of the settings area (the logo).
const filesOwner = settingsArea

// store resolves the scoped store, or fails errs.Unavailable before files is built.
func (l lazyFilesStore) store(op string) (files.Store, error) {
	if l.a.files == nil {
		return nil, errs.E(op, errs.Unavailable, errors.New("the files service is not built yet"))
	}
	st, err := l.a.files.Scoped(filesOwner)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return st, nil
}

// Stage stages through the scoped store.
func (l lazyFilesStore) Stage(ctx context.Context, up files.Upload) (files.Staged, error) {
	st, err := l.store("app.lazyFilesStore.Stage")
	if err != nil {
		return files.Staged{}, err
	}
	return st.Stage(ctx, up) //nolint:wrapcheck // the store's own error
}

// Attach attaches through the scoped store.
func (l lazyFilesStore) Attach(ctx context.Context, s files.Staged, at files.Attachment) (files.File, error) {
	st, err := l.store("app.lazyFilesStore.Attach")
	if err != nil {
		return files.File{}, err
	}
	return st.Attach(ctx, s, at) //nolint:wrapcheck // the store's own error
}

// Discard discards through the scoped store.
func (l lazyFilesStore) Discard(ctx context.Context, s files.Staged) error {
	st, err := l.store("app.lazyFilesStore.Discard")
	if err != nil {
		return err
	}
	return st.Discard(ctx, s) //nolint:wrapcheck // the store's own error
}

// Get reads through the scoped store.
func (l lazyFilesStore) Get(ctx context.Context, id files.FileID) (files.File, error) {
	st, err := l.store("app.lazyFilesStore.Get")
	if err != nil {
		return files.File{}, err
	}
	return st.Get(ctx, id) //nolint:wrapcheck // the store's own error
}

// ByRecord lists through the scoped store.
func (l lazyFilesStore) ByRecord(ctx context.Context, rec audit.Ref) ([]files.File, error) {
	st, err := l.store("app.lazyFilesStore.ByRecord")
	if err != nil {
		return nil, err
	}
	return st.ByRecord(ctx, rec) //nolint:wrapcheck // the store's own error
}

// Open opens through the scoped store.
func (l lazyFilesStore) Open(ctx context.Context, id files.FileID) (files.File, files.Content, error) {
	st, err := l.store("app.lazyFilesStore.Open")
	if err != nil {
		return files.File{}, nil, err
	}
	return st.Open(ctx, id) //nolint:wrapcheck // the store's own error
}

// Delete deletes through the scoped store.
func (l lazyFilesStore) Delete(ctx context.Context, id files.FileID) error {
	st, err := l.store("app.lazyFilesStore.Delete")
	if err != nil {
		return err
	}
	return st.Delete(ctx, id) //nolint:wrapcheck // the store's own error
}

// DeleteByRecord deletes a record's files through the scoped store.
func (l lazyFilesStore) DeleteByRecord(ctx context.Context, rec audit.Ref) (int, error) {
	st, err := l.store("app.lazyFilesStore.DeleteByRecord")
	if err != nil {
		return 0, err
	}
	return st.DeleteByRecord(ctx, rec) //nolint:wrapcheck // the store's own error
}

// lazyIdentityPolicy is identity's PolicySource over a.settings, which settings' part sets
// after identity is built: until then the policy is the shipped default, as it is when
// identity is given no source. It does no I/O of its own.
type lazyIdentityPolicy struct{ a *assembly }

// Policy reads the accounts.* settings when there is a settings service, else the defaults.
func (l lazyIdentityPolicy) Policy(ctx context.Context) (identity.Policy, error) {
	if l.a.settings == nil {
		return identity.DefaultPolicy(), nil
	}
	r, err := l.a.settings.Scoped(accountsSettings)
	if err != nil {
		return identity.Policy{}, fmt.Errorf("app.lazyIdentityPolicy: %w", err)
	}
	return settingsPolicy{r: r}.Policy(ctx)
}

// lazyPeoplePolicy is people's PolicySource over a.settings, resolved when called, so the
// part order does not matter to it.
type lazyPeoplePolicy struct{ a *assembly }

// Policy reads the people.* settings when there is a settings service, else the defaults.
func (l lazyPeoplePolicy) Policy(ctx context.Context) (people.Policy, error) {
	if l.a.settings == nil {
		return people.DefaultPolicy(), nil
	}
	r, err := l.a.settings.Scoped(peopleSettings)
	if err != nil {
		return people.Policy{}, fmt.Errorf("app.lazyPeoplePolicy: %w", err)
	}
	return peoplePolicy{r: r}.Policy(ctx)
}
